# ADR-0008: Settings, rights and templates

- Status: accepted
- Date: 2026-10-27

## Context

Epic SET adds personal settings, project settings, project tokens, administration, templates and per-status creation rules. SET 4 asked for a configurable rights matrix and custom roles, and flagged it as large and structural.

## Decisions

1. **Roles stay fixed (viewer, member, admin) plus global admin and token scopes.** A configurable matrix and custom roles are *not* built. Every check lives in `core.access`; making it data-driven means touching every service and needs a migration of defaults and a test per cell. We show the real matrix read-only (`core.RightsMatrix`, verified against enforcement in `TestRightsMatrixMatchesEnforcement`), so a future configurable version starts from a tested baseline and changes no behaviour.
2. **Account and admin operations are session-only.** A token never changes passwords, lists sessions, creates tokens or administers users. Instance settings are read-only in the UI: they come from the environment (`ATLAS_*`), so there are no database overrides to reconcile.
3. **Project tokens are ordinary tokens limited to one project.** Ownership is the creating admin. Access still resolves through the owner's membership, so a token can never exceed its owner. When the owner is removed from the project, tokens limited to exactly that project are revoked (explicit and permanent instead of only dormant). `last_used_at` is written at most every 5 minutes to spare the single writer.
4. **Deactivation instead of deletion.** Users are deactivated (`deactivated_at`); their audit trail and authored data stay intact.
5. **Templates.** Item templates are per project and only have two placeholders (`{date}`, `{user}`): no scripting. A template part that no longer fits (a removed or blocked status) produces a warning on the created item, never a failed create. Project templates are instance-wide, hold workflow, labels, custom fields and item templates (no items, members or tokens), have a versioned JSON schema, and are applied in one transaction together with the project. Built-ins: Kanban, Bug tracking, Scrum.
6. **`allow_create` per status** follows the pattern of the WIP limit: enforced in `core.CreateItem`, so REST, MCP, import and templates all obey it. Moving into the status stays allowed.

## Consequences

- Not built: configurable rights and custom roles (see decision 1). Registration policy, invites, instance-wide item templates, subitems in templates, profile pictures, email change, account deletion and TOTP 2FA were built afterwards; see ADR-0009.
- Deactivated users keep their rows; re-activating restores access but not their old sessions.
