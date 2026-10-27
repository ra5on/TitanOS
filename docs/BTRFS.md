# Btrfs-Volumes und Snapshots

Btrfs-Spiegel gibt es seit `v0.1.0-beta.3`, weitere Profile und das Hinzufügen
und Entfernen von Platten seit 0.29.0.

Unter **Speicher → Volume anlegen** `btrfs` und eines der Profile wählen:

| Profil | Platten | Kopien | darf ausfallen |
|---|---|---|---|
| RAID 1 | ab 2 | 2 | 1 Platte |
| RAID 1 (3 Kopien), `raid1c3` | ab 3 | 3 | 2 Platten |
| Einzeln, `single` | ab 1 | 1 (Metadaten doppelt auf jeder Platte) | keine |

Die Bestätigung löscht alle ausgewählten Platten. Daten und Metadaten verteilt
Btrfs selbst; darunter liegt kein mdadm-Array. Anders als bei mdadm werden
Platten unterschiedlicher Größe ausgenutzt, soweit jede Kopie eines Blocks auf
einer anderen Platte Platz findet (zum Beispiel RAID 1 aus 4 + 4 + 8 TB ergibt
8 TB). Der Anlegen-Dialog zeigt den so geschätzten Platz. TitanOS lässt wie bei
mdadm 128 MiB Reserve und speichert die Dateisystem-UUID statt eines
wechselnden Gerätenamens; in `volumes.json` stehen zusätzlich `profile` und
`devices` (die erwartete Zahl der Platten). Fehlen die Felder, ist es ein
Spiegel aus zwei Platten wie vor 0.29.0.

Bestehende ext4-/XFS-Volumes werden nicht umgewandelt. Neue VM-Verzeichnisse
erhalten das Btrfs-NOCOW-Attribut; ihre neu angelegten virtuellen Platten
vermeiden damit doppeltes Copy-on-Write. Für diese Dateien entfallen die
Btrfs-Datenprüfsummen.

## Platten hinzufügen, entfernen, Profil ändern (ab 0.29.0)

- **Platte hinzufügen** bei einem vollständigen Volume: Die Platte kommt per
  `btrfs device add` dazu, danach verteilt `btrfs balance` die Daten neu. Im
  Dialog lässt sich dabei das Profil wechseln, etwa von RAID 1 auf drei Kopien.
  Fehlt dem Volume eine Platte, wird sie stattdessen ersetzt (siehe unten).
- **Platte entfernen** (Kreuz an einem Mitglied, nur bei vollständigem Volume
  und nur, wenn das Profil danach noch genug Platten hat): `btrfs device
  remove` verschiebt zuerst alle Daten auf die übrigen Platten. TitanOS prüft
  vorher, dass die belegten Daten mit 5 % Reserve auf die übrigen Platten
  passen, löscht die Platte danach und meldet das Ergebnis
  (`volume_disk_removed` bzw. `volume_disk_remove_failed`).
- **Profil ändern** ohne neue Platte (`PUT /api/v1/storage/volumes/{name}/profile`):
  eine Balance mit `-dconvert`/`-mconvert` und dem Filter `soft`, damit schon
  umgewandelte Blöcke nicht noch einmal geschrieben werden. Weniger Kopien
  zeigen vorher eine Warnung.

Während einer Balance oder einer Entfernung steht das Volume auf „Umbau“
(`reshaping`) mit Fortschritt aus `btrfs balance status`. App-Updates, Wartung
und andere Speicheränderungen warten so lange. Profil und Plattenzahl werden in
`volumes.json` geschrieben, sobald die Platte zum Dateisystem gehört bzw. das
Umwandeln beginnt. Eine Balance läuft im Kernel und überlebt einen Neustart von
titand; nach einem NAS-Neustart setzt Btrfs sie beim Einbinden selbst fort. Ein
Entfernen bricht bei einem NAS-Neustart ab; die Platte bleibt dann Mitglied und
das Volume vollständig.

Ein Volume ohne zweite Kopie (`single`) wird mit fehlender Platte nicht
eingebunden: Es wartet, bis alle Platten wieder da sind.

Ältere App-Versionen (Rückweg auf 0.28.x) kennen nur Spiegel aus zwei Platten:
Ein Volume mit einer oder drei Platten zeigen sie als degradiert bzw. lassen es
nicht löschen; ein einzelnes Volume binden sie nach 60 Sekunden degradiert ein.
Daten gehen dabei nicht verloren.

## Snapshots

Auf der Volume-Kachel **Snapshots** öffnen und einen Namen vergeben
(Buchstaben, Ziffern, `_` und `-`, höchstens 64 Zeichen). Ein Snapshot ist
schreibgeschützt und liegt in `.titanos/snapshots/<name>` auf demselben Volume.
Er teilt sich die unveränderten Datenblöcke mit dem Original. Er schützt weder
vor dem Verlust beider Platten noch ersetzt er ein externes Backup.

Vor einem konsistenten Stand laufende Datenbanken und VMs sauber stoppen.
Btrfs-Snapshots sind nicht rekursiv: verschachtelte Subvolumes, etwa von Docker,
werden nicht mitgesichert. Freier Speicher wird von Volume und Snapshots
zusammen verbraucht; alte Snapshots rechtzeitig entfernen.

**Als neuen Ordner wiederherstellen** erstellt eine beschreibbare Kopie unter
`/mnt/<volume>/restored/<snapshot>`. Das laufende Volume und vorhandene
Wiederherstellungen werden niemals überschrieben. Der konkrete Pfad erscheint
nach dem Vorgang. Administratoren können benötigte Dateien über das NAS-Terminal
oder SSH zurückkopieren und dabei deren Rechte erhalten. Der Wiederherstellungs-
Ordner bleibt geschützt und wird nicht automatisch als Freigabe veröffentlicht.
Löschen eines Snapshots fragt mit einem einfachen Bestätigungsdialog nach; die Oberfläche schickt den Namen selbst.

## Prüfen und eine fehlende Platte ersetzen

Die Volume-Prüfung startet einen Btrfs-Scrub. Nicht korrigierbare Fehler werden
als Fehlerzähler angezeigt; ein Scrub ist kein Ersatz für ein Backup.

Mit einer fehlenden Platte bindet TitanOS ein Volume mit mindestens zwei Kopien
nach 60 Sekunden degradiert ein. Unter **Platte hinzufügen** eine mindestens
ausreichend große Ersatzplatte auswählen und das Löschen bestätigen. TitanOS
verwendet `btrfs replace`, damit die Kopien erhalten bleiben. Die bisherige,
entfernte Platte nicht gleichzeitig wieder anschließen. Hot-Spares gibt es bei
Btrfs nicht. Zum vollständigen Löschen müssen alle Mitglieder erreichbar sein.
Ein fehlendes Mitglied wird dabei nicht stillschweigend vergessen.

Der Test `scripts/smoke-storage.py` prüft diesen Ablauf in QEMU einschließlich
Neustarts, Plattenausfall und Ersatz. Echte Platten, Stromausfälle und defekte
Sektoren sind damit noch nicht geprüft.

## Erweiterte Abläufe ab 0.16.0

Vorhandene Btrfs-Volumes (single, RAID 1, RAID1C3; alle Platten vorhanden,
nicht eingebunden) lassen sich nach lesender Vorschau registrieren; RAID0,
RAID5/6, RAID10 und halb umgewandelte Volumes werden abgelehnt. Der neue
Snapshot-Dateibrowser kopiert ausgewählte reguläre Dateien in neue Ordner;
Symlinks und interne Betriebsdaten bleiben ausgenommen. Wartungszeitpläne
erzeugen und inventarisieren eigene Snapshots; ihre bestätigte Aufbewahrung
löscht keine manuellen Kopien. Siehe [WORKFLOWS.md](WORKFLOWS.md).

## Ganze Ordner aus einem Snapshot wiederherstellen (ab 0.28.0)

Unter **Speicher → Verwalten → Snapshots** einen Snapshot wählen und neben
einem Ordner (oder für den gerade geöffneten Ordner) **Wiederherstellen …**
anklicken. Der Dialog bietet zwei Ziele:

- **An den ursprünglichen Ort:** Der Ordner wird rekursiv in den laufenden
  Ordner gleichen Pfads zurückgeführt; vorhandene Ordner werden
  zusammengeführt. Für Namen, die es dort schon gibt, gilt eine Regel wie im
  Kopierdialog: **Überspringen** (Standard), **Beide behalten** (`name (2).ext`)
  oder **Ersetzen**. Ersetzen tauscht nur Dateien bzw. Links gegeneinander aus
  und verlangt ein zusätzliches Häkchen, wenn etwas ersetzt würde. Ein Ordner
  wird nie gelöscht, um Platz zu machen; trifft ein Ordner auf eine Datei oder
  einen Link (oder umgekehrt), bleiben beide erhalten. Fehlt der Ordner oder
  fehlen schon seine Elternordner, werden sie aus dem Snapshot neu angelegt.
- **Als neuen Ordner daneben:** `<ordner> (<snapshot>)` neben dem Original,
  bei Bedarf `… (2)`. Der Elternordner muss noch existieren.

Vor dem Start zeigt eine Vorschau Dateien, Unterordner, Größe, Links,
ausgelassene Sondereinträge, das Ziel und die Zahl der Konflikte; sie schreibt
nichts, zählt höchstens zwei Minuten und bindet den Start an die
Btrfs-UUID des Snapshots. Geänderte Optionen lösen eine neue Vorschau aus.

Die Wiederherstellung läuft als Hintergrundauftrag (erst Zählen, dann
Kopieren) mit Fortschritt nach Dateien und Bytes. Sie erscheint im Snapshot-
Reiter und unter **Werkzeuge → Aufträge**, lässt sich abbrechen und meldet
Abschluss (`snapshot_restore_done`) oder Fehler (`snapshot_restore_failed`).
Es läuft höchstens eine Ordner-Wiederherstellung gleichzeitig. Solange sie
läuft, warten App-Updates und Wartungsaufträge; der verwendete Snapshot und
das Volume lassen sich nicht löschen. Der Start steht im Aktivitätsprotokoll
(Volume, Snapshot, Pfad, Ziel und Regel).

Sicherheit und Treue:

- Der Snapshot wird nur lesend geöffnet. Geschrieben wird ausschließlich im
  Zielordner; Pfade mit `..`, absolute Pfade, `.titanos`, `.recycle` und
  interne `.titanos-*`-Namen werden abgelehnt bzw. ausgelassen.
- Alle Zugriffe laufen über festgehaltene Verzeichnis-Deskriptoren mit
  `O_NOFOLLOW`. Symlinks werden auf keiner Seite verfolgt: Ein Symlink im
  Snapshot wird als Link wiederhergestellt, ein Symlink oder ein anderer Mount
  auf dem Weg zum Ziel führt zur Ablehnung.
- Jede Datei und jeder neue Ordner entsteht unter einem versteckten Namen
  (`.titanos-restore-….titanos-part`) und wird erst vollständig per
  `renameat2(RENAME_NOREPLACE)` veröffentlicht (bei „Ersetzen“ per
  atomarem Umbenennen über die alte Datei). Ein neuer Ordner daneben wird
  dadurch erst sichtbar, wenn er komplett ist.
- Besitzer, Gruppe, Rechte (einschließlich setuid/setgid), Zugriffs- und
  Änderungszeit sowie erweiterte Attribute einschließlich POSIX-ACLs werden
  übernommen. Dateien werden auf demselben Btrfs-Volume per Reflink
  (`FICLONE`) geklont und belegen keinen zusätzlichen Platz; ohne Reflink wird
  normal kopiert. Jede Datei wird vor der Veröffentlichung mit `fsync`
  gesichert.
- Vorhandene Ordner am ursprünglichen Ort behalten ihre eigenen Rechte und
  Zeiten.

Grenzen: Aufträge liegen nur im RAM (die letzten 20 beendeten bleiben
sichtbar) und überleben keinen titand-Neustart. Beim Abbruch oder Fehler
bleiben bereits zurückgeführte Dateien am ursprünglichen Ort erhalten; die
gerade geschriebene Datei bzw. der unfertige neue Ordner wird entfernt. Nach
einem Absturz können versteckte `.titanos-restore-*`-Reste liegen bleiben.
Hardlinks werden als getrennte Dateien wiederhergestellt; Geräte, Pipes und
Sockets werden ausgelassen. Verschachtelte Subvolumes sind im Snapshot leer
(siehe oben). Ohne Reflink-Unterstützung braucht die Kopie freien Platz; er
wird nicht vorab geprüft. Unit-Tests prüfen Pfadsicherheit, Konfliktregeln,
Metadaten und den Auftragsablauf auf einem gewöhnlichen Dateisystem; Reflinks
auf echtem Btrfs und große Bäume sind in QEMU bzw. auf echter Hardware noch
zu prüfen.
