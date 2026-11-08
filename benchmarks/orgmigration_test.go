package benchmarks_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kees/atlas/benchmarks/seed"
	"github.com/kees/atlas/internal/backup"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/orgmigrate"
	"github.com/kees/atlas/migrations"
)

// Run with: ATLAS_BENCH=1 go test ./benchmarks -run OrgMigration -v
// Rehearsal of ADR-0028 on the 10.000-item seed with legacy accounts and roles: report, verified backup, migration,
// then a restore of the pre-migration backup (rollback) and of a backup taken after the migration.
func TestOrgMigrationRehearsal(t *testing.T) {
	if testing.Short() || !benchEnabled() {
		t.Skip("set ATLAS_BENCH=1 to run")
	}
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "atlas.db"), db.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(migrations.FS, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(d.W, seed.Options{Items: 10000, Projects: 3, Seed: 1, LegacyAccess: true}); err != nil {
		t.Fatal(err)
	}
	cfg := orgmigrate.Config{SuperadminEmail: "super@seed.test", OrgKey: "MAIN", OrgName: "Main"}
	m := orgmigrate.Mapping{Roles: map[string]string{"viewer": "member", "wide": "remove", "narrow": "member"}}

	start := time.Now()
	rep, err := orgmigrate.Plan(d.R, cfg, m)
	if err != nil || len(rep.Blocking) > 0 {
		t.Fatalf("plan: %v %v", err, rep.Blocking)
	}
	t.Logf("report in %v: %d transitions, %d warnings", time.Since(start), len(rep.Transitions), len(rep.Warnings))

	pre := filepath.Join(t.TempDir(), "pre-migration")
	start = time.Now()
	if _, err := orgmigrate.Run(d, cfg, m, rep.Hash(), orgmigrate.Options{UploadsDir: filepath.Join(dir, "uploads"), BackupDir: pre}); err != nil {
		t.Fatal(err)
	}
	t.Logf("verified backup + migration in %v", time.Since(start))

	check := func(name, backupDir string, wantOrgs int, wantLegacyRoles bool) {
		t.Helper()
		data := t.TempDir()
		start := time.Now()
		if _, err := backup.Restore(backupDir, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r, err := db.Open(filepath.Join(data, "atlas.db"), db.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		var orgs, legacy, items int
		r.R.QueryRow(`SELECT COUNT(*) FROM organizations`).Scan(&orgs)
		r.R.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'legacy_roles'`).Scan(&legacy)
		r.R.QueryRow(`SELECT COUNT(*) FROM work_items`).Scan(&items)
		if orgs != wantOrgs || (legacy == 1) != wantLegacyRoles || items != rep.Counts["work_items"] {
			t.Fatalf("%s: organizations %d, legacy roles %d, items %d", name, orgs, legacy, items)
		}
		t.Logf("%s restored and checked in %v", name, time.Since(start))
	}
	check("rollback (pre-migration backup)", pre, 0, false)
	post := filepath.Join(t.TempDir(), "post-migration")
	if _, err := backup.Create(d, filepath.Join(dir, "uploads"), post); err != nil {
		t.Fatal(err)
	}
	check("restore after the migration", post, 1, true)
}
