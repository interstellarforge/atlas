package core

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kees/atlas/internal/auth"
)

// SetUserActive suspends (active = false) or reactivates an account; SUPERADMIN only, human session only (ADR-0031).
// It is a compare-and-set on users.status in one transaction with audit and event. Suspending ends sessions and
// revokes tokens and OAuth grants in the same transaction; the last active SUPERADMIN and the last ADMIN of an
// organization cannot be suspended, and nobody suspends themselves. Reactivating does not bring back revoked tokens.
func (s *Service) SetUserActive(ctx context.Context, a auth.Actor, id int64, active bool) error {
	if err := s.adminOnly(a); err != nil {
		return err
	}
	from, to, op := "active", "suspended", "user.suspend"
	if active {
		from, to, op = "suspended", "active", "user.reactivate"
	} else if id == a.UserID {
		return rule("you cannot suspend yourself")
	}
	return s.tx(func(tx *sql.Tx) error {
		var status string
		var super bool
		if err := tx.QueryRow(`SELECT status, is_superadmin FROM users WHERE id = ?`, id).Scan(&status, &super); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != from {
			if active && status == "active" {
				return nil // already active
			}
			return rule("the account is %s: this is not possible", status)
		}
		if !active {
			if super {
				if n, err := activeSuperadmins(tx, id); err != nil {
					return err
				} else if n == 0 {
					return rule("there must be at least one active superadmin")
				}
			}
			if err := guardOrgAdminLoss(tx, id); err != nil {
				return err
			}
		}
		now := s.nowMs()
		var deactivated any
		if !active {
			deactivated = now
		}
		res, err := tx.Exec(`UPDATE users SET status = ?, deactivated_at = ?, status_changed_at = ?, status_changed_by = ? WHERE id = ? AND status = ?`,
			to, deactivated, now, a.UserID, id, from)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return rule("the account changed in the meantime: reload and try again")
		}
		if !active {
			for _, st := range []struct {
				q    string
				args []any
			}{
				{`DELETE FROM sessions WHERE user_id = ?`, []any{id}},
				{`UPDATE api_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, []any{now, id}},
				{`DELETE FROM oauth_refresh WHERE user_id = ?`, []any{id}},
				{`DELETE FROM oauth_codes WHERE user_id = ?`, []any{id}},
			} {
				if _, err := tx.Exec(st.q, st.args...); err != nil {
					return err
				}
			}
		}
		return s.journal(ctx, tx, a, change{Operation: op, Target: userTarget(id), Event: "admin.updated", Changes: map[string]any{"from": from, "to": to}})
	})
}
