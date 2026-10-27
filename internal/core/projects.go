package core

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/repo"
)

var projectKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

var defaultStatuses = []struct{ Name, Category string }{
	{"Backlog", "todo"}, {"Todo", "todo"}, {"In Progress", "active"}, {"Review", "review"}, {"Done", "completed"},
}

// CreateProject creates a project with the default workflow; the creator becomes its admin.
func (s *Service) CreateProject(ctx context.Context, a auth.Actor, key, name string) (Project, error) {
	if !a.Can(auth.ScopeWrite) {
		return Project{}, ErrForbidden
	}
	key, name = strings.ToUpper(strings.TrimSpace(key)), strings.TrimSpace(name)
	if !projectKeyRe.MatchString(key) {
		return Project{}, invalid("key must be 2-10 characters: letters and digits, starting with a letter")
	}
	if name == "" {
		return Project{}, invalid("name is required")
	}
	if !a.CanProject(key) {
		return Project{}, ErrForbidden
	}

	p := Project{Key: key, Name: name, CreatedAt: s.nowMs(), Role: "admin"}
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO projects (key, name, created_at) VALUES (?, ?, ?)`, key, name, p.CreatedAt)
		if err != nil {
			if isUnique(err) {
				return rule("project key %s already exists", key)
			}
			return err
		}
		p.ID, _ = res.LastInsertId()
		for i, st := range defaultStatuses {
			if _, err := tx.Exec(`INSERT INTO project_statuses (project_id, name, position, category) VALUES (?, ?, ?, ?)`,
				p.ID, st.Name, i, st.Category); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, 'admin')`, p.ID, a.UserID); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "project.create", Target: key, Event: "project.created",
			Project: key, Changes: map[string]any{"name": name}})
	})
	return p, err
}

func (s *Service) ListProjects(ctx context.Context, a auth.Actor) ([]Project, error) {
	readable, err := s.readableProjects(s.db.R, a)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT id, key, name, archived_at, created_at, merge_columns, repo_url, repo_branch, time_tracking FROM projects ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		var archived sql.NullInt64
		var repoURL, repoBranch string
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &archived, &p.CreatedAt, &p.MergeColumns, &repoURL, &repoBranch, &p.TimeTracking); err != nil {
			return nil, err
		}
		p.Repository = linkedRepo(repoURL, repoBranch)
		if _, ok := readable[p.ID]; !ok {
			continue
		}
		if archived.Valid {
			p.ArchivedAt = &archived.Int64
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) GetProject(ctx context.Context, a auth.Actor, key string) (Project, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	return p, err
}

// UpdateProject accepts name (string), archived (bool) and repository ({"url","branch"}, or null to unlink).
func (s *Service) UpdateProject(ctx context.Context, a auth.Actor, key string, patch map[string]any) (Project, error) {
	var out Project
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actProjectUpdate)
		if err != nil {
			return err
		}
		changes := map[string]any{}
		for k, v := range patch {
			switch k {
			case "name":
				name, ok := v.(string)
				if !ok || strings.TrimSpace(name) == "" {
					return invalid("name must be a non-empty string")
				}
				if _, err := tx.Exec(`UPDATE projects SET name = ? WHERE id = ?`, strings.TrimSpace(name), p.ID); err != nil {
					return err
				}
				changes[k] = map[string]any{"from": p.Name, "to": strings.TrimSpace(name)}
			case "archived":
				archived, ok := v.(bool)
				if !ok {
					return invalid("archived must be a boolean")
				}
				var at any
				if archived {
					at = s.nowMs()
				}
				if _, err := tx.Exec(`UPDATE projects SET archived_at = ? WHERE id = ?`, at, p.ID); err != nil {
					return err
				}
				changes[k] = archived
			case "repository":
				url, branch := "", ""
				if v != nil {
					m, ok := v.(map[string]any)
					if !ok {
						return invalid("repository must be an object with url and branch, or null")
					}
					u, _ := m["url"].(string)
					b, _ := m["branch"].(string)
					r, err := repo.Parse(u, b)
					if err != nil {
						return invalid("%v", err)
					}
					url, branch = r.URL, r.Branch
				}
				if _, err := tx.Exec(`UPDATE projects SET repo_url = ?, repo_branch = ? WHERE id = ?`, url, branch, p.ID); err != nil {
					return err
				}
				changes[k] = map[string]any{"url": url, "branch": branch}
			default:
				return invalid("unknown field %q", k)
			}
		}
		if len(changes) == 0 {
			return invalid("no fields to update")
		}
		if err := s.journal(ctx, tx, a, change{Operation: "project.update", Target: key, Event: "project.updated",
			Project: key, Changes: changes}); err != nil {
			return err
		}
		out, _, err = s.access(tx, a, key, actView)
		return err
	})
	return out, err
}

// ---- users and members ----

type User struct {
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"is_admin"`
	Avatar  bool   `json:"avatar"`
}

type Member struct {
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
}

// ListUsers lists everyone for a signed-in person (assignee pickers). A token only sees people who share a
// project it may access, without the global admin flag.
func (s *Service) ListUsers(ctx context.Context, a auth.Actor) ([]User, error) {
	if !a.Can(auth.ScopeRead) {
		return nil, ErrForbidden
	}
	q, args := `SELECT id, email, name, is_admin, avatar_path IS NOT NULL FROM users ORDER BY name`, []any(nil)
	if a.Kind == auth.KindToken {
		q, args = s.sharedUsersQuery(a)
	}
	rows, err := s.db.R.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.Avatar); err != nil {
			return nil, err
		}
		if a.Kind == auth.KindToken {
			u.IsAdmin = false
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// sharedUsersQuery selects the users that are members of a project the token owner can reach and the token may use.
func (s *Service) sharedUsersQuery(a auth.Actor) (string, []any) {
	q := `SELECT id, email, name, is_admin, avatar_path IS NOT NULL FROM users WHERE id IN (
		SELECT m.user_id FROM project_members m JOIN projects p ON p.id = m.project_id WHERE 1 = 1`
	var args []any
	if !a.IsAdmin {
		q += ` AND p.id IN (SELECT project_id FROM project_members WHERE user_id = ?)`
		args = append(args, a.UserID)
	}
	if a.Projects != nil {
		q += ` AND p.key IN (` + strings.TrimSuffix(strings.Repeat("?,", len(a.Projects)), ",") + `)`
		for _, k := range a.Projects {
			args = append(args, k)
		}
	}
	return q + `) ORDER BY name`, args
}

// CreateUser is an instance-administration action: human admins only, journaled like every mutation.
func (s *Service) CreateUser(ctx context.Context, a auth.Actor, email, name, password string, admin bool) (User, error) {
	if err := adminOnly(a); err != nil {
		return User{}, err
	}
	email = strings.TrimSpace(email)
	if email == "" || name == "" {
		return User{}, invalid("email and name are required")
	}
	if err := checkPassword(s.Policy(), password); err != nil {
		return User{}, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return User{}, err
	}
	u := User{Email: email, Name: name, IsAdmin: admin}
	err = s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO users (email, name, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?, ?)`,
			email, name, hash, admin, s.nowMs())
		if err != nil {
			if isUnique(err) {
				return rule("email already in use")
			}
			return err
		}
		u.ID, _ = res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "user.create", Target: userTarget(u.ID), Event: "admin.updated",
			Changes: map[string]any{"is_admin": admin}})
	})
	return u, err
}

func (s *Service) ListMembers(ctx context.Context, a auth.Actor, key string) ([]Member, error) {
	p, _, err := s.access(s.db.R, a, key, actView)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT u.id, u.name, u.email, m.role FROM project_members m
		JOIN users u ON u.id = m.user_id WHERE m.project_id = ? ORDER BY u.name`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Name, &m.Email, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMember adds or updates a member. An empty roleName means the instance's default role.
func (s *Service) SetMember(ctx context.Context, a auth.Actor, key string, userID int64, roleName string) error {
	if roleName == "" {
		roleName = s.Policy().DefaultRole
	}
	return s.tx(func(tx *sql.Tx) error {
		p, mine, err := s.access(tx, a, key, actMembers)
		if err != nil {
			return err
		}
		var known int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM roles WHERE name = ?`, roleName).Scan(&known); err != nil {
			return err
		}
		if known == 0 {
			return invalid("unknown role")
		}
		if err := guardAdminRole(tx, p.ID, userID, mine, roleName); err != nil {
			return err
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return invalid("unknown user")
		}
		if roleName != "admin" {
			if err := s.guardLastAdmin(tx, p.ID, userID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, ?)
			ON CONFLICT (project_id, user_id) DO UPDATE SET role = excluded.role`, p.ID, userID, roleName); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "member.set", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"user_id": userID, "role": roleName}})
	})
}

func (s *Service) RemoveMember(ctx context.Context, a auth.Actor, key string, userID int64) error {
	return s.tx(func(tx *sql.Tx) error {
		p, mine, err := s.access(tx, a, key, actMembers)
		if err != nil {
			return err
		}
		if err := guardAdminRole(tx, p.ID, userID, mine, ""); err != nil {
			return err
		}
		if err := s.guardLastAdmin(tx, p.ID, userID); err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM project_members WHERE project_id = ? AND user_id = ?`, p.ID, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if err := s.revokeOwnedProjectTokens(tx, userID, key); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "member.remove", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"user_id": userID}})
	})
}

// guardAdminRole stops a custom role that holds "members" from escalating: only an admin may grant,
// change or remove the admin role. newRole is "" when the member is being removed.
func guardAdminRole(tx *sql.Tx, projectID, userID int64, actorRole, newRole string) error {
	if actorRole == "admin" {
		return nil
	}
	var cur string
	err := tx.QueryRow(`SELECT role FROM project_members WHERE project_id = ? AND user_id = ?`, projectID, userID).Scan(&cur)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if cur == "admin" || newRole == "admin" {
		return ErrForbidden
	}
	return nil
}

// guardLastAdmin refuses to demote or remove the only admin, which would orphan the project.
func (s *Service) guardLastAdmin(tx *sql.Tx, projectID, userID int64) error {
	var isAdmin bool
	err := tx.QueryRow(`SELECT role = 'admin' FROM project_members WHERE project_id = ? AND user_id = ?`, projectID, userID).Scan(&isAdmin)
	if errors.Is(err, sql.ErrNoRows) || !isAdmin {
		return nil
	}
	if err != nil {
		return err
	}
	var admins int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM project_members WHERE project_id = ? AND role = 'admin'`, projectID).Scan(&admins); err != nil {
		return err
	}
	if admins <= 1 {
		return rule("a project needs at least one admin")
	}
	return nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
