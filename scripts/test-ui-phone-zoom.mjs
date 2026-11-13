// PLAN 87: zooming with two fingers on the phone – a picture in the file
// preview, the first page of a PDF (phones show no PDF in a page), and the
// NVR's large camera, where a swipe to the side shows the next camera
// (not while zoomed). A double tap zooms back. Phone emulation (isMobile,
// touch, 3x) at 390×844 with CDP two-finger touch events; isolated data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5267 node scripts/test-ui-phone-zoom.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { deflateSync } from "node:zlib";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
import { installNvr, nvrState } from "./test-ui-nvr-fixtures.mjs";

const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5267";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const out = process.env.UI_OUTPUT || "out/validation/phone-zoom";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const create = fixtureFactory(browser, errors);
const W = 390,
  H = 844;

// A test picture as PNG (blue with a yellow square), made here.
function png(w, h) {
  const crcTable = Array.from({ length: 256 }, (_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });
  const crc = (buf) => {
    let c = 0xffffffff;
    for (const b of buf) c = crcTable[(c ^ b) & 0xff] ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  };
  const chunk = (type, data) => {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const body = Buffer.concat([Buffer.from(type), data]);
    const sum = Buffer.alloc(4);
    sum.writeUInt32BE(crc(body));
    return Buffer.concat([len, body, sum]);
  };
  const raw = Buffer.alloc((w * 3 + 1) * h);
  for (let y = 0; y < h; y++)
    for (let x = 0; x < w; x++) {
      const inner =
        x > w / 3 && x < (2 * w) / 3 && y > h / 3 && y < (2 * h) / 3;
      const i = y * (w * 3 + 1) + 1 + x * 3;
      raw.set(inner ? [245, 197, 66] : [45, 95, 138], i);
    }
  const head = Buffer.alloc(13);
  head.writeUInt32BE(w, 0);
  head.writeUInt32BE(h, 4);
  head.set([8, 2, 0, 0, 0], 8);
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk("IHDR", head),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}
const picture = png(320, 240);

async function phone() {
  const made = await create("owner", "graphit", W, "de", {
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 3,
    viewport: { width: W, height: H },
  });
  const { ctx, page } = made;
  await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  made.data.prefs.desktop = { version: 1 };
  await ctx.route("**/api/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname.slice(7);
    if (path === "/files/list")
      return route.fulfill({
        json: [
          {
            name: "Garten.png",
            dir: false,
            size: 120000,
            modified: "2026-11-12T10:00:00Z",
          },
          {
            name: "Plan.pdf",
            dir: false,
            size: 90000,
            modified: "2026-11-12T11:00:00Z",
          },
        ],
      });
    if (path === "/files/download" || path === "/files/thumb")
      return route.fulfill({ body: picture, contentType: "image/png" });
    return route.fallback();
  });
  const cdp = await ctx.newCDPSession(page);
  return { ...made, cdp };
}
async function touch(cdp, type, points) {
  await cdp.send("Input.dispatchTouchEvent", {
    type,
    touchPoints: points.map(([x, y], id) => ({ x, y, id })),
  });
}
/** Two fingers from `gap` apart to `to` apart around the middle of `box`. */
async function pinch(page, cdp, box, gap, to) {
  const cx = box.x + box.width / 2,
    cy = box.y + box.height / 2;
  await touch(cdp, "touchStart", [
    [cx - gap / 2, cy],
    [cx + gap / 2, cy],
  ]);
  for (let i = 1; i <= 8; i++) {
    const g = gap + ((to - gap) * i) / 8;
    await touch(cdp, "touchMove", [
      [cx - g / 2, cy + 1],
      [cx + g / 2, cy + 1],
    ]);
  }
  await touch(cdp, "touchEnd", []);
  await page.waitForTimeout(150);
}
const scaleOf = (locator) =>
  locator.evaluate((el) => {
    const m = getComputedStyle(el).transform;
    return m === "none" ? 1 : new DOMMatrix(m).a;
  });
async function doubleTap(page, box) {
  const x = box.x + box.width / 2,
    y = box.y + box.height / 2;
  await page.touchscreen.tap(x, y);
  await page.waitForTimeout(80);
  await page.touchscreen.tap(x, y);
  await page.waitForTimeout(200);
}

try {
  // A picture in the preview.
  {
    const { ctx, page, cdp } = await phone();
    await page.goto(base + "/files?root=share%2FDocuments");
    await page
      .locator("[data-file-name='Garten.png']")
      .waitFor({ timeout: 15000 });
    const preview = async (name) => {
      await page.locator(`[data-file-name='${name}'] .entry-more`).tap();
      await page.getByRole("menuitem", { name: /Vorschau/ }).tap();
    };
    await preview("Garten.png");
    const zoom = page.locator("dialog[open] [data-pinch]");
    await zoom.waitFor({ timeout: 10000 });
    await page.waitForFunction(
      () =>
        document.querySelector("dialog[open] [data-pinch] img")?.naturalWidth >
        0,
    );
    await page.waitForTimeout(400);
    const img = zoom.locator("img");
    const box = await zoom.boundingBox();
    await pinch(page, cdp, box, 60, 240);
    const s = await scaleOf(img);
    assert.ok(s > 2, `two fingers zoom the picture (${s})`);
    assert.equal(await zoom.getAttribute("data-zoomed"), "true");
    await page.screenshot({ path: `${out}/vorschau-gezoomt.png` });
    await doubleTap(page, box);
    assert.equal(await scaleOf(img), 1, "a double tap zooms back");
    await page.keyboard.press("Escape");
    // The first page of a PDF as a picture.
    await page
      .locator("dialog[open]")
      .waitFor({ state: "detached" })
      .catch(() => {});
    await preview("Plan.pdf");
    const pdf = page.locator("dialog[open] [data-pinch]");
    await pdf.locator(".pdf-page").waitFor({ timeout: 10000 });
    await page.waitForFunction(
      () => document.querySelector("dialog[open] .pdf-page")?.naturalWidth > 0,
    );
    await page.waitForTimeout(400);
    await pinch(page, cdp, await pdf.boundingBox(), 60, 200);
    assert.ok((await scaleOf(pdf.locator("img"))) > 1.5, "the PDF page zooms");
    await ctx.close();
  }

  // The NVR's large camera: zoom, and a swipe shows the next camera.
  {
    const { ctx, page, cdp, data } = await phone();
    const state = nvrState();
    state.admin = true;
    await installNvr(ctx, state);
    void data;
    await page.goto(base + "/nvr?camera=haustuer");
    const solo = page.locator(".tile.solo[data-pinch]");
    await solo.waitFor({ timeout: 15000 });
    await page.waitForTimeout(600);
    const first = await solo.getAttribute("data-solo");
    const box = await solo.boundingBox();
    // Swipe to the side: the next camera.
    await touch(cdp, "touchStart", [
      [box.x + box.width - 40, box.y + box.height / 2],
    ]);
    for (let i = 1; i <= 8; i++)
      await touch(cdp, "touchMove", [
        [box.x + box.width - 40 - i * 30, box.y + box.height / 2 + 2],
      ]);
    await touch(cdp, "touchEnd", []);
    await page.waitForTimeout(500);
    const second = await page.locator(".tile.solo").getAttribute("data-solo");
    assert.notEqual(
      second,
      first,
      `swiped to the next camera (${first} → ${second})`,
    );
    // Zoomed, a swipe moves the picture and keeps the camera.
    const now = page.locator(".tile.solo[data-pinch]");
    const nb = await now.boundingBox();
    await pinch(page, cdp, nb, 50, 200);
    assert.equal(
      await now.getAttribute("data-zoomed"),
      "true",
      "the live picture zooms",
    );
    await page.screenshot({ path: `${out}/nvr-gezoomt.png` });
    await touch(cdp, "touchStart", [
      [nb.x + nb.width - 40, nb.y + nb.height / 2],
    ]);
    for (let i = 1; i <= 8; i++)
      await touch(cdp, "touchMove", [
        [nb.x + nb.width - 40 - i * 30, nb.y + nb.height / 2],
      ]);
    await touch(cdp, "touchEnd", []);
    await page.waitForTimeout(400);
    assert.equal(
      await page.locator(".tile.solo").getAttribute("data-solo"),
      second,
      "zoomed: no change of camera",
    );
    await ctx.close();
  }
} catch (err) {
  console.error(err);
  process.exitCode = 1;
}
await browser.close();
const relevant = errors.filter(
  (e) => !/Failed to load resource|net::ERR|WebSocket|webrtc|go2rtc/i.test(e),
);
assert.deepEqual(relevant, [], "no script errors");
if (!process.exitCode)
  console.log(
    "PASS phone zoom: preview picture, PDF first page, NVR large camera with swipe",
  );
