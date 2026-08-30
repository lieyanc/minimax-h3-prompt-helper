package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"h3helper/internal/task"
)

// writeStub creates an empty file so the next upload has to pick a new name.
func writeStub(dir, name string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
}

func wiredTask() *task.Task {
	return &task.Task{
		ID: "t1",
		Constraints: task.Constraints{
			Mode:          "Ref2VA",
			PictureLabels: []string{"<Picture 1>", "<Picture 2>"},
		},
		Images: []task.Image{
			{Label: "<Picture 1>", Slot: "image1", Source: "a.png", Origin: task.OriginWorkflow, Wired: true},
			{Label: "<Picture 2>", Slot: "image2", Source: "b.png", Origin: task.OriginWorkflow, Wired: true},
		},
		Answers: map[string]string{
			"image_role:<Picture 1>": "subject_appearance",
			"image_role:<Picture 2>": "environment",
			"brief":                  "她走上站台",
		},
		Facts: map[string]task.Facts{
			"<Picture 1>": {Label: "<Picture 1>", Summary: "woman"},
			"<Picture 2>": {Label: "<Picture 2>", Summary: "platform"},
		},
		Asked: []task.Question{
			{Slot: "image_role:<Picture 1>", Label: "<Picture 1>", Title: "<Picture 1> 是谁？"},
			{Slot: "image_role:<Picture 2>", Label: "<Picture 2>", Title: "<Picture 2> 是哪里？"},
			{Slot: "brief", Title: "要发生什么？"},
		},
	}
}

func TestAttachUploadReplacesAWiredSlot(t *testing.T) {
	tk := wiredTask()
	attachUpload(tk, "<Picture 1>", "new.png")

	img := tk.Images[0]
	if img.Origin != task.OriginUpload || img.File != "new.png" {
		t.Fatalf("image = %+v, want the upload attached", img)
	}
	if !img.Wired {
		t.Error("replacing what a wired slot loads keeps the slot wired")
	}
	if img.Replaces != "a.png" {
		t.Errorf("replaces = %q, want the workflow file kept for restore", img.Replaces)
	}
	if _, ok := tk.Facts["<Picture 1>"]; ok {
		t.Error("the facts of the old picture must not survive a swap")
	}
	if _, ok := tk.Answers["image_role:<Picture 1>"]; ok {
		t.Error("what the user decided about the old picture must not survive a swap")
	}
	if tk.Answers["image_role:<Picture 2>"] == "" {
		t.Error("swapping one picture must not touch the answers about another")
	}
	if len(tk.Constraints.PictureLabels) != 2 {
		t.Errorf("labels = %v, want them unchanged", tk.Constraints.PictureLabels)
	}
}

func TestSwappingAPictureAlsoDropsAgentSlotAnswers(t *testing.T) {
	tk := wiredTask()
	// The agent names its own slots, so the label only appears on the question.
	tk.Asked = append(tk.Asked, task.Question{
		Slot: "picture_1_retention", Label: "<Picture 1>", Title: "保留多少？",
	})
	tk.Answers["picture_1_retention"] = "fully_preserved"

	attachUpload(tk, "<Picture 1>", "new.png")

	if _, ok := tk.Answers["picture_1_retention"]; ok {
		t.Error("an answer about the replaced picture survived under an invented slot key")
	}
	for _, q := range tk.Asked {
		if q.Label == "<Picture 1>" {
			t.Errorf("question %q still stands for a picture that was replaced", q.Slot)
		}
	}
}

func TestAttachUploadAddsALabelBeyondTheWorkflow(t *testing.T) {
	tk := wiredTask()
	attachUpload(tk, "", "extra.png")

	if len(tk.Images) != 3 {
		t.Fatalf("got %d images, want the upload appended", len(tk.Images))
	}
	added := tk.Images[2]
	if added.Label != "<Picture 3>" {
		t.Errorf("label = %q, want the next contiguous label", added.Label)
	}
	if added.Wired {
		t.Error("an image the workflow does not feed is not wired")
	}
	want := []string{"<Picture 1>", "<Picture 2>", "<Picture 3>"}
	if strings.Join(tk.Constraints.PictureLabels, ",") != strings.Join(want, ",") {
		t.Errorf("labels = %v, want %v", tk.Constraints.PictureLabels, want)
	}
}

func TestRemovingAnImageRenumbersAndCarriesTheAnswers(t *testing.T) {
	tk := wiredTask()
	attachUpload(tk, "", "extra.png")
	tk.Answers["image_role:<Picture 3>"] = "style"
	tk.Asked = append(tk.Asked, task.Question{
		Slot: "image_role:<Picture 3>", Label: "<Picture 3>", Title: "<Picture 3> 提供什么？",
	})

	// Drop the middle picture, the way deleting an upload would.
	dropLabelData(tk, "<Picture 2>")
	tk.Images = append(tk.Images[:1], tk.Images[2:]...)
	relabel(tk)

	if got := tk.Images[1].Label; got != "<Picture 2>" {
		t.Fatalf("the third picture is now %q, want <Picture 2>", got)
	}
	if tk.Answers["image_role:<Picture 2>"] != "style" {
		t.Errorf("answers = %v, want the renumbered key to carry the style answer", tk.Answers)
	}
	if _, ok := tk.Answers["image_role:<Picture 3>"]; ok {
		t.Error("the old key survived the renumbering")
	}
	if _, ok := tk.Facts["<Picture 2>"]; ok {
		t.Error("the removed picture's facts were carried onto its successor")
	}
	for _, q := range tk.Asked {
		if strings.Contains(q.Slot, "<Picture 3>") || q.Label == "<Picture 3>" {
			t.Errorf("question %q still points at a label that no longer exists", q.Slot)
		}
	}
	if strings.Contains(strings.Join(tk.Constraints.PictureLabels, ","), "<Picture 3>") {
		t.Errorf("labels = %v, want them contiguous", tk.Constraints.PictureLabels)
	}
}

func TestComfyNoticesSayWhatIsStillMissing(t *testing.T) {
	tk := wiredTask()
	attachUpload(tk, "<Picture 1>", "new.png")
	attachUpload(tk, "", "extra.png")
	tk.Images[1].Missing = true

	notices := comfyNotices(tk)
	if len(notices) != 3 {
		t.Fatalf("got %d notices, want one per unmatched picture: %+v", len(notices), notices)
	}
	kinds := map[string]bool{}
	for _, n := range notices {
		kinds[n.Kind] = true
		if n.Text == "" {
			t.Errorf("notice %+v has no text", n)
		}
	}
	for _, want := range []string{"replace", "missing", "add"} {
		if !kinds[want] {
			t.Errorf("missing a %q notice, got %v", want, kinds)
		}
	}
}

func TestUniqueNameNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	first := uniqueName(dir, "shot.png")
	if first != "shot.png" {
		t.Fatalf("first upload = %q, want the original name", first)
	}
	if err := writeStub(dir, first); err != nil {
		t.Fatal(err)
	}
	if second := uniqueName(dir, "shot.png"); second != "shot-2.png" {
		t.Errorf("second upload = %q, want shot-2.png", second)
	}
	if traversal := uniqueName(dir, "../../etc/passwd"); strings.Contains(traversal, "/") {
		t.Errorf("upload name %q escaped the directory", traversal)
	}
}

func TestBlockersOnlyCountWhatWasAsked(t *testing.T) {
	tk := wiredTask()
	tk.Asked = append(tk.Asked, task.Question{Slot: "brief", Role: task.RoleBrief, Required: true})
	tk.Answers["brief"] = "她走上站台"
	tk.Pending = []task.Question{{Slot: "camera", Title: "镜头怎么动？", Required: true}}

	got := blockers(tk)
	if len(got) != 1 || got[0] != "镜头怎么动？" {
		t.Fatalf("blockers = %v, want the unanswered pending question", got)
	}

	tk.Answers["camera"] = "Push In"
	if got := blockers(tk); len(got) != 0 {
		t.Errorf("blockers = %v, want none once everything asked is answered", got)
	}
}

func TestAPageOfSkippedOptionalQuestionsClears(t *testing.T) {
	tk := wiredTask()
	tk.Pending = []task.Question{
		{Slot: "screen_text", Title: "画面上有字吗？", Required: false},
		{Slot: "music_desc", Title: "配乐怎么写？", Required: false},
	}

	// Handing the page in with nothing filled has to move the interview on,
	// otherwise the same page comes back forever.
	if left := stillRequired(tk, tk.Pending); len(left) != 0 {
		t.Errorf("page still holds %v after being handed in", left)
	}

	tk.Pending = append(tk.Pending, task.Question{
		Slot: "camera", Title: "镜头怎么动？", Required: true,
	})
	left := stillRequired(tk, tk.Pending)
	if len(left) != 1 || left[0].Slot != "camera" {
		t.Errorf("kept %v, want only the unanswered required question", left)
	}
}
