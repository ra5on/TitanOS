# Sperrbildschirm (Sitzung sperren)

Seit dem Desktop-Modus (PLAN 61) lässt sich die eigene Anmeldung sperren, ohne
sich abzumelden: offene Fenster, Konsolen-Tabs und laufende Aufgaben bleiben,
aber bis zum Entsperren ist nichts zu sehen und nichts zu bedienen.

## Bedienung

- **Sperren:** Konto-Menü › „Sperren“ oder Strg+Alt+L (Mac: ⌘+⌥+L).
- **Automatisch:** nach einer einstellbaren Zeit ohne Tastatur, Maus oder
  Berührung (ab Werk 15 Minuten, 0 = nie). Gezählt wird die letzte Benutzung
  in irgendeinem Tab oder Fenster des NAS in diesem Browser.
- **Sperrbildschirm:** Uhr und Datum, Anfangsbuchstabe und Name des Kontos,
  Passwortfeld mit Pfeil-Knopf, bei Zwei-Faktor-Anmeldung zusätzlich das
  Code-Feld, „Mit Passkey entsperren“, wenn das Konto einen Passkey für diese
  Adresse hat, und „Abmelden“. Unten eine ruhige Zeile (z. B. „2 Aufgaben
  laufen weiter · Gesperrt nach 15 Min ohne Bedienung“). Escape tut nichts.
- Falsches Passwort oder falscher Code zählen wie bei der Anmeldung; nach
  mehreren Fehlversuchen nennt der Bildschirm die Wartezeit.

## Was die Sperre bewirkt

Die Sperre liegt im NAS, nicht nur im Browser. Solange gesperrt, beantwortet
titand jede Anfrage dieser Anmeldung mit „gesperrt“ (HTTP 423), außer dem
Abfragen der Anmeldung, dem Entsperren und dem Abmelden. Neue Konsolen werden
abgewiesen, offene Konsolen und VM-Bildschirme enden nach wenigen Sekunden.
Andere Geräte und API-Tokens desselben Kontos bleiben angemeldet. Hat die
Anmeldung so lange keine Anfrage gestellt, wie die automatische Sperre
eingestellt ist (Browser geschlossen, Gerät im Ruhezustand), sperrt das NAS sie
beim nächsten Zugriff selbst.

Die Sperre steht nur im Arbeitsspeicher (und in `/run`, damit sie einen
Neustart von titand übersteht), nie auf dem Stick. Eine Anmeldung mit
„Angemeldet bleiben“ kommt nach einem Neustart des NAS ungesperrt zurück, wird
aber beim ersten Zugriff nach der eingestellten Zeit sofort gesperrt.

Im Aktivitätsprotokoll stehen Sperren, automatisches Sperren, Entsperren
(mit Passwort oder Passkey) und Fehlversuche.

## Grenzen

- Benutzung innerhalb eingebetteter Docker-Apps (fremde Adresse im Fenster)
  zählt nicht als Benutzung; dort hilft nur, zwischendurch das TitanOS-Fenster
  zu berühren oder die Zeit länger zu stellen.
- Uploads, die der Browser selbst schickt, stocken während der Sperre; Aufträge
  des NAS laufen weiter.
- Die Seite hinter dem Sperrbildschirm ist nur unbedienbar gemacht; wer
  Zugriff auf den entsperrten Rechner und die Entwicklerwerkzeuge des Browsers
  hat, kann zuvor geladene Inhalte dort noch lesen. Neue Daten liefert das NAS
  erst nach dem Entsperren.
- Die Einstellung „Automatisch sperren“ steht im Feld `lockMinutes` der
  Desktop-Einstellungen des Kontos; die Oberfläche dafür kommt mit dem
  Desktop-Modus.
