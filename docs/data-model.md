# Data model

SQLite. One migration file per change in `migrations/`, forward-only, applied automatically at startup in one transaction each.

## Hierarchy

```text
Project > Epic > Story > Task (bug allowed per rules)
```

All item types live in `work_items`. `type` plus configurable hierarchy rules decide valid parent/child combinations. Default rules: epic (root) > story > task; bug may sit under epic or story; max depth 5 (configurable constant).

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
| `users`, `project_members` | Users and per-project role |
| `change_events` | Sync journal: seq (autoincrement), type, project, key, actor, ts |
| `audit_entries` | Actor, operation, target, changes (JSON), batch_id, ts |
| `notifications` | Persistent inbox |
| `api_tokens` | Hashed token, scopes, project limits, expiry, revoked_at |
| `idempotency_records` | key, actor, request hash, stored result, created_at |
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

- `rank` is a lexicographic string key, unique per (project, status) in practice; ties broken by `id`.
- Move = update one item's `rank` to a key between its neighbours.
- If keys exceed a length limit (default 64 chars), rebalance the affected column in one short transaction.
- Required tests: concurrent moves, equal ranks, cross-project moves.

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
