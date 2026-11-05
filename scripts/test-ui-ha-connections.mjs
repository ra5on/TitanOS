// Several Home Assistant connections (PLAN §45, owner decision 5 Nov 2026),
// Settings › Home Assistant against isolated fixtures: the list on top,
// adding one with a name, the sections below working on the chosen one
// (?connection=), removing it with a plain confirmation.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5173 node scripts/test-ui-ha-connections.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5173";
const out = process.env.UI_OUTPUT || "out/validation/ha-connections";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const mqtt = { enabled: false, host: "", port: 1883, username: "", tls: false, prefix: "homeassistant", groups: [], commands: [] };

try {
  for (const [name, width] of [["1440", 1440], ["390", 390]]) {
    const { ctx, page } = await create("owner", "graphit", width);
    const settings = {
      main: { name: "", enabled: true, url: "http://192.168.10.20:8123", fingerprint: "", account: "", domains: [], entities: [], policies: {}, mqtt },
    };
    const calls = [];
    const list = () => Object.entries(settings).map(([id, s]) => ({ id, name: s.name, url: s.url, enabled: s.enabled, active: s.enabled && !!s.url, mqtt: false }));
    await page.route("**/api/v1/homeassistant/connections**", (r) => {
      const req = r.request();
      const path = new URL(req.url()).pathname;
      calls.push([req.method(), path]);
      if (req.method() === "POST") {
        settings.ab12cd34 = { name: req.postDataJSON().name, enabled: false, url: "", fingerprint: "", account: "", domains: [], entities: [], policies: {}, mqtt };
        return r.fulfill({ status: 201, json: { id: "ab12cd34", connections: list() } });
      }
      if (req.method() === "DELETE") delete settings[path.split("/").pop()];
      return r.fulfill({ json: { connections: list(), max: 8 } });
    });
    await page.route("**/api/v1/homeassistant/settings**", (r) => {
      const req = r.request();
      const id = new URL(req.url()).searchParams.get("connection") || "main";
      calls.push([req.method(), "settings", id]);
      if (req.method() === "PUT") settings[id] = { ...req.postDataJSON(), clearSecrets: undefined };
      return r.fulfill({ json: { settings: settings[id], tokenSet: id === "main", mqttPasswordSet: false, domains: [], sensorGroups: [], commandGroups: [], mqttDevices: [], status: { watching: id === "main", watchError: "", mqtt: false, mqttError: "" }, diagnostics: null } });
    });
    await page.route("**/api/v1/homeassistant/storage**", (r) => {
      calls.push(["GET", "storage", new URL(r.request().url()).searchParams.get("connection") || "main"]);
      return r.fulfill({ json: { account: "", server: "", entries: [], volumes: [], shares: [], homeAssistant: true } });
    });
    await page.goto(base + "/settings/homeassistant");
    const conns = page.getByRole("region", { name: "Verbindungen" });
    await conns.getByRole("button", { name: /Erste Verbindung/ }).waitFor();
    assert.equal(await conns.getByRole("button", { name: "Entfernen" }).count(), 0, "the first one cannot be removed");

    await conns.getByRole("button", { name: "Verbindung hinzufügen" }).click();
    const dialog = page.locator("dialog[open]");
    await dialog.getByLabel("Name").fill("Ferienwohnung");
    await dialog.screenshot({ path: `${out}/add-${name}.png`, animations: "disabled" });
    await dialog.getByRole("button", { name: "Hinzufügen" }).click();
    await conns.getByRole("button", { name: /Ferienwohnung/ }).waitFor();
    assert.equal(await conns.getByRole("button", { name: /Ferienwohnung/ }).getAttribute("aria-current"), "true");
    // The page now works on the new connection.
    const url = page.getByLabel("Adresse", { exact: false }).first();
    await url.waitFor();
    assert.equal(await url.inputValue(), "");
    assert.ok(calls.some((c) => c[1] === "storage" && c[2] === "ab12cd34"), "storage of the new connection loaded");
    await url.fill("http://192.168.20.5:8123");
    await page.locator('button[type="submit"][form="ha-form"]').click();
    for (let i = 0; i < 50 && settings.ab12cd34.url === ""; i++) await page.waitForTimeout(100);
    assert.equal(settings.ab12cd34.url, "http://192.168.20.5:8123");
    assert.equal(settings.main.url, "http://192.168.10.20:8123", "the first one stays");
    await conns.screenshot({ path: `${out}/list-${name}.png`, animations: "disabled" });
    await page.screenshot({ path: `${out}/page-${name}.png`, animations: "disabled" });
    if (width === 390) {
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
    }

    // Back to the first, then remove the second.
    await conns.getByRole("button", { name: /Erste Verbindung/ }).click();
    await page.waitForFunction(() => document.querySelector('input[type="url"]')?.value === "http://192.168.10.20:8123");
    await conns.getByRole("button", { name: "Entfernen" }).click();
    const confirm = page.locator("dialog[open]");
    await confirm.getByText("„Ferienwohnung“").waitFor();
    await confirm.screenshot({ path: `${out}/remove-${name}.png`, animations: "disabled" });
    await confirm.getByRole("button", { name: "Entfernen" }).click();
    await conns.getByRole("button", { name: /Ferienwohnung/ }).waitFor({ state: "detached" });
    assert.ok(calls.some((c) => c[0] === "DELETE" && c[1].endsWith("/ab12cd34")));
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log(`ok – screenshots in ${out}`);
} finally {
  await browser.close();
}
