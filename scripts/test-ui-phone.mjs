// PLAN 64: TitanOS on the phone – the home grid is not cut off (search below
// the status bar, sticky, nothing sideways), Docker apps are in the grid
// (also an app whose address is a template the browser cannot use), they
// open full screen in the remote view and Back returns; a program opens with
// its bar; the cluster switcher sits beside the search and the bar names the
// node being managed. Phone emulation (isMobile, touch, 3x) at 390×844 and
// 430×932. Isolated API data only; nothing reaches a NAS. Not a real iPhone.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5261 node scripts/test-ui-phone.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5261";
const out = process.env.UI_OUTPUT || "out/validation/phone";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const nodes = {
  manages: true,
  version: "0.61.0",
  nodes: [
    {
      id: "n1",
      name: "titan",
      self: true,
      online: true,
      version: "0.61.0",
      compatible: true,
    },
    {
      id: "n2",
      name: "thor",
      self: false,
      online: true,
      version: "0.61.0",
      compatible: true,
    },
    {
      id: "n3",
      name: "loki-mit-langem-namen",
      self: false,
      online: false,
      version: "0.61.0",
      compatible: true,
    },
  ],
};
// What GET /apps/mine sends: with an HTTPS name, with a port only, and with a
// catalog template the browser cannot turn into an address.
const mine = [
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

async function phone(width, height, { cluster = true, node = "" } = {}) {
  const made = await create("owner", "graphit", width, "de", {
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 3,
    viewport: { width, height },
  });
  const { ctx, page, data } = made;
  await ctx.addInitScript((node) => {
    localStorage.removeItem("titanos.layout");
    if (node) sessionStorage.setItem("titanos.cluster.node", node);
  }, node);
  data.prefs.desktop = {
    version: 1,
    wallpaper: { source: "builtin", id: "wave", live: false },
  };
  await ctx.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    // A managed node answers through the cluster: the same test data.
    const path = p.replace(/^\/cluster\/nodes\/n\d\/api/, "");
    if (path === "/apps/mine") return route.fulfill({ json: { apps: mine } });
    if (path === "/cluster/nodes")
      return route.fulfill(
        cluster
          ? { json: nodes }
          : {
              status: 404,
              json: { error: { code: "not_found", message: "x" } },
            },
      );
    if (path !== p)
      return route.continue({ url: route.request().url().replace(p, path) });
    return route.fallback();
  });
  await page.goto(base + "/");
  await page.locator(".home").waitFor({ timeout: 15000 });
  await page.waitForTimeout(900);
  return made;
}

const noSideways = async (page, width, what) =>
  assert.ok(
    (await page.evaluate(() => document.documentElement.scrollWidth)) <= width,
    `${what}: nothing wider than the screen`,
  );

for (const [width, height] of [
  [390, 844],
  [430, 932],
]) {
  const label = `${width}`;
  const { ctx, page } = await phone(width, height);
  // Not cut off: the search lies fully on the screen, not zoomed.
  const search = await page.locator(".home .search").boundingBox();
  assert.ok(search.y >= 0, `search starts on the screen (${search.y})`);
  assert.ok(search.height >= 40, "search is a full touch target");
  assert.equal(
    await page.evaluate(() => window.visualViewport?.scale ?? 1),
    1,
    "not zoomed",
  );
  assert.equal(
    await page.evaluate(() => window.scrollY),
    0,
    "home starts at its top",
  );
  await noSideways(page, width, "home");
  // Docker apps in the grid, the template one too.
  for (const name of ["Immich", "jellyfin", "paperless"])
    await page.locator(".grid .app", { hasText: name }).first().waitFor();
  const font = await page
    .locator(".home .search input")
    .evaluate((el) => parseFloat(getComputedStyle(el).fontSize));
  assert.ok(font >= 16, "search field ≥ 16 px (no zoom on focus)");
  await page.screenshot({ path: `${out}/01-home-${label}.png` });
  // The bar sticks while the grid scrolls.
  await page.evaluate(() => window.scrollTo(0, 400));
  await page.waitForTimeout(200);
  const stuck = await page.locator(".home .top").boundingBox();
  assert.ok(
    Math.abs(stuck.y) <= 1,
    `search bar sticks at the top (${stuck.y})`,
  );
  await page.evaluate(() => window.scrollTo(0, 0));
  // Cluster switcher beside the search; its menu stays on the screen.
  const trigger = page.locator(".home .node button.trigger");
  await trigger.waitFor();
  const t = await trigger.boundingBox();
  assert.ok(t.width >= 44 && t.height >= 44, "switcher is a 44 px target");
  await trigger.tap();
  const menu = page.locator(".home .node .menu");
  await menu.waitFor();
  const m = await menu.boundingBox();
  assert.ok(
    m.x >= 0 && m.x + m.width <= width + 0.5,
    "node menu inside the screen",
  );
  for (const box of await menu
    .locator("button")
    .evaluateAll((els) => els.map((e) => e.getBoundingClientRect().height)))
    assert.ok(box >= 43.5, `node entries are 44 px high (${box})`);
  await noSideways(page, width, "node menu");
  await page.screenshot({ path: `${out}/02-cluster-${label}.png` });
  await page.keyboard.press("Escape");
  // A Docker app opens full screen in its remote view; Back returns.
  await page.locator(".grid .app", { hasText: "paperless" }).first().tap();
  const full = page.locator(".remote-full");
  await full.waitFor();
  const f = await full.boundingBox();
  assert.ok(
    f.x === 0 && f.y === 0 && f.width === width && f.height >= height - 1,
    "full screen",
  );
  await noSideways(page, width, "remote view");
  await page.screenshot({ path: `${out}/03-docker-app-${label}.png` });
  await page.getByRole("button", { name: "Zum Startbildschirm" }).first().tap();
  await full.waitFor({ state: "detached" });
  await page.locator(".grid .app", { hasText: "Immich" }).first().waitFor();
  // A program of TitanOS: its bar at the top, the page below, Back to the grid.
  await page.locator(".grid .app", { hasText: "Speicher" }).first().tap();
  const bar = page.locator("header.phone-bar");
  await bar.waitFor();
  const b = await bar.boundingBox();
  assert.ok(b.y >= 0 && b.y <= 1, "program bar at the top");
  await page.waitForTimeout(800);
  await noSideways(page, width, "program page");
  await page.screenshot({ path: `${out}/04-programm-${label}.png` });
  await bar.locator("button").first().tap();
  await page.locator(".home .search").waitFor();
  await ctx.close();
}

// Managing another node: its name stays in the program bar.
{
  const { ctx, page } = await phone(390, 844, { node: "n2" });
  await page.locator(".grid .app", { hasText: "Docker" }).first().tap();
  const chip = page.locator("header.phone-bar .node");
  await chip.waitFor();
  assert.equal((await chip.textContent()).trim(), "thor");
  await page.waitForTimeout(800);
  await page.screenshot({ path: `${out}/05-anderer-node.png` });
  await ctx.close();
}

// Not in a cluster: no switcher.
{
  const { ctx, page } = await phone(390, 844, { cluster: false });
  assert.equal(
    await page.locator(".home .node").count(),
    0,
    "no switcher without a cluster",
  );
  await ctx.close();
}

await browser.close();
const relevant = errors.filter(
  (e) => !/Failed to load resource|net::ERR/.test(e),
);
assert.deepEqual(relevant, [], "no script errors");
console.log(
  "PASS phone home not cut, Docker apps, remote full screen, program bar, cluster switcher",
);
