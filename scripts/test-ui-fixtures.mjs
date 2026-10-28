// Shared isolated API fixtures. No requests reach a NAS.
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
    version: "0.16.0",
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
  ) {
    const ctx = await browser.newContext({
      locale: "de-DE",
      viewport: { width, height: 1000 },
    });
    await ctx.addInitScript(
      ({ theme, locale }) => {
        localStorage.setItem("titanos.locale", locale);
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
      // Activity log: tests may replace data.activity; queries are kept.
      if (path === "/activity") {
        const log = (data.activity ??= {
          settings: { enabled: true, volume: "", days: 90, maxMiB: 64 },
          entries: [
            { at: time, user: "alex", via: "web", ip: "192.168.1.20", action: "POST /api/v1/files/delete", target: "Media/alt.mkv", status: 200, ok: true },
            { at: time, user: "sam", via: "web", ip: "192.168.1.31", action: "login_failed", ok: false },
            { at: time, user: "alex", via: "token", action: "PUT /api/v1/system/timezone", status: 200, ok: true },
          ],
          queries: [],
        });
        const status = { settings: log.settings, volume: log.settings.enabled ? "Data" : "", pending: 0, dropped: 0, error: "" };
        if (method === "PUT") {
          log.settings = route.request().postDataJSON();
          return json({ ...status, settings: log.settings });
        }
        log.queries.push(Object.fromEntries(url.searchParams));
        if (url.searchParams.get("format")) {
          data.exports++;
          return route.fulfill({ body: "time,user\n", contentType: "text/csv" });
        }
        const user = url.searchParams.get("user"),
          failed = url.searchParams.get("failed") === "1";
        const entries = log.entries.filter((e) => (!user || e.user === user) && (!failed || !e.ok));
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
          setupRequired: false,
          user: { name: "alex", role, twoFactor: false },
          hostname: "titan-test",
        });
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
        if (method === "PUT") data.ha.settings = route.request().postDataJSON();
        return json(data.ha ?? {});
      }
      if (path.startsWith("/preferences/"))
        return route.fulfill({ status: 204 });
      if (path === "/system") return json(data.system);
      // Share quotas per volume; PUT .../quotas switches them like titand.
      const quotas = (data.volumeQuotas ??= { Data: "active", Fast: "unsupported", Safe: "off" });
      const switched = /^\/storage\/volumes\/([^/]+)\/quotas$/.exec(path);
      if (switched && method === "PUT") {
        const on = route.request().postDataJSON().enabled;
        quotas[decodeURIComponent(switched[1])] = on ? "pending" : "off";
        return route.fulfill({ status: 204 });
      }
      if (path === "/storage")
        return json({
          volumes: [
            { ...volume, level: "raid5", shareQuotas: quotas.Data },
            { ...volume, name: "Fast", filesystem: "btrfs", shareQuotas: quotas.Fast },
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
          containers: data.webMissing
            ? [container("db", "exited")]
            : [container("web"), container("db", "exited")],
          apps: ["photos"],
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
            recommendations: data.plugins.recommendations.filter((r) => r.plugin !== name),
            dismissed: (data.plugins.dismissed ?? 0) + 1,
          };
        } else if (method === "DELETE") {
          data.plugins = {
            ...data.plugins,
            recommendations: [...data.plugins.recommendations, ...(data.hiddenRecommendations ?? [])],
            dismissed: 0,
          };
          data.hiddenRecommendations = [];
        }
        return json(data.plugins);
      }
      if (path.endsWith("/update") && path.startsWith("/plugins/") && method === "POST") {
        (data.pluginUpdates ??= []).push([path, route.request().postDataJSON()]);
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
      if (path === "/assistant") return json({ available: false });
      // Formatting drives: tests set data.usbDisks and data.formatError and
      // read data.formats; data.usbJob is the job the NAS reports.
      if (path === "/usb/disks" && method === "GET")
        return json({ disks: data.usbDisks ?? [], job: data.usbJob ?? null });
      // Partition editor: tests set data.usbLayouts (disk ID -> layout) and
      // read data.partitionActions; each action starts a running job.
      const partitionRoute = path.match(/^\/usb\/disks\/([^/]+)\/(partitions|table)(?:\/(\d+)(?:\/(\w+))?)?$/);
      if (partitionRoute) {
        const [, rawId, kind, number, verb] = partitionRoute;
        const id = decodeURIComponent(rawId);
        const layout = data.usbLayouts?.[id];
        if (!layout)
          return route.fulfill({ status: 404, json: { error: { code: "usb_not_found", message: "usb: no such drive" } } });
        if (method === "GET") return json({ ...layout, job: data.usbJob ?? null });
        const body = method === "DELETE" ? {} : route.request().postDataJSON();
        const action = kind === "table" ? "table" : !number ? "create" : method === "DELETE" ? "delete" : verb;
        (data.partitionActions ??= []).push({ id, action, ...(number ? { number: Number(number) } : {}), ...body });
        data.usbJob = { disk: id, device: layout.disk.device, name: layout.disk.model, action, partition: number ? layout.disk.device + number : "", filesystem: body.filesystem ?? "", label: body.label ?? "", scheme: layout.table, phase: "partition", started: time };
        return json({ disks: data.usbDisks ?? [], job: data.usbJob });
      }
      if (path.startsWith("/usb/disks/") && path.endsWith("/format") && method === "POST") {
        const body = route.request().postDataJSON();
        (data.formats ??= []).push({ id: decodeURIComponent(path.split("/")[3]), ...body });
        if (data.formatError)
          return route.fulfill({ status: 409, json: { error: { code: data.formatError, message: "usb: this drive cannot be formatted now: inuse" } } });
        const disk = data.usbDisks.find((d) => d.id === decodeURIComponent(path.split("/")[3]));
        data.usbJob = { disk: disk.id, device: disk.device, name: disk.model, filesystem: body.filesystem, label: body.label, scheme: "mbr", phase: "format", started: time };
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
      if (path === "/hardware")
        return json({
          hardware: { pci: [], usb: [], iommu: false },
          usedBy: {},
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
      return json({});
    });
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
