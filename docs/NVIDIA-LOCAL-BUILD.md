# NVIDIA-Treiber auf dem NAS vorbereiten

Ab dem Entwicklungsstand nach Beta 5 bezieht TitanOS neue Treiber direkt aus
NVIDIAs öffentlichem Debian-Archiv. Es baut die offenen Kernelmodule für den
exakten laufenden TitanOS-Kernel auf dem NAS. Originale NVIDIA-Bibliotheken,
GSP-Firmware und Container-Werkzeuge bleiben unveränderte Herstellerdateien.
Seit 0.47.0 ist jede vollständige Version des Archivs wählbar (Abschnitt
„Version wählen“).

1. **Neue NVIDIA-Version prüfen** authentifiziert den Paketindex mit dem festen
   NVIDIA-GPG-Schlüssel und lädt einen vollständigen Paketstand auf das
   Plugin-Daten-Volume. Standardmäßig geschieht das täglich; abschaltbar.
2. **Treiber vorbereiten** startet ausdrücklich den lokalen Build. Compiler,
   Kernelheader, Quellen und Arbeitsdateien liegen auf dem Daten-Volume. Mindestens
   5 GiB freier Platz sind nötig; der Build ist auf zwei CPU-Kerne und 2 GiB RAM
   begrenzt. Download und Status allein starten keinen Compilerlauf.
3. Der fertige Eintrag erscheint als **Auf diesem NAS gebaut**. Mit
   **Beim nächsten Neustart verwenden** wird er für den nächsten NAS-Neustart
   ausgewählt. Ein Neustart von titand aktiviert ihn nicht. Geladene Module,
   laufende GPU-VMs und Container bleiben bis dahin unverändert.

**Auswahl zurücknehmen** verwirft die geplante Aktivierung. **Mit TitanOS
ausgelieferter Treiber** bleibt als Rückkehrmöglichkeit erhalten. Während ein
Treiberwechsel auf den Neustart wartet, sind andere Plugin-Mutationen und die
Vorbereitung eines Systemupdates gesperrt. Alte Dateien werden nicht gelöscht.

## Version wählen

Die Prüfung schreibt alle Treiberversionen, deren Pakete im Archiv
vollständig vorliegen, in `versions.json`. Unter Plugins › NVIDIA stehen sie
in einer Auswahl neben dem mit TitanOS ausgelieferten Treiber. **Bauen und beim
nächsten Neustart verwenden** lädt die gewählte Version (`TITANOS_NVIDIA_DRIVER`
für die Bauumgebung), baut sie und wählt sie für den nächsten Neustart, in
einem Auftrag. Die Wahl steht in `/config/plugins/settings.json`
(`nvidiaDriver`).

Nach einem Kernel- oder Systemupdate passt der lokale Bau nicht mehr; der neue
Satz bringt den ausgelieferten Treiber. titand baut die gewählte Version dann
von selbst für den neuen Kernel, einmal je Release, Kernel und Version, und wählt
sie für den nächsten Neustart. Schlägt der Bau fehl, läuft weiter der
ausgelieferte Treiber, die Seite zeigt den Fehler und eine Meldung
(`nvidia_build_failed`) geht raus. Ältere Treiberreihen als die ausgelieferte
bauen mit neuen Kerneln oft nicht; die Auswahl weist darauf hin.

Bauumgebungen aus Kernel-Sätzen vor dieser Änderung liefern keine Liste. Dann
zeigt die Seite die bisherige Bedienung (neueste Version prüfen, vorbereiten,
auswählen).

## Herkunft und Bauumgebung

Jedes neue OS-Release enthält `nvidia-build-sdk.tar.zst`: eine separate Debian-
Bauumgebung einschließlich exakt passender Header. Kernel, Version, SHA-256 und
Größe stehen im signierten OS-Manifest. Sie wird nur für das laufende Release
akzeptiert und liegt unter `.titanos/plugins/nvidia-build/<OS-Version>/`.
Sie wird nicht in den RAM-Systembereich installiert. Ihre Debian-Lizenzen und
Quellpaketlinks liegen im Archiv und in `nvidia-build-sdk-sources.txt`.

Die Bauumgebung authentifiziert NVIDIAs `InRelease` mit dem fest hinterlegten
Schlüssel, prüft den Paketindex und danach jedes heruntergeladene Paket. Die
Pakete einer Treiber-/Toolkitgruppe müssen zusammenpassen; automatische
Downgrades und fremde URLs sind ausgeschlossen. Der Compiler läuft in einem
abgeschotteten systemd-Dienst ohne Netzwerk, zusätzliche Capabilities oder
Schreibzugriff außerhalb seines Arbeitsverzeichnisses. Er lädt keine Module.

Nach erfolgreichem Build prüft titand Image, Zielkernel und Manifest. Eine mit
einem privaten lokalen Schlüssel authentifizierte Herkunftsdatei bindet das
Ergebnis an dieses NAS. Sie ersetzt keine offizielle TitanOS-Signatur und wird
in der Oberfläche getrennt gekennzeichnet. Der Schlüssel liegt ausschließlich
in `/config/plugins/nvidia/build-key.json`, wird nur beim ersten ausdrücklich
gestarteten Build erzeugt und ist nicht Bestandteil von Diagnosepaketen.

Ergebnisse mit Quellen/Lizenzen liegen unter
`.titanos/plugins/driver-updates/<OS-Version>/<SHA256>/`. Eine Auswahl erzeugt
einen vollständigen Plugin-Satz per Hardlinks unter `releases/<Version>/.sets/`.
Die Originaldateien für den OS-Rückfall bleiben erhalten. Der lokale Build ist
an den exakten OS-Kernel gebunden; ein Systemupdate lädt wieder das dazu passende
signierte Standardplugin, eine spätere lokale Vorbereitung ist erneut möglich.

Fertig kompilierte Zusatztreiber baut der manuelle Workflow `nvidia-driver.yml`
im [Plugin-Repository](https://github.com/ra5on/TitanOS-Plugins) für dessen
Kernel-Sätze; TitanOS bietet sie aus dem Satz seines Kernels an (ältere Versionen
bis 0.35 lesen nur Zusatztreiber an ihrem eigenen Release, dort kommen keine neuen
mehr hinzu). Neue Treiber brauchen keinen neuen TitanOS-Release. Jeder
Kernel-Satz und damit jedes volle Release enthält einen passenden Standardtreiber.
Die Bauumgebung (`nvidia-build-sdk.tar.zst`) stammt aus dem Kernel-Satz und passt
zu jedem Release mit diesem Kernel; titand übergibt dem Bau die eigene Version
(`TITANOS_VERSION`).

Die Release-Pipeline prüft den echten Buildpfad in der herunterladbaren Umgebung.
Unit-Tests prüfen Signaturen, falsche Kernel, manipulierte lokale Ergebnisse,
Auswahl/Rücknahme und die Boot-ID-Sperre. CUDA/NVENC und ein echter Treiberwechsel
auf der RTX 5060 Ti bleiben Hardwaretests; ein erfolgreicher Compilerlauf allein
bestätigt diese Funktionen nicht.
