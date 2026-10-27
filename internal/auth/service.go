package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/kees/atlas/internal/db"
)

const (
	SessionTTL      = 14 * 24 * time.Hour
	tokenPrefix     = "atl_"
	loginMaxFails   = 5
	loginFailWindow = 15 * time.Minute
	lastUsedEvery   = 5 * time.Minute
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrRateLimited        = errors.New("too many login attempts")
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrInvalid            = errors.New("invalid input")
)

type User struct {
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"is_admin"`
}

type Token struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Prefix    string   `json:"prefix"`
	Scopes    []string `json:"scopes"`
	Projects  []string `json:"projects"`
	ExpiresAt *int64   `json:"expires_at"`
	RevokedAt *int64   `json:"revoked_at"`
	CreatedAt int64    `json:"created_at"`
	// LastUsedAt is when the token last authenticated (updated at most every few minutes).
	LastUsedAt *int64 `json:"last_used_at"`
}

type Service struct {
	db *db.DB

	mu    sync.Mutex
	fails map[string][]time.Time
	// seen throttles last_used_at writes: token id -> when it was last written.
	seen map[int64]time.Time
}

func NewService(d *db.DB) *Service {
	return &Service{db: d, fails: map[string][]time.Time{}, seen: map[int64]time.Time{}}
}

// EnsureAdmin creates the first admin when the database has no users.
func (s *Service) EnsureAdmin(email, password string) (created bool, err error) {
	var n int
	if err := s.db.W.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 || email == "" || password == "" {
		return false, nil
	}
	if _, err := s.CreateUser(email, "Admin", password, true); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) CreateUser(email, name, password string, admin bool) (User, error) {
	email = strings.TrimSpace(email)
	if email == "" || name == "" || len(password) < 8 {
		return User{}, ErrInvalid
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	res, err := s.db.W.Exec(`INSERT INTO users (email, name, password_hash, is_admin, created_at)
		VALUES (?, ?, ?, ?, ?)`, email, name, hash, admin, time.Now().UnixMilli())
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Email: email, Name: name, IsAdmin: admin}, nil
}

// Login verifies credentials and returns a new session token (cookie value) and CSRF token.
func (s *Service) Login(email, password, clientKey string) (user User, session, csrf string, err error) {
	key := strings.ToLower(email) + "|" + clientKey
	if s.limited(key) {
		return User{}, "", "", ErrRateLimited
	}

	var hash string
	err = s.db.R.QueryRow(`SELECT id, email, name, is_admin, password_hash FROM users WHERE email = ? AND deactivated_at IS NULL`, email).
		Scan(&user.ID, &user.Email, &user.Name, &user.IsAdmin, &hash)
	ok := false
	if err == nil {
		ok, _ = VerifyPassword(password, hash)
	} else if errors.Is(err, sql.ErrNoRows) {
		// Spend comparable time so unknown users are not distinguishable by latency.
		_, _ = VerifyPassword(password, dummyHashOnce())
	} else {
		return User{}, "", "", err
	}
	if !ok {
		s.recordFail(key)
		return User{}, "", "", ErrInvalidCredentials
	}

	session = randomToken(32)
	csrf = randomToken(16)
	now := time.Now()
	if _, err := s.db.W.Exec(`INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`, hashToken(session), user.ID, csrf, now.UnixMilli(), now.Add(SessionTTL).UnixMilli()); err != nil {
		return User{}, "", "", err
	}
	s.clearFails(key)
	return user, session, csrf, nil
}

func (s *Service) Logout(session string) error {
	_, err := s.db.W.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(session))
	return err
}

// SessionActor resolves a session cookie value to an actor.
func (s *Service) SessionActor(session string) (Actor, error) {
	var a Actor
	var exp int64
	err := s.db.R.QueryRow(`SELECT s.user_id, u.is_admin, s.csrf_token, s.expires_at, s.rowid
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ? AND u.deactivated_at IS NULL`, hashToken(session)).
		Scan(&a.UserID, &a.IsAdmin, &a.CSRF, &exp, &a.SessionID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && exp < time.Now().UnixMilli()) {
		return Actor{}, ErrUnauthenticated
	}
	if err != nil {
		return Actor{}, err
	}
	a.Kind = KindHuman
	return a, nil
}

func (s *Service) User(id int64) (User, error) {
	var u User
	err := s.db.R.QueryRow(`SELECT id, email, name, is_admin FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin)
	return u, err
}

// NewTokenSecret makes a token: the plaintext (shown once), its display prefix and the hash that is stored.
func NewTokenSecret() (plain, prefix, hash string) {
	plain = tokenPrefix + randomToken(32)
	return plain, plain[:len(tokenPrefix)+4], hashToken(plain)
}

// CreateToken returns the plaintext token once; only its hash is stored.
func (s *Service) CreateToken(userID int64, name string, scopes, projects []string, expiresAt *int64) (Token, string, error) {
	if name == "" || len(scopes) == 0 {
		return Token{}, "", ErrInvalid
	}
	for _, sc := range scopes {
		if sc != ScopeRead && sc != ScopeWrite {
			return Token{}, "", ErrInvalid
		}
	}
	plain, prefix, hash := NewTokenSecret()
	scopesJSON, _ := json.Marshal(scopes)
	var projectsJSON any
	if projects != nil {
		b, _ := json.Marshal(projects)
		projectsJSON = string(b)
	}
	now := time.Now().UnixMilli()
	res, err := s.db.W.Exec(`INSERT INTO api_tokens (user_id, name, prefix, token_hash, scopes, projects, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, userID, name, prefix, hash, string(scopesJSON), projectsJSON, expiresAt, now)
	if err != nil {
		return Token{}, "", err
	}
	id, _ := res.LastInsertId()
	return Token{ID: id, Name: name, Prefix: prefix, Scopes: scopes, Projects: projects, ExpiresAt: expiresAt, CreatedAt: now}, plain, nil
}

func (s *Service) ListTokens(userID int64) ([]Token, error) {
	rows, err := s.db.R.Query(`SELECT id, name, prefix, scopes, projects, expires_at, revoked_at, created_at, last_used_at
		FROM api_tokens WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Token{}
	for rows.Next() {
		var t Token
		var scopes string
		var projects sql.NullString
		var exp, rev, used sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &scopes, &projects, &exp, &rev, &t.CreatedAt, &used); err != nil {
			return nil, err
		}
		if used.Valid {
			t.LastUsedAt = &used.Int64
		}
		_ = json.Unmarshal([]byte(scopes), &t.Scopes)
		if projects.Valid {
			_ = json.Unmarshal([]byte(projects.String), &t.Projects)
		}
		if exp.Valid {
			t.ExpiresAt = &exp.Int64
		}
		if rev.Valid {
			t.RevokedAt = &rev.Int64
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeToken takes effect immediately, without restart. Returns false if the token is not the user's.
func (s *Service) RevokeToken(userID, id int64) (bool, error) {
	res, err := s.db.W.Exec(`UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		time.Now().UnixMilli(), id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// TokenActor resolves a bearer token to an actor; revoked or expired tokens fail.
func (s *Service) TokenActor(plain string) (Actor, error) {
	if !strings.HasPrefix(plain, tokenPrefix) {
		return Actor{}, ErrUnauthenticated
	}
	var a Actor
	var scopes string
	var projects sql.NullString
	var exp, rev sql.NullInt64
	err := s.db.R.QueryRow(`SELECT t.id, t.user_id, u.is_admin, t.scopes, t.projects, t.expires_at, t.revoked_at
		FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = ? AND u.deactivated_at IS NULL`, hashToken(plain)).
		Scan(&a.TokenID, &a.UserID, &a.IsAdmin, &scopes, &projects, &exp, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return Actor{}, ErrUnauthenticated
	}
	if err != nil {
		return Actor{}, err
	}
	if rev.Valid || (exp.Valid && exp.Int64 < time.Now().UnixMilli()) {
		return Actor{}, ErrUnauthenticated
	}
	a.Kind = KindToken
	s.touch(a.TokenID)
	_ = json.Unmarshal([]byte(scopes), &a.Scopes)
	if projects.Valid {
		_ = json.Unmarshal([]byte(projects.String), &a.Projects)
	}
	return a, nil
}

// touch records that a token was used, at most once per lastUsedEvery: one write per request would
// burden the single writer for a hint nobody needs to the second.
func (s *Service) touch(id int64) {
	s.mu.Lock()
	last, ok := s.seen[id]
	if ok && time.Since(last) < lastUsedEvery {
		s.mu.Unlock()
		return
	}
	s.seen[id] = time.Now()
	s.mu.Unlock()
	_, _ = s.db.W.Exec(`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, time.Now().UnixMilli(), id)
}

// PurgeExpiredSessions is cheap housekeeping; call at startup.
func (s *Service) PurgeExpiredSessions() error {
	_, err := s.db.W.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().UnixMilli())
	return err
}

func (s *Service) limited(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-loginFailWindow)
	recent := s.fails[key][:0]
	for _, t := range s.fails[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(s.fails, key)
	} else {
		s.fails[key] = recent
	}
	return len(recent) >= loginMaxFails
}

func (s *Service) recordFail(key string) {
	s.mu.Lock()
	s.fails[key] = append(s.fails[key], time.Now())
	s.mu.Unlock()
}

func (s *Service) clearFails(key string) {
	s.mu.Lock()
	delete(s.fails, key)
	s.mu.Unlock()
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Tokens and sessions are high-entropy random values, so a plain SHA-256 is sufficient.
func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// Lazy: hashing allocates argon2 memory, which should not count towards idle RSS.
var dummyHashOnce = sync.OnceValue(func() string {
	h, err := HashPassword("atlas-dummy-password")
	if err != nil {
		panic(err)
	}
	return h
})
