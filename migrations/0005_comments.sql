CREATE TABLE comments (
  id INTEGER PRIMARY KEY,
  item_id INTEGER NOT NULL REFERENCES work_items (id),
  actor TEXT NOT NULL,
  body TEXT NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX comments_item ON comments (item_id, id);
