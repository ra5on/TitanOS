// Settings sidebar (decision of 28 Oct 2026, "Entwurf A – Seitenleiste"):
// sidebar beside every settings page, active item, search with keyboard,
// overview list, phone list with a way back, no horizontal scroll, contrast
// in the light theme, states read once. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5190 node scripts/test-ui-settings-sidebar.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5190";
const out = process.env.UI_OUTPUT || "out/validation/ui-settings-sidebar";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

// The area count follows settings-search.ts instead of a number that goes stale.
const source = await readFile(
  new URL("../web/src/lib/settings-search.ts", import.meta.url),
  "utf8",
);
const list =
  source.match(/export const settingsAreas:[^=]*=\s*\[([\s\S]*?)\n\];/)?.[1] ?? "";
const entries = list.match(/\{\s*id:[^}]*\}/g) ?? [];
const allAreas = entries.length;
const userAreas = entries.filter((e) => !/admin:\s*true/.test(e)).length;
assert.ok(allAreas > userAreas && userAreas > 0);

const nav = (page) =>
  page.getByRole("navigation", { name: "Einstellungsbereiche", exact: true });
const overflow = (page) =>
  page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
async function fonts(page) {
  await page.evaluate(async () => {
    for (const f of ['400 14px "IBM Plex Sans"', '500 14px "IBM Plex Sans"', '600 14px "IBM Plex Sans"'])
      await document.fonts.load(f);
    await document.fonts.ready;
  });
}
// Contrast of an element's text against what is behind it, colors resolved
// by the browser (also color-mix and alpha) through a canvas.
function contrast(page, selector) {
  return page.locator(selector).first().evaluate((el) => {
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = 1;
    const g = canvas.getContext("2d", { willReadFrequently: true });
    const layers = [];
    for (let node = el; node; node = node.parentElement) {
      const bg = getComputedStyle(node).backgroundColor;
      if (bg && bg !== "transparent" && bg !== "rgba(0, 0, 0, 0)") layers.unshift(bg);
    }
    layers.unshift(getComputedStyle(document.body).backgroundColor);
    g.fillStyle = "#fff";
    g.fillRect(0, 0, 1, 1);
    for (const layer of layers) {
      g.fillStyle = layer;
      g.fillRect(0, 0, 1, 1);
    }
    const back = [...g.getImageData(0, 0, 1, 1).data].slice(0, 3);
    g.fillStyle = getComputedStyle(el).color;
    g.fillRect(0, 0, 1, 1);
    const front = [...g.getImageData(0, 0, 1, 1).data].slice(0, 3);
    const lum = (rgb) => {
      const [r, gr, b] = rgb.map((v) => {
        v /= 255;
        return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * r + 0.7152 * gr + 0.0722 * b;
    };
    const [a, b] = [lum(front), lum(back)].sort((x, y) => y - x);
    return (a + 0.05) / (b + 0.05);
  });
}

try {
  // Desktop: sidebar on a subpage, active item, state marker, compact controls.
  {
    const { ctx, page, data } = await create("owner");
    data.update.available = { version: "0.31.0", tag: "v0.31.0", channel: "stable", released: "2026-10-27T10:00:00Z", kernel: "6.12", changelog: "", files: [{ name: "filesystem.squashfs", size: 6e8 }] };
    // The shared fixtures leave this page empty; it is one of the pages visited.
    await ctx.route("**/api/v1/system/power-button", (route) =>
      route.fulfill({ json: { settings: { action: "poweroff" }, readOnly: false } }),
    );
    await page.goto(base + "/settings/updates");
    await nav(page).waitFor();
    const current = nav(page).locator('a[aria-current="page"]');
    await current.waitFor();
    assert.equal(await current.count(), 1);
    assert.equal(await current.getAttribute("href"), "/settings/updates");
    await current.getByText("Update verfügbar").waitFor({ state: "attached" });
    assert.match(
      (await current.getAttribute("title")) ?? (await current.locator(".marker").getAttribute("title")),
      /Update verfügbar/,
    );
    assert.equal(await nav(page).locator(".group-title").count(), 6);
    assert.deepEqual(
      (await nav(page).locator(".group-title").allTextContents()).map((s) => s.trim()),
      ["System", "Hardware und Energie", "Netzwerk und Zugriff", "Daten", "Integrationen", "Persönlich"],
    );
    assert.equal(await nav(page).locator("a").count(), allAreas);
    // The location is in the sidebar: no breadcrumb, no way back on desktop.
    assert.equal(await page.locator(".crumbs").first().isVisible(), false);
    assert.equal(await page.locator(".settings-back").isVisible(), false);
    await page.getByRole("heading", { level: 1, name: "Updates" }).waitFor();
    const button = page.locator(".settings-page .btn").first();
    const height = (await button.boundingBox()).height;
    assert.ok(height <= 32, `settings button height ${height}`);
    // Sidebar about 250 px wide and sticky under the header.
    const aside = await page.locator(".settings-aside").boundingBox();
    assert.ok(aside.width > 230 && aside.width < 270, `sidebar width ${aside.width}`);
    assert.equal(
      await page.locator(".settings-aside").evaluate((e) => getComputedStyle(e).position),
      "sticky",
    );
    await fonts(page);
    await page.screenshot({ path: `${out}/desktop-updates.png` });

    // Search: Enter opens the section anchor, the search clears afterwards.
    const search = nav(page).getByRole("searchbox", { name: "Einstellungen suchen" });
    await search.fill("DNS");
    assert.equal(
      await nav(page).locator("a").first().getAttribute("href"),
      "/settings/network#network-dns",
    );
    await search.press("Enter");
    await page.waitForURL("**/settings/network#network-dns");
    assert.equal(await search.inputValue(), "");
    assert.equal(
      await nav(page).locator('a[aria-current="page"]').getAttribute("href"),
      "/settings/network",
    );
    // Arrow keys move through the results, Enter opens the highlighted one.
    await search.fill("power");
    const hrefs = await nav(page)
      .locator("a")
      .evaluateAll((a) => a.map((x) => x.getAttribute("href")));
    assert.ok(hrefs.length >= 2, `results for "power": ${hrefs}`);
    await search.press("ArrowDown");
    assert.equal(
      await search.getAttribute("aria-activedescendant"),
      "settings-side-1",
    );
    await search.press("Enter");
    await page.waitForURL("**" + hrefs[1]);
    await search.fill("kein-treffer-xyz");
    await nav(page).getByText("Keine passenden Einstellungen gefunden.").waitFor();
    await search.press("Escape");
    assert.equal(await nav(page).locator("a").count(), allAreas);

    // The states were read once when the settings opened, not per page.
    await page.goto(base + "/settings");
    const upsReads = () =>
      data.requests.filter(([m, p]) => m === "GET" && p === "/ups").length;
    await page.locator(".settings-group").first().waitFor();
    const before = upsReads();
    await nav(page).getByRole("link", { name: "Hardware", exact: true }).click();
    await page.waitForURL("**/settings/hardware");
    await nav(page).getByRole("link", { name: "Ein-/Ausschalter", exact: true }).click();
    await page.waitForURL("**/settings/power-button");
    assert.equal(upsReads(), before, "states are not read again per page");

    // Overview: every area as a row with its state, in the content column.
    await nav(page).getByRole("link", { name: "Über TitanOS", exact: true }).click();
    await page.goto(base + "/settings");
    await page.locator(".settings-group").first().waitFor();
    assert.equal(await page.locator(".settings-group").count(), 6);
    assert.equal(await page.locator(".settings-page .areas a").count(), allAreas);
    await page
      .locator('.settings-page .areas a[href="/settings/updates"] .chip')
      .getByText("Update verfügbar")
      .waitFor();
    assert.equal(await page.getByRole("searchbox").count(), 1, "one visible search on desktop");
    assert.equal(await nav(page).locator('a[aria-current="page"]').count(), 0);
    await fonts(page);
    await page.screenshot({ path: `${out}/desktop-overview.png`, fullPage: true });
    // Old deep links keep working.
    await page.goto(base + "/settings/stick");
    await page.waitForURL("**/settings/backups/configuration");
    assert.equal(
      await nav(page).locator('a[aria-current="page"]').getAttribute("href"),
      "/settings/backups",
    );
    await ctx.close();
  }

  // Phone: the overview is the list, subpages get a way back, no sidebar.
  {
    const { ctx, page } = await create("owner", "graphit", 375);
    await page.goto(base + "/settings");
    await page.locator(".settings-group").first().waitFor();
    assert.equal(await nav(page).isVisible(), false);
    assert.equal(await page.locator(".settings-group").count(), 6);
    assert.ok((await overflow(page)) <= 0, "no horizontal scroll on the overview");
    const search = page.getByRole("searchbox", { name: "Einstellungen suchen" });
    await search.fill("SSH");
    assert.equal(await page.locator(".settings-page .areas a").count(), 1);
    await search.fill("");
    await fonts(page);
    await page.screenshot({ path: `${out}/mobile-overview.png`, fullPage: true });
    await page.locator('.settings-page .areas a[href="/settings/updates"]').click();
    await page.waitForURL("**/settings/updates");
    const back = page.getByRole("link", { name: "‹ Einstellungen", exact: true });
    await back.waitFor();
    assert.ok((await back.boundingBox()).height >= 44);
    assert.ok((await overflow(page)) <= 0, "no horizontal scroll on a subpage");
    await fonts(page);
    await page.screenshot({ path: `${out}/mobile-updates.png`, fullPage: true });
    await back.click();
    await page.waitForURL(/\/settings$/);
    for (const path of ["/settings/network", "/settings/backups", "/settings/appearance"]) {
      await page.goto(base + path);
      await page.locator("h1").first().waitFor();
      assert.ok((await overflow(page)) <= 0, `no horizontal scroll on ${path}`);
    }
    await ctx.close();
  }

  // Light theme: readable sidebar and overview.
  {
    const { ctx, page } = await create("owner", "hell");
    await page.goto(base + "/settings/updates");
    await nav(page).waitFor();
    await fonts(page);
    for (const selector of [
      '.settings-sidebar a[aria-current="page"]',
      ".settings-sidebar a:not([aria-current]) .label",
      ".settings-sidebar .group-title",
    ]) {
      const ratio = await contrast(page, selector);
      assert.ok(ratio >= 4.5, `${selector}: contrast ${ratio.toFixed(2)}`);
    }
    await page.screenshot({ path: `${out}/desktop-updates-hell.png` });
    await page.goto(base + "/settings");
    await page.locator(".settings-group").first().waitFor();
    for (const selector of [".settings-page .areas .name", ".settings-page .tile-title"]) {
      const ratio = await contrast(page, selector);
      assert.ok(ratio >= 4.5, `${selector}: contrast ${ratio.toFixed(2)}`);
    }
    await ctx.close();
  }

  // Accounts with the role "user" see only their areas.
  {
    const { ctx, page } = await create("user");
    await page.goto(base + "/settings/appearance");
    await nav(page).waitFor();
    assert.equal(await nav(page).locator("a").count(), userAreas);
    await nav(page).getByRole("searchbox").fill("SSH");
    assert.equal(await nav(page).locator("a").count(), 0);
    await ctx.close();
  }

  assert.deepEqual(errors, []);
  console.log("settings sidebar UI checks passed");
} finally {
  await browser.close();
}
