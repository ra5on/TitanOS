# Leistung des Kalenders 1.13 (Oberfläche)

Stand 18.11.2026, Zweig `feat/v1.13-calendar`. Gemessen mit `scripts/measure-ui-kalender.mjs` (Playwright, Chromium headless, isolierte Testdaten aus `scripts/test-ui-kalender-fixtures.mjs`, Uhr fest auf Dienstag, 17.11.2026, 12:29 Berlin), vorher auf dem Stand `def6eb37` (Basis `feat/v1.13` mit dem Backend der Leistungsarbeit), nachher auf `3032e172` und noch einmal auf `c1f49351` (Stand der Übergabe; Anfragen und Scrollposition gleich, Zeiten siehe Tabelle). Backend-Messungen (Store, ETag, `full=1`, Strom): `performance-113.md`.

## Methode

- Daten: die Termine der Entwürfe plus 30 Termine dieses Monats mit Titeln über der Grenze des Index (`clipped`), wie sie ein iPhone nach einigen Jahren hat.
- Jede Antwort der Kalender-API wartet **50 ms** (`MEASURE_DELAY`), vorher und nachher gleich – ein NAS antwortet nicht sofort, Wasserfälle werden so sichtbar.
- Zeit bis Raster: vom Beginn der Navigation bis ein Termin im Monat sichtbar ist, Median aus 5 Öffnungen (die erste wärmt den Zwischenspeicher und zählt nicht); „ganze Titel“: bis der letzte gekürzte Titel ganz dasteht.
- Runde (1440 × 900): Monat öffnen, Woche, „Arbeit“ aus- und wieder einblenden, eine Änderung eines anderen Fensters (BroadcastChannel, Terminfenster gespeichert), ein Ereignis des Live-Stroms (iPhone ändert „privat“), nächste Woche, zurück zum Monat. Gezählt werden die Anfragen je Art.
- Layout der Woche: Chrome `Performance.getMetrics` (LayoutCount, RecalcStyleCount, LayoutDuration) beim Öffnen der Woche und beim Ziehen der Fensterbreite in 10 Schritten von 1440 auf 1040 px und zurück; Scrollposition der Stunden nach Neuladen (vorher ganz nach unten gescrollt, 900 → höchstens 518 px).

## Ergebnis

| | vorher | nachher |
|---|---|---|
| Zeit bis Raster sichtbar | 406 ms | 219 ms (`c1f49351`: 279 ms, Maschine parallel belastet) |
| Zeit bis alle Titel ganz | 2212 ms | 226 ms (`c1f49351`: 284 ms) |
| Anfragen der Runde | 84 | 14 (davon 3 Live-Strom) |
| davon Einzelabrufe `GET /calendar/events/{cal}/{obj}` | 70 | 0 |
| Öffnen: Anfragen | 34 (access → calendars → events nacheinander, dann 31 Einzelabrufe) | 4 (access, calendars, events gleichzeitig; Strom) |
| Aus- und Einblenden eines Kalenders | 14 | 3 (2 × PUT, 1 × nur dieser Kalender) |
| Änderung eines anderen Fensters | 7 | 1 (nur ohne Live-Strom; mit Strom liest das Ereignis nur die Sammlung) |
| Live-Ereignis „privat geändert“ | – (nicht ausgewertet) | 1 (`calendars=privat`) |
| Zurück zum Monat | 22 | 1 (`If-None-Match` → `304`) |
| Woche öffnen: Layouts / Stilberechnungen / Layoutzeit | 33 / 37 / 5,7 ms | 4 / 5 / 3,0 ms (`c1f49351`: 4 / 5 / 2,7 ms) |
| Fensterbreite ziehen (20 Schritte): Layouts / Stile / Zeit | 150 / 161 / 40,8 ms | 40 / 46 / 24,4 ms (`c1f49351`: 40 / 40 / 18,4 ms) |
| Scrollposition der Stunden nach Neuladen | springt auf 07:00 (356 px) | bleibt (518 px) |

## Ursachen und was geändert ist

- **N+1 der gekürzten Titel** (`fillClipped`, bis 40 Einzelabrufe, jeder setzte die Liste neu und zeichnete alle Ansichten): ersetzt durch `GET /calendar/events?…&full=1`.
- **Start-Wasserfall** `/calendar/access` → `/calendar/calendars` → `/calendar/events`: alle drei gleichzeitig (`lib/pim/cal/calendar-data.svelte.ts`); das Fenster liegt einen Tag weiter um die gezeigten Tage, eine abweichende NAS-Zone verschiebt höchstens um diesen Tag.
- **ETag/304**: die letzten 16 Antworten mit ETag je Abfrage (`calendar-api.ts loadEvents`, `If-None-Match` über `apiFetch`).
- **Aus-/Einblenden**: lokal sofort; nur der eingeblendete Kalender wird gelesen.
- **Live**: `watchPim` – `kind:"calendar"` liest die betroffene Sammlung, `kind:"contacts"` die Geburtstage, `kind:"tasks"` die Erinnerungen, `resync` alles; Änderungen innerhalb 200 ms einmal. Eine Ansage eines anderen Fensters (BroadcastChannel) wartet 1,2 s, ob der Strom die Sammlung nennt.
- **`Intl.DateTimeFormat` je Aufruf** (`calendar-text.ts fmt`, auch `whenText`, `startText`, `weekTitle`): je Sprache und Optionen einmal gebaut.
- **WeekView `relayout`** (Schreiben/Lesen im Wechsel, ResizeObserver setzte selbst `minHeight`, Sprung auf 07:00 bei jedem Neuladen): Positionen nur aus den Minuten (oben/Höhe, Spuren aus `overlapLanes` mit derselben Mindestlänge), kein Messen; die Stunden scrollen zum Morgen nur beim Öffnen und bei anderen Tagen.

## Grenzen

- Gemessen mit Testdaten im Browser, nicht gegen ein NAS; die 50 ms Antwortzeit sind angenommen. Die Zahl der Anfragen hängt nicht davon ab, die Zeiten schon.
- Die Layoutzahlen stammen aus einem Lauf; sie schwanken um einige Layouts.
- Der Live-Strom im Messlauf ist eine Attrappe (ein Ereignis, danach abgewiesen); der Wiederaufbau nach Verlust (1 s … 30 s) zählt mit („stream“).
