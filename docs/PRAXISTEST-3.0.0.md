# Titan 3.0.0 — Praxistest

Diese zehn Punkte prüfen Titan nach dem Boot über die Oberfläche. Verwende
eigene Testdateien, Testkonten und Testcontainer. Notiere je Punkt **Bestanden**,
**Fehler** oder **Nicht geprüft**, dazu Titan-Version, Gerät/Browser und bei einem
Fehler einen Screenshot oder den genauen Meldungstext. **Nicht geprüft** zählt
nicht als bestanden. Die automatischen Nachweise stehen in [QA-3.0.0](QA-3.0.0.md).

1. **Desktop und Smartphone.** Öffne Dateimanager, Docker, Fotos und
   Systemsteuerung. Verschiebe, verkleinere, minimiere und öffne Fenster erneut;
   ordne Desktop-Verknüpfungen um. Prüfe am Smartphone Hoch-/Querformat und das
   eingeklappte Widget. **Bestanden:** Inhalte und Aktionen bleiben erreichbar,
   nichts überlagert wichtige Bedienelemente, kein unbeabsichtigtes seitliches
   Scrollen. Ein mobil geöffnetes Fenster nutzt auf einem größeren Bildschirm
   wieder den verfügbaren Platz.

2. **Benutzer und SMB-Rechte.** Erstelle eine Testfreigabe und Testkonten für
   Lesen/Schreiben, nur Lesen und keinen Zugriff. Öffne die angezeigte
   Freigabeadresse auf einem zweiten Gerät mit dem jeweiligen Konto.
   **Bestanden:** Nur das Schreibkonto kann Dateien anlegen und ändern; das
   Lesekonto kann sie öffnen; das ausgeschlossene Konto erhält keinen Zugriff.
   Rechteänderungen werden anschließend tatsächlich wirksam.

3. **Dateimanager.** Erstelle einen Testordner sowie eine Text- und eine
   Python-Datei über die angebotenen Dateitypen. Bearbeite Text mit Umlauten,
   speichere und öffne ihn erneut. Kopiere, verschiebe und benenne Testdateien um;
   verschiebe eine in den Papierkorb und stelle sie wieder her. **Bestanden:**
   Inhalte bleiben erhalten, Aktionen oben sind auch mobil erreichbar, nur die
   ausgewählte Datei wird verändert. Löschen verlangt Ja/Nein; Systemdateien
   bleiben geschützt. Textdateien lassen sich auch nach Umbenennen auf eine
   andere Endung bearbeiten.

4. **Fotos mit echten Uploads.** Lege eine Bibliothek auf einem angebotenen
   NAS-Speicherbereich an. Wähle über Smartphone und Desktop echte JPEG-/PNG-Fotos
   aus und lade sie hoch. Öffne die Vorschau, setze einen Favoriten, lege ein
   Album an und teste Papierkorb/Wiederherstellen. **Bestanden:** Bilder und
   Zuordnungen bleiben nach erneutem Öffnen erhalten; eine hochgeladene Datei
   lässt sich als Original herunterladen und öffnen.

5. **Eigener Docker-Container und Netzwerk.** Erstelle unter Docker ein eigenes
   Bridge-Netz und einen Testcontainer, beispielsweise mit dem Image
   `nginx:stable`. Wähle dieses Netz, einen freien Webport und ein RAM-Limit.
   Öffne die App, stoppe und starte den Container, prüfe Details und Protokolle.
   **Bestanden:** Status und tatsächliche Erreichbarkeit stimmen überein;
   gestoppt steht der Live-RAM-Verbrauch auf null. Ein verwendetes Netz lässt
   sich nicht entfernen. Nach Entfernen des Testcontainers mit Ja/Nein lässt
   sich auch das nun ungenutzte Testnetz entfernen.

6. **LinuxServer.io-Vorlage.** Öffne Docker → Vorlagen und richte eine kleine
   LinuxServer.io-Anwendung ein, zum Beispiel nginx. Prüfe Speicher-, Port- und
   Netzwerkvorgaben sowie die Hinweise zur ersten Anmeldung. Ändere einen freien
   Webport und öffne die Anwendung über Titan. **Bestanden:** Die eingestellten
   Werte werden verwendet; zusammengehörende Dienste erscheinen als Projekt;
   Start/Stop und Entfernen funktionieren. Fehlende eigene Pflichtangaben werden
   verständlich angezeigt, ohne eine erfolgreiche Einrichtung vorzutäuschen.

7. **RAM und Bedienbarkeit unter Last.** Beobachte die Systemressourcen während
   einer Vorlageninstallation. Öffne gleichzeitig Ordner und Vorschauen im
   Dateimanager und stoppe einen anderen Testcontainer. **Bestanden:** Die
   Verwaltung bleibt bedienbar, Ordner laden innerhalb von zehn Sekunden und
   Fehler oder knapper RAM werden verständlich gemeldet. Container-/VM-Limits
   werden nicht als Live-Verbrauch ausgegeben. Optional bei einer Test-VM:
   Mit bewusst zu kleinem zugewiesenem RAM kalt starten und anschließend wieder
   mit ausreichend RAM starten. Der Schutz muss Docker/VMs anhalten, die
   Verwaltung erreichbar lassen und sich nach Wiederherstellung erholen.

8. **Echtes Gastbetriebssystem und VNC.** Verwende getrennte Test-VMs für UEFI
   und BIOS mit einem jeweils unterstützten Gastbetriebssystem. Wähle Speicher,
   CPU/RAM und ein echtes Installationsmedium; führe die Gastinstallation durch.
   Tippe im Gast ein paar Zeichen in ein Textfeld. Schließe und öffne die
   integrierte Konsole erneut und teste eine kurze Browser-Netzunterbrechung.
   **Bestanden:** Der Gast startet erneut, Bild und Tastatur funktionieren und
   die Konsole verbindet sich ohne notwendige Tastenkombination wieder. Ein
   Bootmoduswechsel macht ein bereits installiertes Gast-OS nicht automatisch
   mit dem anderen Modus kompatibel.

9. **USB/GPU nur bei vorhandener Hardware.** Wähle in einem passenden
   Testcontainer ein tatsächlich erkanntes, unterstütztes Gerät mit verständlichem
   Namen. Prüfe dessen Funktion in der Anwendung und entferne die Zuordnung
   anschließend wieder. **Bestanden:** Nur gewählte Geräte sind nutzbar;
   verschwundene oder ersetzte Geräte werden nicht stillschweigend verwechselt.
   Ohne geeignete Hardware/Treiber bleibt dieser Punkt **Nicht geprüft**; eine
   sichtbare Auswahl allein belegt keine funktionierende Beschleunigung.

10. **Signiertes Systemupdate und Rollback.** Sobald ein echtes signiertes
    Angebot verfügbar ist: Unter Systemsteuerung → Updates prüfen, installieren
    und laufende Test-VMs vor dem separaten Neustart geordnet herunterfahren.
    Den Neustart mit Ja/Nein bestätigen. Danach Version,
    Testdateien, Benutzerrechte und App-Erreichbarkeit prüfen. Einen angebotenen
    Rückkehrstand auswählen, laufende Test-VMs wieder geordnet herunterfahren
    und den NAS-Neustart bestätigen. **Bestanden:** Der gewählte
    Systemstand läuft und die vorhandenen Daten/Rechte bleiben erhalten. Ohne
    Angebot oder verfügbaren Rückkehrstand bleibt dieser Teil ausstehend; ein
    Release-Entwurf ist noch kein Updateangebot.

## Grenze bei Sicherung und Wiederherstellung

System-Rollback setzt weder Nutzerdaten noch App-Datenbanken zurück. Die
vorhandene Sicherung ist **keine vollständige Wiederherstellung auf einem neuen,
leeren NAS**: Konten, Speicherzuordnungen, Appinstallationen und das integrierte
Rückspielen von App-Datenbanken werden dafür noch nicht vollständig aufgebaut.
Auch ein erfolgreicher Praxistest bestätigt diese fehlende Funktion nicht.
Details und weitere ausstehende Freigabeprüfungen stehen in [QA-3.0.0](QA-3.0.0.md).
