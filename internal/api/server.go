// Package api holds the HTTP adapters. Domain logic lives in core services.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/events"
)

const maxBodyBytes = 1 << 20

// Deps are the collaborators of the HTTP server. MCP is mounted at /mcp and may be nil.
type Deps struct {
	DB           *db.DB
	Auth         *auth.Service
	Core         *core.Service
	Feed         *events.Feed
	CookieSecure bool
	SPA          fs.FS
	MCP          http.Handler
}

type Server struct {
	db           *db.DB
	auth         *auth.Service
	core         *core.Service
	feed         *events.Feed
	cookieSecure bool
	spa          fs.FS
	mcp          http.Handler
}

func New(d Deps) *Server {
	return &Server{db: d.DB, auth: d.Auth, core: d.Core, feed: d.Feed, cookieSecure: d.CookieSecure, spa: d.SPA, mcp: d.MCP}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	protect := func(h http.HandlerFunc) http.Handler { return auth.Require(writeError, h) }

	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.Handle("POST /api/v1/auth/logout", protect(s.logout))
	mux.Handle("GET /api/v1/auth/me", protect(s.me))
	mux.Handle("GET /api/v1/tokens", protect(s.listTokens))
	mux.Handle("POST /api/v1/tokens", protect(s.createToken))
	mux.Handle("DELETE /api/v1/tokens/{id}", protect(s.revokeToken))
	s.registerWork(mux, protect)
	s.registerBoard(mux, protect)
	mux.Handle("GET /api/v1/events", protect(s.events))
	mux.Handle("GET /api/v1/changes", protect(s.changes))
	if s.mcp != nil {
		// MCP is POST-only, so the method-based scope check would demand write for reads;
		// scopes are enforced per tool in core instead.
		mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
			if _, ok := auth.ActorFrom(r.Context()); !ok {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
				return
			}
			s.mcp.ServeHTTP(w, r)
		})
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "unknown endpoint")
	})
	mux.Handle("/", s.spaHandler())

	return s.logRequests(s.auth.Authenticate(securityHeaders(mux)))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush keeps SSE working through the wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		holder := &auth.Holder{}
		r = r.WithContext(auth.WithHolder(r.Context(), holder))
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		actor := "anonymous"
		if holder.Actor != nil {
			actor = holder.Actor.String()
		}
		level := slog.LevelInfo
		if sw.status >= 500 {
			level = slog.LevelError
		}
		slog.Log(r.Context(), level, "request",
			"request_id", id, "method", r.Method, "path", r.URL.Path,
			"status", sw.status, "duration_ms", time.Since(start).Milliseconds(), "actor", actor)
	})
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError follows docs/api-contract.md: {"error":{"code","message"}}.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid", "malformed JSON body")
		}
		return false
	}
	return true
}

func (s *Server) spaHandler() http.Handler {
	files := http.FileServerFS(s.spa)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(s.spa, name); err != nil {
			// Client-side routes fall back to the SPA shell.
			if _, err := fs.Stat(s.spa, "index.html"); err != nil {
				http.NotFound(w, r)
				return
			}
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}
