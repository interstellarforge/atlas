package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/kees/atlas/internal/core"
)

// Project context (ADR-0015). The words bootstrap and search are reserved as entry keys.
func (s *Server) registerContext(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/projects/{key}/context", protect(s.listContext))
	mux.Handle("POST /api/v1/projects/{key}/context", protect(s.createContext))
	mux.Handle("GET /api/v1/projects/{key}/context/bootstrap", protect(s.contextBootstrap))
	mux.Handle("GET /api/v1/projects/{key}/context/search", protect(s.searchContext))
	mux.Handle("GET /api/v1/projects/{key}/context/{ckey}", protect(s.getContext))
	mux.Handle("PATCH /api/v1/projects/{key}/context/{ckey}", protect(s.patchContext))
	mux.Handle("DELETE /api/v1/projects/{key}/context/{ckey}", protect(s.archiveContext))
	mux.Handle("POST /api/v1/projects/{key}/context/{ckey}/verify", protect(s.verifyContext))
	mux.Handle("POST /api/v1/projects/{key}/context/{ckey}/supersede", protect(s.supersedeContext))
}

func listParam(r *http.Request, name string) []string {
	var out []string
	for _, v := range r.URL.Query()[name] {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// ifMatch reads an expected version from the If-Match header ("3", W/"3" or 3); 0 when absent or not a number.
func ifMatch(r *http.Request) int64 {
	v := strings.Trim(strings.TrimPrefix(r.Header.Get("If-Match"), "W/"), `" `)
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

func contextFilter(w http.ResponseWriter, r *http.Request) (core.ContextFilter, bool) {
	q := r.URL.Query()
	f := core.ContextFilter{Categories: listParam(r, "category"), Status: q.Get("status"), Q: strings.TrimSpace(q.Get("q")),
		Fields: listParam(r, "fields"), Cursor: q.Get("cursor")}
	limit, ok := intParam(w, r, "limit")
	if !ok {
		return f, false
	}
	budget, ok := intParam(w, r, "max_bytes")
	if !ok {
		return f, false
	}
	f.Limit, f.MaxBytes = int(limit), int(budget)
	switch q.Get("pinned") {
	case "":
	case "true", "1":
		t := true
		f.Pinned = &t
	case "false", "0":
		t := false
		f.Pinned = &t
	default:
		writeError(w, http.StatusBadRequest, "invalid", "pinned must be true or false")
		return f, false
	}
	return f, true
}

func (s *Server) listContext(w http.ResponseWriter, r *http.Request) {
	f, ok := contextFilter(w, r)
	if !ok {
		return
	}
	page, err := s.core.ListContext(r.Context(), actor(r), r.PathValue("key"), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) searchContext(w http.ResponseWriter, r *http.Request) {
	f, ok := contextFilter(w, r)
	if !ok {
		return
	}
	if f.Q == "" {
		writeError(w, http.StatusBadRequest, "invalid", "q is required")
		return
	}
	f.Compact, f.MaxBytes = true, core.ClampContextBytes(f.MaxBytes)
	if f.Limit <= 0 {
		f.Limit = 10
	}
	page, err := s.core.ListContext(r.Context(), actor(r), r.PathValue("key"), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) contextBootstrap(w http.ResponseWriter, r *http.Request) {
	f, ok := contextFilter(w, r)
	if !ok {
		return
	}
	b, err := s.core.ContextBootstrapFor(r.Context(), actor(r), r.PathValue("key"), f.Categories, f.Limit, f.Cursor, f.MaxBytes)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) getContext(w http.ResponseWriter, r *http.Request) {
	e, err := s.core.GetContext(r.Context(), actor(r), r.PathValue("key"), r.PathValue("ckey"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatInt(e.Version, 10)+`"`)
	writeJSON(w, http.StatusOK, e)
}

type contextBody struct {
	Key      string                `json:"key"`
	Category *string               `json:"category"`
	Title    *string               `json:"title"`
	Summary  *string               `json:"summary"`
	Body     *string               `json:"body"`
	Pinned   *bool                 `json:"pinned"`
	Sources  *[]core.ContextSource `json:"sources"`
	Version  int64                 `json:"version"`
}

func (b contextBody) input(r *http.Request) core.ContextInput {
	v := b.Version
	if v == 0 {
		v = ifMatch(r)
	}
	return core.ContextInput{Key: b.Key, Category: b.Category, Title: b.Title, Summary: b.Summary, Body: b.Body, Pinned: b.Pinned,
		Sources: b.Sources, ExpectedVersion: v, IdempotencyKey: r.Header.Get("Idempotency-Key")}
}

func (s *Server) createContext(w http.ResponseWriter, r *http.Request) {
	var in contextBody
	if !decode(w, r, &in) {
		return
	}
	if in.Version != 0 {
		writeError(w, http.StatusBadRequest, "invalid", "version is for updates; use PATCH")
		return
	}
	ci := in.input(r)
	res, err := s.core.UpsertContext(r.Context(), actor(r), r.PathValue("key"), ci)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, res)
}

func (s *Server) patchContext(w http.ResponseWriter, r *http.Request) {
	var in contextBody
	if !decode(w, r, &in) {
		return
	}
	if in.Key != "" && in.Key != r.PathValue("ckey") {
		writeError(w, http.StatusBadRequest, "invalid", "key cannot be changed")
		return
	}
	in.Key = r.PathValue("ckey")
	ci := in.input(r)
	if ci.ExpectedVersion <= 0 {
		writeError(w, http.StatusBadRequest, "invalid", "version (body) or If-Match (header) is required")
		return
	}
	// PATCH never creates: an unknown key is a 404.
	if _, err := s.core.GetContext(r.Context(), actor(r), r.PathValue("key"), in.Key); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.core.UpsertContext(r.Context(), actor(r), r.PathValue("key"), ci)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) archiveContext(w http.ResponseWriter, r *http.Request) {
	res, err := s.core.ArchiveContext(r.Context(), actor(r), r.PathValue("key"), r.PathValue("ckey"), ifMatch(r), r.Header.Get("Idempotency-Key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) verifyContext(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64 `json:"version"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	if in.Version == 0 {
		in.Version = ifMatch(r)
	}
	res, err := s.core.VerifyContext(r.Context(), actor(r), r.PathValue("key"), r.PathValue("ckey"), in.Version)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) supersedeContext(w http.ResponseWriter, r *http.Request) {
	var in struct {
		By      string `json:"by"`
		Version int64  `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Version == 0 {
		in.Version = ifMatch(r)
	}
	res, err := s.core.SupersedeContext(r.Context(), actor(r), r.PathValue("key"), r.PathValue("ckey"), in.By, in.Version, r.Header.Get("Idempotency-Key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
