# Docker-Apps im Fenster: Programme, Browser-Sperre, automatische Namen, Zertifizierungsstelle

PLAN 61 (Desktop-Modus, Entscheidungen des Projektinhabers vom 6.11.2026, 61-1 bis 61-9).
Der Arbeitsplatz zeigt jede Docker-App mit WebUI in einem Fenster: ein iframe ihres
HTTPS-Namens (`https://immich.nas.local`, siehe `docs/APP-PROXY.md`). Dieses Dokument
beschreibt den Teil im App-Proxy und in titand; Fenster und Arbeitsplatz selbst
stehen in der Dokumentation des Desktop-Modus.

## Einbetten nur durch TitanOS

Der Proxy (`appproxy/frame.go`) ändert jede Antwort einer App:

- `X-Frame-Options` fällt weg.
- In **jeder** Richtlinie von `Content-Security-Policy` (mehrere Zeilen oder durch Komma
  getrennte Richtlinien), die `frame-ancestors` nennt, wird die Anweisung ersetzt; nennt keine
  sie, kommt eine eigene Richtlinie `frame-ancestors …` dazu. Alle anderen Anweisungen bleiben.
  `Content-Security-Policy-Report-Only` wird dort ersetzt, wo es die Anweisung gibt, aber nie
  ergänzt.
- Erlaubt sind nur `'self'` und die https-Adressen, unter denen die Weboberfläche von TitanOS
  erreichbar ist: Hostname und `<hostname>.local`, Namen des Let's-Encrypt-Zertifikats, eines
  eigenen Zertifikats, des dynamischen DNS und des Tailnets sowie die Adressen des NAS (IPv6 in
  Klammern, mit Port, wenn die Weboberfläche nicht auf 443 läuft). **Nie `*`.** Die Liste kommt
  nur aus dem RAM und wird höchstens einmal je Minute neu gebildet.

Grenze: Weboberflächen anderer Cluster-Knoten stehen nicht in der Liste; ihr Arbeitsplatz kann
die Apps dieses NAS nicht im Fenster zeigen („In eigenem Tab öffnen“).

## Programme: Rechte je Konto

Einstellungen › Programme (`/settings/programs`, nur Besitzer/Administratoren). Paket
`appaccess`, Datei `/config/docker/app-access.json` (`newApps`, `apps` → `{users, noGate}`),
beim Start einmal gelesen und im RAM gehalten, geschrieben nur bei Änderung.

- Besitzer und Administratoren dürfen immer alle Apps.
- Ein Benutzer bekommt eine App nur mit Haken. Apps ohne Eintrag folgen „Neue Docker-Apps“:
  „Nur für Besitzer und Administratoren“ (Standard) oder „Für alle Konten“.
- Gespeichert werden nur Konten, die beim Speichern normale Benutzer sind. Geprüft wird
  jedes Mal gegen das aktuelle Konto: gelöschte oder gesperrte Konten verlieren den Zugang
  sofort.
- Je App „Anmeldung im Browser prüfen“ (ab Werk an, `noGate` schaltet es aus).

## Browser-Sperre im Proxy

Nur für **Seitenaufrufe im Browser** (`appproxy/gate.go`): `Sec-Fetch-Mode: navigate`
(auch `nested-navigate`) oder `Sec-Fetch-Dest` `document`/`iframe`/`frame`; ohne alle
`Sec-Fetch-*`-Kopfzeilen ein GET/HEAD mit `Accept` mit `text/html`. WebSockets, `fetch`,
Bilder, Skripte und alle Aufrufe der Handy- und TV-Apps (eigene Anmeldung der App) gehen
unverändert durch. Die Sperre schützt die Seiten der App, nicht ihre Schnittstelle.
Achtung: Apps auf Handy oder Fernseher, die die **Webseite** der App in einer WebView zeigen
(z. B. Jellyfin für Android, die Home-Assistant-App), senden ebenfalls Seitenaufrufe und werden
über den HTTPS-Namen gesperrt. Für sie die Prüfung je App ausschalten oder den Port der App
verwenden. Ein Service Worker (PWA) kann eine zwischengespeicherte Seite ohne Prüfung zeigen;
die Aufrufe der Seite an die App gehen ohnehin durch.

1. Die Weboberfläche holt `POST /api/v1/apps/{app}/window` und lädt die Adresse
   `https://<app>/.titanos/enter?t=<token>` ins Fenster. Das Token ist ein HMAC (Schlüssel
   `/config/tls/app-gate.key`, einmal erzeugt) über Konto, App und Ablauf, gilt **60 s** und
   **nur einmal** (verbrauchte Tokens im RAM).
2. `/.titanos/enter` auf dem App-Namen prüft Token und aktuelles Recht, setzt das Cookie
   `__Host-titanos_app` (HttpOnly, Secure, SameSite=None, Partitioned, Path=/, 12 h, HMAC über
   Konto, App, Ablauf) und leitet auf `/` weiter. Antwort mit `Cache-Control: no-store` und
   `Referrer-Policy: no-referrer`; der Pfad erreicht die App nie.
3. Jeder weitere Seitenaufruf braucht ein gültiges Cookie **und** das aktuelle Recht. Ohne
   kommt Status 403 mit einer kleinen Seite „Diese App über TitanOS öffnen“ (Deutsch und
   Englisch, im Fenster darstellbar) mit Link auf `GET /api/v1/apps/{app}/open?host=<name>`
   der passenden Weboberfläche.

Wie bisher erreicht keine TitanOS-Anmeldung die App: Cookies `titanos_*`, `__Host-titanos*`
und `__Secure-titanos*` (also auch das Sperr-Cookie) werden entfernt, und Apps können sie
nicht setzen. Ziele außerhalb von Docker haben keine Rechte und keine Sperre; Namen im
Tailnet (`ts.net`) einer App sind gesperrt wie ihre anderen Namen.

Links auf HTTPS-Namen im Docker-Bereich gehen deshalb über `/api/v1/apps/{app}/open`, das mit
frischem Token weiterleitet. Lesezeichen auf die App-Adresse landen beim ersten Aufruf auf der
Sperrseite, ein Klick genügt. Grenze: Safari und andere Browser trennen Cookies aus Fenstern
(iframe) von denen eines eigenen Tabs, wenn App und Weboberfläche nicht dieselbe Site sind;
„In eigenem Tab“ holt deshalb ebenfalls ein Token.

## Automatische HTTPS-Namen

Mit eingeschaltetem Proxy bekommt jede Docker-App (Compose-Projekt), deren Container ein
`titanos.webui`-Label mit Port auf dem NAS trägt, einen Namen `<app>.<lokaler Name>.local`
bzw. `<app>.<domain>` (nur Heimnetz, Tailscale, NetBird). Schalter „Automatisch für alle Apps
mit WebUI“ unter Einstellungen › HTTPS-Namen für Apps, ab Werk an (`noAuto` in
`/config/docker/proxy.json` nur, wenn ausgeschaltet).

- Die Namen leben **nur im RAM**; titand fragt Docker alle 30 s (nur wenn Docker antwortet)
  und veröffentlicht neu nur bei Änderung. Entfernte Apps verlieren ihren Namen.
- Eine eigene Route der App in `proxy.json` gewinnt immer, auch eine ausgeschaltete. Belegte
  Namen werden mit `-2`, `-3` … umgangen.
- Abschalten oder Bearbeiten eines automatischen Namens auf der Seite macht ihn zu einer
  eigenen Route. Container ohne Compose-Projekt und WebUIs auf anderen Rechnern bekommen
  keinen automatischen Namen.

## TitanOS-Zertifizierungsstelle

Ein iframe zeigt keine wegklickbare Zertifikatswarnung. Ohne echtes Zertifikat stellt
TitanOS deshalb die Zertifikate seiner Namen selbst aus (`tlscert/ca.go`):

- Beim ersten Bedarf entsteht eine Zertifizierungsstelle „TitanOS <hostname> CA“
  (ECDSA P-256, 10 Jahre, `IsCA`, nur Zertifikate signieren). Schlüssel und Zertifikat liegen
  in `/config/tls/ca-key.pem` und `ca-cert.pem`. Ist nur eine der Dateien da oder der Stick
  nicht lesbar, wird nie eine neue darübergeschrieben; dann bleibt es beim selbst signierten
  Zertifikat.
- Reihenfolge: eigenes Zertifikat (deckt es den Namen) > Let's Encrypt > Tailscale >
  TitanOS-Zertifizierungsstelle > selbst signiert (nur, wenn die Zertifizierungsstelle fehlt).
- Zwei ausgestellte Zertifikate: Weboberfläche (Hostname, `.local`, `localhost`, Adressen des
  NAS) und Apps (die Platzhalter, Kurz- und vollen Namen wie bisher). Sie gelten 397 Tage
  (unter der Grenze der Browser), liegen im RAM und in `/config/tls/ca-leaf-ui.pem` bzw.
  `ca-leaf-apps.pem` und bleiben über Neustarts gleich, damit eine Browser-Ausnahme von vor der
  Installation hält. Geschrieben wird nur beim Ausstellen: wenn sich die Namen ändern, 30 Tage
  vor Ablauf, oder für die Weboberfläche, wenn ein Gerät eine Adresse des NAS anspricht, die
  noch nicht darin steht. Keine Schreibzugriffe je Anfrage oder nach Zeitplan.
- Der private Schlüssel verlässt `/config` nie: nicht über API oder MCP, nicht im
  Diagnosepaket (Positivliste `configFiles`, Test `TestBundleLeavesOutKeys`), `tls` ist im
  Stick-Dateimanager ausgeblendet. Bewusste Ausnahme: die Sicherungen des Sticks enthalten
  `/config` vollständig, damit eine Wiederherstellung die Zertifizierungsstelle behält.
- Download (jedes angemeldete Konto): `GET /api/v1/certs/ca.crt` (PEM) und `ca.cer` (DER),
  `application/x-x509-ca-cert`, als Anhang. Die Seite HTTPS-Namen für Apps erklärt die
  Installation für Windows, macOS, iPhone/iPad (einschließlich
  „Zertifikatsvertrauenseinstellungen“), Android und Linux.
- Wer zuvor dem selbst signierten Zertifikat im Browser eine Ausnahme gegeben hat, sieht nach
  dem Update einmal eine neue Warnung, bis die Zertifizierungsstelle installiert ist (danach
  bleibt das neue Zertifikat über Neustarts gleich).

## API

Nur Web-Oberfläche, nicht in `publicRoutes` (kein Token, kein MCP):

- `GET /api/v1/apps/access` (Besitzer/Admin) → `{newApps: "admins"|"everyone", apps: [{app, icon, url, webui, running, gate, users}], accounts: [{name, role, disabled}]}`
- `PUT /api/v1/apps/access` (Besitzer/Admin) mit `{newApps, apps: [{app, gate, users}]}`; Apps, die fehlen, folgen danach `newApps`. Fehler `app_access_invalid` (400).
- `GET /api/v1/apps/mine` (jedes Konto) → `{apps: [{app, icon, url, webui, running}]}`: Apps mit HTTPS-Namen oder WebUI-Label, die das Konto nutzen darf; `url` ist der HTTPS-Name (`""` ohne).
- `POST /api/v1/apps/{app}/window` (jedes Konto) → `{url, expires}`; `url` = `https://<name>/.titanos/enter?t=<token>`, `expires` RFC 3339 (60 s). Fehler `app_forbidden` (403), `proxy_no_route` (409).
- `GET /api/v1/apps/{app}/open?host=<name>` → 303 zur Eintrittsadresse (auf `host`, wenn das ein Name der App ist); ohne Anmeldung 303 auf `/`, ohne Recht 403.
- `GET /api/v1/certs/ca` → `{available, name, fingerprint, notAfter}`; `GET /api/v1/certs/ca.crt`, `GET /api/v1/certs/ca.cer`.

## Prüfstand und Grenzen

Geprüft mit Go-Tests (`appaccess`, `appproxy/gate_test.go`, `tlscert/ca_test.go`,
`api/app_access_test.go`, `diagnose/secrets_test.go`) und Node-Tests
(`web/tests/appaccess.test.mjs`): Kopfzeilen, Sperre mit Token-Ablauf, einmaligem Token,
entzogenem Recht, gesperrtem/gelöschtem Konto und Nicht-Browser-Aufrufen, automatische
Namen, Zertifikatskette bis zur Zertifizierungsstelle. Nicht geprüft: echte Browser
(Partitioned-Cookies in Safari/Firefox/Chrome im iframe), echte Apps (Immich, Jellyfin,
Vaultwarden, Paperless, Home Assistant, Nextcloud) und die Installation der
Zertifizierungsstelle auf echten Geräten. Apps, die per JavaScript `window.top` prüfen,
lassen sich nicht zwingen. Ein neu angelegtes Konto mit dem Namen eines gelöschten erbt
dessen gespeicherte Haken bis zum nächsten Speichern der Seite.
