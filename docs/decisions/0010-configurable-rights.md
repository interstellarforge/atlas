# ADR-0010: Configurable rights and custom roles

- Status: accepted
- Date: 2026-10-27
- Supersedes: decision 1 of ADR-0008

## Context

SET 4 asks admins to decide who may do what. ADR-0008 only showed a read-only matrix. We now build the configurable version, starting from that tested baseline.

## Decisions

1. **Roles are data.** `roles` (name, builtin) and `role_actions` (role, action). `project_members.role` is a role name; its CHECK constraint is dropped (SQLite needs a table rebuild, done in migration 0012) and `core.SetMember` validates against `roles`.
2. **Actions are the existing ten matrix rows** (view, export, item.create, item.update, comment, import, workflow, project.update, members, tokens). Every `core` check names an action: `access(q, actor, project, action)` replaces the old minimum role. Token scope still applies first: any action except view and export needs write scope.
3. **Defaults equal the old behaviour**, so there is no regression: viewer = view, export; member adds item.create, item.update, comment, import; admin = all.
4. **Guard rails.** `admin` is built in, fixed and cannot be deleted, so a project can always be administered and `guardLastAdmin` keeps working. Every role keeps `view`. Built-ins cannot be deleted; a role still assigned to members cannot be deleted. A role holding `members` cannot grant, change or remove `admin` (no self-escalation). Global admins keep every action on every project.
5. **Instance-wide only, edited by global admins through a human session.** Role changes are audited (`role.save`, `role.delete`) and apply at once (the role is read per request, one indexed lookup).

6. **Default role for new members** is an instance policy (`default_role`, default `member`). `SetMember` with an empty role uses it, and the add-member form preselects it. It must be an existing role and cannot be deleted while it is the default.
7. **Forced password change.** `users.must_change_password` (migration 0013). An admin sets it with `PATCH /admin/users/{id}` `{"force_password_reset": true}`: every session of the user ends at once. The next session works only for `GET /auth/me`, `POST /auth/logout` and `PUT /me/password` (`auth.Require` answers 403 `password_change_required` elsewhere); changing the password clears the flag. API tokens keep working: they are separate credentials, revoked on their own.

## Consequences

- Not built: per-project overrides of the matrix (the story called them optional). Add `project_role_actions` later if needed; `access` is the single place to change.
- A role with `item.update` but not `view` is impossible by design.
- Action names are part of the API; adding an action means a migration that grants it to the right roles.
