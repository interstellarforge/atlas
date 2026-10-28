package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// OAuth 2.1 authorization server for MCP clients (ATL-75, ADR-0025): public clients registered dynamically
// (RFC 7591), authorization code with mandatory PKCE S256, rotating refresh tokens. Access tokens are
// api_tokens, so scopes, listing and revocation work as for personal tokens.

const (
	AccessTokenTTL  = time.Hour
	RefreshTokenTTL = 30 * 24 * time.Hour
	codeTTL         = 5 * time.Minute
	maxClients      = 1000
)

var (
	ErrOAuthClient = errors.New("unknown client")
	ErrOAuthGrant  = errors.New("invalid grant")
	schemeRe       = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)
	verifierRe     = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

type OAuthClient struct {
	ID           string   `json:"client_id"`
	Name         string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

// validRedirectURI accepts https, http on a loopback host, and private-use schemes (RFC 8252); never a
// fragment, and never a scheme a browser would run as script or file access.
func validRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 500 || u.Fragment != "" || u.User != nil || !schemeRe.MatchString(u.Scheme) {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Hostname() != ""
	case "http":
		h := u.Hostname()
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	case "javascript", "data", "file", "vbscript", "about", "blob", "ftp", "ws", "wss":
		return false
	}
	return true
}

func (s *Service) RegisterClient(name string, uris []string) (OAuthClient, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "MCP client"
	}
	if len(name) > 100 || len(uris) == 0 || len(uris) > 5 {
		return OAuthClient{}, ErrInvalid
	}
	for _, u := range uris {
		if !validRedirectURI(u) {
			return OAuthClient{}, ErrInvalid
		}
	}
	var n int
	if err := s.db.R.QueryRow(`SELECT COUNT(*) FROM oauth_clients`).Scan(&n); err != nil {
		return OAuthClient{}, err
	}
	if n >= maxClients {
		return OAuthClient{}, ErrInvalid
	}
	c := OAuthClient{ID: "atlas-" + randomToken(16), Name: name, RedirectURIs: uris}
	b, _ := json.Marshal(uris)
	_, err := s.db.W.Exec(`INSERT INTO oauth_clients (id, name, redirect_uris, created_at) VALUES (?, ?, ?, ?)`, c.ID, name, string(b), time.Now().UnixMilli())
	return c, err
}

func (s *Service) OAuthClient(id string) (OAuthClient, error) {
	var c OAuthClient
	var uris string
	err := s.db.R.QueryRow(`SELECT id, name, redirect_uris FROM oauth_clients WHERE id = ?`, id).Scan(&c.ID, &c.Name, &uris)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrOAuthClient
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal([]byte(uris), &c.RedirectURIs)
}

// NormalizeScopes keeps the known scopes of a space separated request; none requested means read only.
func NormalizeScopes(requested string) []string {
	out := []string{}
	for _, sc := range strings.Fields(requested) {
		if (sc == ScopeRead || sc == ScopeWrite) && !slices.Contains(out, sc) {
			out = append(out, sc)
		}
	}
	if len(out) == 0 {
		out = []string{ScopeRead}
	}
	return out
}

// CreateCode stores a single-use authorization code for the signed-in user and returns it.
func (s *Service) CreateCode(userID int64, c OAuthClient, redirectURI, challenge string, scopes []string) (string, error) {
	if !slices.Contains(c.RedirectURIs, redirectURI) || len(challenge) != 43 {
		return "", ErrOAuthGrant
	}
	code := randomToken(32)
	sj, _ := json.Marshal(scopes)
	_, err := s.db.W.Exec(`INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, challenge, scopes, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		hashToken(code), c.ID, userID, redirectURI, challenge, string(sj), time.Now().Add(codeTTL).UnixMilli())
	return code, err
}

type OAuthTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	Scopes       []string
}

// issue creates an access token and a refresh token of the family.
func (s *Service) issue(userID int64, c OAuthClient, scopes []string, family string) (OAuthTokens, error) {
	exp := time.Now().Add(AccessTokenTTL).UnixMilli()
	tok, plain, err := s.CreateToken(userID, "OAuth: "+c.Name, scopes, nil, &exp)
	if err != nil {
		return OAuthTokens{}, err
	}
	refresh := randomToken(32)
	sj, _ := json.Marshal(scopes)
	now := time.Now()
	if _, err := s.db.W.Exec(`INSERT INTO oauth_refresh (token_hash, family, client_id, user_id, scopes, access_token_id, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		hashToken(refresh), family, c.ID, userID, string(sj), tok.ID, now.Add(RefreshTokenTTL).UnixMilli(), now.UnixMilli()); err != nil {
		return OAuthTokens{}, err
	}
	return OAuthTokens{AccessToken: plain, RefreshToken: refresh, ExpiresIn: int64(AccessTokenTTL.Seconds()), Scopes: scopes}, nil
}

// ExchangeCode trades an authorization code for tokens. The code is consumed even when the rest fails.
func (s *Service) ExchangeCode(clientID, code, redirectURI, verifier string) (OAuthTokens, error) {
	c, err := s.OAuthClient(clientID)
	if err != nil {
		return OAuthTokens{}, ErrOAuthClient
	}
	var user int64
	var uri, challenge, scopes string
	var exp int64
	tx, err := s.db.W.Begin()
	if err != nil {
		return OAuthTokens{}, err
	}
	defer tx.Rollback()
	err = tx.QueryRow(`SELECT user_id, redirect_uri, challenge, scopes, expires_at FROM oauth_codes WHERE code_hash = ? AND client_id = ?`, hashToken(code), clientID).
		Scan(&user, &uri, &challenge, &scopes, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthTokens{}, ErrOAuthGrant
	}
	if err != nil {
		return OAuthTokens{}, err
	}
	if _, err := tx.Exec(`DELETE FROM oauth_codes WHERE code_hash = ?`, hashToken(code)); err != nil {
		return OAuthTokens{}, err
	}
	if err := tx.Commit(); err != nil {
		return OAuthTokens{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if exp < time.Now().UnixMilli() || uri != redirectURI || !verifierRe.MatchString(verifier) || subtle.ConstantTimeCompare([]byte(want), []byte(challenge)) != 1 {
		return OAuthTokens{}, ErrOAuthGrant
	}
	var sc []string
	_ = json.Unmarshal([]byte(scopes), &sc)
	return s.issue(user, c, sc, randomToken(8))
}

// Refresh rotates a refresh token. Presenting one that was already used means it leaked: the whole family is
// revoked (refresh tokens and their access tokens).
func (s *Service) Refresh(clientID, refresh string) (OAuthTokens, error) {
	c, err := s.OAuthClient(clientID)
	if err != nil {
		return OAuthTokens{}, ErrOAuthClient
	}
	var family, scopes string
	var user int64
	var access sql.NullInt64
	var exp int64
	var used sql.NullInt64
	err = s.db.R.QueryRow(`SELECT family, user_id, scopes, access_token_id, expires_at, used_at FROM oauth_refresh WHERE token_hash = ? AND client_id = ?`, hashToken(refresh), clientID).
		Scan(&family, &user, &scopes, &access, &exp, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthTokens{}, ErrOAuthGrant
	}
	if err != nil {
		return OAuthTokens{}, err
	}
	now := time.Now().UnixMilli()
	if used.Valid {
		s.revokeFamily(family)
		return OAuthTokens{}, ErrOAuthGrant
	}
	if exp < now {
		return OAuthTokens{}, ErrOAuthGrant
	}
	res, err := s.db.W.Exec(`UPDATE oauth_refresh SET used_at = ? WHERE token_hash = ? AND used_at IS NULL`, now, hashToken(refresh))
	if err != nil {
		return OAuthTokens{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return OAuthTokens{}, ErrOAuthGrant
	}
	if access.Valid {
		_, _ = s.db.W.Exec(`UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, now, access.Int64)
	}
	var sc []string
	_ = json.Unmarshal([]byte(scopes), &sc)
	return s.issue(user, c, sc, family)
}

func (s *Service) revokeFamily(family string) {
	now := time.Now().UnixMilli()
	_, _ = s.db.W.Exec(`UPDATE api_tokens SET revoked_at = ? WHERE revoked_at IS NULL AND id IN (SELECT access_token_id FROM oauth_refresh WHERE family = ?)`, now, family)
	_, _ = s.db.W.Exec(`UPDATE oauth_refresh SET used_at = COALESCE(used_at, ?) WHERE family = ?`, now, family)
}

// PurgeOAuth drops expired codes and refresh tokens.
func (s *Service) PurgeOAuth() {
	now := time.Now().UnixMilli()
	_, _ = s.db.W.Exec(`DELETE FROM oauth_codes WHERE expires_at < ?`, now)
	_, _ = s.db.W.Exec(`DELETE FROM oauth_refresh WHERE expires_at < ?`, now)
	// Short-lived access tokens would pile up in the token list: drop OAuth ones a day after they ended.
	_, _ = s.db.W.Exec(`DELETE FROM api_tokens WHERE name LIKE 'OAuth: %' AND COALESCE(revoked_at, expires_at) < ?`, now-24*time.Hour.Milliseconds())
}
