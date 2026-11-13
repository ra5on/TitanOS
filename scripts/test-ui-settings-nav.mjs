// PLAN 86 (owner 13.11.2026, variant A, draft 1): the menu of Systemsteuerung
// – eight areas that open their pages in the column, one at a time, the area
// of the page shown open by itself, an area with one page opening it at once
// – and the search that finds every single setting and leads to its row
// (tab chosen, rows opened, the row marked). Also: a normal account's
// column, old "?area=" links, the phone's "Alle Einstellungen" and its
// search. No sideways scroll, no cut text. Isolated API data only.
//
// SETTINGS_NAV_SHOTS=dir also writes the pictures for the owner (light,
// deviceScaleFactor 2) with German names.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5264 node scripts/test-ui-settings-nav.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5264";
const out = process.env.UI_OUTPUT || "out/validation/settings-nav";
const owner = process.env.SETTINGS_NAV_SHOTS || "";
await mkdir(out, { recursive: true });
if (owner) await mkdir(owner, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

async function shot(page, name) {
  await page.waitForTimeout(250);
  await page.screenshot({ path: `${out}/${name}.png`, animations: "disabled" });
  if (owner)
    await page.screenshot({
      path: `${owner}/${name}.png`,
      animations: "disabled",
    });
}

/** The desktop with one window of Systemsteuerung at `path`. */
async function desk(role = "owner", path = "/settings", width = 1280) {
  const height = 860;
  const made = await create(role, "hell", width, "de", {
    deviceScaleFactor: 2,
    viewport: { width, height },
  });
  // Tailscale installed: its row on Netzwerk › Fernzugriff opens to its parts.
  made.data.plugins = {
    driverUpdate: null,
    volume: "Data",
    kernel: "test",
    plugins: [
      {
        name: "tailscale",
        title: "Tailscale",
        description: { de: "Fernzugriff", en: "Remote access" },
        version: "1.0.0",
        installed: true,
        enabled: true,
        active: true,
        official: true,
        category: "remote",
        services: [],
        modules: [],
        problem: "",
        update: null,
      },
    ],
    catalog: { state: "ready" },
    recommendations: [],
    dismissed: 0,
    sources: [],
  };
  await made.ctx.addInitScript(
    ({ path, width, height }) => {
      localStorage.removeItem("titanos.layout");
      localStorage.setItem(
        "titanos.desktop.windows.alex",
        JSON.stringify([
          {
            id: "w1",
            app: "app:settings",
            href: path,
            title: "Systemsteuerung",
            x: 8,
            y: 4,
            w: width - 16,
            h: height - 110,
            z: 10,
            minimized: false,
            snap: null,
          },
        ]),
      );
    },
    { path, width, height },
  );
  await made.page.goto(base + "/");
  const side = made.page.locator("section.win nav.side");
  await side.waitFor({ timeout: 20000 });
  const frame = made.page.frameLocator("section.win iframe").first();
  await frame.locator("main").first().waitFor({ timeout: 20000 });
  await fonts(made.page);
  return { ...made, side, frame };
}

async function fonts(page) {
  await page.evaluate(async () => {
    for (const f of ['400 14px "IBM Plex Sans"', '600 14px "IBM Plex Sans"'])
      await document.fonts.load(f);
    await document.fonts.ready;
  });
}

/** Opens an area in the column unless it is open; waits for its pages. */
async function openArea(page, side, name) {
  const area = side.getByRole("button", { name, exact: true });
  if ((await area.getAttribute("aria-expanded")) !== "true") await area.click();
  await side.locator(".pages").first().waitFor();
  await page.waitForTimeout(250);
}

/** The address of the window's page, once it is `href`. */
function at(page, href) {
  return page.waitForFunction(
    (href) => {
      const loc =
        document.querySelector("section.win iframe")?.contentWindow?.location;
      return loc && loc.pathname + loc.search + loc.hash === href;
    },
    href,
    { timeout: 10000 },
  );
}

/** The entries of the column, as text; areas with their state. */
function column(side) {
  return side
    .locator(".items > .item, .items > .pages > .item")
    .evaluateAll((items) =>
      items.map((item) => {
        const text = item.textContent.replace(/\s+/g, " ").trim();
        const open = item.getAttribute("aria-expanded");
        const page = item.closest(".pages") ? "  " : "";
        return (
          page + text + (open === null ? "" : open === "true" ? " ▾" : " ▸")
        );
      }),
    );
}

async function sound(page, name, frame) {
  const target = frame ?? page;
  const overflow = await target.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 1, `${name}: page scrolls sideways by ${overflow}px`);
  const cut = await target.evaluate(cutTextIn);
  assert.deepEqual(cut, [], `${name}: cut text`);
}

// --- 1. The column at 1280 px: areas, accordion, keyboard ----------------
{
  const { ctx, page, side, frame } = await desk("owner", "/settings");
  await frame.locator(".overview .sgroup, .sgroup").first().waitFor();
  assert.deepEqual(await column(side), [
    "Übersicht",
    "System ▸",
    "Hardware und Energie ▸",
    "Netzwerk und Fernzugriff ▸",
    "Sicherheit und Konten ▸",
    "Benachrichtigungen",
    "Daten und Sicherung ▸",
    "Cluster",
    "Smart Home und KI ▸",
  ]);
  await sound(page, "column");
  await shot(page, "01-spalte-zugeklappt");

  // A click opens the area in the column; a second one closes it.
  const hardware = side.getByRole("button", { name: "Hardware und Energie" });
  await hardware.click();
  assert.equal(await hardware.getAttribute("aria-expanded"), "true");
  assert.deepEqual((await column(side)).slice(2, 9), [
    "Hardware und Energie ▾",
    "  Hardware",
    "  Temperaturen",
    "  Energie",
    "  Ein-/Ausschalter",
    "  USV",
    "  Schreibcache",
  ]);
  // One area at a time.
  await side.getByRole("button", { name: "System", exact: true }).click();
  assert.equal(await hardware.getAttribute("aria-expanded"), "false");
  assert.deepEqual((await column(side)).slice(1, 8), [
    "System ▾",
    "  Über TitanOS",
    "  Updates",
    "  Darstellung",
    "  Datum und Uhrzeit",
    "  Dienste",
    "  Fernansicht",
  ]);
  // A page in two clicks; the path above it names the area.
  await side.getByRole("button", { name: "Datum und Uhrzeit" }).click();
  await at(page, "/settings/timezone");
  await frame
    .getByRole("heading", { name: "Datum und Uhrzeit", exact: true })
    .waitFor();
  assert.equal(
    (await frame.locator(".set-path").innerText()).replace(/\s+/g, " ").trim(),
    "System › Datum und Uhrzeit",
  );
  assert.equal(
    await side.locator('.item[aria-current="page"]').innerText(),
    "Datum und Uhrzeit",
  );
  // An area with one page opens it at once (no expander).
  await side.getByRole("button", { name: "Cluster", exact: true }).click();
  await at(page, "/settings/cluster");
  // Let the page finish loading: a second navigation at once would abort
  // the first (SvelteKit then reports its promise as an error).
  await frame
    .locator("main h1")
    .filter({ hasText: "Cluster" })
    .waitFor({ state: "attached" });
  await page.waitForTimeout(600);
  assert.equal(
    await side
      .getByRole("button", { name: "Cluster", exact: true })
      .getAttribute("aria-expanded"),
    null,
  );
  // Keyboard: arrows move, right opens, left closes, Enter on a page opens it.
  const net = side.getByRole("button", { name: "Netzwerk und Fernzugriff" });
  await net.focus();
  await page.keyboard.press("ArrowRight");
  assert.equal(await net.getAttribute("aria-expanded"), "true");
  await page.keyboard.press("ArrowRight");
  assert.equal(
    await page.evaluate(() => document.activeElement?.textContent?.trim()),
    "Netzwerk",
  );
  await page.keyboard.press("ArrowDown");
  assert.equal(
    await page.evaluate(() => document.activeElement?.textContent?.trim()),
    "WireGuard",
  );
  await page.keyboard.press("Enter");
  await at(page, "/settings/wireguard");
  await page.keyboard.press("ArrowLeft");
  assert.equal(await net.getAttribute("aria-expanded"), "false");
  assert.equal(
    await page.evaluate(() =>
      document.activeElement?.textContent?.replace(/\s+/g, " ").trim(),
    ),
    "Netzwerk und Fernzugriff",
  );
  // The area of the page shown is open again after the next page.
  await page.keyboard.press("Enter");
  assert.equal(await net.getAttribute("aria-expanded"), "true");

  // Darstellung with its area open: the picture of the open column.
  await openArea(page, side, "System");
  await side.getByRole("button", { name: "Darstellung" }).click();
  await at(page, "/settings/appearance");
  await frame.getByRole("heading", { name: "Sprache und Schrift" }).waitFor();
  await sound(page, "appearance");
  await shot(page, "02-bereich-aufgeklappt-darstellung");

  // Old links of an area page lead to the area's first page.
  await page.evaluate(() => {
    const frame = document.querySelector("section.win iframe");
    frame.contentWindow.postMessage(
      { type: "titanos-goto", href: "/settings?area=accounts" },
      location.origin,
    );
  });
  await at(page, "/settings/users");
  assert.equal(
    await side
      .getByRole("button", { name: "Sicherheit und Konten" })
      .getAttribute("aria-expanded"),
    "true",
  );

  // The folded column: symbols only; an area opens its first page.
  await side.getByRole("button", { name: "Seitenleiste einklappen" }).click();
  await side
    .locator(".side.folded, nav.side.folded")
    .first()
    .waitFor()
    .catch(() => {});
  await page.waitForTimeout(300);
  await shot(page, "03-spalte-eingeklappt");
  await side.locator('.item[title="Hardware und Energie"]').click();
  await at(page, "/settings/hardware");
  await side.locator(".item.fold").click();
  await ctx.close();
}

// --- 2. The search: every setting, and a hit leads to its row ------------
{
  const { ctx, page, side, frame } = await desk("owner", "/settings/about");
  const search = side.getByRole("searchbox", { name: "Einstellung suchen" });
  const hits = () =>
    side
      .locator(".items .item.result")
      .evaluateAll((items) =>
        items.map((item) => item.innerText.replace(/\s+/g, " ").trim()),
      );
  for (const [query, top, picture] of [
    ["Cluster", "Cluster Cluster", "04-suche-cluster"],
    [
      "Sprache",
      "Sprache Darstellung › Farben und Schrift › Sprache und Schrift",
      "05-suche-sprache",
    ],
    ["Tailscale", "Tailscale Netzwerk › Fernzugriff", "06-suche-tailscale"],
  ]) {
    await search.fill(query);
    await side.locator(".items .item.result").first().waitFor();
    const found = await hits();
    assert.equal(found[0], top, `${query}: ${found.slice(0, 4).join(" | ")}`);
    await shot(page, picture);
  }
  // What the owner did not find before.
  for (const [query, expected] of [
    ["Knoten", /Cluster/],
    ["mehrere NAS", /^Cluster/],
    ["eingeloggt", /Angemeldet bleiben/],
    ["Schriftgröße", /^Schriftgröße/],
    ["Kernel", /Kernel/],
    ["Akzent", /^Akzentfarbe/],
    ["Glocke", /Glocke/],
  ]) {
    await search.fill(query);
    await side.locator(".items .item.result").first().waitFor();
    assert.ok(
      (await hits()).some((h) => expected.test(h)),
      query,
    );
  }
  // A hit on another tab of the page shown: tab chosen, row marked, keyboard there.
  await search.fill("");
  await openArea(page, side, "System");
  await side.getByRole("button", { name: "Darstellung" }).click();
  await at(page, "/settings/appearance");
  await frame.getByRole("tab", { name: "Feinheiten und Effekte" }).click();
  await search.fill("Sprache");
  await side.locator(".items .item.result").first().click();
  await frame.locator(".srow.found").waitFor();
  assert.equal(
    await frame
      .getByRole("tab", { name: "Farben und Schrift" })
      .getAttribute("aria-selected"),
    "true",
  );
  assert.equal(
    (await frame.locator(".srow.found .srow-title").first().innerText()).trim(),
    "Sprache",
  );
  assert.equal(await search.inputValue(), "");
  await shot(page, "07-treffer-geoeffnet-sprache");
  // A part inside a closed row on another page: the row opens, the part is marked.
  await search.fill("Internet-Ausgang");
  await side.locator(".items .item.result").first().click();
  await frame.locator(".srow-sub.found").waitFor({ timeout: 10000 });
  assert.match(
    await frame.locator(".srow-sub.found .srow-title").first().innerText(),
    /Internet-Ausgang/,
  );
  assert.equal(
    await frame
      .getByRole("tab", { name: "Fernzugriff" })
      .getAttribute("aria-selected"),
    "true",
  );
  await shot(page, "08-treffer-geoeffnet-tailscale");
  // A row on a page whose row is closed: "Angemeldet bleiben" opens.
  await search.fill("eingeloggt");
  await side
    .locator(".items .item.result")
    .filter({ hasText: "Angemeldet bleiben" })
    .first()
    .click();
  await at(page, "/settings/access#access-stay");
  await frame.locator("#access-stay.found").waitFor();
  await sound(page, "search hit");
  // Nothing found says so.
  await search.fill("xyzzy");
  await side.getByText("Nichts gefunden").waitFor();
  await search.fill("");
  // The command palette (Strg+K) knows the same settings and leads there too.
  // The keyboard back on the desktop (a click on the account in the column).
  await side.locator(".side-account").click();
  await page.keyboard.press("Control+k");
  const palette = page.locator("#palette-results");
  await page.getByRole("combobox").waitFor();
  await page.keyboard.type("Schriftgröße");
  await palette
    .locator('[role="option"]')
    .filter({ hasText: "Sprache und Schrift" })
    .first()
    .waitFor();
  await page.keyboard.press("Enter");
  await at(
    page,
    "/settings/appearance?tab=colors#find=Sprache%20und%20Schrift|Schriftgr%C3%B6%C3%9Fe",
  );
  await frame
    .locator(".srow.found")
    .filter({ hasText: "Schriftgröße" })
    .waitFor({ timeout: 10000 });
  await ctx.close();
}

// --- 3. A normal account: only its pages; areas without one disappear ---
{
  const { ctx, page, side } = await desk("user", "/settings/appearance");
  await page.waitForTimeout(400);
  assert.deepEqual(await column(side), [
    "Übersicht",
    "System ▾",
    "  Über TitanOS",
    "  Darstellung",
    "Hardware und Energie",
    "Sicherheit und Konten",
    "Smart Home und KI",
  ]);
  const search = side.getByRole("searchbox", { name: "Einstellung suchen" });
  await search.fill("ssh");
  await side.locator(".items .item.result").first().waitFor();
  const found = await side.locator(".items .item.result").allInnerTexts();
  assert.ok(!found.some((h) => /Zugang/.test(h)), found.join(" | "));
  await search.fill("");
  await shot(page, "09-spalte-benutzer");
  await ctx.close();
}

// --- 4. The phone: "Alle Einstellungen" after the new areas, its search ----
for (const role of ["owner", "user"]) {
  const made = await create(role, "hell", 390, "de", {
    deviceScaleFactor: 2,
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
  });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  const { ctx, page } = made;
  await page.goto(base + "/settings");
  await page.locator(".home .sgroup").first().waitFor({ timeout: 20000 });
  await fonts(page);
  const headings = await page
    .locator(".groups .sgroup-title")
    .evaluateAll((h) => h.map((x) => x.textContent.trim()));
  assert.deepEqual(
    headings,
    role === "owner"
      ? [
          "System",
          "Hardware und Energie",
          "Netzwerk und Fernzugriff",
          "Sicherheit und Konten",
          "Benachrichtigungen",
          "Daten und Sicherung",
          "Cluster",
          "Smart Home und KI",
        ]
      : [
          "System",
          "Hardware und Energie",
          "Sicherheit und Konten",
          "Smart Home und KI",
        ],
  );
  await sound(page, `phone ${role}`);
  if (role === "owner") await shot(page, "10-handy-alle-einstellungen");
  // The ⋯ menu lists only the areas the account may open (it listed all).
  await page.locator('.phone-bar button[aria-haspopup="menu"]').click();
  const menu = page.locator('.phone-bar [role="menu"]');
  await menu.waitFor();
  const pages = await menu
    .locator('[role="menuitemradio"]')
    .evaluateAll((items) => items.map((i) => i.textContent.trim()));
  assert.deepEqual(
    pages.slice(0, role === "owner" ? 9 : 5),
    role === "owner"
      ? [
          "Übersicht",
          "System",
          "Hardware und Energie",
          "Netzwerk und Fernzugriff",
          "Sicherheit und Konten",
          "Benachrichtigungen",
          "Daten und Sicherung",
          "Cluster",
          "Smart Home und KI",
        ]
      : [
          "Übersicht",
          "System",
          "Hardware und Energie",
          "Sicherheit und Konten",
          "Smart Home und KI",
        ],
  );
  if (role === "owner") assert.equal(pages.length >= 9, true);
  else assert.equal(pages.length, 5, pages.join(" | "));
  if (role === "user") await shot(page, "13-handy-menue-benutzer");
  await page.keyboard.press("Escape");
  if (await menu.isVisible())
    await page.locator('.phone-bar button[aria-haspopup="menu"]').click();
  const search = page.locator(".home-search input[type=search]");
  await search.fill("Sprache");
  await page.locator(".groups .hit a.srow").first().waitFor();
  assert.match(
    await page.locator(".groups .hit a.srow").first().innerText(),
    /^Sprache/,
  );
  await sound(page, `phone search ${role}`);
  if (role === "owner") await shot(page, "11-handy-suche-sprache");
  await page.locator(".groups .hit a.srow").first().click();
  await page.waitForURL("**/settings/appearance?tab=colors#find=*");
  await page.locator(".srow.found").waitFor();
  if (role === "owner") await shot(page, "12-handy-treffer-sprache");
  // "?area=" scrolls the list to its area.
  await page.goto(base + "/settings?area=data");
  if (role === "owner") {
    await page.locator("#settings-home-group-data").waitFor();
    await page.waitForTimeout(300);
    const top = await page
      .locator("#settings-home-group-data")
      .evaluate((el) => el.getBoundingClientRect().top);
    assert.ok(top < 200, `area heading at ${top}`);
  }
  await ctx.close();
}

await browser.close();
const real = errors.filter(
  (e) => !/Failed to load resource|favicon|ResizeObserver/.test(String(e)),
);
assert.deepEqual(real, [], "browser errors");
console.log("settings navigation and search: ok");
