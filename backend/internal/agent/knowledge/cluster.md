# Cluster aus mehreren TitanOS
_Mitgeliefert mit TitanOS, wird bei Updates ersetzt. Eigene Anleitungen gehören in den Ordner Wissen außerhalb von „TitanOS“._

Diese Anleitung erklärt, wie mehrere TitanOS zu einem Cluster verbunden sind, welche Rollen die Knoten haben und was über Knoten hinweg geht.

## Was ein Cluster ist

- Mehrere TitanOS (Knoten) werden über eine Beitrittsinformation verbunden.
- Eingerichtet wird unter **Einstellungen › Cluster** (Besitzer und Administratoren). Erstellen, Beitreten, Verlassen und Entfernen darf nur ein Besitzer in einer Websitzung des Knotens selbst.
- **Nicht dabei:** Ausfallschutz (HA), Quorum und gemeinsamer Speicher. Fehlen andere Knoten, arbeitet jeder für sich weiter. VMs und Apps lassen sich noch nicht zwischen Knoten verschieben.

## Rollen der Knoten

| Rolle | Bedeutung |
|---|---|
| **Admin-Knoten** | hat den Cluster erstellt (oder die Rolle übernommen), verwaltet alle anderen, verteilt die Konten |
| **Knoten mit Freigabe** | darf mitverwalten, wenn der Admin-Knoten „Verwalten erlauben“ eingeschaltet hat (ab Werk aus) |
| **Mitglied** | wird verwaltet, verwaltet selbst keinen anderen Knoten |

- Die **Konten des Admin-Knotens gelten auf allen Knoten** (Passwort, 2FA, Passkeys, Rolle, Sperre, SSH-Schlüssel). Eigene Konten eines Mitglieds bleiben auf ihm.
- Die Admin-Rolle lässt sich an einen anderen Knoten übergeben. Antwortet der Admin-Knoten nicht, kann ein Knoten mit Freigabe sie übernehmen.
- Wer einen Knoten verwaltet, kann alle verwalten. Wer Root-Zugriff auf einen Knoten hat, hat den ganzen Cluster.

## Was übergreifend geht

1. **Andere Knoten verwalten:** in der Oberfläche über den Knotenwechsler (TitanOS-Symbol oben links). Der Browser bleibt auf deinem NAS; die Anfragen werden signiert weitergeleitet. Im Aktivitätsprotokoll des Knotens steht „per Cluster“.
2. **Backups und Replikation** von Knoten zu Knoten: im Backup-Editor Ziel „Anderes TitanOS“, Auswahl „Cluster-Knoten“. Auf dem Zielknoten muss „Backups anderer TitanOS annehmen“ eingeschaltet sein.
3. **Alle aktualisieren** (Einstellungen › Cluster oder › Updates): nacheinander (vorausgewählt; ein Fehler hält an) oder alle gleichzeitig. Der eigene Knoten kommt zuletzt.
4. **Meldungen** jedes Knotens erscheinen in der Knotenliste; ist ein Knoten länger nicht erreichbar (ab Werk 10 Minuten), kommt eine Meldung.
5. **Uhrzeit:** Weicht die Uhr eines Knotens mehr als eine Minute ab, steht ein Hinweis am Knoten; ab zwei Minuten scheitern die signierten Anfragen. Dann NTP prüfen.

**Nur auf dem Knoten selbst** gehen Netzwerk- und Firewall-Änderungen samt Bestätigung.

## Der Agent im Cluster

- Auf einem **verwaltenden Knoten** sieht der Agent die anderen Knoten:
  - `cluster_nodes`: Liste der Knoten mit Zustand.
  - `cluster_read`: Zustand eines anderen Knotens lesen (z. B. Speicher, Docker, VMs).
  - `cluster_action`: Container, VMs und Systemcontainer auf einem anderen Knoten starten oder stoppen, mit deiner Freigabe.
- Er handelt dabei als dein Konto; der andere Knoten prüft die Rechte selbst.
- Auf einem Mitglied sieht der Agent nur den eigenen Knoten.
- **Agent eines anderen Knotens:** Ein Knoten kann unter Einstellungen › KI-Agent „Agent von ADMIN-KNOTEN“ wählen und mit „Freigabe anfragen“ das Modell des Admin-Knotens nutzen. Chats, Gedächtnis, Rechte und Ordner bleiben auf dem eigenen Knoten; Modelle lädt man auf dem Admin-Knoten.

## Typische Aufgaben

### Überblick über alle Knoten

1. `cluster_nodes` aufrufen: erreichbar, Version, Weg.
2. Für auffällige Knoten `cluster_read` (Speicher, Meldungen).
3. Kurz zusammenfassen: welcher Knoten fehlt, welcher ein Update braucht, wo Speicher knapp ist.

### Knoten nicht erreichbar

1. In der Knotenliste den letzten Weg und die Antwortzeit ansehen.
2. **Wege prüfen** (Einstellungen › Cluster) fragt jeden Knoten auf jeder Adresse ab und nennt den Grund (Zeitüberschreitung, abgelehnt, nicht erlaubt).
3. Häufige Ursachen: Knoten aus oder im Neustart, VPN getrennt, Adresse geändert, Uhrzeit falsch.
4. Kurze Neustarts bei App-Updates überbrückt die Oberfläche von selbst („Knoten startet neu, gleich wieder da“).

### Dienst auf einem anderen Knoten neu starten

1. `cluster_read` des Knotens: Zustand des Containers bzw. der VM.
2. `cluster_action` vorschlagen (z. B. Container neu starten); du gibst frei.
3. Danach erneut `cluster_read` prüfen.

## Wege zwischen den Knoten

- Jeder Knoten nennt seine Adressen mit Weg: Heimnetz, Tailscale, NetBird, WireGuard, eigene Adresse (z. B. DDNS-Name).
- Für Verwaltung und Sicherungen lässt sich je ein bevorzugter Weg wählen, auch „nur diesen Weg“.
- Ab Werk nur nicht-öffentliche Adressen. „Knoten auch über das Internet erreichen“ erlaubt öffentliche; TitanOS öffnet dafür keine Ports.
- Optional ein eigener WireGuard-Tunnel zwischen den Knoten (ab Werk aus).

## Vorsicht

- Ein Knoten entfernen erneuert die Geheimnisse; ein Knoten, der dabei nicht erreichbar war, muss neu beitreten.
- Vor dem Beitritt zeigt die Vorschau, welche Konten des neuen NAS durch gleichnamige des Clusters ersetzt werden. Das genau lesen.
- Die Beitrittsinformation ist ein Geheimnis. Der Agent fragt nie danach und zeigt sie nie.
