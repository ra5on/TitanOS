# T-Mail – Vertrag (1.05)

Eigenes E-Mail-Programm in TitanOS (PLAN §81, Inhaber 13.11. „Eigenes T-Mail“, Fragerunde 16.11.2026). Dieses Dokument ist der Vertrag zwischen Backend (`backend/internal/tmail`, `backend/internal/api/tmail*.go`) und Oberfläche (`web/src/routes/mail/`, `web/src/lib/tmail*`). Abweichungen werden hier eingetragen, bevor sie gebaut werden. Was das Backend genauer festgelegt oder geändert hat, steht im Abschnitt „Festlegungen des Backends“ (17.11.2026); er geht den Zeilen darüber vor.

## Umfang der ersten Fassung (Entscheidungen des Inhabers)

- IMAP/SMTP mit Passwort oder App-Passwort (GMX, Web.de, IONOS, T-Online, iCloud, Gmail mit App-Passwort, eigene Domains). **Kein OAuth** (Gmail/Microsoft-Anmeldung später).
- Lesen, Schreiben, Antworten, Weiterleiten, Ordner, Suche, Anhänge (hoch- und herunterladen, aus und nach Dateien des NAS), Entwürfe.
- **Meldung bei neuer Mail** (Glocke und Push), je Postfach abschaltbar.
- **Adressbuch** aus gesendeten Mails (Vorschläge beim Schreiben), kein CardDAV.
- **Signaturen** je Postfach (mehrere, eine als Vorgabe).
- **Regeln/Filter**, die auf dem NAS laufen (z. B. „Absender enthält X → in Ordner Y“).
- **Jedes Postfach privat:** nur das Konto, das es angelegt hat, sieht es. Kein Teilen.
- **Der KI-Agent bekommt keinen Zugriff** (meine Festlegung, dem Inhaber gesagt): kein Werkzeug, nicht im MCP-Katalog, nicht in `publicRoutes`. Ein Test hält das fest.
- Das Postfach des Papierkrams (`imapmini`, KI-Agent) bleibt davon getrennt.

## Rechte

- Eigene App-Freigabe „T-Mail“ je Konto (Benutzer › Konto › Apps), **ab Werk aus**, für jede Rolle. Jede Route prüft serverseitig: angemeldet, App freigegeben, Postfach gehört dem Konto.
- Keine Route in `publicRoutes`, keine im MCP-/Werkzeugkatalog. Alle Routen nur aus der Web-Sitzung (`browserOnly`), nicht über Tokens, MCP oder Anfragen, die titand im Namen eines Kontos stellt.
- Konto löschen nimmt T-Mail mit (am Haken aus 1.03, `agentForgetUser`-Muster): Einstellungen, Passwörter, Adressbuch, offene Verbindungen. Auch bei Löschung durch einen anderen Knoten im Cluster. Ein neues Konto gleichen Namens erbt nichts.
- Mail-Passwörter: nie an die Seite (nur `passwordSet`), nie ins Protokoll, nie ins Diagnosepaket, nie in den Kontenabgleich des Clusters.

## Datenablage

- **Stick** `/config/tmail/<konto>.json`: Postfächer (ohne Klartext-Passwort), Signaturen, Regeln, Meldungs-Schalter. Geschrieben **nur beim Speichern** durch die Person. Atomar wie alle `/config`-Dateien.
- **Passwörter** verschlüsselt (AES-256-GCM) mit einem Schlüssel des NAS in `/config/tmail/key` (0600, root). Ehrlich dokumentiert: Das schützt gegen eine einzelne kopierte Datei oder ein Protokoll, **nicht** gegen jemanden mit voller Kontrolle über das NAS. Eine Sicherung der Einstellungen enthält Schlüssel und Passwörter (wie andere Geheimnisse des Sticks); das steht in der Doku.
- **Daten-Volume** `.titanos/mail/<konto>/` über `volumedata` (Home-Volume von TitanOS): Adressbuch (`contacts.json`, wächst von selbst). Ohne eingebundenes Volume lernt das Adressbuch nichts dazu (Vorschläge aus dem RAM bis zum Neustart), nie Ersatz im RAM/Overlay als Datei.
- **Kein dauerhafter Mail-Cache.** Inhalte holt titand bei Bedarf per IMAP; Suche serverseitig über IMAP `SEARCH`. Zwischenspeicher nur im RAM (begrenzt, z. B. Ordnerliste, letzte Listen, geöffnete Nachricht).
- **Stand für Meldungen** (UIDNEXT der INBOX je Postfach) nur im RAM. Nach einem Neustart von titand gilt UIDNEXT beim ersten Verbinden als Grundlinie: Mails, die ankamen, während titand nicht lief, melden sich nicht (dokumentiert).

## Verbindungen

- **Nur TLS:** `security` = `tls` (implizit, Vorgabe 993/465) oder `starttls` (Pflicht, Vorgabe 143/587). Zertifikat wird geprüft (Systemspeicher). Kein Klartext, keine Ausnahme für selbstsignierte Zertifikate in der ersten Fassung.
- **Ziele:** beliebiger Host und Port, aber nicht Loopback (`127.0.0.0/8`, `::1`), nicht Link-Local (`169.254.0.0/16`, `fe80::/10`) und nicht die eigenen Adressen des NAS auf Ports von titand. Heimnetz-Adressen sind für Besitzer und Administratoren erlaubt (eigener Mailserver im LAN); **Konten der Rolle Benutzer erreichen nur öffentliche Adressen** (wie bei `notify`; Festlegung 17.11.2026, siehe „Festlegungen des Backends“). Geprüft nach DNS-Auflösung, bei jeder Verbindung.
- **Zeitgrenzen:** Verbinden 15 s, Befehle 60 s, Abruf einer Nachricht 120 s, Senden 120 s.
- **Anbieter schonen:** Nach einem **Anmeldefehler** wird nicht weiter probiert, bis das Passwort (oder Benutzername/Host) geändert ist; Status `auth_failed`. Andere Fehler: Wiederholen mit wachsender Pause (30 s … 15 min). Höchstens **eine IDLE-Verbindung je Postfach (nur INBOX)** dauerhaft, und nur wenn Meldungen oder Regeln für das Postfach an sind. Weitere Verbindungen nur, solange die Oberfläche offen ist (z. B. eine für Listen/Abrufe je Postfach, nach 2 min Ruhe geschlossen).
- Nach einem App-Update oder Neustart: neu verbinden, Grundlinie neu, **keine doppelten Meldungen**.

## Sicherheit der Anzeige

- **HTML-Mails:** serverseitig bereinigt (bluemonday, Positivliste: Textauszeichnung, Tabellen, Bilder, Links, eingeschränkte Inline-Styles; keine Skripte, Formulare, `<style>`-Blöcke mit `@import`, `<base>`, `<meta>`, Event-Attribute, `javascript:`/`data:`-Links). Angezeigt in einem `iframe` mit `srcdoc` und `sandbox="allow-popups allow-popups-to-escape-sandbox"` (**ohne** `allow-scripts`, **ohne** `allow-same-origin`) und CSP-Meta: `default-src 'none'; style-src 'unsafe-inline'; img-src <erlaubt>; font-src 'none'`.
- **Externe Bilder:** ab Werk ersetzt durch Platzhalter; Knopf „Bilder laden“ holt die Fassung mit `images=1` (dann `img-src https:` zusätzlich). Kein Laden über das NAS als Proxy.
- **Eingebettete Bilder (`cid:`):** über eine angemeldete Route, nur `image/png`, `image/jpeg`, `image/gif`, `image/webp` inline; alles andere als Anhang.
- **Links:** `target="_blank" rel="noopener noreferrer"`; die Oberfläche zeigt das echte Ziel (`title`/Statuszeile) und warnt, wenn sichtbarer Text eine andere Domain nennt.
- **Anhänge** immer mit `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox`, nie inline vom TitanOS-Origin (auch nicht HTML/SVG/PDF).
- **Grenzen gegen bösartige Mails:** MIME-Tiefe 10, höchstens 200 Teile, Kopfzeilen 64 KiB, Anzeige-Text höchstens 2 MB je Teil, Nachricht zur Anzeige höchstens 25 MB (größere: Kopf und Anhangsliste, Text auf Klick). Zeichensätze über go-message/charset.
- **Senden:** Empfänger höchstens 100, Anhänge zusammen höchstens 25 MB (Anbietergrenzen), `From` immer die Adresse des Postfachs (kein frei wählbarer Absender).

## API (alle unter `/api/v1/mail`, JSON, Fehler wie überall `{error:{code,message}}`)

| Route | Zweck |
|---|---|
| `GET /accounts` | Postfächer des Kontos: `[{id,name,email,senderName,user,imap:{host,port,security},smtp:{host,port,security},passwordSet,notify,signature,status:{state,code,since},unread}]`; `state` ∈ `ok`,`connecting`,`auth_failed`,`unreachable`,`off` |
| `POST /accounts` | anlegen: Felder wie oben + `password` |
| `PUT /accounts/{id}` | ändern; `password` leer = behalten |
| `DELETE /accounts/{id}` | entfernen (nur in TitanOS, nichts beim Anbieter) |
| `POST /accounts/test` | Felder + `password` (oder `id` für gespeichertes Passwort): prüft IMAP-Anmeldung und SMTP-Anmeldung → `{imap:{ok,code},smtp:{ok,code}}` |
| `GET /providers?email=` | Vorschlag Host/Port/Sicherheit aus fester Liste bekannter Anbieter (kein Abruf im Netz) + Hinweis App-Passwort (`appPassword:true`) |
| `GET /accounts/{id}/folders` | `[{path,name,role,unread,total}]`, `role` ∈ `inbox`,`sent`,`drafts`,`trash`,`junk`,`archive`,`` (aus SPECIAL-USE, sonst Namen) |
| `POST /accounts/{id}/folders` · `PUT …/folders` · `DELETE …/folders?path=` | anlegen (`{path}`), umbenennen (`{path,to}`), löschen |
| `GET /accounts/{id}/messages?folder=&before=&limit=&q=&unread=&flagged=` | `{messages:[{uid,from:{name,addr},to:[…],subject,date,flags:{seen,flagged,answered},size,attachments:bool}],next,total}` – neueste zuerst, `before` = UID für die nächste Seite, `limit` ≤ 100; `q` → IMAP SEARCH (Betreff, Absender, Empfänger, Text) |
| `GET /accounts/{id}/message?folder=&uid=&images=0/1` | `{uid,folder,from,to,cc,replyTo,subject,date,messageId,inReplyTo,references,flags,text,html,externalImages:bool,attachments:[{part,name,type,size,inline,cid}],truncated:bool}`; markiert als gelesen; `html` ist bereinigt und fertig für `srcdoc` |
| `GET /accounts/{id}/part?folder=&uid=&part=` | Anhang herunterladen (Kopfzeilen siehe Sicherheit) |
| `GET /accounts/{id}/cid?folder=&uid=&cid=` | eingebettetes Bild (nur erlaubte Bildtypen) |
| `POST /accounts/{id}/part/save` | `{folder,uid,part,path}` Anhang in Dateien des NAS speichern, mit den Dateirechten des Kontos |
| `POST /accounts/{id}/flags` | `{folder,uids,add:[],remove:[]}` (`seen`,`flagged`,`answered`) |
| `POST /accounts/{id}/move` | `{folder,uids,to}` |
| `POST /accounts/{id}/delete` | `{folder,uids}` → in den Papierkorb; im Papierkorb endgültig |
| `POST /accounts/{id}/send` | multipart: `meta` (JSON `{to,cc,bcc,subject,text,inReplyTo,references,signature,draftUid}`), Dateien als `file`, `nasFiles` (Pfade, mit Dateirechten geprüft); legt Kopie in „Gesendet“ ab (außer der Anbieter tut es selbst, z. B. Gmail); lernt Empfänger ins Adressbuch; entfernt den Entwurf |
| `POST /accounts/{id}/draft` | wie `send` ohne Versand → in „Entwürfe“, Antwort `{uid}` (ersetzt `draftUid`) |
| `GET /contacts?q=` · `DELETE /contacts?addr=` | Vorschläge (`[{name,addr,count,last}]`, höchstens 20), Eintrag vergessen |
| `GET /settings` · `PUT /settings` | `{signatures:[{id,name,text,default:[accountId…]}],rules:[{id,account,enabled,when:{from,to,subject},then:{moveTo,markRead,flag}}],pushContent}` |
| `GET /events` | Server-Sent Events für die offene Oberfläche: `mail` (neue Mail `{account,folder,uid}`), `status` (Postfach-Status), `flags` |

**Neue Fehlercodes** (je mit Text Deutsch/Englisch; HTTP-Status und weitere Codes siehe „Festlegungen des Backends“): `mail_disabled` (App nicht freigegeben), `mail_auth_failed`, `mail_unreachable`, `mail_tls` (kein TLS/Zertifikat ungültig), `mail_host_forbidden`, `mail_timeout`, `mail_too_large`, `mail_not_found`, `mail_folder_exists`, `mail_send_failed` (mit Antwort des Servers ohne Geheimnisse).

## Meldungen und Regeln

- Neue Mail in der INBOX (über IDLE erkannt): zuerst Regeln (in Reihenfolge; `moveTo` per IMAP MOVE, Rückfall COPY+DELETE+EXPUNGE), dann Meldung nur, wenn die Mail noch in der INBOX ist und Meldungen für das Postfach an sind. Art `mail_new`, Titel = Absender, Text = Betreff (in der Push-Mitteilung nur, wenn die Push-Einstellung des Kontos Inhalte erlaubt; sonst „Neue E-Mail in <Postfach>“). Mehrere neue Mails in kurzer Zeit → eine Sammelmeldung.
- Regeln laufen nur für neu eintreffende Mails, nicht rückwirkend.

## Oberfläche

- Programm **„T-Mail“** im Arbeitsplatz und am Handy (eigenes Symbol, Fenster), Route `/mail`. Erscheint nur bei App-Freigabe.
- Arbeitsplatz: drei Spalten (Ordner je Postfach | Liste | Nachricht), am Handy Ebenen (Postfächer/Ordner → Liste → Nachricht) mit Zurück-Pfeil und Wischen.
- **Kein abgeschnittener Text:** Betreff und Absender in der Liste brechen um; **keine Vorschauzeile** in der Liste (wäre zwangsläufig gekürzt). Datum mit Tag, wenn nicht heute.
- Schreiben: An/Cc/Bcc mit Vorschlägen aus dem Adressbuch, Betreff, Text (reiner Text in der ersten Fassung; Antworten zitieren den Text), Signatur wählbar, Anhänge hochladen oder aus den Dateien des NAS, Entwurf automatisch speichern (alle 30 s bei Änderung). Am Handy als Seite, am Arbeitsplatz als eigenes Fenster.
- Einrichten: Assistent (E-Mail-Adresse → Vorschlag aus `/providers` → Passwort/App-Passwort mit Hinweis je Anbieter → „Verbindung prüfen“ → speichern). Einstellungen: Postfächer, Signaturen, Regeln, Meldungen je Postfach.
- Externe Bilder: Leiste „Externe Bilder blockiert – laden“; Links zeigen ihr Ziel.
- Alle Texte Deutsch/Englisch, 44 px Tippflächen, Status nie nur über Farbe, Bestätigungsdialog bei endgültigem Löschen.

## Abhängigkeiten

- `github.com/emersion/go-imap/v2` (MIT), festgelegt auf **v2.0.0-beta.9** (Grund: IDLE/NOTIFY im Client, Korrekturen gegenüber beta.8 – u. a. keine getrennte Verbindung bei COPYUID, Fehler werden durchgereicht; Prüfung der Änderungen 17.11.2026). Testserver `imapserver/imapmemserver` für Protokoll-Tests im Prozess.
- `github.com/emersion/go-message` (MIT), `github.com/emersion/go-sasl` (MIT), `github.com/emersion/go-smtp` (MIT, Client; Server für Tests), `github.com/microcosm-cc/bluemonday` (BSD-3-Clause).
- Lizenztexte müssen im Inventar (`scripts/third-party-licenses.py`) gefunden werden; `THIRD_PARTY_LICENSES` ergänzen. Das Release bleibt ein App-Update (alles unter `backend/`, `web/`, `docs/`).

## Festlegungen des Backends (17.11.2026)

Beim Bau des Backends genauer festgelegt oder geändert. Routen, Felder und Codes hier gelten für die Oberfläche.

**Freigabe (Rechte)**
- Gespeichert in `/config/tmail/access.json` (`{given:{<konto>:<Zeit>}}`), nur beim Ändern geschrieben. Die Freigabe gehört dem Konto, dem sie gegeben wurde: ein später angelegtes Konto gleichen Namens hat sie nicht (wie die Kamerarechte des NVR). Ein gesperrtes Konto hat T-Mail nicht.
- `GET /api/v1/users/{name}/mail` → `{enabled}`, `PUT /api/v1/users/{name}/mail` `{enabled}` → `{enabled}`: Besitzer/Administratoren, Besitzer nur durch Besitzer (`403 forbidden`). Eigene Route, weil `PUT /users/{name}/apps` Besitzer und Administratoren ablehnt (Docker-Apps haben sie immer), T-Mail aber für jede Rolle ab Werk aus ist. Die Oberfläche zeigt den Schalter unter Benutzer › Konto › Apps.
- `GET /api/v1/mail/access` → `{enabled}` für das angemeldete Konto, ohne 403: so weiß der Arbeitsplatz, ob er T-Mail zeigt.
- Jede andere Route ohne Freigabe: `403 mail_disabled`. Alle Routen zusätzlich `browserOnly` (Token, MCP, KI-Agent, Home Assistant: `403 browser_session_required`) und `clusterLocal` (über einen anderen Knoten: `403 cluster_local_only`; T-Mail läuft je Knoten, Postfächer liegen auf dem Knoten, auf dem sie angelegt wurden).
- Fremdes Postfach: `404 mail_not_found` (Kennungen gelten nur im eigenen Konto).

**Fehlercodes und HTTP-Status**

| Code | Status | Bedeutung |
|---|---|---|
| `mail_disabled` | 403 | keine Freigabe |
| `mail_auth_failed` | 409 | Anbieter lehnt die Anmeldung ab; es wird nicht weiter probiert, bis Passwort, Benutzername, Host, Port oder Sicherheit geändert sind oder „Verbindung prüfen“ mit den gespeicherten Daten gelingt |
| `mail_unreachable` | 502 | Server nicht erreichbar |
| `mail_tls` | 502 | kein TLS, STARTTLS fehlt oder Zertifikat ungültig |
| `mail_host_forbidden` | 400 | Ziel verboten (Loopback, Link-Local, eigene Adresse auf titand-Port; für Konten der Rolle Benutzer jede nicht öffentliche Adresse – dann nennt `message` diesen Grund) |
| `mail_timeout` | 504 | Zeitgrenze überschritten |
| `mail_too_large` | 413 | Anhänge über 25 MB, Anhang zum Speichern über 64 MB |
| `mail_not_found` | 404 | Postfach, Ordner, Nachricht oder Teil fehlt |
| `mail_folder_exists` | 409 | Ordner gibt es schon |
| `mail_send_failed` | 502 | SMTP lehnt ab; `message` enthält Code und Text des Servers (einzeilig, höchstens 300 Zeichen) |
| `mail_invalid` | 400 | Eingabe ungültig; `message` nennt das Feld |
| `mail_password_required` | 400 | **neu:** Host von IMAP oder SMTP geändert (oder „prüfen“ gegen andere Hosts) ohne neues Passwort – ein gespeichertes Passwort geht nie an einen anderen Host |
| `mail_no_password` | 409 | **neu:** kein Passwort gespeichert |
| `mail_rate_limited` | 429 | **neu:** mehr als 10 Verbindungsprüfungen (`POST /accounts/test`) je Minute und Konto; `Retry-After: 60` |
| `mail_settings_unreadable` | 503 | **neu:** `/config/tmail/<konto>.json` oder `access.json` beim Start nicht lesbar; nichts wird darüber geschrieben |

Fehler der Dateirechte (Anhang aus/nach NAS-Dateien) kommen mit den Codes des Dateimanagers (`file_not_found`, `file_exists` …).

**Ziele je Rolle (Festlegung des Koordinators, 17.11.2026)**
- Konten der Rolle **Benutzer** erreichen nur öffentliche Adressen (keine privaten Netze, kein Tailscale/CGNAT, kein Link-Local, nichts Reserviertes; `netguard.Public` wie bei `notify`). Besitzer und Administratoren dürfen zusätzlich ins Heimnetz. Geprüft im Dialer nach der DNS-Auflösung, für jede Verbindung des Kontos (Listen, Lesen, Senden, IDLE, `POST /accounts/test`); Rolle wie sie jetzt ist.
- Fehler `mail_host_forbidden` mit dem Grund im Text. Ein schon gespeichertes Postfach eines Benutzers mit LAN-Ziel bleibt stehen; seine Verbindung wird abgewiesen, Status `unreachable` mit Code `mail_host_forbidden` (wird später ein Administrator daraus, verbindet es wieder).
- Gegen Port-Abtasten: `POST /accounts/test` höchstens 10-mal je Minute und Konto (alle Rollen, `429 mail_rate_limited`). Für Benutzer unterscheiden die Codes der Prüfung die Ursache nicht: verweigert, Zeitüberschreitung, kein Mailserver und TLS-Fehler heißen alle `mail_unreachable`; nur `mail_auth_failed` und `mail_host_forbidden` bleiben.

**Postfächer**
- Neues Feld `senderName` (Name im `From`, leer = nur Adresse). `name` ist nur die Bezeichnung in T-Mail.
- `security` leer = `tls`; `port` 0 = Vorgabe (993/143, 465/587). Host ohne Schema und Port.
- `status.state` `off` heißt: keine dauerhafte Verbindung (Meldungen und Regeln aus); Listen und Lesen gehen trotzdem. `status.code` ist der Fehlercode oder leer.
- `unread`: zuletzt bekannte Zahl ungelesener Mails der INBOX (aus IDLE-Verbindung oder Ordnerliste), 0 wenn unbekannt.
- `POST /accounts/test`: Felder wie `POST /accounts`, mit `password` oder mit `id` ohne Passwort (dann nur für dieselben Hosts, sonst `mail_password_required`). Antwort `{imap:{ok,code},smtp:{ok,code}}` (HTTP 200). Prüft auch im Zustand `auth_failed`; gelingt die Prüfung mit den gespeicherten Daten, endet `auth_failed`.
- `GET /providers` ohne `email`: `{providers:[…]}`; mit `email` flach: `{known,id,name,domains,imap:{host,port,security},smtp:{…},appPassword,supported,hint}` (`known:false` mit leeren Feldern, wenn der Anbieter unbekannt ist). `supported:false` (Outlook/Hotmail: nur OAuth) → Oberfläche sagt „nicht unterstützt“. `hint` ist ein Textschlüssel der Oberfläche (`imap_enable`, `app_password`, `app_password_2fa`, `mail_password`, `oauth_only`). Feste Liste: GMX, WEB.DE, IONOS, Telekom/T-Online, iCloud, Gmail, Outlook/Hotmail, Posteo, mailbox.org, freenet, Yahoo.

**Ordner und Listen**
- Ordner: `POST {path}`, `PUT {path,to}`, `DELETE ?path=`; die INBOX lässt sich nicht umbenennen oder löschen (`mail_invalid`). Fehlt ein Papierkorb, Entwürfe- oder Gesendet-Ordner, legt T-Mail beim ersten Bedarf `Trash`, `Drafts` bzw. `Sent` an.
- `messages`: `next` ist 0 am Ende; `unread=1`, `flagged=1`; als gelöscht markierte Mails erscheinen nicht. `total` zählt alle Treffer des Filters.

**Lesen**
- Neues Feld `bcc` (nur bei eigenen Mails bekannt, z. B. Entwürfen). `references` ist ein String (Message-IDs mit Leerzeichen getrennt), ebenso `inReplyTo`; so werden sie auch beim Senden zurückgegeben.
- `html` ist ein **ganzes Dokument** mit eigener CSP-Meta (`default-src 'none'; style-src 'unsafe-inline'; img-src data:` – mit `images=1` zusätzlich `https:` –`; font-src 'none'; form-action 'none'; base-uri 'none'`) und `referrer` `no-referrer`; die Oberfläche gibt es unverändert in `srcdoc` (Sandbox wie oben).
- **Eingebettete Bilder (`cid:`) stehen im `html` als `data:`-URL** (nur PNG/JPEG/GIF/WebP, Inhalt geprüft, je höchstens 2 MB, zusammen 8 MB). Grund: Das Sitzungs-Cookie ist `SameSite=Strict`, und ein `srcdoc`-Rahmen ohne `allow-same-origin` hat eine fremde Herkunft – eine angemeldete Bild-Route würde dort nicht laden. `GET /cid` bleibt (nur erlaubte Bildtypen, `inline`, `nosniff`, `CSP: sandbox`), z. B. für die Anhangsliste.
- `<style>`-Blöcke werden ganz entfernt (nicht nur `@import`), Inline-Styles nur aus einer Positivliste (Farben, Schrift, Abstände, Ränder, Breiten; kein `url()`). Externe Bilder ohne `images=1`: grauer Platzhalter (`data:`-SVG); `http:`-Bilder bleiben auch mit `images=1` Platzhalter (CSP nur `https:`).
- Grenzen: Struktur über BODYSTRUCTURE (Tiefe 10, 200 Teile), Kopf `References` höchstens 64 KiB, Textteile je 2 MB (per Teilabruf, nie die ganze Mail). **Abweichung:** Auch bei Mails über 25 MB holt T-Mail die Textteile sofort (begrenzt wie oben) statt „auf Klick“, nur eingebettete Bilder entfallen. `truncated:true`, wenn etwas gekürzt oder weggelassen wurde.
- Teilenummern sind die IMAP-Abschnittsnummern (`1`, `1.2`, …); eine angehängte Mail (`message/rfc822`) ist ein Anhang.
- `GET /part`: immer `Content-Type: application/octet-stream`, `Content-Disposition: attachment`, `nosniff`, `CSP: sandbox; default-src 'none'`. Zeitgrenze 10 min (der Download läuft im Tempo des Browsers).

**Senden und Entwürfe**
- `to`/`cc`/`bcc`: Liste aus Strings (`"Name <a@b>"` oder `"a@b"`) oder Objekten `{name,addr}`.
- `nasFiles`: JSON-Liste `[{root,path}]` (Wurzeln wie `GET /files/roots`), gelesen mit den Rechten des Kontos.
- **Neu:** `meta.attachFrom: {folder,uid,parts:[…]}` hängt Teile einer vorhandenen Mail an (Weiterleiten, wieder geöffneter Entwurf mit Anhängen).
- `signature`: Kennung; der Text kommt unter `-- ` an den Text.
- Antwort `send`: `{sentCopy}` (`true`, wenn die Kopie in „Gesendet“ liegt – bei Gmail, erkannt an `X-GM-EXT-1`, legt der Anbieter sie ab). Schlägt nur die Kopie fehl, gilt die Mail trotzdem als gesendet.
- Ein Entwurf behält `Bcc` im Kopf (er wird nie so versendet); Antwort `{uid}` (0, wenn der Server keine UID nennt).
- `POST /part/save`: `{folder,uid,part,root,path}`; die Datei darf es noch nicht geben; höchstens 64 MB (läuft durch den RAM).

**Signaturen und Einstellungen**
- `signatures[].default` ist die Liste der Postfächer, die diese Signatur vorschlagen (gelesen wird auch eine einzelne Kennung, `{accountId:"…"}` oder `{"<id>":true}`; geschrieben immer die Liste); dieselbe Angabe wie `accounts[].signature` (eine Signatur je Postfach). `PUT /settings` setzt beides.
- **Neu:** `pushContent` (ab Werk `true`): Push-Mitteilungen nennen Absender und Betreff.

**Meldungen**
- **Glocke** (nur Besitzer/Administratoren haben eine): Art `mail_new` bzw. `mail_new_many` (Sammelmeldung), Betreff = Name des Postfachs, Text neutral („Neue E-Mail in {s}.“). **Abweichung:** kein Absender, kein Betreff in der Glocke, denn die Glocke ist über das Werkzeug `notifications` für den KI-Agenten lesbar. Die Oberfläche braucht `notify.kind.mail_new.*` und `notify.kind.mail_new_many.*` in beiden Sprachdateien.
- **Push** an die eigenen Geräte, neue Art `mail` (je Gerät abschaltbar: `PUT /api/v1/notifications/push/{id}` mit `mail:true|false`, weggelassen bleibt es): Titel = Absender, Text = Betreff, wenn `pushContent` an ist und die persönliche Einstellung des Kontos „Inhalt“ nicht `none` ist; sonst „Neue E-Mail“ / „Neue E-Mail in <Postfach>“. Sammelmeldung immer neutral. Tippen öffnet `/mail`. Konten der Rolle Benutzer bekommen nur Push.
- Sammelmeldung: neue Mails eines Postfachs innerhalb von 10 s werden eine Meldung.
- Bereits gelesene Mails (z. B. durch eine Regel „als gelesen markieren“) melden sich nicht.
- Regeln: alle passenden in Reihenfolge; Teile von Absender (Name und Adresse), Empfänger (An und Cc) und Betreff ohne Groß-/Kleinschreibung; nach einem Verschieben enden sie. Verschieben in einen fehlenden Ordner wird protokolliert, die Mail bleibt.

**Ereignisse (`/events`)**
- `mail` `{account,folder,uid}` kommt nur über die IDLE-Verbindung (also wenn Meldungen oder Regeln für das Postfach an sind); bei Verschieben durch eine Regel mit dem Zielordner (`uid` 0, wenn der Server sie nicht nennt). `status` `{account,status}`. `flags` `{account,folder,uids}` nach Flags, Verschieben und Löschen über die API. Der Strom endet mit der Anmeldung (Abmelden, Sperre), mit der Freigabe und wenn titand stoppt (ein offenes Fenster hält kein App-Update auf).

**Datenablage und Löschen**
- `/config/tmail/<konto>.json` nennt sein Konto mit Anlegezeit (`owner`); Einstellungen eines früheren Kontos gleichen Namens werden nicht angezeigt. Das Adressbuch (`.titanos/mail/<konto>/contacts.json`, höchstens 2000 Einträge) nennt sein Konto ebenso.
- Der Schlüssel `/config/tmail/key` entsteht beim ersten Speichern eines Passworts, nie beim Start. Speichern ohne neues Passwort schreibt dasselbe Chiffrat (kein neuer Schreibzugriff ohne Änderung).
- Konto löschen (hier oder durch einen anderen Knoten) und Konto anlegen (auch Übernahme aus dem Cluster): Verbindungen und Ereignisströme enden, `/config/tmail/<konto>.json` und der Eintrag in `access.json` werden entfernt, das Adressbuch vom Volume gelöscht. Lesen und Löschen legen auf dem Volume nichts an (nur Speichern legt `.titanos/mail` an). Ist das Volume gerade nicht da, bleibt die Datei liegen und wird von einem neuen Konto gleichen Namens nicht gelesen.
- Ein App-Update wartet nicht auf T-Mail (keine Arbeit des NAS); beim Beenden von titand werden die Verbindungen ohne Abmelden geschlossen.
- Ziele: „Ports von titand“ sind HTTPS, HTTP, Sicherungsziel (8750), Cluster-Relay (8751), Buddy (8752) und die Wyoming-Ports der Sprachdienste (10300, 10200).

## Prüfstand und Grenzen

Geprüft am 17.11.2026 mit Go-Tests (auch mit `-race`), ohne echte Mailanbieter und ohne Netz nach außen:

- **Protokoll im Prozess** (`backend/internal/tmail`, Server in `tmail/tmailtest`): go-imap `imapserver/imapmemserver` und ein go-smtp-Server, beide mit implizitem TLS und einem eigens erzeugten Zertifikat, dem nur der Test vertraut. Abgedeckt: Anmelden; Anmeldefehler → kein weiterer Verbindungsversuch (gezählt am Server), auch nicht beim Senden, bis das Passwort geändert ist; SMTP-Anmeldefehler setzt `auth_failed`; Ordner mit Rollen und Zählern, anlegen/umbenennen/löschen; Liste mit Seiten, Filtern und Suche; Lesen einteiliger Mails (Text base64, nur HTML, ohne MIME) und mehrteiliger mit Zeichensatz (ISO-8859-1, quoted-printable), bereinigtem HTML (`<script>`, Event-Attribute, `javascript:`, `data:`-Links, Formulare, Rahmen, `<base>`, `<meta>`, `<style>`/`@import`, SVG, `url()` in Styles), Platzhalter für externe Bilder, `images=1`, `cid:` als `data:`; Teile und `cid` per Abschnitt; Flags, Verschieben, Löschen in den Papierkorb und dort endgültig; Senden mit Anhang, Signatur, Antwort-Kopfzeilen, Bcc nur im Umschlag, Kopie in „Gesendet“, Entwurf ersetzt Entwurf, Senden entfernt den Entwurf; Grenzen (101 Empfänger, Kopfzeilen-Einschleusung, 25 MB); IDLE → Regel verschiebt und markiert gelesen, Meldung kommt einmal, mehrere Mails als Sammelmeldung, nach einem Neustart keine Meldung für vorhandene Mails, danach wieder für neue; ohne Meldungen/Regeln keine Verbindung; `Run` endet sofort und schließt die Ereignisströme; STARTTLS gegen einen Server ohne STARTTLS scheitert ohne LOGIN (am Server gezählt), STARTTLS gegen einen TLS-Port und ein unbekanntes Zertifikat scheitern; Signatur-Vorgabe in allen Formen; `/providers` flach.
- **Grenzen gegen bösartige Mails:** Strukturen mit 100 Ebenen und 5000 Teilen (direkt) sowie eine echte Mail mit 30 Ebenen und eine mit 400 Teilen über den IMAP-Server; Text über 2 MB wird gekürzt; Dateinamen ohne Pfade/Steuerzeichen.
- **Ziele je Rolle:** Benutzer → Heimnetz/Loopback abgewiesen (Listen, Senden, Prüfung, IDLE-Status `unreachable`/`mail_host_forbidden`, Postfach bleibt, am Server keine Verbindung), Administrator → Heimnetz erlaubt; Prüfcodes für Benutzer ohne Ursache; Rate-Grenze 10/min greift und gibt nach einer Minute frei (auch über die API mit `429`/`Retry-After`).
- **Ziele:** Loopback (auch über `localhost`), Link-Local, `::1`, `fe80::`, Multicast, eigene Adresse auf titand-Port verboten; Heimnetz und eigene Adresse auf anderem Port erlaubt; der echte Dialer erreicht den Testserver auf 127.0.0.1 nicht.
- **Ablage:** kein Klartext-Passwort im Konfigurationsordner oder in Antworten, Schlüssel 0600, Chiffrat an Postfach gebunden, unverändertes Speichern schreibt nichts Neues; Freigabe ab Werk aus, gesperrt aus, nicht vererbt; Konto vergessen entfernt Einstellungen, Freigabe, Adressbuch und Ereignisströme; Einstellungen eines früheren Kontos gleichen Namens unsichtbar.
- **API** (`api/tmail_test.go`): ohne Freigabe 403 an jeder Route (auch für den Besitzer); fremdes Postfach 404; Benutzer kann sich nicht selbst freigeben, Administrator nicht den Besitzer; jede T-Mail-Route lehnt In-Process-Aufrufe, Tokens und Aufrufe über den Cluster ab; keine Route in `publicRoutes`, kein Werkzeug im Katalog, keine Katalog-/Agent-Datei nennt T-Mail; Anhänge mit `attachment`/`nosniff`/`sandbox`, `cid` nur Bild; Speichern in NAS-Dateien mit Rechten (fremde Freigabe 404, vorhandene Datei abgelehnt); Senden mit Upload und NAS-Datei (fremde Datei 404, nichts versendet); Entwurf; Ereignis `flags` im Strom; Konto löschen hier und über `clusterApplied`, neues Konto erbt nichts; Diagnosepaket (entpackt) und Kontodatensatz des Clusters ohne Mail-Passwort.

Nicht geprüft: echte Anbieter (GMX, Gmail, iCloud …) und ihre Eigenheiten (Gmail-Ordner, APPENDLIMIT, Drosselung), ein erfolgreiches STARTTLS (nur die Ablehnung ohne TLS ist geprüft), der Datensatz, den der Cluster tatsächlich verschickt (geprüft ist der Kontodatensatz `auth.User`, den der Abgleich überträgt), Server ohne MOVE/UIDPLUS (Rückfall COPY+STORE+EXPUNGE nur im Code), Push-Zustellung an echte Geräte, die Oberfläche, ein echtes NAS mit Volume-Wechsel des Adressbuchs, ein App-Update mit offenen IDLE-Verbindungen auf echter Hardware.

Grenzen der ersten Fassung: Nach einem Neustart von titand ist ein Anmeldefehler vergessen (ein neuer Versuch je Start); Mails, die eintrafen, während titand nicht lief, melden sich nicht; `UID SEARCH` liefert bei sehr großen Ordnern alle UIDs (Liste je Anfrage, kein Zwischenspeicher); kein CardDAV, kein OAuth, nur Text beim Schreiben.
