// Login page look (Settings › Appearance › Login page): the editor saves one
// look for the NAS, the logged-out login page shows it, and without settings
// the page is exactly as before. Isolated API data only: no request reaches a NAS.
import assert from "node:assert/strict";
import { mkdir, readFile } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5192";
const out = process.env.UI_OUTPUT || "out/validation/ui-login-appearance";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);
const picture = await readFile(
  new URL("../web/static/backgrounds/weltraum.webp", import.meta.url),
);
const imageID = "c".repeat(64);

function defaults() {
  return {
    configured: false,
    theme: "inherit",
    accent: "blau",
    customAccent: "#E5B92E",
    style: "glass",
    glass: 45,
    blur: 18,
    background: { source: "theme", fit: "cover", x: 50, y: 50, zoom: 100, dim: 30 },
    image: null,
    position: "center",
    width: "normal",
    texts: {},
    hideHost: false,
    hideLogo: false,
    clock: false,
  };
}

// The login appearance as titand keeps it, plus the requests that reached it.
async function loginRoutes(ctx, state) {
  await ctx.route("**/api/v1/login-appearance**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.startsWith("/api/v1/login-appearance/background/")) {
      state.imageRequests++;
      return path.endsWith(imageID)
        ? route.fulfill({ body: picture, contentType: "image/webp" })
        : route.fulfill({ status: 404, json: { error: { code: "not_found" } } });
    }
    if (request.method() === "PUT") {
      let body;
      const type = request.headers()["content-type"] || "";
      if (type.startsWith("multipart/")) {
        const raw = request.postDataBuffer().toString("latin1");
        const settings = /name="settings"\r\n\r\n([\s\S]*?)\r\n--/.exec(raw)[1];
        body = JSON.parse(Buffer.from(settings, "latin1").toString("utf8"));
        state.uploads++;
        body.image = { id: imageID, width: 1280, height: 720, bytes: picture.length };
      } else {
        body = request.postDataJSON();
        body.image = body.removeImage ? null : state.value.image;
      }
      delete body.removeImage;
      state.saves.push(body);
      state.value = { ...body, configured: true };
      return route.fulfill({ json: state.value });
    }
    if (request.method() === "DELETE") {
      state.value = defaults();
      return route.fulfill({ status: 204 });
    }
    return route.fulfill({ json: state.value });
  });
}

// A browser that is not logged in, with a given look on the NAS.
async function loggedOut(value, { width = 1440, theme = "graphit", height = 900 } = {}) {
  const { ctx, page } = await create("owner", theme, width);
  await page.setViewportSize({ width, height });
  const state = { value, saves: [], uploads: 0, imageRequests: 0 };
  await loginRoutes(ctx, state);
  await ctx.route("**/api/v1/session", (route) =>
    route.fulfill({
      json: {
        configAvailable: true,
        setupRequired: false,
        user: null,
        hostname: value.hideHost ? "" : "titan-test",
      },
    }),
  );
  await ctx.route("**/api/v1/login", (route) =>
    route.fulfill({
      status: 401,
      json: { error: { code: "second_factor_required", message: "code" } },
    }),
  );
  await page.goto(base + "/");
  await page.getByRole("button", { name: /Anmelden|Sign in/ }).waitFor();
  return { ctx, page, state };
}

async function noHorizontalScroll(page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - innerWidth,
  );
  assert.ok(overflow <= 1, `horizontal scroll ${overflow}`);
}

try {
  // 1. Without settings: the login page as it always was.
  {
    const { ctx, page } = await loggedOut(defaults());
    const main = page.locator("main.auth");
    assert.equal(await main.getAttribute("style"), null);
    assert.equal(await main.getAttribute("data-theme"), null);
    assert.equal(await page.locator(".login-background").count(), 0);
    assert.equal(await page.locator("h1").textContent(), "Anmelden");
    await page.getByText("titan-test").waitFor();
    assert.equal(await page.locator(".auth .logo").count(), 1);
    const card = await page.locator("section.card").boundingBox();
    assert.equal(Math.round(card.width), 440);
    assert.ok(Math.abs(card.x + card.width / 2 - 720) <= 1, "card centered");
    await page.screenshot({ path: `${out}/login-default.png` });
    await ctx.close();
  }

  // 2. Editor: only for owners and administrators, saves one look for the NAS.
  {
    const { ctx, page } = await create("user");
    await loginRoutes(ctx, { value: defaults(), saves: [], uploads: 0, imageRequests: 0 });
    await page.goto(base + "/settings/appearance");
    await page.getByRole("heading", { name: "Theme", exact: true }).first().waitFor();
    assert.equal(await page.getByRole("heading", { name: "Anmeldeseite" }).count(), 0);
    await ctx.close();
  }
  let saved;
  {
    const { ctx, page } = await create("owner");
    const state = { value: defaults(), saves: [], uploads: 0, imageRequests: 0 };
    await loginRoutes(ctx, state);
    await page.goto(base + "/settings/appearance");
    const section = page.locator("section.login-look");
    await section.getByRole("heading", { name: "Anmeldeseite" }).waitFor();
    await section.getByText("Gilt für das ganze NAS").waitFor();
    // The preview is the login page itself, scaled down.
    await section.locator(".frame h1", { hasText: "Anmelden" }).waitFor();

    await section.getByLabel(/^Hintergrund/).selectOption("builtin");
    await section.getByRole("radio", { name: "Hintergrund Nordlicht" }).click();
    await section.getByRole("radio", { name: "Hintergrund Wüste" }).click();
    await section.getByRole("radio", { name: "Hintergrund Nordlicht" }).click();
    await section.getByLabel(/^Theme/).selectOption("mitternacht");
    await section.getByLabel("Stil der Flächen").selectOption("glass");
    await section.getByRole("radio", { name: "Türkis" }).click();
    const de = section.locator(".language").first();
    await de.getByLabel("Überschrift").fill("Willkommen zu Hause");
    await de.getByLabel("Begrüßung oder Hinweis").fill("Nur für die Familie. <b>Kein HTML</b>");
    await section.getByLabel("Uhrzeit und Datum zeigen").check();
    await section.locator(".frame .welcome", { hasText: "<b>Kein HTML</b>" }).waitFor();
    assert.equal(await section.locator(".frame .welcome b").count(), 0);
    await section.locator(".frame .clock").waitFor();

    // Contrast warning: dark text on a see-through card over a dark motif.
    const warning = section.getByText(/nur etwa .* Kontrast/);
    assert.equal(await warning.count(), 0);
    await section.getByLabel(/^Theme/).selectOption("hell");
    await section.getByLabel("Stil der Flächen").selectOption("outline");
    await section.getByRole("radio", { name: "Hintergrund Weltraum" }).click();
    await warning.waitFor();
    await section.getByLabel("Stil der Flächen").selectOption("flat");
    await warning.waitFor({ state: "detached" });
    await section.getByLabel(/^Theme/).selectOption("mitternacht");
    await section.getByLabel("Stil der Flächen").selectOption("glass");
    await section.getByRole("radio", { name: "Hintergrund Nordlicht" }).click();

    // Preview in a new tab without logging out.
    const [popup] = await Promise.all([
      ctx.waitForEvent("page"),
      section.getByRole("button", { name: "Vorschau in neuem Tab" }).click(),
    ]);
    await popup.getByText("Vorschau – Anmelden ist hier ohne Wirkung").waitFor();
    await popup.locator("h1", { hasText: "Willkommen zu Hause" }).waitFor();
    assert.match(
      await popup.locator(".login-background .picture").getAttribute("style"),
      /nordlicht\.webp/,
    );
    await popup.close();

    await section.getByRole("button", { name: "Anmeldeseite speichern" }).click();
    await section.getByText("Anmeldeseite gespeichert.").waitFor();
    saved = state.saves.at(-1);
    assert.equal(saved.theme, "mitternacht");
    assert.equal(saved.accent, "tuerkis");
    assert.deepEqual(saved.background, { source: "builtin", id: "nordlicht", fit: "cover", x: 50, y: 50, zoom: 100, dim: 30 });
    assert.equal(saved.texts.de.title, "Willkommen zu Hause");
    assert.equal(saved.clock, true);
    assert.equal("configured" in saved, false);
    await section.screenshot({
      path: `${out}/settings-section.png`,
      // The sticky page header would cover part of the tall section.
      style: "header { visibility: hidden !important; }",
    });

    // Mobile: the section fits without horizontal scrolling.
    await page.setViewportSize({ width: 375, height: 812 });
    await section.scrollIntoViewIfNeeded();
    await noHorizontalScroll(page);
    await ctx.close();
  }

  // 3. Logged out: the saved look (glass on a gallery motif, welcome text, 2FA step).
  {
    const { ctx, page } = await loggedOut({ ...saved, configured: true });
    const main = page.locator("main.auth");
    assert.equal(await main.getAttribute("data-theme"), "mitternacht");
    assert.equal(await main.getAttribute("data-surface"), "glass");
    await page.locator("h1", { hasText: "Willkommen zu Hause" }).waitFor();
    await page.locator(".welcome", { hasText: "<b>Kein HTML</b>" }).waitFor();
    await page.locator(".clock time").waitFor();
    assert.match(await page.locator(".login-background .picture").getAttribute("style"), /nordlicht\.webp/);
    await page.screenshot({ path: `${out}/login-glass-motif.png` });
    // The two-factor step keeps the look.
    await page.getByLabel("Name").fill("alex");
    await page.getByLabel("Passwort").fill("secret-password");
    await page.getByRole("button", { name: "Anmelden" }).click();
    await page.getByLabel("Code").waitFor();
    await page.locator("h1", { hasText: "Willkommen zu Hause" }).waitFor();
    // English falls back to the German texts.
    await page.getByRole("radio", { name: "English" }).click();
    await page.locator("h1", { hasText: "Willkommen zu Hause" }).waitFor();
    await ctx.close();
  }

  // 4. Hell theme, card on the left, flat, wide, without logo; mobile centers it.
  {
    const look = {
      ...defaults(),
      configured: true,
      theme: "hell",
      accent: "violett",
      style: "flat",
      position: "left",
      width: "wide",
      hideLogo: true,
      texts: { de: { title: "Familien-NAS", notice: "Bei Problemen: Alex fragen." } },
    };
    const { ctx, page } = await loggedOut(look, { theme: "graphit" });
    const main = page.locator("main.auth");
    assert.equal(await main.getAttribute("data-theme"), "hell");
    assert.equal(await main.getAttribute("data-mode"), "light");
    assert.equal(await page.locator(".auth .logo").count(), 0);
    const card = await page.locator("section.card").boundingBox();
    assert.ok(card.x < 200, `card left: ${card.x}`);
    assert.equal(Math.round(card.width), 560);
    // Text on the light card is dark enough.
    const colors = await page.evaluate(() => {
      const card = getComputedStyle(document.querySelector("section.card"));
      return { text: card.color, bg: card.backgroundColor };
    });
    assert.equal(colors.text, "rgb(20, 24, 29)");
    assert.equal(colors.bg, "rgb(255, 255, 255)");
    await page.screenshot({ path: `${out}/login-hell-left.png` });
    await page.setViewportSize({ width: 375, height: 812 });
    const mobile = await page.locator("section.card").boundingBox();
    assert.ok(Math.abs(mobile.x + mobile.width / 2 - 187.5) <= 1, "centered on phones");
    await noHorizontalScroll(page);
    await page.screenshot({ path: `${out}/login-hell-left-mobile.png`, fullPage: true });
    await ctx.close();
  }

  // 5. Own image, darkened, black theme, no hostname, clock.
  {
    const look = {
      ...defaults(),
      configured: true,
      theme: "schwarz",
      accent: "kupfer",
      style: "glass",
      glass: 60,
      background: { source: "custom", fit: "cover", x: 50, y: 40, zoom: 110, dim: 55 },
      image: { id: imageID, width: 1280, height: 720, bytes: picture.length },
      hideHost: true,
      clock: true,
      texts: { en: { title: "Welcome back" } },
    };
    const { ctx, page, state } = await loggedOut(look);
    await page.locator("h1", { hasText: "Welcome back" }).waitFor();
    assert.equal(await page.getByText("titan-test").count(), 0);
    assert.match(
      await page.locator(".login-background .picture").getAttribute("style"),
      new RegExp(`/api/v1/login-appearance/background/${imageID}`),
    );
    assert.equal(await page.locator(".login-background .dim").evaluate((e) => e.style.opacity), "0.55");
    await page.waitForFunction(() => document.querySelector(".clock time"));
    await page.waitForTimeout(300);
    assert.ok(state.imageRequests >= 1);
    await page.screenshot({ path: `${out}/login-own-image-dark.png` });
    await page.setViewportSize({ width: 375, height: 812 });
    await noHorizontalScroll(page);
    await ctx.close();
  }

  // 6. Upload of an own image in the editor goes out as multipart; default again removes the file.
  {
    const { ctx, page } = await create("owner");
    const state = { value: defaults(), saves: [], uploads: 0, imageRequests: 0 };
    await loginRoutes(ctx, state);
    await page.goto(base + "/settings/appearance");
    const section = page.locator("section.login-look");
    await section.getByRole("heading", { name: "Anmeldeseite" }).waitFor();
    const chooser = page.waitForEvent("filechooser");
    await section.getByLabel(/^Hintergrund/).selectOption("custom");
    await (await chooser).setFiles({ name: "weltraum.webp", mimeType: "image/webp", buffer: picture });
    await section.getByText("Wird beim Speichern hochgeladen.").waitFor();
    await section.getByRole("button", { name: "Anmeldeseite speichern" }).click();
    await section.getByText("Anmeldeseite gespeichert.").waitFor();
    assert.equal(state.uploads, 1);
    assert.equal(state.value.background.source, "custom");
    await section.getByRole("button", { name: "Standard", exact: true }).click();
    await section.getByText("Das gespeicherte eigene Bild wird beim Speichern entfernt.").waitFor();
    await section.getByRole("button", { name: "Anmeldeseite speichern" }).click();
    await section.getByText("Anmeldeseite gespeichert.").waitFor();
    assert.equal(state.value.configured, false);
    await ctx.close();
  }

  // 7. The light theme as the browser's own look, without login settings, is unchanged too.
  {
    const { ctx, page } = await loggedOut(defaults(), { theme: "hell", width: 375, height: 812 });
    assert.equal(await page.locator("main.auth").getAttribute("style"), null);
    assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), "hell");
    await noHorizontalScroll(page);
    await page.screenshot({ path: `${out}/login-default-hell-mobile.png` });
    await ctx.close();
  }

  assert.deepEqual(errors, []);
  console.log("login appearance UI checks passed");
} finally {
  await browser.close();
}
