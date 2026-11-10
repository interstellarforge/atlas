-- Account lifecycle (ATL-366, ADR-0030): pending | active | rejected | suspended.
-- deactivated_at stays for now (anonymised users, the last-superadmin trigger); ATL-368 moves every check to status.
ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('pending', 'active', 'rejected', 'suspended'));
ALTER TABLE users ADD COLUMN status_changed_at INTEGER;
ALTER TABLE users ADD COLUMN status_changed_by INTEGER REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE users ADD COLUMN rejection_note TEXT;
-- Re-apply limits for rejected accounts: 24 h after status_changed_at, at most 3 per 30 days from reapply_window_at.
ALTER TABLE users ADD COLUMN reapply_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN reapply_window_at INTEGER;

UPDATE users SET status = 'suspended', status_changed_at = deactivated_at WHERE deactivated_at IS NOT NULL;

CREATE INDEX idx_users_status ON users (status) WHERE status <> 'active';

-- The last-superadmin triggers (0030) now follow status; a row that is no longer ACTIVE or has deactivated_at set no longer counts.
DROP TRIGGER users_keep_a_superadmin_update;
DROP TRIGGER users_keep_a_superadmin_delete;

CREATE TRIGGER users_keep_a_superadmin_update BEFORE UPDATE OF is_superadmin, deactivated_at, status ON users
WHEN OLD.is_superadmin = 1 AND OLD.status = 'active' AND OLD.deactivated_at IS NULL
  AND (NEW.is_superadmin = 0 OR NEW.status <> 'active' OR NEW.deactivated_at IS NOT NULL)
  AND NOT EXISTS (SELECT 1 FROM users WHERE is_superadmin = 1 AND status = 'active' AND deactivated_at IS NULL AND id <> OLD.id)
BEGIN
  SELECT RAISE(ABORT, 'at least one active superadmin must remain');
END;

CREATE TRIGGER users_keep_a_superadmin_delete BEFORE DELETE ON users
WHEN OLD.is_superadmin = 1 AND OLD.status = 'active' AND OLD.deactivated_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM users WHERE is_superadmin = 1 AND status = 'active' AND deactivated_at IS NULL AND id <> OLD.id)
BEGIN
  SELECT RAISE(ABORT, 'at least one active superadmin must remain');
END;
