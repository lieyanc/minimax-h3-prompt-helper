package comfy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInputPathRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "3d"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.png", "3d/b.png"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lib := NewLibrary(nil, root)

	ok := []string{"a.png", "3d/b.png", "a.png [input]", "./a.png"}
	for _, name := range ok {
		path, err := lib.ResolveInputPath(name)
		if err != nil {
			t.Errorf("ResolveInputPath(%q) failed: %v", name, err)
			continue
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("ResolveInputPath(%q) = %q, which does not exist", name, path)
		}
		if !lib.InputExists(name) {
			t.Errorf("InputExists(%q) = false", name)
		}
	}

	// Traversal attempts must stay inside the input directory.
	for _, name := range []string{"../secret", "../../etc/passwd", "/etc/passwd", "3d/../../escape"} {
		path, err := lib.ResolveInputPath(name)
		if err != nil {
			continue // rejected outright, also fine
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || filepath.IsAbs(rel) || rel == ".." || len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator) {
			t.Errorf("ResolveInputPath(%q) = %q, which escapes %q", name, path, root)
		}
		if lib.InputExists(name) {
			t.Errorf("InputExists(%q) = true, expected the escape to resolve to nothing", name)
		}
	}

	if _, err := lib.ResolveInputPath(""); err == nil {
		t.Error("expected an error for an empty name")
	}
}

func TestCleanInputName(t *testing.T) {
	cases := map[string]string{
		"a.png":                   "a.png",
		"a.png [input]":           "a.png",
		"clipspace/x.png [input]": "clipspace/x.png",
		"  b.png  ":               "b.png",
		`sub\c.png`:               "sub/c.png",
		"./d.png":                 "d.png",
	}
	for in, want := range cases {
		if got := CleanInputName(in); got != want {
			t.Errorf("CleanInputName(%q) = %q, want %q", in, got, want)
		}
	}
}
