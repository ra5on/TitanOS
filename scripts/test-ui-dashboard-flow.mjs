// Actual Svelte UI with isolated API data. Never contacts a NAS.
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
const errors = [],
  context = fixtureFactory(browser, errors),
  base = process.env.UI_URL || "http://127.0.0.1:5187",
  out = process.env.UI_OUTPUT || "out/validation/dashboard-flow";
await mkdir(out, { recursive: true });
const tile = (p, id) => p.locator(`[gs-id="${id}"]`);
const order = (p) =>
  p
    .locator(".dashboard-grid > [gs-id]")
    .evaluateAll((es) => es.map((e) => e.getAttribute("gs-id")));
const inspector = (p) => p.locator(".inspector");
async function noOverflow(page) {
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
    "page overflows",
  );
}
const vm = (name, i) => ({
  name,
  displayName: name,
  description: "",
  template: "linux",
  memoryMiB: 2048,
  vcpus: 2,
  volume: "Data",
  disks: [
    { file: name + ".qcow2", format: "qcow2", sizeBytes: 40e9, volume: "Data" },
  ],
  iso: "",
  autostart: true,
  cpuPin: [],
  exclusive: false,
  folders: [],
  tpm: false,
  secureBoot: false,
  firmware: "uefi",
  pci: [],
  usb: [],
  network: "bridge",
  mac: "52:54:00:00:00:0" + i,
  job: null,
  state: i === 4 ? "stopped" : "running",
  available: true,
  cpuPercent: 8,
  memoryBytes: 1e9,
  since: "",
});
try {
  const { ctx, page, data } = await context("owner", "graphit", 1600);
  data.system.inventory = {
    mainboard: "ASRock B560M Pro4",
    bios: "P2.30 · 12.09.2025",
  };
  data.system.cpu.threadUsage = Array.from({ length: 8 }, (_, i) => ({
    id: "cpu" + i,
    usagePercent: i * 3,
  }));
  data.system.accelerators = [
    {
      address: "0000:00:02.0",
      name: "Intel UHD Graphics",
      vendor: "Intel",
      kind: "gpu",
      state: "ready",
      driver: "i915",
      usagePercent: 100,
      temperatureC: 44,
      powerWatts: 6,
      memoryUsedBytes: 1e9,
      memoryTotalBytes: 4e9,
      frequencyMHz: 500,
      engines: [],
      history: [20, null, 100],
    },
    {
      address: "0000:00:04.0",
      name: "Intel AI Boost",
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
  const vms = [
    vm("Windows 11", 1),
    vm("Home Assistant OS", 2),
    vm("Debian", 3),
    vm("Testumgebung mit einem sehr langen Namen zur Layoutprüfung", 4),
  ];
  await ctx.route("**/api/v1/vms", (route) =>
    route.fulfill({
      json: {
        vms,
        kvm: true,
        bridge: true,
        host: {
          cpus: 8,
          memoryBytes: 32e9,
          usedMemoryBytes: 6e9,
          cpuModel: "Fixture",
        },
      },
    }),
  );
  await page.goto(base + "/");
  await tile(page, "vms")
    .getByRole("button", { name: "Alle VM-Details", exact: true })
    .waitFor();
  assert.deepEqual((await order(page)).slice(0, 6), [
    "system",
    "docker",
    "cpu",
    "vms",
    "memory",
    "volumes",
  ]);
  assert.ok(!(await tile(page, "vms").innerText()).match(/CPU|RAM/));
  assert.equal(
    await tile(page, "docker")
      .getByRole("button", { name: "photos", exact: true })
      .getAttribute("aria-expanded"),
    "false",
  );
  await tile(page, "system")
    .getByText("ASRock B560M Pro4", { exact: true })
    .waitFor();
  assert.equal(await tile(page, "cpu").locator(".core").count(), 8);
  const readings = await tile(page, "accelerators")
    .locator(".reading")
    .allTextContents();
  assert.deepEqual(readings, ["100%", "–%"]);
  for (const r of await tile(page, "accelerators").locator(".reading").all())
    assert.ok(await r.evaluate((e) => e.scrollWidth <= e.clientWidth + 1));
  await page.screenshot({ path: out + "/dashboard.png", fullPage: true });
  await tile(page, "vms")
    .getByRole("button", { name: "Alle VM-Details", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByText("2 vCPU", { exact: true })
    .first()
    .waitFor();
  assert.equal(
    await page.getByRole("dialog").locator(".vm-details-grid section").count(),
    4,
  );
  await page.screenshot({ path: out + "/vm-details.png", fullPage: true });
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Fertig", exact: true })
    .click();
  await tile(page, "docker")
    .getByRole("button", { name: "Aktionen: photos", exact: true })
    .click();
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "WebUI", exact: true })
    .waitFor();
  assert.equal(await page.getByRole("menu").locator("a").count(), 5);
  await page.waitForFunction(() =>
    document.querySelector("[role=menu]")?.matches(":popover-open"),
  );
  assert.ok(
    await page.getByRole("menu").evaluate((e) => {
      const r = e.getBoundingClientRect();
      return e.contains(document.elementFromPoint(r.x + 20, r.y + 20));
    }),
    "menu clipped by tile",
  );
  await page.screenshot({ path: out + "/docker-menu.png" });
  await page.keyboard.press("Escape");
  await tile(page, "vms")
    .getByRole("button", { name: "Aktionen: Windows 11", exact: true })
    .click();
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Herunterfahren", exact: true })
    .waitFor();
  const beforeActions = data.requests.filter((r) => r[0] === "POST").length;
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Herunterfahren", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Abbrechen", exact: true })
    .click();
  assert.equal(
    data.requests.filter((r) => r[0] === "POST").length,
    beforeActions,
  );
  await tile(page, "docker")
    .getByRole("button", { name: "Aktionen: photos", exact: true })
    .click();
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Als eigene Kachel anheften", exact: true })
    .click();
  await tile(page, "app:photos").waitFor();
  await page.getByRole("button", { name: "Abbrechen", exact: true }).click();
  assert.equal(await tile(page, "app:photos").count(), 0);
  await page.getByRole("button", { name: "Bearbeiten", exact: true }).click();
  await tile(page, "system")
    .getByRole("button", { name: "Kachel anpassen: System", exact: true })
    .click();
  await inspector(page)
    .getByRole("button", { name: "Inhalt", exact: true })
    .click();
  await inspector(page)
    .getByRole("checkbox", { name: "IPv4-Adresse", exact: true })
    .check();
  await inspector(page)
    .getByRole("checkbox", { name: "BIOS / UEFI", exact: true })
    .uncheck();
  await tile(page, "system")
    .getByText("192.168.1.20/24", { exact: true })
    .waitFor();
  assert.equal(
    await tile(page, "system")
      .getByText("P2.30 · 12.09.2025", { exact: true })
      .count(),
    0,
  );
  await page.screenshot({ path: out + "/editor.png", fullPage: true });

  for (const id of ["volumes", "shares"]) {
    await tile(page, id).locator(".tile-action").first().click();
    await inspector(page)
      .getByRole("button", { name: "Inhalt", exact: true })
      .click();
    await inspector(page)
      .getByRole("combobox", { name: "Liste", exact: true })
      .selectOption("grid");
    const cells = await tile(page, id)
      .locator("tbody tr")
      .evaluateAll((es) => es.map((e) => e.getBoundingClientRect().toJSON()));
    assert.equal(cells.length, 3);
    assert.ok(
      cells[1].x > cells[0].x && Math.abs(cells[1].y - cells[0].y) < 2,
      "grid option did not create columns",
    );
    await inspector(page)
      .getByRole("combobox", { name: "Liste", exact: true })
      .selectOption("list");
  }
  await inspector(page)
    .getByRole("checkbox", { name: "Zusatzwerte anzeigen", exact: true })
    .uncheck();
  assert.equal(await tile(page, "shares").locator("thead th").count(), 2);
  await inspector(page)
    .getByRole("checkbox", { name: "Zusatzwerte anzeigen", exact: true })
    .check();
  await inspector(page)
    .getByRole("button", { name: "Größe", exact: true })
    .click();
  await inspector(page)
    .getByRole("spinbutton", { name: "Höhe (px)", exact: true })
    .fill("280");
  await inspector(page)
    .getByRole("spinbutton", { name: "Höhe (px)", exact: true })
    .press("Tab");
  assert.equal(
    await inspector(page)
      .getByRole("checkbox", {
        name: "Höhe automatisch an Inhalt anpassen",
        exact: true,
      })
      .isChecked(),
    false,
  );
  assert.ok(
    Math.abs((await tile(page, "shares").boundingBox()).height - 280) < 1,
  );
  await inspector(page)
    .getByRole("checkbox", {
      name: "Höhe automatisch an Inhalt anpassen",
      exact: true,
    })
    .check();
  // Editing content selects styling and never starts operations or opens runtime menus.
  await tile(page, "vms")
    .getByRole("button", { name: "Aktionen: Windows 11", exact: true })
    .click();
  assert.equal(await page.getByRole("menu").count(), 0);
  const original = await order(page);
  await tile(page, "cpu").locator(".grip").focus();
  await page.keyboard.press("ArrowLeft");
  assert.notDeepEqual(await order(page), original);
  await page.getByRole("button", { name: "Rückgängig", exact: true }).click();
  assert.deepEqual(await order(page), original);
  await page
    .getByRole("combobox", { name: "Spalten", exact: true })
    .selectOption("3");
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  await page.getByRole("button", { name: "Bearbeiten", exact: true }).waitFor();
  assert.equal(data.prefs.dashboard.version, 5);
  assert.equal(data.prefs.dashboard.columns, 3);
  assert.ok(
    data.prefs.dashboard.tileOptions.system.systemFields.includes("ipv4"),
  );
  assert.ok(
    !data.prefs.dashboard.tileOptions.system.systemFields.includes("bios"),
  );
  const saved = structuredClone(data.prefs.dashboard);
  for (const width of [1600, 1024, 768, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.waitForTimeout(120);
    await noOverflow(page);
    assert.deepEqual(
      await order(page),
      saved.tiles.map((t) => t.id),
    );
    for (const name of await tile(page, "vms").locator(".entry-name").all())
      assert.ok(
        await name.evaluate((e) => e.scrollWidth <= e.clientWidth + 1),
        "VM name overflow",
      );
  }
  await page.screenshot({ path: out + "/mobile.png", fullPage: true });
  await page.getByRole("button", { name: "Bearbeiten", exact: true }).click();
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  await page.getByRole("button", { name: "Bearbeiten", exact: true }).waitFor();
  assert.deepEqual(
    data.prefs.dashboard,
    saved,
    "mobile save changed desktop preferences",
  );
  await page.reload();
  await tile(page, "system")
    .getByText("192.168.1.20/24", { exact: true })
    .waitFor();
  await page.setViewportSize({ width: 1600, height: 1000 });
  await page.getByRole("button", { name: "Bearbeiten", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Spalten", exact: true })
    .selectOption("4");
  await page.getByRole("button", { name: "Abbrechen", exact: true }).click();
  assert.equal(data.prefs.dashboard.columns, 3);
  await ctx.close();
  const regular = await context("user", "hell", 390, "en");
  regular.data.prefs.dashboard = {
    version: 4,
    tiles: [
      { id: "network", x: 7, y: 3, w: 5, h: 4 },
      { id: "system", x: 0, y: 0, w: 3, h: 2 },
    ],
    tileOptions: { network: { ipv6: false, interface: "br0", valueSize: 28 } },
  };
  await regular.page.goto(base + "/");
  await tile(regular.page, "network").waitFor();
  assert.deepEqual(await order(regular.page), ["system", "network"]);
  await regular.page.getByRole("button", { name: "Edit", exact: true }).click();
  await regular.page.getByRole("button", { name: "Save", exact: true }).click();
  await regular.page
    .getByRole("button", { name: "Edit", exact: true })
    .waitFor();
  assert.equal(
    regular.data.prefs.dashboard.tileOptions.network.interface,
    "br0",
  );
  assert.equal(regular.data.prefs.dashboard.tileOptions.network.valueSize, 28);
  assert.equal(regular.data.prefs.dashboard.tileOptions.network.ipv6, false);
  assert.equal(
    regular.data.requests.filter((r) => ["/docker", "/vms"].includes(r[1]))
      .length,
    0,
  );
  await regular.ctx.close();
  assert.deepEqual(errors, []);
  console.log(
    "Dashboard flow, menus, VM details, editor, responsive order and migration passed.",
  );
} finally {
  await browser.close();
}
