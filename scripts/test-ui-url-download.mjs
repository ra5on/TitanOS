// "Download from link" in the file manager. Isolated API data only: no
// request reaches a NAS or the internet, nothing is downloaded.
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
const out = process.env.UI_OUTPUT || "out/validation/ui-url-download";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

async function openFromNewMenu(page, label = "Neu", entry = "Von Link herunterladen") {
  await page.getByRole("button", { name: label, exact: true }).filter({ visible: true }).first().click();
  await page.getByRole("menuitem", { name: entry }).filter({ visible: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.waitFor();
  return dialog;
}

try {
  // Owner: probe fills the name, an existing name offers "keep both".
  {
    const { ctx, page, data } = await create("owner");
    data.urlProbe = { name: "debian-13.iso", size: 4e9, type: "application/x-iso9660-image", exists: true, free: 6e11 };
    data.urlConflict = true;
    await page.goto(base + "/files?root=share/Media&path=");
    const dialog = await openFromNewMenu(page);
    assert.match(await dialog.textContent(), /Von Link herunterladen/);
    const link = dialog.getByLabel("Link (http oder https)");
    await link.fill("ftp://example.test/a.iso");
    assert.match(await dialog.textContent(), /vollständigen http- oder https-Link/);
    assert.equal(await dialog.getByRole("button", { name: "Herunterladen", exact: true }).isDisabled(), true);
    await link.fill("https://example.test/isos/debian%2013.iso?token=secret");
    assert.equal(await dialog.getByLabel("Name", { exact: true }).inputValue(), "debian 13.iso");
    await dialog.getByRole("button", { name: "Name und Größe abfragen" }).click();
    await page.waitForFunction(() => document.querySelector("dialog input:not([type=url])")?.value === "debian-13.iso");
    assert.match(await dialog.textContent(), /4 GB|3,7 GiB|4,0 GB|GB/);
    assert.deepEqual(data.urlProbes.at(-1), { root: "share/Media", dir: "", url: "https://example.test/isos/debian%2013.iso?token=secret" });
    assert.match(await dialog.textContent(), /gibt es in diesem Ordner schon/);
    assert.doesNotMatch(await dialog.textContent(), /Benutzer|nur Adressen im Internet/);
    const digest = dialog.getByLabel("Erwartete SHA-256-Prüfsumme");
    await digest.fill("abc");
    assert.equal(await dialog.getByRole("button", { name: "Herunterladen", exact: true }).isDisabled(), true);
    await digest.fill("A".repeat(64));
    await page.screenshot({ path: `${out}/owner-dialog.png` });
    await dialog.getByRole("button", { name: "Herunterladen", exact: true }).click();
    await dialog.getByRole("button", { name: "Beide behalten" }).waitFor();
    assert.equal(data.urlDownloads.length, 1);
    assert.equal(data.urlDownloads[0].conflict, "");
    await page.screenshot({ path: `${out}/owner-conflict.png` });
    await dialog.getByRole("button", { name: "Beide behalten" }).click();
    await dialog.waitFor({ state: "detached" });
    assert.deepEqual(data.urlDownloads[1], {
      root: "share/Media",
      dir: "",
      url: "https://example.test/isos/debian%2013.iso?token=secret",
      name: "debian-13.iso",
      sha256: "A".repeat(64),
      conflict: "rename",
    });
    // The job shows in the existing jobs list with size, speed and cancel.
    const jobs = page.getByRole("region", { name: "Dateiaufträge" });
    await jobs.first().waitFor();
    assert.match(await jobs.first().textContent(), /Von Link herunterladen/);
    assert.match(await jobs.first().textContent(), /debian-13\.iso/);
    await jobs.first().getByRole("button", { name: "Abbrechen" }).waitFor();
    await page.screenshot({ path: `${out}/owner-job.png` });
    // The empty-area context menu offers the same entry.
    const browserArea = page.locator("section.browser");
    const box = await browserArea.boundingBox();
    await page.mouse.click(box.x + box.width - 20, box.y + box.height - 20, { button: "right" });
    await page.getByRole("menuitem", { name: "Von Link herunterladen" }).waitFor();
    await page.keyboard.press("Escape");
    await ctx.close();
  }
  // A user account is told about the public-only rule.
  {
    const { ctx, page } = await create("user");
    await page.goto(base + "/files?root=share/Media&path=");
    const dialog = await openFromNewMenu(page);
    assert.match(await dialog.textContent(), /nur Adressen im Internet/);
    await dialog.getByRole("button", { name: "Abbrechen" }).click();
    await ctx.close();
  }
  // Read-only shares offer no "New" menu, hence no download.
  {
    const { ctx, page, data } = await create("owner");
    data.readOnlyShares = ["Media"];
    await page.goto(base + "/files?root=share/Media&path=");
    await page.locator("section.browser").waitFor();
    assert.equal(await page.getByRole("button", { name: "Neu", exact: true }).filter({ visible: true }).count(), 0);
    assert.equal(await page.getByRole("menuitem", { name: "Von Link herunterladen" }).count(), 0);
    await ctx.close();
  }
  // Phone width and English texts.
  {
    const { ctx, page } = await create("owner", "graphit", 390, "en");
    await page.goto(base + "/files?root=share/Media&path=");
    const dialog = await openFromNewMenu(page, "New", "Download from link");
    assert.match(await dialog.textContent(), /Expected SHA-256 checksum/);
    const width = await page.evaluate(() => document.documentElement.scrollWidth);
    assert.ok(width <= 390, `page scrolls sideways: ${width}`);
    await page.screenshot({ path: `${out}/mobile-en.png` });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("url download UI: ok");
} finally {
  await browser.close();
}
