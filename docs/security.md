# Security model

## Authentication

- UI: server-side sessions, `HttpOnly`, `Secure`, `SameSite=Lax` cookie; CSRF token on state-changing requests.
- Passwords: argon2id; login rate limiting.
- API/MCP: personal access tokens, shown once, stored hashed (SHA-256 of a high-entropy token), prefix stored for identification, optional expiry, revocable at runtime without restart.
- HTTPS required for MCP and non-local deployments (terminated by the user's reverse proxy or built-in TLS).

## Authorization

- Roles per project via `project_members` (`admin`, `member`, `viewer`).
- Token scopes: `read` and `write` separately, optionally limited to projects. A token never exceeds its owner's rights.
- Enforced in `core` services, not only in handlers. SSE and `changes_since` filter by permission.
- Required tests: permission matrix for read/write/other-project access, token scope, revoked token.

## Agent safety

- Rate limit per token; write quota (default 500 mutations/hour, configurable).
- Idempotency on every write; optimistic locking via `version`.
- Mass-delete protection (threshold plus explicit confirmation); prefer archive over delete.
- Full audit trail: actor, operation, target, changes, batch id, timestamp.
- Undo of agent batches is v1.5; store reversible inverse data then, audit alone is not enough.

## Input and output

- Validate at trust boundaries: body size, JSON schema, enum values, hierarchy/workflow rules.
- Parameterized SQL only.
- Markdown rendered client-side with raw HTML off; if HTML is enabled, sanitize with DOMPurify.
- Content-Security-Policy; no inline scripts; security headers set by server.

## Uploads

- Allowed: PNG, JPEG, WebP; max 10 MB and 20 MP (configurable).
- Check size, magic bytes/MIME and extension; read dimensions without full decode; reject over-limit before decoding.
- Store under generated names (never user-supplied paths); serve through authorized streaming endpoints with `X-Content-Type-Options: nosniff`.
- Thumbnail generation single-flight with memory measured before release.

## Data protection

- SQLite and uploads on local disk with restrictive permissions; backups contain secrets-free data (tokens hashed).
- Logs never contain tokens, passwords or full request bodies.
- Dependencies pinned; Go vulnerability scan (`govulncheck`) in CI.

## Accounts, sessions and administration (epic SET)

- Password and session operations, tokens, and the admin API need a user session: a token cannot change a password, list sessions, mint tokens or administer users.
- Changing a password requires the current one and ends every other session. Passwords, token secrets and hashes never enter the audit log (tested).
- Deactivating a user deletes their sessions and makes their tokens fail; sign-in is refused. The last active admin cannot be demoted or deactivated, and nobody deactivates themselves.
- Project tokens never exceed their owner: access still goes through the owner's membership. When the owner leaves the project, those tokens are revoked.
- The rights matrix shown in the admin UI is the code's own table (`core.RightsMatrix`) and a test checks it against real requests for viewer, member and admin. See ADR-0008.
