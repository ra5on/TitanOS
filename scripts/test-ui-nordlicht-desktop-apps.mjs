// Desktop and phone open the real shell, with deterministic local catalog,
// file and container data. UI_BASELINE captures the same scenes from PR 414.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const base = process.env.UI_URL;
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const engine = process.env.UI_BROWSER || "chromium";
const pw = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await pw[engine].launch({ headless: true });
const baseline = process.env.UI_BASELINE === "1";
const out = process.env.UI_OUTPUT;
if (out) await mkdir(out, { recursive: true });
const errors = [];
const make = fixtureFactory(browser, errors);
async function run(theme, width) {
  const phone = width < 900;
  const {
    ctx,
    page: firstPage,
    data,
  } = await make("owner", theme, width, "de", {
    isMobile: phone,
    hasTouch: phone,
    viewport: { width, height: phone ? 844 : 900 },
    reducedMotion: "reduce",
    serviceWorkers: "block",
    nativeShell: true,
  });
  let page = firstPage;
  try {
    data.prefs.appearance.font = theme === "hell" ? 120 : 100;
    data.prefs.desktop = {
      version: 1,
      wallpaper: { source: "builtin", id: "nordlicht", live: false },
    };
    const files = [
      "Projekte",
      "Familie",
      "Steuer",
      "Rechnung.pdf",
      "Urlaub.jpg",
      "Notizen.txt",
    ].map((name, i) => ({
      name,
      dir: i < 3,
      size: i < 3 ? 0 : 413209,
      modified: "2026-11-21T14:00:00Z",
    }));
    await ctx.route("**/api/v1/files/list?*", (r) =>
      r.fulfill({ json: files }),
    );
    const catalog = ["photos", "music", "audiobooks"].map((id, n) => ({
      id,
      source: "titanos",
      name: ["Fotosammlung", "Musikserver", "Hörbücher"][n],
      description: {
        de: [
          "Bilder und Alben verwalten",
          "Musik im Heimnetz hören",
          "Hörbücher und Podcasts sammeln",
        ][n],
      },
      category: "media",
      icon: "",
      image: id + ":latest",
    }));
    await ctx.route("**/api/v1/apps", (r) =>
      r.fulfill({
        json: {
          apps: catalog,
          sources: [
            { id: "titanos", name: "TitanOS", builtin: true, count: 3 },
          ],
        },
      }),
    );
    const view = (path) =>
      phone
        ? page
        : page.frameLocator('iframe[data-app="app:' + path + '"]').last();
    const snap = async (name) => {
      if (out)
        await page.screenshot({
          path:
            out +
            "/" +
            engine +
            "-" +
            theme +
            "-" +
            width +
            "-" +
            name +
            ".png",
          animations: "disabled",
        });
    };
    async function coldOpen(path) {
      // Keep prior windows alive while their background requests finish. A cold
      // deep link opens a fresh document, including on the phone.
      if (page.url() !== "about:blank") {
        page = await ctx.newPage();
        page.on("pageerror", (err) => errors.push(String(err)));
      }
      page.setDefaultTimeout(30000);
      await page.goto(base + path);
    }
    async function open(path, app) {
      await coldOpen(path);
      const f = view(app);
      await f.locator("main").waitFor();
      if (!baseline) {
        assert.equal(
          await f.locator("html").getAttribute("data-nord-desktop"),
          phone ? null : path.split("/")[1].split("?")[0],
        );
      }
      return f;
    }
    let f = await open("/files?root=share%2FDocuments", "files");
    await f.getByText("Rechnung.pdf", { exact: true }).first().waitFor();
    if (theme === "hell") {
      const scale = await f
        .locator("html")
        .evaluate((el) =>
          getComputedStyle(el).getPropertyValue("--font-scale"),
        );
      assert.equal(Number(scale), 1.2);
    }
    await snap("dateien");
    if (!phone) {
      const up = f
        .locator(".command-bar button")
        .filter({ hasText: /Hochladen/ })
        .first();
      await up.focus();
      assert.ok(await up.evaluate((el) => el === document.activeElement));
      if (!baseline) {
        const row = f
          .locator(".file-entries .row")
          .filter({ hasText: "Rechnung.pdf" })
          .first();
        const contrast = await row.evaluate((el) => {
          const ctx = document.createElement("canvas").getContext("2d");
          ctx.canvas.width = ctx.canvas.height = 1;
          const chain = [];
          for (let p = el; p; p = p.parentElement) chain.unshift(p);
          for (const p of chain) {
            ctx.fillStyle = getComputedStyle(p).backgroundColor;
            ctx.fillRect(0, 0, 1, 1);
          }
          const lum = () => {
            const rgb = ctx.getImageData(0, 0, 1, 1).data.slice(0, 3);
            return Array.from(rgb, (x) => {
              const s = x / 255;
              return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
            }).reduce((v, c, i) => v + c * [0.2126, 0.7152, 0.0722][i], 0);
          };
          const bg = lum();
          ctx.fillStyle = getComputedStyle(el).color;
          ctx.fillRect(0, 0, 1, 1);
          const fg = lum();
          return (Math.max(bg, fg) + 0.05) / (Math.min(bg, fg) + 0.05);
        });
        assert.ok(contrast >= 4.5, "File list text contrast " + contrast);
        console.log(engine, theme, "file list contrast", contrast.toFixed(1));
      }
      assert.equal(
        await f
          .locator("html")
          .evaluate((e) => e.scrollWidth <= innerWidth + 1),
        true,
      );
    }
    f = await open("/docker", "docker");
    const install = f.getByRole("link", {
      name: "App installieren",
      exact: true,
    });
    await install.first().waitFor();
    await snap("docker");
    if (!phone && !baseline) {
      const more = f.getByRole("button", {
        name: "Aktionen und Einstellungen",
        exact: true,
      });
      await more.focus();
      await more.press("Enter");
      await f.getByRole("menuitem", { name: /Eigener Container/ }).waitFor();
      await snap("docker-menue");
      await page.keyboard.press("Escape");
      await f
        .getByRole("menuitem", { name: /Eigener Container/ })
        .waitFor({ state: "hidden" });
    }
    f = await open("/apps", "appcenter");
    await f.getByText("Fotosammlung", { exact: true }).first().waitFor();
    await snap("app-center");
    if (!phone && !baseline) {
      const installed = f.getByRole("button", { name: /^Installiert/ }).first();
      await installed.click();
      const card = f.locator("a.card").filter({ hasText: "Fotosammlung" });
      assert.equal(await card.getAttribute("href"), "/docker/apps/photos");
      await installed.click();
      assert.equal(
        await f
          .locator("a.card")
          .filter({ hasText: "Fotosammlung" })
          .getAttribute("href"),
        "/apps/install/titanos/photos",
      );
      // Assert that an externally opened admin settings link resolves even
      // if the optional agent is disabled for the fixture account.
      await coldOpen("/settings/agent");
      await page
        .frameLocator('iframe[data-app="app:settings"]')
        .locator("main")
        .waitFor();
    }
    if (!phone) {
      f = await open("/files?root=share%2FDocuments", "files");
      await f.getByText("Rechnung.pdf", { exact: true }).first().waitFor();
      const edge = page.locator(".win.focused .edge.e").first();
      const rect = await edge.boundingBox();
      assert.ok(rect, "window resize grip");
      const old = await f.locator("html").evaluate(() => innerWidth);
      await page.mouse.move(rect.x + rect.width / 2, rect.y + rect.height / 2);
      await page.mouse.down();
      await page.mouse.move(
        rect.x + rect.width / 2 - Math.max(0, old - 475),
        rect.y + rect.height / 2,
        { steps: 10 },
      );
      await page.mouse.up();
      const size = await f
        .locator("html")
        .evaluate(() => ({
          width: innerWidth,
          scroll: document.documentElement.scrollWidth,
        }));
      assert.ok(
        size.width < 630 && size.scroll <= size.width + 1,
        "narrow file window " + JSON.stringify(size),
      );
      await snap("schmales-fenster");
    }
  } finally {
    // After the last assertion, closing multiple live API pollers aborts
    // pending requests as part of teardown on WebKit.
    for (const testedPage of ctx.pages()) {
      testedPage.removeAllListeners("pageerror");
      testedPage.removeAllListeners("console");
    }
    await ctx.close();
  }
}
try {
  await run("graphit", 1440);
  await run("hell", 1280);
  await run("graphit", 390);
  assert.deepEqual(errors, []);
} finally {
  await browser.close();
}
