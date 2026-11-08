// Tools › Scripts against isolated fixtures: list with triggers and last
// runs, start with live output, cancel, editor with schedule and cron,
// delete in the danger zone with a plain confirmation (nothing to type),
// the log of the runs, a missing volume, no access for users, phone width
// and light theme. Screenshots: desktop 1440 and phone 390, Graphit and Hell.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs UI_URL=http://127.0.0.1:5199 node scripts/test-ui-scripts.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5173";
const out = process.env.UI_OUTPUT || "out/validation/skripte";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

let lastPage;
async function open(role = "owner", theme = "graphit", width = 1440, prepare) {
  const made = await context(role, theme, width);
  lastPage = made.page;
  prepare?.(made.data);
  if (width < 761) await made.page.setViewportSize({ width, height: 844 });
  await made.page.goto(base + "/tools/scripts");
  await made.page.locator("main").waitFor();
  await made.page.waitForLoadState("networkidle");
  return made;
}
const row = (page, name) => page.locator("li.script").filter({ hasText: name });
const noSideScroll = (page) =>
  page.evaluate(
    () =>
      document.documentElement.scrollWidth <=
      document.documentElement.clientWidth + 1,
  );

try {
  {
    // PLAN 61/65: the side bar of the Tools window leads here.
    const { ctx, page } = await context("owner", "graphit", 1440);
    await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
    // The disk tests on that page are not part of the shared fixtures.
    await ctx.route("**/api/v1/storage/tests", (r) =>
      r.fulfill({ json: { tests: [] } }),
    );
    await page.goto(base + "/tools");
    const side = page.locator("section.win nav.side");
    await side.getByRole("button", { name: "Skripte", exact: true }).click();
    const frame = page.frameLocator("section.win iframe").first();
    await frame
      .getByRole("heading", { name: "Skripte", level: 1 })
      .waitFor({ timeout: 15000 });
    assert.match(
      await frame
        .locator("main")
        .first()
        .evaluate(() => location.pathname),
      /\/tools\/scripts$/,
    );
    await ctx.close();
  }
  {
    const { ctx, page, data } = await open();
    assert.equal(await page.locator("li.script").count(), 4);
    assert.ok(
      await row(page, "Fotos sichern")
        .getByText("Täglich um 03:00")
        .isVisible(),
    );
    assert.ok(
      await row(page, "Docker aufräumen")
        .getByText("Sonntags um 04:00 Uhr")
        .isVisible(),
    );
    assert.ok(
      await row(page, "Docker aufräumen")
        .getByText("fehlgeschlagen (Exit-Code 1)")
        .isVisible(),
    );
    assert.ok(
      await row(page, "Lüfterkurve setzen").getByText("Beim Start").isVisible(),
    );
    assert.ok(
      await row(page, "Sync vor dem Ausschalten")
        .getByText("Noch nicht gelaufen")
        .isVisible(),
    );
    assert.ok(await page.getByText("/mnt/Data/.titanos/scripts").isVisible());
    assert.ok(
      await page
        .getByText("Skripte laufen mit vollen Rechten (root)", { exact: false })
        .isVisible(),
    );
    await page.screenshot({
      path: `${out}/liste-graphit-1440.png`,
      fullPage: true,
    });

    // Start: the output follows live, then shows the result.
    await row(page, "Fotos sichern")
      .getByRole("button", { name: "Starten" })
      .click();
    const output = row(page, "Fotos sichern").locator(".output");
    await output.getByText("Sicherung beginnt", { exact: false }).waitFor();
    await output.getByText("12 Dateien kopiert", { exact: false }).waitFor();
    await output.getByText("erfolgreich").waitFor();
    assert.ok(
      data.scriptCalls.some(
        ([m, p]) => m === "POST" && p === "/scripts/a1a1a1a1a1a1a1a1/run",
      ),
    );
    await page.screenshot({
      path: `${out}/ausgabe-graphit-1440.png`,
      fullPage: true,
    });

    // A run that does not end by itself is canceled.
    data.scriptHold = true;
    await row(page, "Docker aufräumen")
      .getByRole("button", { name: "Starten" })
      .click();
    const cancel = row(page, "Docker aufräumen").getByRole("button", {
      name: "Abbrechen",
    });
    await cancel.waitFor();
    await cancel.click();
    await row(page, "Docker aufräumen")
      .locator(".output")
      .getByText("abgebrochen")
      .waitFor();
    data.scriptHold = false;

    // The log of the runs.
    await row(page, "Fotos sichern")
      .getByRole("button", { name: "Protokoll" })
      .click();
    const runs = page.getByRole("dialog", {
      name: "Protokoll von Fotos sichern",
    });
    await runs.waitFor();
    await runs.getByText("Zeit abgelaufen").waitFor();
    await runs.locator("summary").first().click();
    await runs.getByText("12 Dateien kopiert", { exact: false }).waitFor();
    await runs.evaluate((d) =>
      Promise.all(d.getAnimations({ subtree: true }).map((a) => a.finished)),
    );
    await page.screenshot({ path: `${out}/protokoll-graphit-1440.png` });
    await runs.getByRole("button", { name: "Schließen" }).click();

    // A new script on a custom schedule.
    await page.getByRole("button", { name: "Neues Skript" }).click();
    const editor = page.getByRole("dialog", { name: "Neues Skript" });
    await editor.waitFor();
    await editor.getByRole("textbox", { name: "Name" }).fill("Nachts prüfen");
    await editor.locator("select").first().selectOption("schedule");
    await editor.getByText("Erweitert").click();
    await editor.locator("input.mono").fill("30 2 * * 1-5");
    await editor
      .getByText("Läuft werktags (Montag bis Freitag) um 02:30 Uhr.")
      .waitFor();
    await editor
      .getByRole("button", { name: "Was bedeutet „Cron-Ausdruck“?" })
      .click();
    await page.getByText("Fünf Felder", { exact: false }).waitFor();
    await page.keyboard.press("Escape");
    await editor.locator("textarea").fill("#!/bin/bash\necho hallo\n");
    await page.screenshot({ path: `${out}/editor-graphit-1440.png` });
    await editor.getByRole("button", { name: "Speichern" }).click();
    await editor.waitFor({ state: "detached" });
    const posted = JSON.parse(
      data.scriptCalls.findLast(
        ([m, p]) => m === "POST" && p === "/scripts",
      )[2],
    );
    assert.deepEqual(posted.schedule, {
      every: "custom",
      cron: "30 2 * * 1-5",
    });
    assert.equal(posted.trigger, "schedule");
    assert.equal(posted.content, "#!/bin/bash\necho hallo\n");
    await row(page, "Nachts prüfen")
      .getByText("Werktags (Montag bis Freitag) um 02:30 Uhr")
      .waitFor();

    // Delete: danger zone, plain confirmation, nothing to type.
    await row(page, "Sync vor dem Ausschalten")
      .getByRole("button", { name: "Bearbeiten" })
      .click();
    const edit = page.getByRole("dialog", { name: "Skript bearbeiten" });
    await edit.locator("textarea").waitFor();
    assert.equal(
      await edit.locator("textarea").inputValue(),
      "#!/bin/bash\nsync\n",
    );
    assert.ok(await edit.getByText("Gefahrenzone").isVisible());
    await edit.getByRole("button", { name: "Skript löschen" }).click();
    const confirm = page.getByRole("dialog", { name: "Skript löschen?" });
    await confirm.waitFor();
    assert.equal(await confirm.locator("input").count(), 0, "nothing to type");
    await confirm.evaluate((d) =>
      Promise.all(d.getAnimations({ subtree: true }).map((a) => a.finished)),
    );
    await page.screenshot({ path: `${out}/loeschen-graphit-1440.png` });
    await confirm.getByRole("button", { name: "Skript löschen" }).click();
    await row(page, "Sync vor dem Ausschalten").waitFor({ state: "detached" });
    assert.ok(
      data.scriptCalls.some(
        ([m, p]) => m === "DELETE" && p === "/scripts/d4d4d4d4d4d4d4d4",
      ),
    );

    // Another volume.
    await page.locator("form.place select").selectOption("Fast");
    await page
      .locator("form.place")
      .getByRole("button", { name: "Speichern" })
      .click();
    await page.getByText("/mnt/Fast/.titanos/scripts").waitFor();
    assert.ok(
      data.scriptCalls.some(
        ([m, p, b]) =>
          m === "PUT" && p === "/scripts/volume" && b.includes("Fast"),
      ),
    );
    await ctx.close();
  }
  {
    // The volume is gone: said clearly, nothing to start.
    const { ctx, page } = await open("owner", "graphit", 1440, (data) => {
      data.scripts.available = false;
      data.scripts.volumes = ["Fast"];
      data.scripts.scripts = [];
    });
    await page
      .getByText("Das Volume Data mit den Skripten ist nicht eingebunden", {
        exact: false,
      })
      .waitFor();
    assert.ok(
      await page.getByRole("button", { name: "Neues Skript" }).isDisabled(),
    );
    await page.screenshot({ path: `${out}/volume-fehlt-graphit-1440.png` });
    await ctx.close();
  }
  {
    // Users get no scripts.
    const { ctx, page, data } = await open("user");
    await page.getByText("Dafür fehlen diesem Konto die Rechte.").waitFor();
    assert.equal(data.scriptCalls.length, 0);
    await ctx.close();
  }
  {
    const { ctx, page } = await open("owner", "hell", 1440);
    await row(page, "Fotos sichern")
      .getByRole("button", { name: "Ausgabe" })
      .click();
    await row(page, "Fotos sichern").locator(".output").waitFor();
    await page.screenshot({
      path: `${out}/liste-hell-1440.png`,
      fullPage: true,
    });
    await ctx.close();
  }
  for (const theme of ["graphit", "hell"]) {
    const { ctx, page } = await open("owner", theme, 390);
    assert.ok(await noSideScroll(page), "no sideways scrolling at 390 px");
    await page.screenshot({
      path: `${out}/liste-${theme}-390.png`,
      fullPage: true,
    });
    await row(page, "Fotos sichern")
      .getByRole("button", { name: "Bearbeiten" })
      .click();
    const edit = page.getByRole("dialog", { name: "Skript bearbeiten" });
    await edit.locator("textarea").waitFor();
    const box = await edit.boundingBox();
    assert.ok(
      box.x >= 0 && box.x + box.width <= 390,
      `editor inside the screen (${box.x}, ${box.width})`,
    );
    await page.screenshot({ path: `${out}/editor-${theme}-390.png` });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("scripts UI ok");
} catch (err) {
  console.error(errors.join("\n").slice(0, 3000));
  await lastPage?.screenshot({ path: `${out}/fehler.png` }).catch(() => {});
  throw err;
} finally {
  await browser.close();
}
