package api_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func (e env) form(method, path string, v url.Values, mut func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if mut != nil {
		mut(r)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func TestOAuthCodeFlowWithPKCEAndRefreshRotation(t *testing.T) {
	e := setup(t)

	if w := e.do("POST", "/mcp", nil, nil); w.Code != 401 || !strings.Contains(w.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("mcp without token: %d %q", w.Code, w.Header().Get("WWW-Authenticate"))
	}
	var meta struct {
		Issuer  string   `json:"issuer"`
		Methods []string `json:"code_challenge_methods_supported"`
	}
	w := e.do("GET", "/.well-known/oauth-authorization-server", nil, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &meta) != nil || meta.Issuer == "" || len(meta.Methods) != 1 || meta.Methods[0] != "S256" {
		t.Fatalf("metadata: %d %s", w.Code, w.Body)
	}

	const redirect = "http://localhost:8123/callback"
	if w := e.do("POST", "/oauth/register", map[string]any{"client_name": "x", "redirect_uris": []string{"javascript:alert(1)"}}, nil); w.Code != 400 {
		t.Fatalf("bad redirect accepted: %d", w.Code)
	}
	w = e.do("POST", "/oauth/register", map[string]any{"client_name": "Test client", "redirect_uris": []string{redirect}}, nil)
	var client struct {
		ID string `json:"client_id"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &client) != nil || client.ID == "" {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}

	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	authz := url.Values{"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {redirect}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"}, "scope": {"read write"}, "state": {"xyz"}}

	// A redirect address that was not registered is never redirected to.
	bad := url.Values{}
	for k, v := range authz {
		bad[k] = v
	}
	bad.Set("redirect_uri", "https://evil.example/cb")
	if w := e.do("GET", "/oauth/authorize?"+bad.Encode(), nil, nil); w.Code != 400 || w.Header().Get("Location") != "" {
		t.Fatalf("unregistered redirect: %d %q", w.Code, w.Header().Get("Location"))
	}
	// Without PKCE the request is refused by redirecting the error back.
	nopkce := url.Values{}
	for k, v := range authz {
		nopkce[k] = v
	}
	nopkce.Del("code_challenge")
	if w := e.do("GET", "/oauth/authorize?"+nopkce.Encode(), nil, nil); w.Code != 302 || !strings.Contains(w.Header().Get("Location"), "error=invalid_request") {
		t.Fatalf("no pkce: %d %q", w.Code, w.Header().Get("Location"))
	}

	// Anonymous: a login form. Approving needs the session and its CSRF token.
	if w := e.do("GET", "/oauth/authorize?"+authz.Encode(), nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Sign in") {
		t.Fatalf("login page: %d", w.Code)
	}
	cookie, csrf := e.login(t)
	withCookie := func(r *http.Request) { r.AddCookie(cookie) }
	approve := url.Values{"action": {"approve"}}
	for k, v := range authz {
		approve[k] = v
	}
	if w := e.form("POST", "/oauth/authorize", approve, withCookie); w.Code != 403 {
		t.Fatalf("approve without csrf: %d", w.Code)
	}
	approve.Set("csrf", csrf)
	w = e.form("POST", "/oauth/authorize", approve, withCookie)
	loc, _ := url.Parse(w.Header().Get("Location"))
	code := ""
	if loc != nil {
		code = loc.Query().Get("code")
	}
	if w.Code != 302 || code == "" || loc.Query().Get("state") != "xyz" || loc.Host != "localhost:8123" {
		t.Fatalf("approve: %d %q", w.Code, w.Header().Get("Location"))
	}

	token := func(v url.Values) (*httptest.ResponseRecorder, map[string]any) {
		w := e.form("POST", "/oauth/token", v, nil)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w, out
	}
	exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {strings.Repeat("w", 50)}}
	if w, _ := token(exchange); w.Code != 400 {
		t.Fatalf("wrong verifier: %d", w.Code)
	}
	exchange.Set("code_verifier", verifier)
	if w, _ := token(exchange); w.Code != 400 {
		t.Fatalf("code reused after a failed attempt: %d", w.Code) // the code is consumed by the first try
	}

	// A fresh code with the right verifier works; the access token is a normal scoped token.
	approve.Set("csrf", csrf)
	w = e.form("POST", "/oauth/authorize", approve, withCookie)
	loc, _ = url.Parse(w.Header().Get("Location"))
	exchange.Set("code", loc.Query().Get("code"))
	w, tok := token(exchange)
	if w.Code != 200 || tok["token_type"] != "Bearer" || tok["scope"] != "read write" {
		t.Fatalf("token: %d %s", w.Code, w.Body)
	}
	bearer := func(s string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+s) }
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, bearer(tok["access_token"].(string))); w.Code != 200 {
		t.Fatalf("access token: %d", w.Code)
	}

	// Refresh rotates: the old access and refresh tokens die, and reusing the old refresh token kills the family.
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {tok["refresh_token"].(string)}}
	w, tok2 := token(refresh)
	if w.Code != 200 || tok2["access_token"] == tok["access_token"] {
		t.Fatalf("refresh: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, bearer(tok["access_token"].(string))); w.Code != 401 {
		t.Fatalf("old access token still works: %d", w.Code)
	}
	if w, _ := token(refresh); w.Code != 400 {
		t.Fatalf("old refresh token reused: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/auth/me", nil, bearer(tok2["access_token"].(string))); w.Code != 401 {
		t.Fatalf("family survived reuse: %d", w.Code)
	}
}
