# NVIDIA und die GPU-/NPU-Kacheln

Ab `v0.1.0-beta.2` gibt es ein separates NVIDIA-Plugin. Die Beta wurde ohne
physische GPU getestet. CUDA, Transcoding und GPU-Container müssen noch auf
echter Hardware geprüft werden.

## Unterstützte Geräte und Umfang

- NVIDIA **Turing und neuer**, beispielsweise RTX 20/30/40/50, mit den offenen
  NVIDIA-Kernelmodulen. Ältere Pascal-/Maxwell-Karten werden nicht unterstützt.
- Treiber **615.71.09**, passend zum **exakten Kernel des jeweiligen Releases**.
- CUDA-Treiberbibliotheken, NVENC/NVDEC, NVML und `nvidia-smi`.
- NVIDIA Container Toolkit **1.20.1**, Geräte für Docker über CDI.
- Kein CUDA-Entwicklungs-SDK (`nvcc`), kein Desktop-/Xorg-Treiber und keine
  OpenGL-/Vulkan-Laufzeit. Anwendungen bringen ihre CUDA-Laufzeit im Container mit.

Die Firmware und Treiberbibliotheken stehen unter ihren ursprünglichen
NVIDIA-Lizenzen, die offenen Kernelmodule unter MIT/GPL-2.0 und das Container
Toolkit unter Apache-2.0. Lies vor der Installation die Originalbedingungen
im Release-Archiv `nvidia-licenses.tar.gz`, insbesondere
`doc/nvidia-driver-cuda/copyright`. `titanos/nvidia/SOURCES.txt` nennt alle
Paketversionen, Downloadadressen und Quellprojekte. Die passenden offenen
Kernelquellen liegen zusätzlich in `nvidia-kernel-sources.tar.gz` bei.

Das Plugin liegt auf einem Daten-Volume und wird erst nach dessen Einbindung
geladen. Sein Abbild wird nicht mit dem Grundsystem in den RAM kopiert;
geladene Module, Bibliotheken und der Dateicache benötigen trotzdem RAM.
Nouveau wird im Grundsystem nicht automatisch geladen, damit es die Karte
nicht vor dem NVIDIA-Plugin belegt. Die UEFI-Textkonsole bleibt verfügbar.

## Installation

1. TitanOS auf das gewünschte Release aktualisieren und neu starten. Ein
   Daten-Volume mit einer Freigabe muss vorhanden sein.
2. Von **demselben [GitHub-Release](https://github.com/ra5on/TitanOS/releases)**
   diese drei Dateien herunterladen, ohne ihre Namen zu ändern:
   `nvidia.raw`, `nvidia.plugin.json`, `nvidia.plugin.json.minisig`.
3. Die Manifest-Signatur mit dem vertrauenswürdig bezogenen öffentlichen
   [Release-Schlüssel](../build/mkosi.extra/usr/share/titanos/update-keys/)
   prüfen. Beispielsweise im Downloadordner:

   ```sh
   minisign -V -p titanos-release-1.pub -m nvidia.plugin.json
   ```

   Das signierte Manifest enthält SHA-256 und Größe des Abbilds. TitanOS
   prüft beides beim Installieren. Zur Prüfung aller Release-Downloads siehe
   [RELEASING.md](RELEASING.md#downloads-prüfen).
4. Alle drei Dateien über **Dateien** oder SMB in denselben Ordner einer
   Freigabe laden, höchstens vier Ordnerebenen unter dem Volume.
5. **Plugins → NVIDIA → Installieren** öffnen und das Ziel-Volume auswählen.
   Das Plugin muss als **Offiziell** erscheinen. Bei „Community“ zuerst die
   Dateien, die Signatur und die verwendete TitanOS-Version prüfen.
6. Nach erfolgreichem Start zeigt die Seite **Aktiv**. Ohne passende GPU
   erscheint ein Aktivierungsfehler. Nach Beheben der Ursache lässt sich die
   Aktivierung dort erneut versuchen. Dienstprotokoll:
   `journalctl -u titanos-nvidia.service`; Kernelmeldungen unter **Werkzeuge**.

TitanOS kopiert die Dateien nach `/mnt/<volume>/.titanos/plugins` und merkt
sich die Aktivierung auf dem Stick. Beim nächsten Start wird das Plugin
wieder geladen. Die ursprünglich hochgeladenen Dateien bleiben erhalten.

## NVIDIA in einem Docker-Container

Docker muss unter **Docker** auf einem Daten-Volume eingerichtet sein. In
einer Compose-Datei erhält ein Dienst die GPU mit `devices`. Beispiel für
einen einmaligen Test aus der NAS-Shell:

```sh
docker run --rm --device nvidia.com/gpu=all ubuntu:24.04 nvidia-smi -L
```

Beispiel für den Geräteabschnitt eines vorhandenen Compose-Dienstes:

```yaml
services:
  application:
    image: <dein-gpu-faehiges-image>
    devices:
      - nvidia.com/gpu=all
```

`nvidia-ctk cdi list` zeigt die verfügbaren Gerätenamen; statt `all` lässt
sich ein einzelnes Gerät auswählen. Die CDI-Datei wird nach jedem Start im
RAM erzeugt. Eine Änderung der Docker-Laufzeit oder ein Docker-Neustart ist
dafür nicht nötig. Die grafische Geräteauswahl im Container-Editor steht
in beta.2 noch aus; dort den Compose-Editor verwenden. Der Entwicklungsstand enthält eine Geräteauswahl, siehe [Hardware und Plugins](PLUGINS.md).

## Abschalten, VMs und Systemupdates

GPU-Container und andere GPU-Aufträge zuerst beenden, dann das Plugin unter
**Plugins** ausschalten oder entfernen. Ein noch belegtes Kernelmodul
verhindert das Aushängen. Bei einem Fehler bleibt die Erweiterung verfügbar.
In beta.2 ist das automatische Umschalten zwischen VM und Containern noch nicht
umgesetzt; eine an `vfio-pci` gebundene GPU steht dem Plugin nicht zur Verfügung.

In beta.2 werden Plugin-Updates **nicht automatisch mit einem Systemupdate**
installiert. Bei einem anderen Kernel bleibt das bisherige Plugin inaktiv.
Nach dem Systemupdate das alte Plugin entfernen und die drei Plugin-Dateien
des neuen Releases installieren. Vorherige Plugin-Dateien für einen möglichen
System-Rollback aufbewahren; danach wieder die dazu passende Version installieren.
Der Entwicklungsstand ergänzt Plugin-Mitnahme, Leistungslimit und VM-Wechsel;
Einrichtung und der Übergang von beta.2 stehen in [PLUGINS.md](PLUGINS.md).
Ein einstellbarer NVIDIA-Persistenzmodus ist nicht enthalten.

## Dashboard

Unter **Dashboard bearbeiten → Kachel hinzufügen** gibt es getrennte Kacheln
für **iGPU/GPU · Intel & AMD**, **NPU** und **NVIDIA**. Jede Kachel zeigt die
erkannten Geräte dieser Gruppe; sie kann unabhängig angeordnet, vergrößert
oder ausgeblendet werden. Das Layout wird pro Konto gespeichert.

Messwerte werden alle fünf Sekunden gelesen, der Verlauf umfasst bis zu
drei Minuten und liegt ausschließlich im RAM. Je nach Gerät/Treiber sind
Auslastung, Engine-Last, Temperatur, Leistungsaufnahme, Gerätespeicher und
Takt verfügbar. Intel-/AMD-Grafik nutzt sysfs und DRM-Prozesszähler, NVIDIA
`nvidia-smi`, die NPU ihre verfügbaren sysfs-Zähler. Bei mehreren Engines
zeigt der große Ring die höchste gemessene Engine-Auslastung. Prozesszähler
brauchen zwei Messpunkte; ohne auswertbaren Zähler bleibt die Anzeige „–“.
Gemeinsam genutzter Systemspeicher wird nicht als vermeintlicher VRAM addiert.

**„–“ bedeutet nicht null Prozent.** Ältere Kernel/Treiber bieten insbesondere
für NPUs nicht alle Zähler. Keine erkannte Hardware, ein fehlender Treiber,
Ruhezustand und eine VM-Reservierung werden als Text angezeigt. Diese Kacheln
installieren keine Treiber und ändern keine Gerätezuweisung.

Technische Grundlagen: [NVIDIA-Treiber](https://docs.nvidia.com/datacenter/tesla/driver-installation-guide/debian.html),
[offene Kernelmodule](https://docs.nvidia.com/datacenter/tesla/driver-installation-guide/kernel-modules.html),
[CDI](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/cdi-support.html)
und [DRM-Messwerte](https://www.kernel.org/doc/html/latest/gpu/drm-usage-stats.html).
