-- Replay protection for batch writes: same actor + key returns the stored result.
CREATE TABLE idempotency_records (
  actor TEXT NOT NULL,
  key TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  result TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (actor, key)
) STRICT, WITHOUT ROWID;
