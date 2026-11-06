# PLAN 54-S · Gruppe „Schutz“

Drei Funktionen des KI-Agenten für das ganze NAS: Ransomware-Wächter, Notfallhandbuch und Notfallzugang. Besitzer und Administratoren schalten sie unter Einstellungen › KI-Agent › Funktionen ein (oder im Agenten unter Einstellungen › Funktionen). **Alle drei sind ab Werk aus.** Die Antworten des Inhabers (54-S) sind die Werte ab Werk, jede andere Antwort ist eine Einstellung (54-E).

Code: `backend/internal/protect/` (Erkennung, fanotify, SMB-Sitzungen, Zurückstellen, Notfallzugang, Handbuch), `backend/internal/api/agent_protect*.go` (Einstellungen, Routen, Tick, Werkzeuge), `web/src/lib/features/protect.ts`, `web/src/lib/components/agent/features/Protect*.svelte`, Texte in `web/src/lib/i18n/features/protect.*.json`.

## Ransomware-Wächter

**Erkennen.** titand beobachtet mit Linux-fanotify die ganzen Dateisysteme der eingebundenen Volumes (`FAN_MARK_FILESYSTEM`, `FAN_REPORT_DFID_NAME`): jedes Schließen nach dem Schreiben, jedes Umbenennen und jede neue Datei. Der Kernel meldet nur; es gibt kein Abfragen und kein Lesen ruhender Platten. Ereignisse werden über den Pfad einer Freigabe zugeordnet (titands eigene Ordner `.titanos`, `.recycle` und ausgenommene Freigaben zählen nicht) und je Freigabe und schreibendem Prozess gezählt. Ein Alarm entsteht, wenn in einem Zeitfenster (ab Werk 60 s) eine Schwelle erreicht ist:

| Zeichen | ab Werk |
|---|---|
| Umbenennungen auf bekannte Ransomware-Endungen (`.locked`, `.lockbit`, …) | 5 |
| Umbenennungen von einer normalen auf eine unbekannte Endung | 30 |
| Neu geschriebene Dateien, deren Anfang nicht mehr zur Endung passt und zufällig wirkt (Signatur + Entropie der ersten 8 KiB, höchstens 50 Dateien je Sekunde; die Platte ist dabei wach, sie wurde gerade beschrieben) | 30 |
| Erpresserbriefe (`HOW_TO_DECRYPT.txt` u. ä.) in verschiedenen Ordnern | 3 |

Temporäre Namen von Office, Editoren und Synchronisierern (`~$x.docx`, `x.docx.sb-…`, `.tmp`, `.part`) zählen nicht. Der Vorgang gilt als beendet, wenn 120 s nichts Verdächtiges mehr kam. Das Gerät findet titand über `smbstatus --json` (Prozess-ID → Konto, IP, Rechnername); bei NFS oder Programmen auf dem NAS steht der Prozessname da.

**Verhalten** (Einstellung „Verhalten“):
- *Nur melden:* Mitteilung an Besitzer und Administratoren (Push, Glocke), Meldung im Agenten.
- *Sperren und melden* (ab Werk): zusätzlich alle Schreibrechte der Freigabe auf „nur lesen“ (die alten Rechte bleiben in der Meldung gespeichert), NFS nur lesend, `smbcontrol smbd close-share` und `kill-client-ip` für das Gerät, und der Snapshot von vor dem Vorfall wird festgehalten (Kopie `guard-hold-…`, damit die Aufbewahrung von Agent- oder Zeitplan-Snapshots ihn nicht löscht).
- *Alles automatisch:* zusätzlich stellt titand die Dateien selbst zurück, sobald der Vorgang aufgehört hat. Die Freigabe bleibt nur lesbar, bis jemand sie öffnet.

**Snapshot zum Zurückgehen.** Auf Btrfs-Volumes nimmt der Wächter höchstens einmal je Stunde einen schreibgeschützten Snapshot `guard-pre-<UTC>`, und nur, während ohnehin in eine Freigabe geschrieben wird (die Platten sind wach). Er wird zwei Tage aufbewahrt, außer eine offene Meldung braucht ihn. Abschaltbar („Stündlicher Snapshot“). Für das Zurückstellen gilt der jüngste Snapshot des Volumes von vor dem ersten verdächtigen Ereignis (Zeit aus dem Namen: `guard-` in UTC, `agent-` und Zeitplan-Snapshots in Ortszeit).

**Meldung** (Ansicht `guard`, Entwurf 06): was, wann, Gerät, Konto, betroffene Dateien, dazu
- *Zurückstellen* (fragt einmal nach): jede betroffene Datei kommt aus dem Snapshot an ihren alten Platz; was jetzt dort liegt (die `.locked`-Datei oder die neu geschriebene), wandert in `.recycle/titanos-guard/<Zeit>/` der Freigabe, nichts wird gelöscht. Dateien, die der Snapshot nicht hat, bleiben unberührt. Während des Zurückstellens zählen titands eigene Schreibvorgänge nicht.
- *Freigabe wieder öffnen:* gibt genau die Rechte zurück, die der Wächter genommen hat; was ein Administrator während der Sperre geändert hat, bleibt.
- *Fehlalarm:* öffnet die Freigabe, stellt nichts zurück, und dasselbe Gerät löst eine Stunde lang auf dieser Freigabe keinen Alarm aus.
- Ohne Btrfs (ext4/XFS) oder ohne Snapshot von vorher: Sperren und Melden wie oben; die Meldung verweist auf Einstellungen › Datensicherung › Zurückstellen und darauf, dass Btrfs-Snapshots besser schützen.

**Daten.** Meldungen in `.titanos/agent/protect-guard.json` auf dem Volume des Agenten (höchstens 20 Meldungen, je Meldung höchstens 3000 Dateien; bei mehr steht die Zahl dabei). Die Rechte der Freigabe stehen wie immer in `/config` auf dem Stick; der Stick wird nur beim Sperren und Öffnen beschrieben, nie zyklisch.

**Werkzeug des Agenten:** `ransomware_guard_status` (lesen, nur Besitzer/Administratoren). Zurückstellen und Öffnen macht nur der Mensch in der Ansicht.

## Notfallhandbuch

Ein Dokument über den Aufbau des NAS und „Was tun, wenn …“ (Platte fällt aus, NAS startet nicht, Stick kaputt, Daten zurückholen, wo die Backups liegen). Inhalt: Volumes mit RAID-Art, Dateisystem und Platten (Modell, Seriennummer, Größe), Boot-Stick, Freigaben, Apps, VMs und Systemcontainer (aus der NAS-Karte der Nacht), Netzwerkadressen, Fernzugang (Tailscale-Name, NetBird-FQDN, WireGuard-Endpunkt), Datensicherungen mit Ziel. **Nie Passwörter, Schlüssel oder Tokens**: aus Sicherungszielen nur Art, Volume/Ordner, Gerät, Host/Pfad, Endpunkt/Bucket bzw. die URL ohne Benutzer und Query.

- Ansicht `handbook` (Entwurf 07) mit PDF (über `internal/office`) und Markdown zum Herunterladen.
- Neu erstellt nachts 03:20–03:59 (nach der NAS-Karte), nur wenn sich die Fakten geändert haben (Hash), nicht während das Volume des Agenten schläft; „Jetzt neu erstellen“ auf Knopfdruck. Die Speicherdaten kommen aus `Storage.Status` (lsblk/mdadm, weckt keine Platte).
- Einstellungen: Ergebnisse der Wiederherstellungsproben aufnehmen (ab Werk aus), Ansprechpartner aufnehmen mit eigenen Einträgen (ab Werk aus, höchstens 10).
- Lesen dürfen Besitzer, Administratoren und eine Person mit freigeschaltetem Notfallzugang (jedes Lesen dieser Person steht im Aktivitätsprotokoll).
- Daten: `.titanos/agent/protect-handbook.json` (Fakten, Hash, Zeit). Werkzeug `emergency_handbook` (lesen).

## Notfallzugang

- Besitzer/Administratoren tragen Personen ein (bestehende Konten), je Person Wartezeit (ab Werk 3 Tage, 1–60) und Freigaben, auf die sie lesend zugreifen darf.
- Die Person beantragt in ihrer Agent-App (Einstellungen › Funktionen › Notfallzugang) mit einem Grund, oder über das Werkzeug `emergency_access_request`. Ein nicht eingetragenes Konto kann nicht beantragen.
- Besitzer und Administratoren bekommen sofort eine Push-Mitteilung und sehen den Antrag (Ansicht `emergency`, Entwurf 08) mit **Ablehnen** und **Jetzt freigeben** (fragt einmal nach). Niemand entscheidet über den eigenen Antrag.
- Ohne Antwort wird nach der Wartezeit freigeschaltet (Tick jede Minute). Freischalten heißt: auf jeder gewählten Freigabe, auf der die Person noch **keinen** Zugang hat, bekommt sie „nur lesen“; vorhandene Rechte bleiben, es gibt nie Schreibrechte und nie mehr, als die Person schon hat. Dazu das Handbuch.
- Nach 30 Tagen endet der Zugang von selbst; ein Besitzer kann ihn vorher beenden; er endet auch, wenn die Person ausgetragen, ihr Konto gesperrt oder die Funktion ausgeschaltet wird. Zurückgenommen wird genau, was die Freischaltung hinzugefügt hat (und nur, wenn es noch „nur lesen“ ist).
- Antrag, Ablehnen, Freischalten, Ende, Zurückziehen und jedes Lesen des Handbuchs durch die Person stehen im Aktivitätsprotokoll.
- Optional (ab Werk aus): gedruckter Einmalcode je Person. Der Code wird einmal gezeigt (zum Ausdrucken), gespeichert ist nur sein SHA-256; mit ihm wird ohne Wartezeit freigeschaltet, danach ist er verbraucht.
- Daten: `.titanos/agent/protect-emergency.json` (Anträge, Fristen, Freischaltungen, Code-Hashes) auf dem Volume des Agenten; übersteht Neustarts. Die Freigaberechte selbst stehen in `/config` (nur beim Freischalten und Beenden geschrieben).

## Routen

| Route | Wer |
|---|---|
| `GET /api/v1/agent/protect/me` | jedes Konto (was davon für es da ist) |
| `GET /agent/protect/guard`, `POST /agent/protect/guard/{id}/restore\|reopen\|dismiss` | Besitzer/Admin |
| `GET /agent/protect/handbook`, `GET …/handbook/download?format=pdf\|md&lang=de\|en` | Besitzer/Admin, freigeschaltete Person |
| `POST /agent/protect/handbook/refresh` | Besitzer/Admin |
| `GET /agent/protect/emergency` | Besitzer/Admin |
| `POST /agent/protect/emergency/request` | eingetragene Person |
| `POST /agent/protect/emergency/{id}/decline\|grant\|end` | Besitzer/Admin (nicht über den eigenen Antrag) |
| `POST /agent/protect/emergency/{id}/cancel` | die Person selbst oder Besitzer/Admin |
| `POST /agent/protect/emergency/code/{account}` | Besitzer/Admin, nur mit eingeschalteten Codes |

## Geprüft

- `go test ./internal/protect`: Erkennung mit künstlichen Ereignisströmen (bekannte/unbekannte Endungen, Neuschreiben, Erpresserbriefe, normales Arbeiten mit Office-Temporärnamen ohne Alarm, langsame Umbenennungen ohne Alarm, Stummschalten nach Fehlalarm), Signatur/Entropie, Lesen von fanotify-Ereignissen aus Bytes, `smbstatus`-JSON, Trennen (keine unsicheren IPs auf der Kommandozeile), Zurückstellen aus einem Ordner als Snapshot, Notfallzugang mit falscher Uhr über einen Neustart, Ablehnen, Ende nach 30 Tagen, Einmalcode, Handbuch.
- `go test ./internal/api -run 'Protect|Guard|Emergency|Handbook'`: ab Werk aus, Rechte (Benutzer vs. Admin, fremdes Konto), ungültige Einstellungen, Sperren/Zurückstellen/Öffnen mit Temp-Freigabe und Snapshot-Ordner (fanotify durch eine Fake-Quelle ersetzt, Befehle durch einen Fake-Runner), Admin-Änderung während der Sperre bleibt, Zustand übersteht Neustart, „Nur melden“, Fehlalarm, ohne Snapshot kein Zurückstellen; Notfallzugang Wartezeit über Neustart, nicht eingetragenes Konto, nur lesen und nie mehr als vorher, Ende nach 30 Tagen, Ablehnen, Austragen beendet; Code nie wieder angezeigt; Handbuch-PDF, keine Geheimnisse aus Sicherungszielen, nur bei Änderung neu.
- `scripts/test-ui-agent54-protect.mjs` (gemockte API): Zeilen in Funktionen, Ansichten 06/07/08 bei 390×844 und 1440×900, kein seitliches Scrollen, Bestätigungsdialoge; Bilder in `out/validation/v057-protect/`.

## Grenzen, nicht geprüft

- fanotify selbst ist nicht automatisch getestet (braucht root und `CAP_SYS_ADMIN`); nicht geprüft auf echter Hardware, insbesondere `FAN_MARK_FILESYSTEM` auf Btrfs-Subvolumes (ab Kernel 6.8 möglich) und mit LUKS. Scheitert das Einrichten, zeigt die Ansicht „Der Wächter beobachtet gerade nicht: …“.
- Läuft die Ereignis-Warteschlange über (sehr viele Schreibvorgänge), gehen Ereignisse verloren; titand zählt das (`overflows`), erkennt dann aber ggf. später.
- `smbstatus --json` braucht Samba 4.16+; das Feld `server_id.pid` ist die Zuordnung. Sitzt das Gerät hinter NAT, wird die NAT-Adresse getrennt.
- Wurde der stündliche Snapshot erst mit dem ersten Ereignis des Angriffs genommen, kann er die ersten paar Dateien schon verändert enthalten; diese zählt das Zurückstellen als „fehlt“ und lässt sie liegen.
- Dateilesen über SMB/NFS durch die Person mit Notfallzugang steht nicht einzeln im Aktivitätsprotokoll (nur Antrag, Freischalten, Ende und das Lesen des Handbuchs über titand).
- Der Tick läuft nur, solange der Agent eingeschaltet ist: ist der Agent aus, startet der Wächter nicht und Fristen laufen erst weiter, wenn er wieder an ist (siehe Bericht, Änderung an gemeinsamen Dateien).
- Die Push-Links (`/agent?feature=guard|emergency`) öffnen die Ansicht erst, wenn die Agent-Seite den Parameter auswertet (gemeinsame Datei).
