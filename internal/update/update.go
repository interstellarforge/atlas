// Package update tells administrators when a newer Atlas release exists on GitHub.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	LatestURL = "https://api.github.com/repos/interstellarforge/atlas/releases/latest"
	okTTL     = 6 * time.Hour
	failTTL   = 30 * time.Minute
)

type Status struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	URL       string `json:"url,omitempty"`
	Available bool   `json:"available"`
}

// Checker caches the latest release so GitHub is asked at most once per okTTL (failTTL after an error).
type Checker struct {
	Current string // build version; "dev" or empty never reports an update
	URL     string
	Client  *http.Client

	mu      sync.Mutex
	expires time.Time
	status  Status
}

func (c *Checker) Check(ctx context.Context) Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expires) {
		return c.status
	}
	c.status = Status{Current: c.Current}
	ttl := okTTL
	if latest, url, err := c.fetch(ctx); err != nil {
		ttl = failTTL
	} else {
		c.status.Latest, c.status.URL = latest, url
		c.status.Available = newer(latest, c.Current)
	}
	c.expires = time.Now().Add(ttl)
	return c.status
}

func (c *Checker) fetch(ctx context.Context) (tag, url string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "atlas-update-check")
	res, err := c.Client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github releases: %s", res.Status)
	}
	var rel struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, res.Body, 1<<20)).Decode(&rel); err != nil {
		return "", "", err
	}
	return rel.Tag, rel.URL, nil
}

// newer reports whether latest is a higher dotted-numeric version than current ("v1.2.3" or "1.2.3").
// An unparsable current (e.g. "dev") is never outdated.
func newer(latest, current string) bool {
	l, ok1 := parse(latest)
	c, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parse(v string) (p [3]int, ok bool) {
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-") // drop pre-release suffix
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return p, false
	}
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil {
			return p, false
		}
		p[i] = n
	}
	return p, true
}
