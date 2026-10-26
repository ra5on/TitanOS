# Entwicklungsumgebung

Die am 25. Oktober 2026 geprüfte Umgebung verwendet **Fedora 44, x86-64**
als Entwicklungsrechner. Das gebaute NAS bleibt **Debian 13 (trixie)**.
Für andere Hosts gelten weiterhin die allgemeinen Voraussetzungen in der
[README](../README.md#entwicklung) und die [Release-Anleitung](RELEASING.md).

## Fedora einrichten

```sh
sudo dnf --repo=fedora --repo=updates install -y \
  git gh make gcc glibc-static python3 python3-pip curl tar xz unzip \
  apt dpkg debian-keyring squashfs-tools zstd zip minisign \
  qemu-system-x86-core qemu-img edk2-ovmf mtools util-linux \
  systemd-container debootstrap iptables-nft iproute kmod dnsmasq \
  busybox restic poppler-utils tesseract tesseract-langpack-deu \
  tesseract-langpack-eng exfatprogs samba-client \
  moby-engine docker-cli docker-compose
```

`apt` und `dpkg` werden auch auf Fedora benötigt: mkosi benutzt sie zum Bau
des Debian-Abbilds. `glibc-static` wird für den minimalen verschachtelten
Linux-Testgast der VM-Snapshot-Prüfung benötigt. Die explizite Repo-Auswahl
begrenzt diese Installation auf die Fedora-Paketquellen.

```sh
mkdir -p "$HOME/projects"
cd "$HOME/projects"
git clone https://github.com/ra5on/TitanOS.git
git clone https://github.com/ra5on/TitanOS-Apps.git
cd TitanOS
```

Beide Repositories bleiben eigenständig. Die Apps-Tests erwarten standardmäßig
`TitanOS-Apps` neben dem OS-Checkout; ein anderer Ort ist mit `--apps-dir` möglich.

### Toolchains im Benutzerkonto

Der geprüfte Stand ist in [dev-toolchains.lock.json](../scripts/dev-toolchains.lock.json)
festgehalten: Go **1.27.3**, Node **24.22.1**, npm **11.19.1** aus diesem
Node-Archiv und mkosi **25.3** vom dort genannten Git-Commit. Das Fedora-Go-Paket
war beim Test noch 1.26 und reicht für `backend/go.mod` nicht aus.

Im OS-Checkout ausführen; das folgende Beispiel lädt Go und Node mit festen
Prüfsummen und installiert sie ohne Administratorrechte:

```sh
python3 - <<'PY'
import hashlib, json, pathlib, tarfile, urllib.request

lock = json.loads(pathlib.Path('scripts/dev-toolchains.lock.json').read_text())
base = pathlib.Path.home() / '.local/share/titanos-dev'
downloads = base / 'downloads'
bins = pathlib.Path.home() / '.local/bin'
downloads.mkdir(parents=True, exist_ok=True)
bins.mkdir(parents=True, exist_ok=True)
for name in ('go', 'node'):
    entry = lock[name]
    archive = downloads / entry['url'].rsplit('/', 1)[1]
    urllib.request.urlretrieve(entry['url'], archive)
    with archive.open('rb') as stream:
        actual = hashlib.file_digest(stream, 'sha256').hexdigest()
    if actual != entry['sha256']:
        raise SystemExit(f'{name}: SHA-256 mismatch')
    with tarfile.open(archive) as packed:
        packed.extractall(base, filter='data')
    folder = base / ('go' if name == 'go' else f"node-v{entry['version']}-linux-x64")
    for tool in (('go', 'gofmt') if name == 'go' else ('node', 'npm', 'npx')):
        link = bins / tool
        target = folder / 'bin' / tool
        if link.is_symlink() and link.resolve() == target.resolve():
            continue
        if link.exists() or link.is_symlink():
            raise SystemExit(f'{link} already exists; resolve the conflict first')
        link.symlink_to(target)
PY
export PATH="$HOME/.local/bin:$PATH"
dev_tools="$HOME/.local/share/titanos-dev"
python3 -m venv "$dev_tools/mkosi-venv"
"$dev_tools/mkosi-venv/bin/pip" install \
  'git+https://github.com/systemd/mkosi.git@a31ed1fc8c6901ae3ee40d458761e261adca835e'
ln -s "$dev_tools/mkosi-venv/bin/mkosi" "$HOME/.local/bin/mkosi"
```

Python 3.14 wurde geprüft. Das Installationsbeispiel benötigt Python 3.12+
für die sichere Archivextraktion; die Anwendung hat dadurch keine neue
Python-Mindestversion. Einen bereits vorhandenen `mkosi`-Link prüfen, statt
ihn zu überschreiben. `$HOME/.local/bin` muss auch in neuen Shells im `PATH`
stehen; Fedora richtet dies üblicherweise in `~/.bashrc` ein.

```sh
go version
node --version
npm --version
mkosi --version
command -v newuidmap newgidmap
cat /etc/subuid /etc/subgid
unshare --user --map-auto --map-root-user true
```

Für den eigentlichen Abbildbau sind beide UID/GID-Helfer und ein eigener
Bereich in `/etc/subuid` und `/etc/subgid` erforderlich. Fehlende Zuordnungen
müssen vom Administrator vergeben werden; fremde Bereiche nicht kopieren.
QEMU nutzt KVM bei beschreibbarem `/dev/kvm`, sonst langsamere TCG-Emulation.

### GitHub und Docker

```sh
gh auth login --hostname github.com --git-protocol https --web
gh auth setup-git
gh auth status
git config --local user.name 'DEIN NAME'
git config --local user.email 'DEINE GITHUB-NOREPLY-ADRESSE'
```

Die Git-Identität in beiden Repositories separat setzen. Zugangsdaten gehören
in den Anmeldedienst, nicht in das Repository oder Testprotokolle.

Für Container auf dem **Entwicklungsrechner** optional:

```sh
sudo systemctl enable --now docker
sudo docker info
docker compose version
```

Die unten beschriebenen QEMU-Tests starten Docker **im NAS-Gast**. Sie benötigen
weder Zugriff auf den Docker-Socket des Hosts noch eine Docker-Gruppenmitgliedschaft.

## Bauen und lokal entwickeln

```sh
make dev
# Alternativ: Weboberfläche und statisches Binary bauen.
make build VERSION=0.22.0
```

`make dev` startet das Backend auf `127.0.0.1:8080` und Vite üblicherweise auf
`localhost:5173`. Entwicklungsdaten liegen unter `test/config/`. Ein fertiges
Binary startet mit `backend/bin/titand -dev -config test/config`; ohne
Ersteinrichtung ist `health.ready=false` im Entwicklungsmodus erwartbar.

```sh
make zip VERSION=0.22.0
(cd out && sha256sum -c titanos-0.22.0.zip.sha256)
```

Das erzeugt das vollständige Systemabbild und den Stick-Inhalt unter `out/`.
Die Versionsangabe ohne führendes `v` entspricht dem Release-Skript und dem
Smoke-Test. Ein lokaler Build wird damit **nicht** signiert oder veröffentlicht.
Für Veröffentlichungen gilt ausschließlich [RELEASING.md](RELEASING.md).

## Tests auf Fedora

[scripts/fedora-dev.sh](../scripts/fedora-dev.sh) stellt nur für den gestarteten
Prozessbaum einen kurzen Hostnamen und die erwarteten Debian-OVMF-Pfade bereit.
Fedora liefert die 4-MiB-Firmware als qcow2; der Helfer wandelt sie temporär in
raw um. Hostname und `/usr/share` des Hosts bleiben unverändert.

Im privaten User-Namespace erscheinen hostseitige root-SSH-Konfigurationsdateien
mit fremdem Eigentümer. Deshalb verwenden ausschließlich die Testprozesse
`ssh/scp -F /dev/null`. Explizite Testschlüssel und eigene `known_hosts` bleiben
aktiv. Den Helfer als normalen Benutzer **ohne sudo** aufrufen; für GitHub und
die normale Entwicklung wird er nicht benötigt.

```sh
scripts/fedora-dev.sh make test test-release
(cd ../TitanOS-Apps && python3 -m unittest discover -s scripts -p 'test_*.py' -v)
```

Eine vollständige virtuelle Prüfung nach dem Build:

```sh
mkdir -p out/validation/local
scripts/fedora-dev.sh python3 scripts/smoke-release.py \
  out/titanos-0.22.0.zip --log out/validation/local/boot-serial.log \
  > out/validation/local/boot.log 2>&1
scripts/fedora-dev.sh python3 backend/tests/smoke_workflows.py \
  out/titanos-0.22.0.zip backend/bin/titand \
  --log out/validation/local/workflows-serial.log \
  > out/validation/local/workflows.log 2>&1
scripts/fedora-dev.sh python3 scripts/test-storage-apps-qemu.py \
  --zip out/titanos-0.22.0.zip --apps-dir ../TitanOS-Apps \
  --output out/validation/local/storage-apps \
  > out/validation/local/storage-apps.log 2>&1
```

Die Aufrufe nacheinander ausführen: jeder NAS-Gast erhält 4 GiB RAM. Der
Prüfrechner hatte 12 GiB RAM und etwa 100 GiB freien Plattenplatz. Die Tests
erzeugen ausschließlich neue virtuelle USB-/Datenplatten und räumen sie beim
regulären Ende auf. Originalmedien oder produktive NAS niemals anschließen.

Der letzte Runner erweitert den vorhandenen Storage-Test um Nextcloud mit
MariaDB/Cron und Mosquitto aus dem angegebenen Apps-Checkout. Seine frischen
qcow2-Datenplatten sind dafür 16 statt 2 GiB groß. Voraussetzungen sind
`npm ci` im Webordner (durch `make build` erledigt), Internetzugang für die
Container-Images und `glibc-static`. Er verwendet die echte TitanOS-Compose-API,
zufällige Testzugänge und synthetische Dateien. Das Ergebnis mit Image-IDs
steht in `catalog-apps-result.json` im Ausgabeordner.

Der Workflow-Test injiziert das angegebene Binary in den RAM des NAS. Für eine
Aussage über dasselbe Abbild muss es zu dessen gebautem Binary passen. Der
Storage-/Apps-Test bootet das Binary unmittelbar aus dem ZIP. Der innere
VM-Snapshot-Gast ist ein minimaler Linux-Gast unter TCG; er ist kein Nachweis
für Windows, Desktopinstallation, verschachtelte KVM-Leistung oder Passthrough.

## Gemessener Prüfstand vom 25. Oktober 2026

Getesteter OS-Commit: `1f803ded377e42e6e60fe50b82cf8a91aefd463f`;
Apps-Commit: `d8ce929a333c8689361f3aeea18ba0a7b7640813`.
Der vollständige Build erzeugte Kernel `6.12.113+deb13-amd64`, ein etwa
433-MiB-Squashfs und ein 508-MiB-Stick-ZIP. SHA-256 des lokalen ZIPs:

```text
9cf3b1c8f807a4219ab2d451c5daed6c6d5176b549ca7fe54c3e1a11567c624f
```

| Prüfung | Ergebnis dieses Laufs |
|---|---|
| Go vet/Tests, Web, Svelte, Format | Bestanden; 81 Webtests, keine Svelte-Fehler/-Warnungen |
| Release-/Apps-Python-Tests | 42 Release- und elf Apps-Tests bestanden |
| Vollständiger Build | Debian-Abbild samt Microcode-/Firmware-/Kernelmodulprüfung bestanden |
| Zwei UEFI-Starts mit KVM | HTTPS, Ersteinrichtung, WebUI, Konto/Zertifikat/Einstellungen nach Neustart erhalten |
| mdadm | RAID1-Übernahme ohne Neuformatierung; RAID5 von drei auf vier Platten samt Dienstneustart und unveränderten Daten erweitert |
| Btrfs RAID1 | Vier Boots, Snapshots/Restore, fehlendes Mitglied, Ersatz, gesunder Remount und Scrub bestanden |
| Docker im NAS | Compose/CDI, Container-Reparatur, Sicherung/Restore und Wiederanlauf nach Dienstabsturz bestanden |
| Nextcloud 35.0.2 + MariaDB + Cron | HTTP, WebDAV-Anmeldung, Upload/Download und Daten/Konto nach App-Neuerstellung erhalten |
| Mosquitto | Angemeldetes MQTT, Ablehnung falscher/anonymer Zugriffe, Retained-Nachricht nach Neuerstellung erhalten |
| VMs | Unabhängiger Klon; laufender Linux-Gast mit RAM-/Platten-/UEFI-/TPM-Snapshot und Restore bestanden |

Details, Image-IDs und Prüfsummen der lokalen Protokolle stehen im
[maschinenlesbaren Prüfbericht](validation/development-environment-2026-10-25.json).
Rohprotokolle, Testschlüssel, Zugangsdaten, Binärdateien und VM-Medien bleiben
unter dem ignorierten `out/` und werden nicht veröffentlicht.

Das Bootlog enthielt Meldungen zu zunächst fehlendem `efivarfs`, fehlender
WLAN-`regulatory.db`, fehlender VSOCK-CID und beim Herunterfahren zu
`/run/live/medium`/Loop-Geräten. Beide angeforderten Neustarts waren erfolgreich;
die Meldungen wurden durch diese Einrichtung nicht behoben.

Dies bestätigt die beschriebenen Entwicklungs- und Funktionspfade, keine
pauschale Hardwareabnahme, alle RAID-Level, alle Katalog-Apps, Upgrades bestehender
Daten oder Dauerlast. Debian-Spiegel und `latest`-Container-Tags verändern sich:
die festen Toolchains und dokumentierten Schritte machen den Ablauf wiederholbar,
garantieren aber keinen bytegleichen zukünftigen Build. Für einen historischen
Vergleich die oben genannten Git-Stände separat auschecken.

## Netzwerk- und Workflow-Prüfung ab 0.23.1

```sh
scripts/fedora-dev.sh make test test-release
make zip VERSION=0.23.1
scripts/fedora-dev.sh python3 scripts/smoke-storage.py out/titanos-0.23.1.zip \
  --workflows023 --online-virtio --log out/validation/workflows023-serial.log
```

`--workflows023` ergänzt vier virtuelle Ethernetports an QEMU-internen Hubs:
VLAN-Paketverkehr mit IPv4/IPv6, Active-backup-Bond, Verwaltungsbestätigung,
VLAN-Wechsel/Rückfall, echte Docker-ipvlan/macvlan-Endpunkte, NFS-RO/RW,
Medienimport mit Dienstneustart, öffentliche CLI und Plugin-Aufräumen. Vier
NAS-Starts prüfen zusätzlich Persistenz und den bestehenden Btrfs-Ausfall/
Plattenersatz/Scrub. Die virtuellen Datenplatten sind hierbei 8 statt 2 GiB. Die optionale große VirtIO-ISO soll vor dem absichtlich degradierten Wiederaufbau ausreichend freie Btrfs-Blockgruppen für interne Metadaten lassen. Ein voller/fast vollständig allokierter Btrfs-Spiegel kann beim Ersatz mit ENOSPC abbrechen; der Test führt keine automatische Balance oder Datenlöschung aus.
`--online-virtio` lädt zusätzlich die offizielle stabile Windows-Treiber-ISO;
ohne Option wird dieser externe Download nicht als getestet bezeichnet.
`--tcg` erzwingt dieselbe CPU-Emulation wie auf CI-Rechnern ohne KVM; Prozessausgang und Host-RAM/-Platz werden bei Abbruch ausgegeben.
`--workflows-only` überspringt ausschließlich die schon getrennt geprüften
USB-/SMB-/CDI-/RAM-Snapshot-Teilsuiten für gezielte Wiederholungen; Storage-
Erzeugung und vier Neustarts bleiben erhalten. Nie gegen ein echtes NAS richten.

Die Browserprüfung benötigt einen frisch gestarteten Preview-Server **nach**
dem letzten `npm run build`; ein noch laufender Preview-Prozess kann auf alte
Asset-Hashes zeigen. Alle APIs werden abgefangen, keine NAS-Mutation:

```sh
npm --prefix web run preview -- --host 127.0.0.1 --port 5186
UI_URL=http://127.0.0.1:5186 PLAYWRIGHT_MODULE=/pfad/zu/playwright/index.mjs \
  node scripts/test-ui-023.mjs
```

Ergebnisse/Bilder liegen unter `out/validation/ui-023/`. Der Release-Bericht
nennt den konkreten Quellstand, erfolgreiche Messungen und offene Hardware-/
Gast-/Cloud-Prüfungen. Ein grüner Test ist kein Nachweis aller Netzadapter,
Switches, LACP-Konfigurationen, Windows-Versionen oder echter Tailnets.


## Öffentliches Systemupdate und Rückwechsel

Nach Veröffentlichung eines vollständigen Stable-Releases prüft dieser Test
Signatur und Boot eines alten öffentlichen ZIPs, den echten GitHub-Updateweg,
den bestätigten neuen Slot und den Rückwechsel. Besitzerkonto und HTTPS-Zertifikat
müssen über drei Starts erhalten bleiben. Alle Medien werden neu erzeugt; es
wird kein existierendes NAS angesprochen. Das Ziel muss beim Test noch das
aktuelle öffentliche Stable-Systemrelease sein.

```sh
gh release download v0.22.0 --dir out/public-v0.22.0 \
  --pattern 'titanos-0.22.0.zip*' --skip-existing
scripts/fedora-dev.sh python3 backend/tests/public_update_qemu.py \
  out/public-v0.22.0/titanos-0.22.0.zip \
  --from-version 0.22.0 --to-version 0.23.1 \
  --log out/validation/public-update-0231-serial.log \
  --result out/validation/public-update-0231.json
```

Ohne KVM kann `--tcg` verwendet werden. Die Signatur muss zu den bereits
vertrauenswürdigen öffentlichen Projektschlüsseln passen; ein Spiegel oder
alternativer Update-Endpunkt wird nicht gesetzt. App-Updates ohne Systemwechsel
werden weiterhin mit `scripts/smoke-app-update.py` gesondert geprüft.

Der gezielte 2FA-Nachweis aus 0.23.1 steht in
[der Validierungsdatei](validation/v0.23.1-authorization.json): zwei tatsächlich
getrennte Prozesse, paralleler Faktorverbrauch, alte Kontocaches, Neustart und
fehlgeschlagene Speicherung. Die Schreibfehlerfälle zusätzlich als normaler
Benutzer mit `cd backend && go test -race ./internal/auth -count=1` ausführen;
als Namespace-root werden diese reinen Dateirechtefälle ausdrücklich übersprungen.
