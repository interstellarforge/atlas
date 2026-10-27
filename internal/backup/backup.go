// Package backup writes and restores full copies of an Atlas data directory: a consistent database
// snapshot, the uploads it references and a manifest with checksums.
package backup

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kees/atlas/internal/db"
)

const (
	dbFile       = "atlas.db"
	manifestFile = "manifest.json"
)

type File struct {
	Path   string `json:"path"` // relative to the backup directory
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Version    int    `json:"version"`
	CreatedAt  string `json:"created_at"`
	Schema     string `json:"schema"`     // last applied migration
	Generation string `json:"generation"` // meta.generation of the source database
	Files      []File `json:"files"`
}

// Create writes a backup of the live database and its uploads to dest (new or empty directory).
// Protocol: the database is snapshotted first; uploads are immutable and never deleted, so copying
// exactly the files the snapshot references yields a consistent set while the server keeps running.
func Create(d *db.DB, uploadsDir, dest string) (Manifest, error) {
	if entries, err := os.ReadDir(dest); err == nil && len(entries) > 0 {
		return Manifest{}, fmt.Errorf("%s is not empty", dest)
	}
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return Manifest{}, err
	}
	snap := filepath.Join(dest, dbFile)
	if err := d.Snapshot(snap); err != nil {
		return Manifest{}, fmt.Errorf("snapshot: %w", err)
	}
	m := Manifest{Version: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	paths, err := readSnapshot(snap, &m)
	if err != nil {
		return m, err
	}
	for _, rel := range append([]string{dbFile}, paths...) {
		src := filepath.Join(dest, rel)
		if rel != dbFile {
			src = filepath.Join(uploadsDir, rel)
			rel = filepath.Join("uploads", rel)
			if err := copyFile(src, filepath.Join(dest, rel)); err != nil {
				return m, fmt.Errorf("upload %s: %w", rel, err)
			}
			src = filepath.Join(dest, rel)
		}
		f, err := describe(src, rel)
		if err != nil {
			return m, err
		}
		m.Files = append(m.Files, f)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return m, os.WriteFile(filepath.Join(dest, manifestFile), b, 0o640)
}

// readSnapshot fills schema and generation from the snapshot and returns its attachment paths.
func readSnapshot(path string, m *Manifest) ([]string, error) {
	s, err := db.Open(path, db.Options{})
	if err != nil {
		return nil, err
	}
	defer s.Close()
	if err := s.QuickCheck(); err != nil {
		return nil, err
	}
	if err := s.R.QueryRow(`SELECT COALESCE(MAX(version), '') FROM schema_migrations`).Scan(&m.Schema); err != nil {
		return nil, err
	}
	if err := s.R.QueryRow(`SELECT value FROM meta WHERE key = 'generation'`).Scan(&m.Generation); err != nil {
		return nil, err
	}
	rows, err := s.R.Query(`SELECT path FROM attachments ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// Restore replaces the database in dataDir with the backup and copies its uploads. The server must be
// stopped. The previous database is kept under backups/pre-restore-<time>, and the restored database gets
// a new generation so connected clients reload everything. The backup is verified against its manifest first.
func Restore(src, dataDir string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(src, manifestFile))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("manifest: %w", err)
	}
	if err := verify(src, m); err != nil {
		return m, err
	}

	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return m, err
	}
	live := filepath.Join(dataDir, dbFile)
	staged := live + ".restore"
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(staged + suffix)
	}
	if err := copyFile(filepath.Join(src, dbFile), staged); err != nil {
		return m, err
	}
	// The staged copy is validated and re-generationed before it can touch the live database.
	if err := prepare(staged, &m); err != nil {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			os.Remove(staged + suffix)
		}
		return m, err
	}
	for _, f := range m.Files {
		if f.Path == dbFile {
			continue
		}
		if err := copyFile(filepath.Join(src, f.Path), filepath.Join(dataDir, f.Path)); err != nil {
			return m, err
		}
	}
	if _, err := os.Stat(live); err == nil {
		old := filepath.Join(dataDir, "backups", "pre-restore-"+time.Now().UTC().Format("20060102T150405Z"))
		if err := os.MkdirAll(old, 0o750); err != nil {
			return m, err
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Rename(live+suffix, filepath.Join(old, dbFile+suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return m, err
			}
		}
	}
	return m, os.Rename(staged, live)
}

// verify requires exactly one atlas.db entry, only local regular files under uploads/, and matching checksums.
func verify(src string, m Manifest) error {
	dbs := 0
	for _, f := range m.Files {
		switch {
		case f.Path == dbFile:
			dbs++
		case !strings.HasPrefix(f.Path, "uploads/") || !filepath.IsLocal(f.Path):
			return fmt.Errorf("manifest path %q is not allowed", f.Path)
		}
		if err := regularPath(src, f.Path); err != nil {
			return err
		}
		got, err := describe(filepath.Join(src, f.Path), f.Path)
		if err != nil {
			return err
		}
		if got.SHA256 != f.SHA256 || got.Size != f.Size {
			return fmt.Errorf("%s does not match the manifest", f.Path)
		}
	}
	if dbs != 1 {
		return fmt.Errorf("manifest must list %s exactly once", dbFile)
	}
	return nil
}

// regularPath rejects symlinks anywhere along rel below root.
func regularPath(root, rel string) error {
	cur := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", rel)
		}
	}
	return nil
}

// prepare integrity-checks the staged database and gives it a new generation.
func prepare(path string, m *Manifest) error {
	d, err := db.Open(path, db.Options{})
	if err != nil {
		return err
	}
	if err := d.QuickCheck(); err != nil {
		d.Close()
		return err
	}
	var gen [8]byte
	if _, err := rand.Read(gen[:]); err != nil {
		d.Close()
		return err
	}
	m.Generation = hex.EncodeToString(gen[:])
	if _, err := d.W.Exec(`UPDATE meta SET value = ? WHERE key = 'generation'`, m.Generation); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}

func describe(path, rel string) (File, error) {
	f, err := os.Open(path)
	if err != nil {
		return File{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return File{Path: filepath.ToSlash(rel), Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
