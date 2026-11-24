// Run against the local development web UI with isolated API fixtures.
import assert from "node:assert/strict";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const base = process.env.UI_URL || "http://127.0.0.1:5277";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const errors = [];
const fixture = fixtureFactory(browser, errors);

try {
  for (const width of [390, 320]) {
    const { page, ctx } = await fixture("owner", "graphit", width, "de", {
      viewport: { width, height: 844 },
      isMobile: true,
      hasTouch: true,
    });
    try {
      await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
      const bar = page.locator("header.phone-bar");
      await page.goto(base + "/settings/backups");
      await bar.getByText("Sicherungen", { exact: true }).waitFor();
      assert.equal(await page.locator(".set-path").count(), 0);
      const status = page.getByRole("heading", { name: "Sicherungsstand" });
      const refresh = page.getByRole("button", { name: "Status aktualisieren" });
      assert.ok((await status.boundingBox()).y < 115);
      assert.ok(Math.abs((await status.boundingBox()).y - (await refresh.boundingBox()).y) < 30);
      const restore = page.getByRole("link", { name: "Datei zurückholen" });
      assert.ok((await restore.boundingBox()).y < 430);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));

      await page.goto(base + "/settings/network?tab=remote");
      await bar.getByText("Netzwerk", { exact: true }).waitFor();
      await page.getByRole("tab", { name: "Fernzugriff" }).waitFor();
      assert.equal(await page.getByRole("button", { name: "Netz hinzufügen" }).count(), 0);
      await page.getByRole("tab", { name: "Übersicht" }).click();
      await page.getByRole("button", { name: "Netz hinzufügen" }).waitFor();

      await page.goto(base + "/settings");
      await page.getByRole("link", { name: "Von unterwegs zugreifen" }).waitFor();
      const search = page.getByRole("searchbox");
      for (const [query, href] of [
        ["von unterwegs", "/settings/network?tab=remote"],
        ["Passwort ändern", "/account#acc-password"],
        ["Daten zurückholen", "/settings/backups/restore"],
      ]) {
        await search.fill(query);
        await page.locator('#settings-home-0 a[href="' + href + '"]').waitFor();
      }
    } finally {
      await ctx.close();
    }
  }
  assert.deepEqual(errors, []);
  console.log("Mobile Seitenköpfe und Suchwege bei 390 und 320 Pixeln geprüft.");
} finally {
  await browser.close();
}
