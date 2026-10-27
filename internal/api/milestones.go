package api

import (
	"net/http"
	"strconv"
)

func (s *Server) listMilestones(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListMilestones(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) createMilestone(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		DueAt *int64 `json:"due_at"`
	}
	if !decode(w, r, &in) {
		return
	}
	m, err := s.core.CreateMilestone(r.Context(), actor(r), r.PathValue("key"), in.Name, in.DueAt)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func milestoneID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "id must be a number")
		return 0, false
	}
	return id, true
}

func (s *Server) patchMilestone(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	id, ok := milestoneID(w, r)
	if !ok || !decode(w, r, &patch) {
		return
	}
	m, err := s.core.UpdateMilestone(r.Context(), actor(r), r.PathValue("key"), id, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) deleteMilestone(w http.ResponseWriter, r *http.Request) {
	id, ok := milestoneID(w, r)
	if !ok {
		return
	}
	if err := s.core.DeleteMilestone(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
