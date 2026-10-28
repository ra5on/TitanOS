# Dynamisches DNS

TitanOS hält auf Wunsch Namen auf die Adressen des NAS: den **A-Eintrag** auf die
öffentliche IPv4-Adresse des Anschlusses, den **AAAA-Eintrag** auf die globale
IPv6-Adresse des NAS selbst. **Ab Werk aus.** Einstellungen › Netzwerk und
Dienste › **Dynamisches DNS** (`/settings/ddns`, nur Besitzer und
Administratoren).

## Bedienung

- **Anbieter:** Cloudflare, deSEC/dedyn.io, Hetzner (Console), IONOS, Netcup,
  INWX, DuckDNS, IPv64.net, eigener Nameserver (RFC 2136) sowie **anderer
  DynDNS-Dienst** (dyndns2): Update-Adresse mit Benutzer und Passwort, z. B.
  No-IP `https://dynupdate.no-ip.com/nic/update`; oder mit Platzhaltern wie bei
  Routern (`<domain>`, `<ipaddr>`, `<ip6addr>`, `<username>`, `<passwd>`), z. B.
  Strato `https://dyndns.strato.com/nic/update?hostname=<domain>&myip=<ipaddr>,<ip6addr>`.
- **Namen** (höchstens 8) bekommen alle dieselben Adressen.
- **IPv4:** Die Adresse fragt TitanOS über IPv4 bei einem Dienst ab
  (nacheinander api.ipify.org, ipv4.icanhazip.com, v4.ident.me; bei IPv64 zuerst
  `https://ipv64.net/api/v2/ip`; oder ein eigener Dienst). So hängt sie nicht vom
  Router ab. Nur öffentliche Adressen werden übernommen; mit DS-Lite ist die
  IPv4-Adresse nicht erreichbar, dann nur IPv6 verwenden.
- **IPv6:** die erste feste globale Adresse des NAS aus `/proc/net/if_inet6`
  (ohne temporäre, veraltete und noch nicht bestätigte Adressen, ohne
  ULA/link-local, ohne Docker-, VM-, LXC-, Tailscale- und NetBird-Schnittstellen),
  wahlweise einer bestimmten Schnittstelle. Der Router muss Verbindungen zum NAS
  zulassen.
- **Nachsehen alle** 5 Minuten bis 1 Tag (Vorgabe 10 Minuten). Geändert wird nur,
  wenn sich eine Adresse geändert hat; nach einem Neustart vergleicht TitanOS
  zuerst mit dem, was die Namen im DNS schon zeigen. **Jetzt aktualisieren**
  schickt die Adressen auch ohne Änderung.
- Der Zustand (gefundene und eingetragene Adressen, letzter Blick, letzte
  Aktualisierung, Fehler, nächster Blick) liegt nur im RAM.

## Fehler und Grenzen der Anbieter

- Nach einem Fehler neuer Versuch nach höchstens 5 Minuten; verlangt der
  Anbieter eine Pause (HTTP 429 mit `Retry-After`, gesperrter Schlüssel), wartet
  TitanOS so lange, mindestens 10 Minuten.
- Meldung `ddns_failed` einmal je Fehlerreihe; Aktivitätsprotokoll
  `ddns_updated` (Namen und Adressen) bzw. `ddns_failed`.
- Dienste, die eine fehlende Adresse aus der Absenderadresse ergänzen könnten
  (dyndns2, DuckDNS, IPv64-Updater), bekommen immer beide bekannten Adressen
  zusammen.

## IPv64.net

Neben den Adressen bindet die Seite IPv64 weiter ein (umgesetzt nach der
OpenAPI-Beschreibung `https://ipv64.net/api/v2/openapi.json` sowie
`dyndns_updater_api` und `healthcheck_updater_api`):

- **Zwei Zugänge:** der **API-Key des Kontos** (Account › API) für alles oder
  nur der **Domain-Update-Token** (geringste Rechte, nur DynDNS). Einer genügt.
  Mit API-Key gehen Updates über `POST /api/v2/domains/{domain}/dyndns` (laut
  IPv64 nicht durch das Tageslimit begrenzt); nur mit Update-Token oder mit
  „IPv6-Präfix mitschicken“ (`auto` oder fester Präfix, `ipv6prefix`) über
  `https://ipv64.net/nic/update`. Dort hält TitanOS höchstens die Hälfte des
  Tageslimits ein (aus `/me` gelesen, sonst 48 am Tag) und höchstens drei
  Anfragen in 10 Sekunden. Den Update-Token einer Domain holt TitanOS mit dem
  API-Key selbst aus `/domains`; er geht nie an die Oberfläche.
- **Domains:** Liste zum Übernehmen statt Eintippen; **neue Subdomain** als
  `<name>.<zone>` mit Auswahl der Zonen von IPv64 (einfacher
  Bestätigungsdialog); **Wildcard** an/aus; **für App-HTTPS-Namen verwenden**
  setzt die Domain unter Einstellungen › HTTPS-Namen für Apps (mit Wildcard sind
  Apps dann unter `<app>.<domain>` erreichbar).
- **Konto, DNS-Einträge und alles Weitere** (API-Keys, Log, eigene Domains,
  Löschen/Zurücksetzen, neuer Update-Token, Überwachung, Ausfallschutz, CDN,
  Cloud Router, Blocklist) stehen seit dem Bereich **Einstellungen › IPv64**
  dort (`docs/IPV64.md`); die Seite Dynamisches DNS verlinkt ihn und behält ihre
  Updater-Einstellungen. Dieselben Einstellungen (mit IPv64 als festem Anbieter
  und dem Key des IPv64-Bereichs) lassen sich auch unter Einstellungen › IPv64 ›
  DynDNS bearbeiten; beide Seiten zeigen sofort dasselbe. Unter IPv64 ›
  Zuweisungen kommen Namen einzeln dazu oder heraus (`AddHostname`/
  `RemoveHostname`, der Rest bleibt). Erzeugt man dort einen neuen Update-Token für eine
  Domain, deren Token dynamisches DNS benutzt, übernimmt TitanOS ihn selbst.
- **Erreichbarkeit überwachen** (Healthcheck, **ab Werk aus**): Erst beim
  Einschalten legt TitanOS bei IPv64 einen Push-Healthcheck an (Name
  `titanos-<hostname>`, Abstand ab 20 Minuten, Vorgabe 30, wegen 72 Pings am
  Tag; Kulanzzeit; Benachrichtigungswege aus `/integrations`, nur Namen) oder
  nimmt einen vorhandenen, und meldet sich dann im gewählten Abstand per
  `https://ipv64.net/health`. Optional pausiert TitanOS den Check vor geplantem
  Ausschalten oder Neustart (Menü, Energie-Zeitplan, Updates; braucht den
  API-Key) und setzt ihn nach dem nächsten Start fort (`/config/ddns/heartbeat-paused`
  merkt das über den Neustart). Ein Ausschalten durch die USV pausiert nicht.
  Zustand bei IPv64 und letzte Meldung auf Klick, Testnachricht höchstens alle
  30 Sekunden. Ausschalten fragt in einem einfachen Dialog, ob der Healthcheck
  bei IPv64 pausiert, gelöscht oder nur nicht mehr angepingt wird.
- Für die DNS-Challenge von Let's Encrypt legt TitanOS `_acme-challenge` als
  TXT-Eintrag über die API an, wartet, bis IPv64 ihn nicht mehr als
  `pending_dns` meldet, und löscht danach genau diesen Eintrag
  (`DELETE /domains/{d}/records?type=TXT&prefix=…&content=…`). Domain und
  Platzhalter verwenden denselben Namen.
- CDN, Cloud Router, DNS-Failover, Statusseiten und die v64 Blocklist: unter
  Einstellungen › IPv64. Der Healthcheck „Erreichbarkeit überwachen“ bleibt hier
  und erscheint dort als „dieses NAS“; „Neuer Token“ dort übernimmt TitanOS für
  die Pings selbst.

Alle Aufrufe von IPv64 zählen zum Token-Bucket des Kontos (300, 10 je
Sekunde); der Client von TitanOS hält ihn für alle Aufrufe gemeinsam ein und
folgt `RateLimit-Remaining` und `Retry-After`. Die Seite lädt deshalb nichts von
selbst, nur auf Klick.

## Ablage und Geheimnisse

`/config/ddns/settings.json` mit Tokens, Passwörtern und dem
Healthcheck-Token; die API gibt sie nie heraus (nur „ist gesetzt“), ein leeres
Feld behält den gespeicherten Wert nur für denselben Anbieter und dasselbe Konto.
Der Ordner `ddns/` ist im Dateimanager (Stick) ausgeblendet und nicht im
Diagnosepaket. Tokens gehen nur in HTTP-Kopfzeilen (bei DuckDNS und Platzhaltern
von dyndns2 notgedrungen in die Adresse; Fehlermeldungen enthalten sie nie).
Gelesen wird `/config` nur beim Start.

## API

`GET`/`PUT /api/v1/network/ddns`, `POST /api/v1/network/ddns/update`; IPv64:
`POST /api/v1/network/ipv64/{account,domains,domains/create,domains/change,records,records/add,records/delete,healthchecks,heartbeat/on,heartbeat/off,heartbeat/status,heartbeat/test}`
(POST, damit ein eingetippter, noch nicht gespeicherter API-Key nie in einer
Adresse steht). Nur Besitzer/Administratoren.

## Prüfstand

Geprüft nur gegen nachgebaute Dienste (`httptest`): alle Anbieter-APIs, IPv64
API v2 nach der OpenAPI-Beschreibung, `/nic/update` und `/health`, ein
nachgebauter RFC-2136-Nameserver mit TSIG-Prüfung, Erkennung der IPv6-Adresse
aus einer künstlichen `if_inet6`-Tabelle, Aktualisieren nur bei Änderung,
Abgleich mit DNS nach dem Start, Grenzen und Pausen, Healthcheck mit Pause über
einen Neustart. **Echte Konten bei den Anbietern und echte Anschlüsse
(DS-Lite, Präfixwechsel) sind nicht geprüft.**
