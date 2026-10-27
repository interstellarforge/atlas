-- Time tracking per project (ADR-0014). Off by default; time is booked by hand on an item.
ALTER TABLE projects ADD COLUMN time_tracking INTEGER NOT NULL DEFAULT 0;

CREATE TABLE time_entries (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES work_items (id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users (id),
  -- The item's status when the time was booked, so a summary can split time per status.
  status_id INTEGER REFERENCES project_statuses (id) ON DELETE SET NULL,
  minutes INTEGER NOT NULL CHECK (minutes BETWEEN 1 AND 1440),
  spent_on TEXT NOT NULL CHECK (spent_on GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
  note TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

CREATE INDEX time_entries_item ON time_entries (item_id);
CREATE INDEX time_entries_project_day ON time_entries (project_id, spent_on);

-- time.log goes to every role that may edit items (ADR-0010: new actions are granted by migration).
INSERT INTO role_actions (role, action) SELECT role, 'time.log' FROM role_actions WHERE action = 'item.update';
