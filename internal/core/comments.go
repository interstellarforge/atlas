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
		if _, _, err := s.access(tx, a, pk, roleMember); err != nil {
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
		return s.journal(ctx, tx, a, change{Operation: "comment.add", Target: key, Event: "comment.added",
			Project: pk, ItemKey: key, Changes: map[string]any{"comment": c.ID}})
	})
	return c, err
}

// ListComments returns an item's comments, oldest first.
func (s *Service) ListComments(ctx context.Context, a auth.Actor, key string) ([]Comment, error) {
	pk, err := projectOfKey(key)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.access(s.db.R, a, pk, roleViewer); err != nil {
		return nil, err
	}
	rows, err := s.db.R.Query(`SELECT c.id, c.actor, c.body, c.created_at FROM comments c
		JOIN work_items i ON i.id = c.item_id WHERE i.key = ? ORDER BY c.id`, key)
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
