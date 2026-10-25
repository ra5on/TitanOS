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
