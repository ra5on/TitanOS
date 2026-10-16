# TitanOS

Ein schlankes NAS-Betriebssystem, das vom USB-Stick bootet und komplett im RAM läuft – inspiriert von Unraid, aber mit klassischem RAID. Wird auf GitHub veröffentlicht.

Neues, eigenständiges Projekt. Keinen Code, keine Struktur und keine Annahmen aus anderen Projekten übernehmen.

## Festgelegte Entscheidungen

| Bereich | Entscheidung |
|---|---|
| Basis | Debian 13 (trixie) minimal, amd64 |
| Boot | nur UEFI, systemd-boot, Secure Boot aus |
| Stick | FAT32, Label `TITANOS` |
| Systemabbild | squashfs (zstd), wird beim Boot **komprimiert** in den RAM (tmpfs) kopiert, darüber overlayfs mit größenbegrenzter tmpfs-Schicht (256–512 MB) |
| Nach dem Boot | Stick wird nur noch für Konfigurationsänderungen und Updates beschrieben |
| Updates | zwei Systemdateien (A/B) auf dem Stick, Rollback = alte Datei booten |
| Auslieferung | Zip zum Kopieren auf einen FAT32-Stick. Kein Pflicht-`.img` (optional später im CI) |
| Speicher | klassisches RAID per **mdadm** (0, 1, 5, 6, 10) mit ext4/XFS (umgesetzt); Btrfs optional für native Zwei-Platten-Spiegel mit Snapshots (Entwicklungsstand nach beta.2, siehe docs/BTRFS.md); ZFS ggf. später |
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

## Noch nicht entworfen, aber Pflicht vor dem ersten Release

Für diese Punkte gibt es noch keine Entwurfsseite. Vor der Umsetzung kurz mit dem Projektinhaber abstimmen und einen Entwurf vorschlagen.

1. **Backup des Sticks:** automatisches Backup von `/config` auf ein Volume (optional extern) und geführter Ablauf „Stick ersetzen“ (neuen Stick flashen, Konfiguration zurückspielen). Umgesetzt, siehe Web-UI › Sicherung des Sticks.
2. **Ersteinrichtung:** Assistent beim ersten Start – Sprache, Kontoname und Passwort (Besitzer), Hostname, Netzwerk, Zeitzone, erstes Volume. Umgesetzt als `/welcome` nach dem Anlegen des Besitzerkontos: Sprache, Name des NAS, Zeitzone (Vorschlag aus dem Browser), Blick aufs Netzwerk mit Verweis auf die Einstellungen, danach eine Liste der nächsten Schritte (Volume, Freigabe, Zwei-Faktor, Apps). Das erste Volume wird nicht im Assistenten selbst angelegt, sondern über die Speicher-Seite.
3. **Netzwerk:** feste IP, Bridge `br0` (für VMs/Container mit eigener IP), VLANs, Bonding, DNS. Änderungen mit Sicherheitsfrist: ohne Bestätigung innerhalb von 2 Minuten automatisch zurück. Umgesetzt, siehe Web-UI › Netzwerk.
4. **Shares und Nutzer:** Seiten für SMB/NFS-Freigaben, Rechte, Papierkorb, Time-Machine-Unterstützung; Kontoverwaltung mit Rollen. Konzept für Shares abgestimmt, siehe Web-UI › Shares.
5. **Platten vorbereiten:** Belastungstest neuer Platten vor dem Einbau ins RAID (SMART-Langtest + Schreib-/Lesetest). Umgesetzt, siehe Web-UI › Werkzeuge.
6. **Benachrichtigungen:** E-Mail und Push (z. B. ntfy, Telegram, Discord) bei Plattenfehlern, Rebuild, vollen Volumes, Updates, Backup-Fehlern. Umgesetzt für Volumes und Platten, siehe Web-UI › Benachrichtigungen. Offen: Meldungen zu Updates und Backups (kommen mit diesen Funktionen).
7. **Sicherheit:** 2FA (TOTP), Sperre nach Fehlversuchen, selbst signiertes Zertifikat oder Import eines eigenen Zertifikats, SSH standardmäßig aus. Umgesetzt: 2FA und die Sperre nach Fehlversuchen (siehe Web-UI › Konten und Rechte), SSH, selbst signiertes Zertifikat und Zertifikatsimport (siehe Web-UI › Zugang). Let's Encrypt ist vorerst zurückgestellt.
8. **Sprachen:** Deutsch und Englisch von Anfang an, alle Texte über Übersetzungsdateien.
9. **Diagnose:** Log-Ansicht und „Diagnosepaket erstellen“ (Passwörter, Schlüssel und IPs entfernt) für GitHub-Issues. Umgesetzt, siehe Web-UI › Werkzeuge.
10. **Dateimanager:** Dateien im Browser auf Shares hochladen (auch ganze Ordner, per Drag-and-drop, große Dateien fortsetzbar), herunterladen, verschieben, umbenennen, löschen; mit den Rechten des angemeldeten Kontos. Konzept abgestimmt, siehe Web-UI › Dateimanager.

Später: Verlaufsgrafiken (CPU, Temperaturen, Netzwerk, Platten), Volume-Verschlüsselung (LUKS), eingebauter Fernzugriff (WireGuard/Tailscale/Cloudflare Tunnel), Energie-Zeitplan und Wake-on-LAN, Befehlspalette (Strg+K), dokumentierte API und CLI.

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
│   ├── a/  vmlinuz, initrd.img, filesystem.squashfs, version
│   └── b/  (Update-Ziel)
└── config/                        # einzige persistente Daten: Volumes, Shares, Nutzer, Apps, Netzwerk
```

Grundsätze:
- Das Rootfs ist bei jedem Boot frisch. Alles Persistente liegt in `/config` auf dem Stick. Configs für Samba, NFS, Netzwerk usw. werden beim Start aus `/config` erzeugt.
- Schreibzugriffe auf `/config` erfolgen atomar (temp-Datei + rename) und selten.
- Der Stick wird nur bei Bedarf eingebunden (systemd-Automount auf `/efi`, `/config` ist ein Link auf `/efi/config`) und nach 30 Sekunden ohne Zugriff wieder ausgehängt. titand liest `/config` deshalb beim Start und hält es im Speicher; kein Zugriff auf `/config` in zyklischen Abfragen, keine offenen Dateien oder Verzeichnisse dort.
- Schwere Daten (Docker, VM-Images, große Logs) liegen nie im Overlay, sondern auf einem Volume.
- Volumes: ein mdadm-Array mit einem Dateisystem (ext4 oder XFS), eingebunden unter `/mnt/<name>`. Der Name hat höchstens 12 Zeichen (`[a-z][a-z0-9-]*`), weil er auch Array-Name und Dateisystem-Label ist. Welche Arrays TitanOS gehören, steht in `/config/storage/volumes.json` (Array-UUID, Dateisystem-UUID); Gerätenamen wie `sda` ändern sich zwischen Neustarts und werden nie gespeichert.
- Jede Mitgliedsplatte bekommt eine GPT mit einer RAID-Partition, die 128 MiB der Platte frei lässt, damit eine etwas kleinere Ersatzplatte noch passt.
- Arrays werden nach dem Start von udev zusammengesetzt, nicht im initramfs: mdadm ist dort bewusst nicht enthalten. titand bindet die Volumes ein, sobald ihr Array läuft, auch mit fehlender Platte.
- Im Entwicklungsmodus (`titand -dev`) verändert titand keine Platten.
- journald im RAM mit Größenbegrenzung.
- Firmware: gezielte Debian-Pakete aus `non-free-firmware` für Realtek/Broadcom-Netzwerk, QLogic und weitere Adapter sowie Intel-/AMD-Grafik; kein Sammel-Metapaket. Intel-GPU-Firmware (GuC/HuC) und Intel-NPU-Firmware sind Pflicht und werden vor dem Packen geprüft. Geräteunterstützung zusätzlich auf echter Hardware testen.
- RAM-Ziel: komprimiertes Systemabbild ca. 0,5–0,8 GB; Grundsystem mit allen Diensten (Kernel, titand, Samba/NFS, Docker-Engine, libvirt) ca. 1,0–1,5 GB, ohne Container und VMs. Plugins liegen auf einem Volume und belegen keinen festen RAM.

## Repo-Struktur (Vorschlag)

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
│       └── system/         # Updates A/B, Netzwerk, Dienste
├── web/                    # Svelte + TypeScript
├── scripts/
│   ├── make-zip.sh         # squashfs + Kernel + initrd + Bootloader → Zip
│   └── run-qemu.sh         # Test-Boot mit OVMF
├── .github/workflows/      # build.yml, release.yml
└── docs/
    ├── design/             # UI-Entwürfe (*.dc.html) + README, optional screenshots/
    └── brand/              # Logo und Schriftzug als SVG + README
```

## Build-Pipeline

```
mkosi baut Debian-Rootfs (inkl. titand)
→ filesystem.squashfs (zstd)
+ vmlinuz, initrd.img, systemd-boot
→ titanos-x.y.z.zip  + SHA256 + Signatur (minisign)
```

GitHub Actions baut bei jedem Tag und lädt das Zip als Release hoch.

## Updates über GitHub

- Quelle der Updates sind die GitHub Releases von `ra5on/TitanOS`. Kein eigener Server nötig.
- Jedes Release enthält neben dem Zip ein `manifest.json` (Version, Kanal, Kernelversion, Dateien mit SHA-256, Größe, Mindestversion, Changelog-Text) und dessen Signatur `manifest.json.minisig`.
- TitanOS lädt `https://github.com/ra5on/TitanOS/releases/latest/download/manifest.json` (Stable) bzw. für Beta das neueste Pre-Release. Direkter Asset-Download statt API, damit das API-Limit für Anfragen ohne Anmeldung (60 pro Stunde je IP) keine Rolle spielt.
- Prüfung: Signatur des Manifests mit dem öffentlichen Schlüssel, der fest im Abbild steckt; danach SHA-256 jeder Datei. Ohne gültige Signatur keine Installation – auch nicht manuell ohne ausdrückliche Warnung.
- Ablauf: neues Abbild in den inaktiven Slot (`titanos/b/`) auf dem Stick schreiben → Boot-Eintrag mit Zähler (systemd-boot Boot Counting, z. B. 3 Versuche) → Neustart → Health-Check (titand erreichbar, Volumes gemountet) → `systemd-bless-boot` markiert den Slot als gut. Scheitert der Start dreimal, bootet automatisch wieder der alte Slot.
- Plugins: Für jedes Release baut die CI passende Plugin-Images (gleiche Kernelversion) und legt sie als Assets bzw. in einem eigenen Plugin-Repo ab. Ein System-Update lädt die passenden Plugins mit, bevor es den Slot umschaltet.
- App-Katalog: eigenes Repo (z. B. `ra5on/TitanOS-apps`), TitanOS liest es als Quelle.
- UI: Kanal Stable/Beta, Changelog vor dem Update, „Jetzt aktualisieren“ oder Zeitplan, Anzeige des aktiven und des Rückfall-Slots, „Auf vorherige Version zurück“.
- Signierschlüssel: privater Schlüssel nur als Secret in einem geschützten GitHub-Environment mit manueller Freigabe (oder offline signieren). Verlust oder Diebstahl des Schlüssels ist der schwerste Sicherheitsfall des Projekts; Schlüsselwechsel muss per Update vorbereitet sein (zweiter öffentlicher Schlüssel im Abbild).

### Stand der Umsetzung (Paket `update`, Seite Einstellungen › Updates)

- **Release-Dateien** (`scripts/make-release.sh`, Workflow `release.yml`, Anleitung `docs/RELEASING.md`): das Zip für einen neuen Stick, die drei Dateien eines Slots einzeln (`titanos-<version>-vmlinuz`, `-initrd.img`, `-filesystem.squashfs`), `manifest.json` mit Signatur und eine Liste der Debian-Quellpakete mit Links auf snapshot.debian.org. Ein NAS lädt die drei Slot-Dateien direkt in den freien Slot auf dem Stick – ohne Zwischenablage im RAM oder auf einem Volume, mit Prüfung von Größe und SHA-256 und Wiederaufnahme nach einem Abbruch (HTTP-Range).
- **Manifest:** `version`, `tag`, `channel` (stable/beta), `kernel`, `released`, `minVersion`, `changelog`, `cmdline` (Kernel-Kommandozeile des Boot-Eintrags mit `@SLOT@`) und `files`. Es wird nur angenommen, wenn es von einem Schlüssel aus `/usr/share/titanos/update-keys/*.pub` signiert ist (Paket `minisign`, gemeinsam mit den Plugins). Im Abbild liegen `titanos-release-1.pub` (ID `8EAABC2A42904C1D`) und `titanos-recovery-1.pub` (ID `E58A91B20B598ABB`). Der verschlüsselte private Release-Schlüssel und seine Passphrase sind als Secrets im geschützten GitHub-Environment `release` hinterlegt; der private Reserve-Schlüssel bleibt bei seinem Besitzer. Der Projektinhaber hat am 14. Oktober 2026 die externe Sicherung von Schlüsseln und Passphrasen bestätigt. Offline-Verwahrung des Reserve-Schlüssels ist noch nicht bestätigt. Ein Installieren ohne Signatur gibt es nicht, auch nicht von Hand.
- Stable: `releases/latest/download/manifest.json`. Beta: neuester Eintrag aus `releases.atom`, dann dessen Manifest – ohne die GitHub-API.
- **Boot-Einträge:** `loader/loader.conf` hat kein `default` mehr. systemd-boot startet den ersten Eintrag in seiner Reihenfolge, der noch Versuche hat; mit festem `default` würde er einen Eintrag auch dann starten, wenn er ihn aufgegeben hat. Die Reihenfolge bestimmt der `sort-key`: `titanos-1` für den Slot, der starten soll, `titanos-2` für den Rückfall. Ein neuer Slot bekommt den Eintrag `titanos-<slot>+3.conf` (drei Versuche).
- **Bestätigung:** Die Einheit `titanos-boot-check.service` ruft `titand -check-boot` auf. Das tut nichts, wenn der Eintrag des laufenden Slots keinen Zähler trägt. Sonst wartet es bis zu fünf Minuten darauf, dass titand „bereit“ meldet (`/api/v1/health`: Konfiguration gelesen, Volumes versorgt und – nach einem Update – jedes Volume wieder eingebunden, das vor dem Update eingebunden war), und benennt den Eintrag dann in `titanos-<slot>.conf` um. Schlägt es fehl, startet die Einheit das NAS neu (`FailureAction=reboot`); nach dem dritten Versuch startet der Loader wieder den alten Slot. TitanOS macht das selbst und braucht `systemd-bless-boot` nicht (das Debian-Paket dazu brächte Dienste mit, die bei jedem Start auf den Stick schreiben).
- Was mit dem nächsten Neustart ansteht, steht in `/config/system/update-pending.json` (mit der Boot-ID, damit ein Neustart von titand nichts entscheidet). Nach dem Neustart meldet titand „installiert“, „fehlgeschlagen“ oder „zurückgewechselt“.
- **Zurück auf die vorherige Version:** tauscht die `sort-key` der beiden Einträge; auch das läuft auf Probe. Vor dem Neustart lässt sich jeder Wechsel zurücknehmen („Doch nicht“).
- Einstellungen in `/config/system/update.json`: Kanal, „von selbst installieren“ mit Uhrzeit. TitanOS sucht einmal am Tag. Das Feld `source` (nicht in der Oberfläche) zeigt auf eine andere Adresse als GitHub, für Spiegel und Tests; die Signatur wird trotzdem geprüft.
- Getestet in QEMU mit einem nachgebauten Release-Server und einem Wegwerf-Schlüssel: Update 1.0.0 → 1.1.0 mit abgebrochenem und fortgesetztem Download, Start aus Slot B, Bestätigung nach rund 20 Sekunden, zurück auf 1.0.0; ein Release mit fremder Signatur wird abgelehnt; ein Release, dessen System nie hochkommt, wird nach drei Fehlstarts aufgegeben, das NAS startet wieder das alte System und meldet den Fehlschlag; ein Release, in dem nur titand nicht startet, startet sich dreimal selbst neu und ist nach 17 Minuten ohne jedes Zutun wieder beim alten System.
- Release-Prüfung: PRs mit Änderungen an Build-/Release-Dateien und manuelle Workflow-Läufe bauen mit einem Wegwerf-Schlüssel, prüfen Signaturen und booten das tatsächliche ZIP zweimal in einer isolierten QEMU-VM. Tag-Builds verlangen zwei verschiedene echte öffentliche Schlüssel (Release und Offline-Reserve); erst der geschützte Publish-Job erhält das Release-Secret. ZIP, Manifest und vollständige Prüfsummenliste werden signiert. Schlüssel und Release-Secrets sind eingerichtet. Die externe Sicherung von Schlüsseln und Passphrasen ist bestätigt. `v0.1.0-beta.1`, `v0.1.0-beta.2` und `v0.1.0-beta.3` sind veröffentlicht. Am 15. Oktober 2026 wurden alle 25 öffentlichen Beta-3-Assets auf Signaturen/Prüfsummen geprüft; der echte GitHub-Updatepfad Beta 2 → Beta 3 (Slot B) und der Rückwechsel zu Beta 2 (Slot A) liefen in QEMU erfolgreich mit erhaltenem Besitzerkonto und Zertifikat. Der signierte Coral-Download auf ein Ein-Platten-RAID0 sowie Installation/Deaktivierung/Entfernen bei unverändert laufender libvirt-Test-VM wurden ebenfalls geprüft. Am 14. Oktober 2026 wurden alle 17 öffentlichen Beta-2-Dateien geprüft und in einer frischen QEMU-VM das echte GitHub-Update Beta 1 → Beta 2 (Slot B) sowie der Rückwechsel zu Beta 1 (Slot A) einschließlich Startbestätigung, Besitzerkonto und Zertifikat erfolgreich getestet. Noch nötig: Offline-Verwahrung der Reserve und Tests mit echtem Stick und echter Hardware. Beide Betas wurden auf Wunsch des Projektinhabers mit ausdrücklich dokumentierten Hardware-Testgrenzen veröffentlicht. Testschlüssel werden nie veröffentlicht.
- Noch offen: Der Boot-Loader selbst (`EFI/BOOT/BOOTX64.EFI`) wird von Updates nicht ersetzt. Der Entwicklungsstand lädt passende offizielle Plugins vor dem Slot-Wechsel mit und bewahrt Dateien/Einstellungen je Release; beta.2 kann das noch nicht. Ein Update aus einer Datei von Hand gibt es nicht.

## Testen

- QEMU mit OVMF (UEFI). Stick-Inhalt als FAT-Laufwerk: entweder `-drive file=fat:rw:<ordner>` (vvfat, Schreibzugriff experimentell) oder ein per mtools erzeugtes FAT-Image.
- Zusätzliche virtuelle Platten für RAID-Tests (Ausfall simulieren, Rebuild prüfen).
- Später Test auf echter Hardware mit echtem Stick.

## Web-UI

Bedienkonzept angelehnt an Unraid, Gestaltung eigenständig.

**Entwürfe:** `docs/design/` enthält die Quelltexte aller Entwurfsseiten (`*.dc.html`, Übersicht in `docs/design/README.md`). Vor der Arbeit an einer Seite die passende Datei lesen und Aufbau, Abstände, Farben (siehe `Appearance.dc.html` für alle Theme-Werte) und Verhalten übernehmen – nicht den Code, der nutzt ein Entwurfsformat mit eigener Laufzeit. Beispieldaten darin nie ins Produkt übernehmen. Screenshots, falls vorhanden, in `docs/design/screenshots/`.

**Grundaufbau**
- Kopfzeile: Logo (Symbol + Schriftzug, siehe „Marke“), Reiter-Navigation (Dashboard, Speicher, Shares, Dateien, Nutzer, Docker, VMs, Apps, Plugins, Einstellungen, Werkzeuge), Terminal, Meldungen, Konto-Menü. Kein Hostname/IP-Badge neben dem Logo.
- **Konto-Menü** (Klick auf den eigenen Namen oben rechts, mit Initiale in Akzentfarbe): Name und Rolle, Mein Konto (Passwort, 2FA, SSH-Schlüssel), Darstellung, Angemeldete Geräte, NAS neu starten …, NAS herunterfahren …, Abmelden. Neustart/Herunterfahren nur für Besitzer/Administratoren und immer mit Bestätigungsdialog, der zeigt, was passiert: laufende Container/VMs werden sauber beendet, laufender Rebuild/Prüfung wird unterbrochen und fortgesetzt, Option „erst nach Abschluss des Wiederaufbaus“, Hinweis auf Wake-on-LAN beim Herunterfahren.
- Kopfzeile immer einzeilig mit fester Höhe (ca. 60 px). Reicht die Breite nicht, scrollt die Navigation seitlich mit sichtbarer Scrollleiste, Links-/Rechtspfeilen und Ziehen mit gedrückter Maustaste. Normale Link-Klicks, Tastatur und Touch bleiben bedienbar.
- Statusleiste unten: Volume-Zustand, laufende Rebuilds/Prüfungen, Version und System-Slot.
- Schrift: IBM Plex Sans für Text, IBM Plex Mono für Werte, Pfade, Gerätenamen (lokal ausliefern, keine Google-Fonts-Abhängigkeit im Produkt).
- Mobil: Navigation seitlich scrollen, Kacheln stapeln, breite Tabellen horizontal scrollbar.

**Marke** (Entwurf `Brand.dc.html`, Dateien in `docs/brand/`)
- Symbol: Sechseck-Rahmen mit „T“ (mit Fuge zwischen Balken und Stamm). Schriftzug „TITANOS“ aus eigenen geometrischen Formen (keine Schriftart, keine Lizenzfrage): gleiche Strichstärke, „A“ ohne Querstrich, „T“ mit Fuge.
- Rahmen und „OS“ immer in `--accent`, Buchstaben in `--text`. Im hellen Theme Akzent abdunkeln (Kontrast ≥ 3:1).
- Als Svelte-Komponente mit CSS-Variablen umsetzen. Favicon: Akzentquadrat mit „T“.

Die Einstellungsübersicht zeigt kompakte Kacheln ohne Beschreibung, alphabetisch nach dem übersetzten Titel sortiert. Meldungen können einzeln oder als angezeigte Liste gelöscht werden (nur Besitzer/Administratoren, für alle Sitzungen; neue Meldungen währenddessen bleiben erhalten).

**Konten und Rechte**
- Ersteinrichtung legt das erste Konto mit **frei gewähltem Namen** an (z. B. „alex“). Dieses Konto ist **Besitzer** mit vollen Rechten – praktisch wie root – und sein Name steht oben rechts. Kein festes „admin“.
- Der Besitzer ist **nicht** der Linux-Benutzer root: root ist gesperrt (kein Passwort-Login), SSH ist standardmäßig aus. Wird SSH aktiviert, meldet man sich mit dem eigenen Namen und SSH-Schlüssel an und nutzt `sudo`. Gründe: kein erratbarer Login-Name, jede Aktion im Protokoll einer Person zuordenbar.
- titand läuft als Systemdienst mit den nötigen Rechten und führt Aktionen im Namen des angemeldeten Kontos aus. Die NAS-Shell im Terminal ist eine root-Shell für angemeldete Besitzer/Administratoren; auf ausdrücklichen Wunsch des Projektinhabers ohne zweite Passwort-/2FA-Abfrage. Gastkonsolen und andere sensible Aktionen behalten ihre erneute Bestätigung.
- Rollen: **Besitzer** (alles, mindestens einer muss existieren), **Administrator** (alles außer Besitzer verwalten), **Benutzer** (nur eigene Shares/Dateien, kein Docker/VMs/System). Kontoname wird auch als SMB-Benutzer angelegt.
- Konten, Passwort-Hashes (argon2id) und 2FA-Geheimnisse in `/config/users/`. Anmeldeschutz gegen Durchprobieren.
- Umgesetzt: Zwei-Faktor-Anmeldung mit TOTP (RFC 6238, sechs Stellen, 30 Sekunden, ±1 Schritt, jeder Code nur einmal) und acht Wiederherstellungscodes, von denen nur Hashes gespeichert werden. Das Geheimnis gilt erst, wenn ein Code daraus geprüft wurde. Der Code wird beim Anmelden nach dem richtigen Passwort verlangt, außerdem bei der erneuten Bestätigung vor Gastkonsolen und zum Ausschalten; die NAS-Shell verwendet die bestehende Administrator-Anmeldung. Administratoren können den zweiten Faktor eines anderen Kontos zurücksetzen (alle seine Anmeldungen enden dabei).
- Umgesetzt: Seite „Mein Konto“ (`/account`) mit Passwort, Zwei-Faktor-Anmeldung und „Angemeldete Geräte“ (Anmeldungen liegen nur im Speicher von titand; einzelne lassen sich beenden).
- Umgesetzt: SSH-Schlüssel je Konto unter „Mein Konto“ (`sshKeys` in `account.json`, eine Zeile je Schlüssel, Optionen vor dem Schlüssel werden abgelehnt), SSH-Zugang und eigenes Zertifikat, siehe „Zugang“.

**Dashboard mit frei anpassbaren Kacheln**
- Raster aus 12 Spalten mit festen Zeilenhöhen. Kacheln rasten auf Spalten/Zeilen ein, keine freien Pixelgrößen.
- Bearbeiten-Modus (Schalter auf dem Dashboard): Kacheln am Griff verschieben, an der Ecke Größe ändern, ausblenden, über „Kachel hinzufügen“ zurückholen, „Standard wiederherstellen“. Außerhalb des Modus ist alles fixiert.
- Layout pro Nutzer in `/config` gespeichert. Auf schmalen Bildschirmen wird automatisch gestapelt.
- Umsetzung mit gridstack.js. Erkannte iGPU/GPU-, NPU- und NVIDIA-Kacheln werden bei Layouts vor Version 2 ergänzt; danach bleibt die bewusste Auswahl erhalten. Im Bearbeiten-Modus lassen sich mehrere einzelne VMs und Docker-Container mit CPU/RAM und Zustand hinzufügen; Auswahl nach VM-/Containername, kein Ersatz durch ein anderes Objekt bei Löschung. Fehlende Messwerte werden nicht erfunden.

**Themes und Farben (pro Nutzer wählbar, Seite Einstellungen › Darstellung)**
- Themes: Graphit (Standard), Schwarz (Tiefschwarz, hoher Kontrast), Mitternacht (Nachtblau), Hell, Automatisch (folgt `prefers-color-scheme`: Hell bzw. Graphit), **Eigenes** (Farbwähler für Hintergrund, Flächen, Kopfzeile, Text, Linien; gedämpfter Text und Chips werden abgeleitet; Kontrastwarnung unter 4,5:1).
- Akzentfarben: Blau, Grün, Türkis, Violett, Kupfer, Rosé, plus **eigene Farbe** per Farbwähler. Textfarbe auf Akzentflächen automatisch dunkel oder weiß, je nach Kontrast.
- **Stil der Flächen:** Flach (deckend), **Glas** (durchscheinend wie „Liquid Glass“: `backdrop-filter: blur() saturate()`, halbtransparente Fläche, helle Innenkante oben), Kontur (nur Linien).
  - Glas-Regler: Transparenz (0–90 %), Unschärfe (0–40 px), Glanzkante (0–60 %). Transparenz wird automatisch begrenzt, damit Text lesbar bleibt (dunkle Themes Deckkraft mindestens 35 %, helle mindestens 50 %). Fallback ohne `backdrop-filter`: deckende Fläche. `prefers-reduced-transparency` respektieren.
- **Ecken:** Regler für Kacheln/Fenster (0–32 px) und für Knöpfe/Felder/Menüs (0–22 px) – gilt überall.
- Schriftgröße 90–120 %, Dichte Normal/Kompakt.
- **Hintergrund:** Einfarbig, Formen (weiche Farbflächen, eine davon in Akzentfarbe), eigenes Bild (Upload, in `/config` gespeichert, Regler „Abdunkeln“). Glas wirkt nur mit Hintergrund – Hinweis anzeigen, wenn Glas + Einfarbig.
- Theme als JSON exportieren/importieren. „Standard wiederherstellen“.
- Technisch: alle Werte als CSS-Variablen (`--bg`, `--surface`, `--header`, `--border`, `--text`, `--muted`, `--track`, `--chip`, `--accent`, `--on-accent`, `--surface-alpha`, `--glass-blur`, `--glass-shine`, `--radius-tile`, `--radius-ctl`, `--font-scale`), Theme als `data-theme`, Stil als `data-surface="flat|glass|outline"` auf `<html>`. Kein hart codierter Farbwert oder Radius in Komponenten.
- Statusfarben sind in allen Themes gleich: OK grün, Warnung gelb, Fehler rot, inaktiv grau. Status nie nur über Farbe, immer mit Text.
- Kontrast mindestens 4,5:1 für Text in jedem Theme.

**Energie** (Einstellungen › Energie; Entwicklungsstand nach beta.3)
- Liest die tatsächlich verfügbaren CPU-Governors, Energy Performance Preferences und ACPI-Plattformprofile aus sysfs. Bei mehreren CPU-Gruppen werden nur gemeinsam unterstützte Werte angeboten; keine künstlichen Profile oder festen Taktgrenzen. CPU-Governor `userspace` wird ohne manuelle Taktsteuerung nicht angeboten.
- Nur Besitzer/Administratoren dürfen Profile ändern. Auswahl in `/config/system/energy.json`, beim Start erneut angewendet; Status liest nur sysfs und RAM. Der Entwicklungsmodus ist ausschließlich lesend. Bei Schreib- oder Speicherfehlern werden vorherige Werte wiederhergestellt; Fehler werden angezeigt.
- Die Wahl „Nicht beim Start setzen“ beendet die Verwaltung dieses Werts; der aktuelle Wert bleibt bis zu einer anderen Änderung oder zum Neustart erhalten. Governor und EPP hängen vom CPU-Treiber ab: `intel_pstate` erlaubt bei `performance` keine stromsparende EPP.
- Schnittstellen: [CPUFreq](https://docs.kernel.org/admin-guide/pm/cpufreq.html), [Intel P-state](https://docs.kernel.org/admin-guide/pm/intel_pstate.html), [Plattformprofile](https://docs.kernel.org/userspace-api/sysfs-platform_profile.html). Tests nutzen künstliche sysfs-Dateien; die Wirkung auf echte NAS-Hardware bleibt zu prüfen.

**Festplatten-Ruhemodus** (Speicher › Ruhemodus für Festplatten, nur Besitzer und Administratoren; Entwicklungsstand nach `v0.1.0-beta.1`)
- Eine gemeinsame Leerlaufzeit für alle unterstützten HDDs: Aus (Standard), 5, 10, 15 oder 30 Minuten sowie 1, 2 oder 3 Stunden. Jede Platte wird getrennt beobachtet und wacht beim nächsten Zugriff wieder auf. Aus beendet die automatische Steuerung, weckt bereits schlafende Platten aber nicht auf.
- Einstellung in `/config/storage/disk-sleep.json`; nur die Dauer wird gespeichert, niemals Gerätenamen. Der Zustand und die Zeitmessung liegen im RAM. Änderungen werden atomar gespeichert; nach Neustart, geänderter Einstellung oder einer Beobachtungslücke beginnt eine volle neue Ruhezeit.
- `storage.RunDiskSleep` liest alle zehn Sekunden die I/O-Zähler aus `/sys/block/*/stat`. Lese-, Schreib-, Discard- und Flush-Vorgänge sowie laufende I/O setzen die Zeit zurück. Keine zyklischen Zugriffe auf `/config`, keine Plattendaten-Abfragen zur Zeitmessung. Die vorhandenen SMART-Abfragen nutzen `--nocheck=standby`.
- Unterstützt sind als nicht wechselbar gemeldete SATA-/SAS-/SCSI-HDDs und USB-HDDs mit passendem Adapter. SSDs, NVMe, Bootstick, Virtio- oder netzgebundene Platten, an VMs vergebene Platten sowie Platten mit laufendem Test oder RAID-Wartung bleiben ausgenommen. Apps, Freigaben und andere Zugriffe können das Einschlafen verhindern.
- Das Debian-Paket `hd-idle` erhält ausschließlich einzelne Standby-Aufrufe (ATA bei SATA, SCSI bei SAS/USB; SAS mit Power Condition 3 zum automatischen Aufwachen). `hd-idle.service` ist maskiert. Firmware-Timer werden nicht verändert. Ein fehlgeschlagener Aufruf erscheint auf der Speicher-Seite und wird erst nach neuer Aktivität oder einer Einstellung erneut versucht. Im Entwicklungsmodus werden keine Standby-Befehle ausgeführt.
- Regressionstests prüfen alle Zeiten, Aktivität, Gerätetausch, Ausschlüsse, fehlerhafte Konfiguration/I/O-Zähler und API-Rechte. Der Release-Smoke-Test prüft Standard Aus und Speicherung über zwei UEFI-Starts. Einschlafen und Aufwachen auf echter Hardware einschließlich USB-Adaptern müssen noch getestet werden.

**Backups** (Einstellungen › Backups, nur für Besitzer und Administratoren; umgesetzt ohne Entwurfsseite, Aufbau selbst gewählt)
- Paket `backup`, Werkzeug restic (Debian-Paket, rund 24 MB). Ein Auftrag sichert Freigaben an ein Ziel: anderes Volume dieses NAS (nie dasselbe wie das der Freigaben), S3-kompatibler Speicher, ein Server über SFTP oder ein restic-REST-Server. Aufträge in `/config/backup/jobs.json`.
- Jeder Auftrag bekommt beim Anlegen ein zufälliges Passwort, mit dem restic verschlüsselt. Die Seite zeigt es direkt danach und später nach erneuter Passwort-Eingabe – ohne dieses Passwort ist ein Backup wertlos, wenn das NAS verloren geht. Geheimnisse des Ziels (Secret Key, Passwort des REST-Servers) gibt die API nie heraus; sie stehen nur in der Umgebung von restic, nie auf einer Kommandozeile.
- SFTP: Das NAS hat einen eigenen Schlüssel (`/config/backup/ssh/id_ed25519`), den die Seite zum Eintragen auf dem Server zeigt. Der Server wird beim ersten Verbinden gemerkt (`known_hosts`); meldet sich später ein anderer unter dem Namen, bricht das Backup ab. Während eines Laufs liegen Schlüssel und `known_hosts` in `/run/titanos-backup`.
- Zeitplan täglich oder wöchentlich zu einer Uhrzeit; ein verpasster Termin wird nach dem Start nachgeholt, das erste Backup beginnt gleich nach dem Anlegen. Aufbewahren: je der neueste von N Tagen, Wochen, Monaten (`restic forget --prune`). Ein Auftrag nach dem anderen.
- Ausgelassen werden `.recycle` und halbe Uploads (`*.titanos-part`). Der Cache von restic und was ein Auftrag über seine Läufe weiß, liegen auf dem Volume der ersten Freigabe (`.titanos/backup/`), nicht im RAM und nicht auf dem Stick.
- Wiederherstellen: ein Stand in einen neuen Ordner `/mnt/<volume>/restored/<auftrag>-<zeit>/`, nie über die Freigabe. Was man braucht, holt man sich von dort.
- Abbrechen beendet restic samt ssh (eigene Prozessgruppe). Fehlgeschlagene Läufe ergeben eine Meldung (`backup_failed`); konnte restic einzelne Dateien nicht lesen, gilt das Backup mit Warnung.
- Getestet in QEMU: Backup auf ein zweites Volume, zweiter Lauf, Wiederherstellen (Prüfsumme gleich, Freigabe unberührt), das Passwort öffnet das Repository auch von Hand, SFTP zum NAS selbst mit dessen Schlüssel, unerreichbarer Server. Nicht getestet: S3 und REST-Server gegen echte Dienste (nur Adresse und Umgebung im Unit-Test).
- Noch offen: einzelne Dateien statt ganzer Stände wiederherstellen, Backups von Docker-Appdata und VMs (geht über Freigaben, aber nicht ausdrücklich angeboten), Prüfen eines Repositorys (`restic check`), Bandbreite begrenzen.

**USV** (Einstellungen › USV; umgesetzt ohne Entwurfsseite, Aufbau selbst gewählt)
- Paket `ups`, Debian-Pakete `nut-server` und `nut-client` (NUT 2.8). Drei Arten: keine USV, USV am Kabel dieses NAS (Treiber `usbhid-ups` für die meisten USB-Geräte, dazu `nutdrv_qx`, `blazer_usb`, `blazer_ser`, `apcsmart`; „Nach USV am USB suchen“ mit `nut-scanner`), oder eine USV, die ein anderes Gerät per NUT im Netz teilt. Einstellungen in `/config/system/ups.json`.
- titand schreibt `/etc/nut/*` bei jedem Start und jeder Änderung (Modus 0640, Gruppe `nut`) und startet die Dienste selbst; ab Werk laufen sie nicht. Das Konto, mit dem upsmon auf diesem NAS liest, bekommt bei jedem Start ein neues Passwort.
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

**Sicherung des Sticks** (Einstellungen › Sicherung des Sticks und die Ersteinrichtungsseite; umgesetzt ohne Entwurfsseite, Aufbau selbst gewählt)
- Paket `configbackup`. Eine Sicherung ist eine Datei `config-<Zeit>-<Prüfsumme>.tar.gz` in `/mnt/<volume>/.titanos/backups` (Ordner nur für root) mit allem aus `/config`, also auch Passwort-Hashes, 2FA-Geheimnissen und den `.env`-Dateien der Apps. Die erste Datei im Archiv, `titanos-backup.json`, nennt Format, Zeit, Name des NAS und Version.
- Ab Werk eingeschaltet, Ziel ist das erste eingebundene Volume (oder ein gewähltes), 14 Kopien bleiben. Einstellungen in `/config/system/backup.json`.
- Gesichert wird einmal nach jedem Start und fünf Minuten nach der letzten Änderung. Ob sich etwas geändert hat, sagt ein Zähler im `config.Store` (jeder Schreibzugriff zählt hoch) – der Stick wird dafür nicht geweckt. Eine neue Datei entsteht nur, wenn sich der Inhalt von der neuesten Kopie unterscheidet. Ist das Ziel-Volume nicht eingebunden, wird nichts geschrieben (der Ordner läge sonst im RAM).
- Herunterladen einer Kopie („optional extern“) nur für Besitzer und Administratoren und erst nach erneuter Passwort-Eingabe, wie bei Konsolen. Schlägt eine Sicherung fehl, gibt es eine Meldung (`config_backup_failed`).
- **Stick ersetzen:** neuen Stick mit dem Zip bespielen und starten. Solange es kein Konto gibt, bietet die Ersteinrichtungsseite „Konfiguration wiederherstellen“ an. TitanOS kennt auf einem neuen Stick seine Volumes nicht; es sieht deshalb in alle laufenden md-Arrays mit ext4 oder XFS hinein – nur lesend und ohne das Journal nachzuspielen (`ro,noload` bzw. `ro,norecovery`), unter `/run/titanos-restore`, und hängt sie sofort wieder aus. Alternativ lässt sich eine heruntergeladene Kopie hochladen. Danach startet das NAS neu.
- Das Wiederherstellen braucht keine Anmeldung (es gibt noch kein Konto) und ist nur möglich, solange keines existiert. Es verschafft niemandem Zugang: Es bringt die Konten der Sicherung zurück. Eine Sicherung wird vollständig geprüft, bevor etwas geschrieben wird (nur gewöhnliche Dateien mit Pfaden innerhalb von `/config`, mindestens ein Konto, Obergrenzen für Größe und Anzahl); die Konten werden zuletzt geschrieben, damit ein abgebrochener Vorgang wiederholt werden kann.
- Nicht umgesetzt: Zurückspielen eines älteren Stands auf ein laufendes, eingerichtetes NAS; Sicherung auf ein Ziel außerhalb des NAS ohne Zutun (kommt mit den Backups in Phase 7); ein Hinweis, wenn der Stick selbst Lesefehler zeigt.

**Benachrichtigungen** (Glocke „Meldungen“ in der Kopfzeile und Einstellungen › Benachrichtigungen; umgesetzt ohne Entwurfsseite, Aufbau selbst gewählt)
- Paket `notify`. Jede Meldung hat eine Art (`volume_degraded`, `disk_unhealthy`, …), einen Betreff (Volume oder Platte) und eine Stufe (Hinweis, Warnung, Fehler). titand behält die letzten 100 Meldungen im Speicher; nach einem Neustart ist die Liste leer. Sichtbar für Besitzer und Administratoren.
- Ein Beobachter sieht alle 30 Sekunden auf den Speicher und meldet Änderungen, nicht Zustände: Volume ohne Redundanz, ausgefallen, nicht gefunden, Wiederaufbau, wieder in Ordnung, Prüfung mit Abweichungen, Volume über 90 % voll (erneut erst nach unter 85 %), Platte mit SMART-Fehler, Platte über 55 °C (erneut erst nach unter 50 °C). Ein Problem wird erst gemeldet, wenn zwei Blicke hintereinander es sehen – beim Start brauchen die Arrays einen Moment.
- Wege nach draußen: E-Mail (SMTP mit STARTTLS oder TLS; ohne Verschlüsselung wird kein Passwort übertragen), ntfy, Telegram, Discord. Je Weg drei Versuche. „Verschicken ab“ legt die Stufe fest (Standard: Warnungen und Fehler); die Glocke zeigt immer alles. Die Sprache der verschickten Texte ist eine Einstellung (`texts.go`), die Glocke übersetzt selbst in die Sprache des Betrachters (`notify.kind.*`).
- Einstellungen in `/config/notify/settings.json`. Passwörter, Tokens und die Webhook-Adresse verlassen das NAS nie über die API (nur „ist gesetzt“); ein leeres Feld behält den gespeicherten Wert, Entfernen ist ausdrücklich (`clearSecrets`). „Testnachricht senden“ speichert erst und zeigt die Antwort der Gegenseite.
- Neue Meldungen aus anderen Paketen: `notify.Manager.Notify(stufe, art, betreff)` aufrufen und die Texte in `texts.go` sowie `notify.kind.<art>.title/.text` in beiden Sprachdateien ergänzen.
- Gelesen/ungelesen merkt sich der Browser, nicht das NAS.

**Netzwerk** (Einstellungen › Netzwerk; umgesetzt ohne Entwurfsseite, Aufbau selbst gewählt)
- Konfiguration in `/config/network/network.json`: Name des NAS, Anschlüsse (keiner gewählt = jeder Anschluss per DHCP, wie ab Werk; mehrere = Bond mit wählbarer Art), VLAN-Nummer, Bridge `br0`, DHCP oder feste Adresse mit Gateway, Nameserver. Daraus schreibt das Paket `network` bei jedem Start und jeder Änderung die Dateien für systemd-networkd (`/etc/systemd/network/10-titanos-*`); Schichtung: Anschlüsse → Bond → VLAN → Bridge → Adresse.
- Die Bridge übernimmt die Hardware-Adresse des ersten Anschlusses, damit der Router über Neustarts dieselbe Adresse vergibt.
- Entscheidung vom 15. Oktober 2026: Firewall vorerst ausgeschaltet; IPv4/IPv6 INPUT, OUTPUT und FORWARD sowie DOCKER-USER geben allen Verkehr frei. `titanos-firewall-open.service` läuft vor dem Netzwerkstart, der Helfer zusätzlich bei Netzwerkänderungen und nach Docker-Starts. NAT und Portweiterleitungen bleiben erhalten. nftables.service ist maskiert, libvirts ungenutztes Standard-NAT-Netz startet nicht automatisch. Eine einstellbare Firewall in der Oberfläche folgt später. Details und Tests: `docs/NETWORKING.md`, `scripts/test-bridge-forwarding.py`, `scripts/network_smoke.py`.
- Adressen, Gateway, DNS und IPv6-Router-Advertisements liegen nur auf der obersten Schnittstelle; Bridge-Ports sind ohne eigene Adressen. Netzwerkänderungen überstehen den Abbruch der anstoßenden HTTP-Verbindung, jeder Anwendungsversuch ist auf 30 Sekunden begrenzt. Fehler beim Umkonfigurieren werden gemeldet; fehlgeschlagene Rückfälle werden erneut versucht und bleiben bis zum Erfolg nicht bestätigbar.
- Jede Änderung gilt 2 Minuten auf Probe und wird erst mit der Bestätigung auf den Stick geschrieben. Die Bestätigung kommt über die Weboberfläche – also über die neue Verbindung – per Leiste oben auf jeder Seite. Ohne sie stellt titand den vorigen Zustand wieder her. Ändert sich die Adresse, muss man sich unter der neuen anmelden und dort bestätigen.
- Zeitzone unter „Einstellungen“ (`/config/system/settings.json`), wird bei jedem Start gesetzt und neuen Apps mitgegeben.
- VMs wählen „nur ausgehend“ oder „eigene Adresse im Heimnetz“ (Karte an `br0`); Letzteres nur, wenn die Bridge eingerichtet ist.
- Getestet in QEMU: Bridge mit DHCP und fester Adresse, Zurücksetzen nach unbestätigter Änderung, VM an der Bridge, Neustart des NAS. Nicht getestet (nur die erzeugten Dateien): Bonding und VLAN – dafür fehlen in der Testumgebung zweite Anschlüsse und ein VLAN-fähiges Netz.
- Noch offen: IPv6-Einstellungen, mehrere Netze, Container mit eigener Adresse im LAN (macvlan/ipvlan an `br0`), Wake-on-LAN.

**Shares** (Reiter „Shares“, Konzept abgestimmt, noch keine Entwurfsseite)
- Liste: Name, Volume und Pfad, Protokolle (SMB, NFS), wer Zugriff hat, belegter Platz, Status.
- Anlegen und Bearbeiten in einem Seitenpanel:
  - Name und Volume. Ablage `/mnt/<volume>/<share>`.
  - Rechte pro Konto: kein Zugriff, nur lesen, lesen und schreiben.
  - Gastzugriff: standardmäßig aus, Einschalten mit Warnung.
  - Papierkorb mit Aufbewahrungsdauer.
  - „In der Netzwerkumgebung sichtbar“.
  - Time-Machine-Ziel mit Größenlimit.
- NFS pro Share optional, mit erlaubten Netzen oder Hosts.
- Löschen entfernt nur die Freigabe, die Daten bleiben. Das Löschen der Daten ist ein eigener Schritt mit ausdrücklicher Bestätigung.
- Verbindungshilfe: fertige Adressen zum Kopieren für Windows, macOS und Linux.
- Konfiguration in `/config/shares/shares.json`, daraus werden `smb.conf` und die NFS-Exports beim Start und bei jeder Änderung erzeugt. Freigaben, deren Volume nicht eingebunden ist, werden nicht angeboten (ihr Ordner läge sonst im RAM).
- Umsetzung der Rechte: Der Zugriff wird im Dienst geregelt, nicht über Dateirechte. Samba prüft Konto und Recht (`valid users`, `write list`; Gäste nur lesend). Alle Dateien gehören der Gruppe `users` (Ordner mit setgid, Modus 2770). NFS kennt keine Konten: Wer auf einem erlaubten Rechner sitzt, schreibt als `nobody:users`.
- Linux- und Samba-Konten entstehen bei jedem Start neu aus `/config/users/` (UID und NT-Hash stehen in `account.json`). Der NT-Hash ist schwach, aber SMB kennt nichts anderes; er entsteht beim Anlegen, beim Passwortwechsel oder bei der nächsten Anmeldung.
- NFS: Jede NFS-Freigabe wird nach `/srv/nfs/<name>` eingebunden (bind mount), `/srv/nfs` ist die Wurzel des NFSv4-Baums. Clients verbinden sich mit `nas:/<name>` (NFSv4) bzw. `nas:/srv/nfs/<name>` (NFSv3). Ganze Volumes werden nie exportiert.
- Der Papierkorb liegt in `.recycle/<konto>` der Freigabe; titand räumt stündlich weg, was älter als die Aufbewahrungsdauer ist.
- Noch nicht geprüft: Time Machine mit einem echten Mac und die Sichtbarkeit in der Netzwerkumgebung (wsdd2, avahi).

**Dateimanager** (Reiter „Dateien“ direkt nach „Shares“, Konzept abgestimmt, noch keine Entwurfsseite)
- Aufbau: links die Shares, auf die das Konto Zugriff hat, darunter freier Platz des gewählten Shares; „Volumes“ und Papierkorb nur für Administratoren. Rechts Pfad als Brotkrumen, Werkzeugleiste (Hochladen, Neuer Ordner, Suchen), Dateiliste (Name, Größe, Geändert), bei Auswahl eine Aktionsleiste (Herunterladen, Verschieben, Umbenennen, Löschen).
- Hochladen per Knopf oder Drag-and-drop, einzelne Dateien und ganze Ordner. Warteschlange mit Fortschritt, Tempo und Restzeit je Datei, Pausieren und Abbrechen; läuft weiter, wenn die Seite gewechselt wird.
- Große Dateien in Stücken hochladen und nach Verbindungsabbruch fortsetzen (ISOs, VM-Abbilder).
- Keine halben Dateien: direkt auf das Ziel-Volume in eine Teildatei schreiben, erst am Ende auf den richtigen Namen umbenennen. Uploads nie im RAM, im Overlay, in `/tmp` oder auf dem Stick puffern.
- Vor dem Start: freien Platz prüfen; bei gleichem Namen fragen: Ersetzen, Beide behalten, Überspringen.
- Herunterladen: einzelne Dateien direkt, mehrere Dateien oder Ordner als Zip-Stream (ohne Zwischendatei).
- Löschen geht in den Papierkorb des Shares, wenn er aktiv ist; sonst endgültig mit Bestätigung. Verschieben und Kopieren großer Mengen als Hintergrundauftrag mit Fortschritt.
- Rechte: Jede Aktion läuft mit den Rechten des angemeldeten Kontos und nur unterhalb von `/mnt`; symbolische Links dürfen nicht aus dem Share hinausführen. Hochgeladene Dateien gehören dem Konto, wie bei einem Upload per SMB.
- Die Ordnerauswahl ist eine gemeinsame Komponente: auch im Container-Editor („Im Dateimanager öffnen“), bei virtiofs und bei der ISO-Auswahl für VMs.
- Umsetzung: Jede Anfrage nennt eine Wurzel (`share/<name>` oder, für Administratoren, `volume/<name>`), auf die das Konto Zugriff hat. Alle Dateizugriffe laufen über `os.Root` und können die Wurzel nicht verlassen, auch nicht über symbolische Links. Ein Upload wird in Stücken von 8 MiB in eine versteckte Teildatei `.<name>.<id>.titanos-part` im Zielordner geschrieben; der Client fragt nach einer Unterbrechung den Stand ab und setzt dort fort.
- Umgesetzt: Auflisten, Hochladen von Dateien und Ordnern (Knopf und Drag-and-drop) mit Pausieren, Fortsetzen, Abbrechen und Abfrage bei gleichem Namen, Herunterladen (Datei oder Zip), Neuer Ordner, Umbenennen, Löschen (Papierkorb bzw. endgültig mit Bestätigung).
- Noch offen aus dem Konzept: Verschieben und Kopieren mit Ordnerauswahl, Suchen, Ansicht des Papierkorbs, die gemeinsame Ordnerauswahl-Komponente. Teildateien eines Uploads, der nie fortgesetzt wird, räumt titand nach 24 Stunden weg – aber nicht nach einem Neustart von titand.
- Später: Vorschau (Bilder, Text, PDF, Video), Freigabe-Links mit Ablaufdatum.

**Docker: Stand der Umsetzung**
- Umgesetzt: Docker-Engine (Debian-Pakete `docker.io`, `docker-cli`, `docker-compose`) mit allen Daten auf einem gewählten Volume (`/mnt/<volume>/.titanos/docker`); die Engine läuft nur, solange dieses Volume eingebunden ist. Einstellung in `/config/docker/settings.json`.
- Umgesetzt: Übersicht mit Karten, Compose-Stacks, Suche, Filter, Start/Stopp/Neustart, Logs, Entfernen sowie „Compose importieren“. Eine importierte App liegt in `/config/apps/<name>/compose.yaml`; relative Pfade der Compose-Datei zeigen auf `/mnt/<volume>/appdata/<name>` (`--project-directory`), nie auf den Stick.
- titand spricht die Docker-API direkt über den Unix-Socket (nur Standardbibliothek) und ruft für Apps `docker compose` auf.
- Umgesetzt: Update-Hinweise (Digest des Tags in der Registry gegen das lokale Image, alle 12 Stunden und per Knopf), „Alle aktualisieren“, WebUI-Link über das Label `titanos.webui` (Port, Port/Pfad oder URL), Ports als Links auch in Stack-Zeilen.
- Umgesetzt: Container-Konsole (xterm.js, WebSocket ↔ `docker exec`), nur für Administratoren nach erneuter Passwort-Eingabe (gilt 15 Minuten), Ende nach 15 Minuten ohne Eingabe, im Protokoll vermerkt.
- Umgesetzt: Container-Editor unter `/docker/apps/<name>` (Formular ↔ Compose auf demselben YAML-Text, Kommentare bleiben erhalten): Image, Autostart/Neustart-Richtlinie, Netzwerk, WebUI, Privilegiert, Pfade, Ports, Variablen, Labels mit Live-Prüfungen.
- Umgesetzt: App-Store unter `/apps` (Backend-Paket `catalog`): eingebauter Startkatalog (`backend/internal/catalog/builtin/<app>/compose.yaml`, per `go:embed`) plus eigene Quellen in `/config/docker/sources.json`. Eine Quelle ist ein Repository (Archiv-Download `/archive/HEAD.tar.gz` bzw. GitLab `/-/archive/…`, kein Git im Abbild), ein Archiv oder eine einzelne Compose-Datei; Vorlagen liegen nur im RAM und werden nach dem Start und täglich neu geladen. Installationspanel mit Herkunft pro Feld; TitanOS ergänzt Zeitzone, PUID/PGID (Konto des Installierenden, Gruppe `users`), Appdata-Pfade, Freigaben für `suggest: media|share` und freie Ports. Geheimnisse (`secret: generate`) stehen in `/config/apps/<name>/.env`, die Compose-Datei verweist per `${NAME}` darauf.
- `x-titanos`-Schema: `name`, `category`, `description` (Text oder `{de, en}`); `variables.<NAME>`: `label`, `help`, `placeholder`, `required`, `secret: generate`, `fill: timezone|uid|gid`; `paths.<Containerpfad>`: `label`, `suggest`; `ports.<Containerport[/udp]>`: `label`, `webui`.
- Umgesetzt: CasaOS-Stores als Quelle (geprüft an `IceWhaleTech/CasaOS-AppStore` und `bigbeartechworld/big-bear-casaos`, zusammen 668 Vorlagen, alle für amd64). Format: `Apps/<Name>/docker-compose.yml` mit `x-casaos` oben (Titel, Tagline, Kategorie, `main`, `port_map`, `scheme`, `index`, `tips.before_install`, `architectures`) und je Dienst (Beschreibungen zu Variablen, Pfaden, Ports). Das Backend liest daraus nur die Angaben für die Liste; übersetzt wird beim Installieren im Browser (`web/src/lib/compose/casaos.ts`):
  - `/DATA/AppData/$AppID/…` → Datenordner der App (`./…`), andere Ordner unter `/DATA` → Vorschlag einer Freigabe (`suggest: media|share`), sonst ebenfalls im Datenordner. Nichts landet unter `/DATA` im RAM.
  - `$AppID`, `$PUID`, `$PGID`, `$TZ` und Platzhalter wie `[CASAOS_IP]` werden ersetzt; offene Platzhalter (`[EUFY_USERNAME]`) werden zu Pflichtfeldern.
  - `port_map`/`scheme`/`index` → Label `titanos.webui`; Hinweise des Autors werden im Panel angezeigt.
  - App-Symbole der Stores (fremde Logos von fremden Servern) werden nicht geladen; es bleibt beim Monogramm.
- Die beiden Stores stehen in der Quellenliste als Vorschlag mit „Hinzufügen“. Sie werden nie von selbst eingetragen: Ihre Apps sind von TitanOS nicht geprüft, und der offizielle Store lädt beim ersten Abruf rund 240 MB.
- Für alle Vorlagen gilt: Das Panel nennt erweiterte Rechte (privilegiert, Host-Netzwerk, Docker-Socket, Wurzelverzeichnis, Capabilities) und warnt bei Pfaden außerhalb der Volumes. Geräte, die es auf dem NAS nicht gibt (z. B. `/dev/dri` ohne GPU, Raspberry-Pi-Geräte), werden weggelassen – mit ihnen startet der Container nicht.
- Große Quellen: tar.gz-Archive werden beim Laden durchgereicht und nur die YAML-Dateien behalten (der offizielle CasaOS-Store ist wegen der Screenshots rund 240 MB groß). Beim täglichen Abgleich schickt titand das ETag mit; ein unverändertes Archiv wird nicht erneut geladen.
- Das Formular schreibt nur zurück, was geändert wurde (`web/src/lib/compose/service.ts`): unberührte Einträge behalten Schreibweise, Kommentare und YAML-Anker.
- Noch offen: eigener Katalog als Repo `ra5on/TitanOS-apps`, App-Symbole, Update-Hinweise aus der Quelle, Herkunft/Geheim-Schalter/Geräte im Editor, `x-titanos.hardware`, Ordnerauswahl, globales Terminal-Panel. CasaOS-Vorlagen enthalten oft feste Standardpasswörter und feste Containernamen (zweite Installation derselben App scheitert daran).

**Docker: Container bearbeiten** (wichtigste Seite neben dem Speicher)
- Formular mit klickbaren, einzeln editierbaren Zeilen in Abschnitten mit Kopfleiste und „… hinzufügen“-Knopf:
  - Allgemein: Name, Image, Netzwerk (bridge, host, eigene IP im LAN, none), Neustart-Richtlinie, WebUI-Adresse, Autostart, Privilegiert (mit Warnung).
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

**Docker: Übersicht**
- Container als Karten: App-Symbol, Name, Image, Status, Update-Hinweis, CPU-Verlauf als Sparkline, RAM, Ports als Chips, WebUI-Link, Start/Stopp direkt auf der Karte.
- Klick auf das App-Symbol oder „…“ öffnet Schnellaktionen: Logs, Konsole, Bearbeiten, Neustarten, Aktualisieren, Entfernen.
- Compose-Stacks (mehrere Container einer App, z. B. immich) als aufklappbare Gruppe.
- Suche und Filter (Alle, Laufen, Gestoppt, Updates), Summen für CPU/RAM, „Alle aktualisieren“, „Compose importieren“.
- Live-Werte per WebSocket. App-Symbole aus der Vorlage; ohne Symbol ein farbiges Monogramm (keine fremden Logos mitliefern).

**Apps: Store mit Quellen**
- App-Quellen frei hinterlegbar (Git-Repository oder URL), täglich und per Knopf aktualisiert. Unterstützte Formate:
  1. TitanOS-Katalog: pro App eine `compose.yaml` mit Metadaten-Block `x-titanos`.
  2. Ordner mit einfachen Compose-Dateien (ohne Metadaten, Felder werden aus der Compose-Datei abgeleitet).
  3. CasaOS-kompatible Stores (Compose mit eigenen Metadaten `x-casaos`).
- `x-titanos` beschreibt pro App: Name, Symbol, Kategorie, Beschreibung; pro Variable Bezeichnung, Hilfetext, Standardwert, `required`, `secret: generate`; pro Pfad Bezeichnung und Vorschlag (`appdata`, `media`, …); pro Port Bezeichnung und `webui: true`.
- Installation in einem Seitenpanel, alles vorausgefüllt. Jedes Feld zeigt seine Herkunft: „aus Vorlage“, „von TitanOS ergänzt“, „automatisch erzeugt“, „bitte ausfüllen“. Zusätzlich direkt im Panel „Eigene Variable / Eigener Pfad / Eigener Port“ (Herkunft „eigene“, gleiche Prüfungen wie im Container-Editor; eine eigene Variable mit gleichem Namen ersetzt den Vorlagenwert mit Hinweis).
- TitanOS ergänzt selbstständig: Zeitzone vom System, PUID/PGID, Appdata-Pfade auf dem schnellsten Volume, Medienpfade aus passenden Shares, freie Ports (bei Konflikt nächster freier Port mit Hinweis), Geheimnisse zufällig erzeugt in `.env`.
- „Als Compose bearbeiten“ führt in den Container-Editor. Installierte Apps sind markiert; Updates aus der Quelle erscheinen als Update-Hinweis in der Docker-Übersicht.
- „Compose-Datei importieren“: einfügen oder hochladen, wird geparst und als Formular angezeigt.

**Terminal und Konsolen**
- Terminal-Symbol ohne Text in der Kopfzeile auf jeder Seite (zusätzlich Strg+`). Öffnet ein Panel am unteren Rand, Höhe ziehbar, Seite darüber bleibt bedienbar; auch als eigener Browser-Tab.
- Mehrere Sitzungen als Tabs, gekennzeichnet nach Art:
  - **NAS** – root-Shell auf dem Host (PTY im Backend). Hinweis beim Start, dass das Systemabbild schreibgeschützt ist und nur `/config` und `/mnt` dauerhaft sind.
  - **CT** – Container-Konsole per `docker exec`, Shell (bash/sh) und Benutzer wählbar. Auch aus dem Schnellmenü der Docker-Übersicht („Konsole“).
- „+“ öffnet: neue NAS-Shell, Konsole eines laufenden Containers, VM-Konsole.
- Werkzeuge: Suchen im Verlauf, Kopieren, Einfügen, Schriftgröße. Farben folgen dem Theme. Sitzungen laufen weiter, wenn das Panel minimiert oder die Seite gewechselt wird.
- Technik: xterm.js im Browser, WebSocket zu titand, PTY (z. B. creack/pty) bzw. Docker-Exec-API.
- Sicherheit: nur angemeldete Besitzer/Administratoren. Die NAS-Shell öffnet auf ausdrücklichen Nutzerwunsch ohne zweite Passwortabfrage; für Gastkonsolen bleibt die erneute Passwort-/2FA-Bestätigung bestehen. Abbruch nach 15 Min Inaktivität; jede Sitzung im Protokoll (wer, wann, welches Ziel); in den Einstellungen komplett abschaltbar.
- Umgesetzt: Panel am unteren Rand (Knopf in der Kopfzeile, Strg+`), Höhe ziehbar, Tabs für NAS-Shell und Container-Konsolen, „+“-Menü, Shell-Auswahl (automatisch/bash/sh) für Container, Suchen, Kopieren, Einfügen, Schriftgröße, eigener Browser-Tab (`/terminal`). Sitzungen laufen weiter, wenn das Panel minimiert oder die Seite gewechselt wird – nicht aber über ein Neuladen der Seite hinweg. Abschalten unter „Einstellungen“ (`/config/system/terminal.json`) beendet offene Sitzungen sofort.
- Die NAS-Shell ist `bash --login` als root hinter einem Pseudo-Terminal (`backend/internal/terminal`, nur `golang.org/x/sys`, kein creack/pty). Im Entwicklungsmodus gibt es sie nicht – es wäre eine Shell auf dem Rechner des Entwicklers.
- Noch offen: Benutzerwahl für Container-Konsolen, 2FA bei der Bestätigung.

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
- Umgesetzt: Durchreichen von PCI-Geräten (Grafikkarten und ausdrücklich ausgewählte Zusatzfunktionen) und USB-Geräten, dazu „Einstellungen › Hardware“ (Paket `hardware`): IOMMU-Gruppe, Treiber, Zuordnung. libvirt nimmt ein PCI-Gerät beim Start der VM vom Treiber des NAS und gibt es beim Stopp zurück (`managed='yes'`); es braucht dafür keinen Neustart und keine „Modi“ – ein Gerät gehört der VM, der es zugeordnet ist, solange sie läuft. Das weicht vom Konzept unter „Hardware und Durchreichung“ ab (dort: Verwendung pro Gerät, Neustart-Banner). NPUs werden nicht angeboten, der Stick, von dem TitanOS läuft, auch nicht.
- Weil `dynamic_ownership = 0` gilt, öffnet titand selbst, was QEMU braucht, und nur für die Laufzeit der VM: Gerätedateien ganzer Platten und zugeordneter USB-Geräte bekommen die Gruppe `kvm`; für `/dev/vfio/*` sorgt eine udev-Regel.
- PCI-Funktionen werden nicht automatisch ergänzt. Benötigte Funktionen derselben IOMMU-Gruppe müssen ausdrücklich ausgewählt werden. Auswahl wird auch unmittelbar vor jedem VM-Start geprüft.
- IOMMU ist im Debian-Kernel ohne Kernel-Parameter aktiv, sobald das BIOS es anbietet.
- Noch offen: Windows 11 selbst (nur der Start der VM mit TPM und Secure Boot ist geprüft, nicht die Installation), virtio-win-ISO automatisch, Abbilder in .xz/.gz/.zip, Hochladen/„von URL laden“ im Formular, Live-Auslastung und Firmware-Status auf der Hardware-Seite; reale GPU-/TPU-Tests. Geräteauswahl und optionaler NVIDIA-Wechsel sind im Entwicklungsstand umgesetzt.
- Getestet in QEMU mit verschachtelter Virtualisierung und emuliertem IOMMU: Als Gast lief eine Kopie des TitanOS-Sticks; im Gast geprüft wurden Kernzahl, TPM, durchgereichtes PCI- und USB-Gerät (beide von QEMU emuliert), ganze Platte und virtiofs. Echte Grafikkarten, ein Gast mit Desktop, Windows und eine deutsche Tastatur im Gast sind nicht geprüft.

**VMs: Erstellen** (Seite „VM erstellen“, aus der VM-Liste per „+“)
- Vorlagen: Windows 11 (TPM 2.0, Secure Boot, virtio-win-ISO automatisch), Linux, Fertiges Abbild, Andere. Name, RAM (mit Hinweis auf freien Speicher), UEFI/OVMF, q35, CPU host-passthrough.
- **Prozessorkerne**, zwei Modi:
  - **Automatisch:** Anzahl vCPUs per Regler, Linux verteilt frei.
  - **Kerne festlegen (Pinning):** Raster aller logischen CPUs „CPU 0, CPU 1, …“, jede mit Kennzeichnung **P-Kern** oder **E-Kern**, Hinweis wer sie schon nutzt (andere VMs) und bei CPU 0 „nicht fest an VMs vergeben“. Schnellwahl: „Alle P-Kerne außer CPU 0“, „Freie E-Kerne“, „Auswahl leeren“.
  - Wichtig: Pinning schränkt **nur die VM** ein. TitanOS, Docker und andere VMs nutzen weiterhin **alle Kerne**, auch die gepinnten. Der Hinweis zu CPU 0 bedeutet nur, dass keine VM fest darauf sitzen sollte (dort landen viele Hardware-Interrupts).
  - Optional **„Kerne exklusiv reservieren, solange die VM läuft“** (Standard aus): beim VM-Start per cgroup v2 cpuset `AllowedCPUs` für `system.slice`, `user.slice` und die Docker-/Container-Slices auf die übrigen Kerne setzen, beim Stopp zurücksetzen. Kein Neustart, kein `isolcpus`. Nur sinnvoll für latenzkritische VMs (Spiele).
  - Erkennung: Kernart aus `/sys/devices/cpu_core/cpus` (P) und `/sys/devices/cpu_atom/cpus` (E); fehlen die Dateien, ist die CPU nicht hybrid. Fallback: maximale Taktfrequenz je Kern. Bei CPUs mit Hyper-Threading/SMT die Geschwister-Threads (`thread_siblings_list`) anzeigen und standardmäßig paarweise vergeben.
  - Warnungen: P- und E-Kerne gemischt (VM kennt die Kernarten nicht, ungleichmäßige Leistung – für Spiele/Windows nur P-Kerne), CPU 0 gewählt, Kern wird mit anderer VM geteilt, keine Auswahl.
  - Umsetzung: libvirt `<cputune><vcpupin …/>`; Emulator- und I/O-Threads (`emulatorpin`) auf die nicht an die VM vergebenen Kerne.
- Datenträger, drei Wege:
  - **Neue virtuelle Platte(n):** pro Platte Format **qcow2** (wächst mit, Snapshots, Standard) oder **img/raw** (volle Größe sofort, etwas schneller), Größe frei per Regler/Eingabe/Schnellwahl, Speicherort (Volume) mit Platzprüfung; weitere Platten hinzufügbar. Ablage `/mnt/<volume>/vms/<name>/vdiskN.<qcow2|img>`. Auf Btrfs-Volumes VM-Ordner mit `nodatacow`.
  - **Vorhandenes Abbild:** TitanOS sucht in Shares und VM-Ordnern nach .qcow2, .img/.raw, .vmdk, .vhdx, .vdi (auch .xz/.gz/.zip) und listet sie mit virtueller und belegter Größe; außerdem Hochladen oder von URL laden. Optionen: Kopie anlegen oder Datei direkt verwenden (nur ohne Umwandlung), Format behalten oder umwandeln (`qemu-img convert`), Größe nur vergrößern (`qemu-img resize`), mit Hinweis, die Partition im Gast zu erweitern.
  - **Ganze Festplatte:** nur Platten, die in keinem Volume stecken, per `/dev/disk/by-id`.
- Plattengröße bestehender VMs später in „Bearbeiten“ vergrößerbar (VM aus, nur vergrößern).
- Durchreichen: GPU (zugehörige Audio-/Zusatzfunktionen einzeln auswählen), iGPU mit Warnung, NPU gesperrt; nur Geräte im passenden Hardware-Modus. Eine GPU darf mehreren VMs zugewiesen sein, es läuft aber immer nur eine davon. Optionale zusätzliche virtuelle Anzeige für VNC.
- **NAS-Ordner freigeben per virtiofs:** Ordner + Name in der VM + Nur lesen/Lesen und schreiben. libvirt braucht dafür Shared-Memory-Backing (automatisch setzen). Linux: `mount -t virtiofs <name> <ziel>`. Windows: VirtIO-FS-Dienst (virtio-win) + WinFsp, TitanOS legt beides aufs Treiber-Medium; Alternative SMB-Share.
- Zusammenfassung rechts, „Erstellen und starten“ / „Nur erstellen“, libvirt-XML ansehbar.

**VMs: Konsole**
- Seite „VMs“: Liste der VMs links (Status, vCPU/RAM), rechts die gewählte VM mit Starten/Herunterfahren/Neustarten/Ausschalten erzwingen und Bearbeiten.
- Ansicht **Bildschirm (VNC)** per noVNC über WebSocket-Proxy in titand (kein offener VNC-Port im LAN): Strg+Alt+Entf senden, Zwischenablage, Screenshot, in eigenem Fenster öffnen. Hinweis: Klick fängt Maus/Tastatur, Strg+Alt gibt frei.
- Konsolenfenster frei in der Größe ziehbar (Ecke unten rechts); die Anzeige passt sich live an. Drei Anzeigemodi:
  - **Skalieren:** Bild ins Fenster einpassen, Seitenverhältnis bleibt (schwarze Ränder).
  - **Auflösung anpassen:** Die VM übernimmt die Fenstergröße als Auflösung (noVNC Remote-Resize; braucht passende Unterstützung in QEMU und im Gast, z. B. virtio-gpu mit Gastagent – vorab prüfen). Fallback: Skalieren.
  - **100 %:** pixelgenau, mit Scrollen.
  - Keine eingeblendete Auflösungs- oder Skalierungsanzeige im Bild.
- **Vollbild** über die Fullscreen-API des Browsers: Konsole füllt den ganzen Bildschirm, schwebende Leiste unten (VM-Name, Strg+Alt+Entf, Anzeigemodus, „Vollbild beenden“), Esc beendet. Zusätzlich Seitenleiste mit VM-Liste ausblendbar für mehr Platz.
- Tastatur als Scancodes übertragen (noVNC/QEMU Extended Key Events), damit deutsche Tastaturen korrekt ankommen – mit deutschem Gast testen.
- Automatisch neu verbinden, wenn die VM neu startet.
- Ansicht **Serielle Konsole** (ttyS0 über libvirt) im selben Terminal-Stil – für Server-VMs ohne Desktop.
- VMs mit GPU-Passthrough: Hinweis, dass das Bild am Monitor der Grafikkarte erscheint; VNC nur mit zusätzlicher virtueller Anzeige.

**Hardware und Durchreichung** (Einstellungen › Hardware)
- Erkennt GPUs und Beschleuniger und zeigt nur Vorhandenes: Intel-iGPU (`/dev/dri`), Intel-NPU (`/dev/accel`), NVIDIA-GPUs, später AMD und Coral. Pro Gerät: PCI-Adresse, IOMMU-Gruppe, Treiber- und Firmware-Status, Live-Auslastung.
- Pro Gerät eine Verwendung:
  - **Container und System** – mehrere Container gleichzeitig (iGPU/NPU per Device-Node, NVIDIA über die Docker-Laufzeit).
  - **Exklusiv für eine VM** – Bindung an `vfio-pci` beim Start. Nur anbieten, wenn IOMMU aktiv und die Gruppe sauber ist. Für die NPU nicht anbieten. Für die iGPU mit Warnung (bei aktuellen Intel-CPUs oft unzuverlässig).
  - **Automatisch wechseln** (NVIDIA, per Plugin): Container nutzen die Karte, solange die zugewiesene VM aus ist; beim VM-Start werden GPU-Container angehalten, die Karte an vfio-pci übergeben und danach zurückgeholt.
- Anzeige „Genutzt von / Reserviert für“ mit Containern und VMs. Änderungen zeigen vorher, wer betroffen ist, und brauchen einen Neustart (Banner mit „Speichern und neu starten“ / „Verwerfen“).
- Container-Editor, App-Installation und VM-Editor bieten nur Geräte an, die im passenden Modus sind. App-Vorlagen können Hardware anfordern (`x-titanos.hardware: [gpu-intel, npu-intel, gpu-nvidia]`); TitanOS trägt dann die richtigen Geräte bzw. die NVIDIA-Laufzeit automatisch ein.
- Weitere Geräte (USB) einzeln an Container oder VMs zuordnen, z. B. Zigbee-Stick an eine VM, USV an TitanOS.

**Plugins** (eigener Reiter)
- Systemerweiterungen für Treiber und Kernelfunktionen – getrennt von Apps (Docker).
- Technik: signierte `systemd-sysext`-Images (squashfs, legt sich über `/usr`), gespeichert auf einem Volume unter `/mnt/<volume>/.titanos/plugins`, geladen nach dem Start der Volumes. Plugin-Einstellungen in `/config/plugins/<name>/`.
- Kernelmodule werden pro TitanOS-Release in der CI für genau diesen Kernel gebaut; Plugin-Updates kommen zusammen mit TitanOS-Updates (A/B-konsistent), sonst passt das Modul nicht.
- Vor der Installation: Rechte-Übersicht „Darf / Darf nicht“ (Kernelmodule, Gerätedateien, Dienste, Docker-Laufzeit, kein Netzwerk, keine Shares). Fortschritt in Schritten (Signatur, Kompatibilität, Download, Einbinden, Module laden, Prüfen).
- Kennzeichnung „Offiziell“ vs. „Community (nicht geprüft)“; Hinweis, ob passende Hardware erkannt wurde.
- Erste Plugins: NVIDIA-Treiber, Intel-GPU-Werkzeuge, ZFS, Coral Edge TPU, Lüftersteuerung (Community).
- **NVIDIA-Plugin:** offene Kernelmodule (für aktuelle RTX-Generationen nötig – vorab prüfen), CUDA/NVENC/NVDEC-Bibliotheken, NVIDIA Container Toolkit. Einstellungen:
  - Energiesparen im Leerlauf (Persistenz-Dienst, damit die Karte in den sparsamsten P-State fällt).
  - Leistungslimit in Prozent (nvidia-smi Power Limit).
  - Automatisch zwischen VM und Containern wechseln (siehe Hardware).
  - Live: P-State, Verbrauch, Temperatur.
  - Lizenz der NVIDIA-Bibliotheken für die Weitergabe vor dem ersten Release prüfen.
- Umgesetzt (Paket `plugins`, Seite „Plugins“): Ein Plugin ist ein sysext-Abbild `<name>.raw` mit Manifest `<name>.plugin.json` (Titel, Beschreibung, Version, SHA-256 und Größe des Abbilds, optional `kernel`, `modules`, `services`). TitanOS findet Plugins, die in einer Freigabe liegen, kopiert sie beim Installieren nach `/mnt/<volume>/.titanos/plugins` (Prüfsumme vor und nach dem Kopieren) und legt die eingeschalteten per Link in `/run/extensions` und `systemd-sysext refresh` über das System, sobald das Volume eingebunden ist. Welche eingeschaltet sind, steht in `/config/plugins/settings.json`.
- „Offiziell“ ist ein Plugin, wenn sein Manifest eine minisign-Signatur (`.minisig`) eines Schlüssels aus `/usr/share/titanos/plugin-keys/*.pub` trägt; das Manifest nennt die Prüfsumme des Abbilds, die Signatur deckt also beides. `plugin-keys` verweist auf die eingebauten Release-/Reserve-Schlüssel unter `update-keys`; offizielle NVIDIA-Manifeste werden im geschützten Publish-Job signiert. Unsignierte Plugins bleiben „Community (nicht geprüft)“ und brauchen die Eingabe ihres Namens. Die Prüfung ist gegen das echte `minisign` getestet (beide Signaturformen).
- Ein Plugin wird Teil des Systems und hat volle Rechte; die Seite sagt das statt einer „Darf / Darf nicht“-Liste, die TitanOS nicht durchsetzen könnte. Plugins für einen anderen Kernel (`kernel` im Manifest) werden nicht geladen.
- Direkter GitHub-Katalog (Entwicklungsstand nach beta.2): Beim Öffnen der Plugin-Seite signiertes Release-Inventar und signierte Plugin-Manifeste passend zur laufenden Version abrufen, zehn Minuten nur im RAM cachen; manueller Refresh möglich. Status-Polling bleibt offline. Erst bei Installation Abbild direkt auf dem Daten-Volume unter `.titanos/plugins/.download-*` zwischenspeichern, Signaturen, Version, Kernel, Größe und SHA-256 prüfen und den bestehenden Installationsweg verwenden; temporäre Downloads bei Erfolg/Fehler entfernen. Keine „latest“-Ausweichversion und keine URLs vom Client. Lokale Dateien bleiben als manueller Weg erhalten; Entwicklungs-Builds und Abruffehler werden erklärt. HTTP-/Signatur-/Abbruchtests in `catalog_test.go`, Anleitung in `docs/PLUGINS.md`.
- Mit einem aktiven Plugin ist `/usr` schreibgeschützt (sysext legt ein Overlay darüber). Das Abbild meldet `SYSEXT_LEVEL=1`; Plugins tragen `ID=titanos` und denselben Level.
- `scripts/make-plugin.sh <name> <version> <ordner>` packt einen Ordner (`usr/…`) zu Abbild und Manifest.
- NVIDIA umgesetzt für `v0.1.0-beta.2`: `scripts/make-nvidia-plugin.sh` baut offene Module 615.71.09 gegen exakt den Kernel des Abbilds, mit SHA-256-fixierten NVIDIA-Debian-Paketen. CUDA/NVENC/NVDEC, NVML und Container Toolkit 1.20.1, Docker über native CDI-Geräte. Kernelquellen und originale Lizenztexte liegen jedem Release bei. Anleitung: `docs/NVIDIA.md`. Keine Grafik-/Desktop-Laufzeit, kein proprietärer Legacy-Treiber. Nouveau ist für den später geladenen NVIDIA-Treiber gesperrt.
- Installation ohne sofortige Aktivierung ist wählbar: geprüfte Dateien auf dem Daten-Volume speichern, noch keine Dienste/Module laden und keine laufenden Verbraucher verändern. Später über den Plugin-Schalter aktivieren. Laufende VMs ohne zugewiesene Plugin-Hardware sperren die Installation nicht. Eine Systemupdate-Sperre hat eine eigene Fehlermeldung und wird nicht mehr als belegte GPU bezeichnet.
- Plugin-Aktivierung prüft Modul- und Dienstfehler. `depmod` schreibt seinen Index unter `/run/titanos-plugin-modules`, weil `/usr` durch sysext schreibgeschützt ist. Abschalten stoppt Dienste und entlädt Module vor dem Aushängen; belegte Module verhindern das. Die Aktivierung auf einer RTX 5060 Ti wurde am 15. Oktober 2026 nach Korrektur der CDI-Dateiausgabe vom Projektinhaber bestätigt; CUDA/NVENC und VM-Wechsel auf dieser Karte bleiben ungetestet.
- Dashboard: grafische Kacheln iGPU/GPU (Intel/AMD), NPU und NVIDIA. Messwerte alle fünf Sekunden, nur im RAM, Verlauf bis drei Minuten; sysfs/DRM-fdinfo (inklusive Xe-Zyklen) und `nvidia-smi`, unbekannte Werte bleiben `null`. Keine Beispieldaten im Produkt.
- Nach beta.4: RAM = MemTotal − MemFree einschließlich Cache; MemAvailable separat. i915-Auslastung über geräteweite perf-Busy-Zähler, fdinfo als Rückfall; Intel-GT-Isttakt und NPU-Busy-Zähler auch während NPU-Suspend. Einstellungen › Temperaturen wählt hwmon-Sensoren pro Konto für eine Dashboard-Kachel, mit stabilen Geräte-/Kanal-IDs. Keine Disk-hwmon-Abfragen, keine erzwungenen Sensortreiber. Details und Hardware-Testgrenzen: `docs/MONITORING.md`.
- Nach beta.4: Das erste neu angelegte Volume erhält `vms`, `appdata`, `iso`, erst nach erfolgreichem Mount, mit fortsetzbarer einmaliger Einrichtung. Bestehende Volumes und Freigaben bleiben unverändert. VM-Anzeigenamen (`displayName`, bis 80 Unicode-Zeichen ohne Steuerzeichen) sind von unveränderlichen Kennungen (`name`) getrennt und auch laufend änderbar; Pfade, MAC/UUID und Dashboard-Zuordnungen bleiben erhalten.
- Entwicklungsstand nach beta.2: Geräteauswahl je Compose-Dienst (`x-titanos-hardware`, stabile PCI-IDs); Auflösung auf aktuelle Render-/Accel-Knoten bzw. NVIDIA-CDI-UUIDs erst beim Start. NVIDIA-Leistungslimit in `/config/plugins/nvidia/settings.json`, Grenzen aus `nvidia-smi`. Optionaler GPU-Wechsel je VM (`autoGPU`): zuvor laufende GPU-Container stoppen, Start konkurrierender GPU-Container sperren, nach Rückgabe des Treibers/CDI erneuern und wieder starten. Reservierungen in `/run/titanos-hardware/leases.json` überleben einen titand-Neustart. Kein automatischer Wechsel für iGPU/NPU/Coral.
- Plugin-Mitnahme: signierte Manifeste und Images vor dem Slot-Wechsel prüfen; pro Release `/mnt/<volume>/.titanos/plugins/releases/<version>/` und `/config/plugins/releases/<version>.json`. Legacy-Dateien und Einstellungen bleiben für den Rückfall erhalten. Aktivierte Plugins gehören zum Update-Healthcheck; Mutationen während Download/ausstehendem Neustart sind gesperrt. Noch keine Bereinigung alter Plugin-Sätze. Der erste Übergang aus beta.2 benötigt bei einem Kernelwechsel das Abschalten und anschließende Neuinstallieren des NVIDIA-Plugins, siehe `docs/PLUGINS.md`.
- Weitere Plugins: `scripts/make-tools-plugins.sh` baut Intel GPU Tools aus Debian (Zusatzbibliotheken, originale Lizenztexte und Quellpaketlinks) sowie den eigenen Coral-USB-Helfer. Coral-Runtime/Firmware werden nicht verteilt; die Container-App bringt sie mit. USB-Bus-Zugriff ist in der Oberfläche ausdrücklich gekennzeichnet. PCIe-/M.2-Coral, ZFS, Lüftersteuerung, NVIDIA-Persistenz-Einstellungen, weitere Plugin-Einstellungen, Fortschritt in Schritten und reale Hardwaretests bleiben offen.
- Btrfs: RAID1 für Daten und Metadaten auf genau zwei GPT-Partitionen; UUID-basiertes Einbinden einschließlich degradiertem Start. Snapshots schreibgeschützt in `.titanos/snapshots`, Wiederherstellung als neue beschreibbare Kopie unter `restored`, nie über laufende Daten. Verschachtelte Subvolumes sind nicht enthalten. Scrub, Ersatz eines fehlenden Mitglieds, geschützte Snapshot-Pfade und Konfigurationssuche ohne Log-Replay umgesetzt. `scripts/smoke-storage.py` testet vier QEMU-Boots, Daten/Snapshots/Restore, fehlendes Mitglied, Ersatz und Intel-/Coral-Plugin-Aktivierung.

## Phasen

0. **Repo-Grundgerüst:** Struktur, Go-Modul, Svelte-Projekt, CI-Skelett.
1. **Bootbares Minimal-OS:** mkosi → squashfs → Zip; Boot in QEMU per UEFI, toram, Overlay, `/config` vom Stick gemountet. *Abnahme: Stick-Ordner bootet, RAM-Verbrauch gemessen.*
2. **titand-Grundgerüst:** HTTP-Server, Login, Svelte-Shell eingebettet, systemd-Unit, HTTPS mit selbstsigniertem Zertifikat. Theme-System (CSS-Variablen, Themes, Akzente, Dichte) und Dashboard-Raster von Anfang an.
3. **Storage:** Platten auflisten (lsblk, smartctl), mdadm-Arrays anlegen, Status, Rebuild, Platte tauschen; Dateisysteme; Mounts aus `/config`. Deutliche Warnung bei RAID0 (keine Redundanz).
4. **Shares:** Samba, NFS, Nutzer und Rechte; danach der Dateimanager (braucht Shares und Rechte).
5. **Docker:** Engine, Container-Liste, Container-Editor (Formular ↔ Compose), Vorlagen und App-Katalog.
6. **VMs und Hardware:** libvirt/QEMU, OVMF, VFIO-Passthrough, noVNC, Hardware-Seite (iGPU/NPU/NVIDIA), Plugin-System (sysext) mit NVIDIA-Plugin.
7. **Betrieb:** A/B-Updates aus der UI, Backups (restic/kopia → S3/Cloud/anderer Server), Benachrichtigungen, SMART-Überwachung, USV (NUT). Umgesetzt: Updates, Backups (restic), Benachrichtigungen, SMART-Überwachung (Meldungen bei Fehlern und Hitze, Plattentest), USV (NUT).

## Regeln

- Kein Unraid-Name, -Logo, -Code und keine 1:1-Unraid-Optik (auch nicht deren Orange-Verlauf). Bedienabläufe dürfen angelehnt sein. „Inspiriert von Unraid“ in der README ist okay.
- GPL-Komponenten (Kernel, Debian-Pakete) werden verteilt: jedem Release die passenden Quellen beilegen oder verlinken (im CI automatisieren).
- Destruktive Aktionen (RAID anlegen, Platte löschen) immer mit ausdrücklicher Bestätigung in der UI.
- Datenverlust vermeiden hat Vorrang vor Funktionen.

### NVIDIA-Treiber auf dem NAS (Entwicklungsstand nach beta.5)

- Nutzerentscheidung vom 15.10.2026: öffentliche neue NVIDIA-Treiber automatisch herunterladen, offene Kernelmodule direkt auf dem NAS bauen, **nur per Knopf „Treiber vorbereiten“**. Keine älteren Archivversionen zusätzlich anbieten. Aktivierung weiterhin ausdrücklich beim nächsten NAS-Neustart, mit Boot-ID und Rückkehr zum mitgelieferten Treiber.
- Separate, im OS-Manifest authentifizierte Bauumgebung mit exakten Kernelheadern; Compiler, Pakete und Arbeitsdateien ausschließlich auf dem Plugin-Daten-Volume. 5 GiB frei, Build begrenzt auf 2 GiB RAM und 200 % CPU, Compilerlauf ohne Netzwerk. Dies ersetzt die frühere Entscheidung „keine Compiler auf dem NAS“ ausdrücklich.
- NVIDIA-InRelease/GPG und Paketprüfsummen prüfen; fertiges lokales Image mit NAS-eigenem Schlüssel authentifizieren, in der UI „Auf diesem NAS gebaut“, niemals als offiziell signiert ausgeben. API-Status liest keine Konfiguration vom Stick. Details: `docs/NVIDIA-LOCAL-BUILD.md`.
- CI baut die passende Bauumgebung und prüft den realen Buildpfad. Der ältere Workflow für fertig gebaute Zusatztreiber bleibt manuell; täglicher CI-Treiberbau ist abgeschaltet. Hardwareaktivierung/CUDA/NVENC weiter auf echtem NAS prüfen.

### Ergänzungen nach beta.5

- Dashboard Layoutversion 3: CPU, RAM und Systeminfos getrennt, pro Kachel Rasterbreite/-höhe und Anzeigeoptionen. CPU-Temperatur optional aus coretemp „Package id 0“. GridStack besitzt seine DOM-Größen; Svelte darf beim Scrollen/Resize keine Neuinitialisierung auslösen. VM-/Container-Kacheln kompakt mit Name, CPU und RAM. VM-RAM = QEMU VmRSS, NAS-RAM bleibt MemTotal − MemFree. Netzwerk mit Schnittstellenwahl, IPv4/IPv6, Gateways, MAC, MTU und Traffic. Siehe `docs/MONITORING.md`.
- Bildschirmbefehle unterhalb des VNC-Fensters, auch im separaten Tab; VM-Power-Aktionen bleiben oben. Vollbildleiste unten.
- Einstellungen › Ein-/Ausschalter: kurzer physischer Tastendruck ignorieren/neustarten/herunterfahren, logind per Reload, persistente Wahl in `/config/system/power-button.json`; langer hardwareseitiger Ausschaltimpuls unverändert. Siehe `docs/POWER-BUTTON.md`.
- Tailscale als signiertes Plugin mit eigener Detail-/Einstellungsansicht, Subnet-Routing, NAS als Exit Node oder Nutzung eines anderen Exit Nodes, LAN-Zugang bleibt erhalten. Einstellungen mit zweiminütiger Sicherheitsfrist; private Geräteschlüssel auf dem Daten-Volume. Scoped Tailscale-Firewall-Hooks vor der allgemeinen Freigabe erhalten. Siehe `docs/TAILSCALE.md`; echtes Tailnet weiter prüfen.


## Entwicklungsstand Beta 0.2.0

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


## Entwicklungsstand nach Beta 0.2.0

- VM-Snapshots: Menü neben Bearbeiten, manuelle Aufnahme, Wiederherstellung,
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
  Die abgeleitete Immich-Compose-Vorlage bleibt AGPL-3.0-only und liegt samt
  Lizenz im Release-Lizenzarchiv. Details: `docs/NATIVE-APPS.md`.
- Release-Prüfung ergänzt echte RAM-/Platten-Restores in einem verschachtelten
  QEMU-Gast auf Btrfs sowie separate App-Installations- und Persistenztests.
  App-Tests nur auf Wegwerf-Runnern bzw. isolierten NAS-Test-VMs ausführen.
