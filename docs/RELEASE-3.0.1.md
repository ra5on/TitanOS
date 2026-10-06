# Titan-Anwendung 3.0.1

Die Cloudflared-Web-Vorlage verwendet im Host-Netz standardmäßig den tatsächlichen
Webport 14333. Ein bewusst gewählter anderer Webport steuert bei neuen
Installationen und beim Speichern der Einstellungen auch den Listener und die
Bridge-Portzuordnung. App-Links berücksichtigen den tatsächlich konfigurierten
Listener des Containers.

`BASIC_AUTH_PASS` wird als privates Passwort behandelt. Neue Installationen
verlangen ein eigenes Passwort; die Anmeldehinweise erklären Benutzername und
Passwort. Vorhandene Passwörter und Tunnel-Konfigurationen werden beim Update
nicht verändert. Auch ältere Bridge-Zuordnungen und abweichende Host-Listener
bleiben verwaltbar. Ihre gesamte Definition wird weiterhin exakt geprüft.

Host-Vorlagen behalten beim Import und beim Aktualisieren des Katalogs ihren
nativen Port. Verwendete Vorlagen und laufende Container werden dadurch nicht
automatisch umgestellt.

Für eine bereits installierte Cloudflared-Web-App: unter **App verwalten**
stoppen, in **Einstellungen** ein eigenes `BASIC_AUTH_PASS` setzen und im
Host-Netz `WEBUI_PORT` auf 14333 belassen. Anschließend starten und
`http://NAS-IP:14333` öffnen. Benutzername ist die gespeicherte Einstellung
`BASIC_AUTH_USER`, standardmäßig `admin`. Den Tunnel-Token ausschließlich in
der App eingeben.

Automatische Regressionstests prüfen Listener, Portzuordnungen, Öffnungslinks,
Passwortschutz und die Verwaltung älterer Installationen. Der Fehler auf dem
konkreten NAS ist ohne dessen geöffnete Adresse und Laufzeitstatus noch nicht
abschließend bestätigt.

Lokale Validierung: 1.847 Python-Tests bestanden (eine umgebungsbedingte
Auslassung), 49 UI-Testsuiten und JavaScript-Syntaxprüfung bestanden. Die
Cloudflared-Kompatibilität ist mit simulierten Docker-Antworten geprüft; ein
realer Start dieses Containers auf dem betroffenen NAS steht noch aus.
