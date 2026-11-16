# Durchsicht 1.01: Konten, Zugang, Netz, System, Updates, Cluster – Zusammenfassung

Stand 16.11.2026, Zweig `review/r3`. Die vollständige Liste mit Orten und Tests steht in `durchsicht-102-zugang.md`.

## Was geprüft wurde

Anmeldung (Passwort, Zwei-Faktor, Passkeys), Sperrbildschirm, Sitzungen, API-Tokens und MCP-Server, Konten und Rollen, SSH, Zertifikate, WebDAV, Netzwerk und Firewall mit ihrem Probebetrieb, Tailscale und NetBird, Benachrichtigungen aufs Handy, Systemupdates und App-Updates mit ihren Rückwegen sowie der Cluster. Vorher und nachher laufen alle automatischen Tests ohne Fehler.

Insgesamt ist der Bereich in gutem Zustand: Geheimnisse werden zeitkonstant verglichen, Updates werden ohne gültige Signatur nie verwendet, Rechte werden auf dem Server geprüft. Gefunden wurden 11 Punkte: 1 mittel, 8 niedrig, 1 Aufräumen, 1 Vorschlag zur Härtung. Nichts davon ist schwer.

## Behoben

- **Cluster-Konsolen enden jetzt mit der Anmeldung.** Wer auf einem NAS die Konsole eines anderen Knotens geöffnet hatte, behielt sie nach dem Abmelden, nach „Gerät abmelden“ oder nach dem Sperren des Bildschirms. Jetzt endet sie wie eine Konsole des eigenen NAS innerhalb weniger Sekunden.
- **Ein wartendes Update geht nicht mehr verloren.** Wartete ein Update auf den Neustart und ließ sich die neuere Version nicht installieren (zum Beispiel zu wenig Platz auf dem Stick), war das wartende Update trotzdem zurückgenommen. Jetzt bleibt es stehen.
- **Kein Datenrest nach einem abgelehnten App-Update.** Eine heruntergeladene, aber abgelehnte App blieb auf dem Stick liegen. Sie wird jetzt entfernt.
- **WebDAV liest den Stick nicht mehr bei jeder Anfrage.** Ein eingebundenes WebDAV-Laufwerk führte zu ständigen Lesezugriffen auf den Stick. Jetzt wird das WebDAV-Passwort einmal gelesen und im Arbeitsspeicher gehalten; neue oder gelöschte Passwörter gelten sofort.
- **Netzwerk- und Firewall-Änderungen bestätigt nur eine Person im Browser.** Das galt schon für Tokens und andere Knoten, jetzt ausdrücklich auch für alles, was das NAS im Namen eines Kontos selbst anstößt (MCP, Home Assistant).
- Kleinere Stabilitätskorrekturen: die Beitrittsvorschau des Clusters stürzt nicht mehr ab, wenn der Knoten gleichzeitig den Cluster verlässt; die Anmeldeseite liest Kontobilder nur noch, wenn sie sie auch zeigen darf.

Alle Korrekturen kommen mit einem App-Update. Update-Format und Signaturprüfung sind unverändert.

## Notiert, nicht gebaut

- **Entscheidung nötig:** Eine von Hand gesperrte Handy-Anmeldung mit „Angemeldet bleiben“ ist nach einem NAS-Neustart wieder entsperrt, weil der Sperrzustand nach den Regeln nie auf den Stick geschrieben wird. Möglich wäre, ihn beim Sperren und Entsperren auf den Stick zu schreiben.
- **Vorschlag:** Routen für Tokens, Passwörter, Zwei-Faktor, Passkeys, SSH-Schlüssel und die MCP-Einstellungen zusätzlich so absichern wie jetzt die Bestätigungen. Heute schützt sie der feste Werkzeugkatalog.
- Seltene Kleinigkeiten (ein sehr enges Zeitfenster beim Wählen einer bestimmten Version, liegengebliebene kleine Dateien nach einem gescheiterten App-Update) sind beschrieben, aber den Umbau nicht wert.

## Grenzen

Nicht auf echter Hardware, nicht mit zwei echten NAS im Cluster, nicht mit echten Tailscale-/NetBird-Netzen, echtem SSH oder echter Zertifikatsausstellung geprüft; Systemupdates nicht in QEMU. Die Weboberfläche wurde nicht verändert.
