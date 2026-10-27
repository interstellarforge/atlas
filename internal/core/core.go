// Package core holds all business rules. REST and MCP are thin adapters over it.
package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/db"
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

func New(d *db.DB, users *auth.Service) *Service { return &Service{db: d, users: users, now: time.Now} }

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

type role int

const (
	roleNone role = iota
	roleViewer
	roleMember
	roleAdmin
)

func parseRole(s string) role {
	switch s {
	case "admin":
		return roleAdmin
	case "member":
		return roleMember
	case "viewer":
		return roleViewer
	}
	return roleNone
}

type Project struct {
	ID         int64  `json:"-"`
	Key        string `json:"key"`
	Name       string `json:"name"`
	ArchivedAt *int64 `json:"archived_at"`
	CreatedAt  int64  `json:"created_at"`
	Role       string `json:"role,omitempty"`
}

// access resolves the project and checks the actor holds at least min on it.
// Projects the actor cannot see are reported as not found, so existence does not leak.
func (s *Service) access(q queryer, a auth.Actor, key string, min role) (Project, role, error) {
	scope := auth.ScopeRead
	if min >= roleMember {
		scope = auth.ScopeWrite
	}
	if !a.Can(scope) {
		return Project{}, roleNone, ErrForbidden
	}
	var p Project
	var archived sql.NullInt64
	err := q.QueryRow(`SELECT id, key, name, archived_at, created_at FROM projects WHERE key = ?`, key).
		Scan(&p.ID, &p.Key, &p.Name, &archived, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !a.CanProject(p.Key)) {
		return Project{}, roleNone, ErrNotFound
	}
	if err != nil {
		return Project{}, roleNone, err
	}
	if archived.Valid {
		p.ArchivedAt = &archived.Int64
	}

	r := roleNone
	if a.IsAdmin {
		r = roleAdmin
	} else {
		var name string
		err := q.QueryRow(`SELECT role FROM project_members WHERE project_id = ? AND user_id = ?`, p.ID, a.UserID).Scan(&name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Project{}, roleNone, err
		}
		r = parseRole(name)
	}
	if r == roleNone {
		return Project{}, roleNone, ErrNotFound
	}
	if r < min {
		return Project{}, roleNone, ErrForbidden
	}
	p.Role = [...]string{"", "viewer", "member", "admin"}[r]
	return p, r, nil
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
