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
	"h3helper/internal/questions"
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
	uploads string
	static  http.Handler
	version string
}

// New builds a server. uploads is the directory manually uploaded reference
// images are kept in; nothing is ever written into ComfyUI's own folders.
func New(cfg *config.Manager, lib *comfy.Library, st *store.Store, uploads string, static http.Handler, version string) *Server {
	return &Server{cfg: cfg, lib: lib, store: st, uploads: uploads, static: static, version: version}
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
	mux.HandleFunc("POST /api/tasks/{id}/plan", s.guard(s.handlePlan))
	mux.HandleFunc("POST /api/tasks/{id}/images", s.guard(s.handleUploadImage))
	mux.HandleFunc("DELETE /api/tasks/{id}/images/{label}", s.guard(s.handleDeleteImage))
	mux.HandleFunc("GET /api/tasks/{id}/uploads/{file}", s.guard(s.handleUploadedImage))
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

// providerView is a provider with the key replaced by whether one is stored.
type providerView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
	HasKey  bool   `json:"hasKey"`
}

type configView struct {
	config.Config
	// Shadows Config.Providers so the keys never leave the machine.
	Providers  []providerView  `json:"providers"`
	SearchDirs []string        `json:"searchDirs"`
	InputDir   string          `json:"inputDir"`
	Presets    []config.Preset `json:"presets"`
	ConfigPath string          `json:"configPath"`
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	c := s.cfg.Snapshot()
	providers := make([]providerView, 0, len(c.Providers))
	for _, p := range c.Providers {
		providers = append(providers, providerView{
			ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, HasKey: p.APIKey != "",
		})
	}
	view := configView{
		Config:     c,
		Providers:  providers,
		SearchDirs: s.cfg.WorkflowSearchDirs(),
		InputDir:   s.cfg.InputDir(),
		Presets:    config.Presets(),
		ConfigPath: s.cfg.Path(),
	}
	view.Config.Providers = nil // the shadowed copy, cleared so no key can leak
	view.Token = ""
	writeJSON(w, http.StatusOK, view)
}

// providerPatch carries an absent or empty apiKey to mean "leave the stored
// one alone", so the settings page never has to hold a key it was not given.
type providerPatch struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	BaseURL string  `json:"baseURL"`
	APIKey  *string `json:"apiKey"`
}

type configPatch struct {
	Listen          *string   `json:"listen"`
	Token           *string   `json:"token"`
	ComfyUIRoot     *string   `json:"comfyuiRoot"`
	WorkflowDirs    *[]string `json:"workflowDirs"`
	StrictEnglish   *bool     `json:"strictEnglish"`
	MaxRepairRounds *int      `json:"maxRepairRounds"`

	Providers *[]providerPatch `json:"providers"`
	Models    *[]config.Model  `json:"models"`

	Vision *struct {
		ModelID      *string `json:"modelId"`
		ImageMaxEdge *int    `json:"imageMaxEdge"`
	} `json:"vision"`

	Writer *struct {
		ModelID *string `json:"modelId"`
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
		if patch.Providers != nil {
			c.Providers = mergeProviders(c.Providers, *patch.Providers)
		}
		if patch.Models != nil {
			c.Models = *patch.Models
		}
		if v := patch.Vision; v != nil {
			setString(&c.Vision.ModelID, v.ModelID)
			setInt(&c.Vision.ImageMaxEdge, v.ImageMaxEdge)
		}
		if v := patch.Writer; v != nil {
			setString(&c.Writer.ModelID, v.ModelID)
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存配置失败: "+err.Error())
		return
	}

	s.lib.Configure(s.cfg.WorkflowSearchDirs(), s.cfg.InputDir())
	s.handleGetConfig(w, r)
}

// mergeProviders replaces the list, carrying the stored key over to every
// entry the request did not send one for.
func mergeProviders(current []config.Provider, patch []providerPatch) []config.Provider {
	keys := make(map[string]string, len(current))
	for _, p := range current {
		keys[p.ID] = p.APIKey
	}
	out := make([]config.Provider, 0, len(patch))
	for _, p := range patch {
		next := config.Provider{
			ID:      strings.TrimSpace(p.ID),
			Name:    strings.TrimSpace(p.Name),
			BaseURL: strings.TrimSpace(p.BaseURL),
		}
		next.APIKey = keys[next.ID]
		if p.APIKey != nil && strings.TrimSpace(*p.APIKey) != "" {
			next.APIKey = strings.TrimSpace(*p.APIKey)
		}
		out = append(out, next)
	}
	return out
}

func (s *Server) handleTestConfig(w http.ResponseWriter, r *http.Request) {
	var (
		ep  config.Endpoint
		err error
	)
	switch id := strings.TrimSpace(r.URL.Query().Get("modelId")); {
	case id != "":
		ep, err = s.cfg.Resolve(id)
	case r.URL.Query().Get("target") == "writer":
		ep, err = s.cfg.WriterEndpoint()
	default:
		ep, err = s.cfg.VisionEndpoint()
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	client := clientFor(ep)
	reply, err := client.Complete(ctx, []llm.Message{
		llm.Text("user", "Reply with the single word: ok"),
	}, llm.Options{MaxTokens: 16})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"reply":    strings.TrimSpace(reply),
		"model":    client.Model,
		"label":    ep.DisplayName,
		"provider": ep.ProviderName,
	})
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
	// The opening intent question needs no model, so a new task always arrives
	// with something to answer even when no endpoint is configured yet.
	if page, err := questions.Next(r.Context(), nil, t); err == nil {
		applyPage(t, page)
	}
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
	Step     *string           `json:"step"`
	// SubmitPage marks the answers as a whole question page being handed in, so
	// the page is cleared even when every question on it was optional and left
	// blank.
	SubmitPage *bool `json:"submitPage"`
	// Reask puts an already answered question back on the page, so a decision
	// shown in the sidebar can be changed without restarting the interview.
	Reask *string `json:"reask"`
	// SkipQuestions ends the interview where it stands.
	SkipQuestions *bool `json:"skipQuestions"`
}

func (s *Server) handlePatchTask(w http.ResponseWriter, r *http.Request) {
	var req patchTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}

	// Questions are written from the vision facts. Guard the transition at the
	// API boundary as well as in the UI so a direct PATCH cannot create a page
	// from missing or failed image analysis.
	needsVision := req.Reask != nil
	if req.Step != nil {
		step := strings.TrimSpace(*req.Step)
		needsVision = needsVision || step == task.StepQuestions || step == task.StepGenerate
	}
	if needsVision {
		current, err := s.store.Get(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if pending := visionBlockers(current); len(pending) > 0 {
			writeError(w, http.StatusBadRequest, visionGateMessage(pending))
			return
		}
	}

	t, err := s.store.Update(r.PathValue("id"), func(t *task.Task) error {
		if req.Title != nil {
			t.Title = *req.Title
		}
		submitted := req.SubmitPage != nil && *req.SubmitPage
		if len(req.Answers) > 0 || submitted {
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
			if b := strings.TrimSpace(t.AnswerByRole(task.RoleBrief)); b != "" {
				t.Brief = b
			}
			// A handed-in page is done with. Only a required question that was
			// left empty stays, and the form does not allow that. The next page
			// is asked for explicitly, so answering never waits on a model.
			t.Pending = stillRequired(t, t.Pending)
			t.Plan.Questions = t.Pending
		}
		if req.Reask != nil {
			reask(t, strings.TrimSpace(*req.Reask))
		}
		if req.SkipQuestions != nil && *req.SkipQuestions {
			t.Plan.Done = true
			t.Plan.Questions = nil
			t.Plan.Note = "你选择了跳过剩余追问。"
			t.Pending = nil
		}
		if req.Step != nil {
			if step := strings.TrimSpace(*req.Step); validStep(step) {
				t.Step = step
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

		t.Status = statusOf(t)
		return nil
	})
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.taskView(t))
}

// stillRequired keeps the questions of a handed-in page that are required and
// still unanswered.
func stillRequired(t *task.Task, page []task.Question) []task.Question {
	var out []task.Question
	for _, q := range page {
		if q.Required && strings.TrimSpace(t.Answer(q.Slot)) == "" {
			out = append(out, q)
		}
	}
	return out
}

// reask clears one answer and puts its question back on the current page.
func reask(t *task.Task, slot string) {
	if slot == "" {
		return
	}
	delete(t.Answers, slot)
	q, ok := t.QuestionFor(slot)
	if !ok {
		q, ok = fixedQuestion(t, slot)
	}
	if !ok {
		return
	}
	for _, p := range t.Pending {
		if p.Slot == slot {
			return
		}
	}
	t.Pending = append([]task.Question{q}, t.Pending...)
	t.Plan.Questions = t.Pending
	t.Plan.Done = false
	t.Step = task.StepQuestions
}

// fixedQuestion rebuilds a question from the deterministic table. Tasks
// answered before the question agent existed carry no record of the wording
// their answers came from.
func fixedQuestion(t *task.Task, slot string) (task.Question, bool) {
	probe := *t
	probe.Answers = map[string]string{}
	for k, v := range t.Answers {
		if k != slot {
			probe.Answers[k] = v
		}
	}
	probe.Asked, probe.Pending = nil, nil
	for _, q := range slots.Next(&probe, 0) {
		if q.Slot == slot {
			return q, true
		}
	}
	return task.Question{}, false
}

func validStep(step string) bool {
	for _, s := range task.Steps {
		if s == step {
			return true
		}
	}
	return false
}

// statusOf derives the task status from how far the interview and the writing
// have got.
func statusOf(t *task.Task) string {
	switch {
	case t.Prompt != "":
		return task.StatusDone
	case t.Plan.Done && len(slots.Unanswered(t)) == 0:
		return task.StatusReady
	default:
		return task.StatusQuestioning
	}
}

// applyPage installs a freshly planned page on a task.
func applyPage(t *task.Task, page task.Page) {
	t.Plan = page
	t.Pending = page.Questions
	t.Remember(page.Questions)
	t.Status = statusOf(t)
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

	client, err := s.visionClient()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	maxEdge := s.cfg.Snapshot().Vision.ImageMaxEdge

	sse, err := newSSE(w)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer sse.close()

	// Model calls dominate the cost of this pass and each image is independent,
	// so start every valid reference image at once. Results are buffered and
	// consumed below by this request goroutine, keeping SSE writes and task-file
	// updates serial even though the upstream vision requests run concurrently.
	type analysisResult struct {
		label string
		facts task.Facts
	}
	results := make(chan analysisResult, len(t.Images))
	pending := 0

	for _, img := range t.Images {
		path, err := s.imagePath(t, img)
		if err != nil {
			facts := task.Facts{Label: img.Label, Error: err.Error(), AnalyzedAt: time.Now()}
			label := img.Label
			if _, uerr := s.store.Update(id, func(t *task.Task) error {
				if t.Facts == nil {
					t.Facts = map[string]task.Facts{}
				}
				t.Facts[label] = facts
				return nil
			}); uerr != nil {
				sse.send("image", map[string]any{"label": label, "error": uerr.Error()})
				continue
			}
			sse.send("image", map[string]any{"label": label, "error": err.Error(), "facts": facts})
			continue
		}

		sse.send("progress", map[string]any{"label": img.Label, "state": "analyzing"})
		pending++
		go func(label, path string) {
			facts, err := vision.Analyze(r.Context(), client, path, label, maxEdge)
			if err != nil {
				facts.Error = err.Error()
			}
			results <- analysisResult{label: label, facts: facts}
		}(img.Label, path)
	}

	for range pending {
		result := <-results
		if _, uerr := s.store.Update(id, func(t *task.Task) error {
			if t.Facts == nil {
				t.Facts = map[string]task.Facts{}
			}
			t.Facts[result.label] = result.facts
			return nil
		}); uerr != nil {
			sse.send("image", map[string]any{"label": result.label, "error": uerr.Error()})
			continue
		}
		sse.send("image", map[string]any{"label": result.label, "facts": result.facts})
	}

	final, err := s.store.Update(id, func(t *task.Task) error {
		refreshPlanAfterVision(t)
		return nil
	})
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
	if pending := visionBlockers(t); len(pending) > 0 {
		writeError(w, http.StatusBadRequest, visionGateMessage(pending))
		return
	}
	if missing := blockers(t); len(missing) > 0 {
		writeError(w, http.StatusBadRequest, "还有必答项没回答: "+strings.Join(missing, "、"))
		return
	}

	client, err := s.writerClient()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	sse, err := newSSE(w)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer sse.close()

	cfg := s.cfg.Snapshot()
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

		out, err := client.Stream(r.Context(), msgs, llm.Options{}, func(delta llm.Delta) {
			if delta.Reasoning != "" {
				sse.send("reasoning", map[string]any{"text": delta.Reasoning, "round": round})
			}
			if delta.Content != "" {
				sse.send("delta", map[string]any{"text": delta.Content, "round": round})
			}
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

func (s *Server) visionClient() (*llm.Client, error) {
	ep, err := s.cfg.VisionEndpoint()
	if err != nil {
		return nil, fmt.Errorf("视觉模型没配好: %w", err)
	}
	return clientFor(ep), nil
}

func (s *Server) writerClient() (*llm.Client, error) {
	ep, err := s.cfg.WriterEndpoint()
	if err != nil {
		return nil, fmt.Errorf("写作模型没配好: %w", err)
	}
	return clientFor(ep), nil
}

func clientFor(ep config.Endpoint) *llm.Client {
	return llm.New(ep.BaseURL, ep.APIKey, ep.Model, ep.MaxTokens, ep.Temperature, ep.ReasoningEffort)
}

func (s *Server) specFor(t *task.Task) validate.Spec {
	spec := t.Constraints.Spec(s.cfg.Snapshot().StrictEnglish)
	spec.Dialogue = slots.DialogueTexts(t)
	return spec
}

type taskView struct {
	*task.Task
	Questions     []task.Question `json:"questions"`
	Missing       []string        `json:"missing"`
	VisionPending []string        `json:"visionPending"`
	Answered      int             `json:"answeredCount"`
	Total         int             `json:"requiredCount"`
	Blocking      int             `json:"blockingFindings"`
	// Notices are the things the user still has to do inside ComfyUI itself
	// before the generated prompt can actually be run.
	Notices []comfyNotice `json:"comfyNotices"`
}

// comfyNotice is one reference asset the workflow does not currently feed.
type comfyNotice struct {
	Label string `json:"label"`
	Slot  string `json:"slot"`
	File  string `json:"file"`
	Kind  string `json:"kind"` // replace | add | missing
	Text  string `json:"text"`
}

// comfyNotices lists what has to be uploaded or rewired in ComfyUI. This tool
// never writes into ComfyUI's own folders, so an image picked here is only a
// stand-in until the user loads it there too.
func comfyNotices(t *task.Task) []comfyNotice {
	var out []comfyNotice
	for _, img := range t.Images {
		switch {
		case img.Origin == task.OriginUpload && img.Wired:
			text := fmt.Sprintf("%s 用的是你在这里上传的 %s，工作流的 %s 输入接的还是别的图。到 ComfyUI 里把这张图传给对应的 LoadImage 节点。",
				img.Label, img.File, img.Slot)
			if img.Replaces != "" {
				text = fmt.Sprintf("%s 用的是你在这里上传的 %s，工作流的 %s 输入接的仍然是 %s。到 ComfyUI 里把这张图传给对应的 LoadImage 节点。",
					img.Label, img.File, img.Slot, img.Replaces)
			}
			out = append(out, comfyNotice{Label: img.Label, Slot: img.Slot, File: img.File, Kind: "replace", Text: text})
		case img.Origin == task.OriginUpload:
			out = append(out, comfyNotice{
				Label: img.Label, Slot: img.Slot, File: img.File, Kind: "add",
				Text: fmt.Sprintf("%s 是这里额外加的参考图，工作流里没有对应的输入。到 ComfyUI 里加一个参考图输入、上传 %s 并接好，否则提示词里的 %s 没有对应资产。",
					img.Label, img.File, img.Label),
			})
		case img.Missing && img.Source != "":
			out = append(out, comfyNotice{
				Label: img.Label, Slot: img.Slot, File: img.Source, Kind: "missing",
				Text: fmt.Sprintf("%s 指向 %s，但 ComfyUI 的 input 目录里找不到这个文件。到 ComfyUI 里重新上传它。", img.Label, img.Source),
			})
		}
	}
	return out
}

// blockers lists what still stops a prompt from being written.
func blockers(t *task.Task) []string {
	var out []string
	if strings.TrimSpace(t.AnswerByRole(task.RoleBrief)) == "" &&
		strings.TrimSpace(t.Answer(slots.Brief)) == "" {
		out = append(out, "还没有说明这条视频要发生什么")
	}
	return append(out, slots.Unanswered(t)...)
}

// visionBlockers lists reference images that cannot yet provide context to the
// interview. A task without reference images needs no vision pass.
func visionBlockers(t *task.Task) []string {
	var out []string
	for _, img := range t.Images {
		facts, ok := t.Facts[img.Label]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s 尚未完成视觉分析", img.Label))
		case strings.TrimSpace(facts.Error) != "":
			out = append(out, fmt.Sprintf("%s 视觉分析失败：%s", img.Label, facts.Error))
		}
	}
	return out
}

func visionGateMessage(pending []string) string {
	return "进入第三步前必须完成所有参考图的视觉分析: " + strings.Join(pending, "；")
}

// refreshPlanAfterVision makes sure the next interview page is derived from
// the facts that were just written. The opening brief page is rebuilt locally
// so it can carry a vision-based suggestion; later pages are cleared and will
// be planned by the question model after the wizard enters step three.
func refreshPlanAfterVision(t *task.Task) {
	if len(visionBlockers(t)) > 0 {
		return
	}
	t.PlanError = ""
	if strings.TrimSpace(t.AnswerByRole(task.RoleBrief)) == "" &&
		strings.TrimSpace(t.Answer(slots.Brief)) == "" {
		if page, err := questions.Next(context.Background(), nil, t); err == nil {
			applyPage(t, page)
		}
		return
	}
	t.Pending = nil
	t.Plan.Questions = nil
	t.Plan.Done = false
	t.Plan.Note = ""
	t.Status = statusOf(t)
}

func (s *Server) taskView(t *task.Task) taskView {
	normalize(t)
	// Old tasks (or direct API callers) may already point beyond the image
	// step. Present them at the gate until every current image has usable facts.
	if pending := visionBlockers(t); len(pending) > 0 &&
		(t.Step == task.StepQuestions || t.Step == task.StepGenerate) {
		t.Step = task.StepImages
	}
	answered, total := slots.Progress(t)
	blocking := 0
	for _, f := range t.Findings {
		if f.Severity == validate.SeverityError {
			blocking++
		}
	}
	return taskView{
		Task:          t,
		Questions:     nonNil(t.Pending),
		Missing:       nonNil(blockers(t)),
		VisionPending: nonNil(visionBlockers(t)),
		Answered:      answered,
		Total:         total,
		Blocking:      blocking,
		Notices:       nonNil(comfyNotices(t)),
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
	t.Asked = nonNil(t.Asked)
	t.Plan.Questions = nonNil(t.Plan.Questions)
	t.Constraints.PictureLabels = nonNil(t.Constraints.PictureLabels)
	t.Constraints.VideoLabels = nonNil(t.Constraints.VideoLabels)
	t.Constraints.AudioLabels = nonNil(t.Constraints.AudioLabels)
	t.Constraints.Notes = nonNil(t.Constraints.Notes)
	if t.Step == "" {
		t.Step = task.StepConstraints
	}
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
		Step:         task.StepConstraints,
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
			Origin:  task.OriginWorkflow,
			Wired:   true,
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
