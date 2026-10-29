# VM-Einstellungen auf dem Volume und „VM übernehmen“

Entscheidung des Projektinhabers vom 29.10.2026. Maßgeblich für eine VM bleibt
`/config/vms/<name>.json` auf dem Stick: titand liest die Datei beim Start und
hält sie im Speicher. Zusätzlich liegt im Ordner der VM auf ihrem Volume eine
Kopie, damit sich die VM ohne den Stick wieder einrichten lässt – nach dem
Verlust des Sticks ohne Sicherung, nach dem Entfernen einer VM ohne ihre Platten
oder wenn das Volume an ein anderes TitanOS wandert.

## Die Kopie `vm.json`

- Ort: `/mnt/<volume>/vms/<ordner>/vm.json`, also im Ordner der VM auf ihrem
  Hauptvolume (`volume` der VM; Ordner = `directory` bzw. der Name). Eine VM mit
  Platten auf mehreren Volumes hat die Kopie nur dort; die übrigen Platten
  stehen in den Einstellungen und zusätzlich mit vollem Pfad unter `diskPaths`.
- Inhalt: `format` (1), ein kurzer Hinweis, `vm` (genau das, was in
  `/config/vms/<name>.json` steht), `diskPaths` und gegebenenfalls
  `removed`/`removedAt`. Keine Geheimnisse; Datei 0600, root.
- Geschrieben wird sie überall dort, wo titand die VM-Einstellungen speichert
  (`persistVM`: Anlegen, Bearbeiten, Anzeigename, Zeitpläne, CD-Wechsel,
  Vergrößern, fertige Plattenkopie, Klon, Snapshot-Wiederherstellung,
  Übernehmen) – direkt nach dem atomaren Schreiben auf den Stick. Sie entsteht
  atomar (Teildatei, fsync, Umbenennen, fsync des Ordners) über `volumedata`:
  Das Volume muss eingebunden sein und wird mit `os.Root` und der
  Kernel-Mount-ID festgehalten; `vms` und der VM-Ordner dürfen keine Links sein
  und müssen auf demselben Mount liegen. Ein fehlender Ordner wird nicht
  angelegt, ein fehlendes Volume nie durch RAM/Overlay ersetzt.
- Ein Fehler beim Schreiben der Kopie macht das Speichern nicht rückgängig; er
  steht im Protokoll. Gezählt wird nur der Stick.
- Beim Start von titand: Sobald das Volume einer VM eingebunden ist, wird ihre
  Kopie einmal mit den Einstellungen im Speicher verglichen und nur bei einem
  Unterschied (oder wenn sie fehlt) neu geschrieben. Danach nicht wieder bis zum
  nächsten Start von titand oder der nächsten Änderung; kein zyklisches Lesen
  oder Schreiben und kein Zugriff auf `/config` dafür. Eine Änderung, während
  das Volume fehlt, wird auf diese Weise nachgeholt, sobald es wieder da ist.
- Entfernen einer VM:
  - mit „Platten löschen“: die Kopie wird mit gelöscht (vor dem Ordner, der
    danach nur verschwindet, wenn er leer ist);
  - ohne: die Kopie bleibt und bekommt `"removed": true` und `removedAt`. So
    lässt sich eine versehentlich entfernte VM mit ihren Platten wieder
    übernehmen. Die Markierung verschwindet beim Übernehmen.
- Ältere titand kennen die Datei nicht, lesen sie nie und lassen sie liegen.
  Schreibt eine ältere Version die VM, kann die Kopie bis zum nächsten Start
  einer aktuellen Version veraltet sein.

## VM übernehmen

Auf der VM-Seite (Besitzer und Administratoren) öffnet „VM übernehmen …“ einen
Dialog; ohne VMs steht der Knopf im Seitenkopf, sonst als Symbol neben „+“ über
der VM-Liste.

- `GET /api/v1/vms/adopt` sucht nur dann, also nie im Status-Polling, in
  `vms/*/vm.json` aller eingebundenen Volumes (höchstens 512 Ordner je Volume,
  keine Links, Kopie höchstens 1 MiB, nur Format 1). Angezeigt werden Kopien,
  deren VM hier nicht registriert ist: weder dieselbe UUID noch eine VM, die
  diesen Ordner benutzt. Je Eintrag: Name, Ordner, vCPU/RAM, Platten, ob
  entfernt, ob der Name vergeben ist, was sich ändern wird und welche Volumes
  fehlen (dann nicht wählbar).
- `POST /api/v1/vms/adopt` mit `volume`, `directory` und optional `name` liest
  die Kopie erneut vom Volume (der Browser schickt keine Einstellungen) und
  prüft wie beim Anlegen (`prepare`): Name, Vorlage, Firmware, RAM/vCPU,
  Installationsmedium, Netzwerk, Ordner. Dazu:
  - Platten, die TitanOS angelegt hat, müssen als gewöhnliche Datei mit
    einfachem Namen im VM-Ordner ihres eingebundenen Volumes liegen
    (kein `..`, kein Link), ein qcow2/raw-Abbild ohne Backing-Datei sein und
    keiner anderen VM gehören. Direkt genutzte Abbilder müssen auf einem
    eingebundenen Volume liegen; ganze Platten per `/dev/disk/by-id` müssen
    frei sein (in keinem Volume, keiner anderen VM, nicht in Prüfung).
  - Der Ordner darf keiner anderen VM gehören.
  - Wurde das Volume oder der Ordner umbenannt, zählt der Fundort: Platten im
    alten Ordner werden auf den neuen umgeschrieben.
- Was sich ändert (Codes in der Antwort `changes`, in der Oberfläche
  übersetzt): PCI-/USB-Auswahl und der GPU-Wechsel entfallen (die Hardware kann
  eine andere sein), Autostart aus, Energie- und Snapshot-Zeitplan
  ausgeschaltet (ihre Einstellungen bleiben), feste Kerne/exklusive
  Reservierung entfallen, wenn die CPUs nicht passen bzw. die Reservierung immer,
  vCPUs auf die CPUs dieses NAS begrenzt, die MAC bleibt außer bei einer
  Kollision mit einer registrierten VM, ein nicht gefundenes Installationsmedium
  bzw. nicht gefundene NAS-Ordner entfallen, ein unbekanntes benanntes Netz
  oder eine nicht bereite Bridge wird zu „nur ausgehend“, ein vergebener
  Anzeigename entfällt. UUID bleibt, damit vorhandene Snapshots
  (`.titanos/vm-snapshots/<UUID>`) weiter zur VM gehören.
- Ist der Name vergeben, antwortet die API mit `vm_exists`; der Dialog fragt
  nach einem neuen Namen (Vorschlag `<name>-2`). Der Ordner behält dann seinen
  Namen (`directory`). Keine Namenseingabe zur Bestätigung: ein einfacher
  Dialog nennt VM, Ordner, Platten und Änderungen.
- Danach wird `/config/vms/<name>.json` normal geschrieben (und die Kopie neu).
  Die VM startet nicht von selbst. Im Entwicklungsmodus lehnt die API ab
  (`read_only`). Fehler: `vm_copy_not_found` (keine lesbare Kopie),
  `invalid_folder`, `invalid_image`, `vm_volume_unavailable`, `invalid_vm_name`.

## Grenzen und Tests

- Unit-Tests in `backend/internal/vm/volumecopy_test.go` mit Temp-Ordnern und
  nachgebautem `qemu-img`: Kopie beim Anlegen/Bearbeiten, nicht ohne Volume,
  einmaliges Auffrischen beim Start, keine Links, Entfernen mit/ohne Platten,
  Übernehmen nach Stickverlust, Namens-/MAC-/Anzeigenamen-Kollisionen,
  ungültige Pfade und Formate. API-Tests in
  `backend/internal/api/vm_adopt_test.go` (Rechte, Entwicklungsmodus, Ablauf).
- Nicht geprüft: echte Volumes mit `/mnt`-Mount-ID-Prüfung, ein Umzug eines
  Volumes zwischen zwei echten NAS und der Start einer übernommenen VM mit
  echtem libvirt.
- Die Kopie ersetzt keine Sicherung des Sticks: Konten, Freigaben und andere
  Einstellungen stehen nur dort.
