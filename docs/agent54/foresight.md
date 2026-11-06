# Vorausschau (PLAN 54-V): Was-wäre-wenn und Hardware-Berater

Zwei Funktionen des KI-Agenten für das ganze NAS. Beide sind **ab Werk aus**;
ein- und ausschalten, einstellen und ansehen können nur Besitzer und
Administratoren (serverseitig `requireAdmin`, die Werkzeuge bekommt nur ein
Konto mit `CanAdminister`). Einstellungen: Einstellungen › KI-Agent ›
Funktionen › Vorausschau (oder im Agenten unter Einstellungen › Funktionen).
Gespeichert in `agent.json` (`features.whatIf`, `features.advisor`) auf dem
Stick, nur beim Speichern; nichts davon ist geheim.

## Was-wäre-wenn

Spielt am Abbild des NAS durch, ohne etwas zu ändern:

- **Platte X fällt aus** (je Platte): Daten in Gefahr, geschätzte
  Wiederaufbauzeit, Risiko in dieser Zeit, Backup-Lücken je Freigabe, was
  gesichert ist, Rat.
- **Volume wird voll**: Belegung, Wachstum je Tag und Tage bis voll (wenn
  gemessen), was dann nicht mehr geht (Schreiben, Snapshots, Sicherungen auf
  dieses Volume, Daten des Agenten).
- **Was bringt RAID 1 (Spiegel) / RAID 5 / RAID 6 / RAID 10 / Btrfs RAID1C3**:
  Ausfallsicherheit heute und danach, nutzbarer Platz mit den vorhandenen
  Platten, wie viele Platten fehlen, ob die Daten passen und welcher Weg
  (Btrfs: „Profil ändern“ im Betrieb; mdadm: neues Volume und umkopieren).

Bedienung: Speicher › Volume öffnen › **Durchspielen** (und in den Details
einer Mitgliedsplatte). Oben wählt man das Szenario dieses Volumes. Im Chat
über das Werkzeug `whatif` (Lesen, Bereich Speicher), z. B. „Was passiert,
wenn sdb ausfällt?“. Der Rat verlinkt nur vorhandene Seiten; „Starten“ startet
die gewählte Sicherung über ihre eigene Route (`POST /backups/{id}/run`), erst
auf Klick.

Daten (alle aus dem RAM, keine Platte wird geweckt): `storage.Status`
(Volumes, RAID-Art, Mitglieder und ihr Zustand, Größen, SSD-Cache),
zwischengespeicherte SMART-Werte (`SmartReports`, kein smartctl), die
Wiederaufbau-Geschwindigkeit (laufender Sync; sonst die zuletzt gesehene,
alle fünf Minuten aus dem Status gemerkt; sonst geschätzt nach Plattenart und
Tempo-Voreinstellung: HDD 150 MB/s, Btrfs auf HDD 100 MB/s, SSD 400 MB/s,
NVMe 800 MB/s, „schonend“ 30 MB/s), Freigaben und Sicherungen
(`backup.CachedViews`: nur bekannte Zustände, nie ein Repository oder eine
Zustandsdatei geöffnet).

Regeln der Rechnung (`backend/internal/whatif`):

- Verkraftet: RAID 0 und Btrfs single 0, RAID 1 n−1, RAID 5 1, RAID 6 2,
  RAID 10 sicher 1 (die zweite Platte: „könnte verloren sein“, weil die
  Spiegelpaare nicht bekannt sind), Btrfs RAID 1 immer 1 (zwei Kopien), RAID1C3 2.
  Fehlende, ausgefallene und noch nicht wieder aufgebaute Platten zählen ab.
- Btrfs single mit mehreren Platten gilt als verloren (kein „teilweise lesbar“).
- Lese-Cache fällt aus: nur langsamer. Lese- und Schreib-Cache: was noch nicht
  zurückgeschrieben ist, ist weg; Rat „nur lesen“.
- Wiederaufbau mdadm: ganze Platte; Btrfs: belegte Daten dieser Platte.
- Risiko: übrige Platten mit umgelagerten/schwebenden Sektoren, Lesefehlern,
  SMART-Fehler, ≥ 3 bzw. ≥ 5 Jahren Betriebszeit, ≥ 80 % Abnutzung; schon
  geschwächtes Volume; Wiederaufbau über einen Tag. „hoch“ nur, wenn danach
  keine Redundanz bleibt. Fehlende SMART-Werte allein sind kein Risiko.
- Backup schützt eine Freigabe nur, wenn die Sicherung eingeschaltet ist, die
  Freigabe (oder das ganze Volume) enthält und **nicht auf demselben Volume**
  liegt. Lücke: kein Erfolg in den letzten 7 Tagen, nie geklappt, keine
  Sicherung, oder Zustand gerade unbekannt (eigene Anzeige, kein „gesichert“).
  Snapshots schützen nicht vor dem Ausfall der Platten (Hinweis bei Btrfs).
- Freigabe-Größen: nur, was die letzte Sicherung gezählt hat; sonst keine Zahl.

API (nur Besitzer/Administratoren): `GET /api/v1/agent/whatif` (an/aus und
Ziel-RAID-Arten, für den Knopf), `POST /api/v1/agent/whatif` mit
`{kind: disk|full|level, disk, volume, level}`; aus: 403
`agent_feature_off`; unbekannte Platte/Volume 404; falsches Szenario 400.

## Hardware-Berater

Sagt aus dem SMART-Verlauf voraus, welche Platte wahrscheinlich ausfällt, und
erkennt Engpässe. Einstellungen (Werte ab Werk, sobald eingeschaltet):

| Einstellung | ab Werk |
|---|---|
| Plattenausfall vorhersagen | an |
| Engpässe erkennen | an |
| Ersatz mit Preis (Internetsuche) | aus |
| Monatsbericht (Mitteilung) | aus |
| Uhrzeit des nächtlichen Laufs | 03:30 |

`predict` und `bottlenecks` fehlen in `agent.json`, solange sie an sind
(`nil` heißt an), damit ältere Dateien die Antwort des Inhabers bedeuten.

Ablauf: Läuft einmal in der Nacht zur eingestellten Zeit (aus dem Minutentakt
des Agenten, also nur, solange der Agent eingeschaltet ist) und holt eine
verpasste Nacht tagsüber nach (bis 20 Stunden danach). Er wartet, solange eine
Platte des Agent-Volumes oder des Volumes mit dem SMART-Verlauf schläft. „Jetzt
prüfen“ in der Ansicht rechnet sofort; liegt der Verlauf dann auf einer
schlafenden Platte, wird nur mit den heutigen Werten gerechnet und das
angezeigt. Der Bericht liegt in `advisor.json` im gemeinsamen Ordner des
Agenten (`.titanos/agent/` auf dem Agent-Volume), nie auf dem Stick.

Daten:

- SMART-Verlauf: vorhandener täglicher Verlauf `internal/smarthistory`
  (`.titanos/smart/` auf dem ersten eingebundenen Volume). Kein eigener
  Recorder. Ohne Verlauf nur die heutigen Werte.
- Arbeitsspeicher alle 10 Minuten (verfügbar, PSI-Druck, Swap aus
  `/proc/meminfo`), CPU jede Minute, beides aus dem RAM des Monitors, Ringe im
  RAM (1 Tag CPU, 7 Tage Speicher).
- Volume-Belegung stündlich (30 Tage, mit dem Bericht gespeichert und nach
  einem Neustart mit den neuen Proben zusammengeführt); Wachstum
  mit der Regression von `monitoring.CapacityTrends`, bei eingeschaltetem
  längerem Monitoring dessen Trend.
- Grafikkarte: `Accelerators.Snapshot()` (fehlende Werte bleiben „unbekannt“,
  nie 0) und was die geladenen Ollama-Modelle belegen (Zeitlimit 5 s).
- Temperaturen: Platten aus SMART, sonst die Sensoren des Monitors.

Regeln (`backend/internal/advisor`): Fenster 6 Wochen. „Ausfall“: SMART meldet
Fehler; umgelagerte Sektoren steigen zusammen mit schwebenden Sektoren oder
Lesefehlern oder um ≥ 10; schwebende Sektoren steigen; Lesefehler oder
NVMe-Medienfehler steigen; Abnutzung ≥ 95 % oder in < 180 Tagen am Ende;
NVMe-Reserve unter der Schwelle. „ansehen“: ein einzelner neuer Sektor,
gleichbleibende Sektoren, schwebende Sektoren (können zurückgehen), ≥ 80 %
Abnutzung, ≥ 5 Jahre Betriebszeit, steigende CRC-Fehler (Kabel/Anschluss, nicht
die Platte). Engpässe: Speicher ≥ 90 % belegt oder PSI ≥ 10 in einem Viertel
der Proben, Swap > 25 % belegt; Volume voll in < 60 Tagen oder ≥ 95 %;
CPU ≥ 6 Stunden am Tag über 85 %; Grafikspeicher ≥ 95 %; Platten ≥ 55 °C
(NVMe 70 °C), Chips ≥ 90 °C. Betriebszeit heißt „in Betrieb“, nicht Alter.

Ersatz mit Preis: nur wenn eingeschaltet und die Internetsuche in den Rechten
des Agenten nicht gesperrt ist; die Suchanfrage enthält nur Modell, Größe und
Art (nie Seriennummer, Namen, Pfade). Treffer sind fremde Seiten: angezeigt
als Links mit `rel="noopener noreferrer nofollow"` und „ungeprüft“; das
Werkzeug ist dann als fremder Inhalt markiert.

Monatsbericht: beim ersten nächtlichen Lauf eines Monats über
`Notify.SendReport` (deutscher Text wie der Wochenbericht); der gesendete
Monat steht in `advisor.json`.

Ansicht: Agent-Ansicht `advisor` (`ForesightViews.svelte`), erreichbar über
Funktionen › Hardware-Berater › „Bericht öffnen“. Gesunde Platten werden nicht
einzeln aufgeführt. Werkzeug `hardware_advice` (Lesen): letzter Bericht, sonst
ein Lauf jetzt. API: `GET /api/v1/agent/advisor`, `POST /api/v1/agent/advisor/run`
(beide Besitzer/Admins, aus: 403).

## Grenzen und nicht getestet

- Die Wiederaufbauzeit ist eine Schätzung; die Geschwindigkeit hängt von Last
  und Platten ab. Gemessen heißt: auf diesem Volume zuletzt gesehen.
- RAID 10: welches Spiegelpaar betroffen ist, wird nicht ausgelesen.
- Freigabe-Größen ohne Sicherung sind nicht bekannt (kein Scan im Hintergrund).
- In den ersten Minuten nach dem Start kennt titand die Zustände der
  Sicherungen noch nicht (die Sicherung liest sie ab der dritten Minute);
  bis dahin zeigt Durchspielen „Stand gerade nicht bekannt“ statt „gesichert“.
- Eine Platte, die seit dem Start schläft, wird mit dem neuesten Tag ihres
  SMART-Verlaufs beurteilt; ohne Verlauf heißt es „keine SMART-Werte“.
- Die Vorhersage ist eine Faustregel aus Trends, keine Garantie; ohne Verlauf
  ist sie schwächer.
- Nicht getestet: echte Platten, echtes mdadm/Btrfs, echte Grafikkarte, echtes
  Ollama, echte Websuche, Zustellung des Monatsberichts. Getestet mit Fakes:
  Go-Tabellentests (`internal/whatif`, `internal/advisor`), API-Tests
  (`agent_foresight_test.go`: ab Werk aus, Rechte Benutzer/Admin, Werkzeuge nur
  wenn an, Websuche über httptest ohne Seriennummer, nächtlicher Lauf einmal je
  Nacht), Web-Tests (`tests/agent54-foresight.test.mjs`) und
  `scripts/test-ui-agent54-foresight.mjs` (gemockte API, 390×844 und 1440×900,
  Bilder unter `out/validation/v057-foresight/`).
- Hinweise als Vorschläge im Eingang des Agenten sind nicht gebaut (optional).
- Ein Eintrag „Hardware-Berater“ im Menü des Agenten braucht Änderungen an
  gemeinsamen Dateien (siehe Bericht).
