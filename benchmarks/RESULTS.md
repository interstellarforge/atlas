# Benchmark results

Measured numbers only. Targets live in `docs/performance.md`.

## Baseline 1 (Foundation, 2026-10-27)

- Hardware: developer Mac (Apple Silicon, arm64), SSD. **Not representative hardware**: repeat on the low-end target machine.
- Dataset: 10.000 items, 3 projects (`atlas-seed`, seed 1). SQLite level only; no HTTP board endpoint exists yet.
- Reproduce: `ATLAS_BENCH=1 go test ./benchmarks -run Baseline -v` and `benchmarks/rss.sh`.

| Query (500 runs) | synchronous | p50 | p95 |
|---|---|---|---|
| Column page, 50 cards | FULL | 127 µs | 312 µs |
| Column page, 50 cards | NORMAL | 90 µs | 106 µs |
| Subtree prefix query (`path LIKE '/1/%'`, ~3.3k rows) | FULL | 2.97 ms | 5.82 ms |
| Subtree prefix query | NORMAL | 2.35 ms | 2.84 ms |
| Single item update (write tx) | FULL | 200 µs | 368 µs |
| Single item update (write tx) | NORMAL | 136 µs | 235 µs |

Note: update latency on a Mac SSD says little about fsync cost on real servers; FULL vs NORMAL must be re-run on target hardware.

| Process memory | Result |
|---|---|
| Server idle RSS, 10.000 items (native binary) | ~19 MB |
| Container memory after one login (`docker stats`, includes page cache) | ~76 MiB of 384 MiB |
| Image size | 21.8 MB |

## Not measured yet

Board API p95 over HTTP, concurrent clients, SSE, uploads, materialized path vs recursive CTE (WRK), MCP metrics.
