// Storage for Home Assistant (owner request of 29 Oct 2026), Settings ›
// Home Assistant: set up a share and account, show the values once with the
// password, enter it in Home Assistant, watch the backups. Desktop and 390
// px. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5186 node scripts/test-ui-ha-storage.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5186";
const out = process.env.UI_OUTPUT || "out/validation/ui-ha-storage";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

async function open(width) {
  const made = await create("owner", "graphit", width);
  const state = { setup: null, calls: [], backup: undefined };
  const status = () => ({ setup: state.setup, volumes: ["tank", "fast"], shares: [{ name: "Medien", volume: "tank" }, { name: "Dokumente", volume: "fast" }], homeAssistant: true, server: "192.168.10.5", backup: state.backup });
  await made.ctx.route("**/api/v1/homeassistant/storage**", async (r) => {
    const req = r.request();
    const path = new URL(req.url()).pathname;
    state.calls.push([req.method(), path, req.postData() ?? ""]);
    if (req.method() === "POST" && path.endsWith("/storage")) {
      const body = JSON.parse(req.postData());
      state.setup = { share: body.share, folder: body.folder, existing: body.existing, readOnly: body.readOnly, account: body.account, usage: body.usage, mount: "titanos_" + body.share.toLowerCase(), server: "192.168.10.5", created: "2026-10-29T18:00:00Z", maxAgeDays: 3 };
      return r.fulfill({ status: 201, json: { setup: state.setup, password: "Xy7-geheimes-Passwort-9", registered: true } });
    }
    if (req.method() === "POST" && path.endsWith("/check")) {
      state.backup = { checked: "2026-10-29T18:10:00Z", latest: "2026-10-29T03:00:00Z", name: "Automatisch", overdue: false };
    }
    if (req.method() === "PUT") state.setup.maxAgeDays = JSON.parse(req.postData()).maxAgeDays;
    if (req.method() === "DELETE") state.setup = null;
    return r.fulfill({ json: status() });
  });
  // What titand answers for a connected Home Assistant without MQTT.
  await made.ctx.route("**/api/v1/homeassistant/settings", (r) =>
    r.fulfill({
      json: {
        settings: {
          enabled: true, url: "http://192.168.10.20:8123", fingerprint: "", account: "alex", domains: [], entities: [], policies: {},
          mqtt: { enabled: false, host: "", port: 1883, username: "", tls: false, prefix: "homeassistant", groups: [], commands: [] },
        },
        tokenSet: true, mqttPasswordSet: false, domains: [], sensorGroups: [], commandGroups: [], mqttDevices: [],
        status: { watching: true, watchError: "", mqtt: false, mqttError: "" }, diagnostics: null,
      },
    }),
  );
  // Folders of the share Medien for the chooser.
  const tree = { "": ["Musik", "Videos", ".recycle"], Musik: ["Rock", "Jazz"], "Musik/Rock": [], "Musik/Jazz": [], Videos: [] };
  await made.ctx.route("**/api/v1/files/list?**", (r) => {
    const url = new URL(r.request().url());
    state.calls.push(["GET", url.pathname, url.search]);
    const names = tree[url.searchParams.get("path")] ?? [];
    return r.fulfill({ json: [...names.map((name) => ({ name, dir: true, size: 0 })), { name: "liste.m3u", dir: false, size: 10 }] });
  });
  await made.page.goto(`${base}/settings/homeassistant`);
  await made.page.locator("#storage").waitFor();
  return { ...made, state };
}

try {
  {
    const { ctx, page, state } = await open(1440);
    const section = page.locator("#storage");
    await section.getByLabel("Volume").selectOption("fast");
    await section.getByLabel("Obergrenze in GiB").fill("200");
    await section.getByRole("radio", { name: /Backups/ }).check();
    await section.screenshot({ path: `${out}/form.png`, animations: "disabled" });
    await section.getByRole("button", { name: "Einrichten" }).click();
    const dialog = page.locator("dialog[open]");
    await dialog.getByText("Xy7-geheimes-Passwort-9").waitFor();
    const post = JSON.parse(state.calls.find(([m, p]) => m === "POST" && p.endsWith("/storage"))[2]);
    assert.deepEqual(post, { volume: "fast", share: "homeassistant", account: "homeassistant", usage: "backup", quotaGiB: 200, readOnly: false, register: true });
    await dialog.getByText(/hat den Speicher „titanos_homeassistant“ eingebunden/).waitFor();
    await dialog.getByText(/nur jetzt angezeigt/).waitFor();
    await dialog.screenshot({ path: `${out}/result.png`, animations: "disabled" });
    await dialog.getByRole("button", { name: "Schließen" }).click();
    await dialog.waitFor({ state: "detached" });
    // The password is gone once the dialog is closed.
    assert.equal(await page.getByText("Xy7-geheimes-Passwort-9").count(), 0);
    // Watching the backups.
    await section.getByRole("button", { name: "Jetzt prüfen" }).click();
    await section.getByText(/Neuestes Backup auf dem NAS: Automatisch/).waitFor();
    await section.getByLabel(/Melden, wenn kein neues Backup kam seit/).selectOption("7");
    await section.getByRole("button", { name: "Speichern" }).click();
    await page.waitForFunction(() => true);
    assert.ok(state.calls.some(([m, , b]) => m === "PUT" && b.includes('"maxAgeDays":7')));
    await section.screenshot({ path: `${out}/managed.png`, animations: "disabled" });
    // Stop managing: a plain confirmation, no name to type.
    await section.getByRole("button", { name: "Nicht mehr verwalten" }).click();
    const confirm = page.locator("dialog[open]");
    assert.equal(await confirm.locator("input").count(), 0);
    await confirm.getByRole("button", { name: "Nicht mehr verwalten" }).click();
    await section.getByRole("button", { name: "Einrichten" }).waitFor();
    await ctx.close();
  }
  {
    // A share that exists, a folder in it, read only.
    const { ctx, page, state } = await open(1440);
    const section = page.locator("#storage");
    await section.getByRole("radio", { name: "Vorhandene Freigabe verwenden" }).check();
    assert.equal(await section.getByLabel("Obergrenze in GiB").count(), 0, "no limit for a share that exists");
    await section.getByRole("radio", { name: /Medien/ }).check();
    await section.getByRole("button", { name: "Ordner wählen …" }).click();
    const picker = page.locator("dialog[open]");
    await picker.getByRole("button", { name: "Musik" }).click();
    await picker.getByRole("button", { name: "Rock" }).waitFor();
    assert.equal(await picker.getByRole("button", { name: ".recycle" }).count(), 0);
    assert.equal(await picker.getByText("liste.m3u").count(), 0, "only folders");
    await picker.screenshot({ path: `${out}/picker.png`, animations: "disabled" });
    await picker.getByRole("button", { name: "Diesen Ordner nehmen" }).click();
    await section.getByText("Medien/Musik", { exact: true }).waitFor();
    await section.getByLabel(/Nur lesen/).check();
    await section.screenshot({ path: `${out}/existing.png`, animations: "disabled" });
    await section.getByRole("button", { name: "Einrichten" }).click();
    await page.locator("dialog[open]").getByText("Xy7-geheimes-Passwort-9").waitFor();
    const post = JSON.parse(state.calls.find(([m, p]) => m === "POST" && p.endsWith("/storage"))[2]);
    assert.deepEqual(post, { existing: true, share: "Medien", folder: "Musik", account: "homeassistant", usage: "media", readOnly: true, register: true });
    await ctx.close();
  }
  {
    const { ctx, page } = await open(390);
    assert.ok((await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)) <= 0, "no sideways scroll");
    await page.locator("#storage").screenshot({ path: `${out}/mobile.png`, animations: "disabled" });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("ui ha storage ok:", out);
} finally {
  await browser.close();
}
