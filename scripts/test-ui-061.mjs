// Browser checks against isolated API fixtures. No NAS operations or real disks.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-061.mjs
// README_SCREENSHOTS=1 UI_OUTPUT=docs/screenshots/v0.6.1 captures the README gallery.
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5179";
const out = process.env.UI_OUTPUT || "out/validation/ui-061";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
// The tile counts follow the settings hub's own area list instead of a number that goes stale.
// The list lives in settings-search.ts, shared with the command palette.
const settingsSource = await readFile(
  new URL("../web/src/lib/settings-search.ts", import.meta.url),
  "utf8",
);
const areaList =
  settingsSource.match(
    /export const settingsAreas:[^=]*=\s*\[([\s\S]*?)\n\];/,
  )?.[1] ?? "";
const areaEntries = areaList.match(/\{\s*id:[^}]*\}/g) ?? [];
const allAreas = areaEntries.length,
  userAreas = areaEntries.filter(
    (entry) => !/admin:\s*true/.test(entry),
  ).length;
assert.ok(
  allAreas > 0 && userAreas > 0 && userAreas < allAreas,
  "settings areas parsed from settings-search.ts",
);
const time = "2026-10-20T09:00:00Z";
const system = {
  hostname: "titan-test",
  version: "0.6.0",
  kernel: "test",
  uptimeSeconds: 1000,
  cpu: { model: "Test CPU", threads: 8, usagePercent: 12 },
  memory: {
    totalBytes: 16e9,
    usedBytes: 3e9,
    freeBytes: 13e9,
    availableBytes: 13e9,
  },
  temperatures: [],
  accelerators: [],
  image: {
    slot: "a",
    sizeBytes: 1e9,
    overlayUsedBytes: 0,
    overlayTotalBytes: 512e6,
  },
  stick: { present: true, label: "TITANOS" },
  network: null,
  networks: [],
  networkPendingSeconds: 0,
  notificationsLatest: 1,
};
const volume = {
  name: "data",
  level: "raid1",
  filesystem: "ext4",
  mountPoint: "/mnt/data",
  mounted: true,
  state: "checking",
  sizeBytes: 1e12,
  usedBytes: 4e11,
  raidDisks: 2,
  members: [],
  sync: { percent: 32, speedBytesPerSecond: 1e8, remainingSeconds: 500 },
  mismatches: 0,
};
const vm = {
  name: "linux",
  displayName: "Linux Server",
  description: "Test VM",
  template: "linux",
  memoryMiB: 2048,
  vcpus: 2,
  volume: "data",
  disks: [],
  iso: "",
  autostart: true,
  cpuPin: [],
  exclusive: false,
  folders: [],
  tpm: false,
  secureBoot: false,
  pci: [],
  usb: [],
  network: "user",
  job: null,
  state: "stopped",
  available: true,
  cpuPercent: 4,
  memoryBytes: 1e9,
  since: time,
};
const backup = {
  job: {
    id: "daily",
    sources: ["documents"],
    destination: { type: "local", volume: "data", folder: "backups" },
    enabled: true,
    every: "daily",
    weekday: 0,
    time: "02:00",
    keep: { daily: 7, weekly: 2, monthly: 1 },
  },
  secretSet: {},
  state: {
    running: true,
    phase: "backup",
    percent: 25,
    bytesDone: 25,
    bytesTotal: 100,
    lastRun: time,
    lastOK: time,
    lastError: "",
    warning: "",
    bytes: 100,
    restoredTo: "",
  },
  next: "2026-10-21T05:30:00+03:00",
};
let failLoad = false,
  failTerminal = false,
  failConfig = false;
let terminalValue = { enabled: true, confirm: false };
let offline = false,
  failVM = false,
  failZone = false,
  failTasks = false;
const requests = [];
// layout "bare": the page alone, the way its window shows it (PLAN 65);
// "desktop": the desktop with windows (from 900 px) or the phone frame.
async function context(
  role = "owner",
  theme = "graphit",
  width = 1440,
  layout = "bare",
) {
  const ctx = await browser.newContext({
    locale: "de-DE",
    viewport: { width, height: 1000 },
  });
  await ctx.addInitScript(
    ({ theme, layout }) => {
      localStorage.setItem("titanos.locale", "de");
      if (layout === "bare") localStorage.setItem("titanos.layout", "bare");
      else localStorage.removeItem("titanos.layout");
      localStorage.setItem("titanos.appearance", JSON.stringify({ theme }));
    },
    { theme, layout },
  );
  await ctx.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.slice(
      "/api/v1".length,
    );
    const method = route.request().method();
    requests.push([role, method, path]);
    const json = async (data) => route.fulfill({ json: data });
    const fail = async () =>
      route.fulfill({
        status: 500,
        json: { error: { code: "internal", message: "fixture failure" } },
      });
    if (path === "/session")
      return json({
        configAvailable: true,
        setupRequired: false,
        user: { name: "tester", role, twoFactor: false },
        hostname: "titan-test",
      });
    if (path === "/preferences")
      return json({
        appearance: { theme },
        dashboard: null,
        docker: null,
        vnc: null,
        sound: null,
      });
    if (path === "/system") {
      if (offline) return route.abort();
      return json(system);
    }
    if (path === "/storage")
      return json({
        volumes: role === "user" ? [] : [volume],
        disks: [],
        diskSleep: { idleMinutes: 0, available: true, errors: [] },
      });
    if (path === "/notifications")
      return json({
        messages: [
          {
            id: 1,
            time,
            severity: "warning",
            kind: "volume_full",
            subject: "data",
            title: "Volume",
            text: "Full",
          },
        ],
      });
    if (path === "/terminal") {
      if (failLoad || (method === "PUT" && failTerminal)) return fail();
      if (method === "PUT") {
        await new Promise((r) => setTimeout(r, 200));
        terminalValue = { ...terminalValue, ...route.request().postDataJSON() };
      }
      return json(terminalValue);
    }
    if (path === "/system/ssh")
      return json({
        enabled: false,
        running: false,
        fingerprint: "",
        accounts: [],
        allowedNetworks: [],
        homeNetworks: [],
        tailscaleNetworks: ["100.64.0.0/10", "fd7a:115c:a1e0::/48"],
        netbirdNetworks: ["100.64.0.0/10"],
      });
    if (path === "/system/certificate")
      return json({
        kind: "self-signed",
        names: ["nas"],
        issuer: "nas",
        notAfter: "2027-10-20T00:00:00Z",
        fingerprint: "abc",
      });
    if (path === "/config-backup") {
      if (failConfig) return fail();
      return json({
        backup: {
          settings: {
            enabled: true,
            volume: "data",
            keep: 7,
            schedule: "daily",
            time: "03:00",
            weekday: 0,
          },
          target: "data",
          lastCheck: time,
          lastError: "",
          backups: [
            {
              name: "test.tar.gz",
              volume: "data",
              time,
              sizeBytes: 1234,
              hostname: "nas",
              version: "0.6.0",
            },
          ],
          next: "2026-10-21T03:00:00Z",
        },
        volumes: ["data"],
      });
    }
    if (path === "/backups/daily/snapshots")
      return json({
        snapshots: [{ id: "abc123", time, paths: ["documents"], bytes: 100 }],
      });
    if (path === "/network") {
      if (failLoad) return fail();
      return json({ timezone: "Europe/Berlin" });
    }
    if (path === "/system/timezone") {
      await new Promise((r) => setTimeout(r, 250));
      return failZone ? fail() : json({});
    }
    if (path === "/files/jobs")
      return json([
        {
          id: "copy1",
          move: false,
          state: "running",
          from: "share/documents",
          to: "volume/data",
          dir: "backup",
          names: ["report.pdf"],
          files: 1,
          done: 0,
          bytes: 200,
          copied: 100,
          current: "report.pdf",
          error: "",
        },
      ]);
    if (path === "/backups" && failTasks) return fail();
    if (path === "/backups")
      return json({
        smbTargets: [],
        jobs: [backup],
        shares: [{ name: "documents", volume: "data" }],
        volumes: ["data"],
      });
    if (path === "/backups/daily/stop") {
      await new Promise((r) => setTimeout(r, 500));
      backup.state.running = false;
      return json({
        smbTargets: [],
        jobs: [backup],
        shares: [],
        volumes: ["data"],
      });
    }
    if (path === "/backups/application-recovery")
      return json({ operation: null, active: false });
    if (path === "/vm-clones/data") return json([]);
    if (path === "/backups/target")
      return json({
        target: {
          enabled: false,
          volume: "",
          port: 0,
          sources: [],
          listening: [],
          problem: "",
        },
        volumes: ["data"],
      });
    if (path === "/usb") return json([]);
    if (path === "/update/progress")
      return json({
        progress: {
          phase: "download",
          percent: 40,
          file: "filesystem.squashfs",
        },
      });
    if (path === "/vms")
      return json({
        kvm: true,
        bridge: true,
        host: { cpus: 8, memoryBytes: 16e9, usedMemoryBytes: 3e9 },
        vms: [vm],
      });
    if (path === "/vms/linux/start") {
      await new Promise((r) => setTimeout(r, 700));
      return failVM ? fail() : json({});
    }
    if (path === "/vms/linux/snapshots")
      return json({
        snapshots: [],
        schedule: {
          enabled: false,
          time: "03:00",
          days: [],
          keep: 3,
          memory: false,
          allowPause: false,
        },
        pending: false,
        reason: "",
        job: null,
      });
    if (path === "/plugins/tailscale")
      return json({
        active: true,
        state: "Running",
        name: "test.ts.net",
        ips: [],
      });
    if (path === "/docker")
      return json({
        settings: { enabled: true, volume: "data" },
        running: true,
        version: "26.0",
        containers: [
          {
            id: "media",
            name: "media",
            image: "media:latest",
            state: "running",
            since: time,
            ports: [{ host: 8096, container: 8096, protocol: "tcp" }],
            app: "media",
            service: "media",
            managed: true,
            cpuPercent: 4,
            memoryBytes: 512e6,
            cpuHistory: [2, 4, 3, 4],
            webui: "8096",
            publicUrl: "https://media.example.test",
            icon: "",
            updateAvailable: false,
          },
        ],
        apps: ["media"],
        jobs: {},
        updatesChecked: time,
      });
    return json({});
  });
  const page = await ctx.newPage();
  await page.clock.install({ time: new Date("2026-10-20T12:00:00Z") });
  page.on("pageerror", (e) => errors.push(String(e)));
  return { ctx, page };
}
try {
  if (process.env.README_SCREENSHOTS) {
    system.version = "0.6.1";
    backup.state.bytesDone = 256 * 1024 ** 2;
    backup.state.bytesTotal = 1024 ** 3;
    backup.state.bytes = 1024 ** 3;
    const { ctx, page } = await context();
    async function capture(name) {
      await page.evaluate(async () => {
        for (const font of [
          '400 14px "IBM Plex Sans"',
          '500 14px "IBM Plex Sans"',
          '600 14px "IBM Plex Sans"',
          '400 14px "IBM Plex Mono"',
        ])
          await document.fonts.load(font);
        await document.fonts.ready;
      });
      await page.screenshot({
        path: out + "/" + name + ".png",
        fullPage: true,
      });
    }
    for (const [path, selector, name] of [
      ["/settings", ".tiles", "einstellungen"],
      ["/settings/backups", "#backup-next strong", "sicherungen"],
      ["/settings/backups/restore", "#stick-replace", "wiederherstellung"],
      ["/vms", "#vm-title", "vms"],
      ["/docker", "h1", "docker"],
    ]) {
      backup.state.running = path !== "/settings/backups/restore";
      await page.goto(base + path);
      await page.locator(selector).first().waitFor();
      if (path === "/settings/backups")
        await page
          .getByText("Nicht verfügbar", { exact: true })
          .waitFor({ state: "hidden" });
      if (path === "/docker")
        await page.getByText("media:latest", { exact: true }).waitFor();
      await capture(name);
    }
    backup.state.running = true;
    await page.goto(base + "/settings");
    await page.getByRole("searchbox").first().waitFor();
    await page.getByRole("button", { name: /Laufende Aufgaben/ }).click();
    await page.locator("dialog progress").nth(3).waitFor();
    await capture("aufgaben");
    await ctx.close();
    const mobile = await context("owner", "hell", 390);
    await mobile.page.goto(base + "/settings/backups");
    await mobile.page.locator("#backup-next strong").first().waitFor();
    await mobile.page
      .getByText("Nicht verfügbar", { exact: true })
      .waitFor({ state: "hidden" });
    await mobile.page.evaluate(() => document.fonts.ready);
    await mobile.page.screenshot({
      path: out + "/sicherungen-mobil.png",
      fullPage: true,
    });
    await mobile.ctx.close();
    assert.deepEqual(errors, []);
    console.log(
      "README screenshots captured from 0.6.1 with isolated example data.",
    );
  } else {
    // PLAN 71: the Systemsteuerung window's side bar lists the areas and
    // searches them; a hit opens the area at the section found.
    {
      const desk = await context("owner", "graphit", 1440, "desktop");
      const { page } = desk;
      await page.goto(base + "/settings");
      const side = page.locator("section.win nav.side");
      const frame = page.frameLocator("section.win iframe").first();
      const items = () =>
        side
          .locator(".items .item")
          .evaluateAll((a) => a.map((x) => x.textContent.trim()));
      const search = side.getByRole("searchbox", {
        name: "Einstellung suchen",
      });
      await search.waitFor({ timeout: 15000 });
      await frame.locator(".overview .sgroup").first().waitFor();
      assert.equal(
        await frame.locator("main select, main input[type=checkbox]").count(),
        0,
      );
      // PLAN 86: the overview and the eight areas of the menu, with the
      // account and its real role on top; an area opens in the column.
      assert.deepEqual(await items(), [
        "Übersicht",
        "System",
        "Hardware und Energie",
        "Netzwerk und Fernzugriff",
        "Sicherheit und Konten",
        "Benachrichtigungen",
        "Daten und Sicherung",
        "Cluster",
        "Smart Home und KI",
      ]);
      assert.equal(
        (await side.locator(".side-account small").textContent()).trim(),
        "Besitzer",
      );
      const at = (href) =>
        page.waitForFunction(
          (href) => {
            const loc =
              document.querySelector("section.win iframe")?.contentWindow
                ?.location;
            return loc && loc.pathname + loc.search + loc.hash === href;
          },
          href,
          { timeout: 8000 },
        );
      // An area opens its pages in the column (the order of the confirmed
      // draft); a page there opens it, its title is the path "area › page".
      await side.getByRole("button", { name: "System", exact: true }).click();
      await side.locator(".pages").waitFor();
      assert.deepEqual(
        await side
          .locator(".pages .item > span:not(.new)")
          .evaluateAll((a) => a.map((x) => x.textContent.trim())),
        [
          "Über TitanOS",
          "Updates",
          "Darstellung",
          "Datum und Uhrzeit",
          "Dienste",
          "Fernansicht",
        ],
      );
      await side.getByRole("button", { name: "Datum und Uhrzeit" }).click();
      await at("/settings/timezone");
      await frame
        .getByRole("heading", { name: "Datum und Uhrzeit", exact: true })
        .waitFor();
      assert.equal(
        (await frame.locator(".set-path").innerText()).replace(/\s+/g, " "),
        "System › Datum und Uhrzeit",
      );
      assert.equal(
        await side
          .locator('.item[aria-current="page"] > span:not(.new)')
          .evaluate((x) => x.textContent.trim()),
        "Datum und Uhrzeit",
      );
      // The search finds single settings and leads to them (PLAN 86).
      await page.waitForTimeout(600);
      for (const [query, hitText, href] of [
        ["SSH", "SSH-Zugang einschalten", "/settings/access#access-ssh"],
        [
          "Zertifikat",
          "Zertifikat der Weboberfläche",
          "/settings/access#access-cert",
        ],
        ["Zeitzone", "Zeitzone", "/settings/timezone#find=Zeitzone"],
        ["Backup", "Sicherungen", "/settings/backups"],
      ]) {
        await search.fill(query);
        const hit = side
          .locator(".items .item")
          .filter({ hasText: new RegExp("^\\s*" + hitText) })
          .first();
        await hit.waitFor();
        await hit.click();
        await at(href);
        // Let the window's page finish loading before the next search.
        await frame.locator("main h1").first().waitFor({ state: "attached" });
        await page.waitForTimeout(600);
      }
      await search.fill("");
      await page.screenshot({
        path: out + "/settings-desktop.png",
        fullPage: true,
      });
      await desk.ctx.close();
    }
    const { ctx, page } = await context();
    failLoad = true;
    await page.goto(base + "/settings/timezone");
    await page
      .getByRole("button", { name: "Erneut versuchen", exact: true })
      .waitFor();
    failLoad = false;
    await page
      .getByRole("button", { name: "Erneut versuchen", exact: true })
      .click();
    await page.locator("select").selectOption("UTC");
    await page.getByText("Gespeichert", { exact: true }).waitFor();
    await page.clock.runFor(5100);
    assert.equal(
      await page.getByText("Gespeichert", { exact: true }).count(),
      0,
    );
    failZone = true;
    await page.locator("select").selectOption("Europe/Berlin");
    await page.locator("[role=alert]").waitFor();
    assert.equal(await page.locator("select").inputValue(), "UTC");
    failZone = false;
    failLoad = true;
    await page.goto(base + "/settings/access#access-terminal");
    await page
      .locator("#access-terminal")
      .getByRole("button", { name: "Erneut versuchen" })
      .waitFor();
    failLoad = false;
    await page
      .locator("#access-terminal")
      .getByRole("button", { name: "Erneut versuchen" })
      .click();
    // PLAN 78-1: the settings are rows with a switch.
    const enabled = page.getByRole("switch", {
      name: "Terminal und Konsolen erlauben",
      exact: true,
    });
    await enabled.waitFor();
    // A refused change flips the box back, so click instead of uncheck (which insists on the new state).
    failTerminal = true;
    await enabled.click();
    await page.locator("#access-terminal [role=alert]").waitFor();
    assert.equal(await enabled.isChecked(), true);
    failTerminal = false;
    await enabled.uncheck();
    await page
      .locator("#access-terminal")
      .getByText("Gespeichert", { exact: true })
      .waitFor();
    assert.equal(await enabled.isChecked(), false);
    await page.clock.runFor(5100);
    assert.equal(
      await page
        .locator("#access-terminal")
        .getByText("Gespeichert", { exact: true })
        .count(),
      0,
    );
    await page
      .getByRole("switch", {
        name: "Passwort vor dem Herunterladen von Stick-Sicherungen erneut abfragen",
        exact: true,
      })
      .check();
    await page
      .locator("#access-terminal")
      .getByText("Gespeichert", { exact: true })
      .waitFor();
    await page.goto(base + "/settings/backups");
    await page.locator(".settings-page #backup-restore").waitFor();
    await page
      .getByText("Noch keine Sicherung", { exact: true })
      .waitFor({ state: "hidden" });
    await page
      .getByText("Nicht verfügbar", { exact: true })
      .waitFor({ state: "hidden" });
    const expectedNext = await page.evaluate(() =>
      new Intl.DateTimeFormat("de", {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(new Date("2026-10-21T02:30:00Z")),
    );
    assert.equal(
      await page.locator("#backup-next strong").innerText(),
      expectedNext,
      "next backup compares instants across time zone offsets",
    );
    await page.screenshot({
      path: out + "/backups-desktop.png",
      fullPage: true,
    });
    const configReads = requests.filter(
      ([, method, path]) => method === "GET" && path === "/config-backup",
    ).length;
    await page.clock.runFor(61000);
    assert.equal(
      requests.filter(
        ([, method, path]) => method === "GET" && path === "/config-backup",
      ).length,
      configReads,
      "hub must not poll backup archives",
    );
    failConfig = true;
    await page.getByRole("button", { name: "Status aktualisieren" }).click();
    await page.getByRole("button", { name: "Erneut versuchen" }).waitFor();
    assert.ok(await page.getByText("Nicht verfügbar", { exact: true }).count());
    failConfig = false;
    await page.getByRole("button", { name: "Erneut versuchen" }).click();
    await page
      .getByRole("button", { name: "Erneut versuchen" })
      .waitFor({ state: "hidden" });
    backup.state.running = false;
    backup.state.lastError = "fixture backup error";
    await page.getByRole("button", { name: "Status aktualisieren" }).click();
    await page.getByText(/Auftrag\/Aufträge mit Fehlern/).waitFor();
    backup.state.lastError = "";
    await page.goto(base + "/settings/stick");
    await page.waitForURL("**/settings/backups/configuration");
    await page
      .getByRole("heading", { name: "Systemkonfiguration", exact: true })
      .waitFor();
    await page.goto(base + "/settings/backups#backup-daily");
    await page.waitForURL("**/settings/backups/data#backup-daily");
    await page.locator("#backup-daily").waitFor();
    await page.goto(base + "/settings/backups/restore");
    await page.locator("#backup-daily").waitFor();
    assert.equal(
      await page
        .getByRole("button", { name: "Jetzt sichern", exact: true })
        .count(),
      0,
    );
    await page
      .locator("#backup-daily")
      .getByRole("button", { name: "Wiederherstellen …", exact: true })
      .click();
    await page.getByRole("dialog").waitFor();
    await page.keyboard.press("Escape");
    await page.locator("#stick-replace").waitFor();
    await page.screenshot({
      path: out + "/recovery-desktop.png",
      fullPage: true,
    });
    await ctx.close();
    for (const theme of ["graphit", "hell"])
      for (const width of [1440, 390]) {
        const { ctx, page } = await context("owner", theme, width);
        for (const path of [
          "settings",
          "settings/backups",
          "settings/backups/configuration",
          "settings/backups/restore",
          "settings/access",
          "settings/timezone",
        ]) {
          await page.goto(base + "/" + path);
          await page.locator("h1").waitFor();
          await page.evaluate(() => document.fonts.ready);
          if (path === "settings/backups")
            await page
              .getByRole("button", { name: "Status aktualisieren" })
              .waitFor();
          assert.ok(
            await page.evaluate(
              () => document.documentElement.scrollWidth <= innerWidth,
            ),
            path + " overflow",
          );
          await page.screenshot({
            path:
              out +
              "/" +
              path.replaceAll("/", "-") +
              "-" +
              theme +
              "-" +
              width +
              ".png",
            fullPage: true,
          });
        }
        await ctx.close();
      }
    // The window's side bar lists only the areas a user may open (plus the overview).
    const userDesk = await context("user", "graphit", 1440, "desktop");
    await userDesk.page.goto(base + "/settings");
    const userSide = userDesk.page.locator("section.win nav.side");
    await userSide
      .getByRole("searchbox", { name: "Einstellung suchen" })
      .waitFor({ timeout: 15000 });
    await userSide.locator(".items .item").first().waitFor();
    // PLAN 86: the overview and the areas with a page the account may open,
    // under its real role; one with a single page opens it directly.
    assert.deepEqual(
      await userSide
        .locator(".items .item")
        .evaluateAll((a) => a.map((x) => x.textContent.trim())),
      [
        "Übersicht",
        "System",
        "Hardware und Energie",
        "Sicherheit und Konten",
        "Smart Home und KI",
      ],
    );
    assert.equal(
      (await userSide.locator(".side-account small").textContent()).trim(),
      "Benutzer",
    );
    // An area lists only the pages the account may open.
    await userSide.getByRole("button", { name: "System", exact: true }).click();
    await userSide.locator(".pages").waitFor();
    assert.deepEqual(
      await userSide
        .locator(".pages .item > span:not(.new)")
        .evaluateAll((a) => a.map((x) => x.textContent.trim())),
      ["Über TitanOS", "Darstellung"],
    );
    assert.equal(
      await userSide
        .getByRole("button", { name: "Hardware und Energie" })
        .getAttribute("aria-expanded"),
      null,
    );
    await userDesk.ctx.close();
    const user = await context("user");
    for (const path of [
      "backups",
      "backups/data",
      "backups/configuration",
      "backups/restore",
      "timezone",
      "access",
    ]) {
      await user.page.goto(base + "/settings/" + path);
      await user.page
        .getByText("Dafür fehlen diesem Konto die Rechte.", { exact: true })
        .waitFor();
    }
    assert.ok(
      !requests.some(
        ([role, , path]) =>
          role === "user" &&
          [
            "/backups",
            "/config-backup",
            "/network",
            "/system/ssh",
            "/system/certificate",
          ].includes(path),
      ),
    );
    await user.ctx.close();
    assert.deepEqual(errors, []);
    console.log(
      "UI 0.6.1 checks passed: grouping, deep links, legacy links, restore dialog, errors/retry, local feedback, no archive polling, roles and responsive themes.",
    );
  }
} finally {
  await browser.close();
}
