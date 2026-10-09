# Eigene QCOW2-Festplatten importieren

Unter **Virtuelle Maschinen → Weitere → Eigene virtuelle Maschine** ein ISO-, IMG- oder QCOW2-Abbild hochladen oder aus **Dateien** auswählen. Ein QCOW2-Abbild ist eine vorhandene Festplatte, kein Installationsmedium. CPU, RAM, Netzwerk und BIOS/UEFI passend zum ursprünglichen System einstellen; die Zielplatte muss mindestens so groß wie die virtuelle Quellplatte sein.

Titan erstellt eine unabhängige Festplatte und lässt die Quelldatei unverändert. Der Import arbeitet mit einer privaten Kopie in einer abgeschotteten Umgebung ohne Netzwerk, persönliche Verzeichnisse, Host-Konfiguration oder Host-Geräte. Dafür ist vorübergehend Platz für die Quellkopie und die Zielplatte erforderlich. Die Kopie wird auch nach Fehlern entfernt.

Images mit Backing-Dateien, externen Datendateien oder Verschlüsselung werden abgewiesen. Solche Images zuvor auf ihrem ursprünglichen System zu einer eigenständigen QCOW2-Datei zusammenführen. Cloud-Images benötigen gegebenenfalls eine eigene Cloud-init-Konfiguration; beim eigenen Import richtet Titan keine Zugangsdaten für das Gastsystem ein.

Die Isolation verwendet [Bubblewrap](https://github.com/containers/bubblewrap). Ist die Isolation nicht verfügbar, wird der Import nicht außerhalb der Sandbox ausgeführt.
