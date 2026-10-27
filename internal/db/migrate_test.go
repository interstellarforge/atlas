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
