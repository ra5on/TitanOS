# Cluster

Mehrere TitanOS lassen sich zu einem **Cluster** verbinden, ähnlich wie bei Proxmox:

- Alle Knoten vertrauen sich **vollständig**. Von der Weboberfläche jedes Knotens aus lassen sich alle Knoten verwalten.
- Die **Konten sind auf allen Knoten dieselben**: Passwort, Zwei-Faktor-Anmeldung, Passkeys, Rolle, Sperre und SSH-Schlüssel.
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

   Ein Passwort oder Konto steht nicht darin. Jeder Knoten zeigt dieselbe Information.
3. Auf dem neuen NAS die Information einfügen und **Prüfen**. Die Vorschau zeigt:
   - welche Konten des neuen NAS durch die gleichnamigen des Clusters **ersetzt** werden,
   - welche von ihm in den Cluster gehen,
   - welche aus dem Cluster dazukommen.
4. **Beitreten**, mit einem einfachen Bestätigungsdialog.

Erstellen, Beitreten, Verlassen und Entfernen dürfen nur Besitzer, und nur in einer Websitzung des Knotens selbst.

**Neuen Schlüssel erstellen** macht die bisherige Beitrittsinformation ungültig. Knoten, die schon im Cluster sind, bleiben drin. Jeder Beitritt erzeugt die Meldung `cluster_node_joined` und einen Eintrag im Protokoll. Falsche Schlüssel bremst dieselbe Sperre wie falsche Passwörter.

Alle Knoten müssen **dieselbe Version** haben. Beitreten und Verwalten gehen sonst nicht; die Seite sagt das.

## Verbindung zwischen den Knoten

Die Knoten sprechen über den HTTPS-Port von titand. Jeder Knoten nennt seine Adressen in dieser Reihenfolge:

1. **Weitere Adressen**, die man selbst einträgt, z. B. einen DDNS-Namen, eine Adresse in einem eigenen VPN oder Name:Port einer Portweiterleitung.
2. **Gefundene Adressen** der Netzwerkschnittstellen: zuerst das Heimnetz, dann Tailscale und NetBird (100.64.0.0/10), WireGuard- und andere Tunnel mit privaten Adressen.

Docker- und VM-Brücken gelten nicht als Weg. Ein Knoten probiert die Adressen der Reihe nach und merkt sich im RAM die, die geantwortet hat.

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

- **Knoten wählen:** in der Statusleiste („Knoten ▾“) oder unter Einstellungen › Cluster › **Verwalten**.
- **Weiterleitung:** Der Browser bleibt auf dem eigenen NAS. Dessen titand leitet `/api/v1/cluster/nodes/<id>/api/…` an den Knoten weiter, auch WebSockets (Terminal, Konsolen, VM-Bildschirm), Uploads und Downloads.
- **Konto und Rechte:** Der andere Knoten prüft Konto und Rechte selbst, mit seinem synchronisierten Konto.
- **Protokoll:** Im Aktivitätsprotokoll des Knotens steht die Herkunft „per Cluster“.
- **Lokal bleiben:** Anmeldung, das eigene Konto, die Darstellung und die Cluster-Einstellungen bleiben auf dem eigenen NAS.
- **Anzeige:** Ein Balken unter der Kopfzeile zeigt, welcher Knoten gerade verwaltet wird, und führt zurück. Die Wahl gilt je Browser-Tab.

**Nur auf dem Knoten selbst** gehen das Ändern und Bestätigen von Netzwerk und Firewall (`cluster_local_only`). Die Bestätigung soll zeigen, dass der Browser das NAS noch erreicht. Über einen anderen Knoten zeigt sie das nicht.

**Weitergeleitet werden nur Websitzungen**, keine API-Tokens, kein MCP und nicht der Assistent. Eine Anfrage, die schon von einem anderen Knoten kommt, wird nicht weitergereicht. Cluster-Routen nehmen sie nicht an.

## Konten

- **Version:** Jede Kontoänderung auf einem Knoten bekommt eine Version aus Zeit und Knoten. Neuer gewinnt, bei gleicher Zeit die höhere Knoten-ID.
- **Verteilung:** Der Knoten schickt die Änderung sofort an alle anderen. Jede Minute vergleichen die Knoten ihre Versionen, damit ein Knoten, der fehlte, nachzieht. Dabei wird nur der RAM verglichen; auf den Stick wird nur bei einer echten Änderung geschrieben.
- **Löschen:** Gelöschte Konten bleiben als Grabstein in `cluster/cluster.json`.
- **Linux-UID:** Jeder Knoten behält seine eigene Linux-UID je Konto, weil seine Dateien ihr gehören.
- **Übernahme:** Übernommene Änderungen wirken wie lokale:
  - Bei neuem Passwort, Zwei-Faktor-Wechsel, entferntem Passkey oder Sperre enden die Sitzungen.
  - Samba und sshd ziehen nach.
  - Ein gelöschtes Konto verliert auch hier Einstellungen, Chats und Freigaberechte.
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
| `cluster/node.json` | ID, Cluster-Zertifikat und Schlüssel dieses Knotens, selbst eingetragene Adressen |
| `cluster/cluster.json` | Name, Generation, Knoten, Versionen der Konten, Internet-Option |
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
  - Wiederholung, falsches Geheimnis, Internet-Absender, Sperre gegen Raten.
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
  - restic über das Relais gegen einen echten Knoten.
