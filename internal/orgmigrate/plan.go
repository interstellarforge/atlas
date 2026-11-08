// Package orgmigrate moves an existing single-organization database into the organization model (ADR-0028).
// Plan reads and decides, Apply writes in one transaction, Verify checks the result against the plan.
package orgmigrate

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/kees/atlas/internal/core"
)

// Reader is what the planner needs; *sql.DB and *sql.Tx both satisfy it.
type Reader interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

type Config struct {
	SuperadminEmail string
	OrgKey          string
	OrgName         string
}

// Mapping holds the owner's decisions. Admins: email -> admin|user|none for legacy admins other than the SUPERADMIN.
// Roles: role -> member|remove for every REVIEW-REQUIRED role.
type Mapping struct {
	Admins map[string]string `json:"admins"`
	Roles  map[string]string `json:"roles"`
}

type AdminRow struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	Active      bool   `json:"active"`
	Tokens      int    `json:"tokens"`
	Memberships int    `json:"project_memberships"`
	Decision    string `json:"decision"` // superadmin | admin | user | none | suspended
}

type RoleRow struct {
	Name       string   `json:"name"`
	Builtin    bool     `json:"builtin"`
	Actions    []string `json:"actions"`
	Members    int      `json:"members"`
	Class      string   `json:"class"`                // equal | admin | review_required
	Resolution string   `json:"resolution,omitempty"` // member | remove (review_required only)
}

// Transition is one user x project whose effective actions change. Kind is the classification of ADR-0028.
type Transition struct {
	User    string   `json:"user"`
	Project string   `json:"project"`
	Cause   string   `json:"cause"`
	Kind    string   `json:"kind"` // approved-reduction | approved-expansion | UNEXPECTED
	Lost    []string `json:"lost"`
	Gained  []string `json:"gained"`
}

type Report struct {
	Superadmin  string         `json:"superadmin"`
	OrgKey      string         `json:"org_key"`
	OrgName     string         `json:"org_name"`
	Admins      []AdminRow     `json:"admins"`
	Roles       []RoleRow      `json:"roles"`
	Mapping     Mapping        `json:"mapping"`
	Transitions []Transition   `json:"transitions"`
	Counts      map[string]int `json:"counts"`
	Invites     int            `json:"unused_admin_invites_to_revoke"`
	Warnings    []string       `json:"warnings"`
	Blocking    []string       `json:"blocking"`

	// Computed outcome, used by Apply and Verify; part of the hash.
	OrgMembers  map[int64]string  `json:"org_members"`  // user id -> admin|user
	ProjectRows map[string]string `json:"project_rows"` // "project:user" -> member (rows that stay)
}

// Hash identifies the reviewed report including the mapping.
func (r Report) Hash() string {
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type user struct {
	id      int64
	email   string
	admin   bool
	active  bool
	members map[int64]string // project id -> raw role
}

type project struct {
	id  int64
	key string
}

func actionSet(l []string) map[string]bool {
	m := map[string]bool{}
	for _, a := range l {
		m[a] = true
	}
	return m
}

func diff(from, to map[string]bool) (lost, gained []string) {
	for _, a := range core.Actions {
		if from[a] && !to[a] {
			lost = append(lost, a)
		}
		if !from[a] && to[a] {
			gained = append(gained, a)
		}
	}
	return
}

func allActions() map[string]bool { return actionSet(core.Actions) }

func sameSet(a, b map[string]bool) bool {
	l, g := diff(a, b)
	return len(l) == 0 && len(g) == 0
}

// Plan reads the database and computes the report. It writes nothing.
func Plan(q Reader, cfg Config, m Mapping) (Report, error) {
	rep := Report{OrgKey: cfg.OrgKey, OrgName: cfg.OrgName, Mapping: m, Counts: map[string]int{},
		Warnings: []string{}, Blocking: []string{}, Admins: []AdminRow{}, Roles: []RoleRow{}, Transitions: []Transition{},
		OrgMembers: map[int64]string{}, ProjectRows: map[string]string{}}
	if m.Admins == nil {
		rep.Mapping.Admins = map[string]string{}
	}
	if m.Roles == nil {
		rep.Mapping.Roles = map[string]string{}
	}
	block := func(f string, a ...any) { rep.Blocking = append(rep.Blocking, fmt.Sprintf(f, a...)) }

	var orgs, owned int
	if err := q.QueryRow(`SELECT (SELECT COUNT(*) FROM organizations), (SELECT COUNT(*) FROM projects WHERE organization_id IS NOT NULL)`).Scan(&orgs, &owned); err != nil {
		return rep, err
	}
	if orgs > 0 || owned > 0 {
		block("already migrated: the database has organizations or projects assigned to one")
		return rep, nil // a migrated database has no legacy columns left to read
	}
	if cfg.OrgKey == "" || cfg.OrgName == "" {
		block("default organization key and name are required")
	}

	for _, c := range []struct{ name, query string }{
		{"users", `SELECT COUNT(*) FROM users`}, {"projects", `SELECT COUNT(*) FROM projects`},
		{"work_items", `SELECT COUNT(*) FROM work_items`}, {"comments", `SELECT COUNT(*) FROM comments`},
		{"attachments", `SELECT COUNT(*) FROM attachments`}, {"project_members", `SELECT COUNT(*) FROM project_members`},
	} {
		var n int
		if err := q.QueryRow(c.query).Scan(&n); err != nil {
			return rep, err
		}
		rep.Counts[c.name] = n
	}

	users, _, err := loadUsers(q)
	if err != nil {
		return rep, err
	}
	projects, err := loadProjects(q)
	if err != nil {
		return rep, err
	}
	roles, err := loadRoles(q)
	if err != nil {
		return rep, err
	}

	// roles: classify, resolve through the mapping
	memberSet := actionSet(core.MemberActions)
	roleOf := map[string]*RoleRow{}
	for i := range roles {
		r := &roles[i]
		set := actionSet(r.Actions)
		switch {
		case r.Name == "admin":
			r.Class = "admin"
		case sameSet(set, memberSet):
			r.Class = "equal"
		default:
			r.Class = "review_required"
		}
		roleOf[r.Name] = r
	}
	for _, u := range users { // roles used by members but missing from the roles table
		for _, raw := range u.members {
			if _, ok := roleOf[strings.ToLower(raw)]; !ok {
				r := &RoleRow{Name: strings.ToLower(raw), Actions: []string{}, Class: "review_required"}
				roles = append(roles, *r)
				roleOf[r.Name] = &roles[len(roles)-1]
			}
		}
	}
	for i := range roles { // re-point after appends
		roleOf[roles[i].Name] = &roles[i]
	}
	for _, u := range users {
		for _, raw := range u.members {
			roleOf[strings.ToLower(raw)].Members++
		}
	}
	for name, res := range m.Roles {
		r, ok := roleOf[strings.ToLower(name)]
		switch {
		case !ok:
			block("mapping names the unknown role %q", name)
		case r.Class != "review_required":
			block("mapping resolves role %q, which is not review-required", name)
		case res != "member" && res != "remove":
			block("role %q: resolution must be member or remove, not %q", name, res)
		default:
			r.Resolution = res
		}
	}
	for i := range roles {
		if roles[i].Class == "review_required" && roles[i].Resolution == "" && roles[i].Members > 0 { // an unused role changes nobody
			block("role %q needs a decision in the mapping (member or remove)", roles[i].Name)
		}
	}
	slices.SortFunc(roles, func(a, b RoleRow) int { return strings.Compare(a.Name, b.Name) })
	rep.Roles = roles
	for i := range rep.Roles {
		roleOf[rep.Roles[i].Name] = &rep.Roles[i]
	}

	// the one SUPERADMIN
	var super *user
	for _, u := range users {
		if u.admin && u.active && strings.EqualFold(u.email, cfg.SuperadminEmail) {
			if super != nil {
				block("several accounts match %q", cfg.SuperadminEmail)
			}
			super = u
		}
	}
	if super == nil {
		block("ATLAS_SUPERADMIN_EMAIL %q must match one active account that is a legacy admin", cfg.SuperadminEmail)
	} else {
		rep.Superadmin = super.email
	}

	// legacy admins
	decisions := map[int64]string{}
	for _, u := range users {
		if !u.admin {
			continue
		}
		row := AdminRow{ID: u.id, Email: u.email, Active: u.active, Memberships: len(u.members)}
		_ = q.QueryRow(`SELECT COUNT(*) FROM api_tokens WHERE user_id = ? AND revoked_at IS NULL`, u.id).Scan(&row.Tokens)
		switch {
		case super != nil && u.id == super.id:
			row.Decision = "superadmin"
		case !u.active:
			row.Decision = "suspended"
			if _, ok := m.Admins[u.email]; ok {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("mapping for suspended admin %s is ignored", u.email))
			}
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("suspended admin %s gets no organization role: assign one after reactivating the account", u.email))
		default:
			row.Decision = "admin"
			if d, ok := lookup(m.Admins, u.email); ok {
				if d != "admin" && d != "user" && d != "none" {
					block("admin %s: decision must be admin, user or none, not %q", u.email, d)
				} else {
					row.Decision = d
				}
			}
		}
		decisions[u.id] = row.Decision
		rep.Admins = append(rep.Admins, row)
	}
	for email := range m.Admins {
		if !slices.ContainsFunc(rep.Admins, func(a AdminRow) bool { return strings.EqualFold(a.Email, email) }) {
			block("mapping names %q, which is not a legacy admin", email)
		}
	}
	slices.SortFunc(rep.Admins, func(a, b AdminRow) int { return strings.Compare(a.Email, b.Email) })

	// project rows that stay, and organization memberships
	retained := map[int64]bool{} // users with at least one retained row
	causes := map[string]string{}
	for _, u := range users {
		for pid, raw := range u.members {
			r := roleOf[strings.ToLower(raw)]
			key := fmt.Sprintf("%d:%d", pid, u.id)
			switch {
			case r.Class == "equal":
				rep.ProjectRows[key], causes[key] = "member", "member"
			case r.Class == "admin":
				rep.ProjectRows[key], causes[key] = "member", "admin_to_member"
			case r.Resolution == "member":
				rep.ProjectRows[key], causes[key] = "member", "mapped_member:"+r.Name
			case r.Resolution == "remove":
				causes[key] = "mapped_remove:" + r.Name
			}
			if _, ok := rep.ProjectRows[key]; ok {
				retained[u.id] = true
			}
		}
	}
	for _, u := range users {
		switch decisions[u.id] {
		case "superadmin", "admin":
			rep.OrgMembers[u.id] = "admin"
		case "user":
			rep.OrgMembers[u.id] = "user"
		}
		if _, ok := rep.OrgMembers[u.id]; !ok && retained[u.id] {
			rep.OrgMembers[u.id] = "user"
		}
	}

	// effective actions, old and new, per active user and project
	pkeys := map[int64]string{}
	for _, p := range projects {
		pkeys[p.id] = p.key
	}
	for _, u := range users {
		if !u.active {
			continue
		}
		for _, p := range projects {
			old := oldActions(u, p.id, roleOf)
			cause := adminCause(decisions[u.id], u, p.id, causes)
			nw := newActions(u.id, p.id, rep.OrgMembers, decisions, rep.ProjectRows)
			lost, gained := diff(old, nw)
			if len(lost) == 0 && len(gained) == 0 {
				continue
			}
			t := Transition{User: u.email, Project: p.key, Cause: cause, Lost: nz(lost), Gained: nz(gained)}
			t.Kind = classify(t)
			rep.Transitions = append(rep.Transitions, t)
			if t.Kind == "UNEXPECTED" {
				block("unexpected change for %s on %s: lost %v, gained %v", u.email, p.key, lost, gained)
			}
			if len(lost) > 0 && len(rep.Warnings) < 200 {
				var tok int
				_ = q.QueryRow(`SELECT COUNT(*) FROM api_tokens WHERE user_id = ? AND revoked_at IS NULL`, u.id).Scan(&tok)
				if tok > 0 {
					rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s loses actions on %s and has %d active token(s): review them", u.email, p.key, tok))
				}
			}
		}
	}
	slices.SortFunc(rep.Transitions, func(a, b Transition) int {
		if c := strings.Compare(a.User, b.User); c != 0 {
			return c
		}
		return strings.Compare(a.Project, b.Project)
	})

	if err := q.QueryRow(`SELECT COUNT(*) FROM invites WHERE is_admin = 1 AND used_at IS NULL`).Scan(&rep.Invites); err != nil {
		return rep, err
	}
	var open string
	_ = q.QueryRow(`SELECT value FROM instance_settings WHERE key = 'registration'`).Scan(&open)
	if open == "open" {
		rep.Warnings = append(rep.Warnings, "registration is open: new accounts will have no organization or project access")
	}
	return rep, nil
}

func nz(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func lookup(m map[string]string, email string) (string, bool) {
	for k, v := range m {
		if strings.EqualFold(k, email) {
			return v, true
		}
	}
	return "", false
}

// adminCause names why a user's actions on a project change.
func adminCause(decision string, u *user, projectID int64, rowCauses map[string]string) string {
	if u.admin {
		switch decision {
		case "user", "none":
			return "admin_decision:" + decision
		}
	}
	if c, ok := rowCauses[fmt.Sprintf("%d:%d", projectID, u.id)]; ok {
		return c
	}
	return "none"
}

// classify says whether a change is covered by an owner decision. Everything else is UNEXPECTED and aborts.
func classify(t Transition) string {
	switch {
	case len(t.Gained) == 0 && (t.Cause == "admin_to_member" || strings.HasPrefix(t.Cause, "mapped_remove:") ||
		strings.HasPrefix(t.Cause, "admin_decision:") || strings.HasPrefix(t.Cause, "mapped_member:")):
		return "approved-reduction"
	case strings.HasPrefix(t.Cause, "mapped_member:"):
		return "approved-expansion"
	}
	return "UNEXPECTED"
}

func oldActions(u *user, projectID int64, roleOf map[string]*RoleRow) map[string]bool {
	if u.admin {
		return allActions()
	}
	if raw, ok := u.members[projectID]; ok {
		return actionSet(roleOf[strings.ToLower(raw)].Actions)
	}
	return map[string]bool{}
}

// newActions is the effective result of the new model for an active user (all projects are in the default org).
func newActions(userID, projectID int64, orgMembers map[int64]string, decisions map[int64]string, rows map[string]string) map[string]bool {
	if orgMembers[userID] == "admin" {
		return allActions()
	}
	if _, ok := rows[fmt.Sprintf("%d:%d", projectID, userID)]; ok && orgMembers[userID] != "" {
		return actionSet(core.MemberActions)
	}
	return map[string]bool{}
}

func loadUsers(q Reader) ([]*user, map[int64]*user, error) {
	rows, err := q.Query(`SELECT id, email, is_admin = 1, deactivated_at IS NULL FROM users ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	var users []*user
	byID := map[int64]*user{}
	for rows.Next() {
		u := &user{members: map[int64]string{}}
		if err := rows.Scan(&u.id, &u.email, &u.admin, &u.active); err != nil {
			rows.Close()
			return nil, nil, err
		}
		users = append(users, u)
		byID[u.id] = u
	}
	rows.Close()
	mrows, err := q.Query(`SELECT project_id, user_id, role FROM project_members`)
	if err != nil {
		return nil, nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var pid, uid int64
		var role string
		if err := mrows.Scan(&pid, &uid, &role); err != nil {
			return nil, nil, err
		}
		if u := byID[uid]; u != nil {
			u.members[pid] = role
		}
	}
	return users, byID, mrows.Err()
}

func loadProjects(q Reader) ([]project, error) {
	rows, err := q.Query(`SELECT id, key FROM projects ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []project
	for rows.Next() {
		var p project
		if err := rows.Scan(&p.id, &p.key); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func loadRoles(q Reader) ([]RoleRow, error) {
	rows, err := q.Query(`SELECT r.name, r.builtin, a.action FROM roles r LEFT JOIN role_actions a ON a.role = r.name ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoleRow
	for rows.Next() {
		var name string
		var builtin bool
		var act sql.NullString
		if err := rows.Scan(&name, &builtin, &act); err != nil {
			return nil, err
		}
		name = strings.ToLower(name)
		if len(out) == 0 || out[len(out)-1].Name != name {
			out = append(out, RoleRow{Name: name, Builtin: builtin, Actions: []string{}})
		}
		if act.Valid {
			out[len(out)-1].Actions = append(out[len(out)-1].Actions, act.String)
		}
	}
	for i := range out {
		slices.Sort(out[i].Actions)
	}
	return out, rows.Err()
}
