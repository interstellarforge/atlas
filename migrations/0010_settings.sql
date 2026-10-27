-- Settings, rights and templates (epic SET).
ALTER TABLE users ADD COLUMN deactivated_at INTEGER;
ALTER TABLE users ADD COLUMN preferences TEXT NOT NULL DEFAULT '{}';
ALTER TABLE api_tokens ADD COLUMN last_used_at INTEGER;
ALTER TABLE project_statuses ADD COLUMN allow_create INTEGER NOT NULL DEFAULT 1;

CREATE TABLE item_templates (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id),
  name TEXT NOT NULL COLLATE NOCASE,
  type TEXT NOT NULL,
  title_prefix TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  priority TEXT NOT NULL DEFAULT 'normal',
  labels TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT '',
  checklist TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL,
  UNIQUE (project_id, name)
) STRICT;

CREATE TABLE project_templates (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  description TEXT NOT NULL DEFAULT '',
  data TEXT NOT NULL,
  created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL
) STRICT;

-- Built-in project templates: workflows people actually use. Admins can delete or replace them.
INSERT INTO project_templates (name, description, data, created_at) VALUES
('Kanban', 'Backlog, Todo, In Progress, Review, Done: the Atlas default.',
 '{"version":1,"statuses":[{"name":"Backlog","category":"todo","allow_create":true},{"name":"Todo","category":"todo","allow_create":true},{"name":"In Progress","category":"active","allow_create":true},{"name":"Review","category":"active","allow_create":true},{"name":"Done","category":"completed","allow_create":true}],"transitions":[],"labels":[],"custom_fields":[],"item_templates":[]}',
 CAST(strftime('%s','now') AS INTEGER) * 1000),
('Bug tracking', 'New, Triage, In Progress, Fixed, Closed; bugs cannot start as Fixed or Closed.',
 '{"version":1,"statuses":[{"name":"New","category":"todo","allow_create":true},{"name":"Triage","category":"todo","allow_create":true},{"name":"In Progress","category":"active","wip_limit":5,"allow_create":false},{"name":"Fixed","category":"active","allow_create":false},{"name":"Closed","category":"completed","allow_create":false}],"transitions":[],"labels":["regression","needs-info"],"custom_fields":[{"name":"Severity","type":"select","required":false,"options":["minor","major","critical"]}],"item_templates":[{"name":"Bug report","type":"bug","title_prefix":"","description":"## Steps to reproduce\n\n## Expected\n\n## Actual\n","priority":"normal","labels":[],"status":"New","checklist":["Reproduced","Fix verified"]}]}',
 CAST(strftime('%s','now') AS INTEGER) * 1000),
('Scrum', 'Backlog, Sprint, In Progress, Review, Done with a work-in-progress limit.',
 '{"version":1,"statuses":[{"name":"Backlog","category":"todo","allow_create":true},{"name":"Sprint","category":"todo","allow_create":true},{"name":"In Progress","category":"active","wip_limit":4,"allow_create":false},{"name":"Review","category":"active","allow_create":false},{"name":"Done","category":"completed","allow_create":false}],"transitions":[],"labels":[],"custom_fields":[{"name":"Story points","type":"number","required":false}],"item_templates":[{"name":"User story","type":"story","title_prefix":"","description":"As a ..., I want ..., so that ...\n\n## Acceptance\n","priority":"normal","labels":[],"status":"Backlog","checklist":[]}]}',
 CAST(strftime('%s','now') AS INTEGER) * 1000);
