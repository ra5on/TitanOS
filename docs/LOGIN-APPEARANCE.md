# Anmeldeseite gestalten

Einstellungen › Darstellung › Abschnitt „Anmeldeseite“. Die übrige Darstellung gilt je Konto; die Anmeldeseite ist **eine Einstellung für das ganze NAS**. Nur Besitzer und Administratoren sehen und ändern den Abschnitt.

## Optionen

Alle Optionen sind freiwillig. Ohne gespeicherte Einstellungen (oder mit „Standard“) sieht die Seite genau so aus wie bisher.

- **Theme:** „Wie Standard“ (bisheriges Verhalten: die zuletzt in diesem Browser benutzte Darstellung, sonst Graphit) oder fest Graphit, Schwarz, Mitternacht, Hell, Automatisch. Mit festem Theme zusätzlich Akzentfarbe (Vorgaben oder eigene) und Stil der Karte (Flach, Glas, Kontur); Glas-Transparenz und -Unschärfe werden wie überall begrenzt.
- **Hintergrund:** keiner (Hintergrund des Themes), eines der zehn Galerie-Motive oder ein eigenes Bild; Füllen/Einpassen, Zoom, Position und Abdunkeln wie beim Dashboard.
- **Karte:** Position Mitte, links oder rechts (auf Smartphones bis 760 px immer mittig), Breite normal oder breit.
- **Texte:** Überschrift (Standard „Anmelden“, höchstens 60 Zeichen) und eine Zeile Begrüßung/Hinweis (höchstens 300 Zeichen), je Deutsch und Englisch; fehlt eine Sprache, gilt die andere. Nur Text: kein HTML, kein Markdown, keine Zeilenumbrüche; angezeigt wird immer als Text.
- **Anzeigen:** Namen des NAS (an), Logo (an), Uhrzeit und Datum (aus). Ist der Name ausgeschaltet, nennt auch die öffentliche Antwort von `/api/v1/session` ihn vor der Anmeldung nicht mehr.

Der Abschnitt zeigt eine verkleinerte Vorschau der echten Anmeldeseite. „Vorschau in neuem Tab“ öffnet `/login-preview`: dieselbe Seite mit den ungespeicherten Werten, ohne Abmeldung (nur Besitzer/Administratoren, das Formular schickt nichts). Liegt der Text der Karte auf dem gewählten Hintergrund unter 4,5:1 Kontrast (geschätzt aus der mittleren Bildfarbe nach dem Abdunkeln und der Deckkraft der Karte), warnt der Editor. Wer im System weniger Transparenz eingestellt hat (`prefers-reduced-transparency`), sieht eine deckende Karte.

Die Einstellungen gelten für die Anmeldung samt Zwei-Faktor-Schritt und für alle Seiten im gleichen Rahmen (`AuthCard`), auch die Ersteinrichtung; dort erscheinen Theme und Hintergrund, nicht die Texte der Anmeldung. Auf einem frischen Stick gibt es keine Einstellungen, die Ersteinrichtung sieht also aus wie bisher.

## Speicherung

- `/config/system/login-appearance.json` – neue Datei, ältere titand ignorieren sie.
- Eigenes Bild daneben als `/config/system/login-background-<sha256>`. JPEG, PNG oder WebP bis 5 MiB; titand dekodiert es, verkleinert es auf höchstens 3840 px Kantenlänge und speichert es neu (JPEG, mit Transparenz PNG) – Metadaten wie Ort oder Kamera fallen weg. Das alte Bild wird nach dem erfolgreichen Speichern entfernt, ebenso wenn kein eigenes Bild mehr gewählt ist.
- Geschrieben wird nur beim ausdrücklichen Speichern (atomar). titand liest Datei und Bild beim ersten Bedarf einmal und hält sie danach im RAM.

## Schnittstelle

| Route | Zugriff | Inhalt |
|---|---|---|
| `GET /api/v1/login-appearance` | ohne Anmeldung | nur die Anzeigewerte und `configured`; keine Version, keine Konten, keine Netzwerkdaten |
| `GET /api/v1/login-appearance/background/{id}` | ohne Anmeldung | nur das konfigurierte Bild (`id` = SHA-256), `Cache-Control: public, immutable`, `nosniff`; alles andere 404 |
| `PUT /api/v1/login-appearance` | Besitzer/Administrator | JSON oder multipart (`settings`, `image`); unbekannte Felder und ungültige Werte → 400, `removeImage` entfernt das Bild |
| `DELETE /api/v1/login-appearance` | Besitzer/Administrator | zurück zum Standard, Bild wird entfernt |

Jede Prüfung steht in `backend/internal/api/login_appearance.go`; die Weboberfläche (`web/src/lib/login/look.ts`) prüft dieselben Regeln noch einmal, bevor sie etwas anzeigt. Externe Adressen gibt es nicht: Hintergründe sind ausschließlich die mitgelieferten Motive oder das eine gespeicherte Bild. Änderungen stehen im Aktivitätsprotokoll („Anmeldeseite geändert“).

## Prüfungen

- Go: `login_appearance_test.go` (Prüfung aller Werte, öffentliche Felder, Rechte, Bildformate/-größe, Neukodierung, nur das konfigurierte Bild).
- Web: `web/tests/login-look.test.mjs` (Bereinigung, Rückfall der Texte, Kontrast).
- Browser: `scripts/test-ui-login-appearance.mjs` mit isolierten Daten (Editor, Vorschau-Tab, abgemeldete Seite, Standard unverändert, Hell, 375 px). `prefers-reduced-transparency` ist nur über die bestehende CSS-Regel abgedeckt, nicht im Browser emuliert.
