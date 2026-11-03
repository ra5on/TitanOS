// Browser regression with isolated API fixtures only; never contacts a NAS.
// Since 0.43 a template installs on its own page ("Apps › <App> installieren",
// one column): every Compose service has a "Repository (<service>)" line with
// the full image reference; the services after the first show in "Erweitert".
// The edit page picks the service with a radio and has one "Repository" line.
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
const digest = `worker:stable@sha256:${"a".repeat(64)}`;
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
    image: ${digest}
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
    failed = false,
    cachePercent = 20;
  const puts = [];
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
              services: 3,
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
          cpuMissing: [],
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
        puts.push(stored);
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
          apps: installed ? ["photos", "fixture-stack"] : ["photos"],
          containers: [],
          jobs: {
            "fixture-stack": {
              state: failed ? "failed" : "done",
              output: failed ? "manifest unknown: fixture tag" : "",
            },
          },
        },
      });
    if (path === "/system/memory") {
      if (method === "PUT")
        cachePercent = route.request().postDataJSON().cachePercent;
      return route.fulfill({
        json: {
          settings: { cachePercent },
          readOnly: false,
          budgetBytes: (16e9 * cachePercent) / 100,
          reserveBytes: 1.6e9,
          inFlightBytes: 0,
          memory: data.system.memory,
          paused: false,
        },
      });
    }
    return route.fallback();
  });

  // RAM tile: available memory, the occupancy bar (occupied, write cache,
  // free; the read cache counts as free) and no warning while plenty is free.
  await page.goto(base + "/");
  const ram = page.locator('[gs-id="memory"]');
  await ram.getByText(/Verfügbar:/).waitFor();
  assert.equal(await ram.locator(".notice-warn").count(), 0);
  const bar = ram.getByRole("img", { name: /^Belegt / });
  await bar.waitFor();
  assert.match(
    await bar.getAttribute("aria-label"),
    /^Belegt 4(,0)? GB, Schreibcache 0 B, frei 12(,0)? GB$/,
  );
  await page.screenshot({ path: out + "/ram-cache.png", fullPage: true });
  data.system.memory.lowAvailable = true;
  data.system.memory.availableBytes = 1e9;
  await ram.getByRole("status").waitFor();
  assert.match(await ram.getByRole("status").innerText(), /30 Sekunden/);

  // Cache budget: without PSI the pressure reads "Nicht verfügbar"; the
  // chosen share is sent and applied.
  delete data.system.memory.pressure;
  await page.goto(base + "/settings/memory");
  await page.getByText("Nicht verfügbar", { exact: true }).waitFor();
  assert.match(
    await page.getByRole("status").first().innerText(),
    /30 Sekunden/,
  );
  assert.equal(await page.locator("#cache-budget").inputValue(), "20");
  await page.locator("#cache-budget").selectOption("30");
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  await page
    .getByText("Gespeichert und angewendet.", { exact: true })
    .waitFor();
  assert.equal(cachePercent, 30);
  await page.getByText("4,8 GB", { exact: true }).waitFor();

  // Install page: one repository line per service, each tag on its own.
  await page.goto(base + "/apps");
  await page
    .getByRole("link", { name: /Fixture Stack/ })
    .first()
    .click();
  await page.waitForURL(/\/apps\/install\/fixture\/fixture-stack$/);
  await page
    .getByRole("heading", { name: "Fixture Stack installieren" })
    .waitFor();
  const repo = (service) =>
    // The label ends in a CSS colon, which is part of the accessible name.
    page.getByRole("textbox", {
      name: new RegExp(`^Repository \\(${service}\\):?$`),
    });
  assert.equal(await repo("web").inputValue(), "registry.test:5000/web");
  assert.equal(await repo("db").count(), 0, "basic view: main service only");
  await page.getByRole("switch", { name: "Erweitert" }).click();
  assert.equal(await repo("db").inputValue(), "postgres:17");
  // A digest stays whole and valid; it is never split into a tag.
  assert.equal(await repo("worker").inputValue(), digest);
  assert.equal(await repo("worker").getAttribute("aria-invalid"), "false");
  // An invalid tag blocks the install until it is fixed.
  await repo("web").fill("registry.test:5000/web:bad tag");
  assert.equal(await repo("web").getAttribute("aria-invalid"), "true");
  assert.ok(
    await page
      .getByRole("button", { name: "Installieren", exact: true })
      .isDisabled(),
  );
  await repo("web").fill("registry.test:5000/web:v2");
  await repo("db").fill("postgres:16");
  await page
    .getByRole("button", { name: "Als Compose-Datei bearbeiten", exact: true })
    .click();
  const yaml = page.getByRole("textbox", {
    name: "Als Compose bearbeiten",
    exact: true,
  });
  let text = await yaml.inputValue();
  assert.ok(text.includes("registry.test:5000/web:v2"));
  assert.ok(text.includes("postgres:16"));
  assert.ok(text.includes(digest));
  await yaml.fill(text.replace("web:v2", "web:v3"));
  await page
    .getByRole("button", { name: "Zurück zum Formular", exact: true })
    .click();
  assert.equal(await repo("web").inputValue(), "registry.test:5000/web:v3");
  assert.equal(await repo("db").inputValue(), "postgres:16");
  assert.equal(await repo("worker").inputValue(), digest);
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
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole("button", { name: "Installieren", exact: true }).click();
  await page
    .getByText("Der Befehl wurde erfolgreich ausgeführt.")
    .waitFor({ timeout: 15000 });
  assert.equal(installed.name, "fixture-stack");
  assert.ok(installed.compose.includes("registry.test:5000/web:v3"));
  assert.ok(installed.compose.includes("postgres:16"));
  assert.ok(installed.compose.includes(digest));
  assert.ok(
    installed.env.PASSWORD &&
      !installed.compose.includes(installed.env.PASSWORD),
  );

  // Edit page: each service keeps its own image while switching services.
  await page.goto(base + "/docker/apps/fixture-stack");
  await page
    .getByRole("heading", { name: "fixture-stack bearbeiten" })
    .waitFor();
  const image = page.getByRole("textbox", { name: /^Repository:?$/ });
  await page.getByRole("radio", { name: "db", exact: true }).click();
  assert.equal(await image.inputValue(), "postgres:16");
  await image.fill("postgres:15");
  await page.getByRole("radio", { name: "web", exact: true }).click();
  assert.equal(await image.inputValue(), "registry.test:5000/web:v3");
  await page.getByRole("radio", { name: "worker", exact: true }).click();
  assert.equal(await image.inputValue(), digest);
  await page.getByRole("radio", { name: "db", exact: true }).click();
  assert.equal(await image.inputValue(), "postgres:15");
  failed = true;
  await page.getByRole("button", { name: "Übernehmen", exact: true }).click();
  await page
    .getByText("manifest unknown: fixture tag", { exact: true })
    .waitFor();
  assert.match(
    await page.locator(".result").innerText(),
    /Das Übernehmen ist fehlgeschlagen:/,
  );
  assert.equal(puts.length, 1);
  assert.ok(
    stored.includes("postgres:15") &&
      stored.includes("registry.test:5000/web:v3") &&
      stored.includes(digest),
  );
  // The failed change stays in the form, ready to fix or apply again.
  assert.equal(await image.inputValue(), "postgres:15");
  await page.getByRole("radio", { name: "web", exact: true }).click();
  assert.equal(await image.inputValue(), "registry.test:5000/web:v3");
  assert.ok(
    await page
      .getByRole("button", { name: "Übernehmen", exact: true })
      .isEnabled(),
  );
  await ctx.close();
  assert.deepEqual(errors, []);
  console.log(
    "RAM/cache, PSI fallback, cache budget, per-service images, YAML/secret roundtrip, editor, pull failure and mobile layout passed",
  );
} finally {
  await browser.close();
}
