package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// Join link and join requests (ADR-0031). The ADMIN enables /join/<key>; a signed-in account asks to join; the ADMIN
// approves (USER membership, no project access) or rejects. A confirmed email domain (domains.go) also opens the way,
// and mode auto joins a verified address at once. RequestJoin answers the same whether the organization
// exists, is archived, has the link off and no domain applies, the account is already a member, a request waits, or the account must wait
// after a rejection (24 h, at most 3 per 30 days), so nothing about the organization is disclosed.

// JoinRequest is one request as the organization ADMIN sees it.
type JoinRequest struct {
	ID           int64  `json:"id"`
	UserEmail    string `json:"user_email"`
	UserName     string `json:"user_name"`
	Status       string `json:"status"` // pending, approved or rejected
	CreatedAt    int64  `json:"created_at"`
	DecidedAt    *int64 `json:"decided_at"`
	DecisionNote string `json:"decision_note,omitempty"`
}

func (s *Service) adminOfOrg(q queryer, a auth.Actor, key string) (Organization, error) {
	o, err := s.authorizeOrg(q, a, key, orgMembers)
	if err != nil {
		return Organization{}, err
	}
	if o.Role != "admin" {
		return Organization{}, ErrForbidden // a support grant never decides who joins
	}
	return o, nil
}

func (s *Service) JoinLinkEnabled(ctx context.Context, a auth.Actor, key string) (bool, error) {
	o, err := s.adminOfOrg(s.db.R, a, key)
	if err != nil {
		return false, err
	}
	var on bool
	err = s.db.R.QueryRow(`SELECT COALESCE((SELECT join_link_enabled FROM organization_settings WHERE organization_id = ?), 0)`, o.ID).Scan(&on)
	return on, err
}

func (s *Service) SetJoinLink(ctx context.Context, a auth.Actor, key string, enabled bool) error {
	return s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, key)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO organization_settings (organization_id, join_link_enabled) VALUES (?, ?)
			ON CONFLICT (organization_id) DO UPDATE SET join_link_enabled = excluded.join_link_enabled`, o.ID, enabled); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "org.joinlink.set", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"enabled": enabled}})
	})
}

// RequestJoin files a request for the signed-in account. It returns nil whenever the caller may be told "received",
// including when nothing was filed.
func (s *Service) RequestJoin(ctx context.Context, a auth.Actor, key string) error {
	if !a.Can(auth.ScopeWrite) || a.Projects != nil || a.Kind != auth.KindHuman {
		return ErrForbidden
	}
	return s.tx(func(tx *sql.Tx) error {
		st, err := s.standing(tx, a)
		if err != nil {
			return err
		}
		if !st.active {
			return ErrForbidden
		}
		var orgID int64
		var orgKey string
		var link, byDomain bool
		err = tx.QueryRow(`SELECT o.id, o.key, COALESCE(x.join_link_enabled, 0) = 1,
			EXISTS (SELECT 1 FROM organization_domains d JOIN users u ON u.id = ?2 WHERE d.organization_id = o.id AND d.status = 'confirmed'
				AND lower(substr(u.email, instr(u.email, '@') + 1)) = lower(d.domain))
			FROM organizations o LEFT JOIN organization_settings x ON x.organization_id = o.id
			WHERE o.key = ?1 AND o.status = 'active'`, strings.ToUpper(strings.TrimSpace(key)), a.UserID).Scan(&orgID, &orgKey, &link, &byDomain)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !link && !byDomain) {
			return nil // the join link is off and no confirmed domain applies: same answer as for a missing organization
		}
		if err != nil {
			return err
		}
		if byDomain { // mode auto joins a verified address at once; otherwise the request below is filed
			if err := s.autoJoin(ctx, tx, a, orgID); err != nil {
				return err
			}
		}
		now := s.nowMs()
		var blocked bool
		err = tx.QueryRow(`SELECT
			EXISTS (SELECT 1 FROM organization_memberships WHERE organization_id = ?1 AND user_id = ?2)
			OR EXISTS (SELECT 1 FROM organization_join_requests WHERE organization_id = ?1 AND user_id = ?2 AND status = 'pending')
			OR EXISTS (SELECT 1 FROM organization_join_requests WHERE organization_id = ?1 AND user_id = ?2 AND status = 'rejected' AND decided_at > ?3)
			OR (SELECT COUNT(*) FROM organization_join_requests WHERE organization_id = ?1 AND user_id = ?2 AND created_at > ?4) >= ?5`,
			orgID, a.UserID, now-applyCooldownMs, now-applyWindowMs, applyMaxInWin).Scan(&blocked)
		if err != nil || blocked {
			return err
		}
		res, err := tx.Exec(`INSERT INTO organization_join_requests (organization_id, user_id, created_at) VALUES (?, ?, ?)`, orgID, a.UserID, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "org.join.request", Target: orgTarget(orgKey), Event: "admin.updated", Changes: map[string]any{"request": id}})
	})
}

func (s *Service) ListJoinRequests(ctx context.Context, a auth.Actor, key, status string) ([]JoinRequest, error) {
	o, err := s.adminOfOrg(s.db.R, a, key)
	if err != nil {
		return nil, err
	}
	q := `SELECT r.id, u.email, u.name, r.status, r.created_at, r.decided_at, r.decision_note FROM organization_join_requests r
		JOIN users u ON u.id = r.user_id WHERE r.organization_id = ?`
	args := []any{o.ID}
	if status != "" {
		q += ` AND r.status = ?`
		args = append(args, status)
	}
	rows, err := s.db.R.Query(q+` ORDER BY r.id DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JoinRequest{}
	for rows.Next() {
		var r JoinRequest
		var at sql.NullInt64
		if err := rows.Scan(&r.ID, &r.UserEmail, &r.UserName, &r.Status, &r.CreatedAt, &at, &r.DecisionNote); err != nil {
			return nil, err
		}
		if at.Valid {
			r.DecidedAt = &at.Int64
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DecideJoinRequest approves (USER membership, no project access) or rejects. Compare-and-set: one concurrent
// decision wins; a decided request is a rule error.
func (s *Service) DecideJoinRequest(ctx context.Context, a auth.Actor, key string, id int64, decision, note string) error {
	if decision != "approve" && decision != "reject" {
		return invalid("decision must be approve or reject")
	}
	return s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, key)
		if err != nil {
			return err
		}
		var userID int64
		var active bool
		err = tx.QueryRow(`SELECT r.user_id, u.status = 'active' FROM organization_join_requests r JOIN users u ON u.id = r.user_id
			WHERE r.id = ? AND r.organization_id = ?`, id, o.ID).Scan(&userID, &active)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		status := "rejected"
		if decision == "approve" {
			if !active {
				return rule("the account is not active")
			}
			status = "approved"
		}
		now := s.nowMs()
		res, err := tx.Exec(`UPDATE organization_join_requests SET status = ?, decided_at = ?, decided_by = ?, decision_note = ? WHERE id = ? AND status = 'pending'`,
			status, now, a.UserID, strings.TrimSpace(note), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return rule("this request was already decided")
		}
		if status == "approved" {
			if _, err := tx.Exec(`INSERT INTO organization_memberships (organization_id, user_id, role, created_at, created_by) VALUES (?, ?, 'user', ?, ?)
				ON CONFLICT (organization_id, user_id) DO NOTHING`, o.ID, userID, now, a.UserID); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "org.join." + decision, Target: orgTarget(o.Key), Event: "admin.updated",
			Changes: map[string]any{"request": id, "user_id": userID}})
	})
}
