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
