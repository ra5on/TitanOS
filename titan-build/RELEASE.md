**TitanOS 2.0.1 · Stable**

Deutsche Oberfläche, einheitliche Titan-Namen und neue Dock-Symbole. Oben links
auf dem Desktop öffnet der Systembutton das Menü für Abmelden, Neustarten und
Herunterfahren. Systemaktionen werden mit Ja/Nein bestätigt. Foto's trägt den
neuen Namen; die Werbekarte für die Smartphone-App wurde entfernt. Support
führt zum Titan-Projekt auf GitHub.

**Kompaktes Image:** `titan-2.0.1.img.xz` enthält das Betriebssystem ohne eine
künstliche Vergrößerung auf 32 GiB. Beim ersten Start auf einer SSD richtet Titan
zwei 10-GiB-Systembereiche ein und nutzt den übrigen Platz für die Daten.
Empfohlen sind eine SSD ab 64 GB, 8 GB RAM und UEFI mit deaktiviertem Secure Boot.

**In einer VM:** Die importierte Boot-Festplatte vor dem ersten Start auf
mindestens **32 GiB**, empfohlen **64 GiB**, vergrößern. Eine zweite Datendisk
ersetzt diesen Schritt nicht. Der Zugriff erfolgt über `http://VM-IP/` oder
bei verfügbarer lokaler Namensauflösung über `http://titan.local/`.

**Updates:** TitanOS veröffentlicht ausschließlich Stable-Systemupdates. Alpha
und Beta kennzeichnen nur einzelne Funktionen, wenn sie ausdrücklich so
gekennzeichnet wurden; stabile Funktionen haben kein zusätzliches Badge.
Signaturen, Prüfsummen, Architektur und Festplattenaufbau werden vor dem Update
geprüft. Updates und Rollback verwenden den vorhandenen Rugix-Aufbau.

**Von `2.0.0-titan.1` oder `.2` aktualisieren:** Im bisherigen Kanalmenü einmal
**Stable** auswählen. Zuerst das signierte Übergangsupdate
`2.0.0-titan.3` installieren und neu starten. Anschließend erneut nach Updates
suchen und `2.0.1` installieren. Die Übergangsversion liefert ausschließlich das
Updatebundle, kein zusätzliches IMG; die interne Buildkennung bleibt zur
Kompatibilität mit dem bisherigen Updater `.3`. Die Oberfläche der älteren `.3` zeigt bereits
TitanOS 2.0.0. Eigene Hostnamen bleiben erhalten, der alte Standard `umbrel`
wird zu `titan` geändert. Verwende gegebenenfalls die IP-Adresse zum Zugriff.

Die frühere Titan-Basis mit einem anderen Festplattenaufbau benötigt eine
Neuinstallation. Beim Schreiben eines IMG wird das Ziellaufwerk überschrieben.
Apps und deren Abhängigkeiten werden anschließend über den App-Store installiert.
Google Drive, Dropbox und OneDrive benötigen eigene OAuth-Konfigurationen.

Der Build prüft das tatsächliche Image mit einem UEFI-Start, die Weboberfläche
und die installierte Versionskennung. Die Berichte liegen beim Release.
Prüfsummen und Ed25519-Signatur findest du in `SHA256SUMS` und `SHA256SUMS.sig`.
Der vertrauenswürdige öffentliche Schlüssel liegt im Repository unter
`.titan/release-public.pem`.

```sh
openssl pkeyutl -verify -rawin -pubin -inkey release-public.pem -in SHA256SUMS -sigfile SHA256SUMS.sig
sha256sum --check --ignore-missing SHA256SUMS
```

Lizenz und Herkunft: [LICENSE.md](https://github.com/ra5on/TitanOS/blob/main/LICENSE.md)
und [UPSTREAM.md](https://github.com/ra5on/TitanOS/blob/main/UPSTREAM.md).

**Repository-Wechsel:** TitanOS 2.0.1 enthält den neuen Updatekanal
`ra5on/TitanOS`. Für bestehende NAS werden dieselben signierten Dateien
zusätzlich im bisherigen Repository angeboten. Eine installierte `.3` oder
`2.0.0` kann direkt auf `2.0.1` aktualisieren. Ältere Releases bleiben verfügbar.
