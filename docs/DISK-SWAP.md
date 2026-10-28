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

## Prüfstand

Mit nachgebautem `lsblk`/`mdadm` und künstlichem sysfs geprüft
(`replace_test.go`): Reihenfolge der Befehle, alte Platte vor dem Kopieren
nicht gelöscht, Ablehnung bei Fehlender, laufender Prüfung, Ersatzplatte,
Btrfs, Entwicklungsmodus; „Platte finden“ im Hintergrund, nur eine. Die
Auswahl im Assistenten mit `disk-swap.test.mjs`. **Nicht geprüft:** echtes
`mdadm --replace`, echte Platten und Aktivitätsanzeigen.
