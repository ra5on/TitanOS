# LXC-Systemcontainer

Systemcontainer (LXC) liegen zwischen Docker-Apps und VMs: ein ganzes Linux-System mit eigenem init, eigenen Diensten und eigener Paketverwaltung, das den Kernel des NAS mitbenutzt. Sie starten in Sekunden, brauchen kein KVM und kaum RAM über das hinaus, was ihre Programme selbst belegen.

Reiter **LXC** (`/lxc`, bis 0.40 „Container“), nur für Besitzer und Administratoren. Backend-Paket `backend/internal/lxc`, API unter `/api/v1/lxc`.

## Ablage

| Was | Wo |
|---|---|
| Einstellungen eines Containers | `/config/lxc/<name>.json` (neue Datei; ältere titand kennen sie nicht und lassen sie liegen) |
| lxcpath | `/mnt/<volume>/lxc` (0711, root) |
| Root-Dateisystem | `/mnt/<volume>/lxc/<name>/rootfs` |
| LXC-Konfiguration | `/mnt/<volume>/lxc/<name>/config`, **vor jedem Start** aus `/config` neu geschrieben |
| Snapshots (nur Btrfs) | `/mnt/<volume>/lxc/<name>/snapshots/<JJJJMMTT-hhmmss>` |
| Abbild-Cache, temporäre Dateien des Downloads | `/mnt/<volume>/lxc/.cache`, `/mnt/<volume>/lxc/.tmp` (0700) |
| Umgebung der Unit | `/run/titanos-lxc/<name>.env` (`LXC_PATH=…`, nur RAM) |
| Autostart erledigt | `/run/titanos/lxc-autostarted` (nur RAM, überlebt einen titand-Neustart) |

Nichts davon liegt im RAM-Overlay oder auf dem Stick, außer der kleinen JSON-Datei in `/config`. Der Download-Template von LXC bekommt `LXC_CACHE_PATH` und `TMPDIR` auf dem Volume, damit auch das heruntergeladene `rootfs.tar.xz` nie in `/var/cache` oder `/tmp` (RAM) landet.

`/config/lxc/<name>.json` enthält `name`, optional `displayName` und `description`, `volume`, `distribution`, `release`, `arch`, optional `cpus` und `memoryMiB` (0/fehlend = unbegrenzt), `network`, `mac`, optional `mounts` (`path`, `target`, `readOnly`), `autostart`, optional `privileged` und `btrfs`, `created`. Alle neuen Felder sind optional.

## Anlegen

- Abbilder von `images.linuxcontainers.org` über das `download`-Template von LXC. Die Liste (`/meta/1.0/index-system`, Variante `default`, Architektur des NAS) holt titand beim Öffnen des Dialogs und hält sie sechs Stunden im RAM; nach einem Fehlschlag frühestens nach einer Minute erneut, bis dahin gilt die letzte bekannte Liste. Ein Container lässt sich nur aus einem Abbild dieser Liste anlegen.
- Anlegen läuft im Hintergrund (`lxc-create -P <lxcpath> -n <name> -B dir|btrfs -f <erste Konfiguration> -t download -- --dist … --release … --arch …`) mit den Phasen Vorbereiten, Laden/Entpacken, Einrichten. Die Seite darf geschlossen werden. Ein fehlgeschlagenes Anlegen bleibt mit der Ausgabe von LXC sichtbar; man entfernt den Container (mit Daten) und legt ihn neu an.
- Ein bereits vorhandener Ordner `/mnt/<volume>/lxc/<name>` wird nie überschrieben (`lxc_exists`).
- Abbild, Volume, Architektur und privilegiert/unprivilegiert sind nach dem Anlegen fest. Name, Beschreibung, Grenzen, Netzwerk, MAC, NAS-Ordner und Autostart lassen sich ändern; sie gelten ab dem nächsten Start.

## Rechte: unprivilegiert ab Werk

- Ab Werk **unprivilegiert**: Die IDs 0–65535 des Containers sind die IDs 100000–165535 des NAS (`lxc.idmap`). Root im Container ist auf dem NAS niemand.
- Ausnahme: Gruppe 100 des Containers ist die Gruppe `users` (GID 100) des NAS, der alle Konten und Freigabeordner gehören (2770, setgid). So können Prozesse im Container, die in der Gruppe `users` sind, in eingebundene Freigaben schreiben. Andere Dateien des NAS erscheinen im Container als `nobody`/`nogroup`.
- Das Abbild trägt dafür `root:100000:65536` in `/etc/subuid` und `/etc/subgid` sowie `root:100:1` in `/etc/subgid` ein.
- **Privilegiert** ist eine ausdrückliche Option beim Anlegen, mit Warnung: root im Container ist root des NAS. Nur für Software, die es wirklich braucht.
- Ohne `apparmor_parser` im Abbild setzt TitanOS `lxc.apparmor.profile = unconfined` (sonst würde LXC auf einem Kernel mit eingeschaltetem AppArmor den Start verweigern); mit ihm `generated`. Die Isolierung beruht damit auf User-Namespaces, cgroups und den Standard-Einschränkungen von LXC (`common.conf`/`userns.conf`, Capabilities, seccomp).

## Grenzen, Netzwerk, Ordner

- CPU: `lxc.cgroup2.cpu.max = <n×100000> 100000` (n CPUs Rechenzeit, keine Kernbindung). RAM: `lxc.cgroup2.memory.max`.
- Netzwerk wie bei VMs, ohne lxc-net: `bridge` (Heimnetz-Bridge `br0`), `network:<id>` (eine bestätigte Bridge aus Einstellungen › Netzwerk) oder `none` (nur loopback). Mit Bridge bekommt der Container ein veth-Paar `eth0` an der Bridge, eine feste MAC (ab Werk zufällig aus `00:16:3e:…`, frei wählbar als Unicast-Adresse) und seine Adresse vom Router. `lxc-net` (NAT-Bridge mit eigenem DHCP) ist im Abbild maskiert, ebenso `lxc.service` und `lxc-monitord.service`.
- Konfigurierte Container schützen ihre Netze wie VMs vor Entfernen/Umbau (`NetworkReferences` in `connection.CheckChange`). Start an eine Bridge hält die Netzwerk-Sperre (`AttachmentLease`) wie ein VM-Start.
- NAS-Ordner: bis 16 Bind-Mounts (`lxc.mount.entry … none bind,create=dir[,ro]`). Quelle muss ein Ordner unterhalb eines Volumes sein (nicht die Volume-Wurzel, keine Punkt-Ordner wie `.titanos`, nicht `lxc/`), Links werden aufgelöst. Ziel: absoluter Pfad ohne Leerzeichen, nicht `/`, nicht ein Systemordner selbst (`/etc`, `/usr`, `/var`, …) und nichts unter `/proc`, `/sys`, `/dev`, `/run`. Die Ordner werden bei jedem Start erneut geprüft.

## Betrieb

- Gestartet wird über die Unit `titanos-lxc@<name>.service` (`lxc-start -F`, `ExecStop=lxc-stop -t 60`, `Delegate=yes`). Ein Neustart von titand (App-Update) lässt laufende Container weiterlaufen; beim Herunterfahren des NAS stoppt systemd die Units und damit die Container sauber.
- Herunterfahren: `systemctl stop` (der Container bekommt bis 60 Sekunden zum Herunterfahren). Ausschalten erzwingen: `lxc-stop -k`, danach `systemctl stop`; einfacher Bestätigungsdialog. Neu starten: Herunterfahren, Konfiguration neu schreiben, Starten.
- Zustand, CPU und RAM liest titand alle fünf Sekunden aus den cgroup-Dateien (`cgroup.events`, `cpu.stat`, `memory.current`) von `lxc.payload.<name>` bzw. der Unit; LXC und systemd werden dafür nicht aufgerufen, `/config` nicht gelesen. CPU in Prozent der erlaubten CPUs.
- Autostart: einmal je NAS-Start, sobald das Volume eingebunden und das gewählte Netz bereit ist. Wer einen Container stoppt, dem startet ihn auch ein titand-Neustart nicht wieder.
- Konsole im Terminal-Panel (Art „LXC“): **Konsole** ist eine Login-Shell im Container (`lxc-attach --clear-env`, bash falls vorhanden, sonst sh), **Textkonsole** ist die tty des Containers (`lxc-console -t 0`, verlassen mit Strg+a q). Beide wie alle Konsolen: bestehende Administrator-Anmeldung ohne zweite Passwortabfrage, abschaltbar mit den Terminals, Ende nach 15 Minuten ohne Eingabe und mit der Anmeldung, im Protokoll vermerkt.
- Entfernen: nur ausgeschaltet. Ab Werk bleibt der Ordner auf dem Volume. Das Häkchen „Auch die Daten löschen“ löscht `/mnt/<volume>/lxc/<name>` samt Snapshots (Btrfs-Subvolumes per `btrfs subvolume delete`); einfacher Bestätigungsdialog, die Oberfläche schickt den Namen selbst als Bestätigung. Ist unterhalb des Ordners noch etwas eingebunden (`/proc/self/mountinfo`), wird nichts gelöscht (`lxc_mounted`) – so kann ein hängender Bind-Mount keine Freigabe mitreißen.
- Die Seite zeigt die erzeugte LXC-Konfiguration zur Kontrolle (`GET /api/v1/lxc/<name>/config`).

## Snapshots (Btrfs)

- Liegt das Volume beim Anlegen auf Btrfs, wird das Root-Dateisystem ein Subvolume (`lxc-create -B btrfs`, Feld `btrfs`). Nur dann gibt es Snapshots.
- Anlegen: `btrfs subvolume snapshot -r` nach `snapshots/<Zeit>`, auch bei laufendem Container (Stand auf der Platte in diesem Moment, kein RAM-Zustand).
- Wiederherstellen nur bei ausgeschaltetem Container: zuerst beschreibbare Kopie `rootfs.restore` aus dem Snapshot, dann `rootfs` → `rootfs.old`, Kopie → `rootfs`, zuletzt `rootfs.old` löschen. Reste eines abgebrochenen Vorgangs werden beim nächsten Wiederherstellen weggeräumt. Einfacher Bestätigungsdialog.
- Keine Zeitpläne und keine Aufbewahrungsregeln; der Snapshot liegt auf demselben Volume und ersetzt kein Backup.

## Sicherungen, Updates, Wartung

- Anlegen und Snapshot-Arbeit melden sich als beschäftigt (`Busy`): App-Updates und Wartungsaufträge warten darauf.
- Die Sperre der Anwendungssicherungen gilt auch für `/api/v1/lxc`: Solange eine Anwendungssicherung läuft oder geprüft werden muss, sind Container-Aktionen und Konsolen gesperrt (`application_backup_running`); Ansehen bleibt möglich. Container werden von Anwendungssicherungen noch nicht selbst angehalten; der Bereich `/mnt/<volume>/lxc` ist kein eigener Sicherungsbereich. Ein laufender Container wird bei einer Ordnerkopie nur absturzkonsistent erfasst.
- Entwicklungsmodus (`titand -dev`): Container werden angezeigt, aber nichts wird angelegt, gestartet, gestoppt oder gelöscht; kein Autostart.

## API

| Methode und Pfad | Zweck |
|---|---|
| `GET /api/v1/lxc` | Liste mit Zustand, Netzen, Host-CPU/RAM, `installed`, `dev` |
| `GET /api/v1/lxc/images` | Abbilder (RAM-Cache) und ggf. Fehler |
| `POST /api/v1/lxc` | Anlegen (Hintergrund, 202) |
| `PUT /api/v1/lxc/{name}` | Einstellungen ändern |
| `DELETE /api/v1/lxc/{name}` | Entfernen, optional `{"deleteData": true, "confirm": "<name>"}` |
| `POST /api/v1/lxc/{name}/{start,stop,restart,kill}` | Betrieb |
| `GET /api/v1/lxc/{name}/console?mode=shell\|console` | WebSocket-Konsole |
| `GET /api/v1/lxc/{name}/config` | erzeugte LXC-Konfiguration |
| `GET/POST /api/v1/lxc/{name}/snapshots`, `POST …/snapshots/{id}/restore`, `DELETE …/snapshots/{id}` | Snapshots |

Alles nur für Besitzer/Administratoren; Anlegen und Snapshot-Aktionen über die gemeinsame Aufnahme schwerer Vorgänge. Nicht im MCP-Werkzeugkatalog und nicht in der öffentlichen API.

## Abbild

`build/mkosi.conf`: `lxc` und `wget` (das Download-Template von LXC 6 braucht `wget`, `tar` und `xz`; keine GPG-Prüfung mehr, nur HTTPS). Keine `lxc-templates`, kein `debootstrap`, keine Recommends (kein lxcfs, kein dnsmasq). `build/mkosi.postinst.chroot` maskiert die LXC-Dienste und trägt die ID-Bereiche ein; die Unit liegt unter `build/mkosi.extra/usr/lib/systemd/system/titanos-lxc@.service`. Neue Pakete bedeuten ein volles Systemrelease.

## Prüfstand und Grenzen

- Geprüft mit nachgebautem Runner und künstlichen cgroup-/mountinfo-Dateien (`backend/internal/lxc/lxc_test.go`): Prüfung der Einstellungen, erzeugte Konfiguration (idmap, Grenzen, Netz, Ordner, AppArmor), Befehlsfolgen für Anlegen/Starten/Stoppen/Erzwingen/Neustart/Löschen/Snapshots, Autostart, Entwicklungsmodus; API-Rechte und Sperre der Anwendungssicherungen in `backend/internal/api/lxc_test.go`; Webhilfen in `web/tests/lxc.test.mjs`.
- **Nicht geprüft:** ein echtes Abbild mit LXC (mkosi braucht root), echtes `lxc-create`/`lxc-start` auf dem NAS, der tatsächliche cgroup-Pfad unter systemd mit `Delegate=yes`, das Verhalten beim Herunterfahren des NAS, AppArmor im Debian-Kernel, Konsolen mit echtem `lxc-attach`/`lxc-console`, Btrfs-Snapshots auf einem echten Volume, Schreibrechte in Freigaben über die Gruppe `users`. Vor einem Release in einer QEMU-Wegwerf-VM abnehmen.
- Alle unprivilegierten Container teilen sich denselben ID-Bereich (100000–165535); ein Ausbruch aus einem Container hätte damit die Rechte der Dateien der anderen Container. Getrennte Bereiche je Container sind ein möglicher Ausbau.
- Kein Dashboard-Kacheltyp für Container, keine Zeitpläne, kein Klonen, keine Geräte-Durchreichung (GPU), kein verschachteltes Docker im Container (`lxc.apparmor.allow_nesting = 0`, keine `nesting`-Option), keine Übernahme vorhandener LXC-Container.
