// PLAN §45 (owner decisions of 5 Nov 2026) against isolated fixtures:
// - Tools › Hardware: "Abhilfe setzen" for a USB bridge with UAS trouble
//   writes usb-storage for that device, shows "after the next restart" and
//   can be undone; a port without a cable is neutral "nicht verbunden".
// - Settings › Home Assistant: the MQTT interval is sent as a number.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5179 node scripts/test-ui-uas-mqtt.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5179";
const out = process.env.UI_OUTPUT || "out/validation/uas-mqtt";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const report = {
  generated: "2026-11-05T07:23:43Z", kernel: "6.12.115+deb13.1-amd64", board: "Testboard", boot: "uefi",
  cpu: { model: "Test CPU", vendor: "intel", cores: 4, threads: 4, level: 3, features: ["avx2"] },
  virtualization: { cpuFlag: true, kvm: true, disabledByFirmware: false, iommu: true, iommuTable: true },
  memory: { totalBytes: 16e9, ecc: false, corrected: 0, uncorrected: 0 },
  controllers: [],
  disks: [
    { name: "sda", model: "3.0", sizeBytes: 500e9, transport: "usb", rotational: true, removable: false, usb: "152d:0578", usbDriver: "uas",
      hints: [{ code: "uas_quirk", setting: "usb-storage.quirks=152d:0578:u" }] },
    { name: "sdc", model: "SN570 2TB", sizeBytes: 2000e9, transport: "usb", rotational: false, removable: false, usb: "0bda:9210", usbDriver: "uas", hints: [] },
  ],
  network: [
    { name: "enp2s0", driver: "r8169", address: "0000:02:00.0", bus: "pci", speedMbs: 1000, link: true },
    { name: "enp3s0", driver: "r8169", address: "0000:03:00.0", bus: "pci", speedMbs: 0, link: false },
  ],
  graphics: [], sensors: [], unbound: [], firmwareMissing: [],
  summary: [{ level: "warn", code: "uas_quirk", subject: "sda 3.0" }],
};

try {
  for (const [name, width] of [["1440", 1440], ["390", 390]]) {
    const { ctx, page } = await create("owner", "graphit", width);
    const quirks = { devices: [], active: [], pending: [], entries: { a: "" }, deferred: false, writable: true };
    const puts = [];
    await page.route("**/api/v1/hardware/report", (r) => r.fulfill({ json: report }));
    await page.route("**/api/v1/hardware/uas-quirks", (r) => {
      if (r.request().method() === "PUT") {
        const body = r.request().postDataJSON();
        puts.push(body);
        quirks.devices = body.on ? [body.device] : [];
        quirks.pending = body.on ? [body.device] : [];
      }
      return r.fulfill({ json: quirks });
    });
    await page.goto(base + "/tools/hardware");
    await page.getByRole("button", { name: "Hardware prüfen" }).click();
    const disks = page.getByRole("region", { name: "Platten", exact: true });
    await disks.getByRole("button", { name: "Abhilfe setzen" }).click();
    await disks.getByText("Abhilfe eingetragen, wirkt nach dem nächsten Neustart.").waitFor();
    assert.deepEqual(puts, [{ device: "152d:0578", on: true }]);
    // Only the bridge with known trouble gets the button.
    assert.equal(await disks.getByRole("button", { name: /Abhilfe/ }).count(), 1);
    await disks.screenshot({ path: `${out}/hardware-uas-${name}.png`, animations: "disabled" });
    const network = page.getByRole("region", { name: "Netzwerk", exact: true });
    await network.getByText("nicht verbunden").waitFor();
    assert.equal(await network.locator(".notice-warn, .error").count(), 0, "a port without cable shows no warning");
    await disks.getByRole("button", { name: "Abhilfe zurücknehmen" }).click();
    await disks.getByRole("button", { name: "Abhilfe setzen" }).waitFor();
    assert.deepEqual(puts.at(-1), { device: "152d:0578", on: false });
    if (width === 390) {
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
    }

    // MQTT interval.
    let saved = null;
    await page.route("**/api/v1/homeassistant/settings", (r) => {
      if (r.request().method() === "PUT") saved = r.request().postDataJSON();
      const mqtt = { enabled: true, host: "192.168.10.5", port: 1883, username: "", tls: false, prefix: "homeassistant", groups: ["system"], commands: [], interval: saved?.mqtt.interval || 0 };
      return r.fulfill({ json: {
        settings: { enabled: true, url: "http://192.168.10.20:8123", fingerprint: "", account: "", domains: [], entities: [], policies: {}, mqtt },
        tokenSet: true, mqttPasswordSet: false, domains: [], sensorGroups: ["system"], commandGroups: [], mqttDevices: [],
        status: { watching: true, watchError: "", mqtt: true, mqttError: "" }, diagnostics: null } });
    });
    await page.route("**/api/v1/homeassistant/storage**", (r) => r.fulfill({ json: { account: "", server: "", entries: [], volumes: [], shares: [], homeAssistant: true } }));
    await page.goto(base + "/settings/homeassistant");
    const interval = page.getByLabel("Intervall (Sekunden)");
    await interval.fill("120");
    await page.locator('button[type="submit"][form="ha-form"]').click();
    for (let i = 0; i < 50 && !saved; i++) await page.waitForTimeout(100);
    assert.equal(saved?.mqtt.interval, 120);
    await interval.scrollIntoViewIfNeeded();
    await page.screenshot({ path: `${out}/ha-mqtt-interval-${name}.png`, animations: "disabled" });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log(`ok – screenshots in ${out}`);
} finally {
  await browser.close();
}
