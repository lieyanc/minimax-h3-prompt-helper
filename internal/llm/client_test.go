package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// captureServer records the last request body and replies with a canned answer.
func captureServer(t *testing.T, reply string) (*httptest.Server, *map[string]any) {
	t.Helper()
	captured := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

func TestTextMessageSerialisesAsPlainString(t *testing.T) {
	srv, captured := captureServer(t, "ok")
	client := New(srv.URL, "key", "model-x", 100, 0.2, "")

	got, err := client.Complete(context.Background(), []Message{Text("user", "hello")}, Options{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "ok" {
		t.Errorf("Complete = %q, want %q", got, "ok")
	}

	msgs := (*captured)["messages"].([]any)
	content := msgs[0].(map[string]any)["content"]
	if _, isString := content.(string); !isString {
		t.Errorf("single text part should serialise as a bare string, got %T", content)
	}
}

func TestImageMessageSerialisesAsParts(t *testing.T) {
	srv, captured := captureServer(t, "{}")
	client := New(srv.URL, "", "model-x", 100, 0.2, "")

	msg := Message{Role: "user", Parts: []Part{
		{Text: "look"},
		{ImageURL: "data:image/png;base64,AAAA"},
	}}
	if _, err := client.Complete(context.Background(), []Message{msg}, Options{JSONObject: true}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	msgs := (*captured)["messages"].([]any)
	parts, ok := msgs[0].(map[string]any)["content"].([]any)
	if !ok {
		t.Fatalf("multimodal content should serialise as an array")
	}
	if len(parts) != 2 {
		t.Fatalf("got %d parts, want 2", len(parts))
	}
	if parts[0].(map[string]any)["type"] != "text" {
		t.Errorf("first part should be text")
	}
	image := parts[1].(map[string]any)
	if image["type"] != "image_url" {
		t.Errorf("second part should be image_url")
	}
	url := image["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("image url = %q", url)
	}
	if rf, ok := (*captured)["response_format"].(map[string]any); !ok || rf["type"] != "json_object" {
		t.Errorf("JSONObject option should set response_format")
	}
}

func TestStreamCollectsContentAndReasoningDeltas(t *testing.T) {
	captured := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"First\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning\":\" then\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"thinking\":{\"text\":\" decide\"}}}]}\n\n")
		flusher.Flush()
		for _, content := range []string{"Hello", ", ", "world"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", content)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	client := New(srv.URL, "", "model-x", 100, 0.2, "")
	var content, reasoning strings.Builder
	full, err := client.Stream(context.Background(), []Message{Text("user", "hi")}, Options{},
		func(d Delta) {
			content.WriteString(d.Content)
			reasoning.WriteString(d.Reasoning)
		})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if full != "Hello, world" {
		t.Errorf("Stream = %q, want %q", full, "Hello, world")
	}
	if content.String() != full {
		t.Errorf("content deltas = %q, want %q", content.String(), full)
	}
	if reasoning.String() != "First then decide" {
		t.Errorf("reasoning deltas = %q, want all supported field shapes", reasoning.String())
	}
	if stream, ok := captured["stream"].(bool); !ok || !stream {
		t.Errorf("stream = %#v, want true", captured["stream"])
	}
}

func TestStreamDeliversADeltaBeforeTheResponseCompletes(t *testing.T) {
	release := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"live\"}}]}\n\n")
		flusher.Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := make(chan Delta, 1)
	done := make(chan error, 1)
	client := New(srv.URL, "", "model-x", 100, 0.2, "")
	go func() {
		_, err := client.Stream(ctx, []Message{Text("user", "hi")}, Options{}, func(d Delta) {
			seen <- d
		})
		done <- err
	}()

	select {
	case delta := <-seen:
		if delta.Reasoning != "live" {
			t.Errorf("first live delta = %+v, want the reasoning chunk", delta)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the flushed delta was buffered until the response completed")
	}
	release <- struct{}{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish after the server was released")
	}
}

func TestReasoningEffortIsSentAndTemperatureIsOmitted(t *testing.T) {
	srv, captured := captureServer(t, "ok")
	client := New(srv.URL, "", "model-x", 100, 0.7, " HIGH ")

	if _, err := client.Complete(context.Background(), []Message{Text("user", "hi")}, Options{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := (*captured)["reasoning_effort"]; got != "high" {
		t.Errorf("reasoning_effort = %#v, want %q", got, "high")
	}
	if _, ok := (*captured)["temperature"]; ok {
		t.Error("temperature must be omitted for a reasoning model")
	}
	if got := (*captured)["max_completion_tokens"]; got != float64(100) {
		t.Errorf("max_completion_tokens = %#v, want 100", got)
	}
	if _, ok := (*captured)["max_tokens"]; ok {
		t.Error("max_tokens must be replaced for a reasoning model")
	}
}

func TestErrorsCarryTheUpstreamBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "bad", "model-x", 100, 0.2, "")
	_, err := client.Complete(context.Background(), []Message{Text("user", "hi")}, Options{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error should surface the upstream message, got: %v", err)
	}
}

func TestMissingConfigIsReportedBeforeDialling(t *testing.T) {
	if _, err := New("", "", "m", 10, 0, "").Complete(context.Background(), nil, Options{}); err == nil {
		t.Error("expected an error when the base URL is empty")
	}
	if _, err := New("http://example.invalid", "", "", 10, 0, "").Complete(context.Background(), nil, Options{}); err == nil {
		t.Error("expected an error when the model is empty")
	}
}
