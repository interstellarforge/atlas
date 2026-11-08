# ADR-0029: Authorization model, retired roles tables and 401/403/404 semantics

- Status: accepted
- Date: 2026-11-08
- Supersedes: ADR-0010 (configurable rights and custom roles), once accepted
- Tickets: ATL-491 and ATL-434 (stories ATL-472, ATL-407)

## Context

Authorization today is `core.Service.access` (internal/core/core.go) reading `Actor.IsAdmin`, `project_members.role` and `role_actions`. Organizations (ADR-0027) need one decision point that also covers tokens, OAuth and background work.

## Decision

### Model
1. **One function.** All decisions live in `internal/core/authz.go`. No other code reads `is_superadmin` or membership tables to decide access. Deny by default.
2. **Three roles, one project role.** SUPERADMIN, organization ADMIN, USER. `project_members.role` has the single value `member`. No viewer and no custom roles in the MVP (epic ADV).
3. **`users.is_superadmin` replaces `is_admin`** (reuse rejected: the old flag meant implicit project admin everywhere). `Actor.IsAdmin` is removed and not replaced: the Actor carries identity and authentication method only (kind, user id, token id, session id, scopes, projects, CSRF), never a privilege flag. Set only by bootstrap (`ATLAS_SUPERADMIN_EMAIL`) or by an existing SUPERADMIN; never via invite, registration, organization ADMIN, request body or MCP.
4. **Retired tables.** `/admin/roles` endpoints, UI and the `default_role` setting are removed. `legacy_roles`/`legacy_role_actions` are never read by authorization (a guard test allows only migration and report code) and stay read-only until owner sign-off (ADR-0028).
5. **Fail closed on status; live rights only.** Every decision requires `users.status = active`. `authz.go` reads account status, `is_superadmin` and memberships from the database on every decision (inside the same transaction as the mutation); it never trusts a value copied into the Actor, session, token, OAuth grant or cache. Revoking SUPERADMIN, suspending an account or removing a membership therefore takes effect on the next request of an existing session. Tokens and OAuth intersect with the owner's current rights. Required tests: revoke SUPERADMIN / suspend / remove membership while a session and a token are open, then expect denial.
6. **Initial ADMIN.** Creating an organization requires an initial ADMIN (ADR-0027) and the migration assigns one (ADR-0028), so the last-ADMIN guard never starts violated.

### Actions
- Member set (`core.MemberActions`): `view`, `export`, `item.create`, `item.update`, `comment`, `import`, `time.log`, `context.write`. `context.verify` and every management action belong to ADMIN and SUPERADMIN.
- Organization/project management (ADMIN of that organization, SUPERADMIN): `workflow`, `project.update`, `members`, `tokens`, `webhooks`, `project.create`, `project.archive`, `org.update`, `org.members`, `org.admins` (never grants SUPERADMIN), `org.audit`.
- Server (SUPERADMIN only): `server.settings`, `server.users`, `server.audit`, `org.create`, `org.archive`, `org.any` (open any organization without membership).
- Everyone active: read own organizations, assigned projects and own effective access. Another user's effective access: SUPERADMIN, or ADMIN for members of their organizations.
- Full matrix: Context `planned-org-rbac-model`; every row needs an allowed and a denied test.

### API (spec)
```go
type Access struct {
    Level int    // none | member | org_admin | superadmin
    Via   string // explicit | inherited_org_admin | server_admin
}

func (s *Service) authorizeProject(q queryer, a auth.Actor, key string, act Action) (Project, Access, error)
func (s *Service) authorizeOrg(q queryer, a auth.Actor, orgKey string, act Action) (Organization, Access, error)
func (s *Service) authorizeServer(q queryer, a auth.Actor, act Action) error
func (s *Service) readableProjects(q queryer, a auth.Actor) (map[int64]string, error)
```
Errors: `ErrUnauthorized` (401), `ErrForbidden` (403), `ErrNotFound` (404), nil. Token scope (`read`/`write`, project list) is checked first, as today.

- **Tokens and OAuth:** allowed = scope and project list AND the owner's current rights. A suspended owner means 401.
- **Sessions:** same function; CSRF failure is 403.
- **Background actors** (scheduler, webhook delivery, notifications): carry no rights through `authorize*`. Work done for a user is filtered with that user's live rights; system-initiated writes use a dedicated actor kind that can only call internal functions, never the public action list.

### Response semantics
- **401:** missing, invalid or expired credentials; any session, token or OAuth of a pending, rejected or suspended account. Correct credentials of a non-active account: 403 with a state code; wrong credentials: generic 401.
- **403:** the caller may see the resource but lacks permission for the requested action: `/api/v1/admin/*` for non-SUPERADMIN, a management action by a USER on a project they are a member of, an action an organization ADMIN lacks inside an organization they administer, token lacks scope, CSRF failure.
- **404 (identical body):** the resource does not exist, or the caller has no right to know it exists. Visibility is decided first, the action second: a USER who is a member of the organization but not of project ATL gets 404 for every action on ATL, including management actions, never 403. Same for items, attachments, exports, organizations the caller does not belong to and users outside shared organizations.
- Error bodies and headers carry no resource names, roles or hints beyond a fixed code.
- **429** rate limits. **503** retryable when the database stays busy; nothing is applied.

### Last-admin guards and concurrency
- **Last SUPERADMIN:** at least one ACTIVE superadmin always exists; no override.
- **Last organization ADMIN:** each non-archived organization keeps one ACTIVE ADMIN; SUPERADMIN may override with explicit `allow_orphan`. Pending and suspended accounts never count.
- Guards run on every path (demote, remove, suspend, reject, self-delete, anonymize), in the mutating transaction.
- `DB.W` is one connection opened with `_txlock=immediate`, so `Begin()` is `BEGIN IMMEDIATE`; a test asserts it. Correctness must not rely on that: a second process (migration, `atlas-seed`, restore) can hold the write lock.
- Guard check and mutation share one write transaction; never decide from the read pool.
- Safety net: triggers on `users` abort when no ACTIVE superadmin would remain. The last-ADMIN trigger is evaluated in ORG 1 because `allow_orphan` must bypass it; default is the core check inside `BEGIN IMMEDIATE` plus tests.
- Lock not acquired within `busy_timeout`: 503, nothing applied, no audit or event, guard never skipped.
- One transaction covers the change, revocation of sessions/tokens, audit and change_event. Events go out after commit.
- Status transitions and invitation acceptance are compare-and-set (`WHERE status = expected`, check rows affected).

## Implementation (M1)

- `internal/core/authz.go` decides; the Actor has no privilege flag; `is_admin`, `role_actions` and the roles tables are read only by the data migration (`TestLegacyAuthorizationIsNotRead`).
- Only a SUPERADMIN or an organization ADMIN creates projects; the creator no longer becomes a project admin. `Project.role` is `admin` for inherited management and `member` for explicit members.
- A project without organization is reachable by a SUPERADMIN only; the server refuses to start on an unmigrated database.

## Consequences

- Behaviour change: project admins become members and lose management actions; viewers are handled by the migration review (ADR-0028).
- Tests per ticket: authz unit matrix, 401/403/404 semantics, escalation and concurrency tests (ATL-479, ATL-438, ATL-483, ATL-490).
- Follow-up work: ATL-435 (actor), ATL-436 (`authz.go`), ATL-478 (remove roles).
