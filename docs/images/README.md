# TitanOS-Aufnahmen

Die vier JPEG-Aufnahmen zeigen das **veröffentlichte Image von TitanOS 2.0.11**:
die echte Oberfläche mit dem echten Titan-Backend, gestartet in einer
Wegwerf-VM. Es wurden keine UI-Mocks und keine nachträglichen Bildänderungen
verwendet.

- **Startbildschirm:** Begrüßung mit Titan-Schriftzug, Widgets und Dock.
- **Dock bearbeiten:** Bearbeitungsmodus des Docks mit Werkzeugleiste. Diese
  Aufnahme stammt aus dem Dock-Test des Release-Builds
  (`dock-edit.vm.test.ts`) auf demselben Image.
- **Dateien:** Persönlicher Ordner mit den Standardordnern Dokumente, Downloads,
  Fotos und Videos sowie einer Beispieldatei.
- **Einstellungen:** Übersicht derselben Testumgebung.

## So entstehen die Aufnahmen

Die Aufnahmen entstehen im Release-Build auf dem frisch gebauten Image, bevor
es veröffentlicht wird. Der Workflow **README screenshots**
(`.github/workflows/readme-screenshots.yml`) kann sie zusätzlich für ein bereits
veröffentlichtes Image wiederholen: Er lädt das Image, prüft die signierte
Prüfsumme und startet es. In beiden Fällen läuft das Image in einer VM mit
generischer PC-Hardware und führt
`packages/titand/source/modules/test-utilities/readme-screenshots.vm.test.ts`
aus. Der Test legt das Testkonto „Titan“ mit einem zufälligen Passwort an, lädt
wenige Beispieldateien hoch, meldet sich über die echte Anmeldeseite an und
fotografiert Startbildschirm, Dateien und Einstellungen in 1280 × 800.

Angezeigte Ressourcen, IP-Adresse und Gerätename gehören zur Test-VM. Keine
Aufnahme enthält ein Passwort, einen Sitzungsschlüssel oder persönliche
NAS-Dateien. Das SVG-Banner verwendet das Titan-Logo aus diesem Repository.
