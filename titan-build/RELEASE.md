# TitanOS 2.0.8 · Stable

## Neu in 2.0.8: QCOW2-Import hochgeladener Abbilder

Über die Oberfläche hochgeladene QCOW2-Abbilder (zum Beispiel Home Assistant
OS) lassen sich jetzt importieren. Bisher brach der Import mit „Das
QCOW2-Abbild konnte nicht gelesen oder umgewandelt werden“ ab: Die private
Arbeitskopie übernahm den Besitzer der hochgeladenen Datei und war für die
abgeschottete Umwandlung nicht lesbar. Die Quelldatei und ihre Rechte bleiben
unverändert. Die Prüfung vor jeder Veröffentlichung importiert nun ein Abbild,
das wie ein echter Upload dem Dateibesitzer gehört.

## Aus 2.0.7

- **Deutsche Ordnernamen:** Jeder persönliche Ordner enthält jetzt
  **Dokumente**, **Downloads**, **Fotos** und **Videos** – auch bei neuen
  Benutzern. Bestehende Ordner `Documents` und `Photos` werden beim ersten Start
  einmalig umbenannt; ihr Inhalt bleibt erhalten. Favoriten, Freigaben,
  App-Ordnerzugriffe und die Quellen der Fotos-App zeigen danach auf die neuen
  Namen. Enthält ein Ordner `Dokumente` oder `Fotos` bereits Dateien, bleibt der
  alte Ordner unverändert daneben bestehen.
- **Transparenz:** Alle Konten starten nach dem Update einmalig mit der
  Standardtransparenz von 75 %. Über den Regler unter **Darstellung** lässt sie
  sich weiterhin individuell anpassen.
- **Startbildschirm:** Über der Begrüßung steht nur noch der Titan-Schriftzug,
  ohne das Symbol davor.
- **Systemupdates:** Ein Update startet erst, wenn der laufende Systemstart
  bestätigt ist. Zuvor konnte ein sehr früh gestartetes Update nach dem Neustart
  unbestätigt bleiben.
- **Build:** Die Veröffentlichung nutzt GitHub-Actions mit Node.js 24.

## Zuverlässiger QCOW2-Import

Eigene QCOW2-Festplattenabbilder lassen sich jetzt auch dann importieren, wenn
der Import bisher mit „Die Aktion konnte nicht ausgeführt werden“ abbrach. Die
abgeschottete Umwandlung erreicht ihre private Arbeitskopie nun unabhängig von
den Rechten geschützter persönlicher Ordner; diese Rechte bleiben unverändert.
Kann ein Abbild nicht gelesen oder umgewandelt werden, nennt TitanOS den Grund
verständlich. Der Fortschritt der Umwandlung wird laufend angezeigt.

## Aus 2.0.5

Eigene QCOW2-Festplattenabbilder lassen sich hochladen oder aus Dateien auswählen. Der Import erstellt eine unabhängige VM-Festplatte in einer abgeschotteten Umgebung. Titan nutzt den neuen Carbon-Schriftzug und das passende T-Symbol.

Dock, Widgets und Systemmenü verwenden einen einheitlichen Glaseffekt. Im
Systemmenü oben links kann jeder Benutzer unter **Darstellung** die Transparenz
anpassen. Die Vorschau reagiert sofort; gespeichert wird die Einstellung im
jeweiligen Benutzerkonto. Texte und Symbole bleiben deckend.

Bestätigungsdialoge ordnen **Ja links** und **Nein rechts** an, auch auf kleinen
Bildschirmen.

## Systemwiederherstellung

Die Software-Update-Einstellungen ergänzen die Rückkehr zum vorherigen
verfügbaren Systemstand. Die Auswahl wird vor der Ausführung erneut geprüft;
ein Neustart aktiviert den ausgewählten Stand. Nach einer Neuinstallation wird
kein nicht vorhandener Wiederherstellungsstand angeboten.

Das Startmenü bietet zusätzlich die Auswahl des aktuellen und, wenn vorhanden,
des vorherigen Systemstands. Ohne Auswahl startet nach fünf Sekunden der
vorgesehene Systemstand. Das Menü ist am angeschlossenen Bildschirm oder in der
VM-Konsole erreichbar und benötigt keine laufende Weboberfläche. Bei einer
kompatiblen älteren Installation wird es nach dem ersten erfolgreich
bestätigten Start von 2.0.8 eingerichtet.

Ein Systemrollback setzt persönliche Dateien, Container-Datenbanken und
VM-Laufwerke nicht zurück. Diese gemeinsamen Daten benötigen eigene Sicherungen.

## Virtuelle Maschinen

Die Netzwerkauswahl aus 2.0.3 bleibt verfügbar:

- **NAT (Standard):** Internet über das NAS; Dienste erreichst du mit Portweiterleitungen.
- **Host-only:** Privates Netz zwischen NAS und VM, ohne Internet- oder LAN-Zugang.
- **Heimnetz (Bridge):** Verbindung über eine vorhandene Linux-Bridge. Fehlt sie, richtet TitanOS sie bei dieser Auswahl automatisch über den aktiven kabelgebundenen NAS-Anschluss ein.

Bei eigenen Images und ISOs kannst du das Netzwerk bereits beim Erstellen wählen.
Kataloginstallationen verwenden zunächst NAT für ihre Einrichtung. Danach lässt
sich das Netzwerk in den VM-Einstellungen ändern, sobald die VM ausgeschaltet ist.
Vorhandene Portregeln bleiben gespeichert und werden ausschließlich bei NAT aktiv.
Fehlende Bridges und Konflikte des privaten Subnetzes werden verständlich gemeldet.
Die Bridge-IP wird angezeigt, sobald das NAS sie im gewählten Netzwerk beobachten kann.

Die automatische Bridge-Einrichtung übernimmt die bisherige NAS-Verbindung des
aktiven Anschlusses. Die Weboberfläche kann dabei kurz die Verbindung verlieren
und verbindet sich automatisch neu. Lass das Fenster geöffnet, damit die neue
Verbindung bestätigt werden kann. Ein Netzwerk-Checkpoint schützt die bisherige
Konfiguration und nimmt die Umstellung ohne erfolgreiche Bestätigung automatisch
zurück. Bestehende Bridges und andere
Netzwerkanschlüsse bleiben erhalten; WLAN wird nicht automatisch umgebaut.

## Download und Aktualisierung

- `titan-2.0.8.img.xz`: Kompaktes AMD64-Image für eine Neuinstallation mit UEFI.
- `titan-2.0.8.update`: Vollständiges Systemupdate für kompatible TitanOS-Installationen.
- `SHA256SUMS` und `SHA256SUMS.sig`: Prüfsummen und Ed25519-Signatur aller Release-Dateien.
- `image-verification.json`: Bericht zur Image-Struktur, zum realen UEFI-Boot und zur geprüften Versionskennung.
- `bridge-smoke.json`: Zwölf echte VM-Prüfungen für DHCP, statische IP, automatische Rücknahme und Neustart mit der Bridge.
- `recovery-smoke.json` und `recovery-evidence.json`: Echte VM-Prüfungen für signiertes Update, Versionswechsel, Neustart, Startmenü, manuellen Rollback, Datenerhalt und automatische Rückkehr nach einem beschädigten Teststart. Beide Prüfgruppen müssen für die Veröffentlichung vollständig erfolgreich sein.

Der Wiederherstellungstest verwendet eine private, separat signierte ältere
Testversion aus demselben Quellcode. Er belegt den Ablauf mit den nativen
Systembereichen und öffentlichen APIs, aber keine vollständige Migration
echter 2.0.3-App-Daten. Die [Update-Anleitung](https://github.com/ra5on/TitanOS/blob/main/docs/UPDATES.md)
beschreibt auch die Vertrauensgrenze bei der Übernahme bestehender Systemslots.

**TitanOS ab 2.0.1** mit der Kennung `titan-rugix-amd64-v2` kann über die
Systemeinstellungen aktualisiert werden. **Titan 3.x** und ältere Installationen
mit anderem Systemaufbau benötigen eine Neuinstallation; sichere ihre Daten
vorher außerhalb des Systems. Es gibt keinen automatischen Formatwechsel.

Vergrößere beim VM-Test die importierte Boot-Festplatte **vor dem ersten Start
auf mindestens 32 GiB**, empfohlen sind 64 GiB. TitanOS nutzt den verbleibenden
Platz nach der Einrichtung für Daten. Secure Boot muss deaktiviert sein.

Die Veröffentlichung erfolgt erst nach erfolgreichen Quell-, Regressions-,
Image- und UEFI-Boot-Prüfungen sowie Signaturprüfung. Der Boot-Test bestätigt
keine umfassende Prüfung aller Gastbetriebssysteme, LAN-Konfigurationen oder Hardwarekombinationen.

[VM-Netzwerke](https://github.com/ra5on/TitanOS/blob/main/docs/MACHINE-NETWORKS.md) ·
[Download-Prüfung](https://github.com/ra5on/TitanOS/blob/main/docs/DOWNLOADS.md) ·
[Support](https://github.com/ra5on/TitanOS/issues)
