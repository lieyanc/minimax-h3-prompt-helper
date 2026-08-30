package slots

import (
	"strings"
	"testing"

	"h3helper/internal/task"
)

func refTask() *task.Task {
	return &task.Task{
		Constraints: task.Constraints{
			Mode:          "Ref2VA",
			Duration:      5.17,
			Frames:        124,
			MaxShots:      3,
			PictureLabels: []string{"<Picture 1>"},
		},
		Images:  []task.Image{{Label: "<Picture 1>", Slot: "ref_image_0", Role: "reference"}},
		Answers: map[string]string{},
		Facts:   map[string]task.Facts{},
	}
}

func TestNextAsksAtMostThreeRequiredFirst(t *testing.T) {
	tk := refTask()
	qs := Next(tk, 3)
	if len(qs) != 3 {
		t.Fatalf("got %d questions, want 3", len(qs))
	}
	for _, q := range qs {
		if !q.Required {
			t.Errorf("required questions must come first, got optional %q", q.Slot)
		}
	}
	if qs[0].Slot != Brief {
		t.Errorf("first question = %q, want %q", qs[0].Slot, Brief)
	}
}

func TestFullReferenceModeAsksPerImage(t *testing.T) {
	tk := refTask()
	var slots []string
	for _, d := range Definitions(tk) {
		slots = append(slots, d.Slot)
	}
	joined := strings.Join(slots, ",")
	for _, want := range []string{
		ImageRolePrefix + "<Picture 1>",
		ImageRetentionPrefix + "<Picture 1>",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("full-reference mode should ask for %q; got slots: %v", want, slots)
		}
	}

	// Base modes have no per-image slots.
	base := refTask()
	base.Constraints.Mode = "I2VA"
	for _, d := range Definitions(base) {
		if strings.HasPrefix(d.Slot, ImageRolePrefix) {
			t.Errorf("base mode should not ask for image roles, got %q", d.Slot)
		}
	}
}

func TestAnsweringShrinksTheRequiredSet(t *testing.T) {
	tk := refTask()
	before := len(MissingRequired(tk))
	if before == 0 {
		t.Fatal("a fresh task should have required slots")
	}

	guard := 0
	for len(Next(tk, 3)) > 0 && guard < 20 {
		guard++
		for _, q := range Next(tk, 3) {
			value := "answer"
			if len(q.Options) > 0 {
				value = q.Options[0].Value
				if value == "" && len(q.Options) > 1 {
					value = q.Options[1].Value
				}
			}
			tk.Answers[q.Slot] = value
		}
	}
	if missing := MissingRequired(tk); len(missing) != 0 {
		t.Errorf("still missing after answering everything: %v", missing)
	}
	answered, total := Progress(tk)
	if answered != total || total == 0 {
		t.Errorf("Progress = %d/%d, want a full bar", answered, total)
	}
}

func TestDialogueSlotsOnlyAppearWhenThereIsDialogue(t *testing.T) {
	tk := refTask()
	has := func(slot string) bool {
		for _, d := range Definitions(tk) {
			if d.Slot != slot {
				continue
			}
			return d.Applies == nil || d.Applies(tk)
		}
		return false
	}

	tk.Answers[HasDialogue] = "no"
	if has(DialogueLines) {
		t.Error("dialogue lines should not be asked when there is no dialogue")
	}
	tk.Answers[HasDialogue] = "yes"
	if !has(DialogueLines) {
		t.Error("dialogue lines should be asked once dialogue is confirmed")
	}
}

func TestDialogueTexts(t *testing.T) {
	tk := refTask()
	tk.Answers[DialogueLines] = strings.Join([]string{
		"年轻女性，气声 | English | I get off at the next station.",
		"  ",
		"男人 | English | Wait for me!",
		"just the line",
	}, "\n")

	got := DialogueTexts(tk)
	want := []string{
		"I get off at the next station.",
		"Wait for me!",
		"just the line",
	}
	if len(got) != len(want) {
		t.Fatalf("DialogueTexts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("DialogueTexts[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestVisionFactsDriveSuggestions(t *testing.T) {
	tk := refTask()
	tk.Facts["<Picture 1>"] = task.Facts{
		Label:          "<Picture 1>",
		Style:          "live-action",
		PossibleAction: "The water surface ripples as the light fades.",
		Environment:    "a still lake ringed by dark conifers",
		Subjects: []task.FactSubject{
			{Name: "winter lakeside", Kind: "environment"},
		},
		VisibleText: []string{"营业中"},
	}

	byslot := map[string]task.Question{}
	for _, q := range Next(tk, 50) {
		byslot[q.Slot] = q
	}
	if got := byslot[Style].Suggestion; got != "live-action" {
		t.Errorf("style suggestion = %q, want live-action", got)
	}
	if got := byslot[Brief].Suggestion; !strings.Contains(got, "ripples") {
		t.Errorf("brief suggestion = %q, want the vision model's action guess", got)
	}
	if got := byslot[ImageRolePrefix+"<Picture 1>"].Suggestion; got != "environment" {
		t.Errorf("role suggestion for an environment-only image = %q, want environment", got)
	}
	if got := byslot[ScreenText].Suggestion; got != "营业中" {
		t.Errorf("screen text suggestion = %q, want the transcribed text", got)
	}
}

func TestShotOptionsRespectTheWorkflowDuration(t *testing.T) {
	tk := refTask()
	var opts []task.Option
	for _, d := range Definitions(tk) {
		if d.Slot == Shots {
			opts = d.Options
		}
	}
	if len(opts) != tk.Constraints.MaxShots {
		t.Fatalf("got %d shot options, want %d", len(opts), tk.Constraints.MaxShots)
	}
	if opts[0].Value != "1" {
		t.Errorf("the first shot option should be a single shot, got %q", opts[0].Value)
	}
}
