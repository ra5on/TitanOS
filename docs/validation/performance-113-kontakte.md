## Kontakte (Oberfläche)

Stand 18.11.2026, Zweig `feat/v1.13-contacts` (Schritt 6 des Baustands, Frontend-Teil; Abschnitt für `docs/validation/performance-113.md`, dort vom Koordinator einzufügen). Gemessen mit `scripts/measure-ui-kontakte.mjs`: Kontakte bei 1440 px mit **2011 Kontakten** öffnen (die elf der Entwürfe plus 2000 erzeugte, jeder fünfte mit Bild), „beisp“ tippen (eine Taste alle 60 ms), die Suche leeren, einen Kontakt öffnen, seine Notiz ändern und speichern. Isolierte Daten (`test-ui-kontakte-fixtures.mjs`, die Antworten kommen aus dem Browser-Test, kein NAS), gebaute Oberfläche in `vite preview`. **Vorher** = Oberfläche von `feat/v1.13` (`def6eb37`, Kontakte wie 1.11 mit der Karte), **nachher** = `feat/v1.13-contacts`. Je drei Läufe abwechselnd, angegeben der Median. Maschine wie oben, zwei weitere Agenten bauten gleichzeitig: Zeiten schwanken um ±20 %, die Anfragen sind exakt.

### Anfragen

| Schritt | vorher | nachher |
|---|---|---|
| Öffnen | `GET /contacts`, `GET /contacts/suggestions` (alle Vorschläge, auch ungezeigt), `GET /calendar/access`, 13 Bilder | `GET /contacts` (mit ETag), `GET /contacts/overview` (Adressbücher, Gruppen, Zahl der Dubletten), `GET /calendar/access`¹, 13 Bilder² |
| Suche „beisp“ | 2: `GET /contacts?q=`, `GET /contacts/suggestions?q=` | 1: `GET /contacts/search?q=` |
| Kontakt öffnen | 1: `GET /contacts/kontakte/{id}` | 1: unverändert |
| Speichern | 4: `PUT`, dann die ganze Liste, alle Vorschläge, der Kontakt | 2: `PUT` (Antwort mit der neuen Zeile und dem ETag der Liste), der Kontakt |
| Live-Ereignis der eigenen Änderung | – (gab es nicht) | keine Anfrage (das Ereignis nennt den Stand, den die Seite schon kennt); nach einem Wiederaufbau des Stroms (`resync`) `GET /contacts` → **304** ohne Körper |

¹ Nur weil die Testdaten Geburtstage dieser Woche haben (die Zeitzone des NAS für „heute“/„am Freitag“); ohne solche entfällt die Anfrage. „Termin“ fragt sie erst beim Klick.
² Die Bilder der Liste tragen jetzt `?v=<ETag der Karte>`; titand antwortet dann `private, max-age=604800, immutable`, beim nächsten Öffnen kommen sie ohne Anfrage aus dem Browser (nicht gemessen, folgt aus den Kopfzeilen von wt-113b).

### Zeiten (Median aus drei Läufen, ms)

| Messung | vorher | nachher |
|---|---|---|
| Öffnen bis zur ersten Zeile (Wandzeit) | 464 | 403 |
| Suche bis zur ersten Markierung (5 Tasten, 200 ms Pause eingeschlossen) | 614 | 595 |
| 20 Bildlauf-Schritte bis ans Ende der Liste (je ein Frame) | 609 | 424 |
| Speichern bis zur Ansicht des Kontakts | 879 | 914 |
| Chromium: Layout beim Öffnen (`LayoutDuration`) | 78 | 32 |
| Chromium: Stil beim Öffnen (`RecalcStyleDuration`) | 38 | 10 |
| Chromium: Skript beim Öffnen (`ScriptDuration`) | 144 | 149 |
| Chromium: Layout + Stil beim Bildlauf | 40 + 0 | 40 + 30 |

### Was sich geändert hat und warum

- **Liste ohne Fensterung, aber nur gezeichnet, was zu sehen ist:** jeder Buchstabe ist ein Block mit `content-visibility: auto` und `contain-intrinsic-size: auto <Zeilen × 54 px>`; Zeilen mit variabler Höhe (Firma, Geburtstag, lange Namen umbrochen) behalten ihre gemessene Höhe. Alle 2011 Zeilen bleiben im DOM (Suchen im Browser, die gewählte Zeile per `scrollIntoView`, Ziehen auf eine Gruppe, `cutTextIn` prüft sie mit `content-visibility: visible`). Layout und Stil beim Öffnen fallen auf etwa ein Drittel; beim Bildlauf kostet das Auslegen der hereinkommenden Blöcke Stil-Zeit, die Frames sind trotzdem schneller.
- **Vorschläge aus Mail** nur, wenn ihre Ansicht offen ist; die Seitenleiste zeigt dafür keine Zahl mehr.
- **Eine Anfrage je Suche** (`GET /contacts/search`, Kontakte und Vorschläge zusammen); eine überholte Suche wird abgebrochen (`AbortController`).
- **Nach dem Schreiben nur die Zeile:** jede Schreib-Antwort (`POST`/`PUT` Kontakt, Bild, Vorschlag übernehmen, Zusammenführen) trägt `item` (die neue Zeile) und `listETag`; Gruppen-Antworten `listETag`, `DELETE` die Kopfzeile `X-List-ETag`. Die Seite setzt die Zeile an ihren Platz (`upsertItem`) und merkt sich den ETag – das Live-Ereignis der eigenen Änderung kostet dann keine Anfrage, ein `resync` liest die Liste als 304.
- **Live:** `watchPim` (wt-113b) liest bei einem Ereignis `pim`/`contacts` oder `resync` die Liste (mit `If-None-Match`, unverändert 304) und die Seitenleiste; ein offener Kontakt wird nur neu gelesen, wenn sich das ETag seiner Zeile geändert hat und er nicht bearbeitet wird.
- **Ein gespeicherter Kontakt wird einmal gelesen** (vorher zweimal, weil der Effekt beim Wechsel Bearbeiten → Ansicht ein zweites Mal startete).

### Grenzen

- Gemessen mit Testdaten im Browser, nicht gegen ein NAS: die Zeiten enthalten keine Netz- und Serverzeit; die Serverseite misst `performance-113.md` oben.
- Ein Gerät mit wenig Leistung (iPhone) ist nicht gemessen; `content-visibility` gibt es in Safari seit 18.
- Den ETag eines Schreibens behält die Seite nur, wenn der ctag des Adressbuchs danach genau eins über dem ist, den sie kennt (Antwortfeld `ctag`, Kopfzeile `X-CTag`; bekannt aus `ready` und den Ereignissen des Live-Stroms). Hat ein anderes Gerät dazwischen geschrieben, liest die Seite die Liste beim nächsten Mal ganz. Ohne Live-Strom (kein Volume, Strom getrennt) behält sie keinen ETag aus einem Schreiben.
