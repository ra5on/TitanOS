# Dokumente und Fristen (PLAN 54-D)

Drei Funktionen des KI-Agenten: **Dokumenten-Gedächtnis**, **Fristenwächter** und **Papierkram-Autopilot**. Alle sind ab Werk aus. Owner und Administratoren schalten sie unter Einstellungen › KI-Agent › Funktionen für das NAS ein (`/api/v1/agent/features`, Teil `docs`). Danach schaltet jedes Konto sie in der Unterseite für sich selbst ein (`/api/v1/agent/features/me`, Teile `docs` und `paper`). Die Werte ab Werk sind die Antworten des Inhabers aus §54-D; jeder andere Weg ist eine Einstellung (§54-E).

## Bedienung

**Dokumenten-Gedächtnis** (Entwurf 02)
- Ordner: ab Werk nur `KI-Agent/<konto>/Dokumente`. Mit „+ Ordner hinzufügen“ kommt jeder Ordner einer Freigabe dazu, die das Konto lesen darf; mit ✕ fällt er wieder heraus. Ganze Volumes, USB und der Stick sind nie wählbar.
- Eingelesen wird nachts ab der NAS-weiten Uhrzeit (`docs.at`, ab Werk 03:00), drei Stunden lang alle zehn Minuten, solange noch ein Ordner fehlt. Jeder Ordner zeigt Dateien, letzten Lauf und Zustand in Worten: eingelesen, läuft (mit %), Platte schläft, kein Zugriff. „Jetzt einlesen“ startet einen Lauf sofort.
- Gelesen werden: PDF-Textschicht (`pdftotext` über `office.ReadPDF`, bis 60 Seiten), `.txt`, `.md`, `.csv`, `.docx`, `.xlsx`. Bei Scans (PDF ohne Text, die ersten drei Seiten mit `pdftoppm` als Bild) und Fotos (`.jpg`, `.png`, `.webp`) fragt titand das Bild-Modell des Agenten (`Ollama.Look`, mit demselben `keep_alive` und `num_ctx` wie `image_view`). Ohne erreichbares Ollama oder ohne Bild-Modell warten Scans auf den nächsten Lauf. „Scans und Fotos“ lässt sich je Konto abschalten.
- Werkzeug des Agenten: **`documents_search`** (lesen, fremder Inhalt). Es antwortet mit Datei, Seite und Ausschnitt. Der Name `docs_search` ist schon für das TitanOS-Handbuch vergeben.

**Fristenwächter** (Entwurf 03)
- Er erkennt Kündigung, Garantie, Zahlung und Fälligkeit nach Regeln mit deutschen und englischen Datumsangaben. Beispiele: „einen Monat vor Ablauf (31.12.)“ ergibt den 30.11., „2 Jahre Gewährleistung“ zählt vom Rechnungsdatum, „zahlbar innerhalb von 14 Tagen“ ebenso, „abgebucht“ heißt Lastschrift.
- „Erst vorschlagen“ (ab Werk): Die Frist erscheint in den Vorschlägen des Agenten (Art `deadline`) und unter „Erkannte Fristen“, jeweils mit Quelle. **Übernehmen** legt Erinnerungen an (ab Werk 14 und 2 Tage vorher, jeweils 09:00), die als Mitteilung kommen. **Öffnen** führt zum Ordner im Dateimanager, **Bezahlt** gibt es bei Rechnungen aus dem Papierkram, **Ablehnen** verwirft. Eine abgelehnte Frist kommt nicht wieder.
- „Gleich als Erinnerung“: Die Erinnerungen entstehen ohne Rückfrage. Die Vorlaufzeiten (höchstens drei) stellt jedes Konto selbst ein.

**Papierkram-Autopilot** (Entwürfe 04/05)
- Eingang `KI-Agent/<konto>/Eingang`, angelegt beim ersten Lauf. Der Scanner legt per SMB ab, oder man zieht Dateien hinein. titand sieht alle zehn Minuten nach, Dateien jünger als eine Minute bleiben noch liegen. „Jetzt nachsehen“ startet sofort.
- Eigenes Postfach (IMAP) alle 20 Minuten: Server, Port, TLS oder STARTTLS (Zertifikat wird immer geprüft), Benutzer, Passwort. PDF- und Bildanhänge kommen in den Eingang, der Mailtext als Markdown-Dokument („Anhänge und Mailtext“, ab Werk). Die Alternative ist „Nur Anhänge“. Im Postfach setzt der Agent nur `\Seen`, er löscht und verschiebt nichts. Erst wird alles geschrieben, dann wird die Mail als gelesen markiert. Doppelte Mails erkennt er an der Message-ID.
- Erkannt werden Absender, Datum, Betrag, Fälligkeit, Art (Strom und Gas, Internet und Handy, Versicherungen, Einkäufe, Gesundheit, Wohnen, Steuern und Ämter, Sonstiges) und Dokumentart. Der neue Name lautet etwa `2026-11-03 Stadtwerke Abschlag.pdf`. Ablageort ist ab Werk `KI-Agent/<konto>/Dokumente/Ablage/<Absender>/`; das Schema „je Art“ oder „je Jahr“ ist wählbar, ebenso jeder beschreibbare Ordner einer Freigabe.
- „Selbst ablegen“ (ab Werk) oder „Jedes Mal vorschlagen“. Bei Unsicherheit (Absender, Art oder Datum fehlen, zwei Arten passen etwa gleich gut) bleibt die Datei im Eingang und wartet auf **Zuordnen**. **Rückgängig** schiebt eine abgelegte Datei in den Eingang zurück, dort wartet sie auf Zuordnen.
- Ansicht „Papierkram“ (Menü des Agenten): drei Monate, die Summe mit Vormonat, Summen nach Art, offene Zahlungen (mit „Bezahlt“) und zuletzt Abgelegtes. Auf dem Handy ist sie Vollbild, auf dem Desktop eine Seitenleiste mit 460 px. Werkzeug `paperwork_summary` für „Was habe ich im November ausgegeben?“.

## Daten

- Index, Zustand und Papierkram-Protokoll liegen in `.titanos/docs/<konto>/` auf dem Volume des Agenten (`index.json`, `state.json`, `paper.json`, root, 0700, über `volumedata.Open`). Sie liegen nie auf dem Stick. Je Dokument bleiben höchstens 24 KiB Text für Ausschnitte, je Konto höchstens 64 MiB Text. Ist das Gedächtnis voll, warten neue Dokumente, und der Ordner zeigt „Speicher voll“. Einen unlesbaren Index baut der nächste Lauf neu auf.
- Es liest immer nur ein Konto gleichzeitig ein (RAM, eine Grafikkarte). Die anderen Konten folgen in den nächsten Zehn-Minuten-Fenstern der Nacht.
- Ein Ordner, der früher gespeichert wurde und inzwischen nicht mehr erreichbar ist, bleibt in den Einstellungen und zeigt „kein Zugriff“. Er blockiert das Speichern anderer Einstellungen nicht. Neue Ordner prüft titand immer.
- Einstellungen je Konto liegen in `features.json` im privaten Agent-Ordner (Gerüst). Das IMAP-Passwort steht nur dort. `view()` gibt `passwordSet` statt des Werts zurück, ein PUT ohne Passwort behält das alte.
- API (nur Web-Sitzung, `requireAgent`): `GET /agent/docs`, `POST /agent/docs/run`, `GET /agent/docs/search?q=`, `GET /agent/paper?month=`, `POST /agent/paper/run`, `POST /agent/paper/mail/test`, `POST /agent/paper/{id}/undo|assign|paid`. Für Fristen dient das vorhandene `/agent/inbox`.

## Platten-Ruhe und Rechte

- Hintergrundarbeit läuft nur, wenn die Platte des Agent-Volumes wach ist (`agentVolumeAsleep`). Jeder Ordner wird nur gelesen, wenn seine Platte wach ist (`agentAsleep`); sonst steht „Platte schläft“ da, und der Lauf holt ihn später in der Nacht nach. Das Bild-Modell arbeitet nicht, solange ein Chat läuft, und höchstens 40 Seiten je Lauf.
- Rechte prüft titand bei jedem Speichern, bei jedem Lauf (Ordner ohne Zugriff fallen samt Index heraus) und bei jeder Suche (nur Freigaben, die das Konto heute sieht, plus die Ordner-Regeln des Agenten, „Verboten“ blendet Treffer aus). Versteckte Dateien und `.titanos` werden nie gelesen. Jedes Konto sieht nur seinen eigenen Index und sein eigenes Protokoll.

## Tests

- `internal/docindex`: Datumsformate, Beträge, elf deutsche und englische Fristbeispiele, BM25-Suche mit Umlauten und Stämmen. Hinzu kommen sieben **Testbriefe** (`testdata/briefe`: Stadtwerke, Telekom, HUK, Zahnarzt, MediaMarkt, congstar-Mail, Finanzamt). Ergebnis: **42 von 42 Werten richtig** (Absender, Art, Dokumentart, Datum, Betrag, Fälligkeit). Die Briefe sind ausgedachte Testbriefe, an denen die Regeln entwickelt wurden. Für echte Post ist das keine Messung.
- `internal/imapmini`: ein falscher IMAP-Server per `net.Listen` mit TLS und STARTTLS. Geprüft werden PEEK ohne Gelesen-Markierung, `\Seen` erst durch STORE, kein EXPUNGE/MOVE/COPY, falsches Passwort ohne Passwort in der Meldung, unbekanntes Zertifikat und Klartext abgelehnt, MIME (quoted-printable, base64, Latin-1, nur HTML) und unsichere Anhangnamen.
- `internal/api/agent_docs_test.go`: ab Werk aus (Routen 409, keine Werkzeuge), NAS-Schalter nur für Admins, fremde Freigabe, Volume und `..` als Ordner abgelehnt, Ablage nur in beschreibbare Freigaben, Passwort nie in `view()` und bei leerem PUT behalten. Außerdem: Einlesen, Suche und Werkzeug mit Quelle, Vorschlag mit Quelle, Übernehmen mit Erinnerungen am 16.11. und 28.11. (zu 30.11.), kein zweiter Vorschlag, kein Treffer für ein anderes Konto, „Gleich als Erinnerung“. Beim Papierkram: Ablage, frische Datei bleibt liegen, unsichere Datei bleibt liegen, Monatssumme, Rückgängig, Zuordnen, Bezahlt, „Jedes Mal vorschlagen“, Scan wartet auf das Bild-Modell. Beim Postfach: Mails vom falschen Server, Anhang und Mailtext abgelegt, gelesen markiert, Mail nach verlorenem STORE nicht doppelt. Dazu die Nachtzeit.
- Oberfläche: `scripts/test-ui-agent54-docs.mjs` mit gemockter API. Bilder in `out/validation/v057-docs/` bei 390×844 und 1440×900, ohne waagrechtes Scrollen und ohne Überlappung in den Zeilen.

## Grenzen und nicht Geprüftes

- **Texterkennung von Scans** hängt am Bild-Modell. Für Beträge und Daten aus echten Scans gibt es noch keine Messung, gemessen sind nur Textbriefe. Der Inhaber wollte Testbriefe auf der RTX 5060 Ti mit gemma4: dieser Lauf steht aus. Tesseract wird bewusst nicht benutzt.
- **Fristen** kommen nur aus Regeln; das Modell wird für Fristen nicht gefragt. Ungewöhnliche Formulierungen fallen durch, Datumsangaben wie `11/03/2026` (Schrägstrich) erkennt der Wächter nicht.
- **Suche** ist eine Wortsuche mit BM25 und leichter Stammbildung, keine Bedeutungssuche (keine Embeddings). Jede Suche liest den Index des Kontos neu ein und zerlegt ihn (bei großen Archiven einige hundert Millisekunden; nicht gemessen).
- **Öffnen** führt zum Ordner der Datei im Dateimanager. Die Route `/files` kennt keinen Parameter, der eine einzelne Datei auswählt.
- **Postfach**: Nur ungelesene Mails im INBOX-Ordner. Eine Mail, die schon in einem anderen Programm gelesen wurde, holt der Agent nicht. Höchstens 20 Mails je Abruf, 30 MiB je Mail. Die Größe wird vor dem Abholen erfragt. Zu große, vom Server falsch angegebene und unlesbare Mails bleiben ungelesen im Postfach und kommen auf eine Merkliste (`uidvalidity:uid`, höchstens 500). Sie blockieren die nächsten Mails nicht. Gerät der Datenstrom durcheinander, verbindet sich der Agent neu. Zeilen (1 MiB) und Antworten (eine Mail) sind begrenzt, ein fremder Server kann den RAM nicht füllen. Kein OAuth (Gmail/Outlook verlangen App-Passwörter).
- Abgelegtes wird erst im nächsten Nachtlauf durchsuchbar, wenn der Ablage-Ordner zu den Ordnern des Gedächtnisses gehört (ab Werk ja).
- Zieht der Agent auf ein anderes Volume um, nimmt er `.titanos/docs` nicht mit. Der Index baut sich in der nächsten Nacht neu auf, das Papierkram-Protokoll (Rückgängig) bleibt auf dem alten Volume.
- `paper.json` wird nur geschrieben, wenn sich etwas geändert hat. Der Zeitpunkt des letzten Postfach-Abrufs steht nur im RAM, so kann die Platte in Ruhe gehen. Ein Absturz in einem Hintergrundlauf wird protokolliert und beendet titand nicht.
- Das Lesen des Eingangs alle zehn Minuten kann eine Platte wach halten, wenn ihr Verzeichnis nicht im Cache liegt. Auf echter Hardware ist das nicht gemessen.
- Nicht auf echter Hardware, nicht mit echtem Ollama, nicht mit echten IMAP-Servern und nicht mit Scannern geprüft.
