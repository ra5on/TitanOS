# Bericht über das NAS (Health-Report)

Seit 0.34.0 schickt TitanOS auf Wunsch regelmäßig eine Zusammenfassung:
Einstellungen › Benachrichtigungen › **Bericht über das NAS** (nur Besitzer
und Administratoren), ab Werk aus.

## Inhalt

- **Volumes:** Zustand, eingebunden, Belegung (ab 90 % Warnung), gesperrte
  verschlüsselte Volumes.
- **Platten:** Anzahl, SMART-Fehler und -Warnungen, wärmste Platte (über
  55 °C Warnung).
- **Sicherungen:** je eingeschaltetem Auftrag letzter Erfolg, Fehlschlag oder
  überfällig; kein Auftrag ergibt eine Warnung.
- **System:** Version, Laufzeit, belegter Arbeitsspeicher, verfügbares Update,
  USV (Verbindung, Batterie, Ladung).
- **Meldungen:** Fehler und Warnungen der Glocke seit dem letzten Bericht.

Der Titel nennt den Gesamtzustand („alles in Ordnung“, „bitte ansehen“,
„Handlungsbedarf“). Sprache wie die übrigen Benachrichtigungen.

## Zustellung und Zeitplan

- Über alle eingeschalteten Wege (E-Mail, ntfy, Telegram, Discord, Home
  Assistant) unabhängig von „Verschicken ab“, dazu an Geräte mit Mitteilungen
  (`docs/WEB-PUSH.md`). Nicht in der Glocke.
- Wöchentlich (Wochentag) oder monatlich (Tag 1–28) zu einer Uhrzeit in der
  Zeitzone des NAS. Ein verpasster Bericht (NAS aus) geht nach dem Start raus,
  wenn sein Termin weniger als einen Tag zurückliegt. Fehlgeschlagen: neuer
  Versuch nach einer Stunde. Der Zeitpunkt des letzten Berichts liegt nur in
  `/run/titanos/health-report-sent` (übersteht einen titand-Neustart, keinen
  NAS-Neustart).
- „Jetzt schicken“ und „Vorschau“ auf der Seite.

## Technik

Paket `healthreport`. Daten nur aus dem RAM von titand (Speicherstatus mit
zwischengespeichertem SMART, `backup.CachedViews`, Update-, USV- und
Systemmonitor), weckt keine Platten. Einstellungen in
`/config/system/health-report.json`. API: `GET`/`PUT /api/v1/health-report`,
`POST /api/v1/health-report/send`.

Geprüft mit Tests für Inhalt, Zustände und Zeitplan (`healthreport_test.go`)
und die Routen (`api/healthreport_test.go`); echte Zustellung über die Wege
wie bei den übrigen Benachrichtigungen.
