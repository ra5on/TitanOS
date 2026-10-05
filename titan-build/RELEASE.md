# TitanOS veröffentlichen

TitanOS erscheint ausschließlich als Stable-Version im Repository `ra5on/TitanOS`. Die Version in `.titan/release.json` ist numerisch, beispielsweise `2.0.1`. Dateinamen sind `titan-2.0.1.img.xz` und `titan-2.0.1.update`; die Oberfläche zeigt **TitanOS 2.0.1**. Der GitHub-Release-Tag lautet `v2.0.1`.

## Voraussetzungen

- Vollständiger aktueller Source-Checkout mit Lizenz- und Drittkomponentenhinweisen.
- Ed25519-Schlüsselpaar: der öffentliche Schlüssel steht in `.titan/release-public.pem`; der dazu passende private Schlüssel liegt ausschließlich im Actions-Secret `TITAN_SIGNING_KEY`.
- Ausreichend Speicherplatz und ein AMD64-Builder mit Docker, QEMU und UEFI-Firmware.

## Ablauf

Die Workflow-Datei `.github/workflows/titan-image.yml` prüft zunächst den aktuellen Checkout. Danach folgen Signaturkonfiguration, relevante Regressionstests, vollständiger Image-Build und der Boot-Test. Der Boot-Test verwendet ein mindestens 32 GiB großes Laufwerk, prüft die Weboberfläche und bestätigt die genaue TitanOS-Version. Erst danach werden Manifest und Prüfsummen signiert und die geprüften Dateien auf GitHub veröffentlicht.

Ein bereits veröffentlichter Release wird nicht überschrieben. Jede Änderung benötigt eine neue Versionsnummer. In der README wird erst nach erfolgreicher Veröffentlichung ein direkter Download-Link eingetragen.

## Neuinstallation und Updates

Dieses Image beginnt mit dem Titan-Namensraum und der Kennung `titan-rugix-amd64-v2`. Es benötigt eine Neuinstallation. Alte Update-Kanäle und Übergangsupdates werden nicht angeboten. Nach dieser Installation bezieht das NAS seine späteren vollständigen Systemupdates ausschließlich aus dem signierten TitanOS-Stable-Feed auf GitHub. Der vorherige Systemslot steht für den Rollback bereit.

Eine Funktion erhält nur auf ausdrückliche Vorgabe ein Alpha- oder Beta-Kennzeichen. Das ändert den System-Update-Kanal nicht.
