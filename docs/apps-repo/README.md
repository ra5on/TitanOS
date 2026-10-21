# Tests für das Apps-Repository (Vorschlag)

Die App-Vorlagen liegen in [ra5on/TitanOS-Apps](https://github.com/ra5on/TitanOS-Apps).
Die Release-CI von TitanOS testet keine App-Installationen mehr. Die drei Dateien
hier sind die bisherigen Tests und sollen in das Apps-Repo umziehen (dort als
Vorschlag auf einem Branch, nicht direkt auf `main`):

| Datei | Ziel im Apps-Repo | Zweck |
|---|---|---|
| `native-apps.test.mjs` | `tests/native-apps.test.mjs` | Node-Test: Installationsformular von Immich und Ollama (Geheimnisse, Pfade, Ports) |
| `native-apps-render.mjs` | `tests/native-apps-render.mjs` | Rendert eine Vorlage mit dem Installationscode von TitanOS |
| `native_apps_smoke.py` | `tests/native_apps_smoke.py` | Installiert, nutzt und erstellt Immich und Ollama neu (nur auf einem GitHub-Actions-Runner) |

Die Skripte lesen die Vorlagen aus `../<app>/compose.yaml` relativ zu ihrem Ordner
(`tests/`) und brauchen die Umgebungsvariable `TITANOS_DIR`: ein Checkout von
`ra5on/TitanOS` mit `npm ci` in `web/`. Die Pfade in den Skripten sind darauf
ausgelegt, dass sie im Apps-Repo unter `tests/` liegen.

Beispiel für `.github/workflows/apps.yml` im Apps-Repo:

```yaml
name: Apps
on:
  pull_request:
  push:
    branches: [main]
jobs:
  apps:
    name: Native app installations
    runs-on: ubuntu-24.04
    timeout-minutes: 45
    steps:
      - uses: actions/checkout@v7
      - uses: actions/checkout@v7
        with:
          repository: ra5on/TitanOS
          path: titanos
      - uses: actions/setup-node@v7
        with:
          node-version: 24
          cache: npm
          cache-dependency-path: titanos/web/package-lock.json
      - run: cd titanos/web && npm ci
      - run: node --test tests/*.test.mjs
        env:
          TITANOS_DIR: ${{ github.workspace }}/titanos
      - name: Install, use and recreate Immich and Ollama with Open WebUI
        run: python3 tests/native_apps_smoke.py
        env:
          TITANOS_DIR: ${{ github.workspace }}/titanos
```

Die Skripte sind hier nur abgelegt und werden von TitanOS nicht ausgeführt.

## Vorlage TitanOS-Agent

`titanos-agent/compose.yaml` ist die App-Vorlage für den Beispiel-Agenten aus `examples/agent` (Chat mit einem Ollama-Modell über die MCP-Werkzeuge von TitanOS). Sie gehört nach `titanos-agent/compose.yaml` im Apps-Repo, sobald das Image `ghcr.io/ra5on/titanos-agent` veröffentlicht und öffentlich sichtbar ist.

