// Capture the built WebUI with isolated demonstration data. See README.md in this folder.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { fixtureFactory } from "../../scripts/test-ui-fixtures.mjs";
const errors = [],
  out = process.env.UI_OUTPUT || "docs/screenshots/current";
await mkdir(out, { recursive: true });
const iconDir = (process.env.UI_ICON_DIR || "out/readme-icons") + "/";
await mkdir(iconDir, { recursive: true });
const iconSources = JSON.parse(
  await readFile(new URL("./icon-sources.json", import.meta.url), "utf8"),
);
for (const icon of iconSources) {
  let bytes;
  try {
    bytes = await readFile(iconDir + icon.file);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    const response = await fetch(icon.source);
    assert.ok(response.ok, "Icon download failed: " + icon.name);
    bytes = Buffer.from(await response.arrayBuffer());
  }
  assert.equal(
    createHash("sha256").update(bytes).digest("hex"),
    icon.sha256,
    "Original logo changed; review icon-sources.json: " + icon.name,
  );
  await writeFile(iconDir + icon.file, bytes);
}
const base = process.env.UI_URL || "http://127.0.0.1:5187";
assert.ok(
  ["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname),
  "Use a local UI preview, never a live NAS",
);
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const { ctx, page, data } = await fixtureFactory(browser, errors)(
  "owner",
  "graphit",
  1600,
);
await ctx.route("**/*", (r) =>
  new URL(r.request().url()).origin === new URL(base).origin
    ? r.fallback()
    : r.abort(),
);
await page.clock.setFixedTime(new Date("2026-10-24T15:40:00+02:00"));
const apps = [
  ["immich", "immich.svg", 3],
  ["AdGuard Home", "adguard-home.png"],
  ["Jellyfin", "jellyfin.svg"],
  ["Home Assistant", "home-assistant.svg"],
  ["paperless", "paperless.svg", 2],
  ["Nextcloud", "nextcloud.png"],
  ["Grafana", "grafana.svg"],
  ["Gitea", "gitea.svg"],
  ["Uptime Kuma", "uptime-kuma.svg"],
  ["MQTT", "mqtt.svg"],
  ["Syncthing", "syncthing.png"],
  ["Vaultwarden", "vaultwarden.svg"],
  ["SFTPGo", "sftpgo.png"],
  ["Testserver", ""],
];
const containers = apps.flatMap(([name, file, count = 1], i) =>
  Array.from({ length: count }, (_, j) => ({
    id: "c" + i + "-" + j,
    name: count > 1 ? name + "-" + ["web", "db", "ml"][j] + "-1" : name,
    image: name.toLowerCase().replaceAll(" ", "-") + ":latest",
    state: i === 13 ? "exited" : "running",
    app: count > 1 ? name : "",
    service: count > 1 ? ["web", "db", "ml"][j] : "",
    managed: count > 1,
    icon:
      file && (j === 0 || !count) ? "https://icons.example.test/" + file : "",
    webui: j === 0 ? "8096" : "",
    publicUrl: i === 2 ? "https://jellyfin.example.test" : "",
    ports: [],
    cpuPercent: 1,
    memoryBytes: 1e8,
    cpuHistory: [2, 3, 2, 4, 1],
    updateState: "current",
    updateAvailable: false,
  })),
);
await ctx.route("https://icons.example.test/**", async (r) => {
  const file = new URL(r.request().url()).pathname.slice(1);
  assert.ok(!file.includes("/"));
  await r.fulfill({
    body: await readFile(iconDir + file),
    contentType: file.endsWith(".svg") ? "image/svg+xml" : "image/png",
  });
});
await ctx.route("**/api/v1/docker", (r) =>
  r.fulfill({
    json: {
      settings: { enabled: true, volume: "data" },
      running: true,
      version: "28.5.1",
      containers,
      apps: ["immich", "paperless"],
      jobs: {},
    },
  }),
);
const vms = ["Windows 11", "Home Assistant OS", "Debian", "Testumgebung"].map(
  (displayName, i) => ({
    name: "vm" + i,
    displayName,
    state: i === 3 ? "stopped" : "running",
    template: i === 0 ? "windows" : "linux",
    memoryMiB: [8192, 4096, 2048, 2048][i],
    vcpus: 2,
    volume: "data",
    disks: [{ file: "disk.qcow2", format: "qcow2", sizeBytes: 40e9 }],
    firmware: "uefi",
    autostart: true,
    available: true,
    job: null,
    network: "bridge",
    mac: "52:54:00:00:00:01",
    cpuPercent: 8,
    memoryBytes: 2e9,
  }),
);
await ctx.route("**/api/v1/vms", (r) =>
  r.fulfill({
    json: {
      kvm: true,
      bridge: true,
      vms,
      host: { cpus: 16, memoryBytes: 32e9, usedMemoryBytes: 4e9 },
    },
  }),
);
await ctx.route("**/api/v1/vms/*/addresses", (r) =>
  r.fulfill({ json: { addresses: ["192.168.1.55"] } }),
);
const volumes = [
  ["data", "raid5", 12e12, 7.6e12],
  ["fast", "raid1", 2e12, 560e9],
  ["backup", "btrfs1", 4e12, 1.6e12],
].map(([name, level, sizeBytes, usedBytes]) => ({
  name,
  level: level === "btrfs1" ? "raid1" : level,
  filesystem: name === "backup" ? "btrfs" : "ext4",
  mountPoint: "/mnt/" + name,
  mounted: true,
  state: "ok",
  sizeBytes,
  usedBytes,
  raidDisks: 2,
  members: [],
  sync: null,
  mismatches: 0,
}));
const disks = Array.from({ length: 8 }, (_, i) => ({
  name: "sd" + String.fromCharCode(97 + i),
  path: "/dev/sd" + String.fromCharCode(97 + i),
  identity: "demo-disk-" + i,
  serial: "DEMO-20" + String(i + 1).padStart(2, "0"),
  model: i < 4 ? "NAS HDD 4 TB" : i < 6 ? "NAS SSD 2 TB" : "NAS HDD 4 TB",
  sizeBytes: i < 4 || i >= 6 ? 4e12 : 2e12,
  kind: i >= 4 && i < 6 ? "ssd" : "hdd",
  role: "member",
  volume: i < 4 ? "data" : i < 6 ? "fast" : "backup",
  memberState: "active",
  mounted: true,
  testing: false,
  smart: {
    healthy: true,
    temperatureC: 31 + i,
    powerOnHours: 4200 + i * 25,
    errors: 0,
    rotationRate: i >= 4 && i < 6 ? 0 : 5400,
    wearPercent: i >= 4 && i < 6 ? 2 : 0,
  },
}));
for (const v of volumes) {
  v.members = disks
    .filter((d) => d.volume === v.name)
    .map((d) => ({ disk: d.name, state: "active" }));
  v.raidDisks = v.members.length;
}
await ctx.route("**/api/v1/storage", (r) =>
  r.fulfill({
    json: {
      volumes,
      disks,
      diskSleep: { idleMinutes: 0, available: true, errors: [] },
    },
  }),
);
data.shares = ["Dokumente", "Medien", "Fotos", "Projekte"].map((name, i) => ({
  ...data.shares[0],
  name,
  volume: i === 3 ? "fast" : "data",
  path: "/mnt/" + (i === 3 ? "fast" : "data") + "/" + name,
  nfs: { enabled: i === 1, clients: [] },
}));
data.system = {
  ...data.system,
  hostname: "titan-nas",
  version: "0.20.1",
  kernel: "6.12.113+deb13-amd64",
  uptimeSeconds: 20 * 3600 + 41 * 60,
  inventory: { mainboard: "ASRock B560M Pro4", bios: "P2.30 · 12.09.2025" },
};
data.system.cpu = {
  model: "Intel Core i7-11700",
  threads: 16,
  usagePercent: 12,
  threadUsage: Array.from({ length: 16 }, (_, i) => ({
    id: "cpu" + i,
    usagePercent: [8, 15, 6, 19, 11, 4, 21, 12, 10, 8, 4, 16, 7, 2, 13, 6][i],
  })),
};
data.system.memory = {
  totalBytes: 32 * 1024 ** 3,
  usedBytes: 8.2 * 1024 ** 3,
  availableBytes: 23.8 * 1024 ** 3,
  cacheBytes: 4.6 * 1024 ** 3,
};
data.system.network = {
  interface: "eth0",
  speedMbps: 2500,
  ipv4: "192.168.1.20",
  ipv4Addresses: ["192.168.1.20"],
  ipv6Addresses: [],
  rxBytesPerSecond: 84.2e6,
  txBytesPerSecond: 12.8e6,
};
data.system.networks = [data.system.network];
data.system.accelerators = [
  ["Intel UHD 750", "Intel", "gpu", 36, 48],
  ["NVIDIA RTX 4060", "NVIDIA", "gpu", 27, 39],
].map(([name, vendor, kind, usagePercent, temperatureC], i) => ({
  address: `0000:0${i}:02.0`,
  name,
  vendor,
  kind,
  state: "ready",
  driver: ["i915", "nvidia", "ivpu"][i],
  usagePercent,
  temperatureC,
  powerWatts: i === 1 ? 22 : null,
  memoryUsedBytes: null,
  memoryTotalBytes: null,
  frequencyMHz: null,
  engines: [],
  history: [],
}));
data.system.temperatures = ["CPU-Paket", "NVMe", "Mainboard"].map(
  (label, i) => ({
    id: String(i + 1).repeat(32),
    chip: i ? "board" : "coretemp",
    label: i ? label : "Package id 0",
    device: "/sys/test",
    celsius: [42, 37, 33][i],
    criticalC: 100,
  }),
);
data.prefs.dashboard = {
  version: 6,
  visual: {
    transparency: 48,
    blur: 14,
    glow: 18,
    dim: 15,
    motion: true,
    duration: 450,
    images: [],
    background: {
      source: "builtin",
      id: "nordlicht",
      fit: "contain",
      x: 50,
      y: 50,
      zoom: 100,
      fixed: true,
    },
  },
  columns: 2,
  minimumWidth: 350,
  tiles: [
    "system",
    "docker",
    "cpu",
    "vms",
    "memory",
    "volumes",
    "accelerators",
    "shares",
    "network",
    "temperatures",
  ].map((id) => ({ id, w: 6, h: 2, span: 1, autoHeight: true })),
  temperatureSensors: data.system.temperatures.map((s, i) => ({
    id: s.id,
    chip: s.chip,
    label: ["CPU-Paket", "NVMe", "Mainboard"][i],
  })),
  tileOptions: { network: { ipv6: false }, temperatures: { details: false } },
};
for (const [i, v] of vms.entries())
  Object.assign(v, {
    description: i === 1 ? "Smart Home und Automationen" : "",
    exclusive: false,
    folders: [],
    tpm: i === 0,
    secureBoot: i === 0,
    pci: [],
    usb: [],
    cpuPin: [],
    since: "2026-10-24T09:15:00Z",
    iso: "",
    volume: "fast",
    mac: "52:54:00:00:00:0" + (i + 1),
    disks: [
      {
        file: "vdisk1.qcow2",
        format: "qcow2",
        sizeBytes: 40 * 1024 ** 3,
        volume: "fast",
      },
    ],
  });
await ctx.route("**/api/v1/hardware", (r) =>
  r.fulfill({
    json: { hardware: { iommu: true, pci: [], usb: [] }, usedBy: {} },
  }),
);
await ctx.route("**/api/v1/vms/media", (r) =>
  r.fulfill({ json: { media: [] } }),
);
await ctx.route("**/api/v1/files/roots", (r) =>
  r.fulfill({
    json: data.shares.map((share) => {
      const v = volumes.find((v) => v.name === share.volume);
      return {
        id: "share/" + share.name,
        name: share.name,
        kind: "share",
        writable: true,
        recycle: true,
        totalBytes: v.sizeBytes,
        freeBytes: v.sizeBytes - v.usedBytes,
      };
    }),
  }),
);
await ctx.route("**/api/v1/files/list?*", (r) =>
  r.fulfill({
    json: [
      ...["Finanzen", "Fotos", "Projekte", "Verträge"].map((name) => ({
        name,
        dir: true,
        size: 0,
        modified: "2026-10-24T08:30:00Z",
      })),
      ...[
        ["Haushaltsplan.xlsx", 28672],
        ["Netzwerkplan.pdf", 1245184],
        ["Notizen.md", 4096],
        ["Sicherungskonzept.docx", 98304],
        ["Urlaubsplanung.pdf", 2678784],
      ].map(([name, size]) => ({
        name,
        dir: false,
        size,
        modified: "2026-10-23T16:20:00Z",
      })),
    ],
  }),
);
async function shot(name, fullPage = true) {
  await page.evaluate(() => document.fonts.ready);
  assert.ok(
    await page.evaluate(() =>
      ["IBM Plex Sans", "IBM Plex Mono"].every((family) =>
        document.fonts.check('14px "' + family + '"'),
      ),
    ),
    "Local UI fonts must be loaded",
  );
  await page.evaluate(() =>
    Promise.all([...document.images].map((i) => i.decode().catch(() => {}))),
  );
  await page.waitForTimeout(200);
  await page.screenshot({ path: out + "/" + name + ".png", fullPage });
}
try {
  await page.goto(base + "/");
  await page
    .getByRole("button", { name: "Aktionen: Jellyfin", exact: true })
    .first()
    .waitFor();
  await shot("dashboard");
  assert.ok(
    await page
      .locator("[gs-id=docker] img")
      .evaluateAll(
        (es) =>
          es.length === 13 && es.every((e) => e.complete && e.naturalWidth > 0),
      ),
  );
  await page
    .locator("[gs-id=vms]")
    .getByRole("button", { name: "Aktionen: Home Assistant OS", exact: true })
    .click();
  await page
    .getByRole("menu")
    .getByRole("menuitem", { name: "Details anzeigen", exact: true })
    .click();
  await page
    .locator(".vm-inline")
    .getByText("192.168.1.55", { exact: true })
    .waitFor();
  await shot("dashboard-vm-details");
  await page
    .getByRole("button", { name: "VM-Details schließen", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .click();
  await page
    .getByRole("complementary", { name: "Gesamtes Dashboard", exact: true })
    .waitFor();
  await shot("dashboard-editor");
  await page
    .getByRole("button", {
      name: "Bild auswählen oder hinzufügen …",
      exact: true,
    })
    .click();
  await page
    .getByRole("dialog", { name: "Hintergrund auswählen", exact: true })
    .waitFor();
  await shot("dashboard-hintergruende");
  await page
    .getByRole("button", { name: "Eigenes Bild hinzufügen", exact: true })
    .click();
  await page
    .locator("input[type=file]")
    .setInputFiles("web/static/backgrounds/bergsee.webp");
  await page.locator(".personal-image").waitFor();
  await shot("dashboard-bild-upload");
  await page
    .getByRole("button", { name: "Abbrechen", exact: true })
    .last()
    .click();
  await page.getByRole("button", { name: "Abbrechen", exact: true }).click();
  await page.setViewportSize({ width: 1600, height: 780 });
  await page.goto(base + "/storage#volume-data");
  await page.locator("article#volume-data").waitFor();
  await shot("speicher");
  await page.goto(base + "/shares");
  await page.getByRole("button", { name: "Dokumente", exact: true }).waitFor();
  await shot("shares");
  await page.setViewportSize({ width: 1600, height: 1000 });
  await page.goto(base + "/files?root=share%2FDokumente");
  await page.getByText("Sicherungskonzept.docx", { exact: true }).waitFor();
  await shot("dateien");
  await page.goto(base + "/docker");
  await page.getByText("Jellyfin", { exact: true }).first().waitFor();
  await shot("docker");
  await page.setViewportSize({ width: 1600, height: 800 });
  await page.goto(base + "/vms?vm=vm1&tab=overview");
  await page.locator("#vm-title").waitFor();
  await shot("vms");
  await page.setViewportSize({ width: 1600, height: 1000 });
  await page.goto(base + "/settings");
  await page.locator(".settings-group").first().waitFor();
  await shot("einstellungen");
  await page.goto(base + "/files?root=share%2FDokumente");
  await page.getByText("Sicherungskonzept.docx", { exact: true }).waitFor();
  await page.setViewportSize({ width: 390, height: 844 });
  await shot("dateien-mobil", false);
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  );
  await page.goto(base + "/");
  await page.locator("[gs-id=system]").waitFor();
  await shot("dashboard-mobil", false);
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  );
  assert.deepEqual(errors, []);
  const names = [
    "dashboard",
    "dashboard-editor",
    "dashboard-hintergruende",
    "dashboard-bild-upload",
    "dashboard-vm-details",
    "speicher",
    "shares",
    "dateien",
    "docker",
    "vms",
    "einstellungen",
    "dateien-mobil",
    "dashboard-mobil",
  ];
  const images = [];
  for (const name of names) {
    const bytes = await readFile(out + "/" + name + ".png");
    images.push({
      file: name + ".png",
      width: bytes.readUInt32BE(16),
      height: bytes.readUInt32BE(20),
      bytes: bytes.length,
      sha256: createHash("sha256").update(bytes).digest("hex"),
    });
  }
  await writeFile(
    out + "/capture.json",
    JSON.stringify(
      {
        source: execFileSync(
          "git",
          ["rev-parse", process.env.UI_SOURCE_REF || "HEAD"],
          { encoding: "utf8" },
        ).trim(),
        version: data.system.version,
        captured: "2026-10-24",
        demoData: true,
        theme: "Graphit / Blau / Glas",
        browserErrors: errors,
        loadedAppLogos: 13,
        images,
      },
      null,
      2,
    ) + "\n",
  );
  console.log(
    "13 current WebUI screenshots captured; fonts, Docker logos, mobile width and browser runtime checked.",
  );
} catch (error) {
  await page.screenshot({
    path: "out/readme-capture-error.png",
    fullPage: true,
  });
  console.error(errors);
  console.error((await page.locator("body").innerText()).slice(0, 9000));
  throw error;
} finally {
  await browser.close();
}
