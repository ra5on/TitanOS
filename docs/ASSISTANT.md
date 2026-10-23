# KI-Assistent und Hintergrundagent

Geltender Stand: 0.15.0. Diese Seite ersetzt die früheren Entwürfe mit browsergesteuerter Ausführung. Historische Unterschiede stehen in den Release-Notizen.

Der Ausbau von 0.15.0 mit Auftragszentrale, persönlichem Wissen, Dokumentbearbeitung, Modelltests und Home Assistant ist in [ASSISTANT-EXPANSION.md](ASSISTANT-EXPANSION.md) mit Grenzen und Datenablage beschrieben.

## Einrichtung und Rechte

Ab Werk ausgeschaltet. Unter Einstellungen › KI-Assistent werden Ollama-Adresse, Modell und erlaubte Bereiche gewählt. Ein Modell mit Werkzeugunterstützung ist nötig; Modelle werden nicht vorinstalliert. Modellverwaltung, Kontextlänge und Temperatur bleiben einstellbar.

Besitzer und Administratoren dürfen den eingeschalteten Assistenten nutzen. Benutzer benötigen eine zusätzliche Freigabe und erreichen ausschließlich ihre eigenen Freigaben mit ihren bestehenden Dateirechten. Je Bereich gilt aus, lesen oder lesen/ändern: Dateien, Docker/Apps, VMs, Speicher, Shares, Backups, Konten, System und Updates. Konten, System und Updates sind standardmäßig nur lesbar. Direktmodus erweitert diese Rechte nicht.

Es gibt zwei Ausführungsarten:

- **Mit Bestätigung** (Standard): einzelne Aktionen erscheinen als Karte; gefährliche Aktionen verlangen ein Häkchen, zerstörerische den Zielnamen. Ein einmal bestätigter Auftrag (`start_task`) erlaubt die folgenden nicht zerstörerischen Schritte ohne weitere Klicks. Zerstörerisches fragt weiter einzeln.
- **Direkt ausführen**: Der Agent führt erlaubte Schritte einschließlich zerstörerischer Änderungen ohne Plan-, Klick-, Häkchen- oder Namensabfrage aus. Die Einstellung erläutert ausdrücklich die Folgen. Rollen, Bereichsrechte, Zielprüfung und die bestehenden API-Sperren gelten weiterhin. Fehlende fachliche Angaben, etwa welches Volume gemeint ist, muss der Agent weiterhin klären.

Die Einstellung gilt für die zugelassenen Konten des NAS. Bestehende Bestätigungsdialoge außerhalb des Assistenten bleiben unverändert. „Alle Agenten pausieren“ unterbricht auch die Modellanfrage; einzelne Aufträge werden anschließend gezielt fortgesetzt.

## Ausführung und Wiederaufnahme

Der Browser zeigt den Verlauf und sendet Freigaben für gespeicherte Aktions-IDs. **titand führt die Aktionen aus** und ermittelt das Ergebnis selbst. Vom Browser behauptete Erfolge werden nicht übernommen. Vor jeder Änderung wird der gespeicherte Vorschlag mit aktuellen Rechten und Zielen neu geplant und mit der ursprünglichen Route, Methode, Bestätigungsstufe und dem Inhalt verglichen.

Aufträge laufen bei geschlossener Seite oder getrenntem Browser weiter. Eine Warteschlange begrenzt gleichzeitige Agentenarbeit auf einen aktiven Auftrag. Im Chat stehen Pause, Fortsetzen, Stopp und ergänzende Anweisungen zur Verfügung. Eine Ergänzung unterbricht die aktuelle Runde und wird vor der Fortsetzung in den Verlauf aufgenommen. Anhänge werden mit neuen Nachrichten gesendet, nicht während einer laufenden Runde.

`maxSteps=0` ist wirklich unbegrenzt: keine 500-Runden-Grenze und keine Gesamtfrist für den Auftrag. Die Oberfläche speichert unbegrenzt; frühere endliche API-Werte bleiben lesbar. Einzelne Modellaufrufe haben zehn Minuten, Werkzeugaufrufe drei Minuten Zeit; die jeweilige API kann engere Fristen setzen. Wiederholung desselben Aufrufs ohne Fortschritt führt nach drei Versuchen zur Abschlussantwort. Identische Mutationen werden innerhalb eines Auftrags auch nach Kontextverkürzung nicht nochmals ausgeführt.

`agent_wait` speichert eine beobachtete Voraussetzung und einen Wiederaufnahmezeitpunkt (5 Sekunden bis 1 Stunde). Die Laufzeit prüft fällige Aufträge alle 15 Sekunden, ohne auf `/config` zuzugreifen. Der Agent liest danach den tatsächlichen Zustand erneut. Ein erwartetes Ereignis wird durch Warten allein nicht als eingetreten behandelt.

Auf einem eingebundenen Volume werden Schritte und Zustände atomar gespeichert. Vor einer Änderung werden der Zustand „wird ausgeführt“ und ein separates Ausführungsprotokoll geschrieben und synchronisiert. Nach einem Dienst-/NAS-Neustart werden wartende und unterbrochene Aufträge wieder geladen. Ein vollständig gespeichertes Ergebnis wird übernommen. Fehlt ein eindeutiges Ergebnis, blockiert der Auftrag: **keine automatische Wiederholung einer möglicherweise ausgeführten Änderung**. „Unterbrechung quittieren und Ergebnis prüfen“ lässt den Agenten mit ausdrücklich unbekanntem Ergebnis weiterarbeiten; es bestätigt keinen Erfolg. Die ursprüngliche Mutation bleibt gegen automatische Wiederholung gesperrt.

Bei fehlendem Chat-Volume stoppt ein zuvor dauerhaft gespeicherter Auftrag, statt seine Checkpoints unbemerkt im RAM oder auf einem anderen Volume fortzusetzen. Ein bewusst pausierter Auftrag startet nach einem Neustart nicht von allein. Ohne Volume bzw. im temporären Modus bleiben Chats nur bis zum nächsten titand-Neustart erhalten; Zeit-Warten funktioniert bis dahin weiterhin. Fremde Konten können Aufträge weder sehen noch steuern.

## Verlässliche Ergebnisse

- Fehler werden als fehlgeschlagene Werkzeugergebnisse mit Ursache in den Modellkontext gegeben. Ein fehlgeschlagener Updatecheck bedeutet „nicht bekannt“, niemals „keine Updates“.
- Docker nennt je Image `not_checked`, `failed`, `current`, `available` oder `pinned`, dazu Fehler und Prüfzeitpunkt. An Digest/ID gebundene Images haben keinen wandernden Tag. Erfolgreich geprüfte Images dürfen getrennt von fehlgeschlagenen beantwortet werden.
- Dateiwurzeln müssen exakt aus `files_list` übernommen werden, beispielsweise `share/Media` oder `volume/nvme`. Ein ungültiger Name liefert die zugänglichen Alternativen. `files_list` und `files_search` liefern Folgeseiten mit `nextOffset`; die Suche zeigt Grenzen und Zahl der untersuchten Dateien. Über 1000 Treffer bzw. die Scan-Grenze erfordern eine engere Suche in Unterordnern.
- `files_search` akzeptiert `extension:"images"`/`"photos"` oder mehrere Endungen mit Komma. Der Foto-Filter umfasst auch HEIC/HEIF, AVIF, TIFF und verbreitete RAW-Formate. Er sucht Dateien, analysiert aber keine Bildinhalte.
- Große Werkzeugantworten werden als ausdrücklich unvollständige Vorschau gekennzeichnet. Der Agent muss Filter/Folgeseiten verwenden, bevor er vollständige Aussagen trifft.
- Eine erfolgreiche API-Anfrage ist von einem verifizierten Endzustand getrennt. Container-/VM-Änderungen bekommen eine anschließende Statusbeobachtung. Dateierzeugung prüft die Existenz und Größe. Hintergrundkopien, Backups, App- und Systemupdates müssen über die jeweiligen Statuswerkzeuge weiter geprüft werden. Keine pauschale Garantie, dass jede fachliche Nachbedingung automatisch geprüft wird.
- Neu erzeugen/Bearbeiten darf bereits versuchte Änderungen nicht aus der Historie entfernen und erneut auslösen. Hier ist eine neue ausdrückliche Nachricht nötig.

## Prompt, Profile und Tests

Der vollständige mitgelieferte Systemprompt ist bearbeitbar (bis 24.000 Zeichen), zurücksetzbar und mit Versionskennung versehen. Bis zu fünf vorherige Fassungen werden serverseitig gehalten. Die Einstellungen zeigen die Standardvorlage und eine Vorschau des gespeicherten Systemkontexts. Der tatsächliche Auftrag ergänzt aktuelle Identität, Rechte, NAS-Version und Ausführungszustand. Der frühere Prompt-Zusatz bleibt kompatibel.

Explizit eingetragene dauerhafte Präferenzen ergänzen den Prompt; es gibt kein heimliches Lernen aus Chats. Bis zu 20 benannte Profile speichern Modell, Prompt, Kontextgröße und Temperatur. Ein Profil wird bewusst auf die Einstellungen angewendet und gespeichert.

Der Modelltest kann schon vor dem Speichern mit einer eigenen Frage laufen. Er bietet ausschließlich ein synthetisches Werkzeug an. Wählbare Szenarien behandeln fehlgeschlagene Prüfungen, Mehrdeutigkeit, Teilergebnisse und fremde Anweisungen. Er zeigt, ob das Modell das Werkzeug tatsächlich aufgerufen hat, und dessen Antwort zur Prüfung der Fehlerinterpretation. Der Test verändert nichts am NAS und ist keine allgemeine Qualitätszertifizierung des Modells.

## Oberfläche und Datenschutz

Nur die eigene Seite `/assistant`, erreichbar über Kopfzeile, schwebenden Knopf und Strg+J. Streaming, Gedanken/Werkzeugdetails, Modellwahl, Kontext der vorherigen Seite, Anhänge, Markdown und interne Dateilinks bleiben erhalten. Chats lassen sich wechseln, während ein Auftrag läuft. Ein Probelauf pro Chat erlaubt Lesezugriffe und zeigt geplante Änderungen, führt sie aber nicht aus. Unterstützte Dateioperationen können auf ausdrücklichen Wunsch über `file_undo_list`/`agent_undo` rückgängig gemacht werden; dies ist kein universelles Rollback. Die vorhandenen Undo-Einträge verfallen nach 30 Minuten oder Dienstneustart.

Konfiguration: `/config/system/assistant.json`, persönliche Auswahl: `/config/users/<konto>/assistant.json`. Verläufe: `/mnt/<volume>/.titanos/assistant/<konto>/` mit Modus 0700, einzelne Chats und separate Ausführungsprotokolle. Höchstens 200 Chats und 12 MiB pro Chat; aktive oder fortsetzbare Chats werden nicht automatisch verdrängt. Lange Kontexte werden deterministisch verkürzt; Originalauftrag, jüngste Schritte und ein als unvollständig gekennzeichneter Arbeitsvermerk bleiben, alte Details können entfallen. Diese Verkürzung ersetzt kein vollständiges Archiv.

Optionale Aufbewahrung in Tagen löscht nur alte abgeschlossene/gestoppte Chats. Null bedeutet keine zeitbasierte Löschung. Der temporäre Modus schreibt neue Verläufe nur in den RAM; vorhandene Volume-Chats werden dadurch nicht gelöscht. Chatdaten gehören weder zur Sticksicherung noch zum allgemeinen Diagnosepaket. Der gesonderte Diagnoseexport enthält standardmäßig nur Ablaufmetadaten; Inhalte sind ausschließlich mit ausdrücklicher Wahl `?content=true` enthalten und werden durch den Diagnosefilter geschwärzt. Freie Texte können trotz Filter sensible Angaben enthalten.

Der konfigurierte Ollama-Server erhält den Modellkontext und zulässige Werkzeugdaten. Hochgeladene Anhänge sind nicht automatisch vollständig anonymisiert. Administrator-Aufträge erzeugen Abschluss-/Blockierungsmeldungen ohne Chatinhalte oder Dateinamen; normale Benutzer erzeugen keine Meldungen im gemeinsamen Administrator-Kanal.

## Sicherheitsgrenzen und Werkzeuge

Die feste API-Liste steht in `backend/internal/api/assistant_api.go`. Keine beliebigen Shellbefehle, kein allgemeines HTTP-Werkzeug und keine Erweiterung der Rechte durch einen Prompt. Konsolen, Kontenanlegen mit Passwort, private Schlüssel, Backup-Passwörter, `.env`, Wiederherstellungscodes und Sticksicherungsdownloads sind ausgeschlossen. Die Bestätigung einer Netzwerkänderung bleibt außerhalb des Agenten; der normale zeitliche Netzwerk-Rückfall gilt auch im Direktmodus.

Dateien, Protokolle, Webseiten, Anhänge und Werkzeugergebnisse sind Daten, keine neuen Aufträge oder Erlaubnisse. Der Standardprompt verlangt, deren eingebettete Anweisungen zu ignorieren und auf das Nutzerziel begrenzt zu arbeiten. Das ist keine Garantie gegen jede Fehlentscheidung eines Modells; die technisch durchgesetzten Rechte gelten zusätzlich.

`file_write` erzeugt Text, Markdown, CSV, JSON, DOCX, XLSX und PDF; Ersetzen erfordert das explizite Werkzeugargument und erhält `.prev`. `files_move` verschiebt bis zu 500 Einträge ohne Überschreiben; `file_copy` verwendet bestehende Kopieraufträge. Lesen von DOCX/XLSX und PDF ist möglich; `document_read` liefert begrenzte PDF-Seitenbereiche und optional OCR. `document_edit` ändert exakte Word-/Textstellen oder vorhandene Excel-Wertzellen in einer neuen Kopie mit geprüftem Ausgangshash. NAS-Anhänge werden mit Kontorechten gelesen. Bilder setzen ein Vision-Modell voraus.

Websuche ist optional, ab Werk aus, für Benutzer separat freizugeben. DuckDuckGo-HTML ohne Schlüssel; `web_read` nur HTTP/HTTPS auf 80/443, mit Größen-/Weiterleitungsgrenzen und Prüfung jeder Zieladresse gegen private, lokale, Tailnet- und NAS-Adressen. Keine garantierte Verfügbarkeit dieser externen Suchschnittstelle.

## Aufbau und Prüfgrenzen

`assistant` enthält Modellkommunikation, Prompt, Verlauf und Kontextverkürzung; `api/assistant_runtime.go` besitzt die Aufträge und Ausführung, `assistant_registry.go`/`assistant_*.go` die berechtigten Werkzeuge. `office` erzeugt Dokumente. Der Web-Build bettet das aktuelle Handbuch ein. Der frühere externe MCP-Server, Bearer-API-Schlüssel und die Agent-App sind entfernt.

Automatisierte Tests verwenden ein nachgebautes Ollama und isolierte Dateiwurzeln: direkte Ausführung, Probelauf, Browserabbruch, Rechte, persistente Ergebnisse, unbekannte Ausgänge, Fotosuche, fehlgeschlagene Registry-Prüfung, Promptverlauf und mehr als 500 Runden. Browserprüfungen testen Einstellungen und Chatdarstellung mit isolierten API-Daten. Nicht abgenommen: echte Ollama-Modelle, reale NAS-Platten/VMs/Container sowie Stromausfall während einer echten Mutation. Ein unterbrochener Neustart-/Updateauftrag kann deshalb bewusst zur Ergebnisprüfung blockieren.

## Werkzeugprüfung seit 0.12.1

Der gemeldete Fehler mit `gpt-oss:20b` (`error parsing tool call`, HTTP 500)
war ein Fehler bei der Verarbeitung einer vom Modell erzeugten Antwort. Er
belegt weder einen NAS-Ausfall noch das Fehlen von Fotos. Der damalige Export
enthielt keine Ursache des vorherigen `files_search`-Fehlers; diese lässt sich
rückwirkend nicht bestimmen.

- Alle Werkzeugaufrufe werden vor Lesen oder Planen gegen das angebotene
  Parameterschema geprüft: Objekt, Pflichtfelder, Typen, erlaubte Werte,
  Zahlen-/Listengrenzen und unbekannte Felder. Keine Umwandlung fehlerhafter
  Argumente in einen ausführbaren leeren Aufruf. Fachliche Prüfungen der
  vorhandenen API bleiben zusätzlich wirksam.
- Ein von Ollama ausdrücklich gemeldeter Tool-Parserfehler wird pro Modellrunde
  höchstens zweimal neu generiert, mit Korrekturhinweis und gespeichertem
  Diagnosecode. Aus der verworfenen Antwort wird kein Werkzeug ausgeführt.
  Bereits abgeschlossene Änderungen werden nicht wiederholt. Netzwerkfehler,
  Speicherfehler des Modells und unklare Ausführungsergebnisse lösen diese
  Wiederholung nicht aus. Streams ohne Abschlussmarker gelten als Fehler.
- Wiederholt identische ungültige oder unbekannte Aufrufe zählen zur bestehenden
  Schleifenerkennung. Dies begrenzt Fehlerschleifen, nicht die Auftragsdauer.
- Metadatenexporte enthalten Fehlerkategorien ohne Inhalte, unter anderem
  `invalid_arguments`, `unknown_tool`, `root_not_found`, `invalid_path`,
  `file_not_found`, `tool_timeout` und `model_tool_parse`. `ok` beschreibt nur
  Werkzeugergebnisse, nicht normale Nutzer-/Assistentennachrichten.
- Fotosuche: exakte Wurzel aus `files_list`, `extension: "images"`, kein
  Namensfilter wie „Fotos“ für die Suche nach Bildtypen. Mehrere Wurzeln separat
  durchsuchen; begrenzte oder fehlgeschlagene Suchen beweisen keine Abwesenheit.
- Mutation von Dateien: absolute Pfade und `..` ablehnen, nicht auf einen anderen
  relativen Zielpfad umschreiben; Leerzeichen in Dateinamen erhalten.
- API-Katalog: Einstellungen nur mit dem jeweils dokumentierten Einstellungsobjekt
  ändern, nicht mit dem gesamten Status. VM-Snapshot-Restore bestätigt die VM;
  Löschen verwendet die Snapshot-ID im Pfad und den Snapshot-Namen zur Bestätigung.
  Systemstatus und Meldungen beachten den Systembereich, eigene Shares den
  Dateibereich. Ohne erlaubte Änderungen kein allgemeines Änderungswerkzeug
  und kein leerer Änderungsplan.

Prüfumfang: Katalogverträge aller angebotenen Werkzeuge für Besitzer,
Administratoren und Benutzer einschließlich verschachtelter Dokument- und
Verschiebeargumente; bestehende API-Routen-/Rechte-/Ausführungsprüfungen;
synthetische HTTP-/Stream-Parserfehler, abgebrochene Streams, begrenzte
Wiederholung, Schleifenerkennung, Diagnose ohne Inhalte und Snapshot-Bestätigung.
Dies ersetzt keinen echten Ollama-/Hardwaretest und garantiert keine fehlerfreien
Modellentscheidungen oder Verfügbarkeit externer Dienste.

## Automationen und Laufzeit (0.13.0)

`/assistant/automations` verwaltet dauerhafte Aufträge je Konto; Link auf der
Assistentenseite. Erstellen, bearbeiten, duplizieren (zunächst deaktiviert),
Zeitplan pausieren, manuell starten, löschen und bis zu 50 Läufe mit Chat-Link.
Der Agent verwendet dafür dieselben APIs über `automation_list`,
`automation_save`, `automation_run` und `automation_delete`. Keine Zusage einer
Automation ohne erfolgreichen API-Aufruf. `agent_wait` bleibt eine Wartefunktion
für den bestehenden Auftrag. Keine automatische Anlage wiederkehrender Aufträge
ohne entsprechenden Nutzerauftrag.

- Einmalig mit absolutem Zeitpunkt; Intervall in Minuten; täglich/wöchentlich mit
  IANA-Zeitzone. Nicht existierende Uhrzeiten beim Wechsel zur Sommerzeit werden
  übersprungen, doppelte Uhrzeiten verwenden die erste Instanz. Nächste Starts
  werden angezeigt. Verpasste Termine: letzten innerhalb 24 Stunden nachholen
  (Standard) oder überspringen. Keine Aufholserie aller verpassten Termine.
- Ereignisse: neuer Meldungstyp, USB-Dateisystem-UUID erscheint, Volume überschreitet
  Prozentgrenze, Metadatenänderung im ausdrücklich ausgewählten Ordner. Die erste
  Beobachtung setzt die Ausgangsbasis; Ereignisse werden zusammengefasst, mindestens
  eine Minute zwischen Starts. Ordnerüberwachung ist nicht rekursiv und auf 5.000
  Einträge begrenzt; keine Inhaltsindizierung. USB- und Speicherabfragen erfolgen
  nur für konfigurierte Ereignisse/Bedingungen. Keine LLM-Aufrufe zur Ereigniserkennung.
  Beobachtung ungefähr alle 15 Sekunden; kurzlebige Ereignisse zwischen zwei
  Beobachtungen können entgehen. Meldungen bleiben an den bestehenden RAM-Verlauf
  gebunden; keine lückenlose Ereignisquelle über NAS-Auszeiten hinweg.
- Bedingungen: erforderliches eingebundenes Volume, optional Ende der Speicherwartung.
  Bei fehlender Voraussetzung wird ein Lauf vorgemerkt. Überschneidungen überspringen
  oder genau einen Folgelauf vormerken. Blockierte/pausierte/freizugebende Chats
  gelten weiterhin als offen; zuerst im Chat beheben oder stoppen.
- Ausführung übernimmt die globale Einstellung oder verwendet ausdrücklich
  Bestätigung/Direktmodus. Konten- und Bereichsrechte gelten weiterhin und werden
  vor Werkzeugen geprüft. Standardbenutzer dürfen nur eigene Datei-Ereignisse
  nutzen. Ziele im Auftrag erweitern keine Rechte; freie Auftragsprosa ist keine
  zusätzliche technische Begrenzung auf bestimmte Ressourcen.
- Benachrichtigungen: nur Fehler (Standard), nach jedem abgeschlossenen Lauf oder
  keine. Der bestehende Benachrichtigungskanal ist nur für Administratoren;
  private Ergebnisse bleiben im jeweiligen Chat. Pausieren/Löschen betrifft
  zukünftige Starts; bereits gestartete Chats werden separat gesteuert.
- Definitionen und Zustände liegen unter
  `/mnt/<volume>/.titanos/assistant/<konto>/automations/definitions.json`, atomar
  gespeichert, Verzeichnisse 0700. Kein RAM-Ersatz ohne verfügbares Daten-Volume,
  kein zyklischer Zugriff auf `/config`. Bearbeitung mit Revisionsprüfung.
  Ein Lauf erhält vor dem Start eine dauerhaft gespeicherte Chat-ID und einen
  Schnappschuss seines Auftrags/Modus. Wiederanlauf verwendet dieselbe ID; unbekannte
  Aktionsausgänge behalten die vorhandene Mutationssperre. Betriebsdaten werden
  auf dem Volume gelesen; unveränderte Definitionen werden nicht neu geschrieben.
- Ein Downgrade auf 0.12.x kennt keine Automationszeitpläne. Dateien bleiben
  erhalten; bestehende Chats behalten ihre bekannten Zustände. Für geplanten
  Downgrade vorher Zeitpläne pausieren und aktive Aufträge stoppen.

### Fehlerbehandlung und überprüfte Ergebnisse

Aktionskörper werden strukturell statt nach JSON-Feldreihenfolge verglichen;
große JSON-Ganzzahlen bleiben beim Speichern/Laden unverändert. Wirklich geänderte,
noch nicht ausgeführte Vorschläge verfallen und können neu geplant werden.
Bereits versuchte Änderungen und unklare Ergebnisse werden weiterhin nicht
blind wiederholt. Ein Mehrfach-Werkzeugaufruf hält den Aktionsindex statt eines
nach Slice-Vergrößerung veralteten Zeigers.

Beim Speichern geänderter Einstellungen werden laufende Modellaufrufe beendet;
aktive Aufträge werden mit der neuen Einstellung wieder eingereiht, globales
Pausieren/Deaktivieren pausiert sie. Wechsel zu Direktmodus reiht wartende
Bestätigungen ebenfalls ein, außer Probeläufen und expliziten
Bestätigungs-Automationen. Explizit pausierte/gestoppte Aufträge bleiben so.
Die Wiederaufnahme erfolgt mit dem nächsten Scheduler-Durchlauf.

Sichtbare Laufphasen: Warteschlange, Modellprüfung, Modell, lesendes Werkzeug,
Aktion, Verifikation und erneuter Versuch. Diagnosen enthalten Schrittzeiten.
Modellprüfung: 30 Sekunden; Modellaufruf: höchstens zehn Minuten und zwei Minuten
ohne Stream-Fortschritt. Vorübergehende Verbindungsfehler und HTTP 429/502/503/504
werden höchstens zweimal mit Abstand erneut versucht, ausschließlich vor
Werkzeugdispatch. Parserfehler behalten ihre gezielte Wiederholung. Abgeschnittene
Streams und sonstige HTTP-500-Fehler werden nicht pauschal erneut ausgeführt.
Die Gesamtauftragsdauer bleibt unbegrenzt. Vier aufeinanderfolgende fehlgeschlagene
Werkzeugschritte ohne Erfolg beenden die Werkzeugschleife mit einer Antwort zu
den offenen Punkten, auch bei wechselnden ungültigen Argumenten. Erfolgreiche
Schritte setzen diese Fehlerzählung zurück.

`shares_list` nennt exakte Root-IDs und Verfügbarkeit/Schreibbarkeit. Der
Standardprompt verlangt vorhandene Freigaben für Dokumentaufträge; „erste“ meint
die erste verfügbare beschreibbare Freigabe in der gelieferten Reihenfolge.
Eigene gespeicherte Prompts bleiben erhalten; neue Vorlagen werden nicht
ungefragt über persönliche Änderungen geschrieben.

`file_write` liefert den Hash der tatsächlich gerenderten Datei. Die Verifikation
liest anschließend die gespeicherten Bytes und vergleicht den Hash (bis 50 MiB),
Word/Excel werden zusätzlich geparst. Eine gültige Datei beweist nicht die fachliche
Richtigkeit erfundener Eingangsdaten; die Quellen müssen weiterhin beobachtet sein.

Prüfungen: isolierte gespeicherte Word-Pläne mit Bestätigung und Direktmodus,
Automationsanlage durch Werkzeug und anschließender Dokumentlauf, Wiederanlauf
zwischen Reservierung und Chatanlage, Überschneidung, verpasste Termine, Sommerzeit,
Ordnerereignisse, Voraussetzungen, Kontentrennung, geänderte Rechte, Revisionen,
Volume-Verlust und Abbruch eines wartenden Modells beim Einstellungswechsel.
Browserprüfungen verwenden API-Fixtures auf Desktop/Mobil. Kein Nachweis für
jedes reale Ollama-Modell oder echte Hardware; insbesondere der konkrete
`gpt-oss:20b`-Betrieb des Projektinhabers bleibt ein gesonderter Abnahmetest.

## Benachrichtigungen, Home Assistant und Statistik (0.14.0)

### Persönliche Benachrichtigungen

Jedes Konto richtet unter *Mein Konto › Meine Benachrichtigungen* eigene Wege ein. Die Einstellungen liegen in `/config/users/<konto>/notify.json`.

- **Wege:** E-Mail über den gemeinsamen Mailserver an die eigene Adresse, ntfy, Telegram, Discord und die Home-Assistant-App.
- **Ereignisse:**
  - `confirmation`: ein Auftrag wartet auf eine Freigabe
  - `done`: ein Auftrag ist fertig
  - `blocked`: ein Auftrag kommt nicht weiter
  - `agent`: Nachricht über das Werkzeug `notify_me`
- **Inhalt:** `summary` (bis 300 Zeichen der letzten Antwort bzw. der Begründung der wartenden Aktion) oder `none`. Anhänge und Werkzeugdaten stehen nie in einer Nachricht.
- **Ruhezeit:** Nachrichten werden standardmäßig im RAM zurückgehalten, auf Wunsch auf dem Daten-Volume ohne Freigabetoken gespeichert und danach gesammelt geschickt; Fehler und Nachrichten mit `priority: high` kommen sofort.
- **Einmal je Ereignis:** Je Auftrag, Ereignis und Aktion wird ein Versandauftrag angenommen; fehlgeschlagene Dienstaufrufe werden höchstens dreimal versucht. Optional springt ein Ersatzkanal ein. Bei unbekanntem Zustellausgang sind doppelte Zustellungen durch den Dienst möglich.
- Bei Automationen gilt ihre Einstellung: `errors` meldet `blocked` und `confirmation`, `always` alles, `never` nichts.

Der gemeinsame Admin-Kanal bleibt wie bisher ohne Chatinhalte.

`notify_me` schreibt nur an den Besitzer des Chats. Es gilt höchstens eine Nachricht pro Minute und 20 pro Stunde. Im Probelauf geht nichts hinaus. Das Werkzeug wird nur angeboten, wenn das Konto einen Weg eingerichtet und das Ereignis `agent` eingeschaltet hat. Es ist keine Aktion und braucht keine Bestätigung, weil es am NAS nichts ändert.

### Freigeben aus Benachrichtigungen

Die Einstellung `pushApproval` ist ab Werk aus. Ist sie an, tragen Nachrichten über wartende Aktionen Knöpfe:

- **ntfy:** `http`-Aktionen, die `POST /api/v1/assistant/push/{token}/{approve|reject}` aufrufen.
- **Home-Assistant-App:** Aktionen `TITANOS_APPROVE_<token>` und `TITANOS_REJECT_<token>`. Die App meldet sie als `mobile_app_notification_action` über die WebSocket-Verbindung.

Das Token hat 256 Bit, liegt nur im RAM, gilt einmal und 15 Minuten lang und ist an Konto, Chat und Aktion gebunden. Danach geht die Freigabe denselben Weg wie in der Oberfläche. Rechte, Zustand und Stufe werden dabei neu geprüft. Aktionen der Stufe `destroy` bekommen nie Knöpfe.

Wer die Nachricht lesen kann, kann damit freigeben. Ein ntfy-Thema sollte deshalb geschützt sein.

### Home Assistant

Der Bereich `home` ist ab Werk aus und steht nur Besitzern und Administratoren zur Verfügung.

- `ha_entities` und `ha_state` lesen freigegebene Entitäten. Ihre Antworten gelten als nicht vertrauenswürdig.
- Die Aktion `ha_call` geht über `POST /api/v1/homeassistant/call`.
- Automationen können auf Zustandswechsel freigegebener Entitäten reagieren (Ereignis `homeassistant` mit `to`, `from`, `above`, `below`).
- Assist-Fragen laufen als Chat des in den Home-Assistant-Einstellungen gewählten Kontos.

Einzelheiten stehen in [HOME-ASSISTANT.md](HOME-ASSISTANT.md).

### Statistik, Vorlagen, Dashboard

- **`stats`** je Chat: Runden, gelesene (`prompt`) und geschriebene (`generated`) Token aus dem letzten Ollama-Chunk, Modell- und Werkzeugzeit. Die Felder stehen unter dem Chat und im Metadatenexport.
- **Vorlagen** füllen im Automationseditor nur das Formular aus. Die Vorlage für die USB-Kopie läuft mit Bestätigung.
- **Dashboard-Kachel „KI-Assistent“:** die letzten fünf eigenen Aufträge und die nächsten drei Automationen.

### Prüfgrenzen

Geprüft mit nachgebautem Ollama, ntfy, Home Assistant (REST und WebSocket) und MQTT-Broker. Nicht geprüft sind echte Smartphones, Apps, ein echtes Home Assistant und ein echter Broker.
