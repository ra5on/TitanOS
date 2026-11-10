# Eine Version veröffentlichen

TitanOS installiert nur Updates, deren Manifest von einem Schlüssel aus
seinem Abbild signiert ist. Die Release-Pipeline signiert zusätzlich das
Installations-ZIP, alle Plugin-Manifeste und `SHA256SUMS` für sämtliche Release-Dateien.

## Ab 0.5.0: ein gemeinsamer Kanal

Neue System-Releases verwenden reguläre Tags wie `v0.5.0` und werden als Stable
veröffentlicht. Optionale Beta-Funktionen werden in der Oberfläche und im
Changelog gekennzeichnet. Alte Beta-Manifeste bleiben für Kompatibilitäts- und
Rückwechseltests unterstützt; es gibt keine neue parallele Beta-Release-Reihe.
Vor Veröffentlichung den Wechsel Beta 9 → 0.5.0 und den Rückwechsel prüfen.
Der Storage-Smoke-Test prüft zusätzlich SMB-Kopien gegen einen verschlüsselten
Samba-Server in einem separaten Netzwerk-Namensraum des Wegwerf-Gasts.

## Einmalig: Schlüssel und GitHub einrichten

Das GitHub-Environment `release` verlangt eine Freigabe von `ra5on` und
akzeptiert nur Tags `v*`. Private Sicherheitsmeldungen sind aktiviert.
Diese Einstellungen gehören zum Repository und werden nicht durch die
Workflow-Datei allein eingerichtet.

Neue NVIDIA-Treiber werden ab beta.6 aus dem authentifizierten öffentlichen
NVIDIA-Repository geladen und auf ausdrücklichen Knopfdruck auf dem NAS gebaut.
Der frühere CI-Treiberworkflow ist nur noch manuell verfügbar. Die passende
Bauumgebung wird weiterhin pro OS-Release gebaut und über dessen signiertes
Manifest geprüft; sie liegt auf dem Daten-Volume, nicht im RAM-System.
Siehe [NVIDIA-LOCAL-BUILD.md](NVIDIA-LOCAL-BUILD.md).

Seit dem 14. Oktober 2026 sind beide öffentlichen Schlüssel eingebaut und die
Secrets `MINISIGN_SECRET_KEY` und `MINISIGN_PASSWORD` eingerichtet:

| Schlüssel | Minisign-Key-ID | Verwendung |
|---|---|---|
| `titanos-release-1.pub` | `8EAABC2A42904C1D` | reguläre Releases, privater Teil verschlüsselt im Environment `release` |
| `titanos-recovery-1.pub` | `E58A91B20B598ABB` | Reserve; privater Teil wird nicht bei GitHub hinterlegt |
| `titanos-release-2.pub` | `10239C7F77392DC9` | ab 3.11.2026 in den Environments `release` und `security-release` |
| `titanos-recovery-2.pub` | `725EF673B860F5F6` | Reserve ab 3.11.2026; offline beim Inhaber, nicht bei GitHub |

Beide Schlüssel wurden lokal erzeugt und durch Signaturprüfung getestet.
Der Projektinhaber hat am 14. Oktober 2026 die externe Sicherung der Schlüssel
und der Passphrasen bestätigt und die Veröffentlichung der ersten Beta
beauftragt. Eine Offline-Verwahrung des Reserve-Schlüssels ist damit noch
nicht bestätigt: Eine Cloud-Kopie ist kein Offline-Medium. Der Reserve-Schlüssel
wurde auf dem Entwicklungsrechner erzeugt; seine lokale private Kopie besteht
noch. GitHub-Secrets ersetzen keine eigene Sicherung.

Die folgenden Schritte dokumentieren die Einrichtung; vorhandene Schlüssel
nicht neu erzeugen oder überschreiben. Bei einem Wechsel neue Dateinamen
verwenden und den Ablauf unter „Schlüsselwechsel und Verlust“ beachten.

1. Auf einem vertrauenswürdigen Rechner **außerhalb dieses Repositorys**
   ein Verzeichnis mit Modus `0700` anlegen. Dort mit
   [minisign](https://jedisct1.github.io/minisign/) ausführen:

   ```bash
   umask 077
   minisign -G -p titanos-release-1.pub -s titanos-release-1.key
   ```

   Ein starkes Passwort vergeben. Private Datei und Passwort getrennt und
   dauerhaft sichern. Niemals in Git, Chat, Issues oder Build-Artefakten ablegen.

2. Einen zweiten Schlüssel als Reserve erzeugen, möglichst auf einem
   Offline-Rechner:

   ```bash
   umask 077
   minisign -G -p titanos-recovery-1.pub -s titanos-recovery-1.key
   ```

   Dieser private Schlüssel bleibt offline und wird **nicht** bei GitHub
   hinterlegt. Auch seine Passphrase separat sichern.

3. Nur beide `.pub`-Dateien nach
   `build/mkosi.extra/usr/share/titanos/update-keys/` kopieren und committen.
   Keine Testschlüssel übernehmen. `plugin-keys` verweist auf `update-keys`: offizielle
   Plugins verwenden dieselben Release-/Reserve-Schlüssel.

4. Im Environment `release` die Secrets setzen. Mit GitHub CLI, aus dem
   Verzeichnis mit dem privaten **Release**-Schlüssel:

   ```bash
   gh secret set MINISIGN_SECRET_KEY --repo ra5on/TitanOS --env release < titanos-release-1.key
   gh secret set MINISIGN_PASSWORD --repo ra5on/TitanOS --env release
   ```

   Beim zweiten Befehl die Minisign-Passphrase interaktiv eingeben. Sie
   nicht als Kommandozeilenargument setzen. Der Projektinhaber muss selbst
   bestätigen, dass beide Schlüssel und Passphrasen sicher verwahrt sind.

## Ab 0.8.0: App-Releases und volle Releases

Ein Tag wird entweder ein **volles Release** (neues Systemabbild mit mkosi,
neue Plugins, Neustart des NAS) oder ein **App-Release** (nur titand mit der
eingebauten Weboberfläche, ohne Neustart des NAS). Das entscheidet
`scripts/release-kind.py` im Release-Workflow, nicht der Mensch:

- Basis ist das Systemabbild des neuesten Releases davor (`app.base` in dessen
  Manifest). Releases vor 0.8.0 kennen das Feld nicht; der erste Tag danach
  wird deshalb immer voll.
- Haben sich seit dem Tag der Basis nur `backend/`, `web/`, `docs/`, die
  Browserprüfungen, `examples/` und `agent.yml` (der frühere Beispiel-Agent,
  inzwischen entfernt) und die Projektdateien (README, CLAUDE,
  CONTRIBUTING, …) geändert, wird es
  ein App-Release. Das gilt auch für `release.yml` und `release-kind.py`
  selbst: Sie laufen nur im CI und gehören nicht zum Abbild (Entscheidung
  des Projektinhabers vom 4.11.2026). Soll eine Änderung an `release.yml`
  das Abbild ändern, braucht der Tag `[system]`. Jede andere Änderung, etwa unter
  `build/`, `boot/`, `scripts/` (außer Browserprüfungen), `.github/` (außer
  `release.yml`) oder am `Makefile`, ergibt ein volles Release. Ebenfalls
  kein Systemanteil: `.gitignore` und die Prüfungen des QEMU-Speichertests
  (`scripts/workflows023_smoke.py`), die nur im CI gegen ein volles Abbild
  laufen. Die vollständige Liste steht in `ALLOWED` in `release-kind.py`
  (Nachtrag 8.11.2026: das Aufräumen am selben Tag hatte 0.67.0 und 0.68.0
  versehentlich zu vollen Releases gemacht; beide warteten dann vergeblich
  auf die Plugin-Sets des neuen Kernels; ebenso der erste Tag v0.70.0 wegen
  des neuen Aufnahmeskripts `scripts/capture-readme-screenshots.mjs`. Der Lauf
  wurde abgebrochen, der unveröffentlichte Tag gelöscht und nach der Korrektur
  neu gesetzt).
- Eine Tag-Nachricht mit `[system]` erzwingt ein volles Release. Das gilt überall in
  der Nachricht, auch in erklärendem Text: Release-Notes eines App-Updates nennen
  die Markierung deshalb nicht wörtlich (v0.47.3 lief so versehentlich als volles
  Release).

Ein App-Release baut kein Abbild. `scripts/repack-release.py` lädt alle
Dateien der Basis von GitHub, prüft sie wie der Publish-Job mit den
öffentlichen Schlüsseln aus diesem Repository, entpackt das squashfs, setzt
den neuen titand ein, erzeugt das Lizenzinventar für Go und npm neu (Debian
wird aus dem Abbild gelesen) und packt mit denselben Optionen wie
`mkosi.finalize` wieder. Kernel, initrd, Boot-Loader und Plugins sind die der
Basis, Byte für Byte. Danach läuft alles wie gewohnt: `make-release.sh` mit
`BASE_VERSION`, Signatur im geschützten Publish-Job, `smoke-release.py` mit
Plugins. `smoke-storage.py` entfällt, weil Abbild und Plugins schon mit der
Basis geprüft wurden.

Jedes Release enthält zusätzlich `titanos-<version>-titand` und im Manifest
das Feld `app` (`base`, `asset`, `sha256`, `size`). Ein NAS ab 0.8.0 mit
passender Basis lädt nur diese Datei; ältere Versionen ignorieren das Feld und
installieren wie bisher die drei Systemdateien. `make-release.sh` prüft, dass
`titand` genau das im Abbild ist und das Abbild seine Basis-Version
(`/usr/share/titanos/base-version`) nennt. Ablauf auf dem NAS: siehe
[APP-UPDATES.md](APP-UPDATES.md).

Der Probelauf auf Pull Requests baut immer das volle Abbild und spielt darauf
mit dem Wegwerf-Schlüssel App-Updates durch (`scripts/smoke-app-update.py`):
eines, das läuft (ohne Neustart, Anmeldung bleibt, nach einem Neustart des
NAS noch da), eines, das nicht startet (nach drei Starts zurück), und den Weg
zurück zum titand des Systemabbilds.

Danach spielt er volle Systemupdates durch (`scripts/smoke-system-update.py`,
PLAN 76 F-13-7 und F-13-5), ebenfalls mit dem Wegwerf-Schlüssel und einem
Release-Server auf dem Host: Jedes Release wird über die API in den anderen
Slot installiert; den Stick liest das Skript vom Host mit mtools (Namen in
`loader/entries`, bei GRUB dazu `boot/grub/grubenv`).

- **good:** ein neueres System. Der Eintrag trägt beim Probestart seinen Zähler
  (`+2-1`), das NAS läuft aus dem anderen Slot, der Boot-Check entfernt den
  Zähler, `/api/v1/update` zeigt das System bestätigt.
- **nostart:** ein System, dessen titand nicht startet. Der Zähler sinkt mit
  jedem Start, der vierte Start ist wieder das System von vorher; titand
  schreibt `config/system/update-failed.json`, meldet den Fehlschlag und
  installiert dieses Release nicht noch einmal von selbst.
- **panic:** ein System mit unbrauchbarer initrd. Der Kernel gerät in Panik,
  `panic=10` startet neu, nach drei Starts läuft das System von vorher.
- **hang:** ein System, das vor titand im Notfallmodus stehen bleibt (ein
  Mount, der nie kommt, über die Kommandozeile des Releases).
  `titanos-trial-watchdog.timer` startet es jeweils nach 30 Minuten neu, nach
  drei Starts läuft das System von vorher.

Mit SeaBIOS/GRUB (`--firmware bios`) laufen im Workflow `good` und `nostart`;
das Skript kann dort alle vier (`--scenarios`).

Grenzen, auch im Kopf des Skripts genannt: `good` und `nostart` sind das
Abbild des ZIP, entpackt und mit anderem titand und anderer Basis-Version neu
gepackt (zstd 6 statt 19), Kernel und initrd bleiben die des ZIP; es ist also
kein Wechsel auf einen anderen Kernel. Die Manifeste tragen keine
Kernel-Variante. Bei `hang` läuft die Uhr der VM über QEMUs Befehlszähler
(`-icount sleep=off`) und springt im Leerlauf vor: die 30 Minuten vergehen im
Gast, nicht auf dem Host; Abbild und Timer bleiben unverändert (`--real-time`
wartet sie ab). Nicht abgedeckt: echte Hardware, 32-Bit-UEFI, Plugins und
Volumes im Boot-Check, der Rückwechsel („Zurück zur vorigen Version“) und ein
Start, der im initramfs oder in einer hängenden Unit ohne Notfallmodus stehen
bleibt.

Der Probelauf startet nur, wenn ein Pull Request Dateien aus der Liste
`paths` in `release.yml` ändert (Abbild, Boot, Plugin-Katalog, QEMU-Prüfungen).
Release-Notizen unter `docs/releases/` gehören nicht dazu: Sie werden erst beim
Veröffentlichen aus der Tag-Nachricht gelesen. Pflicht vor dem Merge ist nur
`build`; ein Pull Request muss nicht auf dem neuesten `main` stehen (seit
4.11.2026). `main` wird nach jedem Merge erneut geprüft.

**Debian-Updates und Autopilot:** App-Releases behalten die Debian-Pakete
ihrer Basis. `autopilot.yml` vergleicht jede Nacht die Pakete des neuesten
Releases mit trixie, trixie-updates, trixie-security und (Kernel „Neu“)
trixie-backports. Mit der Variable `AUTOPILOT=on` baut und veröffentlicht er
daraus selbst ein Patch-Release, ohne Freigabe, über das Environment
`security-release`. **Für diese Sicherheits-Releases ersetzen die QEMU-Tests
des Release-Workflows den Test auf dem echten Stick** (Entscheidung
3.11.2026). Ohne die Variable öffnet er wie früher einen Pull Request, der
`build/debian-security.txt` anlegt oder ändert; nach dem Merge wird der
nächste Tag ein volles Release. In den Repository-Einstellungen muss dafür
„Allow GitHub Actions to create and approve pull requests“ an sein.
Einzelheiten, Einrichtung und Grenzen: [AUTOPILOT.md](AUTOPILOT.md).

**Downloads früherer Releases (ab 0.71.1):** `release-kind.py`, `repack-release.py` und `viewer-image-reuse.py` laden über `open_release` (in `release-kind.py`). Serverfehler (5xx) und abgebrochene Verbindungen werden bis zu viermal mit wachsender Pause wiederholt; danach holt es die Datei über die GitHub-API (Token des Laufs, `GH_TOKEN`). Am 9.11.2026 antwortete die Download-Adresse einzelner Dateien stundenlang mit 504, während die API sie lieferte; die Läufe von 0.70.2 und 0.71.0 brachen daran ab. `repack-release.py` läuft nur in CI und löst daher kein volles Release aus.

## Ab 0.41: zweiter Kernel, Stick-Abbild und GRUB

- **Kernel „Neu“** ([KERNEL.md](KERNEL.md)): Ein volles Release baut das
  Abbild zweimal, `scripts/make-zip.sh` und `scripts/make-zip.sh --kernel-new`
  (mkosi-Profil `kernel-new`, Kernel und Firmware aus trixie-backports).
  `make-release.sh` packt die zweite Fassung als
  `titanos-<version>-new-{vmlinuz,initrd.img,filesystem.squashfs}` und
  beschreibt sie im Manifest unter `variants.new`; `verify-release.py` prüft
  sie mit. App-Releases übernehmen die Variante der Basis
  (`repack-release.py`). Plugin-Sätze werden für beide Kernel geholt bzw.
  bestellt (`kernelVariant=new` an das Plugin-Repository). Ins Release kommen
  nur die Plugins des stabilen Kernels. Fehlt der Satz des neuen Kernels,
  warnt der Workflow nur.
- **GRUB** ([BOOT.md](BOOT.md)): Das Abbild baut `BOOTIA32.EFI` und den
  BIOS-Kern von GRUB (`scripts/make-grub.sh` in `mkosi.postinst.chroot`).
  `make-zip.sh` legt sie mit `boot/grub/grub.cfg` auf den Stick. Mit
  `STICK_IMAGE=1` (im Workflow an) erzeugt `make-release.sh` zusätzlich
  `titanos-<version>.img.xz` (4 GiB, MBR, GRUB im MBR) für Boards ohne UEFI.
  Es steht in `SHA256SUMS`; die Prüfsummenliste ist signiert.
- Der Workflow bootet zusätzlich die Variante mit neuem Kernel
  (`smoke-release.py --kernel-new`) und das ZIP mit SeaBIOS und 32-Bit-OVMF
  (`--firmware bios|ia32`), sofern das Release beides enthält.

## Ab 0.61.0: Abbild der Fernansicht

`release.yml` baut in jedem Release-Lauf vor „Pack the release“ das Abbild
der Fernansicht mit `scripts/build-viewer-image.sh <version> out/viewer`.
Das Skript legt `titanos-viewer-<version>.tar.zst` und
`titanos-viewer-<version>.packages.txt` ab und gibt als **letzte Zeile** die
Image-ID (Config-Digest `sha256:…`, wie sie `docker image inspect` mit dem
klassischen Image-Speicher und `manifest.json` im Archiv nennen) aus; der
Workflow schreibt sie nach `titanos-viewer-<version>.image-id`.
Hat sich seit dem letzten Release mit Abbild nichts an `viewer/`,
`backend/cmd/titanos-viewer/`, `backend/internal/viewer/proto/`,
`scripts/build-viewer-image.sh` oder `backend/go.mod`/`go.sum` geändert,
übernimmt `scripts/viewer-image-reuse.py` stattdessen dessen Archiv: Manifest
mit gültiger Signatur, Archiv nach Größe und SHA-256 daraus, Paketliste aus dem
Lizenzarchiv (Prüfsumme in der signierten `SHA256SUMS`). Es erscheint unter dem
neuen Dateinamen mit derselben Image-ID, und die NAS laden es nicht erneut.
Exit-Code 3 heißt „neu bauen“, andere Fehler brechen den Lauf ab.
`make-release.sh` nimmt mit `VIEWER_DIR=out/viewer` diese Dateien, ohne
`VIEWER_DIR` ruft es das Skript selbst auf. Fehlt eine der drei Dateien oder
ist die ID keine `sha256:`-Kennung, bricht es ab. Das Abbild ist eine eigene
Release-Datei, nicht Teil des System-squashfs: Änderungen unter `viewer/`
und an `scripts/build-viewer-image.sh` erzwingen kein volles Release
(`scripts/release-kind.py`). `verify-release.py` prüft den Eintrag `viewer`,
wenn er da ist.

Format von `.packages.txt` (eine Zeile je installiertem Paket, durch Leerzeichen
oder Tabulatoren getrennt): `dpkg-query -W -f='${Package} ${Version} ${source:Package} ${source:Version}\n'`;
fehlt die Quellversion, gilt die Paketversion ohne binNMU-Endung (`+bN`).
`scripts/third-party-licenses.py viewer <datei>` liest es (auch zum Prüfen von Hand).

## Ab 0.63.0: Web-Apps

`release.yml` lädt vor „Pack the release“ mit `scripts/fetch-webapps.py out/webapps`
die Archive aus `webapps/catalog.json` von den GitHub-Releases der Apps, prüft
Größe, SHA-256 und Inhalt (Regeln wie titand, `docs/WEBAPPS.md`) und legt sie
unverändert als `titanos-webapp-<id>-<version>.zip` ab, dazu `webapps.json` und
die Lizenzdateien je App. `make-release.sh` nimmt sie mit `WEBAPPS_DIR=out/webapps`
(ohne `WEBAPPS_DIR` ruft es das Skript selbst auf; `WEBAPPS_SOURCE=<ordner>` nimmt
die Archive aus einem Ordner statt von GitHub, für Tests), listet sie im
Manifest unter `webapps`, schreibt ihre Quell-Links in `sources.txt` und ihre
Lizenzdateien ins Lizenzarchiv (`webapps/<id>-<version>/`). `verify-release.py`
verlangt genau die Apps des Katalogs mit dessen Version, SHA-256 und Größe.
Neue App-Versionen: Eintrag in `webapps/catalog.json` ändern (Version,
`upstreamAsset`, `sha256` und `size` des Upstream-Archivs, mit deren
`SHA256SUMS.txt` abgleichen) und nach `backend/internal/webapps/catalog.json`
kopieren; das ist kein volles Release (`scripts/release-kind.py`).

Nicht abgedeckt: Die QEMU-Smoke-Tests des Releases laden das Abbild nicht und
öffnen keine Fernansicht.

## Ab 0.72: zweiteilige Versionen

Angezeigt wird ab 0.72 nur noch `0.72`, `0.73` … (Inhaber 9.11.2026). Jedes Release erhöht die zweite Zahl, auch eine reine Korrektur: nach 0.72 kommt 0.73, nie 0.72.1.

- **Technisch bleibt die Version dreiteilig mit `.0`:** Tag `v0.72.0`, Manifest `"version": "0.72.0"`, Notizen `docs/releases/v0.72.0.md`, Dateinamen. titand bis 0.71.1 erkennt nur dreiteilige Versionen als Update; mit `0.72` käme das Update dort nie an.
- Oberfläche, Statusbericht, Handbuch und KI-Agent zeigen ohne `.0` (`osVersion` in `web/src/lib/format.ts`, `version.Display` in Go). Der Titel der Release-Notizen heißt „TitanOS 0.72 — …“.
- Ältere Versionen mit dritter Zahl (0.71.1) bleiben so, wie sie sind.
- **Autopilot:** Auch ein Sicherheits-Release des Autopiloten erhöht die zweite Zahl (nach 0.74 kommt 0.75; vergebene Nummern werden übersprungen). Sein Wächter (`scripts/autopilot.py guard`) verlangt ab 0.72: direkter Nachfolger eines veröffentlichten Releases, gleiche Hauptzahl, höhere zweite Zahl, dritte Zahl 0, nur `build/debian-security.txt` geändert (Entscheidung des Inhabers vom 10.11.2026, PLAN 76 F-13-17).

## Probelauf ohne Veröffentlichung

Änderungen an Build, Boot oder Release-Skripten lösen in Pull Requests den
Image-Probelauf aus (`release.yml`). Er baut und startet nur das System, ohne
Plugins (`WITHOUT_PLUGINS=1`); ein solches Paket lehnt `verify-release.py` ab,
außer mit `--without-plugins`. Die Plugins werden in
[ra5on/TitanOS-Plugins](https://github.com/ra5on/TitanOS-Plugins) gebaut,
gegen ein frisches TitanOS-Abbild in QEMU geprüft und dort signiert
(`docs/PLUGIN-REPO.md`). Ein Tag eines vollen Releases holt mit
`scripts/plugin-set.py fetch` die Sätze für den Kernel seines Abbilds; fehlt der
Kernel-Satz, stößt er ihn dort per `repository_dispatch` an (Repository-Secret
`PLUGINS_DISPATCH_TOKEN`, ein Fine-grained-Token nur für TitanOS-Plugins mit
„Contents: read and write“) und wartet, bis er veröffentlicht ist (Freigabe im
Plugin-Repository). `scripts/plugin-set.py import` legt die Plugins mit der
Version `<Satzversion>-titanos-<Basis>` nach `out/plugins`; das Release signiert
diese Manifeste neu, damit NAS bis 0.35 sie wie bisher annehmen. Actions ›
Release › „Run workflow“ nimmt einen vorhandenen Satz, sonst nur das System. Solche Läufe
veröffentlichen nichts (außer dem Tag) und Probeläufe haben keinen Zugriff auf
die Signiergeheimnisse.

Der Probelauf baut mit einem kurzlebigen Testschlüssel, prüft Signaturen,
packt alle Lizenznachweise und startet das tatsächliche ZIP zweimal in
einer frischen UEFI-QEMU-VM. Geprüft werden Ersteinrichtung, Besitzerkonto,
Versionsangabe, Weboberfläche, Lizenzübersicht, SSH aus, HTTPS-Zertifikat und
Persistenz über einen Neustart. Mit `--plugins` werden außerdem zwei neue virtuelle
Datenplatten verwendet: Plugin installieren, Signatur prüfen, sysext und den
Modulindex im RAM kontrollieren, fehlende GPU korrekt melden, nach Neustart
wieder laden und entfernen. Ein temporärer SSH-Testschlüssel ermöglicht diese
Prüfungen; SSH wird anschließend wieder ausgeschaltet. Eine echte GPU wird
damit nicht simuliert. Er verwendet ausschließlich neu angelegte
virtuelle Medien. Das Artifact `release-rehearsal` ist **kein Release** und
darf nicht verteilt oder auf einem produktiven NAS installiert werden.

Lokal:

```bash
make test test-release
make zip VERSION=0.1.0-beta.3
# Plugins im Checkout von TitanOS-Plugins bauen (dessen README), dann übernehmen:
python3 scripts/plugin-set.py import --base 0.1.0-beta.3 ../TitanOS-Plugins/out/sets/kernel ../TitanOS-Plugins/out/sets/common
scripts/make-release.sh 0.1.0-beta.3 beta docs/releases/v0.1.0-beta.3.md
python3 scripts/smoke-release.py out/release/titanos-0.1.0-beta.3.zip --plugins out/release
```

Für lokale Signaturtests einen separaten Wegwerf-Schlüssel verwenden.
`scripts/sign-release.sh out/release /pfad/zum/test.key` signiert die Dateien;
`python3 scripts/verify-release.py out/release /pfad/zu/test-public-keys`
prüft sie. Private Testschlüssel ebenfalls nie committen. Ein publizierter
Tag darf keinen `rehearsal.pub` enthalten und benötigt die beiden echten
öffentlichen Schlüssel.

## v0.1.0-beta.3 veröffentlichen

Erst nach grünem Probelauf, eingebauten öffentlichen Schlüsseln und Prüfung
des konkreten Release-Commits:

```bash
git tag --cleanup=verbatim -a v0.1.0-beta.3 -F docs/releases/v0.1.0-beta.3.md
git push origin v0.1.0-beta.3
```

`--cleanup=verbatim` erhält Markdown-Überschriften im Tag-Text; ohne diese
Option entfernt Git Zeilen, die mit `#` beginnen, als Kommentare. Bereits
veröffentlichte Tags nicht ersetzen. Eine reine Darstellungskorrektur der
Release-Seite kann über „Edit release“ erfolgen, ohne die signierten Assets
oder das Manifest zu ändern.

Der Tag löst einen neuen vollständigen Build samt QEMU-Test aus. Danach
wartet der Job `publish` auf die Freigabe im Environment `release`.
Erst dieser Job erhält den privaten Release-Schlüssel. Er signiert Manifest,
ZIP, Plugin-Manifeste und Prüfsummenliste, verifiziert die Signaturen und alle Dateiinhalte und
legt das GitHub-Pre-Release an. Ein normaler Tag ohne Vorabkennung wird im
Kanal Stable veröffentlicht. Die Release-Seite nennt das Apache-2.0-Datum
(tatsächliches Veröffentlichungsdatum plus zwei Jahre).

| Datei | Wofür |
|---|---|
| `titanos-<version>.zip`, `.zip.sha256`, `.zip.minisig` | neuer Stick, Prüfsumme und Herkunftsnachweis |
| `titanos-<version>-vmlinuz`, `-initrd.img`, `-filesystem.squashfs` | Dateien für den inaktiven System-Slot |
| `manifest.json`, `manifest.json.minisig` | signierte Update-Beschreibung mit Größe und SHA-256 jeder Slot-Datei |
| `nvidia.raw`, `nvidia.plugin.json`, `nvidia.plugin.json.minisig` | optionales NVIDIA-Plugin und signiertes Manifest für genau diesen Kernel |
| `intel-gpu-tools.raw`, `intel-gpu-tools.plugin.json`, `.minisig`, `intel-gpu-tools-licenses.tar.gz` | optionale Intel-Werkzeuge, Signatur, Originalhinweise und Debian-Quellpaketlinks |
| `coral-usb.raw`, `coral-usb.plugin.json`, `.minisig`, `coral-usb-licenses.tar.gz` | Coral-USB-Diagnose und Container-Zuordnung; ohne Google-Runtime |
| `nvidia-kernel-sources.tar.gz`, `nvidia-licenses.tar.gz` | passende offene Kernelquellen und originale NVIDIA-/Toolkit-Lizenztexte samt Paket-/Quellverzeichnis |
| `nvidia-build-sdk.tar.zst`, `nvidia-build-sdk-sources.txt` | exakte Kernelheader und abgeschlossene Bauumgebung mit Original-Lizenztexten und Debian-Quellpaketlinks; SHA-256 im signierten Manifest |
| `tailscale.raw`, `tailscale.plugin.json`, `.minisig`, `tailscale-licenses.tar.gz` | Tailscale, Subnet-/Exit-Node-Einstellungen und Original-Lizenztexte |
| `netbird.raw`, `netbird.plugin.json`, `.minisig`, `netbird-licenses.tar.gz` | NetBird-Client (BSD-3-Clause, ohne AGPL-Server), Original-Lizenz und Hinweise der Go-Module |
| `<treiber>.raw`, `<treiber>.plugin.json`, `.minisig`, `<treiber>-licenses.tar.gz`, `<treiber>-sources.tar.gz` für `wangxun-nic`, `intel-qat`, `it87-extended` | Treiber-Plugins aus dem Kernel-Satz des Plugin-Repositorys für genau den Kernel des Releases; die Quellarchive enthalten die kompilierten GPL-Quellen und das Baurezept, `titanos-<version>-sources.txt` nennt Debian-Quellpaket bzw. Upstream-Commit. `make-release.sh` verweigert ein Treiber-Plugin ohne Kernel, Quellenangabe oder Quellarchiv |
| `titanos-viewer-<version>.tar.zst` | Abbild der Fernansicht (`docker save`, zstd; Chromium, Xvfb, GStreamer mit x264/VA-API, PulseAudio, Agent `titanos-viewer`), seit 0.61.0 in **jedem** Release (App und System). Im signierten Manifest unter `viewer` mit `asset`, `sha256`, `size` und `imageId` (Config-Digest, `sha256:…`); titand lädt es bei der ersten Fernansicht, prüft Signatur, Größe, SHA-256 und nach `docker load` die Image-ID (`docs/FERNANSICHT.md`). Die Debian-Pakete des Abbilds stehen mit Quellpaketlinks in `titanos-<version>-sources.txt` („Viewer image“), die Liste zusätzlich unter `viewer/packages.txt` im Lizenzarchiv; die Copyright-Dateien liegen im Abbild unter `/usr/share/doc` |
| `SHA256SUMS`, `SHA256SUMS.minisig` | signierte Prüfsummen aller unveränderlichen Release-Dateien |
| `titanos-<version>-cli-linux-amd64` | statische öffentliche CLI, durch signierte SHA256SUMS abgedeckt |
| `titanos-<version>-sources.txt` | genaue Debian-/Go-/npm-Quellpakete und Versionen |
| `titanos-<version>-licenses.tar.gz` | Original-Lizenzhinweise und maschinenlesbares Komponentenverzeichnis |

Das Lizenzarchiv enthält auch Build-Abhängigkeiten des Frontends und
Schriftlizenzen. Bei zwei npm-Paketen (`is-reference` und `locate-character`)
liefert Upstream nur die MIT-Erklärung in README und Paketmetadaten; diese
werden unverändert beigelegt. Firmware-Pakete können ausschließlich
Binärdateien enthalten; der Debian-Quellpaketlink macht daraus keinen
verfügbaren Firmware-Quellcode. Der Hinweistext in „Über TitanOS“ wird aus
diesen tatsächlichen Komponenten erzeugt; die Repo-Datei ist die Übersicht.

Wer ohne privaten Schlüssel bei GitHub arbeiten möchte, lässt die Secrets
weg, lädt das Artifact `release` aus dem **Tag-Build** herunter und signiert
lokal mit `scripts/sign-release.sh`. Vor manuellem Upload immer
`scripts/verify-release.py` mit den eingebauten öffentlichen Schlüsseln
ausführen und das Apache-2.0-Datum in den Release-Text aufnehmen.

## Downloads prüfen

Den öffentlichen Release-Schlüssel aus einer bereits vertrauenswürdigen
Kopie beziehen (beispielsweise dem vorherigen TitanOS-Abbild oder dem
geprüften Projekt-Repository). In einem Ordner mit ZIP und Signatur:

```bash
minisign -V -p /pfad/titanos-release-1.pub -m titanos-0.1.0-beta.2.zip
sha256sum -c titanos-0.1.0-beta.2.zip.sha256
```

Für sämtliche Dateien zusätzlich `SHA256SUMS.minisig` prüfen und danach
`sha256sum -c SHA256SUMS` ausführen. Eine Prüfsumme ohne vertrauenswürdige
Signatur zeigt nur Übertragungsfehler, nicht die Herkunft.

## Abnahme vor der Freigabe

GitHubs `releases.atom` enthält auch Tags, deren Release-Dateien noch nicht
veröffentlicht sind. Die derzeitige Beta-Suche nimmt den ersten Tag und meldet
bei fehlendem Manifest einen Abruffehler. Deshalb den öffentlichen Update-Test
erst nach dem erfolgreichen Publish-Job starten. Ein Überspringen noch nicht
vollständig veröffentlichter Tags ist noch offen.

- Probelauf und reguläre Tests für den Release-Commit grün.
- Release- und Reserve-Schlüssel im Abbild, Geheimnisse sicher verwahrt.
- Echter FAT32-Stick: Kaltstart, Netzwerk, Besitzerkonto, Neustart und
  Persistenz. Mit separaten Testplatten RAID, Freigaben, Sicherung und
  Wiederherstellung prüfen. Diesen Hardwaretest übernimmt der Projektinhaber.
  Für `v0.1.0-beta.2` ist er noch offen; auch diese Beta
  wird auf ausdrücklichen Wunsch des Projektinhabers mit diesem Hinweis
  veröffentlicht. Daraus folgt keine Freigabe für produktive Daten.
- Nach Veröffentlichung den Download über das echte GitHub-Pre-Release im
  Beta-Kanal sowie Update und Rollback prüfen. Der lokale Release-Server
  und der ZIP-Boot-Test ersetzen diesen GitHub-Test nicht. Dieser Test muss für das jeweilige veröffentlichte Release erfolgen.
- CLA-Entwurf prüfen lassen und CLA Assistant vor Annahme externer Beiträge
  aktivieren; das ist getrennt von der technischen Release-Freigabe.

## Abnahme von v0.1.0-beta.2 am 14. Oktober 2026

Der [signierte Probelauf](https://github.com/ra5on/TitanOS/actions/runs/38564875559)
und der [offizielle Release-Lauf](https://github.com/ra5on/TitanOS/actions/runs/38567360790)
sind erfolgreich. Beide starteten das Installations-ZIP zweimal mit UEFI in
QEMU und prüften den Plugin-Lebenszyklus. Lokale Tests liefen zusätzlich mit
KVM und mit reiner CPU-Emulation.

Nach der Veröffentlichung wurden alle 17 öffentlichen Downloads erneut mit
`verify-release.py` geprüft. Eine frische lokale QEMU-VM startete aus dem
signaturgeprüften öffentlichen Beta-1-ZIP, fand Beta 2 über den normalen
GitHub-Beta-Kanal, lud die signierten Slot-Dateien und bestätigte den Start
aus Slot B. Anschließend wurde über die API auf Beta 1 in Slot A
zurückgewechselt und auch dieser Start bestätigt. Besitzerkonto und
HTTPS-Zertifikat blieben bei beiden Wechseln erhalten. Ein Spiegel oder
lokaler Release-Server wurde dabei nicht verwendet.

Die oben genannten Tests mit echtem Stick, HDDs und GPUs/NPUs stehen weiterhin aus.

## Schlüsselwechsel und Verlust

**Wechsel am 3. November 2026:** Die privaten Teile von `titanos-release-1`
und `titanos-recovery-1` waren nicht mehr auffindbar, auch nicht in der
Sicherung. `titanos-release-2` und `titanos-recovery-2` wurden neu erzeugt
und kommen mit 0.46.2 ins Abbild. Weil kein vertrauter alter Schlüssel mehr da
ist, ist 0.46.2 schon mit `titanos-release-2` signiert: Ein NAS vor 0.46.2 kann
es nicht prüfen und bekommt 0.46.2 einmalig über einen neu geschriebenen Stick
mit Rücksicherung der Konfiguration. Die alten öffentlichen Schlüssel werden in
einer späteren Version entfernt. Der neue Reserveschlüssel liegt offline beim
Inhaber; seine Sicherung an zwei Orten ist vom Inhaber zu bestätigen.

## Neue Regressionen im Entwicklungsstand

`scripts/smoke-storage.py <installations.zip> --plugins <plugin-ordner>` bootet
viermal mit ausschließlich neu erzeugten virtuellen Platten. Der Test erstellt
einen nativen Btrfs-Spiegel, prüft Daten/Metadaten im RAID1-Profil, einen
schreibgeschützten Snapshot und die Wiederherstellung ohne Änderung der
aktuellen Daten. Er bootet normal und mit einem fehlenden Mitglied, ersetzt es
über die API und prüft beim nächsten Start beide Mitglieder und die Daten.
Intel GPU Tools und Coral USB werden dabei installiert und über Neustarts
geprüft. Der Release-Workflow führt diesen Test zusätzlich zum bestehenden
ZIP-/NVIDIA-Test aus. Alle Testmedien und SSH-Schlüssel sind Wegwerf-Daten.

Die Go-Tests prüfen die signierte Plugin-Mitnahme, falsche Signaturen/Kernel/
Versionen, unveränderte Rückfall-Dateien bei Fehlern, getrennte Einstellungen
pro Release, GPU-Reservierungen und Wiederanlauf nach einem titand-Neustart.
Echte GPU-/TPU- und PCIe-Reset-Tests sind weiterhin nötig.

`scripts/smoke-firewall.py <installations.zip>` prüft die Firewall im echten
Abbild (zwei Starts, Usermode-Netz, keine Datenplatten): ab Werk aus,
Einschalten auf Probe und Bestätigen, verworfener Dienst aus einem
Netzwerk-Namespace des Gasts und über die Portweiterleitung, Zurücknehmen von
Hand und nach Ablauf, automatisches Sperren nach falschen Web-Anmeldungen samt
Freigeben, Tabelle nach Neustart, Ausschalten. Er läuft bisher lokal, nicht im
Release-Workflow; vor einem vollen Release mit Firewall-Änderungen ausführen.
