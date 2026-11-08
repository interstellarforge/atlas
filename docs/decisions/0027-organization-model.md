# ADR-0027: Organization model and project key uniqueness

- Status: accepted
- Date: 2026-11-08
- Tickets: ATL-448 (story ATL-472)

## Context

Atlas has one flat project list. A global `users.is_admin` sees everything and everyone else sees projects they are a member of. Multi-user use needs a boundary between groups of projects and people, without a heavyweight tenant system.

## Decision

1. **Hierarchy:** Server > Organization > Project > Work item. Every project belongs to exactly one organization (`projects.organization_id NOT NULL`).
2. **Isolated by default.** Nothing crosses an organization boundary unless a SUPERADMIN acts.
3. **Project keys stay globally unique.** Routes (`/projects/{key}`), MCP tools and ticket keys are unchanged; organization fields are additive. Organization keys are unique, case-insensitive.
4. **Users are global identities** with any number of organization memberships. Account status, organization membership and project membership are separate concepts.
5. **Three roles only:** SUPERADMIN (global, `users.is_superadmin`), organization ADMIN (`organization_memberships.role = admin`), USER (`role = user` plus explicit `project_members` rows).
6. **Access inheritance:** SUPERADMIN and organization ADMIN reach projects without an explicit membership. An ADMIN never reaches another organization. A USER reaches only projects with a `project_members` row; organization membership alone grants nothing.
7. **Invariant:** a project member is a member of the project's organization. Enforced in core and by a DB trigger; removing an organization membership removes that organization's project rows in the same transaction.
8. **Effective access is derived** (`explicit`, `inherited_org_admin`, `server_admin`), never stored.
9. **Initial ADMIN.** Every organization has at least one ACTIVE ADMIN from the moment it exists. Creating an organization requires an initial ADMIN (default: the creating SUPERADMIN, who then holds an explicit ADMIN membership). `allow_orphan` (ADR-0029) is an override for existing organizations, never the way to create one. SUPERADMIN powers stay inherited; the explicit membership adds none.
10. **Archived organization:** its projects are hidden and read-only except to SUPERADMIN.

## Consequences

- One new table pair and one `projects` rebuild; no per-organization database or schema.
- Global key uniqueness keeps every existing URL, token scope and git reference valid, at the cost of keys being a shared namespace across organizations.
- Schema details: ADR-0029 and `docs/data-model.md`. Existing-data mapping: ADR-0028.
