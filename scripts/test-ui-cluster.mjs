// The cluster (PLAN 23, decided 1.11.2026): ways chosen freely for managing
// and backups, an exception per node, "Check ways", the tunnel of TitanOS,
// notices; nodes of another version managed with a strip that names both
// versions; "Update all nodes" one after the other or all at once; the
// cluster tile on the dashboard. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5173 node scripts/test-ui-cluster.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5173";
const out = process.env.UI_OUTPUT || "out/validation/ui-cluster";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const time = "2026-10-23T16:00:00Z";
const status = (name, version) => ({
  name,
  hostname: name,
  version,
  protocol: 1,
  uptimeSeconds: 86400 * 3,
  cpuPercent: 8,
  memory: { totalBytes: 32e9, availableBytes: 20e9 },
  volumes: [{ name: "Data", state: "ok", mounted: true, sizeBytes: 8e12, usedBytes: 3.1e12 }],
  warnings: 0,
  errors: 0,
});
const node = (id, name, addresses, ways = {}) => ({ id, name, addresses, ways, extra: [], cert: "a".repeat(64), joined: time, updated: time });
const nodes = [
  node("n1", "titan", ["192.168.1.20:443", "100.101.7.12:443", "titan.tail1234.ts.net:443"]),
  node("n2", "thor", ["192.168.1.30:443", "100.83.25.99:443", "10.77.0.2:443"], { "10.77.0.2:443": "tunnel" }),
  node("n3", "loki", ["10.8.0.3:443", "100.70.4.18:443", "10.77.0.3:443"], { "10.77.0.3:443": "tunnel" }),
];
function clusterPage() {
  return {
    cluster: {
      self: "n1",
      name: "Zuhause",
      internet: false,
      nodes,
      admin: "n1",
      manages: true,
      extra: [],
      found: ["192.168.1.20:443", "100.101.7.12:443", "titan.tail1234.ts.net:443"],
      foundWays: { "192.168.1.20:443": "home", "100.101.7.12:443": "tailscale", "titan.tail1234.ts.net:443": "tailscale" },
      ways: {},
      routes: { n3: { manage: "tunnel" } },
      tunnel: { running: false },
      version: "0.44.0",
      protocol: 1,
    },
    nodes: [
      { ...nodes[0], self: true, online: true, compatible: true, status: status("titan", "0.44.0") },
      { ...nodes[1], self: false, online: true, compatible: true, status: status("thor", "0.43.0"), address: "192.168.1.30:443", way: "home", millis: 2 },
      {
        ...nodes[2],
        self: false,
        online: true,
        compatible: true,
        status: status("loki", "0.44.0"),
        address: "100.70.4.18:443",
        way: "tailscale",
        millis: 38,
        preferred: "tunnel",
        fallback: true,
        clockSkew: 182,
      },
    ],
  };
}
const switchNodes = {
  manages: true,
  version: "0.44.0",
  nodes: [
    { id: "n1", name: "titan", self: true, online: true, version: "0.44.0", compatible: true },
    { id: "n2", name: "thor", self: false, online: true, version: "0.43.0", compatible: true },
    { id: "n3", name: "loki", self: false, online: true, version: "0.44.0", compatible: true },
  ],
};
const probes = {
  checked: time,
  nodes: [
    {
      id: "n2",
      name: "thor",
      probes: [
        { way: "home", address: "192.168.1.30:443", ok: true, millis: 2, used: true },
        { way: "tunnel", address: "10.77.0.2:443", ok: true, millis: 3, used: false },
      ],
    },
    {
      id: "n3",
      name: "loki",
      probes: [
        { way: "tunnel", address: "10.77.0.3:443", ok: false, millis: 6000, problem: "timeout", used: false },
        { way: "tailscale", address: "100.70.4.18:443", ok: true, millis: 38, used: true },
      ],
    },
  ],
};
const update = (version, available) => ({ version, available: available ? { version: available } : null, kind: "app" });

async function shot(page, file) {
  const size = page.viewportSize();
  const height = await page.evaluate(() => document.documentElement.scrollHeight);
  await page.setViewportSize({ width: size.width, height: Math.max(size.height, height) });
  await page.waitForTimeout(150);
  await page.screenshot({ path: file });
  await page.setViewportSize(size);
}

async function open(width, path, node = "") {
  const made = await create("owner", "graphit", width);
  made.data.prefs.appearance = { theme: "graphit", nav: { order: [], hidden: [], layout: "side", magnify: true } };
  const state = { put: [], routes: [], runs: [], run: null, page: clusterPage() };
  const { ctx, page } = made;
  await ctx.route("**/api/v1/cluster", (r) => {
    if (r.request().method() === "PUT") {
      const body = r.request().postDataJSON();
      state.put.push(body);
      state.page.cluster.ways = body.ways;
      state.page.cluster.tunnel = { enabled: body.tunnel, address: body.tunnel ? "10.77.0.1" : "", port: 51821, running: body.tunnel };
    }
    return r.fulfill({ json: state.page });
  });
  await ctx.route("**/api/v1/cluster/nodes", (r) => r.fulfill({ json: switchNodes }));
  await ctx.route("**/api/v1/cluster/probe", (r) => r.fulfill({ json: probes }));
  await ctx.route("**/api/v1/cluster/nodes/*/route", (r) => {
    state.routes.push({ url: r.request().url(), body: r.request().postDataJSON() });
    return r.fulfill({ json: state.page });
  });
  await ctx.route("**/api/v1/cluster/update", (r) => {
    if (r.request().method() === "POST") {
      const body = r.request().postDataJSON();
      state.runs.push(body);
      state.run = { parallel: body.parallel, running: true, started: time, nodes: body.nodes.map((id) => ({ id, name: id, state: "waiting" })) };
    }
    return r.fulfill({ json: { run: state.run } });
  });
  await ctx.route("**/api/v1/cluster/nodes/n2/api/update", (r) => r.fulfill({ json: update("0.43.0", "0.44.0") }));
  await ctx.route("**/api/v1/cluster/nodes/n3/api/update", (r) => r.fulfill({ json: update("0.44.0", null) }));
  await ctx.route("**/api/v1/cluster/nodes/n2/api/notifications**", (r) => r.fulfill({ json: { messages: [] } }));
  await ctx.route("**/api/v1/cluster/nodes/n2/api/docker", (r) => r.fulfill({ json: { settings: { enabled: true, volume: "Data" }, running: true, version: "27.5.1", containers: [], apps: [], jobs: {} } }));
  if (node) await ctx.addInitScript((id) => sessionStorage.setItem("titanos.cluster.node", id), node);
  await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 });
  await page.goto(base + path);
  return { ctx, page, state, data: made.data };
}

try {
  {
    // The page: nodes with their ways, versions and clock; settings in one
    // column; saving sends the ways and the tunnel.
    const { ctx, page, state } = await open(1440, "/settings/cluster");
    const list = page.locator("#cluster-nodes");
    await list.getByText("über Heimnetz · 2 ms").waitFor();
    await list.getByText("ältere Version").waitFor();
    await list.getByText("eigener Weg: WireGuard (TitanOS)").waitFor();
    await list.getByText(/WireGuard \(TitanOS\) nicht erreichbar/).waitFor();
    await list.getByText("Uhr geht 3 min vor (NTP prüfen)").waitFor();
    assert.equal(await page.locator("#cluster-alert").inputValue(), "0", "the notice defaults to 10 minutes");
    await shot(page, out + "/1-cluster.png");
    await page.locator("#cluster-way").selectOption("tailscale");
    await page.getByRole("switch", { name: "Nur diesen Weg benutzen" }).click();
    await page.locator("#cluster-backup").selectOption("home");
    await page.getByRole("switch", { name: "WireGuard-Tunnel von TitanOS" }).click();
    await page.getByRole("button", { name: "Speichern" }).click();
    await page.getByText("An · 10.77.0.1").waitFor();
    assert.deepEqual(state.put.at(-1).ways, { manage: "tailscale", only: true, backup: "home", return: 0, alert: 0 });
    assert.equal(state.put.at(-1).tunnel, true);

    // Check ways.
    await page.getByRole("button", { name: "Wege prüfen" }).click();
    const probe = page.getByRole("dialog", { name: "Wege prüfen" });
    await probe.getByText("Zeitüberschreitung").waitFor();
    assert.equal(await probe.getByText("benutzt").count(), 2);
    await page.screenshot({ path: out + "/2-wege-pruefen.png" });
    await probe.getByRole("button", { name: "Schließen" }).click();

    // The way to one node.
    await list.getByRole("button", { name: "Weitere Aktionen" }).nth(1).click();
    await page.getByRole("menuitem", { name: /Weg zu diesem Knoten/ }).click();
    const route = page.getByRole("dialog", { name: "Weg zu loki" });
    assert.equal(await route.locator("#route-manage").inputValue(), "tunnel");
    await page.screenshot({ path: out + "/3-weg-je-knoten.png" });
    await route.locator("#route-backup").selectOption("home");
    await route.getByRole("button", { name: "Übernehmen" }).click();
    await route.waitFor({ state: "detached" });
    assert.match(state.routes.at(-1).url, /\/cluster\/nodes\/n3\/route$/);
    assert.deepEqual(state.routes.at(-1).body, { manage: "tunnel", only: false, backup: "home" });

    // A node of another version is managed, not greyed out.
    assert.equal(await list.getByRole("button", { name: "Verwalten" }).first().isEnabled(), true);
    await ctx.close();
  }
  {
    // Managing an older node: the strip names both versions; what it does
    // not have yet says so instead of "not found".
    const { ctx, page } = await open(1440, "/docker", "n2");
    await page.getByText("thor läuft mit 0.43.0, dieses NAS mit 0.44.0", { exact: false }).waitFor();
    await page.getByRole("link", { name: "thor aktualisieren" }).waitFor();
    await shot(page, out + "/4-aeltere-version.png");
    await ctx.close();
  }
  {
    // Updates: the nodes of the cluster and "Update all".
    const { ctx, page, state } = await open(1440, "/settings/updates");
    const section = page.getByRole("region", { name: /Cluster „Zuhause“/ });
    await section.getByText("Update verfügbar").waitFor();
    await shot(page, out + "/5-updates.png");
    await section.getByRole("button", { name: "Alle aktualisieren" }).click();
    const dialog = page.getByRole("dialog", { name: "Alle Knoten aktualisieren" });
    await dialog.getByText("App-Update, kein NAS-Neustart").waitFor();
    assert.equal(await dialog.getByRole("radio", { name: /Nacheinander/ }).isChecked(), true);
    await page.screenshot({ path: out + "/6-alle-aktualisieren.png" });
    await dialog.getByRole("radio", { name: /Alle gleichzeitig/ }).check();
    await dialog.getByRole("button", { name: "1 Knoten aktualisieren" }).click();
    await section.getByText("Aktualisiert die Knoten gleichzeitig").waitFor();
    assert.deepEqual(state.runs.at(-1), { nodes: ["n2"], parallel: true });
    await ctx.close();
  }
  {
    // The dashboard tile.
    const { ctx, page, data } = await open(1440, "/settings/cluster");
    data.prefs.dashboard = { tiles: [{ id: "cluster", x: 0, y: 0, w: 6, h: 4 }] };
    await page.goto(base + "/");
    await page.getByText("3 von 3 Knoten erreichbar").first().waitFor();
    await page.getByText("0.43.0 · Heimnetz · CPU 8 %", { exact: false }).waitFor();
    await shot(page, out + "/7-dashboard.png");
    await ctx.close();
  }
  {
    const { ctx, page } = await open(390, "/settings/cluster");
    await page.locator("#cluster-nodes").getByText("über Heimnetz · 2 ms").waitFor();
    assert.ok((await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)) <= 0, "no sideways scroll");
    await shot(page, out + "/8-cluster-handy.png");
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("ui-cluster ok:", out);
} finally {
  await browser.close();
}
