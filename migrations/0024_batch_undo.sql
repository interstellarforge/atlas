-- Undo of agent batches (ATL-74, ADR-0024). One row per applied operation of a batch, written in the batch
-- transaction: the fields it changed with their values before and after, so the batch can be reversed later.
CREATE TABLE batch_inverses (
  id INTEGER PRIMARY KEY,
  batch_id TEXT NOT NULL,
  project TEXT NOT NULL,
  item_key TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('create', 'update')),
  before TEXT NOT NULL, -- JSON object field -> value before the operation ('{}' for create)
  after TEXT NOT NULL,  -- JSON object field -> value after the operation
  subitems TEXT NOT NULL DEFAULT '[]', -- create: keys of items the operation created below it
  after_version INTEGER NOT NULL,
  actor TEXT NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX batch_inverses_batch ON batch_inverses (batch_id, id);
CREATE INDEX batch_inverses_project ON batch_inverses (project, id);

CREATE TABLE batch_undos (
  batch_id TEXT PRIMARY KEY,
  undone_at INTEGER NOT NULL,
  undone_by TEXT NOT NULL
) STRICT;
