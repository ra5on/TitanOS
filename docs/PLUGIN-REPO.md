# Plugins aus einem eigenen Repository

Stand 29.10.2026: Client, Bau, Tests und Workflows sind umgesetzt; die Quellen
und Build-Skripte liegen in `ra5on/TitanOS-Plugins`. Signiert wird in TitanOS,
der Schlüssel verlässt dieses Repository nicht. Solange es keinen signierten
Zeiger gibt, nimmt TitanOS die Plugins des eigenen Releases.

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

## Bau und Veröffentlichung

Im Plugin-Repository (README und RELEASING.md dort):

- Jeder Lauf baut das TitanOS-Abbild aus dem angefragten Commit, alle Plugins
  dagegen (`TITANOS` zeigt auf den Checkout) und daraus beide Sätze
  (`scripts/sets.py`). Er packt sie wie unten beschrieben in ein TitanOS-Release
  und bootet es mit den Plugins in QEMU (`smoke-release.py`,
  `smoke-storage.py`).
- Kernelunabhängige Plugins mit unveränderten Eingaben (Quellen, Lock-Datei,
  Bauskript, Katalogeintrag) behalten Bau und Version des vorigen Satzes; neue
  Versionen bietet TitanOS deshalb nur bei echten Änderungen an. `minBase` ist das
  Release, gegen dessen Abbild gebaut wurde: ältere Systeme behalten ihren Satz.
- Ein getesteter Satz wird dort nur als **Entwurf** (unveröffentlichte Release)
  angelegt, NVIDIA-Treiberupdates als Entwurf `pending-nvidia-<lauf>-<satz>`.
  Signiert wird dort nichts.

Signiert und veröffentlicht wird in TitanOS (`.github/workflows/plugin-sign.yml`):

- Alle 15 Minuten (oder von Hand) schaut der Workflow mit `PLUGINS_DISPATCH_TOKEN`
  nach Entwürfen. Gibt es welche, startet er sich selbst vom neuesten
  Release-Tag aus, der ihn enthält, denn das Environment `release` erlaubt nur
  Tags `v*`. Ein Tag reicht: Das volle Release, das auf seinen Kernel-Satz wartet,
  ist zu dem Zeitpunkt noch nicht veröffentlicht. Dieser Lauf wartet auf die Freigabe des Projektinhabers.
- Nach der Freigabe prüft er, dass der Schlüssel zu den öffentlichen Schlüsseln
  des Abbilds passt, signiert Manifeste und Index, veröffentlicht den Satz und
  erhöht `serial` im Zeiger. Treiberupdates hängt er an ihren Kernel-Satz und
  ersetzt dort nur `nvidia-updates.json`; der Entwurf wird danach gelöscht.
- Signiert wird mit `scripts/sign-plugin-set.py` aus diesem Repository (gleiche
  Dateien, Kommentare und Formate wie früher `sets.py sign`/`pointer` und
  `nvidia-update-release.py sign` dort, geprüft gegen die Schlüssel des
  Abbilds). Aus dem Plugin-Repository kommen nur die Dateien der Entwürfe;
  sein Code läuft nie neben dem Schlüssel.
- Voraussetzung: Ein Tag `v*` enthält `scripts/sign-plugin-set.py` (ab dem ersten
  Release nach 0.38.0). Ältere Tags werden für das Signieren nicht mehr gewählt.

In TitanOS (`.github/workflows/release.yml`, `scripts/plugin-set.py`):

- Ein Tag eines vollen Releases holt die Sätze für den Kernel seines Abbilds
  (`fetch`). Fehlt der Kernel-Satz, schickt er `repository_dispatch`
  `titanos-release` (Version, Commit, Kernel, Kernelpaket) und wartet, bis der
  Satz veröffentlicht ist. Das Plugin-Repository prüft, dass sein Abbild aus
  diesem Commit denselben Kernel hat.
- `import` legt die Plugins nach `out/plugins`; `make-release.sh` schreibt
  `kernelPackage` ins Manifest.

## Ältere NAS

TitanOS bis 0.35 kennt das Repository nicht. Damit ihre vollen Updates und die
manuelle Installation „aus dem gleichen Release“ weiter funktionieren, hängt die
Release-CI die Plugin-Assets des passenden Satzes **kopiert** (nicht neu gebaut)
weiter an jedes volle TitanOS-Release und führt sie im Manifest unter `plugins`.

## Was nur der Projektinhaber tun kann

1. Einen Fine-grained-Token nur für `ra5on/TitanOS-Plugins` (Contents: read and
   write) als Repository-Secret `PLUGINS_DISPATCH_TOKEN` in TitanOS hinterlegen
   (erledigt am 29.10.2026). Er stößt Bauten an, liest die Entwürfe und
   veröffentlicht sie; der Build-Job eines Releases braucht ihn vor der
   Freigabe, deshalb liegt er nicht im Environment. Er läuft ab; rechtzeitig
   erneuern.
2. Signierläufe von `plugin-sign.yml` im Environment `release` freigeben.

Das Environment `release` im Plugin-Repository und sein `MINISIGN_PASSWORD`
werden nicht gebraucht; sie können bleiben oder entfernt werden.
