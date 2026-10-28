# Plugins in einem eigenen Repository (Plan)

Stand: Entwurf vom 28.10.2026, noch nicht umgesetzt. Heute baut jedes volle
Release alle offiziellen Plugins neu und hängt sie an sein eigenes Release
(`docs/PLUGINS.md`). Im RAM-System liegen sie schon heute nicht: TitanOS lädt
ein Abbild erst bei der Installation direkt auf das Daten-Volume. Der Umzug
entlastet also vor allem den Release-Ablauf.

## Ziel

- Plugins kommen aus `ra5on/TitanOS-Plugins`, nicht mehr aus jedem
  TitanOS-Release.
- **Ohne Kernelbindung** (Tailscale, NetBird, Coral-USB, Intel GPU Tools):
  einmal bauen und prüfen, gültig für alle Systembasen mit derselben
  Debian-Version und demselben `SYSEXT_LEVEL`. Neuer Bau nur bei einem Update
  des Plugins selbst.
- **Mit Kernelbindung** (NVIDIA, `wangxun-nic`, `intel-qat`, `it87-extended`):
  geordnet nach dem exakten `linux-image` (`uname -r` plus Debian-Paketversion).
  Neuer Bau und Prüfung nur, wenn ein volles Release einen neuen Kernel bringt.
  Releases mit gleichem Kernel übernehmen die vorhandenen Plugins.

## Aufbau des Repositorys

- Releases mit festen Tags, nie „latest“:
  - `common-<n>`: kernelunabhängige Plugins.
  - `kernel-<uname-r>-<debianversion>-<n>`: Kernel-Plugins für genau diesen Kernel.
- Je Release eine signierte `index.json` (+ `.minisig`) mit Name, Version,
  Asset, SHA-256, Größe, optional `kernel`, `minBase` (älteste Systembasis)
  und `sysextLevel`. Plugin-Manifeste bleiben einzeln signiert wie heute.
- Signatur mit denselben Schlüsseln wie TitanOS (`titanos-release-1`), in
  einem geschützten Environment `release` mit Freigabe durch `ra5on`.

## Änderungen in TitanOS

1. `plugins/catalog.go`: Katalog zuerst aus dem Plugin-Repository
   (`common` + passender `kernel-…`-Index der **laufenden Kernelversion**),
   sonst wie bisher aus dem eigenen Release. Prüfungen: Signatur des Index,
   Tag passt exakt, `kernel` = laufender Kernel, `minBase` ≤ Basis,
   `sysextLevel` gleich. Die Regel „keine latest-Ausweichversion“ bleibt: Der
   Index wird über den exakten Kernel gefunden, nie über das neueste Release.
2. `update.PreparePlugins`: Für das Ziel eines vollen Updates den Index zum
   **Ziel-Kernel** aus dessen Manifest holen; fehlt er, wie bisher die
   Plugins des Ziel-Releases verwenden.
3. Plugin-Ablage pro Systembasis bleibt (`releases/<version>/`), damit
   Rückfall und `docs/PLUGIN-CLEANUP.md` unverändert funktionieren.

## Änderungen in der CI

- TitanOS `release.yml`: Plugin-Bau nur noch, wenn das Plugin-Repository für
  den Kernel des neuen Abbilds noch keinen Index hat; sonst überspringen und
  `smoke-release.py --plugins` mit den Plugins aus dem Plugin-Repository.
- Plugin-Repository: Workflow auf Anstoß (`repository_dispatch` aus dem
  TitanOS-Release mit Kernelversion und Abbild-URL) und bei Änderungen an
  einem Plugin; QEMU-Kurztest (Plugin aktivieren, Modul lädt, Dienst startet)
  wie heute in `smoke-storage.py`.

## Was nur der Projektinhaber tun kann

1. Repository `ra5on/TitanOS-Plugins` anlegen (öffentlich).
2. Environment `release` mit Freigabe durch `ra5on` und Tag-Regel einrichten.
3. `MINISIGN_SECRET_KEY` und `MINISIGN_PASSWORD` dort hinterlegen (wie in
   `docs/RELEASING.md` beschrieben, Passphrase interaktiv).
4. Ein Token bzw. eine GitHub-App für `repository_dispatch` vom TitanOS- in
   das Plugin-Repository.

Ohne Schritt 3 kann das Plugin-Repository nichts signieren, das TitanOS als
„Offiziell“ annimmt. Erst danach werden die Änderungen oben umgesetzt und mit
einem vollen Release geprüft; bis dahin bleibt der heutige Weg unverändert.
