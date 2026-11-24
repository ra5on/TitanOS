// Run against the local development web UI with isolated API fixtures.
import assert from "node:assert/strict";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const base = process.env.UI_URL || "http://127.0.0.1:5276";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const errors = [];
const fixture = fixtureFactory(browser, errors);

async function desktop() {
  const made = await fixture("owner", "graphit", 1440);
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  return made;
}

try {
  {
    const { ctx, page, data } = await desktop();
    try {
      data.prefs.desktop = { version: 1, dock: { show: true, magnify: 2 } };
      await page.emulateMedia({ reducedMotion: "no-preference" });
      await page.goto(base);
      const slot = page.locator(".dock .slot[data-key]").first();
      await slot.hover();
      const magnification = () => Number(getComputedStyle(document.querySelector(".dock .slot[data-key]")).getPropertyValue("--s"));
      await page.waitForFunction(() => {
        const slot = document.querySelector(".dock .slot[data-key]");
        return Number(getComputedStyle(slot).getPropertyValue("--s")) > 1;
      });
      assert.ok(await page.evaluate(magnification) > 1);
      await page.emulateMedia({ reducedMotion: "reduce" });
      await page.waitForFunction((read) => {
        const slot = document.querySelector(".dock .slot[data-key]");
        return Number(getComputedStyle(slot).getPropertyValue(read)) === 1;
      }, "--s");
      await page.mouse.move(4, 50);
      await slot.hover();
      assert.equal(await page.evaluate(magnification), 1);
    } finally {
      await ctx.close();
    }
  }

  {
    const { ctx, page } = await desktop();
    let mode = "initial";
    let requests = 0;
    const waiting = Promise.withResolvers();
    const release = Promise.withResolvers();
    const olderFinished = Promise.withResolvers();
    const older = "Alt.txt", newer = "Neu.txt";
    const entry = (name) => [{ name, size: 18, dir: false, modified: "2026-11-25T10:00:00Z" }];
    try {
      await ctx.route((url) => url.pathname === "/api/v1/files/list", async (route) => {
        if (mode === "initial") return route.fulfill({ json: entry("Vorher.txt") });
        if (mode === "race" && ++requests === 1) {
          waiting.resolve();
          await release.promise;
          await route.fulfill({ json: entry(older) }).catch(() => {});
          olderFinished.resolve();
          return;
        }
        return route.fulfill({ json: entry(newer) });
      });
      await ctx.route((url) => url.pathname === "/api/v1/files/uploads", (route) =>
        mode === "uploads-error"
          ? route.fulfill({ status: 503, json: { error: { code: "unavailable", message: "fixture" } } })
          : route.fulfill({ json: [] })
      );
      await page.goto(base + "/files?root=share/Documents");
      const app = page.frameLocator("iframe[data-window]").first();
      const row = (name) => app.locator('[data-file-name="' + name + '"]').first();
      const reload = app.getByRole("button", { name: "Neu laden", exact: true }).first();
      await row("Vorher.txt").waitFor();
      mode = "race";
      await reload.click();
      await Promise.race([waiting.promise, new Promise((_, reject) => setTimeout(() => reject(new Error("reload did not start")), 5000).unref())]);
      await reload.click();
      await row(newer).waitFor();
      release.resolve();
      await olderFinished.promise;
      await page.waitForTimeout(100);
      assert.ok(await row(newer).isVisible());
      assert.equal(await row(older).isVisible(), false);
      mode = "uploads-error";
      await reload.click();
      await app.getByText("Unfertige Uploads konnten nicht geladen werden.").waitFor();
      await row(newer).waitFor();
      assert.equal(await row(older).isVisible(), false);
    } finally {
      release.resolve();
      await ctx.close();
    }
  }

  {
    const { ctx, page, data } = await desktop();
    let unavailable = true;
    try {
      await page.clock.install();
      data.prefs.desktop = {
        version: 1,
        widgets: [
          { id: "w-net", kind: "network", x: 2, y: 2, w: 17, h: 9 },
          { id: "w-backups", kind: "backups", x: 20, y: 2, w: 17, h: 10 },
        ],
      };
      await ctx.route((url) => url.pathname === "/api/v1/backups" && url.searchParams.has("cached"), (route) =>
        unavailable
          ? route.fulfill({ status: 503, json: { error: { code: "unavailable", message: "fixture" } } })
          : route.fulfill({ json: { jobs: [{ job: { id: "usb", name: "USB-Sicherung" }, state: { running: false, lastRun: "2026-11-25T10:00:00Z" } }] } })
      );
      await page.goto(base);
      await page.getByRole("heading", { name: "Netzwerk", exact: true }).waitFor();
      await page.getByText("Sicherungsstand gerade nicht erreichbar.").waitFor();
      assert.equal(await page.getByText("Keine Sicherungen eingerichtet.").count(), 0);
      unavailable = false;
      await page.clock.runFor(21000);
      await page.getByText("USB-Sicherung", { exact: true }).waitFor();
      unavailable = true;
      await page.clock.runFor(21000);
      await page.getByText(/Abruf fehlgeschlagen. Stand:/).waitFor();
      assert.ok(await page.getByText("USB-Sicherung", { exact: true }).isVisible());
    } finally {
      await ctx.close();
    }
  }
  assert.deepEqual(errors, []);
  console.log("Dateiliste, Widgets und Dock geprüft.");
} finally {
  await browser.close();
}
