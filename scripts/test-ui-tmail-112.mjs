// 1.12, T-Mail overhaul (docs/TMAIL.md "1.12"): the twelve points of the
// owner on isolated data (test-ui-mail-fixtures.mjs), nothing reaches a
// NAS or a mail server. Desktop at 1440 px light/German and dark/English
// (and once in the desktop's window), narrow window at 800 px, phone at
// 420 and 390 px: draggable columns with ARIA separators kept per device,
// no bar above the columns, preview whole, marks, date groups,
// conversations, sender pictures (contact picture and initials, 503 →
// initials), quick actions, selecting several with the bar, dragging onto
// a folder (not onto another mailbox), keys and their help, search over
// all folders with filters, favourites and folding, no counts for spam and
// trash, the overview, deleting in the trash asks, swipes on the phone.
// On every page: no sideways scroll, no cut text. Screenshots into UI_OUTPUT.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5212 UI_OUTPUT=out/validation/tmail-112 node scripts/test-ui-tmail-112.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import { installMail, mailState } from "./test-ui-mail-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5212";
const out = process.env.UI_OUTPUT || "out/validation/tmail-112";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const SANDBOXED =
  /document is sandboxed and lacks the 'allow-same-origin' flag/;
// A picture that answers 503 (no volume) is logged by the browser; that is
// the case under test, not an error of the page.
const pageErrors = () =>
  errors.filter((e) => !SANDBOXED.test(e) && !/503/.test(e));

const day = (daysAgo, hour = 9) => {
  const d = new Date();
  d.setDate(d.getDate() - daysAgo);
  d.setHours(hour, 15, 0, 0);
  return d.toISOString();
};
const PREVIEWS = [
  "Hallo Alex, wie besprochen schicke ich dir die Unterlagen für Freitag. Sag Bescheid, ob die Uhrzeit passt",
  "Ihre Nebenkostenabrechnung für das Jahr 2026 liegt im Kundenportal bereit. Bitte prüfen Sie die Angaben",
  "Ihr Paket wird heute zwischen 10 und 14 Uhr zugestellt. Sendungsnummer siehe unten",
  "Die Fotos vom Wochenende sind da, schau mal rein, besonders das vom See ist schön geworden",
];

/** The data of 1.12: dates over four groups, previews, a conversation, pictures. */
function data(s) {
  const inbox = s.messages["acc-privat"].INBOX;
  inbox.forEach((r, i) => {
    r.preview = PREVIEWS[i % PREVIEWS.length];
    if (i >= 8 && i < 20) r.date = day(1, 20 - i);
    else if (i >= 20 && i < 40) r.date = day(2 + ((i - 20) % 2), 8);
    else if (i >= 40) r.date = day(10 + i);
  });
  // A conversation of three: the root and two answers.
  const [root, a1, a2] = [inbox[5], inbox[3], inbox[1]];
  root.subject = "Grillabend am Samstag";
  root.messageId = "<grill-1@example.org>";
  a1.subject = "Re: Grillabend am Samstag";
  a1.messageId = "<grill-2@example.org>";
  a1.inReplyTo = "<grill-1@example.org>";
  a2.subject = "AW: Grillabend am Samstag";
  a2.messageId = "<grill-3@example.org>";
  a2.references = "<grill-1@example.org> <grill-2@example.org>";
  for (const r of [root, a1, a2]) r.from = { name: "Ben Muster", addr: "ben@example.net" };
  inbox[2].flags.forwarded = true;
  // Anna has a contact with a picture.
  s.avatars = { "anna@example.org": "anna.vcf" };
  // Spam and trash with unread mail: no count shown for them.
  s.messages["acc-privat"].Spamverdacht = [
    { ...inbox[10], uid: 70, flags: { seen: false, flagged: false, answered: false } },
  ];
  s.messages["acc-privat"].Papierkorb[0].flags.seen = false;
  // Flagged mail for the overview.
  inbox[6].flags.flagged = true;
  inbox[7].flags.flagged = true;
}

async function open({
  width = 1440,
  height = 1000,
  phone = false,
  theme = "hell",
  locale = "de",
  desk = false,
  change = () => {},
} = {}) {
  const made = await create(
    "user",
    theme,
    width,
    locale,
    phone
      ? {
          isMobile: true,
          hasTouch: true,
          deviceScaleFactor: 3,
          viewport: { width, height },
        }
      : { viewport: { width, height } },
  );
  const state = mailState();
  data(state);
  change(state);
  if (desk)
    await made.ctx.addInitScript(() =>
      localStorage.removeItem("titanos.layout"),
    );
  await installMail(made.ctx, state);
  return { ...made, state };
}

async function sound(target, name) {
  const overflow = await target.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `${name}: scrolls sideways by ${overflow}px`);
  assert.deepEqual(await target.evaluate(cutTextIn), [], `${name}: cut text`);
  assert.equal(
    await target.evaluate(() => document.body.innerText.includes("…")),
    false,
    `${name}: an ellipsis`,
  );
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
/** A finger on the screen (as test-ui-phone-gestures.mjs swipes). */
async function swipe(page, from, to, steps = 10) {
  const cdp = await page.context().newCDPSession(page);
  const touch = (type, points) =>
    cdp.send("Input.dispatchTouchEvent", {
      type,
      touchPoints: points.map(([x, y], id) => ({ x, y, id })),
    });
  await touch("touchStart", [from]);
  for (let i = 1; i <= steps; i++)
    await touch("touchMove", [
      [
        from[0] + ((to[0] - from[0]) * i) / steps,
        from[1] + ((to[1] - from[1]) * i) / steps,
      ],
    ]);
  await touch("touchEnd", []);
  await page.waitForTimeout(250);
  await cdp.detach();
}
async function windowFrame(page, prefix) {
  let found = null;
  await until(async () => {
    found = page.frames().find((f) => {
      if (f === page.mainFrame() || f.parentFrame() !== page.mainFrame())
        return false;
      try {
        return new URL(f.url()).pathname.startsWith(prefix);
      } catch {
        return false;
      }
    });
    return !!found;
  }, `window ${prefix}`);
  return found;
}

try {
  // ---- Desktop 1440, light, German ----
  {
    const { page, state } = await open();
    await page.goto(base + "/mail");
    await page.locator(".mail.wide .col-folders").waitFor();
    await page.locator(".row").first().waitFor();
    await page.waitForURL(/a=acc-privat&f=INBOX/);

    // 9: no bar above the columns; "Neue E-Mail" heads the folders.
    assert.equal(
      await page.getByRole("button", { name: "Aktualisieren" }).count(),
      0,
      "no refresh button",
    );
    assert.equal(
      await page
        .locator(".col-folders")
        .getByRole("button", { name: "Neue E-Mail" })
        .count(),
      1,
      "compose in the folder column",
    );
    assert.equal(await page.locator(".win-actions").count(), 0, "no tool bar");

    // 3: groups in order; 2: previews whole, marks with words.
    const heads = await page.locator(".group h3").allInnerTexts();
    assert.deepEqual(
      heads.map((h) => h.toLowerCase()),
      ["heute", "gestern", "diese woche", "älter"].filter((g) =>
        heads.map((h) => h.toLowerCase()).includes(g),
      ),
      "groups in order",
    );
    assert.ok(heads.length >= 3, `groups: ${heads}`);
    const preview = page.locator(".preview").first();
    assert.equal(
      await preview.evaluate((el) => getComputedStyle(el).webkitLineClamp),
      "none",
      "preview not clamped",
    );
    assert.ok(
      PREVIEWS.includes(await preview.innerText()),
      "preview whole",
    );
    await page.locator(".mark", { hasText: "Weitergeleitet" }).first().waitFor({ state: "attached" });

    // 12: Anna's contact picture through the contacts' route; others initials.
    await until(
      () => sent(state, "GET", /\/contacts\/kontakte\/anna\.vcf\/photo/).length > 0,
      "contact picture asked",
    );
    assert.ok((await page.locator("img.av").count()) > 0, "a picture");
    assert.ok((await page.locator("span.av.letters").count()) > 0, "initials");

    // 4: the conversation is one entry with its count.
    const toggle = page.getByRole("button", {
      name: /Unterhaltung aufklappen: 3 Nachrichten/,
    });
    await toggle.waitFor();
    await toggle.click();
    assert.equal(await toggle.getAttribute("aria-expanded"), "true");
    assert.equal(await page.locator(".entry.child").count(), 2, "two below");
    await sound(page, "desktop list");

    // 11: the overview with nothing open.
    const overview = page.locator(".overview");
    await overview.waitFor();
    await overview.getByText(/ungelesene Nachrichten in Privat/).waitFor();
    await overview.getByText(/Zuletzt aktualisiert/).waitFor();
    assert.ok(
      (await overview.locator(".card").nth(1).locator("button.link").count()) >= 2,
      "flagged mails",
    );
    await shot(page, "01-desktop-liste-uebersicht-1440-hell");

    // 10: spam and trash count nothing; favourites and folding, per device.
    for (const name of ["Spam", "Papierkorb"]) {
      const f = page.locator(".col-folders .folder", { hasText: name }).first();
      assert.equal(await f.locator(".count").count(), 0, `${name} without count`);
    }
    assert.ok(
      (await page
        .locator(".col-folders .folder", { hasText: "Posteingang" })
        .first()
        .locator(".count")
        .count()) === 1,
      "inbox counts",
    );
    await page.getByRole("button", { name: "„Rechnungen“ als Favorit anheften" }).click();
    await page.locator("#mail-favourites").waitFor();
    await page.getByRole("button", { name: "Unterordner von „Rechnungen“ verbergen" }).click();
    assert.equal(
      await page.locator(".col-folders .folder", { hasText: "2026" }).count(),
      0,
      "folded",
    );
    await page.reload();
    await page.locator(".row").first().waitFor();
    await page.locator("#mail-favourites").waitFor();
    assert.equal(
      await page.locator(".col-folders .folder", { hasText: "2026" }).count(),
      0,
      "folded after a reload",
    );
    await page.getByRole("button", { name: "Unterordner von „Rechnungen“ zeigen" }).click();
    await page.locator(".col-folders .folder", { hasText: "2026" }).waitFor();

    // 1: the separators: keys, kept per device, double click resets.
    const seps = page.getByRole("separator");
    assert.equal(await seps.count(), 2, "two separators");
    const list = seps.nth(1);
    const before = Number(await list.getAttribute("aria-valuenow"));
    await list.focus();
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("Shift+ArrowRight");
    assert.equal(Number(await list.getAttribute("aria-valuenow")), before + 64);
    const colWidth = () =>
      page.locator(".col-list").evaluate((el) => Math.round(el.getBoundingClientRect().width));
    assert.ok(Math.abs((await colWidth()) - (before + 64)) <= 2, "the column follows");
    await page.reload();
    await page.locator(".row").first().waitFor();
    assert.equal(
      Number(await page.getByRole("separator").nth(1).getAttribute("aria-valuenow")),
      before + 64,
      "kept per device",
    );
    // Dragging the folders' border.
    const fsep = page.getByRole("separator").nth(0);
    const box = await fsep.boundingBox();
    const f0 = Number(await fsep.getAttribute("aria-valuenow"));
    await page.mouse.move(box.x + box.width / 2, box.y + 200);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + 60, box.y + 200, { steps: 5 });
    await page.mouse.up();
    assert.ok(Number(await fsep.getAttribute("aria-valuenow")) >= f0 + 55, "dragged");
    await page.getByRole("separator").nth(1).dblclick();
    assert.equal(
      await page.evaluate(() => localStorage.getItem("titanos.mail.listWidth")),
      null,
      "double click resets",
    );
    await shot(page, "02-desktop-spalten-gezogen-1440-hell");

    // 5: quick actions on hover: read/unread asks titand.
    const second = page.locator(".entry").nth(1);
    await second.hover();
    await second.locator(".quick").waitFor();
    assert.equal(
      await second.locator(".quick").evaluate((el) => getComputedStyle(el).opacity),
      "1",
    );
    await shot(page, "03-desktop-schnellaktionen-1440-hell");
    const flagsBefore = sent(state, "POST", /\/flags$/).length;
    await second.getByRole("button", { name: /Als (un)?gelesen markieren/ }).click();
    await until(() => sent(state, "POST", /\/flags$/).length > flagsBefore, "quick flags");

    // 6: several selected, Shift range, the bar archives.
    const boxes = page.getByRole("checkbox", { name: /Auswählen/ });
    await boxes.nth(6).check();
    await boxes.nth(8).click({ modifiers: ["Shift"] });
    const bar = page.getByRole("toolbar", { name: "Aktionen für die Auswahl" });
    await bar.getByText("3 ausgewählt").waitFor();
    await sound(page, "bulk bar");
    await shot(page, "04-desktop-mehrfachauswahl-1440-hell");
    await bar.getByRole("button", { name: "Archivieren" }).click();
    await until(() => sent(state, "POST", /\/archive$/).length === 1, "bulk archive");
    const archived = sent(state, "POST", /\/archive$/)[0][2];
    assert.equal(archived.uids.length, 3, "three archived in one request");
    await page.getByText("3 archiviert.").waitFor();

    // 6: dragging a row onto a folder of the same mailbox moves it …
    const row = page.locator(".entry").nth(4);
    const subject = await row.locator(".subject").innerText();
    await row.dragTo(page.locator(".col-folders .folder", { hasText: "2026" }).first());
    await until(() => sent(state, "POST", /\/move$/).length === 1, "drop moves");
    assert.equal(sent(state, "POST", /\/move$/)[0][2].to, "Rechnungen/2026");
    await page.getByText(/nach „2026“ verschoben/).waitFor();
    assert.equal(await page.locator(".subject", { hasText: subject }).count(), 0);
    // … onto another mailbox's folder nothing happens.
    await page
      .locator(".entry")
      .nth(4)
      .dragTo(page.locator('[aria-labelledby="mail-acc-acc-arbeit"] .folder', { hasText: "Entwürfe" }).first());
    await page.waitForTimeout(300);
    assert.equal(sent(state, "POST", /\/move$/).length, 1, "no move across mailboxes");

    // 7: keys – j opens, k goes back, / to the search, ? the help.
    await page.locator("body").click({ position: { x: 5, y: 5 } });
    await page.keyboard.press("j");
    await page.waitForURL(/&m=\d+/);
    const firstOpen = new URL(page.url()).searchParams.get("m");
    // The list marks the open one once the page has taken the address.
    await page.locator(`.entry.current[data-uid="${firstOpen}"]`).waitFor();
    await page.keyboard.press("j");
    await until(() => new URL(page.url()).searchParams.get("m") !== firstOpen, "j");
    await page.locator(`.entry.current:not([data-uid="${firstOpen}"])`).waitFor();
    await page.keyboard.press("k");
    await until(() => new URL(page.url()).searchParams.get("m") === firstOpen, "k");
    await page.locator(`.entry.current[data-uid="${firstOpen}"]`).waitFor();
    await page.locator("#mail-message-subject").waitFor();
    await page.keyboard.press("/");
    assert.equal(await page.evaluate(() => document.activeElement?.id), "mail-search");
    // Typing in the search field is no key of T-Mail.
    await page.keyboard.type("jk");
    assert.equal(new URL(page.url()).searchParams.get("m"), firstOpen, "typing is no key");
    await page.locator("#mail-search").fill("");
    await page.locator("#mail-message-subject").click();
    await page.keyboard.press("?");
    const help = page.getByRole("dialog", { name: "Tastenkürzel" });
    await help.waitFor();
    await help.getByText("Nächste Nachricht").waitFor();
    await sound(page, "keys help");
    await shot(page, "05-desktop-tastenkuerzel-1440-hell");
    await help.getByRole("button", { name: "Schließen" }).click();
    // e archives the open one, the next opens.
    const archives = sent(state, "POST", /\/archive$/).length;
    await page.keyboard.press("e");
    await until(() => sent(state, "POST", /\/archive$/).length === archives + 1, "e archives");
    await until(() => new URL(page.url()).searchParams.get("m") !== firstOpen, "next after e");
    // r answers the open one.
    await page.keyboard.press("r");
    await page.waitForURL(/\/mail\/compose\?.*mode=reply/);
    await page.goto(base + "/mail?a=acc-privat&f=INBOX");
    await page.locator(".row").first().waitFor();

    // 8: search over all folders with filters; an opened hit keeps the hits.
    await page.getByRole("button", { name: "Allen Ordnern" }).click();
    await page.getByText(/Gib einen Suchbegriff ein/).waitFor();
    await page.getByRole("searchbox", { name: "In allen Ordnern suchen" }).fill("Liste");
    await page.getByRole("searchbox", { name: "In allen Ordnern suchen" }).press("Enter");
    await until(() => sent(state, "GET", /\/search\?.*q=Liste/).length > 0, "search all");
    const hit = page.locator(".entry", { hasText: "Tagesübersicht der Liste" });
    await hit.waitFor();
    await hit.getByText("in Rechnungen").waitFor();
    await page.getByRole("button", { name: "Weitere Filter" }).click();
    await page.getByRole("checkbox", { name: "Mit Anhang" }).check();
    await until(() => sent(state, "GET", /\/search\?.*attachment=1/).length > 0, "attachment filter");
    await page.getByRole("checkbox", { name: "Mit Anhang" }).uncheck();
    await page.getByRole("searchbox", { name: "Von", exact: true }).fill("anna");
    await until(() => sent(state, "GET", /\/search\?.*from=anna/).length > 0, "from filter");
    await page.getByRole("searchbox", { name: "Von", exact: true }).fill("");
    // The last search is again the one with "Liste" alone.
    await until(() => {
      const last = sent(state, "GET", /\/search\?/).at(-1)?.[1] ?? "";
      return /q=Liste/.test(last) && !/from=|attachment=/.test(last);
    }, "search without filters");
    await page.waitForTimeout(300);
    await hit.waitFor();
    await sound(page, "search all folders");
    await shot(page, "06-desktop-suche-alle-ordner-1440-hell");
    const searches = sent(state, "GET", /\/search\?/).length;
    await hit.locator(".row").click();
    await page.waitForURL(/f=INBOX&m=60&mf=Rechnungen/);
    await page.locator("[data-raw-text]").waitFor();
    await hit.waitFor();
    assert.equal(sent(state, "GET", /\/search\?/).length, searches, "hits kept, not searched again");
    assert.equal(await hit.evaluate((el) => el.classList.contains("current")), true, "the hit is the open one");

    // Deleting in the trash asks, on the quick action too.
    await page.locator(".col-folders .folder", { hasText: "Papierkorb" }).first().click();
    const trashRow = page.locator('.entry[data-folder="Papierkorb"]').first();
    await trashRow.waitFor();
    await trashRow.hover();
    const deletes = sent(state, "POST", /\/delete$/).length;
    await trashRow.getByRole("button", { name: "Endgültig löschen" }).click();
    const ask = page.getByRole("dialog");
    await ask.getByText("Endgültig löschen?").waitFor();
    assert.equal(sent(state, "POST", /\/delete$/).length, deletes, "nothing deleted before the answer");
    await shot(page, "07-desktop-papierkorb-frage-1440-hell");
    await ask.getByRole("button", { name: "Abbrechen" }).click();
    await page.close();
  }

  // ---- Desktop 1440, dark, English; pictures without a volume: initials ----
  {
    const { page, state } = await open({
      theme: "graphit",
      locale: "en",
      change: (s) => (s.photosGone = true),
    });
    await page.goto(base + "/mail");
    await page.locator(".row").first().waitFor();
    await page.getByRole("heading", { name: "Today" }).waitFor();
    await until(
      () => sent(state, "GET", /\/contacts\/kontakte\/anna\.vcf\/photo/).length > 0,
      "picture asked",
    );
    // The first of Anna's mails (in sight; further ones load lazily) falls back.
    const anna = page.locator(".entry", { hasText: "Anna Beispiel" }).first();
    await anna.locator("span.av.letters").waitFor();
    assert.equal(await anna.locator("img.av").count(), 0, "503 → initials");
    await page.getByRole("button", { name: "All folders" }).waitFor();
    await page.locator(".overview").getByText(/Last refreshed/).waitFor();
    await sound(page, "desktop dark en");
    await shot(page, "08-desktop-liste-1440-dunkel-en");
    await page.locator(".row").first().click();
    await page.locator("#mail-message-subject").waitFor();
    await sound(page, "desktop dark en message");
    await shot(page, "09-desktop-nachricht-1440-dunkel-en");
    await page.close();
  }

  // ---- In the desktop's window (the owner's screenshot) ----
  {
    const { page } = await open({ desk: true });
    await page.goto(base + "/mail");
    await page.locator("section.win").first().waitFor();
    const mail = await windowFrame(page, "/mail");
    await mail.locator(".row").first().waitFor();
    assert.equal(await mail.getByRole("separator").count(), 2);
    assert.equal(await mail.getByRole("button", { name: "Aktualisieren" }).count(), 0);
    await mail.locator(".overview").waitFor();
    // Keys reach the window's frame.
    await mail.locator(".overview h2").click();
    await mail.page().keyboard.press("j");
    await until(() => /[?&]m=\d+/.test(mail.url()), "j in the window");
    await shot(page, "10-arbeitsplatz-fenster-1440-hell");
    await page.close();
  }

  // ---- Narrow window 800 px: levels, no separators, compose in the list ----
  {
    const { page } = await open({ width: 800 });
    await page.goto(base + "/mail");
    await page.locator(".mail.levels .col-folders").waitFor();
    assert.equal(await page.getByRole("separator").count(), 0, "no separators when narrow");
    await sound(page, "800 folders");
    await page.locator(".col-folders .folder", { hasText: "Posteingang" }).first().click();
    await page.locator(".row").first().waitFor();
    await page.locator(".col-list").getByRole("button", { name: "Neue E-Mail" }).waitFor();
    await sound(page, "800 list");
    await shot(page, "11-fenster-liste-800-hell");
    await page.close();
  }

  // ---- Phone 420 px, dark, English: swipe to the actions ----
  {
    const { page, state } = await open({
      width: 420,
      height: 900,
      phone: true,
      desk: true,
      theme: "graphit",
      locale: "en",
    });
    await page.goto(base + "/mail?a=acc-privat&f=INBOX");
    await page.locator(".row").first().waitFor();
    assert.equal(await page.locator(".quick").count(), 0, "no hover actions on the phone");
    await sound(page, "phone 420 list");
    await shot(page, "12-handy-liste-420-dunkel-en");
    const row = page.locator("[data-swipe-row]").nth(1);
    const b = await row.boundingBox();
    const y = b.y + b.height / 2;
    // Half way to the left: the actions show, nothing happens yet.
    await swipe(page, [b.x + b.width - 30, y], [b.x + b.width - 200, y + 2]);
    await page.locator('[data-swipe-row].open [data-swipe-action="delete"]').waitFor();
    assert.equal(sent(state, "POST", /\/delete$/).length, 0, "nothing deleted by the swipe alone");
    await shot(page, "13-handy-wischen-420-dunkel-en");
    await page.locator('[data-swipe-row].open [data-swipe-action="delete"]').click();
    await until(() => sent(state, "POST", /\/delete$/).length === 1, "swipe delete");
    await page.close();
  }

  // ---- Phone 390 px, light, German: swipe right archives; back keeps the search ----
  {
    const { page, state } = await open({ width: 390, height: 844, phone: true, desk: true });
    await page.goto(base + "/mail");
    await page.locator(".mail.levels .col-folders").waitFor();
    await page.locator(".col-folders").getByRole("button", { name: "Neue E-Mail" }).waitFor();
    await sound(page, "phone 390 folders");
    await shot(page, "14-handy-ordner-390-hell");
    await page.locator(".col-folders .folder", { hasText: "Posteingang" }).first().click();
    await page.locator(".row").first().waitFor();
    const row = page.locator("[data-swipe-row]").first();
    const b = await row.boundingBox();
    // Right, away from the edge that belongs to "Zurück": archive.
    await swipe(page, [b.x + 60, b.y + b.height / 2], [b.x + 300, b.y + b.height / 2 + 4]);
    await until(() => sent(state, "POST", /\/archive$/).length === 1, "swipe right archives");
    await page.getByRole("searchbox").fill("Paket");
    await page.getByRole("searchbox").press("Enter");
    await until(() => sent(state, "GET", /\/messages\?.*q=Paket/).length > 0, "search");
    await page.locator(".row", { hasText: "Paket" }).first().waitFor();
    await sound(page, "phone 390 list");
    await shot(page, "15-handy-liste-suche-390-hell");
    await page.locator(".row", { hasText: "Paket" }).first().click();
    await page.locator("#mail-message-subject").waitFor();
    await sound(page, "phone 390 message");
    await page.goBack();
    await page.locator(".row").first().waitFor();
    assert.equal(await page.getByRole("searchbox").inputValue(), "Paket", "search kept");
    await page.close();
  }

  assert.deepEqual(pageErrors(), [], "page errors");
  console.log("test-ui-tmail-112: ok");
} finally {
  await browser.close();
}
