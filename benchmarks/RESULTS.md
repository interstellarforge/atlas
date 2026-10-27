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

## Baseline 2 (WRK, 2026-10-27): subtree strategies

Same machine and dataset. Subtree of the first epic (31 items), 500 runs. The earlier "subtree prefix query" row in Baseline 1 used `LIKE` over a ~3.3k-row subtree and is not comparable.

| Strategy | p50 | p95 |
|---|---|---|
| `path LIKE prefix%` | 1.11 ms | 1.27 ms |
| `path` range scan | 19 µs | 20 µs |
| Recursive CTE | 52 µs | 59 µs |

Column page (50 cards): p50 51 µs, p95 58 µs (FULL). Single update: p50 120 µs / p95 197 µs (FULL), 64 µs / 120 µs (NORMAL). Decision: ADR-0003.

## Baseline 3 (KAN, 2026-10-27): board over HTTP

Same machine (Apple Silicon Mac, local loopback, so no network latency), native binary, 10.000 items in one project (about 2.000 per status column). Numbers include the `curl` process start, so the server is faster than shown.

| Request (200 runs) | p50 | p95 |
|---|---|---|
| `GET /projects/P01/board?limit=50` (5 columns x 50 cards, 45 KB) | 7.5 ms | 10.1 ms |
| `GET /projects/P01/board/columns/{id}?limit=50` | 2.6 ms | 4.3 ms |
| `POST /items/{key}/move` (status change, 100 runs) | 1.9 ms | 2.8 ms |

Targets: board p95 <= 150 ms, update p95 <= 100 ms: met on this machine, not yet on low-end hardware.

Memory: 50 concurrent SSE streams did not raise RSS measurably (25 MB after the streams, 23 MB after closing them).

Browser (built-in browser, same data): the board with ~2.000 cards per column renders 55 cards in the DOM; the list view 19 rows; the hierarchy view 9.970 rows with 24 in the DOM. No render timings were taken.

## MCP catalog (2026-10-27)

`tools/list` for the 8 current tools: 5.978 bytes of JSON (about 1.5k tokens, estimate at 4 bytes/token). Every tool call logs one line (`mcp call tool= ms= bytes= error=`); no aggregation or p95 yet.

## Image upload and thumbnails (REL, 2026-10-27)

Process peak RSS (`/usr/bin/time -l` on the `internal/media` test binary, Apple Silicon Mac): validating and thumbnailing one 20 MP image. Upload validation reads only the header; the thumbnail decodes the full image, one at a time.

| Image (5000 x 4000) | File | Peak RSS |
|---|---|---|
| idle test binary | | 10,8 MB |
| JPEG | 1,6 MB | 84,7 MB |
| PNG | 0,5 MB | 134,4 MB |

Reproduce: `ATLAS_BENCH_IMAGE=<file> /usr/bin/time -l <media.test> -test.run Thumbnail`. Not measured: how long the Go runtime keeps that memory after the thumbnail, concurrent uploads, corrupt images, WebP at 20 MP.

## Not measured yet

Low-end hardware, concurrent writers under load, uploads, reparenting large subtrees, large-subtree comparison, MCP metrics.
