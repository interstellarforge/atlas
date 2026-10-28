# Security model

## Authentication

- UI: server-side sessions, `HttpOnly`, `Secure`, `SameSite=Lax` cookie; CSRF token on state-changing requests.
- Passwords: argon2id; login rate limiting.
- API/MCP: personal access tokens, shown once, stored hashed (SHA-256 of a high-entropy token), prefix stored for identification, optional expiry, revocable at runtime without restart.
- HTTPS required for MCP and non-local deployments (terminated by the user's reverse proxy or built-in TLS).

## Authorization

- An admin can force a password change (`must_change_password`): sessions end, the next one may only reach `/auth/me`, logout and `/me/password`. Tokens are not touched. See ADR-0010.
- Roles per project via `project_members`: built-in `admin` (fixed, all actions), `member` and `viewer` (editable) plus custom roles. Each role is a set of actions in `role_actions` (ADR-0010); every project check in `core` names an action through `Service.access`.
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
- Dependencies pinned; Go vulnerability scan (`govulncheck`) and `pnpm audit --prod` run in CI (`.github/workflows/ci.yml`). pnpm is pinned through `packageManager`; container base images use tags, not digests.

## Audit hardening (epic SEC, ADR-0017)

- Creating users is a human-admin session action, journaled like other mutations (`user.create`); tokens, also admin tokens, get `forbidden`.
- A token lists only users who share a project it may access, without the global admin flag; avatars follow the same rule.
- Login and registration throttles key on the connection address. Behind a reverse proxy set `ATLAS_TRUSTED_PROXIES`; only then is `X-Forwarded-For` read, from the right, skipping trusted hops.
- The HTTP server has a 2 minute `ReadTimeout`; JSON bodies must hold exactly one document.
- The CSP forbids inline scripts; the theme bootstrap is `/theme-init.js`.
- MCP errors are mapped to stable codes; unexpected errors are logged server-side and reported as `internal error`.
- Audit entries holding e-mail addresses (`account.email`, matching `invite.create`) are scrubbed when the account is deleted.
- CSV exports prefix cells that start with `= + - @`, tab or CR with `'`; the CSV import removes that prefix.
- The README fetcher also refuses documentation, benchmarking, reserved and 0.0.0.0/8 ranges.

## Accounts, sessions and administration (epic SET)

- Password and session operations, tokens, and the admin API need a user session: a token cannot change a password, list sessions, mint tokens or administer users.
- Changing a password requires the current one and ends every other session. Passwords, token secrets and hashes never enter the audit log (tested).
- Deactivating a user deletes their sessions and makes their tokens fail; sign-in is refused. The last active admin cannot be demoted or deactivated, and nobody deactivates themselves.
- Project tokens never exceed their owner: access still goes through the owner's membership. When the owner leaves the project, those tokens are revoked.
- The rights matrix in the admin UI is the `roles`/`role_actions` data that `core.access` enforces; `TestRightsMatrixMatchesEnforcement` checks every role × action against real requests, `TestRoleEditing` the guards. Only global admins (human session) edit roles. A role holding `members` cannot grant, change or remove `admin` (no self-escalation). Defaults equal the old fixed behaviour. See ADR-0010.

### Two-factor authentication, registration and deletion

- TOTP (RFC 6238, SHA-1, 6 digits, 30 s, one step of drift). The secret must be readable to verify codes, so it is stored as is in `users.totp_secret`: anyone with the database file can read it. A used code cannot be replayed (`totp_last_step`), two logins racing with one code let only one win. Recovery codes (10, single use) are stored hashed. An admin can reset 2FA of a user who lost the device.
- Wrong second factors count against the login failure limiter; "code required" does not.
- Invite codes are random, stored hashed, single use, expiring, optionally bound to an email, and never logged. Unless email is set up (below) Atlas sends no email and the admin hands over the link; with email, an invite for a given address is also mailed and the code sits in the outbox body until the mail is sent or abandoned, then it is blanked. Registration refusals count against the same limiter.
- Profile pictures run through the attachment pipeline; only a re-encoded thumbnail is served (EXIF dropped), the original stays on disk.
- Account deletion anonymises instead of erasing rows: audit entries and authored items keep their references. It refuses to leave the instance without an admin or a project without an admin.
- Audit export is admin-only and streams at most 50.000 entries per request.

### Outgoing email (ADR-0012)

- Optional. Only a global admin in a human session can read or change the settings; tokens cannot.
- The SMTP password is encrypted at rest (AES-256-GCM) with `<data dir>/mail.key` (mode 0600). It is not part of API answers, audit entries or logs, and send errors are redacted before they are stored or shown. Environment settings (`ATLAS_SMTP_PASSWORD`) stay in the process environment. A backup does not contain the key: after a restore elsewhere, enter the password again.
- Mail is sent by a worker outside any database transaction, with limited retries. Recipient and subject with line breaks are refused (header injection). Plain SMTP login over an unencrypted connection to a remote host is refused by `net/smtp`.
- Notification mails go only to users who switched them on; they contain a summary and a link, never item contents.

### Repository link and README (ADR-0013)

- The link holds no secrets: URLs with credentials are refused, and Atlas never logs in to a git server. Only project admins can link or unlink; the change is audited.
- Atlas fetches READMEs and README images itself, so the fetch must not become a way into the internal network (SSRF). The HTTP client refuses every address that is not a public internet address at the moment it connects (loopback, private ranges, link-local and cloud metadata, CGNAT, multicast, IPv4-mapped IPv6), allows https only, follows at most three redirects and only on the same host, ignores proxies, and caps size (README 1 MiB, image 5 MiB) and time (10 s). `ATLAS_REPO_ALLOW_PRIVATE=true` lifts the address check for a self-hosted server on a private network: only set it when project admins are trusted to point Atlas at anything it can reach.
- The image proxy only fetches from the repository's hosts, `*.githubusercontent.com` and a few badge hosts, requires a session or token with read access to the project, checks that the data is an image and answers with `nosniff` and a sandboxing CSP, so a malicious SVG cannot run script. The page CSP is unchanged (`img-src 'self' data:`).
- README markdown is rendered with raw HTML switched off; `javascript:`, `data:` and `file:` links and images are dropped. Failures are logged with project, host and reason, never with response bodies.

### Time tracking and insight (ADR-0014)

- Booking needs the `time.log` action, an enabled project setting and write scope; entries can only be changed by their owner or a project admin, and every change is audited. Progress, timeline, activity and summaries need `view`; CSV export needs `export`. The activity journal is limited to item, comment, milestone, attachment and time operations, so member, token and settings changes are not exposed to viewers.
- CSV cells that start with `=`, `+`, `-`, `@`, tab or carriage return are prefixed with `'` so spreadsheets do not run them as formulas.

### Project context (ADR-0015)

- Actions: reading needs `view`; adding, editing, archiving and superseding need `context.write` (members and admins by default) and write scope; `context.verify` (admins by default) marks an entry verified. Verifying is only possible for a human session: a token never verifies, even a token of a project admin, so an agent cannot confirm its own claims. Every change is audited with the actor (`human:user-4` or `token:pat-17`) and, for MCP calls, a batch id.
- Entries start `unverified`. Editing a verified entry makes it `unverified`; a changed `content_hash` on a kept source makes it `stale`. `source_kind` is derived from the sources and cannot be set by the caller.
- Source paths are validated: relative to the repository, no `..`, no backslash, no absolute or `~` paths, and no secret files (`.env*`, private keys, `*.pem`, `*.key`, `.netrc`, `.npmrc`, credentials files). URLs must be `https://`. Atlas stores references only: it never reads the repository paths, so a path cannot be used to read files from the server.
- Prompt-injection boundary: entry text is data written by people and agents. The API returns it as data, the MCP tool descriptions tell agents never to follow instructions found in entries and to treat `unverified` and `stale` entries as hints, and nothing in Atlas acts on entry text. The web app renders bodies with markdown-it with raw HTML off and refuses `javascript:` links (covered by the same renderer tests as comments and README).
- Token scopes and project limits apply as everywhere: a token outside a project gets `not_found`; a token without read scope gets `forbidden`.

### MCP status, server log and hardening (ADR-0016)

- The MCP connection list (token names, owners, client names) and the server log window are admin-only and need a human session; tokens get 403. Log lines carry request ids, actors, paths and error reasons, never secrets or bodies.
- Handler panics are recovered and logged with a stack; the client only sees a generic 500. Header size is capped at 64 KiB and MCP request bodies at 1 MiB.
- Responses carry `Permissions-Policy` (camera, microphone, geolocation, payment off) and `Cross-Origin-Opener-Policy: same-origin`; with `ATLAS_COOKIE_SECURE` on, also `Strict-Transport-Security` (one year, no subdomains).

### OAuth for MCP clients (ADR-0025)

- Atlas is its own authorization server. Public clients only (no client secrets); authorization code with mandatory PKCE S256; codes live 5 minutes and are single use, also when the exchange fails.
- Redirects: the `redirect_uri` must match a registered one exactly, otherwise the user sees an error page and nothing is redirected. Registration refuses `javascript:`, `data:`, `file:` and similar schemes, fragments and plain `http` outside localhost.
- Consent: the page needs a signed-in session (or signs the user in, with the normal login rules: throttling, two-factor) and a CSRF token tied to that session. It sends `X-Frame-Options: DENY` and a frame-ancestors CSP.
- Tokens: access tokens are normal `api_tokens` (hashed, scoped read/write, 1 hour, revocable). Refresh tokens are hashed, rotate on use, live 30 days; reuse of a used refresh token revokes the whole family. A token cannot mint tokens, so OAuth cannot be used to widen access.
- Abuse: registration and failed token requests count against a per-address limit (5 per 15 minutes); at most 1000 clients exist. Registered clients are not shown to users beyond their name on the consent page: treat the name as untrusted text (it is escaped).
- The `resource` parameter, when sent, must equal `<base>/mcp` (RFC 8707). Set `ATLAS_PUBLIC_URL`; without it the metadata is built from the request's Host header.
