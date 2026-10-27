# MCP contract

Transport: Streamable HTTP at `/mcp` over HTTPS. Official Go MCP SDK. Auth: scoped personal access token (bearer). OAuth is v1.5.
Tools are thin adapters over `core` services (same as REST). Tool names, descriptions and schemas stay stable.

## Tools (first contract, extensible when it measurably reduces calls/tokens/errors)

| Tool | Purpose |
|---|---|
| `projects_list` | Discover projects compactly |
| `projects_write` | Manage projects |
| `workflow_get` | Types, statuses, labels for a project |
| `board_get` | Compact board overview |
| `items_search` | Filter and search items |
| `item_get` | One item in detail (links, checklist, last 10 comments) |
| `items_write` | Create, update, move, batch |
| `comment_add` | Agent updates and comments |
| `checklist_write` | Add, tick, rename, remove checklist items |
| `changes_since` | Delta since a cursor |
| `attachments` | List metadata, link attachments |

Implemented: `projects_list`, `workflow_get`, `board_get` (10 cards per column by default), `items_search`, `item_get`, `items_write` (ops `create`/`update`/`move`, `clear` nulls assignee/due_at/parent), `changes_since`, `comment_add`, `attachments` (metadata and fetch path, never contents), `checklist_write` (add/set/remove, applied one by one), resources `atlas://projects/{key}/board`, `atlas://projects/{key}/workflow`, `atlas://items/{key}` (same compact shapes, same 16 KiB cap, same project scoping). Every tool call is logged with tool, actor, ms, bytes and error flag. Limits: 120 requests/minute per token (HTTP 429 + `Retry-After`), 500 mutations/hour per token (tool error `rate_limited`), 16 KiB per result (tool error `too_large`). Counters are in memory and reset on restart. Not yet: `projects_write`, uploading through MCP (no base64), mass-delete guard (nothing is deletable yet), aggregated metrics (p95, truncation rate).
`items_write` failures are tool errors (`isError`) with `{"error":{"code","message","current"?},"operation":N,"applied":false}`; partial mode returns per-operation `error` entries instead. Idempotency records live 24 h per actor+key (`idempotency_records`).

## Response rules

- Compact by default; human-readable keys (`EXO-42`) and status names.
- `fields` selects returned fields (e.g. `key,title,status,version`).
- Server-side filters: project, type, status, assignee, priority, labels, milestone, parent, updated_since.
- Cursor pagination with `next_cursor`.
- Max response size 16 KiB (configurable). Oversized results are paginated or return an explicit error; never truncated JSON.
- No binary/base64; attachments return metadata and a fetch URL.

```json
{ "project": "EXO", "seq": 1842,
  "items": [ { "key": "EXO-42", "title": "Calendar sync", "status": "Todo", "version": 3 } ],
  "next_cursor": null }
```

## Writes

- Every write accepts `idempotency_key`; replay returns the same result.
- `version` required for updates; conflict returns compact 409 with current version and relevant fields.
- `items_write` batch: max 25 operations; atomic by default, or `partial: true` with per-operation results.
- Every mutation records the actor (`agent:<name>` / `token:<id>`) and `batch_id`.

## Delta sync

`changes_since(cursor)` returns visible changes (including deletes/archives) and a new cursor. Same `seq` as REST/SSE. `resync_required` means re-read via `board_get` / `items_search`.

## Resources

`atlas://projects/EXO/board`, `atlas://projects/EXO/workflow`, `atlas://items/EXO-42`. Offered only where clients can use them; they cost tokens when shown to a model.

## Safety

Scopes: read and write separately, optionally per project. Rate limit per token; write quota (default 500 mutations/hour); mass-delete guard (deleting more than N items in one call/hour needs explicit confirmation flag); instant revocation.

## Measured

Tool catalog size, response bytes, calls per user action, mean/p95 latency, % paginated/truncated, optional estimated tokens.
