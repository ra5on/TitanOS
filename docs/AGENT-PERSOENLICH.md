# Persönlicher Agent – Betrieb und Schnittstellen

TitanOS führt Aufträge pro lokalem Konto. Die persönliche Agent-Oberfläche verwendet eine angemeldete Websession. Die Sitzung bestimmt das Konto. Ein neu angelegtes NAS-Konto erhält einen eigenen Speicher, auch wenn es denselben Namen wie ein früheres Konto trägt.

## Dienst und Daten

`titanos-agent.service` läuft als `_titanos-agent` ohne Linux-Capabilities. Zwei unabhängige Auftragsläufe und die Posteingangsprüfung verwenden den privaten Unix-Socket `/run/titanos-personal-agent/broker.sock`. Der Worker fordert den nächsten Auftrag und dessen nächsten Schritt mit einer kurzlebigen Kennung an. Der Broker in `titand` prüft die UID des anfragenden Prozesses und führt registrierte native Operationen im Kontext des ermittelten NAS-Kontos aus.

Die Kontozustände liegen in `.titanos/agent-v2` auf dem Systemdatenvolume. Eine Transaktion schreibt zuerst eine private temporäre Datei, synchronisiert, ersetzt die Kontodatei und synchronisiert das Verzeichnis. Der Konfigurationsspeicher hält zusätzlich die Berechtigungsrevisionen. Fehlt das Datenvolume, meldet die API den Speicherfehler. Die Daten älterer Agent-Versionen gehören zu eigenen Verzeichnissen.

Beim ersten eingebundenen Volume speichert TitanOS dessen Namen unter `/config/system/agent-volume.json`. Bei einem Wechsel bleiben Quelle und Ziel eingetragen, bis alle Kontodateien am neuen Ort dauerhaft vorliegen. Fehlt die Quelle, erhalten auch neue Aufträge HTTP 503. Nach dem Wiederanschluss übernimmt TitanOS Verlauf und Aufträge. Bei unterschiedlichen Versionen derselben Kontodatei an beiden Orten bleiben beide erhalten und der Agent wartet auf Klärung.

## Aufträge und Freigaben

`POST /api/v1/personal-agent/jobs` erwartet `requestId`, `tool` und je nach Werkzeug `prompt`, `sources`, `args`, `chatId` oder `runAt`. Dieselbe Request-ID und derselbe Inhalt liefern denselben Auftrag zurück. Der Client liest das Ergebnis und den aktuellen Schritt unter `GET /jobs/{id}` innerhalb dieses API-Präfixes. Die JSON-Verträge für Aufträge, Vorschauen, Belege und Skills stehen in `backend/internal/personalagent/types.go`.

Die effektive Berechtigung ergibt sich aus der aktuellen NAS-Rolle, der App-/Ressourcenberechtigung, den vorab gewählten Quellen und Schritten, der Admin-Obergrenze und der persönlichen Einstellung. Neue Werkzeug-IDs haben die Obergrenze `never`, sofern der Katalog kein eng begrenztes Einstiegsrecht definiert. Ein Konto kann die Obergrenze auf `ask` oder `never` einschränken. Bei `ask` wartet der Auftrag vor dem Zugriff. Für sensible Änderungen bleibt anschließend eine objektbezogene Freigabe erforderlich.

Ein wartender Auftrag akzeptiert `POST /jobs/{id}/approve` mit seinem aktuellen Token. Der Broker baut die konkrete Vorschau vor dem Schreiben erneut auf. Änderungen an Inhalt oder Berechtigung verwerfen die bisherige Freigabe. Mit `revise` kann ein nativer Editor seine geänderten Argumente an denselben wartenden Auftrag binden. `pause`, `resume`, `cancel` und `seen` gelten für diesen einen Auftrag. `PUT /settings` mit `paused` betrifft die Warteschlange des Kontos. Ein schon angestoßener externer Schreibvorgang kann bis zur Rückmeldung laufen.

Vor jeder externen Nebenwirkung setzt der Broker dauerhaft die Phase `executing`. Eine ausgebliebene Bestätigung, etwa nach einem Neustart, ergibt `failed/uncertain`. Für diesen Fall zeigt der Auftrag die letzte konkrete Vorschau. Eine bestätigte Ablehnung ergibt `effect_rejected`. Bei einer bestätigten Durchführung speichert der Broker einen Schrittbeleg und das Ergebnis. Adapter können für dasselbe native Objekt auch über verschiedene Request-IDs hinweg einen gemeinsamen Ausführungsschlüssel verwenden.

## Antworten lokal vorlesen

`POST /api/v1/personal-agent/jobs/{id}/speech` liest die fertige Antwort eines einzelnen eigenen `chat.ask`-Auftrags. Der Browser sendet einen leeren Anfragekörper. Bei der persönlichen Stufe `ask` gehört zur einzelnen Wiedergabe der Header `X-TitanOS-Play-Reply: yes`. Das eigene Recht `speech.speak` ist bei der Auslieferung gesperrt. Die Sprachausgabe benötigt einen laufenden lokalen Piper-Dienst. Die Antwort enthält eine WAV-Datei mit `Cache-Control: no-store`.

TitanOS prüft den Zugriff auf den konkreten Chattext vor der Synthese und vor der Ausgabe. Das Beenden des Dialogs bricht die HTTP-Anfrage ab und schließt die Verbindung zu Piper. Ein Rechteentzug während der Synthese verwirft die Ausgabe, auch wenn die Berechtigung unmittelbar danach erneut erteilt wird.

Über das Mikrofon im Chat wählt das Konto zwischen Diktat und Sprechmodus. Diktat übernimmt den erkannten Text in das Eingabefeld. Im Sprechmodus startet das Konto die Aufnahme ausdrücklich. Nach einer Sprechpause wird ein Beitrag des aktuellen lokalen Chats angelegt. Die Sprachkugel zeigt Mikrofonpegel und Wiedergabe, der Text benennt den aktuellen Zustand. Eine Antwort wird erst gesprochen, wenn der Auftrag abgeschlossen und die lokale Audiowiedergabe erlaubt ist. Bei „Nachfragen“ erscheint die passende Freigabetaste. „Beenden und zum Chat“, das Schließen des Blatts und das Verlassen der Seite geben das Mikrofon frei und beenden die Wiedergabe. Bereits angelegte Beiträge sind im Chat und unter Aufträge zu finden.

Für eine Aufnahme benötigt der Browser einen sicheren Ursprung, Mikrofonzugriff und eine Audioeingabe. Für den Sprechmodus müssen lokale Erkennung, Chat und lokale Ausgabe für das Konto verfügbar sein. Eine fehlende Einrichtung zeigt das Blatt mit Verweis auf die persönlichen Rechte und für die Verwaltung auf die Sprachdienste.

## Persönliche Rezepte und Beobachtung

Ein Skill enthält höchstens acht vorab gewählte Werkzeuge. Jeder Schritt sieht nur die für dieses Werkzeug gewählten Quellentypen. Er kann bis zu 8192 Zeichen Text aus dem letzten abgeschlossenen Schritt erhalten. Vor einer Weitergabe kontrolliert der Broker erneut das Zugriffsrecht auf die vorherigen Quellen. `manual` startet auf Wunsch, `schedule` einmalig zum angegebenen Zeitpunkt, `mail` bei neuen Nachrichten des ausgewählten Absenders im gewählten Posteingang. Automatische Mailbeobachtung benötigt ausdrücklich das Recht `mail.watch=auto`.

Beim Aktivieren einer Mailregel beginnt die Suche bei der aktuell folgenden IMAP-UID. Cursor und neu angelegte Aufträge werden zusammen gespeichert. Ein Wechsel der UIDVALIDITY beginnt eine neue Beobachtungsperiode. Ein neuer Verbindungsversuch nach einem temporären Postfachfehler erscheint als `retry`. Nach Rechteentzug zeigt das Rezept `revoked`; für eine erneute Beobachtung legt der Nutzer die Regel neu an. Bei einer erwarteten Antwort unterdrückt die Bedingung nur die Fertigmeldung. Freigaben und Fehler haben eigene Benachrichtigungseinstellungen.

## Mehrere Dateien vergleichen oder zusammenführen

Für `office.compare` werden zwei bis acht freigegebene Angebote und genau ein beschreibbarer Zielordner ausgewählt. Das Werkzeug liest Text, DOCX, XLSX und ausdrücklich gewählte PDF-Seiten. Es übernimmt beschriftete Angaben zu Anbieter, Gesamtpreis, Lieferzeit, Gültigkeit und Zahlungsbedingungen mit ihrer ursprünglichen Währung und Preisbezeichnung. Fehlende Angaben erscheinen als „Nicht ausgewiesen“. Die bearbeitbare XLSX enthält echte Hyperlinks in Spalte A. Sie führen über die Adresse der lokalen TitanOS-Websitzung zur normalen Dateien-Downloadroute. Wer die Tabelle erhält, braucht für die Originale eine eigene berechtigte NAS-Sitzung.

`office.pdf-merge` nimmt zwei bis acht ausgewählte PDF-Dateien und bis zu zwanzig ausdrücklich aufgelistete Seiten. Die Reihenfolge der Liste bestimmt die Reihenfolge im Ergebnis. Die Vorschau enthält Quelldatei und Originalseitennummer für jede Seite. Die Ausgabe übernimmt die gewählten PDF-Seiten einschließlich ihrer grafischen Inhalte.

Beide Werkzeuge begrenzen die Quelldateien zusammen auf 32 MiB und erzeugen eine neue Datei im gewählten Zielordner. Änderungen an einer Quelle verlangen eine frische Vorschau und Freigabe. Vor der Veröffentlichung prüft der Broker die Dateirechte und Inhalte erneut. Anschließend öffnet er die geschriebene Datei über denselben nativen Zugriff und prüft ihre Prüfsumme.

## Nachweis im Test

Die Integrationstests in `backend/internal/api/personal_engine_test.go` und `personal_privacy_test.go` verwenden temporäre Daten, getrennte NAS-Testsitzungen, einen privaten Unix-Socket und ein lokales IMAP-Testpostfach. Sie prüfen unter anderem Wiederaufnahme, geänderte Vorschauen, doppelte Einreichungen, Rechteentzug zwischen zwei Schritten und die Sichtbarkeit von Anhängen und Suchergebnissen.

`scripts/smoke-storage.py` prüft im gebauten Wegwerf-Gast die tatsächliche Worker-UID und den Capability-Satz aus `/proc`, startet den Dienst neu und wartet auf ein persistentes Ergebnis. Im bestätigten A/B-Update prüft `scripts/smoke-system-update.py` die deaktivierten Altrouten und vergleicht die archivierte alte Agent-Konfiguration byteweise.
