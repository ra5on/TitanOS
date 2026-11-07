// The simple schedule (ScheduleField) in the script editor, against
// isolated fixtures: every N minutes, every hour, daily, weekdays as
// toggles, monthly with the last day; the sentence and the next three runs
// in the zone of the NAS; cron only under "Advanced", open for an
// expression the simple choice cannot show. Screenshots 1440 and 390 px.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs UI_URL=http://127.0.0.1:5199 node scripts/test-ui-schedule.mjs
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
const out = process.env.UI_OUTPUT || "out/validation/zeitplan";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

let lastPage;
async function open(width = 1440, prepare) {
  const made = await context("owner", "graphit", width);
  lastPage = made.page;
  made.data.scripts.timezone = "Europe/Berlin";
  prepare?.(made.data);
  if (width < 761) await made.page.setViewportSize({ width, height: 844 });
  await made.page.goto(base + "/tools/scripts");
  await made.page.locator("main").waitFor();
  await made.page.waitForLoadState("networkidle");
  return made;
}
const row = (page, name) => page.locator("li.script").filter({ hasText: name });
const settle = (dialog) =>
  dialog.evaluate((d) =>
    Promise.all(d.getAnimations({ subtree: true }).map((a) => a.finished)),
  );
const lastPut = (data) =>
  JSON.parse(
    data.scriptCalls.findLast(
      ([m, p]) => m === "PUT" && p.startsWith("/scripts/a1"),
    )[2],
  );

try {
  {
    const { ctx, page, data } = await open();
    await row(page, "Fotos sichern")
      .getByRole("button", { name: "Bearbeiten" })
      .click();
    const edit = page.getByRole("dialog", { name: "Skript bearbeiten" });
    const field = edit.locator(".schedule-field");
    await field.waitFor();
    const kind = field.getByRole("combobox", { name: "Wann" });
    // The stored daily schedule shows in the simple choice, Advanced closed.
    assert.equal(await kind.inputValue(), "daily");
    assert.equal(await field.locator("details").evaluate((d) => d.open), false);
    await field.getByText("Läuft täglich um 03:00 Uhr.").waitFor();
    assert.ok(
      await field
        .getByText("Die nächsten Ausführungen (Zeitzone Europe/Berlin):")
        .isVisible(),
    );
    assert.equal(await field.locator(".preview li").count(), 3);

    // Weekdays: Monday is on by default, Thursday added.
    await kind.selectOption("weekdays");
    await field.getByRole("button", { name: "Donnerstag" }).click();
    assert.equal(
      await field
        .getByRole("button", { name: "Donnerstag" })
        .getAttribute("aria-pressed"),
      "true",
    );
    await field
      .getByText("Läuft montags und donnerstags um 03:00 Uhr.")
      .waitFor();
    await settle(edit);
    await page.screenshot({ path: `${out}/wochentage-graphit-1440.png` });
    await field.getByRole("button", { name: "Montag", exact: true }).click();
    await field.getByRole("button", { name: "Donnerstag" }).click();
    await field.getByText("Bitte mindestens einen Wochentag wählen.").waitFor();
    assert.ok(
      await edit.getByRole("button", { name: "Speichern" }).isDisabled(),
      "no weekday, no save",
    );
    await field.getByRole("button", { name: "Freitag" }).click();
    await field.getByRole("textbox", { name: "Uhrzeit" }).fill("22:30");
    await field.getByText("Läuft freitags um 22:30 Uhr.").waitFor();
    await edit.getByRole("button", { name: "Speichern" }).click();
    await edit.waitFor({ state: "detached" });
    assert.deepEqual(lastPut(data).schedule, {
      every: "custom",
      cron: "30 22 * * 5",
    });
    await row(page, "Fotos sichern")
      .getByText("Freitags um 22:30 Uhr")
      .waitFor();

    // Every 10 minutes, every hour at :45, monthly on the last day.
    await row(page, "Fotos sichern")
      .getByRole("button", { name: "Bearbeiten" })
      .click();
    await field.waitFor();
    assert.equal(await kind.inputValue(), "weekdays");
    await kind.selectOption("minutes");
    await field.getByRole("combobox", { name: "Abstand" }).selectOption("10");
    await field.getByText("Läuft alle 10 Minuten.").waitFor();
    await kind.selectOption("hourly");
    await field.getByRole("spinbutton", { name: "Zur Minute" }).fill("45");
    await field.getByText("Läuft jede Stunde zur Minute 45.").waitFor();
    await kind.selectOption("monthly");
    await field.getByRole("combobox", { name: "Am Tag" }).selectOption("last");
    await field
      .getByText("Läuft am letzten Tag jedes Monats um 03:00 Uhr.")
      .waitFor();
    // The cron of it, under Advanced.
    await field.getByText("Erweitert").click();
    assert.equal(await field.locator("input.mono").inputValue(), "0 3 L * *");
    await settle(edit);
    await page.screenshot({
      path: `${out}/monatlich-erweitert-graphit-1440.png`,
    });
    await edit.getByRole("button", { name: "Speichern" }).click();
    await edit.waitFor({ state: "detached" });
    assert.deepEqual(lastPut(data).schedule, {
      every: "custom",
      cron: "0 3 L * *",
    });
    await ctx.close();
  }
  {
    // An expression the simple choice cannot show: Advanced stays open,
    // the choice says so, the next runs still show.
    const { ctx, page } = await open(1440, (data) => {
      data.scripts.scripts[1].schedule = {
        every: "custom",
        cron: "0 */6 * * *",
      };
    });
    await row(page, "Docker aufräumen")
      .getByText("Cron: 0 */6 * * *")
      .waitFor();
    await row(page, "Docker aufräumen")
      .getByRole("button", { name: "Bearbeiten" })
      .click();
    const field = page
      .getByRole("dialog", { name: "Skript bearbeiten" })
      .locator(".schedule-field");
    await field.waitFor();
    assert.equal(await field.locator("details").evaluate((d) => d.open), true);
    assert.equal(
      await field.getByRole("combobox", { name: "Wann" }).inputValue(),
      "",
    );
    await field
      .getByText("Läuft nach dem eigenen Cron-Ausdruck 0 */6 * * *.")
      .waitFor();
    assert.equal(await field.locator(".preview li").count(), 3);
    // Typing a simple one moves it into the choice; a wrong one is said.
    await field.locator("input.mono").fill("0 5 * * 1-5");
    assert.equal(
      await field.getByRole("combobox", { name: "Wann" }).inputValue(),
      "weekdays",
    );
    await field
      .getByText("Läuft werktags (Montag bis Freitag) um 05:00 Uhr.")
      .waitFor();
    await field.locator("input.mono").fill("0 25 * * *");
    await field
      .getByText("Der Cron-Ausdruck ist ungültig", { exact: false })
      .waitFor();
    assert.ok(
      await page
        .getByRole("dialog", { name: "Skript bearbeiten" })
        .getByRole("button", { name: "Speichern" })
        .isDisabled(),
      "an invalid expression is not saved",
    );
    await settle(page.getByRole("dialog", { name: "Skript bearbeiten" }));
    await page.screenshot({ path: `${out}/eigener-ausdruck-graphit-1440.png` });
    await ctx.close();
  }
  {
    // A new script starts daily at 03:00.
    const { ctx, page } = await open(390);
    await page.getByRole("button", { name: "Neues Skript" }).click();
    const edit = page.getByRole("dialog", { name: "Neues Skript" });
    await edit.locator("select").first().selectOption("schedule");
    const field = edit.locator(".schedule-field");
    await field.getByText("Läuft täglich um 03:00 Uhr.").waitFor();
    await field
      .getByRole("combobox", { name: "Wann" })
      .selectOption("weekdays");
    await field.getByRole("button", { name: "Mittwoch" }).click();
    const box = await field.boundingBox();
    assert.ok(
      box.x >= 0 && box.x + box.width <= 390,
      `field inside the screen (${box.x}, ${box.width})`,
    );
    await field.scrollIntoViewIfNeeded();
    await settle(edit);
    await page.screenshot({ path: `${out}/neu-graphit-390.png` });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("schedule UI ok");
} catch (err) {
  console.error(errors.join("\n").slice(0, 3000));
  await lastPage?.screenshot({ path: `${out}/fehler.png` }).catch(() => {});
  throw err;
} finally {
  await browser.close();
}
