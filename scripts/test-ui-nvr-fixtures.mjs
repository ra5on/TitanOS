// PLAN 79-7: isolated data for the NVR's pages, shaped like docs/NVR.md
// (phase 1 and "API Phase 2"). Used by test-ui-nvr.mjs, test-ui-nvr-phone.mjs
// and the README screenshots. Install it after the shared fixtures: these
// routes then answer first. Every request is kept in `state.requests`
// ([method, path, body]); pictures are drawn gradients, the live WebSocket
// sends no video (the pages then show still pictures) and the feed says what
// `state.activity` holds.

import { readFile } from "node:fs/promises";

const day = 86400;

// PLAN 85: the recordings' HLS with `state.vod` set – a real chain around the
// live suite's test video (VP9: the test Chromium has no H.264). Its ftyp and
// moov are the init segment, the fragments after them one media segment.
let media = null;
async function vodFile(name) {
  if (!media) {
    const file = await readFile(new URL("./test-ui-media/nvr-live-vp9.mp4", import.meta.url));
    let end = 0;
    while (end < file.length && file.toString("latin1", end + 4, end + 8) !== "moof") end += file.readUInt32BE(end);
    media = { init: file.subarray(0, end), segment: file.subarray(end) };
  }
  if (name === "master.m3u8")
    return { contentType: "application/vnd.apple.mpegurl", body: '#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=300000,RESOLUTION=320x180,CODECS="vp09.00.10.08"\nindex.m3u8\n' };
  if (name === "index.m3u8")
    return {
      contentType: "application/vnd.apple.mpegurl",
      body: '#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:12\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MAP:URI="init.mp4"\n#EXTINF:12.0,\nseg.m4s\n#EXT-X-ENDLIST\n',
    };
  return { contentType: "video/mp4", body: name === "init.mp4" ? media.init : media.segment };
}

/** A camera as administrators get it. */
export function camera(id, name, order, over = {}) {
  return {
    id,
    name,
    enabled: true,
    order,
    source: {
      kind: "rtsp",
      host: `192.168.1.${60 + order}`,
      port: 554,
      onvifPort: 0,
      user: "viewer",
      hasPassword: true,
      mainURL: `rtsp://192.168.1.${60 + order}:554/main`,
      subURL: `rtsp://192.168.1.${60 + order}:554/sub`,
      allowPublic: false,
    },
    streams: {},
    detect: { enabled: true, objects: ["person"], plates: false, faces: false },
    record: { mode: "events", retainDays: 14, preSeconds: 5, postSeconds: 5 },
    audio: { enabled: false, twoWay: false },
    ptz: false,
    notify: {
      titan: true,
      push: false,
      email: false,
      homeAssistant: false,
      agent: false,
      kinds: ["person", "offline"],
    },
    state: {
      online: true,
      known: true,
      fps: 15,
      detectionFps: 1.2,
      offlineSince: null,
    },
    ...over,
  };
}

const hardware = () => ({
  devices: [
    { kind: "npu", title: "Intel AI Boost", present: true, available: true },
    { kind: "igpu", title: "Intel Arc", present: true, available: true },
    { kind: "cpu", title: "CPU", present: true, available: true },
  ],
  auto: "npu",
  // PLAN 82-4: the GPUs that can decode (the NVIDIA card is with a VM).
  decoders: [
    { id: "0000:00:02.0", title: "Intel Arc", kind: "igpu", available: true },
    { id: "0000:01:00.0", title: "NVIDIA RTX A2000", kind: "nvidia", available: false, reason: "vm" },
  ],
  videoDecode: true,
  accelDevice: true,
  avx2: true,
  memoryBytes: 32 * 2 ** 30,
  enrichmentsPossible: true,
});

/**
 * The sample recordings run from 06:00 of the local day until now, so a run
 * between midnight and 06:00 would find none. Unless TZ is given, the suites
 * pin a fixed zone in which it is about 15:00 now; the browser inherits it
 * (call before chromium.launch).
 */
export function pinAfternoon() {
  if (process.env.TZ) return process.env.TZ;
  let off = (((15 - new Date().getUTCHours()) % 24) + 24) % 24;
  if (off > 14) off -= 24;
  // Etc/GMT zones count the other way round: UTC+3 is Etc/GMT-3.
  process.env.TZ = off === 0 ? "Etc/GMT" : off > 0 ? `Etc/GMT-${off}` : `Etc/GMT+${-off}`;
  return process.env.TZ;
}

/** The data a test starts from; change it before or while the page runs. */
export function nvrState(now = Math.floor(Date.now() / 1000)) {
  const midnight = (() => {
    const d = new Date(now * 1000);
    d.setHours(0, 0, 0, 0);
    return Math.floor(d.getTime() / 1000);
  })();
  const event = (n, cam, label, ago, over = {}) => ({
    id: `${now - ago}.000000-fix${n}`,
    camera: cam,
    label,
    subLabel: "",
    plate: "",
    score: 0.87,
    start: now - ago,
    end: now - ago + 14,
    hasClip: true,
    hasSnapshot: true,
    zones: [],
    reviewed: n > 3,
    kept: false,
    ...over,
  });
  return {
    now,
    midnight,
    requests: [],
    admin: true,
    settings: {
      enabled: true,
      volume: "Data",
      detector: { kind: "auto" },
      storage: { limitGB: 200 },
      privacyNoticeDismissed: true,
      rtspOut: false,
      rtspUser: "",
      rtspPassword: "",
      plates: [{ name: "Unser Auto", plates: ["K-TI 2026"] }],
      decoder: "auto",
    },
    state: "running",
    error: null,
    image: { state: "ready", version: "0.18.2", bytes: 0, size: 0, progress: 1 },
    detector: { kind: "auto", active: "npu", failover: null, inferenceMs: 11.4 },
    storage: {
      usedBytes: 21.4e9,
      recordingsBytes: 20.4e9,
      exportsBytes: 1e9,
      limitGB: 200,
      spanDays: 12.4,
      cap: "ok",
      lastCleanup: null,
    },
    hardware: hardware(),
    dockerOff: false,
    sleepingVolumes: [],
    cameras: [
      camera("haustuer", "Haustür", 1, {
        detect: { enabled: true, objects: ["person", "dog"], plates: false, faces: true },
        audio: { enabled: true, twoWay: true },
      }),
      camera("einfahrt", "Einfahrt", 2, {
        detect: { enabled: true, objects: ["person", "car"], plates: true, faces: false },
        record: { mode: "continuous", retainDays: 7, preSeconds: 5, postSeconds: 5 },
        // PLAN 82-3: chosen accounts (sam may not see this camera), a pause and a window.
        notify: {
          titan: true,
          push: true,
          email: false,
          homeAssistant: true,
          agent: false,
          kinds: ["car", "plate", "offline"],
          recipients: [
            { user: "alex", bell: true, push: true, email: false },
            { user: "sam", bell: false, push: true, email: false },
          ],
          pauseSeconds: 300,
          window: { on: true, days: [1, 2, 3, 4, 5], from: "22:00", to: "06:00" },
        },
      }),
      camera("garten", "Garten", 3, {
        ptz: true,
        source: { ...camera("garten", "Garten", 3).source, kind: "onvif", onvifPort: 8000, mainWidth: 2560, mainHeight: 1440, subWidth: 640, subHeight: 360 },
        record: { mode: "motion", retainDays: 14, preSeconds: 5, postSeconds: 5 },
      }),
      camera("terrasse", "Terrasse", 4, {
        state: {
          online: false,
          known: true,
          fps: 0,
          detectionFps: 0,
          offlineSince: new Date((now - 1800) * 1000).toISOString(),
        },
      }),
      // Removed: no live picture, its recordings stay until they expire (administrators only).
      camera("schuppen", "Schuppen", 5, {
        enabled: false,
        deleted: true,
        deletedAt: now - 2 * day,
        expiresAt: now + 12 * day,
        state: { online: false, known: false, fps: 0, detectionFps: 0, offlineSince: null },
      }),
    ],
    objects: [
      "person", "bicycle", "car", "motorcycle", "bus", "bird",
      "cat", "dog", "horse", "sheep", "cow", "bear",
    ],
    /**
     * What GET /nvr/recipients answers (PLAN 82-3): alex is signed in, mara
     * has neither an address nor a device, sam (Benutzer) may see the front
     * door and switched camera messages off on his device.
     */
    recipients: {
      mailServer: true,
      accounts: [
        { name: "alex", admin: true, email: true, pushDevices: 1, pushDevicesOff: 0, cameras: null, names: null },
        { name: "mara", admin: true, email: false, pushDevices: 0, pushDevicesOff: 0, cameras: null, names: null },
        // sam has no right "Namen und Kennzeichen" anywhere.
        { name: "sam", admin: false, email: true, pushDevices: 0, pushDevicesOff: 1, cameras: ["haustuer"], names: [] },
      ],
    },
    /** Rights of the signed-in account when it is no administrator. */
    rights: { haustuer: { live: true, recordings: false } },
    /** What GET /users/{name}/cameras answers per account. */
    userCameras: {},
    // PLAN 83: Home Assistant for the cameras' conditions; null: none set up.
    ha: null,
    discovered: [
      { address: "192.168.10.41", onvifPort: 8000, name: "Reolink RLC-810A", hardware: "RLC-810A", id: "uuid-1", known: false },
      { address: "192.168.10.44", onvifPort: 80, name: "Tapo C320WS", hardware: "C320WS", id: "uuid-2", known: false },
    ],
    activity: { haustuer: { motion: true, objects: ["person"] } },
    events: [
      event(1, "haustuer", "person", 600, { subLabel: "Patrick", end: null }),
      event(2, "einfahrt", "car", 3600 * 3, { subLabel: "Unser Auto", plate: "K-TI 2026" }),
      event(3, "garten", "person", 3600 * 4),
      event(4, "garten", "cat", 3600 * 5),
      event(5, "haustuer", "person", 3600 * 10, { hasClip: false, kept: true }),
      event(6, "haustuer", "dog", 8 * day + 3600),
    ],
    exports: [
      { id: "haustuer_ab12cd", camera: "haustuer", name: "Paketbote", from: now - 7200, to: now - 7080, created: now - 3600, state: "done", sizeBytes: 18234567, owner: "owner" },
      { id: "einfahrt_ef34gh", camera: "einfahrt", name: "Einfahrt am Nachmittag mit einem ziemlich langen Namen", from: now - 5000, to: now - 3200, created: now - 60, state: "running", sizeBytes: null, owner: "owner" },
      { id: "garten_ij56kl", camera: "garten", name: "Lücke", from: now - day - 3600, to: now - day - 3000, created: now - day, state: "failed", sizeBytes: null, owner: "" },
      // Made outside TitanOS (Frigate's own export): no period known.
      { id: "haustuer_frigate", camera: "haustuer", name: "Alter Export", from: 0, to: 0, created: now - 3 * day, state: "done", sizeBytes: 4200000, owner: "" },
    ],
    faces: {
      enabled: true,
      faces: [
        { name: "Patrick", images: ["a1.webp", "a2.webp"] },
        { name: "Alicia", images: ["b1.webp"] },
      ],
      attempts: [
        { file: `${now - 900}.1-abc-${now - 899}.5-patrick-0.62.webp`, at: now - 899.5, guess: "Patrick", score: 0.62 },
        { file: `${now - 4000}.1-def-${now - 3999}.5--0.31.webp`, at: now - 3999.5, guess: "", score: 0.31 },
      ],
    },
    recognized: [
      { plate: "K-TI 2026", count: 14, lastSeen: now - 3600 * 3, camera: "einfahrt" },
      { plate: "B-XY 987", count: 1, lastSeen: now - day, camera: "einfahrt" },
      // Older than the newest 1000 vehicle events: nothing known about it.
      { plate: "HH-Q 4", count: 0, lastSeen: null, camera: "" },
    ],
    ptz: { supported: true, features: ["pt", "zoom"], presets: ["home", "tor", "terrasse"] },
    log: [
      "[2026-11-12 08:00:01] frigate.app                    INFO    : Starting Frigate (0.18.2)",
      "[2026-11-12 08:00:04] frigate.detectors.plugins.openvino INFO    : OpenVINO detector started on NPU",
      "[2026-11-12 08:00:05] frigate.app                    INFO    : Camera processor started for haustuer: 512 and a very long line that has to wrap instead of being cut off at the right edge of a narrow window",
    ],
    npuFirmware: ["intel_vpu 0000:00:0b.0: Firmware: intel/vpu/vpu_37xx_v1.bin, version: 20250415*MTL_CLIENT_SILICON-release*1900*ci_tag_ud202518_vpu_rc_20250415_1900*ae83b65d01c"],
    /** Pushed by the tests: sockets of the feed that are open. */
    feedSockets: [],
    /** How many PTZ "stop" commands are answered with 429 rate_limited. */
    ptzDropStops: 0,
    /** The close reason of the talk WebSocket (docs/NVR.md "Gegensprechen"). */
    talkClose: "nvr_not_running",
  };
}

const hue = (text) => [...text].reduce((h, c) => (h * 31 + c.charCodeAt(0)) % 360, 7);
const picture = (seed, label = "") => {
  const h = hue(seed);
  return `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360" viewBox="0 0 640 360"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(${h} 30% 34%)"/><stop offset="1" stop-color="hsl(${(h + 40) % 360} 28% 14%)"/></linearGradient></defs><rect width="640" height="360" fill="url(#g)"/><rect y="250" width="640" height="110" fill="hsl(${h} 20% 10%)" opacity=".55"/>${label ? `<text x="20" y="40" font-family="sans-serif" font-size="20" fill="#fff" opacity=".5">${label}</text>` : ""}</svg>`;
};

/** PLAN 82-4: the decoder as the setting resolves (nvr/hardware.go); "auto" never takes NVIDIA. */
function decoderStatus(s) {
  const setting = s.settings.decoder || "auto";
  const cpu = { setting, active: "cpu", kind: "cpu", title: "CPU" };
  const list = s.hardware.decoders ?? [];
  if (setting === "cpu") return cpu;
  if (setting === "auto") {
    for (const kind of ["igpu", "gpu"]) {
      const d = list.find((g) => g.kind === kind && g.available);
      if (d) return { setting, active: d.id, kind: d.kind, title: d.title };
    }
    return cpu;
  }
  const d = list.find((g) => g.id === setting);
  if (!d) return { ...cpu, reason: "gone" };
  if (!d.available) return { ...cpu, reason: d.reason || "driver" };
  return { setting, active: d.id, kind: d.kind, title: d.title };
}

function status(s) {
  const mine = visible(s).filter((c) => !c.deleted);
  const counts = {
    total: mine.length,
    enabled: mine.filter((c) => c.enabled).length,
    online: mine.filter((c) => c.enabled && c.state.online).length,
  };
  if (!s.admin)
    return {
      enabled: s.settings.enabled,
      state: s.settings.enabled && s.state !== "running" ? "unavailable" : s.settings.enabled ? "running" : "off",
      cameras: counts,
      privacyNoticeDismissed: s.settings.privacyNoticeDismissed,
    };
  return {
    enabled: s.settings.enabled,
    state: s.settings.enabled ? s.state : "off",
    error: s.error,
    image: s.image,
    volume: s.settings.volume,
    volumeMounted: s.state !== "no_volume",
    detector: { ...s.detector, kind: s.settings.detector.kind },
    storage: { ...s.storage, limitGB: s.settings.storage.limitGB },
    cameras: counts,
    privacyNoticeDismissed: s.settings.privacyNoticeDismissed,
    rtspOut: s.settings.rtspOut,
    frigateVersion: "0.18.2",
    hardware: s.hardware,
    decoder: decoderStatus(s),
  };
}

function visible(s) {
  if (s.admin) return s.cameras;
  return s.cameras
    .filter((c) => !c.deleted && (s.rights[c.id]?.live || s.rights[c.id]?.recordings))
    .map((c) => ({
      id: c.id,
      name: c.name,
      enabled: c.enabled,
      order: c.order,
      audio: c.audio,
      ptz: c.ptz,
      state: c.state,
      rights: { live: !!s.rights[c.id]?.live, recordings: !!s.rights[c.id]?.recordings, names: !!s.rights[c.id]?.names },
    }));
}

function diagnosis(s) {
  return {
    state: s.settings.enabled ? s.state : "off",
    container: {
      state: s.state === "running" ? "running" : s.state === "stopped" ? "exited" : "missing",
      exitCode: s.state === "stopped" ? 1 : 0,
      startedAt: new Date((s.now - 7200) * 1000).toISOString(),
      finishedAt: null,
      recentEnds: s.detector.failover ? 3 : 0,
    },
    imageReference: "ghcr.io/blakeblackshear/frigate@sha256:9ebab4801be69ab4ebc7f6ab1d582c859ff7412d457a3fa550056894e8444dad",
    imageVersion: "0.18.2",
    imagePresent: true,
    runningVersion: s.state === "running" ? "0.18.2-abc1234" : "",
    detector: { ...s.detector, kind: s.settings.detector.kind },
    detectorPid: s.state === "running" ? 412 : null,
    lastError: s.error,
    log: s.log,
    logSource: "frigate",
    accelDevice: true,
    npuFirmware: s.npuFirmware,
    gpuUsage: {},
    npuUsage: {},
    cameras: s.cameras.filter((c) => !c.deleted).map((c) => ({
      id: c.id,
      name: c.name,
      ...c.state,
      processFps: c.state.online ? 5 : 0,
      skippedFps: 0,
    })),
    shm: { totalMB: 384, usedMB: 143.2, freeMB: 240.8 },
    hardware: s.hardware,
    decoder: decoderStatus(s),
  };
}

function segments(s, from, to) {
  // Recorded from 06:00 with a gap 11:00–11:40; movement and objects in places.
  const out = [];
  const end = Math.min(to, s.now);
  for (let t = Math.max(from, s.midnight + 6 * 3600); t < end; t += 10) {
    const inDay = t - s.midnight;
    if (inDay >= 11 * 3600 && inDay < 11 * 3600 + 2400) continue;
    const minute = Math.floor(inDay / 60);
    out.push({
      start: t,
      end: Math.min(t + 10, end),
      motion: minute % 17 < 3 ? 120 : 0,
      objects: minute % 61 < 2 ? 2 : 0,
    });
  }
  return out;
}

/**
 * Answers the NVR's routes from `state` on a page or a browser context.
 * Returns the state (made here when none is given).
 */
export async function installNvr(target, state = nvrState()) {
  const s = state;
  // PLAN 83: the connections and entities the condition of a camera offers.
  await target.route(/\/api\/v1\/homeassistant\/(connection-names|entities)/, (route) => {
    const url = new URL(route.request().url());
    if (!s.ha) return route.fulfill({ status: 404, json: { error: { code: "ha_off", message: "ha_off" } } });
    if (url.pathname.endsWith("/connection-names")) return route.fulfill({ json: { connections: s.ha.connections } });
    const connection = url.searchParams.get("connection") || "main";
    return route.fulfill({ json: { entities: s.ha.entities[connection] ?? [] } });
  });
  await target.route(/\/api\/v1\/(nvr(\/|$|\?)|users\/[^/]+\/cameras)/, async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname.slice("/api/v1".length);
    const method = request.method();
    let body = null;
    try {
      body = request.postDataJSON();
    } catch {
      body = request.postData();
    }
    s.requests.push([method, path + url.search, body, Date.now()]);
    const json = (value, status = 200) => route.fulfill({ status, json: value });
    const fail = (status, code) =>
      route.fulfill({ status, json: { error: { code, message: code } } });
    const image = (seed, label) =>
      route.fulfill({ status: 200, contentType: "image/svg+xml", body: picture(seed, label) });
    const adminOnly = () => !s.admin && (fail(403, "forbidden"), true);
    let m;

    if ((m = /^\/users\/([^/]+)\/cameras$/.exec(path))) {
      const name = decodeURIComponent(m[1]);
      const rows = (s.userCameras[name] ??= {
        admin: false,
        cameras: s.cameras.map((c) => ({ id: c.id, name: c.name, live: false, recordings: false, names: false, ...(c.deleted ? { deleted: true } : {}) })),
      });
      if (method === "PUT")
        for (const [id, rights] of Object.entries(body?.cameras ?? {})) {
          const row = rows.cameras.find((c) => c.id === id);
          // Names count only with recordings (PLAN 82-3); left out, they stay.
          const names = (rights.names ?? row?.names) === true && !!rights.recordings;
          if (row) Object.assign(row, { live: !!rights.live, recordings: !!rights.recordings, names });
        }
      return json(rows);
    }
    if (path === "/nvr/status") return json(status(s));
    if (path === "/nvr/recipients") {
      if (adminOnly()) return;
      // Administrators see every camera (null in the state: all of them).
      const all = s.cameras.filter((c) => !c.deleted).map((c) => c.id);
      return json({
        mailServer: s.recipients.mailServer,
        accounts: s.recipients.accounts.map((a) => ({ ...a, cameras: a.cameras ?? all, names: a.names ?? all })),
      });
    }
    if (path === "/nvr/settings") {
      if (adminOnly()) return;
      if (method === "PUT") {
        // PLAN 85: saving the known plates fails with this code.
        if (s.platesFail && body.plates) return fail(503, s.platesFail);
        if (body.volume && s.sleepingVolumes.includes(body.volume) && !body.confirmSleepingVolume)
          return fail(409, "nvr_volume_sleeps");
        const { enableDocker: _d, confirmSleepingVolume: _s, ...rest } = body;
        Object.assign(s.settings, rest);
        if (rest.rtspOut) Object.assign(s.settings, { rtspUser: "titanos", rtspPassword: "k3Jw-91Lm-x0Qp-77Zt" });
      }
      return json(s.settings);
    }
    if (path === "/nvr/enable" && method === "POST") {
      if (s.dockerOff && !body?.enableDocker) return fail(409, "nvr_docker_off");
      if (s.sleepingVolumes.includes(body?.volume) && !body?.confirmSleepingVolume)
        return fail(409, "nvr_volume_sleeps");
      s.dockerOff = false;
      s.settings.enabled = true;
      if (body?.volume) s.settings.volume = body.volume;
      if (s.state === "docker_off") s.state = "running";
      return json(status(s));
    }
    if (path === "/nvr/disable" && method === "POST") {
      s.settings.enabled = false;
      return json(status(s));
    }
    if (path === "/nvr/diagnosis") return adminOnly() || json(diagnosis(s));
    if (path === "/nvr/detector/retry" && method === "POST") {
      s.detector = { ...s.detector, active: s.hardware.auto, failover: null };
      s.state = "running";
      s.error = null;
      return json(status(s));
    }
    if (path === "/nvr/cameras/discover") return json({ cameras: s.discovered });
    if (path === "/nvr/cameras/probe") {
      if (body.password === "wrong")
        return json({ outcome: "unauthorized", device: null, profiles: [], mainURL: "", subURL: "", streams: [], ptz: false, onvifPort: 0, clockOffsetSeconds: 0 });
      const host = body.host || new URL(body.mainURL.replace(/^rtsp/, "http")).hostname;
      const onvif = body.kind === "onvif";
      const main = onvif ? `rtsp://${host}:554/h265Preview_01_main` : body.mainURL;
      const sub = onvif ? `rtsp://${host}:554/h264Preview_01_sub` : body.subURL;
      const third = `rtsp://${host}:554/h264Preview_01_ext`;
      // ONVIF: four profiles, three streams (two profiles share the main stream).
      const profiles = onvif
        ? [
            { token: "p0", name: "mainStream", width: 3840, height: 2160, codec: "H265", url: main },
            { token: "p1", name: "subStream", width: 640, height: 360, codec: "H264", url: sub },
            { token: "p2", name: "extStream", width: 1280, height: 720, codec: "H264", url: third },
            { token: "p3", name: "mainStream PTZ", width: 3840, height: 2160, codec: "H265", url: main },
          ]
        : [];
      return json({
        outcome: "ok",
        device: onvif ? { manufacturer: "Reolink", model: "RLC-810A", firmware: "3.1" } : null,
        profiles,
        mainURL: main,
        subURL: sub,
        streams: [
          { role: "main", url: main, ...(onvif ? { profile: "mainStream" } : {}), outcome: "ok", codec: "H265", audio: "AAC", width: 3840, height: 2160 },
          ...(sub ? [{ role: "sub", url: sub, ...(onvif ? { profile: "subStream" } : {}), outcome: "ok", codec: "H264", audio: "", width: 640, height: 360 }] : []),
          ...(onvif ? [{ role: "other", url: third, profile: "extStream", outcome: "ok", codec: "H264", audio: "", width: 1280, height: 720 }] : []),
        ],
        ptz: body.kind === "onvif",
        onvifPort: body.onvifPort || 0,
        clockOffsetSeconds: 0,
      });
    }
    // Camera names are unique, upper and lower case aside (removed cameras count too).
    const nameTaken = (name, id = "") =>
      s.cameras.some((c) => c.id !== id && c.name.trim().toLowerCase() === String(name ?? "").trim().toLowerCase());
    if (path === "/nvr/cameras") {
      if (method === "POST" && nameTaken(body.name)) return fail(409, "nvr_camera_name_exists");
      if (method === "POST") {
        const id = body.name.toLowerCase().replace(/ü/g, "ue").replace(/[^a-z0-9]+/g, "_");
        const made = camera(id, body.name, s.cameras.length + 1, {
          ...body,
          id,
          order: s.cameras.length + 1,
          source: { ...body.source, password: undefined, hasPassword: !!body.source.password, port: 554 },
        });
        s.cameras.push(made);
        return json(made, 201);
      }
      return json({ cameras: visible(s), ...(s.admin ? { objects: s.objects } : {}) });
    }
    if ((m = /^\/nvr\/cameras\/([^/]+)$/.exec(path))) {
      const index = s.cameras.findIndex((c) => c.id === m[1]);
      if (index < 0) return fail(404, "nvr_camera_not_found");
      if (method === "DELETE") {
        s.cameras.splice(index, 1);
        return route.fulfill({ status: 204 });
      }
      if (method === "PUT" && body.name !== undefined && nameTaken(body.name, m[1]))
        return fail(409, "nvr_camera_name_exists");
      // A narrower mode or fewer days delete recordings: the NAS asks first (manager.go, shrinks).
      const keeps = { continuous: 3, off: 3, motion: 2, events: 1 };
      const was = s.cameras[index].record;
      if (
        method === "PUT" &&
        body.record &&
        !body.confirmShrink &&
        (keeps[body.record.mode] < keeps[was.mode] || body.record.retainDays < was.retainDays)
      )
        return fail(409, "nvr_recordings_shrink");
      if (method === "PUT") {
        const { source, clearPassword: _c, confirmShrink: _s, ...rest } = body;
        Object.assign(s.cameras[index], rest);
        if (source) Object.assign(s.cameras[index].source, { ...source, password: undefined });
      }
      return json(s.cameras[index]);
    }
    if (/^\/nvr\/cameras\/[^/]+\/snapshot\.jpg$/.test(path)) {
      const id = path.split("/")[3];
      const cam = s.cameras.find((c) => c.id === id);
      if (!cam || !cam.state.online) return fail(404, "nvr_no_picture");
      return image(id);
    }
    if ((m = /^\/nvr\/cameras\/([^/]+)\/ptz$/.exec(path))) {
      if (method === "POST") {
        // The NAS takes at most 10 commands a second; tests drop a "stop" this way.
        if (body?.action === "stop" && s.ptzDropStops > 0) {
          s.ptzDropStops--;
          return fail(429, "rate_limited");
        }
        return route.fulfill({ status: 204 });
      }
      const cam = s.cameras.find((c) => c.id === m[1]);
      return json(cam?.ptz ? s.ptz : { supported: false, features: [], presets: [] });
    }
    if (/^\/nvr\/cameras\/[^/]+\/recordings\/summary$/.test(path))
      return json({ days: [{ day: new Date(s.now * 1000).toISOString().slice(0, 10), hours: [] }] });
    // A removed camera's recordings deleted now (owner 13.11.2026): only removed ones, administrators.
    if ((m = /^\/nvr\/cameras\/([^/]+)\/recordings$/.exec(path)) && method === "DELETE") {
      if (!s.admin) return fail(403, "forbidden");
      const index = s.cameras.findIndex((c) => c.id === m[1] && c.deleted);
      if (index < 0) return fail(404, "nvr_camera_not_found");
      if (s.purgeFails) return fail(502, "nvr_purge_failed");
      s.cameras.splice(index, 1);
      return route.fulfill({ status: 204 });
    }
    if (/^\/nvr\/cameras\/[^/]+\/recordings$/.test(path)) {
      const from = Number(url.searchParams.get("from"));
      const to = Number(url.searchParams.get("to"));
      return json({ from, to, segments: segments(s, from, to) });
    }
    if ((m = /\/vod\/\d+\/\d+\/(master\.m3u8|index\.m3u8|init\.mp4|seg\.m4s)$/.exec(path)) && s.vod)
      return route.fulfill({ status: 200, ...(await vodFile(m[1])) });
    if (/\/vod\//.test(path)) return fail(404, "not_found");
    if (path === "/nvr/events") {
      const cams = url.searchParams.get("camera")?.split(",");
      const labels = url.searchParams.get("label")?.split(",");
      const from = Number(url.searchParams.get("from")) || 0;
      const to = Number(url.searchParams.get("to")) || Infinity;
      const allowed = new Set(visible(s).map((c) => c.id));
      return json({
        events: s.events.filter(
          (e) =>
            allowed.has(e.camera) &&
            (!cams || cams.includes(e.camera)) &&
            (!labels || labels.includes(e.label)) &&
            e.start >= from &&
            e.start < to,
        ),
        next: null,
      });
    }
    if ((m = /^\/nvr\/events\/([^/]+)(\/.*)?$/.exec(path))) {
      const id = decodeURIComponent(m[1]);
      const event = s.events.find((e) => e.id === id);
      if (!event) return fail(404, "nvr_event_not_found");
      if (m[2] === "/thumbnail.jpg" || m[2] === "/snapshot.jpg") return image(event.camera + event.label, "");
      if (m[2] === "/clip.mp4") return route.fulfill({ status: 200, contentType: "video/mp4", body: "" });
      if (m[2] === "/reviewed") {
        event.reviewed = !!body.reviewed;
        return json(event);
      }
      if (method === "DELETE") {
        s.events = s.events.filter((e) => e !== event);
        return route.fulfill({ status: 204 });
      }
      return json(event);
    }
    if (path === "/nvr/exports") {
      if (method === "POST") {
        const made = { id: `${body.camera}_new${s.exports.length}`, camera: body.camera, name: body.name || "Export", from: body.from, to: body.to, created: s.now, state: "running", sizeBytes: null, owner: "owner" };
        s.exports.unshift(made);
        return json(made, 201);
      }
      return json({ exports: s.exports });
    }
    if ((m = /^\/nvr\/exports\/([^/]+)(\/.*)?$/.exec(path))) {
      const item = s.exports.find((e) => e.id === m[1]);
      if (!item) return fail(404, "nvr_export_not_found");
      if (m[2] === "/thumbnail.jpg") return image(item.camera);
      if (m[2] === "/download") return route.fulfill({ status: 200, contentType: "video/mp4", body: "" });
      if (method === "PUT") item.name = body.name;
      if (method === "DELETE") {
        s.exports = s.exports.filter((e) => e !== item);
        return route.fulfill({ status: 204 });
      }
      return json(item);
    }
    if (path === "/nvr/faces") {
      if (adminOnly()) return;
      if (method === "POST") {
        if (s.faces.faces.some((f) => f.name === body.name)) return fail(409, "nvr_face_exists");
        s.faces.faces.push({ name: body.name, images: [] });
        return json(s.faces, 201);
      }
      return json(s.faces);
    }
    // Faces seen but not assigned (docs/NVR.md: /face-attempts, not /faces/attempts).
    if ((m = /^\/nvr\/face-attempts\/([^/]+)(\/assign)?$/.exec(path))) {
      const file = decodeURIComponent(m[1]);
      if (method === "GET") return image(file);
      s.faces.attempts = s.faces.attempts.filter((a) => a.file !== file);
      if (!m[2]) return route.fulfill({ status: 204 });
      let face = s.faces.faces.find((f) => f.name === body.name);
      if (!face) s.faces.faces.push((face = { name: body.name, images: [] }));
      face.images.push("neu.webp");
      return json(s.faces);
    }
    if ((m = /^\/nvr\/faces\/([^/]+)(\/images(\/[^/]+)?)?$/.exec(path))) {
      const name = decodeURIComponent(m[1]);
      const face = s.faces.faces.find((f) => f.name === name);
      if (!face) return fail(404, "nvr_face_not_found");
      if (m[3] && method === "GET") return image(name + m[3]);
      if (m[3] && method === "DELETE") {
        face.images = face.images.filter((f) => "/" + f !== decodeURIComponent(m[3]));
        return route.fulfill({ status: 204 });
      }
      if (m[2] && method === "POST") {
        face.images.push(`neu${face.images.length}.webp`);
        return json(s.faces, 201);
      }
      if (method === "PUT") {
        face.name = body.name;
        return json(s.faces);
      }
      if (method === "DELETE") {
        s.faces.faces = s.faces.faces.filter((f) => f !== face);
        return route.fulfill({ status: 204 });
      }
      return json(s.faces);
    }
    if (path === "/nvr/plates/recognized") return adminOnly() || json({ plates: s.recognized });
    return fail(404, "not_found");
  });

  // The live picture: go2rtc's protocol without video; the pages fall back to still pictures.
  await target.routeWebSocket(/\/api\/v1\/nvr\/cameras\/[^/]+\/live/, (ws) => {
    ws.onMessage(() => {});
  });
  // Talking: the NAS closes with the reason in `talkClose` once the offer arrives.
  await target.routeWebSocket(/\/api\/v1\/nvr\/cameras\/[^/]+\/talk/, (ws) => {
    ws.onMessage((message) => {
      s.talkOffers = (s.talkOffers ?? 0) + 1;
      if (String(message).includes("webrtc/offer")) ws.close({ code: 4000, reason: s.talkClose });
    });
  });
  await target.routeWebSocket(/\/api\/v1\/nvr\/feed/, (ws) => {
    s.feedSockets.push(ws);
    ws.onMessage(() => {});
    ws.send(JSON.stringify({ t: "hello", running: s.state === "running" }));
    for (const [cam, activity] of Object.entries(s.activity))
      ws.send(JSON.stringify({ t: "activity", camera: cam, ...activity }));
  });
  return s;
}
