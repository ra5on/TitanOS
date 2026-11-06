# PLAN 54-G: Gemeinschaft und Familie

Drei Funktionen des KI-Agenten: Backup-Buddy, Skill-Katalog und Familien-Tagebuch. Jede ist ab Werk aus. Eingeschaltet werden sie unter Einstellungen › KI-Agent › Funktionen (Besitzer und Administratoren). Die Werte nach dem Einschalten folgen §54-G und lassen sich alle ändern (§54-E).

## Backup-Buddy

**Bedienung.** Unter Funktionen › Backup-Buddy trägt man die Adresse dieses NAS ein, so wie der Partner es erreicht, z. B. `nas-eva.tail1234.ts.net`. Dazu kommen das Volume für die Sicherungen der Partner, der Zeitplan (ab Werk wöchentlich, Sonntag 02:00), die Probe-Wiederherstellung (ab Werk monatlich, 50 Stichproben), die Schutzfrist (ab Werk 30 Tage) und der Port (ab Werk 8752). „Backup-Buddy öffnen“ zeigt die Ansicht `buddy` wie im Entwurf 11:

- jeden Partner mit letzter Sicherung, Platz beim Partner und Probe-Wiederherstellung;
- was der Partner hier belegt;
- „Neues TitanOS koppeln“.

**Koppeln.** NAS A legt fest, wie viel Platz es dem Partner gibt und welche eigenen Freigaben es beim Partner sichert. Dann zeigt es einen Code wie `K7Q-4MZ-92X`. Der Code gilt 10 Minuten und nur einmal; nach 5 falschen Versuchen verfällt er. Auf NAS B gibt man die Adresse von A und den Code ein und legt ebenfalls Platz und Freigaben fest. B schickt A in einer Anfrage seinen Namen, seine Adresse, den Namen, den es A gibt, und das Token, mit dem A sich bei B anmeldet. A antwortet mit dem Gleichen in der anderen Richtung. Jede Seite speichert nur den SHA-256 des Tokens, das sie selbst ausgegeben hat. Das Token für den Partner liegt nur in `/config` und im Sicherungsauftrag.

**Senden.** Für jeden Partner mit gewählten Freigaben gibt es einen gewöhnlichen Sicherungsauftrag `buddy-<partner>` der Art `rest`. Er steht auch auf der Seite Sicherungen, mit Wiederherstellen und Prüfen. Lässt er sich nicht speichern, zeigt die Ansicht den Grund beim Partner. Geschrieben wird der Auftrag nur, wenn sich Freigaben, Zeitplan oder Ziel wirklich ändern; ein Start von titand schreibt nichts auf den Stick. restic verschlüsselt mit dem Passwort des Auftrags; das Passwort verlässt dieses NAS nie. Das Token geht als `RESTIC_REST_PASSWORD` in die Umgebung von restic, nie in die Adresse oder die Kommandozeile. Behalten werden die Stände der letzten 31 Tage, dazu 8 wöchentliche und 12 monatliche. So soll nie etwas jünger als die Schutzfrist gelöscht werden. Stellt ein Partner eine Schutzfrist über 31 Tage ein, meldet `forget` beim Absender Warnungen (403), die Sicherung selbst klappt. Die monatliche Probe startet der Agent direkt nach einer gelungenen Sicherung (höchstens eine Stunde danach), weil die Platten dann wach sind. Danach wartet sie 30 Tage.

**Empfangen (Paket `backend/internal/buddy`).** Ein kleiner Server spricht das REST-Protokoll von restic (v1 und v2): `POST /?create=true`, `config`, Listen, `HEAD/GET` mit Range, `POST`, `DELETE`. Er lauscht nur auf den Tailscale-Adressen dieses NAS (100.64.0.0/10, fd7a:115c:a1e0::/48), auf dem eigenen Port, ohne TLS; die Verbindung verschlüsselt WireGuard. Im Entwicklungsmodus lauscht er nicht. Ist der Agent oder der Backup-Buddy aus, beantwortet er nichts (404); der Zeitplan der Aufträge läuft als gewöhnliche Sicherung weiter, die monatliche Probe startet nur bei eingeschaltetem Agenten. Pfade:

- `/v1/pair`: die Kopplungsanfrage, nur mit gültigem Code;
- `/v1/<partner>/…`: Anmeldung per Basic mit Partnername und Token. Ein falsches Token, ein fehlendes und der Ordner eines anderen Partners bekommen dieselbe 401.

Abgelegt wird unter `.titanos/buddy/<partner>/` auf dem gewählten Volume (root, 0700, über `volumedata.Open`). Ein nicht eingebundenes Volume wird abgelehnt (503); nichts landet im RAM-Overlay.

**Löschregel (nur anhängen).** Eine vorhandene Datei wird nie ersetzt. `config` und `keys` werden nie gelöscht, `locks` immer, denn restic entfernt nach jedem Lauf seine Sperre. `data`, `index` und `snapshots` dürfen erst gelöscht werden, wenn die Datei älter als die Schutzfrist dieses NAS ist; gemessen wird an der Zeit, zu der sie hier ankam. So räumt der Besitzer der Daten mit `restic forget --prune` alte Stände ab. Ein übernommener Absender kann die jüngsten Sicherungen aber nicht vernichten. restic meldet eine abgelehnte Löschung (403) als Warnung; der Auftrag zeigt sie an.

**Kontingent.** Jeder Partner hat sein Kontingent. Ein Hochladen darüber hinaus wird vor dem Ablegen mit 507 abgelehnt, mit und ohne `Content-Length`. Sperrdateien gehen auch bei vollem Kontingent hinein, damit restic aufräumen kann. Der Verbrauch wird beim ersten Zugriff des Partners nach dem Start von titand einmal gezählt und danach im RAM fortgeschrieben; die Seite liest dafür keine Platte. Bis dahin zeigt sie nur das Kontingent.

**Daten.** Partner mit Tokens: `/config/system/agent-buddy.json`, geschrieben nur beim Koppeln, Ändern und Lösen. Einstellungen: `agent.json` › `features.buddy`. Ein `PUT /agent/features` kann die Partner nicht ändern. Routen (nur Besitzer/Admin, nur Web-Sitzung, nicht in `publicRoutes`):

- `GET /agent/buddy`
- `POST`/`DELETE /agent/buddy/code`
- `POST /agent/buddy/pair`
- `PUT`/`DELETE /agent/buddy/partners/{id}` (`?data=1` löscht auch die Sicherungen des Partners hier)

## Skill-Katalog

**Bedienung.** Der Besitzer trägt die https-Adresse des Katalogs und den öffentlichen ed25519-Schlüssel ein (32 Byte, base64). Ab Werk ist beides leer; einen Katalog oder Schlüssel liefert TitanOS nicht mit. „Katalog öffnen“ zeigt die Ansicht `catalog` wie im Entwurf 12. Jedes Konto des Agenten kann sie öffnen, wenn die Funktion an ist. Tippt man auf einen Skill, zeigt der Agent Name, Beschreibung, wann der Skill gilt, alle Schritte und die Werkzeuge, die er nutzt. Erst danach kommt „Installieren“. Unter „Eigene Skills teilen“ lädt man einen eigenen Skill als Einreichungspaket herunter (`titanos-skill-submission`, JSON mit Name, Wann, Schritten und erkannten Werkzeugen, ohne Kontonamen). Eingereicht wird beim Betreiber des Katalogs.

**Format.** Die Indexdatei (`{"version":1,"skills":[{id,name,description,when,steps,tools,author,version,installs}]}`) liegt an der Adresse. Die abgetrennte Signatur liegt unter derselben Adresse mit `.sig`: base64 der 64 Signaturbytes über die genauen Bytes des Index. Geprüft wird die Signatur vor jedem Lesen. Ohne Schlüssel, ohne Signatur oder mit falscher Signatur zeigt der Agent nichts und installiert nichts. Ein verändertes Eintragsbyte bricht die Signatur.

**Installieren.** Der Server lädt und prüft den Katalog erneut. Er nimmt den Eintrag nur, wenn er genau dem gezeigten entspricht (SHA-256 des Eintrags); sonst kommt 409. Abgelehnt wird auch:

- ein Werkzeug, das das Konto nicht hat; gezählt werden die genannten und die in den Schritten vorkommenden Namen wie `files_search`;
- ein gleichnamiger eigener Skill.

Gespeichert wird über `skill_save` mit dessen Regeln (keine Geheimnisse, Länge). Der Skill gilt danach als fremder Inhalt (`untrusted`) und trägt in der Beschreibung `Skill-Katalog: <id>`. Ein Skill ändert selbst nichts. Werkzeuge, die etwas ändern, fragen wie immer.

Routen (jedes Konto des Agenten, nur Web-Sitzung): `GET /agent/catalog`, `POST /agent/catalog/install`, `GET /agent/catalog/export/{id}`.

## Familien-Tagebuch

**Bedienung.** Besitzer oder Administratoren schalten die Funktion für den Agenten ein. Jedes Konto legt unter Funktionen › Familien-Tagebuch seine eigenen Quellen fest:

- Chats mit dem Agenten, Erinnerungen und Home Assistant (alle drei ab Werk an);
- einen Fotos-Ordner aus einer eigenen Freigabe;
- Immich (Adresse und API-Schlüssel);
- Frigate (Adresse).

Im Chat beantwortet der Agent Fragen wie „Was haben wir letztes Wochenende gemacht?“ mit dem Werkzeug `diary` (lesend, fremder Inhalt). Es nimmt einen Zeitraum von höchstens 31 Tagen. „Wer war heute an der Tür?“ (`focus: door`) beantwortet nur Frigate. Ohne Frigate sagt der Agent das.

**Quellen und Rechte.**

- Chats und Erinnerungen: nur die eigenen des Kontos.
- Home Assistant: nur für Besitzer und Administratoren, wie bei den anderen `ha_*`-Werkzeugen. Nur Geräte, die für den Agenten freigegeben sind, und nur aus den Bereichen Personen, Alarmanlage, Schlösser, Türsensoren, Abdeckungen und Geräteortung. Gelesen wird der Verlauf (`/api/history`) von höchstens 12 Geräten über höchstens 7 Tage. Eine eigene Logbuch-Abfrage gibt es nicht, weil das Paket `homeassistant` keine anbietet.
- Fotos-Ordner: Beim Speichern wird geprüft, ob das Konto die Freigabe öffnen darf; ein fremder Ordner wird abgelehnt. Beim Lesen wird erneut mit den Rechten von jetzt geprüft. Gezählt werden Aufnahmedaten aus EXIF (JPEG) oder sonst die Änderungszeit, höchstens 20 000 Dateien, nur Dateien, die seit Beginn des Zeitraums geschrieben wurden. Schläft die Platte, wird der Ordner nur gelesen, wenn das Konto unter Eigeninitiative „Platten wecken“ erlaubt und der Chat kein Hintergrund-Chat ist. Ein lesendes Werkzeug kann nicht nachfragen; „fragen“ gilt deshalb als „nicht wecken“. Ordner, die für den Agenten gesperrt sind (Rechte › Ordner), bleiben zu, auch unterhalb des gewählten Ordners.
- Immich: `POST /api/search/metadata` mit dem eigenen Schlüssel (`x-api-key`), höchstens 1 000 Aufnahmen. Je Tag werden Anzahl, Orte und Personen gezählt.
- Frigate: `GET /api/events` im Zeitraum, höchstens 200. Personen erscheinen mit Uhrzeit, andere Ereignisse gezählt je Kamera.

Der Immich-Schlüssel ist ein Geheimnis. Er kommt nie zur Seite zurück, nur `immichKeySet`; ein Speichern ohne Wert behält ihn, „Gespeicherten Schlüssel entfernen“ löscht ihn. Er steht in `features.json` im privaten Agent-Ordner des Kontos und nie in einem Werkzeugergebnis.

**Quellen unter der Antwort.** Das Ergebnis von `diary` trägt die Quellen als `sources` des Schritts, z. B. `{"title":"Fotos (48)","url":"titanos:diary/photos"}`, und nennt sie auch im Text. Die Komponente `FamilyDiarySources.svelte` zeigt sie als Zeile unter der Antwort (Entwurf 13). Sie muss dafür in `ChatView.svelte` eingebunden werden (siehe unten); bis dahin stehen die Quellen nur im Schritt und in der Antwort des Modells.

## Geprüft

- `go test ./internal/buddy`: Protokoll wie restic es schickt (anlegen, config, hochladen, Listen v1/v2, Range), Anmeldung (falsches/fehlendes Token, fremder Ordner), Kontingent (mit und ohne Länge, Sperre trotz vollem Kontingent), nur anhängen (kein Überschreiben, Löschregel mit Uhr), Volume weg, Ablauf und Raten des Codes, Kopplung hin und zurück, Adressen. **Mit dem echten restic dieses Rechners** (0.18): `init`, `backup`, `check --read-data`, `restore` gegen den Server; die Empfangsseite enthält keinen Klartext, und `forget` eines jungen Stands wird mit 403 abgelehnt.
- `go test ./internal/api -run Family54`:
  - ab Werk aus;
  - Prüfung der Einstellungen;
  - Benutzer sehen den Buddy nicht;
  - Kopplung zweier Test-Server mit Code (falsch, einmalig, abgelaufen);
  - Sicherungsauftrag auf beiden Seiten (Art `rest`, Adresse ohne Token, Benutzername, Aufbewahrung), folgt täglich/wöchentlich/monatlich, verschwindet ohne Freigaben;
  - Hashes passen zu den Tokens, kein Token in den Ansichten, Partner bleibt bei `PUT /agent/features`, Lösen sperrt;
  - Katalog: gültig, Signatur fehlt, falsche Signatur, anderer Schlüssel, veränderter Eintrag, unbekanntes Werkzeug, geänderter Eintrag (Digest), Installation als fremder Inhalt, Export;
  - Tagebuch: Schlüssel nicht in der Ansicht und behalten, Ordner in einer Freigabe, die das Konto nicht öffnen darf, abgelehnt, Werkzeug mit Fakes für Immich, Frigate und Home Assistant (nur freigegebene Geräte), Tür nur mit Frigate, Benutzer ohne Home Assistant und ohne fremde Quellen, Zeitraum geprüft.
- Web: `tests/agent54-family.test.mjs`; `scripts/test-ui-agent54-family.mjs` mit gemockter API bei 390×844 und 1440×900. Bilder liegen in `out/validation/v057-family/`.

## Grenzen, nicht geprüft

- **Zwei echte NAS** und ein Lauf über echtes Tailscale sind nicht getestet. Geprüft sind restic gegen den Server im selben Prozess und zwei Test-Server mit Kopplung über HTTP.
- Der Buddy-Server lauscht nur über Tailscale. Andere Wege (WireGuard, NetBird, Heimnetz) gibt es nicht, weil ohne TLS sonst das Token im Klartext liefe.
- „Ich sichere bei …“ zeigt die Datenmenge des letzten Laufs, nicht den belegten Platz beim Partner (dedupliziert und komprimiert meist weniger).
- `prune` kann junge, nicht mehr gebrauchte Pakete (abgebrochene Läufe) nicht löschen; sie bleiben bis zur Schutzfrist und zählen zum Kontingent.
- Ein Katalog, ein Hosting und ein Schlüssel sind nicht festgelegt; das entscheidet der Inhaber. Getestet wurde nur mit einem Testschlüssel.
- Die Werkzeugerkennung in den Schritten zählt Wörter wie `files_search`; ein Wort dieser Form, das kein Werkzeug ist, verhindert die Installation.
- Das Tagebuch fragt keinen Kalender ab (es gibt keinen Kalender in TitanOS). Die Fotos-Seite gibt es nicht mehr; deshalb Immich oder ein Ordner. EXIF von HEIC wird nicht gelesen, dort zählt die Änderungszeit.
- Echte Immich-, Frigate- und Home-Assistant-Server sind nicht getestet, nur ihre API-Formen als Fakes.
- Der Fotos-Ordner selbst (EXIF lesen, gesperrte Unterordner, schlafende Platte) hat keinen Go-Test: Der Testserver hat keine Freigabe mit Dateien. Die EXIF-Auswertung ist die vorhandene aus `thumbs`.
- Die Quellen unter der Antwort und die Menüeinträge brauchen Änderungen an gemeinsamen Dateien (`family-shared-files.patch`). Mit dem Patch geprüft: `svelte-check` ohne Fehler und ein Bild des Chats (`phone-09-…`); danach wurde der Patch wieder entfernt.

## Nötige Änderungen an gemeinsamen Dateien

Genau als Patch in `docs/agent54/family-shared-files.patch`:

- `web/src/lib/components/agent/ChatView.svelte`: `FamilyDiarySources` unter der Antwort.
- `AgentMenu.svelte` und `routes/agent/+page.svelte`: Menüeinträge „Backup-Buddy“ (nur Besitzer/Admin) und „Skill-Katalog“, wenn die Funktion an ist.

Dazu, ohne Patch:

- `backend/internal/firewall/services.go`: Dienst `{ID: "buddy", Ports: []PortSet{{"tcp", []string{"8752"}}}, Default: local}` neben `backuptarget`, mit Text dazu in `docs/FIREWALL.md`.
- `backend/internal/api/ipv64_public.go` (`reservedPort`): 8752 zu `22, 80, 443, 8750`.
- `docs/rules/API.md`, Abschnitt KI-Agent: die neuen Routen und dass der Buddy-Server ein eigener Zugang außerhalb von Sitzung und `publicRoutes` ist (eigener Port nur auf Tailscale, Basic mit Partner-Token, nur der eigene Ordner, `/v1/pair` nur mit gültigem Code).
- `docs/rules/BACKUPS.md`: Verweis auf den Backup-Buddy neben „Anderes TitanOS“.

Nicht genommen wurde der Weg über den Port der Weboberfläche: `bearerAccess` in `tokens.go` lehnt jede Basic-Anmeldung außerhalb von `/dav` und `/mcp` ab. Dafür hätte man dort eine Ausnahme für `/buddy/` gebraucht.
