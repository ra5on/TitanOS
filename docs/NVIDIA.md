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

Ab **beta.3** kann das Plugin unter **Plugins → NVIDIA →
Herunterladen und installieren** direkt aus dem passenden GitHub-Release geladen
werden. Dafür genügt ein eingebundenes Daten-Volume; eine Freigabe ist nicht nötig.
Siehe [Plugin-Installation](PLUGINS.md#installation-der-zusätzlichen-plugins).
Die folgenden Schritte gelten für beta.2 und die manuelle Installation.

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

## Aktivierungsfehler in beta.3

Wenn `nvidia-smi -L` die Karte erkennt, der Dienst aber mit
`mv: cannot stat '/run/cdi/.titanos-nvidia.tmp'` scheitert, liegt das an der
Veröffentlichung der CDI-Datei: nvidia-ctk ergänzt bei diesem Dateinamen
selbstständig `.yaml`. Der Entwicklungsstand nach beta.3 schreibt die
YAML-Ausgabe stattdessen in eine temporäre Datei und benennt sie erst nach
erfolgreichem Abschluss atomar um. Fehlgeschlagene Downloads sind davon
unabhängig. Die Korrektur wurde mit dem echten nvidia-ctk aus beta.3 und
mit simuliertem Abbruch geprüft.

Die temporäre Korrektur wurde am 15. Oktober 2026 auf einer RTX 5060 Ti bestätigt:
Das Plugin ließ sich anschließend aktivieren. Diese Bestätigung umfasst noch
keine CUDA-/NVENC-Aufträge oder die Übergabe der Karte an eine VM.

Bei der Installation kann „Direkt aktivieren“ abgewählt werden. Dann liegen die
geprüften Plugin-Dateien auf dem Daten-Volume, ohne Treiber zu laden oder Geräte
von laufenden VMs/Containern anzufassen. Die Aktivierung erfolgt später über den
Plugin-Schalter, wenn die Hardware für das NAS verfügbar ist. VMs ohne diese
GPU müssen dafür nicht ausgeschaltet werden. Ein laufendes oder noch nicht
abgeschlossenes Systemupdate wird separat gemeldet; beta.3 konnte dafür
irreführend „Plugin/GPU wird verwendet“ anzeigen.

## Versionsmenü und unabhängige Updates (Entwicklung nach beta.3)

Unter **Plugins → NVIDIA → NVIDIA-Treiberversion** werden die für den laufenden
TitanOS-Release und dessen exakten Kernel veröffentlichten Versionen angeboten.
Die Kernelmodule sind offen; Firmware und Treiberbibliotheken behalten ihre
NVIDIA-Lizenz. Ein beliebiges upstream `.run`-Paket wird nicht auf dem NAS gebaut.

Standardmäßig sucht TitanOS täglich nach neuen Versionen und lädt die empfohlene
Version automatisch auf das Daten-Volume. Die Option lässt sich abschalten.
**Versionen prüfen** sucht sofort; eine andere Version im Menü lässt sich über
**Diese Version herunterladen** bereitstellen. Heruntergeladene Versionen können
auch bei vorübergehend fehlender Internetverbindung gewählt werden.

**Beim nächsten Neustart verwenden** speichert die Auswahl. Geladene Treiber und
laufende GPU-VMs/-Container bleiben bis dahin unberührt. Ein Neustart von titand
allein reicht ausdrücklich nicht. **Auswahl zurücknehmen** verwirft die geplante
Aktivierung. Bis zur Rücknahme oder zum Neustart sind weitere Plugin-Änderungen
und die Vorbereitung eines OS-Updates gesperrt, damit der vorbereitete Satz
nicht veraltet. Ein ausgeschaltetes Plugin bleibt ausgeschaltet. Bei Problemen
kann der mit TitanOS ausgelieferte Treiber für den nächsten Neustart gewählt
werden. Ein automatischer Download überschreibt die bewusste Auswahl nie.

Downloads liegen in `.titanos/plugins/driver-updates/<OS-Version>/<SHA256>/` auf
dem Plugin-Volume. Erst vollständige, signierte und nachgeprüfte Dateien bekommen
einen Ready-Marker. Bei Auswahl entsteht ein kompletter Plugin-Satz unter
`releases/<OS-Version>/.sets/` über Hardlinks; die Einstellungen in
`/config/plugins/releases/<OS-Version>.json` enthalten Auswahl und Boot-ID.
Die Originaldateien bleiben für den OS-Rückfall erhalten. Eine automatische
Bereinigung alter Downloads und vorbereiteter Sätze ist noch nicht umgesetzt.

### Veröffentlichung ohne neue TitanOS-Version

Der Workflow **NVIDIA driver updates** prüft täglich die jeweils jüngste Stable-
und Beta-Version. Er benötigt keinen neuen OS-Build oder Tag. `workflow_dispatch`
kann gezielt eine vorhandene TitanOS-Version, eine upstream Treiberversion oder
`latest` und einen erzwungenen Neubau wählen. Eine neue Workflow-Ausführung trägt
eine höhere Revision; ein erneuter Publish derselben Revision wird abgelehnt.

`scripts/update-nvidia-lock.py` prüft NVIDIAs `InRelease` gegen den im Repository
festgelegten Schlüssel, danach den SHA-256 des Paketindex. Nur vollständige
Paketgruppen gleicher Treiberversion bzw. gleicher Toolkitversion werden gewählt.
Die Pakete werden weiterhin per SHA-256 und Debian-Paketidentität geprüft.
Compiler und Header laufen ausschließlich im wegwerfbaren Debian-Buildroot.
Fehlen die exakten Kernelheader, schlägt der Build fehl, statt auf einen anderen
Kernel auszuweichen.

Der Publish-Job benötigt weiterhin die Freigabe des geschützten Environments
`release`. Unveränderliche Images, signierte Plugin-Manifeste sowie Quellen- und
Lizenzarchive werden als `nvidia-update-<SHA256>.*` zum vorhandenen OS-Release
hinzugefügt. Zuletzt wird das signierte Versionsmenü `nvidia-updates.json`
ersetzt. Die ursprünglichen `manifest.json`, `SHA256SUMS`, `nvidia.raw` und
OS-Dateien bleiben unverändert. Dadurch funktionieren auch ältere NAS-Updater.
Ein kurzzeitig uneinheitliches Menü/Signatur-Paar wird verworfen; installierte
und bereits heruntergeladene Treiber bleiben erhalten.

Die CI kontrolliert Signaturen, Archive, Image-Inhalt, Modulversion und exakten
Kernel. Sie kann ohne NVIDIA-Hardware keine CUDA-/Transcoding-Funktion oder
GPU-Rückgabe aus einer VM bestätigen. Der erste echte unabhängige Treiberwechsel
muss nach Veröffentlichung mit einer GPU getestet werden.
