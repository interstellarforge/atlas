# ADR-0020: Choose which categories merge into one board column

- Status: accepted
- Date: 2026-10-28

## Context

ADR-0011 merges the statuses of every category with one project switch. Teams want to merge only some, for example Backlog + Todo but keep Done and Rejected apart.

## Decision

1. `projects.merge_categories` (comma separated, board order) lists the categories that merge. Migration 0020 sets all four for projects that had `merge_columns` on.
2. `merge_columns` stays in the API and the table, as "any category merged", so existing clients and templates keep working. `PATCH workflow` accepts `merge_categories`, or the old boolean (all or none).
3. Grouping (`groupStatuses`) and rank sets (`columnOf`) check the category of each status instead of one flag.
4. Templates store `merge_categories`; templates saved earlier with only `merge_columns` still merge everything.
5. Merging stays per category, not per status; picking single statuses is not built.
