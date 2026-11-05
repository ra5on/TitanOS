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

- **Aussehen (PLAN 48):** Handy und Desktop sehen gleich aus, angelehnt an die großen Chat-Apps.
  - **Agent-Menü:** Suche, „Neuer Chat“, Projekte (Klick klappt ihre Chats auf), dann Angeheftet und die Chats nach Tagen. Laufende Chats leuchten, wartende zeigen „Freigabe“, ungelesene einen Punkt (auch bei zugeklapptem Projekt). Unten Wissen, Darstellung und Einstellungen (nur Admins).
  - **Kopfzeile:** in der Mitte Modell und Denkstufe; Antippen öffnet die Auswahl (Desktop als Fenster unter dem Knopf, Handy als Blatt von unten). Im Chat stehen dort Titel, Projekt und Modell. Rechts Anheften (Desktop), Vollbild (Desktop), **⋯** (Umbenennen, In Projekt verschieben, Export Markdown/PDF, Löschen; am Handy auch Anheften) und ✎ Neuer Chat.
  - **Start = neuer Chat:** Begrüßung mit leuchtendem Agent-Symbol (atmet, solange ein Auftrag läuft), Projekte als Kacheln (Antippen öffnet das Projekt), laufende und wartende Aufträge mit Stopp-Knopf, vier Vorschläge, die das Eingabefeld füllen.
  - **Eingabefeld:** eine Pille mit Büroklammer, Text und Senden-Knopf, Glas mit Leuchtrand. Auf dem Handy 16 px Schrift, damit iOS nicht heranzoomt.
  - **Effekte:** Glas, Leuchten, Verlaufsschrift, weiche Übergänge; „Leuchten“ und „Weniger Bewegung“ unter Darstellung schalten sie ab (auch die Systemeinstellung „Bewegung reduzieren“).
- **Desktop:** Links bleibt das TitanOS-Menü, daneben das Agent-Menü, rechts der Chat (höchstens etwa 800 px breit, mittig). **Vollbild** in der Kopfzeile blendet das TitanOS-Menü aus, **Mit Menü** wieder ein; die Wahl gilt je Gerät (`localStorage`, PLAN 47).
- **Handy:** Der Agent ist immer eine eigene Ansicht ohne TitanOS-Rahmen (PLAN 48). ☰ oben links (oder Wischen vom linken Rand) öffnet das Agent-Menü, Wischen nach links schließt es. Ganz unten im Menü führt **„Zurück zu TitanOS“** zum Dashboard.
- **Home-Bildschirm:** Zwischen TitanOS und dem Agenten lädt die Seite neu. Safari liest das Manifest nur beim Laden. So legt „Zum Home-Bildschirm“ auf `/agent` das Agent-Symbol an und überall sonst das von TitanOS, auch wenn man über das Menü hingekommen ist.
- **Eigene App:** Auf `/agent` „Zum Home-Bildschirm“ (iOS) bzw. „App installieren“ (Chrome/Edge). Die App hat ein eigenes Symbol und Manifest (`/agent.webmanifest`, Start `/agent?app=1`) und zeigt nie die TitanOS-Navigation.
- **Projekte** haben eigene Anweisungen und einen Standard-Ablageort.
- **Schritte:** Jeder Werkzeugaufruf steht im Chat, zusammengefasst als „n Schritte“. Die Gedanken des Modells lassen sich aufklappen. Der runde Knopf hält den Auftrag an.
- **Rückfragen:** Passt ein Name auf mehreres (Home Assistant als Docker-App und als VM), fragt der Agent mit Knöpfen.
- **Darstellung:** Akzent, Chat-Stil (Blasen/Klar/Kompakt), Glas, Leuchten, Schreibanimation, Vibration (nur Android), weniger Bewegung. Gespeichert nur im Browser.

Aufträge laufen auf dem NAS weiter, auch wenn die Seite zu ist. Ist er fertig, wartet er auf dich oder gab es einen Fehler, kommt eine Push-Mitteilung (Web Push); sie öffnet nur den Chat und gibt nie etwas frei.

**Gelesen-Stand und Mitteilungen (PLAN 48, Entscheidung des Inhabers vom 5.11.2026):**
- Solange ein Chat auf einem Gerät sichtbar offen ist, meldet die Seite das alle 15 Sekunden (`POST /api/v1/agent/chats/{id}/seen`). Der Chat gilt dann auf allen Geräten des Kontos als gelesen.
- Endet ein Lauf, während der Chat irgendwo sichtbar ist (Meldung jünger als 40 Sekunden), geht **keine** Mitteilung raus, auch nicht bei Freigabe oder Fehler. Sonst geht sie sofort raus, ohne Wartezeit.
- Je Chat höchstens eine ungelesene Mitteilung: Solange die letzte nicht angesehen wurde, kommt keine weitere (z. B. bei mehreren Freigaben in einem Lauf). Eine neue ersetzt die alte (gleicher Tag `agent-<chat>`).
- Mitteilungen gelesener Chats verschwinden, sobald TitanOS oder der Agent auf dem Gerät geöffnet wird (Service Worker, `getNotifications`). Sofort beim Lesen auf einem anderen Gerät geht das nicht: iOS erlaubt keinen stillen Push.
- Die Chat-Liste liefert `unread`. Gelesen-Stände liegen in `.titanos/agent/<konto>/seen.json` und werden nur geschrieben, wenn sich der Chat seit dem letzten Ansehen geändert hat; den letzten Stand der Chats hält titand im RAM, die 15-Sekunden-Meldung greift ohne Änderung nicht auf die Platte zu (Ruhemodus). Chats von vor dem Update gelten als gelesen.

## Grundanweisung, Landkarte, Beschreibungen

- **Grundanweisung** (Einstellungen › KI-Agent): gilt in jedem Chat aller Konten, vor allem anderen; mit Vorlage zum Ausfüllen.
- **Meine Anweisung** (im Agenten unter „Gedächtnis, Skills, Geplant“): zusätzlich je Konto.
- **NAS-Landkarte** (im Agenten, Reiter „NAS-Landkarte“): Freigaben mit ihren obersten Ordnern, Apps mit Port, VMs, Systemcontainer, Sicherungen und Räume aus Home Assistant. TitanOS liest sie nachts um 3 Uhr und auf „Neu einlesen“ ein und legt sie auf dem Daten-Volume ab; eine Kurzfassung geht in jeden Auftrag, Einzelheiten holt der Agent mit `nas_map`. Benutzer sehen nur ihre Freigaben.
- **Beschreibungen** zu Freigaben, Ordnern, Apps, VMs, Systemcontainern, Sicherungen und Räumen („Eingescannte Briefe“); Besitzer und Administratoren schreiben sie in der Landkarte, der Agent schlägt selbst welche vor und speichert sie (gekennzeichnet „vom Agenten“).

## Mehr im Chat

- **Anhänge:** Die Büroklammer im Eingabefeld bietet „Vom Gerät hochladen“ (bis 20 MB je Datei, Text, Word, Excel, PDF; größere Dateien werden mit einer Meldung abgelehnt) und „Vom NAS wählen“: eine Dateiauswahl über die Freigaben, die das Konto im Dateimanager sieht (gleiche Rechte, Suche, bis 10 Dateien). Dateien vom NAS werden nicht kopiert, der Agent liest sie mit `file_read` an ihrem Ort (`[Datei: root/pfad]`), auch große. Dasselbe geht im Dateimanager per Rechtsklick „Mit KI-Agent …“. Hochgeladene Anhänge liegen beim Chat und verschwinden mit ihm. Der Titel eines Chats kommt aus der Frage, nicht aus den Dateinamen.
- **Rückgängig** an Schritten, wo TitanOS es kann: ersetzte Datei (alte Fassung zurück, neue in den Papierkorb), gelöschte Dateien in einer Freigabe mit Papierkorb, Schalten in Home Assistant (innerhalb von 30 Minuten).
- **Export** als Markdown oder PDF.
- **Glocke:** „Agent ansehen lassen“ an jeder Meldung. **Dashboard:** Kachel „KI-Agent“ mit laufenden Aufträgen und Eingabefeld.
- **Vorladen:** Beim Öffnen lädt Ollama das Modell schon; wie lange es geladen bleibt, stellt „Modell im Speicher halten“ ein. Neue Modelle lädt „Modell herunterladen“ direkt auf den Ollama-Server.
- **Cluster:** Auf einem verwaltenden Knoten sieht der Agent die anderen Knoten (`cluster_nodes`, `cluster_read`) und startet/stoppt dort Container, VMs und Systemcontainer (`cluster_action`), als dein Konto über den Cluster-Proxy. Das ist bewusst mehr als bei Tokens und MCP (Entscheidung des Projektinhabers vom 5.11.2026).
- **Agent eines anderen Knotens** (PLAN 47, Entscheidung 5.11.2026): Jeder Knoten eines Clusters wählt unter Einstellungen › KI-Agent „Eigener Agent“ oder „Agent von <Admin-Knoten>“. Mit dem Agenten des Admin-Knotens kommt nur das Modell von dort; Chats, Gedächtnis, Skills, Rechte und Ordner bleiben auf dem Knoten, er braucht keine eigene Ollama-Adresse. Der Knoten fragt mit „Freigabe anfragen“ an; der Admin-Knoten meldet das (`agent_cluster_request`) und gibt unter „Andere Knoten im Cluster“ frei, entzieht oder entfernt die Anfrage. Die Anfragen laufen signiert über den Cluster (`/agent/…` der Cluster-Routen, nur Admin-Knoten); weitergereicht werden nur `/api/version`, `/api/tags`, `/api/show`, `/api/ps` und `/api/chat` von Ollama, kein Herunterladen. Modelle lädt man auf dem Admin-Knoten.
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

## Ordner „KI-Agent“ (PLAN 49)

Ist der Agent eingeschaltet, legt TitanOS auf seinem Volume die Freigabe **„KI-Agent“** an (wie `appdata` bei Docker). Jedes Konto, das den Agenten nutzen darf, bekommt darin einen eigenen Ordner, den **nur dieses Konto** sieht: im Netz (SMB, Pfad `…/KI-Agent/%u`) und im Dateimanager (Wurzel `share/KI-Agent`). NFS und Gastzugang gibt es für diese Freigabe nicht; die Konten hält TitanOS selbst aktuell (Feld `perUser` der Freigabe, nur von titand gesetzt). Wer als Administrator ganze Volumes öffnen darf, sieht dort wie überall alle Ordner.

| Ordner | Inhalt |
|---|---|
| `Chats/`, `Projekte/<Name>/` | jeder Chat als Markdown, nach jeder Änderung neu geschrieben; Projekte mit `Anweisungen.md` |
| `Dokumente/` | was der Agent erstellt, wenn nichts anderes gesagt wurde |
| `Wissen/TitanOS/` | mitgelieferte Anleitungen (Docker, Home Assistant, Backups, Fehlersuche, Ordnung und Dokumente, VMs und LXC, Netzwerk und Firewall, Konten/Updates/Plugins, Cluster, Planen); bei jedem Update ersetzt |
| `Skills/TitanOS/`, `Skills/Eigene/` | mitgelieferte und eigene Skills als Lesekopie |
| `Notizen/` | dein Wissen für den Agenten; er bekommt die Dateinamen in jedem Auftrag und liest passende |

- Der maßgebliche Stand (Chats mit Freigaben, Rechte, Gedächtnis) bleibt privat unter `.titanos/agent/`. Die Dateien in der Freigabe sind Abbilder.
- **Bearbeiten wirkt** (Entscheidung des Inhabers vom 5.11.2026): Änderst du eine Chat-Datei, liest der Agent deinen Text vor dem nächsten Auftrag in diesem Chat als Inhalt (Eintrag „In der Datei geändert“). Freigaben, Werkzeugaufrufe und Rechte kommen nie aus der Datei. Der Chat gilt danach als „mit fremdem Inhalt“ (Websuche fragt nach).
- titand schreibt dort als root, aber nur über `os.Root`: ein Link, den jemand in seinen Ordner legt, führt nicht hinaus. Dateien gehören dem Konto (Gruppe `users`).
- Geschrieben wird nur, wenn sich ein Chat geändert hat (letzter Stand aus dem RAM, kein minütliches Lesen der Platten).

## Geplante Aufträge, Erinnerungen, Morgenbericht (PLAN 49)

- Im Chat in normaler Sprache: „Erinnere mich morgen um 9 an den Arzt“, „in 2 Stunden“, „jeden Montag um 8 Gesundheitscheck“, „werktags um 7“. Der Agent bestätigt mit einer Karte (Art, Zeit, nächstes Mal).
- **Arten:** Auftrag (der Agent arbeitet ihn zur Zeit ab, eigener Chat, Ergebnis als Mitteilung) oder **Erinnerung** (nur Mitteilung per Push und Chat-Eintrag „⏰ Erinnerung“, kein Modell-Lauf; kommt immer, auch wenn ein Chat offen ist).
- **Zeiten:** einmalig (Datum und Uhrzeit oder „in N Minuten“), alle N Minuten (ab 5), Stunden (1–168) oder Tage (1–365), stündlich, täglich, an Wochentagen, wöchentlich, monatlich (Tag 1–28), freie Cron-Zeile (fünf Felder) oder Zustand in Home Assistant. Zeitzone des NAS.
- **Verpasst** (NAS aus, titand neu gestartet): wiederkehrende Aufträge laufen nach dem Start einmal nach, wenn der Termin höchstens 12 Stunden her ist, einmalige bis 7 Tage; der Chat heißt dann „… (nachgeholt)“.
- **Übersicht „Geplant“** im Agent-Menü (auch unter „Gedächtnis, Skills, Geplant“): nächster Lauf, Pausieren, Ändern, Jetzt ausführen, Löschen, letztes Ergebnis; Formular mit allen Arten.
- **Morgenbericht** (ab Werk aus, je Konto): oben in „Geplant“ mit Uhrzeit, Wochentagen und Inhalt per Häkchen (NAS-Zustand und Speicher, Backups und Updates, Erinnerungen und Aufträge des Tages, Home Assistant) und eigenem Wunschtext. Er liest nur und ändert nichts. Der Agent legt ihn nicht im Chat an.
- Ältere Versionen ignorieren die neuen Arten (Feld `every` mit unbekanntem Wert); beim Rückweg laufen sie nicht.

## Sprache, Bilder, Vorlesen (PLAN 49/51)

- **Spracheingabe (Whisper):** Einstellungen › KI-Agent › Sprache, Bilder und Sicherheit, ab Werk aus. Adresse eines Whisper-Dienstes mit OpenAI-Schnittstelle (`/v1/audio/transcriptions`), am besten auf dem Ollama-Rechner mit Grafikkarte, und Modellname; „Verbindung prüfen“ liest `/v1/models`. Dann zeigt das Eingabefeld einen Mikrofon-Knopf: Aufnahme im Browser, titand schickt sie an Whisper (`POST /api/v1/agent/transcribe`, höchstens 10 MB, nicht gespeichert), der Text kommt ins Feld. Beispiel für den Ollama-Rechner (Speaches mit CUDA):

  ```yaml
  services:
    whisper:
      image: ghcr.io/speaches-ai/speaches:latest-cuda
      ports: ["8000:8000"]
      environment:
        - WHISPER__TTL=-1
      volumes: ["./hf-cache:/home/ubuntu/.cache/huggingface/hub"]
      deploy:
        resources:
          reservations:
            devices: [{ driver: nvidia, count: all, capabilities: [gpu] }]
  ```

  Modell z. B. `Systran/faster-whisper-large-v3` oder eine turbo-Variante (ca. 1,5–2 GB Grafikspeicher). Image-Name und Variablen vor dem Einsatz in der Doku von Speaches nachsehen.
- **Bilder verstehen:** Sieht das Hauptmodell Bilder (Fähigkeit `vision`, z. B. `gemma4:12b`), bekommt es sie direkt; sonst optional ein eigenes Bild-Modell. Werkzeug `image_view`: Bild aus dem Chat (Büroklammer › „Foto oder Kamera“) oder vom NAS, mit Frage; die Antwort des Modells gilt als fremder Inhalt.
- **Vorlesen:** Lautsprecher-Knopf an jeder Antwort, wahlweise automatisch (Darstellung › „Antworten vorlesen“); Sprachausgabe des Geräts, ohne Server.
- Messung des Inhabers (5.11.2026): `gemma4:12b` mit 256K Kontext belegt 12,6 GB von 16 GB (RTX 5060 Ti); Whisper turbo passt daneben.

## Snapshot vor Änderungen (PLAN 51)

Bevor der Agent eine App aktualisiert oder entfernt (`app_action`) oder Dateien verschiebt, umbenennt oder ersetzt (Gelöschtes landet im Papierkorb der Freigabe), legt TitanOS einen schreibgeschützten Btrfs-Snapshot `agent-JJJJMMTT-hhmmss` des betroffenen Volumes an (höchstens einer je Volume in 10 Minuten, 7 Tage aufbewahrt, nächtlich aufgeräumt). Der Chat zeigt ihn als Eintrag 📸. Einzelne Dateien holt man auf der Speicher-Seite unter Snapshots zurück (Wiederherstellen legt eine beschreibbare Kopie an; das Volume selbst wird nie zurückgesetzt). Volumes ohne Btrfs bekommen keinen, der Chat sagt das. Abschaltbar (`noSnapshots`). VM- und LXC-Snapshots legt der Agent nicht selbst an.

## Geteilte Projekte (PLAN 51)

Im Projekt-Dialog wählt der Besitzer eines Projekts andere Konten des Agenten als Mitglieder. Sie sehen das Projekt (mit dem Namen des Besitzers), seine Anweisungen und alle Chats darin und legen eigene Chats an. Jeder Chat bleibt bei dem, der ihn schreibt: andere lesen ihn nur (kein Weiterschreiben, keine Freigaben), und jeder Lauf hat die Rechte seines Autors. Von fremden Chats sehen Mitglieder **nur Fragen und Antworten**, nicht die Schritte: Werkzeug-Ergebnisse, Argumente, Gedanken, Dateien und wartende Freigaben stammen aus den Rechten des Autors und bleiben bei ihm (`foreignChat`). Für Mitglieder heißt das Projekt `<besitzer>:<id>`; nur der Besitzer ändert oder löscht es.

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
| Einstellungen | `/config/system/agent.json` (Stick, nur beim Speichern); auf dem Admin-Knoten auch die Anfragen anderer Knoten (`shared`) |
| Vollbild ja/nein | `localStorage` des Browsers (`titanos.agent.full`) |
| Chats, Projekte, Gedächtnis, Skills, geplante Aufträge | `.titanos/agent/<konto>/` auf einem Daten-Volume: beim ersten Gebrauch das erste eingebundene, danach fest (`volume` in `agent.json`) |
| Darstellung | `localStorage` des Browsers |
| Gelesen-Stand je Chat | `.titanos/agent/<konto>/seen.json` (nur bei Änderung geschrieben) |
| Stand der Abbilder in der Freigabe | `.titanos/agent/<konto>/mirror.json` |
| Ordner des Kontos | Freigabe `KI-Agent/<konto>/` auf dem Volume des Agenten |
| Snapshots vor Änderungen | `.titanos/snapshots/agent-*` auf Btrfs-Volumes, 7 Tage |

Ohne dieses Volume ist der Agent nicht verfügbar (er weicht nicht still auf ein anderes aus); er speichert nie im RAM oder auf dem Stick. Ein Auftrag, den titand nicht beenden konnte (Neustart, Update), wird beim nächsten Start als „unterbrochen“ markiert. App-Updates warten, solange ein Auftrag läuft.

## Prüfstand (5.11.2026)

- Go-Tests: Ablauf, Freigaben, Rechte, Ordner-Regeln, Speicher, Websuche (mit nachgebautem Ollama und Testservern), API-Rechte, Aktivitätsprotokoll, Assist-Adressprüfung.
- Live gegen Ollama 0.40.14 mit gpt-oss:20b (Fragen ohne Änderungen; `TITANOS_OLLAMA=… go test ./internal/api -run TestAgentLiveOllama`).
- Ereignisstrom (SSE) durch die ganze Handler-Kette, Ordner-Regeln auch über Volume-Wurzeln, Home-Assistant-Regeln in tiefen Argumenten.
- Oberfläche: `scripts/test-ui-agent.mjs` mit Testdaten (Desktop, Handy, eigene App, Einstellungen); Seiten-Crawl gegen `titand -dev` ohne Befund; Einstellungen gegen das echte Ollama über `titand -dev` (ohne Daten-Volume, daher keine Chats).
- Kopfzeile: mit dem 13. Reiter passt bei 1366 px alles, bei 1280 px kommen die Pfeile.
- PLAN 47: `test-ui-agent` prüft zusätzlich ☰ statt rundem Knopf, Suche und „Angeheftet“ auf dem Handy, 16 px im Eingabefeld, Dateiauswahl vom NAS bis zu den Kärtchen, Meldung bei zu großen Dateien, ⋯-Menü, Vollbild hin und zurück (auch nach Neuladen), Einstellungen für Mitglied und Admin-Knoten. Go-Tests: Anhängen wie der Browser (leerer Chat, Upload mit Dateityp, Nachricht, Lesen von Anhang und NAS-Datei), zwei echte API-Server im Cluster mit geteiltem Agenten (keine Freigabe, Anfrage, Freigabe, Chat bleibt auf dem Knoten, kein Herunterladen, Entziehen).
- PLAN 48: `test-ui-agent` prüft das Agent-Menü am Desktop (neben dem TitanOS-Menü, „Freigabe“ beim wartenden Chat) und am Handy (☰, Angeheftet zuerst, Ungelesen-Punkt, Suche, „Zurück zu TitanOS“ lädt das Dashboard), Modell und Denkstufe oben (Fenster bzw. Blatt), Vorschläge füllen das Eingabefeld, Handy ohne TitanOS-Rahmen auch ohne `?app=1`, Neuladen samt richtigem Manifest beim Wechsel TitanOS ↔ Agent in beide Richtungen (auch Zurück/Vor ohne Hin- und Herspringen), die Seen-Meldung des offenen Chats. Go-Test `TestAgentReadStateAndPush`: ungelesen nach einer Antwort, eine Mitteilung bis zum Ansehen, gelesen nach `seen`, keine Mitteilung bei sichtbarem Chat, wieder ungelesen nach neuer Antwort, kein Schreiben ohne Änderung; `TestSeenNeedsNoDiskWithoutChange`: wiederholtes Ansehen öffnet das Volume nicht.
- PLAN 49/51 (5.11.2026): Go-Tests `TestAgentScheduleKinds` (alle Zeitarten, Nachholen, Eingabeprüfung, Morgenbericht-Text), `TestAgentReminder` (Erinnerung aus dem Chat, Bestätigung, Lauf ohne Modell), `TestAgentShareFolders` (Freigabe mit `perUser`, Ordner, Wissen, Kopie des Chats, nur eigener Ordner im Dateimanager, kein Schreiben durch einen gelegten Link, geänderte Datei als Inhalt und `tainted`, kein `perUser` über die API), `TestAgentSharedProject` (Mitglied sieht und liest, schreibt nicht in fremde Chats, eigener Chat mit Anweisungen, Nicht-Mitglied sieht nichts), `TestAgentVoiceAndPictures` (Whisper aus/an, Adressprüfung, Weitergabe, Status; Bild an das Bild-Modell), `TestPerUserShare` (smb.conf mit `%u`). `test-ui-agent`: Geplant mit Morgenbericht und neuer Erinnerung, Mikrofon- und Foto-Knopf, geteiltes Projekt mit Lese-Chat am Handy, Aufgaben-Blatt und Kontoknopf am Handy, Einstellungen Sprache/Bilder/Snapshots.
- **Nicht geprüft (PLAN 49/51):** echter Whisper-Dienst und Mikrofon am iPhone (Safari braucht HTTPS), echtes Bild-Modell, Btrfs-Snapshot auf echter Platte (nur über die vorhandenen Speicherfunktionen), SMB-Zugriff auf `KI-Agent/%u` mit echtem Samba.
- **Nicht geprüft:** geteilter Agent zwischen zwei echten NAS; echtes Home Assistant (Assist über die Ollama-Integration, Konfigurations-API für Automationen, Langzeit-Statistik), echte Push-Mitteilungen (auch das Aufräumen in der Mitteilungszentrale), Installation als App auf echten Geräten (Home-Bildschirm-Symbol des Agenten auf dem iPhone).
