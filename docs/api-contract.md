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
| GET | `/projects/{key}/export` | Download a project: JSON (default; workflow, items with checklist, comments, links, attachment metadata, archived items included) or `?format=csv` (key,type,title,description,status,priority,parent,labels,due_at,milestone,archived). Needs read access |
| POST | `/projects/{key}/import` | Import items into the project in the path (the explicit project mapping; it must exist, needs member role). Body: `{"items":[...]}` (an export file works) or `?format=csv`. Rows carry source `key` and `parent` (a row key or an existing item). `?dry_run=1` validates with the real create rules and writes nothing; `?allow_duplicates=1` imports rows whose type, title and parent already exist (default: skipped and reported). Any row error rejects the whole file (`applied:false`). Report: `created`, `duplicates`, `errors` (row, message), `mapping` (source key to new key). Max 20.000 rows, 32 MiB. Imports items only: comments, checklists, links, attachments and assignees are not imported |
| PATCH | `/me` | Own profile: `{"name"}` (session only) |
| PUT | `/me/password` | `{"current","new"}` (min. 8): needs the current password, signs out every other session. Never logged |
| GET, DELETE | `/me/sessions`, `/me/sessions/{id}` | Own active sessions (`current` marks this one); sign one out |
| GET, PUT | `/me/preferences` | `{"theme": system/light/dark, "default_view": board/list/backlog/tree, "muted_notifications": [assigned, mention, blocker_done, followed]}`; muted types create no notification |
| GET, POST, DELETE | `/projects/{key}/tokens[/{id}]` | Tokens limited to exactly this project (project admin, session only). POST `{"name","write","expires_at"?}`; secret shown once; list shows owner, scopes, `last_used_at`, expiry. Owner leaves the project: tokens are revoked |
| GET, POST, DELETE | `/projects/{key}/item-templates[/{id}]` | Item templates (read: viewer; write: project admin). POST upserts by name: `{"name","type","title_prefix","description","priority","labels","status","checklist","subitems"}`; `subitems` is a tree `[{"type","title","description"?,"priority"?,"children"?}]` (3 levels, 50 items, hierarchy rules checked at save) created with the item in one transaction. The list includes instance-wide templates (`scope`). Placeholders `{date}`, `{user}` only |
| GET, POST, DELETE | `/project-templates[/{id}]` | Project templates (read: any user; write: global admin). POST `{"name","description"}` plus `"from_project": "KEY"` or `"data": {version:1, statuses, transitions, labels, custom_fields, item_templates}`. `POST /projects` takes `"template": id` and applies it in one transaction |
| GET | `/admin/users` | Global admin, session only |
| PATCH | `/admin/users/{id}` | `{"is_admin"?, "active"?}`; no self-deactivation, the last active admin stays; deactivating ends sessions, tokens stop working |
| GET | `/admin/audit` | Audit log newest first; filters `actor`, `operation` (prefix), `target`, `before` (id), `limit` (max 200) |
| GET | `/admin/instance` | Effective instance settings (read-only, from the environment) |
| GET | `/roles` | Project roles with their actions, plus the list of action names |
| PUT, DELETE | `/admin/roles/{name}` | Global admin only. PUT `{"actions":[...]}` creates a custom role or replaces the actions of viewer, member or a custom role (`admin` is fixed, `view` is mandatory). DELETE refuses built-ins and roles still assigned |
| POST | `/auth/register` | `{"email","name","password","invite"?}`: with a valid invite, or when the instance policy is `open` (optionally one email domain). Refusals count against the login rate limiter. Closed instance without invite: 403 |
| GET | `/auth/policy` | Public: `registration` (closed/invite/open), `min_password_length`, `allowed_domain` |
| POST | `/auth/login` | Also accepts `code` (authenticator code or recovery code). With 2FA on and no code the answer is 401 `totp_required`; a used code cannot be reused |
| PUT | `/me/email` | `{"email","password"}`: needs the password, shape and uniqueness checked (no confirmation mail), audited |
| DELETE | `/me` | `{"password"}`: closes the own account for good (anonymised, sessions/tokens/memberships removed; refused for the only instance admin or the only admin of a project) |
| PUT, DELETE | `/me/avatar` | Multipart `file`, same limits and checks as attachments; remove |
| GET | `/users/{id}/avatar` | 320 px JPEG thumbnail for any signed-in reader (metadata such as EXIF is dropped); 404 without a picture |
| GET | `/me/security` | `{totp, recovery_remaining}` |
| POST | `/me/totp/setup`, `/me/totp/enable`, `/me/totp/disable` | Enrol: setup returns `secret` and `otpauth` `uri`; enable `{"code"}` returns 10 recovery codes once; disable `{"password","code"}` (a code or recovery code) |
| GET, PUT | `/admin/policy` | `{"min_password_length": 8-128, "registration", "allowed_domain", "session_days": 1-90}` (global admin) |
| GET, POST, DELETE | `/admin/invites[/{id}]` | POST `{"email"?,"is_admin","days"}` returns the `code` once; single use; with an email only that address may redeem it |
| GET | `/admin/audit/export` | CSV (default) or `?format=json` (JSON lines) with the filters of `/admin/audit`; at most 50.000 entries per download (continue with `before`) |
| GET, POST, DELETE | `/item-templates[/{id}]` | Instance-wide item templates (read: any user; write: global admin); usable in every project, a project template of the same name wins |
| GET | `/mcp/stats` | Aggregate MCP call metrics per tool (calls, errors, bytes, p95 ms, paginated, too_large); in memory, reset on restart |
| GET/POST/PATCH | `/projects`, `/projects/{key}` | Projects |
| GET/POST, PATCH/DELETE `/projects/{key}/milestones[/{id}]` | Milestones with progress (`total`, `done` = active items in a completed status). Changes need a project admin; PATCH `{"name"?,"due_at"?,"released"?}`; delete refused while items use it. Items get one via `milestone` (name or null) on create/update; filter with `?milestone=` |
| GET/PUT | `/projects/{key}/workflow` | GET: types, hierarchy, statuses, transitions, labels, milestones, custom fields. PUT `{"transitions":[{"from","to"}]}` replaces the rules; empty list allows every change |
| POST/PATCH/DELETE | `/projects/{key}/statuses[/{id}]` | Manage statuses (project admin); delete refused while items use it. PATCH accepts `allow_create` (false: items cannot be created directly in this status, moving in stays allowed; at least one status must stay creatable). Create in a blocked status (also the default one) fails with 422 `rule_violation`, over REST, MCP and import |
| POST/DELETE | `/projects/{key}/custom-fields[/{name}]` | Custom field definitions (project admin) |
| GET | `/projects/{key}/members`, PUT/DELETE `/projects/{key}/members/{user}` | Role: any name from `GET /roles` (built-in admin, member, viewer) |
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
| GET | `/notifications?unread=1&limit=` | The caller's inbox: `{items, unread}`; types `assigned`, `mention` (`@` + the part of the email before the @, in a comment), `blocker_done` (a blocker moved to a completed status; detail is its key) and `followed` (detail is the event type) |
| POST | `/notifications/read`, `/notifications/{id}/read` | Mark all / one read (write scope) |
| DELETE | `/notifications/{id}` | Remove one |
| GET/PUT/DELETE | `/items/{key}/follow` | Follow state of the caller; following notifies on updates, moves, archive and comments |
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
- Types now: `item.created`, `item.updated`, `item.moved`, `item.archived`, `project.created`, `project.updated`, `board.rebalanced` (clients refetch the board). `comment.added`, `checklist.changed`, `attachment.added` (carry the item key) and `notification.created` (only sent to its recipient; refetch `/notifications`). Planned: `item.deleted`, `notification.created`.
- Control events: `hello` (`{"generation","seq"}`, first frame; a changed `generation` means the database was replaced, so reload), `resync_required` (`{"seq"}`).
- `Last-Event-ID` (or `?since=`) resumes from `change_events`. A cursor ahead of the journal or behind its oldest entry gives `resync_required`. Retention: `ATLAS_EVENT_RETENTION_DAYS` (default 7), always keeping the newest 1000.
- Keepalive comment every 20 s. Events are filtered by the caller's project access. A stream ends after 10 minutes so credentials are re-checked on reconnect.
- Clients fetch only changed items after an event. `GET /changes?since=<seq>&limit=` returns the same events as JSON: `{"events":[…],"seq","resync_required","more"}`.

## Limits (configurable)

Body 1 MiB (uploads separate); upload 10 MB; batch 25; page size 200.
