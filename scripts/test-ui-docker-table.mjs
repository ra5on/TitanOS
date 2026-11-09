// Docker › Container as one table (PLAN 67, owner's decision of 7 Nov 2026):
// one row per app (a Compose stack is one row), state as text with a mark
// (never colour alone), CPU bar, RAM, ports, update badge, Start/Stop and a
// ⋯ menu per row; filters with counts, four figures, a click on a row opens
// the app's page. 1600 and 1280 px in a window of the desktop, the page alone
// at 1280 px and the phone at 390 px (stacked rows without overlap).
// Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5283 node scripts/test-ui-docker-table.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5283";
const out = process.env.UI_OUTPUT || "out/validation/docker-tabelle";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const time = "2026-11-07T08:00:00Z";
const port = (host, protocol = "tcp") => ({ host, container: host, protocol });
const container = (name, o = {}) => ({
  id: name,
  name,
  image: o.image ?? name + ":latest",
  state: o.state ?? "running",
  since: time,
  ports: o.ports ?? [],
  app: o.app ?? name,
  service: o.service ?? name,
  managed: true,
  cpuPercent: o.cpu ?? 1.2,
  memoryBytes: o.mem ?? 180e6,
  cpuHistory: [],
  webui: o.webui ?? "",
  publicUrl: "",
  icon: "",
  updateAvailable: !!o.update,
  ...(o.host ? { hostNetwork: true } : {}),
});
const containers = () => [
  container("immich-server", {
    app: "immich",
    service: "server",
    cpu: 4.1,
    mem: 1.4e9,
    webui: "2283",
    ports: [port(2283)],
  }),
  container("immich-ml", {
    app: "immich",
    service: "ml",
    cpu: 0.4,
    mem: 300e6,
  }),
  container("immich-db", {
    app: "immich",
    service: "db",
    cpu: 0.2,
    mem: 120e6,
  }),
  container("jellyfin", {
    image: "jellyfin/jellyfin:10.10",
    cpu: 6.5,
    mem: 1.1e9,
    webui: "8096",
    ports: [port(8096)],
    update: true,
  }),
  container("vaultwarden", {
    image: "vaultwarden/server:latest",
    state: "exited",
    ports: [port(8081)],
  }),
  container("frigate", {
    image: "ghcr.io/blakeblackshear/frigate:stable",
    state: "restarting",
    ports: [port(5000)],
    host: true,
  }),
];
const apps = ["immich", "jellyfin", "vaultwarden", "frigate"];

async function open(width, { desktop = false, height = 900 } = {}) {
  const made = await create("owner", "graphit", width);
  await made.page.setViewportSize({ width, height });
  made.data.dockerContainers = containers();
  made.data.dockerApps = apps;
  // An older stored preference with view fields is still read without trouble.
  made.data.prefs.docker = {
    version: 1,
    view: "cards",
    size: "l",
    order: [],
    details: true,
    primaryServices: {},
  };
  if (desktop)
    await made.ctx.addInitScript(() =>
      localStorage.removeItem("titanos.layout"),
    );
  await made.page.goto(base + "/docker");
  return made;
}
const table = (scope) => scope.locator("table.app-table");
const row = (scope, name) => scope.locator(`tr[data-app="${name}"]`);

try {
  // 1. In a window of the desktop at 1600 and 1280 px.
  for (const width of [1600, 1280]) {
    const { ctx, page } = await open(width, { desktop: true });
    await page
      .locator("section.win iframe")
      .first()
      .waitFor({ timeout: 20000 });
    const frame = page.frameLocator("section.win iframe").first();
    await table(frame).waitFor({ timeout: 20000 });
    await page.waitForTimeout(600);
    // One row per app; the stack is one row.
    assert.equal(await table(frame).locator("tbody tr").count(), 4);
    assert.match(await row(frame, "immich").innerText(), /3 Container/);
    assert.match(await row(frame, "frigate").innerText(), /Host-Netz/);
    assert.doesNotMatch(await row(frame, "jellyfin").innerText(), /Host-Netz/);
    // State as text with a mark, never colour alone.
    const states = await table(frame)
      .locator("td.state")
      .evaluateAll((cells) => cells.map((c) => c.textContent.trim()));
    assert.ok(states.includes("läuft"), "running as text");
    assert.ok(states.includes("gestoppt"), "stopped as text");
    assert.ok(states.includes("startet neu"), "restarting as text");
    // Besides the text a mark of its own shape: ring for stopped, triangle for restarting.
    const marks = await table(frame)
      .locator("td.state .mark")
      .evaluateAll((els) =>
        els.map((e) => {
          const c = getComputedStyle(e);
          return c.clipPath !== "none"
            ? "triangle"
            : c.borderStyle === "solid"
              ? "ring"
              : "dot";
        }),
      );
    assert.deepEqual([...new Set(marks)].sort(), ["dot", "ring", "triangle"]);
    // Columns.
    await page.screenshot({ path: `${out}/01-fenster-${width}.png` });
    assert.match(await row(frame, "jellyfin").innerText(), /7 %/);
    assert.match(await row(frame, "jellyfin").innerText(), /8096/);
    assert.match(await row(frame, "jellyfin").innerText(), /Update/);
    // Filters with their counts (apps).
    const segments = frame.getByRole("group", { name: "Filter" });
    assert.match(await segments.innerText(), /Alle\s*4/);
    assert.match(await segments.innerText(), /Laufen\s*2/);
    assert.match(await segments.innerText(), /Gestoppt\s*1/);
    assert.match(await segments.innerText(), /Updates\s*1/);
    // PLAN 75-19 (style W4): the figures in the status bar, no tiles.
    assert.equal(await frame.locator(".stats").count(), 0, "no figure tiles");
    const bar = frame.locator("footer.statusbar");
    assert.match(await bar.innerText(), /Laufen\s*2 von 4/);
    assert.match(await bar.innerText(), /Updates\s*1 verfügbar/);
    const pinned = await bar.evaluate((el) => {
      const r = el.getBoundingClientRect();
      return Math.abs(r.bottom - innerHeight) <= 1;
    });
    assert.ok(pinned, `status bar at the window's lower edge at ${width}`);
    // A stack opens downward to its containers (style E3).
    await row(frame, "immich")
      .getByRole("button", { name: "Container von immich zeigen" })
      .click();
    assert.equal(await table(frame).locator("tbody tr.member").count(), 3);
    await row(frame, "immich")
      .getByRole("button", { name: "Container von immich zeigen" })
      .click();
    assert.equal(await table(frame).locator("tbody tr.member").count(), 0);
    // No view switch any more.
    assert.equal(
      await frame.getByRole("button", { name: "Karten" }).count(),
      0,
      "no card view",
    );
    // The row's buttons are inside the card (no hidden column at the right).
    const inside = await row(frame, "vaultwarden").evaluate((tr) => {
      const card = tr.closest("section").getBoundingClientRect();
      const buttons = tr.querySelector("td.actions").getBoundingClientRect();
      const wrap = tr.closest(".table-wrap");
      return (
        buttons.right <= card.right + 1 &&
        wrap.scrollWidth <= wrap.clientWidth + 1
      );
    });
    assert.ok(inside, `row actions visible at ${width}`);
    // Nothing overlaps: the table fits the window's page.
    const fits = await frame
      .locator("body")
      .evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1);
    assert.ok(fits, `no side scrolling at ${width}`);
    await page.screenshot({ path: `${out}/01-fenster-${width}.png` });
    if (width === 1600) {
      // Filter "Gestoppt" shows only vaultwarden.
      await segments.getByRole("button", { name: /Gestoppt/ }).click();
      assert.equal(await table(frame).locator("tbody tr").count(), 1);
      await segments.getByRole("button", { name: /Alle/ }).click();
      // Row ⋯: every action reachable.
      await row(frame, "immich")
        .getByRole("button", { name: /Aktionen für immich/ })
        .click();
      const menu = frame.getByRole("menu");
      await menu.waitFor();
      const items = await menu.getByRole("menuitem").allInnerTexts();
      for (const item of [
        "Neu starten",
        "Protokoll: server",
        "Konsole: server",
        "Bearbeiten",
        "Entfernen",
      ])
        assert.ok(
          (await menu
            .getByRole("menuitem", { name: new RegExp(item) })
            .count()) > 0,
          `menu has ${item}: ${items.join(" | ")}`,
        );
      await page.waitForTimeout(250);
      await page.screenshot({ path: `${out}/02-zeilenmenue.png` });
      await page.keyboard.press("Escape");
      // Start/Stop by the row's own button.
      assert.ok(
        await row(frame, "vaultwarden")
          .getByRole("button", { name: "vaultwarden starten" })
          .isVisible(),
      );
      assert.ok(
        await row(frame, "jellyfin")
          .getByRole("button", { name: "jellyfin stoppen" })
          .isVisible(),
      );
      // A wide window (maximised) shows the selected app at the right
      // (style W4): a click selects, a double click opens the app's page.
      await page
        .locator("section.win")
        .first()
        .getByRole("button", { name: "Maximieren" })
        .click();
      await page.waitForTimeout(600);
      await row(frame, "jellyfin").locator("td.state").click();
      const pane = frame.locator("aside.detail-pane");
      await pane.waitFor();
      assert.match(await pane.innerText(), /jellyfin/);
      assert.match(await pane.innerText(), /8096/);
      assert.equal(
        await pane
          .getByRole("link", { name: "Bearbeiten" })
          .getAttribute("href"),
        "/docker/apps/jellyfin",
      );
      await page.screenshot({ path: `${out}/02b-details.png` });
      // A double click opens the app's own page.
      await row(frame, "jellyfin").locator("td.state").dblclick();
      await frame
        .locator("main")
        .getByText("jellyfin", { exact: false })
        .first()
        .waitFor();
      await page.waitForFunction(
        () =>
          [...document.querySelectorAll("section.win iframe")].some((f) =>
            f.contentWindow?.location.pathname.startsWith(
              "/docker/apps/jellyfin",
            ),
          ),
        null,
        { timeout: 10000 },
      );
    }
    await ctx.close();
  }

  // 2. The page alone at 1280 px (keyboard: the app name is a link).
  {
    const { ctx, page } = await open(1280);
    await table(page).waitFor({ timeout: 20000 });
    const link = row(page, "immich").getByRole("link", { name: "immich" });
    assert.equal(await link.getAttribute("href"), "/docker/apps/immich");
    await page.screenshot({ path: `${out}/03-seite-1280.png`, fullPage: true });
    await ctx.close();
  }

  // 3. Phone 390 px: stacked rows – name, state and actions, figures in the sub-line.
  {
    const { ctx, page } = await open(390, { desktop: true, height: 844 });
    await table(page).waitFor({ timeout: 20000 });
    await page.waitForTimeout(600);
    assert.equal(
      await page.locator("table.app-table thead").isVisible(),
      false,
      "no table head on the phone",
    );
    const r = row(page, "jellyfin");
    assert.match(await r.innerText(), /CPU 7 %/);
    const boxes = await r.evaluate((tr) => {
      const box = (s) => tr.querySelector(s)?.getBoundingClientRect();
      const n = box("td.name"),
        s = box("td.state"),
        a = box("td.actions");
      return { n, s, a, w: innerWidth };
    });
    assert.ok(boxes.n.right <= boxes.a.left + 1, "name beside the actions");
    assert.ok(boxes.s.top >= boxes.n.bottom - 1, "state under the name");
    assert.ok(boxes.a.right <= boxes.w, "actions on screen");
    const fits = await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    );
    assert.ok(fits, "no side scrolling on the phone");
    await page.screenshot({ path: `${out}/04-handy-390.png`, fullPage: true });
    await ctx.close();
  }
} finally {
  await browser.close();
}
const relevant = errors.filter(
  (e) => !/Failed to load resource|net::ERR|WebSocket/.test(e),
);
assert.deepEqual(relevant, [], "no page errors");
console.log("docker table UI ok");
