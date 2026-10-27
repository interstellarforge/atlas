-- Minimal core schema so the seed generator and benchmarks have real tables.
-- Services, workflow rules and the remaining tables arrive with the WRK epic (see docs/data-model.md).

CREATE TABLE projects (
  id INTEGER PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  next_item_number INTEGER NOT NULL DEFAULT 1,
  archived_at INTEGER,
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE project_statuses (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  position INTEGER NOT NULL,
  category TEXT NOT NULL CHECK (category IN ('todo', 'active', 'completed')),
  wip_limit INTEGER
) STRICT;

CREATE INDEX project_statuses_project_id ON project_statuses (project_id, position);

CREATE TABLE work_items (
  id INTEGER PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  parent_id INTEGER REFERENCES work_items (id),
  path TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('epic', 'story', 'task', 'bug')),
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status_id INTEGER NOT NULL REFERENCES project_statuses (id),
  priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
  assignee_id INTEGER REFERENCES users (id),
  due_at INTEGER,
  rank TEXT NOT NULL,
  custom_data TEXT NOT NULL DEFAULT '{}',
  version INTEGER NOT NULL DEFAULT 1,
  archived_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

CREATE INDEX work_items_board ON work_items (project_id, status_id, rank) WHERE archived_at IS NULL;
CREATE INDEX work_items_path ON work_items (path);
CREATE INDEX work_items_parent ON work_items (parent_id);
CREATE INDEX work_items_updated ON work_items (updated_at);
