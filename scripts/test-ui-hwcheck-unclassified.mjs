// Settings › Hardware › devices without a driver against isolated fixtures:
// an unclassified Intel chipset function (8086:7f2f on titan) is folded away
// with its own name and a link to the hardware issue form.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-hwcheck-unclassified.mjs
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
const out = process.env.UI_OUTPUT || "out/validation/hwcheck-unclassified";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

const report = {
  unbound: [
    {
      bus: "pci",
      address: "0000:80:14.5",
      id: "8086:7f2f",
      class: "000000",
      name: "Intel Corporation 800 Series Chipset Family, unnamed function",
      hint: "unclassified",
    },
  ],
  firmwareMissing: [],
};

try {
  for (const [name, width, theme] of [
    ["graphit-1440", 1440, "graphit"],
    ["graphit-390", 390, "graphit"],
    ["hell-390", 390, "hell"],
  ]) {
    const { ctx, page } = await context("owner", theme, width);
    await page.route("**/api/v1/hardware/report", (route) =>
      route.fulfill({ json: report }),
    );
    await page.goto(base + "/settings/hardware");
    // PLAN 78: "Geräte ohne Treiber" is a row; it says "In Ordnung" and opens
    // to the list.
    const panel = page.locator("#unbound");
    await panel.getByText("In Ordnung", { exact: true }).waitFor();
    await panel
      .getByRole("button", { name: "Geräte ohne Treiber: Aufklappen" })
      .click();
    // Needs no attention, so it is folded away.
    await panel.locator("summary").waitFor();
    assert.equal(
      await panel
        .getByRole("link", { name: "Gerät melden (GitHub, Hardware-Bericht)" })
        .isVisible(),
      false,
    );
    await panel.locator("summary").click();
    await panel
      .getByText(
        "Intel Corporation 800 Series Chipset Family, unnamed function",
      )
      .waitFor();
    const link = panel.getByRole("link", {
      name: "Gerät melden (GitHub, Hardware-Bericht)",
    });
    assert.equal(
      await link.getAttribute("href"),
      "https://github.com/ra5on/TitanOS/issues/new?template=hardware.yml",
    );
    assert.equal(await link.getAttribute("target"), "_blank");
    await panel.screenshot({
      path: `${out}/${name}.png`,
      animations: "disabled",
    });
    if (width === 390) {
      const overflow = await page.evaluate(
        () =>
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      );
      assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
    }
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log(`ok – screenshots in ${out}`);
} finally {
  await browser.close();
}
