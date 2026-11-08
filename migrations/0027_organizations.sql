-- Organizations and their memberships (ATL-424, ADR-0027). Projects get organization_id in a later migration
-- (ATL-425) once the default organization can be created by the data migration (ADR-0028).
CREATE TABLE organizations (
  id INTEGER PRIMARY KEY,
  key TEXT NOT NULL UNIQUE COLLATE NOCASE,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
  created_at INTEGER NOT NULL,
  created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  archived_at INTEGER
) STRICT;

CREATE TABLE organization_memberships (
  organization_id INTEGER NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
  created_at INTEGER NOT NULL,
  created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  PRIMARY KEY (organization_id, user_id)
) STRICT;

CREATE INDEX organization_memberships_user_id ON organization_memberships (user_id);
