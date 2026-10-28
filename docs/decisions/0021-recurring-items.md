# ADR-0021: Recurring items

- Status: accepted
- Date: 2026-10-28

## Context

ATL-72: teams want items that appear on a schedule. The scheduler must not create duplicates.

## Decision

1. Table `recurrences` (migration 0021) holds the item template, the rule (`daily`, `weekly`, `monthly`, interval, weekday or month day, minute of day, UTC) and `next_run_at`.
2. `internal/scheduler.Every` ticks every minute and calls `core.RunDueRecurrences`. Adapters hold no logic.
3. Idempotency: one transaction advances `next_run_at` (`WHERE next_run_at = <seen>`) and creates the item. A crash creates nothing; a second runner finds the claim taken.
4. Items are created as the user who created the recurrence, so project access is checked at run time. A failing run (archived parent, lost access) skips the occurrence and stores `last_error`.
5. After downtime missed occurrences are skipped, one item is created. UTC only. Both can be upgraded when needed.
6. Only human sessions manage recurrences; there is no MCP tool or UI yet.
