CREATE TABLE attachments (
  id INTEGER PRIMARY KEY,
  item_id INTEGER NOT NULL REFERENCES work_items (id),
  filename TEXT NOT NULL,
  mime TEXT NOT NULL,
  size INTEGER NOT NULL,
  width INTEGER NOT NULL,
  height INTEGER NOT NULL,
  path TEXT NOT NULL UNIQUE,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX attachments_item ON attachments (item_id, id);
