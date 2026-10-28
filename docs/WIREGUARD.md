# WireGuard-Server

Seit 0.34.0 hat TitanOS einen eigenen, möglichst einfachen VPN-Server:
Einstellungen › Netzwerk und Zugriff › **WireGuard** (nur Besitzer und
Administratoren), ab Werk aus.

## Bedienung

1. **WireGuard-Server einschalten** und die **Adresse aus dem Internet**
   eintragen (Name oder IP ohne Port; vorgeschlagen wird der erste Name aus
   Dynamisches DNS). Speichern.
2. Am Router **UDP 51820** an das NAS weiterleiten. Ist die Firewall
   eingeschaltet, lässt sie den Port aus dem Internet ab Werk zu (Dienst
   „VPN-Tunnel“).
3. **Gerät hinzufügen** mit einem Namen. TitanOS zeigt einen QR-Code für die
   WireGuard-App (iPhone, Android) und bietet die Datei zum Speichern an
   (Windows, macOS, Linux). **Nur jetzt**: Der private Schlüssel des Geräts
   wird auf dem NAS erzeugt und nicht gespeichert.

**Zugriff:** „NAS und Heimnetz“ (Vorgabe; `AllowedIPs` = Tunnel-Netz und die
privaten IPv4-Netze des NAS, das Internet läuft weiter direkt über das Gerät)
oder „Alles“ (`0.0.0.0/0, ::/0`, der gesamte Verkehr läuft über das NAS; IPv6
der Geräte geht dann nicht ins Internet, weil der Tunnel nur IPv4 hat).
Optional ein DNS-Server für die Geräte, etwa der Router.

Die Geräteliste zeigt Tunnel-Adresse, „verbunden“ (Handshake in den letzten
drei Minuten), letzten Kontakt und Datenmenge. Entfernen sperrt das Gerät
sofort aus.

## Technik

- Paket `wgserver`, Schnittstelle `titanoswg`, `wireguard-tools` (seit 0.31.0
  im Abbild) und das WireGuard-Modul des Debian-Kernels.
- `/config/wireguard/server.json`: Schalter, Adresse, Port (Vorgabe 51820),
  Tunnel-Netz (beim ersten Einschalten das erste von `10.253.0.0/24`,
  `10.254.13.0/24`, `172.31.253.0/24`, `192.168.253.0/24`, das kein Netz des NAS
  überlappt; mit Geräten nicht mehr änderbar), Zugriff, DNS und die Geräte
  (Name, öffentlicher Schlüssel, Preshared Key, Adresse). Schlüssel des
  Servers in `/config/wireguard/server.key`. Geschrieben nur beim Speichern,
  Hinzufügen und Entfernen; der Ordner ist im Stick-Dateimanager ausgeblendet
  und nicht im Diagnosepaket.
- Bei jedem Start und jeder Änderung: `ip link add … type wireguard`,
  `wg syncconf` aus einer Datei in `/run/titanos-wireguard` (0600, sofort
  wieder gelöscht; Schlüssel nie auf der Kommandozeile), erste Adresse des
  Tunnel-Netzes, IPv4-Weiterleitung an, NAT (`MASQUERADE`) in eigener Kette
  `TITANOS-WG` mit Sprung aus `POSTROUTING`, damit Router und Heimnetz keine
  Route zurück brauchen. Ausschalten entfernt Schnittstelle und Kette; die
  Geräte bleiben gespeichert.
- Firewall: `titanoswg` gehört zur Zone VPN (wie `tailscale0`, `wt0`).
- Status (`wg show titanoswg dump`) nur beim Öffnen der Seite.
- API: `GET`/`PUT /api/v1/network/wireguard`,
  `POST /api/v1/network/wireguard/peers` (Antwort mit Konfiguration,
  `Cache-Control: no-store`), `POST /api/v1/network/wireguard/peers/remove`.

## Prüfstand

Mit nachgebautem `ip`/`wg`/`iptables` geprüft (`wgserver_test.go`,
`api/wireguard_test.go`): Netzwahl, Befehle, kein Schlüssel auf der
Kommandozeile oder in `/run`, Gerätekonfiguration (geteilter und voller
Tunnel, IPv6-Endpunkt, DNS), privater Schlüssel des Geräts nirgends
gespeichert, Status aus `wg show dump`, Entfernen, Ausschalten,
Entwicklungsmodus, Rechte. **Nicht geprüft:** echter Tunnel mit der
WireGuard-App, NAT neben Docker, Zusammenspiel mit eingeschalteter Firewall.
