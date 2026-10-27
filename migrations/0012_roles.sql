-- Configurable rights (ADR-0010): roles are rows, each with a set of actions. The built-ins keep today's behaviour.
CREATE TABLE roles (
  name TEXT PRIMARY KEY COLLATE NOCASE,
  builtin INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE role_actions (
  role TEXT NOT NULL REFERENCES roles (name) ON DELETE CASCADE,
  action TEXT NOT NULL,
  PRIMARY KEY (role, action)
) STRICT;

INSERT INTO roles (name, builtin) VALUES ('viewer', 1), ('member', 1), ('admin', 1);
INSERT INTO role_actions (role, action) VALUES
  ('viewer', 'view'), ('viewer', 'export'),
  ('member', 'view'), ('member', 'export'), ('member', 'item.create'), ('member', 'item.update'), ('member', 'comment'), ('member', 'import'),
  ('admin', 'view'), ('admin', 'export'), ('admin', 'item.create'), ('admin', 'item.update'), ('admin', 'comment'), ('admin', 'import'),
  ('admin', 'workflow'), ('admin', 'project.update'), ('admin', 'members'), ('admin', 'tokens');

-- The CHECK on role names goes: a member's role is now any row in roles (checked in core.SetMember).
CREATE TABLE project_members_new (
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  PRIMARY KEY (project_id, user_id)
) STRICT;
INSERT INTO project_members_new SELECT project_id, user_id, role FROM project_members;
DROP TABLE project_members;
ALTER TABLE project_members_new RENAME TO project_members;
CREATE INDEX project_members_user ON project_members (user_id);
