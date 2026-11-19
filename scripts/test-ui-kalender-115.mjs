// 1.15 (PLAN 115-5, drafts 33–39 in Entwuerfe/115-mail-handy, owner
// 19 Nov 2026 "wie Apple Kalender", docs/KALENDER.md "1.15"): Kalender
// with isolated data (test-ui-kalender-fixtures.mjs; the clock fixed at
// Tuesday, 17 November 2026, 12:29 in Berlin).
// Sizes of a window (a desktop window is a page of its own): 1440×900,
// 900×700, 600×800, 420×520 and the phone 390×844, light and dark – the
// month (wide: the weeks fill the height, nothing runs out of a week,
// what does not fit is "+N weitere"; compact under 620 px: the months one
// after the other with bars, "‹ 2026" and the pills), the day (week strip,
// "Dienstag – 17. Nov. 2026", "Ganztägig" with chips that wrap), the week;
// no sideways scroll and no cut text anywhere. Today's red: white on the
// circle and the red numbers on the background reach 4.5:1. The line of
// now: red with its time, the full hour beside it steps aside. The phone:
// a tap on a day opens it with movement (Animationen an), without it under
// reduced motion; a swipe goes to the next day and week; "‹ November" goes
// back; the year and a month of it; the search; "Alle ausblenden" in one
// request; draft 33 "Neues Ereignis" (Abbrechen · Neues Ereignis ·
// Hinzufügen, calendar and alerts in sheets, the request), editing (Fertig,
// "Ereignis löschen"), draft 35 the event's page ("‹ Di., 17. Nov.",
// "Bearbeiten", "Ereignis löschen", no Schließen); the KI-Agent in the ⋯ menu of the head (opens it; off without
// the agent). One window resized while open (wide, lower, compact, wide
// again). Once in a real desktop window. Screenshots to UI_SHOTS (the
// Baustand: Entwuerfe/115-mail-handy/bau/kalender-*.png) and UI_OUTPUT.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5191 node scripts/test-ui-kalender-115.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import { calendarState, installCalendar } from "./test-ui-kalender-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");

const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5191";
const out = process.env.UI_OUTPUT || "out/validation/kalender-115";
const shots = process.env.UI_SHOTS || out;
await mkdir(out, { recursive: true });
await mkdir(shots, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const NOW = new Date("2026-11-17T12:29:00+01:00");

async function open({
  width = 1440,
  height = 900,
  theme = "hell",
  phone = false,
  desk = false,
  now = NOW,
  reduce = false,
} = {}) {
  const made = await create(
    "user",
    theme,
    width,
    "de",
    phone
      ? {
          isMobile: true,
          hasTouch: true,
          deviceScaleFactor: 2,
          viewport: { width, height },
          timezoneId: "Europe/Berlin",
          reducedMotion: reduce ? "reduce" : "no-preference",
        }
      : {
          viewport: { width, height },
          timezoneId: "Europe/Berlin",
          reducedMotion: reduce ? "reduce" : "no-preference",
        },
  );
  await made.ctx.clock.setFixedTime(now);
  if (phone || desk) await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  const state = calendarState();
  await installCalendar(made.ctx, state);
  return { ...made, state };
}
async function sound(target, name) {
  const overflow = await target.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `${name}: scrolls sideways by ${overflow}px`);
  assert.deepEqual(await target.evaluate(cutTextIn), [], `${name}: cut text`);
}
const shot = (page, name) => page.screenshot({ path: `${out}/${name}.png`, animations: "disabled" });
const keep = (page, name) =>
  page.screenshot({ path: `${shots}/kalender-${name}.png`, animations: "disabled" });
async function until(fn, what, ms = 8000) {
  const end = Date.now() + ms;
  while (!(await fn())) {
    if (Date.now() > end) throw new Error(`timed out: ${what}`);
    await new Promise((r) => setTimeout(r, 100));
  }
}
async function ready(page, view) {
  await page.locator(`[data-pim-ready][data-view="${view}"]`).waitFor();
  await page.locator(".ev, .blk, .arow, .month .bar, .ym").first().waitFor();
  await page.waitForTimeout(400);
  // 1.15: a movement that is still running (a day opening) is over first.
  await page.waitForFunction(() =>
    document
      .getAnimations()
      .every((a) => a.playState !== "running" || a.effect?.getTiming().iterations === Infinity),
  );
}
const sent = (state, method, re) => state.requests.filter(([m, p]) => m === method && re.test(p));
/** The contrast of an element's text on a background color (both computed). */
const contrast = (page, selector, bgOf = "self") =>
  page.locator(selector).first().evaluate((el, bgOf) => {
    const rgb = (c) => (c.match(/[\d.]+/g) ?? []).slice(0, 4).map(Number);
    const lum = ([r, g, b]) => {
      const f = (v) => ((v /= 255) <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
      return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
    };
    const fg = rgb(getComputedStyle(el).color);
    let node =
      bgOf === "self" ? el : bgOf === "parent" ? el.parentElement : document.querySelector(bgOf);
    let bg = null;
    while (node) {
      const c = rgb(getComputedStyle(node).backgroundColor);
      if (c.length >= 3 && (c.length < 4 || c[3] > 0.5)) {
        bg = c;
        break;
      }
      node = node.parentElement;
    }
    bg ??= rgb(getComputedStyle(document.body).backgroundColor);
    const [a, b] = [lum(fg), lum(bg)].sort((x, y) => y - x);
    return (a + 0.05) / (b + 0.05);
  }, bgOf);
/** Touch swipe by dx over the middle of an element (CDP: a real finger). */
async function swipe(page, selector, dx) {
  const box = await page.locator(selector).first().boundingBox();
  const cdp = await page.context().newCDPSession(page);
  const x = box.x + box.width / 2 + (dx < 0 ? 60 : -60);
  const y = box.y + Math.min(box.height / 2, 120);
  await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x, y }] });
  for (let i = 1; i <= 10; i++)
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchMove",
      touchPoints: [{ x: x + (dx * i) / 10, y }],
    });
  await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
  await cdp.detach();
}

try {
  // ---- Every size, light and dark: month, day, week ----
  const sizes = [
    [1440, 900, false],
    [900, 700, false],
    [600, 800, false],
    [420, 520, false],
    [390, 844, true],
  ];
  for (const theme of ["hell", "graphit"]) {
    for (const [width, height, phone] of sizes) {
      const name = `${phone ? "handy" : width + "x" + height}-${theme}`;
      const compact = phone || width < 620;
      const { page, ctx } = await open({ width, height, theme, phone });
      await page.goto(base + "/calendar");
      await ready(page, "month");
      await sound(page, `month ${name}`);
      if (compact) {
        // The pills: "‹ 2026" on top with views · search · new, "Heute" and two below.
        assert.match(await page.locator(".pills.top .back-pill").innerText(), /2026/, name);
        assert.match(await page.locator(".pills.bottom").innerText(), /Heute/, name);
        assert.equal(await page.locator(".cf-bar").count(), 0, `${name}: no bar`);
        // The months one after the other, bars without titles.
        assert.equal(await page.locator(".month .ev").count(), 0, `${name}: no titles`);
        assert.ok((await page.locator(".month .bar").count()) > 10, `${name}: bars`);
        assert.ok((await page.locator(".month .more").count()) > 0, `${name}: +N`);
        await keep(page, `monat-${name}`);
        await page.locator(".months").evaluate((el) => (el.scrollTop += 900));
        await until(
          async () => (await page.locator('.mname:text("Dezember")').count()) > 0,
          "December follows",
        );
        await page.waitForTimeout(300);
        await sound(page, `month scrolled ${name}`);
        // Nothing under the floating pills at rest: the last row has room.
        const room = await page.locator(".months .room").evaluate((el) => el.offsetHeight);
        assert.ok(room >= 70, `${name}: room under the pills`);
        // Today: white on a red circle.
        assert.ok((await contrast(page, '.pick.today .n')) >= 4.5, `${name}: today's circle`);
      } else {
        assert.equal(await page.locator(".cf-bar").count(), 1, `${name}: one bar`);
        const fill = await page.evaluate(() => {
          const m = document.querySelector(".month").getBoundingClientRect();
          const c = document.querySelector(".col-cal-main").getBoundingClientRect();
          return Math.abs(c.bottom - m.bottom);
        });
        assert.ok(fill < 4, `${name}: the month fills the window (${fill}px)`);
        // Nothing runs out of a week: what does not fit is "+N weitere".
        const over = await page.evaluate(() =>
          [...document.querySelectorAll(".mweek")]
            .filter((w) => w.scrollHeight > w.clientHeight + 1)
            .map((w) => w.dataset.week),
        );
        assert.deepEqual(over, [], `${name}: weeks overflowing`);
        assert.ok((await contrast(page, ".cell.today .n")) >= 4.5, `${name}: today's circle`);
        if (width === 1440)
          assert.equal(
            await page.locator(".mweek .ev.long", { hasText: "Oma zu Besuch" }).count(),
            1,
            "a multi-day event is one bar",
          );
      }
      await shot(page, `monat-${name}`);
      if (width === 1440 || width === 900) await keep(page, `monat-${name}`);

      // The day with its strip and the all-day chips.
      await page.goto(base + "/calendar?view=day&date=2026-11-18");
      await ready(page, "day");
      assert.equal(await page.locator("[data-strip] .d").count(), 7, `${name}: week strip`);
      assert.equal(
        await page.locator("[data-strip] .d.on").getAttribute("aria-label"),
        "Mittwoch, 18. November",
      );
      assert.match(await page.locator(".dayline").innerText(), /Mittwoch – 18\. Nov\. 2026/);
      const lab = page.locator(".week.one .wk-all .lab");
      assert.equal((await lab.innerText()).trim(), "Ganztägig");
      assert.equal(
        await page.locator(".week.one .ad").evaluate((el) => getComputedStyle(el).flexWrap),
        "wrap",
        `${name}: the chips wrap`,
      );
      assert.match(await page.locator(".week.one .ad").innerText(), /Oma zu Besuch/);
      assert.ok(
        (await contrast(page, "[data-strip] .d.today .num", "parent")) >= 4.5,
        `${name}: today's red number`,
      );
      await sound(page, `day ${name}`);
      await shot(page, `tag-${name}`);
      if (phone || width === 1440) await keep(page, `tag-18-${name}`);
      // Today: the red line of now with its time.
      await page.goto(base + "/calendar?view=day");
      await ready(page, "day");
      assert.equal(await page.locator(".nowtime").innerText(), "12:29");
      assert.ok((await contrast(page, ".nowtime")) >= 4.5, `${name}: the time of now`);
      assert.ok(
        (await contrast(page, "[data-strip] .d.on.today .num", "[data-strip] .sel i")) >= 4.5,
        `${name}: chosen today`,
      );
      await sound(page, `today ${name}`);
      if (phone) await keep(page, `tag-heute-${name}`);

      if (!compact) {
        await page.goto(base + "/calendar?view=week");
        await ready(page, "week");
        assert.ok((await contrast(page, ".wh.today b")) >= 4.5, `${name}: today in the week`);
        await sound(page, `week ${name}`);
        await shot(page, `woche-${name}`);
        if (width === 1440) await keep(page, `woche-${name}`);
      } else {
        // The week is the day there.
        await page.goto(base + "/calendar?view=week");
        await ready(page, "day");
      }
      await page.goto(base + "/calendar?view=list");
      await ready(page, "list");
      await sound(page, `list ${name}`);
      await ctx.close();
    }
  }

  // ---- The hour beside the line of now steps aside ----
  {
    const { page, ctx } = await open({
      width: 390,
      height: 844,
      phone: true,
      theme: "graphit",
      now: new Date("2026-11-17T09:55:00+01:00"),
    });
    await page.goto(base + "/calendar?view=day");
    await ready(page, "day");
    assert.equal(await page.locator(".nowtime").innerText(), "09:55");
    const hidden = await page
      .locator(".hours span", { hasText: "10:00" })
      .evaluate((el) => getComputedStyle(el).visibility);
    assert.equal(hidden, "hidden", "10:00 steps aside");
    assert.equal(
      await page.locator(".hours span", { hasText: "09:00" }).evaluate((el) => getComputedStyle(el).visibility),
      "visible",
    );
    await sound(page, "now 09:55");
    await keep(page, "tag-jetzt-handy-graphit");
    await ctx.close();
  }

  // ---- The phone: movement, swipes, back, year, search, calendars ----
  for (const reduce of [false, true]) {
    const { page, ctx, state } = await open({ width: 390, height: 844, phone: true, theme: "graphit", reduce });
    await page.goto(base + "/calendar");
    await ready(page, "month");
    await page.evaluate(() => (document.documentElement.dataset.fxTransitions = "on"));
    await page.evaluate(() => {
      window.__moves = 0;
      const animate = Element.prototype.animate;
      Element.prototype.animate = function (...args) {
        window.__moves++;
        return animate.apply(this, args);
      };
    });
    await page.locator('.pick[data-day="2026-11-18"]').click();
    await ready(page, "day");
    const moves = await page.evaluate(() => window.__moves);
    if (reduce) assert.equal(moves, 0, "reduced motion: the day comes at once");
    else assert.ok(moves >= 2, `a tap moves the week up and the hours in (${moves})`);
    // Swipes: the next day (the hours), the next week (the strip).
    await swipe(page, ".wk-scroll", -200);
    await until(() => page.url().includes("date=2026-11-19"), "swipe to the next day");
    await swipe(page, "[data-strip]", -200);
    await until(() => page.url().includes("date=2026-11-26"), "swipe to the next week");
    await swipe(page, "[data-strip]", 200);
    await until(() => page.url().includes("date=2026-11-19"), "swipe back a week");
    await page.locator('[data-strip] .d[aria-label^="Freitag"]').click();
    await until(() => page.url().includes("date=2026-11-20"), "a day of the strip");
    if (reduce) {
      await ctx.close();
      continue;
    }
    // "‹ November": back to the months, the day's week in view.
    assert.match(await page.locator(".back-pill").innerText(), /November/);
    await page.locator(".back-pill").click();
    await ready(page, "month");
    await page.locator('.pick[data-day="2026-11-20"]').waitFor();
    assert.ok(await page.locator('.pick[data-day="2026-11-20"]').isVisible(), "the day's week in view");
    // The year and a month of it.
    await page.locator(".back-pill").click();
    await ready(page, "year");
    assert.equal(await page.locator(".ym").count(), 12);
    await sound(page, "year");
    await keep(page, "jahr-handy-graphit");
    await page.locator('.ym[data-month="2026-12"]').click();
    await ready(page, "month");
    await until(
      () =>
        page.evaluate(() => {
          const s = document.querySelector('.months [data-month="2026-12"]');
          const m = document.querySelector(".months");
          return s && Math.abs(s.getBoundingClientRect().top - m.getBoundingClientRect().top) < 80;
        }),
      "December at the top",
    );
    // "Heute" goes to today's month.
    await page.locator(".today-pill").click();
    await until(
      () =>
        page.evaluate(() => {
          const t = document.querySelector(".pick.today");
          const m = document.querySelector(".months").getBoundingClientRect();
          const r = t?.getBoundingClientRect();
          return r && r.top >= m.top && r.bottom <= m.bottom;
        }),
      "today in view",
    );
    // The search.
    await page.locator('.pills.top button[aria-label="Termine suchen"]').click();
    await page.locator('[data-pim-ready][data-view="search"] [data-cal-search] input').waitFor();
    await page.locator("[data-cal-search] input").fill("messe");
    await page.locator(".arow", { hasText: "Messe Köln" }).first().waitFor();
    await sound(page, "search");
    await keep(page, "suche-handy-graphit");
    await page.locator(".arow", { hasText: "Messe Köln" }).first().click();
    await page.locator("dialog[open] [data-event-detail]").waitFor();
    await page.waitForTimeout(400);
    await sound(page, "event detail");
    await keep(page, "ereignis-handy-graphit");
    await page.keyboard.press("Escape");
    // The calendars: hide all with one request, show them again.
    await page.goto(base + "/calendar");
    await ready(page, "month");
    await page.locator('.pills.bottom button[aria-label="Kalender"]').click();
    await page.locator('.calrow[data-calendar="privat"]').waitFor();
    await sound(page, "calendars");
    await keep(page, "kalender-auswahl-handy-graphit");
    const before = sent(state, "PUT", /^\/preferences\/calendar$/).length;
    await page.getByRole("button", { name: "Alle ausblenden" }).click();
    await until(() => sent(state, "PUT", /^\/preferences\/calendar$/).length === before + 1, "one request");
    const hid = sent(state, "PUT", /^\/preferences\/calendar$/).at(-1)[2].hidden;
    assert.ok(hid.includes("privat") && hid.includes("arbeit") && hid.includes("geburtstage"), "all hidden");
    assert.match(await page.locator('.calrow[data-calendar="privat"]').innerText(), /ausgeblendet/);
    await page.getByRole("button", { name: "Alle einblenden" }).click();
    await until(() => sent(state, "PUT", /^\/preferences\/calendar$/).length === before + 2, "shown again");
    assert.deepEqual(sent(state, "PUT", /^\/preferences\/calendar$/).at(-1)[2].hidden, []);
    await ctx.close();
  }

  // ---- Draft 33: "Neues Ereignis" as a sheet; draft 35: the event's page ----
  for (const theme of ["graphit", "hell"]) {
    const { page, ctx, state } = await open({ width: 390, height: 844, phone: true, theme });
    await page.goto(base + "/calendar/event?date=2026-11-19&time=10:00&back=/calendar");
    await page.locator("[data-event-form]").waitFor();
    const head = page.locator("[data-sheet-head]");
    assert.match(await head.innerText(), /Abbrechen\s*Neues Ereignis\s*Hinzufügen/);
    assert.equal(await page.getByRole("button", { name: "Speichern" }).count(), 0, "no Speichern");
    assert.equal(await page.getByRole("button", { name: "Verwerfen" }).count(), 0, "no Verwerfen");
    await page.locator("#t-title").fill("Zahnarzt");
    await page.locator("#t-place").fill("Praxis am Markt");
    // The calendar: a row with its value and an arrow, a sheet to choose.
    await page.getByRole("button", { name: /^Kalender/ }).click();
    await page.locator("dialog[open] .choice", { hasText: "Arbeit" }).click();
    await until(async () => (await page.locator("dialog[open]").count()) === 0, "the sheet closes");
    assert.match(await page.getByRole("button", { name: /^Kalender/ }).innerText(), /Arbeit/);
    // An alert: "Hinweis · Ohne ›", chosen in a sheet; then "Zweiter Hinweis".
    await page.getByRole("button", { name: /^Hinweis/ }).click();
    await page.locator("dialog[open]").waitFor();
    await page.waitForTimeout(400);
    await sound(page, `alert sheet ${theme}`);
    if (theme === "graphit") await keep(page, "neu-hinweis-handy-graphit");
    await page.locator("dialog[open] .choice", { hasText: "15 Minuten vorher" }).click();
    await page.getByRole("button", { name: /^Zweiter Hinweis/ }).waitFor();
    assert.match(await page.getByRole("button", { name: /^Hinweis/ }).innerText(), /15 Minuten vorher/);
    await page.locator("#t-title").focus();
    await page.locator("#t-title").blur();
    await sound(page, `new event ${theme}`);
    await keep(page, `neu-handy-${theme}`);
    await page.getByRole("button", { name: "Hinzufügen" }).click();
    await until(() => sent(state, "POST", /^\/calendar\/events$/).length === 1, "the new event is sent");
    const made = sent(state, "POST", /^\/calendar\/events$/)[0][2];
    assert.equal(made.calendar, "arbeit");
    assert.deepEqual(made.event.alarms, [15]);
    assert.equal(made.event.title, "Zahnarzt");

    // The event's page: back to its day, "Bearbeiten", the red row.
    await page.goto(base + "/calendar?view=day");
    await ready(page, "day");
    await page.locator(".blk", { hasText: "Elternabend" }).click();
    const sheet = page.locator("dialog[open].evpage");
    await sheet.waitFor();
    await until(async () => (await sheet.innerText()).includes("Bastelsachen"), "the notes");
    assert.match(await sheet.locator(".evtop").innerText(), /Di\., 17\. Nov\.\s*Bearbeiten/);
    assert.equal(await sheet.getByRole("button", { name: "Schließen" }).count(), 0, "no Schließen");
    await page.waitForTimeout(300);
    await sound(page, `event page ${theme}`);
    await keep(page, `ereignis-handy-${theme}`);
    await sheet.getByRole("button", { name: "Ereignis löschen" }).click();
    await page.getByText("Nur diesen Termin").waitFor();
    await page.keyboard.press("Escape");
    await until(async () => (await page.locator("dialog[open]").count()) === 0, "closed");
    await page.locator(".blk", { hasText: "Elternabend" }).click();
    await sheet.waitFor();
    await sheet.locator(".evtop .back").click();
    await until(async () => (await page.locator("dialog[open]").count()) === 0, "back closes it");
    await page.goto(base + "/calendar?view=day&date=2026-11-19");
    await ready(page, "day");
    await page.locator(".blk", { hasText: "Kino" }).click();
    await page.locator("dialog[open].evpage").getByRole("button", { name: "Bearbeiten" }).click();
    await page.waitForURL(/\/calendar\/event\?/);
    await page.locator("[data-event-form]").waitFor();
    assert.match(await head.innerText(), /Abbrechen\s*Ereignis bearbeiten\s*Fertig/);
    await page.locator(".delrow").scrollIntoViewIfNeeded();
    await sound(page, `edit event ${theme}`);
    if (theme === "graphit") await keep(page, "neu-bearbeiten-handy-graphit");
    await page.getByRole("button", { name: "Ereignis löschen" }).click();
    await page.getByText("Der Termin am", { exact: false }).first().waitFor();
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "Abbrechen" }).click();
    await page.waitForURL(/\/calendar(\?|$)/);
    await ctx.close();
  }

  // ---- The KI-Agent: in the ⋯ menu of the head, not among the pills ----
  {
    const { page, ctx } = await open({ width: 390, height: 844, phone: true, theme: "graphit" });
    await page.route("**/api/v1/services/state", (route) =>
      route.fulfill({
        json: { docker: true, vms: true, lxc: true, agent: true, calendar: true, contacts: true },
      }),
    );
    await page.goto(base + "/calendar");
    await ready(page, "month");
    assert.equal(
      await page.locator(".pills").getByRole("button", { name: "KI-Agent" }).count(),
      0,
      "no agent among the pills",
    );
    await page.locator('header.phone-bar button[aria-haspopup="menu"]').click();
    const item = page.locator('header.phone-bar [role="menu"]').getByText("KI-Agent", { exact: true });
    await item.waitFor();
    await sound(page, "⋯ menu");
    await keep(page, "agent-menue-handy-graphit");
    await item.click();
    await until(
      () => page.evaluate(() => localStorage.getItem("titanos.calendar.agentPanel") === "1"),
      "the agent opens from the ⋯ menu",
    );
    await page.locator("[role=dialog] .agent-panel").waitFor();
    await ctx.close();
  }
  {
    // Without the agent the entry is there but cannot be chosen.
    const { page, ctx } = await open({ width: 390, height: 844, phone: true, theme: "hell" });
    await page.goto(base + "/calendar");
    await ready(page, "month");
    await page.locator('header.phone-bar button[aria-haspopup="menu"]').click();
    const off = page
      .locator('header.phone-bar [role="menu"] [role^="menuitem"]')
      .filter({ hasText: "KI-Agent" });
    await off.waitFor();
    assert.ok(
      (await off.isDisabled()) || (await off.getAttribute("aria-disabled")) === "true",
      "no agent: the entry is off",
    );
    await ctx.close();
  }

  // ---- One window resized while open: wide → lower → compact → wide ----
  {
    const { page, ctx } = await open({ width: 1440, height: 900, theme: "graphit" });
    await page.goto(base + "/calendar");
    await ready(page, "month");
    for (const [w, h] of [
      [900, 600],
      [900, 900],
      [420, 520],
      [600, 800],
      [1440, 900],
    ]) {
      await page.setViewportSize({ width: w, height: h });
      await page.waitForTimeout(500);
      const name = `resized ${w}x${h}`;
      await sound(page, name);
      if (w >= 620) {
        assert.equal(await page.locator(".cf-bar").count(), 1, `${name}: the bar`);
        const over = await page.evaluate(() =>
          [...document.querySelectorAll(".mweek")].filter((x) => x.scrollHeight > x.clientHeight + 1).length,
        );
        assert.equal(over, 0, `${name}: weeks overflowing`);
        const fill = await page.evaluate(() => {
          const m = document.querySelector(".month").getBoundingClientRect();
          const c = document.querySelector(".col-cal-main").getBoundingClientRect();
          return Math.abs(c.bottom - m.bottom);
        });
        assert.ok(fill < 4, `${name}: the month follows the height (${fill}px)`);
      } else {
        await page.locator(".pills.bottom").waitFor();
        const view = await page.locator(".calview").boundingBox();
        assert.ok(Math.abs(view.y + view.height - h) < 2, `${name}: as high as the window`);
      }
    }
    await ctx.close();
  }

  // ---- Light phone: the pills and the day in the light theme ----
  {
    const { page, ctx } = await open({ width: 390, height: 844, phone: true, theme: "hell" });
    await page.goto(base + "/calendar?view=day&date=2026-11-19");
    await ready(page, "day");
    await sound(page, "phone day light");
    await keep(page, "tag-19-handy-hell");
    await ctx.close();
  }

  // ---- In a window of the real desktop ----
  {
    const { page, ctx } = await open({ desk: true, height: 1000 });
    await page.goto(base + "/calendar");
    await page.locator("section.win").first().waitFor();
    let frame = null;
    await until(() => {
      frame = page.frames().find((f) => f !== page.mainFrame() && f.url().includes("/calendar"));
      return !!frame;
    }, "the window");
    await frame.locator('[data-pim-ready][data-view="month"]').waitFor();
    await frame.locator(".ev").first().waitFor();
    await page.waitForTimeout(600);
    await sound(frame, "desktop window");
    await keep(page, "fenster-desktop-hell");
    await ctx.close();
  }

  assert.deepEqual(errors, [], "page errors");
  console.log(`Kalender 1.15: all checks passed, screenshots in ${out} and ${shots}`);
} finally {
  await browser.close();
}
