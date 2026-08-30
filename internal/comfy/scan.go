package comfy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Workflow is one parsed workflow file.
type Workflow struct {
	File     string       `json:"file"`
	Name     string       `json:"name"`
	Dir      string       `json:"dir"`
	ModTime  time.Time    `json:"modTime"`
	Size     int64        `json:"size"`
	Variants []Variant    `json:"variants"`
	Models   ModelSummary `json:"models"`
	Error    string       `json:"error,omitempty"`
}

// Library scans workflow directories and serves the ComfyUI input folder.
type Library struct {
	mu       sync.RWMutex
	dirs     []string
	inputDir string

	cache map[string]cachedWorkflow
}

type cachedWorkflow struct {
	modTime time.Time
	size    int64
	wf      Workflow
}

// NewLibrary builds a library over the given workflow directories.
func NewLibrary(dirs []string, inputDir string) *Library {
	return &Library{dirs: dirs, inputDir: inputDir, cache: map[string]cachedWorkflow{}}
}

// Configure swaps the scanned directories, dropping the parse cache.
func (l *Library) Configure(dirs []string, inputDir string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dirs = append([]string(nil), dirs...)
	l.inputDir = inputDir
	l.cache = map[string]cachedWorkflow{}
}

// InputDir reports the configured ComfyUI input directory.
func (l *Library) InputDir() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.inputDir
}

// Scan parses every workflow file found in the configured directories. Files
// whose size and mtime are unchanged are served from cache.
func (l *Library) Scan() ([]Workflow, error) {
	l.mu.RLock()
	dirs := append([]string(nil), l.dirs...)
	l.mu.RUnlock()

	var out []Workflow
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable subtree: skip rather than abort the scan
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && path != dir {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(d.Name()), ".json") {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if seen[path] {
				return nil
			}
			seen[path] = true
			info, err := d.Info()
			if err != nil {
				return nil
			}
			out = append(out, l.load(path, dir, info))
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return out, err
		}
	}

	sort.Slice(out, func(i, j int) bool {
		// Workflows that actually contain H3 variants come first, newest first.
		li, lj := len(out[i].Variants) > 0, len(out[j].Variants) > 0
		if li != lj {
			return li
		}
		return out[i].ModTime.After(out[j].ModTime)
	})
	return out, nil
}

func (l *Library) load(path, dir string, info fs.FileInfo) Workflow {
	l.mu.RLock()
	cached, ok := l.cache[path]
	l.mu.RUnlock()
	if ok && cached.modTime.Equal(info.ModTime()) && cached.size == info.Size() {
		return cached.wf
	}

	wf := Workflow{
		File:     path,
		Name:     strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Dir:      dir,
		ModTime:  info.ModTime(),
		Size:     info.Size(),
		Variants: []Variant{},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		wf.Error = err.Error()
		return wf
	}
	g, err := ParseGraph(data)
	if err != nil {
		wf.Error = err.Error()
		return wf
	}
	if variants := ExtractVariants(g, l.InputExists); variants != nil {
		wf.Variants = variants
	}
	wf.Models = SummarizeModels(g)

	l.mu.Lock()
	l.cache[path] = cachedWorkflow{modTime: info.ModTime(), size: info.Size(), wf: wf}
	l.mu.Unlock()
	return wf
}

// FindVariant locates a variant by workflow path and variant id.
func (l *Library) FindVariant(file, variantID string) (Workflow, Variant, error) {
	workflows, err := l.Scan()
	if err != nil {
		return Workflow{}, Variant{}, err
	}
	for _, wf := range workflows {
		if wf.File != file {
			continue
		}
		for _, v := range wf.Variants {
			if v.ID == variantID {
				return wf, v, nil
			}
		}
		return wf, Variant{}, fmt.Errorf("variant %q not found in %s", variantID, file)
	}
	return Workflow{}, Variant{}, fmt.Errorf("workflow %q not found", file)
}

// ------------------------------------------------------------ input files --

// InputImage is a file in the ComfyUI input directory.
type InputImage struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true,
	".bmp": true, ".gif": true, ".tif": true, ".tiff": true,
}

// CleanInputName strips the "[input]"-style annotations ComfyUI appends to
// LoadImage widget values and normalises the relative path.
func CleanInputName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndex(name, " ["); i > 0 && strings.HasSuffix(name, "]") {
		name = strings.TrimSpace(name[:i])
	}
	name = strings.ReplaceAll(name, "\\", "/")
	return strings.TrimPrefix(name, "./")
}

// ResolveInputPath maps a LoadImage filename to an absolute path, refusing any
// name that escapes the input directory.
func (l *Library) ResolveInputPath(name string) (string, error) {
	root := l.InputDir()
	if root == "" {
		return "", errors.New("ComfyUI input directory is not configured")
	}
	clean := CleanInputName(name)
	if clean == "" {
		return "", errors.New("empty file name")
	}
	abs := filepath.Join(root, filepath.Clean("/"+clean))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q escapes the input directory", name)
	}
	return abs, nil
}

// InputExists reports whether a LoadImage filename resolves to a real file.
func (l *Library) InputExists(name string) bool {
	path, err := l.ResolveInputPath(name)
	if err != nil {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// InputImages lists the images available in the ComfyUI input directory.
func (l *Library) InputImages() ([]InputImage, error) {
	root := l.InputDir()
	if root == "" {
		return nil, errors.New("ComfyUI input directory is not configured")
	}
	var out []InputImage
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !imageExts[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() == 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		out = append(out, InputImage{
			Name:    filepath.ToSlash(rel),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}
