-- Accounts (avatar, 2FA), instance policy, invites, and wider item templates.
ALTER TABLE users ADD COLUMN avatar_path TEXT;
ALTER TABLE users ADD COLUMN totp_secret TEXT;
ALTER TABLE users ADD COLUMN totp_pending TEXT;
ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0;

CREATE TABLE recovery_codes (
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  code_hash TEXT NOT NULL,
  used_at INTEGER,
  PRIMARY KEY (user_id, code_hash)
) STRICT, WITHOUT ROWID;

-- Instance policy; absent keys use the defaults in code.
CREATE TABLE instance_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

CREATE TABLE invites (
  id INTEGER PRIMARY KEY,
  code_hash TEXT NOT NULL UNIQUE,
  email TEXT NOT NULL DEFAULT '',
  is_admin INTEGER NOT NULL DEFAULT 0,
  created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at INTEGER,
  used_by INTEGER REFERENCES users (id) ON DELETE SET NULL
) STRICT;

-- Item templates: project_id NULL means instance wide; subitems is a JSON tree.
CREATE TABLE item_templates_new (
  id INTEGER PRIMARY KEY,
  project_id INTEGER REFERENCES projects (id),
  name TEXT NOT NULL COLLATE NOCASE,
  type TEXT NOT NULL,
  title_prefix TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  priority TEXT NOT NULL DEFAULT 'normal',
  labels TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT '',
  checklist TEXT NOT NULL DEFAULT '[]',
  subitems TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL
) STRICT;
INSERT INTO item_templates_new (id, project_id, name, type, title_prefix, description, priority, labels, status, checklist, created_at)
  SELECT id, project_id, name, type, title_prefix, description, priority, labels, status, checklist, created_at FROM item_templates;
DROP TABLE item_templates;
ALTER TABLE item_templates_new RENAME TO item_templates;
CREATE UNIQUE INDEX item_templates_name ON item_templates (COALESCE(project_id, 0), name);
