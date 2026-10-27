// Real browser interactions with isolated APIs only; no NAS or VM is contacted.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5179";
assert.ok(["127.0.0.1", "localhost"].includes(new URL(base).hostname));
const out = process.env.UI_OUTPUT || "out/validation/vm-create";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const fixtures = fixtureFactory(browser, errors);
let last;
const GIB = 1024 ** 3;
async function context({
  theme = "graphit",
  width = 1600,
  locale = "de",
  bridge = true,
  configured = true,
  ambiguous = false,
  touch = false,
} = {}) {
  const factory = touch
    ? fixtureFactory(
        {
          newContext: (options) =>
            browser.newContext({ ...options, hasTouch: true, isMobile: true }),
        },
        errors,
      )
    : fixtures;
  const result = await factory("owner", theme, width, locale);
  const { ctx, page, data } = result;
  last = page;
  await page.setViewportSize({ width, height: 1050 });
  data.system.version = "0.20.2-vm-ui-test";
  data.system.cpu.threads = 16;
  data.system.memory.totalBytes = 32 * GIB;
  const network = {
    configured,
    bridgeReady: bridge,
    config: { bridge, ports: configured ? ["eth0"] : [], dhcp: true },
    pending: null,
    pendingSeconds: 0,
    device: bridge ? "br0" : "eth0",
    addresses: ["192.168.1.20/24"],
    interfaces: [
      { name: "eth0", carrier: true },
      { name: "eth1", carrier: false },
    ],
  };
  const common = {
    description: "",
    template: "linux",
    memoryMiB: 8192,
    vcpus: 4,
    disks: [
      {
        file: "vdisk1.qcow2",
        format: "qcow2",
        sizeBytes: 64 * GIB,
        volume: "Fast",
      },
    ],
    iso: "",
    autostart: false,
    cpuPin: [],
    exclusive: false,
    folders: [],
    pci: [],
    usb: [],
    tpm: false,
    secureBoot: false,
    firmware: "uefi",
    network: "bridge",
    volume: "Fast",
    available: true,
    job: null,
    cpuPercent: 0,
    memoryBytes: 0,
    since: "",
    mac: "52:54:00:00:00:01",
  };
  const vms = [
    { ...common, name: "debian", displayName: "Debian 13", state: "stopped" },
    { ...common, name: "running", displayName: "Running VM", state: "running" },
  ];
  await ctx.route("**/api/v1/storage/tests", (r) =>
    r.fulfill({ json: { tests: [] } }),
  );
  const requests = [];
  const state = { logs: "valid", nextLog: 0, ambiguous, bridgeCalls: 0 };
  await ctx.route("**/api/v1/network**", (r) => {
    const path = new URL(r.request().url()).pathname;
    const body =
      r.request().method() === "POST" ? r.request().postDataJSON() : null;
    requests.push([r.request().method(), path, body]);
    if (path.endsWith("/bridge")) {
      state.bridgeCalls++;
      if (body.onlyUnconfigured && network.configured)
        return r.fulfill({ json: { network } });
      if (state.ambiguous && !body.port)
        return r.fulfill({
          status: 409,
          json: { error: { code: "network_bridge_port" } },
        });
      network.pending = {
        ...network.config,
        bridge: true,
        ports: [body.port || "eth0"],
      };
      network.pendingSeconds = 120;
      network.bridgeReady = false;
    }
    if (path.endsWith("/confirm")) {
      network.config = network.pending;
      network.pending = null;
      network.configured = true;
      network.bridgeReady = true;
      network.device = "br0";
    }
    if (path.endsWith("/revert")) {
      network.pending = null;
      network.pendingSeconds = 0;
    }
    return r.fulfill({ json: { network, bondModes: ["active-backup"] } });
  });
  await ctx.route("**/api/v1/vms**", (r) => {
    const path = new URL(r.request().url()).pathname;
    const method = r.request().method();
    if (path === "/api/v1/vms/virtio") return r.fulfill({json:{id:"virtio-fixture",volume:"Data",name:"virtio-win.iso",phase:"done",bytes:100,total:100,written:100,outputBytes:100,output:"/mnt/Data/iso/virtio-win.iso"}});
    if (path === "/api/v1/vms/imports") return r.fulfill({json:{jobs:[],chunkBytes:8<<20}});
    if (path === "/api/v1/vms/host")
      return r.fulfill({
        json: {
          cpus: Array.from({ length: 16 }, (_, id) => ({
            id,
            siblings: [id % 8, (id % 8) + 8],
            kind: "",
            usedBy: [],
          })),
          images: [
            {
              path: "/mnt/Fast/images/base.qcow2",
              format: "qcow2",
              virtualBytes: 32 * GIB,
              usedBy: "",
            },
          ],
          disks: [
            {
              device: "sdc",
              path: "/dev/disk/by-id/fixture-unused",
              usedBy: "",
            },
          ],
        },
      });
    if (path === "/api/v1/vms/media")
      return r.fulfill({
        json: {
          media: [{ path: "/mnt/Data/isos/debian-13.iso", sizeBytes: 700e6 }],
        },
      });
    if (path === "/api/v1/vms" && method === "GET")
      return r.fulfill({
        json: {
          vms,
          kvm: true,
          bridge: network.config.bridge || !!network.pending,
          host: {
            cpus: 16,
            memoryBytes: 32 * GIB,
            usedMemoryBytes: 8 * GIB,
            availableMemoryBytes: 24 * GIB,
            cpuModel: "Intel Core i7-11700",
          },
        },
      });
    if (path.endsWith("/snapshots"))
      return r.fulfill({
        json: {
          snapshots: [],
          schedule: { enabled: false, days: [], time: "03:00" },
          pending: false,
          reason: "",
          job: null,
        },
      });
    if (path.endsWith("/addresses"))
      return r.fulfill({ json: { addresses: [], source: "" } });
    if (path.endsWith("/xml")) return r.fulfill({ json: { xml: "<domain/>" } });
    requests.push([
      method,
      path,
      method === "POST" || method === "PUT" ? r.request().postDataJSON() : null,
    ]);
    return r.fulfill({ status: 204 });
  });
  await ctx.route("**/api/v1/logs?*", (r) => {
    requests.push([
      "GET",
      new URL(r.request().url()).pathname + new URL(r.request().url()).search,
    ]);
    if (state.logs === "html")
      return r.fulfill({
        contentType: "text/html",
        body: "<h1>Dashboard</h1><p>titan · Läuft seit 20 Std 41 Min · TitanOS 0.19.0</p>",
      });
    if (state.logs === "invalid")
      return r.fulfill({
        json: { entries: [{ time: "not-a-date" }], cursor: "" },
      });
    if (state.logs === "empty")
      return r.fulfill({ json: { entries: [], cursor: "" } });
    const after = new URL(r.request().url()).searchParams.get("after");
    return r.fulfill({
      json: {
        entries: after
          ? state.nextLog++ === 0
            ? [
                {
                  time: "2026-10-24T15:41:00Z",
                  priority: 4,
                  source: "network",
                  message: "Bridge test followed",
                },
              ]
            : []
          : [
              {
                time: "2026-10-24T15:40:00Z",
                priority: 6,
                source: "titand",
                message: "Test journal entry · TitanOS 0.19.0 started",
              },
            ],
        cursor: after ? "s=abc;i=2" : "s=abc;i=1",
      },
    });
  });
  return { ...result, network, vms, requests, state };
}
async function menu(page, name, item, how = "mouse") {
  const button = page.getByRole("button", { name: `Aktionen für ${name}` });
  if (how === "right") await button.click({ button: "right" });
  else await button.click();
  const target = page.getByRole("menuitem", { name: item, exact: true });
  if (how === "keyboard") {
    await target.focus();
    await page.keyboard.press("Enter");
  } else await target.click();
}
async function shot(page, file) {
  await page.evaluate(async () => {
    await document.fonts.ready;
    document.activeElement?.blur();
    window.scrollTo(0, 0);
  });
  await page.screenshot({ path: `${out}/${file}.png`, fullPage: true });
}
try {
  // Regression: each action must snapshot the VM before clearing its menu.
  {
    const { ctx, page, requests } = await context();
    await page.goto(`${base}/vms?vm=debian&tab=overview`);
    await menu(page, "Debian 13", "Starten");
    await page.waitForTimeout(150);
    assert.equal(
      requests.filter(
        ([m, p]) => m === "POST" && p === "/api/v1/vms/debian/start",
      ).length,
      1,
    );
    await menu(page, "Running VM", "Neustarten", "keyboard");
    await page.waitForTimeout(150);
    assert.equal(
      requests.filter(
        ([m, p]) => m === "POST" && p === "/api/v1/vms/running/reboot",
      ).length,
      1,
    );
    await menu(page, "Running VM", "Herunterfahren");
    await page.waitForTimeout(150);
    assert.equal(
      requests.filter(
        ([m, p]) => m === "POST" && p === "/api/v1/vms/running/shutdown",
      ).length,
      1,
    );
    await menu(page, "Debian 13", "Snapshots", "right");
    await page.waitForURL("**/vms?vm=debian&tab=snapshots");
    await menu(page, "Debian 13", "Bearbeiten");
    await page.waitForURL("**/vms/edit?vm=debian");
    await page
      .getByRole("button", { name: "Speichern", exact: true })
      .waitFor();
    await page.goto(`${base}/vms?vm=debian`);
    await menu(page, "Debian 13", "Entfernen …");
    await page.getByRole("dialog").waitFor();
    assert.equal(requests.filter(([m]) => m === "DELETE").length, 0);
    await page.getByRole("button", { name: "Abbrechen", exact: true }).click();
    await menu(page, "Debian 13", "VM klonen");
    await page.getByRole("dialog").waitFor();
    await page.getByRole("button", { name: "Abbrechen", exact: true }).click();
    await page.goto(`${base}/vms?vm=running&tab=overview`);
    await page.locator(".head .dropdown button").click();
    await page
      .getByRole("menuitem", { name: "Neustarten", exact: true })
      .click();
    await page.waitForTimeout(150);
    assert.equal(
      requests.filter(
        ([m, p]) => m === "POST" && p === "/api/v1/vms/running/reboot",
      ).length,
      2,
    );
    await menu(page, "Running VM", "Ausschalten erzwingen …");
    await page.getByRole("dialog").waitFor();
    assert.equal(
      requests.filter(([m, p]) => p.endsWith("/poweroff")).length,
      0,
    );
    await ctx.close();
  }
  // The approved layout, all themes/languages and narrow layouts.
  for (const [theme, width, locale] of [
    ["graphit", 1600, "de"],
    ["hell", 1600, "de"],
    ["graphit", 390, "de"],
    ["hell", 390, "en"],
    ["graphit", 1600, "en"],
  ]) {
    const { ctx, page } = await context({ theme, width, locale });
    await page.goto(`${base}/vms/new`);
    const name = page.locator('input[maxlength="160"]');
    await name.fill(locale === "de" ? "Debian Entwicklung" : "Debian development");
    await page.locator("#vm-vcpus").fill("4");
    await page.locator("#vm-memory").fill("8");
    await page.locator("#vm-iso").fill("/mnt/Data/isos/debian-13.iso");
    await page.waitForFunction(() =>
      document.querySelector(".bridge-setup")?.textContent?.includes("br0"),
    );
    assert.equal(await page.locator(".netsel select").inputValue(), "bridge");
    assert.equal(await page.locator(".vm-card").count(), 5);
    await shot(page, `${theme}-${width}-${locale}`);
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      "horizontal overflow",
    );
    if (width === 1600) {
      const [left, right] = await Promise.all([
        page.locator(".steps").boundingBox(),
        page.locator('aside[aria-labelledby="summary-title"]').boundingBox(),
      ]);
      assert.ok(right.x > left.x + left.width);
    }
    await ctx.close();
  }
  // Draft survives network activation, confirmation and cancellation.
  for (const revert of [false, true]) {
    const { ctx, page, requests, state } = await context({
      bridge: false,
      configured: true,
      ambiguous: true,
    });
    await page.goto(`${base}/vms/new`);
    await page.locator('input[maxlength="160"]').fill("Bridge draft");
    await page
      .getByRole("button", { name: "Heimnetz aktivieren", exact: true })
      .click();
    await page
      .getByLabel("Ethernet-Anschluss für das Heimnetz")
      .selectOption("eth0");
    await page
      .getByRole("button", { name: "Heimnetz aktivieren", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Nur erstellen", exact: true })
      .waitFor();
    assert.ok(
      await page
        .getByRole("button", { name: "Nur erstellen", exact: true })
        .isDisabled(),
    );
    await page
      .locator(".bridge-setup")
      .getByRole("button", {
        name: revert ? "Zurück zur vorherigen" : "Behalten",
        exact: true,
      })
      .click();
    await page.waitForTimeout(200);
    assert.equal(
      await page.locator('input[maxlength="160"]').inputValue(),
      "Bridge draft",
    );
    assert.equal(
      await page.locator(".netsel select").inputValue(),
      revert ? "user" : "bridge",
    );
    assert.equal(state.bridgeCalls, 2);
    if (!revert) {
      await page
        .getByRole("button", { name: "Nur erstellen", exact: true })
        .click();
      await page.waitForTimeout(150);
      assert.equal(
        requests.filter(([m, p]) => m === "POST" && p === "/api/v1/vms").length,
        1,
      );
    }
    await ctx.close();
  }
  // All disk sources, pinning, passthrough, folders and guest firmware survive regrouping.
  {
    const { ctx, page, requests } = await context();
    await ctx.route("**/api/v1/hardware", (r) =>
      r.fulfill({
        json: {
          hardware: {
            iommu: true,
            pci: [
              {
                address: "0000:01:00.0",
                vendorName: "Fixture GPU",
                kind: "gpu",
                passable: true,
                companions: [],
                vendor: "1234",
                device: "5678",
              },
            ],
            usb: [
              {
                id: "1234:0001",
                port: "1-2",
                name: "Fixture USB",
                stick: false,
              },
              {
                id: "1234:0002",
                port: "1-3",
                name: "TitanOS stick",
                stick: true,
              },
            ],
          },
          usedBy: {},
        },
      }),
    );
    await page.goto(`${base}/vms/new`);
    await page.locator('input[maxlength="160"]').fill("Hardware test");
    await page.locator("summary").filter({ hasText: "VM-Ordner" }).click();
    await page
      .locator('input[placeholder="hardware-test"]')
      .fill("custom-folder");
    await page
      .getByRole("radio", { name: "Kerne festlegen", exact: true })
      .click();
    await page.getByRole("button", { name: /CPU 2/ }).first().click();
    await page
      .getByLabel("Diese Kerne exklusiv reservieren, solange die VM läuft")
      .check();
    await page
      .getByRole("button", { name: "+ Neue Platte", exact: true })
      .click();
    await page
      .getByRole("button", { name: "+ Vorhandenes Abbild", exact: true })
      .click();
    assert.ok(
      await page
        .getByRole("button", { name: "Erstellen und starten", exact: true })
        .isDisabled(),
    );
    await page
      .getByRole("radio", { name: "Datei direkt verwenden", exact: true })
      .click();
    await page
      .getByRole("button", { name: "+ Ganze Festplatte", exact: true })
      .click();
    assert.equal(await page.locator(".disk").count(), 4);
    await page
      .locator("summary")
      .filter({ hasText: "Geräte durchreichen" })
      .click();
    await page.getByLabel("Grafikkarte · Fixture GPU", { exact: true }).check();
    await page.getByLabel("USB · Fixture USB", { exact: true }).check();
    assert.ok(
      await page
        .getByLabel("USB · TitanOS stick", { exact: true })
        .isDisabled(),
    );
    await page
      .locator("summary")
      .filter({ hasText: "Ordner vom NAS freigeben" })
      .click();
    await page
      .getByRole("button", { name: "+ Ordner hinzufügen", exact: true })
      .click();
    await page.getByLabel("Name in der VM", { exact: true }).fill("documents");
    await page.getByRole("radio", { name: /^Windows/ }).click();
    await page.locator("summary").filter({ hasText: "Start-Firmware" }).click();
    await page.locator("#vm-firmware").selectOption("bios");
    await page.locator("#vm-firmware").selectOption("uefi");
    await page
      .getByRole("button", { name: "Nur erstellen", exact: true })
      .click();
    await page.waitForTimeout(150);
    const body = requests.find(
      ([m, p]) => m === "POST" && p === "/api/v1/vms",
    )?.[2];
    assert.ok(body);
    assert.deepEqual(body.cpuPin, [2, 10]);
    assert.equal(body.exclusive, true);
    assert.equal(body.firmware, "uefi");
    assert.equal(body.tpm, true);
    assert.equal(body.directory, "custom-folder");
    assert.equal(body.disks.length, 4);
    assert.equal(body.disks[2].path, "/mnt/Fast/images/base.qcow2");
    assert.equal(body.disks[3].path, "/dev/disk/by-id/fixture-unused");
    assert.deepEqual(body.pci, ["0000:01:00.0"]);
    assert.deepEqual(body.usb, ["1234:0001"]);
    assert.equal(body.folders[0].tag, "documents");
    await page.goto(`${base}/vms/edit?vm=running`);
    await page.locator('input[maxlength="160"]').fill("Updated running VM");
    assert.ok(await page.locator("#vm-memory").isDisabled());
    assert.ok(await page.locator(".netsel select").isDisabled());
    await page.getByRole("button", { name: "Speichern", exact: true }).click();
    await page.waitForTimeout(150);
    assert.equal(
      requests.find(([m, p]) => m === "PUT" && p === "/api/v1/vms/running")?.[2]
        .displayName,
      "Updated running VM",
    );
    await ctx.close();
  }
  // Native touch events on the mobile VM list.
  {
    const { ctx, page, requests } = await context({ width: 390, touch: true });
    await page.goto(`${base}/vms?vm=debian&tab=overview`);
    await page.locator(".picker").tap();
    await page.getByRole("button", { name: "Aktionen für Debian 13" }).tap();
    await page.getByRole("menuitem", { name: "Starten", exact: true }).tap();
    await page.waitForTimeout(150);
    assert.equal(
      requests.filter(
        ([m, p]) => m === "POST" && p === "/api/v1/vms/debian/start",
      ).length,
      1,
    );
    await ctx.close();
  }
  // Automatic setup runs only for a new network and only once.
  for (const configured of [false, true]) {
    const { ctx, page, state } = await context({ bridge: false, configured });
    await page.goto(`${base}/welcome`);
    await page.getByRole("button", { name: "Überspringen" }).click();
    await page.waitForTimeout(300);
    assert.equal(state.bridgeCalls, configured ? 0 : 1);
    await ctx.close();
  }
  // Actual log entries, follow, legacy link, HTML fallback, retry, empty state.
  {
    const { ctx, page, state } = await context();
    await page.goto(`${base}/logs?source=network`);
    await page.waitForURL("**/tools?source=network#tools-logs");
    await page
      .getByText("Test journal entry · TitanOS 0.19.0 started", { exact: true })
      .waitFor();
    await page
      .locator(".logs .message")
      .filter({ hasText: "Bridge test followed" })
      .waitFor({ timeout: 7000 });
    assert.equal(
      await page.locator(".logs select").first().inputValue(),
      "network",
    );
    for (const mode of ["html", "invalid"]) {
      state.logs = mode;
      await page.locator('.logs input[type="search"]').fill(mode);
      await page.locator('.logs [role="alert"]').waitFor();
      assert.equal(await page.locator(".logs h1").count(), 0);
    }
    state.logs = "valid";
    await page
      .locator(".logs")
      .getByRole("button", { name: "Erneut versuchen", exact: true })
      .click();
    await page
      .getByText("Test journal entry · TitanOS 0.19.0 started", { exact: true })
      .waitFor();
    await shot(page, "system-logs");
    state.logs = "empty";
    await page.locator('.logs input[type="search"]').fill("empty");
    await page
      .getByText("Keine Zeilen zu dieser Auswahl.", { exact: true })
      .waitFor();
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log(
    "VM cards, menu actions, bridge setup/rollback and journal browser checks passed.",
  );
} catch (e) {
  if (last && !last.isClosed()) {
    console.error(await last.locator("body").innerText());
    await last.screenshot({
      path: `out/validation/vm-create/failure.png`,
      fullPage: true,
    });
  }
  throw e;
} finally {
  await browser.close();
}
