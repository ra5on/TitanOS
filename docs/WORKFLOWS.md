# Speicher, Wartung und Aufträge ab 0.16.0

Die neuen Bedienseiten verwenden die bestehenden Rollen: Besitzer und
Administratoren verwalten Speicher, Wartung, Backups und VMs. Dateiübertragungen
bleiben Eigentum des jeweiligen Kontos. LUKS-Verschlüsselung ist nicht vorgesehen.

## Bedienung

- **Shares:** kompakte Liste mit Suche, Volume-/Protokollfilter, Sortierung und
  Dichte. Details öffnen auf Klick; Verbindungshilfe und Dateimanager sind direkt
  erreichbar. Ansicht je Konto, Suche/Auswahl/Scrollposition nur im laufenden
  Browser. Formularreiter erhalten Eingaben, Fehler schließen den Dialog nicht.
- **Docker:** Stacks starten geschlossen. WebUI, Tailscale-WebUI und öffentliche
  Adresse stammen vom automatisch oder ausdrücklich gewählten Hauptdienst.
  Gespeichert wird der Compose-Dienstname je Konto. Fehlt ein ausdrücklich
  gewählter Dienst, wird kein anderer eingesetzt. Suche/Filter zeigen passende
  Dienste; ausgefallene Abfragen behalten den gekennzeichneten letzten Stand.
- **Strg/⌘+K:** Befehlspalette mit autorisierten Navigationszielen, einschließlich
  Wartung, Aufträgen und Monitoring. Keine Shell und keine Änderungen durch
  bloßes Auswählen eines Treffers.
- **Werkzeuge › Aufträge:** laufende Datei-, Backup-, Speicher-, Docker- und
  VM-Arbeit, Fehler, nächste Termine sowie Prüfung unterbrochener Vorgänge.
  Das Dashboard zeigt dieselben handlungsrelevanten Zustände in kompakter Form.
  Seit 0.17.0: Alle/Aktiv/Geplant/Abgeschlossen/Fehler, Suche, eigene KI-Aufträge
  und aktivierte Automationen, registrierte Uploads und unterstützte direkte
  Steuerung. Ereignis-Automationen ohne Termin bleiben wartend. Angezeigt werden
  die je Fachbereich vorhandenen Ergebnisse (Wartung 100, Datei-Aufträge begrenzt,
  sonst letzte bekannte Zustände), kein unbegrenztes neues Gesamtarchiv. Eigene
  Uploads benötigen zum Fortsetzen weiterhin die Browser-Dateiobjekte.

## Vorhandenen Speicher übernehmen

**Speicher › Speicher übernehmen** erkennt bereits zusammengesetzte,
unregistrierte mdadm-Arrays mit ext4/XFS sowie native Btrfs-Spiegel mit genau
zwei Geräten auf zwei physischen Platten. Nicht zusammengesetzte Arrays, LVM,
ZFS, verschlüsselte Volumes und fremde Speichermanager werden nicht automatisch
umgebaut oder zusammengefügt.

Die Erkennung verändert keine Platte. Eine ausdrücklich angeforderte Vorschau
bindet vorübergehend mit `ro,nodev,nosuid,noexec` ein, zusätzlich ext4 `noload`,
XFS `norecovery,nouuid` bzw. Btrfs `nologreplay,subvolid=5`. Btrfs muss Daten
und Metadaten im RAID1-Profil halten. Sichtbar sind höchstens 200 unmittelbare
Ordner; keine Konten, Freigaben oder internen Einstellungen werden importiert.

Beim Registrieren sind Name und Gerätefingerabdruck erneut zu bestätigen.
TitanOS prüft die Identität nochmals, speichert Array-/Dateisystem-UUIDs und
bindet das Volume regulär schreibbar ein. Es formatiert und benennt nichts um
und legt auf importierten Volumes keine Standardordner an. Dateisystemfehler
werden nicht durch eine automatische Reparatur übergangen.

## RAID-Kapazität erweitern

**Speicher › Volume › Kapazität erweitern** fügt einem gesunden, untätigen
RAID5/6 mit ext4 oder XFS genau eine ausreichend große Platte hinzu.
Andere RAID-Level, Levelwechsel und Verkleinerungen gehören nicht zu diesem
Ablauf. Die Vorschau zeigt aktuelle und geschätzte künftige Nutzkapazität aus der
Array-Geometrie; Dateisystemreserve und Formatierung beeinflussen den Endwert. Die neue Platte braucht eine eindeutige Seriennummer; die Anfrage
enthält zusätzlich den aktuellen Gerätefingerabdruck und die Löschbestätigung.

Voraussetzung ist ein gesundes, eingebundenes Rettungs-Volume auf anderen
physischen Platten mit mindestens 256 MiB freiem Speicher. Dort liegen unter
`.titanos/growth/` Journal und die von mdadm verwendete Rettungsdatei.
`/config/storage/growth.json` enthält nur den selten geschriebenen Verweis;
Gerätenamen wie `sda` werden darin nicht gespeichert.

Das Journal steht vor jeder Vorbereitung, Aufnahme und Umbauphase auf dem
Volume. Während ein Vorgang offen ist, sind andere Speicheränderungen gesperrt
und App-Updates warten. Erst ein gesundes, untätiges Array mit der bestätigten
neuen Mitgliederzahl erlaubt `resize2fs` bzw. `xfs_growfs`.

Nach Unterbrechung zeigt die Oberfläche den offenen Vorgang. **Geprüft
wiederaufnehmen** verwendet vorhandene Metadaten, Seriennummer und dieselbe
Rettungsdatei. Es partitioniert keine Platte erneut. Ein nicht aktives Array
wird nur mit seiner UUID und vorhandener Rettungsdatei zusammengesetzt, ohne
`--force` oder `--invalid-backup`. Fehlende Voraussetzungen bleiben Fehler.
Eine noch nicht gestartete Vorbereitung lässt sich nach Prüfung beenden;
der Backend-Abgleich muss die alte gesunde Geometrie und `reshape_position=none`
bestätigen. Eine neue Spare-Platte wird allenfalls ausgehängt, nicht erneut
gewischt. Der Abbruch stellt vorher gelöschte Daten der neuen Platte nicht wieder her.

Eine aktuelle externe Sicherung bleibt vor dem Umbau erforderlich. Der
QEMU-Prüfstand umfasst RAID5/ext4 und einen titand-Neustart während des Umbaus;
ein Stromausfall im kritischen mdadm-Fenster und sämtliche RAID6-/XFS-Varianten
sind damit nicht pauschal abgenommen. Die Wiederherstellungsregeln folgen der
[mdadm-Dokumentation](https://man7.org/linux/man-pages/man8/mdadm.8.html).

## Btrfs-Snapshots und Speicherverbrauch

Der Snapshot-Browser zeigt unmittelbare Ordner mit Folgeseiten. Er verfolgt keine
Symlinks und blendet `.titanos`, Papierkorb und interne Teilkopien aus. Bis zu
200 reguläre Dateien, zusammen höchstens 10 GiB, lassen sich in einen **neuen**
Ordner unter `restored/` eines gemounteten Volumes kopieren. Veröffentlichte
Dateien überschreiben nichts; Quelle und Ziel bleiben über Dateideskriptoren
festgehalten. Die bisherige Wiederherstellung als neue beschreibbare
Btrfs-Untervolume-Kopie bleibt erhalten. Vor dem Start zeigt eine Vorschau
Dateien/Gesamtgröße/Ziel; der Start prüft die Snapshot-UUID erneut.

Geplante Snapshots stehen unter **Werkzeuge › Wartung**. Die Aufbewahrung löscht
nur Snapshots, die dieser Zeitplan selbst erzeugt und in seinem privaten
Inventar unter `.titanos/snapplans/<auftrag>.v2.json` mit Btrfs-UUID registriert
hat. UUID-Prüfung und Löschung teilen dieselbe Speichersperre. Manuelle Ersatzkopien
gleichen Namens bleiben erhalten. Alte reine Namensinventare werden geleert und
ihre vorhandenen Kopien erhalten; sie begründen keine Löschrechte. Nach einem Fehler beim Speichern
des Inventars bleibt eine bereits erzeugte Kopie erhalten und der Fehler sichtbar.

**Speicherverbrauch** scannt nur auf ausdrücklichen Start: höchstens 30 Sekunden
und 200.000 geprüfte Einträge, Verzeichnisse in begrenzten Leseblöcken.
Freigaben, Apps/Docker, VMs, Papierkorb, Snapshots und sonstige Ordner werden
zugeordnet; Papierkorbdaten werden aus den jeweiligen laufenden Ordnersummen
herausgelöst. Interne Betriebsdaten sind ausgenommen. Fehler und Begrenzungen
kennzeichnen Teilergebnisse. Logische Größen und belegte Blöcke versprechen
bei Hardlinks, Reflinks und Snapshots keinen entsprechend frei werdenden Speicher.
Die grafische Übersicht zeigt Anteile der erfassten logischen Daten.

## Gemeinsame Wartungsplanung

**Werkzeuge › Wartung** bietet RAID-Prüfung/Btrfs-Scrub, SMART-Langtest,
Backup-Datenprüfung, Wiederherstellungsprobe und Btrfs-Snapshot mit Aufbewahrung.
Ein gemeinsames Fenster und eine IANA-Zeitzone gelten für tägliche, wöchentliche
oder monatliche Termine (Monatstag 1–28). Gleiche Fenstergrenzen erlauben den
ganzen Tag. Verpasste Läufe werden je Auftrag entweder im nächsten Fenster
nachgeholt oder übersprungen. Es wird höchstens der letzte fällige Termin nachgeholt.

Ein optionaler Vorgänger bildet Auftragsketten: Er muss im aktuellen Termin
bestanden haben, ohne neueren Fehler oder ungeklärten Ausgang. Zyklen und fehlende
Vorgänger werden abgelehnt. Für ältere Apps werden solche Aufträge mit deaktiviertem
altem `enabled`-Feld gespeichert; die aktuelle Version stellt die bewusste Auswahl
aus `dependencyEnabled` wieder her. Optional haben fällige bzw. in fünf Minuten
geplante Backups Vorrang vor neuen Wartungsläufen. Laufende Wartung wird nicht
unterbrochen. Wartegründe stehen an den Aufträgen.

Genau **ein Wartungsauftrag** läuft zur Zeit. Die Planung wartet auf bekannte
TitanOS-Backups, Datei-, Speicher-, Docker-App-, VM- und Plugin-Arbeit. Die Aufnahme neuer
schwerer API-Aufträge und automatischer Backup-/VM-Snapshot-Läufe wird mit dem
Wartungsstart abgestimmt. Normale SMB-/NFS-Zugriffe und laufende Gäste sind keine
pauschal angehaltenen Dienste. Eine RAID-Prüfung prüft zusätzlich den echten
Arrayzustand; SMART adressiert eine eindeutige Seriennummer.

Einstellungen liegen atomar auf dem Stick und während des Betriebs im RAM.
Checkpoints und höchstens 100 Ergebnisse liegen auf dem gewählten Daten-Volume
unter `.titanos/maintenance/`. Ohne dieses Volume gibt es keine ersatzweise
RAM-Ausführung. Nach Prozessabsturz bleibt ein unbekannter Ausgang gesperrt,
bis der Administrator den Zielzustand geprüft und die Kennung bestätigt hat.
Entfernen oder Umwidmen eines gesperrten Auftrags umgeht diese Prüfung nicht.
Freigeben führt den Auftrag nicht sofort aus. Bei fehlendem Daten-Volume lässt
sich eine sonst unveränderte Konfiguration deaktivieren; ungeklärte Zustände
bleiben gesperrt. Abbrechen im Auftragsdialog verwirft auch die dortige Volume-Wahl.
Globale ungespeicherte Einstellungen werden beim Navigieren geschützt. Fehler erscheinen auch als
Administrator-Benachrichtigung ohne Chatinhalte.

Ein SMART-Langtest wird nur als bestanden gemeldet, wenn sein Lauf zuerst
beobachtet und danach ein erfolgreicher Abschluss gemeldet wurde. Nicht
unterstützte oder nicht beobachtbare Tests bleiben Fehler/ungewiss; ein alter
Testbericht gilt nicht als neuer Erfolg. Physische SMART-Geräte sind für diese
Version nicht allgemein abgenommen.

## Geprüfte und koordinierte Backups

**Wiederherstellungsprobe** wählt reguläre Dateien aus dem neuesten restic-Stand,
bis zum eingestellten Umfang (1–1024 MiB und 1–200 Dateien). Sie stellt in ein
neues privates Verzeichnis wieder her und verwendet `restic restore --verify`
gegen genau diesen Stand. Musterzeichen in ausgewählten Pfaden werden maskiert.
Das Ziel bleibt über einen geerbten Verzeichnisdeskriptor an sein Daten-Volume
gebunden; es fällt bei Volume-Verlust nicht auf das RAM-System zurück.
Das Testverzeichnis wird anschließend entfernt. Ergebnis, Umfang, Begrenzungen
und Fehler stehen separat vom letzten Backup-Erfolg. Ein kleiner Probelauf ist
keine Vollprüfung sämtlicher Sicherungsdateien. USB-/SMB-Kopien besitzen kein
restic-Archiv und unterstützen diese Probe nicht.

In einem versionierten Backup lassen sich Anwendungen auswählen:

- **Docker sauber anhalten:** tatsächliche beschreibbare Bind-/Docker-Volumes
  auf eingebundenen Daten-Volumes sichern; erzwungener Kill bzw. OOM wird nicht
  als saubere Sicherung akzeptiert. Weitere laufende Container, die ausgewählte
  Rohdaten beschreiben, blockieren diesen Ablauf auch bei reinen VM-Aufträgen.
  Ein deaktivierter Docker-Dienst wird unter Lease geprüft und nicht vorschnell
  als bereits beendet behandelt. Beschreibbare virtiofs-Host-Ordner anderer
  laufender VMs gehören ebenfalls zur Prüfung.
- **VM sauber anhalten:** unterstützte Gäste ohne durchgereichte Geräte,
  Host-Ordner, Blockplatten oder vorbereitete/externe Abbilder. Der Gast erhält
  ein normales Herunterfahren, höchstens fünf Minuten, keinen erzwungenen Abbruch.
- **PostgreSQL exportieren:** festes `docker exec --user postgres <ID>
  pg_dumpall --no-password`, ohne freie Shell oder Befehlsfelder; bis 8 GiB und
  30 Minuten, Ausgabe direkt auf dem Volume. Der Container braucht diese Werkzeuge
  und passende lokale PostgreSQL-Rechte. Dies ist ein logischer Export; für eine
  gemeinsame Ruhephase aller Datenbanken ist die Stopp-Methode zu wählen.

Stoppen muss in der Auftragskonfiguration ausdrücklich erlaubt sein. Die
ursprünglichen Container-IDs/VM-UUIDs und Laufzustände werden **vor** dem Stopp
in einem privaten Volume-Journal gespeichert. Lebenszyklusänderungen sind
währenddessen und bis zum ausdrücklichen Abschluss einer Wiederherstellung
gesperrt. App-Updates warten ebenfalls auf diesen Abschluss. Nur vorher laufende Originalinstanzen werden wieder
gestartet; Fehler dabei bleiben Fehler des Gesamtauftrags. Ein titand-Absturz
führt nicht zum blinden Neustart oder zur Wiederholung des Backups. Unter
**Werkzeuge › Aufträge** kann der Zustand geprüft und der Wiederanlauf ausdrücklich
beauftragt werden. Die sichtbare Seite aktualisiert neue Vorgänge und lädt
ursprüngliche Mitglieder/Laufzustände/Phasen bei ausdrücklicher Prüfung. Ein Journal lässt sich auch nach manueller Prüfung ohne
Neustart der Anwendungen abschließen.

Gesichert werden die ausgewählten persistenten Daten, VM-Platten/Firmware und
Exporte. Deployment- und NAS-Konfiguration wird zusätzlich über die vorhandene
Option **Systemkonfiguration** aufgenommen. Nicht gewählte Anwendungen werden
nicht stillschweigend als konsistent ausgegeben.

Koordinierte Aufträge speichern ihre Auswahl im optionalen Feld
`coordinatedSelection`; die alten Quellfelder bleiben dafür leer. Dadurch führt
eine ältere App-Version diesen Auftrag nicht versehentlich als gewöhnliche
Live-Kopie aus. Die aktuelle Version stellt die Auswahl beim Laden wieder her.
Andere Aufträge und vorhandene Archivpasswörter bleiben unverändert.

Siehe [restic-Wiederherstellung](https://restic.readthedocs.io/en/stable/050_restore.html).

## Abdeckung und Anwendungswiederherstellung

**Einstellungen › Sicherungen › Sicherungsabdeckung** ordnet ausdrücklich gewählte
Shares und Anwendungen ihren Aufträgen zu. Teilauswahl, manuelle Zeitpläne,
fehlende Quellen/lokale Ziele und getrennte Zeitpunkte für Erfolg, Prüfung und
Probe bleiben sichtbar. Konfigurierte, inzwischen fehlende Quellen bleiben in
der Übersicht. Rohordnerkopien begründen keine koordinierte App-Zuordnung.
Entfernte Ziele werden erst bei tatsächlichem Zugriff geprüft.

**Anwendungswiederherstellung** zeigt Stand/Pfade/Umfang/Ziel vor einer neuen Kopie.
Neue koordinierte Sicherungen nehmen ihr ursprüngliches Anwendungsprotokoll mit
auf, soweit die Auswahlfilter es nicht ausschließen. Die Vorschau liest dieses
mit höchstens 1 MiB/30 Sekunden; fehlt es, wird die aktuelle Zuordnung ausdrücklich
als solche gekennzeichnet. Keine automatische App-Registrierung oder Ausführung
und kein Live-Import der NAS-Konfiguration. Eine getrennte Testinstanz folgt als
manueller fachlicher Prüfschritt nach der Datenkopie.

Ganze, ausgewählte und Anwendungs-Restores verwenden privates Volume-Staging,
einen geerbten Verzeichnisdeskriptor, `--verify --overwrite never` und eine atomare
Veröffentlichung ohne Überschreiben unter einem eindeutigen neuen Namen in
`restored/`. Der veröffentlichte Zielbaum ist zunächst 0700. Bei Volume-Verlust
kein Ausweichen auf das RAM-Overlay. Das Anwendungs-Restore-Ergebnis besitzt einen
eigenen Zeitpunkt/Fehler/Verifiziert-Status, getrennt vom letzten Backup-Erfolg.

## Längere Monitoring-Verläufe

Ab Werk aus. **Werkzeuge › Monitoring** speichert Minutenwerte auf einem gewählten
Daten-Volume: CPU, RAM, Netzwerk, Volume-Belegung sowie physische Datenträger-I/O
und mittlere Latenz abgeschlossener Vorgänge aus Kernelzählern. Es gibt keine
Messung einer SSD-Latenz ohne I/O und keine erfundenen Nullwerte bei fehlenden
Zählern. Rücklauf, Neustart und Beobachtungslücken setzen die Differenzbildung zurück.

Aufbewahrung 7 oder 30 Tage, Speichergrenze 8–256 MiB. Alte Tagesdateien werden
begrenzt entfernt; bei erschöpftem Tagesbudget steht ein Fehler. Unvollständige
letzte Schreibvorgänge werden abgeschnitten, fehlerhafte vollständige Datensätze
nicht übergangen. Anzeige über 24 Stunden, sieben oder 30 Tage, ausgedünnt mit
erhaltenen Beobachtungslücken. Bei zu vielen Lücken fordert die API einen kürzeren
Bereich. Daten liegen unter `.titanos/monitoring/`, niemals zyklisch auf dem Stick.
Dies kann ruhende Datenplatten wecken; die Einstellung erklärt das ausdrücklich.
CSV/JSON exportieren den gewählten Zeitraum einschließlich Lücken; CSV höchstens
250.000 Messzeilen. Textwerte dürfen keine Tabellenformeln auslösen.

Kapazitätsentwicklung verwendet mindestens 24 stündliche Werte aus dem letzten
zusammenhängenden Abschnitt, stabile Kapazität und höchstens zwei Stunden alte
Werte. Große Lücken, Kapazitätswechsel und stark schwankende Daten ergeben keine
Prognose. Die lineare Schätzung zeigt grobe Tage bis 90 % und voll. Bei 90 % oder
höchstens sieben projizierten Tagen bis dahin erscheint auf der geladenen Seite
ein Hinweis. Kein zusätzliches Push-Ereignis, keine Wachstumsgarantie.

## Unabhängige VM-Kopien

**VMs › Weitere Aktionen › VM klonen** verlangt eine ausgeschaltete Ausgangs-VM
und ein eingebundenes Ziel-Volume. Platten werden unabhängig und ohne Backing-Kette
kopiert, mit `qemu-img compare` geprüft; vorhandene NVRAM-/TPM-Dateien werden
mitkopiert. Die Kopie erhält neue UUID/MAC, keinen Autostart und keine übernommenen
exklusiven Kernreservierungen. Gastinterne Namen, Zugangsdaten oder statische
IP-Einstellungen werden dadurch nicht geändert.

Staging und Journal liegen unter `.titanos/vmclones/`. Das Ziel wird ohne
Überschreiben veröffentlicht, die Konfiguration erst danach registriert.
Unterbrochene unveröffentlichte Kopien lassen sich verwerfen, verifizierte bzw.
veröffentlichte Kopien nach Kennungsprüfung abschließen. Eine unbekannte
Veröffentlichung wird nicht durch erneutes Kopieren überdeckt.

## Dateiübertragungen nach Dienstneustart

Kopieren/Verschieben mit Ziel auf einem Daten-Volume speichert je Konto einen
unveränderlichen Quellplan und den Ausführungsstand in einer transaktionalen
bbolt-Datenbank unter `.titanos/transfers/`. Es gibt keine feste Obergrenze für
Dateianzahl, Nutzdatenmenge oder Gesamtgröße des Kopierplans. Verzeichnisse
werden in 128er-Blöcken gelesen und Quellmetadaten in 1024er-Transaktionen
abgelegt. Der gesamte Dateibaum und alle fertigen Ergebnisse werden nicht als
RAM-Maps gehalten. Die Datenbank aktualisiert einzelne Einträge, statt ein
unbegrenzt wachsendes Fortschrittsjournal neu einzulesen.

Der vollständige Umfang und freie Zielplatz werden vor Dateiänderungen geprüft,
nach der Planablage nochmals. Dateisystemblöcke, Verzeichnisse und Auftragsdaten
benötigen zusätzlich Platz; zwischenzeitliche andere Schreibzugriffe können das
Ziel füllen. Ein solcher Fehler wird gemeldet und beendet den Auftrag nicht als
Erfolg. Die Größen-Vorschau der Oberfläche darf nach 15 Sekunden auf die
vollständige Berechnung beim Start verweisen; normale Kopien bleiben startbar.
Die Schutzgrenzen der Ersetzen-/Einzelkonfliktvorschau bleiben erhalten.

20 bekannte abgeschlossene Ergebnisse und höchstens 100 gespeicherte Aufträge
je Konto/Volume begrenzen die Auftragshistorie, nicht die Dateien eines Auftrags.
Unbekannte Veröffentlichungen werden nicht automatisch verdrängt. Andere Ziele
sowie Extraktionsaufträge behalten ihren bisherigen RAM-Ablauf und benötigen
entsprechend Arbeitsspeicher. Kein unendlicher physischer Speicher wird zugesagt.

Nach titand-Neustart muss das Konto den Auftrag ausdrücklich fortsetzen. Aktuelle
Root-Rechte, Zuordnung, Boot-ID, Verzeichnisidentität, unveränderte Quelldateien
und bereits veröffentlichte Ziele werden erneut geprüft. Die noch offene Datei
beginnt bei Byte null; fertige Dateien werden übersprungen. Eine protokollierte
Teilkopie wird nur bei passendem temporärem Namen, Eigentümer und Geräte-/Inode-Paar
entfernt. Veröffentlichung und Quellentfernung stehen vor ihrer Ausführung im
dauerhaften Datenbankstand, danach folgen Verzeichnissynchronisation und die
tatsächlichen Ergebnisse in einer atomaren Transaktion. Ältere JSON-/Append-
Aufträge bleiben importierbar; neue kleine JSON-Deskriptoren sperren die
Wiederaufnahme durch Apps, die das Datenbankformat nicht verstehen.
Ein beim Vorbereiten unterbrochener Plan bleibt sichtbar gesperrt und entfernbar.
Ein unklarer Ausgang bleibt gesperrt. Nicht fertig geschriebene letzte
Protokollzeilen löschen keine vorherige Mutationssperre.

Ein kompletter NAS-Neustart erlaubt wegen wechselnder Geräteidentität keine
ungeprüfte Wiederaufnahme. Quelle/Ziel sind dann zu prüfen und ein neuer Auftrag
anzulegen. **Auftragseintrag entfernen** funktioniert mit einem Klick ohne Eingabe einer
internen Kennung. **Alle beendeten Aufträge entfernen** entfernt sämtliche eigenen
beendeten Einträge, einschließlich der gespeicherten Pläne/Journale/Datenbanken und der
RAM-Aufträge; laufende oder gerade abschließende Aufträge bleiben erhalten.
Die Liste zeigt alle verfügbaren eigenen Einträge statt nur fünf beendete.
Kopierte Dateien und verbliebene Teilkopien bleiben erhalten. Vor dem Entfernen
der Ereignisse wird der Plan gelöscht und das Verzeichnis synchronisiert, damit
ein Abbruch keinen wiederholbaren Plan ohne Veröffentlichungssperren hinterlässt.
Fehler behalten den betreffenden Eintrag und bieten einen erneuten Versuch. Browser-Uploads und Rückgängig-Verlauf
haben weiterhin ihre eigenen, in der Dateimanager-Dokumentation beschriebenen Grenzen.

## Prüfungen

- `make test test-release`, produktiver Build und gezielte Go-Race-Tests.
- `scripts/test-ui-workflows.mjs`: Shares, Formularerhalt, Fehler, Kontentrennung,
  Docker-Verknüpfungen/Filter/veralteter Stand und Befehlspalette.
- `scripts/test-ui-operations.mjs`: Wartung, Monitoring/Lücken, Importfehler,
  RAID-Bestätigung, Snapshot-Dateiauswahl, Klonen, Rollen, Graphit/Hell, 1440/390 px.
- `backend/tests/smoke_workflows.py <Basis-ZIP> <titand>`: isolierte UEFI-QEMU-VM
  mit sieben frisch angelegten virtuellen Platten. Kein Host-Blockgerät wird
  angehängt. Der lokale Build wird nur im RAM der VM gestartet. Prüft echten
  Import, RAID-Umbau, Snapshots, Wartung, Monitoring, VM-Kopie, restic-Probe,
  Anwendungssicherung und unterbrochene/fortgesetzte Vorgänge.

Der genaue Release-Prüfstand und verbleibende Hardware-/Stromausfallgrenzen stehen
in [v0.17.0](releases/v0.17.0.md). Diese Prüfungen ersetzen keine Abnahme aller
NAS-Platten, Controller, PostgreSQL-Images oder Gastsysteme.

### Entwicklungsprüfung nach 0.21.0

Isolierte Go-Regressionen prüfen die Vorbereitung und das erneute Laden eines
Plans mit 85.177 echten leeren Testdateien, über einer Million synthetischer
Datenbankeinträge und einer über 2 GiB großen (größtenteils dünn belegten)
Datenbankdatei sowie ein altes Ereignisjournal über 128 MiB,
Einzel-/Sammelentfernung einschließlich Dienstneustart, Kontentrennung und den
Erhalt von Originalen, Kopien und Teilkopien. Das ist keine vollständige
318-GB-Übertragung und kein Stromausfalltest. `scripts/test-ui-drive-transfers.mjs`
prüft Einzelklick ohne Kennung, acht beendete Einträge, Erhalt laufender Arbeit,
Neuladen und Fehler/Wiederholen auf Desktop/Smartphone sowie auf Englisch.
Neue Datenbankpläne sind ausdrücklich gegen die Wiederaufnahme durch ältere
Apps gesperrt; unterbrochene/fehlende Datenbanken bleiben gesperrt statt neu
und leer erzeugt zu werden. Keine Vollübertragung von einer Million Dateien
oder Messung ihres Durchsatzes wird aus dem Metadaten-Test abgeleitet.

`scripts/test-ui-large-copy.mjs` prüft auf Desktop und Smartphone, dass eine
abgelaufene Größen-/Zielvorschau normales Kopieren zulässt, der Server einen
Speicherplatzfehler weiterhin melden kann und der Dialog zum Wiederholen offen
bleibt. `scripts/test-ui-copy-confirmation.mjs` prüft weiterhin die ausdrückliche
Ersetzen-Bestätigung einschließlich Fehlern und veralteten Antworten.
Quell-/Zielalias und das Kopieren in den eigenen Ordner sind gesondert geprüft;
Kopien dürfen beim blockweisen Lesen keine eigenen Ausgabedateien erneut finden.
