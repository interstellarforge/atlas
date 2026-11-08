package core_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ADR-0029: authorization never reads the legacy admin flag or the legacy role tables. Only the data migration, its
// report and the rehearsal seed may mention them; everything else decides through authz.go.
func TestLegacyAuthorizationIsNotRead(t *testing.T) {
	legacy := regexp.MustCompile(`(?i)is_admin|role_actions|\b(FROM|JOIN|INTO|UPDATE)\s+roles\b|legacy_role`)
	allowed := []string{"internal/orgmigrate/", "benchmarks/seed/"}
	root := "../.."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "dist" || d.Name() == "data") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, a := range allowed {
			if strings.HasPrefix(rel, a) {
				return nil
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if m := legacy.FindString(string(b)); m != "" {
			t.Errorf("%s mentions %q: authorization must not read legacy admin or role data", rel, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
