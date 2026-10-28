// Settings › IPv64: all tabs and "Public through IPv64" in the app editor.
// Isolated API data only (test-ui-fixtures.mjs plus the answers of titand's
// /api/v1/ipv64 routes below); no request reaches IPv64 or a NAS.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5191";
const out = process.env.UI_OUTPUT || "out/validation/ui-ipv64";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const time = "2026-10-28T08:00:00Z";

function ipv64Data() {
  const usage = (used, limit) => ({ used, limit });
  const check = (id, name, type, status, extra = {}) => ({
    id, name, type, status, notify: { on_down: true, on_up: true, integrations: [0] }, created_at: time, last_seen_at: time, last_alarm_at: null, pings_total: 12, failed_in_row: 0, ...extra,
  });
  return {
    calls: [],
    status: {
      status: {
        settings: { notify: { enabled: false, failover: true, cdnTraffic: true, healthchecks: true }, blocklist: { enabled: false }, wireguard: { enabled: false }, published: [], maintenance: [] },
        keySet: false, keySource: "ddns", poll: {}, blocklistStatus: { entries: 0, live: false }, tunnel: { up: false, routes: [] }, readOnly: false,
      },
      firewall: true, nasCheck: "nas01", hostname: "titan-test",
      zones: ["ipv64.de", "ipv64.net", "home64.de"], recordTypes: ["A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "TLSA", "CAA"],
    },
    me: {
      username: "owner", registered_at: time, last_login_at: time, plan: { id: 1, name: "Free", valid_until: null },
      usage: { dyndns_domains: usage(2, 3), dyndns_updates_24h: usage(20, 48), healthchecks: usage(3, 10), cdn_domains: usage(1, 1), cdn_traffic_gb: usage(1.5, 10), vpn_traffic_gb: { used: 0.2, limit: 5, limited: false } },
      log_retention_days: 7, auth: { type: "api_key", key_id: 1, key_name: "titanos" },
    },
    keys: [
      { id: 1, name: "titanos", token_hint: "Ab12…9xYz", created_at: time, expires_at: null, expired: false, uses: 120, usedBy: ["ddns"] },
      { id: 2, name: "laptop", token_hint: "Cd34…7wVu", created_at: time, expires_at: "2026-12-01T00:00:00Z", expired: false, uses: 3, usedBy: [] },
    ],
    domains: [
      { id: 1, name: "nas.ipv64.de", kind: "dyndns", zone: "ipv64.de", wildcard: true, ignore: "none", active: true, updateCount: 214 },
      { id: 2, name: "example.org", kind: "own", zone: "example.org", wildcard: false, ignore: "none", active: true, updateCount: 0 },
    ],
    records: {
      "nas.ipv64.de": [
        { id: 11, domain: "nas.ipv64.de", prefix: "", fqdn: "nas.ipv64.de", type: "A", content: "203.0.113.10", ttl: 60, state: "active", pending_dns: false, token: "rec11" },
        { id: 12, domain: "nas.ipv64.de", prefix: "", fqdn: "nas.ipv64.de", type: "AAAA", content: "2001:db8::10", ttl: 60, state: "cdn_proxied", pending_dns: false, token: "rec12" },
        { id: 13, domain: "nas.ipv64.de", prefix: "_acme-challenge", fqdn: "_acme-challenge.nas.ipv64.de", type: "TXT", content: "abc", ttl: 60, state: "active", pending_dns: true, token: "rec13" },
      ],
    },
    checks: [
      check("nas01", "titanos-titan-test", "push", "up", { push: { period_seconds: 1800, grace_seconds: 600, expected_by: time } }),
      check("web01", "Nextcloud", "http", "down", { check: { target: "", prefer_ipv4: false, port: 0, url: "https://cloud.example.org", verify_tls: true, keyword: "", interval_seconds: 300, retry_interval_seconds: 60, failures_before_alarm: 2, next_check_at: time } }),
      check("ssh01", "SSH", "tcp", "paused", { check: { target: "nas.ipv64.de", prefer_ipv4: true, port: 22, url: "", verify_tls: false, keyword: "", interval_seconds: 600, retry_interval_seconds: 60, failures_before_alarm: 3, next_check_at: null } }),
    ],
    integrations: [
      { id: 0, type: "account_email", name: "Account-E-Mail", status: "active", default: true, options: {}, uses: 5 },
      { id: 7, type: "telegram", name: "Handy", status: "active", default: false, options: { chat_id: "42" }, uses: 2 },
    ],
    pages: [{ id: 3, name: "Mein NAS", slug: "meinnas", url: "https://ipv64.net/status/meinnas", description: "", password_protected: true, design: 2, autorefresh: true, healthchecks: ["nas01", "web01"] }],
    offline: [{ id: "op00000000000001", name: "Wartung", domain: "nas.ipv64.de", email: "", phone: "", alternative_url: "", text: "Gleich zurück", icon_url: null, views: 4, in_use_by_policy: "po00000000000001", active: false, preview_url: "https://ipv64.net/offline/preview/op00000000000001" }],
    policies: [{
      id: "po00000000000001", name: "TitanOS nas.ipv64.de", status: "active", manual: false, mode: "auto_failover_and_fallback", fallback_requires_all: false, linking: "or", notifications_enabled: true, integrations: [0], monitors_down: 0, last_check_at: time,
      monitors: [{ healthcheck: "nas01", name: "titanos-titan-test", negate: false, healthcheck_status: "up" }],
      rules: [{ record: "rec11", fqdn: "nas.ipv64.de", record_type: "A", state: "standby", target: { kind: "offline_page", offline_page: "op00000000000001" } }],
    }],
    maintenance: [{ id: "po00000000000001", domain: "nas.ipv64.de", check: "nas01", checkName: "titanos-titan-test", offlinePage: "op00000000000001", policy: "po00000000000001", records: ["rec11"], at: time }],
    cdn: [{
      id: "cd000000000000000000000000000001", domain: "nas.ipv64.de", domain_id: 1, active: true, pending: [], dns_records_enabled: true,
      server: { name: "cdn1", hostname: "cdn1.ipv64.net", ipv4: "192.0.2.50", ipv6: "2001:db8:cd::50" },
      proxy: { state: "on", frontend: "https_redirect", backend: { scheme: "http", port: 2283 }, websocket: true, certificate: "letsencrypt", caching: { enabled: false, time_minutes: 60, size_mb: 100 }, access_list: { enabled: false, mode: "denylist" } },
      portmapper: { state: "off" }, traffic_mb: { in: 1234, out: 5678 }, mappings: [],
    }],
    routers: [{
      id: "rt000000000000000000000000000001", name: "Zuhause", vrf: "vrf1", traffic_limited: false,
      gateways: [{
        id: "gw000000000000000000000000000001", name: "Frankfurt", state: "ready", disabled: false, traffic_limited: false, server: { name: "vpn1", hostname: "vpn1.ipv64.net", location: "Frankfurt" }, listen_port: 51820,
        ipv4: "10.64.0.1/24", ipv6: "fd64::1/64", public_key: "GatewayPublicKeyGatewayPublicKeyGatewayPubA=", traffic_bytes: { in: 1e6, out: 2e6 },
        peers: [{ id: "pe000000000000000000000000000001", name: "laptop", state: "ready", disabled: false, ipv4: "10.64.0.2/32", ipv6: null, public_key: "x", last_handshake: "00:01:10", traffic_bytes: { in: 1, out: 2 } }],
      }],
    }],
    app: { ports: [2283], firewall: true, zones: { home: true, vpn: true, internet: false }, keySource: "ddns" },
  };
}

async function routeIPv64(ctx, d) {
  await ctx.route("**/api/v1/ipv64**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.slice("/api/v1".length);
    const method = route.request().method();
    d.calls.push([method, path, route.request().postData() ?? ""]);
    const json = (value, status = 200) => route.fulfill({ status, json: value });
    if (path === "/ipv64" && method === "GET") return json(d.status);
    if (path === "/ipv64/blocklist" && method === "PUT") {
      const body = route.request().postDataJSON();
      d.status.status.settings.blocklist.enabled = body.enabled;
      d.status.status.blocklistStatus = body.enabled ? { entries: 18342, live: false, fetched: time, next: "2026-10-29T08:00:00Z" } : { entries: 0, live: false };
      return json(d.status);
    }
    if (path === "/ipv64/notify" && method === "PUT") {
      d.status.status.settings.notify = route.request().postDataJSON();
      d.status.status.poll = { last: time };
      return json(d.status);
    }
    if (path === "/ipv64/me") return json({ me: d.me, keySource: "ddns" });
    if (path === "/ipv64/logs") return json({ entries: [{ id: 20, domain_id: 1, title: "DynDNS", message: "Update nas.ipv64.de auf 203.0.113.10", time }], next_before: null, retention_days: 7 });
    if (path === "/ipv64/api-keys" && method === "GET") return json({ keys: d.keys });
    if (path === "/ipv64/api-keys" && method === "POST") {
      const body = route.request().postDataJSON();
      d.keys.push({ id: 9, name: body.name, token_hint: "Ef56…1aBc", created_at: time, expires_at: null, expired: false, uses: 0, usedBy: [] });
      return json({ key: d.keys.at(-1), token: "full-new-key-shown-once" }, 201);
    }
    if (path.startsWith("/ipv64/api-keys/") && method === "DELETE") {
      d.keys = d.keys.filter((k) => String(k.id) !== path.split("/").at(-1));
      return route.fulfill({ status: 204 });
    }
    if (path === "/ipv64/domains" && method === "GET") return json({ domains: d.domains, zones: d.status.zones, ddnsHosts: ["nas.ipv64.de"] });
    if (path.startsWith("/ipv64/domains/")) {
      const name = decodeURIComponent(path.split("/")[3]);
      if (path.endsWith("/rotate-token")) return json({ domain: d.domains[0], ddnsUpdated: true });
      const dom = d.domains.find((x) => x.name === name);
      return json({ ...dom, records: d.records[name] ?? [] });
    }
    if (path === "/ipv64/healthchecks" && method === "GET") return json({ healthchecks: d.checks, integrations: d.integrations, nasCheck: "nas01" });
    if (path === "/ipv64/healthchecks" && method === "POST") {
      const body = route.request().postDataJSON();
      d.checks.push({ ...d.checks[1], id: "new01", name: body.name, type: body.type, status: "new" });
      return json(d.checks.at(-1), 201);
    }
    if (/\/stats$/.test(path)) {
      const series = Array.from({ length: 24 }, (_, i) => ({ time, measurements: 12, availability_percent: i === 17 ? 60 : 100, latency_avg_ms: 20 + (i % 5), latency_max_ms: 40 }));
      return json({ range: url.searchParams.get("range"), measurements: 288, availability_percent: 98.3, latency_ms: { avg: 22, min: 18, max: 41 }, bucket_seconds: 3600, series });
    }
    if (/\/events$/.test(path)) return json({ events: [{ id: 9, time, status: "down", message: "HTTP 502", latency_ms: null }, { id: 8, time, status: "up", message: "HTTP 200", latency_ms: 21 }], has_more: false, next_before: null });
    if (/\/incidents$/.test(path)) return json({ incidents: [{ id: 1, status: "open", created_at: time, closed_at: null, events: [] }] });
    if (path === "/ipv64/integrations" && method === "POST") return json(d.integrations[1], 201);
    if (path === "/ipv64/status-pages") return json({ pages: d.pages });
    if (path === "/ipv64/offline-pages") return json({ pages: d.offline });
    if (path === "/ipv64/failover") return json({ policies: d.policies, maintenance: d.maintenance });
    if (path === "/ipv64/cdn" && method === "GET") return json({ cdn: d.cdn, published: d.status.status.settings.published });
    if (path.startsWith("/ipv64/cdn/") && method === "GET") return json({ cdn: d.cdn[0], records: d.records["nas.ipv64.de"].filter((r) => r.type === "A" || r.type === "AAAA"), accessList: [{ id: 1, address: "198.51.100.0/24" }] });
    if (path === "/ipv64/routers") return json({ routers: d.routers });
    if (path.startsWith("/ipv64/routers/")) return json({ router: d.routers[0], routes: [{ id: 1, destination: "10.64.0.0/24", gateway: "10.64.0.1", distance: 1, peer: null, system: true, disabled: false, state: "ready" }], rules: [{ id: 2, chain: "forward", action: "drop", family: "ipv4", source: "10.64.0.0/24", destination: "192.168.1.0/24", disabled: false, state: "ready" }], publicIPs: [] });
    if (path === "/ipv64/tunnel" && method === "POST") {
      d.status.status.settings.wireguard = { enabled: true, router: d.routers[0].id, gateway: d.routers[0].gateways[0].id, peer: "pe2", name: "titanos-titan-test", publicKey: "NasPublicKeyNasPublicKeyNasPublicKeyNasPubA=", addresses: ["10.64.0.5/32"], endpoint: "vpn1.ipv64.net:51820", allowedIPs: ["10.64.0.0/24", "0.0.0.0/0"] };
      d.status.status.tunnel = { up: true, routes: ["10.64.0.0/24"] };
      return json(d.status.status.tunnel);
    }
    if (path.startsWith("/ipv64/apps/")) {
      if (method === "PUT") {
        const body = route.request().postDataJSON();
        d.publish = body;
        d.app.published = { domain: body.domain, prefix: body.prefix, port: body.port, firewall: true, at: time };
        d.app.zones.internet = true;
        return json(d.app.published, 201);
      }
      return json(d.app);
    }
    return json({});
  });
  await ctx.route("**/api/v1/docker/apps/photos", (route) =>
    route.fulfill({ json: { compose: "services:\n  server:\n    image: immich:test\n    ports:\n      - 2283:2283\n" } }),
  );
}

const tab = async (page, name) => {
  await page.getByRole("tab", { name, exact: true }).click();
};

try {
  const { ctx, page } = await create("owner");
  const d = ipv64Data();
  await routeIPv64(ctx, d);

  // The settings overview lists the area under network.
  await page.goto(base + "/settings");
  // The area carries the "neu" badge in 0.31.0, which is part of the link name.
  await page.getByRole("link", { name: /^IPv64( neu)?$/ }).first().waitFor();

  // 1 Overview
  await page.goto(base + "/settings/ipv64");
  await page.getByText("Diese Seite nutzt den API-Key von Dynamisches DNS.").waitFor();
  await page.getByText("DynDNS-Updates (24 h)").waitFor();
  await page.getByText("TitanOS (dynamisches DNS)").waitFor();
  assert.equal(await page.getByRole("row", { name: /titanos/ }).getByRole("button", { name: "Löschen" }).count(), 0, "the key of TitanOS has no delete button");
  await page.screenshot({ path: `${out}/1-overview.png`, fullPage: true });
  await page.getByRole("button", { name: "API-Key anlegen", exact: true }).click();
  let dialog = page.getByRole("dialog");
  await dialog.getByLabel("Name").fill("Backup-Skript");
  await dialog.getByRole("button", { name: "API-Key anlegen", exact: true }).click();
  await page.getByRole("dialog").getByText("wird nur jetzt angezeigt").waitFor();
  assert.equal(await page.getByRole("dialog").locator("input").inputValue(), "full-new-key-shown-once");
  await page.screenshot({ path: `${out}/1-overview-key-once.png` });
  await page.getByRole("dialog").getByRole("button", { name: "Fertig" }).click();
  assert.ok(d.calls.some(([m, p, b]) => m === "POST" && p === "/ipv64/api-keys" && JSON.parse(b).expiresIn === 7776000), "key with expiry");

  // 2 Domains and DNS
  await tab(page, "Domains und DNS");
  await page.getByRole("row", { name: /nas\.ipv64\.de/ }).getByRole("button", { name: "Verwalten" }).click();
  await page.getByText("_acme-challenge").waitFor();
  await page.getByText("von CDN/Failover verwaltet").waitFor();
  await page.screenshot({ path: `${out}/2-domains.png`, fullPage: true });
  await page.getByRole("button", { name: "Zone zurücksetzen", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByText("Alle aktiven DNS-Einträge von nas.ipv64.de werden gelöscht").waitFor();
  assert.equal(await dialog.locator("input[type=text]").count(), 0, "no typed confirmation");
  await page.screenshot({ path: `${out}/2-domains-reset-confirm.png` });
  await dialog.getByRole("button", { name: "Abbrechen" }).click();
  await page.getByRole("button", { name: "Neuen Update-Token", exact: true }).click();
  await page.getByRole("dialog").getByText("TitanOS übernimmt den neuen Token automatisch").waitFor();
  await page.getByRole("dialog").getByRole("button", { name: "Neuen Token erzeugen" }).click();
  await page.getByText("TitanOS hat ihn für dynamisches DNS übernommen").waitFor();

  // 3 Monitoring
  await tab(page, "Überwachung");
  await page.getByRole("row", { name: /titanos-titan-test/ }).getByText("dieses NAS").waitFor();
  await page.getByRole("row", { name: /Nextcloud/ }).getByRole("button", { name: "Details" }).click();
  await page.getByRole("img", { name: "Verfügbarkeit im Zeitraum" }).waitFor();
  await page.getByRole("button", { name: "7d", exact: true }).click();
  assert.ok(d.calls.some(([, p]) => p === "/ipv64/healthchecks/web01/stats"));
  await page.screenshot({ path: `${out}/3-monitoring.png`, fullPage: true });
  await page.getByRole("button", { name: "Healthcheck anlegen", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("Name").fill("Jellyfin");
  await dialog.getByLabel("Typ").selectOption("keyword");
  await dialog.getByLabel("URL").fill("https://media.example.org");
  assert.equal(await dialog.getByRole("button", { name: "Speichern" }).isDisabled(), true, "keyword is required");
  await dialog.getByLabel("Text, der auf der Seite stehen muss").fill("Jellyfin");
  await page.screenshot({ path: `${out}/3-monitoring-check-dialog.png` });
  await dialog.getByRole("button", { name: "Speichern" }).click();
  const created = d.calls.find(([m, p]) => m === "POST" && p === "/ipv64/healthchecks");
  assert.deepEqual(Object.keys(JSON.parse(created[2])).sort(), ["failures_before_alarm", "integrations", "interval_seconds", "keyword", "name", "notify_on_down", "notify_on_up", "retry_interval_seconds", "type", "url", "verify_tls"]);
  await page.getByRole("button", { name: "Integration anlegen", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("Typ").selectOption("telegram");
  assert.equal(await dialog.getByLabel("Bot-Token").getAttribute("type"), "password");
  await page.screenshot({ path: `${out}/3-monitoring-integration.png` });
  await dialog.getByRole("button", { name: "Abbrechen" }).click();
  await page.getByRole("link", { name: "ipv64.net/status/meinnas" }).waitFor();

  // 4 Failover
  await tab(page, "Ausfallschutz");
  await page.getByText("Wartungsseite, wenn dieses NAS ausfällt").first().waitFor();
  await page.getByText("1 geschützte Einträge").waitFor();
  await page.getByRole("button", { name: "Jetzt umschalten" }).click();
  await page.getByRole("dialog").getByText("zeigen in den nächsten Sekunden auf ihren Ersatz").waitFor();
  await page.screenshot({ path: `${out}/4-failover-confirm.png` });
  await page.getByRole("dialog").getByRole("button", { name: "Abbrechen" }).click();
  await page.screenshot({ path: `${out}/4-failover.png`, fullPage: true });

  // 5 Public access
  await tab(page, "Öffentlicher Zugriff");
  await page.getByRole("row", { name: /nas\.ipv64\.de/ }).getByRole("button", { name: "Verwalten" }).click();
  await page.getByText("IPv64 dokumentiert keine Adressbereiche").waitFor();
  await page.getByText("198.51.100.0/24").waitFor();
  await page.screenshot({ path: `${out}/5-public.png`, fullPage: true });

  // 6 Cloud Router
  await tab(page, "Cloud Router");
  await page.getByRole("row", { name: /Zuhause/ }).getByRole("button", { name: "Verwalten" }).click();
  await page.getByText("Den Port vergibt IPv64").waitFor();
  await page.getByRole("button", { name: "Dieses NAS verbinden" }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByText("IPv64 bekommt nur den öffentlichen Schlüssel").waitFor();
  await page.screenshot({ path: `${out}/6-router-connect.png` });
  await dialog.getByRole("button", { name: "Dieses NAS verbinden" }).click();
  await page.getByText("Verbunden. Die Schnittstelle ipv64wg ist aufgebaut.").waitFor();
  await page.screenshot({ path: `${out}/6-router.png`, fullPage: true });

  // 7 Blocklist and notifications
  await tab(page, "Blocklist und Meldungen");
  await page.getByLabel("Blocklist in der Firewall verwenden").click();
  await page.getByRole("dialog").getByRole("button", { name: "Einschalten" }).click();
  await page.getByText("18342").waitFor();
  await page.getByLabel("Konto beobachten").click();
  await page.getByText(/Zuletzt nachgesehen/).waitFor();
  await page.screenshot({ path: `${out}/7-blocklist.png`, fullPage: true });

  // The app editor: publish through IPv64 behind a warning.
  await page.goto(base + "/docker/apps/photos");
  await page.getByRole("heading", { name: "Öffentlich über IPv64" }).waitFor();
  await page.getByRole("button", { name: "Öffentlich machen …" }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByText("Jeder im Internet kann https://nas.ipv64.de aufrufen.").waitFor();
  assert.equal(await dialog.getByRole("button", { name: "Öffentlich machen …" }).isDisabled(), true, "needs the own-login tick");
  await page.screenshot({ path: `${out}/8-app-warning.png` });
  await dialog.getByLabel("Die App verlangt eine eigene Anmeldung").check();
  await dialog.getByRole("button", { name: "Öffentlich machen …" }).click();
  await page.getByText("Die Firewall lässt die Ports der App aus dem Internet zu.").waitFor();
  assert.equal(d.publish.port, 2283);
  assert.equal(d.publish.confirm, true);
  assert.equal(d.publish.firewall, true);
  await page.getByRole("heading", { name: "Öffentlich über IPv64" }).scrollIntoViewIfNeeded();
  await page.screenshot({ path: `${out}/8-app-published.png` });

  // Phone width: no horizontal scroll of the page.
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto(base + "/settings/ipv64?tab=monitoring");
  await page.getByRole("tab", { name: "Überwachung" }).waitFor();
  await page.getByText("Nextcloud").first().waitFor();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
  assert.ok(overflow <= 1, `horizontal scroll ${overflow}`);
  await page.screenshot({ path: `${out}/9-mobile.png`, fullPage: true });
  await ctx.close();

  // Accounts with the role "user" do not get the area.
  {
    const { ctx: userCtx, page: userPage } = await create("user");
    await userPage.goto(base + "/settings");
    assert.equal(await userPage.getByRole("link", { name: "IPv64", exact: true }).count(), 0);
    await userCtx.close();
  }
  assert.deepEqual(errors, []);
  console.log("IPv64 UI checks passed");
} finally {
  await browser.close();
}
