# Textdateien im Dateimanager bearbeiten

Unter **Dateien** eine einzelne Datei markieren und **Bearbeiten** wählen.
Der Editor unterstützt UTF-8-Text bis 512 KiB: unter anderem JSON, YAML/YML,
TXT, Markdown, INI/CONF/CFG, TOML, XML, CSV/TSV, Logs und Skripte. Dockerfile,
Makefile und `.env` sind ebenfalls möglich, sofern der betreffende Bereich sie
nicht verbirgt. Office-Dokumente, PDFs, Bilder und Binärdateien sind kein Texteditor-Format.

Auf Freigaben gelten die Schreibrechte des angemeldeten Kontos. Ganze Volumes,
USB-Geräte und die sichtbare Stick-Konfiguration bleiben auf Besitzer und
Administratoren begrenzt. Verwaltete VM-Dateien sowie verborgene Schlüssel und
Kontodateien lassen sich damit nicht verändern.

**Speichern** oder **Strg+S / ⌘S** schreibt die Datei über eine temporäre Datei.
Die unmittelbar vorherige Fassung bleibt daneben als `<Dateiname>.prev`.
Ungültiges JSON wird abgelehnt. Eine bereits seit dem Öffnen geänderte Datei
wird nicht still überschrieben; der Entwurf bleibt im Editor. Vor dem Schließen
mit ungespeicherten Änderungen erfolgt eine Rückfrage. Die `.prev`-Datei ersetzt
kein versioniertes Backup.

Änderungen an der Stick-Konfiguration müssen weiterhin zum TitanOS-Schema passen.
Ein späteres Speichern über die normale Einstellungsseite kann manuelle Änderungen
ersetzen; viele Einstellungen werden erst beim Start neu eingelesen.

## USB, interne Datenträger und exFAT

Beta 7 prüfte Upload-Ziele nur gegen interne Volumes. Dadurch konnte eine korrekt
montierte USB-SSD als nicht verfügbar erscheinen. Außerdem verwendete der
Upload-Abschluss Hardlinks, die FAT/exFAT nicht unterstützen. Beide Fehler werden
im Folgestand korrigiert: USB-Ziele werden gegen ihre tatsächliche Einbindung
geprüft und neue Dateien atomar ohne Überschreiben vorhandener Namen umbenannt.

Nicht verwaltete SATA-/SAS-/NVMe-Dateisysteme erscheinen ebenso wie USB unter
**Dateien › Angeschlossene Laufwerke** und lassen sich dort ausdrücklich einbinden.
NTFS nutzt bei einem Fehler von ntfs3 einen ausschließlich lesenden ntfs-3g-
Ausweichweg. Der sichtbare Hinweis und die API sperren dann alle Schreibaktionen.
Der Bootstick wird hier samt seinen weiteren Partitionen ausgenommen.

Bei FAT, exFAT und NTFS werden Dateien mit auf root beschränkten Einbinderechten
bereitgestellt, damit Upload-Metadaten dieselben Schutzprüfungen erfüllen wie auf
Linux-Dateisystemen. Der Dateimanager vermittelt den Zugriff für angemeldete
Administratoren. Bestehende Dateisysteme werden nicht formatiert oder repariert.

## Lange Namen und Windows-Netzwerkfreigaben

Web-Uploads und Kopien verwenden temporäre Namen unabhängig vom Dateinamen.
Damit benötigen gültige Namen am Dateisystemlimit keinen zusätzlichen Platz
für interne Suffixe. Unterbrochene Uploads mit dem älteren Namensschema können
weiterhin fortgesetzt werden.

Windows-Explorer/SMB benutzt diesen Web-Upload-Pfad nicht. Bei „Dateiname für
Zielordner zu lang“ müssen Dateinamen und vollständiger Zielpfad getrennt geprüft
werden. Eine Verbindung als Netzlaufwerk (z. B. `Z:`) oder ein kürzerer Zielpfad
kann die Windows-Pfadgrenze vermeiden. Eine Grenze einzelner Namen hebt das
nicht auf: ext4 erlaubt beispielsweise höchstens 255 Bytes je Namen; Umlaute
und andere Unicode-Zeichen können mehrere Bytes belegen. TitanOS kürzt oder
benennt vorhandene Dateien nicht automatisch um.

Quellen: [Windows-Pfadlängen](https://learn.microsoft.com/en-us/windows/win32/fileio/maximum-file-path-limitation),
[ext4-Verzeichniseinträge](https://www.kernel.org/doc/html/latest/filesystems/ext4/directory.html).
Der Release-Test prüft einen 255-Byte-Dateinamen und einen verschachtelten
SMB-Pfad über 260 Zeichen mit `smbclient`. Das ist kein Test des Windows-Explorers.
