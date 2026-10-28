# Performance targets

All values are acceptance targets, not measured results. Record measurements in `benchmarks/RESULTS.md` with hardware and date.

| Metric | Target |
|---|---|
| Idle RAM | <= 100 MB RSS |
| Normal use | <= 200 MB RSS |
| Container memory | 384 MB initial |
| Idle CPU | negligible |
| Board API p95 | <= 150 ms (LAN) |
| Item update p95 | <= 100 ms (LAN) |
| Dataset | 10.000 work items supported |
| Column size | 2.000 items virtualized |
| Concurrent clients | >= 5 |
| Containers / external DBs | 1 / 0 |
| MCP default response | <= 16 KiB |
| Context bootstrap and search (200 entries) | p95 < 100 ms, default answer <= 4 KiB |

## Method

- Seed generator produces a reproducible 10.000-item dataset (deep hierarchies, labels, links).
- Workloads: concurrent UI + MCP writes, SSE clients, large hierarchies, search, uploads, thumbnail generation.
- Run on representative (low-end) hardware, not only a developer machine.
- Measure before optimizing: profile and benchmark before adding caches or infrastructure.

### Representative server with Docker

The server only needs Docker and Git; a host Go installation is not required. From the repository root, run:

```sh
benchmarks/server.sh all | tee "performance-server-$(date +%F-%H%M).log"
```

Use `benchmark` or `rss` instead of `all` to run one part. The runner archives the exact committed `HEAD` from the read-only Git directory, so untracked files such as `.env` are neither copied nor exposed. It uses persistent Docker volumes for the Go module and build caches. Run `benchmarks/server.sh --help` for supported overrides.

## Benchmarks required

1. Materialized path vs indexed recursive CTE (subtree reads, re-parent).
2. `synchronous=FULL` (default) vs `NORMAL` with the same workload.
3. SQLite driver candidates (ADR-0002).
4. Board API at 10.000 items; column pagination.
5. Image upload/thumbnail RAM peaks with large and corrupt images.
6. MCP: catalog size, bytes per response, calls per task.
7. Project context: bootstrap, search and detail on 200 entries; RSS with and without; agent runs with and without `context_get` (ADR-0015).

## Rules

- No optimization without a benchmark showing the need.
- A missed target triggers profiling first; extra infrastructure is last resort.
