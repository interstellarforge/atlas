# ADR-0011: Required workflow steps and merged board columns

- Status: accepted
- Date: 2026-10-27

## Context

SET 11 asks for "an item may not enter Done without having been in Review". SET 12 asks to show statuses of one category (for example In Progress, Review, Blocked) in one board column. Today a workflow has optional `workflow_transitions` (no rows = anything goes), a per-status WIP limit and `allow_create`.

## Decisions

### Required steps

1. **A rule per status: "requires first".** `status_requirements (status_id, required_status_id)`. An item may enter a status only if, for every required status, it is in it now or has been in it before. No rows = no rule (migration default), so nothing changes until an admin sets one.
2. **History is its own small table, not parsed from the audit log.** The story preferred deriving history from the journal. We store `item_status_visits (item_id, status_id)` instead: the audit log holds status *names* (a rename would silently break the rule), has no entry for the status an item is created in, and is a log meant for humans. The table is keyed by status id, written in the same transaction as the move (create, `MoveItem`, `UpdateItem`), at most one row per item and status. Migration 0014 backfills the current status plus every status the audit log shows an item moving from or to; older history that was never logged cannot be recovered.
3. **Chosen over fixed transitions.** "Only Review -> Done" as a transition would forbid going back and forth (Done -> Todo -> Done). A required step allows that after the first pass. Transitions stay as they are and combine with required steps: both must allow the move.
4. **Enforced in `core` next to the transition check** (`checkRequiredSteps`) in create, `MoveItem` and `UpdateItem`, so REST, MCP, import and templates follow. Creating an item directly in a status with a rule fails (it has visited nothing). Import turns the same error into a row error; nothing is written (existing import behaviour). Error: `status "Done" requires the item to have been in Review first` (HTTP 422 `rule_violation`, same over MCP).
5. **Rights:** the existing `workflow` action. A status cannot require itself; cycles are allowed (items can start anywhere that `allow_create` permits).
6. **No admin bypass.** The story marked it optional. A bypass would need a reason field and an audit path; it is not built. Removing the rule is one workflow change, and audited.
7. **Templates:** `TemplateStatus.requires` (names) and `ProjectTemplateData.merge_columns`, both optional, so template version stays 1.

### Merged columns

8. **One project switch, grouping by category.** `projects.merge_columns` (default 0 = one column per status). On: the board returns one column per category, ordered by its first status; `Column.status` is that first status, `Column.statuses` lists all members. The finer "show in column of <status>" per status is not built; it can be added without changing the column shape.
9. **A column shares one rank order.** Ranks stay per item; all rank queries (neighbours, append, rebalance) take the set of statuses of the column (`columnOf`). A move inside a merged column keeps the item's status; a move from another column sends the target status name, and the client sends the first status of the column. Moving between statuses of one column is a normal status change: transitions and required steps apply. Changing status via `UpdateItem` inside a merged column keeps the item's place.
10. **WIP stays per status.** A merged column is flagged `over_wip` when any member status is over its own limit. A column-wide limit would change what the number means for existing projects.
11. **Cards show their status** in a merged column (chip with the status colour, and in the accessible name), so the screen reader announces column and status. "Add item" uses the first status of the column that allows creating.
12. **Toggling is a `project.updated` event**, so clients refetch workflow and board.

## Consequences

- Merged boards read slower: 10.000 items, 50 cards per column, core level: p95 15.7 ms merged vs 6.7 ms per-status (`ATLAS_BENCH=1 go test ./benchmarks -run MergedBoard -v`). The target is 150 ms; the default mode is unchanged. If it matters, read per status with `LIMIT` and merge in Go.
- `item_status_visits` grows by at most the number of distinct statuses per item.
- The visit set records that an item *was* in a status, not when or how long. Time-in-status would need a real history table.
