# Ordnerkopien auf ein zweites NAS (Beta, ab 0.5.0)

Unter **Einstellungen → Backups** zuerst ein **SMB-Ziel einrichten**:

1. Auf dem zweiten TitanOS oder einem anderen NAS einen Benutzer und eine
   beschreibbare SMB-Freigabe anlegen. SMB3 mit Transportverschlüsselung muss
   unterstützt sein. Der Server muss über LAN oder einen vorhandenen VPN-Weg
   erreichbar sein.
2. Namen, Adresse, SMB-Benutzer und Passwort eintragen. **Verbinden und Freigaben
   anzeigen** listet die angebotenen Datenträgerfreigaben. Eine angebotene
   Freigabe kann schreibgeschützt sein; der folgende Schreibtest prüft das.
3. Freigabe und optionalen Unterordner wählen. **Schreibzugriff testen und Ziel
   speichern** legt bei Bedarf diesen Ordner und kurz eine Testdatei an.
4. Einen Backupauftrag erstellen, **SMB-Ordnerkopie · Beta** und das gespeicherte
   Ziel wählen. Eine oder mehrere Freigaben ganz oder einzelne Ordner auswählen.
   Die gewählten Ordner werden mit allen enthaltenen Unterordnern kopiert.
5. Auftragsordner und Zeitplan festlegen, speichern und **Jetzt sichern** wählen.
   Zeitpläne entsprechen den vorhandenen USB-/restic-Aufträgen.

Der vollständige Zielpfad lautet
`//server/freigabe/optionaler-unterordner/auftragsordner/quellfreigabe/relativer-pfad`.
Ohne eigenen Auftragsordner wird `backups/<auftragsname>` verwendet. Verschiedene
Aufträge sollten verschiedene Auftragsordner verwenden.

Dateien sind am Ziel direkt lesbar. Unveränderte Dateien werden durch
Inhaltsvergleich erkannt und nicht erneut geschrieben. Neue/geänderte Dateien
werden erst vollständig in eine kurze temporäre Datei geschrieben, synchronisiert
und dann an ihren endgültigen Namen verschoben. Gelöschte Quelldateien bleiben
am Ziel. Geänderte Dateien ersetzen die vorherige Kopie; dies ist keine
Versionshistorie. Für ältere Fassungen die vorhandenen restic-Backups nutzen.

Eine Rücksicherung ist eine normale Dateikopie vom Ziel-NAS. Zunächst in einen
neuen Ordner zurückkopieren und prüfen. Die Funktion speichert keine restic-
Archive und braucht kein zusätzliches Backup-Verschlüsselungspasswort. SMB3
verschlüsselt den Transport, nicht die Dateien auf der Zielplatte.

Die Konfiguration liegt in `/config/backup/smb-targets.json`, geschützt wie die
übrigen Backup-Zugangsdaten und im Dateimanager verborgen. API-Antworten enthalten
kein Passwort. Beim Bearbeiten behält ein leeres Passwort das bisherige nur bei
unverändertem Server und Benutzer. Löschen eines Ziels löscht keine Backups;
zuerst müssen darauf verweisende Aufträge geändert oder entfernt werden.

Die Kopie verwendet keine Daten-Zwischenablage im RAM. Verbindungen werden nur
zum Prüfen oder Sichern eingebunden und danach wieder gelöst. Fehlende Ziele
führen zu einem Fehler, niemals zu einer Kopie in ein leeres lokales
Mountverzeichnis. Das eigene NAS wird als Netzwerkziel abgelehnt. Keine
Freigabe benutzen, die über Umleitungen wieder auf die Quelle zeigt.

Symlinks und Spezialdateien werden mit Fehler abgewiesen. Papierkorb,
unvollständige Uploads und konfigurierte Ausschlüsse werden übersprungen.
Zusätzliche Quellen für Konfiguration/Appdata/VMs sind bei unverschlüsselten
Ordnerkopien nicht verfügbar. Laufende Datenbanken/VM-Abbilder brauchen eine
anwendungsgerechte Sicherung; eine Dateikopie ist kein konsistenter Snapshot.

**Beta** bezeichnet dieses neue optionale Verfahren. Der System-Updater bleibt
im einzigen Stable-Kanal. Ein erfolgreicher Kopierlauf sollte durch eine eigene
Rücksicherung auf der verwendeten Hardware ergänzt werden.


## Schreibweise der Namen (Entwicklungsstand nach 0.6.1)

Auftrags- und Zielnamen behalten die eingegebene Groß-/Kleinschreibung. Beispielsweise bleibt `Backup` auch im automatisch vorgeschlagenen Zielordner `backups/Backup` erhalten. Der feste Präfix `backups/` ist eine Ablagestruktur, keine zweite Fassung des Auftragsnamens. Freigaben, Unterordner und Dateien werden unverändert benannt, etwa `backups/Backup/Daten/MeineFotos/Urlaub.JPG`.

Bestehende Aufträge und Pfade werden nicht umbenannt. Zwei neue Aufträge oder Zielbezeichnungen, die sich nur durch Groß-/Kleinschreibung unterscheiden, werden nicht parallel angelegt. Die Prüfungen verwenden isolierte Zielverzeichnisse für den gemeinsamen USB-/SMB-Kopierpfad; die Namensbehandlung des konkreten externen NAS muss dessen Dateisystem und SMB-Konfiguration ebenfalls unterstützen.

## Bandbreite begrenzen (alle Backupaufträge, ab 0.28.0)

Gilt für jeden Backupauftrag, nicht nur für SMB-Ordnerkopien: restic-Aufträge
(anderes Volume, S3, SFTP, REST-Server, anderes TitanOS) sowie USB- und
SMB-Ordnerkopien. Im Auftragsformular unter **Bandbreite**:

- **Bandbreite begrenzen** mit einer Grenze in MiB/s (ganze Zahl 1–10.000;
  0 = unbegrenzt).
- Optional **In einem Zeitfenster eine andere Grenze**: Von/Bis (HH:MM) mit eigener
  Grenze, 0 = unbegrenzt. Das Fenster folgt denselben Regeln wie das Startfenster
  des Zeitplans: Es darf über Mitternacht gehen (z. B. 22:00–06:00), Beginn ist
  enthalten, Ende nicht; maßgeblich ist die eingestellte Zeitzone des NAS.
  Beispiel „tagsüber 10 MiB/s, nachts unbegrenzt“: Grenze 10, Fenster 22:00–06:00
  mit 0. Umgekehrt „nur während der Arbeitszeit bremsen“: Grenze 0, Fenster
  08:00–18:00 mit 5.

Gespeichert wird das optionale Feld `bandwidth` (`limit`, `windowFrom`,
`windowTo`, `windowLimit`) im Auftrag in `/config/backup/jobs.json`, nur beim
ausdrücklichen Speichern des Auftrags. Eine Einstellung, die nichts begrenzt, wird
nicht gespeichert. Ältere titand-Versionen kennen das Feld nicht, lesen den Auftrag
unverändert und sichern dann ohne Grenze; speichern sie den Auftrag neu, entfällt
das Feld.

**Wirkung je Verfahren**

- **restic:** `--limit-upload` und `--limit-download` in KiB/s (MiB/s × 1024) für
  alle restic-Aufrufe eines Backup-Laufs (Repository öffnen/anlegen, `unlock`,
  `backup`, `forget --prune`). restic kann die Grenze während eines Aufrufs nicht
  ändern: Es gilt die Grenze zum **Start des Laufs** für den ganzen Lauf, auch wenn
  er über eine Fenstergrenze hinaus läuft. Prüfen, Wiederherstellungsproben,
  Wiederherstellen und Snapshot-Listen laufen ohne Grenze. Die Grenze zählt, was
  restic an sein Backend überträgt (nach Deduplizierung und Kompression), nicht
  die gelesenen Quelldaten.
- **USB- und SMB-Ordnerkopie:** In titand selbst begrenzt (kein rsync): gezählt
  werden die ins Ziel geschriebenen Bytes und die zum Inhaltsvergleich vom Ziel
  zurückgelesenen. Die Kopie sieht höchstens einmal je Sekunde auf die Uhr und
  wechselt beim Überschreiten einer Fenstergrenze **während des Laufs** auf die
  neue Grenze. Lesen der Quelle und SMB-Protokollaufwand werden nicht gezählt; die
  Leitung kann daher etwas über der Grenze ausgelastet sein. Pausen (Öffnen von
  Dateien, langsame Platten) ergeben keinen nachgeholten Schub von mehr als einer
  Sekunde.

Während ein Auftrag läuft, zeigt die Fortschrittszeile die gerade geltende Grenze
(„begrenzt auf 10 MiB/s“ bzw. „gerade unbegrenzt“); die API liefert sie als
`state.bandwidthLimit` (MiB/s, fehlt bei unbegrenzt). Nach dem Lauf wird der Wert
geleert und nicht mit dem Zustand gespeichert.

Geprüft mit Unit-Tests (Argumente für restic, Umrechnung, Fensterwahl über
Mitternacht, Tempo und Fensterwechsel der Kopie mit künstlicher Uhr, API-Prüfung)
und nachgebautem restic. Nicht geprüft: tatsächlich erreichte Raten mit echtem
restic gegen S3/SFTP/REST und SMB/USB auf echter Hardware.
