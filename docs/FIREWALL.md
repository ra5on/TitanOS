# Firewall

TitanOS hat einen eigenen Paketfilter unter **Einstellungen › Firewall** (nur
Besitzer und Administratoren). Er ist **ab Werk aus**; nach einem Update bleibt
alles so offen wie bisher (`titanos-firewall-open`, siehe
[Netzwerk](NETWORKING.md#firewall)), bis die Firewall in der Oberfläche
eingeschaltet wird. Paket `backend/internal/firewall`, API `/api/v1/firewall`.

## Zonen

Jede neue Verbindung wird nach ihrem Absender einer Zone zugeordnet:

| Zone | Absender |
|---|---|
| Heimnetz | private Bereiche (10/8, 172.16/12, 192.168/16, 169.254/16, `fc00::/7`, `fe80::/10`) und die Netze der eigenen Anschlüsse des NAS (Bridge `br0`, VLANs, Bonds), damit auch das öffentliche IPv6-Präfix des Heimnetzes |
| VPN | Schnittstellen `tailscale0`, `wt0` und `ipv64wg` (Tunnel zu einem IPv64 Cloud Router, `docs/IPV64.md`), Bereiche `100.64.0.0/10` und `fd7a:115c:a1e0::/48` (Tailscale, NetBird) |
| Internet | alles andere, ausdrücklich auch öffentliche IPv6-Adressen außerhalb des eigenen Präfixes |

Die Netze der eigenen Anschlüsse liest titand alle 15 Sekunden aus dem Kernel
(dieselbe Auswahl wie die Vorgabe „Heimnetz“ der SSH-Netzliste, ohne Docker-,
VM- und VPN-Schnittstellen). Ändert sich etwa das IPv6-Präfix des Providers,
werden die Regeln neu geladen. Präfixe kürzer als /8 zählen nie als Heimnetz.

## Dienste und Vorgaben

Eingeschaltet gilt die Matrix Zone × Dienst. Vorgabe (Entscheidung des
Projektinhabers): Heimnetz und VPN erlaubt, Internet gesperrt; die
Netzwerkumgebung nur aus dem Heimnetz.

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
Internet gesperrt ist: dann DNS-01 verwenden oder eine eigene Regel „TCP 80 aus
dem Internet“ anlegen ([Let's Encrypt](LETSENCRYPT.md)).

Abweichung von „nichts aus dem Internet“: die VPN-Tunnel. Ihre Gegenstellen
kommen naturgemäß aus dem Internet, das VPN prüft sie selbst kryptografisch,
und das Tailscale-Plugin verlangt UDP 41641 für direkte Verbindungen. Ohne
diese Freigabe funktionieren Tailscale und NetBird weiter, aber öfter nur über
Relays. Der Schalter lässt sich wie jeder andere abschalten.

Die festen NFS-Hilfsports setzt das Abbild in
`/etc/nfs.conf.d/titanos-ports.conf` und `/etc/modprobe.d/titanos-nfs.conf`
(lockd). NFSv4 braucht nur 2049.

**Weboberfläche und HTTPS-Namen** teilen sich Port 443. nftables lässt den Port
zu, wenn einer der beiden Dienste für die Zone erlaubt ist; titand entscheidet
danach am Namen (`appproxy.Manager.Gate` → `firewall.AllowHTTP`) und antwortet
sonst mit 403. Die eigenen erlaubten Netze einer App (Einstellungen › HTTPS-Namen
für Apps) gelten zusätzlich.

**Immer erlaubt:** bestehende Verbindungen (`ct state established,related`),
Loopback, DHCP-/DHCPv6-Antworten an das NAS, ICMP-Fehlermeldungen (auch Path
MTU), IPv6-Nachbar- und Router-Erkennung, MLD-Abfragen, von NetBird auf das NAS
umgeleitetes DNS seiner Peers (`wt0` mit `ct status dnat`) und – solange eine
Änderung auf Probe läuft – die Adresse der Websitzung, die sie gespeichert hat.

## Docker-Apps

Veröffentlichte Ports holt titand alle 15 Sekunden über die Docker-API (Compose-
Projekt bzw. `container:<name>`). Jede App hat eine eigene Zeile der Matrix; Apps
ohne eigene Einstellung folgen „Neue Apps“ (Vorgabe Heimnetz und VPN). So gilt
für eine neu installierte App sofort eine Regel, auch bevor titand ihre Ports
kennt.

Docker (Debian 13: Docker 26 mit `iptables-nft`) setzt veröffentlichte Ports
per DNAT in `nat/PREROUTING` um; die Pakete laufen dann durch den
**Forward-Hook**, nicht durch Input. TitanOS filtert sie deshalb in seiner
eigenen Kette `forward` nach `ct original proto-dst`, also nach dem Port auf dem
NAS vor der Umsetzung. `DOCKER-USER` wird nicht verwendet: es ist eine
iptables-Kette, die Docker nach eigenem Ermessen anlegt und umsortiert, während
die eigene nftables-Tabelle von Docker nie angefasst wird und mit einem Neustart
von Docker nicht verschwindet. IPv6-Ports und Verbindungen über Dockers
userland-proxy kommen über Input an; dieselben Ports stehen deshalb auch in den
Input-Ketten der Zonen.

Nicht gefiltert werden: ausgehende Verbindungen der Container, Container
untereinander, Apps mit eigener LAN-Adresse (macvlan/ipvlan) und Apps im
Host-Netzwerk. Letztere veröffentlichen keine Ports; die Seite markiert sie, ihre
Ports gibt man mit eigenen Regeln frei.

## IPv64: veröffentlichte Apps und v64 Blocklist

- **„Öffentlich über IPv64“** im App-Editor (`docs/IPV64.md`): Ist die Firewall
  eingeschaltet, lässt TitanOS auf Wunsch (Häkchen, ab Werk an) die
  veröffentlichten Ports **genau dieser App** aus der Zone Internet zu
  (`Manager.SetAppZones`, ohne Probezeit: die Ports einer App tragen nie die
  Weboberfläche, niemand kann sich damit aussperren; während eine andere
  Änderung auf Probe läuft, wird abgelehnt). Die vorige Einstellung wird
  gemerkt und beim Zurücknehmen wiederhergestellt. Ist die Firewall aus, ändert
  sich nichts. IPv64 dokumentiert keine Quelladressbereiche des CDN; die
  Freigabe gilt deshalb für die ganze Zone Internet, nur für die Ports der App.
  Die Ports der Weboberfläche (und 22, 8750) werden nie veröffentlicht.
- **v64 Blocklist** (ab Werk aus, Einstellungen › IPv64 › Blocklist): titand
  holt die Liste höchstens einmal am Tag (nur im RAM, höchstens 200.000
  Einträge, ohne private/VPN-Bereiche und Netze größer als /16 bzw. /32) und legt
  sie als Mengen `blocklist4`/`blocklist6` in die Tabelle. Verworfen wird nur in
  der Zone Internet (`internet` und `app_internet`), und nur, solange die
  Firewall eingeschaltet ist.

## Eigene Regeln

Erlauben oder Sperren, Protokoll (TCP, UDP, beides), Port oder Bereich, Absender
(Adresse oder Netz) und optional eine Zone; höchstens 100. Sie werden der Reihe
nach **vor** der Matrix geprüft, die erste passende entscheidet; sie gelten für
das NAS und für veröffentlichte Ports der Apps. Gesperrte Pakete erscheinen unter
„Zuletzt gesperrt“.

## Nicht gefiltert

VMs und LXC-Container an `br0` sind eigene Geräte im Netz. Auch wenn Docker
`br_netfilter` lädt und überbrückte Pakete dadurch den Forward-Hook passieren,
behandelt die Kette `forward` nur von Docker umgesetzte (DNAT) Verbindungen;
alles andere – auch Tailscale-/NetBird-Routing und Exit-Node-Verkehr – lässt sie
durch. Ausgehender Verkehr des NAS wird nicht gefiltert.

## Auf Probe und Rückfall

Einschalten und jede Änderung im eingeschalteten Zustand laufen wie
Netzwerkänderungen zwei Minuten auf Probe: Die neuen Regeln sind sofort aktiv,
zusätzlich ist die speichernde Adresse ganz freigegeben. Ohne „Regeln behalten“
innerhalb von zwei Minuten lädt titand die vorherigen Regeln (Meldung
`firewall_reverted`, Aktivitätseintrag). Erst die Bestätigung schreibt
`/config/firewall/settings.json` und lädt die Regeln ohne Ausnahme.
Bestätigen geht nur in einer angemeldeten Websitzung: API-Tokens und der
Assistent erreichen `/firewall/confirm` nicht. Lässt eine Änderung die
Weboberfläche für die Adresse der speichernden Sitzung nicht mehr zu (Matrix oder
eigene Sperrregel), lehnt die API sie mit `firewall_excludes_you` ab – kein
Bestätigungsdialog, kein Aussperren. Ausschalten gilt sofort und ohne Probe;
im ausgeschalteten Zustand gespeicherte Einstellungen gelten beim Einschalten.
Ein titand-Neustart während der Probe lädt den bestätigten Stand.

Rückweg ohne Weboberfläche: lokale Konsole, `nft delete table inet titanos`
(öffnet bis zum nächsten Start von titand alles) oder in
`/config/firewall/settings.json` `"enabled": false` setzen.

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
`limit rate 5/second` und dem Präfix `titanos-fw <zone>` ins Kernel-Log; titand
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
  iptables-Ketten von `titanos-firewall-open`, Dockers NAT/`DOCKER-USER`, die
  Hooks von Tailscale (`ts-input`/`ts-forward`) und NetBirds eigene Tabellen
  `netbird` unverändert bestehen und funktionieren weiter; TitanOS fasst keine
  fremde Tabelle an.
- titand liest `/config` nur beim Start und bei Änderungen; die Prüfung alle 15
  Sekunden (Schnittstellen, Docker-Ports, ablaufende Sperren, Tabelle noch
  vorhanden – sonst neu laden) arbeitet nur mit RAM, Kernel und Docker-Socket.
- Beim Start lädt titand die Regeln direkt nach dem Netzwerk; bis die erste
  Docker-Abfrage vorliegt (höchstens 15 Sekunden), gilt für alle App-Ports
  „Neue Apps“.
- Im Entwicklungsmodus (`titand -dev`) wird nichts geladen.

## Geprüft

- Go-Tests (`backend/internal/firewall`): Golden-Dateien des erzeugten
  Regelsatzes (`testdata/*.nft`), Zonenzuordnung einschließlich öffentlicher
  IPv6-Adressen innerhalb und außerhalb des eigenen Präfixes, Prüfung eigener
  Regeln, Probe mit Rückfall/Bestätigung/Zurücknehmen, Ablehnung bei
  Selbstaussperrung, `AllowHTTP`, Neuladen bei geänderten Schnittstellen und
  Docker-Ports, Zählen/Ausnahmen/Ablauf/Dauerhaft/Freigeben der automatischen
  Sperre, Lesen der Journal-Zeilen von sshd, Samba und Kernel.
- `TestRealNft` lädt die erzeugten Regelsätze mit dem echten `nft` in einen
  eigenen User- und Netzwerk-Namespace (`unshare -rn`) und prüft damit, dass der
  Kernel sie annimmt; ohne `nft`/Namespaces wird der Test übersprungen.
- API-Tests (Rechte, Fehlercodes, Token-Grenzen), Appproxy-Test der getrennten
  Freigabe von Weboberfläche und HTTPS-Namen, Webtests der Hilfsfunktionen und
  Übersetzungen.

**Nicht geprüft** (weder in QEMU noch auf echter Hardware): tatsächliches
Filtern im Abbild, Docker-Portweiterleitungen mit IPv4/IPv6 und userland-proxy,
Zusammenspiel mit laufendem Tailscale/NetBird, die festen NFS-Hilfsports mit
einem NFSv3-Client, die Meldezeilen echter sshd-/Samba-Versionen (nur
nachgebildete Zeilen) und die Kernel-Logzeilen in journald.
