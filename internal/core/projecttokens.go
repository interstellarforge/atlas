package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// ProjectToken is an API token limited to exactly one project (for an agent or CI). Its owner is the
// admin who created it; it can never do more than that owner may do in the project (see access).
type ProjectToken struct {
	auth.Token
	Owner string `json:"owner"`
}

func projectScope(key string) string {
	b, _ := json.Marshal([]string{key})
	return string(b)
}

func (s *Service) ListProjectTokens(ctx context.Context, a auth.Actor, key string) ([]ProjectToken, error) {
	if err := selfOnly(a); err != nil {
		return nil, err
	}
	p, _, err := s.access(s.db.R, a, key, actTokens)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT t.id, t.name, t.prefix, t.scopes, t.expires_at, t.revoked_at, t.created_at, t.last_used_at, u.name
		FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.projects = ? ORDER BY t.id`, projectScope(p.Key))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectToken{}
	for rows.Next() {
		var t ProjectToken
		var scopes string
		var exp, rev, used sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &scopes, &exp, &rev, &t.CreatedAt, &used, &t.Owner); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(scopes), &t.Scopes)
		t.Projects = []string{p.Key}
		if exp.Valid {
			t.ExpiresAt = &exp.Int64
		}
		if rev.Valid {
			t.RevokedAt = &rev.Int64
		}
		if used.Valid {
			t.LastUsedAt = &used.Int64
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateProjectToken returns the plaintext secret once.
func (s *Service) CreateProjectToken(ctx context.Context, a auth.Actor, key, name string, write bool, expiresAt *int64) (ProjectToken, string, error) {
	if err := selfOnly(a); err != nil {
		return ProjectToken{}, "", err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return ProjectToken{}, "", invalid("a token needs a name (max 100 characters)")
	}
	scopes := []string{auth.ScopeRead}
	if write {
		scopes = append(scopes, auth.ScopeWrite)
	}
	var out ProjectToken
	var plain string
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actTokens)
		if err != nil {
			return err
		}
		var prefix, hash string
		plain, prefix, hash = auth.NewTokenSecret()
		sj, _ := json.Marshal(scopes)
		now := s.nowMs()
		res, err := tx.Exec(`INSERT INTO api_tokens (user_id, name, prefix, token_hash, scopes, projects, expires_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, a.UserID, name, prefix, hash, string(sj), projectScope(p.Key), expiresAt, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		out = ProjectToken{Token: auth.Token{ID: id, Name: name, Prefix: prefix, Scopes: scopes, Projects: []string{p.Key}, ExpiresAt: expiresAt, CreatedAt: now}}
		_ = tx.QueryRow(`SELECT name FROM users WHERE id = ?`, a.UserID).Scan(&out.Owner)
		return s.journal(ctx, tx, a, change{Operation: "token.create", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"token": id, "name": name, "scopes": scopes}}) // never the secret
	})
	return out, plain, err
}

func (s *Service) RevokeProjectToken(ctx context.Context, a auth.Actor, key string, id int64) error {
	if err := selfOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actTokens)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND projects = ? AND revoked_at IS NULL`, s.nowMs(), id, projectScope(p.Key))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "token.revoke", Target: key, Event: "project.updated", Project: key,
			Changes: map[string]any{"token": id}})
	})
}

// revokeOwnedProjectTokens ends the project tokens of a user who leaves the project: they would
// stop working anyway (a token never exceeds its owner), this makes it visible and permanent.
func (s *Service) revokeOwnedProjectTokens(tx *sql.Tx, userID int64, key string) error {
	_, err := tx.Exec(`UPDATE api_tokens SET revoked_at = ? WHERE user_id = ? AND projects = ? AND revoked_at IS NULL`,
		s.nowMs(), userID, projectScope(key))
	return err
}
