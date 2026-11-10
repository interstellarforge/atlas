-- Organization invitations (ADR-0031): the organization ADMIN invites an email address; accepting proves the address
-- (users.email_verified_at) and creates a USER membership. Only the token hash is stored; "expired" is derived.
ALTER TABLE users ADD COLUMN email_verified_at INTEGER;

CREATE TABLE organization_invites (
  id INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  email TEXT NOT NULL COLLATE NOCASE,
  token_hash TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'revoked')),
  invited_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  accepted_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  accepted_at INTEGER
) STRICT;

CREATE UNIQUE INDEX organization_invites_one_pending ON organization_invites (organization_id, email) WHERE status = 'pending';

CREATE TABLE organization_invite_projects (
  invite_id INTEGER NOT NULL REFERENCES organization_invites (id) ON DELETE CASCADE,
  project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  PRIMARY KEY (invite_id, project_id)
) STRICT;
