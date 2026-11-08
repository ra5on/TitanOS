// PLAN 72: storage analysis as a view of the Files window – folders as
// areas (click goes one level deeper), large and old files with marking,
// and the recycle bin after a plain confirmation; on a phone too.
// Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5240 node scripts/test-ui-storage-analysis.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5240";
const out = process.env.UI_OUTPUT || "out/validation/storage-analysis";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const OLD = "2019-08-03T10:00:00Z";
const NEW = "2026-10-02T10:00:00Z";
function usage(path) {
  if (path === "Media")
    return {
      bytes: 2.1e12,
      files: 3000,
      folders: 40,
      children: [
        { path: "Media/Filme", bytes: 1.4e12, files: 900, dir: true, sub: [{ path: "Media/Filme/Eigene", bytes: 9e11, files: 200, dir: true }] },
        { path: "Media/Serien", bytes: 5e11, files: 1500, dir: true },
        { path: "Media/Musik", bytes: 2e11, files: 600, dir: true },
      ],
      largest: [],
      old: [],
      olderThan: "2023-11-08T00:00:00Z",
    };
  return {
    bytes: 3.4e12,
    files: 412000,
    folders: 9000,
    children: [
      { path: "Media", bytes: 2.1e12, files: 3000, dir: true, sub: [
        { path: "Media/Filme", bytes: 1.4e12, files: 900, dir: true },
        { path: "Media/Serien", bytes: 5e11, files: 1500, dir: true },
        { path: "Media/Musik", bytes: 2e11, files: 600, dir: true },
      ] },
      { path: "Backups", bytes: 6.2e11, files: 40, dir: true, sub: [
        { path: "Backups/Laptop Maria", bytes: 4.1e11, files: 20, dir: true },
        { path: "Backups/Handy", bytes: 2.1e11, files: 20, dir: true },
      ] },
      { path: "Fotos", bytes: 4.1e11, files: 400000, dir: true },
      { path: "Projects", bytes: 9.5e10, files: 7000, dir: true },
      { path: "notiz.txt", bytes: 2e3, files: 1, dir: false },
    ],
    largest: [
      { path: "Media/Filme/Eigene/Urlaub 2019 Rohschnitt.mov", bytes: 8.4e10, files: 1, dir: false, modified: OLD },
      { path: "Backups/VMs/win10-alt.qcow2", bytes: 6.4e10, files: 1, dir: false, modified: "2023-01-12T10:00:00Z" },
      { path: "Backups/Laptop Maria/Laptop.img", bytes: 5.8e10, files: 1, dir: false, modified: NEW },
    ],
    old: [
      { path: "Media/Filme/Eigene/Urlaub 2019 Rohschnitt.mov", bytes: 8.4e10, files: 1, dir: false, modified: OLD },
      { path: "Projects/Archiv/Installationsdateien.zip", bytes: 2.3e10, files: 1, dir: false, modified: OLD },
    ],
    olderThan: "2023-11-08T00:00:00Z",
  };
}

async function open(width, height, phone = false) {
  const made = await create("owner", "graphit", width, "de",
    phone ? { isMobile: true, hasTouch: true, deviceScaleFactor: 2 } : {});
  const { ctx, page } = made;
  made.usageCalls = [];
  made.deletes = [];
  await page.setViewportSize({ width, height });
  await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  await ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    if (p === "/files/roots")
      return route.fulfill({ json: [{ id: "share/Data", name: "Data", kind: "share", writable: true, recycle: true, totalBytes: 4e12, freeBytes: 6e11 }] });
    if (p === "/files/list")
      return route.fulfill({ json: [{ name: "Media", dir: true, size: 0, modified: NEW }] });
    if (p === "/files/usage") {
      const body = route.request().postDataJSON();
      made.usageCalls.push(body);
      return route.fulfill({ json: usage(body.path) });
    }
    if (p === "/files/delete") {
      made.deletes.push(route.request().postDataJSON());
      return route.fulfill({ status: 204, body: "" });
    }
    return route.fallback();
  });
  await page.goto(base + "/files?root=share/Data");
  return made;
}

async function openAnalysis(ui) {
  // 0.70: the Explorer-like command bar keeps it in "Mehr" (…).
  const tools = ui.getByRole("button", { name: /Werkzeuge/ });
  if (await tools.count()) await tools.first().click();
  else await ui.locator(".command-bar").getByRole("button", { name: "Weitere Aktionen", exact: true }).click();
  await ui.getByRole("menuitem", { name: "Speicher-Analyse" }).click();
  await ui.getByText("412,000 Dateien", { exact: false }).or(ui.getByText(/412\.000 Dateien|412000 Dateien|412,000 Dateien/)).first().waitFor({ timeout: 15000 });
}

// Desktop: in the window.
{
  const made = await open(1440, 900);
  const { ctx, page } = made;
  const ui = page.frameLocator("iframe[data-window]").first();
  await ui.locator('[data-file-name="Media"]').first().waitFor({ timeout: 15000 });
  await openAnalysis(ui);
  // Areas: a button per folder, the largest first.
  const media = ui.getByRole("button", { name: /^Media\s/ }).first();
  await media.waitFor();
  await ui.getByRole("button", { name: /Laptop Maria/ }).first().waitFor();
  assert.equal(made.usageCalls.at(-1).olderThanDays, 1095, "three years by default");
  await page.locator("section.win").screenshot({ path: `${out}/ordner.png` });
  // A click on an area goes one level deeper.
  await media.click();
  await ui.getByRole("button", { name: /^Filme\s/ }).first().waitFor();
  assert.equal(made.usageCalls.at(-1).path, "Media");
  await ui.getByLabel("Speicher-Analyse").getByRole("button", { name: "Data", exact: true }).click();
  await ui.getByRole("button", { name: /^Backups\s/ }).first().waitFor();
  // Large files: mark two, move them to the bin after the confirmation.
  await ui.getByRole("tab", { name: "Große Dateien" }).click();
  await ui.getByRole("checkbox", { name: "Urlaub 2019 Rohschnitt.mov" }).check();
  await ui.getByRole("checkbox", { name: "win10-alt.qcow2" }).check();
  await ui.getByText("2 markiert · 148 GB").waitFor();
  await page.locator("section.win").screenshot({ path: `${out}/gross.png` });
  await ui.getByRole("button", { name: "In den Papierkorb" }).click();
  const dialog = ui.getByRole("dialog", { name: "In den Papierkorb legen?" });
  await dialog.waitFor();
  // Enter on the opened dialog never moves anything.
  await page.keyboard.press("Enter");
  await page.waitForTimeout(300);
  assert.equal(made.deletes.length, 0, "Enter does not confirm");
  // Enter may have pressed "Cancel" (it has the focus); open it again.
  if (!(await dialog.isVisible())) await ui.getByRole("button", { name: "In den Papierkorb" }).click();
  await dialog.getByRole("button", { name: "In den Papierkorb" }).click();
  await dialog.waitFor({ state: "detached" });
  assert.deepEqual(made.deletes, [{ root: "share/Data", paths: ["Media/Filme/Eigene/Urlaub 2019 Rohschnitt.mov", "Backups/VMs/win10-alt.qcow2"], permanent: false }]);
  // Old files: the filter goes to the server.
  await ui.getByRole("tab", { name: "Alte Dateien" }).click();
  await ui.getByText("Installationsdateien.zip").waitFor();
  await ui.getByLabel("Nicht geändert seit").selectOption("5");
  await page.waitForTimeout(300);
  assert.equal(made.usageCalls.at(-1).olderThanDays, 1825);
  await page.locator("section.win").screenshot({ path: `${out}/alt.png` });
  // Back to the files.
  await ui.getByRole("button", { name: /Zurück zu den Dateien/ }).click();
  await ui.locator('[data-file-name="Media"]').first().waitFor();
  await ctx.close();
}

// Phone: same view, nothing overflows.
{
  const made = await open(390, 844, true);
  const { ctx, page } = made;
  await page.locator('[data-file-name="Media"]').first().waitFor({ timeout: 15000 });
  const tools = page.getByRole("button", { name: /Werkzeuge/ });
  if (await tools.count()) await openAnalysis(page);
  else {
    await page.getByRole("button", { name: /Mehr|Weitere/ }).first().click();
    await page.getByRole("menuitem", { name: "Speicher-Analyse" }).click();
  }
  await page.getByRole("button", { name: /^Media\s/ }).first().waitFor({ timeout: 15000 });
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  assert.ok(overflow <= 1, `no horizontal scroll on the phone (${overflow})`);
  await page.screenshot({ path: `${out}/handy-ordner.png` });
  await page.getByRole("tab", { name: "Große Dateien" }).click();
  await page.getByText("Laptop.img").waitFor();
  await page.screenshot({ path: `${out}/handy-gross.png` });
  await ctx.close();
}

await browser.close();
assert.deepEqual(errors.filter((e) => !/favicon/.test(e)), []);
console.log("storage analysis UI ok:", out);
