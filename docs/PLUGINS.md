# Hardware für Container und zusätzliche Plugins

Die folgenden Ergänzungen sind ab `v0.1.0-beta.3` enthalten.
Für die veröffentlichte beta.2 gilt weiterhin die [NVIDIA-Anleitung](NVIDIA.md).

## Installation der zusätzlichen Plugins

Der Release-Workflow baut neben NVIDIA zwei weitere optionale Erweiterungen:

| Plugin | Inhalt | Voraussetzung |
| --- | --- | --- |
| `intel-gpu-tools` | Debian Intel GPU Tools, unter anderem `intel_gpu_top`, zusätzliche Bibliotheken und deren Lizenz-/Quellverzeichnis | Intel-GPU mit funktionierendem Kernel-Treiber; einzelne Tools benötigen bestimmte Generationen |
| `coral-usb` | USB-Erkennung und `titanos-coral-info`, Zuordnung für Container | Coral USB; die verwendete App bringt die TPU-Laufzeit mit |

Unter **Plugins** erscheint der offizielle Katalog des laufenden TitanOS-Releases.
TitanOS prüft die Signaturen des Release-Inventars und der Plugin-Beschreibungen,
bevor es die Einträge anzeigt. Welche Plugins angeboten werden, hängt vom
Release ab; ein neueres oder „latest“-Release wird nicht ersatzweise verwendet.

1. Ein Daten-Volume unter **Speicher** anlegen bzw. einbinden. Eine Freigabe ist
   für den direkten Download nicht erforderlich.
2. Unter **Plugins** die gewünschte Erweiterung auswählen und **Herunterladen
   und installieren** anklicken.
3. Den Lizenzlink und den Hinweis zu Systemrechten lesen, das Ziel-Volume
   auswählen und die Installation bestätigen.

Erst dann lädt TitanOS das Abbild von GitHub direkt auf das Daten-Volume.
Signaturen, Zielversion, Kernel-Kompatibilität, Größe und SHA-256 werden vor
der Aktivierung geprüft. Der Download wird nicht auf dem Bootstick oder im
RAM-Systemabbild zwischengespeichert. Bei einem fehlgeschlagenen Download
werden die temporären Dateien entfernt; die Installation kann erneut gestartet
werden. Ein aktives Plugin wird beim Boot nach Einbindung des Volumes wieder
geladen. Eine erfolgreiche Installation ersetzt nicht die Prüfung auf passender
echter Hardware.

Beim Öffnen der Seite wird der Katalog abgerufen und für zehn Minuten im RAM
zwischengespeichert. **Katalog aktualisieren** erzwingt einen neuen Versuch;
die regelmäßige Statusanzeige ruft GitHub nicht ab. Bei Verbindungsproblemen
bleiben installierte Plugins, lokale Dateien und bereits geprüfte Katalogeinträge
sichtbar. Vor einer direkten Installation wird die Herkunft erneut online
geprüft. Eine identische signierte Kopie in einer Freigabe hat Vorrang vor dem
Download und lässt sich auch bei zwischengespeichertem Katalog offline installieren.
Entwicklungs-Builds ohne Release-Version sowie fehlende oder noch
unvollständige Releases erhalten einen erklärenden Hinweis.

**Manuelle Installation:** Alternativ aus dem **gleichen Release** `<name>.raw`,
`<name>.plugin.json` und `<name>.plugin.json.minisig` herunterladen und gemeinsam
in eine NAS-Freigabe legen. Unter **Plugins** erscheinen sie auch ohne Verbindung
zu GitHub. Ein offizieller Download muss dort als **Offiziell** erkannt werden.

Die Intel-Werkzeuge lassen sich im bestätigten NAS-Terminal verwenden.
`intel_gpu_top` benötigt eine passende Intel-GPU; das Plugin erzeugt keine
künstlichen Dashboard-Messwerte. Kernel und Firmware kommen aus dem Basisabbild.

Coral USB benötigt am Host keinen `gasket`-/`apex`-Treiber. Das Plugin verteilt
keine Google-Runtime und keine Google-Firmware. Die Container-App muss diese
selbst mitbringen und deren Nutzungsbedingungen erfüllen. PCIe-/M.2-Coral wird
mit diesem Plugin nicht unterstützt. `titanos-coral-info` erkennt sowohl den
USB-Zustand vor als auch nach dem Laden der Firmware.

## Geräte auswählen

Im App-Installationsdialog und beim Bearbeiten eines Compose-Dienstes gibt es
**Geräte für diesen Container**. Intel-/AMD-Grafik, NPU und NVIDIA werden nur angeboten,
wenn ein nutzbarer Treiber bzw. Geräteknoten vorhanden ist. Eine an eine VM
vergebene GPU wird gesperrt angezeigt. Jeder Dienst wählt seine Geräte selbst.

Die Compose-Datei speichert PCI-Kennungen in `x-titanos-hardware`. Erst beim
Start werden die aktuellen Render-/Accel-Knoten samt Gruppen oder die
NVIDIA-CDI-UUID aufgelöst. Bestehende manuelle Gerätezuordnungen bleiben erhalten.
Vorlagenanforderungen `gpu-intel`, `npu-intel` und `gpu-nvidia` lassen sich durch
eine konkrete Auswahl ersetzen oder ausdrücklich abwählen.

Coral USB wird als gemeinsamer USB-Zugriff ausgewählt. Wegen der USB-Neuanmeldung
beim Firmware-Laden wird `/dev/bus/usb` eingebunden und Zugriff auf USB-Geräte
freigegeben. Die Oberfläche weist darauf hin: Das gewährt der App Zugriff auf
weitere USB-Geräte. Nur dafür vertrauenswürdige Apps verwenden. Ein laufender
Coral-Container blockiert die Übergabe der TPU an eine VM; dafür gibt es keinen
automatischen Wechsel.

## NVIDIA zwischen VM und Containern

In den VM-Einstellungen kann **NVIDIA automatisch zwischen dieser VM und
Containern wechseln** aktiviert werden. Beim Start reserviert TitanOS die ausgewählte
NVIDIA-Karte, beendet ihre laufenden Container mit einer Frist von 30 Sekunden
und startet danach die VM. Ohne diese Option wird der VM-Start bei belegter
Karte abgelehnt. Pausierte Container werden nicht automatisch übernommen.

Wenn die VM endet und der Host-Treiber wieder verfügbar ist, wird die CDI-
Beschreibung erneuert und nur die zuvor laufenden Container werden wieder
gestartet. Scheitert der VM-Start, erfolgt derselbe Wiederanlauf. Reservierungen
und Container-IDs bleiben bei einem titand-Neustart in `/run` erhalten; nach
einem vollständigen NAS-Neustart gelten wieder die normalen Startregeln.
Container mit mehreren GPUs warten auf alle benötigten Karten. Solange eine
Karte reserviert ist, werden konkurrierende Starts über TitanOS abgewehrt.
Manuelle GPU-Zuordnungen in Compose werden dabei konservativ gesperrt.

Die Übergabe benötigt funktionierendes IOMMU, eine isolierbare Gerätegruppe und
GPU-/Firmware-Unterstützung für das Zurücksetzen der Karte. Der automatische
Ablauf ist mit simulierten Docker-/VM-Zuständen geprüft, bisher nicht mit einer
echten NVIDIA-Karte. Falls der Treiber nicht zurückkehrt, bleiben die Container
gestoppt; die Ursache steht im Protokoll. Direkte Docker-/virsh-Befehle außerhalb
von TitanOS umgehen dessen Koordination.

Unter **Einstellungen → Hardware** lässt sich außerdem das NVIDIA-Leistungslimit
als Anteil des Standardlimits oder auf **Werkseinstellung** setzen. TitanOS prüft zuerst
die von der Karte gemeldeten Watt-Grenzen; nicht unterstützte Werte werden
abgelehnt. Nach einem Treiberwechsel wird die gespeicherte Einstellung erneut
angewendet. Eine der VM übergebene GPU wird nicht verändert.

## Plugins bei A/B-Updates

Vor dem Schreiben des neuen Boot-Slots werden installierte offizielle Plugins
aus dem signierten Release-Inventar geladen. Ihre Manifeste, Zielversion, der
Kernel sowie Größe und SHA-256 des Abbilds werden geprüft. Ein fehlendes oder
ungültiges notwendiges Treiberpaket bricht das Update ab, bevor der Rückfall-
Slot überschrieben wird. Kernel-unabhängige Community-Plugins können übernommen
werden; für ein Community-Kernelmodul ohne passenden Build wird das Update
abgebrochen. Deaktivierte installierte Plugins werden ebenfalls berücksichtigt.

Dateien liegen je Systemversion unter
`/mnt/<volume>/.titanos/plugins/releases/<version>/`, die Aktivierungseinstellungen
unter `/config/plugins/releases/<version>.json`. Ein Rollback lädt seine eigenen
Dateien und Einstellungen. Änderungen an Plugins sind während eines laufenden
oder zum Neustart vorgemerkten Systemupdates gesperrt. Alte Plugin-Sätze bleiben
für den Rückfall liegen; eine automatische Bereinigung gibt es noch nicht.
Beim Probestart müssen aktivierte Plugins erfolgreich geladen sein, bevor
TitanOS den Start bestätigt.

**Übergang von beta.2:** Deren alter Updater kann Plugins noch nicht vorbereiten.
Bei einem Kernelwechsel das NVIDIA-Plugin vor diesem ersten Systemupdate
abschalten, nach dem Update die neue passende Version installieren und die
vorherigen Download-Dateien für einen Rückfall aufbewahren. Ab dem neuen
Updater läuft die Mitnahme automatisch. Ein bestehendes Plugin wird nicht
allein durch das Herunterladen eines neuen TitanOS-ZIPs aktualisiert.

## Entwicklung und Tests

`scripts/make-tools-plugins.sh <version>` baut die Werkzeuge in einem isolierten
Debian-Rootfs. Es benötigt zuerst das Basisabbild unter `build/mkosi.output` und
läuft als root oder in einem Benutzer-/Mount-Namespace. Die Release-Pipeline
signiert jedes Plugin-Manifest und die vollständige Prüfsummenliste.

`scripts/smoke-storage.py <zip> --plugins out/plugins` prüft Installation,
Bibliotheken und Aktivierung der Werkzeuge über Neustarts in QEMU. Tests mit
echter Intel-GPU, Coral TPU und NVIDIA fehlen weiterhin.

Die Go-Tests in `backend/internal/plugins/catalog_test.go` prüfen den Katalog
und Downloads mit einem lokalen HTTP-Server und echten minisign-Signaturen:
Abruf erst bei Bedarf, Versions-/Kernelbindung, Manipulationen, Abbrüche,
Bereinigung und die Sperre bei einem beginnenden Systemupdate.
