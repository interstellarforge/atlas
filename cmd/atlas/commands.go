package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kees/atlas/internal/backup"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/db"
)

// runCommand handles operator subcommands, run against the configured data directory:
//
//	atlas check paths [--fix]   verify (and optionally repair) the materialized item paths
//	atlas backup <dir>          write a consistent backup (database, uploads, manifest) while the server may run
//	atlas restore <dir>         replace the database with a backup; stop the server first (see run in main.go)
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
	return fmt.Errorf("unknown command %v; available: check paths [--fix], backup <dir>, restore <dir>", args)
}
