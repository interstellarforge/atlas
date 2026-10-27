// Package repo understands git repository links: URL parsing, provider detection, the raw and web URLs of a
// README, and an SSRF-safe HTTP fetcher (ADR-0013). Atlas only stores a link; it never clones.
package repo

import (
	"errors"
	"net"
	"net/url"
	"path"
	"strings"
)

// Repo is a validated repository link. URL is always the https web address without credentials or ".git".
type Repo struct {
	URL      string `json:"url"`
	Host     string `json:"host"`
	Path     string `json:"path"` // owner/name (GitLab: may have subgroups)
	Provider string `json:"provider"`
	Branch   string `json:"branch"`
}

const DefaultBranch = "main"

// Parse accepts https://host/path[.git], ssh://git@host/path[.git] and git@host:path[.git]. Credentials in the
// URL are refused (the ssh user "git" is dropped), and the result is the https web address.
func Parse(raw, branch string) (Repo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 300 || strings.ContainsAny(raw, " \t\r\n\\") {
		return Repo{}, errors.New("repository URL is empty, too long or contains spaces")
	}
	var host, p string
	switch {
	case strings.HasPrefix(raw, "https://"), strings.HasPrefix(raw, "ssh://"):
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return Repo{}, errors.New("repository URL is not valid")
		}
		if u.Scheme == "https" && u.User != nil {
			return Repo{}, errors.New("repository URL must not contain credentials")
		}
		if u.Scheme == "ssh" && u.User != nil {
			if _, hasPassword := u.User.Password(); hasPassword || u.User.Username() != "git" {
				return Repo{}, errors.New("repository URL must not contain credentials")
			}
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return Repo{}, errors.New("repository URL must not have a query or fragment")
		}
		host, p = strings.ToLower(u.Hostname()), u.Path
		if u.Scheme == "https" {
			host = strings.ToLower(u.Host) // keep a non-standard port of a self-hosted server; ssh ports are not https ports
		}
	case strings.Contains(raw, "@") && strings.Contains(raw, ":") && !strings.Contains(raw, "://"):
		user, rest, _ := strings.Cut(raw, "@") // git@host:owner/name.git
		if user != "git" {
			return Repo{}, errors.New("repository URL must not contain credentials")
		}
		h, pp, ok := strings.Cut(rest, ":")
		if !ok {
			return Repo{}, errors.New("repository URL is not valid")
		}
		host, p = strings.ToLower(h), pp
	default:
		return Repo{}, errors.New("repository URL must start with https:// or ssh://, or look like git@host:owner/name")
	}
	if net.ParseIP(host) != nil && !strings.Contains(host, ".") && !strings.Contains(host, ":") {
		return Repo{}, errors.New("repository host is not valid")
	}
	p = strings.Trim(strings.TrimSuffix(strings.Trim(p, "/"), ".git"), "/")
	if p == "" || strings.Contains(p, "..") || strings.Count(p, "/") < 1 {
		return Repo{}, errors.New("repository URL needs an owner and a name")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || strings.ContainsAny(seg, "?#%") {
			return Repo{}, errors.New("repository path is not valid")
		}
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = DefaultBranch
	}
	if len(branch) > 100 || strings.ContainsAny(branch, " ~^:?*[\\") || strings.Contains(branch, "..") || strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") {
		return Repo{}, errors.New("branch name is not valid")
	}
	r := Repo{Host: host, Path: p, Branch: branch}
	r.URL = "https://" + host + "/" + p
	r.Provider = detectProvider(host)
	return r, nil
}

func detectProvider(host string) string {
	switch {
	case host == "github.com":
		return "github"
	case host == "gitlab.com" || strings.HasPrefix(host, "gitlab."):
		return "gitlab"
	case host == "codeberg.org" || strings.Contains(host, "gitea") || strings.Contains(host, "forgejo"):
		return "gitea"
	case host == "bitbucket.org":
		return "bitbucket"
	}
	return "other"
}

// RawURL is where the raw content of a file in the repository root lives.
func (r Repo) RawURL(file string) string {
	switch r.Provider {
	case "github":
		return "https://raw.githubusercontent.com/" + r.Path + "/" + r.Branch + "/" + file
	case "gitlab":
		return r.URL + "/-/raw/" + r.Branch + "/" + file
	case "bitbucket":
		return r.URL + "/raw/" + r.Branch + "/" + file
	}
	return r.URL + "/raw/branch/" + r.Branch + "/" + file // Gitea, Forgejo and look-alikes
}

// BlobURL is the web page of a file or folder.
func (r Repo) BlobURL(file string) string {
	switch r.Provider {
	case "github":
		return r.URL + "/blob/" + r.Branch + "/" + file
	case "gitlab":
		return r.URL + "/-/blob/" + r.Branch + "/" + file
	case "bitbucket":
		return r.URL + "/src/" + r.Branch + "/" + file
	}
	return r.URL + "/src/branch/" + r.Branch + "/" + file
}

// Hostname is the host without a port.
func (r Repo) Hostname() string {
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		return h
	}
	return r.Host
}

// RawHost is the host README files and images of this repository are served from.
func (r Repo) RawHost() string {
	u, _ := url.Parse(r.RawURL("x"))
	return u.Hostname()
}

// cleanRel resolves a path found in a README (relative to the repository root, where the README lives).
// It never leaves the root: "../x" becomes "x".
func cleanRel(p string) string {
	return strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(p, "./")), "/")
}
