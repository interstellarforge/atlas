-- Email domains and email verification (ADR-0031). An ADMIN claims a domain, the SUPERADMIN confirms it; accounts with
-- a confirmed domain may ask to join (mode request) or join at once when their email is verified (mode auto).
-- One domain belongs to one organization (pending or confirmed). Verification links are single use and expire.
CREATE TABLE organization_domains (
  id INTEGER PRIMARY KEY,
  organization_id INTEGER NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  domain TEXT NOT NULL COLLATE NOCASE,
  mode TEXT NOT NULL DEFAULT 'request' CHECK (mode IN ('request', 'auto')),
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed')),
  claimed_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  claimed_at INTEGER NOT NULL,
  confirmed_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
  confirmed_at INTEGER
) STRICT;

CREATE UNIQUE INDEX organization_domains_one_owner ON organization_domains (domain);

CREATE TABLE email_verifications (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  email TEXT NOT NULL COLLATE NOCASE,
  token_hash TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at INTEGER
) STRICT;
