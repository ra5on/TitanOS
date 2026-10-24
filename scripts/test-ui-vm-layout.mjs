// Built Svelte UI and a synthetic RFB peer. Never connects to a NAS or a real VM.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5187";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const out = process.env.UI_OUTPUT || "out/validation/vm-layout";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
let lastPage;
const fixtures = fixtureFactory(browser, errors);

async function context(theme = "graphit", width = 1600, locale = "de") {
  const result = await fixtures("owner", theme, width, locale);
  const { ctx, page, data } = result;
  lastPage = page;
  await page.setViewportSize({ width, height: 1000 });
  await page.clock.setFixedTime(new Date("2026-10-24T15:40:00+02:00"));
  Object.assign(data.system, {
    version: "0.20.1",
    hostname: "titan-nas",
    kernel: "6.12.113+deb13-amd64",
  });
  const common = {
    description: "",
    exclusive: false,
    folders: [],
    tpm: false,
    secureBoot: false,
    pci: [],
    usb: [],
    job: null,
    available: true,
    cpuPercent: 8,
    memoryBytes: 2e9,
    since: "2026-10-24T09:15:00Z",
    cpuPin: [],
    firmware: "uefi",
    iso: "",
    network: "bridge",
    autostart: true,
    vcpus: 2,
    volume: "Data",
    disks: [
      {
        file: "vdisk1.qcow2",
        format: "qcow2",
        sizeBytes: 40 * 1024 ** 3,
        volume: "Data",
      },
    ],
  };
  const vms = ["Windows 11", "Home Assistant OS", "Debian", "Testumgebung"].map(
    (displayName, i) => ({
      ...common,
      name: "vm" + i,
      displayName,
      state: i === 3 ? "stopped" : "running",
      template: i === 0 ? "windows" : "linux",
      memoryMiB: [8192, 4096, 4096, 2048][i],
      mac: "52:54:00:00:00:0" + (i + 1),
      description: i === 2 ? "Dienste und Testumgebung" : "",
    }),
  );
  await ctx.route("**/api/v1/vms", (r) =>
    r.fulfill({
      json: {
        kvm: true,
        bridge: true,
        vms,
        host: { cpus: 16, memoryBytes: 32e9, usedMemoryBytes: 4e9 },
      },
    }),
  );
  await ctx.route("**/api/v1/vms/*/addresses", (r) =>
    r.fulfill({ json: { addresses: ["192.168.1.55"], source: "agent" } }),
  );
  await ctx.route("**/api/v1/vms/*/snapshots", (r) =>
    r.fulfill({
      json: {
        snapshots: [
          {
            id: "snapshot-1",
            name: "Vor dem Update",
            created: "2026-10-24T08:00:00Z",
            memory: true,
            automatic: false,
            bytes: 3.2 * 1024 ** 3,
          },
        ],
        schedule: {
          enabled: true,
          time: "03:00",
          days: [0, 1, 2, 3, 4, 5, 6],
          keep: 7,
          memory: true,
          allowPause: true,
        },
        pending: false,
        reason: "",
        job: null,
      },
    }),
  );
  await ctx.addInitScript(() => {
    // A minimal RFB 3.8 peer: version, security, ServerInit and raw framebuffer.
    // Exercising the real noVNC canvas detects overlays after connect/resize/fullscreen.
    const fixture = { latest: null, keys: 0, connections: 0 };
    window.__vncFixture = fixture;
    class RfbPeer {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSING = 2;
      static CLOSED = 3;
      constructor(url) {
        this.url = url;
        this.binaryType = "arraybuffer";
        this.protocol = "";
        this.readyState = 0;
        this.onopen = null;
        this.onmessage = null;
        this.onerror = null;
        this.onclose = null;
        this.step = 0;
        this.width = 640;
        this.height = 360;
        this.painted = false;
        fixture.latest = this;
        fixture.connections++;
        setTimeout(() => {
          if (this.readyState !== 0) return;
          this.readyState = 1;
          this.onopen?.({});
          this.emit(new TextEncoder().encode("RFB 003.008\n"));
        }, 0);
      }
      emit(bytes) {
        setTimeout(() => {
          if (this.readyState === 1)
            this.onmessage?.({
              data: bytes.buffer.slice(
                bytes.byteOffset,
                bytes.byteOffset + bytes.byteLength,
              ),
            });
        }, 0);
      }
      send(data) {
        const bytes =
          data instanceof ArrayBuffer
            ? new Uint8Array(data)
            : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
        if (this.step === 0) {
          this.step++;
          this.emit(new Uint8Array([1, 1]));
          return;
        }
        if (this.step === 1) {
          this.step++;
          this.emit(new Uint8Array(4));
          return;
        }
        if (this.step === 2) {
          this.step++;
          const name = new TextEncoder().encode("Synthetic VNC test guest");
          const init = new Uint8Array(24 + name.length),
            view = new DataView(init.buffer);
          view.setUint16(0, this.width);
          view.setUint16(2, this.height);
          init[4] = 32;
          init[5] = 24;
          init[7] = 1;
          view.setUint16(8, 255);
          view.setUint16(10, 255);
          view.setUint16(12, 255);
          init[14] = 16;
          init[15] = 8;
          view.setUint32(20, name.length);
          init.set(name, 24);
          this.emit(init);
          return;
        }
        if (bytes[0] === 4) fixture.keys++;
        if (bytes[0] === 3 && !this.painted) {
          this.painted = true;
          this.paint(false);
        }
      }
      paint(resize) {
        const canvas = document.createElement("canvas");
        canvas.width = this.width;
        canvas.height = this.height;
        const ctx = canvas.getContext("2d");
        ctx.fillStyle = "#080c10";
        ctx.fillRect(0, 0, this.width, this.height);
        ctx.fillStyle = "#d8dee7";
        ctx.font = "14px monospace";
        ctx.fillText("Debian GNU/Linux 13", 24, 42);
        ctx.fillText("VNC-Testgast · isolierte Beispieldaten", 24, 78);
        ctx.fillStyle = "#8ac6a6";
        ctx.fillText("alex@debian:~$ _", 24, 132);
        const rgba = ctx.getImageData(0, 0, this.width, this.height).data;
        const prefix = resize ? 28 : 16;
        const update = new Uint8Array(prefix + rgba.length),
          view = new DataView(update.buffer);
        view.setUint16(2, resize ? 2 : 1);
        if (resize) {
          view.setUint16(8, this.width);
          view.setUint16(10, this.height);
          view.setInt32(12, -223);
        }
        const rectangle = resize ? 16 : 4;
        view.setUint16(rectangle + 4, this.width);
        view.setUint16(rectangle + 6, this.height);
        for (let i = 0; i < rgba.length; i += 4) {
          update[prefix + i] = rgba[i + 2];
          update[prefix + i + 1] = rgba[i + 1];
          update[prefix + i + 2] = rgba[i];
        }
        this.emit(update);
      }
      resize(width, height) {
        this.width = width;
        this.height = height;
        this.paint(true);
      }
      disconnect() {
        if (this.readyState === 3) return;
        this.readyState = 3;
        this.onclose?.({
          code: 1006,
          reason: "Synthetic disconnect",
          wasClean: false,
        });
      }
      close() {
        this.readyState = 3;
      }
    }
    window.WebSocket = RfbPeer;
  });
  return result;
}

async function connected(page) {
  await page.waitForFunction(() => {
    const canvas = document.querySelector(".frame canvas");
    return (
      canvas?.width === 640 &&
      !document.querySelector(".screen-footer [role=status]")
    );
  });
}
async function clearFrame(page) {
  assert.equal(await page.locator(".frame").innerText(), "");
  assert.equal(
    await page
      .locator(
        ".frame button,.frame p,.frame span,.frame .resolution,.frame .fsbar,.frame [role=status]",
      )
      .count(),
    0,
  );
}
async function noOverflow(page) {
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
    "Horizontal page overflow",
  );
}

try {
  {
    const { ctx, page, data } = await context();
    await page.goto(base + "/vms?vm=vm2&tab=screen");
    await connected(page);
    await page.evaluate(() => document.fonts.ready);
    assert.equal(await page.locator(".tasks-entry").count(), 1);
    assert.equal(await page.locator(".vm-page>.head .tasks-entry").count(), 1);
    const boxes = await page.evaluate(() => {
      const rect = (selector) => {
        const r = document.querySelector(selector).getBoundingClientRect();
        return { top: r.top, bottom: r.bottom, right: r.right };
      };
      return {
        list: rect(".layout>aside"),
        frame: rect(".frame"),
        tabs: rect(".command-bar>.tabs"),
        tools: rect(".command-bar>.toolbar"),
        head: rect(".vm-page>.head"),
      };
    });
    assert.ok(
      Math.abs(boxes.list.top - boxes.frame.top) <= 1,
      "VM list and VNC frame must align",
    );
    assert.ok(
      Math.abs(
        (boxes.tabs.top + boxes.tabs.bottom) / 2 -
          (boxes.tools.top + boxes.tools.bottom) / 2,
      ) <= 2,
      "Tabs and console tools must share one row",
    );
    assert.ok(boxes.head.bottom <= boxes.tabs.top);
    await clearFrame(page);
    await noOverflow(page);
    await page.screenshot({
      path: out + "/screen-graphit-1600.png",
      fullPage: true,
    });
    await page.getByRole("button", { name: "Senden", exact: true }).click();
    await page
      .getByRole("menuitem", { name: "Strg+Alt+Entf", exact: true })
      .click();
    assert.ok(
      await page.evaluate(() => window.__vncFixture.keys >= 6),
      "Keyboard shortcut must reach noVNC",
    );
    await page.evaluate(() => window.__vncFixture.latest.resize(800, 450));
    await page.waitForFunction(
      () => document.querySelector(".frame canvas")?.width === 800,
    );
    await clearFrame(page);
    await page.getByRole("button", { name: "Vollbild", exact: true }).click();
    await page.waitForFunction(() =>
      document.fullscreenElement?.classList.contains("frame"),
    );
    await clearFrame(page);
    await page.evaluate(() => document.exitFullscreen());
    await page.evaluate(() => window.__vncFixture.latest.disconnect());
    await page.locator(".screen-footer [role=status]").waitFor();
    await clearFrame(page);
    await page.waitForFunction(() => window.__vncFixture.connections >= 2);
    await connected(page);
    await clearFrame(page);
    await page.getByRole("button", { name: /Laufende Aufgaben/ }).click();
    await page.getByRole("dialog").waitFor();
    await page.getByRole("button", { name: "Fertig", exact: true }).click();
    await page.goto(base + "/vms?vm=vm2&tab=overview");
    await page.getByText("192.168.1.55", { exact: true }).waitFor();
    assert.equal(await page.locator(".command-bar>.tabs>a").count(), 3);
    assert.equal(await page.locator(".command-bar>.toolbar").count(), 0);
    await page.screenshot({
      path: out + "/overview-graphit-1600.png",
      fullPage: true,
    });
    await page.goto(base + "/vms?vm=vm2&tab=snapshots");
    await page.getByText("Vor dem Update", { exact: true }).waitFor();
    await page
      .getByRole("button", { name: "Snapshot planen", exact: true })
      .click();
    await page.getByRole("dialog").waitFor();
    await page.getByRole("button", { name: "Abbrechen", exact: true }).click();
    await page.screenshot({
      path: out + "/snapshots-graphit-1600.png",
      fullPage: true,
    });
    await page.goto(base + "/vms?vm=vm3&tab=screen");
    await page
      .getByRole("button", { name: "VM starten", exact: true })
      .waitFor();
    await clearFrame(page);
    assert.equal(await page.locator(".screen-state").count(), 1);
    await page.goto(base + "/vms/screen?vm=vm2");
    await connected(page);
    await clearFrame(page);
    assert.equal(await page.locator(".bar").count(), 1);
    assert.deepEqual(
      data.requests.filter(([method]) => !["GET", "HEAD"].includes(method)),
      [],
      "Viewing the page must not write preferences or execute actions",
    );
    await ctx.close();
  }
  for (const [theme, width, locale] of [
    ["hell", 1600, "de"],
    ["graphit", 1024, "en"],
    ["hell", 760, "de"],
    ["graphit", 390, "de"],
    ["hell", 320, "en"],
  ]) {
    const { ctx, page } = await context(theme, width, locale);
    await page.goto(base + "/vms?vm=vm2&tab=screen");
    await connected(page);
    await clearFrame(page);
    await noOverflow(page);
    assert.ok(
      await page.evaluate(() => window.scrollY <= 1),
      "Connecting must keep the VM heading and tabs in view",
    );
    assert.equal(await page.locator(".command-bar>.tabs>a").count(), 3);
    if (width <= 760) {
      assert.equal(await page.locator(".vm").first().isVisible(), false);
      await page.locator(".picker").click();
      assert.equal(await page.locator(".vm").first().isVisible(), true);
      await noOverflow(page);
      await page.locator(".picker").click();
    }
    await page.screenshot({
      path: `${out}/screen-${theme}-${width}-${locale}.png`,
      fullPage: true,
    });
    await page.goto(base + "/vms?vm=vm2&tab=overview");
    await page.locator("#ov-network").waitFor();
    await noOverflow(page);
    await page.goto(base + "/vms?vm=vm2&tab=snapshots");
    await page.getByText("Vor dem Update", { exact: true }).waitFor();
    await noOverflow(page);
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log(
    "ok – VM alignment, tabs, controls, RFB resize/reconnect, clear fullscreen/popout, DE/EN and five responsive sizes",
  );
} catch (error) {
  console.error(errors);
  if (lastPage && !lastPage.isClosed()) {
    await lastPage.screenshot({ path: out + "/error.png", fullPage: true });
    console.error((await lastPage.locator("body").innerText()).slice(0, 5000));
    console.error(
      await lastPage.evaluate(() => ({
        peer: window.__vncFixture?.latest?.step,
        size: [...document.querySelectorAll(".frame canvas")].map((c) => [
          c.width,
          c.height,
        ]),
        scroll: window.scrollY,
      })),
    );
  }
  throw error;
} finally {
  await browser.close();
}
