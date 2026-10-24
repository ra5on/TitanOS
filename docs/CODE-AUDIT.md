# Codeprüfung für 0.21.0

Die Prüfung kombiniert Go-Vet, Erreichbarkeitsanalyse mit
`golang.org/x/tools/cmd/deadcode@v0.52.0 -test` (zusätzlich `-tags embedui`),
alle bestehenden Go-/Web-/Paketierungstests und gezielte Quell-/Browserprüfung.
Zusätzlich `honnef.co/go/tools/cmd/staticcheck@v0.8.2 -checks=S*,SA*,U*`:
Reine Stilhinweise sind ausgenommen; Produktnamen in Fehlermeldungen bleiben erhalten;
protokollbedingt nötiges MD4 für SMB ist am Import gezielt begründet.
Sie ist keine Zusicherung, jede Hardware oder jeden möglichen Ablauf praktisch
abgenommen zu haben. Benutzerdateien, eigene Änderungen, historische Berichte
und dokumentiert abgelegte Vorlagen werden nicht als „toter Code“ gelöscht.

## Prüfmatrix

Alle Backend-Pakete werden von Vet und den vorhandenen Pakettests erfasst.
Die folgende Matrix ordnet die Funktionsbereiche den zentralen Prüfpunkten zu;
die zusätzlichen Browser-/Race-/QEMU-Prüfungen nennt der Releasebericht.

| Funktionsbereich | Schnittstelle/Rechte und Zustandsgrenze | Regressionen |
|---|---|---|
| Einrichtung, Konten, Rollen, 2FA | Sitzungen, eigenes Konto, Administrator-/Besitzerrechte | `auth`, `api` |
| Konfiguration | atomare seltene Stickschreibzugriffe, RAM-Abfragen | `config`, `configbackup`, `api` |
| mdadm, Btrfs, SMART, Standby | bestätigte stabile Geräteidentität, Entwicklungsmodus, Mounts | `storage`, `api`, Storage-Smoke |
| SMB/NFS und Shares | Kontorechte, ausgewähltes Volume | `shares`, `api`, Browser-Workflows |
| Dateien, Papierkorb, Uploads | autorisierte Roots, Symlinks, Konflikte, Auftragszustände | `files`, `volumedata`, `api` |
| Docker und Apps | Administratoren, Compose-Dienste, Hintergrundfehler, Volume | `docker`, `catalog`, `api`, neue Browserprüfung |
| VMs und Snapshots/Klone | Administratoren, Ressourcen-/Lifecycle-Sperren | `vm`, `api`, Storage-Smoke |
| Hardware und Plugins | stabile IDs, exakter Kernel, Signaturen, Durchreichung | `hardware`, `plugins`, `minisign`, Hardware-Buildgate |
| RAM und Monitoring | physische Belegung, verfügbarer RAM, Writeback, Beobachtungslücken | `system`, `monitoring`, neue Browserprüfung |
| Netzwerk und Fernzugriff | bestätigter Wechsel/Rückfall, bestehende Firewallentscheidung | `network`, `tailscale`, `api` |
| Backups, Ziele, App-Sicherungen | Quell-/Zielidentität, Writer-Sperren, Restore als neue Kopie | `backup`, `backuptarget`, `appbackup`, `api` |
| Wartung und Aufträge | Aufnahme, Abhängigkeiten, unbekannte Ausgänge | `maintenance`, `api`, Browser-Operations |
| Assistent und Dokumente | Schema, Konto/Bereich, Bestätigung/Direktmodus, Journal | `assistant`, `office`, `api` |
| Meldungen und Integrationen | persönliche Inhalte, SMTP/HA, externe Verbindungen | `notify`, `homeassistant`, `api` |
| SSH, Terminal und TLS | bestehende Admin-Anmeldung, Origin, Sitzungsgrenzen | `sshd`, `terminal`, `tlscert`, `api` |
| USV, Energie und USB | erkannte Hardware, Entwicklungsmodus, explizite Mounts | `ups`, `usb`, `system`, `api` |
| System-/App-Updates und Boot | Signaturen, A/B, Healthcheck, Rückfallkompatibilität | `update`, `app`, `cmd/titand`, Release-Smoke |
| WebUI, Navigation und Darstellung | Rollen, DE/EN, gemeinsame Komponenten, Mobilansicht | Webtests, Svelte, Browserprüfungen |
| Hilfspakete | Befehlsausführung, Namen, Version, eingebettete Dateien | Compiler/Vet, Aufrufer-/Erreichbarkeitsprüfung |

## Belegte Befunde und Korrekturen

- RAM-Gesamtbelegung enthielt Cache und wirkte dadurch trotz viel verfügbarem
  Speicher voll. Anzeige trennt jetzt Verfügbarkeit und physische Belegung;
  serverseitige Hysterese und optionale PSI-Werte ergänzen die Diagnose.
- Im Store-Formular fehlten einzelne Image-Tags. Dienstweise Bearbeitung ergänzt;
  Store-Pins, Digests und Ausdrücke bleiben erhalten.
- YAML-Änderungen gingen beim Rückwechsel ins Installationsformular verloren.
  Bearbeitetes YAML ist nun maßgeblich, ohne neue Geheimnisse zu erzeugen oder
  bearbeitete Zeitzonen/Ports/Pfade erneut automatisch vorzubelegen.
- Änderungen an geteilten YAML-Ankern konnten andere Dienste verändern.
  Betroffene Aliase werden beim Schreiben unabhängig aufgelöst; unberührte
  Anker bleiben erhalten. Hardwareauswahl verwendet dieselbe Dienstlogik.
- Compose-Validierung berücksichtigte die spätere `.env` nicht. Prüfung und Start
  verwenden jetzt dieselben Variablen; vor Erfolg keine persistente Ablage neuer
  Geheimnisse, temporäre Datei 0600 mit Entfernung auch im Fehlerfall.
- Intel-/AMD-Microcode fehlte; die Builder-CPU darf den Inhalt eines
  portablen NAS-Abbilds nicht beschränken. Beide Hersteller und neue Inhaltsgates
  sind ergänzt. NAS-Firmwareauswahl wurde verbreitert.
- Beim Dateikopieren war die Ersetzen-Bestätigung bis zum ersten Kopierklick
  gesperrt. Die Zielprüfung startet jetzt bei Auswahl von Ersetzen/Einzelentscheidungen;
  Zielwechsel und veraltete Antworten können keine alte Freigabe übernehmen.
- Drei von Erreichbarkeitsanalyse und Aufrufersuche bestätigte unbenutzte Helfer
  entfernt: `Server.assist`, `app.ManifestInfo` und `office.Plain`.
- Ungenutzte Zwischenzuweisungen beim Automationsabgleich und beim Papierkorb
  entfernt; die anschließenden maßgeblichen Metadatenwerte bleiben unverändert.
- Wiederholte RAM-Statusabfragen verschwiegen Fehler. Die Einstellungsseite
  zeigt sie jetzt und pausiert Hintergrundabfragen bei ausgeblendetem Dokument.

## Grenzen

Tests mit simulierten Diensten sind keine Abnahme realer Ollama-Modelle,
Mailzustellung, Tailnets, USVs, physischer Datenträger oder GPUs. Ein grüner
Compiler oder eine leere Deadcode-Liste beweist keine vollständige fachliche
Fehlerfreiheit. Versionsbezogene Build-/QEMU-Ergebnisse und offene Hardwaretests
stehen in [den Release-Notizen](releases/v0.21.0.md).
