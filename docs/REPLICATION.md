# Replikation auf ein anderes TitanOS (ab 0.29.0)

Ein Btrfs-Volume lässt sich Snapshot für Snapshot auf ein zweites TitanOS
spiegeln, etwa als Kopie außer Haus. Paket `backend/internal/replication`,
Oberfläche unter **Einstellungen › Sicherungen › Datensicherung ›
Replikation auf ein anderes TitanOS**.

## Einrichten

1. Auf dem **empfangenden** NAS „Backups anderer TitanOS annehmen“
   einschalten, ein **Btrfs**-Volume wählen und einen sendenden NAS
   hinzufügen. Der Kopplungscode wird einmal angezeigt.
2. Auf dem **sendenden** NAS „Replikation einrichten“: Name, Btrfs-Volume,
   Kopplungscode, Abstand in Stunden (1–168), Zahl der Stände auf dem
   anderen NAS (2–365) und optional eine Grenze in MiB/s.

Beide NAS brauchen Tailscale. Der Empfänger ist derselbe kleine Server wie für
restic-Backups anderer TitanOS (`backuptarget`): Er lauscht nur auf seinen
Tailscale-Adressen, Port 8750, und kennt jeden Sender an Name und Token. Unter
`/<sender>/_replica/` antwortet er mit der Liste der vorhandenen Stände
(`GET`) bzw. nimmt mit `PUT /<sender>/_replica/<stand>` einen Datenstrom von
`btrfs send` entgegen. Dasselbe Token gilt für Backups und Replikation.

## Ablauf eines Laufs

- Der Sender legt einen schreibgeschützten Snapshot des ganzen Volumes unter
  `.titanos/replicated/<auftrag>/<UTC-Zeit>` an.
- Er fragt den Empfänger, welche Stände dieser hat, und schickt den neuen
  Snapshot mit `btrfs send -p` als Differenz zum neuesten gemeinsamen Stand;
  beim ersten Lauf alles.
- Der Empfänger schreibt mit `btrfs receive -e` zuerst in einen eigenen Ordner
  `.incoming`, erst danach erscheint der Stand unter
  `.titanos/replicas/<sender>/<stand>`. Ein abgebrochener Empfang hinterlässt
  keinen halben Stand; Reste werden beim nächsten Empfang entfernt.
- Danach löscht der Empfänger Stände über der gewählten Zahl (mindestens zwei
  bleiben), der Sender behält seine letzten zwei Snapshots als Grundlage für
  die nächste Differenz. Scheitert ein Lauf, wird sein neuer Snapshot gelöscht
  und es gibt die Meldung `replication_failed`; ein neuer Versuch folgt nach
  30 Minuten.
- Laufende Replikationen halten App-Updates und Wartung an.

Unter-Subvolumes (zum Beispiel die Daten von Docker) gehören nicht zu einem
Btrfs-Snapshot und werden nicht übertragen.

## Wiederherstellen

Auf dem empfangenden NAS listet „Von anderen NAS empfangen“ alle Stände. „Als
neuen Ordner wiederherstellen“ legt einen beschreibbaren Snapshot unter
`/mnt/<volume>/restored/<sender>-<stand>` an; das Replikat bleibt unverändert.
Was man braucht, holt man sich von dort.

## Einstellungen und Grenzen

Aufträge stehen in `/config/backup/replication.json` (eigene Datei, ältere
Versionen ignorieren sie); das Token wird über die API nie herausgegeben.
Der Zustand der Läufe liegt im RAM.

Geprüft mit Unit-Tests, die Sender und Empfänger über einen echten
HTTP-Server mit nachgebautem `btrfs` verbinden (erste und folgende Läufe,
Differenzen, Aufbewahrung, falsches Token, abgebrochener Empfang,
Wiederherstellen). Echtes `btrfs send/receive` zwischen zwei NAS über
Tailscale ist noch nicht geprüft.
