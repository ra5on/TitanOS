// 1.11 (step 15), T-Kalender (docs/KALENDER.md, drafts 01–07, 40–44) with
// isolated data (test-ui-kalender-fixtures.mjs; the clock fixed at
// Tuesday, 17 November 2026, 12:29 in Berlin). Desktop 1440 px light and
// dark: month (today framed and named, "+2 weitere" opens the day, long
// titles wrap), week (KW, all-day row, time and repeat sign in every
// event, nothing lies on another event, the line of the current time),
// day, list; an event's details (links only http(s)/mailto/tel with
// rel="noopener noreferrer" and their real target as text, no HTML of the
// notes), deleting from a series sends the chosen scope with occurrence
// and etag (asserted on the request, also for a file name that needs
// encoding), a conflict says so; calendars: create, change, import with a
// summary, delete in its own dialog, calendar_last as text, hide with the
// word "ausgeblendet". The event form new (request asserted) and changed
// (scope asked, only the changed field sent), danger zone. Unavailable
// (503 and volume:false) shows the message, never a grid. Truncated
// (titand's cap of occurrences) shows a note. English once.
// Phone 390 px light and dark: list with the week on top, month with dashes,
// the form; targets ≥ 44 px. Once in a real desktop window, with "Ablage ›
// Neuer Termin" opening the event window. Every view: no sideways scroll,
// no cut text, the calendar's name beside every color.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5211 node scripts/test-ui-kalender.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import {
  LONG_TITLE,
  ODD_ID,
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
const base = process.env.UI_URL || "http://127.0.0.1:5211";
const out = process.env.UI_OUTPUT || "out/validation/kalender";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const NOW = new Date("2026-11-17T12:29:00+01:00");

/** A context with T-Kalender's data. `phone`: the phone's frame; `desk`: the real desktop. */
async function open({
  width = 1440,
  height = 1000,
  theme = "hell",
  lang = "de",
  phone = false,
  desk = false,
  zone = "Europe/Berlin",
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
          deviceScaleFactor: 3,
          viewport: { width, height: 844 },
          timezoneId: zone,
        }
      : { viewport: { width, height }, timezoneId: zone },
  );
  await made.ctx.clock.setFixedTime(NOW);
  if (phone || desk)
    await made.ctx.addInitScript(() =>
      localStorage.removeItem("titanos.layout"),
    );
  const state = calendarState();
  change(state);
  await installCalendar(made.ctx, state);
  return { ...made, state };
}

/** No sideways scroll and no cut text in a page or frame. */
async function sound(target, name) {
  const overflow = await target.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `${name}: scrolls sideways by ${overflow}px`);
  assert.deepEqual(await target.evaluate(cutTextIn), [], `${name}: cut text`);
}
/** Every colored event and calendar carries its calendar's name as text. */
async function namedColors(target, name) {
  const bare = await target.evaluate(() => {
    const found = [];
    for (const el of document.querySelectorAll(".ev, .blk, .arow"))
      if (
        el.getClientRects().length &&
        !/Privat|Arbeit|Familie|Sport|Geburtstage|Birthdays|Verein/.test(
          el.textContent,
        )
      )
        found.push(el.textContent.trim().slice(0, 40));
    for (const el of document.querySelectorAll(".calrow, .cp"))
      if (!el.textContent.trim()) found.push("calendar row without a name");
    return found;
  });
  assert.deepEqual(bare, [], `${name}: a color without the calendar's name`);
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
const sent = (state, method, re) =>
  state.requests.filter(([m, p]) => m === method && re.test(p));
async function ready(page, view) {
  await page.locator(`[data-pim-ready][data-view="${view}"]`).waitFor();
  // The events have come (a chip, block or row).
  await page.locator(".ev, .blk, .arow").first().waitFor();
  await page.waitForTimeout(300);
}
const dialog = (page) => page.locator("dialog[open]").last();

/** The frame of a window whose page path starts with `prefix`. */
async function windowFrame(page, prefix) {
  let found = null;
  await until(
    async () => {
      found = page.frames().find((f) => {
        if (f === page.mainFrame()) return false;
        try {
          return new URL(f.url()).pathname.startsWith(prefix);
        } catch {
          return false;
        }
      });
      return !!found;
    },
    `window ${prefix}`,
    15000,
  );
  return found;
}

try {
  // ---- Desktop 1440: month, week, day, list in light and dark ----
  for (const theme of ["hell", "graphit"]) {
    const { page, ctx } = await open({ theme });
    await page.goto(base + "/calendar");
    await ready(page, "month");
    const todayCell = page.locator('.cell[data-day="2026-11-17"]');
    assert.match(await todayCell.innerText(), /Heute/, "today is named");
    assert.match(await todayCell.innerText(), /\+2 weitere/, "+2 weitere");
    // The clipped title of the list is read whole (no cut text).
    await until(
      async () =>
        (await todayCell.innerText()).includes(
          "Nebenkostenabrechnung 2026 vorbereiten",
        ),
      "the whole long title",
    );
    // The hidden calendar says so in words and its events stay away.
    assert.match(
      await page.locator('.calrow[data-calendar="sport"]').innerText(),
      /ausgeblendet/,
    );
    assert.equal(
      await page.getByText("Lauftreff").count(),
      0,
      "hidden calendar not shown",
    );
    assert.match(
      await page.locator('.calrow[data-calendar="geburtstage"]').innerText(),
      /aus den Kontakten, nur lesen/,
    );
    assert.equal(
      await page.locator('.calrow[data-calendar="geburtstage"] button').count(),
      0,
      "Geburtstage cannot be changed",
    );
    assert.match(await page.locator(".cbar h2").innerText(), /November 2026/);
    // ISO weeks in the mini month and the grid.
    assert.match(await page.locator(".mini").innerText(), /47/);
    await sound(page, `month ${theme}`);
    await namedColors(page, `month ${theme}`);
    await shot(page, `monat-${theme}`);

    await page.goto(base + "/calendar?view=week");
    await ready(page, "week");
    assert.match(await page.locator(".cbar h2").innerText(), /KW 47/);
    const col = page.locator('.col[data-day="2026-11-17"]');
    assert.equal(
      await col.locator(".now").count(),
      1,
      "line of the current time",
    );
    // Every block carries its time; series their sign (in words too).
    for (const text of await page.locator(".blk .tm").allInnerTexts())
      assert.match(text, /\d\d:\d\d/, "time in every event");
    assert.ok(
      (
        await page
          .locator(".blk", { hasText: "Fahrgemeinschaft" })
          .first()
          .innerText()
      ).includes("wiederholt sich") ||
        (await page
          .locator(".blk", { hasText: "Fahrgemeinschaft" })
          .first()
          .locator(".sr-only")
          .first()
          .textContent()) === "wiederholt sich",
      "repeat sign with words",
    );
    // The long title grows; nothing lies on top of another block of its lane.
    const boxes = await col.locator(".blk").evaluateAll((els) =>
      els.map((e) => ({
        lane: e.dataset.lane,
        top: e.offsetTop,
        bottom: e.offsetTop + e.offsetHeight,
        text: e.textContent,
      })),
    );
    const long = boxes.find((b) => b.text.includes(LONG_TITLE));
    assert.ok(long && long.bottom - long.top > 100, "the long event grew");
    for (const a of boxes)
      for (const b of boxes)
        if (a !== b && a.lane === b.lane)
          assert.ok(
            a.bottom <= b.top + 1 || b.bottom <= a.top + 1,
            `blocks overlap: ${a.text.slice(0, 20)} / ${b.text.slice(0, 20)}`,
          );
    assert.match(
      await page.locator(".wk-all").innerText(),
      /Oma zu Besuch/,
      "all-day row",
    );
    await sound(page, `week ${theme}`);
    await namedColors(page, `week ${theme}`);
    await shot(page, `woche-${theme}`);

    await page.goto(base + "/calendar?view=day&date=2026-11-17");
    await ready(page, "day");
    assert.equal(await page.locator(".col").count(), 1, "day: one column");
    await sound(page, `day ${theme}`);
    await shot(page, `tag-${theme}`);

    await page.goto(base + "/calendar?view=list");
    await ready(page, "list");
    assert.match(
      await page.locator(".agenda").innerText(),
      /Heute\s*Dienstag, 17\. November/,
    );
    await sound(page, `list ${theme}`);
    await namedColors(page, `list ${theme}`);
    await shot(page, `liste-${theme}`);
    await ctx.close();
  }

  // ---- "+2 weitere" opens the day; keyboard; details; links ----
  {
    const { page, ctx } = await open();
    await page.goto(base + "/calendar");
    await ready(page, "month");
    await page.locator('.cell[data-day="2026-11-17"] .more').click();
    // Today needs no date in the address.
    await until(
      () =>
        page.url().includes("view=day") &&
        !/date=(?!2026-11-17)/.test(page.url()),
      "+2 weitere opens the day",
    );
    await ready(page, "day");
    // Tab reaches the controls of the bar.
    const reached = new Set();
    for (
      let i = 0;
      i < 150 && !["Heute", "Zurück", "Weiter"].every((w) => reached.has(w));
      i++
    ) {
      await page.keyboard.press("Tab");
      reached.add(
        await page.evaluate(
          () =>
            (document.activeElement?.textContent ?? "").trim() ||
            document.activeElement?.getAttribute("aria-label") ||
            "",
        ),
      );
    }
    for (const want of ["Heute", "Zurück", "Weiter"])
      assert.ok(
        [...reached].some((r) => r === want),
        `Tab reaches ${want}`,
      );
    await page.locator(".blk", { hasText: "Elternabend" }).click();
    const d = dialog(page);
    await d.locator("[data-event-detail]").waitFor();
    await until(
      async () => (await d.innerText()).includes("Bastelsachen"),
      "notes loaded",
    );
    const links = await d.locator("a").evaluateAll((as) =>
      as.map((a) => ({
        href: a.getAttribute("href"),
        text: a.textContent,
        rel: a.rel,
      })),
    );
    assert.deepEqual(
      links.map((l) => l.href),
      [
        "https://schule.example.org/elternabend?k=3b",
        "mailto:krueger@schule.example",
        "tel:+49-221-444120",
      ],
    );
    for (const l of links) {
      assert.equal(l.text, l.href, "the real target is visible");
      assert.equal(l.rel, "noopener noreferrer");
    }
    assert.equal(
      await d.locator("img").count(),
      0,
      "notes as text, never HTML",
    );
    assert.match(await d.innerText(), /<img src=x onerror=alert\(1\)>/);
    assert.match(await d.innerText(), /Familie/);
    assert.match(await d.innerText(), /jeden Monat am dritten Dienstag/);
    assert.match(await d.innerText(), /1 Stunde vorher/);
    assert.match(await d.innerText(), /Am Vortag um 18:00/);
    await sound(page, "detail");
    await shot(page, "termin-details");
    await page.keyboard.press("Escape");
    await until(
      async () => (await page.locator("dialog[open]").count()) === 0,
      "Escape closes the details",
    );
    await ctx.close();
  }

  // ---- Deleting from a series: scope, occurrence and etag reach the API ----
  for (const scope of ["this", "following", "all"]) {
    const { page, ctx, state } = await open({
      theme: scope === "all" ? "graphit" : "hell",
    });
    await page.goto(base + "/calendar?view=week");
    await ready(page, "week");
    await page
      .locator('.col[data-day="2026-11-16"] .blk', {
        hasText: "Wochenbesprechung",
      })
      .click();
    await dialog(page).locator("[data-event-detail]").waitFor();
    await dialog(page).getByRole("button", { name: "Löschen …" }).click();
    const del = dialog(page);
    await del.getByText("Nur diesen Termin").waitFor();
    const text = await del.innerText();
    assert.match(text, /„Wochenbesprechung“ löschen\?/);
    assert.match(
      text,
      /Montag, 16\. November, 09:00 gehört zu einer Reihe: jede Woche am Montag/,
    );
    assert.match(text, /Alle 12 Termine, auch die vergangenen/);
    // "Löschen" left of "Abbrechen", never focused at first.
    const danger = await del
      .locator(".dialog-actions .btn-danger")
      .boundingBox();
    const cancel = await del
      .locator(".dialog-actions .btn", { hasText: "Abbrechen" })
      .boundingBox();
    assert.ok(danger.x < cancel.x, "danger button left of Abbrechen");
    assert.equal(
      await page.evaluate(() =>
        document.activeElement?.classList.contains("btn-danger"),
      ),
      false,
    );
    if (scope === "this") {
      await sound(page, "series delete dialog");
      await shot(page, "reihe-loeschen");
    }
    const label = {
      this: "Nur diesen Termin",
      following: "Diesen und alle folgenden",
      all: "Die ganze Reihe",
    }[scope];
    await del.getByText(label).click();
    await del.getByRole("button", { name: "Löschen", exact: true }).click();
    await until(
      async () => (await page.locator("dialog[open]").count()) === 0,
      "dialog closes",
    );
    const req = sent(state, "DELETE", /^\/calendar\/events\//);
    assert.equal(req.length, 1);
    const url = new URL("http://x" + req[0][1]);
    assert.equal(url.pathname, "/calendar/events/arbeit/wochenbesprechung.ics");
    assert.equal(url.searchParams.get("scope"), scope);
    assert.equal(url.searchParams.get("occurrence"), "20261116T080000Z");
    assert.equal(url.searchParams.get("etag"), '"e-wochenbesprechung.ics"');
    await ctx.close();
  }

  // ---- A file name that needs encoding; a conflict says so ----
  {
    const { page, ctx, state } = await open({
      change: (s) => (s.conflictOnce = true),
    });
    await page.goto(base + "/calendar?view=week");
    await ready(page, "week");
    await page
      .locator('.col[data-day="2026-11-17"] .blk', { hasText: "Elternabend" })
      .click();
    await dialog(page).locator("[data-event-detail]").waitFor();
    await until(
      async () => (await dialog(page).innerText()).includes("Bastelsachen"),
      "notes",
    );
    await dialog(page).getByRole("button", { name: "Löschen …" }).click();
    // No count from the server: "Alle Termine".
    await dialog(page)
      .getByText("Alle Termine, auch die vergangenen.")
      .waitFor();
    await dialog(page)
      .getByRole("button", { name: "Löschen", exact: true })
      .click();
    await dialog(page)
      .getByText(/inzwischen geändert/)
      .waitFor();
    await dialog(page).getByRole("button", { name: "Neu laden" }).click();
    await until(
      async () =>
        !(await dialog(page).innerText()).includes("inzwischen geändert"),
      "reloaded",
    );
    await dialog(page)
      .getByRole("button", { name: "Löschen", exact: true })
      .click();
    await until(
      async () => (await page.locator("dialog[open]").count()) === 0,
      "deleted",
    );
    const req = sent(state, "DELETE", /^\/calendar\/events\//);
    assert.equal(req.length, 2);
    assert.ok(
      req[1][1].startsWith(
        `/calendar/events/familie/${encodeURIComponent(ODD_ID)}?scope=this&occurrence=20261117T180000Z&etag=`,
      ),
      `encoded id: ${req[1][1]}`,
    );
    await ctx.close();
  }

  // ---- Calendars: create, hide, change, import, export, delete ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/calendar");
    await ready(page, "month");
    await page.getByRole("button", { name: "Neuer Kalender" }).click();
    await dialog(page).locator("[data-calendar-dialog]").waitFor();
    await sound(page, "new calendar dialog");
    await shot(page, "kalender-neu");
    await page.keyboard.press("Escape");
    await until(
      async () => (await page.locator("dialog[open]").count()) === 0,
      "Escape closes",
    );
    await page.getByRole("button", { name: "Neuer Kalender" }).click();
    await dialog(page).getByLabel("Name").fill("Verein");
    await dialog(page).getByLabel("Grün").check({ force: true });
    await dialog(page).getByRole("button", { name: "Anlegen" }).click();
    await page.locator('.calrow[data-calendar="verein"]').waitFor();
    assert.deepEqual(sent(state, "POST", /^\/calendar\/calendars$/)[0][2], {
      name: "Verein",
      color: "#15803d",
    });

    // Hide "Familie": the preference names it, the row says so.
    await page.locator('.calrow[data-calendar="familie"] input').uncheck();
    await until(
      async () =>
        (
          await page.locator('.calrow[data-calendar="familie"]').innerText()
        ).includes("ausgeblendet"),
      "ausgeblendet",
    );
    assert.deepEqual(
      sent(state, "PUT", /^\/preferences\/calendar$/).at(-1)[2],
      { hidden: ["familie", "sport"] },
    );

    await page.getByRole("button", { name: "Arbeit: Einstellungen" }).click();
    const d = dialog(page);
    await d.locator("[data-calendar-import]").waitFor();
    await d.locator("input[type=file]").setInputFiles({
      name: "outlook.ics",
      mimeType: "text/calendar",
      buffer: Buffer.from("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"),
    });
    await d.getByText("Ersetzen").click();
    await d.getByRole("button", { name: "Importieren", exact: true }).click();
    await d.locator("[data-import-result]").waitFor();
    assert.match(
      await d.locator("[data-import-result]").innerText(),
      /3 neu, 1 ersetzt, 2 übersprungen/,
    );
    assert.match(
      await d.locator("[data-import-result]").innerText(),
      /aufgabe-1@example.org: kein Termin/,
    );
    assert.ok(
      sent(
        state,
        "POST",
        /^\/calendar\/calendars\/arbeit\/import\?onDuplicate=replace$/,
      ).length === 1,
    );
    await sound(page, "import result");
    await shot(page, "kalender-import");
    const download = page.waitForEvent("download");
    await d.getByRole("button", { name: "Als .ics herunterladen" }).click();
    assert.equal((await download).suggestedFilename(), "arbeit.ics");

    await d.getByRole("button", { name: "Kalender löschen …" }).click();
    const c = dialog(page);
    await c.getByText(/Kalender „Arbeit“ löschen\?/).waitFor();
    assert.match(await c.innerText(), /mit allen seinen \d+ Terminen/);
    const danger = await c.locator(".dialog-actions .btn-danger").boundingBox();
    const cancel = await c
      .locator(".dialog-actions .btn", { hasText: "Abbrechen" })
      .boundingBox();
    assert.ok(danger.x < cancel.x, "calendar delete: danger left of Abbrechen");
    await sound(page, "calendar delete dialog");
    await shot(page, "kalender-loeschen");
    await c
      .getByRole("button", { name: "Kalender löschen", exact: true })
      .click();
    await until(
      async () => (await page.locator("dialog[open]").count()) === 0,
      "calendar deleted",
    );
    assert.equal(
      sent(state, "DELETE", /^\/calendar\/calendars\/arbeit$/).length,
      1,
    );
    await page
      .locator('.calrow[data-calendar="arbeit"]')
      .waitFor({ state: "detached" });
    await ctx.close();
  }
  {
    // The last calendar stays; titand's reason as text.
    const { page, ctx } = await open({
      change: (s) =>
        (s.calendars = s.calendars.filter((c) => c.id === "privat")),
    });
    await page.goto(base + "/calendar");
    await page.locator('.calrow[data-calendar="privat"]').waitFor();
    await page.getByRole("button", { name: "Privat: Einstellungen" }).click();
    await dialog(page)
      .getByRole("button", { name: "Kalender löschen …" })
      .click();
    await dialog(page)
      .getByRole("button", { name: "Kalender löschen", exact: true })
      .click();
    await dialog(page)
      .getByText("Der letzte Kalender lässt sich nicht löschen.")
      .waitFor();
    await ctx.close();
  }

  // ---- The event form: new ----
  {
    const { page, ctx, state } = await open();
    await page.goto(
      base + "/calendar/event?date=2026-11-17&time=19:00&back=%2Fcalendar",
    );
    await page.locator("[data-event-form]").waitFor();
    await page.locator("#t-title").fill("Elternabend Klasse 3b");
    await page.locator(".cp", { hasText: "Familie" }).click();
    assert.match(await page.locator(".cp.on").innerText(), /Familie/);
    assert.match(
      await page.locator("[data-duration]").innerText(),
      /1 Stunde · Zeitzone Berlin/,
    );
    await page.locator("#t-rep").selectOption("monthlyNth");
    assert.match(
      await page.locator("#t-rep option:checked").innerText(),
      /Jeden Monat am dritten Dienstag/,
    );
    await page.getByRole("radio", { name: "Am Datum" }).click();
    await page.getByLabel("Endet am").fill("2027-07-13");
    await page.locator("[data-preview]").waitFor();
    assert.match(
      await page.locator("[data-preview]").innerText(),
      /Nächste Termine: 15\. Dezember, 19\. Januar, 16\. Februar … · 8 Termine bis 13\. Juli 2027/,
    );
    // "+ Erinnerung" (drafts 05/43) opens the choices.
    await page.getByRole("button", { name: "Erinnerung hinzufügen" }).click();
    await page.getByRole("menuitem", { name: "1 Stunde vorher" }).click();
    await page.locator("#t-place").fill("Grundschule am Park, Raum 12");
    await page.locator("#t-note").fill("Bastelsachen mitbringen.");
    await sound(page, "new event");
    await shot(page, "termin-neu");
    await page.getByRole("button", { name: "Speichern" }).click();
    await until(
      () => sent(state, "POST", /^\/calendar\/events$/).length === 1,
      "POST",
    );
    const body = sent(state, "POST", /^\/calendar\/events$/)[0][2];
    assert.equal(body.calendar, "familie");
    assert.deepEqual(body.event, {
      start: "2026-11-17T19:00",
      end: "2026-11-17T20:00",
      allDay: false,
      timezone: "Europe/Berlin",
      title: "Elternabend Klasse 3b",
      location: "Grundschule am Park, Raum 12",
      notes: "Bastelsachen mitbringen.",
      alarms: [60],
      repeat: {
        freq: "monthly",
        interval: 1,
        byDay: ["3TU"],
        until: "2027-07-13",
      },
    });
    await until(
      () => new URL(page.url()).pathname === "/calendar",
      "back to the calendar",
    );
    await ctx.close();
  }

  // ---- The event form: changing one of a series (dark) ----
  {
    const { page, ctx, state } = await open({ theme: "graphit" });
    await page.goto(
      base +
        `/calendar/event?cal=familie&id=${encodeURIComponent(ODD_ID)}&occurrence=20261117T180000Z&back=%2Fcalendar%3Fview%3Dweek`,
    );
    await page.locator("[data-event-form]").waitFor();
    assert.equal(
      await page.locator("#t-title").inputValue(),
      "Elternabend Klasse 3b",
    );
    assert.match(
      await page.locator("#t-rep option:checked").innerText(),
      /Jeden Monat am dritten Dienstag/,
    );
    const form = await page.locator("[data-event-form]").innerText();
    assert.match(form, /1 Stunde vorher/);
    assert.match(form, /Am Vortag um 18:00/);
    assert.match(form, /1 Stunde 30 Minuten · Zeitzone Berlin/);
    assert.match(form, /Gefahrenzone/i);
    await sound(page, "edit event");
    await shot(page, "termin-bearbeiten-graphit");
    await page.locator("#t-title").fill("Elternabend Klasse 3b – mit Wahl");
    await page.getByRole("button", { name: "Speichern" }).click();
    const d = dialog(page);
    await d.getByText("Diesen und alle folgenden").waitFor();
    await sound(page, "save scope dialog");
    await shot(page, "termin-speichern-reihe");
    await d.getByText("Diesen und alle folgenden").click();
    await d.getByRole("button", { name: "Speichern" }).click();
    await until(
      () => sent(state, "PUT", /^\/calendar\/events\//).length === 1,
      "PUT",
    );
    const [, path, body] = sent(state, "PUT", /^\/calendar\/events\//)[0];
    assert.equal(
      path,
      `/calendar/events/familie/${encodeURIComponent(ODD_ID)}`,
    );
    assert.deepEqual(body, {
      etag: `"e-${ODD_ID}"`,
      scope: "following",
      occurrence: "20261117T180000Z",
      event: { title: "Elternabend Klasse 3b – mit Wahl" },
    });
    await until(
      () => page.url().includes("/calendar?view=week"),
      "back to the week",
    );
    await ctx.close();
  }
  {
    // Danger zone of the form: the series question again.
    const { page, ctx, state } = await open();
    await page.goto(
      base +
        `/calendar/event?cal=arbeit&id=wochenbesprechung.ics&occurrence=20261116T080000Z`,
    );
    await page.locator("[data-event-form]").waitFor();
    await page.getByRole("button", { name: "Termin löschen …" }).click();
    await dialog(page)
      .getByText("Alle 12 Termine, auch die vergangenen.")
      .waitFor();
    await dialog(page).getByText("Die ganze Reihe").click();
    await dialog(page)
      .getByRole("button", { name: "Löschen", exact: true })
      .click();
    await until(
      () => sent(state, "DELETE", /scope=all/).length === 1,
      "DELETE all from the form",
    );
    await ctx.close();
  }

  // ---- Unavailable: the message, never an empty grid ----
  for (const mode of ["503", "volume"]) {
    const { page, ctx } = await open({ change: (s) => (s.unavailable = mode) });
    await page.goto(base + "/calendar");
    await page.locator('[data-pim-state="unavailable"]').waitFor();
    assert.match(
      await page.locator('[data-pim-state="unavailable"]').innerText(),
      /Der Kalender ist gerade nicht verfügbar/,
    );
    assert.equal(
      await page.locator("[data-pim-ready], .month, .week").count(),
      0,
      "no grid",
    );
    await sound(page, `unavailable ${mode}`);
    if (mode === "503") await shot(page, "nicht-verfuegbar");
    await ctx.close();
  }

  // ---- Too many events (S-20): titand says truncated, the page says so ----
  for (const lang of ["de", "en"]) {
    const { page, ctx } = await open({
      lang,
      change: (s) => (s.truncated = true),
    });
    await page.goto(base + "/calendar");
    await ready(page, "month");
    const note = page.locator('.notice-warn[role="status"]');
    await note.waitFor();
    assert.match(
      await note.innerText(),
      lang === "de"
        ? /Nicht alle Termine angezeigt/
        : /Not all events are shown/,
    );
    await sound(page, `truncated ${lang}`);
    await ctx.close();
  }
  {
    const { page, ctx } = await open();
    await page.goto(base + "/calendar");
    await ready(page, "month");
    assert.equal(
      await page.locator('.notice-warn[role="status"]').count(),
      0,
      "no note without truncated",
    );
    await ctx.close();
  }

  // ---- The NAS zone (TECHNIK 2.2 point 5): the browser elsewhere ----
  {
    const { page, ctx, state } = await open({
      zone: "America/New_York",
      change: (s) =>
        s.events.push({
          ...s.events.find((e) => e.title === "Elternabend Klasse 3b"),
          id: "tokio.ics",
          uid: "tokio",
          title: "Anruf Tokio",
          start: "2026-11-19T09:00:00+09:00",
          end: "2026-11-19T09:30:00+09:00",
          occurrence: "20261119T000000Z",
          timezone: "Asia/Tokyo",
          recurring: false,
          rule: null,
          etag: '"e-tokio"',
        }),
    });
    await page.goto(base + "/calendar?view=week");
    await ready(page, "week");
    // Shown at the NAS clock, not New York's (13:00).
    assert.match(
      await page
        .locator(".blk", { hasText: "Elternabend" })
        .first()
        .innerText(),
      /19:00/,
    );
    // An event of another zone: the 01:00 Berlin block, its own zone in the details.
    await page.locator(".blk", { hasText: "Anruf Tokio" }).click();
    assert.match(await dialog(page).innerText(), /Zeitzone Tokyo/);
    await sound(page, "nas zone details");
    await page.keyboard.press("Escape");
    // A new event is made in the NAS zone.
    await page.goto(base + "/calendar/event?date=2026-11-18&time=10:00");
    await page.locator("[data-event-form]").waitFor();
    await page.locator("#t-title").fill("Zahnarzt");
    await until(
      async () =>
        /Zeitzone Berlin/.test(
          await page.locator("[data-duration]").innerText(),
        ),
      "form in the NAS zone",
    );
    await page.getByRole("button", { name: "Speichern" }).click();
    await until(
      () => sent(state, "POST", /^\/calendar\/events$/).length === 1,
      "POST in the NAS zone",
    );
    const made = sent(state, "POST", /^\/calendar\/events$/)[0][2].event;
    assert.equal(made.timezone, "Europe/Berlin");
    assert.equal(made.start, "2026-11-18T10:00");
    await ctx.close();
  }
  // Without a NAS zone the browser's.
  {
    const { page, ctx, state } = await open({
      zone: "America/New_York",
      change: (s) => (s.timezone = ""),
    });
    await page.goto(base + "/calendar/event?date=2026-11-18&time=10:00");
    await page.locator("[data-event-form]").waitFor();
    await page.locator("#t-title").fill("Zahnarzt");
    await page.getByRole("button", { name: "Speichern" }).click();
    await until(
      () => sent(state, "POST", /^\/calendar\/events$/).length === 1,
      "POST in the browser zone",
    );
    assert.equal(
      sent(state, "POST", /^\/calendar\/events$/)[0][2].event.timezone,
      "America/New_York",
    );
    await ctx.close();
  }

  // ---- English once ----
  {
    const { page, ctx } = await open({ lang: "en" });
    await page.goto(base + "/calendar?view=week");
    await ready(page, "week");
    assert.match(
      await page.locator(".cbar h2").innerText(),
      /November 16\s*–\s*22, 2026 · Week 47/,
    );
    assert.match(await page.locator(".cbar").innerText(), /Today/);
    assert.match(await page.locator(".calrow").last().innerText(), /Birthdays/);
    await sound(page, "week en");
    await shot(page, "woche-en");
    await ctx.close();
  }

  // ---- Phone 390: list with the week, month with dashes, the form; 44 px ----
  const small = (page, scope) =>
    page.evaluate((scope) => {
      const box = (el) => {
        if (el.matches("input[type=checkbox], input[type=radio]")) {
          const label = el.closest("label") ?? el.labels?.[0];
          return label
            ? label.getBoundingClientRect()
            : el.getBoundingClientRect();
        }
        return el.getBoundingClientRect();
      };
      return [
        ...document.querySelectorAll(
          `${scope} button, ${scope} input, ${scope} a, ${scope} select`,
        ),
      ]
        .filter(
          (el) =>
            el.getClientRects().length &&
            getComputedStyle(el).visibility === "visible",
        )
        .filter(
          (el) =>
            !(
              el.matches("input[type=radio]") &&
              getComputedStyle(el).opacity === "0" &&
              !el.closest("label")
            ),
        )
        .map((el) => [
          el.textContent.trim() ||
            el.getAttribute("aria-label") ||
            el.id ||
            el.type,
          box(el),
        ])
        .filter(([, r]) => r.height < 44 || (r.width < 44 && r.width > 0))
        .map(([n, r]) => `${n} ${Math.round(r.width)}x${Math.round(r.height)}`);
    }, scope);
  for (const theme of ["graphit", "hell"]) {
    const { page, ctx } = await open({ width: 390, theme, phone: true });
    await page.goto(base + "/calendar");
    await ready(page, "list");
    assert.equal(
      await page.locator(".strip .sd").count(),
      7,
      "the week on top",
    );
    const first = await page.locator(".arow").first().innerText();
    assert.match(first, /07:45[\s\S]*Fahrgemeinschaft Schule[\s\S]*Familie/);
    assert.ok(
      await page.locator(".arow .al .sr-only").count(),
      "bell with words",
    );
    assert.deepEqual(
      await small(page, "[data-pim-shell]"),
      [],
      `phone list ${theme}: targets under 44 px`,
    );
    await sound(page, `phone list ${theme}`);
    await namedColors(page, `phone list ${theme}`);
    // The dialogs come as sheets with 44 px targets.
    for (const [opener, what] of [
      [
        page.getByRole("button", { name: "Privat: Einstellungen" }).first(),
        "calendar dialog",
      ],
      [
        page.locator(".arow", { hasText: "Elternabend" }).first(),
        "event detail",
      ],
    ]) {
      await opener.click();
      const sheet = dialog(page);
      await sheet.waitFor();
      await page.waitForTimeout(500);
      if (what === "event detail")
        await until(
          async () => (await sheet.innerText()).includes("Bastelsachen"),
          "notes on the phone",
        );
      assert.deepEqual(
        await small(page, "dialog[open]"),
        [],
        `phone ${what} ${theme}: targets under 44 px`,
      );
      await sound(page, `phone ${what} ${theme}`);
      await shot(page, `handy-${what.replace(" ", "-")}-${theme}`);
      if (what === "event detail") {
        await sheet.getByRole("button", { name: "Löschen …" }).click();
        await dialog(page).getByText("Nur diesen Termin").waitFor();
        await page.waitForTimeout(500);
        assert.deepEqual(
          await small(page, "dialog[open]"),
          [],
          `phone delete ${theme}: targets under 44 px`,
        );
        await sound(page, `phone delete ${theme}`);
        await shot(page, `handy-loeschen-${theme}`);
      }
      await page.keyboard.press("Escape");
      await until(
        async () => (await page.locator("dialog[open]").count()) === 0,
        `${what} closes`,
      );
    }
    await shot(page, `handy-liste-${theme}`);

    await page.getByRole("radio", { name: "Monat" }).click();
    await ready(page, "month");
    assert.equal(
      await page.locator(".month .ev").count(),
      0,
      "no titles in the cells",
    );
    assert.ok(
      (await page.locator(".marks i").count()) > 10,
      "dashes per event",
    );
    await page.locator(".pick", { hasText: "19" }).first().click();
    await until(
      async () =>
        (await page.locator(".agenda").innerText()).includes("Messe Köln"),
      "the chosen day below",
    );
    assert.deepEqual(
      await small(page, "[data-pim-shell]"),
      [],
      `phone month ${theme}: targets under 44 px`,
    );
    await sound(page, `phone month ${theme}`);
    await namedColors(page, `phone month ${theme}`);
    await shot(page, `handy-monat-${theme}`);

    await page.goto(base + "/calendar/event?date=2026-11-19&time=12:30");
    await page.locator("[data-event-form]").waitFor();
    await page.locator("#t-title").fill("Mittagessen mit Anna");
    assert.deepEqual(
      await small(page, "[data-event-form]"),
      [],
      `phone form ${theme}: targets under 44 px`,
    );
    await sound(page, `phone form ${theme}`);
    await shot(page, `handy-termin-${theme}`);
    await page.locator("#t-note").scrollIntoViewIfNeeded();
    await shot(page, `handy-termin-unten-${theme}`);
    await ctx.close();
  }

  // ---- In a window of the real desktop; "Ablage › Neuer Termin" opens the event window ----
  {
    const { page, ctx, state } = await open({ desk: true, height: 1000 });
    await page.goto(base + "/calendar");
    await page.locator("section.win").first().waitFor();
    const frame = await windowFrame(page, "/calendar");
    await frame.locator('[data-pim-ready][data-view="month"]').waitFor();
    await frame.locator(".ev").first().waitFor();
    await until(
      async () =>
        (await page.locator("section.win").first().textContent()).includes(
          "Monat",
        ),
      "title bar names the view",
    );
    await sound(frame, "desktop window");
    await sound(page, "desktop");
    await shot(page, "arbeitsplatz-fenster");
    await page.waitForTimeout(800);
    await page
      .locator("header.topbar .menubar")
      .getByRole("menuitem", { name: "Ablage", exact: true })
      .click();
    const item = page.locator('.mb-menu [data-id="calendar.newEvent"]');
    await item.waitFor();
    await until(async () => !(await item.isDisabled()), "Neuer Termin enabled");
    await item.click();
    const ev = await windowFrame(page, "/calendar/event");
    await ev.locator("[data-event-form]").waitFor();
    await until(
      async () => (await page.locator("section.win").count()) >= 2,
      "a window of its own",
    );
    await sound(ev, "event window");
    await shot(page, "arbeitsplatz-neuer-termin");
    // Saving closes the event window and the calendar window reads again.
    await ev.locator("#t-title").fill("Rückruf Hausverwaltung");
    const posted = state.requests.length;
    await ev.getByRole("button", { name: "Speichern" }).click();
    await until(
      () => sent(state, "POST", /^\/calendar\/events$/).length === 1,
      "POST from the window",
    );
    await until(
      async () => (await page.locator("section.win").count()) === 1,
      "the event window closes",
    );
    await until(
      () =>
        state.requests
          .slice(posted)
          .some(([m, p]) => m === "GET" && p.startsWith("/calendar/events?")),
      "the calendar reloads after the save",
    );
    // The week in the window (about 1000 px): no cut text in narrow blocks.
    await frame.getByRole("radio", { name: "Woche" }).click();
    await frame.locator('[data-pim-ready][data-view="week"]').waitFor();
    await frame.locator(".blk").first().waitFor();
    await page.waitForTimeout(400);
    await sound(frame, "desktop window week");
    await shot(page, "arbeitsplatz-fenster-woche");
    await ctx.close();
  }

  // ---- A smaller window (800 px, the page alone): month, week, form ----
  for (const [path, view] of [
    ["/calendar", "month"],
    ["/calendar?view=week", "week"],
    ["/calendar?view=list", "list"],
  ]) {
    const { page, ctx } = await open({ width: 800 });
    await page.goto(base + path);
    await ready(page, view);
    await sound(page, `800 ${view}`);
    await namedColors(page, `800 ${view}`);
    await shot(page, `fenster-800-${view}`);
    await ctx.close();
  }
  {
    const { page, ctx } = await open({ width: 800, theme: "graphit" });
    await page.goto(
      base +
        `/calendar/event?cal=familie&id=${encodeURIComponent(ODD_ID)}&occurrence=20261117T180000Z`,
    );
    await page.locator("[data-event-form]").waitFor();
    await page.waitForTimeout(400);
    await sound(page, "800 form");
    await shot(page, "fenster-800-termin");
    await ctx.close();
  }

  assert.deepEqual(errors, [], "script errors on the page");
  console.log(`T-Kalender: all checks passed, screenshots in ${out}`);
} finally {
  await browser.close();
}
