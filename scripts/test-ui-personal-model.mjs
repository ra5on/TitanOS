// Called by TestPersonalModelReadinessThroughAccountSettings with two real web sessions.
import assert from "node:assert/strict";
let input = "";
for await (const part of process.stdin) input += part;
const { origin, password } = JSON.parse(input);
assert.equal(new URL(origin).hostname, "127.0.0.1");
const engine = process.env.UI_ENGINE || "webkit";
const phone = engine === "webkit";
const pw = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await pw[engine].launch();
try {
  const ctx = await browser.newContext({
    baseURL: origin,
    locale: "de-DE",
    isMobile: phone,
    hasTouch: phone,
    viewport: phone
      ? { width: 390, height: 844 }
      : { width: 1440, height: 920 },
    serviceWorkers: "block",
  });
  await ctx.addInitScript((appOrigin) => {
    if (location.origin === appOrigin)
      localStorage.setItem("titanos.locale", "de");
  }, origin);
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const agent = phone
    ? page
    : page.frameLocator('iframe[data-app="app:agent"]').last();
  const model = agent.locator('details[data-section="model"]');
  const settings = async () => {
    await agent.locator(".pa-model-status").getByRole("button").click();
    await model.locator("input[list]").waitFor();
    assert.equal(await model.evaluate((e) => e.open), true);
  };
  async function account(login, name) {
    const loggedIn = await ctx.request.post("/api/v1/login", {
      data: { name: login, password },
      headers: { Origin: origin },
    });
    assert.equal(loggedIn.status(), 200);
    await page.goto(origin + "/agent");
    const note = agent.locator(".pa-model-status");
    await note
      .getByText(
        "Das ausgewählte Modell " + name + " ist lokal nicht verfügbar.",
        { exact: true },
      )
      .waitFor();
    await agent.locator(".pa-compose textarea").fill("Frage im Test");
    assert.equal(await agent.locator(".pa-send").isDisabled(), true);
  }
  await account("alex", "alex-only");
  await settings();
  await model.getByRole("button", { name: "Lokale Modelle suchen" }).click();
  await model
    .getByText("Ein lokales Modell gefunden", { exact: true })
    .waitFor();
  await model.locator("input[list]").fill("fixture:latest");
  await model.getByRole("button", { name: "Speichern", exact: true }).click();
  await model
    .getByText("Lokales Modell fixture:latest ist bereit.", { exact: true })
    .waitFor();
  if (process.env.UI_MODEL_SCREENSHOT)
    await page.screenshot({ path: process.env.UI_MODEL_SCREENSHOT });
  await agent.locator('[data-guide="chat-tab"]:visible').click();
  assert.equal(
    await agent.locator(".pa-compose textarea").inputValue(),
    "Frage im Test",
  );
  assert.equal(await agent.locator(".pa-send").isEnabled(), true);
  await page.goto("about:blank");
  const logout = await ctx.request.post("/api/v1/logout", {
    headers: { Origin: origin },
  });
  assert.equal(logout.status(), 204);
  assert.equal(
    (await ctx.request.get("/api/v1/personal-agent/model/readiness")).status(),
    401,
  );
  await account("bea", "bea-only");
  await settings();
  assert.equal(await model.locator("input[list]").inputValue(), "bea-only");
  assert.deepEqual(errors, []);
  console.log(
    engine +
      ": missing model → settings → scan and save → chat ready; second account retains its own missing model",
  );
} finally {
  await browser.close();
}
