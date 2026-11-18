// 1.13 (Baustand step 5, Mail's folder state): how many requests a usual
// round in Mail makes – open Mail at 1440 px, open five mails (two of them
// unread), move three by dragging them onto a folder, and three events of
// the live stream (two new mails, one change of marks) on the way. Isolated
// data (test-ui-mail-fixtures.mjs), nothing reaches a NAS or a mail server.
// The stream is held open until the round sends its events, then it ends
// and is refused from then on (no reconnect storm, no "resync" noise), so
// the numbers depend on the page, not on the stream's timing.
// Prints the requests per kind as JSON; MEASURE_OUT writes them to a file.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5212 node scripts/measure-ui-mail-requests.mjs
import { writeFile } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
import { installMail, mailState } from "./test-ui-mail-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

const base = process.env.UI_URL || "http://127.0.0.1:5212";
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const create = fixtureFactory(browser, errors);

async function until(fn, what, ms = 8000) {
  const end = Date.now() + ms;
  while (!(await fn())) {
    if (Date.now() > end) throw new Error(`timed out: ${what}`);
    await new Promise((r) => setTimeout(r, 50));
  }
}

const KINDS = [
  ["accounts", "GET", /^\/mail\/accounts$/],
  ["folders", "GET", /^\/mail\/accounts\/[^/]+\/folders/],
  ["list", "GET", /^\/mail\/accounts\/[^/]+\/messages\?/],
  ["message", "GET", /^\/mail\/accounts\/[^/]+\/message\?/],
  ["flags", "POST", /\/flags$/],
  ["move", "POST", /\/move$/],
];

try {
  const { page } = await create("user", "hell", 1440, "de", {
    viewport: { width: 1440, height: 1000 },
  });
  const state = mailState();
  state.flagsBeforeRead = true;
  // Of the first five, the 2nd and 4th are unread: opening them counts.
  state.messages["acc-privat"].INBOX.forEach((r, i) => {
    if (r.flags) r.flags.seen = !(i === 1 || i === 3);
  });
  await installMail(page, state);
  // The live stream: the first one waits for the round's events.
  let release;
  const held = new Promise((r) => (release = r));
  let streams = 0;
  await page.route(/\/api\/v1\/mail\/events/, async (route) => {
    streams++;
    if (streams > 1) return route.abort();
    const events = await held;
    const body =
      [
        ": hallo",
        ...events.map((e) => `event: ${e.type}\ndata: ${JSON.stringify(e.data)}`),
      ].join("\n\n") + "\n\n";
    return route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body,
    });
  });

  await page.goto(base + "/mail");
  await page.locator(".row").first().waitFor();
  await page.waitForURL(/a=acc-privat&f=INBOX/);
  await page.waitForTimeout(1500);
  const opened = state.requests.length;

  // Five mails, one after the other, each read before the next.
  for (let i = 0; i < 5; i++) {
    const entry = page.locator(".entry").nth(i);
    const uid = await entry.getAttribute("data-uid");
    await entry.locator(".row").click();
    await page.waitForURL(new RegExp(`&m=${uid}`));
    await page.locator("#mail-message-subject").waitFor();
    await page.waitForTimeout(400);
  }

  // Live: two new mails in the inbox and a change of marks.
  const inbox = state.messages["acc-privat"].INBOX;
  release([
    { type: "mail", data: { account: "acc-privat", folder: "INBOX", uid: inbox[0].uid } },
    { type: "mail", data: { account: "acc-privat", folder: "INBOX", uid: inbox[0].uid } },
    {
      type: "flags",
      data: { account: "acc-privat", folder: "INBOX", uids: [inbox[2].uid] },
    },
  ]);
  await page.waitForTimeout(800);

  // Three moved, one drag each onto "Rechnungen".
  for (let i = 0; i < 3; i++) {
    const before = state.requests.filter(([m, p]) => m === "POST" && /\/move$/.test(p)).length;
    await page
      .locator(".entry")
      .nth(6)
      .dragTo(page.locator(".col-folders .folder", { hasText: "Rechnungen" }).first());
    await until(
      () =>
        state.requests.filter(([m, p]) => m === "POST" && /\/move$/.test(p)).length > before,
      "moved",
    );
    await page.waitForTimeout(400);
  }
  // Long enough for anything held back to go out.
  await page.waitForTimeout(8000);

  const count = (from = 0) => {
    const out = { total: 0 };
    for (const [name] of KINDS) out[name] = 0;
    for (const [m, p] of state.requests.slice(from)) {
      out.total++;
      const kind = KINDS.find(([, km, re]) => km === m && re.test(p));
      if (kind) out[kind[0]]++;
    }
    return out;
  };
  const result = {
    whole: count(0),
    afterOpening: count(opened),
    streams,
  };
  console.log(JSON.stringify(result, null, 2));
  if (process.env.MEASURE_LOG)
    for (const [m, p] of state.requests) console.log(m, p);
  if (process.env.MEASURE_OUT)
    await writeFile(process.env.MEASURE_OUT, JSON.stringify(result, null, 2));
  if (errors.length) console.error("page errors:", errors);
} finally {
  await browser.close();
}
