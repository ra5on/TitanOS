# VM-Snapshots

Auf der VM-Seite steht neben „Bearbeiten“ das Menü „Snapshots“ mit Erstellen,
Planen und Verwalten. Ein Snapshot enthält alle virtuellen qcow2-/raw-Platten,
die VM-Konfiguration, UEFI-NVRAM und den vollständigen emulierten TPM-Zustand.
Bei laufenden Gästen wird zusätzlich der RAM-Zustand gesichert. Ausgeschaltete
Gäste erhalten einen Snapshot ohne RAM.

## Unterbrechung und Voraussetzungen

Ein laufender Gast wird mit `virsh save --bypass-cache --running` gespeichert.
Er läuft während der Plattenkopien nicht und wird anschließend mit `virsh
restore` fortgesetzt. Das braucht eine ausdrückliche Bestätigung, auch beim
Einrichten eines Zeitplans. Netzwerkverbindungen können währenddessen auslaufen.
Btrfs-Reflinks werden nach Möglichkeit verwendet; ansonsten entstehen unabhängige,
sparse Kopien. Bei großen Platten kann die Unterbrechung entsprechend lange dauern.
Es gibt keine veränderlichen qcow2-Backing-Ketten.

PCI-/USB-Durchreichung, physische Platten und virtiofs-Freigaben sind nicht
vollständig erfassbar; für solche VMs ist die Funktion gesperrt. Ein noch
zugewiesenes Installationsmedium muss vorher ausgeworfen werden. Es werden
weder fremde Ordner noch eine externe ISO als vermeintlich vollständiger
Gastzustand erfasst. Kopien mit Backing-Dateien werden abgelehnt.

## Ablage und Wiederherstellung

Die unabhängigen Dateien und Metadaten liegen auf dem primären VM-Volume unter
`.titanos/vm-snapshots/<VM-UUID>/<Snapshot-ID>/`. RAM und Platten werden niemals
im RAM-Overlay oder auf dem Bootstick zwischengespeichert. Der private Elternordner
verhindert Zugriff durch normale NAS-Konten. Der Dateimanager schützt interne
Snapshot-Dateien sowie Platten und Firmware registrierter VMs.

Wiederherstellen ist nur bei ausgeschalteter VM und nach Eingabe des VM-Namens
möglich. Alle benötigten Volumes, Pfade, Dateitypen, Größen und der freie Platz
werden vorab geprüft. Gleichzeitige Snapshot-Vorgänge reservieren ihren Bedarf
gemeinsam pro Dateisystem; eine Reservierung wird erst nach Abschluss freigegeben.
Schreibzugriffe anderer Dienste können den verfügbaren Platz trotzdem verändern. Neue Dateien werden neben den bisherigen vorbereitet;
bisherige Dateien bleiben bis zum vollständigen Wechsel als Rückfallebene
vorhanden. Die VM-Konfiguration wird zuletzt atomar gespeichert. Ein RAM-Restore
startet die VM; ohne RAM bleibt sie zunächst ausgeschaltet. Der aktuelle Zeitplan
bleibt bei der Wiederherstellung erhalten.

Für RAM wird ein Fingerabdruck aus QEMU-Version, OVMF-Code und Host-CPU verglichen.
Nach inkompatiblen Änderungen bleibt die Wiederherstellung ohne RAM möglich.
Die von libvirt gespeicherte Hardwarebeschreibung wird beim RAM-Restore erhalten.

Ein Journal auf dem Daten-Volume sperrt andere VM-Aktionen bei einem unterbrochenen
Vorgang. Die Oberfläche bietet eine Wiederaufnahme bzw. Rücknahme unvollständiger
Dateiwechsel an. Wurde ein RAM-Restore bereits gestartet, wird dieser Zustand
niemals automatisch ein zweites Mal eingespielt: Der Besitzer muss nötigenfalls
den RAM verwerfen und später frisch booten. Das verhindert, dass alter RAM mit
zwischenzeitlich weitergeschriebenen Platten kombiniert wird.

Snapshots lassen sich einzeln nach einem Bestätigungsdialog löschen, auch während die VM
läuft. Beschädigte/unvollständige Einträge bleiben sichtbar und können nach
Bestätigung ihrer ID entfernt werden. Vor dem Entfernen einer VM müssen deren
Snapshots ausdrücklich gelöscht werden. Ein Snapshot auf demselben Volume ist
kein Ersatz für ein externes Backup.

## Zeitpläne

Pro VM: Wochentage, Uhrzeit in NAS-Zeitzone, RAM-/Unterbrechungswahl und 1–100
aufzubewahrende geplante Snapshots. Der Plan gehört zur VM-Konfiguration auf dem
Stick; zyklische Prüfungen lesen ihn ausschließlich aus RAM. Die letzte Ausführung
liegt auf dem Daten-Volume. Verpasste Termine werden nicht nachgeholt; Neustarts
und die wiederholte Stunde bei Zeitumstellung erzeugen keine doppelten Läufe.
Ohne RAM-/Unterbrechungserlaubnis wird eine laufende VM nicht gesichert.

Alte geplante Snapshots werden erst nach erfolgreicher Sicherung und Fortsetzung
entfernt. Manuelle Snapshots sind von der Aufbewahrungsregel ausgenommen. Fehler
bleiben in der VM-Snapshot-Ansicht sichtbar und erscheinen im Systemprotokoll.

## Grundlagen und Prüfung

- [libvirt save/restore](https://libvirt.org/manpages/virsh.html#save)
- Unit-Tests: unabhängige Plattenkopien, TPM-Baum, Pfad-/Geräteausschlüsse,
  Namensbestätigungen, abgebrochener Dateitausch, RAM-Wiederholungsschutz,
  Zeitplan/Retention, Sparse-Dateien und beschädigte Metadaten.
- `scripts/vm_snapshot_smoke.py` startet einen eigenen minimalen Linux-Testgast
  mit unterschiedlichen RAM- und Plattenmarkierungen. Er verändert beide nach
  der Aufnahme und verlangt nach dem Restore die alten Werte zurück. Nur in
  einer isolierten Test-NAS-VM mit Wegwerf-Datenträgern ausführen.
