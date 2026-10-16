# Eigene Apps: Immich und Ollama

Die mit TitanOS ausgelieferten Vorlagen stehen im App-Store unter der Quelle
TitanOS. Es werden keine Container-Abbilder oder KI-Modelle in das NAS-System
gepackt. Erst die Installation lädt die ausgewählten Container auf das
Docker-Daten-Volume. Ports, Pfade und Umgebungswerte lassen sich im vorhandenen
Installationsdialog anpassen; eine zweite Installation bekommt einen eigenen
App-Namen und Datenordner.

## Immich

Enthalten sind Immich Server und Machine Learning **v3.3.3**, Valkey **9** und die
offizielle PostgreSQL-14-/VectorChord-Kombination. Die Datenbank und Valkey sind
auf die von Immich veröffentlichten Digests festgelegt. Die Datenbank verwendet
ein zufällig erzeugtes Passwort, das beide benötigten Dienste gemeinsam aus der
privaten `.env` erhalten. Es steht nicht im Compose-Text. Abhängigkeiten warten
auf die Datenbank- und Valkey-Healthchecks.

Standard: Weboberfläche auf Port **2283**, Fotos unter `appdata/<name>/library`,
PostgreSQL unter `appdata/<name>/postgres`, KI-Modelle unter
`appdata/<name>/model-cache`. Mindestens 6 GB RAM und zwei CPU-Kerne für Immich
vorsehen. Beim ersten Öffnen das Administratorkonto erstellen. Fotos und Datenbank
zusammen sichern. Neue Server- und ML-Versionen gemeinsam nach den offiziellen
Upgradehinweisen übernehmen.

CPU-Betrieb funktioniert ohne GPU-Plugin. Hardware wird ausschließlich auf
Wunsch einem Dienst zugeordnet. Für Video-Transcoding wird die GPU dem Server
zugewiesen und die Beschleunigung zusätzlich in Immich aktiviert. GPU-Inferenz
im ML-Dienst erfordert dessen offizielles CUDA- oder OpenVINO-Abbild; das normale
CPU-Abbild wird nicht automatisch ersetzt.

Die angepasste offizielle Compose-Vorlage bleibt **AGPL-3.0-only**. Originaltext
und geänderter Quelltext liegen im Release-Lizenzarchiv. Diese Lizenz gilt für
diese Vorlage; der eigene TitanOS-Code behält seine Lizenz.

## Ollama mit Open WebUI

**Ollama 0.40.5** und **Open WebUI v0.12.2**. Weboberfläche auf Port **3000**;
Ollamas API hat keinen veröffentlichten Host-Port und ist im App-Netz erreichbar.
Open WebUI wartet auf Ollamas Healthcheck. Anmeldung bleibt aktiviert, der
Sitzungsschlüssel wird zufällig erzeugt und dauerhaft gespeichert. Das erste
Konto wird beim ersten Öffnen eingerichtet. Die Oberfläche und Markenhinweise
der Originalsoftware bleiben unverändert.

Modelle liegen unter `appdata/<name>/models`, Konten, Chats und weitere WebUI-Daten
unter `appdata/<name>/webui`. Ein Sprachmodell wird erst nach Auswahl durch den
Nutzer geladen. RAM-/VRAM- und Speicherbedarf richten sich nach diesem Modell.
Die normale Vorlage läuft auf der CPU. Für NVIDIA wird im Installationsdialog
beim Dienst **ollama** ausdrücklich eine GPU ausgewählt; das NVIDIA-Plugin muss
aktiv sein. TitanOS übergibt sie über die von Docker 26 / Compose 2.26 unterstützte
CDI-Gerätereservierung. Bestehende Big-Bear-Apps werden nicht ersetzt.

## Quellen

- [Immich v3.3.3](https://github.com/immich-app/immich/releases/tag/v3.3.3)
- [Immich Installationsanforderungen](https://docs.immich.app/install/requirements/)
- [Ollama](https://github.com/ollama/ollama/releases/tag/v0.40.5)
- [Open WebUI](https://github.com/open-webui/open-webui/releases/tag/v0.12.2)
