// 1.13, Mail on the shared column frame (Baustand step 4, docs/TMAIL.md
// "1.13"): on isolated data (test-ui-mail-fixtures.mjs), nothing reaches a
// NAS or a mail server. Desktop 1440 px light/German and dark/English, in
// the desktop's window, narrow window 800 px light and dark, phone 390 px:
// no card (no page padding, no border), the frame as high as the window,
// each column scrolls on its own, two separators, the side bar folds to a
// rail and stays folded per device, loading in the same column (nothing
// jumps), folders sorted by dragging the grip and with Alt+arrow and saved
// for the account (PUT /preferences/order, read back after a reload), the
// shown name "Mail". On every page: no sideways scroll, no cut text.
// Screenshots into UI_OUTPUT.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5212 UI_OUTPUT=out/validation/113-shell node scripts/test-ui-mail-113.mjs
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
const out = process.env.UI_OUTPUT || "out/validation/113-shell";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const SANDBOXED =
  /document is sandboxed and lacks the 'allow-same-origin' flag/;
const pageErrors = () => errors.filter((e) => !SANDBOXED.test(e) && !/503/.test(e));

async function open({
  width = 1440,
  height = 900,
  phone = false,
  theme = "hell",
  locale = "de",
  desk = false,
} = {}) {
  const made = await create(
    "user",
    theme,
    width,
    locale,
    phone
      ? { isMobile: true, hasTouch: true, deviceScaleFactor: 3, viewport: { width, height } }
      : { viewport: { width, height } },
  );
  const state = mailState();
  if (desk)
    await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  await installMail(made.ctx, state);
  // The order of the side bars, kept like titand keeps it (merged per key).
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
async function windowFrame(page, prefix) {
  let found = null;
  await until(async () => {
    found = page.frames().find((f) => {
      if (f === page.mainFrame() || f.parentFrame() !== page.mainFrame()) return false;
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

/** The frame fills the page: no padding, no card, as high as the window. */
async function edgeToEdge(target, name) {
  const got = await target.evaluate(() => {
    const frame = document.querySelector(".cf.columns");
    const main = frame.parentElement;
    const r = frame.getBoundingClientRect();
    const s = getComputedStyle(frame);
    return {
      main: getComputedStyle(main).padding,
      left: Math.round(r.left),
      right: Math.round(innerWidth - r.right),
      bottom: Math.round(innerHeight - r.bottom),
      border: s.borderTopWidth,
      radius: s.borderTopLeftRadius,
      pageScroll: document.documentElement.scrollHeight - innerHeight,
    };
  });
  assert.equal(got.main, "0px", `${name}: main without padding`);
  assert.deepEqual([got.left, got.right, got.bottom], [0, 0, 0], `${name}: edge to edge ${JSON.stringify(got)}`);
  assert.equal(got.border, "0px", `${name}: no card border`);
  assert.equal(got.radius, "0px", `${name}: no card radius`);
  assert.ok(got.pageScroll <= 0, `${name}: the page itself does not scroll (${got.pageScroll})`);
}

const PRIVAT = '[aria-labelledby="mail-acc-acc-privat"]';
const folderNames = (page, account = "acc-privat") =>
  page
    .locator(`[aria-labelledby="mail-acc-${account}"] .folder .name`)
    .allInnerTexts();

try {
  // ---- Desktop 1440, light, German ----
  {
    const { page, order, data } = await open();
    // Loading: the mailboxes arrive late; the side bar is already the column.
    let release;
    const held = new Promise((r) => (release = r));
    await page.route(/\/api\/v1\/mail\/accounts$/, async (route) => {
      await held;
      return route.fallback();
    });
    await page.goto(base + "/mail");
    await page.locator(".mail.wide .col-folders").waitFor();
    await page.locator(".col-folders [role=status]", { hasText: "Wird geladen" }).waitFor();
    const before = await page.locator(".col-folders").boundingBox();
    await shot(page, "00-laden-1440-hell");
    release();
    await page.locator(".row").first().waitFor();
    await page.waitForURL(/a=acc-privat&f=INBOX/);
    const after = await page.locator(".col-folders").boundingBox();
    assert.deepEqual(
      [before.x, before.y, before.width, before.height].map(Math.round),
      [after.x, after.y, after.width, after.height].map(Math.round),
      "the side bar stays where it was while loading",
    );

    await edgeToEdge(page, "1440");
    assert.equal(await page.getByRole("separator").count(), 2, "two separators");
    assert.equal(
      await page.locator(".col-folders").getByRole("button", { name: "Neue E-Mail" }).count(),
      1,
      "the main action heads the side bar",
    );
    // Each column scrolls on its own: the list scrolls, the side bar does not move.
    const list = page.locator(".col-list");
    assert.equal(await list.evaluate((el) => getComputedStyle(el).overflowY), "auto");
    assert.ok(await list.evaluate((el) => el.scrollHeight > el.clientHeight), "the list is longer than the window");
    await list.evaluate((el) => el.scrollTo(0, 400));
    assert.equal(Math.round((await page.locator(".col-folders").boundingBox()).y), 0, "side bar stays");
    await list.evaluate((el) => el.scrollTo(0, 0));
    assert.match(await page.title(), /Posteingang|Mail/);
    await sound(page, "1440 light");
    await shot(page, "01-mail-1440-hell");

    // Sorting: the trash's grip dragged above the inbox.
    assert.deepEqual(await folderNames(page), [
      "Posteingang",
      "Entwürfe",
      "Gesendet",
      "Spam",
      "Papierkorb",
      "Rechnungen",
      "2026",
    ]);
    const trashLine = page.locator(PRIVAT + ' [data-sort-key="Papierkorb"]');
    await trashLine.hover();
    const grip = trashLine.getByRole("button", { name: "„Papierkorb“ verschieben" });
    const g = await grip.boundingBox();
    const inbox = await page.locator(PRIVAT + ' [data-sort-key="INBOX"]').boundingBox();
    await page.mouse.move(g.x + g.width / 2, g.y + g.height / 2);
    await page.mouse.down();
    await page.mouse.move(g.x + g.width / 2, inbox.y + 30, { steps: 6 });
    await page.mouse.move(g.x + g.width / 2, inbox.y + 4, { steps: 4 });
    await page.locator(PRIVAT + ' [data-sort-key="INBOX"][data-sort-drop="before"]').waitFor();
    await shot(page, "02-ordner-ziehen-1440-hell");
    await page.mouse.up();
    assert.deepEqual((await folderNames(page)).slice(0, 5), [
      "Papierkorb",
      "Posteingang",
      "Entwürfe",
      "Gesendet",
      "Spam",
    ]);
    await page.getByRole("status").filter({ hasText: "„Papierkorb“ steht jetzt an Stelle 1 von 5." }).waitFor({ state: "attached" });
    // Alt+↓ on a folder moves it down one.
    await page.locator(PRIVAT + ' [data-sort-key="INBOX"] .folder').focus();
    await page.keyboard.press("Alt+ArrowDown");
    assert.deepEqual((await folderNames(page)).slice(0, 3), ["Papierkorb", "Entwürfe", "Posteingang"]);
    assert.equal(
      await page.evaluate(() => document.activeElement?.closest("[data-sort-key]")?.getAttribute("data-sort-key")),
      "INBOX",
      "the focus stays on the moved folder",
    );
    await until(() => order.puts.length === 1, "one save after two steps");
    assert.deepEqual(order.puts[0], {
      mail: {
        "acc-privat": ["Papierkorb", "Entw&APw-rfe", "INBOX", "Gesendet", "Spamverdacht"],
      },
    });
    // Kept for the account: after a reload (the NAS sends it back).
    assert.ok(data.prefs.order, "the order is on the NAS");
    await page.reload();
    await page.locator(".row").first().waitFor();
    assert.deepEqual((await folderNames(page)).slice(0, 3), ["Papierkorb", "Entwürfe", "Posteingang"]);
    // The other mailbox is not touched.
    assert.deepEqual(await folderNames(page, "acc-arbeit"), ["Posteingang", "Entwürfe", "Gesendet", "Papierkorb"]);

    // Folding: the rail with the inboxes and their counts, kept per device.
    await page.getByRole("button", { name: "Seitenleiste einklappen" }).click();
    assert.equal(await page.getByRole("separator").count(), 1, "the side bar's separator goes");
    const railInbox = page.getByRole("button", { name: /^Posteingang · Privat, \d+ ungelesen$/ });
    await railInbox.waitFor();
    const side = await page.locator(".col-folders").boundingBox();
    assert.ok(side.width <= 64, `rail ${side.width}px`);
    await page.locator(".col-folders").getByRole("button", { name: "Neue E-Mail" }).waitFor();
    await sound(page, "1440 folded");
    await shot(page, "03-eingeklappt-1440-hell");
    await page.getByRole("button", { name: /^Posteingang · Arbeit/ }).click();
    await page.waitForURL(/a=acc-arbeit&f=INBOX/);
    await page.reload();
    await page.getByRole("button", { name: "Seitenleiste ausklappen" }).waitFor();
    await page.getByRole("button", { name: "Seitenleiste ausklappen" }).click();
    await page.locator(".col-folders .folder").first().waitFor();
    assert.equal(await page.evaluate(() => localStorage.getItem("titanos.mail.sideFolded")), null);
    await page.close();
  }

  // ---- Desktop 1440, dark, English, a message open ----
  {
    const { page } = await open({ theme: "graphit", locale: "en" });
    await page.goto(base + "/mail?a=acc-privat&f=INBOX&m=1000");
    await page.locator("#mail-message-subject").waitFor();
    await edgeToEdge(page, "1440 dark");
    await page.locator(".col-folders").getByRole("button", { name: "New e-mail" }).waitFor();
    await sound(page, "1440 dark en");
    await shot(page, "04-mail-nachricht-1440-dunkel-en");
    await page.close();
  }

  // ---- In the desktop's window: edge to edge in the frame, the title "Mail" ----
  {
    const { page } = await open({ desk: true });
    await page.goto(base + "/mail");
    await page.locator("section.win").first().waitFor();
    const mail = await windowFrame(page, "/mail");
    await mail.locator(".row").first().waitFor();
    await edgeToEdge(mail, "window");
    assert.equal(await mail.getByRole("separator").count(), 2);
    await page.locator("section.win .titlebar", { hasText: "Mail" }).first().waitFor();
    assert.equal(await page.locator("section.win .titlebar", { hasText: "T-Mail" }).count(), 0, "the old name is gone");
    await shot(page, "05-arbeitsplatz-fenster-1440-hell");
    await page.close();
  }

  // ---- Narrow window 800 px, light and dark: one level at a time ----
  for (const theme of ["hell", "graphit"]) {
    const { page } = await open({ width: 800, theme });
    await page.goto(base + "/mail");
    await page.locator(".mail.levels .col-folders").waitFor();
    assert.equal(await page.getByRole("separator").count(), 0, "no separators when narrow");
    assert.equal(await page.getByRole("button", { name: /verschieben$/ }).count(), 0, "no sorting in levels");
    await sound(page, `800 ${theme} folders`);
    await shot(page, `06-ordner-800-${theme === "hell" ? "hell" : "dunkel"}`);
    await page.locator(".col-folders .folder", { hasText: "Posteingang" }).first().click();
    await page.locator(".row").first().waitFor();
    await sound(page, `800 ${theme} list`);
    await shot(page, `07-liste-800-${theme === "hell" ? "hell" : "dunkel"}`);
    await page.close();
  }

  // ---- Phone 390 px ----
  {
    const { page } = await open({ width: 390, height: 844, phone: true, desk: true });
    await page.goto(base + "/mail");
    await page.locator(".mail.levels .col-folders").waitFor();
    await page.locator(".col-folders").getByRole("button", { name: "Neue E-Mail" }).waitFor();
    await sound(page, "phone folders");
    await shot(page, "08-handy-ordner-390-hell");
    await page.locator(".col-folders .folder", { hasText: "Posteingang" }).first().click();
    await page.locator(".row").first().waitFor();
    await sound(page, "phone list");
    await shot(page, "09-handy-liste-390-hell");
    await page.locator(".row").first().click();
    await page.locator("#mail-message-subject").waitFor();
    await sound(page, "phone message");
    await shot(page, "10-handy-nachricht-390-hell");
    await page.close();
  }

  assert.deepEqual(pageErrors(), [], "page errors");
  console.log("test-ui-mail-113: ok");
} finally {
  await browser.close();
}
