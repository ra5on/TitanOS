// PLAN 71: windows never slide under the dock – when opened, when dragged
// down, when a stored window lies there, when resized at the bottom edge.
// Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5240 node scripts/test-ui-dock-clear.mjs
import assert from "node:assert/strict";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5240";
const errors = [];
const create = fixtureFactory(browser, errors);

async function check(page, what) {
  // Away from the dock: hovering magnifies its icons.
  await page.mouse.move(5, 300);
  await page.waitForTimeout(300);
  const dock = await page.locator(".dock").boundingBox();
  for (const box of await page.locator("section.win").evaluateAll((els) =>
    els.map((el) => {
      const r = el.getBoundingClientRect();
      return { top: r.top, bottom: r.bottom };
    }),
  ))
    assert.ok(
      box.bottom <= dock.y + 1,
      `${what}: window bottom ${box.bottom} under the dock top ${dock.y}`,
    );
}

for (const [width, height] of [
  [1440, 900],
  [1280, 800],
]) {
  const made = await create("owner", "graphit", width);
  const { ctx, page } = made;
  await page.setViewportSize({ width, height });
  await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  await page.goto(base + "/");
  await page.locator(".dock").waitFor({ timeout: 15000 });
  // Open several programs: the offset of later windows must not push them down.
  for (const name of ["Systemsteuerung", "Dateien", "Docker", "Monitor"]) {
    await page
      .locator(".dock")
      .getByRole("button", { name, exact: true })
      .click();
    await page.waitForTimeout(400);
  }
  await check(page, `${width}: opened`);
  // Drag the window in front down by far more than the room below it.
  const bar = page.locator("section.win.focused header.titlebar");
  const b = await bar.boundingBox();
  await page.mouse.move(b.x + b.width / 2, b.y + 10);
  await page.mouse.down();
  await page.mouse.move(b.x + b.width / 2, b.y + 260, { steps: 6 });
  await page.mouse.move(b.x + b.width / 2 + 5, b.y + 300, { steps: 2 });
  await page.mouse.up();
  await page.waitForTimeout(300);
  await check(page, `${width}: dragged down`);
  // A window stored low (other screen, larger dock before) comes back above the dock.
  await page.reload();
  await page.locator(".dock").waitFor();
  await page.waitForTimeout(800);
  await check(page, `${width}: after reload`);
  await ctx.close();
}

// PLAN 75-17: the dock made smaller in another tab (Einstellungen in a tab
// of its own): the maximised window of the desktop follows at once, without
// a reload – its bottom stays just above the dock.
{
  const made = await create("owner", "graphit", 1440);
  const { ctx, page } = made;
  await page.setViewportSize({ width: 1440, height: 900 });
  await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  await page.goto(base + "/");
  await page.locator(".dock").waitFor({ timeout: 15000 });
  await page
    .locator(".dock")
    .getByRole("button", { name: "Dateien", exact: true })
    .click();
  const win = page.locator("section.win.focused");
  await win.waitFor();
  await win.locator("header.titlebar").dblclick();
  await page.waitForTimeout(400);
  const gap = async () => {
    await page.mouse.move(5, 300);
    await page.waitForTimeout(400);
    const dock = await page.locator(".dock").boundingBox();
    const box = await win.boundingBox();
    return { gap: dock.y - (box.y + box.height), dockTop: dock.y };
  };
  const before = await gap();
  // The other tab saves the desktop with a smaller dock.
  const other = await ctx.newPage();
  await other.goto(base + "/");
  await other.locator(".dock").waitFor({ timeout: 15000 });
  await other.evaluate(
    ({ user }) => {
      const channel = new BroadcastChannel("titanos-desktop-prefs");
      channel.postMessage({ user, prefs: { dock: { size: 36 } } });
    },
    { user: "alex" },
  );
  await page.waitForTimeout(800);
  const after = await gap();
  assert.ok(
    after.dockTop > before.dockTop + 8,
    `the dock got smaller (${before.dockTop} → ${after.dockTop})`,
  );
  assert.ok(
    Math.abs(after.gap - before.gap) <= 2,
    `the maximised window follows the dock (gap ${before.gap} → ${after.gap})`,
  );
  await ctx.close();
}

await browser.close();
assert.deepEqual(errors, [], "no script errors");
console.log("dock clear UI ok");
