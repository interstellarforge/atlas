-- Projects point at their organization (ATL-425, ADR-0027). Additive and nullable on purpose: NOT NULL needs the
-- default organization, which only the reviewed data migration (ADR-0028, ORG 2) may create. That migration fills the
-- column and rebuilds the table to NOT NULL in its own transaction; until then a project without organization is
-- reachable by its members and the global admin only.
ALTER TABLE projects ADD COLUMN organization_id INTEGER REFERENCES organizations (id);

CREATE INDEX projects_organization_id ON projects (organization_id);
