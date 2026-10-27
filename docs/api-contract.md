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
| GET | `/projects/{key}/board` | Every status column with `count`, `over_wip` and the first `limit` cards (default 50, max 200), plus `seq` (journal position to subscribe from). Filters: `type`, `priority`, `label`, `assignee`, `q`, `archived=1`; `fields` selects card fields |
| GET | `/projects/{key}/board/columns/{statusId}?cursor=` | Further cards of one column |
| GET | `/board?projects=A,B` | Cross-project overview: three columns by status category (todo, active, completed), no WIP limits, read-only. Same filters |
| GET/POST | `/items` | Search/list, create. Filters also: `under` (subtree), `q` (title), `category`, `projects=A,B`, `archived=1`. `sort`: `key` (default, keyset paging), `updated`, `priority`, `due`, `rank` (offset paging). `fields` may include `root` (top-level ancestor) |
| GET/PATCH | `/items/{key}` | Read; update (needs `version` in body or `If-Match`). Archive via `{"archived": true}`; there is no hard delete |
| POST | `/items/{key}/move` | `{"version","status"?,"after"?,"before"?}`: status and/or position between neighbour keys (same project and target column, else 422). Omitting both neighbours appends. Conflicts return 409 |
| POST | `/items/batch` | Up to 25 mutations |
| GET/POST | `/items/{key}/comments` | Comments, oldest first; body is Markdown (rendered client-side, raw HTML off), max 10.000 characters |
| GET/POST, PATCH/DELETE `/items/{key}/checklist[/{id}]` | Checklist items (max 100); PATCH `{"text"?,"done"?}`. Changes do not bump the item version; event type `checklist.changed` |
| GET/POST | `/items/{key}/links` (POST `{"type","target"}`), DELETE `/items/{key}/links/{type}/{target}` | Relations; `blocks` cycles are refused |
| GET/POST | `/items/{key}/attachments` | Metadata / upload (multipart, part `file`; PNG, JPEG, WebP; max 10 MB and 20 MP by default; 400 for wrong type or content, 413 over the limits; max 50 per item). Names are generated; the original name is kept for display |
| GET | `/attachments/{id}` , `/attachments/{id}/thumb` | Authorised streaming download (`nosniff`, sandbox CSP); thumbnail is a JPEG with longest edge 320 px, generated on first request. No delete yet |
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

- Frame: `id: 1842` + `data: {"seq":1842,"type":"item.updated","project":"EXO","key":"EXO-42"}` (default message event).
- Types now: `item.created`, `item.updated`, `item.moved`, `item.archived`, `project.created`, `project.updated`, `board.rebalanced` (clients refetch the board). `comment.added`, `checklist.changed` and `attachment.added` (carry the item key). Planned: `item.deleted`, `notification.created`.
- Control events: `hello` (`{"generation","seq"}`, first frame; a changed `generation` means the database was replaced, so reload), `resync_required` (`{"seq"}`).
- `Last-Event-ID` (or `?since=`) resumes from `change_events`. A cursor ahead of the journal or behind its oldest entry gives `resync_required`. Retention: `ATLAS_EVENT_RETENTION_DAYS` (default 7), always keeping the newest 1000.
- Keepalive comment every 20 s. Events are filtered by the caller's project access. A stream ends after 10 minutes so credentials are re-checked on reconnect.
- Clients fetch only changed items after an event. `GET /changes?since=<seq>&limit=` returns the same events as JSON: `{"events":[…],"seq","resync_required","more"}`.

## Limits (configurable)

Body 1 MiB (uploads separate); upload 10 MB; batch 25; page size 200.
