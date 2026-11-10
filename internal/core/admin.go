package core

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

type AdminUser struct {
	User
	MustChangePassword bool   `json:"must_change_password"`
	DeactivatedAt      *int64 `json:"deactivated_at"`
	Status             string `json:"status"` // pending, active, rejected or suspended
	CreatedAt          int64  `json:"created_at"`
}

func (s *Service) ListAdminUsers(ctx context.Context, a auth.Actor) ([]AdminUser, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT id, email, name, is_superadmin, must_change_password, deactivated_at, status, created_at FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminUser{}
	for rows.Next() {
		var u AdminUser
		var off sql.NullInt64
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsSuperadmin, &u.MustChangePassword, &off, &u.Status, &u.CreatedAt); err != nil {
			return nil, err
		}
		if off.Valid {
			u.DeactivatedAt = &off.Int64
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// activeSuperadmins counts admins that can still sign in, optionally ignoring one user.
func activeSuperadmins(tx *sql.Tx, except int64) (int, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE is_superadmin = 1 AND status = 'active' AND id <> ?`, except).Scan(&n)
	return n, err
}

// SetUserSuperadmin grants or removes global admin. The last active admin cannot be demoted.
func (s *Service) SetUserSuperadmin(ctx context.Context, a auth.Actor, id int64, admin bool) error {
	if err := s.adminOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		if admin {
			var status string
			if err := tx.QueryRow(`SELECT status FROM users WHERE id = ?`, id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			if status != "active" {
				return rule("only an active account can become superadmin")
			}
		}
		if !admin {
			if n, err := activeSuperadmins(tx, id); err != nil {
				return err
			} else if n == 0 {
				return rule("there must be at least one active superadmin")
			}
		}
		res, err := tx.Exec(`UPDATE users SET is_superadmin = ? WHERE id = ?`, admin, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "user.superadmin", Target: userTarget(id), Event: "admin.updated", Changes: map[string]any{"is_superadmin": admin}})
	})
}

// ForcePasswordReset signs the user out everywhere; their next session can only change the password.
// Tokens keep working: they are separate credentials the user or an admin revokes on their own.
func (s *Service) ForcePasswordReset(ctx context.Context, a auth.Actor, id int64) error {
	if err := s.adminOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE users SET must_change_password = 1 WHERE id = ? AND status = 'active'`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "user.force_password", Target: userTarget(id), Event: "admin.updated", Changes: map[string]any{}})
	})
}

func userTarget(id int64) string { return "user-" + strconv.FormatInt(id, 10) }

type AuditEntry struct {
	ID        int64  `json:"id"`
	Actor     string `json:"actor"`
	Operation string `json:"operation"`
	Target    string `json:"target"`
	Changes   string `json:"changes"`
	BatchID   string `json:"batch_id,omitempty"`
	TS        int64  `json:"ts"`
}

type AuditFilter struct {
	Actor, Operation, Target string
	Before                   int64 // entries with a smaller id, for paging
	Limit                    int
}

// AuditLog lists audit entries newest first (admins only). Filters are exact, except Operation which is a prefix.
func (s *Service) AuditLog(ctx context.Context, a auth.Actor, f AuditFilter) ([]AuditEntry, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	var conds []string
	var args []any
	if f.Actor != "" {
		conds, args = append(conds, "actor = ?"), append(args, f.Actor)
	}
	if f.Operation != "" {
		conds, args = append(conds, "operation LIKE ? ESCAPE '\\'"), append(args, strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Operation)+"%")
	}
	if f.Target != "" {
		conds, args = append(conds, "target = ?"), append(args, f.Target)
	}
	if f.Before > 0 {
		conds, args = append(conds, "id < ?"), append(args, f.Before)
	}
	q := `SELECT id, actor, operation, target, changes, COALESCE(batch_id, ''), ts FROM audit_entries`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	rows, err := s.db.R.Query(q+" ORDER BY id DESC LIMIT ?", append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Actor, &e.Operation, &e.Target, &e.Changes, &e.BatchID, &e.TS); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AdminUserPatch is a set of administrator changes to one account, applied together.
type AdminUserPatch struct {
	IsSuperadmin       *bool
	Active             *bool
	ResetTOTP          bool
	ForcePasswordReset bool
}

// UpdateUser applies every requested change in one transaction: all of them take effect or none does.
func (s *Service) UpdateUser(ctx context.Context, a auth.Actor, id int64, p AdminUserPatch) error {
	if err := s.adminOnly(a); err != nil {
		return err
	}
	_, err := s.Idempotent(ctx, a, "", nil, nil, func(ctx context.Context, s *Service) error {
		if p.ResetTOTP {
			if err := s.ResetUserTOTP(ctx, a, id); err != nil {
				return err
			}
		}
		if p.ForcePasswordReset {
			if err := s.ForcePasswordReset(ctx, a, id); err != nil {
				return err
			}
		}
		if p.IsSuperadmin != nil {
			if err := s.SetUserSuperadmin(ctx, a, id, *p.IsSuperadmin); err != nil {
				return err
			}
		}
		if p.Active != nil {
			return s.SetUserActive(ctx, a, id, *p.Active)
		}
		return nil
	})
	return err
}

// GrantSuperadmin is the operator path (`atlas superadmin grant <email>`): it needs shell access to the data
// directory and is the only way to make a SUPERADMIN without an existing one, besides the first-install bootstrap
// and the data migration. No HTTP route reaches it.
func (s *Service) GrantSuperadmin(ctx context.Context, email string) error {
	a := auth.Actor{Kind: auth.KindSystem}
	return s.tx(func(tx *sql.Tx) error {
		var id int64
		err := tx.QueryRow(`SELECT id FROM users WHERE email = ? AND status = 'active'`, strings.TrimSpace(email)).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return invalid("no active account has that email")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE users SET is_superadmin = 1 WHERE id = ?`, id); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "user.superadmin", Target: userTarget(id), Event: "admin.updated",
			Changes: map[string]any{"is_superadmin": true, "via": "cli"}})
	})
}
