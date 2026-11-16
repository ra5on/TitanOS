# Versionshinweise: Durchsicht Container, VMs und Geräte (1.01)

Zusammenfassung für die Versionshinweise. Einzelheiten, Tests und offene Punkte stehen in `durchsicht-102-container.md`.

## Was sich für dich ändert

**Daten und Speicher**
- **Excel-Dateien bringen das NAS nicht mehr in Not.** Eine präparierte, nur wenige Kilobyte große Excel-Datei konnte titand mehrere Gigabyte Arbeitsspeicher kosten. Das galt beim Durchsuchen von Dateien, beim Lesen durch den KI-Agenten und über MCP. Arbeitsmappen werden jetzt Stück für Stück und mit fester Obergrenze gelesen.
- **VM-Sicherung nach einem Stromausfall.** Wenn eine VM eine zweite Platte auf einem anderen Volume hat und dieses Volume nach dem Neustart noch nicht eingebunden war, konnten Daten dieser Platte aus der Sicherungszeit liegen bleiben. TitanOS wartet jetzt, bis alle Volumes der VM da sind, bevor es aufräumt und die VM startet.
- **PDF-Export mit sehr langen Wörtern.** Ein PDF mit einer sehr langen Zeichenkette ohne Leerzeichen (zum Beispiel ein Chat-Export mit einem Code-Block) hielt einen Prozessorkern stundenlang beschäftigt. Jetzt ist es in Sekundenbruchteilen fertig.

**Docker-Apps**
- **Automatische Updates erkennen eine abstürzende App.** Startete ein neues Abbild immer wieder neu, galt das Update bisher als gelungen. Jetzt wird es zurückgenommen.
- **Der Rückweg klappt auch bei Apps mit fest angegebenen Abbildern** (zum Beispiel Immich). Bisher brach er dort ab, und die App lief mit dem kaputten Update weiter.
- Wird „Nach Updates suchen“ abgebrochen, weil die Seite geschlossen wurde, bleibt das letzte Ergebnis stehen.
- Die Container der Sprachdienste und die Sandboxen des KI-Agenten lassen sich nicht mehr über die App-Befehle für API-Tokens und MCP starten, stoppen oder löschen.

**Kameras (NVR)**
- Wurde das Frigate-Abbild aufgeräumt, während der NVR aus war, lädt TitanOS es jetzt neu, statt jede Minute erfolglos zu starten.
- Lehnte Docker einmal einen Neustart von Frigate ab, übernimmt TitanOS die Kamera-Änderung jetzt beim nächsten Versuch, statt sie zu vergessen.
- Doppelklick auf „Behalten“ speichert das Video nur einmal.
- Eine kurz überlastete Antwort von Frigate löscht keine Exporte mehr aus der Liste.

**Home Assistant**
- **Die MQTT-Geräte bleiben erhalten, wenn du die Einstellungen änderst.** Bisher vergaß Home Assistant bei jeder Änderung, zum Beispiel am Namen oder an den MCP-Freigaben, alle TitanOS-Entitäten samt Umbenennungen und Räumen.
- **Mehrere Verbindungen gehen nicht mehr verloren**, wenn eine Datei auf dem Stick einmal nicht lesbar ist.
- Der KI-Agent sieht bei „Fähigkeiten eines Geräts“ jetzt die Dienste, die seine eigene Freigabe erlaubt, unabhängig von den MCP-Freigaben.
- Eine ausgeschaltete Verbindung wird auch für Statistiken nicht mehr abgefragt.

**USV**
- Scheitert das Einrichten von NUT nach dem Speichern, zeigt die Seite jetzt den Grund. TitanOS versucht es außerdem zehn Minuten lang jede Minute erneut. Bisher blieb das NAS bis zum nächsten Speichern ohne Überwachung der USV.

**Plugins und Hardware**
- Gehst du während eines NVIDIA-Baus zum mitgelieferten Treiber zurück, wählt sich der fertige Bau nicht mehr selbst für den nächsten Neustart.
- Ein hängender Download-Server blockiert nicht mehr App-Updates und Wartung bis zum Neustart. Betroffen waren die NVIDIA-Bauumgebung und Community-Quellen.
- Eine entfernte Community-Quelle kommt nicht mehr zurück, und eine doppelt angeklickte Quelle wird nur einmal angelegt.
- Neue Hardware wird auch dann erkannt, wenn sehr viele Geräte gleichzeitig dazukommen.
- Scheitert die Übergabe einer Grafikkarte an eine VM, bekommt das NAS seine Textkonsole zurück.

## Noch offen

Folgende Punkte sind notiert, aber nicht umgesetzt, weil sie eine Entscheidung oder einen größeren Umbau brauchen:
- Fristen für `docker compose pull/up` und für libvirt-Befehle in der VM-Schleife.
- Ein abgebrochenes Anlegen eines LXC-Containers wird nach einem Neustart nicht als gescheitert erkannt.
- Ein von Docker abgelehnter Start von Frigate zählt als Absturz und wechselt das Erkennungsgerät.
- Plugin-Downloads zählen nicht als laufende Arbeit.

## Was nicht geprüft ist

Alle Korrekturen sind mit Go-Tests gegen nachgebaute Befehle und Server geprüft. Es lief nichts gegen echtes Docker, libvirt/QEMU, LXC, NUT, Frigate, Home Assistant oder echte Grafikkarten.

Es handelt sich um ein reines App-Update: Nur titand ändert sich, die Oberfläche bleibt gleich, und es gibt keine neuen Einstellungen auf dem Stick.
