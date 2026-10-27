package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kees/atlas/internal/auth"
)

const (
	MaxBatch        = 25
	idempotencyTTL  = 24 * 60 * 60 * 1000 // ms
	maxIdempotency  = 128
	batchItemFields = "labels"
)

// BatchOp is one mutation of a batch: create, update or move.
type BatchOp struct {
	Op      string         `json:"op"`
	Key     string         `json:"key,omitempty"`
	Version int64          `json:"version,omitempty"`
	Create  CreateItem     `json:"create"`
	Patch   map[string]any `json:"patch,omitempty"`
	Move    MoveSpec       `json:"move"`
}

type BatchError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Current map[string]any `json:"current,omitempty"`
}

type BatchResult struct {
	Item  map[string]any `json:"item,omitempty"`
	Error *BatchError    `json:"error,omitempty"`
}

type BatchOutcome struct {
	BatchID string        `json:"batch_id"`
	Results []BatchResult `json:"results"`
	// Replayed is set when an earlier result for the same idempotency key was returned.
	Replayed bool `json:"replayed,omitempty"`
}

// OpError reports which operation made an atomic batch fail; nothing was applied.
type OpError struct {
	Index int
	Err   error
}

func (e *OpError) Error() string { return fmt.Sprintf("operation %d: %s", e.Index, e.Err) }
func (e *OpError) Unwrap() error { return e.Err }

// ErrorInfo maps a core error to the stable error codes of the API contract.
func ErrorInfo(err error) BatchError {
	var inv *InvalidError
	var r *RuleError
	var c *ConflictError
	switch {
	case errors.Is(err, ErrNotFound):
		return BatchError{Code: "not_found", Message: "not found"}
	case errors.Is(err, ErrForbidden):
		return BatchError{Code: "forbidden", Message: "not allowed"}
	case errors.As(err, &inv):
		return BatchError{Code: "invalid", Message: inv.Msg}
	case errors.As(err, &r):
		return BatchError{Code: "rule_violation", Message: r.Msg}
	case errors.As(err, &c):
		return BatchError{Code: "conflict", Message: c.Msg, Current: c.Current}
	}
	return BatchError{Code: "internal", Message: "internal error"}
}

// Batch runs up to MaxBatch operations in one transaction. Atomic (partial=false): the first failure
// rolls everything back and is returned as an *OpError. Partial: each operation is isolated in a
// savepoint and reports its own result. A non-empty idemKey makes a retry return the first result.
func (s *Service) Batch(ctx context.Context, a auth.Actor, ops []BatchOp, partial bool, idemKey string) (BatchOutcome, error) {
	if len(ops) == 0 || len(ops) > MaxBatch {
		return BatchOutcome{}, invalid("a batch needs 1-%d operations", MaxBatch)
	}
	if len(idemKey) > maxIdempotency {
		return BatchOutcome{}, invalid("idempotency key is longer than %d characters", maxIdempotency)
	}
	reqJSON, err := json.Marshal(struct {
		Ops     []BatchOp
		Partial bool
	}{ops, partial})
	if err != nil {
		return BatchOutcome{}, err
	}
	sum := sha256.Sum256(reqJSON)
	hash := hex.EncodeToString(sum[:])

	var out BatchOutcome
	tx, err := s.db.W.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	if idemKey != "" {
		var storedHash, stored string
		err := tx.QueryRow(`SELECT request_hash, result FROM idempotency_records WHERE actor = ? AND key = ?`,
			a.String(), idemKey).Scan(&storedHash, &stored)
		switch {
		case err == nil:
			if storedHash != hash {
				return out, invalid("idempotency key was already used for a different request")
			}
			if err := json.Unmarshal([]byte(stored), &out); err != nil {
				return out, err
			}
			out.Replayed = true
			return out, nil
		case !errors.Is(err, sql.ErrNoRows):
			return out, err
		}
	}

	var idb [8]byte
	if _, err := rand.Read(idb[:]); err != nil {
		return out, err
	}
	out.BatchID = hex.EncodeToString(idb[:])
	bs := *s // shares the database; its tx() joins the batch transaction through savepoints
	bs.outer = tx
	bctx := WithBatch(ctx, out.BatchID)

	out.Results = make([]BatchResult, len(ops))
	for i, op := range ops {
		if _, err := tx.Exec(`SAVEPOINT op`); err != nil {
			return out, err
		}
		it, opErr := bs.runOp(bctx, a, op)
		if opErr != nil {
			if _, err := tx.Exec(`ROLLBACK TO op`); err != nil {
				return out, err
			}
		}
		if _, err := tx.Exec(`RELEASE op`); err != nil {
			return out, err
		}
		if opErr == nil {
			m, err := Select(it, append(append([]string{}, defaultFields...), batchItemFields))
			if err != nil {
				return out, err
			}
			out.Results[i].Item = m
			continue
		}
		info := ErrorInfo(opErr)
		if info.Code == "internal" {
			return out, opErr
		}
		if !partial {
			return BatchOutcome{}, &OpError{Index: i, Err: opErr}
		}
		out.Results[i].Error = &info
	}

	if idemKey != "" {
		body, err := json.Marshal(out)
		if err != nil {
			return out, err
		}
		now := s.nowMs()
		if _, err := tx.Exec(`DELETE FROM idempotency_records WHERE created_at < ?`, now-idempotencyTTL); err != nil {
			return out, err
		}
		if _, err := tx.Exec(`INSERT INTO idempotency_records (actor, key, request_hash, result, created_at) VALUES (?, ?, ?, ?, ?)`,
			a.String(), idemKey, hash, string(body), now); err != nil {
			return out, err
		}
	}
	if err := tx.Commit(); err != nil {
		return BatchOutcome{}, err
	}
	if s.notify != nil {
		s.notify()
	}
	return out, nil
}

func (s *Service) runOp(ctx context.Context, a auth.Actor, op BatchOp) (Item, error) {
	switch op.Op {
	case "create":
		return s.CreateItem(ctx, a, op.Create)
	case "update":
		return s.UpdateItem(ctx, a, op.Key, op.Version, op.Patch)
	case "move":
		return s.MoveItem(ctx, a, op.Key, op.Version, op.Move)
	}
	return Item{}, invalid("op must be create, update or move")
}
