package api

import (
	"net/http"
	"strconv"
)

func (s *Server) listBatches(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.core.ListBatches(r.Context(), actor(r), r.PathValue("key"), limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) undoBatch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Force bool `json:"force"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.core.UndoBatch(r.Context(), actor(r), r.PathValue("id"), in.Force)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
