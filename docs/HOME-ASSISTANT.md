# Home Assistant

Seit 0.14.0 arbeitet TitanOS mit [Home Assistant](https://www.home-assistant.io/) zusammen:

- Der **KI-Assistent** liest freigegebene Geräte und schaltet sie.
- **Benachrichtigungen** gehen über die Home-Assistant-App aufs Smartphone.
- **Zustandsänderungen** in Home Assistant starten Automationen des Assistenten.
- **Werte des NAS** erscheinen per MQTT als Gerät „TitanOS“ in Home Assistant, auf Wunsch mit Knöpfen und Schaltern.
- **Assist:** Du fragst per Sprache oder Text „Frag das NAS …“.

Alles ist ab Werk aus. Einrichten können es nur Besitzer und Administratoren unter **Einstellungen › Home Assistant**.

Details zum [Ausbau in 0.15.0](ASSISTANT-EXPANSION.md): einzelne Dienstefreigaben und Aliasnamen, Gerätefähigkeiten, Zielzustandsprüfung, begrenzte Rücknahme, Historien, zusätzliche Automationsbedingungen und MQTT-Backupwerte.

## Verbindung

1. In Home Assistant unter *Profil › Sicherheit › Langlebige Zugriffstoken* ein Token erstellen. Für Assist und für die Knöpfe der App-Benachrichtigungen muss das Profil ein **Administrator** in Home Assistant sein. Nur dann darf die Verbindung diese Ereignisse abonnieren.
2. In TitanOS die Adresse eintragen (z. B. `http://homeassistant.local:8123`), das Token einfügen, einschalten und **Verbindung prüfen**.

TitanOS verbindet sich nur mit genau dieser Adresse. Es folgt keinen Weiterleitungen und nutzt keinen Proxy. Ein selbst signiertes HTTPS-Zertifikat wird nur angenommen, wenn sein SHA-256-Fingerabdruck eingetragen ist; dann gilt genau dieses Zertifikat.

Das Token bleibt auf dem NAS, in `/config/homeassistant/settings.json`. Die Oberfläche zeigt es nie wieder an, und der KI-Assistent bekommt es nie zu sehen.

## Der KI-Assistent steuert Home Assistant

Unter **Freigaben für den KI-Assistenten** legst du fest, was der Assistent sieht und schaltet. Das geht für ganze Domänen (zum Beispiel alle `light`) oder für einzelne Geräte. Zusätzlich muss unter *Einstellungen › KI-Assistent* der Bereich **Home Assistant** auf „lesen“ oder „ändern“ stehen.

| Domänen | Bestätigung |
|---|---|
| `light`, `switch`, `scene`, `script`, `media_player`, `climate`, `fan`, `humidifier`, `input_*` | normale Freigabe |
| `lock`, `cover`, `alarm_control_panel`, `vacuum`, `button`, `valve`, `water_heater` | Freigabe mit Häkchen |
| `sensor`, `binary_sensor`, `weather`, `person`, `device_tracker`, `sun`, `zone` | nur lesen |

Andere Domänen (zum Beispiel `homeassistant`, `camera`, `automation`, `shell_command`) lassen sich gar nicht freigeben.

Was Home Assistant zurückgibt, behandelt der Assistent als Daten und nie als Anweisung. Ein Aufruf geht über `POST /api/v1/homeassistant/call`. TitanOS prüft dort die Freigabe erneut. Zielangaben wie `entity_id` im Datenteil lehnt TitanOS ab.

## Benachrichtigungen über die App

- **Gemeinsam:** *Einstellungen › Benachrichtigungen › Home Assistant*, mit einem Dienst wie `notify.mobile_app_pixel_9`.
- **Persönlich:** *Mein Konto › Meine Benachrichtigungen*. Jedes Konto wählt dort sein eigenes Gerät.

Ein Tippen auf die Nachricht öffnet die passende Seite. Fehler kommen mit hoher Priorität.

Ist unter *Einstellungen › KI-Assistent* **Freigeben und Ablehnen aus Benachrichtigungen** eingeschaltet, tragen Nachrichten über wartende Aktionen die Knöpfe „Freigeben“ und „Ablehnen“. Jeder Knopf gilt einmal, 15 Minuten lang und nur bis zum nächsten Neustart von TitanOS. Zerstörende Aktionen lassen sich nie aus einer Nachricht freigeben.

## Automationen mit Home-Assistant-Auslöser

Im Automationseditor gibt es das Ereignis **Home Assistant**. Du wählst ein freigegebenes Gerät und optional einen dieser Filter:

- „wechselt zu“ bzw. „wechselt von“, zum Beispiel `not_home`
- „steigt über“ bzw. „fällt unter“ für Zahlen

TitanOS hält dafür eine WebSocket-Verbindung und abonniert nur die Geräte eingeschalteter Automationen. Zustandswechsel werden im Speicher mitgeschrieben. Ein kurzer Wechsel zwischen zwei Prüfungen geht deshalb nicht verloren. Was passiert, während die Verbindung unterbrochen ist, wird nicht nachgeholt.

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
Verbindungsproben entfernter Ziele. Kein Export von Geheimnissen oder Chatinhalten.

### Knöpfe und Schalter

Wahlweise und je Gruppe einzeln: Container starten, stoppen und neu starten; VMs starten und herunterfahren; eingerichtete Backup-Aufträge starten; nach Updates suchen; das NAS neu starten oder herunterfahren.

- Die Knöpfe handeln im Namen des Kontos, das unter **Konto für Home Assistant** gewählt ist. Es gelten dessen Rechte, und jeder Druck steht im Protokoll. Ohne gewähltes Konto legt TitanOS keine Knöpfe an.
- Neustart und Herunterfahren lehnt TitanOS ab, solange kopiert, gesichert, geprüft oder ein Volume wiederhergestellt wird, und meldet das.
- **Wichtig:** Jeder, der auf deinem Broker schreiben darf, kann diese Knöpfe drücken. Schütze den Broker mit Benutzern und Zugriffsregeln.

Für Wake-on-LAN aus Home Assistant zeigt *Einstellungen › Energie* die MAC-Adresse des NAS. Diese trägst du in die [Wake-on-LAN-Integration](https://www.home-assistant.io/integrations/wake_on_lan/) ein.

## Assist: „Frag das NAS …“

1. Unter *Einstellungen › Home Assistant* ein Konto wählen und **Fragen aus Home Assistant beantworten** einschalten.
2. In Home Assistant die Blueprint [`titanos-assist.yaml`](homeassistant/titanos-assist.yaml) importieren: *Einstellungen › Automationen & Szenen › Blueprints › Blueprint importieren*, mit der Adresse der Datei auf GitHub. Danach daraus eine Automation anlegen.

Die Blueprint feuert `titanos_ask` mit einer Kennung und der Frage. TitanOS beantwortet die Frage als das gewählte Konto in einem eigenen Chat und feuert `titanos_answer`. Braucht eine Änderung eine Freigabe, antwortet TitanOS „Dafür brauche ich deine Freigabe in TitanOS“ und schickt, wenn eingerichtet, die Benachrichtigung mit den Knöpfen.

## Native Assist-Integration (optional)

Die Blueprint bleibt verwendbar. Für Gespräche mit Rückfragen:

1. `titanos-0.15.0-homeassistant.zip` aus dem Release entpacken und den Ordner
   `custom_components/titanos` in das Konfigurationsverzeichnis von Home Assistant
   kopieren. Alternativ die Dateien aus [custom_components/titanos](homeassistant/custom_components/titanos) verwenden.
2. Home Assistant neu starten. Unter *Einstellungen › Geräte & Dienste ›
   Integration hinzufügen* **TitanOS** wählen.
3. Die NAS-Kennung aus *TitanOS › Einstellungen › Home Assistant* eintragen.
   Diese Kennung ist kein Geheimnis. Verbindung, HA-Konto und Assist-Freigabe in
   TitanOS müssen bereits eingerichtet sein.
4. In der Assist-Sprachpipeline **TitanOS** als Gesprächsagent auswählen.

Die Integration sendet Fragen über den HA-Ereignisbus und wartet auf die Antwort
mit passender Request- und NAS-Kennung. Gesprächszusammenhang bleibt im NAS-Chat;
keine separate Modellverbindung und kein NAS-Passwort in Home Assistant. Nach
30 Minuten ohne Verwendung oder einem titand-Neustart beginnt ein neues Gespräch.
Freigaben erfolgen über die bestehenden NAS-Karten bzw. persönliche Meldungen.
Lange Aufträge laufen im NAS weiter; ihre Ergebnismeldungen werden in *Mein Konto*
eingerichtet. Doppelte Ereignisse lösen innerhalb des RAM-Zeitfensters keine
zweite Ausführung aus.

Die Integration ist mit der aktuellen Conversation-Entity-Schnittstelle
implementiert, aber noch nicht in einer echten HA-Installation abgenommen.

## Prüfstand

Geprüft mit nachgebauten Servern für REST, WebSocket und MQTT:

- Token, Weiterleitungen und Fingerabdruck
- Freigaben und Stufen
- Auslöser über die API
- Discovery, Retain, Last Will und Befehle
- Assist und die Knöpfe der App

**Nicht geprüft:** ein echtes Home Assistant, ein echter Mosquitto-Broker, die Home-Assistant-App auf einem Smartphone und die Blueprint in einem echten Home Assistant.

Discovery richtet sich nach den offiziellen MQTT-Schemata für
[Sensoren](https://www.home-assistant.io/integrations/sensor.mqtt/),
[Binärsensoren](https://www.home-assistant.io/integrations/binary_sensor.mqtt/) und
[Schalter](https://www.home-assistant.io/integrations/switch.mqtt/): Ablaufzeit
nur für Sensoren, individuelle Verfügbarkeit auch für Schalter.
