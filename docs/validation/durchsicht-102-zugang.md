# Durchsicht 1.01 – Konten, Zugang, Netz, System, Updates, Cluster (Fundliste)

Auftrag des Inhabers vom 16.11.2026: „die ganze Codebasis prüfen und den Code dementsprechend aufräumen und verbessern, Stabilität reinbringen“. Diese Liste deckt den Bereich **Konten, Zugang, Netz, System, Updates und Cluster** ab (Zweig `review/r3`, Stand 1.01). Speicher, Dateien, Docker/VMs, Weboberfläche und KI-Agent prüfen parallele Durchsichten.

Nummern `Z-<lfd>`. Schwere: **hoch** (Sicherheit/Datenverlust, ohne Zutun ausnutzbar), **mittel** (Sicherheitsregel verletzt oder halber Zustand, braucht besondere Lage), **niedrig** (seltener Fehler, kleine Wirkung), **Aufräumen**.
Status: behoben (Commit, Test) · notiert (nicht gebaut, Grund) · bewusst gelassen.

Funde aus `durchsicht-076.md` werden nicht wiederholt.

## Funde

| Nr. | Schwere | Ort | Wirkung | Status |
|---|---|---|---|---|
| Z-1 | mittel | `api/cluster.go` `handleClusterProxy`, `api/terminal_console.go` `loginHolds` | Eine Konsole, ein Protokoll- oder Ereignisstrom eines anderen Knotens, über den Cluster-Proxy geöffnet, lief weiter, nachdem man sich auf dem verwaltenden Knoten abgemeldet, die Anmeldung unter „Angemeldete Geräte“ beendet oder den Bildschirm gesperrt hatte. Der Zielknoten prüft bei Cluster-Anfragen kein Cookie (er verlässt sich auf den verwaltenden Knoten), der verwaltende Knoten sah nach dem Öffnen nie wieder nach. Verstößt gegen die Regel „offene Konsolen enden innerhalb weniger Sekunden nach Abmelden/Sperren“. | behoben: der Proxy prüft die Anmeldung wie die eigenen Konsolen alle 3 s (`whileLoginHolds`) und bricht die weitergereichte Verbindung ab. `TestClusterProxyEndsWithTheLogin` (echte Konsole auf B über A; ohne Korrektur bleibt sie nach dem Abmelden offen). Für den Test kann `terminal.Manager` einen anderen Befehl bekommen (`SetCommandForTest`). |
| Z-2 | niedrig | `update/update_install.go` `Install` (PLAN 74) | Wartete ein Systemupdate auf den Neustart und wurde ein neueres angeboten, nahm „{neu} statt {wartend} installieren“ den wartenden Wechsel zurück, **bevor** geprüft war, ob das neuere überhaupt installierbar ist (Mindestversion, Kernel-Variante, Platz auf dem Stick, laufende Arbeit bei App-Updates). Scheiterte die Prüfung, war das wartende Update weg (halber Zustand: Dateien im Slot, aber nicht mehr bevorzugt). | behoben: Rücknahme erst nach allen Prüfungen, die ablehnen können (`appReady` für App-Updates). `TestRefusedNewerReleaseKeepsTheWaitingUpdate`. |
