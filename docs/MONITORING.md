# RAM, Netzwerk, Beschleuniger und Temperaturen

## RAM

Das Dashboard zeigt den gesamten vom Kernel verwalteten belegten RAM:
`MemTotal - MemFree` aus `/proc/meminfo`. Darin sind Programme, VMs, Container,
Kernel, Dateicache und tmpfs einschließlich des TitanOS-Abbilds bereits enthalten.
Sie werden nicht noch einmal addiert. `MemAvailable` wird separat als verfügbar
angezeigt; dieser Wert berücksichtigt wieder freigebbaren Cache. Installierter,
von Firmware oder Geräten reservierter Speicher ist nicht Teil von `MemTotal`.

## Netzwerkgeschwindigkeit

Die Linkgeschwindigkeit kommt vom ausgehandelten physischen Anschluss hinter
der ausgewählten Schnittstelle. Bei einer Bridge werden virtuelle VM-/Container-
Ports ausgelassen; VLANs folgen ihrem unteren Gerät. Linux 6.12 meldet für veth
10.000 Mbit/s und für eine Bridge den höchsten Wert ihrer aktiven Mitglieder.
Dieser Bridge-Wert ist daher kein verlässliches Tempo des NAS-Anschlusses.

Eine Bridge mit genau einem physischen Uplink zeigt dessen Linktempo. Bei
mehreren unabhängigen physischen Uplinks bleibt der gemeinsame Wert unbekannt;
die einzelnen Anschlüsse sind weiterhin auswählbar. Für Bonds bleibt der vom
Kernel anhand aktiver Mitglieder und Betriebsart ermittelte Gesamtwert erhalten.
Fehlender Carrier, unbekannte Geschwindigkeit und reine virtuelle Netze ergeben
keinen erfundenen Messwert. Die Topologie wird aus sysfs gelesen, ohne Netzwerk-
oder Stickkonfiguration zu verändern. Schnittstellenname, IP-Adressen und
Traffic-Zähler bleiben die der ausgewählten Schnittstelle.

Die Regressionstests bilden Bridge mit 2.500-Mbit/s-Anschluss und virtuellem
10.000-Mbit/s-Port, VLANs, Bonds, Mehrdeutigkeit und fehlende Werte in temporärem
sysfs nach. Eine Prüfung am Anschluss des Besitzers steht noch aus.

Quellen: [Bridge in Linux 6.12](https://github.com/torvalds/linux/blob/v6.12/net/bridge/br_device.c),
[veth](https://github.com/torvalds/linux/blob/v6.12/drivers/net/veth.c),
[Bonding](https://github.com/torvalds/linux/blob/v6.12/drivers/net/bonding/bond_main.c).

## iGPU und NPU

- i915: geräteweite Auslastung aus den `*-busy`-perf-Ereignissen des zum PCI-Gerät
  gehörenden PMU. IDs und CPU werden aus sysfs gelesen. Prozentwerte entstehen
  erst nach zwei gültigen Messungen; Multiplexing, Zählerrücklauf, verschwundene
  Geräte und fehlende Zugriffsrechte werden berücksichtigt. Angezeigt werden die
  einzelnen Engines und als Gesamtwert die am stärksten ausgelastete Engine.
- Ohne PMU bleibt die bestehende DRM-fdinfo-Auswertung als Rückfall erhalten.
  Xe verwendet weiterhin seine fdinfo-Zähler. Der Intel-Takt stammt aus den
  tatsächlich gemessenen GT-Frequenzen, bei mehreren GTs dem höchsten Wert.
- Intel-NPU: `npu_busy_time_us` ist ein im Treiber geführter Softwarezähler.
  Er wird auch bei Runtime-Suspend gelesen, ohne die NPU aufzuwecken. Ein
  unveränderter Zähler ergibt gemessene 0 %. Andere Messdateien werden bei
  suspendierten Geräten weiterhin nicht gelesen.
- Nicht vorhandene Messwerte bleiben unbekannt. Insbesondere bietet der
  mitgelieferte 6.12-Treiber auf dem getesteten Core Ultra 225 keinen NPU-Takt
  und keinen NPU-Speicherwert über die verwendeten sysfs-Dateien an.

Die NAS-Ausgaben vom 15.10.2026 bestätigten i915 auf `8086:7d67`, intel_vpu auf
`8086:ad1d`, sechs i915-Busy-Ereignisse und den NPU-Busy-Zähler während Suspend.
Die Auslastungswerte der neuen Implementierung unter echter GPU-/NPU-Last sind
weiter auf dem NAS zu prüfen; synthetische Zähler ersetzen diese Prüfung nicht.

## Protokolle über einen Neustart (seit 0.41.0)

Das Systemprotokoll läuft im RAM. Zusätzlich schreibt TitanOS es je Start in
eine Datei auf dem ersten gesunden Volume (`.titanos/logs/`, Größe begrenzt,
die ältesten Starts fallen weg), nie auf den Stick. Unter Werkzeuge ›
Protokolle lässt sich deshalb auch der **vorige Start** ansehen, etwa nach
einem Absturz; der Download liefert bis 50.000 Zeilen. Absturzdaten des
Kernels (pstore) werden beim nächsten Start dorthin gesichert. Auch die
Meldungen der Glocke überstehen so einen Neustart. Ohne Volume bleibt nur das
Protokoll im RAM.

## Temperaturen

Unter Einstellungen → Temperaturen wählt jedes Konto seine Sensoren und die
Sichtbarkeit der Dashboard-Kachel. Die Auswahl liegt zusammen mit dem Layout
in den bestehenden Dashboard-Präferenzen. Jede Temperaturkachel kann im Editor
statt dieser Auswahl eigene Sensoren zeigen (`items` in `tileOptions`), damit
Kopien der Kachel z. B. CPU und NVMe getrennt darstellen. Die CPU-Kachel nimmt
ab Werk den Paketsensor (coretemp „Package id 0“, k10temp/zenpower „Tctl“),
optional einen ausdrücklich gewählten Sensor (`sensor`); fehlt er, bleibt der
Wert leer statt auf einen anderen zu wechseln. Die Beschleunigerkachel zeigt die
GPU-Temperatur nur einmal: in den Details, ohne Details neben dem Zustand.
Messwerte werden im Hintergrund aus
hwmon gelesen und nur im RAM gehalten; keine zyklischen Zugriffe auf `/config`.

Sensoren werden über Gerätepfad, Treibername und Kanal identifiziert, nicht über
die beim Boot wechselnde hwmon-Nummer. Fehlende ausgewählte Sensoren werden
nicht durch andere ersetzt. Fehlerhafte, ausgeschaltete oder suspendierte
Sensoren zeigen keinen Messwert. Grenzwerte stammen ausschließlich vom Treiber.
CPU-, Mainboard- und RAM-Sensoren erscheinen, soweit der Kernel sie bereitstellt;
unbeschriftete Kanäle bekommen keine erfundene Zuordnung. Laufwerkstemperaturen
bleiben bei der vorhandenen Speicherüberwachung, damit hwmon-Abfragen keine
schlafenden Laufwerke aufwecken. Es werden keine Sensorchips aktiv sondiert oder
Treiber erzwungen.

Schnittstellen:
[proc/meminfo](https://docs.kernel.org/filesystems/proc.html),
[i915 PMU](https://kernel.googlesource.com/pub/scm/linux/kernel/git/torvalds/linux/+/v6.12/drivers/gpu/drm/i915/i915_pmu.c),
[DRM-fdinfo](https://docs.kernel.org/gpu/drm-usage-stats.html),
[Intel-NPU-Zähler in Linux 6.12](https://kernel.googlesource.com/pub/scm/linux/kernel/git/torvalds/linux/+/v6.12/drivers/accel/ivpu/ivpu_sysfs.c),
[hwmon](https://docs.kernel.org/hwmon/sysfs-interface.html).

## Dashboard nach Beta 5

CPU, Arbeitsspeicher und Systeminfos sind getrennte Kacheln. Vorhandene Layouts
werden einmal ergänzt; die bisherige Systemkachel wird nicht verschoben oder
verkleinert. Ab Layoutversion 3 bleiben auch bewusst entfernte CPU-/RAM-Kacheln
entfernt. Der Bearbeitungsmodus bietet für jede Kachel Breite/Höhe in Rasterzellen.
Scrollende Inhalte dürfen die vom Nutzer gewählte Größe nicht verändern.

Bei CPU/RAM und Beschleunigern lassen sich Zusatzwerte und Verlauf getrennt
schalten. Die CPU-Kachel kann den vorhandenen coretemp-Sensor „Package id 0“
als Zahl anzeigen. Der CPU-/RAM-Verlauf umfasst etwa drei Minuten im Browser;
eine Beobachtungslücke unterbricht den Verlauf. Netzwerk zeigt auf Wunsch IPv4,
IPv6, Gateways, MAC, MTU, Linktempo und Verkehr; die Schnittstelle ist wählbar.
Ein verschwundener ausgewählter Anschluss wird nicht stillschweigend ersetzt.

VM-/Container-Kacheln zeigen kompakt den Namen sowie CPU und RAM. VM-RAM ist
der resident belegte Speicher des QEMU-Prozesses (VmRSS), nicht das konfigurierte
RAM-Limit oder allein die Anwendungen im Gast. Deshalb kann eine VM mit 8 GiB
zugewiesenem Speicher beispielsweise 3,12 GiB resident belegen. Die NAS-Summe
bleibt `MemTotal − MemFree` und zählt das laufende System mit. Die zugewiesenen
8 GiB dürfen nicht zusätzlich auf diese Summe addiert werden.

## Optionale längere Verläufe ab 0.16.0

Werkzeuge › Monitoring ergänzt Minutenwerte auf einem Daten-Volume: CPU, RAM,
Netzwerk, Volume-Belegung sowie physische I/O-Zähler und mittlere Latenz. Ab Werk
aus, 7/30 Tage und 8–256 MiB. Kein periodisches Schreiben auf den Bootstick.
Beobachtungslücken/Zählerrücklauf bleiben erkennbar; Speicherung kann ruhende
Datenplatten wecken. Die kurzen RAM-Verläufe des Dashboards bleiben erhalten.
Einzelheiten, Fehler-/Aufbewahrungsregeln und Prüfgrenzen in
[WORKFLOWS.md](WORKFLOWS.md#längere-monitoring-verläufe).

Seit 0.17.0 können die Werte mit Messlücken als CSV/JSON exportiert werden.
Die optionale Kapazitätsentwicklung auf derselben Seite verwendet nur ausreichend
lange, frische zusammenhängende Messungen. Fehlende oder stark schwankende Daten
bleiben unbekannt. Hinweise sind auf der geladenen Seite sichtbar; es gibt dafür
keine zusätzlichen Push-Meldungen. Genaue Grenzen in [WORKFLOWS.md](WORKFLOWS.md).

## Dashboard-Editor und MQTT seit 0.18.0

Der [persönliche Editor](design/PersonalDashboard.md) ergänzt Typografie, Flächen,
Messwertdarstellungen und mehrere unabhängige Kachelinstanzen auf einer Startseite.
Alte gespeicherte Layouts bleiben erhalten. MQTT nutzt die vorhandenen
Beschleunigermesswerte und begrenzte stündliche RAM-Beobachtungen des Recorders
für Prognosen; es liest dafür keine Historie nach. Einmaliges Vorwärmen erfolgt
im Recorder, nicht im MQTT-Takt. Datenlücken, Volume-Wechsel und alte Daten
verhindern falsche Prognosen. Details: [Home Assistant](HOME-ASSISTANT.md).

## Darstellung der Messwerte

Im Dashboard-Editor sind sanfte Übergänge für Balken, Ringe und Verläufe
abschaltbar und zwischen 150 und 1000 ms einstellbar (Standard 450 ms).
Die Einstellung gilt auch für die entsprechenden Docker-, VM- und
Speicheranzeigen. Die Bewegungsreduktion des Browsers hat Vorrang.
Es werden ausschließlich Anzeigeübergänge berechnet: Erfassungsintervalle,
Monitoring-Daten und MQTT-Werte bleiben unverändert. Fehlende Werte bleiben
unbekannt bzw. Lücken. Feste Verlaufsslots verhindern Strecksprünge bei kurzen
Verläufen; die Docker-Skala wächst bei Bedarf weich und bleibt während der
Anzeige stabil. Verdeckte Seiten beenden ihre laufenden Übergänge.

## CPU-Leistung (seit 0.35.0)

Die Leistung der CPU-Pakete kommt aus den Energiezählern des Kernels
(powercap/RAPL, `/sys/class/powercap/*rapl:N/energy_uj`, nur die
Paket-Zonen, Überlauf über `max_energy_range_uj`), gemittelt über das
Messintervall des Systemmonitors (fünf Sekunden). Es ist nur die CPU, nicht
das ganze NAS: Platten, Lüfter, Mainboard und Netzteil fehlen. Auf Intel und
AMD ab Zen; ohne Zähler (auch in VMs) bleibt der Wert unbekannt. Anzeige als
Feld „CPU-Leistung“ der Systeminfo-Kachel und im Bericht über das NAS; API
`cpu.powerWatts` in der Systeminfo.

## Taskmanager (PLAN 61, Monitor-App)

Seite `/tools/taskmanager` (Werkzeuge › Beobachten › Taskmanager, im Desktop-Modus
die App „Monitor“). Sie fragt alle zwei Sekunden nach, nur solange die Seite
sichtbar ist, und nur, was der offene Reiter braucht.

- **Übersicht** (auch für Benutzer, nur lesen): CPU gesamt und je Kern, RAM,
  Netzwerk der Standardschnittstelle, Durchsatz je Volume, Grafik/Beschleuniger
  mit gemeldeter Auslastung; kleine Verläufe der letzten 80 Sekunden im Browser.
  Quelle ist `GET /api/v1/system`; das Feld `disks` (`name`,
  `readBytesPerSecond`, `writeBytesPerSecond`, `busyPercent`) kommt aus den
  Zählern in `/sys/block/<name>/stat`, die `system.Monitor` alle zwei Sekunden
  liest. Das Lesen dieser Kernelzähler schickt nichts an die Platte und weckt
  keine. Der Durchsatz eines Volumes ist die Summe seiner Platten; bei Spiegeln
  zählt jedes Schreiben je Platte (was die Platten tun, nicht was Programme
  schrieben).
- **Prozesse** (Besitzer/Administratoren): `GET /api/v1/system/processes` liefert
  `{processes: [{pid, ppid, name, command, user, state, cpuPercent, memoryBytes,
  start, owner: {kind, id, name}, protected}], cpus, memoryTotalBytes,
  sampledSeconds}`. Gelesen wird nur `/proc` (`stat`, `status`, `cmdline`,
  `cgroup`) und `/etc/passwd`; Kernel-Threads und Threads fehlen. CPU ist in
  Prozent des ganzen NAS (100 = alle CPUs voll) und wird gegen die vorige
  Abfrage gerechnet; ist die älter als zehn Sekunden, nimmt titand zwei Proben
  im Abstand von 300 ms. Speicher ist RSS. Die Zugehörigkeit kommt aus der
  cgroup: `docker-<id>.scope` (Name über die Docker-API, fünf Sekunden
  zwischengespeichert), `machine-qemu\x2d<n>\x2d<name>.scope` (VM),
  `lxc.payload.<name>`/`titanos-lxc@<name>.service` (Systemcontainer), sonst der
  systemd-Dienst. Lange Namen kürzt systemd selbst; dann steht der gekürzte Name da.
- **Beenden/Erzwingen:** `POST /api/v1/system/processes/{pid}/signal` mit
  `{signal: "term"|"kill", start}` (SIGTERM bzw. SIGKILL), nur aus einer
  Web-Sitzung (`browserOnly`), nicht in `publicRoutes`, nicht im MCP-/Agent-
  Werkzeugkatalog. `start` ist die Startzeit aus der Liste; passt sie nicht
  mehr, ist die Nummer neu vergeben (409 `process_changed`). Abgelehnt werden
  PID ≤ 2, Kernel-Threads, Thread-IDs, titand, sein Elternprozess und alle von
  titand gestarteten Prozesse samt Nachkommen (also auch Shells der NAS-Konsole
  und was darin läuft), dazu die für den Betrieb nötigen Dienste `dockerd`,
  `containerd`, `containerd-shim*`, `sshd` (auch `sshd-session`/`sshd-auth`),
  `systemd-journald`, `systemd-udevd`, `smbd`, `nmbd`, `mdadm`, `libvirtd` und
  `virtqemud` (Name aus `/proc/<pid>/stat`, 403 `process_protected`; die Liste
  zeigt sie als geschützt) sowie alles im Entwicklungsmodus (409 `dev_mode`).
  Das Signal geht über einen pidfd (`pidfd_open`, danach wird die Startzeit
  erneut gelesen, dann `pidfd_send_signal`), sodass eine inzwischen neu vergebene
  Prozessnummer nie getroffen wird. Jede Anfrage steht im Aktivitätsprotokoll mit
  Prozessname und Signal.
- **Container, VMs und Systemcontainer:** die vorhandenen Listen und Aktionen
  (`/docker`, `/vms`, `/lxc`); Stoppen und Neustarten mit einfachem
  Bestätigungsdialog.
- **Netzwerk:** jede Schnittstelle mit Zustand, Linktempo und Durchsatz.
- **Platten:** Zustand aus dem Ruhemodus (`schläft`/`aktiv`), Temperatur aus dem
  zuletzt gelesenen SMART-Wert, Durchsatz und Belegung aus `disks`. Schlafende
  Platten zeigen keinen Durchsatz und werden nie geweckt. `/storage` wird nur
  alle 30 Sekunden gelesen.

Prüfung: Go-Tests `internal/procs` (Fixture-`/proc` in `testdata`, nie der
Host), `internal/system/diskio_test.go`, `internal/api/processes_test.go`
(Rollen, Tokens, MCP, geschützte PIDs, Aktivitätsprotokoll), Vitest
`tests/lib/taskmanager.test.ts`, UI `scripts/test-ui-taskmanager.mjs` (1440, 800,
390 px). Nicht auf echter Hardware geprüft: echte cgroup-Pfade von libvirt und
LXC sind aus der Dokumentation bzw. dem Code von `internal/lxc` nachgebildet.
