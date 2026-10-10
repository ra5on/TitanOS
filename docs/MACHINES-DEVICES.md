# Ordner, Datenfestplatten und PCI-Geräte für virtuelle Maschinen

Alle drei Bereiche stehen unter **Virtuelle Maschinen → Maschine →
Einstellungen** und lassen sich nur bei **ausgeschalteter** Maschine ändern.
USB-Geräte beschreibt die [USB-Anleitung](MACHINES-USB.md).

## Ordnerfreigaben

Ein Ordner aus **Dateien** (persönlicher Ordner, externes Laufwerk oder
Netzlaufwerk) wird live in die Maschine eingebunden. Titan und die Maschine
sehen dieselben Dateien.

1. **Ordner freigeben** wählen und den Ordner auswählen.
2. Den Namen im Gast bei Bedarf anpassen; **Nur lesen** schützt den Ordner vor
   Änderungen aus der Maschine.
3. Maschine starten und den Ordner im Gast einhängen, zum Beispiel:

   ```sh
   sudo mkdir -p /mnt/dokumente
   sudo mount -t virtiofs dokumente /mnt/dokumente
   ```

   Dauerhaft über `/etc/fstab`: `dokumente /mnt/dokumente virtiofs defaults 0 0`

- Linux-Gäste bringen den Treiber mit. Windows benötigt den VirtIO-FS-Treiber
  aus den VirtIO-Treibern und WinFsp.
- Die Maschine greift mit vollen Rechten auf den Ordner zu. Dateien, die sie
  anlegt, gehören dem Benutzer aus dem Gast und lassen sich über die
  Netzwerkfreigabe von Titan möglicherweise nicht ändern.
- Ist das Laufwerk mit dem Ordner beim Start nicht angeschlossen, startet die
  Maschine ohne diese Freigabe.
- Ältere Systeme (Windows 7, Windows 98, Legacy-Profile) werden nicht
  unterstützt.

## Datenfestplatten

Eine zusätzliche virtuelle Festplatte, deren Abbild in einem Ordner deiner Wahl
liegt, zum Beispiel auf einem externen Laufwerk. Im Gast erscheint sie als
weitere Festplatte (`/dev/vdj`, `/dev/vdk`, …) und muss dort wie jede neue
Festplatte partitioniert und formatiert werden.

- Die Datei heißt `<Maschine>-data-<Kennung>.qcow2` und belegt nur den
  tatsächlich genutzten Platz.
- Vorhandene Datenfestplatten lassen sich vergrößern, nicht verkleinern.
- **Entfernen löscht das Abbild mit allen Daten**, ebenso das Deinstallieren der
  Maschine.
- Datenfestplatten sind **nicht Teil der Maschinen-Sicherung**. Sichere wichtige
  Daten getrennt.
- Fehlt das Laufwerk beim Start, startet die Maschine ohne diese Festplatte.

## PCI-Geräte durchreichen (Beta)

Grafikkarten, NPUs und andere PCI-Geräte gehören der Maschine, solange sie
läuft. Titan, Apps und andere Maschinen können das Gerät in dieser Zeit nicht
verwenden; nach dem Ausschalten erhält Titan es zurück.

Voraussetzungen:

- **VT-d** (Intel) bzw. **AMD-Vi/IOMMU** ist im BIOS/UEFI aktiviert. Fehlt
  das, zeigt Titan einen Hinweis statt der Geräteliste.
- Hardwarevirtualisierung (KVM) ist verfügbar und die Maschine hat die
  Architektur des Titan.

Was du wissen solltest:

- Geräte einer **IOMMU-Gruppe** lassen sich nur gemeinsam durchreichen; ein
  Schalter schaltet die ganze Gruppe. Bei Grafikkarten gehört meist die
  Audiofunktion dazu.
- Titan behält Geräte, die es selbst benötigt: Controller mit angeschlossenen
  Laufwerken, die aktive Netzwerkkarte und alles, was sich mit ihnen eine Gruppe
  teilt.
- Wird die Grafik durchgereicht, über die Titan sein Bild ausgibt, bleibt der
  lokale Monitor dunkel. Die Weboberfläche bleibt erreichbar. Apps wie Jellyfin
  oder Ollama können diese Grafik dann nicht mehr nutzen.
- Die Maschine startet nur, wenn alle zugewiesenen Geräte vorhanden sind.
- Der Gast braucht den passenden Treiber des Herstellers.
- Nicht jede Hardware eignet sich. Integrierte Grafik und manche Karten
  lassen sich nicht oder nur mit Einschränkungen durchreichen. Startet die
  Maschine nicht, entferne das Gerät wieder in den Einstellungen.

Dieser Bereich ist als **Beta** gekennzeichnet: Er wird im Release mit
automatischen Tests der Geräteerkennung geprüft, nicht mit echter Hardware.

## Snapshots

Ein Snapshot hält den Stand der Systemfestplatte einer Maschine fest, zum
Beispiel vor einem Update von Home Assistant. Später lässt sich die Maschine
auf diesen Stand zurücksetzen.

1. Fahre die Maschine herunter.
2. Öffne ihre Einstellungen und gib unter **Snapshots** einen Namen ein, etwa
   „Vor dem Update“. **Snapshot erstellen** legt ihn sofort an.
3. Starte die Maschine wieder.

Zum Zurücksetzen fährst du die Maschine herunter und wählst beim Snapshot
**Wiederherstellen**. Alle Änderungen seit dem Snapshot gehen dabei verloren.
Willst du den aktuellen Stand behalten, erstelle vorher einen weiteren Snapshot.

- Enthalten sind die Systemfestplatte und die UEFI-Einstellungen. Nicht
  enthalten sind Ordnerfreigaben, Datenfestplatten und ein virtuelles TPM.
- Snapshots liegen in der Festplattendatei der Maschine. Sie belegen dort so
  viel Platz, wie sich seit dem Snapshot geändert hat, und sind Teil der
  Maschinen-Sicherung.
- Pro Maschine sind bis zu 10 Snapshots möglich.
- Wurde die Festplatte nach dem Snapshot vergrößert, hat sie nach dem
  Zurücksetzen wieder die frühere Größe.
- Ein Snapshot ersetzt keine Sicherung: Geht die Festplattendatei verloren,
  sind auch ihre Snapshots weg.

## Autostart und Startverzögerung

**Mit TitanOS starten** legt fest, ob eine Maschine nach dem Hochfahren von
TitanOS von selbst startet. Mit der **Startverzögerung** wartet sie danach noch
15 Sekunden bis 10 Minuten. Maschinen ohne Verzögerung starten zuerst, die
anderen in der Reihenfolge ihrer Wartezeit. So ist zum Beispiel ein Router oder
DNS-Server oben, bevor die übrigen Maschinen starten.
