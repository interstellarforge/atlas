# ADR-0023: Sprints

- Status: accepted
- Date: 2026-10-28

## Context

ATL-73: teams want to work in time-boxed sprints or cycles, with board and backlog filters.

## Decision

1. `sprints` (migration 0023): name, start and end (unix ms), `closed_at`; names are unique per project. `work_items.sprint_id` holds at most one sprint per item. Sprints sit next to milestones, which stay release-oriented.
2. The backlog is "no sprint": `?sprint=none` on items and board. `?sprint=<name>` filters one sprint.
3. A closed sprint takes no new items; its items stay, so history keeps its meaning. Moving open items to the next sprint is a normal `sprint` update.
4. Management needs a project admin (as milestones). REST `/projects/{key}/sprints`, MCP `sprint_write`; `workflow_get` lists sprints with progress.
5. Not built: burndown (optional in the ticket; add it when the change journal gives a cheap daily series), web UI, CSV import/export column.
