# NVR (Kamerarekorder)

Festgelegte Entscheidungen des Projektinhabers vom 2.11.2026 (Plan 26). Der NVR ist im Aufbau; diese Datei nennt, was gilt, und wird mit jedem Baustein ergänzt. Gebaut ist bisher nur `backend/internal/nvr` (Suchregeln, Adressen, Herstellertabelle, Versuchsbudget).

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
