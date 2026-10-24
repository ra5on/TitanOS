# RAM, Beschleuniger und Temperaturen

## RAM

Das Dashboard zeigt den gesamten vom Kernel verwalteten belegten RAM:
`MemTotal - MemFree` aus `/proc/meminfo`. Darin sind Programme, VMs, Container,
Kernel, Dateicache und tmpfs einschließlich des TitanOS-Abbilds bereits enthalten.
Sie werden nicht noch einmal addiert. `MemAvailable` wird separat als verfügbar
angezeigt; dieser Wert berücksichtigt wieder freigebbaren Cache. Installierter,
von Firmware oder Geräten reservierter Speicher ist nicht Teil von `MemTotal`.

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

## Temperaturen

Unter Einstellungen → Temperaturen wählt jedes Konto seine Sensoren und die
Sichtbarkeit der Dashboard-Kachel. Die Auswahl liegt zusammen mit dem Layout
in den bestehenden Dashboard-Präferenzen. Messwerte werden im Hintergrund aus
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
