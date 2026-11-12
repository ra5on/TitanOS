# IPv64.net in TitanOS

**Der IPv64-Bereich ist entfernt** (PLAN 79-2, Entscheidungen des
Projektinhabers vom 11. und 12.11.2026: „Reiter + Backend“, danach auch
„Öffentlich über IPv64“ je App: „Nutze ich nicht, weg damit“). Einstellungen ›
Dynamisches DNS hat keine Reiter mehr. Weg sind: Übersicht mit API-Keys und
Account-Log, Domains und DNS-Einträge verwalten, der eigene DynDNS-Unterreiter,
Zuweisungen, Überwachung (Healthchecks aller Arten, Integrationen,
Statusseiten), Ausfallschutz (Offline-Seiten, Failover, Wartungsseite),
öffentlicher Zugriff (CDN, Portmapper, „Öffentlich über IPv64“ im App-Editor),
Cloud Router samt WireGuard-Tunnel `ipv64wg`, v64 Blocklist und die Meldungen
`ipv64_*`. Dafür ist die Webseite von IPv64.net da. Nicht wieder einbauen oder
vorschlagen.

## Was bleibt

- **IPv64 als Anbieter von Dynamisches DNS** (`docs/DDNS.md`): API-Key oder
  Domain-Update-Token, Domains übernehmen oder eine neue DynDNS-Domain anlegen,
  Platzhalter, „für App-HTTPS-Namen verwenden“ und **Erreichbarkeit überwachen**
  (Push-Healthcheck). Routen `POST /api/v1/network/ipv64/…`, nur
  Besitzer/Administratoren. Unter `/api/v1/ipv64/…` gibt es keine Routen mehr
  (404).
- **DNS-Challenge von Let's Encrypt** über IPv64 (`docs/LETSENCRYPT.md`).

Der API-Key ist der von Dynamisches DNS bzw. Let's Encrypt; er geht nur als
`Authorization: Bearer` zu IPv64. Alle Aufrufe gehen durch einen gemeinsamen
Token-Bucket (300, 10 je Sekunde), der `RateLimit-Remaining` übernimmt und bei
429 für `Retry-After` Sekunden anhält. **Nur gegen Nachbauten geprüft**
(`backend/internal/dnsprovider/ipv64fake`), nie gegen das echte IPv64.

## Vorhandene Einstellungen auf einem NAS

- **`/config/ipv64/`** (`settings.json` mit eigenem Key, Meldungen, Blocklist,
  Tunnel, veröffentlichten Apps, Wartungsseiten; `wireguard.key`/`.psk`):
  titand liest den Ordner nicht mehr, schreibt nicht hinein und löscht ihn
  nicht. Er stört keinen Start. So findet eine ältere App-Version nach einem
  Rückweg (`docs/rules/UPDATES.md`) ihre Einstellungen wieder; Blocklist, Tunnel
  und Meldungen laufen dort wieder an. Der Ordner bleibt im Stick-Dateimanager
  ausgeblendet (er kann Schlüssel enthalten). Wer die Reste loswerden will,
  löscht `/config/ipv64/` von Hand. Der eigene Key des Bereichs wird nicht mehr
  benutzt; Dynamisches DNS und Let's Encrypt haben ihre eigenen.
- **Tunnel `ipv64wg`:** Ein App-Update startet nur titand neu, die
  Schnittstelle der alten Version stünde weiter. titand entfernt sie deshalb
  einmal beim Start (`ip link del dev ipv64wg`, eine Zeile im Journal; nie im
  Entwicklungsmodus) und baut sie nicht wieder auf. Die Firewall zählt
  `ipv64wg` nicht mehr zur Zone VPN.
- **v64 Blocklist:** Die Mengen `blocklist4`/`blocklist6` stehen nicht mehr in
  den Regeln; mit dem ersten Laden der Regeln sind sie weg.
- **Meldungen und Abfragen:** Es gibt keinen Abfrage-Takt mehr. Bereits
  angezeigte Meldungen `ipv64_*` verlinken auf Dynamisches DNS.
- **Eine App, die über „Öffentlich über IPv64“ veröffentlicht war:** Docker,
  die App und die HTTPS-Namen hängen nicht an dem Eintrag; nichts scheitert
  beim Start. TitanOS nimmt die Veröffentlichung aber **nicht** zurück: Die
  CDN-Domain mit Reverse Proxy bei IPv64 bleibt bestehen, die App ist also
  weiter aus dem Internet erreichbar, bis man das CDN auf ipv64.net entfernt.
  Hatte TitanOS dafür die Ports der App in der Firewall aus der Zone Internet
  zugelassen, bleibt diese Richtlinie bestehen; sie steht unter Einstellungen ›
  Firewall bei den Docker-Apps und lässt sich dort zurücknehmen.
- **Bei IPv64 selbst bleibt alles, wie es ist:** Healthchecks, Statusseiten,
  Failover-Policies, Offline-Seiten (auch die „Wartungsseite, wenn dieses NAS
  ausfällt“), CDN-Domains, Portmapper, Cloud Router und Peers löscht TitanOS
  nicht. Sie lassen sich auf ipv64.net verwalten oder entfernen.
- **Alte Links:** `/settings/ipv64` (auch mit `?tab=…`) führt auf
  Einstellungen › Dynamisches DNS; `/settings/ddns?tab=ipv64&sub=…` zeigt
  dieselbe Seite.

Das Paket `wireguard-tools` bleibt im Systemabbild: der WireGuard-Server des NAS
(`docs/WIREGUARD.md`) braucht `wg`.

## Prüfstand

Go-Tests: Client (`dnsprovider/ipv64_test.go`, `ipv64_api_test.go`: Konto,
Domains, Einträge, Healthcheck, Integrationen ohne Geheimnisse, Token-Bucket),
API (`api/ipv64_test.go`: Anbieter-Routen), Rest-Tunnel
(`cmd/titand/ipv64_leftover_test.go`). Browserprüfung:
`scripts/test-ui-firewall.mjs` (Dynamisches DNS ohne Reiter, alte Links),
`scripts/test-ui-appform.mjs` (App-Editor). Start mit einem
`/config/ipv64/` aus 0.76 im Entwicklungsmodus: keine Warnung, Dateien
unverändert. Nicht geprüft: echtes IPv64, das Entfernen von `ipv64wg` auf einem
echten NAS.
