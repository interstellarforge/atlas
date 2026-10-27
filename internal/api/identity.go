package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
)

func (s *Server) registerIdentity(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.HandleFunc("POST /api/v1/auth/register", s.register)
	mux.HandleFunc("GET /api/v1/auth/policy", s.publicPolicy)
	mux.Handle("PUT /api/v1/me/email", protect(s.changeEmail))
	mux.Handle("DELETE /api/v1/me", protect(s.deleteAccount))
	mux.Handle("PUT /api/v1/me/avatar", protect(s.putAvatar))
	mux.Handle("DELETE /api/v1/me/avatar", protect(s.deleteAvatar))
	mux.Handle("GET /api/v1/users/{id}/avatar", protect(s.getAvatar))
	mux.Handle("GET /api/v1/me/security", protect(s.getSecurity))
	mux.Handle("POST /api/v1/me/totp/setup", protect(s.totpSetup))
	mux.Handle("POST /api/v1/me/totp/enable", protect(s.totpEnable))
	mux.Handle("POST /api/v1/me/totp/disable", protect(s.totpDisable))
	mux.Handle("GET /api/v1/admin/policy", protect(s.adminGetPolicy))
	mux.Handle("PUT /api/v1/admin/policy", protect(s.adminSetPolicy))
	mux.Handle("GET /api/v1/admin/invites", protect(s.adminInvites))
	mux.Handle("POST /api/v1/admin/invites", protect(s.adminCreateInvite))
	mux.Handle("DELETE /api/v1/admin/invites/{id}", protect(s.adminRevokeInvite))
	mux.Handle("GET /api/v1/admin/audit/export", protect(s.adminAuditExport))
}

// publicPolicy tells the login page whether to offer registration and how long passwords must be.
func (s *Server) publicPolicy(w http.ResponseWriter, r *http.Request) {
	p := s.core.Policy()
	writeJSON(w, http.StatusOK, map[string]any{"registration": p.Registration, "min_password_length": p.MinPasswordLength, "allowed_domain": p.AllowedDomain})
}

// register is open to everyone, so it shares the login failure limiter against invite guessing.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email, Name, Password, Invite string
	}
	if !decode(w, r, &in) {
		return
	}
	key := "register|" + s.clientKey(r)
	if s.auth.Throttled(key) {
		w.Header().Set("Retry-After", "900")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts")
		return
	}
	u, err := s.core.Register(r.Context(), in.Email, in.Name, in.Password, in.Invite)
	if err != nil {
		// Only refusals count against the limiter (invite guessing, closed doors); typos and weak passwords do not.
		if errors.Is(err, core.ErrForbidden) || (in.Invite != "" && core.ErrorInfo(err).Code == "rule_violation") {
			s.auth.Failed(key)
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": u})
}

func (s *Server) changeEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := s.core.ChangeEmail(r.Context(), actor(r), in.Email, in.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.core.DeleteAccount(r.Context(), actor(r), in.Password); err != nil {
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) putAvatar(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUpload+maxBodyBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "expected multipart/form-data with a file part")
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeError(w, http.StatusRequestEntityTooLarge, "too_large", "file exceeds the upload limits")
			} else {
				writeError(w, http.StatusBadRequest, "invalid", "missing file part")
			}
			return
		}
		if part.FormName() != "file" {
			continue
		}
		if err := s.core.SetAvatar(r.Context(), actor(r), part.FileName(), part); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				err = core.ErrTooLarge
			}
			s.fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
}

func (s *Server) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	if err := s.core.RemoveAvatar(r.Context(), actor(r)); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	path, err := s.core.AvatarThumb(r.Context(), actor(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer f.Close()
	serveImage(w, r, f, "image/jpeg", "avatar.jpg")
}

func (s *Server) getSecurity(w http.ResponseWriter, r *http.Request) {
	sec, err := s.core.GetSecurity(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sec)
}

func (s *Server) totpSetup(w http.ResponseWriter, r *http.Request) {
	secret, uri, err := s.core.TOTPSetup(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secret": secret, "uri": uri})
}

func (s *Server) totpEnable(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	codes, err := s.core.TOTPEnable(r.Context(), actor(r), in.Code)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (s *Server) totpDisable(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.core.TOTPDisable(r.Context(), actor(r), in.Password, in.Code); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminGetPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := s.core.GetPolicy(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) adminSetPolicy(w http.ResponseWriter, r *http.Request) {
	var in core.Policy
	if !decode(w, r, &in) {
		return
	}
	p, err := s.core.SetPolicy(r.Context(), actor(r), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) adminInvites(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListInvites(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) adminCreateInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email   string `json:"email"`
		IsAdmin bool   `json:"is_admin"`
		Days    int    `json:"days"`
	}
	if !decode(w, r, &in) {
		return
	}
	inv, code, err := s.core.CreateInvite(r.Context(), actor(r), in.Email, in.IsAdmin, in.Days)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"invite": inv, "code": code})
}

func (s *Server) adminRevokeInvite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid invite id")
		return
	}
	if err := s.core.RevokeInvite(r.Context(), actor(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminAuditExport streams the audit log, newest first, as CSV (default) or JSON lines, with the same filters as
// the list. At most exportMax entries per download; use before= to continue.
func (s *Server) adminAuditExport(w http.ResponseWriter, r *http.Request) {
	const exportMax = 50000
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	csvOut := q.Get("format") != "json"
	if csvOut {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="atlas-audit.csv"`)
	} else {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="atlas-audit.jsonl"`)
	}
	cw := csv.NewWriter(w)
	enc := json.NewEncoder(w)
	started, sent := false, 0
	for sent < exportMax {
		page, err := s.core.AuditLog(r.Context(), actor(r), core.AuditFilter{Actor: q.Get("actor"), Operation: q.Get("operation"), Target: q.Get("target"), Before: before, Limit: 200})
		if err != nil {
			if !started {
				w.Header().Del("Content-Disposition")
				s.fail(w, r, err)
			}
			return
		}
		if !started {
			started = true
			if csvOut {
				_ = cw.Write([]string{"id", "time", "actor", "operation", "target", "batch_id", "changes"})
			}
		}
		for _, e := range page {
			if csvOut {
				_ = cw.Write([]string{strconv.FormatInt(e.ID, 10), time.UnixMilli(e.TS).UTC().Format(time.RFC3339), e.Actor, e.Operation, e.Target, e.BatchID, e.Changes})
			} else {
				_ = enc.Encode(e)
			}
			before = e.ID
			sent++
		}
		if len(page) < 200 {
			break
		}
	}
	cw.Flush()
}
