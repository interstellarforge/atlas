-- Global SUPERADMIN flag (ATL-474, ADR-0029). It is a new concept, not a rename of is_admin: nobody is promoted here.
-- The data migration (ADR-0028) sets exactly one account and then drops is_admin and invites.is_admin.
ALTER TABLE users ADD COLUMN is_superadmin INTEGER NOT NULL DEFAULT 0 CHECK (is_superadmin IN (0, 1));
