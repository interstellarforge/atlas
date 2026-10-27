package core

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/media"
)

const (
	maxAttachmentsPerItem = 50
	maxFilenameLen        = 200
)

// ErrTooLarge is an upload over the configured size or pixel limit (HTTP 413).
var ErrTooLarge = errors.New("too large")

type Attachment struct {
	ID        int64  `json:"id"`
	Item      string `json:"item"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
	URL       string `json:"url"`
	ThumbURL  string `json:"thumb_url"`
	Path      string `json:"-"`
}

// SetMedia enables attachments; without it uploads are refused.
func (s *Service) SetMedia(m *media.Store) { s.media = m }

func attachmentURLs(a *Attachment) {
	a.URL = "/api/v1/attachments/" + itoa(a.ID)
	a.ThumbURL = a.URL + "/thumb"
}

func itoa(n int64) string {
	b := [20]byte{}
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			return string(b[i:])
		}
	}
}

// cleanFilename keeps only the base name and drops control characters, so the stored name is safe to show.
func cleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if len(name) > maxFilenameLen {
		ext := filepath.Ext(name)
		if len(ext) > maxFilenameLen/2 { // an absurd extension is cut like the rest, never a negative slice bound
			ext = ""
		}
		name = name[:maxFilenameLen-len(ext)] + ext
	}
	return name
}

// AddAttachment stores an uploaded image for an item. The upload is checked before it is read, and the
// file is removed again if recording it fails.
func (s *Service) AddAttachment(ctx context.Context, a auth.Actor, key, filename string, r io.Reader) (Attachment, error) {
	if s.media == nil {
		return Attachment{}, rule("uploads are not enabled")
	}
	pk, err := projectOfKey(key)
	if err != nil {
		return Attachment{}, err
	}
	// Check permission and room before spending disk and CPU on the body.
	var n int
	if _, _, err := s.access(s.db.R, a, pk, actItemUpdate); err != nil {
		return Attachment{}, err
	}
	if err := s.db.R.QueryRow(`SELECT COUNT(*) FROM attachments c JOIN work_items i ON i.id = c.item_id WHERE i.key = ?`, key).Scan(&n); err != nil {
		return Attachment{}, err
	}
	if n >= maxAttachmentsPerItem {
		return Attachment{}, rule("an item holds at most %d attachments", maxAttachmentsPerItem)
	}

	name := cleanFilename(filename)
	saved, err := s.media.Save(r, name)
	switch {
	case errors.Is(err, media.ErrTooLarge):
		return Attachment{}, ErrTooLarge
	case errors.Is(err, media.ErrType), errors.Is(err, media.ErrCorrupt):
		return Attachment{}, invalid("%s", err.Error())
	case err != nil:
		return Attachment{}, err
	}

	att := Attachment{Item: key, Filename: name, Mime: saved.Mime, Size: saved.Size, Width: saved.Width, Height: saved.Height,
		CreatedBy: a.String(), Path: saved.Path}
	err = s.tx(func(tx *sql.Tx) error {
		if _, _, err := s.access(tx, a, pk, actItemUpdate); err != nil {
			return err
		}
		ref, err := refOf(tx, key)
		if err != nil {
			return err
		}
		att.CreatedAt = s.nowMs()
		res, err := tx.Exec(`INSERT INTO attachments (item_id, filename, mime, size, width, height, path, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, ref.id, att.Filename, att.Mime, att.Size, att.Width, att.Height, att.Path, att.CreatedBy, att.CreatedAt)
		if err != nil {
			return err
		}
		att.ID, _ = res.LastInsertId()
		return s.journal(ctx, tx, a, change{Operation: "attachment.add", Target: key, Event: "attachment.added", Project: pk, ItemKey: key,
			Changes: map[string]any{"id": att.ID, "filename": att.Filename, "size": att.Size}})
	})
	if err != nil {
		s.media.Remove(saved.Path)
		return Attachment{}, err
	}
	attachmentURLs(&att)
	return att, nil
}

const attachmentSelect = `SELECT c.id, i.key, c.filename, c.mime, c.size, c.width, c.height, c.created_by, c.created_at, c.path
	FROM attachments c JOIN work_items i ON i.id = c.item_id `

func scanAttachment(sc interface{ Scan(...any) error }) (Attachment, error) {
	var a Attachment
	err := sc.Scan(&a.ID, &a.Item, &a.Filename, &a.Mime, &a.Size, &a.Width, &a.Height, &a.CreatedBy, &a.CreatedAt, &a.Path)
	attachmentURLs(&a)
	return a, err
}

func (s *Service) ListAttachments(ctx context.Context, a auth.Actor, key string) ([]Attachment, error) {
	pk, err := projectOfKey(key)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.access(s.db.R, a, pk, actView); err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(attachmentSelect+`WHERE i.key = ? ORDER BY c.id`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		att, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, att)
	}
	return out, rows.Err()
}

// GetAttachment returns one attachment the actor may read; others are reported as not found.
func (s *Service) GetAttachment(ctx context.Context, a auth.Actor, id int64) (Attachment, error) {
	att, err := scanAttachment(s.db.R.QueryRow(attachmentSelect+`WHERE c.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Attachment{}, ErrNotFound
	}
	if err != nil {
		return Attachment{}, err
	}
	pk, err := projectOfKey(att.Item)
	if err != nil {
		return Attachment{}, err
	}
	if _, _, err := s.access(s.db.R, a, pk, actView); err != nil {
		return Attachment{}, err
	}
	return att, nil
}
