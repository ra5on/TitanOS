# TitanOS 2.0.1 herunterladen und prüfen

Das [Release](https://github.com/ra5on/TitanOS/releases/tag/v2.0.1) enthält das
Installations-Image und seine signierten Prüfsummen. Lade diese vier Dateien in
denselben Ordner:

- [titan-2.0.1.img.xz](https://github.com/ra5on/TitanOS/releases/download/v2.0.1/titan-2.0.1.img.xz)
- [SHA256SUMS](https://github.com/ra5on/TitanOS/releases/download/v2.0.1/SHA256SUMS)
- [SHA256SUMS.sig](https://github.com/ra5on/TitanOS/releases/download/v2.0.1/SHA256SUMS.sig)
- [release-public.pem](https://github.com/ra5on/TitanOS/releases/download/v2.0.1/release-public.pem)

## Unter Linux prüfen

Öffne ein Terminal in diesem Ordner. Der öffentliche Schlüssel benötigt kein
Passwort. Sein SHA-256-Fingerabdruck lautet:

```text
503c465e8ab95c6b59f6b8e56a8f913ea4afa975ba698e98eebb089bf4b11ee3
```

Vergleiche ihn mit der Ausgabe dieses Befehls:

```sh
sha256sum release-public.pem
```

Prüfe anschließend mit OpenSSL die Signatur der Prüfsummenliste:

```sh
openssl pkeyutl -verify -rawin -pubin -inkey release-public.pem \
  -in SHA256SUMS -sigfile SHA256SUMS.sig
```

Bei Erfolg erscheint `Signature Verified Successfully`. Danach prüfst du genau
das heruntergeladene Image:

```sh
grep '  titan-2\.0\.1\.img\.xz$' SHA256SUMS | sha256sum --check
```

Die Ausgabe muss `titan-2.0.1.img.xz: OK` lauten. Andere Dateien aus dem Release,
etwa das Update-Bundle, musst du für diese Image-Prüfung nicht herunterladen.
Wenn eine Prüfung fehlschlägt, verwende diese Download-Datei nicht.

Entpacke danach das Image und folge der [Installationsübersicht](../README.md)
oder der [VM-Anleitung](VM.md).
