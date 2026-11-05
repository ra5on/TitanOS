# Fotos im Dateimanager

Seit 0.53.0 (PLAN 52, Entscheidungen des Projektinhabers vom 5.11.2026). Links im Dateimanager stehen unter „Fotos“ die **Zeitleiste**, die **Alben** und die **Karte**; oben schaltet man zwischen Ordnern und Fotos um.

## Foto-Ordner

- Jedes Konto wählt seine Foto-Ordner selbst aus den Freigaben, die es sieht (höchstens 20). Es gelten die aktuellen Rechte: Fotos aus einer Freigabe, die das Konto nicht mehr öffnen darf, verschwinden aus der Ansicht.
- TitanOS liest die Ordner samt Unterordnern (versteckte wie `.thumbs`, `@eaDir` ausgenommen, höchstens 30.000 Fotos und Videos je Konto), mit Aufnahmedatum, GPS-Position und Kamera aus EXIF (JPEG), sonst dem Änderungsdatum.
- Gelesen wird beim Öffnen, wenn der Stand älter als 10 Minuten ist, nach einer Änderung der Ordner oder auf Knopfdruck; kein Hintergrund-Polling, das die Platten weckt.
- Nichts wird kopiert. Vorschaubilder kommen aus dem vorhandenen Vorschau-Cache (`/files/thumb`).

## Zeitleiste und Suche

- Nach Monaten gruppiert, neueste zuerst; Großansicht mit Pfeiltasten und Wischen, Videos als Stream.
- Neben dem Foto: KI-Beschreibung, Datum, Ort, Kamera, Ordner, Größe; Knöpfe „Zu Album“, „Im Ordner zeigen“, „Mit KI-Agent …“.
- Die Suche geht über Beschreibungen, Dateinamen, Ordner, Monatsnamen und Jahr („Hund am Strand“, „Dezember 2025“, „Telekom-Rechnung“).

## KI-Beschreibungen

- Das Bild-Modell des KI-Agenten (Hauptmodell mit Bildern wie `gemma4:12b`, sonst das eigene Bild-Modell aus Einstellungen › KI-Agent) beschreibt jedes Foto in ein, zwei Sätzen mit Stichwörtern und dem wichtigsten Text im Bild. Personen werden nur gezählt, nicht benannt.
- Voraussetzung: KI-Agent eingeschaltet und für das Konto erlaubt.
- **Der erste Lauf startet per Knopf.** Danach wählt man: nur auf Knopfdruck, nachts (Zeitfenster, ab Werk 01–06 Uhr) oder nachts und sonst immer, wenn kein Chat läuft. Ein laufender Chat hat Vorrang: das Beschreiben wartet.
- Ein Foto nach dem anderen; grob 2–5 Sekunden je Foto mit `gemma4:12b`. Beschreibungen sind höchstens 600 Bytes lang. Fotos über 20 MB und Videos bekommen keine.
- Lehnt Ollama ein einzelnes Bild ab (HTTP 400), bleibt dieses Foto ausgenommen, bis sich die Datei oder das Bild-Modell ändert. Ist Ollama nicht erreichbar oder meldet es einen Server- oder Modellfehler, endet der Lauf nach höchstens drei Versuchen in Folge, ohne Fotos auszunehmen; die Meldung steht in der KI-Leiste.
- Der Index wird während eines Laufs alle drei Minuten und am Ende gespeichert. Ist die Datei nicht lesbar (nicht: fehlt), bleibt sie unverändert liegen und Lesen und Beschreiben halten an.
- Während Ordner gelesen oder Fotos beschrieben werden, fragt die Seite nur den Stand ab (`GET /photos?status=1`), nicht die ganze Liste.

## Alben

- Alben sind Listen von Verweisen (höchstens 5.000 Fotos, 200 Alben je Konto); nichts wird kopiert oder verschoben.
- Teilen mit anderen Konten: Sie sehen das Album, aber nur Fotos aus Freigaben, die sie selbst öffnen dürfen; Fotos aus der Freigabe „KI-Agent“ (jedes Konto sein eigener Ordner) nie. Ändern und löschen kann nur der Besitzer.
- Der KI-Agent legt Alben an und füllt sie (`photo_album_save`), z. B. „Leg ein Album mit den Fotos vom Strandurlaub an“.

## Karte

- Ab Werk aus. Eingeschaltet lädt der **Browser** Kartenbilder von OpenStreetMap (`tile.openstreetmap.org`, mit Namensnennung); TitanOS selbst fragt nicht an. Fotos nah beieinander bilden einen Punkt mit ihrer Zahl.
- Ohne Karte gibt es eine Liste der Orte (GPS-Positionen auf etwa 10 km zusammengefasst).

## KI-Agent

- `photos_search`: findet Fotos über denselben Index (Ort, Datum, Beschreibung); `image_view` sieht sie sich genauer an. Es gelten die Regeln des Agenten: mit „Dateien: verboten“ gar nicht, Fotos in einem verbotenen Ordner nie.
- `photo_album_save`: Album anlegen oder Fotos hinzufügen.

## Daten

| Was | Wo |
|---|---|
| Foto-Ordner, Zeitplan, Karte an/aus | `.titanos/agent/<konto>/photoprefs.json` auf dem Volume des Agenten |
| Index mit Datum, GPS, Beschreibungen | `.titanos/agent/<konto>/photos.json` |
| Alben | `.titanos/agent/<konto>/photoalbums.json` |

Nie auf dem Stick. Beim Rückweg auf 0.52.0 bleiben die Dateien liegen und werden wieder genutzt, wenn man zurückkommt.

## Grenzen

- Personen (Gesichter) erkennt TitanOS nicht; das ist ein späterer Schritt.
- EXIF nur aus JPEG; HEIC und RAW tragen nur das Änderungsdatum.
- Geprüft mit Go-Tests (echte JPEGs mit EXIF, nachgebautes Bild-Modell) und `scripts/test-ui-photos.mjs` (Testdaten, Kartenbilder ersetzt); nicht mit einem echten Bild-Modell und großen Fotosammlungen auf echter Hardware.
