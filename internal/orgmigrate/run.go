package orgmigrate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kees/atlas/internal/backup"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
)

// ErrNotReviewed means the database no longer produces the report the owner approved.
var ErrNotReviewed = errors.New("the current report differs from the reviewed one (confirm-report hash); review it again")

type matrix map[[2]int64]map[string]bool // user id, project id -> actions

// snapshotOld records what every active user may do today, before anything changes.
func snapshotOld(q Reader, rep Report) (matrix, error) {
	users, _, err := loadUsers(q)
	if err != nil {
		return nil, err
	}
	projects, err := loadProjects(q)
	if err != nil {
		return nil, err
	}
	roleOf := map[string]*RoleRow{}
	for i := range rep.Roles {
		roleOf[rep.Roles[i].Name] = &rep.Roles[i]
	}
	out := matrix{}
	for _, u := range users {
		if !u.active {
			continue
		}
		for _, p := range projects {
			out[[2]int64{u.id, p.id}] = oldActions(u, p.id, roleOf)
		}
	}
	return out, nil
}

// currentNew reads what the new model grants from the tables as they are now.
func currentNew(q Reader) (matrix, error) {
	rows, err := q.Query(`SELECT u.id, p.id, u.is_superadmin = 1,
		COALESCE((SELECT role FROM organization_memberships m WHERE m.organization_id = p.organization_id AND m.user_id = u.id), ''),
		EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = p.id AND pm.user_id = u.id AND pm.role = 'member')
		FROM users u CROSS JOIN projects p WHERE u.deactivated_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := matrix{}
	for rows.Next() {
		var uid, pid int64
		var super, member bool
		var orgRole string
		if err := rows.Scan(&uid, &pid, &super, &orgRole, &member); err != nil {
			return nil, err
		}
		set := map[string]bool{}
		switch {
		case super || orgRole == "admin":
			set = allActions()
		case orgRole != "" && member:
			set = actionSet(core.MemberActions)
		}
		out[[2]int64{uid, pid}] = set
	}
	return out, rows.Err()
}

// Apply writes the mapping of the plan. It must run in the transaction that Plan and Verify also use.
func Apply(tx *sql.Tx, rep Report, now int64) error {
	res, err := tx.Exec(`INSERT INTO organizations (key, name, created_at) VALUES (?, ?, ?)`, rep.OrgKey, rep.OrgName, now)
	if err != nil {
		return err
	}
	orgID, _ := res.LastInsertId()
	if _, err := tx.Exec(`UPDATE projects SET organization_id = ?`, orgID); err != nil {
		return err
	}
	for _, a := range rep.Admins {
		if a.Decision == "superadmin" {
			if _, err := tx.Exec(`UPDATE users SET is_superadmin = 1 WHERE id = ?`, a.ID); err != nil {
				return err
			}
		}
	}
	for uid, role := range rep.OrgMembers {
		if _, err := tx.Exec(`INSERT INTO organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`, orgID, uid, role, now); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`SELECT project_id, user_id FROM project_members`)
	if err != nil {
		return err
	}
	var all [][2]int64
	for rows.Next() {
		var pid, uid int64
		if err := rows.Scan(&pid, &uid); err != nil {
			rows.Close()
			return err
		}
		all = append(all, [2]int64{pid, uid})
	}
	rows.Close()
	for _, r := range all {
		if _, keep := rep.ProjectRows[fmt.Sprintf("%d:%d", r[0], r[1])]; keep {
			_, err = tx.Exec(`UPDATE project_members SET role = 'member' WHERE project_id = ? AND user_id = ?`, r[0], r[1])
		} else {
			_, err = tx.Exec(`DELETE FROM project_members WHERE project_id = ? AND user_id = ?`, r[0], r[1])
		}
		if err != nil {
			return err
		}
	}
	for _, q := range []string{
		`ALTER TABLE roles RENAME TO legacy_roles`,
		`ALTER TABLE role_actions RENAME TO legacy_role_actions`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE invites SET expires_at = MIN(expires_at, ?) WHERE is_admin = 1 AND used_at IS NULL`, now); err != nil {
		return err
	}
	body, _ := json.Marshal(rep)
	for k, v := range map[string]string{"org_migration_hash": rep.Hash(), "org_migration_report": string(body)} {
		if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO audit_entries (actor, operation, target, changes, ts) VALUES ('system:migrate-orgs', 'org.migrate', ?, ?, ?)`,
		"org:"+rep.OrgKey, fmt.Sprintf(`{"report_hash":%q}`, rep.Hash()), now); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO change_events (type, project, item_key, actor, ts) VALUES ('admin.updated', '', '', 'system:migrate-orgs', ?)`, now); err != nil {
		return err
	}
	return finalizeTx(tx) // projects keep their organization, the legacy admin flags go
}

// Verify checks the written result in both directions and returns every problem; an empty list means it holds.
// old is the snapshot taken before Apply, counts come from the report.
func Verify(tx *sql.Tx, rep Report, old matrix) []string {
	var bad []string
	add := func(f string, a ...any) { bad = append(bad, fmt.Sprintf(f, a...)) }

	for name, want := range rep.Counts {
		if name == "project_members" {
			want = len(rep.ProjectRows)
		}
		var got int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM ` + name).Scan(&got); err != nil || got != want {
			add("count of %s: %d, expected %d (%v)", name, got, want, err)
		}
	}
	var supers int
	var superEmail string
	_ = tx.QueryRow(`SELECT COUNT(*), COALESCE(MAX(email), '') FROM users WHERE is_superadmin = 1`).Scan(&supers, &superEmail)
	if supers != 1 || !strings.EqualFold(superEmail, rep.Superadmin) {
		add("expected exactly one SUPERADMIN (%s), found %d", rep.Superadmin, supers)
	}
	got := map[int64]string{}
	if rows, err := tx.Query(`SELECT user_id, role FROM organization_memberships`); err != nil {
		add("organization memberships: %v", err)
	} else {
		for rows.Next() {
			var uid int64
			var role string
			_ = rows.Scan(&uid, &role)
			got[uid] = role
		}
		rows.Close()
	}
	for uid, role := range got {
		if rep.OrgMembers[uid] != role {
			add("UNEXPECTED organization membership: user %d as %q", uid, role)
		}
	}
	for uid, role := range rep.OrgMembers {
		if got[uid] != role {
			add("missing organization membership: user %d as %q", uid, role)
		}
	}
	rows := map[string]bool{}
	if r, err := tx.Query(`SELECT project_id, user_id, role FROM project_members`); err != nil {
		add("project members: %v", err)
	} else {
		for r.Next() {
			var pid, uid int64
			var role string
			_ = r.Scan(&pid, &uid, &role)
			k := strconv.FormatInt(pid, 10) + ":" + strconv.FormatInt(uid, 10)
			rows[k] = true
			if _, ok := rep.ProjectRows[k]; !ok || role != "member" {
				add("UNEXPECTED project membership %s as %q", k, role)
			}
		}
		r.Close()
	}
	for k := range rep.ProjectRows {
		if !rows[k] {
			add("missing project membership %s", k)
		}
	}
	var orphans int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM organizations o WHERE o.status = 'active' AND NOT EXISTS (
		SELECT 1 FROM organization_memberships m JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = o.id AND m.role = 'admin' AND u.deactivated_at IS NULL)`).Scan(&orphans)
	if orphans > 0 {
		add("an organization has no active admin")
	}
	var legacy int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('legacy_roles', 'legacy_role_actions')`).Scan(&legacy)
	if legacy != 2 {
		add("legacy role tables were not retained")
	}

	// LOSS and EXPANSION: every difference between old and new must be a transition of the approved report
	nw, err := currentNew(tx)
	if err != nil {
		return append(bad, err.Error())
	}
	uemail, pkey := map[int64]string{}, map[int64]string{}
	ur, _ := tx.Query(`SELECT id, email FROM users`)
	for ur.Next() {
		var id int64
		var e string
		_ = ur.Scan(&id, &e)
		uemail[id] = e
	}
	ur.Close()
	pr, _ := tx.Query(`SELECT id, key FROM projects`)
	for pr.Next() {
		var id int64
		var k string
		_ = pr.Scan(&id, &k)
		pkey[id] = k
	}
	pr.Close()
	approved := map[string]Transition{}
	for _, t := range rep.Transitions {
		approved[t.User+"|"+t.Project] = t
	}
	seen := map[string]bool{}
	for k, before := range old {
		lost, gained := diff(before, nw[k])
		if len(lost) == 0 && len(gained) == 0 {
			continue
		}
		name := uemail[k[0]] + "|" + pkey[k[1]]
		seen[name] = true
		t, ok := approved[name]
		if !ok || t.Kind == "UNEXPECTED" || !sameList(t.Lost, lost) || !sameList(t.Gained, gained) {
			add("UNEXPECTED change for %s: lost %v, gained %v", name, lost, gained)
		}
	}
	for name := range approved {
		if !seen[name] {
			add("approved transition %s did not happen", name)
		}
	}
	if fk, err := tx.Query(`PRAGMA foreign_key_check`); err != nil {
		add("foreign_key_check: %v", err)
	} else {
		if fk.Next() {
			add("foreign_key_check reports violations")
		}
		fk.Close()
	}
	return bad
}

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Options locate the side files of a real run.
type Options struct {
	UploadsDir string
	BackupDir  string // a new directory; kept until the owner closes the migration
}

// Run performs the real migration: reviewed hash, verified backup, then one transaction that re-plans, compares,
// applies and verifies (ADR-0028). Any problem rolls everything back.
func Run(d *db.DB, cfg Config, m Mapping, confirm string, opt Options) (Report, error) {
	rep, err := Plan(d.R, cfg, m)
	if err != nil {
		return rep, err
	}
	if len(rep.Blocking) > 0 {
		return rep, fmt.Errorf("preflight failed: %s", strings.Join(rep.Blocking, "; "))
	}
	if rep.Hash() != confirm {
		return rep, ErrNotReviewed
	}
	if err := verifiedBackup(d, cfg, m, confirm, opt); err != nil {
		return rep, fmt.Errorf("backup: %w", err)
	}

	tx, err := d.W.Begin() // BEGIN IMMEDIATE: no other writer can interleave from here on
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()
	fresh, err := Plan(tx, cfg, m)
	if err != nil {
		return rep, err
	}
	if len(fresh.Blocking) > 0 {
		return fresh, fmt.Errorf("preflight failed: %s", strings.Join(fresh.Blocking, "; "))
	}
	if fresh.Hash() != confirm {
		return fresh, ErrNotReviewed
	}
	old, err := snapshotOld(tx, fresh)
	if err != nil {
		return fresh, err
	}
	if err := Apply(tx, fresh, time.Now().UnixMilli()); err != nil {
		return fresh, err
	}
	if bad := Verify(tx, fresh, old); len(bad) > 0 {
		return fresh, fmt.Errorf("verification failed, nothing was changed: %s", strings.Join(bad, "; "))
	}
	if err := tx.Commit(); err != nil {
		return fresh, err
	}
	var res string
	if err := d.W.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		return fresh, fmt.Errorf("integrity_check after commit: %q %v", res, err)
	}
	return fresh, nil
}

// verifiedBackup writes the backup and proves it can be restored: a restored copy passes the integrity check and
// produces the same report as the reviewed one.
func verifiedBackup(d *db.DB, cfg Config, m Mapping, confirm string, opt Options) error {
	if opt.BackupDir == "" {
		return errors.New("a backup directory is required")
	}
	if _, err := backup.Create(d, opt.UploadsDir, opt.BackupDir); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "atlas-restore-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := backup.Restore(opt.BackupDir, tmp); err != nil {
		return fmt.Errorf("restore test: %w", err)
	}
	copyDB, err := db.Open(filepath.Join(tmp, "atlas.db"), db.Options{})
	if err != nil {
		return fmt.Errorf("restore test: %w", err)
	}
	defer copyDB.Close()
	if err := copyDB.QuickCheck(); err != nil {
		return fmt.Errorf("restore test: %w", err)
	}
	var full string
	if err := copyDB.W.QueryRow(`PRAGMA integrity_check`).Scan(&full); err != nil || full != "ok" {
		return fmt.Errorf("restore test: integrity_check %q %v", full, err)
	}
	rep, err := Plan(copyDB.R, cfg, m)
	if err != nil {
		return fmt.Errorf("restore test: %w", err)
	}
	if rep.Hash() != confirm {
		return errors.New("restore test: the restored copy produces a different report")
	}
	return nil
}
