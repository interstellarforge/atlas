# Atlas agent instructions

Lightweight, MCP-native project management. Go + SQLite + Svelte 5, one container. Read only the docs relevant to your ticket: `docs/architecture.md` first, then the specific contract.

## Commands

- Backend: `go build ./...`, `go vet ./...`, `go test ./...`
- Frontend (Node 24, pnpm): `pnpm --dir web install`, then `typecheck`, `lint`, `format`, `build`
- Run: `ATLAS_ADMIN_EMAIL=… ATLAS_ADMIN_PASSWORD=… ATLAS_COOKIE_SECURE=false docker compose up --build`
- Seed 10.000 items: `go run ./cmd/atlas-seed -data-dir ./data`
- Benchmarks: `ATLAS_BENCH=1 go test ./benchmarks -run Baseline -v`, `benchmarks/rss.sh`
- Config env: `ATLAS_ADDR`, `ATLAS_DATA_DIR`, `ATLAS_LOG_LEVEL`, `ATLAS_SYNCHRONOUS` (FULL|NORMAL), `ATLAS_COOKIE_SECURE`

## Rules

- Work in scoped tickets with acceptance criteria (Taiga project Atlas).
- API and MCP are thin adapters over `internal/core`. No business logic or duplicated logic elsewhere.
- Every mutation: actor context, one SQLite transaction writing item + audit + change_event, event sent after commit.
- Tests are mandatory for concurrency, permissions and data integrity. Keep other tests minimal and meaningful.
- Measure performance before optimizing; targets in `docs/performance.md`.
- No abstractions for hypothetical future needs. No new dependency for a few lines.
- No hardcoded user-facing text in the web app; use the locale JSON files.
- Log failures compactly with relevant context. Never log secrets.
- Update docs and add an ADR (`docs/decisions/`) on structural changes.
- Do not commit, push or tag unless asked.

## Layout

`cmd/atlas` entrypoint, `internal/{api,auth,core,db,events,mcp,media,notifications,scheduler}`, `migrations`, `benchmarks`, `web`, `docs`.
