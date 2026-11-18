# Leistung 1.13: Kontakte, Kalender, iPhone-Abgleich, Mail (Backend)

Stand 18.11.2026, Zweig `feat/v1.13-perf` (Schritt 7 und 5 des Baustands `Entwuerfe/113-pim/BAUSTAND.md`, Backend-Teil). Anlass: der Inhaber empfindet Kontakte und Kalender als „sehr träge“ (Öffnen, Geburtstage, iPhone-Abgleich) und Mail als zäh, solange eine Suche oder der Ordnerstatus läuft. Dieses Dokument hält fest, was gemessen wurde, wie, was die Messung zeigt und was sich geändert hat. Die Oberfläche (Wasserfall beim Start, `fillClipped`, WeekView) misst und ändert ein anderer Schritt.

## Methode

- **Daten** (`backend/internal/pim/pimtest`): 2000 Kontakte als vCard 3.0 wie vom iPhone (Name, Firma, zwei E-Mail-Adressen, zwei Nummern, Adresse; neun von zehn mit Geburtstag, davon jeder zehnte ohne Jahr `1604`; jeder zweite mit Foto, JPEG 320×320 px, ~25 KB in der Karte). Fünf Kalender mit zusammen 3000 Terminen in `Europe/Berlin`, davon 200 Serien (wöchentlich, täglich mit `COUNT`, monatlich „2. Dienstag“, jährlich; jede achte mit `EXDATE` und einer verschobenen Ausnahme), Einzeltermine ein Jahr vor und nach dem 18.11.2026, jeder hundertste mit einem Titel über der Indexgrenze (`clipped`), jeder fünfte mit Erinnerung. Angelegt mit `Store.Import`.
- **Messungen** hinter `TITANOS_PERF=1` (sonst übersprungen):
  - `go test ./internal/pim/store -run TestPerfStore -v` – Store direkt.
  - `go test ./internal/api -run TestPerfPIMRoutes -v` – Web-Routen über HTTP auf Loopback mit Sitzung, ganzer Handlerkette, JSON.
  - `go test ./internal/davsync -run TestPerfDAV -v` – was ein iPhone fragt, über TLS mit Gerätepasswort.
  - `go test ./internal/tmail -run TestPerfMailLanes -v` – Mail gegen den IMAP-Testserver (`tmailtest`).
- **Zeit:** `testing.Benchmark` fünfmal, angegeben ist der Median (`pimtest.Bench`). **Blockieren:** eine Anfrage läuft im Hintergrund immer wieder, gemessen wird eine zweite (Median und Maximum aus 6–10 Proben, `pimtest.Blocked`).
- **Langsame Platte:** der `Volume`-Haken der Tests schläft vor jedem Öffnen des Volumes (`pimtest.Delay`; 200 ms für „Platte läuft an“, 5 ms für „langsame Platte“). So wartet jeder Lese- und Schreibzugriff, der das Volume öffnet; Antworten nur aus dem RAM warten nie. Mail: `tmailtest` verzögert jedes `SEARCH` um 300 ms bzw. jedes `STATUS` um 100 ms und kann Verbindungen über einer Grenze abweisen (`BYE` beim Gruß oder `NO [LIMIT]` bei der Anmeldung).
- **Maschine:** Entwicklungsrechner (Intel Core Ultra 5 225, 6 Kerne, 9 GB RAM), Testdaten auf tmpfs. Zwei weitere Agenten bauten gleichzeitig; Einzelwerte schwanken um ±20 %, nur die Größenordnungen sind belastbar.

## Ergebnisse vorher → nachher

### Was blockiert (die eigentliche Trägheit)

| Messung | vorher | nachher |
|---|---|---|
| Kontaktliste (`Objects`) während ein `Get` auf die Platte wartet (200 ms) | 395 ms | 1,5 ms |
| ctag (`GetCollection`) während ein `Get` wartet | 394 ms | 0,01 ms |
| Kontaktliste während ein Foto auf die Platte wartet | 395 ms | 1,9 ms |
| `GET /contacts` während ein Foto wartet (HTTP) | 196 ms | 5,7 ms |
| `GET /contacts` während ein Kontakt geöffnet wird (HTTP) | 195 ms | 3,6 ms |
| `GET /calendar/events` Woche (mit Geburtstagen) während ein Kontakt wartet | 195 ms | 3,3 ms |
| iPhone: `PROPFIND` getctag während ein Multiget wartet | 382 ms | 0,35 ms |
| iPhone: `PROPFIND` Depth 1 (2000 Karten) während ein Multiget wartet | 394 ms | 6,0 ms |
| iPhone: Multiget von 200 Karten mit Daten bei 5 ms je Öffnen | 1103 ms | 33 ms |
| Mail öffnen, während die Ordnerzähler laufen (13 Ordner × 100 ms) | 1303 ms | 0,4 ms |
| Mail öffnen, während die Suche über alle Ordner läuft (300 ms je SEARCH) | 294 ms | 0,5 ms |
| Liste eines Ordners, während die Suche läuft | 598 ms | 304 ms¹ |
| Kontaktliste, während ein **Schreiben** auf die Platte wartet | 199 ms | 202 ms² |

¹ Die Liste macht selbst ein `SEARCH`, das der Testserver ebenfalls 300 ms verzögert (allein: 304 ms); sie wartet nicht mehr auf die Suche.
² Bewusst nicht geändert, siehe Grenzen.

### Antwortzeiten ohne Wartezeit der Platte

| Route / Aufruf | vorher | nachher |
|---|---|---|
| `GET /contacts` (2000) | 5,0 ms | 3,4 ms (gzip eingerechnet) |
| `GET /contacts?q=…` | 4,6 ms | 0,36 ms |
| `GET /contacts` mit `If-None-Match` (unverändert) | – | 0,03 ms (304) |
| `GET /contacts/books` | 0,87 ms | 0,03 ms |
| `GET /calendar/calendars` | 1,0 ms | 0,04 ms |
| `GET /calendar/events` Monat (6 Wochen) + Geburtstage | 10,8 ms | 13,6 ms³ |
| … nur Geburtstage | 1,5 ms | 1,5 ms |
| … Woche + Geburtstage | 4,4 ms | 2,9 ms |
| … Monat mit `If-None-Match` (unverändert) | – | 0,04 ms (304) |
| … Monat mit `full=1` | – | 13,9 ms |
| Foto, gespeichertes JPEG | 0,15 ms | 0,15 ms |
| Foto mit `If-None-Match` | 0,15 ms (200, Datei gelesen) | 0,03 ms (304, ohne Platte) |
| Store `Get` je Karte (200 in Folge) | 37 µs | 31 µs |
| iPhone Multiget 200 Karten mit Daten | 33,8 ms | 26,7 ms |
| iPhone Multiget 200 Termine mit Daten | 4,0 ms | 2,3 ms |
| iPhone `PROPFIND` getctag / `sync-collection` unverändert | 55 / 71 µs, 0 Volumezugriffe | 58 / 60 µs, 0 Volumezugriffe |
| Ordnerliste mit Zählern, Postfach unverändert | 0,31 ms (immer beim Anbieter) | 0,002 ms (15 s gemerkt) |

³ Teurer durch die Kompression (gzip, schnellste Stufe, ~3 ms CPU auf dieser Maschine), dafür gehen statt 941 KB nur 73 KB über die Leitung. Auf dem LAN ist das gleich, über Tailscale oder am Handy deutlich schneller.

### Antwortgrößen

| Antwort | JSON | übertragen (gzip) |
|---|---|---|
| `GET /contacts` (2000) | 398 KB | 42 KB |
| `GET /calendar/events` Monat (1746 Termine, 203 Geburtstage) | 941 KB | 73 KB |
| `GET /calendar/events` Woche | 143 KB | 11 KB |
| iPhone `PROPFIND` Depth 1 Buch / `sync-collection` voll / Multiget 200 Karten | 578 / 457 / 4637 KB | unverändert (davsync komprimiert nicht) |

## Ursachen und Änderungen

1. **Lesen unter der Sammlungssperre** (`pim/store/read.go`): `Get` und `Photo` lasen die Datei, während `c.mu` gehalten war; dieselbe Sperre brauchen Liste, ctag, `Stat`, `sync-collection`, Vorschläge und Geburtstage. Eine Platte, die anläuft, hielt also alles an. Jetzt: unter der Sperre nur ETag und Zeit kopieren, Datei außerhalb lesen, danach prüfen (`Reader`): passen die Bytes zum Index, fertig; hat ein Schreiben inzwischen den Index geändert und passen die Bytes zum neuen Stand, kommen diese mit ihrem ETag; sonst erneut (bis dreimal), ein Löschen dazwischen ist 404, eine Datei, die sich außerhalb von titand geändert hat, zählt wie bisher mit ihrem eigenen ETag. Nie Bytes einer Fassung mit dem ETag einer anderen (`TestGetWhileWriting` mit `-race`, `TestGetOvertakenByAWrite`, `TestReadsDoNotHoldTheLock`, `TestPhotoFollowsItsCard`). `PhotoTag` nennt den ETag der Karte, zu der das Bild gehört.
2. **Multiget öffnete je Eintrag das Volume** (Volume-Haken + drei `OpenRoot`): `Reader` öffnet den Ordner einmal je Anfrage (davsync `streamMembers`, Exporte `.ics`/`.vcf`).
3. **Foto ohne 304:** `If-None-Match` mit dem ETag der Karte → `304` aus dem RAM (`Stat`), ohne Platte. Mit `?v=<ETag>` (so baut die Seite die Adresse) `private, max-age=604800, immutable`, sonst wie bisher `private, no-cache`. Die Absenderbilder von Mail laden über dieselbe Route und bekommen damit ebenfalls 304.
4. **Geburtstage und Kontaktliste je Anfrage neu:** jede Anfrage kopierte den Index (2000 Einträge mit Listen), sortierte und faltete ihn (NFD je Vergleich, `cmp.Or` wertet alle Argumente aus). Jetzt hält `pim_cache.go` je Konto das vorbereitete Adressbuch (`cardweb.ListIndex` mit sortierten Zeilen und gefalteten Suchtexten, `calweb.BirthdayIndex`) unter dem ctag des Buchs und dem Volume; Einträge und ctag stammen aus einem Augenblick (`Store.ObjectsTag`). Jede Änderung – Web, CardDAV vom iPhone, Agent, Import – erhöht den ctag und macht den Eintrag damit von selbst veraltet; kein Schreibpfad muss daran denken. Höchstens 32 Bücher, nur RAM, beim Löschen eines Kontos verworfen.
5. **Keine Live-Aktualisierung:** neues `GET /api/v1/calendar/stream` (siehe `docs/KALENDER.md` „Live-Aktualisierung (1.13)“), gespeist von `Store.OnChanged`.
6. **Kontaktliste und Termine ohne Bedingung:** beide Antworten tragen jetzt einen ETag (ctags, Suche/Fenster, Woche, Zone); mit `If-None-Match` antworten sie `304` ohne Körper und ohne Abfrage. Die Körper bleiben `no-store` (der Browser legt keine Kopie an); die Seite muss den ETag selbst mitschicken und ihre Liste behalten.
7. **Gekürzte Termine einzeln nachgeladen** (bis zu 40 Anfragen der Seite): `GET /calendar/events?full=1` liefert Titel und Ort ganz (höchstens 200 Vorkommen, jede Datei einmal gelesen).
8. **Große Antworten:** `GET /contacts` und `GET /calendar/events` ab 32 KB mit gzip, wenn der Browser es annimmt.
9. **Mail: eine Verbindung je Postfach:** Ordnerzähler (STATUS) und Suche über alle Ordner laufen auf einer zweiten Verbindung (`tmail/lanes.go`), mit IDLE höchstens drei je Postfach. Die zweite wird nur neben einer funktionierenden ersten versucht; lehnt der Anbieter ab (`BYE` oder `NO` bei der Anmeldung, wie iCloud/Gmail bei zu vielen Verbindungen), läuft die Arbeit 30 Minuten lang wieder über die eine Verbindung, und die Ablehnung gilt nie als falsches Passwort (`TestLanesFallBackToOneConnection`). Ohne `LIST-STATUS` gehen alle `STATUS` auf einmal hinaus (eine Rundreise statt einer je Ordner); die Ordnerliste mit Zählern bleibt 15 s gemerkt, solange im Postfach nichts geschah (kein Befehl der Seite, kein Ereignis; Einstellungen im Fingerabdruck; Listen über 256 KB nie).
10. **iPhone, unverändert gut:** ein unveränderter ctag, das Home-Set und ein unveränderter Sync-Token öffnen das Volume nicht (Zähler `opens` bleibt 0, `docs/KALENDER.md` „Ruhemodus der Platten“ stimmt), und das in ~60 µs.

## Grenzen

- **Schreiben hält die Sammlung weiter an.** `Put`, `DeleteObject` und `Import` halten `c.mu` über Datei, `fsync` und `state.json` (bei 2000 Kontakten ~3 ms je Karte auf SSD); das Öffnen des Kontoordners hält zusätzlich die Kontosperre. Läuft die Platte erst an, warten Liste und ctag so lange wie das Schreiben (Messung: 200 ms bei 200 ms Verzögerung). Die Lösung wäre eine eigene Schreibsperre je Sammlung, `state.json` außerhalb von `c.mu`, und das Vorbereiten des Ordners ohne Kontosperre – ein Umbau der Sperrreihenfolge des Stores, den 1.13 nicht mehr sicher abdeckt. Vorschlag für eine Folgeversion.
- Die langsame Platte ist eine Pause beim **Öffnen des Volumes** im Test, kein echtes Anlaufen einer Festplatte; im Betrieb kann der erste Lese- oder Schreibzugriff länger sein, die Wirkung auf die Sperren ist dieselbe. Auf dem NAS selbst nicht gemessen.
- Der IMAP-Testserver arbeitet die Befehle einer Verbindung nacheinander ab; den Gewinn des Pipelinings (Rundreisezeit zum Anbieter) zeigt die Messung daher nicht. Echte Anbieter (iCloud, Gmail, Web.de) und ihre Verbindungsgrenzen wurden nicht getestet; das Verhalten bei Ablehnung ist nur mit dem Testserver geprüft.
- Netz (LAN/Tailscale/Handy) ist nicht simuliert; die Größen sind angegeben.
- Die Oberfläche nutzt ETag, `full=1` und den Strom noch nicht; die Zahlen „nachher“ sind die der Routen. Was die Seite davon hat, misst der Schritt der Oberfläche.
- `TestCalendarImportZoneAmplification` (api) und `TestParallelCommandsOfAnAccountBounded` (tmail) messen den Heap und schlagen fehl, wenn `go test` beide Pakete gleichzeitig laufen lässt (held 15–17 MiB, peak 178–187 MiB). Das tut die Basis `ea88176a` unter denselben Bedingungen genauso; jedes Paket für sich ist grün (tmail dreimal wiederholt).
