# Sicherungen in TitanOS
_Mitgeliefert mit TitanOS, wird bei Updates ersetzt. Eigene Anleitungen gehören in den Ordner Wissen außerhalb von „TitanOS“._

Diese Anleitung erklärt, wie Sicherungen in TitanOS aufgebaut sind, wie du prüfst, ob sie gelaufen sind, und wie du einzelne Dateien zurückholst.

## Grundsatz: RAID ist kein Backup

RAID schützt vor dem Ausfall einer Platte, nicht vor Löschen, Ransomware, Brand oder Diebstahl. Dafür braucht es Sicherungen.

## Die 3-2-1-Regel

- **3 Kopien:** das Original und mindestens zwei Sicherungen.
- **2 Arten von Speicher:** z. B. ein anderes Volume und ein USB-Laufwerk.
- **1 Kopie außer Haus:** z. B. ein anderes TitanOS bei Freunden, S3 oder SFTP.

Die Übersicht unter **Einstellungen › Sicherungen** zeigt je Punkt „erfüllt“ oder „fehlt“.

## Ziele und Arten

Sicherungen richten Besitzer und Administratoren unter Einstellungen › Sicherungen › Datensicherung ein.

- **Ziele:** anderes Volume dieses NAS, USB-Laufwerk, SMB-Freigabe, anderes TitanOS, S3, SFTP, restic-REST-Server.
- **Verschlüsselte Versionen (restic):** mehrere Stände, Prüfung, Einzeldatei-Wiederherstellung. S3, SFTP, REST und anderes TitanOS sind immer so.
- **Lesbare Kopie:** bei Volume, USB und SMB wählbar. Dateien liegen direkt lesbar am Ziel, ohne Versionen (außer mit Spiegel oder Datumsordnern).
- **Quellen:** Freigaben, Ordner, ganze Volumes und die Bereiche `appdata` (Docker-Apps), `vms`, `lxc` und `config` (Einstellungen des Sticks).
- **Anwendungen:** ausgewählte Container und VMs werden für die Sicherung sauber angehalten und danach wieder gestartet.
- **Zeitplan:** stündlich, täglich (mehrere Uhrzeiten), Wochentage oder monatlich, optional mit Zeitfenster. Verpasste Termine holt das NAS nach dem Start nach.
- **Aufbewahren:** „Die letzten N Sicherungen behalten“ oder „Behalten, bis das Ziel voll ist“.

**Wichtig:** Jeder restic-Auftrag bekommt ein eigenes Passwort. Ohne dieses Passwort ist das Backup wertlos, wenn das NAS verloren geht. Bewahre es außerhalb des NAS auf (z. B. im Passwortmanager). Der Agent sieht und speichert dieses Passwort nie.

## Prüfen, ob die Sicherung gelaufen ist

1. `backup_status` aufrufen: letzter Lauf, Ergebnis, letzte gelungene Sicherung, nächster Termin.
2. Achten auf:
   - **fehlgeschlagen:** Ursache im Ergebnis lesen (Ziel nicht erreichbar, Laufwerk fehlt, voll).
   - **mit Warnung:** einzelne Dateien waren nicht lesbar, oder eine Freigabe fehlte (Volume nicht eingebunden).
   - **überfällig:** seit mindestens zwei Terminen und 48 Stunden kein Erfolg; TitanOS meldet das einmal.
3. `notifications` lesen: Meldungen zu Sicherungen der letzten Zeit.
4. Bei Bedarf `backup_check` vorschlagen (Archivprüfung mit restic). Ein fehlgeschlagener Check ist kein fehlgeschlagenes Backup, aber ein Warnzeichen.
5. Ergebnis kurz melden: was ist gesichert, wann zuletzt, was fehlt.

Ein fehlgeschlagener Lauf wird nach 30 Minuten wiederholt. Ein USB-Auftrag startet sofort, wenn sein Laufwerk angesteckt wird.

## Eine Sicherung jetzt starten

1. Mit `backup_status` den Auftrag und seine ID suchen.
2. `backup_action` mit `run` vorschlagen; du gibst frei.
3. Fortschritt später mit `backup_status` prüfen. Ein laufender Auftrag lässt sich mit `stop` abbrechen.

## Einzelne Dateien wiederherstellen

Wiederherstellen schreibt **immer in einen neuen Ordner** unter `/mnt/VOLUME/restored/…`, nie über die Freigabe. Es wird also nichts überschrieben.

1. `backup_snapshots` des Auftrags holen: Stände mit Datum.
2. Den passenden Stand wählen (vor dem Löschen oder vor der Änderung).
3. Mit `backup_files` durch die Ordner des Stands gehen und die Dateien finden.
4. `backup_restore_files` mit den Pfaden vorschlagen (höchstens 200 auf einmal).
5. Den neuen Ordner nennen. Du prüfst die Dateien und kopierst sie selbst an ihren Platz, oder der Agent schlägt das Kopieren vor (`file_copy`).
6. Bei lesbaren Kopien (USB, SMB): Dateien einfach über den Dateimanager zurückkopieren.

## Sicherung des Sticks (Konfiguration)

- Ab Werk an: Nach jedem Start und fünf Minuten nach jeder Änderung entsteht eine Kopie von `/config` auf einem Daten-Volume (`.titanos/backups/`), 14 Kopien bleiben.
- Sie enthält Konten, Einstellungen und Geheimnisse, aber keine Nutzdaten. Auf Wunsch mit eigenem Passwort verschlüsselt.
- Unter Einstellungen › Sicherungen › Systemkonfiguration lässt sich eine Kopie herunterladen (mit erneuter Bestätigung).
- **Stick ausgefallen:** neuen Stick mit dem ZIP bespielen, starten und auf der Ersteinrichtungsseite **vor dem Anlegen eines Kontos** „Konfiguration wiederherstellen“ wählen. Datenplatten dabei nie neu anlegen oder formatieren.
- Der Agent kann mit `stick_backup` den Stand ansehen und eine Sicherung vorschlagen.

## Snapshots (Btrfs)

- Nur Btrfs-Volumes haben Snapshots. ext4 und XFS nicht.
- Ein Snapshot ist schreibgeschützt und liegt auf **demselben Volume**. Er hilft gegen versehentliches Löschen, ersetzt aber kein Backup.
- Verschachtelte Subvolumes (z. B. von Docker) sind im Snapshot leer.
- Snapshots legst du auf der Volume-Kachel an; geplante Snapshots mit Aufbewahrung gibt es unter Werkzeuge › Wartung.
- **Automatisch vor Änderungen des Agenten:** Bevor der Agent eine App aktualisiert oder entfernt oder Dateien löscht, verschiebt oder ersetzt, legt TitanOS einen Snapshot `agent-JJJJMMTT-hhmmss` des betroffenen Btrfs-Volumes an (höchstens einer je 10 Minuten, 7 Tage aufbewahrt, abschaltbar unter Einstellungen › KI-Agent).
- **Wiederherstellen:** unter Speicher › Verwalten › Snapshots einen Ordner wählen und „Wiederherstellen …“: an den ursprünglichen Ort (vorhandene Dateien ab Werk überspringen) oder als neuer Ordner daneben. Eine Vorschau zeigt vorher Dateien und Konflikte.
- In Windows erscheinen Snapshots einer Freigabe auf Wunsch als „Vorgängerversionen“.
- Datenbanken und VMs vor einem Snapshot sauber stoppen, damit der Stand stimmig ist.

## Was der Agent bei Sicherungen beachtet

1. Vor riskanten Änderungen (Updates, Löschen, Umbau) zuerst `backup_status` prüfen und eine frische Sicherung oder einen Snapshot vorschlagen.
2. Nie über Originaldaten zurückschreiben, ohne dich zu fragen.
3. Passwörter und Schlüssel von Zielen nie abfragen oder speichern.
4. Bei „Behalten, bis voll“ und vollen Zielen auf den freien Platz achten (`storage_overview`).
