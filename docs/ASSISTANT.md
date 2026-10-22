# KI-Assistent

Stand: 22. Oktober 2026 · Entwurf vom Projektinhaber bestätigt („mach alles fertig“), **alle Schritte umgesetzt**. Was davon abweicht, steht unter „Stand der Umsetzung“ am Ende. Löst `docs/AGENT-API.md` ab: Beispiel-Agent, MCP-Server und API-Schlüssel entfallen, es bleibt nur der Assistent in der Weboberfläche.

Vom Projektinhaber am 22. Oktober 2026 entschieden:

- MCP-Server, API-Schlüssel und Agent-App werden **entfernt**.
- Chats werden **auf einem Volume** gespeichert, nicht auf dem Stick.
- **Alles**, was die Oberfläche kann, darf der Assistent vorschlagen, ausgeführt wird **nur nach Bestätigung**, gefährliche Aktionen mit derselben Bestätigung wie in der Oberfläche.
- **Benutzer** dürfen den Assistenten auch nutzen, eingeschränkt auf das, was ihre Rolle sieht.

## Ziel

Ein Chat in der Weboberfläche, der das NAS kennt, Fragen mit echten Werten beantwortet, die TitanOS-Doku und auf Wunsch das Web durchsucht und Dinge erledigt, sobald ein Mensch zustimmt. Das Modell läuft in Ollama (meist als App auf diesem NAS); titand spricht mit Ollama, nicht der Browser.

## Grundsätze

- **Ab Werk aus.** Einstellungen › KI-Assistent schaltet ihn ein.
- **Rechte des angemeldeten Kontos.** Der Assistent sieht und tut nie mehr als das Konto selbst in der Oberfläche.
- **Keine Aktion ohne Klick.** Das Modell schlägt vor, der Mensch führt aus. Es gibt keinen Modus „ohne Rückfrage“: Protokolle, Containernamen, Dateien und Webseiten können Text enthalten, der sich an das Modell richtet. Ein solcher Text kann höchstens eine Karte erzeugen, die jemand ablehnt.
- **Ausgeführt wird im Browser über die normale API.** Der Klick auf „Ausführen“ ruft dieselbe Route mit dem Cookie des Kontos auf wie der entsprechende Knopf der Oberfläche. Rechte, Prüfungen, Sperren (`update_working`, Platten in Prüfung …) und Protokoll sind damit dieselben; titand bekommt keinen zweiten Weg für Aktionen.
- **Geheimnisse sieht das Modell nie:** keine `.env`-Werte, Backup-Passwörter, Zugangsdaten von Zielen, Wiederherstellungscodes, privaten Schlüssel. Protokolle laufen weiter durch `diagnose.Redactor`.
- **Was Werkzeuge liefern, ist Daten.** Der Systemprompt sagt das, und jedes Werkzeugergebnis wird als solches gekennzeichnet.

## 1. Aufräumen

Entfällt:

- `examples/agent/`, `.github/workflows/agent.yml`, `docs/apps-repo/titanos-agent/` und ihr Abschnitt in `docs/apps-repo/README.md`.
- `POST /api/v1/mcp`, Paket `mcp`, Schalter „API-Schlüssel und Agenten erlauben“ unter Einstellungen › Zugang, „API-Schlüssel“ unter Mein Konto, `auth.APIKey` samt Bereichen, Bearer-Prüfung und Tests.
- `docs/AGENT-API.md` (durch diese Datei ersetzt), Abschnitt in `CLAUDE.md` und Eintrag in `README.md` angepasst.

Die Werkzeuge aus `api/mcp.go` ziehen ins Paket `assistant` (`assistant/tools*.go`); sie werden direkt aufgerufen, nicht über HTTP.

Alte Dateien auf vorhandenen Sticks (`/config/system/api.json`, `/config/users/<konto>/api-keys.json`) werden nicht mehr gelesen und nicht gelöscht: Nichts bleibt offen, und ein Rückwechsel auf 0.9.x findet sie noch vor.

Außerhalb dieses Repos, von Hand: das Paket `ghcr.io/ra5on/titanos-agent` auf GitHub und der Ordner `titanos-agent` in `ra5on/TitanOS-Apps`.

## 2. Chat

### Ablauf

- **Streaming:** titand fragt Ollama mit `"stream": true` und reicht Teile als SSE-Ereignis `delta` weiter. Werkzeugaufrufe erscheinen als `tool` (Name, Argumente, Ergebnis gekürzt), die Antwort endet mit `done`.
- **Stopp-Knopf** an Stelle von „Senden“, solange eine Antwort läuft (`AbortController`; titand bricht die Anfrage an Ollama über `ctx` ab). Was bis dahin kam, bleibt mit dem Vermerk „abgebrochen“ stehen.
- **Denken:** Inhalt von `<think>` bzw. dem Feld `thinking` eingeklappt über der Antwort statt weggeschnitten. Er geht nicht zurück an das Modell.
- **Neu generieren** für die letzte Antwort, **eigene Nachricht bearbeiten** (alles danach entfällt), **Antwort kopieren**.
- Höchstens 8 Werkzeugschritte je Antwort bleiben; der Hinweis bei Überschreitung kommt aus den Übersetzungsdateien.

### Darstellung

- `chatMarkdown` lernt Codeblöcke, Tabellen, Listen und Links – Links **nur** auf Pfade der Oberfläche aus einer festen Liste (`/storage`, `/shares`, `/docker/apps/<name>`, `/vms?vm=<name>`, `/settings/…`, `/tools`, …). Alles andere bleibt Text. Quellen der Websuche erscheinen getrennt unter der Antwort als externe Links (`rel="noopener noreferrer"`, Domain sichtbar).
- Jeder Werkzeugschritt ist aufklappbar und zeigt Argumente und das, was das Modell gelesen hat.

### Orte

- **Seitenleiste** wie heute, Breite ziehbar, pro Konto gemerkt.
- **Eigene Seite `/assistant`** mit Chatliste links (auf dem Smartphone die einzige Ansicht).
- **Knöpfe:** Sprechblase in der Kopfzeile bleibt; dazu ein schwebender Chat-Knopf unten rechts (abschaltbar unter Darstellung), Kürzel Strg+J.

### Seitenkontext

Jede Frage schickt die aktuelle Route mit (`/vms?vm=win11`, `/docker/apps/immich`). titand macht daraus eine Zeile im Systemprompt („Die Person sieht gerade die VM *win11*“). So reichen Fragen wie „Warum startet die nicht?“. Abschaltbar je Chat.

### Erklären-Knöpfe

„Fragen“ (Sprechblase) an Meldungen in der Glocke, Fehlerzeilen unter Werkzeuge › Protokoll, fehlgeschlagenen Backup-Läufen, gescheiterten App-Installationen und Fehlern von VMs. Öffnet einen neuen Chat mit vorbereiteter Frage und dem Gegenstand als Kontext.

### Anhänge

- Textdatei aus dem Dateimanager (bis 256 KiB, Rechte des Kontos, Auswahl über die Ordnerauswahl).
- Protokollausschnitt (aus der Protokollansicht „An Assistenten“).
- Bild, nur wenn das Modell Bilder versteht (`/api/show` meldet `vision`), bis 5 MB.

## 3. Gespeicherte Chats

- Ablage `/mnt/<volume>/.titanos/assistant/<konto>/<id>.json`, Ordner nur für root (0700). Volume: in den Einstellungen gewählt, sonst das erste eingebundene – wie bei der Sicherung des Sticks.
- Ein Chat: `id`, `title` (aus der ersten Frage, umbenennbar), `created`, `updated`, `model`, `messages` (mit Werkzeugschritten und Aktionskarten samt Ergebnis). Höchstens 200 Chats je Konto, 2 MB je Chat; die ältesten verschwinden zuerst (mit Hinweis).
- Geschrieben wird atomar nach jeder fertigen Antwort. Nie auf dem Stick, nie im RAM-Overlay.
- Ohne eingebundenes Volume: Chat funktioniert, Verlauf nur im Tab (sessionStorage) wie heute, mit Hinweis.
- Liste mit Suche (Titel und Text), Umbenennen, Löschen (mit Bestätigung), „Alle löschen“. Administratoren sehen **nicht** die Chats anderer Konten. Ein gelöschtes Konto nimmt seine Chats mit.
- Nicht im Diagnosepaket und nicht in der Sicherung des Sticks; Backups erfassen sie nur, wenn jemand `.titanos` ausdrücklich wählt (heute nicht möglich).
- API: `GET/POST /api/v1/assistant/chats`, `GET/PUT/DELETE /api/v1/assistant/chats/<id>`.

## 4. Rechte

| | Besitzer / Administrator | Benutzer |
|---|---|---|
| Chat, Doku | ja | ja |
| Lesen | alles | System allgemein (Name, Version, Speicherplatz der eigenen Freigaben), eigene Freigaben und Dateien, eigene Meldungen nicht (sie sehen die Glocke nicht) |
| Docker, VMs, Platten, Protokolle, Netzwerk, Backups | ja | nein |
| Aktionen | alles aus Abschnitt 5 | nur im eigenen Bereich: Ordner anlegen, Datei umbenennen/verschieben/löschen in eigenen Freigaben |
| Websuche | wenn eingeschaltet | wenn eingeschaltet **und** für Benutzer erlaubt |

Einstellungen › KI-Assistent: „Benutzer dürfen den Assistenten nutzen“ (ab Werk aus), „Websuche“ (aus), „Websuche auch für Benutzer“ (aus). Die Werkzeugliste, die das Modell bekommt, wird je Anfrage aus der Rolle gebaut: ein Werkzeug, das die Rolle nicht darf, kennt das Modell gar nicht. Zusätzlich prüft jedes Werkzeug die Rolle selbst.

## 5. Aktionen

### Ablauf

1. Das Modell ruft ein Aktionswerkzeug auf, z. B. `container_action {"name": "jellyfin", "action": "restart", "reason": "antwortet nicht"}`.
2. titand prüft, ob die Aktion für dieses Konto bekannt und erlaubt ist, und beendet die Runde mit dem SSE-Ereignis `action`: Art, Ziel, Grund, Route, Methode, Inhalt und Bestätigungsstufe. Es führt **nichts** aus.
3. Der Browser zeigt eine **Aktionskarte** im Chat: „Container **jellyfin** neu starten – Grund: antwortet nicht“. „Ausführen“ (`.btn-warn` bzw. `.btn-danger`, links) und „Abbrechen“; Enter und Autofokus lösen nichts aus.
4. Nach dem Klick ruft der Browser die normale API-Route auf. Das Ergebnis (OK oder Fehlercode mit Text) geht als Werkzeugergebnis in die nächste Anfrage, und das Modell antwortet weiter („jellyfin läuft wieder seit 3 Sekunden“).
5. „Abbrechen“ meldet dem Modell „abgelehnt“. Eine Karte gilt nur in ihrem Chat und bis zur nächsten eigenen Nachricht; danach ist sie ausgegraut.

Stammt etwas in der Runde aus der Websuche, einer Webseite, einer Datei oder einem Protokoll, trägt die Karte den Hinweis „Vorschlag entstand nach dem Lesen von …“.

### Bestätigungsstufen

| Stufe | Bestätigung | Beispiele |
|---|---|---|
| normal | Klick | Container/Stack/App starten, stoppen, neu starten, aktualisieren; VM starten, herunterfahren, neu starten; Backup jetzt; Volume prüfen; Plattentest „nur lesen“; Ordner anlegen; Freigabe anlegen oder Rechte ändern; Meldungen als gelesen |
| gefährlich | Klick + Häkchen mit Folgen | VM ausschalten erzwingen; App entfernen (Daten bleiben); Dateien löschen in den Papierkorb; Netzwerk ändern (läuft wie immer 2 Minuten auf Probe); Update installieren; NAS neu starten oder herunterfahren (Dialog aus dem Konto-Menü mit allen Hinweisen) |
| zerstörerisch | Namen eintippen bzw. Passwort, genau wie in der Oberfläche | Volume anlegen oder entfernen; Platte überschreiben; App samt Daten entfernen; VM samt Platten entfernen; Dateien endgültig löschen; Freigabe samt Daten löschen; Konten anlegen, Rolle ändern, löschen |

Die Stufe legt titand fest, nie das Modell. Wo die Oberfläche zusätzliche Angaben verlangt (Passwort bei Konsolen, Name bei Löschen), verlangt die Karte dieselben.

### Bleibt ausgeschlossen

Auch mit Bestätigung nicht über den Assistenten: Terminal und Konsolen öffnen, Geheimnisse anzeigen (Backup-Passwort, `.env`, Wiederherstellungscodes, Kopplungscodes), Sticksicherung herunterladen, Plugins installieren, Zertifikat und SSH-Schlüssel ändern, eigenes Passwort und 2FA. Hier verlinkt der Assistent die Seite.

### Protokoll

Jede ausgeführte Aktion steht im Journal von titand wie ein Klick in der Oberfläche, zusätzlich mit „über den Assistenten, Chat <id>“.

## 6. Werkzeuge

Lesen (bestehend): `system_status`, `notifications`, `update_status`, `ups_status`, `storage_overview`, `shares_list`, `docker_list`, `container_logs`, `vm_list`, `backup_status`, `logs_search`.

Lesen (neu):

- `disk_details` – SMART-Werte, Temperatur, Fehlerzähler, Ruhezustand je Platte (`--nocheck=standby`, weckt nichts auf).
- `network_status` – Anschlüsse, Adressen, Bond/VLAN/Bridge, Tailscale.
- `hardware` – GPUs, NPU, IOMMU-Gruppen, Zuordnung, Plugins.
- `disk_tests`, `file_jobs` – laufende Plattentests und Kopieraufträge.
- `container_stats` – CPU/RAM je Container.
- `files_list`, `file_read` – Ordner und Textdateien unterhalb der Wurzeln des Kontos (`os.Root`, höchstens 256 KiB, nie `.env`, nie `/config/users|tls|ssh|notify|backup`).
- `docs_search`, `docs_read` – die TitanOS-Doku, als Markdown im Abbild unter `/usr/share/titanos/docs/` (README, `docs/*.md`, Hilfetexte der Seiten). Damit stimmen Antworten auf „Wie richte ich … ein?“.
- `web_search`, `web_read` – siehe Abschnitt 7.

Werkzeuge liefern kompakte Zusammenfassungen und nehmen Filter (`volume`, `container`, `since`, `level`), statt große JSON-Blöcke abzuschneiden.

Aktionen: je Bereich ein Werkzeug (`container_action`, `app_action`, `vm_action`, `backup_action`, `volume_action`, `disk_action`, `share_action`, `file_action`, `account_action`, `network_change`, `system_action`, `update_action`), jeweils mit Pflichtfeld `reason`. Jedes übersetzt seine Argumente in genau eine bestehende API-Route.

## 7. Websuche

- Ab Werk aus. Quelle: DuckDuckGo über die HTML-Seite (`https://html.duckduckgo.com/html/`, POST, ohne Schlüssel und ohne Konto). Höchstens 10 Treffer (Titel, Adresse, Auszug). DuckDuckGo hat keine offizielle Such-API; ändert sich die Seite, meldet das Werkzeug „Suche nicht verfügbar“ statt falscher Treffer.
- `web_read` lädt eine Seite und gibt ihren Text zurück (höchstens 2 MB laden, 20.000 Zeichen Text). Nur `http`/`https`, höchstens 5 Weiterleitungen, **jede Zieladresse wird vor dem Verbinden aufgelöst und abgelehnt**, wenn sie privat, Loopback, Link-Local, CGNAT/Tailscale, Multicast oder eine Adresse dieses NAS ist. So lässt sich über den Assistenten weder das LAN abtasten noch titand selbst erreichen.
- User-Agent `TitanOS/<version>`, keine Cookies, keine Daten des NAS in Suchanfragen außer dem, was das Modell als Suchbegriff wählt. Die Karte der Websuche zeigt den Suchbegriff.
- Treffer und Seiten sind Daten (siehe Grundsätze); Quellen erscheinen unter der Antwort.

## 8. Einstellungen (Einstellungen › KI-Assistent)

- Ein/Aus, Ollama-Adresse, Standardmodell; Modell je Konto im Chat wählbar und gemerkt (`/preferences/assistant`).
- **Ollama finden:** Läuft eine App mit dem Image `ollama/ollama`, schlägt die Seite ihre Adresse vor.
- **Modelle:** Liste mit Größe und Fähigkeiten aus `/api/show` (Werkzeuge, Bilder, Denken). Modelle ohne Werkzeuge sind gekennzeichnet und nicht wählbar. „Modell laden“ (`/api/pull` mit Fortschritt) und „Entfernen“ (mit Bestätigung).
- Benutzer erlauben, Websuche, Websuche für Benutzer, Volume für Chats, eigener Zusatz zum Systemprompt (höchstens 2000 Zeichen).
- Optional `num_ctx`, `keep_alive`, `temperature`; leer = was Ollama eingestellt hat.

## Umsetzung in Schritten

1. **Aufräumen:** Agent-App, MCP, API-Schlüssel entfernen; Werkzeuge ins Paket `assistant`.
2. **Chat:** Streaming, Stopp, Denken, Werkzeugdetails, Neu generieren/Bearbeiten/Kopieren, Markdown mit internen Links, Seite `/assistant`, schwebender Knopf, Seitenkontext.
3. **Gespeicherte Chats** auf dem Volume.
4. **Rechte** je Rolle, neue Lese-Werkzeuge, Doku im Abbild.
5. **Aktionen** mit Karten und Bestätigungsstufen.
6. **Websuche.**
7. **Erklären-Knöpfe, Anhänge, Modellverwaltung.**

Jeder Schritt ist ein eigener PR mit Tests: Rechte je Rolle (Werkzeugliste und Prüfung im Werkzeug), keine Ausführung ohne Klick, Stufe kommt von titand, Abwehr privater Adressen in `web_read` (auch nach Weiterleitung und DNS-Antworten mit mehreren Adressen), Chats anderer Konten unerreichbar, nichts auf dem Stick.

## Stand der Umsetzung

Umgesetzt wie oben, mit diesen Abweichungen und Festlegungen:

- **Werkzeuge** liegen im Paket `api` (`assistant_registry.go` für Liste und Rollen, `assistant_tools.go` für die Lese-Werkzeuge, `assistant_actions.go` für die Aktionen), weil sie die Manager von titand brauchen. Das Paket `assistant` enthält Ablauf, Ollama, Chats, Websuche und Handbuch.
- **Ohne eingebundenes Volume** liegen Chats im Speicher von titand (nicht im sessionStorage des Browsers), bis titand neu startet. Ein in den Einstellungen gewähltes Volume, das gerade nicht eingebunden ist, ergibt ebenfalls Chats im Speicher – nie ein anderes Volume.
- **Bestätigung „zerstörerisch“** ist immer der eingetippte Name (Volume, App, VM, Platte, Konto, Datei), nie ein Passwort. Wo die Route selbst einen Namen verlangt (`confirm` im Inhalt, `deleteData` in der Adresse), setzt der Browser das Eingetippte dort ein.
- **Nicht als Aktion umgesetzt:** Netzwerk ändern (das Formular mit Probezeit ist zu vielschichtig für einen Vorschlag; der Assistent erklärt und verlinkt die Seite), Konten anlegen (das Modell soll keine Passwörter wählen), Freigaben bearbeiten (nur anlegen und entfernen).
- **Weitere Aktionen:** `apps_update_all`, `docker_check_updates`, `stick_backup` (Sicherung des Sticks jetzt), `update_action` mit `check`, `cancel` und `app_rollback`.
- **Benutzer** bekommen Aktionen im eigenen Bereich (Ordner anlegen, umbenennen/verschieben, löschen in Freigaben mit Schreibrecht) und die Werkzeuge `nas_info` und `my_shares` statt der Systemwerkzeuge.
- **Schwebender Knopf** lässt sich in der Chatliste (nicht unter Darstellung) je Konto abschalten. Breite der Seitenleiste, gewähltes Modell und „Aktuelle Seite mitschicken“ stehen in `/preferences/assistant`.
- **Websuche** verbindet sich nur mit den Ports 80 und 443. DuckDuckGo-HTML ist keine offizielle Schnittstelle; eine eigene SearXNG-Quelle wäre die Alternative, falls sie sich ändert.
- **Handbuch:** Der Web-Build kopiert README und `docs/*.md` nach `web/static/manual` (nicht in Git); SvelteKit legt sie zu den Seiten, titand liest sie aus dem eingebetteten Dateisystem. Im Entwicklungsmodus ohne Build gibt es kein Handbuch.
- **Fragen-Knöpfe** an Meldungen der Glocke, Fehler- und Warnzeilen des Protokolls („?“ je Zeile, „Assistent fragen“ für den sichtbaren Ausschnitt), fehlgeschlagenen Datensicherungen und App-Einrichtungen; „Dem Assistenten zeigen“ im Kontextmenü des Dateimanagers für Textdateien und Bilder. Was so angehängt wird, ist nicht geschwärzt (die Einstellungsseite sagt das).
- **Nicht umgesetzt:** Anhängen über die gemeinsame Ordnerauswahl (stattdessen Hochladen vom Rechner und das Kontextmenü des Dateimanagers), Bildschirmfotos einer VM als Anhang, Anzeige der Fähigkeit „Denken“ im Chat selbst.
- **Getestet:** Unit- und API-Tests (Ablauf mit nachgebautem Ollama, Streaming, Stopp, Aktionen, Rechte je Rolle, fremde Chats, Ablage auf Platte, Abwehr privater Adressen, DuckDuckGo-Parser, Handbuch, Markdown). Im Browser mit `titand -dev` und einem nachgebauten Ollama: Einstellungen, Modellliste mit Fähigkeiten, Laden mit Fortschritt, Chat mit Streaming und Gedanken, Karte „NAS neu starten“ bis zur Ausführung, Seite `/assistant` auf Desktop und Smartphone, echte Websuche bei DuckDuckGo. Nicht geprüft: ein echtes Ollama-Modell, Aktionen gegen echte Volumes/Container/VMs, Benutzer-Rolle im Browser.

## Ausbau zum Agenten (nach 0.10.0)

Vom Projektinhaber am 22. Oktober 2026 entschieden: nur noch die große Seite statt Seitenleiste, Aufträge mit einmal freigegebenem Plan, Dateien in Word, Excel, PDF und Text, „alles außer Geheimnissen“, Runden einstellbar oder unbegrenzt.

- **Eine Oberfläche:** Die Seitenleiste ist entfallen. Sprechblase in der Kopfzeile, runder Knopf und Strg+J öffnen die Seite `/assistant`; die Seite, von der man kam, geht als Kontext mit.
- **Runden:** `maxSteps` in den Einstellungen (0 = unbegrenzt, höchstens 500 als Sicherung). Wiederholt das Modell einen Aufruf mit gleichen Argumenten, bekommt es den Hinweis statt eines zweiten Ergebnisses; nach drei Wiederholungen oder an der Grenze antwortet es ohne Werkzeuge mit dem Gefundenen, statt verworfen zu werden. Eine leere Antwort zeigt einen Hinweis.
- **Rechte je Bereich** (`areas`): Dateien, Docker und Apps, VMs, Speicher, Freigaben, Backups, Konten, System und Einstellungen, Updates und Neustart – je „nichts“, „nur lesen“ oder „lesen und ändern“. Ab Werk ändern nur in Dateien, Docker, VMs, Speicher, Freigaben und Backups; Konten, System und Updates nur lesen. Die Werkzeugliste folgt den Bereichen; Benutzer erreichen weiter nur ihre Freigaben.
- **Aufträge:** `start_task` schlägt einen Plan vor (Titel und Schritte). Wird er freigegeben, sind die folgenden Aktionen des Auftrags `auto`: Der Browser führt sie ohne Karte aus und meldet das Ergebnis. Zerstörerisches fragt weiter einzeln. Der Auftrag endet mit der Antwort des Modells, einer neuen Nachricht oder Stopp.
- **Alles außer Geheimnissen:** `api_routes`, `api_read`, `api_change` über eine feste Liste von Routen der Oberfläche mit Bereich und Stufe (`api/assistant_api.go`). `api_read` ruft die Route intern mit der Anmeldung des Chats auf. Nicht in der Liste: Backup-Passwörter, Herunterladen der Sticksicherung, Konsolen, Zertifikat (privater Schlüssel), Bestätigen einer Netzwerkänderung (bleibt beim Menschen), Konten anlegen (Passwort). Ergebnisse von Aktionen und `api_read` laufen durch `diagnose.Secrets`.
- **Dateien:** `file_write` schreibt Text, Markdown, CSV, JSON, Word (.docx) und PDF aus Markdown und Excel (.xlsx) aus Blättern oder CSV (Paket `office`, nur Standardbibliothek); fehlende Ordner entstehen, Ersetzen nur ausdrücklich, die alte Fassung bleibt als `.prev` (`files.WriteFile`). `files_move` verschiebt bis zu 500 Einträge in einer Wurzel und legt Ordner an, ersetzt nie. `file_copy` nutzt die Kopieraufträge des Dateimanagers. `files_search` sucht nach Namen. `file_read` liest auch Word und Excel; PDF nicht.
- **Dateien vom NAS anhängen:** Knopf „Datei vom NAS anhängen“ im Chat; das NAS liest die Dateien selbst mit den Rechten des Kontos (Text, Word, Excel, Bilder).
- **Getestet:** Go-Tests für Runden, Wiederholungen, Bereiche, Katalog (jede Route existiert, keine geheime), `api_read` mit fremder Adresse und Geheimnis-Route, Aufträge, Dokumente über die echten Routen mit vorgetäuschtem Volume, Anhänge vom NAS; Word, Excel und PDF zusätzlich mit LibreOffice und `pdftotext` geprüft. Im Browser: Bereichstabelle, Sprechblase öffnet die Seite, Auftrag mit Plan-Freigabe und automatisch ausgeführtem Schritt. Nicht geprüft: echtes Modell, Dateiaufträge im Browser (der Entwicklungsserver hat kein Volume).
