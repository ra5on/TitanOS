# T-Mail – Vertrag (1.06)

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
| `mail_too_large` | 413 | Anhänge über 25 MB, Anhang zum Speichern über 64 MB, Antwort eines Servers über ihr Budget |
| `mail_not_found` | 404 | Postfach, Ordner, Nachricht oder Teil fehlt |
| `mail_folder_exists` | 409 | Ordner gibt es schon |
| `mail_send_failed` | 502 | SMTP lehnt ab; `message` enthält Code und Text des Servers (einzeilig, höchstens 300 Zeichen) |
| `mail_invalid` | 400 | Eingabe ungültig; `message` nennt das Feld |
| `mail_password_required` | 400 | **neu:** Host von IMAP oder SMTP geändert (oder „prüfen“ gegen andere Hosts) ohne neues Passwort – ein gespeichertes Passwort geht nie an einen anderen Host |
| `mail_no_password` | 409 | **neu:** kein Passwort gespeichert |
| `mail_rate_limited` | 429 | **neu:** mehr als 10 Verbindungsprüfungen oder 10 Verbindungen zu noch nie erfolgreichen Servern je Minute und Konto; `Retry-After: 60` |
| `mail_busy` | 503 | **neu:** schon zwei große Anfragen des Kontos (`send`, `draft`, `part/save`), schon vier IMAP-Befehle des Kontos über alle Postfächer, schon vier Downloads des Kontos (16 auf dem NAS), oder auch nach 5 s (bzw. bis zum Ende der Anfrage) kein Platz im RAM-Budget von TitanOS (je laufendem Befehl 24 MiB; **der erste laufende Befehl jedes Kontos** läuft immer, damit T-Mail auch bei vollem Budget benutzbar bleibt – seit 1.06 je Konto, vorher zwei auf dem ganzen NAS); reserviert wird nach der echten Größe (Upload × 3, Dateien des NAS und Teile von Mails × 3, gespeicherter Anhang × 1), unter 1 MiB (reiner Text) nichts; Downloads (`/part`, `/cid`) streamen, reservieren nichts und halten den Platz eines Befehls nur, bis der Teil zu fließen beginnt; `Retry-After: 5` |
| `mail_key_missing` | 503 | **neu:** Schlüssel `/config/tmail/key` fehlt, obwohl Passwörter gespeichert sind (kein neuer Schlüssel; aus der Stick-Sicherung wiederherstellen oder Postfächer neu anlegen) |
| `mail_key_damaged` | 503 | **neu:** Schlüssel beschädigt oder ein Passwort öffnet nicht |
| `mail_settings_unreadable` | 503 | **neu:** `/config/tmail/<konto>.json` oder `access.json` beim Start nicht lesbar; nichts wird darüber geschrieben |

Fehler der Dateirechte (Anhang aus/nach NAS-Dateien) kommen mit den Codes des Dateimanagers (`file_not_found`, `file_exists` …).

**Ziele je Rolle (Festlegung des Koordinators, 17.11.2026)**
- **Grenzen von `netguard.Confined`** (bewusst so): Die Netze des NAS zählen bei IPv6 als /56. Bekommt der Anschluss ein /48 (manche Anbieter, Firmen), liegen andere /56 desselben Anschlusses außerhalb und gelten für Benutzer als öffentlich. Steht das NAS in einem Rechenzentrum mit öffentlichem IPv4-Netz, sind die Nachbarn im selben Netz für Benutzer gesperrt, auch wenn sie fremde Mailserver sind.
- Konten der Rolle **Benutzer** erreichen nur öffentliche Adressen (keine privaten Netze, kein Tailscale/CGNAT, kein Link-Local, nichts Reserviertes, kein 6to4 `2002::/16`, kein NAT64 `64:ff9b::/96`/`64:ff9b:1::/48`) **außerhalb der Netze des NAS** (Netze der eigenen Schnittstellen mit ihrer Maske, IPv6 mindestens als /56; die eigenen Adressen auf allen Ports) – `netguard.Confined`, dieselbe Regel wie bei `notify` und Link-Downloads. Besitzer und Administratoren dürfen zusätzlich ins Heimnetz. Geprüft im Dialer nach der DNS-Auflösung, für jede Verbindung des Kontos (Listen, Lesen, Senden, IDLE, `POST /accounts/test`); Rolle wie sie jetzt ist.
- Fehler `mail_host_forbidden` mit dem Grund im Text. Ein schon gespeichertes Postfach eines Benutzers mit LAN-Ziel bleibt stehen; seine Verbindung wird abgewiesen, Status `unreachable` mit Code `mail_host_forbidden` (wird später ein Administrator daraus, verbindet es wieder).
- Gegen Port-Abtasten: `POST /accounts/test` höchstens 10-mal je Minute und Konto, und höchstens 10 verschiedene Versuche je Minute und Konto mit neuen Servern (dieselbe Adresse samt Port nach DNS zählt einmal, eine Prüfung im Assistenten mit IMAP und SMTP zählt einmal). Nicht neu sind: die Server der beim Start gespeicherten Postfächer mit Passwort, solange sie unverändert sind, **an der ersten Adresse, zu der ihr Name in diesem Lauf aufgelöst wird** (seit 1.06; zeigt der Name später woandershin, zählt das als neues Ziel), und Server, an denen sich das Konto in diesem Lauf angemeldet hat – gemerkt nach ihrer **Adresse nach DNS** samt Port, nicht nach dem Namen. Versuche zählen ebenfalls nach Adresse samt Port (seit 1.06; vorher nach Name und Port, so dass ein Name mit wechselndem DNS in einer Minute beliebig viele Adressen erreichte) (beides alle Rollen, `429 mail_rate_limited`, `Retry-After: 60`; gilt für jede Route, auch über ein gespeichertes Postfach). Für Benutzer unterscheiden **alle** Antworten (Prüfung und jede andere Route) die Ursache nicht: verweigert, Zeitüberschreitung, kein Mailserver und TLS-Fehler heißen `mail_unreachable` ohne Servertext, `mail_send_failed` ohne Antwort des Servers.

**Postfächer**
- Neues Feld `senderName` (Name im `From`, leer = nur Adresse). `name` ist nur die Bezeichnung in T-Mail.
- `security` leer = `tls`; `port` 0 = Vorgabe (993/143, 465/587). Host ohne Schema und Port.
- `status.state` `off` heißt: keine dauerhafte Verbindung (Meldungen und Regeln aus); Listen und Lesen gehen trotzdem. `status.code` ist der Fehlercode oder leer.
- `unread`: zuletzt bekannte Zahl ungelesener Mails der INBOX (aus IDLE-Verbindung oder Ordnerliste), 0 wenn unbekannt.
- `POST /accounts/test`: Felder wie `POST /accounts`, mit `password` oder mit `id` ohne Passwort (dann nur für dieselben Hosts, sonst `mail_password_required`). Antwort `{imap:{ok,code},smtp:{ok,code}}` (HTTP 200). Prüft auch im Zustand `auth_failed`; gelingt die Prüfung mit den gespeicherten Daten, endet `auth_failed`.
- `GET /providers` ohne `email`: `{providers:[…]}`; mit `email` flach: `{known,id,name,domains,imap:{host,port,security},smtp:{…},appPassword,supported,hint}` (`known:false` mit leeren Feldern, wenn der Anbieter unbekannt ist). `supported:false` (Outlook/Hotmail: nur OAuth) → Oberfläche sagt „nicht unterstützt“. `hint` ist ein Textschlüssel der Oberfläche (`imap_enable`, `app_password`, `app_password_2fa`, `mail_password`, `oauth_only`). Feste Liste: GMX, WEB.DE, IONOS, Telekom/T-Online, iCloud, Gmail, Outlook/Hotmail, Posteo, mailbox.org, freenet, Yahoo.

**Ordner und Listen**
- Ordner: `POST {path}`, `PUT {path,to}`, `DELETE ?path=`; die INBOX lässt sich nicht umbenennen oder löschen (`mail_invalid`). Fehlt ein Papierkorb, Entwürfe- oder Gesendet-Ordner, legt T-Mail beim ersten Bedarf `Trash`, `Drafts` bzw. `Sent` an.
- Suche mit ESEARCH (`RETURN (ALL)`), wo der Server es hat. Eine ESEARCH-Antwort ohne `UID` (also Folgenummern) gilt nicht als leer: T-Mail fragt noch einmal ohne ESEARCH; ist auch diese Antwort ohne UIDs, endet die Anfrage mit `mail_unreachable` (seit 1.06).
- `messages`: `next` ist 0 am Ende; **neu** `tooLarge:true` an einer Mail, deren Kopf zu groß zum Auflisten ist (nur UID); `attachments` kommt aus dem `Content-Type` des Kopfs (`multipart/mixed` oder einteilig kein Text); `unread=1`, `flagged=1`; als gelöscht markierte Mails erscheinen nicht. `total` zählt alle Treffer des Filters.

**Lesen**
- Neues Feld `bcc` (nur bei eigenen Mails bekannt, z. B. Entwürfen). `references` ist ein String (Message-IDs mit Leerzeichen getrennt), ebenso `inReplyTo`; so werden sie auch beim Senden zurückgegeben.
- **Links im `html`:** `title` ist immer das echte Ziel (ein `title` des Absenders wird ersetzt). Das Ziel steht in `href`, `title` und Markierung immer als Punycode (`idna`), ohne unsichtbare Zeichen; ein Host, den IDNA ablehnt oder der unsichtbare Zeichen enthält, verliert sein `href` und wird markiert. Der Linktext wird vor dem Vergleich normalisiert (NFKC, ohne unsichtbare Zeichen), die Domain wird irgendwo im Text gesucht („Login: www.sparkasse.de“). Sieht ein Teil wie eine Webadresse aus (mit Schema, mit `www.` oder mit einer Top-Level-Domain der ICANN-Liste, `x/net/publicsuffix`) und nennt er eine andere registrierbare Domain (eTLD+1; `www.sparkasse.de` und `banking.sparkasse.de` sind dieselbe), trägt der Link `data-tmail-mismatch="1"` und dahinter steht im Dokument „⚠ <echte Domain>“ (rot, fett). Dateinamen (`rechnung.pdf`), Versionsnummern (`v1.2`) und Mail-Adressen als Text werden nicht verglichen; bei `mailto:` wird eine Adresse im Text mit der Adresse des Links verglichen. Mischt ein Host im Text oder im Ziel Schriften (ein kyrillisches „а“ in lateinischem Namen), wird immer markiert. Die Markierung trägt eigene Stile mit `!important` (Schriftgröße, Zeilenhöhe, Farbe, Hintergrund, Sichtbarkeit, Deckkraft, Position und seit 1.06 Laufweite und Wortabstand, Schriftart `sans-serif`, Einzug, Groß-/Kleinschreibung, Schriftschnitt, Unterstreichung, Umbruch und Grundlinie), sodass `font-size:0`, weiße Schrift, `letter-spacing`, eine Symbolschrift oder ein Einzug des Absenders sie nicht verstecken.
- `html` ist ein **ganzes Dokument** mit eigener CSP-Meta (`default-src 'none'; style-src 'unsafe-inline'; img-src data:` – mit `images=1` zusätzlich `https:` –`; font-src 'none'; form-action 'none'; base-uri 'none'`) und `referrer` `no-referrer`; die Oberfläche gibt es unverändert in `srcdoc` (Sandbox wie oben).
- **Eingebettete Bilder (`cid:`) stehen im `html` als `data:`-URL** (nur PNG/JPEG/GIF/WebP, Inhalt geprüft, je höchstens 2 MB, zusammen 8 MB). Grund: Das Sitzungs-Cookie ist `SameSite=Strict`, und ein `srcdoc`-Rahmen ohne `allow-same-origin` hat eine fremde Herkunft – eine angemeldete Bild-Route würde dort nicht laden. `GET /cid` bleibt (nur erlaubte Bildtypen, `inline`, `nosniff`, `CSP: sandbox`), z. B. für die Anhangsliste.
- `<style>`-Blöcke werden ganz entfernt (nicht nur `@import`), Inline-Styles nur aus einer Positivliste (Farben, Schrift, Abstände, Ränder, Breiten; kein `url()`). Externe Bilder ohne `images=1`: grauer Platzhalter (`data:`-SVG); `http:`-Bilder bleiben auch mit `images=1` Platzhalter (CSP nur `https:`).
- Grenzen: Struktur über BODYSTRUCTURE (Tiefe 10, 200 Teile), Kopf `References` höchstens 64 KiB, Textteile je 2 MB (per Teilabruf, nie die ganze Mail). **Abweichung:** Auch bei Mails über 25 MB holt T-Mail die Textteile sofort (begrenzt wie oben) statt „auf Klick“, nur eingebettete Bilder entfallen. `truncated:true`, wenn etwas gekürzt oder weggelassen wurde.
- **Zu große Struktur (seit 1.06):** Ist schon die Antwort mit BODYSTRUCTURE zu groß (`mail_too_large`, typisch eine Sammelmail mit Hunderten angehängter Mails), zeigt `GET /message` die Mail ohne Struktur: Kopf aus wenigen Kopfzeilen (wie beim Wächter, je Gruppe höchstens 16 KiB) und den Anfang des Inhalts per `BODY.PEEK[TEXT]` (höchstens 1 MiB) so, wie er ankam – gültiges UTF-8, ohne Steuer- und unsichtbare Zeichen, nicht dekodiert, ohne HTML und ohne Anhänge. Neues Feld `rawText:true` (zusammen mit `truncated:true`); die Oberfläche zeigt dann statt des Hinweises auf Kürzungen `mail.rawText` („zu groß gegliedert … den Anfang ihres Inhalts so, wie er ankam“). Die Mail wird wie sonst als gelesen markiert.
- Teilenummern sind die IMAP-Abschnittsnummern (`1`, `1.2`, …); eine angehängte Mail (`message/rfc822`) ist ein Anhang.
- `GET /part`: immer `Content-Type: application/octet-stream`, `Content-Disposition: attachment`, `nosniff`, `CSP: sandbox; default-src 'none'`. Zeitgrenze 10 min (der Download läuft im Tempo des Browsers). Seit 1.06 haben Downloads (`/part`, `/cid`) eigene Plätze – höchstens 4 je Konto und 16 auf dem NAS, ohne RAM-Reservierung –, und den Platz eines Befehls halten sie nur bis zum Kopf des Literals: hängende Downloads blockieren weder andere Konten noch die übrigen Postfächer des Kontos. Eine Anfrage an ein Postfach, dessen Verbindung gerade ein Download nutzt, wartet auf die Verbindung ohne Befehlsplatz und endet mit der Anfrage. Auch das Schreiben an den Browser ist begrenzt (`SetWriteDeadline` ab dem ersten Byte, 10 min bei `/part`, 120 s bei `/cid`): ein Browser, der nicht mehr liest, gibt Download-Platz und Verbindung danach frei – der Server von titand hat sonst keine Schreib-Zeitgrenze.

**Senden und Entwürfe**
- `to`/`cc`/`bcc`: Liste aus Strings (`"Name <a@b>"` oder `"a@b"`) oder Objekten `{name,addr}`.
- `nasFiles`: JSON-Liste `[{root,path}]` (Wurzeln wie `GET /files/roots`), gelesen mit den Rechten des Kontos.
- **Neu:** `meta.attachFrom: {folder,uid,parts:[…]}` hängt Teile einer vorhandenen Mail an (Weiterleiten, wieder geöffneter Entwurf mit Anhängen); `parts:["*"]` sind alle Anhänge. **Ohne `uid`, mit `draftUid`:** die Anhänge des Entwurfs, den diese Speicherung ersetzt (Ordner findet T-Mail; ohne `parts` alle) – hochgeladene Dateien müssen nicht bei jeder Speicherung neu kommen.
- **Neu:** `meta.answers: {folder,uid}`: diese Mail bekommt nach dem Senden `answered`. Ohne `answers`, mit `inReplyTo`: die Mail mit dieser Message-ID in der INBOX.
- `signature`: Kennung; der Text kommt unter `-- ` an den Text.
- Antwort `send`: `{sentCopy}` (`true`, wenn die Kopie in „Gesendet“ liegt – bei Gmail, erkannt an `X-GM-EXT-1`, legt der Anbieter sie ab). Schlägt nur die Kopie fehl, gilt die Mail trotzdem als gesendet.
- Ein Entwurf behält `Bcc` im Kopf (er wird nie so versendet); Antwort `{uid,attachments:[{part,name,type,size,inline,cid}]}` (`uid` 0, wenn der Server keine UID nennt; `attachments` die Anhänge des neuen Entwurfs).
- `POST /part/save`: `{folder,uid,part,root,path}`; die Datei darf es noch nicht geben; höchstens 64 MB (läuft durch den RAM).

**Signaturen und Einstellungen**
- `signatures[].default` ist die Liste der Postfächer, die diese Signatur vorschlagen (gelesen wird auch eine einzelne Kennung, `{accountId:"…"}` oder `{"<id>":true}`; geschrieben immer die Liste); dieselbe Angabe wie `accounts[].signature` (eine Signatur je Postfach). `PUT /settings` setzt beides.
- **Neu:** `pushContent` (ab Werk `true`): Push-Mitteilungen nennen Absender und Betreff.

**Meldungen**
- **Glocke** (nur Besitzer/Administratoren haben eine): Art `mail_new` mit Betreff = Name des Postfachs, nur wenn die Person ihn selbst vergeben hat und er kein `@` enthält, sonst „T-Mail“ („Neue E-Mail in {s}.“); Art `mail_new_many` (Sammelmeldung) mit Betreff = Anzahl („{s} neue E-Mails in T-Mail.“). Nie eine Adresse. **Abweichung:** kein Absender, kein Betreff in der Glocke, denn die Glocke ist über das Werkzeug `notifications` für den KI-Agenten lesbar. Die Oberfläche braucht `notify.kind.mail_new.*` und `notify.kind.mail_new_many.*` in beiden Sprachdateien.
- **Push** an die eigenen Geräte, neue Art `mail` (je Gerät abschaltbar: `PUT /api/v1/notifications/push/{id}` mit `mail:true|false`, weggelassen bleibt es): Titel = Absender, Text = Betreff, wenn `pushContent` an ist und die persönliche Einstellung des Kontos „Inhalt“ nicht `none` ist; sonst die Texte der Glocke. Sammelmeldung immer neutral. Titel höchstens 120, Text höchstens 240 Zeichen; unsichtbare Zeichen und Steuerzeichen der Schreibrichtung entfernt. Tippen öffnet `/mail`. Konten der Rolle Benutzer bekommen nur Push.
- Sammelmeldung: neue Mails eines Postfachs innerhalb von 10 s werden eine Meldung.
- Bereits gelesene Mails (z. B. durch eine Regel „als gelesen markieren“) melden sich nicht.
- **Runden des Wächters (seit 1.06):** Neue Mails holt der Wächter in Runden von höchstens 20 und nur mit den Kopfzeilen `From`, `Sender`, `Subject`, `Date` sowie `To`, `Cc` – zwei Gruppen per `BODY.PEEK[HEADER.FIELDS (…)]`, je höchstens 16 KiB (`Partial`), statt ENVELOPE. So passt eine Runde auch, wenn ein Absender Köpfe von Hunderten KiB schickt; eine abgeschnittene Adressliste behält die Adressen vor ihrem letzten Komma, kodierte Wörter (RFC 2047) werden dekodiert. Ist eine Runde trotzdem zu groß (ein Server, der `Partial` nicht beachtet), wird sie halbiert, nach drei gelungenen Runden wieder verdoppelt (bis 20); eine einzelne zu große Mail wird übergangen (keine Meldung, sie bleibt im Posteingang). Eine zu große Runde gilt nicht als Verbindungsverlust: Status bleibt, neue Verbindung nach der kürzesten Pause (30 s), die Pause wächst nicht. Zu große Antworten außerhalb einer Runde (SELECT, IDLE) bleiben ein Fehler mit wachsender Pause.
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
- **1.06** (`tmail/review3_test.go`): Downloads blockieren keine anderen Konten und Postfächer, ein abgebrochener Download hängt nicht; der Wächter meldet 25 Mails mit 250-KiB-Köpfen ohne Pause, halbiert zu große Runden ohne wachsende Pause; gespeicherte Server nur an der ersten Adresse, Versuche nach Adresse; Sammelmail als Rohtext; Markierung mit allen Stilen; ESEARCH ohne UID. Siehe „Dritte Nachprüfung“.
- **Sicherheitsdurchsicht:** siehe Abschnitt „Sicherheitsdurchsicht 1.05“; zusätzlich ein bösartiger IMAP-Server (512-MiB-Literal: Abbruch nach dem Budget, Speicherspitze unter 96 MiB), ein SMTP-Server mit 200 000 Antwortzeilen, ein Server ohne UIDNEXT mit leerer INBOX, Mails mit 50 000 Teilen in der Liste.
- **API** (`api/tmail_test.go`): ohne Freigabe 403 an jeder Route (auch für den Besitzer); fremdes Postfach 404; Benutzer kann sich nicht selbst freigeben, Administrator nicht den Besitzer; jede T-Mail-Route lehnt In-Process-Aufrufe, Tokens und Aufrufe über den Cluster ab; keine Route in `publicRoutes`, kein Werkzeug im Katalog, keine Katalog-/Agent-Datei nennt T-Mail; Anhänge mit `attachment`/`nosniff`/`sandbox`, `cid` nur Bild; Speichern in NAS-Dateien mit Rechten (fremde Freigabe 404, vorhandene Datei abgelehnt); Senden mit Upload und NAS-Datei (fremde Datei 404, nichts versendet); Entwurf; Ereignis `flags` im Strom; Konto löschen hier und über `clusterApplied`, neues Konto erbt nichts; Diagnosepaket (entpackt) und Kontodatensatz des Clusters ohne Mail-Passwort.

Nicht geprüft: echte Anbieter (GMX, Gmail, iCloud …) und ihre Eigenheiten (Gmail-Ordner, APPENDLIMIT, Drosselung), ein erfolgreiches STARTTLS (nur die Ablehnung ohne TLS ist geprüft), der Datensatz, den der Cluster tatsächlich verschickt (geprüft ist der Kontodatensatz `auth.User`, den der Abgleich überträgt), Server ohne MOVE/UIDPLUS (Rückfall COPY+STORE+EXPUNGE nur im Code), Push-Zustellung an echte Geräte, die Oberfläche, ein echtes NAS mit Volume-Wechsel des Adressbuchs, ein App-Update mit offenen IDLE-Verbindungen auf echter Hardware.

Grenzen der ersten Fassung: Nach einem Neustart von titand ist ein Anmeldefehler vergessen (ein neuer Versuch je Start); Mails, die eintrafen, während titand nicht lief, melden sich nicht; `UID SEARCH` liefert bei sehr großen Ordnern alle UIDs (Liste je Anfrage, kein Zwischenspeicher); kein CardDAV, kein OAuth, nur Text beim Schreiben.

## Offen für das Backend

**Stand 17.11.2026 (Backend):** Punkte 1–4 sind gebaut – `mailAccounts` in `GET /users/{name}/agent-data`; `title` = echtes Ziel und Markierung abweichender Domains im Dokument; `meta.answers` bzw. Suche über `inReplyTo`; `attachFrom` ohne `uid` mit `draftUid` und `attachments` in der Antwort von `draft`. Einzelheiten in den Festlegungen oben.

**Stand 17.11.2026 (Oberfläche, `feat/v1.05-b`):** Die Oberfläche nutzt alle Punkte: Der Löschdialog eines Kontos nennt „T-Mail-Postfächer (n)“ aus `mailAccounts`; die Liste „Linkziele in dieser Nachricht“ warnt genau dort, wo titand `data-tmail-mismatch` setzt (keine eigene Prüfung, also kein Widerspruch zum „⚠“ im Dokument); eine Antwort schickt `meta.answers`; Entwürfe schicken hochgeladene Dateien und NAS-Dateien nur einmal und übernehmen danach alle Anhänge mit `attachFrom` ohne `uid` (Liste der Teile aus `attachments` der Antwort von `draft`; ein entfernter Anhang fehlt in der Liste). Außerdem: `tooLarge` erscheint in der Liste als „Mail zu groß zum Anzeigen“; `mail_busy` wird einmal nach `Retry-After` wiederholt (Senden, Entwurf, Anhang speichern); `mail_rate_limited` von jeder Route sperrt „Verbindung prüfen“ bis zum Ablauf, der Grund steht oben auf jeder Seite von T-Mail. Fehlertexte, `notify.kind.mail_new*` und `push.kindMail` stehen in `web/src/lib/i18n/features/mail.{de,en}.json` – nicht in anderen Sprachdateien noch einmal anlegen.

## Sicherheitsdurchsicht 1.05: Funde und Behebung

Zwei unabhängige Durchsichten des Backends (Stand `bb8ea52a`), behoben am 17.11.2026. Jeder Fund hat einen Regressionstest (`backend/internal/tmail/security_test.go`, `netguard_test.go`, `api/tmail_test.go`, `cmd/titand/wiring_mail_test.go`). Die Tests der Funde, die sich gegen den alten Code bauen lassen, schlagen dort fehl (geprüft mit einer Kopie von `bb8ea52a`); die übrigen prüfen neue Schnittstellen.

| Stufe | Fund | Behebung |
|---|---|---|
| HOCH | Der IMAP-Client (go-imap beta.9) begrenzt keine Literale, Texte und Zeilen; ein bösartiger oder kaputter Server ließ titand Gigabytes puffern, über die IDLE-Verbindung nach jedem Neustart wieder | Jede Verbindung (IMAP und SMTP, unter TLS) liest durch ein Budget je Befehl: 1 MiB für Anmeldung, 16 MiB für LIST/SEARCH/Köpfe/IDLE, für Teile die erwartete Größe plus 1 MiB; darüber wird die Verbindung geschlossen, `mail_too_large`. FETCH-Antworten Mail für Mail (`Next`), LIST bricht nach 500 Ordnern ab, UID-Mengen werden nie ganz ausgepackt (höchstens 100 000, neue Mails in Runden zu 100). Die Liste nimmt die Anhang-Markierung aus dem Kopf statt aus der Struktur; eine Mail mit zu großem Kopf erscheint als `tooLarge`. Der Wächter geht nach `mail_too_large` auf `unreachable` mit Pause. SMTP: 1 MiB und 2000 Zeilen je Gespräch |
| MITTEL | IPv6: Benutzer erreichten eigene öffentliche Adressen des NAS und Geräte im Heimnetz | `netguard.Confined`: keine Adresse in den Netzen der eigenen Schnittstellen (IPv6 mindestens /56), das NAS auf keinem Port, kein `2002::/16`, kein `64:ff9b:1::/48` – auch für `notify` und Link-Downloads |
| MITTEL | Port-Abtasten über gespeicherte Postfächer (Port ändern, Ordner abrufen, Code lesen) | Höchstens 10 Verbindungen je Minute und Konto zu Servern ohne erfolgreiche Anmeldung (alle Rollen); für Benutzer verschleierte Codes und keine Servertexte in jeder Antwort |
| MITTEL | Speicher bei parallelen großen Anfragen | Höchstens zwei große Anfragen je Konto, gekoppelt an das RAM-Budget von TitanOS (`MemoryPolicy.TryReserve`); ohne Platz sofort `503 mail_busy`. `PartBytes` ohne zusätzliche Kopie |
| NIEDRIG | Rollenwechsel ließ Verbindungen ins Heimnetz offen | Rolle im Fingerabdruck der Verbindung und der Konfiguration des Wächters; Rollenwechsel (hier und im Cluster) schließt die Verbindungen |
| NIEDRIG | Verwaiste Verbindung nach Entfernen von Postfach, Konto oder Freigabe während einer Anfrage | Am Ende jeder Anfrage geprüft und geschlossen; Freigabe entziehen schließt alle Verbindungen und Ströme; Downloads enden mit Anmeldung und Freigabe |
| NIEDRIG | Fehlender oder beschädigter Schlüssel: still ein neuer | Kein neuer Schlüssel, solange Passwörter liegen; `mail_key_missing`/`mail_key_damaged` (503) mit Protokollzeile |
| NIEDRIG | Unsichtbare und Bidi-Zeichen in Betreff, Namen, Dateinamen, Push | Unicode-Kategorie `Cf` (auch U+202A–202E, U+2066–2069) entfernt; Dateinamen höchstens 255 Byte mit Endung |
| NIEDRIG | Push-Nutzlast unbegrenzt | Titel 120, Text 240 Zeichen |
| NIEDRIG | Servertexte im Protokoll (der KI-Agent liest es) | Nur noch der Code |
| NIEDRIG | Glocke nannte den Postfachnamen, oft eine Adresse | „T-Mail“ oder ein selbst vergebener Name ohne `@`; Sammelmeldung mit Anzahl |
| NIEDRIG | Dateien ohne `Created` galten für jedes Konto des Namens | Ohne `Created` fremd für ein Konto mit `Created`; Passwörter neu als `v2:` mit der Anlegezeit in den Zusatzdaten (`v1:` wird weiter gelesen); Adressbuch ebenso |
| NIEDRIG | Beliebig viele Ereignisströme | Höchstens 8 je Konto, der älteste endet |
| NIEDRIG | Leere INBOX ohne UIDNEXT: `UID SEARCH UID *` wird abgelehnt, Wiederholschleife | Grundlinie mindestens UID 1 |

**Nachprüfung (Stand `c4d0eec7`):** 13 von 14 Funden behoben; nachgebessert:

| Stufe | Fund | Behebung |
|---|---|---|
| HOCH | Beim Herunterladen folgte das Budget der Größe, die der Server in der Struktur angab (bis 4 GiB), und blieb danach an der Verbindung im Pool stehen: eine unverlangte Zeile statt des Literals (Spitze 877 MiB) oder eine Flut nach dem Anhang (1096 MiB) | Vor jedem Abruf gilt das Budget eines Befehls; erst wenn das Literal ankommt, wird es auf dessen Größe plus 1 MiB gehoben (gestreamt, nie gepuffert; bei begrenzten Teilen höchstens das Dreifache der Grenze) und gleich danach zurückgesetzt. Nach jeder Anfrage setzt `withIMAP`, nach jedem Schritt der Wächter das Budget eines Befehls. Ein zu großer Teil schließt die Verbindung, statt den Rest zu lesen. Test mit beiden Wegen: Spitze unter 96 MiB (vorher 562/519 MiB) |
| NIEDRIG | `mail_busy` auf Geräten mit wenig RAM (pauschal 83 MiB je Senden/Entwurf, 8 MiB je Download, kein Warten) | Reservierung nach echter Größe, unter 1 MiB keine, Downloads ohne; bis 5 s Warten auf Platz |
| NIEDRIG | Grenze für neue Ziele traf nach jedem Start legitime Postfächer | Server gespeicherter Postfächer mit Passwort beim Laden bekannt; gleiche Versuche zählen einmal, eine Prüfung (IMAP+SMTP) einmal |
| INFO | Grenzen von `netguard.Confined` (/48-Anschlüsse, Rechenzentrum) | dokumentiert unter „Ziele je Rolle“ und in `docs/rules/SYSTEM.md` |

**Zweite Nachprüfung (Stand `c4066095`):**

| Stufe | Fund | Behebung |
|---|---|---|
| HOCH | `PartBytes` machte Platz nach der Größe aus der Struktur; `attachFrom` mit `"*"` hielt bei 100 × „25 MB“ mit je 8 echten Bytes 2,4 GiB; die Grenze von 100 Teilen galt vor dem Aufklappen von `"*"`; `part/save` mit Abstand zwischen Größe und Abruf | Höchstens 1 MiB im Voraus, gehalten wird nur die echte Länge (sonst Kopie); Grenze der Teile nach dem Aufklappen; die echten Längen werden gegen die Reservierung gerechnet und nachreserviert (`mail_busy`); `part/save` holt höchstens die reservierte Größe (`mail_too_large`). Test: gehalten unter 1 MiB statt 2,4 GiB |
| MITTEL | Das Budget galt je Verbindung, viele Postfächer eines Kontos addierten sich (20 × LIST knapp unter 16 MiB: 1,7 GiB; BODYSTRUCTURE 15 MiB: ~105 MiB je `Read`) | Höchstens vier IMAP-Befehle je Konto über alle Postfächer (auch die Schritte des Wächters), ab dem dritten auf dem NAS gekoppelt an das RAM-Budget (24 MiB je Befehl), sonst `mail_busy`, der Wächter pausiert; Budget eines Befehls 4 MiB (auch Wächter und LIST), Antworten mit BODYSTRUCTURE 1 MiB; neue Mails des Wächters in Runden zu 20, eine einzelne zu große Mail wird übergangen; Suche mit ESEARCH, wo vorhanden. Tests: 20 Postfächer parallel und acht `Read` mit 15-MiB-Strukturen bleiben unter 160 MiB bzw. 128 MiB Zuwachs (vorher 2,4 GiB bzw. 852 MiB) |
| NIEDRIG | Link-Markierung: Punycode, unsichtbare Zeichen, Look-alikes, Domain im Text, versteckte Markierung | siehe „Lesen“: Punycode in Ziel/Titel/Markierung, NFKC, Domain irgendwo im Text, gemischte Schriften immer markiert, Markierung mit `!important` |
| NIEDRIG | „Bekannter Server“ nach Name und Port | Bekannt nach erfolgreicher Anmeldung, nach Adresse nach DNS samt Port; beim Start nur die unveränderten Ziele gespeicherter Postfächer |
| INFO | `More()` wartete ohne Kontext | Wartet nur, solange die Anfrage lebt |

**Dritte Nachprüfung (Stand `971b19af`), behoben in 1.06:** Regressionstests in `backend/internal/tmail/review3_test.go` nach den Belegen der Prüfer (`poc4`).

| Stufe | Fund | Behebung |
|---|---|---|
| NIEDRIG | Gestreamte Downloads (`Part`, `CID`) hielten den Platz eines Befehls bis zu 10 min; der Sockel ohne RAM-Reservierung (2 Befehle) galt für das ganze NAS: bei ausgeschöpftem RAM-Budget blockierten zwei hängende Downloads eines Kontos alle anderen Konten (`mail_busy`) | Eigene Plätze für Downloads (4 je Konto, 16 auf dem NAS, ohne Reservierung); der Befehlsplatz geht beim Kopf des Literals zurück; der Sockel gilt je Konto (der erste laufende Befehl jedes Kontos ohne Reservierung); eine Anfrage wartet auf die Verbindung ihres Postfachs, bevor sie einen Befehlsplatz nimmt, und endet mit der Anfrage. Test: zwei und vier hängende Downloads, ein anderes Konto und ein anderes Postfach desselben Kontos gehen sofort, ein fünfter Download `mail_busy` |
| NIEDRIG | Ein Absender mit sehr großen Köpfen (≥ 20 Mails mit je > 200 KiB) machte jede Runde des Wächters zu groß; der Wächter wertete das als Verbindungsverlust mit wachsender Pause und verzögerte die Meldungen des Postfachs stundenlang | Nur `From`/`Sender`/`Subject`/`Date` und `To`/`Cc` per `HEADER.FIELDS` mit `Partial` (je 16 KiB) statt ENVELOPE; eine zu große Runde halbiert die nächste, drei gelungene verdoppeln wieder; keine wachsende Pause dafür. Tests: 25 Mails mit 250-KiB-Köpfen melden sich ohne jede Pause (Regel auf `To` und Betreff greift, Betreff dekodiert); ein Server, der `Partial` übergeht, braucht drei kurze Pausen statt wachsender |
| NIEDRIG | „Bekannter Server“ nach Name: ein beim Start geladenes Postfach galt für jede Adresse, zu der sein Name später zeigte; Versuche zählten nach Name, so dass wechselndes DNS in einer Minute beliebig viele Adressen erreichte | Geladene Ziele gelten nur für die erste aufgelöste Adresse; Versuche zählen nach Adresse samt Port. Der Test, dass nach einem Neustart mehr als zehn gespeicherte Postfächer nicht in die Grenze laufen, bleibt grün. Test: Name mit elf wechselnden Adressen, gespeichert und ungespeichert |
| INFO | Sammelmails mit sehr großer Struktur ließen sich nicht öffnen (`mail_too_large`) | Rückfall auf wenige Kopfzeilen und den Anfang von `BODY.PEEK[TEXT]` (1 MiB) mit `rawText:true` und Hinweis in der Oberfläche. Test mit einer Struktur über dem Budget; `test-ui-mail` zeigt den Hinweis |
| INFO | Die Link-Markierung erbte `letter-spacing`, `font-family`, `text-indent` des Absenders | Eigene Werte mit `!important` auch dafür (und Wortabstand, Groß-/Kleinschreibung, Schriftschnitt, Unterstreichung, Umbruch, Grundlinie). Test prüft jeden Wert am Span |
| INFO | Eine ESEARCH-Antwort ohne `UID` galt still als leer | Erneut ohne ESEARCH; ist auch das ohne UIDs, Fehler statt leerer Liste. Test mit beiden Fällen |
| (eigener Fund) | Brach der Browser einen Download ab (oder schickte ein Server mehr als angefragt, oder eine Antwort ein Literal, nach dem niemand fragte), schloss T-Mail die Verbindung aus dem lesenden Ablauf heraus; go-imap wartet dabei, bis das ausgegebene Literal gelesen ist – die Anfrage hing für immer und hielt die Verbindung des Postfachs (gleiches bei mehr als 500 Ordnern) | `abandon`: Verbindung im Hintergrund schließen, den Rest der Antwort (dann sofort fehlschlagend) weglesen; nicht angefragte Literale werden im Budget eines Befehls weggelesen. Der Download-Test bricht seine hängenden Downloads am Ende ab und endet (vorher hing er) |
| (eigener Fund) | Ein Browser, der einen Download nicht mehr las, aber die Verbindung offen hielt, hielt Download und Verbindung des Postfachs, bis TCP aufgab: die Zeitgrenze von 10 min schloss nur die IMAP-Seite, das Schreiben an den Browser hatte keine | Schreib-Frist am Download (`/part` 10 min, `/cid` 120 s). Test `api/TestMailStalledDownloadLetsGo`: 48 MiB, der Browser liest nach dem Kopf nicht weiter, dasselbe Postfach antwortet nach der (im Test auf 1 s gekürzten) Frist; ohne Frist nach 15 s noch nicht |

Gegenprobe: Die Tests für Downloads, Runden, bekannte Server, Rohtext, Markierung und ESEARCH schlagen gegen `a929224c` (Stand 1.05) fehl bzw. hängen (geprüft in einem eigenen Arbeitsbaum).


**Prüfung der Änderungen für 1.06 (Stand `d7126153`):** Urteil „kann veröffentlicht werden“, kein hoher oder mittlerer Fund. Die drei kleinen Punkte sind gleich in 1.06 behoben (Regressionstests in `security_test.go`, die ersten beiden schlagen mit `d7126153` fehl):
- **A (niedrig):** Nach dem Literal eines Downloads (Befehlsplatz und Reservierung schon zurückgegeben) gilt nur noch `budgetTail` = 64 KiB statt `budgetCommand`; ein großer, nicht angefragter String danach beendet die Verbindung (`TestDownloadTailHasASmallBudget`).
- **B (niedrig):** Bis das Literal beginnt, hat ein Download die Zeit eines Befehls (`partHeadTimeout` = `commandTimeout`, 60 s), erst danach `PartTimeout` (10 min); ein Server, der vorher hängt, hält Befehlsplatz und Reservierung nicht mehr zehn Minuten (`TestDownloadHeadHasTheTimeOfACommand`).
- **C (Info):** Die Link-Markierung trägt `direction:ltr; unicode-bidi:isolate` mit `!important`; ein `<bdo dir="rtl">` des Absenders spiegelt sie nicht (`TestLinkMarkIsNotMirrored`).
- Offen als Info: ein Absender mit unbekanntem Zeichensatz im Namen fällt aus den Meldungs-Regeln (Regel „Absender enthält …“ greift dann nicht); wechselndes DNS großer Anbieter kostet Versuche der Bremse, bis eine Anmeldung sie bekannt macht.