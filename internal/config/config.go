// Package config loads and persists the JSON configuration file.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
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

	// Providers are the endpoints an API key belongs to. Models point at one
	// of them by ID.
	Providers []Provider `json:"providers"`
	// Models are the named entries the two roles below choose from.
	Models []Model `json:"models"`

	Vision VisionConfig `json:"vision"`
	Writer WriterConfig `json:"writer"`

	// StrictEnglish turns CJK leakage outside <d> and quoted screen text into a
	// blocking error rather than a warning.
	StrictEnglish bool `json:"strictEnglish"`
	// MaxRepairRounds is how many times a failed validation is fed back to the
	// writing model.
	MaxRepairRounds int `json:"maxRepairRounds"`
}

// Provider is one OpenAI-compatible endpoint together with its key. ID is what
// models refer to; Name is what the settings page shows.
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"`
}

// Model is one model served by ProviderID. Model is the name sent to the API,
// DisplayName the label shown in the pickers.
type Model struct {
	ID              string  `json:"id"`
	ProviderID      string  `json:"providerId"`
	Model           string  `json:"model"`
	DisplayName     string  `json:"displayName"`
	MaxTokens       int     `json:"maxTokens"`
	Temperature     float64 `json:"temperature"`
	ReasoningEffort string  `json:"reasoningEffort"`
}

// Label is the name to show for a model, falling back to the API name.
func (m Model) Label() string {
	if s := strings.TrimSpace(m.DisplayName); s != "" {
		return s
	}
	if s := strings.TrimSpace(m.Model); s != "" {
		return s
	}
	return m.ID
}

// VisionConfig picks the model that reads the reference images.
type VisionConfig struct {
	ModelID string `json:"modelId"`
	// ImageMaxEdge downsizes images before base64 upload. 0 disables resizing.
	ImageMaxEdge int `json:"imageMaxEdge"`
}

// WriterConfig picks the model that composes the final H3 prompt. It may be
// the same entry as Vision.
type WriterConfig struct {
	ModelID string `json:"modelId"`
}

// Endpoint is a model joined to its provider: everything the llm client needs
// for one call, plus the labels the UI reports back.
type Endpoint struct {
	ProviderID   string
	ProviderName string
	BaseURL      string
	APIKey       string
	ModelID      string
	Model        string
	DisplayName  string
	MaxTokens    int
	Temperature  float64
	// ReasoningEffort is sent as reasoning_effort when non-empty. Keeping the
	// empty value means older OpenAI-compatible endpoints receive no new field.
	ReasoningEffort string
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
		Providers: []Provider{
			{ID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1"},
		},
		// Two entries pointing at the same provider: the roles want different
		// sampling even when they run on one model.
		Models: []Model{
			{ID: "vision", ProviderID: "openai", Model: "gpt-4o", DisplayName: "看图的模型", MaxTokens: 4096, Temperature: 0.2},
			{ID: "writer", ProviderID: "openai", Model: "gpt-4o", DisplayName: "写提示词的模型", MaxTokens: 8192, Temperature: 0.7},
		},
		Vision:          VisionConfig{ModelID: "vision", ImageMaxEdge: 1280},
		Writer:          WriterConfig{ModelID: "writer"},
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
	var onDisk map[string]any
	if err := json.Unmarshal(data, &onDisk); err != nil {
		return nil, fmt.Errorf("%s 不是合法 JSON: %w", path, err)
	}
	// Lists are replaced, not merged: decoding into the default slice would
	// leave its entries' other fields showing through the file's.
	if _, ok := onDisk["providers"]; ok {
		m.cfg.Providers = nil
	}
	if _, ok := onDisk["models"]; ok {
		m.cfg.Models = nil
	}
	if err := json.Unmarshal(data, &m.cfg); err != nil {
		return nil, fmt.Errorf("%s 不是合法 JSON: %w", path, err)
	}
	if _, ok := onDisk["providers"]; !ok {
		m.cfg.migrateLegacy(data)
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

// legacyConfig is the pre-provider file shape, where each role carried its own
// endpoint, key and model.
type legacyConfig struct {
	Vision struct {
		BaseURL     string  `json:"baseURL"`
		APIKey      string  `json:"apiKey"`
		Model       string  `json:"model"`
		MaxTokens   int     `json:"maxTokens"`
		Temperature float64 `json:"temperature"`
	} `json:"vision"`
	Writer struct {
		SameAsVision bool    `json:"sameAsVision"`
		BaseURL      string  `json:"baseURL"`
		APIKey       string  `json:"apiKey"`
		Model        string  `json:"model"`
		MaxTokens    int     `json:"maxTokens"`
		Temperature  float64 `json:"temperature"`
	} `json:"writer"`
}

// migrateLegacy rewrites a pre-provider file into one provider (two, if the
// writer pointed somewhere else) plus a model entry per role, so upgrading
// keeps the endpoint, the key and both sampling profiles.
func (c *Config) migrateLegacy(data []byte) {
	var old legacyConfig
	if err := json.Unmarshal(data, &old); err != nil {
		return
	}
	if strings.TrimSpace(old.Vision.BaseURL) == "" && strings.TrimSpace(old.Vision.Model) == "" {
		return
	}

	taken := map[string]bool{}
	visionProvider := Provider{
		ID:      uniqueID(providerID(old.Vision.BaseURL), taken),
		Name:    providerName(old.Vision.BaseURL),
		BaseURL: TrimBaseURL(old.Vision.BaseURL),
		APIKey:  strings.TrimSpace(old.Vision.APIKey),
	}
	c.Providers = []Provider{visionProvider}

	writerProviderID := visionProvider.ID
	writerBase := TrimBaseURL(old.Writer.BaseURL)
	if !old.Writer.SameAsVision && writerBase != "" && writerBase != visionProvider.BaseURL {
		writerProvider := Provider{
			ID:      uniqueID(providerID(writerBase), taken),
			Name:    providerName(writerBase),
			BaseURL: writerBase,
			APIKey:  strings.TrimSpace(old.Writer.APIKey),
		}
		c.Providers = append(c.Providers, writerProvider)
		writerProviderID = writerProvider.ID
	}

	writerModel := strings.TrimSpace(old.Writer.Model)
	if writerModel == "" {
		writerModel = strings.TrimSpace(old.Vision.Model)
	}
	c.Models = []Model{
		{
			ID: "vision", ProviderID: visionProvider.ID,
			Model:       strings.TrimSpace(old.Vision.Model),
			DisplayName: roleLabel(old.Vision.Model, "看图"),
			MaxTokens:   old.Vision.MaxTokens, Temperature: old.Vision.Temperature,
		},
		{
			ID: "writer", ProviderID: writerProviderID,
			Model:       writerModel,
			DisplayName: roleLabel(writerModel, "写作"),
			MaxTokens:   old.Writer.MaxTokens, Temperature: old.Writer.Temperature,
		},
	}
	c.Vision.ModelID = "vision"
	c.Writer.ModelID = "writer"
}

// providerID derives a stable identifier from an endpoint's host.
func providerID(baseURL string) string {
	host := baseURL
	if u, err := url.Parse(strings.TrimSpace(baseURL)); err == nil && u.Host != "" {
		host = u.Host
	}
	return slug(host)
}

// providerName prefers the name of a known preset, so a migrated file reads
// like something a human typed.
func providerName(baseURL string) string {
	trimmed := TrimBaseURL(baseURL)
	for _, p := range Presets() {
		if p.BaseURL == trimmed {
			return p.Name
		}
	}
	if u, err := url.Parse(trimmed); err == nil && u.Host != "" {
		return u.Host
	}
	return trimmed
}

// roleLabel names a migrated entry after the model and the job it did, so the
// two are told apart in the pickers even when they run the same model.
func roleLabel(model, role string) string {
	if s := strings.TrimSpace(model); s != "" {
		return fmt.Sprintf("%s（%s）", s, role)
	}
	return role + "的模型"
}

// slug reduces a string to an identifier made of letters, digits and dashes.
// It returns "" for input with nothing usable, such as a purely CJK name.
func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteRune('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// uniqueID marks base as taken, appending -2, -3 … until it is free.
func uniqueID(base string, taken map[string]bool) string {
	if base == "" {
		base = "item"
	}
	id := base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	taken[id] = true
	return id
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

	if c.WorkflowDirs == nil {
		c.WorkflowDirs = []string{}
		fixed = append(fixed, "workflowDirs")
	}
	if c.MaxRepairRounds < 0 {
		c.MaxRepairRounds = def.MaxRepairRounds
		fixed = append(fixed, "maxRepairRounds")
	}
	if c.Vision.ImageMaxEdge < 0 {
		c.Vision.ImageMaxEdge = def.Vision.ImageMaxEdge
		fixed = append(fixed, "vision.imageMaxEdge")
	}

	fixed = append(fixed, c.repairProviders(def)...)
	fixed = append(fixed, c.repairModels(def)...)
	fixed = append(fixed, c.repairRoles()...)
	return fixed
}

// repairProviders gives every endpoint a unique id and a name, and normalises
// the address. Ids the user chose are kept as typed.
func (c *Config) repairProviders(def Config) []string {
	var fixed []string
	if len(c.Providers) == 0 {
		c.Providers = append([]Provider{}, def.Providers...)
		fixed = append(fixed, "providers")
	}

	taken := map[string]bool{}
	for i := range c.Providers {
		p := &c.Providers[i]
		name := func(key string) string { return fmt.Sprintf("providers[%d].%s", i, key) }

		// The settings page says not to include the method path, but pasting a
		// full endpoint is the obvious mistake to make.
		if trimmed := TrimBaseURL(p.BaseURL); trimmed != p.BaseURL {
			p.BaseURL = trimmed
			fixed = append(fixed, name("baseURL"))
		}
		p.Name = strings.TrimSpace(p.Name)
		p.APIKey = strings.TrimSpace(p.APIKey)

		id := strings.TrimSpace(p.ID)
		if id == "" {
			id = providerID(p.BaseURL)
		}
		if id == "" {
			id = slug(p.Name)
		}
		if id == "" {
			id = fmt.Sprintf("provider-%d", i+1)
		}
		if unique := uniqueID(id, taken); unique != p.ID {
			p.ID = unique
			fixed = append(fixed, name("id"))
		}
		if p.Name == "" {
			p.Name = p.ID
			fixed = append(fixed, name("name"))
		}
	}
	return fixed
}

// defaultMaxTokens is what a model entry falls back to; the roles set their
// own in Default.
const defaultMaxTokens = 4096

// repairModels gives every model a unique id and a live provider. A model
// pointing at a deleted provider falls back to the first one rather than
// failing at request time.
func (c *Config) repairModels(def Config) []string {
	var fixed []string
	if len(c.Models) == 0 {
		c.Models = append([]Model{}, def.Models...)
		fixed = append(fixed, "models")
	}

	known := map[string]bool{}
	for _, p := range c.Providers {
		known[p.ID] = true
	}
	first := ""
	if len(c.Providers) > 0 {
		first = c.Providers[0].ID
	}

	taken := map[string]bool{}
	for i := range c.Models {
		mo := &c.Models[i]
		name := func(key string) string { return fmt.Sprintf("models[%d].%s", i, key) }

		mo.Model = strings.TrimSpace(mo.Model)
		mo.DisplayName = strings.TrimSpace(mo.DisplayName)
		mo.ReasoningEffort = strings.ToLower(strings.TrimSpace(mo.ReasoningEffort))

		id := strings.TrimSpace(mo.ID)
		if id == "" {
			id = slug(mo.Model)
		}
		if id == "" {
			id = fmt.Sprintf("model-%d", i+1)
		}
		if unique := uniqueID(id, taken); unique != mo.ID {
			// Roles referring to the old id are re-pointed by repairRoles.
			mo.ID = unique
			fixed = append(fixed, name("id"))
		}
		if pid := strings.TrimSpace(mo.ProviderID); !known[pid] {
			mo.ProviderID = first
			fixed = append(fixed, name("providerId"))
		}
		if mo.MaxTokens <= 0 {
			mo.MaxTokens = defaultMaxTokens
			fixed = append(fixed, name("maxTokens"))
		}
		if mo.Temperature < 0 {
			mo.Temperature = 0
			fixed = append(fixed, name("temperature"))
		}
	}
	return fixed
}

// repairRoles points each role at a model that exists.
func (c *Config) repairRoles() []string {
	var fixed []string
	known := map[string]bool{}
	for _, mo := range c.Models {
		known[mo.ID] = true
	}
	first := ""
	if len(c.Models) > 0 {
		first = c.Models[0].ID
	}

	if id := strings.TrimSpace(c.Vision.ModelID); !known[id] {
		c.Vision.ModelID = first
		fixed = append(fixed, "vision.modelId")
	}
	if id := strings.TrimSpace(c.Writer.ModelID); !known[id] {
		// One model for both roles is a valid setup, so the vision entry is a
		// better fallback than the first one in the list.
		c.Writer.ModelID = c.Vision.ModelID
		fixed = append(fixed, "writer.modelId")
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
	out.Providers = append([]Provider{}, m.cfg.Providers...)
	out.Models = append([]Model{}, m.cfg.Models...)
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

// Resolve joins a model entry to its provider.
func (m *Manager) Resolve(modelID string) (Endpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.resolve(modelID)
}

// VisionEndpoint is the model that reads the reference images.
func (m *Manager) VisionEndpoint() (Endpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.resolve(m.cfg.Vision.ModelID)
}

// WriterEndpoint is the model that composes the prompt.
func (m *Manager) WriterEndpoint() (Endpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.resolve(m.cfg.Writer.ModelID)
}

func (c *Config) resolve(modelID string) (Endpoint, error) {
	if len(c.Models) == 0 {
		return Endpoint{}, fmt.Errorf("还没有配置任何模型，先在设置页添加一个")
	}
	modelID = strings.TrimSpace(modelID)
	idx := -1
	for i, mo := range c.Models {
		if mo.ID == modelID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Endpoint{}, fmt.Errorf("找不到模型 %q，请在设置页重新选择", modelID)
	}
	mo := c.Models[idx]

	for _, p := range c.Providers {
		if p.ID != mo.ProviderID {
			continue
		}
		return Endpoint{
			ProviderID: p.ID, ProviderName: p.Name, BaseURL: p.BaseURL, APIKey: p.APIKey,
			ModelID: mo.ID, Model: mo.Model, DisplayName: mo.Label(),
			MaxTokens: mo.MaxTokens, Temperature: mo.Temperature, ReasoningEffort: mo.ReasoningEffort,
		}, nil
	}
	return Endpoint{}, fmt.Errorf("模型 %q 指向的供应商 %q 不存在", mo.Label(), mo.ProviderID)
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
