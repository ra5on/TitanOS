# Sprachdienste: Sprache zu Text und Text zu Sprache

PLAN 92, Entscheidungen des Projektinhabers vom 14.11.2026 (Entwurf `Entwuerfe/92-sprache/`). Ab Werk aus. Seite: Einstellungen › Smart Home und KI › Sprachdienste (`/settings/speech`), nur Besitzer und Administratoren.

## Was es ist

- **Sprache zu Text:** [faster-whisper](https://github.com/SYSTRAN/faster-whisper) mit Wyoming-Anschluss, Port 10300.
- **Text zu Sprache:** [Piper](https://github.com/rhasspy/piper) mit Wyoming-Anschluss, Port 10200.
- Beide laufen in je einem Container, den TitanOS selbst verwaltet (`titanos-speech-whisper`, `titanos-speech-piper`). Sie erscheinen nicht als Apps.
- Nutzer: **Home Assistant** über die Integration „Wyoming Protocol“ und der **KI-Agent** (Mikrofon im Eingabefeld, „Vorlesen“).

## Aufbau

- **Paket `backend/internal/speech`:** Einstellungen und Modellliste (`model.go`), Abruf von Hugging Face (`fetch.go`), Verwaltung der Container, Modelle und Downloads (`manager.go`), Durchreichung der Anschlüsse (`proxy.go`), kleiner Wyoming-Client (`wyoming.go`). Docker-Schicht `backend/internal/docker/speech.go`, API `backend/internal/api/speech.go`, Verdrahtung `cmd/titand/wiring_speech.go`.
- **Einstellungen** in `/config/system/speech.json`: `enabled`, `volume`, `device` (`cpu` oder die Kennung einer NVIDIA-Karte), `whisperModel`, `language` (`auto` oder zwei Buchstaben), `voice`, `speed` (0,5–2), `reach` (`ha` oder `lan`), `agent`.
- **Modelle und Stimmen** liegen auf dem gewählten Volume unter `.titanos/speech/` (`whisper/<modell>/`, `piper/<stimme>.onnx` und `.onnx.json`, dazu die Kopie des Stimmenkatalogs `voices.json`). Der Ordner ist privat wie alle Betriebsdaten; verschwindet das Volume, entfernt titand die Container beim nächsten Blick. Das Volume lässt sich nur bei ausgeschalteter Sprache wechseln; Modelle ziehen dabei nicht mit um.
- **Abbilder:** eine geprüfte Version je TitanOS-Release, über den Digest festgenagelt (`speech.WhisperDigest`, `WhisperGPUDigest`, `PiperDigest` in `speech/manager.go` – Version und Digest zusammen ändern): `lscr.io/linuxserver/faster-whisper` v3.8.1-ls13 (Prozessor, etwa 0,23 GB zu laden) bzw. dessen CUDA-Bau (etwa 1,7 GB) und `lscr.io/linuxserver/piper` 2.5.3-ls138 (etwa 0,17 GB). TitanOS lädt sie beim Einschalten, sie gehören nicht zum Release. Lizenz der Abbilder: GPL-3.0-only (LinuxServer.io).
- **Kein Internet für die Container:** Sie sitzen in einem eigenen Docker-Netz ohne Weg nach draußen (`titanos-speech`, `internal`), veröffentlichen keinen Port und laufen mit `LOCAL_ONLY`. Modelle und Stimmen lädt **titand** herunter.
- **Anschlüsse:** titand hört selbst auf 10300 und 10200 und reicht eine Verbindung nur durch, wenn der Absender sie nutzen darf (die Wyoming-Anschlüsse haben kein Passwort): „Nur Home Assistant“ (ab Werk) = die Adressen der unter Einstellungen › Home Assistant eingetragenen Verbindungen, Apps auf diesem NAS (Docker-Netze) und das NAS selbst; „Heimnetz“ = jede private Adresse (auch Tailscale). Ist ein Port schon belegt (eine eigene Wyoming-App), sagt die Seite das. Je Dienst merkt sich titand im RAM die letzte Verbindung (Zeit, Absender, angenommen oder abgelehnt).

## Modelle und Stimmen

- **Whisper:** `large-v3-turbo` (empfohlen, 1,6 GB), `large-v3` (3,1 GB), `medium` (1,5 GB), `small` (0,5 GB), `base`, `tiny`. Quelle: die Repositories von faster-whisper bei Hugging Face (`Systran/faster-whisper-…`, `large-v3-turbo` unter `dropbox-dash/faster-whisper-large-v3-turbo`), laut Modellkarten MIT (nachgesehen 14.11.2026). titand liest die Dateiliste mit Größe und SHA-256 (`/api/models/<repo>?blobs=true`) und nimmt nur `config.json`, `model.bin`, `tokenizer.json`, `vocabulary.*`, `preprocessor_config.json`; das Modell selbst nie ohne Prüfsumme.
- **Piper:** der Katalog `voices.json` von `rhasspy/piper-voices` (am 14.11.2026: 182 Stimmen in 58 Sprachen, elf deutsche, u. a. Thorsten in drei Qualitäten, Kerstin, Karlsson). titand liest ihn höchstens einmal am Tag und hält eine Kopie auf dem Volume für Zeiten ohne Internet. Größe und MD5 jeder Datei stehen im Katalog. **Jede Stimme hat ihre eigene Lizenz** (Modellkarte der Stimme); TitanOS liefert keine Stimme mit.
- **Download:** im Hintergrund, mit Fortschritt; jede Datei entsteht neben ihrem Platz und wird erst nach Größen- und Prüfsummenprüfung dorthin verschoben, ein Modell erst als ganzer Ordner. Abbrechen und ein Fehlschlag hinterlassen nichts.
- **Aktiv** ist je ein Modell und eine Stimme; nur Heruntergeladenes lässt sich aktivieren, das Aktive nicht löschen. Wechseln ersetzt den Container des Dienstes (einige Sekunden), den anderen nicht.
- Piper meldet Home Assistant den ganzen Katalog, nicht nur die heruntergeladenen Stimmen (Verhalten des Dienstes): eine dort gewählte, hier nicht geladene Stimme spricht nicht.

## Grafikkarte

- „Rechnen auf“ listet jede NVIDIA-Karte, die TitanOS Containern anbietet (Treiber-Plugin, wie beim NVR über CDI `nvidia.com/gpu=<UUID>`), mit belegtem und freiem Speicher, und den Prozessor. Eine an eine VM durchgereichte oder ohne Treiber ist sichtbar, aber nicht wählbar.
- Die Grafikkarte nutzt **nur Sprache zu Text** (CUDA-Abbild, `--device cuda`). Piper läuft auf dem Prozessor: Für sein GPU-Abbild fanden sich nur Berichte ohne spürbaren Gewinn, geprüft ist es nicht.
- Ist die gewählte Karte nicht (mehr) da, meldet der Dienst „Grafikkarte nicht verfügbar“ und startet nicht – er weicht nicht still auf den Prozessor aus.
- Grafikspeicher laut fremden Messungen (nicht selbst gemessen): `large-v3-turbo` etwa 2 bis 2,7 GB, `large-v3` etwa 3,3 bis 4,4 GB. Reicht der Speicher neben Ollama und NVR nicht, endet der Dienst; die Seite zeigt dann seine letzten Zeilen.

## Zustände

`off` (aus), `model` (nichts aktiv oder nicht heruntergeladen), `pulling` (Abbild wird geladen, mit Fortschritt), `starting`, `running`, `error` mit `volume`, `docker`, `device` oder den Worten des Dienstes bzw. von Docker. Ein Container, der dreimal hintereinander endet, wird nicht weiter gestartet; eine geänderte Einstellung versucht es neu.

## API

Besitzer und Administratoren: `GET /api/v1/speech` (Einstellungen, Zustand, Modelle, Stimmen, Downloads, Grafikkarten, Volumes, Adressen), `PUT /api/v1/speech/settings`, `POST`/`DELETE /api/v1/speech/download` (`{kind: "model"|"voice", id}`), `DELETE /api/v1/speech/{kind}/{id}`, `POST /api/v1/speech/say` (`{text, voice}` → WAV), `POST /api/v1/speech/hear` (WAV, 16 Bit PCM → `{text}`).
Jedes Konto, das den Agenten nutzen darf, bei eingeschaltetem „Im KI-Agenten“: `GET /api/v1/agent/speech` (`{say, hear}`), `POST /api/v1/agent/speech/say`, `POST /api/v1/agent/speech/hear`. Der Browser wandelt die Aufnahme vorher in WAV (16 kHz, mono), auf dem NAS läuft kein Umwandler. Über API-Tokens und MCP sind die Sprachdienste nicht erreichbar.

## Home Assistant

1. Einstellungen › Geräte & Dienste › Integration hinzufügen › „Wyoming Protocol“: Host = Adresse des NAS, Port 10300.
2. Dieselbe Integration noch einmal mit Port 10200.
3. Einstellungen › Sprachassistenten: „faster-whisper“ für Sprache-zu-Text, „piper“ und die Stimme für Text-zu-Sprache; als Gesprächsagent der KI-Agent von TitanOS (`docs/AGENT.md`, Assist).

Home Assistant als App auf diesem NAS nimmt dieselbe Adresse; über Tailscale den Namen des NAS im Tailnet (dann „Heimnetz“ wählen oder Home Assistant mit seiner Tailnet-Adresse eintragen).

## Prüfstand (14.11.2026)

- Go: `internal/speech` (Einstellungen, Katalog, Download mit falscher Prüfsumme, Wyoming hin und zurück, WAV, Absenderregeln, Verwaltung mit nachgebautem Docker einschließlich Grafikkarte über CDI und fehlender Karte), `TestSpeechAPI`.
- **Echter Lauf** `TITANOS_SPEECH_REAL=<ordner> go test ./internal/speech -run TestRealSpeech` auf dem Entwicklungsrechner: echtes Docker, die echten Abbilder (Prozessor), Hugging Face; Modell `tiny` und Stimme `de_DE-thorsten-low` geladen, beide Container ohne Internet gestartet, ein deutscher Satz gesprochen und wieder mitgeschrieben, Anschluss 10200 durchgereicht, geändertes Tempo ersetzt nur den Piper-Container, am Ende alles entfernt.
- Oberfläche: `scripts/test-ui-speech.mjs` mit nachgebauten Daten (Arbeitsplatz, Handy, aus, Fehlerzustände, kein Überlauf).
- **Nicht geprüft:** eine NVIDIA-Karte und das CUDA-Abbild (auch nicht der Start über CDI auf einem NAS), die größeren Modelle, echtes Home Assistant über Wyoming, Mikrofon und Vorlesen im Agenten auf echten Geräten (Safari braucht HTTPS fürs Mikrofon), die Absenderregel mit echtem Home Assistant als Docker-App, Verhalten bei vollem Grafikspeicher. Zu prüfen auf dem NAS: `PRUEFEN.txt`.
