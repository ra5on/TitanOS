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
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5212 UI_OUTPUT=out/validation/113-agent node scripts/test-ui-agent-panel-113.mjs
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
  assert.deepEqual(pageErrors(), [], "no errors on the pages");
  console.log("test-ui-agent-panel-113: ok");
} finally {
  await browser.close();
}
