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
  seit dem Assistenten entfernt) und die Projektdateien (README, AGENTS,
  CLAUDE, …) geändert, wird es
  ein App-Release. Jede andere Änderung, etwa unter
  `build/`, `boot/`, `scripts/` (außer Browserprüfungen), `.github/` oder am
  `Makefile`, ergibt ein volles Release.
- Eine Tag-Nachricht mit `[system]` erzwingt ein volles Release.

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

**Debian-Sicherheitsupdates:** App-Releases behalten die Debian-Pakete ihrer
Basis. `debian-security.yml` vergleicht jeden Montag die Pakete des neuesten
Releases mit trixie-security und öffnet bei neueren Versionen einen Pull
Request, der `build/debian-security.txt` anlegt oder ändert. Nach dem Merge
wird der nächste Tag ein volles Release. In den Repository-Einstellungen muss
dafür „Allow GitHub Actions to create and approve pull requests“ an sein.

## Probelauf ohne Veröffentlichung

Änderungen an Build, Boot oder Release-Skripten lösen in Pull Requests den
Image-Probelauf aus (`release.yml`). Er baut und startet nur das System, ohne
Plugins (`WITHOUT_PLUGINS=1`); ein solches Paket lehnt `verify-release.py` ab,
außer mit `--without-plugins`. Änderungen an Plugins, ihren Skripten und der
Plugin-Seite prüft stattdessen `plugins.yml`: Abbild bauen, alle offiziellen
Plugins dagegen bauen und mit `--plugins` in QEMU starten. Ein Tag und
Actions › Release › „Run workflow“ bauen und prüfen immer beides. Solche Läufe
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
sudo env PATH="$PATH" scripts/make-nvidia-plugin.sh 0.1.0-beta.3
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

Einen neuen öffentlichen Schlüssel zuerst per Update verteilen, noch mit
dem alten Schlüssel signiert. Erst danach das GitHub-Secret wechseln.
Den alten Schlüssel erst in einer späteren Version entfernen und mit
`MIN_VERSION` sicherstellen, dass ältere Installationen den Zwischenschritt
einhalten.

Bei Verlust oder Diebstahl des normalen Schlüssels kann der bereits im
Abbild enthaltene Offline-Reserve-Schlüssel ein Ersatz-Update signieren,
das den kompromittierten Schlüssel entfernt. GitHub-Secrets und
Repository-Zugriff ebenfalls prüfen. Ohne irgendeinen verbliebenen
vertrauenswürdigen privaten Schlüssel müssen Nutzer einen neuen Stick
erstellen und ihre Konfiguration wiederherstellen.

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
