# Vorschaubilder, Fotoansicht und Volltextsuche

Diese Seite beschreibt drei Ergänzungen des Dateimanagers: Vorschaubilder, die
das NAS selbst erzeugt, die Fotoansicht eines Ordners und den optionalen
Volltextindex. Pakete: `backend/internal/thumbs`, `backend/internal/fileindex`,
API in `backend/internal/api/file_media.go`.

## Vorschaubilder

`GET /api/v1/files/thumb?root=&path=&size=` liefert ein kleines Bild
(JPEG, bei Transparenz PNG), nie die Datei selbst.

- **Rechte:** Wurzel und Pfad werden wie beim Herunterladen geprüft
  (`fileRoot`, `files.Manager.Open`, `os.Root`, versteckte/interne und geheime
  Pfade). Der Stick bekommt keine Vorschaubilder.
- **Arten:** JPEG, PNG, GIF (erstes Bild), WebP (`golang.org/x/image/webp`),
  PDF (erste Seite über `pdftoppm`), Videos (MP4, M4V, MOV, MKV, WebM, AVI,
  OGV, MTS/M2TS, WMV; ein Bild bei einer Sekunde, bei kürzeren Videos das erste,
  über `ffmpeg`). HEIC und RAW werden nicht unterstützt.
- **Hilfsprogramme:** feste Argumente, die bereits über `os.Root` geöffnete
  Datei als Deskriptor 3 (`/dev/fd/3`), Ausgabe als PNG über eine RAM-Pipe mit
  32 MiB Grenze, 20 Sekunden Frist, keine temporären Dateien. Fehlt `ffmpeg`
  (älteres Abbild, Entwicklungsrechner), zeigt die Oberfläche das Dateisymbol.
- **Grenzen:** Bilder höchstens 64 MiB und 100 Megapixel (geprüft vor dem
  Dekodieren), höchstens zwei Erzeugungen gleichzeitig, gleiche Anfragen
  werden zusammengefasst; wer 15 Sekunden auf einen Platz wartet, bekommt 503.
- **EXIF:** Die Ausrichtung (1–8) von JPEGs wird angewendet. Gelesen werden
  nur die ersten 256 KiB.
- **Größen:** 160, 320, 640 und 1280 Pixel Kantenlänge; Anfragen werden
  aufgerundet. Kleinere Bilder werden nicht vergrößert.
- **Cache:** je Volume unter `/mnt/<volume>/.titanos/thumbs/` über
  `volumedata.Open` (Ordner gehört root, 0700, keine Links, gleiche
  Einbindung). Schlüssel: SHA-256 über Version, Pfad relativ zum Volume,
  Größe, Änderungszeit, Inode und Kantenlänge – jede Änderung der Datei ergibt
  einen neuen Eintrag, Freigabe- und Volume-Ansicht teilen sich den Cache.
  Höchstens 512 MiB je Volume; darüber werden die am längsten nicht gebrauchten
  Bilder entfernt, bis drei Viertel erreicht sind (ein Treffer erneuert die
  Dateizeit höchstens einmal am Tag). USB-Laufwerke bekommen keinen Cache.
  „Vorschaubilder löschen“ unter Einstellungen › Dateisuche leert ihn.
- **Browser-Cache:** `Cache-Control: private, max-age=604800`, `ETag` aus dem
  Schlüssel; die Oberfläche hängt die Änderungszeit an die Adresse.
- **Kein Hintergrund:** Ein Vorschaubild entsteht nur, wenn ein Browser es
  anfordert (Einträge laden erst beim Sichtbarwerden).

## Fotoansicht

Ansicht › „Fotoansicht“ im Dateimanager. `GET /api/v1/files/photos?root=&path=`
listet Bilder und Videos des geöffneten Ordners (nicht rekursiv, höchstens
5.000 Einträge oder 20 Sekunden) und liest bei JPEGs das Aufnahmedatum
(`DateTimeOriginal`, sonst `DateTime`) aus dem Dateianfang. Das Raster sortiert
nach Aufnahmedatum (ersatzweise Änderungszeit) oder Name. Das Großbild nutzt
das 1280er-Vorschaubild, Videos (MP4/WebM/OGV) laufen über den bestehenden
autorisierten Medienstrom. Pfeiltasten, Wischen auf Touchgeräten, Vollbild
über die Fullscreen-API, Escape schließt erst das Großbild.

## Volltextindex

Ab Werk aus. Einstellungen › Dateisuche (nur Besitzer/Administratoren).

### Einstellungen

`/config/files/index.json` (neue Datei, ältere Versionen kennen sie nicht und
bieten einfach keine Inhaltssuche):

```json
{"enabled": true, "volumes": ["data"], "shares": ["Dokumente"], "ocr": false, "time": "03:15"}
```

- `volumes`: ganze Volumes; `shares`: nur der Ordner der Freigabe
  (`/mnt/<volume>/<freigabe>`).
- `ocr`: gescannte PDFs ohne Textebene mit tesseract erkennen (langsam, erste
  fünf Seiten je Datei). Nur wählbar, wenn tesseract vorhanden ist.
- `time`: optional ein täglicher Lauf zu dieser Uhrzeit (Zeitzone des NAS,
  innerhalb einer Stunde danach). **Dieser Lauf weckt ruhende Platten.**
- Gelesen wird die Datei beim Start; danach nur noch bei Änderungen über die
  API geschrieben. Kein zyklischer Zugriff auf `/config`.

API: `GET /api/v1/files/index` (Einstellungen, Stand je Volume, Auswahl),
`PUT /api/v1/files/index`, `POST /api/v1/files/index/run` (`{"volume": ""}` =
alle), `DELETE /api/v1/files/index/{volume}`, `DELETE /api/v1/files/thumbs/{volume}`.
Alle nur für Besitzer/Administratoren.

### Wann ein Lauf beginnt

titand sieht einmal pro Minute nach, liest dabei nur `/proc/self/mountinfo` und
die I/O-Zähler in `/sys/class/block/*/stat` (bei mdadm die der
Mitgliedspartitionen) und weckt so keine Platte. Ein Lauf beginnt:

- wenn auf dem Volume in den letzten drei Minuten andere Zugriffe stattfanden
  und der letzte Lauf mindestens 30 Minuten zurückliegt (die Zähler werden nach
  jedem Lauf neu gesetzt, damit dessen eigene Zugriffe keinen weiteren Lauf
  auslösen),
- zur optionalen täglichen Uhrzeit,
- auf Knopfdruck („Jetzt aktualisieren“, weckt ruhende Platten).

Lässt sich die Plattenaktivität nicht bestimmen, laufen nur Uhrzeit und
Knopfdruck. Läufe für mehrere Volumes kommen nacheinander, in einer einzigen
Goroutine mit 50 ms Pause nach jeder gelesenen Datei.

### Ablauf eines Laufs

- Volume über `volumedata.OpenVolume` festhalten, Index über
  `volumedata.Open(..., "fileindex")`; beide müssen auf derselben Einbindung
  liegen. Datenbank: `/mnt/<volume>/.titanos/fileindex/index.db` (bbolt, 0600,
  ohne Linkverfolgung geöffnet).
- Rekursiv nur die gewählten Ordner, ohne Links. Ausgelassen: alles, was mit
  einem Punkt beginnt (`.titanos`, `.recycle`, Teildateien), Snapshot-Ordner,
  VM-geschützte Dateien.
- Gelesen werden: Text (`.txt`, `.md`, `.csv`, `.log`, `.json`, `.xml`,
  `.html`, `.yaml`, `.ini`, `.conf`, `.tex`, `.srt`, … bis 8 MiB, davon das
  erste MiB), Word/Excel (`.docx`, `.xlsx` bis 32 MiB, `office.Text`), PDF bis
  50 MiB (`office.ReadPDF`, 20 Seiten je Aufruf, höchstens 200 Seiten bzw.
  2 MiB Text). Der Dateiname zählt immer mit.
- Unverändert ist eine Datei bei gleicher Größe, Änderungszeit und Inode; sie
  wird dann nicht gelesen. Nicht lesbare Dateien werden mit ihrem Namen
  aufgenommen und erst nach einer Änderung erneut versucht.
- Änderungen werden in Gruppen bis 64 Dateien oder fünf Sekunden festgeschrieben.
  Abbruch (Dienstende, App-Update, Einstellungsänderung) verliert höchstens die
  offene Gruppe; der nächste Lauf macht weiter. Der Index hält deshalb **kein
  App-Update auf** und zählt nicht zu `update.Working`.
- Gelöschte oder nicht mehr ausgewählte Dateien werden erst nach einem
  vollständigen Durchgang entfernt. Konnte ein Ordner nicht gelesen werden oder
  gab es mehr als zwei Millionen Einträge, bleibt alles stehen.

### Aufbau des Index

- Wörter: Folgen aus Buchstaben und Ziffern, klein geschrieben, 2–48 Zeichen,
  höchstens 20.000 verschiedene je Datei. Umlaute und ß bleiben erhalten.
- Buckets: `files` (Kennung → Pfad relativ zum Volume, Größe, Zeit, Inode,
  Wörter), `paths` (Pfad → Kennung), `postings` (`wort 0x00 kennung`),
  `meta` (Version, nächste Kennung). Eine unbekannte Version wird neu aufgebaut.
- Suche: jedes Wort auch als Wortanfang („rechn“ findet „Rechnungen“), alle
  Wörter müssen vorkommen, höchstens 100.000 Kandidaten je Wort, 1.000 Treffer.

### Suche und Rechte

Der Suchdialog bietet „Im Inhalt suchen“, wenn der Index den Speicherort
abdeckt (`indexed` in `GET /api/v1/files/roots`). `POST /api/v1/files/search`
mit `"content": true` sucht dann im Index:

- Die Wurzel der Anfrage wird wie jede Dateianfrage gegen die **aktuellen**
  Rechte des Kontos aufgelöst. Gesucht wird nur unter dem Ordner dieser Wurzel
  (Freigabe bzw. ganzes Volume für Administratoren) und dem gewählten
  Unterordner; der Index selbst kennt keine Konten.
- Jeder Treffer wird vor der Antwort über `os.Root` der Wurzel erneut
  nachgeschlagen; verschwundene oder ersetzte Dateien und Pfade mit
  Punktordnern fallen weg. Endung, Größe und Datum filtern wie bei der
  Namenssuche.
- USB-Laufwerke und der Stick haben keinen Index (`index_unavailable`).
- Inhalte stehen nur im Index auf dem Volume selbst; Treffer zeigen keinen
  Textauszug.

### Grenzen

- Der Index ist nicht in Echtzeit: Neue Dateien erscheinen erst nach dem
  nächsten Lauf.
- Der Stand in Einstellungen › Dateisuche liegt im RAM und ist nach einem
  Neustart von titand bis zum ersten Lauf unbekannt.
- Btrfs-Spiegel: Die Aktivität wird am Gerät gemessen, von dem das Dateisystem
  eingebunden ist (Schreibzugriffe treffen beide Platten).
- OCR ist langsam und kann bei großen Scans viel CPU brauchen.
- `ffmpeg` gehört ab dieser Version zum Systemabbild; das erfordert ein volles
  Systemrelease. Auf einer älteren Basis bleiben Videos ohne Vorschaubild.

## Prüfstand

Go-Tests: `internal/thumbs` (EXIF-Ausrichtung und -Datum, Transparenz,
fehlendes Hilfsprogramm, PDF über ein nachgebautes `pdftoppm` mit
Deskriptorprüfung, Cache-Schlüssel, Cache auf dem Volume, LRU-Bereinigung),
`internal/fileindex` (Wörter, Hinzufügen/Ändern/Löschen, Filter nach Ordnern,
Bereichswechsel, Einstellungen, Start nur bei Aktivität bzw. zur Uhrzeit,
Index löschen) und `internal/api` (Rechte der neuen Routen). Webtests:
`web/tests/file-media.test.mjs`. Nicht geprüft: echtes `ffmpeg`/`pdftoppm` im
Abbild, große reale Datenbestände, Verhalten mit echten schlafenden Platten.
