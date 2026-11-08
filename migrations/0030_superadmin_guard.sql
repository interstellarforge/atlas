-- Last-SUPERADMIN invariant as a safety net (ATL-483, ADR-0029): no UPDATE or DELETE on users may leave the
-- installation without an ACTIVE superadmin. The core checks first and reports a rule error; this trigger catches
-- every other writer (a second process, a restore script, a bug). There is no override. It does not fire on a
-- database that never had a superadmin (a pre-organization database that has not been migrated yet).
CREATE TRIGGER users_keep_a_superadmin_update BEFORE UPDATE OF is_superadmin, deactivated_at ON users
WHEN OLD.is_superadmin = 1 AND OLD.deactivated_at IS NULL AND (NEW.is_superadmin = 0 OR NEW.deactivated_at IS NOT NULL)
  AND NOT EXISTS (SELECT 1 FROM users WHERE is_superadmin = 1 AND deactivated_at IS NULL AND id <> OLD.id)
BEGIN
  SELECT RAISE(ABORT, 'at least one active superadmin must remain');
END;

CREATE TRIGGER users_keep_a_superadmin_delete BEFORE DELETE ON users
WHEN OLD.is_superadmin = 1 AND OLD.deactivated_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM users WHERE is_superadmin = 1 AND deactivated_at IS NULL AND id <> OLD.id)
BEGIN
  SELECT RAISE(ABORT, 'at least one active superadmin must remain');
END;
