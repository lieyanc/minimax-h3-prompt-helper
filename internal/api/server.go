// Package api exposes the HTTP surface: workflow discovery, task CRUD, the
// interactive question loop, and the streaming generation endpoint.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"h3helper/internal/comfy"
	"h3helper/internal/config"
	"h3helper/internal/llm"
	"h3helper/internal/promptgen"
	"h3helper/internal/slots"
	"h3helper/internal/store"
	"h3helper/internal/task"
	"h3helper/internal/validate"
	"h3helper/internal/vision"
)

// Server wires the HTTP handlers to the library, the store and the model
// endpoints.
type Server struct {
	cfg     *config.Manager
	lib     *comfy.Library
	store   *store.Store
	static  http.Handler
	version string
}

// New builds a server.
func New(cfg *config.Manager, lib *comfy.Library, st *store.Store, static http.Handler, version string) *Server {
	return &Server{cfg: cfg, lib: lib, store: st, static: static, version: version}
}

// Handler returns the root mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/config", s.guard(s.handleGetConfig))
	mux.HandleFunc("PUT /api/config", s.guard(s.handlePutConfig))
	mux.HandleFunc("POST /api/config/test", s.guard(s.handleTestConfig))

	mux.HandleFunc("GET /api/workflows", s.guard(s.handleWorkflows))
	mux.HandleFunc("GET /api/inputs", s.guard(s.handleInputs))
	mux.HandleFunc("GET /api/image", s.guard(s.handleImage))

	mux.HandleFunc("GET /api/tasks", s.guard(s.handleListTasks))
	mux.HandleFunc("POST /api/tasks", s.guard(s.handleCreateTask))
	mux.HandleFunc("GET /api/tasks/{id}", s.guard(s.handleGetTask))
	mux.HandleFunc("DELETE /api/tasks/{id}", s.guard(s.handleDeleteTask))
	mux.HandleFunc("PATCH /api/tasks/{id}", s.guard(s.handlePatchTask))
	mux.HandleFunc("POST /api/tasks/{id}/analyze", s.guard(s.handleAnalyze))
	mux.HandleFunc("POST /api/tasks/{id}/generate", s.guard(s.handleGenerate))
	mux.HandleFunc("POST /api/tasks/{id}/validate", s.guard(s.handleValidate))

	if s.static != nil {
		mux.Handle("/", s.static)
	}
	return logRequests(mux)
}

// guard enforces the shared token when one is configured.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := s.cfg.Snapshot().Token
		if token != "" {
			given := r.Header.Get("X-Auth-Token")
			if given == "" {
				given = r.URL.Query().Get("token")
			}
			if given != token {
				writeError(w, http.StatusUnauthorized, "访问令牌不正确")
				return
			}
		}
		next(w, r)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

// ----------------------------------------------------------------- health --

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	c := s.cfg.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"version":     s.version,
		"needsToken":  c.Token != "",
		"comfyuiRoot": c.ComfyUIRoot,
	})
}

// ----------------------------------------------------------------- config --

type configView struct {
	config.Config
	VisionHasKey bool            `json:"visionHasKey"`
	WriterHasKey bool            `json:"writerHasKey"`
	SearchDirs   []string        `json:"searchDirs"`
	InputDir     string          `json:"inputDir"`
	Presets      []config.Preset `json:"presets"`
	ConfigPath   string          `json:"configPath"`
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	c := s.cfg.Snapshot()
	view := configView{
		Config:       c,
		VisionHasKey: c.Vision.APIKey != "",
		WriterHasKey: c.Writer.APIKey != "",
		SearchDirs:   s.cfg.WorkflowSearchDirs(),
		InputDir:     s.cfg.InputDir(),
		Presets:      config.Presets(),
		ConfigPath:   s.cfg.Path(),
	}
	view.Vision.APIKey = ""
	view.Writer.APIKey = ""
	view.Token = ""
	writeJSON(w, http.StatusOK, view)
}

type configPatch struct {
	Listen          *string   `json:"listen"`
	Token           *string   `json:"token"`
	ComfyUIRoot     *string   `json:"comfyuiRoot"`
	WorkflowDirs    *[]string `json:"workflowDirs"`
	StrictEnglish   *bool     `json:"strictEnglish"`
	MaxRepairRounds *int      `json:"maxRepairRounds"`

	Vision *struct {
		BaseURL      *string  `json:"baseURL"`
		APIKey       *string  `json:"apiKey"`
		Model        *string  `json:"model"`
		MaxTokens    *int     `json:"maxTokens"`
		Temperature  *float64 `json:"temperature"`
		ImageMaxEdge *int     `json:"imageMaxEdge"`
	} `json:"vision"`

	Writer *struct {
		SameAsVision *bool    `json:"sameAsVision"`
		BaseURL      *string  `json:"baseURL"`
		APIKey       *string  `json:"apiKey"`
		Model        *string  `json:"model"`
		MaxTokens    *int     `json:"maxTokens"`
		Temperature  *float64 `json:"temperature"`
	} `json:"writer"`
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var patch configPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}

	err := s.cfg.Update(func(c *config.Config) {
		setString(&c.Listen, patch.Listen)
		setString(&c.Token, patch.Token)
		setString(&c.ComfyUIRoot, patch.ComfyUIRoot)
		if patch.WorkflowDirs != nil {
			c.WorkflowDirs = *patch.WorkflowDirs
		}
		if patch.StrictEnglish != nil {
			c.StrictEnglish = *patch.StrictEnglish
		}
		if patch.MaxRepairRounds != nil && *patch.MaxRepairRounds >= 0 {
			c.MaxRepairRounds = *patch.MaxRepairRounds
		}
		if v := patch.Vision; v != nil {
			setString(&c.Vision.BaseURL, v.BaseURL)
			setString(&c.Vision.APIKey, v.APIKey)
			setString(&c.Vision.Model, v.Model)
			setInt(&c.Vision.MaxTokens, v.MaxTokens)
			setFloat(&c.Vision.Temperature, v.Temperature)
			setInt(&c.Vision.ImageMaxEdge, v.ImageMaxEdge)
		}
		if v := patch.Writer; v != nil {
			if v.SameAsVision != nil {
				c.Writer.SameAsVision = *v.SameAsVision
			}
			setString(&c.Writer.BaseURL, v.BaseURL)
			setString(&c.Writer.APIKey, v.APIKey)
			setString(&c.Writer.Model, v.Model)
			setInt(&c.Writer.MaxTokens, v.MaxTokens)
			setFloat(&c.Writer.Temperature, v.Temperature)
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存配置失败: "+err.Error())
		return
	}

	s.lib.Configure(s.cfg.WorkflowSearchDirs(), s.cfg.InputDir())
	s.handleGetConfig(w, r)
}

func (s *Server) handleTestConfig(w http.ResponseWriter, r *http.Request) {
	which := r.URL.Query().Get("target")
	client := s.visionClient()
	if which == "writer" {
		client = s.writerClient()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	reply, err := client.Complete(ctx, []llm.Message{
		llm.Text("user", "Reply with the single word: ok"),
	}, llm.Options{MaxTokens: 16})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reply": strings.TrimSpace(reply), "model": client.Model})
}

// -------------------------------------------------------------- workflows --

func (s *Server) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	workflows, err := s.lib.Scan()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "扫描工作流失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dirs":      s.cfg.WorkflowSearchDirs(),
		"inputDir":  s.cfg.InputDir(),
		"workflows": workflows,
	})
}

func (s *Server) handleInputs(w http.ResponseWriter, r *http.Request) {
	images, err := s.lib.InputImages()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dir": s.cfg.InputDir(), "images": images})
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "缺少 name 参数")
		return
	}
	path, err := s.lib.ResolveInputPath(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, path)
}

// ------------------------------------------------------------------ tasks --

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tasks": s.store.List()})
}

type createTaskRequest struct {
	WorkflowFile string `json:"workflowFile"`
	VariantID    string `json:"variantId"`
	Title        string `json:"title"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}
	wf, variant, err := s.lib.FindVariant(req.WorkflowFile, req.VariantID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	t := buildTask(wf, variant, req.Title)
	t.Pending = slots.Next(t, 3)
	if err := s.store.Create(t); err != nil {
		writeError(w, http.StatusInternalServerError, "创建任务失败: "+err.Error())
		return
	}
	// Read back a copy so the view helper does not mutate the stored task.
	saved, err := s.store.Get(t.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.taskView(saved))
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.taskView(t))
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type patchTaskRequest struct {
	Title    *string           `json:"title"`
	Answers  map[string]string `json:"answers"`
	Duration *float64          `json:"duration"`
	Frames   *int              `json:"frames"`
	Prompt   *string           `json:"prompt"`
}

func (s *Server) handlePatchTask(w http.ResponseWriter, r *http.Request) {
	var req patchTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}

	t, err := s.store.Update(r.PathValue("id"), func(t *task.Task) error {
		if req.Title != nil {
			t.Title = *req.Title
		}
		if len(req.Answers) > 0 {
			if t.Answers == nil {
				t.Answers = map[string]string{}
			}
			answered := map[string]string{}
			for k, v := range req.Answers {
				t.Answers[k] = v
				if strings.TrimSpace(v) != "" {
					answered[k] = v
				}
			}
			// Clearing a slot to re-ask it is not a question round.
			if len(answered) > 0 {
				t.Rounds = append(t.Rounds, task.Round{
					Index:     len(t.Rounds) + 1,
					Questions: t.Pending,
					Answers:   answered,
					At:        time.Now(),
				})
			}
			if b, ok := req.Answers[slots.Brief]; ok {
				t.Brief = b
			}
		}
		if req.Frames != nil && *req.Frames > 0 {
			t.Constraints.Frames = comfy.SnapFrames(*req.Frames)
			t.Constraints.Duration = float64(t.Constraints.Frames) / comfy.H3FPS
			t.Constraints.MaxShots = maxShots(t.Constraints.Duration)
		} else if req.Duration != nil && *req.Duration > 0 {
			frames := comfy.SnapFrames(int(*req.Duration * comfy.H3FPS))
			t.Constraints.Frames = frames
			t.Constraints.Duration = float64(frames) / comfy.H3FPS
			t.Constraints.MaxShots = maxShots(t.Constraints.Duration)
		}
		if req.Prompt != nil {
			t.Prompt = *req.Prompt
			t.Findings = nonNil(validate.Check(t.Prompt, s.specFor(t)))
		}

		t.Pending = slots.Next(t, 3)
		if len(slots.MissingRequired(t)) == 0 {
			if t.Prompt != "" {
				t.Status = task.StatusDone
			} else {
				t.Status = task.StatusReady
			}
		} else {
			t.Status = task.StatusQuestioning
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.taskView(t))
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.Update(r.PathValue("id"), func(t *task.Task) error {
		t.Findings = nonNil(validate.Check(t.Prompt, s.specFor(t)))
		return nil
	})
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.taskView(t))
}

// ---------------------------------------------------------------- analyze --

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := s.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	sse, err := newSSE(w)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer sse.close()

	client := s.visionClient()
	maxEdge := s.cfg.Snapshot().Vision.ImageMaxEdge

	for _, img := range t.Images {
		if img.Source == "" || img.Missing {
			sse.send("image", map[string]any{
				"label": img.Label,
				"error": "参考图不在 ComfyUI input 目录里，跳过",
			})
			continue
		}
		path, err := s.lib.ResolveInputPath(img.Source)
		if err != nil {
			sse.send("image", map[string]any{"label": img.Label, "error": err.Error()})
			continue
		}

		sse.send("progress", map[string]any{"label": img.Label, "state": "analyzing"})
		facts, err := vision.Analyze(r.Context(), client, path, img.Label, maxEdge)
		if err != nil {
			facts.Error = err.Error()
		}

		label := img.Label
		captured := facts
		if _, uerr := s.store.Update(id, func(t *task.Task) error {
			if t.Facts == nil {
				t.Facts = map[string]task.Facts{}
			}
			t.Facts[label] = captured
			t.Pending = slots.Next(t, 3)
			return nil
		}); uerr != nil {
			sse.send("image", map[string]any{"label": label, "error": uerr.Error()})
			continue
		}
		sse.send("image", map[string]any{"label": label, "facts": captured})
	}

	final, err := s.store.Get(id)
	if err == nil {
		sse.send("done", s.taskView(final))
	} else {
		sse.send("error", map[string]any{"error": err.Error()})
	}
}

// --------------------------------------------------------------- generate --

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := s.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if missing := slots.MissingRequired(t); len(missing) > 0 {
		writeError(w, http.StatusBadRequest, "还有必填项没回答: "+strings.Join(missing, ", "))
		return
	}

	sse, err := newSSE(w)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer sse.close()

	cfg := s.cfg.Snapshot()
	client := s.writerClient()
	spec := s.specFor(t)

	msgs := promptgen.Build(t)
	var prompt string
	var findings []validate.Finding
	var attempts []task.Attempt

	for round := 0; ; round++ {
		if round > 0 {
			sse.send("repair", map[string]any{"round": round, "findings": findings})
			msgs = promptgen.BuildRepair(t, prompt, findings)
		}
		sse.send("progress", map[string]any{"state": "writing", "round": round})

		out, err := client.Stream(r.Context(), msgs, llm.Options{}, func(delta string) {
			sse.send("delta", map[string]any{"text": delta, "round": round})
		})
		if err != nil {
			sse.send("error", map[string]any{"error": err.Error()})
			s.markError(id, err.Error())
			return
		}

		prompt = cleanOutput(out)
		findings = nonNil(validate.Check(prompt, spec))
		attempts = append(attempts, task.Attempt{
			Index:    round + 1,
			Prompt:   prompt,
			Findings: findings,
			Repaired: round > 0,
			At:       time.Now(),
		})
		sse.send("validated", map[string]any{"round": round, "findings": findings, "prompt": prompt})

		if !validate.HasErrors(findings) || round >= cfg.MaxRepairRounds {
			break
		}
	}

	final, err := s.store.Update(id, func(t *task.Task) error {
		t.Prompt = prompt
		t.Findings = findings
		t.Attempts = attempts
		t.GeneratedAt = time.Now()
		t.Status = task.StatusDone
		t.Error = ""
		return nil
	})
	if err != nil {
		sse.send("error", map[string]any{"error": err.Error()})
		return
	}
	sse.send("done", s.taskView(final))
}

func (s *Server) markError(id, msg string) {
	_, _ = s.store.Update(id, func(t *task.Task) error {
		t.Status = task.StatusError
		t.Error = msg
		return nil
	})
}

// cleanOutput strips markdown fences a model may wrap the prompt in.
func cleanOutput(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}

// ---------------------------------------------------------------- helpers --

func (s *Server) visionClient() *llm.Client {
	c := s.cfg.Snapshot()
	return llm.New(c.Vision.BaseURL, c.Vision.APIKey, c.Vision.Model, c.Vision.MaxTokens, c.Vision.Temperature)
}

func (s *Server) writerClient() *llm.Client {
	w := s.cfg.ResolvedWriter()
	return llm.New(w.BaseURL, w.APIKey, w.Model, w.MaxTokens, w.Temperature)
}

func (s *Server) specFor(t *task.Task) validate.Spec {
	spec := t.Constraints.Spec(s.cfg.Snapshot().StrictEnglish)
	spec.Dialogue = slots.DialogueTexts(t)
	return spec
}

type taskView struct {
	*task.Task
	Questions []task.Question `json:"questions"`
	Missing   []string        `json:"missing"`
	Answered  int             `json:"answeredCount"`
	Total     int             `json:"requiredCount"`
	Blocking  int             `json:"blockingFindings"`
}

func (s *Server) taskView(t *task.Task) taskView {
	normalize(t)
	answered, total := slots.Progress(t)
	blocking := 0
	for _, f := range t.Findings {
		if f.Severity == validate.SeverityError {
			blocking++
		}
	}
	return taskView{
		Task:      t,
		Questions: nonNil(slots.Next(t, 3)),
		Missing:   nonNil(slots.MissingRequired(t)),
		Answered:  answered,
		Total:     total,
		Blocking:  blocking,
	}
}

// nonNil keeps empty slices out of the JSON as `null`, which the frontend would
// otherwise have to guard on every read.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// normalize fills in the nil slices and maps of a task before it is serialised.
func normalize(t *task.Task) {
	t.Images = nonNil(t.Images)
	t.Rounds = nonNil(t.Rounds)
	t.Attempts = nonNil(t.Attempts)
	t.Findings = nonNil(t.Findings)
	t.Pending = nonNil(t.Pending)
	t.Constraints.PictureLabels = nonNil(t.Constraints.PictureLabels)
	t.Constraints.VideoLabels = nonNil(t.Constraints.VideoLabels)
	t.Constraints.AudioLabels = nonNil(t.Constraints.AudioLabels)
	t.Constraints.Notes = nonNil(t.Constraints.Notes)
	if t.Answers == nil {
		t.Answers = map[string]string{}
	}
	if t.Facts == nil {
		t.Facts = map[string]task.Facts{}
	}
}

func buildTask(wf comfy.Workflow, v comfy.Variant, title string) *task.Task {
	c := task.Constraints{
		Mode:        v.Mode,
		Width:       v.Width,
		Height:      v.Height,
		Frames:      v.Frames,
		FPS:         v.FPS,
		Duration:    v.Duration,
		AspectLabel: aspectLabel(v.Width, v.Height),
		MaxShots:    maxShots(v.Duration),
		Notes:       v.Notes,
	}

	t := &task.Task{
		Title:        title,
		Status:       task.StatusQuestioning,
		WorkflowFile: wf.File,
		WorkflowName: wf.Name,
		VariantID:    v.ID,
		VariantNode:  v.NodeID,
		Answers:      map[string]string{},
		Facts:        map[string]task.Facts{},
	}
	if t.Title == "" {
		t.Title = fmt.Sprintf("%s · %s", wf.Name, v.Mode)
	}

	for _, slot := range v.Images {
		if !slot.Connected {
			continue
		}
		c.PictureLabels = append(c.PictureLabels, slot.Label)
		t.Images = append(t.Images, task.Image{
			Label:   slot.Label,
			Slot:    slot.Slot,
			Role:    slot.Role,
			Source:  slot.Source,
			Origin:  "workflow",
			Missing: slot.Source == "" || !slot.SourceExists,
		})
	}
	for _, slot := range v.Videos {
		if slot.Connected {
			c.VideoLabels = append(c.VideoLabels, slot.Label)
		}
	}
	for _, slot := range v.Audios {
		if slot.Connected {
			c.AudioLabels = append(c.AudioLabels, slot.Label)
		}
	}

	t.Constraints = c
	return t
}

func maxShots(duration float64) int {
	n := int(duration / 1.5)
	if n < 1 {
		return 1
	}
	if n > 6 {
		return 6
	}
	return n
}

// aspectLabel names the frame shape. ComfyUI's ResolutionSelector rounds each
// edge up to a multiple of 32, so an "16:9" preset lands on 864x480 — an exact
// 9:5. Report the nearest well-known ratio instead of the literal reduction.
func aspectLabel(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	shape := "横构图"
	switch {
	case w < h:
		shape = "竖构图"
	case w == h:
		shape = "方构图"
	}

	ratio := float64(w) / float64(h)
	known := []struct {
		name string
		val  float64
	}{
		{"1:1", 1}, {"4:3", 4.0 / 3}, {"3:4", 3.0 / 4},
		{"3:2", 3.0 / 2}, {"2:3", 2.0 / 3},
		{"16:9", 16.0 / 9}, {"9:16", 9.0 / 16},
		{"21:9", 21.0 / 9}, {"9:21", 9.0 / 21},
	}
	best, bestDiff := "", 0.0
	for i, k := range known {
		diff := ratio/k.val - 1
		if diff < 0 {
			diff = -diff
		}
		if i == 0 || diff < bestDiff {
			best, bestDiff = k.name, diff
		}
	}
	if bestDiff <= 0.05 {
		exact := fmt.Sprintf("%d:%d", w/gcd(w, h), h/gcd(w, h))
		if exact == best {
			return best + " " + shape
		}
		return fmt.Sprintf("≈%s %s", best, shape)
	}

	g := gcd(w, h)
	return fmt.Sprintf("%d:%d %s", w/g, h/g, shape)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func setString(dst *string, src *string) {
	if src != nil {
		*dst = strings.TrimSpace(*src)
	}
}

func setInt(dst *int, src *int) {
	if src != nil {
		*dst = *src
	}
}

func setFloat(dst *float64, src *float64) {
	if src != nil {
		*dst = *src
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// ------------------------------------------------------------------- SSE --

type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSE(w http.ResponseWriter) (*sseWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("响应不支持流式输出")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &sseWriter{w: w, flusher: flusher}, nil
}

func (s *sseWriter) send(event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data)
	s.flusher.Flush()
}

func (s *sseWriter) close() {
	fmt.Fprint(s.w, "event: close\ndata: {}\n\n")
	s.flusher.Flush()
}
