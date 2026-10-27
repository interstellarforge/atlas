# Atlas architecture

Status: proposed v2.0. Lightweight by default. Powerful when needed. AI-native by design.

## Principles

1. Resource efficiency: minimal RAM, CPU, disk, infrastructure.
2. Token efficiency: agents get only the context they need.
3. Realtime by default: every client sees changes immediately, also agent changes.
4. One source of truth: UI, REST and MCP share business logic and database.
5. Simple to operate: one Docker container, automatic migrations, simple backups, no external DB.

Out of scope: Jira clone, ERP, document management, AI-agent framework. A feature must show value before it is built.

## Stack

| Component | Choice |
|---|---|
| Backend | Go |
| Frontend | Svelte 5 + TypeScript + Vite |
| Database | SQLite (WAL) |
| Realtime | Server-Sent Events |
| API | REST/JSON, `/api/v1` |
| AI | Native MCP, official Go SDK, Streamable HTTP, `/mcp` |
| Files | Filesystem `/uploads` + on-demand thumbnails |
| Search | SQL; FTS5 later if measured need |
| Auth | Sessions + scoped API tokens |
| Deploy | One container |
| License | See ADR-0001 |

## Runtime

```text
Browser (Svelte SPA) --REST+SSE--> ATLAS SERVER (one Go binary) <--MCP-- Claude/Codex
   /api/v1  /api/v1/events  /mcp
   auth -> services -> business rules -> change journal
                 |                 |
              SQLite           Filesystem
             atlas.db           /uploads
```

- Frontend is compiled in the Docker build; the Go binary embeds and serves it.
- Production runs no Node, Redis, PostgreSQL or message queue.
- One process; internal modules keep clear boundaries. No microservices.

## Module boundaries (`internal/`)

| Module | Responsibility | May depend on |
|---|---|---|
| `core` | Business services and rules (the only place for domain logic) | `db`, `events` |
| `db` | Connections, migrations, queries | - |
| `events` | Change journal read side, SSE hub | `db` |
| `api` | REST/SSE handlers; thin adapters over `core` | `core`, `auth`, `events` |
| `mcp` | MCP tools/resources; thin adapters over `core` | `core`, `auth` |
| `auth` | Sessions, tokens, scopes, actor context | `db` |
| `media` | Upload validation, thumbnails | `db` |
| `notifications` | Inbox creation and read state | `core`, `db` |
| `backup` | Full backup and restore of the data directory (CLI only) | `db` |
| `scheduler` | Built-in jobs, outbox processing (v1.5) | `core` |

Rules:

- `api` and `mcp` never contain business logic and never touch `db` directly for domain data.
- Every mutation goes through a `core` service with an actor context.
- No abstractions for hypothetical future needs.

## Mutation flow

```text
UI / REST / MCP -> core service -> SQLite tx { change item, audit, change_event } -> commit -> SSE notify
```

Events are sent only after a successful commit. The persistent `change_events` table is authoritative, not the in-memory SSE hub.

## Decisions

Recorded as ADRs in `docs/decisions/`. Open items to decide via ADR: license, SQLite driver, materialized path vs recursive CTE (benchmark), rank-key algorithm.

## Related docs

[data-model](data-model.md), [api-contract](api-contract.md), [mcp-contract](mcp-contract.md), [security](security.md), [performance](performance.md).
