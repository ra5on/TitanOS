// Settings › Firewall, Let's Encrypt and Dynamic DNS (new in 0.31.0): the
// pages render inside the settings sidebar with the compact controls, the
// firewall switches on with a trial and confirm, and the Let's Encrypt page
// warns when HTTP-01 meets a firewall that keeps port 80 from the internet.
// Isolated API data only (test-ui-fixtures.mjs plus the answers below); no
// request reaches a NAS, Let's Encrypt or a DNS provider.
//
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5190 node scripts/test-ui-firewall.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5190";
const out = process.env.UI_OUTPUT || "out/validation/ui-firewall";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const time = "2026-10-28T08:00:00Z";

const zones = (home, vpn, internet) => ({ home, vpn, internet });
const local = zones(true, true, false);
function firewallData() {
  const services = [
    { id: "webui", ports: [{ protocol: "tcp", ports: ["80", "443"] }], default: local },
    { id: "apps", ports: [{ protocol: "tcp", ports: ["443"] }], default: local },
    { id: "smb", ports: [{ protocol: "tcp", ports: ["139", "445"] }, { protocol: "udp", ports: ["137-138"] }], default: local },
    { id: "nfs", ports: [{ protocol: "tcp", ports: ["111", "2049", "20048", "32765-32766", "32768"] }, { protocol: "udp", ports: ["111", "2049", "20048", "32765-32766", "32768"] }], default: local },
    { id: "ssh", ports: [{ protocol: "tcp", ports: ["22"] }], default: local },
    { id: "discovery", ports: [{ protocol: "udp", ports: ["3702", "5353", "5355"] }, { protocol: "tcp", ports: ["5355", "5357"] }], default: zones(true, false, false) },
    { id: "ping", ports: [{ protocol: "icmp", ports: null }], default: local },
    { id: "ups", ports: [{ protocol: "tcp", ports: ["3493"] }], default: local },
    { id: "backuptarget", ports: [{ protocol: "tcp", ports: ["8750"] }], default: local },
    { id: "vpn", ports: [{ protocol: "udp", ports: ["41641", "51820"] }], default: zones(true, true, true) },
  ];
  // Settings version 2: built-in zones, one own zone for a lab VLAN with
  // a Docker network, and every cell as the backend fills them in.
  const builtin = (id) => ({ id, builtin: true, networks: [], sources: [] });
  const zoneList = [builtin("home"), builtin("vpn"), builtin("internet"), builtin("docker"), { id: "zlab", name: "Labor", networks: ["net:lab", "docker:media"], sources: ["10.30.0.0/16"] }];
  const allow = (service) => ({ action: "allow", protocol: "any", service });
  const cells = [];
  for (const zone of ["home", "vpn", "internet"]) {
    cells.push({ from: zone, to: "nas", default: "deny", policies: services.filter((s) => s.default[zone]).map((s) => allow(s.id)) });
    cells.push({ from: zone, to: "apps", default: zone === "internet" ? "deny" : "allow", policies: [] });
  }
  cells.push({ from: "docker", to: "nas", default: "deny", policies: [allow("apps"), { action: "allow", protocol: "any", ports: "53", comment: "DNS" }] });
  cells.push({ from: "docker", to: "apps", default: "allow", policies: [] });
  cells.push({ from: "zlab", to: "nas", default: "deny", policies: [allow("ping")] });
  cells.push({ from: "zlab", to: "apps", default: "deny", policies: [] });
  for (const from of ["docker", "zlab"])
    for (const to of zoneList.map((z) => z.id))
      cells.push({ from, to, default: to === "docker" || to === "zlab" ? "deny" : "allow", policies: [] });
  const config = {
    enabled: false,
    zones: zoneList,
    cells,
    autoBlock: { enabled: false, attempts: 10, within: 5, minutes: 60, permanent: false, exemptLocal: true, exempt: [] },
  };
  return {
    puts: [],
    actions: [],
    status: {
      config,
      pending: null,
      pendingSeconds: 0,
      active: false,
      services,
      apps: [{ key: "photos", name: "Photos", ports: [{ port: 2283, protocol: "tcp" }] }],
      homeNetworks: ["192.168.1.0/24", "fd00:1::/64"],
      vpnNetworks: ["100.64.0.0/10", "fd7a:115c:a1e0::/48"],
      networks: [
        { id: "net:lan", kind: "network", name: "LAN", device: "br0", subnets: ["192.168.1.0/24"] },
        { id: "net:lab", kind: "network", name: "Labor", device: "tvlab", subnets: ["192.168.40.0/24"] },
        { id: "net:iot", kind: "network", name: "IoT", device: "tviot", subnets: ["192.168.50.0/24"] },
        { id: "docker:bridge", kind: "docker", name: "bridge", device: "docker0", subnets: ["172.17.0.0/16"] },
        { id: "docker:media", kind: "docker", name: "media", device: "br-0123456789ab", subnets: ["172.19.0.0/16"] },
        { id: "docker:titanos-lan-apps", kind: "dockerlan", name: "titanos-lan-apps", parent: "tnapps", subnets: ["192.168.30.0/24"] },
      ],
      clientAddress: "192.168.1.50",
      clientZone: "home",
      blocked: [{ address: "203.0.113.9", since: time, until: "2026-10-28T09:00:00Z", source: "web", attempts: 10 }],
      recent: [
        { at: time, source: "198.51.100.4", zone: "internet", protocol: "tcp", port: 22, interface: "br0" },
        { at: time, source: "203.0.113.9", zone: "blocked", protocol: "tcp", port: 443, interface: "br0" },
        { at: time, source: "172.19.0.5", zone: "zlab", protocol: "tcp", port: 22, forward: true, destination: "192.168.1.9", interface: "br-0123456789ab", out: "br0" },
      ],
    },
  };
}

function acmeData(challenge) {
  return {
    settings: { enabled: true, email: "", staging: false, domains: ["nas.example.org"], apps: false, challenge, provider: { type: challenge === "dns-01" ? "cloudflare" : "" }, propagation: 0, directory: "" },
    secrets: { token: challenge === "dns-01" },
    names: ["nas.example.org"],
    appDomain: "",
    certificate: { names: ["nas.example.org"], issuer: "R11", notBefore: "2026-10-01T00:00:00Z", notAfter: "2026-12-30T00:00:00Z", fingerprint: "ab" },
    running: false,
    lastAttempt: time,
    lastSuccess: time,
    failures: 0,
    renewAt: "2026-11-30T00:00:00Z",
    httpPort: true,
    firewall: false,
    internetHttp: true,
  };
}

const ddnsData = {
  settings: { enabled: true, hostnames: ["nas.example.org"], provider: { type: "cloudflare" }, ipv4: true, ipv6: true, interval: 0, lookupURL: "", interface: "", heartbeat: { enabled: false } },
  secrets: { token: true },
  currentIPv4: "198.51.100.20",
  currentIPv6: "2001:db8::20",
  publishedIPv4: "198.51.100.20",
  publishedIPv6: "2001:db8::20",
  lastCheck: time,
  lastUpdate: time,
  nextCheck: "2026-10-28T08:05:00Z",
  running: false,
  heartbeat: {},
};

async function routes(ctx, fw, acme) {
  await ctx.route("**/api/v1/firewall**", async (route) => {
    const path = new URL(route.request().url()).pathname.slice("/api/v1".length);
    const method = route.request().method();
    const json = (value) => route.fulfill({ json: value });
    if (path === "/firewall" && method === "GET") return json(fw.status);
    if (path === "/firewall" && method === "PUT") {
      const body = route.request().postDataJSON();
      fw.puts.push(body);
      fw.status = { ...fw.status, pending: body, pendingSeconds: 120, active: body.enabled };
      return json(fw.status);
    }
    if (path === "/firewall/confirm" && method === "POST") {
      fw.actions.push("confirm");
      fw.status = { ...fw.status, config: fw.status.pending, pending: null, pendingSeconds: 0 };
      return json(fw.status);
    }
    if (path === "/firewall/revert" && method === "POST") {
      fw.actions.push("revert");
      fw.status = { ...fw.status, pending: null, pendingSeconds: 0, active: fw.status.config.enabled };
      return json(fw.status);
    }
    if (path === "/firewall/unblock" && method === "POST") {
      fw.actions.push("unblock " + route.request().postDataJSON().address);
      fw.status = { ...fw.status, blocked: [] };
      return json(fw.status);
    }
    return json(fw.status);
  });
  await ctx.route("**/api/v1/system/letsencrypt**", (route) => route.fulfill({ json: acme.value }));
  await ctx.route("**/api/v1/network/ddns**", (route) => route.fulfill({ json: ddnsData }));
}

const overflow = (page) => page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
const nav = (page) => page.getByRole("navigation", { name: "Einstellungsbereiche", exact: true });

try {
  // Desktop.
  {
    const { ctx, page, data } = await create("owner");
    data.system.version = "0.31.0";
    await page.setViewportSize({ width: 1440, height: 1000 });
    const fw = firewallData();
    const acme = { value: acmeData("http-01") };
    await routes(ctx, fw, acme);

    // The overview lists the four new areas under "Netzwerk und Zugriff",
    // in order and with the "neu" badge.
    await page.goto(base + "/settings");
    await page.locator(".settings-page .areas a").first().waitFor();
    await page.screenshot({ path: `${out}/settings-overview-desktop.png`, fullPage: true });
    const overview = await page.locator(".settings-page .areas a").evaluateAll((links) => links.map((a) => a.getAttribute("href")));
    assert.ok(overview.indexOf("/settings/firewall") === overview.indexOf("/settings/network") + 1, "overview order");
    await page.goto(base + "/settings/network");
    await nav(page).waitFor();
    const group = nav(page).locator(".group-title", { hasText: "Netzwerk und Zugriff" });
    await group.waitFor();
    const hrefs = await nav(page).locator("a").evaluateAll((links) => links.map((a) => a.getAttribute("href")));
    const network = hrefs.slice(hrefs.indexOf("/settings/network"), hrefs.indexOf("/settings/access") + 1);
    assert.deepEqual(network, [
      "/settings/network", "/settings/firewall", "/settings/letsencrypt", "/settings/ddns", "/settings/wireguard", "/settings/proxy", "/settings/access",
    ]);
    for (const area of ["firewall", "letsencrypt", "ddns"])
      assert.equal(await nav(page).locator(`a[href="/settings/${area}"]`).getByText("neu", { exact: true }).count(), 1, area);

    // Firewall: inside the sidebar, compact controls, switch on with trial.
    await page.goto(base + "/settings/firewall");
    await page.getByRole("heading", { level: 1, name: "Firewall" }).waitFor();
    assert.equal(await nav(page).locator('a[aria-current="page"]').getAttribute("href"), "/settings/firewall");
    // Opening an area once takes its "new" badge away; the others keep theirs.
    assert.equal(await nav(page).locator('a[href="/settings/firewall"]').getByText("neu", { exact: true }).count(), 0, "seen");
    assert.equal(await nav(page).locator('a[href="/settings/ddns"]').getByText("neu", { exact: true }).count(), 1, "not seen yet");
    assert.equal(await page.locator(".crumbs").first().isVisible(), false, "no breadcrumb beside the sidebar");
    const button = page.locator(".settings-page .tile .btn").first();
    await button.waitFor();
    assert.ok((await button.boundingBox()).height <= 32, "compact settings buttons");
    const aside = await page.locator(".settings-aside").boundingBox();
    const main = await page.locator(".settings-page").first().boundingBox();
    assert.ok(main.x >= aside.x + aside.width, "the page sits right of the sidebar");
    assert.ok(main.width <= 64 * 16 + 1, `page at most 64rem wide (${main.width})`);
    await page.screenshot({ path: `${out}/firewall-desktop-off.png`, fullPage: true });
    const master = page.getByRole("checkbox", { name: "Firewall einschalten", exact: true });
    await master.check();
    await page.getByRole("button", { name: "Auf Probe übernehmen" }).click();
    await page.getByRole("button", { name: "Regeln behalten" }).waitFor();
    assert.equal(fw.puts.at(-1)?.enabled, true, "PUT with enabled");
    await page.screenshot({ path: `${out}/firewall-desktop-trial.png`, fullPage: true });
    await page.getByRole("button", { name: "Regeln behalten" }).click();
    await page.getByText("Die neuen Regeln gelten jetzt dauerhaft.").waitFor();
    assert.deepEqual(fw.actions, ["confirm"]);
    await page.screenshot({ path: `${out}/firewall-desktop-on.png`, fullPage: true });

    // Zone matrix: cells with their state, grey where traffic does not
    // pass the NAS, the policies of a cell below on click.
    const cell = (from, to) => page.locator(`[data-cell="${from}>${to}"]`);
    assert.match(await cell("home", "nas").innerText(), /Teilweise/);
    assert.match(await cell("internet", "apps").innerText(), /Blockiert/);
    assert.match(await cell("docker", "internet").innerText(), /Erlaubt/);
    assert.equal(await cell("home", "internet").count(), 0, "home → internet does not pass the NAS");
    assert.ok((await page.getByTitle("Läuft nicht über das NAS").count()) > 0);
    await cell("docker", "zlab").click();
    await page.getByRole("heading", { name: "Docker → Labor" }).waitFor();
    await page.getByRole("button", { name: "Richtlinie hinzufügen" }).click();
    await page.locator("[data-policy='0'] input").nth(1).fill("5432");
    await page.screenshot({ path: `${out}/firewall-desktop-cell.png`, fullPage: true });
    assert.match(await cell("docker", "zlab").innerText(), /Teilweise/);
    // A new zone with the IoT VLAN; on trial with the policy above.
    await page.getByRole("button", { name: "Zone anlegen" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Name").fill("IoT");
    await dialog.getByRole("checkbox", { name: /IoT/ }).check();
    assert.equal(await dialog.getByRole("checkbox", { name: /titanos-lan-apps/ }).isDisabled(), true, "macvlan/ipvlan cannot be assigned");
    await page.screenshot({ path: `${out}/firewall-desktop-zone-dialog.png`, fullPage: true });
    await dialog.getByRole("button", { name: "Übernehmen" }).click();
    await page.getByRole("rowheader", { name: /IoT/ }).waitFor();
    await page.getByRole("button", { name: "Auf Probe übernehmen" }).click();
    await page.getByRole("button", { name: "Regeln behalten" }).waitFor();
    const sent = fw.puts.at(-1);
    const iot = sent.zones.find((z) => z.name === "IoT");
    assert.ok(iot && iot.networks.includes("net:iot") && /^z[a-z0-9]+$/.test(iot.id), "new zone sent");
    assert.deepEqual(sent.cells.find((c) => c.from === "docker" && c.to === "zlab").policies.map((p) => p.ports), ["5432"]);
    await page.getByRole("button", { name: "Regeln behalten" }).click();
    await page.getByText("Die neuen Regeln gelten jetzt dauerhaft.").waitFor();

    // Networks: zone per network, Docker by default in the zone Docker.
    await page.getByRole("tab", { name: "Netze" }).click();
    await page.locator("[data-network='docker:bridge']").waitFor();
    assert.equal(await page.locator("[data-network='docker:bridge'] select").inputValue(), "");
    assert.match(await page.locator("[data-network='docker:bridge'] select option:checked").innerText(), /Standard-Zone Docker/);
    assert.equal(await page.locator("[data-network='docker:media'] select").inputValue(), "zlab");
    assert.equal(await page.locator("[data-network='docker:titanos-lan-apps'] select").count(), 0);
    await page.screenshot({ path: `${out}/firewall-desktop-networks.png`, fullPage: true });
    await page.getByRole("tab", { name: "Richtlinien" }).click();
    await page.locator("[data-policy]").first().waitFor();
    await page.screenshot({ path: `${out}/firewall-desktop-policies.png`, fullPage: true });
    await page.getByRole("tab", { name: "Auto-Sperre" }).click();
    await page.getByText("203.0.113.9").waitFor();
    await page.getByRole("tab", { name: "Protokoll" }).click();
    await page.getByText("nach 192.168.1.9").waitFor();
    await page.waitForURL(/tab=recent/);

    // Let's Encrypt with HTTP-01 and the firewall off: no hint.
    await page.goto(base + "/settings/letsencrypt");
    await page.getByRole("heading", { level: 1, name: "Let's Encrypt" }).waitFor();
    assert.equal(await page.getByTestId("le-firewall-http").count(), 0, "no hint without firewall");
    // Firewall on, port 80 not open from the internet: the hint with links.
    acme.value = { ...acmeData("http-01"), firewall: true, internetHttp: false };
    await page.reload();
    const hint = page.getByTestId("le-firewall-http");
    await hint.waitFor();
    await hint.getByText("Port 80 gehört zur Weboberfläche").waitFor();
    assert.equal(await hint.getByRole("link", { name: "Zur Firewall" }).getAttribute("href"), "/settings/firewall?tab=policies#firewall-policies");
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({ path: `${out}/letsencrypt-desktop-http01-firewall.png`, fullPage: true });
    // "Use the DNS challenge" only changes the form; nothing is saved.
    await hint.getByRole("button", { name: "DNS-Challenge verwenden" }).click();
    assert.equal(await page.getByTestId("le-firewall-http").count(), 0);
    assert.equal(await page.getByRole("radio", { name: /DNS-Challenge/ }).isChecked(), true);
    // An own rule for port 80 from the internet: no hint.
    acme.value = { ...acmeData("http-01"), firewall: true, internetHttp: true };
    await page.reload();
    await page.getByRole("heading", { level: 1, name: "Let's Encrypt" }).waitFor();
    await page.getByRole("heading", { name: "Zertifikat von Let's Encrypt" }).waitFor();
    assert.equal(await page.getByTestId("le-firewall-http").count(), 0, "no hint once port 80 is open");
    acme.value = acmeData("dns-01");
    await page.reload();
    await page.getByRole("heading", { name: "Zertifikat von Let's Encrypt" }).waitFor();
    await page.screenshot({ path: `${out}/letsencrypt-desktop.png`, fullPage: true });

    // Dynamic DNS.
    await page.goto(base + "/settings/ddns");
    await page.getByRole("heading", { level: 1, name: "Dynamisches DNS" }).waitFor();
    await page.getByText("198.51.100.20").first().waitFor();
    assert.equal(await nav(page).locator('a[aria-current="page"]').getAttribute("href"), "/settings/ddns");
    await page.screenshot({ path: `${out}/ddns-desktop.png`, fullPage: true });
    await ctx.close();
  }

  // Phone width: no sidebar, way back, no horizontal scroll.
  {
    const { ctx, page, data } = await create("owner", "graphit", 390);
    data.system.version = "0.31.0";
    const fw = firewallData();
    fw.status.config.enabled = true;
    fw.status.active = true;
    const acme = { value: { ...acmeData("http-01"), firewall: true, internetHttp: false } };
    await routes(ctx, fw, acme);
    for (const [path, heading, name] of [
      ["/settings/firewall", "Firewall", "firewall"],
      ["/settings/letsencrypt", "Let's Encrypt", "letsencrypt"],
      ["/settings/ddns", "Dynamisches DNS", "ddns"],
    ]) {
      await page.goto(base + path);
      await page.getByRole("heading", { level: 1, name: heading }).waitFor();
      await page.getByRole("link", { name: "‹ Einstellungen", exact: true }).waitFor();
      assert.equal(await nav(page).isVisible(), false);
      if (name === "letsencrypt") await page.getByTestId("le-firewall-http").waitFor();
      assert.ok((await overflow(page)) <= 1, `${name}: horizontal scroll`);
      await page.screenshot({ path: `${out}/${name}-mobile.png`, fullPage: true });
      if (name === "firewall")
        for (const tab of ["networks", "policies", "autoblock", "recent"]) {
          await page.goto(`${base}/settings/firewall?tab=${tab}`);
          await page.getByRole("tab", { selected: true }).waitFor();
          assert.ok((await overflow(page)) <= 1, `firewall ${tab}: horizontal scroll`);
          await page.screenshot({ path: `${out}/firewall-mobile-${tab}.png`, fullPage: true });
        }
    }
    await page.goto(base + "/settings");
    await page.locator(".settings-group").first().waitFor();
    await page.screenshot({ path: `${out}/settings-overview-mobile.png`, fullPage: true });
    await ctx.close();
  }

  // Accounts with the role "user" see none of the areas.
  {
    const { ctx, page } = await create("user");
    await page.goto(base + "/settings");
    await page.locator(".settings-page").first().waitFor();
    for (const area of ["firewall", "letsencrypt", "ddns"])
      assert.equal(await page.locator(`a[href="/settings/${area}"]`).count(), 0, area);
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("firewall, Let's Encrypt and dynamic DNS UI checks passed");
} finally {
  await browser.close();
}
