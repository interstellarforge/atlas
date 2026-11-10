# Security model

## Authentication

- UI: server-side sessions, `HttpOnly`, `Secure`, `SameSite=Lax` cookie; CSRF token on state-changing requests.
- Passwords: argon2id; login rate limiting.
- API/MCP: personal access tokens, shown once, stored hashed (SHA-256 of a high-entropy token), prefix stored for identification, optional expiry, revocable at runtime without restart.
- HTTPS required for MCP and non-local deployments (terminated by the user's reverse proxy or built-in TLS).

## Authorization

- An admin can force a password change (`must_change_password`): sessions end, the next one may only reach `/auth/me`, logout and `/me/password`. Tokens are not touched. See ADR-0010.
- Roles per project via `project_members`: built-in `admin`, `member` and `viewer` plus any custom roles that already exist. Roles are frozen: they can no longer be edited (ADR-0029, which supersedes ADR-0010) and are replaced by the organization model in the ORG 2 migration. Each role is a set of actions in `role_actions`; every project check in `core` names an action through `Service.access`, and all account-level decisions live in `internal/core/authz.go`. `/api/v1/admin/*` is refused with 403 before the body is read. `internal/api/matrix_test.go` runs every registered route and MCP tool as each actor; an unclassified route or tool fails the test.
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
- People are visible only through a shared organization or project (`visibleUsersQuery`); tokens list only users who share a project they may access; nobody but a SUPERADMIN sees the SUPERADMIN flag, and a SUPERADMIN sees no one outside their own organizations and projects (accounts for suspension: `/admin/users`); avatars follow the same rule (`TestUserListingIsScoped`, `TestUserListingIsScopedToOrganizations`).
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
- Deactivating a user deletes their sessions and makes their tokens fail; sign-in is refused. The last active SUPERADMIN cannot be demoted, suspended or deleted: the core refuses in the same `BEGIN IMMEDIATE` transaction, and triggers on `users` (migration 0030) abort any other writer, with no override (tested with two database handles, a busy lock and a failing audit write). Neither can the last active ADMIN of an organization, and nobody deactivates themselves.
- Project tokens never exceed their owner: access is resolved live from the owner's current rights. When the owner leaves the project or the organization (or an organization ADMIN is demoted), those tokens are revoked.
- A SUPERADMIN is made only by the first-install bootstrap (`ATLAS_ADMIN_EMAIL`), the data migration (`ATLAS_SUPERADMIN_EMAIL`), an existing SUPERADMIN in a human session, or the operator command `atlas superadmin grant <email>` (audited as `system:cli`). Invites, registration, organization ADMINs, tokens (even a SUPERADMIN's) and MCP cannot (`TestOnlyASuperadminCanMakeASuperadmin`).
- Authorization follows ADR-0029 as amended by ADR-0031: the SUPERADMIN (`users.is_superadmin`) runs Atlas and has no project or organization-data access of its own; organization ADMIN and USER with a project membership (`core.MemberActions`) do the work. A SUPERADMIN user session (never a token) reaches an organization only through a support grant that its ADMIN created (`support_grants`: read_only = view and export, full = every action, time-limited or until revoked, read live, revocable at once; use is audited as `support.access`, reading at most once per 10 minutes per grant and user). Under a grant a SUPERADMIN can neither create or extend grants nor change their own membership. A SUPERADMIN may appoint the first ADMIN of an organization that has none. `internal/core/authz.go` is the only place that decides; `TestLegacyAuthorizationIsNotRead` fails if other code reads the legacy `is_admin` column or the retired role tables, `TestProjectAccessMatrix` and `TestAuthorizationMatrix` cover actor × action × resource and the 401/403/404 mapping. The rights tab shows the single member role read-only.

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
- Every mail is sent as `multipart/alternative`: the plain-text template plus an HTML version in the Atlas layout (`internal/notifications/html.go`). The HTML is built with `html/template`, so template values are escaped; a paragraph that is only a URL becomes a button. No remote images or scripts.
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

- Actions: reading needs `view`; adding, editing, archiving and superseding need `context.write` (members and organization admins) and write scope; `context.verify` (organization admins and SUPERADMINs) marks an entry verified. Verifying is only possible for a human session: a token never verifies, even a token of a project admin, so an agent cannot confirm its own claims. Every change is audited with the actor (`human:user-4` or `token:pat-17`) and, for MCP calls, a batch id.
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

### Registration and invite review (ATL-376, ADR-0030)

Result of reviewing `POST /auth/register`, invites and login, with tests in `internal/api/account_status_test.go`.

- **No self-chosen privilege.** The body decoder rejects unknown fields (400), so `is_superadmin`, `status`, `role` or an organization cannot be sent. Register creates an `active` account without any organization (no access) and never `is_superadmin`; invites carry no role field. Only a SUPERADMIN session or the operator command makes a SUPERADMIN.
- **Taken email.** The answer is the same 201 with no user id as for a new email; nothing changes and a presented invite is not consumed. The password hash is computed before any lookup, so the difference in time is one indexed query.
- **Errors.** Password, name, domain and invite failures do not depend on whether the email exists. Refusals (closed door, bad invite) count against the shared per-client limiter; login failures (5 per 15 minutes per email and client) are unchanged and identical for unknown and non-active accounts.
- **Cookies and CSRF.** Session cookie: HttpOnly, SameSite=Lax, Secure unless `ATLAS_COOKIE_SECURE=false`. Register and login are public JSON endpoints and create no state besides the account itself; every state-changing session request still needs the CSRF header.
- **Residual risk (accepted).** Without email verification, someone who registers an address and then tries to sign in with the password they chose learns whether the address already had an account (sign-in works for a new one, 401 for an existing one). Mitigations: the login limiter and the register limiter; a new account has no access anyway (ADR-0031). Closing it needs verified email, which is out of scope for the MVP.
- **For M3.** Organization invitations must keep the same answer whether or not the email has an account (ATL-414 and the invitation tests).

## Email domains: threat note (ATL-503, ADR-0031)

Risk: whoever controls an address at a domain could join the organization that claims it.

- A claim does nothing until the SUPERADMIN confirms it. An unconfirmed claim gives no join path (tested).
- Free-mail providers (fixed list in `internal/core/domains.go`) cannot be claimed. A domain another organization holds gets the same refusal, so a claimant learns nothing about other organizations. One domain, one organization (unique index).
- Mode `auto` joins only a verified address, only while outgoing mail is set up, and never an account an ADMIN rejected. An unverified address, or an instance without mail, falls back to a request the ADMIN decides. Joining gives a USER membership and no project access.
- Verification links are random, stored as a hash, single use, valid 24 h and bound to the address they were sent to; changing the email clears the verified flag. Unusable links give one answer and count against the login limiter.
- Join options are listed only to a verified address, so registering with a victim domain reveals nothing.
- Known limit: in mode `auto` an ADMIN who removes a member of the domain cannot stop that person joining again; switch the domain to `request`. Domain ownership is the SUPERADMIN's judgement (no DNS proof). A domain that changes hands needs the SUPERADMIN to reject the confirmed claim.

## Organization rules on sessions and tokens (ATL-504)

- The auth service asks the core for the live rules of the user on every login, session request and token request (`auth.Service.SetUserPolicy`): shortest session wins, required 2FA if any organization requires it, tokens only if every organization allows them, shortest token lifetime.
- Required 2FA limits the session to setting it up (`totpSetupPaths`); tokens are not affected by it (they carry no second factor). An ADMIN who requires it is bound by it too.
- Token rules bind at creation and at use (`created_at` + lifetime), so a rule applies to existing tokens too; OAuth consent and the token endpoint refuse when access is off.
- The password minimum applies when the password is changed, not retroactively. Registration and invitation sign-up use the instance minimum because the account has no organization yet.
- A member who creates a project (`project_creation` = `members`) becomes a member of it, not an administrator of the organization.

## Company model: actors and organization routes (ATL-506, ADR-0031)

`TestOrganizationRouteMatrix` (`internal/api/matrix_test.go`) runs the organization-scoped routes as every actor of the company model. `TestAuthorizationMatrix` and `TestFreshAccountReachesNothing` keep covering every other route and a fresh account.

| Actor | Organization read/write routes of a company | What the test pins |
| --- | --- | --- |
| Anonymous | 401 | every route |
| Account without a company | 404, byte-identical to a missing organization; a join request answers 202 like for a missing one | no organization is listed or disclosed |
| ADMIN of another company | 404, identical to a missing organization | no cross-company reach |
| Plain member (USER) | 403 | members cannot invite, change rules, decide requests or claim domains |
| SUPERADMIN without a grant | 403 | the Atlas admin runs Atlas, not companies |
| SUPERADMIN with a `read_only` grant | 403 | read_only only opens project views and exports |
| SUPERADMIN with a `full` grant | members and invitations lists 200; everything else 403 | a grant never decides who joins, who is invited or which rules apply, and never reads the company's join requests, domains, rules, audit log or support grants |
| Company ADMIN | 200 | the only actor with the organization's own management |

Not covered by a test, by design: organization management has no MCP tool (`docs/mcp-contract.md`).

## Authorization audit matrix (ATL-374)

Every REST route, MCP tool and other surface with its protection, target rule and expected answer (ADR-0029). Visibility comes first: a resource the caller may not know about answers 404, identical to a missing one; a visible resource without the needed right answers 403; no or invalid credentials, or a suspended account, answer 401. `TestAuthorizationMatrix` runs every route below as anonymous, suspended, plain, organization-admin, no-organization and SUPERADMIN actors, `TestMCPToolMatrix` every tool, and `TestSecurityDocListsEveryRoute` fails when a route is missing here. The "Rule" column names the action(s) the handler reaches in `core` (parsed from the code); read the handler for the exact check.

### REST routes

| Route | Protection | Rule | Answers |
|---|---|---|---|
| `GET /.well-known/oauth-authorization-server` | public | no credentials; the handler validates its own input | - |
| `GET /.well-known/oauth-protected-resource` | public | no credentials; the handler validates its own input | - |
| `GET /.well-known/oauth-protected-resource/mcp` | public | no credentials; the handler validates its own input | - |
| `GET /api/v1/admin/audit` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `GET /api/v1/admin/audit/export` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `GET /api/v1/admin/instance` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `GET /api/v1/admin/invites` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `POST /api/v1/admin/invites` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `DELETE /api/v1/admin/invites/{id}` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `GET /api/v1/admin/logs` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `DELETE /api/v1/admin/mail` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `GET /api/v1/admin/mail` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `PUT /api/v1/admin/mail` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `POST /api/v1/admin/mail/test` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `GET /api/v1/admin/policy` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `PUT /api/v1/admin/policy` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `GET /api/v1/admin/update` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `GET /api/v1/admin/users` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `PATCH /api/v1/admin/users/{id}` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `GET /api/v1/organizations` | signed in | the caller's own active organizations; a SUPERADMIN lists all (name, key, status): `core.ListOrganizations` | 401 |
| `GET /api/v1/organizations/{key}/support-grants` | explicit ADMIN of that organization, user session | `authorizeOrg` `orgGrants`: never through a support grant, never a token with a project list | 401 / 403 / 404 |
| `POST /api/v1/organizations/{key}/support-grants` | explicit ADMIN of that organization, user session | `authorizeOrg` `orgGrants`; level read_only or full, hours 1-720 or until_revoked | 400 / 401 / 403 / 404 |
| `DELETE /api/v1/organizations/{key}/support-grants/{id}` | explicit ADMIN of that organization, user session | `authorizeOrg` `orgGrants`; ends the grant at once | 401 / 403 / 404 |
| `GET /api/v1/admin/organizations` | SUPERADMIN session | `protect()` refuses everyone else before the body is read; name, key, status, ADMIN emails, member count only | 401 / 403 |
| `POST /api/v1/admin/organizations` | SUPERADMIN session | `protect()`; `core.CreateOrganizationFor`: the first ADMIN is an existing active account, the creator does not join | 401 / 403 |
| `GET /api/v1/admin/organization-applications` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `POST /api/v1/admin/organization-applications/{id}/{decision}` | SUPERADMIN session | `protect()`; `core.DecideApplication`: compare-and-set, human session only | 401 / 403 / 404 / 422 |
| `GET /api/v1/organizations/{key}/invites` | explicit ADMIN of that organization, user session | `authorizeOrg` `orgMembers` | 401 / 403 / 404 |
| `POST /api/v1/organizations/{key}/invites` | explicit ADMIN of that organization, user session | `core.CreateOrgInvite`: role is always USER; projects must belong to the organization; the Atlas admin and support grants are refused; same answer for any address; the token is shown once | 400 / 401 / 403 / 404 |
| `DELETE /api/v1/organizations/{key}/invites/{id}` | explicit ADMIN of that organization, user session | `core.RevokeOrgInvite` | 401 / 403 / 404 |
| `POST /api/v1/organizations/invitations/accept` | signed-in human session | `core.AcceptOrgInvite`: the account's email must be the invited address; never changes an existing role; compare-and-set | 401 / 403 / 422 / 429 |
| `GET /api/v1/organizations/{key}/join-link` | explicit ADMIN of that organization, user session | `core.adminOfOrg`: `orgMembers`, support grants refused | 401 / 403 / 404 |
| `PUT /api/v1/organizations/{key}/join-link` | explicit ADMIN of that organization, user session | `core.SetJoinLink`; audited | 400 / 401 / 403 / 404 |
| `POST /api/v1/organizations/{key}/join-requests` | signed-in human session | `core.RequestJoin`: always 202; an unknown, archived or disabled link, a member, a waiting request and a cooldown (24 h after a rejection, 3 per 30 days) answer the same and file nothing | 401 / 403 |
| `GET /api/v1/organizations/{key}/join-requests` | explicit ADMIN of that organization, user session | `core.adminOfOrg`; the requester gets 404 | 401 / 403 / 404 |
| `POST /api/v1/organizations/{key}/join-requests/{id}/{decision}` | explicit ADMIN of that organization, user session | `core.DecideJoinRequest`: compare-and-set; approve adds a USER membership and no project access | 400 / 401 / 403 / 404 / 422 |
| `GET /api/v1/organizations/{key}/domains` | explicit ADMIN of that organization, user session | `core.adminOfOrg` | 401 / 403 / 404 |
| `POST /api/v1/organizations/{key}/domains` | explicit ADMIN of that organization, user session | `core.ClaimDomain`: the claim stays pending; a free-mail provider and a held domain give the same refusal | 400 / 401 / 403 / 404 / 422 |
| `PUT /api/v1/organizations/{key}/domains/{id}` | explicit ADMIN of that organization, user session | `core.SetDomainMode`: request or auto | 400 / 401 / 403 / 404 |
| `DELETE /api/v1/organizations/{key}/domains/{id}` | explicit ADMIN of that organization, user session | `core.RemoveDomain` | 401 / 403 / 404 |
| `GET /api/v1/admin/organization-domains` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `POST /api/v1/admin/organization-domains/{id}/{decision}` | SUPERADMIN session | `protect()`; `core.DecideDomain`: approve is compare-and-set; reject also revokes a confirmed domain | 401 / 403 / 404 / 422 |
| `POST /api/v1/me/email/verification` | signed-in human session | `core.RequestEmailVerification`: mails a link to the own address only; needs outgoing mail (422 without); one per minute | 401 / 403 / 422 |
| `GET /api/v1/me/join-options` | signed-in human session | `core.JoinOptions`: only for a verified address, only organizations holding a confirmed domain of that address | 401 / 403 |
| `GET /api/v1/organizations/{key}/members` | explicit ADMIN of that organization, user session | `core.ListOrgMembers`: `authorizeOrg` `orgMembers` | 401 / 403 / 404 |
| `PUT /api/v1/organizations/{key}/members/{user}` | explicit ADMIN of that organization, user session | `core.SetOrgMember`: role admin or user; the last active ADMIN is protected; a support grant cannot make its own access permanent | 400 / 401 / 403 / 404 / 422 |
| `DELETE /api/v1/organizations/{key}/members/{user}` | explicit ADMIN of that organization, user session | `core.RemoveOrgMember`: also leaves every project of the organization; the last active ADMIN is protected | 401 / 403 / 404 / 422 |
| `GET /api/v1/organizations/{key}/logo` | member of that organization (the Atlas admin sees the organization too), user session | `authorizeOrg` `orgView`; only a re-encoded thumbnail is served; no logo and no access answer the same | 401 / 404 |
| `PUT /api/v1/organizations/{key}/logo` | explicit ADMIN of that organization, user session | `core.SetOrgLogo`: the ADMIN check runs before the upload is read; same limits, magic bytes and pixel count as attachments | 400 / 401 / 403 / 404 / 413 |
| `DELETE /api/v1/organizations/{key}/logo` | explicit ADMIN of that organization, user session | `core.RemoveOrgLogo` | 401 / 403 / 404 |
| `GET /api/v1/organizations/{key}/templates` | explicit ADMIN of that organization, user session | `core.ListOrgTemplates`: only the organization's own item and project templates | 401 / 403 / 404 |
| `POST /api/v1/organizations/{key}/item-templates` | explicit ADMIN of that organization, user session | `core.SaveOrgItemTemplate`: create or replace by name; support grants refused | 400 / 401 / 403 / 404 |
| `DELETE /api/v1/organizations/{key}/item-templates/{id}` | explicit ADMIN of that organization, user session | `core.DeleteOrgItemTemplate`: only the organization's own | 401 / 403 / 404 |
| `POST /api/v1/organizations/{key}/project-templates` | explicit ADMIN of that organization, user session | `core.SaveOrgProjectTemplate`: data or a project of the same organization | 400 / 401 / 403 / 404 / 422 |
| `DELETE /api/v1/organizations/{key}/project-templates/{id}` | explicit ADMIN of that organization, user session | `core.DeleteOrgProjectTemplate`: only the organization's own | 401 / 403 / 404 |
| `GET /api/v1/organizations/{key}/settings` | explicit ADMIN of that organization, user session | `core.adminOfOrg` | 401 / 403 / 404 |
| `PUT /api/v1/organizations/{key}/settings` | explicit ADMIN of that organization, user session | `core.UpdateOrgSettings`: password minimum never below the instance's; unknown keys refused; audited | 400 / 401 / 403 / 404 |
| `GET /api/v1/organizations/{key}/audit` | explicit ADMIN of that organization, user session | `core.ListOrgAudit`: the organization's own entries and those of its projects and items only | 401 / 403 / 404 |
| `GET /api/v1/organizations/applications` | signed-in human session | `core.ListMyApplications`: own applications only | 401 / 403 |
| `POST /api/v1/organizations/applications` | signed-in human session, instance setting `organization_creation` = apply | `core.ApplyForOrganization`: one waiting application, 24 h after a rejection, 3 per 30 days; a key that exists is not disclosed | 400 / 401 / 403 / 422 |
| `GET /api/v1/attachments/{id}` | signed in + project right | the attachment's item: `access` view; token scope applies | 401 / 404 hidden |
| `GET /api/v1/attachments/{id}/thumb` | signed in + project right | the attachment's item: `access` view; token scope applies | 401 / 404 hidden |
| `POST /api/v1/auth/login` | public | no credentials; the handler validates its own input | - |
| `POST /api/v1/auth/logout` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `GET /api/v1/auth/me` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `GET /api/v1/auth/policy` | public | no credentials; the handler validates its own input | - |
| `POST /api/v1/auth/register` | public | no credentials; the handler validates its own input | - |
| `GET /api/v1/auth/invitations/{token}` | public, the token holder | `core.PreviewOrgInvite`: token hash lookup; every invalid, used, revoked or expired token gives the same 422; failures count against the login limiter | 422 / 429 |
| `POST /api/v1/auth/email-verification` | public, the link holder | `core.VerifyEmail`: single use, 24 h, must still match the account's address; every unusable link gives the same 422; failures count against the login limiter | 400 / 422 / 429 |
| `POST /api/v1/auth/invitations/accept` | public, the token holder | `core.AcceptOrgInviteNew`: creates the account for the invited address (marks the email verified) and a USER membership; an address with an account must sign in; compare-and-set, one winner | 400 / 422 / 429 |
| `POST /api/v1/batches/{id}/undo` | signed in + project right | `access`: export, item.update, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/board` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/changes` | signed in | events of readable projects only (`ReadableProjectKeys`); notifications only to their recipient | 401 |
| `GET /api/v1/events` | signed in | same filter as `/changes`, delivered as a stream | 401 |
| `GET /api/v1/health` | public | no credentials; the handler validates its own input | - |
| `GET /api/v1/item-templates` | signed in | instance-wide templates, read-only for everyone | 401 |
| `POST /api/v1/item-templates` | SUPERADMIN session | `adminOnly` in core | 401 / 403 |
| `DELETE /api/v1/item-templates/{id}` | SUPERADMIN session | `adminOnly` in core | 401 / 403 |
| `GET /api/v1/items` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/items` | signed in + project right | `access`: export, item.create, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/items/{key}` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/items/{key}` | signed in + project right | `access`: export, item.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/items/{key}/attachments` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/items/{key}/attachments` | signed in + project right | `access`: export, item.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/items/{key}/checklist` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/items/{key}/checklist` | signed in + project right | `access`: export, item.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/items/{key}/checklist/{id}` | signed in + project right | `access`: export, item.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/items/{key}/checklist/{id}` | signed in + project right | `access`: export, item.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/items/{key}/comments` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/items/{key}/comments` | signed in + project right | `access`: comment, export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/items/{key}/follow` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/items/{key}/follow` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PUT /api/v1/items/{key}/follow` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/items/{key}/links` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/items/{key}/links` | signed in + project right | `access`: export, item.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/items/{key}/links/{type}/{target}` | signed in + project right | `access`: export, item.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/items/{key}/move` | signed in + project right | `access`: export, item.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/items/{key}/time` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/items/{key}/time` | signed in + project right | `access`: export, time.log, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/mcp/stats` | SUPERADMIN session | `protect()` refuses everyone else before the body is read | 401 / 403 |
| `DELETE /api/v1/me` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `PATCH /api/v1/me` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `DELETE /api/v1/me/avatar` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `PUT /api/v1/me/avatar` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `PUT /api/v1/me/email` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `PUT /api/v1/me/password` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `GET /api/v1/me/preferences` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `PUT /api/v1/me/preferences` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `GET /api/v1/me/security` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `GET /api/v1/me/sessions` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `DELETE /api/v1/me/sessions/{id}` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `POST /api/v1/me/totp/disable` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `POST /api/v1/me/totp/enable` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `POST /api/v1/me/totp/setup` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `GET /api/v1/notifications` | signed in (own account) | the caller's own notifications | 401 |
| `POST /api/v1/notifications/read` | signed in (own account) | the caller's own notifications | 401 |
| `DELETE /api/v1/notifications/{id}` | signed in (own account) | the caller's own notification | 401 / 404 |
| `POST /api/v1/notifications/{id}/read` | signed in (own account) | the caller's own notification | 401 / 404 |
| `GET /api/v1/project-templates` | signed in | instance-wide templates, read-only for everyone | 401 |
| `POST /api/v1/project-templates` | SUPERADMIN session | `adminOnly` in core | 401 / 403 |
| `DELETE /api/v1/project-templates/{id}` | SUPERADMIN session | `adminOnly` in core | 401 / 403 |
| `GET /api/v1/projects` | signed in | lists the projects the caller may read (`readableProjects`) | 401 |
| `POST /api/v1/projects` | signed in + project right | `access`: export, project.update, view, workflow and organization rules; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/projects/{key}` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/activity` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/batches` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/board` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/board/columns/{status}` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/context` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/context` | signed in + project right | `access`: context.write, export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/context/bootstrap` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/context/search` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/context/{ckey}` | signed in + project right | `access`: context.write, export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/context/{ckey}` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/projects/{key}/context/{ckey}` | signed in + project right | `access`: context.write, export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/context/{ckey}/supersede` | signed in + project right | `access`: context.write, export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/context/{ckey}/verify` | signed in + project right | `access`: context.verify, export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/custom-fields` | signed in + project right | `access`: export, view, workflow; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/custom-fields/{name}` | signed in + project right | `access`: export, view, workflow; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/export` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/import` | signed in + project right | `access`: export, import, item.create, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/item-templates` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/item-templates` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/item-templates/{id}` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/members` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/members/{user}` | signed in + project right | `access`: export, members, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PUT /api/v1/projects/{key}/members/{user}` | signed in + project right | `access`: export, members, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/milestones` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/milestones` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/milestones/{id}` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/projects/{key}/milestones/{id}` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/progress` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/readme` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/readme/asset` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/recurrences` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/recurrences` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/recurrences/{id}` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/projects/{key}/recurrences/{id}` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/sprints` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/sprints` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/sprints/{id}` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/projects/{key}/sprints/{id}` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/statuses` | signed in + project right | `access`: export, view, workflow; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/statuses/{id}` | signed in + project right | `access`: export, view, workflow; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/projects/{key}/statuses/{id}` | signed in + project right | `access`: export, view, workflow; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/time` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PUT /api/v1/projects/{key}/time-tracking` | signed in + project right | `access`: export, view, workflow; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/time/export` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/time/{id}` | signed in + project right | `access`: export, time.log, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/projects/{key}/time/{id}` | signed in + project right | `access`: export, time.log, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/timeline` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/tokens` | signed in + project right | `access`: export, tokens, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/tokens` | signed in + project right | `access`: export, tokens, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/tokens/{id}` | signed in + project right | `access`: export, tokens, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/webhooks` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `POST /api/v1/projects/{key}/webhooks` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `DELETE /api/v1/projects/{key}/webhooks/{id}` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/projects/{key}/webhooks/{id}` | signed in + project right | `access`: export, project.update, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/projects/{key}/workflow` | signed in + project right | `access`: export, view; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PATCH /api/v1/projects/{key}/workflow` | signed in + project right | `access`: export, view, workflow; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `PUT /api/v1/projects/{key}/workflow` | signed in + project right | `access`: export, view, workflow; token scope and project list apply | 401 / 404 hidden / 403 missing right |
| `GET /api/v1/roles` | signed in | the single fixed member role, read-only | 401 |
| `GET /api/v1/tokens` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `POST /api/v1/tokens` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `DELETE /api/v1/tokens/{id}` | signed in (own account) | only the caller's own account (changing credentials needs a human session) | 401 |
| `GET /api/v1/users` | signed in | lists the people the caller may see (`visibleUsersQuery`, ADR-0027) | 401 |
| `POST /api/v1/users` | SUPERADMIN session | `adminOnly` in core | 401 / 403 |
| `GET /api/v1/users/{id}/avatar` | signed in | only for people the caller may see (`visibleUsersQuery`); hidden = 404 | 401 / 404 |
| `GET /oauth/authorize` | public | no credentials; the handler validates its own input | - |
| `POST /oauth/authorize` | public | no credentials; the handler validates its own input | - |
| `POST /oauth/register` | public | no credentials; the handler validates its own input | - |
| `POST /oauth/token` | public | no credentials; the handler validates its own input | - |

### MCP tools

| Tool | Kind | Rule |
|---|---|---|
| `projects_list` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `project_create` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `project_templates_list` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `workflow_get` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `items_search` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `issues_search` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `issue_create` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `issue_update` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `item_get` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `board_get` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `changes_since` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `items_write` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `comment_add` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `project_progress` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `time_log` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `time_summary` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `context_get` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `context_write` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `checklist_write` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `attachments` | read | token scope (read) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `milestone_write` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `sprint_write` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `batch_undo` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |
| `attachment_add` | write | token scope (write) and project list, then the owner's live rights through `access`; hidden = not found, identical to a missing project |

### Other surfaces

| Surface | Rule |
|---|---|
| SSE `/events`, `/changes` | only events of projects the caller may read; user-targeted notifications only to that user; project-less events are never delivered |
| Search (`items?q=`, FTS) | a project filter through `access`; a hidden project answers 404 |
| Exports and imports | `export` / `import` actions on the project (member and up) |
| Attachments and thumbnails | through the item's project (`view`); avatars through `visibleUsersQuery` |
| Webhooks, recurrences, milestones, sprints, workflow, tokens | management actions: SUPERADMIN or the organization ADMIN |
| Notifications | the caller's own only |
| OAuth (`/oauth/*`, `/.well-known/*`) | public by design (PKCE, exact redirect match, hashed codes); issued tokens are ordinary scoped tokens |
| Background work (recurrences, webhooks, mail) | no rights of its own: runs for the user who created it with that user's live rights (a suspended creator fails closed) |
| `atlas` subcommands | operator with shell access to the data directory: `backup`, `restore`, `check paths`, `superadmin grant`, `migrate-orgs` |

### Findings

- ATL-493: users and avatars were listed to every signed-in person. Fixed.
- Four admin routes validated the body before authorizing (400 instead of 403). Fixed in `protect()`.
- No route is unclassified. Known gaps are tracked: organization membership has no REST or UI yet (ATL-413, 439, 451).

## Inventory: `is_admin` and role references before the flip (ATL-473, kept for audit)

Code audit at commit 4e7217b (non-test files). Feeds ATL-475/477 and the authorization ADR (ADR-0029). Data inventory (c, d) needs the live database and is produced by the dry-run command (ATL-428), not guessed here.

### (a) `users.is_admin`, `invites.is_admin`, `Actor.IsAdmin`

| Area | Files | Action in the new model |
|---|---|---|
| Actor carrier | `internal/auth/actor.go`; session loader `auth/service.go:202`; token loader `auth/service.go:309` | remove the flag from the Actor; `authz.go` reads status/superadmin/memberships live (fail closed), ATL-435/436 |
| User create/read | `auth/service.go:115,135,217`; `core/identity.go`; `api/identity.go`; `api/work.go:111` (`CreateUser` takes `is_admin`) | `is_superadmin` never settable via create/body; ATL-437/438 |
| Invites | `core/policy.go:147-286`; `migrations/0011` | `invites.is_admin` dropped; kinds registration/organization (M3) |
| Project authz | `core/core.go:163 access`, `core/projects.go` | replaced by `authz.go`, ATL-436 |
| Bypasses outside `access()` | `core/notifications.go:93`, `core/items.go:465` (assignee/recipient check `is_admin = 1 OR member`) | must route through `authz.go`; easy to miss |
| Background actor | `core/recurrence.go:263,296` builds an `Actor` from stored `is_admin` | must use live rights / system actor (ADR-0029) |
| Admin API | `core/admin.go`, `api/admin.go` | SUPERADMIN-only (`/api/v1/admin/*`) |
| SPA | `web/src/lib/{Admin,Shell,ProjectSettings,ItemTime,ContextView}.svelte`, `App.svelte`, `api.ts` | `is_admin` field becomes `is_superadmin`; UI hiding is convenience only |
| Schema | `migrations/0001_auth.sql` | ATL-474 |

### (b) Role consumers

`roles`, `role_actions`, `project_members.role`: `core/roles.go` (role CRUD), `core/core.go` (`access`), `core/projects.go` (members, `guardLastAdmin`), `core/identity.go`, `core/items.go`, `core/notifications.go`, `api/admin.go` (`/admin/roles`), `web/src/lib/api.ts` plus the Admin roles UI, migrations 0003, 0012, 0017, 0018. All go away or are rewritten in ATL-478 and ATL-436; `guardLastAdmin` is replaced by the last-organization-ADMIN guard (ATL-490).

### (c), (d) Data inventory and role comparison

Pending: produced by `migrate-orgs --dry-run` (ATL-428) against the real database. Classes: equal, reduction (admin), REVIEW-REQUIRED (viewer, custom). See ADR-0028.
