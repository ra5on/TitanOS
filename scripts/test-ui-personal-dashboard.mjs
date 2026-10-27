// Real browser interactions against isolated API fixtures. No NAS is contacted.
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
const out = process.env.UI_OUTPUT || "out/validation/ui-personal-dashboard";
await mkdir(out, { recursive: true });
const errors = [],
  context = fixtureFactory(browser, errors);
const tile = (page, id) => page.locator(`.grid-stack-item[gs-id="${id}"]`);
const inspector = (page) => page.locator(".inspector");
async function slider(page, label, value) {
  await inspector(page)
    .getByRole("slider", { name: label, exact: true })
    .fill(String(value));
}
function telemetry(data) {
  data.system.network = {
    ...data.system.network,
    interface: "eth0",
    rxBytesPerSecond: 4e6,
    txBytesPerSecond: 2e6,
  };
  data.system.networks = [
    data.system.network,
    { ...data.system.network, interface: "br0" },
  ];
  data.system.accelerators = [
    {
      address: "0000:00:02.0",
      name: "Fixture iGPU",
      vendor: "Intel",
      kind: "gpu",
      state: "ready",
      driver: "i915",
      usagePercent: 27,
      temperatureC: 44,
      powerWatts: 6,
      memoryUsedBytes: 1e9,
      memoryTotalBytes: 4e9,
      frequencyMHz: 500,
      engines: [],
      history: [20, null, 27],
    },
    {
      address: "0000:00:04.0",
      name: "Fixture NPU",
      vendor: "Intel",
      kind: "npu",
      state: "ready",
      driver: "ivpu",
      usagePercent: null,
      temperatureC: null,
      powerWatts: null,
      memoryUsedBytes: null,
      memoryTotalBytes: null,
      frequencyMHz: null,
      engines: [],
      history: [null],
    },
  ];
}
try {
  const { ctx, page, data } = await context();
  telemetry(data);
  await page.goto(base + "/");
  await tile(page, "docker")
    .getByRole("button", { name: "photos: Container anzeigen", exact: true })
    .waitFor();
  assert.equal(
    await page
      .getByText(/Aufmerksamkeit|nächste Schritte|Laufende Aufgaben/)
      .count(),
    0,
  );
  assert.equal(
    await tile(page, "docker")
      .getByRole("button", { name: "photos: Container anzeigen", exact: true })
      .getAttribute("aria-expanded"),
    "false",
  );
  await tile(page, "docker")
    .getByRole("button", { name: "photos: Container anzeigen", exact: true })
    .click();
  await tile(page, "docker")
    .getByText("photos-web-1", { exact: true })
    .waitFor();
  await tile(page, "docker")
    .getByRole("button", { name: "Aktionen: photos", exact: true })
    .click();
  assert.equal(
    await page.getByRole("menu").locator('a[target="_blank"]').count(),
    3,
  );
  await page.keyboard.press("Escape");
  assert.equal(
    await tile(page, "accelerators").locator(".reading").last().textContent(),
    "–%",
  );
  await page.screenshot({ path: out + "/dashboard.png", fullPage: true });
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .click();
  await tile(page, "docker").locator(".identity .entry-name").first().click();
  assert.equal(new URL(page.url()).pathname, "/");
  assert.equal(
    await inspector(page)
      .getByRole("combobox", { name: "Ausgewähltes Element", exact: true })
      .inputValue(),
    "value",
  );
  await tile(page, "cpu")
    .getByRole("button", { name: "Kachel anpassen: Prozessor", exact: true })
    .click();
  await inspector(page)
    .getByRole("combobox", { name: "Ausgewähltes Element", exact: true })
    .selectOption("value");
  await slider(page, "Schriftgröße", 56);
  assert.equal(
    await tile(page, "cpu")
      .locator(".reading")
      .evaluate((e) => getComputedStyle(e).fontSize),
    "56px",
  );
  await page.getByRole("button", { name: "Rückgängig", exact: true }).click();
  assert.equal(
    await tile(page, "cpu")
      .locator(".reading")
      .evaluate((e) => getComputedStyle(e).fontSize),
    "18px",
  );
  await page.getByRole("button", { name: "Wiederholen", exact: true }).click();
  await tile(page, "cpu").locator(".grip").focus();
  await page.keyboard.press("ArrowDown");
  await page.waitForFunction(() =>
    document.activeElement?.classList.contains("grip"),
  );
  await page.keyboard.press("ArrowRight");
  await page.waitForFunction(() =>
    document.activeElement?.classList.contains("grip"),
  );
  await inspector(page)
    .getByRole("combobox", { name: "Messwertdarstellung", exact: true })
    .selectOption("ring");
  assert.equal(await tile(page, "cpu").locator("svg.ring-svg").count(), 1);
  await inspector(page)
    .getByRole("button", { name: "Inhalt", exact: true })
    .click();
  await inspector(page)
    .getByRole("textbox", { name: "Eigener Anzeigename", exact: true })
    .fill("Meine CPU");
  await inspector(page)
    .getByRole("checkbox", { name: "Zusatzwerte anzeigen", exact: true })
    .uncheck();
  await inspector(page)
    .getByRole("button", { name: "Größe", exact: true })
    .click();
  await inspector(page)
    .getByRole("spinbutton", { name: "Höhe (px)", exact: true })
    .fill("160");
  await inspector(page)
    .getByRole("spinbutton", { name: "Höhe (px)", exact: true })
    .press("Tab");
  await inspector(page)
    .getByRole("button", { name: "Kachel duplizieren", exact: true })
    .click();
  assert.equal(
    await page.locator('.grid-stack-item[gs-id^="cpu@"]').count(),
    1,
  );
  await page.screenshot({ path: out + "/editor.png", fullPage: true });
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .waitFor();
  assert.equal(data.prefs.dashboard.version, 6);
  assert.equal(data.prefs.dashboard.tileOptions.cpu.valueSize, 56);
  assert.equal(data.prefs.dashboard.tiles.find((t) => t.id === "cpu").h, 4);
  assert.equal(
    data.prefs.dashboard.tiles.find((t) => t.id === "cpu").autoHeight,
    false,
  );
  const saved = structuredClone(data.prefs.dashboard);
  await page.reload();
  await tile(page, "cpu")
    .getByRole("heading", { name: "Meine CPU", exact: true })
    .waitFor();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(350);
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  );
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .click();
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .waitFor();
  assert.deepEqual(
    data.prefs.dashboard.tiles,
    saved.tiles,
    "mobile save corrupted desktop positions",
  );
  await page.screenshot({ path: out + "/mobile.png", fullPage: true });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .click();
  await tile(page, "cpu")
    .getByRole("button", { name: "Kachel anpassen: Meine CPU", exact: true })
    .click();
  await inspector(page)
    .getByRole("button", { name: "Inhalt", exact: true })
    .click();
  await inspector(page)
    .getByRole("textbox", { name: "Eigener Anzeigename", exact: true })
    .fill("Discard me");
  await page.getByRole("button", { name: "Abbrechen", exact: true }).click();
  assert.equal(
    await tile(page, "cpu")
      .getByRole("heading", { name: "Meine CPU", exact: true })
      .count(),
    1,
  );
  // Save errors keep the editable draft and allow a real retry.
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .click();
  data.failSave = true;
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  await page.getByRole("alert").waitFor();
  data.failSave = false;
  await page
    .getByRole("button", { name: "Erneut versuchen", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .waitFor();
  // Tailscale tile: add it, choose devices by stable ID and fields in order.
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .click();
  await page
    .locator(".add-tiles")
    .getByRole("button", { name: "Tailscale", exact: true })
    .click();
  await tile(page, "tailscale")
    .getByText("alex-laptop", { exact: true })
    .waitFor();
  assert.equal(
    await tile(page, "tailscale")
      .getByText("Dieses NAS", { exact: true })
      .count(),
    1,
  );
  await tile(page, "tailscale")
    .getByRole("button", { name: "Kachel anpassen: Tailscale", exact: true })
    .click();
  await inspector(page)
    .getByRole("button", { name: "Inhalt", exact: true })
    .click();
  await inspector(page)
    .getByRole("checkbox", { name: "Subnet-Routen", exact: true })
    .check();
  await inspector(page)
    .getByRole("checkbox", { name: "Betriebssystem", exact: true })
    .uncheck();
  await inspector(page)
    .getByRole("button", { name: "Subnet-Routen: Nach oben", exact: true })
    .click();
  // Reordered rows keep the checkbox state of their own field.
  for (const [name, checked] of [
    ["Betriebssystem", false],
    ["Exit Node", false],
    ["Zuletzt gesehen", true],
  ])
    assert.equal(
      await inspector(page)
        .getByRole("checkbox", { name, exact: true })
        .isChecked(),
      checked,
      name,
    );
  await inspector(page)
    .getByRole("checkbox", {
      name: "Alle Einträge, einschließlich neuer",
      exact: true,
    })
    .uncheck();
  await inspector(page)
    .getByRole("checkbox", { name: "router", exact: true })
    .uncheck();
  assert.equal(
    await tile(page, "tailscale").getByText("router", { exact: true }).count(),
    0,
  );
  await tile(page, "tailscale")
    .getByText("192.168.10.0/24", { exact: true })
    .waitFor();
  await page.screenshot({ path: out + "/tailscale.png", fullPage: true });
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .waitFor();
  const tailscaleOptions = data.prefs.dashboard.tileOptions.tailscale;
  assert.deepEqual(tailscaleOptions.items, ["nSELF1CNTRL", "nLAPTOP2CNTRL"]);
  assert.deepEqual(tailscaleOptions.tailscaleFields, [
    "status",
    "self",
    "ipv4",
    "routes",
    "lastSeen",
  ]);
  assert.ok(data.prefs.dashboard.tiles.some((t) => t.id === "tailscale"));
  // Without an active plugin the tile states it calmly instead of failing.
  data.tailnet = null;
  await page.reload();
  await tile(page, "tailscale")
    .getByText("Das Tailscale-Plugin ist nicht aktiv.", { exact: true })
    .waitFor();
  await page.goto(base + "/storage");
  await page.getByRole("button", { name: "Data", exact: true }).click();
  await page.locator(".storage-layout aside").waitFor();
  await page
    .getByRole("searchbox", {
      name: "Volumes oder Laufwerke suchen",
      exact: true,
    })
    .fill("Fast");
  assert.equal(
    await page.locator(".storage-layout .table tbody tr").count(),
    1,
  );
  await page
    .getByRole("searchbox", {
      name: "Volumes oder Laufwerke suchen",
      exact: true,
    })
    .fill("");
  await page.getByRole("button", { name: /^Laufwerke \d+$/ }).click();
  await page.getByRole("button", { name: "vdd", exact: true }).click();
  await page
    .locator(".disk-details")
    .getByText("TEST4", { exact: true })
    .waitFor();
  await page.screenshot({ path: out + "/storage.png", fullPage: true });
  await page.goto(base + "/settings");
  await page.locator(".settings-group").first().waitFor();
  assert.equal(await page.locator(".settings-group").count(), 5);
  for (const group of await page.locator(".settings-group").all()) {
    const labels = await group.locator(".name").allTextContents();
    assert.deepEqual(
      labels,
      [...labels].sort((a, b) => a.localeCompare(b, "de")),
    );
  }
  await page.getByRole("searchbox").fill("SSH");
  await page.locator('a[href="/settings/access#access-ssh"]').waitFor();
  await page.getByRole("searchbox").fill("");
  await page.screenshot({ path: out + "/settings.png", fullPage: true });
  data.ha = {
    settings: {
      enabled: false,
      url: "",
      fingerprint: "",
      domains: [],
      entities: [],
      mqtt: {
        enabled: true,
        host: "broker.test",
        port: 1883,
        username: "",
        tls: false,
        prefix: "homeassistant",
        groups: ["gpu"],
        commands: [],
        devices: { gpu: ["removed-pci"] },
      },
    },
    tokenSet: false,
    mqttPasswordSet: true,
    domains: [],
    sensorGroups: ["system", "gpu", "igpu", "npu", "network"],
    commandGroups: [],
    status: { watching: false, mqtt: true },
    mqttDevices: [
      { group: "gpu", id: "card-a", name: "GPU A" },
      { group: "gpu", id: "card-b", name: "GPU B" },
    ],
  };
  await page.goto(base + "/settings/homeassistant");
  await page.locator(".device-selection summary").click();
  await page
    .getByRole("checkbox", { name: "removed-pci", exact: true })
    .waitFor();
  await page.getByRole("checkbox", { name: "GPU A", exact: true }).check();
  await page
    .getByRole("checkbox", { name: "removed-pci", exact: true })
    .click();
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  assert.deepEqual(data.ha.settings.mqtt.devices.gpu, ["card-a"]);
  await page.screenshot({ path: out + "/mqtt.png", fullPage: true });
  await ctx.close();
  // Existing custom v3 coordinates and options survive migration. Regular users
  // never query Docker or VM routes and see their permitted shares.
  const regular = await context("user", "hell", 390, "en");
  regular.data.prefs.dashboard = {
    version: 3,
    tiles: [{ id: "network", x: 7, y: 3, w: 5, h: 4 }],
    tileOptions: { network: { ipv6: false } },
  };
  await regular.page.goto(base + "/");
  await tile(regular.page, "network").waitFor();
  await regular.page
    .getByRole("button", { name: "Edit dashboard", exact: true })
    .click();
  await regular.page.getByRole("button", { name: "Save", exact: true }).click();
  assert.equal(regular.data.prefs.dashboard.tileOptions.network.ipv6, false);
  assert.equal(regular.data.prefs.dashboard.tiles[0].id, "network");
  assert.equal(regular.data.prefs.dashboard.tiles[0].span, 1);
  assert.equal(regular.data.prefs.dashboard.tiles[0].autoHeight, true);
  assert.equal(
    regular.data.requests.filter((r) =>
      ["/docker", "/vms", "/plugins/tailscale/devices"].includes(r[1]),
    ).length,
    0,
  );
  await regular.ctx.close();
  assert.deepEqual(errors, []);
  console.log(
    "Personal dashboard/editor, storage, settings and MQTT UI passed.",
  );
} finally {
  await browser.close();
}
