// PLAN 61: the desktop mode – desktop with icons and widgets, programs in
// windows (move, resize, snap, fold the side bar), main menu, context menus,
// switcher, phone home grid. Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5240 node scripts/test-ui-desktop.mjs
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
const out = process.env.UI_OUTPUT || "out/validation/desktop";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const time = "2026-11-06T08:00:00Z";
const hist = (c) =>
  Array.from({ length: 30 }, (_, i) =>
    Math.max(0.2, c + Math.sin(i / 3) * c * 0.4),
  );
const ctr = (name, o = {}) => ({
  id: name,
  name,
  image: name + ":latest",
  state: o.state ?? "running",
  since: time,
  ports: (o.ports ?? []).map((h) => ({
    host: h,
    container: h,
    protocol: "tcp",
  })),
  app: name,
  service: name,
  managed: true,
  cpuPercent: o.cpu ?? 1.2,
  memoryBytes: o.mem ?? 180e6,
  cpuHistory: hist(o.cpu ?? 1.2),
  webui: o.ports ? String(o.ports[0]) : "",
  publicUrl: "",
  proxyUrl: `https://${name}.nas.local`,
  icon: "",
  updateAvailable: !!o.update,
});
const containers = [
  ctr("immich", { ports: [2283], cpu: 4.1, mem: 1.4e9 }),
  ctr("jellyfin", { ports: [8096], cpu: 6.5, mem: 1.1e9, update: true }),
  ctr("paperless", { ports: [8000] }),
  ctr("vaultwarden", { ports: [8081], state: "exited" }),
];

async function open(
  width = 1600,
  height = 1000,
  theme = "graphit",
  path = "/",
  setup = null,
) {
  const made = await create("owner", theme, width);
  await made.page.setViewportSize({ width, height });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  made.data.dockerContainers = containers;
  made.data.dockerApps = containers.map((c) => c.name);
  made.data.prefs.docker = {
    version: 1,
    view: "cards",
    order: [],
    details: true,
    primaryServices: {},
  };
  made.puts = [];
  await made.ctx.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    const method = route.request().method();
    if (p === "/preferences/desktop" && method === "PUT") {
      made.puts.push(route.request().postDataJSON());
      made.data.prefs.desktop = route.request().postDataJSON();
      return route.fulfill({ status: 204 });
    }
    if (p === "/apps/mine")
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found" } },
      });
    if (p.startsWith("/apps/") && p.endsWith("/window"))
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found" } },
      });
    return route.fallback();
  });
  // The Docker apps' own pages do not exist here: an empty page stands in.
  await made.ctx.route("https://*.nas.local/**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "text/html",
      body: '<!doctype html><title>App</title><body style="background:#223;color:#fff;font:16px sans-serif;padding:20px">Oberfläche der App (Testdaten)</body>',
    }),
  );
  if (setup) await setup(made);
  await made.page.goto(base + path);
  await made.page.waitForSelector(".desktop, .home", { timeout: 15000 });
  await made.page.waitForTimeout(800);
  return made;
}
const shot = (page, name) => page.screenshot({ path: `${out}/${name}.png` });

// 1. Desktop: icons, widgets, top bar; no classic head line.
{
  const { ctx, page } = await open();
  assert.equal(
    await page.locator("header.bar-surface").count(),
    0,
    "classic head line hidden",
  );
  await page.locator(".ico", { hasText: "Docker" }).first().waitFor();
  assert.ok(
    await page.locator(".ico", { hasText: "immich" }).count(),
    "Docker app on the desktop",
  );
  assert.ok(await page.locator(".badge.count").count(), "update badge");
  await shot(page, "01-arbeitsplatz");
  // Open Docker: a window with the program's side bar and the real page inside.
  await page.locator(".ico", { hasText: "Docker" }).first().click();
  const win = page.locator("section.win").first();
  await win.waitFor();
  const frame = page.frameLocator("section.win iframe").first();
  await frame
    .locator("h1, .page-head, main")
    .first()
    .waitFor({ timeout: 15000 });
  assert.equal(
    await frame.locator("header.bar-surface").count(),
    0,
    "no head line inside the window",
  );
  assert.equal(
    new URL(page.url()).pathname,
    "/docker",
    "address follows the window",
  );
  await page.waitForTimeout(1500);
  await shot(page, "02-docker-fenster");
  // Side bar: Netzwerke navigates inside the window.
  await win.getByRole("button", { name: "Netzwerke" }).click();
  await page.waitForTimeout(1500);
  assert.equal(
    new URL(page.url()).pathname,
    "/docker/networks",
    "side bar moves the frame",
  );
  // Fold the side bar.
  await win.getByRole("button", { name: "Seitenleiste einklappen" }).click();
  await page.waitForTimeout(300);
  assert.ok(await win.locator("nav.side.folded").count(), "side bar folded");
  await shot(page, "03-seitenleiste-eingeklappt");
  // Resize at the left edge and the bottom right corner.
  const before = await win.boundingBox();
  const west = await win.locator(".edge.w").boundingBox();
  await page.mouse.move(west.x + 2, west.y + 50);
  await page.mouse.down();
  await page.mouse.move(west.x - 80, west.y + 50, { steps: 5 });
  await page.mouse.up();
  const after = await win.boundingBox();
  assert.ok(after.width > before.width + 60, "resized at the left edge");
  // Drag to the left edge: snap preview and half the desktop.
  const bar = await win.locator("header.titlebar").boundingBox();
  await page.mouse.move(bar.x + 300, bar.y + 20);
  await page.mouse.down();
  await page.mouse.move(3, 400, { steps: 12 });
  assert.ok(await page.locator(".snap-preview").count(), "snap preview shown");
  await shot(page, "04-andock-vorschau");
  await page.mouse.up();
  const snapped = await win.boundingBox();
  assert.ok(
    snapped.x <= 2 && Math.abs(snapped.width - 800) < 4,
    "snapped to the left half",
  );
  // A second program and the switcher.
  await page.getByRole("button", { name: "Alle Programme" }).click();
  await page.locator(".launcher .prog", { hasText: "Speicher" }).click();
  await page.waitForTimeout(1200);
  await page.keyboard.down("Alt");
  await page.keyboard.press("q");
  assert.ok(await page.locator(".switcher").count(), "switcher shown");
  await shot(page, "05-app-wechsler");
  await page.keyboard.up("Alt");
  await page.waitForTimeout(200);
  assert.equal(await page.locator(".switcher").count(), 0, "switcher closed");
  // Main menu with search.
  await page.getByRole("button", { name: "Alle Programme" }).click();
  await page
    .getByRole("textbox", { name: "Programme und Seiten suchen …" })
    .fill("netz");
  await page.waitForTimeout(200);
  assert.ok(
    await page
      .locator(".launcher .page", { hasText: "Docker › Netzwerke" })
      .count(),
    "search finds pages",
  );
  await page
    .getByRole("textbox", { name: "Programme und Seiten suchen …" })
    .fill("");
  await shot(page, "06-hauptmenue");
  await page.keyboard.press("Escape");
  // "Show desktop" minimizes every window; then the context menus.
  // PLAN 67: "Arbeitsplatz zeigen" sits in the logo menu of the top bar (and Alt + K).
  await page.locator('header.topbar [data-menu="logo"]').click();
  await page
    .locator(".mb-menu")
    .getByRole("menuitem", { name: "Arbeitsplatz zeigen" })
    .click();
  await page.waitForTimeout(300);
  assert.equal(
    await page.locator("section.win:not(.min)").count(),
    0,
    "all windows minimized",
  );
  await page
    .locator(".ico", { hasText: "Docker" })
    .first()
    .click({ button: "right" });
  await page
    .getByRole("menuitem", { name: "Vom Arbeitsplatz entfernen" })
    .waitFor();
  await shot(page, "07-rechtsklick-symbol");
  await page.keyboard.press("Escape");
  const area = await page.locator(".desk-area").boundingBox();
  // The middle of the desktop: the dock takes the bottom edge (PLAN 62).
  await page.mouse.click(
    area.x + area.width * 0.5,
    area.y + area.height * 0.45,
    {
      button: "right",
    },
  );
  await page.getByRole("menuitem", { name: "Widgets bearbeiten" }).waitFor();
  await shot(page, "08-rechtsklick-arbeitsplatz");
  await page.getByRole("menuitem", { name: "Widgets bearbeiten" }).click();
  await page.getByRole("button", { name: "Widget hinzufügen" }).click();
  await page.getByRole("button", { name: "Uhr in Größe M hinzufügen" }).click();
  await page.getByRole("button", { name: "Fertig" }).last().click();
  await page.waitForTimeout(1800);
  await shot(page, "09-widgets-bearbeiten");
  await ctx.close();
}

// 2. Saved desktop: the account's settings reach the NAS (debounced).
{
  const { ctx, page, puts } = await open();
  await page
    .locator(".ico", { hasText: "immich" })
    .first()
    .click({ button: "right" });
  await page
    .getByRole("menuitem", { name: "Vom Arbeitsplatz entfernen" })
    .click();
  await page.waitForTimeout(2000);
  assert.ok(puts.length >= 1, "desktop saved");
  assert.ok(
    !puts.at(-1).icons.includes("docker:immich") && puts.at(-1).customIcons,
    "icon removed in the saved desktop",
  );
  // The Docker app window: like an app (PLAN 66) – name, no address, ⋯ menu, open-in-tab button.
  await page.locator(".ico", { hasText: "jellyfin" }).first().click();
  await page
    .locator('section.win .titlebar button[aria-label="Mehr"]')
    .waitFor();
  assert.equal(
    await page.locator("section.win .addr").count(),
    0,
    "no address",
  );
  await page.waitForTimeout(800);
  await shot(page, "10-app-fenster");
  await ctx.close();
}

// 3. Light theme and a deep link.
{
  const { ctx, page } = await open(1440, 900, "hell", "/vms");
  await page.locator("section.win").first().waitFor();
  await page.waitForTimeout(1500);
  await shot(page, "11-hell-vms-link");
  // PLAN 62-3: the window's side bar lists the VMs; the page's own list is gone.
  const side = page.locator("section.win nav.side");
  await side.getByText("Deine VMs").waitFor();
  await side.getByRole("button", { name: "Source VM" }).click();
  const vmFrame = page.frameLocator("section.win iframe").first();
  await vmFrame.locator("main").first().waitFor();
  assert.equal(
    await vmFrame.locator("aside.tile").count(),
    0,
    "no VM list inside the window",
  );
  await ctx.close();
}

// PLAN 62-5: a console asked for in a window opens in the Terminal window.
{
  const { ctx, page } = await open(
    1600,
    1000,
    "graphit",
    "/docker/apps/immich",
  );
  const frame = page.frameLocator("section.win iframe").first();
  await frame.getByRole("button", { name: "Konsole" }).click();
  const term = page.locator("section.win", { hasText: "Terminal" });
  await term.waitFor({ timeout: 10000 });
  const termFrame = page.frameLocator("section.win iframe").last();
  await termFrame.locator("section.panel").waitFor({ timeout: 10000 });
  await shot(page, "14-konsole-im-terminal-fenster");
  await ctx.close();
}

// 3b. Cluster (PLAN 64): the node switcher sits right after the menus (PLAN 67),
// names the node and its menu is not cut off by the bar.
{
  const { ctx, page } = await open(1600, 1000, "graphit", "/", (made) =>
    made.ctx.route("**/api/v1/cluster/nodes", (r) =>
      r.fulfill({
        json: {
          manages: true,
          version: "0.44.0",
          nodes: [
            {
              id: "n1",
              name: "titan",
              self: true,
              online: true,
              version: "0.44.0",
              compatible: true,
            },
            {
              id: "n2",
              name: "thor",
              self: false,
              online: true,
              version: "0.44.0",
              compatible: true,
            },
          ],
        },
      }),
    ),
  );
  const bar = page.locator("header.topbar");
  const trigger = bar.getByRole("button", { name: /titan/ });
  await trigger.waitFor();
  assert.ok((await trigger.innerText()).includes("titan"), "node name shown");
  // PLAN 67: right after the menus of the program in front.
  const menus = await bar.locator(".menubar").boundingBox();
  const node = await trigger.boundingBox();
  assert.ok(
    node.x >= menus.x + menus.width - 1 &&
      node.x - (menus.x + menus.width) < 30,
    "switcher right after the menus",
  );
  await trigger.click();
  const item = page.getByRole("menuitemradio", { name: /thor/ });
  await item.waitFor();
  const box = await item.boundingBox();
  const hit = await page.evaluate(
    ({ x, y }) =>
      document.elementFromPoint(x, y)?.closest("[role=menuitemradio]")
        ?.textContent ?? "",
    { x: box.x + box.width / 2, y: box.y + box.height / 2 },
  );
  assert.ok(hit.includes("thor"), "node menu visible, not clipped by the bar");
  await shot(page, "14-cluster-umschalter");
  await ctx.close();
}

// 4. Phone: home grid, a program full screen with a way back.
{
  const { ctx, page } = await open(390, 844);
  await page.locator(".home .app", { hasText: "Docker" }).first().waitFor();
  await shot(page, "12-handy-start");
  await page.locator(".home .app", { hasText: "Docker" }).first().click();
  await page.locator(".phone-bar").waitFor();
  await page.waitForTimeout(1200);
  await shot(page, "13-handy-docker");
  await page.getByRole("button", { name: "Zum Startbildschirm" }).click();
  await page.locator(".home").waitFor();
  await ctx.close();
}

// 5. PLAN 62: dock, icons placed freely, widget menu, menu closes beside it, bar size.
{
  const { ctx, page } = await open();
  const dock = page.locator("nav.dock");
  await dock.waitFor();
  assert.ok((await dock.locator(".slot").count()) >= 3, "dock with programs");
  await shot(page, "10-dock");
  // Opening from the dock: the window gets a dot under its icon.
  await dock.getByRole("button", { name: "Dateien" }).click();
  await page.locator("section.win").first().waitFor();
  assert.equal(
    await dock.locator('[data-key="app:files"] .dot.on').count(),
    1,
    "open program marked in the dock",
  );
  await dock.getByRole("button", { name: "Dateien" }).click();
  await page.waitForTimeout(300);
  // Right click on the dock itself: its own menu and the edit mode.
  const d = await dock.boundingBox();
  await page.mouse.click(d.x + 4, d.y + d.height / 2, { button: "right" });
  await page.getByRole("menuitem", { name: "Dock bearbeiten" }).click();
  await page.getByRole("group", { name: "Dock bearbeiten" }).waitFor();
  await page.getByRole("radio", { name: "Stark" }).click();
  await page.locator(".dock-edit input[type=range]").first().fill("64");
  await shot(page, "11-dock-bearbeiten");
  await page
    .getByRole("group", { name: "Dock bearbeiten" })
    .getByRole("button", { name: "Fertig" })
    .click();
  // Right click on an icon in the dock: the program's menu.
  await dock.getByRole("button", { name: "Docker" }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "Aus dem Dock entfernen" }).click();
  await page.waitForTimeout(200);
  assert.equal(
    await dock.getByRole("button", { name: "Docker", exact: true }).count(),
    0,
    "unpinned",
  );
  // PLAN 67: dragging a program from the desktop or from the main menu into the dock pins it.
  await page
    .locator(".icons > .ico", { hasText: "Docker" })
    .first()
    .dragTo(dock, { force: true });
  await dock
    .getByRole("button", { name: "Docker", exact: true })
    .waitFor({ timeout: 5000 });
  await page.getByRole("button", { name: "Alle Programme" }).first().click();
  await page.locator(".launcher").waitFor();
  await page
    .locator(".launcher .prog", { hasText: "Sicherung" })
    .first()
    .dragTo(dock, { force: true });
  await dock
    .getByRole("button", { name: "Sicherung", exact: true })
    .waitFor({ timeout: 5000 });
  await page.keyboard.press("Escape");
  // An icon dragged to a free spot stays there.
  const icon = page.locator(".icons > .ico", { hasText: "Speicher" });
  const before = await icon.boundingBox();
  const area = await page.locator(".desk-area").boundingBox();
  await icon.dragTo(page.locator(".desk-area"), {
    targetPosition: { x: area.width * 0.45, y: area.height * 0.35 },
    force: true,
  });
  await page.waitForTimeout(400);
  const after = await icon.boundingBox();
  assert.ok(after.x - before.x > 200, "icon moved freely to the right");
  // PLAN 66: while an icon is dragged the grid shows, the target cell is
  // marked, the icon stays faint in its place; all gone after the drop.
  {
    const moving = page.locator(".icons > .ico", { hasText: "Freigaben" });
    const box = await moving.boundingBox();
    await page.mouse.move(box.x + box.width / 2, box.y + 24);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + 20, box.y + 44, { steps: 4 });
    await page.mouse.move(
      area.x + area.width * 0.4,
      area.y + area.height * 0.55,
      { steps: 12 },
    );
    await page.locator(".drag-grid .cell.target").waitFor({ timeout: 3000 });
    assert.ok(
      (await page.locator(".drag-grid .cell").count()) > 20,
      "grid of all cells",
    );
    assert.equal(
      await page.locator(".drag-grid .cell.target").count(),
      1,
      "one target cell",
    );
    assert.equal(
      await page.locator(".ico.lifted").count(),
      1,
      "dragged icon faint in place",
    );
    // The grid reaches down to the dock: less than one row is left above it.
    const cells = await page
      .locator(".drag-grid .cell")
      .evaluateAll((els) =>
        Math.max(...els.map((e) => e.getBoundingClientRect().bottom)),
      );
    const dockTop =
      (await page.locator(".dock").first().boundingBox())?.y ??
      area.y + area.height;
    assert.ok(
      dockTop - cells < 40,
      `grid ends at the dock (${dockTop - cells} px)`,
    );
    await shot(page, "11b-raster-beim-verschieben");
    await page.mouse.up();
    await page.waitForTimeout(300);
    assert.equal(
      await page.locator(".drag-grid").count(),
      0,
      "grid gone after the drop",
    );
    assert.equal(
      await page.locator(".ico.lifted").count(),
      0,
      "icon back to full strength",
    );
  }
  // Widgets: right click, hide all; back from the desktop menu.
  await page.locator(".widget").first().click({ button: "right" });
  await page.getByRole("menuitem", { name: "Widget entfernen" }).waitFor();
  await shot(page, "12-widget-rechtsklick");
  await page.getByRole("menuitem", { name: "Alle Widgets ausblenden" }).click();
  assert.equal(await page.locator(".widget").count(), 0, "widgets hidden");
  await page.mouse.click(
    area.x + area.width * 0.6,
    area.y + area.height * 0.6,
    { button: "right" },
  );
  await page
    .getByRole("menuitemcheckbox", { name: "Widgets anzeigen" })
    .click();
  assert.ok((await page.locator(".widget").count()) > 0, "widgets back");
  // The menu closes on a click beside it.
  await page.getByRole("button", { name: "Alle Programme" }).first().click();
  await page.locator(".launcher").waitFor();
  await page.mouse.click(area.x + area.width - 300, area.y + area.height * 0.5);
  await page.locator(".launcher").waitFor({ state: "detached", timeout: 3000 });
  // The bars at the top: size by right click.
  await page
    .locator("header.topbar")
    .click({ button: "right", position: { x: 700, y: 6 } });
  await page.getByRole("menuitemradio", { name: /Groß/ }).click();
  const pb = await page.locator("header.topbar .pb").first().boundingBox();
  assert.ok(pb.height >= 40, "large bar");
  await shot(page, "13-leisten-gross");
  await ctx.close();
}

// PLAN 65-5: the desktop by keyboard – menu with arrow keys, focus into a
// new window and back after closing it, dock with arrow keys, context menu
// with the menu key.
{
  const { ctx, page } = await open();
  const active = () =>
    page.evaluate(() => {
      const el = document.activeElement;
      return {
        tag: el?.tagName,
        opens: el?.getAttribute("data-opens"),
        cls: el?.className,
        label: el?.getAttribute("aria-label") ?? el?.textContent?.trim(),
      };
    });
  await page.keyboard.press("Control+Space");
  await page.locator(".launcher").waitFor();
  await page.keyboard.press("ArrowDown");
  assert.match((await active()).cls, /prog/, "arrow down: first program");
  await page.keyboard.press("ArrowRight");
  const second = (await active()).label;
  await page.keyboard.press("ArrowLeft");
  assert.notEqual((await active()).label, second, "arrow left goes back");
  await page.keyboard.press("ArrowUp");
  assert.equal(
    (await active()).tag,
    "INPUT",
    "arrow up from the first row: search",
  );
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  await page.locator("section.win").first().waitFor();
  await page.waitForFunction(
    () => document.activeElement?.tagName === "IFRAME",
    null,
    { timeout: 10000 },
  );
  // Inside the window Alt+W closes it; the focus comes back to the desktop.
  // The page in the frame must have loaded (it forwards the desktop keys).
  await page
    .frameLocator("section.win iframe")
    .first()
    .locator("main.in-window")
    .waitFor({ timeout: 15000 });
  await page.keyboard.press("Alt+w");
  await page.locator("section.win").first().waitFor({ state: "detached" });
  await page.waitForTimeout(100);
  assert.equal(
    (await active()).opens,
    "launcher",
    "focus back on the menu button",
  );
  // The dock: arrow keys move along it.
  const dockButtons = page.locator("nav.dock button.it");
  await dockButtons.first().focus();
  await page.keyboard.press("ArrowRight");
  assert.equal(
    await page.evaluate(() =>
      [...document.querySelectorAll("nav.dock button.it")].indexOf(
        document.activeElement,
      ),
    ),
    1,
    "dock: arrow right",
  );
  // The menu key on a desktop icon opens its menu; arrows and Escape work.
  await page.locator(".icons > .ico").first().focus();
  await page.keyboard.press("Shift+F10");
  await page.locator("[role=menu]").first().waitFor();
  await page.keyboard.press("ArrowDown");
  assert.match((await active()).tag, /BUTTON|A/, "menu item focused");
  await page.keyboard.press("Escape");
  await page.locator("[role=menu]").waitFor({ state: "detached" });
  // Every control of a window's title bar has a name.
  await page.locator(".icons > .ico", { hasText: "Docker" }).first().click();
  const unnamed = await page
    .locator("section.win header.titlebar button")
    .evaluateAll(
      (els) => els.filter((b) => !b.getAttribute("aria-label")).length,
    );
  assert.equal(unnamed, 0, "title bar buttons are labelled");
  await shot(page, "15-tastatur");
  await ctx.close();
}

// PLAN 65-10: icons keep their places when the browser window shrinks and
// grows; a move in the small window changes only the moved icon.
{
  const stored = {
    "app:files": { c: 0, r: 0 },
    "app:shares": { c: 5, r: 1 },
    "app:docker": { c: 12, r: 0 },
    "app:storage": { c: 12, r: 3 },
  };
  const { ctx, page, puts } = await open(1600, 1000, "graphit", "/", (made) => {
    made.data.prefs.desktop = {
      version: 1,
      sort: "free",
      iconPos: stored,
      widgetsHidden: true,
    };
  });
  const places = () =>
    page.evaluate(() =>
      Object.fromEntries(
        [...document.querySelectorAll(".icons > .ico[data-key]")].map((el) => [
          el.dataset.key,
          el.style.left + "," + el.style.top,
        ]),
      ),
    );
  const resize = async (width) => {
    const docker = await page
      .locator('.ico[data-key="app:docker"]')
      .getAttribute("style");
    await page.setViewportSize({ width, height: 1000 });
    await page.waitForFunction(
      (before) =>
        document
          .querySelector('.ico[data-key="app:docker"]')
          ?.getAttribute("style") !== before,
      docker,
      { timeout: 5000 },
    );
    await page.waitForTimeout(200);
  };
  await page.locator('.ico[data-key="app:docker"]').waitFor();
  const wide = await places();
  const colW = parseFloat(wide["app:docker"]) / 12;
  const rowH = parseFloat(wide["app:storage"].split(",")[1]) / 3;
  assert.ok(colW > 0 && rowH > 0, "stored places shown at 1600");
  await resize(900);
  const narrow = await places();
  assert.notEqual(
    narrow["app:docker"],
    wide["app:docker"],
    "pulled inside at 900",
  );
  assert.equal(narrow["app:files"], wide["app:files"], "files stays at 900");
  await resize(1600);
  assert.deepEqual(await places(), wide, "1600 → 900 → 1600 keeps every place");
  // Move "Freigaben" at 900 to a free cell; nothing else changes.
  await resize(900);
  const area = await page.locator(".desk-area").boundingBox();
  const icons = await page.locator(".icons").boundingBox();
  const putsBefore = puts.length;
  await page
    .locator('.ico[data-key="app:shares"]')
    .dragTo(page.locator(".desk-area"), {
      targetPosition: {
        x: icons.x - area.x + 2 * colW + colW / 2,
        y: icons.y - area.y + 4 * rowH + rowH / 2,
      },
      force: true,
    });
  // Saved a moment after the last change (lib/desktop/wm: 1.5 s).
  await page.waitForTimeout(2200);
  assert.ok(puts.length > putsBefore, "the move is saved");
  const saved = puts.at(-1).iconPos;
  assert.deepEqual(saved["app:shares"], { c: 2, r: 4 }, "moved icon stored");
  for (const key of ["app:files", "app:docker", "app:storage"])
    assert.deepEqual(saved[key], stored[key], key + " keeps its stored place");
  await resize(1600);
  const back = await places();
  for (const key of ["app:files", "app:docker", "app:storage"])
    assert.equal(back[key], wide[key], key + " back at its place at 1600");
  assert.equal(
    back["app:shares"],
    `${2 * colW}px,${4 * rowH}px`,
    "moved icon at 1600",
  );
  await shot(page, "14-symbole-plaetze");
  await ctx.close();
}

// PLAN 69: the dock's menu button opens all programs above the dock; the
// widget edit bar sits clear of the dock.
{
  const { ctx, page } = await open(1440, 900);
  const button = page
    .locator(".dock")
    .getByRole("button", { name: "Alle Apps" });
  await button.waitFor();
  const first = await page.locator(".dock .slot").first().getAttribute("class");
  assert.match(first, /menu-slot/, "menu button first in the dock");
  await button.click();
  const launcher = page.locator(".launcher");
  await launcher.waitFor();
  assert.equal(await button.getAttribute("aria-expanded"), "true");
  const lb = await launcher.boundingBox();
  const db = await page.locator(".dock").boundingBox();
  assert.ok(
    lb.y + lb.height <= db.y + 1,
    `menu above the dock: ${lb.y + lb.height} > ${db.y}`,
  );
  await launcher
    .getByRole("button", { name: "Docker", exact: true })
    .first()
    .waitFor();
  await button.click();
  await launcher.waitFor({ state: "detached" });
  // The search button of the top bar still opens it under the bar.
  await page.locator('[data-opens="launcher"]').first().click();
  const top = await page.locator(".launcher").boundingBox();
  assert.ok(top.y < 120, `menu from the top bar at the top: ${top.y}`);
  await page.keyboard.press("Escape");
  const area = await page.locator(".desk-area").boundingBox();
  await page.mouse.click(
    area.x + area.width * 0.5,
    area.y + area.height * 0.45,
    { button: "right" },
  );
  await page.getByRole("menuitem", { name: "Widgets bearbeiten" }).click();
  const bar = await page.locator(".edit-bar").boundingBox();
  const dock = await page.locator(".dock").boundingBox();
  assert.ok(
    bar.y + bar.height <= dock.y,
    `edit bar above the dock: ${bar.y + bar.height} > ${dock.y}`,
  );
  await shot(page, "15-dock-menue-widgetleiste");
  await ctx.close();
}

await browser.close();
assert.deepEqual(errors, [], "no script errors");
console.log("desktop UI ok");
