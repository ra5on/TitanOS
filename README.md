<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/titanos-wordmark-dark-bg.svg">
    <img src="docs/brand/titanos-wordmark-light-bg.svg" alt="TitanOS" width="420">
  </picture>
</p>

<p align="center"><strong>Ein schlankes NAS-Betriebssystem, das vom USB-Stick bootet und komplett im RAM läuft.</strong></p>

---

> **Stand:** frühe Entwicklung – es gibt noch nichts zum Installieren. Das System bootet, die Weboberfläche läuft; Speicher, Freigaben, Docker und VMs folgen. Wer mitlesen will, beobachtet das Repository bis zum ersten Release.

<p align="center">
  <img src="docs/screenshots/dashboard.png" alt="Dashboard von TitanOS mit den Kacheln System, Volumes und Netzwerk" width="900">
</p>

## Was TitanOS ist

TitanOS macht aus einem PC ein NAS. Das System liegt auf einem USB-Stick, wird beim Start komplett in den Arbeitsspeicher geladen und läuft von dort. Der Stick wird danach nur noch angefasst, wenn sich die Konfiguration ändert oder ein Update kommt. Bedient wird alles im Browser.

Die Idee ist von Unraid inspiriert, setzt aber auf klassisches RAID. TitanOS ist ein eigenständiges Projekt und steht in keiner Verbindung zu Lime Technology. Es basiert auf Debian.

## Was schon funktioniert

- **Start vom Stick, Betrieb im RAM.** Das Systemabbild (146 MB) wird beim Booten in den Arbeitsspeicher kopiert, darüber liegt eine auf 512 MB begrenzte Schreibschicht. Nach dem Start belegt das Grundsystem rund 0,5 GB RAM.
- **Konfiguration auf dem Stick.** Alles Dauerhafte liegt im Ordner `config` auf dem Stick. Der Stick wird nur bei Bedarf eingebunden und nach 30 Sekunden ohne Zugriff wieder ausgehängt – ein Stromausfall trifft ihn so fast nie im eingebundenen Zustand.
- **Ersteinrichtung und Anmeldung.** Beim ersten Start legst du das Besitzerkonto mit frei gewähltem Namen an. Es gibt kein festes „admin“ und keinen Login als root. Wiederholte Fehlversuche werden ausgebremst.
- **HTTPS von Anfang an.** Die Weboberfläche läuft über HTTPS mit einem selbstsignierten Zertifikat, das über Neustarts gleich bleibt.
- **Dashboard mit frei anordenbaren Kacheln.** Kacheln rasten in einem Raster aus 12 Spalten ein, lassen sich verschieben, in der Größe ändern und ausblenden. Das Layout wird pro Konto gespeichert.
- **Darstellung nach Wunsch.** Themes, Akzentfarben, Glas-Effekt, Eckenradius, Schriftgröße, Dichte und Hintergrund, mit Vorschau und Export als Datei.
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

<p align="center">
  <img src="docs/screenshots/einrichtung.png" alt="Ersteinrichtung: Besitzerkonto anlegen" width="600"><br>
  Ersteinrichtung beim ersten Start
</p>

## Was geplant ist

- **Klassisches RAID.** RAID 0, 1, 5, 6 und 10 über Linux-Software-RAID (mdadm) mit ext4 oder XFS; optional Btrfs-Spiegel mit Snapshots.
- **Freigaben und Dateien.** SMB- und NFS-Freigaben mit Rechten pro Konto, Papierkorb und Time-Machine-Ziel; dazu ein Dateimanager im Browser mit Hochladen per Drag-and-drop.
- **Docker mit App-Store.** Eigene App-Quellen; Vorlagen füllen Variablen, Pfade und Ports von selbst aus. Jeder Container lässt sich als Formular oder als Compose-Datei bearbeiten.
- **Virtuelle Maschinen.** KVM/QEMU mit UEFI, Durchreichen von GPU und iGPU, NAS-Ordner in der VM per virtiofs, Konsole im Browser.
- **Hardware im Blick.** Erkennt Intel-iGPU, Intel-NPU und NVIDIA-GPUs und lässt dich festlegen, ob Container oder eine VM sie nutzen. NVIDIA-Treiber als Plugin.
- **Sichere Updates.** Signierte Updates aus den GitHub-Releases in einen zweiten System-Slot auf dem Stick, mit automatischer Rückkehr zur alten Version, falls die neue nicht startet.

## Installation (geplant)

1. USB-Stick (8–32 GB) als FAT32 mit dem Namen `TITANOS` formatieren.
2. Das neueste `titanos-x.y.z.zip` aus den [Releases](https://github.com/ra5on/TitanOS/releases) laden, Prüfsumme vergleichen und auf den Stick entpacken.
3. Das NAS vom Stick starten (UEFI, Secure Boot aus) und im Browser `https://<nas-ip>` öffnen. Das Zertifikat ist selbstsigniert, der Browser fragt deshalb einmal nach einer Bestätigung.

## Entwicklung

Voraussetzungen: Go 1.27+, Node.js 24+ und `make`.
Für das Systemabbild zusätzlich (Debian 13): `mkosi squashfs-tools mtools zip systemd-boot-efi systemd-container`, zum Testen `qemu-system-x86 ovmf`. Root-Rechte sind nicht nötig.

| Befehl | Was er tut |
|---|---|
| `make build` | Baut die Weboberfläche und `backend/bin/titand` mit eingebetteter Oberfläche |
| `make test` | Führt die Go-Tests und die Typprüfung der Weboberfläche aus |
| `make dev` | Startet `titand` im Entwicklungsmodus auf `127.0.0.1:8080` (ohne HTTPS, Konfiguration in `test/config/`) und den Vite-Server mit automatischem Neuladen |
| `make zip` | Baut das Systemabbild und packt den Stick-Inhalt: `out/stick/` und `out/titanos-<version>.zip` |
| `make zip-dev` | Dasselbe als Entwicklungsvariante, bei der root an der Konsole automatisch angemeldet ist |
| `make qemu` | Startet `out/stick/` in QEMU (UEFI) von einem emulierten USB-Stick; Weboberfläche unter `https://127.0.0.1:8443` |

Das Backend (`backend/`, Go) und die Weboberfläche (`web/`, Svelte + TypeScript) liegen in diesem Repository. Ein einfaches `go build` oder `go test` funktioniert ohne Node.js und enthält statt der Weboberfläche eine Platzhalterseite.

## Lizenz

TitanOS ist **Fair Source** unter der [Functional Source License 1.1 (FSL-1.1-ALv2)](LICENSE.md):

- Nutzen, Ändern und Weitergeben ist frei – privat, im Unternehmen, für Lehre und Forschung.
- Nicht erlaubt: TitanOS oder ein wesentlich darauf beruhendes Produkt als konkurrierendes kommerzielles Produkt oder als Dienst anzubieten.
- Jede Version steht zwei Jahre nach ihrem Erscheinen unter der **Apache License 2.0**.

Maßgeblich ist der Lizenztext in [LICENSE.md](LICENSE.md). Fremdkomponenten (Linux-Kernel, Debian-Pakete und andere) behalten ihre eigenen Lizenzen, siehe [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES).

## Mitmachen

Beiträge sind willkommen – bitte lies zuerst [CONTRIBUTING.md](CONTRIBUTING.md) (auf Englisch). Sicherheitslücken bitte nicht öffentlich melden, sondern wie in [SECURITY.md](SECURITY.md) beschrieben.
