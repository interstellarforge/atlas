-- Free accounts (ADR-0031): registration no longer waits for an approval of the Atlas admin; access is decided per
-- organization. Accounts that were waiting (pending) or declined (rejected) become ordinary active accounts without
-- any membership, so they reach nothing. The registration mode `request` and the setting approval_by are retired.
UPDATE users SET status = 'active', status_changed_at = NULL, rejection_note = NULL WHERE status IN ('pending', 'rejected');
UPDATE instance_settings SET value = 'closed' WHERE key = 'registration' AND value = 'request';
DELETE FROM instance_settings WHERE key = 'approval_by';
