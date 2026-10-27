package core

import (
	"context"
	"database/sql"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

const maxCommentLen = 10000

type Comment struct {
	ID        int64  `json:"id"`
	Actor     string `json:"actor"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at"`
}

// AddComment appends a comment. It does not change the item, so its version stays the same.
func (s *Service) AddComment(ctx context.Context, a auth.Actor, key, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > maxCommentLen {
		return Comment{}, invalid("comment is required (max %d characters)", maxCommentLen)
	}
	pk, err := projectOfKey(key)
	if err != nil {
		return Comment{}, err
	}
	c := Comment{Actor: a.String(), Body: body}
	err = s.tx(func(tx *sql.Tx) error {
		if _, _, err := s.access(tx, a, pk, actComment); err != nil {
			return err
		}
		var itemID int64
		if err := tx.QueryRow(`SELECT id FROM work_items WHERE key = ?`, key).Scan(&itemID); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			return err
		}
		c.CreatedAt = s.nowMs()
		res, err := tx.Exec(`INSERT INTO comments (item_id, actor, body, created_at) VALUES (?, ?, ?, ?)`, itemID, c.Actor, body, c.CreatedAt)
		if err != nil {
			return err
		}
		c.ID, _ = res.LastInsertId()
		notices, err := mentionNotices(tx, body)
		if err != nil {
			return err
		}
		for i := range notices {
			notices[i].Detail = excerpt(body)
		}
		return s.journal(ctx, tx, a, change{Operation: "comment.add", Target: key, Event: "comment.added",
			Project: pk, ItemKey: key, Changes: map[string]any{"comment": c.ID}, Notify: notices})
	})
	return c, err
}

// maxComments bounds one comment read; longer threads show their newest comments.
const maxComments = 500

// ListComments returns an item's newest comments (at most maxComments), oldest first.
func (s *Service) ListComments(ctx context.Context, a auth.Actor, key string) ([]Comment, error) {
	return s.ListRecentComments(ctx, a, key, maxComments)
}

// ListRecentComments returns the newest n comments of an item, oldest first.
func (s *Service) ListRecentComments(ctx context.Context, a auth.Actor, key string, n int) ([]Comment, error) {
	pk, err := projectOfKey(key)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.access(s.db.R, a, pk, actView); err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT id, actor, body, created_at FROM (SELECT c.id, c.actor, c.body, c.created_at FROM comments c
		JOIN work_items i ON i.id = c.item_id WHERE i.key = ? ORDER BY c.id DESC LIMIT ?) ORDER BY id`, key, min(n, maxComments))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.Actor, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// excerpt is the start of a comment, short enough for a notification.
func excerpt(body string) string {
	r := []rune(strings.Join(strings.Fields(body), " "))
	if len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return string(r)
}
