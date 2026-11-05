# Aufgaben und Erinnerungen planen
_Mitgeliefert mit TitanOS, wird bei Updates ersetzt. Eigene Anleitungen gehören in den Ordner Wissen außerhalb von „TitanOS“._

Diese Anleitung erklärt, wie der Agent Aufgaben und Erinnerungen zeitlich plant: einmalig, wiederkehrend oder als Morgenbericht, und was mit verpassten Terminen passiert.

## Grundlagen

- Du sagst es im Chat in normaler Sprache („erinnere mich morgen um 9 an den Müll“). Der Agent legt den Plan mit `schedule_create` an und nennt dir danach genau, wann er läuft („am 06.11.2026 um 09:00“).
- Alle Zeiten gelten in der **Zeitzone des NAS**.
- Die geplanten Aufträge deines Kontos zeigt `schedule_list` mit dem nächsten Lauf. In der Oberfläche findest du sie im Agent-Menü unter **Geplant** (mit Pausieren, Ändern, Jetzt ausführen, Löschen).
- Jeder Lauf bekommt einen eigenen Chat mit Name und Uhrzeit im Titel. Das Ergebnis kommt als Mitteilung aufs Handy.
- Ein geplanter Lauf hat dieselben Rechte wie der Agent sonst. Was eine Freigabe braucht, wartet im Chat auf dich.

## Zwei Arten

| Art | Was passiert |
|---|---|
| **Auftrag** (Standard) | Der Agent arbeitet die Aufgabe zur Zeit ab und meldet das Ergebnis. |
| **Erinnerung** (`kind: reminder`) | Nur eine Mitteilung mit deinem Text, als Chat-Eintrag und Push aufs Handy; der Agent führt nichts aus. |

- Eine Erinnerung kommt immer, auch wenn der Agent gerade offen ist. Ist Home Assistant mit Mitteilungen eingerichtet, geht sie zusätzlich dorthin.
- „Erinnere mich …“ ist eine Erinnerung. „Mach …“, „Sortier …“, „Prüf …“ ist ein Auftrag.

## Einmalig

1. Mit Datum und Uhrzeit: `every: once`, `date: JJJJ-MM-TT`, `at: HH:MM` (24 Stunden).
   - „morgen um 9“ → Datum von morgen, `09:00`.
   - „am 24.12. um 18 Uhr“ → `2026-12-24`, `18:00` (liegt das Datum schon hinter uns, das nächste Jahr).
2. In einer Zeitspanne: `every: once` mit `in_minutes`.
   - „in 2 Stunden“ → `in_minutes: 120`.
3. „Mach das bis Freitag 18 Uhr fertig“: den Auftrag so legen, dass er rechtzeitig vorher läuft, und das im Chat sagen.
4. Ist die Angabe unklar („nächste Woche“), fragt der Agent mit Knöpfen nach Tag und Uhrzeit.

## Wiederkehrend

| Wunsch | Einstellung |
|---|---|
| alle 15 Minuten | `every: minutes`, `interval: 15` (mindestens 5, höchstens 1440) |
| alle 3 Stunden | `every: hours`, `interval: 3` (1–168) |
| jede volle Stunde | `every: hourly` |
| täglich um 7:30 | `every: daily`, `at: 07:30` |
| alle 2 Tage um 20 Uhr | `every: days`, `interval: 2`, `at: 20:00` (1–365) |
| Montag bis Freitag um 7 | `every: weekdays`, `weekdays: [1,2,3,4,5]`, `at: 07:00` |
| jeden Sonntag um 10 | `every: weekly`, `weekday: 0`, `at: 10:00` |
| am 1. jedes Monats um 8 | `every: monthly`, `day: 1`, `at: 08:00` (Tag 1–28) |
| für Kenner: Cron-Zeile | `every: cron`, `cron: "0 7 * * 1-5"` |

- **Wochentage:** 0 = Sonntag, 1 = Montag … 6 = Samstag.
- **Cron:** fünf Felder: Minute, Stunde, Tag, Monat, Wochentag. Vorsicht bei „jeden ersten Montag“: In klassischem Cron verknüpft `0 9 1-7 * 1` Tag und Wochentag oft mit „oder“ (also jeden 1.–7. und jeden Montag). Sicherer: wöchentlich montags planen und in der Aufgabe sagen „nur wenn heute der erste Montag im Monat ist“. Nach dem Anlegen prüft der Agent mit `schedule_list`, ob der nächste Lauf stimmt.
- Den Monatstag 29–31 gibt es nicht, weil nicht jeder Monat ihn hat. Für „Monatsende“ den 28. nehmen oder Cron.

## Auslöser aus Home Assistant

Ein Auftrag kann auch starten, wenn eine Entität in Home Assistant einen Zustand erreicht (z. B. „Waschmaschine fertig“), auf Wunsch erst nach einigen Minuten in diesem Zustand. Mit dem Häkchen „Als Knopf in Home Assistant anbieten“ erscheint er dort als Knopf.

## Morgenbericht

- Ein besonderer geplanter Auftrag, **ab Werk aus**. Einrichten im Agent-Menü unter **Geplant › Morgenbericht**: Uhrzeit, Wochentage und Inhalt stellt jedes Konto selbst ein. Der Agent legt ihn nicht im Chat an, er verweist auf diese Stelle.
- Inhalt je Konto wählbar:
  - NAS-Zustand und Speicher (Volumes, SMART, Temperatur, freier Platz, Auffälliges der Nacht);
  - Sicherungen und Updates;
  - Erinnerungen und Aufträge des Tages;
  - Home Assistant (z. B. Strom von gestern, offene Fenster, schwache Batterien);
  - ein eigener Wunschtext.
- Der Morgenbericht **ändert nie etwas**; er liest nur und fasst kurz zusammen, das Wichtigste zuerst.

## Verpasste Termine werden nachgeholt

War das NAS zur geplanten Zeit aus (oder der Dienst neu gestartet), holt TitanOS den Lauf nach dem Start **einmal** nach:

1. Wiederkehrende Aufträge: wenn der Termin höchstens **12 Stunden** zurückliegt.
2. Einmalige Aufträge und Erinnerungen: wenn der Termin höchstens **7 Tage** zurückliegt.
3. Ein nachgeholter Lauf trägt im Titel „(nachgeholt)“.
4. Mehrere verpasste Läufe eines wiederkehrenden Auftrags ergeben nur einen Nachholer, nicht viele.
5. Ältere Termine verfallen still; der nächste reguläre Lauf kommt wie geplant.

Das gilt für Aufträge des Agenten. Andere Zeitpläne von TitanOS haben eigene Regeln (Backups holen verpasste Termine nach, VM-Snapshots nicht).

## So plant der Agent sauber

1. Aus deinem Satz Art (Auftrag oder Erinnerung), Zeitpunkt und Wiederholung herauslesen.
2. Bei Unklarheit nachfragen, nicht raten.
3. Einen kurzen Namen wählen („Rechnungen sortieren“, „Müll rausstellen“).
4. Die Aufgabe so formulieren, dass sie später ohne den Chat verständlich ist (Ordner, Freigabe, Ziel nennen).
5. `schedule_create` aufrufen und die Antwort mit dem nächsten Lauf wiedergeben.
6. Bei Aufträgen, die etwas ändern: daran erinnern, dass Freigaben im Chat auf dich warten.
7. Ändern oder Löschen eines Plans: in der Übersicht „Geplant“ im Agenten.

## Beispiele

- „Erinnere mich in 30 Minuten an den Kuchen“ → Erinnerung, einmalig, `in_minutes: 30`.
- „Sortier jeden Montag um 8 die neuen Rechnungen“ → Auftrag, `weekly`, `weekday: 1`, `at: 08:00`.
- „Prüf alle 6 Stunden, ob die Platten zu warm sind, und melde dich nur dann“ → Auftrag, `hours`, `interval: 6`; die Aufgabe sagt „nur bei Auffälligem melden“.
- „Schick mir werktags um 7 einen Morgenbericht“ → auf **Geplant › Morgenbericht** verweisen (Mo–Fr, 07:00 dort einstellen).
