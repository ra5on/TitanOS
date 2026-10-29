# Plugins aus einem eigenen Repository

Stand 29.10.2026: Der Client in TitanOS ist umgesetzt, das Repository
`ra5on/TitanOS-Plugins` ist angelegt. Bau, Signatur und Veröffentlichung der
Plugin-Sätze dort folgen. Solange es dort keinen signierten Zeiger gibt,
verhält sich TitanOS wie bisher und nimmt die Plugins des eigenen Releases.

## Ziel

- **Ohne Kernelbindung** (Tailscale, NetBird, Coral-USB, Intel GPU Tools):
  einmal bauen und prüfen, gültig für alle Systembasen mit derselben
  Debian-Version (`VERSION_CODENAME`) und demselben `SYSEXT_LEVEL`. Neuer Bau
  nur bei einem Update des Plugins selbst; installierte Plugins lassen sich dann
  ohne Systemupdate aktualisieren.
- **Mit Kernelbindung** (NVIDIA, `wangxun-nic`, `intel-qat`, `it87-extended`):
  geordnet nach dem exakten Kernel (`uname -r` plus Debian-Paketversion). Neuer
  Bau nur, wenn ein volles Release einen neuen Kernel bringt. Releases mit
  gleichem Kernel übernehmen die vorhandenen Plugins. NVIDIA-Treiberupdates
  hängen am Kernel-Satz.

## Format

Alle Dateien sind mit `titanos-release-1` signiert (`.minisig` daneben).

- **Zeiger** `releases/download/catalog/catalog.json` (feste Release `catalog`):

  ```json
  {"serial": 3, "debian": "trixie", "sysextLevel": "1",
   "common": {"tag": "common-2", "index": "<sha256 von index.json>"},
   "kernels": [{"kernel": "6.12.113+deb13-amd64", "kernelPackage": "6.12.113-1",
                "tag": "kernel-6.12.113-1-1", "index": "<sha256>"}]}
  ```

  `serial` steigt mit jeder Änderung. Gefunden wird ein Satz nur über den
  exakten Kernel, nie als „neuester“.
- **Satz-Releases** `common-<n>` und `kernel-<kernelPackage>-<n>` (ohne `+`,
  damit die Download-URLs eindeutig bleiben) mit `index.json`:

  ```json
  {"tag": "common-2", "debian": "trixie", "sysextLevel": "1", "minBase": "0.36.0",
   "kernel": "", "kernelPackage": "",
   "plugins": [{"name": "tailscale", "version": "1.92.1-1",
                "manifest": "<sha256 von tailscale.plugin.json>",
                "sha256": "<sha256 des Abbilds>", "size": 31457280}]}
  ```

  Daneben wie bisher `<name>.raw`, `<name>.plugin.json(.minisig)`,
  `<name>-licenses.tar.gz` und bei Treibern `<name>-sources.tar.gz`. Ein
  Kernel-Satz enthält außerdem `nvidia-updates.json` (mit `kernelPackage` statt
  `titanos`) und die NVIDIA-Update-Assets.
- **Plugin-Manifeste**: Version `<upstream>-<satzrevision>`, kein Suffix
  `-titanos-<release>` mehr. Kernel-Plugins nennen `kernel`; ein NVIDIA-Update
  aus einem Kernel-Satz hat kein `titanos`.

## Was TitanOS prüft (`backend/internal/plugins/repo.go`)

1. Signatur des Zeigers, `serial` nicht kleiner als die zuletzt gesehene
   (`/config/plugins/catalog-serial.json`, nur geschrieben, wenn sie steigt),
   `debian` und `sysextLevel` gleich denen des Abbilds.
2. Kernel-Satz mit `kernel` = laufender Kernel und `kernelPackage` = Debian-Version
   aus `/proc/version`; der gemeinsame Satz immer.
3. Je Satz: Signatur und SHA-256 der `index.json` gegen den Zeiger, Tag, Kernel,
   `minBase` ≤ Basis-Version (sonst gilt der Satz als nicht vorhanden).
4. Je Plugin: Signatur des Manifests und SHA-256 gegen den Index; Version,
   Abbild-Hash, Größe und Kernel gleich dem Index.

Kein passender Zeiger (404, andere Debian-Version, kein Satz für diese Basis):
die Plugins des eigenen Releases wie bisher. Kein Kernel-Satz: gemeinsamer Satz
aus dem Repository, Kernel-Plugins aus dem eigenen Release. Eine falsche Signatur,
ein falscher Hash oder eine ältere `serial` sind Fehler und nie ein Grund für den
Rückfall.

Volle Systemupdates (`PrepareUpdate`) suchen die Sätze für den **Ziel-Kernel**;
dafür trägt das Release-Manifest künftig das optionale Feld `kernelPackage`.
Fehlt es, passt nur der gemeinsame Satz, Kernel-Plugins kommen aus dem
Ziel-Release. Die Ablage pro Basis (`releases/<version>/`) und
`docs/PLUGIN-CLEANUP.md` bleiben unverändert.

Ist im Katalog eine neuere Version eines installierten offiziellen Plugins
(außer NVIDIA, das seinen eigenen Treiberweg hat), bietet die Plugin-Seite
„Aktualisieren“ an (`POST /api/v1/plugins/{name}/update` ohne `path`). TitanOS lädt
die Fassung direkt auf das Daten-Volume, prüft sie und tauscht sie wie beim
Update aus einer Freigabe: startet sie nicht, kommt die bisherige zurück.
Automatisch passiert nichts.

## Ältere NAS

TitanOS bis 0.35 kennt das Repository nicht. Damit ihre vollen Updates und die
manuelle Installation „aus dem gleichen Release“ weiter funktionieren, hängt die
Release-CI die Plugin-Assets des passenden Satzes **kopiert** (nicht neu gebaut)
weiter an jedes volle TitanOS-Release und führt sie im Manifest unter `plugins`.

## Noch offen

1. Plugin-Repository: Quellen und Build-Skripte umziehen, Workflows für Bau,
   QEMU-Kurztest, Signatur und Veröffentlichung (`repository_dispatch` aus dem
   TitanOS-Release mit Version, Kernel, kernelPackage und Abbild-URL).
2. TitanOS-CI: Plugin-Bau entfällt; `release.yml` wartet auf den Satz des neuen
   Kernels, kopiert ihn ans Release und schreibt `kernelPackage` ins Manifest.
3. Durch den Projektinhaber: Environment `release` im Plugin-Repository mit
   Freigabe durch `ra5on`, dort `MINISIGN_SECRET_KEY` und `MINISIGN_PASSWORD`
   (wie in `docs/RELEASING.md`), und ein Fine-grained-Token nur für
   `TitanOS-Plugins` (Contents: write) als `PLUGINS_DISPATCH_TOKEN` im
   TitanOS-Environment `release`.
