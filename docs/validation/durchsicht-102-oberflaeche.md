# Durchsicht 1.01 – Weboberfläche ohne KI-Agent (16.11.2026)

Auftrag des Inhabers vom 16.11.2026: die ganze Codebasis prüfen, aufräumen, verbessern und Stabilität hineinbringen. Diese Liste deckt die Weboberfläche außerhalb des KI-Agenten ab: `web/src/**` ohne `routes/agent/`, `lib/components/agent/`, `lib/agent*` und `routes/settings/agent/`, dazu Service Worker und Manifest. Die anderen Bereiche (Backend-Speicher, Container/VMs im Backend, Konten/Netz/Updates, KI-Agent/Skripte) haben eigene Listen.

Zweig `review/r4` auf dem Stand 1.01. Funde der Durchsicht 0.75 (`durchsicht-076.md`, Abschnitt 12) sind nicht wiederholt.

**Vorgehen:** Der Code wurde in vier Teilen gelesen:
- Arbeitsplatz, Fenster und Handy;
- Dateien, Speicher, Freigaben und Konten;
- Docker, VMs, LXC, NVR, Plugins, Web-Apps und Werkzeuge;
- Systemsteuerung, Anmeldung, Ersteinrichtung, `lib/*.ts` und Service Worker.

Hinzu kamen Suchen über alle Dateien: Intervalle und Listener ohne Aufräumen, still verschluckte Fehler, Kürzungen mit „…“ und `line-clamp`, ungenutzte Exporte, Schlüssel nur in einer Sprache.

Jeder Fund wurde am Code bestätigt. Wo es ging, wurde er zusätzlich mit einem Test belegt, der ohne die Korrektur fehlschlägt (in der Spalte „Stand“ genannt).

**Schwere:**
- **hoch:** falsche Daten oder Rechte, Sicherheit, Arbeit geht verloren.
- **mittel:** sichtbarer Fehler oder hängender Zustand.
- **niedrig:** Regelverstoß ohne Folgeschaden, Kleinigkeit.

**Stand:** behoben (Commit-Thema) · notiert (nicht gebaut, Grund) · beim Inhaber (braucht eine Entscheidung) · bewusst gelassen.

## Behoben

| Nr. | Schwere | Ort | Wirkung | Stand |
|---|---|---|---|---|
| D102-1 | hoch | `lib/lock.svelte.ts` `lock`, Abfrage im gesperrten Zustand | Scheiterte die Sperranfrage (Netz weg, titand startet nach einem App-Update neu) oder war sie noch unterwegs, las die Abfrage „nicht gesperrt“ vom NAS. Der Sperrbildschirm verschwand dann **ohne Passwort**. | behoben: Eine noch nicht bestätigte Sperre wird erneut gesendet und entsperrt nie. Test `tests/lib/lock.test.ts` („a lock the NAS has not taken never unlocks …“). |
| D102-2 | hoch | `routes/terminal/+page.svelte` | `/terminal?open=…` (eine Konsole, die ein anderes Fenster anfordert) wurde bei jeder Änderung der Sitzungen erneut ausgeführt. Eine beendete Konsole öffnete sich sofort wieder, ein neuer Reiter verlor den Fokus, mit `nas` hätte es sich endlos geöffnet. | behoben (`untrack`). UI-Prüfung in `scripts/test-ui-windows.mjs`; rot ohne Korrektur. |
| D102-3 | hoch | `lib/components/FanControl.svelte` | Nach dem ersten Speichern sprang jede weitere Änderung an Lüfterwerten nach spätestens 5 s auf den alten Wert zurück. Eine Abfrage von vor dem Speichern konnte die alten Einstellungen zurückbringen, und ein einmaliger Abfragefehler blieb stehen. | behoben: `lib/fan-drafts.ts` (geänderte Lüfter behalten ihren Entwurf), Runden gegen alte Antworten. Test `tests/fan-drafts.test.mjs`. |
| D102-4 | hoch | `routes/files/CopyDialog.svelte`, `lib/components/FolderPicker.svelte` | Eine langsame Ordnerliste kam an, nachdem schon ein anderer Ordner gewählt war. Ihre Unterordner standen dann unter dem falschen Pfad. Ein Klick darauf ergab ein falsches Ziel: Kopieren/Verschieben landete im falschen Ordner, Sicherungs- und App-Ordner bekamen einen falschen Pfad. Auch der Schutz „Ordner nicht in sich selbst“ konnte entfallen. | behoben: Nur die Antwort für den angezeigten Ordner zählt. Test `tests/components/folder-picker.test.ts` (rot ohne Korrektur). Der Kopieren-Dialog hat denselben Schutz, aber keinen eigenen Test. |
| D102-5 | hoch | `routes/users/AccountDetail.svelte` | Änderte man ein App- oder Kamerarecht und wählte vor der Antwort ein anderes Konto, erschienen die Rechte des ersten Kontos beim zweiten. Die nächste Kamera-Änderung speicherte sie dann für das zweite Konto. | behoben: Die Antwort gilt nur für ihr Konto. Test `tests/components/account-rights.test.ts` (rot ohne Korrektur). |
| D102-6 | mittel | `routes/nvr/events/+page.svelte` `keep` | Schloss man das Ereignis, während „Behalten“ lief, öffnete die Antwort einen leeren Dialog („undefined“, „Invalid Date“, „NaN %“) und fragte einen kaputten Stream an. | behoben. UI-Prüfung in `scripts/test-ui-nvr.mjs`; rot ohne Korrektur. |
| D102-7 | mittel | `routes/nvr/recordings/+page.svelte` | Ein Aufnahmeabschnitt, der nach einem neueren Klick, Tageswechsel oder Verlassen der Seite fertig lud, nahm den Player in Beschlag (falsches Video, falsches „Wiedergabe fehlgeschlagen“). hls.js lud danach weiter. Betroffen sind Browser mit hls.js, z. B. Firefox. | behoben (Ladezähler). Nur gelesen und `test-ui-nvr` ohne Fehler; das Test-Chromium spielt kein HLS. |
| D102-8 | mittel | `lib/nvr/LiveTile.svelte` (Gegensprechen) | Ein zweiter Druck während des Verbindungsaufbaus öffnete eine zweite Verbindung, und die erste behielt das Mikrofon. Eine Kachel, die während des Aufbaus verschwand, übernahm die Verbindung danach und konnte senden, ohne dass sie sich beenden ließ. | behoben. Nicht mit einer echten Kamera geprüft. |
| D102-9 | mittel | `lib/webapps/BrowserApps.svelte` `replaceBy` | „Durch Nachfolger ersetzen“ löschte das Original nach 15 Minuten auch dann, wenn der Nachfolger noch nicht fertig geladen war. Der Vorgang lief auch nach dem Schließen weiter. | behoben: Gelöscht wird nur nach „bereit“, sonst kommt eine Meldung (`webapps.successorNotReady`). Ohne eigenen Test. |
| D102-10 | mittel | `routes/vms/media/+page.svelte` | Pausieren, Abbrechen oder Verlassen während der Prüfung eines fortgesetzten Hochladens schickte danach trotzdem „fortsetzen“. Das NAS wartete dann auf Daten, die niemand schickte. | behoben. Ohne eigenen Test; `test-ui-vms` ohne Fehler. |
| D102-11 | mittel | `lib/components/Dialog.svelte` | Fast alle Dialoge schließen, indem die Seite sie entfernt. Der Fokus fiel dann auf nichts, und wer mit der Tastatur arbeitet, begann wieder oben auf der Seite. | behoben: Der Fokus geht an das auslösende Element zurück, sofern ihn nichts anderes übernommen hat. Test `tests/components/dialog-focus.test.ts` (rot ohne Korrektur). |
| D102-12 | mittel | `routes/share/+page.svelte`, `static/sw.js` | Teilen-Ziel: Ein zweiter Tipp, während die Dateien gelesen wurden, lud alles doppelt hoch. Fehlte eine Datei im Speicher, verrutschte die Fortschrittsanzeige aller folgenden Zeilen. Eine Teilung, die nicht ganz gespeichert werden konnte (Kontingent), ließ Reste liegen. Namen wie „datei-1“ und „geteilt-….txt“ waren fest deutsch. | behoben: Sperre, Zuordnung je Datei, „nicht mehr im Browser“ je Zeile, Aufräumen im Worker, Namen aus den Übersetzungen. Test `tests/sw.test.mjs` (zwei neue Fälle, beide rot mit dem alten Worker). |
| D102-13 | mittel | `lib/i18n/index.svelte.ts` | Scheiterte das Laden der Texte (Netz, nach einem App-Update fehlende Dateien), blieb die Seite für immer leer, ohne Hinweis. Eine langsam ladende frühere Sprachwahl schaltete nach einer neueren zurück. | behoben: Nach einem Fehlschlag wird neu versucht, beim dritten Fehlschlag lädt die Seite einmal neu; die jüngste Wahl gewinnt. Ohne eigenen Test (Ladefehler eines Moduls lässt sich in vitest schlecht nachstellen). |
| D102-14 | mittel | `routes/settings/appearance/+page.svelte` | Änderungen wurden 700 ms verzögert gespeichert, und Verlassen der Seite brach das Speichern ab. Der Bildschirm zeigte die Änderung, das NAS bekam sie nie; der nächste Start brachte das alte Aussehen. | behoben: Ausstehendes wird beim Verlassen und bei `pagehide` mit `keepalive` gesendet. Ohne eigenen Test. |
| D102-15 | mittel | `lib/components/PushDevices.svelte`, `routes/settings/wireguard/+page.svelte` | „seit 14:32“ bei einem Gerät von vor Wochen, „zuletzt 09:10“ bei einem Handshake von vor Tagen: Es stand nur die Uhrzeit da. | behoben (Datum bzw. Datum und Uhrzeit). |
| D102-16 | mittel | `routes/settings/speech/+page.svelte` | Bei einer Antwort ohne `status` stürzte die Seite ab (in `test-ui-settings-home` als Seitenfehler sichtbar). Das eigene Intervall lief auch im verborgenen Tab und konnte sich überholen. Ein einziger Abfragefehler blieb bis zum Verlassen als Fehler stehen. Die Geschwindigkeit hatte im Englischen ein Komma. | behoben: Antwort wird geprüft, gemeinsames `poll`, Abfragefehler getrennt vom Speicherfehler, Zahl im Format der Sprache. `test-ui-settings-home` jetzt ohne Seitenfehler. |
| D102-17 | mittel | `routes/settings/access/+page.svelte` (SSH), `routes/settings/files/+page.svelte` (Dateisuche) | Eine Abfrage, die vor dem Speichern losgeschickt wurde, brachte den alten Zustand zurück: Der SSH-Schalter stand wieder auf dem alten Wert und das Nachfragen hörte auf, das Formular der Dateisuche zeigte die alten Einstellungen. | behoben (Runden). Ohne eigenen Test. |
| D102-18 | mittel | `routes/files/JobsPanel.svelte` `cancel` | Ein abgelehntes Abbrechen eines Kopierauftrags blieb ohne Meldung. | behoben (Meldung wie bei den anderen Aktionen). |
| D102-19 | mittel | `routes/files/NavBar.svelte` (Fenster), `routes/files/MobileLocation.svelte` (Handy) | Im schmalen Fenster verbarg die Adresszeile bei tiefem Pfad den aktuellen Ordner und den Knopf „Pfad bearbeiten“ (`overflow: hidden`, kein Umbruch). Am Handy lief der Pfad aus dem 44-px-Knopf hinaus. | behoben: Pfad bricht um, der Knopf wächst mit. Im Browser nicht mit einem tiefen Pfad geprüft (die Testdaten haben nur flache Pfade). |
| D102-20 | mittel | `lib/components/Shell.svelte` | Konnte der Arbeitsplatz oder der Handy-Startbildschirm nicht geladen werden (alte Dateinamen nach einem App-Update), blieb der Bildschirm leer, ohne ein Wort. | behoben: Meldung mit „Erneut versuchen“. |
| D102-21 | mittel | `lib/components/Shell.svelte` `settle` | Netzwerk auf Probe: Ein abgelehntes „Behalten“ blieb ohne Meldung. Die Änderung ging nach Ablauf der Zeit zurück, und niemand wusste warum. | behoben (Meldung im Probe-Banner). |
| D102-22 | niedrig | `routes/lxc/+page.svelte` | Eine späte Antwort zeigte die Konfiguration eines anderen Containers. Beim Wechsel zwischen zwei Btrfs-Containern standen kurz die Snapshot-Zeilen des ersten da, mit Knöpfen, die auf den zweiten wirkten. | behoben. |
| D102-23 | niedrig | `routes/settings/systemdata/+page.svelte` | Nach dem Verlassen fragte die Seite bis zu zwei Minuten weiter (bis zu 40 Abfragen). | behoben. |
| D102-24 | niedrig | `routes/shares/+page.svelte` `saveView` | Eine Sortierung, die während des Speicherns der vorigen gewählt wurde, war sichtbar, wurde aber nie gespeichert. | behoben (wird danach gespeichert). |
| D102-25 | niedrig | `lib/taskmanager.ts` `shortCommand`, `routes/account/+page.svelte`, `lib/pages/Restore.svelte`, `routes/settings/cluster/+page.svelte`, `routes/files/files-page.svelte.ts` | Abgeschnittener Text entgegen PLAN 76: Befehlszeilen im Task-Manager mit „…“ (die Zeile scrollt ohnehin), nicht erkannte Browsernamen nach 60 Zeichen, nur zehn gefundene Sicherungen bei der Ersteinrichtung, nur drei bzw. vier Adressen eines Knotens, nur fünf Namen nicht lesbarer gezogener Dateien und dann „…“. | behoben: Alles steht ganz da. `shortCommand` und sein Test entfallen. |
| D102-26 | niedrig | `lib/desktop/WidgetView.svelte` `.ag-last` | Die Vorschau des letzten Chats im Agent-Widget nutzt `line-clamp` ohne die Kennzeichnung der Ausnahme. | behoben: `data-cut="excerpt"`. Der ganze Chat öffnet sich mit einem Klick, wie bei der Notiz-Vorschau. |
| D102-27 | niedrig | `lib/components/Segments.svelte`, `lib/desktop/PhoneNotices.svelte` | Tippflächen am Handy unter 44 px: kompakte Segmente 26 px (NVR-Ereignisse und -Aufnahmen), normale 38 px, der Link im Kopf der Mitteilungen 40 px. | behoben (44 px bei schmaler Breite und Touch). |
| D102-28 | niedrig | `lib/components/Notifications.svelte`, `routes/storage/smart/+page.svelte` | Status nur über Farbe: Das Glocken-Abzeichen zeigte Warnung bzw. Fehler nur farbig; ein auffälliger SMART-Wert war in der Attributtabelle nur rot. | behoben: „!“ und Text im Abzeichen (`notify.unreadWarning`, `notify.unreadError`), „Auffällig“ in der Zeile (`smart.attrWatch`). |
| D102-29 | niedrig | `routes/vms/VmActionDialog.svelte`, `routes/settings/backups/TargetPanel.svelte`, `routes/settings/backups/ReplicationPanel.svelte`, `lib/components/PowerSchedule.svelte` | Barrierefreiheit: Textfeld „Text einfügen“ und Kopplungscode ohne Namen, Schalter der Replikation ohne `role="switch"`, Wochentage im Energie-Zeitplan nur als „Mo“/„Di“ ohne vollen Namen. | behoben. |
| D102-30 | niedrig | `routes/settings/temperatures/*`, `routes/settings/updates/+page.svelte` | Temperaturen und Update-Geschwindigkeit mit Dezimalpunkt im Deutschen, MB/s von Hand gerechnet. | behoben (gemeinsame Formatierer). |
| D102-31 | niedrig (Aufräumen) | `lib/metric-history.ts`, `lib/nvr/timeline.ts` `recordedAt`, `lib/webapps/recent.ts` `forgetDoc`, `lib/desktop/desk-ops.ts` `addIcon` | Ohne Aufrufer (`metric-history.ts` stammt vom 0.62 entfernten Dashboard). | entfernt. |

## Notiert, nicht gebaut

Stand 17.11.2026: Bis auf D102-43, D102-44 und D102-52 in 1.03 behoben (Zweig `feat/v1.03-c`, `docs/release-parts/v1.3.0-c.md`). In 1.04 (Zweig `feat/v1.04-c`, `docs/release-parts/v1.4.0-c.md`) sind auch D102-43, D102-44 und die Reste von D102-42 und D102-49 behoben; D102-52 und das ✕ in der Terminal-Reiterleiste bleiben bewusst. Offen ist nichts mehr.

| Nr. | Schwere | Ort | Wirkung | Stand |
|---|---|---|---|---|
| D102-40 | mittel | `lib/desktop/desk-ops.ts` `dockerAction`, `desktop/window-commands.ts` `restartDockerApp` | Starten, Stoppen und Neustarten einer Docker-App aus dem Kontextmenü des Arbeitsplatzes bzw. der Menüleiste verschluckt jeden Fehler. Der Punkt am Symbol ändert sich erst mit der nächsten Abfrage (20 s). | behoben in 1.03: kurzer Hinweis unten mit Grund (Entscheidung 16.11.2026), auch für „Neu starten“ der Menüleiste. Tests `tests/lib/desk-notices.test.ts`, `test-ui-desktop`. |
| D102-41 | mittel | `lib/desktop/desk-messages.ts` `sendFilesToFileManager`, `routes/files/files-page.svelte.ts` | Auf den Arbeitsplatz gezogene Dateien: Nimmt der Dateimanager sie in 14 s nicht an, sind sie ohne Meldung weg. Zwei schnelle Ablagen: Die zweite wird bestätigt, aber verworfen. | behoben in 1.03: Hinweis nach 14 s ohne Annahme; jede Ablage trägt einen Namen, eine zweite wartende kommt dazu. Test `tests/lib/desk-notices.test.ts`. |
| D102-42 | mittel/niedrig | `lib/desktop/wm.svelte.ts` (Speichern der Arbeitsplatz-Einstellungen) | Ein gescheitertes Speichern (Hintergrund, Dock, Symbole, Sperrminuten) bleibt still. Der Kommentar „in diesem Browser behalten“ stimmt nicht, der Wert liegt nur im Speicher. Andere Tabs bekommen den Wert, bevor er gespeichert ist. Ein Neuladen innerhalb von 1,5 s verliert die Änderung. | behoben in 1.03: Hinweis beim gescheiterten Speichern, Kommentar berichtigt. **Rest behoben in 1.04:** Eine noch wartende Änderung geht beim Verlassen (`pagehide`) mit `keepalive` hinaus; andere Tabs bekommen den Wert erst, wenn das NAS ihn gespeichert hat. Test `tests/lib/desk-notices.test.ts` (die zwei neuen Fälle rot ohne Korrektur). |
| D102-43 | niedrig | `routes/nvr/+layout.svelte` Datenschutzhinweis | Scheitert „Ausblenden“, bleibt der Hinweis ohne Begründung stehen. Laut Kommentar zeigt die Einstellungsseite den Grund. | behoben in 1.04: Die Meldung steht im Hinweis selbst, unter „Verstanden“ (`nvr.privacy.failed`, mit Grund). Der kurze Hinweis unten kam nicht in Frage: Er gilt nur für Stellen ohne eigenen Platz und erscheint nicht in Fenstern, und das NVR läuft im Fenster. Prüfung in `test-ui-nvr` (rot ohne Korrektur). |
| D102-44 | niedrig | `routes/settings/updates/+page.svelte` | Scheitert `/storage`, fehlt vor einem Rückweg auf eine Version vor 0.31 die Warnung, dass verschlüsselte Volumes nicht eingehängt werden. | behoben in 1.04: Lässt sich `/storage` nicht lesen, sagt der Dialog vor einem Rückweg auf eine Version vor 0.31, dass er es nicht prüfen konnte, und warnt trotzdem (`update.rollbackEncryptedUnknown`). Prüfung in `test-ui-updates` (rot ohne Korrektur). |
| D102-45 | niedrig | `lib/components/HealthReport.svelte` | „Jetzt senden“ verwirft ungespeicherte Änderungen am Zeitplan des Berichts. | behoben in 1.03. Test `tests/components/health-report-send.test.ts` (rot ohne Korrektur). |
| D102-46 | niedrig | `routes/settings/ups/+page.svelte` | Eine Abfrage von vor dem Speichern kann kurz den alten Stand (Passwort gesetzt, Status) zeigen; die nächste Abfrage berichtigt ihn. | behoben in 1.03 (Runden; ebenso auf der Updates-Seite für Prüfen, Installieren und Speichern). Prüfung in `test-ui-updates` (rot mit der Seite von 1.02); die USV-Seite ohne eigenen Test. |
| D102-47 | niedrig | `routes/files/TextEditor.svelte:50` | Verwerfen beim Wegnavigieren fragt mit `window.confirm` statt mit `Dialog.svelte`; das ist die einzige Stelle in der Oberfläche. | behoben in 1.03: Navigation hält an, Frage im Editor, „Änderungen verwerfen“ geht weiter. Test `tests/components/text-editor-leave.test.ts`. |
| D102-48 | niedrig | `lib/components/TerminalSettings.svelte`, `routes/settings/+page.svelte`, `routes/settings/ddns/DdnsStatus.svelte` | Kleine Zeitgeber: Der Hinweis „gespeichert“ verschwindet nach einem zweiten Speichern zu früh, die Suchschleife der Übersicht läuft nach dem Verlassen bis zu 5 s weiter, ein Doppelklick auf „Jetzt aktualisieren“ (DDNS) sendet zweimal. | behoben in 1.03. Test `tests/components/ddns-update.test.ts`. |
| D102-49 | niedrig | `lib/components/Dropdown.svelte`, `TerminalPanel.svelte`, `Tabs.svelte`, `Segments.svelte` | Barrierefreiheit: Die Wahl eines Menüeintrags lässt den Fokus fallen. In der Terminal-Reiterleiste (`role="tablist"`) stehen auch Knöpfe und das Menü. `Tabs`/`Segments` haben die Rollen `tab`/`radio`, aber keine Pfeiltasten-Bedienung mit einem Tab-Halt. | behoben in 1.03: `Tabs`, `Segments` und die Reiter des Terminals mit einem Tab-Halt, Pfeiltasten, Pos1/Ende; `Dropdown` gibt den Fokus an seinen Knopf zurück, öffnet mit Pfeiltasten, Tab verlässt es. Test `tests/components/keyboard.test.ts` (rot ohne Korrektur). **Rest in 1.04:** „+“ und sein Menü stehen neben der `tablist`, nicht mehr darin; Entf auf einem Reiter beendet die Sitzung (Muster „Tabs“ der WAI-ARIA); danach bleibt der Fokus auf dem nächsten Reiter bzw. auf „+“ (auch nach ✕ und nach der Nachfrage bei einer root-Shell). Prüfung in `test-ui-windows` (rot ohne Korrektur). Bewusst gelassen: Das ✕ bleibt im Reiter, also in der `tablist`. Herausgenommen wäre es für Bildschirmleser am Handy und für Tastaturnutzer, die Entf nicht kennen, nicht mehr erreichbar; ein Reiter mit eigenem Schließen-Knopf ist das übliche Bild. |
| D102-50 | niedrig | `static/manifest.webmanifest` | Beschreibung, `lang` und die Namen der Kurzbefehle („Dateien“, „Systemsteuerung“) stehen nur auf Deutsch. Englische Nutzer sehen deutsche Kurzbefehle. | behoben in 1.03: Manifest je Sprache (`manifest.en.webmanifest`), gleiche App. Tests `tests/manifest.test.mjs`, `test-ui-pwa`. |
| D102-51 | niedrig | `lib/components/TerminalPanel.svelte:70` | Wird die letzte NAS-Shell beendet, öffnet sich sofort eine neue root-Shell; das Panel ist nie leer. | behoben in 1.03: keine neue Shell von selbst, Knopf „Neue Shell“ (Entscheidung 16.11.2026). Prüfung in `test-ui-windows`. |
| D102-53 | niedrig (Tests) | `scripts/test-ui-phone.mjs:337`, `scripts/test-ui-workflows.mjs:188` | Zwei UI-Prüfungen sind veraltet und scheitern auch auf dem Ausgangsstand `fc7813b6` (gegengeprüft mit eigenem Bau): `phone` erwartet am Handy mindestens 20 Zeilen unter „Alle Einstellungen“ und Übersichtskarten (seit 0.87 eine Zeile je Bereich, keine Karten); `workflows` sucht in der Befehlspalette den Eintrag „Zugang › SSH“. | behoben in 1.03: `test-ui-phone` prüft die Ebenen der Systemsteuerung; `test-ui-workflows` war schon mit 1.02 angepasst. |
| D102-52 | – | `routes/files/partitions.ts` `partitionJob`, `lib/users-view.ts` `sanitizeStatusFilter`, `lib/firewall.ts` `appPorts`, `lib/settings-search.ts` `settingsSideAllowed` | Werden nur noch von Tests benutzt. Rund 20 weitere Exporte werden nur in ihrer eigenen Datei benutzt. | bewusst gelassen (in 1.04 erneut geprüft, weiter nur von Tests benutzt): kein Fehler, kleine geprüfte Hilfsfunktionen; ein Entfernen bringt nichts und erzeugt nur Konflikte mit den parallel laufenden Zweigen von 1.04. |
| D102-54 | mittel (neu in 1.04) | `lib/desktop/Window.svelte` (Nachricht `titanos-page`), `wm.svelte.ts` `navigateWindow` | Gefunden beim Lauf aller UI-Skripte (`test-ui-scripts` rot unter Last): Ein Klick in der Seitenleiste eines Fensters, solange dessen erste Seite noch lud, ging verloren, wenn die erste Seite sich innerhalb von 2,5 s nach dem Klick meldete. Ihre Meldung galt als „spät“ und wurde verworfen, bevor die wartende Seitenwahl abgeschickt war. Die Leiste zeigte die gewählte Seite, das Fenster die erste. | behoben in 1.04: Die wartende Wahl geht bei jeder ersten Meldung hinaus, auch bei einer späten. Prüfung in `test-ui-windows` (erste Seite 1,5 s verzögert; mit dem alten Fenster 5 von 5 Läufen falsch). |

## Oberflächenteile aus anderen Fundlisten (1.04)

Die Backend-Teile hat der Zweig `feat/v1.04-b` erledigt; hier steht nur, was an der Oberfläche gebaut ist. Die Funde selbst stehen in `durchsicht-102-agent-skripte.md` bzw. der Liste des Speicher-/Container-Teils.

| Nr. | Ort | Oberfläche | Stand |
|---|---|---|---|
| A-12 | `agent/features/ProtectGroup.svelte` | Die Nachfragen bei „Ablehnen“ und „Zugang beenden“ gibt es seit 1.03. Neu: Die Zähler der Zeilen (offene Meldungen, offene Anträge) werden beim Schließen einer Ansicht neu gelesen, nicht erst beim nächsten Öffnen der Liste. | behoben in 1.04. Prüfung in `test-ui-agent54-protect`. |
| A-22 | `agent/features/FeaturesPanel.svelte` | Zwei schnelle Änderungen hintereinander: Die zweite entstand auf dem Stand vor der ersten und kam als 409 „anderswo geändert“ zurück (dieselbe Funktion: verloren) bzw. überschrieb bei den eigenen Einstellungen still die erste. Jetzt zeigt die Seite eine Änderung sofort, Speichern wartet auf das vorige und schickt den neuesten Stand mit dem `rev`, den das NAS zuletzt gab. | behoben in 1.04. Prüfung in `test-ui-agent54-docs` (zwei Schalter derselben Funktion schnell hintereinander). |
| A-32 | `agent/features/ForesightGroup.svelte` | Ein Speicherfehler steht jetzt im offenen Blatt selbst (die Fehlerzeile der Liste lag dahinter); Schließen vergisst ihn. | behoben in 1.04. Test `tests/components/foresight-save-error.test.ts` (rot ohne Korrektur). |
| A-36 (Rest) | `lib/features/family.ts` | `stepsPreview` wurde nur vom Test benutzt. | entfernt, samt Test. |
| – | `routes/shares/ShareDialog.svelte` | Neuer Fehler `share_guard_locked` (409 beim Speichern einer Freigabe, die der Ransomware-Wächter gesperrt hat, wenn das Speichern wieder Schreibrecht gäbe): Der Dialog springt auf den Reiter „Zugriff“, nennt den Grund (`error.share_guard_locked`, gleicher Text wie in `feat/v1.04-b`) und sagt, wo die Sperre aufgehoben wird (`shares.guardLockedHint`). | gebaut in 1.04. Prüfung in `test-ui-share-quotas`. |

## Geprüft und in Ordnung

- **Aufräumen:** Alle gefundenen Abfragen, Zeitgeber, Listener, WebSockets und Beobachter in den Bereichen werden aufgeräumt (gemeinsames `poll`, Rückgabe aus `$effect`/`onMount`, `AbortController`). Ausnahmen sind die oben genannten. Das gilt ausdrücklich auch für `VncScreen`, `mse.ts`, `feed.svelte.ts`, `poll.ts`, `TerminalScreen`, `PhoneGestures`, `Desktop`, `PhoneHome` und `WidgetView`.
- **Knöpfe:** Kein Knopf, der nur ein Symbol zeigt, ist ohne Namen; geprüft per Skript über alle `.svelte`-Dateien.
- **Bestätigungen:** Kein Bestätigungsdialog verlangt, einen Namen einzutippen.
- **Übersetzungen:**
  - Deutsch und Englisch haben in allen 19 Dateipaaren dieselben Schlüssel (12 054 je Sprache vor dieser Durchsicht).
  - Platzhalter stimmen überein, kein Schlüssel ist doppelt.
  - Jeder im Code fest genannte Schlüssel existiert.
- **Service Worker:** Er speichert nichts von Oberfläche oder API (`titanos-share` nur für geteilte Dateien).

## Prüfstand

- `npm test` auf dem letzten Stand: Node-Tests 356 und vitest 203, alle grün. Neu sind die Tests für Sperre, Lüfter, Ordnerwahl, Kontenrechte, Dialogfokus und Service Worker. Ohne die Korrektur scheitern die für Ordnerwahl, Kontenrechte, Dialogfokus und Service Worker; Sperre und Lüfter prüfen neue Funktionen.
- `npm run check`: svelte-check 0 Fehler, Prettier sauber.
- **UI-Skripte** gegen die gebaute Vorschau mit isolierten Testdaten, alle grün:
  - Fenster und Systemsteuerung: `test-ui-windows`, `test-ui-settings-home` (jetzt ohne Seitenfehler), `test-ui-settings-nav`, `test-ui-systemdata`, `test-ui-updates`;
  - NVR und VMs: `test-ui-nvr`, `test-ui-nvr-phone`, `test-ui-vms`;
  - Arbeitsplatz: `test-ui-desktop`, `test-ui-lock`;
  - Dateien und Speicher: `test-ui-large-copy`, `test-ui-drive-transfers`, `test-ui-storage-analysis`, `test-ui-share-quotas`, `test-ui-quicklook`.
- **Angepasste Erwartungen:** `test-ui-updates` erwartete „18.0 MB/s“ mit Dezimalpunkt; mit D102-30 lautet die Anzeige im Deutschen „18 MB/s“.
- **Neue Prüfungen:** `test-ui-nvr` (Ereignis bleibt zu) und `test-ui-windows` (angeforderte Konsole bleibt zu); beide scheitern auf dem alten Bau.
- **Rot, aber nicht durch diese Durchsicht:** `test-ui-phone` und `test-ui-workflows` scheitern auch auf dem Ausgangsstand (D102-53).

**Nicht geprüft:**
- echte Kameras, echtes HLS und Mikrofon (das Test-Chromium hat kein H.264);
- echte Handys und Safari;
- echte Web-App-Downloads;
- Verhalten bei echtem Netzausfall;
- die übrigen UI-Suiten, darunter `test-ui-crawl` und `test-ui-windows` mit `WINDOWS_ALL`/`WINDOWS_LONG` (für 1.04 siehe unten: alle Suiten ohne echtes NAS liefen).

## Prüfstand 1.04 (Zweig `feat/v1.04-c`, 17.11.2026)

- `npm test`: Node-Tests 364, vitest 239, alle grün. `npm run check`: svelte-check 0 Fehler, Prettier sauber.
- **Alle UI-Skripte** gegen die gebaute Vorschau, je drei gleichzeitig: 84 von 84 grün auf dem Endstand.
- **Rot beim ersten Lauf (auf dem Stand 1.03):**
  - Ein echter Fehler: `test-ui-scripts` scheiterte unter Last an D102-54, der verlorenen Seitenwahl im Fenster. Allein lief es grün; mit 1,5 s Verzögerung der ersten Seite war es auf dem alten Bau 5 von 5 Mal falsch.
  - Vier Skripte mit veralteter Erwartung, umgeschrieben auf das heutige Verhalten mit demselben Zweck:
    - `test-ui-firewall`: Die Firewall steht laut `docs/rules/WEB-UI.md` unter „Sicherheit und Konten“, die Seiten eines Bereichs in der Reihenfolge des Entwurfs, nicht A–Z.
    - `test-ui-phone-gestures`: Zurück-Wischen über die Bereichsebene von „Alle Einstellungen“ (seit 0.87).
    - `test-ui-login-appearance`: der Knopf „Anmelden“ genau, neben „Mit Passkey anmelden“.
    - `test-ui-060`: Die Suche wartet auf den nachgeladenen Einstellungsindex; die Systemsteuerung am Handy zeigt die Bereiche statt `.overview .sgroup`.
- **Neue Prüfungen, rot auf dem alten Bau:** `test-ui-nvr` (D102-43), `test-ui-updates` (D102-44), `test-ui-windows` (D102-49 Reiterleiste und Fokus, D102-54), `test-ui-agent54-docs` (A-22), `test-ui-agent54-protect` (A-12), `test-ui-share-quotas` (`share_guard_locked`); dazu `tests/lib/desk-notices.test.ts` (D102-42) und `tests/components/foresight-save-error.test.ts` (A-32).
- **Nicht gelaufen:**
  - `test-ui-crawl` braucht ein echtes `titand -dev`.
  - `test-ui-webapp-focus` braucht den gebauten T-PDF für das Web. Hier gibt es keinen Build davon, nur den Download von GitHub.
  - `test-ui-fixtures`, `test-ui-nvr-fixtures` und `test-ui-rfb-peer` sind Hilfsdateien, keine Prüfungen.
