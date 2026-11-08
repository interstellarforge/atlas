# ADR-0028: Data migration to organizations, `is_superadmin` and permission-expansion review

- Status: accepted
- Date: 2026-11-08
- Tickets: ATL-449 (story ATL-472); implemented by ATL-405 (ORG 2)

## Context

Existing data has global `users.is_admin`, per-project roles in `roles`/`role_actions` (ADR-0010, built-in and custom) and no organizations. The migration must not silently widen or remove anyone's access.

## Decision

**Preflight (abort on any failure)**
1. Inventory every `is_admin` account (active, deactivated, anonymized), every role with its action set and member count, project-admin members and project tokens.
2. `ATLAS_SUPERADMIN_EMAIL` must match exactly one existing, non-deactivated `is_admin` account. Exactly one account becomes `is_superadmin`; the migration promotes nobody else.
3. Compare every legacy role's actions with the single new project role `member` (view, export, item.create, item.update, comment, import, time.log). Classify: equal; `admin` (reduction, approved here); everything else (viewer, custom) is REVIEW-REQUIRED.
4. `migrate-orgs --dry-run` writes nothing and prints the report. Unresolved REVIEW-REQUIRED roles block the real run. The owner resolves each in a mapping file: `member` (approved transition, may expand) or `remove` (approved loss). The real run needs `--confirm-report <hash of reviewed report incl. mapping>` and a verified backup.

**Snapshot check (ATL-428/429/430).** The report hash alone does not prove the database is unchanged since the dry run. The real run therefore, inside its own `BEGIN IMMEDIATE` transaction (no other writer can interleave):
1. re-inventories the database;
2. regenerates the report including the approved mapping and compares it with the approved one (hash);
3. aborts, changing nothing, if the starting situation or the permission mapping differs;
4. only then applies the mapping below in that same transaction.

**Mapping (one transaction)**
1. Create the default organization (`ATLAS_DEFAULT_ORG_KEY`/`NAME` optional).
2. All projects get that organization; ids, keys, items, comments, attachments and history are untouched.
3. Users: suspended where `deactivated_at` is set, else active. `is_admin` (and `invites.is_admin`) dropped after the audit.
4. The SUPERADMIN gets an explicit ADMIN membership of the default organization, so it always has an ACTIVE ADMIN (ADR-0029 last-ADMIN guard). Other active legacy admins become ADMIN of the default organization unless the mapping says `user` or `none`. Suspended or anonymized admins get no ADMIN row (reported).
5. Default-organization membership (role `user`) only for users with at least one `project_members` row.
6. `project_members.role`: `member` stays; `admin` becomes `member` (report lists lost actions workflow, project.update, members, tokens); REVIEW-REQUIRED roles follow the approved mapping.
7. `roles`/`role_actions` are renamed `legacy_roles`/`legacy_role_actions`, never read by authorization. A later migration may drop them after owner sign-off.
8. Unused `is_admin` invites are revoked; the rest become kind `registration`. Audit and change events get `organization_id` where derivable.
9. Sessions, tokens and OAuth grants are untouched; rights follow authorization.

**Verification (both directions)**
- Equal counts for projects, work items, comments, attachments, users; `project_members` equal except mapped rows.
- Loss check: per user, readable set old vs new equal except removals in the approved mapping.
- Expansion check: for every user x project x action, new is a subset of old except transitions approved in the mapping. Each difference is approved-reduction, approved-expansion or UNEXPECTED; any UNEXPECTED difference aborts.
- `integrity_check` and `foreign_key_check` clean. The report hash is stored in the meta table.

**Backup and rollback**
Before the real run: automatic backup (ADR-0006) and a restore test into a temporary directory (integrity check, row counts, identical dry-run report). Kept until the owner closes the migration. A failed run changes nothing (single transaction). An unwanted successful run is undone by restoring the backup. No down-migration.

## Consequences

- The owner must read the report and resolve REVIEW-REQUIRED roles before the real run; this is deliberate friction.
- Legacy tables stay in the database until a separate, signed-off migration.
- Needs a verified backup and restore rehearsal on the seeded dataset (ATL-431).
