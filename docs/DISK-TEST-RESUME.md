# Plattenprüfung über einen Neustart hinweg fortsetzen

Seit 0.28.0 endet eine Prüfung unter Werkzeuge › Platten vorbereiten
(`backend/internal/storage/disktest.go`) nicht mehr zwangsläufig mit einem
Neustart des NAS oder von titand. Die Fortsetzung steckt in
`backend/internal/storage/disktest_resume.go`.

## Was gespeichert wird und wo

- Ein Prüfstand je Platte liegt in
  `/mnt/<volume>/.titanos/disktests/tests.json` (Ordner nur für root,
  `volumedata.Open`, atomar über Teildatei, `fsync` und Umbenennen). Nie auf
  dem Stick: `/config` wird von dieser Funktion weder gelesen noch geschrieben.
- Ziel ist das Volume, auf dem der Stand schon liegt, sonst das erste
  eingebundene Volume (alphabetisch). Die geprüfte Platte selbst steckt nie in
  einem Volume, der Stand liegt also nie auf ihr.
- Inhalt: Art der Prüfung (`read`/`write`), Phase (`write`, `verify`, `read`,
  `smart`), wie weit die Phase gekommen ist (Byte-Versatz, auf 4 MiB
  ausgerichtet), bisher gezählte Fehlerstellen und Hinweise, Startzeit, der
  Startwert des Schreibmusters, die SMART-Fehlerzähler vor der Prüfung, die
  Wahl „automatisch fortsetzen“, Anzahl und Zeit der Fortsetzungen.
- Die Platte wird über **Seriennummer, Modell und Größe** wiedererkannt, nie
  über den `/dev`-Namen. Eine WWN liefert die bestehende Plattenerkennung
  (`lsblk`-Felder in `probe.go`) nicht; sie wurde bewusst nicht erweitert.
  Platten ohne Seriennummer werden nicht gespeichert.

## Wann geschrieben wird

- beim Start einer Prüfung, bei jedem Phasenwechsel, während einer Phase
  höchstens alle fünf Minuten und am Ende (dann ohne diese Prüfung; ist keine
  mehr übrig, wird die Datei entfernt). Verwerfen und Abbrechen entfernen den
  Stand ebenfalls.
- Beim Überschreiben wird vor jedem Zwischenstand `fsync` auf die Platte
  ausgeführt: Als erledigt gilt nur, was sicher auf der Platte steht. Nach
  einem Stromausfall wird höchstens das Stück seit dem letzten Zwischenstand
  (bis fünf Minuten Arbeit) noch einmal gelesen bzw. geschrieben.
- **Ohne eingebundenes Daten-Volume** läuft eine Prüfung wie bisher nur im RAM
  und endet mit einem Neustart; die Seite sagt das bei der laufenden Prüfung
  („Kein Daten-Volume eingebunden …“, Feld `notPersistent: no_volume`).
  Bewusst kein Ausweichen auf den Stick. Wird später ein Volume eingebunden,
  landet der nächste Zwischenstand dort.

## Nach dem Start

- Sobald die Volumes versorgt sind (`Settled`), liest `storage.Run` die
  Stände aller eingebundenen Volumes, später eingebundene Volumes in der
  nächsten Runde (alle fünf Sekunden, jede Datei einmal je titand-Lauf). Gibt
  es für eine Platte mehrere Stände, gilt der neueste; ältere Dateien auf
  anderen Volumes werden entfernt.
- **Nur-lesen-Prüfungen**, bei deren Start „Nach einem Neustart automatisch
  fortsetzen“ angehakt war (Standard in der Oberfläche; über die API
  `autoResume: true`, fehlend = aus), laufen sofort an der gespeicherten
  Stelle weiter.
- Alle anderen warten mit dem Ergebnis `interrupted` („Unterbrochen“) auf
  „Prüfung fortsetzen“ oder „Verwerfen“; dazu gibt es die Meldung
  `disk_test_waiting`. Eine wartende Prüfung sperrt die Platte nicht und hält
  keine Updates auf.
- **Überschreibtests werden nie von selbst fortgesetzt**, auch nicht, wenn ein
  Stand das behauptet. „Prüfung fortsetzen“ öffnet einen einfachen
  Bestätigungsdialog, der Platte und Datenverlust nennt; die Oberfläche
  schickt danach den Plattennamen als `confirm` mit. Fortgesetzt wird mit
  demselben Muster, sodass das Zurücklesen auch die vor dem Neustart
  geschriebenen Bereiche prüft.
- Der **SMART-Selbsttest** läuft in der Platte selbst. Hat nur titand neu
  gestartet und meldet die Platte den Test noch als laufend, wird er nur
  wieder beobachtet. Hat ein Stromausfall ihn abgebrochen (üblich bei einem
  NAS-Neustart), startet TitanOS ihn einmal neu; ein älteres Ergebnis zählt
  nie als bestanden.
- Die Seite zeigt Start (`started`, unverändert der ursprüngliche) und
  „fortgesetzt … (n×)“ (`resumed`, `resumes`).

## Verworfene Stände

Ein Stand wird mit Hinweis verworfen (`GET /api/v1/storage/tests` → `notices`,
höchstens 20, nur im RAM), wenn

- keine Platte mit dieser Seriennummer da ist (`disk_missing`),
- Modell oder Größe nicht mehr passen oder die Seriennummer doppelt vorkommt
  (`identity_mismatch`; auch, wenn beim Fortsetzen eine andere Platte unter
  dem Namen steckt),
- die Platte inzwischen in einem Volume steckt, eingebunden ist, der Stick ist
  oder einer VM gehört (`disk_in_use`),
- die Datei unlesbar ist oder eine unbekannte Version hat (`state_unreadable`).

Hinweise blendet „Ausblenden“ aus (`DELETE /api/v1/storage/test-notices`).

## API

- `POST /api/v1/storage/tests` nimmt zusätzlich `autoResume` (nur für `read`).
- `POST /api/v1/storage/tests/{disk}/resume` mit `{"confirm": "<disk>"}`
  (Pflicht bei `write`, sonst leer); wie der Start ein schwerer Vorgang
  (Wartungsaufnahme), nur Besitzer/Administratoren.
- `DELETE /api/v1/storage/tests/{disk}` verwirft auch eine wartende Prüfung
  samt Stand.
- Neue Felder je Prüfung: `autoResume`, `persistent`, `notPersistent`
  (`no_serial`, `no_volume`, `write_failed`), `resumed`, `resumes`; neues
  Ergebnis `interrupted`. Ältere Oberflächen ignorieren die Felder.
- Nicht im MCP-Werkzeugkatalog: Fortsetzen eines Überschreibtests
  bleibt eine Entscheidung in der Oberfläche.

## Geprüft und Grenzen

- Unit-Tests (`disktest_resume_test.go`) mit Platte im Speicher, die an einer
  Stelle „stehen bleibt“ wie bei einem Stromausfall: Rundlauf des Stands,
  Fortsetzen genau ab dem Zwischenstand (Lesen und Schreiben), bestandenes
  Zurücklesen nach fortgesetztem Überschreiben, Verweigerung ohne
  Bestätigung, Identitätsprüfung (andere Platte, fehlend, belegt, unlesbar),
  SMART-Selbsttest weiter beobachten bzw. neu starten, Verwerfen, kein
  Schreiben ohne Daten-Volume, Fünf-Minuten-Abstand. API-Test für Rechte und
  Routen.
- Nicht geprüft: echter Stromausfall mit echter Platte und echtem SMART
  (virtuelle Platten kennen kein SMART), Verhalten verschiedener
  Plattenfirmwares beim Neustart während eines Selbsttests, USB-Adapter, die
  Seriennummern verändert melden.
- Die Fortsetzung prüft nicht, ob jemand die Platte zwischendurch an einem
  anderen Rechner beschrieben hat; ein danach fortgesetzter Überschreibtest
  fällt dann beim Zurücklesen durch (kein Datenverlust über die bestätigte
  Löschung hinaus).
- Die SMART-Langtests der geplanten Wartung (`smart-long`) sind davon nicht
  betroffen; sie folgen weiter den Wartungsregeln.
