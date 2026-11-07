// PLAN 64: Settings › Appearance – the global sliders apply live in every
// window and are saved; Settings › Appearance › Dock changes the dock of the
// desktop (order, size, position) and the dock's right click keeps working;
// the login page in the style of the desktop with its 2FA step, and logout
// lands on it. Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5261 node scripts/test-ui-appearance.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5261";
const out = process.env.UI_OUTPUT || "out/validation/appearance";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const shot = (page, name) => page.screenshot({ path: `${out}/${name}.png` });
const rootVar = (target, name) =>
  target.evaluate(
    (n) =>
      getComputedStyle(document.documentElement).getPropertyValue(n).trim(),
    name,
  );

/** The desktop (no classic frame) with a few programs; PUTs are kept like titand does. */
async function desktop(path = "/", width = 1600, height = 1000) {
  const made = await create("owner", "graphit", width);
  await made.page.setViewportSize({ width, height });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  made.desktopPuts = [];
  made.data.prefs.desktop = {
    version: 1,
    wallpaper: { source: "builtin", id: "aurora", live: false },
  };
  await made.ctx.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    const method = route.request().method();
    if (p === "/preferences/desktop" && method === "PUT") {
      const body = route.request().postDataJSON();
      made.desktopPuts.push(body);
      made.data.prefs.desktop = body;
      return route.fulfill({ status: 204 });
    }
    if (p === "/apps/mine") return route.fulfill({ json: { apps: [] } });
    return route.fallback();
  });
  await made.page.goto(base + path);
  await made.page.waitForSelector(".desktop", { timeout: 15000 });
  await made.page.waitForTimeout(600);
  return made;
}

// 1. The sliders apply at once on the desktop and in every window, and are saved.
{
  const made = await desktop("/settings/appearance");
  const { page, data } = made;
  const frame = page.frameLocator("section.win iframe").first();
  await frame.locator("#look-glass").waitFor({ timeout: 15000 });
  await shot(page, "01-darstellung");
  const before = await rootVar(page, "--desk-surface");
  await frame.locator("#look-glass").fill("90");
  await frame.locator("#look-blur").fill("30");
  await frame.locator("#look-corners").fill("28");
  await frame.locator("#look-saturation").fill("140");
  await frame.locator("#look-font").fill("110");
  // The desktop around the window follows without a reload.
  await page.waitForFunction(
    () =>
      getComputedStyle(document.documentElement)
        .getPropertyValue("--radius-scale")
        .trim() === "2.00",
  );
  assert.notEqual(
    await rootVar(page, "--desk-surface"),
    before,
    "desktop glass follows",
  );
  assert.equal(await rootVar(page, "--desk-blur"), "36px");
  assert.equal(await rootVar(page, "--wallpaper-filter"), "saturate(1.40)");
  assert.equal(await rootVar(page, "--font-scale"), "1.10");
  const panel = await page
    .locator(".glass-panel")
    .first()
    .evaluate((el) => getComputedStyle(el).backdropFilter);
  assert.match(
    panel,
    /blur\(36px\)/,
    "the desktop's glass panels use the new blur",
  );
  // Saved to the account a moment later.
  await page.waitForTimeout(1200);
  assert.equal(data.prefs.appearance.glass, 90);
  assert.equal(data.prefs.appearance.blur, 30);
  assert.equal(data.prefs.appearance.rTile, 28);
  assert.equal(data.prefs.appearance.rCtl, 20);
  assert.equal(data.prefs.appearance.saturation, 140);
  assert.equal(data.prefs.appearance.style, "glass");
  await shot(page, "02-schieberegler-live");
  // Theme: light, then a dark variant.
  await frame.getByRole("radio", { name: "Hell" }).click();
  await page.waitForFunction(
    () => document.documentElement.getAttribute("data-mode") === "light",
  );
  await shot(page, "03-hell");
  await frame.getByRole("radio", { name: "Dunkel" }).click();
  await frame.getByRole("radio", { name: "Mitternacht" }).click();
  await page.waitForFunction(
    () => document.documentElement.getAttribute("data-theme") === "mitternacht",
  );
  await page.waitForTimeout(1000);
  assert.equal(data.prefs.appearance.theme, "mitternacht");
  // Persisted: a new load shows the same.
  await page.reload();
  await page.waitForSelector(".desktop");
  await page.waitForTimeout(800);
  assert.equal(await rootVar(page, "--radius-scale"), "2.00");
  assert.equal(
    await page.evaluate(() => document.documentElement.dataset.theme),
    "mitternacht",
  );
  // Removed options of the classic frame are gone.
  const frame2 = page.frameLocator("section.win iframe").first();
  await frame2.locator("#look-glass").waitFor({ timeout: 15000 });
  for (const gone of ["Oben", "Seitlich", "Hintergrund des Dashboards"])
    assert.equal(
      await frame2.getByText(gone, { exact: true }).count(),
      0,
      `no "${gone}" any more`,
    );
  await made.ctx.close();
}

// 2. Dock settings: preview, order, size, position, remove, add; the dock follows.
{
  const made = await desktop("/settings/appearance/dock");
  const { page } = made;
  const frame = page.frameLocator("section.win iframe").first();
  await frame
    .locator('[data-testid="dock-pins"] li')
    .first()
    .waitFor({ timeout: 15000 });
  const order = async () =>
    page
      .locator("nav.dock:not(.preview) .slot[data-pinned]")
      .evaluateAll((els) => els.map((e) => e.dataset.key));
  const listed = async () =>
    frame
      .locator('[data-testid="dock-pins"] li')
      .evaluateAll((els) => els.map((e) => e.dataset.key));
  const start = await order();
  assert.deepEqual(await listed(), start, "the list shows the dock's programs");
  assert.ok(start.length >= 4, "standard set in the dock");
  await shot(page, "04-dock-einstellungen");
  // Down with the arrow button.
  await frame
    .locator('[data-testid="dock-pins"] li')
    .first()
    .getByRole("button")
    .nth(1)
    .click();
  await page.waitForFunction(
    (first) =>
      document.querySelector("nav.dock:not(.preview) .slot[data-pinned]")
        ?.dataset.key !== first,
    start[0],
  );
  const moved = await order();
  assert.equal(moved[1], start[0], "first program moved one place back");
  // Dragging in the list: the last one to the front.
  const items = frame.locator('[data-testid="dock-pins"] li');
  await items.last().dragTo(items.first());
  await page.waitForTimeout(300);
  const dragged = await order();
  assert.equal(dragged[0], moved.at(-1), "dragged to the front");
  // Remove one, add one from the list.
  const removed = dragged[1];
  await items.nth(1).getByRole("button").nth(2).click();
  await page.waitForTimeout(200);
  assert.ok(!(await order()).includes(removed), "removed from the dock");
  const option = await frame
    .locator("#dock-add option")
    .evaluateAll((o) => o.map((e) => e.value).filter(Boolean));
  assert.ok(option.includes(removed), "removed program can be added again");
  await frame.locator("#dock-add").selectOption(removed);
  await frame.getByRole("button", { name: "Hinzufügen" }).click();
  await page.waitForTimeout(200);
  assert.equal((await order()).at(-1), removed, "added at the end");
  // Size and position.
  await frame.locator("#dock-size").fill("70");
  await page.waitForFunction(
    () =>
      document
        .querySelector("nav.dock:not(.preview)")
        ?.style.getPropertyValue("--size") === "70px",
  );
  await frame.getByRole("radio", { name: "Links" }).click();
  await page.locator("nav.dock.left:not(.preview)").waitFor();
  assert.ok(
    await frame.locator('[data-testid="dock-preview"] nav.dock.left').count(),
    "preview shows the dock on the left",
  );
  await shot(page, "05-dock-links");
  // Saved once on the NAS, by the desktop.
  await page.waitForTimeout(1800);
  const saved = made.desktopPuts.at(-1)?.dock;
  assert.ok(saved, "desktop saved the dock");
  assert.equal(saved.size, 70);
  assert.equal(saved.position, "left");
  assert.equal(saved.customPinned, true);
  assert.deepEqual(saved.pinned, await order());
  // The dock's own right click still works and the settings window follows it.
  await frame.getByRole("radio", { name: "Unten" }).click();
  await page.locator("nav.dock.bottom:not(.preview)").waitFor();
  // A right click on the dock's free plate (not on an icon).
  await page.locator("nav.dock:not(.preview)").evaluate((el) => {
    const r = el.getBoundingClientRect();
    el.dispatchEvent(
      new MouseEvent("contextmenu", {
        bubbles: true,
        cancelable: true,
        clientX: r.left + 3,
        clientY: r.top + r.height / 2,
      }),
    );
  });
  await page.getByRole("menuitem", { name: /Dock bearbeiten/ }).click();
  await page.locator(".dock-edit").waitFor();
  await page.locator('.dock-edit input[type="range"]').first().fill("50");
  for (
    let i = 0;
    i < 30 && (await frame.locator("#dock-size").inputValue()) !== "50";
    i++
  )
    await page.waitForTimeout(100);
  assert.equal(
    await frame.locator("#dock-size").inputValue(),
    "50",
    "settings follow the dock",
  );
  await shot(page, "06-dock-rechtsklick");
  await made.ctx.close();
}

// 3. Login page: desktop style, 2FA step; logout lands on it again.
for (const [width, height, label] of [
  [1440, 900, "desktop"],
  [390, 844, "handy"],
]) {
  const made = await create(
    "owner",
    "graphit",
    width,
    "de",
    width < 600
      ? {
          isMobile: true,
          hasTouch: true,
          deviceScaleFactor: 3,
          viewport: { width, height },
        }
      : {},
  );
  const { ctx, page, data } = made;
  await page.setViewportSize({ width, height });
  await ctx.addInitScript(() => {
    localStorage.removeItem("titanos.layout");
    localStorage.setItem("titanos.login.wallpaper", "dunes");
  });
  let loggedIn = false;
  const logins = [];
  data.prefs.desktop = {
    version: 1,
    wallpaper: { source: "builtin", id: "peaks", live: false },
  };
  await ctx.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    const method = route.request().method();
    if (p === "/session")
      return route.fulfill({
        json: {
          configAvailable: true,
          setupRequired: false,
          user: loggedIn
            ? { name: "alex", role: "owner", twoFactor: true }
            : null,
          hostname: "titan-test",
        },
      });
    if (p === "/login" && method === "POST") {
      const body = route.request().postDataJSON();
      logins.push(body);
      if (!body.code)
        return route.fulfill({
          status: 401,
          json: { error: { code: "second_factor_required", message: "code" } },
        });
      loggedIn = true;
      return route.fulfill({ json: { name: "alex", role: "owner" } });
    }
    if (p === "/logout" && method === "POST") {
      loggedIn = false;
      return route.fulfill({ status: 204 });
    }
    if (p === "/apps/mine") return route.fulfill({ json: { apps: [] } });
    return route.fallback();
  });
  await page.goto(base + "/");
  const login = page.locator('[data-testid="login-page"]');
  await login.waitFor({ timeout: 15000 });
  assert.ok(
    await login.locator(".clock").isVisible(),
    "clock on the login page",
  );
  assert.ok(await login.locator(".card").isVisible(), "glass card");
  const bg = await login
    .locator(".wallpaper")
    .evaluate((el) => getComputedStyle(el).backgroundImage);
  assert.match(bg, /svg/, "the desktop's wallpaper behind the card");
  const card = await login.locator(".card").boundingBox();
  assert.ok(
    card.x >= 0 && card.x + card.width <= width + 0.5,
    "card inside the screen",
  );
  assert.ok(
    (await page.evaluate(() => document.documentElement.scrollWidth)) <= width,
    "no sideways scrolling",
  );
  await shot(page, `07-anmeldung-${label}`);
  await page.getByLabel("Name").fill("alex");
  await page.getByLabel("Passwort").fill("secret-test");
  await page.getByRole("button", { name: "Anmelden" }).click();
  await page.locator('input[autocomplete="one-time-code"]').waitFor();
  await shot(page, `08-anmeldung-2fa-${label}`);
  await page.locator('input[autocomplete="one-time-code"]').fill("123456");
  await page.getByRole("button", { name: "Anmelden" }).click();
  await page.waitForSelector(width < 900 ? ".home" : ".desktop", {
    timeout: 15000,
  });
  assert.equal(logins.length, 2);
  assert.equal(logins[1].code, "123456");
  // The account's wallpaper is remembered for the next login page.
  assert.equal(
    await page.evaluate(() => localStorage.getItem("titanos.login.wallpaper")),
    "peaks",
  );
  // Logout from the account menu.
  if (width < 900) await page.locator(".home .account button.trigger").click();
  else
    await page
      .locator(".topbar button.trigger", { hasText: "alex" })
      .first()
      .click();
  await page.getByRole("menuitem", { name: "Abmelden" }).click();
  await login.waitFor({ timeout: 15000 });
  assert.ok(
    await login.locator(".card").isVisible(),
    "logout lands on the new login page",
  );
  await shot(page, `09-abgemeldet-${label}`);
  await ctx.close();
}

await browser.close();
const relevant = errors.filter(
  (e) => !/Failed to load resource|net::ERR/.test(e),
);
assert.deepEqual(relevant, [], "no script errors");
console.log(
  "PASS appearance sliders live and saved, dock settings, login page, 2FA, logout",
);
