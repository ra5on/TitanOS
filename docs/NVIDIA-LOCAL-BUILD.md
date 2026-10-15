# NVIDIA-Treiber auf dem NAS vorbereiten

Ab dem Entwicklungsstand nach Beta 5 bezieht TitanOS neue Treiber direkt aus
NVIDIAs öffentlichem Debian-Archiv. Es baut die offenen Kernelmodule für den
exakten laufenden TitanOS-Kernel auf dem NAS. Originale NVIDIA-Bibliotheken,
GSP-Firmware und Container-Werkzeuge bleiben unveränderte Herstellerdateien.
Ältere Archivversionen werden nicht zusätzlich angeboten.

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

Der alte CI-Workflow für fertig kompilierte Zusatztreiber bleibt für ältere
TitanOS-Versionen manuell verfügbar. Seine tägliche Ausführung ist abgeschaltet;
neue Treiber brauchen keinen neuen TitanOS-Release und keinen Treiberbuild durch
uns. Jeder OS-Release enthält weiterhin einen passenden Standardtreiber.

Die Release-Pipeline prüft den echten Buildpfad in der herunterladbaren Umgebung.
Unit-Tests prüfen Signaturen, falsche Kernel, manipulierte lokale Ergebnisse,
Auswahl/Rücknahme und die Boot-ID-Sperre. CUDA/NVENC und ein echter Treiberwechsel
auf der RTX 5060 Ti bleiben Hardwaretests; ein erfolgreicher Compilerlauf allein
bestätigt diese Funktionen nicht.
