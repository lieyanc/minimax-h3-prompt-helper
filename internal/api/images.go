package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"h3helper/internal/task"
)

// maxUploadBytes caps one reference image. Reference frames are stills, not
// footage, so this is generous.
const maxUploadBytes = 32 << 20

var uploadExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true,
	".bmp": true, ".gif": true, ".tif": true, ".tiff": true,
}

// handleUploadImage stores a reference image the user picked by hand. The file
// is kept next to the task data — ComfyUI's own input directory is never
// written to — so the answer to "where does this picture come from" stays
// honest, and the task carries a notice telling the user to upload it in
// ComfyUI as well.
func (s *Server) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.Get(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "读取上传内容失败: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请求里没有 file 字段")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !uploadExts[ext] {
		writeError(w, http.StatusBadRequest, "只支持 png / jpg / webp / bmp / gif / tiff 图片")
		return
	}

	root, err := s.uploadRoot(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "创建上传目录失败: "+err.Error())
		return
	}

	name := uniqueName(root, header.Filename)
	dst, err := os.OpenFile(filepath.Join(root, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "写入文件失败: "+err.Error())
		return
	}
	written, err := io.Copy(dst, io.LimitReader(file, maxUploadBytes))
	closeErr := dst.Close()
	if err != nil || closeErr != nil || written == 0 {
		_ = os.Remove(filepath.Join(root, name))
		writeError(w, http.StatusInternalServerError, "保存图片失败")
		return
	}

	label := strings.TrimSpace(r.FormValue("label"))
	updated, err := s.store.Update(id, func(t *task.Task) error {
		attachUpload(t, label, name)
		invalidatePlan(t)
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.taskView(updated))
}

// handleDeleteImage drops a manually uploaded image, restoring the workflow's
// own picture when the upload was standing in for one.
func (s *Server) handleDeleteImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	label := r.PathValue("label")

	var removed string
	updated, err := s.store.Update(id, func(t *task.Task) error {
		for i := range t.Images {
			img := t.Images[i]
			if img.Label != label {
				continue
			}
			if img.Origin != task.OriginUpload {
				return fmt.Errorf("%s 来自工作流，不能在这里删除", label)
			}
			removed = img.File
			if img.Wired {
				t.Images[i].Origin = task.OriginWorkflow
				t.Images[i].Source = img.Replaces
				t.Images[i].File = ""
				t.Images[i].Replaces = ""
				t.Images[i].Missing = img.Replaces == "" || !s.lib.InputExists(img.Replaces)
				dropLabelData(t, label)
				invalidatePlan(t)
				return nil
			}
			t.Images = append(t.Images[:i], t.Images[i+1:]...)
			dropLabelData(t, label)
			relabel(t)
			invalidatePlan(t)
			return nil
		}
		return fmt.Errorf("任务里没有 %s", label)
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if removed != "" {
		if path, perr := s.uploadPath(id, removed); perr == nil {
			_ = os.Remove(path)
		}
	}
	writeJSON(w, http.StatusOK, s.taskView(updated))
}

// handleUploadedImage serves a file from a task's upload directory.
func (s *Server) handleUploadedImage(w http.ResponseWriter, r *http.Request) {
	path, err := s.uploadPath(r.PathValue("id"), r.PathValue("file"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		writeError(w, http.StatusNotFound, "图片不存在")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, path)
}

// imagePath resolves one attached image to a file on disk, wherever it came
// from.
func (s *Server) imagePath(t *task.Task, img task.Image) (string, error) {
	if img.Origin == task.OriginUpload {
		if img.File == "" {
			return "", errors.New("上传记录缺少文件名")
		}
		return s.uploadPath(t.ID, img.File)
	}
	if img.Source == "" || img.Missing {
		return "", errors.New("参考图不在 ComfyUI input 目录里，跳过")
	}
	return s.lib.ResolveInputPath(img.Source)
}

// ------------------------------------------------------------------ paths --

func (s *Server) uploadRoot(taskID string) (string, error) {
	if s.uploads == "" {
		return "", errors.New("上传目录没有配置")
	}
	if !safeName(taskID) {
		return "", errors.New("任务 id 不合法")
	}
	return filepath.Join(s.uploads, taskID), nil
}

func (s *Server) uploadPath(taskID, name string) (string, error) {
	root, err := s.uploadRoot(taskID)
	if err != nil {
		return "", err
	}
	clean := filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if clean == "" || clean == "." || clean == ".." || strings.ContainsRune(clean, os.PathSeparator) {
		return "", errors.New("文件名不合法")
	}
	return filepath.Join(root, clean), nil
}

// safeName keeps path traversal out of the ids and names that become
// directories.
func safeName(s string) bool {
	if s == "" || strings.Contains(s, "/") || strings.Contains(s, "\\") || strings.Contains(s, "..") {
		return false
	}
	return true
}

// uniqueName keeps an upload from silently overwriting an earlier one.
func uniqueName(dir, original string) string {
	base := filepath.Base(strings.ReplaceAll(strings.TrimSpace(original), "\\", "/"))
	base = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == '/', r == '\\', r == ':', r == '*', r == '?', r == '"', r == '<', r == '>', r == '|':
			return '_'
		}
		return r
	}, base)
	if base == "" || base == "." || base == ".." {
		base = "reference.png"
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	name := base
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			return name
		}
		name = fmt.Sprintf("%s-%d%s", stem, i, ext)
	}
}

// ------------------------------------------------------------ task surgery --

// attachUpload either swaps an uploaded file in for an existing reference
// label, or adds a new label beyond what the workflow wires.
func attachUpload(t *task.Task, label, file string) {
	for i := range t.Images {
		if label == "" || t.Images[i].Label != label {
			continue
		}
		img := &t.Images[i]
		if img.Origin != task.OriginUpload {
			img.Replaces = img.Source
		}
		img.Origin = task.OriginUpload
		img.File = file
		img.Source = ""
		img.Missing = false
		// The picture behind the label changed, so what the vision model said
		// about it and what the user decided about it were both about a
		// different image.
		dropLabelData(t, label)
		return
	}

	t.Images = append(t.Images, task.Image{
		Slot:   "手动上传",
		Role:   "reference",
		Origin: task.OriginUpload,
		File:   file,
		Wired:  false,
	})
	relabel(t)
}

// relabel renumbers the picture labels so they stay contiguous — the validator
// rejects a gap — and carries every key that mentions a label along with the
// change.
func relabel(t *task.Task) {
	mapping := map[string]string{}
	labels := make([]string, 0, len(t.Images))
	for i := range t.Images {
		want := fmt.Sprintf("<Picture %d>", i+1)
		if old := t.Images[i].Label; old != "" && old != want {
			mapping[old] = want
		}
		t.Images[i].Label = want
		labels = append(labels, want)
	}
	t.Constraints.PictureLabels = labels
	if len(mapping) > 0 {
		remapLabels(t, mapping)
	}
}

// remapLabels rewrites every stored mention of a reference label. It runs in
// two passes through placeholder tokens, because a renumbering can map one old
// label onto another old label's name.
func remapLabels(t *task.Task, mapping map[string]string) {
	tokens := map[string]string{}
	i := 0
	for old := range mapping {
		tokens[old] = fmt.Sprintf("\x00label%d\x00", i)
		i++
	}
	rewrite := func(s string) string {
		for old, tok := range tokens {
			s = strings.ReplaceAll(s, old, tok)
		}
		for old, tok := range tokens {
			s = strings.ReplaceAll(s, tok, mapping[old])
		}
		return s
	}

	answers := make(map[string]string, len(t.Answers))
	for k, v := range t.Answers {
		answers[rewrite(k)] = v
	}
	t.Answers = answers

	facts := make(map[string]task.Facts, len(t.Facts))
	for k, v := range t.Facts {
		v.Label = rewrite(v.Label)
		facts[rewrite(k)] = v
	}
	t.Facts = facts

	rewriteQuestions(t.Asked, rewrite)
	rewriteQuestions(t.Pending, rewrite)
	rewriteQuestions(t.Plan.Questions, rewrite)
	for i := range t.Rounds {
		rewriteQuestions(t.Rounds[i].Questions, rewrite)
	}
}

func rewriteQuestions(qs []task.Question, rewrite func(string) string) {
	for i := range qs {
		qs[i].Slot = rewrite(qs[i].Slot)
		qs[i].Label = rewrite(qs[i].Label)
		qs[i].Title = rewrite(qs[i].Title)
		qs[i].Help = rewrite(qs[i].Help)
		qs[i].EnLabel = rewrite(qs[i].EnLabel)
	}
}

// dropLabelData forgets everything that was said about one reference label,
// because the picture behind it is gone or has been replaced. The question
// agent invents its own slot keys, so a question is matched by the label it
// declared as well as by its key.
func dropLabelData(t *task.Task, label string) {
	delete(t.Facts, label)
	for _, q := range append(append([]task.Question{}, t.Asked...), t.Pending...) {
		if q.Label == label || strings.Contains(q.Slot, label) {
			delete(t.Answers, q.Slot)
		}
	}
	for k := range t.Answers {
		if strings.Contains(k, label) {
			delete(t.Answers, k)
		}
	}
	t.Asked = withoutLabel(t.Asked, label)
	t.Pending = withoutLabel(t.Pending, label)
	t.Plan.Questions = withoutLabel(t.Plan.Questions, label)
}

func withoutLabel(qs []task.Question, label string) []task.Question {
	var out []task.Question
	for _, q := range qs {
		if q.Label == label || strings.Contains(q.Slot, label) {
			continue
		}
		out = append(out, q)
	}
	return out
}

// invalidatePlan reopens the interview after the reference assets changed: what
// the agent decided to ask next was decided about a different set of pictures.
func invalidatePlan(t *task.Task) {
	t.Plan.Done = false
	t.Plan.Note = ""
	if t.Status == task.StatusReady {
		t.Status = task.StatusQuestioning
	}
}
