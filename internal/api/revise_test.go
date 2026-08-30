package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"h3helper/internal/comfy"
	"h3helper/internal/config"
	"h3helper/internal/store"
	"h3helper/internal/task"
)

func TestReviseSendsBothOriginalImagesAndStoresTheTurn(t *testing.T) {
	var upstreamBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"revised complete prompt\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()

	root := t.TempDir()
	cfg, err := config.Load(filepath.Join(root, "config.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *config.Config) {
		c.Providers = []config.Provider{{ID: "test", Name: "test", BaseURL: upstream.URL}}
		c.Models = []config.Model{{ID: "writer-test", ProviderID: "test", Model: "writer-test", MaxTokens: 100}}
		c.Writer = config.WriterConfig{ModelID: "writer-test", ImageMaxEdge: 0}
		c.MaxRepairRounds = 0
	}); err != nil {
		t.Fatal(err)
	}

	st, err := store.New(filepath.Join(root, "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	uploads := filepath.Join(root, "uploads")
	taskID := "revise-two-images"
	uploadDir := filepath.Join(uploads, taskID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.png", "two.png"} {
		if err := os.WriteFile(filepath.Join(uploadDir, name), []byte("image-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tk := &task.Task{
		ID: taskID, Prompt: "original prompt", Answers: map[string]string{},
		Constraints: task.Constraints{Mode: "Ref2VA", PictureLabels: []string{"<Picture 1>", "<Picture 2>"}},
		Images: []task.Image{
			{Label: "<Picture 1>", Origin: task.OriginUpload, File: "one.png"},
			{Label: "<Picture 2>", Origin: task.OriginUpload, File: "two.png"},
		},
		Facts: map[string]task.Facts{
			"<Picture 1>": {Label: "<Picture 1>", Summary: "first"},
			"<Picture 2>": {Label: "<Picture 2>", Summary: "second"},
		},
	}
	if err := st.Create(tk); err != nil {
		t.Fatal(err)
	}

	handler := New(cfg, comfy.NewLibrary(nil, ""), st, uploads, nil, "test").Handler()
	body := bytes.NewBufferString(`{"message":"把动作改慢，但保留两张图的内容"}`)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/tasks/"+taskID+"/revise", body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("revise status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	requestJSON := string(upstreamBody)
	if got := strings.Count(requestJSON, `"type":"image_url"`); got != 2 {
		t.Fatalf("writer request has %d image parts, want both originals: %s", got, requestJSON)
	}
	for _, want := range []string{"BEGIN ORIGINAL SKILL.md", `\u003cPicture 1\u003e`, `\u003cPicture 2\u003e`, "把动作改慢"} {
		if !strings.Contains(requestJSON, want) {
			t.Errorf("writer request is missing %q", want)
		}
	}

	updated, err := st.Get(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Prompt != "revised complete prompt" || len(updated.Revisions) != 1 {
		t.Fatalf("updated task = prompt %q, revisions %+v", updated.Prompt, updated.Revisions)
	}
	if updated.Revisions[0].PreviousPrompt != "original prompt" {
		t.Errorf("previous prompt = %q", updated.Revisions[0].PreviousPrompt)
	}
}
