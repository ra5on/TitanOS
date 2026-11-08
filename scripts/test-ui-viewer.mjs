// PLAN 63: the remote view ("Fernansicht") of Docker apps – a window shows
// the picture of a mocked viewer (WebSocket routed by Playwright, real video
// frames from the browser's own encoder), sends mouse, keys, paste, quality,
// uploads; volume is remembered per app; loading the viewer program, too
// little memory, host network, the end of a view; the phone full screen and
// the settings page. Isolated data only; nothing reaches a NAS.
//
// The test browser (Playwright's Chromium) has no H.264 decoder, so the mock
// names VP8 in "hello" (the view takes the codec from there); real H.264
// decoding is not covered here. With an H.264-capable browser
// (BROWSER_EXECUTABLE=/path/to/chrome) the same frames are sent as H.264.
//
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5250 node scripts/test-ui-viewer.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { deflateSync } from "node:zlib";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5250";
const out = process.env.UI_OUTPUT || "out/validation/viewer";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
/** Opens (or closes) the ⋯ menu of a window (PLAN 66: the view's controls are there). */
async function more(win, open = true) {
  const button = win.locator('.titlebar button[aria-label="Mehr"]');
  if (((await button.getAttribute("aria-expanded")) === "true") !== open)
    await button.click();
}
const shot = (page, name) => page.screenshot({ path: `${out}/${name}.png` });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Every mocked stream's timer, stopped at the end (a closed context does not always say goodbye).
const timers = new Set();
const W = 640;
const H = 400;

// ---- Real frames, made once by the browser's own encoder ----
async function encodeFrames() {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  // A secure page (localhost) without the app: WebCodecs needs one.
  await page.route(base + "/encoder", (r) =>
    r.fulfill({
      contentType: "text/html",
      body: "<!doctype html><title>enc</title>",
    }),
  );
  await page.goto(base + "/encoder");
  const result = await page.evaluate(
    async ({ W, H }) => {
      const b64 = (u8) => {
        let s = "";
        for (const b of u8) s += String.fromCharCode(b);
        return btoa(s);
      };
      const pick = async () => {
        for (const codec of ["avc1.42E01F", "vp8"]) {
          const config = {
            codec,
            width: W,
            height: H,
            latencyMode: "realtime",
          };
          if (codec.startsWith("avc")) config.avc = { format: "annexb" };
          const enc = await VideoEncoder.isConfigSupported(config);
          const dec = await VideoDecoder.isConfigSupported({ codec });
          if (enc.supported && dec.supported) return config;
        }
        return null;
      };
      const config = await pick();
      if (!config) return null;
      const frames = [];
      const enc = new VideoEncoder({
        output: (chunk) => {
          const a = new Uint8Array(chunk.byteLength);
          chunk.copyTo(a);
          frames.push({
            key: chunk.type === "key",
            ts: chunk.timestamp,
            data: b64(a),
          });
        },
        error: () => {},
      });
      enc.configure(config);
      const canvas = new OffscreenCanvas(W, H);
      const g = canvas.getContext("2d");
      for (let i = 0; i < 30; i++) {
        // A red picture with a moving green square.
        g.fillStyle = "rgb(200, 40, 40)";
        g.fillRect(0, 0, W, H);
        g.fillStyle = "rgb(40, 200, 60)";
        g.fillRect(20 + i * 4, 20, 80, 80);
        const f = new VideoFrame(canvas, { timestamp: i * 33333 });
        enc.encode(f, { keyFrame: i % 15 === 0 });
        f.close();
      }
      await enc.flush();
      let audio = [];
      if (typeof AudioEncoder === "function") {
        const aenc = new AudioEncoder({
          output: (chunk) => {
            const a = new Uint8Array(chunk.byteLength);
            chunk.copyTo(a);
            audio.push({ ts: chunk.timestamp, data: b64(a) });
          },
          error: () => {},
        });
        try {
          aenc.configure({
            codec: "opus",
            sampleRate: 48000,
            numberOfChannels: 2,
          });
          const samples = new Float32Array(48000 * 0.2 * 2).map(
            (_, i) => Math.sin(i / 20) * 0.2,
          );
          aenc.encode(
            new AudioData({
              format: "f32",
              sampleRate: 48000,
              numberOfChannels: 2,
              numberOfFrames: samples.length / 2,
              timestamp: 0,
              data: samples,
            }),
          );
          await aenc.flush();
        } catch {
          audio = [];
        }
      }
      return { codec: config.codec, frames, audio };
    },
    { W, H },
  );
  await ctx.close();
  assert.ok(result, "the test browser can encode and decode VP8 or H.264");
  return result;
}
const media = await encodeFrames();
const toBuf = (b64) => Buffer.from(b64, "base64");
function videoMsg(f) {
  const data = toBuf(f.data);
  const out = Buffer.alloc(10 + data.length);
  out[0] = 1;
  out[1] = f.key ? 1 : 0;
  out.writeBigUInt64BE(BigInt(f.ts), 2);
  data.copy(out, 10);
  return out;
}
function audioMsg(a) {
  const data = toBuf(a.data);
  const out = Buffer.alloc(9 + data.length);
  out[0] = 2;
  out.writeBigUInt64BE(BigInt(a.ts), 1);
  data.copy(out, 9);
  return out;
}

// A solid PNG (w×h, one colour) for the tile mode (PLAN 72).
function png(w, h, [r, g, b]) {
  const crc = (buf) => {
    let c = ~0;
    for (const x of buf) {
      c ^= x;
      for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1));
    }
    return ~c >>> 0;
  };
  const chunk = (type, data) => {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const td = Buffer.concat([Buffer.from(type), data]);
    const c = Buffer.alloc(4);
    c.writeUInt32BE(crc(td));
    return Buffer.concat([len, td, c]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  const row = Buffer.concat([Buffer.from([0]), Buffer.alloc(w * 3)]);
  for (let x = 0; x < w; x++) row.set([r, g, b], 1 + x * 3);
  const raw = Buffer.concat(Array.from({ length: h }, () => row));
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

// 0x04 | codec (2 = PNG) | x y w h | picture.
function tileMsg(x, y, w, h, colour) {
  const head = Buffer.alloc(10);
  head[0] = 4;
  head[1] = 2;
  head.writeUInt16BE(x, 2);
  head.writeUInt16BE(y, 4);
  head.writeUInt16BE(w, 6);
  head.writeUInt16BE(h, 8);
  return Buffer.concat([head, png(w, h, colour)]);
}

// ---- A mocked viewer: what the window sent, and how the viewer behaves ----
function viewer(page, behave) {
  const state = { sent: [], chunks: [], connections: 0, sockets: [] };
  page.routeWebSocket(/\/api\/v1\/viewer\/[^/]+\/stream/, (ws) => {
    state.connections++;
    state.sockets.push(ws);
    const app = decodeURIComponent(ws.url().split("/viewer/")[1].split("/")[0]);
    const mode = behave(app, state.connections);
    // titand accepts every connection, says "starting", and refuses with
    // {"t":"error","code"} and a close 1008 carrying the code.
    ws.send(JSON.stringify({ t: "starting" }));
    const refuse = (code, extra = {}) => {
      ws.send(JSON.stringify({ t: "error", code, text: code, ...extra }));
      ws.close({ code: 1008, reason: code });
    };
    if (typeof mode === "object") return refuse(mode.code, mode.extra);
    let timer = null;
    let i = 0;
    const stream = () => {
      clearInterval(timer);
      timers.delete(timer);
      i = 0;
      timer = setInterval(() => {
        const f = media.frames[i % media.frames.length];
        // Restart at the key frame after the end of the clip.
        ws.send(videoMsg(f));
        if (media.audio[i]) ws.send(audioMsg(media.audio[i]));
        i++;
        if (i >= media.frames.length) i = 0;
      }, 33);
      timers.add(timer);
    };
    // Like the real agent: a hello at its start size first, another after "size".
    ws.send(
      JSON.stringify({
        t: "hello",
        codec: media.codec,
        width: 1280,
        height: 800,
        gpu: "vaapi",
        audio: true,
      }),
    );
    let uploading = null;
    let greeted = false;
    ws.onMessage((m) => {
      if (typeof m !== "string") {
        const b = Buffer.from(m);
        state.chunks.push({
          type: b[0],
          id: b.readUInt32BE(1),
          size: b.length - 5,
        });
        if (uploading) {
          uploading.got += b.length - 5;
          if (uploading.got >= uploading.size)
            ws.send(JSON.stringify({ t: "uploaded", id: uploading.id }));
        }
        return;
      }
      const msg = JSON.parse(m);
      state.sent.push(msg);
      if (msg.t === "size" && !greeted && mode === "tiles") {
        // PLAN 72: the tile mode – hello "tiles", then pictures of areas.
        greeted = true;
        ws.send(
          JSON.stringify({
            t: "hello",
            codec: "tiles",
            width: W,
            height: H,
            gpu: "cpu",
            audio: false,
          }),
        );
        ws.send(JSON.stringify({ t: "mode", mode: "tiles" }));
        ws.send(tileMsg(0, 0, W, H, [210, 30, 30]));
        // Two pictures of the same area: the later one must win.
        ws.send(tileMsg(64, 64, 64, 64, [30, 30, 210]));
        ws.send(tileMsg(64, 64, 64, 64, [30, 200, 60]));
        ws.send(JSON.stringify({ t: "stats", fps: 4, kbps: 12, latencyMs: 3 }));
        return;
      }
      if (msg.t === "size" && !greeted) {
        greeted = true;
        ws.send(
          JSON.stringify({
            t: "hello",
            codec: media.codec,
            width: W,
            height: H,
            gpu: "vaapi",
            audio: true,
          }),
        );
        stream();
        ws.send(
          JSON.stringify({ t: "stats", fps: 60, kbps: 4200, latencyMs: 18 }),
        );
        ws.send(JSON.stringify({ t: "clipboard", text: "Text aus der App" }));
        ws.send(
          JSON.stringify({
            t: "download",
            name: "bild.jpg",
            size: 12345,
            id: "d1",
          }),
        );
        ws.send(JSON.stringify({ t: "cursor", shape: "pointer" }));
      }
      if (msg.t === "keyframe") stream();
      if (msg.t === "upload")
        uploading = { id: msg.id, size: msg.size, got: 0 };
    });
    ws.onClose(() => clearInterval(timer));
    state.bye = (reason) => {
      clearInterval(timer);
      ws.send(JSON.stringify({ t: "bye", reason, text: "" }));
      ws.close();
    };
  });
  return state;
}

const time = "2026-11-06T08:00:00Z";
const ctr = (name, port) => ({
  id: name,
  name,
  image: name + ":latest",
  state: "running",
  since: time,
  ports: [{ host: port, container: port, protocol: "tcp" }],
  app: name,
  service: name,
  managed: true,
  cpuPercent: 1,
  memoryBytes: 200e6,
  cpuHistory: [],
  webui: String(port),
  publicUrl: "",
  proxyUrl: `https://${name}.nas.local`,
  icon: "",
  updateAvailable: false,
});
const containers = [
  ctr("immich", 2283),
  ctr("jellyfin", 8096),
  ctr("paperless", 8000),
];

async function open(width, height, behave, init) {
  const made = await create("owner", "graphit", width);
  await made.page.setViewportSize({ width, height });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  if (init) await made.ctx.addInitScript(init);
  made.data.dockerContainers = containers;
  made.data.dockerApps = containers.map((c) => c.name);
  await made.ctx.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    if (p === "/preferences/desktop" && route.request().method() === "PUT")
      return route.fulfill({ status: 204 });
    if (p === "/apps/mine")
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found" } },
      });
    return route.fallback();
  });
  const v = viewer(made.page, behave);
  await made.page.goto(base + "/");
  await made.page.waitForSelector(".desktop, .home", { timeout: 15000 });
  await made.page.waitForTimeout(600);
  return { ...made, v };
}

async function painted(canvas) {
  await canvas.page().waitForFunction(
    (el) => {
      if (!el || el.width < 2 || getComputedStyle(el).visibility === "hidden")
        return false;
      const [r, g, b, a] = el
        .getContext("2d")
        .getImageData(el.width - 20, el.height - 20, 1, 1).data;
      return a > 0 && r > 150 && g < 100 && b < 100;
    },
    await canvas.elementHandle(),
    { timeout: 10000 },
  );
}
const until = async (fn, what, ms = 5000) => {
  const end = Date.now() + ms;
  while (Date.now() < end) {
    if (fn()) return;
    await sleep(50);
  }
  assert.fail("timed out: " + (typeof what === "function" ? what() : what));
};

// 1. A window with the remote view: picture, bar, input, quality, files, clipboard.
{
  const { ctx, page, v } = await open(1600, 1000, () => "live");
  await page.locator(".ico", { hasText: "immich" }).first().click();
  const win = page.locator("section.win", { hasText: "immich" });
  const canvas = win.locator(".remote canvas");
  await painted(canvas);
  assert.equal(
    await win.locator("iframe").count(),
    0,
    "no frame for a Docker app",
  );
  // PLAN 66 (draft 02): like an app – no address in the title bar, the
  // controls of the view in the window's ⋯ menu.
  assert.equal(
    await win.locator(".titlebar .addr").count(),
    0,
    "no address in the title bar",
  );
  assert.equal(
    await win.locator(".view-bar").count(),
    0,
    "no bar of its own in a window",
  );
  assert.deepEqual(
    await canvas.evaluate((el) => [el.width, el.height]),
    [W, H],
    "the second hello sets the picture size",
  );
  const size = v.sent.find((m) => m.t === "size");
  assert.ok(
    size.width > 300 && size.height > 200 && size.scale >= 1,
    "size sent",
  );
  await more(win);
  await win.locator(".win-menu .pill", { hasText: "Fernansicht" }).waitFor();
  await win
    .locator(".win-menu .stats", { hasText: "60 fps · 18 ms · GPU (VA-API)" })
    .waitFor();
  await shot(page, "01b-menue");
  await more(win, false);
  assert.equal(
    await canvas.evaluate((el) => el.style.cursor),
    "pointer",
    "cursor shape",
  );
  await page.waitForTimeout(600);
  await shot(page, "01-fenster-fernansicht");

  // A click in the middle lands in the middle of the remote screen.
  const box = await canvas.boundingBox();
  v.sent.length = 0;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await until(
    () => v.sent.some((m) => m.t === "mouse" && m.buttons === 1),
    "mouse down",
  );
  const down = v.sent.find((m) => m.t === "mouse" && m.buttons === 1);
  assert.ok(
    Math.abs(down.x - W / 2) <= 2 && Math.abs(down.y - H / 2) <= 2,
    "mapped to remote pixels",
  );
  await until(
    () => v.sent.some((m) => m.t === "mouse" && m.buttons === 0),
    "mouse up",
  );
  // Right click and wheel.
  await page.mouse.click(box.x + 10, box.y + 10, { button: "right" });
  await until(
    () => v.sent.some((m) => m.t === "mouse" && m.buttons === 2),
    "right button",
  );
  await page.mouse.move(box.x + 50, box.y + 50);
  await page.mouse.wheel(0, 120);
  await until(() => v.sent.some((m) => m.t === "wheel" && m.dy > 0), "wheel");

  // Typing: printable keys with their character, function keys by code; both ways.
  v.sent.length = 0;
  await page.keyboard.type("a");
  await page.keyboard.press("Shift+KeyB");
  await page.keyboard.press("Enter");
  await until(() => v.sent.filter((m) => m.t === "key").length >= 6, "keys");
  const keys = v.sent.filter((m) => m.t === "key");
  assert.ok(
    keys.some((k) => k.code === "KeyA" && k.key === "a" && k.down),
    "a down",
  );
  assert.ok(
    keys.some((k) => k.code === "KeyA" && !k.down),
    "a up",
  );
  assert.ok(
    keys.some((k) => k.key === "B" && k.mods.shift),
    "shift+B",
  );
  assert.ok(
    keys.some((k) => k.code === "Enter" && k.down),
    "Enter",
  );
  // A dead key (´ before e) is not sent; the finished character is.
  v.sent.length = 0;
  await canvas.evaluate((el) => {
    for (const [key, code] of [
      ["Dead", "Equal"],
      ["é", "KeyE"],
    ])
      el.dispatchEvent(
        new KeyboardEvent("keydown", { key, code, bubbles: true }),
      );
  });
  await until(() => v.sent.some((m) => m.t === "key" && m.key === "é"), "é");
  assert.ok(
    !v.sent.some((m) => m.t === "key" && m.key === "Dead"),
    "dead key stays home",
  );
  // The desktop's own shortcut stays on the desktop: Alt + Q opens the switcher.
  v.sent.length = 0;
  await page.keyboard.down("Alt");
  await page.keyboard.press("q");
  await page.waitForTimeout(150);
  assert.ok(
    !v.sent.some((m) => m.t === "key" && m.code === "KeyQ"),
    "Alt+Q not sent",
  );
  await page.keyboard.up("Alt");
  await page.waitForTimeout(200);
  await canvas.click({ position: { x: 30, y: 30 } });
  // Paste: the text goes over as the clipboard.
  v.sent.length = 0;
  await canvas.evaluate((el) => {
    const dt = new DataTransfer();
    dt.setData("text/plain", "Hallo NAS");
    el.dispatchEvent(
      new ClipboardEvent("paste", { clipboardData: dt, bubbles: true }),
    );
  });
  await until(
    () => v.sent.some((m) => m.t === "clipboard" && m.text === "Hallo NAS"),
    "paste",
  );

  // Quality.
  await more(win);
  await win.getByRole("menuitem", { name: /Qualität/ }).click();
  await page.getByRole("menuitemradio", { name: "Sparsam" }).click();
  await until(
    () =>
      // 30 fps unless chosen otherwise (0.66.0, owner 7.11.2026).
      v.sent.some((m) => m.t === "quality" && m.mode === "low" && m.fps === 30),
    "quality",
  );
  await win.getByRole("menuitem", { name: /Qualität/ }).click();
  await page.getByRole("menuitemradio", { name: "60 fps" }).click();
  await until(
    () =>
      v.sent.some((m) => m.t === "quality" && m.mode === "low" && m.fps === 60),
    "fps",
  );

  // Clipboard of the app, and sending text into it.
  await win.getByRole("menuitem", { name: "Zwischenablage" }).click();
  await page
    .locator(".win-menu .remote-clip", { hasText: "Text aus der App" })
    .waitFor();
  await page.getByLabel("Text für die App").fill("Notiz");
  await page.getByRole("button", { name: "In die App legen" }).click();
  await until(
    () => v.sent.some((m) => m.t === "clipboard" && m.text === "Notiz"),
    "clipboard send",
  );
  await shot(page, "02-zwischenablage");
  await win.getByRole("menuitem", { name: "Zwischenablage" }).click();

  // Files: upload in 256 KiB pieces with progress, downloads as links.
  await win.getByRole("menuitem", { name: /Heruntergeladen/ }).click();
  const link = page.locator(".win-menu a", { hasText: "bild.jpg" });
  assert.equal(
    await link.getAttribute("href"),
    "/api/v1/viewer/immich/downloads/d1",
  );
  const bytes = 256 * 1024 * 2 + 1000;
  await win.locator('input[type="file"]').setInputFiles({
    name: "bericht.pdf",
    mimeType: "application/pdf",
    buffer: Buffer.alloc(bytes, 7),
  });
  await until(
    () =>
      v.sent.some(
        (m) => m.t === "upload" && m.name === "bericht.pdf" && m.size === bytes,
      ),
    "upload",
  );
  await until(() => v.chunks.length === 3, "three pieces");
  assert.deepEqual(
    v.chunks.map((c) => [c.type, c.size]),
    [
      [3, 262144],
      [3, 262144],
      [3, 1000],
    ],
  );
  const id = v.sent.find((m) => m.t === "upload").id;
  assert.ok(
    v.chunks.every((c) => c.id === id),
    "pieces carry the upload number",
  );
  await page
    .locator(".win-menu li", { hasText: "bericht.pdf" })
    .getByText("hochgeladen")
    .waitFor();
  await shot(page, "03-dateien");
  await win.getByRole("menuitem", { name: /Heruntergeladen/ }).click();

  // Volume: remembered per app in this browser.
  const slider = win.getByRole("slider", { name: "Lautstärke" });
  await slider.fill("30");
  await win.locator(".win-menu .pct", { hasText: "30 %" }).waitFor();
  const stored = await page.evaluate(() =>
    JSON.parse(localStorage.getItem("titanos.viewer.immich")),
  );
  assert.equal(stored.volume, 0.3);
  assert.equal(stored.quality, "low");
  await win.getByRole("button", { name: "Ton aus" }).click();
  assert.equal(
    (
      await page.evaluate(() =>
        JSON.parse(localStorage.getItem("titanos.viewer.immich")),
      )
    ).muted,
    true,
  );
  await win.getByRole("button", { name: "Ton an" }).click();
  await more(win, false);

  // Key frame after a decoder error is asked for by the view; the mock answers a request.
  // The end of the view: "Neu verbinden" starts it again.
  v.bye("idle");
  await win.getByText("Getrennt, weil eine Weile keine Eingabe kam.").waitFor();
  await more(win);
  await win.locator(".win-menu .pill", { hasText: "beendet" }).waitFor();
  await more(win, false);
  await shot(page, "04-beendet");
  await win.getByRole("button", { name: "Neu verbinden" }).click();
  await painted(canvas);
  assert.equal(v.connections, 2);
  // On reconnect, the chosen quality goes along.
  assert.ok(v.sent.filter((m) => m.t === "quality").length >= 3);

  // A network drop: one new attempt by itself.
  await v.sockets.at(-1).close({ code: 1006, reason: "" });
  await until(() => v.connections === 3, "reconnected once", 6000);
  await painted(canvas);

  // Reload: the volume is still 30 %.
  await page.reload();
  await page.waitForSelector(".desktop");
  const win2 = page.locator("section.win", { hasText: "immich" });
  if (!(await win2.count()))
    await page.locator(".ico", { hasText: "immich" }).first().click();
  await more(win2);
  await win2.locator(".win-menu .pct", { hasText: "30 %" }).waitFor();
  await ctx.close();
}

// 2. Refusals: too little memory, host network, a stopped app, loading the viewer program.
{
  let imageReady = false;
  let paperlessRuns = false;
  const loading = {
    state: "downloading",
    bytes: 120e6,
    size: 400e6,
    progress: 0.3,
  };
  const holder = {};
  const { ctx, page, data, v } = await open(1600, 1000, (app) => {
    const data = holder.data;
    if (app === "jellyfin")
      return {
        code: "viewer_memory",
        extra: {
          memory: { availableBytes: 5e8, perViewBytes: 419430400, more: 0 },
        },
      };
    if (app === "paperless")
      return paperlessRuns
        ? { code: "viewer_hostnet" }
        : { code: "viewer_not_running" };
    if (imageReady) return "live";
    return { code: "viewer_image", extra: { image: data.viewer.status.image } };
  });
  holder.data = data;
  data.viewer.status = { ...data.viewer.status, image: loading };
  await page.locator(".ico", { hasText: "jellyfin" }).first().click();
  const jelly = page.locator("section.win", { hasText: "jellyfin" });
  await jelly
    .getByRole("heading", { name: "Nicht genug Arbeitsspeicher" })
    .waitFor();
  await jelly
    .getByRole("button", { name: "In eigenem Tab öffnen" })
    .first()
    .waitFor();
  await shot(page, "05-zu-wenig-speicher");
  await jelly.getByRole("button", { name: "Schließen" }).click();

  await page.locator(".ico", { hasText: "paperless" }).first().click();
  const paper = page.locator("section.win", { hasText: "paperless" });
  await paper.getByText("Die App läuft nicht.", { exact: false }).waitFor();
  paperlessRuns = true;
  await paper.getByRole("button", { name: "Neu verbinden" }).click();
  await paper.getByRole("heading", { name: "App im Netz des NAS" }).waitFor();
  await paper.getByRole("button", { name: "Schließen" }).click();

  data.viewer.status = { ...data.viewer.status, image: loading };
  await page.locator(".ico", { hasText: "immich" }).first().click();
  const imm = page.locator("section.win", { hasText: "immich" });
  await imm
    .getByRole("heading", { name: "Fernansicht wird eingerichtet" })
    .waitFor();
  await imm.getByText("120 MB von 400 MB").waitFor();
  await shot(page, "06-abbild-laedt");
  data.viewer.status = {
    ...data.viewer.status,
    image: { ...loading, state: "loading", bytes: 400e6, progress: 1 },
  };
  await imm.getByText("400 MB von 400 MB").waitFor();
  data.viewer.status = { ...data.viewer.status, image: { state: "ready" } };
  imageReady = true;
  await painted(imm.locator(".remote canvas"));
  await imm.getByRole("button", { name: "Schließen" }).click();
  // A failed download: the error and "retry", no reconnecting in a loop.
  imageReady = false;
  const failed = { state: "error", error: "Signatur ungültig" };
  data.viewer.status = { ...data.viewer.status, image: failed };
  const before = v.connections;
  await page.locator(".ico", { hasText: "immich" }).first().click();
  await imm
    .getByText("Das Laden ist fehlgeschlagen: Signatur ungültig")
    .waitFor();
  await page.waitForTimeout(2500);
  assert.equal(
    v.connections,
    before + 1,
    "no reconnect loop after a failed download",
  );
  await ctx.close();
}

// 3. Phone: full screen with its own bar, tap = click, keyboard, back to the grid.
{
  const { ctx, page, v } = await open(390, 844, () => "live");
  await page.locator(".home .app", { hasText: "immich" }).first().click();
  const view = page.locator(".remote-full .remote.phone");
  await painted(view.locator("canvas"));
  await view.locator(".phone-head .pill", { hasText: "Fernansicht" }).waitFor();
  await shot(page, "07-handy-fernansicht");
  const box = await view.locator("canvas").boundingBox();
  v.sent.length = 0;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await until(
    () => v.sent.some((m) => m.t === "mouse" && m.buttons === 1),
    "tap",
  );
  // Real touch input (pointerType "touch"): tap = click, long press = right
  // click, swipe = wheel.
  const cdp = await ctx.newCDPSession(page);
  await cdp.send("Emulation.setTouchEmulationEnabled", {
    enabled: true,
    maxTouchPoints: 5,
  });
  const touch = (type, points) =>
    cdp.send("Input.dispatchTouchEvent", {
      type,
      touchPoints: points.map(([x, y]) => ({ x, y })),
    });
  const cx = box.x + box.width / 2;
  const cy = box.y + box.height / 2;
  v.sent.length = 0;
  await touch("touchStart", [[cx, cy]]);
  await touch("touchEnd", []);
  await until(
    () =>
      v.sent
        .filter((m) => m.t === "mouse")
        .map((m) => m.buttons)
        .join() === "0,1,0",
    () => "touch tap = click: " + JSON.stringify(v.sent),
  );
  v.sent.length = 0;
  await touch("touchStart", [[cx - 30, cy]]);
  await page.waitForTimeout(800);
  await touch("touchEnd", []);
  await until(
    () => v.sent.some((m) => m.t === "mouse" && m.buttons === 2),
    "long press = right click",
  );
  assert.ok(
    !v.sent.some((m) => m.t === "mouse" && m.buttons === 1),
    "no left click on long press",
  );
  v.sent.length = 0;
  await touch("touchStart", [[cx, cy + 40]]);
  for (let i = 1; i <= 5; i++)
    await touch("touchMove", [[cx, cy + 40 - i * 15]]);
  await touch("touchEnd", []);
  await until(
    () => v.sent.some((m) => m.t === "wheel" && m.dy > 0),
    "swipe up = wheel down",
  );
  assert.ok(
    !v.sent.some((m) => m.t === "mouse" && m.buttons === 1),
    "a swipe is no click",
  );
  await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: false });
  await view
    .locator(".chips")
    .getByRole("button", { name: "Tastatur" })
    .click();
  await page.keyboard.type("Hi");
  await until(
    () =>
      v.sent
        .filter((m) => m.t === "text")
        .map((m) => m.text)
        .join("") === "Hi",
    "text from the keyboard",
  );
  await page.keyboard.press("Backspace");
  await until(
    () => v.sent.some((m) => m.t === "key" && m.code === "Backspace" && m.down),
    "backspace",
  );
  await view.getByRole("button", { name: /Lautstärke/ }).click();
  await view
    .locator(".volume-sheet")
    .getByRole("slider", { name: "Lautstärke" })
    .waitFor();
  await shot(page, "08-handy-lautstaerke");
  await view.getByRole("button", { name: "Zum Startbildschirm" }).click();
  await page.locator(".home").waitFor();
  assert.equal(
    await page.locator(".remote-full").count(),
    0,
    "back on the grid",
  );
  await ctx.close();
}

// 4. Systemsteuerung › Fernansicht.
{
  const { ctx, page, data } = await create("owner", "graphit", 1440);
  await page.goto(base + "/settings/viewer");
  await page.getByRole("heading", { name: "Fernansicht", level: 1 }).waitFor();
  const gpu = page.getByLabel("Grafikkarte");
  assert.equal(await gpu.inputValue(), "auto");
  assert.ok(
    (await gpu.locator("option").allTextContents()).some((o) =>
      o.includes("Automatisch – Intel UHD 770"),
    ),
  );
  await page.getByText("jetzt 1 offen, Platz für etwa 9 weitere").waitFor();
  await page.getByText("photos · alex · offen").waitFor();
  await page.getByText("Neue Ansichten kodieren mit: GPU (VA-API)").waitFor();
  await page.getByText("wie Konsolen: 15 Minuten").waitFor();
  await page.getByText("Geladen (Version 0.60.0)").waitFor();
  await shot(page, "09-einstellungen");
  await gpu.selectOption("/dev/dri/renderD128");
  await page.getByRole("radio", { name: "Ausgewogen" }).click();
  await page.getByRole("radio", { name: "30 fps" }).click();
  // PLAN 72: the transport, tiles by default.
  await page.getByRole("radio", { name: "Immer Video" }).click();
  await page.getByLabel("Ton übertragen").uncheck();
  await page.getByLabel("Profile der Apps").selectOption("Data");
  await page.getByRole("button", { name: "Speichern" }).click();
  await page.getByText("Gespeichert").waitFor();
  assert.deepEqual(data.viewerPuts.at(-1), {
    gpu: "/dev/dri/renderD128",
    quality: "balanced",
    fps: 30,
    transport: "video",
    audio: false,
    volume: "Data",
  });
  await ctx.close();
}
{
  // On the phone, and not loaded yet: "Jetzt laden".
  const { ctx, page, data } = await create("owner", "graphit", 390);
  await page.setViewportSize({ width: 390, height: 844 });
  data.viewer.status = {
    ...data.viewer.status,
    image: { state: "missing" },
  };
  await page.goto(base + "/settings/viewer");
  await page.getByRole("button", { name: "Jetzt laden" }).waitFor();
  await page.waitForTimeout(300);
  await shot(page, "10-einstellungen-handy");
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth + 1,
  );
  assert.equal(overflow, false, "no sideways scrolling at 390 px");
  await page.getByRole("button", { name: "Jetzt laden" }).click();
  await page.waitForTimeout(300);
  assert.ok(
    data.requests.some(([m, p]) => m === "POST" && p === "/viewer/image"),
    "load now asks the NAS",
  );
  await page.getByText("Wird heruntergeladen").waitFor();
  await ctx.close();
}

// PLAN 72: the tile mode – pictures of areas drawn where they belong, the
// later picture of an area over the earlier one, "Kacheln" in the menu.
{
  const { ctx, page, v } = await open(1600, 1000, () => "tiles");
  await page.locator(".ico", { hasText: "immich" }).first().click();
  const win = page.locator("section.win", { hasText: "immich" });
  const canvas = win.locator(".remote canvas");
  await painted(canvas);
  await page.waitForFunction(
    (el) => {
      const [r, g, b] = el.getContext("2d").getImageData(90, 90, 1, 1).data;
      return g > 150 && r < 100 && b < 100;
    },
    await canvas.elementHandle(),
    { timeout: 5000 },
  );
  const [r, g, b] = await canvas.evaluate((el) => [
    ...el.getContext("2d").getImageData(10, 10, 1, 1).data,
  ]);
  assert.ok(
    r > 150 && g < 100 && b < 100,
    `red outside the tile: ${r} ${g} ${b}`,
  );
  await more(win);
  await win.locator(".win-menu", { hasText: "Kacheln" }).first().waitFor();
  await more(win, false);
  // PLAN 66 draft 04: a file dragged into the window is uploaded to the app.
  const drop = await page.evaluateHandle(() => {
    const dt = new DataTransfer();
    dt.items.add(new File(["Hallo"], "notiz.txt", { type: "text/plain" }));
    return dt;
  });
  const remote = win.locator(".remote");
  await remote.dispatchEvent("dragover", { dataTransfer: drop });
  await win.locator(".drop-hint").waitFor();
  await remote.dispatchEvent("drop", { dataTransfer: drop });
  await win.getByText("unter „Hochgeladen“", { exact: false }).waitFor();
  await until(
    () => v.sent.some((m) => m.t === "upload" && m.name === "notiz.txt" && m.size === 5),
    "upload of the dropped file",
  );
  await ctx.close();
}

for (const t of timers) clearInterval(t);
await browser.close();
assert.deepEqual(errors, [], "no script errors");
console.log(
  `viewer UI ok (video codec in the test: ${media.codec}, audio packets: ${media.audio.length})`,
);
