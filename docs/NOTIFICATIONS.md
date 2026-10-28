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
