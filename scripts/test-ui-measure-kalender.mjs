// 1.13 (Baustand steps 9 and 13, Kalender's requests and time): how many
// requests a usual round in Kalender makes and how long the month takes
// to show. Isolated data (test-ui-kalender-fixtures.mjs, the clock fixed at
// Tuesday, 17 November 2026, 12:29 in Berlin), nothing reaches a NAS.
// The fixtures are made heavier than the drafts: 30 more events of this
// month have titles longer than titand's list keeps (clipped).
// Round: open the month (1440 × 900), the week, hide and show "Arbeit",
// a change announced by another window of this browser (the event window
// saved), one change of the live stream (an iPhone changed "privat"),
// next week, back to the month. Every answer of the calendar API waits
// 50 ms (MEASURE_DELAY). The week's layout work is read from Chrome
// (Performance.getMetrics: layouts and style recalculations while the
// week opens and while the window is resized). Prints JSON (requests per kind, time to
// the grid as median of 5 openings); MEASURE_OUT writes it to a file.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5211 node scripts/test-ui-measure-kalender.mjs
import { writeFile } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
import { calendarState, installCalendar } from "./test-ui-kalender-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

const base = process.env.UI_URL || "http://127.0.0.1:5211";
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const create = fixtureFactory(browser, errors);
const NOW = new Date("2026-11-17T12:29:00+01:00");
// Each answer of the calendar API waits this long (a NAS on the LAN with a
// disk that is awake); the same before and after.
const DELAY = Number(process.env.MEASURE_DELAY || 50);

const KINDS = [
  ["access", "GET", /^\/calendar\/access$/],
  ["calendars", "GET", /^\/calendar\/calendars$/],
  ["events", "GET", /^\/calendar\/events\?/],
  ["event", "GET", /^\/calendar\/events\/[^/?]+\/[^/?]+/],
  ["hide", "PUT", /^\/preferences\/calendar$/],
  ["stream", "GET", /^\/calendar\/stream/],
];

function heavier(state) {
  // 30 more events this month with clipped titles (2 a day from the 2nd).
  for (let i = 0; i < 30; i++) {
    const day = `2026-11-${String(2 + Math.floor(i / 2)).padStart(2, "0")}`;
    const id = `lang-${i}.ics`;
    const from = i % 2 ? "14:00" : "15:30";
    const to = i % 2 ? "15:00" : "16:30";
    state.events.push({
      kind: "event",
      id,
      occurrence: "",
      calendar: i % 3 ? "privat" : "arbeit",
      uid: `lang-${i}`,
      title: `Langer Titel ${i} `.padEnd(60, "x"),
      start: `${day}T${from}:00+01:00`,
      end: `${day}T${to}:00+01:00`,
      allDay: false,
      timezone: "Europe/Berlin",
      location: "",
      recurring: false,
      rule: null,
      alarms: [],
      exception: false,
      etag: `"e-${id}"`,
      tzGuess: false,
      recurUnsupported: false,
      readOnly: false,
      clipped: true,
    });
    state.fullTitles[id] =
      `Langer Titel ${i} mit allem, was die Liste von titand nicht behält, weil er länger ist als 256 Byte`;
  }
}

async function context() {
  const made = await create("user", "hell", 1440, "de", {
    viewport: { width: 1440, height: 900 },
    timezoneId: "Europe/Berlin",
  });
  await made.ctx.clock.setFixedTime(NOW);
  const state = calendarState();
  heavier(state);
  state.delay = DELAY;
  let release;
  state.stream = { held: new Promise((r) => (release = r)) };
  await installCalendar(made.ctx, state);
  return { ...made, state, release };
}

const settle = (page, ms = 1200) => page.waitForTimeout(ms);
const count = (state, from = 0) => {
  const out = { total: 0 };
  for (const [name] of KINDS) out[name] = 0;
  for (const [m, p] of state.requests.slice(from)) {
    const kind = KINDS.find(([, method, re]) => method === m && re.test(p));
    if (!kind) continue;
    out[kind[0]]++;
    out.total++;
  }
  return out;
};

try {
  // Time to the grid: from the start of navigation until an event of the
  // month is visible, five times in fresh pages of one context.
  const times = [];
  {
    const { ctx } = await context();
    for (let i = 0; i < 6; i++) {
      const page = await ctx.newPage();
      const t0 = Date.now();
      await page.goto(base + "/calendar?view=month");
      await page.locator(".month .ev").first().waitFor({ state: "visible" });
      const visible = Date.now() - t0;
      // and until every clipped title is whole (the N+1 of 1.11).
      await page
        .getByText("Langer Titel 29 mit allem", { exact: false })
        .first()
        .waitFor({ state: "visible", timeout: 15000 });
      const whole = Date.now() - t0;
      if (i > 0) times.push({ visible, whole }); // the first warms the cache
      await page.close();
    }
  }
  const median = (k) => {
    const v = times.map((x) => x[k]).sort((a, b) => a - b);
    return v[Math.floor(v.length / 2)];
  };

  const { page, state, release } = await context();
  const phases = {};
  let mark = 0;
  const phase = (name) => {
    phases[name] = count(state, mark);
    mark = state.requests.length;
  };
  await page.goto(base + "/calendar?view=month");
  await page.locator(".month .ev").first().waitFor();
  await settle(page, 2500);
  phase("open");

  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Performance.enable");
  const metrics = async () => {
    const { metrics } = await cdp.send("Performance.getMetrics");
    const m = Object.fromEntries(metrics.map((x) => [x.name, x.value]));
    return {
      layouts: m.LayoutCount,
      styles: m.RecalcStyleCount,
      layoutMs: m.LayoutDuration * 1000,
    };
  };
  const diff = (a, b) => ({
    layouts: b.layouts - a.layouts,
    styles: b.styles - a.styles,
    layoutMs: Math.round((b.layoutMs - a.layoutMs) * 10) / 10,
  });
  let m0 = await metrics();
  await page.getByRole("radio", { name: "Woche", exact: true }).click();
  await page.locator(".blk").first().waitFor();
  await settle(page);
  const weekOpen = diff(m0, await metrics());
  phase("week");
  // The window narrower and wider again, ten steps each way.
  m0 = await metrics();
  for (let w = 1440; w >= 1040; w -= 40) {
    await page.setViewportSize({ width: w, height: 900 });
    await page.waitForTimeout(30);
  }
  for (let w = 1040; w <= 1440; w += 40) {
    await page.setViewportSize({ width: w, height: 900 });
    await page.waitForTimeout(30);
  }
  await settle(page, 500);
  const weekResize = diff(m0, await metrics());
  // Scrolled to the evening: hiding, showing and a change elsewhere keep it.
  await page.evaluate(() => {
    const el = document.querySelector(".wk-scroll");
    if (el) el.scrollTop = 900;
  });

  const arbeit = page.locator('[data-calendar="arbeit"] input[type="checkbox"]');
  await arbeit.click();
  await settle(page);
  await arbeit.click();
  await settle(page);
  phase("hideShow");

  // Another window of this browser saved an event (BroadcastChannel).
  await page.evaluate(() => {
    const ch = new BroadcastChannel("titanos-calendar");
    ch.postMessage("changed");
    ch.close();
  });
  await settle(page);
  phase("announced");
  const weekScroll = await page.evaluate(
    () => document.querySelector(".wk-scroll")?.scrollTop ?? -1,
  );

  release([
    {
      type: "pim",
      data: {
        account: "alex",
        kind: "calendar",
        collection: "privat",
        generation: "1-2",
      },
    },
  ]);
  await settle(page, 2000);
  phase("live");

  await page.getByRole("button", { name: "Weiter", exact: true }).first().click();
  await page.waitForURL(/date=2026-11-2/);
  await settle(page);
  phase("nextWeek");

  await page.getByRole("radio", { name: "Monat", exact: true }).click();
  await page.locator(".month .ev").first().waitFor();
  await settle(page);
  phase("month");

  const result = {
    delayMs: DELAY,
    weekLayout: { open: weekOpen, resize: weekResize, scrollTopAfterLive: weekScroll },
    timeToGridMs: median("visible"),
    timeToWholeTitlesMs: median("whole"),
    phases,
    total: count(state),
  };
  const text = JSON.stringify(result, null, 2);
  console.log(text);
  if (process.env.MEASURE_OUT) await writeFile(process.env.MEASURE_OUT, text + "\n");
  if (errors.length) console.error("page errors:", errors.slice(0, 5));
} finally {
  await browser.close();
}
