package api

import (
	"net/http"
	"strconv"
)

// Notifications of the signed-in user, and following items.
func (s *Server) registerInbox(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/notifications", protect(s.listNotifications))
	mux.Handle("POST /api/v1/notifications/read", protect(s.readAllNotifications))
	mux.Handle("POST /api/v1/notifications/{id}/read", protect(s.readNotification))
	mux.Handle("DELETE /api/v1/notifications/{id}", protect(s.deleteNotification))
	mux.Handle("GET /api/v1/items/{key}/follow", protect(s.getFollow))
	mux.Handle("PUT /api/v1/items/{key}/follow", protect(s.follow))
	mux.Handle("DELETE /api/v1/items/{key}/follow", protect(s.unfollow))
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	in, err := s.core.ListNotifications(r.Context(), actor(r), q.Get("unread") == "1", limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	if err := s.core.MarkRead(r.Context(), actor(r), 0); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func notificationID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid", "id must be a number")
		return 0, false
	}
	return id, true
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := notificationID(w, r)
	if !ok {
		return
	}
	if err := s.core.MarkRead(r.Context(), actor(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := notificationID(w, r)
	if !ok {
		return
	}
	if err := s.core.DeleteNotification(r.Context(), actor(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getFollow(w http.ResponseWriter, r *http.Request) {
	on, err := s.core.IsFollowing(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"following": on})
}

func (s *Server) follow(w http.ResponseWriter, r *http.Request) {
	if err := s.core.Follow(r.Context(), actor(r), r.PathValue("key")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unfollow(w http.ResponseWriter, r *http.Request) {
	if err := s.core.Unfollow(r.Context(), actor(r), r.PathValue("key")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
