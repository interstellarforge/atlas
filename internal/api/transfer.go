package api

import (
	"encoding/json"
	"net/http"

	"github.com/kees/atlas/internal/core"
)

const maxImportBytes = 32 << 20

func (s *Server) registerTransfer(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/projects/{key}/export", protect(s.exportProject))
	mux.Handle("POST /api/v1/projects/{key}/import", protect(s.importItems))
}

// exportProject streams a project as JSON (default) or CSV (?format=csv) download.
func (s *Server) exportProject(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	exp, err := s.core.ExportProject(r.Context(), actor(r), key)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+key+`.csv"`)
		_ = exp.WriteCSV(w)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+key+`.json"`)
	writeJSON(w, http.StatusOK, exp)
}

// importItems imports into the project named in the path, which is the explicit project mapping: keys
// in the file are source keys and the report maps them to the new keys. ?dry_run=1 validates only.
func (s *Server) importItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	var rows []core.ImportRow
	var err error
	if q.Get("format") == "csv" {
		rows, err = core.ParseImportCSV(r.Body)
	} else {
		var doc struct {
			Items []core.ImportRow `json:"items"`
		}
		if err = json.NewDecoder(r.Body).Decode(&doc); err != nil {
			writeError(w, http.StatusBadRequest, "invalid", "malformed JSON body; expected {\"items\": [...]}")
			return
		}
		rows = doc.Items
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rep, err := s.core.ImportItems(r.Context(), actor(r), r.PathValue("key"), rows, q.Get("dry_run") == "1", q.Get("allow_duplicates") == "1")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}
