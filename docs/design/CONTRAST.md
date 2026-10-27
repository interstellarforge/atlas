# WCAG AA — eerste contrastcontrole

Automatisch berekend met de WCAG-relatieve-luminantieformule, voor tokensparen in `tokens.css`.
De eis voor normale tekst is minimaal **4.5:1**. Grotere tekst en bepaalde grafische componenten hanteren andere eisen.

| Paar | Light | Dark |
|---|---:|---:|
| Hoofdtekst op surface | 15.60:1 | 16.12:1 |
| Gedempte tekst op surface | 6.99:1 | 10.80:1 |
| Subtiele tekst op surface | 5.50:1 | 9.19:1 |
| Placeholder op surface | 4.71:1 | 8.47:1 |
| Accenttekst op surface | 6.35:1 | 8.65:1 |
| Primary-knoptekst op primary-achtergrond | 6.35:1 | 8.72:1 |
| Accent-soft-on op accent-soft | 6.97:1 | 8.89:1 |
| Danger op danger-soft | 5.57:1 | 6.36:1 |
| Warning op warning-soft | 5.80:1 | 7.95:1 |
| Success op success-soft | 5.43:1 | 7.40:1 |

`focus-visible` gebruikt een 3px solid outline en contrasterende halo; `prefers-reduced-motion` is ondersteund.

**Nog te testen in echte Atlas-app:** iconcontrast, focusring tegen elk relevant oppervlak, placeholder tegen inputbg, werkelijk keyboardgebruik, interaction target >=44×44 waar vereist, zoom tot 200%, drag via toetsenbord, dynamische tekst/language fonts en contrast onder translucente overlays. Resultaten hierboven zijn **geen** volwaardige WCAG-conformiteitsverklaring.
