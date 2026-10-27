package repo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	good := map[string]string{
		"https://github.com/acme/atlas":        "https://github.com/acme/atlas",
		"https://github.com/acme/atlas.git":    "https://github.com/acme/atlas",
		"https://GitLab.com/group/sub/proj/":   "https://gitlab.com/group/sub/proj",
		"git@github.com:acme/atlas.git":        "https://github.com/acme/atlas",
		"ssh://git@git.example.com/acme/atlas": "https://git.example.com/acme/atlas",
		"https://codeberg.org/acme/atlas":      "https://codeberg.org/acme/atlas",
		"https://gitea.example.com:3000/a/b":   "https://gitea.example.com:3000/a/b",
	}
	for in, want := range good {
		r, err := Parse(in, "")
		if err != nil || r.URL != want || r.Branch != "main" {
			t.Errorf("Parse(%q) = %+v, %v; want %s", in, r, err, want)
		}
	}
	for _, bad := range []string{
		"", "github.com/acme/atlas", "http://github.com/acme/atlas", "https://user:pw@github.com/acme/atlas",
		"https://token@github.com/acme/atlas", "ssh://bob@host/acme/atlas", "bob@host:acme/atlas", "https://github.com/acme",
		"https://github.com/acme/../x", "https://github.com/acme/atlas?x=1", "file:///etc/passwd", "https://github.com/a b/c",
		"javascript:alert(1)",
	} {
		if r, err := Parse(bad, ""); err == nil {
			t.Errorf("Parse(%q) accepted: %+v", bad, r)
		}
	}
	for _, b := range []string{"a b", "-x", "x..y", "x~", "/x"} {
		if _, err := Parse("https://github.com/a/b", b); err == nil {
			t.Errorf("branch %q accepted", b)
		}
	}
}

func TestProvidersAndURLs(t *testing.T) {
	cases := []struct{ in, provider, raw, blob string }{
		{"https://github.com/acme/atlas", "github", "https://raw.githubusercontent.com/acme/atlas/main/docs/a.png", "https://github.com/acme/atlas/blob/main/docs/a.png"},
		{"https://gitlab.com/g/s/p", "gitlab", "https://gitlab.com/g/s/p/-/raw/main/docs/a.png", "https://gitlab.com/g/s/p/-/blob/main/docs/a.png"},
		{"https://codeberg.org/a/b", "gitea", "https://codeberg.org/a/b/raw/branch/main/docs/a.png", "https://codeberg.org/a/b/src/branch/main/docs/a.png"},
		{"https://bitbucket.org/a/b", "bitbucket", "https://bitbucket.org/a/b/raw/main/docs/a.png", "https://bitbucket.org/a/b/src/main/docs/a.png"},
		{"https://git.example.com/a/b", "other", "https://git.example.com/a/b/raw/branch/main/docs/a.png", "https://git.example.com/a/b/src/branch/main/docs/a.png"},
	}
	for _, c := range cases {
		r, err := Parse(c.in, "")
		if err != nil || r.Provider != c.provider || r.RawURL("docs/a.png") != c.raw || r.BlobURL("docs/a.png") != c.blob {
			t.Errorf("%s: %+v %v\n raw %s\n blob %s", c.in, r, err, r.RawURL("docs/a.png"), r.BlobURL("docs/a.png"))
		}
	}
}

func rewriter(t *testing.T) Rewriter {
	r, err := Parse("https://github.com/acme/atlas", "main")
	if err != nil {
		t.Fatal(err)
	}
	return Rewriter{Repo: r, Asset: func(abs string) string { return "/asset?u=" + url.QueryEscape(abs) }}
}

func TestRewriteRelativePaths(t *testing.T) {
	rw := rewriter(t)
	const raw = "https://raw.githubusercontent.com/acme/atlas/main/"
	got := rw.Rewrite("![logo](docs/logo.png) ![up](../../etc/x.png) ![abs](/img/a.svg?raw=true) [guide](./docs/guide.md#install) [top](#top) [root](/)")
	for _, want := range []string{
		"![logo](/asset?u=" + url.QueryEscape(raw+"docs/logo.png") + ")",
		"![up](/asset?u=" + url.QueryEscape(raw+"etc/x.png") + ")", // never leaves the repository root
		"![abs](/asset?u=" + url.QueryEscape(raw+"img/a.svg") + ")",
		"[guide](https://github.com/acme/atlas/blob/main/docs/guide.md#install)",
		"[top](#top)",
		"[root](https://github.com/acme/atlas)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestRewriteImagesAndBadges(t *testing.T) {
	rw := rewriter(t)
	got := rw.Rewrite("[![build](https://img.shields.io/badge/a-b-green.svg)](https://ci.example.com/run) ![t](http://evil.example.com/x.png) ![d](data:image/png;base64,AAAA) [x](javascript:alert(1))")
	if !strings.Contains(got, "[![build](/asset?u=https%3A%2F%2Fimg.shields.io") || !strings.Contains(got, "](https://ci.example.com/run)") {
		t.Errorf("badge link: %s", got)
	}
	if strings.Contains(got, "![t]") || !strings.Contains(got, "[t](https://evil.example.com/x.png)") {
		t.Errorf("image from a host that is not allowed must become a link: %s", got)
	}
	if strings.Contains(got, "data:") || strings.Contains(got, "javascript:") {
		t.Errorf("dangerous scheme survived: %s", got)
	}
}

func TestRewriteHTMLAndCode(t *testing.T) {
	rw := rewriter(t)
	in := "<h1 align=\"center\">Atlas</h1>\n<p align=\"center\"><a href=\"docs/x.md\"><img src=\"assets/hero.png\" alt=\"Hero\" width=\"300\"></a></p>\n<!-- hidden -->\n<br>\n\n```md\n![keep](docs/logo.png)\n<img src=\"x.png\">\n```\n"
	got := rw.Rewrite(in)
	for _, want := range []string{"# Atlas", "[![Hero](/asset?u=", "](https://github.com/acme/atlas/blob/main/docs/x.md)", "```md\n![keep](docs/logo.png)\n<img src=\"x.png\">\n```"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "hidden") || strings.Contains(got, "<p") || strings.Contains(got, "<h1") {
		t.Errorf("html left over:\n%s", got)
	}
}

func TestImageType(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if ImageType("image/png", png) != "image/png" || ImageType("", png) != "image/png" {
		t.Error("png")
	}
	if ImageType("image/png", []byte("<html>not a png</html>")) != "" {
		t.Error("declared png that is html")
	}
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if ImageType("text/plain", svg) != "image/svg+xml" {
		t.Error("svg by content")
	}
	if ImageType("text/html", []byte("<html><script>x</script></html>")) != "" {
		t.Error("html served as image")
	}
}

func TestPublicAddr(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "224.0.0.1"} {
		if publicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should not be fetchable", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "140.82.121.4", "2606:4700::1111"} {
		if !publicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should be fetchable", s)
		}
	}
}

func TestFetcherRefusesPrivateAddressesAndOddURLs(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("secret")) }))
	defer srv.Close()
	f := NewFetcher(false)
	if _, _, err := f.Get(context.Background(), srv.URL+"/README.md", MaxReadme); err == nil || !strings.Contains(err.Error(), "public") {
		t.Errorf("loopback server was fetched: %v", err)
	}
	for _, u := range []string{"http://example.com/x", "file:///etc/passwd", "https://user:pw@example.com/x", "ftp://example.com/x"} {
		if _, _, err := f.Get(context.Background(), u, MaxReadme); err == nil {
			t.Errorf("%s accepted", u)
		}
	}
	// Redirects may not leave the host or downgrade to http.
	via := []*http.Request{{URL: &url.URL{Scheme: "https", Host: "raw.githubusercontent.com"}}}
	if f.client.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "https", Host: "169.254.169.254"}}, via) == nil {
		t.Error("redirect to another host allowed")
	}
	if f.client.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "http", Host: "raw.githubusercontent.com"}}, via) == nil {
		t.Error("redirect to http allowed")
	}
	if f.client.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "https", Host: "raw.githubusercontent.com"}}, via) != nil {
		t.Error("same-host https redirect refused")
	}
}

func TestRewriteIndentedHTMLImageIsNotCode(t *testing.T) {
	rw := rewriter(t)
	got := rw.Rewrite("<p align=\"center\">\n\t\t<img src=\"assets/banner.png\" alt=\"Banner\">\n</p>\n")
	if strings.Contains(got, "\t![") || !strings.Contains(got, "\n![Banner](/asset?u=") && !strings.HasPrefix(got, "![Banner](/asset?u=") {
		t.Errorf("image would render as an indented code block: %q", got)
	}
}
