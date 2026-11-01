// The app form (PLAN 21/22, decided 1.11.2026): installing and editing an
// app share one column of lines (name left, field right, hint below) with
// a basic and an advanced view; "Install" shows the log of the job; the
// Docker page has one head line, filters in the status line, a gear menu
// and the actions for all containers below the list. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5173 node scripts/test-ui-appform.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5173";
const out = process.env.UI_OUTPUT || "out/validation/ui-appform";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const time = "2026-10-23T16:00:00Z";
const container = (app, service, o = {}) => ({
  id: app + service,
  name: `${app}-${service}-1`,
  image: o.image || `${service}:latest`,
  state: o.state || "running",
  since: time,
  ports: o.ports || [],
  app,
  service,
  managed: true,
  cpuPercent: o.cpu ?? 1.2,
  memoryBytes: o.mem ?? 150e6,
  cpuHistory: [],
  webui: o.webui || "",
  publicUrl: "",
  icon: "",
  updateAvailable: !!o.update,
});
const containers = [
  container("immich", "server", { webui: "8097", mem: 900e6, ports: [{ host: 8097, container: 2283, protocol: "tcp" }], update: true }),
  container("immich", "postgres", { mem: 400e6 }),
  container("jellyfin", "jellyfin", { webui: "8096", ports: [{ host: 8096, container: 8096, protocol: "tcp" }], update: true }),
  container("vaultwarden", "vaultwarden", { webui: "8081", ports: [{ host: 8081, container: 80, protocol: "tcp" }] }),
  container("testserver", "nginx", { state: "exited", cpu: 0, mem: 0 }),
];
const immichCompose = `services:
  immich-server:
    image: ghcr.io/immich-app/immich-server:v1.135.3
    restart: unless-stopped
    ports: ["8097:2283"]
    volumes: ["/mnt/Data/Fotos:/usr/src/app/upload"]
    environment:
      TZ: Etc/UTC
      DB_PASSWORD: \${DB_PASSWORD}
    labels:
      titanos.webui: "8097"
    x-titanos-hardware: []
  database:
    image: ghcr.io/immich-app/postgres:14
`;
const template = `x-titanos:
  name: Immich
  main: immich-server
  variables:
    DB_PASSWORD: { label: Datenbank-Passwort, secret: generate }
    TZ: { label: Zeitzone }
  paths:
    /usr/src/app/upload: { label: Fotos und Videos, suggest: media }
  ports:
    "2283": { label: Weboberfläche, webui: true }
services:
  immich-server:
    image: ghcr.io/immich-app/immich-server:v1.135.3
    restart: unless-stopped
    ports: ["8096:2283"]
    volumes: ["./upload:/usr/src/app/upload", "./db:/var/lib/postgresql/data"]
    environment:
      TZ: UTC
      DB_PASSWORD: ""
  database:
    image: ghcr.io/immich-app/postgres:14
`;

// Whole page in one piece: a tall window instead of stitched screenshots,
// so the sticky sidebar and the bar with "Apply" stay where they belong.
async function shot(page, file) {
  const size = page.viewportSize();
  const height = await page.evaluate(() => document.documentElement.scrollHeight);
  await page.setViewportSize({ width: size.width, height: Math.max(size.height, height) });
  await page.waitForTimeout(150);
  await page.screenshot({ path: file });
  await page.setViewportSize(size);
}

async function open(width, path, extra = {}) {
  const made = await create("owner", "graphit", width);
  made.data.prefs.appearance = { theme: "graphit", nav: { order: [], hidden: [], layout: "side", magnify: true } };
  const state = {
    jobs: {},
    apps: ["immich", "jellyfin", "vaultwarden", "testserver"],
    posted: [],
    proxyPut: [],
    compose: immichCompose,
    jobPolls: 0,
    started: [],
    stopped: [],
    ...extra,
  };
  const docker = () => ({
    settings: { enabled: true, volume: "Data" },
    running: true,
    version: "27.5.1",
    containers,
    apps: state.apps,
    jobs: state.jobs,
    updatesChecked: time,
  });
  await made.ctx.route("**/api/v1/docker**", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      p = url.pathname.slice("/api/v1".length);
    if (p === "/docker") {
      // The job grows with every look, like titand fills it while it runs.
      const job = state.jobs.photos;
      if (job && job.state === "running") {
        state.jobPolls++;
        job.output = [
          "immich-server Pulled",
          state.jobPolls > 1 ? "database Pulled" : "database Pulling",
          "4f4fb700ef54 Pull complete",
          `a2318d6c47ec Downloading [===>  ]  ${state.jobPolls > 1 ? "890MB" : "520MB"}/890MB`,
          ...(state.jobPolls > 2 ? ["Container photos-database-1 Started", "Container photos-immich-server-1 Started"] : []),
        ].join("\n");
        if (state.jobPolls > 3) job.state = "done";
      }
      return route.fulfill({ json: docker() });
    }
    if (p === "/docker/apps" && req.method() === "POST") {
      state.posted.push(req.postDataJSON());
      state.jobs.photos = { state: "running", output: "" };
      return route.fulfill({ status: 202, body: "" });
    }
    if (p === "/docker/apps/immich") return route.fulfill({ json: { compose: state.compose } });
    if (p.startsWith("/docker/proxy/apps/") && req.method() === "PUT") {
      state.proxyPut.push([p, req.postDataJSON()]);
      return route.fulfill({ json: proxy() });
    }
    if (p === "/docker/proxy") return route.fulfill({ json: proxy() });
    if (p === "/docker/updates/check") return route.fulfill({ json: docker() });
    const action = /^\/docker\/containers\/([^/]+)\/(start|stop)$/.exec(p);
    if (action) {
      state[action[2] === "start" ? "started" : "stopped"].push(action[1]);
      return route.fulfill({ status: 204, body: "" });
    }
    return route.fulfill({ json: {} });
  });
  const proxy = () => ({
    domain: "",
    suffixes: ["titan-test.local"],
    mdns: true,
    defaultLocalName: "titan-test",
    localName: "",
    off: false,
    tailnet: { available: true, keySet: false },
    apps: [{ app: "jellyfin", name: "jellyfin", port: 8096, enabled: true, scheme: "http", urls: [] }],
    targets: [],
  });
  await made.ctx.route("**/api/v1/apps**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice("/api/v1".length);
    if (p === "/apps")
      return route.fulfill({
        json: {
          sources: [{ id: "titanos", name: "TitanOS-Apps", builtin: true, count: 1 }],
          apps: [
            {
              icon: "",
              id: "immich",
              source: "titanos",
              name: "Immich",
              category: "media",
              description: { de: "Fotos und Videos vom Handy auf das NAS sichern.", en: "Back up photos and videos." },
              image: "ghcr.io/immich-app/immich-server",
              services: 2,
            },
          ],
        },
      });
    const tpl = (compose) => ({
      compose,
      timezone: "Europe/Berlin",
      uid: 1000,
      gid: 100,
      appdata: "/mnt/Data/appdata",
      listening: [],
      missingDevices: [],
      cpuMissing: [],
    });
    if (p === "/apps/blank") return route.fulfill({ json: tpl('services:\n  app:\n    image: ""\n    restart: unless-stopped\n') });
    return route.fulfill({ json: tpl(template) });
  });
  await made.ctx.route("**/api/v1/ipv64/**", (route) =>
    route.fulfill({
      json: route.request().url().endsWith("/domains")
        ? { domains: [] }
        : { ports: [8097], firewall: false, zones: { internet: false }, keySource: "" },
    }),
  );
  await made.ctx.route("**/api/v1/hardware/container-devices", (route) =>
    route.fulfill({
      json: [
        { id: "0000:00:02.0", title: "Intel UHD 770", kind: "igpu", vendor: "8086", available: true, reason: "" },
        { id: "0000:01:00.0", title: "NVIDIA GeForce GTX 1060", kind: "nvidia", available: false, reason: "vm" },
      ],
    }),
  );
  await made.page.goto(base + path);
  return { ...made, state };
}

try {
  // Installing: one column, the template's values filled in, port moved off Jellyfin's.
  {
    const { ctx, page, state } = await open(1440, "/apps");
    await page.getByRole("link", { name: /Immich/ }).first().click();
    await page.waitForURL(/\/apps\/install\/titanos\/immich$/);
    await page.getByRole("heading", { name: "Immich installieren" }).waitFor();
    const name = page.locator("#app-name");
    assert.equal(await name.inputValue(), "immich-2", "a second copy gets a name of its own");
    await name.fill("photos");
    assert.match(await page.getByText(/Container-Pfad: \/usr\/src\/app\/upload/).innerText(), /Volume Data/);
    assert.equal(await page.getByRole("textbox", { name: "Weboberfläche" }).inputValue(), "8098", "8096 is Jellyfin's, 8097 Immich's");
    assert.match(await page.locator(".form-row", { hasText: "Weboberfläche" }).innerText(), /8096/);
    // Secrets are made up and hidden; the HTTPS name follows the app's name.
    assert.equal(await page.locator("#var-1").getAttribute("type"), "password");
    assert.equal(await page.locator("#https-name").inputValue(), "photos");
    // Advanced lines appear only on demand.
    assert.equal(await page.getByText("CPU-Kerne").count(), 0);
    await shot(page, out + "/1-installieren.png");
    await page.getByRole("switch", { name: "Erweitert" }).click();
    await page.getByText("CPU-Kerne").waitFor();
    await page.locator(".cpu", { hasText: /^\s*2\s*$/ }).click();
    await page.locator(".cpu", { hasText: /^\s*3\s*$/ }).click();
    await page.locator("#memory").selectOption("4g");
    await page.locator(".form-row", { hasText: "GPU für immich-server" }).getByRole("checkbox", { name: /Intel UHD 770/ }).check();
    await shot(page, out + "/2-installieren-erweitert.png");
    // Adding a device through the dialog.
    await page.getByRole("button", { name: /Weiteren Pfad, Port/ }).click();
    await page.getByRole("radio", { name: "Gerät" }).click();
    await page.getByRole("dialog").getByRole("textbox", { name: "Gerät" }).fill("/dev/ttyUSB0");
    await page.screenshot({ path: out + "/6-geraet-hinzufuegen.png" });
    await page.getByRole("dialog").getByRole("button", { name: "Hinzufügen" }).click();
    await page.getByRole("dialog").waitFor({ state: "hidden" });

    await page.getByRole("button", { name: "Installieren", exact: true }).click();
    await page.getByText("photos wird installiert").waitFor();
    await page.getByText("Images laden:").waitFor();
    await shot(page, out + "/3-installation-laeuft.png");
    await page.getByText("Der Befehl wurde erfolgreich ausgeführt.").waitFor({ timeout: 15000 });
    await shot(page, out + "/4-installation-fertig.png");
    const sent = state.posted[0];
    assert.equal(sent.name, "photos");
    assert.match(sent.compose, /cpuset: "?2,3"?/);
    assert.match(sent.compose, /mem_limit: 4g/);
    assert.match(sent.compose, /\/dev\/ttyUSB0/);
    assert.match(sent.compose, /0000:00:02\.0/);
    assert.match(sent.compose, /"8098:2283"/);
    assert.ok(sent.env.DB_PASSWORD?.length >= 32, "the secret goes apart into .env");
    assert.equal(state.proxyPut.length, 1);
    assert.deepEqual([state.proxyPut[0][0], state.proxyPut[0][1].name, state.proxyPut[0][1].port], ["/docker/proxy/apps/photos", "photos", 8098]);
    await ctx.close();
  }

  // An own container starts from the blank template and asks for the image.
  {
    const { ctx, page } = await open(1440, "/apps/new");
    await page.getByRole("heading", { name: "Eigenen Container einrichten" }).waitFor();
    assert.ok(await page.getByRole("button", { name: "Installieren", exact: true }).isDisabled(), "no image yet");
    await page.getByRole("textbox", { name: "Repository" }).fill("docker.io/library/nginx:1.27");
    assert.ok(await page.getByRole("button", { name: "Installieren", exact: true }).isEnabled());
    await ctx.close();
  }

  // Editing: the same lines, changes marked, advanced lines on demand.
  {
    const { ctx, page } = await open(1440, "/docker/apps/immich");
    await page.getByRole("heading", { name: "immich bearbeiten" }).waitFor();
    await page.getByRole("textbox", { name: "Repository" }).waitFor();
    assert.match(await page.locator(".form-row", { hasText: "Repository" }).innerText(), /neueres Image/);
    await page.getByRole("switch", { name: "Erweitert" }).click();
    await page.locator("#memory").selectOption("6g");
    await page.getByRole("textbox", { name: "TZ" }).fill("Europe/Berlin");
    assert.equal(await page.locator(".form-row.changed").count(), 2);
    assert.match(await page.locator(".apply").innerText(), /Übernehmen/);
    await shot(page, out + "/5-bearbeiten.png");
    await ctx.close();
  }

  // Docker page: one head line, filters in the status line, gear menu, actions below.
  {
    const { ctx, page, state } = await open(1440, "/docker");
    await page.getByRole("button", { name: /2 Updates/ }).waitFor();
    await page.screenshot({ path: out + "/7-docker.png" });
    await page.getByRole("button", { name: "Docker-Einstellungen" }).click();
    for (const item of [/Einstellungen/, /LAN-Netze/, /Automatische Updates/, /Seite anpassen/])
      await page.getByRole("menuitem", { name: item }).waitFor();
    await page.screenshot({ path: out + "/8-docker-zahnrad.png" });
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "Weitere Arten zu installieren" }).click();
    assert.equal(await page.getByRole("menuitem", { name: /Eigener Container/ }).getAttribute("href"), "/apps/new");
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: /2 Updates/ }).click();
    assert.equal(await page.getByRole("button", { name: /2 Updates/ }).getAttribute("aria-pressed"), "true");
    await page.getByRole("button", { name: "Alle starten" }).click();
    await page.waitForTimeout(300);
    assert.deepEqual(state.started, ["testservernginx"]);
    await page.getByRole("button", { name: "Alle stoppen" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Alle stoppen" }).click();
    await page.waitForTimeout(300);
    assert.equal(state.stopped.length, 4);
    await ctx.close();
  }
  {
    const { ctx, page } = await open(390, "/docker");
    await page.getByRole("button", { name: /2 Updates/ }).waitFor();
    assert.ok((await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)) <= 0, "no sideways scroll");
    await shot(page, out + "/9-docker-handy.png");
    await page.goto(base + "/apps/install/titanos/immich");
    await page.getByRole("heading", { name: "Immich installieren" }).waitFor();
    assert.ok((await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)) <= 0, "no sideways scroll");
    await shot(page, out + "/10-installieren-handy.png");
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("ui-appform ok:", out);
} finally {
  await browser.close();
}
