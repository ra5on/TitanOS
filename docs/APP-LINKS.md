# App-Icons und Webadressen

Der offizielle Katalog bleibt das separate Repository `ra5on/TitanOS-Apps`.
Die Quelle heißt „TitanOS-Apps“. Doppelte manuelle Einträge derselben Quelle
werden beim Laden einmalig entfernt; installierte Apps bleiben erhalten.
TitanOS liest `x-titanos.icon` (HTTPS) bzw. das CasaOS-Icon. Die Oberfläche lädt
Bilder als `<img>` ohne Referrer und fällt bei Fehlern auf Namenskürzel zurück.
Bei Neuinstallation wird das Icon als `titanos.icon` am Hauptdienst gespeichert.
Bestehende Container ohne dieses Label behalten ihre Namenskürzel.

`titanos.webui` enthält einen Host-Port, einen Port mit Pfad oder eine vollständige
HTTP-/HTTPS-Adresse mit `[host]`. Bei der Installation folgt die Adresse dem
gewählten Host-Port. Der lokale Knopf verwendet die NAS-Adresse. Der Tailscale-
Knopf erscheint nur bei aktivem Plugin im Zustand `Running` und verwendet zuerst
den MagicDNS-Namen, sonst eine Tailscale-IP (IPv6 mit Klammern). Ein fest
konfigurierter externer Host wird nicht als Tailnet-Adresse umgeschrieben.
Der Client muss selbst Zugriff auf das Tailnet haben. Tailscale ändert das
HTTP-/HTTPS-Protokoll nicht; Zertifikatwarnungen selbstsignierter Apps bleiben.

Die optionale öffentliche URL wird bei Installation oder im Container-Editor
je Dienst als `titanos.public-url` gespeichert. Nur HTTP/HTTPS ohne eingebettete
Zugangsdaten werden als Link angeboten. Leer entfernt den Knopf. Diese Einstellung
konfiguriert keinen Cloudflare-Tunnel, Reverse-Proxy oder Firewallzugang.
Änderungen im Container-Editor werden wie bisher über Compose angewendet.

## Darstellung

Gemeinsame Variablen regeln Seitentitel, Text, Kontrollhöhe und Abstände. Die
Inhaltsfläche nutzt auch breite Bildschirme. Normal/Kompakt und die persönliche
Schriftskalierung bleiben erhalten. Auf Touchgeräten sind gemeinsame Knöpfe
mindestens 44 px hoch; Eingabefelder vermeiden automatischen iOS-Eingabezoom.
Dialoge berücksichtigen die dynamische Fensterhöhe. Breite Tabellen und
Docker-Filter scrollen in ihrem eigenen Bereich; Docker-Stacks und Formulare
stapeln sich auf Smartphones. Das gespeicherte Dashboard-Raster wird nicht
migriert oder durch ein Smartphone-Layout ersetzt.

Regressionen: `web/tests/app-links.test.mjs`, Katalog-Icon-Test und bestehende
Docker-/API-Tests. Die manuelle Browserabnahme nutzt einen Entwicklungsserver
mit ausschließlich temporärer Konfiguration und Testdaten für Containerlinks.

Auf Smartphones bis 760 px öffnet der Menüknopf neben dem Logo alle für das
Konto erlaubten Bereiche. Der aktuelle Bereich bleibt am Knopf sichtbar.
Links haben mindestens 44 px Höhe; das Menü schließt bei Auswahl, Escape,
Außenklick und Wechsel zur Desktopbreite. Die Desktop-Reiterleiste bleibt
seitlich scrollbar. Diese Anpassung gehört zum Entwicklungsstand nach Beta 7.
