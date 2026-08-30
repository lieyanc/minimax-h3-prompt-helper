package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"h3helper/internal/comfy"
	"h3helper/internal/config"
	"h3helper/internal/store"
	"h3helper/internal/task"
)

func TestAspectLabel(t *testing.T) {
	cases := []struct {
		w, h int
		want string
	}{
		// ResolutionSelector rounds each edge up to a multiple of 32, so a
		// "16:9" preset actually produces 864x480.
		{864, 480, "≈16:9 横构图"},
		{1344, 768, "≈16:9 横构图"},
		{1920, 1080, "16:9 横构图"},
		{768, 1344, "≈9:16 竖构图"},
		{1024, 1024, "1:1 方构图"},
		{1000, 300, "10:3 横构图"},
		{0, 100, ""},
	}
	for _, tc := range cases {
		if got := aspectLabel(tc.w, tc.h); got != tc.want {
			t.Errorf("aspectLabel(%d, %d) = %q, want %q", tc.w, tc.h, got, tc.want)
		}
	}
}

func TestMaxShots(t *testing.T) {
	cases := map[float64]int{
		3.04:  2,
		5.17:  3,
		0.5:   1,
		15.08: 6,
	}
	for duration, want := range cases {
		if got := maxShots(duration); got != want {
			t.Errorf("maxShots(%v) = %d, want %d", duration, got, want)
		}
	}
}

func TestVisionBlockersRequireEveryReferenceImage(t *testing.T) {
	tk := &task.Task{
		Images: []task.Image{
			{Label: "<Picture 1>"},
			{Label: "<Picture 2>"},
			{Label: "<Picture 3>"},
		},
		Facts: map[string]task.Facts{
			"<Picture 1>": {Label: "<Picture 1>", Summary: "a woman"},
			"<Picture 2>": {Label: "<Picture 2>", Error: "模型超时"},
		},
	}

	got := visionBlockers(tk)
	if len(got) != 2 {
		t.Fatalf("visionBlockers = %v, want one failed and one missing analysis", got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"<Picture 2> 视觉分析失败", "<Picture 3> 尚未完成视觉分析"} {
		if !strings.Contains(joined, want) {
			t.Errorf("visionBlockers = %v, missing %q", got, want)
		}
	}

	tk.Facts["<Picture 2>"] = task.Facts{Label: "<Picture 2>", Summary: "a station"}
	tk.Facts["<Picture 3>"] = task.Facts{Label: "<Picture 3>", Summary: "a train"}
	if got := visionBlockers(tk); len(got) != 0 {
		t.Errorf("visionBlockers = %v after all analyses succeeded, want none", got)
	}
}

func TestVisionBlockersAllowTasksWithoutImages(t *testing.T) {
	if got := visionBlockers(&task.Task{}); len(got) != 0 {
		t.Errorf("visionBlockers = %v for a text-only task, want none", got)
	}
}

func TestVisionAnalysisRunsImagesConcurrently(t *testing.T) {
	const imageCount = 3

	started := make(chan struct{}, imageCount)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }

	var active atomic.Int32
	var peak atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		for {
			previous := peak.Load()
			if current <= previous || peak.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)

		reply := `{"style":"live-action","summary":"a test frame","subjects":[],"environment":"room","lighting":"daylight","composition":"centered","shotSize":"medium shot","visibleText":[],"colorPalette":"neutral","possibleAction":"The subject moves."}`
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, reply)
	}))
	defer func() {
		unblock()
		upstream.Close()
	}()

	root := t.TempDir()
	cfg, err := config.Load(filepath.Join(root, "config.json"), root)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := cfg.Update(func(c *config.Config) {
		c.Providers = []config.Provider{{ID: "test", Name: "test", BaseURL: upstream.URL}}
		c.Models = []config.Model{{ID: "vision-test", ProviderID: "test", Model: "vision-test", MaxTokens: 100}}
		c.Vision = config.VisionConfig{ModelID: "vision-test", ImageMaxEdge: 0}
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	st, err := store.New(filepath.Join(root, "tasks"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	uploads := filepath.Join(root, "uploads")
	taskID := "concurrent-vision"
	uploadDir := filepath.Join(uploads, taskID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("create uploads: %v", err)
	}
	tk := &task.Task{ID: taskID, Facts: map[string]task.Facts{}}
	for i := 1; i <= imageCount; i++ {
		name := fmt.Sprintf("image-%d.jpg", i)
		if err := os.WriteFile(filepath.Join(uploadDir, name), []byte("test image"), 0o644); err != nil {
			t.Fatalf("write image: %v", err)
		}
		tk.Images = append(tk.Images, task.Image{
			Label: fmt.Sprintf("<Picture %d>", i), Origin: task.OriginUpload, File: name,
		})
	}
	if err := st.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	handler := New(cfg, comfy.NewLibrary(nil, ""), st, uploads, nil, "test").Handler()
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/tasks/"+taskID+"/analyze", nil))
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for i := 0; i < imageCount; i++ {
		select {
		case <-started:
		case <-deadline:
			t.Fatalf("only %d/%d vision requests started before release; analysis is still sequential", i, imageCount)
		}
	}
	if got := peak.Load(); got != imageCount {
		t.Errorf("peak concurrent vision requests = %d, want %d", got, imageCount)
	}

	unblock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("analysis handler did not finish after releasing the model requests")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("analyze status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	updated, err := st.Get(taskID)
	if err != nil {
		t.Fatalf("get analyzed task: %v", err)
	}
	if got := len(updated.Facts); got != imageCount {
		t.Errorf("stored facts = %d, want %d", got, imageCount)
	}
}

func TestVisionAnalysisRefreshesTheQuestionContext(t *testing.T) {
	tk := &task.Task{
		Images:  []task.Image{{Label: "<Picture 1>"}},
		Facts:   map[string]task.Facts{"<Picture 1>": {Label: "<Picture 1>", PossibleAction: "The woman opens the door."}},
		Answers: map[string]string{},
	}

	refreshPlanAfterVision(tk)
	if len(tk.Pending) != 1 || tk.Pending[0].Role != task.RoleBrief {
		t.Fatalf("pending = %+v, want the rebuilt opening brief question", tk.Pending)
	}
	if got := tk.Pending[0].Suggestion; got != "The woman opens the door." {
		t.Errorf("brief suggestion = %q, want the vision action", got)
	}

	tk.Answers[tk.Pending[0].Slot] = "她推门进屋"
	tk.Rounds = append(tk.Rounds, task.Round{})
	tk.Pending = []task.Question{{Slot: "stale", Title: "旧问题"}}
	tk.Plan.Questions = tk.Pending
	refreshPlanAfterVision(tk)
	if len(tk.Pending) != 0 || len(tk.Plan.Questions) != 0 || tk.Plan.Done {
		t.Errorf("plan was not reopened for fact-aware planning: %+v", tk.Plan)
	}
}

func TestTaskViewKeepsLegacyTasksAtTheVisionGate(t *testing.T) {
	tk := &task.Task{
		Step:    task.StepQuestions,
		Images:  []task.Image{{Label: "<Picture 1>"}},
		Facts:   map[string]task.Facts{},
		Answers: map[string]string{},
	}

	view := (&Server{}).taskView(tk)
	if view.Step != task.StepImages {
		t.Errorf("step = %q, want %q while vision analysis is pending", view.Step, task.StepImages)
	}

	tk.Step = task.StepQuestions
	tk.Facts["<Picture 1>"] = task.Facts{Label: "<Picture 1>", Summary: "a woman"}
	view = (&Server{}).taskView(tk)
	if view.Step != task.StepQuestions {
		t.Errorf("step = %q after vision analysis, want %q", view.Step, task.StepQuestions)
	}
}
