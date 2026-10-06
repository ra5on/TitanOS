# TitanOS-Systemupdates

Auf TitanOS 2.0.1 oder neuer öffnest du **Einstellungen → Software-Update**. Dort kannst du nach Updates suchen, ein angebotenes Update installieren und anschließend neu starten. Es gibt nur den **Stable**-Kanal.

TitanOS 2.0.2 kann als Systemupdate auf bestehenden Installationen dieses
TitanOS-Forks mit 2.0.1 installiert werden. Das Image und Update-Bundle werden
nach erfolgreicher Prüfung im [Release 2.0.2](https://github.com/ra5on/TitanOS/releases/tag/v2.0.2)
veröffentlicht.

TitanOS prüft vor der Installation die Signatur, Prüfsummen, genaue Versionskennung und den unterstützten Festplattenaufbau. Das vollständige Systemupdate enthält die Titan-Software und die Debian-Systempakete. Deine Dateien und App-Daten bleiben auf dem separaten Datenbereich.

Der neue Build verwendet ausschließlich die signierten [TitanOS-Veröffentlichungen](https://github.com/ra5on/TitanOS/releases). Frühere Feeds und Übergangsupdates sind entfernt. Installationen mit früherem Namensraum benötigen eine Neuinstallation; sichere die benötigten Daten davor.

Ein Wechsel von Titan 3.x auf diesen Umbrel-basierten TitanOS-Fork benötigt ebenfalls eine Neuinstallation. Sichere zuvor deine Dateien und App-Daten außerhalb des NAS. Die beiden Installationsbasen verwenden unterschiedliche Systemlayouts und Updateformate; ein direktes Systemupdate zwischen ihnen wird nicht angeboten.

Der vorherige Rugix-Systemslot ermöglicht einen Rollback des Systemabbilds. Ein Rollback ersetzt keine Sicherung deiner Dateien und der veränderlichen App-Daten.

[Übersicht](../README.md) · [VM-Einrichtung](VM.md)
