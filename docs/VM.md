# TitanOS 2.0.4 in einer virtuellen Maschine

TitanOS wird als Festplatten-Image bereitgestellt. Importiere das Image in
deine VM und starte anschließend von dieser Festplatte.

## Vorbereiten

Du benötigst einen Hypervisor für **x86-64 / amd64** mit UEFI-Unterstützung.
Plane mindestens zwei virtuelle CPU-Kerne und empfohlen **8 GB RAM** ein.
Deaktiviere Secure Boot und verbinde die VM mit einem Netzwerk, in dem sie eine
IP-Adresse erhalten kann.

Wenn du innerhalb von TitanOS weitere virtuelle Maschinen betreiben möchtest,
muss dein Hypervisor die Hardwarevirtualisierung an die TitanOS-VM weitergeben
(Nested Virtualization). Die genaue Einstellung hängt von deinem Hypervisor ab.

## Image importieren

1. Lade [titan-2.0.4.img.xz](https://github.com/ra5on/TitanOS/releases/download/v2.0.4/titan-2.0.4.img.xz) herunter. Prüfsummen und Signatur findest du im [Release](https://github.com/ra5on/TitanOS/releases/tag/v2.0.4).
2. Prüfe die Download-Datei anhand der [Signatur- und Prüfsummenanleitung](DOWNLOADS.md).
3. Entpacke die Datei zu `titan-2.0.4.img`. Mit dem Werkzeug `xz` geht das beispielsweise so:

   ```sh
   xz --decompress --keep titan-2.0.4.img.xz
   ```

4. Erstelle eine VM mit UEFI und importiere `titan-2.0.4.img` als ihre Boot-Festplatte. Stelle diese Festplatte an die erste Stelle der Startreihenfolge.
5. **Vergrößere genau diese importierte Boot-Festplatte vor dem ersten Start auf mindestens 32 GiB, empfohlen 64 GiB.** Verwende dafür die Größenanpassung deines Hypervisors.
6. Starte die VM.

Das Image ist bewusst kompakt und entspricht noch nicht der empfohlenen
Bootdisk-Größe. Eine neu hinzugefügte, zweite Festplatte vergrößert die
Boot-Festplatte nicht und ersetzt Schritt 5 nicht.

## Erster Start und Zugriff

Beim ersten Start richtet TitanOS zwei **10-GiB-Systembereiche** ein. Der
verbleibende Platz der Boot-Festplatte wird als Datenbereich genutzt.

Ermittle die IP-Adresse der VM in deinem Router beziehungsweise in der
Netzwerkverwaltung deines Hypervisors und öffne `http://VM-IP/` im Browser.
Wenn die lokale Namensauflösung in deinem Netzwerk verfügbar ist, kannst du
auch `http://titan.local/` verwenden. Schließe dort die Einrichtung ab und
installiere anschließend deine Apps im App-Store.

## Wenn die Einrichtung nicht startet

| Beobachtung | Prüfe zuerst |
| --- | --- |
| Die VM findet kein startfähiges System | UEFI-Modus, deaktiviertes Secure Boot und Startreihenfolge der importierten Festplatte |
| Der erste Start scheitert bei der Festplatteneinrichtung | Die tatsächliche Größe der importierten Boot-Festplatte: mindestens 32 GiB, empfohlen 64 GiB |
| `titan.local` ist nicht erreichbar | Zugriff über die IP-Adresse; VM-Netzwerk und lokale Namensauflösung |
| Weitere VMs innerhalb von TitanOS starten nicht | Weitergabe der Hardwarevirtualisierung durch den Hypervisor |

## Bestehende TitanOS-VM aktualisieren

Systemupdates werden ausschließlich als **Stable** veröffentlicht und direkt
in den Einstellungen installiert. Die [Update-Anleitung](UPDATES.md) erklärt
die Prüfung und Installation dieser Updates. Bestehende Installationen dieses
TitanOS-Forks ab 2.0.1 können auf 2.0.4 aktualisiert werden. Für den Wechsel von
Titan 3.x oder einem früheren Namensraum ist eine Neuinstallation nötig.

## Was beim Release geprüft wird

Das zuvor veröffentlichte TitanOS 2.0.1 hat den Start in einer UEFI-Test-VM mit
einer 32-GiB-Boot-Festplatte erfolgreich bestanden. Weboberfläche, Backend und
Versionskennung wurden über das VM-Netzwerk geprüft. Der signierte
[Boot-Bericht](https://github.com/ra5on/TitanOS/releases/download/v2.0.1/image-verification.json)
enthält die Ergebnisse dieser Ausgabe. Diese Prüfung deckt nicht jede
Hypervisor-, Hardware-, App- oder Nested-Virtualization-Konfiguration ab.
Die Berichte und die Dateien `titan-2.0.1.img.xz` und
`titan-2.0.1.update` findest du beim
[Release](https://github.com/ra5on/TitanOS/releases/tag/v2.0.1).

Für 2.0.4 erzeugt die Veröffentlichung einen eigenen Boot-Bericht. Image und
Update-Bundle stehen nach erfolgreichem Build, Prüfung und Veröffentlichung im
[Release 2.0.4](https://github.com/ra5on/TitanOS/releases/tag/v2.0.4) bereit.

[Zur Übersicht](../README.md) · [Support](https://github.com/ra5on/TitanOS/issues)
· [Lizenz](../LICENSE.md) · [Herkunft](../UPSTREAM.md)
