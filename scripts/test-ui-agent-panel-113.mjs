// 1.13 (Baustand step 12, docs/AGENT.md "1.13: Mail, Kontakte, Kalender,
// Erinnerungen"): the agent's panel in Mail on isolated data (test-ui-mail-
// fixtures.mjs and the agent's answers below; nothing reaches a NAS, a
// model or a mail server). Desktop 1440 px light: the panel is a column
// right of the message (ColumnFrame's aside), the note about a provider in
// the internet comes before the first use and is kept (PUT /agent/pim-chat),
// the chip "Zusammenfassen" sends a reference to the open mail (context,
// never its content) and starts a chat of the panel (origin "panel"); a
// step in "Nur Vorschläge" shows a draft card, "Öffnen und bearbeiten" opens
// the mail editor filled with it (To, subject, text, the note) and sends
// nothing by itself. 1440 dark: the column; 1024: no room next to the list,
// a sheet, the message keeps 320 px. Narrow window 800 px light and dark and
// phone 390 px light and
// dark: the panel is a sheet from below. On every page: no sideways scroll,
// no cut text.
// Kontakte and Kalender (test-ui-kontakte-fixtures.mjs, test-ui-kalender-
// fixtures.mjs, the clock at Tuesday 17 November 2026 in Berlin): the panel
// opens from the side bar (1440 light and dark: a column), from "‹ Zurück"'s
// row (800: a sheet) and from the bar at the bottom (390: a sheet); the
// chips send a reference only (the open contact's book and id; the shown
// day, an event asked about from its details), "Daten ergänzen" waits for
// an open contact. Drafts: a new contact and a change fill the form of
// Kontakte, a new event and a change of a series fill the event window, a
// reminder opens the reminder dialog and "abhaken" asks first – each from
// the card in the panel of the same page (no reload) and nothing written
// before the person's click. The desktop: the card in a Kontakte window
// fills that window; a reminder opens Kalender's window, an event its own.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5212 UI_OUTPUT=out/validation/113-agent node scripts/test-ui-agent-panel-113.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import { installMail, mailState } from "./test-ui-mail-fixtures.mjs";
import { calendarState, installCalendar } from "./test-ui-kalender-fixtures.mjs";
import { contactsState, installContacts } from "./test-ui-kontakte-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5212";
const out = process.env.UI_OUTPUT || "out/validation/113-agent";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const SANDBOXED =
  /document is sandboxed and lacks the 'allow-same-origin' flag/;
const pageErrors = () =>
  errors.filter((e) => !SANDBOXED.test(e) && !/503/.test(e));

const CHAT = "panelchat01";
const STEP = "draftstep01";
const draft = {
  app: "mail",
  kind: "reply",
  fields: {
    a: "acc-privat",
    to: ["service@bank.example"],
    cc: [],
    bcc: [],
    subject: "Re: Ihr Kontoauszug November",
    text: "Guten Tag,\n\nvielen Dank für den Auszug.\n\nViele Grüße\nAlex",
    f: "INBOX",
    uid: 1000,
  },
};

async function open({
  width = 1440,
  height = 900,
  phone = false,
  theme = "hell",
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
          deviceScaleFactor: 3,
          viewport: { width, height },
        }
      : { viewport: { width, height } },
  );
  await made.ctx.addInitScript(() => {
    localStorage.removeItem("titanos.mail.agentPanel");
  });
  await installMail(made.ctx, mailState());
  const seen = { panel: [], posts: [] };
  const panel = { chat: "", cloud: true, cloudNoted: false, mail: true };
  const chat = () => ({
    id: CHAT,
    title: "Zusammenfassen",
    created: "2026-11-18T10:00:00Z",
    updated: "2026-11-18T10:00:05Z",
    origin: "panel",
    state: "idle",
    entries: [
      {
        id: "u1",
        at: "2026-11-18T10:00:00Z",
        role: "user",
        content: seen.posts[0]?.message ?? "",
        context: seen.posts[0]?.context,
      },
      {
        id: STEP,
        at: "2026-11-18T10:00:03Z",
        role: "tool",
        tool: "mail_send",
        ok: true,
        kind: "change",
        result: '{"draft":true}',
        draft,
      },
      {
        id: "a1",
        at: "2026-11-18T10:00:05Z",
        role: "assistant",
        content:
          "Die Sparkasse schickt den Kontoauszug für November. Ich habe eine kurze Antwort als Entwurf vorbereitet.",
      },
    ],
  });
  // Later routes win: the agent's answers over the fixtures.
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const json = (value, status = 200) =>
      route.fulfill({ status, json: value });
    if (p === "/services/state")
      return json({ docker: true, vms: true, lxc: true, agent: true });
    if (!p.startsWith("/agent")) return route.fallback();
    if (p === "/agent/pim-chat") {
      if (method === "PUT") {
        const body = route.request().postDataJSON();
        seen.panel.push(body);
        Object.assign(panel, body);
        return json(panel);
      }
      return json(panel);
    }
    if (p === "/agent/chats" && method === "POST") {
      seen.posts.push(route.request().postDataJSON());
      return json(chat(), 201);
    }
    if (p === `/agent/chats/${CHAT}/events`)
      return route.fulfill({
        status: 200,
        headers: { "content-type": "text/event-stream" },
        body: "event: end\ndata: {}\n\n",
      });
    if (p === `/agent/chats/${CHAT}`) return json(chat());
    if (p === `/agent/chats/${CHAT}/drafts/${STEP}`) return json(draft);
    return json({ error: { code: "not_found", message: "fixture" } }, 404);
  });
  return { ...made, seen, panel };
}

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

// ---------- Kontakte and Kalender ----------
const NOW = new Date("2026-11-17T12:29:00+01:00");
const PIM_DRAFTS = {
  contactNew: {
    tool: "contacts_create",
    draft: {
      app: "contacts",
      kind: "contact",
      fields: {
        first: "Clara",
        last: "Neumann",
        name: "Clara Neumann",
        org: "Neumann Bau GmbH",
        emails: [{ type: "work", value: "clara@neumann-bau.example" }],
        phones: [{ type: "work", value: "+49 221 777 0101" }],
      },
    },
  },
  contactChange: {
    tool: "contacts_update",
    draft: {
      app: "contacts",
      kind: "contact",
      fields: { book: "kontakte", id: "ben.vcf", org: "Muster & Söhne" },
    },
  },
  eventNew: {
    tool: "calendar_create",
    draft: {
      app: "calendar",
      kind: "event",
      fields: {
        title: "Zahnarzt Dr. Weiß",
        start: "2026-11-19T08:30:00+01:00",
        end: "2026-11-19T09:15:00+01:00",
        allDay: false,
        timezone: "Europe/Berlin",
        location: "Hauptstraße 1, Köln",
        notes: "Versichertenkarte mitnehmen",
        calendar: "arbeit",
        alarms: [30],
      },
    },
  },
  eventChange: {
    tool: "calendar_update",
    draft: {
      app: "calendar",
      kind: "event",
      fields: {
        cal: "arbeit",
        id: "wochenbesprechung.ics",
        occurrence: "20261116T080000Z",
        start: "2026-11-16T10:00",
        end: "2026-11-16T10:30",
        scope: "this",
      },
    },
  },
  taskNew: {
    tool: "tasks_create",
    draft: {
      app: "tasks",
      kind: "task",
      fields: {
        title: "Winterreifen aufziehen lassen",
        due: "2026-11-21T10:30",
        priority: 1,
        list: "einkauf",
        notes: "Werkstatt anrufen",
      },
    },
  },
  taskDone: {
    tool: "tasks_complete",
    draft: {
      app: "tasks",
      kind: "complete",
      fields: {
        list: "erinnerungen--titanos",
        id: "steuer.ics",
        title: "Steuererklärung abgeben",
        done: true,
      },
    },
  },
};
const STEPS = {
  contacts: ["contactNew", "contactChange"],
  calendar: ["eventNew", "eventChange", "taskNew", "taskDone"],
};

/** Kontakte or Kalender with the agent's panel on isolated data. */
async function openPim(
  app,
  { width = 1440, height = 900, phone = false, theme = "hell", desk = false, steps } = {},
) {
  const made = await create(
    "user",
    theme,
    width,
    "de",
    phone
      ? {
          isMobile: true,
          hasTouch: true,
          deviceScaleFactor: 3,
          viewport: { width, height },
          timezoneId: "Europe/Berlin",
        }
      : { viewport: { width, height }, timezoneId: "Europe/Berlin" },
  );
  await made.ctx.clock.setFixedTime(NOW);
  if (phone || desk)
    await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  // Closed when the page first loads; later loads keep what the page set.
  await made.ctx.addInitScript(() => {
    if (sessionStorage.getItem("panel-test")) return;
    sessionStorage.setItem("panel-test", "1");
    localStorage.removeItem("titanos.contacts.agentPanel");
    localStorage.removeItem("titanos.calendar.agentPanel");
  });
  const cal = calendarState();
  await installCalendar(made.ctx, cal);
  const con = contactsState();
  await installContacts(made.ctx, con);
  const seen = { panel: [], posts: [], messages: [] };
  const panel = { chat: "", cloud: false, cloudNoted: false, mail: false };
  const shown = steps ?? STEPS[app];
  const chat = () => ({
    id: CHAT,
    title: "Kontakte und Kalender",
    created: "2026-11-17T11:00:00Z",
    updated: "2026-11-17T11:00:05Z",
    origin: "panel",
    state: "idle",
    entries: [
      {
        id: "u1",
        at: "2026-11-17T11:00:00Z",
        role: "user",
        content: seen.posts[0]?.message ?? "Hallo",
        context: seen.posts[0]?.context,
      },
      ...shown.map((step, i) => ({
        id: step,
        at: `2026-11-17T11:00:0${i + 1}Z`,
        role: "tool",
        tool: PIM_DRAFTS[step].tool,
        ok: true,
        kind: "change",
        result: '{"draft":true}',
        draft: PIM_DRAFTS[step].draft,
      })),
      {
        id: "a1",
        at: "2026-11-17T11:00:09Z",
        role: "assistant",
        content: "Ich habe die Vorschläge als Entwürfe vorbereitet.",
      },
    ],
  });
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const json = (value, status = 200) => route.fulfill({ status, json: value });
    if (p === "/services/state")
      return json({ docker: true, vms: true, lxc: true, agent: true, calendar: true, contacts: true });
    if (!p.startsWith("/agent")) return route.fallback();
    if (p === "/agent/pim-chat") {
      if (method === "PUT") {
        const body = route.request().postDataJSON();
        seen.panel.push(body);
        Object.assign(panel, body);
      }
      return json(panel);
    }
    if (p === "/agent/chats" && method === "POST") {
      seen.posts.push(route.request().postDataJSON());
      return json(chat(), 201);
    }
    if (p === `/agent/chats/${CHAT}/messages` && method === "POST") {
      seen.messages.push(route.request().postDataJSON());
      return json(chat());
    }
    if (p === `/agent/chats/${CHAT}/events`)
      return route.fulfill({
        status: 200,
        headers: { "content-type": "text/event-stream" },
        body: "event: end\ndata: {}\n\n",
      });
    if (p === `/agent/chats/${CHAT}`) return json(chat());
    const m = p.match(/^\/agent\/chats\/([^/]+)\/drafts\/([^/]+)$/);
    if (m && m[1] === CHAT && PIM_DRAFTS[m[2]]) return json(PIM_DRAFTS[m[2]].draft);
    return json({ error: { code: "not_found", message: "fixture" } }, 404);
  });
  /** Requests that write (anything but GET) to Kalender or Kontakte. */
  const writes = () =>
    [...cal.requests, ...con.requests].filter(
      // POST /calendar/preview only reads the next dates of a repetition.
      ([m, path]) =>
        m !== "GET" && !path.startsWith("/calendar/stream") && path !== "/calendar/preview",
    );
  return { ...made, seen, panel, cal, con, writes };
}

/** The card of a draft in the panel (its title names the kind). */
const card = (scope, title) => scope.locator(".draft").filter({ hasText: title });
const openCard = (scope, title) =>
  card(scope, title).getByRole("button", { name: "Öffnen und bearbeiten" }).click();

try {
  // ---- Desktop 1440, light: the panel as a column ----
  {
    const { page, seen } = await open();
    await page.goto(base + "/mail?a=acc-privat&f=INBOX&m=1000");
    await page.locator(".col-message h1, .col-message h2").first().waitFor();
    await page
      .locator(".col-folders .agent-toggle", { hasText: "KI-Agent" })
      .click();
    const aside = page.locator(".cf-aside .agent-panel");
    await aside.waitFor();
    const box = await aside.boundingBox();
    assert.ok(
      box.width >= 300 && box.x + box.width >= 1438,
      `the panel is the right column: ${JSON.stringify(box)}`,
    );
    // The provider in the internet: said before the first use.
    const note = aside.locator(".cloud");
    await note.waitFor();
    assert.ok(
      (await note.innerText()).includes("Anbieter"),
      "the cloud note names the provider",
    );
    const chip = aside.getByRole("button", { name: "Zusammenfassen" });
    assert.ok(await chip.isDisabled(), "nothing goes out before the note");
    await shot(page, "01-panel-hinweis-1440-hell");
    await aside.getByRole("button", { name: "Verstanden" }).click();
    await note.waitFor({ state: "detached" });
    assert.deepEqual(seen.panel.at(-1), { cloudNoted: true });
    for (const label of [
      "Zusammenfassen",
      "Antwort entwerfen",
      "Termin aus Mail",
      "Absender als Kontakt",
      "Ordner vorschlagen",
    ])
      await aside.getByRole("button", { name: label }).waitFor();
    await chip.click();
    await aside.locator(".draft").waitFor();
    const sent = seen.posts[0];
    assert.equal(sent.origin, "panel");
    assert.deepEqual(sent.context, {
      app: "mail",
      mail: { a: "acc-privat", f: "INBOX", uid: 1000 },
    });
    assert.ok(
      !JSON.stringify(sent).includes("Kontoauszug für November liegt"),
      "the mail's content is not in the message",
    );
    assert.deepEqual(seen.panel.at(-1), { chat: CHAT });
    await aside.getByText("Entwurf einer E-Mail").waitFor();
    await sound(page, "1440 panel");
    await shot(page, "02-panel-entwurf-1440-hell");
    // Open and edit: the editor filled with the draft, nothing sent.
    let sends = 0;
    await page.route(/\/api\/v1\/mail\/accounts\/[^/]+\/send$/, (route) => {
      sends++;
      return route.fulfill({ status: 200, json: {} });
    });
    await aside.getByRole("button", { name: "Öffnen und bearbeiten" }).click();
    await page.waitForURL(/\/mail\/compose\?agentDraft=/);
    await page.getByText("Entwurf des KI-Agenten").waitFor();
    await page.getByText("service@bank.example").first().waitFor();
    assert.equal(
      await page.locator("input[id*=subject], input[name=subject]").first().inputValue(),
      "Re: Ihr Kontoauszug November",
    );
    assert.ok(
      (await page.locator("textarea").first().inputValue()).includes(
        "vielen Dank für den Auszug",
      ),
    );
    assert.equal(sends, 0, "nothing was sent by itself");
    await sound(page, "1440 editor");
    await shot(page, "03-editor-aus-entwurf-1440-hell");
    await page.context().close();
  }

  // ---- Narrow window 800 dark and the phone: a sheet from below ----
  // ---- 1440 dark: the column; 1024: no room next to the list, a sheet ----
  for (const [width, theme] of [
    [1440, "dunkel"],
    [1024, "hell"],
  ]) {
    const { page, panel } = await open({ width, height: 768, theme });
    panel.cloud = false;
    await page.goto(base + "/mail?a=acc-privat&f=INBOX&m=1000");
    await page.locator(".col-message").first().waitFor();
    await page.locator(".col-folders .agent-toggle").click();
    const where = width >= 1200 ? ".cf-aside .agent-panel" : "[role=dialog] .agent-panel";
    await page.locator(where).waitFor();
    const content = await page.locator(".cf-content").boundingBox();
    assert.ok(content.width >= 320, `${width}: the message keeps 320 px (${content.width})`);
    await page.locator(where).locator("textarea").scrollIntoViewIfNeeded();
    assert.ok(await page.locator(where).locator("textarea").isVisible(), `${width}: the input is in view`);
    await sound(page, `${width} ${theme}`);
    await shot(page, `05-panel-${width}-${theme}`);
    await page.context().close();
  }

  for (const [width, phone, theme] of [
    [800, false, "hell"],
    [800, false, "dunkel"],
    [390, true, "hell"],
    [390, true, "dunkel"],
  ]) {
    const { page, panel } = await open({
      width,
      height: phone ? 844 : 900,
      phone,
      theme,
    });
    panel.cloud = false;
    await page.goto(base + "/mail?a=acc-privat&f=INBOX&m=1000");
    await page.locator(".col-message").first().waitFor();
    // The phone: in the bar at the bottom ("Weitere Aktionen"), the narrow window: the toolbar.
    if (phone) {
      await page.getByRole("button", { name: "Weitere Aktionen" }).last().click();
      await page.getByRole("menuitem", { name: "KI-Agent" }).click();
    } else {
      await page
        .locator(".col-message")
        .getByRole("button", { name: "KI-Agent" })
        .click();
    }
    const sheet = page.locator('[role=dialog] .agent-panel');
    await sheet.waitFor();
    await sheet.getByRole("button", { name: "Zusammenfassen" }).waitFor();
    await page.waitForTimeout(600); // the sheet slides up
    const input = await sheet.locator("textarea").boundingBox();
    assert.ok(
      input && input.y + input.height <= (phone ? 844 : 900),
      `${width}: the input stands in the sheet's view ${JSON.stringify(input)}`,
    );
    await sound(page, `${width} ${theme} sheet`);
    await shot(page, `04-panel-blatt-${width}-${theme}`);
    await page.context().close();
  }

  // ---- Kontakte 1440 light: the column, the note, the chips, the drafts ----
  {
    const { page, seen, panel, writes } = await openPim("contacts");
    panel.cloud = true;
    await page.goto(base + "/contacts");
    await page.locator("[data-pim-ready]").waitFor();
    await page.locator(".col-side .agent-toggle", { hasText: "KI-Agent" }).click();
    const aside = page.locator(".cf-aside .agent-panel");
    await aside.waitFor();
    const box = await aside.boundingBox();
    assert.ok(box.width >= 300 && box.x + box.width >= 1438, `Kontakte: a column ${JSON.stringify(box)}`);
    assert.equal(
      await page.locator(".col-side .agent-toggle").getAttribute("aria-pressed"),
      "true",
    );
    await aside.locator(".cloud").waitFor();
    const complete = aside.getByRole("button", { name: "Daten ergänzen" });
    assert.ok(await complete.isDisabled(), "nothing before the note");
    await shot(page, "panel-kontakte-hinweis-1440-hell");
    await aside.getByRole("button", { name: "Verstanden" }).click();
    await aside.locator(".cloud").waitFor({ state: "detached" });
    await aside.getByRole("button", { name: "Dubletten finden" }).waitFor();
    assert.ok(await complete.isDisabled(), "Daten ergänzen waits for an open contact");
    // "Kontakt aus Text" puts its words into the input, nothing goes out.
    await aside.getByRole("button", { name: "Kontakt aus Text" }).click();
    assert.match(await aside.locator("textarea").inputValue(), /^Lege aus dieser Signatur/);
    assert.equal(seen.posts.length, 0, "a chip that fills sends nothing");
    await aside.locator("textarea").fill("");
    // A contact opened: the chip sends its reference, not its numbers.
    await page.locator(".col-list").getByText("Ben Muster").first().click();
    await page.waitForURL(/id=ben\.vcf/);
    await until(async () => !(await complete.isDisabled()), "Daten ergänzen enabled");
    await complete.click();
    await aside.locator(".draft").first().waitFor();
    const sent = seen.posts[0];
    assert.equal(sent.origin, "panel");
    assert.deepEqual(sent.context, { app: "contacts", card: { book: "kontakte", id: "ben.vcf" } });
    assert.ok(!JSON.stringify(sent).includes("151"), "the contact's number is not in the message");
    await sound(page, "Kontakte 1440 panel");
    await shot(page, "panel-kontakte-1440-hell");
    // A new contact from the card in this page's panel (no reload).
    await openCard(aside, "Entwurf eines Kontakts");
    await page.locator("[data-agent-draft]").waitFor();
    assert.equal(await page.locator('[data-content-mode="new"]').count(), 1, "the form of a new contact");
    const values = await page
      .locator(".col-contact input")
      .evaluateAll((els) => els.map((e) => e.value));
    for (const v of ["Clara", "Neumann", "Neumann Bau GmbH", "clara@neumann-bau.example"])
      assert.ok(values.includes(v), `the form holds ${v}: ${values.join(" | ")}`);
    assert.ok(!page.url().includes("agentDraft"), "the draft left the address");
    assert.deepEqual(writes(), [], "nothing written before Speichern");
    await sound(page, "Kontakte draft new");
    await shot(page, "panel-kontakte-entwurf-neu-1440-hell");
    await page.locator("[data-editor-cancel]").click();
    const discard = page.locator("dialog[open]").last();
    if (await discard.isVisible().catch(() => false)) {
      const leave = discard.getByRole("button", { name: /Verwerfen|Nicht speichern|Ohne Speichern/ });
      if (await leave.count()) await leave.first().click();
    }
    // The change of Ben: his form with the new company, the rest as it was.
    await openCard(aside, "Entwurf einer Kontaktänderung");
    await page.waitForURL(/id=ben\.vcf/);
    await page.locator("[data-agent-draft]").waitFor();
    const changed = await page
      .locator(".col-contact input")
      .evaluateAll((els) => els.map((e) => e.value));
    assert.ok(changed.includes("Muster & Söhne") && changed.includes("Ben"), changed.join(" | "));
    assert.deepEqual(writes(), [], "nothing written before Speichern");
    await shot(page, "panel-kontakte-entwurf-aenderung-1440-hell");
    // Saving is the person's: then one PUT with the new company.
    await page.locator("[data-editor-save]").click();
    await until(() => writes().some(([m, path]) => m === "PUT" && path.includes("ben.vcf")), "PUT ben");
    const put = writes().find(([m, path]) => m === "PUT" && path.includes("ben.vcf"));
    assert.equal(put[2].contact.org, "Muster & Söhne");
    await page.context().close();
  }

  // ---- Kalender 1440 light: the column, chips, an event asked about, drafts ----
  {
    const { page, seen, panel, cal, writes } = await openPim("calendar");
    panel.cloud = true;
    await page.goto(base + "/calendar");
    await page.locator('[data-pim-ready][data-view="month"]').waitFor();
    await page.locator(".col-cal-side .agent-toggle", { hasText: "KI-Agent" }).click();
    const aside = page.locator(".cf-aside .agent-panel");
    await aside.waitFor();
    const box = await aside.boundingBox();
    assert.ok(box.width >= 300 && box.x + box.width >= 1438, `Kalender: a column ${JSON.stringify(box)}`);
    const main = await page.locator(".cf-content").boundingBox();
    assert.ok(main.width >= 560, `the month keeps 560 px (${main.width})`);
    await aside.locator(".cloud").waitFor();
    await shot(page, "panel-kalender-hinweis-1440-hell");
    await aside.getByRole("button", { name: "Verstanden" }).click();
    await aside.locator(".cloud").waitFor({ state: "detached" });
    for (const label of ["Termin eintragen", "Freie Zeit finden", "Tagesüberblick", "Wochenüberblick"])
      await aside.getByRole("button", { name: label }).waitFor();
    await aside.getByRole("button", { name: "Termin eintragen" }).click();
    assert.match(await aside.locator("textarea").inputValue(), /^Trage ein:/);
    assert.equal(seen.posts.length, 0, "a chip that fills sends nothing");
    await aside.locator("textarea").fill("");
    await aside.getByRole("button", { name: "Wochenüberblick" }).click();
    await aside.locator(".draft").first().waitFor();
    assert.deepEqual(seen.posts[0].context, { app: "calendar", date: "2026-11-17" });
    assert.match(seen.posts[0].message, /Woche/);
    // An event asked about from its details: its reference goes with the next message.
    await page.locator(".month .ev", { hasText: "Wochenbesprechung" }).first().click();
    const detail = page.locator("dialog[open]").last();
    await detail.locator("[data-event-detail]").waitFor();
    await detail.getByRole("button", { name: "KI-Agent" }).click();
    await detail.waitFor({ state: "detached" });
    await aside.getByRole("button", { name: "Tagesüberblick" }).click();
    await until(() => seen.messages.length === 1, "the second message");
    const ctx = seen.messages[0].context;
    assert.equal(ctx.app, "calendar");
    assert.equal(ctx.event.cal, "arbeit");
    assert.equal(ctx.event.id, "wochenbesprechung.ics");
    assert.match(ctx.event.occurrence, /^\d{8}T\d{6}Z$/);
    assert.ok(!JSON.stringify(seen.messages[0]).includes("09:00"), "no times of the event in the message");
    await sound(page, "Kalender 1440 panel");
    await shot(page, "panel-kalender-1440-hell");
    // A reminder from the card in this page's panel: the dialog, filled.
    await openCard(aside, "Entwurf einer Erinnerung");
    const dlg = page.locator("dialog[open]").last();
    await dlg.locator("[data-task-dialog] [data-agent-draft]").waitFor();
    assert.equal(await dlg.locator("input[type=text]").first().inputValue(), "Winterreifen aufziehen lassen");
    assert.equal(await dlg.locator("select").inputValue(), "einkauf");
    assert.equal(await dlg.locator("input[type=date]").inputValue(), "2026-11-21");
    assert.equal(await dlg.locator("input[type=time]").inputValue(), "10:30");
    assert.equal(await dlg.locator("textarea").inputValue(), "Werkstatt anrufen");
    assert.ok(!page.url().includes("agentDraft"), "the draft left the address");
    assert.deepEqual(writes(), [], "nothing written before Sichern");
    await sound(page, "reminder draft");
    await shot(page, "panel-kalender-erinnerung-1440-hell");
    await dlg.getByRole("button", { name: "Sichern" }).click();
    await until(() => writes().some(([m, path]) => m === "POST" && path === "/calendar/tasks"), "POST task");
    const made = writes().find(([m, path]) => m === "POST" && path === "/calendar/tasks")[2];
    assert.equal(made.title, "Winterreifen aufziehen lassen");
    assert.equal(made.list, "einkauf");
    assert.equal(made.due, "2026-11-21T10:30");
    assert.equal(made.priority, 1);
    // Ticking off asks first.
    const before = writes().length;
    await openCard(aside, "Vorschlag: Erinnerung abhaken");
    const ask = page.locator("dialog[open]").last();
    await ask.getByText("„Steuererklärung abgeben“ als erledigt markieren?").waitFor();
    assert.equal(writes().length, before, "nothing ticked off before the button");
    await shot(page, "panel-kalender-abhaken-1440-hell");
    await ask.locator("[data-done-confirm]").click();
    await until(() => writes().some(([, path]) => path.endsWith("/steuer.ics/done")), "done");
    assert.equal(writes().find(([, path]) => path.endsWith("/steuer.ics/done"))[2].done, true);
    // A new event: the event window, filled; saved only with "Speichern".
    await openCard(aside, "Entwurf eines Termins");
    await page.waitForURL(/\/calendar\/event\?agentDraft=/);
    await page.locator("[data-event-form] [data-agent-draft]").waitFor();
    assert.equal(await page.locator("#t-title").inputValue(), "Zahnarzt Dr. Weiß");
    assert.equal(await page.locator("#t-from").inputValue(), "2026-11-19");
    assert.deepEqual(
      await page.locator("input[type=time]").evaluateAll((els) => els.map((e) => e.value)),
      ["08:30", "09:15"],
    );
    assert.equal(await page.locator("#t-place").inputValue(), "Hauptstraße 1, Köln");
    assert.equal(await page.locator("#t-note").inputValue(), "Versichertenkarte mitnehmen");
    assert.ok(await page.locator('input[name=cal][value="arbeit"]').isChecked(), "calendar Arbeit");
    await page.getByText("30 Minuten vorher").first().waitFor();
    assert.equal(cal.created.length, 0, "nothing created before Speichern");
    await sound(page, "event draft");
    await shot(page, "panel-kalender-termin-entwurf-1440-hell");
    await page.getByRole("button", { name: "Speichern" }).click();
    await until(() => cal.created.length === 1, "POST event");
    const ev = cal.created[0];
    assert.equal(ev.calendar, "arbeit");
    assert.equal(ev.event?.title ?? ev.title, "Zahnarzt Dr. Weiß");
    await page.context().close();
  }

  // ---- The event window with a change of a series: the series asks as always ----
  {
    const { page, writes } = await openPim("calendar");
    await page.goto(base + `/calendar/event?agentDraft=${CHAT}:eventChange`);
    await page.locator("[data-event-form] [data-agent-draft]").waitFor();
    assert.equal(await page.locator("#t-title").inputValue(), "Wochenbesprechung");
    assert.equal(await page.locator("#t-from").inputValue(), "2026-11-16");
    const times = await page.locator("input[type=time]").evaluateAll((els) => els.map((e) => e.value));
    assert.deepEqual(times, ["10:00", "10:30"]);
    await shot(page, "panel-kalender-terminaenderung-1440-hell");
    await page.getByRole("button", { name: "Speichern" }).click();
    const series = page.locator("dialog[open]").last();
    await series.getByText("Nur diesen Termin").first().waitFor();
    assert.ok(
      await series.getByRole("radio", { name: /Nur diesen Termin/ }).isChecked(),
      "the draft's scope",
    );
    await series.getByRole("button", { name: "Abbrechen" }).click();
    assert.deepEqual(writes(), [], "nothing changed without the person");
    // A draft that is gone: the window opens empty with a short note.
    await page.goto(base + `/calendar/event?agentDraft=${CHAT}:gibtsnicht`);
    await page.locator("[data-event-form]").waitFor();
    await page.getByText("Der Entwurf des KI-Agenten ist nicht mehr da.").waitFor();
    assert.equal(await page.locator("#t-title").inputValue(), "");
    await page.context().close();
  }

  // ---- 1440 dark, 800 light/dark, 390 light/dark: column or sheet ----
  for (const app of ["contacts", "calendar"]) {
    for (const [width, phone, theme] of [
      [1440, false, "dunkel"],
      [800, false, "hell"],
      [800, false, "dunkel"],
      [390, true, "hell"],
      [390, true, "dunkel"],
    ]) {
      const height = phone ? 844 : 900;
      const { page, seen } = await openPim(app, { width, height, phone, theme });
      const name = app === "contacts" ? "kontakte" : "kalender";
      await page.goto(base + (app === "contacts" ? "/contacts" : "/calendar"));
      await page.locator("[data-pim-ready]").first().waitFor();
      // Kontakte opens on the list's level: the KI-Agent next to "‹ Zurück"
      // (800); on the phone (1.15) in the ⋯ menu of the phone's bar.
      if (phone) await page.locator('header.phone-bar button[aria-haspopup="menu"]').click();
      const toggle =
        width >= 900
          ? page.locator(".agent-toggle", { hasText: "KI-Agent" })
          : phone
            ? page.getByRole("menuitem", { name: "KI-Agent" })
            : app === "contacts"
              ? page.locator(".agent-level")
              : page.locator(".agent-toggle");
      await toggle.first().click();
      const where =
        width >= 900 ? ".cf-aside .agent-panel" : "[role=dialog] .agent-panel";
      const panelBox = page.locator(where);
      await panelBox.waitFor();
      await panelBox.getByRole("button", { name: app === "contacts" ? "Dubletten finden" : "Tagesüberblick" }).waitFor();
      await page.waitForTimeout(600); // the sheet slides up
      const input = await panelBox.locator("textarea").boundingBox();
      assert.ok(
        input && input.y + input.height <= height,
        `${app} ${width} ${theme}: the input in view ${JSON.stringify(input)}`,
      );
      await sound(page, `${app} ${width} ${theme}`);
      await shot(page, `panel-${name}-${width}-${theme}`);
      // Closed again: kept closed on this device.
      await panelBox.getByRole("button", { name: "Schließen" }).first().click();
      await panelBox.waitFor({ state: "detached" });
      assert.equal(
        await page.evaluate((k) => localStorage.getItem(k), `titanos.${app}.agentPanel`),
        null,
      );
      assert.equal(seen.posts.length, 0);
      await page.context().close();
    }
  }

  // ---- The desktop: cards open in the program's window ----
  {
    const { page, panel } = await openPim("contacts", {
      desk: true,
      height: 1000,
      steps: ["contactNew", "taskNew", "eventNew"],
    });
    panel.chat = CHAT;
    const frameAt = async (prefix) => {
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
    };
    await page.goto(base + "/contacts");
    await page.locator("section.win").first().waitFor();
    const win = await frameAt("/contacts");
    await win.locator("[data-pim-ready]").waitFor();
    await win.locator(".agent-toggle", { hasText: "KI-Agent" }).click();
    const inWin = win.locator(".agent-panel");
    await inWin.locator(".draft").first().waitFor();
    await shot(page, "panel-arbeitsplatz-kontakte");
    // The contact's card: the same window takes it.
    await openCard(inWin, "Entwurf eines Kontakts");
    await win.locator("[data-agent-draft]").waitFor();
    assert.equal(await page.locator("section.win").count(), 1, "the Kontakte window itself");
    // The reminder's card: Kalender's window with the dialog.
    await openCard(inWin, "Entwurf einer Erinnerung");
    const calWin = await frameAt("/calendar");
    await calWin.locator("[data-task-dialog] [data-agent-draft]").waitFor({ timeout: 15000 });
    assert.equal(
      await calWin.locator("[data-task-dialog] input[type=text]").first().inputValue(),
      "Winterreifen aufziehen lassen",
    );
    await shot(page, "panel-arbeitsplatz-erinnerung");
    await calWin.locator("dialog[open]").last().getByRole("button", { name: "Abbrechen" }).click();
    // The event's card: a window of its own (the Kalender window lies over
    // the Kontakte window now: the button is pressed where it is).
    await card(inWin, "Entwurf eines Termins")
      .getByRole("button", { name: "Öffnen und bearbeiten" })
      .dispatchEvent("click");
    const evWin = await frameAt("/calendar/event");
    await evWin.locator("[data-event-form] [data-agent-draft]").waitFor({ timeout: 15000 });
    assert.equal(await evWin.locator("#t-title").inputValue(), "Zahnarzt Dr. Weiß");
    await until(async () => (await page.locator("section.win").count()) >= 3, "three windows");
    await sound(page, "desktop");
    await shot(page, "panel-arbeitsplatz-termin");
    await page.context().close();
  }

  assert.deepEqual(pageErrors(), [], "no errors on the pages");
  console.log("test-ui-agent-panel-113: ok");
} finally {
  await browser.close();
}
