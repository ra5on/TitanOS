# MCP-Server für KI-Clients

Seit der Entscheidung des Projektinhabers vom 29.10.2026 hat TitanOS einen
MCP-Server: Externe KI-Clients wie Claude Desktop, Claude Code oder andere
Programme mit dem Model Context Protocol arbeiten damit am NAS – mit genau den
Rechten eines persönlichen API-Tokens und des Kontos dahinter. Der integrierte
KI-Assistent (`docs/ASSISTANT.md`) bleibt davon getrennt; der MCP-Server
erweitert ihn nicht und nutzt keine seiner Einstellungen.

## Einschalten

1. **Einstellungen › API und CLI › MCP für KI-Clients**: „MCP-Server für dieses
   NAS einschalten“ (nur Besitzer und Administratoren; ab Werk aus). Die
   Einstellung liegt in `/config/system/mcp.json` und gilt sofort; Ausschalten
   weist auch laufende Clients beim nächsten Aufruf ab (`403 mcp_disabled`).
2. „Token für MCP erstellen“ legt ein persönliches Token an, vorbelegt mit
   `mcp`, `system:read`, `files:read` und für Besitzer/Administratoren
   zusätzlich `docker:read` und `vms:read`. Schreibrechte (`files:write`,
   `docker:write`, `vms:write`) nur bewusst dazunehmen. Das Geheimnis erscheint
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
  darüber `429` mit `Retry-After` und JSON-RPC-Fehler `-32000`.

Umsetzung: `backend/internal/api/mcp.go` (Transport, Einstellung, Limit,
Protokoll), `mcp_tools.go` (Werkzeuge), ausschließlich Standardbibliothek.

## Werkzeuge

Es gibt keine eigene Werkzeugliste. Die Werkzeuge entstehen bei jedem
`tools/list` und `tools/call` aus zwei bestehenden Listen:

- **Lesewerkzeuge aus dem festen Katalog des Assistenten**
  (`statusTools` in `assistant_registry.go`, Ausgaben aus `assistant_tools.go`).
  `mcpCatalog` in `mcp_tools.go` wählt aus und nennt den nötigen Tokenbereich.
  Welche es gibt, entscheidet wie beim Assistenten die **aktuelle Rolle**:
  - Besitzer/Administratoren: `system_status`, `notifications`, `ups_status`,
    `storage_overview`, `disk_tests`, `backup_status` (`system:read`),
    `shares_list` (`files:read`), `docker_list`, `container_logs`
    (`docker:read`), `vm_list` (`vms:read`).
  - Benutzer: `nas_info` (`system:read`), `my_shares` (`files:read`).
  - Alle: `files_list`, `file_read`, `file_jobs` (`files:read`) – nur die
    Wurzeln des Kontos, ohne Stick, ohne `.titanos`, `.env` und Teildateien.
- **Routen der öffentlichen API** (`publicRoutes` in `public_routes.go`, dieselbe
  Liste, aus der Tokenprüfung und OpenAPI entstehen), sofern sie ein Werkzeug
  nennen. Pfadparameter, Abfrage- und Body-Felder werden zum Eingabeschema;
  der Aufruf läuft als Anfrage an genau diese Route durch dieselbe
  Handler-Kette wie `/api/v1`, mit dem Konto des Tokens:
  - `files_read_text` (`files:read`; wie im Editor nur mit Schreibrecht auf die
    Freigabe), `files_mkdir`, `files_create`, `files_rename`, `files_delete`,
    `files_copy`, `files_write_text` (`files:write`),
  - `docker_container_start|stop|restart`, `docker_app_network`
    (`docker:write`, nur Besitzer/Administratoren),
  - `vm_start`, `vm_shutdown`, `vm_reboot` (`vms:write`, nur
    Besitzer/Administratoren).

Ein Werkzeug erscheint nur, wenn das Token den Bereich hat und die Rolle ihn
erlaubt; die Handler prüfen Rolle, Freigaberechte und Wurzeln bei jedem Aufruf
erneut. Wird ein Konto herabgestuft, gesperrt, gelöscht oder sein Passwort
geändert, gilt das sofort auch für MCP.

**Nicht enthalten** sind Geheimnisse (Backup-Passwörter, `.env`, Schlüssel,
Sticksicherungen), Shells und Konsolen, Updates, Netzwerk-/Firewall-Bestätigung
und -Änderungen, Konten, Passwörter, Zwei-Faktor und Passkeys, die Verwaltung
der Tokens selbst, die Einstellungen des Assistenten sowie Datei-Downloads als
Binärdaten. Diese Routen stehen in keiner der beiden Listen;
`TestMCPToolsComeOnlyFromTheExplicitLists` prüft das.

**Bestätigung:** Werkzeuge der Stufe „Gefahr“ oder „Zerstörung“ (Löschen,
Kopieren/Verschieben mit Ersetzen, Text überschreiben, Container stoppen/neu
starten, App-Netz, VM herunterfahren/neu starten) verlangen das Argument
`confirm`, das im Schema Pflicht ist, nur `true` erlaubt und beschreibt, dass es
erst nach ausdrücklicher Zustimmung der Person gesetzt werden darf. Die
Annotationen `readOnlyHint`/`destructiveHint` sagen dasselbe. Das ist eine
Absicherung für den Client, keine zweite Freigabestufe in TitanOS:
Ob der Client vorher fragt, entscheidet er.

**Ergebnisse** sind Daten, keine Anweisungen (Beschreibung jedes Werkzeugs und
`instructions` von `initialize`, Deutsch und Englisch). Text als
`content[0].text`, mit Protokoll `2025-06-18` zusätzlich `structuredContent`.
Fehler der Route oder ungültige Argumente (zentrale Schemaprüfung
`assistant.ValidateArguments`, unbekannte Felder abgelehnt) ergeben
`isError: true` mit der Meldung; unbekannte Werkzeuge `-32602`. Ein Aufruf darf
zwei Minuten dauern, Antworten höchstens 4 MiB. `202` (Kopieraufträge) heißt
nur gestartet; den Endzustand über `file_jobs` abfragen.

## Aktivitätsprotokoll

Jeder `tools/call` steht genau einmal unter Werkzeuge › Aktivität mit Herkunft
„per MCP“, dem Konto und als Ziel `token <Name>`, dem Werkzeugnamen und den von
der Route genannten Zielen (Pfade, Namen; nie Inhalte). Änderungen tragen die
Route als Aktion (z. B. `POST /api/v1/files/delete`), Lesewerkzeuge `mcp_tool`.
Abgelehnte Aufrufe (fehlende Bestätigung, Rechte) stehen als fehlgeschlagen
darin. `initialize`, `ping` und `tools/list` werden nicht protokolliert. Das
Ein-/Ausschalten selbst ist `PUT /api/v1/system/mcp`.

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
`backend/cmd/titanosctl/mcp_test.go`) und Webtests (`web/tests/mcp.test.mjs`):
Handshake mit beiden Versionen, Benachrichtigung, Stapel, SSE, falsche
Protokollversion, Fremdherkunft, Schalter aus/an, fehlender Bereich, Werkzeuge
nach Rolle und Bereich (Benutzer gegen Besitzer, herabgestuftes Konto,
Fremdfreigabe), schreibgeschützte Freigabe, Pfadausbruch aus einer Route,
Bestätigungspflicht, Aktivitätseinträge, keine geheimen Routen, Rate-Limit und
die stdio-Brücke gegen einen nachgebauten Server.

Nicht geprüft: echte Clients (Claude Desktop, Claude Code, mcp-remote) gegen
ein echtes NAS, Windows-/macOS-Builds von `titanosctl` und das Verhalten von
Clients bei `confirm`. Das Rate-Limit beginnt nach einem titand-Neustart neu.
