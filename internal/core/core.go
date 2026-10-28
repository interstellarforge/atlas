// Package core holds all business rules. REST and MCP are thin adapters over it.
package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/db"
	"github.com/kees/atlas/internal/media"
	"github.com/kees/atlas/internal/repo"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
)

// InvalidError is a malformed or unacceptable input (HTTP 400).
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

// RuleError breaks a business rule such as hierarchy or workflow (HTTP 422).
type RuleError struct{ Msg string }

func (e *RuleError) Error() string { return e.Msg }

// ConflictError is an optimistic-locking failure (HTTP 409). Current holds the
// item's compact current state so the caller can retry without another read.
type ConflictError struct {
	Msg     string
	Current map[string]any
}

func (e *ConflictError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &InvalidError{fmt.Sprintf(format, a...)} }
func rule(format string, a ...any) error    { return &RuleError{fmt.Sprintf(format, a...)} }

type Service struct {
	db     *db.DB
	users  *auth.Service
	now    func() time.Time
	notify func()
	// outer is set on the per-batch copy of the service: tx() then joins the batch transaction.
	outer *sql.Tx
	media *media.Store
	mail  MailConfig
	repos RepoFetcher
	// readmes caches fetched READMEs per project key (ADR-0013).
	readmes *readmeCache
}

// SetNotifier registers a callback that runs after every committed mutation, so realtime
// subscribers can look for new change events. It must not block.
func (s *Service) SetNotifier(f func()) { s.notify = f }

// ReadableProjectKeys lists the project keys the actor may read.
func (s *Service) ReadableProjectKeys(a auth.Actor) (map[string]bool, error) {
	byID, err := s.readableProjects(s.db.R, a)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(byID))
	for _, k := range byID {
		out[k] = true
	}
	return out, nil
}

func New(d *db.DB, users *auth.Service) *Service {
	return &Service{db: d, users: users, now: time.Now, readmes: &readmeCache{m: map[string]readmeEntry{}}}
}

func (s *Service) nowMs() int64 { return s.now().UnixMilli() }

type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// tx runs f in a write transaction (BEGIN IMMEDIATE on the single writer connection).
func (s *Service) tx(f func(tx *sql.Tx) error) error {
	if s.outer != nil {
		return f(s.outer) // Batch commits, rolls back and notifies
	}
	tx, err := s.db.W.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Events are only announced after the commit, never from inside the transaction.
	if s.notify != nil {
		s.notify()
	}
	return nil
}

type ctxKey int

const batchKey ctxKey = iota

// WithBatch tags every mutation made with the returned context with a batch id in the audit log.
func WithBatch(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, batchKey, id)
}

func batchFrom(ctx context.Context) any {
	if id, ok := ctx.Value(batchKey).(string); ok && id != "" {
		return id
	}
	return nil
}

// ---- authorization ----

type Project struct {
	ID         int64  `json:"-"`
	Key        string `json:"key"`
	Name       string `json:"name"`
	ArchivedAt *int64 `json:"archived_at"`
	CreatedAt  int64  `json:"created_at"`
	Role       string `json:"role,omitempty"`
	// MergeColumns is true when any category is merged; MergeCategories names them (ADR-0011, ADR-0020).
	MergeColumns    bool     `json:"merge_columns"`
	MergeCategories []string `json:"merge_categories"`
	// Repository is the linked git repository, or null (ADR-0013).
	Repository *repo.Repo `json:"repository"`
	// TimeTracking lets members book time on items (ADR-0014).
	TimeTracking bool `json:"time_tracking"`
}

// linkedRepo rebuilds the repository link stored on a project row.
func linkedRepo(url, branch string) *repo.Repo {
	if url == "" {
		return nil
	}
	r, err := repo.Parse(url, branch)
	if err != nil {
		return nil
	}
	return &r
}

// access resolves the project and checks the actor's role grants act on it.
// Projects the actor cannot see are reported as not found, so existence does not leak.
// A global admin holds every action. The returned string is the actor's role name.
func (s *Service) access(q queryer, a auth.Actor, key string, act action) (Project, string, error) {
	scope := auth.ScopeRead
	if act != actView && act != actExport {
		scope = auth.ScopeWrite
	}
	if !a.Can(scope) {
		return Project{}, "", ErrForbidden
	}
	var p Project
	var archived sql.NullInt64
	var repoURL, repoBranch, merged string
	err := q.QueryRow(`SELECT id, key, name, archived_at, created_at, merge_categories, repo_url, repo_branch, time_tracking FROM projects WHERE key = ?`, key).
		Scan(&p.ID, &p.Key, &p.Name, &archived, &p.CreatedAt, &merged, &repoURL, &repoBranch, &p.TimeTracking)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !a.CanProject(p.Key)) {
		return Project{}, "", ErrNotFound
	}
	if err != nil {
		return Project{}, "", err
	}
	if archived.Valid {
		p.ArchivedAt = &archived.Int64
	}
	p.Repository = linkedRepo(repoURL, repoBranch)
	p.setMerged(merged)

	role, allowed := "admin", true
	if !a.IsAdmin {
		err := q.QueryRow(`SELECT m.role, EXISTS (SELECT 1 FROM role_actions r WHERE r.role = m.role AND r.action = ?)
			FROM project_members m WHERE m.project_id = ? AND m.user_id = ?`, string(act), p.ID, a.UserID).Scan(&role, &allowed)
		if errors.Is(err, sql.ErrNoRows) {
			return Project{}, "", ErrNotFound
		}
		if err != nil {
			return Project{}, "", err
		}
	}
	if !allowed {
		return Project{}, "", ErrForbidden
	}
	p.Role = role
	return p, role, nil
}

// readableProjects lists the project ids and keys the actor may read.
func (s *Service) readableProjects(q queryer, a auth.Actor) (map[int64]string, error) {
	if !a.Can(auth.ScopeRead) {
		return nil, ErrForbidden
	}
	query := `SELECT id, key FROM projects`
	args := []any{}
	if !a.IsAdmin {
		query = `SELECT p.id, p.key FROM projects p JOIN project_members m ON m.project_id = p.id WHERE m.user_id = ?`
		args = append(args, a.UserID)
	}
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, err
		}
		if a.CanProject(key) {
			out[id] = key
		}
	}
	return out, rows.Err()
}

// setMerged fills the merge fields from the stored comma separated category list.
func (p *Project) setMerged(stored string) {
	p.MergeCategories = []string{}
	if stored != "" {
		p.MergeCategories = strings.Split(stored, ",")
	}
	p.MergeColumns = len(p.MergeCategories) > 0
}

// merges reports whether statuses of this category share a board column.
func (p Project) merges(category string) bool { return slices.Contains(p.MergeCategories, category) }
