# Fonts, iconen en licenties

## Standaard (geleverd, geen nieuwe dependencies)

- **Font:** CSS system font stack `system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`. Geen downloads, geen nieuwe npm-/CSS-dependency, geen externe fontbestanden; render kan op besturingssystemen licht verschillen.
- **Monospace:** `ui-monospace, "SFMono-Regular", Consolas, monospace` voor werkitemkeys.
- **Logo:** `assets/atlas-mark.svg` is een origineel in deze designhandoff gemaakt Atlas-concept (blauwe hoek-A met oranje omloop). Geen externe package. Gebruik het pas als definitieve brand asset na goedkeuring.
- **UI-iconen:** gebruik de iconen die **nu al** in de app aanwezig zijn. De HTML-demo gebruikt kleine Unicode-symbolen als *visuele placeholders*. Niet als toegankelijk, definitief iconensysteem in productie.

## Optioneel, uitsluitend na bewuste keuze en bestaande dependency-check

1. **Inter** — https://github.com/rsms/inter — SIL Open Font License 1.1. Niet toegevoegd aan dit pakket en geen fontbestanden meegeleverd. Als je Inter kiest, self-host alleen fonts die je licentiebeheer expliciet toestaat.
2. **Lucide** — https://github.com/lucide-icons/lucide — hoofdzakelijk ISC, sommige afgeleide Feather-iconen onder MIT. Niet geïnstalleerd; licentieteksten meeleveren als je SVG's distribueert.

**Geen nieuwe dependency zonder akkoord.** Als er reeds een iconenpakket is, behoud dat; de CSS specificeert alleen afmetingen en kleuren.
