package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/kees/atlas/internal/auth"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.db.R.PingContext(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	user, session, csrf, err := s.auth.Login(in.Email, in.Password, clientKey(r))
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts")
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid email or password")
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: auth.SessionCookie, Value: session, Path: "/", HttpOnly: true,
		Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(auth.SessionTTL),
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "csrf_token": csrf})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		if err := s.auth.Logout(c.Value); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: auth.SessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	a, _ := auth.ActorFrom(r.Context())
	user, err := s.auth.User(a.UserID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "csrf_token": a.CSRF, "actor": a.String()})
}

// Tokens are managed by humans only: a token must not be able to mint more tokens.
func (s *Server) humanOnly(w http.ResponseWriter, r *http.Request) (auth.Actor, bool) {
	a, _ := auth.ActorFrom(r.Context())
	if a.Kind != auth.KindHuman {
		writeError(w, http.StatusForbidden, "forbidden", "tokens are managed through a user session")
		return a, false
	}
	return a, true
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	a, ok := s.humanOnly(w, r)
	if !ok {
		return
	}
	tokens, err := s.auth.ListTokens(a.UserID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tokens})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	a, ok := s.humanOnly(w, r)
	if !ok {
		return
	}
	var in struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		Projects  []string `json:"projects"`
		ExpiresAt *int64   `json:"expires_at"`
	}
	if !decode(w, r, &in) {
		return
	}
	tok, plain, err := s.auth.CreateToken(a.UserID, in.Name, in.Scopes, in.Projects, in.ExpiresAt)
	if errors.Is(err, auth.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "invalid", "name and scopes (read, write) are required")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	// The plaintext token is only ever returned here.
	writeJSON(w, http.StatusCreated, map[string]any{"token": tok, "secret": plain})
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	a, ok := s.humanOnly(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid token id")
		return
	}
	done, err := s.auth.RevokeToken(a.UserID, id)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !done {
		writeError(w, http.StatusNotFound, "not_found", "token not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("internal error", "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "internal", "internal error")
}
