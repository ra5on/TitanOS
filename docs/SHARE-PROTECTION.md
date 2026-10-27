# Vorgängerversionen und Ransomware-Schutz (ab 0.29.0)

Beides wird je Freigabe unter **Shares › Bearbeiten › Optionen** eingeschaltet
und gilt für SMB.

## Vorgängerversionen in Windows

Auf Btrfs-Volumes erscheinen die Snapshots des Volumes im Explorer unter
*Eigenschaften › Vorgängerversionen*, ebenso in macOS/Linux-Clients, die
SMB-Snapshots kennen. Samba (`shadow_copy2`) braucht dafür Snapshots mit
Namen `@GMT-JJJJ.MM.TT-hh.mm.ss` in einem Ordner, den das verbundene Konto
durchlaufen darf. Die TitanOS-Snapshots liegen dagegen privat in
`.titanos/snapshots` (nur root) unter frei gewählten Namen. titand hält deshalb
für jeden Snapshot einen schreibgeschützten Snapshot des Snapshots unter
`/mnt/<volume>/.snapshots/@GMT-…` (Zeit der Erstellung in UTC). Er teilt alle
Blöcke mit dem Original und kostet keinen Platz. Die Dateien darin behalten
Besitzer und Rechte; Samba zeigt einer Freigabe nur ihren eigenen Ordner
jedes Snapshots (`shadow:basedir`). Der Ordner `.snapshots` ist im
Dateimanager ausgeblendet.

Abgeglichen wird nach jedem geplanten Snapshot und alle zehn Minuten: neue
Snapshots bekommen ihren @GMT-Zwilling, Zwillinge gelöschter Snapshots werden
gelöscht, und sobald keine Freigabe des Volumes mehr Vorgängerversionen will,
alle. Die Zuordnung steht in `.titanos/shadowcopies/index.json`. Auf ext4/XFS
gibt es keine Snapshots und damit keine Vorgängerversionen.

## Ransomware-Schutz

Mit dem Schutz schreibt Samba jede Umbenennung und jedes Löschen dieser
Freigabe über `full_audit` ins Journal (`smbd_audit`, nur die Pfade). titand
liest mit und sperrt die Freigabe, wenn ein Konto innerhalb einer Minute

- mindestens fünf Dateien auf eine bekannte Ransomware-Endung umbenennt
  (`.locked`, `.encrypted`, `.crypt`, `.lockbit`, `.akira` … Liste in
  `backend/internal/shares/guard.go`), oder
- mindestens 40 Dateien auf dieselbe neue Endung umbenennt, die vorher eine
  andere war.

Gesperrt heißt: die Freigabe ist für alle nur noch lesbar (kein `write list`,
NFS `ro`), offene SMB-Verbindungen werden getrennt (`smbcontrol close-share`),
auf Btrfs entsteht sofort ein Snapshot `guard-<zeit>`, und es gibt die Meldung
`share_guard_locked`. Die Sperre steht als `guardLocked` (Zeit, Konto, Grund,
Zahl der Dateien) in `shares.json` und überlebt Neustarts. Aufheben nur durch
Besitzer/Administratoren unter Shares („Wieder freigeben“), nach einer
einfachen Rückfrage. Beim Bearbeiten der Freigabe bleibt die Sperre bestehen.

Grenzen: Der Schutz sieht nur SMB, nicht NFS, Apps oder den Dateimanager. Er
erkennt Umbenennungen, nicht das Überschreiben von Dateien an Ort und Stelle.
Bis zur Sperre geänderte Dateien sind betroffen; zurück kommen sie aus
Snapshots, Papierkorb oder Backup. Ältere Versionen (Rückweg auf 0.28.x)
ignorieren `guardLocked` und geben die Freigabe wieder frei.

## Sperrfrist für Snapshots

Ein geplanter Snapshot-Auftrag (Werkzeuge › Wartung) kann eine Sperrfrist von
bis zu 365 Tagen haben (`lockDays`). Solange lässt sich ein Snapshot dieses
Auftrags weder von Hand noch durch die Aufbewahrung löschen (Fehler
`snapshot_locked`); die Aufbewahrung holt das nach Ablauf nach. Die Fristen
stehen in `.titanos/snaplocks/locks.json` auf dem Volume. Wer root-Zugriff auf
das NAS hat, kann Snapshots weiterhin mit `btrfs` löschen: Die Sperre schützt
vor Angriffen über Konten der Weboberfläche und SMB, nicht vor einem
übernommenen System.

Geprüft mit Unit-Tests (Erkennungsregeln, Sperren/Entsperren, erzeugte
`smb.conf`/Exports, @GMT-Abgleich, Sperrfrist). Echte Windows-Clients und
echtes Samba-Auditing sind noch nicht geprüft.
