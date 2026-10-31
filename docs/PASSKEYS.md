# Passkeys

Seit 0.33.0 kann sich jedes Konto zusätzlich mit Passkeys (WebAuthn)
anmelden: Fingerabdruck, Gesicht oder PIN eines Handys, Computers oder
Sicherheitsschlüssels ersetzen Passwort **und** Zwei-Faktor-Code. Passwort
und TOTP bleiben unverändert nutzbar.

## Einrichten

Mein Konto › Passkeys › **Passkey hinzufügen**. Der Dialog verlangt das
Passwort und, wenn eingeschaltet, den Zwei-Faktor-Code; danach fragt das
Gerät nach seiner Entsperrung. Höchstens zehn Passkeys je Konto.

Ein Passkey gehört zu genau dem Namen, unter dem die Seite geöffnet war
(Relying-Party-ID, z. B. `nas.example.org`). Browser erlauben Passkeys nur
über HTTPS mit gültigem Zertifikat (Let's Encrypt oder ein importiertes,
dem Gerät vertrautes Zertifikat) und nie über eine IP-Adresse; nur
`localhost` geht auch ohne HTTPS. Mit dem selbst signierten Zertifikat ab
Werk lehnt der Browser ab; die Seite sagt das.

## Anmelden

Auf der Anmeldeseite **Mit Passkey anmelden**. Der Browser bietet die
Passkeys an, die er für diesen Namen kennt; ein Kontoname wird nicht
eingegeben (auffindbare Passkeys, `residentKey: required`).

## Technik

- Paket `internal/webauthn` (nur Standardbibliothek): eigener begrenzter
  CBOR-Leser (keine unbestimmten Längen, Tags, Gleitkommazahlen; Tiefe 8,
  1024 Einträge), COSE-Schlüssel ES256, EdDSA (Ed25519) und RS256 (≥ 2048 Bit).
- Geprüft werden Typ, Challenge (32 Byte, einmalig, 2 Minuten, nur im RAM,
  höchstens 256 offen), exakter Origin, `crossOrigin` aus, RP-ID-Hash,
  **User Present und User Verified**, Signatur über
  `authenticatorData || SHA-256(clientDataJSON)` und ein steigender Zähler,
  sobald ein Authenticator zählt (synchronisierte Passkeys senden 0).
  Attestation wird nicht ausgewertet (`none`).
- Gespeichert in `account.json` (`passkeys`: ID, Name, COSE-Schlüssel,
  RP-ID, Zähler, synchronisiert, angelegt; `webauthnId`: zufälliger
  Benutzer-Handle statt des Kontonamens). Auf den Stick wird nur beim
  Anlegen, Entfernen und bei einem geänderten Zähler geschrieben. Ältere
  titand ignorieren die Felder.
- Fehlversuche zählen wie falsche Passwörter (IP-Sperre, automatisches
  Sperren der Firewall), gesperrte Konten werden nach gültigem Passkey mit
  `account_disabled` abgewiesen. Setzt ein Administrator die
  Zwei-Faktor-Anmeldung eines Kontos zurück, werden auch dessen Passkeys
  entfernt.
- API: `GET /api/v1/account/passkeys`, `POST /api/v1/account/passkeys/begin`
  (Passwort, Code), `POST /api/v1/account/passkeys`,
  `DELETE /api/v1/account/passkeys/{id}`, öffentlich
  `POST /api/v1/login/passkey/begin` und `POST /api/v1/login/passkey`.
  Nicht im MCP-Werkzeugkatalog, nicht über API-Tokens.

## Prüfstand

Unit-Tests mit Software-Authenticator (ES256/Ed25519, falscher Origin,
falsche Challenge, falsche RP-ID, fehlendes UV, kaputte Signatur,
Zähler-Rückschritt, CBOR-Grenzen) und ein API-Ablauf Registrieren →
Anmelden → Entfernen. Echte Browser, Handys und Sicherheitsschlüssel sind
noch nicht abgenommen.
