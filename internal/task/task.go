// Package task holds the domain types shared by the question engine, the
// prompt writer and the HTTP layer, plus the JSON-file task store.
package task

import (
	"strings"
	"time"

	"h3helper/internal/validate"
)

// Task statuses.
const (
	StatusDraft       = "draft"
	StatusQuestioning = "questioning"
	StatusReady       = "ready"
	StatusDone        = "done"
	StatusError       = "error"
)

// Wizard steps. The task page walks these one at a time instead of showing
// every block at once.
const (
	StepConstraints = "constraints"
	StepImages      = "images"
	StepQuestions   = "questions"
	StepGenerate    = "generate"
)

// Steps lists the wizard steps in order.
var Steps = []string{StepConstraints, StepImages, StepQuestions, StepGenerate}

// Image origins.
const (
	OriginWorkflow = "workflow"
	OriginUpload   = "upload"
)

// Constraints are the hard limits derived from the ComfyUI workflow. They are
// injected into the writing prompt and enforced again by the validator.
type Constraints struct {
	Mode     string  `json:"mode"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Frames   int     `json:"frames"`
	FPS      float64 `json:"fps"`
	Duration float64 `json:"duration"`

	AspectLabel string `json:"aspectLabel"`

	PictureLabels []string `json:"pictureLabels"`
	VideoLabels   []string `json:"videoLabels"`
	AudioLabels   []string `json:"audioLabels"`

	// MaxShots caps the number of shots, derived from the duration.
	MaxShots int `json:"maxShots"`

	Notes []string `json:"notes"`
}

// Spec converts the constraints into the validator's expectations.
func (c Constraints) Spec(strictEnglish bool) validate.Spec {
	return validate.Spec{
		Mode:          c.Mode,
		Duration:      c.Duration,
		PictureLabels: c.PictureLabels,
		VideoLabels:   c.VideoLabels,
		AudioLabels:   c.AudioLabels,
		StrictEnglish: strictEnglish,
	}
}

// Image is one reference image attached to a task.
type Image struct {
	Label   string `json:"label"`
	Slot    string `json:"slot"`
	Role    string `json:"role"`
	Source  string `json:"source"`
	Origin  string `json:"origin"` // workflow | upload
	Missing bool   `json:"missing"`

	// File is the stored name under the task's upload directory. Only manually
	// uploaded images have one; workflow images are read from ComfyUI's input
	// directory through Source.
	File string `json:"file,omitempty"`
	// Wired reports whether the ComfyUI workflow actually feeds this label. An
	// upload that stands in for a wired slot stays wired; one that adds a label
	// beyond what the workflow connects does not, and has to be wired by hand
	// in ComfyUI before the prompt can be run.
	Wired bool `json:"wired"`
	// Replaces is the workflow filename this upload stands in for, kept so the
	// original can be restored when the upload is removed.
	Replaces string `json:"replaces,omitempty"`
}

// NeedsComfyUpload reports whether the user still has to load this image into
// ComfyUI by hand before the generated prompt can actually be run.
func (i Image) NeedsComfyUpload() bool {
	return i.Origin == OriginUpload || (i.Missing && i.Source != "")
}

// FactSubject is one identifiable thing the vision model found in an image.
type FactSubject struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Appearance string `json:"appearance"`
	Position   string `json:"position"`
}

// Facts is the structured reading of a single reference image.
type Facts struct {
	Label          string        `json:"label"`
	Style          string        `json:"style"`
	Summary        string        `json:"summary"`
	Subjects       []FactSubject `json:"subjects"`
	Environment    string        `json:"environment"`
	Lighting       string        `json:"lighting"`
	Composition    string        `json:"composition"`
	ShotSize       string        `json:"shotSize"`
	VisibleText    []string      `json:"visibleText"`
	ColorPalette   string        `json:"colorPalette"`
	PossibleAction string        `json:"possibleAction"`
	Error          string        `json:"error,omitempty"`
	AnalyzedAt     time.Time     `json:"analyzedAt"`
}

// Option is one selectable answer.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Desc  string `json:"desc,omitempty"`
}

// Question kinds.
const (
	KindText        = "text"
	KindTextarea    = "textarea"
	KindChoice      = "choice"
	KindMultiChoice = "multichoice"
)

// Roles tie a question to the machine-readable use of its answer. The wording,
// the options and the order of the questions are written by the question agent,
// but anything the validator or the writing prompt has to read back needs a
// stable handle, and that handle is the role.
const (
	RoleBrief          = "brief"
	RoleAction         = "action"
	RoleStyle          = "style"
	RoleShots          = "shots"
	RoleCamera         = "camera"
	RoleCameraDynamic  = "camera_dynamic"
	RoleDialogue       = "dialogue"
	RoleDialogueMode   = "dialogue_mode"
	RoleScreenText     = "screen_text"
	RoleSoundscape     = "soundscape"
	RoleMusic          = "music"
	RoleMusicDesc      = "music_desc"
	RoleEnding         = "ending"
	RoleImageRole      = "image_role"
	RoleImageRetention = "image_retention"
	RoleFreeform       = "" // ordinary detail that only feeds the brief
)

// Roles is the set a generated question may claim.
var Roles = []string{
	RoleBrief, RoleAction, RoleStyle, RoleShots, RoleCamera, RoleCameraDynamic,
	RoleDialogue, RoleDialogueMode, RoleScreenText, RoleSoundscape,
	RoleMusic, RoleMusicDesc, RoleEnding, RoleImageRole, RoleImageRetention,
}

// Question is one thing the user still has to decide.
type Question struct {
	Slot        string   `json:"slot"`
	Group       string   `json:"group"`
	Title       string   `json:"title"`
	Help        string   `json:"help,omitempty"`
	Kind        string   `json:"kind"`
	Options     []Option `json:"options,omitempty"`
	Suggestion  string   `json:"suggestion,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Required    bool     `json:"required"`
	// AllowFree lets a choice question accept a free-text answer too.
	AllowFree bool `json:"allowFree,omitempty"`

	// Role names how the answer is consumed. Empty means it only enriches the
	// brief handed to the writing model.
	Role string `json:"role,omitempty"`
	// Label is the reference label the question is about, e.g. "<Picture 2>".
	Label string `json:"label,omitempty"`
	// Why is the one-line reason the H3 format needs this answered.
	Why string `json:"why,omitempty"`
	// EnLabel is the English name this answer carries into the writing prompt.
	EnLabel string `json:"enLabel,omitempty"`
	// Vocab names the controlled vocabulary the options were taken from, so the
	// server can replace them with the canonical list.
	Vocab string `json:"vocab,omitempty"`
}

// Page is one screen of questions produced by the question agent.
type Page struct {
	Index     int        `json:"index"`
	Title     string     `json:"title"`
	Intro     string     `json:"intro,omitempty"`
	Questions []Question `json:"questions"`
	// Remaining is the agent's estimate of how many pages are still to come.
	Remaining int `json:"remaining"`
	// Done is set when the agent reports it has everything the format needs.
	Done bool `json:"done"`
	// Note carries the agent's closing remark, shown when Done.
	Note string `json:"note,omitempty"`
	// Fallback is set when the deterministic slot table produced this page
	// because the agent was unreachable or answered with nothing usable.
	Fallback bool `json:"fallback,omitempty"`
}

// Round records one batch of questions and the answers given.
type Round struct {
	Index     int               `json:"index"`
	Questions []Question        `json:"questions"`
	Answers   map[string]string `json:"answers"`
	At        time.Time         `json:"at"`
}

// Attempt is one generation pass plus its validation result.
type Attempt struct {
	Index    int                `json:"index"`
	Prompt   string             `json:"prompt"`
	Findings []validate.Finding `json:"findings"`
	Repaired bool               `json:"repaired"`
	At       time.Time          `json:"at"`
}

// Task is one prompt-writing session, persisted as a single JSON file.
type Task struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	WorkflowFile string `json:"workflowFile"`
	WorkflowName string `json:"workflowName"`
	VariantID    string `json:"variantId"`
	VariantNode  int    `json:"variantNode"`

	Constraints Constraints `json:"constraints"`

	Images []Image          `json:"images"`
	Facts  map[string]Facts `json:"facts"`

	Brief   string            `json:"brief"`
	Answers map[string]string `json:"answers"`
	Rounds  []Round           `json:"rounds"`
	Pending []Question        `json:"pending"`

	// Step is where the wizard stands: constraints | images | questions |
	// generate.
	Step string `json:"step"`
	// Asked keeps every question that was ever put to the user, in the order it
	// was asked, so an answer can always be shown with the wording that
	// produced it even after the agent has moved on.
	Asked []Question `json:"asked"`
	// Plan is the current question page, as the agent handed it over.
	Plan Page `json:"plan"`
	// PlanError records why the last planning call fell back to the fixed slot
	// table, if it did.
	PlanError string `json:"planError,omitempty"`

	Prompt      string             `json:"prompt"`
	Findings    []validate.Finding `json:"findings"`
	Attempts    []Attempt          `json:"attempts"`
	GeneratedAt time.Time          `json:"generatedAt"`

	Error string `json:"error,omitempty"`
}

// Summary is the compact form used by the task list.
type Summary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	Mode         string    `json:"mode"`
	WorkflowName string    `json:"workflowName"`
	Duration     float64   `json:"duration"`
	Images       int       `json:"images"`
	Answered     int       `json:"answered"`
	Findings     int       `json:"findings"`
	HasPrompt    bool      `json:"hasPrompt"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Summarize builds the list view of a task.
func (t *Task) Summarize() Summary {
	blocking := 0
	for _, f := range t.Findings {
		if f.Severity == validate.SeverityError {
			blocking++
		}
	}
	return Summary{
		ID:           t.ID,
		Title:        t.Title,
		Status:       t.Status,
		Mode:         t.Constraints.Mode,
		WorkflowName: t.WorkflowName,
		Duration:     t.Constraints.Duration,
		Images:       len(t.Images),
		Answered:     t.AnsweredCount(),
		Findings:     blocking,
		HasPrompt:    t.Prompt != "",
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}

// Answer returns a trimmed answer for a slot.
func (t *Task) Answer(slot string) string {
	if t.Answers == nil {
		return ""
	}
	return t.Answers[slot]
}

// AnswerByRole returns the answer of the most recently asked question claiming
// a role. Roles are how the validator and the writing prompt find an answer
// whose slot key the question agent invented.
func (t *Task) AnswerByRole(role string) string {
	if role == "" {
		return ""
	}
	for i := len(t.Asked) - 1; i >= 0; i-- {
		q := t.Asked[i]
		if q.Role != role {
			continue
		}
		if v := strings.TrimSpace(t.Answer(q.Slot)); v != "" {
			return v
		}
	}
	return ""
}

// QuestionFor returns the question a slot was asked with.
func (t *Task) QuestionFor(slot string) (Question, bool) {
	for i := len(t.Asked) - 1; i >= 0; i-- {
		if t.Asked[i].Slot == slot {
			return t.Asked[i], true
		}
	}
	return Question{}, false
}

// Remember records the questions of a page, replacing any earlier wording of
// the same slot so the list never grows a duplicate.
func (t *Task) Remember(questions []Question) {
	for _, q := range questions {
		replaced := false
		for i := range t.Asked {
			if t.Asked[i].Slot == q.Slot {
				t.Asked[i] = q
				replaced = true
				break
			}
		}
		if !replaced {
			t.Asked = append(t.Asked, q)
		}
	}
}

// AnsweredCount counts the questions that carry a non-empty answer.
func (t *Task) AnsweredCount() int {
	n := 0
	for _, v := range t.Answers {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}

// ImageByLabel finds an attached image by its reference label.
func (t *Task) ImageByLabel(label string) (Image, bool) {
	for _, img := range t.Images {
		if img.Label == label {
			return img, true
		}
	}
	return Image{}, false
}
