# REST API contract

Base: `/api/v1`. JSON. UI, REST and MCP call the same `core` services.

## Conventions

- Items are referenced by key (`EXO-42`); numeric ids are accepted too.
- Auth: session cookie (UI) or `Authorization: Bearer <token>`.
- Mutations accept `Idempotency-Key` header; replay returns the original result.
- Updates require `version` (body or `If-Match`); mismatch returns 409.
- Field selection: `?fields=key,title,status,version`. Default responses are compact.
- Pagination: `?limit=` (default 50, max 200) and opaque `?cursor=`; response has `next_cursor` (null at end).
- Filters (server-side): `project`, `type`, `status`, `assignee`, `priority`, `label`, `milestone`, `parent`, `updated_since`, `archived`.

## Endpoints (v1)

| Method | Path | Purpose |
|---|---|---|
| GET | `/health` | Liveness |
| POST | `/auth/login`, `/auth/logout` | Sessions |
| GET/POST | `/tokens`, DELETE `/tokens/{id}` | Scoped API tokens |
| GET/POST/PATCH | `/projects`, `/projects/{key}` | Projects |
| GET/PUT | `/projects/{key}/workflow` | GET: types, hierarchy, statuses, transitions, labels, custom fields. PUT `{"transitions":[{"from","to"}]}` replaces the rules; empty list allows every change |
| POST/PATCH/DELETE | `/projects/{key}/statuses[/{id}]` | Manage statuses (project admin); delete refused while items use it |
| POST/DELETE | `/projects/{key}/custom-fields[/{name}]` | Custom field definitions (project admin) |
| GET | `/projects/{key}/members`, PUT/DELETE `/projects/{key}/members/{user}` | Roles: admin, member, viewer |
| GET/POST | `/users` | List users; create (global admin) |
| GET | `/projects/{key}/board` | Compact board, paginated per column (KAN) |
| GET/POST | `/items` | Search/list, create. List filters also: `under` (subtree), `q` (title), `archived=1` |
| GET/PATCH | `/items/{key}` | Read; update (needs `version` in body or `If-Match`). Archive via `{"archived": true}`; there is no hard delete |
| POST | `/items/{key}/move` | Status and/or position (neighbour keys) |
| POST | `/items/batch` | Up to 25 mutations |
| GET/POST | `/items/{key}/comments` | Comments |
| GET/POST/PATCH | `/items/{key}/checklist` | Checklist items |
| GET/POST | `/items/{key}/links` (POST `{"type","target"}`), DELETE `/items/{key}/links/{type}/{target}` | Relations; `blocks` cycles are refused |
| GET/POST | `/items/{key}/attachments` | Metadata / upload |
| GET | `/attachments/{id}` , `/attachments/{id}/thumb` | Streaming download |
| GET | `/changes?since=<seq>` | Delta sync |
| GET | `/events` | SSE |
| GET | `/notifications`, POST `/notifications/{id}/read` | Inbox |
| GET | `/export`, POST `/import` | Export / import (dry-run via `?dry_run=1`) |

## Errors

```json
{ "error": { "code": "conflict", "message": "version mismatch", "current": { "key": "EXO-42", "version": 4, "status": "Review" } } }
```

| HTTP | code |
|---|---|
| 400 | `invalid` |
| 401 | `unauthenticated` |
| 403 | `forbidden` |
| 404 | `not_found` |
| 409 | `conflict` (includes current version and relevant fields) |
| 413 | `too_large` |
| 422 | `rule_violation` (hierarchy, workflow, WIP hard limit if enabled) |
| 429 | `rate_limited` (with `Retry-After`) |

## SSE `/events`

- Event: `{"seq":1842,"type":"item.updated","project":"EXO","key":"EXO-42"}`.
- Types: `item.created`, `item.updated`, `item.moved`, `item.archived`, `item.deleted`, `comment.added`, `notification.created`, `resync_required`.
- `Last-Event-ID` resumes from `change_events`. Too far behind or new database generation: `resync_required`.
- Keepalive comment every 20 s. Events filtered by the caller's permissions.
- Clients fetch only changed items after an event.

## Limits (configurable)

Body 1 MiB (uploads separate); upload 10 MB; batch 25; page size 200.
