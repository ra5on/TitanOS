# Kopie-Backups auf USB-Laufwerke

Unter **Einstellungen › Backups › Neuer Auftrag** als Ziel **USB-Kopie** wählen.
Freigaben und bei Bedarf einzelne Unterordner auswählen, die externe Platte und
 einen Zielordner festlegen. Die täglichen, wöchentlichen, monatlichen und
stundenweisen Zeitpläne einschließlich Zeitfenstern gelten auch für Kopien.

Die Zielplatte wird über ihre Dateisystem-UUID wiedererkannt und bei Bedarf
unter `/run/titanos-usb` eingebunden. Mehrdeutige UUIDs werden abgelehnt. Fehlt die
Platte, schlägt der Auftrag fehl und wird nach der bestehenden Backup-Regel
nach 30 Minuten erneut versucht. Ein fehlender Mount wird nie als leerer Ordner
im RAM verwendet. Während eines Auftrags verweigert TitanOS das Auswerfen.
Nach Abschluss bleibt die Platte eingebunden; im Dateimanager sicher auswerfen.

Dateien liegen lesbar unter `<Zielordner>/<Freigabe>/<Unterordner>`. Die Kopie ist
nicht verschlüsselt und führt keine Versionshistorie. Dateien, die auf dem NAS
gelöscht wurden, bleiben auf der USB-Platte erhalten. Geänderte Dateien werden
ersetzt; jeder Lauf kopiert die ausgewählten regulären Dateien vollständig.
Ein Ersatz wird zuerst als temporäre Datei geschrieben und synchronisiert,
dann atomar umbenannt. Bei einem Fehler bleibt die bisherige Zieldatei erhalten;
der gesamte Auftrag ist aber keine atomare Momentaufnahme aller Quelldateien.

Ordnerausschlüsse sowie einfache Dateinamenmuster wie `*.tmp` werden unterstützt.
Muster mit Pfaden oder `**` werden abgelehnt. Papierkorb und unvollständige Uploads
bleiben ausgenommen. Symlinks und Spezialdateien führen zu einem sichtbaren
Fehler. Konfiguration, VM- und App-Daten als zusätzliche Systembereiche bleiben
bei diesem Kopieformat ausgenommen; dafür stehen die verschlüsselten
restic-Aufträge zur Verfügung. Für konsistente Kopien veränderlicher Dateien
Anwendungen vorher beenden bzw. deren eigene Exportfunktion verwenden.

Rücksicherung: Dateien über den Dateimanager zurückkopieren oder die Platte an
einem anderen Rechner öffnen. Unterstützte Dateisysteme entsprechen der
USB-Verwaltung (u. a. ext4, XFS, Btrfs, exFAT, NTFS und FAT32); Größen- und
Namensgrenzen des Ziel-Dateisystems gelten weiterhin.
