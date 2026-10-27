-- A project can link one git repository (ADR-0013). Only the link is stored: the normalised https web address
-- and the default branch. The provider is derived from the address when needed. Empty = not linked.
ALTER TABLE projects ADD COLUMN repo_url TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN repo_branch TEXT NOT NULL DEFAULT '';
