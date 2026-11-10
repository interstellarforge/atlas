-- Join link and join requests (ADR-0031): the organization ADMIN enables /join/<key>; any signed-in account may ask
-- to join; the ADMIN approves (USER membership, no project access) or rejects. organization_settings starts here and
-- grows with the company settings.
CREATE TABLE organization_settings (
  organization_id INTEGER PRIMARY KEY REFERENCES organizations (id) ON DELETE CASCADE,
  join_link_enabled INTEGER NOT NULL DEFAULT 0 CHECK (join_link_enabled IN (0, 1))
) STRICT;

CREATE TABLE organization_join_requests (
  id INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
  created_at INTEGER NOT NULL,
  decided_at INTEGER,
  decided_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  decision_note TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX organization_join_requests_queue ON organization_join_requests (organization_id, status, created_at);
CREATE UNIQUE INDEX organization_join_requests_one_waiting ON organization_join_requests (organization_id, user_id) WHERE status = 'pending';
