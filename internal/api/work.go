package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
)

func actor(r *http.Request) auth.Actor {
	a, _ := auth.ActorFrom(r.Context())
	return a
}

// fail maps core errors to the status codes and error codes of docs/api-contract.md.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var inv *core.InvalidError
	var rule *core.RuleError
	var conflict *core.ConflictError
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, core.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "not allowed")
	case errors.Is(err, core.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "file exceeds the upload limits")
	case errors.As(err, &inv):
		writeError(w, http.StatusBadRequest, "invalid", inv.Msg)
	case errors.As(err, &rule):
		writeError(w, http.StatusUnprocessableEntity, "rule_violation", rule.Msg)
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{
			"code": "conflict", "message": conflict.Msg, "current": conflict.Current}})
	default:
		s.internal(w, r, err)
	}
}

func (s *Server) registerWork(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/users", protect(s.listUsers))
	mux.Handle("POST /api/v1/users", protect(s.createUser))

	mux.Handle("GET /api/v1/projects", protect(s.listProjects))
	mux.Handle("POST /api/v1/projects", protect(s.createProject))
	mux.Handle("GET /api/v1/projects/{key}", protect(s.getProject))
	mux.Handle("PATCH /api/v1/projects/{key}", protect(s.patchProject))

	mux.Handle("GET /api/v1/projects/{key}/workflow", protect(s.getWorkflow))
	mux.Handle("PUT /api/v1/projects/{key}/workflow", protect(s.putTransitions))
	mux.Handle("POST /api/v1/projects/{key}/statuses", protect(s.addStatus))
	mux.Handle("PATCH /api/v1/projects/{key}/statuses/{id}", protect(s.patchStatus))
	mux.Handle("DELETE /api/v1/projects/{key}/statuses/{id}", protect(s.deleteStatus))
	mux.Handle("POST /api/v1/projects/{key}/custom-fields", protect(s.addCustomField))
	mux.Handle("DELETE /api/v1/projects/{key}/custom-fields/{name}", protect(s.deleteCustomField))

	mux.Handle("GET /api/v1/projects/{key}/members", protect(s.listMembers))
	mux.Handle("PUT /api/v1/projects/{key}/members/{user}", protect(s.setMember))
	mux.Handle("DELETE /api/v1/projects/{key}/members/{user}", protect(s.removeMember))

	mux.Handle("GET /api/v1/items", protect(s.listItems))
	mux.Handle("POST /api/v1/items", protect(s.createItem))
	mux.Handle("GET /api/v1/items/{key}", protect(s.getItem))
	mux.Handle("PATCH /api/v1/items/{key}", protect(s.patchItem))
	mux.Handle("GET /api/v1/items/{key}/links", protect(s.listLinks))
	mux.Handle("POST /api/v1/items/{key}/links", protect(s.addLink))
	mux.Handle("DELETE /api/v1/items/{key}/links/{type}/{target}", protect(s.removeLink))
	mux.Handle("GET /api/v1/projects/{key}/milestones", protect(s.listMilestones))
	mux.Handle("POST /api/v1/projects/{key}/milestones", protect(s.createMilestone))
	mux.Handle("PATCH /api/v1/projects/{key}/milestones/{id}", protect(s.patchMilestone))
	mux.Handle("DELETE /api/v1/projects/{key}/milestones/{id}", protect(s.deleteMilestone))
	s.registerNotes(mux, protect)
	s.registerInbox(mux, protect)
}

// ---- users, projects, members ----

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.core.ListUsers(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": users})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := s.core.CreateUser(r.Context(), actor(r), in.Email, in.Name, in.Password, in.IsAdmin)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.core.ListProjects(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": ps})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, err := s.core.CreateProject(r.Context(), actor(r), in.Key, in.Name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.core.GetProject(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) patchProject(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if !decode(w, r, &patch) {
		return
	}
	p, err := s.core.UpdateProject(r.Context(), actor(r), r.PathValue("key"), patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	ms, err := s.core.ListMembers(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": ms})
}

func (s *Server) setMember(w http.ResponseWriter, r *http.Request) {
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
	if err := s.core.SetMember(r.Context(), actor(r), r.PathValue("key"), uid, in.Role); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid user id")
		return
	}
	if err := s.core.RemoveMember(r.Context(), actor(r), r.PathValue("key"), uid); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- workflow ----

func (s *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	wf, err := s.core.GetWorkflow(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

// putTransitions replaces the transition rules; an empty list allows every status change.
func (s *Server) putTransitions(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Transitions []core.Transition `json:"transitions"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.core.SetTransitions(r.Context(), actor(r), r.PathValue("key"), in.Transitions); err != nil {
		s.fail(w, r, err)
		return
	}
	s.getWorkflow(w, r)
}

func (s *Server) addStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Category string `json:"category"`
		WIPLimit *int   `json:"wip_limit"`
	}
	if !decode(w, r, &in) {
		return
	}
	st, err := s.core.AddStatus(r.Context(), actor(r), r.PathValue("key"), in.Name, in.Category, in.WIPLimit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) patchStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid status id")
		return
	}
	var patch map[string]any
	if !decode(w, r, &patch) {
		return
	}
	st, err := s.core.UpdateStatus(r.Context(), actor(r), r.PathValue("key"), id, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) deleteStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid status id")
		return
	}
	if err := s.core.DeleteStatus(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addCustomField(w http.ResponseWriter, r *http.Request) {
	var f core.CustomField
	if !decode(w, r, &f) {
		return
	}
	if err := s.core.AddCustomField(r.Context(), actor(r), r.PathValue("key"), f); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) deleteCustomField(w http.ResponseWriter, r *http.Request) {
	if err := s.core.DeleteCustomField(r.Context(), actor(r), r.PathValue("key"), r.PathValue("name")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- items ----

func fieldsParam(r *http.Request) []string {
	if v := r.URL.Query().Get("fields"); v != "" {
		return strings.Split(v, ",")
	}
	return nil
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := core.ItemFilter{
		Project: q.Get("project"), Type: q.Get("type"), Status: q.Get("status"), Priority: q.Get("priority"),
		Label: q.Get("label"), Milestone: q.Get("milestone"), Parent: q.Get("parent"), Under: q.Get("under"), Q: q.Get("q"),
		Category: q.Get("category"), Sort: q.Get("sort"),
		IncludeArchived: q.Get("archived") == "1" || q.Get("archived") == "true",
		Cursor:          q.Get("cursor"), Fields: fieldsParam(r),
	}
	if v := q.Get("projects"); v != "" {
		f.Projects = strings.Split(v, ",")
	}
	num := func(name string) (*int64, bool) {
		v := q.Get(name)
		if v == "" {
			return nil, true
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid", name+" must be a number")
			return nil, false
		}
		return &n, true
	}
	var ok bool
	if f.Assignee, ok = num("assignee"); !ok {
		return
	}
	if f.UpdatedSince, ok = num("updated_since"); !ok {
		return
	}
	if lim, ok := num("limit"); !ok {
		return
	} else if lim != nil {
		f.Limit = int(*lim)
	}
	page, err := s.core.ListItems(r.Context(), actor(r), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project     string         `json:"project"`
		Type        string         `json:"type"`
		Title       string         `json:"title"`
		Description string         `json:"description"`
		Parent      string         `json:"parent"`
		Status      string         `json:"status"`
		Priority    string         `json:"priority"`
		Assignee    *int64         `json:"assignee"`
		DueAt       *int64         `json:"due_at"`
		Labels      []string       `json:"labels"`
		CustomData  map[string]any `json:"custom_data"`
		Milestone   string         `json:"milestone"`
	}
	if !decode(w, r, &in) {
		return
	}
	it, err := s.core.CreateItem(r.Context(), actor(r), core.CreateItem(in))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, it)
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	it, err := s.core.GetItem(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if fields := fieldsParam(r); fields != nil {
		m, err := core.Select(it, fields)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, m)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// patchItem needs the current version, in the body ("version") or the If-Match header.
func (s *Server) patchItem(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if !decode(w, r, &patch) {
		return
	}
	var version int64
	if v, ok := patch["version"]; ok {
		f, isNum := v.(float64)
		if !isNum {
			writeError(w, http.StatusBadRequest, "invalid", "version must be a number")
			return
		}
		version = int64(f)
		delete(patch, "version")
	} else if h := strings.Trim(r.Header.Get("If-Match"), `"`); h != "" {
		n, err := strconv.ParseInt(h, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid", "If-Match must be the item version")
			return
		}
		version = n
	} else {
		writeError(w, http.StatusBadRequest, "invalid", "version is required (body or If-Match)")
		return
	}
	it, err := s.core.UpdateItem(r.Context(), actor(r), r.PathValue("key"), version, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) listLinks(w http.ResponseWriter, r *http.Request) {
	ls, err := s.core.ListLinks(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ls)
}

func (s *Server) addLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Type   string `json:"type"`
		Target string `json:"target"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.core.AddLink(r.Context(), actor(r), r.PathValue("key"), in.Target, in.Type); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeLink(w http.ResponseWriter, r *http.Request) {
	if err := s.core.RemoveLink(r.Context(), actor(r), r.PathValue("key"), r.PathValue("target"), r.PathValue("type")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
