# Eigene Skripte

Unter **Werkzeuge › Skripte** legst du eigene Shell-Skripte an und lässt sie
auf dem NAS laufen: per Knopf, nach Zeitplan, einmal beim Start oder beim
Herunterfahren. Die Seite sehen nur Besitzer und Administratoren
(Entscheidung des Projektinhabers vom 4.11.2026).

> **Skripte laufen als root.** Sie haben volle Rechte über das ganze NAS:
> Ein fehlerhaftes Skript kann Daten löschen oder das System lahmlegen.
> Verwende nur Skripte, die du verstehst. Eine Sandbox gibt es nicht.

## Speicherort

Die Skripte liegen auf einem **Daten-Volume**, nicht auf dem Stick:

```
/mnt/<volume>/.titanos/scripts/
├── index.json            # Namen, Beschreibungen, Auslöser, Zeitpläne
└── <kennung>/
    ├── script            # das Skript selbst
    └── runs.json         # die letzten zehn Läufe
```

- Vorgeschlagen wird das Volume, auf dem die Plugins liegen, sonst das erste
  eingebundene Daten-Volume. Mit dem ersten Skript (oder wenn du unter
  „Speicherort“ ein Volume wählst und speicherst) merkt sich TitanOS die
  Wahl in `/config/system/scripts.json`. Nur dann wird der Stick beschrieben.
- Ein anderes Volume zu wählen verschiebt nichts: Die Skripte bleiben auf dem
  alten Volume und erscheinen wieder, wenn du es zurückwählst.
- Fehlt das Volume (nicht eingebunden, gesperrt, ausgefallen), sagt die Seite
  das deutlich, und es laufen keine Skripte. Ist das Volume zehn Minuten nach
  dem Start von TitanOS noch nicht da, kommt einmal die Meldung
  „Volume … der Skripte fehlt“; erst nachdem es wieder da war, erneut.

## Auslöser

| Auslöser | Wann |
|---|---|
| Per Knopf | Nur wenn du auf **Starten** drückst. Die Ausgabe erscheint live unter dem Skript. |
| Nach Zeitplan | Stündlich (zur gewählten Minute), täglich, wöchentlich (Wochentag) oder monatlich (Tag 1–28) zur gewählten Uhrzeit, oder ein eigener Cron-Ausdruck. Es gilt die Zeitzone des NAS. |
| Beim Start | Einmal je Start des NAS, sobald das Volume eingebunden ist (auch spät, etwa nach dem Entsperren eines verschlüsselten Volumes). Ein Neustart von TitanOS selbst, etwa durch ein App-Update, startet sie nicht noch einmal. |
| Beim Herunterfahren | Beim Neustarten und Herunterfahren **über TitanOS**, bevor VMs, LXC-Container und Docker beendet werden. Jedes Skript höchstens 60 Sekunden, dann wird es beendet. |

Mehrere Skripte desselben Auslösers laufen nacheinander in der Reihenfolge der
Liste.

### Cron-Ausdruck

Fünf Felder: Minute, Stunde, Tag im Monat, Monat, Wochentag (0 oder 7 =
Sonntag). Erlaubt sind `*`, Zahlen, Bereiche (`1-5`), Listen (`1,15`) und
Schritte (`*/15`, `0-30/10`). Namen wie `MON` und Kürzel wie `@daily` gibt es
nicht. Sind Tag im Monat und Wochentag beide eingeschränkt, reicht wie bei
jedem Cron eines von beiden: `0 3 1 * 1` läuft am Ersten jedes Monats und an
jedem Montag.

Beispiele: `*/15 * * * *` (alle 15 Minuten), `30 2 * * 1-5` (werktags 2:30),
`0 4 1 */3 *` (vierteljährlich am Ersten um 4:00).

Verpasste Termine werden nicht nachgeholt: Läuft das NAS zur geplanten Zeit
nicht oder fehlt das Volume, fällt der Lauf aus. Kommt der Planer wenige
Minuten zu spät (hohe Last), holt er höchstens die letzten zehn Minuten nach.

## Ausführung

- Beginnt das Skript mit `#!` und einem Programm (`#!/bin/bash`,
  `#!/usr/bin/python3`, `#!/usr/bin/env python3`), läuft es damit, sonst mit
  `/bin/bash`. Das Skript selbst braucht keine Ausführrechte.
- Arbeitsordner ist der Ordner des Skripts; `PATH`, `HOME=/root` und
  `LANG=C.UTF-8` sind gesetzt, dazu `TITANOS_SCRIPT_ID` und
  `TITANOS_SCRIPT_NAME`. Eingaben gibt es keine (`stdin` ist leer).
- Ausgabe und Fehlerausgabe werden gemeinsam aufgezeichnet. Während des Laufs
  hält TitanOS das letzte 1 MB im Speicher, gespeichert werden je Lauf die
  letzten 64 KB.
- Ein Skript läuft immer nur einmal gleichzeitig. Ein zweiter Start (auch vom
  Zeitplan) wird abgelehnt.
- **Abbrechen** schickt dem Skript und allen seinen Unterprozessen SIGTERM,
  nach zehn Sekunden SIGKILL. Dasselbe passiert, wenn ein Skript beim
  Herunterfahren seine 60 Sekunden überschreitet.
- Programme, die ein Skript im Hintergrund startet und die seine Ausgabe offen
  halten, hindern das Ende des Laufs nicht länger als fünf Sekunden.
- Im Entwicklungsmodus (`titand -dev`) laufen keine Skripte.

## Protokoll und Meldungen

- Die Liste zeigt je Skript den Auslöser, das Ergebnis des letzten Laufs
  (erfolgreich, fehlgeschlagen mit Exit-Code, Zeit abgelaufen, abgebrochen,
  nicht gestartet, unterbrochen), Startzeit, Dauer und den nächsten Termin.
- **Protokoll** zeigt die letzten zehn Läufe mit Startzeit, Dauer, Auslöser,
  Ergebnis und Ausgabe.
- Wurde TitanOS während eines Laufs beendet, steht der Lauf danach als
  „unterbrochen“ im Protokoll: Wie er ausging, ist unbekannt.
- Endet ein **automatischer** Lauf (Zeitplan, Start, Herunterfahren) mit
  Fehler, wird er nach Ablauf der Zeit beendet oder startet er gar nicht,
  kommt die Meldung „Skript … fehlgeschlagen“. Läufe per Knopf melden nichts;
  ihr Ergebnis steht ja auf der Seite. Abbrechen ist kein Fehler.

## Herunterfahren

Beim Neustarten und Herunterfahren über das Konto-Menü, ein Update, den
Energie-Zeitplan, Home Assistant oder die API laufen die Skripte mit Auslöser
„Beim Herunterfahren“ als Erstes, solange Volumes, Apps und VMs noch da sind.
Der Dialog zeigt jedes Skript mit Zustand. „Trotzdem jetzt …“ beendet das
laufende Skript und lässt die übrigen aus. Danach bekommen die Gäste ihre
volle Wartezeit von fünf Minuten.

**Nicht** laufen sie, wenn das NAS an TitanOS vorbei ausgeht: bei
fast leerer USV-Batterie (dort zählt jede Sekunde), bei `systemctl poweroff`
in einer Shell, beim Druck auf den Ein-/Ausschalter (den übernimmt systemd
direkt, siehe `docs/POWER-BUTTON.md`) und natürlich bei Stromausfall.

## Rechte und Schnittstellen

- Alle Routen unter `/api/v1/scripts` verlangen die Web-Sitzung eines
  Besitzers oder Administrators. API-Tokens (auch mit allen Bereichen),
  `titanosctl`, Home Assistant und der MCP-Server erreichen sie nicht; im
  MCP-Werkzeugkatalog gibt es kein Werkzeug für Skripte. Was als root läuft,
  kommt nur von der Person an der Weboberfläche.
- Anlegen, Ändern, Löschen, Starten und Abbrechen stehen im
  Aktivitätsprotokoll (Bereich „Skripte“) mit dem Namen des Skripts, nie mit
  seinem Inhalt.
- Löschen entfernt Skript und Protokoll vom Volume, nach einem einfachen
  Bestätigungsdialog in der Gefahrenzone des Editors.
- Ein App-Update wartet, bis kein Skript mehr läuft.

## Schreiben und Aufwecken von Platten

- Der Stick wird nur beim Wählen des Volumes beschrieben.
- Die Liste der Skripte liest TitanOS einmal, wenn das Volume eingebunden
  wird, und hält sie im Speicher. Der Planer sieht jede Minute nur dort nach;
  das Volume wird erst angefasst, wenn ein Skript wirklich läuft (Skript lesen,
  Lauf beginnen und beenden im Protokoll vermerken) oder wenn du die Seite
  bearbeitest. Ein geplantes Skript weckt eine schlafende Platte also nur zu
  seinem Termin, wie das Skript selbst es ohnehin täte.

## Grenzen und Prüfstand

- Ein Skript ist höchstens 256 KB groß; bis zu 200 Skripte je Volume.
- Geprüft (4.11.2026): Go-Tests des Pakets `scripts` (Ausgabe und Exit-Code,
  `#!`-Zeile, nur ein Lauf, Abbrechen mit SIGKILL nach ignoriertem SIGTERM,
  Zeitgrenze beim Herunterfahren, zehn Läufe, unterbrochene Läufe, Start einmal
  je Boot, Zeitplan mit verspätetem Planer, Meldungen, fehlendes Volume,
  Schreiben nur bei Änderungen, Cron-Felder und die ODER-Regel der Tagesfelder),
  `shutdown` (Skripte vor den Gästen, „Trotzdem jetzt“), API (Rechte, keine
  Tokens, kein MCP-Werkzeug) und die Browserprüfung
  `scripts/test-ui-scripts.mjs` mit isolierten Testdaten (1440 und 390 px,
  Graphit und Hell).
- Nicht geprüft: echte Läufe auf einem NAS in QEMU oder auf Hardware,
  Start-Skripte nach echtem Einbinden eines Volumes, Skripte beim echten
  Herunterfahren.
