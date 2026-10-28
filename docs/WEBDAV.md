# WebDAV

Seit 0.35.0 sind die Freigaben auch per WebDAV erreichbar: als Netzlaufwerk
in der Dateien-App des iPhones („Mit Server verbinden“), im Finder, im
Windows-Explorer oder in Apps wie Infuse – über HTTPS auf dem Port der
Weboberfläche, also auch von unterwegs über den Tailnet-Namen mit gültigem
Zertifikat. **Ab Werk aus.**

## Einrichten

1. Einstellungen › Zugang › **WebDAV einschalten** (Besitzer/Administratoren).
2. Jedes Konto erzeugt unter Mein Konto › **WebDAV** ein eigenes
   WebDAV-Passwort. TitanOS zeigt es genau einmal; ein neues ersetzt das alte,
   Löschen sperrt alle Apps damit aus.
3. In der App: Server `https://<NAS>/dav/`, Benutzer = Kontoname, Passwort =
   WebDAV-Passwort. `/dav/` listet die Freigaben des Kontos, jede Freigabe ist
   ein Ordner (`/dav/<Freigabe>/`).

## Regeln

- Dieselben Rechte wie bei SMB (`access` der Freigabe): „nur lesen“ lehnt
  jede Änderung ab, ohne Zugriff ist die Freigabe nicht zu sehen. Gesperrte
  Konten kommen nicht hinein.
- Alles läuft über `os.Root` der Freigabe (kein Pfad und kein Link führt
  hinaus), interne Dateien (`.recycle`, halbe Uploads) sind unsichtbar und
  nicht anlegbar, geschützte Dateien (VM-Abbilder) nicht änderbar.
- Löschen verschiebt in den Papierkorb der Freigabe, wenn er eingeschaltet
  ist (wie Samba und der Dateimanager), sonst endgültig. Neue Dateien und
  Ordner gehören dem Konto und der Gruppe `users`.
- Verschieben nur innerhalb einer Freigabe; zwischen Freigaben kopiert die
  App selbst. Auf oberster Ebene lässt sich nichts anlegen.
- Anmeldung per HTTP Basic über HTTPS. Das WebDAV-Passwort hat mehr als 128 Bit
  Zufall und liegt nur als SHA-256 in `/config/users/<konto>/webdav.json`
  (wie API-Tokens: Apps schicken es bei jeder Anfrage, ein langsamer
  Passwort-Hash ginge nicht). Kontopasswort und zweiter Faktor kommen nie in
  eine App. Falsche Anmeldungen bremst derselbe Schutz wie die Anmeldung
  (je Adresse und Konto, danach `429` mit `Retry-After`) und sie stehen im
  Journal.
- Die Prüfung der API-Tokens lässt `/dav` durch; WebDAV prüft selbst.
- Sperren (LOCK) nur im RAM von titand.

## Technik

`golang.org/x/net/webdav` (BSD-Lizenz) mit einem eigenen Dateisystem
(`files.Manager.DAV`) und einer virtuellen obersten Ebene der Freigaben
(`api/webdav.go`). Einstellung `/config/system/webdav.json`. API:
`GET`/`PUT /api/v1/system/webdav`, `GET`/`POST`/`DELETE /api/v1/account/webdav`.

## Prüfstand

`api/webdav_test.go` gegen den echten Handler: aus = 404, Kontopasswort
abgelehnt, Liste der Freigaben, Listen ohne interne Dateien, Hochladen,
Lesen, Ordner, Verschieben, Löschen, nichts oben und nichts Internes
anlegbar, kein Ausweg per `..`, fremde Freigabe, nur-lesen-Konto, Bremse bei
falschen Passwörtern. **Nicht geprüft:** echte Clients (iOS-Dateien-App,
Finder, Windows), große Dateien über langsame Verbindungen.
