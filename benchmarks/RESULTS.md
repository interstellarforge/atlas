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

`tools/list` for the 8 current tools: 5.978 bytes of JSON (about 1.5k tokens, estimate at 4 bytes/token). Every tool call logs one line (`mcp call tool= ms= bytes= error=`) and is aggregated per tool at `GET /api/v1/mcp/stats` (p95, bytes, paginated and `too_large` counts).

### Agent scenarios (2026-10-27, local Atlas, 2 items)

| Client | Scenario | Result | Tool calls | Bytes returned |
|---|---|---|---|---|
| Claude Code 2.1.307 (haiku) | board_get, move to In Progress, comment, 2 checklist items, item_get | all steps correct | 5 | 1.943 |
| Codex CLI 0.145.0 | items_search, move, stale-version update, retry | conflict surfaced as `conflict`, retry with the current version succeeded | 4 | not measured |

Codex refuses mutating MCP tools in non-interactive mode unless approved: `-c mcp_servers.atlas.tools.items_write.approval_mode="approve"`. Claude Code needs `--allowedTools "mcp__atlas__*"`. Two scenarios on two small models, one run each: this shows the protocol path works, not reliability across many tasks.

## Image upload and thumbnails (REL, 2026-10-27)

Process peak RSS (`/usr/bin/time -l` on the `internal/media` test binary, Apple Silicon Mac): validating and thumbnailing one 20 MP image. Upload validation reads only the header; the thumbnail decodes the full image, one at a time.

| Image (5000 x 4000) | File | Peak RSS |
|---|---|---|
| idle test binary | | 10,8 MB |
| JPEG | 1,6 MB | 84,7 MB |
| PNG | 0,5 MB | 134,4 MB |

Reproduce: `ATLAS_BENCH_IMAGE=<file> /usr/bin/time -l <media.test> -test.run Thumbnail`. Not measured: how long the Go runtime keeps that memory after the thumbnail, concurrent uploads, corrupt images, WebP at 20 MP.

## v1.0 validation (REL, 2026-10-27)

Hardware: developer Mac (Apple Silicon, arm64, SSD), loopback. **Not representative low-end hardware**; this is the main gap in this validation. Reproduce: `ATLAS_BENCH=1 go test ./benchmarks -run Concurrent -v`, `benchmarks/rss.sh`, `docker run -m 384m`.

| Target | Result | Verdict |
|---|---|---|
| Idle RSS <= 100 MB | 21.8 MB (native, 10.000 items); container idle 41 MiB | met on this machine |
| Normal use <= 200 MB | 19.8 MB after 2.000 requests (native); container 75 MiB after login + 3.000 requests, with `-m 384m` | met on this machine |
| Container 384 MB | image 25.9 MB; 75 MiB in use under that limit; large-image thumbnails peak at ~135 MB (see below) | met |
| Board p95 <= 150 ms | 5 concurrent clients, real HTTP, 10.000 items: p50 14 ms, p95 28-33 ms (5 x 50 cards) | met on this machine |
| Update p95 <= 100 ms | same run, PATCH: p50 1.8 ms, p95 9-10 ms | met on this machine |
| >= 5 concurrent clients | 5 clients x 100 rounds, no errors | met |

`synchronous=FULL` vs `NORMAL` through HTTP with 5 clients: board p95 28-33 ms and update p95 9-10 ms for both (three runs, no consistent difference). At SQLite level (Baseline 1 and `TestBaseline`) NORMAL is faster for writes (update p95 about 0.57 ms vs 0.16 ms in the latest run), but fsync cost on a Mac SSD says little about a server disk: repeat on target hardware before changing the FULL default.

Not measured in this local run: low-end hardware, more than 5 clients, container memory with the 10.000-item dataset loaded (the container run used an empty database), sustained write load, import of 20.000 rows, backup duration with many uploads. The representative server run below closes the low-end hardware gap.

## Representative server validation (REL, 2026-10-28)

Hardware: Debian 13 VM on KVM, 4 vCPU (Intel Core i5-10210U), 1.9 GiB RAM, 4 GiB swap, ext4, Docker 29.9.2. Commit `4c5179475e520298aef3d315aefaf53241c71232`, Go 1.27.3, 10.000 items, 200 context entries. Three repetitions with `benchmarks/server.sh all`; the complete benchmark suite passed in 45.4 seconds.

| Target | Worst observed result over 3 runs | Verdict |
|---|---|---|
| Idle RSS <= 100 MB | 27.1 MB | met |
| Normal use <= 200 MB | 31.3 MB after 2.000 health requests; concurrent benchmark process 106 MB including server, clients and seed | met |
| Container 384 MB | production limit 384 MiB; 22.6 MiB idle; not OOM-killed | met |
| Board p95 <= 150 ms | 59.4 ms with 5 concurrent clients | met |
| Update p95 <= 100 ms | 34.4 ms with 5 concurrent clients | met |
| >= 5 concurrent clients | 5 clients x 100 rounds, no errors in any run | met |
| Context p95 < 100 ms and default <= 4 KiB | bootstrap p95 0.87 ms, 3.848 bytes | met |

`synchronous=FULL` remained within target but was slower for writes. Across the three HTTP runs, FULL board p95 was 32.9-59.4 ms and PATCH p95 31.2-34.4 ms; NORMAL board p95 was 38.4-38.8 ms and PATCH p95 12.7-15.7 ms. At SQLite level, item-update p95 was 5.82-6.83 ms with FULL and 0.078-0.089 ms with NORMAL. The durability default remains FULL; these measurements do not change that decision.

The benchmark harness used a 1 GiB limit to leave room for compilation. The production container's 384 MiB limit and idle use were checked separately, so the harness run is not evidence of peak container memory under the production limit.

## Not measured yet

More than 5 clients, sustained concurrent writers, uploads on the server, reparenting large subtrees, large-subtree comparison, and MCP calls per user action beyond the two scenarios above.

## Merged board columns (SET, 2026-10-27)

Core level (`svc.Board`, no HTTP), Apple Silicon Mac, 10.000 items in one project, 50 cards per column, 200 runs: per-status columns (5) p50 6,0 ms / p95 6,7 ms; merged columns (3) p50 15,1 ms / p95 15,7 ms. The merged read orders the union of a category's statuses by rank. Board over HTTP with 5 clients (default mode) after the change: p50 15 ms, p95 27 ms, update p95 8 ms, unchanged from the REL validation. Reproduce: `ATLAS_BENCH=1 go test ./benchmarks -run 'MergedBoard|Concurrent' -v`.

## Progress, timeline and time (INS, 2026-10-27)

Core level (no HTTP), Apple Silicon Mac, 10.000 items in one project, 20.000 time entries, 10.000 audit entries, 100 runs. Progress p50 12,4 ms / p95 12,9 ms; timeline of 90 days p50 6,7 ms / p95 7,2 ms; activity (50 entries) p50 11,9 ms / p95 12,4 ms; time summary (total, per item, person, status and period over all 20.000 entries) p50 89 ms / p95 92 ms (158 ms before aggregating on the raw column and joining names afterwards). All below the 150 ms board target; the time summary is the one to watch on slow hardware. Reproduce: `ATLAS_BENCH=1 go test ./benchmarks -run Insight -v`.

## Atlas Context (CTX, 2026-10-27)

Hardware: Apple M1 Pro, 16 GB. Core level (no HTTP), 10.000 items plus 200 context entries (about a sixth pinned, summaries of about 230 characters, one or two sources) in one project, 200 runs. Bootstrap p50 0,40 ms / p95 0,58 ms, 3.848 bytes. Search "module 7" p50 0,41 ms / p95 0,56 ms, 2.995 bytes. Detail of 3 entries with bodies p50 0,30 ms / p95 0,48 ms, 3.992 bytes. Unbudgeted list of all 200 entries (REST, no bodies) p50 3,7 ms / p95 4,0 ms, 115.980 bytes. Targets (p95 < 100 ms, default answer at most 4 KiB) met. RSS with `benchmarks/rss.sh` (idle, then 2.000 health requests): 24,0 / 25,8 MB without context, 24,2 / 24,3 MB with 200 entries; the load does not call context endpoints. Reproduce: `ATLAS_BENCH=1 go test ./benchmarks -run 'TestContext$' -v`, `CONTEXT=200 benchmarks/rss.sh`.

MCP catalogue: 18 tools, 15.232 bytes of JSON (about 3,8k tokens at 4 bytes/token); `context_get` and `context_write` are 2.754 bytes of that (about 690 tokens), so 16 tools were 12.478 bytes before. The 8 tool / 5.978 byte figure above predates the issue, progress and time tools.

### Agent runs with and without context_get

Setup: Atlas on localhost with project ATL holding 13 hand-written entries about this repository (all verified by a human session, 6 pinned; the entries answer the three tasks, see below). Agents ran read-only in the repository (Claude Code 2.1.307 with haiku: Read, Grep, Glob and `context_get` only, `CLAUDE.md` loading and user settings off; Codex CLI 0.145.0 with the default model `gpt-5.6-terra`, reasoning effort low, read-only sandbox, a clean `CODEX_HOME`). Tasks: find the project permission check (auth), list what to change for a new role action (migration), what to know before adding `Idempotency-Key` to the REST item routes (pitfall). Variants: `without` (no Atlas MCP), `with` (Atlas MCP plus the AGENTS.md paragraph in the system prompt or prompt), `server` (Claude only: MCP connected and server `instructions`, nothing else; the feature was removed afterwards), `explicit` (as `with`, plus "first call context_get"). Claude: 3 runs per cell, 9 per variant. Codex: 2 runs per cell planned; stopped after 5 runs per variant (pitfall: 1) because a run takes about two minutes. "Tokens" is everything the client reports as input plus output, including cached system prompt, so it is dominated by a fixed overhead and noisy; tool calls and bytes returned by tools are the cleaner signals.

| Client | Variant | Runs | `context_get` calls | Tool calls (mean) | Bytes returned by tools (mean) | Tokens (mean) |
|---|---|---|---|---|---|---|
| Claude haiku | without | 9 | 0 | 9,0 | 24.342 | 208.024 |
| Claude haiku | with | 9 | 0 | 8,0 | 22.490 | 194.453 |
| Claude haiku | server | 9 | 0 | 8,7 | 23.677 | 232.027 |
| Claude haiku | explicit | 9 | 18 | 3,8 | 4.939 | 134.558 |
| Codex | without | 5 | 0 | 3,2 | 77.970 | 110.154 |
| Codex | with | 5 | 4 | 3,6 | 56.108 | 151.136 |
| Codex | explicit | 5 | 5 | 1,8 | 7.872 | 67.611 |

Reading:

- When the agent calls `context_get`, it needs fewer steps and far fewer bytes (Claude: 3,8 calls and 4,9 KB instead of 9,0 and 24,3 KB; Codex: 1,8 calls and 7,9 KB instead of 3,2 and 78 KB) and reads no files. Answers to auth and migration were right in every run in all variants (keyword check; one Codex migration run without context missed it).
- Without an explicit instruction Claude haiku never called the tool, with the paragraph in its system prompt (0 of 9) or with server instructions (0 of 9): the benefit only exists when the agent is told to use it. Codex used it in 4 of 5 runs with the paragraph, but its token total did not drop in that variant (151k vs 110k, one run each cell at this size is noise).
- The pitfall keyword check (documented as not implemented, mentions `Batch`) scored 0 to 2 of 3 in every variant and is unreliable: the answers I read were substantively right. It is also contaminated: this change's own docs (`docs/api-contract.md`) now state the pitfall, so the no-context variant could find it.
- Bias: the entries were written by the author of the tasks, so they answer them directly. This is an upper bound for a hand-curated entry on a question it was written for, not the saving on questions nobody anticipated. Two small models, three tasks, few runs: no claim of a general saving. Not measured: entries that are wrong or stale, questions the entries do not cover, a human-written set for another repository.
