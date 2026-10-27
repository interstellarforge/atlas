package core

import (
	"context"
	"database/sql"
	"regexp"
	"strings"

	"github.com/kees/atlas/internal/auth"
)

const (
	NotifyAssigned    = "assigned"
	NotifyMention     = "mention"
	NotifyBlockerDone = "blocker_done"
	NotifyFollowed    = "followed"

	maxNotificationPage = 100
)

// Notice is a notification to create inside the transaction of the mutation that caused it.
// ItemKey is the item it is about; empty means the item of the change.
type Notice struct {
	User    int64
	Type    string
	ItemKey string
	Detail  string
}

// followEvents are the item changes that notify followers.
var followEvents = map[string]bool{"item.updated": true, "item.moved": true, "item.archived": true, "comment.added": true}

// fanOut creates the notifications of one change: the explicit ones, then followers of the item. Nobody is
// notified of their own action, and each person gets at most one notification per item and change.
func (s *Service) fanOut(tx *sql.Tx, a auth.Actor, c change) error {
	if c.ItemKey == "" {
		return nil
	}
	notices := append([]Notice(nil), c.Notify...)
	if followEvents[c.Event] {
		rows, err := tx.Query(`SELECT f.user_id FROM item_follows f JOIN work_items i ON i.id = f.item_id WHERE i.key = ?`, c.ItemKey)
		if err != nil {
			return err
		}
		for rows.Next() {
			var uid int64
			if err := rows.Scan(&uid); err != nil {
				rows.Close()
				return err
			}
			notices = append(notices, Notice{User: uid, Type: NotifyFollowed, Detail: c.Event})
		}
		rows.Close()
	}
	seen := map[[2]any]bool{}
	for _, n := range notices {
		key := n.ItemKey
		if key == "" {
			key = c.ItemKey
		}
		if (a.Kind == auth.KindHuman && n.User == a.UserID) || seen[[2]any{n.User, key}] {
			continue
		}
		seen[[2]any{n.User, key}] = true
		pk, err := projectOfKey(key)
		if err != nil {
			continue
		}
		if ok, err := s.canRead(tx, n.User, pk); err != nil {
			return err
		} else if !ok {
			continue
		}
		now := s.nowMs()
		if _, err := tx.Exec(`INSERT INTO notifications (user_id, type, project, item_key, actor, detail, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			n.User, n.Type, pk, key, a.String(), n.Detail, now); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO change_events (type, project, item_key, actor, ts, target_user) VALUES ('notification.created', ?, ?, ?, ?, ?)`,
			pk, key, a.String(), now, n.User); err != nil {
			return err
		}
	}
	return nil
}

// canRead reports whether a user can see a project: global admin or member.
func (s *Service) canRead(q queryer, userID int64, projectKey string) (bool, error) {
	var ok int
	err := q.QueryRow(`SELECT 1 FROM users u WHERE u.id = ? AND (u.is_admin = 1 OR EXISTS (
		SELECT 1 FROM project_members m JOIN projects p ON p.id = m.project_id WHERE m.user_id = u.id AND p.key = ?))`, userID, projectKey).Scan(&ok)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// statusCategory returns the category (todo, active, completed) of a status id.
func statusCategory(q queryer, id int64) (string, error) {
	var c string
	err := q.QueryRow(`SELECT category FROM project_statuses WHERE id = ?`, id).Scan(&c)
	return c, err
}

// completedNotices tells the assignees of items that this item blocks that the blocker is done.
func completedNotices(tx *sql.Tx, itemID int64, key string) ([]Notice, error) {
	rows, err := tx.Query(`SELECT b.assignee_id, b.key FROM item_links l JOIN work_items b ON b.id = l.to_id
		WHERE l.from_id = ? AND l.type = 'blocks' AND b.assignee_id IS NOT NULL AND b.archived_at IS NULL`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notice
	for rows.Next() {
		var n Notice
		if err := rows.Scan(&n.User, &n.ItemKey); err != nil {
			return nil, err
		}
		n.Type, n.Detail = NotifyBlockerDone, key
		out = append(out, n)
	}
	return out, rows.Err()
}

var mentionRe = regexp.MustCompile(`(?:^|\s)@([A-Za-z0-9._-]{1,64})`)

// mentionNotices finds users named as @handle (the part of their email before the @) in a comment.
func mentionNotices(tx *sql.Tx, body string) ([]Notice, error) {
	var out []Notice
	seen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(body, -1) {
		h := strings.ToLower(m[1])
		if seen[h] {
			continue
		}
		seen[h] = true
		rows, err := tx.Query(`SELECT id FROM users WHERE lower(substr(email, 1, instr(email, '@') - 1)) = ?`, h)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var n Notice
			if err := rows.Scan(&n.User); err != nil {
				rows.Close()
				return nil, err
			}
			n.Type = NotifyMention
			out = append(out, n)
		}
		rows.Close()
	}
	return out, nil
}

// ---- follow ----

func (s *Service) followTx(ctx context.Context, a auth.Actor, key string, on bool) error {
	if !a.Can(auth.ScopeWrite) {
		return ErrForbidden
	}
	pk, err := projectOfKey(key)
	if err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		if _, _, err := s.access(tx, a, pk, roleViewer); err != nil {
			return err
		}
		ref, err := refOf(tx, key)
		if err != nil {
			return err
		}
		if on {
			_, err = tx.Exec(`INSERT OR IGNORE INTO item_follows (item_id, user_id) VALUES (?, ?)`, ref.id, a.UserID)
		} else {
			_, err = tx.Exec(`DELETE FROM item_follows WHERE item_id = ? AND user_id = ?`, ref.id, a.UserID)
		}
		return err
	})
}

// Follow and Unfollow are personal settings: they are not audited or journaled.
func (s *Service) Follow(ctx context.Context, a auth.Actor, key string) error {
	return s.followTx(ctx, a, key, true)
}

func (s *Service) Unfollow(ctx context.Context, a auth.Actor, key string) error {
	return s.followTx(ctx, a, key, false)
}

func (s *Service) IsFollowing(ctx context.Context, a auth.Actor, key string) (bool, error) {
	pk, err := projectOfKey(key)
	if err != nil {
		return false, err
	}
	if _, _, err := s.access(s.db.R, a, pk, roleViewer); err != nil {
		return false, err
	}
	var n int
	err = s.db.R.QueryRow(`SELECT COUNT(*) FROM item_follows f JOIN work_items i ON i.id = f.item_id WHERE i.key = ? AND f.user_id = ?`, key, a.UserID).Scan(&n)
	return n > 0, err
}

// ---- inbox ----

type Notification struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Project   string `json:"project"`
	Item      string `json:"item"`
	Actor     string `json:"actor"`
	Detail    string `json:"detail"`
	CreatedAt int64  `json:"created_at"`
	Read      bool   `json:"read"`
}

type Inbox struct {
	Items  []Notification `json:"items"`
	Unread int            `json:"unread"`
}

// ListNotifications returns the caller's newest notifications and their unread count.
func (s *Service) ListNotifications(ctx context.Context, a auth.Actor, unreadOnly bool, limit int) (Inbox, error) {
	in := Inbox{Items: []Notification{}}
	if !a.Can(auth.ScopeRead) {
		return in, ErrForbidden
	}
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, maxNotificationPage)
	q := `SELECT id, type, project, item_key, actor, detail, created_at, read_at IS NOT NULL FROM notifications WHERE user_id = ?`
	if unreadOnly {
		q += ` AND read_at IS NULL`
	}
	rows, err := s.db.R.Query(q+` ORDER BY id DESC LIMIT ?`, a.UserID, limit)
	if err != nil {
		return in, err
	}
	defer rows.Close()
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Type, &n.Project, &n.Item, &n.Actor, &n.Detail, &n.CreatedAt, &n.Read); err != nil {
			return in, err
		}
		in.Items = append(in.Items, n)
	}
	if err := rows.Err(); err != nil {
		return in, err
	}
	err = s.db.R.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL`, a.UserID).Scan(&in.Unread)
	return in, err
}

// MarkRead marks one notification (id > 0) or all of them (id == 0) as read.
func (s *Service) MarkRead(ctx context.Context, a auth.Actor, id int64) error {
	if !a.Can(auth.ScopeWrite) {
		return ErrForbidden
	}
	return s.tx(func(tx *sql.Tx) error {
		if id == 0 {
			_, err := tx.Exec(`UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, s.nowMs(), a.UserID)
			return err
		}
		res, err := tx.Exec(`UPDATE notifications SET read_at = COALESCE(read_at, ?) WHERE id = ? AND user_id = ?`, s.nowMs(), id, a.UserID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Service) DeleteNotification(ctx context.Context, a auth.Actor, id int64) error {
	if !a.Can(auth.ScopeWrite) {
		return ErrForbidden
	}
	return s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM notifications WHERE id = ? AND user_id = ?`, id, a.UserID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
