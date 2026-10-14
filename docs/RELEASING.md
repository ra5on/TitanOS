# Eine Version veröffentlichen

TitanOS aktualisiert sich aus den GitHub-Releases dieses Projekts. Ein NAS
installiert ein Update nur, wenn das Manifest des Releases mit einem
Schlüssel signiert ist, dessen öffentlicher Teil in seinem Abbild steckt.
Der private Schlüssel ist damit das Wertvollste, was das Projekt hat: Wer
ihn hat, kann jedem TitanOS ein Abbild unterschieben.

## Einmalig: den Schlüssel anlegen

1. Auf einem Rechner, dem du traust, mit [minisign](https://jedisct1.github.io/minisign/):

   ```bash
   minisign -G -p titanos-release-1.pub -s titanos-release-1.key
   ```

   Vergib ein Passwort. Bewahre `titanos-release-1.key` und das Passwort an
   zwei getrennten Orten auf (Passwort-Manager und ein Ausdruck oder
   USB-Stick im Schrank). Die Datei gehört nie ins Repository.

2. Lege den öffentlichen Schlüssel ins Abbild und committe ihn:

   ```
   build/mkosi.extra/usr/share/titanos/update-keys/titanos-release-1.pub
   ```

   Offizielle Plugins werden genauso geprüft; ihr Schlüssel liegt unter
   `build/mkosi.extra/usr/share/titanos/plugin-keys/`. Es kann derselbe sein.

3. In den Einstellungen des Repositorys ein Environment `release` anlegen:
   - „Required reviewers“: du selbst. Jede Veröffentlichung wartet dann auf
     deine Freigabe.
   - „Deployment branches and tags“: nur Tags `v*`.
   - Secrets `MINISIGN_SECRET_KEY` (der ganze Inhalt der `.key`-Datei) und
     `MINISIGN_PASSWORD`.

   Wer ganz ohne Schlüssel bei GitHub auskommen will, lässt die Secrets weg,
   lädt die Dateien aus dem Build-Job herunter, signiert `manifest.json`
   am eigenen Rechner und legt das Release von Hand an.

## Eine Version herausgeben

```bash
git tag -a v1.2.0 -m "Was sich geändert hat – dieser Text wird zum Changelog."
git push origin v1.2.0
```

- `v1.2.0` wird ein Release im Kanal „Stable“, `v1.2.0-beta.1` ein
  Pre-Release im Kanal „Beta“.
- Der Workflow `release.yml` baut das Abbild, packt die Dateien
  (`scripts/make-release.sh`), wartet auf die Freigabe im Environment
  `release`, signiert `manifest.json` und legt das Release an.
- Er bricht ab, wenn kein öffentlicher Schlüssel im Abbild liegt oder die
  Signatur zu keinem davon passt – ein solches Release könnte kein NAS
  installieren.

Ein Release enthält:

| Datei | Wofür |
|---|---|
| `titanos-<version>.zip` (+ `.sha256`) | neuer Stick |
| `titanos-<version>-vmlinuz`, `-initrd.img`, `-filesystem.squashfs` | Updates: ein NAS lädt diese drei direkt in den freien Slot |
| `manifest.json`, `manifest.json.minisig` | was ein Update ist, signiert |
| `titanos-<version>-sources.txt` | Quellen der Debian-Pakete (GPL) |

Die Release-Seite nennt das Datum, ab dem die Version unter Apache-2.0
steht (Erscheinungsdatum plus zwei Jahre).

## Lokal ausprobieren

```bash
make zip VERSION=1.2.0
scripts/make-release.sh 1.2.0 stable
minisign -S -s titanos-release-1.key -m out/release/manifest.json
```

Ein NAS lässt sich auf eine andere Adresse als GitHub zeigen, indem man in
`/config/system/update.json` das Feld `source` setzt (zum Beispiel auf einen
Webserver, der `out/release` unter `/releases/latest/download/` und
`/releases/download/v1.2.0/` ausliefert). Die Signatur wird trotzdem geprüft.

## Den Schlüssel wechseln

Im Abbild dürfen mehrere öffentliche Schlüssel liegen.

1. Neuen Schlüssel anlegen, seinen öffentlichen Teil zusätzlich ins Abbild
   legen, eine Version herausgeben – signiert noch mit dem **alten**.
2. Das Secret im Environment auf den neuen Schlüssel umstellen. Alle
   weiteren Versionen sind mit dem neuen signiert; jedes NAS, das den
   Schritt 1 mitgemacht hat, nimmt sie an.
3. Einige Versionen später den alten öffentlichen Schlüssel aus dem Abbild
   entfernen. Die Version aus Schritt 1 als `MIN_VERSION` setzen, damit
   ältere Installationen erst dorthin aktualisieren.

## Wenn der Schlüssel verloren oder gestohlen ist

- **Verloren:** Es gibt keinen Weg, bestehende Installationen zu
  aktualisieren. Nutzer müssen einen neuen Stick mit einer Version
  bespielen, die den neuen Schlüssel enthält, und ihre Konfiguration
  zurückspielen (Einstellungen › Sicherung des Sticks).
- **Gestohlen:** Sofort mit dem alten Schlüssel eine Version herausgeben,
  die nur noch den neuen enthält, und öffentlich dazu aufrufen, sie zu
  installieren. Bis dahin kann der Dieb signierte Updates ausliefern, wenn
  er zusätzlich die Releases des Projekts oder die Verbindung eines NAS
  dorthin kontrolliert.

Deshalb gehört von Anfang an ein zweiter, offline aufbewahrter Schlüssel
ins Abbild: Mit ihm lässt sich der erste jederzeit ablösen.
