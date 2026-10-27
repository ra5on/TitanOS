# Konten sperren

Seit 0.28.0 können Besitzer und Administratoren ein Konto vorübergehend
sperren, statt es zu löschen, und später wieder entsperren. Dateien,
Freigaben, Freigaberechte, Rolle, Passwort, Zwei-Faktor-Anmeldung, SSH-Schlüssel
und persönliche Einstellungen bleiben dabei erhalten.

## Bedienung

Seite **Nutzer**: Spalte „Status“ mit „Aktiv“ bzw. „Gesperrt“ (mit Zeitpunkt
und sperrendem Konto), Knopf „Sperren“/„Entsperren“ je Zeile und ein
Statusfilter (Alle/Aktiv/Gesperrt) über der Tabelle. Sperren und Entsperren
laufen über einen einfachen Bestätigungsdialog, der die Folgen nennt; nichts
muss eingetippt werden. Der Filter gilt nur für den aktuellen Seitenbesuch und
schreibt nichts auf den Stick.

API (nur Besitzer/Administratoren, normale Sitzung oder interne Wege mit
denselben Rechten):

- `POST /api/v1/users/{name}/lock` – sperrt, Antwort ist der Kontoeintrag.
- `POST /api/v1/users/{name}/unlock` – entsperrt.
- `GET /api/v1/users` liefert je Konto `disabled`, bei gesperrten Konten
  zusätzlich `disabledAt` und `disabledBy`.

Regeln (serverseitig in `auth.Users.SetDisabled` und `api.handleLockUser`):

- Niemand sperrt sich selbst (`409 cannot_lock_self`).
- Besitzer werden nur von Besitzern gesperrt oder entsperrt (`403 forbidden`),
  wie bei Rollenwechsel und Löschen.
- Der letzte nicht gesperrte Besitzer kann weder gesperrt noch herabgestuft noch
  gelöscht werden (`409 last_active_owner`). Ein gesperrter Besitzer darf
  herabgestuft oder gelöscht werden, solange ein anderer Besitzer aktiv ist.
- Erneutes Sperren eines gesperrten Kontos ändert nichts und schreibt nicht auf
  den Stick (Zeitpunkt und sperrendes Konto bleiben).
- Ein von einem Administrator gesetztes neues Passwort hebt die Sperre nicht auf.

## Wirkung

| Zugang | Verhalten bei Sperre |
|---|---|
| Weboberfläche | alle Sitzungen des Kontos enden sofort; neue Anmeldung wird abgelehnt |
| API-Tokens | werden abgelehnt (`401`), solange das Konto gesperrt ist |
| Assistent, Automationen, Freigabe aus Benachrichtigungen | laufen nicht als gesperrtes Konto (`activeUser`) |
| Home Assistant | ein gesperrtes Konto als HA-Konto gilt wie kein Konto (keine Knöpfe, keine Assist-Antworten in dessen Namen) |
| Konsolen (NAS, Docker, VM-Bildschirm, seriell) | offene Konsolen enden mit der nächsten Prüfung (alle 3 Sekunden) |
| SSH | Konto verlässt `titanos-ssh`, Schlüsseldatei entfällt, laufende Sitzungen enden (`loginctl terminate-user`) |
| SMB | Samba-Konto mit Flag `D` (Import-Datei und zusätzlich `smbpasswd -d`), offene SMB-Verbindungen des Kontos werden beendet (`smbstatus --processes --json`, `kill -TERM` des zugehörigen smbd-Prozesses) |
| Lokale Netzwerkwiederherstellung (TTY2) | lehnt gesperrte Konten ab |

Das Entsperren stellt alles wieder her: Anmeldung mit dem bisherigen Passwort,
SSH mit den bisherigen Schlüsseln, SMB (`smbpasswd -e`) und die bisherigen
API-Tokens. Wer ein kompromittiertes Konto entsperrt, setzt vorher ein neues
Passwort: das macht alle alten Tokens ungültig.

Linux- und Samba-Konten entstehen bei jedem Start neu aus `/config/users/`; die
Sperre wird dabei jedes Mal mit angewendet. Schlägt `smbpasswd -d` fehl, gilt die
Samba-Runde als fehlgeschlagen und wird alle fünf Sekunden wiederholt.

## Anmeldung eines gesperrten Kontos

Ob ein Konto gesperrt ist, erfährt nur, wer Passwort **und** (falls
eingerichtet) Zwei-Faktor-Code richtig angibt: Antwort `403 account_disabled`
(„Dieses Konto ist gesperrt …“). Falsches Passwort oder falscher Code ergeben
weiterhin genau die bisherigen Fehler (`invalid_credentials`, `invalid_code`),
mit derselben Laufzeit und demselben Fehlversuchsschutz. Eine abgelehnte
Anmeldung eines gesperrten Kontos zählt nicht als Fehlversuch, erzeugt keine
Sitzung und steht als `login_disabled` im Aktivitätsprotokoll.

Begründung: Wer das Passwort kennt, soll erfahren, warum die Anmeldung nicht
geht, statt das Passwort für falsch zu halten. Für Dritte ist ein gesperrtes
Konto von einem nicht existierenden nicht zu unterscheiden.

## Speicherung und ältere Versionen

In `/config/users/<konto>/account.json` stehen `disabled`, `disabledAt` und
`disabledBy`. Zusätzlich wird die Sperre so abgelegt, dass auch titand vor
0.28.0 – das diese Felder nicht kennt und beim Lesen ignoriert – das Konto
abweist:

- Der Passwort-Hash bekommt das Präfix `!locked$`. Jede Version lehnt diesen
  Hash als unbekanntes Format ab: keine Web-Anmeldung, keine lokale
  Netzwerkwiederherstellung, und API-Tokens passen nicht mehr (ihr Kontoschlüssel
  ist der Hash des gespeicherten Passwort-Hashes).
- NT-Hash und SSH-Schlüssel stehen während der Sperre in `disabledSmbHash` bzw.
  `disabledSshKeys`. Eine ältere Version sieht das Konto ohne SMB-Passwort
  (Samba-Konto wird entfernt) und ohne Schlüssel (kein SSH).

Im Speicher von titand sind Hash, NT-Hash und Schlüssel unverändert; die
Umwandlung geschieht nur beim Lesen und Schreiben der Datei
(`auth/disabled.go`). Das Präfix allein gilt ebenfalls als Sperre.

Nach einem Rückweg auf eine ältere Version (A/B-Rückfall oder App-Rückfall):

- Gesperrte Konten bleiben dort ausgesperrt, werden aber als normale Konten
  angezeigt; entsperren lässt sich dort nur, indem ein Administrator ein neues
  Passwort setzt.
- Schreibt die ältere Version die Datei neu (z. B. Rollenwechsel oder
  2FA-Zurücksetzen am gesperrten Konto), entfallen `disabled*`, die geparkten
  Felder und damit NT-Hash und SSH-Schlüssel. Das Konto bleibt durch das Präfix
  gesperrt. Nach dem Entsperren in 0.28.0 kommt SMB mit der nächsten
  Web-Anmeldung zurück (wie bei Konten aus der Zeit vor Freigaben), SSH-Schlüssel
  müssen neu hinterlegt werden.

## Grenzen

- NFS kennt keine Konten (Zugriff nach Client-Adresse, `nobody:users`); eine
  Kontosperre wirkt dort nicht.
- Freigaben mit Gastzugriff bleiben wie für jeden Gast erreichbar.
- Bereits laufende Datei-, Download- und Kopieraufträge des Kontos laufen
  zu Ende; neue lassen sich nicht starten.
- Das Beenden offener SMB-Verbindungen und die Samba-Flags sind mit
  nachgebautem Runner getestet (`shares`), SSH mit nachgebautem System
  (`sshd`); gegen echtes Samba/OpenSSH auf dem NAS noch zu prüfen.

## Tests

- `backend/internal/auth/disabled_test.go`: Sperren/Entsperren, letzter aktiver
  Besitzer, Persistenz, Dateiform für ältere Versionen (Hash wird von
  `verifyPassword` abgelehnt, keine SMB-/SSH-Daten, Tokenschlüssel ändert sich),
  Lesen nach Neuschreiben durch eine ältere Version.
- `backend/internal/api/users_lock_test.go`: Rollenregeln, Selbstsperre,
  Sitzungen/Tokens, Anmeldefehler, letzter aktiver Besitzer, Konsolen,
  Aktivitätsprotokoll.
- `backend/internal/shares/shares_test.go` (`TestLockedAccountLosesSMB`),
  `backend/internal/sshd/sshd_test.go` (`TestLockedAccountLosesSSH`).
- `web/tests/users.test.mjs`: Statusfilter, angebotene Sperraktion, Texte.
