// Tools › Activity. Isolated API data only: no request reaches a NAS.
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
const base = process.env.UI_URL || "http://127.0.0.1:5188";
const out = process.env.UI_OUTPUT || "out/validation/ui-activity";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

try {
  {
    const { ctx, page, data } = await create("owner");
    await page.goto(base + "/tools/activity");
    const list = page.getByRole("region", { name: "Einträge" });
    await list.getByText("Dateien gelöscht").waitFor();
    assert.match(await list.textContent(), /Media\/alt\.mkv/);
    assert.match(await list.textContent(), /Anmeldung fehlgeschlagen/);
    assert.match(await list.textContent(), /System: geändert/);
    assert.match(await list.textContent(), /per API-Token/);
    await page.screenshot({ path: `${out}/activity-desktop.png`, fullPage: true });

    await page.getByLabel("Konto").selectOption("sam");
    await list.getByText("Anmeldung fehlgeschlagen").waitFor();
    assert.equal(await list.getByText("Dateien gelöscht").count(), 0);
    assert.equal(data.activity.queries.at(-1).user, "sam");
    await page.getByLabel("Konto").selectOption("");
    await page.getByLabel("Nur Fehlschläge").check();
    assert.equal(data.activity.queries.at(-1).failed, "1");
    await page.getByLabel("Nur Fehlschläge").uncheck();

    await page.getByRole("button", { name: "CSV exportieren" }).click();
    await page.waitForFunction(() => true);
    assert.ok(data.activity.queries.some((q) => q.format === "csv"));

    // Retention: saving is only possible after a change.
    const save = page.getByRole("button", { name: "Speichern" });
    assert.equal(await save.isDisabled(), true);
    await page.getByLabel("Aufbewahrung (Tage)").selectOption("365");
    await save.click();
    await page.getByRole("status").getByText("Gespeichert").waitFor();
    assert.equal(data.activity.settings.days, 365);

    await page.setViewportSize({ width: 375, height: 812 });
    await page.reload();
    await list.getByText("Dateien gelöscht").waitFor();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
    assert.ok(overflow <= 1, `horizontal scroll ${overflow}`);
    await page.screenshot({ path: `${out}/activity-mobile.png`, fullPage: true });
    await ctx.close();
  }
  {
    const { ctx, page } = await create("owner");
    await page.goto(base + "/tools/activity");
    await page.getByLabel("Aktiviert").uncheck();
    await page.getByRole("button", { name: "Speichern" }).click();
    await page.getByText("Das Aktivitätsprotokoll ist ausgeschaltet").waitFor();
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("activity UI checks passed");
} finally {
  await browser.close();
}
