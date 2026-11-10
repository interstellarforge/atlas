-- Company profile (ADR-0031 point 9): a logo (stored through the media store; only a re-encoded thumbnail is
-- served) and a default workflow (a project template applied to every project created in the organization).
ALTER TABLE organization_settings ADD COLUMN logo_path TEXT;
ALTER TABLE organization_settings ADD COLUMN default_project_template_id INTEGER REFERENCES project_templates (id) ON DELETE SET NULL;
