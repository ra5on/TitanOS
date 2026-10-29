// Menus and windows (Settings › Appearance, owner request of 29 Oct 2026):
// account menu, drop-down, context menu, mobile navigation, command palette
// and dialogs use the shared --menu-*/--dialog-* surfaces; own values,
// "like the tiles", clamps, flat style, reduced transparency, the editor
// with live preview and reset. Desktop and 390 px. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5186 node scripts/test-ui-menus.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5186";
const out = process.env.UI_OUTPUT || "out/validation/ui-menus";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const own = {
  theme: "graphit",
  style: "glass",
  bg: "shapes",
  glass: 45,
  blur: 18,
  shine: 20,
  menus: { own: true, glass: 60, blur: 30, shine: 40 },
  dialogs: { own: true, glass: 50, blur: 8, shine: 10, dim: 40 },
};

async function open(role, appearance, width = 1440) {
  const made = await create(role, "graphit", width);
  made.data.prefs.appearance = appearance;
  await made.ctx.addInitScript((value) => {
    localStorage.setItem("titanos.appearance", JSON.stringify(value));
  }, appearance);
  return made;
}
// What the browser made of a surface.
function surface(locator) {
  return locator.evaluate((el) => {
    const s = getComputedStyle(el);
    const alpha = (c) => {
      const m = c.match(/rgba?\(([^)]+)\)/);
      if (!m) return 1;
      const parts = m[1].split(/[ ,/]+/).filter(Boolean);
      return parts.length > 3 ? Number(parts[3]) : 1;
    };
    return {
      backdrop: s.backdropFilter || s.webkitBackdropFilter,
      background: s.backgroundColor,
      alpha: alpha(s.backgroundColor),
      shadow: s.boxShadow,
    };
  });
}
function backdropOf(locator) {
  return locator.evaluate((el) => getComputedStyle(el, "::backdrop").backgroundColor);
}
const overflow = (page) => page.evaluate(() => document.documentElement.scrollWidth - innerWidth);

try {
  // Desktop, own values for menus and dialogs.
  {
    const { ctx, page } = await open("owner", own);
    await page.goto(base + "/");
    await page.locator("header .account .trigger").waitFor();
    await page.locator("header .account .trigger").click();
    const menu = page.locator(".menu.floating-menu[role='menu']");
    await menu.waitFor();
    let s = await surface(menu);
    assert.match(s.backdrop, /blur\(30px\)/, "account menu blur");
    assert.ok(Math.abs(s.alpha - 0.4) < 0.01, `account menu alpha ${s.alpha}`);
    assert.match(s.shadow, /inset/, "account menu shine edge");
    await page.screenshot({ path: `${out}/desktop-account-menu.png`, animations: "disabled" });
    await page.keyboard.press("Escape");
    await page.mouse.click(700, 500);

    // Command palette: a dialog that is a menu.
    await page.keyboard.press("Control+k");
    const palette = page.locator("dialog.as-menu");
    await palette.waitFor();
    s = await surface(palette);
    assert.match(s.backdrop, /blur\(30px\)/, "palette uses menu blur");
    assert.match(await backdropOf(palette), /0\.4\)$/, "dimming behind the palette");
    await page.screenshot({ path: `${out}/desktop-palette.png`, animations: "disabled" });
    await page.keyboard.press("Escape");

    // Drop-down in the file manager.
    await page.goto(base + "/files");
    const trigger = page.locator(".dropdown > button[aria-haspopup='menu']:visible").first();
    await trigger.waitFor();
    await trigger.click();
    const panel = page.locator(".dropdown-panel.floating-menu").first();
    await panel.waitFor();
    s = await surface(panel);
    assert.match(s.backdrop, /blur\(30px\)/, "drop-down blur");
    assert.ok(Math.abs(s.alpha - 0.4) < 0.01, `drop-down alpha ${s.alpha}`);
    await page.screenshot({ path: `${out}/desktop-dropdown.png`, animations: "disabled" });
    await page.keyboard.press("Escape");

    // Context menu (top layer) on the free area of the file list.
    const list = page.locator(".browser .list, .browser [role='grid'], .browser").first();
    const box = await list.boundingBox();
    await page.mouse.click(box.x + box.width / 2, box.y + box.height - 20, { button: "right" });
    const context = page.locator(".context-menu.floating-menu");
    if (await context.count()) {
      s = await surface(context);
      assert.match(s.backdrop, /blur\(30px\)/, "context menu blur");
      await page.screenshot({ path: `${out}/desktop-context-menu.png`, animations: "disabled" });
      await page.keyboard.press("Escape");
    }

    // A dialog with own values and the dimming behind it.
    await page.goto(base + "/shares");
    await page.getByRole("button", { name: /Freigabe (anlegen|hinzufügen|erstellen)|Neue Freigabe/ }).first().click();
    const dialog = page.locator("dialog[open]");
    await dialog.waitFor();
    s = await surface(dialog);
    assert.match(s.backdrop, /blur\(8px\)/, "dialog blur");
    assert.ok(Math.abs(s.alpha - 0.5) < 0.01, `dialog alpha ${s.alpha}`);
    assert.match(await backdropOf(dialog), /0\.4\)$/, "dimming behind dialogs");
    await page.screenshot({ path: `${out}/desktop-dialog.png`, animations: "disabled" });
    await ctx.close();
  }

  // Own values are clamped: 90 % transparency on a light theme stays 50 % opaque.
  {
    const { ctx, page } = await open("owner", { ...own, theme: "hell", menus: { own: true, glass: 90, blur: 20, shine: 20 } });
    await page.goto(base + "/");
    await page.locator("header .account .trigger").click();
    const s = await surface(page.locator(".menu.floating-menu[role='menu']"));
    assert.ok(Math.abs(s.alpha - 0.5) < 0.01, `light clamp ${s.alpha}`);
    await page.screenshot({ path: `${out}/desktop-light-clamped.png`, animations: "disabled" });
    await ctx.close();
  }

  // Like the tiles: at least 70 % opaque, tile blur.
  {
    const { ctx, page } = await open("owner", { theme: "graphit", style: "glass", bg: "shapes", glass: 90, blur: 12 });
    await page.goto(base + "/");
    await page.locator("header .account .trigger").click();
    const s = await surface(page.locator(".menu.floating-menu[role='menu']"));
    assert.match(s.backdrop, /blur\(12px\)/);
    assert.ok(Math.abs(s.alpha - 0.7) < 0.01, `inherited alpha ${s.alpha}`);
    await ctx.close();
  }

  // Flat stays opaque; reduced transparency turns glass opaque too.
  {
    const { ctx, page } = await open("owner", { ...own, style: "flat" });
    await page.goto(base + "/");
    await page.locator("header .account .trigger").click();
    let s = await surface(page.locator(".menu.floating-menu[role='menu']"));
    assert.equal(s.backdrop, "none");
    assert.equal(s.alpha, 1);
    await ctx.close();
  }
  {
    const { ctx, page } = await open("owner", own);
    const cdp = await ctx.newCDPSession(page);
    await cdp.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-reduced-transparency", value: "reduce" }] });
    await page.goto(base + "/");
    await page.locator("header .account .trigger").click();
    const s = await surface(page.locator(".menu.floating-menu[role='menu']"));
    assert.equal(s.backdrop, "none", "reduced transparency: no blur");
    assert.equal(s.alpha, 1, "reduced transparency: opaque");
    await ctx.close();
  }

  // 390 px: mobile navigation, account menu, drop-down, dialog.
  {
    const { ctx, page } = await open("owner", own, 390);
    await page.goto(base + "/");
    await page.locator(".mobile-nav-trigger").click();
    const mobile = page.locator("#mobile-navigation");
    await mobile.waitFor();
    let s = await surface(mobile);
    assert.match(s.backdrop, /blur\(30px\)/, "mobile menu is glass");
    assert.ok(Math.abs(s.alpha - 0.4) < 0.01, `mobile menu alpha ${s.alpha}`);
    assert.ok((await overflow(page)) <= 0);
    await page.screenshot({ path: `${out}/mobile-navigation.png`, animations: "disabled" });
    await page.keyboard.press("Escape");
    await page.locator("header .account .trigger").click();
    await page.locator(".menu.floating-menu[role='menu']").waitFor();
    await page.screenshot({ path: `${out}/mobile-account-menu.png`, animations: "disabled" });
    await page.keyboard.press("Escape");

    await page.goto(base + "/files");
    const trigger = page.locator(".dropdown > button[aria-haspopup='menu']:visible").first();
    await trigger.click();
    const panel = page.locator(".dropdown-panel.floating-menu").first();
    await panel.waitFor();
    s = await surface(panel);
    assert.match(s.backdrop, /blur\(30px\)/);
    await page.screenshot({ path: `${out}/mobile-dropdown.png`, animations: "disabled" });
    await page.keyboard.press("Escape");

    await page.goto(base + "/shares");
    await page.getByRole("button", { name: /Freigabe (anlegen|hinzufügen|erstellen)|Neue Freigabe/ }).first().click();
    await page.locator("dialog[open]").waitFor();
    assert.ok((await overflow(page)) <= 0);
    await page.screenshot({ path: `${out}/mobile-dialog.png`, animations: "disabled" });
    await ctx.close();
  }

  // The editor: own values change the preview live; reset goes back.
  {
    const { ctx, page } = await open("owner", { theme: "graphit", style: "glass", bg: "shapes" });
    await page.goto(base + "/settings/appearance");
    const section = page.locator("#menus");
    await section.getByRole("heading", { name: "Menüs und Fenster" }).waitFor();
    const preview = page.locator(".preview .menu.floating-menu");
    assert.match((await surface(preview)).backdrop, /blur\(18px\)/);
    await section.getByRole("radiogroup", { name: "Menüs" }).getByRole("radio", { name: "Eigene Werte" }).click();
    const blur = section.locator("#menus-blur");
    await blur.fill("36");
    assert.match((await surface(preview)).backdrop, /blur\(36px\)/, "live preview");
    // The page itself keeps the saved look until "Apply".
    assert.equal(await page.evaluate(() => document.documentElement.style.getPropertyValue("--menu-blur")), "18px");
    await section.locator("#dialog-dim").fill("20");
    assert.match(await page.locator(".preview .dim-layer").evaluate((e) => getComputedStyle(e).backgroundColor), /0\.2\)$/);
    await section.screenshot({ path: `${out}/editor-section.png`, animations: "disabled" });
    await page.locator("section.preview").screenshot({ path: `${out}/editor-preview.png`, animations: "disabled" });
    await page.getByRole("button", { name: "Standard wiederherstellen" }).click();
    assert.equal(await section.getByRole("radiogroup", { name: "Menüs" }).getByRole("radio", { name: "Wie die Kacheln" }).getAttribute("aria-checked"), "true");
    assert.match((await surface(preview)).backdrop, /blur\(18px\)/);
    await page.setViewportSize({ width: 390, height: 900 });
    assert.ok((await overflow(page)) <= 0, "no sideways scroll at 390 px");
    await ctx.close();
  }

  assert.deepEqual(errors, []);
  console.log("ui menus ok:", out);
} finally {
  await browser.close();
}
