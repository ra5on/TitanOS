// Test the actual touch target and the separate Home and Up destinations
// in the Nordlicht phone header, with the user's dock switched off.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const engine = process.env.UI_ENGINE || "chromium";
const { [engine]: driver } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5297";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const browser = await driver.launch({
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const output = process.env.UI_OUTPUT;
if (output) await mkdir(output, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

// §121: one bar above every program. At the top level its back arrow leads
// home; below it a separate home link does, since this account has neither
// the bar nor the dock at the bottom.
async function checkHeader(page) {
  const header = page.locator("header.phone-bar");
  await header.waitFor();
  const home = header
    .locator('a[href="/"], button[aria-label="Zum Startbildschirm"]')
    .first();
  await home.waitFor();
  assert.equal(await page.locator(".nord-header").count(), 0, "no second head row");
  assert.equal(await page.locator("[data-phone-dock]:visible").count(), 0);
  assert.equal(await page.locator("[data-phone-tabs]").count(), 0);

  const bounds = await home.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const cx = r.x + r.width / 2,
      cy = r.y + r.height / 2;
    // The middle and each side of the usable area must actually receive taps.
    const active = [
      [cx, cy],
      [r.left + 3, cy],
      [r.right - 3, cy],
      [cx, r.top + 3],
      [cx, r.bottom - 3],
    ].every(([x, y]) => el.contains(document.elementFromPoint(x, y)));
    const overlap = [...el.closest("header").querySelectorAll("button, a")].some(
      (button) => {
        if (button === el) return false;
        const b = button.getBoundingClientRect();
        return (
          Math.min(b.right, r.right) > Math.max(b.left, r.left) &&
          Math.min(b.bottom, r.bottom) > Math.max(b.top, r.top)
        );
      },
    );
    return {
      width: r.width,
      height: r.height,
      offset: Math.abs(cx - innerWidth / 2),
      overflow: document.documentElement.scrollWidth - innerWidth,
      active,
      overlap,
    };
  });
  assert.ok(bounds.width >= 44 && bounds.height >= 44, JSON.stringify(bounds));
  assert.ok(bounds.overflow <= 1, JSON.stringify(bounds));
  assert.equal(bounds.active, true, "the tap area is unobstructed");
  assert.equal(
    bounds.overlap,
    false,
    "the neighboring tools have separate hit areas",
  );
  return home;
}
try {
  for (const width of [320, 390]) {
    const { ctx, page, data } = await create("owner", "graphit", width, "de", {
      nativeShell: true,
      isMobile: true,
      hasTouch: true,
      serviceWorkers: "block",
      reducedMotion: "reduce",
      viewport: { width, height: width === 320 ? 690 : 844 },
    });
    try {
      data.prefs.desktop = {
        version: 1,
        wallpaper: { source: "builtin", id: "nordlicht", live: false },
        phone: {
          favorites: ["app:settings", "app:files"],
          dock: { show: false },
        },
      };
      await ctx.route("**/api/v1/notifications", (route) =>
        route.fulfill({
          json: {
            messages: [
              {
                id: 41,
                time: "2026-11-26T09:02:00Z",
                severity: "info",
                kind: "workspace_test",
                subject: "",
                title: "Neue E-Mail",
                text: "Neue Nachricht im Posteingang.",
              },
              {
                id: 42,
                time: "2026-11-26T08:30:00Z",
                severity: "info",
                kind: "workspace_test",
                subject: "",
                title: "Prüfung abgeschlossen",
                text: "Das Volume wurde geprüft.",
              },
            ],
          },
        }),
      );
      const start = page.locator("[data-nord-home]");
      const settings = page.locator("#settings-home-areas");
      const openSettings = () =>
        start.locator('[data-home-key="app:settings"]').tap();
      const bell = page.locator("[data-phone-bell]");
      const up = page.getByRole("button", {
        name: "Eine Ebene zurück",
        exact: true,
      });
      async function goHome() {
        await (await checkHeader(page)).tap();
        await start.waitFor();
        assert.equal(new URL(page.url()).pathname, "/");
      }
      async function capture(view) {
        if (!output) return;
        await page.evaluate(() => document.fonts.ready.then(() => true));
        await page.screenshot({
          path: join(output, engine + "-" + width + "-" + view + ".png"),
          animations: "disabled",
        });
      }
      await page.goto(base + "/settings");
      await settings.waitFor();
      await checkHeader(page);
      await capture("Einstellungen");
      await settings.locator("a[href*='?area=']").first().tap();
      await up.tap();
      await settings.waitFor();
      await goHome();

      // Opening the bell from Settings records that return destination.
      await openSettings();
      await settings.waitFor();
      await bell.tap();
      await page.locator("[data-notice='41']").waitFor();
      await checkHeader(page);
      await capture("Meldungen");
      await page.locator("[data-notice='41']").tap();
      await page.locator(".notice-detail").waitFor();
      await up.tap();
      await page.locator("[data-notice='41']").waitFor();
      await up.tap();
      await settings.waitFor();
      assert.equal(new URL(page.url()).pathname, "/settings");
      await bell.tap();
      await page.locator("[data-notice='41']").waitFor();
      await goHome();

      // Direct entry has no preceding settings page to go back to.
      await page.goto(base + "/notifications");
      await page.locator("[data-notice='41']").waitFor();
      await goHome();
      console.log(
        engine,
        width,
        "settings and notices: visible Home, distinct Up, unobstructed hit area, dock off",
      );
    } finally {
      await ctx.close();
    }
  }
  assert.deepEqual(errors, []);
} finally {
  await browser.close();
}
