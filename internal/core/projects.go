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

// CreateProject creates a project with the default workflow in the only organization the actor may administer.
func (s *Service) CreateProject(ctx context.Context, a auth.Actor, key, name string) (Project, error) {
	return s.CreateProjectIn(ctx, a, "", key, name)
}

// CreateProjectIn creates a project in an organization (SUPERADMIN, or ADMIN of that organization). An empty
// orgKey means the actor's only organization.
func (s *Service) CreateProjectIn(ctx context.Context, a auth.Actor, orgKey, key, name string) (Project, error) {
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
		org, err := s.projectOrganization(tx, a, orgKey)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO projects (key, name, created_at, organization_id) VALUES (?, ?, ?, ?)`, key, name, p.CreatedAt, org.ID)
		if err != nil {
			if isUnique(err) {
				return rule("project key %s already exists", key)
			}
			return err
		}
		p.ID, _ = res.LastInsertId()
		if org.Role == "user" { // a member who creates a project works in it as a member
			p.Role = "member"
			if _, err := tx.Exec(`INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, 'member')`, p.ID, a.UserID); err != nil {
				return err
			}
		}
		for i, st := range defaultStatuses {
			if _, err := tx.Exec(`INSERT INTO project_statuses (project_id, name, position, category) VALUES (?, ?, ?, ?)`,
				p.ID, st.Name, i, st.Category); err != nil {
				return err
			}
		}
		if !s.skipDefault {
			if err := s.applyDefaultWorkflow(ctx, tx, a, p, org.ID); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "project.create", Target: key, Event: "project.created",
			Project: key, Changes: map[string]any{"name": name}})
	})
	return p, err
}

func (s *Service) ListProjects(ctx context.Context, a auth.Actor) ([]Project, error) {
	return s.ListProjectsIn(ctx, a, "")
}

// ListProjectsIn lists the readable projects, only those of one organization when orgKey is set.
func (s *Service) ListProjectsIn(ctx context.Context, a auth.Actor, orgKey string) ([]Project, error) {
	readable, err := s.readableProjects(s.db.R, a)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT p.id, p.key, p.name, p.archived_at, p.created_at, p.merge_categories, p.repo_url, p.repo_branch, p.time_tracking, COALESCE(o.key, '')
		FROM projects p LEFT JOIN organizations o ON o.id = p.organization_id WHERE ? = '' OR o.key = ? ORDER BY p.key`, orgKey, orgKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		var archived sql.NullInt64
		var repoURL, repoBranch, merged string
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &archived, &p.CreatedAt, &merged, &repoURL, &repoBranch, &p.TimeTracking, &p.Organization); err != nil {
			return nil, err
		}
		p.Repository = linkedRepo(repoURL, repoBranch)
		p.setMerged(merged)
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
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	IsSuperadmin bool   `json:"is_superadmin"`
	Avatar       bool   `json:"avatar"`
}

type Member struct {
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
}

// ListUsers lists the people the actor may see (assignee pickers; see visibleUsersQuery). Only a SUPERADMIN
// session sees who else is SUPERADMIN.
func (s *Service) ListUsers(ctx context.Context, a auth.Actor) ([]User, error) {
	if !a.Can(auth.ScopeRead) {
		return nil, ErrForbidden
	}
	q, args, err := s.visibleUsersQuery(a)
	if err != nil {
		return nil, err
	}
	showFlag := s.IsSuperadmin(a)
	rows, err := s.db.R.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsSuperadmin, &u.Avatar); err != nil {
			return nil, err
		}
		if !showFlag {
			u.IsSuperadmin = false
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CreateUser is an instance-administration action: human admins only, journaled like every mutation.
func (s *Service) CreateUser(ctx context.Context, a auth.Actor, email, name, password string, admin bool) (User, error) {
	if err := s.adminOnly(a); err != nil {
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
	u := User{Email: email, Name: name, IsSuperadmin: admin}
	err = s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO users (email, name, password_hash, is_superadmin, created_at) VALUES (?, ?, ?, ?, ?)`,
			email, name, hash, admin, s.nowMs())
		if err != nil {
			if isUnique(err) {
				return rule("email already in use")
			}
			return err
		}
		u.ID, _ = res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "user.create", Target: userTarget(u.ID), Event: "admin.updated",
			Changes: map[string]any{"is_superadmin": admin}})
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

// SetMember adds a member to a project. The only project role is "member" (an empty role means member); the
// user must already belong to the project's organization (ADR-0027).
func (s *Service) SetMember(ctx context.Context, a auth.Actor, key string, userID int64, roleName string) error {
	if roleName != "" && roleName != "member" {
		return invalid("the only project role is member")
	}
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actMembers)
		if err != nil {
			return err
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return invalid("unknown user")
		}
		if !p.OrganizationID.Valid {
			return invalid("the project has no organization yet")
		}
		var inOrg bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM organization_memberships WHERE organization_id = ? AND user_id = ?)`,
			p.OrganizationID.Int64, userID).Scan(&inOrg); err != nil {
			return err
		}
		if !inOrg {
			return invalid("the user is not a member of the project's organization")
		}
		if _, err := tx.Exec(`INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, 'member')
			ON CONFLICT (project_id, user_id) DO NOTHING`, p.ID, userID); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "member.set", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"user_id": userID, "role": "member"}})
	})
}

func (s *Service) RemoveMember(ctx context.Context, a auth.Actor, key string, userID int64) error {
	return s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, key, actMembers)
		if err != nil {
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
		if still, err := canReachProject(tx, userID, p.ID); err != nil {
			return err
		} else if !still {
			if err := purgeProjectTraces(tx, userID, key); err != nil {
				return err
			}
		}
		return s.journal(ctx, tx, a, change{Operation: "member.remove", Target: key, Event: "project.updated",
			Project: key, Changes: map[string]any{"user_id": userID}})
	})
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// applyDefaultWorkflow gives a new project the organization's default project template, when it has one that is
// still visible to it (an instance wide template or one of its own).
func (s *Service) applyDefaultWorkflow(ctx context.Context, tx *sql.Tx, a auth.Actor, p Project, orgID int64) error {
	var id sql.NullInt64
	err := tx.QueryRow(`SELECT default_project_template_id FROM organization_settings WHERE organization_id = ?`, orgID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !id.Valid) {
		return nil
	}
	if err != nil {
		return err
	}
	t, err := scanProjectTemplate(tx.QueryRow(`SELECT `+projectTemplateCols+` FROM project_templates WHERE id = ? AND (organization_id IS NULL OR organization_id = ?)`, id.Int64, orgID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := t.Data.validate(); err != nil {
		return err
	}
	return s.applyTemplateData(ctx, tx, a, p, t.Name, t.Data)
}
