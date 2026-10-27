package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const (
	SessionCookie = "atlas_session"
	CSRFHeader    = "X-CSRF-Token"
)

// Authenticate resolves the caller (bearer token or session cookie) into the request
// context. It never rejects; use Require for routes that need an actor.
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			if a, err := s.TokenActor(strings.TrimPrefix(h, "Bearer ")); err == nil {
				r = r.WithContext(WithActor(r.Context(), a))
			}
		} else if c, err := r.Cookie(SessionCookie); err == nil {
			if a, err := s.SessionActor(c.Value); err == nil {
				// Cookie auth is ambient, so state-changing requests need the CSRF token.
				if safeMethod(r.Method) || subtle.ConstantTimeCompare([]byte(r.Header.Get(CSRFHeader)), []byte(a.CSRF)) == 1 {
					r = r.WithContext(WithActor(r.Context(), a))
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Require lets only authenticated callers through. Tokens need the read scope for
// safe methods and the write scope otherwise. onFail renders the error response.
func Require(onFail func(w http.ResponseWriter, status int, code, msg string), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := ActorFrom(r.Context())
		if !ok {
			onFail(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
			return
		}
		scope := ScopeWrite
		if safeMethod(r.Method) {
			scope = ScopeRead
		}
		if !a.Can(scope) {
			onFail(w, http.StatusForbidden, "forbidden", "token lacks "+scope+" scope")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
