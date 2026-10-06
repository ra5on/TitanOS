# Titan-Systemupdates und Rollback

Unter **Systemsteuerung → Updates & Rollback** ist **Stable** die einzige
angebotene Kanalwahl. Die Quelle ist `ra5on/TitanOS`. Release-Entwürfe werden
nicht als verfügbare NAS-Updates angeboten.

Aktuell enthält Systemimage **3.0.1** die Titan-Anwendung **3.0.0** und befindet
sich in der Freigabeprüfung. Ein erfolgreicher Build kann einen Release-Entwurf
bereitstellen; er ersetzt weder die dokumentierten Praxistests noch eine
ausdrückliche Stable-Freigabe. Den Stand zeigt [QA-3.0.0](QA-3.0.0.md).

## Ein Ablauf für Titan, Debian und Sicherheitskorrekturen

**Jetzt prüfen → Update vorbereiten → Neu starten** gilt für das gesamte NAS.
Es gibt eine gemeinsame automatische Update-Regel und eine Update-Seite.
Debian-Pakete und Sicherheitskorrekturen gelangen über signierte Systemstände
auf das NAS; die laufende Systempartition wird nicht mit einzelnen Paketupdates
verändert.

Ein Debian-Wartungsstand behält die Titan-Anwendung und ihre Laufzeithelfer auf
ihrem eingefrorenen Quellstand. Ein Funktionsrelease erneuert Titan gemeinsam
mit dem passenden Debian-Systembereich. Die Versionsinformationen unterscheiden
Titan-Quellstand, Systemrevision und gemessene Paketänderungen.

Das Update enthält unter anderem Debian, Kernel, Docker und VM-Komponenten.
Signatur, Prüfsummen und Kompatibilität werden vor der Aktivierung geprüft.
Titan beschreibt den inaktiven der beiden Systembereiche. Der laufende
schreibgeschützte Systemslot bleibt bis zum bestätigten Neustart aktiv.

1. **Jetzt prüfen** wählen und ein angebotenes Update vorbereiten.
2. Offene Arbeiten speichern und laufende VMs geordnet herunterfahren.
3. **Neu starten** wählen und die Ja/Nein-Abfrage bestätigen.
4. Nach erfolgreicher Startprüfung steht der vorherige lokale Systemstand im
   Rollback-Dropdown. Für die Rückkehr auswählen, bestätigen und neu starten.

Nach der ersten Installation gibt es noch keinen vorherigen Systemstand.
Automatische Suche oder Vorbereitung startet das NAS nicht automatisch neu.

## Automatische Suche und Wartungsfenster

Die Oberfläche bietet tägliche oder wöchentliche Prüfung. **Manuell** meldet
verfügbare Updates. **Automatisch im Wartungsfenster vorbereiten** lädt und prüft
den angebotenen Stand am gewählten Wochentag und zur gewählten Stunde der
NAS-Zeitzone. Der Neustart benötigt weiterhin deine Bestätigung.
Die Hintergrundprüfung läuft auch bei geschlossenem Browser.

Der GitHub-Workflow **Debian security and package maintenance** prüft täglich
um **03:17 Uhr, Europe/Berlin** offizielle Debian-13-Quellen einschließlich des
Sicherheitsarchivs. Die Runner-Warteschlange kann den tatsächlichen Beginn
verzögern. Der Workflow arbeitet auf einem Build-Runner und verändert keine
Pakete auf einem installierten NAS. Die Suche in deinem NAS folgt unabhängig
davon deiner gewählten Regel.

Grundlage der Pflege sind veröffentlichte signierte Systemmanifeste und
Paketinventare. Für Stable werden die zwei jüngsten veröffentlichten
Titan-Quellstände berücksichtigt; Debian-Revisionen desselben Quellstands gelten
als ein Stand. Ein Wartungsbuild verwendet den exakten bisherigen Titan-Commit
und einen aktuellen Systembuilder. Beide Quellstände werden signiert
festgehalten. Ohne geeignete veröffentlichte Ausgangsversion wird keine
Wartungsbasis erfunden.

Vertrauenswürdige neuere Debian-Pakete lösen einen Wartungsbuild aus.
Unveränderte Pakete erzeugen keine zusätzliche Veröffentlichung. Die vollständige
Paketliste und eine begrenzte Änderungsvorschau werden beigelegt. Vor einer
Veröffentlichung müssen Boot- und NAS-Laufzeittests sowie echte Update-,
Rollback- und Fehler-Rückfalltests bestehen. Wartungsbuilds verwenden den zuvor
veröffentlichten signierten Systembundle als geprüfte Testbasis. Die tägliche
Pflege kann erst beginnen, wenn ein geeigneter Titan-Systemstand veröffentlicht
ist. Wartung liefert Update-Bundles; nicht jede Paketkorrektur erzeugt ein neues
Installationsimage.

## Daten, Rückkehr und Grenzen

Benutzer, Rechte, NAS-Konfiguration, App-/VM-Daten und persönliche Dateien liegen
auf dem gemeinsamen Datenbereich. Unveränderte Systemkonfigurationen folgen dem
ausgewählten Systemstand; lokale Änderungen unter `/etc` bleiben persistent.
Ein Rollback stellt auch die früheren Debian-Paketversionen und damit den
vorherigen Sicherheitsstand wieder her.

Persistente Daten und Container-Datenbanken werden nicht rückwärts migriert.
Ein Rollback ist keine Datenwiederherstellung und schützt nicht gegen den Ausfall
des gemeinsamen Datenträgers. Die vorhandene Sicherung ermöglicht noch keine
vollständige Rekonstruktion von Konten, Speicher und Anwendungen auf einem
frischen Image. Geeignete unabhängige Datensicherungen bleiben erforderlich;
die konkreten Grenzen stehen in [QA-3.0.0](QA-3.0.0.md).

Bestätigt ein neuer Systemstand seinen Start nicht, kann der Bootloader beim
nächsten Neustart auf den vorherigen gesunden Stand zurückgehen. Ein vollständig
hängender Gast benötigt einen Reset durch den Host; der Ablauf ersetzt keinen
Hardware-Watchdog. Der gemeinsame EFI-/GRUB-Bereich und Änderungen am
Partitionslayout werden nicht durch normale Systemupdates erneuert. Solche
Änderungen benötigen derzeit ein neues Installationsimage.

## Status in der Oberfläche

Prüfung, Vorbereitung, Neustart und Rollback sind auf einer Seite zusammengeführt.
Ein Systemupdate meldet Angebotsprüfung, Konfigurationssicherung, Download,
Signaturprüfung, Schreiben des inaktiven Slots und Bereitschaft zum Neustart.
Download-Prozentwerte entstehen aus übertragenen Bytes und der signierten
Gesamtgröße. Andere Phasen zeigen Zustand und Dauer ohne erfundenen
Prozentfortschritt. Fehler und unterbrochene Vorgänge bleiben unterscheidbar.

[Systemimage und Testgrenzen](TITAN-IMAGE.md).
