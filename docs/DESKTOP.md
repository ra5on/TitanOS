# Desktop-Modus (PLAN 61)

Seit 0.58.0 zeigt TitanOS am großen Bildschirm einen **Arbeitsplatz**: Programme liegen als Symbole auf einem Hintergrundbild, jedes öffnet sich in einem eigenen Fenster. Er ersetzt Kopfzeile, Seitenleiste und Statusleiste (Entscheidung des Projektinhabers vom 6.11.2026); deren Code ist seit 0.62.0 entfernt, der Arbeitsplatz ist die einzige Oberfläche. Am Handy und auf einem hochkant gehaltenen Tablet (unter 900 px Breite) gibt es stattdessen ein App-Raster; jedes Programm öffnet im Vollbild mit „Zurück“.

## Programme

Jede Seite von TitanOS gehört zu genau einem Programm (`web/src/lib/desktop/apps.ts`, ein Test prüft, dass keine Seite fehlt):

| Programm | Seiten |
|---|---|
| Dateien | Dateimanager, Einstellungen › Dateien |
| Freigaben | Freigaben, Dienste |
| Speicher | Speicher (Verwalten, Platten prüfen, Importieren, Platte ersetzen), Plattenwerkzeuge, Arbeitsspeicher |
| Docker | Container, Netzwerke, HTTPS-Namen für Apps |
| Virtuelle Maschinen | Maschinen, neue Maschine, ISO und Medien, VM übernehmen; darunter jede VM einzeln (seit 0.59.0 statt der Liste auf der Seite); jeder VM-Bildschirm als eigenes Fenster |
| Systemcontainer | LXC |
| App-Center | App-Katalog, eigene App, Plugins, Aufräumen |
| Sicherung | Datensicherung (alle Teile), Stick-Sicherung |
| KI-Agent | das Chatfenster des Agenten mit seinen eigenen Einstellungen |
| Monitor | Task-Manager, Verlauf, Aufgaben, Hardware prüfen, Hardware, Temperaturen |
| Protokolle | Systemprotokoll, Aktivität, Diagnosepaket |
| Aufgabenplaner | Skripte, Wartung, Energie-Zeitplan |
| Terminal | NAS-Shell (mehrere Fenster möglich) |
| Benutzer | Konten, Programme (Rechte je App), Zugang |
| Sicherheit | Firewall |
| Mein Konto | Passwort, 2FA, Passkeys, SSH-Schlüssel, WebDAV |
| Systemsteuerung | alle übrigen Einstellungen, Startseite „Alle Einstellungen“ |

Docker-Apps mit Weboberfläche (z. B. Immich) sind eigene Programme; ihr Fenster zeigt die App seit 0.61.0 immer in der **Fernansicht** (`docs/FERNANSICHT.md`): TitanOS öffnet sie auf dem NAS in einem eigenen Browser und überträgt Bild und Ton über die TitanOS-Adresse, also auch per Cloudflare, VPN und am Handy. „In eigenem Tab öffnen“ zeigt die App weiter direkt über ihren Namen bzw. Port. Normale Benutzer sehen nur die Programme ihrer Rolle und die Docker-Apps, die ihnen freigegeben sind (Benutzer › Programme).

## Fenster

- Eine Seite im Fenster läuft in einem Rahmen gleicher Herkunft (`X-Frame-Options: SAMEORIGIN`, `frame-ancestors 'self'`); fremde Seiten dürfen TitanOS weiter nicht einbetten. Im Rahmen zeigt die Seite keine Kopfzeile und keine eigene zweite Seitenleiste (`web/src/lib/desktop/frame.ts`, `inWindow`).
- Die Seitenleiste des Fensters listet die Seiten und Einstellungen des Programms; einklappbar (nur Symbole), je Programm gemerkt.
- Beim Verschieben folgt das Fenster dem Zeiger per Transformation (eine Aktualisierung je Bild), der bewegte Hintergrund hält an; Lage und Größe werden erst beim Loslassen geschrieben.
- Größe an allen vier Ecken und Seiten, Doppelklick auf die Titelleiste = maximieren, an den Rand ziehen = halbe Seite, oben = Vollbild, Ecke = Viertel (mit Vorschau). Knöpfe rechts: in eigenem Tab öffnen, minimieren, maximieren, schließen.
- Unter 1280 px Breite öffnen Fenster maximiert.
- **Seiten als Fenster-Apps (PLAN 64, wie UGOS/DSM):** Die Titelleiste nennt Programm und Seite („Docker › Container“: der Eintrag der Seitenleiste für genau diese Seite, sonst der Titel der Seite). Die Seite zeigt im Fenster keine eigene große Überschrift (sie bleibt für Screenreader). Ihre Aktionen stehen in **einer Werkzeugleiste** direkt unter der Titelleiste (`PageHead`, Klasse `win-toolbar`): kompakt (32 px), oben fest, Hauptaktion rechts; was nicht passt, wandert in das Menü „⋯“ (nie abgeschnittene Knöpfe). Eine Seite ohne Aktionen hat keine leere Leiste; ihr Einleitungssatz steht dann oben. Inhalte als Karten (12 px Ecken, Kopfzeile mit Titel, 16 px Abstand), Einstellungen als Zeilen „Bezeichnung links, Wert rechts“ ab 640 px Fensterbreite, kompakte Tabellen. Das gilt nur im Fenster (`main.in-window`) und in der Vollbildansicht am Handy (`main.phone-page`, dort mit dem Seitennamen links in der Leiste, wenn er vom Programmnamen abweicht). Geprüft mit `scripts/test-ui-windows.mjs` (800/1100 px und Handy).
- Die Adresse des Browsers zeigt die Seite des Fensters vorn; ein Lesezeichen oder Link öffnet das passende Programm. Der Verlauf wächst dabei nicht (Zurück bleibt auf dem Arbeitsplatz).
- Tastatur: Alt + Q Fensterwechsler (Alt + Tab gehört dem Betriebssystem), Alt + Pfeile andocken, Alt + W schließen, Alt + M minimieren, Strg + Leertaste Hauptmenü. Die Tasten wirken auch, wenn eine Seite im Fenster den Fokus hat.

## Arbeitsplatz

- **Hauptmenü:** alle Programme, Suche über Programme und ihre Seiten, Filter (Alle, TitanOS, Docker-Apps, VMs, Zuletzt), unten Konto, Systemsteuerung, Sperren, Ein/Aus. Ein Programm aus dem Menü auf den Arbeitsplatz ziehen legt es dort ab.
- **Leisten oben:** links Arbeitsplatz zeigen, Hauptmenü, im Cluster direkt dahinter der Umschalter zwischen den Nodes (Name des Nodes mit Zustandspunkt, seit 0.61.0), dann die offenen Fenster; rechts Auslastung, Aufgaben, Mitteilungen, Konto. Rechtsklick auf eine Leiste stellt ihre Größe ein (Klein, Mittel, Groß). Hauptmenü und Mitteilungs-Center schließen bei einem Klick daneben (auch in ein Fenster).
- **Dock** (seit 0.59.0, ab Werk an): angeheftete Programme und offene Fenster (nach einem Strich), Punkt = offen, Vergrößern beim Überfahren. Ziehen aus dem Menü oder vom Arbeitsplatz heftet an, Ziehen im Dock sortiert um. Rechtsklick auf das Dock: Dock bearbeiten (Größe, Deckkraft, Vergrößern Aus/Leicht/Stark, Position unten/links/rechts, × entfernt), Position, Dock ausblenden. Rechtsklick auf ein Symbol im Dock: das Programm-Menü mit „Im Dock behalten“/„Aus dem Dock entfernen“. Mehr Platz bietet Systemsteuerung › Darstellung › Dock (PLAN 64) mit denselben Einstellungen, der Liste der angehefteten Programme und einer Vorschau. Angedockte Fenster und Symbole halten den Platz des Docks frei.
- **Symbole:** frei auf einem unsichtbaren Raster (ziehen an eine freie Stelle; auf ein anderes Symbol = tauschen, auf einen Ordner = hinein), „Automatisch anordnen“ im Rechtsklick; sonst ordnen nach Art, Name oder zuletzt benutzt; drei Größen; Ordner; Abzeichen (Zahl = Updates, ! = Warnung, Punkt = läuft/gestoppt, Text beim Überfahren). Rechtsklick auf ein Symbol bzw. den Arbeitsplatz für alle Aktionen.
- **Widgets** frei auf einem Raster von 20 px; Rechtsklick auf ein Widget: Größe, Entfernen, Bearbeiten, Hinzufügen, alle ausblenden (wieder einblenden im Rechtsklick auf den Arbeitsplatz); an der Ecke vergrößern, Galerie mit Zustand, CPU/RAM, Netzwerk, Volumes, Docker-Apps, VMs, Sicherungen, Temperaturen, Uhr, Home Assistant, Aufgaben. Sie lesen nur, was die Oberfläche ohnehin abfragt, und wecken keine Platten.
- **Hintergrund:** sechs eigene Bilder (SVG, hell und dunkel nach Theme) oder ein eigenes Bild (wie der bisherige Hintergrund je Konto auf dem Stick, höchstens 10 MB). „Bewegter Hintergrund“ verschiebt es langsam und färbt es nach Tageszeit; bei „Bewegung reduzieren“ steht es still.
- **Mitteilungs-Center**, **Agent-Blase** (der Agent im kleinen Fenster; die Frage beginnt mit dem Fenster vorn) und **Sperrbildschirm** (Sperren im Menü, automatisch nach einstellbarer Zeit).
- Dateien vom PC auf den Arbeitsplatz ziehen öffnet „Dateien“ und lädt sie in den gerade offenen Ordner hoch (mit der üblichen Rückfrage).

## Gespeichert

- Je Konto in der Einstellung `desktop` auf dem Stick (`/config/users/<name>/desktop.json`): Symbole mit ihren Plätzen, Ordner, Widgets (und ob ausgeblendet), Dock, Größe der Leisten, Hintergrund, Sortierung, Sperrzeit, eingeklappte Seitenleisten, gespeicherte Fenster-Anordnungen. Geschrieben wird nur nach einer Änderung, 1,5 s nach der letzten.
- Offene Fenster mit Lage und Größe nur im Browser (`localStorage`): sie ändern sich ständig und sollen den Stick nicht abnutzen.

## Handy (PLAN 64)

- Startbildschirm: Suchleiste unter der Statusleiste und beim Blättern oben haftend, Ränder für Notch und Home-Leiste auf allen Seiten, nichts breiter als der Bildschirm, Eingabefelder mindestens 16 px (kein Zoom in Safari); beim Öffnen beginnt er oben. Im Cluster steht der Node-Umschalter neben der Suche (44-px-Ziele, Menü über die ganze Breite); verwaltet die Seite einen anderen Node, nennt ihn die Leiste über dem Programm.
- Docker-Apps erscheinen auch, wenn ihre Adresse für den Browser nicht nutzbar ist (z. B. Vorlage `http://[IP]:8080`): die Fernansicht braucht nur den Namen. Sie öffnen im Vollbild; Zurück führt zum Raster.
- Ursache der oben abgeschnittenen Suchleiste war eine fremde, allgemeine Klasse `.bar` (Fortschrittsbalken, 6 px hoch) auf der Kopfzeile des Startbildschirms. Prüfung: `scripts/test-ui-phone.mjs` (Emulation 390×844 und 430×932, kein echtes iPhone).

## Grenzen

- Seiten im Fenster sind eigene Rahmen; jedes offene Fenster lädt die Oberfläche einmal (mehr Arbeitsspeicher im Browser).
- Die Fernansicht braucht einen Browser mit WebCodecs (aktuelle Chrome/Edge, Firefox ab 130, Safari ab 16.4) und HTTPS oder `localhost`; sonst erklärt das Fenster es und bietet „In eigenem Tab öffnen“. Grenzen der Fernansicht (Apps im Host-Netz, Speicher je Ansicht) in `docs/FERNANSICHT.md`.
