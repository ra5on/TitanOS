# T-Mail – Vertrag (1.05)

Eigenes E-Mail-Programm in TitanOS (PLAN §81, Inhaber 13.11. „Eigenes T-Mail“, Fragerunde 16.11.2026). Dieses Dokument ist der Vertrag zwischen Backend (`backend/internal/tmail`, `backend/internal/api/tmail*.go`) und Oberfläche (`web/src/routes/mail/`, `web/src/lib/tmail*`). Abweichungen werden hier eingetragen, bevor sie gebaut werden.

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
- **Ziele:** beliebiger Host und Port, aber nicht Loopback (`127.0.0.0/8`, `::1`), nicht Link-Local (`169.254.0.0/16`, `fe80::/10`) und nicht die eigenen Adressen des NAS auf Ports von titand. Heimnetz-Adressen sind erlaubt (eigener Mailserver im LAN). Geprüft nach DNS-Auflösung, beim Verbinden.
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
| `GET /accounts` | Postfächer des Kontos: `[{id,name,email,user,imap:{host,port,security},smtp:{host,port,security},passwordSet,notify,signature,status:{state,code,since},unread}]`; `state` ∈ `ok`,`connecting`,`auth_failed`,`unreachable`,`off` |
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
| `GET /settings` · `PUT /settings` | `{signatures:[{id,name,text,default:{accountId}}],rules:[{id,account,enabled,when:{from,to,subject},then:{moveTo,markRead,flag}}]}` |
| `GET /events` | Server-Sent Events für die offene Oberfläche: `mail` (neue Mail `{account,folder,uid}`), `status` (Postfach-Status), `flags` |

**Neue Fehlercodes** (je mit Text Deutsch/Englisch): `mail_disabled` (App nicht freigegeben), `mail_auth_failed`, `mail_unreachable`, `mail_tls` (kein TLS/Zertifikat ungültig), `mail_host_forbidden`, `mail_timeout`, `mail_too_large`, `mail_not_found`, `mail_folder_exists`, `mail_send_failed` (mit Antwort des Servers ohne Geheimnisse).

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
