# Tailscale

Das optionale Plugin wird unter **Plugins → Tailscale** auf dem gewählten
Plugin-Volume installiert. Nach dem Aktivieren zeigt dieselbe Ansicht den
Verbindungszustand, Gerätenamen, Tailscale-Adressen, Hinweise und Einstellungen.
**Verbinden / Anmelden** erzeugt einen Link zur Anmeldung bei Tailscale. Die
Anmeldung und Freigabe des Geräts erfolgen dort durch den Besitzer.

- **Freigegebene Subnetze:** ein IPv4- oder IPv6-Netz mit Präfix je Zeile;
  leer bedeutet keinen Subnet-Router. Routing nutzt Source NAT.
- **Exit Node:** entweder dieses NAS als Internet-Ausgang anbieten oder einen
  vorhandenen Exit Node aus dem Tailnet benutzen. Beide Rollen sind wählbar,
  aber nicht gleichzeitig. Beim Benutzen bleibt der lokale LAN-Zugang erlaubt.
- Routen und DNS-Einstellungen anderer Tailscale-Geräte werden nur übernommen,
  wenn die entsprechende Option eingeschaltet ist; beide sind anfangs aus.
- Subnetze und ein angebotener Exit Node müssen in der Tailscale-Verwaltung
  freigegeben werden, sofern keine passenden Auto-Approver-Regeln bestehen.
  Die Zugriffsregeln des Tailnets gelten weiterhin.
- Änderungen gelten zwei Minuten auf Probe. **Zugriff funktioniert – speichern**
  speichert sie dauerhaft; ohne Bestätigung oder mit **Änderungen zurücknehmen**
  kehrt TitanOS zur vorherigen Auswahl zurück. Auch ohne geöffneten Browser
  läuft diese Frist ab. Eine fehlgeschlagene Rücknahme bleibt sichtbar und
  wird erneut versucht. Plugin-Mutationen sind während der Probe gesperrt.

## Dashboard-Kachel

Besitzer und Administratoren können im Dashboard-Editor die Kachel
**Tailscale** hinzufügen. Sie zeigt die Geräte des Tailnets einschließlich
dieses NAS. Je Kachel lassen sich Geräte (über die stabile Tailscale-Node-ID,
nie über die IP) und sortierbare Angaben wählen: Online/Offline, Markierung
dieses NAS, Tailscale-IPv4/IPv6, Hostname, MagicDNS-Name, zuletzt gesehen,
Betriebssystem, Exit Node und Subnet-Routen. Die Daten liefert
`GET /api/v1/plugins/tailscale/devices` aus `tailscale status --json`, höchstens
alle fünf Sekunden und nur während die Kachel sichtbar oder im Editor ist.
Ohne aktives Plugin meldet die Kachel „nicht aktiv“; unbekannte Werte (etwa
Online-Zustände bei getrenntem Backend) bleiben leer. Geprüft mit
aufgezeichneter Statusausgabe und Browser-Fixtures, noch nicht mit einem echten
Tailnet.

## Weboberfläche unter dem Tailnet-Namen (HTTPS)

TitanOS ist im Tailnet unter `https://<name>.<tailnet>.ts.net` erreichbar
(MagicDNS). Seit 0.34.0 liefert titand dort ein **gültiges Zertifikat** aus,
sobald in der Tailscale-Admin-Konsole unter DNS „HTTPS Certificates“
eingeschaltet ist; sonst bleibt es beim selbst signierten Zertifikat mit
Browserwarnung. Passkeys und Mitteilungen auf dem iPhone brauchen das gültige.

- titand fragt `tailscale status --json` (`CertDomains`, eigener `DNSName`) und
  lässt sich mit `tailscale cert --cert-file … --key-file … <name>` das
  Zertifikat geben (Let's Encrypt über Tailscale). Die Dateien liegen nur kurz
  in `/run/titanos-tailscale/cert` (0700) und werden sofort nach dem Lesen
  gelöscht; der Schlüssel bleibt im RAM von titand. tailscaled selbst hält und
  erneuert das Zertifikat in seinem Zustand auf dem Daten-Volume.
- Ausgeliefert per SNI nur für genau diesen Namen
  (`tlscert.Manager.SetTailnet`, nach Let's Encrypt und vor dem eigenen bzw.
  selbst signierten Zertifikat), ohne Neustart. IP-Adressen, `.local` und andere
  Namen bleiben wie bisher.
- Abfrage 20 Sekunden nach dem Start, danach alle 12 Stunden; ohne Zertifikat
  (Plugin aus, nicht verbunden, HTTPS im Tailnet aus, Fehler) alle 5 Minuten.
  Ein fehlgeschlagener Abruf behält ein noch gültiges Zertifikat. Nichts davon
  greift auf `/config` zu; im Entwicklungsmodus passiert nichts.
- Kein eigener Schalter in TitanOS: Das Einschalten von HTTPS im Tailnet ist
  die Entscheidung des Besitzers. Ist es aus, holt titand über das Plugin den
  Ein-Klick-Link von Tailscale (LocalAPI `POST /localapi/v0/query-feature?feature=serve`
  am Socket `/run/tailscale/tailscaled.sock`, derselbe Link wie bei
  `tailscale serve`; nur Links auf `login.tailscale.com` werden gezeigt) und
  die Tailscale-Einstellungen zeigen ihn als Knopf „HTTPS im Tailnet
  einschalten“. Solange die Seite offen ist, sieht titand alle 20 Sekunden
  nach, damit das Zertifikat dem Klick gleich folgt. Der Name erscheint dabei
  in öffentlichen Certificate-Transparency-Protokollen; die Seite sagt das.
- Zustand in `GET /api/v1/plugins/tailscale` unter `https` (`state`
  inactive/disabled/ok/error, `name`, `notAfter`, `error`, `enableUrl`) und in den
  Tailscale-Einstellungen mit Link auf die Adresse.

## Ablage und Netzwerk

Nur Besitzer und Administratoren erhalten Zugriff auf diese API. Einstellungen
liegen in `/config/plugins/tailscale/settings.json` und werden einmal beim Start
gelesen. Tailscales private Geräteschlüssel und Zustand liegen ausschließlich
auf dem Daten-Volume in `.titanos/tailscale/` (root, Modus 0700), nicht im RAM-
Overlay oder auf dem Bootstick. Deaktivieren und Entfernen des Plugins löschen
diesen Zustand nicht. Das reine Stick-Backup enthält ihn deshalb nicht.

`titanos-tailscale.service` startet nach dem Einbinden des Plugin-Volumes.
Tailscale-SSH, die zusätzliche Webverwaltung und automatische Änderungen des
Tailscale-Binarys sind ausgeschaltet. Updates erfolgen über das signierte
TitanOS-Plugin. Client-Telemetrie an Tailscales Logserver ist ausgeschaltet;
für die Verbindung selbst wird der Tailscale-Kontrolldienst benötigt.

Tailscale benutzt `--netfilter-mode=on` und die iptables-Schnittstelle für IPv4
und IPv6. Seine `ts-input`-/`ts-forward`-Hooks stehen vor der temporären
TitanOS-Freigabe. Das ist auch für die Paketmarkierungen des Subnet-SNAT nötig.
Der Firewall-Helfer erhält diese Reihenfolge nach einem Docker-/Netzwerkstart.
Die optionale [Firewall](FIREWALL.md) lässt diese Hooks unberührt, filtert in
ihrer eigenen Tabelle `inet titanos`, zählt `tailscale0` und `100.64.0.0/10`
zur Zone VPN, lässt UDP 41641 ab Werk aus allen Zonen zu (Dienst „VPN-Tunnel“)
und filtert weder ausgehenden Verkehr noch die Tailscale-Weiterleitung; pauschale
neue LAN-Freigaben legt das Plugin nicht an. Ausgeschaltet bleibt die
TitanOS-Freigabe global offen. Ist zusätzlich das NetBird-Plugin installiert, ruft der
Firewall-Helfer am Ende dessen Hook auf: NetBirds Interface `wt0` wird vor
`ts-input` angenommen, weil beide Netze 100.64.0.0/10 nutzen (siehe
[NetBird](NETBIRD.md)).

Subnet-Routing/Exit-Node-Angebot aktivieren IPv4-/IPv6-Forwarding. Vorher werden
bereits erlaubte IPv6-Router-Advertisements mit `accept_ra=2` erhalten. Beim
Abschalten wird gemeinsam mit Docker/VMs genutztes Forwarding nicht global
zurückgesetzt. Die LAN-Adressen, Bridge und DHCP-Konfiguration bleiben erhalten.

## Build und Prüfstand

Der Build verwendet Tailscale 1.104.1 aus dem offiziellen statischen amd64-
Archiv mit im Repository festgelegtem SHA-256. Originale BSD-Lizenz und die
Hersteller-Sammlung der Abhängigkeitshinweise liegen im Plugin und im separaten
Lizenzarchiv. Plugin und Manifest werden im geschützten Release-Job signiert.

Geprüft: API-Rollen, validierte Routen und Login-Links, Speichern/Rücknahme,
Fehlerfälle, echter Dienststart und Lebenszyklus im isolierten QEMU-NAS,
IPv4/IPv6-Forwarding, Firewall-Reihenfolge und automatische Rücknahme nach zwei
Minuten. Eine Verbindung zu einem echten Tailnet, Datenverkehr durch ein
freigegebenes Subnetz und beide Exit-Node-Rollen müssen mit angemeldeten Geräten
geprüft werden; der Test legt keine Tailscale-Konten oder fremde Netzfreigaben an.

Primärquellen: [CLI](https://tailscale.com/docs/reference/tailscale-cli),
[Subnet-Router](https://tailscale.com/docs/features/subnet-routers),
[Exit Nodes](https://tailscale.com/docs/features/exit-nodes),
[Firewall-Modus](https://tailscale.com/docs/features/firewall-mode),
[Lizenzhinweise](https://tailscale.com/licenses/tailscale).
