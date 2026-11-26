# Desktopseiten und Dialoge

Der Vergleich beginnt mit dem unveränderten Baum von PR 414. Ein regulärer Desktopaufruf öffnet ein echtes TitanOS-Fenster. Dessen Inhaltsbreite kann deutlich kleiner als die des Browsers sein. Die Erfassung verwendet deshalb den Fenstermodus und hält auch die tatsächlich geladene Adresse fest. Der Vergleich mit Programmrechten verwendet ein lokales Testkonto. Die separat erfassten Fachdaten stammen aus Testantworten.

## Gemeinsame Entscheidungen

| Element | Bisher | Desktopregel |
|---|---|---|
| Fenster und Leisten | Desktopfenster, Statusleisten und das Menüsystem existierten bereits. Transparente Kacheln standen teilweise auch hinter langen Listen. | Fensterrand, Dock und Menüs beziehen ihre Konturen und Unschärfe aus der Nordlicht-Familie. Formulare und Datenlisten erhalten einen ruhigen Lesefond. Das hinterlegte Hintergrundbild bleibt sichtbar. |
| Beschriftung | Die Titelleiste übernimmt bereits die Rolle der sichtbaren Seitenüberschrift. | Ein Pfad im Fenstertitel, darunter die Werkzeuge der betreffenden Ansicht. Aktuelle Auswahl, offene Störungen und Hauptaktionen sind auch bei Tastaturbedienung erkennbar. |
| Daten und Medien | Explorer, PIM, Kameraansicht, Terminpläne und Terminal haben unterschiedliche fachliche Gerüste. | Zahlen spaltenweise ausrichten, Auswahl auch mit einer Kante markieren. Leseflächen und Seitenleisten unterscheiden. Kamerabild, Terminal, VM-Bildschirm, Nachrichtendesign und Fremdlogos behalten ihre eigenen Inhalte. |
| Formulare und Dialoge | Gemeinsame Einstellungszeilen, native modale Dialoge und ein Gefahrenbereich waren vorhanden. | Verwandte Zeilen bilden eine Gruppe. Dialoginhalt erhält eine gut lesbare Fläche. Aktionszeile und Gefahrenbereich sind vom Inhalt getrennt. Die bestehende Reihenfolge für Fokus, Bestätigen und Abbrechen bleibt wirksam. |
| Bewegung und Dichte | Persönliche Farben, Schriftgröße, Oberflächen und Fenstergeometrien sind schon gespeichert. | Das Fenster öffnet sich aus seinem eigenen Dock-Symbol. Bei reduzierter Bewegung entfällt diese Animation. Profile sind optional. Einzelwerte bleiben gespeicherte Kontodaten. |

## Arbeitsplatz und Programme

Die Spalte „Prüfpunkt“ bezeichnet die Aufgabe oder die fachliche Ausnahme. Der tatsächliche Testumfang und gefundene Einschränkungen stehen im [Abnahmebericht](NORDLICHT-ABNAHME.md). Die Fotos enthalten auch leere, deaktivierte und nicht gefundene Ziele, wenn das Testkonto diesen Zustand auslöst.

| Adresse | Vorhandenes Arbeitsmittel | Prüfpunkt und Gestaltung |
|---|---|---|
| `/` | Arbeitsplatz mit Fenstern und frei belegbaren Widgets | Vorhandenes Statuswidget führt zu Systemzustand, betroffenem Sicherungsauftrag und laufender Aufgabe. CPU und RAM bleiben eigene Messwerte. Das Menü, die Leisten und das Dock teilen eine Glasoberfläche. |
| `/files` | Orte, Pfad, Befehle, virtuelle Dateiliste und optionale zweite Spalte | Orte als Seitenleiste, aktiver Pfad mit Positionsangabe, Hochladen als sichtbare Aktion. Auswahl, Sortierung, Zwischenablage, Kontextmenü, Papierkorb und Wiederherstellung bleiben im Explorer. |
| `/docker` | Tabelle für Apps und einzelne Container | Ein sichtbarer Katalogeinstieg. Importieren, eigenen Container einrichten, Sammelaktionen und Einstellungen unter ⋯. Der Protokolllink öffnet den konkret gefundenen Container. |
| `/docker/apps/[name]` | Compose-Konfiguration und laufende Dienste | Bearbeitung und Aktionsdialoge im vorhandenen Fenster. Dienstnamen und Quelltext brauchen die volle Breite. |
| `/docker/images` | Image-Liste und belegter Platz | Herkunft, Tag und Größe bleiben vergleichbare Tabellendaten. Entfernen führt über den bestehenden Dialog. |
| `/docker/volumes` | Docker-Volumes | Verwendung und belegten Platz vergleichen. Kein Kartenraster. |
| `/docker/networks` | Docker- und LAN-Netze | Netzwerkdetails und Einrichtung bleiben in ihrem eigenen Formular. |
| `/docker/cleanup` | Vorschau der Bereinigung | Vor dem Entfernen stehen Vorschau, Warnung und Bestätigung im Aktionsbereich. |
| `/apps` | Katalog, installierte Apps und Quellen | Katalogkarten mit Originallogo und deutlich lesbarer Herkunft. Der Filter „Installiert“ führt zur bestehenden App. Der Katalog behält den Weg zu einer weiteren Installation. |
| `/apps/install/[source]/[id]` | Installationsformular und Fortschritt | Quelle und angeforderten Speicher im Formular prüfen. Der Fortschritt folgt dem tatsächlichen Installationsauftrag. |
| `/apps/new` | Eigener Container | Gleiches Formular mit freier Image-Wahl. Seltene Ressourcenoptionen bleiben erreichbar. |
| `/plugins`, `/plugins/cleanup` | Erweiterungsliste, Detailbereich und Bereinigung | Auswahl im Katalog markieren. Empfohlene und fremde Erweiterungen sind als solche erkennbar. Bestätigung vor dem Aufräumen. |
| `/storage` | Volume- und Plattentabelle | Status und Belegung stehen neben dem Namen. Belegter und gesamter Platz sind als Messwert zugänglich. Detail- und Aktionsflächen folgen der Auswahl. |
| `/storage/import` | Suche und lesende Prüfung | Import zuerst prüfen. Registrierung und potenziell schreibende Aktionen sind ausdrücklich unterscheidbar. |
| `/storage/manage` | Volume-Details, Erweiterung, Snapshots | Verlauf, Prüfen und Datei-Rücksicherung nutzen dieselbe Auswahl. Größen bleiben echte Zahlen. |
| `/storage/replace` | Austausch eines Laufwerks | Quelle, Ersatz und Bestätigung bleiben in einem eigenen Ablauf. |
| `/storage/smart` | SMART-Werte und Selbsttests | Technische Namen und Rohwerte behalten eine Tabelle. |
| `/shares` | Freigabenliste und Rechte | Auswahl, Anbindung und Verwaltungsaktionen zusammenhalten. Erstellen, Entsperren und Entfernen bleiben eigenständige Dialoge. |
| `/share` | Betriebssystem-Teilen-Eingang der Web-App | Eine vom Gerät geteilte Datei braucht Zielauswahl und Übertragungsstatus. Ein leerer Aufruf hat einen eigenen Zustand. |
| `/vms` | Virtuelle Maschinen, Auswahl und Betrieb | Tabellenzeile, Betriebszustand, Aktionen und Detailbereich bleiben zusammen. Konsolen wechseln in die Medienansicht. |
| `/vms/new`, `/vms/edit` | VM-Assistent und Konfiguration | Laufwerke, Speicher und Geräte sind Formulardaten. Schmale Fenster dürfen die Bestätigung nicht abschneiden. |
| `/vms/media` | Installationsmedien | Medien und tatsächlicher Downloadstatus im eigenen Verwaltungsbereich. |
| `/vms/screen` | Gastbild und Bildschirmsteuerung | Unverfälschte Bildfläche, eigener Kontrast für die Werkzeuge. Die Gast-Tastatureingabe bleibt wirksam. |
| `/lxc` | Liste und Detail eines Systemcontainers | Zustände als Text, markierte Auswahl und Zugriff auf Ressourcendetails. |
| `/users`, `/account` | Kontenverwaltung und eigenes Konto | Eigene Geräte und Anmeldedaten bleiben von Administrationsaktionen getrennt. Rollen, Passwortwechsel und Bestätigungsdialoge behalten ihre bestehenden Prüfungen. |
| `/notifications` | Meldungsliste und Einzelheiten | Schweregrad mit Text. Direkter Weg zum betroffenen Programm. |
| `/tools`, `/tools/activity` | Werkzeuge und Aktivitätsliste | Filter und Vorgang stehen in einer Ebene. Einträge bleiben zeitlich ablesbar. |
| `/tools/tasks`, `/tools/taskmanager` | Planung, Auftragsstatus und aktuelle Prozesse | Planung und laufende Arbeit bleiben verschieden. Messwerte folgen dem echten Prozess bzw. Auftrag. |
| `/tools/monitoring` | Zeitraum, Verlauf und Export | Zeitraum und Messwerte sichtbar. Selten genutzte Dateiexporte im Desktop-Menü. |
| `/tools/logs` | Protokollfilter und fortlaufender Text | Ruhiger Textgrund, Quellspalte, Zeit und beschrifteter Schweregrad. Der Logbereich ist per Tastatur erreichbar. |
| `/tools/disks`, `/tools/hardware` | Laufwerkstests und Hardwareprüfung | Laufwerk bzw. Gerät vor seinen Details. Fortschritt und Diagnose bleiben direkt beim Test. |
| `/tools/maintenance`, `/tools/scripts` | Wartungsplan und eigene Skripte | Plan, letzte Ausführung und Fehler voneinander unterscheiden. Der Skripteditor behält eine nichtproportionale Schrift. |
| `/tools/bundle` | Diagnosepaket | Inhalt vor Erzeugung auswählen. Paket und Fortschritt als eigene Aufgabe. |
| `/terminal` | Terminalemulator | Terminalzeichen, Cursor und Hintergrund gehören zur Sitzung. Fenster, Fokus und Dialoge folgen dem Desktop. |
| `/mail`, `/mail/compose` | Drei Spalten, Leser und separates Schreiben | Breite zum Lesen bzw. Schreiben verwenden. HTML-Mails und Anlagen behalten ihre Darstellung. |
| `/mail/setup`, `/mail/settings` | Postfach hinzufügen und konfigurieren | Anbieterdaten und Kontodaten als Formular. Geheimnisse bleiben verdeckt. |
| `/calendar`, `/calendar/event`, `/calendar/iphone` | Zeitraster, Terminfenster und Geräteeinrichtung | Zeit und Spalten bleiben das Ordnungsprinzip. Lange Termine und private Inhalte dürfen nicht zwischen Konten sichtbar werden. |
| `/contacts`, `/contacts/iphone` | Adressbücher, Personenliste, Karte und Geräteeinrichtung | Auswahl über Liste und zugehörige Fachdetails. Keine Umstellung auf Systemkacheln. |
| `/nvr`, `/nvr/cameras` | Live-Ansicht und Kameraverwaltung | Kameraflächen und Bildverhältnis haben Vorrang. Werkzeuge liegen außerhalb des Bildinhalts. |
| `/nvr/events`, `/nvr/recordings` | Ereignisse, Aufnahme und Zeitleiste | Kamera und Uhrzeit führen durch die Auswahl. Bedienelemente einer Aufnahme gehören zum Player. |
| `/nvr/diagnosis`, `/nvr/settings` | Verfügbarkeit und Aufzeichnungsregeln | Diagnose als Text und Messwert. Regeln als Formular. |
| `/agent` | Unterhaltung, Verlauf und Werkzeuge | Die Unterhaltung bleibt ein eigener Arbeitsraum. Rechte, Datenquelle und Rückfragen behalten ihren Platz. |
| `/welcome`, unbekannte Adresse | Erststart-Assistent und Fehlerseite | Ein klarer nächster Schritt. Der Assistent und die Rückkehrnavigation behalten ihre eigenen Inhalte. |

## Systemsteuerung

Die vollständige Zuordnung der Einstellungen, der früheren Bereiche und der weiterhin erreichbaren Direktlinks steht in [Systemsteuerung](NORDLICHT-SYSTEMSTEUERUNG.md). Der Browservergleich erfasst jede Unterseite einschließlich Sicherungsstand, Wiederherstellung, Datensicherung, Anwendungssicherung, Konfiguration und Abdeckung separat. Alte Adressen, die absichtlich auf eine neue kanonische Adresse führen, werden mit ihrem tatsächlichen Ziel erfasst.

## Dialoge und Zugangsansichten

Der Katalog aus `scripts/test-ui-nordlicht-dialog-inventory.mjs` ordnet die modalen Svelte-Komponenten, nativen Dialoge und Dialogrollen nach Quelldatei. Auch bedingte Dialoge für gesperrte Freigaben, Laufwerkstausch, Rollenwechsel, Kameraeinrichtung und Wiederherstellung sind so auffindbar. Mehrere Auslöser können denselben Baustein öffnen. Der interaktive Klicktest erfasst zusätzlich den Auslöser, die Seite, den tatsächlich angezeigten Titel und ein Bild. Der Bildvergleich führt diese beiden Verzeichnisse getrennt.

Anmeldung, Erstkonfiguration, Sperre und Offline-Zustand werden innerhalb der App-Hülle eingeblendet. Die Anmeldung behält ihr konfiguriertes Erscheinungsbild. Beim Sperren bleiben Dialog und Authentifizierung im Vordergrund. Die Offline-Ansicht muss den Verbindungszustand kenntlich machen.

## Wiederholung

`scripts/test-ui-desktop-inventory.mjs` liest die Routendateien aus dem Checkout und ruft sie mit einem lokalen Entwicklungsserver, einem Wegwerf-Konto und Chromium auf. Die Variable `UI_OUTPUT` bestimmt den Ordner für die Bilder und das JSON mit angeforderter Adresse, tatsächlicher Adresse, Überschriften, Aktionen und Befunden. `scripts/test-ui-nordlicht-desktop-apps.mjs` prüft Desktop-Fenster und Smartphone mit kontrollierten Fachdaten. Das ist von den Ausfalltests auf virtuellen Laufwerken unabhängig.
