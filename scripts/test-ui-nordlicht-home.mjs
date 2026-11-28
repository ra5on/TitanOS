// Nordlicht on a local UI with the regular account/session fixtures.
import assert from "node:assert/strict";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const engine = process.env.UI_ENGINE || "webkit";
const { [engine]: driver } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const base = process.env.UI_URL || "http://127.0.0.1:5276";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const browser = await driver.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const errors = [];
let active;
try {
  const { ctx, page, data } = await fixtureFactory(browser, errors)("owner", "graphit", 390, "de", {
    isMobile: true,
    hasTouch: true,
    serviceWorkers: "block",
    reducedMotion: "reduce",
    viewport: { width: 390, height: 844 },
  });
  active = page;
  await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  data.prefs.desktop = {
    version: 1,
    wallpaper: { source: "builtin", id: "nordlicht", live: false },
    phone: {
      // §121: this account chose the dock instead of the bar at the bottom.
      bottom: "dock",
      dock: {
        custom: true,
        apps: ["app:files", "app:mail", "app:docker", "app:calendar", "app:settings", "app:storage"],
      },
    },
  };
  await ctx.route("**/api/v1/preferences/desktop", async (route) => {
    if (route.request().method() !== "PUT") return route.fallback();
    data.prefs.desktop = route.request().postDataJSON();
    return route.fulfill({ status: 204 });
  });
  await ctx.route("**/api/v1/services/state", (route) =>
    route.fulfill({ json: { docker: true, calendar: true } }),
  );
  await ctx.route("**/api/v1/mail/access", (route) => route.fulfill({ json: { enabled: true } }));
  await page.goto(base + "/");
  const home = page.locator("[data-nord-home]");
  const dock = page.locator("[data-phone-dock]:visible");
  await home.waitFor();
  await home.locator('[data-home-key="app:mail"]').waitFor();
  assert.equal(await home.locator("[data-home-key]").count(), 3);
  assert.equal(await dock.locator("[data-dock-key]").count(), 6);
  if (process.env.UI_OUTPUT) await page.screenshot({ path: process.env.UI_OUTPUT });
  const fixed = await dock.locator(".home-button").boundingBox();
  await dock.locator(".scrolling").evaluate((el) => {
    el.scrollLeft = el.scrollWidth;
  });
  await page.waitForFunction(() =>
    document.querySelector(".indicators [aria-current]")?.getAttribute("aria-label")?.includes("2"),
  );
  assert.equal((await dock.locator(".home-button").boundingBox()).x, fixed.x);

  const fav = page.locator('[data-home-key="app:files"]');
  await fav.scrollIntoViewIfNeeded();
  const b = await fav.boundingBox();
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  await page.mouse.down();
  await page.getByRole("dialog", { name: "Startbildschirm anpassen" }).waitFor();
  await page.mouse.up();
  const editor = page.getByRole("dialog", { name: "Startbildschirm anpassen" });
  await editor.getByRole("button", { name: "Apps", exact: true }).click();
  await editor.getByLabel("App auswählen").selectOption("app:calendar");
  await editor.getByLabel("App hinzufügen").click();
  await editor.getByLabel("„Kalender“ entfernen").waitFor();
  await editor.getByRole("button", { name: "Unten am Handy", exact: true }).click();
  const saved = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === "/api/v1/preferences/desktop" &&
      r.request().postDataJSON()?.phone?.dock?.size === "large",
  );
  await editor.getByRole("radio", { name: "Groß" }).click();
  await editor.getByRole("button", { name: "Fertig" }).click();
  await page.waitForFunction(
    () => document.querySelector("[data-phone-dock]").style.getPropertyValue("--icon") === "55px",
  );
  const response = await saved;
  if (response.status() !== 204)
    throw Error(
      "Preferences fixture returned " +
        response.status() +
        " for " +
        response.url() +
        ": " +
        (await response.text()).slice(0, 180),
    );
  const sent = response.request().postDataJSON();
  assert.deepEqual(
    sent.phone.favorites,
    ["app:files", "app:docker", "app:mail", "app:calendar"],
    JSON.stringify({ sent: sent.phone, stored: data.prefs.desktop.phone }),
  );
  assert.deepEqual(data.prefs.desktop.phone.favorites, [
    "app:files",
    "app:docker",
    "app:mail",
    "app:calendar",
  ]);
  assert.equal(data.prefs.desktop.phone.dock.size, "large");
  await page.reload();
  await home.waitFor();
  await home.locator('[data-home-key="app:calendar"]').waitFor();
  await home.locator("[data-phone-bell]").click();
  await page.waitForURL("**/notifications");
  await dock.getByRole("button", { name: "Zum Startbildschirm" }).click();
  await home.waitFor();
  // §121: "Apps" leads to the list of all programs; the search opens the menu.
  await home.locator("a.title-link").click();
  await page.locator("[data-phone-apps]").waitFor();
  await page.locator("[data-phone-apps] .search").click();
  await page.locator("dialog.library[open] .launcher").waitFor();
  await page.keyboard.press("Escape");
  await dock.getByRole("button", { name: "Zum Startbildschirm" }).click();
  await home.waitFor();
  await home.locator('[data-home-key="app:files"]').click();
  await page.waitForURL("**/files");
  await dock.getByRole("button", { name: "Zum Startbildschirm" }).click();
  await home.waitFor();
  await page.goto(base + "/?phone=pages");
  await page.locator(".home .pages").waitFor();
  await page.locator(".home .top").getByRole("button", { name: "Zum Startbildschirm" }).click();
  await home.waitFor();
  await ctx.close();
  assert.deepEqual(errors, []);
  console.log(
    engine +
      " · Nordlicht home, long press, favorites, scrolling dock, size persistence, notifications, launcher, saved pages and Home passed",
  );
} catch (error) {
  console.error("Route:", active?.url(), "errors:", errors.slice(0, 4));
  if (active)
    console.error(
      (
        await active
          .locator("body")
          .innerText()
          .catch(() => "")
      ).slice(0, 550),
    );
  throw error;
} finally {
  await browser.close();
}
