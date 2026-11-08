// PLAN 67: the account picker of the login page and the ways to it.
// 1. Circles from the NAS's list (GET /login/accounts) and the accounts of
//    this browser; picking fills the name and focuses the password; the
//    keyboard reaches every account, "⋯" forgets a remembered one (also
//    right-click); "Anderes Konto" shows the empty form.
// 2. The list style; an older NAS (404) leaves only this browser's accounts.
// 3. A window whose session is gone shows no login inside the window: the
//    desktop shows the one login page (logout elsewhere, expired, 401).
// 4. "Not reachable" uses the new look (wallpaper and glass card).
// Isolated, mocked API only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5281 node scripts/test-ui-login-picker.mjs
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
const out = process.env.UI_OUTPUT || "out/validation/login-picker";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

/** A logged-out NAS with the given answer of /login/accounts (null = 404). */
async function loggedOut({ accounts, remembered = [], width = 1440 }) {
  const made = await create("owner", "graphit", width);
  await made.page.setViewportSize({ width, height: 900 });
  await made.ctx.addInitScript((remembered) => {
    localStorage.removeItem("titanos.layout");
    localStorage.setItem("titanos.login.accounts", JSON.stringify(remembered));
  }, remembered);
  const state = { logins: [], user: null };
  await made.ctx.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    const method = route.request().method();
    if (p === "/session")
      return route.fulfill({
        json: {
          configAvailable: true,
          setupRequired: false,
          user: state.user,
          hostname: "titan-test",
        },
      });
    if (p === "/login/accounts")
      return accounts
        ? route.fulfill({ json: accounts })
        : route.fulfill({
            status: 404,
            json: { error: { code: "not_found", message: "no" } },
          });
    if (p === "/login" && method === "POST") {
      const body = route.request().postDataJSON();
      state.logins.push(body);
      if (body.password !== "right")
        return route.fulfill({
          status: 401,
          json: { error: { code: "invalid_credentials", message: "no" } },
        });
      state.user = { name: body.name, role: "owner", twoFactor: false };
      return route.fulfill({ json: state.user });
    }
    return route.fallback();
  });
  await made.page.goto(base + "/");
  await made.page
    .locator('[data-testid="login-page"]')
    .waitFor({ timeout: 15000 });
  return { ...made, state };
}

const focusedLabel = (page) =>
  page.evaluate(() => {
    const el = document.activeElement;
    if (el?.tagName === "INPUT") return el.getAttribute("type") || "";
    return el?.getAttribute("aria-label") || el?.getAttribute("type") || "";
  });

// 1. Circles: the NAS's accounts and this browser's.
{
  const { ctx, page, state } = await loggedOut({
    accounts: {
      style: "circles",
      accounts: [{ name: "alex", displayName: "Alex Berger" }, { name: "sam" }],
    },
    remembered: [{ name: "kim" }],
  });
  const picker = page.locator(".accounts.circles");
  await picker.waitFor();
  const names = await picker.locator(".account-name").allInnerTexts();
  assert.deepEqual(
    names,
    ["kim", "Alex Berger", "sam"],
    "this browser's account first, then the NAS's",
  );
  // PLAN 74: the first account is chosen at once, its name once below, the password focused.
  assert.match(await page.locator(".chosen").innerText(), /kim/);
  assert.equal(await focusedLabel(page), "password", "password focused");
  assert.equal(
    await page.locator(".accounts li.on .letter").innerText(),
    "K",
    "the first letter on the picture",
  );
  assert.ok(
    (await page.locator(".accounts li.on .avatar").getAttribute("style")).includes("/backgrounds/"),
    "a part of a built-in photo until the account has its own picture",
  );
  assert.equal(await page.locator("section.card").evaluate((e) => getComputedStyle(e).backgroundColor), "rgba(0, 0, 0, 0)", "no card");
  await page.screenshot({ path: `${out}/circles.png` });
  // Keyboard: Tab reaches the accounts; Enter picks one.
  await page.keyboard.press("Shift+Tab");
  let guard = 0;
  while (!(await focusedLabel(page)).includes("Anmelden als sam") && guard++ < 10)
    await page.keyboard.press("Shift+Tab");
  assert.match(await focusedLabel(page), /Anmelden als sam/);
  await page.keyboard.press("Enter");
  assert.equal(await focusedLabel(page), "password", "password focused");
  assert.match(await page.locator(".chosen").innerText(), /sam/);
  await picker.getByRole("button", { name: "Anmelden als kim" }).click();
  // A wrong password: the usual message, the account stays chosen.
  await page.keyboard.type("wrong");
  await page.keyboard.press("Enter");
  await page.getByRole("alert").waitFor();
  assert.equal(state.logins.at(-1).name, "kim");
  // "Anderes Konto" shows the empty name field.
  await page.getByRole("button", { name: "Anderes Konto" }).click();
  assert.equal(await page.getByLabel("Name").inputValue(), "");
  assert.equal(
    await page.evaluate(() =>
      document.activeElement?.getAttribute("autocomplete"),
    ),
    "username",
    "the name field is focused",
  );
  await page.getByRole("button", { name: "Zurück zur Kontenauswahl" }).click();
  // Forget the remembered account with its "⋯" button (keyboard reachable).
  const more = page.getByRole("button", { name: "Optionen für kim" });
  assert.equal(
    await page.getByRole("button", { name: /Optionen für (Alex|sam)/ }).count(),
    0,
    "only remembered accounts can be forgotten",
  );
  await more.focus();
  await page.keyboard.press("Enter");
  await page.getByRole("menuitem", { name: /kim.*vergessen/ }).click();
  await page.waitForTimeout(200);
  assert.deepEqual(
    await page.evaluate(() =>
      JSON.parse(localStorage.getItem("titanos.login.accounts")),
    ),
    [],
  );
  assert.equal(
    await picker.locator(".account-name", { hasText: "kim" }).count(),
    0,
  );
  // Pick from the NAS's list and log in: the account is remembered.
  await picker.getByRole("button", { name: "Anmelden als sam" }).click();
  await page.keyboard.type("right");
  await page.keyboard.press("Enter");
  await page.waitForSelector(".desktop", { timeout: 15000 });
  assert.deepEqual(
    await page.evaluate(() =>
      JSON.parse(localStorage.getItem("titanos.login.accounts")),
    ),
    [{ name: "sam" }],
  );
  await ctx.close();
}

// 2. List style; right-click forgets; an older NAS (404) keeps the browser's list.
{
  const { ctx, page } = await loggedOut({
    accounts: { style: "list", accounts: [{ name: "alex" }] },
    remembered: [{ name: "kim" }],
  });
  await page.locator(".accounts.list").waitFor();
  await page.screenshot({ path: `${out}/list.png` });
  await page
    .locator(".accounts li", { hasText: "kim" })
    .click({ button: "right" });
  await page.getByRole("menuitem", { name: /kim.*vergessen/ }).waitFor();
  await page.keyboard.press("Escape");
  await ctx.close();
}
{
  const { ctx, page } = await loggedOut({
    accounts: null,
    remembered: [{ name: "kim" }],
    width: 390,
  });
  await page.locator(".accounts.circles").waitFor();
  const names = await page.locator(".accounts .account-name").allInnerTexts();
  assert.deepEqual(names, ["kim"]);
  await page.getByRole("button", { name: "Anderes Konto" }).waitFor();
  const over = await page.evaluate(
    () => document.documentElement.scrollWidth - innerWidth,
  );
  assert.ok(over <= 1, `no sideways scrolling on the phone (${over})`);
  await page.screenshot({ path: `${out}/phone.png`, fullPage: true });
  await ctx.close();
}
// No accounts at all: the plain form, the name field focused.
{
  const { ctx, page } = await loggedOut({ accounts: null });
  await page.getByLabel("Name").waitFor();
  assert.equal(await page.locator(".accounts").count(), 0);
  await ctx.close();
}

// 3. A window's page loses its session: no login inside the window.
{
  const made = await create("owner", "graphit", 1600);
  await made.page.setViewportSize({ width: 1600, height: 1000 });
  await made.ctx.addInitScript(() => {
    localStorage.removeItem("titanos.layout");
    localStorage.setItem(
      "titanos.desktop.windows.alex",
      JSON.stringify([
        {
          id: "w1",
          app: "app:settings",
          href: "/settings/about",
          title: "Systemsteuerung",
          x: 120,
          y: 60,
          w: 1000,
          h: 700,
          z: 10,
          minimized: false,
          snap: null,
        },
      ]),
    );
  });
  let gone = false;
  await made.ctx.route("**/api/v1/session", (route) =>
    gone
      ? route.fulfill({
          json: {
            configAvailable: true,
            setupRequired: false,
            user: null,
            hostname: "titan-test",
          },
        })
      : route.fallback(),
  );
  await made.ctx.route("**/api/v1/login/accounts", (route) =>
    route.fulfill({ status: 404, json: { error: { code: "not_found" } } }),
  );
  await made.page.goto(base + "/");
  const handle = await made.page.waitForSelector("section.win iframe", {
    timeout: 15000,
  });
  await (await handle.contentFrame()).waitForSelector("main");
  // The session ends (elsewhere); the window's page loads again and notices it.
  gone = true;
  await (await handle.contentFrame()).evaluate(() => location.reload());
  await made.page
    .locator('[data-testid="login-page"]')
    .waitFor({ timeout: 15000 });
  const inFrames = await Promise.all(
    made.page
      .frames()
      .filter((f) => f !== made.page.mainFrame())
      .map((f) =>
        f
          .locator('[data-testid="login-page"]')
          .count()
          .catch(() => 0),
      ),
  );
  assert.ok(
    inFrames.every((n) => n === 0),
    "no login page inside a window",
  );
  await made.page.screenshot({ path: `${out}/window-session-lost.png` });
  await made.ctx.close();
}

// 4. "Not reachable": the new look, not the old card.
{
  const made = await create("owner", "graphit", 1440);
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  await made.ctx.route("**/api/v1/session", (route) => route.abort());
  await made.page.goto(base + "/");
  await made.page.getByRole("button", { name: "Erneut versuchen" }).waitFor();
  const bg = await made.page
    .locator(".login-background .wallpaper")
    .evaluate((el) => getComputedStyle(el).backgroundImage);
  assert.match(bg, /svg|url/, "the wallpaper behind the card");
  await made.page.screenshot({ path: `${out}/unreachable.png` });
  await made.ctx.close();
}

await browser.close();
const relevant = errors.filter(
  (e) => !/Failed to load resource|net::ERR/.test(e),
);
assert.deepEqual(relevant, [], "no script errors");
console.log("login picker ok");
