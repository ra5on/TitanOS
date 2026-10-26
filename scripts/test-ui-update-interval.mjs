// Settings › Updates: how often TitanOS looks for updates. Presets, custom
// spacing, manual only and its effect on installing by itself. All NAS
// endpoints are isolated fixtures; no request reaches a NAS.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs UI_URL=http://127.0.0.1:5189 node scripts/test-ui-update-interval.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5189";
const out = process.env.UI_OUTPUT || "out/validation/update-interval";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

try {
  {
    const { ctx, page, data } = await context("owner", "graphit", 1440);
    await page.goto(`${base}/settings/updates`);
    const often = page.getByLabel("Häufigkeit der Suche");
    const auto = page.getByLabel("Updates von selbst installieren");
    const save = page.getByRole("button", { name: "Speichern" });
    // A file without the field reads as once a day.
    await often.waitFor();
    assert.equal(await often.inputValue(), "0");
    assert.ok(await auto.isChecked());
    await page.getByText(/^Nächste Prüfung: /).waitFor();
    assert.ok(await save.isDisabled());

    // Preset: every six hours, installing by itself stays on.
    await often.selectOption({ label: "Alle 6 Stunden" });
    // The next check of the saved interval is hidden while another is chosen.
    assert.equal(await page.getByText(/^Nächste Prüfung: /).count(), 0);
    await save.click();
    await page.getByText("Gespeichert").waitFor();
    assert.deepEqual(data.updatePuts.at(-1), {
      channel: "stable",
      autoInstall: true,
      installTime: "03:30",
      checkEvery: 360,
    });
    await page.getByText(/^Nächste Prüfung: /).waitFor();

    // Manual only turns installing by itself off and disables it.
    await often.selectOption({ label: "Nur manuell" });
    assert.ok(!(await auto.isChecked()));
    assert.ok(await auto.isDisabled());
    assert.ok(
      await page
        .getByText(
          "Updates von selbst installieren geht nur mit automatischer Suche",
          { exact: false },
        )
        .isVisible(),
    );
    assert.ok(
      await page
        .getByText(
          "TitanOS sucht nicht von selbst, auch nicht nach der Anmeldung.",
          { exact: false },
        )
        .isVisible(),
    );
    await page.screenshot({
      path: `${out}/manual-graphit-1440.png`,
      fullPage: true,
    });
    await save.click();
    await page.getByText("Gespeichert").waitFor();
    assert.equal(data.updatePuts.at(-1).checkEvery, -1);
    assert.equal(data.updatePuts.at(-1).autoInstall, false);
    assert.equal(await page.getByText(/^Nächste Prüfung/).count(), 0);
    // The button still looks.
    await page.getByRole("button", { name: "Nach Updates suchen" }).click();
    await page
      .getByText("TitanOS ist aktuell.", { exact: false })
      .or(page.getByText("Zuletzt gesucht", { exact: false }))
      .first()
      .waitFor();
    assert.equal(data.updateChecks, 1);

    // Custom: bounded to an hour to 30 days.
    await often.selectOption({ label: "Benutzerdefiniert" });
    const amount = page.getByLabel("Alle", { exact: true });
    const unit = page.getByLabel("Einheit");
    await amount.fill("0");
    assert.ok(
      await page
        .getByText("zwischen 1 Stunde und 30 Tagen", { exact: false })
        .isVisible(),
    );
    assert.ok(await save.isDisabled());
    await unit.selectOption({ label: "Tage" });
    await amount.fill("45");
    assert.ok(await save.isDisabled());
    await amount.fill("3");
    assert.equal(
      await page
        .getByText("zwischen 1 Stunde und 30 Tagen", { exact: false })
        .count(),
      0,
    );
    // Without automatic installation turned back on, it stays off.
    assert.ok(await auto.isEnabled());
    assert.ok(!(await auto.isChecked()));
    await page.screenshot({
      path: `${out}/custom-graphit-1440.png`,
      fullPage: true,
    });
    await save.click();
    await page.getByText("Gespeichert").waitFor();
    assert.equal(data.updatePuts.at(-1).checkEvery, 3 * 24 * 60);

    // Read back after a reload: custom, 3 days.
    await page.reload();
    await often.waitFor();
    assert.equal(await often.inputValue(), "custom");
    assert.equal(await amount.inputValue(), "3");
    assert.equal(await unit.inputValue(), "days");

    // One day as a custom spacing is the default and shown as such.
    await amount.fill("1");
    await save.click();
    await page.getByText("Gespeichert").waitFor();
    assert.equal(data.updatePuts.at(-1).checkEvery, 0);
    await page.waitForFunction(
      () => document.querySelector("form select")?.value === "0",
    );
    await ctx.close();
  }
  {
    // English, light theme, phone width.
    const { ctx, page, data } = await context("owner", "hell", 390, "en");
    await page.goto(`${base}/settings/updates`);
    const often = page.getByLabel("How often to look");
    await often.waitFor();
    await often.selectOption({ label: "Once a week" });
    await page.getByRole("button", { name: "Save" }).click();
    await page.getByText("Saved").waitFor();
    assert.equal(data.updatePuts.at(-1).checkEvery, 7 * 24 * 60);
    await often.selectOption({ label: "Custom" });
    // Custom starts from the saved week.
    assert.equal(
      await page.getByLabel("Every", { exact: true }).inputValue(),
      "7",
    );
    assert.equal(await page.getByLabel("Unit").inputValue(), "days");
    assert.ok(await page.getByLabel("Unit").isVisible());
    const overflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    );
    assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
    await page.screenshot({
      path: `${out}/custom-hell-390.png`,
      fullPage: true,
    });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log(`ok – screenshots in ${out}`);
} finally {
  await browser.close();
}
