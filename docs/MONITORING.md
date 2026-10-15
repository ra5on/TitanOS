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
