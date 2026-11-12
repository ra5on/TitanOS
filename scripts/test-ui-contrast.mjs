// PLAN 65-5: text contrast of the desktop in every theme, at the standard
// transparency and at its maximum. The background under each text is read
// from a screenshot with the text made invisible (glass lies over wallpaper,
// so computed styles cannot tell it); the text color comes from its style.
// Normal text needs 4.5:1, large text (≥ 24 px, or ≥ 18.7 px bold) 3:1.
// Labels of desktop icons lie on the wallpaper itself; they are listed apart
// (their text shadow is what keeps them readable) and do not fail the run.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5240 node scripts/test-ui-contrast.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5240";
const out = process.env.UI_OUTPUT || "out/validation/contrast";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const themes = (
  process.env.CONTRAST_THEMES || "graphit,schwarz,mitternacht,hell"
)
  .split(",")
  .filter(Boolean);
const levels = [45, 90]; // standard and highest "Transparenz"

/** Marks the visible texts of the page (or a frame) and returns them. */
function collect({ offset, prefix }) {
  const out = [];
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  const seen = new Set();
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    if (!n.textContent.trim()) continue;
    const el = n.parentElement;
    if (!el || seen.has(el)) continue;
    seen.add(el);
    if (el.closest("script, style, .sr-only, [aria-hidden=true], option, svg"))
      continue;
    const s = getComputedStyle(el);
    if (s.visibility === "hidden" || +s.opacity === 0) continue;
    const range = document.createRange();
    range.selectNodeContents(n);
    const r = range.getBoundingClientRect();
    if (r.width < 4 || r.height < 6) continue;
    if (
      r.bottom < 0 ||
      r.right < 0 ||
      r.top > innerHeight ||
      r.left > innerWidth
    )
      continue;
    // Hidden behind something else (a window over the desktop): skip.
    const hit = document.elementFromPoint(
      r.left + r.width / 2,
      r.top + r.height / 2,
    );
    if (!hit || !(hit === el || el.contains(hit) || hit.contains(el))) continue;
    // Faded on purpose (disabled controls) do not count.
    if (el.closest(":disabled, [aria-disabled=true]")) continue;
    const id = prefix + out.length;
    el.dataset.cx = id;
    out.push({
      id,
      text: n.textContent.trim().slice(0, 40),
      where:
        el
          .closest(
            "section.win, .launcher, nav.dock, .widget, [role=menu], header.topbar, .icons",
          )
          ?.className.split(" ")[0] || el.tagName,
      wallpaper: !!el.closest(".icons .label, .icons > .ico"),
      color: s.color,
      size: parseFloat(s.fontSize),
      bold: +s.fontWeight >= 700,
      x: r.left + offset.x,
      y: r.top + offset.y,
      w: r.width,
      h: r.height,
    });
  }
  return out;
}

function hide(on) {
  let style = document.getElementById("cx-hide");
  if (!on) return style?.remove();
  style ??= Object.assign(document.createElement("style"), { id: "cx-hide" });
  style.textContent =
    "[data-cx]{color:transparent!important;-webkit-text-fill-color:transparent!important;text-shadow:none!important;caret-color:transparent!important;transition:none!important}";
  document.head.append(style);
}

/** Median background under each text from the screenshot; contrast to its color. */
async function measure({ png, items }) {
  const img = new Image();
  img.src = "data:image/png;base64," + png;
  await img.decode();
  const c = document.createElement("canvas");
  c.width = img.width;
  c.height = img.height;
  const g = c.getContext("2d");
  g.drawImage(img, 0, 0);
  const lin = (v) => {
    v /= 255;
    return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  const lum = ([r, gg, b]) =>
    0.2126 * lin(r) + 0.7152 * lin(gg) + 0.0722 * lin(b);
  const ratio = (a, b) => {
    const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
    return (x + 0.05) / (y + 0.05);
  };
  return items.map((it) => {
    const x = Math.max(0, Math.floor(it.x)),
      y = Math.max(0, Math.floor(it.y));
    const w = Math.max(1, Math.min(c.width - x, Math.ceil(it.w))),
      h = Math.max(1, Math.min(c.height - y, Math.ceil(it.h)));
    const d = g.getImageData(x, y, w, h).data;
    const px = [];
    for (let i = 0; i < d.length; i += 4) px.push([d[i], d[i + 1], d[i + 2]]);
    // The worst tenth of the background (by luminance) next to the text color.
    // rgb(…) in 0–255, or color(srgb …) in 0–1 (from color-mix).
    const m = it.color.match(/[\d.]+/g).map(Number);
    const srgb = it.color.startsWith("color(");
    const fg = srgb ? m.slice(0, 3).map((v) => v * 255) : m.slice(0, 3);
    const alpha = m[3] ?? 1;
    const sorted = px.map((p) => [p, ratio(p, fg)]).sort((a, b) => a[1] - b[1]);
    const bg = sorted[Math.floor(sorted.length * 0.1)][0];
    // A see-through text color: blended over its background.
    const shown = fg.map((v, i) => v * alpha + bg[i] * (1 - alpha));
    const large = it.size >= 24 || (it.bold && it.size >= 18.66);
    return {
      ...it,
      ratio: +ratio(shown, bg).toFixed(2),
      need: large ? 3 : 4.5,
    };
  });
}

async function check(page, name) {
  await page.waitForTimeout(500);
  const items = await page.evaluate(collect, {
    offset: { x: 0, y: 0 },
    prefix: "t",
  });
  // The page in the window (same origin): its texts with the frame's place.
  for (const [i, frame] of page.frames().slice(1).entries()) {
    const el = await frame.frameElement().catch(() => null);
    const box = el && (await el.boundingBox());
    if (!box) continue;
    const inner = await frame
      .evaluate(collect, { offset: { x: box.x, y: box.y }, prefix: `f${i}-` })
      .catch(() => []);
    // Only what the window shows (inside it, not under a menu of the desktop).
    const shown = inner.filter(
      (t) =>
        t.y >= box.y &&
        t.y + t.h <= box.y + box.height &&
        t.x < box.x + box.width,
    );
    const free = await page.evaluate(
      (list) =>
        list.map(
          (t) =>
            document.elementFromPoint(t.x + t.w / 2, t.y + t.h / 2)?.tagName ===
            "IFRAME",
        ),
      shown,
    );
    items.push(...shown.filter((_, j) => free[j]));
  }
  const frames = page.frames();
  for (const f of frames) await f.evaluate(hide, true).catch(() => {});
  await page.waitForTimeout(150);
  const png = (await page.screenshot()).toString("base64");
  for (const f of frames) await f.evaluate(hide, false).catch(() => {});
  await page.screenshot({ path: `${out}/${name}.png` });
  return page
    .evaluate(measure, { png, items })
    .then((r) => r.map((x) => ({ ...x, shot: name })));
}

const results = [];
for (const theme of themes) {
  for (const glass of levels) {
    const made = await create("owner", theme, 1440);
    await made.page.setViewportSize({ width: 1440, height: 900 });
    const look = { theme, glass, blur: 18 };
    made.data.prefs.appearance = look;
    await made.ctx.addInitScript((look) => {
      localStorage.removeItem("titanos.layout");
      localStorage.setItem("titanos.appearance", JSON.stringify(look));
      localStorage.setItem(
        "titanos.desktop.windows.alex",
        JSON.stringify([
          {
            id: "w1",
            app: "app:settings",
            href: "/settings/appearance",
            title: "Systemsteuerung",
            x: 520,
            y: 40,
            w: 860,
            h: 640,
            z: 10,
            minimized: false,
            snap: null,
          },
        ]),
      );
    }, look);
    await made.ctx.route("**/api/v1/preferences/desktop", (route) =>
      route.request().method() === "PUT"
        ? route.fulfill({ status: 204 })
        : route.fallback(),
    );
    const { page } = made;
    await page.goto(base + "/");
    await page.waitForSelector("section.win iframe", { timeout: 15000 });
    await page.waitForTimeout(1500);
    const tag = `${theme}-${glass}`;
    results.push(...(await check(page, `${tag}-desktop`)));
    await page.locator('[data-opens="launcher"]').first().click();
    await page.locator(".launcher").waitFor();
    results.push(...(await check(page, `${tag}-menu`)));
    await page.keyboard.press("Escape");
    await page.mouse.click(300, 760, { button: "right" });
    await page.locator("[role=menu]").first().waitFor();
    results.push(...(await check(page, `${tag}-context`)));
    await made.ctx.close();
  }
}

const low = results.filter((r) => r.ratio < r.need);
const onWallpaper = low.filter((r) => r.wallpaper);
const failing = low.filter((r) => !r.wallpaper);
const key = (r) =>
  `${r.shot} ${r.where} "${r.text}" ${r.ratio}:1 (needs ${r.need})`;
console.log(`${results.length} texts checked`);
if (onWallpaper.length)
  console.log(
    "on the wallpaper (shadowed labels, not counted):\n" +
      [...new Set(onWallpaper.map(key))].join("\n"),
  );
if (failing.length)
  console.log("too low:\n" + [...new Set(failing.map(key))].join("\n"));
await browser.close();
if (!process.env.CONTRAST_REPORT) {
  assert.deepEqual(failing.map(key), [], "contrast of all texts");
  assert.deepEqual(errors, [], "no script errors");
}
console.log("contrast ok");
