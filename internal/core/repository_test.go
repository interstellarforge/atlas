package core_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/repo"
)

type fakeRepos struct {
	mu     sync.Mutex
	readme string
	err    error
	calls  int
	got    []string
	image  []byte
}

func (f *fakeRepos) Readme(_ context.Context, _ repo.Repo) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.readme, f.err
}

func (f *fakeRepos) Get(_ context.Context, u string, _ int64) ([]byte, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, u)
	return f.image, "image/png", f.err
}

var pngData = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func link(t *testing.T, e *env, project string) {
	t.Helper()
	if _, err := e.svc.UpdateProject(ctx, e.admin, project, map[string]any{"repository": map[string]any{"url": "git@github.com:acme/atlas.git", "branch": "develop"}}); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryLink(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	e.project("OTH")
	if err := e.svc.SetMember(ctx, e.admin, "EXO", e.aliceID, "member"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetMember(ctx, e.admin, "EXO", e.bobID, "viewer"); err != nil {
		t.Fatal(err)
	}

	link(t, e, "EXO")
	p, _ := e.svc.GetProject(ctx, e.alice, "EXO") // members can read the link
	if p.Repository == nil || p.Repository.URL != "https://github.com/acme/atlas" || p.Repository.Branch != "develop" || p.Repository.Provider != "github" {
		t.Fatalf("repository: %+v", p.Repository)
	}
	ps, _ := e.svc.ListProjects(ctx, e.admin)
	for _, x := range ps {
		if (x.Key == "EXO") != (x.Repository != nil) {
			t.Fatalf("%s: repository %+v", x.Key, x.Repository)
		}
	}

	// Only project.update (admins) may change it; another project's admin-less members cannot either.
	for name, a := range map[string]struct{ err error }{
		"member": {func() error {
			_, err := e.svc.UpdateProject(ctx, e.alice, "EXO", map[string]any{"repository": nil})
			return err
		}()},
		"viewer": {func() error {
			_, err := e.svc.UpdateProject(ctx, e.bob, "EXO", map[string]any{"repository": nil})
			return err
		}()},
		"outsider": {func() error {
			_, err := e.svc.UpdateProject(ctx, e.carol, "EXO", map[string]any{"repository": nil})
			return err
		}()},
	} {
		if !errors.Is(a.err, core.ErrForbidden) && !errors.Is(a.err, core.ErrNotFound) {
			t.Fatalf("%s unlinking: %v", name, a.err)
		}
	}
	if p, _ = e.svc.GetProject(ctx, e.admin, "EXO"); p.Repository == nil {
		t.Fatal("link was removed by someone without the right")
	}

	// Credentials and odd addresses are refused and nothing is stored.
	for _, bad := range []string{"https://user:secret@github.com/acme/atlas", "http://github.com/acme/atlas", "file:///etc", "https://github.com/acme"} {
		_, err := e.svc.UpdateProject(ctx, e.admin, "EXO", map[string]any{"repository": map[string]any{"url": bad}})
		if !isInvalid(err) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	if n := e.count(`SELECT COUNT(*) FROM audit_entries WHERE operation = 'project.update' AND changes LIKE '%secret%'`); n != 0 {
		t.Fatal("rejected credentials reached the audit log")
	}

	// Audited with the actor, and unlinking works.
	var actor, changes string
	if err := e.db.R.QueryRow(`SELECT actor, changes FROM audit_entries WHERE operation = 'project.update' ORDER BY id LIMIT 1`).Scan(&actor, &changes); err != nil ||
		!strings.Contains(changes, "https://github.com/acme/atlas") || actor == "" {
		t.Fatalf("audit: %q %q %v", actor, changes, err)
	}
	if p, _ = e.svc.UpdateProject(ctx, e.admin, "EXO", map[string]any{"repository": nil}); p.Repository != nil {
		t.Fatalf("unlink: %+v", p.Repository)
	}
}

func TestReadme(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	e.project("OTH")
	f := &fakeRepos{readme: "# Hi\n![logo](docs/logo.png)\n[guide](docs/guide.md)\n"}
	e.svc.SetRepoFetcher(f)
	now := time.Now()
	e.svc.SetClock(func() time.Time { return now })

	if r, err := e.svc.Readme(ctx, e.admin, "EXO", false); err != nil || r.State != "unlinked" {
		t.Fatalf("unlinked: %+v %v", r, err)
	}
	link(t, e, "EXO")
	r, err := e.svc.Readme(ctx, e.admin, "EXO", false)
	if err != nil || r.State != "ok" || f.calls != 1 {
		t.Fatalf("fetch: %+v %v calls=%d", r, err, f.calls)
	}
	if !strings.Contains(r.Markdown, "/api/v1/projects/EXO/readme/asset?u=https%3A%2F%2Fraw.githubusercontent.com%2Facme%2Fatlas%2Fdevelop%2Fdocs%2Flogo.png") ||
		!strings.Contains(r.Markdown, "[guide](https://github.com/acme/atlas/blob/develop/docs/guide.md)") {
		t.Fatalf("markdown not rewritten for branch develop:\n%s", r.Markdown)
	}

	// Cached; a refresh is honoured only after 15 seconds; the cache is dropped when the link changes.
	e.svc.Readme(ctx, e.admin, "EXO", false)
	e.svc.Readme(ctx, e.admin, "EXO", true)
	if f.calls != 1 {
		t.Fatalf("calls %d: cache or refresh throttle missing", f.calls)
	}
	now = now.Add(20 * time.Second)
	e.svc.Readme(ctx, e.admin, "EXO", true)
	if f.calls != 2 {
		t.Fatalf("calls %d: refresh ignored", f.calls)
	}
	if _, err := e.svc.UpdateProject(ctx, e.admin, "EXO", map[string]any{"repository": map[string]any{"url": "https://github.com/acme/other"}}); err != nil {
		t.Fatal(err)
	}
	e.svc.Readme(ctx, e.admin, "EXO", false)
	if f.calls != 3 {
		t.Fatalf("calls %d: stale cache served for another repository", f.calls)
	}

	// No README and failures are states, not errors.
	now = now.Add(time.Hour)
	f.err = repo.ErrNotFound
	if r, _ = e.svc.Readme(ctx, e.admin, "EXO", false); r.State != "empty" {
		t.Fatalf("not found: %+v", r)
	}
	now = now.Add(time.Hour)
	f.err = errors.New("fetch github.com: timeout")
	if r, _ = e.svc.Readme(ctx, e.admin, "EXO", false); r.State != "error" || !strings.Contains(r.Message, "timeout") {
		t.Fatalf("error: %+v", r)
	}

	// Reading follows project access.
	if _, err := e.svc.Readme(ctx, e.carol, "EXO", false); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("non-member: %v", err)
	}
}

func TestReadmeAssetHostsAndTypes(t *testing.T) {
	e := setup(t)
	e.project("EXO")
	link(t, e, "EXO")
	f := &fakeRepos{image: pngData}
	e.svc.SetRepoFetcher(f)

	body, ct, err := e.svc.ReadmeAsset(ctx, e.admin, "EXO", "https://raw.githubusercontent.com/acme/atlas/develop/a.png")
	if err != nil || ct != "image/png" || len(body) == 0 {
		t.Fatalf("allowed image: %v %q", err, ct)
	}
	// Not the repository's host, not a badge host, not https, or credentials: never fetched.
	calls := len(f.got)
	for _, u := range []string{"https://evil.example.com/a.png", "http://raw.githubusercontent.com/a.png", "https://user:pw@raw.githubusercontent.com/a.png",
		"https://169.254.169.254/latest/meta-data", "file:///etc/passwd", ""} {
		if _, _, err := e.svc.ReadmeAsset(ctx, e.admin, "EXO", u); !errors.Is(err, core.ErrNotFound) {
			t.Errorf("%q: %v", u, err)
		}
	}
	if len(f.got) != calls {
		t.Fatalf("fetched a refused URL: %v", f.got[calls:])
	}
	// Data that is not an image is refused even from an allowed host.
	f.image = []byte("<html><script>alert(1)</script></html>")
	if _, _, err := e.svc.ReadmeAsset(ctx, e.admin, "EXO", "https://raw.githubusercontent.com/acme/atlas/develop/x.png"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("html served as image: %v", err)
	}
	// A project without a link serves nothing.
	e.project("OTH")
	if _, _, err := e.svc.ReadmeAsset(ctx, e.admin, "OTH", "https://raw.githubusercontent.com/acme/atlas/develop/a.png"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unlinked project: %v", err)
	}
}
