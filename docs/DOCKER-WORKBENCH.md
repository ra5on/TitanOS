# Docker direkt verwalten

Docker im Hauptmenü öffnet Titans eigene Verwaltung: Projekte, Container, Images, Netzwerke, Volumes und Vorlagen. Es wird keine fremde Docker-Oberfläche gestartet und keine Telemetrie eingebaut. Der Browser kommuniziert ausschließlich mit dem angemeldeten Titan-Backend; Docker hat keinen öffentlich freigegebenen API-Port. Der aktuelle Arbeitsstand ist Titan 3.0.0; die Freigabegrenzen stehen in [QA-3.0.0](QA-3.0.0.md).

1. Einen benannten NAS-Speicherbereich wählen oder unter **Volumes** bei Bedarf ein lokales Docker-Datenvolume anlegen.
2. **Container erstellen**: Namen und Image, z. B. `nginx:stable`, eintragen. Bridge ist Standard; bei Host/Ohne Netzwerk die Portzuordnungen leer lassen. Eigene vorhandene Netze stehen in der Auswahl.
3. Ports zeilenweise als `8080:80/tcp`, Umgebung zeilenweise als `NAME=Wert` eintragen. Speicher aus der Liste wählen und das Ziel innerhalb des Containers setzen; Titan legt ein eigenes Datenverzeichnis an. RAM- und CPU-Limits sowie Neustartregel prüfen. USB/GPU/NPU werden ausschließlich nach erkannter, expliziter Auswahl durchgereicht.
4. Erstellen & starten. Aktionen zeigen ihren Status direkt im Bereich. Bei einem Startfehler bleibt ein erfolgreich angelegter Container zur Diagnose/erneuten Startauslösung erhalten.
5. Details & Logs zeigt Adressen, Ports, Neustartregel, Datenzuordnungen und letzte 150 Logzeilen. Umgebungsvariablen und deren Passwörter werden nicht in der Übersicht oder im Diagnose-JSON veröffentlicht.

Entfernen verwendet keine Force-/Prune-Aktionen. Nach Ja/Nein-Bestätigung wird ein laufender Container zuerst gestoppt; Datenverzeichnisse und Volumes bleiben erhalten. Ein Volume separat zu löschen entfernt dessen Daten endgültig und erfordert ebenfalls Ja/Nein. Docker lehnt das Entfernen noch verwendeter Volumes und Images ab.

Vorlagen-Apps sind zusätzlich sichtbar. Einzelaktionen betreffen ausschließlich den ausgewählten Container. Die ausdrücklich beschrifteten Paketaktionen unter **Projekte** werden an den Titan-App-Manager weitergegeben und steuern das gesamte zugehörige App-Paket. Die Prüfung von Images, Netzwerk und Datenpfaden bleibt erhalten.

RAM wird einschließlich Dateicache aus Linux-Cgroups gemessen. Wenn diese Messung nicht verfügbar ist, erscheint „—“. Gestoppte Container zeigen 0 B. Ein Docker-RAM-Limit ist eine Obergrenze, kein Verbrauchswert.

Die Oberfläche, Bildsymbole und der Backend-Code werden in Titan gepflegt. Unter **Vorlagen** ergänzt der mitgelieferte Katalog mit 374 normalisierten [LinuxServer.io- und Big-Bear-Konfigurationen](APP-STORES.md) die freie Container-Erstellung. Quellen können aktualisiert und ein- oder ausgeblendet werden. Diese Metadaten-/Compose-Prüfung ist keine Laufzeitfreigabe jeder Anwendung. Ein eigener AppStore mit betreuten Titan-Komplettpaketen ist aufgeschoben; externe Docker-Oberflächen werden nicht eingebettet.

## Eigene Docker-Netzwerke

**Docker → Netzwerke → Erstellen** legt ein eigenes Bridge-Netz an. Ein Name genügt; Titan wählt ein freies privates IPv4-Subnetz und Gateway nach Prüfung vorhandener Docker-Netze und Host-Routen. Unter **Erweiterte Einstellungen** kannst du Subnetz, Gateway und rein interne Kommunikation selbst festlegen. Ein Gateway ohne zugehöriges Subnetz ist nicht zulässig. Schlägt die Prüfung fehl, zeigt die Oberfläche den Grund und bestätigt keine erfolgreiche Erstellung.

Das fertige Netz steht bei **Container erstellen** und bei der App-Installation zur Auswahl. Ein Klick auf den Netznamen zeigt Subnetz, Gateway, verbundene Container und zugeordnete App-Pakete. Die Verwaltung wartet auf das tatsächliche Auftragsende und lädt die Netzliste neu.

**Entfernen** benötigt Ja/Nein und ist nur für ein ungenutztes, von Titan angelegtes Bridge-Netz verfügbar. Eingebaute Docker-Netze, fremde Netze und App-Netze werden nicht entfernt. Auch gestoppte Container oder installierte App-Pakete können ein Netz noch verwenden. Diese Zuordnung zuerst durch Entfernen oder unterstützte Neuerstellung mit einem anderen Netz auflösen. Titan verwendet hierfür keine Force- oder Prune-Aktion. Macvlan-, Overlay- und IPv6-Netze werden in dieser Oberfläche nicht neu eingerichtet.

## Kompakte Verwaltungsansicht

Auf dem Desktop zeigt die Container-Tabelle Namen, Image, Stack, Status, CPU/RAM, Adressen und direkte Aktionen. Mobil erscheint dieselbe Auswahl als kompakte Karten. Statusfilter und Namenssuche greifen gemeinsam; Suche und Cursor bleiben bei Hintergrundaktualisierungen erhalten. Die Live-Übersicht zeigt Containeranzahl, laufende/gestoppte Container, fehlerhafte Healthchecks und den gemessenen Gesamt-RAM. Fehlende aktive Messungen werden nicht als null Verbrauch ausgegeben.

Bis zu 64 Container können für Start/Stop/Neustart ausgewählt werden. Titan prüft die gesamte Auswahl zuerst, führt die Aktion für jeden ausgewählten Container einzeln aus und meldet Teilfehler ausdrücklich. Nicht ausgewählte Dienste eines App-Pakets bleiben unverändert. Vorhandene Compose-Projekte werden anhand ihrer Docker-Metadaten gruppiert; Stacks können gemeinsam gestartet, gestoppt und neu gestartet werden. Hierbei wird keine externe Compose-Datei ausgeführt oder überschrieben.


## Direktes Aktionsmenü und Geräte

Ein Klick auf den Container-Namen öffnet sein Aktionsmenü direkt neben der Übersicht, mobil darüber. Titan-Apps verwenden ihren tatsächlichen Webport für **App öffnen**. Einstellungen, Start/Stop/Neustart und Logs stehen zusammen; Entfernen liegt unter Weitere Aktionen und benötigt Ja/Nein.

Geräte ändern erfordert einen gestoppten Container. Für Vorlagen-Apps öffnet **App-Einstellungen** die gemeinsame App-Konfiguration. Manuelle Titan-Container bieten **Einstellungen & Geräte**: Titan bewahrt den alten Container gestoppt auf, erstellt eine lokale Kopie seiner beschreibbaren Dateischicht mit derselben Datenvolume-Zuordnung und startet die Kopie. Vorherige Sicherungen heißen `titan-previous-…`. Deren Entfernen löscht das weiterverwendete Volume nicht; dieses ist keine unabhängige Datensicherung. Nur unterstützte Konfigurationen werden übernommen, fremde Container bleiben bei diesem Umbau ausgeschlossen.

Der separate Einstellungsdialog unterstützt bei gestoppten manuellen Titan-Containern Ports, Umgebung, Limits und Neustartregel. Nach dieser Neuerstellung startet der Ersatzcontainer automatisch; der vorherige Container bleibt als gestoppte Sicherung erhalten. Vorlagenpakete bleiben nach dem Speichern ihrer App-Einstellungen dagegen gestoppt und können anschließend bewusst gestartet werden. Die Konsole führt Administratorbefehle innerhalb des Containers mit Zeit- und Ausgabebegrenzung aus; sie ist keine interaktive Terminal-Sitzung.

Der aktive Betriebssystemslot bleibt schreibgeschützt. Paket-/Sicherheitskorrekturen kommen zusammen mit Titan über [signierte Systemupdates](UPDATES.md). RAM-Prüfungen berücksichtigen auch den nächsten Autostart; nach zu stark verringerter RAM-Kapazität kann der Offline-Schutz Docker und libvirt anhalten. Die Verwaltung bleibt davon unabhängig. Swap zählt nicht als zusätzliche RAM-Kapazität.

Aufbewahrte Vorgängercontainer, lokale Volumes und App-Archive ersetzen keine unabhängige Datensicherung. System-Rollback setzt Container-Datenbanken nicht zurück. Eine vollständige App-/NAS-Wiederherstellung auf einem frischen Image ist nicht implementiert; siehe [QA-3.0.0](QA-3.0.0.md).
