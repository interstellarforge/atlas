-- Sprints (ATL-73, ADR-0023): time-boxed groups of items next to milestones. An item is in at most one sprint.
CREATE TABLE sprints (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  start_at INTEGER NOT NULL,
  end_at INTEGER NOT NULL CHECK (end_at > start_at),
  closed_at INTEGER,
  created_at INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX sprints_name ON sprints (project_id, name COLLATE NOCASE);

ALTER TABLE work_items ADD COLUMN sprint_id INTEGER REFERENCES sprints (id);
CREATE INDEX work_items_sprint ON work_items (sprint_id) WHERE sprint_id IS NOT NULL;
