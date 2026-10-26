# Plugins und Hardware für Container

## Offizielle Plugins

Jedes volle Release baut und signiert diese optionalen Erweiterungen passend
zum Kernel seiner Systembasis:

| Plugin | Inhalt | Voraussetzung |
| --- | --- | --- |
| `nvidia` | offene NVIDIA-Kernelmodule, CUDA/NVENC/NVDEC-Bibliotheken, NVML und CDI für Docker, siehe [NVIDIA](NVIDIA.md) | NVIDIA-GPU ab Turing |
| `intel-gpu-tools` | Debian Intel GPU Tools, unter anderem `intel_gpu_top`, zusätzliche Bibliotheken und deren Lizenz-/Quellverzeichnis | Intel-GPU mit funktionierendem Kernel-Treiber; einzelne Tools benötigen bestimmte Generationen |
| `coral-usb` | USB-Erkennung und `titanos-coral-info`, Zuordnung für Container | Coral USB; die verwendete App bringt die TPU-Laufzeit mit |
| `tailscale` | Tailscale mit Subnet-Routing und Exit Node, siehe [Tailscale](TAILSCALE.md) | Tailscale-Konto |

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
zwischengespeichert. **Katalog und Freigaben aktualisieren** erzwingt einen neuen
Versuch; die regelmäßige Statusanzeige ruft GitHub nicht ab. Während ein Abbild
heruntergeladen wird, bleiben Plugin-Seite, Tailscale-Einstellungen und
VM-Starts bedienbar; nur die eigentliche Installation danach wird kurz gesperrt
und prüft vorher erneut Systemupdate-Sperre, Plugin-Volume und Treiberwechsel.
Dasselbe Plugin wird nicht zweimal gleichzeitig heruntergeladen. Bei Verbindungsproblemen
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
TitanOS sucht Freigaben und Volumes (ohne `appdata`, `vms` und versteckte
Ordner, bis vier Ebenen tief, höchstens 50.000 Einträge) beim Öffnen der Seite,
bei **Katalog und Freigaben aktualisieren** und wenn andere Volumes eingebunden
sind. Die Statusabfrage alle fünf Sekunden liest die Platten nicht erneut und
weckt ruhende Festplatten deshalb nicht auf. Neu abgelegte Dateien erscheinen
nach dem Aktualisieren.

## Plugins aktualisieren

Offizielle Plugins gehören zur Systembasis: Ein volles Systemupdate bringt die
dazu passenden signierten Fassungen mit (siehe unten). Für NVIDIA gibt es
zusätzlich **Treiber vorbereiten**; ein neuer Treiber wird beim nächsten
Neustart aktiviert und kann zum mitgelieferten zurückkehren
([Lokaler NVIDIA-Bau](NVIDIA-LOCAL-BUILD.md)).

Liegt eine **neuere** Fassung eines installierten Plugins in einer Freigabe,
zeigt die Plugin-Seite **Update** und unter dem Plugin **Aktualisieren**. Ältere
Fassungen werden nicht angeboten. Nach einer Rückfrage prüft TitanOS die neue
Fassung (Prüfsumme, Kernel, bei signierten Plugins die Signatur), kopiert sie
neben die alte, hält das Plugin an und startet es mit der neuen Fassung. Lässt
sich ein Plugin nicht anhalten, weil es noch benutzt wird, bleibt alles
unverändert. Startet die neue Fassung nicht, kommen die bisherigen Dateien
zurück und werden wieder eingeschaltet. Eine unsignierte Fassung zeigt dieselbe
Warnung wie eine neue Community-Installation. NVIDIA wird nie auf diesem Weg
ausgetauscht, auch nicht lokal gebaute Treiber. Versionen werden nach ihren
Zahlen verglichen (`1.10` ist neuer als `1.9`).

Die Intel-Werkzeuge lassen sich im NAS-Terminal verwenden.
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
für den Rückfall liegen, bis sie unter **Alte Plugin-Versionen** ausdrücklich
entfernt werden (siehe unten). Beim Probestart müssen aktivierte Plugins
erfolgreich geladen sein, bevor TitanOS den Start bestätigt. Ein bestehendes
Plugin wird nicht allein durch das Herunterladen eines neuen TitanOS-ZIPs
aktualisiert.

## Entwicklung und Tests

`scripts/make-plugin.sh <name> <version> <ordner>` packt einen Ordner zu Abbild
und Manifest. Enthält der Ordner Kernelmodule (`usr/lib/modules/<kernel>`), muss
`plugin.json` genau diesen Kernel nennen, sonst bricht das Skript ab.

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
Bereinigung, die Sperre bei einem beginnenden Systemupdate und dass ein
laufender Download Statusabfrage, Healthcheck und Tailscale nicht blockiert.
`plugins_test.go` prüft Aktualisieren aus einer Freigabe einschließlich Rückweg
bei Fehlstart, belegtem Plugin und ausgeschlossenem NVIDIA; die Browserprüfung
`scripts/test-ui-plugins.mjs` den Dialog mit isolierten Daten.

### Installation bei laufenden VMs und Containern

Beim Installieren kann „Direkt aktivieren“ abgewählt werden. Signatur, Kernel und Prüfsumme werden weiterhin geprüft;
die Dateien werden nur auf dem Daten-Volume gespeichert. Die Installation lädt
keine Treiber und stoppt keine VMs oder Container. Späteres Einschalten bindet
das Plugin wie bisher ein. Eine GPU, die exklusiv an eine VM durchgereicht ist,
bleibt bis zur Rückgabe bei dieser VM.

Die Sperre während eines Systemupdates ist unabhängig von laufenden VMs und
wird ausdrücklich als Systemupdate-Sperre angezeigt.

## Alte Release-Dateien

Nicht mehr benötigte Plugin-Versionen lassen sich mit Vorschau und erneuter Schutzprüfung aufräumen. Aktuelle, aktive, Rückfall- und vorbereitete Versionen bleiben erhalten. [Ablauf und Grenzen](PLUGIN-CLEANUP.md).
