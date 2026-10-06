# TitanOS-Image

Die Systembasis ist Debian mit einem schreibgeschützten Rugix-System und zwei Systemslots für vollständige Updates und Rollback. Konfiguration, Container, Dateien und VM-Daten liegen auf dem separaten Datenbereich und bleiben beim Wechsel des Systemslots erhalten.

Der veröffentlichte Build unterstützt AMD64 mit UEFI. Die komprimierte Datei heißt `titan-<version>.img.xz`, das signierte Update `titan-<version>.update`. Beim ersten Start legt Rugix den zweiten Systemslot und die Datenpartition an und nutzt die verfügbare Laufwerkskapazität. Für eine VM muss das importierte Bootlaufwerk vor dem Start auf mindestens 32 GiB vergrößert werden.

## Entwicklung

```sh
npm --prefix packages/os run build:amd64:rugix
npm --prefix packages/os run vm -- boot build/titanos-amd64.img --device nas --disk-size 32G
```

Die freigegebene Pipeline in `titan-build` prüft Versionskennung, Datenträgeraufbau, echte UEFI-Inbetriebnahme, Weboberfläche und Signaturen vor der Veröffentlichung. Alte Mender-Images und fremde Update-Feeds werden nicht unterstützt.

Die generischen ARM64- und Raspberry-Pi-Rezepte bleiben Entwicklungsziele; sie werden in diesem Release nicht veröffentlicht. `titan-home` und `titan-pro` sind Testprofile für reale OEM-Hardware. Deren tatsächliche Herstellerkennungen bleiben erhalten, damit die Hardwareerkennung funktioniert.
