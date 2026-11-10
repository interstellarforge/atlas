package core

import (
	"context"
	"database/sql"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

// Organization settings (ADR-0031 point 9), read live. The organization ADMIN sets them; instance values stay the
// defaults and floors, and a person in several organizations gets the strictest value (longest password, shortest
// session, two-factor if any requires it, tokens only if all allow them, shortest token lifetime).

// OrgSettings is the ADMIN's view. Nil values mean "the instance value applies".
type OrgSettings struct {
	PasswordMinLength *int   `json:"password_min_length"` // >= the instance minimum
	SessionDays       *int   `json:"session_days"`        // 1-90; the effective length is the shorter of this and the instance's
	ProjectCreation   string `json:"project_creation"`    // admins (default) or members
	Require2FA        bool   `json:"require_2fa"`
	TokensAllowed     bool   `json:"tokens_allowed"`           // API tokens, OAuth and MCP access
	TokenMaxDays      *int   `json:"token_max_days"`           // 1-365
	DefaultTemplate   *int   `json:"default_project_template"` // id of a project template applied to every new project; nil = none
	DoneVisibleDays   *int   `json:"done_visible_days"`        // 0-3650, 0 = never hide completed items; nil = the instance value
	JoinLinkEnabled   bool   `json:"join_link_enabled"`
	Instance          struct {
		MinPasswordLength int `json:"min_password_length"`
		SessionDays       int `json:"session_days"`
	} `json:"instance"`
}

func (s *Service) loadOrgSettings(q queryer, orgID int64) (OrgSettings, error) {
	st := OrgSettings{ProjectCreation: "admins", TokensAllowed: true}
	var pw, sd, tm, dv, dt sql.NullInt64
	var pc string
	var f2, ta, jl bool
	err := q.QueryRow(`SELECT password_min_length, session_days, project_creation, require_2fa, tokens_allowed, token_max_days, join_link_enabled, done_visible_days, default_project_template_id
		FROM organization_settings WHERE organization_id = ?`, orgID).Scan(&pw, &sd, &pc, &f2, &ta, &tm, &jl, &dv, &dt)
	if err != nil && err != sql.ErrNoRows {
		return OrgSettings{}, err
	}
	if err == nil {
		st.ProjectCreation, st.Require2FA, st.TokensAllowed, st.JoinLinkEnabled = pc, f2, ta, jl
		st.PasswordMinLength, st.SessionDays, st.TokenMaxDays, st.DoneVisibleDays = nullInt(pw), nullInt(sd), nullInt(tm), nullInt(dv)
		st.DefaultTemplate = nullInt(dt)
	}
	pol := s.Policy()
	st.Instance.MinPasswordLength, st.Instance.SessionDays = pol.MinPasswordLength, pol.SessionDays
	return st, nil
}

func nullInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func (s *Service) GetOrgSettings(ctx context.Context, a auth.Actor, key string) (OrgSettings, error) {
	o, err := s.adminOfOrg(s.db.R, a, key)
	if err != nil {
		return OrgSettings{}, err
	}
	return s.loadOrgSettings(s.db.R, o.ID)
}

// UpdateOrgSettings applies the given keys (JSON null clears a nullable one). Only an explicit ADMIN of the
// organization; the password minimum may not go below the instance's.
func (s *Service) UpdateOrgSettings(ctx context.Context, a auth.Actor, key string, patch map[string]any) (OrgSettings, error) {
	var out OrgSettings
	err := s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, key)
		if err != nil {
			return err
		}
		cur, err := s.loadOrgSettings(tx, o.ID)
		if err != nil {
			return err
		}
		for k, v := range patch {
			switch k {
			case "password_min_length":
				if cur.PasswordMinLength, err = optInt(v, cur.Instance.MinPasswordLength, 128, k); err != nil {
					return err
				}
			case "session_days":
				if cur.SessionDays, err = optInt(v, 1, 90, k); err != nil {
					return err
				}
			case "token_max_days":
				if cur.TokenMaxDays, err = optInt(v, 1, 365, k); err != nil {
					return err
				}
			case "default_project_template":
				if cur.DefaultTemplate, err = optInt(v, 1, 1<<30, k); err != nil {
					return err
				}
				if cur.DefaultTemplate != nil {
					var ok bool
					if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM project_templates WHERE id = ? AND (organization_id IS NULL OR organization_id = ?))`, *cur.DefaultTemplate, o.ID).Scan(&ok); err != nil {
						return err
					}
					if !ok {
						return invalid("unknown project template")
					}
				}
			case "done_visible_days":
				if cur.DoneVisibleDays, err = optInt(v, 0, 3650, k); err != nil {
					return err
				}
			case "project_creation":
				if str, _ := v.(string); str == "admins" || str == "members" {
					cur.ProjectCreation = str
				} else {
					return invalid("project_creation must be admins or members")
				}
			case "require_2fa", "tokens_allowed":
				b, ok := v.(bool)
				if !ok {
					return invalid("%s must be true or false", k)
				}
				if k == "require_2fa" {
					cur.Require2FA = b
				} else {
					cur.TokensAllowed = b
				}
			default:
				return invalid("unknown setting %q", k)
			}
		}
		if _, err := tx.Exec(`INSERT INTO organization_settings (organization_id, password_min_length, session_days, project_creation, require_2fa, tokens_allowed, token_max_days, done_visible_days, default_project_template_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (organization_id) DO UPDATE SET password_min_length = excluded.password_min_length,
			session_days = excluded.session_days, project_creation = excluded.project_creation, require_2fa = excluded.require_2fa,
			tokens_allowed = excluded.tokens_allowed, token_max_days = excluded.token_max_days, done_visible_days = excluded.done_visible_days, default_project_template_id = excluded.default_project_template_id`,
			o.ID, cur.PasswordMinLength, cur.SessionDays, cur.ProjectCreation, cur.Require2FA, cur.TokensAllowed, cur.TokenMaxDays, cur.DoneVisibleDays, cur.DefaultTemplate); err != nil {
			return err
		}
		out = cur
		keys := make([]string, 0, len(patch))
		for k := range patch {
			keys = append(keys, k)
		}
		return s.journal(ctx, tx, a, change{Operation: "org.settings.set", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"keys": strings.Join(keys, ",")}})
	})
	return out, err
}

// optInt reads a JSON number or null within [lo, hi].
func optInt(v any, lo, hi int, name string) (*int, error) {
	if v == nil {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) || int(f) < lo || int(f) > hi {
		return nil, invalid("%s must be a whole number from %d to %d, or null", name, lo, hi)
	}
	n := int(f)
	return &n, nil
}

// orgRules are the strictest values over the active organizations a user belongs to.
type orgRules struct {
	passwordMin, sessionDays, tokenMaxDays int
	require2FA, tokensAllowed              bool
}

func (s *Service) userRules(q queryer, userID int64) orgRules {
	r := orgRules{tokensAllowed: true}
	var f2, ta int
	err := q.QueryRow(`SELECT COALESCE(MAX(x.password_min_length), 0), COALESCE(MIN(x.session_days), 0), COALESCE(MIN(x.token_max_days), 0),
		COALESCE(MAX(x.require_2fa), 0), COALESCE(MIN(x.tokens_allowed), 1)
		FROM organization_memberships m JOIN organizations o ON o.id = m.organization_id AND o.status = 'active'
		JOIN organization_settings x ON x.organization_id = o.id WHERE m.user_id = ?`, userID).
		Scan(&r.passwordMin, &r.sessionDays, &r.tokenMaxDays, &f2, &ta)
	if err != nil { // on a read error only the instance defaults apply
		return orgRules{tokensAllowed: true}
	}
	r.require2FA, r.tokensAllowed = f2 == 1, ta == 1
	return r
}

// installUserPolicy gives the auth service the live organization rules of a user.
func (s *Service) installUserPolicy() {
	s.users.SetUserPolicy(func(userID int64) auth.UserPolicy {
		r := s.userRules(s.db.R, userID)
		return auth.UserPolicy{SessionDays: r.sessionDays, RequireTOTP: r.require2FA, TokensAllowed: r.tokensAllowed, TokenMaxDays: r.tokenMaxDays}
	})
}

// passwordPolicy is the instance policy with the minimum raised to what the user's organizations require.
func (s *Service) passwordPolicy(userID int64) Policy {
	p := s.Policy()
	if m := s.userRules(s.db.R, userID).passwordMin; m > p.MinPasswordLength {
		p.MinPasswordLength = m
	}
	return p
}

// ListOrgAudit is the ADMIN's audit log of their own organization: its own entries and those of its projects and
// their items. Filters as for the instance log (operation prefix, paging).
func (s *Service) ListOrgAudit(ctx context.Context, a auth.Actor, key string, f AuditFilter) ([]AuditEntry, error) {
	o, err := s.adminOfOrg(s.db.R, a, key)
	if err != nil {
		return nil, err
	}
	f.Target = ""
	return s.auditLog(f, o.ID, orgTarget(o.Key))
}
