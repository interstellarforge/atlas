package logbuf

import (
	"io"
	"log/slog"
	"testing"
)

func TestBufferKeepsNewestAndFilters(t *testing.T) {
	b := New()
	l := slog.New(b.Handler(slog.NewJSONHandler(io.Discard, nil))).With("svc", "atlas")
	for i := 0; i < Size+3; i++ {
		l.Info("tick", "n", i)
	}
	l.Warn("readme fetch failed", "project", "EXO")

	all := b.List(Filter{Limit: 500})
	if all[0].Msg != "readme fetch failed" || all[0].Attrs["svc"] != "atlas" {
		t.Fatalf("newest first with handler attrs: %+v", all[0])
	}
	if got := b.List(Filter{MinLevel: slog.LevelWarn}); len(got) != 1 {
		t.Fatalf("level filter: %d", len(got))
	}
	if got := b.List(Filter{Query: "exo"}); len(got) != 1 || got[0].Attrs["project"] != "EXO" {
		t.Fatalf("query filter: %+v", got)
	}
	if len(all) != 500 {
		t.Fatalf("limit: %d", len(all))
	}
}
