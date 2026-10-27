# Operations

## Backup

```bash
atlas backup /backups/atlas-2026-10-27     # server may keep running; target must be new or empty
docker compose exec atlas /atlas backup /data/backups/manual-1   # inside the container
```

Writes `atlas.db` (consistent snapshot), the uploads it references and `manifest.json` (SHA-256 per file, last migration, generation). Protocol and reasons: ADR-0006. A migration also writes `backups/pre-migrate-*.db` automatically before applying anything.

## Restore

1. Stop the server.
2. `ATLAS_DATA_DIR=/data atlas restore /backups/atlas-2026-10-27` (the backup is verified against its manifest first: exactly one `atlas.db`, only regular files under `uploads/`, no symlinks, matching checksums. The database is copied next to the live one, integrity-checked and given a new generation, and only then swapped in; a failure leaves the live database untouched).
3. Start the server. The old database is kept in `<data>/backups/pre-restore-<time>/`; clients reload because the generation changed.

### Restore test (2026-10-27, native binary)

Live backup of a running server (project with 2 items) with `atlas backup`, restore into an empty data directory with `atlas restore`, start a second server on it: login works, both items are present, and the SSE `hello` generation changed (`27fba555029bb24f` to `5fe856ae058f772b`). Automated in `internal/backup` (round trip with an upload, new generation, tampered upload is rejected before the database is written). Not tested: restore of a multi-GB uploads directory, restore on another architecture.

## Export and import

Per project over REST, see `docs/api-contract.md`:

```bash
curl -H "Authorization: Bearer $TOKEN" "$URL/api/v1/projects/EXO/export" -o exo.json
curl -H "Authorization: Bearer $TOKEN" "$URL/api/v1/projects/EXO/export?format=csv" -o exo.csv
curl -H "Authorization: Bearer $TOKEN" -X POST --data-binary @exo.json "$URL/api/v1/projects/NEW/import?dry_run=1"
```

Always dry-run first. The report lists duplicates, row errors and the source-to-new key mapping. Import is all-or-nothing. There is no UI for export/import yet.

## Other commands

`atlas check paths [--fix]` verifies the materialized item paths.

## Email (optional)

Atlas sends no email until SMTP is set up, either with environment variables or in the admin screen (Administration > Email). Saved settings override the environment as a whole.

| Variable | Meaning |
|---|---|
| `ATLAS_SMTP_HOST`, `ATLAS_SMTP_PORT` (587) | Server |
| `ATLAS_SMTP_SECURITY` | `starttls` (default), `tls` or `none` |
| `ATLAS_SMTP_USER`, `ATLAS_SMTP_PASSWORD` | Login, optional |
| `ATLAS_SMTP_FROM`, `ATLAS_SMTP_FROM_NAME` | Sender address and name |
| `ATLAS_PUBLIC_URL` | Address users open Atlas at, for links in mails (for example `https://atlas.example.com`) |

A password saved in the admin screen is encrypted with `<data dir>/mail.key`. Backups do not include this file; after restoring on another machine the password must be entered again (the settings otherwise survive). Mails that cannot be delivered are retried five times (1, 5, 15, 60 minutes) and then dropped; `mail_outbox.last_error` has the reason.

## Connecting Claude Code and Codex (MCP)

Atlas serves MCP over Streamable HTTP at `/mcp`. Agents authenticate with an API token (Settings > API tokens, or a project token under project settings > Tokens). Give an agent read scope to let it read project knowledge (`context_get`) and the board; add write scope only when it should create items, comment or write context. A token can be limited to projects and is revoked instantly. Note: a token for the Atlas MCP is not the Taiga server some global agent instructions call "Atlas MCP"; they are different servers.

To see who is connected, open Administration > MCP: status, clients active in the last 5 minutes (token, client name, last seen, calls, errors) and the latest calls. Administration > Server log shows the latest 1000 log lines (filter by level and text); stdout remains the full log.

Claude Code (from the project directory; `ATLAS_TOKEN` holds the token, `https://atlas.example.com` is your public address):

```bash
claude mcp add --transport http atlas https://atlas.example.com/mcp --header "Authorization: Bearer $ATLAS_TOKEN"
```

Then check the connection with `/mcp`. In non-interactive mode allow the tools explicitly, for example `--allowedTools "mcp__atlas__context_get"`.

Codex (`~/.codex/config.toml`, the token is read from the environment):

```toml
[mcp_servers.atlas]
url = "https://atlas.example.com/mcp"
bearer_token_env_var = "ATLAS_TOKEN"
```

Codex asks before it calls a tool that changes data; read-only tools such as `context_get` need no approval.

Agents only use `context_get` when told to: the repository's `AGENTS.md` (or `CLAUDE.md`) should carry the short Atlas Context paragraph from this project's `AGENTS.md`. Measured use without it is in `benchmarks/RESULTS.md`.

## Project context

Entries are kept in the project's database like everything else, so the normal backup covers them. They are not part of the project export (JSON/CSV) yet. A project with more than 200 active entries gets a warning in the bootstrap and the Context tab: archive or merge old entries instead of letting the bootstrap grow.

## Repository README

`ATLAS_REPO_ALLOW_PRIVATE=true` lets Atlas fetch READMEs from a git server on a private address (for example a Gitea on the LAN). By default only public internet addresses are fetched. Provider support for the README: GitHub, GitLab, Gitea/Codeberg/Forgejo, Bitbucket; any other host is assumed to use Gitea-style raw URLs. Private repositories are not supported.
