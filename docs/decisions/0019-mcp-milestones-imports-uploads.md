# ADR-0019: MCP milestones, comment import and image upload

- Status: accepted
- Date: 2026-10-28

## Context

A Taiga migration through MCP could not create milestones, upload attachments or keep comment authors and dates.

## Decision

- `milestone_write` adapts `core.{Create,Update,Delete}Milestone` (project admin).
- `comment_add` accepts `author` + `created_at`; `core.ImportComment` requires project admin and stores the actor as `<author> (imported by <actor>)`, so the audit trail shows who imported. No schema change.
- `attachment_add` accepts base64 image data and calls `core.AddAttachment`, so type, size and pixel limits stay in one place. Results still never contain binary.

## Consequences

- The 1 MiB MCP request cap limits uploads to ~700 KB raw; larger files use the web/API upload. Raising the cap per tool is a later change if needed.
- Imported comments show the importer in the actor text; no separate author column.
