# VM-Bridge und Docker

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

TitanOS richtet deshalb vor dem Aktivieren von `br0` und nach jedem Start von
Docker eine eng begrenzte Ausnahme in `DOCKER-USER` ein: IPv4 und IPv6 dürfen
auf `br0` zwischen Bridge-Ports weitergeleitet werden. Der `physdev`-Filter
beschränkt die Ausnahme auf tatsächlich gebridgte Pakete. Gerouteter Verkehr,
andere Bridges, Docker-Portfreigaben und die Standardrichtlinien bleiben
unverändert. Die Regeln sind flüchtig und werden bei jedem Boot neu erzeugt.
Die systemd-Ergänzung für Docker stellt sie auch nach einem Docker-Neustart
wieder her. Grundlage: [Docker-Dokumentation zu iptables und Weiterleitung](https://docs.docker.com/engine/network/firewall-iptables/).

## Regressionstest

`scripts/test-bridge-forwarding.py` erstellt ausschließlich in einem separaten
Netzwerk-Namespace eine Bridge mit zwei virtuellen Rechnern und HTTP-Diensten
auf Port 8123. Der Test prüft echte IPv4-/IPv6-Verbindungen: funktionierender
Ausgangszustand, reproduzierter Ausfall mit `FORWARD DROP`, erfolgreiche
Korrektur, Wiederherstellung fehlender Hooks und wiederholter Aufruf ohne
doppelte Regeln. Gerouteter Verkehr über dieselbe Bridge und Verkehr auf einer
anderen Bridge müssen weiterhin blockiert sein.

Auf einem Linux-Testrechner mit Python 3, iproute2, iptables und br_netfilter:

```sh
sudo modprobe br_netfilter
sudo unshare --net python3 scripts/test-bridge-forwarding.py
```

Die GitHub-Build-Checks führen diesen Test aus. Zusätzlich bleibt ein Test auf
dem NAS mit dessen Home-Assistant-VM erforderlich.
