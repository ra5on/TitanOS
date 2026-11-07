# Docker: Images, Volumes und Aufräumen

Seit 0.64.0 (PLAN 67-1, Wunsch des Inhabers vom 7.11.2026: „Volumes löschen, Netzwerke erstellen/löschen, Images löschen, die man nicht mehr braucht“). Backend: `backend/internal/docker/resources.go`, Routen in `backend/internal/api/docker_resources.go`. Die LAN-Netze für Apps (macvlan/ipvlan) behalten ihre Routen unter `/api/v1/docker/networks`; einfache Bridge-Netzwerke kommen über die Routen unten dazu (`backend/internal/docker/bridges.go`).

## Grundsätze

- Nur Besitzer und Administratoren. Lesen über `requireAdmin`, Ändern über `requireAppAdmin` (wartet wie andere App-Änderungen nicht auf eine laufende Anwendungssicherung: 423 `application_backup_running`).
- titand entscheidet selbst, was weg darf. Die Prune-Endpunkte der Engine für Images und Volumes werden **nie** aufgerufen („alle ungenutzten“ würde auch die Images der Fernansicht und der Sandbox des KI-Agenten nehmen). Gelöscht wird einzeln und **nie mit force**; benutzt ein Container ein Image oder Volume doch noch, lehnt die Engine selbst ab.
- „Benutzt“ heißt: irgendein Container, laufend oder gestoppt, auch Container der Fernansicht und der Sandbox.
- **Geschützt** (nie löschbar, nicht in „ungenutzt“ gezählt): Images der Fernansicht (Labels `titanos.viewer.image=1` und `titanos.viewer.release`), das festgelegte Sandbox-Image des KI-Agenten (Digest aus `docker.SandboxImage`), Volumes von TitanOS (Name `titanos-…`/`titanos_…` oder ein Label `titanos.…`; heute legt TitanOS selbst keine benannten Volumes an, die Regel schützt künftige).
- Während eine App eingerichtet oder aktualisiert wird (auch automatische Updates, die alte Images für den Rückweg behalten) und während einer Anwendungssicherung wird nichts gelöscht: 409 `app_busy`.
- Docker aus, nicht gestartet: 409 `docker_not_running`; Entwicklungsmodus: 409 `read_only`.

## Routen

| Route | Antwort |
|---|---|
| `GET /api/v1/docker/images` | `{images:[{id, repoTags[], size, sharedSize, created, usedBy:[{app, container, running}], dangling, protected?}], totals:{count, size, unusedCount, unusedSize, danglingCount, danglingSize}}` |
| `DELETE /api/v1/docker/images/{id}` | 204. `id` = `sha256:<hex>` oder mindestens 12 Hex-Zeichen. 409 `image_in_use`, 409 `image_protected`, 404 `docker_not_found` |
| `POST /api/v1/docker/images/prune` | Body `{"unused":false}` (ab Werk, auch ohne Body: nur unbenannte/verwaiste Images) oder `{"unused":true}` (alle ungenutzten). Antwort `{deleted:[ids], failed:[{id, error}], reclaimed}` |
| `GET /api/v1/docker/volumes` | `{volumes:[{name, driver, mountpoint, size, usedBy, labels, createdAt, app?, anonymous, titanos}]}` |
| `DELETE /api/v1/docker/volumes/{name}` | 204. 409 `volume_in_use`, 409 `volume_protected`, 404 `docker_not_found`. **Datenverlust:** die Oberfläche fragt vorher (einfacher Bestätigungsdialog, nichts eintippen) |
| `POST /api/v1/docker/volumes/prune` | Body `{"anonymousOnly":true}` (ab Werk, auch ohne Body) oder `false` (auch benannte). Antwort wie oben, `deleted` sind Namen |
| `POST /api/v1/docker/builder/prune` | `{reclaimed, deleted}` (Anzahl der Cache-Einträge); leert den ganzen Build-Cache (`all=true`), was gerade gebaut wird, bleibt |
| `GET /api/v1/docker/df` | `{images, containers, volumes, buildCache:{count, size, reclaimable}, anonymousReclaimable, reclaimable, measuredAt, measuring}`; `?fresh=1` zählt neu |
| `GET /api/v1/docker/networks/all` | `{networks:[{id, name, driver, scope, kind, subnets[], gateways[], internal, usedBy:[{app, container, running}], app?, protected, createdAt}]}`, sortiert `user`, `app`, `lan`, `system`, `titanos` |
| `POST /api/v1/docker/networks/bridge` | Body `{"name":"media", "subnet":"10.50.0.0/24", "internal":false}` (`subnet` und `internal` freiwillig) → 201 mit dem Netzwerk wie in der Liste. 400 `invalid_network`, 409 `network_exists`, 409 `network_overlap` |
| `DELETE /api/v1/docker/networks/bridge/{name}` | 204 (`name` oder ID). 409 `network_in_use`, 409 `network_protected`, 404 `docker_not_found` |
| `POST /api/v1/docker/networks/prune` | ohne Body; `{deleted:[namen], failed:[{id, error}], reclaimed:0}` |

Einzelheiten:

- `size` eines Images enthält die mit anderen Images geteilten Ebenen, `sharedSize` diesen Teil (-1 unbekannt). Freigegeben wird etwa `size - sharedSize`; `unusedSize`, `danglingSize` und `reclaimed` sind deshalb Näherungen. `totals.size` zählt geteilte Ebenen je Image; die echte Belegung steht in `df.images.size`.
- Gelöscht wird über die ID. Mehrfach benannte Images verlieren vorher alle Namen bis auf einen (die Engine lehnt das Löschen über die ID ohne force sonst ab).
- `usedBy[].app` ist das Compose-Projekt (= App), leer für Container außerhalb von Apps sowie für Fernansicht und Sandbox.
- Volumes: `size` aus der Platzzählung der Engine (`/system/df`), -1 solange unbekannt (Zählung läuft noch, Treiber ohne Angabe). Die Zählung läuft im Hintergrund, unabhängig von der Anfrage (höchstens 10 Minuten), nie doppelt (die Engine lehnt eine zweite ab), und wird 2 Minuten wiederverwendet; eine ältere wird sofort geliefert und im Hintergrund erneuert. Jede Löschung verwirft sie (auch eine gerade laufende wird dann nicht übernommen). Die Volume-Liste wartet höchstens 3 s auf eine Zählung, `GET /docker/df` höchstens 30 s; läuft sie dann noch, antwortet df mit `measuring:true` (ohne frühere Zählung alle Werte 0, `measuredAt` leer) – die Seite fragt etwas später erneut. `mountpoint` sehen nur Administratoren (alle Routen sind admin-only).
- `anonymous`: Label `com.docker.volume.anonymous` (Docker ≥ 23) oder ein 64-stelliger Hex-Name ohne Compose-Projekt.
- Die Volume-Bereinigung lässt außerdem Volumes von **gespeicherten Apps** stehen (Label `com.docker.compose.project` = App unter `/config/apps`), auch wenn gerade kein Container sie benutzt (App heruntergefahren, Daten bleiben). Einzeln löschen geht nach Rückfrage.
- `df.containers.reclaimable` sind die beschreibbaren Ebenen gestoppter Container (entfernen unter Docker › Container, nicht auf der Aufräumen-Seite); `df.reclaimable` = Images + Volumes + Build-Cache. `df.volumes.reclaimable` zählt alle ungenutzten, nicht geschützten Volumes außerhalb gespeicherter Apps, `anonymousReclaimable` davon die anonymen (Voreinstellung der Bereinigung).
- Netzwerke, `kind`: `system` (bridge, host, none der Engine), `lan` (LAN-Netze `titanos-lan-…`, verwaltet über die LAN-Routen), `titanos` (andere `titanos-…`/`titanos_…` oder Label `titanos.viewer`, z. B. Netze der Fernansicht), `app` (Compose-Projekt einer gespeicherten App oder ein Netzwerk, das deren Compose-Datei als ganzes Wort nennt, z. B. ein `external`-Netz; `app` nennt die App), `user` (alles andere, auch übrig gebliebene Netze entfernter Apps). Nur `user` ist löschbar (`protected:false`), und nur, wenn kein Container (auch kein gestoppter) es benutzt. Die Bereinigung löscht alle ungenutzten `user`-Netze; nie force, nie der Prune-Endpunkt der Engine.
- Neues Bridge-Netzwerk: Name `^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`, nicht `bridge`/`host`/`none`, nicht mit `titanos` beginnend, noch nicht vorhanden. `subnet` freiwillig (sonst wählt die Engine): privates IPv4-Netz /8 bis /29, genau die Netzadresse, ohne Überschneidung mit Netzen der Engine, den LAN-Netzen und den Netzen der Anschlüsse des NAS. Die Engine setzt das Gateway (erste Adresse). Netze werden nacheinander angelegt/gelöscht (gleiche Sperre wie die LAN-Netze), damit die Prüfung auf Überschneidung hält.
- Aktivitätsprotokoll: alle ändernden Routen landen automatisch darin (Muster der Route als Aktion, `{id}`/`{name}` als Ziel; bei Bereinigungen zusätzlich `dangling`/`unused` bzw. `anonymous`/`all unused` und die gelöschten Einträge, höchstens die ersten).
- Nicht in `publicRoutes` und nicht im Werkzeugkatalog (kein Token, kein MCP, kein KI-Agent).

## Prüfstand und Grenzen

- Tests mit nachgebildeter Engine (`docker/resources_test.go`): Benutzung durch gestoppte Container und die Fernansicht, Schutz von Fernansicht- und Sandbox-Image, mehrfach benannte Images, nie force, kein Engine-Prune, Volumes gespeicherter Apps, Platzzählung im Hintergrund (langsame Zählung hält weder Liste noch Löschen auf, Zählung vor einer Löschung wird verworfen), Build-Cache; Netzwerke (`docker/bridges_test.go`: Arten, Benutzung auch ohne Netzwerk-ID, Anlegen mit allen Prüfungen, Löschen/Bereinigen, Schutz der Netze gespeicherter Apps); API-Tests (`api/docker_resources_test.go`): nur Administratoren, Entwicklungsmodus, Fehlercodes.
- Nicht gegen eine echte Docker-Engine abgenommen. Zwischenimages des klassischen Builders (`/images/json?all=true`) werden nicht gelistet. Images einer gespeicherten App ohne Container (z. B. gescheiterte Einrichtung) gelten als ungenutzt; eine Neuinstallation lädt sie wieder.
