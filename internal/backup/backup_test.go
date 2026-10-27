package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/backup"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/migrations"
)

// seed makes a migrated database with one attachment row pointing at a real upload.
func seed(t *testing.T, dir string) (*db.DB, string) {
	t.Helper()
	d, err := db.Open(filepath.Join(dir, "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(migrations.FS, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	uploads := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(filepath.Join(uploads, "ab"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploads, "ab", "abc.png"), []byte("png-bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	au := auth.NewService(d)
	u, err := au.CreateUser("a@x.io", "A", "password-123", true)
	if err != nil {
		t.Fatal(err)
	}
	svc, actor := core.New(d, au), auth.Actor{Kind: auth.KindHuman, UserID: u.ID, IsAdmin: true}
	if _, err := svc.CreateProject(context.Background(), actor, "EXO", "Exo"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateItem(context.Background(), actor, core.CreateItem{Project: "EXO", Type: "task", Title: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`INSERT INTO attachments (item_id, filename, mime, size, width, height, path, created_by, created_at)
		VALUES (1, 'a.png', 'image/png', 9, 1, 1, 'ab/abc.png', 'x', 1)`); err != nil {
		t.Fatal(err)
	}
	return d, uploads
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	src := t.TempDir()
	d, uploads := seed(t, src)
	var oldGen string
	d.R.QueryRow(`SELECT value FROM meta WHERE key = 'generation'`).Scan(&oldGen)

	bk := filepath.Join(t.TempDir(), "bk")
	m, err := backup.Create(d, uploads, bk)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 2 || m.Generation != oldGen || m.Schema == "" {
		t.Fatalf("manifest: %+v", m)
	}
	if _, err := backup.Create(d, uploads, bk); err == nil {
		t.Fatal("backup into a non-empty directory succeeded")
	}

	target := t.TempDir()
	if _, err := backup.Restore(bk, target); err != nil {
		t.Fatal(err)
	}
	r, err := db.Open(filepath.Join(target, "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var title, gen string
	if err := r.R.QueryRow(`SELECT title FROM work_items WHERE key = 'EXO-1'`).Scan(&title); err != nil || title != "one" {
		t.Fatalf("restored item: %q %v", title, err)
	}
	r.R.QueryRow(`SELECT value FROM meta WHERE key = 'generation'`).Scan(&gen)
	if gen == oldGen || gen == "" {
		t.Fatalf("generation not renewed: %q (was %q)", gen, oldGen)
	}
	if b, err := os.ReadFile(filepath.Join(target, "uploads", "ab", "abc.png")); err != nil || string(b) != "png-bytes" {
		t.Fatalf("restored upload: %q %v", b, err)
	}

	// Restoring over an existing database keeps the old one aside.
	r.Close()
	if _, err := backup.Restore(bk, target); err != nil {
		t.Fatal(err)
	}
	if matches, _ := filepath.Glob(filepath.Join(target, "backups", "pre-restore-*", "atlas.db")); len(matches) != 1 {
		t.Fatalf("previous database not kept: %v", matches)
	}
}

func TestRestoreRejectsTamperedBackup(t *testing.T) {
	d, uploads := seed(t, t.TempDir())
	bk := filepath.Join(t.TempDir(), "bk")
	if _, err := backup.Create(d, uploads, bk); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bk, "uploads", "ab", "abc.png"), []byte("tampered"), 0o640); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := backup.Restore(bk, target); err == nil {
		t.Fatal("tampered upload was restored")
	}
	if _, err := os.Stat(filepath.Join(target, "atlas.db")); err == nil {
		t.Fatal("database was written before verification passed")
	}
}
