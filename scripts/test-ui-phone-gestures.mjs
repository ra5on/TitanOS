// PLAN 87: gestures on the phone. A swipe from the left edge goes back
// (within the program, then home) – only from a small zone, never when a
// touch starts outside it, not when the finger goes up or down (the page
// scrolls), and not in iOS Safari, which has its own swipe; pulling the bar
// down shows the notifications with quick switches; a message swiped left
// is deleted, swiped right is read; the number of unread messages goes to
// the app's symbol (Badging API); pulling a list page down runs its
// refresh; a file row swiped left offers "Teilen" and "Löschen" (which asks
// first, as a sheet from the bottom). Phone emulation (isMobile, touch, 3x)
// at 390×844 with CDP touch events; isolated API data only. Not a real phone.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5267 node scripts/test-ui-phone-gestures.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5267";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const out = process.env.UI_OUTPUT || "out/validation/phone-gestures";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const create = fixtureFactory(browser, errors);
const W = 390,
  H = 844;
const now = Date.now();
const notices = [
  {
    id: 3,
    time: new Date(now - 60e3).toISOString(),
    severity: "warning",
    kind: "custom",
    subject: "",
    title: "Volume „Data“ zu 91 % voll",
    text: "Auf dem Volume Data sind noch 90 GB frei.",
  },
  {
    id: 2,
    time: new Date(now - 3600e3).toISOString(),
    severity: "info",
    kind: "custom",
    subject: "",
    title: "Sicherung „Fotos“ fertig",
    text: "1 204 Dateien gesichert, 3,2 GB.",
  },
  {
    id: 1,
    time: new Date(now - 7200e3).toISOString(),
    severity: "error",
    kind: "custom",
    subject: "",
    title: "Platte sdb meldet Fehler",
    text: "SMART: 8 wieder zugewiesene Sektoren.",
  },
];
const files = [
  { name: "Urlaub", dir: true, size: 0, modified: "2026-11-10T09:00:00Z" },
  {
    name: "Angebot Küche.pdf",
    dir: false,
    size: 482113,
    modified: "2026-11-12T14:20:00Z",
  },
  {
    name: "Foto 0412.jpg",
    dir: false,
    size: 3_400_112,
    modified: "2026-11-11T18:02:00Z",
  },
  {
    name: "Notizen.txt",
    dir: false,
    size: 1200,
    modified: "2026-11-09T07:45:00Z",
  },
];

async function phone({ ua, theme = "graphit" } = {}) {
  const made = await create("owner", theme, W, "de", {
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 3,
    viewport: { width: W, height: H },
    ...(ua ? { userAgent: ua } : {}),
  });
  const { ctx, page, data } = made;
  await ctx.addInitScript(() => {
    localStorage.removeItem("titanos.layout");
    // The Badging API as Chrome offers it to an installed app.
    window.__badges = [];
    navigator.setAppBadge = (n) => (window.__badges.push(n), Promise.resolve());
    navigator.clearAppBadge = () => (
      window.__badges.push(0),
      Promise.resolve()
    );
  });
  data.prefs.desktop = {
    version: 1,
    wallpaper: { source: "builtin", id: "aurora", live: false },
  };
  data.system.notificationsLatest = 3;
  data.notices = structuredClone(notices);
  data.lists = 0;
  data.dismissed = [];
  await ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.slice(7);
    if (path === "/notifications")
      return route.fulfill({ json: { messages: data.notices } });
    if (path === "/notifications/dismiss") {
      const { ids } = route.request().postDataJSON();
      data.dismissed.push(...ids);
      data.notices = data.notices.filter((n) => !ids.includes(n.id));
      return route.fulfill({ json: { messages: data.notices } });
    }
    if (path === "/files/list") {
      data.lists++;
      return route.fulfill({ json: files });
    }
    if (path === "/cluster/nodes")
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found", message: "x" } },
      });
    return route.fallback();
  });
  await page.goto(base + "/");
  await page.locator(".home .pages").waitFor({ timeout: 15000 });
  await page.waitForTimeout(800);
  const cdp = await ctx.newCDPSession(page);
  return { ...made, cdp };
}
async function touch(cdp, type, points) {
  await cdp.send("Input.dispatchTouchEvent", {
    type,
    touchPoints: points.map(([x, y], id) => ({ x, y, id })),
  });
}
async function swipe(page, cdp, from, to, steps = 10) {
  await touch(cdp, "touchStart", [from]);
  for (let i = 1; i <= steps; i++)
    await touch(cdp, "touchMove", [
      [
        from[0] + ((to[0] - from[0]) * i) / steps,
        from[1] + ((to[1] - from[1]) * i) / steps,
      ],
    ]);
  await touch(cdp, "touchEnd", []);
  await page.waitForTimeout(250);
}
const path = (page) =>
  new URL(page.url()).pathname + new URL(page.url()).search;

try {
  const { ctx, page, data, cdp } = await phone();

  // The number of unread messages on the app's symbol.
  await page.waitForFunction(() => window.__badges.includes(3));

  // Back from the edge: Systemsteuerung › Darstellung, back to the overview, back home.
  await page.goto(base + "/settings");
  await page.locator("header.phone-bar").waitFor();
  await page.locator("a.srow", { hasText: "Darstellung" }).first().tap();
  await page.waitForURL(/\/settings\/appearance/);
  await page.waitForTimeout(500);
  // Outside the zone: nothing.
  await swipe(page, cdp, [44, 420], [220, 425]);
  assert.match(
    path(page),
    /\/settings\/appearance/,
    "a swipe from 44 px is no back",
  );
  // Up and down from the edge: the page scrolls, no back.
  await swipe(page, cdp, [8, 600], [12, 250]);
  assert.match(
    path(page),
    /\/settings\/appearance/,
    "a vertical swipe at the edge is no back",
  );
  assert.ok(
    (await page.evaluate(() => window.scrollY)) > 0,
    "the page scrolled",
  );
  // From the edge to the right, half way: the arrow shows.
  await touch(cdp, "touchStart", [[6, 420]]);
  for (let i = 1; i <= 6; i++)
    await touch(cdp, "touchMove", [[6 + i * 20, 424]]);
  await page.locator("[data-edge-back].armed").waitFor();
  await page.screenshot({ path: `${out}/zurueck-vom-rand.png` });
  await touch(cdp, "touchEnd", []);
  await page.waitForURL((u) => u.pathname === "/settings");
  await page.waitForTimeout(400);
  await swipe(page, cdp, [6, 420], [200, 424]);
  await page.waitForURL((u) => u.pathname === "/");
  await page.locator(".home .pages").waitFor();

  // Pull the bar down: the notifications with quick switches.
  await page.goto(base + "/storage");
  const bar = page.locator("header.phone-bar");
  await bar.waitFor();
  const bb = await bar.boundingBox();
  await swipe(page, cdp, [200, bb.y + 20], [205, bb.y + 260]);
  const sheet = page.locator("dialog[open]");
  await sheet.locator("[data-notices]").waitFor();
  assert.equal(
    await sheet.locator("[data-quick]").count(),
    3,
    "three quick switches",
  );
  for (const b of await sheet
    .locator("[data-quick], .head .btn")
    .evaluateAll((els) => els.map((e) => e.getBoundingClientRect().height)))
    assert.ok(b >= 43.5, `quick switch ≥ 44 px (${b})`);
  await page.waitForTimeout(400);
  await page.screenshot({ path: `${out}/mitteilungen-heruntergezogen.png` });
  // Swiped to the right: read; the number on the symbol follows.
  const second = sheet.locator("[data-notice='2']");
  let b = await second.boundingBox();
  await swipe(
    page,
    cdp,
    [b.x + 60, b.y + b.height / 2],
    [b.x + 300, b.y + b.height / 2 + 4],
  );
  await page.waitForFunction(() => window.__badges.at(-1) === 2);
  assert.equal(
    await sheet.locator("[data-notice='2'] .new").count(),
    0,
    "marked read",
  );
  // Swiped far to the left: deleted.
  b = await sheet.locator("[data-notice='1']").boundingBox();
  await swipe(
    page,
    cdp,
    [b.x + b.width - 30, b.y + b.height / 2],
    [b.x + 10, b.y + b.height / 2 + 3],
  );
  await page.waitForFunction(
    () => !document.querySelector("[data-notice='1']"),
  );
  assert.deepEqual(data.dismissed, [1], "deleted on the NAS");
  // Half way: the action shows, nothing is deleted yet.
  b = await sheet.locator("[data-notice='3']").boundingBox();
  await swipe(
    page,
    cdp,
    [b.x + b.width - 30, b.y + b.height / 2],
    [b.x + b.width - 140, b.y + b.height / 2],
  );
  await sheet
    .locator("[data-swipe-row].open [data-swipe-action='delete']")
    .waitFor();
  assert.deepEqual(data.dismissed, [1]);
  await page.screenshot({ path: `${out}/mitteilung-gewischt.png` });
  // The sheet goes with a swipe down.
  const head = await sheet.locator("h2").boundingBox();
  await swipe(
    page,
    cdp,
    [head.x + 40, head.y + 5],
    [head.x + 40, head.y + 400],
  );
  await sheet.waitFor({ state: "detached" });

  // The bell on the home screen opens the same.
  await page.goto(base + "/");
  await page.locator(".home [data-phone-bell]").tap();
  await page.locator("dialog[open] [data-notices]").waitFor();
  await page.keyboard.press("Escape");

  // Files: pull the list down refreshes it; a row swiped left offers Teilen and Löschen.
  await page.goto(base + "/files?root=share%2FDocuments");
  const row = page.locator("[data-file-name='Angebot Küche.pdf']");
  await row.waitFor({ timeout: 15000 });
  await page.waitForTimeout(500);
  const before = data.lists;
  const list = await page.locator("[data-file-name='Urlaub']").boundingBox();
  await page.evaluate(() => window.scrollTo(0, 0));
  await swipe(page, cdp, [200, list.y + 10], [205, list.y + 230]);
  await page.waitForTimeout(500);
  assert.ok(data.lists > before, "pulled down: the list loads anew");
  const rb = await row.boundingBox();
  await swipe(
    page,
    cdp,
    [rb.x + rb.width - 30, rb.y + rb.height / 2],
    [rb.x + rb.width - 200, rb.y + rb.height / 2 + 2],
  );
  const actions = page.locator("[data-swipe-row].open [data-swipe-action]");
  await actions.first().waitFor();
  assert.deepEqual(
    await actions.evaluateAll((els) => els.map((e) => e.dataset.swipeAction)),
    ["share", "delete"],
  );
  await page.screenshot({ path: `${out}/liste-gewischt.png` });
  await page
    .locator("[data-swipe-row].open [data-swipe-action='delete']")
    .tap();
  const confirm = page.locator("dialog[open]");
  await confirm.waitFor();
  await page.waitForTimeout(350);
  const cb = await confirm.boundingBox();
  assert.ok(
    Math.abs(cb.y + cb.height - H) <= 1,
    "the question comes from the bottom",
  );
  assert.ok(
    await confirm.locator(".btn-danger, .btn-warn").count(),
    "deleting asks first (the question has the delete button)",
  );
  await page.screenshot({ path: `${out}/loeschen-frage-unten.png` });
  await confirm.getByRole("button", { name: "Abbrechen" }).tap();

  // Menus are sheets from the bottom: the ⋯ menu of a program, a button's menu.
  await page.goto(base + "/settings/appearance");
  await page.locator("header.phone-bar").waitFor();
  await page.waitForTimeout(400);
  await page.getByRole("button", { name: "Seiten des Programms" }).tap();
  const more = page.locator("header.phone-bar .pages");
  await more.waitFor();
  await page.waitForTimeout(350);
  let mb = await more.boundingBox();
  assert.ok(
    mb.x === 0 && mb.width === W && Math.abs(mb.y + mb.height - H) <= 1,
    `⋯ menu as a sheet (${JSON.stringify(mb)})`,
  );
  for (const h of await more
    .locator("[role^=menuitem]")
    .evaluateAll((els) => els.map((e) => e.getBoundingClientRect().height)))
    assert.ok(h >= 47.5, `menu entry ≥ 48 px (${h})`);
  await page.screenshot({ path: `${out}/menue-unten.png` });
  await swipe(page, cdp, [200, mb.y + 12], [200, mb.y + 400]);
  await more.waitFor({ state: "detached" });
  // A menu of a button (Dropdown) on the files page.
  await page.goto(base + "/files?root=share%2FDocuments");
  await page.locator("[data-file-name='Urlaub']").waitFor();
  await page.getByRole("button", { name: "Neu" }).first().tap();
  const drop = page.locator(".dropdown-panel:popover-open");
  await drop.waitFor();
  await page.waitForTimeout(350);
  mb = await drop.boundingBox();
  assert.ok(
    mb.x === 0 && mb.width === W && Math.abs(mb.y + mb.height - H) <= 1,
    `button menu as a sheet (${JSON.stringify(mb)})`,
  );
  await swipe(page, cdp, [200, mb.y + 10], [200, mb.y + 300]);
  await drop.waitFor({ state: "detached" });
  await ctx.close();

  // iOS Safari has its own swipe from the edge: TitanOS leaves it alone.
  {
    const { ctx, page, cdp } = await phone({
      ua: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1",
    });
    await page.goto(base + "/settings");
    await page.locator("header.phone-bar").waitFor();
    await page.waitForTimeout(400);
    await swipe(page, cdp, [6, 420], [220, 424]);
    await page.waitForTimeout(400);
    assert.equal(
      new URL(page.url()).pathname,
      "/settings",
      "no own back in iOS Safari",
    );
    assert.equal(await page.locator("[data-edge-back]").count(), 0);
    await ctx.close();
  }
} catch (err) {
  console.error(err);
  process.exitCode = 1;
}
await browser.close();
const relevant = errors.filter(
  (e) => !/Failed to load resource|net::ERR/.test(e),
);
assert.deepEqual(relevant, [], "no script errors");
if (!process.exitCode)
  console.log(
    "PASS phone gestures: edge back (zone, vertical, iOS), notifications pulled down, swiped read/delete, badge, pull to refresh, file row swipe",
  );
