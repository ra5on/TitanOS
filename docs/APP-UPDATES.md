# App-Updates ohne Neustart

Stand: 21. Oktober 2026 · Entwurf vom Projektinhaber am 21. Oktober 2026 bestätigt, **umgesetzt für 0.8.0**. Was davon abweicht und wie es geprüft ist, steht unter „Stand der Umsetzung“ am Ende.

## Ziel

Die meisten Releases ändern nur titand und die eingebettete Weboberfläche (0.7.1, 0.7.2). Trotzdem baut die CI heute jedes Mal das ganze Debian-Abbild, alle Plugins und startet es zweimal in QEMU (rund eine Stunde). Auf dem NAS folgt ein Neustart. Container, VMs und Freigaben sind dabei für einige Minuten weg.

Ein **App-Update** tauscht nur titand aus:

- In der CI etwa 15 statt 60 Minuten.
- Auf dem NAS rund 20 MB Download statt rund 700 MB, und **kein Neustart**. Container, VMs und Freigaben laufen weiter.
- Signiert und mit Rückfall wie heute.

Volle System-Updates bleiben für alles andere: Debian-Pakete und Sicherheitsupdates, Kernel, initramfs, systemd-Einheiten, `mkosi.extra`.

## Begriffe

- **Basis:** ein von mkosi gebautes Systemabbild. Seine Version steht neu in `/usr/share/titanos/base-version` und ändert sich nur bei einem vollen Build.
- **App:** die Datei `titand` mit eingebetteter Weboberfläche.
- **Version eines App-Releases**, zum Beispiel `0.8.1` auf Basis `0.8.0`. Die Oberfläche zeigt „TitanOS 0.8.1 (Basis 0.8.0)“.

## Was ein App-Update ändern darf

Nur `titand`. Alles, was titand nicht selbst mitbringt, braucht ein volles Release. Die CI prüft das, statt sich auf eine Angabe zu verlassen (siehe „CI“).

| Darf | Darf nicht |
|---|---|
| Go-Code von titand, Weboberfläche, Übersetzungen | Dateien unter `build/` (Pakete, `mkosi.extra`, Einheiten, Postinst) |
| neue API-Endpunkte, neue Felder in `/config`-Dateien | Kernel, initramfs, Firmware, Boot-Einträge |
| neue Aufrufe vorhandener Programme | neue Programme oder Bibliotheken, die im Abbild fehlen |

**Regel für `/config`:** Die Basis-titand-Version und die vorige App-Version müssen alles weiter lesen können, was die neue schreibt. Also nur Felder dazu, keine Umbenennungen und keine neuen Pflichtfelder. Das gilt heute schon für den A/B-Rückwechsel.

## Releases bleiben vollständig

Jede ältere Version von titand (bis 0.7.x) lehnt ein Manifest ohne die drei Slot-Dateien ab (`parseManifest`) und lädt immer die Dateien des Releases aus `manifest.Tag`. Damit ältere NAS weiter normal aktualisieren und neue Sticks ein stimmiges Abbild bekommen, enthält **auch ein App-Release ein vollständiges Abbild**. Es wird aber nicht neu gebaut:

1. Die CI lädt das Slot-Abbild der Basis aus deren Release und prüft Signatur und SHA-256.
2. `vmlinuz` und `initrd.img` werden bitgleich übernommen.
3. `filesystem.squashfs` wird entpackt, nur `/usr/bin/titand` ersetzt und mit denselben Optionen wieder gepackt (`unsquashfs` und `mksquashfs` dauern ein paar Minuten, kein mkosi, kein apt).
4. Die Plugins der Basis werden übernommen; der Kernel ist ja derselbe. Die CI prüft ihre Signaturen, und der Publish-Job hängt sie an das neue Release.
5. Das Installations-ZIP entsteht aus denselben Dateien.

Ein NAS **ohne** App-Unterstützung, also bis zur ersten Basis mit diesem Mechanismus, macht damit wie bisher ein volles Update mit Neustart. Ein NAS **mit** App-Unterstützung und passender Basis lädt nur `titand`.

## Manifest

Das bestehende Format bleibt gleich. Dazu kommt ein optionales Feld `app`, das ältere Versionen beim Lesen ignorieren (`encoding/json` überspringt unbekannte Felder):

```json
{
  "version": "0.8.1",
  "tag": "v0.8.1",
  "channel": "stable",
  "kernel": "6.12.48+deb13-amd64",
  "files": [ "… vmlinuz, initrd.img, filesystem.squashfs wie bisher …" ],
  "plugins": [ "… wie bisher …" ],
  "app": {
    "base": "0.8.0",
    "asset": "titanos-0.8.1-titand",
    "sha256": "…",
    "size": 19000000
  }
}
```

- Fehlt `app`, ist es ein volles Release; dann gibt es nur den bisherigen Weg.
- Signiert wird wie heute das ganze Manifest mit dem Release-Schlüssel. `app.sha256` ist damit abgedeckt.

## Ablage auf dem Stick

Je Slot ein Ordner `app` neben den Slot-Dateien:

```
titanos/a/
├── vmlinuz, initrd.img, filesystem.squashfs, version
└── app/
    ├── state.json          # welche App gilt, Probezustand, Versuche
    ├── titand-0.8.2        # aktuelle App
    ├── manifest-0.8.2.json + .minisig
    ├── titand-0.8.1        # vorige App, für den Rückfall
    └── manifest-0.8.1.json + .minisig
```

- Es liegen höchstens zwei App-Versionen da, die aktuelle und die vorige. Das sind etwa 40 MB je Slot.
- Ein volles System-Update in den anderen Slot schreibt dort keinen `app`-Ordner. Das neue Abbild bringt seine titand-Version im squashfs mit.
- Der Rückwechsel auf den anderen Slot nimmt dessen App-Stand mit. Slot und App bleiben also immer zusammen.

`state.json`:

```json
{ "current": "0.8.2", "previous": "0.8.1", "trial": true, "tries": 1, "change": "update" }
```

`result` und `resultVersion` halten fest, was aus der letzten Änderung wurde (`installed`, `failed`, `rolled_back`), bis titand es einmal gemeldet hat.

## Start: titand im Abbild entscheidet

`/usr/bin/titand` im squashfs (die Basis) startet immer zuerst und entscheidet ganz am Anfang, noch vor dem Lesen von `/config`:

1. `titanos/<laufender Slot>/app/state.json` lesen. Fehlt die Datei oder gilt keine App, läuft die Basis selbst weiter.
2. Prüfen:
   - Manifest-Signatur mit den Schlüsseln aus dem Abbild,
   - `app.base` gleich der eigenen `base-version`,
   - SHA-256 der Datei.
   
   Schlägt etwas fehl: die vorige App nehmen, sonst die Basis. Das wird protokolliert und gemeldet.
3. Die Datei nach `/var/lib/titanos/app/titand-<version>` kopieren (im RAM, auf dem Wurzeldateisystem: `/run` erlaubt keine Programme), die Kopie noch einmal prüfen und mit `exec` starten, mit `TITANOS_APP=<version>` in der Umgebung, damit sie sich nicht noch einmal selbst sucht. `/var/lib/titanos/app/running` nennt die gestartete Version.
4. Läuft die App auf Probe, zählt die Basis vor dem `exec` `tries` hoch (ein Schreibzugriff auf den Stick, nur während der Probe). Ab drei Versuchen gilt die App als gescheitert. Dann wird `current` zu `previous`, und es wird zurückgewechselt.

**Was das kostet:** ein Lesen vom Stick bei jedem Start von titand (Automount wie heute) und etwa 20 MB mehr RAM für die Kopie in `/run`. Die Prüfung gegen das Manifest schützt vor beschädigten Dateien. Schutz gegen jemanden mit Zugriff auf den Stick ist das nicht; der könnte das squashfs genauso tauschen.

Diesen Teil muss **eine volle Basis-Version** einführen, zum Beispiel 0.8.0. Erst ab dieser Basis sind App-Updates möglich.

## Ablauf eines App-Updates

1. **Prüfen**, einmal am Tag wie heute. Das Manifest hat `app`, `app.base` passt zur laufenden Basis, und die Version ist neuer: Die Oberfläche bietet ein „Update ohne Neustart“ an. Passt die Basis nicht, gibt es das bekannte volle Update.
2. **Warten, bis titand frei ist** (siehe „Was ein Neustart von titand unterbricht“). Mit „von selbst installieren“ zur eingestellten Uhrzeit, sonst auf Knopfdruck. Laufende Aufträge werden angezeigt.
3. **Laden** nach `titanos/<slot>/app/` in eine Teildatei, mit Fortsetzen wie bei den Slot-Dateien. Danach SHA-256 prüfen und umbenennen; Manifest und Signatur liegen daneben.
4. **`state.json` schreiben:** `current` ist die neue Version, `previous` die alte, `trial` ist wahr, `tries` steht auf 0. Atomar über eine temporäre Datei und Umbenennen.
5. **titand neu starten**, zwei Sekunden später, damit die letzten Antworten noch hinausgehen: `systemd-run … systemctl restart titand.service titanos-app-check.service`. Der Aufruf läuft in einer eigenen Einheit, damit systemd ihn beim Beenden von titand nicht mit beendet. Die Basis startet die neue App.
6. **Probe:** Eine eigene Einheit `titanos-app-check.service` führt immer die Basis aus (`/usr/bin/titand -check-app`), wie heute `titanos-boot-check`. Sie wartet bis zu fünf Minuten, bis `/api/v1/health` „bereit“ meldet, mit denselben Kriterien wie nach einem System-Update: Konfiguration gelesen, Volumes versorgt, alles eingebunden, was vorher eingebunden war. Außerdem muss `/var/lib/titanos/app/running` die neue Version nennen. (`/api/v1/health` gibt die Version bewusst nicht heraus, auch nicht an Anfragen vom NAS selbst: Ein lokaler Proxy wie Tailscale Serve würde sie sonst nach außen reichen.)
   - **Gelungen:** `trial` ist falsch, `tries` steht auf 0. Meldung „installiert“.
   - **Gescheitert:** zurück auf `previous`, titand neu starten, Meldung „fehlgeschlagen, zurückgewechselt“. Die gescheiterte Version wird nicht noch einmal von selbst angeboten.
7. **„Auf vorherige Version zurück“** tauscht `current` und `previous` und startet titand neu, ebenfalls auf Probe. Gibt es keine vorige App, ist das Ziel die Basis.

Das ganze Update dauert für die Nutzer etwa 10 bis 30 Sekunden ohne Weboberfläche. SMB, NFS, Container und VMs bleiben erreichbar.

## Was ein Neustart von titand unterbricht

Vor der Umsetzung geprüft; die Spalte „Vorschlag“ ist umgesetzt, siehe auch „Stand der Umsetzung“:

| Betroffen | Heute | Vorschlag |
|---|---|---|
| Anmeldungen | liegen nur im RAM von titand und gehen verloren | in `/run/titanos/sessions` (tmpfs, 0600) halten: Sie überstehen einen titand-Neustart, aber keinen NAS-Neustart |
| Kopier- und Verschiebeaufträge, Platten-Prüfungen, Kopieren von VM-Platten, Snapshot-Vorgänge | im RAM bzw. als Kindprozesse von titand | Update wartet, bis keiner läuft; die Oberfläche zeigt, worauf |
| laufende Backups (restic, rsync) | Kindprozesse in der cgroup von titand, `systemctl restart` beendet sie | wie oben: warten |
| Terminal- und Konsolensitzungen | enden | vorher Hinweis; nicht blockierend |
| Uploads | werden in Stücken fortgesetzt | nichts nötig |
| Docker, VMs (libvirt), smbd, nfsd, networkd, NUT | eigene Dienste | prüfen, dass der Start von titand ohne Änderung nichts neu startet: smbd nur `reload`, Netzwerk nur bei geänderten Dateien, Docker und libvirt nicht anfassen |
| Netzwerk-Probezeit, ausstehendes System-Update | Zustände in `/config` bzw. im RAM | Update sperren, solange etwas auf Probe läuft |

## Was in der CI passiert

Ob ein Release ein App-Release wird, entscheidet der Release-Workflow selbst:

- **Basis bestimmen:** das neueste volle Release (Tag mit `base-version` gleich der eigenen Version).
- **Prüfen, was sich seit der Basis geändert hat:** `git diff --name-only <basis-tag>..HEAD`. Geändert sein dürfen nur `backend/`, `web/`, `docs/` und Testskripte, außerdem darf sich `go.mod` nicht auf cgo oder Systembibliotheken auswirken. Alles andere ergibt automatisch ein volles Release. Ein volles Release lässt sich auch erzwingen, etwa für Debian-Sicherheitsupdates.
- **Ablauf für ein App-Release:**
  1. `make test`, Svelte-Prüfung, Browserprüfungen.
  2. titand bauen (Version, Basis-Version).
  3. Abbild der Basis laden, prüfen, titand ersetzen (siehe oben), Plugins übernehmen.
  4. In QEMU mit Wegwerf-Schlüssel:
     - das ZIP starten,
     - ein App-Update von der Basis auf die neue Version ohne Neustart,
     - eine absichtlich kaputte App, die nach drei Versuchen auf die vorige zurückfällt,
     - „zurück auf vorherige Version“.
  5. Publish-Job wie heute, mit Freigabe im Environment `release`: Manifest, ZIP, Prüfsummen und Plugin-Manifeste signieren.

Erwartete Dauer: etwa 15 Minuten statt etwa 60.

**Debian-Sicherheitsupdates:** Ein wöchentlicher Lauf vergleicht die Paketversionen der Basis mit dem aktuellen Debian-Stand und öffnet einen PR für ein volles Release, wenn sicherheitsrelevante Pakete neuer sind. Ohne diesen Lauf würden reine App-Releases die Basis unbemerkt altern lassen.

## Oberfläche (Einstellungen › Updates)

- Anzeige: „TitanOS 0.8.2 · Basis 0.8.0 · Slot A“, dazu der Rückfall-Slot wie heute.
- Angebot mit Art des Updates: „Update ohne Neustart (ca. 20 MB)“ oder „System-Update mit Neustart (ca. 700 MB)“.
- Läuft etwas, das ein Neustart von titand abbrechen würde, lehnt TitanOS das App-Update mit einer Meldung ab (`update_working`). „Von selbst installieren“ wartet stattdessen bis zu sechs Stunden ab der eingestellten Uhrzeit.
- Nach dem Neustart von titand verbindet sich die Seite selbst neu und zeigt das Ergebnis der Probe.
- „Auf vorherige Version zurück“ getrennt für App und System.

## Umsetzung in Schritten (Entwurf)

1. **Basis vorbereiten (volles Release, zum Beispiel 0.8.0):**
   - `base-version` im Abbild,
   - Starter in `/usr/bin/titand`,
   - `titanos-app-check.service`,
   - Anmeldungen in `/run`,
   - Start von titand ohne unnötige Neustarts von Diensten,
   - Sperre während laufender Aufträge.
   
   Ein App-Update gibt es da noch nicht.
2. **Updater:** Manifest-Feld `app`, Laden in `app/`, `state.json`, Probe und Rückfall, Meldungen, Oberfläche. Go-Tests mit nachgebautem Release-Server wie bei den heutigen Updates.
3. **CI:** Basis bestimmen, erlaubte Änderungen prüfen, Abbild neu packen, QEMU-Szenarien, Plugins übernehmen, Prüflauf für Debian-Pakete.
4. **Doku:** `RELEASING.md`, Release-Notizen mit der Art des Updates, `CLAUDE.md` (Abschnitt „Updates über GitHub“).
5. **Abnahme:** in QEMU wie beim Beta-Update am 14. Oktober 2026, danach auf echter Hardware.

## Entscheidungen (21. Oktober 2026)

1. **Versionen:** App-Releases sind normale Patch-Versionen (0.8.1, 0.8.2 … auf Basis 0.8.0). Eine neue Basis bekommt eine neue Minor-Version (0.9.0). Die Oberfläche nennt die Basis zusätzlich.
2. **Anmeldungen** liegen in `/run/titanos/sessions` (tmpfs, nur root, Tokens nur als Hash). Sie überstehen einen Neustart von titand, aber keinen Neustart des NAS.
3. **Von selbst installieren:** App-Updates laufen wie System-Updates zur eingestellten Uhrzeit und warten zusätzlich, bis keine Aufträge oder Backups mehr laufen.
4. **Rückfall:** zuerst auf die vorige App, dann auf die Basis.
5. **Debian-Sicherheitsupdates:** Ein wöchentlicher CI-Lauf vergleicht die Paketversionen der Basis. Gibt es Sicherheitsupdates, öffnet er einen PR für ein volles Release. Veröffentlicht wird erst nach Freigabe durch den Projektinhaber.

## Stand der Umsetzung (0.8.0)

**Backend**
- Paket `app` (`backend/internal/app`): Ablage auf dem Stick, `state.json`, Prüfung (Signatur, Basis, SHA-256), Start (`Launch`), Installieren, Rückweg, Probe (`Confirm`) und das einmalige Melden des Ergebnisses.
- `cmd/titand`: Übergabe an die App ganz am Anfang (`handOver`, nur ohne `-dev` und mit Basis-Version), `-check-app` für `titanos-app-check.service`.
- Paket `update`: Manifest-Feld `app`, `Status.base`, `Status.kind` (`app` oder `system`), `Status.app`. App-Update installieren (`installApp`), `POST /api/v1/update/app-rollback`. Ein System-Update leert den `app`-Ordner des Ziel-Slots und bereitet die Plugins für die Basis des Releases vor.
- Plugins richten sich nach der Basis-Version, nicht nach der Version von titand (Katalog, NVIDIA-Build-Umgebung, Treiber-Updates).
- Was ein Neustart von titand nicht mehr anfasst:
  - Anmeldungen bleiben in `/run/titanos/sessions.json`.
  - Netzwerk: Unveränderte Dateien führen zu keinem `networkctl reload/reconfigure`.
  - Docker: Läuft die Engine mit genau dieser `daemon.json`, wird sie übernommen statt neu gestartet. Die Container laufen weiter.
  - NUT: Unveränderte Dateien und laufende Dienste bleiben, das Passwort von upsmon wird übernommen.
  - VM-Autostart: Die Liste in `/run/titanos/vms-autostarted` verhindert, dass eine absichtlich ausgeschaltete VM nach einem App-Update wieder startet.
  - Exklusiv reservierte Kerne werden beim Start abgeglichen.
- Samba (`reload-or-restart`), NFS-Bind-Mounts und Plugins (`systemd-sysext refresh` mit unveränderter Menge) waren schon unkritisch.
- Ein App-Update wartet auf Kopier- und Verschiebeaufträge, Backups, Plattenprüfungen, das Kopieren von VM-Platten, Snapshot-Vorgänge und NVIDIA-Treiber-Builds (`update.Working`).
- `titand.service` erlaubt zehn Starts in zwei Minuten. So kommt der Rückfall nach drei Fehlstarts sicher vor der Startgrenze von systemd.

**Abbild:** `/usr/share/titanos/base-version` (Postinst) und `titanos-app-check.service` (Preset).

**Release und CI:**
- `scripts/release-kind.py` entscheidet die Art des Releases.
- `scripts/repack-release.py` packt das Abbild der Basis mit dem neuen titand neu.
- `make-release.sh` legt `titanos-<version>-titand` und `app` ins Manifest. `verify-release.py` prüft beides; Plugins und das NVIDIA-SDK müssen zur Basis passen.
- `debian-security.yml` (mit `scripts/debian-security.py`) läuft wöchentlich.
- Siehe [RELEASING.md](RELEASING.md).

**Geprüft**
- Go-Tests:
  - Paket `app`: Probe, drei Fehlstarts, beschädigte, fremd signierte und für ein anderes Abbild gebaute Apps, Rückweg zur vorigen App und zur Basis.
  - Paket `update`: App-Update lädt nur titand, wartet auf Arbeit, meldet installiert/fehlgeschlagen/zurück. Ein Release für ein anderes Abbild ist ein System-Update, das den `app`-Ordner leert und die Plugins der Basis vorbereitet.
  - `cmd/titand`: Probe gelungen und gescheitert.
  - Anmeldungen über einen Neustart.
  - Neustart von titand ohne Eingriff in Netzwerk, Docker und NUT.
  - VM-Autostart einmal je Start des NAS.
- Release-Tests:
  - App-Release auf einem älteren Abbild,
  - Neu-Packen gegen einen nachgebauten Release-Server,
  - Ablehnen einer fremd signierten Basis,
  - titand muss der im Abbild sein,
  - Basis-Version im Abbild.
- Tests für `release-kind.py` und `debian-security.py`.
- Browserprüfung `scripts/test-ui-updates.mjs`: Angebot ohne Neustart, Neustart der Weboberfläche, Probe, Rückweg, System-Update auf dem Smartphone.
- QEMU (`scripts/smoke-app-update.py`, in jedem Probelauf von `release.yml`):
  - App-Update ohne Neustart des NAS, mit bestehender Anmeldung; bestätigt, gemeldet und nach einem Neustart des NAS noch da.
  - Eine App, die nicht startet, wird nach drei Starts aufgegeben und gemeldet.
  - Rückweg zum titand des Systemabbilds.
- Lokal am 21. Oktober 2026 mit KVM bestanden, auf einer Testbasis „0.8.0“: das Abbild von 0.7.2 mit dem titand, den Einheiten und der Basis-Version dieses Stands und einem Wegwerf-Schlüssel, ohne Root umgepackt. Dabei gemessen:
  - Die App übernimmt rund drei Sekunden nach dem Download und ist drei Sekunden später bestätigt.
  - Eine App, die nicht startet, ist nach drei Fehlstarts in rund sieben Sekunden aufgegeben.
- Dabei gefunden und behoben:
  - `/run` erlaubt keine Programme; deshalb liegt die Kopie der App unter `/var/lib/titanos/app`.
  - systemd beendete den Aufruf `systemctl restart` zusammen mit titand; jetzt läuft er über `systemd-run`.
  - Bei schnellem Download war titand weg, bevor die Antwort auf „Installieren“ draußen war; jetzt startet titand zwei Sekunden später neu.

**Grenzen**
- Der QEMU-Test läuft nur in Probeläufen. Ein echtes App-Release-Tag startet sein ZIP in QEMU (mit Plugins), ein App-Update spielt es aber nicht durch: Dafür bräuchte die CI einen Testschlüssel im veröffentlichten Abbild.
- Das erste echte App-Release ist erst nach 0.8.0 möglich.
- Nicht auf echter Hardware geprüft.

