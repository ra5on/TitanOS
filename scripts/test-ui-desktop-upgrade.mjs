// Reopen an actual old desktop against the new binary, using one disposable
// config directory, browser storage and HTTP origin across the restart.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtemp, mkdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "node:net";
import { setTimeout as wait } from "node:timers/promises";
import { once } from "node:events";

const [oldBinary, newBinary] = process.argv.slice(2);
assert.ok(
  oldBinary && newBinary,
  "Pass the old and new titand binaries with embedded web UIs",
);
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const scratch = await mkdtemp(join(tmpdir(), "titanos-desktop-upgrade-"));
const name = "upgrade";
const password = randomBytes(24).toString("hex");
const bind = createServer();
bind.listen(0, "127.0.0.1");
await once(bind, "listening");
const address = "127.0.0.1:" + bind.address().port;
const base = "http://" + address;
await new Promise((resolve) => bind.close(resolve));
let server, browser;
async function start(binary) {
  server = spawn(
    binary,
    ["-dev", "-config", join(scratch, "config"), "-dev-listen", address],
    { stdio: "ignore" },
  );
  for (let i = 0; i < 100; i++) {
    if (server.exitCode !== null)
      throw Error("Development server failed to start");
    try {
      if ((await fetch(base + "/api/v1/session")).ok) return;
    } catch {}
    await wait(100);
  }
  throw Error("Development server did not answer");
}
async function stop() {
  if (!server || server.exitCode !== null || server.signalCode) return;
  const done = once(server, "exit");
  server.kill("SIGTERM");
  await done;
}
const keys = ["app:settings", "app:shares", "app:files"];
const sample = {
  version: 1,
  sort: "free",
  iconSize: "s",
  customIcons: true,
  icons: [...keys, "folder:arbeit"],
  iconPos: {
    "app:settings": { c: 1, r: 2 },
    "app:shares": { c: 2, r: 0 },
    "app:files": { c: 0, r: 1 },
  },
  folders: [
    { id: "arbeit", name: "Werkzeuge", apps: ["app:monitor", "app:updates"] },
  ],
  customWidgets: true,
  widgets: [
    { id: "uhr-buero", kind: "clock", x: 26, y: 2, w: 13, h: 7 },
    { id: "status-buero", kind: "status", x: 4, y: 14, w: 19, h: 9 },
  ],
  dock: {
    show: true,
    pinned: keys,
    customPinned: true,
    size: 44,
    opacity: 78,
    magnify: 0,
    position: "right",
    border: true,
  },
  menuOrder: ["app:shares", "app:files", "app:settings"],
  layouts: [
    {
      id: "buero",
      name: "Büro",
      windows: [
        {
          app: "app:settings",
          href: "/settings",
          x: 100,
          y: 50,
          w: 940,
          h: 570,
        },
        { app: "app:files", href: "/files", x: 290, y: 130, w: 710, h: 490 },
      ],
    },
  ],
  wallpaper: { source: "builtin", id: "nordlicht", live: false },
  phone: {
    favorites: ["app:shares", "app:settings"],
    dock: {
      custom: true,
      show: true,
      size: "small",
      apps: ["app:files", "app:shares"],
    },
  },
};
const fileFavorites = {
  favorites: [
    { root: "share:Archiv", path: "Verträge" },
    { root: "share:Medien", path: "Fotos" },
  ],
};
async function update(request, method, path, data) {
  const r = await request[method]("/api/v1" + path, {
    data,
    headers: { Origin: base },
  });
  assert.ok(r.ok(), path + " returned " + r.status());
}
async function current(request) {
  const r = await request.get("/api/v1/preferences");
  assert.ok(r.ok());
  return await r.json();
}
async function roundtrip(page) {
  const fold = page.locator("section.win nav.side .fold");
  // Wait for the real debounced PUT on each action, including the undo.
  for (let n = 0; n < 2; n++) {
    const response = page.waitForResponse(
      (r) =>
        r.url().endsWith("/preferences/desktop") &&
        r.request().method() === "PUT",
    );
    await fold.click();
    assert.equal((await response).status(), 204);
  }
}
async function layout(ctx, page) {
  await page.locator('.dock.right [data-key="app:settings"]').waitFor();
  const win = page.locator('section.win:has(iframe[data-app="app:settings"])');
  await win.waitFor();
  await page.locator(".widget[data-widget=clock]").waitFor();
  const desktop = await page.evaluate(() => {
    const list = (s, fn) => Array.from(document.querySelectorAll(s), fn);
    const rect = (el) => [
      el.style.left,
      el.style.top,
      el.style.width,
      el.style.height,
    ];
    const dock = document.querySelector(".dock");
    return {
      window: rect(document.querySelector("section.win")),
      icons: list(".icons > .ico", (el) => [
        el.dataset.key,
        el.style.left,
        el.style.top,
      ]),
      widgets: list(".widget[data-widget]", (el) => [
        el.dataset.widget,
        ...rect(el),
      ]),
      pinned: list(".dock [data-key]", (el) => el.dataset.key),
      dockSize: dock.style.getPropertyValue("--size"),
      opens: JSON.parse(
        localStorage.getItem("titanos.desktop.windows.upgrade"),
      ),
    };
  });
  await page.locator(".topbar [data-menu-button]").click();
  desktop.menu = await page
    .locator(".launcher .grid .cell[data-key]")
    .evaluateAll((els) => els.map((e) => e.dataset.key).slice(0, 3));
  await page.keyboard.press("Escape");
  const phone = await ctx.newPage();
  await phone.setViewportSize({ width: 390, height: 844 });
  await phone.goto(base);
  await phone.locator(".favorites [data-home-key]").first().waitFor();
  desktop.phoneFavorites = await phone
    .locator(".favorites [data-home-key]")
    .evaluateAll((els) => els.map((e) => e.dataset.homeKey));
  desktop.phoneDock = await phone
    .locator("[data-phone-dock]:visible [data-dock-key]")
    .evaluateAll((els) => els.map((e) => e.dataset.dockKey));
  await phone.close();
  return desktop;
}
try {
  await mkdir(join(scratch, "config/users"), { recursive: true });
  await start(oldBinary);
  browser = await chromium.launch({
    executablePath: process.env.BROWSER_EXECUTABLE,
  });
  const ctx = await browser.newContext({
    baseURL: base,
    viewport: { width: 1440, height: 900 },
    locale: "de-DE",
    serviceWorkers: "block",
    reducedMotion: "reduce",
  });
  await update(ctx.request, "post", "/setup", { name, password });
  await update(ctx.request, "post", "/login", { name, password });
  const oldVersion = (await (await ctx.request.get("/api/v1/system")).json())
    .version;
  await update(ctx.request, "post", "/system/disclaimer", { version: 1 });
  await update(ctx.request, "put", "/preferences/hints", {
    "webapps-0.63": true,
  });
  await update(ctx.request, "put", "/preferences/desktop", sample);
  await update(ctx.request, "put", "/preferences/files", fileFavorites);
  const page = await ctx.newPage();
  page.setDefaultTimeout(12000);
  await page.goto(base);
  await page.locator('.dock.right [data-key="app:settings"]').click();
  const win = page.locator("section.win");
  await win.locator(".titlebar").waitFor();
  const original = await win.boundingBox();
  const edge = await win.locator(".edge.se").boundingBox();
  await page.mouse.move(edge.x + 2, edge.y + 2);
  await page.mouse.down();
  await page.mouse.move(edge.x - 75, edge.y - 55, { steps: 6 });
  await page.mouse.up();
  await roundtrip(page);
  const prefs = await current(ctx.request);
  const before = await layout(ctx, page);
  assert.ok(
    parseFloat(before.window[2]) < original.width &&
      parseFloat(before.window[3]) < original.height,
    "the old UI persisted a manually resized window",
  );
  assert.deepEqual(before.pinned, keys);
  assert.deepEqual(before.menu, sample.menuOrder);
  assert.deepEqual(before.phoneFavorites, sample.phone.favorites);
  assert.deepEqual(before.phoneDock, sample.phone.dock.apps);
  assert.deepEqual(
    before.widgets.map((w) => w[0]),
    ["clock", "status"],
  );
  assert.equal(before.dockSize, "44px");
  assert.equal(before.opens.length, 1);
  assert.deepEqual(prefs.desktop.layouts, sample.layouts);
  assert.deepEqual(prefs.desktop.iconPos, sample.iconPos);
  assert.deepEqual(prefs.files, fileFavorites);

  // Releasing pages before the binary swap represents the browser reopening the
  // same origin with its existing storage. A normal new login may be required.
  await page.close();
  await stop();
  await start(newBinary);
  await update(ctx.request, "post", "/login", { name, password });
  const newVersion = (await (await ctx.request.get("/api/v1/system")).json())
    .version;
  assert.deepEqual((await current(ctx.request)).desktop, prefs.desktop);
  const next = await ctx.newPage();
  await next.goto(base);
  const after = await layout(ctx, next);
  assert.deepEqual(
    after,
    before,
    "reopened geometry, icons, menu, widgets, dock and favorites",
  );
  await roundtrip(next);
  const saved = await current(ctx.request);
  assert.deepEqual(
    saved.desktop,
    prefs.desktop,
    "complete desktop preference after new UI writes",
  );
  assert.deepEqual(saved.files, prefs.files, "file favorites after upgrade");
  console.log(
    JSON.stringify({
      oldVersion,
      newVersion,
      window: after.window,
      widgets: after.widgets,
      icons: after.icons,
      pinned: after.pinned,
      menu: after.menu,
      phoneFavorites: after.phoneFavorites,
      folderFavorites: saved.files.favorites.length,
      layouts: saved.desktop.layouts.length,
    }),
  );
} finally {
  await browser?.close();
  await stop();
  await rm(scratch, { recursive: true, force: true });
}
