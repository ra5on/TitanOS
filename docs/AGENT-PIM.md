# Persönlicher Agent in T-Mail, Kalender und Kontakte

In T-Mail öffnet das Agent-Symbol die ausgewählte Unterhaltung. Auf breiten Fenstern bleibt die Nachricht neben dem Panel lesbar. In schmalen Fenstern öffnet sich das Blatt über der Nachricht. Beim Wechsel zu einer anderen Nachricht läuft ein bereits erteilter Auftrag unter **Agent → Aufträge** weiter. Die neue Nachricht bekommt ihren eigenen Kontext.

**Antwort vorbereiten** legt einen Vorschlag im persönlichen Agent-Auftrag ab. **Im Editor öffnen** übernimmt diesen in T-Mail. Dort lassen sich Empfänger, Cc, Bcc, Betreff, Text, Signatur und Anhänge bearbeiten. Das sendende Postfach gehört zum ausgewählten Thread. **Prüfen und freigeben** speichert den aktuellen Stand im IMAP-Entwurfsordner und zeigt die Versandvorschau. Eine erneute Bearbeitung aus der Freigabe ersetzt die Fassung im selben Auftrag. Der Auftrag prüft die Originalnachricht, die gespeicherte Entwurfsfassung und die Anhänge vor dem Versand erneut.

| Beleg im Auftrag | Bedeutung |
| --- | --- |
| SMTP angenommen, Kopie bestätigt | Der SMTP-Server hat die Nachricht angenommen und der IMAP-Server hat die Ablage bestätigt. |
| SMTP angenommen, Kopie nicht bestätigt | Der Versand ist erfolgt. Die zusätzliche Kopie im Gesendet-Ordner konnte TitanOS nicht bestätigen. Manche Anbieter übernehmen die Ablage selbst. |
| Aktion abgelehnt | Der Server hat den Versand ausdrücklich abgelehnt oder die Aktion konnte vor dem Versand nicht stattfinden. |
| Ergebnis unbestätigt | Die Verbindung ist vor einer eindeutigen Abschlussantwort abgebrochen. Der Auftrag behält die Vorschau. Vor einem weiteren Versand den tatsächlichen Stand beim Anbieter prüfen. |

Beim Zusammenfassen und Erkennen von Terminen liest der Agent die ausgewählte Nachricht ohne Änderung ihres IMAP-Lesestatus. Die T-Mail-Leseansicht markiert eine vom Benutzer geöffnete Nachricht wie bisher als gelesen. Die Option **Zugehörige Nachrichten in diesem Ordner einbeziehen** verwendet Message-ID und References. Bis zu acht Nachrichten einer Unterhaltung können zusammen verarbeitet werden. PDFs müssen einzeln ausgewählt werden. Pro Auftrag sind bis zu drei lesbare PDFs mit jeweils bis zu 8 MiB und den ersten fünf Seiten vorgesehen. Für einen wiederkehrenden Auftrag kann eine exakt benannte PDF gewählt werden. Bei mehreren Anhängen gleichen Namens verlangt der Auftrag eine eindeutige Auswahl.

Für einen Liefertermin lässt sich unter **Mehr** der Ablauf aus Termin erkennen, freie Zeiten im gewählten Kalender prüfen, Antwort vorbereiten und Versand freigeben starten. **Termin als Word-Entwurf** verwendet den Terminvorschlag und das geprüfte Zeitfenster für eine neue Word-Datei im ausgewählten Zielordner. Das Zeitfenster wird ausdrücklich angegeben und auf sieben Tage begrenzt. Die Kalenderprüfung gibt belegte Zeit als Grenze der freien Intervalle weiter. **Nachfassen** prüft zum geplanten Zeitpunkt den Posteingang desselben Postfachs auf eine Antwort mit passendem Nachrichtenverweis. Eine Antwort beendet die Beobachtung still. Die Aktion versendet selbst keine Erinnerung an den Absender.

Ein einzelner, nicht wiederkehrender Termin kann nach Vorschau des sendenden Postfachs, Empfängers und Inhalts als Einladung übermittelt werden. Eine Absage benötigt den Bezug zu einem vorhandenen Teilnehmer oder zu einer im persönlichen Agent bestätigten Einladung. Kontakte und Erinnerungen werden im ausdrücklich gewählten Adressbuch bzw. in der gewählten Liste bearbeitet. Bei einer Erstellung überprüft der Auftrag das gespeicherte Objekt und führt zurück zur nativen App.

## Modellverbindung

Die lokale Standardverbindung nutzt Ollama auf dem NAS. Vor der Übermittlung von Text prüft der Agent die Metadaten der installierten Gewichte. Mail, ausgewählte PDF-Seiten und Ergebnisse vorheriger Schritte werden als Quelldaten übergeben. Die Modellanfrage enthält keine Schreibwerkzeuge. Für **Nur diesen Text an OpenAI senden** braucht das Konto ein freigegebenes Cloud-Werkzeug, ein eigenes konfiguriertes Modell und eine ausdrückliche Freigabe der einzelnen Anfrage. Der Cloud-Pfad akzeptiert ausschließlich die eingegebene Nachricht. Der Server speichert den persönlichen Schlüssel verschlüsselt und gibt über die Modell-Einstellungen nur seinen Hinterlegungsstatus zurück.
