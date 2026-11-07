// Danger zones (plan 10a): on every detail or edit view with a destroying
// action, the action stands in the "Gefahrenzone" at the end of the view,
// not between the normal buttons. Each zone button opens its confirmation
// dialog, which is cancelled; no destroying request is sent. Screenshots
// per page at 1440 and 390 px (graphit) and 1440 px (hell).
// Isolated API data only (test-ui-fixtures.mjs plus the answers below); no
// request reaches a NAS.
//
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5190 node scripts/test-ui-danger-zones.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5190";
const out = process.env.UI_OUTPUT || "out/validation/ui-danger-zones";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const time = "2026-10-23T16:00:00Z";

const lxcList = {
  installed: true,
  dev: false,
  host: { cpus: 8, memoryBytes: 16e9 },
  bridge: true,
  networks: [],
  containers: [
    {
      name: "web",
      displayName: "Webserver",
      volume: "Data",
      distribution: "debian",
      release: "trixie",
      arch: "amd64",
      network: "bridge",
      mac: "52:54:00:00:00:10",
      mounts: [],
      autostart: true,
      btrfs: false,
      created: time,
      state: "stopped",
      available: true,
      cpuPercent: 0,
      memoryBytes: 0,
    },
  ],
};

const plugins = {
  volume: "Data",
  kernel: "test",
  plugins: [
    {
      name: "werkzeug",
      title: "Werkzeug",
      description: { de: "Ein Test-Plugin", en: "A test plugin" },
      version: "1.0",
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
      update: null,
    },
  ],
  catalog: { release: "0.24.0", state: "ready" },
  driverUpdate: null,
};

const backups = [
  {
    job: {
      id: "nightly",
      sources: ["Documents"],
      applications: [],
      enabled: true,
      every: "daily",
      time: "03:00",
      keep: { daily: 7, weekly: 4, monthly: 6 },
      destination: { type: "local", volume: "Data", folder: "restic" },
    },
    stateKnown: true,
    state: { running: false, phase: "" },
  },
];

// Settings version 2 of the firewall: built-in zones plus one own zone.
function firewall() {
  const local = { home: true, vpn: true, internet: false };
  const services = [
    {
      id: "webui",
      ports: [{ protocol: "tcp", ports: ["80", "443"] }],
      default: local,
    },
    { id: "ping", ports: [{ protocol: "icmp", ports: null }], default: local },
  ];
  const builtin = (id) => ({ id, builtin: true, networks: [], sources: [] });
  const zones = [
    builtin("home"),
    builtin("vpn"),
    builtin("internet"),
    builtin("docker"),
    { id: "zlab", name: "Labor", networks: ["net:lab"], sources: [] },
  ];
  const cells = [];
  for (const from of zones.map((z) => z.id))
    for (const to of ["nas", "apps", ...zones.map((z) => z.id)])
      cells.push({
        from,
        to,
        default: to === "nas" ? "deny" : "allow",
        policies: [],
      });
  return {
    config: {
      enabled: false,
      zones,
      cells,
      autoBlock: {
        enabled: false,
        attempts: 10,
        within: 5,
        minutes: 60,
        permanent: false,
        exemptLocal: true,
        exempt: [],
      },
    },
    pending: null,
    pendingSeconds: 0,
    active: false,
    services,
    apps: [],
    homeNetworks: ["192.168.1.0/24"],
    vpnNetworks: ["100.64.0.0/10"],
    networks: [
      {
        id: "net:lan",
        kind: "network",
        name: "LAN",
        device: "br0",
        subnets: ["192.168.1.0/24"],
      },
      {
        id: "net:lab",
        kind: "network",
        name: "Labor",
        device: "tvlab",
        subnets: ["192.168.40.0/24"],
      },
    ],
    clientAddress: "192.168.1.50",
    clientZone: "home",
    blocked: [],
    recent: [],
  };
}

function cluster() {
  const node = (id, name) => ({
    id,
    name,
    addresses: [`192.168.1.${id === "n1" ? 20 : 30}:443`],
    ways: {},
    extra: [],
    cert: "a".repeat(64),
    joined: time,
    updated: time,
  });
  const status = (name) => ({
    name,
    hostname: name,
    version: "0.44.0",
    protocol: 1,
    uptimeSeconds: 86400,
    cpuPercent: 8,
    memory: { totalBytes: 32e9, availableBytes: 20e9 },
    volumes: [],
    warnings: 0,
    errors: 0,
  });
  const nodes = [node("n1", "titan"), node("n2", "thor")];
  return {
    cluster: {
      self: "n1",
      name: "Zuhause",
      internet: false,
      nodes,
      admin: "n1",
      manages: true,
      extra: [],
      found: ["192.168.1.20:443"],
      foundWays: { "192.168.1.20:443": "home" },
      ways: {},
      routes: {},
      tunnel: { running: false },
      version: "0.44.0",
      protocol: 1,
    },
    nodes: [
      {
        ...nodes[0],
        self: true,
        online: true,
        compatible: true,
        status: status("titan"),
      },
      {
        ...nodes[1],
        self: false,
        online: true,
        compatible: true,
        status: status("thor"),
        address: "192.168.1.30:443",
        way: "home",
        millis: 2,
      },
    ],
  };
}

const compose = `services:
  web:
    image: nginx:1.27
    restart: unless-stopped
    ports:
      - "8080:80"
`;

// Each page: where it is, what makes the zone appear, the destroying
// buttons in the zone and the title of the dialog each one opens.
const pages = [
  {
    id: "storage",
    path: "/storage",
    prepare: async (page) => {
      await page.getByRole("button", { name: /^Data/ }).first().click();
      await page.getByRole("heading", { name: "Details" }).waitFor();
    },
    buttons: [["Volume löschen", /Data/]],
  },
  {
    id: "vms",
    path: "/vms?vm=source&tab=overview",
    buttons: [["Entfernen …", /entfernen/i]],
  },
  {
    id: "lxc",
    path: "/lxc?ct=web",
    buttons: [["Entfernen", "Container entfernen"]],
  },
  {
    id: "docker-app",
    path: "/docker/apps/photos",
    buttons: [["Entfernen …", "photos entfernen?"]],
  },
  {
    id: "plugins",
    path: "/plugins",
    buttons: [["Entfernen …", "Werkzeug entfernen?"]],
  },
  {
    id: "firewall-zone",
    path: "/settings/firewall?tab=zones",
    prepare: async (page) => {
      await page
        .getByRole("button", { name: "Bearbeiten", exact: true })
        .first()
        .click();
      await page.getByRole("dialog").waitFor();
    },
    scope: (page) => page.getByRole("dialog"),
    buttons: [["Zone entfernen", "Zone entfernen"]],
  },
  {
    id: "backups",
    path: "/settings/backups/data",
    buttons: [["nightly entfernen", /nightly entfernen/]],
  },
  {
    id: "cluster",
    path: "/settings/cluster",
    buttons: [["Cluster verlassen", /verlassen/]],
  },
];

async function routes(ctx, data) {
  data.plugins = structuredClone(plugins);
  data.backups = structuredClone(backups);
  await ctx.route("**/api/v1/lxc", (r) => r.fulfill({ json: lxcList }));
  await ctx.route("**/api/v1/lxc/web/config", (r) =>
    r.fulfill({ json: { config: "" } }),
  );
  await ctx.route("**/api/v1/docker/apps/photos", (r) =>
    r.fulfill({ json: { compose } }),
  );
  await ctx.route("**/api/v1/firewall", (r) => r.fulfill({ json: firewall() }));
  await ctx.route("**/api/v1/cluster", (r) => r.fulfill({ json: cluster() }));
  await ctx.route("**/api/v1/cluster/nodes", (r) =>
    r.fulfill({
      json: {
        manages: true,
        version: "0.44.0",
        nodes: [
          {
            id: "n1",
            name: "titan",
            self: true,
            online: true,
            version: "0.44.0",
            compatible: true,
          },
        ],
      },
    }),
  );
}

const destroying = (data) =>
  data.requests.filter(([method]) => method === "DELETE");
const overflow = (page) =>
  page.evaluate(() => document.documentElement.scrollWidth - innerWidth);

try {
  for (const [theme, width] of [
    ["graphit", 1440],
    ["graphit", 390],
    ["hell", 1440],
  ]) {
    for (const entry of pages) {
      const { ctx, page, data } = await create("owner", theme, width);
      await routes(ctx, data);
      await page.goto(base + entry.path);
      if (entry.prepare) await entry.prepare(page);
      const scope = entry.scope ? entry.scope(page) : page;
      const zone = scope.getByRole("region", {
        name: "Gefahrenzone",
        exact: true,
      });
      await zone.first().waitFor();
      assert.equal(await zone.count(), 1, `${entry.id}: one danger zone`);
      // The zone ends the view: no other button of its container follows it
      // (the dialog's own Cancel/Apply row aside).
      const after = await zone.evaluate((el) =>
        [
          ...(
            el.parentElement.closest("section, article, aside, form") ??
            el.parentElement
          ).querySelectorAll("button, a.btn"),
        ]
          .filter(
            (b) =>
              !el.contains(b) &&
              !b.closest(".dialog-actions") &&
              b.offsetParent !== null &&
              el.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING,
          )
          .map((b) => b.textContent.trim()),
      );
      assert.deepEqual(after, [], `${entry.id}: nothing after the danger zone`);
      for (const [label, title] of entry.buttons) {
        const button = zone.getByRole("button", { name: label, exact: true });
        assert.equal(
          await button.count(),
          1,
          `${entry.id}: "${label}" in the danger zone`,
        );
        // Not a second time among the normal actions of the view.
        const visible = await scope
          .getByRole("button", { name: label, exact: true })
          .filter({ visible: true })
          .count();
        assert.equal(
          visible,
          1,
          `${entry.id}: "${label}" only in the danger zone`,
        );
        if (theme === "graphit" && width === 1440) {
          await button.click();
          const dialog = page.getByRole("dialog").last();
          await dialog
            .getByRole("heading")
            .filter({ hasText: title })
            .first()
            .waitFor();
          await dialog
            .getByRole("button", { name: "Abbrechen", exact: true })
            .click();
          assert.equal(
            destroying(data).length,
            0,
            `${entry.id}: cancel sent nothing`,
          );
        }
      }
      await zone.scrollIntoViewIfNeeded();
      assert.ok(
        (await overflow(page)) <= 1,
        `${entry.id}: no horizontal scrolling at ${width}px`,
      );
      await page.waitForTimeout(150);
      await page.screenshot({
        path: `${out}/${entry.id}-${theme}-${width}.png`,
      });
      await ctx.close();
    }
  }
  // New on the app page: removing the app with its data, then back to Docker.
  {
    const { ctx, page, data } = await create("owner");
    await routes(ctx, data);
    const removed = [];
    await ctx.route("**/api/v1/docker/apps/photos?*", (r) => {
      removed.push(new URL(r.request().url()).search);
      return r.fulfill({ status: 204, body: "" });
    });
    await page.goto(base + "/docker/apps/photos");
    const zone = page.getByRole("region", {
      name: "Gefahrenzone",
      exact: true,
    });
    await zone
      .getByRole("button", { name: "Entfernen …", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("checkbox").check();
    await dialog
      .getByRole("button", { name: "App und Daten löschen", exact: true })
      .click();
    await page.waitForURL("**/docker");
    assert.deepEqual(
      removed,
      ["?deleteData=photos"],
      "the data are deleted with the app",
    );
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log(`danger zones ok: ${pages.length} pages, screenshots in ${out}`);
} finally {
  await browser.close();
}
