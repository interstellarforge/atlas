package api

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"

	"github.com/kees/atlas/internal/core"
)

func (s *Server) registerInsight(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/projects/{key}/progress", protect(s.getProgress))
	mux.Handle("GET /api/v1/projects/{key}/timeline", protect(s.getTimeline))
	mux.Handle("GET /api/v1/projects/{key}/activity", protect(s.getActivity))
	mux.Handle("PUT /api/v1/projects/{key}/time-tracking", protect(s.putTimeTracking))
	mux.Handle("GET /api/v1/projects/{key}/time", protect(s.getTimeSummary))
	mux.Handle("GET /api/v1/projects/{key}/time/export", protect(s.exportTime))
	mux.Handle("PATCH /api/v1/projects/{key}/time/{id}", protect(s.patchTime))
	mux.Handle("DELETE /api/v1/projects/{key}/time/{id}", protect(s.deleteTime))
	mux.Handle("GET /api/v1/items/{key}/time", protect(s.listItemTime))
	mux.Handle("POST /api/v1/items/{key}/time", protect(s.logTime))
}

// intParam reads an optional integer query parameter; ok is false after answering 400.
func intParam(w http.ResponseWriter, r *http.Request, name string) (n int64, ok bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", name+" must be a number")
		return 0, false
	}
	return n, true
}

func (s *Server) getProgress(w http.ResponseWriter, r *http.Request) {
	p, err := s.core.ProjectProgress(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) getTimeline(w http.ResponseWriter, r *http.Request) {
	from, ok := intParam(w, r, "from")
	if !ok {
		return
	}
	to, ok := intParam(w, r, "to")
	if !ok {
		return
	}
	limit, ok := intParam(w, r, "limit")
	if !ok {
		return
	}
	t, err := s.core.ProjectTimeline(r.Context(), actor(r), r.PathValue("key"), from, to, int(limit))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) getActivity(w http.ResponseWriter, r *http.Request) {
	before, ok := intParam(w, r, "before")
	if !ok {
		return
	}
	limit, ok := intParam(w, r, "limit")
	if !ok {
		return
	}
	list, err := s.core.ProjectActivity(r.Context(), actor(r), r.PathValue("key"), before, int(limit))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) putTimeTracking(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid", "enabled is required")
		return
	}
	if err := s.core.SetTimeTracking(r.Context(), actor(r), r.PathValue("key"), *in.Enabled); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func timeFilter(w http.ResponseWriter, r *http.Request) (core.TimeFilter, bool) {
	q := r.URL.Query()
	f := core.TimeFilter{From: q.Get("from"), To: q.Get("to"), Item: strings.ToUpper(q.Get("item")), Period: q.Get("period")}
	if q.Get("user") != "" {
		u, ok := intParam(w, r, "user")
		if !ok {
			return f, false
		}
		f.UserID = &u
	}
	return f, true
}

func (s *Server) getTimeSummary(w http.ResponseWriter, r *http.Request) {
	f, ok := timeFilter(w, r)
	if !ok {
		return
	}
	sum, err := s.core.TimeSummary(r.Context(), actor(r), r.PathValue("key"), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

// csvCell neutralises spreadsheet formulas in user-provided text.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func (s *Server) exportTime(w http.ResponseWriter, r *http.Request) {
	f, ok := timeFilter(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	list, err := s.core.ExportTime(r.Context(), actor(r), key, f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+key+`-time.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"date", "item", "title", "person", "status", "minutes", "note"})
	for _, e := range list {
		_ = cw.Write([]string{e.SpentOn, e.Item, csvCell(e.Title), csvCell(e.User), csvCell(e.Status), strconv.Itoa(e.Minutes), csvCell(e.Note)})
	}
	cw.Flush()
}

func (s *Server) listItemTime(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListItemTime(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) logTime(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Duration string `json:"duration"`
		Date     string `json:"date"`
		Note     string `json:"note"`
	}
	if !decode(w, r, &in) {
		return
	}
	e, err := s.core.LogTime(r.Context(), actor(r), r.PathValue("key"), in.Duration, in.Date, in.Note)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) patchTime(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	id, ok := milestoneID(w, r)
	if !ok || !decode(w, r, &patch) {
		return
	}
	e, err := s.core.UpdateTime(r.Context(), actor(r), r.PathValue("key"), id, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) deleteTime(w http.ResponseWriter, r *http.Request) {
	id, ok := milestoneID(w, r)
	if !ok {
		return
	}
	if err := s.core.DeleteTime(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
