// 1.13 (Baustand step 6/7, Kontakte in the browser): what a usual round in
// Kontakte costs with a large address book – open Kontakte at 1440 px with
// 2000 contacts (MEASURE_CONTACTS), search by typing "beisp" (a key every
// 60 ms), clear the search, open a contact, change its note and save. On
// isolated data (test-ui-kontakte-fixtures.mjs), nothing reaches a NAS.
// Prints requests per kind, times (ms) and the rows in the DOM as JSON;
// MEASURE_OUT writes them to a file.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5213 node scripts/test-ui-measure-kontakte.mjs
import { writeFile } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
import { calendarState, installCalendar } from "./test-ui-kalender-fixtures.mjs";
import { contactsState, installContacts } from "./test-ui-kontakte-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");

const base = process.env.UI_URL || "http://127.0.0.1:5213";
const COUNT = Number(process.env.MEASURE_CONTACTS || 2000);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const create = fixtureFactory(browser, errors);

const FIRST = ["Anna", "Ben", "Carla", "Dennis", "Eva", "Felix", "Greta", "Hans", "Ida", "Jonas", "Klara", "Lukas", "Mia", "Noah", "Olga", "Paul"];
const LAST = ["Müller", "Schmidt", "Schneider", "Fischer", "Weber", "Meyer", "Wagner", "Becker", "Schulz", "Hoffmann", "Koch", "Richter", "Klein", "Wolf", "Neumann", "Beispiel"];

/** COUNT contacts: names, a number, every third an e-mail address, every fifth a picture. */
function many(state) {
  for (let i = 0; i < COUNT; i++) {
    const first = FIRST[i % FIRST.length];
    const last = `${LAST[Math.floor(i / FIRST.length) % LAST.length]}${i >= 256 ? ` ${i}` : ""}`;
    state.contacts.push({
      id: `m${i}.vcf`,
      book: "kontakte",
      etag: `"1-m${i}.vcf"`,
      first,
      last,
      org: i % 7 === 0 ? `Firma ${i} GmbH` : "",
      phones: [{ type: "mobile", value: `+49 170 ${String(1000000 + i)}` }],
      emails: i % 3 === 0 ? [{ type: "home", value: `${first}.${i}@mail.example`.toLowerCase() }] : [],
      addresses: [],
      birthday: "",
      notes: "",
      hasPhoto: i % 5 === 0,
      modified: "2026-11-03T18:12:00+01:00",
    });
  }
}

const KINDS = [
  ["list", "GET", /^\/contacts$/],
  ["search", "GET", /^\/contacts\?q=/],
  ["suggestions", "GET", /^\/contacts\/suggestions$/],
  ["suggestionsSearch", "GET", /^\/contacts\/suggestions\?q=/],
  ["searchOne", "GET", /^\/contacts\/search\?q=/],
  ["overview", "GET", /^\/contacts\/overview$/],
  ["books", "GET", /^\/contacts\/books$/],
  ["groups", "GET", /^\/contacts\/groups/],
  ["duplicates", "GET", /^\/contacts\/duplicates/],
  ["contact", "GET", /^\/contacts\/kontakte\/[^/]+$/],
  ["photo", "GET", /\/photo(\?|$)/],
  ["update", "PUT", /^\/contacts\/kontakte\/[^/]+$/],
  ["calendarAccess", "GET", /^\/calendar\/access$/],
];

async function until(fn, what, ms = 20000) {
  const end = Date.now() + ms;
  while (!(await fn())) {
    if (Date.now() > end) throw new Error(`timed out: ${what}`);
    await new Promise((r) => setTimeout(r, 20));
  }
}

try {
  const { page, ctx } = await create("user", "hell", 1440, "de", {
    viewport: { width: 1440, height: 1000 },
    timezoneId: "Europe/Berlin",
  });
  await installCalendar(ctx, calendarState());
  const state = contactsState();
  many(state);
  await installContacts(ctx, state);
  const seen = [];
  page.on("request", (req) => {
    const url = new URL(req.url());
    if (!url.pathname.startsWith("/api/v1/")) return;
    seen.push([req.method(), url.pathname.slice(7) + url.search]);
  });
  const count = (from) => {
    const out = {};
    for (const [kind, method, re] of KINDS)
      out[kind] = seen.slice(from).filter(([m, p]) => m === method && re.test(p)).length;
    return out;
  };
  const rows = () => page.locator("[data-contact-list] .crow").count();

  // Chromium's own counters (layout and style time of the page).
  const cdp = await ctx.newCDPSession(page);
  await cdp.send("Performance.enable");
  const metrics = async () => {
    const { metrics: m } = await cdp.send("Performance.getMetrics");
    const get = (n) => m.find((x) => x.name === n)?.value ?? 0;
    return {
      layout: Math.round(get("LayoutDuration") * 1000),
      style: Math.round(get("RecalcStyleDuration") * 1000),
      script: Math.round(get("ScriptDuration") * 1000),
    };
  };
  // 1. Open with the large book.
  const t0 = Date.now();
  await page.goto(base + "/contacts");
  await until(async () => (await rows()) > 0, "the list");
  const openMs = Date.now() - t0;
  await page.waitForTimeout(1500);
  const open = count(0);
  const afterOpen = await metrics();
  const dom = await page.evaluate(() => ({
    rows: document.querySelectorAll("[data-contact-list] .crow").length,
    nodes: document.querySelectorAll("*").length,
  }));
  // Scrolling the list to the end and back: the time of the frames.
  const scrollMs = await page.evaluate(async () => {
    const list = document.querySelector("[data-contact-list]");
    let el = list;
    while (el && el.scrollHeight <= el.clientHeight) el = el.parentElement;
    if (!el) return 0;
    const start = performance.now();
    for (let i = 0; i < 20; i++) {
      el.scrollTop = (el.scrollHeight * i) / 20;
      await new Promise((r) => requestAnimationFrame(() => r(null)));
    }
    el.scrollTop = 0;
    return Math.round(performance.now() - start);
  });
  const afterScroll = await metrics();

  // 2. Search by typing, then clear it.
  let from = seen.length;
  const field = page.getByRole("searchbox").first();
  const t1 = Date.now();
  await field.click();
  await field.pressSequentially("beisp", { delay: 60 });
  await until(
    async () => (await page.locator("[data-contact-list] mark").count()) > 0,
    "the hits",
  );
  const searchMs = Date.now() - t1;
  await page.waitForTimeout(800);
  const search = count(from);
  await field.fill("");
  await page.waitForTimeout(800);

  // 3. Open a contact.
  from = seen.length;
  await page.locator("[data-contact-list] .crow").nth(3).click();
  await page.locator("[data-contact-detail]").waitFor();
  await page.waitForTimeout(500);
  const opened = count(from);
  const id = await page.locator("[data-contact-detail]").getAttribute("data-contact-detail");

  // 4. Change the note and save.
  await page.goto(base + `/contacts?id=${encodeURIComponent(id)}&edit=1`);
  await page.locator("[data-contact-editor]").waitFor();
  await page.waitForTimeout(800);
  from = seen.length;
  await page.locator("[data-contact-editor] textarea.note").fill("Neue Notiz");
  const t2 = Date.now();
  await page.getByRole("button", { name: /^(Speichern|Save)$/ }).first().click();
  await page.locator("[data-contact-detail]").waitFor();
  const saveMs = Date.now() - t2;
  await page.waitForTimeout(1000);
  const saved = count(from);

  const result = {
    contacts: COUNT + contactsState().contacts.length,
    ms: { open: openMs, search: searchMs, scroll20Frames: scrollMs, save: saveMs },
    chromeMs: {
      open: afterOpen,
      scroll: {
        layout: afterScroll.layout - afterOpen.layout,
        style: afterScroll.style - afterOpen.style,
        script: afterScroll.script - afterOpen.script,
      },
    },
    dom,
    requests: { open, search, opened, saved },
    errors: errors.filter((e) => !/503/.test(e)),
  };
  const text = JSON.stringify(result, null, 2);
  console.log(text);
  if (process.env.MEASURE_OUT) await writeFile(process.env.MEASURE_OUT, text + "\n");
} finally {
  await browser.close();
}
