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
  out = process.env.UI_OUTPUT || "out/validation/ui-workflows";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);
try {
  const { ctx, page, data } = await context();
  await page.goto(base + "/shares");
  await page.getByRole("button", { name: "Documents", exact: true }).waitFor();
  assert.equal(await page.locator("tbody tr").count(), 3);
  await page.getByRole("button", { name: "Documents", exact: true }).click();
  await page.getByRole("button", { name: "Verbinden", exact: true }).click();
  assert.equal(
    await page.locator("dialog code").first().innerText(),
    "\\\\192.168.1.20\\Documents",
  );
  await page.keyboard.press("Escape");
  await page
    .getByRole("link", { name: "In Dateien öffnen", exact: true })
    .click();
  await page.waitForURL(/\/files\?root=share/);
  await page.waitForFunction(() =>
    document.body.textContent.includes("Documents"),
  );
  await page.waitForTimeout(300);
  assert.ok(data.files.includes("share/Documents"));
  await page.goto(base + "/files?root=share%2FDocuments&path=archive");
  await page.getByRole("button", { name: "archive", exact: true }).waitFor();
  assert.ok(data.filePaths.includes("archive"));
  await page.goto(base + "/shares");
  await page
    .getByRole("button", { name: "Neue Freigabe", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.locator("input[type=text]").fill("New Share");
  await dialog.getByRole("tab", { name: "Zugriff", exact: true }).click();
  await dialog.getByRole("tab", { name: "Allgemein", exact: true }).click();
  assert.equal(
    await dialog.locator("input[type=text]").inputValue(),
    "New Share",
  );
  await page.keyboard.press("Escape");
  await page
    .getByText("Es gibt ungespeicherte Änderungen.", { exact: true })
    .waitFor();
  await page
    .getByRole("button", { name: "Weiterbearbeiten", exact: true })
    .click();
  assert.equal(
    await dialog.locator("input[type=text]").inputValue(),
    "New Share",
  );
  data.failSave = true;
  await dialog
    .getByRole("button", { name: "Freigabe anlegen", exact: true })
    .click();
  await dialog.locator("[role=alert]").waitFor();
  assert.equal(
    await dialog.locator("input[type=text]").inputValue(),
    "New Share",
  );
  data.failSave = false;
  await dialog
    .getByRole("button", { name: "Freigabe anlegen", exact: true })
    .click();
  await dialog.waitFor({ state: "hidden" });
  await page.getByRole("button", { name: "New Share", exact: true }).waitFor();
  await page
    .getByRole("button", { name: "Aktionen für Media", exact: true })
    .click();
  await page
    .getByRole("menuitem", { name: "Freigabe löschen", exact: true })
    .click();
  data.failDelete = true;
  await page.keyboard.press("Enter");
  assert.equal(data.deleted, 0);
  if (!(await page.getByRole("dialog").count())) {
    await page
      .getByRole("button", { name: "Aktionen für Media", exact: true })
      .click();
    await page
      .getByRole("menuitem", { name: "Freigabe löschen", exact: true })
      .click();
  }
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Freigabe löschen", exact: true })
    .click();
  await page.getByRole("dialog").locator("[role=alert]").waitFor();
  assert.equal(await page.getByRole("dialog").count(), 1);
  data.failDelete = false;
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Freigabe löschen", exact: true })
    .click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  await page
    .getByRole("combobox", { name: "Volume", exact: true })
    .selectOption("Fast");
  await page.waitForTimeout(150);
  assert.equal(data.putShares.at(-1).volume, "Fast");
  await page.reload();
  await page.getByRole("button", { name: "Projects", exact: true }).waitFor();
  assert.equal(await page.locator("tbody tr").count(), 1);
  await page.goto(base + "/docker");
  const stack = page.locator(".stack").first();
  await stack.waitFor();
  assert.equal(await stack.locator(".members").count(), 0);
  await stack.getByRole("link", { name: "WebUI", exact: true }).waitFor();
  assert.equal(
    await stack
      .getByRole("link", { name: "WebUI", exact: true })
      .getAttribute("href"),
    "http://192.168.1.20:2283/",
  );
  assert.equal(
    await stack.getByRole("link", { name: "Öffentlich", exact: true }).count(),
    1,
  );
  await stack
    .getByRole("button", { name: "Verknüpfungen", exact: true })
    .click();
  await page.getByRole("dialog").locator("select").selectOption("web");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Speichern", exact: true })
    .click();
  assert.equal(data.putDocker.at(-1).primaryServices["app:photos"], "web");
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  await page
    .getByRole("searchbox", { name: "Container suchen …", exact: true })
    .fill("db");
  await stack.locator(".members").waitFor();
  assert.equal(await stack.locator(".row").count(), 1);
  await page
    .getByRole("searchbox", { name: "Container suchen …", exact: true })
    .fill("");
  await stack
    .getByRole("button", { name: "1 Dienste prüfen", exact: true })
    .click();
  assert.equal(await stack.locator(".row").count(), 2);
  data.failDocker = true;
  await page
    .getByText(/Angezeigte Werte können veraltet sein/)
    .waitFor({ timeout: 15000 });
  assert.equal(await stack.locator(".row").count(), 2);
  data.failDocker = false;
  await page
    .getByText(/Angezeigte Werte können veraltet sein/)
    .waitFor({ state: "hidden", timeout: 15000 });
  data.webMissing = true;
  await page
    .getByText("Der zugeordnete Dienst web fehlt.", { exact: true })
    .waitFor({ timeout: 15000 });
  // Command palette loads only authorized roots and navigates, never runs a NAS action.
  await page.keyboard.press("Control+k");
  await page.getByRole("dialog").waitFor();
  await page.getByRole("dialog").getByRole("searchbox").fill("SSH");
  await page.getByRole("button", { name: /Zugang › SSH/ }).click();
  await page.waitForURL(/settings\/access#access-ssh/);
  await page.goto(base + "/shares");
  await page.getByRole("button", { name: "Projects", exact: true }).waitFor();
  await page.screenshot({ path: out + "/shares-desktop.png", fullPage: true });
  await ctx.close();
  for (const theme of ["graphit", "hell"])
    for (const width of [1440, 390]) {
      const test = await context("owner", theme, width);
      await test.page.goto(base + "/shares");
      await test.page
        .getByRole("button", { name: "Documents", exact: true })
        .waitFor();
      await test.page.screenshot({
        path: out + `/shares-${theme}-${width}.png`,
        fullPage: true,
      });
      assert.ok(
        await test.page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      );
      await test.page.goto(base + "/docker");
      await test.page.locator(".stack-toggle").waitFor();
      assert.equal(await test.page.locator(".members").count(), 0);
      await test.page.screenshot({
        path: out + `/docker-${theme}-${width}.png`,
        fullPage: true,
      });
      assert.ok(
        await test.page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      );
      await test.ctx.close();
    }
  const user = await context("user");
  await user.page.goto(base + "/files");
  await user.page
    .getByRole("heading", { name: "Dateien", exact: true })
    .waitFor();
  await user.page.keyboard.press("Control+k");
  await user.page.getByRole("dialog").waitFor();
  assert.equal(
    await user.page
      .locator("#palette-results")
      .getByRole("button", { name: /Docker/ })
      .count(),
    0,
  );
  assert.equal(
    user.data.requests.filter(([, p]) => p === "/docker" || p === "/vms")
      .length,
    0,
  );
  await user.ctx.close();
  assert.deepEqual(errors, []);
  console.log(
    "UI workflows: shares, form state, delete failure, preferences, stack links/search/stale status, palette roles and desktop/mobile passed.",
  );
} catch (error) {
  console.error("Browser errors:", errors);
  for (const context of browser.contexts())
    for (const page of context.pages())
      await page
        .screenshot({ path: out + "/failure.png", fullPage: true })
        .catch(() => {});
  throw error;
} finally {
  await browser.close();
}
