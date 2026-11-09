# Komplettdurchsicht 0.75 (PLAN 76) – Fundliste

Jeder Fund hat eine Nummer `F-<Punkt>-<lfd>`, eine Art und einen Stand.

Arten: **Fehler** (falsches Verhalten), **Defekt** (Funktion geht nicht), **Lücke** (sinnvolle Funktion fehlt), **Altlast** (toter oder doppelter Code), **Risiko** (Sicherheit).
Stand: offen · behoben (PR) · bewusst gelassen (Grund) · beim Inhaber.

## 1. Ausgangslage

| Nr. | Art | Wo | Befund | Stand |
|---|---|---|---|---|
| F-1-1 | – | ganzes Repo | Ausgangswerte 10.11.2026 auf `main` nach 0.74: `go vet`, staticcheck, govulncheck (0 im aufgerufenen Code), errcheck (Projekteinstellung), `go test` und `go test -race` aller Pakete, svelte-check (862 Dateien), Prettier, Webtests (111), shellcheck, Skripttests (117): alles grün. | – |
| F-1-2 | Altlast | `scripts/*.py`, `backend/tests`, `docs/apps-repo` | pyflakes: 10 ungenutzte Importe, 2 ungenutzte Variablen, 1 f-String ohne Platzhalter. | behoben |
| F-1-3 | Altlast | `web/` | `npm audit`: 4 × niedrig (deepmerge über `@sveltejs/vite-plugin-svelte`, nur Bauwerkzeug, kein Code im Abbild). Kein Fix ohne Rückschritt von SvelteKit verfügbar. | bewusst gelassen, in 12.7 erneut prüfen |
| F-1-4 | Altlast | `backend/` | deadcode: keine tote Funktion im Produktivcode. Nur von Tests benutzt: `agentDue`, `agentInbox`, `backup.copyFile`, `backup.remoteSMB`, `Job.sourceVersion`, `usb.waitFormat`, `homeassistant.WithRelease`, `webapps.Catalog`, `nalTypes`. | bewusst gelassen (kleine Testhelfer) |
| F-1-5 | Lücke | `homeassistant/watch.go` `Match` | Auswertung von Schwellwerten (über/unter) ist fertig und getestet, aber nirgends angeschlossen; Auslöser des Agenten kennen nur einen exakten Zustand. | offen → 11.2 |
| F-1-6 | – | CI | Von 74 UI-Suites (`scripts/test-ui-*.mjs`) läuft in der CI nur `test-ui-crawl`. | offen → 1.3/13.3 |

## 2. Sicherheit und Zugang

Geprüft und in Ordnung (2.1/2.2, gelesen, nicht gegen ein laufendes System ausgeführt): 607 Routen, jede ausdrücklich mit `requireAdmin`/`requireOwner`/`requireUser`/`requireAgent`/`setupLocal` registriert; ohne Sitzung nur die in den Regeln genannten; API-Tokens nur über `publicRoutes`; keine Route mit zu niedriger Rolle, kein Zugriff auf fremde Einträge über `{id}` gefunden. Passwörter argon2id mit konstantem Vergleich und Scheinhash, keine Kontenerkennung, TOTP ohne Wiederverwendung, Sitzungen enden bei Passwortwechsel/Sperre/Löschen/2FA-Reset, Rollenwechsel wirkt sofort. WebAuthn: Challenge einmalig, Herkunft/RP-ID/UV geprüft, Zählerrückschritt abgelehnt. Alle ändernden Methoden hinter `http.CrossOriginProtection`; Cookies `__Host-`, HttpOnly, Secure, SameSite=Strict. 2.4: Befehle laufen ohne Shell über `run.Exec`; Abrufe mit fremdbestimmter Adresse (Link-Download, Benachrichtigungen, Web-Push, Web-Werkzeuge des Agenten, Katalog, Plugins) prüfen die Zieladresse beim Verbinden (`httpfetch`, `netguard`).

| Nr. | Art | Wo | Befund | Stand |
|---|---|---|---|---|
| F-2-1 | Risiko (mittel) | `api/cluster.go` `handleClusterProxy`, `cluster/proxy.go` | Der Cluster-Proxy reicht WebSocket-Verbindungen ohne Herkunftsprüfung weiter und entfernt `Origin`. Eine Seite gleicher Site, aber anderer Herkunft (App unter eigenem Namen) konnte mit dem Cookie eines Administrators die Konsole eines anderen Knotens öffnen. | behoben: `Origin` muss die eigene Adresse nennen, Test in `TestClusterThroughTheAPI` |
| F-2-2 | Risiko (mittel) | `api/docker.go` `handleConfirm` | `POST /session/confirm` prüfte das Fehlversuchslimit nur vor dem Hashen: gleichzeitig geschickte Rateversuche bekamen alle eine Antwort. | behoben: läuft über `reconfirm`, `TestParallelConfirmGuessesAreLimited` (ohne Korrektur 12 von 12 beantwortet) |
| F-2-3 | Risiko (mittel) | `auth/limiter.go`, Schlüssel `ip:` | Volle Tabelle (10 000) warf den ältesten ungesperrten Eintrag hinaus, auch ein gerade geratenes Konto; IPv6 zählte je Adresse statt je /64. Damit ließ sich der Zähler eines Kontos nach jeder Sperre zurücksetzen. | behoben: Verdrängung nach wenigsten Fehlversuchen, /64-Schlüssel an allen fünf Stellen, `TestLimiterFloodKeepsGuessedAccount` |
| F-2-4 | Risiko (niedrig) | `api/webdav.go`, `api/users.go` | WebDAV-Passwort ließ sich mit bloßer Sitzung erzeugen und überlebte Passwortwechsel und 2FA-Reset. | behoben: Bestätigung wie beim API-Token (Dialog unter Mein Konto), Löschen bei Passwortwechsel/2FA-Reset/Abgleich im Cluster; Tests in `TestWebDAV`, Dialog im Browser bei 1280 und 390 px geprüft |
| F-2-5 | Risiko (niedrig) | `api/configbackup.go` `setupLocal` | Ersteinrichtung galt als „lokal“, wenn ein Reverse-Proxy im Heimnetz sie weiterreichte. | behoben: Anfragen mit Proxy-Kopfzeilen abgelehnt, `TestSetupNotThroughAProxy` |
| F-2-6 | Risiko (niedrig) | `api/api.go` `remoteIP` | Hinter Tunnel/Proxy teilen sich alle Besucher eine Adresse: fünf falsche Anmeldungen sperren die Adresse des Proxys für unbekannte Geräte. Bekannte Geräte haben ihr eigenes Limit und bleiben drin. | bewusst gelassen (Schutz wirkt, nur gröber); in `docs/CLOUDFLARE`/Hilfe erwähnen → 16.1 |
| F-2-7 | Altlast | `api/account.go` `reconfirmProof` | Passkey-Bestätigung prüft nur den Kontoschlüssel des Zählers, nicht Gerät/Adresse. Nicht ausnutzbar (Fehlversuch braucht den privaten Schlüssel). | bewusst gelassen |
| F-2-8 | Frage | `api/users.go` | Setzt ein Administrator ein neues Passwort für ein Konto, bleiben dessen Passkeys und TOTP bestehen (Sitzungen enden). Passt zu „Passwort vergessen, Handy noch da“. | bewusst gelassen |
| F-2-9 | Fehler (Oberfläche) | `routes/account/WebDav.svelte` | Gruppe „WebDAV“ unter Mein Konto hat keinen Innenabstand wie die anderen Gruppen (Text klebt am Rand). | offen → 12.6 |
