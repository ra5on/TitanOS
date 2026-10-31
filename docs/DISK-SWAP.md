# Platte tauschen (Assistent)

Seit 0.35.0: Speicher › Volume › **Platte tauschen …** (`/storage/replace`,
nur Besitzer und Administratoren) führt Schritt für Schritt durch den Tausch
einer Platte. Die Daten bleiben erhalten.

## Arten

- **Wiederaufbau** (Platte ausgefallen oder fehlt): Eine ausgefallene Platte
  wird zuerst aus dem Volume genommen (wie „Entfernen“), dann ausgebaut. Die
  neue Platte nimmt ihren Platz ein und wird aus den übrigen wiederaufgebaut
  (wie „Platte hinzufügen“). Fehlt die Platte ganz, zeigt der Assistent ihren
  leeren Platz.
- **Tausch im laufenden Betrieb** (mdadm, Platte arbeitet noch, z. B. mit
  SMART-Warnungen): Die neue Platte kommt als Ersatz hinzu und übernimmt die
  Daten der alten mit `mdadm --replace`; das Volume bleibt die ganze Zeit
  redundant. Danach wird die alte Platte aus dem Volume genommen und kann
  ausgebaut werden. Braucht einen freien Anschluss. Nicht bei RAID 0, nicht
  während das Volume eine Platte vermisst oder eine Prüfung/einen Wiederaufbau
  macht.
- **Btrfs mit arbeitender Platte:** neue Platte hinzufügen, danach die alte
  entfernen (ihre Daten wandern auf die übrigen Platten). Verschlüsselte
  Btrfs-Volumes fragen das Passwort für die neue Platte.

## Bedienhilfen

- **Platte finden:** liest 30 Sekunden lang nur lesend von der Platte (1-MiB-
  Blöcke über die ganze Platte verteilt, am Seitencache vorbei), damit ihre
  Aktivitätsanzeige flackert; dazu Modell und Seriennummer. Weckt eine
  schlafende Platte. Immer nur eine Platte gleichzeitig.
- Die neue Platte erkennt der Assistent selbst (Plattenliste alle vier
  Sekunden) und markiert „gerade erkannt“. Angeboten werden nur freie oder
  benutzte Platten außerhalb von Volumes, mindestens so groß wie die alte.
- Löschen der neuen Platte mit einfachem Bestätigungsdialog. Der Fortschritt
  kommt aus dem Zustand des Volumes; die Seite darf geschlossen werden.

## Technik

`storage.ReplaceDisk` (`mdadm --manage --add` und `--replace … --with …`, die
neue Platte wird wie beim Hinzufügen geprüft und partitioniert; scheitert
`--replace`, bleibt sie als Ersatzplatte) und `storage.IdentifyDisk`. API:
`POST /api/v1/storage/volumes/{name}/replace` (`old`, `new`, `erase`,
`identity`), `POST /api/v1/storage/disks/{disk}/identify` (`seconds`, 5–120).
Sonst die vorhandenen Routen zum Hinzufügen und Entfernen.

## Tempo beim Wiederaufbau

Seit 0.41.0 zeigt jede Volume-Karte eines mdadm-Volumes mit Redundanz (RAID 1,
5, 6, 10) die Zeile **Tempo beim Wiederaufbau** mit drei Stufen. Sie gilt für
Wiederaufbau, ersten Abgleich und RAID-Prüfung und wirkt sofort, auch auf einen
laufenden Vorgang:

| Stufe | `sync_speed_min` / `sync_speed_max` (KiB/s je Platte) | Wirkung |
|---|---|---|
| Schonend | 1000 / 30000 | NAS bleibt flott, das RAID ist länger ohne volle Ausfallsicherheit |
| Normal (ab Werk) | `system` / `system` (Kernel: 1000 / 200000) | schnell bei Ruhe, langsamer bei Nutzung |
| Schnell | 50000 / 2000000 | mindestens etwa 50 MB/s auch unter Last, Zugriffe solange langsamer |

Gespeichert wird die Stufe in `/config/storage/volumes.json` als `syncSpeed`
(`gentle`/`fast`, Normal ohne Eintrag); ältere Versionen ignorieren das Feld und
lassen die Kernelwerte. titand schreibt nur in `/sys/block/mdX/md/` dieses
Arrays, nie in die globalen `/proc/sys/dev/raid`-Werte. Setzt udev das Array
nach einem Neustart neu zusammen, vergisst der Kernel die Werte; titand sieht
das an der fehlenden Markierung `(local)` und setzt sie erneut. Im
Entwicklungsmodus bleibt sysfs unberührt. API:
`PUT /api/v1/storage/volumes/{name}/sync-speed` mit `{"speed":"gentle"|"normal"|"fast"}`;
`GET /api/v1/storage` nennt die Stufe je Volume als `syncSpeed`.

Geprüft mit künstlichem sysfs (`syncspeed_test.go`: Schreiben, Speichern,
erneutes Setzen nach Zusammensetzen, Normal, RAID 0, Entwicklungsmodus) und
Screenshots mit Testdaten. **Nicht geprüft:** echte Arrays und gemessene
Geschwindigkeiten.

## Prüfstand

Mit nachgebautem `lsblk`/`mdadm` und künstlichem sysfs geprüft
(`replace_test.go`): Reihenfolge der Befehle, alte Platte vor dem Kopieren
nicht gelöscht, Ablehnung bei Fehlender, laufender Prüfung, Ersatzplatte,
Btrfs, Entwicklungsmodus; „Platte finden“ im Hintergrund, nur eine. Die
Auswahl im Assistenten mit `disk-swap.test.mjs`. **Nicht geprüft:** echtes
`mdadm --replace`, echte Platten und Aktivitätsanzeigen.
