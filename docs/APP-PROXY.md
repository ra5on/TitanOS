# Eigener HTTPS-Name je Docker-App

Eine Docker-App kann zusätzlich zu `http://<NAS>:<Port>` unter einem eigenen
Namen erreichbar sein, z. B. `https://immich.nas.local`. Entscheidung des
Projektinhabers: Weiterleitung durch titand selbst, Namen per mDNS (avahi),
optional eine eigene Domain mit Platzhaltereintrag im DNS des Routers. Es gibt
keine Firewall-Änderung und kein Let's Encrypt.

## Bedienung

Docker › App bearbeiten › Abschnitt „HTTPS-Name“ (nur Besitzer und
Administratoren, nur verwaltete Apps unter `/config/apps/<name>/`):

- **HTTPS-Name einschalten**, **Name** (ein DNS-Teil: `a–z`, `0–9`, Bindestrich,
  nicht am Anfang/Ende, höchstens 63 Zeichen; Vorschlag ist der App-Name),
  **Port der App auf dem NAS** (Vorschlag aus `titanos.webui`, sonst der erste
  veröffentlichte TCP-Port) und wie die App selbst antwortet (HTTP oder HTTPS
  mit eigenem Zertifikat der App).
- **Nur aus dem Heimnetz, über Tailscale oder NetBird erreichbar** (ab Werk an).
- Die Einstellung wird mit „Speichern“ sofort übernommen; die Compose-Datei
  ändert sich nicht und kein Container wird neu erstellt. „HTTPS-Name entfernen“
  vergisst sie. Wird die App entfernt, verschwindet auch ihr HTTPS-Name.
- In der Docker-Übersicht, im App-Editor und im Dashboard-Menü erscheint dann der
  Knopf „HTTPS-Name“ neben WebUI/Tailscale/Öffentlich (siehe `APP-LINKS.md`).

Einstellungen › Netzwerk und Dienste › **HTTPS-Namen für Apps**
(`/settings/proxy`, nur Besitzer und Administratoren, seit 0.30.0) bündelt alle
HTTPS-Namen: je App Ein/Aus, Adressen, Ziel-Port, „nur Heimnetz“, Link in den
App-Editor und Entfernen mit einfachem Bestätigungsdialog. Dort steht auch die
**Eigene Domain (für alle Apps)**, z. B. `home.example.org`, sowie eine
Erklärung zu mDNS, Router-DNS und Zertifikat. Der App-Editor verweist für die
Domain auf diese Seite. Dieselbe API wie unten, keine neuen Felder.

## Ablauf in titand

- Paket `backend/internal/appproxy`. titand bindet weiterhin selbst :443 und :80.
  Vor die Weboberfläche ist `appproxy.Manager.Handler` geschaltet: Fragt eine
  Anfrage (Host-Kopfzeile, ohne Port, Groß-/Kleinschreibung und Schlusspunkt
  egal) nach `<name>.<hostname>.local` oder `<name>.<domain>` einer
  eingeschalteten App, geht sie an `http(s)://127.0.0.1:<Port>`; alle anderen
  Namen (NAS-Name, IP-Adressen, unbekannte Namen) erreichen wie bisher die
  Weboberfläche. Port 80 leitet wie bisher auf HTTPS desselben Namens um.
- `net/http/httputil.ReverseProxy` mit `Rewrite`: vom Client mitgeschickte
  `X-Forwarded-*` werden verworfen und neu gesetzt (`X-Forwarded-For`,
  `-Host`, `-Proto`), der ursprüngliche `Host` bleibt erhalten, damit Apps ihre
  Links richtig bilden. WebSocket-Upgrades werden durchgereicht. Antwortet die App
  nicht, kommt 502. Zu Apps mit eigenem HTTPS verbindet titand ohne
  Zertifikatsprüfung (nur über Loopback).
- **Die TitanOS-Anmeldung erreicht nie eine App:** Cookies `titanos_*`,
  `__Host-titanos*` und `__Secure-titanos*` werden aus jeder weitergereichten
  Anfrage entfernt, und Apps dürfen keine Cookies dieses Namens setzen
  (`Set-Cookie` wird verworfen). Das Sitzungscookie ist ohnehin `__Host-` und
  damit an den NAS-Namen gebunden. Anfragen an App-Namen erreichen nie die
  TitanOS-API, auch nicht unter `/api/`. Weil App-Namen zur selben „Site“ wie
  der NAS-Name gehören (`immich.nas.local` und `nas.local`), schützt die
  vorhandene Cross-Origin-Prüfung (`http.CrossOriginProtection`,
  `Sec-Fetch-Site: same-site` gilt als fremd) und die Origin-Prüfung der
  WebSockets die Weboberfläche vor Seiten einer App. Wie bei
  `api/files.go` (Inhalte nie auf der TitanOS-Herkunft ausliefern) laufen
  fremde Inhalte damit immer unter einer eigenen Herkunft.
- „Nur Heimnetz“: erlaubt sind private (RFC 1918, ULA), Link-local- und
  Loopback-Adressen sowie Tailscale/NetBird (`100.64.0.0/10`,
  `fd7a:115c:a1e0::/48`), sonst 403 (`netguard.Local`). Maßgeblich ist die
  tatsächliche Gegenstelle der Verbindung, keine Weiterleitungs-Kopfzeile.
  Leitet der Router Port 443 mit Absender-NAT weiter, sieht titand die
  Router-Adresse und hält die Anfrage für lokal.
- Ziel ist immer `127.0.0.1:<Port>`. Ein Port, der nur auf einer anderen
  Adresse veröffentlicht ist, ist so nicht erreichbar. Die Ports von titand
  selbst (443, 80) sind als Ziel abgelehnt.

## Zertifikate

`tlscert.Manager.ForNames`, aufgerufen aus `GetCertificate` nur für SNI-Namen
eingeschalteter Apps:

1. Deckt das importierte eigene Zertifikat den Namen ab (z. B. ein
   Platzhalterzertifikat `*.home.example.org`), wird es verwendet.
2. Sonst ein eigenes selbst signiertes Zertifikat für Apps, das nur Platzhalter
   nennt: `*.<hostname>.local` und ggf. `*.<domain>`
   (`/config/tls/apps-cert.pem`, `apps-key.pem`). Es bleibt gleich, wenn Apps
   dazukommen oder gehen; neu erzeugt wird es nur, wenn sich Hostname oder Domain
   ändern oder es in weniger als 30 Tagen abläuft. Gelesen wird es beim ersten
   Aufruf eines App-Namens nach dem Start, danach nur aus dem RAM. Schlägt das
   Speichern fehl, gilt es bis zum nächsten Start aus dem RAM.

Browser verlangen die Ausnahme für selbst signierte Zertifikate je Name. Wer das
vermeiden will, importiert unter Einstellungen › Zugang ein eigenes
Platzhalterzertifikat für die eigene Domain oder vertraut dem App-Zertifikat im
Betriebssystem.

## Namen im Netz

- **`.local` (mDNS):** titand schreibt `/etc/avahi/hosts` (im RAM, bei jedem
  Start aus `/config` neu) mit einer Zeile `<IPv4> <name>.<hostname>.local` je
  eingeschalteter App und lässt avahi neu lesen
  (`systemctl try-reload-or-restart avahi-daemon.service`), nur wenn sich Inhalt
  oder NAS-Adresse ändern (Prüfung alle 30 Sekunden ohne `/config`-Zugriff,
  sofort nach Änderungen). Die Adresse ist die Quelladresse der Route ins
  Internet, sonst die erste private IPv4 einer physischen Schnittstelle oder
  Bridge. Gibt es eingeschaltete App-Namen, läuft avahi auch ohne SMB-Freigabe
  (`shares.State.Discovery`). IPv6 wird nicht veröffentlicht.
- **Eigene Domain:** TitanOS veröffentlicht nichts. Im DNS des Routers (z. B.
  dnsmasq, Pi-hole, AdGuard, Fritz!Box mit eigenem DNS) muss
  `*.home.example.org` auf die NAS-Adresse zeigen. Eine `.local`-Domain wird
  abgelehnt, weil diese Namen schon per mDNS entstehen.
- Der Entwicklungsmodus (`titand -dev`) leitet per Host-Kopfzeile weiter, fasst
  avahi aber nie an.

## Konfiguration

`/config/docker/proxy.json`, eine eigene Datei, die ältere titand nicht kennen
und unverändert lassen (beim Rückweg auf eine ältere App-Version gibt es dann
einfach keine HTTPS-Namen):

```json
{
  "domain": "home.example.org",
  "apps": {
    "immich": { "enabled": true, "name": "immich", "port": 2283, "localOnly": true },
    "vault": { "enabled": true, "name": "vault", "port": 8443, "scheme": "https" }
  }
}
```

`domain`, `scheme` (Standard `http`) und `localOnly` sind optional. Ein Name gilt
für genau eine App.

## API

Nur Besitzer/Administratoren, jede Änderung im Aktivitätsprotokoll:

- `GET /api/v1/docker/proxy` → `{domain, suffixes, apps: [{app, enabled, name, port, scheme, localOnly, urls}]}`
- `PUT /api/v1/docker/proxy` mit `{domain}` (leer entfernt sie)
- `PUT /api/v1/docker/proxy/apps/{app}` mit `{enabled, name, port, scheme, localOnly}`
- `DELETE /api/v1/docker/proxy/apps/{app}`

Fehler: `proxy_invalid` (400), `proxy_name_taken` (409). `GET /api/v1/docker`
liefert je Container `proxyUrl`.

## Prüfstand und Grenzen

Geprüft mit Go-Tests (`internal/appproxy`, `internal/tlscert/apps_test.go`,
`internal/api/app_proxy_test.go`, `internal/netguard`, `internal/shares`):
Weiterleitung nach Host-Namen, Entfernen der Sitzungscookies, verworfene
`Set-Cookie`, Weiterleitungs-Kopfzeilen, WebSocket-Upgrade, „Nur Heimnetz“, 502,
Prüfung der Einstellungen, Zertifikatsnamen, importiertes Platzhalterzertifikat,
avahi-Hosts-Datei und Entwicklungsmodus. Webtest `web/tests/app-proxy.test.mjs`.

Nicht geprüft: echtes avahi (Neuladen von `/etc/avahi/hosts`, Namenskonflikte im
Netz, ein von avahi umbenannter NAS-Name wie `nas-2.local`), Browser mit echten
Apps (Immich, Nextcloud, Vaultwarden) hinter dem Proxy, eigene Domain mit
Router-DNS und das Verhalten bei mehreren Netzwerkschnittstellen. Apps, die
einen bestimmten Unterpfad oder feste Ursprungsnamen erwarten, brauchen ggf.
eigene Einstellungen (z. B. die externe URL der App auf den HTTPS-Namen setzen).
