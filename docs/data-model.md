# Data model

SQLite. One migration file per change in `migrations/`, forward-only, applied automatically at startup in one transaction each.

## Hierarchy

```text
Project > Epic > Story > Task (bug allowed per rules)
```

All item types live in `work_items`. `type` plus hierarchy rules decide valid parent/child combinations. Implemented rules (a constant in `internal/core`, not yet per-project): an item may have no parent; epic has no parent type; story under epic; task under story; bug under epic or story. Max depth 5.

Subtree reads use a range scan on `path`, see ADR-0003. 
## Tables

| Table | Purpose |
|---|---|
| `projects` | id, key (unique, e.g. EXO), name, settings, next_item_number, archived_at |
| `work_items` | Epics, stories, tasks, bugs |
| `project_statuses` | Per-project statuses: id, project_id, name, position, color_group, category, wip_limit |
| `workflow_transitions` | Optional allowed from_status -> to_status per project |
| `item_links` | type: blocks / relates / duplicates; from_id, to_id |
| `labels`, `item_labels` | Labels per project |
| `comments` | Human and agent comments (actor stored) |
| `checklist_items` | item_id, text, done, position |
| `milestones` | Releases and milestones |
| `attachments` | Metadata: item_id, filename, mime, size, width, height, path, created_by |
| `custom_field_definitions` | Typed fields per project: name, type, default, validation |
| `users`, `project_members` | Users and per-project role (a name in `roles`) |
| `roles`, `role_actions` | Project roles and the actions each grants (built-ins seeded, custom roles added by admins) |
| `change_events` | Sync journal: seq (autoincrement), type, project, key, actor, ts |
| `audit_entries` | Actor, operation, target, changes (JSON), batch_id, ts |
| `notifications`, `item_follows` | Per-user inbox (kept until read or deleted) and followed items. `change_events.target_user` limits a signal to one user |
| `api_tokens` | Hashed token, scopes, project limits, expiry, revoked_at |
| `idempotency_records` | key, actor, request hash, stored result, created_at |
| `item_templates` | Per project: name, type, title prefix, description, priority, labels, status, checklist |
| `project_templates` | Instance-wide: name, description, JSON (schema version 1); built-ins Kanban, Bug tracking, Scrum |
| `meta` | schema version, database generation id |

Status category enum: `todo`, `active`, `completed`.

## work_items fields

`id`, `key` (`EXO-42`, unique), `project_id`, `parent_id` (canonical), `path`, `type`, `title`, `description`, `status_id`, `priority`, `assignee_id`, `due_at`, `milestone_id`, `rank`, `custom_data` (JSON), `version`, `archived_at`, `created_at`, `updated_at`.

- `version` increments on every update (optimistic locking).
- Timestamps UTC, stored as integer unix milliseconds.
- Priority: small enum (`low`, `normal`, `high`, `urgent`).

## Materialized path

- `parent_id` is canonical; `path` is derived and indexed. Example: `/12/40/42/`.
- Subtree = `path LIKE '/12/40/%'` (prefix query, uses index).
- Re-parenting rewrites descendant paths in one transaction.
- Consistency check (`atlas check paths`) rebuilds paths from `parent_id` and reports diffs.
- Open decision: benchmark against indexed recursive CTE. Keep `path` only if the benefit is measurable (see performance.md).

## Fractional ordering

- `rank` is a lexicographic string key (digits `0-9A-Za-z`, never ending in `0`); the order of a column is `ORDER BY rank, id` among non-archived items of one (project, status). The board index is `(project_id, status_id, rank)`.
- A move writes one new key between its neighbours (`internal/core/rank.go`). New items and status changes append to the column end.
- Keys grow slowly when appending or prepending; past 24 characters, or when a move meets equal keys, the column is rebalanced to short, evenly spaced keys in the same transaction (`board.rebalanced` event). See ADR-0004.
- Tested: concurrent moves, equal ranks, neighbours from another project or column, long append runs.

## Custom fields

- `custom_data` JSON validated against `custom_field_definitions` (type, required, default, allowed values).
- No EAV. Add expression indexes only for fields proven hot.

## Change journal and audit

- Every mutation writes `work_items` change, `audit_entries` and `change_events` in the same transaction.
- Actor format: `human:user-4`, `agent:claude`, `token:pat-17`.
- `change_events` retention configurable; clients older than retention receive `resync_required`.
- `meta.generation` changes after a restore, forcing full client resync.

## Migration rules

1. Forward-only, numbered `NNNN_name.sql`. Never edit an applied migration.
2. Each migration runs in a transaction; startup fails loudly if one fails.
3. Run a lightweight `PRAGMA quick_check` at startup; full `integrity_check` is manual/periodic.
4. Destructive changes (drop/rename column) use expand/contract across releases.
5. Every migration that changes `work_items` keeps a test that the seed dataset still loads.
6. Back up the database file automatically before applying pending migrations.

## Settings, rights and templates (migration 0010)

- `users.deactivated_at` (null = active; sign-in, sessions and tokens stop at once), `users.preferences` (JSON: theme, default view, muted notification types).
- `api_tokens.last_used_at`, written at most every 5 minutes per token.
- `project_statuses.allow_create` (default 1).
- A project token is an `api_tokens` row whose `projects` is exactly `["KEY"]`; it is owned by the admin who created it.

## Accounts, policy and templates (migration 0011)

- `users`: `avatar_path`, `totp_secret`, `totp_pending`, `totp_enabled`, `totp_last_step` (replay protection). `recovery_codes` holds SHA-256 hashes, single use.
- `instance_settings` (key/value): `min_password_length`, `registration`, `allowed_domain`, `session_days`; absent keys use the defaults in `core/policy.go`. The environment-only settings (`ATLAS_*`) are unchanged.
- `invites`: code hash, optional email, admin flag, expiry, used_at/used_by.
- `item_templates.project_id` is nullable (NULL = instance wide), `subitems` holds the JSON tree. A unique index on `(COALESCE(project_id, 0), name)` replaces the old unique constraint.

## Required steps and merged columns (migration 0014)

- `status_requirements (status_id, required_status_id)`: an item may enter `status_id` only if it is in, or has been in, every required status. Empty = no rule.
- `item_status_visits (item_id, status_id)`: statuses an item has been in, written with the status change. Backfilled from the current status and the audit log (by name).
- `projects.merge_columns` (default 0): statuses of one category share a board column. Ranks stay per item; the rank queries work on the set of statuses of a column. See ADR-0011.

## Email (migration 0015)

- `mail_settings` (one row, `id = 1`): SMTP host, port, security, username, `password_enc` (AES-GCM, key in `<data dir>/mail.key`), from address and name. No row = environment defaults.
- `mail_outbox`: mails to send, written in the transaction of the mutation that causes them; `attempts`, `next_attempt_at`, `sent_at`, `failed_at`, `last_error` (redacted). The body is blanked once sent or abandoned. See ADR-0012.
- Per-user switch: `email_notifications` inside `users.preferences`.
