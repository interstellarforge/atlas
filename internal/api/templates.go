package api

import (
	"net/http"
	"strconv"

	"github.com/kees/atlas/internal/core"
)

func (s *Server) registerTemplates(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/projects/{key}/item-templates", protect(s.listItemTemplates))
	mux.Handle("POST /api/v1/projects/{key}/item-templates", protect(s.saveItemTemplate))
	mux.Handle("DELETE /api/v1/projects/{key}/item-templates/{id}", protect(s.deleteItemTemplate))
	mux.Handle("GET /api/v1/item-templates", protect(s.listInstanceItemTemplates))
	mux.Handle("POST /api/v1/item-templates", protect(s.saveInstanceItemTemplate))
	mux.Handle("DELETE /api/v1/item-templates/{id}", protect(s.deleteInstanceItemTemplate))
	mux.Handle("GET /api/v1/project-templates", protect(s.listProjectTemplates))
	mux.Handle("POST /api/v1/project-templates", protect(s.saveProjectTemplate))
	mux.Handle("DELETE /api/v1/project-templates/{id}", protect(s.deleteProjectTemplate))
}

func (s *Server) listItemTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListItemTemplates(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) saveItemTemplate(w http.ResponseWriter, r *http.Request) {
	var in core.ItemTemplate
	if !decode(w, r, &in) {
		return
	}
	t, err := s.core.SaveItemTemplate(r.Context(), actor(r), r.PathValue("key"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) deleteItemTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid template id")
		return
	}
	if err := s.core.DeleteItemTemplate(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listProjectTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListProjectTemplates(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// saveProjectTemplate stores a template given as JSON (an import) or captured from a project ("from_project").
func (s *Server) saveProjectTemplate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string                    `json:"name"`
		Description string                    `json:"description"`
		FromProject string                    `json:"from_project"`
		Data        *core.ProjectTemplateData `json:"data"`
	}
	if !decode(w, r, &in) {
		return
	}
	var t core.ProjectTemplate
	var err error
	switch {
	case in.FromProject != "":
		t, err = s.core.SaveProjectTemplateFromProject(r.Context(), actor(r), in.Name, in.Description, in.FromProject)
	case in.Data != nil:
		t, err = s.core.SaveProjectTemplate(r.Context(), actor(r), in.Name, in.Description, *in.Data)
	default:
		writeError(w, http.StatusBadRequest, "invalid", "give from_project or data")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) deleteProjectTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid template id")
		return
	}
	if err := s.core.DeleteProjectTemplate(r.Context(), actor(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listInstanceItemTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListInstanceItemTemplates(r.Context(), actor(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) saveInstanceItemTemplate(w http.ResponseWriter, r *http.Request) {
	var in core.ItemTemplate
	if !decode(w, r, &in) {
		return
	}
	t, err := s.core.SaveInstanceItemTemplate(r.Context(), actor(r), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) deleteInstanceItemTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "invalid template id")
		return
	}
	if err := s.core.DeleteInstanceItemTemplate(r.Context(), actor(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
