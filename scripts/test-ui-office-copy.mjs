import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";

// Exercise the same source chips, previews and output links used after a native handoff.
export async function checkOfficeCopy({
  id,
  page,
  agent,
  ctx,
  root,
  sources,
  engine,
  screenshot,
}) {
  const word = id === "t-word";
  const filename = word ? "Überarbeitet.docx" : "Überarbeitet.xlsx";
  const region = word ? "Absatz 2" : "Planung · B2:D7";
  const replacement = word
    ? "Der Termin wurde bestätigt."
    : "21\t22\t23\n31\t32\t33\n41\t42\t43\n51\t52\t53\n61\t62\t63\n71\t72\t73";
  const expectedBefore = word
    ? "Zweiter Absatz"
    : "2\t20\t40\n3\t5\t15\n1\t9\t9\n4\t4\t16\n5\t2\t10\n2\t7\t14";
  const context = agent.locator(".pa-context");
  if (word) {
    // Keep one of the two selected paragraphs as the explicit write scope.
    await context
      .getByRole("button", { name: /Entfernen .*Absatz 3$/ })
      .click();
  }
  await context.locator("select").first().selectOption("office.copy");
  await context.getByLabel("Neuer Inhalt").fill(replacement);
  if (!word) await context.getByLabel("Zelltyp").selectOption("number");
  await context.getByLabel("Name der neuen Datei").fill(filename);
  await context.getByRole("button", { name: "Schließen", exact: true }).click();
  await context.waitFor({ state: "hidden" });
  const [creation] = await Promise.all([
    page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname === "/api/v1/personal-agent/jobs",
    ),
    agent.locator(".pa-compose [type=submit]").click(),
  ]);
  assert.equal(creation.status(), 200);
  const job = await creation.json();
  const jobURL = "/api/v1/personal-agent/jobs/" + encodeURIComponent(job.id);
  async function until(status, phase) {
    for (let i = 0; i < 120; i++) {
      const res = await ctx.request.get(jobURL);
      assert.ok(res.ok());
      const current = await res.json();
      assert.notEqual(current.status, "failed", JSON.stringify(current.error));
      if (current.status === status && (!phase || current.phase === phase))
        return current;
      await delay(250);
    }
    assert.fail("Job did not reach " + status + " " + (phase || ""));
  }
  await until("waiting", "permission");
  const detail = agent.locator('.pa-detail[data-job="' + job.id + '"]');
  const card = agent.locator(
    '[data-guide="reply-' + job.id + '"] .pa-card-open',
  );
  if (!(await detail.isVisible())) await card.click();
  const approve = detail.locator(".pa-approval button.pa-primary");
  await approve.click();
  const waiting = await until("waiting", "approval");
  assert.deepEqual(waiting.request.sources, [sources[0]]);
  const diff = detail.locator(".pa-diff");
  await diff.waitFor();
  assert.equal(await diff.locator("h3").textContent(), region);
  const shown = await diff.locator("pre").allTextContents();
  assert.equal(shown[0], expectedBefore);
  assert.equal(shown[1], replacement);
  const output = new URLSearchParams({ root, path: filename });
  const get = "/api/v1/files/download?" + output;
  assert.equal((await ctx.request.get(get)).status(), 404);
  await screenshot(detail, engine, id, "kopie-vor-freigabe");
  await approve.click();
  const done = await until("done");
  assert.deepEqual(done.result.data.document, {
    app: id,
    root,
    path: filename,
  });
  const result = detail.locator('[data-guide="native"]');
  await result.waitFor();
  await detail.locator(".pa-diff.pa-highlight").waitFor();
  await untilSeen();
  // Reopen the completed result to check that the highlight only appears once.
  const close = agent
    .getByRole("button", { name: "Schließen", exact: true })
    .last();
  await close.click();
  await card.click();
  await diff.waitFor();
  assert.equal(await detail.locator(".pa-highlight").count(), 0);
  await screenshot(detail, engine, id, "kopie-gespeichert");
  const [opened] = await Promise.all([
    page.waitForResponse((r) => {
      const u = new URL(r.url());
      return (
        u.pathname === "/api/v1/files/download" &&
        u.searchParams.get("root") === root &&
        u.searchParams.get("path") === filename
      );
    }),
    result.click(),
  ]);
  assert.ok(opened.ok());
  const canvas = page
    .locator('iframe[data-webapp="' + id + '"]:visible')
    .last()
    .contentFrame()
    .locator("canvas");
  await canvas.waitFor({ state: "visible" });
  console.log(
    engine,
    id,
    region,
    "→ preview, approved copy reopened in native editor, first visit persisted",
  );

  async function untilSeen() {
    for (let i = 0; i < 120; i++) {
      const response = await ctx.request.get(jobURL);
      assert.ok(response.ok());
      if ((await response.json()).seen) return;
      await delay(250);
    }
    assert.fail("Completed result was never marked as seen");
  }
}
