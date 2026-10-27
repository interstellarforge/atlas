# v1.0 release checklist

Status 2026-10-27. The original 12-point "definition of success" is not in this repository; this list is derived from the targets in `architecture.md`, `performance.md` and the REL stories. Replace or extend it with the original points.

| # | Point | Status | Evidence |
|---|---|---|---|
| 1 | One container, no external services | done | `Dockerfile`, `compose.yaml`; image 25.9 MB |
| 2 | Memory: idle <= 100 MB, normal <= 200 MB, container 384 MB | met on dev machine | `benchmarks/RESULTS.md` (v1.0 validation) |
| 3 | Board p95 <= 150 ms, update p95 <= 100 ms, 5 clients, 10.000 items | met on dev machine | same |
| 4 | Column of 2.000 items stays virtualized | done | Baseline 3 (55 cards in the DOM) |
| 5 | Realtime: UI, REST and agent changes reach every client | done | SSE, `internal/api/sse_test.go` |
| 6 | MCP read and write with real agents | done, small sample | Claude Code and Codex scenarios in RESULTS.md |
| 7 | Agent safety: scopes, rate limit, write quota, mass-archive guard, audit with actor and batch id | done | `docs/mcp-contract.md` |
| 8 | Attachments, comments, checklists, notifications, milestones | done | stories #53-#58 |
| 9 | Backup and tested restore | done | ADR-0006, `docs/operations.md` |
| 10 | Export and import with dry-run | done (REST only, items only) | `docs/api-contract.md` |
| 11 | Accessibility audit | partly | `docs/accessibility.md`: automated pass; no screen-reader test |
| 12 | License decided | AGPL-3.0 in `LICENSE` and ADR-0001; owner to confirm before first public release | |

## Open before calling it 1.0

- Measure on representative low-end hardware and re-run FULL vs NORMAL there (the targets are only shown on a developer Mac).
- Screen-reader and full keyboard pass; target-size leftovers on cards (fixed by the UIX redesign).
- Export/import UI, and import of comments, checklists, links, attachments and assignees, if needed.
- Confirm the license and replace this list with the original 12 points.
