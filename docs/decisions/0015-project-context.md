# ADR-0015: Project context (knowledge for agents)

- Status: accepted
- Date: 2026-10-27

## Context

Agents (Claude Code, Codex) re-read the codebase and docs every session to learn how a project works. CTX asks for persistent, compact project knowledge in Atlas: six categories (architecture, decision, convention, pitfall, dependency, reference). Git stays the source for code and documentation; Atlas keeps summaries plus pointers to them. Constraints: one Go process, one SQLite database, no vector store, embeddings, LLM, crawler or extra container; every extra MCP tool costs catalogue tokens in every session.

## Decisions

1. **Two tables and one counter.** `project_context_entries` (one row per entry, `UNIQUE(project_id, key)`) and `project_context_sources` (pointers: file, ticket, url or adr). `projects.context_version` is increased in the same transaction as every change to the active context, so a client can tell whether its copy is current. Deriving it from `MAX(seq)` of `change_events` was rejected: the journal is pruned. Migration `0018_project_context.sql`.
2. **Keys are slugs** (`auth-flow`: lowercase letters, digits and `-`, 2 to 64 characters). They can never look like `EXO-123`, so they do not collide with item keys or the route regex. `bootstrap` and `search` are reserved because REST uses them as path segments. When no key is given it is derived from the title.
3. **Limits.** Title 160 characters, summary 1000, body 12 KiB (not 16: a detail answer with metadata and escaping must fit the 16 KiB MCP result limit), at most 10 sources per entry, note 200.
4. **Rights (ADR-0010).** Reading is `view`. New actions `context.write` (member, admin) and `context.verify` (admin), granted by migration. Token scopes follow the usual rule: `read` to read, `write` to write. `context.verify` also requires a human: a token can never mark an entry `verified`, whatever its owner may do.
5. **Verification is a human act.** Every create or edit leaves the entry `unverified`. A human with `context.verify` marks it `verified`. An edit of a verified entry makes it `unverified`; when an edit replaces a source with a different `content_hash` the entry becomes `stale` instead. `stale` never turns back into `verified` by itself. `source_kind` is derived from the sources (none or url: manual, file/adr: repository, ticket: ticket, mixed: derived); callers cannot set it, so an agent cannot dress a claim up as sourced.
6. **Optimistic locking.** Updating needs `expected_version`; a stale one is a `ConflictError` carrying `{key, version, title, summary}`. Archiving, superseding and verifying take an optional version and behave the same. Entries are never hard-deleted; `archive` hides them, `supersede` hides them and points at the replacement.
7. **Bootstrap is deterministic.** Active pinned entries ordered by category (architecture, decision, convention, pitfall, dependency, reference), then `updated_at` descending, then key. No body, `state` only when not `verified`. Above 200 active entries a warning asks to prune.
8. **One MCP tool for reading (`context_get`: bootstrap, search, detail) and one for writing (`context_write`).** Search is `LIKE` on title and summary with an exact title match first; FTS5 only if measured to be needed (story 70). Every answer is fitted to a byte budget (`max_bytes`, default 4096, at most 16 KiB) by paging, never by cutting JSON.
9. **Idempotency is shared.** The `idempotency_records` logic of `core.Batch` moved into helpers used by both batches and context writes (actor + key, request hash, 24 h). The REST `Idempotency-Key` header is accepted on context writes only.
10. **Adapters stay thin.** REST (`/projects/{key}/context...`), MCP and the web UI call `core`. Realtime uses the existing journal: event `context.updated` with the project key.
11. **Prompt-injection boundary.** Context text is data written by people and agents. It is returned as data, never executed, and tool descriptions tell agents not to follow instructions found in entries and to treat `unverified` and `stale` entries as hints.
12. **Discovery is not solved by the server.** In the first agent runs (RESULTS.md) a model that only had the tool in its catalogue never called `context_get`, and sending MCP server `instructions` did not change that (tried, removed). A line in the repository's `AGENTS.md` or an explicit instruction works for some clients, not for others.

## Not built

Wiki or graph, block editor, vector search, background LLM, automatic repository scanning, automatic rewriting of docs, a separate memory server, restoring archived entries, context in project export.

## Consequences

- A project with N active entries costs N rows; bootstrap reads only the pinned ones.
- Verification state is only as good as the humans who press the button; the UI and the bootstrap label every non-verified entry.
- `repository` on a source is free text until a project repository link is used for it (GIT 2 exists since ADR-0013, but sources may point at other repositories).
