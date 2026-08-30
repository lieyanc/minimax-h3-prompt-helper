package comfy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// minimalWorkflow is a hand-written graph with one MiniMaxH3ImageToVideo node
// fed by a LoadImage on first_frame only, i.e. an I2VA setup.
const minimalWorkflow = `{
  "nodes": [
    {"id": 1, "type": "LoadImage", "mode": 0,
     "widgets_values": ["ref.png", "image"],
     "inputs": [{"name": "image", "type": "COMBO", "widget": {"name": "image"}},
                {"name": "upload", "type": "IMAGEUPLOAD", "widget": {"name": "upload"}}],
     "outputs": [{"name": "IMAGE", "type": "IMAGE", "links": [10]}]},
    {"id": 2, "type": "PrimitiveFloat", "mode": 0, "widgets_values": [5],
     "inputs": [{"name": "value", "type": "FLOAT", "widget": {"name": "value"}}],
     "outputs": [{"name": "FLOAT", "type": "FLOAT", "links": [11]}]},
    {"id": 3, "type": "ComfyMathExpression", "mode": 0,
     "widgets_values": ["max(5, round(a * 24)) + (5 - (max(5, round(a * 24)) % 17)) % 17"],
     "inputs": [{"name": "values.a", "type": "FLOAT", "link": 11},
                {"name": "expression", "type": "STRING", "widget": {"name": "expression"}}],
     "outputs": [{"name": "FLOAT", "type": "FLOAT"}, {"name": "INT", "type": "INT", "links": [12]}]},
    {"id": 4, "type": "MiniMaxH3ImageToVideo", "mode": 0,
     "widgets_values": ["existing prompt", 1344, 768, 99],
     "inputs": [{"name": "first_frame", "type": "IMAGE", "link": 10},
                {"name": "last_frame", "type": "IMAGE", "link": null},
                {"name": "prompt", "type": "STRING", "widget": {"name": "prompt"}},
                {"name": "width", "type": "INT", "widget": {"name": "width"}},
                {"name": "height", "type": "INT", "widget": {"name": "height"}},
                {"name": "length", "type": "INT", "link": 12, "widget": {"name": "length"}}],
     "outputs": [{"name": "positive", "type": "CONDITIONING"}, {"name": "LATENT", "type": "LATENT"}]}
  ],
  "links": [[10, 1, 0, 4, 0, "IMAGE"], [11, 2, 0, 3, 0, "FLOAT"], [12, 3, 1, 4, 5, "INT"]]
}`

func TestExtractVariantsResolvesLinkedLength(t *testing.T) {
	g, err := ParseGraph([]byte(minimalWorkflow))
	if err != nil {
		t.Fatalf("ParseGraph: %v", err)
	}

	variants := ExtractVariants(g, func(string) bool { return true })
	if len(variants) != 1 {
		t.Fatalf("got %d variants, want 1", len(variants))
	}
	v := variants[0]

	if v.Mode != ModeI2VA {
		t.Errorf("mode = %q, want %q (only first_frame is linked)", v.Mode, ModeI2VA)
	}
	if !v.Active {
		t.Error("a node with mode 0 should be reported as active")
	}
	// 124 comes from the upstream duration chain, not the stale widget value 99.
	if v.Frames != 124 {
		t.Errorf("frames = %d, want 124 resolved through the expression node", v.Frames)
	}
	if got := v.Duration; got < 5.16 || got > 5.17 {
		t.Errorf("duration = %v, want ~5.17", got)
	}
	if v.Width != 1344 || v.Height != 768 {
		t.Errorf("canvas = %dx%d, want 1344x768", v.Width, v.Height)
	}
	if !v.FramesOnGrid {
		t.Error("124 frames should sit on the 17k+5 grid")
	}
	if len(v.Images) != 2 {
		t.Fatalf("got %d image slots, want first_frame and last_frame", len(v.Images))
	}
	if !v.Images[0].Connected || v.Images[0].Label != "<Picture 1>" {
		t.Errorf("first_frame = %+v, want a connected <Picture 1>", v.Images[0])
	}
	if v.Images[0].Source != "ref.png" {
		t.Errorf("traced source = %q, want ref.png", v.Images[0].Source)
	}
	if v.Images[1].Connected {
		t.Error("last_frame is not linked and must not be reported as connected")
	}
	if v.Images[1].Label != "" {
		t.Errorf("unconnected slots must not get a label, got %q", v.Images[1].Label)
	}
	if v.PromptText != "existing prompt" {
		t.Errorf("promptText = %q", v.PromptText)
	}
	if v.ConnectedImages() != 1 {
		t.Errorf("ConnectedImages = %d, want 1", v.ConnectedImages())
	}
}

func TestBypassedNodesAreStillListedButMarkedInactive(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(minimalWorkflow), &doc); err != nil {
		t.Fatal(err)
	}
	nodes := doc["nodes"].([]any)
	nodes[3].(map[string]any)["mode"] = 4 // bypassed in the editor
	data, _ := json.Marshal(doc)

	g, err := ParseGraph(data)
	if err != nil {
		t.Fatalf("ParseGraph: %v", err)
	}
	variants := ExtractVariants(g, nil)
	if len(variants) != 1 {
		t.Fatalf("bypassed branches must still be offered, got %d variants", len(variants))
	}
	if variants[0].Active {
		t.Error("a bypassed node must be reported as inactive")
	}
}

func TestScanSkipsNonWorkflowFilesAndNeverReturnsNilVariants(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("h3.json", minimalWorkflow)
	write("plain.json", `{"nodes": [{"id": 1, "type": "KSampler", "mode": 0}], "links": []}`)
	write(".index.json", `{"nodes": []}`)
	write("notes.txt", "ignored")

	lib := NewLibrary([]string{dir}, dir)
	workflows, err := lib.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(workflows) != 2 {
		t.Fatalf("got %d workflows, want 2 (hidden and non-JSON files skipped)", len(workflows))
	}
	// Workflows with H3 nodes sort first.
	if len(workflows[0].Variants) == 0 {
		t.Error("the workflow containing an H3 node should sort first")
	}
	for _, wf := range workflows {
		if wf.Variants == nil {
			t.Errorf("%s has nil variants; JSON consumers index into this slice", wf.Name)
		}
	}
}
