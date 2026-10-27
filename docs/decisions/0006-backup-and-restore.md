# ADR-0006: Backup and restore

- Status: accepted
- Date: 2026-10-27

## Decisions

1. **Operator CLI, not an HTTP endpoint.** `atlas backup <dir>` and `atlas restore <dir>` run against the data directory (`internal/backup`). A backup holds secrets-free data (tokens are hashed) but is the whole database, so it is not exposed over HTTP.
2. **Online backup with `VACUUM INTO`.** The database is snapshotted first while the server keeps writing. Uploads are immutable and never deleted, so copying exactly the files the snapshot references gives a consistent set (no locking, no quiesce). Thumbnails are not backed up; they are regenerated on demand.
3. **Manifest with checksums.** `manifest.json` lists every file with size and SHA-256, plus the last migration and the database generation. Restore verifies all of it before touching the data directory.
4. **Offline restore, new generation.** Restore needs the server stopped. The previous database is kept in `backups/pre-restore-<time>`. The restored database gets a new `meta.generation`, so connected clients reload everything (SSE `hello`). Pending migrations run at the next start as usual; `quick_check` runs on every start.
5. **Export/import are separate.** JSON/CSV export and import are per project through REST (`core.ExportProject`, `core.ImportItems`); they are for moving data, not for disaster recovery.

## Consequences

- A backup taken at time T may contain uploads newer than the snapshot only if they were referenced by it; files uploaded after T are simply not in it.
- If uploads ever become deletable, the protocol must change (copy first, or hard-link).
- Restore does not merge: it replaces the database.
