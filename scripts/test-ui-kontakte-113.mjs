// 1.13, Kontakte on the shared column frame (Baustand step 6, docs/KALENDER.md
// "Kontakte 1.13"), on isolated data (test-ui-kontakte-fixtures.mjs; the
// clock fixed at Tuesday, 17 November 2026 in Berlin), nothing reaches a
// NAS. Replaces test-ui-kontakte.mjs (1.11) for the page.
//
// Desktop 1440 px light/German: edge to edge as high as the window, no
// strip above, "Neuer Kontakt" on top of the side bar, the list's search on
// top of its column; one request per search (GET /contacts/search), Mail's
// suggestions only when shown, the list's ETag (304 on a reload); groups
// (filter, new, rename, delete with a dialog – the contacts stay –, a
// contact dragged onto a group, ticked in the form); the form with its own
// Abbrechen/Speichern, a save puts the row in place (no list), a conflict
// with "Neu laden", deleting with a dialog; a new picture cut square;
// import and export from the side bar's menu; e-mail addresses open Mail's
// compose (mailto: without Mail); possible duplicates merged field by field
// (POST /contacts/merge after a dialog); two address books sorted by the
// grip's keys and saved for the account (PUT /preferences/order). Dark and
// English once; 800 px (levels with "‹ Zurück"); the phone 390 px light and
// dark: levels without cards, iOS's round actions (tel:, sms:, e-mail, map),
// the form as a sheet over the screen, bars at the bottom with 44 px
// targets. Unavailable (503): "nicht verfügbar", never a list; an empty
// book: the friendly empty state. Every screen: no sideways scroll, no cut
// text. Screenshots into UI_OUTPUT.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5213 UI_OUTPUT=/path node scripts/test-ui-kontakte-113.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { deflateSync } from "node:zlib";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import { calendarState, installCalendar } from "./test-ui-kalender-fixtures.mjs";
import { contactsState, installContacts } from "./test-ui-kontakte-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");

const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5213";
const out = process.env.UI_OUTPUT || "out/validation/kontakte-113";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const NOW = new Date("2026-11-17T12:29:00+01:00");

/** A PNG of w × h in one colour (a picture to crop). */
function png(w, h) {
  const crc = (buf) => {
    let c = ~0;
    for (const b of buf) {
      c ^= b;
      for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1));
    }
    return ~c >>> 0;
  };
  const chunk = (type, data) => {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const td = Buffer.concat([Buffer.from(type), data]);
    const c = Buffer.alloc(4);
    c.writeUInt32BE(crc(td));
    return Buffer.concat([len, td, c]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  const raw = Buffer.alloc((w * 3 + 1) * h);
  for (let y = 0; y < h; y++)
    for (let x = 0; x < w; x++) raw.set([x * 2, 120, 200], y * (w * 3 + 1) + 1 + x * 3);
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

/** The data of the drafts plus a duplicate of Anna and two groups. */
function withExtras(state) {
  state.contacts.push({
    ...state.contacts[0],
    id: "anna2.vcf",
    etag: '"1-anna2.vcf"',
    org: "",
    hasPhoto: false,
    phones: [{ type: "work", value: "+49 30 999 1234" }],
    notes: "Aus dem alten Handy",
  });
  state.groups = [
    { id: "familie.vcf", name: "Familie", members: ["anna.vcf", "lena.vcf", "Oma Hilde+1@icloud.vcf"] },
    { id: "verein.vcf", name: "Fußballverein Süd", members: ["ben.vcf"] },
  ];
}

async function open({
  width = 1440,
  height = 900,
  theme = "hell",
  lang = "de",
  phone = false,
  change = withExtras,
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
          timezoneId: "Europe/Berlin",
        }
      : { viewport: { width, height }, timezoneId: "Europe/Berlin" },
  );
  await made.ctx.clock.setFixedTime(NOW);
  if (phone) await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  await installCalendar(made.ctx, calendarState());
  const state = contactsState();
  change(state);
  await installContacts(made.ctx, state);
  const order = { puts: [] };
  await made.ctx.route(/\/api\/v1\/preferences\/order$/, (route) => {
    const body = route.request().postDataJSON();
    order.puts.push(body);
    made.data.prefs.order = { ...(made.data.prefs.order ?? {}), ...body };
    return route.fulfill({ status: 204 });
  });
  return { ...made, state, order };
}

async function sound(target, name) {
  const overflow = await target.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `${name}: scrolls sideways by ${overflow}px`);
  const columns = await target.evaluate(() =>
    [...document.querySelectorAll(".cf .col")]
      .filter((el) => el.scrollWidth - el.clientWidth > 1)
      .map((el) => `${el.className} ${el.scrollWidth - el.clientWidth}px`),
  );
  assert.deepEqual(columns, [], `${name}: a column scrolls sideways`);
  // Rows a letter has not laid out yet (content-visibility) are laid out for the check.
  await target.evaluate(() =>
    document.querySelectorAll(".letter-block").forEach((el) => (el.style.contentVisibility = "visible")),
  );
  assert.deepEqual(await target.evaluate(cutTextIn), [], `${name}: cut text`);
  assert.equal(
    // "Bild wählen …" opens something (a space before); a word cut off has none.
    await target.evaluate(() => /\S…/.test(document.body.innerText)),
    false,
    `${name}: an ellipsis`,
  );
  await target.evaluate(() =>
    document.querySelectorAll(".letter-block").forEach((el) => (el.style.contentVisibility = "")),
  );
}
const shot = (page, name) => page.screenshot({ path: `${out}/${name}.png`, animations: "disabled" });
async function until(fn, what, ms = 8000) {
  const end = Date.now() + ms;
  while (!(await fn())) {
    if (Date.now() > end) throw new Error(`timed out: ${what}`);
    await new Promise((r) => setTimeout(r, 100));
  }
}
const sent = (state, method, re) => state.requests.filter(([m, p]) => m === method && re.test(p));
async function ready(page) {
  await page.locator("[data-pim-ready]").waitFor({ state: "attached" });
  await page.waitForTimeout(300);
}
const dialog = (page) => page.locator("dialog[open]").last();
/** Targets under 44 px in a part of the page (phone). */
const small = (page, scope) =>
  page.evaluate((scope) => {
    const out = [];
    for (const el of document.querySelectorAll(
      `${scope} :is(button, a[href], input:not([type=checkbox]):not([type=radio]):not([type=range]), select)`,
    )) {
      const r = el.getBoundingClientRect();
      if (!r.width || el.closest("[hidden], .sr-only")) continue;
      if (r.height < 43.5) out.push(`${el.tagName} ${(el.textContent || el.getAttribute("aria-label") || "").trim().slice(0, 30)} ${Math.round(r.height)}`);
    }
    return out;
  }, scope);

try {
  // ---- Desktop: the frame, the side bar, the list ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/contacts");
    await ready(page);
    const frame = await page.evaluate(() => {
      const f = document.querySelector("main > .cf.columns");
      const main = f.parentElement;
      return {
        pad: getComputedStyle(main).paddingTop,
        top: f.getBoundingClientRect().top - main.getBoundingClientRect().top,
        height: Math.round(f.getBoundingClientRect().height),
        border: getComputedStyle(f).borderTopWidth,
      };
    });
    assert.equal(frame.pad, "0px", "no page padding");
    assert.ok(frame.top <= 1, `nothing above the frame (${frame.top}px)`);
    assert.ok(frame.height >= 880, `the frame is as high as the window (${frame.height})`);
    assert.equal(frame.border, "0px", "no card");
    // "Neuer Kontakt" on top of the side bar; the search on top of the list.
    const action = await page.locator(".cf-side [data-new-contact]").boundingBox();
    assert.ok(action && action.y < 60, "Neuer Kontakt on top");
    assert.ok(await page.locator(".cf-list [data-contact-list] input[type=search]").isVisible());
    assert.equal(await page.locator(".cf-top").count(), 0, "no bar above the columns");
    // Opening reads list and overview; no suggestions, no search.
    assert.equal(sent(state, "GET", /^\/contacts$/).length, 1);
    assert.equal(sent(state, "GET", /^\/contacts\/overview$/).length, 1);
    assert.equal(sent(state, "GET", /^\/contacts\/suggestions/).length, 0, "suggestions not read");
    assert.match(await page.locator("[data-contact-side]").innerText(), /Familie\s+3[\s\S]*Fußballverein Süd\s+1[\s\S]*Mögliche Dubletten\s+1/);
    await sound(page, "desktop list");
    await shot(page, "01-desktop-liste");

    // One request per search, the suggestions with it.
    const before = state.requests.length;
    await page.getByRole("searchbox").first().pressSequentially("beisp", { delay: 40 });
    await page.locator("[data-contact-list] mark").first().waitFor();
    await page.waitForTimeout(400);
    const asked = state.requests.slice(before).map(([m, p]) => `${m} ${p}`);
    assert.deepEqual(asked, ["GET /contacts/search?q=beisp"], `one request: ${asked}`);
    await page.getByRole("searchbox").first().fill("");
    await page.waitForTimeout(200);

    // A group: its members only.
    await page.locator("[data-group='familie.vcf']").click();
    await until(async () => (await page.locator("[data-contact-list] .crow").count()) === 3, "group");
    assert.equal(await page.locator("#con-list-head").innerText(), "Familie");
    await shot(page, "02-desktop-gruppe");
    // Rename, then delete with a dialog; the contacts stay.
    await page.getByRole("button", { name: /Gruppe „Familie“/ }).click();
    await page.getByRole("menuitem", { name: "Umbenennen …" }).click();
    await dialog(page).locator("[data-group-name]").fill("Familie Beispiel");
    await dialog(page).locator("[data-group-save]").click();
    await until(async () => (await page.locator("#con-list-head").innerText()) === "Familie Beispiel", "renamed");
    assert.deepEqual(sent(state, "PUT", /^\/contacts\/groups\/familie\.vcf\?book=kontakte$/).at(-1)[2], { etag: '"g1-familie.vcf"', name: "Familie Beispiel" });
    await page.getByRole("button", { name: /Gruppe „Familie Beispiel“/ }).click();
    await page.getByRole("menuitem", { name: "Gruppe löschen …" }).click();
    assert.match(await dialog(page).innerText(), /Ihre Kontakte bleiben erhalten/);
    await shot(page, "03-desktop-gruppe-loeschen");
    await dialog(page).locator("[data-group-delete]").click();
    await until(async () => !(await page.locator("[data-group='familie.vcf']").count()), "group gone");
    assert.equal(sent(state, "DELETE", /^\/contacts\/groups\/familie\.vcf/).length, 1);
    assert.ok(state.contacts.some((c) => c.id === "anna.vcf"), "contacts stay");
    assert.equal(new URL(page.url()).searchParams.get("g"), null, "back to all contacts");

    // A new group, then a contact dragged onto it.
    await page.locator("[data-new-group]").click();
    await dialog(page).locator("[data-group-name]").fill("Nachbarn");
    await dialog(page).locator("[data-group-save]").click();
    await page.locator("[data-group='G1.vcf']").waitFor();
    await page.locator("[data-contact-side] [data-book]").click();
    await page.locator("[data-contact='tom.vcf']").dragTo(page.locator("[data-group='G1.vcf']"));
    await until(() => sent(state, "PUT", /^\/contacts\/groups\/G1\.vcf/).length === 1, "drop");
    assert.deepEqual(sent(state, "PUT", /^\/contacts\/groups\/G1\.vcf/)[0][2].add, ["tom.vcf"]);
    await page.getByText("Tom Richter ist jetzt in der Gruppe „Nachbarn“.").waitFor();
    await ctx.close();
  }

  // ---- The form: groups ticked, the row put in place, no list again ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/contacts?id=ben.vcf&edit=1");
    await page.locator("[data-contact-editor]").waitFor();
    await page.waitForTimeout(300);
    await sound(page, "desktop form");
    await shot(page, "04-desktop-bearbeiten");
    const editor = page.locator("[data-contact-editor]");
    await editor.getByLabel("Nachname").fill("Muster-Neu");
    await page.locator("[data-editor-groups]").getByLabel("Fußballverein Süd").uncheck();
    const lists = sent(state, "GET", /^\/contacts$/).length;
    await page.locator("[data-editor-save]").click();
    await page.locator("[data-contact-detail]").waitFor();
    await page.waitForTimeout(300);
    assert.deepEqual(sent(state, "PUT", /^\/contacts\/kontakte\/ben\.vcf$/)[0][2].contact, { last: "Muster-Neu" });
    assert.deepEqual(sent(state, "PUT", /^\/contacts\/groups\/verein\.vcf/)[0][2].remove, ["ben.vcf"]);
    assert.equal(sent(state, "GET", /^\/contacts$/).length, lists, "no list after the save");
    assert.match(await page.locator("[data-contact='ben.vcf']").innerText(), /Ben Muster-Neu/);
    // A conflict offers "Neu laden".
    await page.goto(base + "/contacts?id=ben.vcf&edit=1");
    await page.locator("[data-contact-editor]").waitFor();
    state.conflictOnce = true;
    await page.locator("[data-contact-editor]").getByLabel("Vorname").fill("Benno");
    await page.locator("[data-editor-save]").click();
    await page.getByRole("button", { name: "Neu laden" }).waitFor();
    await page.getByRole("button", { name: "Neu laden" }).click();
    await until(async () => (await page.locator("[data-contact-editor]").getByLabel("Vorname").inputValue()) === "Ben", "reloaded");
    // Deleting asks; the row goes, the list is not read again.
    const listsNow = sent(state, "GET", /^\/contacts$/).length;
    await page.getByRole("button", { name: "Kontakt löschen …" }).click();
    assert.match(await dialog(page).innerText(), /wird mit allen Nummern/);
    await dialog(page).getByRole("button", { name: "Kontakt löschen" }).click();
    await until(async () => !(await page.locator("[data-contact='ben.vcf']").count()), "deleted");
    assert.equal(sent(state, "GET", /^\/contacts$/).length, listsNow, "no list after deleting");

    // A new contact with a picture cut square.
    await page.locator("[data-new-contact]").click();
    await page.locator("[data-contact-editor]").waitFor();
    await page.locator("[data-contact-editor]").getByLabel("Vorname").fill("Clara");
    await page.locator("[data-contact-editor] input[type=file]").setInputFiles({
      name: "foto.png",
      mimeType: "image/png",
      buffer: png(160, 80),
    });
    await dialog(page).locator("[data-crop-frame] img").waitFor();
    await page.waitForTimeout(200);
    await dialog(page).locator("[data-crop-frame]").press("ArrowRight");
    await shot(page, "05-desktop-bild-zuschneiden");
    await dialog(page).locator("[data-crop-take]").click();
    await page.locator("[data-editor-save]").click();
    await until(() => sent(state, "PUT", /\/photo\?etag=/).length === 1, "picture sent");
    assert.equal(sent(state, "PUT", /\/photo\?etag=/)[0][2], "(multipart)");
    await page.locator("[data-contact-detail]").waitFor();
    await ctx.close();
  }

  // ---- Import, export, e-mail to Mail's compose ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/contacts?id=ben.vcf");
    await page.locator("[data-contact-detail]").waitFor();
    await page.getByRole("button", { name: "Import und Export" }).click();
    await page.getByRole("menuitem", { name: /Exportieren/ }).click();
    await until(() => sent(state, "GET", /\/export$/).length === 1, "export");
    await page.getByRole("button", { name: "Import und Export" }).click();
    await page.getByRole("menuitem", { name: /Importieren/ }).click();
    await dialog(page).waitFor();
    await dialog(page).getByRole("button", { name: "Abbrechen" }).click();
    await page.getByRole("button", { name: "b.muster@firma.example", exact: true }).click();
    await until(() => new URL(page.url()).pathname === "/mail/compose", "compose");
    assert.equal(new URL(page.url()).searchParams.get("to"), "b.muster@firma.example");
    await ctx.close();
  }
  {
    // Without Mail: a mailto: link, no suggestions' entry.
    const { page, ctx } = await open({ change: (s) => (withExtras(s), (s.mail = false)) });
    await page.goto(base + "/contacts?id=ben.vcf");
    await page.locator("[data-contact-detail]").waitFor();
    assert.equal(await page.locator("a[href='mailto:ben.muster@mail.example']").count(), 2, "the action and the address");
    assert.equal(await page.locator("[data-side-suggestions]").count(), 0);
    await ctx.close();
  }

  // ---- Suggestions only when shown ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/contacts");
    await ready(page);
    await page.locator("[data-side-suggestions]").click();
    await page.locator("[data-suggestions]").getByText("Sparkasse Beispiel").waitFor();
    assert.equal(sent(state, "GET", /^\/contacts\/suggestions$/).length, 1);
    await ctx.close();
  }

  // ---- Duplicates merged field by field ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/contacts");
    await ready(page);
    await page.locator("[data-side-duplicates]").click();
    await page.locator("[data-merge]").getByText("Kontakte zusammenführen").waitFor();
    await page.locator("[data-merge] fieldset").first().waitFor();
    await sound(page, "duplicates");
    await shot(page, "06-desktop-dubletten");
    // Numbers and e-mail addresses joined; the other's note is taken (Anna has none).
    assert.equal(await page.locator("[data-merge] fieldset").count(), 2);
    await page.locator("[data-merge-run]").click();
    assert.match(await dialog(page).innerText(), /wird danach gelöscht/);
    await dialog(page).locator("[data-merge-confirm]").click();
    await until(() => sent(state, "POST", /^\/contacts\/merge$/).length === 1, "merge");
    const body = sent(state, "POST", /^\/contacts\/merge$/)[0][2];
    assert.equal(body.keep.id, "anna.vcf");
    assert.equal(body.drop.id, "anna2.vcf");
    assert.equal(body.contact.phones.length, 2, "numbers joined");
    assert.match(body.contact.notes, /Aus dem alten Handy/);
    await until(async () => !(await page.locator("[data-side-duplicates]").count()), "no duplicates left");
    assert.equal(await page.locator("[data-contact='anna2.vcf']").count(), 0);
    await ctx.close();
  }

  // ---- Two address books sorted with the grip's keys, saved for the account ----
  {
    const { page, ctx, order } = await open({
      change: (s) => {
        withExtras(s);
        s.books = [
          { id: "kontakte", name: "Kontakte", count: 12 },
          { id: "arbeit", name: "Arbeit", count: 0 },
        ];
      },
    });
    await page.goto(base + "/contacts");
    await ready(page);
    const grips = page.locator("[data-contact-side] .sort-grip");
    assert.equal(await grips.count(), 2, "a grip per book");
    await grips.first().focus();
    await page.keyboard.press("ArrowDown");
    await until(() => order.puts.length === 1, "order saved");
    assert.deepEqual(order.puts[0], { contacts: ["arbeit", "kontakte"] });
    await ctx.close();
  }

  // ---- Dark and English ----
  {
    const { page, ctx } = await open({ theme: "dunkel", lang: "en" });
    await page.goto(base + "/contacts?id=ben.vcf");
    await page.locator("[data-contact-detail]").waitFor();
    assert.ok(await page.getByRole("button", { name: "New contact" }).first().isVisible());
    assert.ok(await page.locator("[data-contact-side]").getByText("Possible duplicates", { exact: true }).isVisible());
    await sound(page, "dark English");
    await shot(page, "07-desktop-dunkel-englisch");
    await ctx.close();
  }

  // ---- 800 px: levels with "‹ Zurück" ----
  {
    const { page, ctx } = await open({ width: 800 });
    await page.goto(base + "/contacts");
    await ready(page);
    await page.locator("[data-contact-list] [data-back]").waitFor();
    await sound(page, "800 list");
    await shot(page, "08-800-liste");
    await page.locator("[data-contact='ben.vcf']").click();
    await page.locator("[data-contact-detail]").waitFor();
    await sound(page, "800 contact");
    await page.locator(".col-contact [data-back]").click();
    await page.locator("[data-contact='ben.vcf']").waitFor();
    await ctx.close();
  }

  // ---- The phone: levels, round actions, the form as a sheet ----
  for (const theme of ["hell", "dunkel"]) {
    const { page, ctx } = await open({ width: 390, phone: true, theme });
    await page.goto(base + "/contacts");
    await ready(page);
    const listBox = await page.locator(".cf.levels").evaluate((el) => getComputedStyle(el).marginLeft);
    assert.notEqual(listBox, "0px", "edge to edge (the page's padding given up)");
    assert.ok(await page.locator(".bottombar .bar-new").isVisible(), "Neuer Kontakt at the bottom");
    await sound(page, `phone list ${theme}`);
    await shot(page, `10-handy-liste-${theme}`);
    await page.locator("[data-contact='ben.vcf']").click();
    await page.locator("[data-contact-detail]").waitFor();
    const acts = page.locator("[data-contact-actions]");
    assert.equal(await acts.locator("a[href^='tel:']").count(), 1, "Anrufen");
    assert.equal(await acts.locator("a[href^='sms:']").count(), 1, "Nachricht");
    assert.equal(await acts.getByRole("button", { name: "E-Mail" }).count(), 1, "E-Mail");
    assert.equal(await acts.locator("a[href*='openstreetmap'], a[href*='maps.apple']").count(), 1, "Karte");
    assert.deepEqual(await small(page, ".col-contact"), [], "targets of 44 px");
    assert.deepEqual(await small(page, ".bottombar"), [], "bar targets of 44 px");
    await sound(page, `phone contact ${theme}`);
    await shot(page, `11-handy-kontakt-${theme}`);
    await page.locator("[data-bar-edit]").click();
    const sheet = page.locator("[data-editor-sheet]");
    await sheet.waitFor();
    const box = await sheet.boundingBox();
    assert.ok(box.x === 0 && box.y === 0 && box.width === 390, "the form covers the screen");
    await sound(page, `phone form ${theme}`);
    await shot(page, `12-handy-bearbeiten-${theme}`);
    await page.locator("[data-editor-cancel]").click();
    await page.locator("[data-contact-detail]").waitFor();
    await page.goto(base + "/contacts?side=1");
    await page.locator("[data-contact-side]").waitFor();
    await sound(page, `phone side ${theme}`);
    await shot(page, `13-handy-gruppen-${theme}`);
    await page.locator("[data-group='verein.vcf']").click();
    await until(async () => (await page.locator("[data-contact-list] .crow").count()) === 1, "group on the phone");
    await ctx.close();
  }

  // ---- Unavailable and empty ----
  {
    const { page, ctx } = await open({ change: (s) => (s.unavailable = "503") });
    await page.goto(base + "/contacts");
    await page.locator('[data-pim-state="unavailable"]').getByText(/nicht verfügbar/).waitFor();
    assert.equal(await page.locator("[data-pim-ready]").count(), 0, "no list");
    await sound(page, "unavailable");
    await shot(page, "14-nicht-verfuegbar");
    await ctx.close();
  }
  {
    const { page, ctx } = await open({ change: (s) => ((s.contacts = []), (s.groups = [])) });
    await page.goto(base + "/contacts");
    await page.locator("[data-contacts-empty]").waitFor();
    await sound(page, "empty");
    await shot(page, "15-leer");
    await ctx.close();
  }

  const real = errors.filter((e) => !/503|calendar\/stream/.test(e));
  assert.deepEqual(real, [], "page errors");
  console.log("test-ui-kontakte-113: ok");
} finally {
  await browser.close();
}
