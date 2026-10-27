# ADR-0017: Security audit hardening

Status: accepted. Epic SEC in Taiga (stories SEC 1-11), from an external audit of commit fa51bad.

## Decisions

1. **User creation is administration.** `core.CreateUser` requires a human global admin and writes the user, audit entry and change event in one transaction. No other path creates users except registration and first-run seeding.
2. **Restore installs a verified database only.** The manifest must list `atlas.db` exactly once and only `uploads/` files; symlinks are refused. The database is staged next to the live one, integrity-checked and re-generationed, then swapped in.
3. **Tokens see people through projects.** `ListUsers` and `AvatarThumb` limit a token to users who share a project it can reach; the admin flag is hidden.
4. **One idempotency primitive.** `core.Idempotent` runs a function in one transaction together with the idempotency record. MCP writes use it (`write` helper), so a late failure rolls back every part and a retry returns the first result. Admin PATCH uses it without a key (`core.UpdateUser`).
5. **MCP errors are mapped centrally** (`fail`): core errors keep their message, others are logged and shown as `internal error`. Quota is refunded when a call fails or replays.
6. **Proxy trust is explicit** (`ATLAS_TRUSTED_PROXIES`), never inferred from headers.
7. Smaller fixes: ReadTimeout, single-document JSON, inline theme script moved to a file (CSP), filename slice bound, CSV formula neutralising in the project export, broader SSRF range list, batched export queries and bounded comment reads (newest 500), audit PII scrub on account deletion, CI workflow.

## Not done

- The project export is still built in memory (queries are batched per page of 200 items); streaming needs a different response format.
- Container base images are not pinned by digest; resolve digests from the registry when the images are next bumped.
