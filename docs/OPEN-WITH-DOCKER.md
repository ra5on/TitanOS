# „Öffnen mit“ für Docker-Apps (PLAN 72)

Seit 0.69 öffnet der Dateimanager Dateien auch in Docker-Apps, z. B. eine `.3mf`-Datei in Bambu Studio. Die App bekommt den Pfad der Datei in ihrem Container. Speichert man in der App, landet das direkt in der Originaldatei auf dem NAS. Entscheidungen des Projektinhabers vom 8.11.2026, Entwürfe in `Entwuerfe/oeffnen-docker-72`.

## Vorlage

Eine App gibt in ihrer Vorlage an, welche Dateien sie öffnet:

```yaml
x-titanos:
  name: Bambu Studio
  main: bambustudio
  opens:
    types: [3mf, stl, step, obj]
    command: [bambu-studio, "{file}"]
    user: abc            # optional: Name oder UID:GID, sonst der Benutzer des Abbilds
    env: [DISPLAY=:1]    # optional, höchstens 16 Einträge NAME=Wert
    service: bambustudio # optional, sonst x-titanos.main bzw. der einzige Dienst
```

- **`types`**: 1 bis 32 Endungen, klein geschrieben und ohne Punkt. Ein Punkt am Anfang und Großbuchstaben werden beim Lesen angeglichen.
- **`command`**: eine **Argumentliste, keine Shell-Zeile**.
  - Das erste Wort ist das Programm.
  - `{file}` steht genau einmal als eigenes Argument und wird durch den Pfad der Datei im Container ersetzt.
  - `sh -c "… {file}"` wird abgelehnt.
- **`hidden: true`** nimmt die App aus dem Menü („Im Öffnen-mit-Menü zeigen“ aus).
- Ist der Block ungültig, nutzt titand ihn nicht und zeigt den Fehler im Docker-Editor.

titand liest den Block einmal je App aus `/config/apps/<name>/compose.yaml` und hält ihn im RAM, bis sich die App ändert. Ein Rechtsklick liest also nicht vom Stick.

## Ablauf

1. **Menü:** Rechtsklick auf eine Datei, dann „Öffnen mit ›“.
   - Unter der Überschrift „Docker-Apps“ stehen die Apps, deren `types` passen und die das Konto benutzen darf (Rechte je App, `appaccess`).
   - Für die Liste fragt die Oberfläche `GET /api/v1/docker/openers?root=&path=`.
   - titand prüft dabei die Rechte des Kontos auf die Datei, wie beim Herunterladen: keine versteckten oder geheimen Namen, Links nur innerhalb der Freigabe.
   - titand vergleicht dann den Pfad mit den Bind-Mounts des Containers (Docker-API, nicht die Compose-Datei).
2. **Öffnen:** `POST /api/v1/docker/apps/{app}/open {root, path}`.
   - titand nimmt den Mount mit der längsten passenden Quelle und rechnet den Pfad im Container aus.
   - Dann startet es `command` per `docker exec`: ohne Terminal, ohne zu warten, mit `user` und `env`.
   - Eine gestoppte App wird erst gestartet. titand wartet bis zu 30 s auf „läuft“ und dann 3 s, damit die Oberfläche der App bereit ist.
   - Danach öffnet der Arbeitsplatz das Fenster der App.
3. **Nicht eingehängt:** Liegt die Datei in keinem Ordner der App, zeigt das Menü „<Freigabe> ist dort noch nicht eingehängt“.
   - Besitzer und Administratoren bekommen die Rückfrage „<Freigabe> in <App> einhängen?“:
     - Die App sieht danach die ganze Freigabe.
     - Der Container wird neu erstellt.
     - Wahlweise nur lesend.
     - Rückgängig unter Docker › Bearbeiten › Freigaben.
     - „Abbrechen“ hat den Fokus, Enter hängt nichts ein.
   - Danach wartet die Oberfläche, bis die Freigabe im Container sichtbar ist, höchstens 90 s, und öffnet dann die Datei.
   - Benutzer ohne Verwaltungsrecht sehen nur den Hinweis, dass ein Administrator die Freigabe einhängen muss.
   - Die Freigabe des KI-Agenten mit einem Ordner je Konto wird nie eingehängt.
4. **Standard-App:** „Immer mit … öffnen“ speichert die App je Endung in der Einstellung `fileappsdocker` des Kontos, z. B. `{"3mf":"docker:bambu"}`.
   - Ein Doppelklick öffnet dann direkt dort.
   - Wählt man für dieselbe Endung eine Web-App oder den Texteditor als Standard, wird der Docker-Eintrag entfernt, und umgekehrt.
   - Die Einstellung ist eine eigene Art neben `fileapps`. So liest und schreibt titand 0.66 nach einem Rückweg `fileapps` unverändert, denn es lehnt dort `docker:` ab.

## Einhängen

`POST /api/v1/docker/apps/{app}/shares {share, readOnly}` (Besitzer und Administratoren):
- Hängt eine Zeile `<Ordner der Freigabe>:/shares/<Freigabe>[:ro]` an `volumes` des Dienstes aus `opens` an.
- Die übrige Compose-Datei bleibt erhalten, Kommentare auch. Die YAML wird über `yaml.v3` umgeschrieben, Einrückung 2.
- Danach läuft der gewohnte Weg über `SetCompose`: Prüfung mit `docker compose config`, LAN- und Viewer-Netzregeln, Auftrag `up`.
- Ein Ziel, das schon belegt ist, wird abgelehnt (`already_mounted`).

`DELETE …/shares/{share}` nimmt die Zeile wieder heraus. Die Dateien in der Freigabe bleiben.

`GET`/`PUT /api/v1/docker/apps/{app}/opens` liest bzw. ersetzt den `opens`-Block, das ist die Karte „Dateien öffnen“ im Docker-Editor. Weil sich nur `x-titanos` ändert, erstellt `docker compose up` den Container dabei nicht neu.

Keine dieser Routen steht in `publicRoutes` oder im Werkzeugkatalog: kein Token, kein MCP, kein Agent.

## Grenzen

- Ob ein Programm eine zweite Datei im schon laufenden Fenster öffnet oder ein neues Fenster aufmacht, entscheidet das Programm selbst. Bei Bambu Studio (linuxserver-Abbild mit Selkies) ist das nicht auf einem echten Container geprüft.
- `user` und `env` müssen zur Desktop-Sitzung im Abbild passen (z. B. `abc`, `DISPLAY`/`WAYLAND_DISPLAY`). Die Vorlage muss die richtigen Werte kennen.
- Die App sieht die ganze eingehängte Freigabe, nicht nur die eine Datei. Sie läuft mit den Rechten ihres Containers, nicht mit denen des Kontos.
- Apps ohne grafisches Programm im Container, also reine Web-Oberflächen wie Immich oder Jellyfin, können so nichts öffnen.

## Prüfung

- **Go:**
  - `docker.TestParseOpener`, `TestOpenerValidateRefusesShellAndOddValues`, `TestContainerPath`, `TestEditVolumesKeepsCommentsAndRefusesDoubleTargets`, `TestOpenFileRunsTheProgramWithThePathInTheContainer` (nachgebaute Engine: Befehl, Benutzer, Umgebung, Detach, gestoppte App), `TestMountShareChangesTheComposeFileOnce`, `TestSetOpenerWritesTheTemplateBlock`
  - `api.TestDockerFileAppsPreferenceIsAKindOfItsOwn`, `TestDockerOpenRoutes` (Rechte, Pfadflucht, Engine aus), `TestDockerOpenRoutesAreNotPublic`
- **Web:** `tests/docker-opens.test.mjs` (Programmzeile ohne Shell, eingehängte Freigaben).
- **UI:** `scripts/test-ui-docker-open-with.mjs`
  - Menü mit Überschrift
  - Öffnen und Fenster der App
  - Rückfrage: Fokus auf „Abbrechen“, Enter tut nichts, nur lesend
  - Benutzer ohne Recht
  - Standard-App und Doppelklick
  - Handy
  - Docker-Editor: Typen, Shell-Zeile abgelehnt, Freigabe entfernen
- **Nicht geprüft:** echte Docker-Engine mit einem echten Desktop-Container (Bambu Studio), Rechte der gespeicherten Dateien im Container.
