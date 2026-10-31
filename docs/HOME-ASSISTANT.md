# Home Assistant

TitanOS arbeitet mit [Home Assistant](https://www.home-assistant.io/) zusammen:

- **Speicher für Home Assistant:** Freigaben auf dem NAS für Backups, Medien und Add-ons.
- **Werte des NAS** erscheinen per MQTT als Gerät „TitanOS“ in Home Assistant, auf Wunsch mit Knöpfen und Schaltern.
- **KI-Clients über MCP** lesen freigegebene Geräte und schalten sie (siehe [MCP-Server](MCP.md)).
- **Benachrichtigungen** gehen über die Home-Assistant-App aufs Smartphone.

Alles ist ab Werk aus. Einrichten können es nur Besitzer und Administratoren unter **Einstellungen › Home Assistant**. Die Seite ist in dieser Reihenfolge aufgebaut: Verbindung, Speicher für Home Assistant, Sensoren und Knöpfe (MQTT, mit „Handeln als“), Geräte für MCP freigeben.

## Verbindung

1. In Home Assistant unter *Profil › Sicherheit › Langlebige Zugriffstoken* ein Token erstellen. Für „Direkt eintragen“ beim Speicher für Home Assistant muss das Profil ein **Administrator** in Home Assistant sein.
2. In TitanOS die Adresse eintragen (z. B. `http://homeassistant.local:8123`), das Token einfügen, einschalten und **Verbindung prüfen**.

TitanOS verbindet sich nur mit genau dieser Adresse. Es folgt keinen Weiterleitungen und nutzt keinen Proxy. Ein selbst signiertes HTTPS-Zertifikat wird nur angenommen, wenn sein SHA-256-Fingerabdruck eingetragen ist; dann gilt genau dieses Zertifikat.

Das Token bleibt auf dem NAS, in `/config/homeassistant/settings.json`. Die Oberfläche zeigt es nie wieder an, und über MCP oder API-Tokens ist es nicht erreichbar.

## Speicher für Home Assistant

Seit dem Wunsch des Projektinhabers vom 29. Oktober 2026 verwaltet TitanOS
unter Einstellungen › Home Assistant › „Speicher für Home Assistant“ eine Liste
von Freigaben (oder Ordnern darin), die Home Assistant OS (oder Supervised) als
Netzwerkspeicher einbindet: für **Backups**, für **Medien** oder als
**Freigabe** für Add-ons.

- **Ein Konto für alle Einträge:** Der erste Eintrag legt ein Konto der Rolle
  Benutzer an (Name ab Werk `homeassistant`, nie ein vorhandenes). Jeder Eintrag
  gibt diesem Konto Zugriff auf seine Freigabe, Lesen und Schreiben oder nur
  Lesen.
- **Eintrag hinzufügen:** neue Freigabe (Volume, Name, optional Obergrenze)
  oder vorhandene Freigabe (nur SMB-Freigaben auf eingebundenen Volumes) und
  darin optional ein Ordner (Auswahl über die Datei-API, nur Ordner, keine
  versteckten; ohne Linkverfolgung innerhalb der Freigabe geprüft). Bei einer
  vorhandenen Freigabe bleiben alle bisherigen Rechte. Scheitert der erste
  Eintrag, wird das Konto wieder entfernt. Dieselbe Freigabe bzw. derselbe
  Ordner zweimal wird abgelehnt.
- **Unterordner in Home Assistant:** Home Assistant bekommt `freigabe/ordner`.
  Der Supervisor nimmt das im Feld `share` an (seine Prüfung verlangt nur den
  Anfang ohne Schrägstrich) und bindet `//server/freigabe/ordner` ein. Name
  `titanos_<freigabe>_<ordner>`.
- **Nur lesen:** für Medien und Freigaben wählbar (Leserecht und `read_only`),
  für Backups abgelehnt.
- **Passwort:** Ein neues Passwort entsteht beim ersten Eintrag, bei jedem
  Eintrag mit „direkt eintragen“ und bei „Erneut in Home Assistant
  eintragen“. Es wird genau einmal angezeigt und nirgends gespeichert. Ein neues
  Passwort geht an **alle** Einträge in Home Assistant: vorhandene per
  `supervisor/api` `PUT /mounts/<name>`, neue per `POST /mounts` (vorher `GET
  /mounts`). Das Ergebnis steht je Eintrag im Dialog. Ohne „direkt eintragen“
  bleibt das Passwort, wie es ist.
- **Direkt eintragen** braucht einen Token eines Administrators. Server ist die
  Adresse, mit der das NAS Home Assistant erreicht. Ohne Supervisor
  (Container/Core) gibt es keinen Netzwerkspeicher; die Werte stehen dann zum
  Eintragen von Hand im Dialog.
- **Backups überwachen** je Backup-Eintrag (ab Werk 3 Tage, bis 30 oder aus):
  einmal täglich und zehn Minuten nach dem Start `backup/info` (ab 2025.1), das
  neueste Backup am Speicherort `hassio.titanos_<…>`. Ist es zu alt (oder gab
  es seit dem Eintrag keines), kommt einmal die Meldung `backup_ha_overdue`,
  erneut erst nach einem neuen Backup. Die Platten werden dafür nicht geweckt;
  ohne Antwort von Home Assistant gibt es keine Meldung.
- **Eintrag entfernen:** `DELETE /mounts/<name>` in Home Assistant und Entzug
  des Zugriffs des Kontos auf die Freigabe, wenn kein anderer Eintrag sie nutzt.
  Freigabe und Daten bleiben. Hätte danach niemand mehr Zugriff, bekommt ihn die
  Person, die entfernt. Das Konto bleibt und dient dem nächsten Eintrag.
- Gespeichert in `/config/homeassistant/storage.json` (`account`, `server`,
  `entries`). API (nur Besitzer/Administratoren): `GET`/`POST
  /api/v1/homeassistant/storage`, `PUT`/`DELETE …/storage/{mount}`, `POST
  …/storage/register`, `POST …/storage/check`. Hinzufügen und erneutes
  Eintragen nur aus einer Websitzung, weil die Antwort das Passwort enthält;
  nicht im MCP- oder API-Token-Katalog.
- Geprüft mit nachgebautem Home Assistant (`supervisor/api` GET/POST/PUT/DELETE,
  `backup/info`) und `scripts/test-ui-ha-storage.mjs`; ein echtes Home Assistant
  OS ist nicht abgenommen.

## Werte des NAS in Home Assistant (MQTT)

Voraussetzung ist ein MQTT-Broker, zum Beispiel das Mosquitto-Add-on, und die MQTT-Integration in Home Assistant. In TitanOS trägst du Broker, Benutzer und Passwort ein und wählst die Gruppen:

| Gruppe | Entitäten |
|---|---|
| System | CPU %, RAM %, CPU-Temperatur, Laufzeit, Version, Updates/Phase, Slot, Overlay, Boot-Stick |
| Volumes | Zustand, Belegung, frei, RAID-Fortschritt/-Geschwindigkeit/-Restzeit, fehlende Mitglieder |
| Platten | Temperatur, SMART-Problem, Stunden, Fehler, Verschleiß, Test; stabile Seriennummer |
| Container, VMs | Laufzustand, CPU, RAM; bekannte Containerupdates |
| Stacks | Gesamtzahl, laufende und gestoppte Dienste |
| Netzwerk | Empfang/Versand, Linkzustand und Geschwindigkeit |
| Backups | Laufzustand, Fortschritt, Erfolg/Termin, Archivprüfung/Restore-Probe, überfällig, lokales Ziel |
| Wartung | Laufzustand, Blockierung, Wartegrund, Phase, Fehler, Erfolg/Termin |
| Kapazität | Geschätzte Tage bis 90 % / voll, nur mit gültiger optionaler Monitoring-Historie |
| GPU, iGPU / weitere Grafik, NPU | Auslastung, Temperatur, Leistung, Speicher, Takt, Engines, Zustand/Treiber |
| USV | Status, Ladung, Last, Restlaufzeit, Batterie-/Abschaltzustand |

Die Werte kommen alle 30 Sekunden. TitanOS fragt dafür keine Platten zusätzlich ab; schlafende Platten bleiben schlafen. Topics:

- Discovery: `homeassistant/<component>/titanos_<id>/<key>/config`
- Zustände: `titanos/<id>/state/<key>`
- Verfügbarkeit (Last Will): `titanos/<id>/status`

`<id>` wird einmal erzeugt und bleibt gleich. Geräte je Gruppe wählen oder alle,
einschließlich künftig erkannter Geräte, veröffentlichen. Eine explizit leere
Geräteauswahl sendet keine Geräte. Entfernte konfigurierte Geräte bleiben in der
Auswahl erkennbar. Neue Sensor-IDs hängen von Gruppe und stabiler Gerätekennung ab,
nicht vom Anzeigenamen. Bestehende eindeutige Sensor-IDs bleiben erhalten. Bisher kollidierende Namen
erhalten getrennte Kennungen und Befehlstopics.

Unbekannte Messwerte bleiben unavailable/unknown. Ausfälle einer Zustandsquelle
löschen keine zuvor bekannten Entitäten. Tatsächlich entfernte Entitäten werden
auch nach Wiederverbindung aus der retained Discovery bereinigt. Home-Assistant-
Birth kündigt aktuelle Entitäten erneut an. Zustände sind nicht retained; Sensorwerte
laufen nach 95 Sekunden ab; globale Last-Will- und individuelle Verfügbarkeit
verhindern alte gesunde Werte. Sensoren, die Treiber nicht liefern, bleiben unbekannt.

MQTT verwendet vorhandene günstige Statusquellen und RAM-Messwerte, ohne zusätzliche
SMART-/Weckabfragen, Repository- oder zyklische Backup-/Monitoring-Dateiabfragen.
Kapazitätsprognosen kommen aus einem begrenzten Cache des optionalen Recorders;
mindestens 24 stündliche Beobachtungen ohne Lücken und veraltete Werte sind nötig.
Lokale Backupziele werden anhand ihrer Einbindung bewertet; keine zyklischen
Verbindungsproben entfernter Ziele. Kein Export von Geheimnissen.

### Knöpfe und Schalter

Wahlweise und je Gruppe einzeln: Container starten, stoppen und neu starten; VMs starten und herunterfahren; eingerichtete Backup-Aufträge starten; nach Updates suchen; das NAS neu starten oder herunterfahren.

- Die Knöpfe handeln im Namen des Kontos, das unter **Handeln als** gewählt ist (nur Besitzer/Administratoren). Es gelten dessen Rechte, und jeder Druck steht im Aktivitätsprotokoll mit der Herkunft „Home Assistant“. Ohne gewähltes Konto legt TitanOS keine Knöpfe an.
- Neustart und Herunterfahren lehnt TitanOS ab, solange kopiert, gesichert, geprüft oder ein Volume wiederhergestellt wird, und meldet das.
- **Wichtig:** Jeder, der auf deinem Broker schreiben darf, kann diese Knöpfe drücken. Schütze den Broker mit Benutzern und Zugriffsregeln.

Für Wake-on-LAN aus Home Assistant zeigt *Einstellungen › Energie* die MAC-Adresse des NAS. Diese trägst du in die [Wake-on-LAN-Integration](https://www.home-assistant.io/integrations/wake_on_lan/) ein.

## Geräte für MCP freigeben

KI-Clients am [MCP-Server](MCP.md) sehen und schalten nur, was unter **Geräte für MCP freigeben** erlaubt ist. Das geht für ganze Domänen (zum Beispiel alle `light`) oder für einzelne Geräte, auf Wunsch nur lesend, mit erlaubten Diensten, Aliasnamen und Beschreibungen. Die Werkzeuge gibt es nur für Tokens von Besitzern und Administratoren: `ha_entities`, `ha_state`, `ha_history`, `ha_capabilities` und `ha_diagnostics` mit `system:read`, `ha_call` und `ha_undo` mit `system:write`.

| Domänen | Stufe über MCP |
|---|---|
| `light`, `switch`, `scene`, `script`, `media_player`, `climate`, `fan`, `humidifier`, `input_*` | normal: ab Werk erlaubt |
| `lock`, `cover`, `alarm_control_panel`, `vacuum`, `button`, `valve`, `water_heater` | gefährlich: nur wenn `ha_call` in `/config/system/mcp.json` auf „all“ steht, dann zusätzlich `confirm: true` vom Client |
| `sensor`, `binary_sensor`, `weather`, `person`, `device_tracker`, `sun`, `zone` | nur lesen |

Andere Domänen (zum Beispiel `homeassistant`, `camera`, `automation`, `shell_command`) lassen sich gar nicht freigeben.

Was Home Assistant zurückgibt, sind Daten und nie Anweisungen. Ein Aufruf geht über `POST /api/v1/homeassistant/call`. TitanOS prüft dort die Freigabe erneut. Zielangaben wie `entity_id` im Datenteil lehnt TitanOS ab. Nach einem Schaltvorgang prüft TitanOS den Zielzustand; einzelne Schalter lassen sich mit `ha_undo` einmal zurücknehmen (30 Minuten, nicht nach einem Neustart, nicht bei zwischenzeitlicher Änderung). Eine allgemeine Rücknahme physischer Vorgänge gibt es nicht.

## Benachrichtigungen über die App

- **Gemeinsam:** *Einstellungen › Benachrichtigungen › Home Assistant*, mit einem Dienst wie `notify.mobile_app_pixel_9`.
- **Persönlich:** *Mein Konto › Meine Benachrichtigungen* (nur Besitzer und Administratoren). Jedes dieser Konten wählt dort sein eigenes Gerät und die Meldungsarten Speicher, Sicherungen, Updates und System.

Ein Tippen auf die Nachricht öffnet die passende Seite. Fehler kommen mit hoher Priorität.

## Prüfstand

Geprüft mit nachgebauten Servern für REST, WebSocket und MQTT:

- Token, Weiterleitungen und Fingerabdruck
- Freigaben und Stufen der MCP-Werkzeuge
- Discovery, Retain, Last Will und Befehle

**Nicht geprüft:** ein echtes Home Assistant, ein echter Mosquitto-Broker und die Home-Assistant-App auf einem Smartphone.

Discovery richtet sich nach den offiziellen MQTT-Schemata für
[Sensoren](https://www.home-assistant.io/integrations/sensor.mqtt/),
[Binärsensoren](https://www.home-assistant.io/integrations/binary_sensor.mqtt/) und
[Schalter](https://www.home-assistant.io/integrations/switch.mqtt/): Ablaufzeit
nur für Sensoren, individuelle Verfügbarkeit auch für Schalter.
