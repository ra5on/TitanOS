# SSD-Cache (ab 0.29.0)

Eine freie SSD kann ein mdadm-Volume (ext4/XFS) beschleunigen. Der Cache ist
**ab Werk aus** und wird je Volume auf der Speicher-Seite eingeschaltet
(„SSD-Cache › Einschalten …“). Btrfs-Volumes haben keinen SSD-Cache.

## Modi

| Modus | dm-cache | Was passiert, wenn die SSD ausfällt |
|---|---|---|
| **Lesecache** (Standard) | `writethrough` | nichts geht verloren; ohne SSD wird das Volume direkt eingebunden, es gibt eine Meldung. Weil das Array dann ohne den Cache beschrieben wird, merkt sich TitanOS das (`stale`) und beginnt beim nächsten Start mit SSD mit leerem Cache (Metadaten genullt) |
| **Lese- und Schreibcache** | `writeback` | noch nicht zurückgeschriebene Daten gehen verloren, das Dateisystem kann beschädigt werden; ohne SSD bleibt das Volume **ausgehängt** |

Der Schreibcache ist eine bewusste Wahl mit Warnung im Dialog; empfohlen nur
mit zuverlässiger SSD und USV. Der Modus lässt sich im laufenden Betrieb
wechseln (`dmsetup reload` mit kurzem `suspend`/`resume`).

## Technik

- Die SSD wird vollständig gelöscht und bekommt eine GPT mit zwei
  Partitionen: Metadaten (mindestens 64 MiB, 32 Byte je Cache-Block) und
  Cache-Blöcke (256 KiB). `volumes.json` speichert im optionalen Feld `cache`
  Modus, Seriennummer und die PARTUUIDs; ältere Versionen ignorieren es und
  binden das Array direkt ein (bei Lese- und Schreibcache mit dem Risiko
  veralteter Daten: vor einem Rückweg auf 0.28.x den Cache ausschalten).
- Beim Einbinden legt titand `/dev/mapper/titanos-cache-<volume>` an (Ziel
  `cache` über `/dev/mdX`, Policy `smq`) und bindet dieses Gerät statt
  `UUID=` ein. Das Einschalten wird deshalb mit dem **nächsten Start**
  wirksam; Array und Dateisystem bleiben unverändert.
- Die Cache-SSD zählt als Mitglied des Volumes und wird nirgends sonst
  angeboten. Erweitern (RAID5/6) ist mit Cache gesperrt
  (`volume_cache_configured`).
- Ausschalten: Ein Schreibcache wird zuerst mit der Policy `cleaner`
  vollständig zurückgeschrieben (`removing`). Sobald `dmsetup status` keine
  „dirty“ Blöcke mehr zeigt, wechselt er auf `writethrough` (ab da wird kein
  Block mehr „dirty“), das wird erneut geprüft, und erst dann bekommt
  `/dev/mapper/titanos-cache-<volume>` per `dmsetup reload` eine `linear`-
  Tabelle direkt auf das Array: Das eingebundene Volume läuft weiter, die SSD
  ist frei, und beim Herunterfahren geht nichts verloren. Danach wird der
  Cache vergessen (Meldung `volume_cache_removed`), und titand nullt das
  erste MiB beider SSD-Partitionen (und verwirft die Cache-Partition per
  `blkdiscard`, wo die SSD das kann): Der erste Cache-Block kann eine Kopie
  des Array-Anfangs mit Dateisystem-Signatur und UUID des Volumes sein, und
  udev sähe das Dateisystem dann beim nächsten Start ein zweites Mal auf der
  SSD. Die lineare Zuordnung bleibt bis zum nächsten Start. Ein Lesecache ohne offene Blöcke wird sofort
  so abgelöst. Ein Schreibcache, der nicht läuft, obwohl die SSD da ist (z. B.
  gesperrtes Volume), lässt sich erst ausschalten, wenn er läuft; fehlt die
  SSD, wird er auf Wunsch vergessen (seine offenen Blöcke sind dann ohnehin
  verloren). Fehlt die SSD während `removing`, bleibt das Volume ausgehängt.
- Löschen eines verschlüsselten Volumes mit Cache: erst zurückschreiben und
  `writethrough` (höchstens 5 Minuten, sonst „beschäftigt“, später erneut),
  dann `cryptsetup erase` und `wipefs` durch den Cache, dann das Cache-Gerät
  entfernen; der Anfang beider SSD-Partitionen wird genullt und die
  Cache-Partition per `blkdiscard` verworfen, wo die SSD das kann.
- Die PARTUUIDs der Cache-Partitionen zählen zu `storage.Owns`: Die SSD
  erscheint nicht unter „Laufwerk formatieren“.
- Die Speicher-Seite zeigt Belegung, Lesetreffer und offene Blöcke aus
  `dmsetup status`.
- `dmsetup` und das Kernelmodul `dm-cache` sind im Abbild; es ist kein neues
  Paket nötig.

Mit einem verschlüsselten Volume liegt der Cache **unter** der
Verschlüsselung (`md → titanos-cache-… → titanos-crypt-… → Dateisystem`): Die
SSD hält nur verschlüsselte Blöcke. Der Cache wird beim Entsperren eingerichtet
und wird damit erst nach dem nächsten Entsperren wirksam; siehe
[ENCRYPTION.md](ENCRYPTION.md).

Eine Sicherung des Sticks, die bei einem Stickwechsel von einem Volume mit
Schreibcache gelesen wird, kommt direkt vom Array und kann älter sein als der
letzte Stand auf der SSD.

## Prüfstand

Unit-Tests (Vorbereitung der SSD, erzeugte Tabelle, Einbinden mit und ohne
SSD, Schreibcache ohne SSD, Zurückschreiben vor dem Entfernen, Auswertung von
`dmsetup status`, Löschen mit belegtem Cache-Gerät) prüfen die Logik gegen ein
nachgebildetes `dmsetup`.

Mit echtem device-mapper im Gast prüft `scripts/smoke-cache.py` (sechs
UEFI-Starts in QEMU, nur Wegwerf-qcow2-Platten; im Release-Workflow bei jedem
PR-Probelauf und jedem Tag, Ergebnis `out/validation/cache/cache-result.json`
im Artefakt `release-boot-log`). Volumes: `plain` (mdadm RAID1, ext4) mit
Cache auf SSD1 und `secret` (verschlüsseltes RAID1, ext4) mit Cache auf SSD2;
SSDs und eine freie Kontrollplatte hängen an SATA (AHCI), weil
`GET /usb/disks` virtio-Platten nie anbietet.

- **a)** Schreibcache einschalten, wirkt erst nach dem Neustart; danach ist
  `/dev/mapper/titanos-cache-plain` eingebunden, `dmsetup status` zeigt
  `writeback` und offene Blöcke nach dem Schreiben.
- **b)** Schreibcache ausschalten, während ein Schreiber im Gast 24 × 8 MiB
  schreibt (Prüfsummen beim Schreiben gebildet): der Cache verschwindet aus
  `volumes.json`, das Gerät hat danach eine `linear`-Tabelle, Meldung
  `volume_cache_removed`, die SSD erscheint wieder unter „Laufwerk
  formatieren“. Nach dem Neustart wird das Array direkt eingebunden, alle
  Prüfsummen stimmen, `e2fsck -fn` ist sauber (eingefrorenes Dateisystem,
  schreibgeschütztes Loop-Gerät auf dem Array) und `dmesg` zeigt keine
  ext4-/I/O-Fehler.
- **c)** Lesecache: ein Start ohne SSD bindet ohne Cache ein
  (`volume_cache_missing`, `stale` in `volumes.json`), eine zuvor in den
  Cache gelesene Datei wird an Ort und Stelle überschrieben; der nächste
  Start mit SSD beginnt kalt (belegte Blöcke 2 statt 132), `stale` ist weg,
  und nach `drop_caches` sowie mit `O_DIRECT` werden die neuen Daten gelesen.
- **d)** Löschen des verschlüsselten Volumes mit Schreibcache: auf beiden
  früheren md-Mitgliedern und auf der ganzen Cache-SSD keine LUKS-Signatur
  mehr (vorher auf allen dreien vorhanden), `cryptsetup isLuks` auf keinem
  Gerät. Die SSD ist mit `discard=ignore` angebunden: `blkdiscard` gelingt,
  verwirft aber nichts – der strenge Fall.
- **e)** Die Cache-SSDs fehlen in `GET /usb/disks`, auch während `secret`
  nach dem Neustart gesperrt ist; die Kontrollplatte erscheint.
- **f)** Wechsel `writeback` → `writethrough` per API mit offenen Blöcken:
  danach 0 offene Blöcke, `dmsetup status` und Tabelle `writethrough`,
  gespeicherter Modus geändert, Daten unverändert.

Der erste Lauf fand einen Fehler, der behoben ist: Beim Löschen eines
verschlüsselten Volumes mit Cache war `titanos-cache-<volume>` nach `wipefs`
kurz belegt (udev prüft das gerade beschriebene Gerät), `dmsetup remove`
scheiterte, und ein zweiter Versuch blieb an „kein LUKS-Gerät“ hängen.
Seitdem wartet titand auf udev und versucht das Entfernen erneut, und ein
bereits gelöschter LUKS-Kopf gilt als gelöscht.

Der Release-Probelauf (PR #205, echtes Abbild, TCG ohne KVM) fand einen
zweiten: Nach dem Ausschalten eines Schreibcaches und einem Neustart fehlte
die freie SSD unter „Laufwerk formatieren“. Ursache: Die Cache-Partition
begann mit einer Kopie des ersten Array-Blocks (ext4-Signatur mit der UUID
von `plain`, die TitanOS als eigenes Volume erkennt); udev sieht das erst beim
nächsten Start. Ob der erste Cache-Block diese Kopie hält, hängt davon ab,
welcher Block zuerst in den Cache kommt, deshalb trat es lokal mit KVM nicht
auf. Seitdem nullt titand beim Vergessen eines Caches den Anfang beider
SSD-Partitionen (wie schon beim Löschen), und der Test prüft genau das
(erstes MiB beider Partitionen null, keine Signatur) zusätzlich zur Liste.

Lokal gelaufen am 30.10.2026 mit beiden Korrekturen, mit KVM (4 Minuten)
und mit `--tcg` wie im CI ohne KVM (8 Minuten), alle Phasen grün; die
Doppel-Signatur selbst trat lokal in keinem Lauf auf:
[`docs/validation/cache-qemu-2026-10-30.json`](validation/cache-qemu-2026-10-30.json).
Weil hier kein mkosi-Bau möglich war, lief das Abbild von 0.32.0 mit dem
titand dieses Stands (und dessen `fstab`/`titand.service`); Kernel, dm-cache
und Werkzeuge stammen damit aus 0.32.0.

Grenzen:

- Nur QEMU-Platten, keine echten SSDs; Leistung, Trefferquoten unter echter
  Last und Verhalten bei einer SSD, die im Betrieb ausfällt, sind nicht
  geprüft. Eine Cache-SSD fehlt nur beim Start (Platte nicht angeschlossen).
- Beim Ausschalten unter Last löst titand den Schreibcache erst, wenn in einem
  Moment keine offenen Blöcke vorliegen; im Test war das erst nach dem Ende
  des Schreibers so. Ein Volume mit Dauerlast kann also lange in `removing`
  bleiben.
- Schreibcache ohne SSD beim Start (Volume bleibt ausgehängt), Ausschalten
  eines gesperrten Volumes und Stromausfall mit offenen Blöcken prüft nur
  der Unit-Test.
- XFS und der Rückweg auf ältere Versionen sind nicht Teil des Tests.
