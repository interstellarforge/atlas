package api

import (
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/kees/atlas/internal/core"
)

// Organizations (ADR-0031): own applications for any signed-in account (when the instance allows applying), and the
// SUPERADMIN's own area under /admin/ (protect() refuses everyone else before the body is read).
func (s *Server) registerOrganizations(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/organizations/applications", protect(s.myApplications))
	mux.Handle("POST /api/v1/organizations/applications", protect(s.applyForOrganization))
	mux.Handle("GET /api/v1/admin/organizations", protect(s.adminOrganizations))
	mux.Handle("POST /api/v1/admin/organizations", protect(s.adminCreateOrganization))
	mux.Handle("POST /api/v1/admin/organizations/{key}/{decision}", protect(s.adminArchiveOrganization))
	mux.Handle("GET /api/v1/admin/organization-applications", protect(s.adminApplications))
	mux.Handle("POST /api/v1/admin/organization-applications/{id}/{decision}", protect(s.adminDecideApplication))
	mux.Handle("GET /api/v1/organizations/{key}/invites", protect(s.listOrgInvites))
	mux.Handle("POST /api/v1/organizations/{key}/invites", protect(s.createOrgInvite))
	mux.Handle("DELETE /api/v1/organizations/{key}/invites/{id}", protect(s.revokeOrgInvite))
	mux.Handle("POST /api/v1/organizations/invitations/accept", protect(s.acceptOrgInvite))
	mux.Handle("GET /api/v1/organizations/{key}/domains", protect(s.listDomains))
	mux.Handle("POST /api/v1/organizations/{key}/domains", protect(s.claimDomain))
	mux.Handle("PUT /api/v1/organizations/{key}/domains/{id}", protect(s.setDomainMode))
	mux.Handle("DELETE /api/v1/organizations/{key}/domains/{id}", protect(s.removeDomain))
	mux.Handle("GET /api/v1/admin/organization-domains", protect(s.adminDomainClaims))
	mux.Handle("POST /api/v1/admin/organization-domains/{id}/{decision}", protect(s.adminDecideDomain))
	mux.Handle("POST /api/v1/me/email/verification", protect(s.requestEmailVerification))
	mux.Handle("GET /api/v1/me/join-options", protect(s.joinOptions))
	mux.HandleFunc("POST /api/v1/auth/email-verification", s.verifyEmail)
	mux.Handle("GET /api/v1/organizations/{key}/members", protect(s.listOrgMembers))
	mux.Handle("PUT /api/v1/organizations/{key}/members/{user}", protect(s.setOrgMember))
	mux.Handle("DELETE /api/v1/organizations/{key}/members/{user}", protect(s.removeOrgMember))
	mux.Handle("GET /api/v1/organizations/{key}/members/{user}/access", protect(s.memberAccess))
	mux.Handle("PUT /api/v1/organizations/{key}/members/{user}/projects", protect(s.setMemberProjects))
	mux.Handle("GET /api/v1/me/access", protect(s.myAccess))
	mux.Handle("GET /api/v1/projects/{key}/access", protect(s.projectAccess))
	mux.Handle("GET /api/v1/organizations/{key}/logo", protect(s.getOrgLogo))
	mux.Handle("PUT /api/v1/organizations/{key}/logo", protect(s.putOrgLogo))
	mux.Handle("DELETE /api/v1/organizations/{key}/logo", protect(s.deleteOrgLogo))
	mux.Handle("GET /api/v1/organizations/{key}/templates", protect(s.listOrgTemplates))
	mux.Handle("POST /api/v1/organizations/{key}/item-templates", protect(s.saveOrgItemTemplate))
	mux.Handle("DELETE /api/v1/organizations/{key}/item-templates/{id}", protect(s.deleteOrgItemTemplate))
	mux.Handle("POST /api/v1/organizations/{key}/project-templates", protect(s.saveOrgProjectTemplate))
	mux.Handle("DELETE /api/v1/organizations/{key}/project-templates/{id}", protect(s.deleteOrgProjectTemplate))
	mux.Handle("GET /api/v1/organizations/{key}/settings", protect(s.getOrgSettings))
	mux.Handle("PUT /api/v1/organizations/{key}/settings", protect(s.setOrgSettings))
	mux.Handle("GET /api/v1/organizations/{key}/audit", protect(s.orgAudit))
	mux.Handle("GET /api/v1/organizations/{key}/join-link", protect(s.getJoinLink))
	mux.Handle("PUT /api/v1/organizations/{key}/join-link", protect(s.setJoinLink))
	mux.Handle("GET /api/v1/organizations/{key}/join-requests", protect(s.listJoinRequests))
	mux.Handle("POST /api/v1/organizations/{key}/join-requests", protect(s.requestJoin))
	mux.Handle("POST /api/v1/organizations/{key}/join-requests/{id}/{decision}", protect(s.decideJoinRequest))
	mux.HandleFunc("GET /api/v1/auth/invitations/{token}", s.previewOrgInvite)
	mux.HandleFunc("POST /api/v1/auth/invitations/accept", s.acceptOrgInviteNew)
}

func (s *Server) listOrgInvites(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListOrgInvites(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// createOrgInvite takes {"email", "projects": [keys], "days"}; the token (and link) is returned once.
func (s *Server) createOrgInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string   `json:"email"`
		Projects []string `json:"projects"`
		Days     int      `json:"days"`
	}
	if !decode(w, r, &in) {
		return
	}
	inv, token, err := s.core.CreateOrgInvite(r.Context(), actor(r), r.PathValue("key"), in.Email, in.Projects, in.Days)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"invite": inv, "token": token})
}

func (s *Server) revokeOrgInvite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid invite id")
		return
	}
	if err := s.core.RevokeOrgInvite(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) acceptOrgInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.inviteThrottled(w, r) {
		return
	}
	org, err := s.core.AcceptOrgInvite(r.Context(), actor(r), in.Token)
	s.inviteResult(w, r, org, err)
}

// previewOrgInvite and acceptOrgInviteNew are open to the token holder, so they share the login failure limiter
// against token guessing; every invalid token gives the same answer.
func (s *Server) previewOrgInvite(w http.ResponseWriter, r *http.Request) {
	if s.inviteThrottled(w, r) {
		return
	}
	p, err := s.core.PreviewOrgInvite(r.PathValue("token"))
	if err != nil {
		s.auth.Failed("invite|" + s.clientKey(r))
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) acceptOrgInviteNew(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.inviteThrottled(w, r) {
		return
	}
	org, err := s.core.AcceptOrgInviteNew(r.Context(), in.Token, in.Name, in.Password)
	s.inviteResult(w, r, org, err)
}

func (s *Server) inviteThrottled(w http.ResponseWriter, r *http.Request) bool {
	if !s.auth.Throttled("invite|" + s.clientKey(r)) {
		return false
	}
	w.Header().Set("Retry-After", "900")
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts")
	return true
}

func (s *Server) inviteResult(w http.ResponseWriter, r *http.Request, org string, err error) {
	if err != nil {
		if core.ErrorInfo(err).Code == "rule_violation" {
			s.auth.Failed("invite|" + s.clientKey(r))
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"organization": org})
}

func (s *Server) myApplications(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListMyApplications(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) applyForOrganization(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key  string `json:"key"`
		Name string `json:"name"`
		Note string `json:"note"`
	}
	if !decode(w, r, &in) {
		return
	}
	ap, err := s.core.ApplyForOrganization(r.Context(), actor(r), in.Key, in.Name, in.Note)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ap)
}

func (s *Server) adminOrganizations(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListAdminOrganizations(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// adminCreateOrganization takes {"key", "name", "admin_email"}: the first ADMIN is an existing active account.
func (s *Server) adminCreateOrganization(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key        string `json:"key"`
		Name       string `json:"name"`
		AdminEmail string `json:"admin_email"`
	}
	if !decode(w, r, &in) {
		return
	}
	o, err := s.core.CreateOrganizationFor(r.Context(), actor(r), in.Key, in.Name, in.AdminEmail)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) adminApplications(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListApplications(r.Context(), actor(r), r.URL.Query().Get("status"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// adminArchiveOrganization archives or restores an organization: decision archive or restore (SUPERADMIN).
func (s *Server) adminArchiveOrganization(w http.ResponseWriter, r *http.Request) {
	d := r.PathValue("decision")
	if d != "archive" && d != "restore" {
		writeError(w, http.StatusBadRequest, "invalid", "decision must be archive or restore")
		return
	}
	if err := s.core.SetOrganizationArchived(r.Context(), actor(r), r.PathValue("key"), d == "archive"); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminDecideApplication takes approve (optional {"key"} to change the organization key) or reject (optional {"note"}).
func (s *Server) adminDecideApplication(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid application id")
		return
	}
	var in struct {
		Note string `json:"note"`
		Key  string `json:"key"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	if err := s.core.DecideApplication(r.Context(), actor(r), id, r.PathValue("decision"), in.Note, in.Key); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getJoinLink(w http.ResponseWriter, r *http.Request) {
	on, err := s.core.JoinLinkEnabled(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": on})
}

func (s *Server) setJoinLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.core.SetJoinLink(r.Context(), actor(r), r.PathValue("key"), in.Enabled); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requestJoin always answers 202 for a signed-in human account: an unknown, archived or disabled link, a member, a
// waiting request and a cooldown all look the same.
func (s *Server) requestJoin(w http.ResponseWriter, r *http.Request) {
	if err := s.core.RequestJoin(r.Context(), actor(r), r.PathValue("key")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) listJoinRequests(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListJoinRequests(r.Context(), actor(r), r.PathValue("key"), r.URL.Query().Get("status"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// decideJoinRequest takes approve or reject (optional {"note"}).
func (s *Server) decideJoinRequest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid request id")
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	if err := s.core.DecideJoinRequest(r.Context(), actor(r), r.PathValue("key"), id, r.PathValue("decision"), in.Note); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid id")
	}
	return id, err == nil
}

func (s *Server) listDomains(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListDomains(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// claimDomain takes {"domain", "mode"?: request|auto}; the claim waits for the SUPERADMIN.
func (s *Server) claimDomain(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Domain string `json:"domain"`
		Mode   string `json:"mode"`
	}
	if !decode(w, r, &in) {
		return
	}
	d, err := s.core.ClaimDomain(r.Context(), actor(r), r.PathValue("key"), in.Domain, in.Mode)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) setDomainMode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.core.SetDomainMode(r.Context(), actor(r), r.PathValue("key"), id, in.Mode); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.core.RemoveDomain(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminDomainClaims(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListDomainClaims(r.Context(), actor(r), r.URL.Query().Get("status"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) adminDecideDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.core.DecideDomain(r.Context(), actor(r), id, r.PathValue("decision")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) requestEmailVerification(w http.ResponseWriter, r *http.Request) {
	if err := s.core.RequestEmailVerification(r.Context(), actor(r)); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) joinOptions(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.JoinOptions(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// verifyEmail is open to the link holder, so it shares the login failure limiter against token guessing.
func (s *Server) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.inviteThrottled(w, r) {
		return
	}
	if err := s.core.VerifyEmail(r.Context(), in.Token); err != nil {
		if core.ErrorInfo(err).Code == "rule_violation" {
			s.auth.Failed("invite|" + s.clientKey(r))
		}
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getOrgSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.core.GetOrgSettings(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// setOrgSettings takes any of password_min_length, session_days, token_max_days (number or null), project_creation,
// require_2fa and tokens_allowed.
func (s *Server) setOrgSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if !decode(w, r, &in) {
		return
	}
	st, err := s.core.UpdateOrgSettings(r.Context(), actor(r), r.PathValue("key"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) orgAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	list, err := s.core.ListOrgAudit(r.Context(), actor(r), r.PathValue("key"), core.AuditFilter{Operation: q.Get("operation"), Before: before, Limit: limit})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) listOrgMembers(w http.ResponseWriter, r *http.Request) {
	var list []core.OrgMember
	var err error
	if r.URL.Query().Has("q") { // the one search path for pickers and dialogs: min 2 characters, at most 20 results
		list, err = s.core.SearchOrgMembers(r.Context(), actor(r), r.PathValue("key"), r.URL.Query().Get("q"))
	} else {
		list, err = s.core.ListOrgMembers(r.Context(), actor(r), r.PathValue("key"))
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// setOrgMember takes {"role": "admin"|"user"}; the last active ADMIN cannot be demoted here.
func (s *Server) setOrgMember(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid user id")
		return
	}
	var in struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.core.SetOrgMember(r.Context(), actor(r), r.PathValue("key"), uid, in.Role, false); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeOrgMember(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid user id")
		return
	}
	if err := s.core.RemoveOrgMember(r.Context(), actor(r), r.PathValue("key"), uid, false); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listOrgTemplates(w http.ResponseWriter, r *http.Request) {
	t, err := s.core.ListOrgTemplates(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) saveOrgItemTemplate(w http.ResponseWriter, r *http.Request) {
	var in core.ItemTemplate
	if !decode(w, r, &in) {
		return
	}
	t, err := s.core.SaveOrgItemTemplate(r.Context(), actor(r), r.PathValue("key"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) deleteOrgItemTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.core.DeleteOrgItemTemplate(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// saveOrgProjectTemplate takes {"name","description"?} plus either "data" (a template) or "from_project" (a project key
// of the organization to capture).
func (s *Server) saveOrgProjectTemplate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string                    `json:"name"`
		Description string                    `json:"description"`
		Data        *core.ProjectTemplateData `json:"data"`
		FromProject string                    `json:"from_project"`
	}
	if !decode(w, r, &in) {
		return
	}
	t, err := s.core.SaveOrgProjectTemplate(r.Context(), actor(r), r.PathValue("key"), in.Name, in.Description, in.Data, in.FromProject)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) deleteOrgProjectTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.core.DeleteOrgProjectTemplate(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getOrgLogo(w http.ResponseWriter, r *http.Request) {
	path, err := s.core.OrgLogoThumb(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer f.Close()
	serveImage(w, r, f, "image/jpeg", "logo.jpg")
}

// putOrgLogo takes multipart/form-data with a "file" part, like the profile picture.
func (s *Server) putOrgLogo(w http.ResponseWriter, r *http.Request) {
	s.uploadImage(w, r, func(name string, part io.Reader) error {
		return s.core.SetOrgLogo(r.Context(), actor(r), r.PathValue("key"), name, part)
	})
}

func (s *Server) deleteOrgLogo(w http.ResponseWriter, r *http.Request) {
	if err := s.core.RemoveOrgLogo(r.Context(), actor(r), r.PathValue("key")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// myAccess and memberAccess answer what a person can reach: their own, or a member's for the organization ADMIN.
func (s *Server) myAccess(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.EffectiveAccess(r.Context(), actor(r), "", 0)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"organizations": list})
}

func (s *Server) memberAccess(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid user id")
		return
	}
	list, err := s.core.EffectiveAccess(r.Context(), actor(r), r.PathValue("key"), uid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"organizations": list})
}

// projectAccess lists explicit members and inherited organization ADMINs of a project.
func (s *Server) projectAccess(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ProjectAccess(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	admins, err := s.core.ProjectServerAdmins(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "server_admins": admins})
}

// setMemberProjects takes {"projects": [keys]}: the full set of projects of the organization the member belongs to.
func (s *Server) setMemberProjects(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid user id")
		return
	}
	var in struct {
		Projects []string `json:"projects"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.core.SetMemberProjects(r.Context(), actor(r), r.PathValue("key"), uid, in.Projects); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
