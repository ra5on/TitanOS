# TitanOS

Ein schlankes NAS-Betriebssystem, das vom USB-Stick bootet und komplett im RAM läuft – inspiriert von Unraid, aber mit klassischem RAID. Wird auf GitHub veröffentlicht.

> **Diese Datei enthält nur Kernregeln; Fachregeln stehen in `docs/rules/` bzw. den verlinkten Dokumenten. Vor Arbeiten an einem Bereich dessen Regeldatei lesen. Neue Regeln in die passende Regeldatei, nicht hier anhängen.** Verzeichnis: Abschnitt „Fachregeln“ unten und `docs/rules/README.md`.

Stand: **0.43.0** (Release-Prüfstand siehe docs/releases/v0.43.0.md). Diese Datei und die Regeldateien beschreiben die aktuell geltenden Entscheidungen, nicht die Reihenfolge ihrer Einführung. Historische Release- und Testberichte stehen in `docs/releases/` und den Fachdokumenten. Bei Änderungen die betroffene Regel dort aktualisieren, wo sie steht, statt widersprechende Nachträge anzuhängen.

Eigenständiges Projekt. Keinen Code, keine Struktur und keine Annahmen aus anderen Projekten übernehmen.

## Festgelegte Entscheidungen

| Bereich | Entscheidung |
|---|---|
| Basis | Debian 13 (trixie) minimal, amd64 |
| Boot | 64-Bit-UEFI mit systemd-boot; seit 0.41 zusätzlich GRUB für 32-Bit-UEFI und Legacy-BIOS mit denselben A/B-Einträgen (`docs/BOOT.md`); Secure Boot aus |
| Stick | FAT32, Label `TITANOS` |
| Systemabbild | squashfs (zstd), wird beim Boot **komprimiert** in den RAM (tmpfs) kopiert, darüber overlayfs mit größenbegrenzter tmpfs-Schicht (512 MiB) |
| Nach dem Boot | Stick wird nur noch für Konfigurationsänderungen und Updates beschrieben |
| Updates | ein Stable-Kanal; volle Systemupdates in A/B-Slots mit Boot-Rückfall, kompatible App-Updates nur für titand/Weboberfläche ohne NAS-Neustart mit eigenem Rückfall |
| Auslieferung | Zip zum Kopieren auf einen FAT32-Stick. Kein Pflicht-`.img` (optional später im CI) |
| Speicher | klassisches RAID per **mdadm** (0, 1, 5, 6, 10) mit ext4/XFS (umgesetzt); Btrfs mit den Profilen single, RAID 1 und RAID1C3, Snapshots, Platten hinzufügen/entfernen und Profilwechsel (umgesetzt, siehe docs/BTRFS.md); ZFS ggf. später |
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

- Umgesetzt: Ersteinrichtung, Konten/Rollen und TOTP, SMB/NFS, Dateimanager, mdadm und Btrfs-Spiegel, Docker/Apps, VMs, LXC-Systemcontainer, Plugins, Netzwerk mit Rückfall, Firewall (ab Werk aus), Backups, USV, Benachrichtigungen, Diagnose, Aktivitätsprotokoll, SSH/Zertifikate und Themes. Die jeweiligen Abschnitte unten nennen Grenzen und Prüfstand.
- Ersteinrichtung: frei gewähltes Besitzerkonto, danach `/welcome` mit Sprache, Hostname, Zeitzone, automatischer Heimnetz-Bridge und nächsten Schritten (Einrichtungsliste mit Erledigt-Zustand: Volume, ggf. Treiber, Freigabe, 2FA, Benachrichtigungen, Stick-Sicherung, Datensicherung, Fernzugriff per Tailscale/WireGuard, Mitteilungen aufs Handy, Apps; jederzeit über Einstellungen › Über TitanOS bzw. `/welcome?step=3`). Das erste Volume wird auf der Speicher-Seite angelegt.
- Deutsch und Englisch; alle Oberflächentexte über Übersetzungsdateien.
- Seit 0.8.0 App-Updates; seit 0.14.0 persönliche Benachrichtigungen, Home Assistant, Energie-Zeitplan mit Wake-on-LAN sowie Prüfen und Einzeldatei-Wiederherstellung von Backups. KI-Clients arbeiten ausschließlich über den MCP-Server mit dem festen Werkzeugkatalog (ab Werk aus, Entscheidung des Projektinhabers vom 31.10.2026, `docs/rules/API.md`, `docs/MCP.md`). Einzelheiten in den eigenen Abschnitten.
- Zurückgestellt bzw. offen: ZFS und weitere Fernzugriffsarten. Let's Encrypt und dynamisches DNS sind umgesetzt (`docs/LETSENCRYPT.md`, `docs/DDNS.md`). **Tailscale, NetBird sowie kurze und optionale längere Monitoring-Verläufe sind umgesetzt**, siehe `docs/TAILSCALE.md`, `docs/NETBIRD.md` und `docs/MONITORING.md`.
- LUKS2-Volume-Verschlüsselung ist seit der Entscheidung des Projektinhabers vom 28.10.2026 als Option umgesetzt (je Volume beim Anlegen, ab Werk aus; siehe Grundsätze und `docs/ENCRYPTION.md`). Die Befehlspalette (Strg/⌘+K) ist seit 0.16.0 umgesetzt: nur Navigation, rollenabhängig, keine Mutationen.
- README: den aktuellen Gesamtumfang nach Funktionsbereichen beschreiben, keine „Neu in Version …“-Abschnitte anhängen. Aktuelle echte WebUI-Screenshots mit isolierten Beispieldaten unter `docs/screenshots/current/` pflegen; Herkunft und Aufnahme in `docs/screenshots/README.md` dokumentieren. Versionshistorie und Prüfberichte bleiben in den Releases.
- Bereits umgesetzte Funktionen nicht erneut als unentworfene Release-Voraussetzung behandeln. Für UI-Arbeiten gelten die aktuellen Entwürfe und Folgeentscheidungen unten.

## Stick-Layout

```
TITANOS (FAT32)
├── EFI/BOOT/BOOTX64.EFI          # systemd-boot (64-Bit-UEFI)
├── EFI/BOOT/BOOTIA32.EFI         # GRUB für 32-Bit-UEFI (ab 0.41)
├── boot/grub/                    # grub.cfg, grubenv, i386-pc/ (Legacy-BIOS), stick-bios.py (ab 0.41)
├── loader/
│   ├── loader.conf
│   └── entries/
│       ├── titanos-a.conf
│       └── titanos-b.conf         # erst nach dem ersten Update; auf Probe: titanos-b+3.conf
├── titanos/
│   ├── a/  vmlinuz, initrd.img, filesystem.squashfs, version, app/, bootenv (Kommandozeile für GRUB)
│   └── b/  (zweiter System-Slot; optional ebenfalls app/)
└── config/                        # persistente Konfiguration, Konten und Geheimnisse
```

Grundsätze (Kurzfassung; vollständiger Wortlaut mit SSD-Cache, Verschlüsselung, Firmware/Treiber-Plugins und RAM-Ziel in `docs/rules/STORAGE.md`):
- Rootfs bei jedem Boot frisch. Persistente Systemkonfiguration nur in `/config` auf dem Stick (atomar per temp-Datei + rename, selten); Nutzdaten, Plugin-Dateien, Betriebsdaten und schwere Daten (Docker, VM-Images, große Logs) auf Daten-Volumes, nie im Overlay. Configs für Samba, NFS, Netzwerk usw. werden beim Start aus `/config` erzeugt.
- Stick per systemd-Automount auf `/efi` ohne Leerlauf-Zeitlimit (`/config` → `/efi/config`), kein Einbinden beim Boot (`auto`/`nofail`). titand liest `/config` beim Start und hält es im RAM: **kein Zugriff auf `/config` in zyklischen Abfragen, keine offenen Dateien dort, keine periodischen Stick-Schreibzugriffe.**
- Volumes werden über UUIDs in `/config/storage/volumes.json` zugeordnet, nie über Gerätenamen wie `sda`. Arrays setzt udev nach dem Start zusammen, nicht das initramfs.
- **Im Entwicklungsmodus (`titand -dev`) verändert titand keine Platten.**
- Split-Lock-Erkennung bleibt aktiv; keine Bootparameter/Sysctls automatisch setzen oder Boot-Einträge anpassen.
- Treiber, die Debians Kernel nicht baut, nur als signierte Treiber-Plugins, nicht im Grundsystem; ein Kernelwechsel bleibt Entscheidung des Projektinhabers. Seit 0.41 wählt man unter Updates zwischen Kernel „Stabil“ (Debian) und „Neu“ (trixie-backports), Entscheidung vom 31.10.2026 (`docs/KERNEL.md`).
- journald im RAM mit Größenbegrenzung; seit 0.41.0 schreibt titand das Journal zusätzlich auf ein Daten-Volume (`.titanos/logs/<boot>.log`, begrenzt), nie auf den Stick (Entscheidung 31.10.2026, `docs/rules/SYSTEM.md`).

## Repo-Struktur (wichtige Einstiegspunkte)

```
titanos/
├── CLAUDE.md               # diese Projektanleitung
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
│       ├── lxc/            # LXC-Systemcontainer über lxc-create/systemctl, cgroup-Messwerte
│       ├── update/, app/   # System-/App-Updates und Rückfall
│       ├── toolcatalog/    # Werkzeugtypen, Argumentprüfung, Handbuch für MCP (Werkzeuge in api/catalog*.go, api/mcp_tools.go)
│       ├── office/         # Word-, Excel-, PDF-Erzeugung und Dokumentlesen
│       ├── files/, backup/, configbackup/, backuptarget/
│       ├── plugins/, hardware/, tailscale/, netbird/, usb/, ups/
│       ├── homeassistant/  # REST, WebSocket-Beobachter, MQTT-Sensoren
│       └── system/, network/ # Systemeinstellungen, Netzwerk, Dienste
├── web/                    # Svelte + TypeScript
├── scripts/
│   ├── make-zip.sh         # squashfs + Kernel + initrd + Bootloader → Zip
│   └── run-qemu.sh         # Test-Boot mit OVMF
├── .github/workflows/      # build.yml, release.yml
└── docs/
    ├── rules/              # verbindliche Fachregeln je Bereich (aus CLAUDE.md ausgelagert)
    ├── design/             # UI-Entwürfe (*.dc.html) + README, optional screenshots/
    └── brand/              # Logo und Schriftzug als SVG + README
```

## Build, Releases und Updates

- `make build` baut Svelte/TypeScript und bettet die statische Oberfläche in das statische Go-Binary ein. Aktuelle Toolchains: Go 1.27 und Node 24 (siehe `go.mod`, `package.json`, CI).
- Reproduzierbare Fedora-44-Entwicklungsumgebung: `docs/DEVELOPMENT.md`, geprüfte Toolchains in `scripts/dev-toolchains.lock.json`. `scripts/fedora-dev.sh` passt für Tests ausschließlich im privaten User-/Mount-/UTS-Namespace den Hostnamen, OVMF-Pfade und SSH-Client-Konfiguration an. `scripts/test-storage-apps-qemu.py` ergänzt den vorhandenen Wegwerf-QEMU-Storage-Test um echte Nextcloud-/Mosquitto-Katalog-Apps; keine Host-Docker- oder Host-Plattenzugriffe. Gemessener Umfang und Grenzen vom 25.10.2026: `docs/validation/development-environment-2026-10-25.json`; keine pauschale Hardware-/Release-Abnahme daraus ableiten.
- Ein volles Release baut Debian mit mkosi, packt squashfs (zstd) und erzeugt mit Kernel, initrd und systemd-boot ein FAT32-Stick-ZIP. App-Releases packen das geprüfte Basis-Abbild mit neuem titand neu; Kernel, initrd, Bootloader und Plugins bleiben die der Basis. Jedes Release enthält weiterhin ein vollständiges ZIP und die drei Slot-Dateien für neue Sticks und ältere NAS.
- `scripts/release-kind.py` entscheidet anhand der Änderungen seit der Basis über App-/Systemrelease; neue Systempakete, Kernel, Dienste oder Build-Dateien erfordern ein volles Release. `[system]` in der Tag-Nachricht erzwingt es. **Die Versionsnummer entscheidet nicht über die Art:** 0.10.0 und 0.11.0 sind App-Releases auf Basis 0.9.0.
- `debian-security.yml` prüft wöchentlich Debian-Sicherheitsupdates und bereitet bei Bedarf einen PR für ein volles Release vor. App-Updates aktualisieren keine Debian-Pakete. Veröffentlichung nach Freigabe des Projektinhabers.
- Release-Ablauf, Lizenzinventare, Quellen und Abnahme: `docs/RELEASING.md`, `scripts/make-release.sh`, `.github/workflows/release.yml`.

### Update-Regeln (Kurzfassung; Wortlaut in `docs/rules/UPDATES.md`, Details `docs/APP-UPDATES.md`)

- Quelle nur GitHub Releases von `ra5on/TitanOS`, **nur Stable**, Manifest direkt ohne GitHub-API. **Ohne gültige Signatur keine Installation, auch nicht mit Warnung oder manuell**; danach Größe und SHA-256 jeder Datei.
- Neue Konfigurationsfelder müssen von Basis und voriger App-Version weiterhin lesbar sein; keine inkompatiblen Umbenennungen/Pflichtfelder. Rückwege einschließlich Plugin-Einstellungen erhalten.
- Volle Systemupdates: A/B-Slots mit drei Probestarts und Boot-Check über `/api/v1/health` (**kein `systemd-bless-boot`**); Bootloader (`EFI/BOOT/BOOTX64.EFI`, `EFI/BOOT/BOOTIA32.EFI`, `boot/grub/`) werden nicht ersetzt.
- App-Updates: nur titand mit Weboberfläche bei passender Basis-Version, Probe und Rückfall ohne NAS-Neustart; Installation wartet auf laufende dauerhafte Aufträge. Plugin-Katalog, NVIDIA-Bauumgebung und Treiber richten sich nach der **Basis-Version**.

### Release-Prüfung und Grenzen

- Test-Builds verwenden Wegwerf-Schlüssel, die nie veröffentlicht werden. Tag-Builds verlangen zwei verschiedene echte öffentliche Schlüssel; nur der geschützte Publish-Job erhält das Release-Secret. ZIP, Manifest und Prüfsummenliste werden signiert.
- QEMU-Smoke-Tests prüfen das tatsächliche ZIP, System-Rückfall sowie App-Updates mit erhaltener Anmeldung, Fehlstart-Rückfall und Rückweg zur Basis. Bei App-Releases wird die bereits geprüfte Basis neu gepackt; nicht alle Storage-/Hardwaretests werden erneut ausgeführt.
- Historische GitHub-Update-/Rollback-Prüfungen und Hardwaremeldungen sind dokumentiert, aber keine pauschale Abnahme jedes neuen Releases. Neue Prüfungen und ihre Grenzen ausdrücklich festhalten.

## Testen

- QEMU mit OVMF (UEFI). Stick-Inhalt als FAT-Laufwerk: entweder `-drive file=fat:rw:<ordner>` (vvfat, Schreibzugriff experimentell) oder ein per mtools erzeugtes FAT-Image.
- Zusätzliche virtuelle Platten für RAID-Tests (Ausfall simulieren, Rebuild prüfen).
- Tests auf echter Hardware gezielt ergänzen. Bestätigte frühere Tests gelten nur für den dokumentierten Umfang, nicht automatisch für neue Funktionen.
- `make test`: Go vet/Tests, Webtests und Svelte-Prüfung. `make test-release`: Paketierungs-/Signaturtests mit echten Werkzeugen. UI-Prüfskripte in `scripts/test-ui-*.mjs`; ausschließlich isolierte Testdaten verwenden. Keine Host-Platten formatieren, keine Host-Sysctls für Tests ändern.

## Harte Grundsätze (Kurzfassung bereichsübergreifender Regeln)

Wortlaut und Einzelheiten in der jeweiligen Regeldatei; bei Widerspruch gilt die ausführlichere Regel dort.

- **Daten und Platten:** Datenverlust vermeiden hat Vorrang. Vor jedem Anlegen/Erweitern/Ersetzen fremde Arrays, LVM/crypt/dm-Halter, Bootstick, konfigurierte Volumes und VM-Platten ausschließen und nach dem Stoppen erneut prüfen. Tests nie auf Host-Platten, nur auf neu erzeugten Images bzw. Wegwerf-QEMU-Datenträgern; keine Host-Sysctls ändern (`docs/rules/STORAGE.md`).
- **Private Betriebsdaten** unter `.titanos/<funktion>` gehören root, sind nicht von anderen beschreibbar oder verlinkt; ein verschwundenes Volume ist nie ein beschreibbarer RAM-/Overlay-Ersatz (`docs/rules/STORAGE.md`).
- **Bestätigungen:** Zerstörende Aktionen mit einfachem Bestätigungsdialog (`Dialog.svelte`, `.dialog-actions`), gefährlicher Knopf links von „Abbrechen“, Enter/Autofokus lösen ihn nie aus. **Nirgends einen Namen oder eine Kennung eintippen lassen**; erwartet die API einen Bestätigungswert, schickt ihn die Oberfläche selbst (`docs/rules/WEB-UI.md`).
- **Konsolen:** Alle Konsolen (NAS-Shell, Docker, VM-Bildschirm, serielle VM-Konsole, LXC) nutzen die bestehende Anmeldung ohne zweite Passwort-/2FA-Abfrage; keine erneute Bestätigung einführen. 15 Minuten Inaktivitätsabbruch, Protokollierung und Abschaltbarkeit bleiben (`docs/rules/ACCOUNTS-ACCESS.md`).
- **Rechte:** Rollen Besitzer/Administrator/Benutzer, Prüfung immer serverseitig gegen aktuelle Konten-/Share-Rechte. root ist gesperrt, SSH ab Werk aus. API-Tokens nur über die explizite Routenliste `publicRoutes`, MCP zusätzlich über den festen Werkzeugkatalog (`toolcatalog`, `api/catalog*.go`) mit dem Konto des Tokens; nie Geheimnisse, Passwörter/2FA/Passkeys, Tokenverwaltung, Shells/Konsolen oder Netzwerkbestätigung (`docs/rules/ACCOUNTS-ACCESS.md`, `docs/rules/API.md`).
- **Geheimnisse:** Passwörter (z. B. Volume-Passwörter) nie auf der Kommandozeile, nie protokolliert, nie im MCP-/Token-Katalog; Cluster-Geheimnisse nicht im Diagnosepaket.
- **Aufträge:** Das Backend besitzt Aufträge und führt Änderungen aus; Browserergebnisse nie als Ausführungsnachweis. RAM-Aufträge und offene Verbindungen nicht als dauerhaft behandeln (`docs/rules/STORAGE.md`, `docs/rules/UPDATES.md`).
- **Ab Werk aus:** Firewall, SSH, Verschlüsselung, SSD-Cache, MCP-Server, Wartung, längere Monitoring-Verläufe und andere optionale Funktionen laut Regeldatei.
- **Plugins:** Vor der Installation auf volle Systemrechte hinweisen; keine Plugin-Sandbox behaupten (`docs/rules/PLUGINS.md`).
- **Oberfläche:** Status nie nur über Farbe; Kontrast mindestens 4,5:1; Farben aus Theme-Variablen; alle Texte über Übersetzungsdateien (Deutsch/Englisch). Reale Messwerte, Beispieldaten nur in isolierten Tests.
- **Prüfstände:** Getestetes immer mit Umfang und Grenzen dokumentieren; frühere Hardwaremeldungen sind keine pauschale Abnahme neuer Funktionen.

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

## Fachregeln

Verbindliche Detailregeln je Bereich. Vor Arbeiten an einem Bereich die Datei lesen; die genannten Fachdokumente in `docs/` beschreiben Bedienung, Grenzen und Prüfungen.

| Regeldatei | Lesen vor Arbeiten an … |
|---|---|
| `docs/rules/STORAGE.md` | Stick/`/config`, Volumes, mdadm/Btrfs, SSD-Cache, Verschlüsselung, Firmware/Treiber, Ruhemodus, RAM-/Upload-Budget, Übernahme/Erweiterung, Wartung, Auftragsabläufe, Anwendungssicherungen, Restore-Proben, VM-Klon, Speicherverbrauch |
| `docs/rules/UPDATES.md` | Update-Quelle, Signaturen, Update-Einstellungen, A/B-Systemupdates, App-Updates, Boot-/App-Check |
| `docs/rules/WEB-UI.md` | Kopfzeile, Konto-Menü, Bestätigungsdialoge, Marke, Einstellungsseiten mit Seitenleiste, Dashboard, Themes/Farben/Glas, gemeinsame UI, mobile Navigation, Befehlspalette |
| `docs/rules/ACCOUNTS-ACCESS.md` | Konten/Rollen, 2FA, Passkeys, Sitzungen/„Angemeldet bleiben“, Konten sperren, SSH/Zertifikate (Zugang), Terminal und Konsolen, Aktivitätsprotokoll |
| `docs/rules/SYSTEM.md` | Energie, Zeitplan/Wake-on-LAN, USV, Werkzeuge-Reiter, Benachrichtigungen, Monitoring/CPU-Leistung, Ein-/Ausschalter, Tailscale, NetBird |
| `docs/rules/NETWORK.md` | Netzwerkeinstellungen mit Rückfall, Firewall |
| `docs/rules/BACKUPS.md` | Datensicherung, Sicherung des Sticks, USB-Kopie, SMB-Kopie, Meldungstöne |
| `docs/rules/SHARES-FILES.md` | Shares, Freigabe samt Daten löschen, Kontingente, Dateimanager, Datei-Editor, USB-Uploads, Namensregeln, Vorschau/Suche, Link-Downloads |
| `docs/rules/CLUSTER.md` | Cluster (Beitritt, Proxy, Kontenabgleich, Backups) |
| `docs/rules/API.md` | Öffentliche API/Tokens, titanosctl, MCP-Server und Werkzeugkatalog, VM-Medien-Import, Plugin-Aufräumen |
| `docs/rules/DOCKER.md` | Docker-Übersicht, Container bearbeiten, App-Store/Katalog, Docker-Layout |
| `docs/rules/VMS-LXC.md` | VMs (Erstellen, Firmware, Konsole, Snapshots), App-Vorlagen, Hardware-Durchreichung, LXC-Systemcontainer |
| `docs/rules/PLUGINS.md` | Plugins, Community-Plugins, NVIDIA-Treiberbau |
| `docs/rules/HOME-ASSISTANT.md` | Home-Assistant-Anbindung |

Weitere maßgebliche Dokumente: `docs/design/README.md` (UI-Entwürfe), `docs/RELEASING.md` (Release-Ablauf), `docs/WORKFLOWS.md` (Speicher-/Wartungsabläufe), sowie die Fachdokumente `docs/<BEREICH>.md`, auf die die Regeldateien verweisen.
