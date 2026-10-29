// Storage for Home Assistant (owner request of 29 Oct 2026), Settings ›
// Home Assistant: entries of shares or folders in them, one account for
// all, the password once, entered in Home Assistant, backups watched.
// Desktop and 390 px. Isolated API data only.
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
  const state = { account: "", entries: [], calls: [], password: 0 };
  const status = () => ({
    account: state.account, server: "192.168.10.5", entries: state.entries,
    volumes: ["tank", "fast"], shares: [{ name: "Medien", volume: "tank" }, { name: "Dokumente", volume: "fast" }], homeAssistant: true,
  });
  await made.ctx.route("**/api/v1/homeassistant/storage**", async (r) => {
    const req = r.request();
    const path = new URL(req.url()).pathname;
    state.calls.push([req.method(), path, req.postData() ?? ""]);
    if (req.method() === "POST" && path.endsWith("/storage")) {
      const body = JSON.parse(req.postData());
      const newAccount = !state.account;
      state.account ||= body.account;
      const place = body.folder ? `${body.share}/${body.folder}` : body.share;
      const entry = { share: body.share, folder: body.folder || undefined, existing: !!body.existing, readOnly: body.readOnly, usage: body.usage, mount: "titanos_" + place.toLowerCase().replace(/\W/g, "_"), created: "2026-10-29T18:00:00Z", maxAgeDays: body.usage === "backup" ? 3 : 0 };
      state.entries.push(entry);
      const answer = { setup: { account: state.account, server: "192.168.10.5", entries: state.entries }, entry };
      if (newAccount || body.register) answer.password = `Passwort-${++state.password}-geheim`;
      if (body.register) answer.results = state.entries.map((e) => ({ mount: e.mount, ok: true }));
      return r.fulfill({ status: 201, json: answer });
    }
    if (req.method() === "POST" && path.endsWith("/check")) {
      state.entries[0].backup = { checked: "2026-10-29T18:10:00Z", latest: "2026-10-29T03:00:00Z", name: "Automatisch", overdue: false };
    }
    if (req.method() === "PUT") {
      const mount = path.split("/").pop();
      state.entries.find((e) => e.mount === mount).maxAgeDays = JSON.parse(req.postData()).maxAgeDays;
    }
    if (req.method() === "DELETE") {
      const mount = path.split("/").pop();
      state.entries = state.entries.filter((e) => e.mount !== mount);
      return r.fulfill({ json: { removedError: "" } });
    }
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
    const names = tree[url.searchParams.get("path")] ?? [];
    return r.fulfill({ json: [...names.map((name) => ({ name, dir: true, size: 0 })), { name: "liste.m3u", dir: false, size: 10 }] });
  });
  await made.page.goto(`${base}/settings/homeassistant`);
  await made.page.locator("#storage").waitFor();
  return { ...made, state };
}
const posts = (state) => state.calls.filter(([m, p]) => m === "POST" && p.endsWith("/storage")).map(([, , b]) => JSON.parse(b));

try {
  {
    const { ctx, page, state } = await open(1440);
    const section = page.locator("#storage");
    // The first entry: a new share for backups, the account with it.
    await section.getByRole("button", { name: "Speicher einrichten" }).click();
    let dialog = page.locator("dialog[open]");
    await dialog.getByLabel("Volume").selectOption("fast");
    await dialog.getByLabel("Obergrenze in GiB").fill("200");
    await dialog.screenshot({ path: `${out}/add-first.png`, animations: "disabled" });
    await dialog.getByRole("button", { name: "Einrichten" }).click();
    dialog = page.locator("dialog[open]");
    await dialog.getByText("Passwort-1-geheim").waitFor();
    assert.deepEqual(posts(state)[0], { account: "homeassistant", volume: "fast", share: "homeassistant", quotaGiB: 200, usage: "backup", readOnly: false, register: true });
    await dialog.getByText(/hat den Speicher „titanos_homeassistant“ eingebunden/).waitFor();
    await dialog.getByRole("button", { name: "Schließen" }).click();
    await dialog.waitFor({ state: "detached" });
    assert.equal(await page.getByText("Passwort-1-geheim").count(), 0, "password gone");
    await section.getByText("Konto homeassistant · Server 192.168.10.5").waitFor();

    // A second entry: a folder of a share that exists, read only, no account field.
    await section.getByRole("button", { name: "Freigabe hinzufügen" }).click();
    dialog = page.locator("dialog[open]");
    assert.equal(await dialog.getByLabel("Konto").count(), 0, "one account for all");
    assert.ok(await dialog.getByRole("radio", { name: "Vorhandene Freigabe verwenden" }).isChecked());
    assert.ok(await dialog.getByRole("radio", { name: /Medien/ }).isChecked());
    await dialog.getByText(/aktualisiert damit auch alle anderen Einträge/).waitFor();
    await dialog.getByRole("button", { name: "Ordner wählen …" }).click();
    const picker = page.locator("dialog[open]").last();
    await picker.getByRole("button", { name: "Musik" }).click();
    await picker.getByRole("button", { name: "Rock" }).waitFor();
    assert.equal(await picker.getByRole("button", { name: ".recycle" }).count(), 0);
    assert.equal(await picker.getByText("liste.m3u").count(), 0, "only folders");
    await picker.getByRole("button", { name: "Diesen Ordner nehmen" }).click();
    dialog = page.locator("dialog[open]");
    await dialog.getByText("Medien/Musik", { exact: true }).waitFor();
    await dialog.getByLabel(/Nur lesen/).check();
    await dialog.screenshot({ path: `${out}/add-second.png`, animations: "disabled" });
    await dialog.getByRole("button", { name: "Einrichten" }).click();
    dialog = page.locator("dialog[open]");
    await dialog.getByText("Passwort-2-geheim").waitFor();
    assert.equal(await dialog.locator(".results li").count(), 2, "both entries got the new password");
    assert.deepEqual(posts(state)[1], { account: "homeassistant", existing: true, share: "Medien", folder: "Musik", usage: "media", readOnly: true, register: true });
    await dialog.getByRole("button", { name: "Schließen" }).click();

    // The list, the backups, one entry removed with a plain confirmation.
    assert.equal(await section.locator(".entries li").count(), 2);
    await section.getByRole("button", { name: "Jetzt prüfen" }).click();
    await section.getByText(/Neuestes Backup auf dem NAS: Automatisch/).waitFor();
    await section.locator(".entries li").first().getByRole("combobox").selectOption("7");
    const put = () => state.calls.some(([m, p, b]) => m === "PUT" && p.endsWith("/titanos_homeassistant") && b.includes('"maxAgeDays":7'));
    for (let i = 0; i < 50 && !put(); i++) await page.waitForTimeout(100);
    assert.ok(put(), "days saved");
    await section.screenshot({ path: `${out}/list.png`, animations: "disabled" });
    await section.locator(".entries li").nth(1).getByRole("button", { name: "Entfernen" }).click();
    const confirm = page.locator("dialog[open]");
    assert.equal(await confirm.locator("input").count(), 0);
    await confirm.getByRole("button", { name: "Entfernen" }).click();
    await confirm.waitFor({ state: "detached" });
    assert.ok(state.calls.some(([m, p]) => m === "DELETE" && p.endsWith("/titanos_medien_musik")));
    await page.waitForFunction(() => document.querySelectorAll("#storage .entries li").length === 1);
    await ctx.close();
  }
  {
    const { ctx, page } = await open(390);
    await page.locator("#storage").getByRole("button", { name: "Speicher einrichten" }).click();
    await page.locator("dialog[open]").getByRole("button", { name: "Einrichten" }).waitFor();
    assert.ok((await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)) <= 0, "no sideways scroll");
    await page.locator("dialog[open]").screenshot({ path: `${out}/mobile.png`, animations: "disabled" });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("ui ha storage ok:", out);
} finally {
  await browser.close();
}
