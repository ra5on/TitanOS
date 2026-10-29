# Eigener HTTPS-Name je Docker-App und weitere Ziele

Eine Docker-App kann zusätzlich zu `http://<NAS>:<Port>` unter einem eigenen
Namen erreichbar sein, z. B. `https://immich.nas.local`. Entscheidung des
Projektinhabers: Weiterleitung durch titand selbst, Namen per mDNS (avahi),
optional eine eigene Domain mit Platzhaltereintrag im DNS des Routers. Die
Weiterleitung ändert keine Firewall-Regeln. Ist die
[Firewall](FIREWALL.md) eingeschaltet, entscheidet ihr Dienst „HTTPS-Namen für
Apps“ je Zone zusätzlich zu den erlaubten Netzen der Route (gleicher Port wie
die Weboberfläche; titand unterscheidet am Namen). Ein Zertifikat von Let's
Encrypt für `*.<domain>` holt auf Wunsch Einstellungen › Let's Encrypt
(`docs/LETSENCRYPT.md`).

Unabhängig davon kann eine App unter „Öffentlich über IPv64“ im App-Editor über
das CDN von IPv64.net aus dem Internet erreichbar werden (Reverse Proxy von IPv64
mit HTTPS direkt an den Port der App, nicht über den Proxy von titand; nie für
die Ports der Weboberfläche). Einzelheiten und Warnungen: `docs/IPV64.md`.

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
Namen. Der App-Editor zeigt dieselben Felder für seine App; die Einstellungen
„für alle Namen“ gibt es nur hier.

**Für alle Namen**

- **Hauptschalter** „HTTPS-Namen eingeschaltet“: nimmt alle Namen auf einmal vom
  Netz (sie gehen dann an die Weboberfläche), die Einstellung je App und Ziel
  bleibt erhalten.
- **.local-Namen per mDNS bekannt geben** (ab Werk an). Aus: keine
  `.local`-Namen und keine Kurznamen, nichts wird veröffentlicht, avahi läuft
  dafür nicht; erreichbar bleiben nur Namen unter der eigenen Domain. Ohne beides
  warnt die Seite.
- **Lokaler Name**: steht vor `.local` (`apps` → `immich.apps.local`), ein bis
  drei DNS-Teile, ein angehängtes `.local` wird weggelassen. Leer = Name des NAS.
  Ein neuer Name führt beim nächsten Aufruf zu einem neuen App-Zertifikat.
- **Eigene Domain (für alle Apps)**, z. B. `home.example.org`.
- Eine Erklärung zu mDNS, Router-DNS und Zertifikat mit Link zu Zugang › Zertifikat.

**Je App und je Ziel** (Liste mit Ein/Aus, Adressen, Ziel, Reichweite,
Bearbeiten-Dialog, Link in den App-Editor, Entfernen mit einfachem
Bestätigungsdialog)

- **Weitere Namen** (bis 8), z. B. `fotos` und `bilder` für Immich. Jeder Name
  und weitere Name gilt für genau eine App oder ein Ziel.
- **Kurzname** `<name>.local` ohne den lokalen Namen dazwischen (ab Werk aus, nur
  mit mDNS). `.local` gilt im ganzen Heimnetz; heißt ein anderes Gerät genauso,
  benennt avahi einen davon um. Ein Kurzname, der dem Namen des NAS bzw. dem
  lokalen Namen entspräche, wird abgelehnt.
- **Erreichbar aus**: Heimnetz/Tailscale/NetBird (ab Werk), **nur eigenen
  Netzen** (bis 32 CIDR oder einzelne Adressen, IPv4/IPv6) oder allen Netzen.
  Maßgeblich ist die tatsächliche Gegenstelle, nie eine Weiterleitungs-Kopfzeile.
- **Zertifikat der App prüfen** (nur bei HTTPS zur App, ab Werk aus): ohne
  Prüfung wird jedes Zertifikat angenommen; mit Prüfung nur eines einer
  anerkannten Stelle für genau diese Adresse.

**Weitere Ziele** außerhalb von Docker: VMs, LXC-Container oder Geräte im
Heimnetz (z. B. `ha` → `192.168.1.30:8123`). Adresse als IP oder Hostname.
titand verbindet sich nur mit Adressen im Heimnetz, über Tailscale oder NetBird
(`netguard.Local`, geprüft bei jeder Verbindung an der tatsächlich gewählten
Adresse, auch nach DNS-Auflösung) und nie mit den Ports von titand selbst auf dem
NAS. Öffentliche Ziele werden abgelehnt.

**Volle Domains je Route** (Feld `domains`, bis 8 je Route): Neben
`<name>.<suffix>` antwortet eine App oder ein Ziel auf ganze Hostnamen wie
`fotos.example.org`. Jede Domain gehört genau einer Route (über Apps und Ziele
eindeutig), mindestens zwei Teile, keine Platzhalter, nie `.local`. Eine volle
Domain wird vor `<name>.<suffix>` erkannt. Gesetzt werden sie im Feld „Eigene
Domains“ des App-Editors bzw. des Bearbeiten-Dialogs dieser Seite (seit 0.34.0).
Auf das NAS zeigen muss der Name im DNS selbst, z. B. über Dynamisches DNS. Mit
IPv64.net erledigt beides auch Einstellungen › Dynamisches DNS › IPv64 ›
Zuweisungen (`docs/IPV64.md`): Es schreibt in dasselbe Feld `domains` (keine
zweite Liste) und nimmt den Namen in dynamisches DNS auf; Domains des Kontos, die
hier von Hand stehen, zeigt es als zugewiesen. Formulare ohne `domains`
behalten die gespeicherten. Die Adressen der
Route nennen sie (`urls`).

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

1. Deckt das Zertifikat von Let's Encrypt den Namen ab (Option „Auch HTTPS-Namen
   der Apps abdecken“, Platzhalter `*.<domain>`), wird es verwendet.
2. Sonst das importierte eigene Zertifikat, wenn es den Namen abdeckt (z. B. ein
   Platzhalterzertifikat `*.home.example.org`).
3. Sonst ein eigenes selbst signiertes Zertifikat für Apps, das nur Platzhalter
   nennt: `*.<hostname>.local` und ggf. `*.<domain>`
   (`/config/tls/apps-cert.pem`, `apps-key.pem`). Es bleibt gleich, wenn Apps
   dazukommen oder gehen; neu erzeugt wird es nur, wenn sich Hostname oder Domain
   ändern oder es in weniger als 30 Tagen abläuft. Gelesen wird es beim ersten
   Aufruf eines App-Namens nach dem Start, danach nur aus dem RAM. Schlägt das
   Speichern fehl, gilt es bis zum nächsten Start aus dem RAM. Volle Domains
   der Routen stehen darin einzeln.

**Volle Domains bei Let's Encrypt:** Mit „Auch HTTPS-Namen der Apps abdecken“
(nur DNS-01) nimmt Let's Encrypt die vollen Domains eingeschalteter Routen in
die SAN-Liste auf (`letsencrypt.Manager.AppNames`, höchstens 100 Namen), außer
ein Platzhalter der Liste deckt sie schon ab. Weil ein Name, den der
DNS-Anbieter nicht beweisen kann, das ganze Zertifikat scheitern ließe, nur bei
IPv64 als Anbieter oder für Namen unter der App-Domain bzw. einem Namen des
Zertifikats. Ändern sich die Routen, sieht Let's Encrypt gleich nach
(`NamesChanged`).

Browser verlangen die Ausnahme für selbst signierte Zertifikate je Name. Wer das
vermeiden will, lässt unter Einstellungen › Let's Encrypt ein Zertifikat für
`*.<domain>` holen, importiert unter Einstellungen › Zugang ein eigenes
Platzhalterzertifikat oder vertraut dem App-Zertifikat im Betriebssystem.

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
und unverändert lassen (beim Rückweg auf eine App-Version vor 0.29.0 gibt es dann
einfach keine HTTPS-Namen):

```json
{
  "domain": "home.example.org",
  "localName": "apps",
  "apps": {
    "immich": { "enabled": true, "name": "immich", "aliases": ["fotos"], "short": true, "port": 2283, "localOnly": true, "networks": ["192.168.1.0/24"] },
    "vault": { "enabled": true, "name": "vault", "port": 8443, "scheme": "https", "verifyTLS": true }
  },
  "targets": {
    "ha": { "enabled": true, "name": "ha", "host": "192.168.1.30", "port": 8123, "localOnly": true }
  }
}
```

Alles außer `apps` mit `enabled`/`name`/`port` ist optional. Felder seit 0.30.0:
`localName`, `noMDNS`, `off`, `targets` sowie je Route `aliases`, `short`, `host`
(nur Ziele), `verifyTLS`, `networks`, `paused`, dazu `domains` (volle Domains). Rücksicht auf 0.29.0, das die
neuen Felder nicht kennt:

- **Hauptschalter aus:** `off: true`, und jede eingeschaltete Route steht als
  `enabled: false, paused: true` auf dem Stick. 0.29.0 sieht sie dadurch aus,
  nicht an; diese Version liest sie wieder als eingeschaltet.
- **Eigene Netze** setzen auf dem Stick zusätzlich `localOnly: true`, die
  nächstliegende Regel, die 0.29.0 kennt.
- **Ziele** stehen in einer eigenen Liste `targets`, damit 0.29.0 sie nie als App
  an `127.0.0.1:<Port>` weiterleitet.
- Ältere titand ignorieren `domains` (die vollen Domains antworten dann mit
  der Weboberfläche) und verlieren sie beim Speichern der Datei.
- 0.29.0 ignoriert die übrigen Felder (weitere Namen, Kurznamen, lokaler Name,
  mDNS aus, Zertifikatsprüfung) und verliert sie, wenn es die Datei speichert.

## API

Nur Besitzer/Administratoren, jede Änderung im Aktivitätsprotokoll:

- `GET /api/v1/docker/proxy` → `{domain, localName, defaultLocalName, mdns, off, suffixes, apps: [...], targets: [...]}`; je Route `{app, enabled, name, aliases, short, host, port, scheme, verifyTLS, localOnly, networks, domains, urls}` (bei Zielen ist `app` die Kennung)
- `PUT /api/v1/docker/proxy` mit `{domain?, localName?, mdns?, off?}`; nicht gesendete Felder bleiben unverändert (ältere Seiten schicken nur `domain`)
- `PUT /api/v1/docker/proxy/apps/{app}` und `PUT /api/v1/docker/proxy/targets/{id}` mit der Route (ohne `domains` bleiben die gespeicherten, `[]` leert sie)
- `DELETE /api/v1/docker/proxy/apps/{app}`, `DELETE /api/v1/docker/proxy/targets/{id}`

Fehler: `proxy_invalid` (400), `proxy_name_taken` (409). `GET /api/v1/docker`
liefert je Container `proxyUrl`.

## Zertifikate bei Kurznamen

Ein Platzhalter direkt unter `.local` (`*.local`) taugt nicht; deshalb nennt das
App-Zertifikat Kurznamen einzeln (`immich.local`). Kommt ein Kurzname dazu, wird
das selbst signierte App-Zertifikat neu erzeugt, und Browser fragen für jeden
Namen erneut nach der Ausnahme.

## Namen im Tailnet (seit 0.30.0)

Eine App oder ein Ziel mit **„Über Tailscale erreichbar“** wird ein eigenes
Gerät im Tailnet, mit Namen `<name>.<tailnet>.ts.net` (wählbar über
`tailscaleName`, sonst der HTTPS-Name) und einem Zertifikat, das Tailscale selbst
von Let's Encrypt holt und erneuert (unabhängig vom ACME-Client von TitanOS).

- `internal/appproxy/tailnet.go`. Je Route ein `tailscaled` aus dem Plugin
  (`/usr/sbin/tailscaled`) als transiente systemd-Einheit
  `titanos-ts-<app|target>-<name>` (`systemd-run --collect`, Neustart bei
  Fehlern), mit `--tun=userspace-networking` (keine Schnittstelle, keine
  Firewall-Regeln), `--port=0`, `--socket=/run/titanos-ts-names/<id>.sock` und
  `--statedir=/mnt/<plugin-volume>/.titanos/tailscale-names/<id>` (0700, root,
  wie beim Plugin geprüft). Die Einheit überlebt einen titand-Neustart.
- Anmelden nur, solange das Gerät keinen Schlüssel hat, und nur mit dem
  gespeicherten Auth-Key (`/config/docker/proxy-tailscale.key`, über die API nur
  `keySet`), übergeben als `--auth-key=file:` aus einer RAM-Datei, die danach
  gelöscht wird; `--accept-dns=false --accept-routes=false --ssh=false`.
- `tailscale serve --bg --https=443 https+insecure://127.0.0.1:<HTTPS-Port>`:
  Serve terminiert HTTPS und reicht an den Proxy von titand weiter. Serve behält
  den Host-Kopf (`ipn/ipnlocal/serve.go`), setzt `X-Forwarded-*` und
  `Tailscale-User-*`. titand ordnet die Anfrage über den gemeldeten
  `Self.DNSName` der Route zu und nimmt `ts.net`-Namen **nur von Loopback** an
  (sonst 421): Wer aus dem Heimnetz direkt danach fragt, umginge die
  Zugriffsregeln des Tailnets. Für diese Anfragen gelten die Regeln des Tailnets
  statt „nur Heimnetz“/eigener Netze; auf allen anderen Wegen werden
  `Tailscale-User-*` und `Tailscale-App-Capabilities` entfernt.
- Abgleich beim Start, nach jeder Änderung und jede Minute: ausgeschaltete Route
  oder Hauptschalter aus → Einheit anhalten, Schlüssel bleibt; „Über Tailscale“
  abgewählt oder Route entfernt → `tailscale logout`, anhalten, Zustandsordner
  löschen; Plugin nicht aktiv → nichts starten. Zustände `starting`, `login`
  (kein Auth-Key), `running`, `https-off` (HTTPS-Zertifikate im Tailnet aus),
  `stopped`, `error` im RAM, über die API je Route als `tailnet`.
- Felder in `proxy.json`: `tailscale`, `tailscaleName` je Route (optional).
  API: `PUT /api/v1/docker/proxy` mit `{tailscaleKey}` (`""` entfernt ihn),
  `GET` liefert `tailnet: {available, keySet}`.
- Entwicklungsmodus startet keine Instanzen.

### Mit einem Klick ins Tailnet

Entscheidung des Projektinhabers vom 29.10.2026, angelehnt an Unraid:
**„Im Tailnet veröffentlichen“** im Kontextmenü einer verwalteten App bzw.
eines Stack-Mitglieds der Docker-Übersicht, als Knopf im Kopf eines Stacks und
im Kopfbereich des App-Editors (nur Besitzer/Administratoren, wie die übrigen
Proxy-Einstellungen). `internal/appproxy/oneclick.go`.

- **Ein Klick** schaltet für die App „Über Tailscale erreichbar“ ein. Ohne
  Route legt er eine an: HTTPS-Name aus dem App-Namen als DNS-Teil
  (`DNSLabel`: Kleinbuchstaben, alles außer `a–z`, `0–9`, `-` wird zu `-`,
  höchstens 63 Zeichen, leer → `app`; ist der Name vergeben, `-2`, `-3` …),
  „nur Heimnetz“ an, Port = WebUI-Port des Hauptdienstes (`docker.primaryServices`
  bzw. die automatische Wahl der Übersicht, sonst `titanos.webui` eines
  anderen Dienstes, sonst der erste veröffentlichte TCP-Port; schickt die Seite
  keinen, ermittelt titand ihn genauso aus den Containern der App). Eine
  vorhandene Route behält Namen, Port und weitere Einstellungen und wird
  eingeschaltet. Der Name im Tailnet ist der DNS-Teil des App-Namens (bei
  Kollision mit einem anderen Gerät `-2` …) und lässt sich danach wie bisher im
  App-Editor bzw. unter Einstellungen › HTTPS-Namen für Apps ändern.
- Hat erst der Klick die Route eingeschaltet, trägt sie `tailnetOnly: true`
  (ältere titand ignorieren das Feld). Speichern der Route in einem Formular
  entfernt die Markierung; die Route gehört dann dem Nutzer.
- Der Dialog fragt nur, was fehlt: ohne gespeicherten Auth-Key einmalig ein
  Feld dafür (Link auf `login.tailscale.com/admin/settings/keys`, Hinweis
  „Reusable“ an, „Ephemeral“ aus, Tag optional; gespeichert wie bisher, nur
  „ist gesetzt“). Ist das Plugin nicht installiert oder nicht aktiv, erklärt er
  das und verlinkt die Plugins; ist der Hauptschalter aus, verweist er auf die
  Einstellungen. Danach zeigt er die Schritte (Instanz startet, meldet sich an,
  Zertifikat, Adresse antwortet) bis zur Adresse mit Kopieren/Öffnen. Meldet
  das Gerät `https-off`, zeigt er den Ein-Klick-Link des Plugins
  („HTTPS im Tailnet einschalten“, `tailscale/cert.go`), sonst einen Link auf
  die DNS-Einstellungen bei Tailscale. Schließen bricht nichts ab.
- **Erreichbarkeit:** `POST …/tailnet/check` ruft `https://<name>.ts.net/`
  (HEAD) über das Tailscale des NAS an der Tailnet-Adresse des Geräts auf
  (IPv4 zuerst), mit SNI und Zertifikatsprüfung gegen die Systemwurzeln; jede
  HTTP-Antwort gilt als erreichbar, 502 heißt „die App antwortet nicht“. Der
  Dialog fragt alle fünf Sekunden, nach zwölf Fehlschlägen zeigt er die Adresse
  mit dem Hinweis auf die Zugriffsregeln des Tailnets (das NAS selbst muss das
  Gerät erreichen dürfen).
- **„Aus dem Tailnet nehmen“** (einfacher Bestätigungsdialog): „Über
  Tailscale“ aus; der Abgleich meldet die Instanz ab (`tailscale logout`), hält
  sie an und löscht ihren Zustandsordner. Eine Route mit `tailnetOnly` wird
  ausgeschaltet, eine eigene HTTPS-Name-Route bleibt an. Das Gerät bleibt in der
  Tailscale-Verwaltung aufgeführt, bis der Besitzer es dort entfernt; der Dialog
  sagt das.
- **Links:** `GET /api/v1/docker` setzt `tailnetUrl` an dieselben Container wie
  `proxyUrl`, solange das Gerät `running` ist; die Link-Knöpfe (Docker-Übersicht,
  Stack-Kopf, App-Editor, Dashboard-Menü) zeigen ihn als „ts.net“.
- API (nur Besitzer/Administratoren, Aktivitätsprotokoll mit App und Namen,
  ein mitgeschickter Key als `tailscale-key`, die Prüfung ohne Eintrag):
  `POST /api/v1/docker/proxy/apps/{app}/tailnet` mit `{port?, tailscaleKey?}`,
  `DELETE /api/v1/docker/proxy/apps/{app}/tailnet` (beide antworten wie
  `GET /docker/proxy`), `POST /api/v1/docker/proxy/apps/{app}/tailnet/check` →
  `{url, reachable, status, error}`. Fehler `tailnet_unavailable` (409, Plugin
  nicht aktiv), `tailnet_key_missing` (409), `proxy_off` (409),
  `proxy_no_port` (400), `tailnet_not_ready` (409), sonst wie oben.

## Prüfstand und Grenzen

Geprüft mit Go-Tests (`internal/appproxy`, `internal/tlscert/apps_test.go`,
`internal/api/app_proxy_test.go`, `internal/netguard`, `internal/shares`):
Weiterleitung nach Host-Namen, Entfernen der Sitzungscookies, verworfene
`Set-Cookie`, Weiterleitungs-Kopfzeilen, WebSocket-Upgrade, „Nur Heimnetz“, 502,
Prüfung der Einstellungen, Zertifikatsnamen, importiertes Platzhalterzertifikat,
avahi-Hosts-Datei und Entwicklungsmodus. Seit 0.30.0 zusätzlich
(`internal/appproxy/options_test.go`): lokaler Name, mDNS aus, Kurznamen samt
Zertifikatsnamen und Hosts-Datei, weitere Namen und ihre Eindeutigkeit über Apps
und Ziele, Hauptschalter einschließlich der Sicht von 0.29.0 auf die Datei, eigene
Netze (auch IPv4 in IPv6), Ziele samt Ablehnung öffentlicher Adressen und der
Ports von titand bei jeder Verbindung, Zertifikatsprüfung gegen ein selbst
signiertes Zertifikat. Volle Domains (`TestFullDomains`: Erkennung, Eindeutigkeit,
Prüfung, Behalten ohne Feld, Zertifikatsnamen, Neustart, Hauptschalter) und ihre
Aufnahme bei Let's Encrypt (`TestAppNamesJoinTheCertificate`). Ein-Klick ins
Tailnet (`internal/appproxy/oneclick_test.go`, `internal/api/app_tailnet_test.go`)
mit nachgebautem systemctl/systemd-run/`tailscale`: Anlegen samt Namensbildung
und Kollisionen, fehlender bzw. ungültiger Auth-Key, Plugin nicht aktiv,
Hauptschalter aus, eigene Route bleibt erhalten, Entfernen mit Logout, Prüfung
der Adresse (nachgebaut), Rechte. Webtest `web/tests/app-proxy.test.mjs`.

Nicht geprüft beim Ein-Klick: ein echtes Tailnet (Anmeldung mit echtem Key,
Zertifikat beim ersten Aufruf, Zugriffsregeln zwischen NAS und App-Gerät, die
echte HTTPS-Prüfung über `tailscale0`) und der Dialog im Browser gegen ein
echtes NAS.

Nicht geprüft: echtes avahi (Neuladen von `/etc/avahi/hosts`, Namenskonflikte im
Netz, ein von avahi umbenannter NAS-Name wie `nas-2.local`), Browser mit echten
Apps (Immich, Nextcloud, Vaultwarden) hinter dem Proxy, eigene Domain mit
Router-DNS und das Verhalten bei mehreren Netzwerkschnittstellen. Apps, die
einen bestimmten Unterpfad oder feste Ursprungsnamen erwarten, brauchen ggf.
eigene Einstellungen (z. B. die externe URL der App auf den HTTPS-Namen setzen).
