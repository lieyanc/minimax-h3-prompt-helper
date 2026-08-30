package api

import (
	"context"
	"net/http"
	"time"

	"h3helper/internal/questions"
	"h3helper/internal/task"
)

// handlePlan asks the question agent what to put on the next page. The agent
// reads the skill, the workflow constraints and the vision facts, so the
// questions follow the reference assets actually in front of it instead of a
// fixed table. When it cannot be reached the deterministic table stands in.
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := s.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if pending := visionBlockers(t); len(pending) > 0 {
		writeError(w, http.StatusBadRequest, visionGateMessage(pending))
		return
	}

	// A page that is still being answered is not replaced unless asked for.
	if len(t.Pending) > 0 && r.URL.Query().Get("force") != "1" {
		writeJSON(w, http.StatusOK, s.taskView(t))
		return
	}

	var (
		page   task.Page
		reason string
	)
	client, cerr := s.writerClient()
	if cerr != nil {
		reason = cerr.Error()
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
		defer cancel()
		page, err = questions.Next(ctx, client, t)
		if err != nil {
			reason = err.Error()
		}
	}
	if reason != "" {
		page = questions.Fallback(t, reason)
	}

	updated, err := s.store.Update(id, func(t *task.Task) error {
		applyPage(t, page)
		t.PlanError = reason
		return nil
	})
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.taskView(updated))
}
