// NetBird plugin view and dashboard tile against isolated API fixtures. No
// request reaches a NAS or a NetBird server.
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
const out = process.env.UI_OUTPUT || "out/validation/ui-netbird";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);
const tile = (page, id) => page.locator(`.grid-stack-item[gs-id="${id}"]`);
const inspector = (page) => page.locator(".inspector");
const key = "A1B2C3D4-E5F6-4A7B-8C9D-0E1F2A3B4C5D";

const plugin = {
  name: "netbird",
  title: "NetBird",
  description: {
    de: "Fernzugriff über ein NetBird-Netz.",
    en: "Remote access through a NetBird network.",
  },
  version: "0.24.0",
  size: 8 << 20,
  kernel: "",
  contains: null,
  modules: [],
  services: ["titanos-netbird.service"],
  installed: true,
  enabled: true,
  active: true,
  path: "",
  official: true,
  local: false,
  download: false,
  problem: "",
  update: null,
};

try {
  // Plugin page: setup key, connect, settings on trial.
  {
    const { ctx, page, data } = await context();
    data.plugins = {
      volume: "Data",
      kernel: "test",
      plugins: [plugin],
      catalog: { release: "0.24.0", state: "ready" },
      driverUpdate: null,
    };
    await page.goto(base + "/plugins");
    const view = page.getByRole("region", { name: "NetBird" });
    await view.getByText("Anmeldung erforderlich", { exact: true }).waitFor();
    await view.getByText("Kein Setup-Key gespeichert.").waitFor();
    // Without a key there is nothing to connect with.
    assert.ok(
      await view
        .getByRole("button", { name: "Verbinden", exact: true })
        .isDisabled(),
    );
    await view.getByLabel("Setup-Key", { exact: true }).fill(key);
    await view
      .getByRole("button", { name: "Setup-Key speichern", exact: true })
      .click();
    await view.getByText("Ein Setup-Key ist gespeichert.").waitFor();
    assert.equal(await view.getByLabel("Setup-Key", { exact: true }).inputValue(), "");
    assert.deepEqual(data.netbirdCalls.at(-1), [
      "PUT",
      "/plugins/netbird/setup-key",
      { setupKey: key },
    ]);
    assert.ok(!(await page.content()).includes(key), "key still on the page");
    await view.getByRole("button", { name: "Verbinden", exact: true }).click();
    await view.getByText("Verbunden", { exact: true }).waitFor();
    await view.getByText("titan.netbird.cloud").waitFor();
    await view.getByText("2 von 3 Geräten verbunden").waitFor();
    assert.equal(data.netbirdCalls.at(-1)[1], "/plugins/netbird/control/connect");

    // A self-hosted server and routing run on trial until confirmed.
    await view
      .getByLabel("Management-Server", { exact: true })
      .fill("https://netbird.example.org:443");
    await view
      .getByRole("checkbox", {
        name: "Dieses NAS als Routing-Peer bzw. Exit Node zulassen",
      })
      .check();
    await view
      .getByRole("button", { name: "Änderungen auf Probe anwenden" })
      .click();
    await view.getByText(/Bitte den Zugriff prüfen/).waitFor();
    assert.deepEqual(data.netbirdCalls.at(-1), [
      "PUT",
      "/plugins/netbird",
      {
        managementUrl: "https://netbird.example.org:443",
        acceptRoutes: false,
        serveRoutes: true,
        acceptDNS: false,
      },
    ]);
    // Plugin changes wait for the trial.
    assert.ok(
      await page
        .getByRole("button", { name: "Entfernen …", exact: true })
        .isDisabled(),
    );
    await page.screenshot({ path: `${out}/trial.png`, fullPage: true });
    await view
      .getByRole("button", { name: "Zugriff funktioniert – speichern" })
      .click();
    await view.getByText(/Bitte den Zugriff prüfen/).waitFor({ state: "detached" });
    assert.equal(data.netbirdCalls.at(-1)[1], "/plugins/netbird/control/confirm");

    // Removing the NAS from the network asks first; cancel sends nothing.
    const calls = data.netbirdCalls.length;
    await view
      .getByRole("button", { name: "NAS aus dem NetBird-Netz entfernen" })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog.getByText(/für eine neue Anmeldung ist wieder ein Setup-Key nötig/).waitFor();
    await dialog.getByRole("button", { name: "Abbrechen", exact: true }).click();
    assert.equal(data.netbirdCalls.length, calls);
    await page.screenshot({ path: `${out}/settings.png`, fullPage: true });
    await ctx.close();
  }

  // Dashboard tile: peers by public key, fields in order, connected-only.
  {
    const { ctx, page, data } = await context();
    await page.goto(base + "/");
    await page
      .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
      .click();
    await page
      .locator(".editbar")
      .getByRole("button", { name: "Kachel hinzufügen", exact: true })
      .click();
    await page
      .locator(".add-tiles")
      .getByRole("button", { name: "NetBird", exact: true })
      .click();
    await tile(page, "netbird").getByText("alex-laptop", { exact: true }).waitFor();
    assert.equal(
      await tile(page, "netbird").getByText("Dieses NAS", { exact: true }).count(),
      1,
    );
    await tile(page, "netbird").getByText("Über Relay", { exact: true }).waitFor();
    await tile(page, "netbird")
      .getByRole("button", { name: "Kachel anpassen: NetBird", exact: true })
      .click();
    await inspector(page)
      .getByRole("button", { name: "Inhalt", exact: true })
      .click();
    await inspector(page)
      .getByRole("checkbox", { name: "Netze", exact: true })
      .check();
    await inspector(page)
      .getByRole("checkbox", { name: "Letzter Handshake", exact: true })
      .uncheck();
    await inspector(page)
      .getByRole("button", { name: "Netze: Nach oben", exact: true })
      .click();
    await inspector(page)
      .getByRole("checkbox", { name: "Nur verbundene Geräte anzeigen", exact: true })
      .check();
    assert.equal(
      await tile(page, "netbird").getByText("phone", { exact: true }).count(),
      0,
    );
    await inspector(page)
      .getByRole("checkbox", {
        name: "Alle Einträge, einschließlich neuer",
        exact: true,
      })
      .uncheck();
    await inspector(page)
      .getByRole("checkbox", { name: "office-router", exact: true })
      .uncheck();
    assert.equal(
      await tile(page, "netbird").getByText("office-router", { exact: true }).count(),
      0,
    );
    await tile(page, "netbird").getByText("192.168.10.0/24", { exact: true }).waitFor();
    await page.screenshot({ path: `${out}/tile.png`, fullPage: true });
    await page.getByRole("button", { name: "Speichern", exact: true }).click();
    await page
      .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
      .waitFor();
    const options = data.prefs.dashboard.tileOptions.netbird;
    assert.deepEqual(options.items, [
      "dGl0YW4tc2VsZg==",
      "YWxleC1sYXB0b3A=",
      "cGhvbmU=",
    ]);
    assert.deepEqual(options.netbirdFields, [
      "status",
      "self",
      "ipv4",
      "networks",
      "connection",
    ]);
    assert.equal(options.runningOnly, true);
    // Without an active plugin the tile states it calmly instead of failing.
    data.netbirdPeers = null;
    await page.reload();
    await tile(page, "netbird")
      .getByText("Das NetBird-Plugin ist nicht aktiv.", { exact: true })
      .waitFor();
    await ctx.close();
  }

  // Accounts with the role user are not offered the tile and never ask.
  {
    const { ctx, page, data } = await context("user");
    await page.goto(base + "/");
    await page
      .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
      .click();
    await page
      .locator(".editbar")
      .getByRole("button", { name: "Kachel hinzufügen", exact: true })
      .click();
    await page.locator(".add-tiles").waitFor();
    assert.equal(
      await page
        .locator(".add-tiles")
        .getByRole("button", { name: "NetBird", exact: true })
        .count(),
      0,
    );
    assert.equal(
      data.requests.filter((r) => r[1].startsWith("/plugins/netbird")).length,
      0,
    );
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("NetBird settings, trial and dashboard tile: ok");
} finally {
  await browser.close();
}
