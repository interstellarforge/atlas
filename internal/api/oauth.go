package api

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
)

// OAuth 2.1 endpoints for MCP clients (ATL-75, ADR-0025). The token logic lives in auth; these are adapters.

//go:embed oauth_page.html
var oauthPageSrc string

var oauthPage = template.Must(template.New("oauth").Parse(oauthPageSrc))

func (s *Server) registerOAuth(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.protectedResourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.protectedResourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.authServerMetadata)
	mux.HandleFunc("POST /oauth/register", s.oauthRegister)
	mux.HandleFunc("GET /oauth/authorize", s.oauthAuthorize)
	mux.HandleFunc("POST /oauth/authorize", s.oauthDecide)
	mux.HandleFunc("POST /oauth/token", s.oauthToken)
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server", "/oauth/register", "/oauth/token"} {
		mux.HandleFunc("OPTIONS "+p, func(w http.ResponseWriter, r *http.Request) {
			cors(w)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
			w.WriteHeader(http.StatusNoContent)
		})
	}
}

// cors lets browser-based MCP clients read the metadata and call register/token. No credentials are involved.
func cors(w http.ResponseWriter) { w.Header().Set("Access-Control-Allow-Origin", "*") }

// baseURL is the public address of the server: ATLAS_PUBLIC_URL, else the request's own scheme and host.
func (s *Server) baseURL(r *http.Request) string {
	if s.publicURL != "" {
		return s.publicURL
	}
	scheme := "http"
	if r.TLS != nil || s.cookieSecure {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) resourceMetadataURL(r *http.Request) string {
	return s.baseURL(r) + "/.well-known/oauth-protected-resource"
}

func (s *Server) protectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	cors(w)
	base := s.baseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base},
		"scopes_supported": []string{auth.ScopeRead, auth.ScopeWrite}, "bearer_methods_supported": []string{"header"}})
}

func (s *Server) authServerMetadata(w http.ResponseWriter, r *http.Request) {
	cors(w)
	base := s.baseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{"issuer": base, "authorization_endpoint": base + "/oauth/authorize", "token_endpoint": base + "/oauth/token",
		"registration_endpoint": base + "/oauth/register", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported": []string{auth.ScopeRead, auth.ScopeWrite}})
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	cors(w)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// oauthThrottled counts unauthenticated OAuth calls per client address, like failed logins.
func (s *Server) oauthThrottled(r *http.Request) bool {
	return s.auth.Throttled("oauth|" + s.clientKey(r))
}

func (s *Server) oauthRegister(w http.ResponseWriter, r *http.Request) {
	if s.oauthThrottled(r) {
		oauthError(w, http.StatusTooManyRequests, "invalid_request", "too many requests")
		return
	}
	s.auth.Failed("oauth|" + s.clientKey(r)) // every registration counts: this endpoint is open to anyone
	var in struct {
		Name         string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.AuthMethod != "" && in.AuthMethod != "none" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "only public clients (token_endpoint_auth_method none) are supported")
		return
	}
	c, err := s.auth.RegisterClient(in.Name, in.RedirectURIs)
	if errors.Is(err, auth.ErrInvalid) {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "give 1-5 redirect URIs: https, http on localhost, or an app scheme")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	cors(w)
	writeJSON(w, http.StatusCreated, map[string]any{"client_id": c.ID, "client_name": c.Name, "redirect_uris": c.RedirectURIs, "token_endpoint_auth_method": "none",
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
}

// authzRequest is a validated authorization request, carried through the login and consent forms.
type authzRequest struct {
	ClientID, RedirectURI, Challenge, Scope, State string
}

type pageData struct {
	Mode   string // login, consent or error
	Client string
	Error  string
	Totp   bool
	CSRF   string
	Req    authzRequest
	Scopes []string
	Email  string
}

func (s *Server) page(w http.ResponseWriter, status int, d pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(status)
	_ = oauthPage.Execute(w, d)
}

// parseAuthz validates the request. A bad client or redirect URI is shown to the user and never redirected to.
func (s *Server) parseAuthz(w http.ResponseWriter, r *http.Request, v url.Values) (authzRequest, auth.OAuthClient, bool) {
	req := authzRequest{ClientID: v.Get("client_id"), RedirectURI: v.Get("redirect_uri"), Challenge: v.Get("code_challenge"), Scope: v.Get("scope"), State: v.Get("state")}
	c, err := s.auth.OAuthClient(req.ClientID)
	if err != nil || !containsStr(c.RedirectURIs, req.RedirectURI) {
		s.page(w, http.StatusBadRequest, pageData{Mode: "error", Error: "unknown client or redirect address"})
		return req, c, false
	}
	fail := func(code string) (authzRequest, auth.OAuthClient, bool) {
		redirectBack(w, r, req, url.Values{"error": {code}})
		return req, c, false
	}
	if v.Get("response_type") != "code" {
		return fail("unsupported_response_type")
	}
	if v.Get("code_challenge_method") != "S256" || len(req.Challenge) != 43 {
		return fail("invalid_request")
	}
	if res := v.Get("resource"); res != "" && res != s.baseURL(r)+"/mcp" {
		return fail("invalid_target")
	}
	return req, c, true
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func redirectBack(w http.ResponseWriter, r *http.Request, req authzRequest, extra url.Values) {
	u, _ := url.Parse(req.RedirectURI)
	q := u.Query()
	for k, vs := range extra {
		q[k] = vs
	}
	if req.State != "" {
		q.Set("state", req.State)
	}
	u.RawQuery = q.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (s *Server) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	req, c, ok := s.parseAuthz(w, r, r.URL.Query())
	if !ok {
		return
	}
	if a, ok := s.sessionActor(r); ok {
		s.page(w, http.StatusOK, pageData{Mode: "consent", Client: c.Name, CSRF: a.CSRF, Req: req, Scopes: auth.NormalizeScopes(req.Scope)})
		return
	}
	s.page(w, http.StatusOK, pageData{Mode: "login", Client: c.Name, Req: req})
}

func (s *Server) sessionActor(r *http.Request) (auth.Actor, bool) {
	ck, err := r.Cookie(auth.SessionCookie)
	if err != nil {
		return auth.Actor{}, false
	}
	a, err := s.auth.SessionActor(ck.Value)
	return a, err == nil && !a.MustChangePassword
}

// oauthDecide handles the login form and the approve/deny buttons.
func (s *Server) oauthDecide(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		s.page(w, http.StatusBadRequest, pageData{Mode: "error", Error: "malformed form"})
		return
	}
	req, c, ok := s.parseAuthz(w, r, r.PostForm)
	if !ok {
		return
	}
	scopes := auth.NormalizeScopes(req.Scope)
	switch r.PostForm.Get("action") {
	case "login":
		user, session, csrf, err := s.auth.Login(r.PostForm.Get("email"), r.PostForm.Get("password"), r.PostForm.Get("code"), s.clientKey(r))
		d := pageData{Mode: "login", Client: c.Name, Req: req, Email: r.PostForm.Get("email")}
		switch {
		case errors.Is(err, auth.ErrTOTPRequired):
			d.Totp = true
			s.page(w, http.StatusOK, d)
			return
		case errors.Is(err, auth.ErrRateLimited):
			d.Error = "Too many attempts. Try again later."
			s.page(w, http.StatusTooManyRequests, d)
			return
		case errors.Is(err, auth.ErrInvalidCredentials):
			d.Error = "Invalid email or password."
			s.page(w, http.StatusUnauthorized, d)
			return
		case err != nil:
			s.internal(w, r, err)
			return
		}
		if user.MustChangePassword {
			d.Error = "Change your password in Atlas first."
			s.page(w, http.StatusForbidden, d)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: session, Path: "/", HttpOnly: true, Secure: s.cookieSecure,
			SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(s.auth.SessionTTL())})
		s.page(w, http.StatusOK, pageData{Mode: "consent", Client: c.Name, CSRF: csrf, Req: req, Scopes: scopes})
	case "approve", "deny":
		a, ok := s.sessionActor(r)
		if !ok || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(a.CSRF)) != 1 {
			s.page(w, http.StatusForbidden, pageData{Mode: "error", Error: "your session ended; start again from the application"})
			return
		}
		if r.PostForm.Get("action") == "deny" {
			redirectBack(w, r, req, url.Values{"error": {"access_denied"}})
			return
		}
		code, err := s.auth.CreateCode(a.UserID, c, req.RedirectURI, req.Challenge, scopes)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		redirectBack(w, r, req, url.Values{"code": {code}})
	default:
		s.page(w, http.StatusBadRequest, pageData{Mode: "error", Error: "unknown action"})
	}
}

func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	if s.oauthThrottled(r) {
		oauthError(w, http.StatusTooManyRequests, "invalid_request", "too many failed requests")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "malformed form")
		return
	}
	f := r.PostForm
	if res := f.Get("resource"); res != "" && res != s.baseURL(r)+"/mcp" {
		oauthError(w, http.StatusBadRequest, "invalid_target", "unknown resource")
		return
	}
	var tok auth.OAuthTokens
	var err error
	switch f.Get("grant_type") {
	case "authorization_code":
		tok, err = s.auth.ExchangeCode(f.Get("client_id"), f.Get("code"), f.Get("redirect_uri"), f.Get("code_verifier"))
	case "refresh_token":
		tok, err = s.auth.Refresh(f.Get("client_id"), f.Get("refresh_token"))
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
		return
	}
	switch {
	case errors.Is(err, auth.ErrOAuthClient):
		s.auth.Failed("oauth|" + s.clientKey(r))
		oauthError(w, http.StatusUnauthorized, "invalid_client", "unknown client")
		return
	case errors.Is(err, auth.ErrOAuthGrant):
		s.auth.Failed("oauth|" + s.clientKey(r))
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the code or refresh token is invalid, expired or already used")
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	cors(w)
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": tok.AccessToken, "token_type": "Bearer", "expires_in": tok.ExpiresIn,
		"refresh_token": tok.RefreshToken, "scope": strings.Join(tok.Scopes, " ")})
}
