# Herkunft und Drittkomponenten

TitanOS ist ein unabhängiges, nichtkommerzielles Projekt auf Basis des
vollständigen veröffentlichten [Umbrel-2.0.0-Quellcodes](https://github.com/getumbrel/umbrel/tree/2.0.0),
Commit `9298257b0e904ca8d8270b1702672666343f0b86`. TitanOS ist keine offizielle
Umbrel-Veröffentlichung.

Die ursprüngliche [LICENSE.md](LICENSE.md) bleibt unverändert erhalten.
Die PolyForm Noncommercial License 1.0.0 gilt für den übernommenen Code.
Drittkomponenten und installierbare Anwendungen behalten ihre jeweiligen
Lizenzen und Rechtehinweise. Dieses Dokument ersetzt diese Hinweise nicht.

Die ursprüngliche Projektbeschreibung liegt in `README.upstream.md`. Die
ursprünglichen GitHub-Workflows werden als Text in `.titan/upstream-workflows/`
aufbewahrt und nicht ausgeführt. TitanOS verwendet eine eigene AMD64-Pipeline
für Image-Build, Prüfung, Signatur und Veröffentlichung. Lizenz und
Herkunftshinweise werden ebenfalls im Image unter `/usr/share/doc/titan/`
mitgeliefert.

## Anpassungen von TitanOS

TitanOS übernimmt die NAS-Funktionen und ergänzt eine deutsche Oberfläche,
eigene Desktop-Icons, das Titan-Systemmenü und einen eigenen signierten
GitHub-Updatekanal. Es werden ausschließlich Stable-Systemupdates angeboten.
Einzelne ausdrücklich experimentelle Funktionen können Alpha-/Beta-Hinweise
tragen. Der Updatekanal prüft eine Ed25519-signierte Prüfsummenliste,
Release- und Buildmetadaten, Architektur, Festplattenaufbau und das
heruntergeladene Rugix-Systembundle. Er führt keine entfernten Updateskripte aus.

Eigene Dienste, Dateipfade, APIs und die Benutzeroberfläche verwenden jetzt
Titan-Namen. Das neue Image hat einen eigenen Installationsnamensraum und
benötigt eine Neuinstallation. Die externe App-Manifestkompatibilität bleibt
gezielt erhalten; die Grenzen sind in `packages/titand/NAMESPACE-CONTRACTS.md`
beschrieben. Herstellerkennungen tatsächlicher Hardware werden nicht geändert.

## Externe Dienste und Apps

Die ursprünglichen Google-, Dropbox- und OneDrive-OAuth-Registrierungen sowie
der gehostete OAuth-Redirect wurden entfernt. Diese optionalen Anbieter
benötigen eigene Laufzeit-Zugangsdaten und einen Redirect-Dienst, bevor sie
angeboten werden. WebDAV und iCloud bleiben verfügbar. Fremde gehostete
Integrationen werden durch die Quellcodeübernahme nicht übertragen.

Der App-Store verwendet den externen ursprünglichen App-Katalog. TitanOS
kopiert oder lizenziert diesen separaten Katalog und dessen Container-Images
nicht neu. Apps werden nach der Einrichtung durch den Nutzer installiert;
sie sind nicht sämtlich im Betriebssystem-Image vorinstalliert.
