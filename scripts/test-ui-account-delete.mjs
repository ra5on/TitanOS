// 1.03: the delete dialog of an account names what goes with it of the
// KI-Agent (GET /api/v1/users/{name}/agent-data, A-42): the counts, the
// shared projects that stay with a member, the parts without numbers when
// the agent's volume is missing, and the dialog as before on a NAS without
// the route. Desktop width and a phone, German and English. Isolated API
// data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5201 node scripts/test-ui-account-delete.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5201";
const out = process.env.UI_OUTPUT || "out/validation/account-delete";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

async function openDelete({
  width = 1280,
  locale = "de",
  agentData,
  phone = false,
} = {}) {
  const made = await create(
    "owner",
    "graphit",
    width,
    locale,
    phone
      ? {
          isMobile: true,
          hasTouch: true,
          deviceScaleFactor: 3,
          viewport: { width, height: 844 },
        }
      : {},
  );
  if (agentData) made.data.agentData = agentData;
  const { page } = made;
  await page.goto(base + "/users");
  await page.locator('[data-account="sam"]').first().click();
  await page
    .getByRole("button", {
      name: locale === "de" ? "Löschen" : "Delete",
      exact: true,
    })
    .first()
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.waitFor();
  return { ...made, dialog };
}

// The counts: what goes, and the shared project that stays with a member.
for (const [width, phone] of [
  [1280, false],
  [390, true],
]) {
  const { ctx, page, dialog } = await openDelete({ width, phone });
  const list = dialog.locator("[data-agent-data]");
  await list.waitFor();
  assert.deepEqual(
    (await list.locator("li").allTextContents()).map((s) => s.trim()),
    [
      "Chats mit ihren Dateien: 12",
      "Eigene Projekte: 2",
      "Einträge im Gedächtnis: 7",
      "Geplante Aufträge: 2",
      "Notizen: 4",
      "Der Ordner des Kontos in der Freigabe „KI-Agent“",
    ],
  );
  await dialog
    .getByText(
      "Geteilte Projekte gehen an ein Mitglied und bleiben erhalten: 1",
    )
    .waitFor();
  // Skills: none, so no line for them.
  assert.equal(await list.getByText("Skills").count(), 0);
  // Nothing cut off, nothing beside the screen; the danger button is left of Cancel.
  assert.deepEqual(await page.evaluate(cutTextIn), [], "no cut text");
  const box = await dialog.boundingBox();
  assert.ok(
    box.x >= 0 && box.x + box.width <= width + 0.5,
    "dialog on the screen",
  );
  const danger = await dialog.locator(".btn-danger").boundingBox();
  const cancel = await dialog
    .getByRole("button", { name: "Abbrechen" })
    .boundingBox();
  assert.ok(
    danger.x < cancel.x || danger.y > cancel.y,
    "danger left of Cancel",
  );
  await page.screenshot({ path: `${out}/konto-loeschen-${width}.png` });
  await ctx.close();
}

// Without the agent's volume: the parts without numbers.
{
  const { ctx, page, dialog } = await openDelete({
    locale: "en",
    agentData: { sam: { available: false, chats: 0 } },
  });
  const list = dialog.locator("[data-agent-data]");
  await list.waitFor();
  const lines = (await list.locator("li").allTextContents()).map((s) =>
    s.trim(),
  );
  assert.ok(lines.includes("Chats with their files"), lines.join(" | "));
  assert.ok(lines.includes("Scheduled tasks"), lines.join(" | "));
  assert.ok(!/\d/.test(lines.join(" ")), "no numbers without the volume");
  await dialog.getByText(/volume is not mounted right now/).waitFor();
  await page.screenshot({ path: `${out}/account-delete-no-volume.png` });
  await ctx.close();
}

// A NAS without the route (404): the dialog as before, still able to delete.
{
  const { ctx, page, data, dialog } = await openDelete({ agentData: {} });
  await dialog.getByText("Das Konto, seine Einstellungen").waitFor();
  await page.waitForTimeout(400);
  assert.equal(await dialog.locator("[data-agent-data]").count(), 0);
  assert.equal(await dialog.getByRole("status").count(), 0, "no endless check");
  assert.ok(
    data.requests.some(
      ([m, p]) => m === "GET" && p === "/users/sam/agent-data",
    ),
    "asked the NAS",
  );
  assert.ok(await dialog.locator(".btn-danger").isEnabled());
  await ctx.close();
}

await browser.close();
const relevant = errors.filter(
  (e) => !/Failed to load resource|net::ERR/.test(e),
);
assert.deepEqual(relevant, [], "no script errors");
console.log(
  "PASS account delete names the agent's data (counts, no volume, older NAS)",
);
