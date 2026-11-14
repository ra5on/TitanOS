<p align="center"><img src="docs/readme/hero.svg" alt="TitanOS – Deine Daten. Deine Apps. Dein Server." width="100%"></p>

<p align="center">
  <a href="https://github.com/ra5on/TitanOS/releases/latest"><img src="docs/brand/download-stable.svg" alt="Aktuelle TitanOS-Version herunterladen" width="350" height="54"></a>
</p>

<p align="center">
  <a href="#installation">Installation</a> ·
  <a href="#funktionen">Funktionen</a> ·
  <a href="#dokumentation">Dokumentation</a> ·
  <a href="LICENSE.md">Fair Source</a>
</p>

<br>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/readme/h-intro-dark.svg">
    <img src="docs/readme/h-intro-light.svg" alt="Sieht aus wie ein Desktop. Ist ein ganzer Server." width="760">
  </picture>
</p>

<p align="center">Fenster, Dock, Arbeitsplatz. Im Browser, auf jedem Gerät.<br>Startet vom USB-Stick. Läuft komplett im Arbeitsspeicher.</p>

[![TitanOS: Arbeitsplatz mit Programmsymbolen und Docker-Apps, Widgets für Zustand, CPU/RAM, Volumes und Docker-Apps, oben die Leiste mit dem Knopf „Menü“ und dem Suchfeld, unten das Dock](docs/screenshots/current/arbeitsplatz.png)](docs/screenshots/current/arbeitsplatz.png)

<table>
<tr>
<td width="58%"><a href="docs/screenshots/current/anmeldeseite.png"><img src="docs/screenshots/current/anmeldeseite.png" alt="Anmeldeseite mit bewegtem Nordlicht, einer Reihe Kontobilder und dem Passwortfeld"></a></td>
<td><sub>ANMELDUNG</sub><h3>Nordlicht inklusive.</h3>Ein bewegter Himmel. Dein Bild im Kreis. Ein Feld fürs Passwort – und du bist da.<br><br><sub>Passkey · Zwei-Faktor · Bewegung abschaltbar</sub></td>
</tr>
<tr>
<td><sub>DATEIEN</sub><h3>Vertraut vom ersten Klick.</h3>Adresszeile, Befehlsleiste, Mehrfachauswahl. Suche bis in den Inhalt deiner Dokumente. Word, Excel und PDF öffnen direkt im Fenster.<br><br><sub>SMB · NFS · WebDAV · USB</sub></td>
<td width="58%"><a href="docs/screenshots/current/dateien.png"><img src="docs/screenshots/current/dateien.png" alt="Dateien: Seitenleiste mit Freigaben, Adresszeile, Befehlsleiste, Dateiliste mit zwei markierten Dateien und Statuszeile"></a></td>
</tr>
<tr>
<td width="58%"><a href="docs/screenshots/current/docker.png"><img src="docs/screenshots/current/docker.png" alt="Programm Docker: Tabelle der Apps mit Zustand, CPU, RAM und Ports, rechts die Einzelheiten der gewählten App, unten die Statusleiste"></a></td>
<td><sub>APPS</sub><h3>Installiert. Läuft.</h3>Immich, Jellyfin, Home Assistant und mehr aus dem App-Store. Jede App im eigenen Fenster. Auch unterwegs.<br><br><sub>Compose · Updates · Startreihenfolge</sub></td>
</tr>
<tr>
<td><sub>VIRTUELLE MASCHINEN</sub><h3>Ein Rechner im Rechner.</h3>Windows, Linux, Home Assistant OS. Bildschirm im Browser, Snapshots, durchgereichte Grafikkarte.<br><br><sub>KVM/QEMU · GPU · USB</sub></td>
<td width="58%"><a href="docs/screenshots/current/vms.png"><img src="docs/screenshots/current/vms.png" alt="Programm Virtuelle Maschinen: zwei VMs in der Seitenleiste, Übersicht der laufenden VM „Home Assistant OS“ mit Auslastung, Netzwerk, Hardware und Snapshots"></a></td>
</tr>
<tr>
<td width="58%"><a href="docs/screenshots/current/nvr.png"><img src="docs/screenshots/current/nvr.png" alt="Programm NVR, Seite Live: vier Kamerakacheln, eine davon nicht erreichbar, rechts die letzten Ereignisse; die Kamerabilder sind nachgebildet"></a></td>
<td><sub>KAMERAS</sub><h3>Dein eigener Rekorder.</h3>Netzwerkkameras aufnehmen. Personen, Fahrzeuge und Tiere erkennen. Live, Aufnahmen und Ereignisse in einem Fenster.<br><br><sub>RTSP · ONVIF · Frigate · ab Werk aus</sub></td>
</tr>
</table>

<br>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/readme/h-alles-dark.svg">
    <img src="docs/readme/h-alles-light.svg" alt="Alles drin. Auch das, was man nicht sieht." width="760">
  </picture>
</p>

<table>
<tr>
<td width="33%"><b>Speicher</b><br>RAID 0, 1, 5, 6, 10. Btrfs-Spiegel. SSD-Cache. Verschlüsselung.</td>
<td width="33%"><b>Sicherheit</b><br>Signierte Updates mit Rückfall. Firewall. Zwei-Faktor. Passkeys.</td>
<td width="33%"><b>KI-Agent</b><br>Dein eigenes Ollama. Feste Rechte. MCP für Claude.</td>
</tr>
<tr>
<td><b>Sicherung</b><br>Versioniert. Auf USB, SMB oder ein zweites NAS. Mit Prüfung.</td>
<td><b>Fernzugriff</b><br>Tailscale, NetBird, WireGuard. Let's Encrypt.</td>
<td><b>Handy</b><br>App-Raster. Jedes Programm im Vollbild. Als App installierbar.</td>
</tr>
<tr>
<td><b>Office</b><br>T-Word, T-Exel, T-PDF und elf weitere Programme. Direkt im Fenster.</td>
<td><b>Kameras</b><br>Aufnahme und Erkennung mit Frigate. Rechte je Kamera.</td>
<td><b>Smart Home</b><br>Home Assistant per MQTT. Speicher für HA-Backups.</td>
</tr>
</table>

<p align="center"><sub>Alle Bilder zeigen die echte Oberfläche mit Beispieldaten. Keine Mockups. Die Kamerabilder und das Bild der Fernansicht sind nachgebildet. <a href="docs/screenshots/README.md">Aufnahmestand</a>.</sub></p>

TitanOS basiert auf **Debian 13** und läuft auf jedem 64-Bit-PC (x86-64) und startet von einem USB-Stick, normal per UEFI. Ältere Rechner mit 32-Bit-UEFI-Firmware oder Legacy-BIOS starten ebenfalls; das System bleibt auch dort 64-Bit. Daten, Apps und VMs liegen auf eigenen Volumes, die Konfiguration bleibt auf dem Stick. Die Bedienidee ist von Unraid inspiriert. TitanOS setzt auf klassisches Linux-Software-RAID und ist ein eigenständiges Projekt ohne Verbindung zu Lime Technology.

## Funktionen

Alles im Einzelnen – zum Aufklappen.

<details>
<summary><b>Arbeitsplatz, Fenster und Dock</b></summary>

- **Jeder Bereich ein Programm:** Dateien, Freigaben, Speicher, Docker, Virtuelle Maschinen, Systemcontainer, App-Center, Sicherung, NVR, Monitor, Protokolle, Aufgabenplaner, Terminal, Benutzer, Sicherheit, Mein Konto und Systemsteuerung öffnen als eigene Fenster, der KI-Agent ebenfalls, sobald er eingeschaltet ist. Jedes Fenster hat links seine Seitenleiste mit den Seiten des Programms. In schmalen Fenstern klappt sie von selbst zu ihren Symbolen ein. Die Titelleiste nennt Programm und Seite, die Aktionen stehen in einer Werkzeugleiste darunter.
- **Aufbau wie im Explorer von Windows 11:** Einstellungen stehen als Zeilen unter einer schlichten Überschrift, mit Symbol, Name, Erklärung und dem Wert rechts. Was Unterpunkte hat, klappt in der Zeile auf und zeigt auch zugeklappt seinen Stand. Listen haben unten eine Statusleiste mit den Kennzahlen.
- **Fenster wie am PC:** frei verschieben, an allen Kanten und Ecken vergrößern, an den Rand ziehen für die halbe Seite, nach oben für Vollbild, in die Ecke für ein Viertel – mit Vorschau. Knöpfe rechts: in eigenem Tab öffnen, minimieren, maximieren, schließen. Fensterwechsler und Tastenkürzel (Alt + Q, Alt + Pfeile, Alt + W, Strg + Leertaste). Hat eine Seite ungespeicherte Änderungen oder lädt sie noch hoch, fragt das Fenster vor dem Schließen. Die Browser-Adresse zeigt das Fenster vorn, Lesezeichen öffnen das passende Programm. Fenster-Anordnungen lassen sich speichern.
- **Ein Menü:** Der Knopf „Menü“ sitzt oben links, daneben ein kleines Suchfeld; derselbe Knopf steht vorn im Dock. Das Menü zeigt Filter (Alle, TitanOS, Docker-Apps, VMs, Zuletzt), die Programme von A bis Z oder in eigener Reihenfolge (ziehen, „Anordnen“ mit Pfeilen oder Strg + Umschalt + Pfeil) und unten Konto, Systemsteuerung, Updates, App-Center, Sperren, Abmelden, Neustart und Herunterfahren. Die Suche findet Programme, ihre Seiten und Schnellaktionen, etwa einen Container starten, eine Sicherung anstoßen oder das NAS neu starten. Programme lassen sich aus dem Menü auf den Arbeitsplatz oder ins Dock ziehen.
- **Leiste oben:** Menü und Suche, die Menüleiste des Programms vorn (z. B. Ablage, Bearbeiten, Ansicht, Fenster, Hilfe), im Cluster der Umschalter zwischen den Knoten; rechts laufende Aufgaben, die Glocke mit den Mitteilungen, CPU und RAM (abschaltbar) und das Datum. Größe Klein, Mittel oder Groß per Rechtsklick.
- **Arbeitsplatz:** Programmsymbole frei auf einem unsichtbaren Raster, „Automatisch anordnen“, drei Größen, Ordner, Abzeichen für Updates, Warnungen und „läuft/gestoppt“. Die Symbole behalten ihre Plätze, auch wenn das Browserfenster kleiner und wieder größer wird. Rechtsklick-Menüs auf Symbole, Arbeitsplatz, Widgets, Leiste und Dock.
- **Widgets** frei platzierbar und in der Größe änderbar: Zustand, CPU/RAM, Netzwerk, Volumes, Docker-Apps, VMs, Sicherungen, Temperaturen, Uhr, Home Assistant, Aufgaben, iGPU, NPU, Grafikkarte und Kamera. Grafik-Widgets gibt es nur für Geräte, die das NAS hat; ein Wert, den ein Gerät nicht meldet, bekommt keine Kachel. Widgets lassen sich einzeln entfernen oder alle ausblenden. Sie lesen nur, was die Oberfläche ohnehin abfragt, und wecken keine Platten.
- **Dock** (ab Werk an): angeheftete Programme und offene Fenster, Vergrößern beim Überfahren, umsortieren per Ziehen. Größe, Deckkraft, Vergrößern und Position (unten, links, rechts) im Rechtsklick „Dock bearbeiten“; dazu Umrandung und die angehefteten Programme unter Systemsteuerung › System › Darstellung › Dock mit Vorschau.
- **Sperrbildschirm und Anmeldung:** Sperren im Menü oder automatisch nach einstellbarer Zeit; Entsperren mit Passwort (und 2FA) oder Passkey. Die Anmeldeseite zeigt das bewegte Nordlicht, Kontobilder (eigenes Foto oder mitgeliefertes Bild) und auf Wunsch einen Übergang in den Arbeitsplatz.
- **Dateien hineinziehen:** Dateien vom PC auf den Arbeitsplatz ziehen öffnet „Dateien“ und lädt sie in den offenen Ordner hoch.
- **Am Handy:** Startbildschirm mit dem Knopf „Menü“, Suche und App-Raster; jedes Programm und jede Docker-App öffnet im Vollbild mit „Zurück“. Ränder für Notch und Home-Leiste, keine vergrößernden Eingabefelder in Safari. TitanOS lässt sich als App installieren; unter Android nimmt sie über „Teilen › TitanOS“ Fotos und Dateien für das NAS an.
- **Klare Rückfragen statt Eintippen:** Löschen, Überschreiben, Plattentests, Zwei-Faktor-Zurücksetzen, SSH-Schlüssel und Zertifikate entfernen bestätigt man in einem Dialog, der genau nennt, was passiert. Namen müssen nirgends eingetippt werden; „Abbrechen“ hat den Fokus, Enter löst nie die gefährliche Aktion aus. Zerstörende Knöpfe stehen gesammelt in einer abgesetzten „Gefahrenzone“ am Ende. Fachbegriffe erklärt ein „?“ direkt daneben.
- **Kein abgeschnittener Text:** Auf den Seiten von TitanOS brechen lange Namen und Texte um, statt abgeschnitten zu werden.

[![Menü mit Filtern, Programmen von A bis Z, Docker-Apps und VM-Bildschirmen, unten Konto, Systemsteuerung, Updates, App-Center, Sperren, Abmelden, Neustart und Herunterfahren; oben daneben das Suchfeld der Leiste](docs/screenshots/current/hauptmenue.png)](docs/screenshots/current/hauptmenue.png)

[Dock bearbeiten](docs/screenshots/current/dock-bearbeiten.png) · [Beschreibung des Desktop-Modus](docs/DESKTOP.md) · [Sperrbildschirm](docs/SESSION-LOCK.md) · [TitanOS als Handy-App](docs/PWA.md)

</details>

<details>
<summary><b>Speicher und Laufwerke</b></summary>

- **Klassisches RAID:** mdadm mit RAID 0, 1, 5, 6 und 10; Dateisysteme ext4 und XFS. Volumes anlegen, Zustand und Belegung sehen, Mitglieder ersetzen und Wiederaufbau oder Konsistenzprüfung verfolgen.
- **Btrfs-Volumes:** eine Platte, Spiegel (RAID 1) oder drei Kopien (RAID1C3), auch aus Platten unterschiedlicher Größe. Platten später hinzufügen oder entfernen und das Profil im laufenden Betrieb ändern. Schreibgeschützte Snapshots, Scrub und Plattenersatz. Fehlt beim Start ein Mitglied eines Volumes mit mindestens zwei Kopien, wartet TitanOS drei Minuten auf langsam anlaufende Platten, bevor es das Volume ohne Redundanz einbindet und das meldet; ein Volume ohne zweite Kopie wartet, bis alle Platten da sind. Snapshot-Zeitpläne mit Aufbewahrung sowie Wiederherstellung ganzer Snapshots, Ordner oder einzelner Dateien.
- **Vorhandenen Speicher übernehmen:** lesende Vorschau unterstützter mdadm- und Btrfs-Volumes, danach Registrierung ohne Formatierung. RAID5/6 lässt sich um eine Platte erweitern; dafür ist ein separates Rettungs-Volume erforderlich. Hinzufügen, Erweitern und Ersetzen prüfen die gewählte Platte erneut und lassen fremde Arrays, LVM und eingebundene Dateisysteme auf anderen Platten unangetastet.
- **SSD-Cache:** eine SSD vor ein bestehendes RAID setzen, ohne Neuformatieren. Wahlweise nur als Lesecache (sicher, falls die SSD ausfällt) oder als Lese- und Schreibcache; ab Werk aus. [Einzelheiten](docs/SSD-CACHE.md).
- **Verschlüsselung (optional):** ein Volume beim Anlegen mit LUKS2 verschlüsseln, ab Werk aus. Nach jedem Start bleibt es gesperrt, bis jemand das Passwort in der Weboberfläche eingibt; erst dann starten Freigaben, Apps, VMs und Container darauf. Passwörter ändern oder ergänzen, sperren und Header-Sicherung herunterladen auf der Speicher-Seite. [Einzelheiten](docs/ENCRYPTION.md).
- **Platte tauschen:** ein Assistent führt durch den Tausch einer ausgefallenen, fehlenden oder alternden Platte; eine noch arbeitende Platte eines mdadm-Volumes wird im laufenden Betrieb kopiert, ohne dass das Volume seine Redundanz verliert. „Platte finden“ lässt die Aktivitätsanzeige flackern. [Einzelheiten](docs/DISK-SWAP.md).
- **Plattenzustand und Vorbereitung:** SMART, Temperatur, Betriebsstunden, Fehler und Verschleiß; neue Laufwerke vollständig lesen oder überschreiben und zurücklesen, anschließend einen langen Selbsttest ausführen.
- **Ruhemodus der Festplatten:** als Standard und abweichend je Volume oder je Platte, auch unter Systemsteuerung › Hardware und Energie › Energie. Bei jeder Platte im Ruhemodus steht, wann und wodurch sie zuletzt aufgeweckt wurde, dazu die letzten 20 Weckvorgänge. Sicher zugeordnet wird, was TitanOS selbst tut, und Zugriffe über Dateien und WebDAV; SMB gilt nur als „wahrscheinlich“, NFS und laufende Container bleiben „unbekannt“. [Einzelheiten](docs/DISK-SLEEP.md).
- **Verbrauch verstehen:** kompakte Volume- und Laufwerkslisten mit Details auf Auswahl, Speicheranalyse auf Anforderung und optionale Kapazitätsprognosen aus gültigen Monitoring-Verläufen.

[Btrfs](docs/BTRFS.md) · [Übernahme, Erweiterung und Speicheranalyse](docs/WORKFLOWS.md)

</details>

<details>
<summary><b>Freigaben und Konten</b></summary>

- **SMB:** Rechte je Konto, lesender Gastzugriff, Papierkorb mit Aufbewahrung und Time-Machine-Ziele. **NFS:** getrennt von SMB abschalten, nur lesend oder lesend/schreibend für ausgewählte Clients freigeben.
- **WebDAV (optional):** Freigaben als Netzlaufwerk über HTTPS, etwa in der Dateien-App des iPhones, im Finder oder im Windows-Explorer, auch von unterwegs. Eigenes WebDAV-Passwort je Konto, dieselben Rechte wie bei SMB. [Einzelheiten](docs/WEBDAV.md).
- **Übersichtliche Freigaben:** suchen, filtern und sortieren; Details, Verbindungsadressen und Dateimanager direkt an der Freigabe öffnen.
- **Vorgängerversionen und Ransomware-Schutz:** Btrfs-Snapshots erscheinen in Windows unter „Vorgängerversionen“. Benennt ein Konto plötzlich viele Dateien wie Ransomware um, wird die Freigabe sofort schreibgeschützt und ein Snapshot angelegt. Geplante Snapshots lassen sich für eine Frist gegen Löschen sperren. [Einzelheiten](docs/SHARE-PROTECTION.md).
- **Besitzer, Administratoren und Benutzer:** das erste Besitzerkonto hat einen frei gewählten Namen. Benutzer sehen ihre freigegebenen Dateien und die Apps und Kameras, die man ihnen freigibt; Systemverwaltung, Docker und VMs bleiben den Verwaltungsrollen vorbehalten.
- **Programm Benutzer:** links die Konten, rechts das gewählte Konto mit aufklappbaren Zeilen: Rolle, Apps (ein Haken je App, „Neu installierte Apps automatisch freigeben“), Freigaben, Anmeldung und 2FA sowie die Kameras des NVR.
- **Anmeldung und Sicherheit:** Passwortschutz gegen Durchprobieren, TOTP mit Wiederherstellungscodes, Passkeys, Übersicht angemeldeter Geräte und Abmelden einzelner oder aller anderen Sitzungen. Konten lassen sich sperren, ohne sie zu löschen; das wirkt auch für SMB, SSH und API-Tokens. [Konten sperren](docs/ACCOUNT-LOCK.md) · [Passkeys](docs/PASSKEYS.md).

</details>

<details>
<summary><b>Dateien direkt im Browser verwalten</b></summary>

- **Wie der Explorer:** Adresszeile mit Pfad und Suche, Befehlsleiste, die sich bei schmalen Fenstern selbst zusammenschiebt, ein Klick wählt eine Datei, Häkchen erst bei Mehrfachauswahl, Statuszeile mit Auswahl und freiem Platz.
- **Persönlicher Arbeitsplatz:** Ordnerbaum, benannte und sortierbare Favoriten, Ansichten und Sortierung je Ordner, zwei Ordner nebeneinander und eine Pfadeingabe in den Brotkrumen.
- **Dateien und Ordner bearbeiten:** erstellen, hochladen, herunterladen, als ZIP laden, kopieren, verschieben, umbenennen und löschen. Ziehen zwischen Ansichten, Übertragungsvorschau und Entscheidungen bei Namenskonflikten.
- **Öffnen mit:** Ein Doppelklick öffnet die Standard-App des Dateityps, etwa T-Word, T-Exel oder T-PDF. Rechtsklick › „Öffnen mit“ bietet die passenden Programme, den Texteditor und Docker-Apps, die solche Dateien öffnen (z. B. `.3mf` in Bambu Studio). Die Standard-Apps legt jedes Konto unter Dateien › Einstellungen fest.
- **Schnellansicht:** Die Leertaste zeigt die Vorschau der markierten Datei, die Pfeiltasten blättern durch den Ordner. Word-, Excel-, PowerPoint- und OpenDocument-Dateien zeigen das Vorschaubild, das ihr Programm beim Speichern mitgegeben hat.
- **Von Link herunterladen:** eine HTTP(S)-Adresse, etwa ein ISO-Abbild, lädt das NAS selbst als Hintergrundauftrag in den geöffneten Ordner: optionale SHA-256-Prüfung, Fortsetzen nach Verbindungsabbruch, nie Überschreiben. Benutzerkonten erreichen dabei nur Adressen im Internet.
- **Vorschau und Texteditor:** Bilder, PDF, Audio und Video ansehen; UTF-8-Textdateien bis 512 KiB bearbeiten, einschließlich Markdown, JSON und YAML. Vorige Fassung vergleichen und wiederherstellen.
- **Vorschaubilder und Fotoansicht:** das NAS erzeugt Miniaturen für JPEG, PNG, GIF, WebP, PDF (erste Seite) und Videos (mit ffmpeg) und legt sie auf dem jeweiligen Daten-Volume ab. Die Fotoansicht zeigt einen Ordner als Raster nach Aufnahmedatum (EXIF) und im Großbild mit Pfeiltasten und Wischen.
- **Volltextsuche (optional):** ein ab Werk ausgeschalteter Index je Volume findet Wörter in PDF-, Word-, Excel-, PowerPoint-, OpenDocument- und Textdateien, auf Wunsch auch in gescannten PDFs (OCR). Die ersten 25 Treffer zeigen den Textausschnitt um das gesuchte Wort. Der Index arbeitet nur, während das Volume ohnehin benutzt wird oder zu einer gewählten Uhrzeit, und zeigt jedem Konto nur Treffer in seinen Freigaben.
- **Werkzeuge:** Unterordnersuche, Sammelumbenennen, ZIP-Inhalte, Ordnervergleich, gezielte Übernahme von Unterschieden und Duplikatsuche. Die Speicher-Analyse zeigt Ordner als Flächen nach belegtem Platz, große und alte Dateien und Doppelte; markierte Dateien gehen nach einer Rückfrage in den Papierkorb.
- **Papierkorb:** Originalpfad, Datum und Belegung sehen, filtern und vor der Wiederherstellung prüfen. Unterstützte Aktionen lassen sich innerhalb der Dienstlaufzeit rückgängig machen.
- **Übertragungen steuern:** Warteschlange, Pause, Priorität, Geschwindigkeitslimit, Fortschritt und Restzeitschätzung. Dauerhaft gespeicherte Kopier-/Verschiebeaufträge auf Daten-Volumes lassen sich nach einem Dienstneustart geprüft fortsetzen. Unfertige Uploads können nach erneuter Dateiauswahl fortgesetzt werden.
- **Große Kopieraufträge:** keine feste Datei- oder GB-Grenze pro Auftrag. Auf Daten-Volumes werden Plan und Wiederaufnahmestand stückweise in einer Datenbank gespeichert; Dateien und Metadaten benötigen ausreichend Zielplatz.
- **Weitere Datenträger:** nicht verwaltete USB-, SATA-, SAS- und NVMe-Dateisysteme ausdrücklich einbinden; FAT32, exFAT, NTFS, ext2/3/4, XFS und Btrfs. Bei NTFS steht zusätzlich ein lesender Ausweichweg bereit. Jede Aktion berücksichtigt Kontorechte und Schreibschutz.
- **Laufwerke formatieren und partitionieren:** ganze Platten außerhalb der Volumes neu formatieren oder im Partitionseditor Tabellen anlegen, Partitionen anlegen, löschen, formatieren, umbenennen und in direkt folgenden freien Platz vergrößern – mit erneuter Prüfung der Platte direkt vor dem Schreiben. [Partitionseditor](docs/PARTITIONS.md).

[Texteditor und USB-Dateisysteme](docs/FILE-EDITOR.md) · [Vorschaubilder und Volltextsuche](docs/FILE-SEARCH.md) · [Öffnen mit Docker-Apps](docs/OPEN-WITH-DOCKER.md) · [Partitionseditor](docs/PARTITIONS.md) · [Von Link herunterladen](docs/FILE-DOWNLOADS.md) · [Kopieren und Verschieben](docs/FILE-COPY.md) · [RAM und Uploads](docs/MEMORY.md) · [Lange Windows-SMB-Pfade](docs/WINDOWS-SMB.md)

</details>

<details>
<summary><b>Office und Programme im Browser</b></summary>

- **TitanOS-Office:** T-Word (Texte), T-Exel (Tabellen) und T-PDF gehören zur Grundausstattung; die Ersteinrichtung schlägt sie vor. Im App-Center stehen unter „Programme für den Browser“ dazu T-Deck, T-CAD, T-Fig, T-Film, T-Light, T-Vector, T-Design, T-Effect, T-Sound, T-Texture und T-Material. Es sind eigene Fassungen der Craft-Programme von storytold, gebaut im Repository [TitanOS-Office](https://github.com/ra5on/TitanOS-Office).
- **Direkt im Fenster:** Die Programme laufen in deinem Browser, ohne Docker und ohne Last auf dem NAS. Sie kommen von der Adresse von TitanOS und gehen deshalb auch über Cloudflare Access ohne Zusatzregel.
- **Öffnen und Speichern im NAS:** „Datei › Öffnen“ zeigt den Dateidialog von TitanOS mit deinen Freigaben, „Zuletzt geöffnet“ und „Von diesem Gerät …“. Speichern schreibt in die geöffnete Datei zurück, die vorige Fassung bleibt als `.prev`. Hat jemand anderes die Datei inzwischen geändert, fragt TitanOS nach.
- **Sprechen nur mit dem NAS:** Die Programme laden nichts von fremden Servern und schicken nichts dorthin. Nachladbare KI-Modelle der Originale und die Online-Zusammenarbeit in T-Fig gibt es deshalb nicht.
- **Ablage und Umstieg:** Die Programme liegen auf einem Daten-Volume; „Speicherort der Programme“ im App-Center zeigt es und zieht alle Programme mit „Verschieben …“ um. Ein noch installiertes Craft-Original ersetzt TitanOS selbst durch seine T-Fassung. Neue Versionen kommen mit TitanOS-Updates.
- **Grenzen:** Dialoge, Einstellungen und Dateiansicht von T-Sound und T-Deck sind noch englisch. In T-Film kann ein Reiter schmaler sein als sein Name, T-PDF kürzt die Namen in seiner Werkzeugliste.

[Web-Apps im Fenster](docs/WEBAPPS.md)

</details>

<details>
<summary><b>Docker und App-Store</b></summary>

- **Start nach dem Hochfahren:** Apps und VMs starten erst, wenn Volumes und Freigaben bereit sind, auf Wunsch in fester Reihenfolge mit Wartezeit je Eintrag (Freigaben › Dienste).
- **Apps als Tabelle:** je App eine Zeile mit Symbol, Zustand, CPU, RAM, Ports und Update-Hinweis; Suche und Filter (Alle, Laufen, Gestoppt, Updates). Ein Klick wählt die App und zeigt rechts ihre Einzelheiten, ein Doppelklick öffnet ihre Seite. Apps mit mehreren Containern klappen zu ihren Containern auf. Die Statusleiste unten nennt Anzahl, Auslastung und verfügbare Updates.
- **Container und Compose-Stacks:** Images und Tags einzeln je Dienst bearbeiten, Store-Vorgaben und Digests erhalten; Start, Stopp, Neustart, Logs und Browserkonsole; geprüfte Updatezustände.
- **Images, Volumes, Netzwerke, Aufräumen:** Images und Volumes mit Größe und „benutzt von“, ungenutzte einzeln oder gesammelt löschen; eigene Bridge-Netze anlegen; Build-Cache leeren und sehen, wie viel Platz frei würde. Was eine eingerichtete App oder TitanOS selbst braucht, bleibt geschützt. [Einzelheiten](docs/DOCKER-CLEANUP.md).
- **Docker-Apps im Fenster (Fernansicht):** Jede App mit Weboberfläche ist ein eigenes Programm. Ihr Fenster zeigt die App in der Fernansicht: TitanOS öffnet sie auf dem NAS in einem abgeschotteten Browser-Container und überträgt Bild und Ton über die TitanOS-Adresse – im Heimnetz, per VPN, über einen Cloudflare-Tunnel und am Handy. Ab Werk schickt die Ansicht nur geänderte Bereiche und schaltet bei viel Bewegung auf Video. Bildrate (30 oder 60 je Sekunde), Qualität und Grafikkarte (VA-API für Intel/AMD, NVENC mit dem NVIDIA-Plugin) unter Systemsteuerung › System › Fernansicht. Anmeldungen in den Apps bleiben je Konto und App erhalten; Lautstärke, Datei öffnen, Zwischenablage und Qualität im ⋯-Menü des Fensters. Das Abbild der Fernansicht ist eine eigene signierte Release-Datei und wird bei der ersten Ansicht geladen. „In eigenem Tab öffnen“ bleibt als Ausweg. [Einzelheiten und Grenzen](docs/FERNANSICHT.md).
- **Apps mit eigenem Desktop direkt:** Thunderbird, Firefox, JDownloader, Bambu Studio und KasmVNC-Abbilder zeigen ihren eigenen Bildstrom direkt im Fenster. Ist das nicht erreichbar (etwa über Cloudflare), schaltet das Fenster von selbst auf die Fernansicht um.
- **Wer welche App nutzen darf:** je Konto im Programm Benutzer oder unter „Wer darf die App nutzen“ auf der Seite der App.
- **Anwendungen schnell öffnen:** eigene App-Icons und Links zur WebUI, zur Tailscale-Adresse und zur öffentlichen Adresse, direkt im Menü mit Zieladresse. Bei Stacks bestimmt ein Hauptdienst die gemeinsamen Links.
- **Automatische App-Updates (optional):** gewählte Apps holen neue Images in einem Zeitfenster; läuft eine App danach nicht richtig, kehrt TitanOS zu den vorigen Images zurück. [Einzelheiten](docs/APP-AUTO-UPDATE.md).
- **Eigener HTTPS-Name je App:** auf Wunsch erreichst du eine App unter `https://immich.<nas>.local`, unter einer eigenen Domain oder mit einem Klick als eigenes Gerät im Tailnet unter `https://<app>.<tailnet>.ts.net` mit gültigem Zertifikat. TitanOS leitet selbst weiter (auch WebSockets), gibt die Namen per mDNS im Heimnetz bekannt und hält die TitanOS-Anmeldung von den Apps fern; wahlweise nur aus Heimnetz, Tailscale und NetBird erreichbar. Alle Namen und die gemeinsame Domain stehen unter Docker › HTTPS-Namen für Apps.
- **Eigene LAN-Adressen:** ipvlan L2 oder macvlan mit reserviertem IPv4-Pool, optional fester Adresse pro Dienst und eigenem NAS-Endpunkt; private Dienste eines Stacks bleiben privat. [Einrichtung](docs/NETWORKING.md#container-mit-eigener-lan-adresse).
- **Installieren und Bearbeiten in einem Formular:** eine Spalte mit Bezeichnung, Feld und Hinweis, Basis/Erweitert, Ordner-Browser, belegte Ports mit freiem Vorschlag, GPU-Auswahl mit Zustand, CPU-Kerne und RAM-Limit, HTTPS-Name gleich beim Installieren und ein Live-Protokoll der Installation. Zugangsdaten, die du bei der Installation eingibst, stehen verdeckt im Formular und in `.env` statt in der Compose-Datei. Alternativ Compose direkt bearbeiten. Die Docker-Daten liegen auf einem ausgewählten Daten-Volume.
- **App-Store mit eigenen Quellen:** offizieller [TitanOS-App-Katalog](https://github.com/ra5on/TitanOS-Apps), zusätzliche Repositories, Archive und Compose-Dateien; Unterstützung für CasaOS-kompatible Stores.
- **Installationshilfe:** freie Ports, Zeitzone, Pfade und Geheimnisse vorbereiten und die Herkunft der Feldwerte anzeigen. Eigene Vorlagen für Immich sowie Ollama mit Open WebUI; Container-Abbilder und Modelle werden erst bei Nutzung heruntergeladen.

[![Docker-App „immich“ in der Fernansicht im maximierten Fenster, geöffnetes ⋯-Menü mit Datei öffnen, Heruntergeladen, Zwischenablage, Qualität, Neu verbinden und Zustand der Ansicht (Bild nachgebildet)](docs/screenshots/current/fernansicht.png)](docs/screenshots/current/fernansicht.png)

[Fernansicht](docs/FERNANSICHT.md) · [App-Fenster und Zertifizierungsstelle](docs/APP-WINDOWS.md) · [App-Links](docs/APP-LINKS.md) · [HTTPS-Namen für Apps](docs/APP-PROXY.md) · [Docker aufräumen](docs/DOCKER-CLEANUP.md) · [Immich und Ollama-Vorlagen](docs/NATIVE-APPS.md)

</details>

<details>
<summary><b>NVR: Kameras, Aufnahme und Erkennung</b></summary>

- **Eigenes Programm „NVR“ (ab Werk aus):** nimmt Netzwerkkameras (RTSP, ONVIF) auf und erkennt Personen, Fahrzeuge und Tiere, auf Wunsch auch Kennzeichen und Gesichter. Die Arbeit macht [Frigate](https://github.com/blakeblackshear/frigate) in einem Container, den TitanOS selbst verwaltet. Er erscheint nicht als App; eine Konfigurationsdatei schreibst du nicht.
- **Einrichtung in Zeilen:** Volume für die Aufnahmen, Erkennungsgerät und Speichergrenze wählen, Kameras im Heimnetz suchen (ONVIF) oder per RTSP-Adresse eintragen und prüfen. Ist Docker aus, fragt die Einrichtung, ob es eingeschaltet werden soll; ein Volume mit schlafenden Platten nimmt der NVR nur nach Bestätigung. Die Frigate-Version ist je TitanOS-Release fest; das Abbild (etwa 1,5 GB) lädt TitanOS beim ersten Einschalten.
- **Erkennung auf NPU, iGPU, Coral (USB), Hailo oder CPU:** „Automatisch“ nimmt das beste vorhandene Gerät. Fällt es aus, weicht der NVR auf das nächste aus (NPU → iGPU → CPU) und meldet es. Die Seite Diagnose zeigt ohne Terminal, warum die Erkennung nicht läuft.
- **Live, Aufnahmen, Ereignisse:** Kacheln frei anordnen (je Konto gemerkt), eine Kamera groß oder im Vollbild, die letzten Ereignisse daneben; Ton, Gegensprechen (nur über HTTPS) und Schwenken, wenn die Kamera es kann. Aufnahmen auf einer Zeitleiste je Kamera und Tag abspielen und Ausschnitte exportieren. Ereignisse als Bilder oder Liste, gefiltert nach Kamera, Art und Zeitraum. Gesichter anlernen und bekannte Kennzeichen benennen (Administratoren).
- **Aufnahme je Kamera:** Nicht aufnehmen, Bei erkannten Objekten, Bei Bewegung oder Dauerhaft, mit Aufbewahrung in Tagen. Eine Speichergrenze für alle Kameras löscht die ältesten Aufnahmen zuerst.
- **Meldungen je Kamera:** an die Glocke, als Push, per E-Mail, an Home Assistant (eigene MQTT-Sensoren je Kamera) und an den KI-Agenten; wählbar je Art: Person, Fahrzeug, Tier, Kennzeichen, bekanntes Gesicht, Kamera offline.
- **Rechte je Kamera:** Besitzer und Administratoren sehen alle Kameras. Ein Benutzer sieht eine Kamera nur mit Haken, getrennt für „Live ansehen“ und „Aufnahmen ansehen“. Über API-Tokens und MCP sind die Kameras nicht erreichbar.
- **Widget „Kamera“:** zeigt eine Kamera auf dem Arbeitsplatz, klein als Standbild, groß als Live-Bild; ein Klick öffnet sie im NVR.
- **Sicherung:** Einstellungen, Kameras, Kamerarechte und bekannte Kennzeichen laufen mit der Sicherung des Sticks mit, bekannte Gesichter ebenfalls (bis 12 MB). Aufnahmen und Ereignisse gehören nicht dazu.
- **Prüfstand:** Geprüft mit dem echten Frigate-Abbild und einer nachgestellten Kamera auf einer Entwicklungsmaschine. Nicht geprüft sind NPU, iGPU, Coral und Hailo auf echter Hardware, echte Kameras (ONVIF, Ton, Gegensprechen, Schwenken) und die Erkennung echter Objekte samt den Meldungen dazu. Kennzeichen und Gesichter brauchen eine CPU mit AVX2 und 4 GB RAM.

[NVR: Aufbau, Rechte, Grenzen und Prüfstand](docs/NVR.md)

</details>

<details>
<summary><b>Sprachdienste: Sprache zu Text und Text zu Sprache</b></summary>

- **Eigene Seite „Sprachdienste“ (ab Werk aus):** [faster-whisper](https://github.com/SYSTRAN/faster-whisper) schreibt mit, was gesprochen wird, [Piper](https://github.com/rhasspy/piper) spricht Text – in zwei Containern, die TitanOS selbst verwaltet. Was gesprochen wird, verlässt das NAS nicht.
- **Für Home Assistant:** beide Dienste sprechen das Wyoming-Protokoll (Ports 10300 und 10200). Die Seite nennt Host und Port und die Schritte in Home Assistant. Ab Werk erreicht nur das eingetragene Home Assistant die Dienste, auf Wunsch das ganze Heimnetz.
- **Für den KI-Agenten:** das Mikrofon im Eingabefeld und „Vorlesen“ nutzen dieselben Dienste.
- **Modelle und Stimmen wählen:** Whisper von `tiny` bis `large-v3` und alle Piper-Stimmen des Katalogs (auch deutsche) lassen sich herunterladen, aktivieren, wechseln und löschen; TitanOS prüft Größe und Prüfsumme. Mit Hörprobe und „Ausprobieren“ fürs Mikrofon.
- **Grafikkarte wählbar:** Sprache zu Text rechnet auf einer NVIDIA-Karte (Treiber-Plugin) oder auf dem Prozessor; die Seite zeigt den freien Grafikspeicher. Text zu Sprache läuft auf dem Prozessor.
- **Prüfstand:** Geprüft mit den echten Abbildern auf einer Entwicklungsmaschine ohne Grafikkarte (kleinstes Modell, eine deutsche Stimme). Nicht geprüft sind eine NVIDIA-Karte, die großen Modelle und echtes Home Assistant.

</details>

<details>
<summary><b>Virtuelle Maschinen</b></summary>

- **KVM/QEMU und libvirt:** VMs anlegen, starten, sauber herunterfahren und automatisch starten. UEFI als Standard, optional Legacy-BIOS für passende Gäste; Windows 11 mit TPM 2.0 und Secure Boot im Gast.
- **Ressourcen und Datenträger:** CPU-Kerne zuweisen oder reservieren, qcow2/raw und vorhandene Abbilder nutzen, physische Platten einbinden und ausgeschaltete VMs mit neuen Kennungen klonen.
- **Je VM die Reiter Übersicht, Bildschirm, Hardware, Snapshots und Zeitplan;** die VMs stehen mit ihrem Zustand in der Seitenleiste. Der Bildschirm zeigt ausschließlich das Gastbild; Tastenkombinationen, Text senden, Vollbild und Bildschirmfoto bleiben außerhalb der Bildfläche erreichbar. Er gilt als Konsole: öffnet mit der bestehenden Anmeldung, endet nach der gewählten Zeit ohne Eingabe (5–120 Minuten oder „Nie“, ab Werk 15) und beim Abmelden. Serielle Konsole und ISO-Wechsel im laufenden Betrieb.
- **Zeitplan:** jede VM startet und fährt nach eigenem Zeitplan herunter, etwa werktags von 7 bis 22 Uhr. [Einzelheiten](docs/VM-SCHEDULE.md).
- **Netzwerk und Geräte:** bestätigte benannte NAS-Bridges mit eigener Gast-IP oder QEMU-User-Networking; erkannte IP-Adressen, PCI/GPU-/USB-Passthrough und Ordnerfreigaben per virtiofs. Eine Grafik wird nur übergeben, wenn kein Programm des NAS sie noch nutzt; bei der Start-iGPU wird die Textkonsole vorher gelöst und danach zurückgegeben, damit das NAS dabei nicht hängen bleibt.
- **Installationsmedien und Abbilder:** Browser-Upload oder HTTP(S)-Import, gzip/xz/ZIP auf dem Daten-Volume entpacken, Prüfsummen und kontrollierte Wiederaufnahme. Offizielle stabile Windows-VirtIO-ISO automatisch als zweites CD-Laufwerk einlegen. [Medienimport und Grenzen](docs/VM-MEDIA.md).
- **Snapshots:** VM-Datenträger, Konfiguration und bei unterstützten Gästen RAM-Zustand, UEFI und TPM sichern und zurücksetzen. Zeitpläne, Aufbewahrung und Wiederherstellungsstatus; Voraussetzungen und Ausschlüsse werden angezeigt.
- **VM übernehmen:** Eine Kopie der Einstellungen liegt im Ordner jeder VM auf dem Volume. Nach Verlust des Sticks, für entfernte VMs mit erhaltenen Platten oder für ein Volume von einem anderen TitanOS lassen sich VMs damit wieder registrieren (ohne durchgereichte Geräte, ohne Autostart).
- **Neue VM:** ein Formular in einer Spalte mit einer Zusammenfassung daneben. Die Heimnetz-Bridge `br0` wird bei der Ersteinrichtung automatisch vorbereitet und nach Verbindungsbestätigung dauerhaft übernommen; bestehende NAS aktivieren sie direkt aus der VM-Einrichtung.

[VM-Snapshots und Grenzen](docs/VM-SNAPSHOTS.md) · [VM übernehmen](docs/VM-IMPORT.md) · [VM-Zeitplan](docs/VM-SCHEDULE.md) · [Bridge-Netzwerk](docs/NETWORKING.md)

</details>

<details>
<summary><b>LXC-Systemcontainer</b></summary>

- **Ganze Linux-Systeme ohne VM:** Container aus den Abbildern von images.linuxcontainers.org (Debian, Ubuntu, Alpine und weitere) anlegen, starten, herunterfahren und automatisch starten. Das Root-Dateisystem liegt auf einem Daten-Volume.
- **Sicher voreingestellt:** unprivilegiert mit eigenen IDs; privilegierte Container nur ausdrücklich und mit Warnung. CPU- und RAM-Grenzen, Netzwerk an den Bridges des NAS mit fester MAC-Adresse, NAS-Ordner wahlweise nur lesend.
- **Konsole und Snapshots:** Shell und Textkonsole im Terminal-Fenster; auf Btrfs-Volumes Snapshots anlegen und zurücksetzen. [Einzelheiten und Grenzen](docs/LXC.md).

</details>

<details>
<summary><b>Sicherungen und Wiederherstellung</b></summary>

- **Versionierte Backups mit restic:** Freigaben, Volumes oder einzelne Ordner verschlüsselt auf ein anderes Volume, ein USB-Laufwerk, eine SMB-Freigabe, ein anderes TitanOS, per SFTP, zu S3 oder zu einem restic-REST-Server sichern. Zeitpläne stündlich, täglich, wöchentlich oder monatlich, auch mehrmals am Tag; Aufbewahrung als eine Zahl oder „bis das Ziel voll ist“, Zielprüfung und Fortschritt.
- **Wiederherstellung prüfen:** Archivprüfungen und echte Wiederherstellungsproben; ganze Stände oder einzelne Dateien als neue Kopie in einen eigenen Ordner zurückholen.
- **Anwendungen mit sichern:** ausgewählte Container und VMs sauber anhalten und anschließend wieder starten; optional PostgreSQL exportieren. Sicherungsabdeckung zeigt, welche ausgewählten Shares, Container und VMs berücksichtigt werden.
- **Geführte Anwendungswiederherstellung:** Daten zuerst in einen neuen Ordner holen, vorhandene historische Anwendungsinformationen prüfen und die Übernahme ausdrücklich abschließen. Ungeklärte unterbrochene Vorgänge werden zur Prüfung angezeigt.
- **Replikation auf ein zweites TitanOS:** Btrfs-Volumes Snapshot für Snapshot über Tailscale spiegeln; nach dem ersten Lauf nur Änderungen, mit Zeitplan, Aufbewahrung, Bandbreitengrenze und Wiederherstellung als neuer Ordner. [Einzelheiten](docs/REPLICATION.md).
- **Direkt lesbare Kopie:** neue und geänderte Dateien auf ein anderes Volume, auf USB oder ein SMB-Ziel übertragen, sofort oder nach Zeitplan. Ab Werk ohne Versionen, am Ursprung Gelöschtes bleibt am Ziel; wahlweise ein Ordner je Lauf (Versionen) oder ein Spiegel, der Gelöschtes eine Zeit lang in `_gelöscht` aufhebt. Die SMB-Kopie trägt ein Beta-Label.
- **Laufende VMs in der lesbaren Kopie:** Der Auftrag nennt je Platte, wie sie gesichert wird. Auf Btrfs liest TitanOS sie aus einem kurzlebigen Snapshot, die VM läuft weiter; sonst wählt man „Kurz anhalten“ (höchstens 15 Minuten) oder „Überspringen“. Bricht eine Sicherung ab, führt TitanOS die VM beim nächsten Start zurück.
- **Systemkonfiguration sichern:** ab Werk eine gebündelte Sicherung des Sticks auf einem Daten-Volume, wahlweise nach jeder Änderung oder „täglich, wenn sich etwas geändert hat“; Download und Wiederherstellung bei der Ersteinrichtung eines Ersatzsticks. Die Konfigurationssicherung enthält keine Nutzdaten.
- **Systemdaten:** Systemsteuerung › Daten und Sicherung › Systemdaten zeigt, wo TitanOS seine eigenen Daten ablegt: Sicherung der Einstellungen, System- und Aktivitätsprotokoll, Meldungen, SMART- und Monitoring-Verlauf. Ohne eigene Wahl nimmt TitanOS ein Volume, dessen Platten nie schlafen, und die Seite sagt, ob eine Wahl schlafende Platten wecken würde.

[![Systemsteuerung › Daten und Sicherung › Systemdaten: Ablage-Volume und je eine Zeile für Konfigurationssicherung, Systemprotokoll, Aktivitätsprotokoll, Meldungen, SMART- und Monitoring-Verlauf](docs/screenshots/current/systemdaten.png)](docs/screenshots/current/systemdaten.png)

[Backup- und Wiederherstellungsabläufe](docs/WORKFLOWS.md) · [USB-Kopie](docs/USB-COPY-BACKUPS.md) · [SMB-Kopie](docs/SMB-COPY-BACKUPS.md)

</details>

<details>
<summary><b>Wartung, Monitoring und Diagnose</b></summary>

- **Eigene Skripte:** Aufgabenplaner › Skripte führt eigene Skripte als root aus – per Knopf mit Live-Ausgabe, nach Zeitplan (auch Cron), einmal beim Start nach dem Einbinden der Volumes oder beim Herunterfahren vor dem Beenden der Apps. Sie liegen auf einem Daten-Volume, die letzten zehn Läufe bleiben mit Ausgabe erhalten, fehlgeschlagene automatische Läufe melden sich. Nur für Besitzer/Administratoren, nicht über API-Tokens oder MCP. [Einzelheiten](docs/SCRIPTS.md)
- **Wartungspläne** (Aufgabenplaner › Wartung): gemeinsame Zeitfenster mit Zeitzone, auf Wunsch alle N Stunden; RAID-Prüfungen, Btrfs-Scrub, SMART-Kurz- und Langtests (beim Anlegen eines Volumes vorausgewählt), Backup-Prüfungen, Wiederherstellungsproben und Snapshot-Aufbewahrung. Abhängigkeiten, optionaler Backup-Vorrang und sichtbare Wartegründe.
- **Auftragsübersicht** (Monitor › Aufgaben): laufende und bekannte abgeschlossene Vorgänge filtern, Fehler prüfen und unterstützte Aufträge pausieren, abbrechen oder fortsetzen. Eigene Uploads einsehen.
- **Monitor:** Task-Manager mit CPU und Threads, RAM, Netzverkehr, Volumes, Laufwerken, Temperaturen und Beschleunigern. Kurze Verläufe im RAM; optional sieben oder 30 Tage auf einem Daten-Volume, mit CSV-/JSON-Export. Fehlende Werte bleiben unbekannt.
- **Hardware-Meldungen:** stehender Lüfter, kritische Temperatur, Speicherfehler (ECC), Fehler des Boot-Sticks und unsauberes Herunterfahren werden von selbst gemeldet. Je Sensor zeigt Systemsteuerung › Hardware und Energie › Temperaturen die Schwelle; einzelne Sensoren lassen sich stummschalten. Der Schalter „Bei Überhitzung herunterfahren“ (ab Werk aus) fährt das NAS herunter, wenn ein verlässlicher Sensor mehrmals hintereinander zu heiß meldet. [Meldungen](docs/NOTIFICATIONS.md).
- **Lüfter:** je Lüfter BIOS, feste Drehzahl oder eine Kurve nach Temperatursensor oder wärmster Platte, mit Volllast bei Sensorausfall. CPU-Leistung (RAPL) als Messwert. [Lüfter](docs/FANS.md).
- **RAM-Verwaltung:** verfügbarer RAM, Dateicache und übrige Belegung getrennt sehen; Hinweise bei anhaltender Speicherknappheit und optionale PSI-Wartezeiten. Linux verwaltet Lesecache automatisch. Das einstellbare Schreibpufferbudget und begrenzte Upload-Fenster orientieren sich an verfügbarem Arbeitsspeicher und einer Systemreserve.
- **Protokolle und Diagnose:** Systemjournal im Browser nach Quelle und Stufe filtern, auch das des vorigen Starts (auf einem Daten-Volume aufbewahrt, nie auf dem Stick); ein Diagnosepaket mit Systemzustand und Filterung bekannter sensibler Felder erstellen. Vor dem Weitergeben bleibt eine eigene Prüfung sinnvoll.
- **Obergrenzen für Freigaben:** auf ext4- und XFS-Volumes vom Kernel durchgesetzte Grenze pro Freigabe für SMB, NFS, Apps und Dateimanager; Windows sieht sie als Laufwerksgröße. Einmal pro Volume einschalten, wirksam nach dem nächsten Neustart; nicht auf Btrfs. [Einzelheiten](docs/SHARE-QUOTAS.md).
- **Aktivitätsprotokoll:** wer wann was geändert hat – Anmeldungen und Fehlversuche, Einstellungen, Dateiaktionen mit Pfaden, Konsolen, Apps und VMs. Filter nach Konto, Zeitraum und Fehlschlägen, Export als CSV/JSON. Liegt mit Aufbewahrungsdauer und Größengrenze auf einem Daten-Volume; Passwörter, Schlüssel und Dateiinhalte werden nie gespeichert. [Einzelheiten](docs/ACTIVITY.md).

[Monitoring](docs/MONITORING.md) · [Wartung und Aufträge](docs/WORKFLOWS.md) · [RAM-Verwaltung](docs/MEMORY.md)

</details>

<details>
<summary><b>GPU, iGPU, NPU und Plugins</b></summary>

- **Hardware prüfen:** Monitor › Hardware prüfen fasst CPU, RAM, Controller, Netzwerk, Grafik, Sensoren und Treiberstand zusammen, nennt Geräte ohne Treiber mit Grund (etwa „behoben ab Linux 7.3“ und ob der Kernel „Neu“ es schon hat) und speichert auf Wunsch einen anonymisierten Schnappschuss für einen Fehlerbericht.
- **Stabile Hardwarebasis:** Intel-/AMD-CPU-Microcode für frühes Laden, gezielte Netzwerk-/HBA-Firmware und geprüfte Intel-i915/xe-/NPU- sowie AMD-Grafikmodule und Firmware. Für Intel-Grafik von Skylake bis Tiger/Rocket Lake lässt sich unter Systemsteuerung › Hardware und Energie › Hardware HuC (Low-Power-Encodierung) bzw. GuC-Submission mit HuC laden; ab Werk gilt der Kernelstandard, wirksam nach einem Neustart, nicht auf echter Gen9–11-Hardware geprüft. [Umfang und Hardware-Prüfgrenzen](docs/HARDWARE-COMPATIBILITY.md).
- **Hardware für Apps auswählen:** stabile Gerätekennungen für Intel-/AMD-Grafik, NVIDIA, NPU und Coral USB; Zuordnung je Container-Dienst.
- **Treiber und Werkzeuge als Plugins:** signierter Katalog nach Bereichen mit Suche und ausführlicher Beschreibung, Vorschläge für die erkannte Hardware (nie automatisch installiert), Installation auf einem Daten-Volume und zur Systembasis passende Pakete. Zusätzliche Treiber, die Debians Kernel nicht baut (Wangxun-Netzwerkkarten, Intel QAT Gen 4, neuere ITE-Sensorchips), gibt es als eigene Plugins für genau den Kernel des Releases. Plugin-Dateien und Einstellungen bleiben beim System-Rückfall getrennt erhalten. Neuere Fassungen aus dem Katalog oder einer Freigabe lassen sich auf der Plugin-Seite aktualisieren, mit Rückweg, falls sie nicht starten. Die offiziellen Plugins kommen aus dem eigenen Repository [TitanOS-Plugins](https://github.com/ra5on/TitanOS-Plugins), sobald es für die Systembasis einen Satz gibt.
- **Community-Plugins:** von einem Link oder aus selbst eingetragenen Quellen, mit deutlicher Warnung, Bestätigung und Updates; nie automatisch installiert.
- **Beschleuniger beobachten:** Auslastung, Temperatur, Leistung, Speicher, Takt und Engines, soweit Gerät und Treiber diese Werte liefern. Nicht verfügbare Messwerte werden nicht als null ausgegeben. Die Widgets iGPU, NPU und Grafikkarte zeigen Auslastung, Temperatur, Grafikspeicher, Leistung und Takt auf dem Arbeitsplatz.
- **NVIDIA:** passendes Treiber-Plugin, Leistungslimit und optionaler geregelter Wechsel zwischen VM und GPU-Containern. Jede Treiberversion aus NVIDIAs Archiv lässt sich wählen; das NAS baut sie selbst und nach jedem Kernel-Update von selbst neu, bis dahin läuft der mitgelieferte Treiber.
- **Intel GPU Tools und Coral USB:** zusätzliche Diagnose und Geräteerkennung. Die Coral-Laufzeit stellt die jeweilige Container-App bereit; PCIe-/M.2-Coral ist nicht unterstützt.

[Plugins und Gerätezuordnung](docs/PLUGINS.md) · [NVIDIA](docs/NVIDIA.md) · [Lokaler NVIDIA-Bau](docs/NVIDIA-LOCAL-BUILD.md)

</details>

<details>
<summary><b>Home Assistant und MQTT</b></summary>

- **Bis zu acht Verbindungen,** etwa Haus und Ferienwohnung; jede Verbindung hat ihre eigenen Einstellungen.
- **NAS-Werte per MQTT-Discovery:** System, Volumes/RAID, Laufwerke, Container, Stacks, VMs, Netzwerk, Backups, Wartung, Kapazitätsprognosen und USV sowie GPU, iGPU und NPU; dazu Sensoren für Verbindung und Ereignisse der Kameras des NVR, die an Home Assistant melden.
- **Gezielt veröffentlichen:** Gruppen und einzelne Geräte auswählen. Stabile Kennungen, Verfügbarkeit, Ablaufzeiten und Bereinigung entfernter Entitäten; keine zusätzlichen SMART-Abfragen zum Aufwecken schlafender Platten.
- **Optionale Knöpfe:** Container, VMs und eingerichtete Backups steuern, Updates prüfen sowie NAS-Neustart und Herunterfahren auslösen. Die Rechte des ausgewählten Kontos gelten auch hier.
- **Speicher für Home Assistant:** Freigaben für Backups, Medien und Add-ons mit eigenem Konto direkt in Home Assistant OS eintragen und HA-Backups überwachen.
- **Geräte für MCP freigeben:** ganze Bereiche oder einzelne Geräte mit Aliasnamen und Fähigkeiten erlauben; KI-Clients lesen Zustände und Historie, rufen erlaubte Dienste auf und prüfen Zielzustände; begrenzte Rücknahme unterstützter Schaltvorgänge.
- **Benachrichtigungen** über die Home-Assistant-App aufs Smartphone.

[Home-Assistant-Anleitung, Einrichtung und Prüfgrenzen](docs/HOME-ASSISTANT.md)

</details>

<details>
<summary><b>KI-Agent, API und Automatisierung</b></summary>

- **Persönliche API-Zugänge:** benannte, widerrufbare Tokens mit bewusst gewählten Bereichen und aktuellen Konto-/Share-Rechten. Standard nur lesend, Geheimnis einmalig sichtbar, ausschließlich sein Hash wird gespeichert.
- **OpenAPI und CLI:** dokumentierter API-Vertrag und statisches `titanosctl` für Status, Dateien sowie erlaubte Docker-/VM-Aktionen. [Verwendung](docs/PUBLIC-API.md).
- **KI-Agent (optional, ab Werk aus):** erledigt Aufträge in normaler Sprache mit den Werkzeugen von TitanOS, in mehreren Schritten und mit deinem eigenen Ollama (z. B. gpt-oss). Je Werkzeug, Bereich und Ordner einstellbar: ohne Nachfrage, mit Knopf im Chat oder verboten. Im Desktop-Modus ein eigenes Programm, am Handy eine eigene App.
  - **Chats und Wissen:** Projekte (auch mit anderen Konten geteilt), Archiv, Notizen, Gedächtnis und Skills samt Skill-Katalog; Dateien aus deinen Freigaben im Chat; eigener Ordner je Konto in der Freigabe „KI-Agent“; Websuche.
  - **Werkzeuge:** Word-, Excel- und PDF-Dateien anlegen und ändern; Spracheingabe über Whisper, Bilder verstehen und Vorlesen; Code in einer Sandbox (ab Werk aus); Snapshot vor riskanten Änderungen; Home Assistant samt Assist.
  - **Von selbst:** Erinnerungen, geplante Aufträge (einmalig, Intervalle, Wochentage, Cron) und Morgenbericht; Eigeninitiative bei Meldungen des NAS. Geplante Aufträge wählen, ob sie schlafende Platten wecken dürfen.
  - **Funktionen:** Dokumenten-Gedächtnis, Fristenwächter, Papierkram, Ransomware-Wächter, Notfallhandbuch, Notfallzugang, Was-wäre-wenn, Hardware-Berater und Backup-Buddy.
  - Im Cluster nutzt ein Knoten auf Freigabe das Modell des Admin-Knotens. [Einzelheiten](docs/AGENT.md)
- **MCP-Server (optional, ab Werk aus):** Claude Desktop, Claude Code und andere MCP-Clients arbeiten mit den Rechten eines persönlichen Tokens am NAS: Zustand, Speicher, Freigaben, Dateien und Dokumente (Word, Excel, PDF), Docker, VMs, Sicherungen, Firewall-Regeln, Konten, Plugins, Updates, Neustart/Ausschalten, freigegebene Home-Assistant-Geräte und das Handbuch. Verwaltungsaktionen brauchen den Token-Bereich `system:write` (nur Besitzer/Administratoren). Jedes Werkzeug lässt sich einzeln ausschalten; gefährliche und zerstörende Aufrufe sind ab Werk gesperrt und laufen erst nach ausdrücklicher Freischaltung und mit Bestätigung des Clients. Geheimnisse, Passwörter, Konsolen und Netzwerkbestätigung bleiben außen vor; jeder Aufruf steht im Aktivitätsprotokoll. Die Seite Systemsteuerung › KI und Smart Home › MCP-Server zeigt allen Konten Zustand, Endpunkt und Verbindungsanleitung samt Token-Knopf; Schalter, Werkzeuge und letzte Aufrufe sehen nur Besitzer und Administratoren. [Einrichtung](docs/MCP.md).

</details>

<details>
<summary><b>Netzwerk, Energie und Systemzugang</b></summary>

- **Netzwerk:** mehrere benannte Netze über Anschlüsse, Bonds und VLANs; IPv4-DHCP/feste Adressen, IPv6-SLAAC/DHCPv6/feste Adressen, DNS und Routen. Bevorzugter Verwaltungszugang, zweiminütiger Probelauf und authentifizierte lokale Wiederherstellung. Router und DHCP bleiben extern.
- **Tailscale als Plugin:** Fernzugriff, Subnet-Routing und Exit-Node-Betrieb in beiden Rollen; Routen- und DNS-Einstellungen mit Rückfall. Unter dem Tailnet-Namen (`https://<name>.<tailnet>.ts.net`) liefert TitanOS ein gültiges Zertifikat aus, sobald HTTPS im Tailnet an ist (Ein-Klick-Freischaltung aus den Einstellungen).
- **WireGuard-Server:** eigener VPN-Zugang ohne Zusatzdienst: einschalten, Gerät per Namen hinzufügen, QR-Code mit der WireGuard-App scannen; Zugriff auf Heimnetz oder alles.
- **Cluster wie bei Proxmox:** mehrere TitanOS per Beitrittsinformation verbinden; der Admin-Knoten verwaltet alle anderen, weitere Knoten nur mit seiner Freigabe, seine Konten gelten überall, Backups von Knoten zu Knoten. Den Weg wählst du selbst (Heimnetz, Tailscale, NetBird, WireGuard, eigene Adresse; für Verwaltung und Sicherungen getrennt, je Knoten abweichend, mit Rückkehr auf den bevorzugten Weg), auf Wunsch über einen eigenen WireGuard-Tunnel zwischen den Knoten. Knoten mit unterschiedlicher Version bleiben verwaltbar; „Alle aktualisieren“ nacheinander oder gleichzeitig. Meldungen jedes Knotens direkt in der Knotenliste. Abgesichert durch festgenagelte Zertifikate in beide Richtungen und signierte Anfragen. Kein Ausfallschutz. [Einzelheiten](docs/CLUSTER.md).
- **NetBird als Plugin:** Fernzugriff über NetBird Cloud oder einen eigenen Management-Server, Anmeldung per Setup-Key (wird nie wieder angezeigt), Routen, Routing-Peer/Exit Node und DNS mit zweiminütigem Rückfall. Ausgeliefert wird nur der BSD-lizenzierte Client.
- **Energie und USV:** CPU-Profile, Herunterfahren und Einschalten per RTC-Zeitplan, Wake-on-LAN und Verhalten des physischen Ein-/Ausschalters. USB- oder Netzwerk-USV über NUT, Abschalten bei Stromausfall und Weitergabe des USV-Status.
- **Firewall:** ab Werk aus; eingeschaltet Zonen Heimnetz, VPN und Internet (auch öffentliches IPv6) mit Freigaben je Dienst und je Docker-App, eigenen Regeln, zweiminütiger Probe mit Rückfall und Liste zuletzt verworfener Pakete. Optional automatisches Sperren von Absendern nach fehlgeschlagenen Anmeldungen an Weboberfläche, SSH und SMB.
- **HTTPS:** dauerhaftes selbst signiertes Zertifikat ab dem ersten Start, eigene PEM-Zertifikate samt Schlüssel und Kette importieren oder automatische Zertifikate von Let's Encrypt per DNS-Challenge (Cloudflare, deSEC, Hetzner, IONOS, Netcup, INWX, DuckDNS, IPv64, RFC 2136; auch als Platzhalter für die HTTPS-Namen der Apps) oder über Port 80, mit selbständiger Erneuerung.
- **Dynamisches DNS:** Namen auf die öffentliche IPv4-Adresse des Anschlusses und die IPv6-Adresse des NAS halten, bei denselben Anbietern oder per dyndns2 (Strato, No-IP …); mit IPv64 als Anbieter optional eine Überwachung der Erreichbarkeit bei IPv64.net.
- **Terminal und SSH:** NAS-Shell als Programm „Terminal“ (mehrere Fenster möglich) für Besitzer/Administratoren; Konsolen von Docker-Containern, VMs (seriell) und Systemcontainern öffnen im Terminal-Fenster. Trennen ohne Eingabe nach wählbarer Zeit (5–120 Minuten oder „Nie“, ab Werk 15). SSH ist zunächst ausgeschaltet; nach Aktivierung Anmeldung mit eigenem Konto und Schlüssel, bei Bedarf `sudo`. Linux-root ist für direkte Anmeldung gesperrt. Offene Konsolen enden innerhalb weniger Sekunden nach Abmelden, Kontolöschung, Herabstufung oder Passwort-Zurücksetzen; ein entfernter SSH-Schlüssel beendet auch die SSH-Sitzungen des Kontos.
- **Benachrichtigungen:** Meldungen in der Glocke mit persönlichen Filtern sowie E-Mail, ntfy, Telegram, Discord, Home-Assistant-App und Mitteilungen direkt aufs Handy oder in den Browser (Web Push, auf dem iPhone über TitanOS auf dem Home-Bildschirm) für Systemereignisse; Besitzer und Administratoren bekommen Speicher-, Sicherungs-, Update- und Systemmeldungen zusätzlich auf eigene Wege mit Ruhezeiten; auf Wunsch ein wöchentlicher oder monatlicher Bericht über das NAS.

[Netzwerk](docs/NETWORKING.md) · [Firewall](docs/FIREWALL.md) · [Let's Encrypt](docs/LETSENCRYPT.md) · [Dynamisches DNS](docs/DDNS.md) · [WireGuard](docs/WIREGUARD.md) · [Web Push](docs/WEB-PUSH.md) · [Tailscale](docs/TAILSCALE.md) · [NetBird](docs/NETBIRD.md) · [Ein-/Ausschalter](docs/POWER-BUTTON.md)

</details>

<details>
<summary><b>Systemsteuerung, Darstellung und Handy</b></summary>

- **Systemsteuerung:** oben das angemeldete Konto mit Rolle, darunter das Feld „Einstellung suchen“ (findet auch Stichwörter, etwa „ssh“ › Zugang) und die Bereiche Übersicht, System, Hardware und Energie, Netzwerk, Konten und Zugang, Dienste und Meldungen, KI und Smart Home, Daten und Sicherung sowie Updates. Ein Benutzer sieht nur, was er öffnen darf.
- **Übersicht:** Zustand (Name, Version, Laufzeit, Volumes), „Zu erledigen“ aus der Einrichtungsliste und verfügbaren Updates, Dienste mit „an“ oder „aus“ und „Zuletzt geändert“.
- **Darstellung** (Systemsteuerung › System › Darstellung) gilt für dein Konto überall – Arbeitsplatz, Fenster, Handy. Änderungen wirken sofort und speichern sich selbst; nur App-Symbol und Anmeldeseite haben einen Knopf „Übernehmen“.
  - **Farben:** Hell, Dunkel oder Automatisch; dunkle Variante Graphit, Schwarz, Mitternacht oder Eigenes; Akzentfarbe aus sechs Farben oder eigene; Regler für Glas (von Klar bis Getönt), Ecken, Schriftgröße und Farbstärke; Theme als Datei exportieren und importieren.
  - **Glas & Schrift:** Sprache, Dichte, sanfte Übergänge, Glanzkante, Leuchten, Abdunkeln hinter Dialogen und einzeln abschaltbare Effekte. „Bewegung reduzieren“ des Systems wird beachtet.
  - **Hintergrund:** sechs gezeichnete Hintergründe (hell und dunkel) und sechs Fotos, Nordlicht und Bergsee bewegt, oder ein eigenes Bild. Besitzer und Administratoren wählen hier außerdem das App-Symbol des NAS für den Home-Bildschirm.
  - **Dock** mit Vorschau und der Schalter für CPU und RAM in der oberen Leiste.
- **Anmeldeseite gestalten:** Besitzer und Administratoren legen für das ganze NAS fest, welche Konten erscheinen (als Kreise oder Liste, einzeln wählbar), Theme, Hintergrund (der des Arbeitsplatzes, ein Galerie-Motiv oder ein eigenes Bild), Namen des NAS, Logo, bewegten Hintergrund, Übergang zum Arbeitsplatz und Überschrift und Begrüßung auf Deutsch und Englisch – mit Vorschau. [Einzelheiten](docs/LOGIN-APPEARANCE.md).
- **Deutsch und Englisch**, lokale Schriftdateien, Tastatur- und Touch-Bedienung.
- **Dienste nach Bedarf:** Docker, VMs und LXC lassen sich unter Systemsteuerung › Dienste und Meldungen › Dienste ausschalten; ihr Programm verschwindet, die Daten bleiben.

[![Systemsteuerung › Übersicht mit Seitenleiste, Zustand, Zu erledigen, Diensten und „Zuletzt geändert“](docs/screenshots/current/systemsteuerung.png)](docs/screenshots/current/systemsteuerung.png)

[Systemsteuerung › Darstellung](docs/screenshots/current/darstellung.png) · [Anmeldeseite](docs/screenshots/current/anmeldeseite.png) · [Anmeldeseite gestalten](docs/LOGIN-APPEARANCE.md)

<p align="center">
  <a href="docs/screenshots/current/handy-start.png"><img src="docs/screenshots/current/handy-start.png" alt="Startbildschirm am Smartphone: Knopf „Menü“, Suche, Zustand und App-Raster mit Programmen und Docker-Apps" width="260"></a>
  <a href="docs/screenshots/current/fernansicht-mobil.png"><img src="docs/screenshots/current/fernansicht-mobil.png" alt="Docker-App „immich“ in der Fernansicht am Smartphone im Vollbild mit Zurück, Lautstärke und Tastatur (Bild nachgebildet)" width="260"></a>
</p>

</details>

<details>
<summary><b>Updates mit geprüftem Rückfall</b></summary>

TitanOS nutzt einen **Stable-Kanal**. Die Weboberfläche prüft und installiert Updates manuell oder nach Zeitplan; nach einer Verwaltungsanmeldung kann eine Prüfung im Hintergrund erfolgen.

- **Updates-Seite:** Ein vorhandenes Update steht in einer großen Karte mit Versionssprung, Art (mit oder ohne Neustart), Größe, Kernel und Datum. „Das ist neu in …“ klappt auf, gegliedert in Neu, Sicherheit und Behoben. Die Installation zeigt ihre Schritte.
- **Kompatible App-Updates** ersetzen nur TitanOS mit seiner Weboberfläche, ohne Neustart des NAS. Container, VMs und Freigaben laufen weiter; die Anmeldung bleibt erhalten. Bei fehlgeschlagenem Probestart folgt der Rückfall zur vorigen App oder Systembasis.
- **Version frei wählen:** unter Systemsteuerung › Updates eine andere veröffentlichte Version installieren, auch eine ältere (angeboten werden bis zu 20 der neuesten, frühestens 0.46.2); Kernel „Stabil“ (Debian) oder „Neu“ (trixie-backports).
- **Sicherheitsupdates von selbst:** Ein nächtlicher Lauf prüft Debian-Sicherheitsupdates und neue Kernel und baut, testet und veröffentlicht daraus ein neues Release. Mit „Von selbst installieren“ spielt das NAS Updates zur gewählten Uhrzeit ein. Ein Release, das auf diesem NAS seine Probestarts nicht bestanden hat, wird nicht mehr von selbst installiert. [Autopilot](docs/AUTOPILOT.md).
- **Volle Systemupdates** erneuern auch Kernel und Systempakete. Sie werden in den inaktiven A/B-Slot geschrieben und starten auf Probe. Scheitert die Startprüfung wiederholt, bootet der vorherige Slot.
- **Signatur und Integrität sind Pflicht:** nur gültig signierte Manifeste und passende Datei-Prüfsummen werden angenommen. Laufende relevante Daten- und Wartungsvorgänge blockieren die Installation bis zu ihrem Abschluss.

[App-Updates](docs/APP-UPDATES.md) · [Release-Dateien, Signaturen und Prüfung](docs/RELEASING.md)

</details>

## Voraussetzungen und aktuelle Grenzen

| Bereich | Voraussetzung |
|---|---|
| Rechner | 64-Bit-PC (x86-64/amd64), normal mit UEFI. Auch möglich: 32-Bit-UEFI-Firmware mit 64-Bit-CPU (z. B. Atom „Bay Trail“) oder Legacy-BIOS (dafür das Stick-Abbild oder `boot/grub/stick-bios.py`, siehe `docs/BOOT.md`). Secure Boot für das NAS ausgeschaltet |
| Arbeitsspeicher | Für erste Tests 4 GB einplanen, zusätzlich RAM für Apps und VMs; keine pauschal abgenommene Hardware-Mindestgröße |
| Bootmedium | USB-Stick mit 8–32 GB, FAT32, Label `TITANOS` |
| Nutzdaten | Separate Laufwerke; ein neues Volume löscht die ausgewählten Platten |
| Netzwerk | Kabelgebunden, beim ersten Start DHCP |
| Virtualisierung | Für VMs aktivierte Hardware-Virtualisierung; Passthrough benötigt passende IOMMU-/Geräteunterstützung |
| Kameras (NVR) | Docker eingeschaltet und ein Daten-Volume; das Frigate-Abbild lädt etwa 1,5 GB. Kennzeichen und Gesichter brauchen eine CPU mit AVX2 und 4 GB RAM |
| Sprachdienste | Docker eingeschaltet und ein Daten-Volume; die Abbilder laden etwa 0,4 GB (mit NVIDIA-Karte etwa 1,9 GB), dazu Modelle von 0,1 bis 3,1 GB. Eine Grafikkarte ist freiwillig; große Modelle sind ohne sie langsam |

RAID ersetzt kein Backup. Unterstützung und Messwerte hängen von Kernel, Firmware und konkreter Hardware ab; das gilt auch für GPUs, NPUs, Kameras, USVs und externe Backupziele. Die Release-Prüfungen umfassen automatisierte Tests und virtuelle NAS-Systeme, keine vollständige Abnahme aller Geräte und Dienste. Der NVR ist mit nachgestellter Kamera geprüft, nicht mit echten Kameras und nicht mit NPU, iGPU, Coral oder Hailo auf echter Hardware. Die genauen Ergebnisse stehen in den [Release-Berichten](docs/releases/).

ARM und ZFS sind nicht enthalten. Der Bootloader wird bei Updates nicht automatisch ersetzt; volle Systemupdates aus einer hochgeladenen Datei sind nicht umgesetzt. Die Firewall ist ab Werk aus und filtert eingeschaltet nur, was das NAS selbst und veröffentlichte App-Ports erreicht, nicht VMs, LXC-Container und Apps mit eigener LAN-Adresse; [Firewall](docs/FIREWALL.md).

## Installation

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/readme/h-start-dark.svg">
    <img src="docs/readme/h-start-light.svg" alt="In drei Schritten startklar." width="620">
  </picture>
</p>

<table>
<tr>
<td align="center" width="33%"><b>1</b><br>ZIP laden und Signatur prüfen</td>
<td align="center" width="33%"><b>2</b><br>Auf einen FAT32-Stick „TITANOS“ entpacken</td>
<td align="center" width="33%"><b>3</b><br>Starten und im Browser öffnen</td>
</tr>
</table>

Im Detail:

1. Auf der [aktuellen Stable-Release-Seite](https://github.com/ra5on/TitanOS/releases/latest) das Stick-ZIP `titanos-<version>.zip` sowie dessen `.minisig` und `.sha256` herunterladen. Die Signatur mit [minisign](https://jedisct1.github.io/minisign/) und einem vertrauenswürdig bezogenen [Release-Schlüssel](build/mkosi.extra/usr/share/titanos/update-keys/) prüfen. [Download-Prüfung](docs/RELEASING.md#downloads-prüfen).
2. Einen leeren USB-Stick als **FAT32** mit dem Label **`TITANOS`** formatieren. Das ZIP direkt ins Stammverzeichnis entpacken. Dort liegen anschließend unter anderem `EFI`, `boot`, `loader`, `titanos` und `config`.
3. Per **UEFI, Secure Boot aus**, vom Stick starten. Die DHCP-Adresse steht auf der Konsole oder im Router. Im Browser **`https://<nas-ip>`** öffnen; das selbst signierte Zertifikat erzeugt zunächst eine Browserwarnung.
4. Bei einer Neuinstallation das Besitzerkonto anlegen. Die Einrichtung fragt danach Sprache, Name und Zeit, das Netzwerk und die Office-Programme ab. Unter **Speicher** ein Volume anlegen und unter **Freigaben** eine Freigabe erstellen. Die Sicherung der Einstellungen läuft ab Werk, sobald es ein Volume gibt; die Zwei-Faktor-Anmeldung richtest du unter **Mein Konto** ein.

Ein vorhandenes NAS unter **Systemsteuerung › Updates** aktualisieren. Den eingerichteten Stick dafür nicht neu formatieren.

### Einen ausgefallenen Stick ersetzen

Einen neuen Stick wie oben vorbereiten und beim ersten Start **vor dem Anlegen eines neuen Kontos** die gefundene Konfigurationssicherung wiederherstellen. TitanOS sucht lesend auf unterstützten Volumes nach Sicherungen unter `.titanos/backups/`. Nach Wiederherstellung und Neustart werden die registrierten Volumes wieder eingebunden.

Die Datenplatten nicht als neue Volumes anlegen oder formatieren. Ohne Konfigurationssicherung können unterstützte bestehende mdadm-/Btrfs-Volumes über **Speicher › Weitere Optionen › Speicher übernehmen** registriert werden; Konten, Freigaben und sonstige Einstellungen müssen neu eingerichtet werden. Das ist kein Import beliebiger fremder Speichersysteme. [Voraussetzungen zur Übernahme](docs/WORKFLOWS.md).

## Dokumentation

| Thema | Einstieg |
|---|---|
| Oberfläche | [Desktop-Modus](docs/DESKTOP.md), [Sperrbildschirm](docs/SESSION-LOCK.md), [Handy-App](docs/PWA.md), [Anmeldeseite](docs/LOGIN-APPEARANCE.md) |
| Speicher, Sicherungen und Wartung | [Abläufe und Grenzen](docs/WORKFLOWS.md), [Btrfs](docs/BTRFS.md), [Ruhemodus](docs/DISK-SLEEP.md), [VM-Snapshots](docs/VM-SNAPSHOTS.md), [LXC-Container](docs/LXC.md) |
| Dateien und Übertragungen | [Dateieditor](docs/FILE-EDITOR.md), [Kopieren/Verschieben](docs/FILE-COPY.md), [RAM/Uploads](docs/MEMORY.md), [Windows-SMB](docs/WINDOWS-SMB.md) |
| Apps und Programme | [Fernansicht](docs/FERNANSICHT.md), [Web-Apps und TitanOS-Office](docs/WEBAPPS.md), [Öffnen mit Docker-Apps](docs/OPEN-WITH-DOCKER.md), [HTTPS-Namen für Apps](docs/APP-PROXY.md) |
| Kameras | [NVR](docs/NVR.md) |
| Hardware und Fernzugriff | [Plugins](docs/PLUGINS.md), [NVIDIA](docs/NVIDIA.md), [Tailscale](docs/TAILSCALE.md), [NetBird](docs/NETBIRD.md), [Netzwerk](docs/NETWORKING.md), [Cluster](docs/CLUSTER.md) |
| API und Medien | [Öffentliche API/CLI](docs/PUBLIC-API.md), [VM-Medien](docs/VM-MEDIA.md), [Plugin-Aufräumen](docs/PLUGIN-CLEANUP.md) |
| Integrationen | [Home Assistant/MQTT](docs/HOME-ASSISTANT.md), [KI-Agent](docs/AGENT.md), [MCP-Server](docs/MCP.md) |
| Entwicklung und Veröffentlichung | [Entwicklungsumgebung](docs/DEVELOPMENT.md), [Release-Ablauf](docs/RELEASING.md), [UI-Entwürfe](docs/design/README.md), [Screenshots](docs/screenshots/README.md) |
| Änderungen und Prüfstand | [GitHub Releases](https://github.com/ra5on/TitanOS/releases), [detaillierte Release-Berichte](docs/releases/) |

## Entwicklung

Backend: **Go**, ein statisches `titand`-Binary. Oberfläche: **Svelte + TypeScript**, im Release in das Binary eingebettet. Das Systemabbild entsteht mit mkosi auf Debian 13.

Voraussetzungen: Go 1.27+, Node.js 24+, Python 3 und `make`. Für Systemabbilder zusätzlich mkosi 25.3, `debian-archive-keyring squashfs-tools zstd zip systemd-container`; für Release-Prüfungen `minisign`, für virtuelle Starts `qemu-system-x86 ovmf mtools fdisk`. Die [Entwicklungsanleitung](docs/DEVELOPMENT.md) beschreibt die geprüfte Fedora-44-Einrichtung mit festen Toolchains, GitHub-Anbindung, vollständigem Abbildbau und reproduzierbaren QEMU-Tests für RAID, Docker-Apps und VMs. Details zu Signierung und Veröffentlichung: [RELEASING.md](docs/RELEASING.md).

| Befehl | Ergebnis |
|---|---|
| `make build` | Weboberfläche, `backend/bin/titand` mit eingebetteter UI und `backend/bin/titanosctl` bauen |
| `make test` | Go vet/Tests, Webtests und Svelte-/TypeScript-Prüfung ausführen |
| `make test-release` | Paketierung, Lizenzinventar, Signaturen und Manipulationserkennung prüfen |
| `node scripts/test-ui-*.mjs` | Browserprüfungen mit isolierten Testdaten gegen `npm --prefix web run preview` (Playwright, `UI_URL`, `PLAYWRIGHT_MODULE`) |
| `node scripts/test-ui-crawl.mjs` | Jede Seite eines `titand -dev` öffnen und jeden Knopf klicken, um Darstellungsfehler zu finden (`UI_URL`, `UI_LOGIN=name:passwort`) |
| `node scripts/capture-readme-screenshots.mjs` | Die Bilder dieser README mit Beispieldaten neu aufnehmen (`PW`, `UI_URL`, `OUT`, `ONLY`; siehe [Screenshots](docs/screenshots/README.md)) |
| `make dev` | Backend im Entwicklungsmodus und Vite mit automatischem Neuladen starten |
| `make zip` | Systemabbild und bootfähigen Stick-Inhalt unter `out/` bauen |
| `make zip-dev` | Entwicklungsabbild mit automatischer root-Anmeldung an der lokalen Konsole bauen |
| `make qemu` | UEFI-Testsystem starten; `DISKS=4 make qemu` fügt vier leere virtuelle Testplatten hinzu |

Der Entwicklungsmodus verändert keine Host-Platten. In `backend/` funktionieren `go build ./cmd/titand` und `go test ./...` ohne Node.js; ohne Build-Tag `embedui` wird eine Platzhalterseite ausgeliefert. Für Abbild-Builds sind funktionierende Benutzer-Namespaces mit `newuidmap`/`newgidmap` und passenden `/etc/subuid`-/`/etc/subgid`-Einträgen nötig, alternativ `MKOSI_SUDO=1 make zip`.

## Lizenz und Beiträge

TitanOS ist **Fair Source** unter der [Functional Source License 1.1 (FSL-1.1-ALv2)](LICENSE.md). Nutzen, Ändern und Weitergeben ist privat, im Unternehmen, für Lehre und Forschung erlaubt. Ausgenommen ist das Anbieten eines konkurrierenden kommerziellen Produkts oder Dienstes. Jede Version wechselt zwei Jahre nach ihrem Erscheinen zur **Apache License 2.0**. Maßgeblich ist der unveränderte Lizenztext.

Fremdkomponenten behalten ihre eigenen Lizenzen. Jedes Release enthält ein Lizenzinventar und Quellpaketverweise; die Übersicht steht in [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES). Abgebildete App-Namen und Logos gehören den jeweiligen Projekten.

Beiträge sind willkommen: [CONTRIBUTING.md](CONTRIBUTING.md) erklärt den Ablauf und das Contributor License Agreement. Sicherheitslücken bitte wie in [SECURITY.md](SECURITY.md) beschrieben privat melden.
