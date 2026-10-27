// atlas-seed fills a database with a reproducible dataset for benchmarks.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kees/atlas/benchmarks/seed"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/migrations"
)

func main() {
	dir := flag.String("data-dir", "./data", "data directory (atlas.db is created inside)")
	items := flag.Int("items", 10000, "number of work items")
	projects := flag.Int("projects", 3, "number of projects")
	seedVal := flag.Uint64("seed", 1, "random seed")
	flag.Parse()

	if err := run(*dir, seed.Options{Items: *items, Projects: *projects, Seed: *seedVal}); err != nil {
		fmt.Fprintln(os.Stderr, "atlas-seed:", err)
		os.Exit(1)
	}
}

func run(dir string, o seed.Options) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	d, err := db.Open(filepath.Join(dir, "atlas.db"), db.Options{})
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Migrate(migrations.FS, filepath.Join(dir, "backups")); err != nil {
		return err
	}
	if err := seed.Run(d.W, o); err != nil {
		return err
	}
	fmt.Printf("seeded %d items in %d projects at %s\n", o.Items, o.Projects, d.Path)
	return nil
}
