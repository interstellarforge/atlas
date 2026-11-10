package api

import (
	"net/http"
	"strconv"
)

// Support access (ADR-0031): only an ADMIN of the organization manages grants; the core refuses everyone else,
// SUPERADMIN under a grant included.
func (s *Server) registerSupport(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/organizations/{key}/support-grants", protect(s.listSupportGrants))
	mux.Handle("POST /api/v1/organizations/{key}/support-grants", protect(s.grantSupport))
	mux.Handle("DELETE /api/v1/organizations/{key}/support-grants/{id}", protect(s.revokeSupport))
}

func (s *Server) listSupportGrants(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListSupportGrants(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// grantSupport takes {"level": "read_only"|"full", "hours": 1-720} or {"level": ..., "until_revoked": true}.
func (s *Server) grantSupport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Level        string `json:"level"`
		Hours        int    `json:"hours"`
		UntilRevoked bool   `json:"until_revoked"`
	}
	if !decode(w, r, &in) {
		return
	}
	g, err := s.core.GrantSupport(r.Context(), actor(r), r.PathValue("key"), in.Level, in.Hours, in.UntilRevoked)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) revokeSupport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid grant id")
		return
	}
	if err := s.core.RevokeSupport(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
