# Durchsicht der Weboberfläche (ohne KI-Agent) – Zusammenfassung

Stand 16.11.2026, Zweig `review/r4` (1.01). Alle Einzelheiten stehen in `durchsicht-102-oberflaeche.md`.

## Ergebnis in Zahlen

Gefunden und am Code bestätigt wurden 44 Funde:

| Schwere | Gefunden | Davon behoben |
|---|---|---|
| hoch | 5 | 5 |
| mittel | 19 | 16 |
| niedrig | 20 | 10 |

Die 13 übrigen Funde sind notiert oder brauchen deine Entscheidung (Liste unten). Unter den behobenen niedrigen ist eine Aufräumarbeit: ein ganzes ungenutztes Modul und drei ungenutzte Funktionen sind entfernt. Bewusst gelassen und nicht mitgezählt: einige Funktionen, die nur noch Tests benutzen.

## Was jetzt besser ist

**Die fünf wichtigsten Korrekturen:**
- **Bildschirmsperre:** Wenn das Sperren beim NAS nicht ankam, etwa wegen einer kurzen Netzstörung oder eines Neustarts nach einem App-Update, öffnete sich der Sperrbildschirm nach ein paar Sekunden von selbst, ohne Passwort. Jetzt wird die Sperre erneut gesendet; ohne Passwort geht nichts auf.
- **Rechte von Konten:** Wer ein App- oder Kamerarecht änderte und schnell zum nächsten Konto wechselte, sah dort die Rechte des vorigen Kontos. Ein weiterer Klick hätte diese Kamerarechte dem falschen Konto gegeben. Jetzt bleibt jede Antwort bei ihrem Konto.
- **Kopieren und Ordnerwahl:** Eine langsame Ordnerliste konnte unter einem anderen Ordner erscheinen. Kopieren, Verschieben oder ein Sicherungsziel gingen dann an den falschen Ort. Jetzt zählt nur die Liste des Ordners, der gerade angezeigt wird.
- **Lüftersteuerung:** Nach dem ersten Speichern sprang jede weitere Änderung nach wenigen Sekunden zurück. Das ist behoben.
- **Terminal:** Eine Konsole, die ein anderes Fenster geöffnet hatte, ließ sich nicht schließen; sie ging sofort wieder auf. Das ist behoben.

**Weitere Korrekturen:**
- **NVR:**
  - Ein geschlossenes Ereignis öffnete sich nicht mehr leer wieder.
  - Der Player der Aufnahmen hält sich an den zuletzt gewählten Abschnitt.
  - Beim Gegensprechen bleibt kein Mikrofon offen.
- **Web-Apps:** Das bisherige Programm wird erst gelöscht, wenn sein Nachfolger wirklich bereit ist.
- **Darstellung:** Eine Änderung, kurz bevor man die Seite verlässt, geht nicht mehr verloren.
- **Teilen aufs NAS:** Ein Doppeltipp lädt nicht mehr alles doppelt hoch, und jede Datei zeigt ihren eigenen Fortschritt.
- **Leere Seite:** Lassen sich die Texte oder der Arbeitsplatz nicht laden (z. B. direkt nach einem App-Update), bleibt die Seite nicht mehr leer und stumm.
- **Dialoge:** Nach dem Schließen geben sie den Tastaturfokus zurück.
- **Fehler werden gezeigt:** Abgelehntes Abbrechen eines Kopierauftrags und abgelehntes „Behalten“ einer Netzwerkänderung bleiben nicht mehr unbemerkt.
- **Regeln eingehalten:**
  - Kein abgeschnittener Text mehr bei Befehlszeilen, Pfaden, Adressen und Namen.
  - Tippflächen am Handy sind überall mindestens 44 px.
  - Warnungen der Glocke und auffällige SMART-Werte stehen auch in Worten da, nicht nur farbig.
  - Uhrzeiten von vor Tagen tragen ein Datum.
  - Zahlen erscheinen im Format der Sprache.

## Was bewusst nicht gebaut wurde

Diese Punkte sind notiert, aber nicht umgesetzt, weil sie mehr als eine Korrektur wären:
- **Fehler bei Docker-Aktionen am Arbeitsplatz:** Starten, Stoppen und Neustarten einer Docker-App über das Kontextmenü verschluckt Fehler.
- **Speichern der Arbeitsplatz-Einstellungen:** Ein Fehlschlag bleibt still.
- **Gezogene Dateien:** Auf den Arbeitsplatz gezogene Dateien können ohne Meldung verloren gehen, wenn der Dateimanager sie nicht annimmt.
- **Tastaturbedienung:** Reiter und Segmentknöpfe haben keine Pfeiltasten-Bedienung, und ein gewählter Menüeintrag lässt den Fokus fallen.

## Deine Entscheidung gefragt

1. **Meldungen am Arbeitsplatz:** Wo sollen Meldungen des Arbeitsplatzes erscheinen (fehlgeschlagene Docker-Aktion, nicht gespeicherte Einstellungen)? Als Mitteilung in der Glocke oder als kurzer Hinweis am Symbol?
2. **Kurzbefehle der Handy-App:** Die Namen der Kurzbefehle („Dateien“, „Systemsteuerung“) und die Beschreibung im Manifest gibt es nur auf Deutsch. Möglich ist ein Manifest je Sprache, wie beim KI-Agenten, oder englische Namen.
3. **Letzte NAS-Shell:** Wird sie im Terminal beendet, öffnet sich sofort eine neue. Ist das so gewollt?

## Prüfung

- **Unit-Tests:** Alle grün (Node 356, vitest 203). Neue Tests decken Sperre, Lüfter, Ordnerwahl, Kontenrechte, Dialogfokus und Teilen ab.
- **Gegenprobe:** Ohne die Korrektur scheitern die Tests für Ordnerwahl, Kontenrechte, Dialogfokus und Teilen sowie die neuen Browser-Prüfungen für NVR und Terminal. Sperre und Lüfter prüfen neue Funktionen, die es vorher nicht gab.
- **Typprüfung und Formatierung:** fehlerfrei.
- **Browser-Prüfungen** mit Testdaten gegen die gebaute Oberfläche: Fenster, Systemsteuerung, NVR, VMs, Arbeitsplatz, Sperre, Dateien/Speicher und weitere, alle grün.
- **Ausnahmen `test-ui-phone` und `test-ui-workflows`:** Beide scheitern auch auf dem Stand vor der Durchsicht; ihre Erwartungen sind veraltet. `test-ui-phone` erwartet am Handy noch mindestens 20 Einstellungszeilen, seit 0.87 gibt es dort nur eine Zeile je Bereich. `test-ui-workflows` sucht in der Befehlspalette einen Eintrag „Zugang › SSH“, den es so nicht mehr gibt.

**Nicht geprüft:** echte Kameras und Mikrofon, echte Handys und Safari, echtes Herunterladen von Web-Apps, echter Netzausfall.
