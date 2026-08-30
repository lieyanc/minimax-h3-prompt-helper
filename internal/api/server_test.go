package api

import (
	"strings"
	"testing"

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
