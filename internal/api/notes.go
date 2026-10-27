package api

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"strconv"

	"github.com/kees/atlas/internal/core"
)

// Comments and checklists of an item.
func (s *Server) registerNotes(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/items/{key}/comments", protect(s.listComments))
	mux.Handle("POST /api/v1/items/{key}/comments", protect(s.addComment))
	mux.Handle("GET /api/v1/items/{key}/checklist", protect(s.listChecklist))
	mux.Handle("POST /api/v1/items/{key}/checklist", protect(s.addChecklistItem))
	mux.Handle("PATCH /api/v1/items/{key}/checklist/{id}", protect(s.patchChecklistItem))
	mux.Handle("DELETE /api/v1/items/{key}/checklist/{id}", protect(s.deleteChecklistItem))
	mux.Handle("GET /api/v1/items/{key}/attachments", protect(s.listAttachments))
	mux.Handle("POST /api/v1/items/{key}/attachments", protect(s.uploadAttachment))
	mux.Handle("GET /api/v1/attachments/{id}", protect(s.downloadAttachment))
	mux.Handle("GET /api/v1/attachments/{id}/thumb", protect(s.attachmentThumb))
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	cs, err := s.core.ListComments(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": cs})
}

func (s *Server) addComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &in) {
		return
	}
	c, err := s.core.AddComment(r.Context(), actor(r), r.PathValue("key"), in.Body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) listChecklist(w http.ResponseWriter, r *http.Request) {
	items, err := s.core.ListChecklist(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) addChecklistItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &in) {
		return
	}
	c, err := s.core.AddChecklistItem(r.Context(), actor(r), r.PathValue("key"), in.Text)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func checklistID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "id must be a number")
		return 0, false
	}
	return id, true
}

func (s *Server) patchChecklistItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text *string `json:"text"`
		Done *bool   `json:"done"`
	}
	id, ok := checklistID(w, r)
	if !ok || !decode(w, r, &in) {
		return
	}
	c, err := s.core.UpdateChecklistItem(r.Context(), actor(r), r.PathValue("key"), id, core.ChecklistPatch{Text: in.Text, Done: in.Done})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	id, ok := checklistID(w, r)
	if !ok {
		return
	}
	if err := s.core.RemoveChecklistItem(r.Context(), actor(r), r.PathValue("key"), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listAttachments(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.ListAttachments(r.Context(), actor(r), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// uploadAttachment reads one multipart file part as a stream: the body never sits in memory and is
// cut off at the size limit plus the multipart framing.
func (s *Server) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUpload+maxBodyBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "expected multipart/form-data with a file part")
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeError(w, http.StatusRequestEntityTooLarge, "too_large", "file exceeds the upload limits")
			} else {
				writeError(w, http.StatusBadRequest, "invalid", "missing file part")
			}
			return
		}
		if part.FormName() != "file" {
			continue
		}
		att, err := s.core.AddAttachment(r.Context(), actor(r), r.PathValue("key"), part.FileName(), part)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				err = core.ErrTooLarge
			}
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, att)
		return
	}
}

func (s *Server) attachment(w http.ResponseWriter, r *http.Request) (core.Attachment, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return core.Attachment{}, false
	}
	att, err := s.core.GetAttachment(r.Context(), actor(r), id)
	if err != nil {
		s.fail(w, r, err)
		return att, false
	}
	return att, true
}

// serveImage streams a file with headers that keep the browser from treating it as anything but an image.
func serveImage(w http.ResponseWriter, r *http.Request, f *os.File, mimeType, filename string) {
	st, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	h := w.Header()
	h.Set("Content-Type", mimeType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filename}))
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func (s *Server) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	att, ok := s.attachment(w, r)
	if !ok {
		return
	}
	f, err := s.media.Open(att.Path)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer f.Close()
	serveImage(w, r, f, att.Mime, att.Filename)
}

func (s *Server) attachmentThumb(w http.ResponseWriter, r *http.Request) {
	att, ok := s.attachment(w, r)
	if !ok {
		return
	}
	path, err := s.media.Thumb(att.Path)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer f.Close()
	serveImage(w, r, f, "image/jpeg", att.Filename)
}
