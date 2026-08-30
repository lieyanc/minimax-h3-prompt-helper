package promptgen

import (
	"strings"
	"testing"

	"h3helper/internal/llm"
	"h3helper/internal/task"
	"h3helper/internal/validate"
)

func writerTask() *task.Task {
	return &task.Task{
		Constraints: task.Constraints{
			Mode: "Ref2VA", Duration: 5.17, Frames: 124, FPS: 24, MaxShots: 3,
			PictureLabels: []string{"<Picture 1>", "<Picture 2>"},
		},
		Answers: map[string]string{"brief": "让图一的人物走进图二的车站"},
		Asked:   []task.Question{{Slot: "brief", Role: task.RoleBrief, Title: "意图", EnLabel: "Intent"}},
	}
}

func TestBuildMultimodalInjectsOriginalSkillAndBothImages(t *testing.T) {
	msgs := BuildMultimodal(writerTask(), []ReferenceImage{
		{Label: "<Picture 1>", DataURL: "data:image/png;base64,ONE"},
		{Label: "<Picture 2>", DataURL: "data:image/png;base64,TWO"},
	})
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want system + multimodal user", len(msgs))
	}
	system := msgs[0].Parts[0].Text
	for _, want := range []string{"BEGIN ORIGINAL SKILL.md", "# H3 Prompt Writing", "# Video Prompt Writing Guide", "# Full-Reference Mode Rewrite Output Format Guide"} {
		if !strings.Contains(system, want) {
			t.Errorf("writer system context is missing %q", want)
		}
	}

	user := msgs[1]
	var labels, images int
	for _, part := range user.Parts {
		if strings.Contains(part.Text, "Original reference image <Picture") {
			labels++
		}
		if part.ImageURL != "" {
			images++
		}
	}
	if labels != 2 || images != 2 {
		t.Fatalf("multimodal parts contain %d labels and %d images, want both pictures", labels, images)
	}
}

func TestRepairPreservesMultimodalBase(t *testing.T) {
	base := BuildMultimodal(writerTask(), []ReferenceImage{
		{Label: "<Picture 1>", DataURL: "data:image/png;base64,ONE"},
		{Label: "<Picture 2>", DataURL: "data:image/png;base64,TWO"},
	})
	msgs := Repair(base, "bad prompt", []validate.Finding{{Severity: validate.SeverityError, Message: "bad"}})
	images := 0
	for _, msg := range msgs {
		for _, part := range msg.Parts {
			if part.ImageURL != "" {
				images++
			}
		}
	}
	if images != 2 {
		t.Fatalf("repair retained %d images, want 2", images)
	}
}

func TestBuildRevisionKeepsPriorRequestsAndCurrentPrompt(t *testing.T) {
	tk := writerTask()
	tk.Prompt = "second prompt"
	tk.Revisions = []task.Revision{{
		Request: "第一次修改", PreviousPrompt: "first prompt", Prompt: "second prompt",
	}}
	msgs := BuildRevision(tk, nil, "撤回刚才的改动")
	joined := ""
	for _, msg := range msgs {
		for _, part := range msg.Parts {
			joined += part.Text + "\n"
		}
	}
	for _, want := range []string{"first prompt", "第一次修改", "second prompt", "撤回刚才的改动"} {
		if !strings.Contains(joined, want) {
			t.Errorf("revision conversation is missing %q", want)
		}
	}
	// "撤回刚才的改动" only means anything if the two prompts are in the order
	// they were actually written in.
	assertOrder(t, msgs, "first prompt", "第一次修改", "second prompt", "撤回刚才的改动")
}

// A task written before PreviousPrompt was recorded cannot say what the prompt
// looked like before its first revision: the attempt list holds the repair
// drafts of every round, so nothing in it stands in for that moment. The replay
// has to start at the first request rather than open with a later prompt.
func TestBuildRevisionWithoutARecordedPreviousPromptStartsAtTheFirstRequest(t *testing.T) {
	tk := writerTask()
	tk.Prompt = "second prompt"
	tk.Attempts = []task.Attempt{
		{Index: 1, Prompt: "first draft"},
		{Index: 2, Prompt: "first prompt"},
		{Index: 3, Prompt: "second prompt"},
	}
	tk.Revisions = []task.Revision{{Request: "第一次修改", Prompt: "second prompt"}}

	msgs := BuildRevision(tk, nil, "撤回刚才的改动")
	assertOrder(t, msgs, "第一次修改", "second prompt", "撤回刚才的改动")
	// The point of the fix: nothing may claim to be the prompt as it stood
	// before the first request. Opening with a later version tells the model
	// the history ran the other way round.
	all := turns(msgs)
	if at := firstTurnContaining(all, "second prompt"); at < firstTurnContaining(all, "第一次修改") {
		t.Errorf("turn %d presents the current prompt as the pre-revision one: %q", at, all[at])
	}
	for _, text := range all {
		if strings.Contains(text, "first draft") || strings.Contains(text, "first prompt") {
			t.Errorf("a repair draft leaked into the replayed conversation: %q", text)
		}
	}
}

func turns(msgs []llm.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		text := ""
		for _, part := range msg.Parts {
			text += part.Text
		}
		out = append(out, text)
	}
	return out
}

// firstTurnContaining returns len(all) when the excerpt is absent, so a missing
// turn never reads as "came first".
func firstTurnContaining(all []string, excerpt string) int {
	for i, text := range all {
		if strings.Contains(text, excerpt) {
			return i
		}
	}
	return len(all)
}

// assertOrder checks that each excerpt first appears in a later turn than the
// one before it.
func assertOrder(t *testing.T, msgs []llm.Message, excerpts ...string) {
	t.Helper()
	all := turns(msgs)
	at := -1
	for _, want := range excerpts {
		found := -1
		for i := at + 1; i < len(all); i++ {
			if strings.Contains(all[i], want) {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("%q does not appear after turn %d: %v", want, at, all)
		}
		at = found
	}
}
