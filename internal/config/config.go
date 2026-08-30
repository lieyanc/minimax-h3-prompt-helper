// Package config loads and persists the JSON configuration file.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Config is the application configuration. It is plain data; concurrency is
// handled by Manager.
type Config struct {
	// Listen is the HTTP bind address. LAN access needs 0.0.0.0.
	Listen string `json:"listen"`
	// Token, when non-empty, is required as ?token= or X-Auth-Token on /api routes.
	Token string `json:"token"`

	// ComfyUIRoot is the ComfyUI installation directory.
	ComfyUIRoot string `json:"comfyuiRoot"`
	// WorkflowDirs are scanned for workflow JSON files. Empty derives the
	// default location from ComfyUIRoot.
	WorkflowDirs []string `json:"workflowDirs"`

	Vision VisionConfig `json:"vision"`
	Writer WriterConfig `json:"writer"`

	// StrictEnglish turns CJK leakage outside <d> and quoted screen text into a
	// blocking error rather than a warning.
	StrictEnglish bool `json:"strictEnglish"`
	// MaxRepairRounds is how many times a failed validation is fed back to the
	// writing model.
	MaxRepairRounds int `json:"maxRepairRounds"`
}

// VisionConfig points at an OpenAI-compatible endpoint used for image understanding.
type VisionConfig struct {
	BaseURL     string  `json:"baseURL"`
	APIKey      string  `json:"apiKey"`
	Model       string  `json:"model"`
	MaxTokens   int     `json:"maxTokens"`
	Temperature float64 `json:"temperature"`
	// ImageMaxEdge downsizes images before base64 upload. 0 disables resizing.
	ImageMaxEdge int `json:"imageMaxEdge"`
}

// WriterConfig points at the model that composes the final H3 prompt. It may be
// the same endpoint as Vision; SameAsVision keeps the two in sync.
type WriterConfig struct {
	SameAsVision bool    `json:"sameAsVision"`
	BaseURL      string  `json:"baseURL"`
	APIKey       string  `json:"apiKey"`
	Model        string  `json:"model"`
	MaxTokens    int     `json:"maxTokens"`
	Temperature  float64 `json:"temperature"`
}

// Default returns the built-in configuration. It is the single source of
// truth: the file on disk is written from it on first run, and every key the
// file turns out to be missing is filled back in from here at startup.
func Default(home string) Config {
	return Config{
		Listen:      "0.0.0.0:8199",
		ComfyUIRoot: filepath.Join(home, "ComfyUI"),
		// Empty rather than nil: the released file should show an editable
		// list, not a null.
		WorkflowDirs: []string{},
		Vision: VisionConfig{
			BaseURL:      "https://api.openai.com/v1",
			MaxTokens:    4096,
			Temperature:  0.2,
			ImageMaxEdge: 1280,
		},
		Writer: WriterConfig{
			SameAsVision: true,
			MaxTokens:    8192,
			Temperature:  0.7,
		},
		StrictEnglish:   true,
		MaxRepairRounds: 2,
	}
}

// Preset is a built-in OpenAI-compatible endpoint offered in the settings page
// so the address does not have to be typed from memory. Presets are compiled
// in, never stored in the config file.
type Preset struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
	Note    string `json:"note,omitempty"`
}

// Presets lists the endpoints the settings page can fill in with one click.
// Model names are only a starting point; providers rename them over time.
func Presets() []Preset {
	return []Preset{
		{Name: "OpenAI", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"},
		{Name: "阿里云百炼", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-vl-max"},
		{Name: "智谱 GLM", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4v-plus"},
		{Name: "硅基流动", BaseURL: "https://api.siliconflow.cn/v1", Model: "Qwen/Qwen2.5-VL-72B-Instruct"},
		{Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", Model: "openai/gpt-4o"},
		{Name: "Ollama（本机）", BaseURL: "http://127.0.0.1:11434/v1", Model: "qwen2.5vl:7b", Note: "本地服务通常不需要 API Key"},
		{Name: "LM Studio（本机）", BaseURL: "http://127.0.0.1:1234/v1", Note: "模型名填 LM Studio 里加载的那个"},
	}
}

// Manager owns the config file and serialises access to it.
type Manager struct {
	mu   sync.RWMutex
	path string
	cfg  Config
	def  Config

	created bool
	filled  []string
}

// Load reads the config file. A missing file is written out from Default; an
// existing one is merged over Default so a file written by an older version,
// or hand-edited down to a couple of keys, still starts with every field set.
func Load(path, home string) (*Manager, error) {
	def := Default(home)
	m := &Manager{path: path, cfg: def, def: def}

	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		m.created = true
		return m, m.save()
	case err != nil:
		return nil, err
	}

	// Unmarshalling onto the defaults leaves absent keys at their default
	// value; the raw map is what tells us which keys those were.
	if err := json.Unmarshal(data, &m.cfg); err != nil {
		return nil, fmt.Errorf("%s 不是合法 JSON: %w", path, err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(data, &onDisk); err != nil {
		return nil, fmt.Errorf("%s 不是合法 JSON: %w", path, err)
	}

	m.filled = missingKeys(asMap(def), onDisk, "")
	m.filled = append(m.filled, m.cfg.repair(def)...)
	sort.Strings(m.filled)

	if len(m.filled) > 0 {
		if err := m.save(); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Created reports whether the config file was written from scratch this run.
func (m *Manager) Created() bool { return m.created }

// Filled lists the dotted keys that were missing or unusable on disk and have
// been written back from the defaults.
func (m *Manager) Filled() []string { return append([]string(nil), m.filled...) }

// repair replaces values that cannot work with the default, returning the
// dotted names of everything it touched. Zero stays meaningful where it has a
// meaning: maxRepairRounds 0 disables repairs, imageMaxEdge 0 sends the
// original image.
func (c *Config) repair(def Config) []string {
	var fixed []string
	str := func(name string, p *string, fallback string) {
		if strings.TrimSpace(*p) == "" && fallback != "" {
			*p = fallback
			fixed = append(fixed, name)
		}
	}
	str("listen", &c.Listen, def.Listen)
	str("comfyuiRoot", &c.ComfyUIRoot, def.ComfyUIRoot)
	str("vision.baseURL", &c.Vision.BaseURL, def.Vision.BaseURL)

	if c.WorkflowDirs == nil {
		c.WorkflowDirs = []string{}
		fixed = append(fixed, "workflowDirs")
	}
	if c.MaxRepairRounds < 0 {
		c.MaxRepairRounds = def.MaxRepairRounds
		fixed = append(fixed, "maxRepairRounds")
	}
	if c.Vision.MaxTokens <= 0 {
		c.Vision.MaxTokens = def.Vision.MaxTokens
		fixed = append(fixed, "vision.maxTokens")
	}
	if c.Vision.Temperature < 0 {
		c.Vision.Temperature = def.Vision.Temperature
		fixed = append(fixed, "vision.temperature")
	}
	if c.Vision.ImageMaxEdge < 0 {
		c.Vision.ImageMaxEdge = def.Vision.ImageMaxEdge
		fixed = append(fixed, "vision.imageMaxEdge")
	}
	if c.Writer.MaxTokens <= 0 {
		c.Writer.MaxTokens = def.Writer.MaxTokens
		fixed = append(fixed, "writer.maxTokens")
	}
	if c.Writer.Temperature < 0 {
		c.Writer.Temperature = def.Writer.Temperature
		fixed = append(fixed, "writer.temperature")
	}

	// The settings page says not to include the method path, but pasting a
	// full endpoint is the obvious mistake to make.
	for name, p := range map[string]*string{
		"vision.baseURL": &c.Vision.BaseURL,
		"writer.baseURL": &c.Writer.BaseURL,
	} {
		if trimmed := TrimBaseURL(*p); trimmed != *p {
			*p = trimmed
			fixed = append(fixed, name)
		}
	}
	return fixed
}

// TrimBaseURL drops the surrounding whitespace, any trailing slash and the
// /chat/completions suffix, leaving the base the client appends to.
func TrimBaseURL(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "/")
	if trimmed := strings.TrimSuffix(s, "/chat/completions"); trimmed != s {
		s = strings.TrimRight(trimmed, "/")
	}
	return s
}

// missingKeys walks the default shape and reports every dotted key the file
// does not contain.
func missingKeys(want, got map[string]any, prefix string) []string {
	var out []string
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		path := prefix + name
		have, ok := got[name]
		if !ok {
			out = append(out, path)
			continue
		}
		wantChild, okWant := want[name].(map[string]any)
		gotChild, okGot := have.(map[string]any)
		if okWant && okGot {
			out = append(out, missingKeys(wantChild, gotChild, path+".")...)
		}
	}
	return out
}

func asMap(c Config) map[string]any {
	data, err := json.Marshal(c)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// Path is the config file location.
func (m *Manager) Path() string { return m.path }

// Snapshot returns a copy of the current configuration.
func (m *Manager) Snapshot() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.cfg
	out.WorkflowDirs = append([]string{}, m.cfg.WorkflowDirs...)
	return out
}

// Update applies fn under the write lock, repairs anything it left unusable
// and persists the result.
func (m *Manager) Update(fn func(*Config)) error {
	m.mu.Lock()
	fn(&m.cfg)
	m.cfg.repair(m.def)
	m.mu.Unlock()
	return m.save()
}

func (m *Manager) save() error {
	m.mu.RLock()
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	m.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

// ResolvedWriter returns the writer endpoint, falling back to the vision one.
func (m *Manager) ResolvedWriter() WriterConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w := m.cfg.Writer
	if w.SameAsVision {
		w.BaseURL = m.cfg.Vision.BaseURL
		w.APIKey = m.cfg.Vision.APIKey
		if w.Model == "" {
			w.Model = m.cfg.Vision.Model
		}
	}
	if w.MaxTokens <= 0 {
		w.MaxTokens = 8192
	}
	return w
}

// WorkflowSearchDirs lists every directory scanned for workflow files.
func (m *Manager) WorkflowSearchDirs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.cfg.WorkflowDirs) > 0 {
		return append([]string(nil), m.cfg.WorkflowDirs...)
	}
	if m.cfg.ComfyUIRoot == "" {
		return nil
	}
	return []string{filepath.Join(m.cfg.ComfyUIRoot, "user", "default", "workflows")}
}

// InputDir is the ComfyUI input directory holding LoadImage sources.
func (m *Manager) InputDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg.ComfyUIRoot == "" {
		return ""
	}
	return filepath.Join(m.cfg.ComfyUIRoot, "input")
}
