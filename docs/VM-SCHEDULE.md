# VM-Zeitplan

Jede VM kann sich nach einem eigenen Zeitplan starten und herunterfahren
lassen, zum Beispiel werktags um 07:00 starten und um 22:00 herunterfahren.
Eingestellt wird das im Abschnitt **Zeitplan** des VM-Formulars (VM
bearbeiten bzw. anlegen). Die Übersicht einer VM zeigt den nächsten geplanten
Start und Stopp, die VM-Liste ein Uhrsymbol mit dem nächsten Termin.

## Einstellungen

- **Zeiten:** bis zu 16 Einträge, jeder mit Aktion (Starten oder
  Herunterfahren), Uhrzeit (`HH:MM`) und mindestens einem Wochentag. Starten und
  Herunterfahren zur selben Minute am selben Tag werden abgelehnt.
- **Wartezeit beim Herunterfahren:** 30 Sekunden bis 60 Minuten, Standard
  5 Minuten. Herunterfahren schickt zuerst die ACPI-Anfrage an den Gast
  (`virsh shutdown`) und wartet dann so lange, bis die VM aus ist.
- **Danach ausschalten erzwingen:** Ist der Gast nach der Wartezeit noch an,
  schaltet TitanOS die VM aus (`virsh destroy`, wie Stecker ziehen) und meldet
  das (`vm_schedule_forced`). Ohne diese Einstellung läuft die VM weiter, und es
  gibt die Warnung `vm_schedule_stop_timeout`.
- Der Zeitplan lässt sich auch bei laufender VM ändern; er gehört nicht zur
  virtuellen Hardware. Das Formular schützt ungeänderte Eingaben wie die übrigen
  Felder vor dem Verlassen der Seite.

## Ablage

Der Zeitplan steht im Feld `powerSchedule` der VM in `/config/vms/<name>.json`
und wird nur beim Speichern der VM geschrieben; ohne Zeitplan fehlt das Feld.
Ältere titand-Versionen kennen das Feld nicht und ignorieren es, die VM bleibt
dort lesbar und startbar. Speichert man die VM mit einer älteren Version, geht
der Zeitplan verloren.

Was schon ausgeführt wurde, liegt nur im RAM und in
`/run/titanos/vm-schedule.json`. titand liest für den Zeitplan nichts zyklisch
von `/config`. Ein Klon übernimmt keinen Zeitplan; das Wiederherstellen eines
Snapshots behält den aktuellen Zeitplan.

## Termine und Zeitzone

- Termine gelten in der aktuell eingestellten Zeitzone des NAS, auch nach einer
  Änderung ohne Neustart von titand.
- Sommerzeit: Eine Uhrzeit, die es am Tag der Umstellung nicht gibt (02:30 Ende
  März), wird eine Stunde später ausgeführt (03:30 Sommerzeit). Eine Uhrzeit,
  die es beim Zurückstellen zweimal gibt (02:30 Ende Oktober), wird nur einmal
  ausgeführt, nämlich zur zweiten 02:30 (Winterzeit).
- titand prüft alle fünf Sekunden. Ein Termin, der wegen eines Neustarts von
  titand (App-Update) bis zu 15 Minuten zurückliegt, wird nachgeholt, aber nie
  doppelt ausgeführt.
- Nach dem Einschalten des NAS wird ein bis zu 15 Minuten zurückliegender
  **Start** nachgeholt (etwa wenn das NAS per Energie-Zeitplan dafür geweckt
  wurde). Ein verpasstes Herunterfahren wird nach dem Einschalten nicht
  nachgeholt.

## Zusammenspiel

- **Manuell starten und stoppen** bleibt jederzeit möglich. Wer eine VM nach
  dem geplanten Start von Hand herunterfährt, dem startet TitanOS sie erst zum
  nächsten Start-Termin wieder; eine von Hand gestartete VM wird zum nächsten
  Herunterfahr-Termin heruntergefahren. Läuft die VM zum Start schon (oder ist
  sie zum Herunterfahren schon aus), passiert nichts.
- **Fehlendes Volume oder laufende Arbeit:** Fehlt ein Volume der VM oder läuft
  für sie ein Snapshot, eine Wiederherstellung, ein Klon oder eine Plattenkopie,
  wartet der Termin und wird jede Minute neu versucht, höchstens 30 Minuten.
  Die Übersicht zeigt, worauf er wartet. Danach – oder wenn vorher der nächste
  Termin dieser VM fällig wird – wird er ausgelassen (`vm_schedule_start_skipped`
  bzw. `vm_schedule_stop_skipped`).
- **Energie-Zeitplan des NAS hat Vorrang:** Fährt das NAS laut Einstellungen ›
  Energie innerhalb der nächsten 15 Minuten herunter oder wartet ein fälliges
  Herunterfahren auf laufende Arbeit, wird ein geplanter VM-Start ausgelassen
  (Hinweis `vm_schedule_start_power`). Beim Herunterfahren des NAS fährt
  `libvirt-guests` die VMs wie bisher herunter.
- Schlägt der Start oder das Herunterfahren fehl (etwa wegen belegter
  Hardware), gibt es die Meldung `vm_schedule_start_failed` bzw.
  `vm_schedule_stop_failed`; Einzelheiten stehen im Protokoll von titand.
- Jeder ausgeführte, ausgelassene oder gescheiterte Termin steht im
  Aktivitätsprotokoll (Herkunft „System“).
- Im Entwicklungsmodus werden keine Termine ausgeführt.

## API

- `GET /api/v1/vms` liefert je VM `powerSchedule` (`enabled`, `rules` mit
  `action` `start`/`stop`, `days` 0 = Sonntag bis 6, `time`; `stopTimeout` in
  Sekunden, `forceOff`) und bei eingeschaltetem Zeitplan `schedule` mit
  `nextStart`, `nextStop` sowie `waiting`/`waitReason` (`volume`, `busy`).
- `PUT /api/v1/vms/{name}` nimmt `powerSchedule` mit; fehlt das Feld, bleibt der
  Zeitplan unverändert. `POST /api/v1/vms` nimmt es beim Anlegen.
  Ungültige Zeitpläne ergeben `400 invalid_vm`.

## Prüfstand und Grenzen

Geprüft mit Go-Tests gegen nachgebautes `virsh` (Termine über die
Sommerzeitumstellung in Europe/Berlin, Warten auf Volume und Snapshot/
Plattenkopie, Vorrang des Energie-Zeitplans, Wartezeit mit und ohne
erzwungenes Ausschalten, kein doppelter Start nach einem titand-Neustart,
Ablage in der VM-Konfiguration) sowie API- und Webtests. Nicht geprüft: echte
Gäste und ihr Verhalten auf die ACPI-Anfrage (Windows, Linux ohne `acpid`),
ein echtes Wecken per RTC mit anschließendem VM-Start.
