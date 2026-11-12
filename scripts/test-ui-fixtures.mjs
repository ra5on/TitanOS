// Shared isolated API fixtures. No requests reach a NAS.
import { execFileSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// The release the screenshots show: the newest tag of the repository, or
// without tags (shallow CI checkout) the newest docs/releases/vX.Y.Z.md.
// There is no VERSION file; the build takes its version from git too.
export function releaseVersion() {
  const root = join(dirname(fileURLToPath(import.meta.url)), "..");
  const pattern = /^v?(\d+)\.(\d+)\.(\d+)$/;
  try {
    const tag = execFileSync(
      "git",
      ["describe", "--tags", "--abbrev=0", "--match", "v[0-9]*"],
      { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
    ).trim();
    if (pattern.test(tag)) return tag.replace(/^v/, "");
  } catch {
    // No git or no tags: fall back to the release notes.
  }
  try {
    const versions = readdirSync(join(root, "docs", "releases"))
      .map((name) => /^v(\d+)\.(\d+)\.(\d+)\.md$/.exec(name))
      .filter(Boolean)
      .map((m) => m.slice(1, 4).map(Number))
      .sort((a, b) => a[0] - b[0] || a[1] - b[1] || a[2] - b[2]);
    if (versions.length) return versions.at(-1).join(".");
  } catch {
    // Nothing readable: keep a neutral value.
  }
  return "0.0.0";
}

export function fixtureFactory(browser, errors) {
  const time = "2026-10-23T16:00:00Z";
  const volume = {
    name: "Data",
    level: "raid1",
    filesystem: "ext4",
    mountPoint: "/mnt/Data",
    mounted: true,
    state: "ok",
    sizeBytes: 1e12,
    usedBytes: 4e11,
    raidDisks: 2,
    members: [],
    sync: null,
    mismatches: 0,
  };
  const system = {
    hostname: "titan-test",
    version: releaseVersion(),
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
    network: { ipv4: "192.168.1.20/24" },
    networks: [],
    networkPendingSeconds: 0,
    notificationsLatest: 0,
  };
  const share = (name, volume = "Data") => ({
    name,
    volume,
    smb: true,
    nfs: { enabled: false, clients: [] },
    access: { alex: "rw" },
    guest: false,
    browseable: true,
    recycle: { enabled: true, days: 30 },
    timeMachine: { enabled: false, sizeGB: 0 },
    path: `/mnt/${volume}/${name}`,
    available: true,
  });
  const container = (service, state = "running") => ({
    id: service,
    name: "photos-" + service + "-1",
    image: service + ":test",
    state,
    since: time,
    ports: [],
    app: "photos",
    service,
    managed: true,
    cpuPercent: 2,
    memoryBytes: 128e6,
    cpuHistory: [],
    webui: service === "web" ? "2283" : "",
    publicUrl: service === "web" ? "https://photos.example.test" : "",
    icon: "",
    updateAvailable: false,
  });
  const tailnetDevice = (id, name, online, extra = {}) => ({
    id,
    name,
    dnsName: name + ".example-tailnet.ts.net",
    hostName: name,
    ipv4: ["100.101.1." + (10 + name.length)],
    ipv6: [],
    online,
    lastSeen: null,
    os: "linux",
    exitNodeOption: false,
    exitNode: false,
    routes: [],
    self: false,
    ...extra,
  });
  const netbirdPeer = (id, name, online, extra = {}) => ({
    id,
    name,
    fqdn: name + ".netbird.cloud",
    ipv4: "100.92.1." + (10 + name.length),
    ipv6: "",
    online,
    lastSeen: null,
    networks: [],
    self: false,
    ...extra,
  });
  // GET /plugins/netbird of a fresh installation: active, not registered.
  const netbirdStatus = () => ({
    settings: {
      managementUrl: "",
      acceptRoutes: false,
      serveRoutes: false,
      acceptDNS: false,
    },
    setupKeySet: false,
    connect: false,
    active: true,
    readOnly: false,
    state: "NeedsLogin",
    version: "0.81.5",
    fqdn: "",
    ips: [],
    management: "",
    managementConnected: false,
    signalConnected: false,
    peersConnected: 0,
    peersTotal: 0,
    health: [],
  });
  async function context(
    role = "owner",
    theme = "graphit",
    width = 1440,
    locale = "de",
    // Extra context options, e.g. phone emulation (isMobile, hasTouch, deviceScaleFactor).
    options = {},
  ) {
    const ctx = await browser.newContext({
      locale: "de-DE",
      viewport: { width, height: 1000 },
      ...options,
    });
    await ctx.addInitScript(
      ({ theme, locale }) => {
        localStorage.setItem("titanos.locale", locale);
        // The page scripts check pages in the classic frame; the desktop
        // mode (PLAN 61) has scripts/test-ui-desktop.mjs.
        localStorage.setItem("titanos.layout", "classic");
        localStorage.setItem("titanos.appearance", JSON.stringify({ theme }));
      },
      { theme, locale },
    );
    const data = {
      system: structuredClone(system),
      maintenance: {
        settings: {
          enabled: false,
          volume: "Data",
          timezone: "UTC",
          windowFrom: "00:00",
          windowTo: "00:00",
          jobs: [],
        },
        jobs: [],
        history: [],
        available: true,
        error: "",
      },
      // Tools › Scripts, shaped like GET /scripts; scriptContent, scriptRuns
      // and scriptCalls belong to it (see scriptFixtures).
      ...scriptFixtures(time),
      growStart: 0,
      importCount: 0,
      restoreCount: 0,
      cloneCount: 0,
      failRegister: false,
      requestsOps: [],
      shares: [share("Documents"), share("Media"), share("Projects", "Fast")],
      prefs: { appearance: { theme }, shares: null, docker: null, files: null },
      deleted: 0,
      failDelete: false,
      failSave: false,
      failDocker: false,
      webMissing: false,
      requests: [],
      putShares: [],
      putDocker: [],
      files: [],
      filePaths: [],
      backups: [],
      // GET /system/data: an SSD volume that never sleeps and an HDD volume
      // whose disks may sleep.
      sysdata: {
        volume: "",
        resolved: "nvme",
        sleepKnown: true,
        volumes: [
          { name: "Backup", mounted: true, sleeps: true },
          { name: "nvme", mounted: true, sleeps: false },
        ],
        functions: [
          ["config", 39845888, true],
          ["logs", 222298112, true],
          ["activity", 6291456, true],
          ["notify", 81920, true],
          ["smart", 3145728, true],
          ["monitoring", null, false],
        ].map(([id, sizeBytes, enabled]) => ({
          id,
          choice: "",
          volume: "nvme",
          source: "automatic",
          enabled,
          moving: false,
          sizeBytes,
          sizeAt: sizeBytes === null ? null : time,
        })),
      },
      operations: null,
      recovery: null,
      exports: 0,
      // Settings › Updates, shaped like GET /update. checkEvery is missing
      // as in a file from before the field; the routes below apply the
      // server's rules for it.
      update: {
        version: "0.24.0",
        release: true,
        slots: [],
        settings: {
          channel: "stable",
          autoInstall: true,
          installTime: "03:30",
        },
        canVerify: true,
        available: null,
        lastCheck: time,
        nextCheck: "2026-10-24T16:00:00Z",
        checkError: "",
        progress: {
          phase: "",
          file: "",
          percent: 0,
          speedBytesPerSecond: 0,
          error: "",
        },
        pending: null,
        canRollback: false,
        unconfirmed: false,
        base: "0.24.0",
        kind: "",
        app: { current: "", previous: "", trial: false, canRollback: false },
        variant: "stable",
        bootLoader: "systemd-boot",
      },
      updateChecks: 0,
      updatePuts: [],
      // Shaped like GET /plugins/tailscale/devices; null = plugin not active.
      tailnet: {
        active: true,
        state: "Running",
        updated: time,
        devices: [
          tailnetDevice("nSELF1CNTRL", "titan", true, {
            self: true,
            routes: ["192.168.10.0/24"],
          }),
          tailnetDevice("nLAPTOP2CNTRL", "alex-laptop", true, {
            os: "macOS",
            lastSeen: time,
          }),
          tailnetDevice("nEXIT3CNTRL", "router", false, {
            exitNodeOption: true,
            lastSeen: time,
          }),
        ],
      },
      // Shaped like GET /plugins/netbird/peers; null = plugin not active.
      netbirdPeers: {
        active: true,
        state: "Connected",
        updated: time,
        peers: [
          netbirdPeer("dGl0YW4tc2VsZg==", "titan", true, {
            self: true,
            networks: ["192.168.10.0/24"],
          }),
          netbirdPeer("YWxleC1sYXB0b3A=", "alex-laptop", true, {
            connection: "p2p",
            lastSeen: time,
          }),
          netbirdPeer("b2ZmaWNlLXJvdXRlcg==", "office-router", true, {
            connection: "relayed",
            lastSeen: time,
            networks: ["10.20.0.0/16"],
          }),
          netbirdPeer("cGhvbmU=", "phone", false),
        ],
      },
      // GET /plugins/netbird; the handlers below change it like titand.
      netbird: netbirdStatus(),
      netbirdCalls: [],
      // role "setup": a fresh stick. The session asks for the owner account
      // until POST /setup; setupBackups is what GET /setup/backups finds on
      // the disks (scripts may fill it), setupCalls records what was sent.
      setupDone: role !== "setup",
      setupBackups: [],
      setupCalls: [],
      // PLAN 63: the remote view's settings and state (GET/PUT
      // /viewer/settings, GET /viewer/status); viewerPuts records saves.
      viewer: {
        settings: {
          gpu: "auto",
          quality: "auto",
          fps: 60,
          audio: true,
          volume: "",
        },
        status: {
          image: {
            state: "ready",
            version: "0.60.0",
            bytes: 0,
            size: 0,
            progress: 0,
          },
          gpu: "vaapi",
          gpus: [
            {
              id: "/dev/dri/renderD128",
              kind: "vaapi",
              vendor: "8086",
              title: "Intel UHD 770",
              available: true,
            },
          ],
          sessions: [
            {
              user: "alex",
              app: "photos",
              state: "open",
              since: "2026-11-06T08:00:00Z",
              windows: 1,
              gpu: "vaapi",
              memoryBytes: 380e6,
            },
          ],
          memory: {
            availableBytes: 8e9,
            reserveBytes: 734003200,
            perViewBytes: 380e6,
            more: 9,
          },
          idleMinutes: 15,
        },
      },
      viewerPuts: [],
      // GET /network for the bridge step of /welcome (setup variant only).
      network: {
        config: { bridge: false },
        configured: false,
        bridgeReady: false,
        pending: null,
        pendingSeconds: 0,
        interfaces: [{ name: "enp3s0", carrier: true }],
      },
    };
    await ctx.route("**/api/v1/**", async (route) => {
      const url = new URL(route.request().url()),
        path = url.pathname.slice("/api/v1".length),
        method = route.request().method();
      data.requests.push([method, path]);
      const json = (value) => route.fulfill({ json: value });
      const fail = () =>
        route.fulfill({
          status: 500,
          json: {
            error: { code: "config_write_failed", message: "fixture error" },
          },
        });

      if (path === "/viewer/settings") {
        if (method === "PUT") {
          data.viewer.settings = route.request().postDataJSON();
          data.viewerPuts.push(data.viewer.settings);
        }
        return json(data.viewer.settings);
      }
      if (path === "/viewer/status") return json(data.viewer.status);
      if (path === "/viewer/image" && method === "POST") {
        data.viewer.status.image = {
          ...data.viewer.status.image,
          state: "downloading",
          size: 400e6,
          bytes: 0,
          progress: 0,
        };
        return json(data.viewer.status.image);
      }
      if (path === "/maintenance") {
        if (method === "PUT") {
          const settings = route.request().postDataJSON();
          settings.jobs = settings.jobs.map((j, i) => ({
            ...j,
            id: j.id || "job-" + i,
          }));
          data.maintenance.settings = settings;
          data.maintenance.jobs = settings.jobs.map((job) => ({
            job,
            state: { running: false, blocked: false, error: "", lastOK: null },
            next: time,
          }));
        }
        return json(data.maintenance);
      }
      if (path.startsWith("/maintenance/")) {
        const id = path.split("/")[2];
        const view = data.maintenance.jobs.find((v) => v.job.id === id);
        view.state.lastOK = time;
        data.maintenance.history.push({
          id,
          name: view.job.name,
          kind: view.job.kind,
          target: view.job.target,
          started: time,
          finished: time,
          error: "",
        });
        return json(data.maintenance);
      }
      if (path === "/scripts" || path.startsWith("/scripts/"))
        return scriptRoute(data, path, method, route, time);
      // Activity log: tests may replace data.activity; queries are kept.
      if (path === "/activity") {
        const log = (data.activity ??= {
          settings: { enabled: true, volume: "", days: 90, maxMiB: 64 },
          entries: [
            {
              at: time,
              user: "alex",
              via: "web",
              ip: "192.168.1.20",
              action: "POST /api/v1/files/delete",
              target: "Media/alt.mkv",
              status: 200,
              ok: true,
            },
            {
              at: time,
              user: "sam",
              via: "web",
              ip: "192.168.1.31",
              action: "login_failed",
              ok: false,
            },
            {
              at: time,
              user: "alex",
              via: "token",
              action: "PUT /api/v1/system/timezone",
              status: 200,
              ok: true,
            },
          ],
          queries: [],
        });
        const status = {
          settings: log.settings,
          volume: log.settings.enabled ? "Data" : "",
          pending: 0,
          dropped: 0,
          error: "",
        };
        if (method === "PUT") {
          log.settings = route.request().postDataJSON();
          return json({ ...status, settings: log.settings });
        }
        log.queries.push(Object.fromEntries(url.searchParams));
        if (url.searchParams.get("format")) {
          data.exports++;
          return route.fulfill({
            body: "time,user\n",
            contentType: "text/csv",
          });
        }
        const user = url.searchParams.get("user"),
          failed = url.searchParams.get("failed") === "1";
        const entries = log.entries.filter(
          (e) => (!user || e.user === user) && (!failed || !e.ok),
        );
        return json({ entries, more: false, stored: true, status });
      }
      if (path === "/monitoring") {
        if (method === "PUT") data.monitor = route.request().postDataJSON();
        return json({
          settings: data.monitor ?? {
            enabled: false,
            volume: "Data",
            days: 7,
            maxMiB: 8,
          },
          last: time,
          error: "",
        });
      }
      if (path === "/monitoring/trends")
        return json([
          {
            volume: "Data",
            known: false,
            warning: false,
            daysTo90: null,
            daysToFull: null,
            hours: 3,
          },
        ]);
      if (path === "/monitoring/export") {
        data.exports++;
        return route.fulfill({
          body:
            url.searchParams.get("format") === "csv"
              ? "at,gap\n2026-10-23T16:00:00Z,false\n"
              : "[]",
          contentType: "text/plain",
        });
      }
      if (path === "/monitoring/history")
        return json(
          Array.from({ length: 6 }, (_, i) => ({
            at: new Date(
              Date.parse(time) + i * 60000 + (i >= 3 ? 600000 : 0),
            ).toISOString(),
            gap: i === 3,
            cpu: 10 + i,
            ramTotal: 16e9,
            ramAvailable: 12e9,
            disks: [{ name: "vda", readBps: 1e6, writeBps: 2e6, latencyMs: 3 }],
            volumes: [{ name: "Data", used: 4e11, total: 1e12 }],
            networks: [{ name: "eth0", receive: 1e6, send: 3e6 }],
          })),
        );
      if (path === "/storage/import" && method === "GET")
        return json([
          {
            id: "md:fixture",
            filesystem: "ext4",
            filesystemUUID: "fs-fixture",
            arrayUUID: "md-fixture",
            level: "raid1",
            disks: ["vde", "vdf"],
            size: 8e12,
            fingerprint: "fingerprint",
            reason: "",
          },
        ]);
      if (path === "/storage/import/preview")
        return json({
          candidate: {
            id: "md:fixture",
            fingerprint: "fingerprint",
            filesystemUUID: "fs-fixture",
          },
          folders: ["Photos", "Documents"],
          readOnly: true,
          truncated: false,
        });
      if (path === "/storage/import" && method === "POST") {
        data.importCount++;
        if (data.failRegister) return fail();
        return route.fulfill({ status: 201 });
      }
      if (path === "/storage/growth")
        return json({ pending: false, operation: {}, error: "" });
      if (path.endsWith("/growth/preview"))
        return json({
          volume: "Data",
          diskSerial: "TEST4",
          recoveryVolume: "Safe",
          currentBytes: 16e12,
          plannedBytes: 24e12,
          originalDisks: 3,
          targetDisks: 4,
          phase: "preview",
        });
      if (path.endsWith("/growth")) {
        data.growStart++;
        data.requestsOps.push(route.request().postDataJSON());
        return route.fulfill({ status: 202 });
      }
      if (path.endsWith("/usage"))
        return json({
          at: time,
          entries: [
            {
              name: "Documents",
              bytes: 1024,
              allocated: 4096,
              files: 2,
              incomplete: false,
              error: "",
            },
          ],
          scanned: 3,
          incomplete: false,
        });
      if (path === "/storage/volumes/Fast/snapshots")
        return json([{ name: "before" }]);
      if (path === "/storage/volumes/Fast/snapshots/before/files")
        return json({
          entries: [
            { path: "photo.jpg", directory: false, bytes: 100, modified: time },
          ],
          next: 0,
        });
      if (path.endsWith("/files/preview"))
        return json({
          uuid: "12345678-1234-1234-1234-123456789abc",
          files: 1,
          bytes: 100,
        });
      if (path.endsWith("/files/restore")) {
        data.restoreCount++;
        return json({
          folder: "restored/snapshot-fixture",
          files: 1,
          bytes: 100,
        });
      }
      if (path === "/backups/application-recovery")
        return json({ operation: data.recovery, active: false });
      if (path.startsWith("/vm-clones/")) return json([]);
      if (path === "/backups/coverage")
        return json(data.coverage ?? { items: [], unavailable: [] });
      if (path.startsWith("/backups/") && path.endsWith("/snapshots"))
        return json({
          snapshots: [
            {
              id: "abcdef01",
              time,
              bytes: 1024,
              paths: ["/mnt/Data/appdata/app"],
            },
          ],
        });
      if (path.endsWith("/applications/preview"))
        return json({
          snapshot: {
            id: "abcdef01",
            time,
            bytes: 1024,
            paths: ["/mnt/Data/appdata/app"],
          },
          targetVolume: "Safe",
          freeBytes: 1e12,
          currentSelection: true,
          applications: [{ kind: "docker", name: "app", method: "stop" }],
        });
      if (path.endsWith("/applications/restore")) {
        data.backups[0].state.applicationRestore = {
          at: time,
          snapshot: "abcdef01",
          target: "/mnt/Safe/restored/application-fixture",
          verified: true,
          error: "",
        };
        return route.fulfill({
          status: 202,
          json: { target: "/mnt/Safe/restored/application-fixture" },
        });
      }
      if (path === "/operations")
        return json(
          data.operations ?? {
            transfers: [],
            maintenance: data.maintenance,
            backups: [],
            docker: {},
            vms: [],
            diskTests: [],
            growth: { pending: false, operation: {} },
          },
        );
      if (path.endsWith("/clone")) {
        data.cloneCount++;
        return route.fulfill({ status: 202 });
      }

      if (path === "/session")
        return json({
          configAvailable: true,
          setupRequired: !data.setupDone,
          user: data.setupDone
            ? {
                name: "alex",
                role: role === "setup" ? "owner" : role,
                twoFactor: false,
              }
            : null,
          hostname: "titan-test",
        });
      if (path === "/setup" && method === "POST") {
        const body = route.request().postDataJSON();
        data.setupCalls.push(["setup", body]);
        data.setupDone = true;
        return json({ name: body.name, role: "owner" });
      }
      if (path === "/setup/backups")
        return json({ backups: data.setupBackups });
      if (path.startsWith("/setup/restore")) {
        data.setupCalls.push(["restore", path]);
        return json({});
      }
      // Only for the setup variant; other scripts keep their own answers.
      if (role === "setup" && path === "/network" && method === "GET")
        return json({ network: data.network });
      if (role === "setup" && path === "/network/bridge" && method === "POST") {
        data.network = {
          ...data.network,
          config: { bridge: true },
          configured: true,
          bridgeReady: true,
        };
        return json({ network: data.network });
      }
      if (path === "/preferences") return json(data.prefs);
      if (path === "/preferences/shares") {
        data.prefs.shares = route.request().postDataJSON();
        data.putShares.push(data.prefs.shares);
        return route.fulfill({ status: 204 });
      }
      if (path === "/preferences/docker") {
        data.prefs.docker = route.request().postDataJSON();
        data.putDocker.push(data.prefs.docker);
        return route.fulfill({ status: 204 });
      }
      if (path === "/preferences/dashboard") {
        if (data.failSave) return fail();
        data.prefs.dashboard = route.request().postDataJSON();
        return route.fulfill({ status: 204 });
      }
      if (path === "/homeassistant/settings") {
        // A fresh NAS: Home Assistant not connected (scripts may set data.ha).
        data.ha ??= {
          settings: {
            enabled: false,
            url: "",
            fingerprint: "",
            domains: [],
            entities: [],
            mqtt: {
              enabled: false,
              host: "",
              port: 1883,
              username: "",
              tls: false,
              prefix: "homeassistant",
              groups: [],
              commands: [],
              interval: 30,
            },
          },
          tokenSet: false,
          mqttPasswordSet: false,
          domains: ["light", "switch", "sensor"],
          sensorGroups: ["system", "storage"],
          commandGroups: [],
          status: {
            watching: false,
            watchError: "",
            mqtt: false,
            mqttError: "",
          },
        };
        if (method === "PUT") data.ha.settings = route.request().postDataJSON();
        return json(data.ha);
      }
      if (path === "/system/power-button")
        return json({
          settings: {
            action:
              method === "PUT"
                ? route.request().postDataJSON().action
                : "poweroff",
          },
          readOnly: false,
        });
      if (path === "/notifications/settings")
        return json({
          settings: {
            language: "de",
            minSeverity: "warning",
            email: {
              enabled: false,
              host: "",
              port: 587,
              security: "starttls",
              username: "",
              from: "",
              to: "",
            },
            ntfy: { enabled: false, url: "" },
            telegram: { enabled: false, chatID: "" },
            discord: { enabled: false },
            homeAssistant: { enabled: false, service: "" },
            digest: { enabled: false, time: "08:00", errorsNow: true },
          },
          secretSet: {},
        });
      // Appearance is kept like titand does, so a reload shows what was applied.
      if (path === "/preferences/appearance" && method === "PUT") {
        data.prefs.appearance = route.request().postDataJSON();
        return route.fulfill({ status: 204 });
      }
      if (path.startsWith("/preferences/"))
        return route.fulfill({ status: 204 });
      if (path === "/system") return json(data.system);
      // Share quotas per volume; PUT .../quotas switches them like titand.
      const quotas = (data.volumeQuotas ??= {
        Data: "active",
        Fast: "unsupported",
        Safe: "off",
      });
      const switched = /^\/storage\/volumes\/([^/]+)\/quotas$/.exec(path);
      if (switched && method === "PUT") {
        const on = route.request().postDataJSON().enabled;
        quotas[decodeURIComponent(switched[1])] = on ? "pending" : "off";
        return route.fulfill({ status: 204 });
      }
      if (path === "/storage/tests")
        return json(data.storageTests ?? { tests: [], notices: [] });
      if (path === "/storage")
        return json({
          volumes: [
            { ...volume, level: "raid5", shareQuotas: quotas.Data },
            {
              ...volume,
              name: "Fast",
              filesystem: "btrfs",
              shareQuotas: quotas.Fast,
            },
            { ...volume, name: "Safe", shareQuotas: quotas.Safe },
          ],
          disks: [
            {
              identity: "disk-id",
              name: "vdd",
              serial: "TEST4",
              model: "Fixture disk",
              sizeBytes: 8e12,
              role: "free",
              mounted: false,
              testing: false,
              smart: null,
            },
          ],
          diskSleep: { idleMinutes: 0, available: true, errors: [] },
        });
      if (false)
        return json({
          volumes: role === "user" ? [] : [volume, { ...volume, name: "Fast" }],
          disks: [],
          diskSleep: { idleMinutes: 0, available: true, errors: [] },
        });
      if (path === "/users")
        return json([
          { name: "alex", role: "owner" },
          { name: "sam", role: "user" },
        ]);
      if (path === "/shares") {
        if (method === "POST") {
          if (data.failSave) return fail();
          const s = route.request().postDataJSON();
          data.shares.push({
            ...s,
            path: `/mnt/${s.volume}/${s.name}`,
            available: true,
          });
          return route.fulfill({ status: 201 });
        }
        return json(data.shares);
      }
      if (path.startsWith("/shares/")) {
        if (method === "DELETE") {
          data.deleted++;
          if (data.failDelete) return fail();
          data.shares = data.shares.filter(
            (s) => s.name !== decodeURIComponent(path.split("/").at(-1)),
          );
          return route.fulfill({ status: 204 });
        }
        if (data.failSave) return fail();
        (data.shareSaves ??= []).push(route.request().postDataJSON());
        return route.fulfill({ status: 204 });
      }
      if (path === "/docker") {
        if (data.failDocker) return fail();
        return json({
          settings: { enabled: true, volume: "Data" },
          running: true,
          version: "test",
          containers:
            data.dockerContainers ??
            (data.webMissing
              ? [container("db", "exited")]
              : [container("web"), container("db", "exited")]),
          apps: data.dockerApps ?? ["photos"],
          jobs: {},
          updatesChecked: time,
        });
      }
      // Shaped like GET /plugins; tests set data.plugins.
      if ((path === "/plugins" || path === "/plugins/catalog") && data.plugins)
        return json(data.plugins);
      // Hardware recommendations: dismiss one, show dismissed ones again.
      if (path.startsWith("/plugins/recommendations/") && data.plugins) {
        (data.recommendationCalls ??= []).push([method, path]);
        const name = decodeURIComponent(path.split("/")[3]);
        if (method === "POST" && path.endsWith("/dismiss")) {
          data.hiddenRecommendations = [
            ...(data.hiddenRecommendations ?? []),
            ...data.plugins.recommendations.filter((r) => r.plugin === name),
          ];
          data.plugins = {
            ...data.plugins,
            recommendations: data.plugins.recommendations.filter(
              (r) => r.plugin !== name,
            ),
            dismissed: (data.plugins.dismissed ?? 0) + 1,
          };
        } else if (method === "DELETE") {
          data.plugins = {
            ...data.plugins,
            recommendations: [
              ...data.plugins.recommendations,
              ...(data.hiddenRecommendations ?? []),
            ],
            dismissed: 0,
          };
          data.hiddenRecommendations = [];
        }
        return json(data.plugins);
      }
      // Community plugins: a link is inspected, then installed with its name.
      if (
        path === "/plugins/external/inspect" &&
        method === "POST" &&
        data.linkPreview
      )
        return json(data.linkPreview);
      if (path === "/plugins/external" && method === "POST" && data.plugins) {
        (data.externalInstalls ??= []).push(route.request().postDataJSON());
        data.plugins = data.pluginsAfterExternal ?? data.plugins;
        return json(data.plugins);
      }
      if (
        path.endsWith("/update") &&
        path.startsWith("/plugins/") &&
        method === "POST"
      ) {
        (data.pluginUpdates ??= []).push([
          path,
          route.request().postDataJSON(),
        ]);
        if (data.failPluginUpdate)
          return route.fulfill({
            status: 409,
            json: { error: { code: "plugin_busy", message: "fixture busy" } },
          });
        data.plugins = data.pluginsAfterUpdate ?? data.plugins;
        return json(data.plugins);
      }
      if (path === "/plugins/netbird/peers")
        return json(
          data.netbirdPeers ?? {
            active: false,
            state: "inactive",
            peers: [],
            updated: time,
          },
        );
      if (path.startsWith("/plugins/netbird") && method !== "GET") {
        const body = route.request().postDataJSON();
        data.netbirdCalls.push([method, path, body]);
        const nb = data.netbird;
        if (path === "/plugins/netbird/setup-key")
          nb.setupKeySet = body.setupKey !== "";
        else if (path === "/plugins/netbird") {
          nb.settings = body;
          nb.pendingUntil = "2026-10-23T16:02:00Z";
        } else if (path.endsWith("/connect")) {
          if (!nb.setupKeySet)
            return route.fulfill({
              status: 409,
              json: {
                error: { code: "netbird_setup_key", message: "fixture" },
              },
            });
          Object.assign(nb, {
            connect: true,
            state: "Connected",
            fqdn: "titan.netbird.cloud",
            ips: ["100.92.10.5"],
            management: "https://api.netbird.io:443",
            managementConnected: true,
            peersConnected: 2,
            peersTotal: 3,
          });
        } else if (path.endsWith("/disconnect"))
          Object.assign(nb, { connect: false, state: "Idle" });
        else if (path.endsWith("/confirm") || path.endsWith("/undo"))
          delete nb.pendingUntil;
        return json(nb);
      }
      if (path === "/plugins/netbird") return json(data.netbird);
      if (path === "/plugins/tailscale/devices")
        return json(
          data.tailnet ?? {
            active: false,
            state: "inactive",
            devices: [],
            updated: time,
          },
        );
      if (path === "/plugins/tailscale")
        return json({
          active: true,
          state: "Running",
          name: "nas.example.ts.net",
          ips: ["100.64.1.2"],
        });
      if (path === "/notifications") return json({ messages: [] });
      if (path === "/terminal") return json({ enabled: true });
      if (path === "/session/confirmed") return json({ confirmed: true });
      // Formatting drives: tests set data.usbDisks and data.formatError and
      // read data.formats; data.usbJob is the job the NAS reports.
      if (path === "/usb/disks" && method === "GET")
        return json({ disks: data.usbDisks ?? [], job: data.usbJob ?? null });
      // Partition editor: tests set data.usbLayouts (disk ID -> layout) and
      // read data.partitionActions; each action starts a running job.
      const partitionRoute = path.match(
        /^\/usb\/disks\/([^/]+)\/(partitions|table)(?:\/(\d+)(?:\/(\w+))?)?$/,
      );
      if (partitionRoute) {
        const [, rawId, kind, number, verb] = partitionRoute;
        const id = decodeURIComponent(rawId);
        const layout = data.usbLayouts?.[id];
        if (!layout)
          return route.fulfill({
            status: 404,
            json: {
              error: { code: "usb_not_found", message: "usb: no such drive" },
            },
          });
        if (method === "GET")
          return json({ ...layout, job: data.usbJob ?? null });
        const body = method === "DELETE" ? {} : route.request().postDataJSON();
        const action =
          kind === "table"
            ? "table"
            : !number
              ? "create"
              : method === "DELETE"
                ? "delete"
                : verb;
        (data.partitionActions ??= []).push({
          id,
          action,
          ...(number ? { number: Number(number) } : {}),
          ...body,
        });
        data.usbJob = {
          disk: id,
          device: layout.disk.device,
          name: layout.disk.model,
          action,
          partition: number ? layout.disk.device + number : "",
          filesystem: body.filesystem ?? "",
          label: body.label ?? "",
          scheme: layout.table,
          phase: "partition",
          started: time,
        };
        return json({ disks: data.usbDisks ?? [], job: data.usbJob });
      }
      if (
        path.startsWith("/usb/disks/") &&
        path.endsWith("/format") &&
        method === "POST"
      ) {
        const body = route.request().postDataJSON();
        (data.formats ??= []).push({
          id: decodeURIComponent(path.split("/")[3]),
          ...body,
        });
        if (data.formatError)
          return route.fulfill({
            status: 409,
            json: {
              error: {
                code: data.formatError,
                message: "usb: this drive cannot be formatted now: inuse",
              },
            },
          });
        const disk = data.usbDisks.find(
          (d) => d.id === decodeURIComponent(path.split("/")[3]),
        );
        data.usbJob = {
          disk: disk.id,
          device: disk.device,
          name: disk.model,
          filesystem: body.filesystem,
          label: body.label,
          scheme: "mbr",
          phase: "format",
          started: time,
        };
        disk.formattable = false;
        disk.reason = "formatting";
        return json({ disks: data.usbDisks, job: data.usbJob });
      }
      // Downloads from a link: tests set data.urlProbe, data.urlConflict
      // (answer the first start with file_exists) and read data.urlDownloads.
      if (path === "/files/url-download/probe" && method === "POST") {
        (data.urlProbes ??= []).push(route.request().postDataJSON());
        return json(
          data.urlProbe ?? {
            name: "",
            size: -1,
            exists: false,
            free: 6e11,
          },
        );
      }
      if (path === "/files/url-download" && method === "POST") {
        const body = route.request().postDataJSON();
        (data.urlDownloads ??= []).push(body);
        if (data.urlConflict && body.conflict !== "rename")
          return route.fulfill({
            status: 409,
            json: { error: { code: "file_exists", message: "fixture" } },
          });
        const job = {
          id: "download-" + data.urlDownloads.length,
          operation: "download",
          move: false,
          state: "running",
          from: "https://example.test/isos/debian.iso",
          to: body.root,
          dir: body.dir,
          names: [body.name],
          files: 1,
          done: 0,
          bytes: 4e9,
          copied: 1e9,
          current: "",
          error: "",
          retryable: false,
        };
        (data.fileJobs ??= []).push(job);
        return route.fulfill({ status: 202, json: job });
      }
      if (path === "/files/jobs") return json(data.fileJobs ?? []);
      if (path === "/files/roots")
        return json(
          data.shares.map((s) => ({
            id: "share/" + s.name,
            name: s.name,
            kind: "share",
            writable: !(data.readOnlyShares ?? []).includes(s.name),
            recycle: true,
            totalBytes: 1e12,
            freeBytes: 6e11,
          })),
        );
      if (path === "/files/list") {
        data.files.push(url.searchParams.get("root"));
        data.filePaths.push(url.searchParams.get("path") ?? "");
        return json([]);
      }
      if (
        path === "/files/jobs" ||
        path === "/files/uploads" ||
        path === "/usb"
      )
        return json([]);
      // Settings › Data and backup › System data (PLAN 79-8).
      if (path === "/system/data") {
        if (method === "PUT") {
          const body = route.request().postDataJSON();
          data.sysdataCalls = [...(data.sysdataCalls ?? []), body];
          if (body.volume !== undefined) {
            data.sysdata.volume = body.volume;
            data.sysdata.resolved = body.volume || "nvme";
          }
          for (const f of data.sysdata.functions) {
            if (body.functions?.[f.id] !== undefined)
              f.choice = body.functions[f.id];
            f.volume = f.choice || data.sysdata.resolved;
            f.source = f.choice
              ? "own"
              : data.sysdata.volume
                ? "common"
                : "automatic";
          }
        }
        return json(data.sysdata);
      }
      if (path === "/backups/vm-disks")
        return json(data.vmDisks ?? { choiceNeeded: false, disks: [] });
      if (path === "/config-backup")
        return json({
          backup: {
            settings: { enabled: false },
            target: "",
            lastError: "",
            backups: [],
            next: null,
          },
        });
      if (path === "/backups")
        return json({
          jobs: data.backups,
          shares: [],
          volumes: [],
          smbTargets: [],
        });
      if (path.endsWith("/snapshots") && path.startsWith("/vms/"))
        return json({
          snapshots: [],
          schedule: {
            enabled: false,
            time: "03:00",
            days: [],
            keep: 7,
            memory: false,
            allowPause: false,
          },
          job: null,
          pending: false,
          reason: "",
        });
      if (path.endsWith("/addresses"))
        return json({ addresses: [], source: "" });
      // No Intel GPU in the fixtures: the GuC/HuC tile stays hidden.
      if (path === "/hardware/intel-gpu")
        return json({
          mode: "auto",
          managed: false,
          active: "",
          param: "",
          entries: {},
          deferred: false,
          restartNeeded: false,
          writable: true,
          gpus: [],
          problem: "",
        });
      // Settings › Hardware: no device without a driver, no missing firmware.
      if (path === "/hardware/report")
        return json({
          generated: time,
          kernel: "6.12.0-test",
          board: "",
          boot: "uefi",
          controllers: [],
          disks: [],
          network: [],
          graphics: [],
          sensors: [],
          unbound: [],
          firmwareMissing: [],
          summary: [],
        });
      if (path === "/hardware")
        return json({
          hardware: { pci: [], usb: [], iommu: false },
          usedBy: {},
        });
      // Settings › Services (admin): Docker and VMs on, LXC off (PLAN 71 overview).
      if (path === "/services" && method === "GET")
        return json({
          docker: { enabled: true, running: 3, total: 4, available: true },
          vms: { enabled: true, running: 1, total: 2, available: true },
          lxc: { enabled: false, running: 0, total: 0, available: true },
        });
      // Settings › Access: SSH off, nobody with a key, the self-signed certificate.
      if (path === "/system/ssh")
        return json({
          enabled: false,
          running: false,
          fingerprint: "",
          accounts: [],
          allowedNetworks: [],
          homeNetworks: ["192.168.1.0/24"],
          tailscaleNetworks: ["100.64.0.0/10"],
          netbirdNetworks: [],
          clientAddress: "192.168.1.50",
        });
      if (path === "/system/certificate")
        return json({
          kind: "self-signed",
          names: ["titan-test.local"],
          issuer: "titan-test",
          notBefore: time,
          notAfter: "2036-10-23T16:00:00Z",
          fingerprint: "ab:cd",
        });
      // No LXC containers; the command palette lists them for admins.
      if (path === "/lxc")
        return json({
          installed: true,
          dev: false,
          host: { cpus: 8, memoryBytes: 16e9 },
          bridge: true,
          networks: [],
          containers: [],
        });
      if (path === "/vms/imports")
        return json({ jobs: [], chunkBytes: 8 << 20 });
      if (path === "/vms/media") return json({ media: [] });
      if (path === "/vms")
        return json({
          vms: [
            {
              name: "source",
              displayName: "Source VM",
              volume: "Data",
              state: "stopped",
              available: true,
              job: null,
              description: "",
              template: "linux",
              memoryMiB: 512,
              vcpus: 1,
              disks: [],
              iso: "",
              autostart: false,
              cpuPin: [],
              exclusive: false,
              folders: [],
              tpm: false,
              secureBoot: false,
              pci: [],
              usb: [],
              network: "user",
              mac: "52:54:00:00:00:01",
              cpuPercent: 0,
              memoryBytes: 0,
              since: "",
            },
          ],
          kvm: true,
          bridge: false,
          host: {
            cpus: 8,
            memoryBytes: 16e9,
            usedMemoryBytes: 3e9,
            availableMemoryBytes: 13e9,
            cpuModel: "Test CPU",
          },
        });
      if (path === "/update/progress")
        return json({ progress: { phase: "idle" } });
      if (path === "/update" && method === "GET") return json(data.update);
      if (path === "/update/settings" && method === "PUT") {
        const body = route.request().postDataJSON();
        data.updatePuts.push(body);
        const every = body.checkEvery ?? 0;
        const valid =
          every === 0 ||
          (every === -1 && !body.autoInstall) ||
          (every >= 60 && every <= 30 * 24 * 60);
        if (!valid)
          return route.fulfill({
            status: 400,
            json: {
              error: { code: "invalid_update_settings", message: "invalid" },
            },
          });
        const settings = { ...body, checkEvery: every === 24 * 60 ? 0 : every };
        if (!settings.checkEvery) delete settings.checkEvery;
        if (settings.kernel !== "new") delete settings.kernel;
        const next = new Date(
          Date.parse(data.update.lastCheck) +
            (every > 0 ? every : 24 * 60) * 60e3,
        ).toISOString();
        data.update = {
          ...data.update,
          settings,
          nextCheck: every === -1 ? null : next,
        };
        return json(data.update);
      }
      if (path === "/update/check" && method === "POST") {
        data.updateChecks++;
        return json(data.update);
      }
      // Settings › Files: the full-text index, off, nothing indexed yet.
      if (path === "/files/index" && method === "GET")
        return json({
          status: {
            enabled: false,
            volumes: [],
            shares: [],
            ocr: false,
            time: "",
            indexes: [
              {
                volume: "Data",
                state: "idle",
                files: 0,
                known: false,
                scanned: 0,
                updated: 0,
                bytes: 0,
              },
            ],
            ocrAvailable: false,
          },
          volumes: ["Data"],
          shares: data.shares.map((s) => ({ name: s.name, volume: s.volume })),
          mounted: ["Data"],
        });
      // Settings › Memory: the page cache share of the RAM.
      if (path === "/system/memory" && method === "GET")
        return json({
          settings: { cachePercent: 20 },
          readOnly: false,
          budgetBytes: 3.2e9,
          reserveBytes: 1e9,
          inFlightBytes: 0,
          memory: {
            ...data.system.memory,
            cacheBytes: 2e9,
            dirtyBytes: 0,
            writebackBytes: 0,
            lowAvailable: false,
          },
          paused: false,
        });
      // HTTPS names: no app route (app pages stay as before), one device
      // of the home network.
      if (path === "/docker/proxy" && method === "GET")
        return json({
          domain: "",
          suffixes: ["titan-test.local"],
          apps: [],
          targets: [
            {
              app: "homeassistant",
              enabled: true,
              name: "homeassistant",
              host: "192.168.1.30",
              port: 8123,
              scheme: "http",
              urls: ["https://homeassistant.titan-test.local"],
            },
          ],
          localName: "",
          defaultLocalName: "titan-test",
          mdns: true,
          off: false,
          tailnet: { available: false, keySet: false },
          auto: false,
        });
      if (path === "/certs/ca")
        return json({
          available: true,
          name: "TitanOS titan-test CA",
          fingerprint: "3F:A1:7C:22:9B:04:E8:51:6D:C0:12:7A:9E:44:B3:08",
          notAfter: "2036-10-01T00:00:00Z",
        });
      // Settings › Energy: profiles, disk sleep and the power schedule.
      if (path === "/system/energy" && method === "GET")
        return json({
          settings: { governor: "", preference: "", platform: "" },
          controls: [
            {
              key: "governor",
              choices: ["powersave", "performance"],
              current: "powersave",
              mixed: false,
            },
          ],
          drivers: ["intel_pstate"],
          readOnly: false,
        });
      if (path === "/storage/disk-sleep" && method === "GET")
        return json({
          idleMinutes: 30,
          volumes: {},
          disks: {},
          available: true,
          errors: [],
          listed: true,
          volumeStatus: [
            { name: "Data", override: null, effective: 30, origin: "default" },
          ],
          diskStatus: [
            {
              id: "serial:WD-A1",
              name: "sda",
              model: "WDC WD40EFPX",
              serial: "WD-A1",
              sizeBytes: 4e12,
              volume: "Data",
              override: null,
              effective: 30,
              origin: "default",
              state: "active",
              since: time,
            },
            {
              id: "serial:WD-B1",
              name: "sdb",
              model: "WDC WD40EFPX",
              serial: "WD-B1",
              sizeBytes: 4e12,
              volume: "Data",
              override: null,
              effective: 30,
              origin: "default",
              state: "asleep",
              since: time,
            },
          ],
          unavailable: [],
        });
      if (path === "/system/power-schedule" && method === "GET")
        return json({
          settings: {
            off: { enabled: false, days: [], time: "23:30" },
            on: { enabled: false, days: [], time: "07:00" },
            wakeOnLan: false,
          },
          rtc: true,
          interfaces: [
            {
              name: "enp1s0",
              mac: "52:54:00:00:00:01",
              supported: true,
              enabled: false,
            },
          ],
          readOnly: false,
        });
      // Benutzer (U2, PLAN 75-20): the apps of one account.
      {
        const m = path.match(/^\/users\/([^/]+)\/apps$/);
        if (m) {
          data.userApps ??= {};
          const admin = m[1] === "alex";
          const own = (data.userApps[m[1]] ??= {
            newApps: true,
            apps: { photos: true, jellyfin: true, bambustudio: false },
          });
          if (method === "PUT") {
            const body = route.request().postDataJSON();
            if (typeof body.newApps === "boolean") own.newApps = body.newApps;
            Object.assign(own.apps, body.apps ?? {});
          }
          return json({
            admin,
            newApps: admin || own.newApps,
            apps: Object.entries(own.apps).map(([app, allowed]) => ({
              app,
              icon: "",
              url: "",
              webui: "",
              running: true,
              allowed: admin || allowed,
            })),
          });
        }
      }
      // Settings › Programs: who sees which app.
      if (path === "/apps/access" && method === "GET")
        return json({
          newApps: "admins",
          apps: [
            {
              app: "photos",
              icon: "",
              url: "https://photos.example.test",
              webui: "2283",
              running: true,
              gate: true,
              users: ["alex"],
            },
          ],
          accounts: [
            { name: "alex", role: "owner", disabled: false },
            { name: "mia", role: "user", disabled: false },
          ],
        });
      // Settings › WireGuard: off, one device prepared.
      if (path === "/network/wireguard" && method === "GET")
        return json({
          enabled: false,
          running: false,
          endpoint: "",
          port: 51820,
          network: "10.10.0.0/24",
          fullTunnel: false,
          dns: "",
          publicKey: "q2ZcE9n0d0b8pW3s1o4Vt7yXh6kLm5NfRa9Jc2Ue8Gs=",
          peers: [
            {
              name: "Handy",
              publicKey: "x7Yv3Lq9dK2mP0sT8wR4nB6cF1hJ5aZgE3uNiO7tWpM=",
              address: "10.10.0.2/32",
              created: time,
              received: 0,
              sent: 0,
            },
          ],
        });
      // Settings › KI-Agent: switched off, no Ollama answer yet.
      if (path === "/agent/cluster")
        return json({ member: false, admin: false });
      if (path === "/agent/settings" && method === "GET")
        return json({
          enabled: false,
          volume: "",
          ollama: "",
          model: "",
          think: "",
          context: 0,
          users: [],
          policy: { preset: "standard" },
          locations: [],
          homeassistant: {},
        });
      if (path === "/agent/addresses")
        return json({
          addresses: [
            { kind: "lan", url: "http://192.168.1.20", https: false },
            { kind: "local", url: "https://titan-test.local", https: true },
          ],
        });
      if (path === "/agent/status" && method === "GET")
        return json({
          enabled: false,
          allowed: true,
          admin: true,
          model: "",
          think: "",
          context: 0,
          volume: false,
          web: false,
          reachable: false,
          models: [],
          running: [],
        });
      if (path === "/agent/tools") return json({ tools: [], areas: [] });
      if (path === "/agent/volumes" && method === "GET")
        return json({
          volumes: [
            {
              name: "Data",
              filesystem: "ext4",
              sizeBytes: 1e12,
              freeBytes: 6e11,
              kind: "hdd",
              current: false,
            },
          ],
          move: null,
        });
      if (path === "/homeassistant/connections" && method === "GET")
        return json({ connections: [], max: 8 });
      // Settings › API und CLI: no token yet.
      if (path === "/account/tokens" && method === "GET")
        return json({
          tokens: [],
          scopes: [
            "system:read",
            "system:write",
            "files:read",
            "files:write",
            "docker:read",
            "docker:write",
            "vms:read",
            "vms:write",
            "mcp",
          ],
        });
      // Dynamic DNS: nothing set up, no IPv64 key.
      if (path === "/network/ddns" && method === "GET")
        return json({
          settings: {
            enabled: false,
            hostnames: [],
            provider: { type: "" },
            ipv4: true,
            ipv6: true,
            interval: 0,
            lookupURL: "",
            interface: "",
            heartbeat: { enabled: false },
          },
          secrets: {},
          running: false,
          heartbeat: {},
        });
      // New VM: eight threads, no image or free disk yet.
      if (path === "/vms/host")
        return json({
          cpus: Array.from({ length: 8 }, (_, id) => ({
            id,
            kind: "",
            siblings: [id % 4, (id % 4) + 4],
            usedBy: [],
          })),
          images: [],
          disks: [],
          virt: null,
        });
      // Settings › Network (the welcome flow has its own answer above).
      if (path === "/network" && method === "GET") {
        const lan = {
          id: "admin",
          name: "Heimnetz",
          purpose: "management",
          parent: "enp1s0",
          vlan: 0,
          bridge: true,
          management: true,
          ipv4: "dhcp",
          address: "",
          gateway: "",
          ipv6: "auto",
          address6: "",
          gateway6: "",
          dns: [],
          routes: [],
        };
        const config = {
          hostname: "titan-test",
          ports: ["enp1s0"],
          bondMode: "",
          vlan: 0,
          bridge: true,
          dhcp: true,
          address: "",
          gateway: "",
          dns: [],
          networks: [lan],
          bonds: [],
        };
        return json({
          network: {
            config,
            pending: null,
            pendingSeconds: 0,
            interfaces: [
              {
                name: "enp1s0",
                mac: "52:54:00:00:00:01",
                carrier: true,
                speedMbps: 1000,
              },
            ],
            device: "br0",
            addresses: ["192.168.1.20/24"],
            gateway: "192.168.1.1",
            dns: ["192.168.1.1"],
            configured: true,
            bridgeReady: true,
            topology: config,
            pendingTopology: null,
            live: {
              admin: {
                device: "br0",
                up: true,
                addresses: ["192.168.1.20/24"],
              },
            },
            vlans: false,
            vlansInUse: false,
          },
          bondModes: ["active-backup", "802.3ad", "balance-alb"],
        });
      }
      // Plugins without data.plugins (scripts that set it are answered above).
      if (path === "/plugins" || path === "/plugins/catalog") {
        const plugin = (name, title, category, installed, description) => ({
          name,
          title,
          description,
          version: "1.0.0",
          size: 24e6,
          kernel: "",
          contains: null,
          modules: [],
          services: installed ? ["titanos-" + name + ".service"] : [],
          installed,
          enabled: installed,
          active: installed,
          path: "",
          official: true,
          local: false,
          download: !installed,
          problem: "",
          update: null,
          category,
        });
        return json({
          driverUpdate: null,
          volume: "Data",
          kernel: "test",
          plugins: [
            plugin("intel-gpu-tools", "Intel GPU Tools", "tools", true, {
              de: "Werkzeuge für Intel-Grafik",
              en: "Tools for Intel graphics",
            }),
            plugin("tailscale", "Tailscale", "remote", false, {
              de: "Fernzugriff ohne offene Ports",
              en: "Remote access without open ports",
            }),
          ],
          catalog: { release: releaseVersion(), state: "ready" },
          recommendations: [],
          dismissed: 0,
          sources: [],
        });
      }
      if (path === "/plugins/progress") return json({ operations: [] });
      if (path === "/plugins/releases")
        return json({
          releases: [
            {
              version: releaseVersion(),
              bytes: 600e6,
              protected: true,
              reason: "current",
              fingerprint: "current",
            },
            {
              version: "0.58.0",
              bytes: 590e6,
              protected: true,
              reason: "fallback",
              fingerprint: "fallback",
            },
            {
              version: "0.55.0",
              bytes: 580e6,
              protected: false,
              fingerprint: "old",
            },
          ],
        });
      // Let's Encrypt: one name proven over DNS, certificate valid.
      if (path === "/system/letsencrypt" && method === "GET")
        return json({
          settings: {
            enabled: true,
            email: "",
            staging: false,
            domains: ["nas.example.de"],
            apps: false,
            challenge: "dns-01",
            provider: { type: "cloudflare" },
            propagation: 0,
            directory: "",
          },
          secrets: { token: true },
          names: ["nas.example.de"],
          appDomain: "",
          certificate: {
            names: ["nas.example.de"],
            issuer: "R11",
            notBefore: "2026-10-01T00:00:00Z",
            notAfter: "2026-12-30T00:00:00Z",
            fingerprint: "ab",
          },
          running: false,
          lastAttempt: time,
          lastSuccess: time,
          failures: 0,
          renewAt: "2026-11-30T00:00:00Z",
          httpPort: true,
          firewall: false,
          internetHttp: false,
        });
      // Docker › LAN networks: one container LAN on the home network.
      if (path === "/docker/networks" && method === "GET")
        return json({
          networks: [
            {
              name: "heimnetz",
              parent: "admin",
              driver: "ipvlan",
              subnet: "192.168.1.0/24",
              gateway: "192.168.1.1",
              range: "192.168.1.192/27",
              hostAccess: false,
            },
          ],
          parents: [
            {
              id: "admin",
              name: "Heimnetz",
              purpose: "management",
              parent: "enp1s0",
              vlan: 0,
              bridge: true,
              management: true,
              ipv4: "dhcp",
              address: "",
              gateway: "",
              ipv6: "auto",
              address6: "",
              gateway6: "",
              dns: [],
            },
          ],
        });
      return json({});
    });
    // PLAN 63: a remote view that never starts (scripts that test it
    // route the stream themselves); nothing reaches a NAS.
    await ctx.routeWebSocket(/\/api\/v1\/viewer\/[^/]+\/stream/, () => {});
    const page = await ctx.newPage();
    page.setDefaultTimeout(12000);
    page.on("pageerror", (e) => errors.push(String(e)));
    page.on("console", (m) => {
      if (
        m.type() === "error" &&
        /TypeError|ReferenceError|SyntaxError/.test(m.text())
      )
        errors.push(m.text());
    });
    return { ctx, page, data };
  }

  return context;
}

// Isolated data for Tools › Scripts: four scripts on the volume "Data",
// one per trigger, with kept runs. A started run prints two lines and ends
// after the output was asked for three times (or when it is canceled).
function scriptFixtures(time) {
  const run = (outcome, extra = {}) => ({
    trigger: "schedule",
    started: time,
    durationMs: 4200,
    outcome,
    ...extra,
  });
  return {
    scripts: {
      volume: "Data",
      timezone: "UTC",
      chosen: true,
      available: true,
      disabled: false,
      volumes: ["Data", "Fast"],
      scripts: [
        {
          id: "a1a1a1a1a1a1a1a1",
          name: "Fotos sichern",
          description: "rsync der Handyfotos auf das zweite Volume",
          trigger: "schedule",
          schedule: { every: "daily", time: "03:00" },
          changed: time,
          running: false,
          last: run("ok", { exit: 0 }),
          next: "2026-10-24T03:00:00Z",
        },
        {
          id: "b2b2b2b2b2b2b2b2",
          name: "Docker aufräumen",
          description: "",
          trigger: "schedule",
          schedule: { every: "custom", cron: "0 4 * * 0" },
          changed: time,
          running: false,
          last: run("failed", { exit: 1, durationMs: 800 }),
          next: "2026-10-25T04:00:00Z",
        },
        {
          id: "c3c3c3c3c3c3c3c3",
          name: "Lüfterkurve setzen",
          description: "",
          trigger: "start",
          changed: time,
          running: false,
          last: run("ok", { exit: 0, trigger: "start", durationMs: 300 }),
        },
        {
          id: "d4d4d4d4d4d4d4d4",
          name: "Sync vor dem Ausschalten",
          description: "",
          trigger: "shutdown",
          changed: time,
          running: false,
        },
      ],
    },
    scriptContent: {
      a1a1a1a1a1a1a1a1:
        "#!/bin/bash\nset -e\nrsync -a /mnt/Data/Fotos/ /mnt/Fast/Fotos/\necho fertig\n",
      b2b2b2b2b2b2b2b2: "docker image prune -af\n",
      c3c3c3c3c3c3c3c3: "#!/bin/sh\necho 60 > /sys/class/hwmon/hwmon2/pwm1\n",
      d4d4d4d4d4d4d4d4: "#!/bin/bash\nsync\n",
    },
    scriptRuns: {
      a1a1a1a1a1a1a1a1: [
        run("ok", {
          exit: 0,
          output: "sending incremental file list\nfertig\n",
        }),
        run("timeout", { started: "2026-10-22T03:00:00Z", output: "" }),
      ],
      b2b2b2b2b2b2b2b2: [
        run("failed", {
          exit: 1,
          durationMs: 800,
          output: "Cannot connect to the Docker daemon\n",
        }),
      ],
    },
    scriptCalls: [],
    scriptPolls: {},
  };
}

function scriptRoute(data, path, method, route, time) {
  const json = (value, status = 200) => route.fulfill({ status, json: value });
  const status = data.scripts;
  const parts = path.split("/").slice(2);
  data.scriptCalls.push([method, path, route.request().postData() ?? ""]);
  if (parts.length === 0) {
    if (method === "POST") {
      const body = route.request().postDataJSON();
      const id = (status.scripts.length + 1).toString(16).padStart(16, "e");
      const { content, ...rest } = body;
      const script = { id, ...rest, changed: time, running: false };
      status.scripts.push(script);
      data.scriptContent[id] = content ?? "";
      return json({ ...script, content }, 201);
    }
    return json(status);
  }
  if (parts[0] === "volume") {
    const body = route.request().postDataJSON();
    status.volume = body.volume;
    status.chosen = true;
    return json(status);
  }
  const script = status.scripts.find((s) => s.id === parts[0]);
  if (!script)
    return json(
      { error: { code: "script_not_found", message: "fixture" } },
      404,
    );
  const finish = (outcome, exit) => {
    script.running = false;
    const run = {
      trigger: "manual",
      started: time,
      durationMs: 1500,
      outcome,
      ...(exit === undefined ? {} : { exit }),
    };
    script.last = run;
    (data.scriptRuns[script.id] ??= []).unshift({
      ...run,
      output: "Sicherung beginnt …\n12 Dateien kopiert\n",
    });
  };
  switch (`${method} ${parts[1] ?? ""}`) {
    case "GET ":
      return json({ ...script, content: data.scriptContent[script.id] ?? "" });
    case "PUT ": {
      const { content, ...rest } = route.request().postDataJSON();
      Object.assign(script, rest);
      if (rest.trigger !== "schedule") delete script.schedule;
      if (content !== undefined) data.scriptContent[script.id] = content;
      return json({ ...script, content: data.scriptContent[script.id] });
    }
    case "DELETE ":
      status.scripts = status.scripts.filter((s) => s !== script);
      return route.fulfill({ status: 204 });
    case "POST run":
      if (script.running)
        return json(
          { error: { code: "script_running", message: "fixture" } },
          409,
        );
      script.running = true;
      data.scriptPolls[script.id] = 0;
      return route.fulfill({ status: 202 });
    case "POST cancel":
      if (!script.running)
        return json(
          { error: { code: "script_not_running", message: "fixture" } },
          409,
        );
      finish("canceled");
      return route.fulfill({ status: 202 });
    case "GET runs":
      return json({ runs: data.scriptRuns[script.id] ?? [] });
    case "GET output": {
      const from = Number(
        new URL(route.request().url()).searchParams.get("from") ?? 0,
      );
      const all = "Sicherung beginnt …\n12 Dateien kopiert\n";
      if (script.running) {
        const polls = ++data.scriptPolls[script.id];
        const shown = polls < 2 ? all.indexOf("\n") + 1 : all.length;
        if (polls >= 3 && !data.scriptHold) finish("ok", 0);
        return json({
          running: script.running,
          text: all.slice(Math.min(from, shown), shown),
          next: shown,
          skipped: false,
          run: script.running
            ? {
                trigger: "manual",
                started: time,
                durationMs: 0,
                outcome: "running",
              }
            : script.last,
        });
      }
      const text = script.last ? all : "";
      return json({
        running: false,
        text: text.slice(Math.min(from, text.length)),
        next: text.length,
        skipped: false,
        ...(script.last ? { run: script.last } : {}),
      });
    }
  }
  return json({ error: { code: "not_found", message: "fixture" } }, 404);
}

/**
 * PLAN 76 (F-12-15): text that is cut off – by an ellipsis, a hidden
 * overflow or a line limit. The owner's rule: never; a place too small
 * wraps, scrolls or shows an icon instead. Runs in the page
 * (`frame.evaluate(cutTextIn)`), returns "selector: text" per finding.
 * Fields (their value scrolls) and screen-reader-only text do not count,
 * nor does a box marked `data-cut` itself: "excerpt" for the first lines of
 * long content shown whole elsewhere, "frame" for the desktop and its
 * windows, which clip what is moved partly out of them.
 *
 * PLAN 79-14: it also reports a word that runs over several lines (a label
 * narrower than its longest word) and a native select narrower than the
 * option it shows.
 */
export function cutTextIn() {
  const found = [];
  const seen = new Set();
  const name = (el) =>
    el.tagName.toLowerCase() +
    (el.id ? "#" + el.id : "") +
    (typeof el.className === "string" && el.className.trim()
      ? "." + el.className.trim().split(/\s+/).slice(0, 3).join(".")
      : "");
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if (!node.textContent.trim()) continue;
    // The text and every box around it that could hide a part of it.
    for (
      let el = node.parentElement, hops = 0;
      el && el !== document.body && hops < 6;
      el = el.parentElement, hops++
    ) {
      if (seen.has(el)) break;
      // Deliberate: an excerpt of long content (a note's first lines), or a
      // frame that clips what is moved partly out of it (the desktop).
      if (el.hasAttribute("data-cut")) continue;
      const cs = getComputedStyle(el);
      if (cs.display === "inline" || cs.display === "contents") continue;
      if (
        el.closest(
          'input, textarea, select, .sr-only, [aria-hidden="true"], canvas, svg, .xterm',
        )
      )
        break;
      const r = el.getBoundingClientRect();
      if (r.width < 2 || r.height < 2 || cs.visibility !== "visible") break;
      const hidesX = cs.overflowX === "hidden" || cs.overflowX === "clip";
      const hidesY = cs.overflowY === "hidden" || cs.overflowY === "clip";
      const cutX = hidesX && el.scrollWidth > el.clientWidth + 1;
      const cutY = hidesY && el.scrollHeight > el.clientHeight + 2;
      if (cutX || cutY) {
        seen.add(el);
        found.push(
          `${name(el)} [${cutX ? "x" : ""}${cutY ? "y" : ""}]: ${el.textContent.trim().replace(/\s+/g, " ").slice(0, 60)}`,
        );
        break;
      }
      // A box that scrolls shows everything; nothing above it cuts this text.
      if (/(auto|scroll)/.test(cs.overflowX + cs.overflowY)) break;
    }
  }
  // PLAN 79-14: a word that runs over several lines. A label breaks between
  // words only, so a broken word means its box is narrower than its longest
  // word (words of more than 30 letters are names that cannot fit a phone).
  // Paths, addresses and identifiers (`overflow-wrap: anywhere`) may break,
  // but not in a box squeezed to a few letters per line – the share names
  // that stood one letter per line beside a full-width select (79-1).
  const skip =
    'script, style, noscript, input, textarea, select, .sr-only, [aria-hidden="true"], canvas, svg, .xterm, pre, code, [data-cut]';
  const words = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  const range = document.createRange();
  for (let node = words.nextNode(); node; node = words.nextNode()) {
    const el = node.parentElement;
    if (!el || !node.textContent.trim() || seen.has(el) || el.closest(skip))
      continue;
    const cs = getComputedStyle(el);
    if (cs.visibility !== "visible" || !el.getClientRects().length) continue;
    const anywhere =
      cs.overflowWrap === "anywhere" ||
      cs.wordBreak === "break-all" ||
      cs.wordBreak === "break-word";
    let box = el;
    while (box.parentElement && getComputedStyle(box).display === "inline")
      box = box.parentElement;
    const squeezed = box.clientWidth < 4 * parseFloat(cs.fontSize);
    for (const m of node.textContent.matchAll(/[\p{L}\p{N}]{2,}/gu)) {
      range.setStart(node, m.index);
      range.setEnd(node, m.index + m[0].length);
      const tops = new Set(
        [...range.getClientRects()]
          .filter((r) => r.width > 0)
          .map((r) => Math.round(r.top)),
      );
      if (tops.size < 2) continue;
      if (squeezed || (!anywhere && m[0].length >= 4 && m[0].length <= 30)) {
        seen.add(el);
        found.push(
          `${name(el)} [word over ${tops.size} lines, box ${box.clientWidth}px]: ${m[0].slice(0, 60)}`,
        );
        break;
      }
    }
  }
  // PLAN 79-14: a native select shows its chosen option in one line and
  // cuts what does not fit; the control must be as wide as that text.
  const canvas = document.createElement("canvas").getContext("2d");
  for (const el of document.querySelectorAll("select")) {
    if (el.multiple || el.size > 1 || el.closest("[data-cut], .sr-only"))
      continue;
    const cs = getComputedStyle(el);
    const text = el.selectedOptions[0]?.text.trim();
    if (!text || cs.visibility !== "visible" || !el.getClientRects().length)
      continue;
    canvas.font = `${cs.fontStyle} ${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
    const need = canvas.measureText(text).width;
    // The arrow of a native control stands inside its content box.
    const arrow = cs.appearance === "none" ? 0 : 18;
    const room =
      el.clientWidth -
      parseFloat(cs.paddingLeft) -
      parseFloat(cs.paddingRight) -
      arrow;
    if (need > room + 1)
      found.push(
        `${name(el)} [select ${Math.round(room)}px for ${Math.round(need)}px]: ${text.slice(0, 60)}`,
      );
  }
  return found;
}
