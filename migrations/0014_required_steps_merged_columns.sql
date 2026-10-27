-- Required workflow steps and merged board columns (ADR-0011).
-- A status may require that an item has been in other statuses before it can enter (empty = no rule).
CREATE TABLE status_requirements (
  status_id INTEGER NOT NULL REFERENCES project_statuses (id) ON DELETE CASCADE,
  required_status_id INTEGER NOT NULL REFERENCES project_statuses (id) ON DELETE CASCADE,
  PRIMARY KEY (status_id, required_status_id),
  CHECK (status_id <> required_status_id)
) STRICT;

-- Every status an item has been in. Keyed by status id, so renaming a status keeps the history.
CREATE TABLE item_status_visits (
  item_id INTEGER NOT NULL REFERENCES work_items (id) ON DELETE CASCADE,
  status_id INTEGER NOT NULL REFERENCES project_statuses (id) ON DELETE CASCADE,
  PRIMARY KEY (item_id, status_id)
) STRICT, WITHOUT ROWID;

-- Backfill: the current status, plus every status the audit log shows the item moving from or to (matched by name).
INSERT OR IGNORE INTO item_status_visits (item_id, status_id) SELECT id, status_id FROM work_items;
INSERT OR IGNORE INTO item_status_visits (item_id, status_id)
  SELECT w.id, s.id FROM audit_entries a
  JOIN work_items w ON w.key = a.target
  JOIN project_statuses s ON s.project_id = w.project_id
    AND s.name COLLATE NOCASE IN (json_extract(a.changes, '$.status.to'), json_extract(a.changes, '$.status.from'))
  WHERE a.operation IN ('item.move', 'item.update') AND json_valid(a.changes);

-- Show statuses of one category in one board column. Off = one column per status (the old behaviour).
ALTER TABLE projects ADD COLUMN merge_columns INTEGER NOT NULL DEFAULT 0;
