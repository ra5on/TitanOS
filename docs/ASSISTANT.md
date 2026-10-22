# KI-Assistent und Hintergrundagent

Geltender Stand: 0.12.0. Diese Seite ersetzt die früheren Entwürfe mit browsergesteuerter Ausführung. Historische Unterschiede stehen in den Release-Notizen.

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

Der Modelltest kann schon vor dem Speichern mit einer eigenen Frage laufen. Er bietet ausschließlich ein synthetisches Werkzeug an, das einen fehlgeschlagenen Updatecheck zurückgibt. Er zeigt, ob das Modell das Werkzeug tatsächlich aufgerufen hat, und dessen Antwort zur Prüfung der Fehlerinterpretation. Der Test verändert nichts am NAS und ist keine allgemeine Qualitätszertifizierung des Modells.

## Oberfläche und Datenschutz

Nur die eigene Seite `/assistant`, erreichbar über Kopfzeile, schwebenden Knopf und Strg+J. Streaming, Gedanken/Werkzeugdetails, Modellwahl, Kontext der vorherigen Seite, Anhänge, Markdown und interne Dateilinks bleiben erhalten. Chats lassen sich wechseln, während ein Auftrag läuft. Ein Probelauf pro Chat erlaubt Lesezugriffe und zeigt geplante Änderungen, führt sie aber nicht aus. Unterstützte Dateioperationen können auf ausdrücklichen Wunsch über `file_undo_list`/`agent_undo` rückgängig gemacht werden; dies ist kein universelles Rollback. Die vorhandenen Undo-Einträge verfallen nach 30 Minuten oder Dienstneustart.

Konfiguration: `/config/system/assistant.json`, persönliche Auswahl: `/config/users/<konto>/assistant.json`. Verläufe: `/mnt/<volume>/.titanos/assistant/<konto>/` mit Modus 0700, einzelne Chats und separate Ausführungsprotokolle. Höchstens 200 Chats und 12 MiB pro Chat; aktive oder fortsetzbare Chats werden nicht automatisch verdrängt. Lange Kontexte werden deterministisch verkürzt; Originalauftrag, jüngste Schritte und ein als unvollständig gekennzeichneter Arbeitsvermerk bleiben, alte Details können entfallen. Diese Verkürzung ersetzt kein vollständiges Archiv.

Optionale Aufbewahrung in Tagen löscht nur alte abgeschlossene/gestoppte Chats. Null bedeutet keine zeitbasierte Löschung. Der temporäre Modus schreibt neue Verläufe nur in den RAM; vorhandene Volume-Chats werden dadurch nicht gelöscht. Chatdaten gehören weder zur Sticksicherung noch zum allgemeinen Diagnosepaket. Der gesonderte Diagnoseexport enthält standardmäßig nur Ablaufmetadaten; Inhalte sind ausschließlich mit ausdrücklicher Wahl `?content=true` enthalten und werden durch den Diagnosefilter geschwärzt. Freie Texte können trotz Filter sensible Angaben enthalten.

Der konfigurierte Ollama-Server erhält den Modellkontext und zulässige Werkzeugdaten. Hochgeladene Anhänge sind nicht automatisch vollständig anonymisiert. Administrator-Aufträge erzeugen Abschluss-/Blockierungsmeldungen ohne Chatinhalte oder Dateinamen; normale Benutzer erzeugen keine Meldungen im gemeinsamen Administrator-Kanal.

## Sicherheitsgrenzen und Werkzeuge

Die feste API-Liste steht in `backend/internal/api/assistant_api.go`. Keine beliebigen Shellbefehle, kein allgemeines HTTP-Werkzeug und keine Erweiterung der Rechte durch einen Prompt. Konsolen, Kontenanlegen mit Passwort, private Schlüssel, Backup-Passwörter, `.env`, Wiederherstellungscodes und Sticksicherungsdownloads sind ausgeschlossen. Die Bestätigung einer Netzwerkänderung bleibt außerhalb des Agenten; der normale zeitliche Netzwerk-Rückfall gilt auch im Direktmodus.

Dateien, Protokolle, Webseiten, Anhänge und Werkzeugergebnisse sind Daten, keine neuen Aufträge oder Erlaubnisse. Der Standardprompt verlangt, deren eingebettete Anweisungen zu ignorieren und auf das Nutzerziel begrenzt zu arbeiten. Das ist keine Garantie gegen jede Fehlentscheidung eines Modells; die technisch durchgesetzten Rechte gelten zusätzlich.

`file_write` erzeugt Text, Markdown, CSV, JSON, DOCX, XLSX und PDF; Ersetzen erfordert das explizite Werkzeugargument und erhält `.prev`. `files_move` verschiebt bis zu 500 Einträge ohne Überschreiben; `file_copy` verwendet bestehende Kopieraufträge. Lesen von DOCX/XLSX ist möglich, PDF-Lesen nicht. NAS-Anhänge werden mit Kontorechten gelesen. Bilder setzen ein Vision-Modell voraus.

Websuche ist optional, ab Werk aus, für Benutzer separat freizugeben. DuckDuckGo-HTML ohne Schlüssel; `web_read` nur HTTP/HTTPS auf 80/443, mit Größen-/Weiterleitungsgrenzen und Prüfung jeder Zieladresse gegen private, lokale, Tailnet- und NAS-Adressen. Keine garantierte Verfügbarkeit dieser externen Suchschnittstelle.

## Aufbau und Prüfgrenzen

`assistant` enthält Modellkommunikation, Prompt, Verlauf und Kontextverkürzung; `api/assistant_runtime.go` besitzt die Aufträge und Ausführung, `assistant_registry.go`/`assistant_*.go` die berechtigten Werkzeuge. `office` erzeugt Dokumente. Der Web-Build bettet das aktuelle Handbuch ein. Der frühere externe MCP-Server, Bearer-API-Schlüssel und die Agent-App sind entfernt.

Automatisierte Tests verwenden ein nachgebautes Ollama und isolierte Dateiwurzeln: direkte Ausführung, Probelauf, Browserabbruch, Rechte, persistente Ergebnisse, unbekannte Ausgänge, Fotosuche, fehlgeschlagene Registry-Prüfung, Promptverlauf und mehr als 500 Runden. Browserprüfungen testen Einstellungen und Chatdarstellung mit isolierten API-Daten. Nicht abgenommen: echte Ollama-Modelle, reale NAS-Platten/VMs/Container sowie Stromausfall während einer echten Mutation. Ein unterbrochener Neustart-/Updateauftrag kann deshalb bewusst zur Ergebnisprüfung blockieren.
