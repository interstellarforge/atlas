# ADR-0014: Progress, timeline and time tracking

- Status: accepted
- Date: 2026-10-27

## Context

INS 1 asks for a multi-colour progress view, INS 2 for a project timeline (open question: what is on it?), INS 3 for a project summary with time spent per ticket (open question: manual only, or also automatic counting in `active` statuses?).

## Decisions

### Progress

1. **One grouped query**: `core.ProjectProgress` runs `SELECT status_id, priority, type, COUNT(*) ... GROUP BY` over the project's non-archived items and folds the result in Go into categories (todo/active/completed), statuses (workflow order, empty ones included), priorities and types. Archived items never count. Right: `view`. REST `GET /projects/{key}/progress`, MCP `project_progress`.
2. **Colours come from the design language**: segments per status use `statusTone`; category, priority and type use fixed token colours. Colour is never the only signal: the bar is an `img` with a text alternative, the legend lists name and count, and a per-dimension toggle switches the breakdown. A compact bar (status split only) is shown in the project header and on project cards.

### Timeline

3. **What is on it: both options.** The planning tab is a Gantt of milestones with a due date (diamonds) and items with a due date (bar from creation to due date, coloured by status category). The second tab is the activity journal from `audit_entries`. Items have no start date; adding one is out of scope, so the bar starts at creation.
4. **Only the visible range is loaded**: `GET /projects/{key}/timeline?from&to` (unix ms, at most five years, default limit 200, max 500, `truncated` when more match). Zoom (week = 4 weeks, month = 90 days, quarter = 1 year) and paging move the window. This replaces virtualisation: the server never returns more than 500 rows.
5. **Activity** is `GET /projects/{key}/activity?before=<id>&limit=` (newest first, cursor on the audit id, default 50, max 200). It lists `item.*`, `comment.*`, `milestone.*`, `attachment.*` and `time.*` entries of the project's items (target is the project key or `KEY-n`) so members and tokens, role changes and settings stay out of it. Right: `view`.
6. **Accessible alternative**: the chart has a list view with the same data; bars carry an `aria-label` with key, title, dates and status.

### Time tracking

7. **Manual booking only.** Automatic counting while an item sits in an `active` status measures how long a ticket was open, not how long someone worked on it, forgets weekends, parallel work and wait time, and needs a state machine that survives status renames, restores and clock changes. A booking is a fact stated by a person. Automatic counting can be added later as another source of entries without changing the model.
8. **Setting per project**, default off: `projects.time_tracking`, switched by `PUT /projects/{key}/time-tracking` with the `workflow` action (the same right as the other workflow settings; shown in the workflow settings). Turning it off keeps the bookings and hides the UI; booking while off is a `rule_violation`.
9. **Entries** (`time_entries`): item, project, user, minutes (1 to 1440), `spent_on` (`YYYY-MM-DD`, not more than one day in the future), optional note (500 characters), and the item's status at the moment of booking, so time can be split per status. Duration input is `90`, `45m`, `1.5h` or `1h30m`; it is rounded to the nearest whole minute.
10. **Right `time.log`** (new action in the matrix, ADR-0010). Migration 0017 grants it to every role that holds `item.update` (viewer: no; member and admin: yes; custom roles with `item.update`: yes). Everyone who can `view` reads bookings and summaries. Editing or deleting needs `time.log` and being the owner of the entry, or the project role `admin`. Every mutation writes item-level audit (`time.log`, `time.update`, `time.delete`) and a change event in one transaction; the item version does not change.
11. **Summary**: `GET /projects/{key}/time?from&to&user&item&period=day|week|month` returns the total and the minutes per ticket (top 100), person, status and period. `GET /projects/{key}/time/export` (same filters, `export` action) is a CSV of the bookings (at most 10.000 rows) with spreadsheet formula characters in text neutralised. MCP: `time_log` and `time_summary`.
12. **Not built:** estimated vs spent time (optional in the story), per-booking history, timers.

## Consequences

- Time summaries scale with the number of bookings, not tickets: aggregation runs on `time_entries` first and names are joined after. Measured in `benchmarks/RESULTS.md`.
- Deleting a user is not supported, so bookings never lose their person; deleting an item or project cascades to its bookings.
- A status deleted later leaves bookings with an unknown status (`ON DELETE SET NULL`).
- Weeks in period grouping are SQLite `%W` weeks (Monday first, week 00 before the first Monday).
