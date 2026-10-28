// Package api holds the HTTP adapters. Domain logic lives in core services.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/events"
	"github.com/kees/atlas/internal/logbuf"
	"github.com/kees/atlas/internal/media"
	"github.com/kees/atlas/internal/update"
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
	MCPStats     http.Handler    // aggregate MCP call metrics; may be nil
	Logs         *logbuf.Buffer  // recent server log records for administrators; may be nil
	Instance     map[string]any  // effective settings shown read-only to admins
	Update       *update.Checker // newer-release check for admins; nil disables it
	Media        *media.Store
	MaxUpload    int64 // bytes per uploaded file
	// TrustedProxies are the proxies whose X-Forwarded-For is believed; empty means the connection address is the client.
	TrustedProxies []netip.Prefix
}

type Server struct {
	db           *db.DB
	auth         *auth.Service
	core         *core.Service
	feed         *events.Feed
	cookieSecure bool
	spa          fs.FS
	mcp          http.Handler
	mcpStats     http.Handler
	logs         *logbuf.Buffer
	instance     map[string]any
	update       *update.Checker
	media        *media.Store
	maxUpload    int64
	proxies      []netip.Prefix
}

func New(d Deps) *Server {
	return &Server{db: d.DB, auth: d.Auth, core: d.Core, feed: d.Feed, cookieSecure: d.CookieSecure, spa: d.SPA, mcp: d.MCP, mcpStats: d.MCPStats, logs: d.Logs, instance: d.Instance, update: d.Update, media: d.Media, maxUpload: d.MaxUpload, proxies: d.TrustedProxies}
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
	s.registerTransfer(mux, protect)
	s.registerAccount(mux, protect)
	s.registerProjectTokens(mux, protect)
	s.registerAdmin(mux, protect)
	s.registerTemplates(mux, protect)
	s.registerIdentity(mux, protect)
	s.registerInsight(mux, protect)
	s.registerContext(mux, protect)
	mux.Handle("GET /api/v1/events", protect(s.events))
	mux.Handle("GET /api/v1/changes", protect(s.changes))
	if s.mcpStats != nil {
		// Admins only: the answer says who is connected.
		mux.Handle("GET /api/v1/mcp/stats", protect(func(w http.ResponseWriter, r *http.Request) {
			if a := actor(r); a.Kind != auth.KindHuman || !a.IsAdmin {
				writeError(w, http.StatusForbidden, "forbidden", "administrators only")
				return
			}
			s.mcpStats.ServeHTTP(w, r)
		}))
	}
	if s.mcp != nil {
		// MCP is POST-only, so the method-based scope check would demand write for reads;
		// scopes are enforced per tool in core instead.
		mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
			if _, ok := auth.ActorFrom(r.Context()); !ok {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, mcpMaxBody)
			s.mcp.ServeHTTP(w, r)
		})
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "unknown endpoint")
	})
	mux.Handle("/", s.spaHandler())

	return s.logRequests(s.auth.Authenticate(securityHeaders(s.cookieSecure, mux)))
}

func securityHeaders(hsts bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if hsts { // cookies are secure-only, so the deployment is https: tell browsers to stay on it
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) WriteHeader(code int) {
	w.status, w.wrote = code, true
	w.ResponseWriter.WriteHeader(code)
}

// Flush keeps SSE working through the wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// mcpMaxBody caps one MCP request; tool arguments are small (uploads are not possible through MCP).
const mcpMaxBody = 1 << 20

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

		func() {
			defer func() {
				if p := recover(); p != nil && p != http.ErrAbortHandler {
					slog.Error("panic in handler", "request_id", id, "method", r.Method, "path", r.URL.Path, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
					if !sw.wrote {
						writeError(sw, http.StatusInternalServerError, "internal", "internal error")
					}
					sw.status = http.StatusInternalServerError
				}
			}()
			next.ServeHTTP(sw, r)
		}()

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

// clientKey identifies the caller for rate limits: the connection address, or, when that is a trusted proxy,
// the first address in X-Forwarded-For (from the right) that is not itself a trusted proxy.
func (s *Server) clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if len(s.proxies) == 0 || !s.trusted(host) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if _, err := netip.ParseAddr(hop); err != nil {
			break // garbage from the client side of the chain: stop trusting it
		}
		if !s.trusted(hop) {
			return hop
		}
	}
	return host
}

func (s *Server) trusted(host string) bool {
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	for _, p := range s.proxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
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
	err := dec.Decode(v)
	if err == nil {
		if _, more := dec.Token(); more != io.EOF { // one JSON document only: no trailing values or garbage
			err = errors.New("trailing data")
		}
	}
	if err != nil {
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
		// Discovery probes (OAuth metadata) must not get HTML: clients parse it as JSON and abort.
		if strings.HasPrefix(name, ".well-known/") {
			http.NotFound(w, r)
			return
		}
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
