package db_test

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/migrations"
)

// The review-category migration rebuilds project_statuses: ids, items and status history must survive it.
func TestReviewCategoryMigrationKeepsReferences(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	before := fstest.MapFS{}
	all, _ := fs.Glob(migrations.FS, "*.sql")
	for _, f := range all {
		if !strings.HasPrefix(f, "0019") {
			b, _ := fs.ReadFile(migrations.FS, f)
			before[f] = &fstest.MapFile{Data: b}
		}
	}
	if err := d.Migrate(before, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO projects (id, key, name, created_at) VALUES (1, 'EXO', 'Exo', 0)`,
		`INSERT INTO project_statuses (id, project_id, name, position, category) VALUES (1, 1, 'Todo', 0, 'todo'), (2, 1, 'Review', 1, 'active'), (3, 1, 'Blocked', 2, 'active')`,
		`INSERT INTO work_items (id, key, project_id, path, type, title, status_id, rank, created_at, updated_at) VALUES (1, 'EXO-1', 1, '1', 'task', 'x', 2, 'a', 0, 0)`,
	} {
		if _, err := d.W.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	var cat string
	var items int
	d.R.QueryRow(`SELECT category FROM project_statuses WHERE id = 2`).Scan(&cat)
	d.R.QueryRow(`SELECT COUNT(*) FROM work_items i JOIN project_statuses s ON s.id = i.status_id WHERE s.category = 'review'`).Scan(&items)
	if cat != "review" || items != 1 {
		t.Fatalf("review status: category %q, items %d", cat, items)
	}
	d.R.QueryRow(`SELECT category FROM project_statuses WHERE id = 3`).Scan(&cat)
	if cat != "active" {
		t.Fatalf("other statuses must keep their category, got %q", cat)
	}
	var fk int
	d.W.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign keys were not switched back on")
	}
}

// Account status (ADR-0030): deactivated and anonymised users become suspended, everyone else stays active.
func TestAccountStatusBackfill(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	before := fstest.MapFS{}
	all, _ := fs.Glob(migrations.FS, "*.sql")
	for _, f := range all {
		if !strings.HasPrefix(f, "0031") && !strings.HasPrefix(f, "0033") {
			b, _ := fs.ReadFile(migrations.FS, f)
			before[f] = &fstest.MapFile{Data: b}
		}
	}
	if err := d.Migrate(before, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, name, password_hash, created_at, is_superadmin) VALUES (1, 'a@x.nl', 'A', 'h', 0, 1)`,
		`INSERT INTO users (id, email, name, password_hash, created_at, deactivated_at) VALUES (2, 'b@x.nl', 'B', 'h', 0, 5)`,
		`INSERT INTO users (id, email, name, password_hash, created_at, deactivated_at) VALUES (3, 'deleted-3@deleted.invalid', 'Deleted user', '!x', 0, 7)`,
		`INSERT INTO users (id, email, name, password_hash, created_at) VALUES (4, 'c@x.nl', 'C', 'h', 0)`,
	} {
		if _, err := d.W.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	want := map[int]string{1: "active", 2: "suspended", 3: "suspended", 4: "active"}
	for id, st := range want {
		var got string
		d.R.QueryRow(`SELECT status FROM users WHERE id = ?`, id).Scan(&got)
		if got != st {
			t.Errorf("user %d: status %q, want %q", id, got, st)
		}
	}
	if _, err := d.W.Exec(`UPDATE users SET status = 'gone' WHERE id = 4`); err == nil {
		t.Error("an unknown status was accepted")
	}
	var bad string
	if err := d.R.QueryRow(`PRAGMA integrity_check`).Scan(&bad); err != nil || bad != "ok" {
		t.Fatalf("integrity check: %q %v", bad, err)
	}
}

// Organization tables (ADR-0027): the constraints are the last line of defence for the role and key rules.
func TestOrganizationConstraints(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, name, password_hash, created_at) VALUES (1, 'a@x.nl', 'A', 'h', 0)`,
		`INSERT INTO organizations (id, key, name, created_at) VALUES (1, 'ACME', 'Acme', 0)`,
		`INSERT INTO organization_memberships (organization_id, user_id, role, created_at) VALUES (1, 1, 'admin', 0)`,
	} {
		if _, err := d.W.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for name, q := range map[string]string{
		"key unique ignoring case": `INSERT INTO organizations (key, name, created_at) VALUES ('acme', 'Other', 0)`,
		"unknown role":             `INSERT INTO organizations (id, key, name, created_at) VALUES (2, 'B', 'B', 0); INSERT INTO organization_memberships (organization_id, user_id, role, created_at) VALUES (2, 1, 'viewer', 0)`,
		"duplicate membership":     `INSERT INTO organization_memberships (organization_id, user_id, role, created_at) VALUES (1, 1, 'user', 0)`,
		"unknown status":           `INSERT INTO organizations (key, name, status, created_at) VALUES ('C', 'C', 'gone', 0)`,
		"superadmin is a flag":     `UPDATE users SET is_superadmin = 2`,
	} {
		if _, err := d.W.Exec(q); err == nil {
			t.Errorf("%s: statement was accepted", name)
		}
	}
	if _, err := d.W.Exec(`INSERT INTO projects (key, name, created_at) VALUES ('NOORG', 'No organization yet', 0)`); err != nil {
		t.Fatalf("a project without organization must stay valid until the data migration: %v", err)
	}
	if _, err := d.W.Exec(`INSERT INTO projects (key, name, created_at, organization_id) VALUES ('BAD', 'Bad', 0, 99)`); err == nil {
		t.Error("a project in a missing organization was accepted")
	}
	var super int
	d.R.QueryRow(`SELECT is_superadmin FROM users WHERE id = 1`).Scan(&super)
	if super != 0 {
		t.Fatalf("a new user must not be superadmin, got %d", super)
	}
}

// Free accounts (ADR-0031): waiting and declined accounts become ordinary active ones, the retired registration mode
// and setting disappear, nothing else changes.
func TestFreeAccountsMigration(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	before := fstest.MapFS{}
	all, _ := fs.Glob(migrations.FS, "*.sql")
	for _, f := range all {
		if !strings.HasPrefix(f, "0033") {
			b, _ := fs.ReadFile(migrations.FS, f)
			before[f] = &fstest.MapFile{Data: b}
		}
	}
	if err := d.Migrate(before, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, name, password_hash, created_at, status) VALUES (1, 'a@x.nl', 'A', 'h', 0, 'active')`,
		`INSERT INTO users (id, email, name, password_hash, created_at, status) VALUES (2, 'p@x.nl', 'P', 'h', 0, 'pending')`,
		`INSERT INTO users (id, email, name, password_hash, created_at, status, rejection_note) VALUES (3, 'r@x.nl', 'R', 'h', 0, 'rejected', 'no')`,
		`INSERT INTO users (id, email, name, password_hash, created_at, status, deactivated_at) VALUES (4, 's@x.nl', 'S', 'h', 0, 'suspended', 5)`,
		`INSERT INTO instance_settings (key, value) VALUES ('registration', 'request'), ('approval_by', 'admin'), ('session_days', '7')`,
	} {
		if _, err := d.W.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	want := map[int]string{1: "active", 2: "active", 3: "active", 4: "suspended"}
	for id, st := range want {
		var got string
		d.R.QueryRow(`SELECT status FROM users WHERE id = ?`, id).Scan(&got)
		if got != st {
			t.Errorf("user %d: status %q, want %q", id, got, st)
		}
	}
	var mode, days string
	var left int
	d.R.QueryRow(`SELECT value FROM instance_settings WHERE key = 'registration'`).Scan(&mode)
	d.R.QueryRow(`SELECT value FROM instance_settings WHERE key = 'session_days'`).Scan(&days)
	d.R.QueryRow(`SELECT COUNT(*) FROM instance_settings WHERE key = 'approval_by'`).Scan(&left)
	if mode != "closed" || days != "7" || left != 0 {
		t.Errorf("settings after the migration: registration %q, session_days %q, approval_by rows %d", mode, days, left)
	}
}
