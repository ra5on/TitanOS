# Öffentliche API und Kommandozeile

TitanOS 0.23.0 stellt einen ausdrücklich begrenzten Vertrag unter `/api/v1`
bereit. Die maschinenlesbare OpenAPI-3.1-Datei liegt nach Anmeldung unter
`GET /api/v1/openapi.json`. Die übrigen internen WebUI-Routen sind damit nicht
für externe Anwendungen freigegeben.

## Persönlichen Zugang anlegen

Unter **Einstellungen › API-Zugänge** erstellt jedes Konto eigene benannte
Tokens. Standard ist ausschließlich `system:read`; Schreibrechte müssen
bewusst gewählt werden. Das vollständige Geheimnis erscheint einmal. Ein
verlorenes Token wird widerrufen und ersetzt. Höchstens 32 Tokens je Konto.

Der Server speichert nur einen SHA-256-Hash des zufälligen Geheimnisses in
`/config/users/api-tokens.json`. Berechtigungsprüfung erfolgt bei jedem Aufruf:
aktuelles Konto, Rolle und Rechte auf die ausgewählte Dateiwurzel. Löschen und
Neuanlegen eines gleichnamigen Kontos oder ein Passwortwechsel machen alte
Tokens ungültig. Ein Token erweitert keine Kontorechte. Widerruf gilt sofort.
„Zuletzt verwendet“ liegt ausschließlich im RAM und beginnt nach Dienstneustart
neu; API-Aufrufe schreiben nicht regelmäßig auf den USB-Stick.

| Bereich | Freigegebene Aufrufe |
|---|---|
| `system:read` | System, Speicherstatus, Netzwerkstatus, Plugins, OpenAPI |
| `files:read` | eigene Shares/Dateiwurzeln, Liste, Download/Range, Eigenschaften, Text und eigene Kopieraufträge |
| `files:write` | Ordner/kleine Textdatei anlegen, Kopieren/Verschieben, Umbenennen, Löschen und versionierter Texteditor |
| `docker:read` | Docker-Status und Containerprotokolle |
| `docker:write` | Start/Stop/Neustart; LAN-Zuweisung eines verwalteten App-Dienstes |
| `vms:read` | VM-Status |
| `vms:write` | Start, sauberes Herunterfahren, Neustart |

Docker und VM benötigen weiterhin Besitzer-/Administratorrechte. Netzwerk-
und Pluginstatus ebenfalls. Dateioperationen beachten aktuelle Share-Rechte;
Administratoren sehen zusätzlich ihre erlaubten erweiterten Dateiwurzeln.
Ein `*:write`-Bereich schließt `*:read` nicht automatisch ein.

Tokens öffnen weder Konten/Geheimnisse, Sitzungen oder Shells noch Updates,
Netzwerkbestätigung, Assistent oder beliebige neue API-Routen. Verwaltung der
Tokens selbst erfordert eine normale angemeldete Sitzung. Eine fehlerhafte
Authorization-Kopfzeile fällt nicht auf Browser-Cookies zurück.

## titanosctl

`make build` baut zusätzlich `backend/bin/titanosctl`. Das statische
Linux-amd64-Binary liegt im Systemabbild unter `/usr/bin/titanosctl` und als
`titanos-<version>-cli-linux-amd64` im Release. Seine Prüfsumme gehört zur
signierten `SHA256SUMS`. Nach einem App-Update kann die CLI im Basissystem noch
dessen Version tragen; das aktuelle separate Release-Binary lässt sich verwenden.
Andere Architekturen werden hier nicht als fertige Downloads angeboten.

Token in einer privaten Datei mit Modus `0600` speichern; alternativ die
Umgebungsvariable `TITANOS_TOKEN` setzen. Keine Geheimnisse in Argumenten,
Repositorydateien oder Shell-Historie ablegen. Das eigene NAS-Zertifikat bzw.
dessen CA über einen vertrauenswürdigen Weg beziehen und prüfen.

```sh
chmod 600 "$HOME/.config/titanos-token"
export TITANOS_URL=https://nas.example.test

titanosctl --token-file "$HOME/.config/titanos-token" --ca-file nas-cert.pem status
titanosctl --token-file "$HOME/.config/titanos-token" --ca-file nas-cert.pem files share/Dokumente Projekte
titanosctl --token-file "$HOME/.config/titanos-token" --ca-file nas-cert.pem docker
titanosctl --token-file "$HOME/.config/titanos-token" --ca-file nas-cert.pem vm
titanosctl --token-file "$HOME/.config/titanos-token" --ca-file nas-cert.pem openapi
```

Bei öffentlich vertrauten Zertifikaten ist `--ca-file` unnötig. HTTPS ist
Pflicht, außer für lokale Entwicklung auf Loopback. Weiterleitungen werden
nicht verfolgt. Die CLI prüft Fehlercodes, begrenzt JSON-Ausgaben auf 32 MiB
und streamt Dateiantworten; Strg+C beendet den aktuellen Aufruf.

Beliebige **freigegebene** Aufrufe sind über `request METHOD /pfad [JSON-DATEI]`
erreichbar. Beispiel einer JSON-Datei zum Anlegen eines Ordners:

```json
{"root":"share/Dokumente","path":"Projekte/Neu"}
```

```sh
titanosctl --token-file "$HOME/.config/titanos-token" --ca-file nas-cert.pem request POST /files/mkdir mkdir.json
```

Wurzeln zunächst mit `request GET /files/roots` entdecken. Pfade sind relativ
zur jeweiligen Wurzel, keine Hostpfade. `202 Accepted` bestätigt den Start eines
Auftrags; dessen Endzustand muss anschließend abgefragt werden. Versionierte
Textänderungen brauchen die durch `GET /files/text` gelieferte `version`.
