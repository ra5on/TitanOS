# TitanOS 2.0.3 · Stable

Virtuelle Maschinen erhalten eine automatische Bridge-Einrichtung im vorhandenen
Dropdown-Stil. Das Dock zeigt wieder die ursprünglichen Icons des Forks.

- **NAT (Standard):** Internet über das NAS; Dienste erreichst du mit Portweiterleitungen.
- **Host-only:** Privates Netz zwischen NAS und VM, ohne Internet- oder LAN-Zugang.
- **Heimnetz (Bridge):** Verbindung über eine vorhandene Linux-Bridge. Fehlt sie, richtet TitanOS sie bei dieser Auswahl automatisch über den aktiven kabelgebundenen NAS-Anschluss ein.

Bei eigenen Images und ISOs kannst du das Netzwerk bereits beim Erstellen wählen.
Kataloginstallationen verwenden zunächst NAT für ihre Einrichtung. Danach lässt
sich das Netzwerk in den VM-Einstellungen ändern, sobald die VM ausgeschaltet ist.
Vorhandene Portregeln bleiben gespeichert und werden ausschließlich bei NAT aktiv.
Fehlende Bridges und Konflikte des privaten Subnetzes werden verständlich gemeldet.
Die Bridge-IP wird angezeigt, sobald das NAS sie im gewählten Netzwerk beobachten kann.

Die automatische Bridge-Einrichtung übernimmt die bisherige NAS-Verbindung des
aktiven Anschlusses. Die Weboberfläche kann dabei kurz die Verbindung verlieren
und verbindet sich automatisch neu. Lass das Fenster geöffnet, damit die neue
Verbindung bestätigt werden kann. Ein Netzwerk-Checkpoint schützt die bisherige
Konfiguration und nimmt die Umstellung ohne erfolgreiche Bestätigung automatisch
zurück. Bestehende Bridges und andere
Netzwerkanschlüsse bleiben erhalten; WLAN wird nicht automatisch umgebaut.

## Download und Aktualisierung

- `titan-2.0.3.img.xz`: Kompaktes AMD64-Image für eine Neuinstallation mit UEFI.
- `titan-2.0.3.update`: Vollständiges Systemupdate mit Rugix-Rollback für kompatible TitanOS-Installationen.
- `SHA256SUMS` und `SHA256SUMS.sig`: Prüfsummen und Ed25519-Signatur aller Release-Dateien.
- `image-verification.json`: Bericht zur Image-Struktur, zum realen UEFI-Boot und zur geprüften Versionskennung.

**TitanOS ab 2.0.1** mit der Kennung `titan-rugix-amd64-v2` kann über die
Systemeinstellungen aktualisiert werden. **Titan 3.x** und ältere Installationen
mit anderem Systemaufbau benötigen eine Neuinstallation; sichere ihre Daten
vorher außerhalb des Systems. Es gibt keinen automatischen Formatwechsel.

Vergrößere beim VM-Test die importierte Boot-Festplatte **vor dem ersten Start
auf mindestens 32 GiB**, empfohlen sind 64 GiB. TitanOS nutzt den verbleibenden
Platz nach der Einrichtung für Daten. Secure Boot muss deaktiviert sein.

Die Veröffentlichung erfolgt erst nach erfolgreichen Quell-, Regressions-,
Image- und UEFI-Boot-Prüfungen sowie Signaturprüfung. Der Boot-Test bestätigt
keine umfassende Prüfung aller Gastbetriebssysteme, LAN-Konfigurationen oder Hardwarekombinationen.

[VM-Netzwerke](https://github.com/ra5on/TitanOS/blob/main/docs/MACHINE-NETWORKS.md) ·
[Download-Prüfung](https://github.com/ra5on/TitanOS/blob/main/docs/DOWNLOADS.md) ·
[Support](https://github.com/ra5on/TitanOS/issues)
