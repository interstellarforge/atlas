# ADR-0007: App shell and design language

- Status: accepted
- Date: 2026-10-27

## Context

The first design handoff kept the horizontal top bar. The target references (`design-images/`) use a persistent sidebar, a cinematic page header and richer cards. The app must stay light: one Go binary, no heavy UI dependencies, virtualized lists, SSE.

## Decisions

1. **Sidebar + topbar shell** (`Shell.svelte`): fixed 248 px sidebar (collapsible to 72 px, remembered in `localStorage`), compact topbar with breadcrumb, live indicator and theme toggle. Below 900 px the sidebar becomes an accessible drawer (dialog role, focus trap, Escape closes). Only existing routes are linked; no fake destinations.
2. **Tokens stay in `tokens.css`** (`--atlas-*`, with the old `--color-*` aliases). Primary button fill (`--atlas-primary`) is separate from the link colour so white button text keeps AA contrast in dark mode.
3. **No icon or font dependency.** Icons are inline SVG paths in `icons.ts`; the hero planet is SVG (`HeroArt`), not a bitmap; fonts are the system stack. No WebGL, no animation loops; `prefers-reduced-motion` stays respected.
4. **Card height 140 px, slot 148 px.** Richer cards (type, key, two-line title, two labels, priority, due date, assignee) need more than 88 px. The virtualizer keeps fixed row heights; `--atlas-card-h` and `SLOT` in `Board.svelte` change together. Dynamic measurement was not adopted: it would complicate drag position maths.
5. **Item detail** stays a native `<dialog>` (focus trap, Escape, focus return) but grows to 780 px with a properties column. Fields are associated to one form with the `form` attribute, so layout is free of the form's DOM position. Only changed fields are sent (unchanged behaviour).
6. **Project pages get real data only.** Cards show item counts from the board endpoint; the workflow page edits statuses through the existing endpoints; API tokens use the existing token endpoints (the page was a stub before).

## Consequences

- Adding a sidebar route means adding a link in `Shell.svelte` and a case in `App.svelte`.
- Changing the card height needs `tokens.css`, `Board.svelte` and `docs/design/README.md` updated together.
- Colour for a status comes from its category and position (`statusTone.ts`), so renaming a status does not change its colour.
