// Package questions runs the interview stage. The H3 guide pins down what has
// to be settled before a prompt can be written, but not what to ask about a
// particular set of reference assets: two pictures may be a character and a
// location, two characters, or a location and a character, and each pairing
// needs different questions. So the guide, the workflow constraints and the
// vision facts go to a model, and it writes the next page of the interview.
//
// Everything the validator or the writing prompt has to read back is pinned to
// a role (see task.Roles), and every controlled vocabulary is replaced with the
// canonical list after the model answers, so a creative interviewer can still
// only produce answers the format accepts.
package questions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"h3helper/internal/llm"
	"h3helper/internal/skill"
	"h3helper/internal/slots"
	"h3helper/internal/task"
)

// MaxQuestions is the most questions one page may carry.
const MaxQuestions = 3

// maxPages stops an interview that will not end on its own.
const maxPages = 12

// Next returns the next page of questions. The opening intent question is
// deterministic — the agent has nothing to reason about before it — and every
// page after that is written by the model.
func Next(ctx context.Context, client *llm.Client, t *task.Task) (task.Page, error) {
	index := len(t.Rounds) + 1

	if strings.TrimSpace(t.AnswerByRole(task.RoleBrief)) == "" &&
		strings.TrimSpace(t.Answer(slots.Brief)) == "" {
		return task.Page{
			Index:     index,
			Title:     "你要拍什么",
			Intro:     "先说清楚意图，后面的问题由模型按 H3 规范和参考图内容自己决定。",
			Questions: []task.Question{slots.BriefQuestion(t)},
			Remaining: 3,
		}, nil
	}

	if index > maxPages {
		return task.Page{
			Index:     index,
			Done:      true,
			Title:     "问得差不多了",
			Note:      fmt.Sprintf("已经问满 %d 轮，剩下的交给写作模型和校验器。", maxPages),
			Questions: []task.Question{},
		}, nil
	}

	if client == nil {
		return task.Page{}, fmt.Errorf("提问模型没配好")
	}

	msgs := []llm.Message{
		llm.Text("system", systemPrompt(t)),
		llm.Text("user", statePrompt(t)),
	}

	raw, err := client.Complete(ctx, msgs, llm.Options{JSONObject: true})
	if err != nil {
		// Some compatible servers reject response_format; retry without it.
		raw, err = client.Complete(ctx, msgs, llm.Options{})
		if err != nil {
			return task.Page{}, err
		}
	}

	page, err := parsePage(raw, t)
	if err != nil {
		return task.Page{}, err
	}
	page.Index = index
	return page, nil
}

// Fallback is the page the deterministic table would ask. It stands in when the
// agent cannot be reached, so the interview never dead-ends on a network error.
func Fallback(t *task.Task, reason string) task.Page {
	qs := slots.Next(t, MaxQuestions)
	page := task.Page{
		Index:     len(t.Rounds) + 1,
		Title:     "固定问题表",
		Questions: qs,
		Fallback:  true,
		Remaining: len(slots.MissingRequired(t)) / MaxQuestions,
	}
	if reason != "" {
		page.Intro = "提问模型没能给出问题（" + reason + "），这一轮回退到内置的固定问题表。"
	}
	if len(qs) == 0 {
		page.Done = true
		page.Questions = []task.Question{}
		page.Note = "固定问题表已经问完。"
	}
	return page
}

// ---------------------------------------------------------------- prompts --

func systemPrompt(t *task.Task) string {
	var b strings.Builder
	b.WriteString(`You are the interview stage of a MiniMax H3 prompt pipeline.

A ComfyUI workflow has already fixed the input mode, the canvas, the duration and which reference labels exist. A vision model has already read every reference image into a fact sheet. Your job is to decide what the user still has to be asked before an H3 prompt can be written for THIS setup, and to ask it one short page at a time.

`)
	b.WriteString("===== BEGIN SKILL =====\n")
	b.WriteString(skill.SkillMD())
	b.WriteString("\n===== END SKILL =====\n\n")
	b.WriteString(fmt.Sprintf("===== BEGIN FORMAT GUIDE (%s) =====\n", t.Constraints.Mode))
	b.WriteString(skill.GuideFor(t.Constraints.Mode))
	b.WriteString("\n===== END FORMAT GUIDE =====\n\n")

	b.WriteString(`How to interview:
- Ask only what the guide needs for this mode and this exact set of reference assets. Never ask for anything the workflow already fixes (mode, canvas, duration, frame count, which labels exist) or anything the vision facts already state.
- The reference images are not interchangeable and their meaning is not implied by their number. Read the facts first: a picture may be a character, a location, a costume, a prop, an interface, a storyboard or a style plate. Ask about what is actually in the image — never a generic "what is <Picture 1>". When several pictures are of the same kind, ask what distinguishes their roles (who is who, which one is the opening, what the second location is for).
- One page covers one topic, carries at most 3 questions, and comes after the decisions it depends on. Ask the choice that constrains the rest first.
- Write every user-facing string (title, help, why, option labels, option descriptions, placeholder, intro) in Chinese. Keep option VALUES in the exact English the guide uses when they come from a controlled vocabulary, and keep verbatim material (dialogue, on-screen text) in whatever language the user will type.
- Assume the user has no filmmaking knowledge. Ask concrete questions in everyday Chinese, explain what the result will look like, and never require the user to understand an English term or unexplained jargon. For visual style and camera movement, always ask a choice question with the corresponding controlled vocabulary instead of asking the user to invent a technical term.
- Pre-fill "suggestion" from the vision facts or from the answers so far whenever you honestly can, so the common case is "confirm and continue". For a choice question the suggestion has to be one of the option values.
- When a question is about something the user must supply verbatim (dialogue, lyrics, on-screen text), say so in "help": the validator diffs it word for word.
- Set "done": true with an empty question list once everything below is settled. Do not pad the interview.

Controlled vocabularies are not negotiable. When a question asks for one, set "vocab" and leave "options" empty — the server fills in the canonical list:
- "camera": the camera motion types of the guide
- "camera_dynamic": amplitude and speed
- "style": the visual styles the guide names
- "retention": the retention_analysis markers for visible content
- "audio_retention": the retention markers for <Audio N>
- "task_type": the summary task types of full-reference mode
- "shots": the shot counts this duration can carry

Set "role" when the answer has a fixed downstream use, and leave it empty otherwise:
`)
	b.WriteString(`- "brief" the overall intent | "action" the start → middle → end path | "style" visual style | "shots" the shot count | "camera" the main camera motion | "camera_dynamic" amplitude and speed
- "dialogue" the verbatim lines (one per line, "说话人 | 语言 | 原文"), "dialogue_mode" on-screen / voice-over / both
- "screen_text" text visible in frame, "soundscape" ambience and physical sound, "music" whether there is audience-only music, "music_desc" its instrumentation and dynamics
- "ending" the state the video has to land on
- "image_role" what a reference asset is for, "image_retention" how much of it must survive — both take "label" as well
`)

	b.WriteString("\nCoverage — the guide needs all of these settled before a prompt can be written. Skip whatever this mode, this workflow or the vision facts already settle:\n")
	for i, item := range coverage(t) {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, item))
	}

	b.WriteString(`
Reply with a single JSON object and nothing else — no markdown fences, no commentary:
{
  "done": false,
  "note": "shown to the user when done is true",
  "remaining": 2,
  "page": {
    "title": "短标题",
    "intro": "一句话说明这一页在定什么",
    "questions": [
      {
        "slot": "stable_snake_case_key",
        "role": "",
        "label": "<Picture 1>",
        "title": "问题",
        "help": "补充说明",
        "why": "规范里为什么需要它",
        "enLabel": "English label for the writing prompt",
        "kind": "text | textarea | choice | multichoice",
        "vocab": "",
        "required": true,
        "allowFree": false,
        "placeholder": "",
        "suggestion": "",
        "options": [{"value": "value", "label": "选项", "desc": "说明"}]
      }
    ]
  }
}`)
	return b.String()
}

// coverage lists what the guide needs settled, narrowed to the mode at hand.
func coverage(t *task.Task) []string {
	c := t.Constraints
	out := []string{
		"The intent and the action path: starting state → what happens → the state it lands on.",
		"The visual style that opens [Shot 1].",
		fmt.Sprintf("How many shots, and where the cuts fall inside %.2f seconds (at most %d shots).", c.Duration, c.MaxShots),
		"The main camera motion, plus amplitude and speed when they matter.",
		"Whether anyone speaks or sings. If so: the verbatim lines, their language, who says them (voice, age, on-screen or off), and whether any line crosses a cut.",
		"Any text visible in frame, verbatim.",
		"Ambient and physical sound for overall_soundscape.",
		"Whether there is audience-only music for non_diegetic_music, and its instrumentation, tempo and dynamics.",
	}
	switch c.Mode {
	case "I2VA":
		out = append(out, "How the first frame is picked up: what in <Picture 1> must stay fixed as the action starts.")
	case "FL2VA":
		out = append(out, "The movement path between the first and the last frame, and what has to match the last frame exactly at the end.")
	case "L2VA":
		out = append(out, "The plausible earlier state to open on, and how the shot converges onto the final frame.")
	case "Ref2VA":
		out = append(out,
			"For every reference asset: what it contributes (a subject's appearance, a location, a costume, a prop, a concrete keyframe, a storyboard, a style, a voice) and therefore whether it becomes a <Subject N> definition or stands alone.",
			"For every reference asset: its retention marker, i.e. how much of what it defines has to survive into the target video.",
			"The task-type prefix of summary, taken from the fixed vocabulary and matching what the assets actually do.",
			"Which subject each speaker is, so (S1), (S2) … can be assigned in order of the vocal events.")
	}
	if c.Mode != "T2VA" && c.Mode != "Ref2VA" {
		out = append(out, "The state the video ends on.")
	}
	return out
}

func statePrompt(t *task.Task) string {
	c := t.Constraints
	var b strings.Builder

	b.WriteString("# Fixed by the ComfyUI workflow\n")
	b.WriteString(fmt.Sprintf("- Input mode: %s\n", c.Mode))
	if c.Frames > 0 {
		b.WriteString(fmt.Sprintf("- Duration: %.2f s (%d frames at %.0f fps), so at most %d shots\n", c.Duration, c.Frames, c.FPS, c.MaxShots))
	}
	if c.Width > 0 && c.Height > 0 {
		b.WriteString(fmt.Sprintf("- Canvas: %dx%d (%s)\n", c.Width, c.Height, c.AspectLabel))
	}
	labels := append(append(append([]string{}, c.PictureLabels...), c.VideoLabels...), c.AudioLabels...)
	if len(labels) == 0 {
		b.WriteString("- No reference assets: the prompt may not use any <Picture>, <Video> or <Audio> label\n")
	} else {
		b.WriteString(fmt.Sprintf("- Reference labels wired in: %s\n", strings.Join(labels, ", ")))
	}
	for _, n := range c.Notes {
		b.WriteString("- Note: " + n + "\n")
	}

	if len(t.Images) > 0 {
		b.WriteString("\n# Reference images and what the vision model sees in them\n")
		for _, img := range t.Images {
			b.WriteString(fmt.Sprintf("\n## %s — workflow slot %q, role %q", img.Label, img.Slot, img.Role))
			if img.Origin == task.OriginUpload {
				b.WriteString(", uploaded by hand in this tool")
				if !img.Wired {
					b.WriteString(" and NOT wired into the workflow yet")
				}
			}
			b.WriteString("\n")
			f, ok := t.Facts[img.Label]
			if !ok {
				b.WriteString("- Not analysed yet. Do not assume what is in it; ask.\n")
				continue
			}
			if f.Error != "" {
				b.WriteString("- Analysis failed: " + f.Error + "\n")
				continue
			}
			writeFacts(&b, f)
		}
	}

	answered := slots.Collect(t)
	if len(answered) > 0 {
		b.WriteString("\n# Already answered — never ask any of these again\n")
		for _, f := range answered {
			role := f.Role
			if role == "" {
				role = "-"
			}
			b.WriteString(fmt.Sprintf("- [slot %s, role %s] %s → %s\n", f.Slot, role, f.Title, f.Value))
		}
	} else {
		b.WriteString("\n# Nothing answered yet\n")
	}

	if hint := fallbackHint(t); hint != "" {
		b.WriteString("\n# Topics the built-in fixed table would still cover (a hint, not an order)\n")
		b.WriteString(hint)
	}

	b.WriteString(fmt.Sprintf("\nThis is interview page %d. Decide what to ask now, or set done.\n", len(t.Rounds)+1))
	return b.String()
}

func writeFacts(b *strings.Builder, f task.Facts) {
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
		b.WriteString("- Visible text: " + strings.Join(f.VisibleText, " | ") + "\n")
	}
	if f.ColorPalette != "" {
		b.WriteString("- Palette: " + f.ColorPalette + "\n")
	}
	if f.PossibleAction != "" {
		b.WriteString("- Plausible action from this frame: " + f.PossibleAction + "\n")
	}
}

func fallbackHint(t *task.Task) string {
	var b strings.Builder
	for _, d := range slots.Definitions(t) {
		if d.Applies != nil && !d.Applies(t) {
			continue
		}
		if strings.TrimSpace(t.Answer(d.Slot)) != "" {
			continue
		}
		b.WriteString("- " + d.Title + "\n")
	}
	return b.String()
}

// ------------------------------------------------------------- validation --

type rawOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
}

type rawQuestion struct {
	Slot        string      `json:"slot"`
	Role        string      `json:"role"`
	Label       string      `json:"label"`
	Group       string      `json:"group"`
	Title       string      `json:"title"`
	Help        string      `json:"help"`
	Why         string      `json:"why"`
	EnLabel     string      `json:"enLabel"`
	Kind        string      `json:"kind"`
	Vocab       string      `json:"vocab"`
	Required    *bool       `json:"required"`
	AllowFree   bool        `json:"allowFree"`
	Placeholder string      `json:"placeholder"`
	Suggestion  string      `json:"suggestion"`
	Options     []rawOption `json:"options"`
}

type rawPage struct {
	Done      bool   `json:"done"`
	Note      string `json:"note"`
	Remaining int    `json:"remaining"`
	Page      struct {
		Title     string        `json:"title"`
		Intro     string        `json:"intro"`
		Questions []rawQuestion `json:"questions"`
	} `json:"page"`
	// Some models drop the questions straight at the top level.
	Questions []rawQuestion `json:"questions"`
	Title     string        `json:"title"`
	Intro     string        `json:"intro"`
}

func parsePage(raw string, t *task.Task) (task.Page, error) {
	body := llm.ExtractJSON(raw)
	if body == "" {
		return task.Page{}, fmt.Errorf("提问模型没有返回 JSON：%s", truncate(raw, 200))
	}
	var parsed rawPage
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return task.Page{}, fmt.Errorf("解析提问结果失败: %w", err)
	}

	page := task.Page{
		Done:      parsed.Done,
		Note:      strings.TrimSpace(parsed.Note),
		Remaining: clamp(parsed.Remaining, 0, 20),
		Title:     firstNonEmpty(parsed.Page.Title, parsed.Title),
		Intro:     firstNonEmpty(parsed.Page.Intro, parsed.Intro),
	}

	rawQs := parsed.Page.Questions
	if len(rawQs) == 0 {
		rawQs = parsed.Questions
	}

	seen := map[string]bool{}
	seenRole := map[string]bool{}
	for _, rq := range rawQs {
		q, ok := sanitize(rq, t, len(page.Questions))
		if !ok || seen[q.Slot] {
			continue
		}
		// One role answers one thing. An agent that splits amplitude and speed
		// into two questions would otherwise hand the writing model two values
		// for the same field, from a list that already covers both.
		if q.Role != task.RoleFreeform {
			key := q.Role + "\x00" + q.Label
			if seenRole[key] {
				continue
			}
			seenRole[key] = true
		}
		seen[q.Slot] = true
		page.Questions = append(page.Questions, q)
		if len(page.Questions) == MaxQuestions {
			break
		}
	}

	if len(page.Questions) == 0 {
		if page.Done {
			page.Questions = []task.Question{}
			if page.Title == "" {
				page.Title = "问完了"
			}
			return page, nil
		}
		return task.Page{}, fmt.Errorf("提问模型这一轮没有给出可用的问题")
	}
	if page.Title == "" {
		page.Title = page.Questions[0].Group
	}
	if page.Title == "" {
		page.Title = "继续追问"
	}
	return page, nil
}

func sanitize(rq rawQuestion, t *task.Task, index int) (task.Question, bool) {
	title := strings.TrimSpace(rq.Title)
	if title == "" {
		return task.Question{}, false
	}

	q := task.Question{
		Slot:        slotKey(rq, index),
		Group:       strings.TrimSpace(rq.Group),
		Title:       title,
		Help:        strings.TrimSpace(rq.Help),
		Why:         strings.TrimSpace(rq.Why),
		EnLabel:     strings.TrimSpace(rq.EnLabel),
		Label:       strings.TrimSpace(rq.Label),
		Kind:        kindOf(rq.Kind),
		Placeholder: strings.TrimSpace(rq.Placeholder),
		AllowFree:   rq.AllowFree,
		Required:    rq.Required == nil || *rq.Required,
		Role:        roleOf(rq.Role),
	}

	// Something already answered is never asked again.
	if strings.TrimSpace(t.Answer(q.Slot)) != "" {
		return task.Question{}, false
	}
	if q.Group == "" {
		q.Group = "追问"
	}

	if opts, free, ok := vocabulary(rq.Vocab, t); ok {
		q.Vocab = strings.TrimSpace(rq.Vocab)
		q.Options = opts
		q.Kind = task.KindChoice
		q.AllowFree = free
		applyBeginnerPresetCopy(&q)
	} else {
		for _, o := range rq.Options {
			label := strings.TrimSpace(o.Label)
			if label == "" {
				label = strings.TrimSpace(o.Value)
			}
			if label == "" {
				continue
			}
			q.Options = append(q.Options, task.Option{
				Value: strings.TrimSpace(o.Value),
				Label: label,
				Desc:  strings.TrimSpace(o.Desc),
			})
		}
	}

	if q.Kind == task.KindChoice || q.Kind == task.KindMultiChoice {
		if len(q.Options) == 0 {
			q.Kind = task.KindText
		}
		for _, o := range q.Options {
			// An option that answers with nothing cannot satisfy a required
			// question, so such a question is optional by construction.
			if o.Value == "" {
				q.Required = false
			}
		}
	}

	if s := strings.TrimSpace(rq.Suggestion); s != "" {
		if q.Kind != task.KindChoice || q.AllowFree || hasValue(q.Options, s) {
			q.Suggestion = s
		}
	}
	return q, true
}

// applyBeginnerPresetCopy makes the controlled visual presets understandable
// even when the question model returns a terse title or filmmaking jargon.
// The option values remain the guide's exact English vocabulary.
func applyBeginnerPresetCopy(q *task.Question) {
	switch strings.TrimSpace(strings.ToLower(q.Vocab)) {
	case "style", "styles":
		q.Group = "画面风格"
		q.Title = "你希望画面看起来像哪一种？"
		q.Help = "按你想要的成片感觉选择即可，不需要懂专业术语；有参考图时，一般选最接近参考图的风格。"
	case "camera", "camera_motion":
		q.Group = "镜头"
		q.Title = "你希望镜头怎么拍？"
		q.Help = "这里问的是摄影机怎么移动，不是人物怎么动。拿不准时可选“镜头固定不动”。"
	case "camera_dynamic", "amplitude", "speed":
		q.Group = "镜头"
		q.Title = "镜头移动得多快、多明显？"
		q.Help = "拿不准就选“默认”，模型会使用自然的运动幅度和速度。"
	}
}

// slotKey keeps generated keys stable and free of anything that would confuse
// a JSON map or a URL.
func slotKey(rq rawQuestion, index int) string {
	slot := strings.TrimSpace(rq.Slot)
	if slot == "" {
		slot = strings.TrimSpace(rq.Role)
	}
	if slot == "" {
		slot = fmt.Sprintf("q%d", index+1)
	}
	var b strings.Builder
	for _, r := range slot {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		case r == '_' || r == ':' || r == '-' || r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('_')
		default:
			// Reference labels carry angle brackets and digits; keep them
			// readable rather than dropping the label entirely.
			if r == '<' || r == '>' {
				b.WriteRune(r)
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return fmt.Sprintf("q%d", index+1)
	}
	return out
}

func kindOf(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case task.KindTextarea, "long", "multiline":
		return task.KindTextarea
	case task.KindChoice, "select", "radio", "single":
		return task.KindChoice
	case task.KindMultiChoice, "multi", "checkbox":
		return task.KindMultiChoice
	default:
		return task.KindText
	}
}

func roleOf(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	for _, known := range task.Roles {
		if role == known {
			return known
		}
	}
	return task.RoleFreeform
}

// vocabulary swaps a named controlled vocabulary in for whatever options the
// model wrote, so the guide's fixed words are the only ones that can be picked.
func vocabulary(name string, t *task.Task) (opts []task.Option, allowFree, ok bool) {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "camera", "camera_motion":
		return slots.CameraOptions(), true, true
	case "camera_dynamic", "amplitude", "speed":
		return []task.Option{
			{Value: "", Label: "默认（不写幅度和速度）"},
			{Value: "with small amplitude at slow speed", Label: "小幅度 + 慢速"},
			{Value: "with small amplitude", Label: "小幅度"},
			{Value: "with large amplitude at fast speed", Label: "大幅度 + 快速"},
			{Value: "at slow speed", Label: "慢速"},
			{Value: "at fast speed", Label: "快速"},
		}, false, true
	case "style", "styles":
		return slots.StyleOptions(), true, true
	case "retention", "retention_analysis", "retention_marker":
		return slots.RetentionOptions(), false, true
	case "audio_retention", "audio":
		return slots.AudioRetentionOptions(), false, true
	case "task_type", "task_types":
		return slots.TaskTypeOptions(), false, true
	case "shots", "shot_count":
		return slots.ShotOptions(t.Constraints), false, true
	}
	return nil, false, false
}

func hasValue(opts []task.Option, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
