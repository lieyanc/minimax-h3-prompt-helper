package comfy

import "testing"

func TestEvalFrameSnapExpression(t *testing.T) {
	// The expression every MiniMax H3 workflow uses to snap a duration in
	// seconds onto the model's 17k+5 frame grid at 24 fps.
	const expr = "max(5, round(a * 24)) + (5 - (max(5, round(a * 24)) % 17)) % 17"

	cases := []struct {
		seconds float64
		frames  float64
	}{
		{5, 124},
		{3, 73},
		{1, 39},
		{0.1, 5},
		{8, 192},
	}
	for _, tc := range cases {
		got, err := evalExpr(expr, map[string]float64{"a": tc.seconds})
		if err != nil {
			t.Fatalf("evalExpr(%v): %v", tc.seconds, err)
		}
		if got != tc.frames {
			t.Errorf("evalExpr(a=%v) = %v, want %v", tc.seconds, got, tc.frames)
		}
		if (int(got)-5)%17 != 0 {
			t.Errorf("evalExpr(a=%v) = %v, not on the 17k+5 grid", tc.seconds, got)
		}
	}
}

func TestEvalExprErrors(t *testing.T) {
	if _, err := evalExpr("a + ", nil); err == nil {
		t.Error("expected an error for a truncated expression")
	}
	if _, err := evalExpr("b * 2", map[string]float64{"a": 1}); err == nil {
		t.Error("expected an error for an unbound variable")
	}
	if _, err := evalExpr("1 / 0", nil); err == nil {
		t.Error("expected an error for division by zero")
	}
}

func TestSnapFrames(t *testing.T) {
	cases := map[int]int{
		1:   5,
		5:   5,
		6:   22,
		22:  22,
		23:  39,
		120: 124,
		124: 124,
	}
	for in, want := range cases {
		if got := SnapFrames(in); got != want {
			t.Errorf("SnapFrames(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestParseAspect(t *testing.T) {
	cases := []struct {
		in    string
		ratio float64
		ok    bool
	}{
		{"16:9 (Widescreen)", 16.0 / 9.0, true},
		{"9:16", 9.0 / 16.0, true},
		{"1:1 (Square)", 1, true},
		{"portrait", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseAspect(tc.in)
		if ok != tc.ok {
			t.Errorf("parseAspect(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && got != tc.ratio {
			t.Errorf("parseAspect(%q) = %v, want %v", tc.in, got, tc.ratio)
		}
	}
}

// TestResolutionSelectorMatchesWorkflowNote checks the canvas computation
// against the reference table embedded in the shipped H3 workflow.
func TestResolutionSelectorMatchesWorkflowNote(t *testing.T) {
	g := &Graph{Nodes: map[int]*Node{}, links: map[int]linkRef{}}
	node := &Node{
		ID:   1,
		Type: "ResolutionSelector",
		Inputs: []Input{
			{Name: "aspect_ratio", HasWidget: true},
			{Name: "megapixels", HasWidget: true},
			{Name: "multiple", HasWidget: true},
		},
	}
	g.Nodes[1] = node

	cases := []struct {
		megapixels float64
		aspect     string
		w, h       int
	}{
		{0.2, "16:9 (Widescreen)", 608, 352},
		{0.3, "16:9 (Widescreen)", 736, 416},
		{0.4, "16:9 (Widescreen)", 864, 480},
		{0.5, "16:9 (Widescreen)", 960, 544},
	}
	for _, tc := range cases {
		node.widgets = []any{tc.aspect, tc.megapixels, 32.0}
		w, h, ok := g.resolutionSelector(node)
		if !ok {
			t.Fatalf("resolutionSelector(%v) failed", tc.megapixels)
		}
		if w != tc.w || h != tc.h {
			t.Errorf("resolutionSelector(%v %s) = %dx%d, want %dx%d",
				tc.megapixels, tc.aspect, w, h, tc.w, tc.h)
		}
	}
}
