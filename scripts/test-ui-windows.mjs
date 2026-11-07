// PLAN 64: every page in a window of the desktop looks like a window app –
// the title bar names "Program › Page", the page shows no second big
// heading, its actions sit in one tool bar under the title bar, nothing
// scrolls sideways and nothing lies over the tool bar. Checked at window
// widths of 800 and 1100 px and on the phone (390 px). Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5260 node scripts/test-ui-windows.mjs
// WINDOWS_ALL=1 also photographs every page of every program (no assertions
// beyond overflow), for a visual pass.
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
const out = process.env.UI_OUTPUT || "out/validation/windows";
await mkdir(out, { recursive: true });
const errors = [];
// WINDOWS_REPORT=1 lists findings instead of stopping at the first one.
const failures = [];
function check(ok, message) {
  if (ok) return;
  if (process.env.WINDOWS_REPORT) failures.push(message);
  else assert.fail(message);
}
const create = fixtureFactory(browser, errors);

const time = "2026-11-06T08:00:00Z";
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
  cpuHistory: Array.from({ length: 30 }, () => o.cpu ?? 1.2),
  webui: o.ports ? String(o.ports[0]) : "",
  publicUrl: "",
  proxyUrl: "",
  icon: "",
  updateAvailable: !!o.update,
});
const containers = [
  ctr("immich", { ports: [2283], cpu: 4.1, mem: 1.4e9 }),
  ctr("jellyfin", { ports: [8096], cpu: 6.5, mem: 1.1e9, update: true }),
  ctr("paperless", { ports: [8000] }),
  ctr("vaultwarden", { ports: [8081], state: "exited" }),
];

// [program, path, page name in the title bar (or null), tool bar expected].
// Pages without actions have no tool bar: an empty bar would only take room.
const representative = [
  ["docker", "/docker", "Container", true],
  ["docker", "/docker/networks", "Netzwerke", true],
  ["storage", "/storage", "Übersicht", true],
  ["storage", "/storage/smart", null, true],
  ["shares", "/shares", null, true],
  ["vms", "/vms?vm=source", "Source VM", true],
  ["files", "/files", null, false],
  ["vms", "/vms/media", null, false],
  ["lxc", "/lxc", null, true],
  ["appcenter", "/apps", null, true],
  ["backup", "/settings/backups", null, true],
  ["monitor", "/tools/taskmanager", null, false],
  ["logs", "/tools/logs", null, false],
  ["planner", "/tools/scripts", null, false],
  ["users", "/users", null, false],
  ["security", "/settings/firewall", null, false],
  ["account", "/account", null, false],
  ["settings", "/settings/updates", "Updates", true],
  ["settings", "/settings/network", null, true],
  ["settings", "/settings/about", null, false],
];

const everything = [
  ["files", ["/files", "/settings/files"]],
  ["shares", ["/shares", "/settings/services"]],
  [
    "storage",
    [
      "/storage",
      "/storage/manage",
      "/storage/smart",
      "/storage/import",
      "/storage/replace",
      "/tools/disks",
      "/settings/memory",
    ],
  ],
  [
    "docker",
    [
      "/docker",
      "/docker/images",
      "/docker/volumes",
      "/docker/networks",
      "/docker/cleanup",
      "/settings/proxy",
    ],
  ],
  [
    "vms",
    [
      "/vms",
      "/vms?vm=source",
      "/vms?vm=source&tab=hardware",
      "/vms?vm=source&tab=schedule",
      "/vms/new",
      "/vms/media",
    ],
  ],
  ["lxc", ["/lxc"]],
  ["appcenter", ["/apps", "/apps/new", "/plugins", "/plugins/cleanup"]],
  [
    "backup",
    [
      "/settings/backups",
      "/settings/backups/data",
      "/settings/backups/applications",
      "/settings/backups/configuration",
      "/settings/backups/coverage",
      "/settings/backups/restore",
      "/settings/stick",
    ],
  ],
  [
    "monitor",
    [
      "/tools/taskmanager",
      "/tools/monitoring",
      "/tools/tasks",
      "/tools/hardware",
      "/settings/hardware",
      "/settings/temperatures",
    ],
  ],
  ["logs", ["/tools/logs", "/tools/activity", "/tools/bundle"]],
  ["planner", ["/tools/scripts", "/tools/maintenance", "/settings/energy"]],
  ["terminal", ["/terminal"]],
  ["users", ["/users", "/settings/programs", "/settings/access"]],
  ["security", ["/settings/firewall"]],
  ["account", ["/account"]],
  [
    "settings",
    [
      "/settings",
      "/settings/about",
      "/settings/updates",
      "/settings/timezone",
      "/settings/appearance",
      "/settings/appearance?tab=glass",
      "/settings/appearance?tab=background",
      "/settings/appearance?tab=dock",
      "/settings/appearance?tab=login",
      "/settings/ups",
      "/settings/power-button",
      "/settings/viewer",
      "/settings/network",
      "/settings/wireguard",
      "/settings/ddns",
      "/settings/letsencrypt",
      "/settings/notifications",
      "/settings/homeassistant",
      "/settings/agent",
      "/settings/mcp",
      "/settings/api",
      "/settings/cluster",
      "/settings/ipv64",
    ],
  ],
];

async function prepare(made) {
  made.data.dockerContainers = containers;
  made.data.dockerApps = containers.map((c) => c.name);
  made.data.prefs.docker = {
    version: 1,
    view: "list",
    order: [],
    details: true,
    primaryServices: {},
  };
  await made.ctx.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    const method = route.request().method();
    if (p === "/preferences/desktop" && method === "PUT")
      return route.fulfill({ status: 204 });
    // Answers the shared fixtures do not give in the shape these pages read.
    if (p === "/users" && method === "GET")
      return route.fulfill({
        json: [
          {
            name: "alex",
            role: "owner",
            created: time,
            smbReady: true,
            twoFactor: true,
            disabled: false,
          },
          {
            name: "sam",
            role: "user",
            created: time,
            smbReady: true,
            twoFactor: false,
            disabled: false,
          },
        ],
      });
    if (p === "/account/notifications" && method === "GET")
      return route.fulfill({
        json: {
          delivery: {},
          settings: {
            language: "de",
            email: { enabled: false, to: "" },
            ntfy: { enabled: false, url: "" },
            telegram: { enabled: false, chatID: "" },
            discord: { enabled: false },
            homeAssistant: { enabled: false, service: "" },
            events: {
              storage: true,
              backups: true,
              updates: true,
              system: true,
            },
            content: "summary",
            quiet: { enabled: false, from: "22:00", to: "07:00" },
            origin: "",
          },
          secretSet: {},
          mailServer: false,
        },
      });
    if (p === "/homeassistant/notify-services")
      return route.fulfill({ json: { services: [], connections: [] } });
    if (p === "/apps/mine")
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found" } },
      });
    return route.fallback();
  });
}

const names = {
  files: "Dateien",
  shares: "Freigaben",
  storage: "Speicher",
  docker: "Docker",
  vms: "Virtuelle Maschinen",
  lxc: "Systemcontainer",
  appcenter: "App-Center",
  backup: "Sicherung",
  monitor: "Monitor",
  logs: "Protokolle",
  planner: "Aufgabenplaner",
  terminal: "Terminal",
  users: "Benutzer",
  security: "Sicherheit",
  account: "Mein Konto",
  settings: "Systemsteuerung",
};

/** The desktop with one window of a program at the given width; once more
 * if the dev server reloaded the page meanwhile. */
async function openWindow(app, path, w) {
  try {
    return await openWindowOnce(app, path, w);
  } catch {
    return openWindowOnce(app, path, w);
  }
}

async function openWindowOnce(app, path, w) {
  const made = await create("owner", "graphit", 1600);
  await made.page.setViewportSize({ width: 1600, height: 1000 });
  await made.ctx.addInitScript(
    ({ app, path, w, names }) => {
      localStorage.removeItem("titanos.layout");
      localStorage.setItem(
        "titanos.desktop.windows.alex",
        JSON.stringify([
          {
            id: "w1",
            app: "app:" + app,
            href: path,
            title: names[app] ?? app,
            x: 120,
            y: 60,
            w,
            h: 820,
            z: 10,
            minimized: false,
            snap: null,
          },
        ]),
      );
    },
    { app, path, w, names },
  );
  await prepare(made);
  await made.page.goto(base + "/");
  let frame;
  try {
    await made.page.waitForSelector("section.win iframe", { timeout: 15000 });
    frame = await frameOf(made.page);
  } catch (err) {
    await made.ctx.close();
    throw err;
  }
  await made.page.waitForTimeout(900);
  return { ...made, frame };
}

/** The window's frame once its page is there; the window may draw its frame anew while the desktop loads. */
async function frameOf(page) {
  for (let i = 0; i < 6; i++) {
    const handle = await page.waitForSelector("section.win iframe");
    const frame = await handle.contentFrame();
    try {
      await frame.waitForSelector("main", { state: "attached", timeout: 5000 });
      return frame;
    } catch {
      // Detached or still loading: look again.
    }
  }
  throw new Error("the window's page did not load");
}

/** What a window's page shows at the top and whether anything overflows. */
async function inspect(frame) {
  return frame.evaluate(() => {
    const visible = (el) => {
      const r = el.getBoundingClientRect();
      const s = getComputedStyle(el);
      return (
        r.width > 0 &&
        r.height > 0 &&
        s.visibility !== "hidden" &&
        s.display !== "none" &&
        !el.closest(".sr-only")
      );
    };
    const h1 = [...document.querySelectorAll("main h1")].filter(visible);
    const bar = document.querySelector(".win-toolbar");
    const box = bar?.getBoundingClientRect();
    const overflowX =
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth;
    // Something drawn over the tool bar: the element at points along its
    // middle must belong to the tool bar (or a menu opened from it).
    let covered = [];
    if (bar && box && box.height > 0) {
      for (let x = box.left + 6; x < box.right - 6; x += 37) {
        const el = document.elementFromPoint(x, box.top + box.height / 2);
        if (el && !bar.contains(el) && !el.closest("[role=menu], dialog"))
          covered.push(el.tagName + "." + el.className);
      }
    }
    // Controls cut off at either edge of the tool bar, or lying over each other.
    const parts = bar
      ? [...bar.querySelectorAll("button, a, input, .chip, h1")].filter(
          (el) => visible(el) && !el.closest("[role=menu]"),
        )
      : [];
    const clipped = parts
      .filter((el) => {
        const r = el.getBoundingClientRect();
        return r.right > box.right + 1 || r.left < box.left - 1;
      })
      .map((el) => el.textContent?.trim() || el.getAttribute("aria-label"));
    for (let i = 0; i < parts.length; i++)
      for (let j = i + 1; j < parts.length; j++) {
        if (parts[i].contains(parts[j]) || parts[j].contains(parts[i]))
          continue;
        const a = parts[i].getBoundingClientRect();
        const b = parts[j].getBoundingClientRect();
        const w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
        const h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
        if (w > 2 && h > 2)
          clipped.push(
            "overlap: " +
              (parts[i].textContent?.trim() ||
                parts[i].getAttribute("aria-label")) +
              " / " +
              (parts[j].textContent?.trim() ||
                parts[j].getAttribute("aria-label")),
          );
      }
    return {
      h1: h1.map((el) => el.textContent.trim()),
      toolbar: !!bar && visible(bar),
      overflowX,
      covered,
      clipped,
    };
  });
}

const slug = (path) =>
  path.replace(/^\//, "").replace(/[/?=&]/g, "-") || "home";

// WINDOWS_ALL=only skips the checks below; WINDOWS_PATHS=/a,/b narrows the pass.
const allOnly = process.env.WINDOWS_ALL === "only";
const onlyPaths = process.env.WINDOWS_PATHS?.split(",").filter(Boolean);

// 1. Representative pages at 800 and 1100 px: title bar, tool bar, no H1, no overflow.
for (const w of allOnly ? [] : [800, 1100]) {
  for (const [app, path, title, hasBar] of representative) {
    if (onlyPaths && !onlyPaths.includes(path)) continue;
    const { ctx, page, frame } = await openWindow(app, path, w);
    const res = await inspect(frame);
    await page
      .locator("section.win")
      .screenshot({ path: `${out}/${w}-${slug(path)}.png` });
    const where = `${path} @${w}`;
    check(
      !res.h1.length,
      `${where}: no big heading inside the window (${res.h1})`,
    );
    if (hasBar) check(res.toolbar, `${where}: tool bar under the title bar`);
    check(
      res.overflowX <= 1,
      `${where}: no sideways scrolling (${res.overflowX})`,
    );
    check(
      !res.covered.length,
      `${where}: nothing over the tool bar (${res.covered})`,
    );
    check(!res.clipped.length, `${where}: no clipped buttons (${res.clipped})`);
    if (title) {
      const bar = await page.locator("section.win header.titlebar").innerText();
      check(bar.includes(title), `${where}: title bar names the page (${bar})`);
    }
    // Scrolled down the tool bar stays at the top.
    if (hasBar) await frame.evaluate(() => window.scrollTo(0, 400));
    await frame.waitForTimeout(100);
    const top = await frame.evaluate(
      () => document.querySelector(".win-toolbar")?.getBoundingClientRect().top,
    );
    if (hasBar)
      check(
        top !== undefined && top <= 1,
        `${where}: tool bar sticks (${top})`,
      );
    await ctx.close();
  }
}

// 2. The phone: bar of the program, then the page with its tool bar.
for (const [, path] of allOnly ? [] : representative.slice(0, 9)) {
  if (onlyPaths && !onlyPaths.includes(path)) continue;
  const made = await create("owner", "graphit", 390);
  await made.page.setViewportSize({ width: 390, height: 844 });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  await prepare(made);
  await made.page.goto(base + path);
  await made.page.waitForSelector(".phone-bar", { timeout: 15000 });
  await made.page.waitForTimeout(900);
  const res = await inspect(made.page.mainFrame());
  await made.page.screenshot({ path: `${out}/phone-${slug(path)}.png` });
  check(
    res.overflowX <= 1,
    `${path} @phone: no sideways scrolling (${res.overflowX})`,
  );
  check(
    !res.clipped.length,
    `${path} @phone: no clipped buttons (${res.clipped})`,
  );
  await made.ctx.close();
}

// PLAN 71 ("Variante C"): Systemsteuerung has one menu – the side bar with
// its search – and the start page is the overview with cards. The side bar
// covers the desktop completely.
if (!allOnly && (!onlyPaths || onlyPaths.includes("/settings"))) {
  const made = await openWindow("settings", "/settings", 1100);
  const { page, frame } = made;
  await frame.waitForSelector(".overview .tile.ui-card .card-head");
  check(
    (await frame.$$(".home-tile")).length === 0,
    "/settings in a window: the overview, no tiles",
  );
  const side = page.locator("section.win .side");
  const bg = await side.evaluate((el) => getComputedStyle(el).backgroundColor);
  const alpha = /rgba?\(([^)]+)\)/
    .exec(bg)?.[1]
    .split(/[\s,/]+/)
    .filter(Boolean)[3];
  check(
    alpha === undefined || Number(alpha) >= 0.99,
    `the window's side bar is opaque (${bg})`,
  );
  const items = () =>
    side
      .locator(".items .item")
      .evaluateAll((a) => a.map((x) => x.textContent.trim()));
  const all = (await items()).length;
  const search = side.getByRole("searchbox", { name: "Einstellung suchen" });
  await search.fill("ssh");
  check(
    JSON.stringify(await items()) === JSON.stringify(["Zugang und Sitzungen"]),
    `side bar search "ssh" finds Zugang (${await items()})`,
  );
  // By keyboard: the window's page loads after Enter (no wait for it here).
  await search.focus();
  await page.keyboard.press("Enter");
  await page.waitForFunction(
    () =>
      document.querySelector("section.win iframe")?.contentWindow?.location
        .hash === "#access-ssh",
    null,
    { timeout: 8000 },
  );
  await search.fill("zeitzone");
  check((await items()).includes("Zeitzone"), "side bar search finds Zeitzone");
  await search.fill("platten-ruhe");
  check(
    (await items()).includes("Energie"),
    "side bar search finds Platten-Ruhe",
  );
  await search.press("Escape");
  check((await items()).length === all, "Escape clears the side bar search");
  // Ablage › Einstellungen suchen puts the cursor in the side bar's search.
  await page.locator('[data-menu="file"]').first().click();
  await page.getByRole("menuitem", { name: /Einstellungen suchen/ }).click();
  await page.waitForTimeout(300);
  check(
    await search.evaluate((el) => el === document.activeElement),
    "Ablage › Einstellungen suchen focuses the side bar search",
  );
  // Folded: only the magnifier; a click unfolds and focuses the field.
  await side.locator(".fold").click();
  await side.getByRole("button", { name: "Einstellung suchen" }).click();
  await page.waitForTimeout(300);
  check(
    await side
      .getByRole("searchbox")
      .evaluate((el) => el === document.activeElement),
    "the magnifier of the folded side bar opens the search",
  );
  await made.ctx.close();
}

// Script errors of the checked pages; the visual pass below also opens pages
// whose data the shared fixtures do not have (they have scripts of their own).
// A reload of the dev server while a page loads is no error of the page.
const checkedCount = errors.length;
const checkedErrors = errors.filter(
  (e) => !e.includes("Failed to fetch dynamically imported module"),
);

// 3. Optional: every page of every program, for a visual pass.
if (process.env.WINDOWS_ALL) {
  const report = [];
  for (const w of [800, 1100]) {
    for (const [app, paths] of everything) {
      for (const path of paths.filter(
        (p) => !onlyPaths || onlyPaths.includes(p),
      )) {
        // A fresh desktop per page; once more if the frame went away (a reload).
        for (let attempt = 0; attempt < 2; attempt++) {
          let made = null;
          try {
            made = await openWindow(app, path, w);
            const res = await inspect(made.frame);
            await made.page
              .locator("section.win")
              .screenshot({ path: `${out}/all-${w}-${slug(path)}.png` });
            report.push({ path, w, ...res });
            attempt = 2;
          } catch (err) {
            if (attempt)
              report.push({ path, w, error: String(err).slice(0, 200) });
          } finally {
            await made?.ctx.close();
          }
        }
      }
    }
  }
  for (const [, paths] of everything) {
    const made = await create("owner", "graphit", 390);
    await made.page.setViewportSize({ width: 390, height: 844 });
    await made.ctx.addInitScript(() =>
      localStorage.removeItem("titanos.layout"),
    );
    await prepare(made);
    for (const path of paths.filter(
      (p) => !onlyPaths || onlyPaths.includes(p),
    )) {
      await made.page.goto(base + path);
      await made.page.waitForTimeout(1200);
      const res = await inspect(made.page.mainFrame());
      await made.page.screenshot({
        path: `${out}/all-phone-${slug(path)}.png`,
        fullPage: true,
      });
      report.push({ path, w: 390, ...res });
    }
    await made.ctx.close();
  }
  for (const r of report)
    if (
      r.error ||
      (r.w !== 390 && r.h1.length) ||
      r.overflowX > 1 ||
      r.covered.length ||
      r.clipped.length
    )
      console.log(JSON.stringify(r));
  const later = [...new Set(errors.slice(checkedCount))];
  if (later.length)
    console.log(
      "script errors (fixture gaps):\n" +
        later.map((e) => e.split("\n")[0]).join("\n"),
    );
}

await browser.close();
if (failures.length) {
  console.log(failures.join("\n"));
  process.exitCode = 1;
}
assert.deepEqual(checkedErrors, [], "no script errors");
console.log("window pages ok");
