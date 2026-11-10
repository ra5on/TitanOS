// PLAN 67-1: the top bar variant B with a menu bar per program – layout at
// 1600/1280/1024 px without overlap (long names cut off), keyboard (F10,
// Alt, arrows, Esc), a page command (Dateien › Ablage › Neuer Ordner),
// disabled entries without a handler, the logo menu's dialogs and the
// cluster switcher. Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5282 node scripts/test-ui-menubar.mjs
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
const out = process.env.UI_OUTPUT || "out/validation/menubar";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const shot = (page, name) => page.screenshot({ path: `${out}/${name}.png` });

const LONG = "paperless-ngx-document-archive-with-a-long-name";
const ctr = (name, state = "running") => ({
  id: name,
  name,
  image: name + ":latest",
  state,
  since: "2026-11-06T08:00:00Z",
  ports: [{ host: 8000, container: 8000, protocol: "tcp" }],
  app: name,
  service: name,
  managed: true,
  cpuPercent: 1,
  memoryBytes: 100e6,
  cpuHistory: [1, 1, 1],
  webui: "8000",
  publicUrl: "",
  proxyUrl: `https://${name}.nas.local`,
  icon: "",
  updateAvailable: false,
});

async function open(width = 1600, path = "/", setup = null) {
  const made = await create("owner", "graphit", width);
  await made.page.setViewportSize({ width, height: 900 });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  made.data.dockerContainers = [ctr("immich"), ctr(LONG)];
  made.data.dockerApps = ["immich", LONG];
  await made.ctx.route("**/api/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    if (p === "/preferences/desktop" && route.request().method() === "PUT")
      return route.fulfill({ status: 204 });
    if (p === "/apps/mine" || (p.startsWith("/apps/") && p.endsWith("/window")))
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found" } },
      });
    if (p === "/update")
      return route.fulfill({ json: { available: { version: "9.9.9" } } });
    return route.fallback();
  });
  if (setup) await setup(made);
  await made.page.goto(base + path);
  await made.page.waitForSelector(".desktop", { timeout: 30000 });
  await made.page.waitForTimeout(800);
  return made;
}
const bar = (page) => page.locator("header.topbar");
const top = (page, name) =>
  bar(page).locator(".menubar").getByRole("menuitem", { name, exact: true });
const anyItem = (page, name) =>
  page.locator(`.mb-menu [role^="menuitem"]`, { hasText: name }).first();

// 1. Layout at three widths: one bar, nothing overlaps, long names are cut off.
for (const width of [1600, 1280, 1024]) {
  const { ctx, page } = await open(width, "/vms");
  await page.locator("section.win").first().waitFor();
  await top(page, "Maschine").waitFor();
  const header = await bar(page).boundingBox();
  assert.ok(
    header.y === 0 && Math.abs(header.width - width) < 2,
    "one bar across",
  );
  assert.ok(
    header.height >= 34 && header.height <= 48,
    `bar height ${header.height}`,
  );
  const lastMenu = await top(page, "Hilfe").boundingBox();
  const status = await bar(page).locator(".status").boundingBox();
  assert.ok(
    lastMenu.x + lastMenu.width <= status.x,
    `menus and status do not overlap at ${width} (${lastMenu.x + lastMenu.width} > ${status.x})`,
  );
  assert.ok(status.x + status.width <= width, "status inside the screen");
  // No window icons in the bar any more: they live in the dock.
  assert.equal(await bar(page).locator(".task").count(), 0, "no task icons");
  // The window starts below the bar.
  const win = await page.locator("section.win").first().boundingBox();
  assert.ok(win.y >= header.y + header.height - 1, "window below the bar");
  await shot(page, `01-leiste-vms-${width}`);
  await ctx.close();
}
{
  // A long program name (a Docker app) is shown whole, the menus stay whole.
  const { ctx, page } = await open(1024);
  await page.locator(".ico", { hasText: "paperless" }).first().dblclick();
  await page.locator("section.win").first().waitFor();
  await page.waitForTimeout(600);
  const name = bar(page).locator(".program");
  assert.match(await name.innerText(), /paperless/);
  // PLAN 76: nothing is cut off, a long name neither.
  const cut = await name.evaluate((el) => el.scrollWidth > el.clientWidth + 1);
  assert.ok(!cut, "long name shown whole");
  const help = await top(page, "Hilfe").boundingBox();
  const status = await bar(page).locator(".status").boundingBox();
  assert.ok(help.x + help.width <= status.x, "menus beside the status");
  await shot(page, "02-langer-name-1024");
  await ctx.close();
}

// 2. Keyboard: F10 and Alt into the bar, arrows, Enter, Esc.
{
  const { ctx, page } = await open(1600);
  const active = () =>
    page.evaluate(() => ({
      menu: document.activeElement?.getAttribute("data-menu"),
      id: document.activeElement?.getAttribute("data-id"),
      text: document.activeElement?.textContent?.trim(),
    }));
  await page.mouse.click(800, 600);
  await page.keyboard.press("F10");
  assert.equal((await active()).menu, "file", "F10: first menu (Ablage)");
  await page.keyboard.press("ArrowRight");
  assert.equal((await active()).menu, "view", "arrow right: next menu");
  await page.keyboard.press("Enter");
  await page.locator(".mb-menu").first().waitFor();
  assert.ok((await active()).id, "Enter opens, first entry focused");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowRight");
  // Right on an entry without a submenu: the next menu opens.
  await page.waitForTimeout(100);
  await page.keyboard.press("ArrowLeft");
  await page.waitForTimeout(100);
  await shot(page, "03-tastatur-menue");
  await page.keyboard.press("Escape");
  await page.waitForTimeout(100);
  assert.equal(await page.locator(".mb-menu").count(), 0, "Esc closes");
  assert.ok((await active()).menu, "focus back on the bar");
  await page.keyboard.press("Escape");
  assert.equal(
    (await active()).menu ?? null,
    null,
    "second Esc leaves the bar",
  );
  // Alt alone, also while a window's page has the focus.
  await page.keyboard.press("Alt");
  assert.equal((await active()).menu, "file", "Alt alone: into the bar");
  await page.keyboard.press("Escape");
  // Alt + Q stays the switcher (Alt was not alone).
  await top(page, "Ablage").click();
  await page.locator(".mb-menu").first().waitFor();
  // While one menu is open, pointing at another switches to it.
  await top(page, "Ansicht").hover();
  await anyItem(page, "Widgets anzeigen").waitFor();
  await page.keyboard.press("Escape");
  await ctx.close();
}

// 3. Dateien › Ablage › Neuer Ordner runs the page's command in its window;
// entries without a handler are disabled, never broken.
{
  const { ctx, page, data: filesData } = await open(1600, "/files");
  await page.locator("section.win").first().waitFor();
  const frame = page.frameLocator("section.win iframe").first();
  await frame.locator(".file-entries, .browser, main").first().waitFor();
  await page.waitForTimeout(1500);
  await top(page, "Ablage").click();
  const mkdirItem = anyItem(page, "Neuer Ordner");
  await mkdirItem.waitFor();
  await page.waitForFunction(
    () =>
      !document
        .querySelector('.mb-menu [data-id="files.mkdir"]')
        ?.hasAttribute("disabled"),
    null,
    { timeout: 10000 },
  );
  assert.ok(
    await page.locator('.mb-menu [data-id="files.rename"]').isDisabled(),
    "Umbenennen without a chosen file: disabled",
  );
  await page.waitForTimeout(250);
  await shot(page, "04-dateien-ablage");
  await mkdirItem.click();
  await frame
    .getByRole("dialog")
    .filter({ hasText: "Neuer Ordner" })
    .waitFor({ timeout: 10000 });
  assert.equal(
    await page.evaluate(() => document.activeElement?.tagName),
    "IFRAME",
    "the keyboard goes back into the window",
  );
  await shot(page, "05-neuer-ordner-im-fenster");
  await frame.getByRole("button", { name: "Abbrechen" }).click();
  // F10, Esc and Alt from inside the window's page (keys pass through its frame).
  const focused = () =>
    page.evaluate(() => ({
      tag: document.activeElement?.tagName,
      menu: document.activeElement?.getAttribute("data-menu"),
    }));
  await page.locator("section.win iframe").first().focus();
  await frame.locator("body").press("F10");
  assert.equal((await focused()).menu, "file", "F10 in the window's page");
  await page.keyboard.press("Escape");
  assert.equal(
    (await focused()).tag,
    "IFRAME",
    "Esc goes back into the window",
  );
  await frame.locator("body").press("Alt");
  assert.equal(
    (await focused()).menu,
    "file",
    "Alt alone in the window's page",
  );
  await page.keyboard.press("Escape");
  // Ansicht › Versteckte Dateien shows its check and follows the page.
  await top(page, "Ansicht").click();
  const hidden = page.locator('.mb-menu [data-id="files.hidden"]');
  assert.equal(await hidden.getAttribute("aria-checked"), "false");
  await hidden.click();
  await page.waitForTimeout(400);
  await top(page, "Ansicht").click();
  await page.waitForFunction(
    () =>
      document
        .querySelector('.mb-menu [data-id="files.hidden"]')
        ?.getAttribute("aria-checked") === "true",
    null,
    { timeout: 5000 },
  );
  await page.keyboard.press("Escape");
  // Gehe zu › Freigaben: a list the page supplies; choosing one opens it.
  await top(page, "Gehe zu").click();
  await page.locator('.mb-menu [data-id="files.shares"]').click();
  const media = page.locator(".mb-menu [role=menuitemradio]", {
    hasText: "Media",
  });
  await media.waitFor();
  await page.waitForTimeout(250);
  await shot(page, "05b-gehe-zu-freigaben");
  await media.click();
  await page.waitForTimeout(800);
  assert.ok(
    filesData.files.includes("share/Media"),
    "Media opened in the window",
  );
  // Fenster › Minimieren works on the window in front.
  await top(page, "Fenster").click();
  await anyItem(page, "Minimieren").click();
  await page.locator("section.win.min").waitFor({ state: "attached" });
  assert.match(await bar(page).locator(".program").innerText(), /Arbeitsplatz/);
  await ctx.close();
}

// 3b. In the terminal F10 belongs to the shell (mc, htop), not to the bar.
{
  const { ctx, page } = await open(1600, "/terminal");
  await page.locator("section.win").first().waitFor();
  const frame = page.frameLocator("section.win iframe").first();
  const xterm = frame.locator(".xterm-helper-textarea").first();
  if (await xterm.count().catch(() => 0)) {
    await xterm.focus();
    await xterm.press("F10");
    assert.equal(
      await page.evaluate(() =>
        document.activeElement?.getAttribute("data-menu"),
      ),
      null,
      "F10 stays in the terminal",
    );
  } else {
    await frame.locator("section.panel").waitFor({ timeout: 10000 });
  }
  await ctx.close();
}

// 4. Docker: Ansicht › filters are choices the page marks and follows.
{
  const { ctx, page } = await open(1600, "/docker");
  await page.locator("section.win").first().waitFor();
  await page.waitForTimeout(2000);
  await top(page, "Ansicht").click();
  await page.waitForFunction(
    () =>
      [...document.querySelectorAll('.mb-menu [role="menuitemradio"]')].some(
        (el) => el.getAttribute("aria-checked") === "true",
      ),
    null,
    { timeout: 10000 },
  );
  await page.locator('.mb-menu [data-id="docker.filter.running"]').click();
  await top(page, "Ansicht").click();
  await page.waitForFunction(
    () =>
      document
        .querySelector('.mb-menu [data-id="docker.filter.running"]')
        ?.getAttribute("aria-checked") === "true",
    null,
    { timeout: 5000 },
  );
  await page.waitForTimeout(250);
  await shot(page, "06-docker-ansicht");
  await ctx.close();
}

// 5. The logo menu opens the right dialogs and programs.
{
  const { ctx, page } = await open(1600);
  const logo = bar(page).locator('[data-menu="logo"]');
  await logo.click();
  await anyItem(page, "Updates").waitFor();
  assert.ok(
    await page.locator('.mb-menu [data-id="sys.updates"] .count').count(),
    "updates with their number",
  );
  await page.waitForTimeout(250);
  await shot(page, "07-logo-menue");
  await anyItem(page, "Herunterfahren").click();
  const power = page.getByRole("dialog").filter({ hasText: "herunterfahren" });
  await power.first().waitFor();
  await page.getByRole("button", { name: "Abbrechen" }).click();
  await logo.click();
  await anyItem(page, "Arbeitsplatz zeigen").waitFor();
  await page.keyboard.press("Escape");
  await top(page, "Hilfe").click();
  await anyItem(page, "Tastenkürzel").click();
  await page.getByRole("dialog").filter({ hasText: "Alt" }).first().waitFor();
  await shot(page, "08-tastenkuerzel");
  await page.keyboard.press("Escape");
  await page.waitForTimeout(200);
  await logo.click();
  await anyItem(page, "Über TitanOS").click();
  await page.locator("section.win").first().waitFor();
  assert.match(
    await bar(page).locator(".program").innerText(),
    /System\u00adsteuerung|Systemsteuerung/,
    "Über TitanOS opens the Systemsteuerung",
  );
  // Arbeitsplatz zeigen by keyboard (Alt + K).
  await page.waitForTimeout(1500);
  await page.keyboard.press("Alt+k");
  await page.locator("section.win.min").waitFor({ state: "attached" });
  // Right click on the bar: its size; large makes it taller.
  await bar(page).click({ button: "right", position: { x: 700, y: 10 } });
  await page.getByRole("menuitemradio", { name: /Groß/ }).click();
  await page.waitForTimeout(200);
  assert.ok((await bar(page).boundingBox()).height >= 46, "large bar");
  await ctx.close();
}

// 6. Cluster: the node switcher sits in the bar after the menus.
{
  const { ctx, page } = await open(1600, "/", (made) =>
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
  const trigger = bar(page).getByRole("button", { name: /titan/ });
  await trigger.waitFor();
  const help = await top(page, "Hilfe").boundingBox();
  const node = await trigger.boundingBox();
  assert.ok(node.x >= help.x + help.width, "switcher after the menus");
  await trigger.click();
  await page.getByRole("menuitemradio", { name: /thor/ }).waitFor();
  await shot(page, "09-cluster");
  await ctx.close();
}

await browser.close();
const relevant = errors.filter(
  (e) => !/Failed to load resource|net::ERR|WebSocket/.test(e),
);
assert.deepEqual(relevant, [], "no page errors");
console.log("menubar UI test passed");
