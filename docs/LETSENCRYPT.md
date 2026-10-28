# Let's Encrypt

TitanOS holt auf Wunsch ein Zertifikat von Let's Encrypt (oder einer anderen
Zertifizierungsstelle mit ACME, RFC 8555) für die Weboberfläche und – mit einem
Platzhalter `*.<domain>` – für die HTTPS-Namen der Apps, und erneuert es selbst.
**Ab Werk aus.** Einstellungen › Netzwerk und Dienste › **Let's Encrypt**
(`/settings/letsencrypt`, nur Besitzer und Administratoren).

## Bedienung

- **Namen** (höchstens 20): Namen, die im öffentlichen DNS zu diesem NAS
  gehören, z. B. `nas.example.org`. `.local`-Namen und IP-Adressen kann keine
  öffentliche Zertifizierungsstelle bestätigen.
- **Nachweis:**
  - **DNS-Challenge** (empfohlen): TitanOS legt beim DNS-Anbieter kurz einen
    TXT-Eintrag `_acme-challenge.<name>` an, wartet, bis die Nameserver ihn
    kennen, lässt Let's Encrypt prüfen und löscht genau diesen Eintrag wieder.
    Kein offener Port nötig; nur so gibt es Platzhalter (`*.example.org`).
  - **HTTP-Challenge** über Port 80: titand beantwortet
    `/.well-known/acme-challenge/` auf Port 80 (sonst Weiterleitung auf HTTPS).
    **Port 80 muss aus dem Internet zum NAS erreichbar sein** (Weiterleitung im
    Router; eine Firewall auf dem NAS muss Port 80 erlauben). Keine Platzhalter,
    keine App-Namen. Die Seite warnt, wenn titand ohne `-http` läuft, und
    ebenso, wenn die TitanOS-Firewall eingeschaltet ist und Port 80 aus dem
    Internet nicht für jeden erlaubt (Normalfall: Port 80 gehört zur
    „Weboberfläche“, die nur Heimnetz und VPN erlaubt). Ausweg: DNS-Challenge
    oder unter Firewall › Eigene Regeln „Erlauben, TCP, Port 80, Zone Internet“
    ohne Absender. TitanOS öffnet Port 80 nie von selbst. Eine Regel mit
    Absender zählt nicht, weil Let's Encrypt von wechselnden Adressen prüft
    (`firewall.Manager.InternetHTTP`, Feld `internetHttp` der Status-API).
- **Auch HTTPS-Namen der Apps abdecken** (nur DNS-Challenge): fügt
  `*.<domain>` hinzu, wobei `<domain>` die eigene Domain unter Einstellungen ›
  HTTPS-Namen für Apps ist. Ändert sich diese Domain, wird ein neues Zertifikat
  geholt. Weitere Namen einer Route unter anderen Domains deckt es nicht ab.
- **DNS-Anbieter:** Cloudflare, deSEC/dedyn.io, Hetzner (Console/Cloud-API),
  IONOS, Netcup, INWX, DuckDNS, IPv64.net, eigener Nameserver per RFC 2136
  (TSIG: hmac-sha256/-sha512/-sha384/-sha224/-sha1). Zugangsdaten siehe Hinweise
  auf der Seite. DuckDNS hält je Name nur einen TXT-Eintrag; Name und Platzhalter
  werden dann nacheinander bestätigt.
- **Wartezeit nach dem Setzen:** Vorgabe je Anbieter (Cloudflare 15 s, IONOS und
  INWX 2 min, Netcup 8 min, RFC 2136 10 s, sonst 1 min), 0–3600 s einstellbar.
  Danach fragt TitanOS bis zu zwei Minuten 1.1.1.1 und 9.9.9.9, ob die Einträge
  sichtbar sind (nur zur Beschleunigung; Let's Encrypt fragt die autoritativen
  Nameserver selbst).
- **E-Mail-Adresse** (optional), **Testumgebung (Staging)**, **andere
  ACME-Zertifizierungsstelle** (Adresse des Verzeichnisses, z. B. step-ca).
- **Jetzt erneuern** holt sofort ein neues Zertifikat, auch während einer
  Wartezeit nach Fehlern.

## Verhalten

- Das Zertifikat wird **zusätzlich** ausgeliefert: Fragt ein Browser nach einem
  seiner Namen (SNI), bekommt er es; über IP-Adresse oder `.local` bleibt das
  eigene bzw. selbst signierte Zertifikat (`tlscert.Manager.SetACME`). App-Namen
  unter dem Platzhalter bekommen es vor dem importierten und dem
  App-Platzhalterzertifikat. Austausch ohne Neustart.
- **Erneuerung** 30 Tage vor dem Ablauf, bei kürzeren Laufzeiten nach zwei
  Dritteln. Geplant wird aus dem Zertifikat im RAM; `/config` wird dafür nicht
  gelesen.
- **Fehlschläge:** neuer Versuch nach 1, 2, 4 … höchstens 24 Stunden; das
  bisherige Zertifikat bleibt in Gebrauch. Meldung `letsencrypt_failed` beim
  ersten Fehlschlag einer Reihe, danach höchstens einmal am Tag;
  `letsencrypt_expiring` einmal je Zertifikat, wenn es in 14 Tagen abläuft;
  `letsencrypt_renewed` (Hinweis) nach Erfolg. Aktivitätsprotokoll:
  `letsencrypt_issued` / `letsencrypt_failed` (Herkunft System).
- Ausschalten nimmt das Zertifikat sofort aus dem Dienst; die Datei bleibt für
  ein späteres Einschalten.

## Ablage und Geheimnisse

- `/config/tls/acme/settings.json` – Einstellungen samt Zugangsdaten des
  Anbieters. Tokens und Passwörter gibt die API nie heraus (nur „ist gesetzt“);
  ein leeres Feld behält den gespeicherten Wert nur für denselben Anbieter und
  dasselbe Konto. Sie stehen nie in Protokollen, Fehlermeldungen oder auf einer
  Kommandozeile (es gibt keine externen Programme), Tokens nur in
  HTTP-Kopfzeilen bzw. im Rumpf, nie in Adressen – außer bei DuckDNS, dessen API
  nur Adressparameter kennt; Fehlertexte enthalten die Adresse dort nicht.
- `/config/tls/acme/account-key.pem` – ECDSA-Schlüssel des ACME-Kontos.
- `/config/tls/acme/certificate.pem` – Kette, Schlüssel und Verzeichnis in
  **einer** Datei, atomar geschrieben.
- `tls/` ist im Dateimanager (Stick) ausgeblendet und nie im Diagnosepaket.

## API

`GET`/`PUT /api/v1/system/letsencrypt` (Einstellungen mit `clearSecrets`),
`POST /api/v1/system/letsencrypt/renew`. Nur Besitzer/Administratoren, nicht im
Assistenten-Katalog und nicht in der öffentlichen API.

## Technik und Lizenz

ACME-Client ist `golang.org/x/crypto/acme` (BSD-3-Clause, ohnehin schon
Abhängigkeit von titand); die DNS-Anbieter spricht Paket `dnsprovider` mit der
Standardbibliothek selbst an, RFC 2136 mit eigenem DNS-UPDATE samt TSIG. Kein
lego, keine neuen Module. Das statische Binary (ohne Weboberfläche gemessen, `-s -w`) wächst mit Let's Encrypt, dynamischem DNS und IPv64 zusammen um rund 0,56 MB (19,02 → 19,59 MB).

## Prüfstand

Geprüft nur mit Unit-Tests: ein nachgebauter ACME-Server (`httptest`, Konto,
Bestellung, DNS-01 und HTTP-01, Finalisieren, Kette), nachgebaute APIs aller
DNS-Anbieter (`httptest`) und ein nachgebauter Nameserver, der TSIG-Signaturen
nachrechnet; Planung der Erneuerung, Rückfall, Meldungen und dass keine
Geheimnisse in Status oder Fehlern stehen. **Eine echte Ausstellung bei Let's
Encrypt und echte Konten der Anbieter waren nicht möglich und sind nicht
geprüft.** Die Anbieter-APIs sind nach ihrer öffentlichen Dokumentation
umgesetzt; bei Hetzner die DNS-API der Hetzner Console (Cloud-API), bei IPv64
API v2 nach `https://ipv64.net/api/v2/openapi.json`.
