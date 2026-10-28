// Browser regression with isolated API fixtures only; never contacts a NAS.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5194";
const out = process.env.UI_OUTPUT || "out/validation/ui-memory-images";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const compose = `x-titanos:
  name: Fixture Stack
  variables:
    PASSWORD: {secret: generate}
services:
  web:
    image: registry.test:5000/web
    environment: {PASSWORD: ''}
    depends_on: [db]
  db:
    image: postgres:17
  worker:
    image: worker:stable@sha256:${"a".repeat(64)}
`;
try {
  const { ctx, page, data } = await create();
  data.system.version = "0.31.0";
  data.system.memory = {
    totalBytes: 16e9,
    usedBytes: 15e9,
    freeBytes: 1e9,
    availableBytes: 12e9,
    cacheBytes: 12e9,
    reserveBytes: 1.6e9,
    pressure: { someAvg10: 0, fullAvg10: 0 },
  };
  let stored = compose,
    installed = null,
    failed = false;
  await ctx.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.slice(
        "/api/v1".length,
      ),
      method = route.request().method();
    if (path === "/apps")
      return route.fulfill({
        json: {
          sources: [{ id: "fixture", name: "Fixture Store", count: 1 }],
          apps: [
            {
              id: "fixture-stack",
              name: "Fixture Stack",
              source: "fixture",
              image: "registry.test:5000/web",
              description: { de: "Testvorlage", en: "Test template" },
              category: "development",
            },
          ],
        },
      });
    if (path === "/apps/sources/fixture/apps/fixture-stack")
      return route.fulfill({
        json: {
          compose,
          timezone: "Europe/Berlin",
          uid: 1000,
          gid: 100,
          appdata: "/mnt/Data/appdata",
          missingDevices: [],
          listening: [],
        },
      });
    if (path === "/docker/apps" && method === "POST") {
      installed = route.request().postDataJSON();
      stored = installed.compose;
      return route.fulfill({ status: 201 });
    }
    if (path === "/docker/apps/fixture-stack") {
      if (method === "PUT") {
        stored = route.request().postDataJSON().compose;
        return route.fulfill({ status: 202 });
      }
      return route.fulfill({ json: { compose: stored } });
    }
    if (path === "/docker")
      return route.fulfill({
        json: {
          settings: { enabled: true, volume: "Data" },
          running: true,
          version: "test",
          apps: ["photos"],
          containers: [],
          jobs: {
            "fixture-stack": {
              state: failed ? "failed" : "done",
              output: failed ? "manifest unknown: fixture tag" : "",
            },
          },
        },
      });
    if (path === "/system/memory")
      return route.fulfill({
        json: {
          settings: { cachePercent: 20 },
          readOnly: false,
          budgetBytes: 2e9,
          reserveBytes: 1.6e9,
          inFlightBytes: 0,
          memory: data.system.memory,
          paused: false,
        },
      });
    return route.fallback();
  });
  await page.goto(base + "/");
  const ram = page.locator('[gs-id="memory"]');
  await ram.getByText(/Verfügbar:/).waitFor();
  assert.equal(await ram.locator(".notice-warn").count(), 0);
  await ram.getByText("Übrige Belegung", { exact: true }).waitFor();
  await page.screenshot({ path: out + "/ram-cache.png", fullPage: true });
  data.system.memory.lowAvailable = true;
  data.system.memory.availableBytes = 1e9;
  await ram.getByRole("status").waitFor();
  assert.match(await ram.getByRole("status").innerText(), /30 Sekunden/);
  delete data.system.memory.pressure;
  await page.goto(base + "/settings/memory");
  await page.getByText("Nicht verfügbar", { exact: true }).waitFor();
  await page.locator("#cache-budget").selectOption("30");
  await page.goto(base + "/apps");
  await page.getByRole("button", { name: /Fixture Stack/ }).click();
  const panel = page.getByRole("complementary");
  const web = panel.getByRole("group", { name: "web", exact: true });
  const db = panel.getByRole("group", { name: "db", exact: true });
  const worker = panel.getByRole("group", { name: "worker", exact: true });
  assert.equal(
    await web.getByLabel("Image-Tag", { exact: true }).inputValue(),
    "latest",
  );
  assert.equal(
    await db.getByLabel("Image-Tag", { exact: true }).inputValue(),
    "17",
  );
  assert.equal(
    await worker.getByLabel("Image-Tag", { exact: true }).isDisabled(),
    true,
  );
  await web.getByLabel("Image-Tag", { exact: true }).fill("v2");
  await db.getByLabel("Image-Tag", { exact: true }).fill("16");
  await panel
    .getByRole("button", { name: "Als Compose bearbeiten", exact: true })
    .click();
  const yaml = panel.getByRole("textbox", {
    name: "Als Compose bearbeiten",
    exact: true,
  });
  let text = await yaml.inputValue();
  assert.ok(text.includes("web:v2"));
  assert.ok(text.includes("postgres:16"));
  await yaml.fill(text.replace("web:v2", "web:v3"));
  await panel
    .getByRole("button", { name: "Zurück zum Formular", exact: true })
    .click();
  assert.equal(
    await web.getByLabel("Image-Tag", { exact: true }).inputValue(),
    "v3",
  );
  assert.equal(
    await db.getByLabel("Image-Tag", { exact: true }).inputValue(),
    "16",
  );
  await page.screenshot({ path: out + "/stack-images.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: out + "/stack-images-mobile.png",
    fullPage: true,
  });
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  await panel
    .getByRole("button", { name: "Installieren", exact: true })
    .click();
  await page.waitForURL(base + "/docker");
  assert.ok(installed.compose.includes("web:v3"));
  assert.ok(
    installed.env.PASSWORD &&
      !installed.compose.includes(installed.env.PASSWORD),
  );
  await page.goto(base + "/docker/apps/fixture-stack");
  await page.getByRole("radio", { name: "db", exact: true }).click();
  assert.equal(
    await page.getByLabel("Image-Tag", { exact: true }).inputValue(),
    "16",
  );
  await page.getByLabel("Image-Tag", { exact: true }).fill("15");
  await page.getByRole("radio", { name: "web", exact: true }).click();
  assert.equal(
    await page.getByLabel("Image-Tag", { exact: true }).inputValue(),
    "v3",
  );
  failed = true;
  await page.getByRole("button", { name: "Übernehmen", exact: true }).click();
  await page
    .getByText("manifest unknown: fixture tag", { exact: true })
    .waitFor();
  assert.ok(stored.includes("postgres:15") && stored.includes("web:v3"));
  await ctx.close();
  assert.deepEqual(errors, []);
  console.log(
    "RAM/cache, PSI fallback, independent stack tags, YAML/secret roundtrip, editor, pull failure and mobile layout passed",
  );
} finally {
  await browser.close();
}
