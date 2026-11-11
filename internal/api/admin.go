package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/logbuf"
	"github.com/kees/atlas/internal/update"
)

func (s *Server) registerAdmin(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/admin/users", protect(s.adminUsers))
	mux.Handle("PATCH /api/v1/admin/users/{id}", protect(s.adminPatchUser))
	mux.Handle("GET /api/v1/admin/audit", protect(s.adminAudit))
	mux.Handle("GET /api/v1/admin/instance", protect(s.adminInstance))
	mux.Handle("GET /api/v1/admin/update", protect(s.adminUpdate))
	mux.Handle("GET /api/v1/admin/logs", protect(s.adminLogs))
	mux.Handle("GET /api/v1/admin/mail", protect(s.adminMail))
	mux.Handle("PUT /api/v1/admin/mail", protect(s.adminSaveMail))
	mux.Handle("DELETE /api/v1/admin/mail", protect(s.adminClearMail))
	mux.Handle("POST /api/v1/admin/mail/test", protect(s.adminTestMail))
	mux.Handle("GET /api/v1/roles", protect(s.roles))
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	list, total, err := s.core.ListAdminUsers(r.Context(), actor(r), core.AdminUserFilter{Status: q.Get("status"), Q: q.Get("q"), Limit: limit, Offset: offset})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": total})
}

// adminPatchUser accepts {"is_superadmin": bool} and/or {"active": bool}.
func (s *Server) adminPatchUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid user id")
		return
	}
	var in struct {
		IsSuperadmin *bool `json:"is_superadmin"`
		Active       *bool `json:"active"`
		ResetTOTP    bool  `json:"reset_totp"`
		// ForcePasswordReset ends the user's sessions and makes the next one change the password first.
		ForcePasswordReset bool `json:"force_password_reset"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.IsSuperadmin == nil && in.Active == nil && !in.ResetTOTP && !in.ForcePasswordReset {
		writeError(w, http.StatusBadRequest, "invalid", "give is_superadmin, active, reset_totp and/or force_password_reset")
		return
	}
	if err := s.core.UpdateUser(r.Context(), actor(r), id, core.AdminUserPatch{IsSuperadmin: in.IsSuperadmin, Active: in.Active,
		ResetTOTP: in.ResetTOTP, ForcePasswordReset: in.ForcePasswordReset}); err != nil {
		s.fail(w, r, err)
		return
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
	if a := actor(r); !s.core.IsSuperadmin(a) {
		writeError(w, http.StatusForbidden, "forbidden", "not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.instance})
}

// adminUpdate reports whether a newer release exists; with the check disabled it reports nothing available.
func (s *Server) adminUpdate(w http.ResponseWriter, r *http.Request) {
	if a := actor(r); !s.core.IsSuperadmin(a) {
		writeError(w, http.StatusForbidden, "forbidden", "not allowed")
		return
	}
	if s.update == nil {
		writeJSON(w, http.StatusOK, update.Status{})
		return
	}
	writeJSON(w, http.StatusOK, s.update.Check(r.Context()))
}

// roles lists the project roles with their actions, and the action names the matrix is built from.
func (s *Server) roles(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Roles(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "actions": core.Actions})
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

// adminLogs serves the recent server log (newest first): ?level=debug|info|warn|error, ?q= text, ?limit=.
func (s *Server) adminLogs(w http.ResponseWriter, r *http.Request) {
	if a := actor(r); !s.core.IsSuperadmin(a) {
		writeError(w, http.StatusForbidden, "forbidden", "administrators only")
		return
	}
	f := logbuf.Filter{MinLevel: slog.LevelInfo, Query: r.URL.Query().Get("q")}
	if l := r.URL.Query().Get("level"); l != "" {
		if err := f.MinLevel.UnmarshalText([]byte(l)); err != nil {
			writeError(w, http.StatusBadRequest, "invalid", "level must be debug, info, warn or error")
			return
		}
	}
	f.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	items := []logbuf.Entry{}
	if s.logs != nil {
		items = s.logs.List(f)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
