# TitanOS als Handy-App (PLAN 70, seit 0.66.0)

TitanOS ist eine installierbare Web-App (`web/static/manifest.webmanifest`, Service Worker `web/static/sw.js`).

## Installieren

- **Mein Konto › „TitanOS als App“:** Chrome und Edge (Android, Desktop) bieten „Installieren“ über `beforeinstallprompt` an (`lib/pwa-install.svelte.ts`, früh im Layout geladen). Läuft TitanOS schon als App (`display-mode: standalone`), steht dort „Als App installiert“.
- **iOS/Safari:** Es gibt nur den Weg „Teilen › Zum Home-Bildschirm“, die Karte erklärt das. Ein Teilen-Ziel unterstützt iOS für Web-Apps nicht.
- **Kurzbefehle:** Langes Drücken auf das App-Symbol bietet Dateien, Docker und Systemsteuerung an (`shortcuts`).

## Teilen aufs NAS

- Das Manifest meldet ein **Teilen-Ziel** (`share_target`): POST `multipart/form-data` an `/share-target` mit `files` (alle Typen) sowie `title`, `text` und `url`.
- Der **Service Worker** nimmt diesen POST selbst an und sendet nichts. Er legt die Dateien und eine Liste im Cache Storage `titanos-share` dieses Browsers ab und leitet auf `/share` weiter. Von der API hält er nichts; zur Oberfläche siehe „Ohne Verbindung“.
- **`/share`** (Seite ohne Programm, nur angemeldet) zeigt die Dateien mit Größe und den geteilten Text. Der Text wird ab Werk als `geteilt-<zeit>.txt` gespeichert, abschaltbar.
  - Ein Ordner wird gewählt; der Ordner-Dialog der Web-Apps zeigt im Modus `folder` nur beschreibbare Orte. Der zuletzt gewählte Ordner wird im Browser gemerkt.
  - Hochgeladen wird mit der Upload-Warteschlange des Dateimanagers (`lib/uploads.svelte.ts`): dieselben Rechte und Namensregeln, bei vorhandenem Namen ein neuer Name (`conflict: rename`).
  - Danach löscht die Seite den Cache und bietet „In Dateien zeigen“. „Verwerfen“ löscht ihn ebenfalls.
- **Ohne aktiven Service Worker** (erste Sekunden nach der Installation) erreicht der POST titand. titand liest ihn nicht und leitet auf `/share?missing=1` weiter (Hinweis „nicht angekommen“). Andere POSTs an die Oberfläche bleiben 405.

## Zahl am App-Symbol (PLAN 87)

- Die Zahl ungelesener Mitteilungen (wie die Glocke, unter dem Filter des Kontos) steht am Symbol der installierten App: Badging API `navigator.setAppBadge`/`clearAppBadge` (`lib/app-badge.svelte.ts`). Das bieten Chrome und Edge für installierte Apps (Android, Windows, macOS) und Safari ab iOS 16.4 für Apps auf dem Home-Bildschirm (dort nur mit erlaubten Mitteilungen). Ohne die API passiert nichts.
- Teilen aus anderen Apps (oben) geht mit Android und Chrome/Edge; **iOS unterstützt für Web-Apps kein Teilen-Ziel** – die Karte „TitanOS als App“ sagt das auf dem iPhone. Auf dem iPhone bleiben „Hochladen“ in Dateien und WebDAV (Mein Konto, z. B. in der Dateien-App des iPhones).

## Ohne Verbindung (seit 1.07)

Die installierte App öffnet sich auch, wenn das NAS nicht antwortet (Handy offline, unterwegs ohne Fernzugriff, NAS aus), und zeigt den zuletzt gesehenen Stand statt einer Fehlerseite.

- **Service Worker** (weiter `/sw.js`, Bereich `/`, Mitteilungen und Teilen-Ziel unverändert): Eine Seite der Oberfläche (ein Weg der App, keine Datei, nicht `/api`, nicht Web-Apps, Handbuch, Symbole, Hintergründe) holt er wie bisher vom NAS und hält ihr HTML (`titanos-shell-v1`: eines für TitanOS, eines für die Agent-App). Antwortet das NAS nicht binnen 6 Sekunden, ist das gehaltene HTML die Antwort. Gehalten wird nur eine Antwort `200` ohne Weiterleitung mit dem Einstieg von SvelteKit. Die gehashten Dateien (`/_app/immutable/`) hält er, wie die Seite sie lädt (`titanos-assets-v1`); eine neue Version der Oberfläche (anderer Einstieg im HTML) leert sie. Beim Aktivieren löscht er nur Caches älterer Fassungen von sich selbst, nie `titanos-share`.
- **Was die Seite sich merkt** (`lib/offline.svelte.ts`): eine Liste erlaubter Werte je Konto im `localStorage` dieses Browsers – Name des NAS, Konto und Rolle, Zustand von System (Prozessor, Arbeitsspeicher, Laufzeit), Volumes (Name, Zustand, Belegung) und Datensicherungen, eingeschaltete Dienste und **nur die Titel** der Chats des Agenten. Nie Dateinamen, Mails, Kamerabilder, Mitteilungen oder was ein Chat sagt. Geschrieben vom obersten Fenster (Fenster des Arbeitsplatzes sind Rahmen darin), höchstens alle 20 Sekunden je Teil und beim Verlassen der Seite. Abmelden, Ablauf der Sitzung und ein anderes Konto löschen ihn. Nur für das NAS selbst, nicht beim Blick auf einen anderen Knoten des Clusters.
- **Anzeige:** Ohne Antwort beim Start zeigt die App „NAS nicht erreichbar“ mit dem Stand und seiner Uhrzeit, „Jetzt prüfen“ (44 px) und dem Hinweis, dass nichts geändert werden kann; sie prüft alle 15 Sekunden von selbst und startet wie gewohnt, sobald das NAS antwortet. War die Sitzung gesperrt oder ist die automatische Sperre seitdem abgelaufen, zeigt sie nur das. Ohne gemerkten Stand bleibt die bisherige Fehlerseite.
- **Während der Arbeit:** Antwortet das NAS nicht mehr, steht am Arbeitsplatz, auf dem Startbildschirm des Handys und in der Agent-App ein Hinweis unter der Statusleiste (`role="status"`, „Jetzt prüfen“; die anderen Seiten haben ihren bisherigen Hinweis). Änderungen gehen dann nicht ab: `api.ts` lehnt sie sofort mit „Das NAS ist gerade nicht erreichbar …“ ab, nichts wartet in einer Warteschlange; ein eingegebener Text bleibt stehen. Ein Upload, der schon lief, wartet wie bisher sichtbar („erneuter Versuch“) und setzt fort, wo das NAS aufhörte; ein neuer beginnt nicht.
- **Grenzen:** Ein Teil der Oberfläche, der nie mit dem NAS geöffnet war, fehlt ohne NAS (seine Dateien sind nicht gehalten). Hintergrundbilder und Symbole hält der Worker nicht. iOS: Cache Storage gibt es, Background Sync nicht; Safari kann gehaltene Daten einer selten genutzten Web-App löschen, und eine App auf dem Home-Bildschirm hat eigenen Speicher – die Agent-App merkt sich ihren Stand selbst. Getestet in Chromium (`scripts/test-ui-offline.mjs`: Handy offline, NAS aus bei Netz, gesperrt, Hinweis während der Arbeit, nichts von der API gehalten; vitest `offline.test.ts`), nicht auf einem echten iPhone.

## Grenzen

- Nur über HTTPS (oder localhost), weil Service Worker sonst nicht laufen. Ohne Zertifikat (Let's Encrypt, `docs/LETSENCRYPT.md`) bietet der Browser weder Installieren noch Teilen an.
- Sehr große Dateien liegen bis zum Hochladen ganz im Speicher des Browsers; das Handy kann sie verweigern.
- Getestet in Chromium (Playwright) mit nachgebildeter API und vorbereitetem Cache; nicht auf echten Android-Geräten.
