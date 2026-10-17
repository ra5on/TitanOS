# Beta 7: Konfigurationssicherung und Benachrichtigungstöne

## Sicherung des Sticks

**Einstellungen › Sicherung des Sticks** bietet zusätzlich einen täglichen oder
wöchentlichen Termin mit Uhrzeit in der NAS-Zeitzone und Wochentag. Die bisherige
Sicherung nach dem Start und fünf Minuten nach Änderungen bleibt aktiv. Nach
einem ausgeschalteten NAS übernimmt die Startsicherung die Prüfung. Bei einem
fehlenden Volume wird erneut geprüft, nach einem Fehler nach einer Stunde.

Es wird `/config` gesichert, kein vollständiges Abbild des USB-Sticks. Unveränderte
Inhalte erzeugen kein zusätzliches Archiv. Die Oberfläche zeigt den nächsten
Termin, die letzte Prüfung und die jüngste vorhandene Sicherung. Die automatische
Aufbewahrung bleibt einstellbar. Alle regelmäßigen Zeitplanabfragen verwenden
Einstellungen im RAM; nur ein tatsächlicher Sicherungslauf liest den Stick.

## Töne

Unter **Mein Konto** und **Einstellungen › Benachrichtigungen** steht dieselbe
persönliche Tonauswahl: sanfte Glocke, warmer Holzklang, weicher kurzer Klang,
dezentes Klangspiel und leiser Doppelton. Standard ist **Kein Ton**. Lautstärke,
Vorschau und Auswahl „Alle Meldungen“ oder „Warnungen und Fehler“ gehören dazu.
Die Einstellungen liegen pro Konto unter `/config/users/<name>/sound.json`.

Die Klänge werden lokal aus weich ein- und ausgeblendeten Sinustönen erzeugt.
Es werden keine Audiodateien von anderen Servern geladen. Ein Klick oder eine
Taste im geöffneten Browser erlaubt die Wiedergabe; Browser- und
Betriebssystemeinstellungen können sie weiterhin verhindern. Bereits vorhandene
Meldungen beim Öffnen der Seite werden nicht abgespielt. Innerhalb von zehn
Sekunden gibt es höchstens einen Benachrichtigungston. Ohne offene Oberfläche
ist dies kein Hintergrunddienst für Audio.

## Browser-Zertifikate

Seit der Korrektur in Beta 3 besitzen selbst signierte Zertifikate eindeutige
Ausstellernamen. Beim ersten Start aus Beta 2 wird ein altes Zertifikat einmal
ersetzt. Dadurch wird die Verwechslung mit einem früheren TitanOS-Zertifikat
vermieden, die Chrome/Firefox vollständig blockieren konnte. Eine normale
Vertrauenswarnung bleibt bei selbst signierten Zertifikaten erwartbar.
Eigene Zertifikate werden durch diese Migration nicht ersetzt.
