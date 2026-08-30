// Package validate checks a generated H3 prompt against the format rules in
// the h3-prompt-writing skill and against the constraints read from the
// ComfyUI workflow. Every check is deterministic; no model is involved.
package validate

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"h3helper/internal/skill"
)

// Severity levels. Errors block acceptance and trigger a repair round.
const (
	SeverityError = "error"
	SeverityWarn  = "warn"
)

// Finding is one rule violation.
type Finding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Detail   string `json:"detail,omitempty"`
	Line     int    `json:"line,omitempty"`
}

// Spec is what the prompt is checked against.
type Spec struct {
	Mode          string
	Duration      float64
	PictureLabels []string
	VideoLabels   []string
	AudioLabels   []string
	StrictEnglish bool
	// Dialogue lines the user supplied verbatim; each must survive into a <d>
	// block unchanged.
	Dialogue []string
}

var (
	fieldHeaderRe = regexp.MustCompile(`(?m)^[ \t]*([a-z_]+)[ \t]*:`)
	shotRe        = regexp.MustCompile(`\[Shot (\d+)\]`)
	timestampRe   = regexp.MustCompile(`At (\d{2}):(\d{2})\.(\d{3})`)
	labelRe       = regexp.MustCompile(`<(Picture|Video|Audio|Subject) (\d+)>`)
	speakerRe     = regexp.MustCompile(`\(S(\d+(?:\s*,\s*S\d+)*)\)`)
	dOpenRe       = regexp.MustCompile(`<d>`)
	dCloseRe      = regexp.MustCompile(`</d>`)
	dBlockRe      = regexp.MustCompile(`(?s)<d>(.*?)</d>`)
	langTagRe     = regexp.MustCompile(`^\s*\[[^\]]+\]`)
	quotedRe      = regexp.MustCompile(`"[^"\n]*"`)
	taskPrefixRe  = regexp.MustCompile(`^\s*\[([^\]]+)\]`)
	alignFLRe     = regexp.MustCompile(`aligns with the (\d+\.\d{2})-second mark`)
)

// Check runs every rule and returns the findings, most severe first.
func Check(prompt string, spec Spec) []Finding {
	var out []Finding
	body := strings.ReplaceAll(prompt, "\r\n", "\n")

	sections := splitSections(body, skill.Fields(spec.Mode))

	out = append(out, checkFields(body, spec, sections)...)
	out = append(out, checkInstruction(body, spec)...)
	out = append(out, checkShots(body, spec, sections)...)
	out = append(out, checkLabels(body, spec, sections)...)
	out = append(out, checkDialogue(body, spec)...)
	out = append(out, checkSpeakers(body, spec, sections)...)
	out = append(out, checkAudioSections(spec, sections)...)
	out = append(out, checkEnglish(body, spec)...)
	out = append(out, checkVagueWords(body)...)
	if skill.IsRefMode(spec.Mode) {
		out = append(out, checkRefMode(spec, sections)...)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity == SeverityError
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// HasErrors reports whether any finding blocks acceptance.
func HasErrors(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- sections --

type section struct {
	Name  string
	Body  string
	Start int // byte offset of the header
	Line  int
}

func splitSections(body string, fields []string) map[string]section {
	want := map[string]bool{}
	for _, f := range fields {
		want[f] = true
	}

	type hit struct {
		name  string
		start int
		end   int
	}
	var hits []hit
	for _, m := range fieldHeaderRe.FindAllStringSubmatchIndex(body, -1) {
		name := body[m[2]:m[3]]
		if !want[name] {
			continue
		}
		hits = append(hits, hit{name: name, start: m[0], end: m[1]})
	}

	out := map[string]section{}
	for i, h := range hits {
		stop := len(body)
		if i+1 < len(hits) {
			stop = hits[i+1].start
		}
		out[h.name] = section{
			Name:  h.name,
			Body:  strings.TrimSpace(body[h.end:stop]),
			Start: h.start,
			Line:  lineOf(body, h.start),
		}
	}
	return out
}

func lineOf(body string, offset int) int {
	if offset < 0 || offset > len(body) {
		return 0
	}
	return strings.Count(body[:offset], "\n") + 1
}

// ------------------------------------------------------------------ rules --

func checkFields(body string, spec Spec, sections map[string]section) []Finding {
	var out []Finding
	fields := skill.Fields(spec.Mode)

	var present []section
	for _, f := range fields {
		s, ok := sections[f]
		if !ok {
			out = append(out, Finding{
				Rule:     "field.missing",
				Severity: SeverityError,
				Message:  fmt.Sprintf("缺少字段 %s", f),
				Detail:   fmt.Sprintf("%s 模式必须依次包含：%s", spec.Mode, strings.Join(fields, "、")),
			})
			continue
		}
		if s.Body == "" {
			out = append(out, Finding{
				Rule:     "field.empty",
				Severity: SeverityError,
				Message:  fmt.Sprintf("字段 %s 内容为空", f),
				Line:     s.Line,
			})
		}
		present = append(present, s)
	}

	for i := 1; i < len(present); i++ {
		if present[i].Start < present[i-1].Start {
			out = append(out, Finding{
				Rule:     "field.order",
				Severity: SeverityError,
				Message:  fmt.Sprintf("字段顺序错误：%s 出现在 %s 之前", present[i].Name, present[i-1].Name),
				Detail:   "必须严格按 " + strings.Join(fields, " → ") + " 的顺序排列",
				Line:     present[i].Line,
			})
			break
		}
	}
	return out
}

func checkInstruction(body string, spec Spec) []Finding {
	var out []Finding
	first := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			first = strings.TrimSpace(line)
			break
		}
	}

	switch spec.Mode {
	case "I2VA":
		if !strings.Contains(first, "at 0.00 seconds into the target video") {
			out = append(out, Finding{
				Rule:     "instruction.missing",
				Severity: SeverityError,
				Message:  "I2VA 缺少首帧对齐指令行",
				Detail:   "第一行必须是：For the target video, at 0.00 seconds into the target video, <Picture 1> (from [Shot 1]) is fully referenced.",
				Line:     1,
			})
		}
	case "FL2VA", "L2VA":
		if !strings.Contains(first, "How the reference pictures align with the target video") {
			out = append(out, Finding{
				Rule:     "instruction.missing",
				Severity: SeverityError,
				Message:  spec.Mode + " 缺少关键帧对齐指令行",
				Detail:   "第一行必须以 How the reference pictures align with the target video — 开头",
				Line:     1,
			})
			break
		}
		want := fmt.Sprintf("%.2f", spec.Duration)
		matches := alignFLRe.FindAllStringSubmatch(first, -1)
		found := false
		for _, m := range matches {
			if m[1] == want {
				found = true
			}
		}
		if spec.Duration > 0 && !found {
			out = append(out, Finding{
				Rule:     "instruction.duration",
				Severity: SeverityError,
				Message:  fmt.Sprintf("对齐指令里的末帧时间必须是 %s 秒", want),
				Detail:   "工作流帧数换算出的有效时长为 " + want + " 秒（两位小数）",
				Line:     1,
			})
		}
	case "T2VA":
		if strings.Contains(first, "How the reference pictures align") ||
			strings.Contains(first, "is fully referenced") {
			out = append(out, Finding{
				Rule:     "instruction.unexpected",
				Severity: SeverityError,
				Message:  "T2VA 不应该带图像对齐指令行",
				Line:     1,
			})
		}
	}
	return out
}

func checkShots(body string, spec Spec, sections map[string]section) []Finding {
	var out []Finding
	mainField := "integrated_multimodal_description"
	if skill.IsRefMode(spec.Mode) {
		mainField = "detailed_description"
	}
	main, ok := sections[mainField]
	if !ok {
		return out
	}

	idx := shotRe.FindAllStringSubmatchIndex(main.Body, -1)
	if len(idx) == 0 {
		out = append(out, Finding{
			Rule:     "shot.missing",
			Severity: SeverityError,
			Message:  "正文里没有 [Shot N] 分镜标记",
			Line:     main.Line,
		})
		return out
	}

	prevTime := -1.0
	for i, m := range idx {
		num, _ := strconv.Atoi(main.Body[m[2]:m[3]])
		if num != i+1 {
			out = append(out, Finding{
				Rule:     "shot.numbering",
				Severity: SeverityError,
				Message:  fmt.Sprintf("分镜编号不连续：第 %d 个分镜写成了 [Shot %d]", i+1, num),
				Line:     main.Line + strings.Count(main.Body[:m[0]], "\n"),
			})
		}

		stop := len(main.Body)
		if i+1 < len(idx) {
			stop = idx[i+1][0]
		}
		chunk := main.Body[m[1]:stop]
		head := chunk
		if len(head) > 60 {
			head = head[:60]
		}
		ts := timestampRe.FindStringSubmatch(head)

		if i == 0 {
			if ts != nil {
				out = append(out, Finding{
					Rule:     "shot.firstTimestamp",
					Severity: SeverityError,
					Message:  "[Shot 1] 不能带时间戳",
					Line:     main.Line + strings.Count(main.Body[:m[0]], "\n"),
				})
			}
			prevTime = 0
			continue
		}

		if ts == nil {
			out = append(out, Finding{
				Rule:     "shot.timestamp",
				Severity: SeverityError,
				Message:  fmt.Sprintf("[Shot %d] 缺少 At MM:SS.mmm 切点时间", num),
				Detail:   "格式示例：[Shot 2] At 00:03.500, the camera cuts to…",
				Line:     main.Line + strings.Count(main.Body[:m[0]], "\n"),
			})
			continue
		}

		mm, _ := strconv.Atoi(ts[1])
		ss, _ := strconv.Atoi(ts[2])
		ms, _ := strconv.Atoi(ts[3])
		t := float64(mm)*60 + float64(ss) + float64(ms)/1000
		line := main.Line + strings.Count(main.Body[:m[0]], "\n")

		if t <= prevTime {
			out = append(out, Finding{
				Rule:     "shot.monotonic",
				Severity: SeverityError,
				Message:  fmt.Sprintf("[Shot %d] 的切点 %s 没有严格晚于上一个分镜", num, ts[0]),
				Line:     line,
			})
		}
		if spec.Duration > 0 && t >= spec.Duration {
			out = append(out, Finding{
				Rule:     "shot.overrun",
				Severity: SeverityError,
				Message:  fmt.Sprintf("[Shot %d] 的切点 %s 超出视频时长 %.2fs", num, ts[0], spec.Duration),
				Detail:   fmt.Sprintf("工作流的帧数换算出 %.2f 秒，所有切点必须小于这个值", spec.Duration),
				Line:     line,
			})
		} else if spec.Duration > 0 && t > spec.Duration-0.5 {
			out = append(out, Finding{
				Rule:     "shot.tooLate",
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("[Shot %d] 的切点 %s 距离结尾不足 0.5 秒，这个分镜几乎没有时间展开", num, ts[0]),
				Line:     line,
			})
		}
		prevTime = t
	}
	return out
}

func checkLabels(body string, spec Spec, sections map[string]section) []Finding {
	var out []Finding

	allowed := map[string]bool{}
	for _, l := range spec.PictureLabels {
		allowed[l] = true
	}
	for _, l := range spec.VideoLabels {
		allowed[l] = true
	}
	for _, l := range spec.AudioLabels {
		allowed[l] = true
	}

	used := map[string]int{} // label -> first line
	subjects := map[int]bool{}
	for _, m := range labelRe.FindAllStringSubmatchIndex(body, -1) {
		kind := body[m[2]:m[3]]
		numStr := body[m[4]:m[5]]
		label := fmt.Sprintf("<%s %s>", kind, numStr)
		if _, seen := used[label]; !seen {
			used[label] = lineOf(body, m[0])
		}
		if kind == "Subject" {
			n, _ := strconv.Atoi(numStr)
			subjects[n] = true
			continue
		}
		if !allowed[label] {
			out = append(out, Finding{
				Rule:     "label.undeclared",
				Severity: SeverityError,
				Message:  fmt.Sprintf("用到了工作流里不存在的引用标签 %s", label),
				Detail:   "工作流实际接入的标签只有：" + labelList(spec),
				Line:     lineOf(body, m[0]),
			})
		}
	}

	for label := range allowed {
		if _, ok := used[label]; ok {
			continue
		}
		// The keyframe-alignment instruction in the guide's own FL2VA example
		// writes "Picture 1" without angle brackets, so accept the bare form
		// as a use before reporting a label as unreferenced.
		if bareLabelUsed(body, label) {
			continue
		}
		out = append(out, Finding{
			Rule:     "label.unused",
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("工作流接了 %s，但正文里没有引用它", label),
		})
	}

	if len(subjects) > 0 {
		maxN := 0
		for n := range subjects {
			if n > maxN {
				maxN = n
			}
		}
		for n := 1; n <= maxN; n++ {
			if !subjects[n] {
				out = append(out, Finding{
					Rule:     "label.subjectGap",
					Severity: SeverityError,
					Message:  fmt.Sprintf("<Subject %d> 缺失，Subject 编号必须从 1 连续", n),
				})
			}
		}
	}

	// A dangling "<Picture" with no closing bracket is a common model slip.
	if strings.Contains(body, "<Picture") || strings.Contains(body, "<Subject") {
		danglers := regexp.MustCompile(`<(Picture|Video|Audio|Subject)(?:\s+\d+)?(?:[^>\n]{0,20})?(\n|$)`)
		for _, m := range danglers.FindAllStringIndex(body, -1) {
			out = append(out, Finding{
				Rule:     "label.dangling",
				Severity: SeverityError,
				Message:  "存在没有闭合的引用标签",
				Detail:   strings.TrimSpace(body[m[0]:min(m[1], m[0]+40)]),
				Line:     lineOf(body, m[0]),
			})
		}
	}
	return out
}

func labelList(spec Spec) string {
	all := append([]string{}, spec.PictureLabels...)
	all = append(all, spec.VideoLabels...)
	all = append(all, spec.AudioLabels...)
	if len(all) == 0 {
		return "（无）"
	}
	return strings.Join(all, "、")
}

// bareLabelUsed reports whether a label such as "<Picture 2>" also appears in
// the unbracketed "Picture 2" form the guide uses in alignment instructions.
func bareLabelUsed(body, label string) bool {
	bare := strings.TrimSuffix(strings.TrimPrefix(label, "<"), ">")
	if bare == "" || bare == label {
		return false
	}
	re, err := regexp.Compile(`\b` + regexp.QuoteMeta(bare) + `\b`)
	if err != nil {
		return false
	}
	return re.MatchString(body)
}

func checkDialogue(body string, spec Spec) []Finding {
	var out []Finding

	opens := len(dOpenRe.FindAllString(body, -1))
	closes := len(dCloseRe.FindAllString(body, -1))
	if opens != closes {
		out = append(out, Finding{
			Rule:     "dialogue.unbalanced",
			Severity: SeverityError,
			Message:  fmt.Sprintf("<d> 与 </d> 数量不匹配（%d vs %d）", opens, closes),
		})
	}

	blocks := dBlockRe.FindAllStringSubmatchIndex(body, -1)
	for _, m := range blocks {
		inner := body[m[2]:m[3]]
		line := lineOf(body, m[0])
		if !langTagRe.MatchString(inner) {
			out = append(out, Finding{
				Rule:     "dialogue.language",
				Severity: SeverityError,
				Message:  "<d> 内缺少语言标签",
				Detail:   "格式为 <d>[English] …</d>，语言标签必须紧跟在 <d> 之后",
				Line:     line,
			})
		}
		if strings.TrimSpace(langTagRe.ReplaceAllString(inner, "")) == "" {
			out = append(out, Finding{
				Rule:     "dialogue.empty",
				Severity: SeverityError,
				Message:  "<d> 里没有台词内容",
				Line:     line,
			})
		}
	}

	// User-supplied lines must survive verbatim.
	for _, want := range spec.Dialogue {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		found := false
		for _, m := range blocks {
			inner := strings.TrimSpace(langTagRe.ReplaceAllString(body[m[2]:m[3]], ""))
			if normalizeSpace(inner) == normalizeSpace(want) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, Finding{
				Rule:     "dialogue.verbatim",
				Severity: SeverityError,
				Message:  "台词被改写了，必须逐字保留",
				Detail:   "你提供的原文：" + want,
			})
		}
	}

	// Voice-over lines must state that the on-screen lips stay closed.
	for _, m := range regexp.MustCompile(`says in an off-screen voiceover`).FindAllStringIndex(body, -1) {
		tail := body[m[1]:min(len(body), m[1]+400)]
		if !strings.Contains(tail, "lips remain") {
			out = append(out, Finding{
				Rule:     "dialogue.voiceover",
				Severity: SeverityError,
				Message:  "画外音之后必须补一句角色嘴唇保持闭合",
				Detail:   "示例：… </d> while his lips remain completely closed.",
				Line:     lineOf(body, m[0]),
			})
		}
	}
	return out
}

func checkSpeakers(body string, spec Spec, sections map[string]section) []Finding {
	var out []Finding
	ids := map[int]bool{}
	for _, m := range speakerRe.FindAllStringSubmatch(body, -1) {
		for _, part := range strings.Split(m[1], ",") {
			part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "S"))
			if n, err := strconv.Atoi(part); err == nil {
				ids[n] = true
			}
		}
	}
	if len(ids) > 0 {
		maxN := 0
		for n := range ids {
			if n > maxN {
				maxN = n
			}
		}
		for n := 1; n <= maxN; n++ {
			if !ids[n] {
				out = append(out, Finding{
					Rule:     "speaker.gap",
					Severity: SeverityError,
					Message:  fmt.Sprintf("说话人编号不连续：缺少 (S%d)", n),
					Detail:   "说话人 ID 按视频中实际发声顺序从 S1 开始连续分配",
				})
			}
		}
	}

	if skill.IsRefMode(spec.Mode) {
		if s, ok := sections["retention_analysis"]; ok && speakerRe.MatchString(s.Body) {
			out = append(out, Finding{
				Rule:     "speaker.retention",
				Severity: SeverityError,
				Message:  "retention_analysis 里不允许出现 (Sx) 说话人 ID",
				Line:     s.Line,
			})
		}
	}
	return out
}

func checkAudioSections(spec Spec, sections map[string]section) []Finding {
	var out []Finding
	for _, name := range []string{"overall_soundscape", "non_diegetic_music"} {
		s, ok := sections[name]
		if !ok {
			continue
		}
		if strings.Contains(s.Body, "<d>") {
			out = append(out, Finding{
				Rule:     "audio.dialogue",
				Severity: SeverityError,
				Message:  name + " 里不能出现台词 <d> 块",
				Detail:   "台词和唱词只写在正文里，这两段不重复",
				Line:     s.Line,
			})
		}
	}
	if s, ok := sections["overall_soundscape"]; ok && s.Body != "N/A" {
		sentences := countSentences(s.Body)
		if sentences > 4 {
			out = append(out, Finding{
				Rule:     "audio.soundscapeLength",
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("overall_soundscape 有 %d 句，规范建议 1–4 句", sentences),
				Line:     s.Line,
			})
		}
	}
	if s, ok := sections["non_diegetic_music"]; ok && s.Body != "N/A" {
		sentences := countSentences(s.Body)
		if sentences > 3 {
			out = append(out, Finding{
				Rule:     "audio.musicLength",
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("non_diegetic_music 有 %d 句，规范建议 1–3 句", sentences),
				Line:     s.Line,
			})
		}
	}
	return out
}

func countSentences(s string) int {
	n := 0
	for _, r := range s {
		if r == '.' || r == '!' || r == '?' {
			n++
		}
	}
	if n == 0 && strings.TrimSpace(s) != "" {
		return 1
	}
	return n
}

// checkEnglish flags CJK text outside the places where the guide allows the
// original language: inside <d> blocks and inside double-quoted screen text.
func checkEnglish(body string, spec Spec) []Finding {
	masked := dBlockRe.ReplaceAllStringFunc(body, blank)
	masked = quotedRe.ReplaceAllStringFunc(masked, blank)

	severity := SeverityWarn
	if spec.StrictEnglish {
		severity = SeverityError
	}

	var out []Finding
	reported := map[int]bool{}
	for i, r := range masked {
		if !isCJK(r) {
			continue
		}
		line := lineOf(masked, i)
		if reported[line] {
			continue
		}
		reported[line] = true
		out = append(out, Finding{
			Rule:     "language.cjk",
			Severity: severity,
			Message:  fmt.Sprintf("第 %d 行正文里出现中文/日文/韩文", line),
			Detail:   "改写段落必须是英文；只有 <d> 内的台词唱词和英文双引号里的画面文字保留原语言。片段：" + lineSnippet(masked, line),
			Line:     line,
		})
	}
	if len(out) > 6 {
		rest := len(out) - 6
		out = out[:6]
		out = append(out, Finding{
			Rule:     "language.cjk",
			Severity: severity,
			Message:  fmt.Sprintf("另有 %d 行同样存在非英文正文", rest),
		})
	}
	return out
}

func blank(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		return ' '
	}, s)
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

func lineSnippet(body string, line int) string {
	lines := strings.Split(body, "\n")
	if line-1 < 0 || line-1 >= len(lines) {
		return ""
	}
	s := strings.TrimSpace(lines[line-1])
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

func checkVagueWords(body string) []Finding {
	var out []Finding
	lower := strings.ToLower(body)
	seen := map[string]bool{}
	for _, w := range skill.VagueWords {
		if seen[w] {
			continue
		}
		if idx := strings.Index(lower, strings.ToLower(w)); idx >= 0 {
			seen[w] = true
			out = append(out, Finding{
				Rule:     "style.vague",
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("出现抽象词 %q，规范要求写具体的画面和声音细节", w),
				Line:     lineOf(body, idx),
			})
		}
	}
	return out
}

func checkRefMode(spec Spec, sections map[string]section) []Finding {
	var out []Finding

	defs, hasDefs := sections["subject_definitions"]
	ret, hasRet := sections["retention_analysis"]

	// Every label used anywhere must be defined in subject_definitions.
	if hasDefs {
		defined := map[string]bool{}
		for _, m := range labelRe.FindAllString(defs.Body, -1) {
			defined[m] = true
		}
		for _, s := range sections {
			if s.Name == "subject_definitions" {
				continue
			}
			for _, m := range labelRe.FindAllStringSubmatchIndex(s.Body, -1) {
				label := s.Body[m[0]:m[1]]
				if !defined[label] {
					out = append(out, Finding{
						Rule:     "ref.undefined",
						Severity: SeverityError,
						Message:  fmt.Sprintf("%s 在 %s 中使用，但没有在 subject_definitions 里定义", label, s.Name),
						Line:     s.Line,
					})
					defined[label] = true // report once
				}
			}
		}
	}

	if hasRet {
		for _, line := range strings.Split(ret.Body, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			label := labelRe.FindString(line)
			if label == "" {
				continue
			}
			markers := skill.RetentionMarkers
			if strings.HasPrefix(label, "<Audio") {
				markers = skill.AudioMarkers
			}
			ok := false
			for _, m := range markers {
				if strings.Contains(line, m) {
					ok = true
					break
				}
			}
			if !ok {
				out = append(out, Finding{
					Rule:     "ref.marker",
					Severity: SeverityError,
					Message:  fmt.Sprintf("%s 的保留关系标记无效", label),
					Detail:   "只能使用固定值：" + strings.Join(markers, "、"),
					Line:     ret.Line,
				})
			}
		}
	}

	if s, ok := sections["summary"]; ok {
		m := taskPrefixRe.FindStringSubmatch(s.Body)
		if m == nil {
			out = append(out, Finding{
				Rule:     "ref.taskType",
				Severity: SeverityError,
				Message:  "summary 缺少方括号任务类型前缀",
				Detail:   "例如 [reference generation] 或 [video editing + audio reuse]",
				Line:     s.Line,
			})
		} else {
			seen := map[string]bool{}
			for _, part := range strings.Split(m[1], "+") {
				part = strings.TrimSpace(part)
				valid := false
				for _, t := range skill.TaskTypes {
					if part == t {
						valid = true
						break
					}
				}
				if !valid {
					out = append(out, Finding{
						Rule:     "ref.taskType",
						Severity: SeverityError,
						Message:  fmt.Sprintf("任务类型 %q 不在允许列表内", part),
						Detail:   "允许值：" + strings.Join(skill.TaskTypes, "、"),
						Line:     s.Line,
					})
				}
				if seen[part] {
					out = append(out, Finding{
						Rule:     "ref.taskType",
						Severity: SeverityError,
						Message:  fmt.Sprintf("任务类型 %q 重复了", part),
						Line:     s.Line,
					})
				}
				seen[part] = true
			}
		}
		if labelRe.MatchString(s.Body) && hasDefs {
			// Covered by ref.undefined above; nothing extra to do here.
			_ = defs
		}
	}
	return out
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
