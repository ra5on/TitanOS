# T-Kalender und Kontakte (1.11)

Zwei eigene Programme in TitanOS: **T-Kalender** (Termine) und **Kontakte** (Adressbuch), je Konto, mit Abgleich zum iPhone über CalDAV und CardDAV. Grundlage sind die abgenommenen Entwürfe `Entwuerfe/108-kalender-kontakte/` und die Entscheidungen des Inhabers vom 17.11.2026 (PLAN „T-Kalender & Kontakte“). Dieses Dokument ist zugleich die Hilfe beider Programme (Menüleiste › Hilfe) und der Vertrag zwischen Backend (`backend/internal/pim/…`, `backend/internal/davsync`, `backend/internal/api/calendar*.go`, `contacts*.go`, `devices*.go`, `pim_data.go`, `register_pim.go`) und Oberfläche (`web/src/routes/calendar/`, `web/src/routes/contacts/`, `web/src/lib/pim/`). Es beschreibt, was gebaut ist.

## Kurz für die Bedienung

- **Was du bekommst:** einen Kalender „Privat“ und ein Adressbuch „Kontakte“ für dein Konto, ab Werk für jedes Konto (Besitzer, Administratoren, Benutzer) im Menü, im Dock und am Handy. Weitere Kalender legst du in T-Kalender an (Name, Farbe); das Adressbuch bleibt eins. Niemand sonst sieht deine Termine und Kontakte, auch kein Administrator.
- **T-Kalender:** Monat, Woche, Tag und Liste; die Woche beginnt am Montag und nennt die Kalenderwoche. „Neuer Termin“ öffnet ein eigenes Fenster (am Handy eine eigene Seite). Termine mit Ort, Notiz, Erinnerungen und Wiederholung („täglich“, „werktags“, „wöchentlich“, „monatlich am 3. Dienstag“ …, alle n Tage/Wochen/Monate/Jahre, Ende nach Datum oder Anzahl). Bei einer Reihe fragt T-Kalender beim Speichern und Löschen „Nur diesen Termin“, „Diesen und alle folgenden“ oder „Die ganze Reihe“. Kalender lassen sich ausblenden (Häkchen in der Spalte, je Konto gemerkt), importieren (`.ics`) und exportieren.
- **Geburtstage:** ein Kalender „Geburtstage“ aus den Kontakten, nur lesen und nur in TitanOS (das iPhone bildet ihn selbst). Ein Geburtstag ohne Jahr steht ohne Alter da; der 29. Februar erscheint in anderen Jahren am 28.
- **Kontakte:** Liste von A bis Z (ä wie a, ß wie ss), Suche über Name, Firma, E-Mail, Adresse und Nummer („0170 12“ findet „+49 170 1234567“), Geburtstage dieser Woche in der Liste. Bearbeiten mit Bild (vom Rechner oder aus den Dateien des NAS), Telefon, E-Mail und Adresse mit Art, Geburtstag mit oder ohne Jahr, Notiz. Was die Seite nicht zeigt (eigene Felder des iPhone, Gruppen), bleibt in der Karte unverändert. Import und Export als `.vcf`.
- **Vorschläge aus T-Mail:** Mit Freigabe von T-Mail zeigt Kontakte unter „Vorschläge aus T-Mail“ Adressen, an die du geschrieben hast und die noch in keinem Kontakt stehen; „Als Kontakt übernehmen“ legt einen an. Umgekehrt schlägt T-Mail beim Schreiben deine Kontakte zuerst vor („· Kontakt“). Die gelernten Adressen von T-Mail gehen nie per CardDAV aufs iPhone und nie an den KI-Agenten.
- **Zeitzone:** T-Kalender zeigt und legt Termine in der Zeitzone des NAS an (Einstellungen › System), nicht in der des Browsers. Ein Termin in einer anderen Zone nennt sie in den Details.
- **Ohne Daten-Volume:** Beide Programme sagen „… ist gerade nicht verfügbar“, nie eine leere Liste. Das iPhone behält seinen Stand.

### Am iPhone einrichten

In T-Kalender oder Kontakte › „Am iPhone einrichten“. Die Seite zeigt Server und Benutzername und legt ein **Gerätepasswort** an (je Gerät eins, nach Kontopasswort und, falls eingeschaltet, dem Code der Zwei-Faktor-Anmeldung). Das Passwort steht **nur einmal** auf der Seite, in sieben Vierergruppen aus Kleinbuchstaben und Ziffern zum Abtippen; TitanOS speichert nur einen Abdruck. Kontopasswort und Zwei-Faktor-Code gehören nie aufs iPhone.

Welche Adresse: Ein Name mit gültigem Zertifikat von Let's Encrypt steht oben, dann Tailscale („empfohlen“, braucht die Tailscale-App am iPhone; mit dem Zertifikat von Tailscale fragt iOS nicht nach), dann die Adresse im Heimnetz („nur zu Hause“; beim selbst signierten Zertifikat fragt iOS einmal, ob es ihm trauen soll), darunter DDNS und NetBird, wenn eingerichtet.

Schritte am iPhone (Wortlaut der Seite; die Menüpfade stammen aus den Entwürfen und sind **am echten iPhone mit iOS 26 noch zu bestätigen**):

1. Einstellungen › Apps › Kalender › Kalender-Accounts › Account hinzufügen › Andere › CalDAV-Account hinzufügen. **Nicht über die Einstellungen von Mail einrichten:** so angelegt, bleibt der Account bei iOS 26 manchmal inaktiv (siehe „iOS“).
2. Server, Benutzername und Passwort von der Seite eintragen, als Beschreibung zum Beispiel „TitanOS“. iOS findet die Kalender selbst.
3. „Kalender“ an, „Erinnerungen“ aus (die Erinnerungslisten von iOS kennt TitanOS nicht), „Sichern“.
4. Einstellungen › Apps › Kontakte › Kontakte-Accounts › Account hinzufügen › Andere › CardDAV-Account hinzufügen, mit denselben Angaben.
5. Standard wählen: Einstellungen › Apps › Kalender › Standardkalender „Privat“ unter „TitanOS“, Einstellungen › Apps › Kontakte › Standardaccount „TitanOS“.

Fragt iOS nach mehr: unter „Erweiterte Einstellungen“ SSL an und den Port der Seite; die vollen Adressen `https://<host>[:port]/caldav/<konto>/` bzw. `/carddav/<konto>/` stehen „für den Notfall“ darunter.

Darunter die Liste der Geräte mit „zuletzt abgeglichen“ (heute, gestern, Datum, „vor dem Neustart“ oder „noch nie“, mit Kalender oder Kontakte) und „Entziehen …“. Ein verlorenes Telefon entziehst du hier; Besitzer und Administratoren können im Programm Benutzer die Geräte eines anderen Kontos sehen und entziehen, aber keine anlegen.

## Vertrag

- **Daten je Konto, privat.** Jede Sammlung gehört genau einem Konto; Web-API und CalDAV/CardDAV prüfen den Besitz serverseitig. Ein fremdes oder unbekanntes Konto, ein fremder Kalender, ein fremder Eintrag ist `404`, nie `403`. Kein Teilen zwischen Konten.
- **Sichtbar ab Werk für jedes Konto**, das angemeldet und nicht gesperrt ist: `/services/state` meldet `calendar` und `contacts` (`pimOn`), die Programme sind `optIn` daran gebunden.
- **iPhone über CalDAV/CardDAV mit Gerätepasswort.** Nur Gerätepasswörter öffnen `/caldav/`, `/carddav/` und `/.well-known/caldav|carddav`; das Kontopasswort, das WebDAV-Passwort und API-Tokens werden dort nie angenommen, und ein Gerätepasswort öffnet nichts anderes (keine Freigabe, keine API, kein `/dav/`, kein `/mcp`).
- **Nicht für Tokens und MCP** (Entscheidung des Inhabers): keine Route in `publicRoutes`, kein Werkzeug im MCP-Katalog. Der eingebaute KI-Agent hat eigene Werkzeuge mit eigenen Rechten (siehe „Agent und Home Assistant“).
- **Ohne Volume nie leer.** Fehlt das Daten-Volume, antwortet jede Datenroute `503` (Web-API `calendar_unavailable`/`contacts_unavailable`, CalDAV/CardDAV mit `Retry-After: 300`), nie eine leere Sammlung und nie `404` auf eine bekannte. Sonst löschte das iPhone seine Kopie.
- **ETag streng.** Jede Änderung braucht das ETag des gelesenen Stands (Web-API Pflichtfeld `etag`, DAV `If-Match`); ein älterer Stand ist ein Konflikt, nie ein stilles Überschreiben.

## Adressen und Anmeldung

- **Pfade** (auf dem Port der Weboberfläche, TLS wie dort):
  - CalDAV: `/caldav/` (Wurzel), `/caldav/<konto>/` (Principal), `/caldav/<konto>/calendars/` (Home-Set), `/caldav/<konto>/calendars/<sammlung>/<eintrag>.ics`.
  - CardDAV: `/carddav/<konto>/`, `/carddav/<konto>/addressbooks/`, `/carddav/<konto>/addressbooks/kontakte/<eintrag>.vcf`.
  - `/.well-known/caldav` und `/.well-known/carddav` leiten mit `301` auf `/caldav/` bzw. `/carddav/` (jede Methode, ohne Anmeldung; RFC 6764).
  - Sammlungen heißen `[a-z0-9-]{1,64}` oder eine UUID (iOS); Einträge 1–200 Byte UTF-8, ohne führenden Punkt, ohne `/`, NUL und Steuerzeichen, Endung `.ics`/`.vcf`. Jedes Segment wird genau einmal dekodiert; `..`, `%2F` und ähnliche Pfade erreichen davsync nicht (`davsync.Path`, `davsyncPath` in `api/register_pim.go`).
- **Anmeldung:** HTTP Basic über TLS, Realm `TitanOS Kalender` (`WWW-Authenticate: Basic realm="TitanOS Kalender", charset="UTF-8"`), damit iOS sie nicht mit `/dav/` verwechselt. Reihenfolge in `davsync.ServeHTTP`: `.well-known` → `OPTIONS` (ohne Anmeldung, antwortet nur nach der Form des Pfades mit `DAV` und `Allow`, ohne Store, ohne Kontoprüfung) → Anmeldung → Pfad (sonst `404`) → Pfadkonto = angemeldetes Konto (sonst `404`).
  - Ohne `Authorization`: `401` mit Challenge, nicht gezählt. `Bearer …` gilt als keine Anmeldung (`401`, nicht gezählt).
  - Unbekanntes Konto, falsches Passwort, Passwort über 128 Zeichen, gesperrtes Konto: dieselbe Antwort `401`, gezählt, Journal mit Name (auf 64 Byte gekürzt) und Adresse, nie mit Passwort. Gleiche Arbeit für bekannte und unbekannte Konten (immer 20 + 50 Vergleiche mit `subtle.ConstantTimeCompare`).
  - Entzogenes Gerät (Abdruck in `revoked`): `401`, **nicht gezählt**, eine Journalzeile je Gerät und Stunde („revoked device signs in“). Ein entzogenes iPhone, das weiter alle paar Minuten fragt, sperrt so die anderen Geräte nicht.
  - `devices.json` nicht lesbar: `503`, nicht gezählt.
- **Bremse (eigene Schlüssel, Sicherheitsdurchsicht S-2):** `cal:<adresse>` (IPv6 je /64, wie bei der Anmeldung) und `cal-user:<konto>`, getrennt von WebDAV (`dav:`) und der Anmeldung: ein falsches WebDAV-Passwort sperrt den Kalender nicht und umgekehrt. **Die Bremse hält nur Fehlversuche auf.** Ein aktuelles Gerätepasswort eines nicht gesperrten Kontos kommt auch während einer Sperre durch; alles andere bekommt während der Sperre `429` mit `Retry-After`, ohne weitere Zählung. So kann niemand mit einem bekannten Kontonamen (die Anmeldeseite zeigt Konten im Heimnetz) die Geräte des Kontos aussperren. Raten lohnt ohnehin nicht: 140 Bit Zufall. Die Bremse wirkt gegen Lärm im Journal und Rechenzeit. Schlüssel über 128 Byte hält der Limiter als Anfang + SHA-256 (S-1, gilt auch für Anmeldung und WebDAV).
- **Was ein Gerätepasswort öffnet:** genau `/caldav/<eigenes konto>/…` und `/carddav/<eigenes konto>/…` (Lesen und Schreiben aller Kalender und des Adressbuchs dieses Kontos). **Nicht:** Freigaben, `/dav/`, die Web-API, `/mcp`, `/agent-ollama/`, fremde Konten, die Einstellungen. `bearerAccess` lässt einen `Authorization`-Kopf nur für davsync-Pfade an davsync durch; `clusterAccess` weist Anfragen mit Cluster-Kopf ab (`403 cluster_local_only`).
- **Gerätepasswörter** (`pim/devices`, `/config/users/<konto>/devices.json`):
  - 28 Zeichen aus `23456789abcdefghijkmnpqrstuvwxyz` in sieben Vierergruppen mit Bindestrich; beim Prüfen zählen Bindestriche und Großbuchstaben nicht. Gespeichert wird nur SHA-256 des normalisierten Passworts.
  - Höchstens 20 Geräte je Konto (`409 device_limit`), Namen 1–64 Zeichen ohne Steuer- und unsichtbare Zeichen, je Konto eindeutig ohne Rücksicht auf Groß/klein (`409 device_name_taken`).
  - Entzogene Abdrücke: höchstens 50, älter als 90 Tage fallen heraus.
  - **Entzogen werden alle Geräte** bei Änderung des Kontopassworts (selbst oder durch einen Administrator), beim Zurücksetzen der Zwei-Faktor-Anmeldung und wenn beides über den Cluster kommt; die Abdrücke wandern nach `revoked`, die Liste wird nie geleert. Ein gesperrtes Konto meldet sich nicht an, seine Geräte bleiben. Konto löschen nimmt `devices.json` mit (`users/<konto>/`).
  - `owner{name,created}` bindet die Datei an Konto und Anlegezeit; ein neues Konto gleichen Namens liest sie nicht. Gelesen einmal je Konto nach dem Start (neu bei anderem `Created`/Passwort-Hash), geschrieben nur beim Anlegen, Entziehen oder Entziehen aller.
  - Gerätepasswörter gelten nur auf dem Knoten, der sie angelegt hat: der Kontenabgleich im Cluster überträgt `auth.User`, nicht die Dateien unter `users/<konto>/`. Die Daten liegen ohnehin auf dem Volume dieses Knotens.
- **„Zuletzt abgeglichen“:** im RAM je Gerät (Zeit, Art, Adresse); auf dem Volume in `.titanos/calendar/<konto>/devices-seen.json` höchstens einmal je Stunde und Gerät und nur, wenn die Anfrage das Volume ohnehin geöffnet hat. Nie auf dem Stick.

## Routen

Alle Web-Routen unter `/api/v1`, Bereich `pimDataArea` (`api/pim_data.go`) bzw. `pimArea` (`api/devices.go`), registriert in `api/register_pim.go`: angemeldet (`user`), `clusterLocal`, `browserOnly` (keine Tokens, kein MCP, keine Anfragen, die titand im Namen eines Kontos stellt), nicht in `publicRoutes`, unter `CrossOriginProtection`. Fehler wie überall `{error:{code,message}}`, Texte in `web/src/lib/i18n/{de,en}.json` unter `error.<code>`. Ids von Einträgen sind Dateinamen und gehen in URLs immer durch `encodeURIComponent`. Genaue JSON-Formen: `Entwuerfe/108-kalender-kontakte/BAU-API.md` („Schritt 14a“, „Schritt 14b“, „Schritt 9/10“).

### Kalender (`api/calendar.go`, `calendar_events.go`, `calendar_ics.go`; Logik `pim/calweb`)

| Route | Zweck |
|---|---|
| `GET /calendar/access` | `{enabled, volume, timezone?}` – Volume da (nur RAM), IANA-Name der NAS-Zone |
| `GET /calendar/calendars` | eigene Kalender nach Reihenfolge, zuletzt `geburtstage` (`kind:"birthdays"`, `readOnly`, `unavailable` wenn die Kontakte nicht lesbar sind); vor dem ersten Schreiben die virtuelle `privat` |
| `POST /calendar/calendars` | anlegen (`name`, `color`); Id aus dem Namen (`[a-z0-9-]`, Umlaute ausgeschrieben), Farbe ab Werk `#7c3aed` |
| `PUT /calendar/calendars/{id}` | Name, Farbe, Reihenfolge |
| `DELETE /calendar/calendars/{id}` | `204`; der letzte → `409 calendar_last`; `geburtstage` → `403 calendar_read_only` |
| `GET /calendar/events?from=&to=&calendars=` | Vorkommen im Fenster (≤ 400 Tage), ohne `calendars` alle nicht ausgeblendeten und die Geburtstage; höchstens die frühesten 10 000 (sonst `truncated`, die Seite sagt es); `{events, truncated, birthdaysUnavailable?}` |
| `GET /calendar/events/{cal}/{obj}?occurrence=` | ein Termin oder Vorkommen mit Notiz, `etag`, `occurrences` (Zahl, wenn die Regel endet) |
| `POST /calendar/events` | anlegen; Datei mit PRODID TitanOS, zufälliger UID, VTIMEZONE |
| `PUT /calendar/events/{cal}/{obj}` | ändern mit `etag` (Pflicht), `scope` `this|following|all`, `occurrence`; anderer `calendar` = verschieben (nur ganze Reihe); alter Stand → `409 calendar_conflict` mit `current` |
| `DELETE /calendar/events/{cal}/{obj}?scope=&occurrence=&etag=` | löschen (Umfang wie oben) |
| `POST /calendar/preview` | die nächsten fünf Termine einer Regel und ihr Text (de/en), `total` wenn sie endet |
| `POST /calendar/calendars/{id}/import?onDuplicate=skip|replace` | `.ics` bis 20 MB; je UID eine Datei mit den VTIMEZONEs, die sie nennt, doppelte über UID; eine Datei über 1 MiB oder mit einer Zone über 64 KiB scheitert als `too_large`, bevor sie entsteht; geschrieben in Stapeln von 2 MiB; Zusammenfassung `{added, updated, skipped, failed}` |
| `GET /calendar/calendars/{id}/export` | `.ics` als Anhang (`text/calendar`, `nosniff`, `CSP: sandbox`, `no-store`) |
| `PUT /preferences/calendar` | ausgeblendete Kalender `{hidden:[…]}` (≤ 100 Ids); `GET /preferences` nennt sie |

Zeiten: zeitgebunden RFC 3339 mit dem Offset der Ereigniszone, ganztägig `YYYY-MM-DD` mit **exklusivem Ende** (wie `DTEND`). Bearbeiten schreibt nur geänderte Felder; was die Seite nicht kennt (`X-APPLE-*`, fremde Erinnerungen, Parameter), bleibt. Eine unveränderte Eingabe schreibt nichts (ETag bleibt). Grenzen der Eingabe: Titel und Ort je 2000 Byte, Notiz 32 KiB, Erinnerungen 0–40 320 Minuten (≤ 20), Körper 256 KiB. Titel und Ort der Liste kommen aus dem Index (≤ 256 Byte); ein gekürzter trägt `clipped:true`, die Oberfläche holt dann den ganzen Termin.

### Kontakte (`api/contacts.go`, `contacts_photo.go`, `contacts_suggest.go`, `contacts_vcf.go`; Logik `pim/cardweb`, `pim/card`, `pim/photo`)

| Route | Zweck |
|---|---|
| `GET /contacts/books` | das eine Buch `kontakte` mit Zahl (ohne Gruppen) |
| `GET /contacts?q=&book=` | Liste ohne Gruppen, sortiert nach DIN 5007-1 (Faltung, ohne `x/text/collate`), Suche wie oben, `birthdaySoon` für die Woche Mo–So |
| `GET /contacts/{book}/{id}` | ein Kontakt (Felder, Arten, eigenes iPhone-Label nur lesend, `url` nur http(s), `modified`) |
| `POST /contacts` | anlegen (vCard 3.0, UID = UUID) |
| `PUT /contacts/{book}/{id}` | ändern mit `etag`; nur abweichende Felder, unveränderte Zeilen bleiben byte-gleich; alter Stand → `409 contacts_conflict` mit `current` |
| `DELETE /contacts/{book}/{id}?etag=` | `204` |
| `GET /contacts/{book}/{id}/photo` | Bild als JPEG (≤ 512 px, `nosniff`, `CSP: sandbox`, `private, no-cache`); aus dem Zwischenspeicher oder neu aus der Karte |
| `PUT /contacts/{book}/{id}/photo?etag=` | Bild hochladen (multipart) oder aus den Dateien des NAS (`{root, path}` mit den Dateirechten des Kontos), bis 20 MB; neu kodiert als JPEG ≤ 512 px, ≤ 256 KiB in die Karte |
| `DELETE /contacts/{book}/{id}/photo?etag=` | Bild entfernen |
| `GET /contacts/suggestions?q=` | gelernte Adressen aus T-Mail, die in keinem Kontakt stehen (≤ 500); ohne Freigabe von T-Mail `[]` |
| `POST /contacts/from-suggestion` | Kontakt aus einem Vorschlag; schon vorhanden → `409 contact_exists` |
| `POST /contacts/books/{id}/import?onDuplicate=skip|replace` | `.vcf` bis 20 MB; doppelte über UID, ohne UID über Name + erste E-Mail; Gruppen gehen mit |
| `GET /contacts/books/{id}/export` | alle Karten wie gespeichert (auch Gruppen) als `.vcf`-Anhang |

Gruppen-vCards (`X-ADDRESSBOOKSERVER-KIND:group`, `KIND:group`) erscheinen in keiner Liste und keiner Suche, jede Web-Route darauf ist `404`; CardDAV gibt sie unverändert zurück. Grenzen der Eingabe: Namen 2000 Byte, Werte 1000 Byte (Straße mit Zeilen), Notiz 32 KiB, ≤ 100 je Liste, Körper 512 KiB. Bilder: Kopf geprüft vor dem Dekodieren (JPEG/PNG/WebP, ≤ 50 Mio. Pixel aus TitanOS, ≤ 24 Mio. aus einer Karte vom Gerät), ein Dekodieren zur Zeit, RAM über `TryReserve` (`503 contacts_busy`). Ein Bild per Verweis (`VALUE=uri`) holt TitanOS nie.

### Geräte und Einrichtung (`api/devices.go`, `reach_addresses.go`)

| Route | Zweck |
|---|---|
| `GET /account/devices` | eigene Geräte `{id, name, created, lastSync, lastKind, lastSyncBeforeRestart}`, nie Abdrücke |
| `POST /account/devices` | `{name, password, code}`; Web-Sitzung, Kontopasswort und ggf. Code (`reconfirm`); Name und Grenze werden vorher geprüft und kosten keinen Passwortversuch; Antwort mit dem Passwort einmal, `Cache-Control: no-store` |
| `DELETE /account/devices/{id}` | eigenes Gerät entziehen |
| `GET /users/{name}/devices`, `DELETE /users/{name}/devices/{id}` | Besitzer/Administratoren (Besitzer nur durch Besitzer): ansehen und entziehen, **kein Anlegen für andere** |
| `GET /account/devices/setup` | `{user, port, addresses:[{kind, server, caldav, carddav, certificate, recommended, hints}]}` – Reihenfolge Let's-Encrypt-Domäne, Tailscale, Heimnetz, DDNS, NetBird |

Dazu: `GET /users/{name}/agent-data` nennt für den Löschdialog `calendarEvents` und `contacts` (oder `calendarUnknown`, wenn das Volume fehlt), `/services/state` `calendar` und `contacts`.

### CalDAV und CardDAV (`backend/internal/davsync`, eigener Server-Teil ohne go-webdav)

- **Methoden:** `OPTIONS`, `PROPFIND` (Depth 0 und 1; ohne `Depth` gilt 0, `infinity` → `403 propfind-finite-depth`), `GET`/`HEAD` (mit `If-None-Match` → `304`), `REPORT`, `PUT` (`If-Match`, `If-None-Match: *`), `DELETE` (Eintrag mit ETag, Kalender mit ctag), `MKCALENDAR` (nur CalDAV), `MKCOL` (CardDAV `403`: ein Adressbuch), `PROPPATCH` (`displayname`, `calendar-color`, `calendar-order`, `calendar-description`, `addressbook-description`; anderes `403` je Eigenschaft, alles oder nichts). `MOVE`/`COPY` `403`, `LOCK`/`UNLOCK`/`ACL` `405`.
- **Reports:** `calendar-multiget`, `addressbook-multiget` (≤ 1000 `href`, sonst `507`), `calendar-query` (`comp-filter` VEVENT, eine `time-range` mit offenem Ende, UID; Filter, die TitanOS nicht auswertet, passen auf alles), `addressbook-query` (FN, EMAIL, TEL, UID; Kollationen `i;octet`, `i;ascii-casemap`, `i;unicode-casemap`), `sync-collection` (RFC 6578, `sync-level` 1, mit `limit` in Seiten mit Fortsetzungs-Token). Höchstens eine `time-range` und 64 Filter mit Text je Abfrage, 256 Eigenschaften je PROPFIND/REPORT (S-3, S-4).
- **Kopf `DAV`:** CalDAV `1, 3, access-control, calendar-access, extended-mkcol`, CardDAV `1, 3, access-control, addressbook`.
- **Eigenschaften:** `current-user-principal`, `principal-URL`, `calendar-home-set`/`addressbook-home-set`, `displayname` (Konto), `resourcetype`, `supported-calendar-component-set` (nur VEVENT), `getctag`, `sync-token`, `getetag`, `calendar-color`, `calendar-order`, `current-user-privilege-set` (mit `write`), `supported-report-set`, `calendar-data`/`address-data` (ganze Bytes; `comp`/`expand`/`prop` werden nicht angewandt).
- **Schreiben:** nur VEVENT (VTODO, VJOURNAL → `403 supported-calendar-component`), gültige Daten (`403 valid-calendar-data`/`valid-address-data`), eine UID je Datei; eine andere UID beim Überschreiben → `409 no-uid-conflict`. Grenzen `413 max-resource-size`, `507` bei voller Sammlung. Eine Karte mit Bild über 512 KiB vom Gerät wird verkleinert gespeichert und **ohne ETag** beantwortet (das Gerät holt den neuen Stand); ein nicht lesbares Bild (HEIC …) bleibt, wie es kam.
- **Körper:** höchstens 256 KiB, 60 s Zeit, Tiefe 32, 20 000 Elemente, kein `DOCTYPE`/`ENTITY`. Vor der Anmeldung wird kein Körper gelesen.
- **Journal:** nur bei `500` und abgebrochenem Multiget eine Zeile mit Konto, Gerätename, Sammlung, Methode und Fehlercode; nie Titel, Orte, Namen aus Karten. Abgleiche stehen nicht im Aktivitätsprotokoll (das greift nur unter `/api/v1/`); Anlegen und Entziehen von Geräten schon.

## Speicher

- **Ort:** auf dem Volume der Systemdaten (`volumedata.Home`, wie T-Mail und der Agent), `.titanos/calendar/<konto>/` und `.titanos/contacts/<konto>/`, root, nie im Overlay oder RAM als Ersatz. Lesen legt nichts an; erst das erste Schreiben legt die Ordner an. Ein Umzug der Systemdaten nimmt beide Funktionen mit (`systemData.Watch`).
  ```
  .titanos/calendar/<konto>/owner.json          {"name","created"} – bindet den Ordner an das Konto
  .titanos/calendar/<konto>/devices-seen.json   „zuletzt abgeglichen“
  .titanos/calendar/<konto>/<sammlung>/state.json
  .titanos/calendar/<konto>/<sammlung>/o/<eintrag>   die empfangenen Bytes
  .titanos/contacts/<konto>/kontakte/state.json, o/<eintrag>, photos/<eintrag>.jpg (Zwischenspeicher)
  ```
  Dateinamen auf der Platte prozentkodiert außer `[A-Za-z0-9._~-]`, höchstens 255 Byte; jeder Zugriff über `os.Root`, Kontoordner ohne Symlink.
- **`state.json`** je Sammlung: Name, Farbe, Reihenfolge, Epoche, `seq`, Index aller Einträge (ETag, UID, Größe, Zeiten, Titel/Ort gekürzt, Zone, Erinnerungen, bei Karten FN, Sortierschlüssel, Firma, E-Mails, Nummern, Adressen, Geburtstag, Gruppe, Bild ja/nein) und das Änderungsprotokoll (letzte 2000). Der Index ist neu baubar; beim ersten Laden vergleicht der Store Größe und Änderungszeit jeder Datei, bei Abweichung baut er ihn neu (neue Epoche, die Geräte gleichen einmal voll ab). Jeder Eintrag atomar (`volumedata.Write`), danach `state.json`; Sperre je Sammlung um Prüfen, Schreiben, Zählen.
- **ETag** = 32 Hex-Zeichen (16 Bytes SHA-256 der gespeicherten Bytes), stark; `W/` passt nie. **ctag** = `<epoche>-<seq>`, **Sync-Token** = `https://ns.titanos.invalid/sync/<epoche>/<seq>`; jede Änderung erhöht `seq`. Ein Token mit anderer Epoche, aus der Zukunft oder älter als das Protokoll → `403 valid-sync-token`, das Gerät gleicht voll ab. Eine neue Sammlung mit dem Namen einer gelöschten bekommt eine neue Epoche.
- **Virtuelle Vorgaben:** `privat` („Privat“, `#7c3aed`) und `kontakte` („Kontakte“) gibt es ohne Datei, bis zum ersten Schreiben (ctag `0-0`). Ein Konto, das nie etwas einträgt, legt nichts an. Solange keine eigene Sammlung existiert, ist die Vorgabe immer da; die letzte eigene lässt sich nicht löschen (Web-API `409 calendar_last`, DAV `403`). Ein `MKCALENDAR` vor dem ersten Eintrag macht `privat` im selben Schritt echt, damit das iPhone „Privat“ behält.
- **Marke „Daten vorhanden“:** beim ersten Schreiben, das eine eigene Sammlung hinterlässt, schreibt der Store einmal `/config/users/<konto>/pim.json` (`{"owner":{name,created},"calendar":<zeit>,"contacts":<zeit>}`). Fehlt danach der Kontoordner auf dem aktuellen Volume (anderes Volume eingebunden, Umzug ohne Daten, Ordner eines früheren Kontos), antwortet der Store `ErrElsewhere` – also `503` wie ohne Volume – statt der leeren Vorgabe; das Journal nennt einmal das eingebundene Volume, auf dem die Daten liegen. Eine nicht lesbare Marke ist ebenfalls `503`. Die Daten kommen nicht von selbst zurück: Volume wieder einbinden oder die Systemdaten zurückziehen. Einen Knopf „neu beginnen“ (`Store.Abandon`) gibt es noch nicht.
- **Stick:** `devices.json` und `pim.json` unter `/config/users/<konto>/`, geschrieben nur bei Änderung (Gerät anlegen/entziehen, erste Marke je Funktion); gelesen einmal, danach aus dem RAM. Keine zyklischen Zugriffe auf den Stick.
- **Ruhemodus der Platten:** Jeder Stand bleibt nach dem ersten Lesen im RAM. Ein unveränderter `PROPFIND` auf das Home-Set, `getctag`, `HEAD`, ein Multiget nur nach `getetag` und die Monatsansicht einzelner Termine antworten **ohne Zugriff auf das Volume**; ein iPhone weckt die Platten also nicht alle 15 Minuten. Das Volume öffnen nur Einträge lesen (`GET`, Multiget mit Daten), Schreiben und Reihen, deren Ausdehnung nicht im Zwischenspeicher liegt.
- **Konto löschen** (hier oder durch einen anderen Knoten): RAM, laufende DAV-Anfragen des Kontos (Kontext abgebrochen), beide Kontoordner auf dem Volume, `devices.json` und `pim.json` mit `users/<konto>/`. Synchron vor der Antwort. Fehlt das Volume dabei, bleibt der Ordner liegen; `owner.json` bindet ihn an die alte Anlegezeit, ein neues Konto gleichen Namens liest ihn nie und ersetzt ihn beim ersten Schreiben. Ein neues Konto gleichen Namens bekommt eine andere Anlegezeit, auch in derselben Sekunde (`auth.Users` wartet dann bis zur nächsten).

## Wiederholungen und Zeitzonen

- **Schutzschicht `pim/recur`** (einzige Stelle mit rrule-go): erlaubt sind `FREQ` täglich/wöchentlich/monatlich/jährlich, `INTERVAL` 1–1000, `COUNT` 1–5000, BYDAY-Ordinalzahlen bis 5 (monatlich) bzw. 53 (jährlich), die übrigen BY-Teile in ihren Grenzen, kein `BYEASTER`, kein `BYHOUR`/`BYMINUTE`/`BYSECOND` außer dem Wert aus `DTSTART`, Jahr von `DTSTART` 1900–2200, und eine Regel muss in 400 Jahren treffen. Eigenes Lesen von `DTSTART`, `DTEND`/`DURATION`, `RRULE`, `RDATE`, `EXDATE` (auch Listen, mit `TZID` und `VALUE=DATE`); `UNTIL` in die Zone von `DTSTART` umgerechnet; jede Ausdehnung mit `recover()`.
- **Grenzen:** Fenster höchstens 400 Tage je Abfrage, höchstens 5000 Vorkommen je Eintrag und Abfrage (`truncated`), 200 000 Schritte; je Abfrage eines Kalenders und je Terminliste höchstens die frühesten 10 000 Vorkommen (`truncated`; spätere Reihen werden nur bis zum letzten behaltenen Beginn ausgerechnet, S-20), `calendar_search` des Agenten nur die frühesten `limit` + 1 bzw. mit Suchtext 2000; je Datei 1 MiB, 1000 VEVENT, 5000 Komponenten, Tiefe 8, 1000 EXDATE/RDATE je Ereignis, **ein VTIMEZONE höchstens 64 KiB** (beim Schreiben über CalDAV und die Web-API, sonst `valid-calendar-data` bzw. `calendar_invalid`; S-21). Der Zwischenspeicher gelesener Reihen (1024 Dateien, 32 MiB) zählt die Vorkommen des letzten Fensters mit (S-22).
- **Nicht unterstützt** heißt: gespeichert und unverändert ausgeliefert, in TitanOS als einzelner Termin mit `recurUnsupported:true` („Die Wiederholung dieses Termins kann T-Kalender nicht anzeigen; sie bleibt beim Speichern erhalten“). Eine Regel, die mehr ist als die einfache Form der Seite, steht als Text („Eigene Regel: …“) und bleibt beim Speichern, wie sie ist (`custom:true`).
- **Ausnahmen:** weitere VEVENT mit derselben UID und `RECURRENCE-ID` ersetzen ihr Vorkommen; eine Ausnahme ohne passendes Vorkommen wird trotzdem gezeigt. „Nur diesen“ schreibt eine Ausnahme (nie mit RRULE) oder ein EXDATE, „Diesen und folgende“ kürzt die Reihe (`UNTIL`/Rest von `COUNT`) und legt eine neue an, „Die ganze Reihe“ ab Vorkommen N verschiebt um dieselbe Zahl Tage. Verworfene Ausnahmen zählt die Antwort (`droppedExceptions`), die Seite sagt es.
- **Zeitzonen (`pim/tz`, mit `time/tzdata` im Binary):** IANA-Namen direkt; **Windows-Namen** (Outlook, Exchange: `W. Europe Standard Time`) über eine feste Tabelle aus CLDR `windowsZones.xml`; eine unbekannte TZID mit `VTIMEZONE` über `X-LIC-LOCATION` oder eine IANA-Zone mit gleichen Offsets und Umstellungstagen, sonst die feste Verschiebung des letzten `STANDARD` mit `tzGuess:true` („T-Kalender hat sie erraten“). Die Bytes bleiben. Beim Bearbeiten bleibt der alte TZID-Text, solange die Zone gleich bleibt; eine neue Zone bekommt den IANA-Namen und einen erzeugten `VTIMEZONE` (`tz.VTimezone`).
- **NAS-Zone:** Floating (ohne `Z`, ohne `TZID`) und Ganztägiges gelten in der Zeitzone des NAS; `<C:timezone>` einer CalDAV-Abfrage wird nicht gelesen. Angezeigt und angelegt wird in der NAS-Zone (`/calendar/access` `timezone`), ohne Namen in der Zone des Browsers. Eine wöchentliche Reihe um 09:00 bleibt über die Umstellung bei 09:00.

## iOS

- **Was gebaut ist** (nach den Beobachtungen in TECHNIK 1.5, sabre.io und Berichten; Apple veröffentlicht keine Anforderungen): Discovery über `/.well-known`, `current-user-principal`, Home-Sets, `getctag`, `sync-token` mit `sync-collection`, `calendar-query` mit offenem `time-range`, Multiget, `If-Match`/`If-None-Match`, `MKCALENDAR` mit Farbe, `PROPPATCH` (iOS setzt auf Principal und Einträgen Eigenschaften, die TitanOS nicht kennt: `207` mit `403` je Eigenschaft), vCard 3.0 mit Gruppen, `OPTIONS` auf den Principal mit `DAV` und `Allow` ohne Anmeldung.
- **Fall Stalwart** ([Bericht vom 17./18.08.2026, bestätigt 12.09.2026](https://support.stalw.art/t/ios-26-6-caldav-account-remains-inactive-after-successful-discovery-and-options-on-principal-url/1405)): Mit iOS 26.6 blieb ein über Mail angelegter CalDAV-Account nach `PROPFIND /.well-known/caldav` → `PROPFIND` → `OPTIONS` auf den Principal „inaktiv“; über Einstellungen › Apps › Kalender › Kalender-Accounts angelegt ging er. Die Einrichtungsseite nennt deshalb genau diesen Pfad und warnt vor Mail. TitanOS meldet im `DAV`-Kopf weniger als der Server im Bericht (kein `calendar-auto-schedule`, kein `calendar-no-timezone`); ob das den Fall vermeidet, ist nicht bekannt.
- **Geprüft:** iOS-artige Anfragefolgen in `backend/internal/davsync/testdata/ios26/*.txt` (`TestIOS26`: Einrichten Kalender und Kontakte, `301` und `308`, fremdes Konto, Multiget, Anlegen/Ändern/Löschen, Konflikt, UID-Wechsel, zu groß, falsche Namen, VTODO, Gruppen, erster und weiterer Abgleich, Seiten, veraltetes Token, unveränderter ctag ohne Plattenzugriff, offene und laufende Abfragen, Reihen mit EXDATE, Ganztägiges über die Umstellung, voller Abgleich wie iOS, virtuelle Vorgabe bleibt, Bild verkleinert, Volume weg und `ErrElsewhere`) und die Kette durch titand (`api/devices_dav_test.go`).
- **Nicht geprüft: kein echtes iPhone, kein iOS 26, kein Mac.** Offen am Gerät: die Menüpfade der Einrichtung und „Standardkalender/Standardaccount“, ob iOS dem selbst signierten Zertifikat nach der einmaligen Warnung dauerhaft vertraut, ob iOS `301` bei PROPFIND folgt (`308` ist mitgetestet), wie iOS mit `507`/Seiten im `sync-collection` umgeht (ohne Limit des Geräts kürzt TitanOS nicht), das Verhalten bei `503` mit `Retry-After`, Bilder von iOS, das Zusammenspiel der geänderten Bremse (S-2) mit echten Geräten.

## Agent und Home Assistant

Kurzfassung; Einzelheiten in `docs/AGENT.md`, Abschnitt „T-Kalender und Kontakte (1.11)“.

- Zwei Bereiche „Kalender“ und „Kontakte“ unter Einstellungen › KI-Agent › Rechte, je mit **Lesen** (Erlaubt/Verboten) und **Eintragen** (Ohne Nachfrage/Nachfragen/Verboten; Löschen zählt zum Eintragen), gespeichert in `policy.dataRights`. Ab Werk (Schnellwahl „Standard“): Lesen ja, jedes Eintragen mit Nachfrage – auch Anlegen.
- Neun eigene Werkzeuge (`calendar_list`, `calendar_search`, `calendar_create`, `calendar_update`, `calendar_delete`, `contacts_search`, `contacts_create`, `contacts_update`, `contacts_delete`), nur für das eigene Konto des Chats, nicht im MCP-Katalog.
- **Immer mit Nachfrage**, über alle Rechte, „Alles ohne Nachfrage“, Arbeitsweisen und geplante Aufträge: eine ganze Reihe löschen oder die Wiederholung der ganzen Reihe ändern (S-13); Löschen nach gelesenem fremdem Inhalt (S-14). Gelesene Termine und Kontakte zählen als fremder Inhalt.
- Home Assistant (Assist) ab Werk ohne Zugriff, höchstens „Nur lesen“ (`homeassistant.assistData` in `agent.json`), gelesen wird das Konto „Assist als“.
- Die gelernten Adressen von T-Mail erreicht kein Werkzeug.

## Sicherheit

Die Sicherheitsdurchsicht nach dem Bau (alle 27 Fragen aus TECHNIK 8.3) und eine Nachprüfung von außen stehen mit allen Funden in `docs/validation/sicherheit-111.md`: 22 Funde, zwei hohe (S-19 Import vervielfachte eine Zeitzone in jede Datei, S-20 keine Gesamtgrenze der Vorkommen einer Abfrage – beide RAM des ganzen NAS aus einem Konto), fünf mittlere (S-1 lange Namen in der Bremse, S-2 Aussperren mit bekanntem Kontonamen, S-13 Reihe ohne Nachfrage gekürzt, S-14 Löschen nach fremdem Inhalt ohne Nachfrage, S-21 Zeitzonen ohne Größengrenze beim Schreiben und im Export), sieben niedrige, acht Hinweise. Behoben mit Regressionstest: S-1, S-2, S-3, S-4, S-13, S-14, S-15, S-19 bis S-22. Entscheidung des Inhabers offen: S-5 (Gesamtgröße je Konto). Die übrigen sind begründet gelassen.

Zusammengefasst: Gerätepasswörter öffnen nur davsync des eigenen Kontos und entstehen nur in der Web-Sitzung mit Kontopasswort und Code; Pfade werden einmal dekodiert und laufen über `os.Root`; XML, iCalendar, vCard und Bilder haben Grenzen vor dem Parsen und fangen Paniken ab; jede Web-Route ist `browserOnly`, `clusterLocal` und gegen fremde Seiten geschützt (`TestPIMDataCrossSite`); Text von außen steht in der Oberfläche als Text (kein `{@html}`), Links nur `http(s)`, `mailto`, `tel` mit `rel="noopener noreferrer"`, die Webadresse eines Kontakts zeigt das echte Ziel (S-15). In Notizen ist der Linktext der rohe Text wie in T-Mail (S-18, Hinweis). Journal und Diagnosepaket enthalten keine Inhalte und nichts aus `users/`.

## Sicherung

- **Kalender und Kontakte stehen in keiner Datensicherung** (S-11). Aufträge der Datensicherung lassen `.titanos` immer aus, die Sicherung des Sticks enthält nur `/config`. Schutz heute: die Kopie auf den Geräten und der Export als `.ics`/`.vcf`. Ob eine Btrfs-Replikation des Volumes sie mitnimmt (Schnappschuss des ganzen Volumes), ist nicht geprüft.
- **`devices.json` und `pim.json`** liegen unter `/config/users/<konto>/` und sind in jeder Sicherung des Sticks. **Eine Wiederherstellung bringt die Geräte im Stand der Sicherung zurück** (S-10): ein danach entzogenes Gerät, etwa ein verlorenes Telefon, gilt wieder. Nach einer Wiederherstellung deshalb unter „Am iPhone einrichten“ die Geräte prüfen und verlorene erneut entziehen (gleich bei WebDAV-Passwort und API-Tokens).
- Der Export eines Kalenders ist ein VCALENDAR mit jedem VTIMEZONE einmal und allen VEVENTs wie gespeichert; ein Re-Import ergibt dieselben Termine (Test). Die Zeitzonen eines Exports sind auf 2 MiB begrenzt (reale Zonen haben wenige KiB); darüber fehlen weitere, ihre Termine behalten die TZID (IANA-Namen liest ein Re-Import richtig, sonst mit `tzGuess`), das Journal nennt die Zahl (S-21). Der Export der Kontakte enthält auch die Gruppen.

## Grenzen

- **Größen:** `.ics` 1 MiB und `.vcf` 2 MiB je Eintrag, 20 000 Einträge je Sammlung (`507`, `calendar_full`/`contacts_full`), 50 Kalender je Konto, Import 20 MB, VTIMEZONE 64 KiB, Terminliste 10 000 Vorkommen, Bild in der Karte ≤ 512 KiB vom Gerät (größer wird verkleinert), aus TitanOS 512 px/256 KiB. **Keine Gesamtgrenze je Konto:** ein Konto kann das Volume der Systemdaten an Freigabe-Kontingenten vorbei füllen (S-5, Vorschlag 2 GiB je Konto, **Entscheidung des Inhabers offen**).
- **Nicht gebaut:** gemeinsame Kalender zwischen Konten, Einladungen per E-Mail (iTIP/iMIP, `calendar-auto-schedule`), Abos fremder Kalender (Feiertage, Müllabfuhr), Erinnerungen und Mitteilungen von TitanOS selbst (Erinnerungen kommen nur von den Geräten), Aufgaben (VTODO) und die Erinnerungslisten von iOS, Termine per Ziehen verschieben, eigene Wiederholungsregeln bearbeiten (nur anzeigen und behalten), Gruppen in Kontakte, mehrere Adressbücher, ein Profil `.mobileconfig` zum Antippen statt Abtippen (für später notiert), der Knopf „Daten liegen auf Volume X – zurückholen oder neu beginnen“.
- **Ein `state.json` aus Entwicklungsständen vor 14a/14b** hat Ort, Zone, Erinnerungen und Adressen im Index erst nach dem nächsten Schreiben des Eintrags (betrifft nur Testsysteme, 1.10 hatte keinen Store).
- **Umzug der Systemdaten:** übersteht weiter keinen Neustart (wie T-Mail); die Marke in `pim.json` verhindert dabei die leere Vorgabe, holt die Daten aber nicht zurück.
- **Konto ohne Volume gelöscht:** der Ordner bleibt bis zum ersten Schreiben eines gleichnamigen neuen Kontos liegen (S-6), nicht beim Wiederkommen des Volumes.
- **Cluster:** `/calendar` und `/contacts` laufen immer auf dem eigenen Knoten (`cluster-node.ts`, `clusterLocal`). Mit gewähltem anderem Knoten zeigt die Bildwahl „Aus den Dateien des NAS“ die Dateien jenes Knotens, das Bild geht aber an dieses NAS – `file_not_found` oder eine falsche Datei (Anhänge in T-Mail verhalten sich gleich); vermerkt, nicht behoben. Kein Test mit zwei echten Knoten.
- **Kontaktbild:** die Bildroute ist nur gegen Testdaten im Browser geprüft, nicht gegen ein echtes Volume; sie beantwortet `If-None-Match` nicht (die Seite lädt mit `?v=<etag>` neu). Eine Karte vom Gerät dekodiert ihr Bild ohne eigenes RAM-Budget (≤ 24 Mio. Pixel, eins zur Zeit).
- **Agent nach fremdem Inhalt:** Anlegen und Ändern folgen den Rechten (S-16): mit Eintragen „ohne Nachfrage“ ändert ein Chat nach gelesenem fremdem Inhalt einzelne Termine und Kontakte ohne Nachfrage (jeder Schritt steht mit „alt → neu“ im Chat). Weil schon das Lesen des eigenen Kalenders als fremder Inhalt zählt, fragt fast jedes Löschen durch den Agenten nach, auch unter „Alles ohne Nachfrage“ (S-14).
- **Vorschläge aus T-Mail** stehen in der Seitenleiste auch ohne Freigabe von T-Mail; die Seite erklärt dann, warum es keine gibt. Am Handy führt „Zurück“ nach „E-Mail schreiben“ zu T-Mail, nicht zu Kontakte.
- **Sortierung:** DIN 5007-1 über Faltung, keine vollständige Unicode-Kollation (`x/text/collate` hätte das Programm um 1,5 MB vergrößert).

## Prüfstand

Stand 18.11.2026, Zweig `feat/v1.11`; Umfang und Ergebnisse der Schlussprüfung in `docs/releases/v1.11.0.md` („Prüfung“). Kurz: Go-Tests aller neuen Pakete mit Fuzz-Zielen (Seeds als Tests, lokal je 20–60 s ohne Fund), iOS-artige Anfragefolgen, die Kette durch titand, Playwright-Skripte mit Testdaten (`scripts/test-ui-kalender.mjs`, `test-ui-kontakte.mjs`, `test-ui-iphone-setup.mjs`, `test-ui-agent-rights-pim.mjs`, `test-ui-account-delete.mjs`, `test-ui-mail.mjs`) und die Sichtbarkeit gegen `titand -dev` ohne Attrappe (`test-ui-pim-visibility.mjs`). Nicht geprüft: echtes iPhone/iOS 26, echtes NAS mit Volume für Bilder und Ruhemodus, Cluster mit zwei Knoten, Btrfs-Replikation, ein echtes Modell für den Agenten.
