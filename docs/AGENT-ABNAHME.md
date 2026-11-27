# Persönlichen Agenten prüfen

Die Beispiele laufen auf einem Entwicklungsrechner mit Wegwerfkonten. Für den Browser wird Playwright 1.56.1 mit Chromium und WebKit benötigt. Go und Node sind in `scripts/dev-toolchains.lock.json` festgelegt. Alle Befehle starten im Wurzelverzeichnis des Repositorys.

## Ein vollständiger Auftrag bei geschlossenem Browser

```sh
make build
export TITAND_BIN="$PWD/backend/bin/titand"
export PLAYWRIGHT_MODULE="$PWD/.test-tools/node_modules/playwright/index.mjs"
node scripts/test-ui-personal-agent-live.mjs
```

`PLAYWRIGHT_MODULE` muss auf das installierte Playwright-Modul zeigen. Um die CI-Version separat bereitzustellen, genügen `npm install --prefix .test-tools --no-save --ignore-scripts playwright@1.56.1` und `.test-tools/node_modules/.bin/playwright install --with-deps chromium webkit`. Die Tests laufen als gewöhnlicher Benutzer.

Der Live-Test startet das gebaute `titand -dev` auf einem freien Loopback-Port mit einem temporären Konfigurationsverzeichnis. Ein Testkonto öffnet Agent über das Arbeitsplatzmenü und startet den Beispielauftrag bei pausierter Warteschlange. Dann schließt der Test den gesamten Browser und setzt die Warteschlange über die angemeldete API-Sitzung desselben Kontos fort. Der Worker berechnet **2 + 3 = 5**. Ein frisch gestarteter Browser öffnet Agent wieder über das Menü und liest das Ergebnis in der Auftragszentrale. Der Test prüft den protokollierten Zustandswechsel, genau einen Auftrag für die Request-ID und den Status 404 beim Direktzugriff des zweiten Kontos.

Das ist ein Durchlauf mit echtem Backend und eigenem Worker-Prozess im Entwicklungsmodus. Die Benutzerkennung und die systemd-Härtung des installierten Diensts werden im Gast mit `scripts/smoke-storage.py` geprüft. Die Umstellung von einem älteren Systemstand gehört zum Update-Gasttest `scripts/smoke-system-update.py`.

## Wiederkehrende Rechnungen

`go -C backend test -run '^TestPersonalRecurringInvoiceCreatesOneWorksheetPerMail$' ./internal/api` stellt zwei Rechnungen desselben Absenders im lokalen IMAP-Testpostfach zu. Die Regel verwendet für beide den Basisnamen `Rechnung.xlsx`. Der Test prüft die konkreten Namen in beiden Freigaben, die Zellinhalte beider Dateien und eine bereits vorhandene Datei. Anschließend lädt er den gespeicherten Agent-Zustand neu und gibt dieselben IMAP-Nachrichten nochmals zur Prüfung frei. Die zwei erledigten Aufträge und die Dateien bleiben erhalten.

## Ansichten und native Einstiege vergleichen

Für den Vergleich kann der gebaute Entwicklungsdienst mit eigener temporärer Konfiguration auf Loopback laufen. In einem zweiten Terminal setzen die drei Skripte an der Oberfläche an. Sie liefern definierte API-Antworten für synthetische Benutzer, damit auch Freigaben, Ergebnisse und seltene Zwischenzustände reproduzierbar sind.

```sh
config=$(mktemp -d)
backend/bin/titand -dev -config "$config" -dev-listen 127.0.0.1:5393
```

```sh
export UI_URL=http://127.0.0.1:5393
export UI_OUTPUT=$(mktemp -d)
export UI_SHOTS="$UI_OUTPUT"
export PLAYWRIGHT_MODULE="$PWD/.test-tools/node_modules/playwright/index.mjs"
for engine in chromium webkit; do
  UI_ENGINE="$engine" node scripts/test-ui-personal-agent.mjs || exit 1
  UI_ENGINE="$engine" node scripts/test-ui-personal-agent-pim.mjs || exit 1
  UI_ENGINE="$engine" node scripts/test-ui-personal-voice.mjs || exit 1
done
```

### Bereichsauswahl aus installierten Office-Editoren

`TestOfficeEditorSelectionLive` erstellt ein Wegwerfkonto, die Freigabe „Docs“ und vier Beispieldateien. Der Browser öffnet jede Datei aus der Dateien-App, markiert im Editor einen Bereich und übergibt ihn an den Agenten. Der Test vergleicht die Ergebnisquellen und den gelesenen Inhalt und lädt anschließend die Originaldatei zum Bytevergleich wieder herunter. `UI_OFFICE_PACKAGES` verweist auf die vier zum eingebauten Katalog passenden Paketarchive. TitanOS prüft deren Größen und SHA-256-Prüfsummen bei der Installation.

```sh
make build
export TITAND_BIN="$PWD/backend/bin/titand"
export PLAYWRIGHT_MODULE="$PWD/.test-tools/node_modules/playwright/index.mjs"
export TITANOS_OFFICE_BROWSER=1
# UI_OFFICE_PACKAGES auf das lokale Verzeichnis mit den geprüften Archiven setzen.
for engine in chromium webkit; do
  UI_ENGINE="$engine" go -C backend test -count=1 -tags embedui ./internal/api -run '^TestOfficeEditorSelectionLive$' -v || exit 1
done
```

Der Standardlauf erwartet eine Auswahlantwort des Editors. Mit `UI_OFFICE_APP=t-exel` lässt sich ein einzelner installierter Editor prüfen. Ohne diese Angabe durchläuft der Test alle vier Programme. Für die älteren 1.7.0-Pakete prüft `UI_OFFICE_EXPECT_MANUAL=1` denselben Weg mit der manuellen Bereichsangabe. `UI_OUTPUT` speichert Bilder der Auswahl, der Übergabe und des bestätigten Ergebnisses.

Mit `UI_OFFICE_COPY=1` öffnet derselbe Test die neuen Word- und Tabelleneditoren. Das Testkonto erhält das persönliche Recht „Nachfragen“ zum Erstellen einer Kopie. Der Test vergleicht den markierten Abschnitt mit der sichtbaren Vorher/Nachher-Vorschau, prüft den Zielordner vor der Freigabe und öffnet die gespeicherte Kopie im zugehörigen Editor. Er prüft außerdem die einmalige Hervorhebung beim ersten Ergebnisbesuch, den Inhalt der fertigen Kopien und die unveränderten Originale.

| Einstieg | Prüfschritte |
|---|---|
| Arbeitsplatz und Agent | Arbeitsplatzmenü öffnen, neuen Chat starten, per Programmmenü und Tastatur zwischen Chats, Aufträgen und Einstellungen wechseln. |
| Chat auf Handy und Desktop | Leeren Chat, laufenden Auftrag, Antwortentwurf, konkrete Freigabe, bestätigtes Ergebnis und Auftragsliste abbilden. Auf 390 und 1440 CSS-Pixeln prüfen. Bei 760 Pixeln öffnen sich die Details als Overlay. |
| Sichtbare Rechte und Darstellung | In der Administration das Senderecht auf „Nie“ setzen. Helles Design und ausgeschaltete Bewegung speichern, Agent erneut öffnen und den gespeicherten Zustand prüfen. |
| Dateien und Office | Aus Dateien „Mit Agent bearbeiten“ auslösen. Im T-Word-Host Absatz 3 wählen und dieselbe Auswahl im Agent prüfen. Für zwei PDFs und einen Zielordner die erlaubten Sammelwerkzeuge prüfen. |
| T-Mail | Das Panel an einer Nachricht öffnen, explizite Quellen auswählen, den Entwurf im nativen Editor ändern und die zugehörige Freigabe erneut aufrufen. |
| Sprache | Synthetische Mikrofon- und Sprachdienstantworten durch die Bedienoberfläche schicken. Chat-Zuordnung, Stopp, Diktat, gesprochene Antwort und fehlende Rechte prüfen. Mit aktiver Bewegung die Ankunft der Punkte im Zentrum der Sprachkugel bei geöffneter Ergebnisvorschau messen. |

Für Vorherbilder akzeptiert das erste Skript zusätzlich `UI_BASELINE_URL` mit der Loopback-Adresse einer separat gestarteten früheren Weboberfläche. `UI_OUTPUT` und `UI_SHOTS` erhalten ausschließlich Bilder der anonymen Testfälle. Der Test mit simuliertem Editor prüft den TitanOS-Dialog bei geänderter Auswahl, verspäteter Antwort und unterbrochener Übergabe. An den veröffentlichten Office-Paketen 1.7.0 prüft der Produktlauf die manuelle Bereichsangabe. Für Pakete mit nativer Bereichsauswahl markiert derselbe Produktlauf die Absätze, Zellen, Seiten und Folien direkt im geöffneten Editor. Die mobile WebKit-Prüfung läuft unter Linux. Gerätemikrofon und Safari auf einem physischen iPhone brauchen eine eigene Geräteprüfung.

### Tatsächliche Versandvorschau eines mehrstufigen Auftrags

Nach `make web` und mit gesetztem `PLAYWRIGHT_MODULE` prüft der folgende Test den Ablauf Liefertermin → Kalender → Antwortentwurf → Versand. Das Modell liefert Testtext. IMAP und SMTP laufen nur auf Loopback. Die Prüfung ändert Empfänger, Kopien, Betreff, Anhang und Text des Entwurfs. Der Browser öffnet denselben Auftrag im Agenten und im T-Mail-Panel, vergleicht beide Vorschauen und gibt die aktuelle Fassung einmal frei. Der Backend-Test vergleicht die angenommene Nachricht mit der Vorschau und weist die Freigabe der alten Fassung zurück.

```sh
for engine in chromium webkit; do
  TITANOS_AGENT_APPROVAL_BROWSER=1 UI_ENGINE="$engine" go -C backend test -tags embedui -count=1 -run '^TestPersonalDeliveryMailCalendarDraftAndApprovedSend$' ./internal/api || exit 1
done
```

### Lokales Chatmodell und Kontowechsel

`TestPersonalModelReadinessThroughAccountSettings` prüft mit einem lokalen Ollama-Testserver die automatische Auswahl, fehlende Modelle, nicht erreichbaren Dienst, reine Embedding-Modelle, Cloud-Aliasse und entzogene Rechte. Für die Browserprüfung nach `make web` setzt man `TITANOS_MODEL_BROWSER=1`, `UI_ENGINE=chromium` oder `webkit` und führt `go -C backend test -tags embedui -count=1 -run '^TestPersonalModelReadinessThroughAccountSettings$' ./internal/api` aus. Der Test öffnet den Agenten, richtet das lokale Modell über die Einstellungen ein und wechselt im selben Browser zu einem zweiten Testkonto mit einer eigenen Modellwahl.

### Werkzeugwechsel und Pausen

`UI_AGENT_STEPS_ONLY=1 UI_ENGINE=webkit UI_WIDTH=390 node scripts/test-ui-personal-agent.mjs` prüft die mobile Unterhaltung am oben gestarteten Testdienst. Mit `UI_ENGINE=chromium UI_WIDTH=1440` läuft derselbe Fall im Desktopfenster. Der Test meldet nacheinander zwei laufende Schritte desselben Auftrags, wechselt von der Mail zum Kalender, pausiert und öffnet den Chat über den Aufträge-Tab erneut. Er vergleicht die ruhende Punktposition und prüft die Fortsetzung mit dem aktuellen Werkzeug. Bei aktivierter Bewegungsreduktion bleibt der Werkzeugwechsel bedienbar und die Punkte stehen still.

## Release-Prüfungen

Die Schritte und Werkzeugversionen der Build-CI stehen in `.github/workflows/build.yml`. Der Veröffentlichungsworkflow prüft die erzeugten Release-Dateien. Lokal lassen sich die folgenden Prüfungen wiederholen.

| Prüfung | Befehl beziehungsweise Einstieg |
|---|---|
| Go, Node, Komponenten, Svelte und Formatierung | `make test` sowie `gofmt -l backend` mit leerer Ausgabe |
| Regressionen der Release-Werkzeuge | `make test-release` |
| Nebenläufigkeit | Im Verzeichnis `backend` den Befehl `go test -race ./internal/api/ ./internal/tmail/ ./internal/immich/ ./internal/webpush/ ./internal/vm/` ausführen |
| Produktbuild | `make build` erzeugt `backend/bin/titand` einschließlich Weboberfläche und `backend/bin/titanosctl` |
| Statische Prüfungen | `make lint` führt Staticcheck, Errcheck, Govulncheck, ShellCheck und Actionlint aus |
| Realer Seitendurchlauf | Den Schritt „Crawl every page of titand -dev“ aus `build.yml` mit Wegwerfkonfiguration und zufälligem Kennwort ausführen. Der Test öffnet die im Skript aufgeführten Ansichten einzeln, betätigt sichtbare Schaltflächen und schreibt Befunde nach `UI_OUTPUT/findings.json`. |
| Bridge und DHCP | `scripts/test-bridge-forwarding.py` in einem eigenen Netzwerknamespace starten. Der Dateikopf enthält den unprivilegierten Linux-Aufruf. |

Backend- und Berechtigungsfälle sind in [Agent-Betrieb](AGENT-PERSOENLICH.md) und [Mail und Kalender](AGENT-PIM.md) beschrieben. Der T-Mail-Browsertest und der lokale SMTP-/IMAP-Testdienst verwenden Testadressen. Für einen gezielten Backend-Lauf im Verzeichnis `backend` prüft `go test -count=1 ./internal/api -run TestPersonal` die persönlichen Aufträge, den Worker-Socket, die Nutzertrennung, Objektfreigaben und den Rechteentzug.
