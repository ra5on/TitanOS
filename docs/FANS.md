# Lüfter

Seit 0.35.0: Einstellungen › Temperaturen › **Lüfter** (nur Besitzer und
Administratoren). Gezeigt werden alle Lüfterausgänge, die ein hwmon-Treiber
regelbar anbietet (`pwmN` mit `pwmN_enable`), mit Drehzahl und aktuellem
Tastgrad. Für manche Mainboards braucht es das Treiber-Plugin `it87-extended`.

## Regelung je Lüfter

- **BIOS** (ab Werk): TitanOS fasst den Lüfter nicht an.
- **Fest:** eine Drehzahl in Prozent.
- **Kurve:** 2 bis 8 Punkte Temperatur → Drehzahl, steigend, linear
  dazwischen, nach einem Temperatursensor des NAS oder der **wärmsten Platte**
  (aus dem SMART-Zwischenspeicher, weckt keine Platte). Sinkt die Temperatur
  um weniger als 2 °C, bleibt die Drehzahl (Hysterese).
- Je Lüfter ein **Minimum** (20–100 %, Vorgabe 30 %).

## Sicherheit

- Lässt sich der Sensor einer Kurve nicht lesen, läuft der Lüfter mit 100 %
  und die Seite zeigt den Fehler.
- Bevor TitanOS einen Lüfter übernimmt, merkt es sich dessen Modus
  (`pwmN_enable`) in `/run/titanos/fans-original.json`; zurück auf „BIOS“ und
  beim Beenden von titand bekommt der Lüfter ihn wieder. War er schon manuell,
  bleibt er auf voller Drehzahl. Ein titand-Neustart kennt den Modus noch.
- Abgleich alle fünf Sekunden; geschrieben wird nur bei Änderung.
  Entwicklungsmodus: nur Anzeige.

## Technik

Paket `fans`, Kennungen wie bei den Temperaturen aus Gerätepfad, Chip und
Ausgang (stabil über Neustarts). Einstellungen in `/config/system/fans.json`
(nur geregelte Lüfter). API: `GET`/`PUT /api/v1/system/fans`.

## Prüfstand

Mit künstlichem sysfs geprüft (`fans_test.go`): Erkennung (nur regelbare,
ohne NVMe), Kurve, Hysterese, Minimum, Volllast bei fehlendem Sensor,
Merken des Modus über titand-Neustarts, Rückgabe ans BIOS beim Umschalten
und beim Beenden, Prüfung der Eingaben, Entwicklungsmodus. **Nicht geprüft:**
echte Mainboards und Treiber.
