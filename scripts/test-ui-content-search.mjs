// PLAN 72: full-text search shows the passage around the match with the
// searched words marked; where an index exists, content search is the default.
// Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5240 node scripts/test-ui-content-search.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5240";
const out = process.env.UI_OUTPUT || "out/validation/content-search";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

for (const [width, height] of [[1440, 900], [390, 844]]) {
  const { ctx, page } = await create("owner", "graphit", width, "de",
    width < 500 ? { isMobile: true, hasTouch: true, deviceScaleFactor: 2 } : {});
  await page.setViewportSize({ width, height });
  await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  const searches = [];
  await ctx.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    if (p === "/files/roots")
      return route.fulfill({ json: [{ id: "share/Documents", name: "Documents", kind: "share", writable: true, recycle: true, indexed: true, totalBytes: 1e12, freeBytes: 6e11 }] });
    if (p === "/files/list") return route.fulfill({ json: [{ name: "Versicherungen", dir: true, size: 0, modified: "2026-01-14T10:00:00Z" }] });
    if (p === "/files/search") {
      searches.push(route.request().postDataJSON());
      return route.fulfill({ json: { scanned: 2, truncated: false, entries: [
        { name: "Kfz-Versicherung 2026.pdf", path: "Versicherungen/Kfz-Versicherung 2026.pdf", size: 210000, modified: "2026-01-14T10:00:00Z", dir: false,
          excerpt: "… Ihre Kfz-Versicherung verlängert sich um ein Jahr, wenn sie nicht bis 30.11. gekündigt wird …" },
        { name: "<b>Notiz<\/b>.txt", path: "Notiz <b>alt<\/b>.txt", size: 120, modified: "2026-03-08T10:00:00Z", dir: false,
          excerpt: "<img src=x onerror=alert(1)> neue Versicherung vergleichen" },
      ] } });
    }
    return route.fallback();
  });
  await page.goto(base + "/files?root=share/Documents");
  const ui = width < 500 ? page : page.frameLocator("iframe[data-window]").first();
  await ui.locator('[data-file-name="Versicherungen"]').first().waitFor({ timeout: 15000 });
  // PLAN 73: in a window the tools are in the "…" of the command bar.
  const tools = ui.getByRole("button", { name: /Werkzeuge/ });
  const bar = ui.locator(".command-bar");
  if (await tools.count()) await tools.first().click();
  else if (await bar.isVisible())
    await bar.getByRole("button", { name: "Weitere Aktionen" }).click({ timeout: 5000 });
  else await ui.getByRole("button", { name: /Mehr|Weitere/ }).first().click({ timeout: 5000 });
  await ui.getByRole("menuitem", { name: "Unterordner durchsuchen" }).click();
  const dialog = ui.getByRole("dialog", { name: "Unterordner durchsuchen" });
  await dialog.waitFor();
  assert.equal(await dialog.getByRole("checkbox", { name: /Im Inhalt suchen/ }).isChecked(), true, "content search by default");
  await dialog.getByRole("textbox", { name: /Wörter/ }).fill("kfz versicherung");
  await dialog.getByRole("button", { name: "Suche starten" }).click();
  await dialog.getByText("verlängert sich um ein Jahr", { exact: false }).waitFor();
  assert.equal(searches.at(-1).content, true);
  const marks = await dialog.locator("mark").allTextContents();
  assert.deepEqual(marks, ["Kfz", "Versicherung", "Versicherung"]);
  // Excerpts and names are text, never HTML.
  assert.equal(await dialog.locator(".excerpt img, strong b").count(), 0);
  await dialog.getByText("<img src=x onerror=alert(1)>", { exact: false }).waitFor();
  if (width > 500) await page.locator("section.win").screenshot({ path: `${out}/suche.png` });
  else {
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    assert.ok(overflow <= 1, `no horizontal scroll (${overflow})`);
    await page.screenshot({ path: `${out}/suche-handy.png` });
  }
  await ctx.close();
}
await browser.close();
assert.deepEqual(errors.filter((e) => !/favicon/.test(e)), []);
console.log("content search UI ok:", out);
