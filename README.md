<p align="center">
  <img src="docs/images/titanos-banner.svg" alt="TitanOS" width="920">
</p>

<h1 align="center">TitanOS 2.0.1 · Stable</h1>

<p align="center">
  Ein Zuhause für deine Dateien, Apps und virtuellen Maschinen.<br>
  Auf deinem Server, direkt im Browser.
</p>

<p align="center">
  <a href="https://github.com/ra5on/TitanOS/releases"><strong>Images &amp; Downloads</strong></a>
  · <a href="https://github.com/ra5on/TitanOS/releases">Releases &amp; Prüfsummen</a>
  · <a href="https://github.com/ra5on/TitanOS/issues">Support</a>
</p>

## Dein Server im Überblick

TitanOS verbindet eine deutsche Oberfläche mit einem übersichtlichen Desktop.
Im Dock erreichst du Dateien, Foto's, den App-Store, virtuelle Maschinen und
Einstellungen. Oben links findest du das Systemmenü: Abmelden, Neustarten und
Herunterfahren werden jeweils mit **Ja / Nein** bestätigt.

| Bereich | Was du damit machen kannst |
| --- | --- |
| Dateien & Foto's | Dateien verwalten und Fotos ansehen |
| App-Store & Docker | Apps installieren und Container verwalten |
| Virtuelle Maschinen | Weitere Betriebssysteme auf deinem Server betreiben |
| Speicher & Benutzer | Laufwerke einrichten und Zugänge verwalten |
| Einstellungen | Netzwerk, Backups und Systemupdates konfigurieren |

## Ein Blick auf TitanOS

Die bisherigen Aufnahmen zeigen TitanOS 2.0.0-titan.1 aus einer separaten
Testinstallation. Neue Aufnahmen der TitanOS-Repository-Ausgabe folgen nach
dem ersten signierten Image-Build.

![TitanOS-Desktop der bisherigen Testinstallation](docs/images/titanos-desktop.jpg)

## Installieren

Für einen x86-64-Rechner empfehlen wir **8 GB RAM**, eine **SSD ab 64 GB** und
**UEFI mit deaktiviertem Secure Boot**.

1. Nach Veröffentlichung findest du `titan-2.0.1.img.xz`, Prüfsummen und Signatur unter [Release-Bereich](https://github.com/ra5on/TitanOS/releases). Der erste Build setzt den bestehenden Signing-Key im neuen Repository voraus.
2. Entpacke das Image und schreibe es mit einem geeigneten Image-Werkzeug auf die Ziel-SSD. **Dabei wird der Inhalt dieses Laufwerks überschrieben.**
3. Starte den Rechner von dieser SSD und verbinde ihn mit deinem Netzwerk.
4. Öffne `http://titan.local/` im Browser. Falls dein Netzwerk lokale Namen nicht auflöst, verwende die IP-Adresse aus deinem Router.

Das Download-Image ist kompakt. Beim ersten Start richtet TitanOS zwei
10-GiB-Systembereiche ein und nutzt die **volle verbleibende SSD-Kapazität für
deine Daten**. Apps installierst du anschließend im App-Store.

**Für eine VM:** Vergrößere die importierte **Boot-Festplatte vor dem ersten
Start auf mindestens 32 GiB, empfohlen 64 GiB**. Eine zusätzliche Datendisk
ersetzt diesen Schritt nicht. Die [VM-Anleitung](docs/VM.md) führt durch die
Einrichtung.

## Updates

TitanOS bietet ausschließlich **Stable-Systemupdates** an. Einzelne Funktionen
können ausdrücklich als Alpha oder Beta gekennzeichnet sein; stabile Funktionen
haben kein zusätzliches Badge.

Das bisherige Repository bleibt für den Übergang erreichbar. Die signierte
Version `2.0.1` muss dort ebenfalls veröffentlicht sein, bevor bestehende NAS
den Wechsel anbieten.

Du nutzt bereits `2.0.0-titan.1` oder `2.0.0-titan.2`?

1. Wähle im bisherigen Update-Kanalmenü einmal **Stable**.
2. Installiere das angebotene Übergangsupdate **`2.0.0-titan.3`** und starte neu.
3. Suche erneut nach Updates und installiere **`2.0.1`**.

Eine installierte `2.0.0-titan.3` oder `2.0.0` kann direkt auf `2.0.1` aktualisieren.
Dieses Update stellt den NAS-Updater auf `ra5on/TitanOS` um.

Die ältere `.3`-Übergangsversion stellt nur ein Updatebundle bereit. Ihre interne `.3`-
Kennung ermöglicht den Wechsel vom bisherigen Updater; die Oberfläche zeigt
bereits TitanOS 2.0.0. Eigene Hostnamen bleiben erhalten. Der alte Standardname
`umbrel` wird zu `titan`, sodass du danach `http://titan.local/` oder die
IP-Adresse verwendest.

Installationen auf der früheren Titan-Basis mit anderem Festplattenaufbau
benötigen eine Neuinstallation. Weitere Details, das Updatebundle
`titan-2.0.1.update` und die Prüfberichte findest du beim
[Release-Bereich](https://github.com/ra5on/TitanOS/releases).

## Gut zu wissen

Der Release-Build prüft den Start des tatsächlichen Images über UEFI, die
Weboberfläche und die installierte Versionskennung. Das ist keine umfassende
Prüfung aller Apps, Hardwarekombinationen oder VM-Szenarien.

Der App-Store verwendet externe App-Quellen; Apps werden nach der Einrichtung
installiert. Google Drive, Dropbox und OneDrive benötigen eigene
OAuth-Konfigurationen. Fragen und nachvollziehbare Fehlerberichte kannst du im
[Titan-Support auf GitHub](https://github.com/ra5on/TitanOS/issues) teilen.

---

TitanOS ist ein unabhängiges Projekt auf Basis von
[Umbrel 2.0.0](https://github.com/getumbrel/umbrel/tree/2.0.0).
Es gelten die [Lizenz](LICENSE.md) und die Hinweise zu
[Herkunft und Drittkomponenten](UPSTREAM.md).
