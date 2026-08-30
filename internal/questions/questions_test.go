package questions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"h3helper/internal/llm"
	"h3helper/internal/skill"
	"h3helper/internal/slots"
	"h3helper/internal/task"
)

func refTask() *task.Task {
	return &task.Task{
		Constraints: task.Constraints{
			Mode:          "Ref2VA",
			Duration:      5.17,
			Frames:        124,
			FPS:           24,
			MaxShots:      3,
			PictureLabels: []string{"<Picture 1>", "<Picture 2>"},
		},
		Images: []task.Image{
			{Label: "<Picture 1>", Slot: "image1", Role: "reference", Wired: true},
			{Label: "<Picture 2>", Slot: "image2", Role: "reference", Wired: true},
		},
		Answers: map[string]string{},
		Facts: map[string]task.Facts{
			"<Picture 1>": {Label: "<Picture 1>", Summary: "a woman at a window",
				Subjects: []task.FactSubject{{Name: "young woman", Kind: "person"}}},
			"<Picture 2>": {Label: "<Picture 2>", Summary: "an empty train platform",
				Subjects: []task.FactSubject{{Name: "platform", Kind: "environment"}}},
		},
	}
}

func TestNextAsksTheBriefWithoutAModel(t *testing.T) {
	page, err := Next(context.Background(), nil, refTask())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if len(page.Questions) != 1 {
		t.Fatalf("got %d questions, want the single opening question", len(page.Questions))
	}
	if page.Questions[0].Role != task.RoleBrief {
		t.Errorf("first question role = %q, want %q", page.Questions[0].Role, task.RoleBrief)
	}
}

func TestNextNeedsAClientOnceTheBriefIsIn(t *testing.T) {
	tk := refTask()
	tk.Answers[slots.Brief] = "她走上站台"
	tk.Asked = []task.Question{{Slot: slots.Brief, Role: task.RoleBrief}}
	if _, err := Next(context.Background(), nil, tk); err == nil {
		t.Fatal("planning without an endpoint should fail so the caller can fall back")
	}
}

func TestNextUsesTheSelectedModelsConfiguredTokenBudget(t *testing.T) {
	const configured = 4321
	gotTokens := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotTokens = body.MaxTokens
		reply := `{"done":true,"note":"够了","page":{"questions":[]}}`
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: [DONE]\n\n", reply)
	}))
	defer upstream.Close()

	tk := refTask()
	tk.Answers[slots.Brief] = "她走上站台"
	tk.Asked = []task.Question{{Slot: slots.Brief, Role: task.RoleBrief}}
	client := llm.New(upstream.URL, "", "fast-question", configured, 0.2, "")
	if _, err := Next(context.Background(), client, tk); err != nil {
		t.Fatalf("Next: %v", err)
	}
	if gotTokens != configured {
		t.Fatalf("question max_tokens = %d, want configured value %d", gotTokens, configured)
	}
}

func TestParsePageReplacesOptionsWithTheControlledVocabulary(t *testing.T) {
	raw := `{"done": false, "remaining": 2, "page": {"title": "镜头",
	  "questions": [{"slot": "camera", "role": "camera", "title": "主镜头怎么动？",
	    "kind": "choice", "vocab": "camera",
	    "options": [{"value": "swoosh", "label": "随便编的"}]}]}}`

	page, err := parsePage(raw, refTask())
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	q := page.Questions[0]
	if len(q.Options) != len(skill.CameraMotions) {
		t.Fatalf("got %d options, want the %d canonical camera motions", len(q.Options), len(skill.CameraMotions))
	}
	for _, o := range q.Options {
		if o.Value == "swoosh" {
			t.Fatal("an invented camera motion survived the vocabulary check")
		}
	}
	if !q.AllowFree {
		t.Error("camera motion should still accept a phrase outside the list")
	}
	if q.Title != "你希望镜头怎么拍？" || !strings.Contains(q.Help, "摄影机怎么移动") {
		t.Errorf("camera question is not beginner-facing Chinese: %+v", q)
	}
	for _, option := range q.Options {
		if option.Label == option.Value || option.Desc == "" {
			t.Errorf("camera option is not localized for beginners: %+v", option)
		}
	}
}

func TestParsePageMakesStylePresetQuestionBeginnerFacing(t *testing.T) {
	raw := `{"page":{"questions":[{"slot":"look","role":"style","title":"Visual style?","help":"Choose an aesthetic","kind":"choice","vocab":"style"}]}}`

	page, err := parsePage(raw, refTask())
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	q := page.Questions[0]
	if q.Title != "你希望画面看起来像哪一种？" || !strings.Contains(q.Help, "不需要懂专业术语") {
		t.Errorf("style question is not beginner-facing Chinese: %+v", q)
	}
	for _, option := range q.Options {
		if option.Label == option.Value || option.Desc == "" {
			t.Errorf("style option is not localized for beginners: %+v", option)
		}
	}
}

func TestParsePageSkipsAnsweredAndCapsThePage(t *testing.T) {
	tk := refTask()
	tk.Answers["style"] = "live-action"

	raw := `{"page": {"questions": [
	  {"slot": "style", "title": "风格？", "kind": "text"},
	  {"slot": "a", "title": "一", "kind": "text"},
	  {"slot": "a", "title": "重复的一", "kind": "text"},
	  {"slot": "b", "title": "二", "kind": "text"},
	  {"slot": "c", "title": "三", "kind": "text"},
	  {"slot": "d", "title": "四", "kind": "text"}]}}`

	page, err := parsePage(raw, tk)
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	if len(page.Questions) != MaxQuestions {
		t.Fatalf("got %d questions, want at most %d", len(page.Questions), MaxQuestions)
	}
	for _, q := range page.Questions {
		if q.Slot == "style" {
			t.Error("an already answered slot was asked again")
		}
	}
	if page.Questions[0].Slot != "a" || page.Questions[1].Slot != "b" {
		t.Errorf("duplicate slots were not collapsed: %v", page.Questions)
	}
}

func TestParsePageDowngradesAChoiceWithNoOptions(t *testing.T) {
	raw := `{"page": {"questions": [{"slot": "x", "title": "问", "kind": "choice"}]}}`
	page, err := parsePage(raw, refTask())
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	if page.Questions[0].Kind != task.KindText {
		t.Errorf("kind = %q, want it downgraded to text", page.Questions[0].Kind)
	}
}

func TestParsePageKeepsAnEmptyPageOnlyWhenDone(t *testing.T) {
	if _, err := parsePage(`{"done": false, "page": {"questions": []}}`, refTask()); err == nil {
		t.Error("an empty page that is not done should be rejected so the caller can fall back")
	}
	page, err := parsePage(`{"done": true, "note": "齐了"}`, refTask())
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	if !page.Done || page.Note != "齐了" {
		t.Errorf("done page = %+v", page)
	}
}

func TestParsePageIgnoresUnknownRolesAndKeepsKnownOnes(t *testing.T) {
	raw := `{"page": {"questions": [
	  {"slot": "x", "role": "make_it_pretty", "title": "问", "kind": "text"},
	  {"slot": "y", "role": "Dialogue", "title": "台词", "kind": "textarea"}]}}`
	page, err := parsePage(raw, refTask())
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	if page.Questions[0].Role != task.RoleFreeform {
		t.Errorf("unknown role survived: %q", page.Questions[0].Role)
	}
	if page.Questions[1].Role != task.RoleDialogue {
		t.Errorf("role = %q, want %q", page.Questions[1].Role, task.RoleDialogue)
	}
}

func TestParsePageKeepsOneQuestionPerRole(t *testing.T) {
	raw := `{"page": {"questions": [
	  {"slot": "amplitude", "role": "camera_dynamic", "title": "幅度多大？", "kind": "choice", "vocab": "camera_dynamic"},
	  {"slot": "speed", "role": "camera_dynamic", "title": "速度多快？", "kind": "choice", "vocab": "camera_dynamic"},
	  {"slot": "role1", "role": "image_role", "label": "<Picture 1>", "title": "一号图？", "kind": "text"},
	  {"slot": "role2", "role": "image_role", "label": "<Picture 2>", "title": "二号图？", "kind": "text"}]}}`

	page, err := parsePage(raw, refTask())
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	if len(page.Questions) != MaxQuestions {
		t.Fatalf("got %d questions, want the concise page cap %d: %v", len(page.Questions), MaxQuestions, page.Questions)
	}
	if page.Questions[0].Slot != "amplitude" {
		t.Errorf("kept %q, want the first of the two camera questions", page.Questions[0].Slot)
	}
	if page.Questions[1].Label != "<Picture 1>" {
		t.Error("the first reference-specific question should survive the role check")
	}
}

func TestSystemPromptCarriesTheGuideAndTheAssets(t *testing.T) {
	tk := refTask()
	prompt := systemPrompt(tk)
	if !strings.Contains(prompt, "subject_definitions") {
		t.Error("the full-reference guide should be in the interviewer's prompt")
	}
	state := statePrompt(tk)
	for _, want := range []string{"<Picture 1>", "<Picture 2>", "young woman", "platform"} {
		if !strings.Contains(state, want) {
			t.Errorf("state prompt is missing %q, so the agent cannot tell the pictures apart", want)
		}
	}
}

func TestFallbackAsksTheFixedTable(t *testing.T) {
	page := Fallback(refTask(), "接口超时")
	if !page.Fallback || len(page.Questions) == 0 {
		t.Fatalf("fallback page = %+v", page)
	}
	if !strings.Contains(page.Intro, "接口超时") {
		t.Errorf("intro = %q, want it to say why", page.Intro)
	}
}

func TestInterviewStopsAfterFourConcisePages(t *testing.T) {
	tk := refTask()
	tk.Answers[slots.Brief] = "她走上站台"
	tk.Asked = []task.Question{{Slot: slots.Brief, Role: task.RoleBrief}}
	tk.Rounds = make([]task.Round, maxPages)

	page, err := Next(context.Background(), nil, tk)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !page.Done || len(page.Questions) != 0 {
		t.Fatalf("page = %+v, want the interview to stop without another model call", page)
	}
}
