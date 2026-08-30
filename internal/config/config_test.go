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
	// The template ships an endpoint anyone can reach, with no key in it.
	ep, err := m.VisionEndpoint()
	if err != nil {
		t.Fatalf("the default roles must resolve: %v", err)
	}
	if ep.BaseURL != "https://api.openai.com/v1" || ep.APIKey != "" {
		t.Errorf("default vision endpoint = %+v, want the OpenAI address and no key", ep)
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
	// A hand-written file: one endpoint, one model, nothing else.
	body := `{
	  "providers": [{"id": "p", "name": "P", "baseURL": "https://api.example.com/v1"}],
	  "models": [{"id": "m", "providerId": "p", "model": "my-vlm"}],
	  "token": "secret"
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
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
	if cfg.Models[0].Model != "my-vlm" || cfg.Token != "secret" {
		t.Errorf("existing values were lost: %+v", cfg)
	}
	if cfg.Listen != "0.0.0.0:8199" || cfg.Models[0].MaxTokens != defaultMaxTokens || !cfg.StrictEnglish {
		t.Errorf("defaults were not merged in: %+v", cfg)
	}
	// Both roles have to land on the only model there is.
	if cfg.Vision.ModelID != "m" || cfg.Writer.ModelID != "m" {
		t.Errorf("roles = %+v / %+v, want both pointing at the single model", cfg.Vision, cfg.Writer)
	}

	filled := m.Filled()
	// A partially present object reports its missing leaves; an object that is
	// absent entirely is reported once, by its own name.
	for _, want := range []string{"listen", "vision", "writer", "models[0].maxTokens"} {
		if !slices.Contains(filled, want) {
			t.Errorf("Filled() = %v, want it to mention %q", filled, want)
		}
	}
	if slices.Contains(filled, "models") {
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

func TestLoadMigratesTheLegacyPerRoleEndpoints(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// The shape before providers existed: each role carried its own endpoint.
	body := `{
	  "vision": {"baseURL": "https://api.openai.com/v1", "apiKey": "sk-vision", "model": "gpt-4o",
	             "maxTokens": 4096, "temperature": 0.2, "imageMaxEdge": 900},
	  "writer": {"sameAsVision": false, "baseURL": "http://127.0.0.1:11434/v1", "apiKey": "",
	             "model": "qwen3:8b", "maxTokens": 8192, "temperature": 0.7}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := Load(path, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg := m.Snapshot()

	if len(cfg.Providers) != 2 {
		t.Fatalf("providers = %+v, want one per distinct endpoint", cfg.Providers)
	}
	if cfg.Providers[0].Name != "OpenAI" || cfg.Providers[0].APIKey != "sk-vision" {
		t.Errorf("vision provider = %+v, want the preset name and the key carried over", cfg.Providers[0])
	}
	if cfg.Vision.ImageMaxEdge != 900 {
		t.Errorf("imageMaxEdge = %d, want the file's value kept", cfg.Vision.ImageMaxEdge)
	}

	vision, err := m.VisionEndpoint()
	if err != nil {
		t.Fatalf("VisionEndpoint: %v", err)
	}
	if vision.Model != "gpt-4o" || vision.MaxTokens != 4096 || vision.Temperature != 0.2 {
		t.Errorf("vision = %+v, want the old vision settings", vision)
	}
	writer, err := m.WriterEndpoint()
	if err != nil {
		t.Fatalf("WriterEndpoint: %v", err)
	}
	if writer.Model != "qwen3:8b" || writer.BaseURL != "http://127.0.0.1:11434/v1" || writer.MaxTokens != 8192 {
		t.Errorf("writer = %+v, want the old writer settings and its own endpoint", writer)
	}

	// Migrating once is enough: the rewritten file has providers, so a second
	// start reads it as-is.
	again, err := Load(path, dir)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got := again.Snapshot(); len(got.Providers) != 2 || len(got.Models) != 2 {
		t.Errorf("second start = %d providers / %d models, want the migrated file read back", len(got.Providers), len(got.Models))
	}
}

func TestLegacyWriterFollowsVisionWhenItShared(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
	  "vision": {"baseURL": "https://api.example.com/v1", "apiKey": "sk-one", "model": "m",
	             "maxTokens": 4096, "temperature": 0.2},
	  "writer": {"sameAsVision": true, "baseURL": "", "apiKey": "", "model": "",
	             "maxTokens": 8192, "temperature": 0.7}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := Load(path, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := m.Snapshot().Providers; len(got) != 1 {
		t.Fatalf("providers = %+v, want the shared endpoint stored once", got)
	}
	writer, err := m.WriterEndpoint()
	if err != nil {
		t.Fatalf("WriterEndpoint: %v", err)
	}
	// An empty writer model used to mean "same as vision"; the two sampling
	// profiles still have to survive as separate entries.
	if writer.Model != "m" || writer.APIKey != "sk-one" {
		t.Errorf("writer = %+v, want it to inherit the vision model and key", writer)
	}
	if writer.MaxTokens != 8192 || writer.Temperature != 0.7 {
		t.Errorf("writer = %+v, want the writer's own sampling kept", writer)
	}
}

func TestLoadRepairsUnusableValuesButKeepsMeaningfulZeros(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
	  "listen": "",
	  "comfyuiRoot": "/opt/ComfyUI",
	  "workflowDirs": [],
	  "providers": [
	    {"id": "", "name": "", "baseURL": "https://api.example.com/v1/chat/completions ", "apiKey": ""},
	    {"id": "", "name": "", "baseURL": "https://other.example.com/v1", "apiKey": ""}
	  ],
	  "models": [
	    {"id": "v", "providerId": "gone", "model": "m", "displayName": "", "maxTokens": 0, "temperature": 0},
	    {"id": "w", "providerId": "", "model": "m2", "displayName": "写", "maxTokens": -1, "temperature": 0.7}
	  ],
	  "vision": {"modelId": "nope", "imageMaxEdge": 0},
	  "writer": {"modelId": "w"},
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
	if cfg.Providers[0].BaseURL != "https://api.example.com/v1" {
		t.Errorf("baseURL = %q, want the /chat/completions suffix trimmed", cfg.Providers[0].BaseURL)
	}
	if cfg.Providers[0].ID == "" || cfg.Providers[0].ID == cfg.Providers[1].ID {
		t.Errorf("provider ids = %q / %q, want both filled in and distinct", cfg.Providers[0].ID, cfg.Providers[1].ID)
	}
	if cfg.Providers[0].Name == "" {
		t.Error("a provider without a name cannot be shown in the picker")
	}
	// A model pointing at a provider that is not there falls back rather than
	// failing at request time.
	if cfg.Models[0].ProviderID != cfg.Providers[0].ID || cfg.Models[1].ProviderID != cfg.Providers[0].ID {
		t.Errorf("models = %+v, want dangling providerIds re-pointed", cfg.Models)
	}
	if cfg.Models[0].MaxTokens != defaultMaxTokens || cfg.Models[1].MaxTokens != defaultMaxTokens {
		t.Errorf("non-positive maxTokens should fall back: %+v", cfg.Models)
	}
	// An unknown role target falls back; a valid one is left alone.
	if cfg.Vision.ModelID != "v" || cfg.Writer.ModelID != "w" {
		t.Errorf("roles = %q / %q, want the unknown one repaired and the valid one kept", cfg.Vision.ModelID, cfg.Writer.ModelID)
	}
	// These zeros mean something and must survive.
	if cfg.MaxRepairRounds != 0 {
		t.Errorf("maxRepairRounds = %d, want 0 kept as 'no automatic repair'", cfg.MaxRepairRounds)
	}
	if cfg.Vision.ImageMaxEdge != 0 {
		t.Errorf("imageMaxEdge = %d, want 0 kept as 'send the original'", cfg.Vision.ImageMaxEdge)
	}
	if cfg.Models[0].Temperature != 0 {
		t.Errorf("temperature = %v, want 0 kept", cfg.Models[0].Temperature)
	}
	if cfg.StrictEnglish {
		t.Error("strictEnglish false was explicitly set and must not be overwritten")
	}
}

func TestResolveReportsWhatTheSettingsPageMustFix(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(filepath.Join(dir, "config.json"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Resolve("nope"); err == nil {
		t.Error("an unknown model id must be an error, not a silent fallback")
	}
	ep, err := m.Resolve("writer")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.ProviderName != "OpenAI" || ep.MaxTokens != 8192 {
		t.Errorf("Resolve(writer) = %+v, want the provider joined in", ep)
	}
}

func TestUpdateNormalisesWhatTheAPIWrites(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(filepath.Join(dir, "config.json"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Update(func(c *Config) {
		c.Providers[0].BaseURL = "  https://api.example.com/v1/chat/completions/  "
		c.Models[0].ReasoningEffort = " HIGH "
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := m.Snapshot().Providers[0].BaseURL; got != "https://api.example.com/v1" {
		t.Errorf("baseURL = %q, want it trimmed on save too", got)
	}
	if got := m.Snapshot().Models[0].ReasoningEffort; got != "high" {
		t.Errorf("reasoningEffort = %q, want it normalised on save", got)
	}
	ep, err := m.Resolve(m.Snapshot().Models[0].ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ep.ReasoningEffort != "high" {
		t.Errorf("resolved reasoning effort = %q, want %q", ep.ReasoningEffort, "high")
	}
}

func TestUpdateKeepsRolesPointingAtSomethingThatExists(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(filepath.Join(dir, "config.json"), dir)
	if err != nil {
		t.Fatal(err)
	}
	// Deleting the model a role used is the ordinary settings-page mistake.
	if err := m.Update(func(c *Config) {
		c.Models = c.Models[:1]
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	cfg := m.Snapshot()
	if cfg.Writer.ModelID != cfg.Models[0].ID {
		t.Errorf("writer.modelId = %q, want it moved to the surviving model", cfg.Writer.ModelID)
	}
	if _, err := m.WriterEndpoint(); err != nil {
		t.Errorf("WriterEndpoint after the delete: %v", err)
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
