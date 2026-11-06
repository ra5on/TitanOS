# titanos-viewer

Container image for the TitanOS remote view (Fernansicht, PLAN 63,
`docs/FERNANSICHT.md`): Chromium in kiosk mode on Xvfb shows exactly one
Docker app; the agent `titanos-viewer` (`backend/cmd/titanos-viewer`)
captures the screen with GStreamer (H.264), the sound with PulseAudio
(Opus) and serves the stream protocol on port 7300 to titand.

| File | Purpose |
|---|---|
| `Dockerfile` | Debian trixie-slim (pinned by digest), Debian packages only, agent binary, `USER 1000`, `tini` as PID 1 |
| `dpkg-excludes` | dpkg path filters: no docs (except copyright), man pages, translations, Chromium locales other than de/en, and libraries only used by plugins the viewer never loads |
| `pulse.pa` | PulseAudio: null sink `titan`, local socket only, no network modules |
| `test-integration.sh` | end-to-end test with Docker (see below) |

The package list (`name version source source-version`) is in the image at
`/usr/share/titanos-viewer/packages.txt` and next to the release file.

## Build

```
scripts/build-viewer-image.sh <version> <out-dir>
```

builds the agent (`CGO_ENABLED=0`), the image `titanos-viewer:<version>`
and writes `<out-dir>/titanos-viewer-<version>.tar.zst`
(`docker save | zstd -19`) and `<out-dir>/titanos-viewer-<version>.packages.txt`.
The last line of stdout is the image ID. `VIEWER_TAG_DEV=1` also tags
`titanos-viewer:dev`. Debian packages come from the live mirror, so two
builds on different days can differ in package versions; the base image
is pinned.

## Run (what titand does)

```
docker run -d --name titanos-viewer-<user>-<app> \
  --network titanos-viewer \                 # internal network, no way out
  --read-only --tmpfs /tmp:rw,nosuid,nodev,size=512m \
  --cap-drop ALL --security-opt no-new-privileges --user 1000:1000 \
  -v <volume>/.titanos/viewer/<user>/<app>:/profile \
  -e VIEWER_URL=http://<app-ip>:<port> -e VIEWER_KEY=<session key> \
  -e VIEWER_AUDIO=1 -e VIEWER_GPU=cpu|vaapi|nvenc \
  [--device /dev/dri/renderD128 --group-add <gid of renderD128> -e VIEWER_GPU_DEVICE=/dev/dri/renderD128] \
  titanos-viewer:<version>
```

- Only `/tmp` (tmpfs) and `/profile` need to be writable. `/run` is not
  used, `HOME` is ignored. The profile directory must be writable by
  UID 1000. Chromium uses `/dev/shm`: give the container `--shm-size`
  (titand: 512m; 256m is enough for one 4K view).
- Chromium managed policies are generated at start into
  `/tmp/titanos-viewer/policies`; `/etc/chromium/policies/managed` is a
  symlink there (made at build time).
- `GET /healthz` (no key) answers `ok` once the agent listens.
- `SIGTERM` (docker stop): the client gets `bye` (`stopped`), Chromium gets
  SIGTERM and its child processes up to 5 s to finish, so the cookie
  database is written (tested: a cookie set 0.1 s before the stop is there
  in the next container). Use `docker stop -t 10` or more.

Environment:

| Variable | Meaning |
|---|---|
| `VIEWER_URL` | app address, `http(s)://host[:port][/path]`; its origin is the only allowed origin |
| `VIEWER_KEY` | session key (≥ 16 characters), required as `?key=` or header `X-Viewer-Key` on `/stream` and `/downloads/{id}` |
| `VIEWER_GPU` | `cpu` (default), `vaapi`, `nvenc`; a GPU encoder that does not deliver a frame within 4 s falls back to `cpu` for the life of the container |
| `VIEWER_GPU_DEVICE` | render node for VA-API (default `renderD128`) |
| `VIEWER_AUDIO` | `1` starts PulseAudio and streams Opus |
| `VIEWER_FPS` | 5–60, default 30 |
| `VIEWER_QUALITY` | `auto` (default), `high`, `balanced`, `low` |
| `VIEWER_SIZE` | initial size, e.g. `1280x800@2` (default 1280x800, scale 1) |
| `VIEWER_LANG` | Chromium UI language, default `de` |
| `VIEWER_SANDBOX` | `1` drops `--no-sandbox` (only works if the container may create user namespaces) |
| `VIEWER_DEBUG` | `1` logs all child stderr |

## Implementation notes and limits

- **Screen size:** Xvfb cannot change its screen size via RandR (tested on
  trixie: `xrandr --fb` fails with BadValue). Xvfb therefore runs at
  3840x2160; `size` resizes the Chromium window (XConfigureWindow, no
  window manager) and the capture region, then the encoder restarts
  (new `hello`, key frame). A window that grows (page fullscreen) is put
  back within 0.5 s. Chromium starts with the first `size` of the first
  client (at most 3 s after the agent is up, else 1280x800 scale 1), so
  a HiDPI client does not cause a restart on open. A later change of the
  scale factor restarts Chromium with `--force-device-scale-factor`
  (page reloads; persistent cookies stay, session cookies are lost).
- **Video:** `gst-launch-1.0` writes Annex B with AUD to a pipe; the agent
  splits access units at the next AUD (one frame interval of latency).
  SPS/PPS precede every IDR; `hello.codec` is derived from the SPS
  (`avc1.PPCCLL`). Key frame requests and quality changes restart the
  pipeline (rate limited to 1/s); key frames also come every 3 s. If the
  client cannot keep up, video is dropped up to the next key frame and,
  in `auto`, the bitrate is lowered (raised again after 20 s without
  drops). Encoders only run while a client is connected.
- **Input:** XTest. Keys whose `key` matches the US layout of `code` are
  sent physically (so `KeyboardEvent.code` is right in the app); other
  printable characters are typed via a remapped spare keycode, layout
  independent. Ctrl+Alt+character (AltGr on Windows) is typed as text.
  `text` is typed on its own goroutine (US characters as physical keys,
  the rest via the remapped keycodes, ≤ 10000 characters); long pastes
  should go via `clipboard` + Ctrl+V.
- **Chromium sandbox:** off (`--no-sandbox`). With `--cap-drop ALL`, the
  default seccomp/AppArmor profiles deny user namespaces, and the setuid
  helper does not work with `no-new-privileges`. The container is the
  isolation boundary.
- **Cursor:** XFixes cursor changes are watched, but Chromium's cursors
  carry no names, so only `default` is reported today.
- **Untested here:** VA-API and NVENC encoding (the test machine has only
  a virtio GPU without video encode); the pipelines are built per the
  GStreamer 1.26 element docs. Tested: with `VIEWER_GPU=vaapi` (render
  node passed in) and `nvenc` the agent falls back to the CPU encoder and
  says `gpu: "cpu"` in `hello`.

## Test

```
viewer/test-integration.sh [image]   # KEEP=1 keeps the containers
```

needs Docker and Go; creates only `fern63-test-*` containers and the
network `fern63-test-net` and removes them at the end.
