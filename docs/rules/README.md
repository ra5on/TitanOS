# TitanOS – Fachregeln

Verbindliche Regeln und Entscheidungen des Projektinhabers je Bereich. Sie standen bis 30.10.2026 vollständig in `CLAUDE.md` und wurden inhaltlich unverändert hierher verschoben, damit die Projektdatei klein bleibt. `CLAUDE.md` enthält nur noch die Kernregeln (Entscheidungen, Lizenz, Stick-Layout, Build/Test, harte Grundsätze, Arbeitsweise).

- Vor Arbeiten an einem Bereich dessen Regeldatei lesen.
- Neue Regeln in die passende Regeldatei schreiben, nicht an `CLAUDE.md` anhängen. Bei Änderungen die betroffene Regel aktualisieren, statt widersprechende Nachträge anzuhängen.
- Verweise in den Regeln wie „siehe unten“, „siehe ‚Zugang‘“ oder „im Web-UI-Abschnitt“ stammen aus der früheren Gesamtdatei; den Abschnitt über die Tabelle unten finden.

Früherer Einleitungssatz der Gesamtdatei (unverändert): Stand: **0.38.0** (Release-Prüfstand siehe docs/releases/v0.38.0.md). Diese Datei beschreibt die aktuell geltenden Entscheidungen, nicht die Reihenfolge ihrer Einführung. Historische Release- und Testberichte stehen in `docs/releases/` und den Fachdokumenten. Bei Änderungen die betroffene Regel hier aktualisieren, statt widersprechende Nachträge anzuhängen.

## Verzeichnis

Verbindliche Detailregeln je Bereich. Vor Arbeiten an einem Bereich die Datei lesen; die genannten Fachdokumente in `docs/` beschreiben Bedienung, Grenzen und Prüfungen.

| Regeldatei | Lesen vor Arbeiten an … |
|---|---|
| `docs/rules/STORAGE.md` | Stick/`/config`, Volumes, mdadm/Btrfs, SSD-Cache, Verschlüsselung, Firmware/Treiber, Ruhemodus, RAM-/Upload-Budget, Übernahme/Erweiterung, Wartung, Auftragsabläufe, Anwendungssicherungen, Restore-Proben, VM-Klon, Speicherverbrauch |
| `docs/rules/UPDATES.md` | Update-Quelle, Signaturen, Update-Einstellungen, A/B-Systemupdates, App-Updates, Boot-/App-Check |
| `docs/rules/ASSISTANT.md` | KI-Assistent, Modellanbieter, Aufträge/Freigaben, Automationen, Werkzeugkatalog |
| `docs/rules/WEB-UI.md` | Kopfzeile, Konto-Menü, Bestätigungsdialoge, Marke, Einstellungsseiten mit Seitenleiste, Dashboard, Themes/Farben/Glas, gemeinsame UI, mobile Navigation, Befehlspalette |
| `docs/rules/ACCOUNTS-ACCESS.md` | Konten/Rollen, 2FA, Passkeys, Sitzungen/„Angemeldet bleiben“, Konten sperren, SSH/Zertifikate (Zugang), Terminal und Konsolen, Aktivitätsprotokoll |
| `docs/rules/SYSTEM.md` | Energie, Zeitplan/Wake-on-LAN, USV, Werkzeuge-Reiter, Benachrichtigungen, Monitoring/CPU-Leistung, Ein-/Ausschalter, Tailscale, NetBird |
| `docs/rules/NETWORK.md` | Netzwerkeinstellungen mit Rückfall, Firewall |
| `docs/rules/BACKUPS.md` | Datensicherung, Sicherung des Sticks, USB-Kopie, SMB-Kopie, Meldungstöne |
| `docs/rules/SHARES-FILES.md` | Shares, Freigabe samt Daten löschen, Kontingente, Dateimanager, Datei-Editor, USB-Uploads, Namensregeln, Vorschau/Suche, Link-Downloads |
| `docs/rules/CLUSTER.md` | Cluster (Beitritt, Proxy, Kontenabgleich, Backups) |
| `docs/rules/API.md` | Öffentliche API/Tokens, titanosctl, MCP-Server, VM-Medien-Import, Plugin-Aufräumen |
| `docs/rules/DOCKER.md` | Docker-Übersicht, Container bearbeiten, App-Store/Katalog, Docker-Layout |
| `docs/rules/VMS-LXC.md` | VMs (Erstellen, Firmware, Konsole, Snapshots), App-Vorlagen, Hardware-Durchreichung, LXC-Systemcontainer |
| `docs/rules/PLUGINS.md` | Plugins, Community-Plugins, NVIDIA-Treiberbau |
| `docs/rules/HOME-ASSISTANT.md` | Home-Assistant-Anbindung |

Weitere maßgebliche Dokumente: `docs/design/README.md` (UI-Entwürfe), `docs/RELEASING.md` (Release-Ablauf), `docs/WORKFLOWS.md` (Speicher-/Wartungsabläufe), sowie die Fachdokumente `docs/<BEREICH>.md`, auf die die Regeldateien verweisen.
