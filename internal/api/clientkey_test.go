package api

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientKeyTrustsForwardedForOnlyFromProxies(t *testing.T) {
	s := &Server{proxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	req := func(remote, xff string) string {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return s.clientKey(r)
	}
	for _, c := range []struct{ remote, xff, want string }{
		{"10.0.0.5:1234", "203.0.113.7", "203.0.113.7"},
		{"10.0.0.5:1234", "6.6.6.6, 203.0.113.7, 10.0.0.9", "203.0.113.7"}, // client-supplied hops left of the real one are ignored
		{"198.51.100.1:1234", "6.6.6.6", "198.51.100.1"},                   // not a proxy: header ignored
		{"10.0.0.5:1234", "garbage", "10.0.0.5"},
		{"10.0.0.5:1234", "", "10.0.0.5"},
	} {
		if got := req(c.remote, c.xff); got != c.want {
			t.Errorf("%s + %q: got %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
}
