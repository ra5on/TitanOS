# Aktivitätsprotokoll

Unter **Werkzeuge › Aktivität** sehen Besitzer und Administratoren, wer wann
was am NAS geändert hat. Konten mit der Rolle Benutzer sehen das Protokoll nicht.

## Was aufgezeichnet wird

- **Jede ändernde Anfrage an die API** (POST, PUT, PATCH, DELETE) eines
  angemeldeten Kontos: Zeit, Konto, IP-Adresse, Route, Ziel, HTTP-Status und ob
  sie gelungen ist. Abgewiesene Versuche, etwa eines Benutzers an einer
  Administratorfunktion, stehen als fehlgeschlagen im Protokoll.
- **Anmeldungen:** Anmeldung, Abmeldung, fehlgeschlagene Anmeldung (falsches
  Passwort oder falscher Zwei-Faktor-Code, mit dem eingegebenen Kontonamen) und
  gesperrte Versuche nach zu vielen Fehlversuchen sowie die Ersteinrichtung.
- **Konsolen und sensible Downloads:** NAS-Konsole, Container-Konsole,
  serielle VM-Konsole, VM-Bildschirm, Download der Konfigurationssicherung und
  das Diagnosepaket.
- **SSH-Anmeldungen** (Herkunft „per SSH“): Konto, Adresse und als Ziel Art und
  SHA256-Fingerabdruck des Schlüssels, dazu der Kommentar des passenden
  Schlüssels aus „Mein Konto“ (`ED25519 SHA256:… (laptop)`). Abgewiesene
  Anmeldungen eines SSH-berechtigten Kontos aus einem nicht erlaubten Netz
  stehen als `ssh_login_blocked`. titand liest dafür die Zeilen
  „Accepted publickey …“ bzw. „… not listed in AllowUsers“ von sshd aus dem
  Journal (`journalctl --follow`, `sshd-session`/`sshd`); Versuche mit
  unbekannten Namen bleiben außen vor.
- **Herkunft:** Weboberfläche, API-Token, Assistent/Home Assistant oder SSH. Aufträge
  des Assistenten laufen über dieselbe API und stehen unter dem Konto, für das er
  handelt.

Das **Ziel** ist der Name aus der Route (Konto, Freigabe, VM, App …). Bei
Dateiaktionen kommen die betroffenen Pfade als `Freigabe/Pfad` dazu, höchstens
20 pro Eintrag; bei Downloads von einem Link nur Schema und Server, weil Abfragen
oft Zugangsschlüssel enthalten.

**Nie gespeichert** werden Inhalte von Anfragen: Passwörter, Setup-Keys, Tokens,
Schlüssel, Dateiinhalte und Compose-Dateien. Einmal-Tokens in Pfaden werden
ausgelassen. Steuerzeichen werden ersetzt, damit ein Eintrag eine Zeile bleibt.

Nicht aufgezeichnet werden häufige oder private Vorgänge ohne Änderung am NAS:
Teilstücke von Uploads, Vorschauen, Suchen, Ordnervergleiche, die eigene
Dashboard-Anordnung und das Hintergrundbild, das Ausblenden von
Benachrichtigungen sowie Chats mit dem Assistenten. Normale Lesezugriffe und
Datei-Downloads bleiben ebenfalls außen vor.

## Ablage

Einträge werden einige Sekunden gesammelt und dann als tägliche JSON-Zeilen
(`JJJJ-MM-TT.jsonl`) unter `.titanos/activity/` auf einem Daten-Volume
geschrieben, nie auf den Stick. Standard ist das erste eingebundene Volume;
ein festes Volume ist wählbar. Die Einstellungen liegen in
`/config/system/activity.json`.

- Aufbewahrung 30, 90 (Standard) oder 365 Tage, Speichergrenze 8 bis 256 MiB
  (Standard 64). Ältere Tage werden zuerst entfernt; der laufende Tag nie.
- Ohne eingebundenes Volume bleiben die letzten 500 Einträge zur Anzeige und bis
  zu 5.000 wartende Einträge im Arbeitsspeicher. Sie werden geschrieben, sobald
  ein Volume da ist; bei einem Neustart vorher gehen sie verloren. Verworfene
  Einträge werden auf der Seite gezählt.
- Ein abgerissener letzter Eintrag nach einem Stromausfall wird beim nächsten
  Schreiben entfernt; unlesbare Zeilen werden beim Lesen übersprungen.
- Ausschalten wird selbst noch protokolliert, danach nichts mehr.

## Anzeige und Export

Filter nach Zeitraum (24 Stunden bis 365 Tage), Konto, Suchtext und nur
Fehlschlägen; ältere Einträge werden seitenweise nachgeladen. Export als CSV
oder JSON mit denselben Filtern, höchstens 50.000 Einträge. In der CSV-Datei
werden Namen, die mit `=`, `+`, `-` oder `@` beginnen, mit `'` versehen, damit
Tabellenprogramme sie nicht als Formel ausführen.

API: `GET /api/v1/activity` (`days`, `user`, `q`, `failed=1`, `before`,
`limit`, `format=csv|json`) und `PUT /api/v1/activity` für die Einstellungen;
beide nur für Besitzer und Administratoren, nicht über API-Tokens.

## Grenzen

Das Protokoll ist eine Nachvollziehbarkeitshilfe, kein manipulationssicheres
Revisionsprotokoll: Wer Zugriff auf die NAS-Konsole oder das Volume hat, kann die
Dateien ändern. In der Oberfläche gibt es keine Löschfunktion für Einträge.
Zugriffe über SMB und NFS laufen nicht über titand und erscheinen nicht. Von SSH
erscheint nur die Anmeldung, nicht was in der Sitzung geschieht. Anmeldungen,
während titand nicht läuft (etwa während eines App-Updates), werden nicht
nachgetragen, und das Journal liegt im RAM.
Allgemeine Einträge werden nach Bereich und Methode benannt („System:
geändert“); die genaue Route steht darunter.

Prüfung: `go test ./internal/activity ./internal/api -run Activity`,
`web/tests/activity.test.mjs` und die Browserprüfung `scripts/test-ui-activity.mjs`
mit isolierten Daten.
