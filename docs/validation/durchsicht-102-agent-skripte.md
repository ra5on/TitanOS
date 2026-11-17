# Durchsicht 1.02 – KI-Agent (bisher ungelesene Teile) sowie Skripte und CI: Fundliste

Auftrag des Inhabers vom 16.11.2026: „die ganze Codebasis prüfen und den Code dementsprechend aufräumen und verbessern, Stabilität reinbringen“. Diese Durchsicht ist eine von fünf (Zweig `review/r5`, Stand 1.01). Sie hat zwei Teile:

- **Teil A:** die Teile des KI-Agenten, die in 0.99 nicht gelesen wurden: Dokumente, Schutz/Wächter (mit Notfallzugang und Notfallhandbuch), Vorausschau (Was-wäre-wenn, Hardware-Berater), Familie (geteilte Projekte, Backup-Buddy, Skill-Katalog), Notizen, Volume-Umzug, Cluster und Code-Sandbox. Jeweils das Backend und die Oberfläche. Chat und Lauf-Verwaltung waren nicht Teil dieser Durchsicht.
- **Teil B:** `scripts/*.py`, `scripts/*.sh`, `scripts/tests/` und `.github/workflows/*.yml`.

**So wurde vorgegangen:** Vier Teile wurden parallel gelesen: Dokumente; Schutz; Vorausschau und Familie; Skripte und CI. Notizen, Umzug, Cluster und Sandbox habe ich selbst gelesen. Jeder Fund wurde am Code nachverfolgt. Jeder behobene Fund hat einen Test, der mit dem alten Code fehlschlägt; die Ausnahmen sind in der Tabelle genannt. Bei A-8 und A-43 (1.04) braucht der Test einen Haken im Code (`emergencyGrantStep`, `agentMoveChecked`); „schlägt mit dem alten Code fehl“ heißt dort: alter Ablauf mit demselben Haken an derselben Stelle.

**Schwere:**

| Stufe | Bedeutung |
|---|---|
| hoch | Rechte, Datenverlust oder CI rot |
| mittel | Falsches Verhalten mit spürbarer Folge |
| niedrig | Kleinigkeit, Aufräumen |

**Status:**

| Status | Bedeutung |
|---|---|
| behoben | mit Commit auf `review/r5`; „behoben in 1.03/1.04“: im jeweiligen Release |
| notiert | nicht gebaut, mit Grund (Stand 1.02; seit 1.04 keiner mehr) |
| bewusst gelassen | nicht gebaut, der Grund steht in der Zeile |
| Entscheidung | wartet auf den Inhaber; Frage und Empfehlung stehen in der Zeile |
| Oberfläche: wt-104c | der Teil im NAS ist erledigt, der Rest liegt bei der Oberfläche |

## Teil A – KI-Agent

### Schutz und Wächter

| Nr. | Ort | Wirkung | Schwere | Status |
|---|---|---|---|---|
| A-1 | `api/agent_protect_emergency.go` `emergencyGrant`, `emergencyReconcile` | **Notfallzugang:** Eine wartende Anfrage und ein laufender Zugang behielten die Freigaben vom Zeitpunkt der Anfrage. Nahm der Besitzer eine Freigabe aus dem Eintrag der Person, bekam sie nach der Wartezeit trotzdem Lesezugriff darauf. Ein laufender Zugang behielt sie die vollen 30 Tage, obwohl der Code-Kommentar das Gegenteil sagte. | hoch (Rechte) | behoben `bf970268`, `TestEmergencyFollowsNarrowedShares` |
| A-2 | `api/agent_protect_guard.go` `guardLoadLocked`, `guardRefresh` | **Alarm für immer „aktiv“:** Die Vorfälle des Wächters liegen nur im RAM. Ein Alarm, der vor einem Neustart von titand (auch einem App-Update) als „aktiv“ gespeichert war, endete nie: Wiederherstellen und Freigeben wurden für immer abgelehnt. Dasselbe geschah, wenn der Takt einen Vorfall beendete, während eine langsame Sperre ihren Alarm noch schrieb, und mit „restoring“ nach einem abgebrochenen Wiederherstellen. | hoch | behoben `391df67e`; `TestGuardActiveAlertAfterRestart`, `TestGuardRestoringAlertAfterRestart`, `TestGuardAlertWithoutIncidentStops` |
| A-3 | `agent_protect_guard.go` `guardSaveLocked` | Beim Kürzen auf 20 Alarme fielen die ältesten weg, auch offene und gesperrte. Bei einer gesperrten Freigabe war damit der Vermerk weg, wer vorher Schreibrecht hatte. | mittel | behoben `391df67e`, `TestGuardTrimKeepsLockedAlerts` |
| A-4 | `ProtectGuard.svelte` | Der Bestätigungsdialog wirkte auf den Alarm, der beim Klick oben stand. Die Seite liest während eines Angriffs alle 5 s neu. Kam währenddessen ein zweiter Angriff auf eine andere Freigabe, traf „Fehlalarm“ diese: Sie wurde mitten im Angriff wieder geöffnet. | mittel | behoben `d86f2ccf`, `tests/components/protect-guard.test.ts` |
| A-5 | `ProtectGuard.svelte` | Nach dem Ende des Angriffs las die Seite nicht mehr nach. Ein automatisches Wiederherstellen blieb bei „gestoppt“ bzw. „stellt wieder her“ stehen, und der Knopf „Wiederherstellen“ bekam nur eine Ablehnung. | niedrig | behoben `d86f2ccf` (gleicher Test) |
| A-6 | `agent_protect_handbook.go` `handbookCurrent` | Das Notfallhandbuch wird nur nachts neu erzeugt. Bis dahin enthielt es abgeschaltete Kontakte bzw. alte Nummern, und eine Person mit Notfallzugang las sie. | niedrig (Datenschutz) | behoben `8397102f`, `TestHandbookFollowsContactSettings` |
| A-7 | `agent_protect_emergency.go` | Eine geänderte Wartezeit gilt nicht für schon offene Anfragen. | niedrig | behoben in 1.03: die Wartezeit eines wartenden Antrags folgt der Einstellung (kürzer und länger, ab dem Antrag); `TestEmergencyWaitingTimeFollowsTheSetting`. |
| A-8 | `agent_protect_emergency.go` | Zwei gleichzeitige Freigaben (Takt und „Jetzt freigeben“) können sich überschneiden. Folge: weniger Zugang, nicht mehr. | niedrig | behoben in 1.04: der Zustand ändert sich unter derselben Sperre wie die Freigaben; eine zweite Freigabe wartet und findet den Antrag entschieden. `TestEmergencyGrantTwiceAtOnce` (ohne Korrektur: „freigegeben“ ohne Freigabe, kein Lesezugriff) |
| A-9 | `guardLockShare`, `emergencyGrant` | Sperre und Notfallzugang lesen die Freigabe und schreiben sie ganz zurück. Ein Administrator, der ein vorher geöffnetes Freigabe-Formular speichert, kann eine Sperre still aufheben. | niedrig | behoben in 1.04: `PUT /shares/{name}` antwortet 409 `share_guard_locked`, solange der Wächter die Freigabe sperrt und das Speichern wieder Schreibrecht gäbe (SMB `rw` oder NFS schreibend); Speichern, das die Freigabe lesbar lässt, geht. Prüfung und Speichern unter der Sperre des Wächters. `TestGuardLockSurvivesAnOldShareForm` (ohne Korrektur: 204, Sperre aufgehoben). Übersetzung `error.share_guard_locked`. Dabei gefunden: der minütliche Abgleich der Freigabe „KI-Agent“ setzte die Rechte zurück und hob eine Sperre auf dieser Freigabe binnen einer Minute auf; jetzt bleibt sie während der Sperre lesbar (`TestGuardLockOfTheAgentShareStays`). Übrige Schreiber von Freigaben (Speicher für Home Assistant) lesen den aktuellen Stand und geben höchstens dem eingetragenen Konto ausdrücklich neues Recht; das bleibt eine bewusste Handlung des Administrators |
| A-10 | `agent_protect.go` Werkzeug `emergency_handbook` | Das Recht wird nur beim Aufbau der Werkzeuge geprüft. Endet ein Zugang während eines Laufs, liefert das Werkzeug bis zum Ende des Laufs weiter. | niedrig | behoben in 1.04: das Werkzeug prüft bei jedem Aufruf Konto, Schalter und Zugang. `TestHandbookToolEndsWithTheAccess` |
| A-11 | `agent_protect_guard.go` `guardHold` | Snapshot über den eigenen Runner statt `storage.SnapshotAction`. Damit fehlen die Prüfung auf `-dev`, die Sperre `m.actions` und `diskwake.Note`. | niedrig (Aufräumen) | bewusst gelassen: `storage.SnapshotAction` passt nicht (sie macht einen Snapshot des Volumes, der Wächter kopiert einen vorhandenen Snapshot). `-dev` ist abgedeckt: `guardHold` bekommt nur einen Snapshot aus `storage.Snapshots`, und das lehnt im Entwicklungsmodus ab. Es fehlen nur die Sperre `m.actions` und der Vermerk für das Aufwecken; scheitert die Kopie, weil gleichzeitig ein Snapshot gelöscht wird, steht eine Warnung im Protokoll, und der Alarm behält den ursprünglichen Snapshot. Eine eigene Methode im Paket `storage` (anderer Bereich) wäre mehr Umbau als Nutzen |
| A-12 | `ProtectEmergency.svelte` | „Ablehnen“ (endgültig) und „Zugang beenden“ fragen nicht nach; die Zähler in `ProtectGroup.svelte` werden erst beim nächsten Öffnen neu gelesen. | niedrig | Backend: nichts nötig (die Routen nehmen keinen Bestätigungswert). Oberfläche: wt-104c (Dialoge, Zähler) |

### Dokumente

| Nr. | Ort | Wirkung | Schwere | Status |
|---|---|---|---|---|
| A-13 | `api/users.go` `handleDeleteUser`, `agent_docs_index.go` `docsForgetUser` | **Konto löschen:** Gelöscht wurde weder die eigene Einstellung des Agenten (mit dem Passwort des Postfachs) noch Gedächtnis und Protokoll von „Dokumente“. Ein neues Konto gleichen Namens übernahm beides: Der Papierkram holte die Post der früheren Person in den Ordner der neuen, und alte Ausschnitte kamen in der Suche wieder. | hoch (Rechte/Datenschutz) | behoben `97ca383a`, `TestDeletedAccountLeavesNoDocuments` |
| A-14 | `agent_docs_deadline.go` `docsRemind` | Der Name einer Erinnerung wurde auf 79 Zeichen plus „…“ gekürzt, ein Auftrag nimmt aber 80 Byte. Jeder gekürzte Name und jeder durch Umlaute längere wurde abgewiesen. Mit „Gleich als Erinnerung“ wurde die Frist nie geplant (nur protokolliert), „Übernehmen“ scheiterte für immer. | hoch (im Erinnerungs-Modus) | behoben `3ac0b0bf`, `TestDocsRemindLongName` |
| A-15 | `agent_volume.go` | **Umzug:** `.titanos/docs` (Gedächtnis und Papierkram-Protokoll mit Rückgängig und offenen Fragen) zog nicht mit. Nach dem Umzug begann „Dokumente“ leer; ein Umzug zurück griff sogar den alten Stand wieder auf. | mittel | behoben `e995791a`; `TestAgentMoveTakesTheDocuments`, `TestAgentMoveSetsOldDocumentsAside`. Zurückgebliebene Stände legt der Umzug unter `agent-old/<Zeit>-left` beiseite, ein laufender Dokumente-Lauf verhindert den Umzug. |
| A-16 | `agent_docs_index.go` `docsRun` | Ein Lauf, der an sein Zeitlimit (2 h) kam, nahm alle noch nicht erreichten Dateien aus dem Verzeichnis und markierte die Ordner als gelesen. Die nächste Nacht übersprang sie. | mittel | behoben `30c3e025`, `TestDocsCancelledRunKeepsTheIndex` |
| A-17 | `agent_docs_paper.go` `paperRun` | Eine Frage („Zuordnen“) zu einer im Dateimanager gelöschten Datei blieb für immer stehen und blockierte jede spätere Datei gleichen Namens (`scan.pdf` eines Scanners). Eine Datei, die unter gleichem Namen ersetzt wurde, wurde ebenfalls nie gelesen. | mittel | behoben `6fb17701`, `TestPaperQuestionAboutAGoneFile`. Neues Feld `modified` im Protokoll, ältere Versionen ignorieren es. |
| A-18 | `agent_docs_deadline.go` `docsDeadlinesFound` | Die Fälligkeit einer abgelegten Rechnung kam zweimal: vom Papierkram und vom Verzeichnis, weil `Dokumente/Ablage` ab Werk in `Dokumente` liegt. Folge: zwei Vorschläge bzw. zwei Sätze Erinnerungen. | mittel | behoben `aea046c8`, `TestDocsBillDueOfferedOnce` |
| A-19 | `DocsViews.svelte` | Der Papierkram fragte je Monat zweimal; eine späte Antwort zeigte den falschen Monat. | niedrig | behoben `6a9cf6fa`, `tests/components/docs-paper.test.ts` |
| A-20 | `DocsGroup.svelte` | Der Zustand eines Laufs wurde nach dem Schließen bis zu 2 h weiter alle 2 s abgefragt. | niedrig | behoben `6a9cf6fa` (ohne eigenen Test, geprüft mit svelte-check und Browser) |
| A-21 | `DocsFolderPicker.svelte` | Eine späte Antwort zeigte die Unterordner eines vorigen Ordners unter dem neuen Pfad. | niedrig | behoben `98480637`, `tests/components/docs-folder-picker.test.ts` |
| A-22 | `DocsGroup.svelte`, `FeaturesPanel.svelte` | Zwei schnelle Änderungen hintereinander: Die zweite schickt den Stand ohne die erste und überschreibt sie. | niedrig | Backend: `rev` geliefert (siehe A-30); die heutige Seite schickt es schon mit, ein zweites Speichern vor der Antwort des ersten bekommt 409 statt still zu überschreiben. Oberfläche: wt-104c (Meldung/Neuladen) |
| A-23 | `agent_docs_paper.go` `paperwork_summary` | Das Werkzeug liefert Namen, Pfade und Beträge ohne die Ordnerregeln des Agenten (anders als `documents_search`). | niedrig | Entscheidung: Soll die Papierkram-Übersicht des Agenten die Ordnerregeln beachten, also Dokumente aus Ordnern, die der Agent nicht lesen darf, weder unter „zuletzt abgelegt“ noch in den Summen zeigen? Empfehlung: ja, wie `documents_search`; die Seite „Papierkram“ selbst bleibt unverändert |
| A-24 | `agent_docs_paper.go` | Scheitert ein späterer Anhang einer Mail, werden die früheren beim nächsten Mal als „(2)“ erneut abgelegt. `Hints` haben keine Obergrenze. `docsSearch` liest bei jeder Suche das ganze Verzeichnis (bis 128 MB JSON) in den RAM. | niedrig | behoben in 1.04 (zwei von drei): eine Mail kommt ganz oder gar nicht in den Eingang (Geschriebenes wird nach einem Fehler entfernt), Hinweise zu Dateien, die nicht mehr im Eingang liegen, fallen weg. `TestPaperMailTakenWholeOrNotAtAll`. Bewusst gelassen: `docsSearch` liest das Verzeichnis je Suche. Gemessen (künstlich, je Dokument die Höchstmenge Text): 1000 Dokumente 25 MB, Lesen 36 ms, Suche 0,4 s; 3000 Dokumente 75 MB, 81 ms, 1,1 s. Gesucht wird nur auf Anfrage, nie zyklisch; ein Zwischenspeicher hielte 25–75 MB je Konto dauerhaft im RAM, in dem das ganze System läuft |

### Vorausschau und Familie

| Nr. | Ort | Wirkung | Schwere | Status |
|---|---|---|---|---|
| A-25 | `agent_foresight_model.go` `advisorInput` | **Hardware-Berater:** Ein Lesefehler der Platten (lsblk im Zeitlimit, Seite während „Jetzt prüfen“ verlassen) ergab einen Bericht ohne Platten. Er ersetzte den, der vor einer ausfallenden Platte warnte, und ging am Monatsersten als „nichts Auffälliges“ hinaus. | mittel | behoben `9ecc54d6`, `TestAdvisorKeepsTheReportWhenDisksAreNotReadable` |
| A-26 | `ForesightAdvisor.svelte` | Zwei Grafikkarten desselben Modells ergaben doppelte Schlüssel, und Svelte brach die Seite ab. | mittel | behoben `98cbac64`, `tests/components/foresight-advisor.test.ts` |
| A-27 | `ForesightAdvisor.svelte` | „Prüft gerade“ eines nächtlichen Laufs blieb bis zum erneuten Öffnen. | niedrig | behoben `98cbac64` (gleiche Testdatei) |
| A-28 | `agent_family.go` `foreignChat` | **Geteilte Projekte:** Andere Mitglieder lasen über die Notizen des Chats Argumente zurückgenommener Schritte (Pfade in privaten Ordnern), Fehlertexte und die Titel privater Notizen, Skills, Aufträge und Home-Assistant-Geräte des Autors. | mittel (Datenschutz) | behoben `beebac7a`, `TestForeignChatKeepsTheAuthorsSteps` |
| A-29 | `agent_foresight.go` Werkzeug `hardware_advice` | Mit „Ersatz mit Preis“ aus gab das Werkzeug gespeicherte Angebote fremder Webseiten an das Modell, ohne den Chat als „fremder Inhalt gelesen“ zu markieren. | niedrig | behoben `26063c50`, `TestHardwareAdviceWithoutPricesHasNoOffers` |
| A-30 | `agent_features.go` `PUT /agent/features` | Die Seite schickt immer den ganzen Block der Funktionen. Zwei Administratoren oder zwei Tabs: Das Schalten einer Funktion setzt eine inzwischen von anderer Seite geschaltete still zurück (z. B. den Wächter). | mittel | behoben in 1.03: Feld `rev` (Hash des gespeicherten Stands) in `GET`/`PUT /agent/features`, veraltet → 409 `agent_features_changed`, ohne `rev` wie bisher; `TestAgentFeaturesStaleSaveIsRefused`. |
| A-31 | `agent_foresight_advisor.go` `advisorLoad` | Ein Bericht aus der Zeit, als das Volume schlief, wurde beim Aufwachen durch die ältere Datei ersetzt (samt Vermerk des Monatsberichts). | niedrig | behoben `f9065228`, `TestAdvisorLoadKeepsANewerRun` |
| A-32 | `ForesightGroup.svelte` / `ForesightSheet.svelte` | Ein Speicherfehler im Blatt ist unsichtbar: Die Fehlerzeile liegt hinter dem Blatt. | niedrig | Oberfläche: wt-104c (nur Oberfläche, im NAS nichts zu tun) |
| A-33 | `FamilyBuddy.svelte` | Kontingent als „GB“ eingegeben, gespeichert in GiB. 500 „GB“ erscheinen als „537 GB“. | niedrig | behoben in 1.03: Eingabe und Speicherung in GB (1000³), gespeicherte Werte unverändert, unverändertes Speichern behält die Bytes; `tests/agent54-family.test.mjs`. |
| A-34 | `agent_reflect.go` `agentVolumeAsleep` | Ein Fehler der Schlafabfrage gilt als „wach“; der Berater liest und schreibt dann trotzdem. Der Berater selbst behandelt Fehler als „schläft“. | niedrig | bewusst gelassen: „Fehler = wach“ ist Absicht. Scheitert die Schlafabfrage dauerhaft (lsblk kaputt), fielen sonst alle nächtlichen Arbeiten des Agenten still aus; einmal eine Platte zu wecken ist das kleinere Übel. Der Berater behandelt nur die Platte des SMART-Verlaufs (`volumeAsleep`) bei Fehlern als schlafend, sein eigener Speicher folgt derselben Regel wie die übrigen |
| A-35 | `agent_family54_buddy.go` | Rennen zwischen zwei gleichzeitigen Partner-Änderungen und dem Abgleichauftrag; Partner-Änderung bei ausgeschaltetem Buddy. | niedrig | behoben in 1.04: der Abgleich liest den gespeicherten Partner und läuft einzeln; das Lösen eines Partners gleicht am Ende noch einmal ab. `TestFamily54BuddyJobFollowsTheStoredPartner`. Partner-Änderung bei ausgeschaltetem Buddy: kein Fund, die Aufträge laufen laut `docs/agent54/family.md` auch dann als gewöhnliche Sicherung weiter |
| A-36 | `agent_foresight_advisor.go`, `agent_reflect.go` | „Heute um HH:MM fällig“ steht doppelt (`advisorDue`, `reflectDue`); HH:MM wird auf zwei Arten geprüft. `stepsPreview` wird nur vom Test benutzt; Kommentare in `agent_features.go` sind veraltet. | niedrig (Aufräumen) | behoben in 1.04 (Aufräumen, kein Test möglich, bestehende Tests grün): ein Helfer `dailyDue`/`todayAt` für alle Tageszeiten, HH:MM überall mit `clockRe`, Kommentare in `agent_features.go`. `stepsPreview` liegt in `web/src/lib/features/family.ts`: Oberfläche: wt-104c |

### Notizen, Umzug, Cluster und Code-Sandbox

| Nr. | Ort | Wirkung | Schwere | Status |
|---|---|---|---|---|
| A-37 | `NotesPanel.svelte` | **Notizen:** Wer weitertippte, während ein Speichern noch unterwegs war, schickte das nächste Speichern mit der alten Zeit der Datei. titand antwortete „anderswo geändert“: Die Notiz zeigte einen Konflikt, die letzten Worte waren nicht gespeichert, und „Neu laden“ hätte sie verworfen. Ein Löschen konnte die Notiz durch ein noch laufendes Speichern neu anlegen. | mittel | behoben `2d9c57f5`, `tests/components/notes-save.test.ts` |
| A-38 | `agent_notes.go`, `agent/store.go` | Angeheftete Notizen wurden ohne Sperre gelesen, geändert und geschrieben. Zwei gleichzeitig Angeheftete: Im Test blieben 5 von 16. | niedrig | behoben `30aabb0c`, `TestAgentNotePinsAtOnceAllStay` (`Store.UpdateNotesMeta`) |
| A-39 | `agent_notes.go` `moveNote` | Ein verschobener Ordner konnte Notizen tiefer als vier Ordner legen. Die Liste ließ sie dann weg. | niedrig | behoben `92d9ecbb`, `TestAgentNoteMoveKeepsNotesInSight` |
| A-40 | `routes/settings/agent/+page.svelte`, `AgentVolume.svelte` | Das Abfragen eines Modell-Downloads lief nach dem Verlassen der Seite weiter (ein zweiter Download gab ein zweites Intervall). Der Umzug fragte nach dem Verlassen weiter, wenn eine Antwort noch unterwegs war. | niedrig | behoben `6b49bfd0`; `tests/components/agent-volume-poll.test.ts` (Download ohne eigenen Test) |
| A-41 | `agent_extra.go` `cluster_action` | Das Werkzeug gehört zum Bereich „VMs“, auch für Container und Systemcontainer. Ist „Docker“ verboten und „VMs“ ohne Nachfrage, startet und stoppt der Agent Container auf anderen Knoten ohne Nachfrage. | niedrig–mittel (Rechte, gewollt?) | behoben in 1.03: Bereich je Art (Container → Docker, VM und LXC → VMs, wie `lxc_action`); `TestClusterActionFollowsTheAreaOfItsKind`. |
| A-42 | `api/users.go` | Beim Löschen eines Kontos bleiben Chats, Gedächtnis, Skills und sein Ordner in „KI-Agent“ liegen. Ein neues Konto gleichen Namens erbt sie (A-13 löscht nur Einstellungen und Dokumente). | mittel | behoben in 1.03 (Inhaber: „Mitlöschen“): `api/agent_forget.go`; `TestDeletedAccountTakesTheAgentDataAlong`, `TestForgottenAccountIsNotMadeAgain`, `TestClusterDeletedAccountTakesTheAgentDataAlong`, `TestAgentDataNeedsTheRightToManage`. |
| A-43 | `agent_volume.go` | Zwischen der Prüfung „kein Lauf“ und dem Setzen von „zieht um“ liegt ein kurzes Fenster. Ein Lauf, der genau dann startet, scheitert am gesperrten Speicher. | niedrig | behoben in 1.04: der Umzug setzt „zieht um“ vor dem Blick auf laufende Arbeit und nimmt es zurück, wenn etwas läuft. `TestAgentMoveAndRunNeverTogether` (ohne Korrektur: Lauf und Umzug beide gestartet) |
| A-44 | `agent_code.go`, `docker/sandbox.go` | **Code-Sandbox:** Kein Fund. Gelesen wurden die Zeitlimits (Lauf, Bild-Download 10 min, Laden der Bibliotheken 5 min, Logs 15 s, Löschen 30 s), das Aufräumen bei Abbruch, kein Netz, die Rechte und die Nachfrage-Regel. | – | geprüft |
| A-45 | `agent_cluster.go` | **Cluster-Freigabe:** Kein Fund (Pfadliste, Prüfung der Freigabe, Signatur durch den Cluster). | – | geprüft |

## Teil B – Skripte und CI

| Nr. | Ort | Wirkung | Schwere | Status |
|---|---|---|---|---|
| B-1 | `docs/API-LAYOUT.md` | `agent_runs.go` aus 1.01 fehlte. `test_api_layout` schlug fehl, damit waren `build.yml` und der Test-Schritt von `release.yml` rot. | hoch (CI) | behoben `69e6df3e` |
| B-2 | `release-kind.py`, `viewer-image-reuse.py`, `autopilot.py` | `git diff --name-only` erkennt Umbenennungen und nennt nur den neuen Pfad. Folgen: Eine aus dem Abbild nach `docs/` verschobene Datei ergab ein App-Release. Das Fernansicht-Abbild wurde trotz einer aus `viewer/` verschobenen Datei wiederverwendet. Die Prüfung des Autopiloten sah eine in `build/debian-security.txt` umbenannte Build-Datei nur als Änderung der Markierung. | mittel | behoben `ff790a5d` (`--no-renames`; ohne Umbenennung gleiches Ergebnis); `ChangedFilesTest`, `test_a_file_renamed_to_the_marker_is_no_security_update`. Für das nächste Release geprüft: seit v0.82.0 kommt nur `scripts/licenses/…` dazu, das in `ALLOWED` liegt, also bleibt es ein App-Update. |
| B-3 | `viewer-image-reuse.py` `image_built_at` | Ein nie veröffentlichter Tag (0.67.0, 0.68.0) beendete die Suche nach dem ältesten Release eines wiederverwendeten Abbilds. Das Abbild galt als jünger, der wöchentliche Neubau konnte ausfallen. | niedrig–mittel | behoben `ff790a5d`, `test_an_unpublished_tag_does_not_end_the_chain` |
| B-4 | `release.yml` „Plugin sets“ | Die Funktion läuft als `sets … \|\| status=$?`, dort hält bash bei Fehlern nicht an. Ein abgelehnter Dispatch (abgelaufener Schlüssel) oder ein gescheitertes Lesen von Kernel bzw. Paket führte zu fünf Stunden „Waiting…“. | mittel | behoben `e728bcae` (`\|\| return 1`); mit nachgebautem `gh` geprüft, kein Unit-Test möglich |
| B-5 | `build.yml` Job `build`, `plugin-sign.yml` Job `dispatch` | Kein `timeout-minutes`, also 6 h. Ein hängender Dispatch hält alle 15-Minuten-Läufe auf. | niedrig–mittel | behoben `e728bcae` (90 bzw. 20 min) |
| B-6 | `plugin-sign.yml` Dispatch, `pending=$(… \| sed …)` | Ohne `pipefail` wird ein Fehler von `autopilot.py` zu „nichts ausstehend“. | niedrig | behoben `e728bcae` |
| B-7 | `scripts/tests/` | Testserver wurden nicht geschlossen (ResourceWarning), `test_fetch_webapps` ließ Temp-Ordner liegen, der Git-Test des Autopiloten las die globale Git-Einstellung des Entwicklers (signierte Commits). | niedrig | behoben `e12366fc`, `ff790a5d` |
| B-8 | `plugin-sign.yml` (~Z. 283) | Scheitert der Download des Plugin-Katalogs vorübergehend, beginnt `sign-plugin-set.py pointer` bei Seriennummer 0. Er veröffentlicht einen Katalog nur mit dem neuen Satz und kleinerer Nummer. Jedes NAS lehnt ihn ab („went backwards“), bis die Nummer wieder höher ist; andere Kernel-Sätze fehlen. | hoch | behoben in 1.03 (Inhaber: „Ja, abbrechen“): nur 404 bzw. leeres Release beginnt bei 0, sonst Abbruch vor dem Veröffentlichen; `test_plugin_sign_catalog.py`. |
| B-9 | `plugin-sign.yml` Signierschritt (~Z. 213) | Dieselbe Pipe wie B-6 im Signierschritt, schlägt geschlossen fehl. | niedrig | behoben in 1.03: keine Pipe mehr; mit B-8. |
| B-10 | `plugin-common-check.sh` Z. 245 | Bei zwei Entwürfen desselben `common-N` liefert `--jq` zwei IDs, der nächste Aufruf scheitert. Schlägt geschlossen fehl. | niedrig | bewusst gelassen: das Skript liegt außerhalb von `ALLOWED` (eine Änderung machte das Release zu einem vollen Systemrelease) und schlägt geschlossen fehl: Bei zwei Entwürfen wird nichts von selbst signiert, der Satz wartet auf die Freigabe von Hand |
| B-11 | `repack-release.py` Z. 63, `viewer-image-reuse.py` Z. 274; `autopilot.py` `published`, `debian-security.py` | Wiederholt wird nur das Öffnen eines Downloads; ein Abbruch mitten im mehrere GB großen Abbild bricht das App-Release ab (`RELEASING.md` sagt anderes). Autopilot und Debian-Prüfung wiederholen gar nicht. | mittel | behoben in 1.03: ganze Datei neu bei Abbruch/Timeout/5xx in `repack-release.py`, `viewer-image-reuse.py`, `autopilot.py` (`debian-security.py` unverändert); `test_download_retries.py`. |
| B-12 | mehrere Skripte | Doppelte Helfer: SHA-256 einer Datei (7×), minisign-Prüfung (6×, `plugin-guard.trusted` ohne `rehearsal.pub`), Download mit Wiederholung (3 verschiedene). | niedrig (Aufräumen) | bewusst gelassen: Zusammenlegen ist Aufräumen in Signaturpfaden, das Risiko ist größer als der Nutzen. `plugin-guard.trusted` lässt `rehearsal.pub` absichtlich aus (strenger, kein falsches Ergebnis) |
| B-13 | `nvidia-update-release.py`, `debian-security.py`, `repack-release.py` | Toter Code: in `nvidia-update-release.py` ungenutzte Funktionen (außerhalb von `ALLOWED`), in `debian-security.py` Funktionen nur für Tests, in `repack-release.py` der ungenutzte Parameter `version` von `repack()`. | niedrig | bewusst gelassen: Release-Werkzeug wird nur bei Fehlern geändert (Vorgabe des Inhabers für 1.04); toter Code ist keiner. Geprüft: das unbenutzte `version` in `repack()` ist kein vergessenes Schreiben (die Version des Slots schreibt `repack_rest`). `nvidia-update-release.py` liegt außerdem außerhalb von `ALLOWED` |
| B-14 | `fetch-pci-ids.sh`, `tailscale_smoke.py`, `netbird_smoke.py` | `curl` bzw. `scp` ohne Zeitlimit. | niedrig | bewusst gelassen: alle drei liegen außerhalb von `ALLOWED` (`fetch-pci-ids.sh` gehört zum Bau des Abbilds, die beiden `*_smoke.py` passen nicht auf das Präfix `scripts/smoke-`). Ein Hängen begrenzt das `timeout-minutes` der Jobs in `release.yml` |
| B-15 | `release.yml` | `${{ steps.*.outputs.* }}` direkt in `run:`. Die Werte sind vorher per Regex geprüft, also nicht ausnutzbar. | niedrig | bewusst gelassen: Release-Werkzeug wird nur bei Fehlern geändert; die Werte sind vorher per Regex geprüft, also nicht ausnutzbar |

## Zählung

Fundstufen „niedrig–mittel“ zählen bei A-41 als mittel, bei B-3 und B-5 als niedrig.

| Schwere | behoben | notiert | Summe |
|---|---|---|---|
| hoch | 5 (A-1, A-2, A-13, A-14, B-1) | 1 (B-8) | 6 |
| mittel | 12 (A-3, A-4, A-15, A-16, A-17, A-18, A-25, A-26, A-28, A-37, B-2, B-4) | 4 (A-30, A-41, A-42, B-11) | 16 |
| niedrig | 15 | 21 | 36 |
| **Summe** | **32** | **26** | **58** |

Dazu zwei Bereiche ohne Fund (A-44 Code-Sandbox, A-45 Cluster-Freigabe).

Die Zählung gibt den Stand von 1.02 wieder. In 1.03 behoben (Entscheidungen des Inhabers vom 16.11.2026, Zweig `feat/v1.03-b`): A-7, A-30, A-33, A-41, A-42, B-8, B-9, B-11; bei A-12 und A-22 ist der Teil im NAS erledigt, die Oberfläche offen.

**Stand 1.04** (Zweig `feat/v1.04-b`): Kein Fund ist mehr nur „notiert“.

| Ergebnis | Anzahl | Funde |
|---|---|---|
| behoben in 1.02 | 32 | siehe Tabelle oben |
| behoben in 1.03 | 8 | A-7, A-30, A-33, A-41, A-42, B-8, B-9, B-11 |
| behoben in 1.04 | 7 | A-8, A-9, A-10, A-24 (zwei von drei Teilen), A-35, A-36, A-43 |
| bewusst gelassen | 7 | A-11, A-34, B-10, B-12, B-13, B-14, B-15 (dazu der dritte Teil von A-24) |
| Entscheidung des Inhabers | 1 | A-23 |
| im NAS erledigt, Oberfläche bei wt-104c | 3 | A-12, A-22, A-32 (dazu `stepsPreview` aus A-36) |
| **Summe** | **58** | |

## Angefasste gemeinsame Dateien

In 1.04:

- `backend/internal/api/shares.go`: Haken `guardHeld` vor dem Speichern einer Freigabe (A-9). Gehört zum Bereich Freigaben.
- `web/src/lib/i18n/de.json`, `en.json`: nur der Schlüssel `error.share_guard_locked` (A-9).

Bis 1.02:

- `backend/internal/api/users.go`: ein Haken `agentForgetUser` im Löschen eines Kontos (A-13). Gehört zur Durchsicht „Konten“ (wt-r3).
- `backend/internal/agent/store.go`, `agent/seen.go`: `UpdateNotesMeta`, `RemovePrivate`, `SaveNotesMeta` unter der Sperre.
- `docs/AGENT.md`: Abschnitt Speicherort, Umzug mit `.titanos/docs`.
- `docs/API-LAYOUT.md`: neu erzeugt. Beim Zusammenführen neu erzeugen statt von Hand mischen.
- `.github/workflows/build.yml`, `release.yml`, `plugin-sign.yml` (B-4 bis B-6).

## Nicht geprüft

- Nichts am echten NAS, mit echtem Docker, QEMU oder echtem Modell. Sandbox, Wächter (fanotify, smbcontrol, Btrfs) und IMAP sind in den Tests nachgebaut.
- `shellcheck` und `pyflakes` sind hier nicht installiert. Die Shell in den `run:`-Blöcken wurde nur von actionlint gelesen, ohne shellcheck.
- Die UI-Korrekturen A-20 und A-40 (Download) haben keinen eigenen Test. Geprüft sind sie mit svelte-check und den Browser-Suites.
- Schreibrechte des SMB-Clients während eines Umzugs, mehrere Administratoren gleichzeitig (A-30) und die Cluster-Werkzeuge gegen einen echten zweiten Knoten.
