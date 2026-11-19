# TitanOS

Ein schlankes NAS-Betriebssystem, das vom USB-Stick bootet und komplett im RAM läuft – inspiriert von Unraid, aber mit klassischem RAID. Wird auf GitHub veröffentlicht.

> **Diese Datei enthält nur Kernregeln; Fachregeln stehen in `docs/rules/` bzw. den verlinkten Dokumenten. Vor Arbeiten an einem Bereich dessen Regeldatei lesen. Neue Regeln in die passende Regeldatei, nicht hier anhängen.** Verzeichnis: Abschnitt „Fachregeln“ unten und `docs/rules/README.md`.

Stand: **1.16** (Tag v1.16.0, App-Update auf Basis 0.82: Android-Apps koppeln unter Mein Konto › Geräte – Code als QR, Link oder von Hand mit Fingerabdruck, Bestätigung mit Kontopasswort, Geräteschlüssel P-256 (nur der öffentliche Teil auf dem Stick, `appdevices`), die App ist eine Sitzung wie der Browser und holt sie mit einer unterschriebenen Herausforderung, sperrt sich nicht von selbst, gilt nach „Angemeldet bleiben“, `docs/ANDROID-APP.md`; die App entsteht in `ra5on/TitanOS-Android` –, siehe docs/releases/v1.16.0.md; 1.15: Tag v1.15.0, App-Update auf Basis 0.82: Mail, Kalender und Kontakte gestaltet wie bei Apple, am Desktop wie am Handy und nach Breite/Höhe des Fensters – Mail mit „Bearbeiten“, Leiste unten, Filter-Blatt, „Alle Posteingänge“/„Markiert“ (`/mail/unified`), Postfächer einklappbar und sortierbar (`order`: `mailAccounts`, `mailHidden`), Kalender mit durchlaufenden Monaten, Terminbalken, Zeile „Ganztägig“, Übergängen (nur mit `data-fx-transitions`), Jahr und Suche, Kontakte mit Register, „Meine Karte“ (Einstellungsart `contacts`), Teilen als vCard; KI-Agent: Löschen/Verschieben fragen mit Karte statt Entwurf (`mail_delete`), Hinweis „noch nicht geschehen“ bei falscher Erfolgsmeldung (`ClaimsDone`), Links aus gelesenen Mails nach den Web-Rechten; Kontakte nach iPhone-Import: ein „#“, Liste nach Seitenwechsel, BOM –, siehe docs/releases/v1.15.0.md; 1.14: Tag v1.14.0, App-Update auf Basis 0.82: KI-Agent nach Rückmeldungen des Inhabers – Mail, Kalender und Kontakte werden vorab geladen, wenn die Nachricht davon handelt (`agent/wanted.go`), ein gesperrter Bereich nennt seinen Grund statt „kann ich nicht“, derselbe Entwurf wird je Runde nur eine Karte, neue Abläufe für Mail/Termin/Kontakt, die 47 mitgelieferten Skills auf Deutsch (`BuiltInSkillsDE`, Seite Wissen und `Skills/TitanOS/`), die arbeitenden Punkte bleiben im Bild, wenn die Chatfläche niedriger wird (Tastatur zu, Leiste wieder da) –, siehe docs/releases/v1.14.0.md; 1.13: Tag v1.13.0, App-Update auf Basis 0.82: Mail, Kontakte und Kalender überarbeitet nach Screenshots des Inhabers – angezeigt als „Mail“/„Kalender“ statt „T-Mail“/„T-Kalender“, gemeinsames Spaltengerüst `ColumnFrame` randlos wie der KI-Agent (Seitenleiste/Liste ziehbar und einklappbar, ersetzt §74 für diese drei; Sortieren per Ziehen, Reihenfolge je Konto als Einstellungsart `order`), echte Handy-Gestaltung (Ebenen, Leiste unten, HTML-Mail eingepasst), Kontakte mit Gruppen (iOS-Form) und Dubletten/Zusammenführen, Kalender mit Ziehen/Länge, Serien-Abfrage, Hinweisen zu Erinnerungen und Erinnerungen (VTODO, eigene Store-Art `tasks` unter `.titanos/tasks`), Lesen ohne Sammlungssperre, Live-Strom `/calendar/stream`, ETag/304, zweite IMAP-Verbindung, iPhone-Profil (signiert, QR-Einmal-Link, Feiertage), KI-Agent mit Mail-/Erinnerungs-Werkzeugen, Rechten je Aktion (`actionRights`, Standard „Nur Vorschläge“ als Entwurf, Konto darf verschärfen) und Panel in Mail/Kontakten/Kalender; Durchsichten `docs/validation/sicherheit-113.md`, Messungen `docs/validation/performance-113*.md` –, siehe docs/releases/v1.13.0.md; 1.12: Tag v1.12.0, App-Update auf Basis 0.82: T-Mail überarbeitet nach dem Screenshot des Inhabers – Spalten ziehbar und je Gerät gemerkt, keine Leiste über den Spalten („Neue E-Mail“ in der Ordnerspalte, Neuladen über IDLE bzw. alle 2 Minuten), Vorschau von höchstens 140 Zeichen an einer Wortgrenze vom Server (nie gekürzt dargestellt), Symbole inkl. weitergeleitet, Gruppen Heute/Gestern/Diese Woche/Älter, Unterhaltungen, Schnellaktionen und Wischen, Mehrfachauswahl und Ziehen nur innerhalb eines Postfachs, Tastenkürzel, Suche über alle Ordner mit Filtern (Grenzen 100 Ordner, 50 Treffer, eine Suche je Konto), Favoriten und Einklappen, Übersicht im leeren Lesebereich, Absenderbilder aus den Kontakten; Sicherheitsdurchsicht `docs/validation/sicherheit-112.md`, `docs/TMAIL.md` „1.12“ –, siehe docs/releases/v1.12.0.md; 1.11: Tag v1.11.0, App-Update auf Basis 0.82: T-Kalender und Kontakte – zwei Programme je Konto, ab Werk für jedes Konto sichtbar, Abgleich mit dem iPhone über eigenes CalDAV/CardDAV (`davsync`, kein go-webdav) mit Gerätepasswort je Gerät (nur `/caldav/`, `/carddav/`, `/.well-known`, Vierergruppen, Admins entziehen fremde, legen keine an), Daten unter `.titanos/calendar|contacts` auf dem Systemdaten-Volume, ohne Volume immer 503 statt leer (Marke `users/<konto>/pim.json`), Wiederholungen hinter einer Schutzschicht, Windows-Zonen, Geburtstage aus den Kontakten, T-Mail schlägt Kontakte zuerst vor; nicht über MCP/Tokens; KI-Agent mit neun Werkzeugen und eigenen Rechten `dataRights` (Lesen/Eintragen; ganze Reihe und Löschen nach fremdem Inhalt fragen immer), Home Assistant höchstens lesen (`assistData`); Sicherheitsdurchsicht mit unabhängiger Nachprüfung S-1…S-28 (`docs/validation/sicherheit-111.md`; Assist liest Kalender/Kontakte nur mit Zustimmung des Kontos selbst, Gerätepasswörter an die Zugangsdaten gebunden), `docs/KALENDER.md`; nicht an einem echten iPhone geprüft –, siehe docs/releases/v1.11.0.md; 1.10: der KI-Agent weiß bei typischen Fragen, was er tun soll – Regeln für leere Ergebnisse, Widerspruch, T-Mail und Einstellungen, Werkzeug `agent_access` mit den eigenen Rechten, `hidden`/`why` in allen filternden Werkzeugen, Abläufe nur mit Name und „Wann“ (Schritte über `skill_read`), zehn neue und reparierte Abläufe, neue Lesewerkzeuge für Aktivitätsprotokoll, Monitoring-Verlauf, Kapazitätsprognose, NVR-Ereignisse ohne Bilder, HA-Logbuch und Versionshinweise, ein Werkzeug je Aufgabe, Updates prüfen ohne Nachfrage, Grundlast gemessen 13 400 → 10 300 Token –, siehe docs/releases/v1.10.0.md; 1.09: sechs Fehler des KI-Agenten aus dem Chat-Export des Inhabers – Nachdenken nie in der Antwort (auch `<think>`, Harmony als Text, unmarkiert), Automationen, Skripte und Szenen als eigene Geräteart mit „nicht freigegeben“ statt stiller 0, Home-Assistant-Fragen nie per Dateisuche, PDF-Export ohne „?“ mit Schritten in Worten, alte Titel mit „ …“ als ganze Frage, Fokus bleibt am Arbeitsplatz nach dem Senden im Eingabefeld –, siehe docs/releases/v1.9.0.md; 1.08: T-Mail erscheint mit Freigabe im Menü – `/services/state` meldet `mail`, siehe docs/releases/v1.8.0.md; 1.07: ohne NAS-Verbindung zeigen TitanOS und die Agent-App den zuletzt gesehenen Stand – Service Worker hält die Seite, eine feste Liste unkritischer Werte je Konto, Änderungen scheitern sofort statt in eine Warteschlange, `docs/PWA.md` –; Fotos im KI-Agenten über Immich mit eigenem Schlüssel je Konto, Bereich „Fotos“ nur lesen, Vorschaubilder nur über eine geprüfte Route; Anleitung „Fotos vom iPhone sichern“; Chat in voller Breite, ziehbare Seitenleiste, Schweif über dem Text, Chat-Titel vom Modell, Punkte auf dem Compositor, siehe docs/releases/v1.7.0.md; 1.06: T-Mail nach der dritten Nachprüfung – Downloads mit eigenen Plätzen und Sockel je Konto, Wächter holt nur wenige Kopfzeilen und halbiert zu große Runden ohne wachsende Pause, gespeicherte Server nur an ihrer ersten Adresse, Sammelmails als Rohtext, abgebrochene Downloads hängen nicht mehr –, Figur des KI-Agenten im Widget in Farbe und Größe der Einstellungen, vier wackelige Tests stabil, siehe docs/releases/v1.6.0.md; 1.05: T-Mail – eigenes E-Mail-Programm je Konto, IMAP/SMTP mit Passwort, Freigabe je Konto ab Werk aus, kein Zugriff für den KI-Agenten, zwei Sicherheitsdurchsichten, `docs/TMAIL.md` –; KI-Agent: drei Punkte flach mit Choreografie und wählbarer Größe, „Geplant“ zählt nur Aktives, Denkstufe aus den Einstellungen gilt sofort, Update-Frage nennt die laufende Version, siehe docs/releases/v1.5.0.md; 1.04: Rest der Durchsicht – jeder Fund behoben, begründet gelassen oder entschieden; Ersetzen beim Hochladen in den Papierkorb, geordneter Start nur für vorher laufende Container, Abbilder von NVR/Sprache beim Aufräumen verschont, siehe docs/releases/v1.4.0.md; 1.03: Entscheidungen zur Durchsicht – Konto löschen nimmt die Agent-Daten mit, gesperrte Anmeldung bleibt nach Neustart gesperrt, Fehler am Arbeitsplatz als kurzer Hinweis –, Zeitgrenzen für Docker/libvirt/Freigaben, halb angelegte LXC-Container, Geheimnisse nur aus dem Browser, Tastatur in Reitern und Menüs, siehe docs/releases/v1.3.0.md; 1.02: Durchsicht der ganzen Codebasis in fünf Bereichen – 228 Funde, 123 behoben, Fundlisten docs/validation/durchsicht-102-*.md –, siehe docs/releases/v1.2.0.md; 1.01: mehrere Chats des KI-Agenten zugleich mit wählbarer Grenze (ab Werk ohne), Schnappschuss beim Öffnen eines laufenden Chats, `ChatView` in Teile zerlegt, Figur mit gleich großen Kugeln und weichen Übergängen, übrige Funde der Durchsicht von 0.99 behoben, siehe docs/releases/v1.1.0.md; 1.00: Versionen ab jetzt x.xx – nach 0.100 kommt 1.00, dann 1.01 –, die Fernansicht läuft nach einem Update mit dem bisherigen Programm weiter und lädt ein neues nur, wenn sich seine Module ändern, langer Chat-Titel am Handy bricht um statt „…“, siehe docs/releases/v1.0.0.md; 0.100: die arbeitende Figur des KI-Agenten als drei Kugeln im Raum, je Tätigkeit eine eigene Bewegung, 33 neue Sprüche, weichere Übergänge im Chat, siehe docs/releases/v0.100.0.md; 0.99: Durchsicht des KI-Agenten – Ordnerregeln auch für den Ordner darüber, Bildansicht und Hilfs-Chats unter den Rechten, MCP fragt nach gelesenen Inhalten, Ergebnisse nach Freigabe in der richtigen Reihenfolge, Chat hängt sich nach Verbindungsabbruch wieder an, Aufträge nicht doppelt –, Funde und Offenes in docs/releases/v0.99.0.md; 0.98: der Pfad einer Seite steht nur noch einmal, in der Fensterleiste und anklickbar; „Internet auch nach gelesenen Inhalten ohne Nachfrage“ gilt auch mit Skills von außen und markiertem Gemerkten; Anzeigename für das Modell des KI-Agenten; Hochscrollen während der Antwort, siehe docs/releases/v0.98.0.md; 0.97: der KI-Agent liest beim Zurückkehren in die App Zustand und Chat neu und lädt eine neue Version der Oberfläche, die Container-Karte hat aufklappbare Zeilen mit mehr Aktionen, das neue Symbol liegt unter eigener Adresse, siehe docs/releases/v0.97.0.md; 0.96: externe Anbieter für den KI-Agenten – OpenAI-kompatible Dienste oder Claude statt Ollama, ab Werk aus, Schlüssel bleibt auf dem NAS, „Cloud“ im Chat –, siehe docs/releases/v0.96.0.md; 0.95: Schnellbefehle des KI-Agenten – Schalten, Zustand und Erinnerungen ohne Modell, mit denselben Rechten – und Sprechmodus (Freihand-Gespräch mit gezeichneter Figur, Ins-Wort-Fallen, wahlweise Weckwort), Karten unter der Antwort (Container, Gerät, „In T-Word öffnen“), neues Symbol „Kern und Ringe“, die denkenden Punkte bewegen sich wieder, siehe docs/releases/v0.95.0.md; 0.94: die arbeitenden Punkte des KI-Agenten bilden je Tätigkeit eine Figur mit passendem Wort, am Ende ein Haken; reine Web-Recherchen fragen nicht mehr bei jedem Schritt, Schalter „Internet auch nach gelesenen Inhalten ohne Nachfrage“ unter Rechte (ab Werk aus), siehe docs/releases/v0.94.0.md; 0.93: die arbeitenden Punkte des KI-Agenten wechseln ihre Form, das Feld der eigenen Websuche nennt die Adresse mit http:// und Port, siehe docs/releases/v0.93.0.md; 0.92: Arbeitsplatz des KI-Agenten – die Code-Sandbox hat Bibliotheken für Word, Excel und PDF, einmal geprüft geladen, Läufe weiter ohne Netz –, Arbeitsweise je Chat „Direkt loslegen / Plan zeigen / Jeden Schritt fragen“, eigene Websuche über SearXNG (Katalog-App, Port 8580), Verbindungen zu MCP-Servern (`docs/AGENT.md`), 34 mitgelieferte Abläufe (durchgesehen: keine Erlaubnisfragen im Chat mehr, die Rechte entscheiden), siehe docs/releases/v0.92.0.md; davor 0.91: Codeblöcke des KI-Agenten in Farbe und zum Vergrößern, seine Seiten, Dialoge und Blätter in den Formen des neuen Chats, siehe docs/releases/v0.91.0.md; davor 0.90: KI-Agent in neuer Ansicht nach dem Vorbild der Chat-Apps – am Handy schwebende Knöpfe statt Kopfleiste, Leiste unten mit Chat/Projekte/Aufträge/Notizen, Eingabefeld als abgerundetes Feld mit Modell-Chip, Schritte als Zeile mit Blatt „Zusammenfassung“, Tabellen bleiben Tabellen, Codeblöcke mit Kopf, wechselnde Begrüßung, Kopf beginnt tiefer unter der Statusleiste des iPhone; siehe docs/releases/v0.90.0.md; davor 0.89: Titel im Kopf des KI-Agenten am Handy als schlichter Text, Tabellen in Antworten ohne Umbruch mitten im Wort; davor 0.88: Home-Assistant-Rechte des Agenten je Geräteart mit „Ansehen“ und „Schalten: Ja/Nachfragen/Nein“, Lesen fragt wegen Geräte-Regeln nie nach, kein Glas im Agenten am Handy; davor 0.87: Systemsteuerung am Handy in Ebenen mit Pfeil eine Ebene zurück, kurze Agent-Startseite am Handy, Home-Assistant-Rechte des Agenten mit „Nichts/Auswahl/Alles“, siehe docs/releases/v0.87.0.md; davor 0.86, App-Update auf Basis 0.82: keine Push-Mitteilung mehr für eine Antwort im offenen Chat des KI-Agenten, siehe docs/releases/v0.86.0.md; davor 0.85, App-Update auf Basis 0.82: Korrekturen an Sprachdiensten und KI-Agent am Handy – Home Assistant mit Assist-Schlüssel erreicht die Wyoming-Anschlüsse, „läuft“ erst wenn der Dienst antwortet, neuer Chat ohne Startseite, Einstellungen nur noch im Menü, Handy-Seiten ohne Hintergrundbild, siehe docs/releases/v0.85.0.md; davor 0.84, App-Update auf Basis 0.82: Sprachdienste – Whisper und Piper in verwalteten Containern für Home Assistant und den Agenten, `docs/SPEECH.md` –, Assist mit Schlüssel, eigenen Rechten je Bereich und eigenem Profil, siehe docs/releases/v0.84.0.md; davor 0.83, App-Update auf Basis 0.82 – 0.82 war das erste Sicherheits-Release des Autopiloten vom 14.11.2026 –: KI-Agent aufgebaut wie die Systemsteuerung – Seiten statt Dialoge, Startseite mit NAS-Zustand, Gedächtnis mit Anheften, Bearbeiten/Neu erzeugen/Suche/„/“-Befehle im Chat, einzeiliger Kopf am Handy –, Ransomware-Wächter repariert, Tasten der Fernansicht, siehe docs/releases/v0.83.0.md; davor 0.81: Systemsteuerung mit aufklappbaren Bereichen und Suche über jede Einstellung, Handy-Oberfläche wie am Desktop mit Gesten und VM-Bedienung per Touch, NVR-Zonen/Masken, Ereignisse behalten, getrennte Aufbewahrung, „gesehen“ je Konto, T-PDF am Handy mit Office 1.7.0; Prüfstand von 0.81 in docs/releases/v0.81.0.md, 0.80 in docs/releases/v0.80.0.md, NVR-Release 0.78 in docs/releases/v0.78.0.md, Fundliste der Durchsicht in docs/validation/durchsicht-076.md; 0.67.0 und 0.68.0 wurden nicht veröffentlicht). Diese Datei und die Regeldateien beschreiben die aktuell geltenden Entscheidungen, nicht die Reihenfolge ihrer Einführung. Historische Release- und Testberichte stehen in `docs/releases/` und den Fachdokumenten. Bei Änderungen die betroffene Regel dort aktualisieren, wo sie steht, statt widersprechende Nachträge anzuhängen.

Eigenständiges Projekt. Keinen Code, keine Struktur und keine Annahmen aus anderen Projekten übernehmen.

## Festgelegte Entscheidungen

| Bereich | Entscheidung |
|---|---|
| Basis | Debian 13 (trixie) minimal, amd64 |
| Boot | 64-Bit-UEFI mit systemd-boot; seit 0.41 zusätzlich GRUB für 32-Bit-UEFI und Legacy-BIOS mit denselben A/B-Einträgen (`docs/BOOT.md`); Secure Boot aus |
| Stick | FAT32, Label `TITANOS` |
| Systemabbild | squashfs (zstd), wird beim Boot **komprimiert** in den RAM (tmpfs) kopiert, darüber overlayfs mit größenbegrenzter tmpfs-Schicht (512 MiB) |
| Nach dem Boot | Stick wird nur noch für Konfigurationsänderungen und Updates beschrieben |
| Updates | ein Stable-Kanal; volle Systemupdates in A/B-Slots mit Boot-Rückfall, kompatible App-Updates nur für titand/Weboberfläche ohne NAS-Neustart mit eigenem Rückfall |
| Auslieferung | Zip zum Kopieren auf einen FAT32-Stick. Kein Pflicht-`.img` (optional später im CI) |
| Speicher | klassisches RAID per **mdadm** (0, 1, 5, 6, 10) mit ext4/XFS (umgesetzt); Btrfs mit den Profilen single, RAID 1 und RAID1C3, Snapshots, Platten hinzufügen/entfernen und Profilwechsel (umgesetzt, siehe docs/BTRFS.md); ZFS ggf. später |
| Backend | **Go**, ein einzelnes statisches Binary (`titand`) |
| Frontend | **Svelte + TypeScript**, als statische Dateien per `go:embed` ins Binary eingebettet |
| Veröffentlichung | GitHub: https://github.com/ra5on/TitanOS (öffentlich), Releases per GitHub Actions |
| Lizenz eigener Code | **FSL-1.1-ALv2** (Functional Source License, Fair Source): alles erlaubt außer konkurrierender kommerzieller Nutzung (Verkauf als Produkt, Angebot als Dienst); jede Version wird 2 Jahre nach Erscheinen Apache-2.0. |

## Lizenz und Beiträge

- `LICENSE.md` enthält den unveränderten Text der FSL-1.1-ALv2 (offizielle Vorlage von fsl.software) mit `Copyright 2026 ra5on`. Lizenztext nie umformulieren.
- Bezeichnung: „Fair Source“. Nicht „Open Source“ nennen, solange eine Version noch nicht zu Apache-2.0 gewechselt ist. Lizenzhinweis in README und unter „Einstellungen › Über“.
- Jede Release-Seite nennt das Datum, ab dem diese Version unter Apache-2.0 steht (Erscheinungsdatum + 2 Jahre).
- Fremdkomponenten behalten ihre Lizenz (Linux-Kernel, Debian-Pakete: GPL u. a.; noVNC: MPL-2.0; xterm.js: MIT). Das Abbild ist eine Zusammenstellung; GPL-Teile bleiben GPL, ihre Quellen werden jedem Release beigelegt bzw. verlinkt (CI). Eigener Code, der GPL-Code ableitet (z. B. Kernelmodule), muss GPL sein.
- `THIRD_PARTY_LICENSES` im Repo und im Abbild (unter „Einstellungen › Über“ anzeigen). Umgesetzt: „Einstellungen › Über TitanOS“ zeigt Version, Kernel, den Lizenzhinweis (Fair Source, FSL-1.1-ALv2, Apache-2.0 nach zwei Jahren), den Lizenztext und `THIRD_PARTY_LICENSES`; beide Dateien liegen im Abbild unter `/usr/share/titanos/`. Die Repo-Datei `THIRD_PARTY_LICENSES` ist eine Übersicht. Für jedes Abbild erzeugt `scripts/third-party-licenses.py` ein Versionsinventar aus Debian-Paketdaten, den Go-Modulen des gebauten Binarys und dem npm-Paketbaum einschließlich Fonts und Build-Werkzeugen. Originalhinweise liegen unter `/usr/share/titanos/licenses/`; das Release enthält ein Lizenzarchiv und genaue Quellpaketlinks.
- Beiträge nur mit Contributor License Agreement (z. B. per CLA-Bot), damit kommerzielle Lizenzen später möglich bleiben. `CONTRIBUTING.md` erklärt das.
- Name: „basiert auf Debian“ ist okay, „Debian“ nicht im Produktnamen.

## Offene Punkte

- **CLA:** `CLA.md` Version 1.0 gilt seit 13.11.2026 (Lizenzeinräumung ohne Übertragung des Urheberrechts, Recht zur Unterlizenzierung inkl. kommerziell und Apache-2.0, Patentlizenz; erfasst auch Beiträge vor der Unterschrift). Entscheidung des Inhabers vom 13.11.2026, ohne anwaltliche Prüfung in Betrieb genommen – vor größeren fremden Beiträgen oder einem kommerziellen Lizenzgeschäft prüfen lassen. Bot: gehosteter CLA Assistant (cla-assistant.io) mit dem CLA-Text als öffentlichem GitHub-Gist; Prüfung „license/cla“ an jedem PR; der Inhaber hat unterschrieben. Eine Ausnahmeliste für Bots ließ sich auf cla-assistant.io nicht einstellen: Dependabot-PRs zeigen „license/cla“ dauerhaft ausstehend, deshalb ist die Prüfung keine Pflicht für `main`. Eine neue CLA-Version heißt: Gist aktualisieren, Version und Datum in `CLA.md`. Die „Lite“-GitHub-Action (contributor-assistant/github-action) ist seit März 2026 archiviert – nicht verwenden.
- Initramfs: Debian `live-boot` ist im Einsatz und in QEMU getestet (`toram=filesystem.squashfs`, `live-media-path=` pro A/B-Eintrag, `overlay-size=512M`). Ein initramfs-Skript wartet bis zu 90 Sekunden auf den Stick (alle 10 Sekunden eine Meldung auf der Konsole), damit live-boot ihn beim ersten Versuch findet und nicht alle anderen Platten durchsucht. Nur wenn der Stick gar nicht auftaucht, sucht live-boot weiter und bindet dabei fremde Dateisysteme lesend ein.

## Funktionsumfang und offene Vorhaben

- Umgesetzt: Ersteinrichtung, Konten/Rollen und TOTP, SMB/NFS, Dateimanager, T-Mail, T-Kalender und Kontakte (`docs/TMAIL.md`, `docs/KALENDER.md`), mdadm und Btrfs-Spiegel, Docker/Apps, VMs, LXC-Systemcontainer, Plugins, Netzwerk mit Rückfall, Firewall (ab Werk aus), Backups, USV, Benachrichtigungen, Diagnose, Aktivitätsprotokoll, SSH/Zertifikate und Themes. Die jeweiligen Abschnitte unten nennen Grenzen und Prüfstand.
- Ersteinrichtung: frei gewähltes Besitzerkonto, danach `/welcome` mit Sprache, Hostname, Zeitzone, automatischer Heimnetz-Bridge und nächsten Schritten (Einrichtungsliste mit Erledigt-Zustand: Volume, ggf. Treiber, Freigabe, 2FA, Benachrichtigungen, Stick-Sicherung, Datensicherung, Fernzugriff per Tailscale/WireGuard, Mitteilungen aufs Handy, Apps; jederzeit über Einstellungen › Über TitanOS bzw. `/welcome?step=3`). Das erste Volume wird auf der Speicher-Seite angelegt.
- Deutsch und Englisch; alle Oberflächentexte über Übersetzungsdateien.
- Seit 0.8.0 App-Updates; seit 0.14.0 persönliche Benachrichtigungen, Home Assistant, Energie-Zeitplan mit Wake-on-LAN sowie Prüfen und Einzeldatei-Wiederherstellung von Backups. Externe KI-Clients arbeiten über den MCP-Server, der eingebaute KI-Agent (Ollama, PLAN 44, Entscheidung des Projektinhabers vom 4.11.2026) über denselben festen Werkzeugkatalog mit eigenen Rechten; beide ab Werk aus (`docs/rules/API.md`, `docs/MCP.md`, `docs/AGENT.md`). Einzelheiten in den eigenen Abschnitten.
- Zurückgestellt bzw. offen: ZFS und weitere Fernzugriffsarten. Let's Encrypt und dynamisches DNS sind umgesetzt (`docs/LETSENCRYPT.md`, `docs/DDNS.md`). **Tailscale, NetBird sowie kurze und optionale längere Monitoring-Verläufe sind umgesetzt**, siehe `docs/TAILSCALE.md`, `docs/NETBIRD.md` und `docs/MONITORING.md`.
- LUKS2-Volume-Verschlüsselung ist seit der Entscheidung des Projektinhabers vom 28.10.2026 als Option umgesetzt (je Volume beim Anlegen, ab Werk aus; siehe Grundsätze und `docs/ENCRYPTION.md`). Die Befehlspalette (Strg/⌘+K) ist seit 0.16.0 umgesetzt: nur Navigation, rollenabhängig, keine Mutationen.
- README: den aktuellen Gesamtumfang nach Funktionsbereichen beschreiben, keine „Neu in Version …“-Abschnitte anhängen. Aktuelle echte WebUI-Screenshots mit isolierten Beispieldaten unter `docs/screenshots/current/` pflegen; Herkunft und Aufnahme in `docs/screenshots/README.md` dokumentieren. Versionshistorie und Prüfberichte bleiben in den Releases.
- Bereits umgesetzte Funktionen nicht erneut als unentworfene Release-Voraussetzung behandeln. Für UI-Arbeiten gelten die aktuellen Entwürfe und Folgeentscheidungen unten.

## Stick-Layout

```
TITANOS (FAT32)
├── EFI/BOOT/BOOTX64.EFI          # systemd-boot (64-Bit-UEFI)
├── EFI/BOOT/BOOTIA32.EFI         # GRUB für 32-Bit-UEFI (ab 0.41)
├── boot/grub/                    # grub.cfg, grubenv, i386-pc/ (Legacy-BIOS), stick-bios.py (ab 0.41)
├── loader/
│   ├── loader.conf
│   └── entries/
│       ├── titanos-a.conf
│       └── titanos-b.conf         # erst nach dem ersten Update; auf Probe: titanos-b+3.conf
├── titanos/
│   ├── a/  vmlinuz, initrd.img, filesystem.squashfs, version, app/, bootenv (Kommandozeile für GRUB)
│   └── b/  (zweiter System-Slot; optional ebenfalls app/)
└── config/                        # persistente Konfiguration, Konten und Geheimnisse
```

Grundsätze (Kurzfassung; vollständiger Wortlaut mit SSD-Cache, Verschlüsselung, Firmware/Treiber-Plugins und RAM-Ziel in `docs/rules/STORAGE.md`):
- Rootfs bei jedem Boot frisch. Persistente Systemkonfiguration nur in `/config` auf dem Stick (atomar per temp-Datei + rename, selten); Nutzdaten, Plugin-Dateien, Betriebsdaten und schwere Daten (Docker, VM-Images, große Logs) auf Daten-Volumes, nie im Overlay. Configs für Samba, NFS, Netzwerk usw. werden beim Start aus `/config` erzeugt.
- Stick per systemd-Automount auf `/efi` ohne Leerlauf-Zeitlimit (`/config` → `/efi/config`), kein Einbinden beim Boot (`auto`/`nofail`). titand liest `/config` beim Start und hält es im RAM: **kein Zugriff auf `/config` in zyklischen Abfragen, keine offenen Dateien dort, keine periodischen Stick-Schreibzugriffe.**
- Volumes werden über UUIDs in `/config/storage/volumes.json` zugeordnet, nie über Gerätenamen wie `sda`. Arrays setzt udev nach dem Start zusammen, nicht das initramfs.
- **Im Entwicklungsmodus (`titand -dev`) verändert titand keine Platten.**
- Split-Lock-Erkennung bleibt aktiv; keine Bootparameter/Sysctls automatisch setzen oder Boot-Einträge anpassen.
- Treiber, die Debians Kernel nicht baut, nur als signierte Treiber-Plugins, nicht im Grundsystem; ein Kernelwechsel bleibt Entscheidung des Projektinhabers. Seit 0.41 wählt man unter Updates zwischen Kernel „Stabil“ (Debian) und „Neu“ (trixie-backports), Entscheidung vom 31.10.2026 (`docs/KERNEL.md`).
- journald im RAM mit Größenbegrenzung; seit 0.41.0 schreibt titand das Journal zusätzlich auf ein Daten-Volume (`.titanos/logs/<boot>.log`, begrenzt), nie auf den Stick (Entscheidung 31.10.2026, `docs/rules/SYSTEM.md`).

## Repo-Struktur (wichtige Einstiegspunkte)

```
titanos/
├── CLAUDE.md               # diese Projektanleitung
├── README.md
├── LICENSE.md              # FSL-1.1-ALv2
├── THIRD_PARTY_LICENSES
├── CONTRIBUTING.md         # inkl. CLA-Hinweis
├── CLA.md                  # Contributor License Agreement (Version 1.0)
├── SECURITY.md             # Sicherheitslücken privat melden
├── .gitignore
├── build/                  # mkosi-Konfiguration (Pakete, mkosi.extra/, Postinst)
├── boot/                   # loader.conf, Eintragsvorlagen
├── backend/                # Go
│   ├── cmd/titand/
│   └── internal/
│       ├── api/            # HTTP + WebSocket
│       ├── auth/
│       ├── config/         # Lesen/Schreiben von /config
│       ├── storage/        # Plattenerkennung, mdadm, Dateisysteme, Mounts, SMART
│       ├── shares/         # Samba, NFS, Nutzer
│       ├── docker/         # Container, Compose, Vorlagen
│       ├── vm/             # libvirt über virsh, QEMU-Abbilder über qemu-img
│       ├── lxc/            # LXC-Systemcontainer über lxc-create/systemctl, cgroup-Messwerte
│       ├── update/, app/   # System-/App-Updates und Rückfall
│       ├── toolcatalog/    # Werkzeugtypen, Argumentprüfung, Handbuch für MCP (Werkzeuge in api/catalog*.go, api/mcp_tools.go)
│       ├── office/         # Word-, Excel-, PDF-Erzeugung und Dokumentlesen
│       ├── files/, backup/, configbackup/, backuptarget/
│       ├── plugins/, hardware/, tailscale/, netbird/, usb/, ups/
│       ├── homeassistant/  # REST, WebSocket-Beobachter, MQTT-Sensoren
│       ├── nvr/            # NVR: Frigate-Container, Kameras, Erkennungsgerät (docs/NVR.md)
│       ├── speech/         # Sprachdienste: Whisper/Piper-Container, Modelle, Wyoming (docs/SPEECH.md)
│       └── system/, network/ # Systemeinstellungen, Netzwerk, Dienste
├── web/                    # Svelte + TypeScript
├── scripts/
│   ├── make-zip.sh         # squashfs + Kernel + initrd + Bootloader → Zip
│   └── run-qemu.sh         # Test-Boot mit OVMF
├── .github/workflows/      # build.yml, release.yml
└── docs/
    ├── rules/              # verbindliche Fachregeln je Bereich (aus CLAUDE.md ausgelagert)
    ├── design/             # UI-Entwürfe (*.dc.html) + README, optional screenshots/
    └── brand/              # Logo und Schriftzug als SVG + README
```

## Build, Releases und Updates

- `make build` baut Svelte/TypeScript und bettet die statische Oberfläche in das statische Go-Binary ein. Aktuelle Toolchains: Go 1.27 und Node 24 (siehe `go.mod`, `package.json`, CI).
- Reproduzierbare Fedora-44-Entwicklungsumgebung: `docs/DEVELOPMENT.md`, geprüfte Toolchains in `scripts/dev-toolchains.lock.json`. `scripts/fedora-dev.sh` passt für Tests ausschließlich im privaten User-/Mount-/UTS-Namespace den Hostnamen, OVMF-Pfade und SSH-Client-Konfiguration an. `scripts/test-storage-apps-qemu.py` ergänzt den vorhandenen Wegwerf-QEMU-Storage-Test um echte Nextcloud-/Mosquitto-Katalog-Apps; keine Host-Docker- oder Host-Plattenzugriffe. Gemessener Umfang und Grenzen vom 25.10.2026: `docs/validation/development-environment-2026-10-25.json`; keine pauschale Hardware-/Release-Abnahme daraus ableiten.
- Ein volles Release baut Debian mit mkosi, packt squashfs (zstd) und erzeugt mit Kernel, initrd und systemd-boot ein FAT32-Stick-ZIP. App-Releases packen das geprüfte Basis-Abbild mit neuem titand neu; Kernel, initrd, Bootloader und Plugins bleiben die der Basis. Jedes Release enthält weiterhin ein vollständiges ZIP und die drei Slot-Dateien für neue Sticks und ältere NAS.
- `scripts/release-kind.py` entscheidet anhand der Änderungen seit der Basis über App-/Systemrelease; neue Systempakete, Kernel, Dienste oder Build-Dateien erfordern ein volles Release. `[system]` in der Tag-Nachricht erzwingt es. **Die Versionsnummer entscheidet nicht über die Art:** 0.10.0 und 0.11.0 sind App-Releases auf Basis 0.9.0.
- `autopilot.yml` prüft jede Nacht Debian-Updates (auch Kernel und Point-Releases). Mit `AUTOPILOT=on` baut, testet und veröffentlicht er ein Patch-Release auf dem letzten Release selbst, ohne Freigabe und ohne Staffelung (Entscheidung 3.11.2026, `docs/AUTOPILOT.md`); sonst öffnet er einen PR für ein volles Release. App-Updates aktualisieren keine Debian-Pakete. Alle anderen Releases veröffentlichen nach Freigabe des Projektinhabers.
- Release-Ablauf, Lizenzinventare, Quellen und Abnahme: `docs/RELEASING.md`, `scripts/make-release.sh`, `.github/workflows/release.yml`.

### Update-Regeln (Kurzfassung; Wortlaut in `docs/rules/UPDATES.md`, Details `docs/APP-UPDATES.md`)

- Quelle nur GitHub Releases von `ra5on/TitanOS`, **nur Stable**, Manifest direkt ohne GitHub-API. **Ohne gültige Signatur keine Installation, auch nicht mit Warnung oder manuell**; danach Größe und SHA-256 jeder Datei.
- Neue Konfigurationsfelder müssen von Basis und voriger App-Version weiterhin lesbar sein; keine inkompatiblen Umbenennungen/Pflichtfelder. Rückwege einschließlich Plugin-Einstellungen erhalten.
- Volle Systemupdates: A/B-Slots mit drei Probestarts und Boot-Check über `/api/v1/health` (**kein `systemd-bless-boot`**); Bootloader (`EFI/BOOT/BOOTX64.EFI`, `EFI/BOOT/BOOTIA32.EFI`, `boot/grub/`) werden nicht ersetzt.
- **Versionen ab 0.72 zweiteilig angezeigt** (`0.72`, nie mehr 0.72.1; jede Korrektur ist das nächste Release). **Ab 1.00 immer x.xx** (1.00, 1.01 … 1.99, dann 2.00; Tag `v1.1.0` → „1.01“). Tag/Manifest bleiben dreiteilig (`v1.0.0`) für ältere NAS. Siehe docs/RELEASING.md „Ab 0.72“.
- App-Updates: nur titand mit Weboberfläche bei passender Basis-Version, Probe und Rückfall ohne NAS-Neustart; Installation wartet auf laufende dauerhafte Aufträge. Plugin-Katalog, NVIDIA-Bauumgebung und Treiber richten sich nach der **Basis-Version**.

### Release-Prüfung und Grenzen

- Test-Builds verwenden Wegwerf-Schlüssel, die nie veröffentlicht werden. Tag-Builds verlangen zwei verschiedene echte öffentliche Schlüssel; nur der geschützte Publish-Job erhält das Release-Secret. ZIP, Manifest und Prüfsummenliste werden signiert.
- QEMU-Smoke-Tests prüfen das tatsächliche ZIP, System-Rückfall sowie App-Updates mit erhaltener Anmeldung, Fehlstart-Rückfall und Rückweg zur Basis. Bei App-Releases wird die bereits geprüfte Basis neu gepackt; nicht alle Storage-/Hardwaretests werden erneut ausgeführt.
- Historische GitHub-Update-/Rollback-Prüfungen und Hardwaremeldungen sind dokumentiert, aber keine pauschale Abnahme jedes neuen Releases. Neue Prüfungen und ihre Grenzen ausdrücklich festhalten.

## Testen

- QEMU mit OVMF (UEFI). Stick-Inhalt als FAT-Laufwerk: entweder `-drive file=fat:rw:<ordner>` (vvfat, Schreibzugriff experimentell) oder ein per mtools erzeugtes FAT-Image.
- Zusätzliche virtuelle Platten für RAID-Tests (Ausfall simulieren, Rebuild prüfen).
- Tests auf echter Hardware gezielt ergänzen. Bestätigte frühere Tests gelten nur für den dokumentierten Umfang, nicht automatisch für neue Funktionen.
- `make test`: Go vet/Tests, Webtests und Svelte-Prüfung. `make test-release`: Paketierungs-/Signaturtests mit echten Werkzeugen. UI-Prüfskripte in `scripts/test-ui-*.mjs`; ausschließlich isolierte Testdaten verwenden. Keine Host-Platten formatieren, keine Host-Sysctls für Tests ändern.

## Harte Grundsätze (Kurzfassung bereichsübergreifender Regeln)

Wortlaut und Einzelheiten in der jeweiligen Regeldatei; bei Widerspruch gilt die ausführlichere Regel dort.

- **Daten und Platten:** Datenverlust vermeiden hat Vorrang. Vor jedem Anlegen/Erweitern/Ersetzen fremde Arrays, LVM/crypt/dm-Halter, Bootstick, konfigurierte Volumes und VM-Platten ausschließen und nach dem Stoppen erneut prüfen. Tests nie auf Host-Platten, nur auf neu erzeugten Images bzw. Wegwerf-QEMU-Datenträgern; keine Host-Sysctls ändern (`docs/rules/STORAGE.md`).
- **Private Betriebsdaten** unter `.titanos/<funktion>` gehören root, sind nicht von anderen beschreibbar oder verlinkt; ein verschwundenes Volume ist nie ein beschreibbarer RAM-/Overlay-Ersatz (`docs/rules/STORAGE.md`).
- **Bestätigungen:** Zerstörende Aktionen mit einfachem Bestätigungsdialog (`Dialog.svelte`, `.dialog-actions`), gefährlicher Knopf links von „Abbrechen“, Enter/Autofokus lösen ihn nie aus. **Nirgends einen Namen oder eine Kennung eintippen lassen**; erwartet die API einen Bestätigungswert, schickt ihn die Oberfläche selbst (`docs/rules/WEB-UI.md`).
- **Konsolen:** Alle Konsolen (NAS-Shell, Docker, VM-Bildschirm, serielle VM-Konsole, LXC) nutzen die bestehende Anmeldung ohne zweite Passwort-/2FA-Abfrage; keine erneute Bestätigung einführen. Trennen ohne Eingabe nach wählbarer Zeit (5–120 Minuten oder „Nie“, ab Werk 15), Protokollierung und Abschaltbarkeit bleiben (`docs/rules/ACCOUNTS-ACCESS.md`).
- **Rechte:** Rollen Besitzer/Administrator/Benutzer, Prüfung immer serverseitig gegen aktuelle Konten-/Share-Rechte. root ist gesperrt, SSH ab Werk aus. API-Tokens nur über die explizite Routenliste `publicRoutes`, MCP zusätzlich über den festen Werkzeugkatalog (`toolcatalog`, `api/catalog*.go`) mit dem Konto des Tokens; nie Geheimnisse, Passwörter/2FA/Passkeys, Tokenverwaltung, Shells/Konsolen oder Netzwerkbestätigung (`docs/rules/ACCOUNTS-ACCESS.md`, `docs/rules/API.md`).
- **Geheimnisse:** Passwörter (z. B. Volume-Passwörter) nie auf der Kommandozeile, nie protokolliert, nie im MCP-/Token-Katalog; Cluster-Geheimnisse nicht im Diagnosepaket.
- **Aufträge:** Das Backend besitzt Aufträge und führt Änderungen aus; Browserergebnisse nie als Ausführungsnachweis. RAM-Aufträge und offene Verbindungen nicht als dauerhaft behandeln (`docs/rules/STORAGE.md`, `docs/rules/UPDATES.md`).
- **Ab Werk aus:** Firewall, SSH, Verschlüsselung, SSD-Cache, MCP-Server, Wartung, längere Monitoring-Verläufe und andere optionale Funktionen laut Regeldatei.
- **Plugins:** Vor der Installation auf volle Systemrechte hinweisen; keine Plugin-Sandbox behaupten (`docs/rules/PLUGINS.md`).
- **Oberfläche:** Kein abgeschnittener Text (kein „…“, kein verstecktes Überlaufen, keine Zeilenbegrenzung; umbrechen, scrollen oder Symbol, `docs/rules/WEB-UI.md`). Status nie nur über Farbe; Kontrast mindestens 4,5:1; Farben aus Theme-Variablen; alle Texte über Übersetzungsdateien (Deutsch/Englisch). Reale Messwerte, Beispieldaten nur in isolierten Tests.
- **Prüfstände:** Getestetes immer mit Umfang und Grenzen dokumentieren; frühere Hardwaremeldungen sind keine pauschale Abnahme neuer Funktionen.

## Arbeitsweise

- Das Projekt ist umgesetzt und veröffentlicht; keine früheren Phasenpläne als offene Implementierungsaufträge verwenden.
- Vor UI-Arbeiten aktuellen Entwurf und Folgeentscheidungen in `docs/design/README.md` lesen. Reale Messwerte und Kontorechte beibehalten; Beispieldaten nur in isolierten Tests.
- Historische Prüfberichte beschreiben ihren damaligen Umfang. Neue Funktionen mit passenden Tests absichern und verbleibende Hardware-/Integrationsgrenzen dokumentieren.

## Regeln

- Kein Unraid-Name, -Logo, -Code und keine 1:1-Unraid-Optik (auch nicht deren Orange-Verlauf). Bedienabläufe dürfen angelehnt sein. „Inspiriert von Unraid“ in der README ist okay.
- GPL-Komponenten (Kernel, Debian-Pakete) werden verteilt: jedem Release die passenden Quellen beilegen oder verlinken (im CI automatisieren).
- Destruktive Aktionen (RAID anlegen, Platte löschen) immer mit ausdrücklicher Bestätigung in der UI.
- Datenverlust vermeiden hat Vorrang vor Funktionen. RAID0 immer mit deutlichem Hinweis auf fehlende Redundanz; RAID ersetzt kein Backup.
- Benutzersperren, WebSocket-Origin-Prüfung, Terminal-Abschaltung und Sitzungsbegrenzung auch bei Konsolen ohne zweite Passwortabfrage beibehalten.

## Fachregeln

Verbindliche Detailregeln je Bereich. Vor Arbeiten an einem Bereich die Datei lesen; die genannten Fachdokumente in `docs/` beschreiben Bedienung, Grenzen und Prüfungen.

| Regeldatei | Lesen vor Arbeiten an … |
|---|---|
| `docs/rules/STORAGE.md` | Stick/`/config`, Volumes, mdadm/Btrfs, SSD-Cache, Verschlüsselung, Firmware/Treiber, Ruhemodus, RAM-/Upload-Budget, Übernahme/Erweiterung, Wartung, Auftragsabläufe, Anwendungssicherungen, Restore-Proben, VM-Klon, Speicherverbrauch |
| `docs/rules/UPDATES.md` | Update-Quelle, Signaturen, Update-Einstellungen, A/B-Systemupdates, App-Updates, Boot-/App-Check |
| `docs/rules/WEB-UI.md` | Desktop-Modus (Arbeitsplatz, Fenster, Dock, Handy-Raster), Bestätigungsdialoge, Marke, Systemsteuerung, Darstellung/Themes/Farben/Glas, gemeinsame UI, Ersteinrichtung |
| `docs/rules/ACCOUNTS-ACCESS.md` | Konten/Rollen, 2FA, Passkeys, Sitzungen/„Angemeldet bleiben“, Konten sperren, SSH/Zertifikate (Zugang), Terminal und Konsolen, Aktivitätsprotokoll |
| `docs/rules/SYSTEM.md` | Energie, Zeitplan/Wake-on-LAN, USV, Werkzeuge-Reiter, Benachrichtigungen, Monitoring/CPU-Leistung, Ein-/Ausschalter, Tailscale, NetBird |
| `docs/rules/NETWORK.md` | Netzwerkeinstellungen mit Rückfall, Firewall |
| `docs/rules/BACKUPS.md` | Datensicherung, Sicherung des Sticks, USB-Kopie, SMB-Kopie, Meldungstöne |
| `docs/rules/SHARES-FILES.md` | Shares, Freigabe samt Daten löschen, Kontingente, Dateimanager, Datei-Editor, USB-Uploads, Namensregeln, Vorschau/Suche, Link-Downloads |
| `docs/rules/CLUSTER.md` | Cluster (Beitritt, Proxy, Kontenabgleich, Backups) |
| `docs/rules/API.md` | Öffentliche API/Tokens, titanosctl, MCP-Server und Werkzeugkatalog, VM-Medien-Import, Plugin-Aufräumen |
| `docs/rules/DOCKER.md` | Docker-Übersicht, Container bearbeiten, App-Store/Katalog, Docker-Layout, NVR (Frigate-Container, `docs/NVR.md`), Sprachdienste (Whisper/Piper, `docs/SPEECH.md`) |
| `docs/rules/VMS-LXC.md` | VMs (Erstellen, Firmware, Konsole, Snapshots), App-Vorlagen, Hardware-Durchreichung, LXC-Systemcontainer |
| `docs/rules/PLUGINS.md` | Plugins, Community-Plugins, NVIDIA-Treiberbau |
| `docs/rules/HOME-ASSISTANT.md` | Home-Assistant-Anbindung |

Weitere maßgebliche Dokumente: `docs/design/README.md` (UI-Entwürfe), `docs/RELEASING.md` (Release-Ablauf), `docs/WORKFLOWS.md` (Speicher-/Wartungsabläufe), sowie die Fachdokumente `docs/<BEREICH>.md`, auf die die Regeldateien verweisen.
