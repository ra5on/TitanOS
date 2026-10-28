# Volume-Verschlüsselung (LUKS2)

Ein Volume lässt sich beim Anlegen verschlüsseln („Verschlüsseln (LUKS2)“ im
Dialog „Volume anlegen“). Das ist **ab Werk aus** und wird je Volume gewählt;
ein bestehendes Volume lässt sich nicht nachträglich verschlüsseln (die Daten
müssten dafür umkopiert werden). Entscheidung des Projektinhabers vom
28.10.2026.

Die Verschlüsselung schützt die Daten auf ausgebauten, gestohlenen oder
entsorgten Platten. Sie schützt **nicht** vor jemandem, der das laufende,
entsperrte NAS bedient, und nicht vor Schadsoftware auf Rechnern, die auf die
Freigaben zugreifen.

## Entsperren nach dem Start

- Es gibt **kein gespeichertes Passwort, keine Schlüsseldatei und keinen
  Schlüssel auf dem Stick.** Nach jedem Start des NAS bleibt ein
  verschlüsseltes Volume gesperrt, bis jemand auf der Speicher-Seite
  „Entsperren“ wählt und das Passwort eingibt.
- Solange es gesperrt ist, läuft nichts, was darauf liegt: Freigaben, die
  Docker-Engine samt Apps (wenn Docker auf diesem Volume liegt), VMs,
  LXC-Container, Sicherungen auf dieses Volume, Volltextindex, Vorschaubilder,
  Aktivitätsprotokoll usw. Sie alle fragen ohnehin, welche Volumes eingebunden
  sind, und starten nach dem Entsperren wie nach einem gewöhnlichen Start
  (Autostart von VMs und Containern eingeschlossen).
- Nach dem Start gibt es einmal die Meldung `volume_locked` („Volume … wartet
  auf sein Passwort“), sobald die Platten des Volumes da sind. Die Meldung und
  die Volume-Kachel des Dashboards führen zum Volume auf der Speicher-Seite,
  die Statusleiste zeigt es als „gesperrt“.
- Ein Neustart von titand (etwa durch ein App-Update) sperrt nichts: Die
  geöffneten Geräte bleiben offen.
- Der Boot-Check nach einem Systemupdate zählt ein gesperrtes Volume als
  „wieder da“ (auch Plugins auf ihm): Sonst würde ein Update zurückgerollt,
  nur weil innerhalb von fünf Minuten niemand das Passwort eingegeben hat.

Falsche Passwörter werden je Volume gezählt. Nach fünf falschen Versuchen muss
jeder weitere warten: 30 Sekunden, dann jeweils doppelt so lange, höchstens 15
Minuten. Jeder Versuch kostet ohnehin die Zeit der Schlüsselableitung (rund
zwei Sekunden). Der Zähler liegt nur im RAM. Entsperren, Sperren und jede
Passwortänderung stehen im Aktivitätsprotokoll (nur der Volume-Name, nie ein
Passwort).

**Passwörter kommen nur aus einer angemeldeten Websitzung.** Der KI-Assistent,
Home Assistant und persönliche API-Tokens können nicht entsperren, keine
Passwörter ändern und keine Header-Sicherung laden (`web_session_required`);
die Routen stehen in keinem Katalog.

## Sperren

„Sperren“ (einfacher Bestätigungsdialog) beendet zuerst, was das Volume nutzt:
Freigaben werden abgemeldet und offene SMB-Verbindungen geschlossen, Docker und
die übrigen Dienste sehen das Volume ab sofort als nicht eingebunden, laufende
VMs werden heruntergefahren (nach zwei Minuten ausgeschaltet), LXC-Container
gestoppt. Dann wird das Volume ausgehängt (bis zu drei Minuten Versuche) und
die LUKS-Geräte werden geschlossen. Lässt es sich nicht aushängen, bleibt alles
wie vorher und die gestoppten VMs/Container starten wieder (`volume_busy`).
Nach dem nächsten Entsperren starten die beim Sperren gestoppten VMs und
Container wieder. Ein absichtlich gesperrtes Volume ergibt keine Meldung.

## Passwörter

- **Passwort ändern:** Erst bekommt das neue Passwort einen eigenen Schlüssel-
  platz auf jedem LUKS-Gerät des Volumes, danach wird der Platz des alten
  entfernt. Vorher wird das alte auf allen Geräten geprüft.
- **Weiteres Passwort:** zusätzlicher Schlüsselplatz, zum Beispiel ein zweites
  Passwort, das sicher verwahrt wird.
- **Passwort entfernen:** nur, wenn mindestens ein weiteres bleibt.
- 8 bis 512 Zeichen. Das Passwort ist genau die eingegebene Zeichenfolge
  (UTF-8); wer es später von Hand mit `cryptsetup open` eingibt, tippt dasselbe.

## Technik

- **mdadm-Volumes:** ein LUKS2-Container auf dem Array (`/dev/md/<name>`), das
  Dateisystem (ext4/XFS) darin. Gerät `/dev/mapper/titanos-crypt-<volume>`.
- **Btrfs-Volumes:** ein LUKS2-Container je Mitgliedspartition, alle mit
  demselben Passwort; Btrfs erstreckt sich über die geöffneten Geräte
  `/dev/mapper/titanos-crypt-<volume>-<uuid8>` (die ersten acht Zeichen der
  LUKS-UUID, damit der Name nicht von der Plattenreihenfolge abhängt).
  Entsperren öffnet alle vorhandenen Mitglieder; passt das Passwort auf eines
  nicht, wird keines offen gelassen. Fehlt ein Mitglied, gelten die üblichen
  Regeln für degradiertes Einbinden (60 Sekunden Wartezeit, nur mit zweiter
  Kopie).
- `volumes.json` bekommt das optionale Feld `encryption` mit den LUKS-UUIDs –
  sonst nichts. Ältere titand ignorieren es; sie finden das Dateisystem nicht
  und lassen das Volume ausgehängt (kein Datenverlust, aber auch kein Zugriff,
  bis wieder eine Version mit Verschlüsselung läuft).
- Das Passwort steht nie auf einer Kommandozeile: cryptsetup liest es von
  stdin (`--key-file=-`), ein neues Passwort beim Ändern von einem
  Pipe-Deskriptor (`/dev/fd/3`). Es wird nicht protokolliert und nach Gebrauch
  im Speicher überschrieben, soweit Go das zulässt (der JSON-Text der Anfrage
  bleibt bis zur Speicherbereinigung im RAM).
- Geöffnet wird mit `--disable-keyring`: Der Volume-Schlüssel steht in der
  dm-crypt-Tabelle statt im Kernel-Schlüsselbund. So kann `cryptsetup resize`
  nach dem Erweitern eines RAID 5/6 ohne erneute Passworteingabe laufen. root
  kann den Schlüssel eines entsperrten Volumes ohnehin auslesen.

### Schlüsselableitung

`cryptsetup luksFormat --type luks2 --pbkdf argon2id --pbkdf-memory 262144
--pbkdf-parallel 4 --iter-time 2000` (ebenso für neue Schlüsselplätze):
Argon2id mit **256 MiB** Speicher, vier Bahnen und so vielen Durchläufen, wie
auf diesem NAS etwa zwei Sekunden dauern. 256 MiB statt der 1 GiB, die
cryptsetup sonst nimmt, weil TitanOS komplett im RAM läuft und NAS-Boards oft
4–8 GiB haben; der Speicher wird nur für den Moment des Entsperrens gebraucht.
Bei wenig freiem RAM senkt cryptsetup den Wert selbst. Verschlüsselung:
Standard von cryptsetup 2.7 (`aes-xts-plain64`, 512-Bit-Schlüssel, Sektorgröße
automatisch). Ein Btrfs-Volume mit drei Platten braucht beim Entsperren drei
Ableitungen nacheinander, also rund sechs Sekunden.

### Reihenfolge mit dem SSD-Cache

**dm-cache liegt unter der Verschlüsselung:** `md → titanos-cache-<volume> →
titanos-crypt-<volume> → Dateisystem`. Die Cache-SSD speichert damit nur
verschlüsselte Blöcke; ihr Inhalt verrät nichts, auch nicht im Schreibcache.
Der Cache wird beim Entsperren eingerichtet und LUKS auf dem Cache-Gerät
geöffnet, nie am Cache vorbei auf dem Array (sonst sähe ein Schreibcache ein
anderes Volume als das Array). Header-Operationen (Passwort ändern,
Header-Sicherung) laufen ebenfalls über das Cache-Gerät. Ein eingeschalteter
Cache wird beim nächsten Entsperren wirksam. Fehlt die SSD, gelten die Regeln
aus [SSD-CACHE.md](SSD-CACHE.md): Lesecache → LUKS direkt auf dem Array,
Schreibcache → kein Entsperren möglich.

### Platten und Umbau

- **mdadm:** Ersatz-/Zusatzplatten (Wiederaufbau, Reserve) und das Erweitern
  von RAID 5/6 passieren unterhalb der Verschlüsselung und brauchen kein
  Passwort. Nach dem Erweitern wächst zuerst LUKS (`cryptsetup resize`), dann
  das Dateisystem auf `/dev/mapper/…`. Ist das Volume gesperrt, wenn die
  Erweiterung das Dateisystem vergrößern will (Neustart mitten im Umbau),
  bricht dieser Schritt mit `volume_locked` ab; nach dem Entsperren wird der
  offene Vorgang wie nach anderen Unterbrechungen unter Speicher › Erweitern
  geprüft und fortgesetzt.
- **Btrfs:** Platte hinzufügen und ein fehlendes Mitglied ersetzen verlangen das
  Passwort des Volumes; es wird geprüft, **bevor** die neue Platte gelöscht
  wird. Die neue Partition bekommt einen eigenen LUKS-Container mit diesem
  Passwort (16 MiB größer als das alte Mitglied, Platz für den Header), ihre
  UUID steht in `volumes.json`, bevor Btrfs sie nutzt. Weitere Passwörter des
  Volumes hat die neue Platte nicht: danach erneut „Weiteres Passwort“. Beim
  Ersetzen wird die UUID des fehlenden Mitglieds vergessen. Eine entfernte
  Platte wird geschlossen und ihr LUKS-Header zerstört.
- Ein gesperrtes Btrfs-Volume bietet weder Platte hinzufügen noch Prüfen an;
  ein gesperrtes mdadm-Volume schon (beides betrifft nur das Array).
- **Löschen** eines verschlüsselten Volumes (auch gesperrt) zerstört zuerst die
  Schlüsselplätze (`cryptsetup erase`): Danach ist nichts mehr lesbar, mit
  keinem Passwort und keiner Header-Sicherung von davor.

### Übernehmen bestehender Volumes

Unter Speicher › Übernehmen erscheinen laufende mdadm-Arrays mit LUKS
(LUKS1 oder LUKS2) als „verschlüsselt“. Die Vorschau öffnet sie mit dem
Passwort nur lesend (`--readonly`, Dateisystem `ro,noload` bzw.
`ro,norecovery`), die Übernahme danach dauerhaft; unterstützt sind ext4 und XFS
im Container. Einzelne LUKS-Partitionen (ein verschlüsseltes Btrfs-Mitglied,
eine verschlüsselte Einzelplatte) und von anderen schon geöffnete Container
werden mit Begründung abgelehnt (`encrypted-partition`): TitanOS müsste raten,
welche Container zusammengehören.

## Header-Sicherung und Wiederherstellung

**Wer das Passwort verliert, verliert die Daten.** Niemand kann sie
wiederherstellen, auch TitanOS nicht; eine Hintertür gibt es nicht.

„Header-Sicherung“ lädt ein tar-Archiv mit einer Datei je LUKS-Gerät
(`cryptsetup luksHeaderBackup`) und einer Anleitung. Sie entsteht in einem
privaten Ordner unter `/run` (RAM) und wird nach dem Herunterladen gelöscht.
Sie hilft, wenn der Header eines Geräts beschädigt ist (ohne Header sind die
Daten auch mit Passwort verloren):

```
cryptsetup luksHeaderRestore <gerät> --header-backup-file <datei>
```

Die Sicherung öffnet das Volume mit **jedem Passwort, das es beim Herunterladen
hatte** – auch nachdem eines geändert oder entfernt wurde. Sie gehört an einen
sicheren Ort, nicht auf das NAS selbst. Sie enthält keine Daten. Nach einem
Passwortwechsel oder einer neuen Btrfs-Platte eine neue Sicherung laden.

## Grenzen

- Die Sicherung des Sticks (`configbackup`) auf einem verschlüsselten Volume
  findet ein frisch bespielter Stick bei der Ersteinrichtung **nicht**: Er sucht
  nur in unverschlüsselten Arrays und Btrfs-Dateisystemen. Ziel der
  Sticksicherung daher besser ein unverschlüsseltes Volume, oder die Kopie
  herunterladen/extern sichern.
- Kein Entsperren per Schlüsseldatei, TPM, USB-Stick oder Netzwerk (Clevis,
  Tang) und kein automatisches Entsperren – auf Entscheidung des
  Projektinhabers.
- Ein bestehendes Volume lässt sich nicht nachträglich ver- oder entschlüsseln.
- Ruhemodus: Status und Sperrzustand lesen nur `/dev/mapper` und RAM; die
  Anzahl der Passwörter wird beim Entsperren und nach Änderungen gelesen
  (`luksDump`), nie zyklisch.
- `cryptsetup-bin` ist im Abbild (nur das Werkzeug, keine crypttab und keine
  Initramfs-Einbindung), `dm_crypt` gehört zu den beim Bau geprüften
  Kernelmodulen. Das Paket ist neu: das erste Release damit muss ein volles
  Systemrelease sein.

## Prüfstand

Geprüft mit Unit-Tests und nachgebautem cryptsetup (`storage/crypt_test.go`):
Anlegen (mdadm und Btrfs), gesperrt nach dem Start mit genau einer Meldung,
falsche Passwörter und Wartezeit, Entsperren und Einbinden über
`/dev/mapper`, Sperren mit Stoppen und Zurücknehmen bei belegtem Volume,
Reihenfolge mit dem SSD-Cache, Passwort ändern/ergänzen/entfernen (nie das
letzte), Header-Sicherung, Löschen mit `cryptsetup erase`, Btrfs-Platte
hinzufügen nur mit Passwort, Übernahme-Kandidaten; kein Passwort auf einer
Kommandozeile oder in `volumes.json`. Die Übergabe von stdin und Pipe-
Deskriptoren ist mit echten Prozessen getestet (`run/run_test.go`), die
API-Grenze (nur Websitzung, nicht im Assistenten-Katalog) im API-Test.

**Nicht geprüft:** echtes cryptsetup mit Loop-Geräten oder echten Platten (in
der Entwicklungsumgebung ohne root nicht möglich), Laufzeit und RAM der
Schlüsselableitung auf NAS-Hardware, Erweitern eines verschlüsselten RAID 5/6,
Btrfs-Ersatz eines Mitglieds, Zusammenspiel mit echtem dm-cache, das Sperren
mit laufendem Docker/Samba/VMs, der QEMU-Storage-Test und der Abbildbau mit dem
neuen Paket.
