# Kopieren und Verschieben im Dateimanager

Kopieren und Verschieben laufen als Hintergrundauftrag in titand
(`files/copy.go`, `POST /api/v1/files/copy`, Liste unter `/files/jobs`). Diese
Seite beschreibt den Ablauf seit der Überarbeitung vom 4.11.2026
(Entscheidungen des Projektinhabers vom selben Tag).

## Dialog

- Ziel wählen (Wurzel und Ordner). Sobald ein Ziel feststeht, prüft der Dialog
  es (`POST /api/v1/files/inspect`, 15 Sekunden): Umfang, gleiche Namen mit
  Größe und Änderungszeit beider Seiten, gleiche Dateien, unlesbare Ordner.
  Die Prüfung schreibt nichts.
- „Gibt es den Namen schon“:
  - **Beide behalten** (Standard): die neue Datei heißt `name (2).ext`.
  - **Nur fehlende oder geänderte Dateien** (`missing`): Dateien mit gleicher
    Größe und einer Änderungszeit, die höchstens zwei Sekunden abweicht (FAT
    speichert in Zwei-Sekunden-Schritten), bleiben unberührt; geänderte
    werden ersetzt, fehlende kopiert. Findet die Prüfung gleiche Dateien, ist
    diese Regel vorgewählt und der Dialog nennt Anzahl und Größe – außer der
    Nutzer hat schon selbst eine Regel gewählt. Kopien behalten die
    Änderungszeit, ein zweiter Lauf erkennt sie daher als gleich.
  - **Überspringen**: vorhandene Dateien bleiben. Beim Verschieben bleibt die
    Quelle übersprungener Dateien liegen und steht in der Liste des Auftrags.
  - **Ersetzen**: vorhandene Dateien werden ersetzt. Trifft eine Datei auf
    einen Ordner (oder umgekehrt), wird der Eintrag ausgelassen und gemeldet.
  - **Für jede Datei entscheiden**: bis 200 Konflikte einzeln.
- Ordner werden immer zusammengeführt, auch beim Verschieben innerhalb einer
  Freigabe (dort per Umbenennen, Eintrag für Eintrag; Einzelentscheidungen
  bleiben dabei schnelle Umbenennungen).
- **Ersetzte Dateien** kommen in den Papierkorb der Freigabe, in deren Ordner
  sie liegen (`.recycle/<konto>/…`), wenn deren Papierkorb eingeschaltet ist –
  auch beim Kopieren in die Volume-Ansicht. Sonst sagt der Dialog vor der
  Bestätigung, dass ersetzte Dateien endgültig weg sind (USB-Laufwerke, Stick,
  Freigaben ohne Papierkorb). Ersetzen und „Nur fehlende oder geänderte“ mit
  geänderten Dateien brauchen ein Häkchen zur Bestätigung (kein Eintippen).
- Freier Speicher: der Dialog zeigt, was davon wirklich geschrieben wird;
  titand prüft beim Start nur diese Bytes (übersprungene und gleiche Dateien
  zählen nicht).
- Dauert die Prüfung zu lange, starten Regeln für alle trotzdem; der Auftrag
  entscheidet dann für jede Datei. „Für jede Datei entscheiden“ ist gesperrt
  und der Dialog sagt warum (mehr als 200 Konflikte, Zeitlimit, Fehler).
- Kommt die Prüfung erst beim Klick zurück und findet gleiche Namen, startet
  noch nichts: ein Hinweis nennt die Anzahl, die Liste wird ins Bild
  gescrollt, der zweite Klick startet.
- In den eigenen Ordner kopieren geht nur mit „Beide behalten“ (oder
  Einzelentscheidungen mit neuem Namen); Verschieben dorthin ist gesperrt.
  titand antwortet sonst mit `copy_same_folder`, ein Ordner in sich selbst
  mit `copy_into_itself`.

## Auftrag

- Die Anfrage kehrt sofort zurück. Der Auftrag zählt die Auswahl einmal
  („Auswahl wird gezählt …“), legt dabei für Ziele auf Daten-Volumes den
  dauerhaften Plan an und prüft den Platz; reicht er nicht, endet der Auftrag
  mit „Im Ziel ist nicht genug Speicher frei“.
- **Ein Fehler hält nicht alles an.** Lässt sich eine Datei nicht übertragen,
  steht sie mit Grund in der Liste des Auftrags und der Rest läuft weiter.
  Der Auftrag endet dann als „Mit Fehlern abgeschlossen“. Gründe sind u. a.:
  zu groß für FAT32 (`EFBIG`), Name auf dem Datenträger nicht erlaubt
  (`EINVAL`), Name zu lang, Lese- bzw. Schreibfehler (`EIO`), keine
  Berechtigung, unlesbarer Ordner, während der Übertragung geändert, Typ am
  Ziel verschieden. Nur was alle weiteren Dateien genauso trifft, beendet den
  Auftrag: kein Platz, Volume weg, der eigene Stand lässt sich nicht
  speichern, Abbrechen.
- **Verschieben** entfernt Quellen Datei für Datei und danach leere Ordner,
  und nur, was vollständig kopiert wurde, sich seitdem nicht geändert hat und
  dessen Kopie am Ziel noch unverändert ist. Alles andere bleibt an der Quelle
  und steht in der Liste: übersprungen, Verknüpfung (Symlink), Sonderdatei,
  interne Datei, während des Verschiebens hinzugekommen, nach dem Kopieren
  geändert. Bei „Nur fehlende oder geänderte“ gilt eine gleiche Datei am Ziel
  als schon verschoben: ihre Quelle wird entfernt.
- Interne Teildateien (`.titanos-copy-*.titanos-part`) eines unterbrochenen
  Vorgangs werden in einer verschobenen Quelle entfernt, wenn sie älter als
  24 Stunden sind und zu keinem laufenden Auftrag gehören; jüngere bleiben und
  werden gemeldet.
- Rückgängig: wie bisher für Verschiebungen an einen freien Namen; nicht für
  zusammengeführte Ordner und nicht für Einträge, deren Quelle teilweise
  liegen blieb. Ersetzte Dateien holt man aus dem Papierkorb zurück.

## Wiederholen und Neustart

- „Fehlgeschlagene erneut versuchen“ (bzw. „Offene Dateien erneut versuchen“)
  setzt denselben Auftrag fort: Fertige Dateien bleiben unberührt, die
  gemeldeten werden erneut versucht, inzwischen geänderte oder neue Dateien
  der Quelle werden so übertragen, wie sie jetzt sind. Der Vergleich der
  Quelle nutzt Gerät, Inode, Größe und Änderungszeit, nicht die
  Änderungszeit der Metadaten (ctime), sodass ein `chmod` oder ein neuer
  Hardlink keinen Neuplan erzwingt. Am Ziel inzwischen bearbeitete fertige
  Dateien blockieren die Wiederholung nicht mehr.
- Nach einem Neustart des NAS lässt sich ein gespeicherter Auftrag nicht mehr
  wiederholen (`rebooted`). Die Auftragsliste bietet stattdessen „Fehlende
  Dateien kopieren“: ein neuer Auftrag mit derselben Auswahl und der Regel
  „Nur fehlende oder geänderte Dateien“.

## Aufräumen

- Jede Teildatei einer Kopie auf ein Daten-Volume hinterlässt eine Notiz in
  `.titanos/uploads` des Volumes (wie Uploads). Die stündliche Bereinigung
  entfernt Teildateien, die älter als 24 Stunden sind und zu keinem laufenden
  Auftrag gehören. Auf USB-Laufwerken gibt es keine Notizen; dort bleiben
  Reste nach einem Absturz liegen, bis ein Verschieben sie trifft.

## Papierkorb in der Volume-Ansicht

- Administratoren sehen in der Volume-Ansicht einen Papierkorb-Knopf, wenn auf
  dem Volume Freigaben mit eingeschaltetem Papierkorb liegen. Er öffnet die
  Liste dieser Papierkörbe (bei nur einem direkt dessen Ansicht);
  `GET /api/v1/files/trash?root=volume/<name>&bin=<ordner>`.
- Löschen in der Volume-Ansicht legt Einträge im Ordner einer Freigabe mit
  Papierkorb in deren `.recycle/<konto>/<pfad in der Freigabe>`, wie beim
  Löschen über die Freigabe. Alles andere (auch der Freigabeordner selbst)
  wird nach Bestätigung endgültig gelöscht; eine gemischte Auswahl nennt
  beide Teile.
- USB-Laufwerke und der Stick haben keinen Papierkorb; der Löschdialog sagt
  „Hier gibt es keinen Papierkorb“.
- Auf dem Smartphone ist der Papierkorb ein eigener Knopf in der Leiste.

## Fehlercodes

Einzelne Einträge (`job.problems[].code`, Texte `files.copyProblem.*`):
`too_large`, `name_too_long`, `name_not_allowed`, `read_error`, `write_error`,
`io_error`, `read_only`, `permission`, `missing`, `changed`,
`changed_after_copy`, `changed_target`, `exists`, `into_itself`,
`same_folder`, `protected`, `type_mismatch`, `unreadable_folder`,
`recycle_failed`, `remove_failed`, `other`; an der Quelle belassen:
`kept_skipped`, `kept_link`, `kept_special`, `kept_internal`, `kept_new`,
`kept_changed`, `kept_target_changed`.

Ganzer Auftrag (`job.errorCode`, Texte `files.copyError.*`): `no_space`,
`unavailable`, `store`, `into_itself`, `same_folder`, `rebooted`, `missing`,
`permission`, `changed`, `interrupted`, `unknown_outcome`, `other`.

API-Fehler beim Start: `copy_into_itself`, `copy_same_folder`,
`copy_source_protected` (Stick), `copy_source_changed`, `copy_rebooted`,
`file_access_denied`.

## Prüfung

- Go: `backend/internal/files/copy_regression_test.go` (je Befund F1–F14 und
  Papierkorb in der Volume-Ansicht) sowie die angepassten bestehenden Tests;
  `go test -race`.
- Browser: `scripts/test-ui-copy-review.mjs` (vorgewählte Regel, Hinweis statt
  stummem Klick, Zeitlimit, Fehlerliste, „Fehlende Dateien kopieren“,
  Papierkorb-Knöpfe) und `scripts/test-ui-copy-confirmation.mjs`, nur mit
  Testdaten im Entwicklungsserver.
- Nicht geprüft: echte FAT32-/exFAT-USB-Laufwerke (EFBIG/EINVAL wurden
  simuliert), echte Lesefehler einer Platte, Neustart eines echten NAS.
