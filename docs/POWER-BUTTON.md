# Ein-/Ausschalter

Unter **Einstellungen → Ein-/Ausschalter** wählen Besitzer/Administratoren die
Aktion bei kurzem Druck auf die physische Taste: **Nichts unternehmen**,
**Neustarten** oder **Herunterfahren** (Standard). Ein Tastendruck zeigt keinen
zusätzlichen Browserdialog. Neustart und Ausschalten nutzen den regulären
systemd-Ablauf mit sauberem Stoppen der Dienste und VMs.

Die Auswahl liegt in `/config/system/power-button.json`. TitanOS schreibt daraus
beim Start eine Laufzeitdatei unter `/run/systemd/logind.conf.d/` und lädt logind
über seine unterstützte Reload-Schnittstelle neu. Laufende Sitzungen bleiben
erhalten. Änderungen werden bei Fehlern zurückgenommen; der Entwicklungsmodus
verändert keine Einstellungen. Statusabfragen lesen nicht vom Bootstick.

Langes Gedrückthalten kann hardwareseitig die Stromversorgung unterbrechen;
das kann diese Softwareeinstellung nicht verhindern. Einschalten eines bereits
ausgeschalteten Servers bleibt Sache der Firmware/Hardware. Inhibitoren anderer
Dienste werden respektiert.

Geprüft wurden alle drei logind-Werte ohne Dienstneustart und echte emulierte
ACPI-Tastendrücke für Ignorieren und Herunterfahren in einem isolierten QEMU-NAS.
Die konkrete Taste des NAS muss nach dem Update auf echter Hardware geprüft
werden. Grundlage: [logind.conf](https://www.freedesktop.org/software/systemd/man/latest/logind.conf.html).
