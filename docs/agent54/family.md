# PLAN 54-G: Gemeinschaft und Familie

Zwei Funktionen des KI-Agenten: Backup-Buddy und Skill-Katalog. Das Familien-Tagebuch (Werkzeug `diary`) wurde mit 0.69 entfernt (Entscheidung des Projektinhabers vom 8.11.2026); seine Einstellungen `diary` in `features.json` fallen beim nächsten Speichern weg, ältere Versionen lesen sie als „aus“. Jede Funktion ist ab Werk aus. Eingeschaltet werden sie unter Einstellungen › KI-Agent › Funktionen (Besitzer und Administratoren). Die Werte nach dem Einschalten folgen §54-G und lassen sich alle ändern (§54-E).

## Backup-Buddy

**Bedienung.** Unter Funktionen › Backup-Buddy trägt man die Adresse dieses NAS ein, so wie der Partner es erreicht, z. B. `nas-eva.tail1234.ts.net`. Dazu kommen das Volume für die Sicherungen der Partner, der Zeitplan (ab Werk wöchentlich, Sonntag 02:00), die Probe-Wiederherstellung (ab Werk monatlich, 50 Stichproben), die Schutzfrist (ab Werk 30 Tage). Der Port ist fest 8752 (Dienst „buddy“ der Firewall, ab Werk zu). Die Adresse muss eine Tailscale-Adresse (100.64.0.0/10, fd7a:115c:a1e0::/48) oder ein Name unter `*.ts.net` sein; das Token läuft über einfaches http, das nur WireGuard verschlüsselt. „Backup-Buddy öffnen“ zeigt die Ansicht `buddy` wie im Entwurf 11:

- jeden Partner mit letzter Sicherung, Platz beim Partner und Probe-Wiederherstellung;
- was der Partner hier belegt;
- „Neues TitanOS koppeln“.

**Koppeln.** NAS A legt fest, wie viel Platz es dem Partner gibt und welche eigenen Freigaben es beim Partner sichert. Dann zeigt es einen Code wie `K7Q-4MZ-92X`. Der Code gilt 10 Minuten und nur einmal; nach 5 falschen Versuchen verfällt er. Auf NAS B gibt man die Adresse von A und den Code ein und legt ebenfalls Platz und Freigaben fest. B schickt A in einer Anfrage seinen Namen, seine Adresse, den Namen, den es A gibt, und das Token, mit dem A sich bei B anmeldet. A antwortet mit dem Gleichen in der anderen Richtung. Jede Seite speichert nur den SHA-256 des Tokens, das sie selbst ausgegeben hat. Ist ein NAS mit derselben Adresse oder demselben Namen schon gekoppelt, wird abgelehnt; dabei und bei voller Liste bleibt der Code gültig. Der Name des Partners hier bekommt einen zufälligen Anhang (z. B. `nas-tom-3fa92c`) und wird nie wieder vergeben, solange sein Ordner existiert, damit ein neuer Partner nie alte Daten erbt. Das Token für den Partner liegt nur in `/config` und im Sicherungsauftrag.

**Senden.** Für jeden Partner mit gewählten Freigaben gibt es einen gewöhnlichen Sicherungsauftrag `buddy-<partner>` der Art `rest`. Er steht auch auf der Seite Sicherungen, mit Wiederherstellen und Prüfen. Lässt er sich nicht speichern, zeigt die Ansicht den Grund beim Partner. Geschrieben wird der Auftrag nur, wenn sich Freigaben, Zeitplan oder Ziel wirklich ändern; ein Start von titand schreibt nichts auf den Stick. restic verschlüsselt mit dem Passwort des Auftrags; das Passwort verlässt dieses NAS nie. Das Token geht als `RESTIC_REST_PASSWORD` in die Umgebung von restic, nie in die Adresse oder die Kommandozeile. Beim Koppeln nennt jede Seite ihre Schutzfrist. Behalten werden täglich so viele Stände wie die Schutzfrist des Partners plus ein Tag (mindestens 31), dazu 8 wöchentliche und 12 monatliche. Die monatliche Probe startet der Agent direkt nach einer gelungenen Sicherung (höchstens eine Stunde danach), weil die Platten dann wach sind. Danach wartet sie 30 Tage.

**Empfangen (Paket `backend/internal/buddy`).** Ein kleiner Server spricht das REST-Protokoll von restic (v1 und v2): `POST /?create=true`, `config`, Listen, `HEAD/GET` mit Range, `POST`, `DELETE`. Er lauscht nur auf den Tailscale-Adressen dieses NAS (100.64.0.0/10, fd7a:115c:a1e0::/48), auf Port 8752, ohne TLS, mit Zeitgrenzen (Kopf 15 s, Anfrage und Antwort je 30 Minuten, Leerlauf 2 Minuten), höchstens 32 Verbindungen je Adresse; die Verbindung verschlüsselt WireGuard. Im Entwicklungsmodus lauscht er nicht. Ist der Agent oder der Backup-Buddy aus, beantwortet er nichts (404); der Zeitplan der Aufträge läuft als gewöhnliche Sicherung weiter, die monatliche Probe startet nur bei eingeschaltetem Agenten. Pfade:

- `/v1/pair`: die Kopplungsanfrage, nur mit gültigem Code;
- `/v1/<partner>/…`: Anmeldung per Basic mit Partnername und Token. Ein falsches Token, ein fehlendes und der Ordner eines anderen Partners bekommen dieselbe 401.

Abgelegt wird unter `.titanos/buddy/<partner>/` auf dem gewählten Volume (root, 0700, über `volumedata.Open`). Ein nicht eingebundenes Volume wird abgelehnt (503); nichts landet im RAM-Overlay.

**Löschregel (nur anhängen, Quarantäne).** Eine vorhandene Datei wird nie ersetzt. `config` und `keys` werden nie gelöscht, `locks` sofort, denn restic entfernt nach jedem Lauf seine Sperre. Löscht der Partner eine Datei aus `data`, `index` oder `snapshots`, wird sie nicht entfernt. Sie wandert in die Quarantäne `.trash/<Zeitpunkt in ns>/<typ>/…` im Ordner des Partners. Die Quarantäne erscheint in keiner Liste für den Partner und zählt zu seinem Kontingent. Dieses NAS leert eine Quarantäne erst, wenn die Schutzfrist (ab Werk 30 Tage) seit dem Löschen vorbei ist; geprüft wird höchstens stündlich und nur bei einer Anfrage des Partners, wenn die Platte ohnehin wach ist. So läuft `restic forget --prune` beim Absender ohne Fehler. Ein übernommener Absender kann trotzdem nichts vernichten: Auch alte Pakete, die heutige Stände dank Deduplizierung noch brauchen, liegen dann in der Quarantäne.

**Zurückholen aus der Quarantäne** (von Hand, als root auf diesem NAS, innerhalb der Schutzfrist):

1. Den Sicherungsauftrag beim Partner anhalten.
2. Im Ordner `/mnt/<volume>/.titanos/buddy/<partner>/` jede Datei aus `.trash/<zeit>/` an denselben relativen Pfad zurückschieben (`.trash/<zeit>/data/ab/<id>` → `data/ab/<id>`), sofern dort nichts liegt.
3. Beim Partner `restic repair index` ausführen; danach zeigt `restic snapshots` die Stände wieder.

Der Test mit echtem restic macht genau das.

**Kontingent.** Jeder Partner hat sein Kontingent. Ein Hochladen darüber hinaus wird vor dem Ablegen mit 507 abgelehnt, mit und ohne `Content-Length`. Der Platz wird vor dem Kopieren reserviert (die Länge oder der Rest), sodass gleichzeitige Uploads ihn nicht gemeinsam überschreiten; was nicht gebraucht wird, geht zurück. Sperrdateien (je höchstens 64 KiB) gehen auch bei vollem Kontingent hinein, damit restic aufräumen kann, aber höchstens 64 auf einmal. Ein Partner hat höchstens 8 Anfragen gleichzeitig (sonst 429). Der Verbrauch wird beim ersten Zugriff des Partners nach dem Start von titand einmal gezählt und danach im RAM fortgeschrieben; die Seite liest dafür keine Platte. Bis dahin zeigt sie nur das Kontingent.

**Daten.** Partner mit Tokens: `/config/system/agent-buddy.json`, geschrieben nur beim Koppeln, Ändern und Lösen. Einstellungen: `agent.json` › `features.buddy`. Ein `PUT /agent/features` kann die Partner nicht ändern. Routen (nur Besitzer/Admin, nur Web-Sitzung, nicht in `publicRoutes`):

- `GET /agent/buddy`
- `POST`/`DELETE /agent/buddy/code`
- `POST /agent/buddy/pair`
- `PUT`/`DELETE /agent/buddy/partners/{id}` (`?data=1` löscht auch die Sicherungen des Partners hier)

## Skill-Katalog

**Herkunft (Entscheidung des Inhabers 6.11.2026).** Die Skills kommen aus dem App-Store: aus dem Ordner `skills/` in ra5on/TitanOS-Apps und in jeder eigenen Quelle, die der Besitzer unter Apps eingetragen hat. Geladen werden sie vom Paket `backend/internal/catalog` zusammen mit den Apps (gleicher Download des Archivs, nur im RAM, nach dem Start und einmal am Tag). Es gibt keinen eigenen Schlüssel und keine eigene Adresse. Vertraut wird ihnen wie den Apps derselben Quelle. Eine Quelle, die nur Skills enthält, gilt als gültige Quelle. Die eingebauten Skills bleiben wie bisher in TitanOS.

**Format.** Je Skill ein Ordner mit einer Datei `skills/<name>/skill.yaml`; eine Vorlage liegt in `docs/apps-repo/skill-template.yaml`.

- `<name>`: Kleinbuchstaben, Ziffern, Bindestriche, höchstens 64 Zeichen.
- `name` (Pflicht, höchstens 80 Zeichen), `when` (Pflicht), `steps` (Pflicht, mit `when` höchstens 4000 Zeichen).
- `tools`: Liste von Werkzeugnamen, höchstens 40.
- `description` und `author`: freiwillig.

YAML passt zum Paket `catalog`, das aus den Archiven ohnehin nur YAML-Dateien behält. Ein Skill, der nicht passt, wird übersprungen; die anderen bleiben. Das sind: kaputtes YAML, fehlende Pflichtfelder, zu lang, ein Werkzeugname, der kein Name ist, ein doppelter Ordner.

**Bedienung.** Unter Funktionen gibt es nur den Schalter, ab Werk aus. „Katalog öffnen“ zeigt die Ansicht `catalog` wie im Entwurf 12. Jedes Konto des Agenten kann sie öffnen, wenn die Funktion an ist. Jeder Skill zeigt seine Quelle („TitanOS-Apps“ oder den Namen der eigenen Quelle). Tippt man auf einen Skill, zeigt der Agent Name, Quelle, Autor, wann er gilt, alle Schritte und die Werkzeuge. Erst danach kommt „Installieren“.

Unter „Eigene Skills teilen“ lädt man einen eigenen Skill als Paket herunter: JSON mit `format: titanos-skill-submission`, Name, Wann, Schritten, erkannten Werkzeugen und dem Zielpfad `skills/<name>/skill.yaml`, ohne Kontonamen. Eingereicht wird im Repository.

**Installieren.** Der Server nimmt den Skill aus dem, was der Katalog gerade im RAM hat, unter `<quelle>/<name>`. Er nimmt ihn nur, wenn der Eintrag genau dem gezeigten entspricht (SHA-256 des Eintrags samt Quelle); sonst kommt 409. Abgelehnt wird auch:

- ein Werkzeug, das das Konto nicht hat; gezählt werden die genannten und die in den Schritten vorkommenden Namen wie `files_search`;
- ein gleichnamiger eigener Skill.

Gespeichert wird über `skill_save` mit dessen Regeln (keine Geheimnisse, Länge). Der Skill gilt danach als fremder Inhalt (`untrusted`) und trägt in der Beschreibung `Skill-Katalog: <quelle>/<name>`. Ein Skill ändert selbst nichts. Werkzeuge, die etwas ändern, fragen wie immer.

Routen (jedes Konto des Agenten, nur Web-Sitzung): `GET /agent/catalog`, `POST /agent/catalog/install` (`key`, `digest`), `GET /agent/catalog/export/{id}`.

## Geprüft

- `go test ./internal/buddy`: Protokoll wie restic es schickt (anlegen, config, hochladen, Listen v1/v2, Range), Anmeldung (falsches/fehlendes Token, fremder Ordner), Kontingent (mit und ohne Länge, Sperre trotz vollem Kontingent), nur anhängen (kein Überschreiben, Quarantäne nicht gelistet, gezählt und erst nach der Schutzfrist geleert), reserviertes Kontingent bei gleichzeitigen Uploads, Grenze für Sperrdateien und gleichzeitige Anfragen, Zeitgrenzen und Verbindungsgrenze des Servers, nur Tailscale-Adressen, Volume weg, Ablauf und Raten des Codes, Kopplung hin und zurück, Adressen. **Mit dem echten restic dieses Rechners** (0.18): `init`, zwei `backup`, `check --read-data`, `forget --keep-last 1 --prune` ohne Ablehnung, `check`; danach die Quarantäne zurückgeschoben, `repair index`, beide Stände wieder da und der vergessene Stand identisch wiederhergestellt. Die Empfangsseite enthält keinen Klartext.
- `go test ./internal/api -run Family54`:
  - ab Werk aus;
  - Prüfung der Einstellungen;
  - Benutzer sehen den Buddy nicht;
  - Kopplung zweier Test-Server mit Code (falsch, einmalig, abgelaufen);
  - Sicherungsauftrag auf beiden Seiten (Art `rest`, Adresse ohne Token, Benutzername, Aufbewahrung), folgt täglich/wöchentlich/monatlich, verschwindet ohne Freigaben;
  - Hashes passen zu den Tokens, kein Token in den Ansichten, Partner bleibt bei `PUT /agent/features`, Lösen sperrt;
  - Katalog aus einer gefälschten App-Store-Quelle (Archiv mit `skills/`): kaputter Skill übersprungen, Quelle angezeigt, unbekanntes Werkzeug abgelehnt, geänderter Eintrag (Digest), unbekannter Skill, Installation als fremder Inhalt, Export;
- `go test ./internal/catalog -run Skills`: Skills aus dem offiziellen Archiv und aus einer eigenen Quelle nur mit Skills, mit Quellennamen; kaputtes YAML, fehlende Schritte, falscher Werkzeugname und falscher Ordnername übersprungen; die Apps bleiben unverändert;
  - Kopplung: zufällige, nie wiederverwendete IDs; Schutzfrist ausgetauscht und Aufbewahrung danach; Lösen mit 409, solange der Auftrag läuft, sonst mit dem Auftrag; Doppelkopplung und volle Liste abgelehnt, ohne den Code zu verbrauchen;
- Web: `tests/agent54-family.test.mjs`; `scripts/test-ui-agent54-family.mjs` mit gemockter API bei 390×844 und 1440×900. Bilder liegen in `out/validation/v057-family/`.

## Grenzen, nicht geprüft

- **Zwei echte NAS** und ein Lauf über echtes Tailscale sind nicht getestet. Geprüft sind restic gegen den Server im selben Prozess und zwei Test-Server mit Kopplung über HTTP.
- Der Buddy-Server lauscht nur über Tailscale. Andere Wege (WireGuard, NetBird, Heimnetz) gibt es nicht, weil ohne TLS sonst das Token im Klartext liefe.
- „Ich sichere bei …“ zeigt die Datenmenge des letzten Laufs, nicht den belegten Platz beim Partner (dedupliziert und komprimiert meist weniger).
- Was `prune` löscht, belegt bis zum Ende der Schutzfrist weiter Platz im Kontingent. Ein Partner braucht deshalb etwas mehr Kontingent als seine Daten.
- Das Zurückholen aus der Quarantäne hat keine Oberfläche; es ist ein Eingriff von Hand (siehe oben).
- Den Ordner `skills/` in ra5on/TitanOS-Apps schreibt TitanOS selbst (Entscheidung 8.11.2026, keine Skills aus fremden Quellen).
- Die Werkzeugerkennung in den Schritten zählt Wörter wie `files_search`; ein Wort dieser Form, das kein Werkzeug ist, verhindert die Installation.
- Quellen unter der Antwort und Menüeinträge sind beim Zusammenführen eingebaut.

## Nötige Änderungen an gemeinsamen Dateien

Genau als Patch in `docs/agent54/family-shared-files.patch`:

- `web/src/lib/components/agent/ChatView.svelte`: `FamilyDiarySources` unter der Antwort.
- `AgentMenu.svelte` und `routes/agent/+page.svelte`: Menüeinträge „Backup-Buddy“ (nur Besitzer/Admin) und „Skill-Katalog“, wenn die Funktion an ist.

Dazu, ohne Patch:

- `backend/internal/firewall/services.go`: Dienst `{ID: "buddy", Ports: []PortSet{{"tcp", []string{"8752"}}}, Default: local}` neben `backuptarget`, mit Text dazu in `docs/FIREWALL.md` (auf dem zusammengeführten Zweig schon erledigt).
- `backend/internal/api/ipv64_public.go` (`reservedPort`): 8752 zu `22, 80, 443, 8750` (schon erledigt).
- `docs/rules/API.md`, Abschnitt KI-Agent: die neuen Routen und dass der Buddy-Server ein eigener Zugang außerhalb von Sitzung und `publicRoutes` ist (eigener Port nur auf Tailscale, Basic mit Partner-Token, nur der eigene Ordner, `/v1/pair` nur mit gültigem Code).
- `docs/rules/BACKUPS.md`: Verweis auf den Backup-Buddy neben „Anderes TitanOS“.

Nicht genommen wurde der Weg über den Port der Weboberfläche: `bearerAccess` in `tokens.go` lehnt jede Basic-Anmeldung außerhalb von `/dav` und `/mcp` ab. Dafür hätte man dort eine Ausnahme für `/buddy/` gebraucht.
