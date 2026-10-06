# Netzwerk für virtuelle Maschinen

Ab TitanOS 2.0.3 findest du beim Erstellen einer eigenen VM und in den
Einstellungen einer VM das Dropdown **Netzwerk**.

| Auswahl | Verbindung der VM |
| --- | --- |
| NAT | Privates Netz `10.203.0.x` mit Internetzugriff über das NAS. Dienste erreichst du aus dem LAN über eingerichtete Portweiterleitungen. |
| Host-only | Privates Netz `10.204.0.x` für den Zugriff vom NAS. Die VM erhält keinen Internet- oder LAN-Zugang über dieses Netz. |
| Heimnetz (Bridge) | Direkte Verbindung über einen kabelgebundenen Netzwerkanschluss des NAS. Eine vorhandene Linux-Bridge wird verwendet; fehlt sie, kann TitanOS sie automatisch einrichten. Die IP-Adresse der VM kommt aus deinem LAN, beispielsweise per DHCP vom Router. |

**NAT ist der Standard.** Betriebssysteme aus dem VM-Katalog werden zunächst
mit NAT installiert, damit Downloads und die automatische Einrichtung
funktionieren. Nach Abschluss der Installation kannst du das Netzwerk wechseln.
Bei einer eigenen VM aus einem hochgeladenen Image oder einer ISO kannst du die
Auswahl bereits beim Erstellen treffen.

## Netzwerk einer bestehenden VM ändern

1. Schließe die Installation und die automatische Ersteinrichtung der VM ab.
2. Fahre die VM vollständig herunter. Eine pausierte VM reicht nicht aus.
3. Öffne ihre Einstellungen und wähle unter **Netzwerk** den gewünschten Modus.
4. Wähle **Heimnetz (Bridge)**. Fehlt eine Bridge, richtet TitanOS sie automatisch über den aktiven kabelgebundenen NAS-Anschluss ein. Lass das Fenster geöffnet, bis die Einrichtung abgeschlossen ist. Sind mehrere Bridges vorhanden, wähle die gewünschte Bridge im zusätzlichen Dropdown.
5. Speichere die VM-Einstellungen.
6. Starte die VM wieder. Prüfe im Gast die neue IP-Adresse; bei statischer Netzwerkkonfiguration musst du diese selbst an das gewählte Netz anpassen.

Portweiterleitungen gelten ausschließlich für NAT. Im Modus Heimnetz (Bridge)
verwendest du direkt die LAN-IP-Adresse der VM. Bei Host-only greifst du vom
NAS auf die private VM-IP-Adresse zu.

## Eine Bridge automatisch einrichten

TitanOS erkennt vorhandene Linux-Bridges mit einer Verbindung zu einem
physischen Netzwerkanschluss. Docker-Netze und die privaten VM-Netze werden
nicht als Heimnetz-Bridges angeboten. Fehlt eine passende Bridge, löst die
Auswahl **Heimnetz (Bridge)** ihre automatische Einrichtung über den aktiven
**kabelgebundenen NAS-Anschluss** aus. Die Oberfläche zeigt an, welcher Anschluss
dafür verwendet wird. Eine bestehende Bridge wird verwendet und nicht umgebaut.

Bei der Einrichtung übernimmt die Bridge die bisherige NAS-Verbindung des
aktiven Anschlusses. Die Weboberfläche kann kurz die Verbindung verlieren und
versucht selbstständig, sie wiederherzustellen. **Lass das Fenster währenddessen
geöffnet.** Die Oberfläche bestätigt die neue Verbindung automatisch; erst dann
wird die Bridge dauerhaft gespeichert und für die VM ausgewählt. Ein
Netzwerk-Checkpoint sichert die bisherige Konfiguration. Ohne erfolgreiche
Bestätigung wird die Umstellung automatisch zurückgenommen und die bisherige
VM-Netzwerkauswahl bleibt erhalten.

WLAN und ungeeignete oder nicht verwaltete Netzwerkanschlüsse werden nicht
automatisch umgebaut. Ist kein geeigneter Anschluss verfügbar, bleibt NAT
nutzbar. Die automatische Einrichtung verändert weder die anderen NAS-Anschlüsse
noch vorhandene Docker- oder private VM-Netze.

Wurde eine zuvor ausgewählte Bridge entfernt, wähle bei ausgeschalteter VM
eine vorhandene Bridge oder einen der privaten Netzwerkmodi.

[Übersicht](../README.md) · [TitanOS als VM installieren](VM.md)
