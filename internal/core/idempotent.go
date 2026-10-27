package core

import (
	"context"

	"github.com/kees/atlas/internal/auth"
)

// Idempotent runs f in one transaction: every mutation f makes through the service it gets commits or rolls
// back together. A non-empty key stores out with the commit, so a retry with the same key and request returns
// the first result (replayed) instead of repeating the writes.
func (s *Service) Idempotent(ctx context.Context, a auth.Actor, key string, req, out any, f func(ctx context.Context, s *Service) error) (replayed bool, err error) {
	if len(key) > maxIdempotency {
		return false, invalid("idempotency key is longer than %d characters", maxIdempotency)
	}
	var hash string
	if key != "" {
		if hash, err = requestHash(req); err != nil {
			return false, err
		}
	}
	tx, err := s.db.W.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if key != "" {
		if replayed, err = idemReplay(tx, a, key, hash, out); err != nil || replayed {
			return replayed, err
		}
	}
	bs := *s // shares the database; its tx() joins this transaction
	bs.outer = tx
	if err := f(ctx, &bs); err != nil {
		return false, err
	}
	if key != "" {
		if err := s.idemStore(tx, a, key, hash, out); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if s.notify != nil {
		s.notify()
	}
	return false, nil
}
