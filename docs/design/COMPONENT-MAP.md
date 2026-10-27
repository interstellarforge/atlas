# Atlas UI — component CSS mapping

Deze selectorwaarden zijn het designcontract. **De bestaande app gebruikt mogelijk andere classnames**; breng ze in de Svelte-componenten samen of map selectors zonder event handlers, pointer-targets of keyboardlogica te wijzigen.

## Primitives

| Bestaande component | Designklasse | States/notes |
|---|---|---|
| Button | `.atlas-btn` | `--primary`, `--secondary`, `--link`, `--danger`, `:disabled`, `:focus-visible` |
| Icon-only button | `.atlas-icon-btn` | Als modifier naast `.atlas-btn` |
| Input / Select / Textarea / Date | `.atlas-input`, `.atlas-select`, `.atlas-textarea`, `.atlas-date` | `aria-invalid="true"`, `:focus-visible`, `::placeholder` |
| Checkbox | `.atlas-checkbox` | system accent color, unchanged semantics |
| Chip | `.atlas-chip` | `--type`, `--info`, `--success`, `--warn`, `--danger` |
| Initials avatar | `.atlas-avatar` | géén fotoavatar |
| Banner | `.atlas-banner` | `--error`, `--success`, `--conflict` (changed elsewhere) |
| Empty state | `.atlas-empty` | text + action |
| Live indicator | `.atlas-live` | `--reconnecting` |

## Shell en overzicht

| Onderdeel | Class(es) / vorm |
|---|---|
| App root | `.atlas-root` en `[data-theme="light|dark"]` |
| Topbar | `.atlas-topbar`, `.atlas-brand`, `.atlas-topnav`, `.atlas-topbar-end`, `.atlas-live` |
| Mobiel navigatie-overflow | `.atlas-overflow-menu`, `.atlas-overflow-list` (gebruik bestaande navigatieroutes) |
| Projectkaart overzicht | `.atlas-project-grid`, `.atlas-project-card`, `.atlas-project-card-top`, `.atlas-project-card-bottom` |
| Nieuw-project-formulier | `.atlas-project-form`, `.atlas-field`, `.atlas-field--full` |
| 404 | `.atlas-notfound` |
| API-token empty state | `.atlas-empty` |
| Login | `.atlas-auth`, `.atlas-auth-card`, `.atlas-auth-header`, `.atlas-auth-form` |
| Projectkop | `.atlas-hero`, `.atlas-project-head`, `.atlas-tabs`, `.atlas-tab[aria-current=page]` |
| Filterbar | `.atlas-filters`, `.atlas-mobile-filters` (`details` demo, bestaand mobile-toggle mechanisme is leidend) |
| Kaartvelden tonen/verbergen | `.atlas-field-toggles` |
| Item toevoegen formulier | Zelfde `.atlas-field`, `.atlas-input`, `.atlas-select` en `.atlas-btn` |

## Board

| Staat / onderdeel | Selector | Hoogte |
|---|---|---:|
| Horizontale scroll | `.atlas-board-scroll` | auto |
| Board | `.atlas-board` | auto |
| Kolom | `.atlas-column` | auto |
| Kolomkop | `.atlas-column-head` | **52 px** |
| Teller / WIP | `.atlas-column-count`, `.atlas-wip` | inside header |
| Over WIP | `.atlas-column--over-wip` | idem |
| Lege kolom | `.atlas-empty` binnen `.atlas-column` | auto |
| Load more | `.atlas-load-more` | 38 px |
| Kaart default | `.atlas-card` | **88 px** |
| Focus | `.atlas-card:focus-visible` | **88 px** |
| Drag source | `.atlas-card--drag-source` | **88 px** |
| Drag preview | `.atlas-card--drag-preview` | **88 px** |
| Blocked | `.atlas-card--blocked` | **88 px** |
| Archived | `.atlas-card--archived` | **88 px** |
| Overdue | `.atlas-due--late` | child in card |
| Drop placeholder | `.atlas-drop-indicator` | 4 px line |
| Drag handle | `.atlas-drag-handle` | *houd bestaande drag listeners en touch-action behavior* |
| Archive / restore | `.atlas-card-action` | 24×24 minimaal in demo; behoud 44×44 touch-target waar dat nu bestaat |
| Cross-project categories | `.atlas-cross-board`, `.atlas-cross-column` | 3 columns desktop, horizontal mobile; **read-only** |

**Belangrijk voor toegankelijkheid:** de kleinste actie-hitbox in de demo is 24×24 px. Als je huidige hit-target 44×44 px heeft, behoud dat via een onzichtbaar grotere hitbox of aangepaste padding; verander niet stilzwijgend de interactiezone.

## Andere views

| Onderdeel | Selector | Hoogte |
|---|---|---:|
| List sortable header | `.atlas-table-head`, `.atlas-sort-active` | 44 px |
| List row | `.atlas-table-row` | **48 px** |
| Blocked list row | `.atlas-table-row--blocked` | **48 px** |
| Archived list row | `.atlas-table-row--archived` | **48 px** |
| Epic grouping | `.atlas-group-head` | 48 px |
| Backlog row | `.atlas-backlog-row` | **44 px** |
| Hierarchy row | `.atlas-tree-row[data-depth="0..3"]` | **44 px** |
| Expand / collapse | `.atlas-tree-toggle` | 26 px control height in row |

## Detailpaneel

| Onderdeel | Selector |
|---|---|
| Backdrop | `.atlas-backdrop` |
| Paneel desktop rechts | `.atlas-detail` (`--atlas-panel-w: 500px`) |
| Paneel mobiel | `.atlas-detail` full-screen bij max-width:600px |
| Header badges/sluiten | `.atlas-detail-header`, `.atlas-detail-header-top` |
| Scrollbaar formulier | `.atlas-detail-content`, `.atlas-panel-section`, `.atlas-detail-fields`, `.atlas-field` |
| Custom fields | zelfde `.atlas-panel-section` en primitives |
| Linklijst | `.atlas-link-row` met richting, itemkey, verwijderknop |
| Actiebalk | `.atlas-detail-footer` met Archive, Discard en Save |

Het paneel demonstreert conflictstatus, maar verandert geen API-versies, lock errors of wijzigingen elders. Details van echte state komen uit je bestaande app.

> **Achterhaald door epic UIX:** de sidebar, notificaties (inbox) en de detailpaneel-indeling bestaan nu; zie `README.md` (*UIX-herbouw*). Fotoavatars, comments-in-detail-als-activiteitenfeed en globale zoek blijven buiten scope.

## Nieuwe componenten — expliciete beslissing

| Nieuw element uit screenshots van eerder | In deze restyling? | Reden |
|---|---|---|
| Sidebar | **Nee** | Bestaat niet; topbalk blijft leidend |
| Fotoavatars | **Nee** | Initialen blijven behouden |
| Comments in detail | **Nee** | Nieuw datamodel/API/permissions nodig |
| Notificaties | **Nee** | Nieuw productonderdeel |
| Global search topbar | **Nee** | Bestaat niet; filterzoekveld blijft |

Later toevoegen via aparte epics, niet als verborgen CSS-change.
