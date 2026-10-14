# Eine Version veröffentlichen

TitanOS installiert nur Updates, deren Manifest von einem Schlüssel aus
seinem Abbild signiert ist. Die Release-Pipeline signiert zusätzlich das
Installations-ZIP, das NVIDIA-Plugin-Manifest und `SHA256SUMS` für sämtliche Release-Dateien.

## Einmalig: Schlüssel und GitHub einrichten

Das GitHub-Environment `release` verlangt eine Freigabe von `ra5on` und
akzeptiert nur Tags `v*`. Private Sicherheitsmeldungen sind aktiviert.
Diese Einstellungen gehören zum Repository und werden nicht durch die
Workflow-Datei allein eingerichtet.

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

## Probelauf ohne Veröffentlichung

Änderungen an Build, Boot oder Release-Skripten lösen in Pull Requests den
Image-Probelauf aus. Zusätzlich lässt er sich unter Actions › Release mit
„Run workflow“ und einer Version starten. Solche Läufe veröffentlichen
nichts und haben keinen Zugriff auf die Signiergeheimnisse.

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
make zip VERSION=0.1.0-beta.2
sudo env PATH="$PATH" scripts/make-nvidia-plugin.sh 0.1.0-beta.2
scripts/make-release.sh 0.1.0-beta.2 beta docs/releases/v0.1.0-beta.2.md
python3 scripts/smoke-release.py out/release/titanos-0.1.0-beta.2.zip --plugins out/release
```

Für lokale Signaturtests einen separaten Wegwerf-Schlüssel verwenden.
`scripts/sign-release.sh out/release /pfad/zum/test.key` signiert die Dateien;
`python3 scripts/verify-release.py out/release /pfad/zu/test-public-keys`
prüft sie. Private Testschlüssel ebenfalls nie committen. Ein publizierter
Tag darf keinen `rehearsal.pub` enthalten und benötigt die beiden echten
öffentlichen Schlüssel.

## v0.1.0-beta.2 veröffentlichen

Erst nach grünem Probelauf, eingebauten öffentlichen Schlüsseln und Prüfung
des konkreten Release-Commits:

```bash
git tag --cleanup=verbatim -a v0.1.0-beta.2 -F docs/releases/v0.1.0-beta.2.md
git push origin v0.1.0-beta.2
```

`--cleanup=verbatim` erhält Markdown-Überschriften im Tag-Text; ohne diese
Option entfernt Git Zeilen, die mit `#` beginnen, als Kommentare. Bereits
veröffentlichte Tags nicht ersetzen. Eine reine Darstellungskorrektur der
Release-Seite kann über „Edit release“ erfolgen, ohne die signierten Assets
oder das Manifest zu ändern.

Der Tag löst einen neuen vollständigen Build samt QEMU-Test aus. Danach
wartet der Job `publish` auf die Freigabe im Environment `release`.
Erst dieser Job erhält den privaten Release-Schlüssel. Er signiert Manifest,
ZIP, Plugin-Manifest und Prüfsummenliste, verifiziert die Signaturen und alle Dateiinhalte und
legt das GitHub-Pre-Release an. Ein normaler Tag ohne Vorabkennung wird im
Kanal Stable veröffentlicht. Die Release-Seite nennt das Apache-2.0-Datum
(tatsächliches Veröffentlichungsdatum plus zwei Jahre).

| Datei | Wofür |
|---|---|
| `titanos-<version>.zip`, `.zip.sha256`, `.zip.minisig` | neuer Stick, Prüfsumme und Herkunftsnachweis |
| `titanos-<version>-vmlinuz`, `-initrd.img`, `-filesystem.squashfs` | Dateien für den inaktiven System-Slot |
| `manifest.json`, `manifest.json.minisig` | signierte Update-Beschreibung mit Größe und SHA-256 jeder Slot-Datei |
| `nvidia.raw`, `nvidia.plugin.json`, `nvidia.plugin.json.minisig` | optionales NVIDIA-Plugin und signiertes Manifest für genau diesen Kernel |
| `nvidia-kernel-sources.tar.gz`, `nvidia-licenses.tar.gz` | passende offene Kernelquellen und originale NVIDIA-/Toolkit-Lizenztexte samt Paket-/Quellverzeichnis |
| `SHA256SUMS`, `SHA256SUMS.minisig` | signierte Prüfsummen aller unveränderlichen Release-Dateien |
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
