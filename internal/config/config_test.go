package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadWritesTheDefaultsWhenTheFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	m, err := Load(path, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !m.Created() {
		t.Error("Created() should report the first run")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the config file should exist after Load: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("written config is not valid JSON: %v", err)
	}
	// Every key of the compiled-in shape must be present, so the file can be
	// hand-edited without knowing which keys exist.
	if missing := missingKeys(asMap(Default(dir)), onDisk, ""); len(missing) > 0 {
		t.Errorf("released config is missing %v", missing)
	}
	if got := m.Snapshot().ComfyUIRoot; got != filepath.Join(dir, "ComfyUI") {
		t.Errorf("comfyuiRoot = %q, want it derived from home", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config holds API keys, mode = %v, want 0600", perm)
	}
}

func TestLoadFillsInMissingKeysAndRewritesTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// A file from an older version: only the two keys the user cared about.
	if err := os.WriteFile(path, []byte(`{"vision":{"model":"my-vlm"},"token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := Load(path, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Created() {
		t.Error("an existing file must not be reported as created")
	}

	cfg := m.Snapshot()
	if cfg.Vision.Model != "my-vlm" || cfg.Token != "secret" {
		t.Errorf("existing values were lost: %+v", cfg)
	}
	if cfg.Listen != "0.0.0.0:8199" || cfg.Vision.MaxTokens != 4096 || !cfg.StrictEnglish {
		t.Errorf("defaults were not merged in: %+v", cfg)
	}

	filled := m.Filled()
	// A partially present object reports its missing leaves; an object that is
	// absent entirely is reported once, by its own name.
	for _, want := range []string{"listen", "vision.maxTokens", "writer"} {
		if !slices.Contains(filled, want) {
			t.Errorf("Filled() = %v, want it to mention %q", filled, want)
		}
	}
	if slices.Contains(filled, "vision.model") {
		t.Errorf("Filled() = %v, must not mention a key the file already had", filled)
	}

	// The completed file is on disk, so the next start has nothing to fill.
	again, err := Load(path, dir)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got := again.Filled(); len(got) > 0 {
		t.Errorf("second start still filled %v; the rewrite did not take", got)
	}
}

func TestLoadRepairsUnusableValuesButKeepsMeaningfulZeros(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
	  "listen": "",
	  "comfyuiRoot": "/opt/ComfyUI",
	  "workflowDirs": [],
	  "vision": {"baseURL": "https://api.example.com/v1/chat/completions ", "apiKey": "", "model": "m",
	             "maxTokens": 0, "temperature": 0, "imageMaxEdge": 0},
	  "writer": {"sameAsVision": true, "baseURL": "", "apiKey": "", "model": "",
	             "maxTokens": -1, "temperature": 0.7},
	  "strictEnglish": false,
	  "maxRepairRounds": 0
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := Load(path, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg := m.Snapshot()

	if cfg.Listen != "0.0.0.0:8199" {
		t.Errorf("empty listen = %q, want the default", cfg.Listen)
	}
	if cfg.Vision.BaseURL != "https://api.example.com/v1" {
		t.Errorf("baseURL = %q, want the /chat/completions suffix trimmed", cfg.Vision.BaseURL)
	}
	if cfg.Vision.MaxTokens != 4096 || cfg.Writer.MaxTokens != 8192 {
		t.Errorf("non-positive maxTokens should fall back: %+v", cfg)
	}
	// These zeros mean something and must survive.
	if cfg.MaxRepairRounds != 0 {
		t.Errorf("maxRepairRounds = %d, want 0 kept as 'no automatic repair'", cfg.MaxRepairRounds)
	}
	if cfg.Vision.ImageMaxEdge != 0 {
		t.Errorf("imageMaxEdge = %d, want 0 kept as 'send the original'", cfg.Vision.ImageMaxEdge)
	}
	if cfg.Vision.Temperature != 0 {
		t.Errorf("temperature = %v, want 0 kept", cfg.Vision.Temperature)
	}
	if cfg.StrictEnglish {
		t.Error("strictEnglish false was explicitly set and must not be overwritten")
	}
}

func TestUpdateNormalisesWhatTheAPIWrites(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(filepath.Join(dir, "config.json"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Update(func(c *Config) {
		c.Vision.BaseURL = "  https://api.example.com/v1/chat/completions/  "
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := m.Snapshot().Vision.BaseURL; got != "https://api.example.com/v1" {
		t.Errorf("baseURL = %q, want it trimmed on save too", got)
	}
}

func TestTrimBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                                     "",
		"https://api.openai.com/v1":            "https://api.openai.com/v1",
		"https://api.openai.com/v1/":           "https://api.openai.com/v1",
		" https://api.openai.com/v1 ":          "https://api.openai.com/v1",
		"http://x/v1/chat/completions":         "http://x/v1",
		"http://x/v1/chat/completions/":        "http://x/v1",
		"https://open.bigmodel.cn/api/paas/v4": "https://open.bigmodel.cn/api/paas/v4",
	}
	for in, want := range cases {
		if got := TrimBaseURL(in); got != want {
			t.Errorf("TrimBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPresetsAreUsable(t *testing.T) {
	presets := Presets()
	if len(presets) == 0 {
		t.Fatal("the settings page expects at least one preset")
	}
	seen := map[string]bool{}
	for _, p := range presets {
		if p.Name == "" {
			t.Error("a preset without a name cannot be shown")
		}
		if seen[p.Name] {
			t.Errorf("duplicate preset name %q; the select uses it as the key", p.Name)
		}
		seen[p.Name] = true
		if got := TrimBaseURL(p.BaseURL); got != p.BaseURL {
			t.Errorf("preset %q baseURL %q is not already normalised (want %q)", p.Name, p.BaseURL, got)
		}
	}
}
