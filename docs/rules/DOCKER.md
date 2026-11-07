# Docker und App-Store

Festgelegte Regeln (aus CLAUDE.md). Inhaltlich unverändert aus der Projektdatei `CLAUDE.md` hierher verschoben (30.10.2026); sie gelten weiter verbindlich. Kernregeln stehen in `CLAUDE.md`, das Verzeichnis aller Regeldateien in `docs/rules/README.md`. Neue Regeln dieses Bereichs hier ergänzen. Verweise wie „siehe unten“ oder „siehe ‚Zugang‘“ beziehen sich auf die frühere Gesamtdatei; den genannten Abschnitt über `docs/rules/README.md` finden.

Bereich: Container bearbeiten, Übersicht, App-Store/Katalog, Docker-Layout.

## Docker (aus Abschnitt „Öffentliche API, VM-Medien und Plugin-Aufräumen“)

**Docker: Stand der Umsetzung**
- Umgesetzt: Docker-Engine (Debian-Pakete `docker.io`, `docker-cli`, `docker-compose`) mit allen Daten auf einem gewählten Volume (`/mnt/<volume>/.titanos/docker`); die Engine läuft nur, solange dieses Volume eingebunden ist. Einstellung in `/config/docker/settings.json`.
- Umgesetzt: Übersicht mit Karten, Compose-Stacks, Suche, Filter, Start/Stopp/Neustart, Logs, Entfernen sowie „Compose importieren“. Entfernen behält ab Werk den Datenordner; wahlweise (Häkchen „Auch die Daten der App löschen“, dann nur noch die Rückfrage, kein Eintippen des Namens) löscht es auch `/mnt/<volume>/appdata/<name>`, die Docker-Volumes des Projekts und nicht anderweitig genutzte Images, damit eine Neuinstallation frisch beginnt (`DELETE /api/v1/docker/apps/<name>?deleteData=<name>`). Apps ohne Container (Einrichtung gescheitert) und fehlgeschlagene Einrichtungen zeigt die Docker-Seite mit Starten/Erneut versuchen, Bearbeiten und Entfernen. Eine importierte App liegt in `/config/apps/<name>/compose.yaml`; relative Pfade der Compose-Datei zeigen auf `/mnt/<volume>/appdata/<name>` (`--project-directory`), nie auf den Stick.
- titand spricht die Docker-API direkt über den Unix-Socket (nur Standardbibliothek) und ruft für Apps `docker compose` auf.
- Umgesetzt: Update-Hinweise (Digest des Tags in der Registry gegen das lokale Image, alle 12 Stunden und per Knopf), „Alle aktualisieren“, WebUI-Link über das Label `titanos.webui` (Port, Port/Pfad oder URL), Ports als Links auch in Stack-Zeilen.
- Umgesetzt: Container-Konsole (xterm.js, WebSocket ↔ `docker exec`), nur für Besitzer/Administratoren mit bestehender Anmeldung, ohne zweite Passwortabfrage, Ende nach der unter Einstellungen › Zugang › Konsolen gewählten Zeit ohne Eingabe (5–120 Minuten oder „Nie“, ab Werk 15, PLAN 62-3), im Protokoll vermerkt. Im Desktop-Modus öffnet sie im Terminal-Fenster (seit 0.59.1).
- Container mit dem Label `titanos.sandbox` sind die Wegwerf-Sandboxen des KI-Agenten (`code_run`, PLAN 55, `docker/sandbox.go`). `client.containers` lässt sie aus: Sie erscheinen nirgends als App und werden nie aktualisiert, gestoppt oder gesichert. Nach einem Absturz von titand räumt der nächste Lauf übrig gebliebene weg.
- Umgesetzt: Container-Editor unter `/docker/apps/<name>` (Formular ↔ Compose auf demselben YAML-Text, Kommentare bleiben erhalten): Image, Autostart/Neustart-Richtlinie, Netzwerk, WebUI, Privilegiert, Pfade, Ports, Variablen, Labels mit Live-Prüfungen.
- Umgesetzt: App-Store unter `/apps` (Backend-Paket `catalog`): der offizielle Katalog ist das öffentliche Repository `ra5on/TitanOS-Apps` (je App ein Ordner mit `compose.yaml` im `x-titanos`-Format), fest als nicht entfernbare Quelle „TitanOS-Apps“ (`builtin: true`, ID `titanos`) eingetragen, plus eigene Quellen in `/config/docker/sources.json`. Im Abbild steckt kein Katalog mehr: Ohne Netz beim ersten Start ist der Store leer und zeigt einen Hinweis (kein Fehler); installierte Apps (`/config/apps/<name>/compose.yaml`) laufen unverändert weiter. Die Installationstests der Apps (Immich, Ollama) laufen im Apps-Repo, nicht in der Release-CI; Vorlage dafür in `docs/apps-repo/`. Das Abbild enthält keine AGPL-Vorlagen mehr. Eine Quelle ist ein Repository (Archiv-Download `/archive/HEAD.tar.gz` bzw. GitLab `/-/archive/…`, kein Git im Abbild), ein Archiv oder eine einzelne Compose-Datei; Vorlagen liegen nur im RAM und werden nach dem Start und täglich neu geladen. Installationspanel mit Herkunft pro Feld; TitanOS ergänzt Zeitzone, PUID/PGID (Konto des Installierenden, Gruppe `users`), Appdata-Pfade, Freigaben für `suggest: media|share` und freie Ports. Geheimnisse (`secret: generate`) stehen in `/config/apps/<name>/.env`, die Compose-Datei verweist per `${NAME}` darauf.
- **CPU-Prüfung** (Plan 5h, `hwcheck/cpu.go`): `GET /api/v1/apps/sources/{source}/apps/{app}` liefert `cpuMissing` – Befehlssätze, die die App braucht und die CPU nicht hat (aus `/proc/cpuinfo`, einmal gelesen). Bedarf aus dem optionalen Feld `x-titanos.cpu` (oben oder je Dienst, z. B. `[avx2]`, `x86-64-v3`, `sse4.2`; unbekannte Werte werden übergangen, ältere titand ignorieren es) und der festen Image-Tabelle `knownImages` (MongoDB ab 5 inkl. `latest`, Immich-ML, Frigate: AVX). Das Installationspanel warnt; installieren bleibt möglich. Ohne lesbares cpuinfo keine Warnung.
- `x-titanos`-Schema: `name`, `category`, `description` (Text oder `{de, en}`); `cpu` (optional, Liste benötigter Befehlssätze); `variables.<NAME>`: `label`, `help`, `placeholder`, `required`, `secret: generate`, `fill: timezone|uid|gid`; `paths.<Containerpfad>`: `label`, `suggest`; `ports.<Containerport[/udp]>`: `label`, `webui`.
- Umgesetzt: CasaOS-Stores als Quelle (geprüft an `IceWhaleTech/CasaOS-AppStore` und `bigbeartechworld/big-bear-casaos`, zusammen 668 Vorlagen, alle für amd64). Format: `Apps/<Name>/docker-compose.yml` mit `x-casaos` oben (Titel, Tagline, Kategorie, `main`, `port_map`, `scheme`, `index`, `tips.before_install`, `architectures`) und je Dienst (Beschreibungen zu Variablen, Pfaden, Ports). Das Backend liest daraus nur die Angaben für die Liste; übersetzt wird beim Installieren im Browser (`web/src/lib/compose/casaos.ts`):
  - `/DATA/AppData/$AppID/…` → Datenordner der App (`./…`), andere Ordner unter `/DATA` → Vorschlag einer Freigabe (`suggest: media|share`), sonst ebenfalls im Datenordner. Nichts landet unter `/DATA` im RAM.
  - `$AppID`, `$PUID`, `$PGID`, `$TZ` und Platzhalter wie `[CASAOS_IP]` werden ersetzt; offene Platzhalter (`[EUFY_USERNAME]`) werden zu Pflichtfeldern.
  - `port_map`/`scheme`/`index` → Label `titanos.webui`; Hinweise des Autors werden im Panel angezeigt.
  - App-Symbole aus `x-titanos.icon` bzw. CasaOS werden ohne Referrer geladen, mit Monogramm als Rückfall. Im App-Katalog und Installationspanel bleibt der Hintergrund der Symbole transparent; keine eingefärbten Kacheln hinter den Bildern, Rückfallkürzel nutzen die Theme-Textfarbe. Neue Installationen speichern `titanos.icon`. Siehe `docs/APP-LINKS.md`.
- Die beiden Stores stehen in der Quellenliste als Vorschlag mit „Hinzufügen“. Sie werden nie von selbst eingetragen: Ihre Apps sind von TitanOS nicht geprüft, und der offizielle Store lädt beim ersten Abruf rund 240 MB.
- Für alle Vorlagen gilt: Das Panel nennt erweiterte Rechte (privilegiert, Host-Netzwerk, Docker-Socket, Wurzelverzeichnis, Capabilities) und warnt bei Pfaden außerhalb der Volumes. Geräte, die es auf dem NAS nicht gibt (z. B. `/dev/dri` ohne GPU, Raspberry-Pi-Geräte), werden weggelassen – mit ihnen startet der Container nicht.
- Große Quellen: tar.gz-Archive werden beim Laden durchgereicht und nur die YAML-Dateien behalten (der offizielle CasaOS-Store ist wegen der Screenshots rund 240 MB groß). Beim täglichen Abgleich schickt titand das ETag mit; ein unverändertes Archiv wird nicht erneut geladen.
- Installation und Stack-Editor bieten Image-Tags je Dienst. Ausdrückliche Store-Tags/Digests erhalten, `latest` nur ohne Tag und Digest; Registry-Ports korrekt trennen. Digests/Compose-Ausdrücke bleiben vollständig editierbar. Formular/YAML-Wechsel erhalten bearbeitete Werte und vorhandene Geheimnisse. Das Formular schreibt nur Geändertes zurück (`web/src/lib/compose/service.ts`): unberührte Einträge behalten Kommentare und YAML-Anker, betroffene Aliase werden bei Bedarf unabhängig aufgelöst, damit andere Dienste unverändert bleiben. Compose-Validierung verwendet dieselbe `.env` wie der Start; neue Werte erst nach Prüfung speichern, private temporäre Datei im RAM entfernen.
- Katalog, Icons, Herkunft/Geheim-Schalter, Geräteauswahl und globales Terminal-Panel sind umgesetzt. Image-Updates werden über Registry-Digests erkannt; keine automatische Übernahme geänderter Compose-Vorlagen versprechen. CasaOS-Vorlagen enthalten oft feste Standardpasswörter und feste Containernamen (zweite Installation derselben App scheitert daran).

**App-Formular: Installieren und Bearbeiten** (Entscheidung des Projektinhabers vom 1.11.2026 nach Screenshot-Abstimmung, PLAN 21; ersetzt Installationspanel und Kachel-Editor, Ablauf an Unraid angelehnt, Optik TitanOS)
- Installieren (`/apps/install/<Quelle>/<App>`, eigener Container `/apps/new` aus `GET /api/v1/apps/blank`) und Bearbeiten (`/docker/apps/<name>`) zeigen dasselbe Formular aus gemeinsamen Bausteinen (`web/src/lib/components/appform/`): **eine Spalte**, links die Bezeichnung (rechtsbündig mit Doppelpunkt), rechts das Feld, darunter grau der Hinweis (Container-Pfad, Variable, Volume mit freiem Platz, Herkunft „Vorlage/von TitanOS ergänzt/Eigene“). Keine Schritt-Assistenten, keine Reiter, keine langen Schieberegler, keine Kacheln unterschiedlicher Höhe (ausdrücklich verworfen).
- Schalter **Basis/Erweitert** oben rechts, im Browser gemerkt. Erweitert zeigt zusätzlich: weitere Images, öffentliche URL, CPU-Kerne (Kästchen je Thread → `cpuset`), RAM-Limit (Liste → `mem_limit`), beim Bearbeiten Container-Pfad/Port/Protokoll/Name der Einträge, Labels und „Entfernen“. Unten „Weitere Einstellungen anzeigen (n) …“.
- Abschnitte: „Netzwerk und Zugriff“ (Netzwerk-Modus, LAN-Netz, HTTPS-Name, Tailscale, IPv64, Privilegiert), „Grafikkarte und Geräte“ (GPU-Auswahl als Karten mit Zustand, eigene Geräte `devices`), „Pfade, Ports und Variablen“ (Ordnerwahl mit dem gemeinsamen Ordner-Browser `FolderPicker.svelte`, Port-Prüfung mit „<Port> nehmen“, Geheimnisse verdeckt mit Anzeigen/Kopieren/Neu).
- „+ Weiteren Pfad, Port, Variable, Label oder Gerät hinzufügen“ öffnet einen Dialog mit der Art; Geräte nur unter `/dev`.
- Installieren richtet auf Wunsch gleich den HTTPS-Namen ein (ab Werk an, sofern HTTPS-Namen nicht abgeschaltet sind und die Vorlage einen Port hat; `PUT /docker/proxy/apps/<name>` nach erfolgreichem Start). Danach eine **Protokollseite**: Ordner, Images mit Fortschritt (Bytes aller Ebenen), Geräte, Befehl, Meldungen von Compose, Ergebnis; „Im Hintergrund weiter“, „Fertig“, „<App> öffnen“.
- Backend: Der Auftrag schreibt seine Ausgabe während des Laufs fort (`run.CombinedLines`); vor `up` lädt `docker compose --progress plain pull --policy missing --ignore-buildable --ignore-pull-failures` fehlende Images mit Fortschritt (dasselbe, was `up` sonst still täte, nichts wird ungefragt aktualisiert). Je Image und Ebene bleibt nur die letzte Zeile (`jobLog`), höchstens 8000 Zeichen. App-Updates ziehen weiter mit `pull --quiet`.
- Bearbeiten: geänderte Felder blau markiert, Update-Hinweis am Repository, haftende Leiste Übernehmen/Änderungen verwerfen/Fertig mit Erklärung, „Als Compose-Datei bearbeiten“ statt Umschalter. HTTPS-Name als kompakte Zeile, Felder auf „Ändern/Einrichten“.
- Prüfung: `scripts/test-ui-appform.mjs`, `web/tests/compose-form.test.mjs`, `web/tests/appform.test.mjs`.

**Docker: Container bearbeiten** (wichtigste Seite neben dem Speicher; Aufbau seit 0.43.0 als App-Formular, siehe oben)
- Formular mit klickbaren, einzeln editierbaren Zeilen in Abschnitten mit Kopfleiste und „… hinzufügen“-Knopf:
  - Allgemein: Name, Image, Netzwerk (bridge, host, none; eigene Container-IP im LAN bleibt offen), Neustart-Richtlinie, WebUI-Adresse, Autostart, Privilegiert (mit Warnung).
  - Pfade: Bezeichnung, Pfad auf dem NAS (mit Ordnerauswahl und „Im Dateimanager öffnen“), Pfad im Container, Nur lesen / Lesen und schreiben.
  - Ports: Bezeichnung, NAS-Port, Container-Port, TCP/UDP, Live-Prüfung ob der Port frei ist.
  - Variablen: Bezeichnung + Hinweis, Name, Wert, Markierung Standard / Vorlage / Geheim.
  - Geräte (z. B. `/dev/dri`, erkannte Hardware zur Auswahl) und Labels.
- Umschalter Formular ↔ Compose (YAML). Beide Ansichten bearbeiten dieselbe Datei; Änderungen in einer erscheinen in der anderen.
- Sichtbar angezeigt, wo die Konfiguration liegt: `/config/apps/<name>/compose.yaml`. Geheime Variablen in `/config/apps/<name>/.env`, in der UI verdeckt, nie in Logs oder Exporten.
- Eigene Einträge: In jedem Abschnitt lassen sich neben den Werten aus der Vorlage eigene Pfade, Ports, Variablen und Labels hinzufügen. Jede Zeile zeigt ihre Herkunft („Vorlage“, „TitanOS“, „Eigene“). Eigene Einträge bleiben erhalten, wenn die Vorlage aktualisiert wird.
- Prüfungen live: Port belegt (durch welchen Container/Dienst), doppelt, ungültig; Variablenname gültig (`[A-Za-z_][A-Za-z0-9_]*`) und nicht doppelt; jede Variable per Schalter „Geheim“ in die `.env`.
- „Übernehmen“ erstellt den Container neu; Daten in den Pfaden bleiben erhalten.
- Kopfbereich mit Status, CPU/RAM und Schnellaktionen: WebUI öffnen, Logs, Konsole, Neustarten.

**Docker-Seite: Kopfbereich** (Entscheidung des Projektinhabers vom 1.11.2026, PLAN 22; die Container-Liste bleibt unverändert)
- Eine Kopfzeile: „Docker“, rechts Suche, Liste/Karten als zwei Symbole, Zahnrad, „App installieren“ mit Pfeil.
- Statuszeile darunter = Filter: „Alle n · n laufen · n gestoppt · n Updates“ (Updates nur, wenn es welche gibt), rechts klein CPU/RAM, „Updates geprüft …“ und Symbol „Nach Updates suchen“.
- Zahnrad-Menü: Einstellungen, LAN-Netze, Automatische Updates, Seite anpassen. Pfeil-Menü: aus dem App-Katalog, Compose-Datei importieren, eigener Container.
- Unter der Liste: Alle starten, Alle stoppen (einfache Bestätigung), Nach Updates suchen, Alle aktualisieren (n); daneben Docker-Version und Volume. Start/Stopp laufen nacheinander über die vorhandenen Container-Aktionen.

**Docker-Übersicht und App-Store**
- Container und Compose-Stacks als Liste oder Karten mit persönlicher Reihenfolge (eigene Anordnung oder A–Z): siehe „Docker-Layout“ unten. Das Layout bleibt auf dem bestätigten Stand (Karten: Entscheidung vom 4.11.2026); weitere Änderungen nur auf Auftrag. Beauftragt und umgesetzt (0.27.0): Belegt-Zustand und Aktionsfehler je Container/App statt seitenweit, Container-Menü in der Top-Layer (`ContextMenu.svelte`; öffnet sich per Klick aufs App-Symbol und per Rechtsklick auf Kachel/Karte wie bei den VMs, Wunsch des Projektinhabers vom 30.10.2026; Aufbau gleich auf Docker-Seite und Dashboard: Kopf mit Symbol und Name, Start/Stopp, Adressen mit Ziel, Logs, Konsole, Bearbeiten, Neustarten, Update, Entfernen; nur seitenspezifisch zusätzlich Tailnet bzw. Container anzeigen/Anheften), Logs mit Mitlaufen/Suche/Download, „Alle aktualisieren“ mit Auswahl je App (nacheinander `POST /docker/apps/{app}/update`), App-Editor mit Kopfbereich (Status, CPU/RAM, WebUI/Logs/Konsole/Neustarten) und haftender Leiste Übernehmen/Verwerfen.
- Suche/Filter, Status, CPU/RAM, Ports, WebUI, Start/Stopp und Schnellaktionen bleiben erhalten. Eigene Quellen werden täglich/per Knopf aktualisiert, externe Stores nur ausdrücklich hinzugefügt.
- Installationspanel mit Herkunft je Feld, eigenen Variablen/Pfaden/Ports und erweiterten Rechtehinweisen. Daten auf dem ausgewählten Docker-Volume, niemals im RAM-Overlay; GPU-Nutzung ausdrücklich je Dienst.
- Webadressen über `titanos.webui`, optional `titanos.public-url` und aktiven Tailscale-Host; `titanos.public-url` richtet keinen Tunnel ein. Siehe `docs/APP-LINKS.md` und `docs/NATIVE-APPS.md`.
- **Automatische App-Updates** (seit 0.35.0, `docker/autoupdate.go`, `docs/APP-AUTO-UPDATE.md`, Docker › Automatische Updates): ab Werk für keine App. Je App wählbar, Fenster Wochentage+Uhrzeit (NAS-Zeitzone, 1 h offen, wartet bei Sicherung/VM/Plugin/Speicherumbau). Nur Apps mit neuerem Image: Images merken, pull, `up --pull never`, 3 min beobachten (laufend, nicht unhealthy), sonst alte Image-IDs zurück-taggen und neu starten. Meldungen `docker_auto_updated`/`_update_rolled_back`/`_update_failed`. Daten sind nicht Teil des Rückwegs.
- Eigener HTTPS-Name je App (Entscheidung des Projektinhabers, Paket `appproxy`, `docs/APP-PROXY.md`): titand leitet `https://<name>.<hostname>.local` bzw. `<name>.<domain>` selbst per `httputil.ReverseProxy` (WebSockets, `X-Forwarded-*`) an `127.0.0.1:<Port>` weiter, unbekannte Namen gehen an die Weboberfläche. Einstellungen in eigener Datei `/config/docker/proxy.json` (ältere titand ignorieren sie), API `/api/v1/docker/proxy` nur Besitzer/Admin, Abschnitt „HTTPS-Name“ im App-Editor ohne Neuerstellen des Containers; Übersicht aller Namen unter Einstellungen › HTTPS-Namen für Apps (`/settings/proxy`, seit 0.30.0; Aufbau nach Wunsch des Projektinhabers vom 29.10.2026: Apps und Ziele zuerst, je eine Zeile mit erster Adresse, „+N weitere“ und ⋯-Menü für Bearbeiten/App-Editor/Entfernen, Chips nur für Abweichendes, darunter „Für alle Namen“ und Tailscale-Key, Erklärungen eingeklappt, Hauptschalter im Seitenkopf; Prüfung `scripts/test-ui-proxy.mjs`) mit Hauptschalter, mDNS abschaltbar, eigenem lokalem Namen statt NAS-Name, Domain, je Route weiteren Namen (≤ 8) und eigenen vollen Domains (`domains`, ≤ 8, Feld „Eigene Domains“), Kurzname `<name>.local`, eigenen erlaubten Netzen (≤ 32 CIDR) und optionaler Zertifikatsprüfung zur App, optional je Route ein eigenes Gerät im Tailnet (`tailscale`/`tailscaleName`: eigene `tailscaled`-Instanz je Name im Userspace als transiente systemd-Einheit, Tailscale Serve → titand-Proxy, `ts.net`-Namen nur von Loopback, Auth-Key nur „ist gesetzt“; Zertifikate holt Tailscale selbst; Ein-Klick „Im Tailnet veröffentlichen“/„Aus dem Tailnet nehmen“ im Docker-Kontextmenü, Stack-Kopf und App-Editor, Entscheidung vom 29.10.2026, `appproxy/oneclick.go`, `POST`/`DELETE …/apps/{app}/tailnet`, `…/tailnet/check`: legt fehlende Route mit DNS-Teil des App-Namens, „nur Heimnetz“ und WebUI-Port des Hauptdienstes an bzw. schaltet die vorhandene ein, Gerätename aus dem App-Namen, `tailnetOnly` = beim Herausnehmen wieder aus; Dialog fragt fehlenden Auth-Key einmal ab, erklärt fehlendes Plugin/HTTPS im Tailnet (Ein-Klick-Link aus `tailscale/cert.go`), folgt bis zur geprüften Adresse; Herausnehmen = Logout und Stopp, Gerät in der Tailscale-Verwaltung entfernt der Besitzer; Link `tailnetUrl` als „ts.net“ an den App-Links), dazu Ziele außerhalb von Docker (`targets`, IP/Hostname nur im Heimnetz/Tailscale/NetBird, Prüfung bei jeder Verbindung, nie Ports von titand). Rückweg 0.29.0: Hauptschalter aus = `enabled:false`+`paused:true` je Route, eigene Netze zusätzlich `localOnly:true`, Ziele in eigener Liste; Kurznamen einzeln im App-Zertifikat. TitanOS-Sitzungscookies werden nie an Apps weitergereicht, Apps dürfen sie nicht setzen. Optional nur Heimnetz/Tailscale/NetBird (`netguard.Local`, ab Werk an). Zertifikat: das von Let's Encrypt, wenn es den Namen abdeckt (Platzhalter `*.<domain>`, siehe Zugang), sonst importiertes, wenn es den Namen abdeckt, sonst eines der TitanOS-Zertifizierungsstelle (PLAN 61, `/config/tls/ca-leaf-apps.pem`, nur beim Ausstellen geschrieben), nur ohne sie das selbst signierte Platzhalterzertifikat `/config/tls/apps-cert.pem`. `.local`-Namen per generiertem `/etc/avahi/hosts` (avahi läuft dann auch ohne SMB); eigene Domain braucht einen Platzhalter im Router-DNS (oder öffentliches DNS mit Wildcard). Keine eigenen Firewall-Regeln (der Firewall-Dienst „HTTPS-Namen für Apps“ gilt zusätzlich); der Entwicklungsmodus fasst avahi nicht an.
- **Docker-Apps im Fenster** (PLAN 61, Entscheidungen des Projektinhabers vom 6.11.2026, `docs/APP-WINDOWS.md`; seit 0.61.0 zeigt das Fenster die App nicht mehr als iframe, sondern immer in der Fernansicht, siehe nächster Punkt; Rechte je Konto und die Browser-Schranke für Aufrufe über den HTTPS-Namen, z. B. „In eigenem Tab öffnen“, gelten weiter; die Rahmenfreigabe des Proxys läuft noch, hat aber keinen Verwender mehr, weil die Oberfläche als `frame-src` nur `'self'` erlaubt): Der App-Proxy entfernt `X-Frame-Options` und setzt `frame-ancestors` in jeder CSP-Richtlinie (Report-Only nur ersetzen, nie ergänzen) auf `'self'` und die https-Adressen der Weboberfläche (Hostname, `.local`, Namen von Let's Encrypt/eigenem Zertifikat/DDNS/Tailnet, Adressen des NAS), **nie `*`**, Liste nur aus dem RAM. Rechte je Konto im Paket `appaccess` (`/config/docker/app-access.json`, nur bei Änderung geschrieben; Besitzer/Admins immer, Benutzer nur mit Haken, ohne Eintrag „Neue Docker-Apps“, ab Werk nur Besitzer/Admins; jedes Mal gegen das aktuelle Konto geprüft). **Die Browser-Sperre gilt nur für Seitenaufrufe des Browsers** (`Sec-Fetch-Mode: navigate`, `Sec-Fetch-Dest` document/iframe/frame, ohne `Sec-Fetch-*` GET mit `Accept: text/html`); alles andere (Handy-/TV-Apps, fetch, WebSocket) geht unverändert durch, je App abschaltbar („Anmeldung im Browser prüfen“, ab Werk an). Eintritt nur über `/.titanos/enter` mit einmaligem 60-s-Token (HMAC, Schlüssel `/config/tls/app-gate.key`), danach Cookie `__Host-titanos_app` (12 h, Partitioned), das wie die TitanOS-Sitzung nie zur App gelangt. Ohne gültiges Cookie 403-Seite mit Link über `/api/v1/apps/{app}/open`; Links der Oberfläche auf HTTPS-Namen gehen über diese Route. Automatische HTTPS-Namen für alle Apps mit `titanos.webui`-Port („Automatisch für alle Apps mit WebUI“, ab Werk an, `noAuto` nur bei Änderung gespeichert): nur im RAM, aus Docker alle 30 s, eigene Routen gewinnen auch ausgeschaltet, nie auf den Stick. Routen `/api/v1/apps/access|mine|{app}/window|{app}/open` und `/api/v1/certs/…` nur für die Weboberfläche, nicht in `publicRoutes`/MCP.
- **Fernansicht** (PLAN 62-4/63, Entscheidung des Projektinhabers vom 6.11.2026 „Immer Fernansicht“, seit 0.61.0, `docs/FERNANSICHT.md`, Pakete `viewer`, `docker/viewer.go`, `api/viewer.go`): Das App-Fenster zeigt die App aus einem Browser im Container `titanos-viewer-<hash>` (je Konto und App). Recht wie beim App-Fenster (`appaccess`, serverseitig, alle 3 s erneut zusammen mit Anmeldung/Sperre wie bei Konsolen; Trennzeit = Konsolen-Trennzeit); Löschen des Kontos (auch dessen Profile), Entfernen der App und Entzug des Rechts beenden auch wartende Ansichten. Eigene Objekte nur mit Label `titanos.viewer=1` **und** Namensanfang `titanos-viewer-` (Abbilder: `titanos.viewer.image=1` und `titanos.viewer.release`); titand fasst nie andere an, die Docker-Routen für Apps (und damit MCP/Agent) lehnen Viewer-Container ab, Compose-Dateien mit Netzen `titanos-viewer…` werden abgelehnt. Netz: je Ansicht ein internes Netz `titanos-viewer-<hash>` mit Bridge `tvw<hex>`, nur dieser Viewer und der Web-Container der App (Alias `titanos-viewer-attached`, nur damit wieder abgehängt); die nft-Tabelle `inet titanos_viewer` verwirft neue Verbindungen von `tvw*` zum Host, unabhängig von der Firewall. Apps im Host-Netz (oder `container:`/`service:`) gehen nicht (`viewer_hostnet`, Ausweg „In eigenem Tab“). Container: UID 1000, Wurzel nur lesbar, nur tmpfs `/tmp` (begrenzt), Chromium-Richtlinie von titand nur lesbar eingebunden, `CapDrop ALL`, `no-new-privileges`, 1 GiB RAM, 512 Prozesse, 512 MiB `/dev/shm`, höchstens 2 CPUs mit Gewicht 512, Stoppen mit 10 s Zeit (Chromium schreibt Cookies), GPU nur mit Einstellung (Render-Knoten mit seiner Gruppe bzw. NVIDIA per CDI), nie ein Gerät, das gerade einer VM gehört. Neue Ansicht nur, wenn mit dem reservierten Limit aller Ansichten 700 MB frei bleiben und die Höchstzahl aus dem RAM nicht erreicht ist (`viewer_memory`), höchstens 4 je Konto (`viewer_limit`), Starts nacheinander. Abbild nur aus dem signierten Release der eigenen titand-Version (`manifest.json` → `viewer`), nie aus einer Registry; Profile und Abbild-Download unter `<volume>/.titanos/viewer/` (Eltern root 0700, Profilordner UID 1000 0700), nie auf dem Stick oder im RAM. Einstellungen `/config/viewer/settings.json` (optional, ältere titand ignorieren sie). Routen nur für die Weboberfläche, nicht in `publicRoutes`/MCP/Agent.

## Docker-Layout

- Zwei Ansichten für Container und Compose-Stacks: Liste und Karten (Wechsel
  über die zwei Symbole im Seitenkopf, sofort gespeichert). „Minimal“ und
  Kachelbreiten gibt es seit 553de39 nicht mehr; gespeicherte Breiten werden
  ignoriert, ein gespeichertes „minimal“ wird zur Liste.
- „Seite anpassen“ (Zahnrad-Menü) öffnet die Bearbeiten-Leiste über der
  Übersicht: „Reihenfolge: Eigene Anordnung | A–Z“, in der Kartenansicht
  „Kartengröße: S | M | L“, Image/CPU-Verlauf,
  Standard wiederherstellen, Abbrechen, Layout speichern. Kein neues
  Bedienelement im Seitenkopf. Suche und Filter sind beim Bearbeiten aus.
  - **Eigene Anordnung:** In der Kartenansicht trägt jede Karte (Stack oder
    Container) oben einen Griff „Verschieben“; Ziehen mit Maus oder Finger
    ordnet live um (dieselbe Zeigerlogik wie der Dashboard-Editor,
    `web/src/lib/pointer-drag.ts`; Escape, Fensterwechsel oder Abbruch legen die
    Karte zurück). Tastatur: Griff fokussieren, Pfeiltasten verschieben um eine
    Stelle, die neue Position wird angesagt. Der Karteninhalt ist beim
    Bearbeiten nicht bedienbar (`inert`), geöffnete Stacks klappen zu. In der
    Listenansicht bleibt die Reihenfolgeliste mit Griff und Pfeilknöpfen.
    Beide ändern dieselbe gespeicherte Reihenfolge (`order`, `moveDockerGroup`).
  - **A–Z:** sortiert nach Namen (Stacks unter den Containern einsortiert,
    natürliche Zahlenfolge), in Liste und Karten; Ziehen und Griffe sind aus.
    Die eigene Reihenfolge bleibt gespeichert und gilt nach dem Zurückschalten
    wieder.
- **Karten** (Entscheidung des Projektinhabers vom 4.11.2026, Variante B nach
  Screenshot-Abstimmung): alle Karten gleich groß (die höchste Karte setzt die
  Höhe für alle, auch für die Dienste eines geöffneten Stacks; Regel „keine
  ungleichen Kacheln“), so viele nebeneinander wie passen, Handy eine Spalte
  mit gleichem Verhalten. **Kartengröße** (Folgeentscheidung vom 4.11.2026):
  ein Umschalter S/M/L für **alle Karten gemeinsam**, keine Einzelgrößen je
  Karte. Er ändert nur die Mindestbreite (S 12,5rem, M 17rem wie bisher, L 23rem; S ergibt
  auch neben der Seitenleisten-Navigation bei 1440 px eine Spalte mehr als M)
  und damit, wie viele Karten nebeneinander stehen; alle bleiben gleich groß,
  der Inhalt passt sich an (Container-Abfragen: schmale Karten verdichten die
  Werte, die Laufzeit steht dann nur im Tooltip; der Update-Chip wird zum
  Pfeil mit Tooltip und Screenreader-Text „Update“, damit der Name Platz hat;
  Ports zeigen den ersten Port und „+n“ mit den übrigen im Tooltip, nie einen
  angeschnittenen Chip; breitere Karten brechen Ports notfalls um). Stack- und Dienstkarten
  folgen derselben Größe; am Handy immer eine Spalte. Ein
  Compose-Stack ist **eine** Karte (`StackCard.svelte`), ab Werk zugeklappt:
  Name mit Update-Chip, „Stack · n Dienste“, „x/y laufen“ (Punkt grün/gelb/grau
  plus Text), Summe CPU/RAM der laufenden Dienste, gemeinsamer CPU-Verlauf
  (Summe, am neuesten Wert ausgerichtet), die Dienste als Chips mit
  Zustandspunkt und Update-Pfeil (Zustand auch als Screenreader-Text), Knopf
  „Dienste“, Stack-Aktion „Alle starten“ (sobald ein Dienst steht) bzw. „Alle
  stoppen“ (nacheinander über die Container-Aktionen, ohne Rückfrage wie beim
  einzelnen Container), Links des Hauptdienstes (WebUI/Tailscale/öffentlich).
  Symbol, ⋯ und Rechtsklick öffnen das Stack-Menü. **Geöffneter Stack**
  (Entscheidung des Projektinhabers vom 4.11.2026, Variante C „Rahmen im
  Raster“, nach Screenshots; vorher wirkten die Dienste in eigener Zeile mit
  Überschrift abgetrennt): „Dienste“ öffnet einen getönten Rahmen über die
  ganze Breite. Darin bleibt die Stack-Karte unverändert die erste Karte, die
  Dienste folgen direkt daneben als normale Containerkarten auf denselben
  Spalten wie alle anderen Karten (CSS-Subgrid, gleiche Breite und Höhe);
  reichen die Spalten nicht, bricht die Zeile innerhalb des Rahmens um. Der
  Rahmen beginnt eine eigene Zeile, die nächste Karte folgt darunter. „Dienste“
  auf der Stack-Karte klappt wieder zu (`aria-controls` auf den Rahmen). Die
  Dienstkarten heißen nach dem Dienst (`dockerName(…, 'service')`). Verworfen:
  eigene Kopfzeile statt der Karte (A) und Dienste ohne Rahmen frei im Raster
  (B, reißt am Zeilenende auseinander).
  Prüfung `scripts/test-ui-docker-cards.mjs`, Screenshots unter
  `out/validation/docker-karten/`.
- **Liste als Kacheln** (Entscheidung des Projektinhabers vom 30.10.2026 nach
  Screenshot-Abstimmung): jeder Container eine eigene abgerundete Kachel, alle
  gleich breit (15rem) und 52 px hoch, so viele nebeneinander wie passen
  (Handy: eine Spalte). App-Symbol mittig links, Name (mit Update-Chip) und
  darunter nur CPU · RAM; der Zustand ist der Streifen links (Akzentfarbe =
  läuft, grau = gestoppt; zusätzlich gedimmtes Symbol und „–“ statt Werten,
  Screenreader-Text). Keine Knöpfe in der Kachel: Symbol oder Rechtsklick öffnet
  das Menü (Start/Stopp oben, Adressen mit Ziel, Ports unten). Keine
  Spaltenköpfe. Ein Stack ist eine Kachel derselben Form („laufend/gesamt · CPU ·
  RAM“), sein Name klappt die Dienste als eigene Kacheln dahinter auf, sein
  Symbol öffnet das Stack-Menü (Adressen, Probleme, Links festlegen, Tailnet,
  Bearbeiten). Prüfung: `scripts/test-ui-dashboard-flow.mjs`,
  Screenshots unter `out/validation/`.
- **Karten einheitlich** (Plan 9d, 31.10.2026): Start/Stopp steht immer rechts
  unten, Weblinks (WebUI, Tailscale, öffentlich) rechtsbündig darunter; die
  Portzeile erscheint nur, wenn der Container Ports veröffentlicht, sonst bleibt
  kein leerer Platz. Die Liste (Kacheln ohne Knöpfe und ohne Spaltenköpfe, siehe
  oben) bleibt unverändert schlank; die frühere Tabelle mit Spalte „PORTS“ gibt
  es nicht mehr (der Screenshot `docs/screenshots/current/docker.png` von 0.31.0
  zeigt noch sie).
- Pro Konto in `/config/users/<name>/docker.json`, über die vorhandene atomare
  Preferences-API mit RAM-Cache (`view`, `sort` = `own`/`az`, `size` =
  `s`/`m`/`l`, `order`, `details`, `primaryServices`; ältere Fassungen ohne
  `sort` lesen als „Eigene Anordnung“, ohne `size` als M). Nur ausdrückliches Speichern schreibt, und nur bei einer
  Änderung; unverändertes Speichern schließt die Leiste ohne Schreibzugriff.
  Zuordnung über `app:<Compose-Projekt>` oder `container:<Name>`, nicht die
  nach Updates wechselnde Container-ID. Temporär fehlende Einträge bleiben
  gespeichert; neue Apps werden angehängt. Keine App wird durch Layouts versteckt.
