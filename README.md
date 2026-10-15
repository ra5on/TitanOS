<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/titanos-wordmark-dark-bg.svg">
    <img src="docs/brand/titanos-wordmark-light-bg.svg" alt="TitanOS" width="420">
  </picture>
</p>

<p align="center"><strong>Ein schlankes NAS-Betriebssystem, das vom USB-Stick bootet und komplett im RAM läuft.</strong></p>

---

> **Stand:** `v0.1.0-beta.3` ergänzt direkte Plugin-Downloads, Btrfs-Spiegel und zusätzliche Hardware-Funktionen. Sie richtet sich an Tester mit separaten Testplatten und gesicherten Daten. Downloads und Anleitung stehen auf der [Release-Seite](https://github.com/ra5on/TitanOS/releases/tag/v0.1.0-beta.3); ein Entwicklungs-Build ist kein veröffentlichtes Release. Die bisherigen Systemtests liefen überwiegend in QEMU, die Prüfung auf echter NAS-Hardware steht noch aus.

<p align="center">
  <img src="docs/screenshots/dashboard.png" alt="Dashboard von TitanOS mit den Kacheln System, Volumes und Netzwerk" width="900">
</p>

## Was TitanOS ist

TitanOS macht aus einem PC ein NAS. Das System liegt auf einem USB-Stick, wird beim Start komplett in den Arbeitsspeicher geladen und läuft von dort. Der Stick wird danach nur noch angefasst, wenn sich die Konfiguration ändert oder ein Update kommt. Bedient wird alles im Browser.

Die Idee ist von Unraid inspiriert, setzt aber auf klassisches RAID. TitanOS ist ein eigenständiges Projekt und steht in keiner Verbindung zu Lime Technology. Es basiert auf Debian 13 (trixie) für x86-64.

## Neu in v0.1.0-beta.2

Unter **Speicher › Ruhemodus für Festplatten** lässt sich eine gemeinsame Leerlaufzeit für alle unterstützten HDDs wählen: **Aus**, **5, 10, 15, 30 Minuten** oder **1, 2, 3 Stunden**. Standard ist Aus. Jede Platte schläft nach ihrer eigenen Zeit ohne Zugriff ein; der nächste Zugriff weckt sie wieder auf und kann einige Sekunden dauern. SSDs, NVMe, Bootstick und an VMs vergebene Platten sind ausgenommen. Plattentests und RAID-Wartung pausieren die Zeitmessung; Apps und Freigaben können Platten wach halten.

Die Einstellung bleibt über Neustarts erhalten. Unterstützt werden nicht als Wechselmedium gemeldete SATA-/SAS-/SCSI-HDDs sowie USB-HDDs mit geeignetem Adapter. Der tatsächliche Schlaf- und Aufweckvorgang muss noch auf echter Hardware geprüft werden.

Das optionale **NVIDIA-Plugin** enthält offene Kernelmodule für Turing und neuer sowie CUDA-, NVENC-/NVDEC-Bibliotheken und das NVIDIA Container Toolkit. Es liegt auf einem Daten-Volume und wird passend zum Kernel als separate Release-Datei ausgeliefert. Installation, Signaturprüfung und ein Compose-Beispiel stehen in der [NVIDIA-Anleitung](docs/NVIDIA.md). Echte GPU-Tests stehen noch aus.

Unter **Dashboard bearbeiten › Kachel hinzufügen** stehen drei grafische Kacheln für **iGPU/GPU (Intel und AMD)**, **NPU** und **NVIDIA** bereit. Sie zeigen Lastverlauf, Temperatur, Leistungsaufnahme, Gerätespeicher und Takt, soweit der Treiber diese Werte liefert. Ein fehlender Wert erscheint als „–“, Geräte ohne Treiber oder mit VM-Zuweisung bekommen einen eigenen Status. Das bestehende Dashboard-Layout bleibt erhalten.

## Was schon funktioniert

- **Start vom Stick, Betrieb im RAM.** Das komprimierte Systemabbild wird beim Booten in den Arbeitsspeicher kopiert, darüber liegt eine auf 512 MB begrenzte Schreibschicht. Container, VM-Abbilder und Nutzdaten liegen auf Daten-Volumes. Größe und RAM-Bedarf hängen vom Release und den laufenden Diensten ab.
- **Konfiguration auf dem Stick.** Systemeinstellungen, Konten und Schlüssel liegen im Ordner `config` auf dem Stick. Der Stick wird bei Bedarf eingebunden und nach 30 Sekunden ohne Zugriff wieder ausgehängt. Das reduziert Schreibzugriffe; eine Sicherung der Konfiguration bleibt nötig.
- **Ersteinrichtung und Anmeldung.** Beim ersten Start legst du das Besitzerkonto mit frei gewähltem Namen an; ein kurzer Assistent fragt danach Name und Zeitzone ab. Es gibt kein festes „admin“ und keinen Login als root. Wiederholte Fehlversuche werden ausgebremst, und jedes Konto lässt sich mit einem zweiten Faktor (Code aus einer Authenticator-App) schützen.
- **HTTPS von Anfang an.** Die Weboberfläche läuft über HTTPS mit einem selbst signierten Zertifikat, das über Neustarts gleich bleibt. Ein eigenes Zertifikat mit Schlüssel und Zertifikatskette lässt sich als PEM importieren und ohne Neustart verwenden.
- **Dashboard mit frei anordenbaren Kacheln.** Kacheln rasten in einem Raster aus 12 Spalten ein, lassen sich verschieben, in der Größe ändern und ausblenden. Das Layout wird pro Konto gespeichert.
- **Darstellung nach Wunsch.** Themes, Akzentfarben, Glas-Effekt, Eckenradius, Schriftgröße, Dichte und Hintergrund, mit Vorschau und Export als Datei.
- **Klassisches RAID.** Volumes mit RAID 0, 1, 5, 6 und 10 über Linux-Software-RAID (mdadm) mit ext4 oder XFS. Fällt eine Platte aus, lässt sie sich in der Oberfläche entfernen und ersetzen; Wiederaufbau und Prüfung zeigen ihren Fortschritt.
- **Freigaben.** SMB mit Rechten pro Konto, Gastzugriff nur lesend, Papierkorb und Time-Machine-Ziel; NFS für ausgewählte Rechner. Dazu fertige Adressen zum Verbinden.
- **Dateimanager im Browser.** Dateien und ganze Ordner hochladen, auch per Drag-and-drop; große Uploads lassen sich pausieren und nach einer Unterbrechung fortsetzen. Dazu herunterladen (auch als Zip), umbenennen und löschen in den Papierkorb.
- **Docker.** Die Engine speichert alles auf einem Volume deiner Wahl. Die Übersicht zeigt Container und Stacks mit CPU, RAM, Ports und Update-Hinweisen, dazu Start, Stopp, Logs und eine Konsole im Browser. Jede App lässt sich als Formular oder als Compose-Datei bearbeiten.
- **App-Store.** Ein kleiner eingebauter Katalog und eigene Quellen (Repository, Archiv oder Compose-Datei), darunter CasaOS-kompatible Stores mit mehreren hundert Apps. Beim Installieren füllt TitanOS Zeitzone, Pfade, freie Ports und Geheimnisse selbst aus und zeigt bei jedem Feld, woher der Wert kommt.
- **Virtuelle Maschinen.** KVM/QEMU mit UEFI: VMs anlegen, starten und stoppen, mit dem Bildschirm der VM direkt im Browser. Kerne festlegen, vorhandene Abbilder und ganze Platten nutzen, Grafikkarten und USB-Geräte durchreichen, NAS-Ordner in der VM einbinden, TPM 2.0 und Secure Boot.
- **Netzwerk.** Feste Adresse, Bonding, VLAN und eine Bridge, über die VMs eine eigene Adresse im Heimnetz bekommen. Jede Änderung gilt erst auf Probe: Ohne Bestätigung innerhalb von zwei Minuten kehrt das NAS zur alten Einstellung zurück.
- **Benachrichtigungen.** Fällt eine Platte aus, läuft ein Wiederaufbau oder wird ein Volume voll, steht das in der Weboberfläche und kommt auf Wunsch per E-Mail, ntfy, Telegram oder Discord.
- **Backups.** Freigaben mit restic verschlüsselt auf ein anderes Volume, einen anderen Server (SFTP), S3-Speicher oder einen restic-REST-Server sichern, nach Zeitplan und mit Ständen der letzten Tage, Wochen und Monate; Wiederherstellen in einen eigenen Ordner. S3 und REST sind noch nicht gegen echte Dienste geprüft.
- **USV.** Per USB oder über das Netzwerk; bei Stromausfall fährt das NAS sauber herunter, bevor die Batterie leer ist, und kann den Zustand der USV an andere Geräte weitergeben.
- **Updates mit Rückfall.** Der Stick hält zwei Systeme. Eine neue Version kommt in das, das gerade nicht läuft, und startet auf Probe; kommt sie nicht hoch, startet das NAS von selbst wieder die vorherige. Installiert wird nur, was mit dem Schlüssel des Projekts signiert ist.
- **Sicherung des Sticks.** Automatische Sicherung der Konfiguration auf ein verfügbares Volume, gebündelt nach Änderungen; zusätzlich Download und Wiederherstellung. Beim Ersatz eines Sticks lässt sich eine gefundene Sicherung in der Ersteinrichtung auswählen und zurückspielen.
- **Platten vorbereiten.** Neue Platten lassen sich vor dem Einbau prüfen: ganz lesen oder überschreiben und zurücklesen, danach der lange Selbsttest der Platte.
- **Protokoll und Diagnosepaket.** Die Meldungen des Systems im Browser, nach Quelle und Stufe gefiltert. Für Fehlerberichte packt TitanOS Protokolle und Systemzustand in eine Zip-Datei und filtert bekannte Adress-, Namens- und Geheimnisfelder. Das Paket vor dem Weitergeben prüfen; die Filter können unbekannte Formate übersehen.
- **Plugins.** Systemerweiterungen für Treiber und Werkzeuge, die auf einem Volume liegen und sich ein- und ausschalten lassen. Das NVIDIA-Plugin wird separat mit passendem Kernel und signiertem Manifest ausgeliefert; siehe [Anleitung](docs/NVIDIA.md).
- **Terminal.** Eine Shell auf dem NAS und Konsolen in Containern als Tabs am unteren Rand jeder Seite – nur für Besitzer und Administratoren, nach erneuter Passwort- und gegebenenfalls 2FA-Bestätigung.
- **SSH und eigenes Zertifikat.** SSH ist ab Werk aus; eingeschaltet melden sich Administratoren mit Schlüssel unter ihrem eigenen Namen an, nie als root. Die Weboberfläche nimmt ein eigenes Zertifikat an.
- **Konten mit Rollen.** Besitzer, Administratoren und Benutzer; jedes Konto gilt für die Weboberfläche und für SMB.
- **Deutsch und Englisch.**

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/dashboard-bearbeiten.png" alt="Dashboard im Bearbeiten-Modus mit Griffen und Spaltenraster"></td>
    <td width="50%"><img src="docs/screenshots/darstellung.png" alt="Einstellungen › Darstellung mit Theme-Auswahl und Vorschau"></td>
  </tr>
  <tr>
    <td align="center">Dashboard bearbeiten</td>
    <td align="center">Einstellungen › Darstellung</td>
  </tr>
</table>

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/speicher.png" alt="Speicher: zwei Volumes mit Mitgliedern und Belegung, darunter die Plattentabelle"></td>
    <td width="50%"><img src="docs/screenshots/shares.png" alt="Shares: eine Freigabe mit Rechten und Adressen zum Verbinden"></td>
  </tr>
  <tr>
    <td align="center">Speicher</td>
    <td align="center">Shares</td>
  </tr>
</table>

<p align="center">
  <img src="docs/screenshots/docker.png" alt="Docker: ein Compose-Stack mit zwei Diensten und ein einzelner Container als Karte, beide mit Update-Hinweis" width="900"><br>
  Docker
</p>

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/apps.png" alt="Apps: Katalog mit Kategorien und das Installationspanel einer App"></td>
    <td width="50%"><img src="docs/screenshots/container-bearbeiten.png" alt="Container bearbeiten: Formular mit Image, Netzwerk, Ports und Variablen"></td>
  </tr>
  <tr>
    <td align="center">App-Store</td>
    <td align="center">Container bearbeiten</td>
  </tr>
</table>

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/vms.png" alt="VMs: Liste links, rechts die laufende VM mit ihrem Bildschirm im Browser"></td>
    <td width="50%"><img src="docs/screenshots/terminal.png" alt="Terminal-Panel am unteren Rand mit einer Shell auf dem NAS"></td>
  </tr>
  <tr>
    <td align="center">Virtuelle Maschinen</td>
    <td align="center">Terminal</td>
  </tr>
</table>

<p align="center">
  <img src="docs/screenshots/dateien.png" alt="Dateien: Freigaben links, Dateiliste und abgeschlossene Uploads" width="900"><br>
  Dateimanager
</p>

<p align="center">
  <img src="docs/screenshots/einrichtung.png" alt="Ersteinrichtung: Besitzerkonto anlegen" width="600"><br>
  Ersteinrichtung beim ersten Start
</p>

## Neu in v0.1.0-beta.3

- **Btrfs-Spiegel:** native RAID1-Spiegel aus genau zwei Platten, schreibgeschützte Snapshots, Wiederherstellung in einen neuen Ordner, Scrub und Ersatz eines fehlenden Mitglieds. [Btrfs-Anleitung](docs/BTRFS.md).
- **Geräteauswahl für Container:** iGPU/GPU, NPU, NVIDIA und Coral USB im App-Installationsdialog und Container-Editor; gespeichert werden stabile Gerätekennungen. [Hardware und Plugins](docs/PLUGINS.md).
- **Weitere Plugins:** Intel GPU Tools aus Debian und Coral-USB-Erkennung mit Diagnose. Die Coral-Laufzeit bringt die jeweilige Container-App mit; PCIe/M.2-Coral wird noch nicht unterstützt.
- **Plugins direkt von GitHub:** Die Plugin-Seite zeigt den signierten Katalog des laufenden Releases. Pakete werden erst beim Installieren auf das Daten-Volume geladen und vor der Aktivierung geprüft. Die manuelle Installation aus einer Freigabe bleibt möglich. [Plugin-Installation](docs/PLUGINS.md#installation-der-zusätzlichen-plugins).
- **NVIDIA:** Leistungslimit innerhalb der Kartengrenzen und optionaler Wechsel zwischen VM und zuvor laufenden GPU-Containern. [Bedienung und Grenzen](docs/PLUGINS.md#nvidia-zwischen-vm-und-containern).
- **Plugins bei Systemupdates:** signierte passende Versionen werden vor dem Slot-Wechsel vorbereitet. Dateien und Aktivierungseinstellungen bleiben je Systemversion getrennt, damit ein Rollback seine ursprünglichen Plugins erhält. Das gilt für Updates, die aus beta.3 oder einer späteren Version gestartet werden; der Updater in beta.2 kann das noch nicht.

## Noch geplant

Volume-Verschlüsselung, eingebauter Fernzugriff, Energiezeitpläne/Wake-on-LAN, Befehlspalette sowie dokumentierte API und CLI. Bootloader-Updates und Systemupdates aus einer hochgeladenen Datei stehen ebenfalls noch aus.

## Voraussetzungen und Grenzen der ersten Beta

- PC mit x86-64-Prozessor und UEFI; Secure Boot für das NAS ausgeschaltet. Legacy-BIOS und ARM werden nicht unterstützt. Secure Boot innerhalb einer VM ist davon unabhängig.
- Für erste Tests 4 GB RAM einplanen, zusätzlich RAM für Container und VMs. Das ist eine Testempfehlung, keine auf echter Hardware geprüfte Mindestanforderung.
- USB-Stick mit 8–32 GB, FAT32 und Label `TITANOS`; separate Datenplatten. Anlegen eines Volumes löscht die ausgewählten Platten. RAID ersetzt kein Backup.
- Kabelgebundenes Netzwerk; zunächst DHCP. Netzwerkänderungen sind anschließend in der Weboberfläche möglich. Firmware für ausgewählte Realtek-/Broadcom-Netzwerkkarten, QLogic-Adapter sowie Intel- und AMD-Grafik ist enthalten, einschließlich Intel GuC/HuC und NPU-Firmware. Unterstützung hängt zusätzlich vom Debian-Kernel und dem konkreten Gerät ab.
- Echte USVs, S3-/REST-Backupziele, GPU-Durchreichung und eine vollständige Windows-Installation sind noch nicht abschließend geprüft. Die Beta ist für Tests im vertrauenswürdigen lokalen Netz gedacht.
- Btrfs und automatische Plugin-Mitnahme sind ab beta.3 enthalten. Beim ersten Update aus beta.2 ein installiertes NVIDIA-Plugin vorher abschalten und danach passend zur neuen Version installieren; dessen alter Updater kann Plugins noch nicht vorbereiten. Volume-Verschlüsselung, Bootloader-Updates und Systemupdates aus einer Datei fehlen weiterhin. Let's Encrypt ist zurückgestellt; selbst signierte und importierte Zertifikate werden unterstützt.

## Installation der Beta

Downloads für `v0.1.0-beta.3` stehen auf der [Release-Seite](https://github.com/ra5on/TitanOS/releases/tag/v0.1.0-beta.3). Für einen neuen Stick:

1. `titanos-0.1.0-beta.3.zip`, dessen `.minisig` und `.sha256` laden. Signatur mit [minisign](https://jedisct1.github.io/minisign/) und einem vertrauenswürdig bezogenen öffentlichen [Release-Schlüssel](build/mkosi.extra/usr/share/titanos/update-keys/) prüfen; der Prüfablauf steht in [RELEASING.md](docs/RELEASING.md#downloads-prüfen). Die Prüfsumme allein bestätigt nicht die Herkunft.
2. Einen leeren USB-Stick als FAT32 mit dem Label `TITANOS` formatieren. Das ZIP direkt ins Stammverzeichnis entpacken: Dort müssen `EFI`, `loader`, `titanos` und `config` liegen, ohne zusätzlichen Unterordner.
3. Das NAS vom Stick starten (UEFI, Secure Boot aus). Die per DHCP vergebene IP steht auf der Konsole oder im Router. Im Browser `https://<nas-ip>` öffnen; das selbst signierte Zertifikat verursacht zunächst eine Browserwarnung.
4. Besitzerkonto anlegen, Sprache, Hostname und Zeitzone einstellen. Danach unter „Speicher“ ein Volume anlegen und unter „Shares“ eine Freigabe einrichten. Konfigurationssicherung und gegebenenfalls 2FA einrichten.

Weitere Betas werden unter „Einstellungen › Updates“ im Kanal **Beta** angeboten. Für ein Update einer vorhandenen Installation die Update-Seite verwenden; den Stick mit bestehender Konfiguration nicht neu formatieren.

Während ein neues Release noch gebaut wird, kann dessen Tag bereits im GitHub-Feed stehen. Die Beta-Update-Suche kann dann vorübergehend einen Abruffehler melden, weil das signierte Manifest noch fehlt. Nach der vollständigen Veröffentlichung erneut nach Updates suchen.

## Entwicklung

Voraussetzungen: Go 1.27+, Node.js 24+, Python 3.11+ und `make`.
Für das Systemabbild zusätzlich (Debian 13): mkosi **25.3**, `debian-archive-keyring squashfs-tools zstd zip systemd-container`. Für Release-Tests außerdem `minisign`; für den NVIDIA-Plugin-Bau `kmod`, Netzwerkzugang und einen isolierten Debian-Build als root bzw. in einem Benutzer-Namespace; für den QEMU-Test `qemu-system-x86 ovmf mtools fdisk`. Unprivilegierte Builds benötigen funktionierende Benutzer-Namespaces, `newuidmap`/`newgidmap` und Einträge in `/etc/subuid` und `/etc/subgid`. Alternativ baut `MKOSI_SUDO=1 make zip` das Abbild mit sudo; Weboberfläche und Backend werden weiterhin als normaler Benutzer gebaut.

| Befehl | Was er tut |
|---|---|
| `make build` | Baut die Weboberfläche und `backend/bin/titand` mit eingebetteter Oberfläche |
| `make test` | Führt die Go-Tests und die Typprüfung der Weboberfläche aus |
| `make test-release` | Prüft Verpackung, Lizenzinventar und Manipulationserkennung mit kleinen echten squashfs-Abbildern und Minisign-Schlüsseln |
| `make dev` | Startet `titand` im Entwicklungsmodus auf `127.0.0.1:8080` (ohne HTTPS, Konfiguration in `test/config/`) und den Vite-Server mit automatischem Neuladen |
| `make zip` | Baut das Systemabbild und packt den Stick-Inhalt: `out/stick/` und `out/titanos-<version>.zip` |
| `make zip-dev` | Dasselbe als Entwicklungsvariante, bei der root an der Konsole automatisch angemeldet ist |
| `make qemu` | Startet `out/stick/` in QEMU (UEFI) von einem emulierten USB-Stick; Weboberfläche unter `https://127.0.0.1:8443`. Mit `DISKS=4 make qemu` kommen vier leere Testplatten dazu. |

Das Backend (`backend/`, Go) und die Weboberfläche (`web/`, Svelte + TypeScript) liegen in diesem Repository. In `backend/` funktionieren `go build ./cmd/titand` und `go test ./...` ohne Node.js; ohne das Build-Tag `embedui` wird eine Platzhalterseite statt der Weboberfläche ausgeliefert. Release-Bau und Signierung sind in [RELEASING.md](docs/RELEASING.md) beschrieben.

## Lizenz

TitanOS ist **Fair Source** unter der [Functional Source License 1.1 (FSL-1.1-ALv2)](LICENSE.md):

- Nutzen, Ändern und Weitergeben ist frei – privat, im Unternehmen, für Lehre und Forschung.
- Nicht erlaubt: TitanOS oder ein wesentlich darauf beruhendes Produkt als konkurrierendes kommerzielles Produkt oder als Dienst anzubieten.
- Jede Version steht zwei Jahre nach ihrem Erscheinen unter der **Apache License 2.0**.

Maßgeblich ist der Lizenztext in [LICENSE.md](LICENSE.md). Fremdkomponenten (Linux-Kernel, Debian-Pakete und andere) behalten ihre eigenen Lizenzen, siehe [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES).

## Mitmachen

Beiträge sind willkommen – bitte lies zuerst [CONTRIBUTING.md](CONTRIBUTING.md) (auf Englisch). Sicherheitslücken bitte nicht öffentlich melden, sondern wie in [SECURITY.md](SECURITY.md) beschrieben.

### Entwicklungsstand nach Beta 5

- Getrennte CPU-/RAM-/Systemkacheln, kleinere VM-/Containerkacheln und Einstellungen für Kachelbreite, Höhe und Zusatzwerte; IPv6 im Netzwerk-Dashboard. [Monitoring](docs/MONITORING.md).
- Einstellungen für den physischen [Ein-/Ausschalter](docs/POWER-BUTTON.md).
- [Tailscale-Plugin](docs/TAILSCALE.md) mit Subnet-Routing und beiden Exit-Node-Rollen.
- Öffentliche [NVIDIA-Treiber auf dem NAS vorbereiten](docs/NVIDIA-LOCAL-BUILD.md), anschließend ausdrücklich beim Neustart aktivieren.
