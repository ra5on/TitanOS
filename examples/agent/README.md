# TitanOS-Agent (Beispiel)

Ein kleiner Chat im Browser, der Fragen zu deinem NAS mit einem lokalen Modell aus [Ollama](https://ollama.com) beantwortet. Das Modell schlägt dafür in den Werkzeugen des MCP-Servers von TitanOS nach (siehe [docs/AGENT-API.md](../../docs/AGENT-API.md)): Volumes, Platten, Container, VMs, Backups, Meldungen, Protokolle. Der Agent **liest nur**.

Ein einziges Python-Programm, nur Standardbibliothek, dazu eine HTML-Seite. Es dient auch als Vorlage für eigene Agenten.

```
Browser ──► agent.py ──► Ollama  /api/chat (mit den Werkzeugen als „tools“)
                 │
                 └──────► TitanOS /api/v1/mcp (tools/list, tools/call; API-Schlüssel)
```

Für jede Frage holt der Agent die Werkzeugliste, gibt sie Ollama mit, führt die Werkzeugaufrufe des Modells über MCP aus und reicht die Ergebnisse zurück, bis das Modell antwortet (höchstens acht Schritte). Der Browser sieht, welche Werkzeuge benutzt wurden.

## Auf dem NAS installieren

1. In TitanOS unter **Einstellungen › Zugang** „API-Schlüssel und Agenten erlauben“ einschalten.
2. Unter **Mein Konto › API-Schlüssel für Agenten** einen Schlüssel erstellen. Bereiche nach Bedarf, als Netz „Docker auf diesem NAS“.
3. Die **Ollama**-App installieren und ein Modell laden, das Werkzeuge aufrufen kann, etwa `ollama pull qwen3:8b` (oder `llama3.1:8b`, `qwen3:4b` für wenig RAM).
4. Die App **TitanOS-Agent** installieren (Vorlage: [docs/apps-repo/titanos-agent/compose.yaml](../../docs/apps-repo/titanos-agent/compose.yaml)) und dabei Schlüssel und Fingerabdruck des Zertifikats (Einstellungen › Zugang) eintragen.
5. Die Chat-Seite öffnen (Port 8765). Benutzername beliebig, Passwort ist `AGENT_PASSWORD` (beim Installieren erzeugt, im Container-Editor sichtbar).

Oben auf der Seite steht, ob TitanOS und Ollama erreichbar sind; fehlt etwas, sagt die Seite was.

## Einstellungen

| Variable | Ab Werk | Bedeutung |
|---|---|---|
| `TITANOS_URL` | `https://host.docker.internal` | Adresse des NAS, aus dem Container gesehen |
| `TITANOS_API_KEY` | – | Schlüssel `titanos_ak_…`, Pflicht |
| `TITANOS_FINGERPRINT` | – | SHA-256 des Zertifikats, Pflicht bei https |
| `OLLAMA_URL` | `http://host.docker.internal:11434` | Ollama |
| `OLLAMA_MODEL` | `qwen3:8b` | Voreingestelltes Modell; im Chat wählbar |
| `AGENT_PASSWORD` | – | Passwort der Chat-Seite (HTTP Basic). Leer = offen |
| `AGENT_PORT` | `8080` | Port im Container |

**Zertifikat:** TitanOS signiert sein Zertifikat selbst. Der Agent prüft deshalb keinen Aussteller, sondern vergleicht den Fingerabdruck, bevor er etwas sendet; ein anderes Zertifikat (oder ein Angreifer dazwischen) bekommt den Schlüssel nie zu sehen. Ersetzt du das Zertifikat, trage den neuen Fingerabdruck ein.

**Sicherheit:** Was das Modell aus den Werkzeugen liest, ist für den Agenten nur Information; der Systemtext sagt dem Modell das. Weil der Schlüssel ohnehin nur lesen darf, kann ein eingeschleuster Text in einem Protokoll oder Containernamen nichts verändern. Die Chat-Seite läuft im Heimnetz über HTTP; sie nicht ins Internet freigeben.

## Entwickeln und testen

```bash
cd examples/agent
python3 -m unittest -v test_agent.py
```

Die Tests laufen gegen nachgebaute MCP- und Ollama-Server, auch mit einem selbst signierten Zertifikat (braucht `openssl`).

Gegen den Entwicklungsserver von TitanOS (`make dev`, HTTP ohne Zertifikat):

```bash
TITANOS_URL=http://127.0.0.1:8080 TITANOS_API_KEY=titanos_ak_… OLLAMA_URL=http://127.0.0.1:11434 AGENT_PORT=8765 python3 agent.py
```

Das Image `ghcr.io/ra5on/titanos-agent` baut der Workflow `.github/workflows/agent.yml` bei jeder Änderung auf `main`. Pull Requests bauen und testen nur.
