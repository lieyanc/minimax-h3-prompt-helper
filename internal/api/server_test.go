package api

import "testing"

func TestAspectLabel(t *testing.T) {
	cases := []struct {
		w, h int
		want string
	}{
		// ResolutionSelector rounds each edge up to a multiple of 32, so a
		// "16:9" preset actually produces 864x480.
		{864, 480, "≈16:9 横构图"},
		{1344, 768, "≈16:9 横构图"},
		{1920, 1080, "16:9 横构图"},
		{768, 1344, "≈9:16 竖构图"},
		{1024, 1024, "1:1 方构图"},
		{1000, 300, "10:3 横构图"},
		{0, 100, ""},
	}
	for _, tc := range cases {
		if got := aspectLabel(tc.w, tc.h); got != tc.want {
			t.Errorf("aspectLabel(%d, %d) = %q, want %q", tc.w, tc.h, got, tc.want)
		}
	}
}

func TestMaxShots(t *testing.T) {
	cases := map[float64]int{
		3.04:  2,
		5.17:  3,
		0.5:   1,
		15.08: 6,
	}
	for duration, want := range cases {
		if got := maxShots(duration); got != want {
			t.Errorf("maxShots(%v) = %d, want %d", duration, got, want)
		}
	}
}
