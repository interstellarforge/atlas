# v1.0 release checklist

Status 2026-10-27. The original 12-point "definition of success" is not in this repository; this list is derived from the targets in `architecture.md`, `performance.md` and the REL stories. Replace or extend it with the original points.

| # | Point | Status | Evidence |
|---|---|---|---|
| 1 | One container, no external services | done | `Dockerfile`, `compose.yaml`; image 25.9 MB |
| 2 | Memory: idle <= 100 MB, normal <= 200 MB, container 384 MB | met on representative server | `benchmarks/RESULTS.md` (representative server validation) |
| 3 | Board p95 <= 150 ms, update p95 <= 100 ms, 5 clients, 10.000 items | met on representative server | same |
| 4 | Column of 2.000 items stays virtualized | done | Baseline 3 (55 cards in the DOM) |
| 5 | Realtime: UI, REST and agent changes reach every client | done | SSE, `internal/api/sse_test.go` |
| 6 | MCP read and write with real agents | done, small sample | Claude Code and Codex scenarios in RESULTS.md |
| 7 | Agent safety: scopes, rate limit, write quota, mass-archive guard, audit with actor and batch id | done | `docs/mcp-contract.md` |
| 8 | Attachments, comments, checklists, notifications, milestones | done | stories #53-#58 |
| 9 | Backup and tested restore | done | ADR-0006, `docs/operations.md` |
| 10 | Export and import with dry-run | done (REST only, items only) | `docs/api-contract.md` |
| 11 | Accessibility audit | partly | `docs/accessibility.md`: automated pass; no screen-reader test |
| 12 | License decided | AGPL-3.0 in `LICENSE` and ADR-0001; owner to confirm before first public release | |
| 13 | Project context for agents (ADR-0015) | built; benefit not proven | `internal/core/context_test.go`, `internal/api/context_test.go`, `benchmarks/context_test.go`; agent runs in RESULTS.md show no saving yet |

## Open before calling it 1.0

- Screen-reader and full keyboard pass; target-size leftovers on cards (fixed by the UIX redesign).
- Export/import UI, and import of comments, checklists, links, attachments and assignees, if needed.
- Confirm the license and replace this list with the original 12 points.
- Project context: show that `context_get` saves agent work (see RESULTS.md, Atlas Context), or simplify the module; screen-reader pass of the Context tab.

## Release gate: registration

Open registration, and organization creation by application, must not be offered (policy `registration: open`, `organization_creation: apply`) until all of these are done:

- The M1-M3 permission tests and the ATL-482 final regression (REST, MCP, SSE, exports, attachments, background jobs) pass on a migrated, seeded copy.
- The REG 1-3 account-status stories are done (the approval queue itself was retired by ADR-0031; accounts are free).
- No management screen exists without its backend tests; gaps found in the regression are Bugs and block the gate.

The upgrade release notes state the default registration policy. Linked from ADR-0029 (authorization model).

## Final security regression (ATL-482), 2026-11-11

Run on a scratch copy: `go run ./cmd/atlas-seed -data-dir <dir> -context 200 -legacy-access` (10.000 items, 3 projects, 60 accounts, legacy roles), then `atlas migrate-orgs --dry-run` / `--confirm-report` with a mapping that sends the three review-required roles (narrow, viewer, wide) to `member`.

| Check | Result |
|---|---|
| `go vet ./...`, `go test ./...` (incl. matrix, support, migration, MCP tests) | pass |
| Migration rehearsal | report with 0 blocking problems, 4 approved transitions; work_items 9999 before and after (the seed's count), integrity and foreign-key checks clean; `TestOrgMigrationRehearsal`: verified backup + migration 0.6 s, rollback and restore verified |
| Latency baseline (`ATLAS_BENCH=1 go test ./benchmarks -run Baseline`) | column page p95 0.1 ms, item update p95 0.3 ms; board of a 3.3k-item project on the migrated copy p95 10 ms over REST |
| REST role sweep on the migrated copy (SUPERADMIN with explicit ADMIN membership, ADMIN, viewer-role USER, narrow-project USER, account without membership) | 15 routes per role: ADMIN gets 403 on `/admin/*`; USER gets 403 on organization members/audit and 404 on projects they are not in; an account without membership gets 404 on the organization and every project |
| Account states | login answers `account_pending`, `account_rejected`, `account_suspended` (403) |
| UI pass (SUPERADMIN, ADMIN, USER) | SUPERADMIN: Organization + Admin nav, Requests tab, Rights tab gone; ADMIN: Organization nav, `/admin` refused; USER: no Organization/Admin nav, foreign project shows the 404 text |

Found and fixed during the passes (all re-verified in the browser on the final build):

- A USER could open `#/p/{KEY}/settings/members` and see the settings view (data stayed protected by the server); it now shows the permission message.
- The project Settings tab was hidden for organization ADMINs (they have no project role); it is offered to them again.
- The Admin Requests tab did not load the waiting applications, and the "Appoint first admin" select was empty on the Organizations tab; both load their data now.
- The projects page flashed the wrong empty state (and a create form) while the organizations were loading; it waits for them.

Also checked in the browser on the final build: pending, rejected and suspended logins show their own message; an account without an organization applies, the SUPERADMIN approves it in Requests and the applicant becomes its ADMIN; the SUPERADMIN appoints the first ADMIN of an organization without one; under a support grant the project Members tab shows the server administrator by name to the SUPERADMIN and as a count to the ADMIN; layout checked on desktop and a 375 px light-theme viewport.

Not covered beyond the Go test suites: MCP, SSE, exports, attachments and background jobs against the migrated copy; real SMTP delivery of the application mails. The registration gate above stays closed until the owner has read this result.
