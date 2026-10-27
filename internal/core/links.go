package core

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/kees/atlas/internal/auth"
)

var linkTypes = []string{"blocks", "relates", "duplicates"}

type Link struct {
	Type   string `json:"type"`
	Key    string `json:"key"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type Links struct {
	Outgoing []Link `json:"outgoing"` // this item <type> other
	Incoming []Link `json:"incoming"` // other <type> this item
}

type itemRef struct {
	id      int64
	project string
}

func refOf(q queryer, key string) (itemRef, error) {
	pk, err := projectOfKey(key)
	if err != nil {
		return itemRef{}, err
	}
	var r itemRef
	err = q.QueryRow(`SELECT id FROM work_items WHERE key = ?`, key).Scan(&r.id)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	r.project = pk
	return r, err
}

// AddLink links two items. The actor needs write access to the source item and read access to the target.
func (s *Service) AddLink(ctx context.Context, a auth.Actor, fromKey, toKey, typ string) error {
	if !slices.Contains(linkTypes, typ) {
		return invalid("type must be one of blocks, relates, duplicates")
	}
	if fromKey == toKey {
		return invalid("an item cannot be linked to itself")
	}
	return s.tx(func(tx *sql.Tx) error {
		from, err := refOf(tx, fromKey)
		if err != nil {
			return err
		}
		if _, _, err := s.access(tx, a, from.project, roleMember); err != nil {
			return err
		}
		to, err := refOf(tx, toKey)
		if err != nil {
			return err
		}
		if _, _, err := s.access(tx, a, to.project, roleViewer); err != nil {
			return err
		}

		if typ == "blocks" {
			// from would block to; a cycle exists if to already (transitively) blocks from.
			var cyc int
			err := tx.QueryRow(`WITH RECURSIVE r(id) AS (
				SELECT ? UNION SELECT l.to_id FROM item_links l JOIN r ON l.from_id = r.id WHERE l.type = 'blocks')
				SELECT 1 FROM r WHERE id = ?`, to.id, from.id).Scan(&cyc)
			if err == nil {
				return rule("%s already (indirectly) blocks %s; this would create a cycle", toKey, fromKey)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		} else {
			var rev int
			err := tx.QueryRow(`SELECT 1 FROM item_links WHERE from_id = ? AND to_id = ? AND type = ?`, to.id, from.id, typ).Scan(&rev)
			if err == nil {
				return rule("the reverse %s link already exists", typ)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO item_links (from_id, to_id, type, created_at) VALUES (?, ?, ?, ?)`,
			from.id, to.id, typ, s.nowMs()); err != nil {
			if isUnique(err) {
				return rule("link already exists")
			}
			return err
		}
		return s.linkJournal(ctx, tx, a, "link.add", from, fromKey, toKey, typ, to)
	})
}

func (s *Service) RemoveLink(ctx context.Context, a auth.Actor, fromKey, toKey, typ string) error {
	return s.tx(func(tx *sql.Tx) error {
		from, err := refOf(tx, fromKey)
		if err != nil {
			return err
		}
		if _, _, err := s.access(tx, a, from.project, roleMember); err != nil {
			return err
		}
		to, err := refOf(tx, toKey)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM item_links WHERE from_id = ? AND to_id = ? AND type = ?`, from.id, to.id, typ)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.linkJournal(ctx, tx, a, "link.remove", from, fromKey, toKey, typ, to)
	})
}

// linkJournal emits an update event for both ends, since the blocked state of the target changes.
func (s *Service) linkJournal(ctx context.Context, tx *sql.Tx, a auth.Actor, op string, from itemRef, fromKey, toKey, typ string, to itemRef) error {
	changes := map[string]any{"type": typ, "from": fromKey, "to": toKey}
	if err := s.journal(ctx, tx, a, change{Operation: op, Target: fromKey, Event: "item.updated",
		Project: from.project, ItemKey: fromKey, Changes: changes}); err != nil {
		return err
	}
	return s.journal(ctx, tx, a, change{Operation: op, Target: toKey, Event: "item.updated",
		Project: to.project, ItemKey: toKey, Changes: changes})
}

func (s *Service) ListLinks(ctx context.Context, a auth.Actor, key string) (Links, error) {
	pk, err := projectOfKey(key)
	if err != nil {
		return Links{}, err
	}
	if _, _, err := s.access(s.db.R, a, pk, roleViewer); err != nil {
		return Links{}, err
	}
	ref, err := refOf(s.db.R, key)
	if err != nil {
		return Links{}, err
	}
	readable, err := s.readableProjects(s.db.R, a)
	if err != nil {
		return Links{}, err
	}
	out := Links{Outgoing: []Link{}, Incoming: []Link{}}
	for _, dir := range []struct {
		self, other string
		dst         *[]Link
	}{{"from_id", "to_id", &out.Outgoing}, {"to_id", "from_id", &out.Incoming}} {
		rows, err := s.db.R.Query(`SELECT l.type, o.key, o.title, st.name, o.project_id FROM item_links l
			JOIN work_items o ON o.id = l.`+dir.other+` JOIN project_statuses st ON st.id = o.status_id
			WHERE l.`+dir.self+` = ? ORDER BY l.id`, ref.id)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var l Link
			var pid int64
			if err := rows.Scan(&l.Type, &l.Key, &l.Title, &l.Status, &pid); err != nil {
				rows.Close()
				return out, err
			}
			if _, ok := readable[pid]; ok { // never reveal items from projects the actor cannot read
				*dir.dst = append(*dir.dst, l)
			}
		}
		rows.Close()
	}
	return out, nil
}
