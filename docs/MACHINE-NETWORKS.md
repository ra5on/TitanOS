# Netzwerk für virtuelle Maschinen

Ab TitanOS 2.0.2 findest du beim Erstellen einer eigenen VM und in den
Einstellungen einer VM das Dropdown **Netzwerk**.

| Auswahl | Verbindung der VM |
| --- | --- |
| NAT | Privates Netz `10.203.0.x` mit Internetzugriff über das NAS. Dienste erreichst du aus dem LAN über eingerichtete Portweiterleitungen. |
| Host-only | Privates Netz `10.204.0.x` für den Zugriff vom NAS. Die VM erhält keinen Internet- oder LAN-Zugang über dieses Netz. |
| LAN-Bridge | Direkte Verbindung über eine bereits auf dem NAS eingerichtete Linux-Bridge mit einem physischen Netzwerkanschluss. Die IP-Adresse kommt aus deinem LAN, beispielsweise per DHCP vom Router. |

**NAT ist der Standard.** Betriebssysteme aus dem VM-Katalog werden zunächst
mit NAT installiert, damit Downloads und die automatische Einrichtung
funktionieren. Nach Abschluss der Installation kannst du das Netzwerk wechseln.
Bei einer eigenen VM aus einem hochgeladenen Image oder einer ISO kannst du die
Auswahl bereits beim Erstellen treffen.

## Netzwerk einer bestehenden VM ändern

1. Schließe die Installation und die automatische Ersteinrichtung der VM ab.
2. Fahre die VM vollständig herunter. Eine pausierte VM reicht nicht aus.
3. Öffne ihre Einstellungen und wähle unter **Netzwerk** den gewünschten Modus.
4. Wähle bei **LAN-Bridge** zusätzlich die vorhandene Bridge und speichere die Einstellungen.
5. Starte die VM wieder. Prüfe im Gast die neue IP-Adresse; bei statischer Netzwerkkonfiguration musst du diese selbst an das gewählte Netz anpassen.

Portweiterleitungen gelten ausschließlich für NAT. Im Modus LAN-Bridge
verwendest du direkt die LAN-IP-Adresse der VM. Bei Host-only greifst du vom
NAS auf die private VM-IP-Adresse zu.

## Wenn keine LAN-Bridge angeboten wird

TitanOS erkennt vorhandene Linux-Bridges mit einer Verbindung zu einem
physischen Netzwerkanschluss. Docker-Netze und die privaten VM-Netze werden
nicht als LAN-Bridges angeboten. Die Auswahl legt keine Bridge an und verändert
keinen Netzwerkanschluss des NAS. Ohne passende vorhandene Bridge bleibt die
Option deaktiviert.

Wurde eine zuvor ausgewählte Bridge entfernt, wähle bei ausgeschalteter VM
eine vorhandene Bridge oder einen der privaten Netzwerkmodi.

[Übersicht](../README.md) · [TitanOS als VM installieren](VM.md)
