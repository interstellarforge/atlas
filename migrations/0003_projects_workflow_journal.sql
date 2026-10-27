CREATE TABLE project_members (
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
  PRIMARY KEY (project_id, user_id)
) STRICT;

CREATE INDEX project_members_user ON project_members (user_id);

-- No rows for a project means every status change is allowed.
CREATE TABLE workflow_transitions (
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  from_status_id INTEGER NOT NULL REFERENCES project_statuses (id) ON DELETE CASCADE,
  to_status_id INTEGER NOT NULL REFERENCES project_statuses (id) ON DELETE CASCADE,
  PRIMARY KEY (project_id, from_status_id, to_status_id)
) STRICT;

CREATE TABLE item_links (
  id INTEGER PRIMARY KEY,
  from_id INTEGER NOT NULL REFERENCES work_items (id) ON DELETE CASCADE,
  to_id INTEGER NOT NULL REFERENCES work_items (id) ON DELETE CASCADE,
  type TEXT NOT NULL CHECK (type IN ('blocks', 'relates', 'duplicates')),
  created_at INTEGER NOT NULL,
  UNIQUE (from_id, to_id, type),
  CHECK (from_id <> to_id)
) STRICT;

CREATE INDEX item_links_to ON item_links (to_id, type);

CREATE TABLE labels (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  name TEXT NOT NULL COLLATE NOCASE,
  UNIQUE (project_id, name)
) STRICT;

CREATE TABLE item_labels (
  item_id INTEGER NOT NULL REFERENCES work_items (id) ON DELETE CASCADE,
  label_id INTEGER NOT NULL REFERENCES labels (id) ON DELETE CASCADE,
  PRIMARY KEY (item_id, label_id)
) STRICT;

CREATE INDEX item_labels_label ON item_labels (label_id);

CREATE TABLE custom_field_definitions (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('text', 'number', 'bool', 'date', 'select')),
  required INTEGER NOT NULL DEFAULT 0,
  default_value TEXT,
  options TEXT,
  UNIQUE (project_id, name)
) STRICT;

-- Sync journal: seq is the cursor for SSE and delta reads.
CREATE TABLE change_events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL,
  project TEXT NOT NULL,
  item_key TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL,
  ts INTEGER NOT NULL
) STRICT;

CREATE INDEX change_events_project ON change_events (project, seq);

CREATE TABLE audit_entries (
  id INTEGER PRIMARY KEY,
  actor TEXT NOT NULL,
  operation TEXT NOT NULL,
  target TEXT NOT NULL,
  changes TEXT NOT NULL,
  batch_id TEXT,
  ts INTEGER NOT NULL
) STRICT;

CREATE INDEX audit_entries_target ON audit_entries (target, id);
