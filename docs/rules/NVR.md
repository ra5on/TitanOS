# NVR (Kamerarekorder)

Festgelegte Entscheidungen des Projektinhabers vom 2.11.2026 (Plan 26). Der NVR ist im Aufbau; diese Datei nennt, was gilt, und wird mit jedem Baustein ergänzt. Gebaut ist `backend/internal/nvr` mit Suchregeln, Kamerasuche (ONVIF, RTSP, Herstellertabelle), Kameraverwaltung, Aufnahme, Aufbewahrung und Live-Bild, dazu API `/api/v1/nvr/*`, Dienste-Schalter und die Seite „NVR“ (`web/src/routes/nvr`).

## Stand der Umsetzung (2.11.2026)

Fertig und getestet (gegen Nachbau-Kameras und einen Ersatz-ffmpeg, nicht an echter Hardware):
- Suche: ONVIF (WS-Security-Digest, Uhr der Kamera), RTSP-DESCRIBE (Digest/Basic), Herstellertabelle als Rückfall, eigener RTSP-Link; Hauptstream = größtes, Nebenstream = kleinstes Bild; Adresse der Kamera in Antworten wird durch die erreichte Adresse ersetzt.
- Verwaltung: Kameras in `/config/nvr/cameras.json`, Passwörter getrennt in `/config/nvr/secrets.json`, nie in Antworten.
- Aufnahme: je Kamera ein ffmpeg (`-c:v copy`, Ton nur auf Wunsch, 5-Minuten-Dateien als fragmentiertes MP4 unter `<Volume>/nvr/<Kamera>/<Tag>/<Uhrzeit>.mp4`), Neustart mit Wartezeit, Statusmeldungen ohne Passwort. Der Ordner ist auf Btrfs ein eigenes Subvolume, also nicht in Snapshots.
- Aufbewahrung: je Kamera oder allgemein in Tagen; reicht der freie Platz nicht, gehen die ältesten Dateien zuerst; die jeweils neueste Datei bleibt. Ordner entfernter Kameras altern mit dem allgemeinen Wert aus.
- Live: Bewegt-JPEG (ffmpeg, höchstens sechs gleichzeitig); Wiedergabe der Aufnahmen mit Bereichsanfragen.
- Schalter unter Einstellungen › Dienste (ab Werk aus), Menüpunkt nur bei „an“.

**Abweichungen vom Plan, bewusst und offen:**
- ffmpeg läuft als Kindprozess von titand, nicht in einer eigenen gehärteten Einheit `titan-nvr`; Zugangsdaten stehen dabei in der ffmpeg-Befehlszeile (`/proc`). Eigene Einheit mit eigenem Benutzer folgt.
- MQTT/Home Assistant (Gruppe „Kameras (NVR)“ in den MQTT-Einstellungen): Sensoren für Aufnahme an/aus, belegter Platz und letzte Aufnahme je Kamera; mit der Befehlsgruppe „Kameras“ ein Schalter, der die Aufnahme ein- und ausschaltet (nur Start/Stopp, kein Bild, kein Video über MQTT).
- Zeitserver: titand beantwortet NTP-Anfragen (SNTP, UDP 123) nur von Heimnetz-Adressen, höchstens fünf auf einmal und dann eine je Sekunde, und nur solange der NVR an ist und eine Kamera ihn nutzt. „Zeit von TitanOS“ stellt die Kamera um: ONVIF (`SetNTP`, `SetSystemDateAndTime`), sonst Hikvision (ISAPI) oder Dahua (CGI) mit HTTP-Digest; höchstens drei abgelehnte Anmeldungen. Firewall-Dienst `ntp` (UDP 123, wie andere Dienste für das lokale Netz). An Kameras ungeprüft.
- Bewegung und Ereignisse: je Kamera (Schalter „Bewegung erkennen“, Empfindlichkeit niedrig/mittel/hoch) ein ffmpeg am Nebenstream, 2 Bilder/s in 160×90 Grau; reines Go-Verfahren (`motion.go`) mit nachlaufendem Hintergrund, Ausgleich von Helligkeitswechseln über das ganze Bild, zwei Bilder zum Start und zehn ruhige Sekunden zum Ende. Ein Ereignis (`events/<Tag>/<Uhrzeit>.json` und `.jpg` neben den Aufnahmen) hat ein Bild vom Beginn; Klick springt in die Aufnahme. Ereignisse altern mit der Aufbewahrung aus. „Scharf“ (Seite NVR, nicht unter Einstellungen › Meldungen) sendet höchstens eine Meldung `nvr_motion` je Kamera und fünf Minuten über die vorhandenen Meldewege; in Home Assistant ein Bewegungs-Sensor.
- Objekterkennung: Schnittstelle `Detector` (Beschriftungen wie person, vehicle, package, animal, plate) ist vorgesehen und wird auf das Ereignisbild angewendet; **es ist keine Erkennung eingebaut** (Modelle und Laufzeiten sollen als Plugins kommen, Messungen stehen aus).
- Noch nicht gebaut: Objekterkennung selbst (Modelle), Qualität/Bitrate per ONVIF, Ansichten mit festem Raster, Gegensprechen, go2rtc/WebRTC, geschützte Clips, Ausschluss aus Sicherungen. Siehe Plan 26.
- Die Herstellertabelle ist an keinem Gerät geprüft; die vier Messungen (NPU, Kennzeichen, Geschwindigkeit, Cache) stehen aus und brauchen die Kamera und titan.

## Entscheidungen

- **Eigener Kern, kein Frigate, keine Docker-Abhängigkeit.** Dienst `titan-nvr` (eigener Benutzer, gehärtete systemd-Einheit, Sandkasten wie ffmpeg in `docs/rules/SYSTEM.md`/Plan 6b), nicht in titand. go2rtc als festgelegter Helfer, Aufnahme mit ffmpeg als Segmente ohne Umwandlung, Erkennung in eigenem Hilfsprozess (OpenVINO, ONNX Runtime). Die großen Laufzeiten kommen als signierte Plugins (`docs/rules/PLUGINS.md`).
- **Name in der Oberfläche: „NVR“** (Navigation, Seitentitel, Befehlspalette, Dienste-Schalter). Schalter unter Einstellungen › Dienste; Docker aus stoppt den NVR nicht.
- **Anbindung:** ONVIF oder RTSP-Link, beides. ONVIF zuerst; Herstellertabelle als Rückfall; Fähigkeiten werden je Kamera gemessen, nicht nach Modell angenommen.
- **Modelle** nur mit offener Lizenz (Apache/MIT/BSD); Ultralytics-YOLO ist wegen AGPL ausgeschlossen. Modelle sind eigene, signierte Dateien, nicht im Basisabbild.
- **Aufnahmen** gehören nicht in Btrfs-Snapshots, nicht in den Papierkorb und standardmäßig nicht in Sicherungen; Löschen nach Ablauf wirkt sofort und neustartsicher.
- **MCP:** Kamerafunktionen und Bilder sind nicht im Werkzeugkatalog (Datenschutz).
- **Zugangsdaten** von Kameras nie in Dateien im Klartext, Protokollen, URLs der Oberfläche, Fehlermeldungen oder im Diagnosepaket; Anzeige nur mit `nvr.Redact`.

## Sicherheit der Kamerasuche (gilt ab dem ersten Code)

- Nur Besitzer und Administratoren.
- Ziele nur im Heimnetz: private Adressen und die Bereiche von Tailscale/NetBird (`nvr.AllowedAddr`); nie Loopback, Link-Local (auch Cloud-Metadaten `169.254.169.254`), Multicast, unspezifiziert oder öffentlich. Namen werden aufgelöst, und **jede** Adresse muss erlaubt sein (`nvr.Resolve`, schützt vor DNS-Rebinding); bei jedem Verbindungsaufbau erneut prüfen.
- Nur die eingegebene Adresse wird untersucht. „Im Netz suchen“ nur im Subnetz der gewählten Schnittstelle und nur auf Knopfdruck.
- Höchstens drei abgelehnte Anmeldungen je Suche (`nvr.AuthBudget`); sofortiger Halt bei Kontosperre; die Person erfährt es. TitanOS probiert **keine** Standardpasswörter.
- Passwörter werden für Adressen kodiert (`nvr.StreamURL`).
- Jede Änderung an einer Kamera (Zeit, Qualität) braucht Bestätigung und steht im Aktivitätsprotokoll.

## Herstellertabelle

`nvr.Vendors` ist aus Herstellerangaben und allgemeinem Wissen geschrieben und **noch an keinem Gerät geprüft** (`Verified: false`). Ein Eintrag, der sich als falsch zeigt, wird dort mit seinem Test korrigiert. Gruppe 1: Hikvision, Dahua/Amcrest, Reolink, TP-Link Tapo/VIGI, Axis, UniFi Protect; Gruppe 2 folgt (Plan 26).
