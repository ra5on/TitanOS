# VM-Einrichtung, Menüs und Protokoll – Prüfstand 24. Oktober 2026

Entwicklungsstand nach 0.20.2, kein veröffentlichtes Release. Der vom
Projektinhaber bestätigte Kachelentwurf gilt für Erstellen und Bearbeiten.

## Änderungen

Fünf kompakte Kacheln mit Zusammenfassung rechts und gestapelter Mobilansicht.
VM-Ordner, Firmware, Größenvorgaben, Datenträgerhinweise, Geräte und NAS-Ordner
sind im passenden Bereich aufklappbar. Bestehende Plattenvarianten,
Kernreservierungen und die Einschränkungen laufender VMs bleiben erhalten.
Die Pfadvorschau berücksichtigt einen ausdrücklich gewählten VM-Ordner.
Aktuelle echte [Desktop-](screenshots/current/vm-erstellen.png) und
[Mobilaufnahmen](screenshots/current/vm-erstellen-mobil.png) sowie das
[Protokoll](screenshots/current/protokoll.png) sind mit isolierten Beispieldaten
erstellt; Herkunft und Prüfsummen stehen im Screenshot-Verzeichnis.

Der VM-Zeilenmenüfehler wurde vor der Korrektur mit einem echten Klick auf
„Starten“ reproduziert: Nach `rowMenu = null` war die reaktive `@const vm`
kein gültiges Argument mehr. Der Browser meldete „Cannot read properties of
undefined (reading 'name')“, ohne eine Startanfrage zu senden. Aktionen
verwenden nun eine vorher festgehaltene VM. Kopfmenü und Bestätigungsdialoge
verwenden weiterhin ihre bestehenden Routen und Regeln.

Die Protokollansicht validiert JSON, Einträge und Zeitstempel. HTML-Fallbacks
oder fachfremdes JSON erzeugen einen übersetzten Fehler mit erneutem Abruf.
Fehler beim Nachladen bleiben sichtbar; veraltete Filterantworten werden
verworfen. `/logs` führt mit unterstützten Filtern nach `/tools#tools-logs`.
Historische Meldungen einschließlich alter Versionsnummern bleiben original.
Die konkret gemeldete Dashboard-Zeile des produktiven NAS lag nicht als
HTTP-Antwort vor; eine Ursache auf diesem NAS ist daher nicht nachgewiesen.
Die Prüfungen simulieren ausdrücklich dessen HTML-Inhalt als ungültige Antwort.

## Bridge

`POST /api/v1/network/bridge` übernimmt die vorhandene Konfiguration. Neue
Installationen versuchen die Einrichtung im Netzwerkschritt; bestehende
Konfigurationen werden dort nicht überschrieben. Bestands-NAS verwenden den
Knopf in der VM-Einrichtung. Ein uneindeutiger physischer Uplink verlangt eine
Auswahl. Die Zwei-Minuten-Bestätigung und der automatische Rückfall bleiben
bestehen. `configured` und `bridgeReady` ergänzen den Netzwerkstatus.

Eine Bridge im Probelauf ist auch für das Backend nicht startbereit. Die
Oberfläche behält ihren VM-Entwurf bei einer unterbrochenen HTTP-Verbindung.
Keine zusätzlichen Anschlüsse werden automatisch verbunden. Statusabfragen
lesen RAM und sysfs; keine zyklischen Zugriffe auf `/config`.

## Prüfungen und Grenzen

- `make test`: Go vet und Go-Tests, 75 erfolgreiche Webtests und Svelte-Prüfung
  mit null Fehlern/Warnungen einschließlich Formatprüfung. Die zusätzlichen
  Tests decken Bridge-Erkennung,
  Topologieerhalt, Administratorrechte, Entwicklungsmodus, Fehler,
  Rückfall, Wiederverwendung und die serverseitige VM-Sperre ab.
- `make test-release`: 37 erfolgreiche Paketierungs-/Signaturtests mit
  Miniatur-Abbildern und Wegwerf-Schlüsseln, einschließlich Bootvorlage.
- `scripts/test-ui-vm-create.mjs`: echte Maus-/Tastatur-/Rechtsklick-/Touch-
  Aktionen; genau eine Anfrage für Betriebsaktionen, Navigation und
  Bestätigungsdialoge. Erstellen und Bearbeiten, Kernpaare, Reservierung,
  vier Plattenquellen, Firmware/TPM, PCI/USB, gesperrter Systemstick,
  NAS-Ordner und benutzerdefinierter VM-Ordner. Bridge-Auswahl,
  Bestätigung/Rückfall und automatische Ersteinrichtung. Protokoll,
  Nachladen, ungültige Antworten, Wiederholen und leere Ergebnisse.
- Browserprüfungen gegen die gebaute WebUI bei 1600 und 390 Pixeln,
  Graphit/Hell, Deutsch/Englisch, ohne horizontales Abschneiden oder
  JavaScript-Laufzeitfehler. Zusätzlich vorhandenes `test-ui-vms.mjs`.
- `scripts/test-vm-bridge-qemu.py`: neue Wegwerf-UEFI-/USB-Medien aus dem
  lokal vorhandenen ZIP 0.19.1 (Systembasis 0.15.0), aktuelles Test-Binary
  je Boot ausschließlich in den RAM des Gasts injiziert. Echte
  Zwei-Minuten-Rücknahme, bestätigte Bridge, Gast-DHCP/IPv4-Verbindung,
  Dienstneustart, Wiederherstellung nach NAS-Neustart sowie MAC- und
  Adresszuordnung. Alle drei Bridge-Prüfblöcke erfolgreich.
  Ergebnisse und Serienprotokolle unter
  `out/validation/vm-bridge-qemu-final/`; der Ergebnisnachweis enthält den
  SHA-256 des geprüften Binarys.

Geprüftes Bridge-Test-Binary vor der zwischenzeitlichen Split-Lock-Änderung (SHA-256):
`6fb72bfa27199881ddb8c96c8126039274d32fbb1801aa7f308a2512420cdcbf`.

Die Browser verwenden ausschließlich isolierte API-Daten. Die QEMU-Prüfung
ändert weder Host-Netzwerk noch Host-Laufwerke und ist keine Abnahme eines
neu gepackten Release-ZIPs. Ein zusätzlicher Lauf mit 4 GiB Gast-RAM und
parallelen Build-/Browsertests endete in einem HTTP-Timeout; der abschließende
Lauf verwendet 2 GiB Gast-RAM und hat die Bridge-Prüfungen bestanden. Bond/VLAN
sind durch erzeugte Konfigurationen geprüft, nicht mit echter
Netzwerkinfrastruktur. Keine neue Abnahme realer
GPU-/USB-Durchreichung, Windows-Installation oder NAS-Hardware.

Auf erneute Entscheidung bleibt die Split-Lock-Erkennung im Kernel-Standard
aktiv. Die zwischenzeitliche Abschaltung samt Boot-Migration wurde vor
Veröffentlichung entfernt. Der separate QEMU-Nachweis einer Abschaltung
beschreibt damit keinen aktuellen Produktzustand. Nach der Rücknahme wurden
Update-/Startpaket-Tests und die Paketierungsprüfung erneut ausgeführt; keine
weitere QEMU- oder Hardwareprüfung. Details in [SPLIT-LOCK.md](SPLIT-LOCK.md).
