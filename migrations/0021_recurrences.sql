-- Recurring items (ATL-72). The scheduler creates one item per due recurrence, claiming the occurrence by
-- advancing next_run_at in the same transaction, so a crash or a second runner never creates a duplicate.
CREATE TABLE recurrences (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  item_type TEXT NOT NULL DEFAULT 'task',
  description TEXT NOT NULL DEFAULT '',
  priority TEXT NOT NULL DEFAULT 'normal',
  labels TEXT NOT NULL DEFAULT '[]',
  parent TEXT NOT NULL DEFAULT '',
  freq TEXT NOT NULL CHECK (freq IN ('daily', 'weekly', 'monthly')),
  interval_n INTEGER NOT NULL DEFAULT 1 CHECK (interval_n BETWEEN 1 AND 52),
  weekday INTEGER CHECK (weekday BETWEEN 0 AND 6),
  month_day INTEGER CHECK (month_day BETWEEN 1 AND 31),
  at_minute INTEGER NOT NULL DEFAULT 0 CHECK (at_minute BETWEEN 0 AND 1439),
  next_run_at INTEGER NOT NULL,
  last_run_at INTEGER,
  last_error TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL DEFAULT 1,
  created_by INTEGER NOT NULL REFERENCES users (id),
  created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX recurrences_due ON recurrences (next_run_at) WHERE active = 1;
CREATE INDEX recurrences_project ON recurrences (project_id);
