package db

import (
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

func (d *DB) apply(version, body string) error {
	tx, err := d.W.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(body); err != nil {
		return err
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
