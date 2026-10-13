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
| Speicher | klassisches RAID per **mdadm** (0, 1, 5, 6, 10) mit ext4/XFS (umgesetzt); Btrfs optional für Mirror-Pools mit Snapshots (noch offen); ZFS ggf. später |
| Backend | **Go**, ein einzelnes statisches Binary (`titand`) |
| Frontend | **Svelte + TypeScript**, als statische Dateien per `go:embed` ins Binary eingebettet |
| Veröffentlichung | GitHub: https://github.com/ra5on/TitanOS (öffentlich), Releases per GitHub Actions |
| Lizenz eigener Code | **FSL-1.1-ALv2** (Functional Source License, Fair Source): alles erlaubt außer konkurrierender kommerzieller Nutzung (Verkauf als Produkt, Angebot als Dienst); jede Version wird 2 Jahre nach Erscheinen Apache-2.0. |

## Lizenz und Beiträge

- `LICENSE.md` enthält den unveränderten Text der FSL-1.1-ALv2 (offizielle Vorlage von fsl.software) mit `Copyright 2026 ra5on`. Lizenztext nie umformulieren.
- Bezeichnung: „Fair Source“. Nicht „Open Source“ nennen, solange eine Version noch nicht zu Apache-2.0 gewechselt ist. Lizenzhinweis in README und unter „Einstellungen › Über“.
- Jede Release-Seite nennt das Datum, ab dem diese Version unter Apache-2.0 steht (Erscheinungsdatum + 2 Jahre).
- Fremdkomponenten behalten ihre Lizenz (Linux-Kernel, Debian-Pakete: GPL u. a.; noVNC: MPL-2.0; xterm.js, gridstack: MIT). Das Abbild ist eine Zusammenstellung; GPL-Teile bleiben GPL, ihre Quellen werden jedem Release beigelegt bzw. verlinkt (CI). Eigener Code, der GPL-Code ableitet (z. B. Kernelmodule), muss GPL sein.
- `THIRD_PARTY_LICENSES` im Repo und im Abbild (unter „Einstellungen › Über“ anzeigen).
- Beiträge nur mit Contributor License Agreement (z. B. per CLA-Bot), damit kommerzielle Lizenzen später möglich bleiben. `CONTRIBUTING.md` erklärt das.
- Name: „basiert auf Debian“ ist okay, „Debian“ nicht im Produktnamen.

## Offene Punkte

- **CLA:** Entwurf liegt in `CLA.md` (Lizenzeinräumung ohne Übertragung des Urheberrechts, Recht zur Unterlizenzierung inkl. kommerziell und Apache-2.0, Patentlizenz) – vor Nutzung prüfen lassen. Bot: gehosteter CLA Assistant (cla-assistant.io) mit dem CLA-Text als GitHub Gist. Die „Lite“-GitHub-Action (contributor-assistant/github-action) ist seit März 2026 archiviert – nicht verwenden.
- Initramfs: Debian `live-boot` ist im Einsatz und in QEMU getestet (`toram=filesystem.squashfs`, `live-media-path=` pro A/B-Eintrag, `overlay-size=512M`). Ein initramfs-Skript wartet bis zu 30 Sekunden auf den Stick, damit live-boot ihn beim ersten Versuch findet und nicht alle anderen Platten durchsucht. Nur wenn der Stick gar nicht auftaucht, sucht live-boot weiter und bindet dabei fremde Dateisysteme lesend ein.

## Noch nicht entworfen, aber Pflicht vor dem ersten Release

Für diese Punkte gibt es noch keine Entwurfsseite. Vor der Umsetzung kurz mit dem Projektinhaber abstimmen und einen Entwurf vorschlagen.

1. **Backup des Sticks:** automatisches Backup von `/config` auf ein Volume (optional extern) und geführter Ablauf „Stick ersetzen“ (neuen Stick flashen, Konfiguration zurückspielen).
2. **Ersteinrichtung:** Assistent beim ersten Start – Sprache, Kontoname und Passwort (Besitzer), Hostname, Netzwerk, Zeitzone, erstes Volume.
3. **Netzwerk:** feste IP, Bridge `br0` (für VMs/Container mit eigener IP), VLANs, Bonding, DNS. Änderungen mit Sicherheitsfrist: ohne Bestätigung innerhalb von 2 Minuten automatisch zurück.
4. **Shares und Nutzer:** Seiten für SMB/NFS-Freigaben, Rechte, Papierkorb, Time-Machine-Unterstützung; Kontoverwaltung mit Rollen. Konzept für Shares abgestimmt, siehe Web-UI › Shares.
5. **Platten vorbereiten:** Belastungstest neuer Platten vor dem Einbau ins RAID (SMART-Langtest + Schreib-/Lesetest).
6. **Benachrichtigungen:** E-Mail und Push (z. B. ntfy, Telegram, Discord) bei Plattenfehlern, Rebuild, vollen Volumes, Updates, Backup-Fehlern.
7. **Sicherheit:** 2FA (TOTP), Sperre nach Fehlversuchen, Let's-Encrypt-Zertifikat (DNS-Challenge) oder eigenes Zertifikat, SSH standardmäßig aus.
8. **Sprachen:** Deutsch und Englisch von Anfang an, alle Texte über Übersetzungsdateien.
9. **Diagnose:** Log-Ansicht und „Diagnosepaket erstellen“ (Passwörter, Schlüssel und IPs entfernt) für GitHub-Issues.
10. **Dateimanager:** Dateien im Browser auf Shares hochladen (auch ganze Ordner, per Drag-and-drop, große Dateien fortsetzbar), herunterladen, verschieben, umbenennen, löschen; mit den Rechten des angemeldeten Kontos. Konzept abgestimmt, siehe Web-UI › Dateimanager.

Später: Verlaufsgrafiken (CPU, Temperaturen, Netzwerk, Platten), Volume-Verschlüsselung (LUKS), eingebauter Fernzugriff (WireGuard/Tailscale/Cloudflare Tunnel), Energie (Plattenschlaf, Zeitplan, Wake-on-LAN), Befehlspalette (Strg+K), dokumentierte API und CLI.

## Stick-Layout

```
TITANOS (FAT32)
├── EFI/BOOT/BOOTX64.EFI          # systemd-boot
├── loader/
│   ├── loader.conf
│   └── entries/
│       ├── titanos-a.conf
│       └── titanos-b.conf         # erst nach dem ersten Update
├── titanos/
│   ├── a/  vmlinuz, initrd.img, filesystem.squashfs
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
- Firmware ausdünnen: nur gängige NICs, HBAs und GPUs – Intel-GPU-Firmware (GuC/HuC) und Intel-NPU-Firmware müssen drin bleiben.
- RAM-Ziel: komprimiertes Systemabbild ca. 0,5–0,8 GB; Grundsystem mit allen Diensten (Kernel, titand, Samba/NFS, Docker-Engine, libvirt) ca. 1,0–1,5 GB, ohne Container und VMs. Plugins liegen auf einem Volume und belegen keinen festen RAM.

## Repo-Struktur (Vorschlag)

```
titanos/
├── CLAUDE.md
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
│       ├── vm/             # libvirt (go-libvirt, ohne cgo)
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

## Testen

- QEMU mit OVMF (UEFI). Stick-Inhalt als FAT-Laufwerk: entweder `-drive file=fat:rw:<ordner>` (vvfat, Schreibzugriff experimentell) oder ein per mtools erzeugtes FAT-Image.
- Zusätzliche virtuelle Platten für RAID-Tests (Ausfall simulieren, Rebuild prüfen).
- Später Test auf echter Hardware mit echtem Stick.

## Web-UI

Bedienkonzept angelehnt an Unraid, Gestaltung eigenständig.

**Entwürfe:** `docs/design/` enthält die Quelltexte aller Entwurfsseiten (`*.dc.html`, Übersicht in `docs/design/README.md`). Vor der Arbeit an einer Seite die passende Datei lesen und Aufbau, Abstände, Farben (siehe `Appearance.dc.html` für alle Theme-Werte) und Verhalten übernehmen – nicht den Code, der nutzt ein Entwurfsformat mit eigener Laufzeit. Beispieldaten darin nie ins Produkt übernehmen. Screenshots, falls vorhanden, in `docs/design/screenshots/`.

**Grundaufbau**
- Kopfzeile: Logo (Symbol + Schriftzug, siehe „Marke“), Hostname/IP, Reiter-Navigation (Dashboard, Speicher, Shares, Dateien, Nutzer, Docker, VMs, Apps, Plugins, Einstellungen, Werkzeuge), Terminal, Meldungen, Konto-Menü.
- **Konto-Menü** (Klick auf den eigenen Namen oben rechts, mit Initiale in Akzentfarbe): Name und Rolle, Mein Konto (Passwort, 2FA, SSH-Schlüssel), Darstellung, Angemeldete Geräte, NAS neu starten …, NAS herunterfahren …, Abmelden. Neustart/Herunterfahren nur für Besitzer/Administratoren und immer mit Bestätigungsdialog, der zeigt, was passiert: laufende Container/VMs werden sauber beendet, laufender Rebuild/Prüfung wird unterbrochen und fortgesetzt, Option „erst nach Abschluss des Wiederaufbaus“, Hinweis auf Wake-on-LAN beim Herunterfahren.
- Kopfzeile immer einzeilig mit fester Höhe (ca. 60 px). Reicht die Breite nicht, scrollt die Navigation seitlich – nie umbrechen.
- Statusleiste unten: Volume-Zustand, laufende Rebuilds/Prüfungen, Version und System-Slot.
- Schrift: IBM Plex Sans für Text, IBM Plex Mono für Werte, Pfade, Gerätenamen (lokal ausliefern, keine Google-Fonts-Abhängigkeit im Produkt).
- Mobil: Navigation umbrechen, Kacheln stapeln, breite Tabellen horizontal scrollbar.

**Marke** (Entwurf `Brand.dc.html`, Dateien in `docs/brand/`)
- Symbol: Sechseck-Rahmen mit „T“ (mit Fuge zwischen Balken und Stamm). Schriftzug „TITANOS“ aus eigenen geometrischen Formen (keine Schriftart, keine Lizenzfrage): gleiche Strichstärke, „A“ ohne Querstrich, „T“ mit Fuge.
- Rahmen und „OS“ immer in `--accent`, Buchstaben in `--text`. Im hellen Theme Akzent abdunkeln (Kontrast ≥ 3:1).
- Als Svelte-Komponente mit CSS-Variablen umsetzen. Favicon: Akzentquadrat mit „T“.

**Konten und Rechte**
- Ersteinrichtung legt das erste Konto mit **frei gewähltem Namen** an (z. B. „alex“). Dieses Konto ist **Besitzer** mit vollen Rechten – praktisch wie root – und sein Name steht oben rechts. Kein festes „admin“.
- Der Besitzer ist **nicht** der Linux-Benutzer root: root ist gesperrt (kein Passwort-Login), SSH ist standardmäßig aus. Wird SSH aktiviert, meldet man sich mit dem eigenen Namen und SSH-Schlüssel an und nutzt `sudo`. Gründe: kein erratbarer Login-Name, jede Aktion im Protokoll einer Person zuordenbar.
- titand läuft als Systemdienst mit den nötigen Rechten und führt Aktionen im Namen des angemeldeten Kontos aus. Die NAS-Shell im Terminal ist eine root-Shell, erst nach erneuter Passwort-/2FA-Bestätigung.
- Rollen: **Besitzer** (alles, mindestens einer muss existieren), **Administrator** (alles außer Besitzer verwalten), **Benutzer** (nur eigene Shares/Dateien, kein Docker/VMs/System). Kontoname wird auch als SMB-Benutzer angelegt.
- Konten, Passwort-Hashes (argon2id) und 2FA-Geheimnisse in `/config/users/`. Anmeldeschutz gegen Durchprobieren.

**Dashboard mit frei anpassbaren Kacheln**
- Raster aus 12 Spalten mit festen Zeilenhöhen. Kacheln rasten auf Spalten/Zeilen ein, keine freien Pixelgrößen.
- Bearbeiten-Modus (Schalter auf dem Dashboard): Kacheln am Griff verschieben, an der Ecke Größe ändern, ausblenden, über „Kachel hinzufügen“ zurückholen, „Standard wiederherstellen“. Außerhalb des Modus ist alles fixiert.
- Layout pro Nutzer in `/config` gespeichert. Auf schmalen Bildschirmen wird automatisch gestapelt.
- Umsetzung z. B. mit gridstack.js.

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
  3. CasaOS-kompatible Stores (Compose mit eigenen Metadaten) – Format vor der Umsetzung genau prüfen.
- `x-titanos` beschreibt pro App: Name, Symbol, Kategorie, Beschreibung; pro Variable Bezeichnung, Hilfetext, Standardwert, `required`, `secret: generate`; pro Pfad Bezeichnung und Vorschlag (`appdata`, `media`, …); pro Port Bezeichnung und `webui: true`.
- Installation in einem Seitenpanel, alles vorausgefüllt. Jedes Feld zeigt seine Herkunft: „aus Vorlage“, „von TitanOS ergänzt“, „automatisch erzeugt“, „bitte ausfüllen“. Zusätzlich direkt im Panel „Eigene Variable / Eigener Pfad / Eigener Port“ (Herkunft „eigene“, gleiche Prüfungen wie im Container-Editor; eine eigene Variable mit gleichem Namen ersetzt den Vorlagenwert mit Hinweis).
- TitanOS ergänzt selbstständig: Zeitzone vom System, PUID/PGID, Appdata-Pfade auf dem schnellsten Volume, Medienpfade aus passenden Shares, freie Ports (bei Konflikt nächster freier Port mit Hinweis), Geheimnisse zufällig erzeugt in `.env`.
- „Als Compose bearbeiten“ führt in den Container-Editor. Installierte Apps sind markiert; Updates aus der Quelle erscheinen als Update-Hinweis in der Docker-Übersicht.
- „Compose-Datei importieren“: einfügen oder hochladen, wird geparst und als Formular angezeigt.

**Terminal und Konsolen**
- Terminal-Knopf in der Kopfzeile auf jeder Seite (zusätzlich Strg+`). Öffnet ein Panel am unteren Rand, Höhe ziehbar, Seite darüber bleibt bedienbar; auch als eigener Browser-Tab.
- Mehrere Sitzungen als Tabs, gekennzeichnet nach Art:
  - **NAS** – root-Shell auf dem Host (PTY im Backend). Hinweis beim Start, dass das Systemabbild schreibgeschützt ist und nur `/config` und `/mnt` dauerhaft sind.
  - **CT** – Container-Konsole per `docker exec`, Shell (bash/sh) und Benutzer wählbar. Auch aus dem Schnellmenü der Docker-Übersicht („Konsole“).
- „+“ öffnet: neue NAS-Shell, Konsole eines laufenden Containers, VM-Konsole.
- Werkzeuge: Suchen im Verlauf, Kopieren, Einfügen, Schriftgröße. Farben folgen dem Theme. Sitzungen laufen weiter, wenn das Panel minimiert oder die Seite gewechselt wird.
- Technik: xterm.js im Browser, WebSocket zu titand, PTY (z. B. creack/pty) bzw. Docker-Exec-API.
- Sicherheit: nur Administratoren; Passwort (und 2FA) beim ersten Öffnen erneut bestätigen; Abbruch nach 15 Min Inaktivität; jede Sitzung im Protokoll (wer, wann, welches Ziel); in den Einstellungen komplett abschaltbar.

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
- Durchreichen: GPU (inkl. zugehöriger Audio-Funktion automatisch), iGPU mit Warnung, NPU gesperrt; nur Geräte im passenden Hardware-Modus. Eine GPU darf mehreren VMs zugewiesen sein, es läuft aber immer nur eine davon. Optionale zusätzliche virtuelle Anzeige für VNC.
- **NAS-Ordner freigeben per virtiofs:** Ordner + Name in der VM + Nur lesen/Lesen und schreiben. libvirt braucht dafür Shared-Memory-Backing (automatisch setzen). Linux: `mount -t virtiofs <name> <ziel>`. Windows: VirtIO-FS-Dienst (virtio-win) + WinFsp, TitanOS legt beides aufs Treiber-Medium; Alternative SMB-Share.
- Zusammenfassung rechts, „Erstellen und starten“ / „Nur erstellen“, libvirt-XML ansehbar.

**VMs: Konsole**
- Seite „VMs“: Liste der VMs links (Status, vCPU/RAM), rechts die gewählte VM mit Starten/Herunterfahren/Neustarten/Ausschalten erzwingen und Bearbeiten.
- Ansicht **Bildschirm (VNC)** per noVNC über WebSocket-Proxy in titand (kein offener VNC-Port im LAN): Strg+Alt+Entf senden, Zwischenablage, Screenshot, in eigenem Fenster öffnen. Hinweis: Klick fängt Maus/Tastatur, Strg+Alt gibt frei.
- Konsolenfenster frei in der Größe ziehbar (Ecke unten rechts); die Anzeige passt sich live an. Drei Anzeigemodi:
  - **Skalieren:** Bild ins Fenster einpassen, Seitenverhältnis bleibt (schwarze Ränder).
  - **Auflösung anpassen:** Die VM übernimmt die Fenstergröße als Auflösung (noVNC Remote-Resize; braucht passende Unterstützung in QEMU und im Gast, z. B. virtio-gpu mit Gastagent – vorab prüfen). Fallback: Skalieren.
  - **100 %:** pixelgenau, mit Scrollen.
  - Kleine Anzeige im Bild: Gast-Auflösung und angezeigte Größe.
- **Vollbild** über die Fullscreen-API des Browsers: Konsole füllt den ganzen Bildschirm, schwebende Leiste oben (VM-Name, Strg+Alt+Entf, Anzeigemodus, „Vollbild beenden“), Esc beendet. Zusätzlich Seitenleiste mit VM-Liste ausblendbar für mehr Platz.
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

## Phasen

0. **Repo-Grundgerüst:** Struktur, Go-Modul, Svelte-Projekt, CI-Skelett.
1. **Bootbares Minimal-OS:** mkosi → squashfs → Zip; Boot in QEMU per UEFI, toram, Overlay, `/config` vom Stick gemountet. *Abnahme: Stick-Ordner bootet, RAM-Verbrauch gemessen.*
2. **titand-Grundgerüst:** HTTP-Server, Login, Svelte-Shell eingebettet, systemd-Unit, HTTPS mit selbstsigniertem Zertifikat. Theme-System (CSS-Variablen, Themes, Akzente, Dichte) und Dashboard-Raster von Anfang an.
3. **Storage:** Platten auflisten (lsblk, smartctl), mdadm-Arrays anlegen, Status, Rebuild, Platte tauschen; Dateisysteme; Mounts aus `/config`. Deutliche Warnung bei RAID0 (keine Redundanz).
4. **Shares:** Samba, NFS, Nutzer und Rechte; danach der Dateimanager (braucht Shares und Rechte).
5. **Docker:** Engine, Container-Liste, Container-Editor (Formular ↔ Compose), Vorlagen und App-Katalog.
6. **VMs und Hardware:** libvirt/QEMU, OVMF, VFIO-Passthrough, noVNC, Hardware-Seite (iGPU/NPU/NVIDIA), Plugin-System (sysext) mit NVIDIA-Plugin.
7. **Betrieb:** A/B-Updates aus der UI, Backups (restic/kopia → S3/Cloud/anderer Server), Benachrichtigungen, SMART-Überwachung, USV (NUT).

## Regeln

- Kein Unraid-Name, -Logo, -Code und keine 1:1-Unraid-Optik (auch nicht deren Orange-Verlauf). Bedienabläufe dürfen angelehnt sein. „Inspiriert von Unraid“ in der README ist okay.
- GPL-Komponenten (Kernel, Debian-Pakete) werden verteilt: jedem Release die passenden Quellen beilegen oder verlinken (im CI automatisieren).
- Destruktive Aktionen (RAID anlegen, Platte löschen) immer mit ausdrücklicher Bestätigung in der UI.
- Datenverlust vermeiden hat Vorrang vor Funktionen.
