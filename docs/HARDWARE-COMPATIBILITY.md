# Hardwarebasis und Prüfgrenzen

TitanOS bleibt Debian 13/Trixie auf amd64, UEFI und dem stabilen Debian-Kernel.
Die Auswahl zielt auf NAS-Hardware; sie ist keine Garantie für beliebige Geräte.

## CPU, Grafik und Beschleuniger

`intel-microcode` und `amd64-microcode` sind Bestandteil des Grundsystems.
Die Debian-Hooks werden auf `early` gestellt, bei Intel zusätzlich
`IUCODE_TOOL_SCANCPUS=no`. Damit enthält das auf Intel oder AMD gebaute ZIP
Microcode für beide Hersteller. `scripts/check-image-hardware.py` prüft vor dem
Packen die tatsächlichen unkomprimierten frühen CPIO-Archive auf nichtleere
`GenuineIntel.bin` und `AuthenticAMD.bin`; eine Datei erst im komprimierten
Haupt-Initramfs genügt nicht. Der Kernel entscheidet beim Boot, ob ein passendes
neueres Update angewendet werden kann. Enthaltene Dateien beweisen keine
Aktualisierung auf einem bestimmten physischen Prozessor.

Intel-i915/xe, Intel-NPU (`intel_vpu`) und AMD-Grafik (`amdgpu`) bleiben
Kernelbestandteile. Intel-Grafik-/NPU- und AMD-Grafikfirmware werden gezielt
installiert. Der Build prüft GuC, HuC, DMC, Xe-GuC, Intel-VPU und AMDGPU-Dateien
sowie die Module oder ihre Built-in-Einträge. Container bringen ihre passenden
Anwendungsbibliotheken mit und bekommen Geräte nur nach ausdrücklicher Auswahl.

NVIDIA bleibt ein optionales signiertes Plugin für **Turing und neuer**. Die
vorhandenen exakten Kernel-/Versionsprüfungen, GSP-Firmware, Container Toolkit,
CDI, lokaler Treiberbau und Rückfall bleiben erhalten. Das neue Systemrelease
baut Plugin und NAS-Bauumgebung gegen denselben Kernel. Siehe
[NVIDIA](NVIDIA.md) und [lokaler Treiberbau](NVIDIA-LOCAL-BUILD.md).

## Netzwerk und Datenträger

Gezielte Firmwarepakete: Realtek, Broadcom bnx2/bnx2x, QLogic, NetXen, Myricom,
Netronome, Cavium, `firmware-linux-free` und `firmware-misc-nonfree`. Kein
Firmware-Sammelmetapaket, keine automatische Herstellerinstallation auf dem NAS.

Der Build verlangt die Kernelmodule für Intel e1000e/igb/igc/ixgbe/i40e/ice,
Realtek r8169, Broadcom tg3/bnx2/bnx2x, QLogic qla2xxx, mpt3sas,
megaraid_sas, aacraid, hpsa, NVMe/NVMe-Core, Intel VMD, AHCI, ata_piix,
sata_mv, sata_sil24, SCSI/sd_mod, USB-Storage/UAS, xHCI, Virtio, VFIO, KVM Intel/AMD und
coretemp/k10temp. Built-ins zählen ebenfalls. Das verhindert versehentliches
Weglassen einer Treiberfamilie; nicht jeder Adapter benötigt zusätzliche
Firmware. Unterstützung einzelner PCI-/USB-IDs hängt weiterhin vom Kernel ab.

### Treiber, die Debians Kernel 6.12 nicht baut: Treiber-Plugins

Eine Prüfung des 0.30.0-Abbilds ergab, dass Debians Kernelkonfiguration einige
für NAS-Hardware relevante Treiber abschaltet. Sie kommen nicht ins
Grundsystem, sondern als optionale, signierte Plugins, damit nur installiert
wird, was die Hardware braucht (Entscheidung des Projektinhabers). Jedes
volle Release baut sie gegen genau seinen Kernel; Bau, Prüfungen und Grenzen:
[Plugins › Treiber-Plugins](PLUGINS.md#treiber-plugins).

| Plugin | Treiber | Hardware | Stand |
| --- | --- | --- | --- |
| `wangxun-nic` | `ngbe`, `txgbe` (`libwx`) | Wangxun WX1860 (1 GbE, 8088:0100–010b), SP1000/WX1820 (10 GbE, 8088:1001/2001) | baut ohne Warnungen, keine Firmware nötig; Hardware ungeprüft |
| `tehuti-tn40` | `tn40xx` | Tehuti TN4010 (1fc9:4022/4025, nur die Subsystem-IDs der Treibertabelle) | eingeschränkt: QT2025-Karten brauchen den Rust-PHY-Treiber `AMCC_QT2025_PHY`, den Debian nicht baut; AQR105-Karten ggf. PHY-Firmware `tehuti/aqr105-tn40xx.cld`, die nicht in linux-firmware liegt |
| `intel-qat` | `qat_4xxx`, `qat_420xx` | Intel QAT Gen 4 (8086:4940/4942/4944/4946) | Firmware `qat_4xxx*.bin` aus linux-firmware (Debian liefert sie nicht mit), `qat_402xx`/`qat_420xx` bereits in `firmware-intel-misc`; TitanOS nutzt QAT nicht gezielt |
| `it87-extended` | `it87` von frankcrawford/it87 (fester Commit) | ITE IT8606E, IT8607E, IT8613E, IT8625E, IT8655E, IT8665E, IT8686E, IT8688E, IT8689E, IT8696E, IT8698E, IT8736F, IT8738E, IT8785E zusätzlich zum it87 des Kernels | ersetzt den System-it87; ACPI-Konflikte werden nicht umgangen |

Der it87 von Linux 6.12 kennt IT87952E bereits; dafür ist das Plugin nicht
nötig. Welche Plugins zur vorhandenen Hardware passen, zeigt die Plugin-Seite
unter **Für deine Hardware empfohlen** (Erkennung über die Modaliase der Geräte,
nur Vorschlag, keine automatische Installation).

### Was erst ein neuerer Kernel als 6.12 kann

Geprüft an den Quellen von Linux 6.12.113 (Debian) und den Tags v6.13 bis v6.18
von torvalds/linux. Ein Kernelwechsel ist eine Entscheidung des
Projektinhabers; TitanOS bleibt beim Debian-Kernel.

| Hardware | Treiber | erst ab |
| --- | --- | --- |
| Realtek RTL8127A (10 GbE) | `r8169` | Linux 6.16 |
| Realtek RTL8125D, zweite Chipversion (XID 0x689), und RTL8125BP | `r8169` | Linux 6.14 (die erste RTL8125D-Version, XID 0x688, und RTL8126A kann 6.12 bereits) |
| Intel Ethernet E610 | `ixgbe` | Linux 6.14 |
| Wangxun AML (10/25 GbE, im Treiber AML5010 bis AML5140) | `txgbe` | Linux 6.15 |
| AMCC QT2025 PHY (für Tehuti-Karten) | `qt2025` (Rust) | seit 6.12 im Kernel, aber nur mit Rust-Unterstützung, die Debians Kernel nicht aktiviert |

Debian-Stable-Updates können einzelne Geräte-IDs zurückportieren; die Tabelle
beschreibt den Stand 6.12.113.

Zusätzlich prüft das Abbild die Treiber für NTFS3, FUSE, exFAT, FAT, ext4,
XFS und Btrfs. `ntfs-3g` und `exfatprogs` gehören zur Systembasis. Ein Fehler beim
Einbinden von NTFS3 versucht NTFS-3G ausschließlich lesend; der Dateimanager
zeigt das an und die API verweigert Schreibaktionen. Kein `force`, `ntfsfix`
oder Entfernen eines Windows-Ruhezustands. Schlägt auch der lesende Weg fehl,
bleiben die Treiberfehler im NAS-Protokoll erhalten. Grundlagen:
[NTFS3](https://docs.kernel.org/filesystems/ntfs3.html),
[NTFS-3G](https://manpages.debian.org/trixie/ntfs-3g/ntfs-3g.8.en.html).

Der Dateimanager entdeckt auch interne nicht verwaltete Dateisysteme; ein
unbekannter oder nicht einbindbarer Datenträger wird dadurch nicht automatisch
zu einem TitanOS-RAID-Volume. Dateien zuerst auf ein TitanOS-Volume kopieren;
eine spätere Neuformatierung löscht die vorhandenen Daten. Es gibt keine
verlustfreie automatische Umwandlung von NTFS in ein NAS-Dateisystem.

Beim Abbildbau entfernt `normalize-image-tmpfiles.py` doppelte Erstellregeln
für `/run/lock` und `/run/utmp`, erhält dabei die zuerst wirksame Regel samt
Debian-Rechten sowie alle anderen Regeln und Herstellerhinweise. Hintergründe:
[Debian-Fehlerbericht zur Doppeldefinition](https://bugs.debian.org/1072105).
Die Journalansicht filtert keine Warnungen weg. Meldungen wie „Volume was not
properly unmounted“ oder „Start/Stop Unit failed / DID_BAD_TARGET“ benötigen die
Prüfung des betroffenen Sticks, Laufwerks beziehungsweise Controllers; diese
Änderung repariert keine beschädigten Datenträger und bestätigt keine Behebung
dieser konkreten Hardwaremeldungen.

## Abnahme

Die Python-Regressionen prüfen erfolgreiche Images, beide Microcode-Hersteller,
getrennte frühe Archive, leere/abgeschnittene Archive und fehlende Firmware/Module.
Der Release-Build prüft zusätzlich sein echtes Initramfs und Dateisystem.
QEMU testet ZIP-Boot und die bestehenden Speicher-/Netzwerk-/Pluginabläufe.

Echte Intel-/AMD-Microcode-Aktivierung, Intel-NPU, CUDA/NVENC sowie physische
Netzwerk-/HBA-/USB-Adapter sind separat auf konkreter Hardware zu prüfen.
Die Treiber-Plugins sind gebaut und gegen den Modulindex des Abbilds
aufgelöst (Abhängigkeiten, `vermagic`, Aliase); im QEMU-Storage-Test laden
`wangxun-nic`, `intel-qat` und `tehuti-tn40` ihre Module ohne passende
Hardware. Mit echten Karten, QAT-Geräten oder ITE-Chips sind sie nicht geprüft.
Keine neue pauschale Hardwareabnahme und keine Unterstützung älterer
NVIDIA-Karten werden aus diesen Tests abgeleitet. Abbildgrößen und konkrete
QEMU-Messungen gehören zum jeweiligen Releasebericht.

Grundlagen: [Debian-Microcode](https://wiki.debian.org/Microcode),
[frühes Laden im Kernel](https://docs.kernel.org/arch/x86/microcode.html),
[NVIDIA-Modulunterstützung](https://github.com/NVIDIA/open-gpu-kernel-modules).

Die Erweiterung nach 0.21.0 ist mit isolierten Gerätebäumen für SATA, SAS, NVMe,
fehlende Transportangaben, belegte Dateisysteme, Bootstick-Partitionen und VM-
Platten geprüft. NTFS-Ausweichbefehle und Schreibschutz-Flags verwenden
simulierte Mounttabellen; kein echter NTFS-Mount in dieser Prüfung. Der neue
Systemabbildbau und echte HDD-/SSD-/NVMe-/Controller-Tests stehen noch aus.
Die zusätzlichen Pakete/Build-Prüfungen benötigen ein volles Systemrelease.
