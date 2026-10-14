# Btrfs-Spiegel und Snapshots

Diese Funktionen gehören zum Entwicklungsstand nach `v0.1.0-beta.2`.

Unter **Speicher → Volume anlegen** `RAID 1`, `btrfs` und genau zwei freie
Platten wählen. Die Bestätigung löscht beide ausgewählten Platten. Daten und
Metadaten werden nativ von Btrfs gespiegelt; darunter liegt kein mdadm-Array.
Die kleinere Platte bestimmt den Platz. TitanOS lässt wie bei mdadm 128 MiB
Reserve und speichert die Dateisystem-UUID statt eines wechselnden Gerätenamens.
Bestehende ext4-/XFS-Volumes werden nicht umgewandelt. Neue VM-Verzeichnisse
erhalten das Btrfs-NOCOW-Attribut; ihre neu angelegten virtuellen Platten
vermeiden damit doppeltes Copy-on-Write. Für diese Dateien entfallen die
Btrfs-Datenprüfsummen.

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
Zum Löschen eines Snapshots muss dessen Name eingegeben werden.

## Prüfen und eine fehlende Platte ersetzen

Die Volume-Prüfung startet einen Btrfs-Scrub. Nicht korrigierbare Fehler werden
als Fehlerzähler angezeigt; ein Scrub ist kein Ersatz für ein Backup.

Mit einer fehlenden Platte versucht TitanOS, den Spiegel beim Start degradiert
einzubinden. Unter **Platte hinzufügen** eine mindestens ausreichend große
Ersatzplatte auswählen und das Löschen bestätigen. TitanOS verwendet
`btrfs replace`, damit Daten und Metadaten gespiegelt bleiben. Die bisherige,
entfernte Platte nicht gleichzeitig wieder anschließen. Ein gesunder Spiegel
nimmt keine dritte Platte oder Hot-Spare auf; andere Btrfs-RAID-Profile werden
nicht angeboten. Zum vollständigen Löschen müssen beide Mitglieder erreichbar
sein. Ein fehlendes Mitglied wird dabei nicht stillschweigend vergessen.

Der Test `scripts/smoke-storage.py` prüft diesen Ablauf in QEMU einschließlich
Neustarts, Plattenausfall und Ersatz. Echte Platten, Stromausfälle und defekte
Sektoren sind damit noch nicht geprüft.
