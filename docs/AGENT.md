# KI-Agent

Der KI-Agent erledigt Aufträge auf dem NAS selbstständig. Du gibst ihm eine Aufgabe, er arbeitet sie in mehreren Schritten mit den Werkzeugen von TitanOS ab und meldet sich mit dem Ergebnis. Das Modell läuft auf deinem eigenen Ollama-Server; andere Anbieter gibt es nicht (Entscheidung des Projektinhabers vom 4.11.2026, PLAN 44).

Ab Werk ist der Agent aus. Eingeschaltet wird er unter **Einstellungen › KI-Agent** (nur Besitzer und Administratoren).

## Einrichten

1. Ollama auf einem Rechner im Heimnetz betreiben und ein Modell mit Werkzeugen laden, z. B. `ollama pull gpt-oss:20b`.
2. Einstellungen › KI-Agent: Adresse eintragen (z. B. `http://192.168.10.5:11434`), **Prüfen**, Modell wählen, einschalten, speichern.
3. Den Reiter **KI-Agent** öffnen.

**Denkstufen:** Die Auswahl kommt vom Modell selbst (`thinking.values` aus Ollamas `/api/show`). gpt-oss meldet Niedrig/Mittel/Hoch, andere Modelle nur an/aus oder mehr Stufen. Eine Stufe, die das Modell nicht kennt, wird nicht gesendet; Ollama würde sie sonst still durch seinen Standard ersetzen.

**Kontextlänge:** „Wie in Ollama eingestellt“ schickt keinen eigenen Wert. Die Seite zeigt, womit Ollama das Modell gerade geladen hat (`/api/ps`), und warnt unter 16k. Ollama nimmt ohne Einstellung je nach Grafikspeicher nur 4k. Rund 80 Werkzeuge belegen etwa 6 700 Token (gemessen mit gpt-oss:20b).

## Bedienung

- **Desktop:** links neuer Chat, Suche, Projekte mit ihren Chats, darunter die übrigen Chats nach Tagen; rechts der Chat.
- **Handy:** Startseite mit dem laufenden Auftrag, Projekten als Kacheln und den letzten Chats (Zeile nach links wischen: anheften, Projekt, löschen).
- **Eigene App:** Auf `/agent` „Zum Home-Bildschirm“ (iOS) bzw. „App installieren“ (Chrome/Edge). Die App hat ein eigenes Symbol und Manifest (`/agent.webmanifest`, Start `/agent?app=1`) und zeigt keine TitanOS-Navigation.
- **Projekte** haben eigene Anweisungen und einen Standard-Ablageort.
- **Schritte:** Jeder Werkzeugaufruf steht im Chat, zusammengefasst als „n Schritte“. Die Gedanken des Modells lassen sich aufklappen. Der runde Knopf hält den Auftrag an.
- **Rückfragen:** Passt ein Name auf mehreres (Home Assistant als Docker-App und als VM), fragt der Agent mit Knöpfen.
- **Darstellung:** Akzent, Chat-Stil (Blasen/Klar/Kompakt), Glas, Leuchten, Schreibanimation, Vibration (nur Android), weniger Bewegung. Gespeichert nur im Browser.

Aufträge laufen auf dem NAS weiter, auch wenn die Seite zu ist. Ist er fertig oder wartet er auf dich, kommt eine Push-Mitteilung (Web Push); sie öffnet nur den Chat und gibt nie etwas frei.

## Grundanweisung, Landkarte, Beschreibungen

- **Grundanweisung** (Einstellungen › KI-Agent): gilt in jedem Chat aller Konten, vor allem anderen; mit Vorlage zum Ausfüllen.
- **Meine Anweisung** (im Agenten unter „Gedächtnis, Skills, Geplant“): zusätzlich je Konto.
- **NAS-Landkarte** (im Agenten, Reiter „NAS-Landkarte“): Freigaben mit ihren obersten Ordnern, Apps mit Port, VMs, Systemcontainer, Sicherungen und Räume aus Home Assistant. TitanOS liest sie nachts um 3 Uhr und auf „Neu einlesen“ ein und legt sie auf dem Daten-Volume ab; eine Kurzfassung geht in jeden Auftrag, Einzelheiten holt der Agent mit `nas_map`. Benutzer sehen nur ihre Freigaben.
- **Beschreibungen** zu Freigaben, Ordnern, Apps, VMs, Systemcontainern, Sicherungen und Räumen („Eingescannte Briefe“); Besitzer und Administratoren schreiben sie in der Landkarte, der Agent schlägt selbst welche vor und speichert sie (gekennzeichnet „vom Agenten“).

## Mehr im Chat

- **Anhänge:** Büroklammer im Eingabefeld (bis 20 MB je Datei, Text, Word, Excel, PDF) oder im Dateimanager per Rechtsklick „Mit KI-Agent …“. Anhänge liegen beim Chat und verschwinden mit ihm.
- **Rückgängig** an Schritten, wo TitanOS es kann: ersetzte Datei (alte Fassung zurück, neue in den Papierkorb), gelöschte Dateien in einer Freigabe mit Papierkorb, Schalten in Home Assistant (innerhalb von 30 Minuten).
- **Export** als Markdown oder PDF.
- **Glocke:** „Agent ansehen lassen“ an jeder Meldung. **Dashboard:** Kachel „KI-Agent“ mit laufenden Aufträgen und Eingabefeld.
- **Vorladen:** Beim Öffnen lädt Ollama das Modell schon; wie lange es geladen bleibt, stellt „Modell im Speicher halten“ ein. Neue Modelle lädt „Modell herunterladen“ direkt auf den Ollama-Server.
- **Cluster:** Auf einem verwaltenden Knoten sieht der Agent die anderen Knoten (`cluster_nodes`, `cluster_read`) und startet/stoppt dort Container, VMs und Systemcontainer (`cluster_action`), als dein Konto über den Cluster-Proxy. Das ist bewusst mehr als bei Tokens und MCP (Entscheidung des Projektinhabers vom 5.11.2026).
- **Knopf in Home Assistant:** Ein geplanter Auftrag mit Häkchen „Als Knopf in Home Assistant anbieten“ erscheint dort als Knopf (MQTT-Gruppe „KI-Agent“ unter Einstellungen › Home Assistant).
- **Wochenbericht** (Einstellungen › KI-Agent): montags um 8 Uhr über die Benachrichtigungen, was der Agent geändert hat. Das Protokoll steht auch im Agenten.

## Was der Agent darf

Der Agent nutzt **denselben Werkzeugkatalog wie der MCP-Server**, aber mit eigenen Rechten:

- **Schnellwahl:** Nur lesen · Standard (lesen und Neues anlegen ohne Nachfrage, alles andere mit Nachfrage) · Alles ohne Nachfrage.
- **Bereiche und Werkzeuge:** je Bereich und je Werkzeug „Ohne Nachfrage“, „Nachfragen“ oder „Verboten“. Verbotene Werkzeuge bekommt das Modell gar nicht angeboten; ruft es eines trotzdem, lehnt TitanOS ab.
- **Ordner-Regeln:** je Ordner dieselben drei Stufen; die strengere Regel gewinnt. „Verboten“ blendet den Ordner auch in Listen und Suchergebnissen aus.
- **Nachfragen:** Der Auftrag hält an, im Chat erscheint eine Karte mit „Ausführen/Abbrechen“. „Ausführen“ führt genau den geplanten Aufruf aus, das Modell plant nicht neu. Freigeben kann nur der Besitzer des Chats in seiner Web-Sitzung.
- **Nie:** Passwörter, Tokens, 2FA/Passkeys, Konsolen, Netzwerkbestätigung, die Einstellungen von Agent und MCP. Diese Werkzeuge gibt es im Katalog nicht.
- **Konten:** Besitzer immer; weitere Konten unter Einstellungen › KI-Agent. Jedes Konto hat eigene Chats, Projekte und eigenes Gedächtnis; Benutzer sehen nur ihre Freigaben und haben keine Verwaltungswerkzeuge.

Jede Änderung steht im Aktivitätsprotokoll mit Herkunft **per KI-Agent** (Aufrufe der TitanOS-API über die Handler-Kette; Automationen und Skripte in Home Assistant als eigener Eintrag).

## Ablageorte, Gedächtnis, Skills

- **Ablageorte** geben Ordnern Namen („Home-Assistant-Backups“ → Freigabe Backup, Ordner `Home Assistant`). Freigaben, Apps und VMs kennt der Agent von selbst aus einem kurzen Lagebericht vor jedem Auftrag.
- **Gedächtnis:** Der Agent merkt sich selbst, was du ihm sagst oder korrigierst („mein HA ist die VM“). Einträge sind unter „Gedächtnis, Skills, Geplant“ einsehbar, änderbar und löschbar. Passwörter und Schlüssel werden nie gespeichert.
- **Skills:** mitgelieferte Abläufe (Status eines Dienstes, Gesundheitscheck, Datei finden, Dokument erstellen, Backup-Stand, Speicher aufräumen) und eigene, die der Agent selbst schreibt, wenn sich etwas wiederholt.
- Einträge, die entstanden sind, nachdem der Agent Dateien oder Webseiten gelesen hatte, tragen das Zeichen „aus fremdem Inhalt“.

Das Modell selbst lernt nicht dazu; „Lernen“ heißt: Gedächtnis und Skills gehen in jeden Auftrag mit.

## Internet

Websuche und Seitenlesen über DuckDuckGo (ab Werk an, Bereich „Internet“). Adressen im Heimnetz und das NAS selbst sind gesperrt, auch nach Weiterleitungen. Hat ein Chat schon Dateien, Protokolle oder Webseiten gelesen (oder wurde er durch eine Meldung des NAS gestartet), fragen Websuche und Seitenlesen für den Rest des Chats immer nach, damit nichts vom NAS nach außen gelangt. DuckDuckGo hat keine offizielle Such-API; ändert sich die Seite, meldet der Agent „Suche nicht verfügbar“.

## Geplante Aufträge und Eigeninitiative

- „Mach das jeden Montag um 8 Uhr“ im Chat legt einen **geplanten Auftrag** an (stündlich, täglich, wöchentlich, monatlich oder wenn eine Home-Assistant-Entität einen Zustand erreicht). Jeder Lauf ist ein eigener Chat; das Ergebnis kommt als Mitteilung.
- **Eigeninitiative** (Einstellungen › KI-Agent): Bei Warnungen und Fehlern des NAS sieht der Agent nach und behebt im Rahmen seiner Rechte, je Art höchstens einmal pro Stunde, als Chat „Von selbst: …“.

## Home Assistant

Der Agent nutzt die Verbindung unter Einstellungen › Home Assistant:

- Geräte lesen und schalten, Verläufe, Diagnose (Werkzeuge des MCP-Katalogs). **Eigene Freigabeliste** (Entscheidung des Projektinhabers vom 5.11.2026): Unter Einstellungen › KI-Agent › Home Assistant › „Geräte für den Agenten“ gibst du ganze Bereiche (z. B. `sensor`) oder einzelne Geräte frei – getrennt von „Geräte für MCP freigeben“. Ab Werk ist nichts freigegeben. Die Nur-Lesen-/Dienste-Regeln von MCP gelten für den Agenten nicht; dafür gibt es seine Geräte-Regeln.
- `ha_statistics`: Langzeit-Statistik (z. B. Stromverbrauch pro Tag der letzten Woche).
- `ha_automation_save`, `ha_script_save`: Automationen und Skripte anlegen und ändern, gekennzeichnet „vom TitanOS-Agent“; die vorherige Fassung steht im Ergebnis des Schritts.
- **Geräte-Regeln** (`domain:`, `entity:`, `area:` für Räume, z. B. `area:kueche`): können den Bereich nur strenger machen. Sie gelten für jedes Gerät, das ein Aufruf irgendwo nennt (auch in den Aktionen einer Automation). Schlösser und Alarmanlagen fragen ab Werk nach; eine Automation oder ein Skript ohne erkennbare Geräte fragt immer nach.
- **Mitteilungen** über Notify-Dienste der Home-Assistant-App, zusätzlich zu Web Push.
- **Assist:** In Home Assistant die eingebaute Integration **Ollama** hinzufügen, Adresse `https://<NAS>/agent-ollama`, Modell `titanos-agent`, dann unter Sprachassistenten als Gesprächsagent wählen. TitanOS beantwortet diese Schnittstelle nur, wenn Assist eingeschaltet ist und die Anfrage von einer der Adressen kommt, die unter Einstellungen › Home Assistant als Verbindung eingetragen sind. Bei mehreren Verbindungen hat der Agent je Verbindung eine eigene Geräte-Freigabe (Auswahl über dem Abschnitt), seine Home-Assistant-Werkzeuge nehmen `connection`, Mitteilungen an einen Dienst einer weiteren Verbindung stehen als `<id>/notify.x`. Fragen laufen als das gewählte Konto mit den Rechten des Agenten; dauert etwas länger als 45 Sekunden, antwortet er „Ich kümmere mich darum“ und meldet das Ergebnis.

## Daten

| Was | Wo |
|---|---|
| Einstellungen | `/config/system/agent.json` (Stick, nur beim Speichern) |
| Chats, Projekte, Gedächtnis, Skills, geplante Aufträge | `.titanos/agent/<konto>/` auf einem Daten-Volume: beim ersten Gebrauch das erste eingebundene, danach fest (`volume` in `agent.json`) |
| Darstellung | `localStorage` des Browsers |

Ohne dieses Volume ist der Agent nicht verfügbar (er weicht nicht still auf ein anderes aus); er speichert nie im RAM oder auf dem Stick. Ein Auftrag, den titand nicht beenden konnte (Neustart, Update), wird beim nächsten Start als „unterbrochen“ markiert. App-Updates warten, solange ein Auftrag läuft.

## Prüfstand (5.11.2026)

- Go-Tests: Ablauf, Freigaben, Rechte, Ordner-Regeln, Speicher, Websuche (mit nachgebautem Ollama und Testservern), API-Rechte, Aktivitätsprotokoll, Assist-Adressprüfung.
- Live gegen Ollama 0.40.14 mit gpt-oss:20b (Fragen ohne Änderungen; `TITANOS_OLLAMA=… go test ./internal/api -run TestAgentLiveOllama`).
- Ereignisstrom (SSE) durch die ganze Handler-Kette, Ordner-Regeln auch über Volume-Wurzeln, Home-Assistant-Regeln in tiefen Argumenten.
- Oberfläche: `scripts/test-ui-agent.mjs` mit Testdaten (Desktop, Handy, eigene App, Einstellungen); Seiten-Crawl gegen `titand -dev` ohne Befund; Einstellungen gegen das echte Ollama über `titand -dev` (ohne Daten-Volume, daher keine Chats).
- Kopfzeile: mit dem 13. Reiter passt bei 1366 px alles, bei 1280 px kommen die Pfeile.
- **Nicht geprüft:** echtes Home Assistant (Assist über die Ollama-Integration, Konfigurations-API für Automationen, Langzeit-Statistik), echte Push-Mitteilungen, Installation als App auf echten Geräten.
