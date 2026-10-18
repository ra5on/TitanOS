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

## USB und exFAT

Beta 7 prüfte Upload-Ziele nur gegen interne Volumes. Dadurch konnte eine korrekt
montierte USB-SSD als nicht verfügbar erscheinen. Außerdem verwendete der
Upload-Abschluss Hardlinks, die FAT/exFAT nicht unterstützen. Beide Fehler werden
im Folgestand korrigiert: USB-Ziele werden gegen ihre tatsächliche Einbindung
geprüft und neue Dateien atomar ohne Überschreiben vorhandener Namen umbenannt.

Bei FAT, exFAT und NTFS werden Dateien mit auf root beschränkten Einbinderechten
bereitgestellt, damit Upload-Metadaten dieselben Schutzprüfungen erfüllen wie auf
Linux-Dateisystemen. Der Dateimanager vermittelt den Zugriff für angemeldete
Administratoren. Bestehende Dateisysteme werden nicht formatiert oder repariert.
