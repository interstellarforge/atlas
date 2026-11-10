-- Support access (ADR-0031): an organization ADMIN lets the Atlas SUPERADMIN into the organization for a while.
-- Without a valid grant a SUPERADMIN reaches nothing of an organization. Authorization reads this table live.
CREATE TABLE support_grants (
  id INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  level TEXT NOT NULL CHECK (level IN ('read_only', 'full')),
  granted_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  granted_at INTEGER NOT NULL,
  expires_at INTEGER, -- NULL: until revoked
  revoked_at INTEGER,
  revoked_by INTEGER REFERENCES users (id) ON DELETE SET NULL
) STRICT;

CREATE INDEX support_grants_open ON support_grants (organization_id) WHERE revoked_at IS NULL;
