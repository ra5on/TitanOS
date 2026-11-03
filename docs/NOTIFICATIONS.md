# Benachrichtigungen

## Tägliche Sammelmitteilung (seit 0.35.0)

Einstellungen › Benachrichtigungen › Allgemein: „Einmal täglich gesammelt
schicken“ mit Uhrzeit (Zeitzone des NAS). Statt jeder Meldung einzeln geht
dann einmal am Tag eine Zusammenfassung über alle eingeschalteten Wege und an
Geräte mit Mitteilungen: Titel „N Meldungen heute“, je Meldung Uhrzeit,
Zeichen der Stufe und Titel; die Stufe der Zusammenfassung ist die der
schwersten Meldung. „Fehler trotzdem sofort schicken“ (Vorgabe an) lässt
Fehler sofort hinaus. Die Glocke ist nicht betroffen; der Bericht über das
NAS geht immer zu seiner Zeit. Gesammelt werden höchstens 200 Meldungen im
RAM; ein NAS-Neustart vor der Uhrzeit verliert sie. Wird das Sammeln
ausgeschaltet, gehen wartende Meldungen sofort als Zusammenfassung hinaus.
Feld `digest` (`enabled`, `time`, `errorsNow`) in
`/config/notify/settings.json`; ältere titand ignorieren es.

## Meldungen zu Stick und Hardware (seit 0.41.0)

Diese Meldungen kommen von selbst, ohne Einstellung; sie erscheinen in der
Glocke und gehen über die eingeschalteten Wege hinaus.

- **Boot-Stick hat Fehler:** Der Kernel meldet Lese- oder Schreibfehler des
  Sticks, oder eine Änderung an der Konfiguration ließ sich nicht schreiben.
  Dann die Sicherung des Sticks prüfen (Einstellungen › Sicherungen) und den
  Stick bald tauschen.
- **Lüfter steht:** Ein Lüfter mit Drehzahlsensor dreht nicht mehr.
- **Kritische Temperatur:** Ein Sensor liegt zweimal in Folge über seiner
  Schwelle. Der Text nennt den Chip. Welche Schwelle je Sensor gilt, zeigt
  Einstellungen › Temperaturen › „Meldungen bei Hitze“; dort lassen sich
  einzelne Sensoren stummschalten (nur Besitzer und Administratoren, gilt für
  das ganze NAS). Eingänge von Mainboard-Chips ohne eigenen Grenzwert melden
  nie, weil unbelegte Eingänge oft Fantasiewerte liefern.
- **Speicherfehler (ECC):** korrigierte und nicht korrigierbare Fehler des
  Arbeitsspeichers, sofern das Mainboard sie meldet.
- **Unsauberes Ende:** Das NAS lief beim letzten Mal nicht bis zum sauberen
  Herunterfahren (Stromausfall, Absturz, Reset). Die Meldung nennt die Uhrzeit
  und, falls vorhanden, Hinweise aus den Absturzdaten des Kernels.

Jede Meldung kommt einmal je Ereignisfolge, nicht bei jeder Prüfung erneut.
Eine Notabschaltung bei Hitze gibt es nicht; die Hardware schaltet bei
kritischer Temperatur selbst ab.
