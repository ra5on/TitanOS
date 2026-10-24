# TitanOS – Logo

Eigene Buchstabenformen (keine Schriftart), daher frei von Fremdlizenzen. Gehört zum Projekt und fällt unter dessen Lizenz.

| Datei | Verwendung |
|---|---|
| `titanos-mark-dark-bg.svg` / `-light-bg.svg` | Symbol: Sechseck mit „T“ |
| `titanos-wordmark-dark-bg.svg` / `-light-bg.svg` | Schriftzug „TITANOS“ |
| `download-stable.svg` | Download-Button der Projekt-README, verlinkt auf das neueste Stable-Release |
| `favicon.svg` | Browser-Tab, App-Symbol (unter 24 px nur das „T“) |

Regeln:
- Rahmen des Sechsecks und „OS“ im Schriftzug = Akzentfarbe des Nutzers (`--accent`). Buchstaben = `--text`.
- Im hellen Theme den Akzent abdunkeln, bis der Kontrast zum Hintergrund mindestens 3:1 ist.
- Die Dateien hier nutzen Blau als Standardakzent. Im Produkt das Logo als Svelte-Komponente mit CSS-Variablen einbauen, nicht als Bilddatei.
- Schutzraum: rundum mindestens eine Strichstärke frei. Mindesthöhe Schriftzug 12 px.
