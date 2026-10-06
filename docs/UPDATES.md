# TitanOS-Systemupdates

Nach der Neuinstallation von TitanOS 2.0.1 öffnest du **Einstellungen → Software-Update**. Dort kannst du nach Updates suchen, ein angebotenes Update installieren und anschließend neu starten. Es gibt nur den **Stable**-Kanal.

TitanOS prüft vor der Installation die Signatur, Prüfsummen, genaue Versionskennung und den unterstützten Festplattenaufbau. Das vollständige Systemupdate enthält die Titan-Software und die Debian-Systempakete. Deine Dateien und App-Daten bleiben auf dem separaten Datenbereich.

Der neue Build verwendet ausschließlich die signierten [TitanOS-Veröffentlichungen](https://github.com/ra5on/TitanOS/releases). Frühere Feeds und Übergangsupdates sind entfernt. Installationen mit früherem Namensraum benötigen eine Neuinstallation; sichere die benötigten Daten davor.

Ein Wechsel von Titan 3.x auf diesen Umbrel-basierten TitanOS-Fork benötigt ebenfalls eine Neuinstallation. Sichere zuvor deine Dateien und App-Daten außerhalb des NAS. Die beiden Installationsbasen verwenden unterschiedliche Systemlayouts und Updateformate; ein direktes Systemupdate zwischen ihnen wird nicht angeboten.

Der vorherige Rugix-Systemslot ermöglicht einen Rollback des Systemabbilds. Ein Rollback ersetzt keine Sicherung deiner Dateien und der veränderlichen App-Daten.

[Übersicht](../README.md) · [VM-Einrichtung](VM.md)
