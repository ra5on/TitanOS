# Apple clients

TitanKit provides the connection and security model shared by the iOS and macOS
apps.

## Identifiers

The generated Xcode projects are not committed. Their `project.yml` files are the
source of truth for targets, identifiers, signing, and entitlements.

| Component | Identifier |
| --- | --- |
| Apple Team ID | `JABS8D63XG` |
| iOS app (production) | `io.github.ra5on.titanos.app` |
| iOS app (local development) | `io.github.ra5on.titanos.app.dev` |
| Photos background-upload extension (production) | `io.github.ra5on.titanos.app.photo-background-upload` |
| Photos background-upload extension (local development) | `io.github.ra5on.titanos.app.dev.photo-background-upload` |
| Shared photo-backup group (production) | `group.io.github.ra5on.titanos.app.photos` |
| Shared photo-backup group (local development) | `group.io.github.ra5on.titanos.app.dev` |
| macOS app | `io.github.ra5on.titanos.mac` |

The iOS app and PhotoKit extension use the same shared group for coordinated files
and the source-scoped upload grant. Sessions, certificate pins, and source IDs stay
in the app's private Keychain access group.

## Connection security

Pairing saves the Titan's local CA for app-scoped HTTPS trust. Tailscale addresses
use HTTP inside Tailscale's authenticated WireGuard tunnel. The Titan CA is never
installed as a system-wide certificate.

## Titan-Status

Die Client-Quellen verwenden Titan-Namen und den Titan-Dienst zur Netzwerkerkennung. Native Apple-Apps werden mit dem NAS-Image nicht veröffentlicht. Für einen eigenen nativen Build sind eine eigene Apple-Signatur und Geräteprüfungen erforderlich. Der frühere native Update-Feed ist deaktiviert; ein Titan-Feed muss separat konfiguriert und signiert werden.
