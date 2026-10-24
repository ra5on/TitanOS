// Production UI regression for the approved position grid. All NAS APIs are isolated fixtures.
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [],
  factory = fixtureFactory(browser, errors),
  checks = [];
const base = process.env.UI_URL || "http://127.0.0.1:5187",
  out = process.env.UI_OUTPUT || "out/validation/dashboard-grid";
await mkdir(out, { recursive: true });
const tile = (p, id) => p.locator(`[gs-id="${id}"]`);
const ready = async (p) => {
  await p.evaluate(() => document.fonts.ready);
  await p.waitForTimeout(150);
};
const open = async (p, id, name) => {
  await tile(p, id)
    .getByRole("button", { name: "Aktionen: " + name, exact: true })
    .click();
  await p.getByRole("menu").waitFor();
};
async function noOverlap(p) {
  const bad = await p.locator(".dashboard-grid > [gs-id]").evaluateAll((es) => {
    const rs = es.map((e) => ({
      id: e.getAttribute("gs-id"),
      r: e.getBoundingClientRect(),
    }));
    return rs.flatMap((a, i) =>
      rs
        .slice(i + 1)
        .filter(
          (b) =>
            Math.min(a.r.right, b.r.right) - Math.max(a.r.left, b.r.left) > 1 &&
            Math.min(a.r.bottom, b.r.bottom) - Math.max(a.r.top, b.r.top) > 1,
        )
        .map((b) => [a.id, b.id]),
    );
  });
  assert.deepEqual(bad, [], "tiles overlap");
}
try {
  const { ctx, page, data } = await factory("owner", "graphit", 1600);
  await ctx.route("**/api/v1/assistant/status", (r) =>
    r.fulfill({
      json: {
        available: true,
        enabled: true,
        model: "fixture",
        web: false,
        persistent: false,
      },
    }),
  );
  const vms = Array.from({ length: 4 }, (_, i) => ({
    name: "vm" + i,
    displayName:
      i === 2
        ? "A very long VM name that must remain inside its own entry"
        : `VM ${i}`,
    template: "linux",
    memoryMiB: 2048,
    vcpus: 2,
    volume: "Data",
    disks: [],
    firmware: "uefi",
    autostart: true,
    network: "bridge",
    mac: "52:54:00:00:00:01",
    available: true,
    job: null,
    state: i === 3 ? "stopped" : "running",
    cpuPercent: 8,
    memoryBytes: 1e9,
  }));
  let kvm = true;
  await ctx.route("**/api/v1/vms", (r) =>
    r.fulfill({
      json: {
        kvm,
        bridge: true,
        vms,
        host: { cpus: 8, memoryBytes: 16e9, usedMemoryBytes: 4e9 },
      },
    }),
  );
  await ctx.route("**/api/v1/vms/*/addresses", (r) =>
    r.fulfill({ json: { addresses: ["192.168.1.50"] } }),
  );
  const containers = Array.from({ length: 24 }, (_, i) => ({
    id: "c" + i,
    name: "Application-" + i,
    image: "fixture",
    state: i === 23 ? "exited" : "running",
    app: i < 2 ? "photos" : "",
    service: i === 0 ? "web" : i === 1 ? "db" : "",
    managed: i < 2,
    icon: i === 1 ? "" : "https://icons.example.test/app.svg",
    webui: i === 0 ? "2283" : "",
    publicUrl: i === 0 ? "https://photos.example.test" : "",
    cpuPercent: 1,
    memoryBytes: 1e8,
    ports: [],
    updateAvailable: false,
    updateState: "current",
  }));
  let failAction = false;
  const actions = [];
  await ctx.route("https://icons.example.test/**", (r) =>
    r.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><path fill="#6cb6ff" d="M2 2h28v28H2z"/></svg>',
    }),
  );
  await ctx.route("**/api/v1/docker", (r) =>
    r.fulfill({
      json: {
        settings: { enabled: true, volume: "Data" },
        running: true,
        containers,
        apps: ["photos"],
        jobs: {},
      },
    }),
  );
  await ctx.route("**/api/v1/docker/**", (r) => {
    const path = new URL(r.request().url()).pathname;
    if (r.request().method() === "GET") return r.fallback();
    actions.push([r.request().method(), path]);
    if (failAction)
      return r.fulfill({ status: 500, json: { error: { code: "internal" } } });
    if (path.endsWith("/updates/check"))
      return r.fulfill({ json: { containers } });
    return r.fulfill({ status: 204 });
  });
  data.shares = Array.from({ length: 20 }, (_, i) => ({
    ...data.shares[0],
    name: i ? "Share-" + i : "Documents",
  }));
  data.system.cpu.threadUsage = Array.from({ length: 16 }, (_, i) => ({
    id: "cpu" + i,
    usagePercent: i * 3,
  }));
  data.system.network = {
    ...data.system.network,
    interface: "eth0",
    speedMbps: 2500,
  };
  data.prefs.dashboard = {
    version: 5,
    columns: 2,
    minimumWidth: 350,
    tiles: [
      "system",
      "docker",
      "cpu",
      "vms",
      "memory",
      "volumes",
      "network",
      "shares",
      "container:Application-2",
      "vm:vm0",
    ].map((id) => ({
      id,
      w: 6,
      h: id === "system" ? 15 : 4,
      span: 1,
      autoHeight: id !== "system",
    })),
    tileOptions: {
      docker: { rows: 12 },
      shares: { rows: 12 },
      cpu: { labelSize: 28 },
    },
  };
  await page.goto(base);
  await tile(page, "docker").locator(".workload").first().waitFor();
  await ready(page);
  const sys = await tile(page, "system").boundingBox(),
    dock = await tile(page, "docker").boundingBox(),
    cpu = await tile(page, "cpu").boundingBox(),
    machines = await tile(page, "vms").boundingBox();
  assert.equal(Math.round(cpu.y - sys.y - sys.height), 12);
  assert.equal(Math.round(machines.y - dock.y - dock.height), 12);
  assert.ok(Math.abs(cpu.y - machines.y) > 40);
  assert.equal(await page.locator(".page-head").count(), 0);
  assert.ok(sys.y < 85);
  await noOverlap(page);
  assert.equal(await tile(page, "docker").locator(".workload").count(), 12);
  await tile(page, "docker")
    .getByRole("button", { name: "Alle anzeigen", exact: true })
    .click();
  assert.equal(await tile(page, "docker").locator(".workload").count(), 23);
  assert.equal(await tile(page, "shares").locator("tbody tr").count(), 12);
  await tile(page, "shares")
    .getByRole("button", { name: "Alle anzeigen", exact: true })
    .click();
  assert.equal(await tile(page, "shares").locator("tbody tr").count(), 20);
  assert.equal(
    await tile(page, "container:Application-2")
      .locator(".app-icon img")
      .count(),
    1,
  );
  await open(page, "container:Application-2", "Application-2");
  assert.equal(
    await page
      .getByRole("menu")
      .getByRole("menuitem", {
        name: "Als eigene Kachel anheften",
        exact: true,
      })
      .count(),
    0,
  );
  assert.equal(
    await page.getByRole("menu").locator(".menu-app-icon img").count(),
    1,
  );
  await page.keyboard.press("Escape");
  await tile(page, "docker")
    .getByRole("button", { name: "photos: Container anzeigen", exact: true })
    .click();
  assert.equal(
    await tile(page, "docker").locator(".member-icon img").count(),
    1,
  );
  assert.ok(
    await tile(page, "docker").locator(".member-icon").nth(1).innerText(),
  );
  await open(page, "docker", "Application-1");
  await page
    .getByRole("menu")
    .getByRole("menuitem", {
      name: "Gesamte Anwendung photos entfernen …",
      exact: true,
    })
    .click();
  await page
    .getByRole("dialog")
    .getByText(/Die gesamte Anwendung „photos“/)
    .waitFor();
  assert.ok(!/Application-1/.test(await page.getByRole("dialog").innerText()));
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Entfernen", exact: true })
    .click();
  assert.deepEqual(actions.at(-1), ["DELETE", "/api/v1/docker/apps/photos"]);
  await open(page, "docker", "Application-2");
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Nach Updates suchen", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByText("Application-2: Aktuell", { exact: true })
    .waitFor();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Fertig", exact: true })
    .click();
  failAction = true;
  await open(page, "docker", "Application-2");
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Stopp", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByText(/Aktion für „Application-2“ erneut/)
    .waitFor();
  assert.ok(!/Gast/.test(await page.getByRole("dialog").innerText()));
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Abbrechen", exact: true })
    .click();
  failAction = false;
  await open(page, "docker", "Application-2");
  containers[2].state = "exited";
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Start", exact: true })
    .waitFor({ timeout: 7500 });
  await page.keyboard.press("Escape");
  containers[2].state = "running";
  checks.push(
    "independent heights, count/show-all, real metadata icons, app removal scope, update feedback and action error",
  );
  await tile(page, "docker")
    .getByRole("button", { name: "photos: Container anzeigen", exact: true })
    .click();
  await ready(page);
  // Inline VM details stay under the selected entry, without shifting the other lane's next row.
  const nextBefore = await tile(page, "vms")
    .locator('[data-position="vm3"]')
    .boundingBox();
  await open(page, "vms", "VM 1");
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Details anzeigen", exact: true })
    .click();
  await tile(page, "vms")
    .locator(".vm-inline")
    .getByText("192.168.1.50", { exact: true })
    .waitFor();
  await ready(page);
  assert.ok(
    await tile(page, "vms")
      .locator(".vm-inline dt,.vm-inline dd")
      .evaluateAll((es) => es.every((e) => e.scrollWidth <= e.clientWidth + 1)),
    "inline VM details overflow",
  );
  assert.equal(await tile(page, "vms").locator(".vm-inline").count(), 1);
  assert.equal(
    await tile(page, "vms")
      .locator(".vm-inline")
      .evaluate((e) => e.parentElement.dataset.position),
    "vm1",
  );
  const nextAfter = await tile(page, "vms")
    .locator('[data-position="vm3"]')
    .boundingBox();
  assert.equal(nextAfter.y, nextBefore.y);
  await page.screenshot({ path: out + "/inline-details.png", fullPage: true });
  await open(page, "vms", "VM 0");
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Details anzeigen", exact: true })
    .click();
  assert.equal(
    await tile(page, "vms")
      .locator(".vm-inline")
      .evaluate((e) => e.parentElement.dataset.position),
    "vm0",
  );
  await tile(page, "vms")
    .getByRole("button", { name: "VM-Details schließen", exact: true })
    .click();
  await open(page, "vm:vm0", "VM 0");
  assert.equal(
    await page
      .getByRole("menu")
      .getByRole("menuitem", {
        name: "Als eigene Kachel anheften",
        exact: true,
      })
      .count(),
    0,
  );
  await page.keyboard.press("Escape");
  // Backend capabilities govern the start action as on the VM page.
  kvm = false;
  await page.reload();
  await open(page, "vms", "VM 3");
  assert.equal(
    await page
      .getByRole("menu")
      .getByRole("menuitem", { name: "Starten", exact: true })
      .isDisabled(),
    true,
  );
  await page.keyboard.press("Escape");
  kvm = true;
  // Correct deep-link destinations, not just the generated hrefs.
  await open(page, "shares", "Documents");
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Zugriffsrechte", exact: true })
    .click();
  await page.getByRole("tab", { name: "Zugriff", exact: true }).waitFor();
  assert.equal(
    await page
      .getByRole("tab", { name: "Zugriff", exact: true })
      .getAttribute("aria-selected"),
    "true",
  );
  await page.goto(base);
  await open(page, "volumes", "Data");
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Laufwerke und SMART", exact: true })
    .click();
  await page.waitForURL("**/storage?tab=disks&volume=Data");
  assert.ok(new URL(page.url()).searchParams.get("volume") === "Data");
  await page.getByRole("button", { name: /^Laufwerke/ }).waitFor();
  assert.equal(
    await page
      .getByRole("button", { name: /^Laufwerke/ })
      .getAttribute("aria-pressed"),
    "true",
  );
  assert.equal(
    await page
      .getByRole("combobox", { name: "Volume", exact: true })
      .inputValue(),
    "Data",
  );
  await page.goto(base);
  await tile(page, "docker").locator(".workload").first().waitFor();
  checks.push(
    "inline VM details, capability checks, share access tab and filtered volume drives",
  );
  // Responsive reading order and large thread text never collide or overflow.
  for (const width of [1600, 1024, 768, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await ready(page);
    await noOverlap(page);
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
      "page overflow " + width,
    );
    const overlaps = await tile(page, "cpu")
      .locator(".core")
      .evaluateAll((es) =>
        es.flatMap((e) => {
          const cs = [...e.children].map((n) => n.getBoundingClientRect());
          return cs.slice(1).filter((r, i) => r.left < cs[i].right - 1);
        }),
      );
    assert.equal(overlaps.length, 0, "CPU overlap " + width);
  }
  await page.screenshot({ path: out + "/mobile.png", fullPage: true });
  await page.setViewportSize({ width: 1600, height: 1000 });
  await ready(page);
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .click();
  await ready(page);
  const grip = tile(page, "system").locator(".grip"),
    start = await grip.boundingBox();
  await page.mouse.move(start.x + start.width / 2, start.y + start.height / 2);
  await page.mouse.down();
  await page.mouse.move(
    start.x + start.width / 2 + 780,
    start.y + start.height / 2 + 300,
    { steps: 8 },
  );
  await page.locator(".placement-placeholder").waitFor();
  await page.screenshot({ path: out + "/drag-grid.png", fullPage: true });
  await page.mouse.up();
  await ready(page);
  await noOverlap(page);
  const resize = tile(page, "system").locator(".resize-handle");
  await resize.scrollIntoViewIfNeeded();
  const sizeBefore = (await tile(page, "system").boundingBox()).height,
    handle = await resize.boundingBox();
  await page.mouse.move(
    handle.x + handle.width / 2,
    handle.y + handle.height / 2,
  );
  await page.mouse.down();
  await page.mouse.move(
    handle.x + handle.width / 2,
    handle.y + handle.height / 2 + 80,
    { steps: 4 },
  );
  await page.locator(".placement-placeholder").waitFor();
  assert.ok((await tile(page, "system").boundingBox()).height > sizeBefore);
  await page.mouse.up();
  await ready(page);
  assert.equal(
    (await tile(page, "system").boundingBox()).height,
    sizeBefore + 80,
  );
  await noOverlap(page);
  await page.getByRole("button", { name: "Rückgängig", exact: true }).click();
  await ready(page);
  assert.equal((await tile(page, "system").boundingBox()).height, sizeBefore);
  await page.getByRole("button", { name: "Wiederholen", exact: true }).click();
  await ready(page);
  assert.equal(
    (await tile(page, "system").boundingBox()).height,
    sizeBefore + 80,
  );
  await tile(page, "system")
    .getByRole("button", { name: "Position und Größe sperren", exact: true })
    .click();
  assert.equal(await tile(page, "system").locator(".grip").isDisabled(), true);
  assert.equal(
    await tile(page, "system").locator(".resize-handle").isDisabled(),
    true,
  );
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .waitFor();
  assert.equal(data.prefs.dashboard.version, 6);
  assert.equal(
    data.prefs.dashboard.tiles.find((p) => p.id === "system").column,
    1,
  );
  assert.equal(
    data.prefs.dashboard.tiles.find((p) => p.id === "system").locked,
    true,
  );
  const saved = structuredClone(data.prefs.dashboard);
  await page.reload();
  await tile(page, "docker").locator(".workload").first().waitFor();
  await ready(page);
  assert.deepEqual(data.prefs.dashboard, saved);

  await tile(page, "system")
    .getByRole("button", { name: /einklappen/ })
    .click();
  await ready(page);
  assert.equal(await tile(page, "system").locator(".tile-body").count(), 0);
  assert.equal(data.prefs.dashboard.tileOptions.system.collapsed, true);
  await page.reload();
  await tile(page, "system")
    .getByRole("button", { name: /ausklappen/ })
    .click();
  await tile(page, "system").locator(".tile-body").waitFor();
  await ready(page);
  await noOverlap(page);
  await page.screenshot({ path: out + "/dashboard.png", fullPage: true });
  checks.push(
    "320–1600px, large typography, pointer grid, position locks, persisted coordinates, collapse/reload",
  );
  await ctx.close();
  assert.deepEqual(errors, []);
  console.log(checks.join("\n"));
} finally {
  await writeFile(
    out + "/results.json",
    JSON.stringify({ checks, errors }, null, 2),
  );
  await browser.close();
}
