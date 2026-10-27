package api

import (
	"net/http"
	"strconv"

	"github.com/kees/atlas/internal/core"
)

func (s *Server) registerAdmin(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/admin/users", protect(s.adminUsers))
	mux.Handle("PATCH /api/v1/admin/users/{id}", protect(s.adminPatchUser))
	mux.Handle("GET /api/v1/admin/audit", protect(s.adminAudit))
	mux.Handle("GET /api/v1/admin/instance", protect(s.adminInstance))
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListAdminUsers(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// adminPatchUser accepts {"is_admin": bool} and/or {"active": bool}.
func (s *Server) adminPatchUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid user id")
		return
	}
	var in struct {
		IsAdmin   *bool `json:"is_admin"`
		Active    *bool `json:"active"`
		ResetTOTP bool  `json:"reset_totp"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.IsAdmin == nil && in.Active == nil && !in.ResetTOTP {
		writeError(w, http.StatusBadRequest, "invalid", "give is_admin and/or active")
		return
	}
	a := actor(r)
	if in.ResetTOTP {
		if err := s.core.ResetUserTOTP(r.Context(), a, id); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if in.IsAdmin != nil {
		if err := s.core.SetUserAdmin(r.Context(), a, id, *in.IsAdmin); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if in.Active != nil {
		if err := s.core.SetUserActive(r.Context(), a, id, *in.Active); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	list, err := s.core.AuditLog(r.Context(), actor(r), core.AuditFilter{
		Actor: q.Get("actor"), Operation: q.Get("operation"), Target: q.Get("target"), Before: before, Limit: limit})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// adminInstance shows the effective, read-only instance settings (they come from the environment) and the rights matrix.
func (s *Server) adminInstance(w http.ResponseWriter, r *http.Request) {
	if a := actor(r); !a.IsAdmin || a.Kind != "human" {
		writeError(w, http.StatusForbidden, "forbidden", "not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.instance, "rights": core.RightsMatrix()})
}
