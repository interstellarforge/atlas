package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/kees/atlas/internal/core"
)

// runCommand handles operator subcommands, run against the configured data directory:
//
//	atlas check paths [--fix]   verify (and optionally repair) the materialized item paths
func runCommand(svc *core.Service, args []string) error {
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
	return fmt.Errorf("unknown command %v; available: check paths [--fix]", args)
}
