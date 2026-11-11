package orgmigrate_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/orgmigrate"
	"github.com/kees/atlas/migrations"
)

var cfg = orgmigrate.Config{SuperadminEmail: "super@x.io", OrgKey: "MAIN", OrgName: "Main"}

var mapping = orgmigrate.Mapping{
	Roles:  map[string]string{"viewer": "member", "wide": "remove", "narrow": "member"},
	Admins: map[string]string{},
}

// fixture builds an old-world database: three admins (one deactivated), a project admin, a viewer, custom roles
// that are wider, narrower and equal to member, a user without membership and an unused admin invite.
func fixture(t *testing.T) (*db.DB, map[string]int64) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	a := auth.NewService(d)
	id := map[string]int64{}
	for _, u := range []struct {
		email string
		admin bool
	}{{"super@x.io", true}, {"admin2@x.io", true}, {"gone@x.io", true}, {"projadmin@x.io", false}, {"viewer@x.io", false},
		{"wide@x.io", false}, {"narrow@x.io", false}, {"same@x.io", false}, {"member@x.io", false}, {"nobody@x.io", false}} {
		created, err := a.CreateUser(u.email, u.email, "password-123", false)
		if err != nil {
			t.Fatal(err)
		}
		id[u.email] = created.ID
	}
	// The fixture is a pre-organization database: admins carry the legacy is_admin flag, nobody is_superadmin.
	for _, email := range []string{"super@x.io", "admin2@x.io", "gone@x.io"} {
		if _, err := d.W.Exec(`UPDATE users SET is_admin = 1 WHERE email = ?`, email); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.W.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`UPDATE users SET deactivated_at = 1 WHERE email = 'gone@x.io'`)
	exec(`INSERT INTO projects (id, key, name, created_at) VALUES (1, 'P1', 'One', 0), (2, 'P2', 'Two', 0)`)
	exec(`INSERT INTO roles (name, builtin) VALUES ('wide', 0), ('narrow', 0), ('same', 0)`)
	exec(`INSERT INTO role_actions (role, action) SELECT 'same', action FROM role_actions WHERE role = 'member'`)
	exec(`INSERT INTO role_actions (role, action) SELECT 'wide', action FROM role_actions WHERE role = 'member'`)
	exec(`INSERT INTO role_actions (role, action) VALUES ('wide', 'workflow'), ('narrow', 'view'), ('narrow', 'comment')`)
	for _, m := range []struct {
		email string
		pid   int
		role  string
	}{{"projadmin@x.io", 1, "admin"}, {"viewer@x.io", 1, "viewer"}, {"wide@x.io", 1, "wide"}, {"narrow@x.io", 2, "narrow"},
		{"same@x.io", 2, "same"}, {"member@x.io", 1, "member"}, {"member@x.io", 2, "member"}} {
		exec(`INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, ?)`, m.pid, id[m.email], m.role)
	}
	exec(`INSERT INTO invites (code_hash, is_admin, created_at, expires_at) VALUES ('h1', 1, 0, 9999999999999)`)
	exec(`INSERT INTO invites (code_hash, is_admin, created_at, expires_at, used_at) VALUES ('h2', 1, 0, 9999999999999, 1)`)
	return d, id
}

func count(t *testing.T, d *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.R.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestPreflightBlocksUnresolvedAndWrongInput(t *testing.T) {
	d, _ := fixture(t)
	rep, err := orgmigrate.Plan(d.R, cfg, orgmigrate.Mapping{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rep.Blocking, "|")
	for _, role := range []string{"viewer", "wide", "narrow"} {
		if !strings.Contains(joined, `"`+role+`"`) {
			t.Errorf("unresolved role %s does not block: %v", role, rep.Blocking)
		}
	}
	for name, c := range map[string]orgmigrate.Config{
		"no match":        {SuperadminEmail: "nobody@x.io", OrgKey: "MAIN", OrgName: "Main"},
		"deactivated one": {SuperadminEmail: "gone@x.io", OrgKey: "MAIN", OrgName: "Main"},
	} {
		r, _ := orgmigrate.Plan(d.R, c, mapping)
		if len(r.Blocking) == 0 {
			t.Errorf("%s: no blocking problem", name)
		}
	}
	bad := orgmigrate.Mapping{Roles: map[string]string{"member": "remove", "ghost": "member", "viewer": "member", "wide": "remove", "narrow": "member"}}
	if r, _ := orgmigrate.Plan(d.R, cfg, bad); len(r.Blocking) < 2 {
		t.Errorf("a mapping for a role that is not review-required or unknown must block: %v", r.Blocking)
	}
	if _, err := orgmigrate.Run(d, cfg, orgmigrate.Mapping{}, "x", orgmigrate.Options{BackupDir: filepath.Join(t.TempDir(), "b")}); err == nil {
		t.Fatal("a run with unresolved roles was allowed")
	}
	if count(t, d, `SELECT COUNT(*) FROM organizations`) != 0 {
		t.Fatal("a refused run changed the database")
	}
}

func TestDryRunWritesNothingAndIsStable(t *testing.T) {
	d, _ := fixture(t)
	first, err := orgmigrate.Plan(d.R, cfg, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Blocking) > 0 {
		t.Fatalf("blocking: %v", first.Blocking)
	}
	second, _ := orgmigrate.Plan(d.R, cfg, mapping)
	if first.Hash() != second.Hash() {
		t.Fatal("the report hash is not stable")
	}
	if count(t, d, `SELECT COUNT(*) FROM organizations`)+count(t, d, `SELECT COUNT(*) FROM users WHERE is_superadmin = 1`) != 0 {
		t.Fatal("the dry run wrote to the database")
	}
	kinds := map[string]string{}
	for _, tr := range first.Transitions {
		kinds[tr.User+"|"+tr.Project] = tr.Kind
		if tr.Kind == "UNEXPECTED" {
			t.Errorf("unexpected transition in a clean fixture: %+v", tr)
		}
	}
	for k, want := range map[string]string{
		"projadmin@x.io|P1": "approved-reduction", // admin -> member
		"viewer@x.io|P1":    "approved-expansion", // viewer -> member by mapping
		"narrow@x.io|P2":    "approved-expansion", // narrower custom role -> member by mapping
		"wide@x.io|P1":      "approved-reduction", // removed by mapping
	} {
		if kinds[k] != want {
			t.Errorf("%s: %q, want %q", k, kinds[k], want)
		}
	}
	for _, k := range []string{"member@x.io|P1", "same@x.io|P2", "admin2@x.io|P1", "super@x.io|P1", "nobody@x.io|P1"} {
		if _, ok := kinds[k]; ok {
			t.Errorf("%s must not change", k)
		}
	}
}

func TestRunMigratesAndVerifies(t *testing.T) {
	d, id := fixture(t)
	rep, _ := orgmigrate.Plan(d.R, cfg, mapping)
	opt := orgmigrate.Options{UploadsDir: t.TempDir(), BackupDir: filepath.Join(t.TempDir(), "backup")}
	if _, err := orgmigrate.Run(d, cfg, mapping, "not-the-hash", opt); !errors.Is(err, orgmigrate.ErrNotReviewed) {
		t.Fatalf("wrong hash: %v", err)
	}
	if _, err := orgmigrate.Run(d, cfg, mapping, rep.Hash(), opt); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM users WHERE is_superadmin = 1`); n != 1 {
		t.Fatalf("superadmins: %d", n)
	}
	role := func(email string) string {
		var r string
		err := d.R.QueryRow(`SELECT role FROM organization_memberships WHERE user_id = ?`, id[email]).Scan(&r)
		if errors.Is(err, sql.ErrNoRows) {
			return ""
		}
		return r
	}
	for email, want := range map[string]string{
		"super@x.io": "admin", "admin2@x.io": "admin", "gone@x.io": "", "projadmin@x.io": "user", "viewer@x.io": "user",
		"wide@x.io": "", "narrow@x.io": "user", "same@x.io": "user", "member@x.io": "user", "nobody@x.io": "",
	} {
		if got := role(email); got != want {
			t.Errorf("%s organization role %q, want %q", email, got, want)
		}
	}
	if n := count(t, d, `SELECT COUNT(*) FROM project_members WHERE role <> 'member'`); n != 0 {
		t.Errorf("%d project rows are not member", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM project_members WHERE user_id = ?`, id["wide@x.io"]); n != 0 {
		t.Error("the removed role kept its row")
	}
	if n := count(t, d, `SELECT COUNT(*) FROM projects WHERE organization_id IS NULL`); n != 0 {
		t.Errorf("%d projects without organization", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE name IN ('roles', 'role_actions')`); n != 0 {
		t.Error("the legacy role tables were not renamed")
	}
	if n := count(t, d, `SELECT COUNT(*) FROM legacy_role_actions`); n == 0 {
		t.Error("the legacy role actions were lost")
	}
	if n := count(t, d, `SELECT COUNT(*) FROM invites WHERE code_hash = 'h1' AND expires_at = 9999999999999`); n != 0 {
		t.Error("the unused admin invite was not revoked")
	}
	if n := count(t, d, `SELECT COUNT(*) FROM invites WHERE used_at IS NOT NULL AND expires_at = 9999999999999`); n != 1 {
		t.Error("a used invite was touched")
	}
	if n := count(t, d, `SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'is_admin'`) + count(t, d, `SELECT COUNT(*) FROM pragma_table_info('invites') WHERE name = 'is_admin'`); n != 0 {
		t.Error("the legacy is_admin columns are still there")
	}
	if _, err := d.W.Exec(`INSERT INTO projects (key, name, created_at) VALUES ('NOORG', 'x', 0)`); err == nil {
		t.Error("a project without organization was accepted after the migration")
	}
	if _, err := d.W.Exec(`UPDATE projects SET organization_id = NULL WHERE key = 'P1'`); err == nil {
		t.Error("a project lost its organization")
	}
	if rep2, err := orgmigrate.Plan(d.R, cfg, mapping); err != nil || len(rep2.Blocking) == 0 {
		t.Errorf("planning a migrated database must report it, not fail: %v %v", err, rep2.Blocking)
	}
	var stored string
	d.R.QueryRow(`SELECT value FROM meta WHERE key = 'org_migration_hash'`).Scan(&stored)
	if stored != rep.Hash() {
		t.Error("the report hash was not stored")
	}
	if _, err := orgmigrate.Run(d, cfg, mapping, rep.Hash(), orgmigrate.Options{BackupDir: filepath.Join(t.TempDir(), "b2")}); err == nil {
		t.Fatal("a second run was allowed")
	}
}

// Data that changed after the review must stop the run before anything is written.
func TestRunRefusesDriftSinceReview(t *testing.T) {
	d, id := fixture(t)
	rep, _ := orgmigrate.Plan(d.R, cfg, mapping)
	if _, err := d.W.Exec(`INSERT INTO project_members (project_id, user_id, role) VALUES (2, ?, 'viewer')`, id["nobody@x.io"]); err != nil {
		t.Fatal(err)
	}
	opt := orgmigrate.Options{UploadsDir: t.TempDir(), BackupDir: filepath.Join(t.TempDir(), "backup")}
	if _, err := orgmigrate.Run(d, cfg, mapping, rep.Hash(), opt); !errors.Is(err, orgmigrate.ErrNotReviewed) {
		t.Fatalf("drift: %v", err)
	}
	if count(t, d, `SELECT COUNT(*) FROM organizations`) != 0 {
		t.Fatal("a refused run changed the database")
	}
}

// Every kind of unexpected result must fail verification (ATL-476).
func TestVerifyCatchesUnexpectedResults(t *testing.T) {
	for name, tamper := range map[string]string{
		"second superadmin":      `UPDATE users SET is_superadmin = 1 WHERE email = 'member@x.io'`,
		"unexpected admin row":   `UPDATE organization_memberships SET role = 'admin' WHERE user_id = (SELECT id FROM users WHERE email = 'member@x.io')`,
		"extra organization row": `INSERT INTO organization_memberships (organization_id, user_id, role, created_at) SELECT 1, id, 'user', 0 FROM users WHERE email = 'nobody@x.io'`,
		"extra project":          `INSERT INTO project_members (project_id, user_id, role) SELECT 2, id, 'member' FROM users WHERE email = 'nobody@x.io'`,
		"role without approval":  `INSERT INTO project_members (project_id, user_id, role) SELECT 2, id, 'viewer' FROM users WHERE email = 'wide@x.io'`,
		"lost access":            `DELETE FROM project_members WHERE user_id = (SELECT id FROM users WHERE email = 'member@x.io') AND project_id = 1`,
	} {
		t.Run(name, func(t *testing.T) {
			d, _ := fixture(t)
			rep, _ := orgmigrate.Plan(d.R, cfg, mapping)
			tx, err := d.W.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			fresh, _ := orgmigrate.Plan(tx, cfg, mapping)
			old, err := orgmigrate.SnapshotOld(tx, fresh)
			if err != nil {
				t.Fatal(err)
			}
			if err := orgmigrate.Apply(tx, fresh, 1); err != nil {
				t.Fatal(err)
			}
			if bad := orgmigrate.Verify(tx, fresh, old); len(bad) != 0 {
				t.Fatalf("a clean apply fails verification: %v", bad)
			}
			// the trigger would refuse the tampering that verification must be able to catch on its own
			if _, err := tx.Exec(`DROP TRIGGER project_members_org_member`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(tamper); err != nil {
				t.Fatal(err)
			}
			if bad := orgmigrate.Verify(tx, fresh, old); len(bad) == 0 {
				t.Fatalf("%s was not detected (hash %s)", name, rep.Hash())
			}
		})
	}
}

// The built-in viewer role exists in every database; unused, it needs no decision.
func TestUnusedReviewRequiredRoleDoesNotBlock(t *testing.T) {
	d, _ := fixture(t)
	if _, err := d.W.Exec(`DELETE FROM project_members WHERE role IN ('viewer', 'wide', 'narrow')`); err != nil {
		t.Fatal(err)
	}
	rep, err := orgmigrate.Plan(d.R, cfg, orgmigrate.Mapping{})
	if err != nil || len(rep.Blocking) != 0 {
		t.Fatalf("unused roles blocked the migration: %v %v", err, rep.Blocking)
	}
}

// Verify re-implements what the new model grants; this checks that against the real authorization code, so the
// verification is not self-referential: after the migration every user sees and administers exactly the projects
// the migrated tables say.
func TestMigratedAccessMatchesAuthorization(t *testing.T) {
	d, id := fixture(t)
	rep, _ := orgmigrate.Plan(d.R, cfg, mapping)
	if _, err := orgmigrate.Run(d, cfg, mapping, rep.Hash(), orgmigrate.Options{UploadsDir: t.TempDir(), BackupDir: filepath.Join(t.TempDir(), "b")}); err != nil {
		t.Fatal(err)
	}
	svc := core.New(d, auth.NewService(d))
	ctx := context.Background()
	want := map[string]map[string]string{ // email -> project -> "admin" | "member" | ""
		"super@x.io":     {"P1": "admin", "P2": "admin"},
		"admin2@x.io":    {"P1": "admin", "P2": "admin"},
		"gone@x.io":      {},
		"projadmin@x.io": {"P1": "member"},
		"viewer@x.io":    {"P1": "member"},
		"wide@x.io":      {},
		"narrow@x.io":    {"P2": "member"},
		"same@x.io":      {"P2": "member"},
		"member@x.io":    {"P1": "member", "P2": "member"},
		"nobody@x.io":    {},
	}
	for email, projects := range want {
		a := auth.Actor{Kind: auth.KindHuman, UserID: id[email]}
		for _, key := range []string{"P1", "P2"} {
			p, err := svc.GetProject(ctx, a, key)
			switch level := projects[key]; {
			case level == "" && !errors.Is(err, core.ErrNotFound) && !errors.Is(err, core.ErrForbidden):
				t.Errorf("%s sees %s (%v)", email, key, err)
			case level != "" && err != nil:
				t.Errorf("%s cannot see %s: %v", email, key, err)
			case level != "" && p.Role != level:
				t.Errorf("%s on %s: role %q, want %q", email, key, p.Role, level)
			}
		}
	}
}

// A fresh database has no legacy data: Finalize enforces project organizations, drops the legacy flags and can be
// run again.
func TestFinalizeFreshDatabase(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := orgmigrate.Finalize(d); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	if n := count(t, d, `SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'is_admin'`); n != 0 {
		t.Error("users.is_admin is still there")
	}
	if _, err := d.W.Exec(`INSERT INTO projects (key, name, created_at) VALUES ('NOORG', 'x', 0)`); err == nil {
		t.Error("a project without organization was accepted")
	}
	if _, err := d.W.Exec(`INSERT INTO organizations (id, key, name, created_at) VALUES (1, 'A', 'A', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`INSERT INTO projects (key, name, created_at, organization_id) VALUES ('OK', 'x', 0, 1)`); err != nil {
		t.Errorf("a project with an organization was refused: %v", err)
	}
}

// Finalize refuses a database that still has to be migrated: it would drop the flags the migration reads.
func TestFinalizeRefusesUnmigratedDatabase(t *testing.T) {
	d, _ := fixture(t) // legacy: projects without organization, admins only by is_admin
	if err := orgmigrate.Finalize(d); err == nil {
		t.Fatal("Finalize ran on an unmigrated database")
	}
	if n := count(t, d, `SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'is_admin'`); n != 1 {
		t.Fatal("the legacy column was dropped")
	}
	// users without any SUPERADMIN count as unmigrated too, even when no project exists
	if _, err := d.W.Exec(`DELETE FROM project_members; DELETE FROM projects`); err != nil {
		t.Fatal(err)
	}
	if err := orgmigrate.Finalize(d); err == nil {
		t.Fatal("Finalize ran on a database with users but no SUPERADMIN")
	}
}
