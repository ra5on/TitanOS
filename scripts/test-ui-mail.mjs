// 1.05, T-Mail (docs/TMAIL.md): the desktop at 1440 px and the phone at
// 390 px with isolated data (test-ui-mail-fixtures.mjs). Desktop: the
// program in its window with three columns, an HTML message in the sandbox
// (sandbox attribute exact, the script inside never runs, the tracking
// picture loads only after "Bilder laden"), saving an attachment into the
// NAS's files, writing in a window of its own (reply with a NAS file, send;
// a refused send names the server's answer; discard asks), search, filter,
// pages while scrolling, a new mail over the live stream, the states
// auth_failed (with "Passwort ändern") and unreachable, the setup assistant
// (no password in the browser's storage) and the settings. Phone: levels
// with the bar's arrow, 44 px targets, writing as a page. On every page: no
// sideways scroll, no cut text. Screenshots into UI_OUTPUT.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://localhost:5203 UI_OUTPUT=out node scripts/test-ui-mail.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import { bankHtml, installMail, mailState } from "./test-ui-mail-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://localhost:5203";
const out = process.env.UI_OUTPUT || "out/validation/mail";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const SECRET = "Geheim-App-Passwort-4711";
// The test's own init scripts (localStorage) also run inside the mail's
// frame and fail there: the frame has no same origin. Those are no errors of
// the page; that they happen shows the sandbox at work.
const SANDBOXED =
  /document is sandboxed and lacks the 'allow-same-origin' flag/;
const pageErrors = () => errors.filter((e) => !SANDBOXED.test(e));

/** A context with T-Mail's data. `desk`: the real desktop or phone frame (not the bare page). */
async function open({
  width = 1440,
  height = 1000,
  phone = false,
  desk = true,
  change = () => {},
} = {}) {
  const made = await create(
    "user",
    "hell",
    width,
    "de",
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
  change(state);
  if (desk)
    await made.ctx.addInitScript(() =>
      localStorage.removeItem("titanos.layout"),
    );
  // Every frame notes the messages it gets: a script in the mail would post one.
  await made.ctx.addInitScript(() => {
    window.__heard = [];
    window.addEventListener("message", (e) => {
      if (typeof e.data === "string") window.__heard.push(e.data);
    });
  });
  await installMail(made.ctx, state);
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
/** The frame of a window whose page path starts with `prefix`. */
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
  // ---- Desktop: the program, three columns, the HTML message ----
  {
    const { page, state } = await open();
    await page.goto(base + "/mail");
    await page.locator("section.win").first().waitFor();
    const mail = await windowFrame(page, "/mail");
    await mail.locator(".mail.wide .col-folders").waitFor();
    assert.equal(
      await mail.locator(".mail.wide > .col").count(),
      3,
      "three columns",
    );
    // Unread: bold, a dot and the word for screen readers.
    const first = mail.locator(".row").first();
    await first.waitFor();
    assert.match(await first.innerText(), /Ihr Kontoauszug November/);
    assert.equal(await first.locator(".dot").count(), 1, "unread dot");
    assert.match(
      await first.locator(".sr-only").first().textContent(),
      /Ungelesen/,
    );
    assert.ok(
      (await first
        .locator(".from")
        .evaluate((e) => getComputedStyle(e).fontWeight)) >= 600,
      "unread bold",
    );
    await sound(mail, "desktop list");
    await shot(page, "01-desktop-liste-1440");

    await first.click();
    await mail.locator("#mail-message-subject").waitFor();
    const iframe = mail.locator("iframe.frame");
    await iframe.waitFor();
    assert.equal(
      await iframe.getAttribute("sandbox"),
      "allow-popups allow-popups-to-escape-sandbox",
      "sandbox exact",
    );
    // titand's document goes in unchanged (its own CSP meta first). It
    // carries a script and an onerror handler here: the frame must run neither.
    const srcdoc = await iframe.getAttribute("srcdoc");
    assert.equal(srcdoc, bankHtml(false), "document unchanged in srcdoc");
    assert.match(srcdoc, /<script>/, "the fixture's script is in the frame");
    assert.match(
      srcdoc,
      /data:image\/png;base64/,
      "embedded picture as data: URL",
    );
    await page.waitForTimeout(800);
    assert.deepEqual(
      state.sentinel,
      [],
      "nothing ran or loaded from the message",
    );
    const heard = await mail.evaluate(() => window.__heard);
    assert.ok(!heard.includes("script-ran"), "no script posted from the frame");
    // Opening marked it read.
    await until(
      async () => !(await mail.locator(".row").first().locator(".dot").count()),
      "read after opening",
    );
    // Blocked pictures, then loaded on request.
    await mail.getByText(/Externe Bilder blockiert/).waitFor();
    await mail.getByText(/Linkziele in dieser Nachricht \(2\)/).waitFor();
    await mail.getByText(/Achtung: ein Link führt woanders hin/).waitFor();
    // To/Cc open on request.
    await mail.getByRole("button", { name: "Alle Empfänger zeigen" }).click();
    await mail.getByText("Ben Muster <ben@example.net>").waitFor();
    await sound(mail, "desktop message");
    await shot(page, "02-desktop-nachricht-1440");
    await mail.getByRole("button", { name: "Bilder laden" }).click();
    await until(
      () => state.sentinel.some((u) => u.includes("track.example.test")),
      "picture after Bilder laden",
    );
    assert.ok(
      state.sentinel.every((u) => u.includes("track.example.test")),
      "still no script",
    );
    assert.equal(
      await mail.locator("iframe.frame").getAttribute("srcdoc"),
      bankHtml(true),
    );
    assert.ok(
      sent(state, "GET", /\/message\?.*images=1/).length,
      "images=1 asked",
    );

    // Attachment: download link and "In Dateien speichern" with the folder picker.
    const download = mail.getByRole("link", { name: /Herunterladen/ });
    assert.match(
      await download.getAttribute("href"),
      /^\/api\/v1\/mail\/accounts\/acc-privat\/part\?folder=INBOX&uid=1000&part=2$/,
    );
    await mail.getByRole("button", { name: /In Dateien speichern/ }).click();
    const picker = mail.getByRole("dialog");
    await picker.getByRole("button", { name: /Documents/ }).click();
    await picker.getByRole("button", { name: "Diesen Ordner wählen" }).click();
    await until(() => state.saved.length === 1, "attachment saved");
    assert.deepEqual(state.saved[0], {
      folder: "INBOX",
      uid: 1000,
      part: "2",
      root: "share/Documents",
      path: "Kontoauszug_November_2026_Girokonto_DE12345678901234567890.pdf",
    });
    await mail.getByText(/ist gespeichert in Documents/).waitFor();

    // Flag, unread, move.
    // 1.12: the list's quick actions have the same words; the message's bar.
    const bar = mail.getByRole("toolbar", { name: "Aktionen für diese Nachricht" });
    await bar.getByRole("button", { name: "Markieren", exact: true }).click();
    await until(
      () => sent(state, "POST", /\/flags$/).length === 1,
      "flag sent",
    );
    assert.deepEqual(sent(state, "POST", /\/flags$/)[0][2], {
      folder: "INBOX",
      uids: [1000],
      add: ["flagged"],
      remove: [],
    });
    await bar
      .getByRole("button", { name: "Markierung entfernen", exact: true })
      .waitFor();

    // ---- Writing in a window of its own: reply with a NAS file ----
    await mail.getByRole("button", { name: /^Antworten$/ }).click();
    await until(
      async () => (await page.locator("section.win").count()) === 2,
      "compose window",
    );
    const compose = await windowFrame(page, "/mail/compose");
    await compose.locator("#mail-subject").waitFor();
    assert.equal(
      await compose.locator("#mail-subject").inputValue(),
      "Re: Ihr Kontoauszug November",
    );
    await compose
      .getByText("Sparkasse Beispiel <service@bank.example>")
      .waitFor();
    assert.match(
      await compose.locator("#mail-text").inputValue(),
      /schrieb Sparkasse Beispiel:\n> Guten Tag/,
    );
    await compose.locator("#mail-text").evaluate((el) => {
      el.value = "Danke, ist angekommen." + el.value;
      el.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await compose.getByRole("button", { name: "Aus Dateien des NAS" }).click();
    const nas = compose.getByRole("dialog");
    await nas.getByRole("button", { name: /Documents/ }).click();
    await nas.getByText("Angebot Küche.pdf").click();
    await nas.getByRole("button", { name: /Übernehmen/ }).click();
    await compose.getByText("share/Documents:Angebot Küche.pdf").waitFor();
    await sound(compose, "compose window");
    await shot(page, "03-desktop-schreiben-fenster-1440");
    // The mail window stays as it was.
    assert.ok(
      new URL(mail.url()).pathname === "/mail",
      "mail window not taken over",
    );
    await compose.getByRole("button", { name: /Senden/ }).click();
    await until(() => state.sent.length === 1, "sent");
    const meta = state.sent[0].meta;
    assert.deepEqual(meta.to, ["Sparkasse Beispiel <service@bank.example>"]);
    assert.equal(meta.subject, "Re: Ihr Kontoauszug November");
    assert.equal(meta.inReplyTo, "<1000@fixture.example>");
    assert.equal(meta.references, "<1000@fixture.example>");
    assert.deepEqual(meta.answers, { folder: "INBOX", uid: 1000 });
    assert.equal(meta.attachFrom, undefined, "a reply carries no attachments");
    assert.equal(meta.signature, "s1");
    assert.deepEqual(state.sent[0].nas, [
      { root: "share/Documents", path: "Angebot Küche.pdf" },
    ]);
    await until(
      async () => (await page.locator("section.win").count()) === 1,
      "compose window closed after sending",
    );

    // ---- A refused send names the server's answer; discarding asks ----
    await mail.getByRole("button", { name: "Neue E-Mail" }).first().click();
    await until(
      async () => (await page.locator("section.win").count()) === 2,
      "second compose window",
    );
    const c2 = await windowFrame(page, "/mail/compose");
    await c2.locator("#mail-to").waitFor();
    await c2.locator("#mail-to").pressSequentially("an");
    await c2.getByRole("listbox").waitFor();
    assert.match(await c2.getByRole("listbox").innerText(), /Anna Beispiel/);
    // 1.11: a contact says so in words; a learned address does not.
    assert.match(
      await c2.getByRole("option", { name: /Anna Beispiel/ }).innerText(),
      /anna@example\.org\s*· Kontakt/,
    );
    assert.doesNotMatch(
      await c2.getByRole("option", { name: /Anton/ }).innerText(),
      /Kontakt/,
    );
    await c2.locator("#mail-to").press("ArrowDown");
    await c2.locator("#mail-to").press("Enter");
    await c2.getByText("Anna Beispiel <anna@example.org>").waitFor();
    await c2.locator("#mail-subject").fill("FEHLER beim Versand");
    await c2.getByRole("button", { name: /Senden/ }).click();
    await c2.getByText(/550 5\.1\.1 Mailbox unavailable/).waitFor();
    await c2.getByText(/nicht angenommen/).waitFor();
    await shot(page, "04-desktop-senden-fehler-1440");
    await c2.getByRole("button", { name: /Verwerfen/ }).click();
    const ask = c2.getByRole("dialog", { name: "E-Mail verwerfen?" });
    await ask.waitFor();
    assert.equal(
      (
        await ask.locator(".dialog-actions button").first().textContent()
      ).trim(),
      "Abbrechen",
    );
    await ask.getByRole("button", { name: "Verwerfen" }).click();
    await until(
      async () => (await page.locator("section.win").count()) === 1,
      "discarded window closed",
    );

    // ---- Search, filter, pages while scrolling ----
    const search = mail.getByRole("searchbox");
    await search.fill("Paket");
    await search.press("Enter");
    await until(
      () => sent(state, "GET", /\/messages\?.*q=Paket/).length > 0,
      "search asked",
    );
    await until(
      async () => (await mail.locator(".row").count()) === 18,
      "18 hits",
    );
    assert.match(await mail.locator(".list .count").innerText(), /18 gefunden/);
    await shot(page, "05-desktop-suche-1440");
    await search.fill("");
    await search.press("Enter");
    await mail.getByRole("button", { name: "Ungelesen", exact: true }).click();
    await until(
      () => sent(state, "GET", /\/messages\?.*unread=1/).length > 0,
      "unread filter asked",
    );
    await mail.getByRole("button", { name: "Alle", exact: true }).click();
    await until(
      async () => (await mail.locator(".row").count()) === 50,
      "first page",
    );
    await mail
      .locator(".col-list")
      .evaluate((el) => el.scrollTo(0, el.scrollHeight));
    await until(
      () => sent(state, "GET", /\/messages\?.*before=/).length > 0,
      "next page while scrolling",
    );
    await until(
      async () => (await mail.locator(".row").count()) === 72,
      "all pages",
    );
    // A mail whose head is too large to list says so in words.
    await mail.getByText("Mail zu groß zum Anzeigen").waitFor();
    await sound(mail, "list with a too large mail");

    // ---- A new mail over the live stream ----
    state.messages["acc-privat"].INBOX.unshift({
      uid: 1001,
      from: { name: "Neu Absender", addr: "neu@example.org" },
      to: [],
      subject: "Ganz frisch angekommen",
      date: new Date().toISOString(),
      flags: { seen: false, flagged: false, answered: false },
      size: 900,
      attachments: false,
    });
    state.events.push({
      type: "mail",
      data: { account: "acc-privat", folder: "INBOX", uid: 1001 },
    });
    await mail.getByText("Ganz frisch angekommen").waitFor({ timeout: 40000 });
    assert.ok(
      errors.some((e) => SANDBOXED.test(e)),
      "the mail's frame has no same origin",
    );
    assert.deepEqual(pageErrors(), [], "page errors");
  }

  // ---- Error states: auth_failed with "Passwort ändern", unreachable ----
  {
    const { page, state } = await open({
      desk: false,
      change: (s) => {
        s.accounts[1].status = {
          state: "auth_failed",
          code: "mail_auth_failed",
          since: "2026-11-17T08:00:00Z",
        };
        s.accounts.push({
          ...s.accounts[0],
          id: "acc-alt",
          name: "Altes Postfach beim Hoster mit einem sehr langen Namen",
          email: "alex@alt-hoster.example",
          status: {
            state: "unreachable",
            code: "mail_unreachable",
            since: "2026-11-17T07:00:00Z",
          },
        });
        s.folders["acc-alt"] = [];
        s.messages["acc-alt"] = {};
      },
    });
    await page.goto(base + "/mail");
    await page
      .getByText("Anmeldung fehlgeschlagen", { exact: false })
      .first()
      .waitFor();
    await page
      .getByText("Nicht erreichbar", { exact: false })
      .first()
      .waitFor();
    // Not by colour alone: words and a symbol.
    assert.equal(
      await page.locator('[data-state="auth_failed"] svg').count(),
      1,
    );
    assert.equal(
      await page.locator('[data-state="unreachable"] svg').count(),
      1,
    );
    await page
      .getByText(/Keine Verbindung zum Mailserver/)
      .first()
      .waitFor();
    await sound(page, "error states");
    await shot(page, "06-desktop-fehler-1440");
    await page.getByRole("link", { name: "Passwort ändern" }).click();
    await page.waitForURL(/\/mail\/settings\?edit=acc-arbeit/);
    const field = page.locator("#mail-edit-acc-arbeit-password");
    await field.waitFor();
    await until(
      () => field.evaluate((el) => el === document.activeElement),
      "password field focused",
    );
    await page.getByText(/T-Mail versucht es erst wieder/).waitFor();
    await field.fill(SECRET);
    await page
      .locator("#mail-account-acc-arbeit")
      .getByRole("button", { name: "Speichern" })
      .click();
    await until(
      () => sent(state, "PUT", /\/mail\/accounts\/acc-arbeit$/).length === 1,
      "password saved",
    );
    assert.equal(
      sent(state, "PUT", /\/mail\/accounts\/acc-arbeit$/)[0][2].password,
      SECRET,
    );
    await sound(page, "settings");
    await shot(page, "07-einstellungen-1440");
    const stored = await page.evaluate(
      () =>
        JSON.stringify({ ...localStorage }) +
        JSON.stringify({ ...sessionStorage }),
    );
    assert.ok(!stored.includes(SECRET), "no password in the browser's storage");
    // Removing a mailbox asks first.
    await page.locator("#mail-account-acc-alt").locator(".srow-head").click();
    await page
      .locator("#mail-account-acc-alt")
      .getByRole("button", { name: "Postfach entfernen" })
      .click();
    const ask = page.getByRole("dialog", { name: "Postfach entfernen?" });
    await ask.getByText(/Deine E-Mails bleiben beim Anbieter/).waitFor();
    await ask.getByRole("button", { name: "Postfach entfernen" }).click();
    await until(
      () => sent(state, "DELETE", /\/mail\/accounts\/acc-alt$/).length === 1,
      "removed",
    );
  }

  // ---- Setup assistant (1440 px, bare page) ----
  {
    const { page, state } = await open({
      desk: false,
      change: (s) => (s.testFails["kaputt@gmail.example"] = "auth"),
    });
    await page.goto(base + "/mail/setup");
    // A provider that takes no password any more: said plainly, no way on.
    await page.locator("#mail-setup-email").fill("max@outlook.example");
    await page.getByRole("button", { name: "Weiter" }).click();
    await page
      .getByText(/Outlook \(Test\) nimmt kein Passwort mehr an/)
      .waitFor();
    assert.equal(
      await page.getByRole("button", { name: "Weiter" }).isDisabled(),
      true,
    );
    await page.getByRole("button", { name: "Zurück" }).click();
    await page.locator("#mail-setup-email").fill("kaputt@gmail.example");
    await page.getByRole("button", { name: "Weiter" }).click();
    await page.getByText(/Gmail \(Test\) ist bekannt/).waitFor();
    await page.getByRole("button", { name: "Weiter" }).click();
    await page.getByText(/verlangt ein App-Passwort/).waitFor();
    await page.locator("#mail-setup-password").fill("falsch");
    await page.getByRole("button", { name: "Verbindung prüfen" }).click();
    await page.getByText(/Benutzername oder Passwort stimmen nicht/).waitFor();
    await sound(page, "setup failed check");
    await shot(page, "08-einrichten-fehler-1440");
    await page.getByRole("button", { name: "Zurück" }).click();
    await page.getByRole("button", { name: "Zurück" }).click();
    await page.locator("#mail-setup-email").fill("alex@gmail.example");
    await page.locator("#mail-setup-name").fill("Google");
    await page.getByRole("button", { name: "Weiter" }).click();
    await page.getByRole("button", { name: "Weiter" }).click();
    await page.locator("#mail-setup-password").fill(SECRET);
    await page.getByRole("button", { name: "Verbindung prüfen" }).click();
    await page.getByText("Eingang (IMAP): Anmeldung klappt").waitFor();
    await shot(page, "09-einrichten-1440");
    await page.getByRole("button", { name: "Speichern" }).click();
    await page.waitForURL(/\/mail\?a=acc-2&f=INBOX/);
    const post = sent(state, "POST", /\/mail\/accounts$/)[0][2];
    assert.equal(post.password, SECRET);
    assert.equal(post.imap.host, "imap.gmail.example");
    await page.getByText("Willkommen bei Ihrem neuen Postfach").waitFor();
    const stored = await page.evaluate(
      () =>
        JSON.stringify({ ...localStorage }) +
        JSON.stringify({ ...sessionStorage }),
    );
    assert.ok(
      !stored.includes(SECRET),
      "no password in the browser's storage after setup",
    );
    assert.ok(
      state.requests.every(([, p]) => !p.includes(SECRET)),
      "password never in an address",
    );
  }

  // ---- Phone 390 px: levels, the bar's arrow, 44 px, writing as a page ----
  {
    const { page, state } = await open({
      width: 390,
      height: 844,
      phone: true,
    });
    const big = async (name) => {
      const small = await page.evaluate(() =>
        [
          ...document.querySelectorAll(
            "main button, main a.btn, main input:not([type=checkbox]):not([type=radio]), main select, header.phone-bar button",
          ),
        ]
          .filter((el) => el.getClientRects().length && !el.closest(".sr-only"))
          .map((el) => [
            el.textContent.trim() || el.getAttribute("aria-label") || el.id,
            Math.round(el.getBoundingClientRect().height),
          ])
          .filter(([, h]) => h < 44),
      );
      assert.deepEqual(small, [], `${name}: targets under 44 px`);
    };
    await page.goto(base + "/mail");
    await page.locator(".mail.levels .col-folders").waitFor();
    assert.equal(
      await page.locator(".col-list").count(),
      0,
      "one level at a time",
    );
    await sound(page, "phone folders");
    await big("phone folders");
    await shot(page, "10-handy-postfaecher-390");
    await page
      .locator(".col-folders .folder", { hasText: "Posteingang" })
      .first()
      .click();
    await page.waitForURL(/\/mail\?a=acc-privat&f=INBOX$/);
    await page.locator(".row").first().waitFor();
    await sound(page, "phone list");
    await big("phone list");
    await shot(page, "11-handy-liste-390");
    await page.locator(".row").nth(1).click();
    await page.waitForURL(/&m=999$/);
    await page.locator("#mail-message-subject").waitFor();
    await sound(page, "phone message");
    await big("phone message");
    await shot(page, "12-handy-nachricht-390");
    // A swipe from the left edge goes one level up (PhoneGestures) …
    const cdp = await page.context().newCDPSession(page);
    const touch = (type, points) =>
      cdp.send("Input.dispatchTouchEvent", {
        type,
        touchPoints: points.map(([x, y], id) => ({ x, y, id })),
      });
    await touch("touchStart", [[6, 420]]);
    for (let i = 1; i <= 10; i++) await touch("touchMove", [[6 + 22 * i, 422]]);
    await touch("touchEnd", []);
    await page.waitForURL(/\/mail\?a=acc-privat&f=INBOX$/);
    // … and the bar's arrow, from the list to the mailboxes.
    await page
      .locator("header.phone-bar")
      .getByRole("button", { name: "Eine Ebene zurück" })
      .click();
    await page.waitForURL(/\/mail$/);
    // Writing on the phone is a page.
    await page
      .locator(".col-folders .folder", { hasText: "Posteingang" })
      .first()
      .click();
    await page.locator(".row").nth(1).click();
    await page.getByRole("button", { name: "Weiterleiten" }).click();
    await page.waitForURL(/\/mail\/compose\?/);
    await page.locator("#mail-subject").waitFor();
    assert.match(await page.locator("#mail-subject").inputValue(), /^Fwd: /);
    await sound(page, "phone compose");
    await big("phone compose");
    await shot(page, "13-handy-schreiben-390");
    await page.locator("#mail-to").fill("ben@example.net,");
    await page.getByRole("button", { name: /Senden/ }).click();
    await until(() => state.sent.length === 1, "sent from the phone");
    // Forwarding takes the attachment of the mail along (attachFrom).
    assert.deepEqual(state.sent[0].meta.attachFrom, {
      folder: "INBOX",
      uid: 999,
      parts: ["2"],
    });
    await page.waitForURL(/\/mail\?a=acc-privat&f=INBOX&m=999$/);
    // A digest titand could not take apart: its raw beginning, with a
    // note in words, no frame and no attachments.
    await page.goto(base + "/mail?a=acc-privat&f=Rechnungen&m=60");
    await page.locator("[data-raw-text]").waitFor();
    assert.match(
      await page.locator("[data-raw-text]").innerText(),
      /zu groß gegliedert/,
    );
    await page.getByText("Erster Beitrag der Liste").waitFor();
    assert.equal(await page.locator("iframe").count(), 0, "no frame");
    await sound(page, "phone digest");
    await big("phone digest");
    await shot(page, "12b-handy-sammelmail-390");
    // The setup on the phone.
    await page.goto(base + "/mail/setup");
    await page.locator("#mail-setup-email").fill("neu@unbekannt.example");
    await page.getByRole("button", { name: "Weiter" }).click();
    await page.getByText(/kennt T-Mail keine Server/).waitFor();
    await sound(page, "phone setup servers");
    await big("phone setup servers");
    await shot(page, "14-handy-einrichten-390");
    await page.goto(base + "/mail/settings");
    await page.getByRole("heading", { name: "Signaturen" }).waitFor();
    await sound(page, "phone settings");
    await shot(page, "15-handy-einstellungen-390");
  }

  // ---- mail_rate_limited: the checks wait, the reason is in sight ----
  {
    const { page } = await open({
      desk: false,
      change: (s) => (s.limitChecks = true),
    });
    await page.goto(base + "/mail/setup");
    await page.locator("#mail-setup-email").fill("alex@gmail.example");
    await page.getByRole("button", { name: "Weiter" }).click();
    await page.getByText(/Gmail \(Test\) ist bekannt/).waitFor();
    await page.getByRole("button", { name: "Weiter" }).click();
    await page.locator("#mail-setup-password").fill("x");
    await page.getByRole("button", { name: "Verbindung prüfen" }).click();
    await page.getByText(/T-Mail wartet noch (59|60) s/).waitFor();
    assert.equal(
      await page
        .getByRole("button", { name: "Verbindung prüfen" })
        .isDisabled(),
      true,
    );
    await sound(page, "rate limited");
    await shot(page, "19-zu-viele-pruefungen-1440");
  }

  // ---- Narrow windows (420 and 800 px, the page alone as in a window) ----
  for (const width of [420, 800]) {
    const { page } = await open({ width, desk: false });
    await page.goto(base + "/mail");
    await page.locator(".mail.levels .col-folders").waitFor();
    await sound(page, `window ${width} folders`);
    await page
      .locator(".col-folders .folder", { hasText: "Posteingang" })
      .first()
      .click();
    await page.locator(".row").first().waitFor();
    await sound(page, `window ${width} list`);
    await page.locator(".row").nth(1).click();
    await page.locator("#mail-message-subject").waitFor();
    await sound(page, `window ${width} message`);
    await shot(page, `17-fenster-nachricht-${width}`);
    // A narrow window has its own back button (no phone bar there).
    await page.getByRole("button", { name: "Zurück zu Posteingang" }).click();
    await page.waitForURL(/\/mail\?a=acc-privat&f=INBOX$/);
    await page.getByRole("button", { name: "Postfächer", exact: true }).click();
    await page.waitForURL(/\/mail$/);
  }

  // ---- The release per account under Benutzer › Konto › Apps ----
  {
    const made = await create("owner", "hell", 1440, "de");
    const state = mailState();
    await installMail(made.ctx, state);
    const { page } = made;
    await page.goto(base + "/users");
    await page.locator('[data-account="sam"]').first().click();
    const sw = page.getByRole("checkbox", {
      name: /T-Mail \(E-Mail-Programm\)/,
    });
    await sw.waitFor();
    assert.equal(await sw.isChecked(), false, "off from the start");
    await page
      .getByText(/Ab Werk aus, auch für Besitzer und Administratoren/)
      .waitFor();
    await sw.check();
    await until(() => state.released.sam === true, "released for sam");
    assert.ok(
      state.requests.some(([m, p]) => m === "PUT" && p === "/users/sam/mail"),
    );
    await sound(page, "users mail switch");
    await shot(page, "16-benutzer-freigabe-1440");
    // Deleting the account names its T-Mail mailboxes (agent-data mailAccounts).
    made.data.agentData = {
      sam: { available: true, chats: 2, folder: false, mailAccounts: 2 },
    };
    await page.reload();
    await page.locator('[data-account="sam"]').first().click();
    await page
      .getByRole("button", { name: "Löschen", exact: true })
      .first()
      .click();
    const dialog = page.getByRole("dialog");
    await dialog.getByText(/T-Mail-Postfächer \(2\)/).waitFor();
    await sound(page, "account delete with T-Mail");
    await shot(page, "18-konto-loeschen-tmail-1440");
    await dialog.getByRole("button", { name: "Abbrechen" }).click();
  }

  // ---- Without the app release: no program ----
  {
    const { page } = await open({
      desk: false,
      change: (s) => (s.disabled = true),
    });
    await page.goto(base + "/mail");
    await page
      .getByText("T-Mail ist für dein Konto nicht freigegeben")
      .waitFor();
    assert.equal(
      await page.evaluate(
        () =>
          [...document.querySelectorAll("a, button")].filter((e) =>
            /\/mail\/compose/.test(e.getAttribute("href") ?? ""),
          ).length,
      ),
      0,
    );
  }

  assert.deepEqual(pageErrors(), [], "page errors");
  console.log("test-ui-mail: ok");
} finally {
  await browser.close();
}
