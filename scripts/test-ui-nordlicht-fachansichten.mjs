// The real desktop and app frames, always on loopback and with fixture data.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const current = process.env.UI_URL || "http://127.0.0.1:5297";
const before = process.env.UI_BASELINE;
for (const url of [current, before].filter(Boolean)) {
  assert.ok(
    ["localhost", "127.0.0.1", "[::1]"].includes(new URL(url).hostname),
  );
}
const browser = await chromium.launch({
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const fixture = fixtureFactory(browser, errors);
const when = "2026-11-26T09:00:00Z";
const machines = ["web", "cache"].map((name, i) => ({
  name,
  displayName: i ? "Redis-Cache" : "Webserver",
  distribution: "debian",
  release: "trixie",
  arch: "amd64",
  cpus: 2,
  memoryMiB: 1024,
  volume: "Data",
  network: "bridge",
  state: i ? "stopped" : "running",
  available: true,
  cpuPercent: 1,
  memoryBytes: 1e8,
  created: when,
  autostart: false,
  mounts: [],
  mac: "52:54:00:01:00:01",
}));
const vm = {
  ...machines[0],
  name: "debian",
  displayName: "Debian Test",
  vcpus: 2,
  disks: [],
  cpuPin: [],
  folders: [],
  pci: [],
  usb: [],
  description: "",
  template: "linux",
  since: when,
  iso: "",
  tpm: false,
  secureBoot: false,
  exclusive: false,
};
const host = {
  cpus: 8,
  memoryBytes: 16e9,
  usedMemoryBytes: 3e9,
  availableMemoryBytes: 13e9,
  cpuModel: "Test CPU",
};
const sections = [
  ["storage", "/storage"],
  ["shares", "/shares"],
  ["vms", "/vms?vm=debian&tab=overview"],
  ["lxc", "/lxc"],
  ["logs", "/tools/logs"],
  ["users", "/users"],
];

async function populate(ctx) {
  await ctx.route("**/api/v1/vms", (route) =>
    route.fulfill({ json: { host, vms: [vm], kvm: true, bridge: true } }),
  );
  await ctx.route("**/api/v1/lxc", (route) =>
    route.fulfill({
      json: {
        host,
        installed: true,
        dev: false,
        bridge: true,
        networks: [],
        containers: machines,
      },
    }),
  );
  await ctx.route("**/api/v1/lxc/*/snapshots", (route) =>
    route.fulfill({ json: { snapshots: [] } }),
  );
  await ctx.route("**/api/v1/logs?*", (route) =>
    route.fulfill({
      json: {
        cursor: "",
        entries: [
          {
            time: when,
            source: "titand",
            priority: 6,
            message: "Speicherprüfung abgeschlossen.",
          },
          {
            time: when,
            source: "titand",
            priority: 4,
            message: "Sicherungsziel wartet auf USB.",
          },
          {
            time: when,
            source: "kernel",
            priority: 3,
            message: "Lesefehler auf dem Testgerät.",
          },
        ],
      },
    }),
  );
}

async function test(base, baseline, section, route) {
  const { ctx, page } = await fixture();
  page.setDefaultTimeout(25000);
  try {
    await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
    await populate(ctx);
    await page.goto(base + route);
    const win = page.locator(".win.focused").first();
    const frame = win.frameLocator("iframe[data-window]");
    const button = (name) => frame.getByRole("button", { name, exact: true });
    const shot = async (name) => {
      if (!process.env.UI_OUTPUT) return;
      const folder = join(
        process.env.UI_OUTPUT,
        baseline ? "Vorher" : "Nachher",
      );
      await mkdir(folder, { recursive: true });
      await win.screenshot({
        path: join(folder, section + "-" + name + ".png"),
        animations: "disabled",
      });
    };
    const narrow = async () => {
      await win.evaluate((el) =>
        el.style.setProperty("width", "680px", "important"),
      );
      await frame
        .locator("body")
        .evaluate(
          () => new Promise((resolve) => requestAnimationFrame(resolve)),
        );
      assert.ok(
        await frame
          .locator("body")
          .evaluate(
            () =>
              innerWidth <= 700 &&
              document.documentElement.scrollWidth <= innerWidth,
          ),
      );
    };
    const focused = async (loc) =>
      assert.ok(await loc.evaluate((el) => el === document.activeElement));
    if (section === "storage") {
      await button("Data").click();
      await frame.getByText("Debian Test", { exact: true }).waitFor();
      await frame
        .getByText("Debian Test", { exact: true })
        .scrollIntoViewIfNeeded();
      await shot("volume");
      if (!baseline) {
        assert.equal(
          await frame
            .getByRole("link", { name: "Debian Test" })
            .getAttribute("href"),
          "/vms?vm=debian&tab=hardware",
        );
        assert.equal(
          await frame
            .getByRole("link", { name: "photos", exact: true })
            .getAttribute("href"),
          "/docker/apps/photos",
        );
        await narrow();
      }
    } else if (section === "shares") {
      const documents = button("Documents");
      await documents.click();
      const heading = frame.locator(".detail h2");
      await heading.waitFor();
      await shot("details");
      if (!baseline) {
        await focused(heading);
        await frame.getByRole("tab").first().press("End");
        assert.equal(
          await frame.getByRole("tab").last().getAttribute("aria-selected"),
          "true",
        );
        await frame.locator(".detail-head button").click();
        await focused(documents);
        await narrow();
      }
    } else if (section === "vms") {
      await frame.getByText("Debian Test", { exact: true }).first().waitFor();
      const toolbar = frame.locator(".win-toolbar").first();
      await toolbar.waitFor();
      await shot("aktionen");
      if (!baseline) {
        assert.equal(
          await toolbar.getByRole("button", { name: "Neu starten" }).count(),
          0,
        );
        await toolbar
          .getByRole("button", { name: "Weitere Aktionen" })
          .press("Enter");
        await frame.getByRole("menuitem", { name: "Neu starten" }).waitFor();
        await page.keyboard.press("Escape");
        await narrow();
      }
    } else if (section === "lxc") {
      await frame
        .getByRole("region", { name: "Webserver", exact: true })
        .waitFor();
      await shot("container");
      if (!baseline) {
        await narrow();
        const picker = frame.locator(".container-picker");
        await picker.selectOption("cache");
        await shot("auswahl-schmal");
        await frame
          .locator(".detail")
          .getByText("Redis-Cache", { exact: true })
          .waitFor();
        await picker.selectOption("web");
        await frame
          .locator(".danger-zone")
          .getByRole("button", { name: "Ausschalten erzwingen" })
          .click();
        await frame.getByRole("dialog").waitFor();
        await shot("bestaetigung");
        await page.keyboard.press("Escape");
        await frame.getByRole("dialog").waitFor({ state: "hidden" });
      }
    } else if (section === "logs") {
      await frame
        .locator(".line")
        .filter({ hasText: "Lesefehler auf dem Testgerät." })
        .waitFor();
      await shot("journal");
      if (!baseline) {
        await frame.getByRole("checkbox").first().uncheck();
        await narrow();
        await frame
          .locator(".actions")
          .getByRole("button", { name: "Weitere Aktionen" })
          .press("Enter");
        assert.equal(await frame.getByRole("menuitem").count(), 2);
        await page.keyboard.press("Escape");
        assert.equal(
          await frame.getByRole("checkbox").first().isChecked(),
          false,
        );
      }
    } else if (section === "users") {
      const sam = frame.locator('[data-account="sam"]');
      await sam.waitFor();
      await sam.click();
      await shot("konto");
      if (!baseline) {
        assert.equal(await button("Neues Konto").count(), 1);
        await frame.getByRole("searchbox").fill("saM");
        assert.equal(await frame.locator(".acct[data-account]").count(), 1);
        await narrow();
        if (!(await sam.isVisible()))
          await frame.locator(".account .back").click();
        await sam.click();
        const back = frame.locator(".account .back");
        await focused(back);
        await back.click();
        await focused(sam);
        await button("Neues Konto").click();
        await frame.getByRole("dialog").waitFor();
        await shot("neues-konto");
        await page.keyboard.press("Escape");
        await frame.getByRole("dialog").waitFor({ state: "hidden" });
      }
    }
    console.log(baseline ? "Vorher" : "Nachher", section, "OK");
  } finally {
    await ctx.close();
  }
}

try {
  for (const [section, route] of sections) {
    if (
      process.env.UI_SECTION &&
      !process.env.UI_SECTION.split(",").includes(section)
    )
      continue;
    if (before) await test(before, true, section, route);
    await test(current, false, section, route);
  }
  const { ctx, page } = await fixture("owner", "graphit", 390, "de", {
    isMobile: true,
    hasTouch: true,
  });
  try {
    await populate(ctx);
    await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
    await page.goto(current + "/users");
    await page.locator('[data-account="sam"]').waitFor();
    assert.equal(await page.locator(".find-account").count(), 0);
    await page.goto(current + "/lxc");
    await page
      .locator(".list")
      .getByRole("button", { name: /Webserver/ })
      .waitFor();
    assert.equal(await page.locator(".container-picker").count(), 0);
    await page.goto(current + "/tools/logs");
    await page
      .getByRole("button", { name: "Vorigen Start herunterladen" })
      .waitFor();
    assert.equal(
      await page
        .locator(".actions")
        .getByRole("button", { name: "Weitere Aktionen" })
        .count(),
      0,
    );
    console.log("Mobile Fachansichten bei 390 CSS-Pixeln OK");
  } finally {
    await ctx.close();
  }
  assert.deepEqual(errors, []);
} finally {
  await browser.close();
}
