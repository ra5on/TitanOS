# TitanOS

Ein schlankes NAS-Betriebssystem, das vom USB-Stick bootet und komplett im RAM läuft – inspiriert von Unraid, aber mit klassischem RAID. Wird auf GitHub veröffentlicht.

Stand: **0.17.0** (App-Update auf Systembasis 0.15.0; Release-Prüfstand siehe docs/releases/v0.17.0.md). Diese Datei beschreibt die aktuell geltenden Entscheidungen, nicht die Reihenfolge ihrer Einführung. Historische Release- und Testberichte stehen in `docs/releases/` und den Fachdokumenten. Bei Änderungen die betroffene Regel hier aktualisieren, statt widersprechende Nachträge anzuhängen.

Eigenständiges Projekt. Keinen Code, keine Struktur und keine Annahmen aus anderen Projekten übernehmen.

## Festgelegte Entscheidungen

| Bereich | Entscheidung |
|---|---|
| Basis | Debian 13 (trixie) minimal, amd64 |
| Boot | nur UEFI, systemd-boot, Secure Boot aus |
| Stick | FAT32, Label `TITANOS` |
| Systemabbild | squashfs (zstd), wird beim Boot **komprimiert** in den RAM (tmpfs) kopiert, darüber overlayfs mit größenbegrenzter tmpfs-Schicht (512 MiB) |
| Nach dem Boot | Stick wird nur noch für Konfigurationsänderungen und Updates beschrieben |
| Updates | ein Stable-Kanal; volle Systemupdates in A/B-Slots mit Boot-Rückfall, kompatible App-Updates nur für titand/Weboberfläche ohne NAS-Neustart mit eigenem Rückfall |
| Auslieferung | Zip zum Kopieren auf einen FAT32-Stick. Kein Pflicht-`.img` (optional später im CI) |
| Speicher | klassisches RAID per **mdadm** (0, 1, 5, 6, 10) mit ext4/XFS (umgesetzt); Btrfs optional für native Zwei-Platten-Spiegel mit Snapshots (umgesetzt, siehe docs/BTRFS.md); ZFS ggf. später |
| Backend | **Go**, ein einzelnes statisches Binary (`titand`) |
| Frontend | **Svelte + TypeScript**, als statische Dateien per `go:embed` ins Binary eingebettet |
| Veröffentlichung | GitHub: https://github.com/ra5on/TitanOS (öffentlich), Releases per GitHub Actions |
| Lizenz eigener Code | **FSL-1.1-ALv2** (Functional Source License, Fair Source): alles erlaubt außer konkurrierender kommerzieller Nutzung (Verkauf als Produkt, Angebot als Dienst); jede Version wird 2 Jahre nach Erscheinen Apache-2.0. |

## Lizenz und Beiträge

- `LICENSE.md` enthält den unveränderten Text der FSL-1.1-ALv2 (offizielle Vorlage von fsl.software) mit `Copyright 2026 ra5on`. Lizenztext nie umformulieren.
- Bezeichnung: „Fair Source“. Nicht „Open Source“ nennen, solange eine Version noch nicht zu Apache-2.0 gewechselt ist. Lizenzhinweis in README und unter „Einstellungen › Über“.
- Jede Release-Seite nennt das Datum, ab dem diese Version unter Apache-2.0 steht (Erscheinungsdatum + 2 Jahre).
- Fremdkomponenten behalten ihre Lizenz (Linux-Kernel, Debian-Pakete: GPL u. a.; noVNC: MPL-2.0; xterm.js, gridstack: MIT). Das Abbild ist eine Zusammenstellung; GPL-Teile bleiben GPL, ihre Quellen werden jedem Release beigelegt bzw. verlinkt (CI). Eigener Code, der GPL-Code ableitet (z. B. Kernelmodule), muss GPL sein.
- `THIRD_PARTY_LICENSES` im Repo und im Abbild (unter „Einstellungen › Über“ anzeigen). Umgesetzt: „Einstellungen › Über TitanOS“ zeigt Version, Kernel, den Lizenzhinweis (Fair Source, FSL-1.1-ALv2, Apache-2.0 nach zwei Jahren), den Lizenztext und `THIRD_PARTY_LICENSES`; beide Dateien liegen im Abbild unter `/usr/share/titanos/`. Die Repo-Datei `THIRD_PARTY_LICENSES` ist eine Übersicht. Für jedes Abbild erzeugt `scripts/third-party-licenses.py` ein Versionsinventar aus Debian-Paketdaten, den Go-Modulen des gebauten Binarys und dem npm-Paketbaum einschließlich Fonts und Build-Werkzeugen. Originalhinweise liegen unter `/usr/share/titanos/licenses/`; das Release enthält ein Lizenzarchiv und genaue Quellpaketlinks.
- Beiträge nur mit Contributor License Agreement (z. B. per CLA-Bot), damit kommerzielle Lizenzen später möglich bleiben. `CONTRIBUTING.md` erklärt das.
- Name: „basiert auf Debian“ ist okay, „Debian“ nicht im Produktnamen.

## Offene Punkte

- **CLA:** Entwurf liegt in `CLA.md` (Lizenzeinräumung ohne Übertragung des Urheberrechts, Recht zur Unterlizenzierung inkl. kommerziell und Apache-2.0, Patentlizenz) – vor Nutzung prüfen lassen. Bot: gehosteter CLA Assistant (cla-assistant.io) mit dem CLA-Text als GitHub Gist. Die „Lite“-GitHub-Action (contributor-assistant/github-action) ist seit März 2026 archiviert – nicht verwenden.
- Initramfs: Debian `live-boot` ist im Einsatz und in QEMU getestet (`toram=filesystem.squashfs`, `live-media-path=` pro A/B-Eintrag, `overlay-size=512M`). Ein initramfs-Skript wartet bis zu 30 Sekunden auf den Stick, damit live-boot ihn beim ersten Versuch findet und nicht alle anderen Platten durchsucht. Nur wenn der Stick gar nicht auftaucht, sucht live-boot weiter und bindet dabei fremde Dateisysteme lesend ein.

## Funktionsumfang und offene Vorhaben

- Umgesetzt: Ersteinrichtung, Konten/Rollen und TOTP, SMB/NFS, Dateimanager, mdadm und Btrfs-Spiegel, Docker/Apps, VMs, Plugins, Netzwerk mit Rückfall, Backups, USV, Benachrichtigungen, Diagnose, SSH/Zertifikate und Themes. Die jeweiligen Abschnitte unten nennen Grenzen und Prüfstand.
- Ersteinrichtung: frei gewähltes Besitzerkonto, danach `/welcome` mit Sprache, Hostname, Zeitzone, Netzwerkhinweis und nächsten Schritten. Das erste Volume wird auf der Speicher-Seite angelegt.
- Deutsch und Englisch; alle Oberflächentexte über Übersetzungsdateien.
- Seit 0.8.0 App-Updates; seit 0.11.0 integrierter KI-Assistent mit Aufträgen und Dokumenten; seit 0.14.0 persönliche Benachrichtigungen, Home Assistant, Energie-Zeitplan mit Wake-on-LAN sowie Prüfen und Einzeldatei-Wiederherstellung von Backups. Einzelheiten in den eigenen Abschnitten.
- Zurückgestellt bzw. offen: ZFS, dokumentierte öffentliche API/CLI, Let's Encrypt und weitere Fernzugriffsarten. **Tailscale sowie kurze und optionale längere Monitoring-Verläufe sind umgesetzt**, siehe `docs/TAILSCALE.md` und `docs/MONITORING.md`.
- LUKS-Volume-Verschlüsselung ist auf Entscheidung des Projektinhabers nicht vorgesehen. Die Befehlspalette (Strg/⌘+K) ist seit 0.16.0 umgesetzt: nur Navigation, rollenabhängig, keine Mutationen.
- Bereits umgesetzte Funktionen nicht erneut als unentworfene Release-Voraussetzung behandeln. Für UI-Arbeiten gelten die aktuellen Entwürfe und Folgeentscheidungen unten.

## Stick-Layout

```
TITANOS (FAT32)
├── EFI/BOOT/BOOTX64.EFI          # systemd-boot
├── loader/
│   ├── loader.conf
│   └── entries/
│       ├── titanos-a.conf
│       └── titanos-b.conf         # erst nach dem ersten Update; auf Probe: titanos-b+3.conf
├── titanos/
│   ├── a/  vmlinuz, initrd.img, filesystem.squashfs, version, app/
│   └── b/  (zweiter System-Slot; optional ebenfalls app/)
└── config/                        # persistente Konfiguration, Konten und Geheimnisse
```

Grundsätze:
- Das Rootfs ist bei jedem Boot frisch. Die persistente Systemkonfiguration liegt in `/config` auf dem Stick; Nutzdaten, Chats, Plugin-Dateien und Betriebsdaten liegen auf Daten-Volumes. App-Update-Dateien liegen separat im jeweiligen Stick-Slot. Configs für Samba, NFS, Netzwerk usw. werden beim Start aus `/config` erzeugt.
- Schreibzugriffe auf `/config` erfolgen atomar (temp-Datei + rename) und selten.
- Der Stick wird nur bei Bedarf eingebunden (systemd-Automount auf `/efi`, `/config` ist ein Link auf `/efi/config`) und nach 30 Sekunden ohne Zugriff wieder ausgehängt. titand liest `/config` deshalb beim Start und hält es im Speicher; kein Zugriff auf `/config` in zyklischen Abfragen, keine offenen Dateien oder Verzeichnisse dort.
- Schwere Daten (Docker, VM-Images, große Logs) liegen nie im Overlay, sondern auf einem Volume.
- Klassische RAID-Volumes: ein mdadm-Array mit einem Dateisystem (ext4 oder XFS), eingebunden unter `/mnt/<name>`. Der Name hat höchstens 12 Zeichen (`[A-Za-z][A-Za-z0-9-]*`), weil er auch Array-Name und Dateisystem-Label ist. Welche Arrays TitanOS gehören, steht in `/config/storage/volumes.json` (Array-UUID, Dateisystem-UUID); Gerätenamen wie `sda` ändern sich zwischen Neustarts und werden nie gespeichert.
- Jede Mitgliedsplatte bekommt eine GPT mit einer RAID-Partition, die 128 MiB der Platte frei lässt, damit eine etwas kleinere Ersatzplatte noch passt.
- Arrays werden nach dem Start von udev zusammengesetzt, nicht im initramfs: mdadm ist dort bewusst nicht enthalten. titand bindet die Volumes ein, sobald ihr Array läuft, auch mit fehlender Platte.
- Im Entwicklungsmodus (`titand -dev`) verändert titand keine Platten.
- journald im RAM mit Größenbegrenzung.
- Firmware: gezielte Debian-Pakete aus `non-free-firmware` für Realtek/Broadcom-Netzwerk, QLogic und weitere Adapter sowie Intel-/AMD-Grafik; kein Sammel-Metapaket. Intel-GPU-Firmware (GuC/HuC) und Intel-NPU-Firmware sind Pflicht und werden vor dem Packen geprüft. Geräteunterstützung zusätzlich auf echter Hardware testen.
- RAM-Ziel: komprimiertes Systemabbild ca. 0,5–0,8 GB; Grundsystem mit allen Diensten (Kernel, titand, Samba/NFS, Docker-Engine, libvirt) ca. 1,0–1,5 GB, ohne Container und VMs. Plugin-Dateien liegen auf einem Volume; geladene Module und laufende Dienste benötigen dennoch RAM. Dies sind Zielwerte, keine aktuelle Hardware-Abnahme oder garantierte Mindestanforderung.

## Repo-Struktur (wichtige Einstiegspunkte)

```
titanos/
├── AGENTS.md
├── README.md
├── LICENSE.md              # FSL-1.1-ALv2
├── THIRD_PARTY_LICENSES
├── CONTRIBUTING.md         # inkl. CLA-Hinweis
├── CLA.md                  # Contributor License Agreement (Entwurf)
├── SECURITY.md             # Sicherheitslücken privat melden
├── .gitignore
├── build/                  # mkosi-Konfiguration (Pakete, mkosi.extra/, Postinst)
├── boot/                   # loader.conf, Eintragsvorlagen
├── backend/                # Go
│   ├── cmd/titand/
│   └── internal/
│       ├── api/            # HTTP + WebSocket
│       ├── auth/
│       ├── config/         # Lesen/Schreiben von /config
│       ├── storage/        # Plattenerkennung, mdadm, Dateisysteme, Mounts, SMART
│       ├── shares/         # Samba, NFS, Nutzer
│       ├── docker/         # Container, Compose, Vorlagen
│       ├── vm/             # libvirt über virsh, QEMU-Abbilder über qemu-img
│       ├── update/, app/   # System-/App-Updates und Rückfall
│       ├── assistant/      # Ollama, Chat-Ablauf, Verlauf, Websuche, Handbuch
│       ├── office/         # Word-, Excel-, PDF-Erzeugung und Dokumentlesen
│       ├── files/, backup/, configbackup/, backuptarget/
│       ├── plugins/, hardware/, tailscale/, usb/, ups/
│       ├── homeassistant/  # REST, WebSocket-Beobachter, MQTT-Sensoren
│       └── system/, network/ # Systemeinstellungen, Netzwerk, Dienste
├── web/                    # Svelte + TypeScript
├── scripts/
│   ├── make-zip.sh         # squashfs + Kernel + initrd + Bootloader → Zip
│   └── run-qemu.sh         # Test-Boot mit OVMF
├── .github/workflows/      # build.yml, release.yml
└── docs/
    ├── design/             # UI-Entwürfe (*.dc.html) + README, optional screenshots/
    └── brand/              # Logo und Schriftzug als SVG + README
```

## Build, Releases und Updates

- `make build` baut Svelte/TypeScript und bettet die statische Oberfläche in das statische Go-Binary ein. Aktuelle Toolchains: Go 1.27 und Node 24 (siehe `go.mod`, `package.json`, CI).
- Ein volles Release baut Debian mit mkosi, packt squashfs (zstd) und erzeugt mit Kernel, initrd und systemd-boot ein FAT32-Stick-ZIP. App-Releases packen das geprüfte Basis-Abbild mit neuem titand neu; Kernel, initrd, Bootloader und Plugins bleiben die der Basis. Jedes Release enthält weiterhin ein vollständiges ZIP und die drei Slot-Dateien für neue Sticks und ältere NAS.
- `scripts/release-kind.py` entscheidet anhand der Änderungen seit der Basis über App-/Systemrelease; neue Systempakete, Kernel, Dienste oder Build-Dateien erfordern ein volles Release. `[system]` in der Tag-Nachricht erzwingt es. **Die Versionsnummer entscheidet nicht über die Art:** 0.10.0 und 0.11.0 sind App-Releases auf Basis 0.9.0.
- `debian-security.yml` prüft wöchentlich Debian-Sicherheitsupdates und bereitet bei Bedarf einen PR für ein volles Release vor. App-Updates aktualisieren keine Debian-Pakete. Veröffentlichung nach Freigabe des Projektinhabers.
- Release-Ablauf, Lizenzinventare, Quellen und Abnahme: `docs/RELEASING.md`, `scripts/make-release.sh`, `.github/workflows/release.yml`.

### Gemeinsame Update-Regeln

- Quelle: GitHub Releases von `ra5on/TitanOS`, **nur Stable** seit 0.5.0. Alte Kanal-Einstellungen werden auf Stable migriert; keine Stable/Beta-Auswahl und kein Beta-Feed als aktueller Updateweg. Optionale Funktionen können weiterhin ein Beta-Label tragen (SMB-Ordnerkopie).
- Direkter Download von `https://github.com/ra5on/TitanOS/releases/latest/download/manifest.json`, ohne GitHub-API. Das interne Feld `source` erlaubt Spiegel/Tests, niemals das Umgehen der Signaturprüfung.
- Manifest: Version, Tag, Kanal, Kernel, Veröffentlichungsdatum, Mindestversion, Changelog, Slot-Kommandozeile und Dateien mit Größe/SHA-256, zusätzlich Plugin-Inventar und optional `app` (`base`, `asset`, `sha256`, `size`).
- **Ohne gültige Signatur keine Installation, auch nicht mit Warnung oder manuell.** Signierte Manifeste werden gegen `/usr/share/titanos/update-keys/*.pub` geprüft, danach Größe und SHA-256 jeder Datei. Downloads unterstützen HTTP-Range, ohne große Zwischenablage im RAM.
- Im Abbild: `titanos-release-1.pub` (ID `8EAABC2A42904C1D`) und `titanos-recovery-1.pub` (ID `E58A91B20B598ABB`). Private Release-Geheimnisse nur im geschützten GitHub-Environment `release`, Reserve beim Besitzer. Externe Sicherung am 14. Oktober 2026 bestätigt; Offline-Verwahrung der Reserve noch nicht bestätigt. Schlüsselwechsel vorab per Update vorbereiten.
- Einstellungen in `/config/system/update.json`: automatische Installation mit Uhrzeit, tägliche Prüfung. Nach vollständig erfolgreicher Besitzer-/Administrator-Anmeldung zusätzlich `CheckAfterLogin` im Hintergrund (90 Sekunden, gleichzeitige Anmeldungen zusammenfassen); keine Prüfung bei Benutzerkonten, offener 2FA oder Seitenneuladen, keine automatische Installation durch Anmeldung, kein Prüfen im Entwicklungsmodus. Meldung verlinkt `/settings/updates`; Fehler blockieren die Anmeldung nicht.
- Neue Konfigurationsfelder müssen von Basis und voriger App-Version weiterhin lesbar sein; keine inkompatiblen Umbenennungen/Pflichtfelder. Rückwege einschließlich Plugin-Einstellungen erhalten.

### Volle Systemupdates (A/B)

- Drei Slot-Dateien direkt in den inaktiven Slot schreiben, passende offizielle Plugins **vor** dem Slot-Wechsel vorbereiten. Plugin-Dateien und Einstellungen nach Basis-Version getrennt halten.
- `loader.conf` hat kein festes `default`. `sort-key` `titanos-1` bevorzugt den neuen Slot, `titanos-2` den Rückfall; neuer Eintrag `titanos-<slot>+3.conf` für drei Versuche.
- `titanos-boot-check.service` ruft `titand -check-boot` auf. Bis zu fünf Minuten auf `/api/v1/health` warten: Konfiguration gelesen, Volumes versorgt, zuvor eingebundene Volumes wieder eingebunden; aktivierte Plugins gehören zum Healthcheck. Dann Zähler durch Umbenennen in `titanos-<slot>.conf` entfernen. **Kein `systemd-bless-boot`**. Fehler führt über `FailureAction=reboot` zum nächsten Versuch, nach drei Fehlstarts zum alten Slot.
- `/config/system/update-pending.json` enthält die Boot-ID; bloßer Dienstneustart entscheidet keinen Systemwechsel. Ergebnis nach Neustart: installiert, fehlgeschlagen oder zurückgewechselt.
- Rückwechsel tauscht die `sort-key` und läuft ebenfalls auf Probe; vor dem Neustart abbrechbar („Doch nicht“).
- Der Bootloader `EFI/BOOT/BOOTX64.EFI` wird von Updates weiterhin nicht ersetzt; manuelle Systeminstallation aus einer Datei ist nicht umgesetzt.

### App-Updates ohne NAS-Neustart

- Paket `app`, Details `docs/APP-UPDATES.md`. Bei passender `/usr/share/titanos/base-version` wird nur titand mit Weboberfläche geladen; sonst volles Systemupdate.
- Ablage `titanos/<slot>/app/`: höchstens aktuelle/vorige App samt signierten Manifesten und `state.json`. Basis-titand prüft Signatur, Basis und Hash, kopiert nach `/var/lib/titanos/app/` im RAM und startet per `exec`. Nicht aus `/run` ausführen (noexec).
- Probe über `titand -check-app` / `titanos-app-check.service`; Rückfall nach drei Fehlstarts oder nach fünf Minuten ohne erfolgreichen Healthcheck, zuerst vorige App, sonst Basis. Rückwechsel ohne NAS-Neustart; volle Updates leeren den App-Ordner des Ziel-Slots.
- Installation wartet auf laufende Kopier-/Verschiebeaufträge, Backups, Wartungsaufträge, offene RAID-Erweiterungen, Plattenprüfungen, VM-Plattenkopien/Klone, Snapshots, NVIDIA-Builds und ungeklärte Anwendungssicherungen. Ein titand-Neustart ist kein NAS-Neustart; nur ausdrücklich dauerhaft protokollierte Aufträge überleben ihn. RAM-Aufträge und offene Verbindungen nicht als dauerhaft behandeln.
- Anmeldungen überleben den Dienstneustart in `/run/titanos/sessions.json` (nur root), nicht den NAS-Neustart. Unverändertes Netzwerk, Docker und NUT bleiben laufen; `/run/titanos/vms-autostarted` verhindert erneuten Autostart absichtlich gestoppter VMs. Kernreservierungen werden abgeglichen.
- Plugin-Katalog, NVIDIA-Bauumgebung und Treiber richten sich nach der **Basis-Version**, nicht der angezeigten App-Version.

### Release-Prüfung und Grenzen

- Test-Builds verwenden Wegwerf-Schlüssel, die nie veröffentlicht werden. Tag-Builds verlangen zwei verschiedene echte öffentliche Schlüssel; nur der geschützte Publish-Job erhält das Release-Secret. ZIP, Manifest und Prüfsummenliste werden signiert.
- QEMU-Smoke-Tests prüfen das tatsächliche ZIP, System-Rückfall sowie App-Updates mit erhaltener Anmeldung, Fehlstart-Rückfall und Rückweg zur Basis. Bei App-Releases wird die bereits geprüfte Basis neu gepackt; nicht alle Storage-/Hardwaretests werden erneut ausgeführt.
- Historische GitHub-Update-/Rollback-Prüfungen und Hardwaremeldungen sind dokumentiert, aber keine pauschale Abnahme jedes neuen Releases. Neue Prüfungen und ihre Grenzen ausdrücklich festhalten.

## Testen

- QEMU mit OVMF (UEFI). Stick-Inhalt als FAT-Laufwerk: entweder `-drive file=fat:rw:<ordner>` (vvfat, Schreibzugriff experimentell) oder ein per mtools erzeugtes FAT-Image.
- Zusätzliche virtuelle Platten für RAID-Tests (Ausfall simulieren, Rebuild prüfen).
- Tests auf echter Hardware gezielt ergänzen. Bestätigte frühere Tests gelten nur für den dokumentierten Umfang, nicht automatisch für neue Funktionen.
- `make test`: Go vet/Tests, Webtests und Svelte-Prüfung. `make test-release`: Paketierungs-/Signaturtests mit echten Werkzeugen. UI-Prüfskripte in `scripts/test-ui-*.mjs`; ausschließlich isolierte Testdaten verwenden. Keine Host-Platten formatieren, keine Host-Sysctls für Tests ändern.

## KI-Assistent und Agent (0.15.0)

- Maßgebliche aktuelle Beschreibung: `docs/ASSISTANT.md`, Ausbau und Grenzen von 0.15.0 in `docs/ASSISTANT-EXPANSION.md`. Persönliches Wissen, Vorlagen/Projektnotizen und Modelltests je Konto auf Volume; keine automatische Übernahme von Quellenanweisungen. Eigene Auftragszentrale mit Prioritäten und geprüften Abhängigkeiten, weiterhin genau eine Ausführung. PDF/OCR mit festen Argumenten und begrenzten RAM-Pipes. Dokumentbearbeitung schreibt neue Kopien mit Ausgangshash; Excel-Formelzellen geschützt, Neuberechnung durch Tabellenprogramm. Alte Release-Notizen sind historische Berichte, keine widersprechenden Regeln.
- Ab Werk aus, Ollama mit Werkzeugmodell; Seite `/assistant` mit Automationen unter `/assistant/automations`. Kein externer MCP-Server, keine Bearer-API-Schlüssel, keine separate Agent-App.
- **Backend besitzt die Aufträge und führt Änderungen aus.** Browser nur Anzeige, Freigabe und Steuerung; Browserergebnisse niemals als Ausführungsnachweis übernehmen. Seite schließen beendet keinen Auftrag.
- Standard „mit Bestätigung“: Karte, bei Gefahr Häkchen, bei Zerstörung Zielname. Ein genehmigter Plan erlaubt folgende nicht zerstörerische Aktionen. **Optional „Direkt ausführen“ ohne weitere Bestätigungen, auch für zerstörerische Änderungen.** Diese bewusste Entscheidung des Projektinhabers gilt innerhalb der Rollen-/Bereichsrechte; keine zusätzliche pauschale Bestätigungsschicht ergänzen. Fachlich unklare Ziele weiterhin klären.
- Rollen, Freigaberechte und Bereiche werden serverseitig geprüft. Benutzer nur mit ausdrücklicher Freigabe und innerhalb ihrer Dateien. Konten/System/Updates ab Werk nur lesen. Feste API-Liste, keine Shell/Geheimnisrouten; Netzwerkbestätigung bleibt manuell außerhalb des Agenten. Prompt und Direktmodus erweitern keine Rechte.
- Unbegrenzte Auftragsdauer und `maxSteps=0` ohne 500-Runden-Sicherung. Endliche Fristen nur je Modell-/Werkzeugaufruf; Wiederholungen ohne Fortschritt erkennen. Aktuell eine aktive Ausführung; weitere Aufträge warten. Pause/Stopp/Ergänzen/Fortsetzen und `agent_wait` für Voraussetzungen.
- Checkpoints plus Ausführungsprotokolle auf dem Volume. Vor Änderungen dauerhaft speichern, anschließend echte Ergebnisse. Nach Absturz gespeicherte Ergebnisse übernehmen; unbekannte Ausgänge blockieren, niemals automatisch nochmals ausführen. Originalauftrag und Mutationssperren bei Kontextverkürzung erhalten. Keine Regeneration bereits versuchter Änderungen.
- Vollständiger editierbarer Standardprompt mit Version, fünf Vorgängern, Reset und Kontextvorschau; explizite Präferenzen, Modellprofile, synthetischer Modelltest ohne NAS-Zugriff. Inhalte externer Quellen sind Daten, keine Handlungsberechtigung.
- Docker liefert pro Image Erfolg/Fehler/unbekannt/fest gebunden und Prüfzeit. Fehler nie in „keine Updates“ umdeuten. Dateiwurzeln exakt entdecken, Liste/Suche mit Folgeseiten und Grenzen; Foto-Filter einschließlich HEIC/RAW. API-Erfolg und fachlich geprüfter Endzustand unterscheiden.
- Dokumente und Dateioperationen wie beschrieben in `docs/ASSISTANT.md`; Probelauf führt keine Änderungen aus. Undo nur für die tatsächlich vom Dateimanager unterstützten Operationen.
- Einstellungen atomar auf `/config`, im Betrieb aus RAM. Chats separat je Konto auf Daten-Volume (0700), niemals zyklisch auf den Stick. Ohne Volume/temporär nur RAM; laufende dauerhafte Aufträge bei Volume-Verlust blockieren. Höchstens 200 Chats/12 MiB je Chat, aktive/fortsetzbare nicht verdrängen. Optionale zeitbasierte Aufbewahrung nur für abgeschlossene/gestoppte Chats. Fremde Chats auch für Administratoren unzugänglich.
- Werkzeughärtung seit 0.12.1: zentrale Schemaprüfung vor jedem Aufruf, fehlerhafte Argumente niemals in `{}` umwandeln; unvollständige Modellstreams nicht ausführen. Explizite Ollama-Tool-Parserfehler höchstens zweimal neu generieren, keine Wiederholung bereits ausgeführter Änderungen. Fehlerkategorien im Metadatenexport; identische ungültige/unbekannte Aufrufe zählen zur Schleifenerkennung. Details und Prüfgrenzen in `docs/ASSISTANT.md`.
- Automationen je Konto auf dem Daten-Volume: einmalig, Intervall, täglich/wöchentlich mit Zeitzone sowie explizite Meldungs-/USB-/Volume-/Ordnerereignisse. Verwaltung in UI und über Agentenwerkzeuge mit derselben API. Erstbeobachtung setzt die Basis; keine rekursive Hintergrundindexierung. Ausführung übernimmt die globale Einstellung oder den ausdrücklich gewählten Modus. Rollen/Bereiche gelten weiterhin. Kein RAM-Ersatz für dauerhafte Automationen; atomare Reservierung einer Chat-ID samt Auftrag vor dem Start. Verpasste Termine/Überschneidungen haben ausdrückliche Regeln, Details und Beobachtungsgrenzen in `docs/ASSISTANT.md`.
- Einstellungswechsel beendet laufende Modellaufrufe und reiht aktive Aufträge neu ein; globales Pausieren/Deaktivieren pausiert sie. Direktmodus übernimmt wartende Bestätigungen außer Probeläufen und ausdrücklich bestätigungspflichtigen Automationen. Keine Wiederholung unbekannter Mutationen. Sichtbare Laufphasen und Schrittzeiten; Fristen nur je Aufruf bzw. bei fehlendem Stream-Fortschritt.
- Aktionskörper strukturell und ohne Ganzzahl-Präzisionsverlust vergleichen. Unausgeführte veraltete Vorschläge verfallen; Ausführungsprotokolle bleiben erhalten. Dokumentauftrag verwendet vorhandene Freigaben, legt nicht ohne Auftrag eine neue an. Gespeicherte Dokumentbytes gegen den Hash der Erzeugung prüfen; Word/Excel zusätzlich parsen. Fachliche Richtigkeit benötigt weiterhin echte Quellen.
- **Benachrichtigungen pro Konto (0.14.0):** eigene Wege je Konto in `/config/users/<konto>/notify.json` (E-Mail über den gemeinsamen SMTP-Server an die eigene Adresse, ntfy, Telegram, Discord, Home-Assistant-App), Ereignisse `confirmation`/`done`/`blocked`/`agent`, Inhalt `summary` (höchstens 300 Zeichen der letzten Antwort bzw. der Begründung einer Aktion) oder `none`, Ruhezeiten mit Sammelnachricht (standardmäßig RAM, optional auf Daten-Volume; nie Freigabetoken persistieren). Bis drei Sendeversuche, optionaler Ersatzkanal und persönlicher Annahmestatus; keine garantierte Endzustellung. Je Auftrag/Ereignis/Aktion eine Meldung. Geheimnisse nur „ist gesetzt“. Der gemeinsame Admin-Kanal bleibt ohne Chatinhalte. Automationen `errors` melden auch wartende Freigaben.
- Werkzeug `notify_me`: nur an den Besitzer des Chats, 1/min und 20/h, im Probelauf nichts senden, nur angeboten, wenn das Konto einen Weg und das Ereignis `agent` hat. Keine Aktion (ändert nichts am NAS).
- **Freigabe aus Benachrichtigungen** (Einstellung `pushApproval`, ab Werk aus): ntfy-`http`-Aktionen bzw. Aktionen der HA-App (`mobile_app_notification_action`). Einmal-Token 256 Bit nur im RAM, 15 Minuten, an Konto/Chat/Aktion gebunden; `POST /api/v1/assistant/push/{token}/{approve|reject}` ohne Anmeldung, danach derselbe Freigabeweg wie in der Oberfläche. Nie für `destroy`.
- Statistik je Chat (`stats`: Runden, gelesene/geschriebene Token, Modell-/Werkzeugzeit) unter dem Chat und im Metadatenexport. Feldnamen ohne „token“, sonst schwärzt der Diagnose-Redaktor sie.
- Vorlagen für Automationen (`web/src/lib/automation-templates.ts`) füllen nur das Formular. Dashboard-Kachel `assistant` mit letzten Aufträgen und nächsten Automationen.
- Metadaten-Diagnoseexport, Inhalte nur ausdrücklich; Anhänge nicht als vollständig anonymisiert darstellen. Administrator-Meldungen ohne Chatinhalte. Websuche optional, SSRF-Prüfung bei jeder Verbindung/Weiterleitung beibehalten.
- Prüfungen verwenden isolierte Daten und nachgebautes Ollama. Keine pauschale Abnahme echter Modelle, Hardware oder Crash-Verhalten behaupten; konkrete Prüfgrenzen in den Release-Notizen nennen.

## Web-UI

Bedienkonzept angelehnt an Unraid, Gestaltung eigenständig.

**Entwürfe:** `docs/design/` enthält die Quelltexte aller Entwurfsseiten (`*.dc.html`, Übersicht in `docs/design/README.md`). Vor der Arbeit an einer Seite die passende Datei lesen und Aufbau, Abstände, Farben (siehe `Appearance.dc.html` für alle Theme-Werte) und Verhalten übernehmen – nicht den Code, der nutzt ein Entwurfsformat mit eigener Laufzeit. Beispieldaten darin nie ins Produkt übernehmen. Screenshots, falls vorhanden, in `docs/design/screenshots/`.

**Grundaufbau**
- Kopfzeile: Logo (Symbol + Schriftzug, siehe „Marke“), Reiter-Navigation (Dashboard, Speicher, Shares, Dateien, Nutzer, Docker, VMs, Apps, Plugins, Einstellungen, Werkzeuge), Terminal, Meldungen, Konto-Menü. Kein Hostname/IP-Badge neben dem Logo.
- **Konto-Menü** (Klick auf den eigenen Namen oben rechts, mit Initiale in Akzentfarbe): Name und Rolle, Mein Konto (Passwort, 2FA, SSH-Schlüssel), Darstellung, Angemeldete Geräte, NAS neu starten …, NAS herunterfahren …, Abmelden. Neustart/Herunterfahren nur für Besitzer/Administratoren und immer mit Bestätigungsdialog, der zeigt, was passiert: laufende Container/VMs werden sauber beendet, laufende Speicherwartung kann unterbrochen werden; Plattentests überstehen keinen Neustart, Wiederanlauf von RAID-Wartung nicht mit fortsetzbaren Plattentests verwechseln, Option „erst nach Abschluss des Wiederaufbaus“, Hinweis auf Wake-on-LAN beim Herunterfahren.
- Kopfzeile immer einzeilig mit fester Höhe (ca. 60 px). Reicht die Breite nicht, scrollt die Navigation seitlich mit sichtbarer Scrollleiste, Links-/Rechtspfeilen und Ziehen mit gedrückter Maustaste. Normale Link-Klicks, Tastatur und Touch bleiben bedienbar.
- Statusleiste unten: Volume-Zustand, laufende Rebuilds/Prüfungen, Version und System-Slot.
- Seitenkopf und leerer Zustand sind gemeinsame Komponenten: `PageHead` (Titel, optional Weg zurück wie „Einstellungen › …“, Zeilen darunter, Knöpfe rechts) und `EmptyState` (eigene Kachel oder als Zeile in einer Liste mit `inline`).
- **Bestätigungsdialoge:** Die gefährliche Aktion (Löschen, Entfernen, Herunterfahren, Überschreiben …; `.btn-danger`/`.btn-warn`) steht immer **links** von „Abbrechen“. Das regelt `Dialog.svelte` für jede `.dialog-actions`-Leiste per CSS-`order`; im Markup steht „Abbrechen“ zuerst, damit es den Fokus bekommt. Enter oder Autofokus lösen nie die gefährliche Aktion aus, solange nichts ausdrücklich bestätigt ist (Name eintippen, Häkchen, Passwort). Dialoge immer mit `Dialog.svelte` und `.dialog-actions` bauen, nicht mit eigenem `<dialog>`.
- Schrift: IBM Plex Sans für Text, IBM Plex Mono für Werte, Pfade, Gerätenamen (lokal ausliefern, keine Google-Fonts-Abhängigkeit im Produkt).
- Mobil: bis 760 px Hauptnavigation über einen Menüknopf mit aktuellem Bereich und aufklappbaren Links (native Popover-Funktion, Escape/Außenklick schließt). Desktop behält die seitlich scrollbare Reiterleiste. Kacheln stapeln, breite Tabellen horizontal scrollbar.

**Marke** (Entwurf `Brand.dc.html`, Dateien in `docs/brand/`)
- Symbol: Sechseck-Rahmen mit „T“ (mit Fuge zwischen Balken und Stamm). Schriftzug „TITANOS“ aus eigenen geometrischen Formen (keine Schriftart, keine Lizenzfrage): gleiche Strichstärke, „A“ ohne Querstrich, „T“ mit Fuge.
- Rahmen und „OS“ immer in `--accent`, Buchstaben in `--text`. Im hellen Theme Akzent abdunkeln (Kontrast ≥ 3:1).
- Als Svelte-Komponente mit CSS-Variablen umsetzen. Favicon: Akzentquadrat mit „T“.

Die Einstellungsübersicht zeigt kompakte Kacheln ohne Beschreibung, alphabetisch nach dem übersetzten Titel sortiert. Datum und Uhrzeit ist eine eigene Seite, Terminal/Konsolen liegen unter Zugang. „Sicherungen“ bündelt Datensicherung, Systemkonfiguration und Wiederherstellung mit vorhandenen Status-/Zeitplanwerten; alte Links werden weitergeleitet. Kein Live-Import der Systemkonfiguration in ein bereits eingerichtetes NAS. Suche und Meldungslinks führen gezielt zum passenden Bereich; Fehler bieten Wiederholen, Speicherbestätigungen stehen am betroffenen Feld. Details in `docs/design/README.md`. Meldungen können einzeln oder als angezeigte Liste gelöscht werden (nur Besitzer/Administratoren, für alle Sitzungen; neue Meldungen währenddessen bleiben erhalten).

**Konten und Rechte**
- Ersteinrichtung legt das erste Konto mit **frei gewähltem Namen** an (z. B. „alex“). Dieses Konto ist **Besitzer** mit vollen Rechten – praktisch wie root – und sein Name steht oben rechts. Kein festes „admin“.
- Der Besitzer ist **nicht** der Linux-Benutzer root: root ist gesperrt (kein Passwort-Login), SSH ist standardmäßig aus. Wird SSH aktiviert, meldet man sich mit dem eigenen Namen und SSH-Schlüssel an und nutzt `sudo`. Gründe: kein erratbarer Login-Name, jede Aktion im Protokoll einer Person zuordenbar.
- titand läuft als Systemdienst mit den nötigen Rechten und führt Aktionen im Namen des angemeldeten Kontos aus. Die NAS-Shell im Terminal ist eine root-Shell für angemeldete Besitzer/Administratoren; auf ausdrücklichen Wunsch des Projektinhabers ohne zweite Passwort-/2FA-Abfrage. VM-Konsolen und andere sensible Aktionen behalten ihre jeweiligen Bestätigungsregeln; Docker-Konsolen nutzen ebenfalls die bestehende Anmeldung.
- Rollen: **Besitzer** (alles, mindestens einer muss existieren), **Administrator** (alles außer Besitzer verwalten), **Benutzer** (nur eigene Shares/Dateien, kein Docker/VMs/System). Kontoname wird auch als SMB-Benutzer angelegt.
- Konten, Passwort-Hashes (argon2id) und 2FA-Geheimnisse in `/config/users/`. Anmeldeschutz gegen Durchprobieren.
- Umgesetzt: Zwei-Faktor-Anmeldung mit TOTP (RFC 6238, sechs Stellen, 30 Sekunden, ±1 Schritt, jeder Code nur einmal) und acht Wiederherstellungscodes, von denen nur Hashes gespeichert werden. Das Geheimnis gilt erst, wenn ein Code daraus geprüft wurde. Der Code wird beim Anmelden nach dem richtigen Passwort verlangt, außerdem bei einer angeforderten erneuten Bestätigung für VM-Konsolen und beim Deaktivieren der Zwei-Faktor-Anmeldung; NAS-Shell und Docker-Konsolen verwenden die bestehende Administrator-Anmeldung. Administratoren können den zweiten Faktor eines anderen Kontos zurücksetzen (alle seine Anmeldungen enden dabei).
- Umgesetzt: Seite „Mein Konto“ (`/account`) mit Passwort, Zwei-Faktor-Anmeldung und „Angemeldete Geräte“ (Anmeldungen liegen im RAM, mit Dienstneustart-Persistenz in `/run/titanos/sessions.json`; einzelne lassen sich beenden).
- Umgesetzt: SSH-Schlüssel je Konto unter „Mein Konto“ (`sshKeys` in `account.json`, eine Zeile je Schlüssel, Optionen vor dem Schlüssel werden abgelehnt), SSH-Zugang und eigenes Zertifikat, siehe „Zugang“.

**Dashboard mit frei anpassbaren Kacheln**
- Raster aus 12 Spalten mit festen Zeilenhöhen. Kacheln rasten auf Spalten/Zeilen ein, keine freien Pixelgrößen.
- Bearbeiten-Modus (Schalter auf dem Dashboard): Kacheln am Griff verschieben, an der Ecke Größe ändern, ausblenden, über „Kachel hinzufügen“ zurückholen, „Standard wiederherstellen“. Außerhalb des Modus ist alles fixiert.
- Layout pro Nutzer in `/config` gespeichert. Auf schmalen Bildschirmen wird automatisch gestapelt.
- Umsetzung mit gridstack.js. Das aktuelle Dashboard nutzt Layoutversion 3 mit getrennten CPU-/RAM-/Systemkacheln. Erkannte iGPU/GPU-, NPU- und NVIDIA-Kacheln werden bei Layouts vor Version 2 ergänzt; danach bleibt die bewusste Auswahl erhalten. Im Bearbeiten-Modus lassen sich mehrere einzelne VMs und Docker-Container mit CPU/RAM und Zustand hinzufügen; Auswahl nach VM-/Containername, kein Ersatz durch ein anderes Objekt bei Löschung. Fehlende Messwerte werden nicht erfunden.

**Themes und Farben (pro Nutzer wählbar, Seite Einstellungen › Darstellung)**
- Themes: Graphit (Standard), Schwarz (Tiefschwarz, hoher Kontrast), Mitternacht (Nachtblau), Hell, Automatisch (folgt `prefers-color-scheme`: Hell bzw. Graphit), **Eigenes** (Farbwähler für Hintergrund, Flächen, Kopfzeile, Text, Linien; gedämpfter Text und Chips werden abgeleitet; Kontrastwarnung unter 4,5:1).
- Akzentfarben: Blau, Grün, Türkis, Violett, Kupfer, Rosé, plus **eigene Farbe** per Farbwähler. Textfarbe auf Akzentflächen automatisch dunkel oder weiß, je nach Kontrast.
- **Stil der Flächen:** Flach (deckend), **Glas** (durchscheinend wie „Liquid Glass“: `backdrop-filter: blur() saturate()`, halbtransparente Fläche, helle Innenkante oben), Kontur (nur Linien).
  - Glas-Regler: Transparenz (0–90 %), Unschärfe (0–40 px), Glanzkante (0–60 %). Transparenz wird automatisch begrenzt, damit Text lesbar bleibt (dunkle Themes Deckkraft mindestens 35 %, helle mindestens 50 %). Fallback ohne `backdrop-filter`: deckende Fläche. `prefers-reduced-transparency` respektieren.
- **Ecken:** Regler für Kacheln/Fenster (0–32 px) und für Knöpfe/Felder/Menüs (0–22 px) – gilt überall.
- Schriftgröße 90–120 %, Dichte Normal/Kompakt.
- **Hintergrund:** Einfarbig, Formen (weiche Farbflächen, eine davon in Akzentfarbe), eigenes Bild (Upload, in `/config` gespeichert, Regler „Abdunkeln“). Glas wirkt nur mit Hintergrund – Hinweis anzeigen, wenn Glas + Einfarbig.
- Theme als JSON exportieren/importieren. „Standard wiederherstellen“.
- Technisch: alle Werte als CSS-Variablen (`--bg`, `--surface`, `--header`, `--border`, `--text`, `--muted`, `--track`, `--chip`, `--accent`, `--on-accent`, `--surface-alpha`, `--glass-blur`, `--glass-shine`, `--radius-tile`, `--radius-ctl`, `--font-scale`), Theme als `data-theme`, Stil als `data-surface="flat|glass|outline"` auf `<html>`. Kein hart codierter Farbwert oder Radius in Komponenten. Kleine Teile nutzen die daraus abgeleiteten `--radius-small` (Chips, Marken) und `--radius-bar` (Balken); nur kreisrunde Elemente (`50%`, Pillen) bleiben fest. Farben, die in jedem Theme gleich sein müssen (QR-Code, Abdunkeln des Hintergrundbilds), stehen als Variablen in `app.css`.
- Statusfarben sind in allen Themes gleich: OK grün, Warnung gelb, Fehler rot, inaktiv grau. Status nie nur über Farbe, immer mit Text.
- Kontrast mindestens 4,5:1 für Text in jedem Theme.

**Energie** (Einstellungen › Energie)
- Liest die tatsächlich verfügbaren CPU-Governors, Energy Performance Preferences und ACPI-Plattformprofile aus sysfs. Bei mehreren CPU-Gruppen werden nur gemeinsam unterstützte Werte angeboten; keine künstlichen Profile oder festen Taktgrenzen. CPU-Governor `userspace` wird ohne manuelle Taktsteuerung nicht angeboten.
- Nur Besitzer/Administratoren dürfen Profile ändern. Auswahl in `/config/system/energy.json`, beim Start erneut angewendet; Status liest nur sysfs und RAM. Der Entwicklungsmodus ist ausschließlich lesend. Bei Schreib- oder Speicherfehlern werden vorherige Werte wiederhergestellt; Fehler werden angezeigt.
- Die Wahl „Nicht beim Start setzen“ beendet die Verwaltung dieses Werts; der aktuelle Wert bleibt bis zu einer anderen Änderung oder zum Neustart erhalten. Governor und EPP hängen vom CPU-Treiber ab: `intel_pstate` erlaubt bei `performance` keine stromsparende EPP.
- Schnittstellen: [CPUFreq](https://docs.kernel.org/admin-guide/pm/cpufreq.html), [Intel P-state](https://docs.kernel.org/admin-guide/pm/intel_pstate.html), [Plattformprofile](https://docs.kernel.org/userspace-api/sysfs-platform_profile.html). Tests nutzen künstliche sysfs-Dateien; die Wirkung auf echte NAS-Hardware bleibt zu prüfen.

**Zeitplan und Wake-on-LAN** (Einstellungen › Energie, 0.14.0, Paket `system`, `power_schedule.go`)
- Eigene Datei `/config/system/power-schedule.json` (Herunterfahren/Einschalten je Wochentage und Uhrzeit, Wake-on-LAN), Route `/api/v1/system/power-schedule`. Energieprofile bleiben unverändert in `energy.json`.
- Herunterfahren wartet auf laufende Arbeit (`update.Working`, Wiederaufbau/Prüfung, Agentenauftrag) bis zu sechs Stunden (Meldung `power_postponed`), danach wird dieser Termin ausgelassen (`power_skipped`).
- Einschalten über `/sys/class/rtc/rtc0/wakealarm` (zuerst `0`, dann Unix-Zeit), gesetzt beim Start, bei Änderung und direkt vor dem geplanten Herunterfahren. Ohne RTC nicht anbietbar.
- Wake-on-LAN per ethtool-ioctl (`ETHTOOL_GWOL`/`SWOL`, `WAKE_MAGIC`) auf physischen Anschlüssen, bei jedem Start und jeder Änderung. **Keine `.link`-Datei:** sie würde `99-default.link` und damit die Namensregeln von udev ersetzen.
- Entwicklungsmodus: speichert, verändert aber weder Hardware noch schaltet aus. Echte RTC-/WoL-Wirkung ist hardwareabhängig und nicht geprüft.

**Festplatten-Ruhemodus** (Speicher › Ruhemodus für Festplatten, nur Besitzer und Administratoren)
- Eine gemeinsame Leerlaufzeit für alle unterstützten HDDs: Aus (Standard), 5, 10, 15 oder 30 Minuten sowie 1, 2 oder 3 Stunden. Jede Platte wird getrennt beobachtet und wacht beim nächsten Zugriff wieder auf. Aus beendet die automatische Steuerung, weckt bereits schlafende Platten aber nicht auf.
- Einstellung in `/config/storage/disk-sleep.json`; nur die Dauer wird gespeichert, niemals Gerätenamen. Der Zustand und die Zeitmessung liegen im RAM. Änderungen werden atomar gespeichert; nach Neustart, geänderter Einstellung oder einer Beobachtungslücke beginnt eine volle neue Ruhezeit.
- `storage.RunDiskSleep` liest alle zehn Sekunden die I/O-Zähler aus `/sys/block/*/stat`. Lese-, Schreib-, Discard- und Flush-Vorgänge sowie laufende I/O setzen die Zeit zurück. Keine zyklischen Zugriffe auf `/config`, keine Plattendaten-Abfragen zur Zeitmessung. Die vorhandenen SMART-Abfragen nutzen `--nocheck=standby`.
- Unterstützt sind als nicht wechselbar gemeldete SATA-/SAS-/SCSI-HDDs und USB-HDDs mit passendem Adapter. SSDs, NVMe, Bootstick, Virtio- oder netzgebundene Platten, an VMs vergebene Platten sowie Platten mit laufendem Test oder RAID-Wartung bleiben ausgenommen. Apps, Freigaben und andere Zugriffe können das Einschlafen verhindern.
- Das Debian-Paket `hd-idle` erhält ausschließlich einzelne Standby-Aufrufe (ATA bei SATA, SCSI bei SAS/USB; SAS mit Power Condition 3 zum automatischen Aufwachen). `hd-idle.service` ist maskiert. Firmware-Timer werden nicht verändert. Ein fehlgeschlagener Aufruf erscheint auf der Speicher-Seite und wird erst nach neuer Aktivität oder einer Einstellung erneut versucht. Im Entwicklungsmodus werden keine Standby-Befehle ausgeführt.
- Regressionstests prüfen alle Zeiten, Aktivität, Gerätetausch, Ausschlüsse, fehlerhafte Konfiguration/I/O-Zähler und API-Rechte. Der Release-Smoke-Test prüft Standard Aus und Speicherung über zwei UEFI-Starts. Einschlafen und Aufwachen auf echter Hardware einschließlich USB-Adaptern müssen noch getestet werden.

**Backups** (Einstellungen › Sicherungen › Datensicherung, nur für Besitzer und Administratoren)
- Paket `backup`, Werkzeug restic (Debian-Paket, rund 24 MB). Ein Auftrag sichert Freigaben an ein Ziel: anderes Volume dieses NAS (nie dasselbe wie das der Freigaben), S3-kompatibler Speicher, ein Server über SFTP oder ein restic-REST-Server. Aufträge in `/config/backup/jobs.json`.
- Jeder Auftrag bekommt beim Anlegen ein zufälliges Passwort, mit dem restic verschlüsselt. Die Seite zeigt es direkt danach und später nach erneuter Passwort-Eingabe – ohne dieses Passwort ist ein Backup wertlos, wenn das NAS verloren geht. Geheimnisse des Ziels (Secret Key, Passwort des REST-Servers) gibt die API nie heraus; sie stehen nur in der Umgebung von restic, nie auf einer Kommandozeile.
- SFTP: Das NAS hat einen eigenen Schlüssel (`/config/backup/ssh/id_ed25519`), den die Seite zum Eintragen auf dem Server zeigt. Der Server wird beim ersten Verbinden gemerkt (`known_hosts`); meldet sich später ein anderer unter dem Namen, bricht das Backup ab. Während eines Laufs liegen Schlüssel und `known_hosts` in `/run/titanos-backup`.
- Zeitplan wie unten beschrieben; ein verpasster Termin wird nach dem Start nachgeholt, das erste Backup beginnt gleich nach dem Anlegen. Aufbewahren: je der neueste von N Tagen, Wochen, Monaten (`restic forget --prune`). Ein Auftrag nach dem anderen.
- Ausgelassen werden `.recycle` und halbe Uploads (`*.titanos-part`). Der Cache von restic und was ein Auftrag über seine Läufe weiß, liegen auf dem Volume der ersten Freigabe (`.titanos/backup/`), nicht im RAM und nicht auf dem Stick.
- Wiederherstellen: ein Stand in einen neuen Ordner `/mnt/<volume>/restored/<auftrag>-<zeit>/`, nie über die Freigabe. Was man braucht, holt man sich von dort.
- Abbrechen beendet restic samt ssh (eigene Prozessgruppe). Fehlgeschlagene Läufe ergeben eine Meldung (`backup_failed`); konnte restic einzelne Dateien nicht lesen, gilt das Backup mit Warnung.
- Getestet in QEMU: Backup auf ein zweites Volume, zweiter Lauf, Wiederherstellen (Prüfsumme gleich, Freigabe unberührt), das Passwort öffnet das Repository auch von Hand, SFTP zum NAS selbst mit dessen Schlüssel, unerreichbarer Server. Nicht getestet: S3 und REST-Server gegen echte Dienste (nur Adresse und Umgebung im Unit-Test).
- **Zeitplan** (`backup/schedule.go`): stündlich (alle 1, 2, 3, 4, 6 oder 12 Stunden), täglich mit bis zu acht Uhrzeiten, an gewählten Wochentagen oder monatlich (Tag 1–28); optional ein Zeitfenster (`windowFrom`–`windowTo`, darf über Mitternacht gehen), in dem Läufe beginnen – ein fälliger Lauf wartet, bis es öffnet, ein laufender wird nicht abgebrochen. Ein fehlgeschlagener Versuch (zum Beispiel ist das andere NAS aus) wird nach 30 Minuten wiederholt, bis der fällige Termin erfüllt ist. Die Seite zeigt den nächsten Lauf (`next`). Aufträge aus der Zeit davor (`every` daily/weekly mit `weekday` und `time`) laufen unverändert; leere Listen bedeuten „das eine `weekday`“ bzw. „die eine `time`“.
- **Auswahl** (`backup/selection.go`): je Freigabe ganz, „alles außer …“ (`skip`: `freigabe/ordner`) oder „nur ausgewählte Ordner“ (`folders`), dazu Muster (`patterns`, zum Beispiel `*.tmp`) und eigene Bereiche (`extras`): `appdata` (`/mnt/<volume>/appdata` jedes eingebundenen Volumes), `vms` (`/mnt/<volume>/vms`) und `config` (`/config` des Sticks, mit Konten und Geheimnissen – das Backup ist verschlüsselt). Ordner werden relativ zur Freigabe gespeichert, nie als Gerätename. Die Ordnerliste der Seite kommt aus `GET /api/v1/backups/folders` (nur Ordner, ohne `.recycle` und ohne Links, über `os.Root`). Ein gewählter Ordner, der fehlt, ergibt eine Warnung, nicht einen Fehler. Auf ein lokales Ziel wird dessen eigenes Volume nie gesichert. Laufende VMs werden nur absturzkonsistent gesichert.
- **Ziel „Anderes TitanOS“** (Paket `backuptarget`, Einstellungen › Sicherungen › Datensicherung › „Backups anderer TitanOS annehmen“): Das NAS betreibt einen kleinen Server nach dem REST-Protokoll von restic (Version 2, ohne weiteres Debian-Paket) und lauscht **nur auf seinen Tailscale-Adressen** (100.64.0.0/10, `fd7a:115c:a1e0::/48`), Port 8750; ohne Tailscale oder ohne eingebundenes Volume lauscht es nirgends. Jede Quelle hat einen Namen (`[A-Za-z][A-Za-z0-9-]{0,30}`) und einen zufälligen Token (32 Byte, nur als SHA-256 in `/config/backup/target.json`) und erreicht nur ihren Ordner `/mnt/<volume>/.titanos/backup-target/<name>/`. Die Seite erzeugt einen Kopplungscode (`titanos-backup:` + base64url-JSON mit Adresse, Name und Token), der **einmal** angezeigt wird. Auf dem sendenden NAS wählt man „Anderes TitanOS“ und fügt ihn ein; das ergibt ein ganz gewöhnliches Ziel `rest` (Adresse, Name und Token landen wie dort im Auftrag). Daten sind mit dem Passwort des Auftrags verschlüsselt; das Ziel kann sie nicht lesen. Blobs werden unter einem versteckten Namen geschrieben und erst am Ende umbenannt, Vorhandenes wird nie überschrieben (403), die Konfiguration des Repositorys nie gelöscht. Entfernen einer Quelle löscht ihre Daten nur auf ausdrücklichen Wunsch. Getestet mit einem Go-Client gegen das Protokoll und mit Zugriffsversuchen anderer Quellen; zusätzlich mit echtem restic geprüft: Initialisieren, Sichern, Datenprüfung und identische Wiederherstellung gegen den lokalen Testserver. Zwei echte NAS über Tailscale bleiben zu prüfen.
- **Überfällig** (`overdue` in `backup/schedule.go`): Ist für einen eingeschalteten Auftrag seit mindestens zwei geplanten Terminen und mindestens 48 Stunden kein Backup gelungen, gibt es einmal die Warnung `backup_overdue`; erneut erst, nachdem eines gelungen ist (gemerkt im RAM, nach einem Neustart von titand also höchstens einmal neu). Ein Auftrag, der nie versucht wurde, ist nicht überfällig. Geprüft wird jede Minute aus dem Zustand im Speicher, ohne Zugriff auf `/config`.
- **Prüfen** (0.14.0): `restic check`, optional `--read-data-subset=N%`; Ergebnis als `lastCheck`/`checkError` im Zustand des Auftrags, Fehler als Meldung `backup_check_failed`. Ein fehlgeschlagener Check ist kein fehlgeschlagenes Backup.
- **Einzelne Dateien** (0.14.0): `GET /api/v1/backups/{id}/snapshots/{snapshot}/files?path=` listet einen Ordner (`restic ls` ohne `--recursive`, Ordner zuerst, Seiten zu 500, Grenze 50.000); `POST …/restore` mit `paths` (höchstens 200, absolute Pfade ohne `..`) stellt per `--include` in einen neuen Ordner wieder her, nie über die Freigabe. Beide Routen im API-Katalog des Assistenten.
- Getestet mit nachgebautem restic; echtes restic für `check`/`ls`/`--include` noch zu prüfen.
- Noch offen: Bandbreite begrenzen.

**USV** (Einstellungen › USV; umgesetzt ohne Entwurfsseite, Aufbau selbst gewählt)
- Paket `ups`, Debian-Pakete `nut-server` und `nut-client` (NUT 2.8). Drei Arten: keine USV, USV am Kabel dieses NAS (Treiber `usbhid-ups` für die meisten USB-Geräte, dazu `nutdrv_qx`, `blazer_usb`, `blazer_ser`, `apcsmart`; „Nach USV am USB suchen“ mit `nut-scanner`), oder eine USV, die ein anderes Gerät per NUT im Netz teilt. Einstellungen in `/config/system/ups.json`.
- titand erzeugt `/etc/nut/*` (Modus 0640, Gruppe `nut`) und verwaltet die Dienste; ab Werk laufen sie nicht. Bei einem Dienstneustart bleiben unveränderte Dateien, laufende Dienste und das bestehende upsmon-Passwort erhalten.
- Herunterfahren: Meldet die USV „Batterie fast leer“, fährt upsmon das NAS herunter (`systemctl poweroff`: VMs, Container, Freigaben und Volumes werden sauber beendet). Wahlweise schon nach N Minuten auf Batterie; dann löst titand das aus (`upsmon -c fsd`, damit auch Geräte herunterfahren, die diese USV über das NAS beobachten).
- „Anderen Geräten im Netzwerk mitteilen“: upsd lauscht dann im Netz, mit dem Konto `monitor` und einem festen Passwort, das die Seite für die Einrichtung der anderen Geräte zeigt. Sonst nur auf 127.0.0.1.
- Meldungen: auf Batterie, Strom wieder da, Batterie fast leer, fährt herunter, keine Verbindung zur USV. titand fragt alle zehn Sekunden mit `upsc`.
- Getestet in QEMU mit dem Treiber `dummy-ups` (spielt eine USV aus einer Datei): Zustand und Werte auf der Seite, Anmeldung eines anderen Geräts, Herunterfahren nach einer Minute auf Batterie und durch NUT bei fast leerer Batterie. Nicht getestet: eine echte USV, der Modus „über das Netzwerk“ gegen einen fremden NUT-Server.

**Zugang** (Einstellungen › Zugang; umgesetzt ohne Entwurfsseite, Aufbau selbst gewählt)
- **SSH** (Paket `sshd`, Debian-Pakete `openssh-server` und `sudo`): ab Werk aus, der Dienst ist im Abbild abgeschaltet. Einstellung in `/config/system/ssh.json`; titand startet und stoppt `ssh.service` selbst.
  - Anmelden können sich nur Besitzer und Administratoren, die unter „Mein Konto“ einen Schlüssel hinterlegt haben – unter ihrem eigenen Namen, nie mit Passwort, nie als root (`sshd_config.d/10-titanos.conf`: `AllowGroups titanos-ssh`, `PasswordAuthentication no`, `PermitRootLogin no`).
  - titand schreibt die Schlüssel nach `/etc/ssh/authorized_keys/<konto>` (gehört root: ein Konto kann sich nicht selbst weitere Schlüssel geben), setzt für diese Konten Shell und Heimatverzeichnis (`/home/<konto>`, im RAM) und nimmt sie in die Gruppe `titanos-ssh` auf. Die Gruppe darf `sudo` ohne Passwort – die Konten haben kein Linux-Passwort, nach dem sudo fragen könnte.
  - Verliert ein Konto Rolle oder Schlüssel oder wird SSH ausgeschaltet, endet auch eine offene Sitzung (`loginctl terminate-user`).
  - Der Schlüssel des NAS selbst liegt in `/config/ssh/host_ed25519` und wird beim ersten Einschalten erzeugt. Die beim Bau des Abbilds erzeugten Host-Schlüssel werden gelöscht – sie wären sonst auf jedem TitanOS dieselben.
  - Wer einen Schlüssel hat, kommt ohne zweiten Faktor hinein und hat volle Rechte. Die Seite sagt das.
- **Zertifikat** (`tlscert.Manager`): TitanOS erstellt ab Werk ein selbst signiertes. Ein eigenes Zertifikat samt Kette und Schlüssel (PEM) lässt sich hinterlegen; es wird geprüft (Schlüssel passt, gültig, nennt Hostnamen, ist kein CA-Zertifikat), in `/config/tls/custom-*.pem` gespeichert und sofort verwendet, ohne Neustart. Der Schlüssel wird nie wieder herausgegeben. Ist das eigene Zertifikat abgelaufen, greift beim nächsten Start wieder das selbst signierte; 14 Tage vorher gibt es eine Meldung.
- Im Abbild sind `/etc` und die Dateien aus `mkosi.extra` nicht mehr für die Gruppe schreibbar (`chmod -R go-w` im Postinst). Vorher trugen sie die Rechte des Arbeitsverzeichnisses, aus dem gebaut wurde; sshd verweigert unter einem solchen `/etc` die Arbeit.
- Let's Encrypt per DNS-Challenge ist vorerst zurückgestellt; selbst signiertes Zertifikat und Import eigener Zertifikate reichen für den aktuellen Umfang.
- Noch offen: Fingerabdrücke der Schlüssel im Protokoll einer Anmeldung, SSH nur aus bestimmten Netzen.

**Werkzeuge** (Reiter „Werkzeuge“, nur für Besitzer und Administratoren; umgesetzt ohne Entwurfsseite, Aufbau selbst gewählt)
- Paket `diagnose`.
- **Protokoll:** Zeilen aus dem Journal (`journalctl -o json`), wählbar nach Quelle (alles, titand, Kernel, Platten und RAID, Freigaben, Docker, VMs, Netzwerk), Stufe und Suchtext; „Mitlaufen“ holt alle drei Sekunden nach, was seit dem letzten Stand dazukam (Cursor des Journals). Das Journal liegt im RAM (32 MB) und beginnt mit jedem Start neu.
- titand schreibt seine Zeilen mit der Stufe, wie journald sie liest (`<3>` Fehler, `<4>` Warnung), damit die Auswahl „Nur Fehler“ auch für titand stimmt.
- **Diagnosepaket:** Zip mit `README.txt`, `system.txt`, `journal.txt`, `kernel.txt`, `storage.txt`, `network.txt`, `hardware.txt` (aus `/sys` gelesen; `lspci` und `lsusb` sind nicht im Abbild), `docker.txt`, `vms.txt` und `config/` mit den Einstellungen von Volumes, Freigaben, Netzwerk, Docker, System, Plugins und VMs. Es entsteht beim Klick, wird direkt heruntergeladen und nirgends hingeschickt.
- Ersetzt werden (`redact.go`): IPv4- und IPv6-Adressen, MAC-Adressen, E-Mail-Adressen, der Name des NAS (außer dem Namen ab Werk), Kontonamen, `schlüssel=wert` mit Passwort/Token/Secret/Key im Namen, Anmeldedaten in Adressen, Bearer-Tokens, PEM-Blöcke, SSH-Schlüssel, lange Hex-Ketten. Dieselbe Adresse bekommt im ganzen Paket denselben Platzhalter (`<ip-1>`), damit die Protokolle lesbar bleiben.
- Nie im Paket: `users/`, `apps/` (nur die Liste der App-Namen), `tls/`, `notify/` (dort sind die Adressen selbst das Geheimnis), Dateien der Nutzer. Namen von Freigaben, Containern und VMs bleiben; die Seite weist darauf hin, das Paket vor dem Anhängen durchzusehen.
- **Platten vorbereiten** (`storage/disktest.go`): prüft Platten, die in keinem Volume stecken, nicht eingebunden sind und keiner VM gehören. Zwei Arten: „Nur lesen“ (Daten bleiben) und „Überschreiben und zurücklesen“ (löscht alles, verlangt den Namen der Platte als Bestätigung). Danach lässt TitanOS die Platte ihren langen SMART-Selbsttest machen und vergleicht die Fehlerzähler vorher und nachher.
  - Geschrieben und gelesen wird am Seitencache vorbei (`O_DIRECT`) und exklusiv (`O_EXCL`: der Kernel verweigert das Öffnen, wenn etwas anderes die Platte hält), in Stücken von 4 MiB. Jeder 4-KiB-Block trägt seine eigene Position, damit auch Daten auffallen, die an der falschen Stelle landen. Nach 64 fehlerhaften Stellen bricht die Prüfung ab.
  - Nach dem Überschreiben (auch nach einem Abbruch) werden Anfang und Ende der Platte geleert, damit nichts wie eine Partitionstabelle aussieht.
  - Durchgefallen ist eine Platte bei Schreib-, Lese- oder Vergleichsfehlern, bei fehlgeschlagenem Selbsttest, wenn sie sich selbst als fehlerhaft meldet oder wenn defekte bzw. schwebende Sektoren dazugekommen sind. Ohne SMART (virtuelle Platte, mancher USB-Adapter) wird der Selbsttest übersprungen und das vermerkt.
  - Eine Platte in Prüfung ist für alles andere gesperrt (Volume anlegen, Platte hinzufügen, VM); am Ende gibt es eine Meldung. Prüfungen liegen im Speicher von titand und überstehen keinen Neustart.
  - Getestet in QEMU, auch mit einer Platte, die an einer Stelle Lesefehler liefert (blkdebug). Der SMART-Teil ist nur mit aufgezeichneten Antworten im Unit-Test geprüft: Virtuelle Platten kennen kein SMART.
- Platten, die einer VM als Ganzes gehören, nimmt TitanOS weder in ein Volume noch in eine Prüfung. Umgekehrt bleibt eine solche Platte für die VM nutzbar, nachdem der Gast sie formatiert hat (vorher galt sie dann als „belegt“ und die VM startete nicht mehr).
- Noch offen unter „Werkzeuge“: SMART-Werte je Platte im Diagnosepaket, eine Prüfung über einen Neustart hinweg fortsetzen.

**Sicherung des Sticks** (Einstellungen › Sicherungen › Systemkonfiguration und die Ersteinrichtungsseite)
- Paket `configbackup`. Eine Sicherung ist eine Datei `config-<Zeit>-<Prüfsumme>.tar.gz` in `/mnt/<volume>/.titanos/backups` (Ordner nur für root) mit allem aus `/config`, also auch Passwort-Hashes, 2FA-Geheimnissen und den `.env`-Dateien der Apps. Die erste Datei im Archiv, `titanos-backup.json`, nennt Format, Zeit, Name des NAS und Version.
- Ab Werk eingeschaltet, Ziel ist das erste eingebundene Volume (oder ein gewähltes), 14 Kopien bleiben. Einstellungen in `/config/system/backup.json`.
- Gesichert wird einmal nach jedem Start und fünf Minuten nach der letzten Änderung. Ob sich etwas geändert hat, sagt ein Zähler im `config.Store` (jeder Schreibzugriff zählt hoch) – der Stick wird dafür nicht geweckt. Eine neue Datei entsteht nur, wenn sich der Inhalt von der neuesten Kopie unterscheidet. Ist das Ziel-Volume nicht eingebunden, wird nichts geschrieben (der Ordner läge sonst im RAM).
- Herunterladen einer Kopie („optional extern“) nur für Besitzer und Administratoren und nach der konfigurierten erneuten Passwort-/TOTP-Bestätigung (siehe Terminal und Konsolen). Schlägt eine Sicherung fehl, gibt es eine Meldung (`config_backup_failed`).
- **Stick ersetzen:** neuen Stick mit dem Zip bespielen und starten. Solange es kein Konto gibt, bietet die Ersteinrichtungsseite „Konfiguration wiederherstellen“ an. TitanOS kennt auf einem neuen Stick seine Volumes nicht; es sieht deshalb in alle laufenden md-Arrays mit ext4 oder XFS hinein – nur lesend und ohne das Journal nachzuspielen (`ro,noload` bzw. `ro,norecovery`), unter `/run/titanos-restore`, und hängt sie sofort wieder aus. Alternativ lässt sich eine heruntergeladene Kopie hochladen. Danach startet das NAS neu.
- Das Wiederherstellen braucht keine Anmeldung (es gibt noch kein Konto) und ist nur möglich, solange keines existiert. Es verschafft niemandem Zugang: Es bringt die Konten der Sicherung zurück. Eine Sicherung wird vollständig geprüft, bevor etwas geschrieben wird (nur gewöhnliche Dateien mit Pfaden innerhalb von `/config`, mindestens ein Konto, Obergrenzen für Größe und Anzahl); die Konten werden zuletzt geschrieben, damit ein abgebrochener Vorgang wiederholt werden kann.
- Nicht umgesetzt: Zurückspielen eines älteren Stands auf ein laufendes, eingerichtetes NAS und ein Hinweis bei Lesefehlern des Sticks. Externe Sicherung von `/config` ist über den ausdrücklich gewählten Bereich `config` eines verschlüsselten restic-Auftrags möglich; die automatische `configbackup`-Ablage selbst bleibt auf einem Daten-Volume.

**Benachrichtigungen** (Glocke „Meldungen“ in der Kopfzeile und Einstellungen › Benachrichtigungen; umgesetzt ohne Entwurfsseite, Aufbau selbst gewählt)
- Paket `notify`. Jede Meldung hat eine Art (`volume_degraded`, `disk_unhealthy`, …), einen Betreff (Volume oder Platte) und eine Stufe (Hinweis, Warnung, Fehler). titand behält die letzten 100 Meldungen im Speicher; nach einem Neustart ist die Liste leer. Sichtbar für Besitzer und Administratoren.
- Ein Beobachter sieht alle 30 Sekunden auf den Speicher und meldet Änderungen, nicht Zustände: Volume ohne Redundanz, ausgefallen, nicht gefunden, Wiederaufbau, wieder in Ordnung, Prüfung mit Abweichungen, Volume über 90 % voll (erneut erst nach unter 85 %), Platte mit SMART-Fehler, Platte über 55 °C (erneut erst nach unter 50 °C). Ein Problem wird erst gemeldet, wenn zwei Blicke hintereinander es sehen – beim Start brauchen die Arrays einen Moment.
- Wege nach draußen: E-Mail (SMTP mit STARTTLS oder TLS; ohne Verschlüsselung wird kein Passwort übertragen), ntfy, Telegram, Discord. Je Weg drei Versuche. „Verschicken ab“ legt die Stufe fest (Standard: Warnungen und Fehler); die Glocke zeigt immer alles. Die Sprache der verschickten Texte ist eine Einstellung (`texts.go`), die Glocke übersetzt selbst in die Sprache des Betrachters (`notify.kind.*`).
- Einstellungen in `/config/notify/settings.json`. Passwörter, Tokens und die Webhook-Adresse verlassen das NAS nie über die API (nur „ist gesetzt“); ein leeres Feld behält den gespeicherten Wert, Entfernen ist ausdrücklich (`clearSecrets`). „Testnachricht senden“ speichert erst und zeigt die Antwort der Gegenseite.
- Neue Meldungen aus anderen Paketen: `notify.Manager.Notify(stufe, art, betreff)` aufrufen und die Texte in `texts.go` sowie `notify.kind.<art>.title/.text` in beiden Sprachdateien ergänzen.
- Gelesen/ungelesen merkt sich der Browser, nicht das NAS.
- **Glockenfilter pro Konto** (0.14.0, Preference `bell`): Mindeststufe und Gruppen (Volumes, Platten, Backups, Updates, USV, USB, Assistent, System; Zuordnung nach Präfix der Art in `web/src/lib/notify-groups.ts`). Gefiltert wird nur im Browser, Ton und Zähler folgen dem Filter, ausgeblendete werden gezählt. Fehler an Volumes und Platten lassen sich nicht ausblenden.
- Weg „Home Assistant“ (Dienst `notify.<name>`) gemeinsam und persönlich, über die Verbindung aus Einstellungen › Home Assistant; Fehler mit hoher Priorität.

**Netzwerk** (Einstellungen › Netzwerk; umgesetzt ohne Entwurfsseite, Aufbau selbst gewählt)
- Konfiguration in `/config/network/network.json`: Name des NAS, Anschlüsse (keiner gewählt = jeder Anschluss per DHCP, wie ab Werk; mehrere = Bond mit wählbarer Art), VLAN-Nummer, Bridge `br0`, DHCP oder feste Adresse mit Gateway, Nameserver. Daraus schreibt das Paket `network` bei jedem Start und jeder Änderung die Dateien für systemd-networkd (`/etc/systemd/network/10-titanos-*`); Schichtung: Anschlüsse → Bond → VLAN → Bridge → Adresse.
- Die Bridge übernimmt die Hardware-Adresse des ersten Anschlusses, damit der Router über Neustarts dieselbe Adresse vergibt.
- Entscheidung vom 15. Oktober 2026: Firewall vorerst ausgeschaltet; IPv4/IPv6 INPUT, OUTPUT und FORWARD sowie DOCKER-USER geben Verkehr grundsätzlich frei; benötigte Tailscale-Hooks bleiben vor der allgemeinen Freigabe erhalten. `titanos-firewall-open.service` läuft vor dem Netzwerkstart, der Helfer zusätzlich bei Netzwerkänderungen und nach Docker-Starts. NAT und Portweiterleitungen bleiben erhalten. nftables.service ist maskiert, libvirts ungenutztes Standard-NAT-Netz startet nicht automatisch. Eine einstellbare Firewall in der Oberfläche folgt später. Details und Tests: `docs/NETWORKING.md`, `scripts/test-bridge-forwarding.py`, `scripts/network_smoke.py`.
- Adressen, Gateway, DNS und IPv6-Router-Advertisements liegen nur auf der obersten Schnittstelle; Bridge-Ports sind ohne eigene Adressen. Netzwerkänderungen überstehen den Abbruch der anstoßenden HTTP-Verbindung, jeder Anwendungsversuch ist auf 30 Sekunden begrenzt. Fehler beim Umkonfigurieren werden gemeldet; fehlgeschlagene Rückfälle werden erneut versucht und bleiben bis zum Erfolg nicht bestätigbar.
- Jede Änderung gilt 2 Minuten auf Probe und wird erst mit der Bestätigung auf den Stick geschrieben. Die Bestätigung kommt über die Weboberfläche – also über die neue Verbindung – per Leiste oben auf jeder Seite. Ohne sie stellt titand den vorigen Zustand wieder her. Ändert sich die Adresse, muss man sich unter der neuen anmelden und dort bestätigen.
- Zeitzone unter „Einstellungen › Datum und Uhrzeit“ (`/config/system/settings.json`), wird bei jedem Start gesetzt und neuen Apps mitgegeben.
- VMs wählen „nur ausgehend“ oder „eigene Adresse im Heimnetz“ (Karte an `br0`); Letzteres nur, wenn die Bridge eingerichtet ist.
- Getestet in QEMU: Bridge mit DHCP und fester Adresse, Zurücksetzen nach unbestätigter Änderung, VM an der Bridge, Neustart des NAS. Nicht getestet (nur die erzeugten Dateien): Bonding und VLAN – dafür fehlen in der Testumgebung zweite Anschlüsse und ein VLAN-fähiges Netz.
- Noch offen: IPv6-Einstellungen, mehrere Netze, Container mit eigener Adresse im LAN (macvlan/ipvlan an `br0`), Wake-on-LAN.

**Shares** (Reiter „Shares“, umgesetzt)
- Seit 0.16.0 kompakte sortierbare Tabelle mit Name, Volume, SMB/NFS, Kontenzahl und echtem Verfügbarkeitsstatus; Gast-/Berechtigungswarnungen sichtbar. Suche, Volume-/Protokollfilter und Dichte. Persistente Ansicht pro Konto in `shares.json` über die Preferences-API; Suche/Auswahl/Scrollposition nur im RAM der Weboberfläche, keine zyklischen Stick-Schreibzugriffe. Details auf Klick mit Allgemein/Zugriff/Optionen. Keine erfundenen Größenwerte oder rekursiven Scans.
- Anlegen und Bearbeiten mit `Dialog.svelte` und den Reitern Allgemein/Zugriff/Optionen; Eingaben bleiben beim Wechsel erhalten. Ungespeicherte Änderungen vor Schließen/Navigation erhalten oder ausdrücklich verwerfen; Speichern- und Entfernen-Fehler behalten den Dialog. Felder:
  - Name und Volume. Ablage `/mnt/<volume>/<share>`.
  - Rechte pro Konto: kein Zugriff, nur lesen, lesen und schreiben.
  - Gastzugriff: standardmäßig aus, Einschalten mit Warnung.
  - Papierkorb mit Aufbewahrungsdauer.
  - „In der Netzwerkumgebung sichtbar“.
  - Time-Machine-Ziel mit Größenlimit.
- NFS pro Share optional, mit erlaubten Netzen oder Hosts.
- Löschen entfernt nur die Freigabe, die Daten bleiben. Das Löschen der Daten ist ein eigener Schritt mit ausdrücklicher Bestätigung.
- Verbindungshilfe im eigenen Dialog: fertige Adressen zum Kopieren für Windows, macOS, Linux und NFS. „In Dateien öffnen“ übergibt nur eine Root-ID; der Dateimanager löst sie erneut gegen die autorisierten Roots auf.
- Konfiguration in `/config/shares/shares.json`, daraus werden `smb.conf` und die NFS-Exports beim Start und bei jeder Änderung erzeugt. Freigaben, deren Volume nicht eingebunden ist, werden nicht angeboten (ihr Ordner läge sonst im RAM).
- Umsetzung der Rechte: Der Zugriff wird im Dienst geregelt, nicht über Dateirechte. Samba prüft Konto und Recht (`valid users`, `write list`; Gäste nur lesend). Alle Dateien gehören der Gruppe `users` (Ordner mit setgid, Modus 2770). NFS kennt keine Konten: Wer auf einem erlaubten Rechner sitzt, schreibt als `nobody:users`.
- Linux- und Samba-Konten entstehen bei jedem Start neu aus `/config/users/` (UID und NT-Hash stehen in `account.json`). Der NT-Hash ist schwach, aber SMB kennt nichts anderes; er entsteht beim Anlegen, beim Passwortwechsel oder bei der nächsten Anmeldung.
- NFS: Jede NFS-Freigabe wird nach `/srv/nfs/<name>` eingebunden (bind mount), `/srv/nfs` ist die Wurzel des NFSv4-Baums. Clients verbinden sich mit `nas:/<name>` (NFSv4) bzw. `nas:/srv/nfs/<name>` (NFSv3). Ganze Volumes werden nie exportiert.
- Der Papierkorb liegt in `.recycle/<konto>` der Freigabe; titand räumt stündlich weg, was älter als die Aufbewahrungsdauer ist.
- Noch nicht geprüft: Time Machine mit einem echten Mac und die Sichtbarkeit in der Netzwerkumgebung (wsdd2, avahi).

**Dateimanager** (Reiter „Dateien“ direkt nach „Shares“, umgesetzt; aktuelles Layout und Details weiter unten)
- Aufbau: links die Shares, auf die das Konto Zugriff hat, darunter freier Platz des gewählten Shares; „Volumes“ unter erweitertem Zugriff nur für Besitzer/Administratoren; der Freigabe-Papierkorb ist auch für Benutzer im eigenen Bereich verfügbar. Rechts Pfad als Brotkrumen, Werkzeugleiste (Hochladen, Neuer Ordner, Suchen), Dateiliste (Name, Größe, Geändert), bei Auswahl eine Aktionsleiste (Herunterladen, Verschieben, Umbenennen, Löschen).
- Hochladen per Knopf oder Drag-and-drop, einzelne Dateien und ganze Ordner. Warteschlange mit Fortschritt, Tempo und Restzeit je Datei, Pausieren und Abbrechen; läuft weiter, wenn die Seite gewechselt wird.
- Große Dateien in Stücken hochladen und nach Verbindungsabbruch fortsetzen (ISOs, VM-Abbilder).
- Keine halben Dateien: direkt auf das Ziel-Volume in eine Teildatei schreiben, erst am Ende auf den richtigen Namen umbenennen. Uploads nie im RAM, im Overlay, in `/tmp` oder auf dem Stick puffern.
- Vor dem Start: freien Platz prüfen; bei gleichem Namen fragen: Ersetzen, Beide behalten, Überspringen.
- Herunterladen: einzelne Dateien direkt, mehrere Dateien oder Ordner als Zip-Stream (ohne Zwischendatei).
- Löschen geht in den Papierkorb des Shares, wenn er aktiv ist; sonst endgültig mit Bestätigung. Verschieben und Kopieren großer Mengen als Hintergrundauftrag mit Fortschritt.
- Rechte: Jede Aktion läuft mit den Rechten des angemeldeten Kontos innerhalb der jeweils autorisierten Wurzel (Freigabe, Volume, USB oder eingeschränkter Stick-Zugriff); symbolische Links dürfen nicht aus dem Share hinausführen. Hochgeladene Dateien gehören dem Konto, wie bei einem Upload per SMB.
- Vorhandene Ordner-/Dateiauswahl verwenden und deren Root-/Rechteprüfung erhalten; eine durchgehend vereinheitlichte Auswahl für alle Editoren bleibt ein Ausbauziel.
- Umsetzung: Jede Anfrage nennt eine Wurzel (`share/<name>` oder, für Administratoren, `volume/<name>`), auf die das Konto Zugriff hat. Alle Dateizugriffe laufen über `os.Root` und können die Wurzel nicht verlassen, auch nicht über symbolische Links. Ein Upload wird in Stücken von 8 MiB in eine versteckte Teildatei `.<name>.<id>.titanos-part` im Zielordner geschrieben; der Client fragt nach einer Unterbrechung den Stand ab und setzt dort fort.
- Umgesetzt: Auflisten, Hochladen von Dateien und Ordnern (Knopf und Drag-and-drop) mit Pausieren, Fortsetzen, Abbrechen und Abfrage bei gleichem Namen, Herunterladen (Datei oder Zip), Neuer Ordner, Umbenennen, Löschen (Papierkorb bzw. endgültig mit Bestätigung).
- Umgesetzt: **Kopieren und Verschieben** als Hintergrundauftrag (`files/copy.go`, `POST /api/v1/files/copy`, Liste und Abbrechen unter `/files/jobs`): zwischen beliebigen Wurzeln, die das Konto sieht; Ziel wählt man im Dialog (Wurzel und Ordner), bei gleichem Namen „Beide behalten“, „Überspringen“ oder „Ersetzen“. Dateien werden unter einem versteckten Namen geschrieben und am Ende umbenannt, der Zeitstempel bleibt; Links und interne Dateien werden nicht kopiert; ein Original wird erst nach der vollständigen Kopie entfernt; innerhalb einer Wurzel ist Verschieben eine Umbenennung. Platz wird vorher geprüft, ein Ordner kann nicht in sich selbst. Aufträge liegen im Speicher von titand (die letzten 20 je Konto sichtbar).
- Umgesetzt: **USB-Laufwerke** (Paket `usb`, Wurzel „USB“, nur Besitzer/Administratoren): `lsblk` zeigt jedes Dateisystem eines Laufwerks am USB-Anschluss (Boot-Stick `TITANOS`, RAID-/LUKS-/Swap-Mitglieder und Dateisysteme von Volumes bleiben draußen), ein Hinweis in der Glocke (`usb_plugged`) meldet neue Geräte. Eingebunden wird **nur per Klick** (`POST /api/v1/usb/<UUID>/mount`) nach `/run/titanos-usb/<Name>-<UUID-Anfang>` mit `nosuid,nodev,noexec`; unterstützt sind FAT32, exFAT, NTFS (Kernel-Treiber ntfs3), ext2/3/4, XFS und Btrfs. „Auswerfen“ schreibt aus (`sync`) und hängt aus; ein belegtes Laufwerk bleibt eingebunden und sagt es. Nichts wird gespeichert, ein Laufwerk heißt immer nach seiner UUID. Im Entwicklungsmodus wird nichts eingebunden. Nicht getestet: echte USB-Geräte; ein Test mit Gerätetabelle und Befehlen liegt vor.
- Umgesetzt: **Stick im Dateimanager** (Wurzel „Stick“, nur Besitzer/Administratoren, ohne Passwort-Bestätigung): zeigt `/config` nur lesend; ausgeblendet sind `users`, `tls`, `ssh`, `notify`, `backup` und jede Datei `.env` (nicht auflistbar, nicht herunterladbar, nicht im Zip, nicht kopierbar). Einzelne Textdateien (höchstens 512 KiB, UTF-8, `.json` muss gültig sein) lassen sich bearbeiten, nur wenn sie schon existieren; die alte Fassung bleibt als `<name>.prev`, geschrieben wird über eine Teildatei und Umbenennen. Titand liest die Einstellungen beim Start und kann sie selbst wieder überschreiben – der Dialog sagt das. Der Stick wird beim Anzeigen der Wurzeln nicht geweckt (`/config` wird erst beim Öffnen aufgelöst).
- Umgesetzt (0.5.3): **Papierkorb** einer Freigabe mit Papierkorb (Knopf „Papierkorb“ in der Werkzeugleiste, `files/trash.go`, `GET /api/v1/files/trash`, `POST /api/v1/files/trash/restore|delete`). Pfade beginnen mit dem Konto (`alex/Fotos/a.jpg` = `.recycle/alex/Fotos/a.jpg`); Benutzer sehen nur ihren Teil, Administratoren alle Konten. Wiederherstellen legt an den alten Ort zurück, fehlende Ordner werden angelegt, bei gleichem Namen kommt die Uhrzeit in den Namen; danach leere Ordner im Papierkorb verschwinden. Endgültig löschen und „Papierkorb leeren“ mit Bestätigung, nur mit Schreibrecht.
- Lokale Suche und begrenzte Unterordnersuche sind umgesetzt (siehe unten). Teildateien eines Uploads, der nie fortgesetzt wird, räumt titand nach 24 Stunden weg, auch über einen Neustart hinweg: Jeder Upload auf einem Volume hinterlegt eine Notiz in `/mnt/<volume>/.titanos/uploads/<id>` mit dem Pfad seiner Teildatei; stündlich liest titand nur diese Notizen (nie ganze Freigaben) und entfernt Teildateien, die titand nicht kennt und die seit 24 Stunden unverändert sind. Uploads aus Versionen vor 0.5.6 haben keine Notiz und bleiben liegen, bis jemand ihren Ordner öffnet.
- Vorschau für Bilder, Text, PDF und Audio/Video ist umgesetzt (siehe unten). Freigabe-Links mit Ablaufdatum bleiben offen.

**Docker: Stand der Umsetzung**
- Umgesetzt: Docker-Engine (Debian-Pakete `docker.io`, `docker-cli`, `docker-compose`) mit allen Daten auf einem gewählten Volume (`/mnt/<volume>/.titanos/docker`); die Engine läuft nur, solange dieses Volume eingebunden ist. Einstellung in `/config/docker/settings.json`.
- Umgesetzt: Übersicht mit Karten, Compose-Stacks, Suche, Filter, Start/Stopp/Neustart, Logs, Entfernen sowie „Compose importieren“. Entfernen behält ab Werk den Datenordner; wahlweise (Häkchen „Auch die Daten der App löschen“, dann nur noch die Rückfrage, kein Eintippen des Namens) löscht es auch `/mnt/<volume>/appdata/<name>`, die Docker-Volumes des Projekts und nicht anderweitig genutzte Images, damit eine Neuinstallation frisch beginnt (`DELETE /api/v1/docker/apps/<name>?deleteData=<name>`). Apps ohne Container (Einrichtung gescheitert) und fehlgeschlagene Einrichtungen zeigt die Docker-Seite mit Starten/Erneut versuchen, Bearbeiten und Entfernen. Eine importierte App liegt in `/config/apps/<name>/compose.yaml`; relative Pfade der Compose-Datei zeigen auf `/mnt/<volume>/appdata/<name>` (`--project-directory`), nie auf den Stick.
- titand spricht die Docker-API direkt über den Unix-Socket (nur Standardbibliothek) und ruft für Apps `docker compose` auf.
- Umgesetzt: Update-Hinweise (Digest des Tags in der Registry gegen das lokale Image, alle 12 Stunden und per Knopf), „Alle aktualisieren“, WebUI-Link über das Label `titanos.webui` (Port, Port/Pfad oder URL), Ports als Links auch in Stack-Zeilen.
- Umgesetzt: Container-Konsole (xterm.js, WebSocket ↔ `docker exec`), nur für Besitzer/Administratoren mit bestehender Anmeldung, ohne zweite Passwortabfrage, Ende nach 15 Minuten ohne Eingabe, im Protokoll vermerkt.
- Umgesetzt: Container-Editor unter `/docker/apps/<name>` (Formular ↔ Compose auf demselben YAML-Text, Kommentare bleiben erhalten): Image, Autostart/Neustart-Richtlinie, Netzwerk, WebUI, Privilegiert, Pfade, Ports, Variablen, Labels mit Live-Prüfungen.
- Umgesetzt: App-Store unter `/apps` (Backend-Paket `catalog`): der offizielle Katalog ist das öffentliche Repository `ra5on/TitanOS-Apps` (je App ein Ordner mit `compose.yaml` im `x-titanos`-Format), fest als nicht entfernbare Quelle „TitanOS-Apps“ (`builtin: true`, ID `titanos`) eingetragen, plus eigene Quellen in `/config/docker/sources.json`. Im Abbild steckt kein Katalog mehr: Ohne Netz beim ersten Start ist der Store leer und zeigt einen Hinweis (kein Fehler); installierte Apps (`/config/apps/<name>/compose.yaml`) laufen unverändert weiter. Die Installationstests der Apps (Immich, Ollama) laufen im Apps-Repo, nicht in der Release-CI; Vorlage dafür in `docs/apps-repo/`. Das Abbild enthält keine AGPL-Vorlagen mehr. Eine Quelle ist ein Repository (Archiv-Download `/archive/HEAD.tar.gz` bzw. GitLab `/-/archive/…`, kein Git im Abbild), ein Archiv oder eine einzelne Compose-Datei; Vorlagen liegen nur im RAM und werden nach dem Start und täglich neu geladen. Installationspanel mit Herkunft pro Feld; TitanOS ergänzt Zeitzone, PUID/PGID (Konto des Installierenden, Gruppe `users`), Appdata-Pfade, Freigaben für `suggest: media|share` und freie Ports. Geheimnisse (`secret: generate`) stehen in `/config/apps/<name>/.env`, die Compose-Datei verweist per `${NAME}` darauf.
- `x-titanos`-Schema: `name`, `category`, `description` (Text oder `{de, en}`); `variables.<NAME>`: `label`, `help`, `placeholder`, `required`, `secret: generate`, `fill: timezone|uid|gid`; `paths.<Containerpfad>`: `label`, `suggest`; `ports.<Containerport[/udp]>`: `label`, `webui`.
- Umgesetzt: CasaOS-Stores als Quelle (geprüft an `IceWhaleTech/CasaOS-AppStore` und `bigbeartechworld/big-bear-casaos`, zusammen 668 Vorlagen, alle für amd64). Format: `Apps/<Name>/docker-compose.yml` mit `x-casaos` oben (Titel, Tagline, Kategorie, `main`, `port_map`, `scheme`, `index`, `tips.before_install`, `architectures`) und je Dienst (Beschreibungen zu Variablen, Pfaden, Ports). Das Backend liest daraus nur die Angaben für die Liste; übersetzt wird beim Installieren im Browser (`web/src/lib/compose/casaos.ts`):
  - `/DATA/AppData/$AppID/…` → Datenordner der App (`./…`), andere Ordner unter `/DATA` → Vorschlag einer Freigabe (`suggest: media|share`), sonst ebenfalls im Datenordner. Nichts landet unter `/DATA` im RAM.
  - `$AppID`, `$PUID`, `$PGID`, `$TZ` und Platzhalter wie `[CASAOS_IP]` werden ersetzt; offene Platzhalter (`[EUFY_USERNAME]`) werden zu Pflichtfeldern.
  - `port_map`/`scheme`/`index` → Label `titanos.webui`; Hinweise des Autors werden im Panel angezeigt.
  - App-Symbole aus `x-titanos.icon` bzw. CasaOS werden ohne Referrer geladen, mit Monogramm als Rückfall. Im App-Katalog und Installationspanel bleibt der Hintergrund der Symbole transparent; keine eingefärbten Kacheln hinter den Bildern, Rückfallkürzel nutzen die Theme-Textfarbe. Neue Installationen speichern `titanos.icon`. Siehe `docs/APP-LINKS.md`.
- Die beiden Stores stehen in der Quellenliste als Vorschlag mit „Hinzufügen“. Sie werden nie von selbst eingetragen: Ihre Apps sind von TitanOS nicht geprüft, und der offizielle Store lädt beim ersten Abruf rund 240 MB.
- Für alle Vorlagen gilt: Das Panel nennt erweiterte Rechte (privilegiert, Host-Netzwerk, Docker-Socket, Wurzelverzeichnis, Capabilities) und warnt bei Pfaden außerhalb der Volumes. Geräte, die es auf dem NAS nicht gibt (z. B. `/dev/dri` ohne GPU, Raspberry-Pi-Geräte), werden weggelassen – mit ihnen startet der Container nicht.
- Große Quellen: tar.gz-Archive werden beim Laden durchgereicht und nur die YAML-Dateien behalten (der offizielle CasaOS-Store ist wegen der Screenshots rund 240 MB groß). Beim täglichen Abgleich schickt titand das ETag mit; ein unverändertes Archiv wird nicht erneut geladen.
- Das Formular schreibt nur zurück, was geändert wurde (`web/src/lib/compose/service.ts`): unberührte Einträge behalten Schreibweise, Kommentare und YAML-Anker.
- Katalog, Icons, Herkunft/Geheim-Schalter, Geräteauswahl und globales Terminal-Panel sind umgesetzt. Image-Updates werden über Registry-Digests erkannt; keine automatische Übernahme geänderter Compose-Vorlagen versprechen. CasaOS-Vorlagen enthalten oft feste Standardpasswörter und feste Containernamen (zweite Installation derselben App scheitert daran).

**Docker: Container bearbeiten** (wichtigste Seite neben dem Speicher)
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

**Docker-Übersicht und App-Store**
- Aktuelles gemeinsames Raster für Container und Compose-Stacks mit Karten/Liste/Minimal und persönlichen Layouts: siehe „Docker-Layout“ unten. Die Docker-Oberfläche bleibt auf dem Stand 0.5.9, solange keine Änderung daran beauftragt ist.
- Suche/Filter, Status, CPU/RAM, Ports, WebUI, Start/Stopp und Schnellaktionen bleiben erhalten. Eigene Quellen werden täglich/per Knopf aktualisiert, externe Stores nur ausdrücklich hinzugefügt.
- Installationspanel mit Herkunft je Feld, eigenen Variablen/Pfaden/Ports und erweiterten Rechtehinweisen. Daten auf dem ausgewählten Docker-Volume, niemals im RAM-Overlay; GPU-Nutzung ausdrücklich je Dienst.
- Webadressen über `titanos.webui`, optional `titanos.public-url` und aktiven Tailscale-Host; dies richtet keinen Proxy oder Tunnel ein. Siehe `docs/APP-LINKS.md` und `docs/NATIVE-APPS.md`.

**Terminal und Konsolen**
- Terminal-Symbol ohne Text in der Kopfzeile auf jeder Seite (zusätzlich Strg+`). Öffnet ein Panel am unteren Rand, Höhe ziehbar, Seite darüber bleibt bedienbar; auch als eigener Browser-Tab.
- Mehrere Sitzungen als Tabs, gekennzeichnet nach Art:
  - **NAS** – root-Shell auf dem Host (PTY im Backend). Hinweis beim Start: Änderungen im RAM-Rootfs gehen beim NAS-Neustart verloren; Systemkonfiguration gehört nach `/config`, Nutzdaten auf gemountete Daten-Volumes.
  - **CT** – Container-Konsole per `docker exec`, Shell automatisch/bash/sh wählbar; eine Benutzerwahl ist noch nicht umgesetzt. Auch aus dem Schnellmenü der Docker-Übersicht („Konsole“).
- „+“ öffnet: neue NAS-Shell, Konsole eines laufenden Containers, VM-Konsole.
- Werkzeuge: Suchen im Verlauf, Kopieren, Einfügen, Schriftgröße. Farben folgen dem Theme. Sitzungen laufen weiter, wenn das Panel minimiert oder die Seite gewechselt wird.
- Technik: xterm.js im Browser, WebSocket zu titand, PTY über `golang.org/x/sys` bzw. Docker-Exec-API.
- Sicherheit: nur angemeldete Besitzer/Administratoren. NAS-Shell und Docker-Konsolen nutzen auf ausdrücklichen Nutzerwunsch die bestehende Anmeldung ohne zweite Passwortabfrage. VM-Konsolen verwenden die konfigurierbare erneute Passwort-/TOTP-Bestätigung. Abbruch nach 15 Min Inaktivität; jede Sitzung im Protokoll (wer, wann, welches Ziel); in den Einstellungen komplett abschaltbar.
- Umgesetzt: Panel am unteren Rand (Knopf in der Kopfzeile, Strg+`), Höhe ziehbar, Tabs für NAS-Shell und Container-Konsolen, „+“-Menü, Shell-Auswahl (automatisch/bash/sh) für Container, Suchen, Kopieren, Einfügen, Schriftgröße, eigener Browser-Tab (`/terminal`). Sitzungen laufen weiter, wenn das Panel minimiert oder die Seite gewechselt wird – nicht aber über ein Neuladen der Seite hinweg. Abschalten unter „Einstellungen“ (`/config/system/terminal.json`) beendet offene Sitzungen sofort.
- Die NAS-Shell ist `bash --login` als root hinter einem Pseudo-Terminal (`backend/internal/terminal`, nur `golang.org/x/sys`, kein creack/pty). Im Entwicklungsmodus gibt es sie nicht – es wäre eine Shell auf dem Rechner des Entwicklers.
- Umgesetzt: Die erneute Passwort-Eingabe vor VM-Konsolen und vor dem Herunterladen von Sticksicherungen ist unter „Einstellungen“ abschaltbar (`/config/system/terminal.json`, `noConfirm`; ab Werk bleibt sie an). Ausgeschaltet genügt der Anmelde-Cookie; Beschränkung auf Administratoren, Abbruch nach 15 Minuten ohne Eingabe und das Protokoll bleiben. Die Seite nennt das Risiko (ein gestohlener Cookie genügt dann auch für diese sensiblen Zugriffe). Das Passwort für das Backup-Passwort eines Auftrags bleibt unberührt.
- Erneute Bestätigung berücksichtigt bei aktivierter 2FA auch TOTP. Benutzerwahl für Container-Konsolen bleibt offen.

**VMs: Stand der Umsetzung**
- Umgesetzt: QEMU/KVM hinter libvirt (Debian-Pakete `qemu-system-x86`, `qemu-utils`, `ovmf`, `libvirt-daemon-system`, `libvirt-clients`; das Abbild wächst dadurch um rund 40 MB). VMs anlegen (Vorlagen Linux, Windows, Andere; vCPUs, RAM, neue Platten qcow2/raw auf wählbarem Volume, Installationsmedium), starten, herunterfahren, neu starten, Ausschalten erzwingen, bearbeiten, entfernen (Platten nur auf Wunsch und mit Eingabe des Namens), Autostart, Bildschirm per noVNC mit drei Anzeigemodi, Vollbild, Strg+Alt+Entf, Text einfügen, Screenshot.
- Was eine VM ausmacht, steht in `/config/vms/<name>.json`. libvirt kennt eine VM nur, solange sie läuft (`virsh create`, flüchtige Domain): seine eigene Konfiguration läge im RAM. Platten und die Einstellungen der Firmware (`nvram.fd`) liegen in `/mnt/<volume>/vms/<name>/`.
- titand ruft `virsh` und `qemu-img` auf, statt per go-libvirt mit libvirt zu sprechen – wie bei mdadm und `docker compose`, mit einem ersetzbaren Runner für Tests. libvirt startet erst mit der ersten VM (Socket-Aktivierung) und beendet sich zwei Minuten nach der letzten. Welche VMs laufen und was sie verbrauchen, liest titand aus `/run/libvirt/qemu/<name>.pid` und `/proc`, ohne libvirt zu wecken.
- QEMU läuft als `libvirt-qemu`, nicht als root. Es erreicht Platten und ISO-Dateien in Freigaben über die Gruppe `users`; `dynamic_ownership = 0`, damit libvirt die Besitzer dieser Dateien nicht ändert.
- Der Bildschirm einer VM ist nur über einen Unix-Socket in `/run/titanos-vms/` erreichbar (Gruppe `kvm`), den titand als WebSocket hinter der Anmeldung weiterreicht. Kein VNC-Port im Netz.
- Beim Herunterfahren des NAS fährt `libvirt-guests` die VMs herunter (bis zu 2 Minuten).
- Netzwerk einer VM: nur ausgehend (QEMU-Usermode-Netz) oder eigene Adresse im Heimnetz über die Bridge `br0`.
- Umgesetzt: Kerne festlegen (`cpuPin`, ein vCPU je gewählter CPU, Threads eines Kerns gemeinsam, Emulator-Threads auf die übrigen Kerne) mit P-/E-Kern-Kennzeichnung aus `/sys/devices/cpu_core|cpu_atom/cpus`; „exklusiv reservieren“ setzt `AllowedCPUs` per `systemctl set-property --runtime` für `system.slice`, `user.slice` und `init.scope` und nimmt es zurück, sobald keine solche VM mehr läuft.
- Umgesetzt: Datenträger auf vier Wegen – neue Platte, Kopie eines vorhandenen Abbilds (qcow2, img/raw, vmdk, vhdx, vdi; `qemu-img convert` im Hintergrund, danach ggf. vergrößern), Abbild direkt verwenden (nur qcow2/raw), ganze Festplatte per `/dev/disk/by-id` (nur Platten, die in keinem Volume stecken; bei jedem Start erneut geprüft). Platten lassen sich bei ausgeschalteter VM vergrößern, nie verkleinern. Das Format eines Abbilds wird aus der Dateiendung bestimmt und QEMU ausdrücklich mitgegeben; Abbilder, die auf einer anderen Datei aufbauen, werden abgelehnt.
- Umgesetzt: TPM 2.0 (swtpm) und Secure Boot (OVMF mit Microsoft-Schlüsseln) als Option der Windows-Vorlage, fest ab dem Anlegen. Der Zustand des TPM liegt in `/mnt/<volume>/vms/<name>/tpm` (`<source type='dir'>`, `persistent_state='yes'`); swtpm läuft als `libvirt-qemu`.
- Umgesetzt: NAS-Ordner per virtiofs (`memfd`-Speicher, `virtiofsd`), serielle Konsole (Unix-Socket, als Tab im Terminal-Panel), Konsole in eigenem Fenster (`/vms/screen?vm=`).
- Umgesetzt: Durchreichen von PCI-Geräten (Grafikkarten und ausdrücklich ausgewählte Zusatzfunktionen) und USB-Geräten, dazu „Einstellungen › Hardware“ (Paket `hardware`): IOMMU-Gruppe, Treiber, Zuordnung. libvirt nimmt ein PCI-Gerät beim Start der VM vom Treiber des NAS und gibt es beim Stopp zurück (`managed='yes'`); es braucht dafür keinen Neustart und keine „Modi“ – ein Gerät gehört der VM, der es zugeordnet ist, solange sie läuft. NPUs werden nicht angeboten, der Stick, von dem TitanOS läuft, auch nicht.
- Weil `dynamic_ownership = 0` gilt, öffnet titand selbst, was QEMU braucht, und nur für die Laufzeit der VM: Gerätedateien ganzer Platten und zugeordneter USB-Geräte bekommen die Gruppe `kvm`; für `/dev/vfio/*` sorgt eine udev-Regel.
- PCI-Funktionen werden nicht automatisch ergänzt. Benötigte Funktionen derselben IOMMU-Gruppe müssen ausdrücklich ausgewählt werden. Auswahl wird auch unmittelbar vor jedem VM-Start geprüft.
- IOMMU ist im Debian-Kernel ohne Kernel-Parameter aktiv, sobald das BIOS es anbietet.
- Noch offen: Windows 11 selbst (nur der Start der VM mit TPM und Secure Boot ist geprüft, nicht die Installation), virtio-win-ISO automatisch, Abbilder in .xz/.gz/.zip, Hochladen/„von URL laden“ im Formular, Live-Auslastung und Firmware-Status auf der Hardware-Seite; reale GPU-/TPU-Tests. Geräteauswahl und optionaler NVIDIA-Wechsel sind umgesetzt.
- Getestet in QEMU mit verschachtelter Virtualisierung und emuliertem IOMMU: Als Gast lief eine Kopie des TitanOS-Sticks; im Gast geprüft wurden Kernzahl, TPM, durchgereichtes PCI- und USB-Gerät (beide von QEMU emuliert), ganze Platte und virtiofs. Echte Grafikkarten, ein Gast mit Desktop, Windows und eine deutsche Tastatur im Gast sind nicht geprüft.

**VMs: Erstellen, Firmware und Konsole**
- Vorlagen Linux, Windows und Andere, q35, CPU host-passthrough; UEFI/OVMF als Standard oder Legacy-BIOS/SeaBIOS. Firmware nach dem Anlegen fest; fehlendes Feld bedeutet UEFI. Secure Boot/Windows-11-Option nur mit UEFI. TitanOS selbst bleibt UEFI-only.
- CPU-Pinning beschränkt zunächst nur die VM; exklusive Reservierung ist ausdrücklich wählbar, zur Laufzeit über cgroups und ohne `isolcpus`/NAS-Neustart. SMT-Geschwister und vorhandene P-/E-Kerninformationen berücksichtigen; keine Hardwareeigenschaften erfinden.
- Neue virtuelle Platte, Kopie eines vorhandenen Abbilds, qcow2/raw direkt oder freie physische Platte per stabiler ID. Physische Geräte vor jedem Start erneut prüfen; Platten nur vergrößern. Auf Btrfs erhalten VM-Verzeichnisse NOCOW (`chattr +C`) für neue Dateien; bestehende Dateien nicht nachträglich umschreiben. Komprimierte Abbilder, automatisches virtio-win-Medium und URL-Import im Formular nicht als umgesetzt darstellen.
- Geräte einzeln auswählen; NPU und Bootstick ausschließen, IOMMU-Gruppen unmittelbar vor dem Start prüfen. Keine verpflichtenden Hardware-Modi oder Neustart-Banner für libvirt-Passthrough. Optionaler NVIDIA-Wechsel siehe Plugins.
- NAS-Ordner per virtiofs; erforderliche Gasttreiber muss der Gast bereitstellen. Kein automatisch von TitanOS zusammengestelltes WinFsp-/VirtIO-Treibermedium versprechen.
- VM-Seite: Liste mit Filter und Aktionen, Reiter Übersicht/Bildschirm/Snapshots (`?tab=`); ohne Angabe laufende VM → Bildschirm, ausgeschaltete → Übersicht. Übersichtskacheln und Bildschirmleiste nach `docs/design/VmPage.html`; `scripts/test-ui-vms.mjs` zur Browserprüfung.
- Jede VM hat ein CD-Laufwerk und einen Gastagent-Kanal. CD live wechseln/auswerfen mit `PUT /vms/<name>/medium`; Adressen über Gastagent (20 Sekunden Cache), sonst `/proc/net/arp` für `br0`.
- VNC über noVNC und authentifizierten WebSocket zum Unix-Socket, kein LAN-VNC-Port. Tastatur, Text senden, Screenshot, eigener Tab und Vollbild; serielle Konsole im Terminal-Panel. GPU-Passthrough zeigt ohne zusätzliche virtuelle Anzeige kein Gastbild in VNC.
- Konsole seit 0.9.1 in **Breite und Höhe** an der Ecke veränderbar, pro Konto gespeichert, auf den sichtbaren Platz unter der Kopfzeile begrenzt. Drei Modi: Skalieren, Auflösung anpassen (nur unterstützte virtuelle Grafik/Gäste), 100 % mit Scrollen. Remote-Resize nicht für Standard-VGA versprechen. Dunkle Bildschirmfarben über Theme-Variablen; Auflösungs-/Größenanzeige und Werkzeugleisten der aktuellen Komponente beibehalten.
- Aktuelle Größenlogik ist mit Tests und ausgeschalteter VM im Browser geprüft; daraus keine Abnahme von Remote-Resize, echtem VNC-Bild oder deutscher Gasttastatur ableiten.

**Hardware und Durchreichung** (Einstellungen › Hardware)
- Nur erkannte Geräte und tatsächlich verfügbare Messwerte anzeigen: GPUs, NPU, PCI-/USB-Geräte, IOMMU-Gruppe, Treiber und Zuordnung. Fehlende Messwerte bleiben unbekannt.
- Container nutzen ausdrücklich gewählte Render-/Accel-Knoten oder NVIDIA-CDI-Geräte; Zuordnung über stabile IDs, Auflösung erst beim Start. VM-Passthrough über libvirt `managed=yes`, Rückgabe beim Stopp.
- Optionaler automatischer Wechsel ausschließlich für NVIDIA: betroffene laufende Container stoppen, konkurrierende Starts sperren, nach Treiberrückgabe/CDI-Erneuerung zuvor laufende Container wieder starten. Kein solcher Wechsel für iGPU/NPU/Coral.
- Keine pauschale Neustartpflicht für Gerätezuordnungen. **Lokal vorbereitete NVIDIA-Treiber** werden dagegen ausdrücklich beim nächsten NAS-Neustart aktiviert (siehe eigenen Abschnitt).

**Plugins** (eigener Reiter)
- Systemerweiterungen für Treiber und Kernelfunktionen – getrennt von Apps (Docker).
- Technik: signierte `systemd-sysext`-Images (squashfs, legt sich über `/usr`), gespeichert auf einem Volume unter `/mnt/<volume>/.titanos/plugins`, geladen nach dem Start der Volumes. Plugin-Einstellungen in `/config/plugins/<name>/`.
- Offizielle Kernelmodule werden für den exakten Kernel der Systembasis gebaut. App-Releases übernehmen deren Plugins; volle Systemupdates bereiten passende Plugins A/B-konsistent vor. Zusätzlich sind lokal vorbereitete NVIDIA-Treiber möglich (siehe unten).
- Vor der Installation auf volle Systemrechte hinweisen; keine durchsetzbare Plugin-Sandbox oder „kein Netzwerk/keine Shares“-Beschränkung behaupten. Eine detaillierte Fortschrittsanzeige in Einzelschritten bleibt offen.
- Kennzeichnung „Offiziell“ vs. „Community (nicht geprüft)“; Hinweis, ob passende Hardware erkannt wurde.
- Verfügbar: NVIDIA, Intel GPU Tools, Coral-USB-Helfer und Tailscale. ZFS und Lüftersteuerung bleiben offen. NVIDIA-Leistungslimit wird innerhalb der gemeldeten Kartengrenzen angeboten; keine automatische Energiesparwirkung eines Persistenz-Dienstes versprechen.

- Umgesetzt (Paket `plugins`, Seite „Plugins“): Ein Plugin ist ein sysext-Abbild `<name>.raw` mit Manifest `<name>.plugin.json` (Titel, Beschreibung, Version, SHA-256 und Größe des Abbilds, optional `kernel`, `modules`, `services`). TitanOS findet Plugins, die in einer Freigabe liegen, kopiert sie beim Installieren nach `/mnt/<volume>/.titanos/plugins` (Prüfsumme vor und nach dem Kopieren) und legt die eingeschalteten per Link in `/run/extensions` und `systemd-sysext refresh` über das System, sobald das Volume eingebunden ist. Welche eingeschaltet sind, steht in `/config/plugins/settings.json`.
- „Offiziell“ ist ein Plugin, wenn sein Manifest eine minisign-Signatur (`.minisig`) eines Schlüssels aus `/usr/share/titanos/plugin-keys/*.pub` trägt; das Manifest nennt die Prüfsumme des Abbilds, die Signatur deckt also beides. `plugin-keys` verweist auf die eingebauten Release-/Reserve-Schlüssel unter `update-keys`; offizielle NVIDIA-Manifeste werden im geschützten Publish-Job signiert. Unsignierte Plugins bleiben „Community (nicht geprüft)“ und brauchen die Eingabe ihres Namens. Die Prüfung ist gegen das echte `minisign` getestet (beide Signaturformen).
- Ein Plugin wird Teil des Systems und hat volle Rechte; die Seite sagt das statt einer „Darf / Darf nicht“-Liste, die TitanOS nicht durchsetzen könnte. Plugins für einen anderen Kernel (`kernel` im Manifest) werden nicht geladen.
- Direkter GitHub-Katalog: Beim Öffnen der Plugin-Seite signiertes Release-Inventar und signierte Plugin-Manifeste passend zur laufenden Systembasis abrufen, zehn Minuten nur im RAM cachen; manueller Refresh möglich. Status-Polling bleibt offline. Erst bei Installation Abbild direkt auf dem Daten-Volume unter `.titanos/plugins/.download-*` zwischenspeichern, Signaturen, Version, Kernel, Größe und SHA-256 prüfen und den bestehenden Installationsweg verwenden; temporäre Downloads bei Erfolg/Fehler entfernen. Keine „latest“-Ausweichversion und keine URLs vom Client. Lokale Dateien bleiben als manueller Weg erhalten; Entwicklungs-Builds und Abruffehler werden erklärt. HTTP-/Signatur-/Abbruchtests in `catalog_test.go`, Anleitung in `docs/PLUGINS.md`.
- Mit einem aktiven Plugin ist `/usr` schreibgeschützt (sysext legt ein Overlay darüber). Das Abbild meldet `SYSEXT_LEVEL=1`; Plugins tragen `ID=titanos` und denselben Level.
- `scripts/make-plugin.sh <name> <version> <ordner>` packt einen Ordner (`usr/…`) zu Abbild und Manifest.
- NVIDIA: `scripts/make-nvidia-plugin.sh` baut offene Module gegen exakt den Kernel der Basis, mit Versionen und SHA-256-fixierten Debian-Paketen aus `plugins/nvidia/packages.lock.json` (aktuell Treiber 615.71.09, Toolkit 1.20.1). CUDA/NVENC/NVDEC, NVML und Docker über native CDI-Geräte. Kernelquellen und originale Lizenztexte liegen jedem Release bei. Anleitung: `docs/NVIDIA.md`. Keine Grafik-/Desktop-Laufzeit, kein proprietärer Legacy-Treiber. Nouveau ist für den später geladenen NVIDIA-Treiber gesperrt.
- Installation ohne sofortige Aktivierung ist wählbar: geprüfte Dateien auf dem Daten-Volume speichern, noch keine Dienste/Module laden und keine laufenden Verbraucher verändern. Später über den Plugin-Schalter aktivieren. Laufende VMs ohne zugewiesene Plugin-Hardware sperren die Installation nicht. Eine Systemupdate-Sperre hat eine eigene Fehlermeldung und wird nicht mehr als belegte GPU bezeichnet.
- Plugin-Aktivierung prüft Modul- und Dienstfehler. `depmod` schreibt seinen Index unter `/run/titanos-plugin-modules`, weil `/usr` durch sysext schreibgeschützt ist. Abschalten stoppt Dienste und entlädt Module vor dem Aushängen; belegte Module verhindern das. Die Aktivierung auf einer RTX 5060 Ti wurde am 15. Oktober 2026 nach Korrektur der CDI-Dateiausgabe vom Projektinhaber bestätigt; CUDA/NVENC und VM-Wechsel auf dieser Karte bleiben ungetestet.
- Dashboard: grafische Kacheln iGPU/GPU (Intel/AMD), NPU und NVIDIA. Messwerte alle fünf Sekunden, nur im RAM, Verlauf bis drei Minuten; sysfs/DRM-fdinfo (inklusive Xe-Zyklen) und `nvidia-smi`, unbekannte Werte bleiben `null`. Keine Beispieldaten im Produkt.
- RAM = MemTotal − MemFree einschließlich Cache; MemAvailable separat. i915-Auslastung über geräteweite perf-Busy-Zähler, fdinfo als Rückfall; Intel-GT-Isttakt und NPU-Busy-Zähler auch während NPU-Suspend. Einstellungen › Temperaturen wählt hwmon-Sensoren pro Konto für eine Dashboard-Kachel, mit stabilen Geräte-/Kanal-IDs. Keine Disk-hwmon-Abfragen, keine erzwungenen Sensortreiber. Details und Hardware-Testgrenzen: `docs/MONITORING.md`.
- Das erste neu angelegte Volume erhält `vms`, `appdata`, `iso`, erst nach erfolgreichem Mount, mit fortsetzbarer einmaliger Einrichtung. Bestehende Volumes und Freigaben bleiben unverändert. VM-Anzeigenamen (`displayName`, bis 80 Unicode-Zeichen ohne Steuerzeichen) sind von unveränderlichen Kennungen (`name`) getrennt und auch laufend änderbar; Pfade, MAC/UUID und Dashboard-Zuordnungen bleiben erhalten.
- Geräteauswahl je Compose-Dienst (`x-titanos-hardware`, stabile PCI-IDs); Auflösung auf aktuelle Render-/Accel-Knoten bzw. NVIDIA-CDI-UUIDs erst beim Start. NVIDIA-Leistungslimit in `/config/plugins/nvidia/settings.json`, Grenzen aus `nvidia-smi`. Optionaler GPU-Wechsel je VM (`autoGPU`): zuvor laufende GPU-Container stoppen, Start konkurrierender GPU-Container sperren, nach Rückgabe des Treibers/CDI erneuern und wieder starten. Reservierungen in `/run/titanos-hardware/leases.json` überleben einen titand-Neustart. Kein automatischer Wechsel für iGPU/NPU/Coral.
- Plugin-Mitnahme: signierte Manifeste und Images vor dem Slot-Wechsel prüfen; pro Systembasis `/mnt/<volume>/.titanos/plugins/releases/<version>/` und `/config/plugins/releases/<version>.json`. Legacy-Dateien und Einstellungen bleiben für den Rückfall erhalten. Aktivierte Plugins gehören zum Update-Healthcheck; Mutationen während Download/ausstehendem Neustart sind gesperrt. Noch keine Bereinigung alter Plugin-Sätze. Der erste Übergang aus beta.2 benötigt bei einem Kernelwechsel das Abschalten und anschließende Neuinstallieren des NVIDIA-Plugins, siehe `docs/PLUGINS.md`.
- Weitere Plugins: `scripts/make-tools-plugins.sh` baut Intel GPU Tools aus Debian (Zusatzbibliotheken, originale Lizenztexte und Quellpaketlinks) sowie den eigenen Coral-USB-Helfer. Coral-Runtime/Firmware werden nicht verteilt; die Container-App bringt sie mit. USB-Bus-Zugriff ist in der Oberfläche ausdrücklich gekennzeichnet. PCIe-/M.2-Coral, ZFS, Lüftersteuerung, NVIDIA-Persistenz-Einstellungen, weitere Plugin-Einstellungen, Fortschritt in Schritten und reale Hardwaretests bleiben offen.
- Btrfs: RAID1 für Daten und Metadaten auf genau zwei GPT-Partitionen; UUID-basiertes Einbinden einschließlich degradiertem Start. Snapshots schreibgeschützt in `.titanos/snapshots`, Wiederherstellung als neue beschreibbare Kopie unter `restored`, nie über laufende Daten. Verschachtelte Subvolumes sind nicht enthalten. Scrub, Ersatz eines fehlenden Mitglieds, geschützte Snapshot-Pfade und Konfigurationssuche ohne Log-Replay umgesetzt. `scripts/smoke-storage.py` testet vier QEMU-Boots, Daten/Snapshots/Restore, fehlendes Mitglied, Ersatz und Intel-/Coral-Plugin-Aktivierung.

## Home Assistant (0.15.0)

- Paket `homeassistant`, Einstellungen › Home Assistant (nur Besitzer/Administratoren), `/config/homeassistant/settings.json`. Details: `docs/HOME-ASSISTANT.md`.
- REST nur zur eingetragenen Adresse, keine Weiterleitungen, kein Proxy; selbst signiertes Zertifikat nur mit SHA-256-Fingerabdruck. Long-Lived Token und MQTT-Passwort nur „ist gesetzt“; nie im Agenten-Katalog.
- **Agent:** Bereich `home` (ab Werk aus), nur Besitzer/Administratoren. Freigabe je Domäne oder Entität; nicht freigebbare Domänen werden nie angeboten. `ha_entities`/`ha_state` (Daten, keine Anweisungen), Aktion `ha_call` → `POST /api/v1/homeassistant/call` mit erneuter Prüfung. `lock`, `cover`, `alarm_control_panel`, `vacuum`, `button`, `valve`, `water_heater` mit Häkchen; Zielangaben im `data` abgelehnt.
- **Erweiterte HA-Rechte:** Entity-Policies schränken Freigaben mit Nur-Lesen und Dienstelisten weiter ein; Aliasnamen/Beschreibungen sind Daten. Zielzustandsbeobachtung und begrenzte einmalige Rücknahme einzelner Schalter; keine allgemeine physische Rollback-Garantie. Recorder-Abfragen höchstens 24 Stunden.
- **Automationen:** zusätzliche Ereignisauslöser (ODER), Zustandsbedingungen (UND/ODER), unveränderte Mindestdauer, Zeitfenster/Zeitzone, Hysterese, Abklingzeit, reine Vorschau, fünf vorherige Definitionen. Energieabhängige Auftragsplanung ist ausgeschlossen.
- **Auslöser:** WebSocket-Beobachter (`subscribe_trigger`, Plattform `state`) nur für Entitäten eingeschalteter Automationen; Zustandswechsel als Ringpuffer (20 je Entität) im RAM mit Marken aus Startzeit und Zähler, ausgewertet wie Meldungen per Hochwassermarke. Nur freigegebene Entitäten. Bei Unterbrechung nichts nachgeholt.
- **MQTT:** eigener MQTT-3.1.1-Client (Standardbibliothek, QoS 0, Last Will), Discovery unter `<prefix>/<component>/titanos_<id>/…`, Zustände unter `titanos/<id>/state/…`. `id` wird einmal zufällig erzeugt und gespeichert (die Machine-ID eines RAM-Systems ändert sich je Start). Werte nur aus dem RAM von titand, Platten über Seriennummer und ohne Weckabfragen.
- **MQTT-Ausbau:** bekannte Backup-Cachewerte, Wartungsbelegung und Anzahl laufender Kopier-/KI-Aufträge des HA-Kontos; keine Chattexte und keine zyklischen Backup-Zustandsdatei-Zugriffe.
- **Knöpfe/Schalter** (einschließlich Backup-Start, je Gruppe ab Werk aus): handeln als gewähltes Besitzer-/Administratorkonto über die normalen Routen (`internalAs`), protokolliert. Neustart/Herunterfahren abgelehnt, solange gearbeitet wird (`ha_power_refused`). Ohne Konto keine Knöpfe.
- **Assist:** Blueprint oder optionale Custom Integration unter `docs/homeassistant/custom_components/titanos`; Ereignisse `titanos_ask`/`titanos_answer`, als gewähltes Konto über normale Chat-Routen, ab Werk aus. Conversation-ID nur über konto-/verbindungsgebundene RAM-Zuordnung (30 Minuten), keine fremden Chat-IDs. Wiederholte Request-IDs starten keine neue Ausführung.
- Geprüft mit nachgebauten HA-REST-/WebSocket-Servern und MQTT-Broker; echtes Home Assistant, Mosquitto und die Apps noch zu prüfen.

## Arbeitsweise

- Das Projekt ist umgesetzt und veröffentlicht; keine früheren Phasenpläne als offene Implementierungsaufträge verwenden.
- Vor UI-Arbeiten aktuellen Entwurf und Folgeentscheidungen in `docs/design/README.md` lesen. Reale Messwerte und Kontorechte beibehalten; Beispieldaten nur in isolierten Tests.
- Historische Prüfberichte beschreiben ihren damaligen Umfang. Neue Funktionen mit passenden Tests absichern und verbleibende Hardware-/Integrationsgrenzen dokumentieren.

## Regeln

- Kein Unraid-Name, -Logo, -Code und keine 1:1-Unraid-Optik (auch nicht deren Orange-Verlauf). Bedienabläufe dürfen angelehnt sein. „Inspiriert von Unraid“ in der README ist okay.
- GPL-Komponenten (Kernel, Debian-Pakete) werden verteilt: jedem Release die passenden Quellen beilegen oder verlinken (im CI automatisieren).
- Destruktive Aktionen (RAID anlegen, Platte löschen) immer mit ausdrücklicher Bestätigung in der UI.
- Datenverlust vermeiden hat Vorrang vor Funktionen. RAID0 immer mit deutlichem Hinweis auf fehlende Redundanz; RAID ersetzt kein Backup.
- Benutzersperren, WebSocket-Origin-Prüfung, Terminal-Abschaltung und Sitzungsbegrenzung auch bei Konsolen ohne zweite Passwortabfrage beibehalten.

## NVIDIA-Treiber auf dem NAS

- Nutzerentscheidung vom 15.10.2026: öffentliche neue NVIDIA-Treiber automatisch herunterladen, offene Kernelmodule direkt auf dem NAS bauen, **nur per Knopf „Treiber vorbereiten“**. Keine älteren Archivversionen zusätzlich anbieten. Aktivierung weiterhin ausdrücklich beim nächsten NAS-Neustart, mit Boot-ID und Rückkehr zum mitgelieferten Treiber.
- Separate, im OS-Manifest authentifizierte Bauumgebung mit exakten Kernelheadern; Compiler, Pakete und Arbeitsdateien ausschließlich auf dem Plugin-Daten-Volume. 5 GiB frei, Build begrenzt auf 2 GiB RAM und 200 % CPU, Compilerlauf ohne Netzwerk. Die Bauumgebung gehört ausschließlich auf das Daten-Volume, nicht dauerhaft ins Basis-Abbild oder RAM-Overlay.
- NVIDIA-InRelease/GPG und Paketprüfsummen prüfen; fertiges lokales Image mit NAS-eigenem Schlüssel authentifizieren, in der UI „Auf diesem NAS gebaut“, niemals als offiziell signiert ausgeben. API-Status liest keine Konfiguration vom Stick. Details: `docs/NVIDIA-LOCAL-BUILD.md`.
- CI baut die passende Bauumgebung und prüft den realen Buildpfad. Der ältere Workflow für fertig gebaute Zusatztreiber bleibt manuell; täglicher CI-Treiberbau ist abgeschaltet. Hardwareaktivierung/CUDA/NVENC weiter auf echtem NAS prüfen.

## Monitoring, Ein-/Ausschalter und Fernzugriff

- Dashboard Layoutversion 3: CPU, RAM und Systeminfos getrennt, pro Kachel Rasterbreite/-höhe und Anzeigeoptionen. CPU-Temperatur optional aus coretemp „Package id 0“. GridStack besitzt seine DOM-Größen; Svelte darf beim Scrollen/Resize keine Neuinitialisierung auslösen. VM-/Container-Kacheln kompakt mit Name, CPU und RAM. VM-RAM = QEMU VmRSS, NAS-RAM bleibt MemTotal − MemFree. Netzwerk mit Schnittstellenwahl, IPv4/IPv6, Gateways, MAC, MTU und Traffic. Siehe `docs/MONITORING.md`.
- Einstellungen › Ein-/Ausschalter: kurzer physischer Tastendruck ignorieren/neustarten/herunterfahren, logind per Reload, persistente Wahl in `/config/system/power-button.json`; langer hardwareseitiger Ausschaltimpuls unverändert. Siehe `docs/POWER-BUTTON.md`.
- Tailscale als signiertes Plugin mit eigener Detail-/Einstellungsansicht, Subnet-Routing, NAS als Exit Node oder Nutzung eines anderen Exit Nodes, LAN-Zugang bleibt erhalten. Einstellungen mit zweiminütiger Sicherheitsfrist; private Geräteschlüssel auf dem Daten-Volume. Scoped Tailscale-Firewall-Hooks vor der allgemeinen Freigabe erhalten. Siehe `docs/TAILSCALE.md`; echtes Tailnet weiter prüfen.


## RAM, Uploads und Gerätezuordnung

- RAM-/Writeback-Budget: `/config/system/memory.json`, Standard 20 % von
  `MemAvailable` nach Systemreserve, 10–90 % in Zehnerschritten. Kein globales
  Cache-Leeren und kein behauptetes hartes Linux-Pagecache-Limit. Details:
  `docs/MEMORY.md`. Sysctl-Änderungen nur im NAS bzw. in Wegwerf-VMs testen.
- Browser-Uploads: begrenzte gemeinsame Schreibfenster, fsync vor Bestätigung,
  gezielte Seitenfreigabe, persistente Teildateien/Metadaten auf dem Volume.
  Wiederaufnahme nach Originaldateivergleich, aktuelle Rechte und Volume-/
  Dateiidentität prüfen; keine RAM-/Overlay-Ausweichablage.
- Neue Volumes: fremde ungemountete md-Arrays vor dem Löschen nur freigeben,
  wenn alle vorhandenen Mitglieder ausgewählt sind. Alle Platten vorher prüfen,
  Bootstick/konfigurierte Volumes/VM-Platten/unbekannte Holder ausschließen,
  nach Stoppen erneut prüfen. Keine Host-Platten für Tests verwenden.
- VMs können einen expliziten `directory`-Namen für verwaltete Daten/Firmware
  erhalten. Default bleibt der interne VM-Name; bestehende VMs werden nicht
  umgestellt. Ein direkt genutzter Abbildordner kann wiederverwendet werden,
  wenn er keiner anderen VM gehört und keine fremden Dateien enthält.
- Docker 26 / Compose 2.26: NVIDIA-CDI in `deploy.resources.reservations.devices`
  mit `driver: cdi`, qualifizierten `device_ids`, ohne `count`/`options` übergeben.
  `service.devices` interpretiert diese Compose-Version als Host-Dateipfade.
  Der Start eines alten, deshalb gescheiterten verwalteten Containers darf nur
  dessen Dienst mit `up --no-deps --force-recreate --pull never` erneuern;
  eingebundene Daten und fremde Dienste erhalten.


## VM-Snapshots und App-Vorlagen

- VM-Snapshots: eigener Reiter, manuelle Aufnahme, Wiederherstellung,
  Löschen und Zeitpläne mit Aufbewahrung. Vollständige unabhängige qcow2-/raw-
  Dateien, Konfiguration, UEFI und TPM; laufende Gäste einschließlich RAM mit
  ausdrücklich bestätigter Unterbrechung. PCI/USB, Hostordner, physische Platten
  und angehängte ISOs bleiben ausgeschlossen. Kein qcow2-Backing-Chain-Management.
  Private Ablage und Wiederanlaufjournal auf dem VM-Volume, kein RAM-/Stick-Staging.
  Details und Grenzen: `docs/VM-SNAPSHOTS.md`.
- Dateimanager schützt registrierte VM-Dateien samt Elternordnern vor Löschen,
  Umbenennen und Upload-Ersetzung. VM-Lebenszyklusaktionen bleiben der VM-Seite
  zugeordnet; normale Dateien sind weiterhin bearbeitbar.
- Eigene App-Vorlagen: Immich mit PostgreSQL/Valkey/ML; Ollama mit Open WebUI.
  CPU als Standard, GPU nur ausdrücklich pro Dienst. Keine vorinstallierten
  Sprachmodelle, Daten auf dem Docker-Volume, zufällige persistente Geheimnisse.
  Die abgeleitete Immich-Compose-Vorlage bleibt AGPL-3.0-only; Vorlagen und
  zugehörige Lizenzen liegen im separaten TitanOS-Apps-Repository, nicht im OS-Abbild. Details: `docs/NATIVE-APPS.md`.
- Storage-Prüfungen umfassen echte RAM-/Platten-Restores in einem verschachtelten
  QEMU-Gast auf Btrfs. App-Installations-/Persistenztests laufen im Apps-Repo,
  nicht in der OS-Release-CI; nur Wegwerf-Runner oder isolierte NAS-Test-VMs verwenden.

## Gemeinsame UI und App-Links

- Gemeinsame UI-Größen/Abstände und mobile Docker-Stacks, Filter, Formulare und
  Dialoge. Persönliche Schriftskalierung und Dashboard-Layouts bleiben erhalten.
- Katalog-Icons über `x-titanos.icon`/CasaOS; neue Installationen übernehmen
  `titanos.icon`. Weblinks: `titanos.webui`, aktiver Tailscale-Hostname und optionale
  `titanos.public-url` für externe Proxys. Details: `docs/APP-LINKS.md`.
- Storytold-Paketierung und App-Vorlagen bleiben ausschließlich in TitanOS-Apps.

## USB-Kopie, Konfigurationssicherung und Meldungstöne

- USB-Kopie-Backups: `backup/copy.go`, Ziel `usb-copy` mit Dateisystem-UUID.
  Keine Löschweitergabe und keine Versionierung/Verschlüsselung; ausschließlich
  Freigaben, reguläre Dateien und einfache Namensmuster. `os.Root` hält das
  geprüfte Ziel-Dateisystem fest, keine Ausweichschreibzugriffe ins RAM.
  Die USB-Verwaltung sperrt Auswerfen während einer Kopie. Siehe
  `docs/USB-COPY-BACKUPS.md`; Tests niemals auf Host-Platten ausführen.
- Stick-Konfiguration: zusätzlicher täglicher/wöchentlicher Zeitplan in
  `/config/system/backup.json`; NAS-Zeitzone, Startsicherung und Änderungsbündelung
  bleiben aktiv. Unveränderte Konfiguration wird nicht doppelt archiviert.
- Benachrichtigungstöne pro Nutzer in `sound.json`, standardmäßig aus. Lokale
  Web-Audio-Sinustöne, nur nach Browserinteraktion, keine Wiedergabe alter
  Meldungen beim Laden und höchstens ein Ton je zehn Sekunden. `docs/BETA7.md`.
- Projektinhaber meldet USB-Installation, Neustarts, RAID, Freigaben sowie USB
  und GPU auf echter Server-Hardware mit Beta 2 erfolgreich; keine pauschale
  Hardware-Abnahme neuer Beta-7-Funktionen daraus ableiten.

## Katalog und mobile Navigation

- Offizielle Quelle heißt „TitanOS-Apps“; interne ID `titanos` bleibt stabil.
  Beim Laden werden manuell hinzugefügte Doppelungen dieser Repository-URL
  einmalig aus `docker/sources.json` entfernt. Installationseinstellungen und
  andere Quellen bleiben erhalten. `Add` verhindert erneute Doppelungen.
- Die mobile Hauptnavigation nutzt ein natives Popover mit Rollenfilter und
  mindestens 44 px hohen Links. Schließt bei Navigation, Escape, Außenklick
  und Wechsel zur Desktopbreite. Kein Fokusfang oder verändertes Desktop-Scrolling.

## VM-Firmware-Details

- Beim Anlegen UEFI/OVMF (Standard) oder Legacy-BIOS/SeaBIOS. `firmware` in der
  VM-Konfiguration; fehlendes Feld bleibt UEFI. Auswahl nach dem Anlegen fest.
- BIOS verwendet q35 und `/usr/share/seabios/bios-256k.bin`, ohne NVRAM. Secure
  Boot mit BIOS wird abgelehnt; Windows-11-Option nur unter UEFI.
- Snapshots sichern bei BIOS keine UEFI-Variablen. RAM-Kompatibilität prüft
  die tatsächlich ausgewählte Firmware. TitanOS selbst bootet weiterhin nur UEFI.

## Datei-Editor und USB-Uploads

- `GET/PUT /files/text`: UTF-8 bis 512 KiB, gängige Textformate, JSON-Prüfung,
  Version per Inhalts-Hash gegen veraltete Entwürfe, temporäre Datei mit fsync,
  `.prev` als vorherige Fassung. Rechte, Eigentümer, Benutzerattribute und
  Zugriffs-ACLs erhalten; VM-Dateien und Stick-Geheimnisse schützen.
- Schreibbare Shares für berechtigte Nutzer, ganze Volumes/USB/Stick weiterhin
  nur über die vorhandenen Administrator-Wurzeln. Der direkte Texteditor bearbeitet keine Office-/PDF-Dateien; Dokumenterzeugung und Word-/Excel-Lesen durch den KI-Assistenten sind davon getrennt.
- USB-Verfügbarkeit anhand Mount-ID und Pfad statt leerem internen Volume-Namen.
  Upload-Abschluss mit `renameat2(RENAME_NOREPLACE)` über geöffnete Elternordner;
  keine Hardlink-Abhängigkeit für FAT/exFAT. FAT/exFAT/NTFS mounten root-only,
  damit private Upload-Metadaten nicht durch gelockerte Prüfungen ersetzt werden.
- `scripts/files_usb_smoke.py` prüft echte exFAT-Uploads und Textänderungen in
  der Wegwerf-VM. Nur neu erzeugte Image-Dateien formatieren, nie Host-Platten.

## SMB-Kopie-Backups

- Ziel auf einem zweiten NAS mit SMB-Zugangsdaten, Freigabenauswahl, Quellordnern und Zeitplan. Neue/geänderte Dateien kopieren; gelöschte Quelldateien im Ziel behalten. Keine Versionierung. Weiterhin als Beta gekennzeichnet; `docs/SMB-COPY-BACKUPS.md`.
- Docker-Apps zeigen ihren eingetragenen App-Namen; technische Container-Namen bleiben Identität und Suchmerkmal. Aktuelles Layout siehe nächster Abschnitt.

## Docker-Layout

- Vom Projektinhaber bestätigter Entwurf: gemeinsames Raster für Container und
  Compose-Stacks; Karten, Liste und Minimal. Minimal klappt per Namensknopf auf.
- „Seite anpassen“: Reihenfolge per Griff oder Pfeilen, Breiten 3/4/8 von 12
  Spalten (Schmal/Normal/Breit), Image/CPU-Verlauf wählbar, Speichern/Abbrechen/
  Standard. Listen nutzen volle Breite; kleine Bildschirme stapeln automatisch.
- Pro Konto in `/config/users/<name>/docker.json`, über die vorhandene atomare
  Preferences-API mit RAM-Cache. Nur ausdrückliches Speichern schreibt.
  Zuordnung über `app:<Compose-Projekt>` oder `container:<Name>`, nicht die
  nach Updates wechselnde Container-ID. Temporär fehlende Einträge bleiben
  gespeichert; neue Apps werden angehängt. Keine App wird durch Layouts versteckt.


## Namen und Dateimanager

- Nutzerentscheidung vom 20.10.2026: benutzergewählte Speicher- und Sicherungsnamen behalten ihre Groß-/Kleinschreibung. Keine automatische Umwandlung von `Backup` in `backup`. Volumes: ASCII-Buchstabe am Anfang, danach ASCII-Buchstaben, Ziffern und Bindestriche, höchstens 12 Zeichen; Aufträge, SMB-Ziele und sendende NAS höchstens 31 Zeichen. Gemeinsame Backend-Prüfung in `internal/naming`; unzulässige Namen ablehnen statt umschreiben. Technische Protokollvorgaben etwa für S3-Buckets bleiben erhalten.
- Neue Volumes, Sicherungsaufträge und Zielbezeichnungen dürfen keine nur in der Groß-/Kleinschreibung abweichenden Duplikate anlegen. Vorhandene Namen und Pfade werden nicht migriert oder umbenannt. Freigaben, Dateien und Unterordner werden bei Kopie-Backups mit ihrer Originalschreibweise übertragen; der feste Standardordner `backups/` ist kein umgewandelter Benutzername.
- Freigaben bleiben im Dateimanager direkt sichtbar; Volumes stehen unter „Erweiterter Zugriff“. Ohne gespeicherte Auswahl wird eine Freigabe bevorzugt. Ein gewähltes Volume öffnet den Bereich.
- Papierkorb als fester Navigationseintrag für den ausgewählten Speicherort. Bei deaktiviertem Freigabe-Papierkorb oder nicht unterstütztem Speicherort die Ursache anzeigen. Bei aktivem Papierkorb fragt der Löschdialog nach „In den Papierkorb“ (Standard) oder „Endgültig löschen“; endgültiges Löschen braucht eine ausdrückliche Bestätigung. Keine automatische Änderung bestehender Papierkorb-Einstellungen.
- Dateiliste: Suche filtert nur den geladenen Ordner ohne Änderung der Schreibweise, Sortierung nach Name (natürliche Zahlenreihenfolge), Größe oder Änderungsdatum in beide Richtungen, Ordner immer zuerst. Neue Suche hebt die Auswahl auf; „Alle auswählen“ wählt nur sichtbare Treffer. Ordnerwechsel setzt den Suchtext zurück. Keine rekursiven oder zyklischen Datenträgerabfragen für die Suche.
- Dateimanager: Zurück/Vorwärts stellt Ordner, Suche, Auswahl und Scrollposition innerhalb der Seite wieder her; „Eine Ebene höher“ öffnet den Elternordner. Umschalt-Klick wählt einen Bereich in der sichtbaren Sortierung, auch über die Checkboxen. Eintragsmenü per Dreipunktknopf oder Rechtsklick mit Eigenschaften und den erlaubten Aktionen.
- Vorschau über den bestehenden autorisierten Download: Rasterbilder (PNG/JPEG/GIF/WebP) bis 20 MiB, unterstützte UTF-8-Textdateien bis 512 KiB; HTML bleibt Text, SVG wird nicht als Bild gerendert. Downloadgröße wird während des Lesens begrenzt; Schließen bricht ab und gibt Bild-URLs frei. Ordnerfavoriten (höchstens 50), Ansicht (Details/kleine/große Symbole), Sortierung und letzter Freigabe-/Volume-Speicherort liegen je Konto in `/config/users/<name>/files.json` über die atomare Preferences-API mit RAM-Cache; nur weiterhin zugängliche Speicherorte anzeigen. Kein Local-/Session-Storage für diese Einstellungen. Speichern nur nach Benutzeränderungen, Fehler sichtbar mit Wiederholung.

- Dateimanager-Ergänzung: Rechtsklick auf ausgewählte Einträge erhält die Mehrfachauswahl; Rechtsklick auf freie Fläche bietet Neuer Ordner, Hochladen, Einfügen und Ansichten. Strg/Cmd+A/C/X/V, F2 und Entf beachten Schreibrechte, Eingabefelder und offene Dialoge. Kopieren/Ausschneiden merkt Quellpfade während des Seitenbesuchs; Einfügen öffnet den vorhandenen Dialog am Zielordner (Standard bei Namenskonflikten: umbenennen). Entf öffnet immer den Löschdialog. Pfadeingabe und kopierter Pfad beziehen sich auf die aktuelle Freigabe/das Volume, nie auf beliebige Host-Pfade. Große Symbole laden Rasterbild-Miniaturen bis 2 MiB nur bei Sichtbarkeit, mit Größenprüfung während des Downloads, Abbruch und Freigabe von Blob-URLs. Größere Dateien behalten ein Symbol und die bisherige Vorschauaktion.
- USB-Backups im Dateimanager: Besitzer/Administratoren sehen konfigurierte `usb-copy`-Aufträge links mit unverändertem Zielpfad, auch wenn das Laufwerk fehlt. Zuordnung über die gespeicherte Dateisystem-UUID, nie Gerätenamen. Öffnen bindet ein angeschlossenes, unterstütztes Laufwerk bei Bedarf ausdrücklich ein und öffnet den vorhandenen Zielordner; es erstellt keine Sicherungsordner. Fehlender Ordner, fehlendes Laufwerk und Ladefehler werden erklärt. Auftragsliste beim Öffnen und auf ausdrückliches Aktualisieren laden; bestehende USB-Geräteerkennung bleibt erhalten.
- USB-Backup-Seitenleiste zeigt den letzten erfolgreichen Lauf, Fehler/Warnungen, laufenden Fortschritt und bei eingebundenem Laufwerk freien Speicher. `/api/v1/backups/progress` liefert ausschließlich zwischengespeicherte USB-Auftragszustände aus RAM, nur für Besitzer/Administratoren. Die sichtbare Dateiseite aktualisiert diese alle fünf Sekunden; keine zyklischen Sicherungsarchiv- oder `/config`-Zugriffe. Ohne bekannte Gesamtgröße bleibt der Fortschritt unbestimmt.
- USB-Wiederherstellung: gesamter konfigurierter Backup-Ordner über die Seitenleiste oder ausgewählte Dateien im Backup-Pfad. Der vorhandene Kopierdialog bietet dafür nur beschreibbare Freigaben/Volumes und legt einen neuen Unterordner an; vorhandene Zielordner werden abgelehnt. Anschließend Kopie mit `move=false`, Konfliktregel `rename`. Originale und Backup bleiben erhalten. Während einer als laufend bekannten Sicherung sind die Wiederherstellungsknöpfe deaktiviert. Ein nach fehlgeschlagenem Kopierstart bereits angelegter Ordner wird angezeigt und bei Wiederholung im selben Dialog wiederverwendet; kein automatisches Löschen dieses Ordners.
- Weitere Dateimanager-Ergänzungen: Abschlussmeldungen für beobachtete Kopier-/Verschiebe-/Wiederherstellungsaufträge bieten „Zielordner öffnen“; fehlgeschlagene Aufträge behalten den betroffenen Pfad. Auswahl zeigt Dateigrößen sofort, Ordnergrößen nur nach ausdrücklicher Anforderung. `/files/inspect` verwendet die vorhandene Root-Rechteprüfung, `os.Root`, keine Symlink-Verfolgung, keine internen/geschützten Pfade, 15 Sekunden Frist; der Konfliktvergleich zeigt höchstens 200 bestehende Zieldateien einschließlich Unterordnern. Bei gekürztem Vergleich bleibt Ersetzen gesperrt. Ersetzen verlangt zusätzlich eine ausdrückliche Bestätigung. Bei übersprungenem Konflikt auf dem Kopierpfad eines Verschiebeauftrags wird abgebrochen und die Quelle erhalten, statt sie nach dem Überspringen zu entfernen.
- Punktdateien werden über „Versteckte Dateien anzeigen“ ein-/ausgeblendet; Auswahl wird beim Umschalten aufgehoben, Einstellung im Benutzerkonto. Interne und geheime Dateien bleiben serverseitig verborgen. Konfigurierte USB-Backup-Laufwerke werden in der Dateiseite standardmäßig geschützt durchgesehen; Änderungen erst nach Dialogbestätigung. Schutz gilt für Upload/Drop, Textbearbeitung, Löschen, Verschieben, Umbenennen, Einfügen, Rechtsklick und Tastenkürzel sowie Kopierziele. Entsperrung gilt nur auf dieser Seite für den aktuellen Speicherort und endet beim Wechsel des Speicherorts. Dies ist ein Bedienungsschutz, keine Änderung der Administratorrechte oder des Mount-Modus.
- Weitere fünf Dateimanager-Funktionen: Wiederholen fehlgeschlagener/abgebrochener Kopieraufträge innerhalb der titand-Laufzeit, ohne bereits abgeschlossene Dateien erneut zu übertragen; aktuelle Root-Rechte/Zuordnung prüfen, bei nachträglich geänderten Quellen eines Verschiebeauftrags Quelle behalten. Papierkorb mit flacher Dateiliste, vier Sortierungen und Wiederherstellungsvorschau; neue Löschungen behalten Originalpfad und Zeitpunkt im Attribut `user.titanos.trash`, ältere/Samba-Einträge und Dateisysteme ohne Attribute verwenden vorhandene Pfade/Zeitstempel. Zweite Ordneransicht mit Richtungsknöpfen, im Konto gespeichert; USB-Bedienungsschutz gilt auch dort und für Wiederholungen. Upload-Prüfdialog vor jeglichen Datei-/Ordnerschreibzugriffen mit Umfang, freiem Speicher und einzelnen Konfliktentscheidungen; Ersetzen ausdrücklich bestätigen. Eigenschaften mit logischem Gesamtpfad, Typ, Größe, Dateisystemzeiten, Unix-Rechten und optionaler SHA-256-Berechnung/Vergleich; Anfrage abbrechen beim Schließen, serverseitig zwei Minuten Frist.
- Weitere ausdrücklich freigegebene Dateimanager-Funktionen: Unterordnersuche auf Knopfdruck mit Name/Endung/Größe/Datum und „Ordner öffnen“ (keine Hintergrundindexierung; 30 Sekunden, 50.000 Einträge, 1.000 Treffer, sichtbare Begrenzung). Die bisherige lokale Suche bleibt bestehen. Sammelumbenennung mit Vorschau, wortgetreuer Ersetzung/Präfix/Suffix/Nummerierung, Kollisions- und Versionsprüfung, Veröffentlichung ohne Überschreiben. ZIP-Inhaltsansicht und selektive Extraktion als abbrechbarer Datei-Auftrag in einen neuen, vorgeprüften Zielordner; Staging auf dem Volume, atomare Veröffentlichung nach CRC-Prüfung; keine Symlinks, unsicheren Pfade oder verschlüsselten Einträge, maximal 20.000 Einträge/1 TiB. Ordnervergleich der beiden Ansichten nach Metadaten oder optional SHA-256, ohne Schreibzugriffe, begrenzt auf zwei Minuten/50.000 Einträge je Seite. Rückgängig-Verlauf im RAM, je Konto maximal 20 Aktionen/30 Minuten, für Umbenennen, Papierkorb und nicht überschreibende/zusammenführende Verschiebungen. Aktuelle Rechte, Root-Zuordnung, vollständiger Baumzustand und freie Originalnamen werden erneut geprüft. Endgültiges Löschen bleibt ausgeschlossen. Grenzen und Details stehen in `docs/design/README.md`.
- Weitere Dateimanager-Ergänzungen: Kopieren/Verschieben je Konto mit Pause/Fortsetzen, drei Prioritäten und optionalem MiB/s-Limit (0 unbegrenzt). Seit 0.16.0 erhalten Ziele auf Daten-Volumes dauerhafte Pläne/Ereignisjournale; andere Ziele bleiben im RAM. Kontrollpunkte an Dateien und 256-KiB-Blöcken; aktuelle Root-Rechte vor Fortsetzen, kein Übergreifen auf fremde Aufträge. ZIP-Mehrfachdownload mit Umfangsvorschau und frei wählbarem Namen, leere Ordner enthalten. Speicheranalyse nur nach Start, maximal 50.000 Einträge/30 Sekunden, direkte Unterordner nach logischer Größe und 50 größte Dateien. Eigenschaften erklären TitanOS-Rechte einschließlich Freigabe-/VM-/Stick-/USB-Schutz. Aktive Ordnerlisten alle zehn Sekunden aktualisieren, pausiert bei ausgeblendeter Seite, Dialogen/Eingaben; keine Rekursion und kein Systemstick. Auswahl/Scrollposition erhalten, veraltete Antworten verwerfen. Details und Grenzen in `docs/design/README.md`; Docker unverändert.
- Dateimanager: `.prev`-Textfassungen auf Anforderung gegenüberstellen und nach Bestätigung mit Versionsprüfung wiederherstellen; aktuelle Fassung wird dabei `.prev`. Upload-Warteschlange gemeinsam pausieren/fortsetzen, nur Fehlschläge wiederholen, Gesamtfortschritt; Browser-Dateiobjekte bleiben nur bis zum Schließen des Tabs. Duplikatsuche ausdrücklich nach Größe/SHA-256, begrenzt auf 50.000 Einträge/zwei Minuten; Löschvorschau mit Bestätigung und erneuter Eintragsversionsprüfung, bevorzugt Papierkorb. Kopierkonflikte optional einzeln überspringen/ersetzen/unter ausdrücklich gewähltem Namen übernehmen. Ordnervergleich übernimmt ausgewählte Dateien in beide Richtungen mit erhaltenen Unterordnerpfaden, Vorschau, Versionsprüfung und bestätigtem Ersetzen. USB-Sperre/Root-Rechte beachten, keine automatische Synchronisation oder Löschung. Details in `docs/design/README.md`.
- Dateimanager: Werkzeuge gebündelt, Richtungsknöpfe passend zur Auswahl. Linker Ordnerbaum lädt unmittelbare Unterordner erst beim Aufklappen, unabhängig von Navigation; große Kindlisten in 100er-Schritten. Internes Ziehen öffnet Ziel-/Aktionsbestätigung und danach den bestehenden Konfliktdialog, mit USB-/Root-Schutz. Beide Ansichten virtualisieren ab 201 Treffern; Suche/Sortierung/Gesamtauswahl bleiben vollständig, innere Scrollposition gehört zum Navigationsverlauf. Textfassungen mit markierten Zeilen, Zeilennummern und synchronem Scrollen; begrenzter Abgleich mit erklärtem positionsweisem Rückfall für große Änderungsblöcke. Details in `docs/design/README.md`; Docker unverändert.
- Dateimanager: PDF-Vorschau bis 20 MiB, natives Audio-/Video-Streaming über autorisierten Download mit enger MIME-Allowlist und HTTP-Range, ohne Autoplay; Bildwechsel per Knopf/Pfeiltaste. Papierkorbfilter nach Originalordner/Löschdatum, Auswahlvorschau und Speicherbelegung mit Hardlink-Deduplizierung. Auftragsanzeige mit gemessener Geschwindigkeit, geschätzter Restzeit, Statusfehlern und übersetzten Fehlerursachen. Pfeiltasten/Enter/Leertaste in beiden Dateilisten, F1-Kürzelübersicht; virtuelle Zeilen in den Fokus scrollen. Grenzen in `docs/design/README.md`, keine Änderungen an Docker.
- Dateimanager 0.7.0: benannte/sortierbare Favoriten (50), Ordneransichten je Root/Pfad (200) im Konto; keine Besuchshistorie. Neue TXT/MD/JSON-Dateien über gepinntes Zielverzeichnis und atomare Veröffentlichung ohne Überschreiben, danach vorhandener Editor. Auswahl umkehren/nach Endungsgruppen auf vollständiger Filterliste. Übertragungsvorschau mit Umfang/freiem Speicher und frühen Zielwarnungen; vorhandene Konflikt-/Rechteprüfungen erhalten. Docker-Oberfläche unverändert.
- Dateimanager 0.7.1: **Bedienung aufgeräumt.** Zwei Leisten: oben Verlauf/Hoch (Symbole), Brotkrumen (Klick auf freie Fläche oder Stift = Pfadeingabe, Esc bricht ab), Favorit, Ordnermenü (Pfad bearbeiten/kopieren, Windows-Pfad, Neu laden), Suche; darunter Hochladen, „Neu“ (Ordner, Datei, Ordner hochladen), „Einfügen“ nur mit Zwischenablage, Dropdowns Auswahl/Werkzeuge, Papierkorb (nur Freigaben), Ansicht als Symbole, zwei Ordner, Ansicht-Panel (versteckte Dateien, Ansicht merken, Tastenkürzel). Sortieren über die Spaltenköpfe. Kontext- und „⋯“-Menü sind schwebende `role=menu`-Menüs (`ContextMenu.svelte`, `Dropdown.svelte`, Pfeiltasten, Umschalt+F10/Menütaste), keine Dialoge. Doppelklick öffnet Vorschau/Editor; der zweite Klick eines Doppelklicks auf einen Ordner wirkt im neuen Ordner nicht. Dateityp-Symbole nur aus dem Namen (`fileKind`). Die Aktionsleiste der Auswahl haftet unten (Smartphone: Herunterladen, Löschen, „⋯“), Esc hebt die Auswahl auf. Linke Spalte als `Places.svelte`: Favoriten oben, Platzbalken unter dem gewählten Ort, USB-Backups nur wenn eingerichtet; auf dem Smartphone hinter „Speicherort: …“ eingeklappt.

## UI-Abläufe (0.16.0)

- Docker-Stacks starten in Listen- und Kartenansicht geschlossen. Suche/Filter öffnen passende Dienste; das bewusste Auf-/Zuklappen bleibt innerhalb der geöffneten Seite möglich. Status und nicht laufende/neu startende Dienste bleiben am geschlossenen Stack sichtbar.
- Stack-WebUI/Tailscale-/öffentliche Links stammen aus genau einem vorhandenen Hauptdienst. Kontogetrennte Auswahl unter `docker.primaryServices` nach Gruppenkennung und Compose-Dienstname; keine Container-ID speichern, keine stille Neuzuordnung eines fehlenden ausdrücklich gewählten Dienstes. Bestehende URL-Prüfung wiederverwenden.
- Docker zeigt bei fehlgeschlagenen Abfragen den letzten Stand als möglicherweise veraltet mit Zeit und Wiederholen; keine Umdeutung eines Fehlers in „alles aktuell“. Entfernen-Dialoge bei Fehlern offen halten. Compose-Änderungen vor Navigation erhalten.
- Befehlspalette in der Kopfzeile und per Strg/⌘+K: Seiten/Einstellungen, autorisierte Freigaben, verwaltete Apps und VMs; keine Shell oder mutierenden Aktionen. Objektabfragen nur beim Öffnen, Fehler sichtbar, Kontorechte serverseitig unverändert. Benutzer erhalten keine Docker-/VM-Abfragen.
- `scripts/test-ui-workflows.mjs` verwendet ausschließlich isolierte API-Daten für Shares, Formulare, Kontentrennung, Docker-Stacks und Befehlspalette; Bilder unter `out/validation/ui-workflows/`.

## Speicher-, Wartungs- und Auftragsabläufe (0.17.0)

Maßgebliche Bedienung, Grenzen und Prüfungen: `docs/WORKFLOWS.md`.

- Übernahme nur für unterstützte bereits zusammengesetzte mdadm-Arrays (ext4/XFS) und native Btrfs-Spiegel auf genau zwei physischen Platten. Erkennung unverändernd, Vorschau mit ausgeschalteter Journalwiedergabe nur lesend. Registrierung nach erneutem Fingerabdruck-/Namensabgleich speichert nur UUIDs, formatiert nicht und importiert keine Konten/Freigaben/Standardordner.
- Erweiterung fügt einem gesunden RAID5/6 mit ext4/XFS eine eindeutig identifizierte Platte hinzu. Ein separates gesundes Daten-Volume hält mdadm-Rettungsdatei und Journal, `/config/storage/growth.json` nur den Verweis. Checkpoint vor jedem Schritt; keine wiederholte Partitionierung nach Absturz. Dateisystem erst nach gesundem, abgeschlossenem Umbau vergrößern. Offener Vorgang sperrt andere Speicheränderungen und App-Updates. Wiederaufnahme nur nach Prüfung; keine erzwungene Assembly oder ungültige Rettungsdatei. Vorbereitung nur bei verifizierter alter Geometrie ohne Reshape abbrechen.
- Btrfs-Dateibrowser: nur reguläre Dateien/Ordner, keine Symlinks oder internen Daten; bis 200 Dateien/10 GiB in einen neuen Ordner wiederherstellen. Geplante Snapshots gehören zum Wartungsauftrag; Aufbewahrung inventarisiert Name und Btrfs-UUID in `<auftrag>.v2.json`, prüft die UUID unter derselben Speichersperre vor dem Löschen und erhält manuelle Ersatzkopien gleichen Namens. Alte Namensinventare begründen keine Löschrechte und werden geleert; vorhandene Kopien bleiben erhalten. Eine Restore-Vorschau zeigt Umfang und Ziel und bindet den folgenden Start an dieselbe Snapshot-UUID.
- Wartung ist ab Werk aus. Genau eine Ausführung, gemeinsame IANA-Zeitzone/Fenster, täglich/wöchentlich/monatlich, verpassten letzten Termin nachholen oder überspringen. Explizite schwere API-Routen nutzen die gemeinsame Aufnahme; Datei-Wiederholung/Fortsetzen sowie automatische Backup-/VM-Snapshots ebenso. Bekannte NAS-Arbeit einschließlich Docker-App-Aufträgen abwarten. Ein optionaler Vorgänger muss im aktuellen Termin erfolgreich sein; Zyklen/fehlende Vorgänger ablehnen. Abhängige Aufträge für ältere Apps mit `enabled=false` speichern und in der aktuellen Version aus `dependencyEnabled` wiederherstellen. Optional fünf Minuten vor fälligen Sicherungen zurückstehen, aktive Arbeit nicht verdrängen. Wartegründe sichtbar machen. Normale SMB-/NFS-Zugriffe nicht pauschal anhalten. Einstellungen im RAM/atomar auf Stick, Checkpoints und 100 Ergebnisse auf Daten-Volume. Nach Absturz unbekannte Ausgänge bis zur ausdrücklichen Prüfung sperren; Entfernen/Umwidmen eines gesperrten Auftrags ist kein Ersatz für die Prüfung. Bei fehlendem Volume darf nur eine sonst unveränderte Konfiguration deaktiviert werden; Journal und Sperren erhalten. Globale Einstellungen und Dialogentwürfe getrennt halten, Abbrechen und Navigationsschutz müssen alle bearbeiteten Werte erfassen. SMART-Langtest nur nach beobachtetem Start und erfolgreichem Abschluss als bestanden melden; alte/fehlende Ergebnisse nie als Erfolg.
- Restore-Proben nur für versionierte restic-Backups: begrenzte reguläre Dateien aus genau einem Stand, private neue Kopie, `--verify`, exakte maskierte Pfade, anschließend entfernen. Ziel über geerbten Verzeichnisdeskriptor festhalten, nie bei Volume-Verlust auf Overlay ausweichen. Separates Probeergebnis; keine Vollprüfung behaupten.
- Anwendungssicherungen: explizite Auswahl/Stopp-Erlaubnis; sauberes Stoppen von Docker bzw. unterstützten VMs oder fester PostgreSQL-Export ohne Shell/Passwortargumente. Tatsächliche persistente Pfade erfassen, weitere aktive Schreiber unabhängig von der gewählten Anwendungsart ablehnen, einschließlich beschreibbarer virtiofs-Host-Ordner. Auch eine konfigurierte deaktivierte Docker-Engine unter Lease prüfen; nur ihr tatsächlich fehlender/abgewiesener Socket bedeutet kein Docker-Inventar. Lifecycle währenddessen sperren. Vorherigen Laufzustand und originale IDs/UUIDs vor Stopp auf Volume protokollieren. Nur vorher laufende Originale wieder starten, Fehler erhalten. Nach Absturz nicht blind neu starten oder Backup wiederholen; Prüfung unter Werkzeuge › Aufträge. Neue Vorgänge auf sichtbarer Seite aktualisieren; ursprüngliche Mitglieder/Laufzustände/Phasen erst für die ausdrückliche Prüfung aus dem Volume-Journal laden. `coordinatedSelection` hält alte Quellfelder solcher Aufträge leer, damit vorige App-Versionen nicht unkoordiniert sichern; beim aktuellen Laden zurückwandeln. Bestehende normale Aufträge bleiben unverändert.
- Längere Monitoring-Verläufe optional, ab Werk aus: Minutenwerte auf Daten-Volume, 7/30 Tage, 8–256 MiB, keine periodischen Stickzugriffe. Zählerrücklauf/Beobachtungslücken berücksichtigen, mittlere I/O-Latenz nicht als Perzentil ausgeben. Alte Tagesdateien begrenzen; fehlerhafte vollständige Datensätze anzeigen, nur unvollständigen letzten Anhang verwerfen. Anzeige dünnt mit erhaltenen Lücken aus. Hinweis auf mögliches Aufwecken ruhender Datenplatten. CSV-/JSON-Export mit sichtbaren Lücken und höchstens 250.000 CSV-Messzeilen. Kapazitätsprognose nur aus mindestens 24 stündlichen Beobachtungen im aktuellen zusammenhängenden Abschnitt, stabiler Kapazität und frischen Werten; hohe Schwankung/zu wenig Daten ergeben unbekannt. Hinweise bei 90 % oder höchstens sieben projizierten Tagen bis dahin nur auf der geladenen Seite, kein zusätzlicher Push.
- VM-Klon nur ausgeschaltet und ohne nicht unabhängig kopierbare Ressourcen/Backing-Ketten. Unabhängige geprüfte Platten, NVRAM/TPM, neue UUID/MAC, kein Autostart und keine exklusiven Kernreservierungen. Volume-Staging/Journal, Veröffentlichung ohne Überschreiben, Registrierung zuletzt. Verifizierte unterbrochene Veröffentlichung prüfen/abschließen statt erneut kopieren.
- Dateiübertragungen auf Daten-Volumes: kontogetrennter unveränderlicher Plan und begrenzte Append-Ereignisse (50.000 Dateien, 32 MiB Plan, 128 MiB Journal). Vor Veröffentlichung/Quellentfernung dauerhaftes Intent, danach echte Ergebnisse und Verzeichnis-fsync. Unbekannte Ausgänge nicht erneut ausführen. Nach titand-Neustart aktuelle Rechte/Root-Zuordnung, Boot-/Verzeichnisidentität und Datei-/Zielversionen prüfen; fertige Dateien überspringen, offene Datei neu beginnen. Nur bekannte eigene temporäre Inodes entfernen. Nach NAS-Neustart wegen geänderter Geräteidentität keine ungeprüfte Wiederaufnahme. 20 bekannte fertige Ergebnisse je Konto/Volume; gesperrte nicht verdrängen. Browser-Uploads/Extraktion/Undo behalten ihre eigenen Grenzen.
- Speicherverbrauch nur auf Anforderung, höchstens 30 Sekunden/200.000 Einträge und begrenzte Verzeichnis-Leseblöcke. Freigaben, Apps/VMs, Papierkorb, Snapshots und Sonstiges; private Betriebsdaten ausnehmen. Grafische Anteile beziehen sich auf erfasste logische Daten, nicht auf garantiert frei werdende Blöcke. Teilergebnisse sichtbar; Blöcke bei Hardlinks/Reflinks/Snapshots nicht als frei werdenden Platz versprechen.
- Auftragsseite und Dashboard verwenden eine gemeinsame typisierte Zustandszuordnung. Filter, Suche, bekannte abgeschlossene Ergebnisse und unterstützte direkte Steuerung; fehlgeschlagene Docker-/VM-Aufträge, globale Wartungsfehler und unbekannte Quellen dürfen keine Entwarnung ergeben. Nur eigene Datei-/Upload-/KI-/Automationsdaten, Systemarbeit weiterhin nur für Besitzer/Administratoren. Ereignis-Automationen ohne Termin bleiben wartend; keine Termine im Jahr 1. Verlauf bleibt je Fachbereich begrenzt, kein unbegrenztes neues Gesamtarchiv.
- Sicherungsabdeckung unter Einstellungen › Sicherungen: ausdrücklich zugeordnete Shares/Container/VMs, Teilumfang/manueller Zeitplan, letzter Erfolg/Archivprüfung/Restore-Probe und fehlende lokale Ziele. Auch nicht mehr inventarisierbare konfigurierte Quellen erhalten; fremde Ziele nicht zyklisch auf Erreichbarkeit prüfen. Reine Rohordnerkopien nicht als koordinierte App-Abdeckung zählen.
- Neue Anwendungswiederherstellung: historische Pfade/Umfang/Ziel vorab anzeigen, ursprüngliche App-Metadaten aus dem begrenzten Archivprotokoll lesen, falls vorhanden; sonst aktuelle Auswahl ausdrücklich kennzeichnen. Neue Kopie in privatem Volume-Staging über geerbten FD, `restic --verify --overwrite never`, Veröffentlichung per `RENAME_NOREPLACE`, eindeutiger Name, Ergebnis getrennt vom Backup-Erfolg. Ganze/selektive restic-Restores verwenden denselben Ablauf; Zielbaum vor Veröffentlichung 0700. Keine automatische App-Registrierung/-Start, keine Live-Konfigurationsübernahme. Gastagent-/Freeze-Sicherung bleibt separater Ausbau.
- Betroffene Svelte-Seiten/Komponenten mit `npm run format:workflows` formatieren. Formatter nur Entwicklungsabhängigkeit. Gemeinsame isolierte Browser-Fixtures in `scripts/test-ui-fixtures.mjs`, zusätzliche Fälle in `scripts/test-ui-completion.mjs`.
- Private operative Daten unter `.titanos/<funktion>` müssen root gehören und dürfen nicht durch andere beschreibbar oder verlinkt sein. Produktive `/mnt`-Wurzeln über die Kernel-Mount-ID prüfen und Dateideskriptoren festhalten; ein verschwundenes Volume ist kein beschreibbarer Overlay-Ersatz. Tests verwenden ausdrücklich alternative isolierte Wurzeln.
- `backend/tests/smoke_workflows.py` verwendet ausschließlich neue QEMU-Datenträger. `scripts/test-ui-operations.mjs` prüft die neuen Seiten mit isolierten Daten, zusätzlich zu `scripts/test-ui-workflows.mjs`. Go-Race- und konkrete Hardware-/Unterbrechungsgrenzen in den Release-Notizen festhalten.
