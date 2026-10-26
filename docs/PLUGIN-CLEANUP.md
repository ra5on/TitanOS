# Alte Plugin-Dateien aufräumen

**Plugins › Alte Versionen** zeigt gespeicherte Release-Verzeichnisse auf dem
konfigurierten Plugin-Volume samt geschätzter logischer Dateigröße. Aktiv
benötigte Versionen bleiben geschützt: laufende System-/App-Basis, vollständige
Bootslots und Rückfallversionen, vorherige App-Version, vorbereitete Updates,
aktive/residente Plugin-Manifeste und aktuelle sysext-Ziele. Bei vollständigen Bootslots wird die tatsächliche Basis direkt und begrenzt aus dem squashfs gelesen; eine App-Versionsnummer im Slottitel reicht nicht als Nachweis. Ist die Basis nicht lesbar oder ein Update verändert gerade die Bootverweise, bleibt das Aufräumen gesperrt.

Nur ungeschützte Versionen lassen sich auswählen. „Auswahl prüfen“ zeigt die
Versionen und die geschätzte Summe; der Löschknopf bleibt bis zur ausdrücklichen
Bestätigung gesperrt. Der Server prüft aktuelle Schutzverweise und den
Fingerabdruck der Vorschau vor dem Entfernen erneut. Geänderte Dateien,
fehlende Volumes, fremd beschreibbare/private Pfade, Symlinks und laufende
Plugin-/Updatearbeiten verhindern das Aufräumen.

Entfernt werden ausschließlich ausgewählte alte Verzeichnisse unter
`.titanos/plugins/releases/`. Installierte Einstellungen, aktive Versionen,
NAS-Benutzerdaten und Bootslot-Dateien sind kein Löschziel. Die Größenanzeige
ist keine Garantie auf dieselbe Menge frei werdender Blöcke, etwa bei
Btrfs-Snapshots. Mehr als 100.000 Verzeichniseinträge je Release werden aus
Sicherheitsgründen nicht automatisch inventarisiert.
