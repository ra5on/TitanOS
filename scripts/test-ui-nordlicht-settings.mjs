// The real settings window and the phone's settings page, using disposable API fixtures.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const base = process.env.UI_URL || "http://127.0.0.1:5297";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const engine = process.env.UI_ENGINE || "chromium";
const playwright = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await playwright[engine].launch({
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const out = process.env.UI_OUTPUT;
if (out) await mkdir(out, { recursive: true });

async function check(width) {
  const desktop = width >= 900;
  const { ctx, page, data } = await fixtureFactory(browser, errors)(
    "owner",
    desktop ? "hell" : "graphit",
    width,
    "de",
    {
      serviceWorkers: "block",
      hasTouch: !desktop,
      isMobile: !desktop,
      reducedMotion: "reduce",
      viewport: { width, height: desktop ? 900 : 844 },
    },
  );
  page.setDefaultTimeout(18000);
  try {
    data.prefs.appearance.font = 115;
    const job = {
      job: {
        id: "nacht",
        enabled: true,
        sources: ["Documents"],
        applications: [],
        every: "daily",
        time: "01:00",
        destination: { type: "usb", folder: "Sicherung" },
        keep: { last: 2 },
      },
      stateKnown: true,
      secretSet: {},
      next: null,
      state: {
        running: false,
        phase: "",
        lastError: "drive missing",
        lastErrorCode: "usb_not_found",
        bytesDone: 0,
        bytesTotal: 0,
        lastOK: "2026-11-20T12:00:00Z",
        lastRun: "2026-11-26T01:00:00Z",
      },
    };
    await ctx.route("**/api/v1/backups", (r) =>
      r.fulfill({
        json: {
          jobs: [job],
          shares: ["Documents"],
          volumes: ["Safe"],
          smbTargets: [],
        },
      }),
    );
    data.dockerContainers = [
      {
        id: "server-a1",
        name: "immich_server",
        managed: true,
        app: "immich",
        service: "server",
        state: "running",
        since: "2026-11-26T01:00:00Z",
        image: "immich:test",
        ports: [],
        cpuHistory: [],
        webui: "",
        publicUrl: "",
        icon: "",
        cpuPercent: 2,
        memoryBytes: 1e6,
        updateAvailable: false,
      },
    ];
    await ctx.route("**/api/v1/docker/containers/server-a1/logs?*", (r) =>
      r.fulfill({ json: { logs: "Nordlicht: Immich server ready" } }),
    );
    const recovered = "/mnt/Data/Documents/Brief.txt";
    await ctx.route("**/api/v1/backups/nacht/snapshots/*/files?*", (r) =>
      r.fulfill({
        json: {
          entries: [
            { name: "Brief.txt", path: recovered, type: "file", size: 26 },
          ],
          total: 1,
          cut: false,
        },
      }),
    );
    let restore;
    await ctx.route("**/api/v1/backups/nacht/restore", (r) => {
      restore = r.request().postDataJSON();
      return r.fulfill({ json: { target: "/mnt/Safe/restore-nacht" } });
    });
    await ctx.route("**/api/v1/preferences/appearance", (r) => {
      if (r.request().method() === "PUT")
        data.prefs.appearance = r.request().postDataJSON();
      return r.fulfill({ json: data.prefs.appearance });
    });
    await ctx.addInitScript(
      ({ desktop }) => {
        localStorage.removeItem("titanos.layout");
        if (desktop)
          localStorage.setItem(
            "titanos.desktop.windows.alex",
            JSON.stringify([
              {
                id: "w1",
                app: "app:settings",
                href: "/settings",
                title: "Systemsteuerung",
                x: 10,
                y: 12,
                w: 1180,
                h: 755,
                z: 1,
                minimized: false,
                snap: null,
              },
            ]),
          );
      },
      { desktop },
    );
    const settings = () =>
      desktop ? page.frameLocator('iframe[data-app="app:settings"]') : page;
    const openSetting = async (href) => {
      if (!desktop) return page.goto(base + href);
      await page
        .locator('iframe[data-app="app:settings"]')
        .evaluate((frame, href) => {
          frame.contentWindow.postMessage(
            { type: "titanos-goto", href },
            location.origin,
          );
        }, href);
    };
    const search = () =>
      desktop
        ? page.locator("section.win nav.side").getByRole("searchbox")
        : page.locator(".home-search").getByRole("searchbox");
    await page.goto(base + (desktop ? "/" : "/settings"));
    await search().waitFor();
    if (desktop) {
      await settings().locator('.todo a[href$="#backup-nacht"]').click();
      await settings().locator("#backup-nacht:visible").waitFor();
      await openSetting("/settings");
      await settings().locator(".overview").waitFor();
    }
    await page.evaluate(async () => {
      await document.fonts.ready;
      await new Promise(requestAnimationFrame);
      await new Promise(requestAnimationFrame);
    });
    if (out)
      await page.screenshot({
        path: out + "/" + engine + "-" + width + "-systemsteuerung.png",
      });
    await search().fill("Backup wiederherstellen");
    const result = desktop
      ? page.locator("nav.side .result").first()
      : page.locator(".groups .hit a").first();
    await result.waitFor();
    await search().press("Enter");
    await settings().locator("#restore-files").waitFor();
    await settings()
      .locator("#backup-nacht")
      .getByRole("button", { name: "Wiederherstellen …", exact: true })
      .click();
    const dialog = settings().getByRole("dialog");
    await dialog
      .getByRole("radio", { name: "Einzelne Dateien und Ordner" })
      .check();
    await dialog.getByRole("checkbox", { name: "Brief.txt" }).check();
    await dialog.getByRole("button", { name: "Wiederherstellen" }).click();
    await dialog.getByText("/mnt/Safe/restore-nacht").waitFor();
    assert.deepEqual(restore.paths, [recovered]);
    assert.equal(restore.volume, "Safe");
    await openSetting("/settings?area=notifications");
    await settings()
      .locator("main h1")
      .filter({ hasText: "Benachrichtigungen" })
      .waitFor({ state: "attached" });
    await openSetting("/settings");
    if (desktop) await settings().locator(".overview").waitFor();
    await search().fill("Benachrichtigungen");
    const moved = desktop
      ? page
          .locator("nav.side .result")
          .getByText("Benachrichtigungen", { exact: true })
          .first()
      : page.locator('.groups .hit a[href="/settings/notifications"]').first();
    await moved.click();
    await settings().locator("#push").waitFor();
    await openSetting("/settings");
    if (desktop) await settings().locator(".overview").waitFor();
    await search().fill("Immich Logs");
    const logHit = desktop
      ? page.locator("nav.side .result").filter({ hasText: "immich" })
      : page.locator(".groups .hit a").filter({ hasText: "immich" });
    await logHit.first().click();
    const docker = desktop
      ? page.frameLocator('iframe[data-app="app:docker"]')
      : page;
    await docker
      .getByRole("dialog")
      .getByText("Nordlicht: Immich server ready")
      .waitFor();
    if (desktop) {
      await openSetting("/settings/appearance");
      const profiles = settings().locator("#look-profile");
      await profiles.selectOption("klar");
      await settings().locator('html[data-surface="flat"]').waitFor();
      await profiles.selectOption("kompakt");
      await settings().locator('html[data-density="compact"]').waitFor();
      await profiles.selectOption("nordlicht");
      await settings()
        .locator('html[data-surface="glass"][data-density="normal"]')
        .waitFor();
      await page.waitForFunction(
        () =>
          JSON.parse(localStorage.getItem("titanos.appearance"))
            .desktopProfile === "nordlicht",
      );
      assert.equal(
        await settings()
          .locator("html")
          .evaluate((el) => el.style.getPropertyValue("--font-scale")),
        "1.15",
      );
      await openSetting("/settings/appearance?tab=colors#h-sliders");
      await settings()
        .locator("details.advanced-look[open] #h-sliders")
        .waitFor();
    }
    console.log(
      engine,
      width,
      "settings, backup restore fixture, old URL, named container logs OK",
    );
  } catch (error) {
    console.error(
      engine,
      width,
      page.frames().map((frame) => frame.url()),
      errors,
    );
    for (const frame of page.frames().slice(0, 3))
      console.error(
        (
          await frame
            .locator("body")
            .innerText({ timeout: 1000 })
            .catch(() => "")
        ).slice(0, 550),
      );
    throw error;
  } finally {
    await ctx.close();
  }
}
try {
  if (!process.env.UI_PHONE_ONLY) await check(1280);
  await check(390);
  const { ctx, page, data } = await fixtureFactory(browser, errors)(
    "user",
    "hell",
    390,
  );
  try {
    await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
    await page.goto(base + "/settings");
    const search = page.locator(".home-search").getByRole("searchbox");
    for (const term of ["Immich Logs", "Backup wiederherstellen"]) {
      await search.fill(term);
      await page
        .locator(".search-result")
        .getByRole("status")
        .filter({ hasText: "Keine" })
        .waitFor();
      assert.equal(await page.locator(".groups .hit").count(), 0);
    }
    assert.ok(
      !data.requests.some(
        ([method, path]) => path === "/docker" || path === "/backups",
      ),
    );
  } finally {
    await ctx.close();
  }
  assert.deepEqual(errors, []);
} finally {
  await browser.close();
}
