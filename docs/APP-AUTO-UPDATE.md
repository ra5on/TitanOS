# Automatische App-Updates

Seit 0.35.0: Docker › **Automatische Updates …** (nur Besitzer und
Administratoren). Ab Werk aktualisiert sich keine App von selbst.

## Bedienung

- Apps ankreuzen, die neue Images selbst holen sollen.
- **Zeitfenster:** Wochentage (keiner = jeden Tag) und Uhrzeit in der
  Zeitzone des NAS. Das Fenster ist eine Stunde offen; laufen gerade eine
  Sicherung, eine Anwendungssicherung, VM-Arbeit, eine Plugin-Aktion oder ein
  Speicherumbau, wartet TitanOS innerhalb der Stunde.
- Je App zeigt der Dialog das letzte Ergebnis: aktualisiert, zurückgenommen,
  fehlgeschlagen oder war aktuell.

## Ablauf

1. Registries abfragen (wie „Nach Updates suchen“); nur Apps mit neueren
   Images werden angefasst, eine nach der anderen.
2. Laufende Images je Dienst merken, `docker compose pull`, dann `up` ohne
   weiteren Pull.
3. **Drei Minuten beobachten:** Jeder Container der App muss laufen; einer mit
   Healthcheck darf nicht „unhealthy“ sein (bei „starting“ wartet TitanOS bis
   zum Ende der drei Minuten).
4. **Rückweg:** Andernfalls (oder wenn Pull/Start scheitert) werden die
   vorigen Images mit `docker image tag <alte ID> <Name:Tag>` zurückgesetzt und
   die App mit ihnen gestartet. Die alten Images bleiben dafür liegen, bis sie
   jemand entfernt.

Meldungen: `docker_auto_updated` (Hinweis), `docker_update_rolled_back`
(Warnung), `docker_update_failed` (Fehler, der Rückweg gelang nicht ganz).
Images mit Digest statt Tag werden nicht aktualisiert (wie bei „Nach Updates
suchen“). Daten der App sind nicht Teil des Rückwegs: Ändert ein neues Image
beim ersten Start sein Datenformat, kann das alte damit eventuell nicht mehr
arbeiten – für wichtige Apps vorher eine Anwendungssicherung einrichten.

## Technik

`docker/autoupdate.go`, Einstellungen `/config/docker/auto-update.json`,
Ergebnisse nur im RAM. API: `GET`/`PUT /api/v1/docker/auto-update`.

Geprüft mit nachgebautem Ablauf (`autoupdate_test.go`): behaltenes Update,
Rückweg bei „unhealthy“, zu langem „starting“ und gescheitertem Pull, nichts
Neues, Zeitfenster und nächster Termin. **Nicht geprüft:** echte Registries und
Container.
