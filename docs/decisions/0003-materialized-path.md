# ADR-0003: Materialized path for hierarchy

- Status: accepted (revisit if reparenting becomes a hot path)
- Date: 2026-10-27

## Context

`parent_id` is canonical. The architecture asked whether the derived `path` column earns its complexity compared with an indexed recursive CTE.

## Measurements

Subtree read of one epic (31 items) in the 10.000-item seed, 500 runs, Apple Silicon (see `benchmarks/RESULTS.md`):

| Strategy | p50 | p95 |
|---|---|---|
| `path LIKE prefix%` | 1.11 ms | 1.27 ms |
| `path >= lo AND path < hi` (range) | 19 µs | 20 µs |
| Recursive CTE on `parent_id` | 52 µs | 59 µs |

`LIKE` is slow because SQLite's default LIKE is case-insensitive and cannot use the index; a range scan can.

## Decision

Keep `path`, queried with the range form `[path, prefix-with-'0')` (`'0'` follows `'/'` in ASCII). The CTE stays for consistency checks (`atlas check paths`) and for the link-cycle check, where it needs no stored state.

## Consequences

- Subtree reads are about 2.7x faster than the CTE on this dataset. Both are well inside the latency targets, so the gain is modest on small trees and grows with subtree size.
- Reparenting rewrites all descendant paths in one transaction. Depth is capped at 5 and the cost was not benchmarked yet. Revisit this ADR if reparenting large subtrees is slow; the fallback is dropping `path` and using the CTE, which only needs `parent_id`.
- `path` can drift if written outside the service: `atlas check paths [--fix]` detects and repairs it.
