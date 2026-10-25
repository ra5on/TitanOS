# RAM, Writeback und Uploads

`MemTotal - MemFree` zählt den gesamten belegten RAM genau einmal, einschließlich
Kernel, komprimiertem Systemabbild/tmpfs, QEMU, Containern und Dateicache.
`MemAvailable` bleibt die separate Kernel-Schätzung für neue Aufgaben. Die
Cacheanzeige verwendet `Cached - Shmem + Buffers + SReclaimable`, begrenzt auf den
belegten RAM; sie ist keine Garantie, dass jeder einzelne Cache-Byte sofort
freigegeben werden kann.

Die RAM-Kachel stellt **verfügbaren RAM** einschließlich seines Prozentwerts in
den Vordergrund. Der Verlauf zeigt ebenfalls verfügbaren RAM. Eine zweite
Belegungsleiste unterscheidet übrige Belegung und Dateicache; der Rest ist
physisch frei. „Übrige Belegung“ umfasst unter anderem Anwendungen, Kernel und
tmpfs, nicht nur Anwendungsdaten. `MemAvailable` überlappt diese Anteile und
wird nie zur Gesamtbelegung addiert. Das komprimierte Systemabbild und das
512-MiB-Overlay sind bereits enthalten und kein frei verwerfbarer Lesecache.

Der gemeinsame Systemmonitor liest alle zwei Sekunden RAM und optional
`/proc/pressure/memory`. PSI `someAvg10`/`fullAvg10` sind die prozentualen
Wartezeiten mindestens einer bzw. aller aktiven Aufgaben in den letzten zehn
Sekunden. Fehlende, unvollständige oder ungültige Werte bleiben unbekannt.
Hoher Cache allein erzeugt keine Warnung. Erst mindestens 30 Sekunden mit
`MemAvailable <= Reserve` aktivieren `lowAvailable`; zum Aufheben sind 60
Sekunden oberhalb der Reserve nötig. Beobachtungslücken zählen nicht als
anhaltender Speichermangel oder Erholung. Dieser Zustand liegt nur im RAM und
ändert keine Container-/VM-Zuordnung. Die API-Felder sind optional ergänzt;
bestehende Bytewerte und `cachePercent` behalten ihre Bedeutung.

Die persistente Wahl liegt in `/config/system/memory.json` (`cachePercent`, 10–90
in Zehnerschritten, Standard 20). Die laufende Regelung liest diese Datei nicht
zyklisch. Reserve: 10 % des Gesamt-RAM, mindestens 256 MiB und höchstens 2 GiB,
auf sehr kleinen Systemen höchstens ein Viertel des Gesamt-RAM. Das Zielbudget
beträgt den gewählten Anteil von `max(0, MemAvailable - Reserve)`.
Die Oberfläche bezeichnet diesen Anteil ausdrücklich als **Schreibpufferbudget
für Dateiübertragungen**. Er ist keine Obergrenze für den gesamten Dateicache,
für Container oder für VM-RAM. Linux verwaltet den Lesecache weiter selbst.

Alle fünf Sekunden setzt TitanOS `vm.dirty_bytes` auf das Budget und
`vm.dirty_background_bytes` auf die Hälfte. Bei erschöpfter Reserve bleibt eine
kleine Writeback-Schwelle von 1 MiB, während verwaltete Uploads pausieren. Die
zugehörigen Prozent-Sysctls werden vom Kernel deaktiviert. Änderungen und
Konfigurationsfehler stellen die vorherige aktive Byte-/Prozentkombination wieder
her. Im Entwicklungsmodus werden keine Host-Sysctls verändert.

Browser-Uploads reservieren gemeinsam höchstens das aktuelle Budget. Jede
Reservierung liest die Kapazität frisch; sie wartet nicht auf den Anzeigetakt des
Systemmonitors. PSI wird dabei nicht erneut gelesen. Ein Fenster
ist maximal 8 MiB oder ein Viertel des Budgets. Nach jedem Fenster: `fsync`,
Bestätigung des Offsets, `posix_fadvise(FADV_DONTNEED)` für abgeschlossene Seiten,
Freigabe der Reservierung. `FADV_DONTNEED` ist ein gezielter Hinweis, keine harte
Cache-Garantie. Es gibt kein globales `drop_caches`. SMB/NFS nutzt die
Writeback-Schwellen, aber keinen gezielten Cache-Abwurf; wiederverwendbarer
Lesecache bleibt unter Linux-Verwaltung.

Unfertige Uploads haben eine verborgene `.titanos-part`-Datei und Metadaten daneben
auf dem Daten-Volume. Die Dateimanageransicht findet sie im jeweils geöffneten
Ordner. Nur das zugehörige Konto mit aktuellen Schreibrechten kann fortsetzen;
beim erneuten Auswählen wird der vorhandene Datei-Anfang vollständig verglichen.
Metadaten werden nur als reguläre, von titand besessene 0600-Dateien übernommen,
keine symbolischen Links oder Hardlinks. Fortsetzung prüft Volume- und
Dateiidentität. Bekannte aktive Uploads reservieren ihren Restbedarf gemeinsam;
nach einem Dienstneustart werden Reservierungen beim Öffnen des jeweiligen
Ordners wiederhergestellt. Andere Schreiber wie SMB können weiterhin Platz
verbrauchen; tatsächliche ENOSPC-/Sync-Fehler werden nicht als Erfolg bestätigt.

Prüfungen: künstliche sysctl-Dateien, Schreibfehler, Speicherdruck, Abbruch,
Volumewechsel, Quellenvergleich, fremde Uploads und Platzreservierungen. Der
QEMU-Speichertest prüft echte Dateien, Dienstneustart, Prüfsummen und persistente
Einstellungen auf Wegwerfplatten. Langzeitlast auf echter NAS-Hardware bleibt
separat zu prüfen.
