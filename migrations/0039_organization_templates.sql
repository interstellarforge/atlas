-- Company-scoped templates and completed-items visibility (ADR-0031 point 9). An item template with project_id NULL
-- and organization_id set belongs to one organization; both NULL stays instance wide. A project template with an
-- organization_id is seen and used only by members of that organization, and the project is created in it.
ALTER TABLE item_templates ADD COLUMN organization_id INTEGER REFERENCES organizations (id) ON DELETE CASCADE;
DROP INDEX item_templates_name;
CREATE UNIQUE INDEX item_templates_name ON item_templates (COALESCE(project_id, 0), COALESCE(organization_id, 0), name);

CREATE TABLE project_templates_new (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL COLLATE NOCASE,
  description TEXT NOT NULL DEFAULT '',
  data TEXT NOT NULL,
  organization_id INTEGER REFERENCES organizations (id) ON DELETE CASCADE,
  created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL
) STRICT;
INSERT INTO project_templates_new (id, name, description, data, created_by, created_at)
  SELECT id, name, description, data, created_by, created_at FROM project_templates;
DROP TABLE project_templates;
ALTER TABLE project_templates_new RENAME TO project_templates;
CREATE UNIQUE INDEX project_templates_name ON project_templates (COALESCE(organization_id, 0), name);

-- NULL = the instance value; 0 = never hide completed items.
ALTER TABLE organization_settings ADD COLUMN done_visible_days INTEGER CHECK (done_visible_days IS NULL OR done_visible_days BETWEEN 0 AND 3650);
