package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	MaxReadme  = 1 << 20
	MaxImage   = 5 << 20
	fetchLimit = 10 * time.Second
)

// publicAddr reports whether an address may be fetched from: not loopback, private, link-local, multicast,
// unspecified or carrier-grade NAT. IPv4-mapped IPv6 addresses are unwrapped first.
func publicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() || a.IsInterfaceLocalMulticast() {
		return false
	}
	if a.Is4() {
		b := a.As4()
		if b[0] == 100 && b[1]&0xc0 == 64 { // 100.64.0.0/10
			return false
		}
		for _, n := range reservedV4 {
			if n.Contains(a) {
				return false
			}
		}
	}
	if a.Is6() && netip.MustParsePrefix("2001:db8::/32").Contains(a) { // documentation
		return false
	}
	return true
}

// reservedV4 are special-purpose IPv4 ranges (RFC 6890 and friends) that are not routable on the public internet.
var reservedV4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
}

// Fetcher downloads repository files over https. The check happens when the connection is made, on the address
// that is actually dialled, so DNS tricks (rebinding, redirects to internal names) cannot reach private networks.
type Fetcher struct {
	// AllowPrivate lets self-hosted servers on private addresses through (ATLAS_REPO_ALLOW_PRIVATE).
	AllowPrivate bool
	client       *http.Client
}

func NewFetcher(allowPrivate bool) *Fetcher {
	f := &Fetcher{AllowPrivate: allowPrivate}
	d := &net.Dialer{Timeout: 5 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return err
		}
		if !f.AllowPrivate && !publicAddr(ip) {
			return errors.New("address is not a public internet address")
		}
		return nil
	}}
	f.client = &http.Client{
		Timeout:   fetchLimit,
		Transport: &http.Transport{DialContext: d.DialContext, Proxy: nil, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second, DisableKeepAlives: true},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" || req.URL.Hostname() != via[0].URL.Hostname() {
				return errors.New("redirect to another host refused")
			}
			return nil
		},
	}
	return f
}

// ErrNotFound means the file does not exist (HTTP 404/403 from the host).
var ErrNotFound = errors.New("file not found in the repository")

// Get downloads one https URL: no credentials, no cookies, a size cap, and the content type returned.
func (f *Fetcher) Get(ctx context.Context, rawURL string, limit int64) (body []byte, contentType string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, "", errors.New("only plain https URLs can be fetched")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Atlas")
	res, err := f.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch %s: %w", u.Host, errors.Unwrap(err))
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusUnauthorized:
		return nil, "", ErrNotFound
	case res.StatusCode != http.StatusOK:
		return nil, "", fmt.Errorf("fetch %s: HTTP %d", u.Host, res.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, "", fmt.Errorf("fetch %s: %w", u.Host, err)
	}
	if int64(len(body)) > limit {
		return nil, "", fmt.Errorf("file is larger than %d KiB", limit>>10)
	}
	ct := strings.TrimSpace(strings.SplitN(res.Header.Get("Content-Type"), ";", 2)[0])
	return body, strings.ToLower(ct), nil
}

// Post sends one webhook delivery with the same address checks as Get and returns an error unless the answer is
// 2xx. https is required, except on private networks when AllowPrivate is set. The answer body is not read.
func (f *Fetcher) Post(ctx context.Context, rawURL string, header map[string]string, body []byte) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && !(u.Scheme == "http" && f.AllowPrivate)) {
		return errors.New("webhook URL must be a plain https URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Atlas")
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", u.Host, errors.Unwrap(err))
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("post %s: HTTP %d", u.Host, res.StatusCode)
	}
	return nil
}

// Readme loads README.md (or readme.md) from the repository root.
func (f *Fetcher) Readme(ctx context.Context, r Repo) (string, error) {
	var lastErr error = ErrNotFound
	for _, name := range []string{"README.md", "readme.md"} {
		body, _, err := f.Get(ctx, r.RawURL(name), MaxReadme)
		if err == nil {
			return string(body), nil
		}
		if !errors.Is(err, ErrNotFound) {
			return "", err
		}
		lastErr = err
	}
	return "", lastErr
}
