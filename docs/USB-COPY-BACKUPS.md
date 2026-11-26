# Kopie-Backups auf USB-Laufwerke

Seit 0.41.0 heißt das Ziel **USB-Laufwerk**; dort wählt man zwischen
**lesbarer Kopie** (dieses Dokument) und **verschlüsselten Versionen** (restic
auf der Platte, mit Versionen, Prüfung und Einzeldatei-Wiederherstellung).
Für lesbare Kopien gibt es zusätzlich den Spiegel (auf dem NAS Gelöschtes
kommt nach `_gelöscht/<Datum>` und wird nach 30 Tagen entfernt) und
Datumsordner je Lauf; unter „Aufbewahren“ gilt „Die letzten [N] Sicherungen
behalten“ oder „Behalten, bis das Laufwerk voll ist“ (seit 0.42.0). Einzelheiten in `docs/rules/BACKUPS.md`.

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
bleiben ausgenommen. Symbolische Verknüpfungen werden als Verknüpfung gesichert
(nie verfolgt; auf FAT32/exFAT, die keine halten können, stehen sie im Hinweis
`link_unsupported`). Sockets, FIFOs und Gerätedateien enthalten keine Daten und
werden still übergangen. Für Abbilder laufender VMs entscheidet die Einstellung `vmDisks` des Auftrags:
konsistent sichern (Btrfs-Snapshot, sonst libvirt-Snapshot, für raw-Abbilder
auf Wunsch kurzes Anhalten) oder auslassen; ohne gespeicherte Wahl fasst der
Lauf keine VM an und bittet um die Wahl (Regeln, Kennungen und Vorschau-Route
in `docs/rules/BACKUPS.md`). Konfiguration, VM- und App-Daten als zusätzliche Systembereiche bleiben
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

## Vorigen Stand einer abgebrochenen lesbaren Kopie zurückholen

Diese Schritte gelten für undatierte lesbare Kopien und Spiegel auf einem Volume, einem USB-Laufwerk oder einer SMB-Freigabe. Wenn ein Lauf während des Kopierens abbricht, können im normalen Zielordner bereits einzelne Dateien aus dem neuen Stand liegen. Der letzte vor dem Schreiben gesicherte Zusammenhang bleibt in einem eigenen Ordner lesbar.

1. Pausiere den Zeitplan des betroffenen Auftrags, falls du den Stand jetzt zurückholen möchtest. Ein vollständig erfolgreicher neuer Lauf räumt die Arbeitskopie der vorigen Generation auf.
2. Öffne in „Dateien“ den **Zielordner des Auftrags**. Aktiviere unter den Ansichtsoptionen **„Versteckte Dateien anzeigen“**. Für einen Auftrag mit der Kennung `Fotos` öffnest du dort `.titanos-recovery-Fotos/previous/`. Namen von Freigaben und `Volume-…` entsprechen der normalen Kopie. Der gleichgeordnete Ordner `building` kann während des Schreibens unvollständig sein.
3. Kopiere die **ganze betroffene Freigabe bzw. die zusammengehörigen Ordner aus `previous`** in einen neuen leeren Wiederherstellungsordner. Prüfe dort die Daten, z. B. Katalog und dazugehörige Mediendateien. Den geprüften Stand kannst du anschließend wieder verwenden und den Zeitplan wieder einschalten. Falls der Fehler bereits bei der Vorbereitung eintrat und es noch kein `previous` gibt, hat dieser Lauf den gewöhnlichen Zielbaum noch nicht überschrieben. Beim ersten Lauf auf ein leeres Ziel gibt es keinen älteren Sicherungsstand.

Auf Dateisystemen mit harten Links teilen sich die alten Dateien bis zum Ersetzen ihre Datenblöcke mit der Arbeitskopie. Auf FAT/exFAT benötigt die Vorbereitung zusätzlich Platz für den vorhandenen geschützten Baum und kann entsprechend länger dauern. Bei einem Platzfehler bleibt der bisherige Stand erhalten. Behandle Ziel und Wiederherstellungsordner während des Laufs als schreibgeschützt für andere Programme. Ein bereits vorhandenes fremdes Verzeichnis am reservierten Ort beendet den Lauf mit einem Fehler.

Der Wiederherstellungsordner bewahrt den Inhalt und die vom **Ziel** dargestellten Dateieigenschaften. Die lesbare Kopie kann nur Rechte abbilden, die das Ziel unterstützt. Eine FAT-/exFAT-Platte hat keine individuellen POSIX-Eigentümer und Zugriffsmodi wie ein Linux-Volume. ACLs, erweiterte Attribute und SMB-Rechte sind durch diese Funktion nicht als vollständig wiederherstellbar zugesichert. Prüfe an der zurückgespielten Freigabe insbesondere Besitzer, Gruppen und wirksamen Zugriff, bevor du sie erneut freigibst.
