// Settings overview "Alle Einstellungen" (PLAN 61, drafts 34/35): search,
// equal tiles in four groups, "Zuletzt geändert", role filter, embedded mode
// (inside a same-origin frame: no sidebar, no way back, full width),
// and every settings page at 1440, 800 and 390 px without horizontal scroll,
// overlapping text/controls or cut text. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5233 node scripts/test-ui-settings-home.mjs
// SETTINGS_HOME_PAGES=0 skips the page sweep. The shared fixtures leave many
// pages empty; for a fuller sweep, run Vite against a `titand -dev` with a
// throwaway config (TITANOS_DEV_API) and set SETTINGS_HOME_LOGIN=name and
// SETTINGS_HOME_PASSWORD_FILE=/path/to/file (an owner of that test config):
// then the page sweep signs in there instead of using fixtures.
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
const base = process.env.UI_URL || "http://127.0.0.1:5233";
const out = process.env.UI_OUTPUT || "out/validation/settings-home";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const source = await readFile(
  new URL("../web/src/lib/settings-search.ts", import.meta.url),
  "utf8",
);
const list =
  source.match(/export const settingsAreas:[^=]*=\s*\[([\s\S]*?)\n\];/)?.[1] ??
  "";
const entries = list.match(/\{\s*id:[^}]*\}/g) ?? [];
const ids = entries.map((e) => e.match(/id: '([^']+)'/)[1]);
const userIds = entries
  .filter((e) => !/admin:\s*true/.test(e))
  .map((e) => e.match(/id: '([^']+)'/)[1]);

async function fonts(page) {
  await page.evaluate(async () => {
    for (const f of [
      '400 14px "IBM Plex Sans"',
      '500 14px "IBM Plex Sans"',
      '600 14px "IBM Plex Sans"',
    ])
      await document.fonts.load(f);
    await document.fonts.ready;
  });
}

// Layout problems inside the settings column: page wider than the window,
// text cut by a clipping box, and text or controls lying on each other.
function problems(page, frame) {
  return (frame ?? page).evaluate(() => {
    const root = document.querySelector(".settings-shell") ?? document.body;
    const found = [];
    if (document.documentElement.scrollWidth > innerWidth + 1)
      found.push(
        `horizontal scroll ${document.documentElement.scrollWidth} > ${innerWidth}`,
      );
    const visible = (el) => {
      // checkVisibility also leaves out the content of closed <details>.
      if (
        el.checkVisibility &&
        !el.checkVisibility({ visibilityProperty: true, opacityProperty: true })
      )
        return false;
      if (el.closest("details:not([open])") && !el.closest("summary"))
        return false;
      const s = getComputedStyle(el);
      return (
        s.visibility !== "hidden" &&
        s.display !== "none" &&
        el.getClientRects().length > 0
      );
    };
    const name = (el) =>
      `${el.tagName.toLowerCase()}${el.className && typeof el.className === "string" ? "." + el.className.trim().split(/\s+/).join(".") : ""} "${(el.textContent ?? "").trim().slice(0, 40)}"`;
    const floating = (el) => {
      for (let n = el; n && n !== root; n = n.parentElement)
        if (/sticky|fixed/.test(getComputedStyle(n).position)) return true;
      return false;
    };
    // Boxes: text lines (per text node) and controls.
    const boxes = [];
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (
        !node.textContent.trim() ||
        !node.parentElement ||
        !visible(node.parentElement)
      )
        continue;
      // Hidden from assistive tech = decoration (e.g. the dimmed page behind
      // the sample dialog of Appearance), meant to lie under something.
      if (
        node.parentElement.closest(
          '.sr-only, option, svg, select, textarea, dialog:not([open]), [aria-hidden="true"]',
        )
      )
        continue;
      const range = document.createRange();
      range.selectNodeContents(node);
      for (const r of range.getClientRects())
        if (r.width > 1 && r.height > 1)
          boxes.push({ r, el: node.parentElement, text: true });
    }
    for (const el of root.querySelectorAll(
      ".btn, input:not([type=hidden]), select, textarea, .chip, [role=switch], [role=tab]",
    ))
      if (visible(el) && !el.closest("dialog:not([open]), .sr-only"))
        boxes.push({ r: el.getBoundingClientRect(), el, text: false });
    for (let i = 0; i < boxes.length; i++)
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i],
          b = boxes[j];
        if (a.el === b.el || a.el.contains(b.el) || b.el.contains(a.el))
          continue;
        // Sticky save bars float over the content on purpose.
        if (floating(a.el) || floating(b.el)) continue;
        // Placeholder-like overlays inside fields (icons, units) are meant.
        if (
          a.el.closest(".field-wrap, .input-wrap, .unit") &&
          b.el.closest(".field-wrap, .input-wrap, .unit")
        )
          continue;
        const x = Math.min(a.r.right, b.r.right) - Math.max(a.r.left, b.r.left);
        const y = Math.min(a.r.bottom, b.r.bottom) - Math.max(a.r.top, b.r.top);
        if (x > 3 && y > 3)
          found.push(`overlap: ${name(a.el)} / ${name(b.el)}`);
      }
    // Text cut by a box that clips it (no ellipsis meant).
    for (const el of root.querySelectorAll("*")) {
      if (!visible(el) || el.closest(".sr-only")) continue;
      const s = getComputedStyle(el);
      if (!/hidden|clip/.test(s.overflowX) || s.textOverflow === "ellipsis")
        continue;
      if (
        el.scrollWidth > el.clientWidth + 2 &&
        el.clientWidth > 0 &&
        el.textContent.trim()
      )
        found.push(`cut: ${name(el)} ${el.scrollWidth} > ${el.clientWidth}`);
    }
    return [...new Set(found)].slice(0, 25);
  });
}

const pages = [
  ...ids.map((id) => `/settings/${id}`),
  "/settings/backups/data",
  "/settings/backups/applications",
  "/settings/backups/configuration",
  "/settings/backups/coverage",
  "/settings/backups/restore",
  "/settings/firewall?tab=networks",
  "/settings/firewall?tab=policies",
  "/settings/firewall?tab=autoblock",
  "/settings/ddns?tab=ipv64",
];
const report = [];

try {
  // Overview, desktop: search on top, the pages as rows under their eight areas.
  {
    const { ctx, page, data } = await create("owner");
    data.activity = {
      settings: { enabled: true, volume: "", days: 90, maxMiB: 64 },
      entries: [
        {
          at: "2026-11-06T08:01:00Z",
          user: "alex",
          via: "web",
          action: "PUT /api/v1/system/timezone",
          status: 200,
          ok: true,
        },
        {
          at: "2026-11-05T18:00:00Z",
          user: "alex",
          via: "web",
          action: "PUT /api/v1/firewall",
          status: 200,
          ok: true,
        },
        {
          at: "2026-11-05T17:00:00Z",
          user: "sam",
          via: "web",
          action: "POST /api/v1/files/delete",
          target: "x",
          status: 200,
          ok: true,
        },
        {
          at: "2026-11-02T09:00:00Z",
          user: "alex",
          via: "web",
          action: "PUT /api/v1/update/settings",
          status: 200,
          ok: true,
        },
      ],
      queries: [],
    };
    await page.goto(base + "/settings");
    const main = page.locator(".settings-shell");
    await main.locator("a.srow").first().waitFor();
    assert.deepEqual(
      // PLAN 78-1: the overview below has group headings of its own.
      (
        await main
          .locator(".sgroup-title:not(.overview *)")
          .allTextContents()
      ).map((s) => s.trim()),
      [
        "System",
        "Hardware und Energie",
        "Netzwerk",
        "Konten und Zugang",
        "Dienste und Meldungen",
        "KI und Smart Home",
        "Daten und Sicherung",
        "Updates",
      ],
    );
    assert.equal(
      // PLAN 78-1: the overview below has link rows of its own.
      await main.locator("a.srow:not(.overview a)").count(),
      ids.length,
      "one tile per area",
    );
    const hrefs = await main
      .locator("a.srow")
      .evaluateAll((a) => a.map((x) => x.getAttribute("href")));
    for (const id of ids) assert.ok(hrefs.includes(`/settings/${id}`), id);
    // PLAN 65: the window lists the settings pages; the page has no sidebar of its own.
    assert.equal(
      await page.locator(".settings-aside, .settings-sidebar").count(),
      0,
      "no sidebar",
    );
    // PLAN 78: every page is a flat row under the plain heading of its area –
    // one column, all rows as wide as the page.
    const sizes = await main.locator("a.srow").evaluateAll((a) =>
      a.map((x) => {
        const r = x.getBoundingClientRect();
        return [Math.round(r.width), Math.round(r.left)];
      }),
    );
    assert.equal(new Set(sizes.map((s) => s[0])).size, 1, `equal widths`);
    assert.equal(new Set(sizes.map((s) => s[1])).size, 1, "one column");
    // Zuletzt geändert: only settings changes, the file deletion is left out.
    const recent = main.locator(".recent li");
    await recent.first().waitFor();
    assert.equal(await recent.count(), 3);
    assert.equal(
      await recent.first().locator("a").getAttribute("href"),
      "/settings/timezone",
    );
    assert.ok(!(await main.locator(".recent").textContent()).includes("sam"));
    // Search in the page jumps to a section.
    const search = main.getByRole("searchbox", {
      name: "Einstellungen suchen",
    });
    await search.fill("SSH");
    assert.equal(
      await main.locator("a.srow").first().getAttribute("href"),
      "/settings/access#access-ssh",
    );
    assert.equal(
      await main.locator(".recent").count(),
      0,
      "no recent list while searching",
    );
    await search.press("Enter");
    await page.waitForURL("**/settings/access#access-ssh");
    await fonts(page);
    await page.goto(base + "/settings");
    await main.locator(".recent li").first().waitFor();
    await fonts(page);
    await page.screenshot({ path: `${out}/overview-1440.png`, fullPage: true });
    // Light theme: tile texts readable.
    await ctx.close();
  }

  // Overview at 800 and 390 px, light theme too.
  for (const [width, theme] of [
    [800, "graphit"],
    [390, "graphit"],
    [1440, "hell"],
  ]) {
    const { ctx, page } = await create("owner", theme, width);
    await page.goto(base + "/settings");
    await page.locator("a.srow").first().waitFor();
    const lefts = await page
      .locator(".settings-shell a.srow")
      .evaluateAll(
        (a) =>
          new Set(a.map((x) => Math.round(x.getBoundingClientRect().left)))
            .size,
      );
    assert.equal(lefts, 1, `rows in one column at ${width} px`);
    assert.deepEqual(await problems(page), [], `overview at ${width}`);
    await fonts(page);
    await page.screenshot({
      path: `${out}/overview-${width}-${theme}.png`,
      fullPage: true,
    });
    await ctx.close();
  }

  // Accounts with the role "user": only their tiles, no activity log read.
  {
    const { ctx, page, data } = await create("user");
    await page.goto(base + "/settings");
    await page.locator("a.srow").first().waitFor();
    assert.equal(
      await page.locator(".settings-shell a.srow:not(.overview a)").count(),
      userIds.length,
    );
    assert.equal(await page.locator(".recent").count(), 0);
    assert.equal(data.activity, undefined, "the log is not read for users");
    // PLAN 71: the overview below shows users only what they may read.
    await page.locator(".overview .health").waitFor();
    assert.equal(
      await page.locator(".overview .services").count(),
      0,
      "no services for users",
    );
    await ctx.close();
  }

  // Embedded: the page inside a same-origin frame (a window of the desktop)
  // has no sidebar, no way back and uses the full width.
  {
    const { ctx, page } = await create("owner", "graphit", 1440);
    await page.goto(base + "/settings/about");
    await page.locator(".settings-shell").waitFor();
    for (const [path, name] of [
      ["/settings", "overview"],
      ["/settings/updates", "updates"],
    ]) {
      await page.evaluate((src) => {
        document.querySelector("#embed-test")?.remove();
        const f = document.createElement("iframe");
        f.id = "embed-test";
        f.src = src;
        f.style.cssText =
          "position:fixed;left:0;top:0;width:800px;height:700px;z-index:99999;background:#111;border:0";
        document.body.append(f);
      }, path);
      const frame = page.frameLocator("#embed-test");
      await frame.locator("main.in-window .settings-page").first().waitFor();
      assert.equal(
        await frame.locator(".settings-aside").count(),
        0,
        "no sidebar",
      );
      assert.equal(
        await frame.locator(".settings-back").count(),
        0,
        "no way back",
      );
      const width = await frame
        .locator(".settings-shell")
        .evaluate((el) => el.getBoundingClientRect().width / innerWidth);
      assert.ok(width > 0.85, `content uses the window width (${width})`);
      if (name === "overview") {
        // PLAN 71: in a window the side bar lists the areas; the page is the
        // "Übersicht" with its cards (no tiles, no second search).
        await frame.locator(".overview .sgroup .sgroup-head").first().waitFor();
        assert.equal(
          // PLAN 78-1: the overview has link rows of its own (Benutzer, SSH …).
          await frame.locator("a.srow:not(.overview a)").count(),
          0,
          "no tiles in a window",
        );
        assert.equal(
          await frame.getByRole("searchbox").count(),
          0,
          "no second search",
        );
        for (const title of ["Zustand", "Zu erledigen", "Dienste"])
          await frame
            .locator(".overview h2.sgroup-title", { hasText: title })
            .waitFor();
      }
      await page.waitForTimeout(300);
      await page
        .locator("#embed-test")
        .screenshot({ path: `${out}/embedded-${name}.png` });
    }
    await ctx.close();
  }

  // Every settings page at three widths.
  if (process.env.SETTINGS_HOME_PAGES !== "0") {
    for (const width of [1440, 800, 390]) {
      let ctx, page;
      if (process.env.SETTINGS_HOME_LOGIN) {
        ctx = await browser.newContext({
          locale: "de-DE",
          viewport: { width, height: 1000 },
        });
        await ctx.addInitScript(() =>
          localStorage.setItem("titanos.locale", "de"),
        );
        page = await ctx.newPage();
        const password = (
          await readFile(process.env.SETTINGS_HOME_PASSWORD_FILE, "utf8")
        ).trim();
        const res = await page.request.post(base + "/api/v1/login", {
          data: { name: process.env.SETTINGS_HOME_LOGIN, password },
          headers: { Origin: base },
        });
        assert.ok(res.ok(), `login ${res.status()}`);
      } else {
        ({ ctx, page } = await create("owner", "graphit", width));
        await ctx.route("**/api/v1/system/power-button", (route) =>
          route.fulfill({
            json: { settings: { action: "poweroff" }, readOnly: false },
          }),
        );
      }
      for (const path of pages) {
        await page.goto(base + path);
        await page
          .locator(
            ".settings-shell .settings-page, .settings-shell .notice-error",
          )
          .first()
          .waitFor()
          .catch(() => {});
        await page.waitForTimeout(600);
        await fonts(page);
        const found = await problems(page);
        const file = `${path.replace(/^\/settings\/?/, "").replace(/[/?=&]/g, "-") || "overview"}-${width}.png`;
        await page
          .screenshot({ path: `${out}/pages/${file}`, fullPage: true })
          .catch(async () => {
            await mkdir(`${out}/pages`, { recursive: true });
            await page.screenshot({
              path: `${out}/pages/${file}`,
              fullPage: true,
            });
          });
        if (found.length) report.push({ path, width, found });
      }
      await ctx.close();
    }
    for (const r of report)
      console.log(`${r.width} ${r.path}\n  ${r.found.join("\n  ")}`);
  }

  if (errors.length) console.log("page errors:", errors);
  assert.deepEqual(report, [], "settings pages without layout problems");
  console.log("settings home UI checks passed");
} finally {
  await browser.close();
}
