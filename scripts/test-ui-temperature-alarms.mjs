// Settings › Temperatures › Heat notifications against isolated fixtures:
// limits come from titand with their source (the chip's own value or the
// default of its kind), muting a sensor sends the whole list.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-temperature-alarms.mjs
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
const out = process.env.UI_OUTPUT || "out/validation/temperature-alarms";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

const id = (n) => String(n).repeat(32);
const temperatures = [
  {
    id: id(1),
    chip: "coretemp",
    label: "Package id 0",
    device: "devices/platform/coretemp.0",
    celsius: 48.0,
    criticalC: 100,
  },
  {
    id: id(2),
    chip: "acpitz",
    label: "temp1",
    device: "devices/virtual/thermal/thermal_zone0",
    celsius: 27.8,
    criticalC: 105,
  },
  {
    id: id(3),
    chip: "nct6798",
    label: "temp8",
    device: "devices/platform/nct6775.656",
    celsius: 126.0,
    criticalC: null,
  },
  {
    id: id(4),
    chip: "k10temp",
    label: "Tctl",
    device: "devices/pci0000:00/0000:00:18.3",
    celsius: 52.5,
    criticalC: null,
  },
];
const limits = { [id(1)]: 95, [id(2)]: 100, [id(3)]: null, [id(4)]: 95 };
const own = { [id(1)]: true, [id(2)]: true, [id(4)]: false };

try {
  for (const [name, width, theme] of [
    ["graphit-1440", 1440, "graphit"],
    ["graphit-390", 390, "graphit"],
    ["hell-390", 390, "hell"],
  ]) {
    const { ctx, page, data } = await context("owner", theme, width);
    data.system = { ...data.system, temperatures };
    let muted = [id(2)];
    let heatShutdown = false;
    const puts = [];
    const answer = () => ({
      muted,
      heatShutdown,
      sensors: temperatures.map((t) => ({
        id: t.id,
        limit: limits[t.id],
        own: own[t.id] ?? false,
        muted: muted.includes(t.id),
        // PLAN 75-9: only a reliable chip with its own critical value.
        shutdown: t.chip === "coretemp",
      })),
    });
    await page.route("**/api/v1/notifications/temperatures", (route) => {
      if (route.request().method() === "PUT") {
        puts.push(route.request().postDataJSON());
        muted = puts.at(-1).muted;
        if (typeof puts.at(-1).heatShutdown === "boolean")
          heatShutdown = puts.at(-1).heatShutdown;
      }
      return route.fulfill({ json: answer() });
    });
    await page.route("**/api/v1/system/fans", (route) =>
      route.fulfill({ json: { fans: [], readOnly: false } }),
    );
    await page.goto(base + "/settings/temperatures");
    const panel = page.getByRole("region", { name: "Meldungen bei Hitze" });
    // PLAN 78-1: the sensors are the parts of one row; closed, it says how
    // many of them notify.
    await panel.getByText("2 von 4 melden").waitFor();
    await panel
      .getByRole("button", { name: "Sensoren, die melden: Aufklappen" })
      .click();
    // The effective limit, and where it comes from.
    await panel
      .getByText("Meldet ab 95 °C (5 °C unter dem Grenzwert des Chips)")
      .waitFor();
    assert.ok(
      await panel
        .getByText("Meldet ab 95 °C (Vorgabe für k10temp)")
        .isVisible(),
    );
    assert.ok(
      await panel
        .getByText("Platten 60 °C, NVMe 85 °C, sonst 95 °C", { exact: false })
        .isVisible(),
    );
    assert.ok(await panel.getByText("Stummgeschaltet").isVisible());
    assert.ok(
      await panel
        .getByText("Meldet nie: Chip ohne eigenen Grenzwert")
        .isVisible(),
    );
    const cpu = panel.getByLabel("coretemp Package id 0 melden");
    const board = panel.getByLabel("nct6798 temp8 melden");
    assert.ok(await cpu.isChecked());
    assert.ok(await board.isDisabled());
    assert.equal(await board.isChecked(), false);
    await page.screenshot({
      path: `${out}/${name}.png`,
      fullPage: true,
      animations: "disabled",
    });
    // PLAN 75-9: the heat shutdown, off by default, names its sensors.
    const heat = panel.getByRole("switch", {
      name: "Bei Überhitzung herunterfahren",
    });
    assert.equal(await heat.isChecked(), false);
    assert.ok(
      await panel
        .getByText("fährt das NAS geordnet herunter", { exact: false })
        .isVisible(),
    );
    assert.ok(
      await panel
        .getByText(": coretemp Package id 0. Ab Werk aus.", { exact: false })
        .isVisible(),
    );
    if (width === 1440) {
      await heat.check();
      await panel.getByText("Meldungen gespeichert.").waitFor();
      assert.deepEqual(puts.at(-1), { muted: [id(2)], heatShutdown: true });
      assert.ok(await heat.isChecked());
      await panel.screenshot({
        path: `${out}/heat-on.png`,
        animations: "disabled",
      });
      await heat.uncheck();
      await page.waitForTimeout(300);
      assert.deepEqual(puts.at(-1), { muted: [id(2)], heatShutdown: false });
      puts.length = 0;
    }
    if (width === 1440) {
      await cpu.uncheck();
      await panel.getByText("Meldungen gespeichert.").waitFor();
      assert.deepEqual(puts.at(-1), { muted: [id(2), id(1)] });
      await panel.getByLabel("acpitz temp1 melden").check();
      await panel
        .getByText("Meldet ab 100 °C (5 °C unter dem Grenzwert des Chips)")
        .waitFor();
      assert.deepEqual(puts.at(-1), { muted: [id(1)] });
    } else {
      const overflow = await page.evaluate(
        () =>
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      );
      assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
    }
    await ctx.close();
  }
  // A plain user sees no heat notifications.
  const { ctx, page, data } = await context("user");
  data.system = { ...data.system, temperatures };
  await page.goto(base + "/settings/temperatures");
  // PLAN 65: the dashboard tile choice is gone; wait for the sensor list.
  await page.getByText("coretemp · devices/platform/coretemp.0").waitFor();
  assert.equal(await page.getByText("Meldungen bei Hitze").count(), 0);
  await ctx.close();
  assert.deepEqual(errors, []);
  console.log(`ok – screenshots in ${out}`);
} finally {
  await browser.close();
}
