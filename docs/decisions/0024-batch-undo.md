# ADR-0024: Undo of agent batches

- Status: accepted
- Date: 2026-10-28

## Context

ATL-74: an admin must be able to reverse a batch an agent applied (`items_write`), without destroying later human edits. The audit log does not hold restorable values (descriptions are omitted to stay small).

## Decision

1. `Batch` records one `batch_inverses` row per applied operation in the batch transaction: the fields that changed with their values before and after (title, description, status, priority, assignee, due_at, labels, parent, custom_data, milestone, sprint, archived), plus the item version after the operation. Creates store the keys of subitems created below them.
2. `UndoBatch` (REST `POST /batches/{id}/undo`, MCP `batch_undo`) runs in one transaction, newest operation first, through `UpdateItem`, so workflow rules, audit and events apply. It needs project admin on every project the batch touched.
3. Conflicts: a field is restored only if the item still holds the value the batch set. Otherwise it is left and reported; `force` overrides. A created item whose version changed after the batch is a conflict too.
4. Created items are archived (nothing in Atlas is deletable). Subitems of a created tree are archived with it, unchecked.
5. A batch is undone once (`batch_undos`). The undo itself is not undoable; its changes carry batch id `undo-<id>` in the audit log.
6. Records are pruned daily after the event retention.
7. Not restored: board rank. A hard failure (a transition the workflow now forbids) rolls the whole undo back.
