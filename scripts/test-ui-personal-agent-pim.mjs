// Native T-Mail entry points with synthetic account and job APIs. SMTP/IMAP,
// backend authorization and the durable worker are exercised by the Go suite.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
import { installMail, mailState } from "./test-ui-mail-fixtures.mjs";
const engine = process.env.UI_ENGINE || "chromium";
const pw = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await pw[engine].launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5212";
const out = process.env.UI_OUTPUT || "out/validation/personal-agent";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const caps = [
  "mail.summarize",
  "mail.reply",
  "mail.event",
  "mail.send",
  "mail.followup",
].map((id) => ({
  id,
  title: id,
  app: "mail",
  effective: "ask",
  ceiling: "ask",
  sensitive: id === "mail.send",
}));
const settings = {
  theme: "system",
  colors: {},
  rights: {},
  dotSize: 9,
  motion: "subtle",
  paused: false,
};
try {
  for (const phone of [false, true]) {
    const width = phone ? 390 : 1440,
      height = phone ? 844 : 900;
    const { ctx, page } = await create("user", "dunkel", width, "de", {
      viewport: { width, height },
      isMobile: phone,
      hasTouch: phone,
      reducedMotion: "reduce",
      serviceWorkers: "block",
    });
    const data = mailState();
    await installMail(ctx, data);
    let request, sendRequest, saved, revision;
    const requests = [];
    const jobs = [];
    page.on("request", (req) => {
      if (req.url().includes("/personal-agent/state"))
        requests.push("browser " + req.url());
    });
    page.on("requestfailed", (req) => {
      if (req.url().includes("/personal-agent/state"))
        requests.push("failure " + req.failure()?.errorText);
    });
    await ctx.route("**/api/v1/**", async (route) => {
      const u = new URL(route.request().url()),
        p = u.pathname.slice(7),
        method = route.request().method();
      const json = (body) => route.fulfill({ json: body });
      if (p === "/services/state" || p.startsWith("/personal-agent"))
        requests.push(method + " " + p);
      if (p === "/services/state")
        return json({
          agent: true,
          mail: true,
          calendar: true,
          contacts: true,
        });
      if (p === "/personal-agent/presentation") return json(settings);
      if (p === "/personal-agent/state")
        return json({ settings, capabilities: caps, jobs, chats: [] });
      if (p === "/personal-agent/jobs" && method === "POST") {
        const input = route.request().postDataJSON(),
          anchor = input.sources[0];
        const id = "pim-" + String(jobs.length + 1),
          time = new Date().toISOString();
        const j = {
          id,
          request: { ...input, chatId: "thread-fixture" },
          created: time,
          updated: time,
          seen: false,
          status: "done",
          phase: "complete",
        };
        if (input.tool === "mail.send") {
          sendRequest = input;
          j.status = "waiting";
          j.phase = "approval";
          j.token = "fixture-approval";
          j.preview = {
            title: "Antwort senden",
            text: saved.text,
            sources: [anchor],
            data: {
              account: anchor.ref.account,
              from: "alex@gmx.example",
              to: saved.to,
              cc: saved.cc,
              bcc: saved.bcc,
              subject: saved.subject,
              text: saved.text,
              inReplyTo: anchor.ref.messageId,
              draftUid: saved.uid,
              draftMessageId: saved.messageId,
              draftDate: saved.date,
              attachments: [],
            },
          };
        } else {
          request = input;
          j.result = {
            title: "Antwortvorschlag",
            text: "Vielen Dank für den Auszug.",
            sources: input.sources,
            data: {
              draft: {
                app: "mail",
                kind: "reply",
                fields: {
                  a: anchor.ref.account,
                  f: anchor.ref.folder,
                  uid: Number(anchor.ref.uid),
                  messageId: anchor.ref.messageId,
                  date: anchor.ref.date,
                  to: ["service@bank.example"],
                  subject: "Re: " + anchor.label,
                  text: "Vielen Dank für den Auszug.",
                },
              },
            },
          };
        }
        jobs.unshift(j);
        return json(j);
      }
      if (p.endsWith("/revise")) {
        revision = route.request().postDataJSON();
        const j = jobs.find((j) => j.id === p.split("/").at(-2));
        assert.equal(revision.token, j.token);
        j.phase = "queued";
        j.status = "planned";
        delete j.token;
        return json(j);
      }
      if (p.endsWith("/seen"))
        return json(jobs.find((j) => j.id === p.split("/").at(-2)));
      if (p === "/personal-agent/skills") return json({ skills: [] });
      if (p === "/personal-agent/memory") return json({ items: [] });
      if (p === "/personal-agent/model")
        return json({ cloud: "", local: "", keySet: false });
      if (p.startsWith("/personal-agent/jobs/"))
        return json(jobs.find((j) => j.id === p.split("/").at(-1)));
      if (p.endsWith("/draft") && method === "POST") {
        // The IMAP fixture for this UI test returns an immutable UID; no
        // message is sent. Backend integration checks the actual MIME.
        const raw = route.request().postDataBuffer().toString("utf8");
        const metadata =
          /name="meta"[^\n]*\r?\n(?:[^\r\n]+\r?\n)*?\r?\n([^\r\n]+)/.exec(raw);
        assert.ok(metadata, "native draft metadata");
        const meta = JSON.parse(metadata[1]);
        const uid = (saved?.uid || 9300) + 1;
        saved = {
          ...meta,
          uid,
          messageId: "<draft-" + uid + "@fixture.example>",
          date: "2026-11-26T10:45:00Z",
          attachments: [],
        };
        return json({ uid, attachments: [] });
      }
      if (
        p.endsWith("/message") &&
        saved &&
        Number(u.searchParams.get("uid")) === saved.uid
      )
        return json(saved);
      return route.fallback();
    });
    async function open(uid) {
      await page.goto(base + "/mail?a=acc-privat&f=INBOX&m=" + uid);
      const trigger = page
        .locator(phone ? ".reader-agent" : "button")
        .filter({ visible: true });
      if (phone && (await trigger.count())) await trigger.first().click();
      else {
        const target = page
          .getByRole("button", { name: "Agent", exact: true })
          .first();
        try {
          await target.click();
        } catch (e) {
          console.error(
            "Native reader:",
            page.url(),
            await page.getByRole("heading").allTextContents(),
            await page.getByRole("button").allTextContents(),
            errors.slice(-6),
          );
          throw e;
        }
      }
      const panel = page.locator(".pim-agent:visible");
      await panel.getByText("Diese Unterhaltung").waitFor();
      return panel;
    }
    const panel = await open(1000);
    const quick = [
      "E-Mail zusammenfassen",
      "Antwort vorbereiten",
      "Termin erkennen",
    ];
    for (const label of quick) {
      try {
        await panel.getByRole("button", { name: label, exact: true }).waitFor();
      } catch (e) {
        console.error(
          "Captured routes",
          requests,
          "visible panel",
          await panel.innerText(),
          errors.slice(-6),
        );
        throw e;
      }
    }
    await panel.getByRole("button", { name: quick[1] }).click();
    const pdf = panel.locator("fieldset input[type=checkbox]").first();
    assert.equal(await pdf.isChecked(), false);
    await pdf.check();
    assert.equal(
      await panel.evaluate((e) => e.scrollWidth <= e.clientWidth + 1),
      true,
    );
    await page.screenshot({
      path: out + "/" + engine + "-" + width + "-pim-auswahl.png",
    });
    await panel.getByRole("button", { name: "Auftrag starten" }).click();
    await panel
      .getByText("Vielen Dank für den Auszug.", { exact: true })
      .first()
      .waitFor();
    assert.equal(request.tool, "mail.reply");
    assert.equal(request.sources[0].ref.uid, "1000");
    assert.equal(request.sources[0].ref.scope, "thread");
    assert.equal(request.sources[1].kind, "mail.pdf");
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
      true,
    );
    const bounds = await panel.boundingBox();
    assert.ok(bounds);
    if (!phone) assert.ok(bounds.x > width / 2);
    await page.screenshot({
      path: out + "/" + engine + "-" + width + "-pim.png",
    });
    const old = jobs[0].id;
    const native = panel.locator("[data-guide=native]");
    await native.click();
    await page.waitForURL("**/mail/compose?personalDraft=" + old);
    await page.locator("#mail-text").fill("Geänderte Antwort aus dem Editor.");
    await page.getByRole("button", { name: "Prüfen und freigeben" }).click();
    await page.waitForURL("**/agent?job=*");
    assert.equal(sendRequest.args.draftUid, 9301);
    assert.equal(sendRequest.sources[0].ref.uid, "1000");
    assert.equal(data.sent.length, 0);
    assert.equal(saved.text, "Geänderte Antwort aus dem Editor.");
    const pending = jobs[0];
    await page.goto(base + "/mail/compose?personalReview=" + pending.id);
    await page.locator("#mail-text").fill("Noch einmal geprüfte Antwort.");
    await page.getByRole("button", { name: "Prüfen und freigeben" }).click();
    await page.waitForURL("**/agent?job=" + pending.id);
    assert.equal(saved.text, "Noch einmal geprüfte Antwort.");
    assert.equal(revision.args.draftUid, 9302);
    assert.equal(jobs.length, 2, "revision created a second send");
    const next = await open(999);
    assert.equal(await next.locator(".pa-detail").count(), 0);
    assert.equal(await next.locator("fieldset input:checked").count(), 0);
    assert.equal(jobs.find((j) => j.id === old).status, "done");
    await ctx.close();
    console.log(
      engine,
      phone ? "mobile" : "desktop",
      "selected sources, editable reply, approval handoff and thread change verified",
    );
  }
} finally {
  await browser.close();
}
