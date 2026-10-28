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
| `netbird` | NetBird-Client (nur BSD-3-Clause-Client) mit Setup-Key-Anmeldung, eigenem Management-Server, Routen und DNS, siehe [NetBird](NETBIRD.md) | NetBird-Konto oder eigener NetBird-Server und Setup-Key |
| `wangxun-nic` | Treiber `ngbe`/`txgbe` (mit `libwx`) aus den Kernelquellen des Systems | Netzwerkkarte mit Wangxun WX1860 (1 GbE) oder SP1000/WX1820 (10 GbE) |
| `intel-qat` | Treiber `qat_4xxx`/`qat_420xx` und Firmware `qat_4xxx*.bin` aus linux-firmware | Intel QuickAssist Gen 4 (PCI 8086:4940/4942/4944/4946) |
| `it87-extended` | externer Sensortreiber `it87` (frankcrawford/it87, fester Commit), ersetzt den it87 des Systems | Mainboard mit neuerem ITE-Super-I/O-Chip (z. B. IT8689E, IT8696E) |

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
heruntergeladen wird, bleiben Plugin-Seite, Tailscale-/NetBird-Einstellungen und
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

## Bereiche, Suche und Details

Die Plugin-Seite gruppiert den Katalog nach Bereichen (Netzwerkkarten, Grafik
und Beschleuniger, Sensoren und Mainboard, Speicher-Controller, Werkzeuge,
Fernzugriff und VPN, Sonstige) und lässt sich durchsuchen (Name, Beschreibung,
unterstützte Hardware, erkannte Geräte). Die Detailansicht eines Plugins zeigt,
soweit das Manifest es angibt: „Wann brauche ich das?“, unterstützte Hardware,
die auf diesem NAS erkannten passenden Geräte samt gebundenem Treiber, was es am
System ändert, Grenzen und Risiken, Lizenz, Herkunft mit fester Version,
Platzbedarf (Größe des Abbilds auf dem Daten-Volume und RAM-Hinweis) sowie die
Quellen.

## Empfehlungen für deine Hardware

TitanOS vergleicht die Geräte des NAS mit den Hardware-Regeln der offiziellen
Plugins und zeigt oben auf der Plugin-Seite **Für deine Hardware empfohlen**:
das Plugin, die erkannten Geräte (Name aus `pci.ids` bzw. vom USB-Gerät,
PCI-Adresse) und die Knöpfe **Ansehen und installieren** und **Nicht mehr
vorschlagen**. Installiert wird nie von selbst; der Knopf öffnet nur den
gewohnten Installationsdialog.

- **Was passt:** Jede Regel ist ein Modalias-Muster wie in `modinfo` (`pci:…`,
  `usb:…`, `dmi*:…`), verglichen wie `fnmatch(3)` mit `/sys/bus/pci/devices/*/modalias`,
  `/sys/bus/usb/devices/*:*/modalias` und `/sys/class/dmi/id/modalias`. Für
  Treiber-Plugins stammen die Regeln aus der Aliastabelle des gebauten Moduls;
  der Bau bricht ab, wenn Katalog und Modul nicht übereinstimmen.
- **Wann empfohlen wird:** nur wenn das Plugin nicht installiert ist und – bei
  Regeln mit `"unbound": true` (Treiber) – kein Kerneltreiber das Gerät schon
  übernommen hat. Werkzeuge wie Intel GPU Tools arbeiten neben dem vorhandenen
  Treiber und haben diese Bedingung nicht. NVIDIA wird nur für Grafikgeräte ab
  Turing (PCI-Geräte-ID ab 1E00) empfohlen, wie das Plugin sie unterstützt;
  eine an eine VM übergebene Karte (vfio-pci) gilt als übernommen. Coral wird nur
  für den USB Accelerator empfohlen (PCIe/M.2 unterstützt das Plugin nicht).
  `it87-extended` wird nur für die Boards empfohlen, die der Treiber selbst
  nennt (DMI-Tabelle); auf anderen Boards mit unterstütztem Chip kann er ebenfalls
  helfen.
- **Wann geprüft wird:** beim Start von titand, bei udev-„add“-Ereignissen von
  PCI- und USB-Geräten (gebündelt nach drei Sekunden) und beim Aktualisieren des
  Katalogs. Gelesen wird nur sysfs; die Statusabfrage der Seite rechnet aus dem
  gespeicherten Ergebnis im RAM, ohne erneut zu suchen. Ohne Netz genügt die
  Katalogtabelle des Abbilds (`/usr/share/titanos/plugin-catalog/<name>.json`,
  aus `plugins/catalog/`); der signierte Release-Katalog und installierte
  Manifeste haben Vorrang.
- **Meldung:** Für jede neue Kombination aus Plugin und Geräteart (Hersteller-
  und Geräte-ID bzw. Board) gibt es einmal die Meldung `plugin_recommended`
  (Hinweis, Link auf **Plugins**) – etwa nach dem Einbau einer Karte oder wenn ein
  Update ein neues Plugin mitbringt. Gemeldet wird erst, wenn das Plugin-Volume
  eingebunden ist, damit ein installiertes Plugin nicht fälschlich empfohlen wird.
- **Nicht mehr vorschlagen:** merkt sich die aktuellen Geräte dieses Plugins in
  `/config/plugins/recommendations.json` (`dismissed`; dazu `notified` für die
  einmalige Meldung). Geschrieben wird nur bei einer Änderung, nie zyklisch.
  **Ausgeblendete Vorschläge wieder anzeigen** leert die Liste. API:
  `POST /api/v1/plugins/recommendations/{name}/dismiss`,
  `DELETE /api/v1/plugins/recommendations/dismissed` (Besitzer/Administratoren);
  `GET /api/v1/plugins` enthält `recommendations`, `dismissed` und je Plugin
  `devices`.
- Die Ersteinrichtung (`/welcome`) zeigt im letzten Schritt den Punkt
  „Plugins für deine Hardware“, wenn es Empfehlungen gibt.

## Treiber-Plugins

`scripts/make-driver-plugins.py` baut `wangxun-nic`, `intel-qat` und
`it87-extended` in jedem vollen Release (und in `plugins.yml`) gegen den
exakten Kernel des Abbilds:

- In einem Wegwerf-mkosi-Root (`plugins/drivers/build`) werden
  `linux-headers-<kernel>` und `linux-source-<x.y>` genau in der Version des
  `linux-image`-Pakets aus dem Abbild installiert. Abweichende Paketversionen,
  eine andere Kernelversion im Quellbaum, ein falsches `vermagic` eines Moduls,
  andere als die erwarteten Module oder Modulaliase, die nicht den
  Hardware-Regeln in `plugins/catalog/<name>.json` entsprechen, brechen den Bau
  ab.
- Kerneltreiber stammen unverändert aus dem Debian-Quellpaket; externe Quellen
  (it87) und Firmware (QAT) sind mit Commit und SHA-256 in
  `plugins/drivers/drivers.json` festgelegt.
- Module liegen unter `/usr/lib/modules/<kernel>/updates/titanos-<name>/` und
  haben damit Vorrang vor gleichnamigen Modulen des Systems; das Manifest nennt
  den Kernel und die zu ladenden Module. Geladen wird beim Aktivieren und bei
  jedem Start nach dem Einbinden des Plugin-Volumes, entladen beim Abschalten
  oder Entfernen; für einen anderen Kernel wird das Plugin nicht geladen
  (Hinweis „Anderer Kernel“).
- Die Module sind nicht mit Debians Schlüssel signiert. Der Kernel lädt sie,
  weil Secure Boot bei TitanOS aus ist, und meldet dabei „tainted“.
- Jedes Release enthält `<name>-sources.tar.gz` (genau die kompilierten Quellen
  und das Baurezept) und `<name>-licenses.tar.gz`; `titanos-<version>-sources.txt`
  nennt zusätzlich das Debian-Quellpaket bzw. den Upstream-Commit.

**Grenzen:** Keines der Treiber-Plugins ist bisher auf echter Hardware mit
TitanOS geprüft. Tehuti TN40xx wird nicht angeboten: Karten mit QT2025-PHY
brauchen einen in Rust geschriebenen PHY-Treiber, den Debian nicht baut, Karten
mit AQR105-PHY ggf. die nicht verteilbare PHY-Firmware `tehuti/aqr105-tn40xx.cld`
(siehe [Hardware-Kompatibilität](HARDWARE-COMPATIBILITY.md)). Wie alle
Plugins liegen sie auf dem Daten-Volume und werden erst nach dessen Einbinden
geladen; eine Netzwerkkarte aus einem Plugin eignet sich deshalb nicht als
einziger Verwaltungsanschluss.

**it87-extended:** ersetzt den it87 des Systems, solange das Plugin aktiv ist
(der Dienst `titanos-it87-extended` entlädt einen bereits geladenen System-it87).
Meldet der Treiber einen ACPI-Ressourcenkonflikt, lädt er nicht und das Plugin
zeigt „Nicht geladen“. TitanOS setzt weder `acpi_enforce_resources=lax` noch
`ignore_resource_conflict=1` und bietet dafür keinen Schalter an: Greifen
Firmware und Treiber gleichzeitig auf den Chip zu, sind falsche Werte,
Lüfterprobleme oder ein instabiles System möglich. Wer das bewusst testen will,
kann es bis zum nächsten Neustart in der NAS-Shell tun:
`modprobe -r it87; modprobe -d /run/titanos-plugin-modules it87 ignore_resource_conflict=1`.

## Katalogangaben im Manifest

Alle Felder sind optional und werden von älteren TitanOS-Versionen ignoriert;
die Signatur des Manifests deckt sie wie alles andere ab. Für offizielle Plugins
liegen sie in `plugins/catalog/<name>.json` und werden von
`scripts/make-plugin.sh` mit dem beim Bau erzeugten `plugin.json` zusammengeführt
(Bauwerte wie Version, Kernel und Module haben Vorrang). Dieselben Dateien
kommen als Katalogtabelle ins Abbild.

```json
{
  "title": "Wangxun-Netzwerkkarten",
  "description": {"de": "…", "en": "…"},
  "category": "network",
  "license": "GPL-2.0-only",
  "upstream": {"name": "…", "url": "https://…", "version": "vom Bau gesetzt"},
  "details": {
    "de": {"when": "Wann brauche ich das?", "hardware": ["Chip/Karte …"],
           "changes": "Was es am System ändert", "limits": ["Grenze/Risiko …"],
           "footprint": "RAM-Hinweis"},
    "en": {"…": "…"}
  },
  "hardware": [{"modalias": "pci:v00008088d00001001sv*sd*bc*sc*i*", "unbound": true}],
  "sources": ["https://snapshot.debian.org/package/linux/…/", "wangxun-nic-sources.tar.gz"]
}
```

- `category`: `network`, `graphics` (Grafik und Beschleuniger), `sensors`,
  `storage`, `tools`, `remote`; Unbekanntes erscheint unter „Sonstige“.
- `hardware`: höchstens 1000 Regeln, je höchstens 512 Zeichen. Verglichen werden
  `pci`, `usb` und `dmi`; Regeln für andere Busse bleiben gültig, passen aber nie
  (damit ein neuerer Regeltyp ein signiertes Manifest nicht unbrauchbar macht).
  `unbound: true` für Treiber (siehe oben).
- Neues Treiber-Plugin: Eintrag in `plugins/drivers/drivers.json` (Kernel-
  Verzeichnis und Kconfig-Werte bzw. externe Quelle mit Commit und SHA-256,
  erwartete Module, zu ladende Module, ggf. Firmware), Beschreibung in
  `plugins/catalog/<name>.json` mit den Aliasen des Moduls als `hardware`.
  Die Liste offizieller Plugins in `make-release.sh` und `verify-release.py`
  folgt `drivers.json` von selbst.

## Fortschritt in Schritten

Installieren, Aktualisieren und Entfernen zeigen auf der Plugin-Seite die
tatsächlichen Schritte, die das NAS gerade ausführt, jeweils mit Markierung
(ausstehend, läuft, erledigt, fehlgeschlagen, entfällt):

| Vorgang | Schritte |
| --- | --- |
| Installation von GitHub | Signatur prüfen (signiertes Release-Inventar und Manifest), Herunterladen (Bytes, Gesamtgröße und Tempo), Prüfsumme prüfen, Auf das Daten-Volume schreiben |
| Installation aus einer Freigabe | Signatur prüfen (bei Community-Plugins „entfällt“, die Installation ist dann per Klick bestätigt), Prüfsumme prüfen, Auf das Daten-Volume schreiben |
| Aktualisieren | wie aus einer Freigabe, danach Plugin anhalten |
| Entfernen | Plugin anhalten, Systemerweiterung aktivieren (sysext ohne das Plugin), Dateien vom Daten-Volume löschen |

Wird das Plugin dabei aktiviert, folgen Systemerweiterung aktivieren
(`systemd-sysext refresh`), Kernelmodule laden und Dienste starten (nur wenn
das Manifest Module bzw. Dienste nennt) sowie der Healthcheck: Das Plugin muss
danach aktiv sein, wie es auch die Startprüfung für eingeschaltete Plugins
verlangt. Ohne „Direkt aktivieren“ entfallen diese Schritte.

Scheitert ein Schritt, ist genau dieser markiert und zeigt seine Fehlermeldung;
spätere Schritte bleiben ausstehend. Startet eine aktualisierte Fassung nicht,
folgt der eigene Schritt **Bisherige Version zurückholen**. Ein abgewiesener
Auftrag (etwa während eines Systemupdates oder bei bereits installiertem
Plugin) erzeugt keinen Eintrag; die Seite zeigt dann nur die Fehlermeldung.

Die Schritte liegen nur im RAM von titand (`GET /api/v1/plugins/progress`,
zusätzlich in `GET /api/v1/operations` unter `plugins`) und unter einer eigenen
Sperre: Sie lassen sich auch verfolgen, während eine Aktivierung die
Plugin-Sperre hält. Behalten werden alle laufenden und die letzten zehn
beendeten Vorgänge; `DELETE /api/v1/plugins/progress/{id}` blendet einen
beendeten aus. Es wird nichts nach `/config` oder auf ein Volume geschrieben,
nach einem titand-Neustart ist die Liste leer. Der Vorgang läuft unabhängig von
der Browserverbindung weiter: Wer die Seite verlässt oder den Tab schließt,
findet ihn unter **Werkzeuge › Aufträge** (Art „Plugin“ mit aktuellem Schritt,
Downloadbalken und bei Fehlern der Meldung) und beim Zurückkehren wieder auf der
Plugin-Seite. Fehlgeschlagene Vorgänge bleiben dort sichtbar, bis sie
ausgeblendet werden. Abbrechen lässt sich ein laufender Vorgang nicht; ein
Download ohne Daten endet weiterhin nach zwei Minuten. Der lokale NVIDIA-Bau
unter **Treiber vorbereiten** behält seine eigene Fortschrittsanzeige.

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
`progress_test.go` prüft die gemeldeten Schritte: vollständige Installation,
Fehler in einem mittleren Schritt (sysext, Module, Dienste) mit Fehlermeldung
genau dort, falsche Prüfsumme lokal und beim Download, Bytes während eines
laufenden Downloads bei gehaltener Plugin-Sperre, Rückweg einer Aktualisierung
als eigener Schritt, belegtes Entfernen und die Begrenzung auf zehn beendete
Vorgänge.

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
