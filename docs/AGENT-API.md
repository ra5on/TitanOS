# API-Schlüssel und Agenten (MCP)

Stand: 21. Oktober 2026 · Entwurf vom Projektinhaber am 21. Oktober 2026 bestätigt, offene Punkte mit den Vorschlägen entschieden. **Phase 1 umgesetzt** (Schlüssel und Lese-Werkzeuge). Was davon abweicht, steht unter „Stand der Umsetzung“ am Ende.

## Ziel

Ein eigener KI-Agent, zum Beispiel mit einem Modell aus Ollama, soll das NAS kennen: Zustand der Volumes, Platten, Container, VMs, Backups und Meldungen lesen und erklären. Später soll er auch kleine Dinge tun dürfen (Container neu starten, Backup anstoßen), aber **nie ohne Zustimmung eines Menschen am NAS**.

Dafür braucht es zwei Teile:

1. **API-Schlüssel**: Ein Programm meldet sich ohne Passwort, ohne Cookie und ohne zweiten Faktor an, mit genau den Rechten, die man ihm gibt.
2. **Ein MCP-Server in titand**: eine kleine, feste Liste von Werkzeugen nach dem Model Context Protocol, die jeder MCP-fähige Agent versteht (eigenes Skript, Open WebUI, `ollmcp` und andere).

Heute bleibt einem Agenten nur, sich mit Namen und Passwort wie ein Browser anzumelden. Er hätte dann alle Rechte des Kontos, scheitert an 2FA, und im Protokoll sähe jede seiner Aktionen aus wie die eines Menschen.

## Grundsätze

- **Ab Werk aus.** Ein Schalter unter Einstellungen › Zugang („API-Schlüssel und Agenten erlauben“) schaltet alles ein. Ausgeschaltet wird jeder Schlüssel sofort abgelehnt, gespeicherte Schlüssel bleiben erhalten.
- **Nur lesen zuerst.** Phase 1 enthält keine einzige Aktion.
- **Aktionen nur mit Bestätigung im NAS**, nicht im Agenten. Der Agent kann eine Rückfrage in seinem eigenen Fenster vortäuschen oder von einem eingeschleusten Text dazu gebracht werden. Die Freigabe geschieht deshalb in der Weboberfläche, angemeldet mit Cookie, durch einen Besitzer oder Administrator.
- **Feste Werkzeuge statt der ganzen API.** Ein Schlüssel öffnet nur `/api/v1/mcp`, nicht die übrigen `/api/v1`-Endpunkte. Die REST-API ist nicht dokumentiert und ändert sich; die Werkzeugliste ist klein, geprüft und stabil.
- **Nie über einen Schlüssel erreichbar**, auch nicht später: Löschen jeder Art, Volumes anlegen oder ändern, Konten, Netzwerk, SSH, Zertifikat, Updates, Plugins, Terminal und Konsolen, Herunterladen von Sticksicherungen, Backup-Passwörter, Geheimnisse aus `.env` und Einstellungen, Schreiben von Dateien.
- **Was ein Agent liest, ist Daten, keine Anweisung.** Containernamen und Protokollzeilen können Text enthalten, der sich an das Modell richtet. Weil jede Aktion bestätigt werden muss, kann ein solcher Text höchstens eine Anfrage erzeugen, die ein Mensch ablehnt.

## API-Schlüssel

### Gestalt

```
titanos_ak_<id>_<geheimnis>
```

- `id`: 8 Zeichen, zufällig, nicht geheim. Damit findet titand den Schlüssel und das Protokoll nennt ihn.
- `geheimnis`: 32 zufällige Bytes, base64url.
- Das feste Präfix `titanos_ak_` lässt sich von Werkzeugen zur Geheimnissuche erkennen (zum Beispiel ein eigenes Muster beim GitHub Secret Scanning), falls ein Schlüssel versehentlich in einem Repository landet.

Gesendet wird er als `Authorization: Bearer titanos_ak_…`.

### Speicherung

`/config/users/<konto>/api-keys.json`, getrennt von `account.json`:

```json
{
  "keys": [
    {
      "id": "k3f9a2c1",
      "name": "ollama-helfer",
      "hash": "<SHA-256 des Geheimnisses, hex>",
      "scopes": ["status", "storage", "docker", "logs"],
      "created": "2026-10-21T18:00:00Z",
      "expires": "2027-01-19T18:00:00Z",
      "networks": ["172.16.0.0/12", "192.168.1.0/24"]
    }
  ]
}
```

- Nur der Hash wird gespeichert. Bei 32 Byte Zufall genügt SHA-256, ein langsamer Hash wie argon2id ist nicht nötig (wie bei den Anmeldungen in `sessions.go`). Der Vergleich läuft in konstanter Zeit.
- Eine eigene Datei, weil ältere Versionen von titand `account.json` beim nächsten Schreiben ohne unbekannte Felder neu schreiben würden. Bei einem Rückwechsel auf eine Version ohne Schlüssel bleibt die Datei liegen und wird nicht gelesen; Schlüssel wirken dort einfach nicht.
- Wird das Konto gelöscht, verschwinden seine Schlüssel mit dem Ordner.
- Die Sicherung des Sticks nimmt die Datei mit (wie alles unter `users/`), das Diagnosepaket nicht (`users/` ist dort ausgeschlossen).
- Höchstens 20 Schlüssel je Konto.
- „Zuletzt benutzt“ und „von Adresse“ liegen **nur im RAM** (mit den Anmeldungen in `/run/titanos/`, damit sie ein App-Update überstehen). Der Stick wird durch Benutzung nie beschrieben.

### Wer darf Schlüssel haben

- Nur Besitzer und Administratoren. Alle Werkzeuge aus Phase 1 zeigen Daten, die heute schon nur Administratoren sehen (`requireAdmin`).
- Die Rechte werden **bei jeder Anfrage** neu bestimmt: Bereiche des Schlüssels ∩ was die Rolle des Kontos heute darf. Wird ein Administrator zum Benutzer herabgestuft, ist sein Schlüssel sofort wirkungslos.
- Besitzer sehen unter „Konten“, wie viele Schlüssel ein Konto hat, und können sie entfernen.

### Bereiche

| Bereich | Phase | Inhalt |
|---|---|---|
| `status` | 1 | Name des NAS, Version, Slot, Laufzeit, Last, RAM, Temperaturen, Meldungen, Updates, USV |
| `storage` | 1 | Volumes, Platten, SMART-Zustand, Wiederaufbau und Prüfungen, Freigaben (ohne Rechte einzelner Konten) |
| `docker` | 1 | Container und Stacks, Zustand, Image, Update-Hinweis, CPU/RAM, Ports |
| `vms` | 1 | VMs, Zustand, vCPU/RAM, Adressen |
| `backups` | 1 | Aufträge, letzter und nächster Lauf, Ergebnis (nie Passwörter oder Ziel-Geheimnisse) |
| `logs` | 1 | Journal und Container-Logs, **immer geschwärzt** |
| `docker:act` | 2 | Container starten, stoppen, neu starten; App aktualisieren |
| `vms:act` | 2 | VM starten, herunterfahren, neu starten |
| `backups:act` | 2 | Backup-Auftrag jetzt ausführen |
| `storage:act` | 2 | Prüfung eines Volumes starten |

Ein `:act`-Bereich enthält nie den Lesebereich; die Oberfläche hakt ihn mit an.

### Weitere Schranken

- **Ablauf**: 30 Tage, 90 Tage (Vorschlag), 1 Jahr oder nie. Abgelaufene Schlüssel bleiben sichtbar, bis man sie entfernt.
- **Erlaubte Netze** (optional, CIDR-Liste): Vorschlag „Docker auf diesem NAS“ (`172.16.0.0/12`) und das eigene Heimnetz. Eine Anfrage aus einem anderen Netz wird abgelehnt, als wäre der Schlüssel falsch.
- **Fehlversuche** laufen über den vorhandenen `auth.Limiter` mit der Adresse als Schlüssel, wie bei der Anmeldung.
- **Bearer nur am MCP-Endpunkt.** Jede andere Route prüft weiter nur den Cookie. Eine Anfrage mit Schlüssel an einen anderen Endpunkt bekommt 401, sodass niemand aus Versehen an der Werkzeugliste vorbei arbeitet.

## MCP-Server

### Transport

- `POST /api/v1/mcp`, „Streamable HTTP“ nach der MCP-Spezifikation, **zustandslos**: Jede Anfrage ist eine JSON-RPC-Nachricht, die Antwort kommt als `application/json`. Keine Server-Sent Events, kein `GET`-Strom (405), keine Sitzungs-ID. titand braucht dafür nur die Standardbibliothek; die Bibliotheksliste bleibt wie sie ist. Das offizielle Go-SDK wäre die Alternative, falls später Ströme oder Server-Nachrichten nötig werden.
- Unterstützt: `initialize`, `notifications/initialized`, `ping`, `tools/list`, `tools/call`. Keine Ressourcen, keine Prompts, kein Sampling, keine Elicitation.
- Protokollversion `2025-06-18`. Fragt ein Client eine andere an, antwortet titand mit seiner (so sieht es die Spezifikation vor); der Header `MCP-Protocol-Version` wird geprüft.
- Läuft über denselben HTTPS-Port wie die Weboberfläche. `http.NewCrossOriginProtection` bleibt davor: Programme ohne `Origin`-Header kommen durch, eine fremde Webseite im Browser nicht.
- `tools/list` zeigt nur Werkzeuge, die der Schlüssel benutzen darf.

### Werkzeuge in Phase 1 (nur lesen)

Alle tragen `annotations.readOnlyHint: true`. Jedes liefert die Daten als `structuredContent` und, wie die Spezifikation es für ältere Clients empfiehlt, dasselbe JSON als Textblock.

| Werkzeug | Bereich | Liefert |
|---|---|---|
| `system_status` | `status` | Name, Version, Basis, Slot, Laufzeit, CPU-Last, RAM, Temperaturen |
| `notifications` | `status` | die letzten Meldungen der Glocke (höchstens 100) |
| `update_status` | `status` | aktive und Rückfall-Version, verfügbares Update, Kanal |
| `ups_status` | `status` | USV-Zustand, Ladung, Restlaufzeit |
| `storage_overview` | `storage` | Volumes mit RAID-Art, Zustand, Größe, Belegung, Wiederaufbau/Prüfung mit Fortschritt; Platten mit Modell, Größe, Temperatur, SMART-Urteil, Volume |
| `shares_list` | `storage` | Freigaben mit Volume, Protokollen, Belegung |
| `docker_list` | `docker` | Container und Stacks mit Zustand, Laufzeit, Image, Update-Hinweis, CPU/RAM, Ports |
| `container_logs` | `docker` + `logs` | letzte Zeilen eines Containers (höchstens 200), geschwärzt |
| `vm_list` | `vms` | VMs mit Zustand, vCPU, RAM, Adressen |
| `backup_status` | `backups` | Aufträge mit letztem Ergebnis, letztem Erfolg, nächstem Lauf, überfällig ja/nein |
| `logs_search` | `logs` | Journal nach Quelle, Stufe, Suchtext und Zeitraum (höchstens 200 Zeilen), geschwärzt |

Die Werkzeuge rufen dieselben Funktionen auf wie die Handler der Weboberfläche und lesen aus dem Speicher von titand. Kein Werkzeug greift auf `/config` zu oder weckt Platten.

**Schwärzen:** `logs_search` und `container_logs` laufen durch die Ersetzungen aus `diagnose/redact.go` (IP- und MAC-Adressen, E-Mail-Adressen, Kontonamen, Passwörter und Tokens, Schlüssel). Protokolle sind die Stelle, an der am ehesten ein Geheimnis steht, und ein Agent schickt sie vielleicht doch an ein Modell im Netz. Die übrigen Werkzeuge liefern keine Geheimnisse und bleiben ungeschwärzt.

### Werkzeuge in Phase 2 (Aktionen mit Bestätigung)

| Werkzeug | Bereich | Tut |
|---|---|---|
| `container_action` | `docker:act` | `start`, `stop`, `restart` eines Containers oder Stacks |
| `app_update` | `docker:act` | neues Image einer App holen und neu erstellen (wie „Aktualisieren“) |
| `vm_action` | `vms:act` | `start`, `shutdown`, `reboot` (kein „Ausschalten erzwingen“) |
| `backup_run` | `backups:act` | einen Auftrag jetzt ausführen |
| `volume_check` | `storage:act` | Prüfung eines Volumes starten |
| `request_status` | jeder `:act` | Stand einer Anfrage |

`annotations`: `readOnlyHint: false`, `destructiveHint` bei `stop`, `shutdown` und `reboot` true, sonst false.

### Ablauf einer Aktion

1. Der Agent ruft zum Beispiel `container_action {"name": "jellyfin", "action": "restart", "reason": "antwortet seit 10 Minuten nicht"}` auf. `reason` ist Pflicht und wird angezeigt.
2. titand prüft wie die Weboberfläche (gibt es den Container, ist die Aktion jetzt möglich) und legt eine **Anfrage** an. Die Antwort an den Agenten kommt sofort:
   `structuredContent: {"status": "pending", "request": "r7c2d9e0", "expires": "…"}` mit dem Text „Wartet auf Bestätigung am NAS.“
3. Am NAS erscheint
   - in der Glocke die Meldung `agent_request` (Stufe Warnung, damit sie ab Werk auch per E-Mail, ntfy usw. verschickt wird),
   - für jeden angemeldeten Besitzer und Administrator eine Leiste oben auf jeder Seite, wie bei einer unbestätigten Netzwerkänderung: „Agent **ollama-helfer** (Schlüssel von alex) möchte Container **jellyfin** neu starten. Grund: …“.
4. Ein Klick öffnet einen Dialog (`Dialog.svelte`) mit allen Angaben. „Ausführen“ (`.btn-warn`, links) und „Ablehnen“; Enter oder Autofokus lösen nichts aus. Für Aktionen mit `destructiveHint` muss zusätzlich ein Häkchen gesetzt werden („Ich weiß, dass … dabei kurz nicht erreichbar ist“).
5. Bestätigen geht nur mit Cookie-Anmeldung (`requireAdmin`), nie mit einem Schlüssel. Das bestätigende Konto muss die Aktion selbst ausführen dürfen. Es darf ein anderes sein als das Konto des Schlüssels.
6. titand führt aus. Der Agent erfährt das Ergebnis über `request_status` (`approved`, `running`, `done` mit Ergebnis, `failed` mit Fehler, `rejected`, `expired`).

Regeln für Anfragen:

- Sie liegen nur im RAM und laufen nach 15 Minuten ab. Ein Neustart von titand verwirft sie (Ergebnis `expired`).
- Höchstens 5 offene Anfragen je Schlüssel. Eine Anfrage, die einer offenen gleicht, gibt deren ID zurück.
- Was zur Zeit der Bestätigung nicht mehr passt (Container gibt es nicht mehr, VM läuft schon), schlägt mit Begründung fehl, statt etwas anderes zu tun.
- Solange ein Neustart für ein Update ansteht, werden keine neuen Anfragen angenommen. Eine laufende Aktion zählt umgekehrt zu den Arbeiten, auf die ein App-Update wartet (wie Kopien und Backups, Fehler `update_working`).

## Protokoll

- Jeder Werkzeugaufruf ergibt eine Zeile im Journal mit Konto, Schlüssel (Name und ID), Adresse, Werkzeug und Ergebnis, bei Aktionen zusätzlich mit Argumenten und dem bestätigenden Konto. Die Zeilen tragen ein eigenes Feld, damit „Werkzeuge › Protokoll“ eine Quelle „Agenten“ bekommt.
- Je Schlüssel merkt sich titand im RAM die letzten 50 Aufrufe. Die Seite zeigt sie unter dem Schlüssel.

## Oberfläche

**Mein Konto › API-Schlüssel** (nur Besitzer und Administratoren, nur sichtbar, wenn der Schalter an ist):

- Liste: Name, Bereiche als Chips, erstellt, läuft ab, zuletzt benutzt (seit dem Start) und von welcher Adresse, „Letzte Aufrufe“ zum Aufklappen.
- „Neuer Schlüssel“: Name, Bereiche zum Anhaken (Aktionsbereiche erst in Phase 2 und mit Hinweis), Ablauf, erlaubte Netze mit den beiden Vorschlägen.
- Danach wird der Schlüssel **einmal** angezeigt, wie der Kopplungscode für Backups, zusammen mit fertigen Angaben zum Kopieren:
  - Adresse: `https://<LAN-IP>/api/v1/mcp`
  - Header: `Authorization: Bearer …`
  - SHA-256-Fingerabdruck des Zertifikats und „Zertifikat herunterladen“ (nur der öffentliche Teil)
  - Beispiele für Open WebUI und für ein Python-Skript mit dem MCP-SDK
- „Entfernen“ mit Bestätigung (gefährliche Aktion links) wirkt sofort.

**Einstellungen › Zugang**: Schalter „API-Schlüssel und Agenten erlauben“ mit Erklärung, was ein Schlüssel kann und was nie.

**Glocke und Leiste** für Anfragen wie oben. Neue Texte in beiden Sprachdateien (`notify.kind.agent_request.*` und die Seite).

## Zertifikat

Ein Agent im Container erreicht das NAS über dessen LAN-Adresse; das selbst signierte Zertifikat nennt die lokalen Adressen (`tlscert.localAddresses`). Der Agent muss ihm vertrauen: entweder die heruntergeladene Zertifikatsdatei als CA einbinden oder den Fingerabdruck fest hinterlegen. Abschalten der Prüfung wird in den Beispielen nicht gezeigt. Mit einem eigenen Zertifikat (Zugang › Zertifikat) entfällt das. Bei der Umsetzung prüfen: Steht die Adresse im Zertifikat auch dann, wenn sich die LAN-Adresse nach dem Erzeugen geändert hat?

## So sieht das mit Ollama aus

```
┌──────────── NAS ─────────────────────────────────────────┐
│  Ollama (App)  ◄──  Agent (App, eigener Code             │
│   :11434            oder Open WebUI)                     │
│                       │ HTTPS + Bearer-Schlüssel         │
│                       ▼                                  │
│                titand /api/v1/mcp ──► Anfrage ──► Glocke │
│                                         ▲                │
│                     Mensch bestätigt in der Weboberfläche│
└──────────────────────────────────────────────────────────┘
```

Ollama selbst spricht kein MCP. Das übernimmt der Agent: Er holt `tools/list`, gibt die Werkzeuge als Tool-Definitionen an `/api/chat` von Ollama, führt die Tool-Aufrufe des Modells per `tools/call` aus und reicht die Ergebnisse zurück. Modelle mit Tool-Unterstützung (zum Beispiel Qwen 3 oder Llama 3.1 und neuer) kommen mit den kurzen Textzusammenfassungen gut zurecht.

Ein Beispiel dafür liegt in `examples/agent/`: ein Python-Programm (nur Standardbibliothek) mit Chat im Browser, verbunden mit Ollama und dem MCP-Endpunkt. Es vertraut dem selbst signierten Zertifikat über dessen Fingerabdruck und prüft ihn, bevor es den Schlüssel sendet. Das Image `ghcr.io/ra5on/titanos-agent` baut `.github/workflows/agent.yml` auf `main`; die App-Vorlage für `ra5on/TitanOS-Apps` liegt in `docs/apps-repo/titanos-agent/compose.yaml`.

## Umsetzung in titand

- `auth/apikeys.go`: Laden, Anlegen, Entfernen, Prüfen (Hash, Ablauf, Netze), RAM-Zustand „zuletzt benutzt“.
- `api/apikeys.go`: Endpunkte für die Seite (Cookie, `requireAdmin`): `GET/POST /api/v1/account/api-keys`, `DELETE /api/v1/account/api-keys/{id}`, für Besitzer `DELETE /api/v1/users/{name}/api-keys/{id}`.
- Paket `mcp` (neu): JSON-RPC, Werkzeugliste, Aufruf; bekommt dieselben Manager wie `api.Server` als schmale Schnittstellen, damit es sich mit Attrappen testen lässt.
- `api/mcp.go`: `POST /api/v1/mcp` mit eigener Prüfung `requireKey(scope)`; `requireUser` und `requireAdmin` bleiben unverändert und lesen weiter nur den Cookie.
- Phase 2: `agentreq` (Anfragen im RAM) und `GET /api/v1/agent-requests`, `POST /api/v1/agent-requests/{id}/{approve|reject}`.
- Einstellung im Abbild: nichts. Ein App-Update genügt (nur `backend/` und `web/`).

### Tests

- Schlüssel: falsch, abgelaufen, falsches Netz, Rolle entzogen, Konto gelöscht, Schalter aus, Fehlversuche gebremst; Schlüssel an einer anderen Route ergibt 401.
- MCP: `initialize`, Versionsaushandlung, `tools/list` je nach Bereich, unbekanntes Werkzeug, falsche Argumente, Geschwärztes in Logs.
- Phase 2: Bestätigung nur mit Cookie, nur mit passender Rolle, Ablauf, Doppelung, geänderter Zustand bei Bestätigung, kein Ausführen ohne Bestätigung.
- Ein Durchlauf mit einem echten MCP-Client (Python-SDK) gegen `titand -dev`.

## Phasen

1. **Schlüssel und Lesen**: alles aus „API-Schlüssel“ und die Werkzeuge aus Phase 1, Seite unter „Mein Konto“, Schalter unter „Zugang“.
2. **Aktionen mit Bestätigung**: Anfragen, Glocke, Leiste, Dialog, die Werkzeuge aus Phase 2.
3. **Später**: je Schlüssel „ohne Rückfrage“ für harmlose Aktionen (Container starten oder neu starten, Backup jetzt); Bereich `files:read` für einzelne Freigaben; dokumentierte REST-API mit denselben Schlüsseln. Chat in der Weboberfläche und Vorlage „TitanOS-Agent“ sind umgesetzt.

## Offene Entscheidungen

1. **Freigaben in Phase 1:** `shares_list` ohne Rechte einzelner Konten (Vorschlag) oder mit?
2. **Schwärzen:** nur Protokolle (Vorschlag) oder je Schlüssel wählbar für alle Werkzeuge, etwa für Agenten mit einem Modell im Netz?
3. **Anfragen per Push:** Stufe Warnung, damit sie ab Werk verschickt werden (Vorschlag), oder Hinweis?
4. **Bestätigen vom Smartphone:** reicht die Weboberfläche, oder soll eine Push-Nachricht einen Link enthalten, der direkt zum Dialog führt (nach Anmeldung)?
5. **Phase 3 „ohne Rückfrage“:** überhaupt anbieten oder grundsätzlich jede Aktion bestätigen?

## Chat in der Weboberfläche

Vom Projektinhaber am 22. Oktober 2026 beauftragt und am selben Tag umgesetzt (Paket `assistant`, `api/assistant.go`, `AssistantPanel.svelte`, Einstellungen › KI-Assistent). Geprüft mit Go-Tests gegen eine nachgebaute Ollama (Werkzeugaufrufe, unbekannte Werkzeuge, Schrittgrenze, Fehler, Einstellungen, Rechte, Systemtext aus dem Browser wird verworfen), Tests für die Markdown-Darstellung und im Entwicklungsserver mit nachgebauter Ollama. Nicht geprüft: ein echtes Modell.

- **Kein Schlüssel, keine App:** titand spricht selbst mit Ollama. Wer angemeldet ist, chattet mit den Rechten seines Kontos; nur Besitzer und Administratoren, weil die Werkzeuge zeigen, was nur sie sehen.
- **Dieselben Werkzeuge** wie der MCP-Server, direkt aufgerufen (kein Umweg über HTTP oder einen Schlüssel), alle nur lesend. Protokolle bleiben geschwärzt. Kommen in Phase 2 Aktionen dazu, erscheint die Bestätigung im Chat selbst.
- **Einstellungen › KI-Assistent** (`/config/system/assistant.json`): ein- und ausschalten (ab Werk aus), Adresse von Ollama (ab Werk `http://127.0.0.1:11434`, also die Ollama-App auf diesem NAS), voreingestelltes Modell aus der Liste von Ollama, „Verbindung prüfen“.
- **Bedienung:** Knopf mit Sprechblase in der Kopfzeile neben dem Terminal; der Chat öffnet sich als Seitenleiste rechts und bleibt beim Seitenwechsel offen. Modell wählbar, „Neuer Chat“, Vorschläge für den Anfang, je Antwort die benutzten Werkzeuge.
- **Verlauf** nur im Browser (sessionStorage dieses Tabs), nie im RAM des NAS oder auf dem Stick. Höchstens die letzten 20 Nachrichten gehen an das Modell.
- **Ablauf** wie beim Beispiel-Agenten: höchstens acht Schritte, Werkzeugergebnisse auf 12 000 Zeichen gekürzt, Antwort per Server-Sent Events (`POST /api/v1/assistant/chat`). Jeder Werkzeugaufruf steht als `assistant tool call` mit Konto im Protokoll.

## Stand der Umsetzung (Phase 1)

Umgesetzt in `auth/apikeys.go` (Schlüssel und Schalter), `mcp/mcp.go` (Protokoll), `api/mcp.go` (Endpunkt und Werkzeuge), `api/apikeys.go` (Endpunkte der Oberfläche), Oberfläche in `web/src/routes/account/ApiKeys.svelte`, Einstellungen › Zugang und auf der Seite „Nutzer“.

Entscheidungen zu den offenen Punkten: `shares_list` ohne Rechte einzelner Konten; geschwärzt werden nur Protokolle; Anfragen (Phase 2) mit Stufe Warnung; ein direkter Link zur Bestätigung und „ohne Rückfrage“ bleiben für später.

Abweichungen vom Entwurf:

- **Kein `disk_smart`.** titand kennt nur die zwischengespeicherten SMART-Werte, die `storage_overview` schon je Platte liefert (Zustand, Temperatur, Betriebsstunden, Fehler, Abnutzung). Eine eigene Abfrage hätte die Platte geweckt.
- **Text ist das JSON**, keine eigene Zusammenfassung, und es gibt kein `outputSchema`. Ein Schema, das vom Ergebnis abweicht, ließe Clients mit Prüfung scheitern; kleine Modelle kommen mit dem JSON zurecht.
- **„Zuletzt benutzt“ nur im RAM** von titand, nicht in `/run`: Nach einem App-Update oder Neustart von titand steht dort „seit dem letzten Start nicht benutzt“.
- **Keine eigene Quelle „Agenten“** unter Werkzeuge › Protokoll. Die Zeilen heißen `agent tool call` (bzw. `agent tool call failed`) und tragen Werkzeug, Schlüssel-ID und -Name, Konto und Adresse; die Suche nach „agent“ findet sie.
- **Bremse bei Fehlversuchen** über die Adresse, gemeinsam mit der Anmeldung (`ip:<adresse>` im `auth.Limiter`).
- **Gelöschte Konten:** Ihre Schlüssel verschwinden mit dem Ordner `users/<name>/` und werden im Speicher vergessen (`APIKeys.Forget`). Ein neues Konto gleichen Namens findet sie nicht. Ein Vergleich mit dem Erstellungsdatum des Kontos war geplant, wäre aber bei einer falsch gehenden Uhr (erster Start ohne Zeitabgleich) unzuverlässig.
- **Besitzer und Administratoren** entfernen auf der Seite „Nutzer“ alle Schlüssel eines anderen Kontos auf einmal (`DELETE /api/v1/users/<name>/api-keys`, Regeln wie `mayManage`), nicht einzeln.
- Endpunkte: `GET/POST /api/v1/account/api-keys`, `DELETE /api/v1/account/api-keys/{id}`, `GET/PUT /api/v1/system/api-access` (Schalter in `/config/system/api.json`), `POST /api/v1/mcp`.

Geprüft: Unit-Tests für Schlüssel (Ablauf, Netze, Rolle entzogen, Schalter, Grenzen, Nachfolger-Konto) und für den Endpunkt über HTTP (Versionsaushandlung, Werkzeuge je Bereich, falsche Argumente, unbekannte Werkzeuge und Methoden, Batches, `GET`, Schlüssel an anderen Routen, Cookie am MCP-Endpunkt, Bremse, Rechte der Endpunkte). Im lokalen Dev-Server: Schalter, Schlüssel anlegen, einmalige Anzeige, Netzbeschränkung (Aufruf aus einem anderen Netz abgewiesen), alle Werkzeuge aus `status`, `storage` und `logs`, Werkzeug außerhalb der Bereiche abgewiesen, Protokollzeilen, Entfernen. Nicht geprüft: ein echter MCP-Client (Open WebUI, Python-SDK) und ein Agent als App auf einem echten NAS mit selbst signiertem Zertifikat.
