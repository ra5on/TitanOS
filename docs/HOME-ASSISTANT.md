# Home Assistant

Seit 0.14.0 arbeitet TitanOS mit [Home Assistant](https://www.home-assistant.io/) zusammen:

- Der **KI-Assistent** liest freigegebene Geräte und schaltet sie.
- **Benachrichtigungen** gehen über die Home-Assistant-App aufs Smartphone.
- **Zustandsänderungen** in Home Assistant starten Automationen des Assistenten.
- **Werte des NAS** erscheinen per MQTT als Gerät „TitanOS“ in Home Assistant, auf Wunsch mit Knöpfen und Schaltern.
- **Assist:** Du fragst per Sprache oder Text „Frag das NAS …“.

Alles ist ab Werk aus. Einrichten können es nur Besitzer und Administratoren unter **Einstellungen › Home Assistant**.

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

Ist unter *Einstellungen › KI-Assistent* **Freigeben und Ablehnen aus Benachrichtigungen** eingeschaltet, tragen Nachrichten über wartende Aktionen die Knöpfe „Freigeben“ und „Ablehnen“. Jeder Knopf gilt einmal, 15 Minuten lang und nur bis zum nächsten Neustart von TitanOS. Aktionen, für die man einen Namen eintippen muss, lassen sich nie aus einer Nachricht freigeben.

## Automationen mit Home-Assistant-Auslöser

Im Automationseditor gibt es das Ereignis **Home Assistant**. Du wählst ein freigegebenes Gerät und optional einen dieser Filter:

- „wechselt zu“ bzw. „wechselt von“, zum Beispiel `not_home`
- „steigt über“ bzw. „fällt unter“ für Zahlen

TitanOS hält dafür eine WebSocket-Verbindung und abonniert nur die Geräte eingeschalteter Automationen. Zustandswechsel werden im Speicher mitgeschrieben. Ein kurzer Wechsel zwischen zwei Prüfungen geht deshalb nicht verloren. Was passiert, während die Verbindung unterbrochen ist, wird nicht nachgeholt.

## Werte des NAS in Home Assistant (MQTT)

Voraussetzung ist ein MQTT-Broker, zum Beispiel das Mosquitto-Add-on, und die MQTT-Integration in Home Assistant. In TitanOS trägst du Broker, Benutzer und Passwort ein und wählst die Gruppen:

| Gruppe | Entitäten |
|---|---|
| System | CPU %, RAM %, CPU-Temperatur, Laufzeit, Update verfügbar, Version |
| Volumes | Zustand, Belegung in %, frei in GB |
| Platten | Temperatur, Problem (SMART); benannt nach der Seriennummer |
| Container, VMs | läuft (an/aus) |
| USV | Status, Ladung |

Die Werte kommen alle 30 Sekunden. TitanOS fragt dafür keine Platten zusätzlich ab; schlafende Platten bleiben schlafen. Topics:

- Discovery: `homeassistant/<component>/titanos_<id>/<key>/config`
- Zustände: `titanos/<id>/state/<key>`
- Verfügbarkeit (Last Will): `titanos/<id>/status`

`<id>` wird einmal erzeugt und bleibt gleich. Verschwindet ein Container, verschwindet auch seine Entität.

### Knöpfe und Schalter

Wahlweise und je Gruppe einzeln: Container starten, stoppen und neu starten; VMs starten und herunterfahren; nach Updates suchen; das NAS neu starten oder herunterfahren.

- Die Knöpfe handeln im Namen des Kontos, das unter **Konto für Home Assistant** gewählt ist. Es gelten dessen Rechte, und jeder Druck steht im Protokoll. Ohne gewähltes Konto legt TitanOS keine Knöpfe an.
- Neustart und Herunterfahren lehnt TitanOS ab, solange kopiert, gesichert, geprüft oder ein Volume wiederhergestellt wird, und meldet das.
- **Wichtig:** Jeder, der auf deinem Broker schreiben darf, kann diese Knöpfe drücken. Schütze den Broker mit Benutzern und Zugriffsregeln.

Für Wake-on-LAN aus Home Assistant zeigt *Einstellungen › Energie* die MAC-Adresse des NAS. Diese trägst du in die [Wake-on-LAN-Integration](https://www.home-assistant.io/integrations/wake_on_lan/) ein.

## Assist: „Frag das NAS …“

1. Unter *Einstellungen › Home Assistant* ein Konto wählen und **Fragen aus Home Assistant beantworten** einschalten.
2. In Home Assistant die Blueprint [`titanos-assist.yaml`](homeassistant/titanos-assist.yaml) importieren: *Einstellungen › Automationen & Szenen › Blueprints › Blueprint importieren*, mit der Adresse der Datei auf GitHub. Danach daraus eine Automation anlegen.

Die Blueprint feuert `titanos_ask` mit einer Kennung und der Frage. TitanOS beantwortet die Frage als das gewählte Konto in einem eigenen Chat und feuert `titanos_answer`. Braucht eine Änderung eine Freigabe, antwortet TitanOS „Dafür brauche ich deine Freigabe in TitanOS“ und schickt, wenn eingerichtet, die Benachrichtigung mit den Knöpfen.

## Prüfstand

Geprüft mit nachgebauten Servern für REST, WebSocket und MQTT:

- Token, Weiterleitungen und Fingerabdruck
- Freigaben und Stufen
- Auslöser über die API
- Discovery, Retain, Last Will und Befehle
- Assist und die Knöpfe der App

**Nicht geprüft:** ein echtes Home Assistant, ein echter Mosquitto-Broker, die Home-Assistant-App auf einem Smartphone und die Blueprint in einem echten Home Assistant.
