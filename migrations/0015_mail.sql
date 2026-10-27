-- Optional outgoing email (ADR-0012). No row in mail_settings = use the ATLAS_SMTP_* environment (or no email at all).
CREATE TABLE mail_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  host TEXT NOT NULL,
  port INTEGER NOT NULL,
  security TEXT NOT NULL CHECK (security IN ('tls', 'starttls', 'none')),
  username TEXT NOT NULL DEFAULT '',
  password_enc BLOB, -- AES-GCM with the key in <data dir>/mail.key; NULL = no password
  from_addr TEXT NOT NULL,
  from_name TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL
) STRICT;

-- Written in the same transaction as the mutation that causes a mail; sent later by a worker, outside any transaction.
CREATE TABLE mail_outbox (
  id INTEGER PRIMARY KEY,
  to_addr TEXT NOT NULL,
  subject TEXT NOT NULL,
  body TEXT NOT NULL, -- blanked once sent or given up: it can hold an invite link
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER NOT NULL,
  sent_at INTEGER,
  failed_at INTEGER,
  last_error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX mail_outbox_due ON mail_outbox (next_attempt_at) WHERE sent_at IS NULL AND failed_at IS NULL;

-- Per-user switch lives in users.preferences (email_notifications); nothing to add here.
