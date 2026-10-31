# MCP-Server für KI-Clients

Seit der Entscheidung des Projektinhabers vom 29.10.2026 hat TitanOS einen
MCP-Server: Externe KI-Clients wie Claude Desktop, Claude Code oder andere
Programme mit dem Model Context Protocol arbeiten damit am NAS – mit genau den
Rechten eines persönlichen API-Tokens und des Kontos dahinter. Seit der
Entscheidung des Projektinhabers vom 31.10.2026 ist MCP der einzige Weg für
KI-Aktionen am NAS: Er bietet alle Lese- und Aktionswerkzeuge des festen
Werkzeugkatalogs an. Einen eingebauten Chat oder ein eigenes Modell hat TitanOS
nicht.

## Einschalten

1. **Einstellungen › API und CLI › MCP für KI-Clients**: „MCP-Server für dieses
   NAS einschalten“ (nur Besitzer und Administratoren; ab Werk aus). Die
   Einstellung liegt in `/config/system/mcp.json` und gilt sofort; Ausschalten
   weist auch laufende Clients beim nächsten Aufruf ab (`403 mcp_disabled`).
2. „Token für MCP erstellen“ legt ein persönliches Token an, vorbelegt mit
   `mcp`, `system:read`, `files:read` und für Besitzer/Administratoren
   zusätzlich `docker:read` und `vms:read`. Schreibrechte (`files:write`,
   `docker:write`, `vms:write` und `system:write` für Verwaltungsaktionen) nur
   bewusst dazunehmen; `system:write`, `docker:*` und `vms:*` gibt es nur für
   Besitzer/Administratoren. Das Geheimnis erscheint
   einmal; der Dialog zeigt dabei den fertigen Befehl für Claude Code.
3. Den Client mit einem der Ausschnitte von der Seite einrichten (siehe unten).

Jedes Konto, auch mit Rolle Benutzer, kann ein MCP-Token für seine eigenen
Freigaben und Dateien anlegen. Tokens verwaltet nur eine normale angemeldete
Sitzung, nie ein Token.

## Transport und Protokoll

- **Streamable HTTP**: `POST https://<nas>/mcp`, gleicher Port und dasselbe
  Zertifikat wie die Weboberfläche. JSON-RPC 2.0, ein Nachrichtenobjekt je
  Anfrage; Stapel (Arrays) nur mit Protokoll `2025-03-26`.
- Protokollversionen `2025-06-18` (bevorzugt) und `2025-03-26`. `initialize`
  handelt die Version aus (unbekannt → `2025-06-18`); danach schicken Clients
  `MCP-Protocol-Version`. Fehlt die Kopfzeile, gilt `2025-03-26`, eine
  unbekannte ergibt `400`.
- Antworten kommen als `application/json`. Ein Client, der nur
  `text/event-stream` akzeptiert, bekommt dieselbe Antwort als ein SSE-Ereignis.
  Benachrichtigungen ohne `id` ergeben `202 Accepted` ohne Inhalt.
- Kein serverseitiger Ereignisstrom und keine Sitzungen: `GET`/`DELETE /mcp`
  antworten `405`, es gibt keine `Mcp-Session-Id`. Jede Anfrage trägt das Token.
- Methoden: `initialize`, `ping`, `tools/list`, `tools/call`; alles andere
  `-32601`. Fähigkeit nur `tools` (`listChanged: false`), keine Resources,
  Prompts oder Sampling.
- Browserseiten anderer Herkunft werden wie bei der Weboberfläche abgewiesen
  (Cross-Origin-Schutz von titand).
- Anmeldung: `Authorization: Bearer <token>`. Ohne/ungültiges Token `401` mit
  `WWW-Authenticate: Bearer`, ohne Bereich `mcp` `403 token_scope_required`.
  OAuth bietet der Server nicht an; Clients brauchen die Möglichkeit, eine
  Kopfzeile zu setzen (Claude Code `--header`, mcp-remote `--header`) oder die
  stdio-Brücke.
- Rate-Limit je Token im RAM: 60 Anfragen am Stück, danach eine pro Sekunde;
  darüber `429` mit `Retry-After` und JSON-RPC-Fehler `-32000`. In einem Batch
  (nur 2025-03-26, höchstens 32 Nachrichten) zählt jede weitere Nachricht als
  eigene Anfrage; über dem Limit bekommt sie einzeln den Fehler `-32000`.

Umsetzung: `backend/internal/api/mcp.go` (Transport, Einstellung, Limit,
Protokoll), `mcp_tools.go` (Werkzeuge), ausschließlich Standardbibliothek.

## Werkzeuge

Es gibt keine eigene MCP-Werkzeugliste. Die Werkzeuge entstehen bei jedem
`tools/list` und `tools/call` aus zwei bestehenden Listen (`mcpAvailable` in
`mcp_tools.go`):

- **Fester Werkzeugkatalog** (`catalogTools` in `api/catalog.go`, Werkzeuge in
  `catalog_actions.go`, `catalog_areas.go`, `catalog_files.go`,
  `catalog_status.go` und `homeassistant*.go`; Typen, Argumentprüfung und
  Handbuch im Paket `backend/internal/toolcatalog`). Lesewerkzeuge liefern
  Daten; Aktionswerkzeuge beschreiben genau einen Aufruf der Web-API, den
  titand in-process mit dem Konto des Tokens ausführt (`inProcessIdentity`,
  dieselbe Handler-Kette wie `/api/v1`).
- **Routen der öffentlichen API** (`publicRoutes` in `public_routes.go`, dieselbe
  Liste, aus der Tokenprüfung und OpenAPI entstehen), sofern sie ein Werkzeug
  nennen: `files_read_text`, `files_mkdir`, `files_create`, `files_rename`,
  `files_delete`, `files_copy`, `files_write_text`,
  `docker_container_start|stop|restart`, `docker_app_network`, `vm_start`,
  `vm_shutdown`, `vm_reboot`. Pfadparameter, Abfrage- und Body-Felder werden
  zum Eingabeschema.

Doppelte Katalogwerkzeuge lässt MCP aus (`mcpSkip`): `folder_create`,
`file_rename`, `file_delete` und `container_action`; dafür gibt es
`files_mkdir`, `files_rename`, `files_delete` und
`docker_container_start|stop|restart`.

Welche Werkzeuge ein Token bekommt, entscheiden seine Bereiche und die
**aktuelle Rolle** des Kontos (`mcpToolScope`):

| Bereich | Werkzeuge (Auswahl) | Wer |
|---|---|---|
| `system:read` | `docs_search`, `docs_read` (Handbuch); `system_status`, `notifications`, `update_status`, `ups_status`, `storage_overview`, `disk_tests`, `backup_status`, `backup_snapshots`, `backup_files`, `network_status`, `network_diagnostics`, `hardware_status`, `firewall_status`, `accounts_list`, `plugins_list`, `logs_search`, `ha_entities`, `ha_state`, `ha_history`, `ha_capabilities`, `ha_diagnostics`; für Benutzer `nas_info` | alle; die meisten nur Besitzer/Administratoren |
| `system:write` | `volume_create`, `volume_action`, `disk_test`, `share_create`, `share_delete`, `backup_action`, `backup_check`, `backup_restore_files`, `backup_schedule`, `stick_backup`, `firewall_rule_add`, `firewall_rule_remove`, `firewall_unblock`, `account_action`, `plugin_install`, `plugin_switch`, `plugin_update`, `plugin_remove`, `update_action`, `power_action`, `ha_call`, `ha_undo` | nur Besitzer/Administratoren |
| `files:read` | `files_list`, `file_read`, `document_read`, `files_search`, `file_jobs`, `shares_list`, `my_shares`, `files_read_text` | alle, nur eigene Wurzeln |
| `files:write` | `file_write`, `files_move`, `file_copy` und die `files_*`-Routen | alle, nur eigene Wurzeln |
| `docker:read` / `docker:write` | `docker_list`, `container_logs`, `docker_auto_updates` / `app_action`, `apps_update_all`, `docker_check_updates`, `docker_auto_update` und die `docker_*`-Routen | nur Besitzer/Administratoren |
| `vms:read` / `vms:write` | `vm_list` / `vm_action` und die `vm_*`-Routen | nur Besitzer/Administratoren |

Dateiwerkzeuge sehen nur die Wurzeln des Kontos, ohne Stick, ohne `.titanos`,
`.env` und Teildateien. Die Handler prüfen Rolle, Freigaberechte und Wurzeln bei
jedem Aufruf erneut. Wird ein Konto herabgestuft, gesperrt, gelöscht oder sein
Passwort geändert, gilt das sofort auch für MCP.

### Schaltung je Werkzeug

Jedes Werkzeug hat in `/config/system/mcp.json` (Feld `tools`, Name → Modus)
einen von drei Modi; ein nicht genanntes Werkzeug steht auf „on“:

- **`off`:** Das Werkzeug fehlt in `tools/list` und lässt sich nicht aufrufen.
- **`on`** (ab Werk): Lesen und normale Änderungen sind erlaubt; Aufrufe der
  Stufe „Gefahr“ oder „Zerstörung“ lehnt TitanOS ab (`403`).
- **`all`:** Auch gefährliche und zerstörende Aufrufe sind erlaubt – dann aber
  nur mit `confirm: true` vom Client.

Die Stufe eines Aufrufs bestimmt TitanOS aus dem Ziel und den Argumenten (zum
Beispiel Löschen, Ersetzen, Container stoppen, Plugin entfernen, Konto löschen,
Neustart, gefährliche Home-Assistant-Domänen), nie der Client. Den
Bestätigungswert, den eine Route für zerstörende Aktionen erwartet, setzt der
Server selbst. Eine weitere Freigabe per Klick oder per Handy gibt es nicht.

`GET /api/v1/system/mcp` liefert neben `enabled`, `path`, `protocolVersions` und
`scope` die Liste `tools` mit `name`, `area`, `scope`, `kind` (`read`/`change`),
`dangerous` und `mode` für alle Werkzeuge, die ein Konto dieser Rolle
erreichen könnte. `PUT /api/v1/system/mcp` (nur Besitzer/Administratoren)
speichert `enabled` und `tools`; ungültige Modi ergeben
`400 invalid_tool_mode`. Der PUT ersetzt die ganze Einstellung: Wer nur
`enabled` schickt, setzt alle Werkzeuge auf „on“ zurück. Heute liegt in der
Weboberfläche nur der Ein-/Ausschalter unter Einstellungen › API und CLI.

**Nicht enthalten** sind Geheimnisse (Backup-Passwörter, `.env`, Schlüssel,
Sticksicherungen), Passwörter, Zwei-Faktor und Passkeys, die Verwaltung der
Tokens selbst, Shells und Konsolen, die Bestätigung von Netzwerk- und
Firewall-Änderungen, Firewall ein/aus, die MCP-Einstellungen selbst sowie
Datei-Downloads als Binärdaten. Diese Routen stehen in keiner der beiden
Listen; Routen mit Web-Sitzungspflicht (`web_session_required`, `browserOnly`,
z. B. Volume-Passwörter, IPv64, Konsolen) lehnen In-Process-Aufrufe zusätzlich
ab. `TestMCPToolsComeOnlyFromTheExplicitLists` und
`TestCatalogEntireToolCatalog` prüfen das.

**Bestätigung:** Gefährliche und zerstörende Werkzeuge bieten das Argument
`confirm` an (nur `true`), dessen Beschreibung sagt, dass es erst nach
ausdrücklicher Zustimmung der Person gesetzt werden darf; bei den
Routenwerkzeugen ist es im Schema Pflicht. Die Annotationen
`readOnlyHint`/`destructiveHint` sagen dasselbe. Ob der Client vorher fragt,
entscheidet er; TitanOS erzwingt nur Modus „all“ und `confirm: true`.

**Ergebnisse** sind Daten, keine Anweisungen (Beschreibung jedes Werkzeugs und
`instructions` von `initialize`, Deutsch und Englisch). Text als
`content[0].text`, mit Protokoll `2025-06-18` zusätzlich `structuredContent`.
Fehler der Route oder ungültige Argumente (zentrale Schemaprüfung
`toolcatalog.ValidateArguments`, unbekannte Felder abgelehnt) ergeben
`isError: true` mit der Meldung; unbekannte Werkzeuge `-32602`. Ein Aufruf darf
zwei Minuten dauern, Antworten höchstens 4 MiB.
`202` (Kopieraufträge) heißt nur gestartet; den Endzustand über `file_jobs`
abfragen.

## Aktivitätsprotokoll

Jeder `tools/call` steht genau einmal unter Werkzeuge › Aktivität mit Herkunft
„per MCP“, dem Konto und als Ziel `token <Name>`, dem Werkzeugnamen und den von
der Route genannten Zielen (Pfade, Namen; nie Inhalte). Änderungen tragen die
Route als Aktion (z. B. `POST /api/v1/files/delete`), Lesewerkzeuge `mcp_tool`.
Abgelehnte Aufrufe (fehlende Bestätigung, Rechte) stehen als fehlgeschlagen
darin, auch Aufrufe eines Werkzeugs, das der Token nicht hat (Status 403). `initialize`, `ping` und `tools/list` werden nicht protokolliert. Das
Ein-/Ausschalten und die Werkzeugschaltung sind `PUT /api/v1/system/mcp`.

## Clients einrichten

Die Seite zeigt die Ausschnitte mit der Adresse dieses NAS; `<TOKEN>` durch das
Token ersetzen. Bei selbst signiertem Zertifikat das Zertifikat bzw. dessen CA
über einen vertrauenswürdigen Weg beziehen (Einstellungen › Zugang) – oder ein
Zertifikat von Let's Encrypt bzw. Tailscale verwenden.

**Claude Code** (HTTP direkt):

```sh
claude mcp add --transport http titanos https://nas.example/mcp --header "Authorization: Bearer <TOKEN>"
```

**Claude Desktop mit mcp-remote** (`claude_desktop_config.json`, braucht
Node.js; das Token steht in der Umgebung, weil Claude Desktop Argumente mit
Leerzeichen nicht überall richtig übergibt; bei eigenem Zertifikat zusätzlich
`"NODE_EXTRA_CA_CERTS": "/pfad/nas-cert.pem"` in `env`):

```json
{
  "mcpServers": {
    "titanos": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://nas.example/mcp", "--header", "Authorization:${TITANOS_AUTH}"],
      "env": { "TITANOS_AUTH": "Bearer <TOKEN>" }
    }
  }
}
```

**stdio-Brücke `titanosctl mcp`** für Clients ohne HTTP-Transport (oder ohne
Node.js): liest je Zeile eine JSON-RPC-Nachricht von stdin, schickt sie mit dem
Token an `<url>/mcp` und schreibt jede Antwort als eine Zeile nach stdout.
Das Token kommt wie bei allen Befehlen aus `--token-file` (Modus 0600) oder
`TITANOS_TOKEN`, nie aus einem Argument; `--ca-file` für ein eigenes
Zertifikat; keine Weiterleitungen. Die ausgehandelte Protokollversion wird als
Kopfzeile mitgeschickt, SSE-Antworten werden entpackt. Lehnt das NAS ab
(ausgeschaltet, Token, Rate-Limit, Netzfehler), antwortet die Brücke der
jeweiligen Anfrage mit einem JSON-RPC-Fehler `-32000` und schreibt den Grund
nach stderr. Nachrichten laufen nacheinander.

```json
{
  "mcpServers": {
    "titanos": {
      "command": "titanosctl",
      "args": ["--url", "https://nas.example", "--token-file", "/home/me/.config/titanos-token", "mcp"]
    }
  }
}
```

`titanosctl` liegt im Abbild unter `/usr/bin/titanosctl` und als
`titanos-<version>-cli-linux-amd64` im Release (siehe `docs/PUBLIC-API.md`);
andere Betriebssysteme bauen es mit `go build ./cmd/titanosctl`.

## Prüfstand und Grenzen

Geprüft mit Go-Tests (`backend/internal/api/mcp_test.go`,
`mcp_catalog_test.go`, `catalog_*_test.go`,
`backend/cmd/titanosctl/mcp_test.go`) und Webtests (`web/tests/mcp.test.mjs`):
Handshake mit beiden Versionen, Benachrichtigung, Stapel, SSE, falsche
Protokollversion, Fremdherkunft, Schalter aus/an, fehlender Bereich, Werkzeuge
nach Rolle und Bereich (Benutzer gegen Besitzer, herabgestuftes Konto,
Fremdfreigabe), schreibgeschützte Freigabe, Pfadausbruch aus einer Route,
Bestätigungspflicht, Werkzeugmodi off/on/all und die Werkzeugliste von
`GET /api/v1/system/mcp`, Katalogaktionen in-process, Aktivitätseinträge,
keine geheimen Routen, Rate-Limit und die stdio-Brücke gegen einen
nachgebauten Server.

Nicht geprüft: echte Clients (Claude Desktop, Claude Code, mcp-remote) gegen
ein echtes NAS, Windows-/macOS-Builds von `titanosctl` und das Verhalten von
Clients bei `confirm`. Das Rate-Limit beginnt nach einem titand-Neustart neu.
