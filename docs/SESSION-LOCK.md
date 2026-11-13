# Sperrbildschirm (Sitzung sperren)

Seit dem Desktop-Modus (PLAN 61) lässt sich die eigene Anmeldung sperren, ohne
sich abzumelden: offene Fenster, Konsolen-Tabs und laufende Aufgaben bleiben,
aber bis zum Entsperren ist nichts zu sehen und nichts zu bedienen.

## Bedienung

- **Sperren:** Strg+Alt+L (Mac: ⌘+⌥+L). Einen Knopf „Sperren“ im Menü gibt es
  seit 0.80 nicht mehr (Entscheidung des Projektinhabers vom 13.11.2026, PLAN
  84-3: er tat für ihn dasselbe wie „Abmelden“); auch die Suche im Menü bietet
  ihn nicht mehr an.
- **Automatisch:** nach einer einstellbaren Zeit ohne Tastatur, Maus oder
  Berührung (am Computer ab Werk 15 Minuten, am Handy nie; jeweils auch „nie“).
  Gezählt wird die letzte Benutzung in irgendeinem Tab oder Fenster des NAS in
  diesem Browser. Einstellbar je Konto in den Arbeitsplatz-Einstellungen und
  seit 0.80 unter Systemsteuerung › Konten und Zugang › Zugang › „Sperren bei
  Leerlauf“, neben „Angemeldet bleiben“ (PLAN 84-2; im Fenster des
  Arbeitsplatzes speichert der Arbeitsplatz, `lock-link.svelte.ts`,
  Nachrichten `titanos-lock-get|-set`).
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
abgewiesen, offene Konsolen, VM-Bildschirme und der Ereignisstrom des
KI-Agenten (SSE) enden nach wenigen Sekunden (Prüfung alle 3 Sekunden). Docker-Apps
im Fenster lassen keine Seitenaufrufe mehr zu, bis entsperrt ist (das Cookie der
App ist an diese Anmeldung gebunden, `docs/APP-WINDOWS.md`). Das Abfragen der
Anmeldung durch den Sperrbildschirm zählt nicht als Benutzung: Eine gesperrte
Anmeldung läuft nach den üblichen Fristen (7 Tage ohne Benutzung bzw. die Frist
von „Angemeldet bleiben“) trotzdem ab. Andere Geräte und API-Tokens desselben Kontos bleiben angemeldet. Hat die
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
- Uploads, die der Browser selbst schickt, warten während der Sperre und laufen
  nach dem Entsperren an derselben Stelle weiter; Aufträge des NAS laufen ohnehin
  weiter.
- Die Seite hinter dem Sperrbildschirm ist nur unbedienbar gemacht; wer
  Zugriff auf den entsperrten Rechner und die Entwicklerwerkzeuge des Browsers
  hat, kann zuvor geladene Inhalte dort noch lesen. Neue Daten liefert das NAS
  erst nach dem Entsperren.
- Die Einstellung „Automatisch sperren“ steht im Feld `lockMinutes` der
  Desktop-Einstellungen des Kontos; die Oberfläche dafür kommt mit dem
  Desktop-Modus.
