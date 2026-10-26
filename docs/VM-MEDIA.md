# VM-Medien importieren und Windows-Treiber

Unter **VMs › Medien importieren** nimmt TitanOS ISO-Dateien und eigenständige
VM-Abbilder per Browser-Upload oder HTTP(S)-URL an. Unterstützte Namen enden auf
`.iso`, `.qcow2`, `.raw`, `.img`, `.vmdk`, `.vhdx` oder `.vdi`. Der Import legt
Medien ab; er installiert kein Gastbetriebssystem und registriert keine VM
ungefragt.

## Ablage und Prüfung

Ein eingebundenes Daten-Volume ist erforderlich. Quelle, entpackte Ausgabe und
Checkpoints liegen privat unter `/mnt/<volume>/.titanos/media/<id>/`, niemals
auf dem Bootstick oder als großes RAM-Zwischenabbild. Übertragung und Hashing
verwenden begrenzte Puffer; höchstens vier Hintergrundverarbeitungen zugleich.
Ein Upload-Teil ist höchstens 8 MiB groß. Zusätzlich zur Quelle muss vorübergehend
Platz für die entpackte Ausgabe und mindestens 64 MiB Reserve frei bleiben.
Andere Schreiber können freien Speicher verändern; während des Kopierens wird
weiter geprüft.

Unkomprimiert, gzip, xz und ZIP werden unterstützt. ZIP muss genau **ein**
unterstütztes Abbild mit passender Dateiendung enthalten; unsichere Pfade,
Symlinks und Spezialdateien werden abgewiesen. Ein ZIP mit mehreren Abbildern
wird nicht automatisch nach einer vermeintlich passenden Datei durchsucht.

Eine optionale SHA-256-Angabe prüft die **heruntergeladene/hochgeladene Quelle**, also
bei komprimierten Dateien das Archiv. Die Oberfläche zeigt anschließend den
Hash der entpackten Ausgabe. ISO9660/UDF-Kennung bzw. `qemu-img info` prüfen das
Format; Backing-Dateien und externe Datendateien werden abgelehnt. Das ist kein
Nachweis, dass ein Betriebssystem startet oder vertrauenswürdig ist.

Die fertige Datei wird unter `/mnt/<volume>/iso/<name>` ohne Überschreiben
veröffentlicht. Private Verzeichnisse, Mount-ID und geöffnete Deskriptoren
schützen vor einem Ausweichen in den RAM-Overlay bei Volume-Verlust. Auch das
Entfernen eines Importauftrags löscht die veröffentlichte Datei nicht.

## Unterbrechen und wiederaufnehmen

URL-Downloads laufen bei geschlossener Seite weiter. Pause, Abbrechen und
Wiederholen sind ausdrücklich; Abbrechen entfernt Stagingdaten, vorhandene
Zieldateien bleiben erhalten. Browser-Uploads benötigen den ursprünglichen
Browser/File-Zugriff. Nach Seitenwechsel oder Neuladen dieselbe Originaldatei
wählen. Bereits empfangene Bytes werden in begrenzten Hash-Blöcken vollständig
gegen diese Datei geprüft, bevor weitere Daten angehängt werden. Eine Pause
auf dem Server wird nicht durch den nächsten alten Upload-Teil aufgehoben.

Nach Dienst- oder NAS-Neustart startet kein Download von selbst erneut.
Downloads werden nur mit passendem starken ETag/Last-Modified per HTTP-Range
fortgesetzt. Liefert der Server eine komplette neue Antwort, wird die Quelle
von Anfang an neu geschrieben; widersprüchliche Teilantworten werden abgewiesen.
HTTPS-Downloads dürfen nicht auf HTTP umleiten. URL-Anmeldedaten werden nicht
akzeptiert, URL-Abfrageparameter erscheinen nicht in der Auftragsliste.

Eine unterbrochene Veröffentlichung steht auf **Prüfung erforderlich**. Mit
„Veröffentlichtes Abbild prüfen“ lässt sich ein schon vorhandenes Ziel nur nach
Größen- und vollständigem Hashabgleich als abgeschlossen übernehmen. Eine
fehlende/abweichende Ausgabe wird nicht blind erneut veröffentlicht. Zieldatei
prüfen, Auftrag gegebenenfalls entfernen und einen neuen Namen verwenden.

## Windows VirtIO

Beim Anlegen einer Windows-VM ist der automatische Treiberdownload voreingestellt.
Die Installations-ISO bleibt das erste CD-Laufwerk; die Treiber-ISO kommt als
zweites hinzu. Bestehende VMs bekommen keine ungefragte neue Medienauswahl und
keine geänderten Controller. Manuelle Treiberpfade bleiben möglich.

Quelle ist die offizielle stabile ISO des
[VirtIO-Windows-Projekts](https://github.com/virtio-win/virtio-win-pkg-scripts):
`https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso`.
Weiterleitungen bleiben auf HTTPS bei fedorapeople.org im offiziellen Downloadpfad. Fedora liefert bei diesem Alias teilweise HTTP-Location-Kopfzeilen; TitanOS ersetzt dort das Schema vor dem nächsten Request durch HTTPS. Es wird dabei keine HTTP-Verbindung aufgebaut. Eine bereits importierte
ISO dieser Quelle wird erst nach erneutem Hashabgleich wiederverwendet. Der
Download liegt auf dem gewählten Daten-Volume und erscheint in den Aufträgen.
VM-Anlage wartet auf das fertige Medium; das spätere Starten bleibt eine
bewusste Aktion.

TLS prüft die Herkunft; der lokal gespeicherte Hash prüft unveränderte Bytes.
Es wird keine unabhängige Signatur der gesamten ISO behauptet. Treiberauswahl,
Windows-Version und Secure-Boot-Vertrauen müssen beim konkreten Gast geprüft
werden. Eine VM mit eingelegter Installations- oder Treiber-CD unterstützt
weiterhin keine TitanOS-RAM-Snapshots; dafür beide Medien entfernen.
