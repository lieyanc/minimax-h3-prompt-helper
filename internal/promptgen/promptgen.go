// Package promptgen assembles the messages sent to the writing model: the
// skill guide, the hard constraints read from the ComfyUI workflow, the vision
// facts, and the user's slot answers.
package promptgen

import (
	"fmt"
	"strings"

	"h3helper/internal/llm"
	"h3helper/internal/skill"
	"h3helper/internal/slots"
	"h3helper/internal/task"
	"h3helper/internal/validate"
)

// ReferenceImage is an original task image prepared as an inline data URL for
// the multimodal writer. Label text is placed immediately before each image so
// the model can reliably connect the bytes to <Picture 1>, <Picture 2>, etc.
type ReferenceImage struct {
	Label   string
	DataURL string
}

// BuildMultimodal returns a fresh generation request with every original
// reference image attached to the user's brief.
func BuildMultimodal(t *task.Task, images []ReferenceImage) []llm.Message {
	user := llm.Text("user", userPrompt(t))
	for _, img := range images {
		if strings.TrimSpace(img.DataURL) == "" {
			continue
		}
		label := strings.TrimSpace(img.Label)
		if label == "" {
			label = "reference image"
		}
		user.Parts = append(user.Parts,
			llm.Part{Text: fmt.Sprintf("Original reference image %s. Inspect these pixels directly; the vision fact sheet is only a summary.", label)},
			llm.Part{ImageURL: img.DataURL},
		)
	}
	return []llm.Message{
		llm.Text("system", systemPrompt(t)),
		user,
	}
}

// Repair appends a deterministic validation failure to an existing request.
// Keeping the caller's base messages is important for multimodal generation
// and revisions: the original images and the user's latest instruction remain
// visible during every automatic repair round.
func Repair(base []llm.Message, previous string, findings []validate.Finding) []llm.Message {
	var b strings.Builder
	b.WriteString("The prompt you produced failed the deterministic format check.\n\n")
	b.WriteString("--- your previous output ---\n")
	b.WriteString(previous)
	b.WriteString("\n--- end ---\n\n")
	b.WriteString("Problems that must be fixed:\n")
	for _, f := range findings {
		if f.Severity != validate.SeverityError {
			continue
		}
		b.WriteString("- ")
		b.WriteString(f.Message)
		if f.Detail != "" {
			b.WriteString(" — ")
			b.WriteString(f.Detail)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nRewrite the complete prompt with every problem fixed. Keep everything that was already correct. Output only the prompt.")

	msgs := append([]llm.Message{}, base...)
	msgs = append(msgs, llm.Text("assistant", previous), llm.Text("user", b.String()))
	return msgs
}

// BuildRevision reconstructs the writer conversation from the original brief,
// original images and completed revisions, then adds the user's new request.
// This lets requests such as "撤回刚才的改动" work without dropping the skill
// context or asking the user to repeat earlier decisions.
func BuildRevision(t *task.Task, images []ReferenceImage, request string) []llm.Message {
	msgs := BuildMultimodal(t, images)

	// The replay opens with the prompt as it stood before the first revision.
	// Only Revisions[0].PreviousPrompt records that moment: the attempt list
	// also holds the repair drafts of every round, so any attempt picked from
	// it would put the wrong version at the wrong point in the conversation. A
	// task written before revisions existed may not carry it, and then the
	// replay simply starts at the first revision request.
	last := ""
	if len(t.Revisions) > 0 {
		last = strings.TrimSpace(t.Revisions[0].PreviousPrompt)
	} else {
		last = strings.TrimSpace(t.Prompt)
	}
	if last != "" {
		msgs = append(msgs, llm.Text("assistant", last))
	}

	for _, revision := range t.Revisions {
		if strings.TrimSpace(revision.Request) == "" || strings.TrimSpace(revision.Prompt) == "" {
			continue
		}
		msgs = append(msgs,
			llm.Text("user", revisionPrompt(revision.Request)),
			llm.Text("assistant", revision.Prompt),
		)
		last = strings.TrimSpace(revision.Prompt)
	}

	// A prompt the user edited by hand is in no turn above, and neither is the
	// current prompt of a task whose revision history could not be replayed.
	switch current := strings.TrimSpace(t.Prompt); {
	case current == "" || current == last:
	case last == "":
		msgs = append(msgs, llm.Text("assistant", current))
	default:
		msgs = append(msgs,
			llm.Text("user", "I manually edited the prompt after the previous turn. Treat the next assistant message as the current complete prompt."),
			llm.Text("assistant", current),
		)
	}

	msgs = append(msgs, llm.Text("user", revisionPrompt(request)))
	return msgs
}

func revisionPrompt(request string) string {
	return `Revise the current complete H3 prompt according to the user's request below.

Keep every original workflow constraint, reference-label rule, verbatim line, and skill requirement that the request does not explicitly change. Re-inspect the attached original reference images when the requested change concerns visible content. Return the entire revised prompt, not a patch or an explanation. Output only the prompt.

User request:
` + strings.TrimSpace(request)
}

func systemPrompt(t *task.Task) string {
	var b strings.Builder
	b.WriteString("You are a MiniMax H3 prompt engineer. You rewrite a structured brief into a final H3 generation prompt.\n\n")
	b.WriteString("The original skill and every reference document it requires are included below. Follow them exactly. Field names, section order, labels and timing notation are not negotiable.\n\n")
	b.WriteString("===== BEGIN ORIGINAL SKILL.md =====\n")
	b.WriteString(skill.SkillMD())
	b.WriteString("\n===== END ORIGINAL SKILL.md =====\n\n")
	b.WriteString(fmt.Sprintf("===== BEGIN REQUIRED REFERENCE GUIDES (%s) =====\n", t.Constraints.Mode))
	b.WriteString(skill.FullGuideFor(t.Constraints.Mode))
	b.WriteString("\n===== END REQUIRED REFERENCE GUIDES =====\n\n")

	b.WriteString("Output rules:\n")
	b.WriteString("- Output ONLY the final prompt. No markdown fences, no headings, no explanation, no translation.\n")
	fields := skill.Fields(t.Constraints.Mode)
	b.WriteString(fmt.Sprintf("- Emit exactly these sections, in this order, each starting at the beginning of a line with `name:`: %s.\n", strings.Join(fields, ", ")))
	b.WriteString("- Write every section in English. Keep dialogue, lyrics and on-screen text in their original language.\n")
	b.WriteString("- Use concrete visual and audio detail. Never write cinematic, beautiful, epic, atmospheric or other abstract quality words.\n")
	b.WriteString("- Reproduce user-supplied dialogue verbatim inside <d>[Language] ...</d>. Do not translate, shorten or rephrase a single word.\n")
	return b.String()
}

func userPrompt(t *task.Task) string {
	c := t.Constraints
	var b strings.Builder

	b.WriteString("# Hard constraints from the ComfyUI workflow\n")
	b.WriteString(fmt.Sprintf("- Input mode: %s\n", c.Mode))
	if c.Frames > 0 {
		b.WriteString(fmt.Sprintf("- Effective duration: %.2f seconds (%d frames at %.0f fps). Every cut timestamp must be strictly increasing and strictly below %.2f.\n",
			c.Duration, c.Frames, c.FPS, c.Duration))
	}
	if c.Width > 0 && c.Height > 0 {
		b.WriteString(fmt.Sprintf("- Canvas: %dx%d (%s). Compose for this frame shape.\n", c.Width, c.Height, c.AspectLabel))
	}
	b.WriteString(labelBlock(c))
	if n := shotCount(t); n != "" {
		b.WriteString(fmt.Sprintf("- Shot count: %s. Do not add more shots than this.\n", n))
	}
	for _, note := range c.Notes {
		b.WriteString("- Note: " + note + "\n")
	}

	switch c.Mode {
	case "I2VA":
		b.WriteString("- The first line of your output must be exactly: For the target video, at 0.00 seconds into the target video, <Picture 1> (from [Shot 1]) is fully referenced.\n")
	case "FL2VA":
		b.WriteString(fmt.Sprintf("- The first line of your output must be: How the reference pictures align with the target video — Picture 1 (from Shot 1) aligns with the 0.00-second mark of the target video; Picture 2 (from Shot N) aligns with the %.2f-second mark of the target video. Replace N with the actual final shot number.\n", c.Duration))
	case "L2VA":
		b.WriteString(fmt.Sprintf("- The first line of your output must be: How the reference pictures align with the target video — <Picture 1> (from [Shot N]) aligns with the %.2f-second mark of the target video. Replace N with the actual final shot number.\n", c.Duration))
	}

	if facts := factsBlock(t); facts != "" {
		b.WriteString("\n# What the vision model actually sees in the reference images\n")
		b.WriteString(facts)
	}

	b.WriteString("\n# The user's brief\n")
	for _, f := range slots.Collect(t) {
		if f.Role == task.RoleShots {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", answerLabel(f), f.Value))
	}

	if lines := slots.DialogueTexts(t); len(lines) > 0 {
		b.WriteString("\n# Dialogue that must appear verbatim inside <d> blocks\n")
		for _, l := range lines {
			b.WriteString("- " + l + "\n")
		}
		if dialogueMode(t) == "voiceover" {
			b.WriteString("These lines are voice-over: use the exact phrase `says in an off-screen voiceover` and state right after each </d> that the character's lips remain completely closed.\n")
		}
	}

	if musicChoice(t) == "none" {
		b.WriteString("\nSet non_diegetic_music to exactly: N/A\n")
	}

	b.WriteString("\nNow write the final prompt.")
	return b.String()
}

// shotCount reads the shot count whatever slot key the question agent gave it.
func shotCount(t *task.Task) string {
	if v := strings.TrimSpace(t.AnswerByRole(task.RoleShots)); v != "" {
		return v
	}
	return strings.TrimSpace(t.Answer(slots.Shots))
}

func dialogueMode(t *task.Task) string {
	if v := strings.TrimSpace(t.AnswerByRole(task.RoleDialogueMode)); v != "" {
		return v
	}
	return strings.TrimSpace(t.Answer(slots.DialogueMode))
}

func musicChoice(t *task.Task) string {
	if v := strings.TrimSpace(t.AnswerByRole(task.RoleMusic)); v != "" {
		return v
	}
	return strings.TrimSpace(t.Answer(slots.Music))
}

func labelBlock(c task.Constraints) string {
	var b strings.Builder
	all := append([]string{}, c.PictureLabels...)
	all = append(all, c.VideoLabels...)
	all = append(all, c.AudioLabels...)
	if len(all) == 0 {
		b.WriteString("- No reference assets are wired into this workflow. Do not use any <Picture>, <Video> or <Audio> label.\n")
		return b.String()
	}
	b.WriteString(fmt.Sprintf("- The workflow wires exactly these reference labels: %s. Never invent a label outside this list, and use each of them at least once.\n", strings.Join(all, ", ")))
	return b.String()
}

func factsBlock(t *task.Task) string {
	var b strings.Builder
	for _, img := range t.Images {
		f, ok := t.Facts[img.Label]
		if !ok {
			continue
		}
		b.WriteString(fmt.Sprintf("\n## %s (%s)\n", img.Label, roleWords(img.Role)))
		if f.Summary != "" {
			b.WriteString("- Frame: " + f.Summary + "\n")
		}
		if f.Style != "" {
			b.WriteString("- Style: " + f.Style + "\n")
		}
		for _, s := range f.Subjects {
			b.WriteString(fmt.Sprintf("- Subject: %s (%s) — %s; %s\n", s.Name, s.Kind, s.Appearance, s.Position))
		}
		if f.Environment != "" {
			b.WriteString("- Environment: " + f.Environment + "\n")
		}
		if f.Lighting != "" {
			b.WriteString("- Lighting: " + f.Lighting + "\n")
		}
		if f.Composition != "" {
			b.WriteString("- Composition: " + f.Composition + "\n")
		}
		if f.ShotSize != "" {
			b.WriteString("- Shot size: " + f.ShotSize + "\n")
		}
		if len(f.VisibleText) > 0 {
			b.WriteString("- Visible text (keep verbatim, in double quotes): " + strings.Join(f.VisibleText, " | ") + "\n")
		}
		if f.ColorPalette != "" {
			b.WriteString("- Palette: " + f.ColorPalette + "\n")
		}
	}
	return b.String()
}

func roleWords(role string) string {
	switch role {
	case "first_frame":
		return "first frame of the video"
	case "last_frame":
		return "last frame of the video"
	}
	return "reference asset"
}

// answerLabel names an answer in the brief. The question agent supplies an
// English label with every question it writes; the fixed table and anything
// older fall back to the role table below.
func answerLabel(f slots.Filled) string {
	if f.EnLabel != "" {
		return f.EnLabel
	}
	switch f.Role {
	case task.RoleBrief:
		return "Intent"
	case task.RoleStyle:
		return "Visual style"
	case task.RoleAction:
		return "Action path (start → middle → end)"
	case task.RoleCamera:
		return "Camera motion"
	case task.RoleCameraDynamic:
		return "Camera amplitude and speed"
	case task.RoleDialogueMode:
		return "Dialogue delivery"
	case task.RoleScreenText:
		return "On-screen text (keep verbatim in double quotes)"
	case task.RoleSoundscape:
		return "Ambient and physical sound"
	case task.RoleMusic:
		return "Non-diegetic music wanted"
	case task.RoleMusicDesc:
		return "Non-diegetic music description"
	case task.RoleEnding:
		return "Ending state"
	case task.RoleImageRole:
		return "Role of " + f.Label
	case task.RoleImageRetention:
		return "Retention marker for " + f.Label
	}
	return slotLabel(f.Slot, f.Title)
}

// slotLabel maps the fixed table's slot keys to the English labels used in the
// brief. Tasks written before the question agent existed still carry them.
func slotLabel(slot, fallback string) string {
	switch slot {
	case slots.Brief:
		return "Intent"
	case slots.Style:
		return "Visual style"
	case slots.Action:
		return "Action path (start → middle → end)"
	case slots.CameraMotion:
		return "Camera motion"
	case slots.CameraDynamic:
		return "Camera amplitude and speed"
	case slots.HasDialogue:
		return "Has dialogue"
	case slots.DialogueMode:
		return "Dialogue delivery"
	case slots.ScreenText:
		return "On-screen text (keep verbatim in double quotes)"
	case slots.Soundscape:
		return "Ambient and physical sound"
	case slots.Music:
		return "Non-diegetic music wanted"
	case slots.MusicDesc:
		return "Non-diegetic music description"
	case slots.Ending:
		return "Ending state"
	}
	if strings.HasPrefix(slot, slots.ImageRolePrefix) {
		return "Role of " + strings.TrimPrefix(slot, slots.ImageRolePrefix)
	}
	if strings.HasPrefix(slot, slots.ImageRetentionPrefix) {
		return "Retention marker for " + strings.TrimPrefix(slot, slots.ImageRetentionPrefix)
	}
	return fallback
}
