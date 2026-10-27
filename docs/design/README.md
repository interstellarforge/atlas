# Atlas UI Design Handoff — v1.0

**Doel:** Alleen styles/tokens toepassen op de bestaande Atlas Go/Svelte-app. Geen vervanging van logica, drag-and-drop, keyboardgedrag, SSE, routing of virtualisatie.

## Inhoud (in jouw gevraagde volgorde)

1. `tokens.css` — light/dark variabelen (bron van waarheid)
2. `screenshots/` — 20 echte browserrenders: board, card states, list, detail drawer, login × dark/light × desktop/375px
3. `base.css`, `components.css`, `layouts.css` — CSS + `demo/index.html`, `demo/app.js`, `demo/en.json` als HTML-componentreferentie
4. `FONT-ICONS.md` — géén nieuwe dependency; systeemfonts en eigen SVG-merkicoon

Daarnaast: `COMPONENT-MAP.md`, `CONTRAST.md`, `assets/atlas-mark.svg` en een *optioneel* screenshot-renderscript.

## Gebruik in bestaande repo

Pak het ZIP-bestand uit aan de root van de repo: inhoud staat onder `docs/design/`.

Kopieer vervolgens de CSS naar je bestaande styles-directory of importeer deze CSS in deze volgorde:

```css
@import "./tokens.css";
@import "./base.css";
@import "./components.css";
@import "./layouts.css";
```

1. Plaats `data-theme="dark"` of `data-theme="light"` op de app-root of `<html>`.
2. Plaats `.atlas-root` alleen op Atlas' app-root (of verwerk de selectors in bestaande component-scopes).
3. **Map de bestaande Svelte-componenten op de CSS-classes, verander hun class-structuur niet automatisch.** Zonder huidige componentbroncode kan ik geen veilige 1-op-1 patches leveren.
4. Vervang *geen* user-facing labels: die blijven uit de bestaande `en.json` komen. `demo/en.json` is alleen een zelfstandige referentie voor de mockups.
5. Laat bestaande event handlers en statebeheer intact. `demo/app.js` is nadrukkelijk niet bedoeld voor productie.

## Vastgezette virtualisatiecontracten

| Element | Desktop | Mobiel 375 px | CSS token |
|---|---:|---:|---|
| Kanban-kaart | **88 px** | **88 px** | `--atlas-card-h` |
| Kolomkop | **52 px** | **52 px** | `--atlas-column-header-h` |
| Lijstrij | **48 px** | **48 px** | `--atlas-list-row-h` |
| Backlogrij | **44 px** | **44 px** | `--atlas-backlog-row-h` |
| Hiërarchierij | **44 px** | **44 px** | `--atlas-hierarchy-row-h` |

Er verandert **niets** aan de 88 px-kaartafspraak. De voorbeeldmarkup kapt lange kaarttitels af om die hoogte te waarborgen. Als de applicatie dynamisch lettergroottes of zoom toelaat, moeten itemmeting en virtualisatie met die variatie rekening houden.

## UI-grenzen (restyling vs. nieuwe functionaliteit)

**Alleen restyling:** bestaande topbar, login, projectpagina, werkitemformulier, filter, board, list, backlog, hierarchy, tokens-empty, 404, light/dark en alle fout-/conflicttoestanden.

**Niet meenemen in deze restyling:** nieuwe sidebar, fotoavatars, comments in detail, notificatie-inbox en globale zoekbalk. Deze zijn inhoudelijke uitbreidingen waarvoor aparte tickets, componenten, i18n-keys en tests nodig zijn. Avatars zijn alleen initialen.

Het mobiele overflowmenu in de mockup is uitsluitend een responsive weergave van **bestaande** navigatielinks (API-tokens, Logout), geen nieuw productonderdeel. Koppel dit aan de bestaande navigatie of behoud bestaande mobiele navigatie.

## Designrichting

- **Atlas binnen Interstellarforge:** donker marineblauw, hemelblauw, subtiel oranje sunrise-accent.
- **Werkoppervlak > decor:** alleen de projectkop gebruikt een lichte orbit/starfield-gradient; kaarten en formulieren gebruiken platte, rustige surfaces.
- **Light is volledig uitgewerkt:** geen dark-only of witte tekst op lichte oppervlakken.
- **Geen achtergrondafbeeldingen op runtime:** decoratieve header volledig in CSS, dus geen extra image-requests.
- **Geen icon-/fontdependency** in de CSS. `atlas-mark.svg` is eigen artwork in deze handoff.

## Previewen

```bash
cd docs/design
python3 -m http.server 8000
# Open http://localhost:8000/demo/?view=board&theme=dark
```

Demo views: `board`, `card`, `list`, `detail`, `login`, `backlog`, `hierarchy`, `projects`, `emptyprojects`, `newproject`, `crossboard`, `tokens`, `notfound`. Andere theme: `theme=light`.

Niet naar productie deployen; dit is een visuele referentie. De content in demo is seed/testdata, niet de gebruikersdata van Atlas.

## Wat is geverifieerd?

- Alle 20 screenshots met Chromium gegenereerd, zonder JS-errors.
- Geen documentbrede horizontale overflow bij 375 px; board heeft bewust *interne horizontale scroll*.
- Alle gerenderde Kanban-kaarten exact 88 px in de test.
- Representatieve tekst-/achtergrondkleuren getest op minimaal WCAG AA tekstcontrast; gedetailleerd overzicht in `CONTRAST.md`.
- Dit is geen volledige WCAG-audit of toepassingstest op je echte Svelte-componenten.
