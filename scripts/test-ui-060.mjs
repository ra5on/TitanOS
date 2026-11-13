// Browser checks against isolated API fixtures. No NAS operations or real disks.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-060.mjs
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5179";
const out = process.env.UI_OUTPUT || "out/validation/ui-060";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
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
  next: null,
};
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
    if (path === "/terminal") return json({ enabled: true, confirm: false });
    if (path === "/maintenance")
      return json({
        settings: {
          enabled: true,
          volume: "data",
          timezone: "UTC",
          windowFrom: "00:00",
          windowTo: "00:00",
          jobs: [
            {
              id: "check-data",
              name: "RAID-Prüfung data",
              kind: "raid-check",
              target: "data",
              enabled: true,
              every: "monthly",
              weekdays: [],
              monthDay: 1,
              time: "02:00",
            },
          ],
        },
        jobs: [],
        history: [],
        available: true,
        error: "",
      });
    if (path === "/network") return json({ timezone: "Europe/Berlin" });
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
    if (path === "/vms/imports") return json({ jobs: [] });
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
  if (process.env.DOCKER_ONLY) {
    for (const theme of ["graphit", "hell"])
      for (const width of [1440, 390]) {
        const { ctx, page } = await context("owner", theme, width);
        await page.goto(base + "/docker");
        await page.getByText("media:latest", { exact: true }).waitFor();
        await page.evaluate(async () => {
          await document.fonts.load('400 14px "IBM Plex Sans"');
          await document.fonts.load('500 14px "IBM Plex Sans"');
          await document.fonts.load('600 14px "IBM Plex Sans"');
          await document.fonts.load('400 14px "IBM Plex Mono"');
          await document.fonts.ready;
        });
        await writeFile(
          out + `/docker-${theme}-${width}.json`,
          JSON.stringify(
            await page.evaluate(() =>
              [
                ...document.querySelectorAll("header, main, main *,header *"),
              ].map((e) => {
                const c = getComputedStyle(e);
                const b = e.getBoundingClientRect();
                return [
                  e.tagName,
                  e.textContent.trim().slice(0, 40),
                  c.fontFamily,
                  c.fontSize,
                  c.fontWeight,
                  b.x,
                  b.y,
                  b.width,
                  b.height,
                ];
              }),
            ),
            null,
            2,
          ),
        );
        await page.screenshot({
          path: out + `/docker-${theme}-${width}.png`,
          fullPage: true,
          mask: [page.locator("footer")],
        });
        await ctx.close();
      }
    console.log("Docker comparison screenshots captured.");
  } else {
    // PLAN 71: the settings search sits in the side bar of the
    // Systemsteuerung window; tasks and notifications in the desktop's menu
    // bar; the page of a window shows the connection banner.
    const desk = await context("owner", "graphit", 1440, "desktop");
    {
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
      // The window's page has loaded the timezone page (a new document).
      const timezone = async () => {
        await page.waitForFunction(
          () =>
            document.querySelector("section.win iframe")?.contentWindow
              ?.location.pathname === "/settings/timezone",
        );
        await frame.locator("main").waitFor();
        await page.waitForTimeout(500);
      };
      await search.waitFor({ timeout: 15000 });
      // PLAN 86: the search lists single settings with their place, best first.
      const label = () =>
        side
          .locator(".items .item.result .two > span")
          .evaluateAll((a) => a.map((x) => x.textContent.trim()));
      await search.fill("SSH");
      await side.locator(".items .item.result").first().waitFor();
      assert.equal((await label())[0], "SSH-Zugang einschalten");
      await search.fill("DNS");
      assert.ok(
        (await items()).some((name) => name.startsWith("Netzwerk")),
        "DNS finds Netzwerk",
      );
      await search.fill("Zeitzone");
      assert.deepEqual((await label()).slice(0, 2), [
        "Zeitzone",
        "Datum und Uhrzeit",
      ]);
      await side
        .locator(".items .item.result")
        .filter({ hasText: /^\s*Datum und Uhrzeit/ })
        .click();
      await timezone();
      await frame.locator("select").selectOption("UTC");
      await frame.getByText("Gespeichert", { exact: true }).waitFor();
      failZone = true;
      await frame.locator("select").selectOption("Europe/Berlin");
      await frame.locator("[role=alert]").waitFor();
      assert.equal(await frame.locator("select").inputValue(), "UTC");
      failZone = false;
      await search.fill("unknown-no-match");
      await side.getByText("Nichts gefunden", { exact: true }).waitFor();
      assert.deepEqual(await items(), []);
      await search.fill("Zeitzone");
      await search.press("Enter");
      await frame.locator("select").selectOption("Europe/Berlin");
      await frame.getByText("Gespeichert", { exact: true }).waitFor();
      await search.press("Escape");
      await page.screenshot({
        path: out + "/settings-desktop.png",
        fullPage: true,
      });
      await page.getByRole("button", { name: /Laufende Aufgaben/ }).click();
      const tasks = page.getByRole("dialog", { name: "Laufende Aufgaben" });
      await tasks.waitFor();
      assert.equal(await tasks.locator("progress").count(), 4);
      failTasks = true;
      await tasks
        .getByText(/Ein Teil der Aufgaben konnte nicht aktualisiert/)
        .waitFor({ timeout: 15000 });
      assert.equal(await tasks.locator("progress").count(), 4);
      failTasks = false;
      await tasks
        .getByText(/Ein Teil der Aufgaben konnte nicht aktualisiert/)
        .waitFor({ state: "hidden", timeout: 15000 });
      await page.screenshot({ path: out + "/tasks-desktop.png" });
      await page.keyboard.press("Escape");
      await tasks.waitFor({ state: "hidden" });
      offline = true;
      await frame
        .getByText("Verbindung zum NAS unterbrochen", { exact: true })
        .waitFor({ timeout: 15000 });
      await frame
        .getByText(/Angezeigte Werte sind möglicherweise veraltet/)
        .waitFor();
      await page.screenshot({ path: out + "/connection-lost.png" });
      offline = false;
      await frame
        .getByRole("status")
        .filter({ hasText: "Verbindung zum NAS" })
        .getByRole("button", { name: "Erneut versuchen", exact: true })
        .click();
      await frame
        .getByText("Verbindung zum NAS unterbrochen", { exact: true })
        .waitFor({ state: "hidden" });
      await desk.ctx.close();
    }
    const { ctx, page } = await context();
    await page.goto(base + "/vms");
    await page
      .getByRole("heading", { name: "Linux Server", exact: true })
      .waitFor();
    failVM = true;
    await page.getByRole("button", { name: "Starten", exact: true }).click();
    await page.getByText(/Linux Server: Wird gestartet/).waitFor();
    await page
      .getByRole("button", { name: "Erneut versuchen", exact: true })
      .waitFor();
    failVM = false;
    await page
      .getByRole("button", { name: "Erneut versuchen", exact: true })
      .click();
    await page.getByText(/Aktion für Linux Server angenommen/).waitFor();
    const more = page
      .locator(".dropdown")
      .getByRole("button", { name: "Weitere Aktionen", exact: true });
    await more.click();
    const moreMenu = page.getByRole("menu", {
      name: "Weitere Aktionen",
      exact: true,
    });
    await moreMenu.waitFor();
    await page.keyboard.press("Escape");
    await moreMenu.waitFor({ state: "hidden" });
    assert.equal(await more.getAttribute("aria-expanded"), "false");
    await page.screenshot({ path: out + "/vms-desktop.png", fullPage: true });
    await page.goto(base + "/settings/backups#backup-daily");
    await page.locator("#backup-daily").waitFor();
    await page
      .locator("#backup-daily")
      .getByRole("button", { name: "Abbrechen", exact: true })
      .click();
    await page.getByText(/Auftrag für daily angenommen/).waitFor();
    await page.goto(base + "/docker");
    await page.getByRole("heading", { name: "Docker", exact: true }).waitFor();
    assert.equal(await page.locator("main.readable").count(), 0);
    assert.equal(await page.locator("main .tasks-entry").count(), 0);
    await page.screenshot({
      path: out + "/docker-desktop.png",
      fullPage: true,
    });
    await ctx.close();
    for (const theme of ["graphit", "hell"]) {
      // The phone: the settings overview full screen (PLAN 64/71).
      const { ctx, page } = await context("owner", theme, 390, "desktop");
      await page.goto(base + "/settings");
      await page.locator(".phone-bar").waitFor();
      await page.locator(".overview .sgroup").first().waitFor();
      assert.ok(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
        "settings mobile overflow",
      );
      await page.screenshot({
        path: out + `/settings-${theme}-mobile.png`,
        fullPage: true,
      });
      await page.goto(base + "/vms");
      await page
        .getByRole("heading", { name: "Linux Server", exact: true })
        .waitFor();
      assert.ok(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
        "VM mobile overflow",
      );
      await page.screenshot({
        path: out + `/vms-${theme}-mobile.png`,
        fullPage: true,
      });
      await ctx.close();
    }
    const user = await context("user", "graphit", 1440, "desktop");
    await user.page.goto(base + "/settings");
    const userSide = user.page.locator("section.win nav.side");
    const userSearch = userSide.getByRole("searchbox", {
      name: "Einstellung suchen",
    });
    await userSearch.waitFor({ timeout: 15000 });
    // PLAN 86: a user finds the SSH keys of Mein Konto, never Zugang.
    await userSearch.fill("SSH");
    await userSide.locator(".items .item.result").first().waitFor();
    assert.deepEqual(
      await userSide
        .locator(".items .item.result small")
        .evaluateAll((a) => a.map((x) => x.textContent.trim())),
      ["Mein Konto › Anmeldung"],
    );
    await userSearch.fill("xyzzy");
    await userSide.getByText("Nichts gefunden", { exact: true }).waitFor();
    assert.equal(await userSide.locator(".items .item").count(), 0);
    await user.page.getByRole("button", { name: /Laufende Aufgaben/ }).click();
    const userTasks = user.page.getByRole("dialog", {
      name: "Laufende Aufgaben",
    });
    await userTasks.waitFor();
    assert.equal(await userTasks.locator("progress").count(), 1);
    assert.ok(
      !requests.some(
        ([role, , path]) =>
          role === "user" &&
          ["/backups", "/update/progress", "/vms"].includes(path),
      ),
    );
    await user.ctx.close();
    // A notification's "Details öffnen" in the desktop's menu bar opens the
    // page it names in its window.
    {
      const { ctx, page } = await context("owner", "graphit", 1440, "desktop");
      await page.goto(base + "/");
      await page.locator(".notifications > button").first().click();
      await page
        .getByRole("link", { name: "Details öffnen", exact: true })
        .click();
      const storage = page.frameLocator("section.win iframe").last();
      await storage
        .locator("#volume-data")
        .waitFor({ timeout: 15000 })
        .catch(() => {
          throw new Error(
            "Details öffnen opens Speicher in a window at #volume-data",
          );
        });
      await page.waitForFunction(() =>
        [...document.querySelectorAll("section.win iframe")].some(
          (f) =>
            f.contentWindow?.location.pathname === "/storage" &&
            f.contentWindow.location.hash === "#volume-data",
        ),
      );
      await ctx.close();
    }
    assert.deepEqual(errors, []);
    console.log(
      "UI 0.6.0 browser checks passed (desktop, 390px, Graphit/Hell, roles, errors, tasks, Docker isolation).",
    );
  }
} finally {
  await browser.close();
}
