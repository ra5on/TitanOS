# Ordnung und Dokumente
_Mitgeliefert mit TitanOS, wird bei Updates ersetzt. Eigene Anleitungen gehören in den Ordner Wissen außerhalb von „TitanOS“._

Diese Anleitung zeigt, wie der Agent Rechnungen und Fotos ordnet, Duplikate findet, Berichte als Word oder PDF erstellt und wohin er sie speichert.

## Grundregeln beim Aufräumen

1. Erst einen Plan zeigen: welche Dateien, alter Name, neuer Name, Zielordner. Bei vielen Dateien eine Tabelle mit den ersten Beispielen und der Gesamtzahl.
2. Nie löschen, wenn nur sortiert werden soll. Verschieben und Umbenennen reichen.
3. Nie eine vorhandene Datei überschreiben. Gibt es den Namen schon, hängt der Agent `(2)` an oder fragt nach.
4. Verschieben, Umbenennen und Löschen brauchen deine Freigabe (im Modus „Standard“). Neue Ordner und neue Dateien legt der Agent ohne Nachfrage an.
5. Ordner, die in den Ordner-Regeln „Verboten“ sind, sieht der Agent nicht.
6. Vor großen Umbauten auf eine Sicherung oder einen Snapshot achten (siehe Anleitung „Backups“).

Werkzeuge: `files_list`, `files_search`, `file_read` (auch Word, Excel, PDF), `folder_create`, `files_move`, `file_rename`, `file_copy`, `file_write`, `file_delete`.

## Rechnungen einsortieren

**Namensschema:** `JJJJ-MM-Firma-Inhalt.pdf`, z. B. `2026-03-Stadtwerke-Strom.pdf`. So sortiert sich alles von selbst nach Datum.

1. Den Eingangsordner nennen lassen (z. B. „Eingescannte Briefe“) oder über die Ablageorte und die NAS-Landkarte finden (`locations_list`, `nas_map`).
2. Jede Datei mit `file_read` öffnen und Rechnungsdatum, Firma und Inhalt suchen.
3. Ist das Datum unklar, das Datum der Rechnung nehmen, nicht das Datum des Scans. Ist es gar nicht zu finden, die Datei in eine Liste „unklar“ aufnehmen und nicht umbenennen.
4. Zielordner nach Jahr anlegen, z. B. `Rechnungen/2026/`; auf Wunsch zusätzlich nach Firma.
5. Plan als Tabelle zeigen: alter Name → neuer Name → Ordner.
6. Nach deiner Freigabe umbenennen und verschieben.
7. Am Ende melden: wie viele einsortiert, welche unklar geblieben sind.
8. Wiederkehrend? Als geplanten Auftrag anlegen (siehe Anleitung „Planen“), z. B. jeden Montag den Eingangsordner sortieren.

Die Volltextsuche (ab Werk aus, Einstellungen › Dateisuche) findet Wörter auch in PDF und Word, auf Wunsch in gescannten PDFs (OCR).

## Fotos nach Datum ordnen

**Schema:** `Fotos/JJJJ/JJJJ-MM/` oder `Fotos/JJJJ/JJJJ-MM-TT Anlass/`.

1. Mit `files_search` und der Erweiterung `photos` alle Bilder im Ordner finden.
2. Das Datum bestimmen, in dieser Reihenfolge:
   - aus dem Dateinamen, wenn die Kamera es hineinschreibt (z. B. `IMG_20260314_…`, `PXL_20260314_…`);
   - sonst das Änderungsdatum der Datei (kann vom Aufnahmetag abweichen, das im Plan sagen).
3. Den Plan als Tabelle zeigen: wie viele Fotos pro Monat, Beispiele.
4. Nach Freigabe Ordner anlegen und verschieben. Dateinamen der Fotos nicht ändern, wenn du es nicht möchtest.
5. Videos und RAW-Dateien gehören mit in denselben Monatsordner.

Die Fotoansicht im Dateimanager zeigt einen Ordner ohnehin nach Aufnahmedatum (EXIF); zum Ansehen muss also nichts verschoben werden.

## Duplikate finden

1. Für gründliches Suchen ist die **Duplikatsuche** in den Werkzeugen des Dateimanagers am besten; sie vergleicht die Inhalte. Der Agent hat dafür kein eigenes Werkzeug.
2. Der Agent kann vorab grob suchen: gleiche Namen mit `(1)`, `(2)`, `Kopie`, `copy` und gleiche Größe in `files_list`.
3. Gleicher Name heißt nicht gleicher Inhalt. Vor dem Entfernen müssen Größe und Inhalt passen.
4. Duplikate erst in einen Ordner `Duplikate prüfen/` verschieben, nicht löschen. Löschen entscheidest du danach.
5. In Freigaben mit Papierkorb landen gelöschte Dateien dort und lassen sich zurückholen.

## Berichte als Word oder PDF erstellen

`file_write` erzeugt Text, Markdown, CSV, JSON, Word (`.docx`), Excel (`.xlsx`) und PDF. Für Word und PDF schreibt der Agent den Inhalt als Markdown (Überschriften, Listen, Tabellen); TitanOS macht daraus das Dokument.

1. Klären: Thema, für wen, wie lang, Word oder PDF.
2. Die Daten sammeln (z. B. `storage_overview`, `backup_status`, `ha_statistics`).
3. Gliederung: Titel, Datum, kurze Zusammenfassung oben, dann Abschnitte, Tabellen statt langer Aufzählungen.
4. Dateiname mit Datum, z. B. `2026-11-05 Bericht Speicher.docx`.
5. Am gewählten Ort speichern und den Pfad nennen.
6. Ein Chat lässt sich zusätzlich als Markdown oder PDF exportieren (⋯ in der Kopfzeile des Chats).

## Wohin speichern

1. **Was du sagst, gilt.** Nennst du einen Ordner, kommt die Datei dorthin.
2. **Projekt:** Hat das Projekt des Chats einen Standard-Ablageort, nimmt der Agent ihn.
3. **Ablageorte:** Benannte Orte wie „Rechnungen“ → Freigabe Dokumente, Ordner `Rechnungen` (`locations_list`).
4. **Sonst:** der Ordner `Dokumente` im eigenen KI-Agent-Ordner (`KI-Agent/KONTO/Dokumente/`).
5. Nie in Systemordner (`.titanos`, `appdata`, `vms`) und nie auf den Stick.
6. Weiß der Agent den Ort nicht sicher, fragt er mit Knöpfen nach und merkt sich die Antwort im Gedächtnis.

## Ordnung halten

- Kurze, sprechende Ordnernamen, Datum vorne im Format `JJJJ-MM-TT`.
- Lieber wenige Ebenen als tiefe Bäume.
- Eingangsordner regelmäßig leeren lassen (geplanter Auftrag).
- Beschreibungen in der NAS-Landkarte helfen dem Agenten, die richtigen Ordner zu finden („Eingescannte Briefe“).
