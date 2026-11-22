# WireGuard-Server

Seit 0.34.0 hat TitanOS einen eigenen, möglichst einfachen VPN-Server:
Einstellungen › Sicherheit und Fernzugriff › **WireGuard** (nur Besitzer und
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

## Zugang der Android-App (seit 1.19)

Die App „TitanOS“ legt je Handy selbst einen Zugang an (PLAN 116, Inhaber
21.11.2026): Der Schlüssel entsteht auf dem Handy, das NAS bekommt nur den
öffentlichen Teil (`POST /api/v1/app/vpn`, `docs/ANDROID-APP.md` „VPN“). Der
Zugang steht in der Geräteliste wie jedes andere Gerät, mit „Android-App von
<Konto>“, und lässt sich dort auch entfernen. Er endet mit der App:

- „Beenden“ unter Mein Konto › Geräte, neues Passwort oder 2FA des Kontos,
  Konto gelöscht: sofort aus `server.json` und vom Tunnel.
- Konto gesperrt oder nicht mehr Besitzer/Administrator: ebenso.
- Abgelaufen nach „Angemeldet bleiben“: titand prüft stündlich **nur im
  RAM** und nimmt den Zugang vom Tunnel (Liste: „gerade nicht im Tunnel“);
  aus `server.json` geht er beim nächsten Start oder beim nächsten Ereignis
  oben. Ein Zeitgeber schreibt nie auf den Stick und liest `/config` nicht.
- Sind die Apps eines Kontos gerade nicht lesbar (Stick), bleibt der Zugang.

Ein Gerät je App; „neu erzeugen“ in der App ersetzt Schlüssel und Preshared
Key an derselben Adresse. Anlegen dürfen wie hier nur Besitzer und
Administratoren.

## Technik

- Paket `wgserver`, Schnittstelle `titanoswg`, `wireguard-tools` (seit 0.31.0
  im Abbild) und das WireGuard-Modul des Debian-Kernels.
- `/config/wireguard/server.json`: Schalter, Adresse, Port (Vorgabe 51820),
  Tunnel-Netz (beim ersten Einschalten das erste von `10.253.0.0/24`,
  `10.254.13.0/24`, `172.31.253.0/24`, `192.168.253.0/24`, das kein Netz des NAS
  überlappt; mit Geräten nicht mehr änderbar), Zugriff, DNS und die Geräte
  (Name, öffentlicher Schlüssel, Preshared Key, Adresse; bei der App dazu
  `account` und `device`, sonst fehlen die Felder). Schlüssel des
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
  Für die App `GET`/`POST`/`DELETE /api/v1/app/vpn` (nur App-Sitzungen).

## Prüfstand

Mit nachgebautem `ip`/`wg`/`iptables` geprüft (`wgserver_test.go`,
`api/wireguard_test.go`): Netzwahl, Befehle, kein Schlüssel auf der
Kommandozeile oder in `/run`, Gerätekonfiguration (geteilter und voller
Tunnel, IPv6-Endpunkt, DNS), privater Schlüssel des Geräts nirgends
gespeichert, Status aus `wg show dump`, Entfernen, Ausschalten,
Entwicklungsmodus, Rechte. Zugang der App (`apppeers_test.go`,
`api/appvpn_test.go`): Anlegen, neu erzeugen, Entfernen, fremder Schlüssel,
Name neben einem gleichnamigen Gerät, Ende mit App, Rolle, Sperre, Löschen,
Passwortwechsel, Ablauf (nur RAM, auch der Tunnel bekommt nur den Stand aus
dem RAM), Stick nicht lesbar. Zugang der App im Emulator (22.11.2026, App
`feat/1.00-vpn`): Handshake und Verkehr gegen wireguard-go im Userspace
(gVisor-netstack, Schlüssel und Geräte aus `server.json`/`server.key` des
Test-NAS), nicht gegen `titanoswg`. **Nicht geprüft:** echter Tunnel über
`titanoswg` (Kernel, `wg syncconf`, im Test-NAS ist `Disabled` gesetzt; damit
auch nicht live, dass ein abgelaufener Zugang aus dem Tunnel fällt), mit der
WireGuard-App, NAT neben Docker, Zusammenspiel mit eingeschalteter Firewall.
