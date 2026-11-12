# IPv64.net in TitanOS

**Der Reiter „IPv64“ ist entfernt** (PLAN 79-2, Entscheidung des
Projektinhabers vom 11.11.2026, „Reiter + Backend“). Einstellungen › Dynamisches
DNS hat keine Reiter mehr. Mit dem Reiter sind auch die Funktionen dahinter
weg: Übersicht mit API-Keys und Account-Log, Domains und DNS-Einträge verwalten,
der eigene DynDNS-Unterreiter, Zuweisungen, Überwachung (Healthchecks aller
Arten, Integrationen, Statusseiten), Ausfallschutz (Offline-Seiten, Failover,
Wartungsseite), öffentlicher Zugriff (CDN-Verwaltung, Portmapper), Cloud Router
samt WireGuard-Tunnel `ipv64wg`, v64 Blocklist und die Meldungen `ipv64_*`.
Dafür ist wieder die Webseite von IPv64.net da.

## Was bleibt

- **IPv64 als Anbieter von Dynamisches DNS** (`docs/DDNS.md`): API-Key oder
  Domain-Update-Token, Domains übernehmen oder eine neue DynDNS-Domain anlegen,
  Platzhalter, „für App-HTTPS-Namen verwenden“ und **Erreichbarkeit überwachen**
  (Push-Healthcheck). Routen `POST /api/v1/network/ipv64/…`.
- **DNS-Challenge von Let's Encrypt** über IPv64 (`docs/LETSENCRYPT.md`).
- **„Öffentlich über IPv64“ im App-Editor** (Docker › App, siehe unten). Es
  hing am entfernten Bereich, ist aber eine eigene Funktion, die ein NAS in
  Betrieb haben kann; ob es ebenfalls wegfällt, entscheidet der Projektinhaber
  (offene Frage, Stand 11.11.2026).

Alles weiterhin **nur gegen Nachbauten geprüft**
(`backend/internal/dnsprovider/ipv64fake`); gegen das echte IPv64 ist nichts
davon abgenommen.

## „Öffentlich über IPv64“ im App-Editor

Domain und optional ein Präfix wählen, Port der App, Protokoll zum NAS. Nach
einem deutlichen Warndialog (die App wird aus dem Internet erreichbar, sie
braucht eine eigene Anmeldung – Häkchen –, der Router muss den Port weiterleiten
bzw. freigeben) legt titand an: A-Eintrag für den Namen, falls keiner da ist
(über `/domains/{d}/dyndns` mit den Adressen von dynamischem DNS oder der
Absenderadresse), die Domain im CDN (falls noch nicht), wartet auf den
CDN-Server, leitet die Einträge über das CDN, schaltet den Reverse Proxy ein und
setzt HTTPS-Weiterleitung, Backend-Port und WebSocket. Belegt eine andere App
die Domain schon mit einem anderen Port, lehnt TitanOS ab (je Domain gibt es
**einen** Backend-Port). Die Ports der Weboberfläche von TitanOS (und 22, 8750,
8752, die Ports von titand) werden **nie** veröffentlicht.

- **Firewall:** ist sie eingeschaltet, lässt TitanOS die Ports genau dieser App
  aus der Zone Internet zu (Häkchen, ab Werk an) und zeigt das an; ist sie aus,
  ändert sich nichts. IPv64 dokumentiert keine Quelladressbereiche, aus denen
  das CDN verbindet; eine Einschränkung auf das CDN ist deshalb **nicht**
  möglich.
- **Zurücknehmen** leitet die Einträge wieder direkt, entfernt ein von TitanOS
  angelegtes CDN und stellt die Firewall-Zonen der App wieder her; die
  DNS-Einträge bleiben.
- **Rechte:** nur Besitzer und Administratoren; Veröffentlichen und
  Zurücknehmen nur aus einer angemeldeten Websitzung (`browserOnly`): weder
  MCP-Clients noch der KI-Agent, Home Assistant oder API-Tokens. Keine dieser
  Routen steht im Werkzeugkatalog.
- **API-Key:** der von Dynamisches DNS, sonst der von Let's Encrypt; er geht nur
  als `Authorization: Bearer` zu IPv64. Einen eigenen Key lässt sich nicht mehr
  eintragen. Liegt aus der Zeit des Reiters noch einer in
  `/config/ipv64/settings.json`, nimmt TitanOS ihn als letzte Quelle (`legacy`),
  damit sich eine damit veröffentlichte App zurücknehmen lässt.
- **Limits:** alle Aufrufe gehen durch einen gemeinsamen Token-Bucket (300, 10
  je Sekunde), der `RateLimit-Remaining` übernimmt und bei 429 für
  `Retry-After` Sekunden anhält. Fehler (RFC 9457) erscheinen mit dem `detail`
  von IPv64.

API (nur Besitzer/Admin): `GET /api/v1/ipv64/domains` (Domains des Kontos zur
Auswahl), `GET /api/v1/ipv64/apps/{app}` → `{ports, firewall, zones, published?,
keySource}`, `PUT /api/v1/ipv64/apps/{app}` mit `{domain, prefix?, port, scheme?,
firewall?, confirm}`, `DELETE /api/v1/ipv64/apps/{app}`. Alle anderen Routen
unter `/api/v1/ipv64/…` gibt es nicht mehr (404).

## Vorhandene Einstellungen auf einem NAS

`/config/ipv64/settings.json` teilt sich die Liste der veröffentlichten Apps
(`published`) mit den Einstellungen des entfernten Bereichs (`apiKey`, `notify`,
`blocklist`, `wireguard`, `maintenance`). TitanOS liest die Datei einmal beim
Start, benutzt davon nur `published` (und `apiKey` wie oben) und meldet nichts
über den Rest. Es löscht nichts: Schreibt es die Datei (App veröffentlicht oder
zurückgenommen), bleiben alle anderen Felder Wort für Wort stehen, ebenso
`wireguard.key`/`.psk` daneben. So findet eine ältere App-Version nach einem
Rückweg (`docs/rules/UPDATES.md`) ihre Einstellungen wieder; Blocklist, Tunnel
und Meldungen laufen dort wieder an. Der Ordner bleibt im Stick-Dateimanager
ausgeblendet (er kann Schlüssel enthalten). Wer die Reste loswerden will, löscht
`/config/ipv64/` von Hand, sobald keine App mehr veröffentlicht ist.

Was beim Update von selbst endet:

- **Tunnel `ipv64wg`:** Ein App-Update startet nur titand neu, die
  Schnittstelle der alten Version stünde weiter. titand entfernt sie deshalb
  einmal beim Start (`ip link del dev ipv64wg`, eine Zeile im Journal; nie im
  Entwicklungsmodus) und baut sie nicht wieder auf. Die Firewall zählt
  `ipv64wg` nicht mehr zur Zone VPN.
- **v64 Blocklist:** Die Mengen `blocklist4`/`blocklist6` stehen nicht mehr in
  den Regeln; mit dem ersten Laden der Regeln sind sie weg.
- **Meldungen und Abfragen:** Es gibt keinen Abfrage-Takt mehr. Bereits
  angezeigte Meldungen `ipv64_*` verlinken auf Dynamisches DNS.
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
Domains, Einträge, Healthcheck, Integrationen ohne Geheimnisse, CDN,
Token-Bucket), Ablage (`ipv64/ipv64_test.go`: veröffentlichte Apps, alte
Einstellungen bleiben beim Lesen und Schreiben erhalten, kein Schreiben beim
Start, kaputte Datei, Rest-Tunnel), API (`api/ipv64_test.go`: Anbieter-Routen;
`api/ipv64_area_test.go`: Rechte, Websitzung, Veröffentlichen und Zurücknehmen,
entfernte Routen antworten 404), Firewall (`firewall/appzones_test.go`: Zonen
einer App, keine Blocklist und kein `ipv64wg` in den Regeln). Browserprüfung:
`scripts/test-ui-firewall.mjs` (Dynamisches DNS ohne Reiter, alte Links),
`scripts/test-ui-appform.mjs` (Zeile im App-Editor). Nicht geprüft: echtes
IPv64, Router-Weiterleitung für veröffentlichte Apps, das Entfernen von
`ipv64wg` auf einem echten NAS.
