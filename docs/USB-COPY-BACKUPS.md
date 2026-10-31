# Kopie-Backups auf USB-Laufwerke

Seit 0.41.0 heißt das Ziel **USB-Laufwerk**; dort wählt man zwischen
**lesbarer Kopie** (dieses Dokument) und **verschlüsselten Versionen** (restic
auf der Platte, mit Versionen, Prüfung und Einzeldatei-Wiederherstellung).
Für lesbare Kopien gibt es zusätzlich den Spiegel (auf dem NAS Gelöschtes
kommt nach `_gelöscht/<Datum>` und wird nach 30 Tagen entfernt) und
Datumsordner je Lauf mit „Versionen behalten“ und „älteste löschen, wenn das
Laufwerk voll ist“. Einzelheiten in `docs/rules/BACKUPS.md`.

Unter **Einstellungen › Backups › Neuer Auftrag** als Ziel **USB-Kopie** wählen.
Freigaben und bei Bedarf einzelne Unterordner auswählen, die externe Platte und
 einen Zielordner festlegen. Die täglichen, wöchentlichen, monatlichen und
stundenweisen Zeitpläne einschließlich Zeitfenstern gelten auch für Kopien.

Die Zielplatte wird über ihre Dateisystem-UUID wiedererkannt und bei Bedarf
unter `/run/titanos-usb` eingebunden. Mehrdeutige UUIDs werden abgelehnt. Fehlt die
Platte, schlägt der Auftrag fehl und wird nach der bestehenden Backup-Regel
nach 30 Minuten erneut versucht; wird die Platte angesteckt, startet er sofort.
Gemeldet wird nur der erste Fehlschlag einer Serie und das Wiederanlaufen, nicht
jeder Versuch. Ein fehlender Mount wird nie als leerer Ordner im RAM verwendet;
die Reste einer im Betrieb abgezogenen Platte räumt TitanOS beim nächsten
Einbinden weg. Während eines Auftrags verweigert TitanOS das Auswerfen dieser
Platte und das Formatieren; andere Laufwerke bleiben bedienbar. Eine nur lesbar
eingebundene Platte (z. B. NTFS nach Windows-Schnellstart) wird mit eigenem
Hinweis abgelehnt. Nach Abschluss bleibt die Platte eingebunden; im Dateimanager
sicher auswerfen.

Problemdateien halten den Lauf nicht auf: Dateien über 4 GB auf FAT32, Namen,
die das Ziel-Dateisystem nicht erlaubt, Namen, die sich nur in Groß-/Kleinschreibung
unterscheiden (FAT, exFAT, NTFS behandeln sie als eine Datei; die erste bleibt),
unlesbare oder während des Kopierens geänderte Dateien sowie fehlende Freigaben
oder Ordner werden übersprungen und nach dem Lauf als Warnung mit Anzahl und
Beispielen angezeigt. Nur wenn keine einzige ausgewählte Freigabe da ist oder
das Ziel versagt (voll, schreibgeschützt, Lese-/Schreibfehler, abgezogen), schlägt
der Lauf fehl. Vorab prüft TitanOS, ob der Platz für neue und geänderte Dateien
reicht; die Seite zeigt den Fortschritt in Prozent. Am Ende wird das ganze
Ziel-Dateisystem synchronisiert, bevor der Lauf als erfolgreich gilt.

Dateien liegen lesbar unter `<Zielordner>/<Freigabe>/<Unterordner>`. Die Kopie ist
nicht verschlüsselt und führt keine Versionshistorie. Dateien, die auf dem NAS
gelöscht wurden, bleiben auf der USB-Platte erhalten. Geänderte Dateien werden
ersetzt; jeder Lauf kopiert die ausgewählten regulären Dateien vollständig.
Ein Ersatz wird zuerst als temporäre Datei geschrieben und synchronisiert,
dann atomar umbenannt. Bei einem Fehler bleibt die bisherige Zieldatei erhalten;
der gesamte Auftrag ist aber keine atomare Momentaufnahme aller Quelldateien.

Ordnerausschlüsse sowie einfache Dateinamenmuster wie `*.tmp` werden unterstützt.
Muster mit Pfaden oder `**` werden abgelehnt. Papierkorb und unvollständige Uploads
bleiben ausgenommen. Symlinks und Spezialdateien werden übersprungen und in der
Warnung genannt. Konfiguration, VM- und App-Daten als zusätzliche Systembereiche bleiben
bei diesem Kopieformat ausgenommen; dafür stehen die verschlüsselten
restic-Aufträge zur Verfügung. Für konsistente Kopien veränderlicher Dateien
Anwendungen vorher beenden bzw. deren eigene Exportfunktion verwenden.

Rücksicherung: Dateien über den Dateimanager zurückkopieren oder die Platte an
einem anderen Rechner öffnen. Unterstützte Dateisysteme entsprechen der
USB-Verwaltung (u. a. ext4, XFS, Btrfs, exFAT, NTFS und FAT32); Größen- und
Namensgrenzen des Ziel-Dateisystems gelten weiterhin.

Bandbreite: Auch USB-Kopien lassen sich auf MiB/s begrenzen, optional mit einer
anderen Grenze in einem Zeitfenster; die Kopie folgt dem Fenster während des Laufs.
Einzelheiten für alle Backupverfahren in `docs/SMB-COPY-BACKUPS.md`, Abschnitt
„Bandbreite begrenzen“.

Grenzen: Live geprüft wurden diese Fälle nur mit Tests auf temporären
Verzeichnissen; FAT32/exFAT/NTFS-Verhalten auf echten Laufwerken und Abziehen
während des Laufs sind noch auf Hardware bzw. in QEMU zu prüfen.
