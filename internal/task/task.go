// Package task holds the domain types shared by the question engine, the
// prompt writer and the HTTP layer, plus the JSON-file task store.
package task

import (
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
	Origin  string `json:"origin"` // workflow | picked
	Missing bool   `json:"missing"`
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

// Question is one slot the user still has to fill.
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
		Answered:     len(t.Answers),
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
