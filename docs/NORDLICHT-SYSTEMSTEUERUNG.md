# Systemsteuerung im Nordlicht-Desktop

Grundlage ist der gemergte Stand von PR 414 (`c44beef5`). Die Seiten, ihre Serverrechte und vorhandene Direktadressen waren bereits vorhanden. Im Desktop-Fenster bleibt die linke Spalte die Navigation und Suche. Auf der Übersichtsseite stehen häufige Aufgaben und offene Probleme an erster Stelle. Ab 42 rem verfügbarer Inhaltsbreite liegen beide Gruppen nebeneinander. Unterhalb dieser Breite nutzen beide Gruppen eine Spalte. In der Aufgabenliste öffnet eine fehlgeschlagene Datensicherung den tatsächlich betroffenen Auftrag. Der Link verwendet den vorhandenen ID-Anker der Sicherungsseite. Die Information kommt aus der Abfrage, die die Übersicht bereits für den Einrichtungsstand absetzt.

## Hauptorte

Die Tabelle führt alle 30 Einstiegsseiten auf. Jede ID bezeichnet weiterhin die Adresse `/settings/<id>`. Alle Hauptorte kommen aus derselben Liste für Fenster, Handy, Gruppenpfade, Suche und Routenfilter. Persönliche und administrative Optionen innerhalb einer Seite behalten ihre feineren Rollenprüfungen.

| Einstellung und ID | Bisher | Künftig | Aufgaben auf der Seite | Zugang zur Seite |
|---|---|---|---|---|
| Updates · `updates` | System | System | System, Apps, Kernel | Besitzer/Admin |
| Datum und Uhrzeit · `timezone` | System | System | Zeitzone, Uhrzeit | Besitzer/Admin |
| Über TitanOS · `about` | System | System | Version, Lizenz, Ersteinrichtung | Alle |
| Dienste · `services` | System | System | Docker, VMs, LXC ein- und ausschalten | Besitzer/Admin |
| Darstellung · `appearance` | System | Darstellung und Mitteilungen | Desktop-Profil, Farben, Schrift und Dock | Alle |
| Benachrichtigungen · `notifications` | Benachrichtigungen | Darstellung und Mitteilungen | E-Mail, Push, Messenger | Besitzer/Admin |
| Sicherungen · `backups` | Daten und Sicherung | Daten und Sicherung | Daten, Apps, Stick, Wiederherstellung | Besitzer/Admin |
| Dateisuche · `files` | Daten und Sicherung | Daten und Sicherung | Ordneransichten, Dateisuche und Vorschaubilder | Besitzer/Admin |
| Systemdaten · `systemdata` | Daten und Sicherung | Daten und Sicherung | Ablage-Volume, Konfigurationssicherung, Protokolle | Besitzer/Admin |
| Netzwerk · `network` | Netzwerk und Fernzugriff | Netzwerk und Fernzugriff | Adressen, Bridge, VLAN, DNS | Besitzer/Admin |
| WireGuard · `wireguard` | Netzwerk und Fernzugriff | Netzwerk und Fernzugriff | VPN-Server und Geräte | Besitzer/Admin |
| Dynamisches DNS · `ddns` | Netzwerk und Fernzugriff | Netzwerk und Fernzugriff | DynDNS, IPv64 | Besitzer/Admin |
| Let's Encrypt · `letsencrypt` | Netzwerk und Fernzugriff | Netzwerk und Fernzugriff | HTTPS-Zertifikate per ACME | Besitzer/Admin |
| HTTPS-Namen für Apps · `proxy` | Netzwerk und Fernzugriff | Netzwerk und Fernzugriff | Adressen wie app.nas.local | Besitzer/Admin |
| Cluster · `cluster` | Cluster | Netzwerk und Fernzugriff | Mehrere NAS im Verbund | Besitzer/Admin |
| Benutzer · `users` | Sicherheit und Konten | Sicherheit und Konten | Konten, Rollen, Passwörter, 2FA | Besitzer/Admin |
| Zugang · `access` | Sicherheit und Konten | Sicherheit und Konten | SSH, Zertifikat, Anmeldung, WebDAV | Besitzer/Admin |
| Firewall · `firewall` | Sicherheit und Konten | Sicherheit und Konten | Zonen, Regeln, Sperren | Besitzer/Admin |
| API und CLI · `api` | Sicherheit und Konten | Sicherheit und Konten | Tokens, CLI, OpenAPI | Alle |
| Hardware · `hardware` | Hardware und Energie | Hardware und Energie | Grafik, Treiber, USB-Platten | Besitzer/Admin |
| Energie · `energy` | Hardware und Energie | Hardware und Energie | Zeitplan, Wake-on-LAN, Platten-Ruhe | Besitzer/Admin |
| Temperaturen · `temperatures` | Hardware und Energie | Hardware und Energie | Sensoren, Lüfter, Warnungen | Alle |
| Ein-/Ausschalter · `power-button` | Hardware und Energie | Hardware und Energie | Was die Ein-/Aus-Taste tut | Besitzer/Admin |
| USV · `ups` | Hardware und Energie | Hardware und Energie | USV, Batterie, Herunterfahren | Besitzer/Admin |
| Schreibcache · `memory` | Hardware und Energie | Hardware und Energie | Arbeitsspeicher, Schreibcache | Besitzer/Admin |
| Home Assistant · `homeassistant` | Smart Home und KI | Apps und Integrationen | Verbindungen, MQTT, Speicher | Besitzer/Admin |
| KI-Agent · `agent` | Smart Home und KI | Apps und Integrationen | Modelle, Rechte, Gedächtnis | Besitzer/Admin |
| Sprachdienste · `speech` | Smart Home und KI | Apps und Integrationen | Whisper und Piper für Home Assistant und den Agenten | Besitzer/Admin |
| MCP-Server · `mcp` | Smart Home und KI | Apps und Integrationen | KI-Clients, Werkzeuge | Alle |
| Fernansicht · `viewer` | System | Apps und Integrationen | Docker-Apps im Fenster, GPU und Ton | Besitzer/Admin |

Die Daten-Sicherung hat weiterhin eigene Seiten für Datenaufträge (`backups/data`), Anwendungen (`backups/applications`), Konfiguration (`backups/configuration`), Abdeckung (`backups/coverage`) und Wiederherstellen (`backups/restore`). Die alte Dock-Seite und die bisherigen Einstiege `stick`, `ipv64` und `programs` behalten ihre Weiterleitungen. Auch `?area=notifications`, `?area=cluster`, `?area=accounts`, `?area=ai`, `?area=services` und `?area=updates` bleiben auswertbar. Suchergebnisse und Navigation prüfen die Rolle vor dem Öffnen. Für Benutzer enthält das Menü nur die fünf Bereiche mit zugänglichen Seiten.

## Finden und anpassen

„Backup wiederherstellen“ und „Restore backup“ führen zur Auswahl der Sicherung. „Immich Logs“ sucht in der für Administratoren freigegebenen aktuellen Containerliste. Bei mehreren Diensten gibt es je Dienst einen benannten Treffer. Die ID öffnet dasselbe Protokollfenster wie das Zeilenmenü von Docker. Ein inzwischen entfernter Container wird als solcher gemeldet. Hauptmenü, Systemsteuerung und Befehlspalette teilen die Trefferlogik. Die 610 automatisch erfassten Register, Abschnitte und Einstellungen einschließlich der Bereiche von „Mein Konto“ stehen in `web/src/lib/settings-index.gen.ts`. Der Generator gleicht die Liste mit den Seitentexten ab.

Die Desktop-Profile Nordlicht, Klar und Kompakt verändern Glas, Dichte, Radien, Leuchten und Übergangsdauer im Desktop-Fenster. „Eigene Werte“ nutzt die bereits gespeicherten Einzelwerte. Schriftgröße, Akzent, Modus, Bewegungspräferenz, Hintergrundbild sowie Dock- und Fensterbelegung bleiben bei der Profilwahl erhalten. Der Bereich „Erweitert“ bündelt die zusätzlichen Regler und den Dateiaustausch. Bestehende Suchanker öffnen ihn. Das Handy verwendet die bisherigen Darstellungswerte.

## Abnahme

- Die Node-Tests decken die vollständige Seitenzuordnung, freigegebene Benutzerziele, Direktadressen, deutsche und englische Suchanfragen, getrennte Containerdienste und die Abwärtskompatibilität der Darstellungswerte ab.
- `scripts/test-ui-nordlicht-settings.mjs` öffnet ein echtes TitanOS-Fenster bei 1280 px und die Handy-Oberfläche bei 390 px mit isolierten API-Daten. Bei zwei vorhandenen Sicherungen öffnet der Desktop das Problem aus der Aufgabenliste. Am Handy führt die Glocke zur Meldung „Backup nacht fehlgeschlagen“ und „Details öffnen“ zum selben Auftrag. In beiden Fällen wird die konkrete USB-Fehlerursache im Auftrag geprüft. Anschließend sucht der Test die Wiederherstellung, wählt eine Datei und das Ziel-Volume und prüft die abgeschickte Auswahl. Er öffnet die umsortierten Benachrichtigungen sowohl über die alte Gruppenadresse als auch per Suche und prüft dort die vorhandene Push-Einstellung. Anschließend öffnet er das Protokoll des benannten Containers. Im Desktop-Fenster werden die drei Profile mit größerer Schrift und reduzierter Bewegung durchgeschaltet. Bei einem regulären Konto prüft der Test, dass die administrativen Suchtreffer ausbleiben und keine Abfrage der Container- oder Sicherungslisten ausgelöst wird.
- `scripts/test-ui-settings-nav.mjs` bestand die Prüfung des Fensterakkordeons, der Pfeiltasten, der Befehlspalette, der Benutzermenüs und des mobilen Rückwegs bis Home.
- `scripts/test-ui-settings-home.mjs` lief über 38 Seiten- und Registeraufrufe bei jeweils 1440, 800 und 390 px. Die 114 Layoutproben bestanden die Prüfung auf horizontalen Überlauf, abgeschnittene Inhalte und sich überlagernde Bedienelemente. Je nach Einstellung enthalten die lokalen Testdaten auch leere Zustände.
- Die Browserprüfung belegt Oberfläche, Suchwege und Anfragen. Die Rückmeldung zum Wiederherstellen und das Containerprotokoll kommen im Browsertest aus Fixtures. Den Test mit tatsächlich geschriebenen Dateien dokumentiert die getrennte Stabilitätsprüfung.

Der vollständige Ablauf bestand mit Chromium und WebKit jeweils bei 1280 und 390 px. Die WebKit-Läufe erfolgten in der Linux-Emulation. Ein physisches iPhone sowie VoiceOver wurden hier nicht geprüft.
