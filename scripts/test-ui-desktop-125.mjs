// Local web UI only, with deterministic API fixtures.
import assert from "node:assert/strict";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const base = process.env.UI_URL || "http://127.0.0.1:5278";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const errors = [];
const fixture = fixtureFactory(browser, errors);

try {
  {
    const { ctx, page } = await fixture();
    try {
      await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
      await page.goto(base + "/settings");
      const win = page.locator(".win").first();
      await win.waitFor();
      const settings = page.locator('.dock .slot[data-key="app:settings"] button.it');
      const peek = page.getByRole("group", { name: /Offene Fenster von/ });
      await settings.click();
      await page.locator(".win.min").waitFor({ state: "attached" });
      await page.mouse.move(4, 50);
      await settings.hover();
      await peek.getByText("Minimiert").waitFor();
      await peek.getByRole("button").click();
      await page.locator(".win.focused:not(.min)").waitFor();
      await peek.waitFor({ state: "hidden" });
      await page.mouse.move(4, 50);
      await settings.press("ArrowDown");
      await peek.waitFor();
      await page.keyboard.press("Tab");
      assert.equal(await peek.getByRole("button").evaluate(el => el === document.activeElement), true);
      await page.keyboard.press("Escape");
      await peek.waitFor({ state: "hidden" });
      assert.equal(await settings.evaluate(el => el === document.activeElement), true);
    } finally {
      await ctx.close();
    }
  }
  {
    const { ctx, page } = await fixture("owner", "graphit", 320);
    try {
      await page.goto(base + "/files?root=share/Documents");
      const toolbar = page.locator(".mobile-file-actions");
      await toolbar.getByText("Neu", { exact: true }).waitFor();
      await toolbar.getByText("Hochladen", { exact: true }).waitFor();
      await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth);
      await toolbar.getByRole("button", { name: "Weitere Aktionen", exact: true }).click();
      await toolbar.getByRole("menuitem", { name: "Papierkorb", exact: true }).waitFor();
    } finally {
      await ctx.close();
    }
  }
  {
    const { ctx, page } = await fixture();
    try {
      await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
      await page.goto(base + "/files?root=share/Documents");
      const toolbar = page.frameLocator("iframe[data-window]").first().locator(".command-bar");
      await toolbar.getByText("Neu", { exact: true }).waitFor();
      await toolbar.evaluate(el => el.style.maxWidth = "245px");
      await page.frameLocator("iframe[data-window]").first().locator('.command-bar[data-stage="3"]').waitFor();
      await toolbar.getByText("Neu", { exact: true }).waitFor();
      await toolbar.getByText("Hochladen", { exact: true }).waitFor();
      await toolbar.getByRole("button", { name: "Weitere Aktionen", exact: true }).click();
      await toolbar.getByRole("menuitem", { name: "Für Einfügen kopieren", exact: true }).waitFor();
    } finally {
      await ctx.close();
    }
  }
  {
    const { ctx, page, data } = await fixture();
    try {
      await page.goto(base + "/docker");
      const open = page.getByRole("link", { name: "App öffnen", exact: true });
      assert.equal(await open.getAttribute("href"), "https://photos.example.test/");
      data.webMissing = true;
      await page.reload();
      await page.getByRole("button", { name: /photos starten/i }).waitFor();
      assert.equal(await open.count(), 0);
    } finally {
      await ctx.close();
    }
  }
  {
    const { ctx, page } = await fixture();
    try {
      await page.goto(base + "/tools/monitoring");
      const chart = page.getByRole("slider", { name: "CPU (%)", exact: true });
      const reading = page.locator(".chart .reading").first();
      await chart.focus();
      assert.match(await reading.innerText(), /15/);
      const last = await reading.locator("time").getAttribute("datetime");
      await chart.press("ArrowLeft");
      assert.notEqual(await reading.locator("time").getAttribute("datetime"), last);
      assert.equal((await reading.locator("strong").innerText()).trim(), "14");
      await chart.press("Escape");
      await reading.waitFor({ state: "hidden" });
    } finally {
      await ctx.close();
    }
  }
  assert.deepEqual(errors, []);
  console.log("Dock per Maus und Tastatur, Dateileisten, Docker-Aktionen und Verlaufsmesspunkte geprüft.");
} finally {
  await browser.close();
}
