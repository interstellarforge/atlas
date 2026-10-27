# Accessibility audit

Date: 2026-10-27. Scope: the UI as built before the UIX redesign (epic UIX). Repeat after it.

## Method

- Lighthouse accessibility (Chrome DevTools, snapshot mode) on: login, projects, board (dark, desktop), board (light, 375 px) and the item panel over the list (light, 375 px). Score 100 on all five; the only failures were SEO/agentic items that do not apply to a private app (meta description, robots.txt, llms.txt).
- Layout probe at 375 px on projects, board, list, backlog, hierarchy, milestones, inbox, tokens and the item panel: no horizontal page overflow (scrollWidth = 375).
- Target-size probe (WCAG 2.2 SC 2.5.8, 24 px): listed every button, link and input smaller than 24 px.
- Contrast: tokens verified in `docs/design/CONTRAST.md` (light and dark).
- Existing conventions: skip/landmarks via `<nav aria-label>`, live region for the SSE status, `aria-sort` on list headers, `aria-current` on tabs, drag handles are buttons with a label and a keyboard alternative.

## Fixed

- Checkboxes (filters, checklist) 17 px to 24 px.
- List sort buttons and the "All projects" back link now reach 24 px.
- List view lost its column grid in commit `e53c547` (header cells stacked on top of the rows). Restored the grid in `ListView.svelte`.

## Open

- Card drag handle (22x21), card "Archive" link (40x20) and inline key links in list/hierarchy rows (13-15 px tall) stay under 24 px. Cards have a fixed 88 px height for virtualization; UIX story "Werkitemkaart-redesign" resizes them.
- Not done: screen-reader pass (VoiceOver/NVDA), full keyboard walkthrough of every view by a person, forced-colors mode, text zoom to 200 %. Automated tools find roughly a third of accessibility problems; this audit does not prove conformance.
