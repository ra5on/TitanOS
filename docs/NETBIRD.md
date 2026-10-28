# NetBird

Das optionale Plugin wird unter **Plugins → NetBird** auf dem gewählten
Plugin-Volume installiert. Nach dem Aktivieren zeigt dieselbe Ansicht den
Verbindungszustand, NetBird-Namen (FQDN), NetBird-Adressen, den
Management-Server und die Zahl der verbundenen Geräte. Angemeldet wird das NAS
mit einem **Setup-Key** aus der NetBird-Verwaltung; eine interaktive
SSO-Anmeldung im Browser bietet TitanOS nicht an.

- **Setup-Key:** wird einmal eingefügt, in `/config/plugins/netbird/settings.json`
  (nur root) gespeichert und über die API nie wieder herausgegeben; die Seite
  zeigt nur „gespeichert“ oder „nicht gespeichert“. Ein leeres Feld behält den
  Key, **Gespeicherten Key entfernen** löscht ihn. An `netbird` wird er
  ausschließlich über die Standardeingabe übergeben
  (`--setup-key-file=/dev/stdin`), nie auf einer Kommandozeile oder in einer
  Datei im RAM. Er wird nur gebraucht, solange das NAS noch nicht angemeldet ist
  (Zustand „Anmeldung erforderlich“) oder ein anderer Management-Server gewählt
  wird. Wiederverwendbare Keys sollten in NetBird eine Ablaufzeit haben.
- **Management-Server:** leer bedeutet NetBird Cloud
  (`https://api.netbird.io:443`). Ein eigener Server wird nur mit Schema,
  Hostname und optionalem Port angegeben (`http`/`https`, ohne Pfad, Anmeldedaten
  oder Parameter). Ein anderer Server verlangt einen gespeicherten Setup-Key von
  dort.
- **Routen anderer NetBird-Geräte annehmen** (`--disable-client-routes`),
  **Dieses NAS als Routing-Peer bzw. Exit Node zulassen**
  (`--disable-server-routes`) und **DNS-Einstellungen übernehmen**
  (`--disable-dns`) sind anfangs aus. Welche Netze, Exit Nodes und
  Zugriffsregeln es gibt, wird in der NetBird-Verwaltung festgelegt; TitanOS
  legt dort nichts an.
- Der Zugang zum lokalen Netzwerk bleibt immer erhalten
  (`--block-lan-access=false`, `--block-inbound=false`). NetBird-SSH,
  Fernaufträge des Management-Servers (`--allow-remote-jobs=false`) und
  Rosenpass bleiben aus. Selbstständige Updates gibt es unter Linux nicht; der
  Client kommt ausschließlich über das signierte TitanOS-Plugin.
- Änderungen an Management-Server, Routen und DNS gelten zwei Minuten auf Probe.
  **Zugriff funktioniert – speichern** speichert sie dauerhaft; ohne Bestätigung
  oder mit **Änderungen zurücknehmen** kehrt TitanOS zur vorherigen Auswahl
  zurück, auch ohne geöffneten Browser. Ist das NAS verbunden, wird dafür die
  Verbindung mit den neuen bzw. alten Optionen neu aufgebaut (`netbird down` und
  `netbird up`), denn NetBird übernimmt geänderte Optionen nicht in eine
  bestehende Verbindung. Eine fehlgeschlagene Rücknahme bleibt sichtbar und wird
  erneut versucht. Plugin-Änderungen und Systemupdates sind während der Probe
  gesperrt.
- **Verbinden**, **Verbindung trennen** und **NAS aus dem NetBird-Netz
  entfernen** (`netbird deregister`, mit Bestätigungsdialog) merken sich, ob das
  NAS verbunden sein soll (`connect` in derselben Datei). Der NetBird-Dienst
  verbindet sich nie selbst (`--disable-auto-connect`): nach einem Neustart
  baut TitanOS die Verbindung mit der bestätigten Auswahl im Hintergrund wieder
  auf. Ein nicht erreichbarer Management-Server lässt die Plugin-Aktivierung
  deshalb nicht scheitern, sondern erscheint als Hinweis. Nach einem bloßen
  titand-Neustart bleibt eine bestehende Verbindung mit unveränderten Optionen
  unangetastet.

## Dashboard-Kachel

Besitzer und Administratoren können im Dashboard-Editor die Kachel **NetBird**
hinzufügen. Sie zeigt die Geräte des NetBird-Netzes einschließlich dieses NAS.
Je Kachel lassen sich Geräte (über den öffentlichen WireGuard-Schlüssel des
Geräts, nie über die IP) und sortierbare Angaben wählen: verbunden/nicht
verbunden, Markierung dieses NAS, NetBird-IP (IPv4), NetBird-IPv6, Name (FQDN),
letzter Handshake, Verbindungsart (direkt/P2P oder über Relay) und Netze.
Optional zeigt sie nur verbundene Geräte. Die Auswahl steht in `tileOptions`
(`items`, null = alle einschließlich neuer, `netbirdFields`, `runningOnly`).

Die Daten liefert `GET /api/v1/plugins/netbird/peers` aus
`netbird status --json`, höchstens alle fünf Sekunden aus einem RAM-Cache, ohne
Zugriff auf `/config` und im Entwicklungsmodus ohne Aufruf. Ohne aktives Plugin
meldet die Kachel „nicht aktiv“. NetBird meldet kein Betriebssystem der Geräte;
die Kachel bietet es deshalb nicht an. „Verbunden“ heißt, dass ein Tunnel zum
Gerät besteht: Geräte, die NetBird nur bei Bedarf verbindet (Lazy Connections),
erscheinen im Ruhezustand als nicht verbunden. Ohne verbundenen Client sind die
Zustände der anderen Geräte unbekannt und bleiben leer; „letzter Handshake“ gibt
es nur für verbundene Geräte. Ältere App-Versionen verwerfen die unbekannte
Kachel beim Lesen.

## API

Nur Besitzer und Administratoren; Änderungen laufen über die Plugin-Sperre
(`RuntimeAction("netbird", …)`).

| Route | Zweck |
| --- | --- |
| `GET /api/v1/plugins/netbird` | Einstellungen, `setupKeySet`, Zustand |
| `PUT /api/v1/plugins/netbird` | Management-Server, Routen und DNS auf Probe |
| `PUT /api/v1/plugins/netbird/setup-key` | Setup-Key speichern (leer = entfernen) |
| `POST /api/v1/plugins/netbird/control/{connect,disconnect,logout,confirm,undo}` | Verbindung und Probe |
| `GET /api/v1/plugins/netbird/peers` | Geräte für die Dashboard-Kachel |

Der KI-Assistent kennt nur das Lesen und das Ändern der Einstellungen auf Probe,
nicht den Setup-Key.

## Ablage und Netzwerk

NetBirds privater WireGuard-Schlüssel, Profile und Zustand liegen ausschließlich
auf dem Daten-Volume in `.titanos/netbird/` (root, Modus 0700; der Dienst erhält
`NB_STATE_DIR`), nicht im RAM-Overlay oder auf dem Bootstick. Deaktivieren und
Entfernen des Plugins löschen diesen Zustand nicht. Das reine Stick-Backup
enthält ihn deshalb nicht, wohl aber den Setup-Key. Protokolle gehen ins
Journal (`--log-file console`).

`titanos-netbird.service` startet nach dem Einbinden des Plugin-Volumes und nach
`titanos-firewall-open.service`. Der Client nutzt das Interface `wt0` und
UDP 51820.

**Firewall:** NetBird erkennt unter Debian 13 das nftables-Backend (iptables ist
dort `iptables-nft`) und legt eigene Tabellen `netbird` (IPv4 und IPv6) mit
Basis-Ketten für Eingang, Weiterleitung, NAT und Markierungen an. Deren
Zugriffsregeln gelten nur für Pakete von `wt0` und greifen unabhängig von der
global offenen TitanOS-Freigabe: In nftables beendet ein `accept` in einer
anderen Tabelle die Prüfung nicht, ein `drop` der NetBird-Tabelle wirkt immer.
Zusätzlich setzt NetBird `-i wt0 -j ACCEPT` an den Anfang von `INPUT`/`FORWARD`
der iptables-Tabelle `filter`; `titanos-firewall-open` stellt seine Freigabe
davor, was nichts ändert, weil beide annehmen. Docker-NAT, `DOCKER-USER` (bis
auf den Sprung der Firewall in ihre Kette `TITANOS-FWD`, siehe
[Firewall](FIREWALL.md)) und `nftables.service` (maskiert) bleiben unberührt; NetBird entfernt seine Tabellen
beim Beenden selbst.

Ein eigener Hook ist nur für das Zusammenspiel mit Tailscale nötig: Tailscales
`ts-input` verwirft Absender aus 100.64.0.0/10, die nicht über `tailscale0`
kommen, und NetBird verwendet denselben Bereich. `titanos-netbird-firewall`
setzt deshalb `-i wt0 -m comment --comment titanos-netbird -j ACCEPT` vor die
Tailscale-Hooks in `INPUT` (IPv4 und IPv6). Der Hook läuft beim Dienststart und
am Ende von `titanos-tailscale-firewall`, also auch nach jedem Docker-/
Netzwerkstart über `titanos-firewall-open`; beim Stoppen wird die Regel
entfernt. Weitergeleitete Pakete von `wt0` passieren `ts-forward` ohne Treffer
und brauchen keine Regel. Einzelne Adressen beider Netze können sich im
gemeinsamen Bereich überschneiden; dann den NetBird-Netzbereich in der
NetBird-Verwaltung ändern. Die optionale [Firewall](FIREWALL.md) fasst NetBirds
Tabellen nicht an, zählt `wt0` zur Zone VPN, lässt UDP 51820 ab Werk aus allen
Zonen zu (Dienst „VPN-Tunnel“), lässt von NetBird umgeleitetes DNS (`wt0`,
`ct status dnat`) durch und filtert keinen ausgehenden Verkehr.

**Weiterleitung:** Ist „Routing-Peer bzw. Exit Node zulassen“ eingeschaltet,
schaltet `titanos-netbird-runtime` IPv4-/IPv6-Forwarding vor NetBird ein und
erhält bereits erlaubte IPv6-Router-Advertisements mit `accept_ra=2`. NetBird
stellt beim Beenden seine vorgefundenen Werte wieder her; weil Forwarding dann
bereits an war, bleibt es für Docker, VMs und Tailscale an. TitanOS schaltet es
nie ab. LAN-Adressen, Bridge und DHCP-Konfiguration bleiben unverändert.

## Build, Lizenz und Prüfstand

Der Build verwendet den NetBird-Client 0.81.5 aus dem offiziellen statischen
amd64-Archiv `netbird_0.81.5_linux_amd64.tar.gz` mit im Repository festgelegtem
SHA-256 und fester Größe (`plugins/netbird/packages.lock.json`). Das
NetBird-Repository ist gemischt lizenziert: Der Client steht unter
BSD-3-Clause, die Server-Komponenten `management/`, `signal/`, `relay/` und
`combined/` unter AGPL-3.0. Das Plugin enthält nur das Client-Binary; dessen
Build-Informationen enthalten keine Pakete aus diesen Verzeichnissen. Plugin und
`netbird-licenses.tar.gz` enthalten die originale `LICENSE` (mit der Aufteilung),
`BSD-3-Clause.txt`, die Hinweise aller eingebundenen Go-Module
(`third-party.txt`, darunter MPL-2.0-Module von HashiCorp mit Quelllink je Modul)
und `sources.txt`. `third-party.txt` erzeugt `scripts/netbird-notices.py` aus den
Build-Informationen des Binarys über den Go-Modul-Proxy (Hash je Modul gegen das
Binary geprüft); die Datei wird mit ihrem SHA-256 festgelegt, der Plugin-Build
braucht dafür keine Downloads. Bei einer neuen Client-Version Lock-Datei und
Hinweise gemeinsam erneuern. Plugin und Manifest werden im geschützten
Release-Job signiert.

Geprüft: API-Rollen und Entwicklungsmodus, Validierung von Server-Adresse und
Setup-Key, Key nur über stdin und nie in API-Antworten, Probe mit Neuverbindung,
Bestätigung, Rücknahme und Fehlerfällen, Wiederherstellung nach Start bzw.
titand-Neustart, Statusauswertung und Kachel mit nachgebauter
`netbird status --json`-Ausgabe (Format aus dem Quelltext von 0.81.5),
Browserprüfung von Einstellungen und Kachel mit isolierten Daten
(`scripts/test-ui-netbird.mjs`). `scripts/netbird_smoke.py` prüft im
Plugin-/Release-Workflow im Wegwerf-QEMU-NAS ohne NetBird-Konto Installation,
Dienststart, den Zustand „Anmeldung erforderlich“, Ablage, Hook-Reihenfolge,
Forwarding-Probe, Setup-Key und Entfernen. Nicht geprüft: eine echte Anmeldung
und Verbindung zu NetBird Cloud oder einem eigenen Server, Datenverkehr über
Routen und Exit Nodes, das Verhalten der nftables-Regeln mit echtem Verkehr und
der gleichzeitige Betrieb mit einem verbundenen Tailscale.

Primärquellen: [Releases](https://github.com/netbirdio/netbird/releases),
[CLI](https://docs.netbird.io/how-to/cli),
[Setup-Keys](https://docs.netbird.io/how-to/register-machines-using-setup-keys),
[Routing](https://docs.netbird.io/how-to/routing-traffic-to-private-networks),
Quelltext `client/firewall/nftables` und `client/status` des Tags v0.81.5.
