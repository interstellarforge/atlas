-- Outgoing webhooks per project (ATL-77, ADR-0022). Deliveries are queued in webhook_outbox in the same
-- transaction as the change that causes them and sent later by a scheduler job.
CREATE TABLE webhooks (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  url TEXT NOT NULL,
  secret_enc BLOB NOT NULL, -- AES-GCM with the key in <data dir>/mail.key; signs every delivery
  events TEXT NOT NULL DEFAULT '', -- comma separated event types; empty = every event
  active INTEGER NOT NULL DEFAULT 1,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX webhooks_project ON webhooks (project_id) WHERE active = 1;

CREATE TABLE webhook_outbox (
  id INTEGER PRIMARY KEY,
  webhook_id INTEGER NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
  event TEXT NOT NULL,
  payload TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER NOT NULL,
  delivered_at INTEGER,
  failed_at INTEGER,
  last_error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX webhook_outbox_due ON webhook_outbox (next_attempt_at) WHERE delivered_at IS NULL AND failed_at IS NULL;
