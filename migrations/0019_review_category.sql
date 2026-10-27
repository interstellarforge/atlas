-- atlas:foreign-keys-off
-- A fourth status category, review, for statuses where work waits for someone's judgement. SQLite cannot
-- change a CHECK in place, so the table is rebuilt (the documented 12-step procedure; the runner turns
-- foreign keys off for this file and checks them before it commits). Ids are kept, so every reference holds.
CREATE TABLE project_statuses_new (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  position INTEGER NOT NULL,
  category TEXT NOT NULL CHECK (category IN ('todo', 'active', 'review', 'completed')),
  wip_limit INTEGER,
  allow_create INTEGER NOT NULL DEFAULT 1
) STRICT;
INSERT INTO project_statuses_new (id, project_id, name, position, category, wip_limit, allow_create)
  SELECT id, project_id, name, position, category, wip_limit, allow_create FROM project_statuses;
DROP TABLE project_statuses;
ALTER TABLE project_statuses_new RENAME TO project_statuses;
CREATE INDEX project_statuses_project_id ON project_statuses (project_id, position);

-- Existing statuses called Review move to the new category; everything else keeps its category.
UPDATE project_statuses SET category = 'review' WHERE category = 'active' AND lower(name) = 'review';
UPDATE project_templates SET data = replace(data, '"name":"Review","category":"active"', '"name":"Review","category":"review"');
