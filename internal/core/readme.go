package core

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/repo"
)

// RepoFetcher is what core needs from the network: the README and image files of a linked repository.
type RepoFetcher interface {
	Readme(ctx context.Context, r repo.Repo) (string, error)
	Get(ctx context.Context, rawURL string, limit int64) (body []byte, contentType string, err error)
}

func (s *Service) SetRepoFetcher(f RepoFetcher) { s.repos = f }

// SetClock replaces the time source (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

const (
	readmeTTL        = 10 * time.Minute
	readmeMinRefresh = 15 * time.Second
)

type readmeEntry struct {
	res  ReadmeResult
	link string // repository URL and branch the entry belongs to
	at   time.Time
}

type readmeCache struct {
	mu sync.Mutex
	m  map[string]readmeEntry
}

// ReadmeResult is what the project page shows. State: unlinked, ok, empty (no README in the repository) or
// error (could not be fetched; Message says why, without secrets: none are ever involved).
type ReadmeResult struct {
	State     string `json:"state"`
	Markdown  string `json:"markdown,omitempty"`
	SourceURL string `json:"source_url,omitempty"`
	FetchedAt int64  `json:"fetched_at,omitempty"`
	Message   string `json:"message,omitempty"`
}

func assetPath(project string) func(string) string {
	return func(abs string) string {
		return "/api/v1/projects/" + project + "/readme/asset?u=" + url.QueryEscape(abs)
	}
}

// Readme returns the README of the project's linked repository, fetched server-side and cached for ten
// minutes. refresh asks for a new copy; it is honoured at most every 15 seconds per project.
func (s *Service) Readme(ctx context.Context, a auth.Actor, key string, refresh bool) (ReadmeResult, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return ReadmeResult{}, err
	}
	if p.Repository == nil {
		return ReadmeResult{State: "unlinked"}, nil
	}
	link := p.Repository.URL + "@" + p.Repository.Branch
	now := s.now()
	s.readmes.mu.Lock()
	e, ok := s.readmes.m[p.Key]
	s.readmes.mu.Unlock()
	if ok && e.link == link {
		age := now.Sub(e.at)
		if age < readmeMinRefresh || (!refresh && age < readmeTTL) {
			return e.res, nil
		}
	}
	if s.repos == nil {
		return ReadmeResult{State: "error", Message: "fetching READMEs is not available"}, nil
	}
	fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res := ReadmeResult{FetchedAt: now.UnixMilli(), SourceURL: p.Repository.BlobURL("README.md")}
	text, err := s.repos.Readme(fctx, *p.Repository)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		res.State = "empty"
	case err != nil:
		slog.Warn("readme fetch failed", "project", p.Key, "host", p.Repository.Host, "error", err)
		res.State, res.Message = "error", err.Error()
	default:
		res.State = "ok"
		res.Markdown = repo.Rewriter{Repo: *p.Repository, Asset: assetPath(p.Key)}.Rewrite(text)
	}
	s.readmes.mu.Lock()
	s.readmes.m[p.Key] = readmeEntry{res: res, link: link, at: now}
	s.readmes.mu.Unlock()
	return res, nil
}

// ReadmeAsset fetches an image a README refers to, through the same safe client. Only images from hosts the
// README may use are served, and only real image data.
func (s *Service) ReadmeAsset(ctx context.Context, a auth.Actor, key, rawURL string) ([]byte, string, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return nil, "", err
	}
	u, perr := url.Parse(rawURL)
	if p.Repository == nil || perr != nil || u.Scheme != "https" || u.User != nil || !p.Repository.ImageHostAllowed(u.Hostname()) {
		return nil, "", ErrNotFound
	}
	if s.repos == nil {
		return nil, "", ErrNotFound
	}
	fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, declared, err := s.repos.Get(fctx, u.String(), repo.MaxImage)
	if err != nil {
		if !errors.Is(err, repo.ErrNotFound) {
			slog.Warn("readme image fetch failed", "project", p.Key, "host", u.Host, "error", err)
		}
		return nil, "", ErrNotFound
	}
	ct := repo.ImageType(declared, body)
	if ct == "" {
		return nil, "", ErrNotFound
	}
	return body, ct, nil
}

// RequireType fails with a rule error unless the item is of the given type (used by the issue tools).
func (s *Service) RequireType(ctx context.Context, a auth.Actor, key, typ string) error {
	it, err := s.GetItem(ctx, a, key)
	if err != nil {
		return err
	}
	if it.Type != typ {
		return rule("%s is a %s, not a %s", key, it.Type, typ)
	}
	return nil
}
