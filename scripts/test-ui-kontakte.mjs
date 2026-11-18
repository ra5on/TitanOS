// 1.11 (step 16), Kontakte (docs/KALENDER.md, drafts 10–13, 50–53) with
// isolated data (test-ui-kontakte-fixtures.mjs; the clock fixed at
// Tuesday, 17 November 2026 in Berlin). Desktop 1440 px light and dark:
// the column (address book, suggestions of T-Mail apart), the list by
// letter with pictures or initials and birthdays of this week in words
// (today, coming, past), the contact (tel: links, "E-Mail schreiben" opens
// T-Mail's compose with the address, "Termin" T-Kalender's form with the
// name), the form new (POST with only filled fields, then the picture as
// a file) and changed (PUT with the etag and only the changed known
// fields – never a label, region and street lines kept –, then the
// picture from the NAS files as {root,path}), a conflict with "Neu laden",
// the delete dialog (danger button left of "Abbrechen", focus not on it,
// DELETE with the etag), the search with folded marks and T-Mail's
// suggestions ("Als Kontakt übernehmen" → from-suggestion), the
// suggestions page, import with a summary, export. Without T-Mail's
// release: no "E-Mail schreiben", no suggestions entry, the suggestions
// page explains. Unavailable (503) shows the message, never a list.
// English once. Phone 390 px light and dark: list, contact with three big
// buttons, form, search; targets ≥ 44 px. Once in a real desktop window
// with "Ablage › Neuer Kontakt". Every screen: no sideways scroll, no cut
// text (long names, addresses and e-mail addresses wrap).
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5211 node scripts/test-ui-kontakte.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import {
  calendarState,
  installCalendar,
} from "./test-ui-kalender-fixtures.mjs";
import {
  LONG_MAIL,
  LONG_NAME,
  contactsState,
  installContacts,
} from "./test-ui-kontakte-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5211";
const out = process.env.UI_OUTPUT || "out/validation/kontakte";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const NOW = new Date("2026-11-17T12:29:00+01:00");
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

/** A context with Kontakte's data. `phone`: the phone's frame; `desk`: the real desktop. */
async function open({
  width = 1440,
  height = 1000,
  theme = "hell",
  lang = "de",
  phone = false,
  desk = false,
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
          timezoneId: "Europe/Berlin",
        }
      : { viewport: { width, height }, timezoneId: "Europe/Berlin" },
  );
  await made.ctx.clock.setFixedTime(NOW);
  if (phone || desk)
    await made.ctx.addInitScript(() =>
      localStorage.removeItem("titanos.layout"),
    );
  const calendar = calendarState();
  await installCalendar(made.ctx, calendar);
  const state = contactsState();
  change(state);
  await installContacts(made.ctx, state);
  return { ...made, state, calendar };
}

/** No sideways scroll and no cut text in a page or frame. */
async function sound(target, name) {
  const overflow = await target.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `${name}: scrolls sideways by ${overflow}px`);
  // The columns scroll on their own, but never sideways.
  const columns = await target.evaluate(() =>
    [...document.querySelectorAll(".con > *")]
      .filter((el) => el.scrollWidth - el.clientWidth > 1)
      .map((el) => `${el.className} ${el.scrollWidth - el.clientWidth}px`),
  );
  assert.deepEqual(columns, [], `${name}: a column scrolls sideways`);
  assert.deepEqual(await target.evaluate(cutTextIn), [], `${name}: cut text`);
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
async function ready(page, mode = "") {
  await page
    .locator(`[data-pim-ready]${mode ? `[data-mode="${mode}"]` : ""}`)
    .waitFor();
  await page
    .locator(
      "[data-contact-list] .crow, [data-contact-detail], [data-contact-editor], [data-suggestions]",
    )
    .first()
    .waitFor();
  await page.waitForTimeout(300);
}
const dialog = (page) => page.locator("dialog[open]").last();
const enc = encodeURIComponent;

/** Targets under 44 px in a part of the page (phone). */
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
        `${scope} button, ${scope} input, ${scope} a, ${scope} select, ${scope} textarea`,
      ),
    ]
      .filter(
        (el) =>
          el.getClientRects().length &&
          getComputedStyle(el).visibility === "visible" &&
          !el.classList.contains("sr-only") &&
          !el.closest(".sr-only"),
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

/** Every link of a part of the page: only http(s)/mailto/tel, external ones with rel. */
async function safeLinks(page, scope) {
  const bad = await page.evaluate((scope) => {
    const found = [];
    for (const a of document.querySelectorAll(`${scope} a[href]`)) {
      const href = a.getAttribute("href");
      if (href.startsWith("/")) continue;
      if (!/^(https?:|mailto:|tel:)/i.test(href)) found.push(href);
      if (
        /^https?:/i.test(href) &&
        !/noopener/.test(a.rel + "") &&
        !/noreferrer/.test(a.rel + "")
      )
        found.push(`${href} without rel`);
      if (
        /^https?:/i.test(href) &&
        a.textContent.trim() !== href.replace(/\/$/, "") &&
        a.textContent.trim() !== href
      )
        found.push(`${href} shows "${a.textContent.trim()}"`);
    }
    return found;
  }, scope);
  assert.deepEqual(bad, [], "links");
}

try {
  // ---- Desktop 1440: list, column and contact in light and dark ----
  for (const theme of ["hell", "graphit"]) {
    const { page, ctx } = await open({ theme });
    await page.goto(base + "/contacts?id=ben.vcf");
    await ready(page, "show");
    await page.locator("[data-contact-detail] h2").waitFor();
    const column = await page.locator("nav.books").innerText();
    assert.match(column, /Adressbücher/);
    assert.match(column, /Kontakte\s*11/);
    assert.match(column, /Aus T-Mail/);
    assert.match(column, /Vorschläge aus gesendeten Mails\s*3/);
    // Letters after folding (Krüger under K), one heading per letter.
    const letters = await page
      .locator("[data-contact-list] .letter")
      .allInnerTexts();
    assert.deepEqual(letters, ["A", "B", "G", "H", "K", "M", "R"]);
    // Birthdays of this week in words: coming, today, past (without year).
    const row = (id) => page.locator(`[data-contact="${id}"]`);
    assert.match(await row("ben.vcf").innerText(), /Geburtstag am Freitag/);
    assert.match(await row("tom.vcf").innerText(), /hat heute Geburtstag/);
    assert.match(
      await row("dennis.vcf").innerText(),
      /hatte am Montag Geburtstag/,
    );
    assert.doesNotMatch(await row("anna.vcf").innerText(), /Geburtstag/);
    // Pictures where there are some, initials otherwise.
    assert.equal(await row("ben.vcf").locator("img").count(), 1);
    assert.equal(await row("tom.vcf").locator(".av").innerText(), "TR");
    assert.equal(await row("schule.vcf").locator(".av").innerText(), "G");
    // The chosen contact is marked as text for screen readers, not only by color.
    assert.equal(await row("ben.vcf").getAttribute("aria-current"), "true");
    // The contact: actions, fields with their kind, birthday with the age.
    const detail = page.locator("[data-contact-detail]");
    const text = await detail.innerText();
    for (const want of [
      /Ben Muster/,
      /Anrufen/,
      /E-Mail schreiben/,
      /Termin/,
      /Telefon · Mobil/,
      /Telefon · Arbeit/,
      /E-Mail · Privat/,
      /Adresse · Privat/,
      /Lindenallee 7\s+50667 Köln\s+Deutschland/,
      /20\. November 1986 · wird am Freitag 40/,
      /Allergisch gegen Nüsse/,
      /Im Adressbuch „Kontakte“ · zuletzt geändert am 3\. November 2026/,
    ])
      assert.match(text, want);
    assert.equal(
      await detail.getByRole("link", { name: "Anrufen" }).getAttribute("href"),
      "tel:+491517654321",
    );
    assert.equal(
      await detail
        .getByRole("link", { name: "+49 221 555 0142" })
        .getAttribute("href"),
      "tel:+492215550142",
    );
    await safeLinks(page, "[data-contact-detail]");
    await sound(page, `desktop ${theme}`);
    await shot(page, `kontakte-${theme}`);
    await ctx.close();
  }

  // ---- Long values wrap; links of the note are safe ----
  {
    const { page, ctx } = await open();
    await page.goto(base + "/contacts?id=lang.vcf");
    await ready(page, "show");
    await page.locator("[data-contact-detail] h2").waitFor();
    assert.equal(
      await page.locator("[data-contact-detail] h2").innerText(),
      LONG_NAME,
    );
    assert.match(
      await page.locator("[data-contact-detail]").innerText(),
      new RegExp(LONG_MAIL.replace(/\./g, "\\.")),
    );
    const link = page.locator("[data-contact-detail] .pre a");
    assert.equal(await link.getAttribute("rel"), "noopener noreferrer");
    assert.equal(await link.getAttribute("target"), "_blank");
    await safeLinks(page, "[data-contact-detail]");
    await sound(page, "long values");
    await shot(page, "kontakt-lang");
    // A file name that needs encoding; a label of its own and the region shown.
    await page.locator('[data-contact="Oma Hilde+1@icloud.vcf"]').click();
    await page
      .locator('[data-contact-detail="Oma Hilde+1@icloud.vcf"]')
      .waitFor();
    const oma = await page.locator("[data-contact-detail]").innerText();
    assert.match(oma, /Telefon · Festnetz/);
    assert.match(oma, /Am Weiher 3\s+Hinterhaus\s+51147 Köln\s+NRW/);
    await ctx.close();
  }

  // ---- "E-Mail schreiben" opens T-Mail's compose with the address ----
  {
    const { page, ctx } = await open();
    await page.goto(base + "/contacts?id=ben.vcf");
    await ready(page, "show");
    await page.getByRole("button", { name: "E-Mail schreiben" }).click();
    await until(
      () => new URL(page.url()).pathname === "/mail/compose",
      "compose",
    );
    assert.equal(
      new URL(page.url()).searchParams.get("to"),
      "ben.muster@mail.example",
    );
    await ctx.close();
  }

  // ---- "Termin" opens T-Kalender's form with the name ----
  {
    const { page, ctx } = await open();
    await page.goto(base + "/contacts?id=ben.vcf");
    await ready(page, "show");
    await page.getByRole("button", { name: "Termin" }).click();
    await until(
      () => new URL(page.url()).pathname === "/calendar/event",
      "event form",
    );
    const u = new URL(page.url());
    assert.equal(u.searchParams.get("title"), "Ben Muster");
    assert.equal(u.searchParams.get("date"), "2026-11-17");
    assert.equal(u.searchParams.get("time"), "13:00");
    assert.equal(u.searchParams.get("back"), "/contacts?id=ben.vcf");
    await page.locator("[data-event-form]").waitFor();
    assert.equal(await page.locator("#t-title").inputValue(), "Ben Muster");
    await ctx.close();
  }

  // ---- Changing a contact: only the changed fields, the etag, then the NAS picture ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/contacts?id=ben.vcf");
    await ready(page, "show");
    await page
      .locator("[data-contact-detail]")
      .getByRole("button", { name: "Bearbeiten" })
      .click();
    await page.locator("[data-contact-editor]").waitFor();
    assert.match(new URL(page.url()).search, /edit=1/);
    const form = page.locator("[data-contact-editor]");
    assert.equal(await form.getByLabel("Vorname").inputValue(), "Ben");
    assert.equal(
      await form.getByRole("checkbox", { name: "Jahr unbekannt" }).isChecked(),
      false,
    );
    await sound(page, "edit");
    await shot(page, "kontakt-bearbeiten");
    // The kind of the work number, one e-mail more, the note.
    await form.getByLabel("Art von Telefon").nth(1).selectOption("home");
    await form.getByRole("button", { name: "E-Mail", exact: true }).click();
    await form.getByLabel("E-Mail-Adresse").last().fill("ben@verein.example");
    await form.getByLabel("Notiz").fill("Spielt dienstags Tennis.");
    // A picture from the files of the NAS (pictures only, one).
    await form.getByRole("button", { name: "Aus Dateien des NAS" }).click();
    await dialog(page).getByText("Documents").first().click();
    await dialog(page).getByText("ben.jpg").waitFor();
    assert.equal(
      await dialog(page).getByText("notizen.txt").count(),
      0,
      "pictures only",
    );
    await dialog(page).getByText("ben.jpg").click();
    await dialog(page)
      .getByRole("button", { name: /Übernehmen/ })
      .click();
    await form
      .getByText("Neues Bild – wird beim Speichern übernommen")
      .waitFor();
    await sound(page, "edit with picture");
    await shot(page, "kontakt-bearbeiten-bild");
    await page.getByRole("button", { name: "Speichern" }).click();
    await until(
      () =>
        sent(state, "PUT", /^\/contacts\/kontakte\/ben\.vcf\/photo/).length ===
        1,
      "photo PUT",
    );
    const [put] = sent(state, "PUT", /^\/contacts\/kontakte\/ben\.vcf$/);
    assert.deepEqual(put[2], {
      etag: '"1-ben.vcf"',
      contact: {
        phones: [
          { type: "mobile", value: "+49 151 7654321" },
          { type: "home", value: "+49 221 555 0142" },
        ],
        emails: [
          { type: "home", value: "ben.muster@mail.example" },
          { type: "work", value: "b.muster@firma.example" },
          { type: "home", value: "ben@verein.example" },
        ],
        notes: "Spielt dienstags Tennis.",
      },
    });
    const [photo] = sent(
      state,
      "PUT",
      /^\/contacts\/kontakte\/ben\.vcf\/photo/,
    );
    assert.equal(
      photo[1],
      `/contacts/kontakte/ben.vcf/photo?etag=${enc('"2-ben.vcf"')}`,
    );
    assert.deepEqual(photo[2], { root: "share/Documents", path: "ben.jpg" });
    await until(
      () => !new URL(page.url()).search.includes("edit"),
      "back to the contact",
    );
    await page
      .locator("[data-contact-detail]")
      .getByText("Spielt dienstags Tennis.")
      .waitFor();
    // The list's picture loads anew after the change.
    assert.match(
      await page.locator('[data-contact="ben.vcf"] img').getAttribute("src"),
      /\/photo\?v=/,
    );
    await ctx.close();
  }

  // ---- A card of a device: an unread label, region and street lines stay unsent ----
  {
    const { page, ctx, state } = await open();
    const id = "Oma Hilde+1@icloud.vcf";
    await page.goto(base + `/contacts?id=${enc(id)}&edit=1`);
    await page.locator("[data-contact-editor]").waitFor();
    const form = page.locator("[data-contact-editor]");
    assert.match(
      await form
        .getByLabel("Art von Telefon")
        .locator("option:checked")
        .innerText(),
      /Festnetz/,
    );
    assert.equal(
      await form.getByLabel("Region oder Bundesland").inputValue(),
      "NRW",
    );
    await form.getByLabel("Vorname").fill("Omi");
    await form.getByLabel("Vorname").press("Enter"); // Enter saves
    await until(
      () => sent(state, "PUT", /^\/contacts\/kontakte\//).length === 1,
      "PUT",
    );
    const [put] = sent(state, "PUT", /^\/contacts\/kontakte\//);
    assert.equal(put[1], `/contacts/kontakte/${enc(id)}`);
    assert.deepEqual(put[2], { etag: `"1-${id}"`, contact: { first: "Omi" } });
    await ctx.close();
  }

  // ---- A conflict says so; "Neu laden" reads the contact again ----
  {
    const { page, ctx, state } = await open({
      change: (s) => (s.conflictOnce = true),
    });
    await page.goto(base + "/contacts?id=tom.vcf&edit=1");
    await page.locator("[data-contact-editor]").waitFor();
    await page
      .locator("[data-contact-editor]")
      .getByLabel("Firma")
      .fill("Richter & Söhne");
    await page.getByRole("button", { name: "Speichern" }).click();
    const alert = page.locator(".notice-error", {
      hasText: "inzwischen geändert",
    });
    await alert.waitFor();
    await sound(page, "conflict");
    await shot(page, "kontakt-konflikt");
    const reads = sent(state, "GET", /^\/contacts\/kontakte\/tom\.vcf$/).length;
    await alert.getByRole("button", { name: "Neu laden" }).click();
    await until(
      () =>
        sent(state, "GET", /^\/contacts\/kontakte\/tom\.vcf$/).length > reads,
      "read again",
    );
    await until(
      async () =>
        (await page
          .locator("[data-contact-editor]")
          .getByLabel("Firma")
          .inputValue()) === "",
      "form of the current state",
    );
    await ctx.close();
  }

  // ---- New contact: a name is needed; only filled fields; then the picture file ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/contacts");
    await ready(page);
    assert.match(
      await page.locator(".right").innerText(),
      /Wähle links einen Kontakt/,
    );
    await page.getByRole("button", { name: "Neuer Kontakt" }).click();
    await page.locator("[data-contact-editor]").waitFor();
    assert.equal(
      await page.getByRole("button", { name: "Kontakt löschen …" }).count(),
      0,
      "no danger zone for a new one",
    );
    await page.getByRole("button", { name: "Speichern" }).click();
    await page.getByText("Bitte einen Namen, eine Firma").waitFor();
    assert.equal(sent(state, "POST", /^\/contacts$/).length, 0);
    const form = page.locator("[data-contact-editor]");
    await form.getByLabel("Vorname").fill("Jonas");
    await form.getByLabel("Nachname").fill("Trainer");
    await form.getByRole("button", { name: "Telefon", exact: true }).click();
    await form.getByLabel("Telefonnummer").fill("+49 171 5550001");
    await form.getByRole("button", { name: "Adresse", exact: true }).click();
    await form.getByLabel("Straße und Hausnummer").fill("Sportplatzweg 2");
    await form.getByLabel("Ort").fill("Bonn");
    await form.getByRole("checkbox", { name: "Jahr unbekannt" }).check();
    await form.getByLabel("Tag").selectOption("29");
    await form.getByLabel("Monat").selectOption("2");
    await form
      .locator('input[type="file"]')
      .setInputFiles({ name: "jonas.png", mimeType: "image/png", buffer: PNG });
    await form.getByText("Neues Bild").waitFor();
    assert.equal(
      await form.locator(".photo-row img").count(),
      1,
      "preview of the chosen picture",
    );
    await sound(page, "new contact");
    await shot(page, "kontakt-neu");
    await page.getByRole("button", { name: "Speichern" }).click();
    await until(
      () =>
        sent(state, "PUT", /^\/contacts\/kontakte\/neu-1\.vcf\/photo/)
          .length === 1,
      "photo of the new one",
    );
    assert.deepEqual(sent(state, "POST", /^\/contacts$/)[0][2], {
      book: "kontakte",
      contact: {
        first: "Jonas",
        last: "Trainer",
        phones: [{ type: "mobile", value: "+49 171 5550001" }],
        addresses: [
          {
            type: "home",
            street: "Sportplatzweg 2",
            city: "Bonn",
            postcode: "",
            region: "",
            country: "",
          },
        ],
        birthday: "--02-29",
      },
    });
    const [photo] = sent(
      state,
      "PUT",
      /^\/contacts\/kontakte\/neu-1\.vcf\/photo/,
    );
    assert.equal(photo[2], "(multipart)");
    assert.match(photo[1], /etag=/);
    await until(
      () => new URL(page.url()).searchParams.get("id") === "neu-1.vcf",
      "shows the new contact",
    );
    await page.locator('[data-contact="neu-1.vcf"]').waitFor();
    await ctx.close();
  }

  // ---- Deleting: its own dialog, danger left of "Abbrechen", never by Enter ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/contacts?id=ben.vcf&edit=1");
    await page.locator("[data-contact-editor]").waitFor();
    await page.getByRole("button", { name: "Kontakt löschen …" }).click();
    const d = dialog(page);
    await d.waitFor();
    assert.match(await d.innerText(), /„Ben Muster“ löschen\?/);
    const danger = d.getByRole("button", { name: "Kontakt löschen" });
    const cancel = d.getByRole("button", { name: "Abbrechen" });
    assert.ok(
      (await danger.boundingBox()).x < (await cancel.boundingBox()).x,
      "danger left of Abbrechen",
    );
    assert.equal(
      await danger.evaluate((el) => el === document.activeElement),
      false,
      "focus not on the danger button",
    );
    await sound(page, "delete dialog");
    await shot(page, "kontakt-loeschen");
    await page.keyboard.press("Enter");
    await page.waitForTimeout(300);
    assert.equal(
      sent(state, "DELETE", /^\/contacts\/kontakte\/ben\.vcf/).length,
      0,
      "Enter never deletes",
    );
    if (!(await d.isVisible()))
      await page.getByRole("button", { name: "Kontakt löschen …" }).click();
    await dialog(page).getByRole("button", { name: "Kontakt löschen" }).click();
    await until(
      () =>
        sent(state, "DELETE", /^\/contacts\/kontakte\/ben\.vcf/).length === 1,
      "DELETE",
    );
    assert.equal(
      sent(state, "DELETE", /^\/contacts\/kontakte\/ben\.vcf/)[0][1],
      `/contacts/kontakte/ben.vcf?etag=${enc('"1-ben.vcf"')}`,
    );
    await until(
      async () =>
        (await page.locator('[data-contact="ben.vcf"]').count()) === 0,
      "gone from the list",
    );
    assert.equal(new URL(page.url()).search, "");
    await ctx.close();
  }

  // ---- Search: folded marks, T-Mail's suggestions, "Als Kontakt übernehmen" ----
  for (const theme of ["graphit", "hell"]) {
    const { page, ctx, state } = await open({ theme });
    await page.goto(base + "/contacts");
    await ready(page);
    await page.getByLabel("Kontakte durchsuchen").fill("beispiel");
    await until(
      async () =>
        /4 Kontakte und 1 Vorschlag aus T-Mail für „beispiel“/.test(
          await page.locator("[data-contact-list] .count").innerText(),
        ),
      "count of the hits",
    );
    const marks = await page
      .locator("[data-contact-list] mark")
      .allInnerTexts();
    assert.ok(
      marks.length >= 5 && marks.every((m) => m.toLowerCase() === "beispiel"),
      `marks ${marks}`,
    );
    assert.match(
      await page.locator("[data-contact-list]").innerText(),
      /Vorschläge aus T-Mail\s+Adressen, an die du geschrieben hast/,
    );
    assert.match(
      await page
        .locator('[data-suggestion="service@bank.example"]')
        .innerText(),
      /service@bank\.example · 14 gesendete Mails/,
    );
    assert.ok(
      sent(state, "GET", /^\/contacts\/suggestions\?q=beispiel$/).length >= 1,
    );
    await sound(page, `search ${theme}`);
    await shot(page, `kontakte-suche-${theme}`);
    if (theme === "hell") {
      await page
        .locator('[data-suggestion="service@bank.example"]')
        .getByRole("button", { name: "Als Kontakt übernehmen" })
        .click();
      await until(
        () => sent(state, "POST", /^\/contacts\/from-suggestion$/).length === 1,
        "from-suggestion",
      );
      assert.deepEqual(
        sent(state, "POST", /^\/contacts\/from-suggestion$/)[0][2],
        { addr: "service@bank.example", book: "kontakte" },
      );
      await until(
        () => new URL(page.url()).searchParams.get("id") === "neu-1.vcf",
        "opens the new contact",
      );
      await page.locator('[data-contact-detail="neu-1.vcf"]').waitFor();
      // Folded: "kruger" finds and marks "Krüger"; numbers find a contact.
      await page.getByLabel("Kontakte durchsuchen").fill("kruger");
      await until(
        async () =>
          (
            await page.locator("[data-contact-list] mark").allInnerTexts()
          ).join() === "Krüger",
        "folded mark",
      );
      await page.getByLabel("Kontakte durchsuchen").fill("0151 765");
      await until(
        async () =>
          /1 Kontakt für „0151 765“/.test(
            await page.locator("[data-contact-list] .count").innerText(),
          ),
        "number search",
      );
      assert.match(
        await page.locator("[data-contact-list]").innerText(),
        /Ben Muster/,
      );
    }
    await ctx.close();
  }

  // ---- The suggestions page ----
  {
    const { page, ctx } = await open();
    await page.goto(base + "/contacts?view=suggestions");
    await page.locator("[data-suggestion]").first().waitFor();
    assert.equal(await page.locator("[data-suggestion]").count(), 3);
    assert.match(
      await page.locator("nav.books .book.on").innerText(),
      /Vorschläge aus gesendeten Mails/,
    );
    await page.getByLabel("Vorschläge durchsuchen").fill("paket");
    await until(
      async () => (await page.locator("[data-suggestion]").count()) === 1,
      "filter",
    );
    await page.getByLabel("Vorschläge durchsuchen").fill("");
    await sound(page, "suggestions");
    await shot(page, "vorschlaege");
    await ctx.close();
  }

  // ---- Import with a summary; export ----
  {
    const { page, ctx, state } = await open();
    await page.goto(base + "/contacts");
    await ready(page);
    await page.getByRole("button", { name: "Importieren (.vcf)" }).click();
    const d = dialog(page);
    await d.locator('input[type="file"]').setInputFiles({
      name: "icloud.vcf",
      mimeType: "text/vcard",
      buffer: Buffer.from(
        "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:X\r\nEND:VCARD\r\n",
      ),
    });
    await d.getByRole("radio", { name: "Ersetzen" }).click();
    await d.getByRole("button", { name: "Importieren" }).click();
    await d.locator("[data-import-result]").waitFor();
    assert.match(
      await d.locator("[data-import-result]").innerText(),
      /Importiert: 4 neu, 1 ersetzt, 2 übersprungen\.[\s\S]*Kaputte Karte: keine lesbare Karte/,
    );
    assert.equal(
      sent(
        state,
        "POST",
        /^\/contacts\/books\/kontakte\/import\?onDuplicate=replace$/,
      ).length,
      1,
    );
    await sound(page, "import");
    await shot(page, "import");
    await d.getByRole("button", { name: "Schließen" }).click();
    const download = page.waitForEvent("download");
    await page.getByRole("button", { name: "Exportieren (.vcf)" }).click();
    assert.equal((await download).suggestedFilename(), "kontakte.vcf");
    await ctx.close();
  }

  // ---- Without T-Mail's release: no "E-Mail schreiben", no suggestions; explained ----
  {
    const { page, ctx, state } = await open({
      change: (s) => (s.mail = false),
    });
    await page.goto(base + "/contacts?id=ben.vcf");
    await ready(page, "show");
    await page.locator("[data-contact-detail] h2").waitFor();
    assert.equal(
      await page.getByRole("button", { name: "E-Mail schreiben" }).count(),
      0,
    );
    assert.doesNotMatch(
      await page.locator("nav.books").innerText(),
      /Vorschläge|T-Mail/,
    );
    await page.getByLabel("Kontakte durchsuchen").fill("beispiel");
    await until(
      async () =>
        /^4 Kontakte für „beispiel“$/.test(
          (await page.locator("[data-contact-list] .count").innerText()).trim(),
        ),
      "hits without suggestions",
    );
    assert.equal(
      sent(state, "GET", /^\/contacts\/suggestions/).length,
      0,
      "no suggestions asked",
    );
    await sound(page, "without T-Mail");
    await shot(page, "ohne-tmail");
    await page.goto(base + "/contacts?view=suggestions");
    await page.locator("[data-suggestions-off]").waitFor();
    assert.match(
      await page.locator("[data-suggestions-off]").innerText(),
      /Für dein Konto ist T-Mail nicht freigegeben/,
    );
    assert.equal(await page.locator("[data-suggestion]").count(), 0);
    await sound(page, "without T-Mail suggestions");
    await shot(page, "ohne-tmail-vorschlaege");
    await ctx.close();
  }

  // ---- Unavailable: the message, never an empty list ----
  {
    const { page, ctx } = await open({
      change: (s) => (s.unavailable = "503"),
    });
    await page.goto(base + "/contacts");
    await page.locator('[data-pim-state="unavailable"]').waitFor();
    assert.match(
      await page.locator('[data-pim-state="unavailable"]').innerText(),
      /Kontakte sind gerade nicht verfügbar/,
    );
    assert.equal(
      await page.locator("[data-pim-ready], [data-contact-list]").count(),
      0,
      "no list",
    );
    await sound(page, "unavailable");
    await shot(page, "nicht-verfuegbar");
    await ctx.close();
  }

  // ---- English once ----
  {
    const { page, ctx } = await open({ lang: "en" });
    await page.goto(base + "/contacts?id=ben.vcf");
    await ready(page, "show");
    await page.locator("[data-contact-detail] h2").waitFor();
    const text = await page.locator("[data-contact-detail]").innerText();
    assert.match(text, /Write e-mail/);
    assert.match(text, /Phone · Mobile/);
    assert.match(text, /November 20, 1986 · turns 40 on Friday/);
    assert.match(
      await page.locator('[data-contact="tom.vcf"]').innerText(),
      /has a birthday today/,
    );
    await sound(page, "english");
    await shot(page, "kontakte-en");
    await ctx.close();
  }

  // ---- Phone 390: list, contact with three big buttons, form, search; 44 px ----
  for (const theme of ["graphit", "hell"]) {
    const { page, ctx, state } = await open({ width: 390, theme, phone: true });
    await page.goto(base + "/contacts");
    await ready(page, "none");
    assert.deepEqual(
      await small(page, "[data-pim-ready]"),
      [],
      `phone list ${theme}: targets under 44 px`,
    );
    await sound(page, `phone list ${theme}`);
    await shot(page, `handy-liste-${theme}`);
    await page.locator('[data-contact="ben.vcf"]').click();
    await page.locator("[data-contact-detail]").waitFor();
    const acts = page.locator("[data-contact-detail] .acts .act");
    assert.equal(await acts.count(), 3);
    const boxes = await Promise.all(
      [0, 1, 2].map((i) => acts.nth(i).boundingBox()),
    );
    assert.ok(
      boxes.every((b) => b.height >= 60 && b.width >= 90),
      "three big buttons",
    );
    assert.deepEqual(
      await small(page, "[data-pim-ready]"),
      [],
      `phone contact ${theme}: targets under 44 px`,
    );
    await sound(page, `phone contact ${theme}`);
    await shot(page, `handy-kontakt-${theme}`);
    await page.getByRole("button", { name: "Bearbeiten" }).click();
    await page.locator("[data-contact-editor]").waitFor();
    await page.waitForTimeout(200);
    assert.deepEqual(
      await small(page, "[data-pim-ready]"),
      [],
      `phone form ${theme}: targets under 44 px`,
    );
    await sound(page, `phone form ${theme}`);
    await shot(page, `handy-bearbeiten-${theme}`);
    await page.getByRole("button", { name: "Abbrechen" }).click();
    await page.locator("[data-contact-detail]").waitFor();
    // Leaving changed fields asks first (the back arrow).
    await page.getByRole("button", { name: "Bearbeiten" }).click();
    await page
      .locator("[data-contact-editor]")
      .getByLabel("Vorname")
      .fill("Benjamin");
    await page.goBack();
    await dialog(page)
      .getByText(
        "Die Änderungen an diesem Kontakt sind noch nicht gespeichert.",
      )
      .waitFor();
    await dialog(page).getByRole("button", { name: "Verwerfen" }).click();
    await until(
      () => !new URL(page.url()).search.includes("edit"),
      "left the form",
    );
    assert.equal(sent(state, "PUT", /^\/contacts\//).length, 0);
    await page.goto(base + "/contacts");
    await ready(page, "none");
    await page.getByLabel("Kontakte durchsuchen").fill("beispiel");
    await until(
      async () => (await page.locator("[data-suggestion]").count()) === 1,
      "phone search",
    );
    assert.deepEqual(
      await small(page, "[data-pim-ready]"),
      [],
      `phone search ${theme}: targets under 44 px`,
    );
    await sound(page, `phone search ${theme}`);
    await shot(page, `handy-suche-${theme}`);
    await ctx.close();
  }

  // ---- In a window of the real desktop; "Ablage › Neuer Kontakt" ----
  {
    const { page, ctx } = await open({ desk: true, height: 1000 });
    await page.goto(base + "/contacts?id=ben.vcf");
    await page.locator("section.win").first().waitFor();
    const frame = await windowFrame(page, "/contacts");
    await frame.locator('[data-pim-ready][data-mode="show"]').waitFor();
    await frame.locator("[data-contact-detail] h2").waitFor();
    await page.waitForTimeout(500);
    await sound(frame, "desktop window");
    await sound(page, "desktop");
    await shot(page, "arbeitsplatz-fenster");
    await page
      .locator("header.topbar .menubar")
      .getByRole("menuitem", { name: "Ablage", exact: true })
      .click();
    const item = page.locator('.mb-menu [data-id="contacts.new"]');
    await item.waitFor();
    await until(
      async () => !(await item.isDisabled()),
      "Neuer Kontakt enabled",
    );
    await item.click();
    await frame.locator("[data-contact-editor]").waitFor();
    await until(
      async () =>
        (await page.locator("section.win").first().textContent()).includes(
          "Neuer Kontakt",
        ),
      "title bar names the form",
    );
    await page.waitForTimeout(300);
    await sound(frame, "desktop window new");
    await shot(page, "arbeitsplatz-neuer-kontakt");
    await ctx.close();
  }

  // ---- A narrow window (420 px, not the phone): one pane at a time ----
  {
    const { page, ctx } = await open({ width: 420 });
    await page.goto(base + "/contacts");
    await ready(page, "none");
    await sound(page, "420 list");
    await shot(page, "fenster-420-liste");
    await page.locator('[data-contact="ben.vcf"]').click();
    await page.locator("[data-contact-detail]").waitFor();
    assert.equal(
      await page.locator("[data-contact-list]").count(),
      0,
      "the contact instead of the list",
    );
    await sound(page, "420 contact");
    await shot(page, "fenster-420-kontakt");
    await page.getByRole("button", { name: "Bearbeiten" }).click();
    await page.locator("[data-contact-editor]").waitFor();
    await sound(page, "420 form");
    await page.getByRole("button", { name: "Abbrechen" }).click();
    await page.getByRole("link", { name: "Alle Kontakte" }).click();
    await page.locator("[data-contact-list]").waitFor();
    await ctx.close();
  }

  // ---- A smaller window (800 px, the page alone) ----
  {
    const { page, ctx } = await open({ width: 800 });
    await page.goto(base + "/contacts?id=lang.vcf");
    await ready(page, "show");
    await page.locator("[data-contact-detail] h2").waitFor();
    await sound(page, "800");
    await shot(page, "fenster-800");
    await page.goto(base + "/contacts?id=ben.vcf&edit=1");
    await page.locator("[data-contact-editor]").waitFor();
    await sound(page, "800 form");
    await shot(page, "fenster-800-bearbeiten");
    await ctx.close();
  }

  assert.deepEqual(errors, [], "script errors on the page");
  console.log(`Kontakte: all checks passed, screenshots in ${out}`);
} finally {
  await browser.close();
}
