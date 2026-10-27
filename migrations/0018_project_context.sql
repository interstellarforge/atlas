-- Project context (ADR-0015): compact, curated project knowledge that agents and people share.
ALTER TABLE projects ADD COLUMN context_version INTEGER NOT NULL DEFAULT 0;

CREATE TABLE project_context_entries (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  category TEXT NOT NULL CHECK (category IN ('architecture', 'decision', 'convention', 'pitfall', 'dependency', 'reference')),
  title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 160),
  summary TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 1000),
  body TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'archived')),
  superseded_by INTEGER REFERENCES project_context_entries (id) ON DELETE SET NULL,
  verification_state TEXT NOT NULL DEFAULT 'unverified' CHECK (verification_state IN ('verified', 'unverified', 'stale')),
  source_kind TEXT NOT NULL DEFAULT 'manual' CHECK (source_kind IN ('manual', 'repository', 'ticket', 'derived')),
  pinned INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
  version INTEGER NOT NULL DEFAULT 1,
  created_by TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  verified_at INTEGER,
  verified_by TEXT NOT NULL DEFAULT '',
  UNIQUE (project_id, key)
) STRICT;

CREATE INDEX project_context_status ON project_context_entries (project_id, status, pinned);

CREATE TABLE project_context_sources (
  id INTEGER PRIMARY KEY,
  entry_id INTEGER NOT NULL REFERENCES project_context_entries (id) ON DELETE CASCADE,
  source_type TEXT NOT NULL CHECK (source_type IN ('file', 'ticket', 'url', 'adr')),
  repository TEXT NOT NULL DEFAULT '',
  file_path TEXT NOT NULL DEFAULT '',
  commit_sha TEXT NOT NULL DEFAULT '',
  ticket_key TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL DEFAULT '',
  content_hash TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  verified_at INTEGER
) STRICT;

CREATE INDEX project_context_sources_entry ON project_context_sources (entry_id);

-- New actions are granted by migration (ADR-0010): members write context, only project admins verify it.
INSERT INTO role_actions (role, action) VALUES ('member', 'context.write'), ('admin', 'context.write'), ('admin', 'context.verify');
