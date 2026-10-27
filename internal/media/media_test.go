package media_test

import (
	"os"
	"testing"

	"github.com/kees/atlas/internal/media"
)

// TestThumbnailOfLargeImage is a manual memory check: run it under `/usr/bin/time -l` (see benchmarks/RESULTS.md)
// with ATLAS_BENCH_IMAGE pointing at a large image. It is skipped otherwise.
func TestThumbnailOfLargeImage(t *testing.T) {
	path := os.Getenv("ATLAS_BENCH_IMAGE")
	if path == "" {
		t.Skip("set ATLAS_BENCH_IMAGE to measure")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := media.NewStore(t.TempDir(), media.Limits{MaxBytes: 10 << 20, MaxPixels: 20_000_000})
	saved, err := s.Save(f, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Thumb(saved.Path); err != nil {
		t.Fatal(err)
	}
}
