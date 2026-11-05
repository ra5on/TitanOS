# Netzwerk, Firewall und Fernzugriff
_Mitgeliefert mit TitanOS, wird bei Updates ersetzt. Eigene Anleitungen gehören in den Ordner Wissen außerhalb von „TitanOS“._

Diese Anleitung erklärt, wie Netzwerkänderungen mit Rückfall funktionieren, was die Firewall tut und wie du sicher von unterwegs auf das NAS kommst.

## Was der Agent hier darf

- Lesen: `network_status`, `network_diagnostics`, `firewall_status`.
- Firewall-Regeln: `firewall_rule_add`, `firewall_rule_remove`, `firewall_unblock` nur mit deiner Freigabe.
- **Nicht:** Netzwerkeinstellungen ändern oder bestätigen, die Firewall ein- oder ausschalten, Tailscale/NetBird anmelden, Schlüssel oder Setup-Keys sehen. Das machst du in der Oberfläche, der Agent erklärt die Schritte.

## Netzwerk mit Rückfall

Einstellungen › Netzwerk. TitanOS ist kein Router und kein DHCP-Server; das bleibt dein Router.

1. Änderung vorbereiten (Adresse, DNS, VLAN, Bond, Bridge) und „Prüfen und testen“.
2. Die Änderung läuft **zwei Minuten auf Probe**.
3. Über die **neue** Verwaltungsadresse im Browser bestätigen. Erst dann wird gespeichert.
4. Ohne Bestätigung stellt TitanOS die vorige Fassung von selbst wieder her, auch ohne offenen Browser.
5. Bestätigen geht nur in einer echten Websitzung, nicht über den Agenten, MCP oder API-Tokens. Im Cluster nur auf dem Knoten selbst.
6. Notfall: An der NAS-Konsole öffnet **Strg+Alt+F2** die lokale Netzwerkwiederherstellung (Anmeldung mit Besitzer- oder Administratorkonto und ggf. 2FA). Dort lässt sich die vorige bestätigte Fassung oder DHCP auf einem Anschluss zurückholen.

Tipps:
- Feste Adressen am besten im Router reservieren (DHCP-Reservierung) statt am NAS.
- VLANs nur mit VLAN-fähigem Switch oder Router.
- Netze, die VMs, LXC oder Docker nutzen, lassen sich nicht einfach entfernen; vorher die Anwendungen umstellen.

## Firewall (ab Werk aus)

Einstellungen › Firewall, nur Besitzer und Administratoren.

- **Ab Werk aus.** Ausgeschaltet ist alles so offen wie ohne Firewall.
- **Zonen:** Heimnetz, VPN (Tailscale, NetBird, WireGuard, Cluster-Tunnel), Docker, Internet (auch öffentliches IPv6) und eigene Zonen.
- Eine **Matrix** legt fest, welche Zone welche Dienste des NAS und welche Docker-Apps erreicht.
- **Einschalten und jede Änderung** laufen zwei Minuten auf Probe; ohne „Regeln behalten“ kommen die vorigen Regeln zurück.
- Eine Änderung, die deine eigene Sitzung aussperren würde, lehnt TitanOS ab.
- Optional sperrt TitanOS Absender nach fehlgeschlagenen Anmeldungen (Weboberfläche, SSH, SMB).
- **Grenze:** Die Firewall filtert nur, was das NAS selbst und veröffentlichte App-Ports erreicht. VMs, LXC und Apps mit eigener LAN-Adresse schützt sie nicht.

### Regel über den Agenten ändern

1. `firewall_status` lesen: Zonen, Zellen, eigene Regeln, zuletzt verworfene Pakete.
2. Die Regel in Worten zeigen: von wo, wohin, Port, Protokoll.
3. Nach deiner Freigabe `firewall_rule_add` bzw. `firewall_rule_remove`.
4. Die Änderung läuft auf Probe. **Du** musst sie in der Weboberfläche innerhalb von zwei Minuten bestätigen, sonst geht sie zurück.
5. Gesperrte Absender entsperrt `firewall_unblock`.

## Fernzugriff von unterwegs

Faustregel: Lieber VPN als offene Ports. Die Weboberfläche nicht direkt ins Internet stellen.

### Tailscale (Plugin)

1. Unter **Plugins → Tailscale** installieren (Plugin hat volle Systemrechte, siehe Anleitung „Konten, Updates, Plugins“).
2. „Verbinden / Anmelden“ erzeugt einen Link; Anmeldung und Freigabe bei Tailscale machst du selbst.
3. Optional: Subnetze freigeben (Heimnetz von unterwegs), Exit Node anbieten oder nutzen. Routen und DNS anderer Geräte übernimmt TitanOS nur, wenn du es einschaltest.
4. Änderungen gelten zwei Minuten auf Probe („Zugriff funktioniert – speichern“).
5. Mit HTTPS im Tailnet ist die Weboberfläche unter `https://NAME.TAILNET.ts.net` mit gültigem Zertifikat erreichbar.

### WireGuard-Server (eingebaut)

1. Einstellungen › WireGuard: einschalten, Adresse aus dem Internet eintragen (vorgeschlagen wird ein Name aus Dynamisches DNS).
2. Am Router **UDP 51820** an das NAS weiterleiten.
3. „Gerät hinzufügen“ mit Namen; QR-Code mit der WireGuard-App scannen. Der private Schlüssel des Geräts wird nur jetzt angezeigt und nicht gespeichert.
4. Zugriff „NAS und Heimnetz“ (Vorgabe) oder „Alles“.
5. Ein verlorenes Gerät entfernen: Es ist sofort ausgesperrt.

### NetBird (Plugin)

1. Unter **Plugins → NetBird** installieren.
2. Mit einem **Setup-Key** aus der NetBird-Verwaltung anmelden. Der Key wird nie wieder angezeigt; der Agent fragt nie danach.
3. Leerer Management-Server heißt NetBird Cloud; ein eigener Server ist möglich.
4. Routen, Routing-Peer/Exit Node und DNS sind anfangs aus und laufen bei Änderungen zwei Minuten auf Probe.

## Let's Encrypt und DDNS

### Dynamisches DNS

1. Einstellungen › Dynamisches DNS, ab Werk aus.
2. Anbieter wählen (z. B. Cloudflare, deSEC, DuckDNS, IPv64, oder dyndns2 wie No-IP und Strato) und Namen eintragen.
3. TitanOS hält den A-Eintrag auf der öffentlichen IPv4 und den AAAA-Eintrag auf der IPv6 des NAS aktuell.
4. Mit DS-Lite ist IPv4 von außen nicht erreichbar; dann nur IPv6 oder ein VPN nutzen.

### Let's Encrypt

1. Einstellungen › Let's Encrypt, ab Werk aus.
2. Namen eintragen, die im öffentlichen DNS zum NAS gehören (keine `.local`-Namen, keine IP-Adressen).
3. **DNS-Challenge (empfohlen):** kein offener Port nötig, auch Platzhalter `*.domain` für die HTTPS-Namen der Apps.
4. **HTTP-Challenge:** Port 80 muss aus dem Internet zum NAS erreichbar sein. TitanOS öffnet Port 80 nie von selbst.
5. Die Erneuerung läuft von selbst.

## Gute Gewohnheiten

- Nur eine Netzwerkänderung auf einmal und gleich bestätigen.
- Vor Firewall-Änderungen wissen, wie du an die NAS-Konsole kommst.
- Keine Portweiterleitung für SMB, SSH oder die Weboberfläche ins Internet.
