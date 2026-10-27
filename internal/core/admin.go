package core

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// Instance administration: global admins only, through a user session (never a token).
func adminOnly(a auth.Actor) error {
	if a.Kind != auth.KindHuman || !a.IsAdmin {
		return ErrForbidden
	}
	return nil
}

type AdminUser struct {
	User
	DeactivatedAt *int64 `json:"deactivated_at"`
	CreatedAt     int64  `json:"created_at"`
}

func (s *Service) ListAdminUsers(ctx context.Context, a auth.Actor) ([]AdminUser, error) {
	if err := adminOnly(a); err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT id, email, name, is_admin, deactivated_at, created_at FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminUser{}
	for rows.Next() {
		var u AdminUser
		var off sql.NullInt64
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &off, &u.CreatedAt); err != nil {
			return nil, err
		}
		if off.Valid {
			u.DeactivatedAt = &off.Int64
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// activeAdmins counts admins that can still sign in, optionally ignoring one user.
func activeAdmins(tx *sql.Tx, except int64) (int, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND deactivated_at IS NULL AND id <> ?`, except).Scan(&n)
	return n, err
}

// SetUserAdmin grants or removes global admin. The last active admin cannot be demoted.
func (s *Service) SetUserAdmin(ctx context.Context, a auth.Actor, id int64, admin bool) error {
	if err := adminOnly(a); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		if !admin {
			if n, err := activeAdmins(tx, id); err != nil {
				return err
			} else if n == 0 {
				return rule("there must be at least one active admin")
			}
		}
		res, err := tx.Exec(`UPDATE users SET is_admin = ? WHERE id = ?`, admin, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.journal(ctx, tx, a, change{Operation: "user.admin", Target: userTarget(id), Event: "admin.updated", Changes: map[string]any{"is_admin": admin}})
	})
}

// SetUserActive deactivates (sessions end at once, tokens stop working) or reactivates a user.
// Nobody can deactivate themselves, and the last active admin stays.
func (s *Service) SetUserActive(ctx context.Context, a auth.Actor, id int64, active bool) error {
	if err := adminOnly(a); err != nil {
		return err
	}
	if !active && id == a.UserID {
		return rule("you cannot deactivate yourself")
	}
	return s.tx(func(tx *sql.Tx) error {
		var admin bool
		if err := tx.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, id).Scan(&admin); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			return err
		}
		if !active && admin {
			if n, err := activeAdmins(tx, id); err != nil {
				return err
			} else if n == 0 {
				return rule("there must be at least one active admin")
			}
		}
		var at any
		if !active {
			at = s.nowMs()
		}
		if _, err := tx.Exec(`UPDATE users SET deactivated_at = ? WHERE id = ?`, at, id); err != nil {
			return err
		}
		if !active {
			if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "user.active", Target: userTarget(id), Event: "admin.updated", Changes: map[string]any{"active": active}})
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
	if err := adminOnly(a); err != nil {
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

// RightRow is one line of the rights matrix: what each project role may do. It documents what the
// code enforces (the minimum role each service demands); a test keeps the two in step.
type RightRow struct {
	Action string `json:"action"`
	Viewer bool   `json:"viewer"`
	Member bool   `json:"member"`
	Admin  bool   `json:"admin"`
}

func RightsMatrix() []RightRow {
	return []RightRow{
		{"view", true, true, true},
		{"export", true, true, true},
		{"item.create", false, true, true},
		{"item.update", false, true, true},
		{"comment", false, true, true},
		{"import", false, true, true},
		{"workflow", false, false, true},
		{"project.update", false, false, true},
		{"members", false, false, true},
		{"tokens", false, false, true},
	}
}
