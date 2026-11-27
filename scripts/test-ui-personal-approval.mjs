// Called by TestPersonalDeliveryMailCalendarDraftAndApprovedSend. Documents,
// jobs, approval and the SMTP submission travel through the live test server.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";

let input = "";
for await (const part of process.stdin) input += part;
const { origin, mailbox, job, cookies } = JSON.parse(input);
assert.equal(new URL(origin).hostname, "127.0.0.1");
const engine = process.env.UI_ENGINE || "chromium";
const phone = engine === "webkit";
const pw = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await pw[engine].launch();
try {
  const ctx = await browser.newContext({
    baseURL: origin,
    viewport: phone
      ? { width: 390, height: 844 }
      : { width: 1440, height: 920 },
    isMobile: phone,
    hasTouch: phone,
    serviceWorkers: "block",
    reducedMotion: "reduce",
    locale: "de-DE",
  });
  await ctx.addCookies(
    Object.entries(cookies).map(([name, value]) => ({
      name,
      value,
      url: origin,
      httpOnly: true,
      sameSite: "Strict",
    })),
  );
  await ctx.addInitScript(() => {
    localStorage.setItem("titanos.locale", "de");
  });
  const errors = [];
  const approvals = [];
  const pending = await ctx.request.get("/api/v1/personal-agent/jobs/" + job);
  assert.ok(pending.ok());
  const state = await pending.json();
  assert.equal(state.currentTool, "mail.send");
  assert.equal(state.request.tool, "mail.event");
  assert.equal(state.result.title, "Antwortvorschlag");
  assert.ok(state.result.text.includes("10 Uhr"));
  assert.equal(state.preview.data.text, "Geprüfte Antwortfassung zwei.");
  assert.notEqual(
    state.result.data.draft.fields.to[0],
    state.preview.data.to[0],
  );
  function monitor(page) {
    page.on("pageerror", (err) => errors.push(err.message));
    page.on("request", (r) => {
      if (
        r.method() === "POST" &&
        new URL(r.url()).pathname.endsWith("/" + job + "/approve")
      )
        approvals.push(r.postDataJSON()?.token);
    });
    page.setDefaultTimeout(20000);
    return page;
  }
  const app = (p, id) =>
    phone ? p : p.frameLocator('iframe[data-app="app:' + id + '"]').last();
  const agentPage = monitor(await ctx.newPage());
  await agentPage.goto("/agent?job=" + encodeURIComponent(job));
  const agent = app(agentPage, "agent");
  const agentDetail = agent.locator('.pa-detail[data-job="' + job + '"]');
  const mailPage = monitor(await ctx.newPage());
  await mailPage.goto(
    "/mail?" + new URLSearchParams({ a: mailbox, f: "INBOX", m: "1" }),
  );
  const mail = app(mailPage, "mail");
  const trigger = phone
    ? mail.locator(".reader-agent:visible")
    : mail.getByRole("button", { name: "Agent", exact: true });
  await trigger.first().click();
  const mailDetail = mail.locator(
    '.pim-agent:visible .pa-detail[data-job="' + job + '"]',
  );
  async function verifyPreview(detail) {
    const heading = detail.getByRole("heading", { level: 2 });
    await heading.waitFor();
    assert.equal(
      (await heading.innerText()).trim(),
      state.preview.title,
      "must show this approval version",
    );
    const fields = await detail.locator(".pa-mail-fields").innerText();
    for (const text of [
      "alice@example.org",
      "reviewed@example.test",
      "copy@example.test",
      "private@example.test",
      "Revidierter Termin",
      "Bestaetigung.txt",
      "12 B",
      createHash("sha256").update("revision two").digest("hex"),
    ]) {
      assert.ok(
        fields.includes(text),
        "Missing current preview field: " + text,
      );
    }
    assert.equal(
      (await detail.locator(".pa-readable").innerText()).trim(),
      state.preview.data.text,
    );
    assert.equal(
      await detail
        .getByText("Am 30. November um 10 Uhr passt es.", { exact: true })
        .count(),
      0,
    );
    assert.equal(
      await detail
        .getByRole("heading", { name: "Antwortvorschlag", exact: true })
        .count(),
      0,
    );
    assert.equal(
      await detail
        .locator("details")
        .getByText("Antwortvorschlag", { exact: true })
        .count(),
      1,
      "the saved step remains in the receipts",
    );
    assert.equal(await detail.locator(".pa-verified").count(), 0);
    assert.equal(
      await detail.locator("[data-guide=native]").count(),
      0,
      "previous draft offered as the approved object",
    );
    const gate = detail.locator("[data-guide=lock]");
    await gate
      .getByRole("button", { name: "Entwurf im Editor öffnen", exact: true })
      .waitFor();
    await gate.getByRole("button", { name: /senden/i }).waitFor();
  }
  await verifyPreview(agentDetail);
  await verifyPreview(mailDetail);
  assert.deepEqual(approvals, []);
  const chat = agent.locator('[data-guide="reply-' + job + '"]');
  assert.equal(await chat.locator(".pa-card-excerpt").count(), 0);
  assert.equal(
    await chat.getByText(state.result.text, { exact: true }).count(),
    0,
  );
  const confirming = phone ? mailDetail : agentDetail;
  await confirming.locator("[data-guide=lock] .pa-primary").click();
  await confirming.locator(".pa-verified").waitFor();
  assert.equal(
    (await confirming.locator(".pa-verified").innerText()).trim(),
    "Vom Mailserver angenommen",
  );
  assert.match(
    await confirming.locator(".pa-readable").innerText(),
    /SMTP.*angenommen/i,
  );
  assert.equal(await confirming.locator("[data-guide=lock]").count(), 0);
  const result = await (
    await ctx.request.get("/api/v1/personal-agent/jobs/" + job)
  ).json();
  assert.equal(result.status, "done");
  assert.equal(result.currentTool, "mail.send");
  assert.equal(result.result.data.accepted, true);
  assert.ok(result.result.data.messageId && result.result.data.acceptedAt);
  assert.deepEqual(approvals, [state.token]);
  assert.deepEqual(errors, []);
  console.log(
    engine +
      " verified the revised mail approval in both the thread and Agent, followed by one confirmed SMTP receipt",
  );
} finally {
  await browser.close();
}
