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
	mux.Handle("GET /api/v1/admin/mail", protect(s.adminMail))
	mux.Handle("PUT /api/v1/admin/mail", protect(s.adminSaveMail))
	mux.Handle("DELETE /api/v1/admin/mail", protect(s.adminClearMail))
	mux.Handle("POST /api/v1/admin/mail/test", protect(s.adminTestMail))
	mux.Handle("GET /api/v1/roles", protect(s.roles))
	mux.Handle("PUT /api/v1/admin/roles/{name}", protect(s.adminSaveRole))
	mux.Handle("DELETE /api/v1/admin/roles/{name}", protect(s.adminDeleteRole))
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
		// ForcePasswordReset ends the user's sessions and makes the next one change the password first.
		ForcePasswordReset bool `json:"force_password_reset"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.IsAdmin == nil && in.Active == nil && !in.ResetTOTP && !in.ForcePasswordReset {
		writeError(w, http.StatusBadRequest, "invalid", "give is_admin, active, reset_totp and/or force_password_reset")
		return
	}
	a := actor(r)
	if in.ResetTOTP {
		if err := s.core.ResetUserTOTP(r.Context(), a, id); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if in.ForcePasswordReset {
		if err := s.core.ForcePasswordReset(r.Context(), a, id); err != nil {
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

// adminInstance shows the effective, read-only instance settings (they come from the environment).
func (s *Server) adminInstance(w http.ResponseWriter, r *http.Request) {
	if a := actor(r); !a.IsAdmin || a.Kind != "human" {
		writeError(w, http.StatusForbidden, "forbidden", "not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.instance})
}

// roles lists the project roles with their actions, and the action names the matrix is built from.
func (s *Server) roles(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Roles(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "actions": core.Actions, "default_role": s.core.Policy().DefaultRole})
}

// adminSaveRole creates a custom role or edits viewer, member or a custom role: {"actions": [...]}.
func (s *Server) adminSaveRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Actions []string `json:"actions"`
	}
	if !decode(w, r, &in) {
		return
	}
	role, err := s.core.SaveRole(r.Context(), actor(r), r.PathValue("name"), in.Actions)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (s *Server) adminDeleteRole(w http.ResponseWriter, r *http.Request) {
	if err := s.core.DeleteRole(r.Context(), actor(r), r.PathValue("name")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminMail(w http.ResponseWriter, r *http.Request) {
	m, err := s.core.GetMailSettings(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// adminSaveMail stores the SMTP settings. "password" omitted keeps the stored one, "" removes it; it is never returned.
func (s *Server) adminSaveMail(w http.ResponseWriter, r *http.Request) {
	var in core.MailInput
	if !decode(w, r, &in) {
		return
	}
	m, err := s.core.SetMailSettings(r.Context(), actor(r), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) adminClearMail(w http.ResponseWriter, r *http.Request) {
	m, err := s.core.ClearMailSettings(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) adminTestMail(w http.ResponseWriter, r *http.Request) {
	if err := s.core.SendTestMail(r.Context(), actor(r)); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
