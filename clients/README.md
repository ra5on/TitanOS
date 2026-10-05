# Client apps

Native companion apps for titanOS. They live outside the npm workspace because
they use native toolchains.

| Path | What |
| --- | --- |
| `apple/TitanKit/` | Shared Swift package for discovery, authentication, and API access |
| `apple/macos/` | macOS menu bar app |
| `apple/ios/` | iOS app |

[`TitanKit`](apple/TitanKit) owns discovery, native API transport, authentication,
saved devices, and Keychain storage for both Apple apps. Native clients update
independently of titanOS, so their server contracts must remain compatible. The
server's
[`client-contract.ts`](../packages/titand/source/modules/server/trpc/client-contract.ts)
keeps those request and response fields covered by titand's typecheck.

Each platform README contains its development and release commands.
