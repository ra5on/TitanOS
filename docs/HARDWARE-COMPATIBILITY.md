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

### Intel-Grafik Gen9 bis Gen11: GuC/HuC laden

Betrifft Intel-Grafik der 6. bis 10. Core-Generation (Skylake, Kaby Lake,
Coffee/Whiskey Lake, Comet Lake), Apollo Lake und Gemini Lake (Gen9/Gen9.5),
Ice Lake, Jasper Lake und Elkhart Lake (Gen11) sowie Tiger und Rocket Lake.
Der i915-Treiber von Linux 6.12 lädt dort mit `enable_guc=-1` (automatisch)
weder GuC noch HuC: `uc_expand_default_options` setzt den Wert vor Gen12 und
auf Tiger/Rocket Lake auf 0. Erst ab Alder Lake lädt der Kernel HuC
(Alder Lake-S) bzw. HuC und GuC-Submission (Alder Lake-P, DG2 und neuer) von
selbst; Xe2 und neuer laufen mit `xe`, wo GuC immer aktiv ist. Ohne HuC fehlt
die Low-Power-Encodierung (VDEnc) mit Bitratensteuerung, die Jellyfin, Plex
und andere über VA-API/QSV nutzen. Jasper und Elkhart Lake encodieren nur im
Low-Power-Modus.

**Einstellungen › Hardware › Intel-Grafik: GuC/HuC** bietet drei Modi:

| Modus | Kernelparameter | Wirkung |
|---|---|---|
| Automatisch (Standard) | keiner | Kernel-Standard, GuC und HuC bleiben aus |
| HuC laden | `i915.enable_guc=2` | GuC lädt und authentifiziert die HuC; Aufgaben verteilt weiter der Treiber. Von Jellyfin für diese Generationen empfohlen |
| GuC-Submission und HuC | `i915.enable_guc=3` | zusätzlich verteilt die GuC die Aufgaben. Nicht Kernel-Standard, auf diesen Generationen nicht empfohlen, kann instabil sein; mit einfachem Bestätigungsdialog |

GuC-Submission gibt es erst ab Gen11 (`__guc_submission_supported`:
`GRAPHICS_VER >= 11`). Auf Gen9/Gen9.5 meldet der Kernel bei `=3`
„Incompatible option enable_guc=3 - GuC submission is N/A“ und lädt nur die
HuC; die Seite weist darauf hin.

**Mechanismus:** Der Parameter steht in der `options`-Zeile der Boot-Einträge
auf dem Stick, nicht in `modprobe.d`. `i915.ko` und die GuC/HuC-Firmware
liegen im Initramfs (am Stick-Abbild 0.23.3 mit `lsinitramfs` geprüft:
`kernel/drivers/gpu/drm/i915/i915.ko.xz`, `firmware/i915/skl_huc_2.0.0.bin`
usw.), udev lädt i915 dort beim ersten Gerätescan. Eine Datei, die titand im
laufenden System erzeugt, käme zu spät; eine Datei im Abbild wäre für alle
NAS gleich. `module.param=` auf der Kernel-Kommandozeile gilt dagegen, egal ob
das Modul im Initramfs oder später geladen wird.

- Die Einstellung liegt in `/config/system/intel-gpu.json` (`{"mode":"auto|huc|guc"}`).
  Fehlt die Datei, fasst TitanOS die Boot-Einträge nicht an; ein von Hand
  gesetzter Wert bleibt dann stehen und wird angezeigt.
- Beim Speichern schreibt titand `i915.enable_guc` in alle Einträge
  (`titanos-a.conf`, `titanos-b.conf`, auch einen wartenden `titanos-b+3.conf`
  mit erhaltenem Zähler) und nimmt dabei nur frühere Formen desselben
  Parameters heraus; alle anderen Parameter bleiben in ihrer Reihenfolge.
  Unveränderte Einträge werden nicht geschrieben. „Automatisch“ entfernt den
  Parameter wieder.
- Volle Updates schreiben den neuen Eintrag aus der Slot-Kommandozeile des
  Manifests und hängen den Parameter an; ein Rückwechsel übernimmt ihn ebenso.
  Beim Start gleicht titand die Einträge einmal mit der Einstellung ab (etwa
  nachdem eine ältere Version ein Update ohne den Parameter installiert hat).
- Der Eintrag des laufenden Systems wird nicht geschrieben, solange er auf
  Probe ist: `titand -check-boot` benennt ihn um. titand versucht es dann
  höchstens 20 Minuten lang alle 30 Sekunden erneut. Während ein Update den
  Stick beschreibt, lehnt die Seite die Änderung ab (`update_busy`).
- Wirksam nach dem nächsten Neustart; die Seite bietet ihn an. Die Änderung
  erscheint im Aktivitätsprotokoll.
- Die Seite zeigt je Intel-Grafik Plattform/Generation (aus der PCI-ID nach
  `include/drm/intel/i915_pciids.h` von Linux 6.12), Treiber, Render-Knoten,
  ob die passenden Dateien in `/usr/lib/firmware/i915` liegen, den Wert auf
  der laufenden Kommandozeile und in `/sys/module/i915/parameters/enable_guc`
  und den Zustand aus debugfs (`/sys/kernel/debug/dri/<minor>/gt0/uc/huc_info`,
  `guc_info`, `usage`). Ohne lesbares debugfs dienen die Kernelmeldungen
  dieses Starts als Ersatz („GuC firmware … version …“, „HuC: authenticated
  for all workloads“, „GUC: submission disabled“). Das Diagnosepaket enthält
  denselben Stand als `intel-gpu.json`.

Erwartete Firmware (Linux 6.12, `intel_uc_fw.c`): Skylake `skl_guc_70.1.1.bin`/`skl_huc_2.0.0.bin`,
Apollo Lake `bxt_*` (HuC 2.0.0), Kaby/Coffee/Whiskey/Comet Lake `kbl_*` (Comet
Lake ab Revision 5 `cml_*`, HuC 4.0.0), Gemini Lake `glk_*` (HuC 4.0.0), Ice
Lake `icl_*` (HuC 9.0.0), Jasper/Elkhart Lake `ehl_*` (HuC 9.0.0), Tiger/Rocket
Lake `tgl_guc_70.1.1.bin`/`tgl_huc_7.9.3.bin`. Sie kommen aus
`firmware-intel-graphics`; `scripts/check-image-hardware.py` verlangt sie
beim Abbildbau einzeln.

Hardware-Codecs nach der [Featureliste des Intel-Media-Treibers](https://github.com/intel/media-driver/blob/master/docs/media_features.md)
(iHD, VA-API) und der [Jellyfin-Anleitung für Intel](https://jellyfin.org/docs/general/post-install/transcoding/hardware-acceleration/intel/):

| Plattform | Decodieren | Encodieren | Low-Power-Encodierung (VDEnc + HuC) |
|---|---|---|---|
| Skylake | H.264, HEVC 8 Bit | H.264, HEVC 8 Bit | nur H.264 |
| Apollo Lake | H.264, HEVC 8/10 Bit, VP9 8 Bit | H.264, HEVC 8 Bit | nur H.264 |
| Kaby/Coffee/Whiskey/Comet Lake, Gemini Lake | H.264, HEVC 8/10 Bit, VP9 8/10 Bit | H.264, HEVC 8 Bit | nur H.264 |
| Ice Lake | zusätzlich HEVC/VP9 mit 4:2:2/4:4:4 | H.264, HEVC 8/10 Bit, VP9 8/10 Bit | H.264, HEVC, VP9 |
| Jasper/Elkhart Lake (Gen11) | in beiden Quellen nicht eigens aufgeführt | nur Low-Power (Jellyfin) | nur Low-Power |
| Tiger/Rocket Lake | zusätzlich AV1 8/10 Bit | H.264, HEVC 8/10 Bit, VP9 | H.264, HEVC, VP9 |

Kein AV1 vor Gen12 (Tiger Lake), AV1-Encodierung erst ab DG2/Arc bzw. Meteor
Lake. Der Intel-Media-Treiber fasst Kaby, Coffee, Whiskey und Comet Lake als
„KBLx“ zusammen; Gemini Lake führt er nicht eigens, Jellyfin nennt dort HEVC
10 Bit. Jellyfin nennt HEVC 10 Bit für Kaby Lake auch beim Encodieren; die
Intel-Tabelle führt 10-Bit-HEVC-Encodierung erst ab Ice Lake. Die Tabelle
beschreibt die Hardware und den Treiber, nicht, was eine bestimmte App
daraus macht.

Weitere Grundlagen in [Linux 6.12](https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/tree/drivers/gpu/drm/i915?h=linux-6.12.y):
`i915_params.c` (`enable_guc` als Bitmaske: -1 automatisch, 0 aus,
1 GuC-Submission, 2 HuC laden), `gt/uc/intel_uc.c`,
`gt/uc/intel_guc_submission.c`, `gt/uc/intel_uc_fw.c` und `gt/uc/intel_huc.c`.
`enable_guc` ist dort als „unsafe“ markiert: Ein gesetzter Wert meldet der
Kernel beim Laden sinngemäß mit „Setting dangerous option enable_guc - tainting kernel“
und markiert sich als verändert (Taint). Das ist bei beiden Modi außer
„Automatisch“ zu erwarten und kein Fehler von TitanOS.

**Prüfstand:** Unit-Tests für PCI-ID → Plattform, Firmwaredateien,
Kommandozeilen-/Eintragsbearbeitung mit erhaltenen Parametern, Update- und
Probe-Einträge sowie das Lesen von debugfs-/Kernelmeldungs-Beispielen (nach den
Formatstrings von Linux 6.12 nachgebaut, keine Aufzeichnung einer echten
Maschine). **Nicht auf echter Skylake- oder anderer Gen9-11-Hardware getestet**,
auch nicht in QEMU mit Intel-Grafik; ob HuC lädt und Jellyfin/Plex danach
Low-Power-Encodierung anbieten, bleibt auf echter Hardware zu prüfen.

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
| `intel-qat` | `qat_4xxx`, `qat_420xx` | Intel QAT Gen 4 (8086:4940/4942/4944/4946) | Firmware `qat_4xxx*.bin` aus linux-firmware (Debian liefert sie nicht mit), `qat_402xx`/`qat_420xx` bereits in `firmware-intel-misc`; TitanOS nutzt QAT nicht gezielt |
| `it87-extended` | `it87` von frankcrawford/it87 (fester Commit) | ITE IT8606E, IT8607E, IT8613E, IT8625E, IT8655E, IT8665E, IT8686E, IT8688E, IT8689E, IT8696E, IT8698E, IT8736F, IT8738E, IT8785E zusätzlich zum it87 des Kernels | ersetzt den System-it87; ACPI-Konflikte werden nicht umgangen |

Tehuti TN40xx: nicht unterstützt – braucht QT2025-PHY-Treiber (Rust, von Debian
nicht gebaut) bzw. nicht verteilbare AQR105-Firmware; ggf. mit neuerem Kernel
neu prüfen. Das zwischenzeitlich vorbereitete Plugin `tehuti-tn40` wurde vor
der Veröffentlichung entfernt.

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
`wangxun-nic` und `intel-qat` ihre Module ohne passende
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
