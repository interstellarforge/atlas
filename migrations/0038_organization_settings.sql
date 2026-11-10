-- Company settings (ADR-0031 point 9): rules the organization ADMIN sets, read live. Instance values stay the
-- defaults and floors; a person in several organizations gets the strictest value. NULL = no organization value.
ALTER TABLE organization_settings ADD COLUMN password_min_length INTEGER CHECK (password_min_length IS NULL OR password_min_length BETWEEN 8 AND 128);
ALTER TABLE organization_settings ADD COLUMN session_days INTEGER CHECK (session_days IS NULL OR session_days BETWEEN 1 AND 90);
ALTER TABLE organization_settings ADD COLUMN project_creation TEXT NOT NULL DEFAULT 'admins' CHECK (project_creation IN ('admins', 'members'));
ALTER TABLE organization_settings ADD COLUMN require_2fa INTEGER NOT NULL DEFAULT 0 CHECK (require_2fa IN (0, 1));
ALTER TABLE organization_settings ADD COLUMN tokens_allowed INTEGER NOT NULL DEFAULT 1 CHECK (tokens_allowed IN (0, 1));
ALTER TABLE organization_settings ADD COLUMN token_max_days INTEGER CHECK (token_max_days IS NULL OR token_max_days BETWEEN 1 AND 365);
