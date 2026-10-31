# Start vom Stick: UEFI, 32-Bit-UEFI und Legacy-BIOS

Stand 31.10.2026 (Entscheidung des Projektinhabers zu Punkt 5c/5d). Derselbe FAT32-Stick startet auf drei Arten:

| Rechner | Bootloader | Datei auf dem Stick |
|---|---|---|
| 64-Bit-UEFI (Normalfall) | systemd-boot | `EFI/BOOT/BOOTX64.EFI` |
| 32-Bit-UEFI mit 64-Bit-CPU (z. B. Atom „Bay Trail“) | GRUB `i386-efi` | `EFI/BOOT/BOOTIA32.EFI` |
| Legacy-BIOS ohne UEFI (z. B. HP MicroServer N40L/N54L) | GRUB `i386-pc` | MBR + Sektoren vor der ersten Partition, Menü in `boot/grub/` |

Beide GRUB-Varianten lesen `boot/grub/grub.cfg` (Vorlage `boot/grub.cfg.in`) und starten denselben 64-Bit-Kernel aus `titanos/<slot>/`. Der 64-Bit-Kernel von Debian startet unter 32-Bit-UEFI im „mixed mode“ (`CONFIG_EFI_MIXED`).

## A/B-Regeln unter GRUB

GRUB liest dieselben Einträge in `loader/entries/`, die titand für systemd-boot schreibt:

- `titanos-<slot>+<übrig>[-<verbraucht>].conf` ist auf Probe. Ein solcher Slot mit übrigen Versuchen startet zuerst.
- `titanos-<slot>.conf` ist als gut markiert. Ohne Probe startet der gute Slot aus `titanos_prefer`.
- `+0-…` ist aufgegeben. Ohne startbaren Eintrag wartet das Menü auf eine Wahl.

GRUB kann auf FAT keine Dateien umbenennen. Es zählt die Versuche deshalb in `boot/grub/grubenv`, einem 1024-Byte-Block, den `save_env` an Ort und Stelle überschreibt (`titanos_trial` ist der Name des Eintrags, `titanos_count` die Zahl der Starts). Kann GRUB das System nicht laden, startet es neu, damit die Zählung weiterläuft. Nach drei Fehlstarts startet der andere Slot.

titand (ab 0.41, `backend/internal/update/grub.go`) gleicht beim Start ab, wenn die Kernel-Kommandozeile `titanos.loader=grub-…` trägt:

- Die gezählten Versuche werden in den Dateinamen geschrieben, so wie systemd-boot ihn umbenennt (`+3` → `+2-1`). Danach wird die Zählung geleert. Ein von GRUB aufgegebener Versuch wird zu `+0-3` und zeigt sich damit als fehlgeschlagen. Eine neue Probe desselben Slots bekommt dann wieder alle drei Versuche.
- Bei jeder neuen Probe leert titand die Zählung. `titanos_prefer` folgt dem `sort-key` des guten Eintrags (Update, Rückwechsel, „Doch nicht“, Markieren als gut).
- Die Kernel-Kommandozeile eines Slots (einschließlich der Kernelparameter aus den Einstellungen) liegt für GRUB in `titanos/<slot>/bootenv`. Fehlt sie, nimmt GRUB die Kommandozeile, mit der die Sticks gebaut wurden.
- Der Boot-Check bleibt unverändert: `titand -check-boot` wartet auf `/api/v1/health` und benennt den Eintrag in `titanos-<slot>.conf` um. Es gibt kein `systemd-bless-boot`.

Sticks ohne `boot/grub/grubenv` (vor 0.41 erstellt) bleiben unverändert; titand schreibt dort keine GRUB-Dateien.

## Updates und ältere Versionen

- Updates ersetzen nie `EFI/BOOT/BOOTX64.EFI`, `EFI/BOOT/BOOTIA32.EFI` oder `boot/grub/`. Sie schreiben nur `titanos/<slot>/` und `loader/entries/` (Test `TestNoGrubWork`; die Manifestprüfung lehnt fremde Pfade ab). `grub.cfg` ist deshalb so gebaut, dass alles, was sich mit einem Release ändert, im Slot liegt (`bootenv`).
- Ein alter Stick bekommt GRUB nicht über ein Update. Dafür den Stick neu aus dem ZIP oder dem Stick-Abbild anlegen. Die Konfiguration kommt über die Stick-Sicherung zurück.
- Läuft nach einem Rückwechsel ein System vor 0.41 und wurde es von GRUB gestartet, zählt GRUB weiterhin selbst und fällt zurück. Was fehlt: Das alte titand schreibt die Zählung nicht in den Namen. Ein von GRUB aufgegebenes Update erscheint dort also nicht als „fehlgeschlagen“, und eine erneute Installation desselben Updates aus diesem alten System startet nicht noch einmal auf Probe. Der alte Slot bleibt in beiden Fällen der, der startet. Umgekehrt merkt sich GRUB den bevorzugten Slot selbst (`titanos_prefer` beim Start einer Probe). Wird ein solches altes System über systemd-boot gestartet und markiert, und kommt der Stick danach an ein BIOS-Board, kann GRUB beim ersten Start den anderen guten Slot wählen.

## BIOS-Start einrichten

Ein Stick aus dem ZIP startet unter 64-Bit- und 32-Bit-UEFI sofort. Für ein Legacy-BIOS braucht er zusätzlich Startcode im MBR. Zwei Wege:

1. **Stick-Abbild** `titanos-<version>.img.xz` aus dem Release mit balenaEtcher, Rufus oder `dd` schreiben. Das Abbild hat eine MBR-Partitionstabelle mit einer 4-GiB-FAT32-Partition `TITANOS` ab 1 MiB und GRUB im MBR. Es startet unter BIOS und UEFI. Der Rest eines größeren Sticks bleibt ungenutzt. Erzeugt wird es von `scripts/make-stick-image.py` (ohne root, mit sfdisk und mtools).
2. **Vorhandener Stick** (Linux, als root, Stick ausgehängt): `python3 /pfad/zum/stick/boot/grub/stick-bios.py /dev/sdX`. Das Skript schreibt nur den Startcode des MBR (Partitionstabelle und Datenträgerkennung bleiben) und GRUB in die freien Sektoren vor der ersten Partition. Fehlt eine aktive Partition, wird die erste aktiv gesetzt. Es lehnt ab: GPT, Sticks ohne Partitionstabelle, eine erste Partition, die vor dem Ende von GRUB beginnt (GRUB belegt etwa 300 Sektoren), und belegte Sektoren vor der Partition.

`scripts/make-grub.sh` baut beide GRUB-Abbilder im Image-Build (Debian-Pakete `grub-pc-bin` und `grub-efi-ia32-bin`, `mkosi.postinst.chroot`). Alle nötigen Module und `boot/grub-early.cfg` sind eingebettet; der Stick braucht kein Modulverzeichnis. Die Abbilder liegen auch im System unter `/usr/share/titanos/boot/`. Die GRUB-Quellen stehen über die Debian-Paketliste in `titanos-<version>-sources.txt`.

Eigene Ergänzungen der Kernel-Kommandozeile nur für diesen Stick (z. B. serielle Konsole) gehören in `boot/grub/custom.cfg`: `set titanos_extra="console=ttyS0,115200"`. Diese Datei setzt TitanOS nie selbst.

## Geprüft am 31.10.2026 und Grenzen

- QEMU mit KVM, SeaBIOS auf `-machine pc`, Stick als USB-Gerät, gebaut mit `make-stick-image.py` aus dem Image 0.32.0 und den GRUB-Abbildern dieses Stands (Debian GRUB 2.12-9+deb13u2):
  - Start von Slot A bis zur Anmeldung.
  - Slot B auf Probe mit nicht ladbarem Kernel: drei Versuche mit Neustart, danach Slot A. Mit neuem titand auf A: Eintrag `titanos-b+0-3.conf`, `titanos_prefer=a`.
  - Slot B auf Probe mit gutem System: Start von B. Mit altem titand 0.32 und mit neuem titand: Boot-Check, Eintrag `titanos-b.conf`, `titanos_prefer=b`. Mit neuem titand ist die Zählung danach geleert.
- QEMU mit KVM, 32-Bit-OVMF (`ovmf-ia32` 2025.02) auf q35: `BOOTIA32.EFI` startet den 64-Bit-Kernel bis zur Anmeldung (`titanos.loader=grub-i386-efi`).
- Go-Tests (`grub_test.go`) für Zählung, Abgleich, `bootenv` und Kernelparameter; Python-Tests (`scripts/tests/test_boot.py`) für MBR-Installation, Ablehnungen und das Stick-Abbild.
- `release.yml` bootet das Release-ZIP zusätzlich mit SeaBIOS und 32-Bit-OVMF (`smoke-release.py --firmware bios|ia32`). Dieser CI-Schritt ist nur lokal vorbereitet, noch nicht in CI gelaufen.
- **Nicht geprüft:** echte Hardware (N40L/N54L, Bay-Trail-Boards). BIOS-Eigenheiten beim USB-Start (Diskettenemulation, USB-HDD vs. USB-ZIP) sind nur über die Laufwerksnummer-Korrektur wie bei `grub-bios-setup` abgedeckt. Der vollständige Image-Build mit den neuen Paketen (`make zip`) lief hier nicht, weil er root braucht.

## 32-Bit-UEFI: `efi=noruntime`

Auf 32-Bit-UEFI startet GRUB den 64-Bit-Kernel im gemischten Modus. Aufrufe der EFI-Laufzeitdienste der 32-Bit-Firmware brachten den Kernel im CI-Rauchtest (QEMU/TCG mit OVMF ia32 aus Ubuntu, 31.10.2026) früh zum Absturz („kernel tried to execute NX-protected page“). `boot/grub.cfg.in` hängt deshalb auf diesem Weg fest `efi=noruntime` an (Variable `titanos_platform`, die `custom.cfg` nicht überschreibt). TitanOS braucht dort keine EFI-Variablen: GRUB führt Versuche und bevorzugten Slot in `grubenv`. Das ist eine feste Einstellung des Sticks für diesen Startweg, kein zur Laufzeit gesetzter Parameter. 64-Bit-UEFI (systemd-boot) und Legacy-BIOS sind nicht betroffen.
