package db

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Migrate applies pending forward-only migrations, each in its own transaction.
// Before applying anything to an existing database it writes a backup copy.
func (d *DB) Migrate(migrations fs.FS, backupDir string) error {
	if _, err := d.W.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		return err
	}

	files, err := fs.Glob(migrations, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	applied := map[string]bool{}
	rows, err := d.W.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}

	var pending []string
	for _, f := range files {
		if !applied[f] {
			pending = append(pending, f)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if len(applied) > 0 {
		if err := d.backup(backupDir); err != nil {
			return fmt.Errorf("pre-migration backup: %w", err)
		}
	}

	for _, f := range pending {
		body, err := fs.ReadFile(migrations, f)
		if err != nil {
			return err
		}
		if err := d.apply(f, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", f, err)
		}
		slog.Info("migration applied", "version", f)
	}
	return nil
}

// foreignKeysOff marks a migration that rebuilds a table other tables point to: foreign keys are off while it
// runs (SQLite cannot switch them inside a transaction) and checked before it commits.
const foreignKeysOff = "-- atlas:foreign-keys-off"

func (d *DB) apply(version, body string) error {
	ctx := context.Background()
	conn, err := d.W.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	rebuild := strings.HasPrefix(body, foreignKeysOff)
	if rebuild {
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return err
		}
		defer conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(body); err != nil {
		return err
	}
	if rebuild {
		rows, err := tx.Query(`PRAGMA foreign_key_check`)
		if err != nil {
			return err
		}
		violated := rows.Next()
		rows.Close()
		if violated {
			return fmt.Errorf("foreign key check failed after rebuild")
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) backup(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	name := fmt.Sprintf("pre-migrate-%s.db", time.Now().UTC().Format("20060102T150405Z"))
	return d.Snapshot(filepath.Join(dir, name))
}

// Snapshot writes a consistent copy of the live database to dest (which must not exist) without stopping writers.
func (d *DB) Snapshot(dest string) error {
	// VACUUM INTO gives a consistent copy; the path is ours, but escape quotes anyway.
	_, err := d.W.Exec(`VACUUM INTO '` + strings.ReplaceAll(dest, `'`, `''`) + `'`)
	return err
}

// QuickCheck runs a lightweight integrity check; full checks are manual.
func (d *DB) QuickCheck() error {
	var res string
	if err := d.W.QueryRow(`PRAGMA quick_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("quick_check: %s", res)
	}
	return nil
}
