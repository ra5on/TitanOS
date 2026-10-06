# Titan 3.0.0

Dieses Image ist ein Testbuild des eigenen Titan-NAS. Die Veröffentlichung
bleibt als Entwurf bestehen, bis die vollständigen Freigabeprüfungen und der
Praxistest abgeschlossen sind. Eine erfolgreiche Build-Pipeline allein ist
noch keine Stable-Freigabe.

Enthalten sind die eigene deutsche Desktop-Oberfläche, Docker-Verwaltung mit
LinuxServer.io- und Big-Bear-Vorlagen, die eigene Fotos-Funktion, Dateiverwaltung,
Benutzer und SMB-Freigaben, Speicherverwaltung sowie virtuelle Maschinen.

Das System aktualisiert Debian und Titan über signierte Systemimages. Die
zweite Systempartition ermöglicht ein Rollback; persönliche Daten bleiben
auf der Datenpartition. Ein System-Rollback setzt Datenbanken installierter
Container nicht zurück. Vor App-Upgrades sind geeignete Datensicherungen nötig.

Der Systemslot ist schreibgeschützt. Veränderbare NAS-, Container- und
VM-Daten liegen auf der Datenpartition. Gemeinsame RAM-Grenzen berücksichtigen
auch den nächsten Autostart; nach einer RAM-Verkleinerung können Docker und
virtuelle Maschinen sicher gesperrt bleiben, während die Verwaltung erreichbar
bleibt. USB-Geräte werden vor Durchreichung erneut auf ihre Identität geprüft.

Die neue Systemfamilie wird frisch installiert. Das Image schreibt beim Booten
keine bestehende Installation auf einem anderen Datenträger um. Zum Testen
mindestens 8 GB RAM und 64 GB virtuellen Speicher bereitstellen. Hardware für
Virtualisierung und Durchreichung muss vom Host verfügbar gemacht werden.
Der getestete Bootmodus ist UEFI (OVMF) mit deaktiviertem Secure Boot.

Signaturen, Paketinventar und Ergebnisse der Image-Laufzeittests werden dem
Download beigefügt. Ausstehende Prüfungen stehen in `docs/QA-3.0.0.md`.
