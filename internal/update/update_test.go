package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.10.0", "1.9.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.1", false},
		{"v1.0.0", "dev", false},
		{"nightly", "v1.0.0", false},
	} {
		if got := newer(c.latest, c.current); got != c.want {
			t.Errorf("newer(%q,%q)=%v want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestCheckCachesAndReports(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0","html_url":"https://github.com/x/y/releases/v2.0.0"}`))
	}))
	defer srv.Close()
	c := &Checker{Current: "v1.0.0", URL: srv.URL, Client: srv.Client()}
	for range 2 {
		if s := c.Check(context.Background()); !s.Available || s.Latest != "v2.0.0" {
			t.Fatalf("status %+v", s)
		}
	}
	if hits != 1 {
		t.Fatalf("hits=%d, want cached", hits)
	}
}
