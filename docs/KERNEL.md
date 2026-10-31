# Kernel-Wahl „Stabil / Neu“

Stand 31.10.2026 (Entscheidung des Projektinhabers zu Punkt 5a). Unter **Einstellungen › Updates › Kernel** wählt man:

- **Stabil** (Standard): der Kernel von Debian 13 (`linux-image-amd64` aus trixie).
- **Neu**: der Kernel aus `trixie-backports` (6.16 oder neuer), samt neuerer Firmware-Pakete von dort. Gedacht für sehr neue Hardware, z. B. RTL8127, Intel E610, neueste Intel/AMD-Grafik oder NPU-Funktionen von Arrow Lake.

Die Wahl steht im optionalen Feld `kernel` von `/config/system/update.json` (`"new"` oder fehlend). Ältere Versionen ignorieren es und installieren die stabile Variante.

## Wie ein Release beide Kernel ausliefert

- Gebaut wird zweimal: `scripts/make-zip.sh` (stabil, `build/mkosi.output`, Stick und ZIP) und `scripts/make-zip.sh --kernel-new` (mkosi-Profil `kernel-new`, `build/mkosi.output-new`, Slot-Dateien in `out/kernel-new/`). Das Profil fügt `trixie-backports` als Paketquelle hinzu und hebt Kernel (`linux-image-*`, `linux-kbuild-*`, `linux-base`) und `firmware-*` per apt-Pin auf die Backports-Fassung. Das Abbild trägt `/usr/share/titanos/kernel-variant` mit `new`. Sonst sind Pakete, titand und Basis-Version gleich.
- Das Release enthält zusätzlich `titanos-<version>-new-{vmlinuz,initrd.img,filesystem.squashfs}`. Das signierte Manifest beschreibt sie im optionalen Feld `variants.new` (`kernel`, `kernelPackage`, `files`). Ältere TitanOS-Versionen kennen das Feld nicht und installieren wie bisher `files`.
- Das ZIP enthält nur die stabile Variante. Wer schon zum ersten Start den neuen Kernel braucht (z. B. weil nur eine RTL8127-Karte eingebaut ist), kopiert die drei `-new-`-Dateien ohne das Präfix nach `titanos/a/` des Sticks (`titanos-<version>-new-vmlinuz` → `titanos/a/vmlinuz` usw.) und stellt danach unter Updates „Neu“ ein.
- App-Releases (`repack-release.py`) packen beide Abbilder der Basis mit dem neuen titand neu. Kernel und initrd bleiben bitgleich.
- Lizenzen: `titanos-<version>-sources.txt` listet zusätzlich die Debian-Pakete, die nur im Abbild mit neuem Kernel stecken. Das Lizenzarchiv enthält deren Inventar unter `kernel-new/`.

## Was das NAS tut

- `titand` liest die eigene Variante aus dem Abbild und den Kernel jedes Slots aus dem Kopf von `vmlinuz` (`Slot.kernel`, `Slot.variant`, angezeigt unter „Die beiden Systeme auf dem Stick“).
- Ein neueres Release installiert die gewählte Variante. Fehlt sie im Release, wird es nicht angeboten (`kernelMissing`), statt still auf den anderen Kernel zu wechseln. Die Seite sagt das und nennt den Ausweg.
- Ist die laufende Version dieselbe, aber die Wahl eine andere, erscheint „TitanOS <version> · mit Kernel Neu“ mit dem Knopf „Kernel wechseln“. Das ist ein normales Systemupdate in den anderen Slot, ohne Update-Meldung. Es startet dreimal auf Probe; startet der Kernel nicht, kehrt der Stick zum vorigen System zurück. Danach ist der Rückwechsel wie gewohnt möglich.
- App-Updates (nur titand) gibt es nur, wenn die laufende Variante der Wahl entspricht. Sonst wird das Release als Systemupdate mit der gewählten Variante installiert.

## Treiber-Plugins je Kernel

- Kernel-Plugins (NVIDIA, `nvidia-legacy`, `wangxun-nic`, `intel-qat`, `it87-extended`, `r8125`, `r8126`) werden für jeden Kernel eigens gebaut (doppelte Bauzeit, vom Projektinhaber akzeptiert). Die Kernel-Sätze im Plugin-Repository heißen ohnehin nach dem Kernelpaket (`kernel-<kernelPackage>-<n>`). Der neue Kernel bekommt also einen eigenen Satz.
- `release.yml` holt bzw. bestellt die Sätze für beide Kernel (`repository_dispatch` mit `kernelVariant=new`). Ins Release kommen nur die Plugins des stabilen Kernels. Das NAS mit Kernel „Neu“ lädt seine Plugins aus dem Plugin-Repository (Katalog und `PrepareUpdate` nach `kernelPackage`). Fehlt der Satz, scheitert die Vorbereitung eines Updates mit aktivierten Kernel-Plugins. Das alte System bleibt dann unverändert.
- Auf dem Daten-Volume liegen die Plugins einer Version mit neuem Kernel getrennt unter `.titanos/plugins/releases/<version>+new/`, die Einstellungen unter `/config/plugins/releases/<version>+new.json`. So können beide Varianten derselben Version in den beiden Slots liegen und jeweils Rückfall des anderen sein. „Alte Plugin-Sätze entfernen“ zeigt sie unter ihrem Namen und schützt sie zusammen mit ihrer Version.
- Der lokale NVIDIA-Bau („Treiber vorbereiten“) braucht die Bauumgebung des laufenden Kernels aus dem Release. Für den neuen Kernel liefert das Release keine. Dort ist der lokale Bau deshalb nicht verfügbar; das mitgelieferte NVIDIA-Plugin des Satzes funktioniert.

## Geprüft am 31.10.2026 und Grenzen

- Go-Tests: Kernelwechsel bei gleicher Version (Dateien der Variante, Plugin-Vorbereitung mit Variante und Kernelpaket, keine Release-Plugins, A/B-Einträge), neueres Release mit und ohne Variante, Manifestprüfung, Kernel aus dem `vmlinuz`-Kopf, getrennte Plugin-Sätze und Aufräumschutz.
- `make test-release`: Release mit Variante packen, signieren und prüfen (Manipulation erkannt), falsches Abbild abgelehnt, App-Release übernimmt die Variante.
- UI-Screenshots mit isolierten Testdaten.
- `mkosi summary` mit dem Profil `kernel-new` ist geprüft (Sandbox-Baum, Ausgabeordner). **Nicht gelaufen:** der echte Image-Build mit Backports (braucht root und Netz), ein Boot des neuen Kernels, Plugin-Sätze für den neuen Kernel, die CI-Schritte und die Behandlung von `kernelVariant` im Plugin-Repository (Patch liegt bei). Ob die Firmware-Pakete in trixie-backports neuer sind, entscheidet apt beim Bau. Ohne Backports-Fassung bleibt die aus trixie.

## Stand der Treiber-Plugins für den neuen Kernel (31.10.2026)

Das Plugin-Repository baut Sätze für den neuen Kernel erst, wenn es `kernelVariant` versteht. Die Änderungen dafür liegen als Patch vor und werden vom Projektinhaber im Plugin-Repository eingespielt (dort sind auch die Quellen von `r8125`/`r8126` festzulegen und `nvidia-legacy/packages.lock.json` zu erzeugen). Bis dahin holt der Release-Workflow für den neuen Kernel nur einen schon vorhandenen Satz und warnt sonst; ein NAS mit Kernel „Neu“ kann dann keine Kernel-Plugins (NVIDIA, Netzwerktreiber) nutzen. Der Standard-Kernel ist davon nicht betroffen.
