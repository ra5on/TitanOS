# Mehrere TitanOS verwalten (Entwurf)

Ein TitanOS zeigt den Zustand weiterer TitanOS im eigenen Netz bzw. Tailnet und
führt mit einem Klick in deren Oberfläche. Es gibt keinen zentralen Server und
keinen Cloud-Dienst: Jedes NAS kann „Leitstelle“ sein, jedes kann sich von
einer oder mehreren Leitstellen beobachten lassen. Arbeitsname „Meine NAS“.

## Ziele und Grenzen

- Auf einen Blick: Welches NAS ist erreichbar, gesund, voll, braucht ein Update,
  hat Fehler gemeldet, hat ein überfälliges Backup?
- Ein Klick öffnet die Weboberfläche des anderen NAS (eigene Anmeldung dort).
- Stufe 1 liest nur. Ändernde Aktionen (Update installieren, Neustart) erst in
  Stufe 2 und nur ausdrücklich freigeschaltet.
- Keine Fernsteuerung von Dateien, Konsolen, Konten, Netzwerk oder Firewall –
  dafür öffnet man das andere NAS selbst.
- Keine Platten wecken, kein zyklischer Stickzugriff, auf beiden Seiten.

## Koppeln

Wie beim Backup-Ziel „Anderes TitanOS“ über einen Kopplungscode, nie über
eingetippte Passwörter:

1. Auf dem **beobachteten** NAS: Einstellungen › Meine NAS › „Von einem anderen
   NAS beobachten lassen“. Das NAS legt einen persönlichen Token des
   angemeldeten Besitzers/Administrators mit dem neuen Bereich `fleet:read` an
   (zählt zu den 32 Tokens des Kontos, in der Tokenliste sichtbar und
   widerrufbar) und zeigt **einmal** den Code
   `titanos-fleet:` + base64url-JSON {Name, Adressen, SHA-256-Fingerabdruck des
   Zertifikats, Token}.
2. Auf der **Leitstelle**: „NAS hinzufügen“, Code einfügen. Die Leitstelle
   verbindet sich sofort, prüft Fingerabdruck und Token und zeigt Name und
   Version zur Bestätigung.

Adressen im Code: Tailnet-Name/-Adresse (bevorzugt), NetBird, Heimnetz-IPs.
Verbindungen nur zu Heimnetz/Tailscale/NetBird (`netguard.Local`), nie ins
Internet, keine Weiterleitungen. Das Zertifikat wird per Fingerabdruck
gepinnt (wie bei Home Assistant); ein gültiges Let's-Encrypt-/Tailnet-
Zertifikat ersetzt das Pinning nicht stillschweigend, ein Wechsel muss bestätigt
werden („Zertifikat hat sich geändert“).

Ablage auf der Leitstelle: `/config/fleet/nodes.json` (Name, Adressen,
Fingerabdruck, Reihenfolge, Notiz), Token in `/config/fleet/keys.json` (nur
„ist gesetzt“ über die API, nie im Diagnosepaket/Assistenten-Katalog, Ordner im
Stick-Dateimanager ausgeblendet). Geschrieben nur beim Hinzufügen, Ändern,
Entfernen.

Entfernen auf der Leitstelle löscht nur ihren Eintrag; den Token widerruft man
auf dem beobachteten NAS (Knopf „Beobachtung beenden“ zeigt dort alle
`fleet:read`-Tokens mit letzter Nutzung).

## Neue Route `GET /api/v1/fleet/summary`

Eine Anfrage je NAS statt vieler, Bereich `fleet:read`, in `publicRoutes`
(damit auch OpenAPI/`titanosctl`). Nur Werte aus dem RAM von titand:

- Name, Version, Basis, Slot, Laufzeit, Zeitzone
- Update: verfügbar (Version), läuft, auf Probe, letztes Ergebnis
- CPU %, RAM belegt/verfügbar, CPU-Temperatur
- Volumes: Name, Zustand (ok/degradiert/Wiederaufbau/gesperrt/fehlt), belegt/gesamt
- Platten: Anzahl, mit SMART-Warnung/-Fehler, schlafend
- Meldungen: Anzahl Warnungen/Fehler der letzten 24 h, die neuesten fünf
  (Art, Stufe, Betreff, Zeit – ohne Chatinhalte)
- Backups: Aufträge gesamt, fehlgeschlagen, überfällig
- USV: Zustand, Akku
- Docker/VMs/LXC: laufend/gesamt
- Firewall an/aus, Fernzugriff (Tailscale/NetBird/WireGuard) an/aus

Ältere NAS ohne diese Route: Rückfall auf `/system` und `/storage` mit
Hinweis „Ältere Version – eingeschränkte Ansicht“. Erfordert dann einen Token
mit `system:read`; der Code enthält in Stufe 1 deshalb beide Bereiche.

## Abfragen

- Seite „Meine NAS“ offen: alle 15 s, parallel, Frist 5 s je NAS, Ergebnis nur
  im RAM. Unerreichbar = letzter Stand grau mit Zeitpunkt, nie als „ok“.
- Hintergrundbeobachtung gehört zu Stufe 1 (Entscheidung des Projektinhabers
  vom 29.10.2026), ab Werk **aus**. Eingeschaltet: alle 5 min; Meldung
  `fleet_unreachable` nach 3 Fehlschlägen am Stück (einmal je Ausfall),
  `fleet_problem` für neue Fehler/degradierte Volumes/überfällige Backups des
  anderen NAS (Betreff = NAS-Name). Damit reicht es, Benachrichtigungswege auf
  einem NAS einzurichten.
- Dashboard-Kachel `fleet` nutzt dieselben RAM-Werte der Leitstelle.

## Oberfläche

- Eigener Reiter **„NAS“** in der Kopfzeile (am Ende nach „Werkzeuge“), nur für Besitzer/Administratoren und nur, wenn
  mindestens ein NAS gekoppelt ist.
  Ohne Kopplung erreicht man die Einrichtung über Einstellungen › Meine NAS.
- Seite `/fleet`: oben Zusammenfassung („4 NAS · 3 in Ordnung · 1 Warnung“),
  darunter je NAS eine Karte (Raster wie Docker: Karten/Liste):
  - Kopf: Statuspunkt mit Text, Name, Version, „Update 0.37.0“-Chip
  - Volumes als schmale Balken mit Zustand
  - Zeile CPU / RAM / Temperatur / Laufzeit
  - Zähler: Meldungen, Backups, Docker, VMs
  - Knöpfe: „Öffnen“ (neuer Tab, bevorzugte Adresse), ⋯-Menü (Adresse wählen,
    Umbenennen, Notiz, Entfernen)
- Das eigene NAS steht als erste Karte („Dieses NAS“) mit derselben Darstellung.
- Sortierung: Probleme zuerst, dann gespeicherte Reihenfolge.
- Mobil: Karten untereinander, Volumes eingeklappt.

## Stufe 2 (gewünscht, Entscheidung des Projektinhabers vom 29.10.2026; nach Stufe 1)

- Bereich `fleet:write` (beim Koppeln eigenes Häkchen, ab Werk aus):
  Update installieren, Neustart/Herunterfahren mit denselben Dialogen und
  Wartebedingungen wie lokal; Aktivitätsprotokoll des anderen NAS mit
  Herkunft „per Leitstelle <Name>“.
- Anmelden ohne zweites Passwort beim Öffnen: Einmal-Link (60 s, nur RAM, an
  Konto und Leitstelle gebunden, nur mit `fleet:write`), meldet in eine
  normale Sitzung desselben Kontos auf dem anderen NAS an; Aktivitätsprotokoll
  „per Leitstelle“. Gesperrte Konten und widerrufene Tokens wirken sofort.

## Prüfung

Go-Tests mit nachgebauten NAS (`httptest` + Fake-Summary): Kopplung,
Fingerabdruck-Wechsel, Rückfall auf alte Version, Fristen, Meldungen nur bei
Zustandswechsel, keine Tokens in Fehlertexten. Browserprüfung
`scripts/test-ui-fleet.mjs` mit isolierten Daten. Zwei echte NAS über Tailscale
bleiben abzunehmen.

## Entscheidungen des Projektinhabers (29.10.2026)

1. Eigener Reiter „NAS“ am Ende der Navigation nach „Werkzeuge“.
2. Hintergrundbeobachtung mit Meldungen gehört zu Stufe 1, ab Werk aus.
3. Stufe 2 (Updates/Neustart aus der Ferne, Anmeldung ohne zweites Passwort)
   ist gewünscht und folgt nach Stufe 1.
