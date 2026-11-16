# KI-Agent

Der KI-Agent erledigt Aufträge auf dem NAS selbstständig. Du gibst ihm eine Aufgabe, er arbeitet sie in mehreren Schritten mit den Werkzeugen von TitanOS ab und meldet sich mit dem Ergebnis. Das Modell läuft auf deinem eigenen Ollama-Server; andere Anbieter gibt es nicht (Entscheidung des Projektinhabers vom 4.11.2026, PLAN 44).

Ab Werk ist der Agent aus. Eingeschaltet wird er unter **Einstellungen › KI-Agent** (nur Besitzer und Administratoren).

## Einrichten

1. Ollama auf einem Rechner im Heimnetz betreiben und ein Modell mit Werkzeugen laden, z. B. `ollama pull gpt-oss:20b`.
2. Einstellungen › KI-Agent: Adresse eintragen (z. B. `http://192.168.10.5:11434`), **Prüfen**, Modell wählen, einschalten, speichern. Beim ersten Einschalten fragt ein Fenster nach dem **Speicherort** (siehe unten).
3. Den Reiter **KI-Agent** öffnen.

**Denkstufen:** Die Auswahl kommt vom Modell selbst (`thinking.values` aus Ollamas `/api/show`). gpt-oss meldet Niedrig/Mittel/Hoch, andere Modelle nur an/aus oder mehr Stufen. Eine Stufe, die das Modell nicht kennt, wird nicht gesendet; Ollama würde sie sonst still durch seinen Standard ersetzen.

**Kontextlänge:** „Wie in Ollama eingestellt“ schickt keinen eigenen Wert. Die Auswahl geht bis 256k, aber nie über die Länge des Modells (`context_length` aus `/api/show`). Die Seite zeigt immer, wie viel das Modell kann, und, wenn es geladen ist, womit Ollama es gerade nutzt (`/api/ps`); sie warnt unter 16k und weist über 64k auf den Grafikspeicher hin. Ist das Modell zu Beginn eines Auftrags nicht geladen, lädt der Agent es zuerst und liest dann den echten Wert (PLAN 59a), damit er schon in der ersten Runde rechtzeitig zusammenfasst. Jede Anfrage von TitanOS an das Modell des Agenten (Chat, Zusammenfassen, Nachdenken in der Nacht, Bilder ansehen) schickt dieselbe Haltezeit und denselben Kontext aus den Einstellungen, damit Ollama das Modell nicht neu lädt oder früher entlädt. Rund 80 Werkzeuge belegen etwa 6 700 Token (gemessen mit gpt-oss:20b).

**Speicherort (PLAN 58):** Alles des Agenten liegt auf einem Volume: privat `.titanos/agent/` (Chats mit Gedanken und Schritten, Gedächtnis, Freigaben, Zeitpläne, Anhänge) und die Freigabe `KI-Agent` (Chats als Markdown, Projekte, Dokumente, Skills, Notizen, Arbeitsordner der Sandbox). Die Sprachmodelle bleiben bei Ollama.
- Beim ersten Einschalten fragt ein Fenster nach dem Volume; angeboten werden alle eingebundenen mit freiem Platz und Art (SSD/Festplatte), SSD ist vorgewählt und empfohlen. Wer den Agenten schon nutzt, behält sein Volume ohne Frage.
- Das Volume ändert sich danach nur über **Umziehen** (Übersicht › Speicherort). titand kopiert Ordner und Dateien mit Rechten und Zeiten, lässt Verknüpfungen weg, vergleicht jede Datei per SHA-256, schaltet erst dann `volume` in `agent.json` und die Freigabe um und räumt zuletzt das alte Volume auf. Scheitert ein Schritt vor dem Umschalten, bleibt das alte Volume gültig und die halbe Kopie wird entfernt.
- Seit 1.02 zieht auch `.titanos/docs/` mit (Gedächtnis und Papierkram von „Dokumente“: Verzeichnis, Protokoll mit Rückgängig und offenen Fragen); bis 1.01 blieb es auf dem alten Volume zurück. Liegt auf dem Ziel noch so ein zurückgebliebener Stand, legt der Umzug ihn unter `.titanos/agent-old/<Zeit>-left/` beiseite („Alte Kopie löschen“ entfernt ihn).
- Während des Umzugs pausiert der Agent (keine neuen Aufträge, kein Lesen und Schreiben seiner Daten). Läuft ein Auftrag oder ein Lauf von „Dokumente“, lehnt titand den Umzug ab. Das Ziel braucht etwa 10 % mehr freien Platz als die Daten und darf noch keine Daten des Agenten haben.
- API (Besitzer und Administratoren): `GET /api/v1/agent/volumes`, `POST /api/v1/agent/volume/move` `{to}`, `GET /api/v1/agent/volume/move`. Der Fortschritt liegt nur im RAM; ein Neustart während des Umzugs lässt das alte Volume gültig (die Einstellung wird erst nach der Prüfung geschrieben).

**Mitteilungen (PLAN 58):** Jedes Konto kann seine Geräte für Mitteilungen eintragen (Agent-App: Einstellungen › Mitteilungen; Konto-Seite). Konten mit der Rolle „Benutzer“ bekommen nur die Mitteilungen ihres eigenen Agenten (Auftrag fertig, Freigabe nötig, Erinnerungen); was die Glocke zeigt, geht weiter nur an Besitzer und Administratoren.

**Einstellungen › KI-Agent (seit 0.54.0, PLAN 53)**

Die Seite hat Reiter statt einer langen Spalte:
- **Übersicht:** Ein/Aus mit Zustand, Handy-App, Grundanweisung.
- **Verbindung und Modelle:** Ollama, Knoten, Modell, Sprache, Bilder, Snapshot.
- **Rechte** (seit 0.64.0 in Karten, PLAN 68): Karte „Schnellwahl“ mit der Auswahl Nur lesen / Standard / Alles ohne Nachfrage / Eigene in der Kopfzeile und der Zeile „Nie erlaubt“; Karte „Bereiche“, je Bereich eine Zeile mit kompakter Auswahl Ohne Nachfrage / Nachfragen / Verboten, aufklappbar zu den einzelnen Werkzeugen. Selten geänderte Bereiche (Sicherungen, Speicher, Freigaben, Konten, Plugins, Home Assistant, Code) stehen hinter „＋ n weitere Bereiche anzeigen“, aber nur, solange für sie nichts eigens eingestellt ist; ein geänderter Bereich bleibt immer sichtbar. Karte „Ordner-Regeln“ mit „＋ Regel“. Am Handy steht jede Auswahl in einer eigenen Zeile unter ihrer Bezeichnung.
- **Konten und Orte.**
- **Home Assistant.**
- **Eigeninitiative.**

Links wie `?tab=connection` öffnen einen Reiter direkt. **Ungespeichert (seit 1.01):** Was das NAS selbst sofort speichert – einen anderen Knoten freigeben oder entziehen, das Umziehen auf ein anderes Volume –, zählt nicht als ungespeicherte Änderung; vorher Getipptes bleibt ungespeichert. Wer die Seite mit ungespeicherten Änderungen verlässt (anderer Link, Fenster schließen, Tab schließen), wird vorher gefragt (gemeinsames `NavigationGuard.svelte` mit `samePath`); der Wechsel zwischen den Bereichen der Seite fragt nicht, die Änderungen bleiben dabei stehen. Seit 0.64.0 stehen alle Reiter in Karten (`.tile.ui-card` mit Kopfzeile) wie die übrigen Einstellungen; im Fenster des Agenten ohne Einleitung, mit der Seitenleiste des Agenten links.

**Handy-App**
- Zuerst wählt man den Weg zum NAS, dann erscheint der QR-Code dazu. Der QR-Code wird im Browser erzeugt, kein fremder Dienst.
- Wege: Heimnetz-IP, Tailscale (HTTPS, wenn das Zertifikat läuft), NetBird, dynamisches DNS, eigene Domain mit Zertifikat oder eine eigene Adresse.
- `GET /api/v1/agent/addresses` liefert die Wege, nur für Besitzer und Administratoren. Er liest dafür nur Einstellungen und Zustände im RAM.
- Ein Hinweis erklärt, dass Mitteilungen und das Symbol auf dem Home-Bildschirm HTTPS brauchen.

**Whisper als App**
- „Whisper einrichten“ öffnet die Katalog-App `whisper`: Speaches mit NVIDIA-Grafikkarte, Host-Port 8010, Modell `Systran/faster-whisper-large-v3` wird beim ersten Start geladen.
- Läuft die App, übernimmt „Whisper-App … übernehmen“ ihre Adresse (`http://127.0.0.1:<port>`) und schaltet die Spracheingabe ein.

## Bedienung

- **Aussehen (PLAN 68, seit 0.64.0; Entwurf `out/proposal/agent-68/`, vom Inhaber freigegeben):** Der Agent sieht aus wie jedes andere Programm von TitanOS (Vorbild Docker und Systemsteuerung › Netzwerk). Kein Aurora-Hintergrund, kein Glas, kein Leuchten, keine eigenen Farben mehr: alle Farben kommen aus dem Theme (`--bg`, `--surface`, `--border`, `--text`, `--muted`, `--accent`, `--chip`), auch die Farbstärke der Darstellung gilt jetzt im Agenten. Handy und Desktop haben denselben Aufbau.
  - **Seitenleiste** (264 px, wie die Spalte der Systemsteuerung; PLAN 90-4): oben das Agent-Symbol mit „KI-Agent“ (im Fenster nicht), das Suchfeld „Chats und Gemerktes suchen“ (filtert die Chats; darunter führt „Im Gedächtnis nach … suchen“ auf die Seite Gedächtnis), „Neuer Chat“, dann Start (mit der Zahl dessen, was auf dich wartet), Geplant (mit Anzahl), Gedächtnis, Wissen, Notizen, Archiv und die eingeschalteten Funktionen, Abschnitt PROJEKTE (Anzahl der Chats; wartet ein Chat auf Freigabe, steht „▲ n“ in Warnfarbe, ungelesene haben einen Punkt, laufende pulsieren), „Projekt anlegen“, Angeheftet und die Chats nach Tagen. Unten Einstellungen und „Seitenleiste einklappen“ (je Gerät, `localStorage` `titanos.agent.side`; aufklappen über den Knopf links in der Kopfzeile des Chats bzw. neben der Überschrift einer Seite). Schlichte Zeilen, die offene akzentgetönt; lange Namen brechen um.
  - **Kopfzeile des Chats (Fassung 0.90, 15.11.2026, Vorbild: die großen Chat-Apps):** Am Arbeitsplatz eine Zeile ohne Linie: Titel, Projekt als Chip, rechts runde Knöpfe ohne Rahmen – Lupe „In diesem Chat suchen“, Anheften, außerhalb eines Fensters Vollbild, und **⋯** (Umbenennen, In Projekt verschieben, Archiv, Export Markdown/PDF, Löschen). Modell und Denkstufe stehen nicht mehr im Kopf, sondern im Eingabefeld. **Am Handy gibt es keine Kopfleiste mehr:** runde Knöpfe (44 px) und der Titel in einer Kapsel schweben über dem Chat, der dahinter durchläuft und nach oben ausblendet – links das Menü, in der Mitte Titel mit Projekt und Modell klein darunter (ein Tipp öffnet das Blatt `HeadSheet.svelte` mit Projekt, Modell und Denkstufe), rechts „Neuer Chat“ und ⋯. Sie beginnen unter der Statusleiste (`--agent-top`, das 1,4-Fache des sicheren Bereichs: Als App auf dem Home-Bildschirm zeichnet iOS den Bereich unter der Statusleiste weiter weich, als der sichere Bereich reicht; der Titel stand genau dort). Auf einer Seite (Geplant, Einstellungen …) ist der Kopf eine schlichte Zeile mit „Zurück“ und dem Namen der Seite.
  - **Nachrichten:** höchstens 760 px breit, mittig, Schrift 15 px (Handy 16 px). Die eigene Nachricht ist eine graue, stark gerundete Blase rechts; die Antwort ist schlichter Text über die volle Breite. **Schritte und Gedanken** eines Durchgangs sind eine stille Zeile („3 Schritte · 1,6 s · Docker-Apps gelesen, … ›“, am Handy ohne die Aufzählung); ein Tipp öffnet das Blatt „Zusammenfassung“ (am Handy von unten, am Arbeitsplatz mittig): die Schritte untereinander an einer feinen Linie, „Rückgängig“ wo TitanOS es kann, und ein Tipp auf einen Schritt zeigt „Eingabe“ (Argumente) und „Ausgabe“ (Ergebnis, höchstens 6000 Zeichen) bzw. die Gedanken. Mit „Schritte offen zeigen“ steht die Liste zusätzlich unter der Zeile. **Während der Agent arbeitet** steht dort eine Zeile mit drei Punkten im Orangeton des Agenten, die ihre Form wechseln (seit 0.93, Wunsch des Inhabers vom 15.11.2026): Dreieck, das ein Drittel weiterdreht, eine Reihe, in der sie nacheinander hüpfen, ein großer Punkt, der die anderen schluckt, zum Quadrat wird und sich umdreht, eine Säule und zurück; bei „Animationen: Aus“ und „Weniger Bewegung“ steht das Dreieck still. **Figur und Wort je Tätigkeit (seit 0.94, Wunsch des Inhabers vom 15.11.2026):** Läuft ein Schritt, bilden die Punkte eine Figur dafür (`busyKind` in `lib/agent.ts`): Websuche und Seitenlesen – zwei Punkte kreisen um einen Kern; Dateien lesen und ordnen – drei Blätter, die nacheinander aufgeblättert werden; Schreiben (Dateien, Dokumente, Gemerktes) – drei Tasten, die nacheinander gedrückt werden; Home Assistant – ein Dach, dessen Lichter reihum angehen (beim Schalten schneller); Code – drei Balken, die steigen und fallen. Daneben steht statt des Namens des Schritts ein Wort seiner Art („Stöbert im Netz …“, `agent.busy.<art>.<n>`) und dahinter wie bisher, worum es geht; andere Schritte behalten ihren Namen und die denkenden Punkte. Beim Nachdenken wechseln zwölf Wörter, nachts kommen zwei dazu, ab 20 Sekunden Wörter der Geduld und die Sekunden. Ein neues Wort rollt von unten herein. Unter einer Antwort, die hereinläuft, bleiben die Punkte klein stehen (Tasten). Endet ein Lauf mit einer Antwort, zeichnet sich für gut anderthalb Sekunden ein Haken mit „Fertig“ und fährt dann weg; ohne Antwort (Abbruch, Fehler, Rückfrage) kommt keiner. Läuft ein bestimmter Schritt, steht daneben in Grau sein Name, und über den Text läuft ein Licht von links nach rechts. Denkt der Agent nur nach, steht dort ein kurzes Wort (seit 1.01 grau wie der Rest der Zeile; orange sind nur die Kugeln und der Haken, ein neuer Schritt blendet seinen Text ein, die Figur wird nicht neu aufgebaut), das alle knapp drei Sekunden wechselt („Tüftelt …“, „Brütet …“, „Wägt ab …“; acht eigene Wörter, `agent.busy.*`; seit 0.92). Wartet ein Schritt auf die Person, steht über der Freigabe-Karte ein gelber Ring mit „Wartet auf deine Freigabe“. Der Agent hat zwei eigene Farben (`--ag-warm` für Senden und die Punkte, `--ag-link` für Links; Inhaber 15.11.2026), alle anderen kommen aus dem Theme. „Animationen: Aus“ und „Weniger Bewegung“ lassen die Zeile still. Alles, was im Agenten gleitet (Blätter von unten, Seitenleiste, Notizen, Modellwahl, Dialoge), nutzt seit 0.92 dieselbe Bewegung aus `lib/agent-motion.ts`: 400–420 ms mit langem, weichem Auslauf (`quintOut` bzw. `cubic-bezier(0.22, 1, 0.36, 1)`), nur Verschiebung und Deckkraft. **Tabellen** haben einen runden Rahmen, eine fette Kopfzeile und feine Linien; sie bleiben auch am Handy Tabellen (Karten zum Wischen gibt es seit 0.90 nicht mehr), Wörter brechen nur am Bindestrich, eine zu breite Tabelle scrollt seitlich; darunter „Tabelle kopieren“ (Tabulator-getrennt). **Codeblöcke** stehen in einer Karte mit der Sprache des Blocks und „Kopieren“ im Kopf; lange Zeilen scrollen seitlich. Seit 0.91 hat der Code Farben (`lib/code-highlight.ts`, ein eigener kleiner Zerleger für YAML, JSON, Shell, JavaScript/TypeScript, Python, Go, INI/TOML, Dockerfile und SQL; andere Sprachen bleiben einfarbig, jedes Stück wird maskiert), und „Vergrößern“ legt den Block über den ganzen Chat (Escape oder „Verkleinern“ zurück). Die Seiten, Dialoge und Blätter des Agenten tragen dieselben Formen wie der Chat (runde Zeilen, Knöpfe als Kapseln; Regeln in `routes/agent/+page.svelte`, nur wo der Agent ist). Unter jeder Antwort eine Zeile aus Symbolen: Vorlesen, Kopieren (Haken als Rückmeldung; ohne HTTPS über ein unsichtbares Textfeld), unter der letzten Antwort „Neu erzeugen“, und was sich der Agent direkt davor gemerkt hat („✦ Gemerkt: …“). Unter der eigenen Nachricht am Arbeitsplatz „Bearbeiten“ und „Kopieren“. Die Knöpfe unter einer Antwort sind am Handy 44 px groß; die Aktionen der eigenen Nachricht öffnet dort langes Drücken (Blatt von unten).
  - **Freigabe:** eine Banner-Karte mit Warnrahmen. Titel in Klartext aus Werkzeug und Argumenten („Freigabe nötig: VM „HAOS“ aktualisieren“, Übersetzungen `agent.ask.<karte>` für die Aktionskarten des Katalogs; ohne Übersetzung die Zusammenfassung des NAS), darunter die Stufe als Text („Stufe: Gefahr“, nicht nur über die Farbe) und wann gefragt wurde. Rechts „Abbrechen“ und „Ausführen“ (wie im Entwurf; bei „Zerstörend“ ist Ausführen rot), am Handy darunter. Nichts bekommt den Fokus, Enter löst nichts aus. „Details“ zeigt Werkzeug, Argumente und den Inhalt; bei Dateiinhalten ist es offen.
  - **Eingabefeld:** ein abgerundetes Feld (`Composer.svelte`). Am Arbeitsplatz und am Handy mit Cursor, Text oder Anhang steht oben der Text, darunter „＋“, Modell und Denkstufe als Chip („gpt-oss:20b Mittel“), Mikrofon und der runde Senden-Knopf im Akzent (während eines Laufs „Anhalten“). Ohne Cursor und Text ist es am Handy eine Zeile: ＋, Text, Mikrofon, Senden. „＋“ öffnet am Arbeitsplatz ein Menü, am Handy das Blatt „Zum Chat hinzufügen“: Foto oder Kamera, Vom Gerät hochladen, Vom NAS wählen, darunter „Projekt, Modell und Denkstufe“. Der Chip öffnet am Arbeitsplatz die Auswahl nach oben, am Handy das Blatt. Unter dem Feld steht am Arbeitsplatz eine kleine Zeile, die nur sagt, was die Seite weiß: „Läuft lokal auf diesem NAS über Ollama“ (Ollama auf diesem NAS), „Läuft über Ollama auf <Adresse>“ oder „Läuft über deinen eigenen Ollama-Server“ (Konten ohne Verwaltungsrechte sehen die Adresse nicht), „… eines anderen Cluster-Knotens“; dazu „nichts verlässt das Netzwerk“ nur bei einer Adresse im Heimnetz und ausgeschalteter Internetsuche, sonst „Internetsuche ist an“ bzw. „Chats bleiben auf diesem NAS“ (`whereLine` in `lib/agent.ts`). Am Handy entfällt die Zeile. Auf dem Handy 16 px Schrift, damit iOS nicht heranzoomt. Das Feld wächst mit dem Text bis 220 px. „/“ am Anfang öffnet die Befehle (siehe „Seiten und Chat-Bedienung“).
  - **Leiste unten am Handy:** eine Kapsel mit vier Bereichen – Chat, Projekte (Liste der Projekte mit Zahl der Chats und letzter Änderung, „Projekt anlegen“; `ProjectList.svelte`), Aufträge (die Seite Geplant) und Notizen. Sie steht im Chat, auf der Startseite und auf „Geplant“; bei offener Tastatur und auf den übrigen Seiten ist sie weg.
  - **Begrüßung:** wechselt mit der Tageszeit und von Besuch zu Besuch – je vier Sätze für Morgen (5–11 Uhr), Tag, Abend (ab 18 Uhr) und Nacht (23–5 Uhr, „Na, Nachteule …?“), dazu zwei am Wochenende (`agent.greet.*`). Am Handy und in einem neuen Chat steht sie in der Mitte der Seite.
  - **Start = neuer Chat:** die Startseite (PLAN 90-4, siehe „Seiten und Chat-Bedienung“) mit dem Eingabefeld darunter.
  - **Im Fenster** nutzt der Agent die Menüleiste des Arbeitsplatzes (Ablage › Neuer Chat …, Ansicht › Notizen …, Agent › Modell, Einstellungen). Die Seitenleiste liegt im Fenster unter der Titelleiste (die Fensterleiste kennt nur feste Einträge, Projekte und Chats ändern sich). Einstellungen › KI-Agent zeigt im Fenster des Agenten dieselbe Seitenleiste daneben (Einträge führen zurück in den Agenten).
  - **Bis unter die Statusleiste (PLAN 56):** Auf `/agent` meldet die Seite dem iPhone eine durchsichtige Statusleiste (`black-translucent`, `viewport-fit=cover`); das Bild reicht bis ganz oben. TitanOS selbst behält die schwarze Leiste. iOS liest das beim Laden, deshalb lädt der Wechsel zwischen TitanOS und dem Agenten neu (wie beim Manifest).
- **Wischen:** Dialoge des Agenten (Projekt, Notizen, Archiv …) wischen von rechts herein. Geplant, Gedächtnis, Wissen und die Einstellungen sind seit PLAN 90-4 Seiten und wischen nicht mehr. „Animationen: Reduziert/Aus“ und „Bewegung reduzieren“ des Geräts schalten Bewegung ab.
- **Alle Chats löschen** unten in den Einstellungen des Agenten (`DELETE /api/v1/agent/chats`, `?keepPinned=true`): löscht die eigenen Chats mit Anhängen. Angeheftete bleiben ab Werk, laufende immer. Projekte, Gedächtnis und Skills bleiben, Chats anderer Konten in geteilten Projekten nie.
- **Mitteilungen** (Einstellungen › Mitteilungen, nur Besitzer und Administratoren): Auf dem iPhone ist der Agent vom Home-Bildschirm eine eigene App mit eigenem Abo. Dort einmal einschalten, auch wenn TitanOS schon Mitteilungen bekommt.
- **Bewegung:** weiche Übergänge und Hereinwischen; „Animationen: Reduziert/Aus“ unter Darstellung und Gesten schalten sie ab (auch die Systemeinstellung „Bewegung reduzieren“).
- **Desktop:** Im Desktop-Modus ist der KI-Agent ein eigenes Programm im Fenster (`docs/DESKTOP.md`); die Agent-Blase in der Ecke des Arbeitsplatzes gibt es seit dem 11.11.2026 nicht mehr. Bis 0.61.0 in der alten Oberfläche: Links bleibt das TitanOS-Menü, daneben das Agent-Menü, rechts der Chat (höchstens etwa 800 px breit, mittig). **Vollbild** in der Kopfzeile blendet das TitanOS-Menü aus, **Mit Menü** wieder ein; die Wahl gilt je Gerät (`localStorage`, PLAN 47).
- **Handy:** Der Agent ist immer eine eigene Ansicht ohne TitanOS-Rahmen (PLAN 48). Oben schweben Menü, Titel, „Neuer Chat“ und ⋯, unten stehen das Eingabefeld und die Leiste mit Chat, Projekte, Aufträge und Notizen (siehe „Aussehen“). Das Menü (oder Wischen vom linken Rand) öffnet die Seitenleiste, Wischen nach links schließt sie. Ganz unten im Menü führt **„Zurück zu TitanOS“** zum Startbildschirm (App-Raster; bis 0.58.0 zum Dashboard).
- **Home-Bildschirm:** Zwischen TitanOS und dem Agenten lädt die Seite neu. Safari liest das Manifest nur beim Laden. So legt „Zum Home-Bildschirm“ auf `/agent` das Agent-Symbol an und überall sonst das von TitanOS, auch wenn man über das Menü hingekommen ist.
- **Eigene App:** Auf `/agent` „Zum Home-Bildschirm“ (iOS) bzw. „App installieren“ (Chrome/Edge). Die App hat ein eigenes Symbol und Manifest (`/agent.webmanifest`, Start `/agent?app=1`) und zeigt nie die TitanOS-Navigation.
- **Projekte** haben eigene Anweisungen und einen Standard-Ablageort.
- **Schritte:** Jeder Werkzeugaufruf steht im Chat, zusammengefasst als „n Schritte“; die Gedanken des Modells stehen aufgeklappt in derselben Zeile. Der Knopf rechts im Eingabefeld hält den Auftrag an.
- **Rückfragen:** Passt ein Name auf mehreres (Home Assistant als Docker-App und als VM), fragt der Agent mit Knöpfen.
- **Darstellung und Gesten** (Einstellungen des Agenten): Gesten, Vibration (nur Android), Animationen, Schriftgröße (je Gerät), kompakte Liste, Startansicht, Vorschläge, Schritte offen zeigen, Schreibanimation, Bildschirm wach halten. Für das Konto auf dem NAS gespeichert (`/agent/prefs`), Geräteeinstellungen nur im Browser. **Entfernt in 0.64.0:** Akzent, Chat-Stil (Blasen/Klar/Kompakt), Hintergrund (Aurora/Ozean/Wald), Glas mit Glasstärke und Leuchten (Felder `accent`, `style`, `background`, `glass`, `glassLevel`, `glow`). Gespeicherte Werte älterer Versionen (`titanos.agent.look` im Browser, `prefs.json` des Kontos) werden ohne Fehler gelesen und nicht mehr beachtet; beim nächsten Speichern fallen sie weg. Eine ältere App, die die Felder vermisst, nimmt ihre Standardwerte.

Aufträge laufen auf dem NAS weiter, auch wenn die Seite zu ist. Ist er fertig, wartet er auf dich oder gab es einen Fehler, kommt eine Push-Mitteilung (Web Push); sie öffnet nur den Chat und gibt nie etwas frei.

**Gelesen-Stand und Mitteilungen (PLAN 48, Entscheidung des Inhabers vom 5.11.2026):**
- Solange ein Chat auf einem Gerät sichtbar offen ist, meldet die Seite das alle 15 Sekunden (`POST /api/v1/agent/chats/{id}/seen`). Der Chat gilt dann auf allen Geräten des Kontos als gelesen.
- Endet ein Lauf, während der Chat irgendwo sichtbar ist (Meldung jünger als 40 Sekunden), geht **keine** Mitteilung raus, auch nicht bei Freigabe oder Fehler. Sonst geht sie sofort raus, ohne Wartezeit.
- Je Chat höchstens eine ungelesene Mitteilung: Solange die letzte nicht angesehen wurde, kommt keine weitere (z. B. bei mehreren Freigaben in einem Lauf). Eine neue ersetzt die alte (gleicher Tag `agent-<chat>`).
- Mitteilungen gelesener Chats verschwinden, sobald TitanOS oder der Agent auf dem Gerät geöffnet wird (Service Worker, `getNotifications`). Sofort beim Lesen auf einem anderen Gerät geht das nicht: iOS erlaubt keinen stillen Push.
- Die Chat-Liste liefert `unread`. Gelesen-Stände liegen in `.titanos/agent/<konto>/seen.json` und werden nur geschrieben, wenn sich der Chat seit dem letzten Ansehen geändert hat; den letzten Stand der Chats hält titand im RAM, die 15-Sekunden-Meldung greift ohne Änderung nicht auf die Platte zu (Ruhemodus). Chats von vor dem Update gelten als gelesen.

## Seiten und Chat-Bedienung (PLAN 90-4, ab 0.83)

Entscheidung des Inhabers vom 14.11.2026 (Fragerunde in `PLAN.md` §90, Entwurfsbilder abgenommen): der Agent ist aufgebaut wie die Systemsteuerung 0.81 – Gruppen aus flachen Zeilen (`SettingGroup`/`SettingRow`), nichts doppelt.

- **Seiten statt Dialoge.** Geplant, Gedächtnis, Wissen und Einstellungen stehen im Platz des Chats; die Adresse nennt die Seite (`/agent?page=planned|memory|knowledge|settings`, Unterseiten der Einstellungen `&part=models|speech|personality|initiative|features|push|look|gestures`), „Zurück“ des Browsers funktioniert (seit 1.01 auch vom Chat zurück zur Startseite; vorher änderte sich nur die Adresse), ein Fenster kommt dort wieder, wo es war. Der offene Chat bleibt dabei geladen (`&chat=`). Am Arbeitsplatz heißt die Überschrift „KI-Agent › Seite“, am Handy nennt der Kopf die Seite.
- **Startseite** (kein Chat offen; `HomePage.svelte`): „Wartet auf dich“ (Chats, die auf eine Freigabe warten – „Ansehen“ führt in den Chat, freigegeben wird nur dort, wo der Schritt ganz zu sehen ist – und die Vorschläge des Agenten mit Ja/Nein, gemerkte Sätze vorher änderbar), „Läuft gerade“ (mit Anhalten), „NAS-Zustand“, „Als Nächstes geplant“ (die drei nächsten Läufe), „Zuletzt erledigt“ (die letzten Chats aus geplanten Aufträgen, Erinnerungen und Ereignissen) und „Passt gerade“ (bis zu drei Fragen, füllen das Eingabefeld). Einen eigenen Punkt „Vorschläge“ gibt es nicht mehr.
- **NAS-Zustand ohne Modell:** `GET /api/v1/agent/home` liefert Zahlen, die titand schon hat (Volumes mit Zustand, Belegung und „voll in etwa n Tagen“ aus den täglichen Proben der Vorausschau; aktive Sicherungsaufträge mit letztem Erfolg und Fehlschlägen; laufende und gestoppte Container), eine Minute zwischengespeichert. Nur Besitzer und Administratoren bekommen sie (`state: null` sonst, die Gruppe fehlt dann). Die Sätze schreibt die Oberfläche; jede Zeile sagt ihren Zustand als Wort („Gut“, „Achtung“, „Fehler“), nicht nur über die Farbe, und führt zur passenden Seite von TitanOS. Updates stammen aus den Meldungen der letzten zwei Tage.
- **Gedächtnis** (`MemoryPage.svelte`): Suche, Ordnung nach Thema / zuletzt genutzt / neueste; je Merksatz Herkunft (Chat, anklickbar), Datum, „zuletzt genutzt“, Anheften, Bearbeiten (Text und Thema), Löschen (mit Rückfrage). Am Handy wischt man einen Merksatz: nach rechts anheften/lösen, nach links löschen (mit Rückfrage), langes Drücken bearbeitet. Neue Felder je Merksatz in `memory.json`: `topic` (Gruppe; ohne Thema gruppiert das Projekt, sonst „Ohne Thema“; der Agent kann es bei `memory_save` mitgeben), `pinned` und `used`. **Angeheftet** heißt: verblasst nie, bleibt bei „Gedächtnis leeren“ und beim Kürzen auf 60 Merksätze im Prompt bzw. auf die Höchstzahl erhalten. **Zuletzt genutzt** setzt titand, wenn eine Nachricht der Person einen Merksatz berührt (zwei längere Wörter gemeinsam, bei kurzen Merksätzen ihr einziges), höchstens einmal am Tag je Merksatz; Verblassen rechnet ab dem späteren von Datum und letzter Nutzung. Darunter stehen die Einstellungen des Gedächtnisses (Selbst merken, Widersprüche nachfragen, Altes verblassen lassen 1–24 Monate oder unbegrenzt, Gedächtnis leeren) – nur noch hier, nicht mehr unter „Eigeninitiative“.
- **Wissen** (`KnowledgePage.svelte`): Meine Anweisung, Projekt-Anweisungen (öffnen das Projekt), Skills (aufklappbar, Schalter, eigene änder- und löschbar), NAS-Landkarte, Dokumente (die Orte, an denen er nachschlagen darf; früher „Wo er sucht“ unter Eigeninitiative) und für Besitzer und Administratoren das Protokoll. Ein Suchfeld filtert die Zeilen.
- **Einstellungen** (`SettingsPage.svelte`): Modell und Sprache · Verhalten (Persönlichkeit, Eigeninitiative, Funktionen) · Dieses Gerät (Mitteilungen, Darstellung, Gesten, Beim Öffnen) · NAS (Link in die Systemsteuerung, nur Besitzer und Administratoren) · Daten (Alle Chats löschen). Geplant, Gedächtnis, Skills, NAS-Landkarte, Protokoll und Meine Anweisung stehen dort nicht mehr.
- **Nachricht bearbeiten, Antwort neu erzeugen:** `POST /api/v1/agent/chats/{id}/messages` mit `replace` (Kennung einer eigenen Nachricht des Chats): sie und alles danach verlassen den Chat, dann wird `message` gefragt; ohne `message` dieselbe noch einmal. Ein wartender Schritt verfällt unbeantwortet, der Plan wird verworfen. **Was Schritte danach am NAS geändert haben, bleibt geändert** – nur der Chat vergisst sie; „Rückgängig“ eines Schritts gibt es weiter nur, solange er im Chat steht.
- **Suche im Chat:** Lupe in der Kopfzeile, ⋯-Menü, „/suche“ oder das Blatt einer Nachricht. Treffer in eigenen Nachrichten und Antworten werden gezählt („2 von 3“), Enter/Umschalt+Enter bzw. die Pfeile springen, Escape schließt. Markiert wird mit der CSS Custom Highlight API, die Seite bleibt unverändert; ein Browser ohne sie (Firefox vor 140, Safari vor 17.2) zählt und springt, zeigt aber keine Marken. Eingeklappte Schritte und Gedanken werden nicht durchsucht.
- **Befehle:** „/“ am Anfang des Eingabefelds öffnet die Liste (Pfeiltasten, Enter, Tab oder Tippen): `/neu`, `/projekt [Name]`, `/modell`, `/merke <Text>` (schreibt direkt ins Gedächtnis; ohne Text öffnet die Seite), `/plane <Text>` (bittet den Agenten, es zu planen; ohne Text die Seite Geplant), `/suche <Text>`, `/zustand` (fragt den Gesundheitscheck). Englisch: `/new`, `/project`, `/model`, `/remember`, `/plan`, `/find`, `/status`. Ein Befehl wird nie als Nachricht gesendet.
- **Rückfragen:** Löschen eines Merksatzes, eines Skills und eines geplanten Auftrags sowie „Gedächtnis leeren“ fragen vorher in einem Dialog (Abbrechen zuerst im Markup, kein Autofokus auf dem gefährlichen Knopf).
- **Fokus in Blättern und Menüs (seit 1.01):** Blätter von unten, Aktionen eines Chats, das Menü über dem Chat am Handy, Notizen, Modellwahl, Sprechmodus und die Blätter von „Dokumente“ und „Schutz“ sind keine `<dialog>`; sie halten den Fokus mit `lib/focus-trap.ts`: Der Fokus geht hinein, Tab bleibt darin, Escape schließt nur das oberste (nicht, wenn ein Dialog darüber offen ist), danach steht der Fokus wieder dort, wo er vorher war. Dialoge aus `Dialog.svelte` tun das als `<dialog>` selbst.
- **Geplant, Morgenbericht:** Das offene Formular behält, was getippt ist, wenn die Liste neu gelesen wird (anderen Auftrag schalten, speichern, „Jetzt ausführen“); es übernimmt den gespeicherten Bericht nur, wenn der sich geändert hat (seit 1.01).
- **Listen in einem Schritt (seit 1.01):** Projekte, Skills, Gemerktes und geplante Aufträge werden unter einer Sperre gelesen, geändert und geschrieben (`Store.UpdateProjects`, `UpdateSkills`, `UpdateMemories`, `UpdateSchedules`), von der Seite wie vom Agenten. Zwei Änderungen zur selben Zeit kommen beide an, und ein von der Seite gespeicherter Auftrag behält einen Lauf, den das NAS inzwischen eingetragen hat.

Entfallen sind `AgentSettings.svelte`, `KnowledgeDialog.svelte`, `InboxPanel.svelte` und `AgentStart.svelte` (mit den Projekt-Chips der alten Startseite; Projekte stehen in der Seitenleiste). Das Blatt eines Projekts (`ProjectSheet.svelte`: Chats, Anweisung, die vom Agenten angelegten Dateien, Gemerktes) öffnet jetzt „Chats und Dateien“ unter Wissen › Projekt-Anweisungen.

## Grundanweisung, Landkarte, Beschreibungen

- **Grundanweisung** (Einstellungen › KI-Agent): gilt in jedem Chat aller Konten, vor allem anderen; mit Vorlage zum Ausfüllen.
- **Meine Anweisung** (im Agenten auf der Seite „Wissen“): zusätzlich je Konto.
- **NAS-Landkarte** (im Agenten, Reiter „NAS-Landkarte“): Freigaben mit ihren obersten Ordnern, Apps mit Port, VMs, Systemcontainer, Sicherungen und Räume aus Home Assistant. TitanOS liest sie nachts um 3 Uhr und auf „Neu einlesen“ ein und legt sie auf dem Daten-Volume ab; eine Kurzfassung geht in jeden Auftrag, Einzelheiten holt der Agent mit `nas_map`. Benutzer sehen nur ihre Freigaben.
- **Beschreibungen** zu Freigaben, Ordnern, Apps, VMs, Systemcontainern, Sicherungen und Räumen („Eingescannte Briefe“); Besitzer und Administratoren schreiben sie in der Landkarte, der Agent schlägt selbst welche vor und speichert sie (gekennzeichnet „vom Agenten“).

## Mehr im Chat

- **Anhänge:** Die Büroklammer im Eingabefeld bietet „Vom Gerät hochladen“ (bis 20 MB je Datei, Text, Word, Excel, PDF; größere Dateien werden mit einer Meldung abgelehnt) und „Vom NAS wählen“: eine Dateiauswahl über die Freigaben, die das Konto im Dateimanager sieht (gleiche Rechte, Suche, bis 10 Dateien). Dateien vom NAS werden nicht kopiert, der Agent liest sie mit `file_read` an ihrem Ort (`[Datei: root/pfad]`), auch große. Dasselbe geht im Dateimanager per Rechtsklick „Mit KI-Agent …“. Hochgeladene Anhänge liegen beim Chat und verschwinden mit ihm. Zwei Dateien mit gleichem Namen bleiben beide (seit 1.01; am iPhone heißt jedes Foto „image.jpg“): Im Eingabefeld heißt die zweite „image (2).jpg“, und gibt es im Chat schon eine Datei mit dem Namen, legt das NAS die neue unter einem freien Namen ab und nennt ihn; die Nachricht nennt die Datei, die wirklich abgelegt ist. Vorher fiel die zweite still weg oder ersetzte die erste. Der Titel eines Chats kommt aus der Frage, nicht aus den Dateinamen.
- **Rückgängig** an Schritten, wo TitanOS es kann: ersetzte Datei (alte Fassung zurück, neue in den Papierkorb), gelöschte Dateien in einer Freigabe mit Papierkorb, Schalten in Home Assistant (innerhalb von 30 Minuten).
- **Export** als Markdown oder PDF.
- **Glocke:** „Agent ansehen lassen“ an jeder Meldung. **Dashboard** (bis 0.59.0, seitdem entfernt): Kachel „KI-Agent“ mit laufenden Aufträgen und Eingabefeld.
- **Vorladen:** Beim Öffnen lädt Ollama das Modell schon; wie lange es geladen bleibt, stellt „Modell im Speicher halten“ ein. Neue Modelle lädt „Modell herunterladen“ direkt auf den Ollama-Server.
- **Cluster:** Auf einem verwaltenden Knoten sieht der Agent die anderen Knoten (`cluster_nodes`, `cluster_read`) und startet/stoppt dort Container, VMs und Systemcontainer (`cluster_action`), als dein Konto über den Cluster-Proxy. Das ist bewusst mehr als bei Tokens und MCP (Entscheidung des Projektinhabers vom 5.11.2026).
- **Agent eines anderen Knotens** (PLAN 47, Entscheidung 5.11.2026): Jeder Knoten eines Clusters wählt unter Einstellungen › KI-Agent „Eigener Agent“ oder „Agent von <Admin-Knoten>“. Mit dem Agenten des Admin-Knotens kommt nur das Modell von dort; Chats, Gedächtnis, Skills, Rechte und Ordner bleiben auf dem Knoten, er braucht keine eigene Ollama-Adresse. Der Knoten fragt mit „Freigabe anfragen“ an; der Admin-Knoten meldet das (`agent_cluster_request`) und gibt unter „Andere Knoten im Cluster“ frei, entzieht oder entfernt die Anfrage. Die Anfragen laufen signiert über den Cluster (`/agent/…` der Cluster-Routen, nur Admin-Knoten); weitergereicht werden nur `/api/version`, `/api/tags`, `/api/show`, `/api/ps` und `/api/chat` von Ollama, kein Herunterladen. Modelle lädt man auf dem Admin-Knoten.
- **Knopf in Home Assistant:** Ein geplanter Auftrag mit Häkchen „Als Knopf in Home Assistant anbieten“ erscheint dort als Knopf (MQTT-Gruppe „KI-Agent“ unter Einstellungen › Home Assistant).
- **Wochenbericht** (Einstellungen › KI-Agent): montags um 8 Uhr über die Benachrichtigungen, was der Agent geändert hat. Das Protokoll steht auch im Agenten.

## Notizen (PLAN 57)

Im Agent-Menü unter „Geplant“ öffnet **Notizen** eigene Notizen mit Ordnern (Entscheidungen des Inhabers vom 6.11.2026):

- Jede Notiz ist eine Markdown-Datei, jeder Ordner ein echter Ordner unter `KI-Agent/<konto>/Notizen/`, bis vier Ebenen tief. Sie sind im Dateimanager und per SMB zu sehen, nur für das eigene Konto.
- Bedienung:
  - Ordner anlegen, umbenennen und löschen (samt Inhalt, mit Rückfrage).
  - Notizen anlegen, umbenennen, in Ordner verschieben, anheften und löschen; dazu eine Suche über Titel und Anfang.
  - Am Desktop stehen Liste und Notiz nebeneinander. Am Handy wischt die Notiz über die Liste.
- **Formatleiste:** Überschrift, Fett, Kursiv, Liste, Checkliste (`- [ ] `), Trennlinie. Gespeichert als Markdown, von selbst nach kurzer Pause und beim Verlassen. Wurde die Datei inzwischen woanders geändert (anderes Gerät, Dateimanager), speichert TitanOS nicht darüber und bietet „Neu laden“.
- **Der Agent** liest sie (`notes_list`, `note_read`) und schreibt mit seinen Rechten (`note_write`): Eine neue Notiz legt er ohne Nachfrage an. Ergänzen (`append`), Neuschreiben (`replace`) und Löschen (`note_delete`) sind Änderungen und fragen in der Schnellwahl „Standard“ nach. Die Namen der Notizen bekommt er wie bisher in jedem Auftrag.
- Angeheftete merkt sich TitanOS privat in `.titanos/agent/<konto>/notes.json`. Alles geht über `os.Root`: Ein Link im Ordner führt nicht hinaus.

## Wie der Agent arbeitet (PLAN 55)

Der Agent arbeitet nicht nur Frage für Frage ab. Er hält einen Auftrag selbst zusammen:

- **Plan:** Bei mehr als zwei Schritten schreibt er zuerst einen kurzen Plan (`plan_update`) und hakt ihn ab. Der Plan steht unten im Chat („2 von 3 erledigt“) und geht in jede Runde mit. Eine neue Frage beginnt ohne den alten Plan.
- **Kontext:** Würde die nächste Anfrage mehr als 70 % des Kontexts füllen, fasst er die älteren Teile kurz zusammen. Im Chat steht dann „Ältere Teile des Chats zusammengefasst“, zum Aufklappen. Die Grundregeln und die Frage des laufenden Auftrags bleiben immer erhalten. Ohne eigene Kontextlänge fragt TitanOS Ollama, womit das Modell geladen ist. Ein einzelnes Ergebnis darf höchstens ein Viertel des Kontexts belegen. **Seit 1.01:** Füllen Grundregeln, Werkzeuge, Plan, bisherige Zusammenfassung und die Frage allein schon 70 % (sehr kleines Kontextfenster), hilft Zusammenfassen nicht mehr; dann fasst er in einem Auftrag höchstens einmal zusammen statt vor jeder Runde. Ein Teil, der kleiner ist als ein Zehntel des Fensters (mindestens 400 Token), wird gar nicht zusammengefasst.
- **Werkzeuge nach Bedarf** (ab Werk an): Zuerst bekommt das Modell nur die wichtigsten Bereiche (Dateien, System, eigene Werkzeuge, Internet), weitere lädt es mit `tools_load`. Das spart viel Kontext: Alle Werkzeuge und die Grundanweisung zusammen belegen mit gemma4:12b rund 12 600 Token. Abschaltbar unter „Werkzeuge nach Bedarf laden“.
- **Helfer für Unteraufträge** (`task_delegate`, ab Werk an): Für breites Nachsehen startet der Agent einen Helfer mit leerem Kontext. Der Helfer darf nur ohne Nachfrage lesen und gibt einen Bericht zurück. Seine Schritte zählen zur Grenze des Auftrags.
- **Fehler:** Antwortet Ollama nicht oder mit einem Fehler 5xx, versucht er es bis zu drei Mal; was schon gestreamt war, verschwindet dabei wieder. Einen kaputten Werkzeugaufruf bekommt das Modell als Hinweis zurück. Ruft es denselben Schritt mit denselben Angaben ein drittes Mal, wird der Aufruf nicht ausgeführt; beim fünften hält der Auftrag an („derselbe Schritt kam immer wieder“).
- **Prüfen:** Hat er etwas geändert und danach nichts gelesen, fordert ihn TitanOS einmal auf, das Ergebnis nachzuprüfen („Prüft das Ergebnis nach“).
- **Grenzen:** Schritte je Auftrag (Standard 30) und Zeit je Auftrag (Standard 30 Minuten). An der Grenze schreibt er ohne Werkzeuge, was erledigt ist, was offen ist und was als Nächstes käme. Mit „Weitermachen“ geht es weiter.

### Code ausführen (Sandbox)

Unter Einstellungen › KI-Agent › Verbindung und Modelle › „Code ausführen in einer Sandbox“, **ab Werk aus**, nur mit eingeschaltetem Docker (Entscheidung des Inhabers vom 5.11.2026: der Mittelweg zwischen gar keinem Code und einer Konsole).

- Werkzeug `code_run`: Python 3 oder sh (busybox), z. B. zum Rechnen, Umwandeln, Sortieren oder Prüfen von Daten.
- **Bibliotheken für Word, Excel und PDF (seit 0.92, Entscheidung des Inhabers vom 15.11.2026: der Agent bekommt einen Arbeitsplatz, der Dokumente wirklich ändern kann):** `python-docx`, `openpyxl` und `pypdf` (mit `lxml`, `et-xmlfile`, `typing-extensions`). Damit geht, was `document_edit` nicht kann: Zeilen einfügen, Formeln setzen, PDFs zusammenfügen oder teilen. Der erste Lauf lädt sie einmal von PyPI (`docker.SandboxLibs`): feste Versionen, jede Datei gegen ihre SHA-256-Summe geprüft (`--require-hashes`), nur fertige Pakete (`--only-binary`), sodass beim Einrichten kein Code eines Pakets läuft. Sie liegen auf dem Volume des Agenten unter `.titanos/agent/sandbox-libs-1/` (rund 17 MB) und werden in jeden Lauf nur lesend als `/libs` eingebunden. **Die Läufe selbst haben weiter kein Netz.** Gelingt das Laden nicht (kein Internet), läuft das Programm ohne sie und das Ergebnis sagt es. Eine Datei aus einer anderen Freigabe kopiert der Agent zuerst mit `file_copy` in seinen Ordner und das Ergebnis zurück.
- Jeder Lauf ist ein eigener Wegwerf-Container (`python:3.13-alpine`, fest per Digest): kein Netz, schreibgeschütztes System, `/tmp` im RAM (64 MiB), läuft als das Konto. Er sieht nur den eigenen Ordner der Freigabe „KI-Agent“ als `/work` (lesen und schreiben), sonst nichts vom NAS. Ohne Rechte (`CapDrop ALL`, `no-new-privileges`), mit höchstens 128 Prozessen und 1 CPU. Zeit (Standard 60 s, bis 600 s) und Arbeitsspeicher (Standard 512 MiB) sind einstellbar. Danach wird der Container gelöscht, auch bei Abbruch oder Zeitüberschreitung.
- **Fragt vor jedem Lauf** und zeigt dabei das ganze Programm. Ohne Nachfrage läuft es nur, wenn `code_run` oder der Bereich „Code ausführen“ ausdrücklich auf „Ohne Nachfrage“ steht; „Alles ohne Nachfrage“ reicht dafür nicht.
- Die Ausgabe (höchstens 64 KiB) gilt als fremder Inhalt: Danach fragt die Websuche nach.
- Die Container erscheinen nirgends als App und werden nie aktualisiert oder gesichert. Das Image lädt der erste Lauf.

## Was der Agent darf

Der Agent nutzt **denselben Werkzeugkatalog wie der MCP-Server**, aber mit eigenen Rechten:

- **Schnellwahl:** Nur lesen · Standard (lesen und Neues anlegen ohne Nachfrage, alles andere mit Nachfrage) · Alles ohne Nachfrage.
- **Arbeitsweise je Chat (seit 0.92, Wunsch des Inhabers vom 15.11.2026: Plan besprechen, dann läuft er durch):** im „＋“ des Eingabefelds – **Direkt loslegen** (Standard: er fängt an, fragt nicht, ob er beginnen oder weitermachen soll, und wartet nur dort, wo die Rechte des NAS nachfragen), **Plan zeigen, dann loslegen** (er schreibt zuerst den Plan und arbeitet ihn nach einem Ja durch; das steht in der Anweisung an das Modell, TitanOS erzwingt es nicht) und **Jeden Schritt fragen** (jede Änderung wartet auf die Freigabe, auch wo die Rechte „Ohne Nachfrage“ sagen; das erzwingt TitanOS). Die Wahl steht am Chat (`autonomy`: leer, `plan`, `ask`; `POST`/`PATCH /api/v1/agent/chats…` nehmen `direct`, `plan`, `ask`), ein neuer Chat nimmt die zuletzt auf dem Gerät gewählte. Sie verschärft nur: Was die Rechte verbieten oder nachfragen lassen, bleibt so. Wer wirklich ohne Nachfragen arbeiten will, stellt zusätzlich die Schnellwahl auf „Alles ohne Nachfrage“. Geplante und vom NAS gestartete Läufe warten nie auf ein Ja zum Plan.
- **Bereiche und Werkzeuge:** je Bereich und je Werkzeug „Ohne Nachfrage“, „Nachfragen“ oder „Verboten“. Verbotene Werkzeuge bekommt das Modell gar nicht angeboten; ruft es eines trotzdem, lehnt TitanOS ab.
- **Ordner-Regeln:** je Ordner dieselben drei Stufen; die strengere Regel gewinnt. „Verboten“ blendet den Ordner auch in Listen und Suchergebnissen aus.
- **Nachfragen:** Der Auftrag hält an, im Chat erscheint eine Karte mit „Ausführen/Abbrechen“. „Ausführen“ führt genau den geplanten Aufruf aus, das Modell plant nicht neu. Freigeben kann nur der Besitzer des Chats in seiner Web-Sitzung.
- **Nie:** Passwörter, Tokens, 2FA/Passkeys, Konsolen, Netzwerkbestätigung, die Einstellungen von Agent und MCP. Diese Werkzeuge gibt es im Katalog nicht.
- **Konten:** Besitzer immer; weitere Konten unter Einstellungen › KI-Agent. Jedes Konto hat eigene Chats, Projekte und eigenes Gedächtnis; Benutzer sehen nur ihre Freigaben und haben keine Verwaltungswerkzeuge.

Jede Änderung steht im Aktivitätsprotokoll mit Herkunft **per KI-Agent** (Aufrufe der TitanOS-API über die Handler-Kette; Automationen und Skripte in Home Assistant als eigener Eintrag).

## Ablageorte, Gedächtnis, Skills

- **Ablageorte** geben Ordnern Namen („Home-Assistant-Backups“ → Freigabe Backup, Ordner `Home Assistant`). Freigaben, Apps und VMs kennt der Agent von selbst aus einem kurzen Lagebericht vor jedem Auftrag.
- **Gedächtnis:** Der Agent merkt sich selbst, was du ihm sagst oder korrigierst („mein HA ist die VM“). Einträge sind auf der Seite „Gedächtnis“ einsehbar, durchsuchbar, änderbar, anheftbar und löschbar (PLAN 90-4, siehe „Seiten und Chat-Bedienung“). Passwörter und Schlüssel werden nie gespeichert.
- **Skills:** 34 mitgelieferte Abläufe (`agent.BuiltInSkills`: Status eines Dienstes, Gesundheitscheck, Datei finden, Dokument erstellen, Backup-Stand, Speicher aufräumen, Platten, RAID, Apps, VMs, Freigaben, Netzwerk, Rechnungen, Home Assistant … und seit 0.92 Recherche im Web, Word- oder Excel-Datei ändern, PDFs zusammenfügen/teilen/lesen, Dokument zusammenfassen, Tabelle auswerten, Automation in Home Assistant anlegen). **Sie sind für jedes Konto des Agenten von Anfang an eingeschaltet** und gehen in jeden Auftrag mit; jede Person kann einzelne auf der Seite „Wissen“ für sich abschalten. Seit 0.92 (Durchsicht vom 15.11.2026) bitten die Abläufe im Chat nicht mehr um Erlaubnis („erst nach Zustimmung“): Ob ein Schritt nachfragt, entscheiden die Rechte des NAS über die Freigabe-Karte, nicht der Ablauf. Gefragt wird nur noch, wo es eine echte Wahl gibt (welche Version wiederherstellen, welche Kopie löschen). Die Abläufe des Skill-Katalogs (`TitanOS-Apps/skills`, 17 Stück) sind etwas anderes: Sie kommen erst mit der Funktion „Skill-Katalog“, die ab Werk aus ist, und gelten als fremder Inhalt. Dazu eigene, die der Agent selbst schreibt, wenn sich etwas wiederholt.
- Einträge, die entstanden sind, nachdem der Agent Dateien oder Webseiten gelesen hatte, tragen das Zeichen „aus fremdem Inhalt“.

Das Modell selbst lernt nicht dazu; „Lernen“ heißt: Gedächtnis und Skills gehen in jeden Auftrag mit.

## Internet

Websuche und Seitenlesen (ab Werk an, Bereich „Internet“). Die Suche läuft ab Werk über die Seite von DuckDuckGo. **Eigene Websuche (seit 0.92, Wunsch des Inhabers vom 15.11.2026):** Unter Einstellungen › KI-Agent › Verbindung und Modelle › „Eigene Websuche (SearXNG)“ steht die Adresse eines SearXNG, in der Regel der Docker-App „SearXNG“ aus dem App-Katalog (Port 8580; ihre Vorlage schaltet die nötige JSON-Ausgabe ein). Dann fragt `web_search` dort (`GET /search?format=json`, bis zu 12 Treffer mit Titel, Adresse und Auszug). Die Adresse darf im Heimnetz liegen, deshalb gilt für diese eine Abfrage die Sperre der Heimnetz-Adressen nicht; Weiterleitungen werden nicht verfolgt, und gelesen werden weiterhin nur öffentliche Seiten. Antwortet die eigene Suche nicht, nimmt der Agent DuckDuckGo und sagt das im Ergebnis. Für Recherchen gibt es den mitgelieferten Ablauf „Research a topic on the web“ (mehrere Suchen, vier bis sechs Seiten lesen, Widersprüche nennen, am Ende die Liste „Quellen“). Adressen im Heimnetz und das NAS selbst sind gesperrt, auch nach Weiterleitungen. Hat ein Chat schon Inhalte von außen gelesen – Dateien, Geräte aus Home Assistant, Protokolle, Anhänge – oder wurde er durch eine Meldung des NAS gestartet, fragen Websuche und Seitenlesen für den Rest des Chats immer nach, damit nichts vom NAS nach außen gelangt. **Seit 0.94 (Rückmeldung des Inhabers vom 15.11.2026: trotz „Internet ohne Nachfrage“ fragte der Agent dauernd):** Hat der Chat bisher nur das Web gelesen (`Chat.WebOnly`), sucht er ohne diese Nachfrage weiter und liest Seiten, deren Adresse schon wörtlich im Chat stand – in einer Nachricht der Person oder im Ergebnis einer früheren Suche oder Seite. Eine Adresse, die das Modell selbst zusammensetzt, fragt weiter nach; Ergebnisse anderer Werkzeuge und Fehlermeldungen zählen nicht, weil sie wiederholen können, was das Modell ihnen gab (`webStaysFree`). Der Schalter „Internet auch nach gelesenen Inhalten ohne Nachfrage“ unter Rechte (`Policy.WebFree`, ab Werk aus) nimmt die Nachfrage für alle Chats weg; das Risiko – ein gelesener Inhalt weist das Modell an, Daten in eine Suche oder Adresse zu packen – trägt dann die Person. Ein Skill von außen im Prompt fragt in jedem Fall. Außerdem sagt die Anweisung „direkt loslegen“ seit 0.94, dass ein Gruß kein Auftrag ist und kein Werkzeug braucht. DuckDuckGo hat keine offizielle Such-API; ändert sich die Seite, meldet der Agent „Suche nicht verfügbar“.

## Läufe: Schnappschuss und mehrere Chats zugleich (seit 1.01)

Entscheidung des Inhabers vom 16.11.2026 („Ja, einstellbar“). Code: `api/agent_runs.go`, `api/agent.go` (Start und Ereignisstrom), `routes/agent/agent-chats.svelte.ts`.

- **Schnappschuss beim Anhängen.** `GET /api/v1/agent/chats/{id}/events` sendet zuerst `event: snapshot` mit `{"type":"snapshot","entries":[…],"thinking":"…","content":"…","step":{…},"state":"running","error":"…","pending":{…},"plan":[…]}`. `entries` sind die Einträge, die der laufende Lauf bisher geschrieben hat (nicht die davor), `thinking` und `content` der Teiltext seit seiner letzten Zeile, `step` der laufende Schritt; `plan` steht nur da, wenn der Lauf den Plan geändert hat, leere Felder fehlen. Danach kommen wie bisher `event: state` (`running`) und die Ereignisse. Die Seite ersetzt den Teiltext und führt die Einträge per `id` zusammen (`mergeEntries` in `lib/agent.ts`: die Einträge des Laufs stehen in seiner Reihenfolge am Ende). titand hält dafür je Lauf, was ein Schnappschuss braucht (`agentRunView`), allein aus den Ereignissen, nie aus dem Chat, den der Lauf gerade ändert. Schnappschuss und Anmelden am Lauf geschehen unter derselben Sperre: nichts geht dazwischen verloren.
- **Zu langsame Seite:** Ist ihr Puffer voll (256 Ereignisse), bekommt sie einen neuen Schnappschuss statt stiller Lücken; was davor noch im Puffer lag, fällt weg, weil der Schnappschuss es enthält. Die Verbindung bleibt offen.
- **Rückwärtskompatibel:** Ein älterer Client ignoriert den unbekannten Typ und bekommt seine `state`-Zeile wie bisher; ein neuer Client an einem älteren titand (Cluster) arbeitet ohne Schnappschuss wie in 1.00. Einträge, die ein Lauf ohne Ereignis schreibt (nicht ausgeführte Aufrufe an der Schrittgrenze, die Reihenfolge nach einer Freigabe), bringt wie bisher das Neuladen am Ende.
- **Gleichzeitige Läufe je Konto** (Einstellungen › KI-Agent › Verbindung und Modelle, nur Besitzer und Administratoren): „Ohne Grenze“ (ab Werk, wie bis 1.00) oder 1 bis 4. Feld `maxRuns` in `agent.json`; fehlt es oder ist es 0, gibt es keine Grenze, ältere Versionen lesen die Datei weiter; 5 und mehr sind ungültig. Die Grenze ist eine Möglichkeit: 1.01 nimmt nichts weg, was 1.00 erlaubte. Ein Chat hat weiter höchstens einen Lauf (409 `agent_busy`).
- **Was zählt:** Gezählt und an der Grenze abgewiesen werden nur Läufe, die die Person im Chat startet – neuer Chat, Nachricht, „Neu erzeugen“, Freigabe eines Schritts. Geplante Aufträge, die Reaktion auf Meldungen und Fragen aus Home Assistant starten wie bisher und zählen nicht: sie liefen schon in 1.00 neben den Chats, und mit einer Grenze von 1 sollen sie nicht scheitern. Das Profil von Home Assistant (`@assist`) zählt für sich. Dieselbe Grenze gilt mit einem externen Anbieter (OpenAI-kompatibel, Claude).
- **An der Grenze** antwortet titand mit 409 `agent_limit`, bevor etwas geschrieben ist: der Platz des Laufs wird zuerst belegt (`agentClaim`), erst dann wird die Nachricht gespeichert. Die Nachricht bleibt im Eingabefeld, die Seite sagt „Es läuft schon ein Chat …“ bzw. „Es laufen schon N Chats …“ (N aus `maxRuns` in `/agent/status`, 0 heißt ohne Grenze).
- **Ollama:** Damit N Läufe wirklich zugleich arbeiten, muss Ollama mit `OLLAMA_NUM_PARALLEL` ≥ N laufen, sonst wartet ein Lauf auf den anderen. Eine einzelne Antwort wird dadurch nicht schneller, und jeder Lauf belegt eigenen Speicher für seinen Kontext. TitanOS verwaltet Ollama nicht; die Einstellung zeigt den Hinweis bei „Ohne Grenze“ und ab 2 (beim Anbieter: dieselbe Grenze, dessen Limits und Kosten gelten je Anfrage).
- **`GET /api/v1/agent/runs`** liefert die Chats des Kontos, die gerade laufen, und die Grenze (`{"runs":[…],"max":N}`), nur aus dem RAM.
- **Oberfläche:** Laufend, Teiltext, Freigeben und Senden stehen je Chat (`ChatRun`). Man kann während eines Laufs in einen anderen Chat wechseln, dort senden (wenn die Grenze es erlaubt), zurückwechseln und den ersten Lauf live weitersehen: die Seite hängt sich neu an und übernimmt den Schnappschuss. Gefolgt wird nur dem offenen Chat (ohne TLS erlaubt ein Browser je Adresse nur wenige Verbindungen); ein Lauf in einem anderen Chat wird alle 4 Sekunden über `/agent/runs` abgefragt, bis er endet, und nach acht Antworten ohne einen solchen Lauf nicht mehr. Die Seitenleiste zeigt laufende Chats mit der vorhandenen pulsierenden Marke. Die acht Versuche aus 1.00 für einen Chat, der „arbeitet“ meldet, ohne dass ein Lauf antwortet, gelten je Chat. Der Haken „Fertig“ erscheint nur in dem Chat, dessen Lauf zu Ende ging.
- **Je Chat statt je Konto (durchgesehen):** Freigabe (`chat.Pending`; `decide` nimmt nur den Schritt dieses Chats), Anhalten (je Lauf), Mitteilungen (je Chat, Tag `agent-<chat>`), Assist (eigenes Profil, nimmt einen Chat, der gerade nicht läuft) und Aufträge (eigener Chat je Lauf) waren im Backend schon je Chat. Mit Absicht für das ganze NAS bleiben: die nächtliche Rückschau, das Einlesen der Dokumente, der Umzug des Volumes und ein App-Update warten, solange irgendein Lauf arbeitet.
- **`ChatView.svelte` zerlegt** (`lib/components/agent/chat/`): `UserMessage` (eigene Nachricht mit Bearbeiten und langem Drücken), `MessageSheet` (ihre Aktionen am Handy), `AnswerBlock` (Antwort mit Vorlesen, Kopieren, Neu erzeugen, Karten und Auswahlknöpfen), `Markdown` (Text mit „Tabelle kopieren“, „Kopieren“ und „Vergrößern“ und den Stilen dazu, auch für die hereinlaufende Antwort), `StepsLine`, `StepList` und `StepsSheet` (Schritte als Zeile, Liste und Blatt „Zusammenfassung“), `NoteLine`, `ChatFiles`, `PlanBox`, `LiveLine` (Figur, Wörter, Haken), `ApprovalCard` (Freigabe-Karte) und `ChatFind` (Suche im Chat); Texte der Schritte und Kopieren in `chat-text.svelte.ts`. `ChatView` behält Reihenfolge, Scrollen und Blätter. Das Verhalten ist gleich; jedes Teil bringt sein CSS mit.
- **Prüfung:** Go `TestAgentSnapshotAfterMissedEvents`, `TestAgentSlowPageGetsASnapshot`, `TestAgentRunLimit` (Grenze 1, abgewiesene Nachricht ändert nichts, geplanter Auftrag startet trotzdem), `TestAgentRunsSideBySide` (ohne Einstellung laufen 5 Chats zugleich; mit 2 und 4 laufen so viele zugleich, einer mehr wird abgewiesen; 5 ist ungültig), `TestAgentApprovalStaysInItsChat`; Web `tests/agent.test.mjs` (`mergeEntries`), `tests/components/agent-busy.test.ts` (kein Haken nach Chatwechsel), `scripts/test-ui-agent.mjs` (Wechsel während eines Laufs und zurück, zweiter Chat bei Grenze 2, Meldung an der Grenze).

## Die arbeitende Figur (seit 0.100)

Seit 1.01 (Inhaber 16.11.2026: „nur bei den 3 Punkten bleiben und nichts anderes noch mit reinholen. Dann hätten wir immer eine flüssige Animation“): **immer dieselbe Figur** – drei gleich große, schattierte Kugeln, die im Raum kreisen: der Ring kippt langsam hin und her und dreht sich, die Kugeln laufen zusammen, bis sie sich berühren, und wieder auseinander (Inhaber: „Kreisen 3 dimensional, zb auch drehend, zusammenlaufend und wieder auseinander“), 26 px, ein Tempo, für jede Tätigkeit und auch unter einer hereinlaufenden Antwort. Alle drei haben denselben Farbton; die Tiefe zeigt nur, welche Kugel vorn liegt. Die Figur wird nie neu aufgebaut und läuft über eine durchgehende Uhr weiter, während das Wort daneben wechselt. **Farben wählbar** (Einstellungen › Darstellung, je Konto, Inhaber: „Gut wäre wenn ich mir die Farbe für die Punkte und den Text aussuchen könnte“): „Farbe der Punkte“ (ab Werk das Orange des Agenten) und „Farbe des Textes neben den Punkten“ (ab Werk das Grau des Themes), je acht fertige Farben oder eine eigene, mit Vorschau; bei zu wenig Kontrast des Textes zum Hintergrund (unter 4,5:1) ein Hinweis. Gespeichert als `dotColor`/`textColor` (`#rrggbb` oder leer) in den Einstellungen des Kontos; der Haken „Fertig“ nimmt die Farbe der Punkte. `BusyFigure.svelte` zeichnet, `lib/agent-figures.ts` rechnet (reine Rechnung, `tests/agent-figures.test.mjs`); die Tätigkeit (`busyKind`) wählt nur noch das Wort. Bewegt wird nur `transform` je Bild (`requestAnimationFrame`); „Animationen: Aus“ und `prefers-reduced-motion` zeigen ein Standbild. Die Texte daneben: `agent.busy.*` (je Tätigkeit durchnummeriert; die Anzahl steht in `WORDS` in `chat/LiveLine.svelte`, der Test prüft beide Sprachen auf Lücken). Von 0.100 bis 1.00 hatte jede Tätigkeit eine eigene Figur (Kern und Ringe, Umlaufbahn, Jonglieren, Hüpfen).

## Durchsicht 0.99 (16.11.2026)

Auf Wunsch des Inhabers durchgesehen; Funde, Behobenes und Offenes stehen in `docs/releases/v0.99.0.md`. Was seitdem gilt:

- **Ordnerregeln:** Eine Regel zählt auch, wenn ein Aufruf einen Ordner als Ganzes kopiert, verschiebt, umbenennt oder löscht, in dem der geregelte Ordner liegt (`folderMode`, `realFolderMode(…, whole)`, `TestAgentFolderRuleCoversWholeFolders`). „Ohne Nachfrage“-Regeln lockern dabei nichts.
- **`image_view` mit `root`** bekommt den Modus von `file_read` und zählt zu den Datei-Werkzeugen (Wecken schlafender Platten).
- **Hilfs-Chat (`task_delegate`):** übernimmt `OutsideSkill`, `Schedule` und `Project`; sein Auftragstext ist keine Nachricht der Person (`Chat.Helper`), Adressen darin sind nicht „schon dagewesen“.
- **MCP** fällt wie das Internet unter die Nachfrage nach gelesenen Inhalten; `Policy.WebFree` hebt sie für beide auf.
- **Wartender Schritt:** Die nicht ausgeführten Aufrufe derselben Runde stehen nach dem Ergebnis des freigegebenen Schritts (`Chat.TakeSkipped`).
- **`chat.OutsideSkill`** wird bei jedem Lauf neu bestimmt.
- **Aufträge:** `Schedule.Since` (wieder eingeschaltet oder Zeit geändert) – davor wird nichts nachgeholt; „Jetzt ausführen“ setzt bei einmaligen Aufträgen `LastRun` nicht.
- **Anbieter:** `ProviderError` mit Status; 5xx wird erneut versucht. Der OpenAI-kompatible Strom braucht `[DONE]` oder `finish_reason`; `length` verwirft Aufrufe, unlesbare Angaben sind ein Fehler statt `{}`.
- **Gedächtnis:** `Store.UpdateMemories` für Laufstart und `memory_save`.
- **Oberfläche:** `AgentChats.finish` liest den Chat, bevor „arbeitet“ endet, und hängt sich an einen noch laufenden Auftrag wieder an (bis zu acht Versuche mit wachsendem Abstand); `send` gibt zurück, ob gesendet wurde, und sendet nie doppelt; der Entwurf liegt in `AgentChats.draft`.

## Anzeigename des Modells und Lesen während der Antwort (seit 0.98)

Wünsche des Inhabers vom 16.11.2026.

- **Anzeigename:** Einstellungen › KI-Agent › Verbindung und Modelle › „Angezeigter Name“ gibt dem gewählten Modell einen eigenen Namen (bis 40 Zeichen, `agentSettings.ModelNames`, Modell → Name). Der Chat zeigt ihn überall statt des Modellnamens (Kopf, Chip am Eingabefeld, Menü, Einstellungen des Agenten); in der Modellauswahl steht der echte Name klein darunter. An Ollama bzw. den Anbieter geht weiter der echte Name. `/agent/status` liefert `modelLabel` und je Modell `label` (`TestAgentModelNames`).
- **Hochscrollen während der Antwort:** Jede Bewegung nach oben löst das Mitlaufen sofort (`ChatView.svelte`, `scrolled`); vorher zog die Ansicht zurück, solange man weniger als 80 px vom Ende entfernt war – mit dem Finger kam man dort nicht heraus. Am Ende angekommen oder nach einer eigenen Nachricht läuft sie wieder mit.

## Zurück in der App (seit 0.97)

Rückmeldung des Inhabers vom 16.11.2026: Der Agent vom Homescreen zeigte nach dem Wiederöffnen den alten Stand. iOS hält eine Homescreen-App im Speicher und bringt die Seite zurück, wie sie war; von selbst lädt nichts, und die Verbindung zu einem laufenden Auftrag ist weg. War die Seite länger als 20 Sekunden nicht sichtbar (`visibilitychange`, oder `pageshow` aus dem Speicher des Browsers), liest die Agent-Seite Zustand, Chat-Liste und den offenen Chat neu und hängt sich wieder an einen laufenden Auftrag. Gibt es eine neue Version der Oberfläche (`updated.check()` gegen `/_app/version.json`), lädt sie die Seite ganz neu – nicht, solange etwas im Eingabefeld steht, Dateien angehängt sind oder ein Dialog offen ist. Das Symbol des Agenten liegt seit 0.97 unter `/icons/agent2-*.png`, weil iOS das Bild einer Adresse behält.

## Anbieter des Modells (seit 0.96)

Wunsch des Inhabers vom 16.11.2026 (nicht jedes NAS hat eine Grafikkarte). Unter Einstellungen › KI-Agent › Verbindung und Modelle › „Wo läuft das Modell?“ stehen drei Wege; ab Werk Ollama:

- **Ollama** (bei mir, im Heimnetz) – wie bisher.
- **OpenAI-kompatibler Dienst** – Adresse bis `/v1` und API-Schlüssel (OpenAI, OpenRouter, Groq, Mistral, LM Studio …). `agent/remote.go`: `/models` und `/chat/completions` mit Streaming und Werkzeugaufrufen. Eine Adresse außerhalb des Heimnetzes muss `https` sein; ein Server im Heimnetz darf `http` sein, braucht keinen Schlüssel und gilt als lokal.
- **Claude** – nur der API-Schlüssel. `agent/claude.go` nutzt das offizielle Go-SDK (`anthropic-sdk-go/v2`), ohne Zugangsdaten aus der Umgebung (`WithoutEnvironmentDefaults`), mit Streaming, bis 64 000 Token je Antwort und gecachten Anweisungen. Seit 1.01 richtet sich die Länge nach dem Modell: Seine Grenze (`max_tokens` aus der Modell-Liste des Anbieters) wird einmal gelesen und gemerkt; kann ein Modell weniger, bekommt es seine Grenze. Ist die Grenze nicht lesbar und lehnt der Anbieter 64 000 ab, wird die Anfrage einmal mit 4 096 wiederholt (so viel kann jedes Claude-Modell). Denkblöcke werden nicht in den Verlauf zurückgespielt (der Verlauf des Agenten wird je Runde neu gebaut; alle wegzulassen ist erlaubt). Lehnt der Anbieter eine Anfrage ab (`stop_reason: refusal`), steht das als Fehler im Chat; ein Ausweichmodell ist nicht eingerichtet.

**Was das heißt:** Mit einem Anbieter im Internet verlässt alles, was der Agent liest – Fragen, Dateiinhalte, Gerätenamen, Protokolle, Gedächtnis und NAS-Übersicht im Prompt –, das NAS; das gilt auch für geplante Aufträge, die Reaktion auf Meldungen und das Nachdenken über Chats, weil alle denselben Anbieter nutzen. Die Einstellungen warnen beim Auswählen, der Chat zeigt „Cloud“ neben dem Modell (`status.cloud`). Die Rechte des Agenten gelten unverändert.

**Schlüssel:** bleibt in `agent.json` auf dem Stick, geht nie an die Seite (`keySet`), ein leerer im Formular behält den gespeicherten – aber nur für denselben Anbieter und dieselbe Adresse; bei geänderter Adresse muss er neu eingetragen werden, damit ein gespeicherter Schlüssel nicht an eine andere Adresse geht. Dieselbe Regel gilt seit 0.96 für die Token der MCP-Verbindungen. Fehlermeldungen des Anbieters werden gekürzt gezeigt, der Schlüssel steht in keiner.

**Nur mit Ollama:** Modelle laden, Kontextlänge und „im Speicher halten“ einstellen, Bilder mit einem eigenen Bild-Modell ansehen (`ErrOllamaOnly`). Die Kontextlänge kommt beim Anbieter aus dessen Modellliste, sonst gilt die vorsichtige Schätzung.

## Schnellbefehle (seit 0.95)

Wunsch des Inhabers vom 15.11.2026: Schalten soll sofort reagieren. Eine Nachricht, die in einem kurzen Satz genau ein Gerät und eine Handlung nennt, führt TitanOS selbst aus, ohne das Modell zu fragen (`agent.ParseQuick`, `api/agent_quick.go`):

- **Schalten:** „Stehlampe an“, „Schalte das Licht im Wohnzimmer aus“, „Stehlampe auf 40 Prozent“, „Öffne die Markise“, „Fahre den Rollladen im Büro runter“ (auch auf Englisch). Erlaubte Gerätearten: `light`, `switch`, `fan`, `input_boolean`, `media_player`, `cover`, dazu Szenen („Aktiviere Filmabend“, `scene.turn_on`) und die Solltemperatur eines Raums („Wohnzimmer auf 21,5 Grad“, `climate.set_temperature`, 5–30 °C). Zwei oder drei Befehle mit „und“ laufen nacheinander (`ParseQuickAll`); ist ein Teil keiner, geht der ganze Satz an das Modell. **Nie:** Schlösser, Alarmanlagen, Skripte.
- **Zustand:** „Ist der Fernseher an?“ – liest das eine Gerät und sagt den Zustand in Worten; ein Zustand ohne festes Wort geht an das Modell.
- **Erinnern:** „Timer 10 Minuten“, „Erinnere mich in 20 Minuten an die Pizza“ – legt eine einmalige Erinnerung an (`schedule_create`).
- **Welches Gerät:** der gesagte Name muss genau dem Namen oder einem Alias eines freigegebenen Geräts entsprechen (Wörter in beliebiger Reihenfolge, zusammen oder getrennt geschrieben, wahlweise mit dem Raum davor); findet sich so keins, darf ein Buchstabe abweichen (zwei ab elf Buchstaben), Zahlen nie, und nur ein Gerät darf so nah kommen. „Das Licht im Wohnzimmer“ meint alle Lichter dieses Raums. Kein Treffer, zwei Geräte gleichen Namens oder ein nur lesbares Gerät: das Modell übernimmt.
- **Rechte:** Der Schritt läuft über dieselben Werkzeuge wie beim Modell (`ha_entities`, `ha_call`, `ha_state`), also mit den freigegebenen Geräten, den Regeln je Geräteart und den Rechten des Kontos. Würde der Schritt nachfragen, passiert hier nichts: das Modell bekommt die Nachricht und die Person wird wie immer gefragt (`Runner.Direct`). Schlägt der Schritt fehl, bleibt er als fehlgeschlagener Schritt im Chat und das Modell macht weiter.
- **Wann nicht:** bei „Plan zeigen“ und „Jeden Schritt fragen“, bei mehr als einem Satz, bei Fragen aus Home Assistant (Assist) und in geplanten Aufträgen. Abschaltbar unter Einstellungen › KI-Agent (`quickOff`).
- Im Chat steht der Schritt wie jeder andere („Zusammenfassung“), die Antwort ist ein fester Satz („Stehlampe ist an.“).

## Sprechmodus (seit 0.95)

Ein Gespräch ohne Tippen (`VoiceMode.svelte`), zu öffnen über „＋“ › „Sprechmodus“, wo Spracheingabe eingerichtet ist. Das Mikrofon bleibt offen; gesprochen wird erkannt an der Lautstärke über dem Grundrauschen des Raums, nach etwa 1,3 Sekunden Pause (höchstens 30 Sekunden) geht die Aufnahme an die Spracherkennung (Sprachdienste des NAS oder die eingetragene Whisper-Adresse) und der Text als Nachricht in den Chat. Die Antwort wird vorgelesen (Stimme des NAS oder des Geräts), danach hört der Agent wieder zu. Die Figur in der Mitte (`VoiceOrb.svelte`, Canvas, Wunsch des Inhabers vom 15.11.2026 nach etwas Außergewöhnlichem): ein leuchtender Kern, eine Skala aus Strichen, drei Ringe aus Bögen, die sich gegeneinander drehen, und ein Kranz aus 96 Strichen – beim Zuhören folgt der Kranz dem Spektrum des Mikrofons, beim Nachdenken rasen die Ringe und drei Funken umkreisen den Kern, beim Vorlesen rollt der Kranz, in der Pause wird alles dunkler; bei „Animationen: Aus“ und „Weniger Bewegung“ ein stehendes Bild je Zustand. Tippen auf die Figur: beim Zuhören Pause, beim Sprechen sofort senden, beim Vorlesen abbrechen und zuhören. Wartet ein Schritt auf Bestätigung, hält der Modus an und verweist auf den Chat. **Ins Wort fallen:** deutlich lautes Sprechen über eine Viertelsekunde beendet das Vorlesen und beginnt die Aufnahme. **Weckwort (wahlweise, je Gerät in `localStorage`):** eingeschaltet geht nur in den Chat, was in den ersten Wörtern das Weckwort hat (ab Werk „Titan“, ein Buchstabe Abweichung erlaubt); geprüft wird am erkannten Text, also geht weiterhin alles Gesprochene zur Spracherkennung. Nach einer Antwort oder dem Wort allein gilt neun Sekunden lang auch ein Satz ohne Wort. Der Modus wird von Hand geöffnet und beendet; solange er offen ist, hört das Mikrofon zu. Aufnahmen werden nicht gespeichert.

## Karten unter der Antwort (seit 0.95)

`ResultCards.svelte` zeigt unter der letzten Antwort eines Durchgangs, was dessen Schritte gelesen haben: nach `docker_list` die Container (Zustand in Wort und Punkt, Speicher, CPU, „Update verfügbar“; seit 0.97 ist jede Zeile eine Zeile, ein Tippen klappt ihre Aktionen auf: laufend „Neustart“, „Stoppen“, „Protokoll“, „Details“, „Fehler suchen“, gestoppt „Starten“, „Protokoll“, „Details“, „Warum gestoppt?“, mit Update zuerst „Aktualisieren“), nach `ha_state` das Gerät mit „Einschalten“/„Ausschalten“. Die Daten sind das Ergebnis des Schritts selbst; ein nicht lesbares (gekürztes) Ergebnis gibt keine Karte. Die Knöpfe schreiben den Auftrag als Nachricht in den Chat (nur unter der neuesten Antwort) – Rechte und Nachfragen gelten wie beim Tippen. Der Prompt sagt dem Modell, die Liste nicht als Tabelle zu wiederholen. Die Karte einer geschriebenen Datei hat „In <App> öffnen“, wenn für den Dateityp eine Web-App eingestellt ist (`appForFile`, `openInWebApp`).

## Verbindungen zu anderen Programmen (MCP, seit 0.92)

Der Agent kann die Werkzeuge fremder **MCP-Server** mitbenutzen, zum Beispiel n8n (Wunsch des Inhabers vom 15.11.2026: der Agent soll sich mit anderen Programmen und Agenten verbinden können). TitanOS war bisher nur selbst ein MCP-Server (`docs/MCP.md`); das hier ist die andere Richtung.

- **Einrichten:** Einstellungen › KI-Agent › Verbindung und Modelle › „Verbindungen zu anderen Programmen (MCP)“: Name, Adresse des Servers (Streamable HTTP, z. B. `https://n8n.example/mcp`), bei Bedarf ein Token (`Authorization: Bearer`), Ein/Aus. „Verbindung prüfen“ listet die Werkzeuge des Servers. Höchstens 8 Verbindungen. Nur Besitzer und Administratoren.
- **Token:** steht in `agent.json` und geht nie an die Seite zurück (`tokenSet` sagt, ob einer da ist); ein leeres Feld beim Speichern behält ihn, „Token entfernen“ löscht ihn.
- **Werkzeuge:** heißen im Agenten `mcp_<verbindung>_<werkzeug>` (höchstens 40 je Verbindung), Bereich **„Verbindungen (MCP)“** in den Rechten. Die Liste eines Servers wird fünf Minuten gemerkt; antwortet er nicht, fehlen seine Werkzeuge und der Chat läuft ohne sie weiter. Seit 1.01 bremst ein Server, der nicht antwortet (bis 12 s), nur die eigene Verbindung: Andere Verbindungen und Chats warten nicht auf ihn; ein zweiter Chat bekommt währenddessen die zuletzt gemerkte Liste oder wartet auf dieselbe Antwort, statt ein zweites Mal zu fragen.
- **Vorsicht eingebaut:** Jeder Aufruf gilt als Änderung, weil TitanOS nicht wissen kann, was ein fremdes Werkzeug tut – unter „Standard“ fragt er nach, ohne Nachfrage läuft er nur mit „Alles ohne Nachfrage“ oder dem Bereich auf „Ohne Nachfrage“. Beschreibungen fremder Werkzeuge werden auf 400 Zeichen gekürzt und dem Modell als Angabe des Servers gezeigt, nicht als Anweisung. Ein Ergebnis gilt als fremder Inhalt: Danach fragt die Websuche nach, wie nach einer gelesenen Webseite. Ergebnisse werden auf 24 000 Zeichen gekürzt, Bilder weggelassen.
- **Nicht dabei:** Konten ohne Verwaltungsrechte und das Profil „Home Assistant“ bekommen die Werkzeuge nicht (die Verbindung arbeitet mit dem Token des Besitzers). Nur HTTP-Server; lokale Programme über stdio und die Anmeldung per OAuth gehen nicht.
- Code: `internal/mcpclient` (Client), `api/agent_mcp.go`; `POST /api/v1/agent/mcp/check`.

## Ordner „KI-Agent“ (PLAN 49)

Ist der Agent eingeschaltet, legt TitanOS auf seinem Volume die Freigabe **„KI-Agent“** an (wie `appdata` bei Docker). Jedes Konto, das den Agenten nutzen darf, bekommt darin einen eigenen Ordner, den **nur dieses Konto** sieht: im Netz (SMB, Pfad `…/KI-Agent/%u`) und im Dateimanager (Wurzel `share/KI-Agent`). NFS und Gastzugang gibt es für diese Freigabe nicht; die Konten hält TitanOS selbst aktuell (Feld `perUser` der Freigabe, nur von titand gesetzt). Wer als Administrator ganze Volumes öffnen darf, sieht dort wie überall alle Ordner.

| Ordner | Inhalt |
|---|---|
| `Chats/`, `Projekte/<Name>/` | jeder Chat als Markdown, nach jeder Änderung neu geschrieben; Projekte mit `Anweisungen.md` |
| `Dokumente/` | was der Agent erstellt, wenn nichts anderes gesagt wurde |
| `Wissen/TitanOS/` | mitgelieferte Anleitungen (Docker, Home Assistant, Backups, Fehlersuche, Ordnung und Dokumente, VMs und LXC, Netzwerk und Firewall, Konten/Updates/Plugins, Cluster, Planen); bei jedem Update ersetzt |
| `Skills/TitanOS/`, `Skills/Eigene/` | mitgelieferte und eigene Skills als Lesekopie |
| `Notizen/` | dein Wissen für den Agenten; er bekommt die Dateinamen in jedem Auftrag und liest passende |

- Der maßgebliche Stand (Chats mit Freigaben, Rechte, Gedächtnis) bleibt privat unter `.titanos/agent/`. Die Dateien in der Freigabe sind Abbilder.
- **Bearbeiten wirkt** (Entscheidung des Inhabers vom 5.11.2026): Änderst du eine Chat-Datei, liest der Agent deinen Text vor dem nächsten Auftrag in diesem Chat als Inhalt (Eintrag „In der Datei geändert“). Freigaben, Werkzeugaufrufe und Rechte kommen nie aus der Datei. Der Chat gilt danach als „mit fremdem Inhalt“ (Websuche fragt nach).
- titand schreibt dort als root, aber nur über `os.Root`: ein Link, den jemand in seinen Ordner legt, führt nicht hinaus. Dateien gehören dem Konto (Gruppe `users`).
- Geschrieben wird nur, wenn sich ein Chat geändert hat (letzter Stand aus dem RAM, kein minütliches Lesen der Platten).

## Geplante Aufträge, Erinnerungen, Morgenbericht (PLAN 49)

- Im Chat in normaler Sprache: „Erinnere mich morgen um 9 an den Arzt“, „in 2 Stunden“, „jeden Montag um 8 Gesundheitscheck“, „werktags um 7“. Der Agent bestätigt mit einer Karte (Art, Zeit, nächstes Mal).
- **Arten:** Auftrag (der Agent arbeitet ihn zur Zeit ab, eigener Chat, Ergebnis als Mitteilung) oder **Erinnerung** (nur Mitteilung per Push und Chat-Eintrag „⏰ Erinnerung“, kein Modell-Lauf; kommt immer, auch wenn ein Chat offen ist).
- **Zeiten:** einmalig (Datum und Uhrzeit oder „in N Minuten“), alle N Minuten (ab 5), Stunden (1–168) oder Tage (1–365), stündlich, täglich, an Wochentagen, wöchentlich, monatlich (Tag 1–28), freie Cron-Zeile (fünf Felder) oder Zustand in Home Assistant. Zeitzone des NAS. „Stündlich“ heißt zur vollen Stunde dieser Zeitzone, seit 1.01 auch in Zeitzonen mit halber oder Dreiviertelstunde (Indien, Nepal, Neufundland; vorher lief es dort zu :30 bzw. :45). Eine Stunde, die die Sommerzeit überspringt, entfällt; eine, die sie doppelt bringt, läuft zweimal im Abstand einer Stunde.
- **Verpasst** (NAS aus, titand neu gestartet): wiederkehrende Aufträge laufen nach dem Start einmal nach, wenn der Termin höchstens 12 Stunden her ist, einmalige bis 7 Tage; der Chat heißt dann „… (nachgeholt)“.
- **Seite „Geplant“** im Agent-Menü (`/agent?page=planned`): nächster Lauf, Pausieren, Ändern, Jetzt ausführen, Löschen, letztes Ergebnis; Formular mit allen Arten.
- **Morgenbericht** (ab Werk aus, je Konto): oben in „Geplant“ als eine Zeile mit Schalter und Zusammenfassung (seit 0.92; der Pfeil klappt die Einstellungen auf) mit Uhrzeit, Wochentagen und Inhalt per Häkchen (NAS-Zustand und Speicher, Backups und Updates, Erinnerungen und Aufträge des Tages, Home Assistant) und eigenem Wunschtext. Er liest nur und ändert nichts. Der Agent legt ihn nicht im Chat an.
- Ältere Versionen ignorieren die neuen Arten (Feld `every` mit unbekanntem Wert); beim Rückweg laufen sie nicht.
- **Schlafende Platten je Auftrag** (seit 0.69, PLAN 53/72):
  - Jeder Auftrag hat die Wahl „Wie unter Eigeninitiative“ (ab Werk), „Wecken“, „Vorher fragen“ oder „Nie wecken“ (Feld `wake` des Auftrags).
  - Der Chat eines Laufs merkt sich seinen Auftrag (`schedule`). Die Dateiwerkzeuge nehmen dann die Wahl des Auftrags statt der des Kontos.
  - „Vorher fragen“ hält den Lauf an, bis die Person zustimmt.
  - Hintergrund (Wache, Nachdenken, Morgenbericht) weckt weiter nie.
  - Erinnerungen haben die Wahl nicht.
  - Ältere Versionen ignorieren das Feld; es gilt dann die Wahl des Kontos.

## Sprache, Bilder, Vorlesen (PLAN 49/51)

- **Spracheingabe (Whisper):** Einstellungen › KI-Agent › Sprache, Bilder und Sicherheit, ab Werk aus. Adresse eines Whisper-Dienstes mit OpenAI-Schnittstelle (`/v1/audio/transcriptions`), am besten auf dem Ollama-Rechner mit Grafikkarte, und Modellname; „Verbindung prüfen“ liest `/v1/models`. Dann zeigt das Eingabefeld einen Mikrofon-Knopf: Aufnahme im Browser, titand schickt sie an Whisper (`POST /api/v1/agent/transcribe`, höchstens 10 MB, nicht gespeichert), der Text kommt ins Feld. Beispiel für den Ollama-Rechner (Speaches mit CUDA):

  ```yaml
  services:
    whisper:
      image: ghcr.io/speaches-ai/speaches:latest-cuda
      ports: ["8000:8000"]
      environment:
        - WHISPER__TTL=-1
      volumes: ["./hf-cache:/home/ubuntu/.cache/huggingface/hub"]
      deploy:
        resources:
          reservations:
            devices: [{ driver: nvidia, count: all, capabilities: [gpu] }]
  ```

  Modell z. B. `Systran/faster-whisper-large-v3` oder eine turbo-Variante (ca. 1,5–2 GB Grafikspeicher). Image-Name und Variablen vor dem Einsatz in der Doku von Speaches nachsehen.
- **Bilder verstehen:** Sieht das Hauptmodell Bilder (Fähigkeit `vision`, z. B. `gemma4:12b`), bekommt es sie direkt; sonst optional ein eigenes Bild-Modell. Werkzeug `image_view`: Bild aus dem Chat (Büroklammer › „Foto oder Kamera“) oder vom NAS, mit Frage; die Antwort des Modells gilt als fremder Inhalt.
- **Vorlesen:** Lautsprecher-Knopf an jeder Antwort, wahlweise automatisch (Einstellungen › Sprache › automatisch vorlesen); Sprachausgabe des Geräts, ohne Server.
- Messung des Inhabers (5.11.2026): `gemma4:12b` mit 256K Kontext belegt 12,6 GB von 16 GB (RTX 5060 Ti); Whisper turbo passt daneben.

## Agent-App: Aussehen, Gesten, Einstellungen (PLAN 53)

Von 0.54.0 bis 0.63.0 hatte der Agent einen eigenen Auftritt (Polarlicht hinter Glas, leuchtende Kugel, eigene Akzente). Seit 0.64.0 folgt er der Designsprache von TitanOS (PLAN 68, siehe „Aussehen“ unter „Bedienung“).

**Aussehen**
- Agent-Symbol und Begrüßung mit Namen.
- Projekte als kleine Chips statt der großen Kachel.
- „Passt gerade“: höchstens drei Vorschläge.
  - Zuerst Warnungen des NAS der letzten 48 Stunden (`GET /api/v1/agent/suggestions`, nur Besitzer und Administratoren, aus den Meldungen im RAM).
  - Dann nach Tageszeit: morgens „Was ist über Nacht passiert?“, abends „Ist zu Hause alles zu?“.
  - Dann wechselnd der Rest; „andere“ zeigt die nächsten.

**Einstellungen im Agenten** (unten in der Seitenleiste und im ⋯-Menü der Kopfzeile, für jedes Konto)
- Sprache:
  - automatisch vorlesen an oder aus; aus heißt Knopf „Vorlesen“ an jeder Antwort;
  - Tempo;
  - Spracheingabe per Antippen oder Gedrückthalten;
  - Stand von Whisper.
- Modelle.
- Geplant, Gedächtnis, Skills, Meine Anweisung, NAS-Landkarte, Protokoll: Jeder Teil öffnet sich einzeln, ohne Sammelfenster.
- Darstellung und Gesten:
  - Wischgesten an oder aus;
  - was Wischen nach rechts oder links an einem Chat tut: Anheften, Archivieren, Löschen, Menü, Nichts;
  - vom linken Rand wischen: Chats (die Einstellungen haben einen Weg, das Menü; das Wischen vom rechten Rand dorthin entfiel mit 0.85);
  - Ziehen zum Aktualisieren;
  - Vibration;
  - Animationen voll, reduziert oder aus; `prefers-reduced-motion` gilt immer;
  - Schriftgröße und kompakte Chatliste (Akzent, Hintergrund, Glas, Leuchten und Stil der Antworten sind seit 0.64.0 entfernt, siehe „Aussehen“);
  - beim Öffnen: neuer Chat, letzter Chat oder Übersicht;
  - Vorschläge;
  - Denkschritte und Werkzeuge zeigen;
  - Tipp-Effekt;
  - Bildschirm wach halten, solange der Agent arbeitet.
- Für Besitzer ein Link zu Verbindung und Rechten unter TitanOS › Einstellungen › KI-Agent.

**„Geplant“ im Menü** öffnet nur Aufträge und Erinnerungen. Eine Erinnerung, die noch nie lief, zeigt „noch nicht gelaufen“.

**Menü eines Chats**
- Erreichbar über Wischen mit „Menü“, langes Drücken oder Rechtsklick.
- Anheften, Umbenennen, In Projekt verschieben, Archivieren (bzw. „Aus dem Archiv holen“), Markdown und PDF herunterladen, Löschen (mit einfacher Rückfrage).

**Archiv** (seit 0.69, PLAN 53/72)
- Archivierte Chats verschwinden aus der Liste, aus der Suche des Menüs und aus der Startseite. Der Eintrag „Archiv“ in der Seitenleiste erscheint, sobald es archivierte Chats gibt, und zeigt ihre Zahl.
- Das Archiv listet sie mit „Öffnen“ und „Aus dem Archiv holen“.
- Technik:
  - Feld `archived` des Chats (`PATCH /api/v1/agent/chats/{id}` mit `{"archived": true|false}`).
  - Archivierte Chats werden beim Kürzen auf 2 000 Chats behalten wie angeheftete.
  - Ältere Versionen zeigen sie wieder in der Liste.

**Speicherort**
- Die Einstellungen liegen je Konto auf dem NAS in `.titanos/agent/<konto>/prefs.json` (`GET`/`PUT /api/v1/agent/prefs`).
  - Flaches Objekt mit kurzen Werten, höchstens 8 KiB, eine Ebene Unterobjekte. Die Oberfläche prüft die Felder.
- Nur Vibration, Bildschirm wach halten und Schriftgröße gehören dem Gerät (`localStorage`).
- Ältere Versionen ignorieren die Datei.

## Eigeninitiative, Vorschläge, Gedächtnis und Persönlichkeit (PLAN 53)

Jedes Konto stellt in der Agent-App unter Einstellungen › Eigeninitiative und › Gedächtnis, Skills, Persönlichkeit selbst ein, wie viel der Agent von sich aus tut. Gespeichert wird in `.titanos/agent/<konto>/initiative.json` (`GET`/`PUT /api/v1/agent/initiative`).

**Wie selbstständig**
- Still: nichts von selbst.
- Hinweise oder Vorschläge (Standard): Funde landen auf der Startseite unter „Wartet auf dich“ (früher eigener Punkt „Vorschläge“; `inbox.json`, `GET /api/v1/agent/inbox`, `POST /api/v1/agent/inbox/{id}` mit `accept`).
- Handelt selbst: Bei Meldungen sieht der Agent selbst nach, wie bei der bisherigen Eigeninitiative des Besitzers.

**Nachts nachdenken**
- Zur gewählten Zeit (Standard 03:30) liest der Agent die Chats des Tages noch einmal: nur Gesagtes, keine Werkzeugergebnisse, höchstens 10 Chats.
- Er zieht bis zu 5 Fakten, Vorlieben und offene Punkte heraus.
- Gefiltert wird:
  - Geheimnisse;
  - Systemtexte und Werkzeug-JSON (der Fehler, den OpenClaw-Nutzer beim „Dreaming“ sahen);
  - Doppeltes.
- „Erst bestätigen“ (Standard) legt die Funde auf der Startseite unter „Wartet auf dich“ ab, „Einfach machen“ übernimmt sie direkt. Funde aus Chats mit fremden Inhalten sind markiert.
- Seit 0.75: Steht ein markiertes Gemerktes (aus einem Chat mit fremden Inhalten) im Systemprompt, fragen Web-Werkzeuge in diesem Chat nach, wie bei einem Skill von außen (`chat.OutsideSkill`, `TestUntrustedMemoryMakesWebToolsAsk`). Beschreibungen, die der Agent in einem solchen Chat gespeichert hat, stehen in keinem Systemprompt; unter „Karte“ bleiben sie sichtbar. Ordnerregeln gelten für den echten Ordner, auch hinter einem Link (`TestAgentFolderRuleFollowsLinks`). Die Freigabekarte von `code_run` zeigt das ganze Programm, nicht nur den Anfang.
- Seit 0.98 (Rückmeldung des Inhabers vom 16.11.2026: der Schalter war an, der Agent fragte trotzdem vor jeder Websuche): Mit „Internet auch nach gelesenen Inhalten ohne Nachfrage“ (`Policy.WebFree`) gilt für Web-Werkzeuge immer, was unter „Internet“ eingestellt ist – auch mit einem Skill von außen oder einem markierten Gemerkten im Systemprompt (`webStaysFree`, `TestAgentWebAfterReading`). Vorher blieb in diesem Fall die Nachfrage, und ein einziges Gemerktes aus einem Chat mit Websuche ließ danach jeden Chat nachfragen. Ohne den Schalter (ab Werk) bleibt es dabei.
- Auf Wunsch erwähnt der Morgenbericht in einem Satz, was neu gemerkt wurde.
- Wann der Lauf startet:
  - Er wartet, solange irgendein Chat läuft.
  - Er startet nicht, wenn eine Platte des Agent-Volumes schläft; dann holt er den Lauf im Laufe des Tages nach.
  - Er endet nach den eingestellten Minuten der Grafikkarte.

**Wache und Ereignisse**
- Warnungen des NAS werden für Besitzer und Administratoren mit eingeschalteter Wache zu Vorschlägen. Mit „Handelt selbst“ starten sie einen eigenen Chat.
- Ereignisse (USB-Platte eingesteckt `usb_plugged`, Update verfügbar `update_available`/`docker_auto_updated`) kommen auch als Info-Meldung an. Der Beobachter von `notify` erhält dafür jetzt alle Stufen; die bisherige Eigeninitiative reagiert weiter nur auf Warnungen. Abstürze von Apps meldet TitanOS bisher nicht als eigene Meldung, deshalb gibt es dafür keinen Schalter.
- Zustände in Home Assistant bleiben Auslöser geplanter Aufträge.

**Vorausschau**
- Einmal am Tag ab 04:00 nimmt der Agent die Belegung der Volumes. Die Werte kommen per statfs aus dem Kernel; keine Platte wird geweckt. Sie bleiben im RAM, höchstens 60 Tage.
- Ab 7 Tagen und einer Füllung in höchstens 60 Tagen erscheint „<Volume> ist in etwa N Tagen voll“.

**Skills lernen**
- Läuft derselbe Ablauf aus mindestens drei Werkzeugen zweimal, schlägt der Agent ihn als Skill vor.
- Es gibt 28 fertige Skills für alle Bereiche. Ein Test prüft, dass sie nur vorhandene Werkzeuge nennen; dabei fiel das nicht vorhandene `my_shares` auf.

**Gedächtnis**
- „Selbst merken“: Mit an merkt sich der Agent Wichtiges von selbst und immer bei „merk dir“; mit aus nur bei „merk dir“.
- Sagt eine neue Erinnerung etwas anderes über dasselbe als eine alte, fragt der Agent auf der Startseite unter „Wartet auf dich“ nach.
- Erinnerungen, die älter als die gewählten Monate sind (1–24), kommen zur Frage „Noch aktuell?“. Der Regler ganz rechts heißt „unbegrenzt“: Der Agent fragt nie nach, nichts verblasst.
- „Gedächtnis leeren“ (`DELETE /api/v1/agent/memory`) mit einfacher Rückfrage.

**Persönlichkeit**
- Antwortlänge, Ton, Name und Symbol. Der Agent bekommt sie als Anweisung des Kontos.

**Schlafende Platten**
- Hintergrundarbeit weckt nie eine Platte: Wache, Nacht, Vorausschau und Chats aus Meldungen.
- Bei eigenen Aufträgen gilt „Wecken“, „Erst fragen“ (Freigabe im Chat) oder „Nie“.
- Geprüft wird aus dem, was titand ohnehin weiß (lsblk und die eigene Ruheliste); die Platte wird dafür nicht gelesen.

**Suchorte**
- Sind welche gewählt, sucht `files_search` nur dort. Andere Freigaben durchsucht er nur, wenn die letzte Nachricht sie nennt („such auch im Backup“).
- Rechte und Ordner-Regeln gehen vor.

**Kennenlernen**
- Startet einen Chat, in dem der Agent die NAS-Landkarte ansieht, nach dem Wichtigsten fragt und drei Aufträge vorschlägt, die er erst nach Zustimmung anlegt.
- Ohne Chats steht es als erster Vorschlag auf der Startseite.

**Wochenrückblick:** ein normaler geplanter Auftrag (sonntags 18:00), den man verschieben kann. Er erscheint, sobald man die Eigeninitiative zum ersten Mal ansieht.

**Ruhezeit:** In der Ruhezeit startet der Agent bei Meldungen keinen eigenen Chat, auch nicht mit „Handelt selbst“; die Meldung wird nur zum Vorschlag.

**Ohne Plattenzugriff im Hintergrund:**
- Die Einstellungen jedes Kontos liegen nach dem ersten Lesen im RAM.
- Der Minutentakt und Meldungen lesen sie nur, solange das Agent-Volume ohnehin wach ist.
- Vorschläge, die entstehen, während es schläft, warten im RAM und werden beim nächsten Öffnen der Startseite oder beim nächtlichen Lauf geschrieben.
- Die Werte der Vorausschau liegen im Ordner des Besitzers (`forecast.json`) und werden nur bei wachem Volume gelesen und geschrieben.
- Ein unlesbares `initiative.json` oder `inbox.json` wird nie überschrieben: Die Seite meldet 503, Hintergrundarbeit lässt das Konto aus.

## Snapshot vor Änderungen (PLAN 51)

Bevor der Agent eine App aktualisiert oder entfernt (`app_action`) oder Dateien verschiebt, umbenennt oder ersetzt (Gelöschtes landet im Papierkorb der Freigabe), legt TitanOS einen schreibgeschützten Btrfs-Snapshot `agent-JJJJMMTT-hhmmss` des betroffenen Volumes an (höchstens einer je Volume in 10 Minuten, 7 Tage aufbewahrt, nächtlich aufgeräumt). Der Chat zeigt ihn als Eintrag 📸. Einzelne Dateien holt man auf der Speicher-Seite unter Snapshots zurück (Wiederherstellen legt eine beschreibbare Kopie an; das Volume selbst wird nie zurückgesetzt). Volumes ohne Btrfs bekommen keinen, der Chat sagt das. Abschaltbar (`noSnapshots`). VM- und LXC-Snapshots legt der Agent nicht selbst an.

## Geteilte Projekte (PLAN 51)

Im Projekt-Dialog wählt der Besitzer eines Projekts andere Konten des Agenten als Mitglieder. Sie sehen das Projekt (mit dem Namen des Besitzers), seine Anweisungen und alle Chats darin und legen eigene Chats an. Jeder Chat bleibt bei dem, der ihn schreibt: andere lesen ihn nur (kein Weiterschreiben, keine Freigaben), und jeder Lauf hat die Rechte seines Autors. Von fremden Chats sehen Mitglieder **nur Fragen und Antworten**, nicht die Schritte: Werkzeug-Ergebnisse, Argumente, Gedanken, Dateien und wartende Freigaben stammen aus den Rechten des Autors und bleiben bei ihm (`foreignChat`). Für Mitglieder heißt das Projekt `<besitzer>:<id>`; nur der Besitzer ändert oder löscht es.

- **Eigeninitiative** (Einstellungen › KI-Agent): Bei Warnungen und Fehlern des NAS sieht der Agent nach und behebt im Rahmen seiner Rechte, je Art höchstens einmal pro Stunde, als Chat „Von selbst: …“.

## Home Assistant

Der Agent nutzt die Verbindung unter Einstellungen › Home Assistant:

- Geräte lesen und schalten, Verläufe, Diagnose (Werkzeuge des MCP-Katalogs). **Eigene Freigabeliste** (Entscheidung des Projektinhabers vom 5.11.2026): Unter Einstellungen › KI-Agent › Home Assistant › „Geräte für den Agenten“ gibst du ganze Bereiche (z. B. `sensor`) oder einzelne Geräte frei – getrennt von „Geräte für MCP freigeben“. Ab Werk ist nichts freigegeben; die Seite sagt das ausdrücklich, und der Agent bekommt bei leerer Liste den Hinweis, wo man freigibt. Seit 0.87 (Entscheidung vom 15.11.2026) wählst du bei „Der Agent sieht“ zwischen **Nichts, Auswahl und Alles** (`release.all`: jedes Gerät der Verbindung, auch später hinzugefügte); die Listen einer Auswahl laden von selbst. Ein einzeln freigegebenes Gerät kann der Agent lesen, gleich aus welcher Domäne; schalten kann er weiterhin nur Domänen, für die TitanOS Dienste kennt. Darüber stehen zwei Schalter „Geräte ansehen“ und „Geräte schalten“ (Ohne Nachfrage / Nachfragen / Verboten) – dieselben Werkzeugrechte wie unter Rechte › Home Assistant. Die Nur-Lesen-/Dienste-Regeln von MCP gelten für den Agenten nicht; dafür gibt es seine Geräte-Regeln.
- `ha_statistics`: Langzeit-Statistik (z. B. Stromverbrauch pro Tag der letzten Woche).
- `ha_automation_save`, `ha_script_save`: Automationen und Skripte anlegen und ändern, gekennzeichnet „vom TitanOS-Agent“; die vorherige Fassung steht im Ergebnis des Schritts.
- **Gerätearten und Ausnahmen** (`domain:`, `entity:`, `area:` für Räume, z. B. `area:kueche`; Entscheidung des Projektinhabers vom 15.11.2026, seit 0.88): Unter „Gerätearten“ hat jede Domäne eine Zeile mit **Ansehen** (Freigabeliste) und **Schalten: Ja / Nachfragen / Nein** (Regel `domain:<d>`); `entity:` und `area:` sind Ausnahmen darunter. Die Regeln betreffen das **Schalten**: Lesen fragt ihretwegen nie nach, nur „Verboten“ sperrt ein Gerät auch fürs Lesen. Beim Schalten entscheidet die Regel des Geräts (Gerät vor Raum vor Geräteart) – „Ohne Nachfrage“ schaltet auch dort ohne Nachfrage, wo der Bereich nachfragt; ein unter Rechte verbotener Bereich bleibt verboten. Ohne Regel gilt „Geräte schalten“; Schlösser und Alarmanlagen fragen dann mindestens nach. Mehrere Geräte in einem Aufruf: das Strengste. Sie gelten für jedes Gerät, das ein Aufruf irgendwo nennt (auch in den Aktionen einer Automation); eine Automation oder ein Skript ohne erkennbare Geräte fragt immer nach.
- **Mitteilungen** über Notify-Dienste der Home-Assistant-App, zusätzlich zu Web Push.
- **Assist (PLAN 91, Entscheidungen des Inhabers vom 14.11.2026, Entwurf `Entwuerfe/91-assist/`):** In Home Assistant die eingebaute Integration **Ollama** hinzufügen: Adresse und Schlüssel aus Einstellungen › KI-Agent › Home Assistant, Modell `titanos-agent`, dann unter Sprachassistenten als Gesprächsagent wählen.
  - **Adressen:** Home Assistant prüft Zertifikate und lehnt das selbst ausgestellte von TitanOS ab („Verbindung nicht möglich“). Deshalb beantwortet titand `/agent-ollama/` zusätzlich auf dem HTTP-Port ohne Umleitung (`http://<NAS>/agent-ollama`, im Heimnetz unverschlüsselt) und über HTTPS unter jedem Namen mit gültigem Zertifikat (Tailscale, Let's Encrypt, dynamisches DNS). Die Karte listet, was eingerichtet ist (`GET /api/v1/agent/assist`).
  - **Schlüssel statt Absenderadresse:** TitanOS erzeugt einen Schlüssel (`POST /api/v1/agent/assist/key`, nur Besitzer und Administratoren; `GET` zeigt ihn), den man in Home Assistant als API-Schlüssel der Integration einträgt (`Authorization: Bearer`). Er steht in `agent.json`, geht nie mit den Einstellungen an die Seite und ändert sich nur über seine Route; ein neuer beendet den alten sofort. Falscher oder fehlender Schlüssel ergibt 401 (Home Assistant zeigt „Ungültige Authentifizierung“). **Ohne Schlüssel** (Einrichtung von vor 0.84) gilt weiter die Adresse der eingetragenen Home-Assistant-Verbindungen, damit ein Update nichts abschaltet.
  - **Eigenes Profil:** Fragen laufen nicht mehr im Chat eines Kontos, sondern im Profil „Home Assistant“ (`@assist`, kein Konto, keine Anmeldung): eigene Chats, eigenes Gedächtnis, eigene Anweisung, eigene geplante Erinnerungen, eigener Ordner in der Freigabe KI-Agent. Aus Gedächtnis, Chats und Notizen einer Person kommt nichts hinein. Besitzer und Administratoren sehen und pflegen es im Agenten unter `/agent?profile=assist` (dieselben Routen mit `?profile=assist`; ein Hinweis in der Seitenleiste führt zurück) und geben dort wartende Schritte frei; wartet einer, geht eine Mitteilung an sie. Werkzeuge laufen mit den Rechten des ersten Besitzers (bzw. des früher gewählten Kontos „Assist als“, solange es verwaltet).
  - **Was Home Assistant fragen darf:** je Bereich einzeln `deny` / `read` / `change` (`homeassistant.assistAreas`): Geräte in Home Assistant, System und Status, Speicher, Sicherungen, Docker und Apps, VMs und LXC, Dateien, Freigaben, Konten, Netzwerk und Firewall, Updates und Neustart, Plugins, Internet, Gedächtnis und Notizen, Planen und Erinnern, Code. Ab Werk: Geräte „Auch ändern“, System/Speicher/Sicherungen/Docker/Gedächtnis „Nur lesen“, Planen erlaubt, alles andere verboten – auch Dateien und Internet. Das gilt zusätzlich zu den allgemeinen Rechten und nie darüber hinaus: was dort verboten ist, bleibt verboten, was dort nachfragt, wartet auf die Freigabe in TitanOS.
  - **Letzte Anfrage:** Zeit, Absender, Frage und Dauer bzw. der Grund einer Ablehnung, nur im RAM (seit dem Start von titand). Einen Verbindungstest von TitanOS aus gibt es nicht – TitanOS kann nicht von Home Assistant aus fragen.

## Messen (PLAN 55)

`TestAgentEval` ist ein fester Satz typischer Aufträge mit Testdaten gegen ein echtes Ollama: Freigaben nennen, Rechnung finden, Datei anlegen, zählen und schreiben, Wort suchen, umbenennen, Ordner sortieren, Beträge zusammenzählen, größte Datei unter 400 Dateien, langer Chat bei 8k Kontext, und seit 0.92 drei mit Dokumenten (neue Excel-Datei, Zelle einer Excel-Datei ändern, Wort in einem Word-Brief ersetzen). Geprüft wird nur, was am Ende dasteht (Antwort oder Datei).

```bash
TITANOS_OLLAMA=http://192.168.10.5:11434 TITANOS_OLLAMA_MODEL=gemma4:12b TITANOS_EVAL_RUNS=2 \
  go test ./internal/api -run TestAgentEval -v -timeout 90m
```

Ergebnisse stehen in den Release-Notizen.

## Funktionen (PLAN 54)

Neun Funktionen in vier Gruppen, jede **ab Werk aus** und einzeln einstellbar: Einstellungen des Agenten › Funktionen bzw. Einstellungen › KI-Agent › Funktionen. Was das ganze NAS betrifft, schalten Besitzer und Administratoren ein (`features` in `agent.json`, `GET`/`PUT /api/v1/agent/features`); was ein Konto für sich einstellt (Ordner, Postfach, Quellen), liegt in `features.json` im privaten Agent-Ordner (`GET`/`PUT /api/v1/agent/features/me`). Eingeschaltete Funktionen erscheinen im Agent-Menü; Mitteilungen öffnen sie mit `/agent?feature=<id>`.

| Gruppe | Funktionen | Einzelheiten |
|---|---|---|
| Dokumente und Fristen | Dokumenten-Gedächtnis (Werkzeug `documents_search`), Fristenwächter (Vorschläge auf der Startseite), Papierkram-Autopilot (Eingang, eigenes IMAP-Postfach, Ablage mit Rückgängig, Ansicht „Papierkram“) | `docs/agent54/docs.md` |
| Schutz | Ransomware-Wächter (fanotify, sperren und melden, Zurückstellen aus Btrfs-Snapshots), Notfallhandbuch (Markdown/PDF), Notfallzugang (Antrag mit Wartezeit, nur lesen, 30 Tage) | `docs/agent54/protect.md` |
| Vorausschau | Was-wäre-wenn („Durchspielen“ auf der Speicher-Seite, Werkzeug `whatif`), Hardware-Berater (SMART-Verlauf, Engpässe, Werkzeug `hardware_advice`) | `docs/agent54/foresight.md` |
| Gemeinschaft und Familie | Backup-Buddy (restic REST auf TCP 8752, nur Tailscale), Skill-Katalog (Ordner `skills/` der App-Store-Quellen) (das Familien-Tagebuch wurde mit 0.69 entfernt) | `docs/agent54/family.md` |

**Ansichten als Seite (PLAN 72, 0.67):** Im Agent-Fenster am Computer öffnen die Ansichten der Funktionen (Papierkram, Fristen, Ransomware-Wächter, Notfallhandbuch, Notfallzugang, Hardware-Berater, Backup-Buddy, Skill-Katalog) als Seite im Hauptbereich rechts der Seitenleiste (`FeatureHost.svelte`, Kontext aus `features/page-mode.ts`), nicht mehr als Blatt über der Leiste. Die Leiste bleibt bedienbar; „Neuer Chat“ und ein Chat schließen die Seite, Escape und „‹“ ebenso. Am Handy und beim Öffnen aus den Einstellungen bleibt es beim Blatt. Prüfung: `scripts/test-ui-agent54-docs.mjs` (Lage neben der Leiste, Schließen über „Neuer Chat“) und die übrigen `test-ui-agent54-*.mjs`.

Der Ransomware-Wächter läuft auch, wenn der Chat des Agenten aus ist; er ergänzt den älteren Wächter je Freigabe (Samba `full_audit`, Freigaben › Ransomware-Wächter), „Wieder öffnen“ hebt beide Sperren auf. Der Backup-Buddy ergänzt „Backups anderer TitanOS“ (TCP 8750) um Kopplung per Code, Kontingent und gegenseitiges Sichern. Hintergrundarbeit weckt keine schlafende Platte und schreibt nicht auf den Stick.

## Daten

| Was | Wo |
|---|---|
| Einstellungen | `/config/system/agent.json` (Stick, nur beim Speichern); auf dem Admin-Knoten auch die Anfragen anderer Knoten (`shared`) |
| Vollbild ja/nein | `localStorage` des Browsers (`titanos.agent.full`) |
| Chats, Projekte, Gedächtnis, Skills, geplante Aufträge | `.titanos/agent/<konto>/` auf dem Volume des Agenten: beim Einschalten gewählt (`volume` in `agent.json`), danach nur per Umziehen geändert; ältere Installationen ohne Wahl nehmen beim ersten Gebrauch das erste eingebundene |
| Darstellung | `localStorage` des Browsers |
| Gelesen-Stand je Chat | `.titanos/agent/<konto>/seen.json` (nur bei Änderung geschrieben) |
| Stand der Abbilder in der Freigabe | `.titanos/agent/<konto>/mirror.json` |
| Ordner des Kontos | Freigabe `KI-Agent/<konto>/` auf dem Volume des Agenten |
| Snapshots vor Änderungen | `.titanos/snapshots/agent-*` auf Btrfs-Volumes, 7 Tage |

Ohne dieses Volume ist der Agent nicht verfügbar (er weicht nicht still auf ein anderes aus); er speichert nie im RAM oder auf dem Stick. Ein Auftrag, den titand nicht beenden konnte (Neustart, Update), wird beim nächsten Start als „unterbrochen“ markiert. App-Updates warten, solange ein Auftrag läuft.

## Prüfstand (5.11.2026)

- Go-Tests: Ablauf, Freigaben, Rechte, Ordner-Regeln, Speicher, Websuche (mit nachgebautem Ollama und Testservern), API-Rechte, Aktivitätsprotokoll, Assist-Adressprüfung.
- Live gegen Ollama 0.40.14 mit gpt-oss:20b (Fragen ohne Änderungen; `TITANOS_OLLAMA=… go test ./internal/api -run TestAgentLiveOllama`).
- Ereignisstrom (SSE) durch die ganze Handler-Kette, Ordner-Regeln auch über Volume-Wurzeln, Home-Assistant-Regeln in tiefen Argumenten.
- Oberfläche: `scripts/test-ui-agent.mjs` mit Testdaten (Desktop, Handy, eigene App, Einstellungen); Seiten-Crawl gegen `titand -dev` ohne Befund; Einstellungen gegen das echte Ollama über `titand -dev` (ohne Daten-Volume, daher keine Chats).
- Kopfzeile: mit dem 13. Reiter passt bei 1366 px alles, bei 1280 px kommen die Pfeile.
- PLAN 47: `test-ui-agent` prüft zusätzlich ☰ statt rundem Knopf, Suche und „Angeheftet“ auf dem Handy, 16 px im Eingabefeld, Dateiauswahl vom NAS bis zu den Kärtchen, Meldung bei zu großen Dateien, ⋯-Menü, Vollbild hin und zurück (auch nach Neuladen), Einstellungen für Mitglied und Admin-Knoten. Go-Tests: Anhängen wie der Browser (leerer Chat, Upload mit Dateityp, Nachricht, Lesen von Anhang und NAS-Datei), zwei echte API-Server im Cluster mit geteiltem Agenten (keine Freigabe, Anfrage, Freigabe, Chat bleibt auf dem Knoten, kein Herunterladen, Entziehen).
- PLAN 48: `test-ui-agent` prüft das Agent-Menü am Desktop (neben dem TitanOS-Menü, „Freigabe“ beim wartenden Chat) und am Handy (☰, Angeheftet zuerst, Ungelesen-Punkt, Suche, „Zurück zu TitanOS“ lädt das Dashboard), Modell und Denkstufe oben (Fenster bzw. Blatt), Vorschläge füllen das Eingabefeld, Handy ohne TitanOS-Rahmen auch ohne `?app=1`, Neuladen samt richtigem Manifest beim Wechsel TitanOS ↔ Agent in beide Richtungen (auch Zurück/Vor ohne Hin- und Herspringen), die Seen-Meldung des offenen Chats. Go-Test `TestAgentReadStateAndPush`: ungelesen nach einer Antwort, eine Mitteilung bis zum Ansehen, gelesen nach `seen`, keine Mitteilung bei sichtbarem Chat, wieder ungelesen nach neuer Antwort, kein Schreiben ohne Änderung; `TestSeenNeedsNoDiskWithoutChange`: wiederholtes Ansehen öffnet das Volume nicht.
- PLAN 49/51 (5.11.2026): Go-Tests `TestAgentScheduleKinds` (alle Zeitarten, Nachholen, Eingabeprüfung, Morgenbericht-Text), `TestAgentReminder` (Erinnerung aus dem Chat, Bestätigung, Lauf ohne Modell), `TestAgentShareFolders` (Freigabe mit `perUser`, Ordner, Wissen, Kopie des Chats, nur eigener Ordner im Dateimanager, kein Schreiben durch einen gelegten Link, geänderte Datei als Inhalt und `tainted`, kein `perUser` über die API), `TestAgentSharedProject` (Mitglied sieht und liest, schreibt nicht in fremde Chats, eigener Chat mit Anweisungen, Nicht-Mitglied sieht nichts), `TestAgentVoiceAndPictures` (Whisper aus/an, Adressprüfung, Weitergabe, Status; Bild an das Bild-Modell), `TestPerUserShare` (smb.conf mit `%u`). `test-ui-agent`: Geplant mit Morgenbericht und neuer Erinnerung, Mikrofon- und Foto-Knopf, geteiltes Projekt mit Lese-Chat am Handy, Aufgaben-Blatt und Kontoknopf am Handy, Einstellungen Sprache/Bilder/Snapshots.
- **Nicht geprüft (PLAN 49/51):** echter Whisper-Dienst und Mikrofon am iPhone (Safari braucht HTTPS), echtes Bild-Modell, Btrfs-Snapshot auf echter Platte (nur über die vorhandenen Speicherfunktionen), SMB-Zugriff auf `KI-Agent/%u` mit echtem Samba.
- **Nicht geprüft:** geteilter Agent zwischen zwei echten NAS; echtes Home Assistant (Assist über die Ollama-Integration, Konfigurations-API für Automationen, Langzeit-Statistik), echte Push-Mitteilungen (auch das Aufräumen in der Mitteilungszentrale), Installation als App auf echten Geräten (Home-Bildschirm-Symbol des Agenten auf dem iPhone).
- **PLAN 90-4 (14.11.2026):** Go-Tests `TestPlan90MemoryPinTopicClear` (Anheften, Thema, Leeren behält Angeheftetes, Eingabeprüfung), `TestPlan90MemoryTouchAndFade` („zuletzt genutzt“ einmal am Tag und nur im Projekt des Chats, Verblassen ohne Angeheftetes und Genutztes), `TestPlan90ReplaceMessage` (Bearbeiten, Neu erzeugen, nur eigene Nachrichten dieses Chats; mit nachgebautem Ollama), `TestPlan90Home` (Zustand nur für Besitzer/Administratoren). Oberfläche: neue Suite `scripts/test-ui-agent90.mjs` (Startseite mit Zustandszeilen und Zählung, Suche im Chat samt Marken, Neu erzeugen und Bearbeiten mit der gesendeten Anfrage, „/“-Befehle, Gedächtnis mit Ordnung/Suche/Anheften/Ändern/Löschen/Leeren und Regler „unbegrenzt“, Wissen mit Suche, Projekt-Blatt mit Dateien und Nachschlage-Orten, Einstellungen ohne Doppeltes; am Handy einzeiliger Kopf, Blätter, Karten, 44 px, wachsendes Eingabefeld, kein Überlauf). `test-ui-agent` an die Seiten angepasst (entfallen: Hereinwischen der Unterpunkte, „Einstellungen bleiben unter einem Fenster“, Projekt-Blatt von der Startseite; dafür Adressen, Zurück, Seitenleiste und Überlaufprüfungen der neuen Seiten), dazu bestanden `test-ui-agent-archive`, `test-ui-agent54-docs`, `-family`, `-foresight`, `-protect`. `test-ui-agent54-foresight` fiel in einem von vier Läufen an einer Stelle aus, die nicht zum Agenten gehört (Dialog „Jetzt prüfen“ unter Einstellungen › KI-Agent › Funktionen); Ursache nicht untersucht.
- **Nicht geprüft (PLAN 90-4):** echtes Handy (Wischen, langes Drücken, Blatt nach unten wegwischen), echtes Modell beim Bearbeiten und Neu erzeugen, NAS-Zustand gegen echte Volumes, Sicherungen und Docker (nur nachgebaute Antwort bzw. leeres Testsystem), „zuletzt genutzt“ über mehrere Tage.
- **PLAN 91 (14.11.2026):** Go-Tests `TestPlan91AssistKey` (Schlüssel statt Adresse, 401, Schlüssel nie in den Einstellungen, neuer Schlüssel beendet den alten, nur Verwalter), `TestPlan91AssistRights` (Voreinstellungen, jeder Bereich einzeln, nie mehr als die allgemeinen Rechte, Grenzen gelten nicht für Konten), `TestPlan91AssistProfile` (Frage landet im Profil, dessen Gedächtnis im Prompt und nichts vom Besitzer, Einsicht nur für Verwalter), `TestAssistOverHTTP` (nur `/agent-ollama/` ohne Umleitung). Oberfläche in `test-ui-agent`: Karte mit Adressen, Schlüssel erzeugen/zeigen/neu, abgelehnter Versuch mit Grund, 16 Bereiche, gespeicherte Stufen, Profil-Ansicht mit `?profile=assist` in jeder Anfrage. **Nicht geprüft:** echtes Home Assistant (Einrichtung der Ollama-Integration mit Schlüssel über http und über den Tailscale-Namen, ein Gespräch, Sprachgerät) – das prüft der Inhaber auf dem NAS (`PRUEFEN.txt`).
