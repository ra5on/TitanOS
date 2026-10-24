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
megaraid_sas, NVMe, AHCI, UAS, xHCI, Virtio, VFIO, KVM Intel/AMD und
coretemp/k10temp. Built-ins zählen ebenfalls. Das verhindert versehentliches
Weglassen einer Treiberfamilie; nicht jeder Adapter benötigt zusätzliche
Firmware. Unterstützung einzelner PCI-/USB-IDs hängt weiterhin vom Kernel ab.

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
