# KI und Home Assistant: Ausbau in 0.15.0

Energieabhängige Auftragsplanung (PV, Strompreise) ist ausdrücklich nicht Teil
dieses Ausbaus. Rollen, Bereichsrechte und der bewusst wählbare Direktmodus
bleiben maßgeblich. Betriebsdaten liegen auf dem Daten-Volume, nicht auf dem Stick.

| Bereich | Umgesetzt | Bewusste Grenzen |
|---|---|---|
| Geräteverständnis | Räume, Aliasnamen, Beschreibung, individuelle Lesesperre für Änderungen und Dienstelisten; `ha_capabilities` mit echten Diensteschemata und Geräteattributen | Eine Domänenfähigkeit beweist nicht, dass jedes Gerät sie unterstützt. Eine Einzelregel kann eine Freigabe nur einschränken. |
| Aktionsprüfung | Zielzustände für Schalter, Licht, Lüfter, Klima, Schlösser und Abdeckungen; Teilergebnisse je Entität | Solltemperatur ist kein Beweis erreichter Raumtemperatur. Szenen und beliebige Skripte haben keine allgemeine Nachbedingung. |
| Rücknahme | `ha_undo` für eine beobachtete Ein/Aus-Änderung an `switch`/`input_boolean`; an Konto, Verbindung, Zustand und Änderungszeit gebunden | 30 Minuten, nur RAM, einmal verwendbar. HA bietet kein atomisches Compare-and-set; ein anderes Steuergerät kann zwischen Prüfung und Aufruf eingreifen. Keine Rücknahme von Türen, Szenen oder Abläufen. |
| Automationen | Bis zehn zusätzliche Ereignisauslöser, UND/ODER-Zustandsbedingungen, unveränderte Mindestdauer, Zeitfenster und Zeitzone, Hysterese, Abklingzeit, Vorschau und fünf Vorversionen | Zusätzliche Auslöser wirken als ODER. Zeitfenster/sonstige Voraussetzungen bleiben UND. Dauer meint unveränderten Zustand, nicht einen schwankenden Messwert dauerhaft über der Grenze. Verpasste WS-Ereignisse werden nicht rekonstruiert. |
| Assist | Optionale native HA-Conversation-Integration; Gesprächszuordnung, Rückfragen, kurze Antworten, identische Ereignis-IDs nicht erneut ausführen | Zuordnung 30 Minuten im RAM; maximal 200 Gespräche. Längere Aufträge bleiben im NAS-Chat und nutzen dessen persönliche Ergebnismeldungen. Kein bestätigter Echtbetrieb der Custom Integration. |
| MQTT | Backup-Lauf, Fortschritt, letzter Erfolg, nächster Termin, Problemzustand, Kopier-/KI-Auftragszahl und Wartungsbelegung; optionale Backup-Startknöpfe | Nur bekannte Cachewerte; unbekannt wird nicht als gesund ausgegeben. Keine Chatinhalte. Auftragszahlen gehören zum gewählten HA-Konto. |
| Diagnose | Recorder-Historie einer freigegebenen Entität (bis 24 h, Folgeseiten), WS-Verbindungszeiten, Ringpufferverluste und aktuelle Beobachtungen | Keine automatische Ursachengewissheit aus zeitlicher Korrelation. Bestehende NAS-Protokollwerkzeuge ergänzen die Untersuchung. |
| Dokumentwissen | PDF-Text mit Seitenzahlen, optionale deutsche/englische OCR; persönliche Quellordner und Inhaltssuche mit Belegstellen | Suche erfolgt auf Anfrage, ohne permanenten Volltext-/Vektorindex: 25 Dateien pro Seite, höchstens 1000 Suchtreffer, PDFs zunächst 20 Seiten. Grenzen und Fehler werden ausgegeben. |
| Dokumentbearbeitung | Exakte Text-/Word-Ersetzungen mit erwarteter Trefferzahl; Excel-Wertzellen nach Blatt und Adresse; Vorschau in Aktionskarte, Ausgangshash und neue Kopie | Word-Treffer müssen innerhalb eines Textlaufs liegen. Excel bearbeitet vorhandene Wertzellen, keine Formelzellen/Blattstruktur; Formelergebnisse erst nach Neuberechnung beim Öffnen. Kein vollständiger Office-Editor. |
| Persönliches Wissen | Explizite Fakten, Vorlieben, Regeln, Vorlagen, Projektnotizen und Quellordner mit Herkunft, Revision, Bearbeiten und Vergessen | Kein stilles Lernen, kein kontenübergreifendes Gedächtnis. Nur Fakten/Vorlieben/Regeln ergänzen automatisch den Kontext, begrenzt auf 16 KiB. |
| Auftragszentrale | Eigene Aufträge, Priorität, bis zehn vorausgesetzte eigene Chats, Zyklusprüfung, Laufphase, Pause und Ergebnislinks | Ein ausführender Agent. Abhängigkeiten verketten Aufträge; kein separater grafischer, modellfreier Workflow-Interpreter. Fehlende/fehlgeschlagene Voraussetzungen blockieren. |
| Benachrichtigungen | Dienst-Annahmezustand und Versuche, bis drei Sendeversuche, optionaler Ersatzkanal, HA-Gruppierung, Ruhezeit-Sammelmeldung auf Wunsch auf Volume | Annahme durch Dienst ist keine Lesebestätigung. Freigabetoken bleiben RAM-only. Wartende Sammelmeldungen werden vor Versand aus der Datei entfernt; keine garantierte Zustellung nach Absturz während Versand. Erinnerungen sind über persönliche Automationen möglich. |
| Modellqualität | Optionale Modelle für Assist, Anhänge und Automationen; synthetische Tests für Fehler, Mehrdeutigkeit, Teilergebnisse und fremde Anweisungen; gespeicherte Ergebnisse | Explizite Modellwahl hat Vorrang. Keine automatische Zertifizierung oder Modellwiederholung nach unbekannten Änderungen. |

## Betrieb und Daten

`/assistant/tasks` und `/assistant/knowledge` sind vom Assistenten aus verlinkt.
Wissen liegt unter `.titanos/assistant/<konto>/knowledge/entries.json`, optionale
Ruhezeitmeldungen unter `notifications/pending.json`. „Alle Chats löschen“ lässt
Wissen, Automationen und ausstehende Meldungen erhalten. Die Wissensseite löscht
nur den Eintrag, nicht Quelldateien oder bereits vorhandene Chatantworten.

PDF liest höchstens 50 MiB und 20 Seiten pro Aufruf. OCR ist ab Werk aus und
verarbeitet höchstens fünf Seiten pro Aufruf mit fester Laufzeitbegrenzung,
RAM-Pipes und ohne temporäre Dokumentkopien im Root-Overlay. Leere oder schlecht
erkannte Seiten bleiben eine fachliche Unsicherheit. Poppler und Tesseract samt
Sprachdaten sind Systempakete; 0.15.0 benötigt deshalb ein volles Systemupdate.

Neue Einstellungen sind optional. Eine Rückkehr zur vorigen Version ist
technisch möglich, aber deren alter Funktionsumfang kennt neue Regeln wie
Entity-Policies oder Automationsbedingungen nicht. **Vor einem Downgrade die
neuen Automationen und Home-Assistant-Steuerung deaktivieren.** Das ist keine
Zusage gleicher fachlicher Ausführung unter einer älteren Version.

## Prüfstand

Lokale Go-/Web-/Svelte-Prüfungen, isolierte REST-/MQTT-/Ollama-Gegenstellen,
Race Detector, echte PDF-Textextraktion und Paketierungs-/Signaturtests.
Browserprüfung für Einstellungen, Automationen, Aufgaben und Wissen auf Desktop
und Mobilgerät. Screenshotprüfung der neuen Aufgaben- und Wissensseiten.

Nicht als geprüft ausgeben: echte Geräte, reale Ollama-Modellqualität,
Smartphone-Zustellung, echte HA-Conversation-Integration und OCR-Erkennungsqualität.
Der Release-Workflow prüft das tatsächlich gebaute Systemabbild und die Rückwege
vor der signierten Veröffentlichung; sein Ergebnis ist separat zu dokumentieren.
