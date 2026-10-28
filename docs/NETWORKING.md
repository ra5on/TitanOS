# Netzwerk, VLANs, Bonds und Anwendungen

Ab 0.23.0 kann TitanOS mehrere benannte NAS-Netze verwalten. Seit der
Überarbeitung der Seite **Einstellungen › Netzwerk** gibt es nur noch diese
Ansicht; die frühere Einzelnetz-Ansicht entfällt. Bestehende NAS behalten ihre
gespeicherte Konfiguration unverändert in Betrieb, die Seite zeigt sie bereits
als benanntes Netz (siehe [Übernahme bestehender Einstellungen](#übernahme-bestehender-einstellungen)).
Router, DHCP-Server und
übergreifende Zugriffsregeln bleiben extern. TitanOS wird dadurch kein Router
oder DHCP-Server. Was das NAS selbst annimmt, regelt die eigene, ab Werk
ausgeschaltete [Firewall](FIREWALL.md).

## Mehrere NAS-Netze

Ein Netz hat einen Namen, einen Zweck (Verwaltung, Speicher, Anwendungen oder
benutzerdefiniert), einen physischen Anschluss bzw. Bond, optional eine VLAN-ID
und optional eine Bridge. Mehrere VLANs dürfen denselben Uplink verwenden.
Bond-Ports gehören jeweils genau einem Bond; Switch-Einstellungen müssen zum
gewählten Modus passen. Active-backup benötigt kein LACP; 802.3ad verlangt eine
passende Switch-Konfiguration. VLANs erfordern ein passendes externes getaggtes
Netz. TitanOS verteilt selbst keine Adressen.

IPv4 unterstützt DHCP, feste Adresse/Präfix und Aus. IPv6 unterstützt SLAAC/RA,
DHCPv6/RA, feste Adresse/Präfix und Aus. Dazu kommen DNS und gezielte statische
Routen. Genau ein Netz ist der bevorzugte Verwaltungszugang: nur dort werden
automatisch Standardrouten und DNS aus DHCP/RA übernommen. Sekundäre Netze
verdrängen diese nicht. Explizite Gateways/Routen bleiben eine bewusste
Konfiguration; redundante IP-Netze auf verschiedenen Anschlüssen werden
abgewiesen. Zweck und Verwaltungsmarkierung sind keine Firewall-Regeln.

VMs wählen ein bestätigtes benanntes Netz mit Bridge (`network:<id>`). Docker
verwendet einen bestätigten Anschluss des gewählten NAS-Netzes. Auch gestoppte,
konfigurierte VMs/Container-Netze schützen ihre Netze vor Entfernen bzw. Änderung
von Anschluss, VLAN und Bridge. Vor einem solchen Umbau Anwendungen verlagern.
Bestehende VMs an `br0` behalten beim Umstieg denselben Uplink und dieselbe VLAN-ID.

Neue Topologien liegen separat in `/config/network/topology.json`; die alte
`network.json` bleibt für einen Systemrückfall erhalten. Ältere Versionen sehen
damit ihre vorherige Konfiguration. Benannte VM-Netze werden für alte Versionen
mit deaktiviertem Autostart gespeichert. Beim aktuellen Laden wird der
Autostartwunsch wiederhergestellt; nach einem Rückfall benannte Netze vor
manuellem VM-Start prüfen.

### VLANs ein- und ausschalten

VLANs sind ab Werk ausgeblendet (Schnittstellen › VLANs › „VLANs verwenden“).
Ein VLAN markiert jedes Paket; die Gegenseite muss diese Markierung verstehen.
Ein einfacher Router oder nicht verwalteter Switch verwirft markierte Pakete
oder reicht sie ungeprüft weiter. Ohne VLAN-fähigen Router reicht oft ein
verwalteter Switch; für VMs, LXC und Docker genügen eigene Netze innerhalb des
NAS (Bridges, Docker-Netze) als Firewall-Zonen.

Ausgeschaltet bietet der Netzwerk-Editor keine VLAN-ID an. Einschalten fragt
einmal mit einem einfachen Bestätigungsdialog. Die Wahl steht NAS-weit in
`/config/network/options.json` (`vlans`), wird nur beim Start gelesen und nur
beim Ändern geschrieben; ältere titand ignorieren die Datei. Eine
Konfiguration, die bereits ein VLAN verwendet (aktiv oder auf Probe), gilt als
eingeschaltet; Ausschalten wird dann mit `vlans_in_use` abgelehnt, bis die
VLAN-IDs entfernt sind. API: `PUT /api/v1/network/options` mit
`{"vlans": true|false}`, Status `vlans`/`vlansInUse` in `GET /api/v1/network`.
Die Option ändert keine Netzwerkdateien und braucht keinen Probelauf.

### Aufbau der Seite

- **Übersicht:** Kachel „System“ mit dem Namen des NAS (`PUT /api/v1/system/hostname`,
  sofort wirksam, nicht Teil des Entwurfs, während einer Probe gesperrt) und
  der Zeitzone (Link zu Datum und Uhrzeit). Darunter je Netz eine Karte mit
  Zweck, Zustand (verbunden, ohne Adresse, kein Link oder „noch nicht
  übernommen“), Verbindung und tatsächlicher Schnittstelle, aktuellen Adressen,
  IPv4/IPv6-Modus, Gateway und DNS. Das Menü „⋯“ bietet Bearbeiten, Diagnose
  und Entfernen; das bevorzugte Verwaltungsnetz lässt sich nicht entfernen.
  Der Suchtreffer „DNS“ springt zur DNS-Zeile des Verwaltungsnetzes
  (`#network-dns`).
- **Schnittstellen:** Anschlüsse mit Link, Tempo, MAC und „Verwendet von“
  (Bond oder Netze direkt am Anschluss); Bonds mit übersetztem Modus,
  Bearbeiten und Entfernen. Ein Bond, auf dem noch ein Netz liegt, lässt sich
  nicht entfernen; ein Anschluss, der ein Netz direkt trägt, nicht in einen
  Bond aufnehmen.
- **Diagnose** (siehe unten) und **Fernzugriff**: Tailscale und NetBird mit
  ihren vollständigen Plugin-Einstellungen, sobald das Plugin installiert ist,
  sonst ein Verweis auf Plugins; dazu Verweise auf HTTPS-Namen, App-Links,
  dynamisches DNS.
- Der Editor eines Netzes gliedert sich in Allgemein, IPv4, IPv6 (Modus jeweils
  als Umschalter), DNS und Routen. DNS-Server werden als einzelne Einträge
  gesammelt; Leerzeichen, Komma oder Semikolon trennen (höchstens drei).
  Änderungen landen im Entwurf; erst „Prüfen und testen“ schickt den ganzen
  Entwurf an `PUT /api/v1/network` und startet die Probe. Der gewählte Reiter
  steht in `?tab=`.
- `GET /api/v1/network` liefert zusätzlich `live` (je Netz-ID Schnittstelle,
  Link und globale Adressen, aus einem einzigen `ip -j address show`),
  `migrated` sowie `topology` und `pendingTopology` (siehe nächster Abschnitt).
  `config` und `pending` bleiben genau so, wie sie gespeichert sind; ältere
  Clients und Skripte, die die flachen Felder senden und zurückerwarten,
  arbeiten unverändert.

### Übernahme bestehender Einstellungen

Eine `network.json` ohne `networks[]` (alle NAS aus der Zeit der
Einzelnetz-Ansicht und ohne gespeicherte Netzwerkeinstellung) erscheint im
Status unter `topology` (und eine Probe unter `pendingTopology`) als genau ein
bevorzugtes Verwaltungsnetz (`migrateLegacy`
in `internal/network/migrate.go`):

- Kennung `admin`, Name „LAN“, Zweck Verwaltung, dieselbe VLAN-ID und Bridge.
- Mehrere Anschlüsse werden der Bond `uplink` mit demselben Modus und derselben
  Reihenfolge der Ports; ein Anschluss bleibt direkter Anschluss. Ohne
  gespeicherten Anschluss (jeder Port per DHCP) wird der Port mit der
  Standardroute gewählt, sonst der erste mit Kabel.
- DHCP wird IPv4 DHCP und IPv6 DHCPv6/RA (networkd `DHCP=yes` fragte beides),
  eine feste Adresse bleibt fest mit Gateway und IPv6 SLAAC/RA. DNS-Server
  bleiben.

Diese Fassung ist nur Anzeige und Entwurf (`migrated: true`): titand legt
weiter die bisherigen networkd-Dateien an, VMs, Container und Docker sehen
weiter die bisherige Konfiguration, und auf den Stick wird nichts geschrieben.
Gespeichert wird erst, wenn eine Änderung über „Prüfen und testen“ auf Probe
geht und über die neue Verwaltungsadresse bestätigt wird: dann entsteht
`topology.json`, die bestätigte alte Fassung landet in `last-good.json`,
`network.json` bleibt für ältere Systemslots unverändert. Ohne Bestätigung
kehrt die alte Konfiguration zurück. Die Bridge des Verwaltungsnetzes heißt
weiter `br0` und übernimmt die MAC des ersten Ports; VMs und LXC an `br0`
behalten so Anschluss, VLAN und DHCP-Adresse. Untere Schnittstellen bekommen
dabei die Namen der Netz-Topologie (`tbuplink` statt `bond0`, `tvadmin` statt
`vlan<ID>`), was während der Probe kurz die Verbindung unterbrechen kann.
Legacy-Felder werden in `topology.json` nicht mehr geschrieben;
`GET /api/v1/network` liefert sie für ältere Leser weiter, solange
`network.json` gilt. Ältere Clients können weiterhin eine reine
Legacy-Konfiguration an `PUT /api/v1/network` schicken, solange keine
Topologie bestätigt ist (`POST /api/v1/network/bridge` nutzt diesen Weg).
Geprüft mit Unit-Tests (`migrate_test.go`): Übersetzung, gleiche Bridge-Datei,
Schutz für Legacy-VMs, Status ohne Systemänderung, Probe/Zurücknehmen und
Bestätigen samt Dateien; echte Hardware mit Bond/VLAN nicht.

### Probelauf und lokale Rettung

Jede Änderung läuft weiterhin zwei Minuten auf Probe. Bestätigen muss eine
normale Administratorsitzung über die tatsächliche neue Verwaltungsadresse;
Host-/Forwarded-Kopfzeilen und API-Tokens ersetzen diesen Nachweis nicht.
Ohne Bestätigung wird zurückgesetzt. Auch ein Dienstneustart lädt die zuletzt
bestätigte Konfiguration, keine unbestätigte Browserfassung.

An der angeschlossenen NAS-Konsole öffnet **Strg+Alt+F2** die lokale
Netzwerkwiederherstellung. NAS-Besitzer/Administrator melden sich mit ihrem
NAS-Passwort und gegebenenfalls 2FA-/Recovery-Code an. Danach lässt sich die
vorige bestätigte Konfiguration oder DHCP auf genau einem ausgewählten
physischen Anschluss wiederherstellen. Erst die Texteingabe JA/YES stoppt
vorübergehend titand, stellt das Netz um und startet den Dienst wieder.
Es gibt keinen ungeschützten Root-Login. Bei manueller Wartung kann root
`titand -network-recovery` aufrufen; interaktives
Terminal ist Pflicht.

Am 26. Oktober 2026 wurde dieser Weg auf der tatsächlichen TTY2 eines
Wegwerf-QEMU-Gasts geprüft: falsches Passwort und ungültiger zweiter Faktor
werden abgewiesen. Ein Besitzerkonto mit Passwort und Einmal-Recovery-Code
stellt DHCP auf dem ausgewählten Anschluss wieder her, nachdem eine falsche
Verwaltungsadresse den HTTPS-Zugang unterbrochen hat. Die Weboberfläche war
vor Ablauf des automatischen Probelauf-Rückfalls wieder erreichbar.
[Prüfschritte, Abbildhash und Grenzen](validation/v0.23.0-workflows.json).
Der andere Rettungsmenüpfad und echte Konsolenhardware sind damit nicht
abgenommen.

Die zusätzliche v0.23.1-Prüfung verwendet an TTY2 einen Recovery-Code und
einen TOTP-Code und versucht beide vor jedem Daemon-Neustart erneut im
Webdienst. Beide Wiederholungen werden abgewiesen. Die Prozesssperre und der
aktuelle Kontostand verhindern die zuvor beobachtete Wiederverwendung;
[Nachweis und Verbrauchsbuchung](validation/v0.23.1-authorization.json).

### Container mit eigener LAN-Adresse

Unter **Docker › LAN-Netze** ein IPv4-Subnetz, externes Gateway und einen
reservierten IP-Pool innerhalb des Subnetzes festlegen. **Diesen Pool im
externen DHCP-Server von der Vergabe ausschließen.** Docker-IPAM vergibt daraus
dynamische Adressen; für einzelne Dienste kann eine feste Adresse aus diesem
Pool gesetzt werden. Dafür läuft kein DHCP-Server in TitanOS.

Standard ist ipvlan L2, alternativ macvlan. Sie verwenden ein bestätigtes
NAS-Netz; mehrere Dienste behalten ihre privaten Compose-Netze. Beim Bearbeiten
einer App wird nur der gewählte Dienst ans LAN gehängt, eine Datenbank landet
nicht automatisch im LAN. Manuelle Compose-Einstellungen bleiben prüfbar.
Die App wird erst mit dem normalen Speichern neu erstellt. Laufende bzw.
gespeicherte App-Referenzen sperren das Entfernen eines LAN-Netzes.

ipvlan/macvlan isolieren den ursprünglichen Host-Endpunkt. Optionaler NAS-Zugang
legt deshalb einen eigenen lokalen Endpunkt mit einer freien Adresse außerhalb
des Containerpools an und routet nur diesen Pool darüber. Auch diese Adresse im
externen Netz reservieren. Es entstehen keine zusätzlichen Firewall-Regeln.
Docker-LAN-Pools sind in dieser Version IPv4; IPv6 der NAS-Netze ist unabhängig.
WLAN-Uplinks, alle Switch-/Bond-Varianten und beliebige Docker-Netzwerktreiber
sind nicht durch einen QEMU-Test abgedeckt.

### Fernzugriff und Diagnose

Der Reiter Fernzugriff zeigt die Einstellungen der installierten Tailscale-
und NetBird-Plugins (dieselben Komponenten wie unter Plugins, mit ihren
eigenen Fristen) und verlinkt sonst die Plugin-Seite.
Bestehende Tailscale-Hooks und Docker-NAT/Portweiterleitungen bleiben erhalten.
Das NetBird-Plugin filtert in eigenen nftables-Tabellen; sein einziger Hook
nimmt `wt0` vor den Tailscale-Hooks an (siehe [NetBird](NETBIRD.md)).
Cloudflare bzw. andere öffentliche App-URLs werden bei den App-Links verwaltet;
eine URL richtet keinen Tunnel ein. Cloudflare-Zero-Trust-/Tailnet-ACLs und
Router-Firewalls werden weiterhin im jeweiligen externen System verwaltet.

Die Diagnose prüft tatsächlichen Link/Adressen, konfigurierte Gateways und
DNS-Auflösung. Sie gilt nur übernommenen Netzen (auch dem übersetzten
Verwaltungsnetz einer alten Konfiguration, dann an deren tatsächlicher
Schnittstelle) und ist während einer Probe gesperrt; einen Entwurf prüft man
nach „Prüfen und testen“ über den neuen Zugang. Ein erfolgreicher `ip`-Aufruf allein bedeutet keinen gesunden
Link. DNS-/Ping-Fehler bleiben sichtbar; eine fehlgeschlagene Diagnose ist kein
allgemeiner Ausfallnachweis aller Netzwerkdienste.

### SSH nur aus bestimmten Netzen

Unter Einstellungen › Zugang › SSH lässt sich eine Liste von Netzen in
CIDR-Schreibweise (IPv4/IPv6) angeben, aus denen SSH-Anmeldungen kommen dürfen.
Eine einzelne Adresse gilt als `/32` bzw. `/128`, höchstens 32 Einträge. Eine
leere Liste bedeutet wie bisher: aus allen Netzen. Vorgaben fügen das
**Heimnetz** (die Netze, in denen das NAS gerade selbst eine Adresse hat, ohne
Loopback, Link-local, Docker-, VM- und VPN-Schnittstellen), **Tailscale**
(`100.64.0.0/10`, `fd7a:115c:a1e0::/48`) oder **NetBird** (Standardbereich
`100.64.0.0/10`) hinzu.

- Durchgesetzt wird die Liste von sshd selbst, nicht von einer Firewall:
  titand schreibt `/etc/ssh/sshd_config.d/20-titanos-networks.conf` mit
  `AllowUsers *@<netz> …`. `AllowGroups titanos-ssh` aus der Grundkonfiguration
  gilt weiter; beide müssen eine Anmeldung zulassen. Die Netzliste ändert
  keine Firewall-Regeln; ist die [Firewall](FIREWALL.md) eingeschaltet, muss
  zusätzlich deren Dienst SSH für die Zone erlaubt sein.
- Vor dem Übernehmen prüft `sshd -t` die gesamte Konfiguration. Lehnt sshd sie
  ab, bleibt die vorige Datei bestehen, sshd wird nicht neu geladen, und die
  Seite zeigt den Fehler. Eine geänderte Liste lädt sshd per
  `systemctl reload ssh.service` neu; offene Sitzungen bleiben bestehen, auch
  wenn ihre Adresse nicht mehr erlaubt ist.
- Gespeichert wird im optionalen Feld `allowedNetworks` in
  `/config/system/ssh.json`, nur beim Speichern der Liste. Ältere titand-Versionen
  ignorieren das Feld; schaltet eine ältere Version SSH um, schreibt sie die
  Datei ohne Liste (danach wieder alle Netze). Die Datei unter `/etc` liegt im
  RAM: Nach einem Rückfall auf eine ältere App ohne Neustart gilt sie bis zum
  nächsten NAS-Start weiter.
- Schutz der eigenen Sitzung: Enthält die Liste die Adresse nicht, von der die
  speichernde Websitzung kommt, lehnt die API mit `ssh_network_excludes_you` ab.
  Die Oberfläche zeigt dann einen einfachen Bestätigungsdialog und schickt die
  Liste danach mit `confirm: true`. Die Adresse ist die tatsächliche
  Socket-Adresse der Anfrage; Forwarded-Kopfzeilen zählen nicht.
- Rückweg: Die Weboberfläche ist von der Liste nicht betroffen. Ohne
  Weboberfläche hebt die lokale Konsole (**Strg+Alt+F2**,
  `titand -network-recovery`, Anmeldung wie oben, Auswahl **3**, dann JA/YES)
  die Beschränkung auf; titand wird dafür kurz gestoppt und wieder gestartet.
- Route: `PUT /api/v1/system/ssh/networks` mit `{"networks": [...], "confirm": false}`,
  nur Besitzer/Administratoren in einer Websitzung (nicht über API-Tokens, nicht
  im Assistenten-Katalog). `GET /api/v1/system/ssh` liefert zusätzlich
  `allowedNetworks`, `networksError`, `homeNetworks`, `tailscaleNetworks`,
  `netbirdNetworks` und `clientAddress`.
- Geprüft mit Unit-Tests (Normalisierung, Adressabdeckung, erzeugte Datei,
  abgelehntes `sshd -t`, Reload, Rückweg über die Konsole, Lesbarkeit für ältere
  Versionen) und API-Tests. Ein echtes sshd in QEMU und Anmeldungen über Tailscale
  bzw. NetBird sind damit noch nicht abgenommen.

## Bestehende Heimnetz-Bridge

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

## Firewall

Die konfigurierbare Firewall unter Einstellungen › Firewall ist ab Werk aus;
Zonen, Dienste, Docker-Apps, eigene Regeln, Probe und automatisches Sperren
beschreibt [Firewall](FIREWALL.md).

**Ausgeschaltet** (Stand bis zum ausdrücklichen Einschalten, auch nach einem
Update) ist die Paketfilterung vollständig freigegeben, wie vom Projektinhaber am
15. Oktober 2026 entschieden. `titanos-firewall-open.service` setzt vor dem
Netzwerkstart für IPv4 und IPv6 `INPUT`, `OUTPUT` und `FORWARD` auf `ACCEPT` und
stellt eine unbedingte Freigabe an den Anfang. Auch `DOCKER-USER` bekommt diese
Freigabe, damit neu angelegte Docker-Netze den Verkehr nicht wieder sperren.
Dieselbe Einrichtung läuft vor Netzwerkkonfigurationsänderungen und nach jedem
Docker-Start. Wiederholte Aufrufe erzeugen keine doppelten Freigaben. Die Regeln
werden bei jedem Boot neu erzeugt; dafür gibt es keine zyklischen
Stickzugriffe. Ist nur das automatische Sperren eingeschaltet, enthält die
Tabelle `inet titanos` nur die gesperrten Absender.

**Eingeschaltet** bleibt diese offene iptables-Einrichtung unverändert
bestehen. titand lädt zusätzlich seine eigene Tabelle `inet titanos`; weil in
nftables ein `drop` in irgendeiner Basiskette das Paket verwirft, filtert sie
unabhängig von den `ACCEPT`-Regeln der iptables-Ketten. Docker-NAT, die
Tailscale-Hooks und NetBirds Tabellen bleiben unberührt.

Netfilter bleibt für NAT, Connection Tracking und Docker-Portweiterleitungen
verfügbar. Die NAT-Tabellen werden nicht geleert, Docker läuft weiterhin mit
seiner iptables-Unterstützung. `nftables.service` ist maskiert; das Paket
`nftables` liefert nur das Werkzeug `nft`, mit dem titand seine Tabelle lädt.
libvirts unbenutztes Standard-NAT-Netz startet nicht automatisch. TitanOS
verwendet für VMs `br0` oder QEMU-User-Networking. Die Regelung ist für den mit
Debian 13 ausgelieferten Docker-/libvirt-Stack ausgelegt; zusätzlich von Hand
eingerichtete native nftables-Regelsätze werden nicht automatisch entfernt. Das
NetBird-Plugin legt eigene nftables-Tabellen `netbird` an, deren Regeln nur
`wt0` betreffen und von der offenen Freigabe nicht aufgehoben werden; NetBird
entfernt sie beim Beenden selbst.

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
