# Durchsicht für 1.02: KI-Agent (Dokumente, Schutz, Vorausschau, Familie, Notizen, Umzug) sowie Skripte und CI

Zusammenfassung für den Inhaber. Die vollständige Liste mit Ort, Test und Commit steht in `docs/validation/durchsicht-102-agent-skripte.md`.

**Umfang:**
- Die Teile des KI-Agenten, die in 0.99 nicht gelesen wurden.
- Die Skripte und die GitHub-Abläufe.

**Ergebnis:** 58 Funde, davon 6 schwer, 16 mittel und 36 klein. **32 sind behoben**, jeder mit einem Test, der vorher fehlschlug. 26 sind notiert, weil sie eine Entscheidung brauchen, die Signatur berühren oder außerhalb dessen liegen, was ein App-Update ändern darf. Das nächste Release bleibt ein App-Update.

## Was behoben ist (das Wichtigste)

**Rechte und Datenschutz**
- **Notfallzugang:** Hat der Besitzer eine Freigabe aus dem Eintrag einer Person genommen, bekam die Person nach der Wartezeit trotzdem Lesezugriff darauf. Ein laufender Notfallzugang behielt die Freigabe 30 Tage. Jetzt gilt immer der aktuelle Eintrag.
- **Konto löschen:** Die eigenen Einstellungen des Agenten blieben liegen, mit dem Passwort des Postfachs, ebenso das Gedächtnis von „Dokumente“. Legte man später ein Konto mit demselben Namen an, holte der Papierkram die Post der früheren Person in den Ordner der neuen. Beides wird jetzt mit dem Konto gelöscht.
- **Geteilte Projekte:** Andere Mitglieder sahen in den Notizen eines Chats Pfade aus privaten Ordnern und die Titel privater Notizen des Autors. Das ist jetzt weg.
- **Notfallhandbuch:** Abgeschaltete Kontakte oder alte Telefonnummern standen bis zur nächsten Nacht noch darin. Jetzt gelten sofort die aktuellen Einstellungen.

**Ransomware-Wächter**
- Nach einem Neustart von titand, auch nach einem App-Update, blieb ein Alarm für immer „aktiv“. Wiederherstellen und Freigeben gingen dann nicht mehr, nur „Fehlalarm“ war noch möglich. Jetzt endet so ein Alarm und lässt sich wiederherstellen.
- Bei mehr als 20 Alarmen ging der Vermerk verloren, wer vor der Sperre Schreibrecht hatte. Offene und gesperrte Alarme bleiben jetzt erhalten.
- Kam während des Bestätigungsdialogs ein zweiter Angriff auf eine andere Freigabe dazu, öffnete „Fehlalarm“ genau diese Freigabe mitten im Angriff. Der Dialog bleibt jetzt bei seinem Alarm.

**Dokumente**
- **Fristen:** Erinnerungen an Fristen von Dateien mit langem Namen wurden nie angelegt. Das betraf auch Namen, die erst durch Umlaute zu lang wurden. Bei „Gleich als Erinnerung“ ging die Frist still verloren.
- **Zeitlimit:** Ein Lauf, der an sein Zeitlimit von zwei Stunden kam, leerte das Gedächtnis aller Ordner, die er noch nicht erreicht hatte.
- **Eingang:** Eine gelöschte Datei im Eingang blockierte jede spätere Datei gleichen Namens, zum Beispiel jedes neue `scan.pdf` des Scanners.
- **Rechnungen:** Die Fälligkeit einer abgelegten Rechnung kam zweimal.

**Umzug auf ein anderes Volume:** Die Daten von „Dokumente“ blieben auf dem alten Volume. Dazu gehören das Gedächtnis und das Protokoll mit „Rückgängig“ und den offenen Fragen. Jetzt ziehen sie mit um. Ein zurückgebliebener alter Stand wird beiseitegelegt und lässt sich mit „Alte Kopie löschen“ entfernen.

**Hardware-Berater**
- Konnten die Platten einmal nicht gelesen werden, ersetzte ein leerer Bericht die Warnung vor einer ausfallenden Platte. Am Monatsersten ging dieser leere Bericht dann als „nichts Auffälliges“ hinaus.
- Mit zwei gleichen Grafikkarten stürzte die Seite ab.

**Notizen:** Wer schnell weitertippte, bekam einen falschen Konflikt angezeigt, und die letzten Worte waren nicht gespeichert.

**Skripte und CI**
- Die Prüfung im CI war auf diesem Stand rot, weil die Übersicht der API veraltet war.
- Eine verschobene Datei zählte nur unter ihrem neuen Pfad. Wurde eine Datei aus dem Abbild verschoben, entstand fälschlich ein App-Release statt eines vollen Releases. Die Prüfung des Autopiloten ließ sich auf demselben Weg umgehen.
- Ein fehlgeschlagener Auftrag für einen Plugin-Satz wartete fünf Stunden, statt sofort abzubrechen.
- Ein hängender Build läuft jetzt höchstens 90 Minuten, nicht mehr sechs Stunden.

## Was eine Entscheidung braucht

1. **Daten eines gelöschten Kontos:** Chats, Gedächtnis, Skills und der Ordner in „KI-Agent“ bleiben nach dem Löschen liegen. Ein neues Konto gleichen Namens erbt sie. Sollen sie mitgelöscht oder aufbewahrt werden?
2. **Container auf anderen Knoten:** `cluster_action` steht bei den Rechten unter „VMs“, auch für Container. Ist „Docker“ verboten und „VMs“ ohne Nachfrage erlaubt, darf der Agent Container auf anderen Knoten ohne Nachfrage schalten. Soll das Werkzeug je nach Art unter Docker, VMs oder Systemcontainern stehen?
3. **Plugin-Katalog (Signaturpfad, schwer):** Scheitert der Download des Katalogs nur vorübergehend, veröffentlicht `plugin-sign.yml` einen Katalog mit kleinerer Nummer. Jedes NAS lehnt den Katalog dann ab, bis die Nummer wieder höher ist. Ich empfehle, den Fall „Katalog fehlt“ von anderen Fehlern zu trennen und sonst abzubrechen. Das braucht deine Freigabe, weil es die Signatur betrifft.
4. **Einstellungen der Funktionen:** Speichern zwei Administratoren oder zwei Tabs gleichzeitig, setzt einer still zurück, was der andere geschaltet hat, zum Beispiel den Wächter. Abhilfe wäre eine Versionsnummer oder eigene Routen je Gruppe. Das ist eine Änderung der API.
5. **Downloads im Release:** Bricht ein Download mitten im mehrere GB großen Abbild ab, bricht das ganze App-Release ab. Wiederholt wird nur das Öffnen. Der Autopilot wiederholt gar nicht.
6. **Kleinere Punkte:**
   - Das Kontingent des Backup-Buddys ist mit „GB“ beschriftet, gespeichert wird aber in GiB.
   - „Ablehnen“ und „Zugang beenden“ beim Notfallzugang fragen nicht nach.
   - Eine geänderte Wartezeit gilt nicht für schon offene Anfragen.

## Wie geprüft wurde

- **Go:**
  - `go vet ./...` und `go test ./internal/agent/... ./internal/api/` sind grün.
  - Die neuen Tests laufen zusätzlich mit `-race`.
- **Python:** 134 Tests der Skripte sind grün, einschließlich des vorher roten Tests der API-Übersicht.
- **Web:**
  - `npm test`: 41 Dateien und 207 Tests, dazu die Node-Tests.
  - `npm run check` (svelte-check, prettier): ohne Fehler.
- **Browser** (Vorschau mit Testdaten): `test-ui-agent54-docs`, `-protect`, `-family`, `-foresight` und `test-ui-agent` sind grün.
- **Workflows:** actionlint ohne Befund.

## Was nicht geprüft ist

- **Nichts am echten NAS.** Echtes Docker, QEMU und ein echtes Modell kamen nicht zum Einsatz. Sandbox, Wächter und Postfach sind in den Tests nachgebaut.
- **shellcheck** ist nicht installiert. Die Shell-Teile der Workflows hat nur actionlint gelesen.
- **Ohne eigenen Test** sind zwei kleine Korrekturen an der Oberfläche: das Abfragen nach dem Schließen bei „Dokumente“ und beim Modell-Download. Beide sind mit svelte-check und im Browser geprüft.
