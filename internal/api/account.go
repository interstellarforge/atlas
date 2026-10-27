package api

import (
	"net/http"
	"strconv"

	"github.com/kees/atlas/internal/core"
)

func (s *Server) registerAccount(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("PATCH /api/v1/me", protect(s.updateProfile))
	mux.Handle("PUT /api/v1/me/password", protect(s.changePassword))
	mux.Handle("GET /api/v1/me/sessions", protect(s.listSessions))
	mux.Handle("DELETE /api/v1/me/sessions/{id}", protect(s.revokeSession))
	mux.Handle("GET /api/v1/me/preferences", protect(s.getPreferences))
	mux.Handle("PUT /api/v1/me/preferences", protect(s.putPreferences))
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := s.core.UpdateProfile(r.Context(), actor(r), in.Name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.core.ChangePassword(r.Context(), actor(r), in.Current, in.New); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListSessions(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid session id")
		return
	}
	if err := s.core.RevokeSession(r.Context(), actor(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request) {
	p, err := s.core.GetPreferences(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) putPreferences(w http.ResponseWriter, r *http.Request) {
	var in core.Preferences
	if !decode(w, r, &in) {
		return
	}
	p, err := s.core.SetPreferences(r.Context(), actor(r), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) registerProjectTokens(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/projects/{key}/tokens", protect(s.listProjectTokens))
	mux.Handle("POST /api/v1/projects/{key}/tokens", protect(s.createProjectToken))
	mux.Handle("DELETE /api/v1/projects/{key}/tokens/{id}", protect(s.revokeProjectToken))
}

func (s *Server) listProjectTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListProjectTokens(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) createProjectToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string `json:"name"`
		Write     bool   `json:"write"`
		ExpiresAt *int64 `json:"expires_at"`
	}
	if !decode(w, r, &in) {
		return
	}
	tok, secret, err := s.core.CreateProjectToken(r.Context(), actor(r), r.PathValue("key"), in.Name, in.Write, in.ExpiresAt)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": tok, "secret": secret})
}

func (s *Server) revokeProjectToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid token id")
		return
	}
	if err := s.core.RevokeProjectToken(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
