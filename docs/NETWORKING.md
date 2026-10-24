# Netzwerk, VM-Bridge und Firewall

Eine VM im Bridge-Modus hängt mit ihrer eigenen MAC-Adresse an `br0`. Ihre IP
kommt vom Router bzw. aus der Netzwerkkonfiguration des Gasts. Die IP des NAS
ist nicht die IP der VM. Home Assistant ist normalerweise unter
`http://<Gast-IP>:8123` erreichbar; `.local` kann zusätzlich über IPv6 auflösen.

Docker kann die IPv4-Weiterleitungsregel (`FORWARD`) auf `DROP` setzen. Wenn
`br_netfilter` aktiv ist, durchlaufen auch Pakete zwischen LAN und VM diese
Regeln. Dadurch kann der IPv4-Zugriff ausfallen, während ein Name über IPv6
noch funktioniert oder der Browser erst nach einem Timeout darauf ausweicht.
Das ist ein reproduzierbarer Konflikt; ohne Netzwerkdiagnose auf dem konkreten
NAS ist er keine gesicherte Erklärung für jede langsame Verbindung.

## Bridge einrichten

Im Netzwerkschritt der Ersteinrichtung richtet TitanOS `br0` automatisch ein,
sofern noch keine ausdrückliche Netzwerkkonfiguration besteht. Bei bestehenden
NAS bietet die VM-Einrichtung **Heimnetz aktivieren**. Bestätigung und Rückfall
stehen direkt dort bereit, während die Eingaben für die VM erhalten bleiben.

`POST /api/v1/network/bridge` nimmt `{"port":"","onlyUnconfigured":false}` entgegen
und liefert denselben Status wie `GET /api/v1/network`. Nur Besitzer und
Administratoren dürfen die Route aufrufen. Gespeicherte Anschlüsse, Bond, VLAN,
DHCP/feste Adresse, Gateway und DNS werden übernommen. Ohne gespeicherten
Anschluss wird genau ein physischer Ethernet-Uplink aus der Standardroute
gewählt; uneindeutige oder fremde virtuelle Uplinks verlangen eine ausdrückliche
Anschlussauswahl (`network_bridge_port`). Zusätzliche Anschlüsse werden nicht
verbunden. `onlyUnconfigured:true` schützt die automatische Ersteinrichtung
vor dem Überschreiben bestehender Einstellungen.

Der Status ergänzt `configured` (ausdrückliche Konfiguration geladen/bestätigt)
und `bridgeReady` (bestätigte Konfiguration und vorhandene Bridge, kein Probelauf).
Neue VMs bevorzugen die bestätigte Bridge. Auch das Backend blockiert die
Anlage bzw. den Start einer Heimnetz-VM während eines Probelaufs. Eine bereits
bestätigte vorhandene Bridge wird ohne erneute Umstellung wiederverwendet.
Der Entwicklungsmodus führt keine Netzwerkänderungen aus.

Die Schichtung ist Anschluss → optional Bond → optional VLAN → `br0`.
Nur die oberste Schnittstelle trägt NAS-Adresse, Gateway und DNS. Untere
Schnittstellen haben weder DHCP noch IPv6-Router-Advertisements oder
Link-Local-Adressen; networkd entfernt ihre vorherige Adresskonfiguration.
`br0` übernimmt die MAC des ersten Anschlusses. DHCPv4 verwendet die MAC als
Client-Kennung. IPv6-Router-Advertisements sind auf der obersten Schnittstelle
ausdrücklich aktiv, auch wenn Docker IP-Forwarding einschaltet.

networkd-Dateien werden atomar ersetzt. Erst nach dem Neuladen entfernt TitanOS
nicht mehr benötigte virtuelle Schnittstellen; eine bestehende, weiter benutzte
Bridge samt VM-Ports bleibt erhalten. Fehler beim Löschen und Umkonfigurieren
werden weitergegeben. Ein Verbindungsabbruch des Browsers unterbricht die
Umstellung nicht. Jeder Anwendungsversuch hat höchstens 30 Sekunden Zeit.

Die Bestätigung innerhalb von zwei Minuten bleibt Pflicht. Ohne Bestätigung
kehrt die vorherige Konfiguration zurück. Scheitert das Zurücksetzen, bleibt
der Vorgang offen und wird alle fünf Sekunden erneut versucht; eine halb
zurückgesetzte Konfiguration lässt sich nicht bestätigen. Auf `/config` wird
erst nach erfolgreicher Bestätigung geschrieben.

Grundlage: [systemd-networkd unter Debian 13](https://manpages.debian.org/trixie/systemd/systemd.network.5.en.html)
und [networkctl](https://manpages.debian.org/trixie/systemd/networkctl.1.en.html).

## Firewall derzeit ausgeschaltet

Auf Entscheidung des Projektinhabers vom 15. Oktober 2026 ist die
Paketfilterung vorerst vollständig freigegeben. Eine konfigurierbare Firewall
in der Oberfläche folgt später. Dies ersetzt die frühere, nur für `br0`
geltende Ausnahme.

`titanos-firewall-open.service` setzt vor dem Netzwerkstart für IPv4 und IPv6
`INPUT`, `OUTPUT` und `FORWARD` auf `ACCEPT` und stellt eine unbedingte
Freigabe an den Anfang. Auch `DOCKER-USER` bekommt diese Freigabe, damit neu
angelegte Docker-Netze den Verkehr nicht wieder sperren. Dieselbe Einrichtung
läuft vor Netzwerkkonfigurationsänderungen und nach jedem Docker-Start.
Wiederholte Aufrufe erzeugen keine doppelten Freigaben. Die Regeln werden
bei jedem Boot neu erzeugt; dafür gibt es keine zyklischen Stickzugriffe.

Netfilter bleibt für NAT, Connection Tracking und Docker-Portweiterleitungen
verfügbar. Die NAT-Tabellen werden nicht geleert, Docker läuft weiterhin mit
seiner iptables-Unterstützung. `nftables.service` ist maskiert; libvirts
unbenutztes Standard-NAT-Netz startet nicht automatisch. TitanOS verwendet
für VMs `br0` oder QEMU-User-Networking. Die Regelung ist für den mit Debian 13
ausgelieferten Docker-/libvirt-Stack ausgelegt; zusätzlich von Hand eingerichtete
native nftables-Regelsätze werden nicht automatisch entfernt.

Grundlage: [Docker: iptables](https://docs.docker.com/engine/network/firewall-iptables/)
und [Docker: Paketfilterung und NAT](https://docs.docker.com/engine/network/packet-filtering-firewalls/).

## Regressionstest

`scripts/test-bridge-forwarding.py` erstellt ausschließlich in einem separaten
Netzwerk-Namespace eine Bridge mit zwei virtuellen Rechnern und HTTP-Diensten
auf Port 8123. Der Test prüft echte IPv4-/IPv6-Verbindungen: funktionierender
Ausgangszustand, reproduzierter Ausfall mit `FORWARD DROP`, erfolgreiche
Freigabe einschließlich Host-, Routing- und weiterer Bridge-Verbindungen,
Erhalt der NAT-Regeln und wiederholter Aufruf ohne doppelte Regeln. Ein echter
DHCP-Client (BusyBox) erhält von dnsmasq seine MAC-basierte Reservierung über
die Bridge, ausgehend von einer Schnittstelle ohne IPv4-Adresse.

Auf einem Linux-Testrechner mit Python 3, iproute2, iptables, br_netfilter,
dnsmasq-base und BusyBox:

```sh
sudo modprobe br_netfilter
sudo unshare --net python3 scripts/test-bridge-forwarding.py
```

Die GitHub-Build-Checks führen diesen Test aus. `smoke-release.py --plugins`
prüft außerdem am echten Release-ZIP in UEFI-QEMU: Bridge-Erstellung, DHCP und
feste NAS-Adresse, automatischen Rückfall, Wiederherstellung nach Neustart,
Adressfreiheit des unteren Ports, unveränderte Bridge-/Gast-MAC, Gast-DHCP,
Erhalt eines laufenden Gast-Ports und Verkehr nach echtem Docker-Neustart sowie
nach Erstellen eines Docker-Netzes. Diese Test-VM verwendet nur Wegwerf-Medien.
Bonding/VLAN auf echter Hardware und der neue Stand auf dem NAS mit
Home Assistant bleiben gesonderte Hardwaretests.

Der ergänzende Test `scripts/test-vm-bridge-qemu.py --zip <lokales-installations-zip>`
bootet neue Wegwerf-Medien zweimal mit UEFI/TCG und injiziert das aktuelle
`backend/bin/titand` je Boot ausschließlich in den RAM des Gasts. Er prüft den
neuen Endpunkt, echte Zwei-Minuten-Rücknahme, Gast-DHCP, Dienst- und NAS-Neustart.
Er ändert weder Host-Netzwerk noch Host-Laufwerke und ersetzt keine Prüfung eines
neu gepackten Release-ZIPs. Einzelheiten: [Prüfbericht](VM-CREATE-VALIDATION.md).
