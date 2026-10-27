# ADR-0018: Review as a fourth status category

Status: accepted. Taiga story WRK "Vierde statuscategorie review".

## Context

Categories were `todo`, `active` and `completed`. With merged board columns (ADR-0011) a status such as Review sat in the same column as In Progress, and its purple colour was derived from its position among the active statuses.

## Decisions

1. **New category `review`** next to `todo`, `active` and `completed`. It is a category of its own everywhere categories are used: merged board columns, the cross-project board, progress counts (`categories.review`), the `category` item filter, workflow settings, project templates, MCP `project_progress`.
2. **Migration 0019 rebuilds `project_statuses`** (SQLite cannot change a CHECK in place). Ids are kept, so items, transitions, requirements, visits and time entries keep pointing at the right rows. The runner honours a first line `-- atlas:foreign-keys-off`: foreign keys are off while the file runs, `PRAGMA foreign_key_check` must be clean before the commit, and they are switched on again afterwards.
3. **Only statuses named Review (any case) move** from `active` to `review`, in existing projects and in the built-in templates. Other statuses keep their category; admins can change any status to `review` in the workflow settings.
4. **Colour follows the category**: `review` is purple, `active` is orange, `todo` and `completed` as before (the old "second active status is purple" rule is gone). Default workflows (new projects, seed) create Review as `review`.
5. **"Done" counts stay `completed` only**; review items are not done.

## Consequences

- Projects with merged columns get a separate Review column; admins who prefer the old grouping set the status back to `active`.
- Progress consumers that summed `todo + active + completed` must include `review`.
