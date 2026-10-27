package db_test

import (
	"path/filepath"
	"testing"

	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/migrations"
)

func open(t *testing.T, sync string) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "atlas.db"), db.Options{Synchronous: sync})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestProfileAppliesToEveryConnection(t *testing.T) {
	d := open(t, "")
	// Hold several read connections at once so the pool opens more than one.
	conns := make([]interface{ Close() error }, 0, 4)
	for i := 0; i < 4; i++ {
		c, err := d.R.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		var fk, bt, sync int
		var jm string
		if err := c.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			t.Fatal(err)
		}
		c.QueryRowContext(t.Context(), `PRAGMA busy_timeout`).Scan(&bt)
		c.QueryRowContext(t.Context(), `PRAGMA synchronous`).Scan(&sync)
		c.QueryRowContext(t.Context(), `PRAGMA journal_mode`).Scan(&jm)
		if fk != 1 || bt != 5000 || sync != 2 || jm != "wal" {
			t.Fatalf("conn %d: foreign_keys=%d busy_timeout=%d synchronous=%d journal_mode=%s", i, fk, bt, sync, jm)
		}
	}
	for _, c := range conns {
		c.Close()
	}
}

func TestReadPoolIsReadOnly(t *testing.T) {
	d := open(t, "")
	if _, err := d.R.Exec(`CREATE TABLE x (a)`); err == nil {
		t.Fatal("read pool accepted a write")
	}
}

func TestMigrateIsIdempotentAndBacksUp(t *testing.T) {
	d := open(t, "")
	backups := filepath.Join(t.TempDir(), "backups")
	if err := d.Migrate(migrations.FS, backups); err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(migrations.FS, backups); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if err := d.QuickCheck(); err != nil {
		t.Fatal(err)
	}
}
