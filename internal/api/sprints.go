package api

import "net/http"

func (s *Server) listSprints(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListSprints(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) createSprint(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		StartAt int64  `json:"start_at"`
		EndAt   int64  `json:"end_at"`
	}
	if !decode(w, r, &in) {
		return
	}
	sp, err := s.core.CreateSprint(r.Context(), actor(r), r.PathValue("key"), in.Name, in.StartAt, in.EndAt)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, sp)
}

func (s *Server) patchSprint(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	id, ok := milestoneID(w, r)
	if !ok || !decode(w, r, &patch) {
		return
	}
	sp, err := s.core.UpdateSprint(r.Context(), actor(r), r.PathValue("key"), id, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sp)
}

func (s *Server) deleteSprint(w http.ResponseWriter, r *http.Request) {
	id, ok := milestoneID(w, r)
	if !ok {
		return
	}
	if err := s.core.DeleteSprint(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
