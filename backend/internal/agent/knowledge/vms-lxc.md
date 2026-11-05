# VMs und LXC-Systemcontainer
_Mitgeliefert mit TitanOS, wird bei Updates ersetzt. Eigene Anleitungen gehören in den Ordner Wissen außerhalb von „TitanOS“._

Diese Anleitung erklärt, wie du virtuelle Maschinen und LXC-Systemcontainer startest und stoppst, vor Änderungen Snapshots anlegst und Konsole und Ressourcen nutzt.

## Unterschied in Kürze

- **VM (KVM/QEMU):** ein ganzer Rechner mit eigenem Kernel, z. B. Windows, Home Assistant OS oder ein anderes Linux. Reiter **VMs**.
- **LXC-Systemcontainer:** ein ganzes Linux-System (Debian, Ubuntu, Alpine …), das den Kernel des NAS mitbenutzt. Startet in Sekunden, braucht wenig RAM. Reiter **LXC**.
- Beides gibt es nur für Besitzer und Administratoren. Unter Einstellungen › Dienste lassen sich VMs und LXC ausschalten.

## Was der Agent darf

- Lesen: `vm_list` (Zustand, vCPUs, RAM, Last, Adressen laufender VMs), `lxc_list` (Zustand, Distribution, CPU, RAM), `hardware_status` (welche VM ein Gerät nutzt).
- Ändern mit deiner Freigabe: `vm_action` (start, shutdown, reboot, poweroff, remove, remove_with_disks) und `lxc_action` (start, stop, restart, kill).
- **Nicht:** VM- oder LXC-Snapshots anlegen oder zurücksetzen, Konsolen öffnen, VMs anlegen. Das machst du in der Oberfläche; der Agent sagt dir, wo. (Automatische Snapshots vor Änderungen gibt es nur für Btrfs-Volumes, bei App-Updates und Dateiänderungen, siehe „Backups“.)
- `remove_with_disks` schlägt der Agent nur vor, wenn du ausdrücklich das Löschen der Platten willst.

## Starten und stoppen

### VM

1. `vm_list` prüfen: Zustand und Name.
2. Starten: `vm_action` start.
3. Herunterfahren: `vm_action` shutdown. Das ist ein sauberes Herunterfahren über ACPI; der Gast braucht dafür seine Unterstützung (bei Linux meist der Gastagent oder `acpid`).
4. Reagiert der Gast nicht: erst warten, dann `poweroff` (wie Stecker ziehen, ungespeicherte Daten im Gast gehen verloren). Nur nach Rückfrage.
5. Danach `vm_list` prüfen.
6. Autostart und Zeitpläne (Start/Herunterfahren an Wochentagen) stellst du auf der VM-Seite ein.

### LXC

1. `lxc_list` prüfen.
2. `lxc_action` start, stop (bis 60 Sekunden zum Herunterfahren), restart.
3. `kill` erzwingt das Ausschalten; nur wenn stop nicht wirkt und nach Rückfrage.
4. Änderungen an Name, Grenzen, Netzwerk oder NAS-Ordnern gelten ab dem nächsten Start.

## Snapshots vor Änderungen

Vor Updates im Gast, Umbauten oder Experimenten erst einen Snapshot anlegen. Der Agent schlägt das vor und wartet, bis du es erledigt hast.

### VM-Snapshots

1. Auf der VM-Seite das Menü **Snapshots** öffnen, „Erstellen“.
2. Ein Snapshot enthält alle virtuellen Platten (qcow2/raw), die Einstellungen, UEFI und TPM. Bei einer laufenden VM auch den RAM-Zustand.
3. Eine laufende VM wird dafür **angehalten**, bis die Platten kopiert sind (mit Bestätigung). Netzwerkverbindungen des Gasts können dabei abbrechen.
4. Vorher ein eingelegtes Installationsmedium (ISO) auswerfen.
5. **Nicht möglich** bei VMs mit durchgereichten PCI-/USB-Geräten, physischen Platten oder virtiofs-Ordnern.
6. Wiederherstellen nur bei ausgeschalteter VM. Der Platz wird vorher geprüft.
7. Zeitpläne mit Aufbewahrung (1–100) gibt es je VM. Verpasste Termine werden nicht nachgeholt.
8. Der Snapshot liegt auf demselben Volume und ersetzt kein Backup.

### LXC-Snapshots

1. Nur wenn das Volume beim Anlegen des Containers Btrfs war.
2. Anlegen geht auch bei laufendem Container (Stand der Platte, kein RAM).
3. Wiederherstellen nur bei ausgeschaltetem Container.
4. Keine Zeitpläne und keine Aufbewahrungsregeln; alte Snapshots von Hand entfernen.

Für eine echte Sicherung VMs und LXC in einen Backup-Auftrag aufnehmen (Bereiche `vms` und `lxc`; VMs können dafür sauber angehalten werden). Laufende LXC-Container werden dabei nur absturzkonsistent erfasst.

## Konsole

- **VM:** Reiter „Bildschirm“ auf der VM-Seite (noVNC im Browser), dazu eine serielle Konsole im Terminal-Panel. Es gibt keinen VNC-Port im Netz.
- **LXC:** im Terminal-Panel „Konsole“ (Login-Shell im Container) oder „Textkonsole“ (verlassen mit Strg+a q).
- Konsolen öffnen mit deiner bestehenden Anmeldung, enden nach 15 Minuten ohne Eingabe und beim Abmelden und stehen im Protokoll.
- Der Agent öffnet keine Konsolen. Braucht es einen Befehl im Gast, nennt er ihn dir zum Eintippen.

## Ressourcen

1. `vm_list` bzw. `lxc_list` zeigen CPU und RAM im Betrieb; `system_status` die Last des ganzen NAS.
2. **VM:** vCPUs und RAM im VM-Formular; Kerne lassen sich festlegen oder exklusiv reservieren. Platten lassen sich nur vergrößern.
3. **LXC:** CPU-Grenze (Rechenzeit, keine Kernbindung) und RAM-Grenze; ohne Angabe unbegrenzt.
4. Faustregel: dem NAS selbst genug RAM lassen (für Dateicache, Docker, titand). Zeigt `system_status` dauerhaft knappen RAM, Grenzen senken oder weniger gleichzeitig laufen lassen.
5. Hält eine VM wegen eines Plattenfehlers an, meldet TitanOS das; erst die Ursache klären (Speicherplatz, Platte), dann auf der VM-Seite fortsetzen.

## Netzwerk

- VM: nur ausgehend (QEMU-Usermode) oder eigene Adresse im Heimnetz über eine Bridge wie `br0`.
- LXC: Bridge des Heimnetzes mit fester MAC-Adresse, Adresse vom Router, oder ohne Netz.
- VMs und LXC an einer Bridge sind eigene Geräte im Netz; die TitanOS-Firewall schützt sie nicht.

## Sicherheit bei LXC

- Ab Werk **unprivilegiert**: root im Container ist auf dem NAS niemand.
- **Privilegiert** nur, wenn eine Software es wirklich braucht: root im Container ist dann root des NAS.
- NAS-Ordner wahlweise nur lesend einbinden.
