// Phone-shell regression checks. Run against a local web build with the shared API fixtures.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const engine = process.env.UI_ENGINE || "webkit";
const { [engine]: driver } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await driver.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5276";
const screenshots = process.env.UI_OUTPUT;
const errors = [];
const create = fixtureFactory(browser, errors);

async function check(width) {
  const { ctx, page, data } = await create("owner", "graphit", width, "de", {
    serviceWorkers: "block",
    isMobile: true,
    hasTouch: true,
    reducedMotion: "reduce",
    viewport: { width, height: 820 },
  });
  try {
    await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
    data.prefs.desktop = {
      version: 1,
      wallpaper: { source: "builtin", id: "nordlicht", live: false },
    };
    const catalog = ["photos", "music"].map((id) => ({
      id,
      source: "titanos",
      name: id === "photos" ? "Fotosammlung" : "Musikserver",
      description: { de: "Lokale Mediathek" },
      category: "media",
      image: id + ":latest",
      icon: "",
    }));
    await ctx.route("**/api/v1/apps", (r) =>
      r.fulfill({
        json: {
          sources: [
            { id: "titanos", name: "TitanOS", builtin: true, count: 2 },
          ],
          apps: catalog,
        },
      }),
    );
    const notices = [
      {
        id: 3,
        kind: "volume_test",
        time: new Date().toISOString(),
        severity: "info",
        title: "Volume geprüft",
        text: "Prüfung abgeschlossen.",
        subject: "Data",
      },
    ];
    await ctx.route("**/api/v1/notifications", (r) =>
      r.fulfill({ json: { messages: notices } }),
    );
    await ctx.route("**/api/v1/notifications/dismiss", (r) => {
      notices.splice(0);
      return r.fulfill({ json: { messages: [] } });
    });
    async function shot(name) {
      assert.ok(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
        name + " horizontally overflows",
      );
      if (screenshots) {
        await mkdir(screenshots, { recursive: true });
        await page.screenshot({
          path: screenshots + "/" + engine + "-" + width + "-" + name + ".png",
        });
      }
    }
    await page.goto(base + "/settings");
    await page
      .getByRole("heading", { name: "Einstellungen", exact: true })
      .waitFor();
    assert.match(
      await page
        .locator(".phone-wallpaper")
        .evaluate((el) => getComputedStyle(el).backgroundImage),
      /nordlicht/,
    );
    await shot("einstellungen");
    await page.locator("#settings-home-areas a").first().click();
    await page.waitForURL("**/settings?area=*");
    const target = page
      .locator('#settings-home-results a.srow[href^="/settings/"]')
      .first();
    const href = await target.getAttribute("href");
    await target.click();
    await page.waitForURL(
      (url) => url.pathname === new URL(href, base).pathname,
    );
    // §121: one bar names the page; the programs are in the tab "Apps".
    await page.locator("header.phone-bar .heading b").waitFor();
    assert.equal(await page.locator(".nord-page-head").count(), 0);
    assert.notEqual(new URL(page.url()).pathname, "/settings");
    await page.locator('[data-phone-tabs] a[href="/?phone=apps"]').click();
    await page.locator('[data-phone-apps] [data-home-key="app:appcenter"]').click();
    await page.waitForURL("**/apps");
    const tabs = page.locator(".nord-segments");
    await page.locator(".card", { hasText: "Fotosammlung" }).waitFor();
    await shot("app-center");
    await tabs.getByRole("button", { name: "Installiert" }).click();
    assert.equal(
      await page.locator(".card", { hasText: "Musikserver" }).count(),
      0,
    );
    await tabs.getByRole("button", { name: "Quellen" }).click();
    await page.locator("#sources").waitFor();
    await tabs.getByRole("button", { name: "Katalog" }).click();
    await page.locator(".catalog input[type=search]").fill("Musik");
    await page.locator(".card", { hasText: "Musikserver" }).waitFor();
    assert.equal(await page.locator(".card").count(), 1);
    await page.locator(".card").click();
    await page.waitForURL("**/apps/install/titanos/music");
    await page.locator('[data-phone-tabs] a[href="/?phone=apps"]').click();
    await page.locator('[data-phone-apps] [data-home-key="app:docker"]').click();
    await page.waitForURL("**/docker");
    await page.locator(".container-row").first().waitFor();
    await shot("docker");
    await tabs.getByRole("link", { name: "Ressourcen" }).click();
    await page.locator('a[href="/docker/images"].srow').click();
    await page.waitForURL("**/docker/images");
    await page.goBack();
    await tabs.getByRole("link", { name: "Container", exact: true }).click();
    await page.locator(".container-row").first().click();
    await page.locator(".service summary").first().click();
    await page.locator(".service[open] .service-content").first().waitFor();
    await shot("container-details");
    await page.locator("[data-phone-bell]").click();
    await page.locator("[data-notice]").first().click();
    await page.getByText("Prüfung abgeschlossen.", { exact: true }).waitFor();
    await shot("meldung");
    await page.locator("[data-notice-delete]").click();
    await page.locator(".notice-days .empty").waitFor();
    await page.locator(".phone-bar .back").first().click();
    await page.waitForURL("**/docker?group=*");
    assert.equal(await page.locator(".notice-center").count(), 0);
  } catch (err) {
    console.error(
      engine,
      width,
      page.url(),
      errors,
      (await page.locator("body").innerText()).slice(0, 1600),
    );
    throw err;
  } finally {
    await ctx.close();
  }
}
try {
  for (const width of [352, 430]) await check(width);
  assert.deepEqual(errors, []);
  console.log(
    engine +
      " · 352/430 px · settings, catalog, Docker, notification detail and dismissal passed",
  );
} finally {
  await browser.close();
}
