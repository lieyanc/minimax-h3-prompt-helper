// Package slots turns the H3 writing rules into a fixed list of things the
// user has to decide, and asks only for the ones that are still missing. The
// question set is deterministic — no model decides what to ask.
package slots

import (
	"fmt"
	"strings"

	"h3helper/internal/skill"
	"h3helper/internal/task"
)

// Slot keys.
const (
	Brief         = "brief"
	Style         = "style"
	Action        = "action"
	CameraMotion  = "camera_motion"
	CameraDynamic = "camera_dynamic"
	Shots         = "shots"
	HasDialogue   = "has_dialogue"
	DialogueLines = "dialogue_lines"
	DialogueMode  = "dialogue_mode"
	ScreenText    = "screen_text"
	Soundscape    = "soundscape"
	Music         = "music"
	MusicDesc     = "music_desc"
	Ending        = "ending"
)

// Prefixes for the per-image slots used in full-reference mode.
const (
	ImageRolePrefix      = "image_role:"
	ImageRetentionPrefix = "image_retention:"
)

// Def describes one slot and how to ask for it.
type Def struct {
	Slot        string
	Group       string
	Title       string
	Help        string
	Kind        string
	Required    bool
	AllowFree   bool
	Placeholder string
	Options     []task.Option
	Suggest     func(t *task.Task) string
	Applies     func(t *task.Task) bool
}

// Definitions expands the full slot list for a task, including the per-image
// slots that only exist in full-reference mode.
func Definitions(t *task.Task) []Def {
	defs := []Def{
		{
			Slot:        Brief,
			Group:       "意图",
			Title:       "这条视频要发生什么？一句话说清楚",
			Help:        "模型能看懂图，但猜不出你的意图。写清楚主体做了什么、结果是什么。",
			Kind:        task.KindTextarea,
			Required:    true,
			Placeholder: "例：女人放下手里的信，抬头看向窗外驶过的城市灯光",
			Suggest:     suggestBrief,
		},
	}

	// Full-reference mode needs a role and a retention strength per image.
	if skill.IsRefMode(t.Constraints.Mode) {
		for _, img := range t.Images {
			if img.Label == "" {
				continue
			}
			label := img.Label
			defs = append(defs, Def{
				Slot:     ImageRolePrefix + label,
				Group:    "参考图",
				Title:    fmt.Sprintf("%s 在这条视频里承担什么角色？", label),
				Help:     "决定它写进 subject_definitions 时的定义方式。",
				Kind:     task.KindChoice,
				Required: true,
				Options: []task.Option{
					{Value: "subject_appearance", Label: "人物/主体外观来源", Desc: "定义成 <Subject N>，保留长相、服装、体型"},
					{Value: "environment", Label: "环境/场景来源", Desc: "定义成 <Subject N> 的场景，保留布局和陈设"},
					{Value: "prop_costume", Label: "服装/道具来源", Desc: "只提供衣物、道具或界面元素"},
					{Value: "style", Label: "风格参考", Desc: "只提供画面风格、色调、质感"},
					{Value: "keyframe_first", Label: "首帧锚点", Desc: "作为 [Shot 1] 的第一帧，独立列 <Picture N>"},
					{Value: "keyframe_last", Label: "末帧锚点", Desc: "作为最后一个分镜的落点"},
					{Value: "storyboard", Label: "分镜/构图参考", Desc: "提供机位、主体位置和分镜顺序"},
				},
				Suggest: suggestImageRole(label),
			})
			defs = append(defs, Def{
				Slot:     ImageRetentionPrefix + label,
				Group:    "参考图",
				Title:    fmt.Sprintf("%s 的内容要保留到什么程度？", label),
				Help:     "写进 retention_analysis 的固定关系标记。",
				Kind:     task.KindChoice,
				Required: true,
				Options: []task.Option{
					{Value: "fully_preserved", Label: "fully_preserved", Desc: "定义的特征完整保留"},
					{Value: "partially_preserved", Label: "partially_preserved", Desc: "仍在使用，但部分特征被改动"},
					{Value: "attribute_transfer", Label: "attribute_transfer", Desc: "特征转移到另一个可识别的主体上"},
					{Value: "weak_reference", Label: "weak_reference", Desc: "只保留风格、类别或氛围上的相似"},
				},
				Suggest: func(*task.Task) string { return "fully_preserved" },
			})
		}
	}

	defs = append(defs,
		Def{
			Slot:      Style,
			Group:     "画面",
			Title:     "整体视觉风格",
			Help:      "写在 [Shot 1] 开头。有参考图时应当沿用图里的风格。",
			Kind:      task.KindChoice,
			Required:  true,
			AllowFree: true,
			Options:   styleOptions(),
			Suggest:   suggestStyle,
		},
		Def{
			Slot:        Action,
			Group:       "画面",
			Title:       "动作路径：起点 → 过程 → 落点",
			Help:        keyframeHelp(t.Constraints.Mode),
			Kind:        task.KindTextarea,
			Required:    true,
			Placeholder: "例：她先握着雨伞站在车旁 → 抬伞、按开伞扣、伞面撑开 → 走到伞下，落到末帧的姿势",
			Suggest:     suggestAction,
		},
		Def{
			Slot:      CameraMotion,
			Group:     "镜头",
			Title:     "主镜头运动",
			Help:      "规范里的受控词表，写成句子而不是标签堆叠。",
			Kind:      task.KindChoice,
			Required:  true,
			AllowFree: true,
			Options:   cameraOptions(),
			Suggest:   suggestCamera,
		},
		Def{
			Slot:     CameraDynamic,
			Group:    "镜头",
			Title:    "运动幅度和速度",
			Help:     "中等幅度、常速通常省略不写。",
			Kind:     task.KindChoice,
			Required: false,
			Options: []task.Option{
				{Value: "", Label: "默认（不写幅度和速度）"},
				{Value: "with small amplitude at slow speed", Label: "小幅度 + 慢速"},
				{Value: "with small amplitude", Label: "小幅度"},
				{Value: "with large amplitude at fast speed", Label: "大幅度 + 快速"},
				{Value: "at slow speed", Label: "慢速"},
				{Value: "at fast speed", Label: "快速"},
			},
			Suggest: func(*task.Task) string { return "with small amplitude at slow speed" },
		},
		Def{
			Slot:     Shots,
			Group:    "镜头",
			Title:    "分几个镜头？",
			Help:     shotsHelp(t.Constraints),
			Kind:     task.KindChoice,
			Required: true,
			Options:  shotOptions(t.Constraints),
			Suggest:  suggestShots,
		},
		Def{
			Slot:     HasDialogue,
			Group:    "声音",
			Title:    "有没有人说话或唱歌？",
			Help:     "H3 会生成原生音轨，台词必须逐字给出，模型不能自己编。",
			Kind:     task.KindChoice,
			Required: true,
			Options: []task.Option{
				{Value: "no", Label: "没有台词", Desc: "只有环境音和配乐"},
				{Value: "yes", Label: "有台词/唱词", Desc: "下一步逐字填写"},
			},
			Suggest: func(*task.Task) string { return "no" },
		},
		Def{
			Slot:        DialogueLines,
			Group:       "声音",
			Title:       "逐字写出台词",
			Help:        "一行一句，格式：说话人描述 | 语言 | 台词原文。台词会被逐字比对，模型改一个字都会被判错。",
			Kind:        task.KindTextarea,
			Required:    true,
			Placeholder: "年轻女性，气声 | English | I get off at the next station.",
			Applies:     func(t *task.Task) bool { return t.Answer(HasDialogue) == "yes" },
		},
		Def{
			Slot:     DialogueMode,
			Group:    "声音",
			Title:    "台词是画内说出来的还是画外音？",
			Kind:     task.KindChoice,
			Required: true,
			Options: []task.Option{
				{Value: "on_screen", Label: "画内", Desc: "角色在画面里开口"},
				{Value: "voiceover", Label: "画外音", Desc: "会自动补 lips remain completely closed"},
				{Value: "mixed", Label: "两者都有"},
			},
			Suggest: func(*task.Task) string { return "on_screen" },
			Applies: func(t *task.Task) bool { return t.Answer(HasDialogue) == "yes" },
		},
		Def{
			Slot:        ScreenText,
			Group:       "画面",
			Title:       "画面上要出现的文字（招牌、字幕、霓虹灯等）",
			Help:        "会原样放进英文双引号，不翻译。没有就留空。",
			Kind:        task.KindText,
			Required:    false,
			Placeholder: `例：营业中`,
			Suggest:     suggestScreenText,
		},
		Def{
			Slot:        Soundscape,
			Group:       "声音",
			Title:       "环境音和动作音",
			Help:        "只写环境、物理动作和非语言人声，台词不重复写在这里。留空则按画面推断。",
			Kind:        task.KindTextarea,
			Required:    false,
			Placeholder: "例：雨点打在窗上、列车行进的金属节奏、纸张翻动",
			Suggest:     suggestSoundscape,
		},
		Def{
			Slot:     Music,
			Group:    "声音",
			Title:    "要不要观众才听得到的配乐？",
			Help:     "角色能听到的音乐属于画内声音，不写在 non_diegetic_music 里。",
			Kind:     task.KindChoice,
			Required: true,
			Options: []task.Option{
				{Value: "none", Label: "不要配乐", Desc: "输出 N/A"},
				{Value: "yes", Label: "要配乐"},
			},
			Suggest: func(*task.Task) string { return "yes" },
		},
		Def{
			Slot:        MusicDesc,
			Group:       "声音",
			Title:       "配乐的器乐、速度和动态",
			Help:        "只写乐器、速度、节奏和强弱变化，不写情绪词。",
			Kind:        task.KindText,
			Required:    false,
			Placeholder: "例：缓慢的钢琴单音，低音弦乐渐强后淡出",
			Applies:     func(t *task.Task) bool { return t.Answer(Music) == "yes" },
		},
		Def{
			Slot:        Ending,
			Group:       "意图",
			Title:       "结尾要停在什么状态？",
			Help:        endingHelp(t.Constraints.Mode),
			Kind:        task.KindText,
			Required:    false,
			Placeholder: "例：碎片停止滑动，画面落在末帧的构图上",
			Applies:     func(t *task.Task) bool { return t.Constraints.Mode != "T2VA" },
		},
	)

	return defs
}

// Next returns up to limit questions that still need answers, required first.
func Next(t *task.Task, limit int) []task.Question {
	defs := Definitions(t)
	var required, optional []task.Question

	for _, d := range defs {
		if d.Applies != nil && !d.Applies(t) {
			continue
		}
		if strings.TrimSpace(t.Answer(d.Slot)) != "" {
			continue
		}
		q := toQuestion(d, t)
		if d.Required {
			required = append(required, q)
		} else {
			optional = append(optional, q)
		}
	}

	out := append(required, optional...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// MissingRequired lists the required slots that are still empty.
func MissingRequired(t *task.Task) []string {
	var out []string
	for _, d := range Definitions(t) {
		if !d.Required {
			continue
		}
		if d.Applies != nil && !d.Applies(t) {
			continue
		}
		if strings.TrimSpace(t.Answer(d.Slot)) == "" {
			out = append(out, d.Slot)
		}
	}
	return out
}

// Progress reports how many applicable slots have been answered.
func Progress(t *task.Task) (answered, total int) {
	for _, d := range Definitions(t) {
		if d.Applies != nil && !d.Applies(t) {
			continue
		}
		if !d.Required {
			continue
		}
		total++
		if strings.TrimSpace(t.Answer(d.Slot)) != "" {
			answered++
		}
	}
	return answered, total
}

// Filled is one answered slot, carried into the writing prompt with the
// question it answered.
type Filled struct {
	Slot  string
	Title string
	Value string
}

// Collect gathers the answered slots in definition order.
func Collect(t *task.Task) []Filled {
	var out []Filled
	for _, d := range Definitions(t) {
		if d.Applies != nil && !d.Applies(t) {
			continue
		}
		v := strings.TrimSpace(t.Answer(d.Slot))
		if v == "" {
			continue
		}
		out = append(out, Filled{Slot: d.Slot, Title: d.Title, Value: v})
	}
	return out
}

// DialogueTexts splits the dialogue answer into verbatim lines for the
// validator to diff against.
func DialogueTexts(t *task.Task) []string {
	raw := strings.TrimSpace(t.Answer(DialogueLines))
	if raw == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		text := strings.TrimSpace(parts[len(parts)-1])
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func toQuestion(d Def, t *task.Task) task.Question {
	q := task.Question{
		Slot:        d.Slot,
		Group:       d.Group,
		Title:       d.Title,
		Help:        d.Help,
		Kind:        d.Kind,
		Options:     d.Options,
		Required:    d.Required,
		AllowFree:   d.AllowFree,
		Placeholder: d.Placeholder,
	}
	if d.Suggest != nil {
		q.Suggestion = d.Suggest(t)
	}
	return q
}

// ------------------------------------------------------------ suggestions --

func styleOptions() []task.Option {
	var out []task.Option
	for _, s := range skill.Styles {
		out = append(out, task.Option{Value: s, Label: s})
	}
	return out
}

func cameraOptions() []task.Option {
	var out []task.Option
	for _, m := range skill.CameraMotions {
		out = append(out, task.Option{Value: m, Label: m})
	}
	return out
}

func shotOptions(c task.Constraints) []task.Option {
	out := []task.Option{
		{Value: "1", Label: "单镜头", Desc: "整段一个镜头，靠镜头运动推进"},
	}
	maxShots := c.MaxShots
	if maxShots < 1 {
		maxShots = 1
	}
	for n := 2; n <= maxShots; n++ {
		out = append(out, task.Option{
			Value: fmt.Sprintf("%d", n),
			Label: fmt.Sprintf("%d 个镜头", n),
			Desc:  fmt.Sprintf("平均每镜 %.1f 秒", c.Duration/float64(n)),
		})
	}
	return out
}

func shotsHelp(c task.Constraints) string {
	if c.Duration <= 0 {
		return "切点时间必须严格递增且小于视频总时长。"
	}
	base := fmt.Sprintf("总时长只有 %.2f 秒（%d 帧 @ 24fps），切点必须小于这个值。", c.Duration, c.Frames)
	if c.Mode == "FL2VA" {
		return base + " FL2VA 一般优先单镜头，好让模型在首末帧之间连续插值。"
	}
	return base
}

func keyframeHelp(mode string) string {
	switch mode {
	case "I2VA":
		return "I2VA 从首帧往后发展：先锚定图里的构图和主体，再写动作如何开始。"
	case "FL2VA":
		return "FL2VA 要写首帧到末帧之间的运动路径，而不是把两张图各描述一遍。"
	case "L2VA":
		return "L2VA 要先推一个合理的前置状态，再逐步收敛到末帧那张图。"
	case "Ref2VA":
		return "写清楚每个参考主体在时间线上做了什么、什么时候出现。"
	}
	return "从文本构建完整时间线：起始状态、动作展开、结果或反应。"
}

func endingHelp(mode string) string {
	switch mode {
	case "FL2VA", "L2VA":
		return "结尾必须落在末帧参考图的姿态、构图和光线上。"
	}
	return "结尾状态决定最后一个动作怎么收。"
}

func suggestBrief(t *task.Task) string {
	for _, img := range t.Images {
		f, ok := t.Facts[img.Label]
		if ok && f.PossibleAction != "" {
			return f.PossibleAction
		}
	}
	return ""
}

func suggestStyle(t *task.Task) string {
	for _, img := range t.Images {
		if f, ok := t.Facts[img.Label]; ok && f.Style != "" {
			return f.Style
		}
	}
	return ""
}

func suggestAction(t *task.Task) string {
	for _, img := range t.Images {
		if f, ok := t.Facts[img.Label]; ok && f.PossibleAction != "" {
			return f.PossibleAction
		}
	}
	return ""
}

func suggestCamera(t *task.Task) string {
	for _, img := range t.Images {
		if f, ok := t.Facts[img.Label]; ok && f.ShotSize != "" {
			if strings.Contains(strings.ToLower(f.ShotSize), "close") {
				return "Static Shot"
			}
			return "Push In"
		}
	}
	return "Push In"
}

func suggestShots(t *task.Task) string {
	if t.Constraints.Mode == "FL2VA" || t.Constraints.Duration < 5 {
		return "1"
	}
	return "1"
}

func suggestScreenText(t *task.Task) string {
	var parts []string
	for _, img := range t.Images {
		if f, ok := t.Facts[img.Label]; ok {
			parts = append(parts, f.VisibleText...)
		}
	}
	return strings.Join(parts, " / ")
}

func suggestSoundscape(t *task.Task) string {
	for _, img := range t.Images {
		if f, ok := t.Facts[img.Label]; ok && f.Environment != "" {
			return ""
		}
	}
	return ""
}

func suggestImageRole(label string) func(*task.Task) string {
	return func(t *task.Task) string {
		f, ok := t.Facts[label]
		if !ok {
			return ""
		}
		onlyEnvironment := len(f.Subjects) > 0
		for _, s := range f.Subjects {
			switch s.Kind {
			case "person", "animal":
				return "subject_appearance"
			case "environment":
			default:
				onlyEnvironment = false
			}
		}
		if onlyEnvironment || (f.Environment != "" && len(f.Subjects) == 0) {
			return "environment"
		}
		return "subject_appearance"
	}
}
