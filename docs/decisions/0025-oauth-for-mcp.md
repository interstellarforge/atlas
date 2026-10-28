# ADR-0025: OAuth 2.1 for MCP clients

- Status: accepted
- Date: 2026-10-28

## Context

ATL-75: OAuth-oriented MCP clients (for example `mcp-remote` or hosted connectors) discover how to authorize from standard metadata and expect a standard flow. Atlas offered personal tokens only; ATL-307 already stopped the SPA from answering `/.well-known/*` with HTML.

## Decision

1. Follow the MCP authorization specification (2025-06-18): `/mcp` is an OAuth resource server. A 401 carries `WWW-Authenticate: Bearer resource_metadata=...`; `/.well-known/oauth-protected-resource` (RFC 9728) names Atlas as the authorization server; `/.well-known/oauth-authorization-server` (RFC 8414) lists the endpoints.
2. Atlas is the authorization server itself (no external IdP, one container): `POST /oauth/register` (RFC 7591, public clients), `GET/POST /oauth/authorize` (authorization code, PKCE S256 required, `resource` per RFC 8707), `POST /oauth/token` (authorization code and rotating refresh tokens).
3. Access tokens are ordinary `api_tokens` named `OAuth: <client>` with a 1 hour expiry: `TokenActor`, scopes, project limits, revocation and the token list keep working with no second token path. A background job removes OAuth tokens a day after they ended.
4. Granted scopes are the requested `read`/`write` (default `read`); project limits are not offered through OAuth. Rights of the user still apply on every call.
5. The consent and sign-in page is a small server-rendered HTML template (`internal/api/oauth_page.html`, English), not part of the SPA, so the redirect flow works without SPA routing. It is the only user-facing text outside the web app's locale files, like the email templates.
6. Refresh token reuse revokes the family. Registration and failed requests share the login throttle.

## Not built

- Client management UI and a list of connected apps (they show up as tokens named `OAuth: ...`).
- Client secrets / confidential clients, token introspection and revocation endpoints (RFC 7009), per-project OAuth scopes.
- Localized consent page.
