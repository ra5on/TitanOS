# TitanOS-Systemupdates und Wiederherstellung

Öffne **Einstellungen → Software-Update & Wiederherstellung**. Dort suchst du
nach Updates, installierst eine neue Ausgabe und verfolgst den anschließenden automatischen Neustart.
TitanOS verwendet ausschließlich den **Stable**-Kanal des
[eigenen Repositorys](https://github.com/ra5on/TitanOS/releases).

## Aktualisieren

TitanOS 2.0.4 unterstützt bestehende Installationen dieses Forks ab 2.0.1 mit
der Systemkennung `titan-rugix-amd64-v2`. Das vollständige Systemupdate enthält
Titan und die Debian-Systempakete. Signatur, Prüfsummen, Versionskennung und
Festplattenaufbau werden vor der Installation geprüft. Persönliche Dateien,
App-Daten und VM-Laufwerke liegen im gemeinsamen Datenbereich.

Das Update wird in den zweiten Systembereich geschrieben. Nach dem Neustart
wird es erst bestätigt, wenn die notwendigen Dienste, die HTTP-Schnittstelle
und der Datenbereich verfügbar sind. Ein fehlgeschlagener, unbestätigter Start
kehrt beim nächsten Start automatisch zum bestätigten System zurück.

Installationen mit anderem Namensraum oder Festplattenaufbau benötigen eine
Neuinstallation und vorherige externe Datensicherung. Ein direkter Formatwechsel
von Titan 3.x wird nicht angeboten.

## Vorherige Version auswählen

Als Administrator findest du den verfügbaren vorherigen Systemstand im
Dropdown der Update-Einstellungen. Wähle die Version, bestätige mit **Ja**. TitanOS startet anschließend automatisch neu. Der ausgewählte Stand wird vor der Ausführung erneut geprüft;
veraltete, beschädigte oder inkompatible Einträge werden nicht gestartet.

Nach einer Neuinstallation gibt es noch keine vorherige Version. Die beiden
Systembereiche sind keine unbegrenzte Versionshistorie: Ein weiteres Update
kann den früheren Stand ersetzen.

Bei der ersten Aktualisierung von 2.0.3 prüft TitanOS den vorhandenen vorherigen
Systembereich schreibgeschützt, gleicht seine Identität mit einem signierten
Release ab und speichert die nativen Slot-Fingerabdrücke in einem geschützten
Journal. Alte Releases enthalten noch keinen signierten Hash des ausgepackten
Systembereichs; diese Übernahme ersetzt daher keinen vollständigen
Integritätsnachweis einer zuvor bereits veränderten Installation. Nach
erfolgreicher Übernahme ist die Prüfung auch ohne GitHub-Verbindung möglich.

## Auswahl beim Start

Das Startmenü ist fünf Sekunden am angeschlossenen Bildschirm oder in der
VM-Konsole sichtbar. Ohne Eingabe startet die vorgesehene Version. Wenn ein
gültiger vorheriger Stand vorhanden ist, kannst du ihn mit den Pfeiltasten
auswählen und mit Enter starten. Während eines ausstehenden Update-Teststarts
wird keine zweite manuelle Auswahl angeboten, damit die automatische
Rückkehr unverändert funktioniert.

Das neue Image enthält dieses Menü. Eine kompatible ältere Installation
erhält es nach dem ersten erfolgreich bestätigten Start von 2.0.4 ebenfalls.

**Ein Systemrollback setzt Dateien, Container-Datenbanken und VM-Laufwerke
nicht zurück.** Änderungen ihrer Datenformate können mit einer älteren
Systemversion unverträglich sein. Sichere diese Daten getrennt; ein zweiter
Systembereich ersetzt kein Backup.

## Was der Release-Test nachweist

Der veröffentlichte Prüfbericht umfasst einen echten UEFI-Start, die
VM-Netzwerkprüfungen und den Wiederherstellungsablauf mit Versionswechsel,
Neustart, manueller Rückkehr, Startmenü-Auswahl und automatischer Rückkehr nach
einem beschädigten Teststart. Eine Beispieldatei im Datenbereich muss erhalten
bleiben. Für den Versionswechsel wird eine private, signierte ältere
Testversion aus demselben Quellcode gebaut. Dieser Test prüft die nativen
Systembereiche und öffentlichen APIs; er belegt keine vollständige Migration
echter 2.0.3-App-Daten oder sämtliche Hardwarekombinationen.

[Übersicht](../README.md) · [VM-Einrichtung](VM.md)
