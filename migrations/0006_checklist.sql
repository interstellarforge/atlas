CREATE TABLE checklist_items (
  id INTEGER PRIMARY KEY,
  item_id INTEGER NOT NULL REFERENCES work_items (id),
  text TEXT NOT NULL,
  done INTEGER NOT NULL DEFAULT 0 CHECK (done IN (0, 1)),
  position INTEGER NOT NULL
) STRICT;

CREATE INDEX checklist_items_item ON checklist_items (item_id, position);
