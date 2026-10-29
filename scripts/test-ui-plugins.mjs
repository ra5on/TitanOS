// Plugin page: updating an installed plugin from a share. All NAS endpoints
// are isolated fixtures; no request reaches a NAS.
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
const base = process.env.UI_URL || "http://127.0.0.1:5186";
const out = process.env.UI_OUTPUT || "out/validation/ui-plugins";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

const plugin = (version, update) => ({
  name: "werkzeug",
  title: "Werkzeug",
  description: { de: "Ein Test-Plugin", en: "A test plugin" },
  version,
  size: 4096,
  kernel: "",
  contains: null,
  modules: [],
  services: ["werkzeug.service"],
  installed: true,
  enabled: true,
  active: true,
  path: "",
  official: false,
  local: false,
  download: false,
  problem: "",
  update,
});
const status = (version, update) => ({
  volume: "Data",
  kernel: "test",
  plugins: [plugin(version, update)],
  catalog: { release: "0.24.0", state: "ready" },
  driverUpdate: null,
});
const offered = { path: "/mnt/Data/media/werkzeug.plugin.json", version: "2.0", official: false };

try {
  const { ctx, page, data } = await context();
  data.plugins = status("1.0", offered);
  data.pluginsAfterUpdate = status("2.0", null);
  await page.goto(base + "/plugins");
  await page.getByText("Version 2.0 liegt in einer Freigabe bereit.").waitFor();
  await page.screenshot({ path: `${out}/offered.png`, fullPage: true });

  // Cancel changes nothing.
  await page.getByRole("button", { name: "Aktualisieren", exact: true }).click();
  let dialog = page.getByRole("dialog");
  await dialog.getByText("Werkzeug auf 2.0 aktualisieren?").waitFor();
  await dialog.getByText(offered.path).waitFor();
  await page.screenshot({ path: `${out}/dialog.png`, fullPage: true });
  await dialog.getByRole("button", { name: "Abbrechen", exact: true }).click();
  assert.equal(data.pluginUpdates, undefined, "cancel sent an update");

  // A refused update keeps the dialog open with the reason.
  data.failPluginUpdate = true;
  await page.getByRole("button", { name: "Aktualisieren", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "Aktualisieren", exact: true }).click();
  await dialog.getByRole("alert").waitFor();

  // The click is the confirmation; the page sends the name the API wants.
  data.failPluginUpdate = false;
  await dialog.getByRole("button", { name: "Aktualisieren", exact: true }).click();
  await dialog.waitFor({ state: "detached" });
  assert.deepEqual(data.pluginUpdates.at(-1), [
    "/plugins/werkzeug/update",
    { path: offered.path, confirm: "werkzeug" },
  ]);
  await page.getByText("werkzeug · 2.0").waitFor();
  assert.equal(await page.getByText("liegt in einer Freigabe bereit").count(), 0);
  await page.screenshot({ path: `${out}/updated.png`, fullPage: true });
  await ctx.close();

  // A newer version in the official catalog: no path, the NAS downloads it.
  {
    const { ctx, page, data } = await context();
    const catalog = { path: "", version: "2.1", official: true, download: true };
    data.plugins = status("1.0", catalog);
    data.pluginsAfterUpdate = status("2.1", null);
    await page.goto(base + "/plugins");
    await page.getByText("Version 2.1 ist im offiziellen Katalog verfügbar.").waitFor();
    await page.getByRole("button", { name: "Aktualisieren", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByText("lädt die signierte neue Version", { exact: false }).waitFor();
    await page.screenshot({ path: `${out}/catalog-dialog.png`, fullPage: true });
    await dialog.getByRole("button", { name: "Aktualisieren", exact: true }).click();
    await dialog.waitFor({ state: "detached" });
    assert.deepEqual(data.pluginUpdates.at(-1), [
      "/plugins/werkzeug/update",
      { path: "", confirm: "werkzeug" },
    ]);
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("plugin page update: ok");
} finally {
  await browser.close();
}
