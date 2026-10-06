# Docker-Vorlagen und Geräte

Titan 3.0.0 verwendet seine eigene Docker-Verwaltung. Unter **Hauptmenü →
Docker → Vorlagen** stehen Konfigurationen von LinuxServer.io und Big Bear
Dockge zur Verfügung. Der mitgelieferte Katalog enthält 374 normalisierte
Vorlagen. Eine Aktualisierung der Quellen kann diese Anzahl verändern.

Ein eigener AppStore mit von Titan betreuten Komplettpaketen ist aufgeschoben.
Die Vorlagen ersetzen keine pauschale Laufzeitprüfung jeder Anwendung. Ihre
Konfiguration wurde normalisiert und mit Docker Compose geprüft; das bestätigt
noch nicht Anmeldung, Funktionen oder Hardwarebeschleunigung der jeweiligen App.
Die Freigabegrenzen stehen in [QA-3.0.0](QA-3.0.0.md).

## Eine Vorlage verwenden

1. **Docker → Vorlagen** öffnen. Suche, Quellenauswahl und A–Z/Z–A helfen beim Finden.
2. **Einrichten** wählen und die Hinweise zur Anmeldung und die verlinkte
   Anleitung des Herausgebers lesen. Titan erfindet keine Standardpasswörter.
3. Den benannten Speicherbereich und gegebenenfalls eine Freigabe auswählen.
   Titan legt die vorgesehenen Daten- und Konfigurationsverzeichnisse dort an;
   technische Hostpfade müssen dafür nicht eingegeben werden.
4. Vorgaben für Webport, weitere TCP-/UDP-Ports, Netzwerk und
   Umgebungsvariablen prüfen. Optionale Ports und Datenordner lassen sich gezielt
   einschalten. Benötigte eigene Werte, beispielsweise eine App-Adresse oder
   Zugangsdaten zu einem externen Dienst, vor dem Installieren ergänzen.
5. Bei Bedarf tatsächlich erkannte USB-/GPU-/NPU-Geräte auswählen und installieren.
   Ohne Auswahl erhält der Container keinen Gerätezugriff.

Enthält eine unterstützte Vorlage mehrere Dienste, legt Titan diesen Verbund
gemeinsam an. Datenbanken oder andere Abhängigkeiten, die in der Vorlage fehlen,
werden nicht automatisch ergänzt. Besonders LinuxServer.io-Anwendungen können
eine separat einzurichtende Datenbank oder andere externe Dienste verlangen.
Die jeweilige Anleitung bleibt maßgeblich.

Die Quellen lassen sich unter **Vorlagenquellen verwalten** aktualisieren und
ein- oder ausblenden. Nicht unterstützte Konfigurationen werden ausgelassen;
die Quelle zeigt die Anzahl. Titan führt keine beliebigen Compose-Dateien aus
einem hinzugefügten Internet-Link aus. Container-Images werden bei Bedarf von
ihren Herausgebern heruntergeladen und behalten ihre eigenen Lizenzen.

## Installierte Anwendungen verwalten

Unter **Docker → Projekte** sind zusammengehörende Dienste gruppiert. Die
Containeransicht zeigt Status, CPU/RAM, Ports und Adressen. Ein Klick öffnet
Details mit **App öffnen**, Einstellungen, Start/Stop/Neustart und Protokollen.
App-Links und Desktop-Verknüpfungen öffnen Anwendungen im neuen Browser-Tab.

Bei einem unterstützten Vorlagenpaket gelten gemeinsame App-Aktionen für seinen
gesamten Verbund; Aktionen auf einen einzelnen Container betreffen nur diesen
Dienst. Einstellungen eines Pakets lassen sich erst ändern, wenn alle seine
Dienste gestoppt sind. Bei fehlgeschlagener Neuerstellung versucht Titan, die
vorherige Konfiguration wiederherzustellen, und meldet Fehler ausdrücklich.

Entfernen oder Deinstallieren braucht eine Ja/Nein-Bestätigung. Aufbewahrte
Datenverzeichnisse und Docker-Volumes sind kein unabhängiges Backup. Ein
Betriebssystem-Rollback setzt weder Nutzdaten noch App-Datenbanken zurück.
Die vorhandene Sicherung bietet keine vollständige App-Wiederherstellung auf
einem frischen Image; siehe [QA-3.0.0](QA-3.0.0.md).

## Netzwerk und Arbeitsspeicher

Bridge mit veröffentlichten Ports ist die einfache Vorgabe. Host-Netzwerk und
vorhandene eigene Netze sind auswählbar; bei Host oder ohne Netzwerk werden
keine separaten Portzuordnungen eingetragen. **Docker → Netzwerke → Erstellen**
legt ein eigenes Bridge-Netz an. Ein Name genügt; ein freies Subnetz wird nach
Prüfung vorhandener Netze und Host-Routen gewählt. Subnetz, Gateway und interne
Kommunikation sind erweiterte Optionen. Macvlan-, Overlay- und IPv6-Netze werden
über diese Oberfläche nicht neu angelegt.

Ein verwendetes Netzwerk kann nicht gelöscht werden. Auch gestoppte Container
und installierte App-Pakete können die Zuordnung behalten. System- und fremde
Netze bleiben vor der Titan-Löschaktion geschützt.

Neue Installationen und Starts prüfen frische RAM-Messwerte sowie die
Containergrenzen, VM-Zuweisungen und NAS-Reserve. Das Budget berücksichtigt auch
Autostarts beim nächsten Boot. Swap vergrößert die verfügbare RAM-Kapazität nicht.
Ein Limit ist eine Obergrenze, kein gemessener Verbrauch. Nach einer Verkleinerung
des physischen RAM kann der Offline-Schutz den Start von Docker und libvirt
anhalten; die NAS-Verwaltung und der Dateimanager bleiben dabei unabhängig.
Zur Wiederherstellung dem NAS wieder ausreichend RAM zuweisen. Details unter
[RAM-Schutz](MEMORY-BUDGET.md).

## USB, Grafik und NPU

Die Auswahl zeigt erkannte Geräte mit Hersteller, Modell und Seriennummer,
soweit verfügbar. Geeignete rohe USB-Geräte verlangen eine eindeutige
Identität. Speicher-, Hub-, Netzwerk- und unbekannte USB-Geräte werden darüber
nicht freigegeben. Vor Start und Autostart wird die Identität erneut geprüft;
eine wiederverwendete USB-Adresse genügt nicht. Ein fehlendes oder ersetztes
Gerät kann den automatischen Docker-Start sicher sperren.

- Intel-/AMD-Grafik wird angeboten, wenn ein Rendergerät mit aktivem Treiber
  vorhanden ist. AMD-Compute kann zusätzlich `/dev/kfd` benötigen.
- NVIDIA-GPUs benötigen einen eingerichteten Treiber und eine einsatzbereite
  NVIDIA Container Runtime.
- NPUs werden über vorhandene `/dev/accel/accel*`-Geräte erkannt. Mehrere Geräte,
  beispielsweise Intel-Grafik und NPU, können gemeinsam ausgewählt werden.

Die App braucht passende Beschleunigungssoftware. Die Auswahl installiert keine
GPU-/NPU-Treiber und garantiert keine Unterstützung durch jedes Image. Bei einer
Titan-VM muss die Hardware zuerst durch den Host verfügbar gemacht werden.
Reale Durchreichung bleibt Teil der Hardware-Praxistests.

Änderungen erfordern einen gestoppten Container beziehungsweise ein gestopptes
Vorlagenpaket. Unterstützte manuelle Titan-Container behalten bei einem Umbau
eine gestoppte lokale Vorgängerkopie. Das gemeinsame Datenvolume ist dabei
weiterhin kein unabhängiges Backup. Fremde oder komplexere Konfigurationen
werden nicht automatisch umgebaut.

Quellen: [LinuxServer.io-Dokumentation](https://docs.linuxserver.io/),
[Big Bear Dockge](https://github.com/bigbeartechworld/big-bear-dockge).
