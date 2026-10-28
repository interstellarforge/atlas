package api

import (
	"net/http"

	"github.com/kees/atlas/internal/core"
)

func (s *Server) listRecurrences(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListRecurrences(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) createRecurrence(w http.ResponseWriter, r *http.Request) {
	var in core.Recurrence
	if !decode(w, r, &in) {
		return
	}
	out, err := s.core.CreateRecurrence(r.Context(), actor(r), r.PathValue("key"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) patchRecurrence(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Active bool `json:"active"`
	}
	id, ok := milestoneID(w, r)
	if !ok || !decode(w, r, &in) {
		return
	}
	out, err := s.core.SetRecurrenceActive(r.Context(), actor(r), r.PathValue("key"), id, in.Active)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteRecurrence(w http.ResponseWriter, r *http.Request) {
	id, ok := milestoneID(w, r)
	if !ok {
		return
	}
	if err := s.core.DeleteRecurrence(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
