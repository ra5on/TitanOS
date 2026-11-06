# Desktop-Modus (PLAN 61)

Seit 0.58.0 zeigt TitanOS am großen Bildschirm einen **Arbeitsplatz**: Programme liegen als Symbole auf einem Hintergrundbild, jedes öffnet sich in einem eigenen Fenster. Er ersetzt Kopfzeile, Seitenleiste und Statusleiste (Entscheidung des Projektinhabers vom 6.11.2026). Am Handy und auf einem hochkant gehaltenen Tablet (unter 900 px Breite) gibt es stattdessen ein App-Raster; jedes Programm öffnet im Vollbild mit „Zurück“.

## Programme

Jede Seite von TitanOS gehört zu genau einem Programm (`web/src/lib/desktop/apps.ts`, ein Test prüft, dass keine Seite fehlt):

| Programm | Seiten |
|---|---|
| Dateien | Dateimanager, Einstellungen › Dateien |
| Freigaben | Freigaben, Dienste |
| Speicher | Speicher (Verwalten, Platten prüfen, Importieren, Platte ersetzen), Plattenwerkzeuge, Arbeitsspeicher |
| Docker | Container, Netzwerke, HTTPS-Namen für Apps |
| Virtuelle Maschinen | Maschinen, neue Maschine, ISO und Medien; jeder VM-Bildschirm als eigenes Fenster |
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
| Übersicht | das bisherige Dashboard |

Docker-Apps mit Weboberfläche (z. B. Immich) sind eigene Programme; ihr Fenster zeigt die App über ihren HTTPS-Namen (`docs/APP-LINKS.md`). Normale Benutzer sehen nur die Programme ihrer Rolle und die Docker-Apps, die ihnen freigegeben sind (Benutzer › Programme).

## Fenster

- Eine Seite im Fenster läuft in einem Rahmen gleicher Herkunft (`X-Frame-Options: SAMEORIGIN`, `frame-ancestors 'self'`); fremde Seiten dürfen TitanOS weiter nicht einbetten. Im Rahmen zeigt die Seite keine Kopfzeile und keine eigene zweite Seitenleiste (`web/src/lib/desktop/frame.ts`, `inWindow`).
- Die Seitenleiste des Fensters listet die Seiten und Einstellungen des Programms; einklappbar (nur Symbole), je Programm gemerkt.
- Größe an allen vier Ecken und Seiten, Doppelklick auf die Titelleiste = maximieren, an den Rand ziehen = halbe Seite, oben = Vollbild, Ecke = Viertel (mit Vorschau). Knöpfe rechts: in eigenem Tab öffnen, minimieren, maximieren, schließen.
- Unter 1280 px Breite öffnen Fenster maximiert.
- Die Adresse des Browsers zeigt die Seite des Fensters vorn; ein Lesezeichen oder Link öffnet das passende Programm. Der Verlauf wächst dabei nicht (Zurück bleibt auf dem Arbeitsplatz).
- Tastatur: Alt + Q Fensterwechsler (Alt + Tab gehört dem Betriebssystem), Alt + Pfeile andocken, Alt + W schließen, Alt + M minimieren, Strg + Leertaste Hauptmenü. Die Tasten wirken auch, wenn eine Seite im Fenster den Fokus hat.

## Arbeitsplatz

- **Hauptmenü:** alle Programme, Suche über Programme und ihre Seiten, Filter (Alle, TitanOS, Docker-Apps, VMs, Zuletzt), unten Konto, Systemsteuerung, Sperren, Ein/Aus. Ein Programm aus dem Menü auf den Arbeitsplatz ziehen legt es dort ab.
- **Symbole:** ordnen nach Art, Name, zuletzt benutzt oder frei; drei Größen; Ordner; Abzeichen (Zahl = Updates, ! = Warnung, Punkt = läuft/gestoppt, Text beim Überfahren). Rechtsklick auf ein Symbol bzw. den Arbeitsplatz für alle Aktionen.
- **Widgets** frei auf einem Raster von 20 px, an der Ecke vergrößern, Galerie mit Zustand, CPU/RAM, Netzwerk, Volumes, Docker-Apps, VMs, Sicherungen, Temperaturen, Uhr, Home Assistant, Aufgaben. Sie lesen nur, was die Oberfläche ohnehin abfragt, und wecken keine Platten.
- **Hintergrund:** sechs eigene Bilder (SVG, hell und dunkel nach Theme) oder ein eigenes Bild (wie der bisherige Hintergrund je Konto auf dem Stick, höchstens 10 MB). „Bewegter Hintergrund“ verschiebt es langsam und färbt es nach Tageszeit; bei „Bewegung reduzieren“ steht es still.
- **Mitteilungs-Center**, **Agent-Blase** (der Agent im kleinen Fenster; die Frage beginnt mit dem Fenster vorn) und **Sperrbildschirm** (Sperren im Menü, automatisch nach einstellbarer Zeit).
- Dateien vom PC auf den Arbeitsplatz ziehen öffnet „Dateien“ und lädt sie in den gerade offenen Ordner hoch (mit der üblichen Rückfrage).

## Gespeichert

- Je Konto in der Einstellung `desktop` auf dem Stick (`/config/users/<name>/desktop.json`): Symbole, Ordner, Widgets, Hintergrund, Sortierung, Sperrzeit, eingeklappte Seitenleisten, gespeicherte Fenster-Anordnungen. Geschrieben wird nur nach einer Änderung, 1,5 s nach der letzten.
- Offene Fenster mit Lage und Größe nur im Browser (`localStorage`): sie ändern sich ständig und sollen den Stick nicht abnutzen.

## Grenzen

- Seiten im Fenster sind eigene Rahmen; jedes offene Fenster lädt die Oberfläche einmal (mehr Arbeitsspeicher im Browser).
- Docker-Apps, die sich trotz Proxy nicht einbetten lassen (eigene Anmeldung über fremde Anbieter, Prüfung per JavaScript), öffnet „In eigenem Tab“. Eine reine `http://IP:Port`-Adresse kann eine HTTPS-Seite nicht im Fenster zeigen; das Fenster erklärt es.
- Ohne vertrautes Zertifikat lädt ein App-Fenster nicht (Browser zeigen in Rahmen keine Warnung). Abhilfe: Let's Encrypt, Tailscale-Name oder die TitanOS-Zertifizierungsstelle auf dem Gerät (`docs/APP-LINKS.md`).
