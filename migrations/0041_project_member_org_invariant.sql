-- A project member must belong to the project's organization (ADR-0027). core.SetMember checks it first; the
-- trigger keeps every other writer honest. A project without organization (a database not yet migrated with
-- `atlas migrate-orgs`) is exempt: nobody can reach it anyway.
CREATE TRIGGER project_members_org_member BEFORE INSERT ON project_members
WHEN (SELECT organization_id FROM projects WHERE id = NEW.project_id) IS NOT NULL
 AND NOT EXISTS (SELECT 1 FROM projects p JOIN organization_memberships m ON m.organization_id = p.organization_id
                 WHERE p.id = NEW.project_id AND m.user_id = NEW.user_id)
BEGIN
  SELECT RAISE(ABORT, 'a project member must belong to the project organization');
END;
