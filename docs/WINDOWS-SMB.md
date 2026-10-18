# Windows: „Der Dateiname ist für den Zielordner zu lang“

Auch ein kurzer Dateiname kann diese Meldung auslösen: Windows zählt je nach
Kopierprogramm den gesamten Pfad einschließlich Server, Freigabe und aller
Unterordner. Die TitanOS-SMB-Prüfung überträgt einen verschachtelten Pfad über
260 Zeichen und einen Dateinamen mit 255 ASCII-Zeichen. Das beweist nicht, dass
jedes Windows-Programm dieselben Pfade verarbeiten kann.

Eine kürzere Zielstruktur oder ein Netzlaufwerksbuchstabe kann den Pfad verkürzen.
Für umfangreiche Ordnerbäume unterstützt das mit Windows gelieferte Robocopy
lange Pfade. Beispiel für die **Eingabeaufforderung**; beide Pfade ersetzen:

```bat
robocopy "C:\Quellordner" "\\titanos\Freigabe\Zielordner" /E /Z /R:2 /W:2 /XJ
```

Das kopiert den Inhalt des Quellordners einschließlich leerer Unterordner.
Geänderte Dateien im Ziel können überschrieben werden. Zusätzliche Zieldateien
werden behalten. `/Z` erlaubt das Fortsetzen unterbrochener Dateien, `/R:2 /W:2`
begrenzt Wiederholungen und `/XJ` überspringt NTFS-Verzeichnisverknüpfungen.
`/256` darf nicht ergänzt werden: Diese Option schaltet lange Pfade ab.
Mit zusätzlichem `/L` lässt sich die Auswahl zuerst nur anzeigen.

Wenn auch Robocopy fehlschlägt, den genauen Fehlercode und einen betroffenen
Quell-/Zielpfad festhalten. Neben der gesamten Pfadlänge gelten Grenzen je
Datei-/Ordnername; Nicht-ASCII-Zeichen können auf dem Ziel mehrere UTF-8-Bytes
belegen. TitanOS kürzt Namen nicht automatisch, weil dadurch Kollisionen und
überschriebene Dateien entstehen könnten.

Die Windows-Einstellung für lange Win32-Pfade allein ist keine Garantie: Laut
Microsoft muss auch die Anwendung dafür ausgelegt sein. Ein TitanOS-Update
ändert diese Eigenschaft des Windows-Kopierprogramms nicht.

Quellen: [Microsoft: maximale Pfadlänge](https://learn.microsoft.com/en-us/windows/win32/fileio/maximum-file-path-limitation),
[Microsoft: Robocopy](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/robocopy).
