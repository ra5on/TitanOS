# Autopilot für Sicherheitsupdates

Der Autopilot bringt Debian-Updates zu allen NAS, auch wenn niemand am Projekt
arbeitet: jede Nacht prüfen, bei Bedarf ein Sicherheits-Release bauen, testen,
signieren und veröffentlichen. Kernel-Updates gehören dazu. Entscheidung des
Projektinhabers vom 3.11.2026: voll automatisch, ohne Freigabe, ohne
Staffelung (alle NAS bekommen das Update sofort).

**Wichtig für Kernel-Updates:** Ein neuer Kernel braucht neue Plugin-Sätze.
Ohne `AUTOPILOT_PLUGINS=on` wartet das Release auf die Freigabe der
Plugin-Signatur, nach rund fünf Stunden bricht es ab (Issue), und der
Autopilot wartet auf diesen Tag. Kernel-Updates laufen also nur mit
`AUTOPILOT_PLUGINS=on` wirklich ohne den Inhaber.

## Ablauf

1. **Prüfen** (`.github/workflows/autopilot.yml`, jede Nacht 02:23 UTC):
   `scripts/debian-security.py` vergleicht die Debian-Pakete des neuesten
   veröffentlichten Releases (aus `titanos-<version>-sources.txt`) mit
   `trixie`, `trixie-updates` und `trixie-security`. Die zusätzlichen Pakete
   des Abbilds mit Kernel „Neu“ (eigener Abschnitt in der Quellenliste)
   werden zusätzlich mit `trixie-backports` verglichen. Point-Releases zählen:
   Debian bringt Kernel-ABI-Wechsel oft über sie und nicht über
   trixie-security. Nichts Neues: Ende.
2. **Tag wählen** (`scripts/autopilot.py plan`): die nächste freie Version
   nach dem neuesten veröffentlichten Release. Ab 0.72 ist das die nächste
   zweite Zahl (0.74 → 0.75, Tag `v0.75.0`; Entscheidung vom 10.11.2026),
   davor war es der nächste Patch (0.45.1 → 0.45.2). Vorhandene Tags werden
   übersprungen. Der Autopilot wartet,
   - solange ein eigener Tag noch kein Release hat (baut noch, wartet auf
     Plugin-Sätze oder ist gescheitert: nie ein zweiter Tag dafür),
   - solange ein Tag des Inhabers, der neuer als das letzte Release ist,
     jünger als drei Tage ist (sein Release ist unterwegs).
3. **Schneiden:** vom Tag des neuesten Releases genau ein Commit, der nur
   `build/debian-security.txt` ändert (Paketliste und Prüfdatum), dann ein
   Tag mit der Betreffzeile `Sicherheitsupdate (Autopilot)`. Seine Nachricht
   ist der Changelog: Pakete, Rückweg und „Nicht auf echter Hardware
   geprüft“. Gebaut wird **nicht von `main`**, also kommen keine halbfertigen
   Funktionen mit. Der Code von titand bleibt der des letzten Releases.
4. **Bauen und prüfen** (`release.yml`, gestartet mit `publish=true` auf dem
   Tag): `build/` hat sich geändert, also ein volles Release mit mkosi, beide
   Kernel. Hat sich der Kernel geändert, bestellt der Workflow wie bei jedem
   Release den Plugin-Satz beim Plugin-Repository und wartet darauf. Danach
   laufen alle Tests des Release-Workflows: Abbildprüfung, QEMU-Starts mit
   UEFI, Legacy-BIOS, 32-Bit-UEFI und Kernel „Neu“, Hardware-Varianten,
   Speicher, SSD-Cache, Plugins. Zusätzlich prüft `scripts/autopilot.py
   verify`, dass jedes Update der Liste in den gebauten Abbildern steckt
   (gleiche oder neuere Version laut `components.json`, je Abbild). Fehlt
   eines, wird nicht veröffentlicht: Sonst fände die nächste Nacht dasselbe
   Update wieder und schnitte dasselbe Release noch einmal.
5. **Veröffentlichen ohne Freigabe:** `scripts/autopilot.py guard` prüft aus
   git, ob der Tag ein Sicherheits-Release ist. Nur dann signiert der
   Publish-Job im Environment `security-release` (ohne Freigabe), sonst wie
   immer in `release` (mit Freigabe). Die Bedingungen:
   - der Commit hat genau einen Elternteil, und der trägt den Tag eines
     **veröffentlichten** Releases,
   - die Version folgt auf dieses Release (keine Vorabversion): ab 0.72
     dieselbe Hauptzahl, eine höhere zweite Zahl und dritte Zahl 0; bis 0.71
     ein späterer Patch derselben Minor-Version,
   - der Commit ändert **nur** `build/debian-security.txt`.
   Die Tag-Nachricht spielt dabei keine Rolle.
6. **Auf dem NAS:** Das Update erscheint über die normale Suche. Mit „Von
   selbst installieren“ wird es zur eingestellten Uhrzeit installiert. Ein
   Fehlstart fällt über A/B auf die vorige Version zurück.

**Fehler:** Scheitert der nächtliche Lauf oder ein Autopilot-Release, öffnet
der Workflow ein Issue mit dem Label `autopilot` (oder kommentiert das
offene). Die NAS bleiben auf ihrer Version. Solange der gescheiterte Tag ohne
Release besteht, schneidet der Autopilot keinen neuen; nach der Klärung den
Tag löschen oder den Release-Lauf wiederholen.

## Schalter

| Repository-Variable | Wirkung |
|---|---|
| `AUTOPILOT=on` | Der Autopilot schneidet und veröffentlicht. Ohne die Variable (ab Werk) öffnet der Lauf wie früher die wöchentliche Prüfung nur einen PR mit dem Label `debian-security`, der den nächsten Tag des Inhabers zu einem vollen Release macht. Das ist zugleich die Notbremse. |
| `AUTOPILOT_PLUGINS=on` | Ohne Freigabe signiert (`plugin-sign.yml`, Environment `security-release`): die Kernel- und gemeinsamen Sätze eines wartenden Autopilot-Releases, und ein gemeinsamer Satz des nächtlichen Laufs für neue Plugin-Versionen, der sich nur in neueren Versionen von Tailscale oder NetBird unterscheidet. NVIDIA-Treiber-Updates behalten immer die Freigabe. Ohne die Variable wartet ein Release mit neuem Kernel auf deine Freigabe der Plugin-Signatur. |

Ein veröffentlichtes Release lässt sich wie jedes andere zurückziehen
(Release auf GitHub löschen oder als Entwurf setzen). NAS, die es schon
installiert haben, behalten es; über Einstellungen › Updates geht es zurück.

## Neue Plugin-Versionen (Tailscale, NetBird)

Entscheidung des Inhabers vom 3.11.2026: neue Versionen der Plugins mit
festgehaltener Herstellerdatei kommen ohne Freigabe zu allen NAS.

1. **Nachtlauf im Plugin-Repository** (`.github/workflows/upstream.yml`,
   04:47 UTC): `scripts/upstream.py check` vergleicht Tailscale (stabile
   Pakete von pkgs.tailscale.com) und NetBird (neuestes Release auf GitHub,
   keine Vorabversion) mit den Lock-Dateien.
2. **Anheben:** `scripts/upstream.py bump` hält Archiv-URL, Größe und
   SHA-256 fest und holt die Lizenztexte. Die Lizenz des Programms selbst
   muss wortgleich bleiben; ändert sie sich, hebt der Lauf nichts an und
   öffnet ein Issue mit dem Label `upstream`. Die Listen der Fremdlizenzen
   werden erneuert (NetBird: aus dem Binary erzeugt).
3. **Bauen:** Commit auf `main`, dann `release.yml` mit `sets=common`. Nur die
   geänderten Plugins werden neu gebaut, alle Plugins mit dem Abbild in QEMU
   getestet; der Satz wird ein Entwurf.
4. **Signieren:** `plugin-sign.yml` prüft den Entwurf mit
   `scripts/plugin-guard.py` gegen den signierten veröffentlichten
   gemeinsamen Satz: gleiche Plugins, gleiche Kernel- und Debian-Angaben,
   jedes andere Plugin unverändert, Tailscale oder NetBird nur mit neuerer
   Herstellerversion. Dann signiert es ohne Freigabe, und zwar wie immer von
   einem Release-Tag `v*` aus (kein eigener Tag je Satz, Entscheidung
   3.11.2026), sonst mit Freigabe. Die Prüfung läuft vor dem Start und noch
   einmal im Job mit dem Schlüssel.
5. **Auf dem NAS:** Der Katalog nennt den neuen Satz; installierte Plugins
   werden über die Plugin-Seite aktualisiert.

Ohne Freigabe geht das erst ab dem ersten Release, das
`scripts/plugin-guard.py` enthält (0.46.3); bis dahin fragt `plugin-sign`
wie bisher. NVIDIA-Treiber baut jedes NAS selbst (`docs/NVIDIA-LOCAL-BUILD.md`,
PLAN.md Punkt 31).

## Grenzen

- **Erst ab dem ersten Release mit Autopilot.** GitHub startet den
  Release-Workflow in der Fassung des Tags. Ist das neueste Release älter als
  der Autopilot, öffnet der Lauf stattdessen den PR. Der Autopilot greift ab
  dem ersten Release des Inhabers, das ihn enthält.
- **Kein Test auf echter Hardware.** Für Sicherheits-Releases ersetzen die
  QEMU-Tests des Release-Workflows den Test auf dem echten Stick
  (`docs/RELEASING.md`). Ein Fehler, den diese Tests nicht finden, erreicht
  alle NAS gleichzeitig; A/B fängt einen Fehlstart ab.
- **Kernel „Neu“:** Ein Autopilot für beide Kernel; jedes Release trägt
  beide Abbilder, und der Release-Workflow bestellt die Plugin-Sätze für
  beide (seit 0.47.0; das `~` der Backports-Version wird im Tag zu `_`,
  z. B. `kernel-7.2.11-1_bpo13_1-1`). Der Satz für „Neu“ verlängert den
  Lauf um einen zweiten Plugin-Bau.
- **Plugin-Signatur ohne Freigabe** (`AUTOPILOT_PLUGINS=on`) heißt: Was das
  Plugin-Repository baut, wird als Kernelmodul signiert und an alle NAS
  verteilt. Das Repository muss dafür so vertrauenswürdig sein wie dieses.
- Der Autopilot erzeugt nur Sicherheits-Releases auf dem letzten Release
  (einzige Änderung: `build/debian-security.txt`). Neue Funktionen und
  App-Releases bleiben beim Inhaber. Weil seit 0.72 jedes Release die zweite
  Zahl erhöht, kann der Autopilot eine Nummer belegen, die der Inhaber für
  sein nächstes Release vorgesehen hatte; dieses nimmt dann die nächste.

## Einrichtung (einmalig, durch den Inhaber)

1. Environment `security-release` anlegen: **ohne** erforderliche Prüfer,
   Bereitstellung nur von Tags `v*` (wie `release`).
2. Dort die Secrets `MINISIGN_SECRET_KEY` und `MINISIGN_PASSWORD` setzen
   (dieselben wie in `release`; GitHub kann Secrets nicht kopieren):
   ```
   gh secret set MINISIGN_SECRET_KEY --repo ra5on/TitanOS --env security-release < titanos-release-1.key
   gh secret set MINISIGN_PASSWORD --repo ra5on/TitanOS --env security-release
   ```
3. Empfohlen: Tag-Regel (Ruleset) für `v*`, damit nur der Inhaber und GitHub
   Actions Release-Tags anlegen können. Wer Tags anlegen kann, kommt sonst an
   das Environment ohne Freigabe.
4. Unter „Settings › Actions › General“ muss „Allow GitHub Actions to create
   and approve pull requests“ an sein (für den PR-Weg, wie bisher).
5. Variable `AUTOPILOT=on` setzen, wenn das erste Release mit Autopilot
   veröffentlicht ist; bei Bedarf `AUTOPILOT_PLUGINS=on`.
6. Auf den eigenen NAS „Von selbst installieren“ einschalten.

## Prüfung

- `scripts/tests/test_autopilot.py`: Tag-Wahl (nächster freier Patch,
  vorhandene und alte Tags, eigener offener Tag, frischer Tag des Inhabers,
  verworfener alter Tag wie 0.46.0, Vorabversion), wartender Tag für die
  Plugin-Signatur, die Bedingungen der Veröffentlichung ohne Freigabe, die
  Prüfung der gebauten Pakete gegen die Liste und die git-Abfragen an einem
  echten Wegwerf-Repository. `verify` zusätzlich gegen das lokale Abbild
  (`components.json`, Kernel 6.12.113-1): eine erfüllte Liste geht durch, eine
  mit 6.12.115-2 scheitert.
- `scripts/tests/test_debian_security.py`: Abschnitte der Quellenliste
  (Backports-Kernel überschreibt den Standard-Kernel nicht), Point-Release
  und Backports, Zusammenführen der Paketquellen.
- `scripts/debian-security.py` gegen das echte Release 0.45.1 (3.11.2026):
  findet `linux-image-amd64` 6.12.113-1 → 6.12.115-2 aus dem Point-Release,
  das die frühere Prüfung (nur trixie-security) nicht gesehen hätte.
- `actionlint` mit shellcheck für `autopilot.yml`, `release.yml` und
  `plugin-sign.yml`.
- **Nicht gelaufen:** ein echter Autopilot-Lauf auf GitHub (braucht die
  Einrichtung oben und ein Release mit Autopilot), das Environment
  `security-release`, ein automatisch signierter Plugin-Satz.
