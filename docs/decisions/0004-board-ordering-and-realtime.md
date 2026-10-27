# ADR-0004: Board ordering, realtime and windowing

- Status: accepted
- Date: 2026-10-27

## Decisions

1. **Fractional rank keys.** A card position is a string key between its neighbours (digits `0-9A-Za-z`, ASCII order, never ending in `0`). A move writes one row. Appending steps the first digit, so keys grow by one character per ~30 appends; once a key passes 24 characters the column is rebalanced into short, evenly spaced keys in the same transaction and a `board.rebalanced` event tells clients to refetch. Ties (equal keys) are ordered by id and also trigger a rebalance when a move needs a strict gap.
2. **Moves are optimistic and versioned.** `POST /items/{key}/move` needs the item's `version`; neighbours are given by key (`after`/`before`) and must be in the target column of the same project. Moving between projects is not supported: item keys are project-bound.
3. **One journal, two readers.** Mutations write `change_events` in their transaction; the SSE hub only wakes subscribers after commit, and each subscriber reads the journal itself (`seq` cursor). SSE and `GET /changes` use the same read, filtered by the caller's project access. Streams are closed after 10 minutes, so credentials are re-checked on every reconnect. A cursor ahead of the journal (database replaced) or behind its oldest entry (pruned) yields `resync_required`.
4. **Own windowing instead of TanStack Virtual.** Cards and rows have fixed heights, so windowing is about 40 lines (`VirtualList.svelte`) and the same arithmetic maps a pointer position to a drop index. This avoids a dependency for a few lines (see AGENTS.md). Revisit if variable-height rows are needed.
5. **Pointer-event drag and drop** from a handle (`touch-action: none`), plus Alt + arrow keys for keyboard users. No drag library.

## Consequences

- Board reads stay cheap: a column page is one indexed query on `(project_id, status_id, rank)`.
- Clients must handle `resync_required`, `board.rebalanced` and reconnects; the web client reloads the board in those cases.
- WIP limits are warnings only, as designed; cross-project boards do not merge limits.
