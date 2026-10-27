-- An admin can force a password change: the user's sessions end and the next one may only change the password.
ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0;
