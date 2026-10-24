import { fixtureFactory } from "./test-ui-fixtures.mjs";
// Isolated API fixtures only. Never talks to a NAS or writes real storage.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5179",
  out = process.env.UI_OUTPUT || "out/validation/ui-operations";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);
try {
  const { ctx, page, data } = await context();
  await page.goto(base + "/tools/maintenance");
  await page
    .getByRole("button", { name: "Zeitplan hinzufügen", exact: true })
    .click();
  let dialog = page.getByRole("dialog");
  await dialog
    .getByRole("textbox", { name: "Name", exact: true })
    .fill("Snapshot plan");
  await dialog
    .getByRole("combobox", { name: "Vorgang", exact: true })
    .selectOption("snapshot");
  await dialog
    .getByRole("combobox", { name: "Ziel", exact: true })
    .selectOption("Fast");
  await dialog
    .getByRole("checkbox", { name: /Ältere automatische Snapshots/ })
    .check();
  await dialog.getByRole("button", { name: "Speichern", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  assert.equal(data.maintenance.settings.jobs[0].kind, "snapshot");
  await page
    .getByRole("button", { name: "Jetzt starten", exact: true })
    .click();
  await page
    .getByRole("heading", { name: "Letzte Ergebnisse", exact: true })
    .waitFor();
  assert.equal(data.maintenance.history.length, 1);
  await page.goto(base + "/tools/monitoring");
  await page.getByRole("img", { name: "CPU (%)", exact: true }).waitFor();
  assert.equal(
    await page.locator('svg[aria-label="CPU (%)"] polyline').count(),
    2,
  );
  await page.getByRole("checkbox", { name: "Aktiviert", exact: true }).check();
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  assert.equal(data.monitor.enabled, true);
  await page.goto(base + "/storage/import");
  await page
    .getByRole("button", { name: "Lesend prüfen", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await dialog
    .getByRole("textbox", { name: "Name", exact: true })
    .fill("Archive");
  await dialog
    .getByRole("textbox", {
      name: "Zur Bestätigung Archive eingeben",
      exact: true,
    })
    .fill("Archive");
  data.failRegister = true;
  await dialog
    .getByRole("button", { name: "Volume registrieren", exact: true })
    .click();
  await dialog.locator(".notice-error").waitFor();
  assert.equal(
    await dialog
      .getByRole("textbox", { name: "Name", exact: true })
      .inputValue(),
    "Archive",
  );
  data.failRegister = false;
  await dialog
    .getByRole("button", { name: "Volume registrieren", exact: true })
    .click();
  await dialog.waitFor({ state: "hidden" });
  assert.equal(data.importCount, 2);
  await page.goto(base + "/storage/manage?volume=Data&mode=grow");
  await page
    .getByRole("combobox", { name: "Neue Platte", exact: true })
    .selectOption("vdd");
  await page
    .getByRole("combobox", { name: "Separates Rettungs-Volume", exact: true })
    .selectOption("Safe");
  await page
    .getByRole("button", { name: "Lesend prüfen", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  assert.equal(data.growStart, 0);
  await dialog.getByRole("checkbox").check();
  await dialog
    .getByRole("textbox", {
      name: "Zur Bestätigung Data eingeben",
      exact: true,
    })
    .fill("Data");
  await dialog
    .getByRole("button", { name: "Kapazität erweitern", exact: true })
    .click();
  await dialog.waitFor({ state: "hidden" });
  assert.equal(data.growStart, 1);
  assert.equal(data.requestsOps.at(-1).identity, "disk-id");
  await page.goto(base + "/storage/manage?volume=Fast&mode=snapshots");
  await page
    .getByRole("combobox", { name: "Snapshots", exact: true })
    .selectOption("before");
  await page.getByRole("checkbox").check();
  await page
    .getByRole("combobox", { name: "Ziel", exact: true })
    .selectOption("Safe");
  await page.getByRole("button", { name: /Auswahl wiederherstellen/ }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Auswahl wiederherstellen", exact: true })
    .click();
  await page.getByText(/Wiederhergestellt:/).waitFor();
  assert.equal(data.restoreCount, 1);
  await page.goto(base + "/vms?vm=source");
  await page.getByRole("heading", { name: "Source VM", exact: true }).waitFor();
  await page
    .getByRole("button", { name: "Weitere Aktionen", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "VM klonen", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Name", exact: true }).fill("copy");
  await dialog.getByRole("button", { name: "VM klonen", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  assert.equal(data.cloneCount, 1);
  await ctx.close();
  for (const theme of ["graphit", "hell"])
    for (const width of [1440, 390]) {
      const test = await context("owner", theme, width);
      for (const route of [
        "/tools/maintenance",
        "/tools/monitoring",
        "/tools/tasks",
        "/storage/import",
        "/storage/manage?volume=Data",
      ]) {
        await test.page.goto(base + route);
        await test.page.getByRole("heading", { level: 1 }).waitFor();
        await test.page.waitForTimeout(150);
        assert.ok(
          await test.page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth + 1,
          ),
          route + " overflow",
        );
        await test.page.screenshot({
          path:
            out +
            `/${route.split("?")[0].replaceAll("/", "-")}-${theme}-${width}.png`,
          fullPage: true,
        });
      }
      await test.ctx.close();
    }
  const user = await context("user");
  await user.page.goto(base + "/tools/maintenance");
  await user.page.getByRole("alert").waitFor();
  assert.equal(
    user.data.requests.filter(([, p]) => p === "/maintenance").length,
    0,
  );
  await user.ctx.close();
  assert.deepEqual(errors, []);
  console.log(
    "Operations UI: maintenance, monitoring gaps/settings, import errors, RAID confirmation, snapshot selection, clone, roles and both mobile/desktop themes passed.",
  );
} finally {
  await browser.close();
}
