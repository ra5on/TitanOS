# NVR (Kamerarekorder)

Der NVR nimmt Netzwerkkameras auf, zeigt sie live und meldet Bewegung. Er ist ab Werk aus. Einschalten unter **Einstellungen › Dienste › NVR**; danach erscheint der Menüpunkt **NVR**. Nur Besitzer und Administratoren sehen ihn. Die festgelegten Regeln stehen in [rules/NVR.md](rules/NVR.md).

## Einrichten

1. **Volume für die Aufnahmen wählen** (NVR › Aufnahmen). Dauerschreiben hält Platten wach und nutzt einen SSD-Schreibcache ab; am besten eine eigene Platte oder ein eigenes Volume. Der Ordner `nvr` ist auf Btrfs ein eigenes Subvolume und damit nicht in Snapshots; Sicherungen überspringen ihn.
2. **Kamera hinzufügen:** Adresse, Benutzer, Passwort, dann **Kamera suchen**. TitanOS fragt zuerst per ONVIF, dann die üblichen RTSP-Pfade der großen Hersteller. Findet es nichts, unter **Weitere Angaben** den RTSP-Link eintragen (ohne Benutzer und Passwort im Link).
   - Nach höchstens **drei abgelehnten Anmeldungen** hört TitanOS auf, bei einer Kontosperre sofort. Es probiert keine Standardpasswörter.
   - Am besten einen eigenen Kamerabenutzer mit „nur ansehen“ anlegen.
   - Bei manchen Kameras (z. B. Reolink) sind RTSP und ONVIF ab Werk aus und müssen in der Kamera eingeschaltet werden.
3. **Aufnahme:** *Nur bei Ereignissen* (Vorschlag: aufgenommen wird laufend, behalten wird nur, was eine Minute vor bis eine Minute nach einer Bewegung liegt), *Dauerhaft* oder *Aus*. Aufgenommen wird ohne Umwandlung in Dateien zu fünf Minuten.

## Aufbewahrung

- **Aufnahmen** und **Ereignisse** haben je eine Dauer in Tagen, allgemein und je Kamera.
- **Freien Platz erhalten:** Reicht er nicht, gehen die ältesten Aufnahmen zuerst. Das geschieht nie still: Die Meldung „Aufbewahrung verkürzt“ nennt die verbleibenden und die gewünschten Tage.
- **Clip schützen** nimmt einzelne Aufnahmen von beidem aus, bis der Schutz aufgehoben wird.

## Ansehen

- **Live** je Kamera und **Ansichten** (Raster mit sechs Spalten, Feldgröße frei, leere Felder bleiben leer). Höchstens sechs Live-Bilder gleichzeitig.
- **Aufnahmen** nach Tag; **Ereignisse** mit Bild, ein Klick springt in die Aufnahme.

## Bewegung und Scharfschaltung

- **Bewegung erkennen** je Kamera, Empfindlichkeit niedrig/mittel/hoch. Helligkeitswechsel über das ganze Bild (Licht, Wolken) zählen nicht.
- **Scharfschaltung** unter **Einstellungen › Meldungen › Kameras (NVR)**: aus, an oder nach Zeitplan; Auswahl der Kameras; Ruhezeit je Kamera. Die NVR-Seite zeigt den Zustand und schaltet schnell um.
- Meldungen gehen über die Wege, die unter Einstellungen › Meldungen eingerichtet sind. Außerdem: Kamera liefert kein Bild (nach drei Minuten) und wieder da.
- **Objekterkennung** (Person, Fahrzeug, Paket, Tier, Kennzeichen) ist noch nicht eingebaut.

## Home Assistant

Unter Einstellungen › Home Assistant › MQTT: Gruppe **Kameras (NVR)** liefert je Kamera Aufnahme, Bewegung, belegten Platz und letzte Aufnahme sowie „NVR armed“. Die Befehlsgruppe **Kameras** macht daraus Schalter (Aufnahme je Kamera, Scharfschaltung). Für Anwesenheit schaltet eine Automation „NVR armed“, wenn niemand zuhause ist. Bilder und Video gehen nicht über MQTT.

## Zeit und Qualität

- **Zeit von TitanOS:** Die Kamera holt ihre Uhrzeit vom NAS (NTP, UDP 123). TitanOS stellt sie per ONVIF um, bei Hikvision und Dahua über deren eigene Schnittstelle. Bei eingeschalteter Firewall den Dienst „Zeitserver für Kameras“ für das Netz der Kamera erlauben.
- **Qualität** (nur mit ONVIF): Auflösung, Bilder pro Sekunde und Bitrate je Stream. TitanOS bietet nur an, was die Kamera nennt, fragt vor dem Schreiben und merkt sich die ursprünglichen Werte für **Zurücksetzen**.

## Datenschutz

Wer mit Kameras öffentliche Bereiche, Wege oder Nachbargrundstücke erfasst, muss in Deutschland und der EU die Datenschutzregeln (DSGVO, BDSG) beachten. Üblich sind:

- Nur das eigene Grundstück erfassen; fremde Bereiche mit Bildausschnitt oder Maske ausblenden.
- Ein gut sichtbares Hinweisschild mit verantwortlicher Person und Zweck.
- Eine kurze Speicherdauer (oft genannt: 72 Stunden, wenn nichts passiert ist). Die Aufbewahrung im NVR lässt sich dafür kürzen.
- Ton aufzunehmen ist besonders heikel; TitanOS nimmt Ton nur auf, wenn er je Kamera eingeschaltet ist.

Das ist keine Rechtsberatung. Im Zweifel die Landesdatenschutzbehörde fragen.

## Bekannte Grenzen

- Live ist Bewegt-JPEG über die Weboberfläche, nicht WebRTC; H.265-Aufnahmen spielt nicht jeder Browser ab.
- ffmpeg läuft als Kindprozess von titand; eine eigene gehärtete Einheit folgt.
- Keine Objekterkennung, kein Ton im Live-Bild, kein Gegensprechen.
- An echten Kameras ist das meiste noch nicht geprüft (siehe Release-Notizen).
