# Von Link herunterladen

Im Dateimanager lädt TitanOS eine Datei von einer HTTP(S)-Adresse direkt in den
geöffneten Ordner, etwa ein ISO-Abbild. Der Browser überträgt dabei nichts; der
Download läuft als Hintergrundauftrag in titand weiter, auch wenn die Seite
geschlossen wird.

## Bedienung

- „Neu › Von Link herunterladen“ (Desktop und Smartphone) sowie Rechtsklick auf
  die freie Fläche der Dateiliste. Nur bei Schreibrecht auf den aktuellen
  Speicherort; nicht auf dem Stick und nicht auf einem gesperrten
  USB-Backup-Laufwerk (Bedienungsschutz der Dateiseite).
- Dialog (`UrlDownloadDialog.svelte`): Link (nur `http`/`https`, ohne
  Zugangsdaten in der Adresse), Dateiname und optional die erwartete SHA-256.
  Der Name wird aus dem letzten Pfadteil des Links vorbelegt. „Name und Größe
  abfragen“ (auch beim Verlassen des Linkfelds) fragt den Server
  (`POST /api/v1/files/url-download/probe`); dessen `Content-Disposition`
  ersetzt den Namen, solange er nicht von Hand geändert wurde. Größe, Typ, ein
  vorhandener gleicher Name und zu wenig freier Speicher werden angezeigt. Die
  Abfrage schreibt nichts.
- Start: `POST /api/v1/files/url-download` (`root`, `dir`, `url`, `name`,
  `sha256`, `conflict`). Gibt es den Namen schon, antwortet der Server mit
  `file_exists`; der Dialog bietet dann „Beide behalten“ (`conflict: rename`,
  die neue Datei heißt `name (2).ext`) oder einen anderen Namen an.
  **Ersetzen gibt es nicht.**
- Der Auftrag erscheint in der vorhandenen Auftragsliste der Dateiseite
  (`/files/jobs`, Art `download`) und in „Laufende Aufgaben“: Fortschritt,
  gemessene Geschwindigkeit, Restzeit bei bekannter Größe, Abbrechen,
  Wiederholen und nach dem Ende „Zielordner öffnen“ wie bei Kopieraufträgen.
  Die Abfrage-Adresse wird in der Liste ohne Query-String angezeigt (dort
  stehen oft Zugangstoken).

## Rechte und Netz

- Die Wurzel (Freigabe, Volume, USB) wird bei Start, Abfrage und Wiederholen
  erneut gegen die aktuellen Kontorechte aufgelöst; geschrieben wird nur über
  `os.Root` dieser Wurzel. Registrierte VM-Dateien bleiben geschützt.
- Konten mit Rolle **Benutzer** erreichen nur öffentliche Adressen, dieselbe
  Regel wie `notify.Manager.Confined` (keine Loopback-, privaten,
  Link-local-, Tailscale-/CGNAT-, Dokumentations- und reservierten Bereiche;
  gemeinsame Prüfung in `internal/netguard`). **Besitzer und Administratoren**
  dürfen zusätzlich Adressen im Heimnetz verwenden, nie aber Loopback,
  unspezifizierte oder Multicast-Adressen.
- Geprüft wird die tatsächliche Adresse jeder Verbindung beim Verbindungsaufbau
  (`net.Dialer.Control`), also nach der Namensauflösung und bei jeder
  Weiterleitung; ein Name, der zwischen Prüfung und Verbindung auf eine andere
  Adresse zeigt, hilft nicht. Höchstens zehn Weiterleitungen, nur `http`/`https`
  ohne Zugangsdaten, nach `https` nie wieder `http`. Kein Proxy.
- Nicht im MCP-Werkzeugkatalog und nicht in der öffentlichen API: KI-Clients
  lesen fremde Inhalte; eine eingebettete Anweisung dürfte das NAS nicht dazu
  bringen, beliebige Dateien aus dem Internet oder dem Heimnetz in Freigaben zu
  laden.

## Schreiben, Prüfen, Veröffentlichen

- Geschrieben wird nur in eine versteckte Teildatei
  `.titanos-download-<id>.titanos-part` (root, 0600) im Zielordner, also auf
  dem Ziel-Volume, nie in RAM, Overlay, `/tmp` oder auf den Stick.
- Ist die Größe bekannt (`Content-Length` bzw. `Content-Range`), muss vor dem
  ersten Byte so viel plus 64 MiB Reserve frei sein. Zusätzlich wird vor jedem
  Schreibfenster der freie Platz geprüft, auch bei unbekannter Größe.
- Die Bytes laufen in Fenstern des gemeinsamen Writeback-Budgets
  (`docs/MEMORY.md`, `Acquire` wie bei Browser-Uploads), jedes Fenster mit
  `fsync` und anschließender Seitenfreigabe (`FADV_DONTNEED`). Puffer: 256 KiB.
- Mit erwarteter SHA-256 wird beim Schreiben gehasht (bei einer Fortsetzung
  zuerst der vorhandene Anfang). Stimmt sie nicht, wird die Teildatei verworfen
  und der Auftrag schlägt fehl.
- Danach erhält die Datei das Konto und die Gruppe `users` (0660) und wird per
  `renameat2(RENAME_NOREPLACE)` unter ihrem Namen veröffentlicht. Hat jemand
  den Namen inzwischen belegt, gilt die gewählte Regel: mit „Beide behalten“
  nächste Nummer, sonst Fehler; überschrieben wird nie. `Last-Modified` wird
  nicht übernommen.

## Fortsetzen, Abbrechen, Neustart

- Bricht die Verbindung ab (Netzfehler, unvollständige Antwort, HTTP 5xx/429,
  zwei Minuten ohne Daten), setzt der Auftrag bis zu viermal selbst per HTTP
  `Range`/`If-Range` fort (Wartezeit 2, 4, 8, 16 Sekunden). Grundlage sind nur
  bereits mit `fsync` geschriebene Bytes; ohne starkes `ETag` oder
  `Last-Modified` beginnt er von vorn. Meldet der Server eine andere Fassung,
  beginnt er ebenfalls von vorn.
- Danach ist der Auftrag fehlgeschlagen; die Teildatei bleibt, „Erneut
  versuchen“ setzt dort fort (Rechte und Zielwurzel werden erneut geprüft).
  Gesperrte Adressen, HTTP-4xx, fehlender Platz und Prüfsummenfehler werden
  nicht selbst wiederholt.
- Abbrechen entfernt die Teildatei; ein späteres Wiederholen beginnt von vorn.
  Entfernen eines beendeten Auftrags aus der Liste (oder Verdrängen nach 20
  beendeten Aufträgen je Konto) entfernt auch seine Teildatei.
- **Kein Überleben eines titand-Neustarts:** Die Aufträge liegen nur im RAM.
  Auf Daten-Volumes hinterlegt jeder Download wie ein Browser-Upload eine Notiz
  in `.titanos/uploads/<id>`; die stündliche Upload-Bereinigung entfernt die
  Teildatei nach 24 Stunden ohne Änderung, solange titand den Auftrag nicht
  kennt. Auf Wurzeln ohne Volume (USB) entfernt das Öffnen des Ordners solche
  Teildateien nach 24 Stunden. App-Updates warten wie bei Kopieraufträgen,
  bis laufende Downloads fertig sind (`Busy`).

## Grenzen

- Höchstens drei gleichzeitige Downloads je Konto und acht insgesamt
  (`download_limit`). Keine feste Größengrenze außer dem freien Platz.
- Keine Pause, Priorität oder Geschwindigkeitsbegrenzung wie bei
  Kopieraufträgen; kein FTP, keine Anmeldung bei der Gegenstelle, keine
  Cookies, keine eigenen Kopfzeilen. Zugangsdaten in der Adresse werden
  abgelehnt; Token im Query-String werden gespeichert, solange der Auftrag im
  RAM liegt, aber nicht angezeigt oder protokolliert.
- Komprimierte Abbilder werden nicht entpackt; dafür bleibt der VM-Medienimport
  (`docs/VM-MEDIA.md`) zuständig. Beide verwenden denselben HTTP-Kern
  (`internal/httpfetch`: geprüfter Client, Range/If-Range, Weiterleitungsregeln).

## Prüfungen

- Go (`internal/files/download_test.go`, `internal/httpfetch`,
  `internal/netguard`, `internal/api/file_download_test.go`) mit
  `httptest`-Servern: Veröffentlichung ohne Überschreiben und „Beide behalten“,
  Fortsetzen nach abgerissener Verbindung mit `If-Range`, Wiederholen nach
  HTTP 503, Weiterleitung auf eine private Adresse für Benutzer gesperrt (für
  Administratoren erlaubt), https→http und Weiterleitungsschleife abgelehnt,
  zu wenig Platz, falsche SHA-256, Abbrechen entfernt die Teildatei, Rechte,
  ungültige Namen/Adressen, Limits sowie Aufräumen alter Teildateien.
- Browser: `scripts/test-ui-url-download.mjs` mit isolierten Fixtures
  (`scripts/test-ui-fixtures.mjs`).
- Nicht geprüft: echte Internet-Server, sehr große Dateien auf echter Hardware,
  Verhalten über echte Verbindungsabbrüche von Mobilfunk/VPN.
