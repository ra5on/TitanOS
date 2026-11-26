// 1.07: TitanOS without the NAS. The installed app on the phone was open
// with the NAS once (the service worker keeps the page, the page a snapshot
// of what it saw); then
//   1. the phone is offline: the page opens from the worker and shows the
//      last state, marked as old, with "Jetzt prüfen"; back online it starts
//      as usual;
//   2. the phone is online but the NAS does not answer (its API refuses the
//      connection): the same view;
//   3. a snapshot taken while locked shows only the lock;
//   4. the NAS goes away while the home screen is open: a banner below the
//      status bar, and it goes when the NAS answers again;
//   5. the worker keeps nothing of the API.
// Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5206 node scripts/test-ui-offline.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5206";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const out = process.env.UI_OUTPUT || "out/validation/offline";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const create = fixtureFactory(browser, errors);
const W = 390;
const H = 844;

async function phone() {
  const made = await create("owner", "graphit", W, "de", {
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 2,
    viewport: { width: W, height: H },
    serviceWorkers: "allow",
  });
  const { ctx } = made;
  await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  made.data.prefs.desktop = {
    version: 1,
    wallpaper: { source: "builtin", id: "wave", live: false },
  };
  made.nasDown = false;
  await ctx.route("**/api/v1/**", async (route) => {
    // The NAS is off: its API refuses the connection.
    if (made.nasDown) return route.abort("connectionrefused");
    const p = new URL(route.request().url()).pathname.slice(7);
    if (p === "/backups")
      return route.fulfill({
        json: {
          jobs: [
            {
              job: { id: "j1", name: "Fotos auf USB" },
              state: { running: false, lastRun: "2026-11-16T22:00:00Z", lastOk: true },
            },
          ],
        },
      });
    if (p === "/cluster/nodes")
      return route.fulfill({ status: 404, json: { error: { code: "not_found", message: "x" } } });
    return route.fallback();
  });
  return made;
}

/** Online once, with the worker in control and the snapshot written. */
async function seen(made) {
  const { page } = made;
  await page.goto(base + "/");
  await page.locator("[data-nord-home]").waitFor({ timeout: 20000 });
  await page.evaluate(() => navigator.serviceWorker.ready);
  // The first load is not controlled yet; the second one comes through
  // the worker, which keeps the page and its files.
  await page.reload();
  await page.locator("[data-nord-home]").waitFor({ timeout: 20000 });
  assert.ok(
    await page.evaluate(() => !!navigator.serviceWorker.controller),
    "the worker controls the page",
  );
  try {
    await page.waitForFunction(() => {
      const user = JSON.parse(localStorage.getItem("titanos.offline") || "null");
      const snap = user && JSON.parse(localStorage.getItem("titanos.offline." + user) || "null");
      return snap?.system && snap?.volumes;
    });
  } catch (e) {
    console.error(await page.evaluate(() => JSON.stringify(Object.fromEntries(Object.entries(localStorage).filter(([k]) => k.startsWith("titanos.offline"))))));
    throw e;
  }
  await page.waitForTimeout(500);
}

async function offlineView(page, name) {
  try {
    await page.getByRole("heading", { name: "NAS nicht erreichbar" }).waitFor({ timeout: 20000 });
  } catch (e) {
    await page.screenshot({ path: `${out}/${name}-failed.png` }).catch(() => {});
    console.error(page.url(), (await page.content().catch(() => "")).slice(0, 600));
    throw e;
  }
  const banner = page.locator('[role="status"]', { hasText: "Keine Verbindung zum NAS." }).first();
  await banner.waitFor();
  const button = page.getByRole("button", { name: "Jetzt prüfen" });
  const box = await button.boundingBox();
  assert.ok(box && box.height >= 44, `${name}: "Jetzt prüfen" is big enough (${box?.height})`);
  assert.ok(
    await page.getByText("Zu sehen ist der Stand von").isVisible(),
    `${name}: the time of the state is named`,
  );
  assert.ok(
    await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1),
    `${name}: no sideways scrolling`,
  );
  await page.screenshot({ path: `${out}/${name}.png`, fullPage: true });
}

try {
  // 1. The phone is offline.
  {
    const made = await phone();
    const { ctx, page } = made;
    await seen(made);
    // Offline: the page's HTML only from the worker; the API (answered by
    // this test's routes, which an offline context still reaches) neither.
    await ctx.setOffline(true);
    made.nasDown = true;
    await page.reload().catch(() => {});
    await offlineView(page, "01-handy-offline");
    assert.ok(await page.getByText("Data", { exact: true }).isVisible(), "the volume is listed");
    assert.ok(await page.getByText("Prozessor").isVisible(), "the system is listed");
    // The worker kept nothing of the API.
    const kept = await page.evaluate(async () => {
      const out = [];
      for (const name of await caches.keys())
        for (const req of await (await caches.open(name)).keys()) out.push(new URL(req.url).pathname);
      return out;
    });
    assert.ok(kept.some((p) => p.startsWith("/_app/immutable/")), "the files of the UI are kept");
    assert.deepEqual(
      kept.filter((p) => p.startsWith("/api/")),
      [],
      "nothing of the API is kept",
    );
    // Back online: "Jetzt prüfen" starts TitanOS.
    await ctx.setOffline(false);
    made.nasDown = false;
    await page.getByRole("button", { name: "Jetzt prüfen" }).click();
    await page.locator("[data-nord-home]").waitFor({ timeout: 20000 });
    await ctx.close();
  }

  // 2. The phone is online, the NAS does not answer.
  {
    const made = await phone();
    const { ctx, page } = made;
    await seen(made);
    made.nasDown = true;
    await page.reload();
    await offlineView(page, "02-nas-aus");
    made.nasDown = false;
    // It looks again by itself.
    await page.locator("[data-nord-home]").waitFor({ timeout: 25000 });

    // 4. The NAS goes away while the home screen is open: the banner.
    made.nasDown = true;
    const banner = page.locator(".offline-banner");
    await banner.waitFor({ timeout: 20000 });
    assert.equal(await banner.getAttribute("role"), "status");
    const top = await banner.evaluate((el) => el.getBoundingClientRect().top);
    assert.ok(top >= 0, "the banner is in view");
    await page.screenshot({ path: `${out}/04-banner.png` });
    made.nasDown = false;
    await banner.getByRole("button", { name: "Jetzt prüfen" }).click();
    await banner.waitFor({ state: "detached", timeout: 20000 });
    await ctx.close();
  }

  // 3. Locked when seen: only the lock.
  {
    const made = await phone();
    const { ctx, page } = made;
    await seen(made);
    // The session is locked now (lock screen); the page sees it.
    await ctx.route("**/api/v1/session", (route) =>
      made.nasDown
        ? route.abort("connectionrefused")
        : route.fulfill({
            json: {
              configAvailable: true,
              setupRequired: false,
              user: { name: "alex", role: "owner", twoFactor: false },
              hostname: "titan-test",
              locked: true,
            },
          }),
    );
    await page.reload();
    await page.waitForFunction(() => {
      const user = JSON.parse(localStorage.getItem("titanos.offline") || "null");
      return JSON.parse(localStorage.getItem("titanos.offline." + user) || "{}").locked === true;
    });
    await ctx.setOffline(true);
    made.nasDown = true;
    await page.reload().catch(() => {});
    await page.getByRole("heading", { name: "NAS nicht erreichbar" }).waitFor({ timeout: 20000 });
    assert.ok(await page.getByText("Die Sitzung ist gesperrt.").isVisible(), "the lock is shown");
    assert.equal(await page.getByText("Prozessor").count(), 0, "nothing of the account");
    await page.screenshot({ path: `${out}/03-gesperrt.png` });
    await ctx.close();
  }

  const real = errors.filter((e) => !/Failed to fetch|ERR_INTERNET_DISCONNECTED|ERR_CONNECTION_REFUSED|NetworkError|net::/.test(e));
  assert.deepEqual(real, [], "no script errors");
  console.log("offline UI ok:", out);
} finally {
  await browser.close();
}
