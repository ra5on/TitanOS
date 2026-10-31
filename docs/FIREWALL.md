# Firewall

TitanOS hat einen eigenen Paketfilter unter **Einstellungen › Firewall** (nur
Besitzer und Administratoren). Er ist **ab Werk aus**; nach einem Update bleibt
alles so offen wie bisher (`titanos-firewall-open`, siehe
[Netzwerk](NETWORKING.md#firewall)), bis die Firewall in der Oberfläche
eingeschaltet wird. Paket `backend/internal/firewall`, API `/api/v1/firewall`.

## Zonen

Jede neue Verbindung wird nach ihrem Absender einer Zone zugeordnet, in dieser
Reihenfolge (die erste passende gilt):

| Zone | Absender |
|---|---|
| eigene Zonen (bis 16, in der Reihenfolge der Liste) | Netze des NAS, die der Zone zugeordnet sind (Anschluss **und** Subnetz, siehe unten), und ihre Quellnetze (Adressen/Netze, egal über welchen Anschluss) |
| Docker | alle Docker-Bridge-Netze (`docker0`, `br-<id>`), die keiner eigenen Zone gehören – auch jedes neu angelegte |
| VPN | Schnittstellen `tailscale0`, `wt0`, `titanoswg` (WireGuard-Server des NAS, `docs/WIREGUARD.md`) sowie `ipv64wg` (Tunnel zu einem IPv64 Cloud Router, `docs/IPV64.md`), Bereiche `100.64.0.0/10` und `fd7a:115c:a1e0::/48` (Tailscale, NetBird) |
| Heimnetz | private Bereiche (10/8, 172.16/12, 192.168/16, 169.254/16, `fc00::/7`, `fe80::/10`) und die Netze der eigenen Anschlüsse des NAS (Bridge `br0`, VLANs, Bonds), damit auch das öffentliche IPv6-Präfix des Heimnetzes |
| Internet | alles andere, ausdrücklich auch öffentliche IPv6-Adressen außerhalb des eigenen Präfixes |

Die Netze der eigenen Anschlüsse liest titand alle 15 Sekunden aus dem Kernel
(dieselbe Auswahl wie die Vorgabe „Heimnetz“ der SSH-Netzliste, ohne Docker-,
VM- und VPN-Schnittstellen). Ändert sich etwa das IPv6-Präfix des Providers,
werden die Regeln neu geladen. Präfixe kürzer als /8 zählen nie als Heimnetz.

**Netze des NAS** (Reiter „Netze“, Liste in `GET /firewall` als `networks`,
alle 15 Sekunden abgeglichen, auch bei ausgeschalteter Firewall):

| Art | ID | Gerät | Herkunft |
|---|---|---|---|
| Netzwerk/VLAN/Bridge | `net:<id>` | `br0`, `tn<id>`, `tv<id>`, `tb<id>` | Einstellungen › Netzwerk (bestätigte Topologie), Subnetze aus den Adressen der Schnittstelle |
| Docker | `docker:<name>` | `docker0`, `br-<12 Zeichen der ID>` oder `com.docker.network.bridge.name` | Docker-API `/networks`, Subnetze aus IPAM |
| VM/LXC-NAT | `vm:<gerät>` | `virbr*`, `lxcbr*` | nur, wenn es solche Bridges gibt; TitanOS-VMs nutzen das QEMU-Usermode-Netz oder eine Bridge des LAN, LXC eine Bridge des LAN |
| Docker im LAN | `docker:<name>` | – (Elternschnittstelle) | macvlan/ipvlan, nur angezeigt |

Ohne eigene Zone gehören Docker-Netze zur Zone Docker, alle anderen Netze
werden nach Adresse zugeordnet (meist Heimnetz). Jedes Netz gehört höchstens
einer eigenen Zone. Ein Netz, das verschwindet (Docker-Netz gelöscht, VLAN
entfernt), bleibt in seiner Zone vermerkt und erscheint als „nicht
vorhanden“; kommt es wieder, gilt die Zuordnung wieder. Die Regeln ordnen ein
Netz über `iifname` **und** seine Subnetze zu (`iifname "tv5" ip saddr
192.168.50.0/24 goto <zone>`), damit titand, das nur die Adresse sieht
(Aussperr-Schutz, `AllowHTTP`), zum selben Ergebnis kommt; ein Netz ohne
Adresse auf dem NAS wird nur über die Schnittstelle zugeordnet. Über einen
Anschluss gerouteter Verkehr aus fremden Subnetzen fällt damit nicht in die
Zone des Anschlusses, sondern wird nach Adresse zugeordnet.

**Was am NAS vorbeiläuft:** Docker-Apps mit eigener Adresse im LAN
(macvlan/ipvlan) und VMs/LXC an Bridges des LAN sind eigene Geräte im Netz. Ihr
Verkehr untereinander und mit anderen Geräten passiert den Host-Stack des NAS
nicht. Erfassen lassen sie sich nur als **Absender** über ihre Adressen
(Quellnetze einer eigenen Zone), wenn sie das NAS selbst ansprechen. Eine
`bridge`-Tabelle für Bridge-Ports wäre ein eigener Schritt.

## Matrix, Richtlinien und Vorgaben

Eingeschaltet gilt die **Zonenmatrix** (wie bei UniFi): Zeilen sind die
Quell-Zonen, Spalten die Ziele – **NAS** (Dienste des NAS selbst, Input-Hook),
**Docker-Apps** (veröffentlichte Ports, Forward-Hook nach DNAT) und die
**Zonen**, wohin Verkehr aus Netzen hinter dem NAS weitergeleitet wird. Jede
Zelle hat eine geordnete Liste von **Richtlinien** (erlauben/sperren; ein
Dienst des NAS, eine Docker-App oder Protokoll und Ports; optional Absender,
bei Zellen zu Zonen auch ein Ziel; Notiz; abschaltbar) – die erste passende
entscheidet – und eine **Vorgabe** (erlauben/sperren) für den Rest. Höchstens
640 Richtlinien je Zelle. Die Oberfläche zeigt Zellen als „Erlaubt“,
„Blockiert“ oder „Teilweise“ mit der Zahl der Ausnahmen.

Zellen zu anderen Zonen gibt es nur für Zonen mit Netzen **hinter** dem NAS
(Zone Docker, eigene Zonen mit Docker- oder VM/LXC-NAT-Netzen). Verkehr
zwischen Heimnetz, VPN und Internet läuft nicht über das NAS (es ist kein
Router); diese Zellen sind grau und nicht bearbeitbar.

Vorgaben einer neuen Installation (Entscheidung des Projektinhabers):
Heimnetz und VPN erreichen alle Dienste, Internet nur die VPN-Tunnel, die
Netzwerkumgebung nur das Heimnetz (Tabelle unten). Docker-Apps: Heimnetz, VPN
und Docker erlaubt, Internet gesperrt. Zone Docker: zum NAS nur die
HTTPS-Namen für Apps (App-Proxy) und DNS (TCP/UDP 53), zu Docker-Apps erlaubt,
ins Internet, Heimnetz, VPN und in eigene Zonen ohne Netze hinter dem NAS
erlaubt, in andere Netze hinter dem NAS (Docker-Netze untereinander) gesperrt
– wie Docker sie selbst isoliert. Eigene Zonen: zum NAS und zu Docker-Apps
gesperrt, aus Netzen hinter dem NAS wie die Zone Docker.

Dienste der Matrix (je Dienst eine Richtlinie „erlauben“ in den Zellen
Heimnetz/VPN/Internet → NAS):

| Dienst | Ports | Heimnetz | VPN | Internet |
|---|---|---|---|---|
| Weboberfläche | TCP 80, 443 (bzw. die Ports von `titand -http/-https`) | ja | ja | nein |
| HTTPS-Namen für Apps | TCP 443 | ja | ja | nein |
| SMB | TCP 139, 445; UDP 137–138 | ja | ja | nein |
| NFS | TCP/UDP 111, 2049, 20048 (mountd), 32765–32766 (statd), 32768 (lockd) | ja | ja | nein |
| SSH | TCP 22 | ja | ja | nein |
| Netzwerkumgebung | UDP 3702, 5353, 5355; TCP 5355, 5357 | ja | nein | nein |
| Ping | ICMP/ICMPv6 Echo | ja | ja | nein |
| USV im Netzwerk (NUT) | TCP 3493 | ja | ja | nein |
| Backups anderer TitanOS | TCP 8750 | ja | ja | nein |
| VPN-Tunnel | UDP 41641 (Tailscale), 51820 (NetBird) | ja | ja | **ja** |

Let's Encrypt über HTTP-01 braucht Port 80 aus dem Internet. Bei
eingeschalteter Firewall gehört Port 80 zum Dienst „Weboberfläche“, der im
Internet gesperrt ist: dann DNS-01 verwenden oder in der Zelle „Internet → NAS“
eine Richtlinie „Erlauben, TCP 80“ ohne Absender anlegen ([Let's Encrypt](LETSENCRYPT.md)). Die
Let's-Encrypt-Seite zeigt diesen Hinweis, sobald HTTP-01 gewählt ist und die
Firewall den Port aus dem Internet nicht erlaubt; TitanOS öffnet Port 80 nicht
automatisch.

Abweichung von „nichts aus dem Internet“: die VPN-Tunnel. Ihre Gegenstellen
kommen naturgemäß aus dem Internet, das VPN prüft sie selbst kryptografisch,
und das Tailscale-Plugin verlangt UDP 41641 für direkte Verbindungen. Ohne
diese Freigabe funktionieren Tailscale und NetBird weiter, aber öfter nur über
Relays. Der Schalter lässt sich wie jeder andere abschalten.

Die festen NFS-Hilfsports setzt das Abbild in
`/etc/nfs.conf.d/titanos-ports.conf` und `/etc/modprobe.d/titanos-nfs.conf`
(lockd). NFSv4 braucht nur 2049.

**Weboberfläche und HTTPS-Namen** teilen sich Port 443. nftables lässt den Port
zu, wenn einer der beiden Dienste für die Zone erlaubt ist (eine Sperr-Richtlinie
für einen der beiden lässt den gemeinsamen Port in nftables aus); titand entscheidet
danach am Namen (`appproxy.Manager.Gate` → `firewall.AllowHTTP`) und antwortet
sonst mit 403. Die eigenen erlaubten Netze einer App (Einstellungen › HTTPS-Namen
für Apps) gelten zusätzlich.

**Immer erlaubt:** bestehende Verbindungen (`ct state established,related`),
Loopback, DHCP-/DHCPv6-Antworten an das NAS, ICMP-Fehlermeldungen (auch Path
MTU), IPv6-Nachbar- und Router-Erkennung, MLD-Abfragen, von NetBird auf das NAS
umgeleitetes DNS seiner Peers (`wt0` mit `ct status dnat`) und – solange eine
Änderung auf Probe läuft – die Adresse der Websitzung, die sie gespeichert hat.

## Docker-Apps und Docker-Netze

Veröffentlichte Ports holt titand alle 15 Sekunden über die Docker-API
(Compose-Projekt bzw. `container:<name>`). In den Zellen „→ Docker-Apps“ hat
eine App eine Richtlinie (erlauben/sperren); Apps ohne eigene Richtlinie folgen
der Vorgabe der Zelle (früher „Neue Apps“). So gilt für eine neu installierte
App sofort eine Regel, auch bevor titand ihre Ports kennt.

Docker (Debian 13: Docker 26 mit `iptables-nft`) setzt veröffentlichte Ports
per DNAT in `nat/PREROUTING` um; die Pakete laufen dann durch den
**Forward-Hook**, nicht durch Input. TitanOS filtert sie deshalb in seiner
eigenen Kette `published` → `app_<zone>` nach `ct original proto-dst`, also nach
dem Port auf dem NAS vor der Umsetzung. IPv6-Ports und Verbindungen über
Dockers userland-proxy kommen über Input an; die Ports der Apps, die eine Zelle
erlaubt, stehen deshalb auch in den Input-Ketten der Zonen.

**Docker-Netze als Quelle:** Verkehr eines Containers zum NAS kommt über seine
Bridge und gehört zur Zone Docker (oder der eigenen Zone seines Netzes);
Verkehr zu veröffentlichten Ports anderer Apps folgt der Zelle „→ Docker-Apps“
dieser Zone; neue Verbindungen in andere Zonen (Internet, Heimnetz, VPN,
andere Docker-Netze) folgen den Zellen zwischen Zonen (Kette `routed` →
`from_<zone>` → `fwd_<quelle>_<ziel>`, Ziel nach ausgehender Schnittstelle,
dann nach Adresse). Innerhalb desselben Netzes (gleiche Bridge) wird nichts
gefiltert.

**Docker-Netze untereinander:** Die Tabelle `inet titanos` kann nur zusätzlich
verwerfen. Dockers eigene Isolation (`DOCKER-ISOLATION-*`) greift auf TitanOS
bisher nicht: `titanos-firewall-open` setzt in `FORWARD` und `DOCKER-USER` ein
unbedingtes `ACCEPT` an den Anfang (siehe [Netzwerk](NETWORKING.md)), Docker-Netze
erreichen sich also gegenseitig. Getrennt werden sie bei eingeschalteter Firewall
durch die Zellen zwischen Zonen in `inet titanos` (Vorgabe einer neuen
Installation: gesperrt). Damit eine Freigabe auch dann wirkt, wenn diese
offene Regel fehlt, gilt zusätzlich: Lässt eine Zelle zwischen zwei Zonen mit
Docker-Netzen etwas durch (Vorgabe erlauben oder eine Erlauben-Richtlinie),
öffnet titand Dockers Isolation genau für diese Bridge-Paare in `DOCKER-USER`,
der Kette, die Docker dem Administrator überlässt: eigene Kette `TITANOS-FWD`
in den Filtertabellen von `iptables`/`ip6tables` mit `-i A -o B -j ACCEPT` und
für Antworten `-i B -o A -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT`,
dazu ein Sprung `-I DOCKER-USER 1 -j TITANOS-FWD`. Sonst wird an Dockers
Tabellen nichts geändert (Test `TestDockerUser`). Die genauen Richtlinien der
Zelle (Ports, Adressen) setzt weiterhin `inet titanos` durch. Ohne solche
Zellen, beim Ausschalten und beim Start (Aufräumen) entfernt titand Sprung und
Kette wieder. Läuft Docker noch nicht, gibt es `DOCKER-USER` nicht: titand
meldet das auf der Seite (`dockerError`) und versucht es alle 15 Sekunden
erneut; fehlt nach einem Docker-Neustart der Sprung, wird er wieder gesetzt.
Achtung: Ein so geöffnetes Paar umgeht auch Dockers Isolation von
`--internal`-Netzen. Die Kette wird mit einem `iptables-restore --noflush`
je Adressfamilie in einem Schritt ersetzt (geprüft mit echtem `iptables-nft`
in einem Wegwerf-Namespace: Ersetzen und Entfernen).

Apps im Host-Netzwerk veröffentlichen keine Ports; die Seite markiert sie, ihre
Ports gibt man mit Port-Richtlinien in der Zelle zum NAS frei.

## IPv64: veröffentlichte Apps und v64 Blocklist

- **„Öffentlich über IPv64“** im App-Editor (`docs/IPV64.md`): Ist die Firewall
  eingeschaltet, lässt TitanOS auf Wunsch (Häkchen, ab Werk an) die
  veröffentlichten Ports **genau dieser App** aus der Zone Internet zu
  (`Manager.SetAppZones`: setzt die Richtlinie dieser App in den Zellen
  Heimnetz/VPN/Internet → Docker-Apps bzw. hängt sie an, ohne Probezeit: die Ports einer App tragen nie die
  Weboberfläche, niemand kann sich damit aussperren; während eine andere
  Änderung auf Probe läuft, wird abgelehnt). Die vorige Einstellung wird
  gemerkt und beim Zurücknehmen wiederhergestellt. Ist die Firewall aus, ändert
  sich nichts. IPv64 dokumentiert keine Quelladressbereiche des CDN; die
  Freigabe gilt deshalb für die ganze Zone Internet, nur für die Ports der App.
  Die Ports der Weboberfläche (und 22, 8750) werden nie veröffentlicht.
- **v64 Blocklist** (ab Werk aus, Einstellungen › Dynamisches DNS › IPv64 › Blocklist): titand
  holt die Liste höchstens einmal am Tag (nur im RAM, höchstens 200.000
  Einträge, ohne private/VPN-Bereiche und Netze größer als /16 bzw. /32) und legt
  sie als Mengen `blocklist4`/`blocklist6` in die Tabelle. Verworfen wird nur in
  der Zone Internet (`internet` und `app_internet`), und nur, solange die
  Firewall eingeschaltet ist.

## Einstellungsdatei, Migration und Rückweg

`/config/firewall/settings.json` hat seit den Zonen zwei Teile:

- oben die Felder der Version 1 (`enabled`, `services`, `apps`, `newApps`,
  `rules`, `autoBlock`) – für einen **älteren titand** nach einem A/B-Rückfall
  eine Sicht der eingebauten Zonen Heimnetz/VPN/Internet: Dienste und Apps wie
  ihre Zellen sie erlauben, Port-Richtlinien der Zellen zum NAS als eigene
  Regeln dieser Zone (höchstens 100). Eigene Zonen, Zone Docker und Zellen
  zwischen Zonen kennt er nicht; deren Verkehr ordnet er wie früher nach
  Adresse zu. Damit sich niemand durch den Rückweg aussperrt, gilt die
  Weboberfläche dort aus Heimnetz und VPN als erlaubt, sobald eine eigene Zone
  sie erlaubt;
- unter `v2` die Zonen und Zellen.

Ein älterer titand liest unbekannte Felder nicht und schreibt beim Speichern nur
seine eigenen: `v2` verschwindet dann, und der neue titand übernimmt beim
nächsten Start die Einstellungen des älteren (statt veralteter Zonen). Eine
Datei der Version 1 wird beim Laden **migriert**, ohne sie zu schreiben:
eigene Regeln kommen in Reihenfolge an den Anfang jeder Zelle ihrer Zone (zum
NAS und zu den Apps; ohne Zone in jede), danach die erlaubten Dienste bzw. die
Apps mit eigener Einstellung, Vorgabe „sperren“ bzw. „Neue Apps“. War die
Firewall eingeschaltet, bekommt die Zone Docker die Zellen des Heimnetzes
(Docker-Netze zählten bisher als Heimnetz) und in alle Zonen „erlauben“, auch
untereinander (bisher filterte nichts den Verkehr aus Docker-Netzen, siehe
oben) – das Verhalten bleibt also gleich. Wer Docker-Netze trennen will, setzt
die Zelle „Docker → Docker“ auf „sperren“. Der Golden-Test vergleicht den migrierten Regelsatz mit dem
der Version 1 (`testdata/enabled.nft`).

## Nicht gefiltert

VMs und LXC-Container an `br0` sind eigene Geräte im Netz. Auch wenn Docker
`br_netfilter` lädt und überbrückte Pakete dadurch den Forward-Hook passieren,
behandelt die Kette `forward` nur von Docker umgesetzte (DNAT) Verbindungen und
neue Verbindungen aus Netzen hinter dem NAS in einer Zone (Docker, VM/LXC-NAT);
alles andere – auch Tailscale-/NetBird-Routing und Exit-Node-Verkehr – lässt sie
durch. Ausgehender Verkehr des NAS selbst wird nicht gefiltert.

## Auf Probe und Rückfall

Einschalten und jede Änderung im eingeschalteten Zustand laufen wie
Netzwerkänderungen zwei Minuten auf Probe: Die neuen Regeln sind sofort aktiv,
zusätzlich ist die speichernde Adresse ganz freigegeben. Ohne „Regeln behalten“
innerhalb von zwei Minuten lädt titand die vorherigen Regeln (Meldung
`firewall_reverted`, Aktivitätseintrag). Erst die Bestätigung schreibt
`/config/firewall/settings.json` und lädt die Regeln ohne Ausnahme.
Bestätigen geht nur in einer angemeldeten Websitzung: API-Tokens und
MCP-Clients erreichen `/firewall/confirm` nicht. Lässt eine Änderung die
Weboberfläche für die Adresse der speichernden Sitzung nicht mehr zu (Zelle ihrer
Zone zum NAS, auch einer eigenen Zone, zu der ihr Netz gehört), lehnt die API sie mit `firewall_excludes_you` ab – kein
Bestätigungsdialog, kein Aussperren. Ausschalten gilt sofort und ohne Probe;
im ausgeschalteten Zustand gespeicherte Einstellungen gelten beim Einschalten.
Ein titand-Neustart während der Probe lädt den bestätigten Stand.

Über den MCP-Server (Werkzeuge `firewall_status`, `firewall_rule_add`,
`firewall_rule_remove`, `firewall_unblock`; Token-Bereich `system:write`, nur
Besitzer/Administratoren) ändern KI-Clients nur Regeln
(`firewall.Manager.ChangeInProcess`): Sie können die Firewall weder ein- noch
ausschalten (`firewall_switch_manual`) noch eine Änderung vorschlagen, die die
Weboberfläche für Heimnetz oder VPN ohne eigene Absenderregel schließt
(`firewall_closes_webui`). Die ändernden Werkzeuge sind gefährlich: Sie laufen
nur, wenn das Werkzeug in `/config/system/mcp.json` auf „all“ steht und der
Client `confirm: true` schickt. Ein MCP-Aufruf hat keine Adresse einer
Websitzung, deshalb gibt es keine freigegebene Adresse auf Probe; bestätigen
muss eine Person in der Weboberfläche, sonst geht die Änderung zurück. Siehe
`docs/MCP.md`.

Rückweg ohne Weboberfläche: lokale Konsole, `nft delete table inet titanos`
(öffnet bis zum nächsten Start von titand alles) oder in
`/config/firewall/settings.json` `"enabled": false` setzen. Hat titand
Docker-Netze füreinander geöffnet: `iptables -D DOCKER-USER -j TITANOS-FWD`.

## Automatisch sperren

Wie Synologys „Auto Block“, **ab Werk aus** und unabhängig vom Hauptschalter
(bei ausgeschalteter Firewall enthält die Tabelle dann nur die Sperrliste):

- N Fehlversuche (2–100, Vorgabe 10) innerhalb von M Minuten (Vorgabe 5) sperren
  den Absender für eine Dauer (Vorgabe 60 Minuten) oder dauerhaft.
- IPv6-Absender werden als /64 gezählt und gesperrt.
- Ausnahmen: „Heimnetz und VPN nie sperren“ (Vorgabe an), eine Liste von Adressen
  und Netzen (höchstens 64), Loopback immer. Später ausgenommene Netze werden
  sofort aus den Regeln genommen.
- Gesperrt heißt: gar kein Zugang zum NAS und zu veröffentlichten App-Ports, auch
  bestehende Verbindungen enden. Liste mit Anlass, Zeitpunkt und Ende; „Freigeben“
  mit einfachem Bestätigungsdialog.
- Meldung `firewall_blocked`, Aktivitätseintrag `firewall_block`.
- Dauerhafte Sperren stehen in `/config/firewall/blocked.json` (höchstens einmal
  je Minute geschrieben), zeitlich begrenzte nur im RAM und in
  `/run/titanos/firewall-blocks.json` (überstehen einen titand-Neustart, keinen
  NAS-Neustart). Höchstens 4096 Sperren; ist die Liste voll, weicht die
  zeitlich begrenzte, die zuerst endet.
- Einstellungen werden sofort gespeichert und angewendet (keine Probe, sie können
  niemanden aussperren, der nicht schon gesperrt ist).

Gezählt werden:

| Quelle | Woher |
|---|---|
| Weboberfläche | Anmelde-Handler von titand: falsches Passwort oder falscher Zweitfaktor |
| SSH | Journal von sshd (`SYSLOG_IDENTIFIER=sshd-session`/`sshd`): `Invalid user`, `Failed … for`, `not allowed because`, `Connection closed by authenticating user … [preauth]`, `Too many authentication failures` |
| SMB | Journal von `smbd.service`: `auth_audit` auf Stufe 2 (`log level = 1 auth_audit:2` in der erzeugten `smb.conf`), Status `NT_STATUS_WRONG_PASSWORD`, `NT_STATUS_NO_SUCH_USER`, `NT_STATUS_LOGON_FAILURE` |

NFS kennt keine Anmeldung. Bei Freigaben mit Gastzugriff werden unbekannte Namen
zum Gast und von Samba nicht als Fehlschlag gemeldet. Was passiert, während
titand nicht läuft, wird nicht nachgeholt. Die bestehende Bremse des
Anmeldeformulars (`auth.Limiter`) bleibt unabhängig davon bestehen.

## Zuletzt gesperrt

Die letzten Verwerfungen jeder Zone protokolliert nftables mit
`limit rate 5/second` und dem Präfix `titanos-fw <zone>` (zu Apps
`app-<zone>`, zwischen Zonen `fwd-<zone>` mit Ziel und ausgehendem Anschluss)
ins Kernel-Log; titand
liest sie aus dem Journal (RAM) und hält die letzten 200 nur im Arbeitsspeicher
(Zeit, Absender, Zone, Protokoll, Port, Anschluss). Broadcasts und Multicasts
anderer Geräte werden ohne Protokoll verworfen. Bei veröffentlichten App-Ports
loggt der Kernel den Port nach der Umsetzung, also den Port im Container.

## Technik

- Eigene Tabelle `inet titanos`, erzeugt aus den Einstellungen, geladen mit
  `nft -f -` in einer Transaktion (`add table` + `delete table` + neue Tabelle):
  nie ein halber Regelsatz. `nftables.service` bleibt maskiert,
  `/etc/nftables.conf` wird nie geladen; das Paket `nftables` liefert nur `nft`.
- In nftables beendet ein `accept` die Prüfung nur in der eigenen Tabelle; ein
  `drop` in irgendeiner Basiskette verwirft das Paket. Deshalb bleiben die offenen
  iptables-Ketten von `titanos-firewall-open`, Dockers NAT und Isolation, die
  Hooks von Tailscale (`ts-input`/`ts-forward`) und NetBirds eigene Tabellen
  `netbird` unverändert bestehen und funktionieren weiter; TitanOS fasst keine
  fremde Tabelle an. Einzige Ausnahme: der Sprung in die eigene Kette
  `TITANOS-FWD` in `DOCKER-USER`, nur solange eine Zelle Docker-Netze
  füreinander öffnet (siehe oben).
- titand liest `/config` nur beim Start und bei Änderungen; die Prüfung alle 15
  Sekunden (Schnittstellen, Netze, Docker-Ports und -Netze, ablaufende Sperren,
  Tabelle und `DOCKER-USER`-Sprung noch vorhanden – sonst neu laden) arbeitet
  nur mit RAM, Kernel und Docker-Socket. Ein neues Docker-Netz ist damit
  spätestens nach 15 Sekunden in seiner Zone.
- Beim Start lädt titand die Regeln direkt nach dem Netzwerk; bis die erste
  Docker-Abfrage vorliegt (gleich nach dem Start von `Run`), gilt für alle
  App-Ports die Vorgabe der Zelle und Docker-Netze sind noch keiner Zone
  zugeordnet (nach Adresse: Heimnetz).
- Im Entwicklungsmodus (`titand -dev`) wird nichts geladen.
- Ketten: `input` → `<zone>`/`drop_<zone>` (zum NAS), `forward` → `published` →
  `app_<zone>`/`drop_app_<zone>` (Docker-Apps) und `forward` → `routed` →
  `from_<zone>` → `fwd_<quelle>_<ziel>`/`drop_fwd_<quelle>` (zwischen Zonen,
  nur wenn es Netze hinter dem NAS gibt). Ketten gibt es nur für Zonen mit
  Netzen oder Quellnetzen.

## Geprüft

- Go-Tests (`backend/internal/firewall`): Golden-Dateien des erzeugten
  Regelsatzes (`testdata/*.nft`), Zonenzuordnung einschließlich öffentlicher
  IPv6-Adressen innerhalb und außerhalb des eigenen Präfixes, Prüfung eigener
  Regeln, Probe mit Rückfall/Bestätigung/Zurücknehmen, Ablehnung bei
  Selbstaussperrung, `AllowHTTP`, Neuladen bei geänderten Schnittstellen und
  Docker-Ports, Zählen/Ausnahmen/Ablauf/Dauerhaft/Freigeben der automatischen
  Sperre, Lesen der Journal-Zeilen von sshd, Samba und Kernel. Seit den Zonen:
  Migration v1 → v2 (Golden-Vergleich mit dem Regelsatz der Version 1,
  `migrated.nft`), Rückweg-Sicht für ältere titand und erneute Migration nach
  deren Speichern, eigene Zonen/Docker-Netze/Zone-zu-Zone (`zones.nft`),
  Zuordnung nach Subnetz, Aussperr-Schutz mit eigener Zone, `DOCKER-USER`
  (nur eigene Kette und Sprung, Docker noch nicht da, Sprung verloren,
  Ausschalten), neue Docker-Netze im Abgleich, Liste der Docker-Netze
  (`internal/docker`).
- `TestRealNft` lädt die erzeugten Regelsätze mit dem echten `nft` in einen
  eigenen User- und Netzwerk-Namespace (`unshare -rn`) und prüft damit, dass der
  Kernel sie annimmt; ohne `nft`/Namespaces wird der Test übersprungen.
- API-Tests (Rechte, Fehlercodes, Token-Grenzen), Appproxy-Test der getrennten
  Freigabe von Weboberfläche und HTTPS-Namen, Webtests der Hilfsfunktionen und
  Übersetzungen.

**In QEMU geprüft** (28.10.2026, `scripts/smoke-firewall.py`, Ergebnis
`docs/validation/release-0.31.0-qemu-2026-10-28.json`): echtes Abbild aus
`make zip`, zwei UEFI-Starts mit Usermode-Netz, Host aus Sicht des Gasts im
Heimnetz (10.0.2.2), ein zweiter Client in einem Netzwerk-Namespace des Gasts
(veth, 192.168.77.2). Ab Werk aus, keine Tabelle, `titanos-firewall-open`
vorne; Einschalten auf Probe (Regel „admin on trial“ für den Host) und
Bestätigen; Weboberfläche und SSH vom Host danach erreichbar. SMB im Heimnetz
gesperrt: vom Namespace verworfen statt abgewiesen, über hostfwd keine Antwort,
Eintrag in „Zuletzt gesperrt“; Zurücknehmen von Hand und nach 120 s von selbst
mit Meldung `firewall_reverted`. Drei falsche Web-Anmeldungen aus dem Namespace
sperren ihn (Weboberfläche und Ping verworfen, Host weiter erreichbar),
„Freigeben“ hebt das auf. Nach Neustart wieder an, Ausschalten entfernt die
Tabelle, die offenen iptables-Regeln bleiben vorn.

**Nicht geprüft** (weder in QEMU noch auf echter Hardware): eigene Zonen mit
echten VLANs, Docker-Netze als Zone mit echten Containern (Netz A → B gesperrt
bzw. nach Freigabe über `DOCKER-USER` erlaubt, Container → Internet gesperrt,
Container → NAS-Dienst nach Zelle), VM/LXC-NAT-Bridges, die Zone Internet
mit echten öffentlichen Absendern, Docker-Portweiterleitungen mit IPv4/IPv6 und userland-proxy,
Zusammenspiel mit laufendem Tailscale/NetBird, die festen NFS-Hilfsports mit
einem NFSv3-Client, die Meldezeilen echter sshd-/Samba-Versionen (nur
nachgebildete Zeilen) und die Kernel-Logzeilen in journald.
