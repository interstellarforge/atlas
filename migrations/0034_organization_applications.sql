-- Organization applications (ADR-0031): a signed-in account asks for a new organization; the SUPERADMIN decides.
-- Approval creates the organization with the applicant as its first ADMIN. One waiting application per account.
CREATE TABLE organization_applications (
  id INTEGER PRIMARY KEY,
  applicant_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  key TEXT NOT NULL COLLATE NOCASE,
  note TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
  created_at INTEGER NOT NULL,
  decided_at INTEGER,
  decided_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  decision_note TEXT NOT NULL DEFAULT '',
  organization_id INTEGER REFERENCES organizations (id) ON DELETE SET NULL
) STRICT;

CREATE INDEX organization_applications_status ON organization_applications (status, created_at);
CREATE UNIQUE INDEX organization_applications_one_waiting ON organization_applications (applicant_id) WHERE status = 'pending';
