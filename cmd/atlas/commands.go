package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kees/atlas/internal/backup"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/orgmigrate"
)

// runCommand handles operator subcommands, run against the configured data directory:
//
//	atlas check paths [--fix]   verify (and optionally repair) the materialized item paths
//	atlas backup <dir>          write a consistent backup (database, uploads, manifest) while the server may run
//	atlas restore <dir>         replace the database with a backup; stop the server first (see run in main.go)
//	atlas migrate-orgs --dry-run|--confirm-report <hash> [--mapping file]   move the data into organizations (ADR-0028)
func runCommand(svc *core.Service, database *db.DB, dataDir string, args []string) error {
	if len(args) == 2 && args[0] == "backup" {
		m, err := backup.Create(database, filepath.Join(dataDir, "uploads"), args[1])
		if err != nil {
			return err
		}
		fmt.Printf("backup written to %s: %d files, schema %s\n", args[1], len(m.Files), m.Schema)
		return nil
	}
	if len(args) >= 2 && args[0] == "check" && args[1] == "paths" {
		fix := len(args) > 2 && args[2] == "--fix"
		rep, err := svc.CheckPaths(context.Background(), fix)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
		if !fix && (len(rep.Mismatches) > 0 || rep.Unreachable > 0) {
			return fmt.Errorf("%d inconsistent paths, %d unreachable items (run with --fix to repair paths)", len(rep.Mismatches), rep.Unreachable)
		}
		return nil
	}
	if len(args) >= 1 && args[0] == "migrate-orgs" {
		return migrateOrgs(database, dataDir, args[1:])
	}
	return fmt.Errorf("unknown command %v; available: check paths [--fix], backup <dir>, restore <dir>, migrate-orgs", args)
}

// migrateOrgs prints the ADR-0028 report (--dry-run) or performs the migration (--confirm-report <hash>): verified
// backup first, then one transaction that fails without changing anything.
func migrateOrgs(database *db.DB, dataDir string, args []string) error {
	fs := flag.NewFlagSet("migrate-orgs", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "print the report and write nothing")
	confirm := fs.String("confirm-report", "", "hash of the reviewed report; performs the migration")
	mappingFile := fs.String("mapping", "", "JSON file with the owner's decisions: {\"admins\": {email: admin|user|none}, \"roles\": {role: member|remove}}")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dry == (*confirm != "") {
		return errors.New("give exactly one of --dry-run or --confirm-report <hash>")
	}
	cfg := orgmigrate.Config{SuperadminEmail: os.Getenv("ATLAS_SUPERADMIN_EMAIL"),
		OrgKey: envOr("ATLAS_DEFAULT_ORG_KEY", "DEFAULT"), OrgName: envOr("ATLAS_DEFAULT_ORG_NAME", "Default organization")}
	var m orgmigrate.Mapping
	if *mappingFile != "" {
		b, err := os.ReadFile(*mappingFile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("mapping: %w", err)
		}
	}
	if *confirm != "" {
		backupDir := filepath.Join(dataDir, "backups", "pre-org-migration-"+time.Now().UTC().Format("20060102T150405Z"))
		rep, err := orgmigrate.Run(database, cfg, m, *confirm, orgmigrate.Options{UploadsDir: filepath.Join(dataDir, "uploads"), BackupDir: backupDir})
		if err != nil {
			return err
		}
		fmt.Printf("migrated: %d change(s) approved, report %s\nkeep the backup in %s until you close the migration; roll back with `atlas restore`\n", len(rep.Transitions), rep.Hash(), backupDir)
		return nil
	}
	rep, err := orgmigrate.Plan(database.R, cfg, m)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		return err
	}
	fmt.Printf("report hash: %s\n", rep.Hash())
	if len(rep.Blocking) > 0 {
		return fmt.Errorf("%d blocking problem(s): the real run would be refused", len(rep.Blocking))
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
