# Cluster

Mehrere TitanOS lassen sich zu einem **Cluster** verbinden, ähnlich wie bei Proxmox:

- Der **Admin-Knoten** (der Knoten, der den Cluster erstellt hat) verwaltet alle anderen. Er kann einzelnen Knoten erlauben mitzuverwalten („Verwalten erlauben“, ab Werk aus). Alle übrigen Knoten sind **Mitglieder**: Sie werden verwaltet, verwalten aber selbst keinen anderen Knoten.
- Die **Konten des Admin-Knotens gelten auf allen Knoten**: Passwort, Zwei-Faktor-Anmeldung, Passkeys, Rolle, Sperre und SSH-Schlüssel. Eigene Konten eines Mitglieds bleiben auf ihm und kommen nie auf den Admin-Knoten oder andere Knoten.
- **Backups und Replikation** gehen von Knoten zu Knoten, ohne Kopplungscode von Hand und auf jedem Weg, der den anderen Knoten erreicht.

Nicht dabei sind Ausfallschutz (HA), Quorum und gemeinsamer Speicher. Fehlen die anderen Knoten, arbeitet jeder Knoten für sich weiter. VMs und Apps lassen sich noch nicht zwischen Knoten verschieben.

Einstellung: **Einstellungen › Cluster** (Besitzer und Administratoren). Code: Backend-Paket `backend/internal/cluster`, API `backend/internal/api/cluster.go`, Oberfläche `web/src/routes/settings/cluster/`, `web/src/lib/cluster-node.ts`.

## Erstellen und beitreten

1. Auf dem ersten NAS: **Cluster erstellen** und einen Namen vergeben.
2. Unter **Beitrittsinformation anzeigen** bestätigt man mit dem eigenen Passwort (und ggf. einem Code) oder mit einem Passkey des Kontos für diese Adresse; erst dann zeigt die Seite die **Beitrittsinformation**. Sie verschwindet nach fünf Minuten, beim Verlassen der Seite und bei „Ausblenden“ wieder und steht nie in der normalen Antwort der Seite (`GET /api/v1/cluster`), sondern nur in `POST /api/v1/cluster/join-info`. Das ist ein Text `titanos-cluster:…` mit:
   - dem Namen des Clusters,
   - den Adressen der Knoten,
   - den Fingerabdrücken ihrer Cluster-Zertifikate,
   - dem **Beitrittsschlüssel** (32 Byte).

   Ein Passwort oder Konto steht nicht darin. Nur der Admin-Knoten und Knoten mit seiner Freigabe zeigen sie und nehmen neue Knoten auf; sie enthält nur deren Adressen.
3. Auf dem neuen NAS die Information einfügen und **Prüfen**. Die Vorschau zeigt:
   - welche Konten des neuen NAS durch die gleichnamigen des Clusters **ersetzt** werden,
   - welche nur auf ihm bleiben (sie gehen nicht in den Cluster),
   - welche aus dem Cluster dazukommen.
4. **Beitreten**, mit einem einfachen Bestätigungsdialog.

Erstellen, Beitreten, Verlassen und Entfernen dürfen nur Besitzer, und nur in einer Websitzung des Knotens selbst.

**Neuen Schlüssel erstellen** macht die bisherige Beitrittsinformation ungültig. Knoten, die schon im Cluster sind, bleiben drin. Jeder Beitritt erzeugt die Meldung `cluster_node_joined` und einen Eintrag im Protokoll. Falsche Schlüssel bremst dieselbe Sperre wie falsche Passwörter.

**Versionen** (seit 0.44.0): Es zählt nicht die Versionsnummer, sondern die **Protokollstufe** des Clusters (`cluster.Protocol`, heute 1). Sie steigt nur, wenn sich Signatur, Beitritt oder Weiterleitung so ändern, dass ältere Knoten nicht mehr folgen können. Knoten derselben Stufe treten bei und verwalten einander voll, auch wenn eine 0.43.0 und eine 0.44.0 läuft; Versionen vor 0.44.0 senden keine Stufe und sprechen Stufe 1. Bei unterschiedlicher Stufe gehen über den Cluster nur Update, Systeminfo, Protokolle, Meldungen und Ein-/Ausschalten (`limited` in `cluster/protocol.go`), damit man den Knoten immer noch aktualisieren kann. Ältere titand prüfen beim Beitritt und Verwalten selbst noch die gleiche Version: Ein 0.43.0-Admin-Knoten verwaltet einen 0.44.0-Knoten deshalb erst, wenn er selbst aktualisiert ist; umgekehrt geht es sofort.

Verwaltet man einen Knoten mit anderer Version, nennt der Balken oben beide Versionen und bietet bei einer älteren „<Knoten> aktualisieren“ an. Ruft die neuere Oberfläche etwas auf, das der ältere Knoten nicht kennt (`404 not_found`), zeigt sie „Das gibt es auf diesem Knoten erst in einer neueren Version“ (`cluster_feature_missing`).

**Alle aktualisieren** (Einstellungen › Cluster oder Einstellungen › Updates, nur verwaltende Knoten, eigene Websitzung, `POST /api/v1/cluster/update`, Zustand `GET /api/v1/cluster/update`): Der Dialog zeigt je Knoten Version, gefundenes Release und Art (App- oder Systemupdate). **Nacheinander** (vorausgewählt): Jeder Knoten sucht, installiert und muss mit der neuen Version antworten (höchstens 20 Minuten), bevor der nächste dran ist; ein Fehler hält an, die übrigen gelten als übersprungen. **Alle gleichzeitig**: alle anderen auf einmal. Dieser Knoten kommt in beiden Fällen zuletzt, weil sein Update titand neu startet. Jeder Knoten prüft die Signatur selbst und fällt wie gewohnt zurück; der Lauf liegt im RAM.

## Verbindung zwischen den Knoten

Die Knoten sprechen über den HTTPS-Port von titand. Jeder Knoten nennt seine Adressen in dieser Reihenfolge, jede mit ihrem **Weg** (`Node.Ways`, seit 0.44.0):

1. **Weitere Adressen**, die man selbst einträgt, z. B. einen DDNS-Namen, eine Adresse in einem eigenen VPN oder Name:Port einer Portweiterleitung (Weg „Eigene Adresse“).
2. **Gefundene Adressen** der Netzwerkschnittstellen: zuerst das Heimnetz, dann die VPNs. Der Weg richtet sich nach der Schnittstelle: `titanoscl` ist der Tunnel von TitanOS, `wg*`, `titanoswg` und `ipv64wg` WireGuard, `tailscale0` Tailscale, `wt0`/`nb-*` NetBird; sonst 100.64.0.0/10 Tailscale, private Adressen Heimnetz, öffentliche Internet. Ist Tailscale verbunden, kommt der MagicDNS-Name (`<name>.<tailnet>.ts.net`) hinter den Tailscale-IPs dazu; er überlebt eine neue IP.

Ältere Knoten senden keine Wege; dann bestimmt der Empfänger ihn aus der Adresse. Docker- und VM-Brücken gelten nicht als Weg.

**Wege wählen** (Einstellungen › Cluster, „Verbindung zwischen den Knoten“; gilt clusterweit, nur verwaltende Knoten ändern es, `State.Ways`):

- **Weg für die Verwaltung:** Automatisch (die Reihenfolge oben, wie bisher) oder ein Weg, der zuerst probiert wird. **Nur diesen Weg benutzen** verbietet die anderen; fällt er aus, ist der Knoten nicht erreichbar.
- **Zurück auf den bevorzugten Weg:** Läuft ein Knoten über einen Ausweichweg, probiert titand den bevorzugten jede Minute (wahlweise alle 5 oder 15 Minuten, oder nie) und wechselt zurück, sobald er antwortet. Vorher blieb ein Knoten auf dem Ausweichweg, bis dieser ausfiel.
- **Weg für Sicherungen:** wie bei der Verwaltung, oder **nur** ein bestimmter Weg. Dann warten Backups und Replikation über das Relais, solange dieser Weg fehlt, statt z. B. über ein Tailscale-Relais zu laufen.
- **Pro Knoten** eine Ausnahme über „⋯ › Weg zu diesem Knoten“ (`State.Routes`, `PUT /api/v1/cluster/nodes/{id}/route`, leer = Cluster-Einstellung).
- **Wege prüfen** fragt jeden anderen Knoten auf jeder seiner Adressen und zeigt erreichbar ja/nein, Antwortzeit und den Grund (Zeitüberschreitung, abgelehnt, nicht erlaubt); `POST /api/v1/cluster/probe`.
- Die Knotenliste zeigt, über welchen Weg und wie schnell ein Knoten zuletzt antwortete, und warnt, wenn er über einen Ausweichweg läuft.

**WireGuard-Tunnel von TitanOS** (ab Werk aus, ein Schalter für den ganzen Cluster, `cluster/tunnel.go`): titand legt auf jedem Knoten die Schnittstelle `titanoscl` an, UDP-Port 51821. Das Tunnelnetz ist das erste freie aus 10.77.0.0/24, 10.78.77.0/24, 172.30.77.0/24 und 192.168.177.0/24; die Adressen verteilt der Admin-Knoten (`State.Tunnel`). Jeder Knoten erzeugt seinen Schlüssel selbst (privat in `cluster/node.json`, öffentlich mit seinem Eintrag). Gegenstelle ist die Adresse, über die der Knoten gerade erreicht wird, sonst seine erste andere; der Tunnel braucht also einen Weg zum Start, für andere Orte eine Portweiterleitung von 51821/UDP oder einen DDNS-Namen. Die Firewall zählt `titanoscl` als VPN, der Dienst „VPN-Tunnel“ erlaubt 51821/UDP. Schlüssel gehen über eine Datei in `/run` an `wg`, nie als Argument.

**Erlaubte Netze:** Ab Werk nur nicht-öffentliche Adressen (privat, Tailscale/NetBird). Die Option **„Knoten auch über das Internet erreichen“** erlaubt auch öffentliche Adressen. Dann muss Port 443 aus dem Internet erreichbar sein; TitanOS öffnet dafür nichts, weder an der eigenen Firewall noch am Router.

**Sicherheit**, unabhängig vom Weg:

- **Eigenes Cluster-Zertifikat:**
  - Jeder Knoten hat ein selbst signiertes Zertifikat (ECDSA P-256, 30 Jahre) in `cluster/node.json`.
  - titand liefert es nur aus, wenn ein Knoten nach dem Namen `node.titanos-cluster.invalid` fragt.
  - Die anderen prüfen genau diesen Fingerabdruck (SHA-256). Das Zertifikat der Weboberfläche (Let's Encrypt, Tailscale, eigenes) spielt dabei keine Rolle.
- **Signierte Anfragen:**
  - Jede Anfrage trägt die Kopfzeile `X-Titanos-Cluster` mit einem HMAC-SHA256 über Methode, Pfad und Abfrage, Knoten, Generation, Zeit (±2 min) und Nonce.
  - Das Konto, für das die Anfrage gilt, ist Teil der Signatur.
  - Eine Nonce gilt nur einmal (RAM).
  - Der Inhalt ist nicht Teil der Signatur, weil Uploads und Konsolen strömen. Ihn schützt die TLS-Verbindung mit festgenageltem Zertifikat.
- **Entfernen erneuert die Geheimnisse:**
  - Das Geheimnis des Clusters und der Beitrittsschlüssel werden neu erzeugt und mit dem alten Geheimnis an die verbleibenden Knoten verteilt.
  - Der entfernte Knoten erfährt seine Entfernung und verlässt den Cluster. Seine Konten und Daten behält er.
  - Ein Knoten, der dabei nicht erreichbar war, kennt nur das alte Geheimnis und muss neu beitreten.

## Vertrauensmodell

Wer einen Knoten verwaltet, kann alle verwalten. Wer einen Knoten übernimmt, also Root-Zugriff hat oder den Stick ausliest, hat das Geheimnis und damit den ganzen Cluster. Das ist dieselbe Entscheidung wie bei Proxmox und bewusst so gewählt (Projektinhaber, 30.10.2026).

## Einen anderen Knoten verwalten

- **Knoten wählen:** über das TitanOS-Symbol oben links (kleiner Pfeil, auch auf dem Handy), in der seitlichen Navigation über die Knoten-Karte oben, oder unter Einstellungen › Cluster › **Verwalten**.
- **Weiterleitung:** Der Browser bleibt auf dem eigenen NAS. Dessen titand leitet `/api/v1/cluster/nodes/<id>/api/…` an den Knoten weiter, auch WebSockets (Terminal, Konsolen, VM-Bildschirm), Uploads und Downloads.
- **Konto und Rechte:** Der andere Knoten prüft Konto und Rechte selbst, mit seinem synchronisierten Konto.
- **Protokoll:** Im Aktivitätsprotokoll des Knotens steht die Herkunft „per Cluster“.
- **Lokal bleiben:** Anmeldung, das eigene Konto, die Darstellung und die Cluster-Einstellungen bleiben auf dem eigenen NAS.
- **Anzeige:** Ein Balken unter der Kopfzeile zeigt, welcher Knoten gerade verwaltet wird, und führt zurück. Die Wahl gilt je Browser-Tab.
- **Knoten startet neu** (seit 0.41.0): Lehnt der Knoten die Verbindung ab (`connection refused`, typisch für die Sekunden eines App-Updates oder Neustarts von titand), antwortet die Weiterleitung mit `503 cluster_restarting` und `Retry-After: 3` statt `502 cluster_unreachable`. Die Oberfläche (`web/src/lib/api.ts`, `node-restart.ts`) wartet dann und fragt bis zu zwei Minuten von selbst erneut; die Anfrage hat den Knoten nie erreicht, deshalb gilt das für alle Methoden. Solange gewartet wird, sagt der Balken „Knoten startet neu, gleich wieder da“. Beim Laden der Oberfläche wartet nur die Dienste-Abfrage (`/services/state`) höchstens fünf Sekunden, damit ein neu startender Knoten den Start nicht aufhält. Dauert es länger, erscheint derselbe Satz als Fehlermeldung. Die Übersicht unter Einstellungen › Cluster nennt den Knoten genauso. Im Protokoll steht dazu eine Info-Zeile („node is restarting“), kein Fehler. Andere Verbindungsfehler (Zeitüberschreitung, keine Route) bleiben „nicht erreichbar“.

**Nur auf dem Knoten selbst** gehen das Ändern und Bestätigen von Netzwerk und Firewall (`cluster_local_only`). Die Bestätigung soll zeigen, dass der Browser das NAS noch erreicht. Über einen anderen Knoten zeigt sie das nicht.

**Weitergeleitet werden nur Websitzungen**, keine API-Tokens und kein MCP. Eine Anfrage, die schon von einem anderen Knoten kommt, wird nicht weitergereicht. Cluster-Routen nehmen sie nicht an.

## Meldungen und Uhrzeit

- **Knoten nicht erreichbar:** Antwortet ein Knoten auf keinem Weg, meldet jeder verwaltende Knoten das nach 10 Minuten (einstellbar 5, 30, 60 oder nie, `cluster_node_unreachable`, Warnung) und „wieder erreichbar“, wenn er zurück ist (`cluster_node_back`). Kurze Neustarts bei App-Updates bleiben unter der Schwelle.
- **Uhrzeit vergleichen** (ab Werk an): Jeder Knoten sendet seine Uhrzeit mit dem Status. Weicht sie mehr als eine Minute ab, steht „Uhr geht n min vor/nach (NTP prüfen)“ am Knoten; ab zwei Minuten scheitern die signierten Anfragen.
- **Dashboard-Kachel „Cluster“** (nur Besitzer/Administratoren): je Knoten erreichbar, Version, Weg, CPU und freier Speicher.

## Rollen und Rechte

- **Admin-Knoten:** der Knoten, der den Cluster erstellt hat (`cluster.json` `admin`). Bei einem Cluster aus einer Version ohne Rollen, oder wenn der Admin-Knoten den Cluster verlassen hat, ist es der Knoten, der am frühesten beigetreten ist (bei gleicher Sekunde die kleinere ID); jeder Knoten kommt so auf denselben.
- **Freigabe:** Nur der Admin-Knoten erlaubt einem anderen Knoten das Verwalten oder entzieht es (`PUT /api/v1/cluster/nodes/{id}/manage`, nur Besitzer, eigene Websitzung). Ein neu beigetretener Knoten hat sie nie.
- **Was nur verwaltende Knoten dürfen:** andere Knoten über den Proxy verwalten, Beitrittsinformation zeigen und neue Knoten aufnehmen, Name/Internet-Option ändern, neuen Beitrittsschlüssel erstellen, Knoten entfernen, die Mitgliederliste verteilen. Den Admin-Knoten entfernt niemand; er verlässt den Cluster selbst und teilt das den anderen mit.
- **Was jeder Knoten darf:** seine eigenen Adressen pflegen, den Cluster verlassen (ein verwaltender Knoten führt das Entfernen aus), den Zustand der anderen sehen und Backups an einen Knoten schicken, der sie annimmt.
- **Durchsetzung beim Ziel:** Alle Knoten kennen dasselbe Geheimnis; deshalb prüft der angefragte Knoten zusätzlich, dass die TLS-Verbindung das Cluster-Zertifikat des Knotens zeigt, für den die Anfrage signiert ist (Client-Zertifikat, Fingerabdruck wie beim Server). Ein Mitglied kann sich so nicht als Admin-Knoten ausgeben. Nur der Entwicklungsmodus (HTTP) prüft das nicht.
- Ein Mitglied zeigt keinen Knotenwechsler und keine Beitrittsinformation; die Cluster-Seite nennt seine Rolle.
- **Admin-Rolle übergeben** (seit 0.44.0, nur Besitzer, eigene Websitzung, `POST /api/v1/cluster/nodes/{id}/admin`): Der Admin-Knoten gibt die Rolle an einen beliebigen Knoten ab und darf danach weiter verwalten. Antwortet der Admin-Knoten nicht, kann ein Knoten mit Freigabe die Rolle selbst **übernehmen**. Jede Übergabe erhöht `adminGen`; die höhere gewinnt auf jedem Knoten, auch auf dem alten Admin-Knoten, wenn er zurückkommt.

## Konten

- **Richtung:** Konten gehen nur vom Admin-Knoten zu den anderen. Andere Knoten nehmen Konten nur vom Admin-Knoten an (`cluster_not_permitted` sonst). Ändert ein Mitglied ein übernommenes Konto selbst, bleibt die Änderung dort, bis der Admin-Knoten das Konto wieder ändert.
- **Version:** Jede Kontoänderung auf dem Admin-Knoten bekommt eine Version aus Zeit und Knoten. Neuer gewinnt, bei gleicher Zeit die höhere Knoten-ID.
- **Verteilung:** Der Admin-Knoten schickt die Änderung sofort an alle anderen. Jede Minute vergleicht er die Versionen, damit ein Knoten, der fehlte, nachzieht. Dabei wird nur der RAM verglichen; auf den Stick wird nur bei einer echten Änderung geschrieben.
- **Löschen:** Gelöschte Konten bleiben als Grabstein in `cluster/cluster.json`.
- **Linux-UID:** Jeder Knoten behält seine eigene Linux-UID je Konto, weil seine Dateien ihr gehören.
- **Übernahme:** Übernommene Änderungen wirken wie lokale:
  - Bei neuem Passwort, Zwei-Faktor-Wechsel, entferntem Passkey oder Sperre enden die Sitzungen.
  - Samba und sshd ziehen nach.
  - Ein gelöschtes Konto verliert auch hier Einstellungen und Freigaberechte.
- **Einmal-Codes:** Benutzte TOTP-Schritte und Wiederherstellungscodes gehen mit. Dass ein Code nur einmal gilt, ist clusterweit „best effort“: In den Sekunden bis zur Verteilung könnte derselbe Code auf zwei Knoten angenommen werden.
- **Konflikte:** Zwei Knoten, die gleichzeitig dasselbe Konto ändern, entscheidet die Version (letzter Schreiber).
- **Gleichzeitige Beitritte:** Zwei gleichzeitige Beitritte an zwei verschiedenen Knoten können sich überschreiben. Dann einen der beiden Knoten neu beitreten lassen.

## Backups zwischen Knoten

Im Backup-Editor (Ziel „Anderes TitanOS“) und bei der Replikation gibt es die Auswahl **Cluster-Knoten** mit „Kopplungscode holen“. So läuft es ab:

1. Der Zielknoten legt in „Backups anderer TitanOS annehmen“ eine Quelle mit dem Namen des sendenden Knotens an und gibt deren Token zurück. Dazu muss dort das Annehmen eingeschaltet sein.
2. Der Kopplungscode zeigt auf ein Relais des sendenden Knotens: `http://127.0.0.1:8751/<knoten>/<quelle>`. Es läuft nur, solange der Knoten in einem Cluster ist.
3. Das Relais reicht restics Anfragen über den Cluster weiter, signiert und mit festgenageltem Zertifikat, auf jedem Weg, der den Knoten erreicht.

Der Auftrag selbst ist ein gewöhnliches REST-Ziel. Verschlüsselung, Aufbewahrung, Prüfen und Wiederherstellen gelten wie immer. Der bisherige Tailscale-Server auf Port 8750 und vorhandene Kopplungscodes bleiben unverändert.

## Dateien auf dem Stick

| Datei | Inhalt |
|---|---|
| `cluster/node.json` | ID, Cluster-Zertifikat und Schlüssel dieses Knotens, selbst eingetragene Adressen, privater Tunnel-Schlüssel |
| `cluster/cluster.json` | Name, Generation, Knoten mit Wegen und Tunnel-Schlüssel, Versionen der Konten, Internet-Option, Wege, Ausnahmen je Knoten, Tunnel, Admin-Übergaben |
| `cluster/secret.json` | Geheimnis des Clusters und Beitrittsschlüssel |

Der Ordner ist im Stick-Dateimanager ausgeblendet und nicht im Diagnosepaket. Ältere titand ignorieren ihn; ein Rückweg auf eine Version ohne Cluster lässt die Konten, wie sie sind.

## Prüfung und Grenzen

- **`go test ./internal/cluster/`** (auch `-race`), drei Knoten mit echten TLS-Servern:
  - Beitritt mit Vorschau und Kontozusammenführung,
  - Passwortänderung, die ankommt, eigene UID,
  - dritter Knoten über die Information eines anderen Knotens,
  - Löschen, Weiterleitung ohne Cookie,
  - Übersicht, Backup-Relais,
  - Entfernen mit neuem Geheimnis und Schlüssel, Verlassen,
  - Wiederholung, falsches Geheimnis, Internet-Absender, Sperre gegen Raten,
  - Knoten, der neu startet (geschlossener Port): `503 cluster_restarting` aus Weiterleitung, Zwischenspeicher und Übersicht.
  - seit 0.44.0 (`ways_test.go`): Weg einer Adresse, Reihenfolge und „nur dieser Weg“ für Verwaltung und Sicherungen, Ausnahme je Knoten, Verteilung der Einstellungen, Rückkehr auf den bevorzugten Weg, Wege prüfen, Verwalten eines Knotens mit anderer Version, Protokollstufen, Übergabe und Übernahme der Admin-Rolle samt Rückkehr des alten Admin-Knotens, Tunnel (Schlüssel, Gegenstellen, Befehle mit nachgebildetem `ip`/`wg`, kein Schlüssel als Argument oder in `/run` übrig), Meldung „nicht erreichbar“, „Alle aktualisieren“ nacheinander mit diesem Knoten zuletzt.
- **`node --test tests/node-restart.test.mjs`:** Warten und erneutes Fragen der Oberfläche, Balken an und aus, andere 503-Antworten sofort als Fehler.
- **`go test ./internal/api/ -run TestClusterThroughTheAPI`:**
  - zwei vollständige API-Server, Beitritt über die Routen,
  - Steuern von B über A,
  - `cluster_local_only`, keine Verkettung, gefälschte Signatur,
  - Konto, das ankommt.
- **Live:** zwei titand im Entwicklungsmodus auf einem Rechner; Oberfläche mit Playwright: Seite, Wechsler, weitergeleitete Anfragen, 390 px.
- **Nicht geprüft:**
  - zwei echte NAS,
  - echte Wege über Tailscale, NetBird, WireGuard oder das Internet,
  - Konsolen und VM-Bildschirm über die Weiterleitung,
  - restic über das Relais gegen einen echten Knoten,
  - „Knoten startet neu“ während eines echten App-Updates (nur mit geschlossenem Port im Test nachgestellt).
