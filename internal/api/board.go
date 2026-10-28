package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/kees/atlas/internal/core"
)

func (s *Server) registerBoard(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/projects/{key}/board", protect(s.getBoard))
	mux.Handle("GET /api/v1/projects/{key}/board/columns/{status}", protect(s.getBoardColumn))
	mux.Handle("GET /api/v1/board", protect(s.getCrossBoard))
	mux.Handle("POST /api/v1/items/{key}/move", protect(s.moveItem))
}

// boardFilter reads the filters shared by board endpoints from the query string.
func (s *Server) boardFilter(w http.ResponseWriter, r *http.Request) (core.BoardFilter, bool) {
	q := r.URL.Query()
	f := core.BoardFilter{
		Type: q.Get("type"), Priority: q.Get("priority"), Label: q.Get("label"), Milestone: q.Get("milestone"), Q: q.Get("q"),
		IncludeArchived: q.Get("archived") == "1" || q.Get("archived") == "true",
		Fields:          fieldsParam(r),
	}
	if hideOldDone(q) {
		f.DoneBefore = s.core.DoneCutoff()
	}
	if v := q.Get("assignee"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid", "assignee must be a number")
			return f, false
		}
		f.Assignee = &n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid", "limit must be a number")
			return f, false
		}
		f.Limit = n
	}
	return f, true
}

func (s *Server) getBoard(w http.ResponseWriter, r *http.Request) {
	f, ok := s.boardFilter(w, r)
	if !ok {
		return
	}
	b, err := s.core.Board(r.Context(), actor(r), r.PathValue("key"), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) getBoardColumn(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("status"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid status id")
		return
	}
	f, ok := s.boardFilter(w, r)
	if !ok {
		return
	}
	col, err := s.core.BoardColumn(r.Context(), actor(r), r.PathValue("key"), id, f, r.URL.Query().Get("cursor"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, col)
}

// getCrossBoard groups several projects by status category: GET /board?projects=EXO,ATL (default: all readable).
func (s *Server) getCrossBoard(w http.ResponseWriter, r *http.Request) {
	f, ok := s.boardFilter(w, r)
	if !ok {
		return
	}
	var projects []string
	if v := r.URL.Query().Get("projects"); v != "" {
		projects = strings.Split(v, ",")
	}
	b, err := s.core.CrossBoard(r.Context(), actor(r), projects, f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// moveItem changes status and/or position: {"version", "status"?, "after"?, "before"?}.
func (s *Server) moveItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64  `json:"version"`
		Status  string `json:"status"`
		After   string `json:"after"`
		Before  string `json:"before"`
	}
	if !decode(w, r, &in) {
		return
	}
	it, err := s.core.MoveItem(r.Context(), actor(r), r.PathValue("key"), in.Version,
		core.MoveSpec{Status: in.Status, After: in.After, Before: in.Before})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// hideOldDone is set by the web app so lists and boards leave out long-finished items; API and MCP clients get everything.
func hideOldDone(q url.Values) bool {
	return q.Get("hide_old_done") == "1" || q.Get("hide_old_done") == "true"
}
