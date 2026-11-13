// The live view with real video (owner 13.11.2026: "the video refreshes
// every few seconds and goes black for a moment"). Every camera's live
// WebSocket streams a fragmented MP4 at its real pace, as go2rtc's MSE does;
// the page polls the NVR as always. Checked over LIVE_SECONDS (default 50):
//
// - each tile opens its live socket once: the status poll (every 6 s) must
//   not start the stream anew (it did: new camera objects restarted it);
// - from the first picture on, no tile is ever empty or dark and the video
//   never stands still for 2 s: old video is removed far enough behind the
//   picture that the key frame of the picture on screen stays (with a key
//   frame every 6 s the picture stopped again and again);
// - a stream that breaks off keeps the last picture until the video is back
//   and comes back by itself.
//
// The test browser has no H.264, so the stream is VP9:
// scripts/test-ui-media/nvr-live-vp9.mp4, 12 s of ffmpeg's testsrc2, 320×180,
// 15 frames/s, a key frame every 6 s, one fragment per frame, made with the
// ffmpeg of the pinned Frigate image:
//   ffmpeg -f lavfi -i testsrc2=size=320x180:rate=15 -t 12 -c:v libvpx-vp9
//     -deadline realtime -cpu-used 8 -b:v 60k -g 90 -keyint_min 90
//     -pix_fmt yuv420p -f mp4
//     -movflags frag_every_frame+empty_moov+default_base_moof nvr-live-vp9.mp4
// It plays in a loop; each round's decode times are moved on.
//
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5262 node scripts/test-ui-nvr-live.mjs
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
import { installNvr, nvrState, pinAfternoon } from "./test-ui-nvr-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

// ---- The stream: init segment and one fragment (moof + mdat) per frame ----
const file = await readFile(new URL("./test-ui-media/nvr-live-vp9.mp4", import.meta.url));
const boxes = [];
for (let at = 0; at < file.length; ) {
  const size = file.readUInt32BE(at);
  boxes.push({ type: file.toString("latin1", at + 4, at + 8), data: file.subarray(at, at + size) });
  at += size;
}
const firstMoof = boxes.findIndex((b) => b.type === "moof");
const init = Buffer.concat(boxes.slice(0, firstMoof).map((b) => b.data));
const frames = [];
for (let i = firstMoof; i < boxes.length; i++) {
  if (boxes[i].type === "moof") frames.push(Buffer.concat([boxes[i].data, boxes[i + 1].data]));
}
/** Where the 64-bit decode time (tfdt, version 1) of a fragment lies. */
function tfdtAt(frame) {
  for (let at = frame.indexOf("tfdt", 0, "latin1"); at > 0; ) {
    assert.equal(frame[at + 4], 1, "tfdt version 1");
    return at + 8;
  }
  throw new Error("fragment without tfdt");
}
const firstTime = frames[0].readBigUInt64BE(tfdtAt(frames[0]));
const lastTime = frames.at(-1).readBigUInt64BE(tfdtAt(frames.at(-1)));
const step = (lastTime - firstTime) / BigInt(frames.length - 1);
const round = lastTime - firstTime + step;
const FPS = 15;
/** Frame n of the endless stream: a copy with its decode time moved on. */
function frameAt(n) {
  const frame = Buffer.from(frames[n % frames.length]);
  const at = tfdtAt(frame);
  frame.writeBigUInt64BE(frame.readBigUInt64BE(at) + round * BigInt(Math.floor(n / frames.length)), at);
  return frame;
}

pinAfternoon();
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5262";
const seconds = Number(process.env.LIVE_SECONDS || 50);
const errors = [];
const create = fixtureFactory(browser, errors);
/** The streams being sent; stopped at the end even if a socket never said it closed. */
const senders = new Set();

try {
  const made = await create("owner", "hell", 1280, "de", { deviceScaleFactor: 1 });
  const state = nvrState();
  state.admin = true;
  await installNvr(made.ctx, state);
  const { page } = made;

  const opens = {};
  const sockets = {};
  await page.routeWebSocket(/\/api\/v1\/nvr\/cameras\/[^/]+\/live/, (ws) => {
    const camera = decodeURIComponent(new URL(ws.url()).pathname.split("/").at(-2));
    opens[camera] = (opens[camera] ?? 0) + 1;
    sockets[camera] = ws;
    let timer;
    ws.onMessage((message) => {
      if (timer || !String(message).includes('"mse"')) return;
      ws.send(JSON.stringify({ type: "mse", value: 'video/mp4; codecs="vp09.00.10.08"' }));
      ws.send(init);
      let n = 0;
      timer = setInterval(() => {
        try {
          ws.send(frameAt(n++));
        } catch {
          clearInterval(timer);
        }
      }, 1000 / FPS);
      senders.add(timer);
    });
    ws.onClose(() => clearInterval(timer));
  });

  await page.goto(`${base}/nvr`);
  const cameras = state.cameras.filter((c) => !c.deleted && c.enabled).map((c) => c.id);
  // One camera's stream breaks off a third of the way in.
  const broken = cameras[0];
  const polls = () => state.requests.filter(([m, p]) => m === "GET" && p === "/nvr/status").length;
  const pollsBefore = polls();
  const samples = [];
  const started = Date.now();
  let dropped = false;
  while (Date.now() - started < seconds * 1000) {
    if (!dropped && Date.now() - started > (seconds * 1000) / 3 && sockets[broken]) {
      dropped = true;
      // Like a dropped connection of the NAS (go2rtc lost the camera).
      void sockets[broken].close({ code: 1011, reason: "" }).catch(() => {});
    }
    samples.push(
      await page.evaluate(() =>
        Object.fromEntries(
          [...document.querySelectorAll(".tile-view")].map((tile) => {
            const box = tile.querySelector(".live-video");
            const video = box?.querySelector("video");
            const img = box?.querySelector("img");
            const visible = (el) => !!el && getComputedStyle(el).visibility !== "hidden";
            let light = null;
            if (visible(video) && video.readyState >= 2) {
              const canvas = document.createElement("canvas");
              canvas.width = 32;
              canvas.height = 18;
              const g = canvas.getContext("2d");
              g.drawImage(video, 0, 0, 32, 18);
              const d = g.getImageData(0, 0, 32, 18).data;
              let sum = 0;
              for (let i = 0; i < d.length; i += 4) sum += d[i] + d[i + 1] + d[i + 2];
              light = sum / (d.length / 4) / 3;
            }
            return [
              tile.dataset.camera,
              {
                video: visible(video),
                img: visible(img),
                ready: video?.readyState ?? -1,
                time: video ? video.currentTime : null,
                light,
              },
            ];
          }),
        ),
      ),
    );
    await page.waitForTimeout(250);
  }

  assert.ok(polls() - pollsBefore >= Math.floor(seconds / 6) - 1, `the page polled the NVR (${polls() - pollsBefore} times)`);
  for (const camera of cameras) {
    const seq = samples.map((s) => s[camera]).filter(Boolean);
    const first = seq.findIndex((x) => x.video && x.ready >= 2);
    assert.ok(first >= 0 && first < 20, `${camera}: video within 5 s (sample ${first})`);
    const after = seq.slice(first);
    const empty = after.filter((x) => !x.video && !x.img).length;
    const dark = after.filter((x) => x.video && (x.ready < 2 || (x.light !== null && x.light < 12))).length;
    assert.equal(empty, 0, `${camera}: never an empty tile after the first picture`);
    assert.equal(dark, 0, `${camera}: never a dark or undecoded picture`);
    let stood = 0;
    for (let i = 8; i < after.length; i++) {
      const window = after.slice(i - 8, i + 1);
      if (window.every((x) => x.video) && after[i].time === after[i - 8].time) stood++;
    }
    assert.equal(stood, 0, `${camera}: the video never stands still for 2 s`);
    assert.equal(opens[camera], camera === broken ? 2 : 1, `${camera}: live socket opened ${opens[camera]} times`);
    const end = seq.at(-1);
    assert.ok(end.video, `${camera}: video at the end`);
    if (camera !== broken) assert.ok(end.time > seconds - 10, `${camera}: played on (${end.time.toFixed(1)} s)`);
  }
  assert.deepEqual(errors, [], "no page errors");
  console.log(`NVR live: ${cameras.length} cameras, ${seconds} s of video, ${polls() - pollsBefore} polls, one break-off – ok`);
} finally {
  for (const timer of senders) clearInterval(timer);
  await browser.close();
}
