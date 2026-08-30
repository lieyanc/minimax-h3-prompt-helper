// Package comfy reads ComfyUI workflow JSON files and extracts the generation
// constraints that a MiniMax H3 prompt has to satisfy: input mode, canvas size,
// frame count (and therefore the exact timeline length), and which reference
// image slots are actually wired up.
package comfy

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// H3 runs its timeline on a fixed 24 fps grid, regardless of the fps used by
// the downstream CreateVideo node.
const H3FPS = 24.0

// Node mode values used by the ComfyUI frontend.
const (
	modeNormal = 0
	modeMuted  = 2
	modeBypass = 4
)

// Input is one input slot of a node.
type Input struct {
	Name      string
	Type      string
	Link      *int
	HasWidget bool
}

// Output is one output slot of a node.
type Output struct {
	Name  string
	Type  string
	Links []int
}

// Node is a single node in the workflow graph.
type Node struct {
	ID      int
	Type    string
	Title   string
	Mode    int
	Inputs  []Input
	Outputs []Output

	// widgets holds the positional widgets_values array. Widget-backed inputs
	// appear in Inputs in the same order, which is how names are recovered.
	widgets []any
}

// Widget returns the value of the named widget on this node.
func (n *Node) Widget(name string) (any, bool) {
	idx := 0
	for _, in := range n.Inputs {
		if !in.HasWidget {
			continue
		}
		if in.Name == name {
			if idx < len(n.widgets) {
				return n.widgets[idx], true
			}
			return nil, false
		}
		idx++
	}
	return nil, false
}

// InputByName finds an input slot by its exact name.
func (n *Node) InputByName(name string) (Input, bool) {
	for _, in := range n.Inputs {
		if in.Name == name {
			return in, true
		}
	}
	return Input{}, false
}

// Bypassed reports whether the node is muted or bypassed in the editor.
func (n *Node) Bypassed() bool { return n.Mode == modeMuted || n.Mode == modeBypass }

type linkRef struct {
	originNode int
	originSlot int
}

// Graph is a parsed workflow.
type Graph struct {
	Nodes map[int]*Node
	Order []int
	links map[int]linkRef
}

// ---------------------------------------------------------------- parsing --

type rawWorkflow struct {
	Nodes []rawNode       `json:"nodes"`
	Links json.RawMessage `json:"links"`
}

type rawNode struct {
	ID            int             `json:"id"`
	Type          string          `json:"type"`
	Title         string          `json:"title"`
	Mode          int             `json:"mode"`
	Inputs        []rawInput      `json:"inputs"`
	Outputs       []rawOutput     `json:"outputs"`
	WidgetsValues json.RawMessage `json:"widgets_values"`
}

type rawInput struct {
	Name   string          `json:"name"`
	Type   json.RawMessage `json:"type"`
	Link   *int            `json:"link"`
	Widget json.RawMessage `json:"widget"`
}

type rawOutput struct {
	Name  string          `json:"name"`
	Type  json.RawMessage `json:"type"`
	Links []int           `json:"links"`
}

// ParseGraph decodes a workflow JSON document.
func ParseGraph(data []byte) (*Graph, error) {
	var raw rawWorkflow
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode workflow: %w", err)
	}
	if len(raw.Nodes) == 0 {
		return nil, fmt.Errorf("workflow has no nodes")
	}

	g := &Graph{Nodes: make(map[int]*Node, len(raw.Nodes)), links: map[int]linkRef{}}
	for _, rn := range raw.Nodes {
		n := &Node{ID: rn.ID, Type: rn.Type, Title: rn.Title, Mode: rn.Mode}
		n.widgets = decodeWidgets(rn.WidgetsValues)
		for _, ri := range rn.Inputs {
			n.Inputs = append(n.Inputs, Input{
				Name:      ri.Name,
				Type:      decodeTypeName(ri.Type),
				Link:      ri.Link,
				HasWidget: len(ri.Widget) > 0 && string(ri.Widget) != "null",
			})
		}
		for _, ro := range rn.Outputs {
			n.Outputs = append(n.Outputs, Output{Name: ro.Name, Type: decodeTypeName(ro.Type), Links: ro.Links})
		}
		g.Nodes[n.ID] = n
		g.Order = append(g.Order, n.ID)
	}

	if err := g.decodeLinks(raw.Links); err != nil {
		return nil, err
	}
	return g, nil
}

// decodeWidgets accepts both the positional array form and the newer object
// form of widgets_values.
func decodeWidgets(raw json.RawMessage) []any {
	if len(raw) == 0 {
		return nil
	}
	var arr []any
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		// Object form is keyed by widget name; Widget() falls back to it via
		// a synthetic single-entry slice, so preserve insertion-independent
		// lookup by returning nil here and letting callers use widgetObj.
		out := make([]any, 0, len(obj))
		for _, v := range obj {
			out = append(out, v)
		}
		return out
	}
	return nil
}

func decodeTypeName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

// decodeLinks handles both the array-of-arrays and array-of-objects link forms.
func (g *Graph) decodeLinks(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var asArrays [][]json.RawMessage
	if err := json.Unmarshal(raw, &asArrays); err == nil {
		for _, l := range asArrays {
			if len(l) < 3 {
				continue
			}
			id, err1 := jsonInt(l[0])
			origin, err2 := jsonInt(l[1])
			slot, err3 := jsonInt(l[2])
			if err1 != nil || err2 != nil || err3 != nil {
				continue
			}
			g.links[id] = linkRef{originNode: origin, originSlot: slot}
		}
		return nil
	}

	var asObjects []struct {
		ID         int `json:"id"`
		OriginID   int `json:"origin_id"`
		OriginSlot int `json:"origin_slot"`
	}
	if err := json.Unmarshal(raw, &asObjects); err == nil {
		for _, l := range asObjects {
			g.links[l.ID] = linkRef{originNode: l.OriginID, originSlot: l.OriginSlot}
		}
		return nil
	}
	// Unknown link encoding: the graph is still usable for widget-only reads.
	return nil
}

func jsonInt(raw json.RawMessage) (int, error) {
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, err
	}
	return int(f), nil
}

// origin resolves a link id to the producing node and output slot.
func (g *Graph) origin(link *int) (*Node, int, bool) {
	if link == nil {
		return nil, 0, false
	}
	ref, ok := g.links[*link]
	if !ok {
		return nil, 0, false
	}
	n, ok := g.Nodes[ref.originNode]
	if !ok {
		return nil, 0, false
	}
	return n, ref.originSlot, true
}

// ------------------------------------------------------------- resolution --

// Value is a resolved graph value along with how confident the resolution is.
type Value struct {
	Num      float64
	Str      string
	Bool     bool
	Kind     string // "num", "str", "bool"
	Resolved bool   // false when the value came from a stale widget fallback
}

const maxResolveDepth = 24

// ResolveInput resolves the effective value of an input: the upstream node's
// output when the input is linked, otherwise the node's own widget value.
func (g *Graph) ResolveInput(n *Node, name string) Value {
	in, ok := n.InputByName(name)
	if ok && in.Link != nil {
		if v := g.resolveLink(in.Link, 0); v.Kind != "" {
			return v
		}
	}
	if raw, ok := n.Widget(name); ok {
		v := valueOf(raw)
		// A linked input whose upstream could not be resolved still carries a
		// widget value, but it may be stale, so flag it.
		v.Resolved = !(ok && in.Link != nil)
		return v
	}
	return Value{}
}

func (g *Graph) resolveLink(link *int, depth int) Value {
	if depth > maxResolveDepth {
		return Value{}
	}
	n, slot, ok := g.origin(link)
	if !ok {
		return Value{}
	}
	return g.resolveOutput(n, slot, depth)
}

// resolveOutput computes the value produced on a node's output slot. Only the
// handful of helper nodes that actually drive H3 parameters are modelled; other
// node types resolve to an empty value and callers fall back to widgets.
func (g *Graph) resolveOutput(n *Node, slot, depth int) Value {
	if depth > maxResolveDepth {
		return Value{}
	}
	switch n.Type {
	case "PrimitiveInt", "PrimitiveFloat", "PrimitiveString", "PrimitiveStringMultiline", "PrimitiveBoolean":
		if raw, ok := n.Widget("value"); ok {
			return valueOf(raw)
		}
		if len(n.widgets) > 0 {
			return valueOf(n.widgets[0])
		}

	case "ComfyMathExpression":
		vars := map[string]float64{}
		for _, in := range n.Inputs {
			if !strings.HasPrefix(in.Name, "values.") || in.Link == nil {
				continue
			}
			v := g.resolveLink(in.Link, depth+1)
			if v.Kind == "num" {
				vars[strings.TrimPrefix(in.Name, "values.")] = v.Num
			} else if v.Kind == "bool" {
				vars[strings.TrimPrefix(in.Name, "values.")] = boolToFloat(v.Bool)
			}
		}
		exprVal := g.ResolveInput(n, "expression")
		if exprVal.Str == "" {
			break
		}
		out, err := evalExpr(exprVal.Str, vars)
		if err != nil {
			break
		}
		switch slot {
		case 1: // INT
			return Value{Kind: "num", Num: math.Round(out), Resolved: true}
		case 2: // BOOL
			return Value{Kind: "bool", Bool: out != 0, Resolved: true}
		default:
			return Value{Kind: "num", Num: out, Resolved: true}
		}

	case "ResolutionSelector":
		w, h, ok := g.resolutionSelector(n)
		if !ok {
			break
		}
		if slot == 1 {
			return Value{Kind: "num", Num: float64(h), Resolved: true}
		}
		return Value{Kind: "num", Num: float64(w), Resolved: true}

	case "ComfySwitchNode":
		sel := g.ResolveInput(n, "switch")
		name := "on_false"
		if sel.Bool {
			name = "on_true"
		}
		if in, ok := n.InputByName(name); ok && in.Link != nil {
			return g.resolveLink(in.Link, depth+1)
		}

	case "ImageResizeKJv2":
		switch slot {
		case 1:
			return g.ResolveInput(n, "width")
		case 2:
			return g.ResolveInput(n, "height")
		}

	case "Reroute", "Reroute (rgthree)", "ReroutePrimitive|pysssss":
		if len(n.Inputs) > 0 && n.Inputs[0].Link != nil {
			return g.resolveLink(n.Inputs[0].Link, depth+1)
		}
	}

	// Generic fallback: a node whose only widget is a string (custom text
	// nodes) is treated as a string source.
	if slot == 0 && len(n.widgets) == 1 {
		if s, ok := n.widgets[0].(string); ok {
			return Value{Kind: "str", Str: s, Resolved: true}
		}
	}
	return Value{}
}

// resolutionSelector reproduces the node's megapixel-to-canvas computation:
// each edge is rounded up to the configured multiple.
func (g *Graph) resolutionSelector(n *Node) (int, int, bool) {
	aspect := g.ResolveInput(n, "aspect_ratio").Str
	mp := g.ResolveInput(n, "megapixels").Num
	mult := g.ResolveInput(n, "multiple").Num
	if mult <= 0 {
		mult = 32
	}
	ratio, ok := parseAspect(aspect)
	if !ok || mp <= 0 {
		return 0, 0, false
	}
	total := mp * 1e6
	w := math.Sqrt(total * ratio)
	h := w / ratio
	roundUp := func(v float64) int {
		return int(math.Max(mult, math.Ceil(v/mult)*mult))
	}
	return roundUp(w), roundUp(h), true
}

// parseAspect reads the leading "W:H" out of labels like "16:9 (Widescreen)".
func parseAspect(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if i := strings.IndexAny(s, " ("); i > 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, false
	}
	w, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	h, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || h == 0 {
		return 0, false
	}
	return w / h, true
}

func valueOf(raw any) Value {
	switch v := raw.(type) {
	case float64:
		return Value{Kind: "num", Num: v, Resolved: true}
	case int:
		return Value{Kind: "num", Num: float64(v), Resolved: true}
	case string:
		return Value{Kind: "str", Str: v, Resolved: true}
	case bool:
		return Value{Kind: "bool", Bool: v, Resolved: true}
	}
	return Value{}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
