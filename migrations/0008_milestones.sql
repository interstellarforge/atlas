CREATE TABLE milestones (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  due_at INTEGER,
  released_at INTEGER,
  created_at INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX milestones_name ON milestones (project_id, name COLLATE NOCASE);

ALTER TABLE work_items ADD COLUMN milestone_id INTEGER REFERENCES milestones (id);
CREATE INDEX work_items_milestone ON work_items (milestone_id) WHERE milestone_id IS NOT NULL;
