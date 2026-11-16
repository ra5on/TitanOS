# Durchsicht 1.01 – Speicher und Daten: für die Versionshinweise

Durchsicht vom 16.11.2026 (Auftrag „die ganze Codebasis prüfen … Stabilität reinbringen“), Bereich Speicher, Dateien, Freigaben und Sicherungen. Alle Änderungen betreffen nur titand (App-Update); jede Korrektur hat einen eigenen Test. Einzelheiten und die notierten, nicht gebauten Punkte: `docs/validation/durchsicht-102-speicher.md`.

## Sicherungen (lesbare Kopie auf USB/SMB/Volume)

- **Letzte Kopie bleibt erhalten.** Konnte ein Lauf mit Datumsordnern eine Datei oder einen Ordner nicht lesen (sterbende Platte, fehlende Rechte, Datei änderte sich beim Lesen), fehlte sie in jedem neuen Lauf, und nach „die letzten N behalten“ war auch der letzte Lauf mit ihr gelöscht. Jetzt bleibt der Lauf mit der letzten Kopie stehen.
- **VM-Platten beim Platzschaffen.** War das Ziel voll, löschte der Lauf alte Datumsordner, bevor feststand, ob er die Platten laufender VMs sichern kann – im schlechtesten Fall die einzige Kopie einer VM-Platte. Jetzt bleibt dieser Lauf stehen; reicht der Platz dann nicht, meldet die Sicherung „kein Platz“.
- **„Überspringen“ gilt auch für VMs, die während der Sicherung starten.** Vorher wurde ihre Platte dann doch gelesen und ersetzte die frühere Kopie.
- **Aus Datei wird Ordner (oder umgekehrt).** Solche Einträge wurden nie wieder gesichert. Jetzt wandert der alte Eintrag der Kopie nach `_gelöscht/<Datum>/` und der neue wird kopiert.
- **Ziel „Anderes TitanOS“.** Ein Ordner, den das Ziel-NAS nicht lesen konnte, wurde als leer gemeldet; das sendende NAS hielt sein Repository dann für leer. Jetzt kommt ein Fehler. Außerdem hält das Öffnen der Einstellungsseite laufende Sicherungen nicht mehr bis zu einer Minute an.

## Speicher

- **Schreibcache ohne SSD.** Ein Schreibcache, dessen SSD fehlt, ließ sich auf „Lesecache“ umstellen. Danach wurde das Volume ohne die Daten eingebunden, die nur auf der SSD lagen, und die SSD beim Wiederauftauchen geleert. Die Umstellung wird jetzt abgelehnt, solange der Schreibcache nicht läuft (Ausschalten bleibt möglich).
- **Btrfs-Befehle mit Zeitgrenze.** Ein hängender Snapshot-Befehl hielt bisher alle weiteren Speicheraktionen für immer auf.
- Liegen gebliebene Sicherungs-Snapshots werden nach dem Start zuverlässig gelöscht; die gespeicherte Historie „Aufgeweckt durch“ wird bei einem kurzzeitigen Lesefehler nicht mehr überschrieben.

## Freigaben und Papierkorb

- **Papierkorb leeren bleibt im Papierkorb.** Das stündliche Aufräumen ließ sich durch einen im richtigen Moment gesetzten Link dazu bringen, alte Dateien außerhalb der Freigabe zu löschen. Es arbeitet jetzt fest innerhalb des Papierkorbs.
- **Papierkorb der Freigabe „KI-Agent“** wird jetzt auch geleert (lag in den Ordnern der Konten und wurde nie aufgeräumt).
- **Ransomware-Schutz.** Nach dem Sperren werden offene Verbindungen ein zweites Mal geschlossen, sobald die nur lesende Einstellung geladen ist; vorher konnte ein sofort neu verbundener Client weiterschreiben. Nach „Aufheben“ sperrt eine einzelne Umbenennung nicht gleich wieder, und der Wächter kann nicht mehr unbemerkt stehen bleiben.
- **Kontingente** werden nach einem unterbrochenen Zählen neu gezählt, statt fälschlich „aktiv“ zu zeigen.
- Eine nicht lesbare `shares.json` wird weder halb verwendet noch beim nächsten Bearbeiten überschrieben, und der Stick wird dabei nicht mehr alle fünf Sekunden gelesen.

## Dateimanager

- **Geheimnisse bleiben verborgen.** API-Schlüssel, MCP und der KI-Agent konnten `.env`-Dateien zwar nicht lesen, aber umbenennen (und dann lesen), löschen oder unter anderem Namen kopieren. Das ist jetzt gesperrt.
- Ein Upload mit „Beide behalten“ scheiterte dauerhaft, wenn sein Name während des Hochladens belegt wurde; jetzt nimmt er den nächsten freien Namen.
- „Rückgängig“ nach Löschen in den Papierkorb vergisst den Löschvermerk; die Prüfung vor dem Kopieren zählt verschachtelte Auswahl nicht mehr doppelt; die Duplikatsuche bricht wegen einer unlesbaren Datei nicht mehr ganz ab; ein fertig entpacktes Archiv gilt als fertig, auch wenn kurz danach „Abbrechen“ kam.

## Grenzen

Geprüft mit Unit-Tests und nachgebauten Befehlen (`go test -race`), nicht auf echter Hardware, nicht mit echtem Samba, restic oder dm-cache. Offene Punkte (u. a. Reste nach unterbrochenem Snapshot-Wiederherstellen, Wiederherstellen einzelner Dateien unter der Speichersperre, ein scheiterndes Konto hält das Anwenden aller Freigaben auf) stehen in der Fundliste unter „Notiert“.
