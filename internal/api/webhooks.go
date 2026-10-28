package api

import "net/http"

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListWebhooks(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.core.CreateWebhook(r.Context(), actor(r), r.PathValue("key"), in.URL, in.Events)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) patchWebhook(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Active bool `json:"active"`
	}
	id, ok := milestoneID(w, r)
	if !ok || !decode(w, r, &in) {
		return
	}
	if err := s.core.SetWebhookActive(r.Context(), actor(r), r.PathValue("key"), id, in.Active); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := milestoneID(w, r)
	if !ok {
		return
	}
	if err := s.core.DeleteWebhook(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
