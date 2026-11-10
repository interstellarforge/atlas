package core

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"sync"

	"github.com/kees/atlas/internal/auth"
)

// Support access (ADR-0031): the ADMIN of an organization lets the Atlas SUPERADMIN in for a while. A grant is
// read live by authorization; a SUPERADMIN session (never a token) holds the level of a valid grant, nothing else.

// SupportGrant is one grant of support access to an organization.
type SupportGrant struct {
	ID        int64  `json:"id"`
	Level     string `json:"level"` // read_only or full
	GrantedBy int64  `json:"granted_by"`
	GrantedAt int64  `json:"granted_at"`
	ExpiresAt *int64 `json:"expires_at"` // nil: until revoked
	RevokedAt *int64 `json:"revoked_at"`
	Valid     bool   `json:"valid"`
}

// supportAudit throttles the audit entry written when a grant is used for reading.
type supportAudit struct {
	mu   sync.Mutex
	seen map[[2]int64]int64 // grant id, user id -> ms of the last entry
}

const supportReadAuditEvery = 10 * 60 * 1000 // ms: one entry per grant and user per 10 minutes of reading

// supportAllows is what a read_only grant permits on a project.
func supportAllows(act action) bool { return act == actView || act == actExport }

// supportGrant returns the best valid grant of an organization: its id and the level it gives (levelAdmin for
// full, levelSupport for read_only); id 0 when there is none.
func supportGrant(q queryer, orgID, now int64) (int64, level, error) {
	var id int64
	var lvl string
	err := q.QueryRow(`SELECT id, level FROM support_grants WHERE organization_id = ? AND revoked_at IS NULL
		AND (expires_at IS NULL OR expires_at > ?) ORDER BY CASE level WHEN 'full' THEN 0 ELSE 1 END, id LIMIT 1`, orgID, now).Scan(&id, &lvl)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, levelNone, nil
	}
	if err != nil {
		return 0, levelNone, err
	}
	if lvl == "full" {
		return id, levelAdmin, nil
	}
	return id, levelSupport, nil
}

// noteSupport audits the use of a grant: always for changes, at most every 10 minutes for reading. It runs in the
// background because the caller may hold the write transaction.
func (s *Service) noteSupport(a auth.Actor, orgID, grantID int64, act action) {
	if s.support == nil || grantID == 0 {
		return
	}
	now := s.nowMs()
	if act == actView || act == actExport {
		s.support.mu.Lock()
		key := [2]int64{grantID, a.UserID}
		if last, ok := s.support.seen[key]; ok && now-last < supportReadAuditEvery {
			s.support.mu.Unlock()
			return
		}
		s.support.seen[key] = now
		s.support.mu.Unlock()
	}
	go func() {
		_ = s.tx(func(tx *sql.Tx) error {
			return s.journal(context.Background(), tx, a, change{Operation: "support.access", Target: "support-grant-" + strconv.FormatInt(grantID, 10), Event: "admin.updated",
				Changes: map[string]any{"organization_id": orgID, "grant": grantID, "action": string(act)}})
		})
	}()
}

// ListSupportGrants lists the grants of an organization, newest first. Only an ADMIN of that organization.
func (s *Service) ListSupportGrants(ctx context.Context, a auth.Actor, key string) ([]SupportGrant, error) {
	o, err := s.authorizeOrg(s.db.R, a, key, orgGrants)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT id, level, COALESCE(granted_by, 0), granted_at, expires_at, revoked_at FROM support_grants
		WHERE organization_id = ? ORDER BY id DESC LIMIT 100`, o.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := s.nowMs()
	out := []SupportGrant{}
	for rows.Next() {
		var g SupportGrant
		var exp, rev sql.NullInt64
		if err := rows.Scan(&g.ID, &g.Level, &g.GrantedBy, &g.GrantedAt, &exp, &rev); err != nil {
			return nil, err
		}
		if exp.Valid {
			g.ExpiresAt = &exp.Int64
		}
		if rev.Valid {
			g.RevokedAt = &rev.Int64
		}
		g.Valid = g.RevokedAt == nil && (g.ExpiresAt == nil || *g.ExpiresAt > now)
		out = append(out, g)
	}
	return out, rows.Err()
}

// GrantSupport lets the SUPERADMIN into the organization at the given level (read_only or full) for hours
// (1-720), or until revoked when untilRevoked is set. Only an ADMIN of that organization through a user session:
// a SUPERADMIN acting under a grant can never extend or create one.
func (s *Service) GrantSupport(ctx context.Context, a auth.Actor, key, level string, hours int, untilRevoked bool) (SupportGrant, error) {
	if level != "read_only" && level != "full" {
		return SupportGrant{}, invalid("level must be read_only or full")
	}
	if !untilRevoked && (hours < 1 || hours > 720) {
		return SupportGrant{}, invalid("give hours (1-720) or until_revoked")
	}
	var g SupportGrant
	err := s.tx(func(tx *sql.Tx) error {
		o, err := s.authorizeOrg(tx, a, key, orgGrants)
		if err != nil {
			return err
		}
		now := s.nowMs()
		var exp any
		if !untilRevoked {
			exp = now + int64(hours)*3_600_000
		}
		res, err := tx.Exec(`INSERT INTO support_grants (organization_id, level, granted_by, granted_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
			o.ID, level, a.UserID, now, exp)
		if err != nil {
			return err
		}
		g = SupportGrant{Level: level, GrantedBy: a.UserID, GrantedAt: now, Valid: true}
		g.ID, _ = res.LastInsertId()
		if e, ok := exp.(int64); ok {
			g.ExpiresAt = &e
		}
		return s.journal(ctx, tx, a, change{Operation: "support.grant", Target: orgTarget(o.Key), Event: "admin.updated",
			Changes: map[string]any{"grant": g.ID, "level": level, "expires_at": g.ExpiresAt}})
	})
	return g, err
}

// RevokeSupport ends a grant at once; an open SUPERADMIN session loses access on its next request.
func (s *Service) RevokeSupport(ctx context.Context, a auth.Actor, key string, id int64) error {
	return s.tx(func(tx *sql.Tx) error {
		o, err := s.authorizeOrg(tx, a, key, orgGrants)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE support_grants SET revoked_at = ?, revoked_by = ? WHERE id = ? AND organization_id = ? AND revoked_at IS NULL`,
			s.nowMs(), a.UserID, id, o.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "support.revoke", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"grant": id}})
	})
}
