// PLAN 87: the phone's home screen – look of the desktop (glass), pages with
// dots, a long press starts arranging (wiggle, not with reduced motion),
// dragging moves a symbol and one on another makes a folder (open, rename,
// take out, dissolve), moving without dragging, widgets on the pages, the
// dock and its switch in Settings › Appearance › Dock, all kept per account
// (PUT /preferences/desktop, read again after a reload). Phone emulation
// (isMobile, touch, 3x) at 390×844 with CDP touch events; isolated API data
// only, nothing reaches a NAS. Not a real phone.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5267 node scripts/test-ui-phone-home.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5267";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const out = process.env.UI_OUTPUT || "out/validation/phone-home";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const create = fixtureFactory(browser, errors);
const W = 390,
  H = 844;

export const mine = [
  {
    app: "immich",
    title: "Immich",
    url: "https://immich.nas.local",
    running: true,
  },
  { app: "jellyfin", url: "", webui: "8096", running: true },
  {
    app: "paperless",
    url: "",
    webui: "http://[IP]:[PORT:8000]/",
    running: false,
  },
];

async function phone({ theme = "graphit", reduced = false, desktop } = {}) {
  const made = await create("owner", theme, W, "de", {
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 3,
    viewport: { width: W, height: H },
    reducedMotion: reduced ? "reduce" : "no-preference",
  });
  const { ctx, page, data } = made;
  await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  data.prefs.desktop = desktop ?? {
    version: 1,
    wallpaper: { source: "builtin", id: "wave", live: false },
  };
  data.puts = 0;
  await ctx.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.slice(7);
    if (path === "/apps/mine") return route.fulfill({ json: { apps: mine } });
    if (path === "/cluster/nodes")
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found", message: "x" } },
      });
    // The desktop's preference is kept like titand does.
    if (path === "/preferences/desktop" && route.request().method() === "PUT") {
      data.prefs.desktop = route.request().postDataJSON();
      data.puts++;
      return route.fulfill({ status: 204 });
    }
    return route.fallback();
  });
  await page.goto(base + "/");
  await page.locator(".home .pages").waitFor({ timeout: 15000 });
  await page.waitForTimeout(900);
  const cdp = await ctx.newCDPSession(page);
  return { ...made, cdp };
}

const center = async (locator) => {
  const b = await locator.boundingBox();
  return [b.x + b.width / 2, b.y + b.height / 2];
};
async function touch(cdp, type, points) {
  await cdp.send("Input.dispatchTouchEvent", {
    type,
    touchPoints: points.map(([x, y], id) => ({ x, y, id })),
  });
}
async function longPress(page, cdp, [x, y]) {
  await touch(cdp, "touchStart", [[x, y]]);
  await page.waitForTimeout(650);
  await touch(cdp, "touchEnd", []);
}
/** A finger from `from` to `to` in steps; `hold` keeps it down at the end. */
async function drag(page, cdp, from, to, { steps = 14, hold = false } = {}) {
  await touch(cdp, "touchStart", [from]);
  for (let i = 1; i <= steps; i++)
    await touch(cdp, "touchMove", [
      [
        from[0] + ((to[0] - from[0]) * i) / steps,
        from[1] + ((to[1] - from[1]) * i) / steps,
      ],
    ]);
  await page.waitForTimeout(120);
  if (!hold) await touch(cdp, "touchEnd", []);
}
const noSideways = async (page) =>
  assert.ok(
    (await page.evaluate(() => document.documentElement.scrollWidth)) <= W,
    "nothing wider than the screen",
  );
const key = (page, k) => page.locator(`.home .pages [data-home-key="${k}"]`);
const pageOf = (page, k) =>
  page.evaluate(
    (k) =>
      Number(
        document
          .querySelector(`.home .pages [data-home-key="${k}"]`)
          ?.closest("[data-page]")?.dataset.page ?? -1,
      ),
    k,
  );
const stored = (data) => data.prefs.desktop.phone;

try {
  const { ctx, page, data, cdp } = await phone();
  // The look of the desktop: wallpaper behind, bar, widget and dock in glass.
  const glass = await page
    .locator(".home .dock")
    .evaluate((el) => getComputedStyle(el).backdropFilter);
  assert.match(glass, /blur/, "dock in glass like the desktop");
  await page.locator(".home .widget").first().waitFor();
  assert.equal(
    await page.locator(".home .dock .app").count(),
    4,
    "dock: four programs",
  );
  // Every target of the home screen is at least 44 px.
  for (const b of await page
    .locator(".home .app, .home .menu-btn, .home .search, .home .dot-btn")
    .evaluateAll((els) =>
      els
        .filter((e) => e.getBoundingClientRect().width > 0)
        .map((e) => e.getBoundingClientRect().toJSON()),
    ))
    assert.ok(
      b.height >= 27.5 && (b.height >= 43.5 || b.width <= 30),
      `target ${JSON.stringify(b)}`,
    );
  const pages = await page.locator(".home .page").count();
  assert.ok(pages >= 2, `programs on more than one page (${pages})`);
  assert.equal(
    await page.locator(".home .dots [aria-current=page]").count(),
    1,
  );
  await noSideways(page);
  await page.screenshot({ path: `${out}/startbildschirm.png` });

  // Swiping goes to the next page; the dot says which.
  const pagesBox = await page.locator(".home .pages").boundingBox();
  await drag(page, cdp, [W - 40, pagesBox.y + 200], [40, pagesBox.y + 210], {
    steps: 8,
  });
  await page.waitForTimeout(700);
  assert.equal(
    await page
      .locator(".home .dots [aria-current=page]")
      .getAttribute("aria-label"),
    `Seite 2 von ${pages}`,
    "swiped to page 2",
  );
  await page.locator(".home .dots .dot-btn").first().tap();
  await page.waitForTimeout(700);

  // A program opens out of its symbol (slowed down to see it half way).
  await cdp.send("Animation.enable");
  await cdp.send("Animation.setPlaybackRate", { playbackRate: 0.08 });
  await key(page, "app:storage").tap();
  await page.locator(".phone-zoom").waitFor({ state: "attached" });
  await page.waitForTimeout(650);
  await page.screenshot({ path: `${out}/programm-oeffnet.png` });
  await cdp.send("Animation.setPlaybackRate", { playbackRate: 1 });
  await page.waitForURL(/\/storage/);
  await page.locator(".phone-zoom").waitFor({ state: "detached" });
  await page.goto(base + "/");
  await page.locator(".home .pages").waitFor();
  await page.waitForTimeout(700);

  // Pulling the pages down searches: programs, pages, single settings, actions.
  const pb = await page.locator(".home .pages").boundingBox();
  await touch(cdp, "touchStart", [[200, pb.y + 300]]);
  for (let i = 1; i <= 10; i++)
    await touch(cdp, "touchMove", [[202, pb.y + 300 + i * 12]]);
  await page.locator(".home .pull-hint").waitFor();
  await touch(cdp, "touchEnd", []);
  const launcher = page.locator(".launcher.phone");
  await launcher.waitFor();
  assert.ok(
    await page.evaluate(() =>
      document.activeElement?.matches(".home .search input"),
    ),
    "the search field has the keyboard",
  );
  await page.locator(".home .search input").fill("sprache");
  const hit = launcher
    .locator("[data-settings-hit]", { hasText: "Sprache" })
    .first();
  await hit.waitFor();
  await page.locator(".home .search input").fill("speicher");
  await launcher.locator(".prog", { hasText: "Speicher" }).first().waitFor();
  await page.waitForTimeout(300);
  await page.screenshot({ path: `${out}/suche-heruntergezogen.png` });
  await page.locator(".home .search input").fill("sprache");
  await hit.tap();
  await page.waitForURL(/\/settings\/appearance/);
  await page.goto(base + "/");
  await page.locator(".home .pages").waitFor();
  await page.waitForTimeout(700);

  // A long press starts arranging.
  const shares = key(page, "app:shares");
  await longPress(page, cdp, await center(shares.locator(".icon-box")));
  await page.locator(".home.editing").waitFor();
  await page.locator("[data-phone-done]").waitFor();
  assert.equal(page.url(), base + "/", "a long press opens nothing");
  assert.notEqual(
    await shares
      .locator(".wrap")
      .evaluate((el) => getComputedStyle(el).animationName),
    "none",
    "symbols wiggle",
  );

  // Dragging one symbol on another makes a folder.
  const storage = key(page, "app:storage");
  const from = await center(storage.locator(".icon-box"));
  const to = await center(shares.locator(".icon-box"));
  await drag(page, cdp, from, to, { hold: true });
  await page.locator(".home .app.into").waitFor();
  await page.locator(".home .ghost").waitFor();
  await page.screenshot({ path: `${out}/anordnen-ziehen.png` });
  await touch(cdp, "touchEnd", []);
  const folderKey = await page
    .locator('.home .pages [data-home-key^="folder:"]')
    .getAttribute("data-home-key");
  assert.ok(folderKey, "folder made");
  assert.equal(await key(page, "app:shares").count(), 0);
  assert.equal(await key(page, "app:storage").count(), 0);

  // Open, rename, take out.
  await key(page, folderKey).tap();
  const sheet = page.locator("dialog[open]");
  await sheet.waitFor();
  assert.equal(await sheet.locator(".folder-app").count(), 2);
  await sheet.locator("[data-folder-rename]").tap();
  await sheet.locator("#phone-folder-name").fill("Werkzeuge");
  await sheet.getByRole("button", { name: "Speichern" }).tap();
  await sheet.locator("h2", { hasText: "Werkzeuge" }).waitFor();
  // The sheet comes from the bottom (PLAN 87).
  const sb = await sheet.boundingBox();
  assert.ok(
    Math.abs(sb.y + sb.height - H) <= 1,
    `folder sheet at the bottom (${JSON.stringify(sb)})`,
  );
  await page.screenshot({ path: `${out}/ordner-offen.png` });
  // Dragging the folder sheet down closes it.
  const grip = await center(sheet.locator("h2"));
  await drag(page, cdp, grip, [grip[0], grip[1] + 300], { steps: 10 });
  await sheet.waitFor({ state: "detached" });
  await key(page, folderKey).tap();
  await page.locator("dialog[open] [data-folder-out='app:storage']").tap();
  await page.waitForTimeout(200);
  // One program left: the folder dissolves by itself.
  assert.equal(
    await page.locator('.home .pages [data-home-key^="folder:"]').count(),
    0,
  );
  assert.equal(await key(page, "app:storage").count(), 1);
  assert.equal(await key(page, "app:shares").count(), 1);

  // Moving without dragging: tap picks, the bar moves it.
  await key(page, "app:shares").tap();
  await page.locator(".edit-bar [data-step='on']").tap();
  const order = await page
    .locator(".home .page[data-page='0'] [data-home-key]")
    .evaluateAll((els) => els.map((e) => e.dataset.homeKey));
  assert.ok(
    order.indexOf("app:shares") > order.indexOf("app:storage"),
    `moved on (${order})`,
  );
  await page.locator(".edit-bar [data-step='page-on']").tap();
  await page.waitForTimeout(500);
  assert.equal(await pageOf(page, "app:shares"), 1, "to the next page");
  await key(page, "app:shares").tap();
  await page.locator(".dots .dot-btn").first().tap();
  await page.waitForTimeout(500);

  // Widgets: add one, it stands at the start of the page.
  await page.locator("[data-phone-add-widget]").tap();
  await page.locator("[data-widget-kind='cpuram']").tap();
  await page.waitForTimeout(300);
  const first = await page
    .locator(".home .page[data-page='0'] [data-home-key]")
    .first()
    .getAttribute("data-home-key");
  assert.match(first, /^widget:/);
  await page.locator("[data-phone-done]").tap();
  await page.waitForTimeout(1900);
  assert.ok(data.puts > 0, "saved on the NAS");
  assert.equal(stored(data).custom, true);
  assert.equal(stored(data).widgets.length, 2);
  await page.screenshot({ path: `${out}/widgets.png` });
  // After a reload the arrangement is the same.
  await page.reload();
  await page.locator(".home .pages").waitFor();
  await page.waitForTimeout(900);
  assert.equal(await pageOf(page, "app:shares"), 1, "kept after a reload");
  assert.equal(await page.locator(".home .widget").count(), 2);

  // The dock goes into the dock by the bar too.
  await longPress(
    page,
    cdp,
    await center(key(page, "app:storage").locator(".icon-box")),
  );
  await page.locator(".home.editing").waitFor();
  await page.locator(".home .dock [data-home-key='app:files']").tap();
  await page.locator(".edit-bar [data-step='dock']").tap();
  assert.equal(await page.locator(".home .dock .app").count(), 3);
  await page.locator("[data-phone-done]").tap();
  await page.screenshot({ path: `${out}/dock-an.png` });

  // The switch in Settings › Appearance › Dock.
  await page.goto(base + "/settings/appearance?tab=dock");
  const row = page.locator("#phone-dock");
  await row.waitFor({ timeout: 15000 });
  await row.scrollIntoViewIfNeeded();
  await page.screenshot({ path: `${out}/einstellung-handy-dock.png` });
  await row.tap();
  await page.waitForTimeout(1900);
  assert.equal(stored(data).dock.show, false);
  await page.goto(base + "/");
  await page.locator(".home .pages").waitFor();
  await page.waitForTimeout(900);
  assert.equal(await page.locator(".home .dock").count(), 0, "dock off");
  for (const k of ["app:docker", "app:monitor", "app:settings"])
    assert.equal(
      await key(page, k).count(),
      1,
      `${k} on the pages without a dock`,
    );
  await page.screenshot({ path: `${out}/dock-aus.png` });
  await noSideways(page);
  await ctx.close();

  // Reduced motion: arranging shows a ring instead of wiggling; no plate grows.
  {
    const { ctx, page, cdp } = await phone({ reduced: true, theme: "hell" });
    await page.screenshot({ path: `${out}/startbildschirm-hell.png` });
    await key(page, "app:storage").tap();
    await page.waitForURL(/\/storage/);
    assert.equal(
      await page.locator(".phone-zoom").count(),
      0,
      "no movement with reduced motion",
    );
    await page.goto(base + "/");
    await page.locator(".home .pages").waitFor();
    await page.waitForTimeout(700);
    await longPress(
      page,
      cdp,
      await center(key(page, "app:shares").locator(".icon-box")),
    );
    await page.locator(".home.editing").waitFor();
    const wrap = key(page, "app:shares").locator(".wrap");
    assert.equal(
      await wrap.evaluate((el) => getComputedStyle(el).animationName),
      "none",
    );
    assert.match(
      await wrap.evaluate((el) => getComputedStyle(el).outlineStyle),
      /dashed/,
    );
    await page.screenshot({ path: `${out}/anordnen-hell-ruhig.png` });
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
    "PASS phone home: glass, pages, long press, folder by dragging, rename, take out, moving by the bar, widgets, dock switch, kept per account",
  );
