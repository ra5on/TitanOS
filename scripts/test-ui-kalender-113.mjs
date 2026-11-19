// 1.13 (Baustand steps 9–11, docs/KALENDER.md "1.13"): Kalender after the
// owner's decisions of 18 November 2026, with isolated data
// (test-ui-kalender-fixtures.mjs; the clock fixed at Tuesday,
// 17 November 2026, 12:29 in Berlin). Desktop 1440 and 800 px, light and
// dark, English once: one bar over the content, the month fills the frame,
// the week scrolls its hours on its own. Dragging with the mouse: an event
// to another time (the request carries the instant), its lower edge (the
// end), a series asks which events (this and the following), free space
// makes a new event (the event window gets the times), the keys move one
// (Alt+↓, Enter) and the status line says so, in the month an event goes
// to another day. Reminders: due ones in the month with a round box, ticked
// off where they stand; the list in the side bar adds one. A hint of a
// reminder of an event shows as a toast. Phone 390 px light and dark (1.15):
// the month with bars, a day opens with its hours, the levels "Kalender" and
// "Erinnerungen" with "Fertig", "Heute" in the pills at the bottom, "+" on top,
// the event window as a page of its own. Every view: no sideways scroll,
// no cut text. Screenshots to UI_OUTPUT (the Baustand's shots folder).
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5241 node scripts/test-ui-kalender-113.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import {
  calendarState,
  installCalendar,
} from "./test-ui-kalender-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5241";
const out = process.env.UI_OUTPUT || "out/validation/kalender-113";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const NOW = new Date("2026-11-17T12:29:00+01:00");

async function open({
  width = 1440,
  height = 900,
  theme = "hell",
  lang = "de",
  phone = false,
  change = () => {},
} = {}) {
  const made = await create(
    "user",
    theme,
    width,
    lang,
    phone
      ? {
          isMobile: true,
          hasTouch: true,
          deviceScaleFactor: 2,
          viewport: { width, height: 844 },
          timezoneId: "Europe/Berlin",
        }
      : { viewport: { width, height }, timezoneId: "Europe/Berlin" },
  );
  await made.ctx.clock.setFixedTime(NOW);
  const state = calendarState();
  change(state);
  await installCalendar(made.ctx, state);
  return { ...made, state };
}
async function sound(page, name) {
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `${name}: scrolls sideways by ${overflow}px`);
  assert.deepEqual(await page.evaluate(cutTextIn), [], `${name}: cut text`);
}
const shot = (page, name) =>
  page.screenshot({ path: `${out}/${name}.png`, animations: "disabled" });
async function until(fn, what, ms = 8000) {
  const end = Date.now() + ms;
  while (!(await fn())) {
    if (Date.now() > end) throw new Error(`timed out: ${what}`);
    await new Promise((r) => setTimeout(r, 100));
  }
}
async function ready(page, view) {
  await page.locator(`[data-pim-ready][data-view="${view}"]`).waitFor();
  await page.locator(".ev, .blk, .arow, .month .pick, .month .bar").first().waitFor();
  await page.waitForTimeout(400);
  // 1.15: a movement that is still running (a day opening) is over first.
  await page.waitForFunction(() =>
    document
      .getAnimations()
      .every((a) => a.playState !== "running" || a.effect?.getTiming().iterations === Infinity),
  );
}
const sent = (state, method, re) =>
  state.requests.filter(([m, p]) => m === method && re.test(p));
const dialog = (page) => page.locator("dialog[open]").last();
/** Drags with the mouse from the middle of a box by dx/dy (in steps). */
async function drag(page, box, dx, dy, from = 0.5) {
  const x = box.x + box.width / 2;
  const y = box.y + box.height * from;
  await page.mouse.move(x, y);
  await page.mouse.down();
  for (let i = 1; i <= 8; i++)
    await page.mouse.move(x + (dx * i) / 8, y + (dy * i) / 8);
  await page.mouse.up();
}
/** Buttons, fields and links of a part under 44 px (the phone). */
const small = (page, scope) =>
  page.evaluate((scope) => {
    const box = (el) => {
      if (el.matches("input[type=checkbox], input[type=radio]")) {
        const label = el.closest("label") ?? el.labels?.[0];
        return (label ?? el).getBoundingClientRect();
      }
      return el.getBoundingClientRect();
    };
    return [
      ...document.querySelectorAll(
        `${scope} button, ${scope} input, ${scope} a, ${scope} select, ${scope} summary`,
      ),
    ]
      .filter((el) => el.getClientRects().length && getComputedStyle(el).visibility === "visible")
      .map((el) => [el.textContent.trim() || el.getAttribute("aria-label") || el.type, box(el)])
      .filter(([, r]) => r.height < 44 || (r.width < 44 && r.width > 0))
      .map(([n, r]) => `${n} ${Math.round(r.width)}x${Math.round(r.height)}`);
  }, scope);
const status = (page) => page.locator('p[role="status"][aria-live]').innerText();

try {
  // ---- Desktop: layout in both themes and two widths ----
  for (const theme of ["hell", "graphit"]) {
    for (const width of [1440, 800]) {
      const { page, ctx } = await open({ theme, width });
      await page.goto(base + "/calendar");
      await ready(page, "month");
      // One bar over the content: navigation, the month, the views.
      const bar = page.locator(".cf-bar");
      assert.equal(await bar.count(), 1, "one bar");
      assert.match(await bar.innerText(), /Heute[\s\S]*November 2026[\s\S]*Monat/);
      assert.equal(await page.locator(".win-toolbar, .set-head").count(), 0, "no second head");
      // The month fills the frame.
      const fill = await page.evaluate(() => {
        const m = document.querySelector(".month").getBoundingClientRect();
        const c = document.querySelector(".col-cal-main").getBoundingClientRect();
        return c.bottom - m.bottom;
      });
      assert.ok(fill < 4, `${theme} ${width}: the month fills the frame (${fill}px left)`);
      // Reminders due in the month: their own shape and words (1.15: in the
      // day's stack, or behind its "+N weitere" when the row is full).
      if (width === 1440) {
        const chip = page.locator('.stack[data-stack="2026-11-12"] .task');
        assert.match(await chip.innerText(), /Handyvertrag/);
        assert.equal(await chip.locator(".tick").getAttribute("aria-pressed"), "false");
      } else
        assert.match(
          await page.locator('.stack[data-stack="2026-11-12"]').innerText(),
          /Handyvertrag|\+\d+ weitere/,
        );
      await sound(page, `month ${theme} ${width}`);
      await shot(page, `monat-${width}-${theme}`);

      await page.goto(base + "/calendar?view=week");
      await ready(page, "week");
      const scroll = await page.evaluate(() => {
        const el = document.querySelector(".wk-scroll");
        return { h: el.clientHeight, sh: el.scrollHeight, top: el.scrollTop };
      });
      assert.ok(scroll.sh > scroll.h && scroll.top > 0, "the hours scroll on their own");
      await sound(page, `week ${theme} ${width}`);
      await shot(page, `woche-${width}-${theme}`);
      if (width === 1440) {
        await page.goto(base + "/calendar?view=day");
        await ready(page, "day");
        await sound(page, `day ${theme}`);
        await shot(page, `tag-${theme}`);
        await page.goto(base + "/calendar?view=list");
        await ready(page, "list");
        await sound(page, `list ${theme}`);
        await shot(page, `liste-${theme}`);
      }
      await ctx.close();
    }
  }

  // ---- Dragging in the week ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/calendar?view=week");
    await ready(page, "week");
    // An event one hour later: the request carries the instant (Berlin → UTC).
    const lunch = page.locator(".blk", { hasText: "Mittagessen mit Anna" });
    await drag(page, await lunch.boundingBox(), 0, 52, 0.3);
    await until(() => sent(state, "PUT", /mittag/).length === 1, "moved");
    let body = sent(state, "PUT", /mittag/)[0][2];
    assert.equal(body.event.start, "2026-11-17T12:30:00Z");
    assert.equal(body.event.end, "2026-11-17T13:15:00Z");
    assert.equal(body.scope, "all");
    assert.equal(body.event.timezone, "");
    await until(async () => /verschoben/.test(await status(page)), "said");

    // Its lower edge: 30 minutes longer.
    const parcel = page.locator(".blk", { hasText: "Paket bei der Post" });
    const pb = await parcel.boundingBox();
    await page.mouse.move(pb.x + pb.width / 2, pb.y + pb.height - 3);
    await page.mouse.down();
    for (let i = 1; i <= 6; i++)
      await page.mouse.move(pb.x + pb.width / 2, pb.y + pb.height - 3 + (26 * i) / 6);
    await page.mouse.up();
    await until(() => sent(state, "PUT", /paket/).length === 1, "longer");
    body = sent(state, "PUT", /paket/)[0][2];
    assert.equal(body.event.start, "2026-11-16T16:30:00Z");
    assert.equal(body.event.end, "2026-11-16T17:30:00Z");

    // A series asks which events; "this and the following" is sent with the occurrence.
    const ride = page
      .locator('.wk-grid .col[data-day="2026-11-18"] .blk', { hasText: "Fahrgemeinschaft" });
    await drag(page, await ride.boundingBox(), 0, 52, 0.3);
    const ask = dialog(page);
    await ask.getByText("Diesen und alle folgenden").waitFor();
    assert.match(await ask.innerText(), /Nur diesen Termin[\s\S]*Die ganze Reihe/);
    await shot(page, "serie-verschieben");
    await ask.getByText("Diesen und alle folgenden").click();
    await ask.getByRole("button", { name: "Verschieben" }).click();
    await until(() => sent(state, "PUT", /fahrgemeinschaft/).length === 1, "series moved");
    body = sent(state, "PUT", /fahrgemeinschaft/)[0][2];
    assert.equal(body.scope, "following");
    assert.equal(body.occurrence, "20261118T064500Z");
    assert.equal(body.event.start, "2026-11-18T07:45:00Z");

    // The keys: Alt+↓ twice, Enter.
    await page.waitForTimeout(1000); // the series read again after its move
    const dentist = page.locator(".blk", { hasText: "Zahnarzt" });
    await dentist.focus();
    await page.keyboard.press("Alt+ArrowDown");
    await page.keyboard.press("Alt+ArrowDown");
    assert.match(await status(page), /16:30–17:00[\s\S]*Eingabetaste/);
    assert.equal(await page.locator(".ghost").count(), 1, "the place to come is shown");
    await page.keyboard.press("Enter");
    await until(() => sent(state, "PUT", /zahnarzt/).length === 1, "moved by keys");
    assert.equal(sent(state, "PUT", /zahnarzt/)[0][2].event.start, "2026-11-17T15:30:00Z");

    // Free space: a new event of that time in the event window.
    const thu = page.locator('.wk-grid .col[data-day="2026-11-20"]');
    const tb = await thu.boundingBox();
    const scrollTop = await page.evaluate(() => document.querySelector(".wk-scroll").scrollTop);
    const gridTop = await page.evaluate(() => document.querySelector(".wk-grid").getBoundingClientRect().top);
    const yOf = (minutes) => gridTop + (minutes / 60) * 52 - 0 + 1 + 0 * scrollTop;
    await page.mouse.move(tb.x + 10, yOf(14 * 60) + 2);
    await page.mouse.down();
    for (let i = 1; i <= 6; i++) await page.mouse.move(tb.x + 10, yOf(14 * 60) + 2 + (78 * i) / 6);
    await page.mouse.up();
    await page.waitForURL(/\/calendar\/event\?/);
    const url = new URL(page.url());
    assert.equal(url.searchParams.get("date"), "2026-11-20");
    assert.equal(url.searchParams.get("time"), "14:00");
    assert.equal(url.searchParams.get("end"), "15:30");
    await page.locator("[data-event-form]").waitFor();
    assert.equal(await page.locator("#t-from").inputValue(), "2026-11-20");
    assert.equal(await page.locator('input[aria-label="Ende Uhrzeit"]').inputValue(), "15:30");
    await ctx.close();
  }

  // ---- The month: an event onto another day; reminders ticked off, added ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/calendar");
    await ready(page, "month");
    const tyre = page.locator('.ev[data-start="2026-11-05"]', { hasText: "Reifenwechsel" });
    const target = await page.locator('.cell[data-day="2026-11-06"]').boundingBox();
    const tb = await tyre.boundingBox();
    await drag(page, tb, target.x + target.width / 2 - (tb.x + tb.width / 2), target.y + target.height / 2 - (tb.y + tb.height / 2));
    await until(() => sent(state, "PUT", /reifen/).length === 1, "moved a day");
    const body = sent(state, "PUT", /reifen/)[0][2];
    assert.equal(body.event.start, "2026-11-06T07:00:00Z");
    assert.equal(body.event.end, "2026-11-06T08:00:00Z");

    await page.locator('.stack[data-stack="2026-11-12"] .task .tick').click();
    await until(() => sent(state, "POST", /vertrag\.ics\/done$/).length === 1, "ticked off");
    assert.equal(sent(state, "POST", /vertrag\.ics\/done$/)[0][2].done, true);
    // The side bar's list: a new reminder with Enter.
    const side = page.locator("[data-tasks]");
    assert.match(await side.innerText(), /Erinnerungen[\s\S]*Einkauf/);
    await side.locator(".add input").fill("Fenster putzen");
    await side.locator(".add input").press("Enter");
    await until(() => sent(state, "POST", /^\/calendar\/tasks$/).length === 1, "added");
    assert.equal(sent(state, "POST", /^\/calendar\/tasks$/)[0][2].title, "Fenster putzen");
    // A reminder opens to be changed, deleting asks in its own dialog.
    await side.locator(".task", { hasText: "Buch zurückgeben" }).locator(".tt").click();
    await dialog(page).locator("[data-task-dialog]").waitFor();
    await sound(page, "task dialog");
    await shot(page, "erinnerung-bearbeiten");
    await dialog(page).getByRole("button", { name: "Löschen …" }).click();
    await dialog(page).getByText("Erinnerung löschen", { exact: true }).waitFor();
    await dialog(page).getByRole("button", { name: "Erinnerung löschen" }).click();
    await until(() => sent(state, "DELETE", /buch\.ics/).length === 1, "deleted");
    await ctx.close();
  }

  // ---- A reminder of an event as a hint in TitanOS ----
  {
    const { page, ctx } = await open();
    let latest = "";
    await page.route("**/api/v1/calendar/reminders", (route) =>
      route.fulfill({
        json: {
          epoch: "1",
          reminders: [
            {
              seq: 1,
              at: NOW.toISOString(),
              calendar: "familie",
              id: "x.ics",
              occurrence: "",
              title: "Elternabend Klasse 3b",
              start: "2026-11-17T19:00:00+01:00",
              allDay: false,
              minutes: 60,
            },
          ],
        },
      }),
    );
    // The shared fixtures answer /system; once their answer is known, the
    // newest hint is added to it.
    let system = null;
    page.on("response", async (r) => {
      if (!system && new URL(r.url()).pathname === "/api/v1/system")
        system = await r.json().catch(() => null);
    });
    await page.route("**/api/v1/system", (route) =>
      latest && system
        ? route.fulfill({ json: { ...system, remindersLatest: latest } })
        : route.fallback(),
    );
    await page.goto(base + "/calendar");
    await ready(page, "month");
    latest = "1-1";
    const toast = page.getByText(/Erinnerung: Elternabend Klasse 3b/);
    await toast.waitFor({ timeout: 10000 });
    await shot(page, "hinweis");
    await ctx.close();
  }

  // ---- English once ----
  {
    const { page, ctx } = await open({ lang: "en" });
    await page.goto(base + "/calendar");
    await ready(page, "month");
    assert.match(await page.locator(".cf-bar").innerText(), /Today[\s\S]*November 2026[\s\S]*Month/);
    assert.match(await page.locator("[data-tasks]").innerText(), /Reminders/);
    await sound(page, "month en");
    await shot(page, "monat-en");
    await ctx.close();
  }

  // ---- Phone 390: levels, the floating pills (1.15) ----
  for (const theme of ["hell", "graphit"]) {
    const { page, ctx } = await open({ width: 390, phone: true, theme });
    await page.goto(base + "/calendar");
    await ready(page, "month");
    assert.equal(await page.locator(".month .ev").count(), 0, "no titles in the cells");
    assert.ok((await page.locator(".month .bar").count()) > 20, "bars");
    const bottom = page.locator('.pills.bottom[role="toolbar"]');
    assert.match(await bottom.innerText(), /Heute/);
    assert.equal(await page.locator('.pills.top button[aria-label="Neuer Termin"]').count(), 1, "+ on top");
    await sound(page, `phone month ${theme}`);
    assert.deepEqual(await small(page, ".pills.bottom"), [], `phone pills ${theme}: under 44 px`);
    assert.deepEqual(await small(page, ".pills.top"), [], `phone top ${theme}: under 44 px`);
    await shot(page, `handy-monat-${theme}`);
    // A day opens with its hours.
    await page.locator('.pick[data-day="2026-11-19"]').click();
    await ready(page, "day");
    assert.match(await page.locator(".wk-grid").innerText(), /Messe Köln/, "the chosen day");
    await page.locator(".back-pill").click();
    await ready(page, "month");

    await bottom.getByRole("button", { name: "Kalender", exact: true }).click();
    await page.locator('.calrow[data-calendar="privat"]').waitFor();
    assert.equal(await page.locator(".sort-grip").count(), 0, "no dragging on the phone");
    await sound(page, `phone calendars ${theme}`);
    await page.locator("[data-reminder-settings] summary").click();
    assert.deepEqual(await small(page, ".col-cal-side"), [], `phone calendars ${theme}: under 44 px`);
    await shot(page, `handy-kalender-${theme}`);
    await page.getByRole("button", { name: "Fertig" }).first().click();
    await page.waitForURL((u) => !u.searchParams.has("level"));
    await ready(page, "month");

    await bottom.getByRole("button", { name: "Erinnerungen" }).click();
    await page.locator("[data-tasks]").waitFor();
    await sound(page, `phone reminders ${theme}`);
    assert.deepEqual(await small(page, ".col-cal-tasks"), [], `phone reminders ${theme}: under 44 px`);
    await shot(page, `handy-erinnerungen-${theme}`);
    await page.getByRole("button", { name: "Fertig" }).first().click();
    await page.waitForURL((u) => !u.searchParams.has("level"));
    await ready(page, "month");

    await page.locator(".pills.top .dropdown > .btn").click();
    await page.getByRole("menuitemradio", { name: "Liste" }).click();
    await ready(page, "list");
    await sound(page, `phone list ${theme}`);
    await shot(page, `handy-liste-${theme}`);

    await page.locator('.pills.top button[aria-label="Neuer Termin"]').click();
    await page.waitForURL(/\/calendar\/event/);
    await page.locator("[data-event-form]").waitFor();
    await sound(page, `phone event ${theme}`);
    await shot(page, `handy-termin-${theme}`);
    await ctx.close();
  }

  assert.deepEqual(errors, [], "page errors");
  console.log(`Kalender 1.13: all checks passed, screenshots in ${out}`);
} finally {
  await browser.close();
}
