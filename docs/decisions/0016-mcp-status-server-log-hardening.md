# ADR-0016: MCP connection status, server log window and hardening

- Status: accepted
- Date: 2026-10-27

## Context

Admins could not see whether agents reach `/mcp`, who they are, or what the server is logging without shell access. The README tab was hidden for projects without a linked repository, so it looked missing. A few cheap hardening gaps remained (no panic recovery, no header-size cap, no MCP body cap, missing browser headers).

## Decisions

1. **"Connected" means recently active.** `/mcp` is stateless HTTP: there is no socket to watch. `mcp.Metrics` keeps one entry per actor (token or user): token name and owner, `clientInfo` (name and version) when the client sends it, User-Agent, first and last seen, requests, calls, errors, last tool. A client is `connected` when its last request is under 5 minutes old. At most 200 clients (the stalest is evicted) and the last 200 tool calls (ring, newest first) are kept. Memory only, reset on restart, like the other MCP counters.
2. **Admin only.** `GET /api/v1/mcp/stats` now returns status, start time, connected count, clients and recent calls besides the per-tool aggregate, and answers 403 unless the caller is a human admin: the list names tokens and owners.
3. **Server log window.** `internal/logbuf` wraps the slog handler and keeps the last 1000 records in a ring. `GET /api/v1/admin/logs?level=&q=&limit=` (admin, human session) returns them newest first. stdout stays the durable log; the window is a convenience, not an archive. Existing rule holds: never log secrets. Failed MCP calls now log at warn with the error code.
4. **UI.** Admin gets two sections, "Server log" and "MCP", refreshing every 10 s. The README tab is always shown; without a linked repository it says how to link one.
5. **Hardening.** Panics in handlers are recovered (logged with request id and stack, 500 JSON when nothing was written yet). `MaxHeaderBytes` 64 KiB. MCP requests are capped at 1 MiB. Added `Permissions-Policy`, `Cross-Origin-Opener-Policy: same-origin` and, when `ATLAS_COOKIE_SECURE` is on, `Strict-Transport-Security`.

## Not built

Persistent or searchable log storage, log shipping, alerting, per-client revocation from the MCP page (revoke tokens in the token pages), a global write timeout (event streams are long-lived).
