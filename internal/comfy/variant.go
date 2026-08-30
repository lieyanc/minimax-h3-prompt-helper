package comfy

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// H3 input modes, matching the names used by the h3-prompt-writing skill.
const (
	ModeT2VA   = "T2VA"
	ModeI2VA   = "I2VA"
	ModeFL2VA  = "FL2VA"
	ModeL2VA   = "L2VA"
	ModeRef2VA = "Ref2VA"
)

// H3 node types that define a generation variant.
const (
	nodeImageToVideo     = "MiniMaxH3ImageToVideo"
	nodeReferenceToVideo = "MiniMaxH3ReferenceToVideo"
	nodeEmptyLatent      = "EmptyMiniMaxH3LatentAV"
	nodeAddGuide         = "MiniMaxH3AddGuide"
)

// RefSlot is one reference input of an H3 node.
type RefSlot struct {
	Slot      string `json:"slot"`
	Kind      string `json:"kind"` // image | video | audio
	Role      string `json:"role"` // first_frame | last_frame | reference
	Connected bool   `json:"connected"`
	// Source is the LoadImage filename the slot traces back to, when found.
	Source       string `json:"source"`
	SourceExists bool   `json:"sourceExists"`
	// Label is the reference label the prompt should use, e.g. "<Picture 1>".
	Label string `json:"label"`
}

// Variant is one H3 generation setup found inside a workflow file. A single
// workflow can hold several (the accelerated workflow keeps four branches and
// bypasses all but one).
type Variant struct {
	ID       string `json:"id"`
	Mode     string `json:"mode"`
	NodeID   int    `json:"nodeId"`
	NodeType string `json:"nodeType"`
	Title    string `json:"title"`
	Active   bool   `json:"active"`

	Width  int `json:"width"`
	Height int `json:"height"`
	Frames int `json:"frames"`

	FPS      float64 `json:"fps"`
	Duration float64 `json:"duration"`
	// FramesOnGrid reports whether the frame count sits on the model's 17k+5
	// grid. Off-grid counts are snapped up by the node at run time.
	FramesOnGrid bool `json:"framesOnGrid"`
	// Derived is false when a parameter had to fall back to a possibly stale
	// widget value because its upstream chain could not be evaluated.
	Derived bool `json:"derived"`

	Images []RefSlot `json:"images"`
	Videos []RefSlot `json:"videos"`
	Audios []RefSlot `json:"audios"`

	Guides int `json:"guides"`

	PromptText   string `json:"promptText"`
	PromptNodeID int    `json:"promptNodeId"`

	Notes []string `json:"notes"`
}

// ConnectedImages counts the wired image reference slots.
func (v Variant) ConnectedImages() int { return countConnected(v.Images) }

// ConnectedVideos counts the wired video reference slots.
func (v Variant) ConnectedVideos() int { return countConnected(v.Videos) }

// ConnectedAudios counts the wired audio reference slots.
func (v Variant) ConnectedAudios() int { return countConnected(v.Audios) }

func countConnected(slots []RefSlot) int {
	n := 0
	for _, s := range slots {
		if s.Connected {
			n++
		}
	}
	return n
}

// ExtractVariants finds every H3 generation setup in the graph.
func ExtractVariants(g *Graph, exists func(string) bool) []Variant {
	var out []Variant
	guides := 0
	hasConditioning := false

	ids := append([]int(nil), g.Order...)
	sort.Ints(ids)

	for _, id := range ids {
		n := g.Nodes[id]
		switch n.Type {
		case nodeAddGuide:
			if !n.Bypassed() {
				guides++
			}
		case nodeImageToVideo, nodeReferenceToVideo:
			hasConditioning = true
			out = append(out, buildVariant(g, n, exists))
		}
	}

	// A bare latent node with no conditioning node still describes a T2VA setup.
	if !hasConditioning {
		for _, id := range ids {
			n := g.Nodes[id]
			if n.Type == nodeEmptyLatent {
				out = append(out, buildVariant(g, n, exists))
			}
		}
	}

	for i := range out {
		out[i].Guides = guides
		if guides > 0 {
			out[i].Notes = append(out[i].Notes,
				fmt.Sprintf("工作流里有 %d 个 MiniMaxH3AddGuide 锚点节点，时间线上还有额外的图像/音频锚点。", guides))
		}
		// JSON consumers index into these, so never hand back a null.
		if out[i].Notes == nil {
			out[i].Notes = []string{}
		}
		if out[i].Images == nil {
			out[i].Images = []RefSlot{}
		}
		if out[i].Videos == nil {
			out[i].Videos = []RefSlot{}
		}
		if out[i].Audios == nil {
			out[i].Audios = []RefSlot{}
		}
	}
	return out
}

func buildVariant(g *Graph, n *Node, exists func(string) bool) Variant {
	v := Variant{
		NodeID:   n.ID,
		NodeType: n.Type,
		Title:    n.Title,
		Active:   !n.Bypassed(),
		FPS:      H3FPS,
		Derived:  true,
	}

	width := g.ResolveInput(n, "width")
	height := g.ResolveInput(n, "height")
	length := g.ResolveInput(n, "length")
	v.Width = int(math.Round(width.Num))
	v.Height = int(math.Round(height.Num))
	v.Frames = int(math.Round(length.Num))
	if !width.Resolved || !height.Resolved || !length.Resolved {
		v.Derived = false
		v.Notes = append(v.Notes, "尺寸或帧数由上游节点驱动且无法静态求值，下面显示的是节点上可能过期的 widget 值，请在任务里手动确认。")
	}

	if v.Frames > 0 {
		v.FramesOnGrid = v.Frames >= 5 && (v.Frames-5)%17 == 0
		effective := v.Frames
		if !v.FramesOnGrid {
			effective = SnapFrames(v.Frames)
			v.Notes = append(v.Notes,
				fmt.Sprintf("%d 帧不在模型的 17k+5 网格上，运行时会被抬到 %d 帧。", v.Frames, effective))
			v.Frames = effective
		}
		v.Duration = float64(v.Frames) / H3FPS
	}

	switch n.Type {
	case nodeReferenceToVideo:
		v.Mode = ModeRef2VA
		collectRefSlots(g, n, &v, exists)
	case nodeImageToVideo:
		first := isLinked(n, "first_frame")
		last := isLinked(n, "last_frame")
		switch {
		case first && last:
			v.Mode = ModeFL2VA
		case first:
			v.Mode = ModeI2VA
		case last:
			v.Mode = ModeL2VA
		default:
			v.Mode = ModeT2VA
		}
		for _, name := range []string{"first_frame", "last_frame"} {
			in, ok := n.InputByName(name)
			if !ok {
				continue
			}
			slot := RefSlot{Slot: name, Kind: "image", Role: name, Connected: in.Link != nil}
			if slot.Connected {
				slot.Source = g.traceImageSource(in.Link, 0)
				slot.SourceExists = slot.Source != "" && exists != nil && exists(slot.Source)
			}
			v.Images = append(v.Images, slot)
		}
	case nodeEmptyLatent:
		v.Mode = ModeT2VA
	}

	assignLabels(&v)

	prompt := g.ResolveInput(n, "prompt")
	v.PromptText = prompt.Str
	if in, ok := n.InputByName("prompt"); ok && in.Link != nil {
		if src, _, ok := g.origin(in.Link); ok {
			v.PromptNodeID = src.ID
		}
	} else {
		v.PromptNodeID = n.ID
	}

	v.ID = fmt.Sprintf("%s@%d", v.Mode, n.ID)
	return v
}

// collectRefSlots reads the dynamic ref_images / ref_videos / ref_audios slots
// of a MiniMaxH3ReferenceToVideo node.
func collectRefSlots(g *Graph, n *Node, v *Variant, exists func(string) bool) {
	for _, in := range n.Inputs {
		var kind string
		switch {
		case strings.HasPrefix(in.Name, "ref_images."):
			kind = "image"
		case strings.HasPrefix(in.Name, "ref_videos."):
			kind = "video"
		case strings.HasPrefix(in.Name, "ref_audios."), strings.HasPrefix(in.Name, "ref_video_audios."):
			kind = "audio"
		default:
			continue
		}
		short := in.Name
		if i := strings.LastIndex(short, "."); i >= 0 {
			short = short[i+1:]
		}
		slot := RefSlot{Slot: short, Kind: kind, Role: "reference", Connected: in.Link != nil}
		if slot.Connected && kind == "image" {
			slot.Source = g.traceImageSource(in.Link, 0)
			slot.SourceExists = slot.Source != "" && exists != nil && exists(slot.Source)
		}
		switch kind {
		case "image":
			v.Images = append(v.Images, slot)
		case "video":
			v.Videos = append(v.Videos, slot)
		case "audio":
			v.Audios = append(v.Audios, slot)
		}
	}
}

// assignLabels hands out the <Picture N>/<Video N>/<Audio N> labels the prompt
// must use, numbering only the connected slots.
func assignLabels(v *Variant) {
	pic, vid, aud := 0, 0, 0
	for i := range v.Images {
		if v.Images[i].Connected {
			pic++
			v.Images[i].Label = fmt.Sprintf("<Picture %d>", pic)
		}
	}
	for i := range v.Videos {
		if v.Videos[i].Connected {
			vid++
			v.Videos[i].Label = fmt.Sprintf("<Video %d>", vid)
		}
	}
	for i := range v.Audios {
		if v.Audios[i].Connected {
			aud++
			v.Audios[i].Label = fmt.Sprintf("<Audio %d>", aud)
		}
	}
}

func isLinked(n *Node, name string) bool {
	in, ok := n.InputByName(name)
	return ok && in.Link != nil
}

// traceImageSource walks an IMAGE link backwards to the LoadImage node feeding
// it, following pass-through nodes such as resizers and reroutes.
func (g *Graph) traceImageSource(link *int, depth int) string {
	if depth > maxResolveDepth {
		return ""
	}
	n, _, ok := g.origin(link)
	if !ok {
		return ""
	}
	switch n.Type {
	case "LoadImage", "LoadImageOutput", "LoadImageMask", "Image Load":
		if raw, ok := n.Widget("image"); ok {
			if s, ok := raw.(string); ok {
				return s
			}
		}
		if len(n.widgets) > 0 {
			if s, ok := n.widgets[0].(string); ok {
				return s
			}
		}
		return ""
	}
	// Generic pass-through: follow the first image-carrying input.
	for _, in := range n.Inputs {
		if in.Link == nil {
			continue
		}
		if in.Name == "image" || in.Name == "images" || in.Name == "image1" || strings.EqualFold(in.Type, "IMAGE") {
			if src := g.traceImageSource(in.Link, depth+1); src != "" {
				return src
			}
		}
	}
	return ""
}

// SnapFrames rounds a frame count up to the model's 17k+5 grid.
func SnapFrames(frames int) int {
	if frames < 5 {
		return 5
	}
	if (frames-5)%17 == 0 {
		return frames
	}
	return frames + (17 - (frames-5)%17)
}

// GridFrames lists the valid frame counts within a duration range, useful for
// showing the discrete durations the model actually supports.
func GridFrames(minSeconds, maxSeconds float64) []int {
	var out []int
	for f := 5; float64(f)/H3FPS <= maxSeconds; f += 17 {
		if float64(f)/H3FPS >= minSeconds {
			out = append(out, f)
		}
	}
	return out
}

// ModelSummary describes the loaders active in a workflow, for display only.
type ModelSummary struct {
	UNet  []string `json:"unet"`
	LoRA  []string `json:"lora"`
	CLIP  []string `json:"clip"`
	Steps []int    `json:"steps"`
}

// SummarizeModels collects the active loader settings of a workflow.
func SummarizeModels(g *Graph) ModelSummary {
	s := ModelSummary{UNet: []string{}, LoRA: []string{}, CLIP: []string{}, Steps: []int{}}
	ids := append([]int(nil), g.Order...)
	sort.Ints(ids)
	for _, id := range ids {
		n := g.Nodes[id]
		if n.Bypassed() {
			continue
		}
		switch n.Type {
		case "UNETLoader", "UnetLoaderGGUF":
			if v := g.ResolveInput(n, "unet_name"); v.Str != "" {
				s.UNet = appendUnique(s.UNet, v.Str)
			}
		case "LoraLoaderModelOnly", "LoraLoader":
			if v := g.ResolveInput(n, "lora_name"); v.Str != "" {
				s.LoRA = appendUnique(s.LoRA, v.Str)
			}
		case "CLIPLoader", "CLIPLoaderGGUF":
			if v := g.ResolveInput(n, "clip_name"); v.Str != "" {
				s.CLIP = appendUnique(s.CLIP, v.Str)
			}
		case "BasicScheduler", "KSampler":
			if v := g.ResolveInput(n, "steps"); v.Kind == "num" && v.Num > 0 {
				steps := int(v.Num)
				found := false
				for _, e := range s.Steps {
					if e == steps {
						found = true
					}
				}
				if !found {
					s.Steps = append(s.Steps, steps)
				}
			}
		}
	}
	return s
}

func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}
