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

## Prüfstand

Lokale Go-Tests einschließlich echter restic-Sicherung/Rücksicherung, Web-Tests,
Svelte-Typprüfung und 23 Release-Verpackungs-/Signaturtests bestanden. Getestet
mit Chrome 155, Edge 155 und Firefox 157 auf 360 und 1440 Pixeln: Einstellungen
für Töne, Stick-Sicherung, Backups und Nutzerkonto ohne Seitenüberlauf oder
JavaScript-Fehler. Die persönliche Tonauswahl übersteht einen Seitenreload.
HTTPS mit dem aktuellen Zertifikatscode ist nach Zulassen des selbst signierten
Zertifikats in allen drei Browsern erreichbar. Die automatisierte Vorschau
prüft den Wiedergabepfad, nicht die akustische Wirkung an echten Lautsprechern.

Zusätzlich in einem isolierten QEMU-NAS mit virtuellen FAT- und Btrfs-USB-Laufwerken geprüft:
Mount-Identität, zwei Kopierläufe mit geänderter und gelöschter Quelldatei,
Auswurfsperre und sicheres Auswerfen. Linux-Partitionen, die ein FAT-Dateisystem
auf dem ganzen Gerät vollständig abbilden, werden nur einmal angeboten;
identische UUIDs auf getrennten Geräten bleiben ein Fehler.

Der tägliche Konfigurationszeitplan wurde im laufenden Test-NAS ebenfalls
geprüft: Startsicherung und erneute Sicherung zum eingestellten Termin ohne
Änderungszähler-Auslöser. Der Browser-Test prüft zusätzlich echte Web-Audio-
Aufrufe für neue Meldungen, stille Historie, Schweregradfilter und Drosselung.
