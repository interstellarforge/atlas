package orgmigrate

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/kees/atlas/internal/db"
)

// legacyColumns are the admin flags of the pre-organization model. Only this package knows them: Plan reads them
// as input, Finalize drops them once the database no longer needs them.
var legacyColumns = []string{"users", "invites"}

// projectOrganizationGuards keep projects.organization_id NOT NULL without rebuilding the table (a rebuild would
// need foreign keys switched off, which a transaction cannot do, and every child table points at projects).
var projectOrganizationGuards = []string{
	`CREATE TRIGGER IF NOT EXISTS projects_need_organization_insert BEFORE INSERT ON projects
	 WHEN NEW.organization_id IS NULL
	 BEGIN SELECT RAISE(ABORT, 'a project belongs to an organization'); END`,
	`CREATE TRIGGER IF NOT EXISTS projects_need_organization_update BEFORE UPDATE OF organization_id ON projects
	 WHEN NEW.organization_id IS NULL
	 BEGIN SELECT RAISE(ABORT, 'a project belongs to an organization'); END`,
}

// finalizeTx completes the organization schema: projects always have an organization (ATL-425) and the legacy
// is_admin columns are gone (ATL-474). It is idempotent. Run it only on a database that is fresh or already migrated.
func finalizeTx(tx *sql.Tx) error {
	// Never on a database that still needs the data migration: it would drop the flags the migration reads.
	var noOrg, users, supers int
	if err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM projects WHERE organization_id IS NULL),
		(SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM users WHERE is_superadmin = 1)`).Scan(&noOrg, &users, &supers); err != nil {
		return err
	}
	if noOrg > 0 || (users > 0 && supers == 0) {
		return errors.New("the database still has to be migrated (atlas migrate-orgs); legacy columns are kept")
	}
	for _, q := range projectOrganizationGuards {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	for _, table := range legacyColumns {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'is_admin'`, table).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`ALTER TABLE %s DROP COLUMN is_admin`, table)); err != nil {
			return fmt.Errorf("drop %s.is_admin: %w", table, err)
		}
	}
	return nil
}

// Finalize runs finalizeTx in its own write transaction. The server calls it at startup, after it has checked that the
// database is fresh or migrated; an unmigrated database never reaches it.
func Finalize(d *db.DB) error {
	tx, err := d.W.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := finalizeTx(tx); err != nil {
		return err
	}
	return tx.Commit()
}
