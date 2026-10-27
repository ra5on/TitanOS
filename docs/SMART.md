# SMART-Übersicht

Seit 0.28.0 zeigt TitanOS die SMART-Werte jeder Platte unter **Speicher › Weitere Optionen › SMART-Übersicht** (`/storage/smart`, nur Besitzer und Administratoren). Die Plattendetails der Speicherseite verlinken über „SMART-Details“ direkt auf die Platte (`/storage/smart?disk=sda`); in der Befehlspalette heißt die Seite „SMART-Übersicht“.

## Woher die Werte kommen

- `storage.Manager.RefreshSmart` ruft für jede Platte `smartctl --json=c --health --attributes --info --nocheck=standby <gerät>` auf: beim Start von titand, danach alle zehn Minuten im Hintergrund und auf Anforderung. Es ist dieselbe Abfrage, die vorher schon die Speicherseite, die Meldungen und Home Assistant versorgt hat; es gibt keine zusätzliche zyklische Abfrage.
- **Schlafende Platten werden nicht geweckt.** `--nocheck=standby` lässt smartctl eine Platte im Standby überspringen (Exit-Bit 2, Meldung „Device is in STANDBY mode“). TitanOS erkennt diese Antwort und behält die zuletzt im Wachzustand gelesenen Werte samt Lesezeit; die Oberfläche zeigt zusätzlich das Abzeichen „schläft“. Schläft eine Platte seit dem Start von titand, gibt es noch keine Werte („Noch nicht gelesen“). Seit 0.28.0 behalten auch `GET /api/v1/storage` (Speicherseite), Meldungen und MQTT die letzten Werte einer eingeschlafenen Platte, statt sie wie vorher bis zum Aufwachen zu verlieren.
- Alles liegt nur im RAM von titand (`smartEntry` je Gerätepfad); nichts wird auf den Stick oder ein Volume geschrieben, nichts aus `/config` gelesen.
- Der Plattentest (Werkzeuge › Platten vorbereiten) liest SMART weiterhin ohne `--nocheck`, weil er die Platte ohnehin benutzt.

## API

| Route | Rolle | Wirkung |
|---|---|---|
| `GET /api/v1/storage/smart` | Besitzer/Administrator | `{disks: [...]}` aus dem RAM-Cache, ohne Attributtabellen. Ruft kein smartctl auf. |
| `POST /api/v1/storage/smart/refresh` | Besitzer/Administrator | liest die wachen Platten neu, höchstens alle 30 Sekunden (sonst Cache); gleichzeitige Aufrufe teilen sich einen Lauf. Antwort wie `GET`. Nicht im Aktivitätsprotokoll (reines Lesen). |
| `GET /api/v1/storage/smart/{disk}` | Besitzer/Administrator | eine Platte (`sda`, `nvme0n1`) mit vollständiger Attributtabelle; unbekannt → 404 `disk_not_found`. |

Benutzerkonten erhalten 403. Ein Eintrag (`storage.SmartReport`) enthält Gerät, Modell, Seriennummer, Art, Rolle/Volume, `state` (`ok`, `warning`, `failed`, `unavailable` = kein SMART, `unknown` = noch nicht gelesen), `warnings` (Codes, siehe unten), `sleeping`, `checkedAt` (letzter Blick), `readAt` (Zeit der angezeigten Werte) und `values`: Anschluss, Modellreihe, Firmware, Selbsteinschätzung, Temperatur, Betriebsstunden, Einschaltvorgänge, umgelagerte/schwebende/nicht korrigierbare Sektoren, CRC-Fehler, bei NVMe `nvme` (kritische Warnung, Verschleiß, Reserve und Schwelle, Medienfehler, Fehlerprotokolleinträge, unsaubere Abschaltungen, gelesene/geschriebene Dateneinheiten) und `attributes` (ATA: ID, Name, Wert, schlechtester Wert, Schwelle, Rohwert, Flags, Vorausfall/Alterung, `whenFailed`, `warn`; NVMe: alle Felder des Zustandsprotokolls als Name/Rohwert). Fehlende Werte sind `null`, nie erfunden.

Zuordnung der Zähler: ATA 5 → umgelagert, 197 → schwebend, 198 (oder 187, wenn größer) → nicht korrigierbar, 199 → CRC. SAS/SCSI: Grown-Defect-Liste → umgelagert, Summe der `total_uncorrected_errors` aus dem Fehlerzählerprotokoll → nicht korrigierbar. Betriebsstunden und Temperatur stammen aus den herstellerunabhängigen Feldern `power_on_time` und `temperature` von smartctl, nicht aus teils gepackten Rohwerten.

## Schwellen der Abzeichen

Festgelegt in `backend/internal/storage/smart.go` (`judgeSmart`); bei Änderungen dieses Dokument und den Text `smart.thresholds` beider Sprachdateien mitziehen.

**Fehler (rot)**
- `smartFailed`: die Platte meldet `smart_status.passed = false`.
- `attributeNow`: ein ATA-Attribut liegt jetzt unter seiner Schwelle (`when_failed = now`).
- `nvmeCritical` mit Bit 2, 3 oder 4 der kritischen NVMe-Warnung (Zuverlässigkeit beeinträchtigt, nur lesen, Sicherung des flüchtigen Speichers fehlgeschlagen).

**Warnung (gelb)**
- `reallocated`, `pending`, `uncorrectable`: der jeweilige Sektorzähler ist größer als 0.
- `crc`: UDMA-CRC-Fehler größer als 0 (meist Kabel oder Anschluss).
- `temperature`: ab 55 °C bei HDDs und SATA-SSDs (wie die Meldung „Platte über 55 °C“), ab 70 °C bei NVMe.
- `wear`: NVMe `percentage_used` ab 90 %.
- `mediaErrors`: NVMe-Medienfehler größer als 0.
- `spare`: NVMe-Reserve an oder unter ihrer Schwelle.
- `nvmeCritical` mit anderen Bits (Reserve, Temperatur).
- `attributePast`: ein ATA-Attribut lag früher unter seiner Schwelle (`when_failed = past`).

Sonst **In Ordnung (grün)**. Die Zähler setzen sich nicht von selbst zurück; eine Warnung bleibt bestehen, bis die Platte ersetzt ist. Die auslösenden Zeilen der Attributtabelle sind markiert. Status steht immer als Text im Abzeichen, nie nur als Farbe. Die bisherigen Meldungen (`disk_unhealthy`, Temperatur) bleiben unverändert; die Übersicht erzeugt keine neuen Meldungen.

## Diagnosepaket

Das Diagnosepaket (Werkzeuge) enthält seit 0.28.0 `smart.json`: je Platte Zustand, `sleeping`, `checkedAt`, `readAt` und die vollständige smartctl-Antwort der angezeigten Werte. Vor dem Packen werden wache Platten über den 30-Sekunden-Cache neu gelesen (höchstens zwei Minuten); schlafende werden nicht geweckt und liefern ihre letzten Werte oder keine. Seriennummern, WWN, `logical_unit_id`, EUI-64/NGUID und die lokale Uhrzeit werden aus dem JSON entfernt, danach läuft der übliche Redaktor darüber.

## Prüfung und Grenzen

- Go-Tests mit aufgezeichneten smartctl-Antworten (`backend/internal/storage/testdata/smart-sata-hdd.json`, `smart-nvme.json`, `smart-standby.json`): Parsen, alle Schwellen, Erkennung des Standby, Beibehalten alter Werte, 30-Sekunden-Cache, Entfernen der Kennungen fürs Diagnosepaket, API-Rechte (401/403/200) und 404 für unbekannte Platten. Webtest `web/tests/smart.test.mjs` für Sortierung, Zählung und Antwortprüfung.
- Die Testdaten sind nach dem Format von smartctl 7.4 nachgebaut, nicht von einem konkreten Gerät abgeschrieben. Echte Platten, insbesondere SAS, USB-Adapter (`-d sat`) und das Verhalten von `--nocheck=standby` an echten schlafenden HDDs, sind noch auf Hardware zu prüfen. Virtuelle QEMU-Platten melden kein SMART („Kein SMART“).
- Kein Verlauf der Werte über die Zeit, keine eigenen Schwellen je Platte, keine zusätzlichen Benachrichtigungen aus den Warnungen.
