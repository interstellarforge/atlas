# ADR-0026: FTS5 index for item descriptions

- Status: accepted
- Date: 2026-10-28

## Context

ATL-76. Title search by `LIKE` is fast (3 ms p95 worst case on 10.000 items, ATL-291) but cannot search descriptions. The ticket asked for FTS5 after a measured need; the owner asked for it anyway to search descriptions.

## Decision

1. `items_fts` is an FTS5 table with external content (`work_items`, rowid = id) over title and description, `unicode61` with diacritics removed. Triggers on insert, delete and update of those two columns keep it in sync inside the writing transaction; migration 0026 builds it from existing rows.
2. The `q` filter is `title LIKE %q% OR id IN (FTS match)`. Titles keep substring matching; descriptions match on word prefixes, all words required. Free text is split into words and each is quoted before it reaches FTS5, so no user input is parsed as FTS syntax.
3. Comments are not indexed.

## Consequences

- Larger database (a copy of the tokens of every title and description) and a little more work per item write.
- Search latency with the FTS branch is not benchmarked yet; the ATL-291 numbers cover the `LIKE` branch only.
