# Konten, Updates und Plugins
_Mitgeliefert mit TitanOS, wird bei Updates ersetzt. Eigene Anleitungen gehören in den Ordner Wissen außerhalb von „TitanOS“._

Diese Anleitung erklärt Rollen und Rechte, die Zwei-Faktor-Anmeldung, Updates von System und Apps mit Rückfall und was du bei Plugins wissen musst.

## Konten und Rollen

| Rolle | Darf |
|---|---|
| **Besitzer** | alles, auch andere Besitzer verwalten. Mindestens ein Besitzer muss bleiben. |
| **Administrator** | alles außer Besitzer verwalten. |
| **Benutzer** | nur die eigenen Freigaben und Dateien; kein Docker, keine VMs, keine Systemverwaltung. |

- Konten verwaltest du auf der Seite **Nutzer**. Der Kontoname ist zugleich der SMB-Benutzer.
- Der Besitzer ist nicht der Linux-Benutzer root; root ist für die Anmeldung gesperrt.
- **Sperren statt löschen:** Ein gesperrtes Konto behält Dateien, Rechte und Einstellungen. Sitzungen, API-Tokens, SMB und SSH wirken sofort nicht mehr. Der letzte aktive Besitzer kann weder gesperrt noch herabgestuft noch gelöscht werden.

### Was der Agent bei Konten darf

- Lesen: `accounts_list` (Rolle, ob Zwei-Faktor-Anmeldung an ist).
- Ändern mit deiner Freigabe: `account_action` (make_owner, make_admin, make_user, delete). Löschen schlägt der Agent nur auf ausdrücklichen Wunsch vor; Sperren ist meist besser.
- **Nie:** Passwörter setzen oder sehen, 2FA oder Passkeys einrichten oder zurücksetzen, Tokens erzeugen.
- Als Benutzerkonto hat der Agent keine Verwaltungswerkzeuge und sieht nur deine Freigaben.

## Rechte auf Freigaben

1. Rechte gibt es je Freigabe und je Konto: **kein Zugriff**, **nur lesen**, **lesen und schreiben**. Eingestellt wird das an der Freigabe unter Shares.
2. Gäste haben höchstens Lesezugriff.
3. NFS kennt keine Konten; dort gelten erlaubte Clients und Netze.
4. Freigaben mit Papierkorb fangen gelöschte Dateien auf.
5. Der Agent arbeitet in Dateien immer mit den Rechten deines Kontos, zusätzlich begrenzt durch seine Ordner-Regeln (die strengere Regel gewinnt).
6. `my_shares` zeigt, welche Freigaben dein Konto öffnen und beschreiben darf; `shares_list` alle Freigaben (Verwaltung).

## Zwei-Faktor-Anmeldung (2FA)

1. Unter **Mein Konto** die Zwei-Faktor-Anmeldung mit einer Authenticator-App (TOTP) einrichten.
2. Die acht **Wiederherstellungscodes** sicher außerhalb des NAS aufbewahren.
3. Alternativ oder zusätzlich **Passkeys** anlegen (braucht HTTPS mit gültigem Zertifikat).
4. Unter Mein Konto › Angemeldete Geräte fremde Sitzungen beenden.
5. Der Agent prüft auf Wunsch mit `accounts_list`, welche Konten noch keine 2FA haben, und erinnert daran. Einrichten musst du selbst.

## System- und App-Updates mit Rückfall

Einstellungen › Updates. TitanOS nutzt den Stable-Kanal; nur gültig signierte Updates werden installiert.

- **App-Update:** ersetzt nur TitanOS und seine Weboberfläche, ohne Neustart des NAS. Container, VMs und Freigaben laufen weiter. Startet die neue App nicht richtig, kehrt TitanOS zur vorigen zurück.
- **Volles Systemupdate:** erneuert auch Kernel und Systempakete. Es wird in den zweiten Systemslot geschrieben und startet auf Probe. Scheitert der Start wiederholt, bootet der vorige Slot.
- **Zurück:** Auf die vorige Version im anderen Slot bzw. die vorige App kann man zurückwechseln.
- **Version wählen:** jede veröffentlichte Version ab 0.46.2, auch eine ältere.
- Laufende Backups, Kopien, Wartung oder Snapshots blockieren die Installation bis zu ihrem Ende. Auch ein laufender Auftrag des Agenten hält App-Updates an.

### Ablauf mit dem Agenten

1. `update_status` lesen: laufende Version, System im anderen Slot, gefundenes Release.
2. `backup_status` prüfen; die Sicherung des Sticks sollte aktuell sein (`stick_backup`).
3. Versionshinweise zusammenfassen: App-Update oder volles Systemupdate (dann Neustart nötig).
4. `update_action` mit `install` vorschlagen; du gibst frei.
5. Nach dem Update `update_status` und `notifications` prüfen.
6. Gibt es Probleme: `update_action` mit `rollback` (System) oder `app_rollback` (App) vorschlagen.

### App-Updates von Docker-Apps

Docker-Apps sind etwas anderes: siehe Anleitung „Docker“ (`app_action` update, automatische Updates mit Rückkehr zu den vorigen Images).

## Plugins (volle Systemrechte!)

Plugins erweitern das System selbst: Treiber (NVIDIA, Netzwerkkarten, Sensoren), Werkzeuge (Intel GPU Tools, Coral USB) und Dienste (Tailscale, NetBird).

- **Ein Plugin wird Teil des Systems und hat volle Rechte.** Es gibt keine Sandbox. Nur Plugins installieren, denen du vertraust.
- **Offiziell:** mit gültiger Signatur von TitanOS. **Community (nicht geprüft):** von einem Link oder einer eigenen Quelle, mit deutlicher Warnung.
- Plugins liegen auf einem Daten-Volume und passen genau zum Kernel des Systems; bei vollen Systemupdates werden passende Fassungen vorbereitet.
- Vorschläge für erkannte Hardware werden nie automatisch installiert.

### Was der Agent bei Plugins tut

1. `plugins_list` lesen: installiert, an/aus, offiziell oder Community, Probleme, verfügbare Updates.
2. `hardware_status` zeigt, ob passende Hardware da ist und welcher Treiber fehlt.
3. Installieren (`plugin_install`), Schalten (`plugin_switch`), Aktualisieren (`plugin_update`) und Entfernen (`plugin_remove`) nur mit deiner Freigabe.
4. Vor jeder Installation sagt der Agent ausdrücklich: volles Systemrecht, Herkunft, ob offiziell.
5. Community-Plugins schlägt der Agent nicht von sich aus vor.
6. NVIDIA-Treiber aus dem Archiv werden auf dem NAS gebaut und erst beim nächsten Neustart aktiv.

## Gute Gewohnheiten

- Jedes Konto mit 2FA, nur so viele Administratoren wie nötig.
- Vor Updates sichern, danach prüfen.
- Plugins sparsam einsetzen und Unbenutztes ausschalten oder entfernen.
