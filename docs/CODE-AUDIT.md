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

| Bereich | Oberfläche / API | Rechte | Speicherung | Hintergrundarbeit | Fehlerbehandlung | Neustart / Rückfall |
|---|---|---|---|---|---|---|
| Einrichtung, Konten, 2FA | Setup, Konto, Sitzungen; `auth`/`api` | Besitzer, Administrator, eigenes Konto | atomare Kontokonfiguration | Sitzungsablauf, TOTP | Anmelde-/Schemaprüfung | App-Sitzung erhalten, NAS-Sitzung verwerfen |
| Konfiguration und Restore | Einstellungen/Import; `config`/`configbackup` | Administration | atomare seltene Stickschreibzugriffe, RAM-Lesen | kein zyklischer Stickzugriff | fehlgeschlagene Ablage/Rücknahme | vorhandene Felder kompatibel |
| mdadm, Btrfs, SMART, Standby | Speicher; `storage`/`api`, Browser | bestätigte stabile Geräteidentität | UUIDs und Volume-Zuordnung | Wiederaufbau, Prüfungen, Mounts | degradierte Arrays, Entwicklungsmodus | Mount-/Recovery-Prüfungen im Storage-Smoke |
| SMB/NFS und Shares | Shares; `shares`/`api`, Browser | Konten, Freigaberechte | Konfiguration, gewähltes Volume | Dienste und Wiederabgleich | Dienst-/Pfadfehler | Konfiguration bei Start erzeugen |
| Dateien, Papierkorb, Uploads | Dateien/Aufträge; `files`/`api`, Browser | Roots, Symlinks, Konfliktfreigabe | Nutzdaten auf Volume, Upload-Metadaten | Kopieren, Prüfen, Speicherreservierung | Abbruch, konkurrierende Ziele, veraltete Vorschau | dauerhafte Uploads fortsetzen; RAM-Aufträge nicht als dauerhaft behandeln |
| Docker und Apps | Store/Stack-Editor; `docker`/`catalog`/`api` | Administration, Geräteauswahl | Compose, private `.env` auf Volume | Pull/Start, Updateprüfung | Pull- und Validierungsfehler sichtbar | gespeicherte Images/Tags erhalten |
| VMs und Snapshots | VMs; `vm`/`api`, Storage-Smoke | Administration, Ressourcensperren | Images/Snapshots auf Volume | Kopieren, Klonen, Snapshot | unbekannte Ausgänge, Writer-Sperren | Autostart-Marker, Snapshot-/Recovery-Prüfung |
| Hardware und Plugins | Geräte/Plugins; `hardware`/`plugins`/`api` | stabile IDs, explizite Durchreichung | signierte Dateien auf Volume, Basis-Version | Treiberbau, Aktivierung | Kernel-/Signaturfehler, fehlende GPU | erneute Aktivierung, Rückfall; Hardware-Buildgate |
| RAM und Monitoring | Dashboard/Einstellungen; `system`/`monitoring` | Status lesen, Einstellungen administrieren | Einstellungen kompatibel; Messwerte im RAM | gemeinsamer Takt, Warnhysterese, Upload-Budget | fehlende PSI, Abfragelücken, Sysctl-Rücknahme | Einstellung neu anwenden, keine historischen RAM-Warnungen übernehmen |
| Netzwerk und Fernzugriff | Netzwerk/Zugang; `network`/`tailscale`/`api` | explizite Bestätigung | bestätigte Konfiguration | Rückfallfrist, Dienstabgleich | fehlende Verbindung/Bridge | bestätigte Bridge erhalten, unbestätigte zurücknehmen |
| Backups und Ziele | Sicherungen; `backup`/`backuptarget`/`appbackup`/`api` | Quelle/Ziel, Writer-Sperren | Volume, Restore als neue Kopie | Zeitplan, Prüfung, Wiederherstellung | Zielverlust, ungeklärte App-Sicherung | dauerhafte Zustände abgleichen |
| Wartung und Aufträge | Auftragsanzeige; `maintenance`/`api`, Browser | Bereich und Aktionsrecht | unterstützte Journale auf Volume | Aufnahme, Abhängigkeiten | unbekannte Ausgänge blockieren | nur ausdrücklich dauerhafte Aufträge fortsetzen |
| Assistent und Dokumente | Chat/Aufträge; `assistant`/`office`/`api` | Konto/Bereich, Freigabe/Direktmodus | getrennte Chats, Journal, neue Dokumentkopien | genau eine Ausführung, Checkpoints | Schema, Modell-/Werkzeugfehler, Schleifen | unbekannte Mutationen nicht wiederholen |
| Meldungen und Integrationen | Meldungen/HA; `notify`/`homeassistant`/`api` | persönliche Inhalte, Admin-Systemmeldungen | private Einstellungen | Zustellversuche, HA-Beobachter | Ersatzwege, externe Verbindungsfehler | keine Freigabetoken persistieren |
| SSH, Terminal, TLS | Zugang/Terminal; `sshd`/`terminal`/`tlscert`/`api` | Admin, Origin, Sitzungsgrenze | atomare Schlüssel/Zertifikate | Dienst- und Verbindungslebensdauer | Abbruch, ungültige Herkunft | Zertifikat erhalten, Dienst gemäß Einstellung starten |
| USV, Energie, USB | Integrationen/Geräte; `ups`/`usb`/`system`/`api` | erkannte Geräte, explizite Aktionen | Konfiguration | Ereignisse, Zeitplan | Entwicklungsmodus, Geräteverlust | Dienste neu erzeugen, Mounts ausdrücklich behandeln |
| System-/App-Updates | Updates; `update`/`app`/`cmd/titand`, QEMU | Administration, Signaturen | A/B-Slots und App-Zustand | Download, Healthcheck, Auftragswartezeit | Hash-/Signaturfehler, Fehlstarts | Bootzähler, App-/Systemrückfall |
| WebUI und Hilfspakete | Navigation, DE/EN, gemeinsame Komponenten | rollenabhängige Funktionen | vorhandene Einstellungen | Sichtbarkeit, Polling, Aufräumen | Wiederholen, feldbezogene Fehler | erneutes Öffnen und gespeicherte Werte; Compiler/Vet/Erreichbarkeit |

Die Zellen nennen die jeweils geprüfte Grenze und den zugehörigen Quell- bzw.
Regressionseinstieg. Sie sind keine Zusicherung einer praktischen Abnahme jedes
Einzelablaufs; ergänzende Browser-, Race- und QEMU-Ergebnisse sind versionsbezogen.

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
