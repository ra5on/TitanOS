# IPv64.net aus TitanOS verwalten

Der Reiter **IPv64** unter Einstellungen › Dynamisches DNS
(`/settings/ddns?tab=ipv64`, Unterreiter in `&sub=`, nur Besitzer und
Administratoren) verwaltet ein Konto bei IPv64.net über dessen **API v2**, damit die ipv64-Webseite nur noch für
Registrierung, Paket und Abrechnung nötig ist. Grundlage ist ausschließlich die
öffentliche Beschreibung: OpenAPI 3.1 unter
`https://ipv64.net/api/v2/openapi.json` und die Anleitung unter
`https://ipv64.net/api_v2_docs` (Stand 28.10.2026). Was dort nicht steht, setzt
TitanOS nicht voraus.

**Ort:** Bis 0.33 ein eigener Einstellungsbereich, in 0.34.0 entfernt, seit der
Entscheidung des Projektinhabers vom 29.10.2026 wieder da, aber als Reiter von
Dynamisches DNS (kein eigener Eintrag in der Seitenleiste; die
Einstellungssuche findet die Unterreiter unter „Dynamisches DNS › IPv64 › …“).
`/settings/ipv64?tab=<reiter>` leitet auf `/settings/ddns?tab=ipv64&sub=<reiter>`
um. Einstellungen aus der Zeit vor 0.34 (`/config/ipv64/`, `heartbeat` in
`/config/ddns/settings.json`) blieben liegen und gelten wieder. `/config/ipv64/`
ist im Stick-Dateimanager ausgeblendet.

**Nur gegen Nachbauten geprüft.** Alle Tests laufen gegen einen Fake, der die
Formen der OpenAPI-Beschreibung nachbildet (`backend/internal/dnsprovider/ipv64fake`);
gegen das echte IPv64 ist nichts davon abgenommen.

## Grundsätze

- **Ab Werk aus, nichts ohne Klick.** Ohne ausdrückliche Aktion legt TitanOS
  bei IPv64 nichts an; jede anlegende, löschende oder umschaltende Aktion hat
  einen einfachen Bestätigungsdialog, der die Folgen nennt (kein Eintippen von
  Namen).
- **Rechte:** nur Besitzer und Administratoren. Jede Änderung zusätzlich nur aus
  einer angemeldeten Websitzung (`browserOnly`): weder MCP-Clients noch Home
  Assistant noch API-Tokens können bei IPv64 etwas anlegen oder das NAS
  öffentlich machen. Keine dieser Routen steht im MCP-Werkzeugkatalog.
- **API-Key:** ein eigener Key dieser Seite (`/config/ipv64/settings.json`,
  vor dem Speichern mit `GET /me` geprüft) oder der Key von Dynamisches DNS bzw.
  Let's Encrypt. Er wird nur „ist gesetzt“ angezeigt und geht nur als
  `Authorization: Bearer` zu IPv64.
- **Geheimnisse bleiben in titand:** Healthcheck-Tokens werden in der
  Oberfläche zu einer ID (Hash), Push-Adressen, Domain-Update-Tokens, die
  WireGuard-Konfiguration von Peers und der Listen-Token der Blocklist werden
  nie herausgegeben. Integrationen werden nie mit `include_secrets` gelesen; von
  ihren `options` zeigt TitanOS nur Prioritäten, Gerät, Chat-ID, E-Mail/Nummer
  und Discord-IDs. Passwörter von Statusseiten sind nur zum Setzen da.
- **Limits:** alle Aufrufe gehen durch einen gemeinsamen Token-Bucket
  (300, 10 je Sekunde, aus der Anleitung „Limits“), der `RateLimit-Remaining`
  übernimmt und bei 429 alle Aufrufe für `Retry-After` Sekunden anhält.
  Testnachrichten höchstens eine je 30 Sekunden und Check/Integration.
  Fehler (RFC 9457) erscheinen mit dem `detail` von IPv64.
- **Hintergrundänderungen (202):** Bei CDN, Cloud Router und Failover setzt
  IPv64 vieles im Hintergrund um. TitanOS zeigt „angenommen“ und den Zustand
  (`pending`, `state`, `status`) wie geliefert.
- **Aktivitätsprotokoll:** Änderungen erscheinen mit Namen von Domains, Checks,
  Apps usw., nie mit Tokens oder Keys.

## Die Reiter

### 1. Übersicht
Paket mit Laufzeitende (`plan.valid_until`, beim Free-Paket keins), Verbrauch
je Ressource mit Balken (`/me` `usage`), Account-Log mit Cursor-Blättern
(`/me/logs`, `before`/`next_before`). API-Keys: Liste, Anlegen mit Ablauf
(7/30/90/365 Tage oder unbegrenzt mit Warnung), der neue Key wird **genau einmal**
angezeigt und nicht gespeichert; Löschen mit Bestätigung. Keys, die TitanOS
selbst benutzt (eigener Key, dynamisches DNS, Let's Encrypt; ermittelt über
`auth.key_id` von `/me`), sind markiert und werden serverseitig nie gelöscht.

### 2. Domains und DNS
DynDNS-Domains (`<name>.<zone>`) und eigene Domains (danach Nameserver
`ns1.ipv64.net`/`ns2.ipv64.net` beim Registrar) anlegen; je Domain Platzhalter,
ignorierte Adressfamilie, fester IPv6-Präfix, DNS-Einträge anlegen/ändern/
löschen (vom CDN oder Failover verwaltete sind gesperrt). Neuer Update-Token
(Warnung: Router und andere Updater brauchen den neuen; nutzt dynamisches DNS
den alten, übernimmt TitanOS den neuen selbst), Zone zurücksetzen (deutlicher
einfacher Bestätigungsdialog) und Domain löschen (nennt die Folgen). Das Chip
„dyn. DNS“ führt in den Reiter DynDNS.

### 2a. DynDNS
Alles, was Einstellungen › Dynamisches DNS kann, mit IPv64 als festem Anbieter.
Beide Seiten arbeiten auf derselben Datei (`/config/ddns/settings.json`);
Änderungen sind auf beiden sofort sichtbar. Dynamisches DNS bleibt für andere
Anbieter, und nutzt es gerade einen anderen, sagt der Reiter, dass Speichern es
auf IPv64 umstellt.

- **Zustand:** gefundene und eingetragene IPv4/IPv6, letzter/nächster Blick,
  letzter Fehler, „Jetzt aktualisieren“ (`POST /network/ddns/update`).
- **Key:** keine zweite Eingabe. `PUT /api/v1/ipv64/ddns` behält einen Key, den
  dynamisches DNS für IPv64 schon hat; sonst übernimmt es den Key dieses
  Bereichs (eigener, sonst der von Let's Encrypt). Ein reiner Update-Token
  bleibt nutzbar.
- **Namen** (höchstens 8): Liste mit Entfernen, Aufnehmen aus den eigenen
  Domains oder als Name darunter (z. B. `photos.nas.ipv64.de`), „Anlegen und
  aufnehmen“ für eine neue DynDNS-Domain (einfacher Bestätigungsdialog).
- **IPv6-Präfix mitschicken** (keiner, automatisch, fest), IPv4/IPv6,
  Intervall (Standard, 5 Minuten bis 1 Tag), eigener Abfragedienst,
  Schnittstelle.
- **Je Domain:** Platzhalter, ignorierte Adressfamilie und fester IPv6-Präfix
  (sofort bei IPv64), „Für App-HTTPS-Namen verwenden“ (`PUT /docker/proxy`) und
  „Router-Update …“: die Adresse für Router und andere Updater
  (`https://ipv64.net/nic/update?key=<update-token>&domain=…&ip=<ipaddr>&ip6=<ip6addr>`)
  mit „Neuer Update-Token“. Den Token selbst zeigt TitanOS nie an (er steht bei
  ipv64.net).
- **Erreichbarkeit überwachen** wie unter Dynamisches DNS (dieselbe Komponente,
  `web/src/lib/ddns/`); einschaltbar, sobald DynDNS hier mit IPv64 gespeichert
  ist.

### 2b. Zuweisungen
Eine Tabelle „Domain → Ziel“ für jede Domain des Kontos und jeden Namen darunter.
„Ändern …“ öffnet einen Dialog mit der Ziel-Art:

- **NAS:** der Name kommt in dynamisches DNS (`ddns.AddHostname`); unter ihm
  antwortet die Weboberfläche.
- **App über das NAS:** volle Domain im HTTPS-Namen der App
  (`appproxy` `Route.Domains`, siehe `docs/APP-PROXY.md`) und der Name in
  dynamischem DNS. Das ist dasselbe Feld „Eigene Domains“ wie im App-Editor und
  unter Einstellungen › HTTPS-Namen für Apps: Zuweisungen legen keine eigene
  Liste an, sondern lesen und schreiben `domains` der Route. Von Hand dort
  eingetragene Domains des Kontos erscheinen in der Tabelle als zugewiesen. Hat die App noch keinen HTTPS-Namen, legt TitanOS einen an
  (Port aus den veröffentlichten TCP-Ports der App, nie die von TitanOS, ab
  Werk nur Heimnetz/Tailscale/NetBird; für das Internet Häkchen weg und Port
  443 weiterleiten).
- **App aus dem Internet über das CDN:** dieselbe Veröffentlichung wie im
  App-Editor (Warnung, Häkchen „eigene Anmeldung“, `confirm`, optional
  Firewall), nur für einen Namen direkt unter einer Domain des Kontos. Der Name
  verlässt dabei dynamisches DNS und die HTTPS-Namen, damit kein Updater die
  CDN-Einträge überschreibt.
- **VM oder Gerät:** volle Domain an einem Ziel der HTTPS-Namen; ein neues Ziel
  lässt sich im Dialog anlegen (Felder wie unter HTTPS-Namen für Apps).

Unter einer Domain mit Platzhalter, die selbst in dynamischem DNS steht, braucht
ein Name keinen eigenen DynDNS-Eintrag (Spalte DNS: „Platzhalter“). Neue Namen
unter einer Domain entstehen beim Zuweisen; „Anlegen und zuweisen“ legt zuvor
eine neue DynDNS-Domain an. Ändern nimmt den Namen vom bisherigen Ziel (zuerst
aus dem CDN zurück, dann DynDNS, dann HTTPS-Namen); „Zuweisung entfernen“
(einfacher Bestätigungsdialog) nimmt alles zurück und lässt Domain und
DNS-Einträge bei IPv64. Nutzt dynamisches DNS einen anderen Anbieter, lehnt
TitanOS NAS/App/Gerät mit `ddns_other_provider` ab.

API (nur Besitzer/Admin, Änderungen nur aus einer Websitzung):
`GET /api/v1/ipv64/assignments` → `{domains, assignments: [{domain, zone, prefix, target: nas|app|target|public, app, port, scheme, dns: ddns|wildcard|"", url}], zones, ddns: {enabled, provider, hostnames, max}, firewall}`;
`PUT /api/v1/ipv64/assignments/{domain}` mit `{target: "nas"|"app"|"target", app?, targetId?, port?, scheme?, localOnly?, public?, firewall?, confirm?}`;
`DELETE /api/v1/ipv64/assignments/{domain}`; `PUT /api/v1/ipv64/ddns` mit
`{enabled, hostnames, ipv4, ipv6, interval, lookupURL, interface, ipv6Prefix}`.
Beide Änderungen und der Reiter DynDNS stehen im Aktivitätsprotokoll.

### 3. Überwachung
Healthchecks aller Arten (`push`, `icmp`, `tcp`, `http`, `keyword`) anlegen,
ändern, pausieren/fortsetzen, löschen, neuer Token (Warnung; beim Check dieses
NAS übernimmt TitanOS den neuen für die Pings), Testnachricht. Details mit
Verfügbarkeit/Latenz für 1h/24h/7d/30d samt kleinem Verlauf, Events und
Incidents. Der Check von „Erreichbarkeit überwachen“ heißt hier „dieses NAS“;
eingeschaltet wird er weiter unter Dynamisches DNS. Integrationen (Telegram,
Gotify, ntfy, Pushover, Discord, Webhook, E-Mail, SMS, IPv64-App) mit den
Feldern je Typ aus der Beschreibung anlegen (Geheimnisse nur schreibend),
Vorauswahl ändern, testen, löschen (die Account-E-Mail nicht). Statusseiten
anlegen/ändern/löschen mit Adresse (`slug`, 3–128 Zeichen a–z/A–Z/0–9),
optionalem Passwort, Design 1–4, Reihenfolge der Checks und Link auf
`https://ipv64.net/status/<slug>`.

### 4. Ausfallschutz
Offline-Seiten anlegen/ändern/löschen, Logo hochladen (JPEG/PNG/WebP, höchstens
1 MB und 1024 × 1024, in titand geprüft und als `multipart/form-data` gesendet),
Vorschau. Failover-Policies anlegen/ändern/pausieren/fortsetzen/löschen,
Monitore (Healthcheck, optional umgekehrt) und Regeln (A/AAAA-Eintrag mit genau
einem Ziel: Ersatz-IP, anderer Eintrag oder Offline-Seite), manuell umschalten
und zurückschalten mit Bestätigung.

**„Wartungsseite, wenn dieses NAS ausfällt“:** ein Klick richtet ein: den
Push-Healthcheck des NAS (der vorhandene von „Erreichbarkeit überwachen“, sonst
ein neuer `titanos-<hostname>`, 30 Minuten Abstand, 10 Minuten Kulanz, und die
Pings werden eingeschaltet), eine Offline-Seite für die gewählte Domain und eine
Policy „automatisch umschalten und zurück“ mit dem Check als Monitor und allen
A-/AAAA-Einträgen ohne Präfix als Regeln auf die Offline-Seite. Scheitert ein
Schritt, nimmt TitanOS das bisher Angelegte zurück. Was entstanden ist, steht
mit Namen auf der Seite und lässt sich entfernen (Policy und Offline-Seite; auf
Wunsch auch der dafür angelegte Check).

### 5. Öffentlicher Zugriff
CDN-Domains: aufnehmen, Reverse Proxy ein/aus, Frontend (`http`,
`https_redirect`, `http_and_https`), Backend-Schema und -Port, WebSocket (nur bei
aktivem Proxy), Zertifikat (Anzeige; neu ausstellen), Cache, Zugriffsliste
(erlauben/sperren, Einträge in CIDR), welche Einträge über das CDN laufen
(`full`, `other_family_only`, wieder direkt), neu ausrollen, Traffic zurücksetzen,
Serverumzug, entfernen; Traffic und Zustand des Portmappers
(`suspended_traffic_limit`). Portmapper ein/aus und Mappings (Name, Eintrag,
öffentlicher Port 50000–62000 oder automatisch, Zielport, TCP/UDP). Je Domain
gibt es **einen** Backend-Port.

**„Öffentlich über IPv64“ im App-Editor** (Docker › App): Domain und optional
ein Präfix wählen, Port der App, Protokoll zum NAS. Nach einem deutlichen
Warndialog (die App wird aus dem Internet erreichbar, sie braucht eine eigene
Anmeldung – Häkchen –, der Router muss den Port weiterleiten bzw. freigeben)
legt titand an: A-Eintrag für den Namen, falls keiner da ist (über
`/domains/{d}/dyndns` mit den Adressen von dynamischem DNS oder der Absenderadresse),
die Domain im CDN (falls noch nicht), wartet auf den CDN-Server, leitet die
Einträge über das CDN, schaltet den Reverse Proxy ein und setzt
HTTPS-Weiterleitung, Backend-Port und WebSocket. Belegt eine andere App die
Domain schon mit einem anderen Port, lehnt TitanOS ab. Die Ports der
Weboberfläche von TitanOS (und 22, 8750, die Ports von titand) werden **nie**
veröffentlicht. **Firewall:** ist sie eingeschaltet, lässt TitanOS die Ports
genau dieser App aus der Zone Internet zu (Häkchen, ab Werk an) und zeigt das
an; ist sie aus, ändert sich nichts. IPv64 dokumentiert keine
Quelladressbereiche, aus denen das CDN verbindet; eine Einschränkung auf das CDN
ist deshalb **nicht** möglich. Zurücknehmen leitet die Einträge wieder direkt,
entfernt ein von TitanOS angelegtes CDN und stellt die Firewall-Zonen der App
wieder her; die DNS-Einträge bleiben.

### 6. Cloud Router
Router anlegen und ansehen, Gateways (Name, Adressen in CIDR, deaktivieren,
neue Schlüssel mit Warnung, Traffic zurücksetzen, löschen; der WireGuard-Port
`listen_port` wird angezeigt, ist laut Beschreibung aber nicht änderbar), Peers
anlegen/löschen (Konfiguration mit privatem Schlüssel nur auf ipv64.net),
Routen, Firewall-Regeln (Kette, Aktion, Quelle/Ziel derselben Familie) und die
öffentliche IPv6 (höchstens eine je Router) anlegen, ein-/ausschalten, löschen.

**„Dieses NAS verbinden“** (optional, ab Werk aus): TitanOS legt beim gewählten
Gateway einen Peer an, erzeugt das Schlüsselpaar selbst (X25519) und gibt IPv64
nur den öffentlichen Schlüssel (`PUT /peers/{token}/keys` ohne `private_key`).
Aus `GET /peers/{token}/config` übernimmt es Adressen, Gegenstelle, AllowedIPs,
Keepalive und den Preshared Key. Privater und Preshared Key liegen in
`/config/ipv64/wireguard.key` bzw. `.psk` (Dateien von root, auf FAT32 ohne
eigene Rechte; nie über die API). Die Schnittstelle `ipv64wg` entsteht beim
Verbinden und bei jedem Start von titand neu (`ip link`, `wg set` mit
Schlüsseldateien unter `/run/titanos/ipv64`, `ip address`); AllowedIPs werden
geroutet, **Standardrouten nie**. Die Firewall zählt `ipv64wg` zur Zone VPN.
Trennen entfernt die Schnittstelle, die Schlüssel und auf Wunsch den Peer.
Braucht **wireguard-tools** (`wg`), das mit diesem Stand ins Systemabbild kommt –
wirksam erst mit dem nächsten **vollen Systemupdate**; das Kernelmodul ist im
Debian-Kernel enthalten.

### 7. Blocklist und Meldungen
**v64 Blocklist** (ab Werk aus): `GET /honeypot/list-token` liefert Token und
Basisadresse; titand lädt `ipv64_blocklist_v64.txt` höchstens einmal am Tag
(Knopf „Jetzt holen“ nur, wenn seit einem Tag nichts geholt wurde oder der letzte Versuch scheiterte), nur von `https://…ipv64.net`,
höchstens 32 MiB und 200.000 Einträge, ohne private, VPN- und Sonderadressen und
ohne Netze größer als /16 (IPv6 /32). Die Liste liegt nur im RAM; nach einem
Neustart wird sie neu geholt. Die Firewall verwirft diese Absender nur in der
Zone Internet und nur, wenn sie eingeschaltet ist. Ohne Live-Berechtigung
liefert IPv64 den Tagesstand (`X-IPv64-List: daily`). Der Änderungs-Feed
(`/threat/feed`) wird nicht genutzt (nur für höhere Klassen und Beitragende).

**Meldungen** (ab Werk aus): Bei eingeschaltetem „Konto beobachten“ sieht titand
höchstens alle fünf Minuten nach (aus dem RAM, nie mit Zugriff auf `/config`)
und meldet Änderungen, keine Zustände: Failover umgeschaltet/zurück
(`ipv64_failover`, `ipv64_fallback`), Healthcheck DOWN (`ipv64_check_down`),
CDN-Traffic aufgebraucht (`ipv64_cdn_traffic`, aus Portmapper-Zustand und
`/me`). Der erste Blick setzt nur den Ausgangsstand.

## Ablage

`/config/ipv64/settings.json` (selten, atomar): eigener Key, Meldungen,
Blocklist an/aus, Tunnel (Gateway, Peer, öffentlicher Schlüssel, Adressen),
veröffentlichte Apps (Domain, CDN, Einträge, vorige Firewall-Zonen) und
Wartungsseiten (IDs der angelegten Ressourcen). `wireguard.key`/`.psk` daneben.
Ältere titand ignorieren den Ordner.

## Nicht umgesetzt

- Registrierung, Paket, Abrechnung, Account-Freigaben (`X-IPv64-Act-As`).
- DNS64-Clients, Healthchecks 2.0 (`/hc2/…`) mit Nodes und Wartungsfenstern,
  OSPF und Port-Forwards von Gateways, eigene Peer-Schlüssel für andere Geräte,
  AllowedIPs einzelner Peers, Honeypot-Sensoren und -Meldungen, Frühwarnung,
  IP-Auskunft und Passwortprüfung – in der Beschreibung vorhanden, aber nicht
  Teil dieses Auftrags.
- Quelladressbereiche des CDN: nicht dokumentiert, daher keine Einschränkung.
- Manueller DynDNS-Aufruf außerhalb des Updaters.

## Prüfstand

Go-Tests: Client (`dnsprovider/ipv64_v2_test.go`: jedes Feld und jede
Aufzählung der genutzten Endpunkte, keine Geheimnisse in Antworten,
Token-Bucket), Manager (`ipv64/ipv64_test.go`: Blocklist, Abfragen,
WireGuard mit nachgebauten Befehlen), API (`api/ipv64_area_test.go`: Rechte,
Websitzung, Keys, Domains, Checks, Wartungsseite, Veröffentlichen, CDN, Router;
`api/ipv64_assign_test.go`: Zuweisungen an NAS, App, Ziel, Platzhalter, CDN,
Zurücknehmen, anderer DDNS-Anbieter, Reiter DynDNS), DDNS-Teilupdates
(`ddns/ddns_test.go`), volle Domains im App-Proxy (`appproxy/options_test.go`),
Firewall (`firewall/ipv64_test.go`). Webtests `web/tests/ipv64.test.mjs`
(Helfer, Texte in beiden Sprachen), Browserprüfung `scripts/test-ui-ipv64.mjs`.
Nicht geprüft: echtes IPv64, echtes WireGuard/`wg`, echte nftables-Mengen mit
großer Blocklist, Router-Weiterleitung für veröffentlichte Apps.
