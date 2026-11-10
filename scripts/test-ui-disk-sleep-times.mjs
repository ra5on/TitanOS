// Plan 28b/28d/28g: "schläft seit" / "zuletzt geweckt" per disk, the disk
// sleep section in the disk details and the entry under Settings › Energy.
// Isolated API fixtures only; no disk is touched.
// PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs UI_URL=http://127.0.0.1:5190 node scripts/test-ui-disk-sleep-times.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5190";
const out = process.env.UI_OUTPUT || "out/validation/cd/ruhemodus";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

// Times of today, as the browser (same time zone as node here) shows them.
const now = new Date();
const minutesAgo = (minutes) => {
  const date = new Date(now.getTime() - minutes * 60000);
  // Stay on today's date so the text has no date part.
  return date.toDateString() === now.toDateString()
    ? date
    : new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 1);
};
const asleepAt = minutesAgo(42),
  wokenAt = minutesAgo(7);
const hhmm = (date) =>
  new Intl.DateTimeFormat("de", { hour: "2-digit", minute: "2-digit" }).format(
    date,
  );

const hdd = (name, serial, extra = {}) => ({
  identity: `serial:${serial}`,
  name,
  path: `/dev/${name}`,
  model: "Fixture HDD",
  serial,
  sizeBytes: 8e12,
  kind: "hdd",
  role: "member",
  volume: "Data",
  memberState: "active",
  smart: null,
  ...extra,
});
const sleepDisk = (name, serial, state, since, extra = {}) => ({
  id: `serial:${serial}`,
  name,
  model: "Fixture HDD",
  serial,
  sizeBytes: 8e12,
  volume: "Data",
  override: null,
  effective: 30,
  origin: "volume",
  state,
  ...(since ? { since: since.toISOString() } : {}),
  ...extra,
});
function sleepStatus() {
  return {
    idleMinutes: 15,
    volumes: { Data: 30 },
    disks: { "serial:OTHER": 60 },
    available: true,
    errors: [],
    listed: true,
    volumeStatus: [
      { name: "Data", override: 30, effective: 30, origin: "volume" },
    ],
    diskStatus: [
      sleepDisk("sda", "A1", "asleep", asleepAt),
      sleepDisk("sdb", "B1", "active", wokenAt),
      // Nothing known since the start of titand: no time.
      sleepDisk("sdc", "C1", "active", null),
    ],
    unavailable: [
      {
        name: "nvme0n1",
        model: "Fixture NVMe",
        sizeBytes: 1e12,
        reason: "nvme",
      },
    ],
  };
}

async function open(theme = "graphit", width = 1440) {
  const made = await create("owner", theme, width);
  const { ctx } = made;
  const puts = [];
  made.puts = puts;
  // Later routes win over the shared fixtures.
  await ctx.route("**/api/v1/storage", (route) =>
    route.fulfill({
      json: {
        volumes: [
          {
            name: "Data",
            level: "raid1",
            filesystem: "ext4",
            mountPoint: "/mnt/Data",
            mounted: true,
            state: "ok",
            sizeBytes: 8e12,
            usedBytes: 1e12,
            raidDisks: 3,
            members: [],
            sync: null,
            mismatches: 0,
          },
        ],
        disks: [
          hdd("sda", "A1"),
          hdd("sdb", "B1"),
          hdd("sdc", "C1"),
          {
            ...hdd("nvme0n1", "N1", { kind: "nvme", model: "Fixture NVMe" }),
            role: "free",
            volume: undefined,
            memberState: undefined,
          },
        ],
        diskSleep: sleepStatus(),
      },
    }),
  );
  await ctx.route("**/api/v1/storage/disk-sleep", (route) => {
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON();
      puts.push(body);
      const status = sleepStatus();
      Object.assign(status, body);
      return route.fulfill({ json: status });
    }
    return route.fulfill({ json: sleepStatus() });
  });
  await ctx.route("**/api/v1/system/energy", (route) =>
    route.fulfill({
      json: {
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
      },
    }),
  );
  await ctx.route("**/api/v1/system/power-schedule", (route) =>
    route.fulfill({
      json: {
        settings: {
          off: { enabled: false, days: [], time: "23:00" },
          on: { enabled: false, days: [], time: "07:00" },
          wakeOnLan: false,
        },
        rtc: true,
        interfaces: [],
      },
    }),
  );
  return made;
}

try {
  // Laufwerke: the column shows the time with the state.
  {
    const { ctx, page, puts } = await open();
    await page.goto(`${base}/storage?tab=disks`);
    const row = (name) => page.locator("tr", { hasText: name }).first();
    await row("sda")
      .getByText(`schläft seit ${hhmm(asleepAt)}`)
      .waitFor();
    await row("sdb")
      .getByText(`zuletzt geweckt ${hhmm(wokenAt)}`)
      .waitFor();
    assert.equal(
      await row("sdc")
        .getByText(/schläft seit|zuletzt geweckt/)
        .count(),
      0,
      "no time when titand knows none",
    );
    await page.screenshot({
      path: `${out}/laufwerke-1440.png`,
      fullPage: true,
    });

    // Disk details: the section with value, state and time.
    await row("sdb").locator("button").first().click();
    const details = page.locator("aside.disk-details");
    const section = details.locator("section.sleep");
    await section.getByRole("heading", { name: "Ruhemodus" }).waitFor();
    await section.getByText(`zuletzt geweckt ${hhmm(wokenAt)}`).waitFor();
    await section.getByText("aktiv", { exact: true }).waitFor();
    await details.screenshot({ path: `${out}/plattendetails-1440.png` });

    // Its own value is saved like the dialog: every other value stays.
    await section.getByRole("button", { name: "Ändern …" }).click();
    await section.getByLabel("Ruhezeit dieser Platte").selectOption("60");
    await section.getByRole("button", { name: "Speichern" }).click();
    await section.getByRole("button", { name: "Ändern …" }).waitFor();
    assert.equal(puts.length, 1);
    assert.deepEqual(puts[0], {
      idleMinutes: 15,
      volumes: { Data: 30 },
      // OTHER is no present disk: like the dialog, only present disks go.
      disks: { "serial:B1": 60 },
    });

    // The "…" opens the dialog with the times too.
    await section.getByRole("button", { name: "Ruhemodus einstellen" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByText(`schläft seit ${hhmm(asleepAt)}`).waitFor();
    await ctx.close();
  }
  // A disk that cannot sleep says why in its details.
  {
    const { ctx, page } = await open();
    await page.goto(`${base}/storage?tab=disks`);
    await page
      .locator("tr", { hasText: "nvme0n1" })
      .first()
      .locator("button")
      .first()
      .click();
    await page
      .locator("aside.disk-details section.sleep")
      .getByText("NVMe, schläft nie")
      .waitFor();
    await ctx.close();
  }
  // Einstellungen › Energie: the entry opens the one central dialog.
  for (const [theme, width] of [
    ["graphit", 1440],
    ["graphit", 390],
    ["hell", 1440],
  ]) {
    const { ctx, page } = await open(theme, width);
    await page.goto(`${base}/settings/energy`);
    // PLAN 78-1: the disk sleep is a row with its summary.
    const tile = page.locator(".srow", {
      hasText: "Ruhemodus der Festplatten",
    });
    await tile
      .getByText("Standard: 15 Minuten · 1 von 3 Platten schlafen")
      .waitFor()
      .catch(async (err) => {
        await page.screenshot({
          path: `${out}/failed-energy.png`,
          fullPage: true,
        });
        throw err;
      });
    await tile.screenshot({ path: `${out}/energie-${theme}-${width}.png` });
    if (width === 1440 && theme === "graphit")
      await page.screenshot({
        path: `${out}/energie-seite-${width}.png`,
        fullPage: true,
      });
    await tile.getByRole("button", { name: "Ruhemodus einstellen …" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByText(`zuletzt geweckt ${hhmm(wokenAt)}`).waitFor();
    await page.screenshot({
      path: `${out}/energie-dialog-${theme}-${width}.png`,
    });
    await ctx.close();
  }
  // Volume details: the members with their times.
  for (const [theme, width] of [
    ["graphit", 390],
    ["hell", 1440],
  ]) {
    const { ctx, page } = await open(theme, width);
    await page.goto(`${base}/storage#volume-Data`);
    const members = page.locator(".block.sleep .members-sleep");
    await members.getByText(`schläft seit ${hhmm(asleepAt)}`).waitFor();
    await members.getByText(`zuletzt geweckt ${hhmm(wokenAt)}`).waitFor();
    await page
      .locator(".block.sleep")
      .screenshot({ path: `${out}/volumedetails-${theme}-${width}.png` });
    await ctx.close();
  }
  // Narrow and light storage views.
  for (const [theme, width] of [
    ["graphit", 390],
    ["hell", 1440],
  ]) {
    const { ctx, page } = await open(theme, width);
    await page.goto(`${base}/storage?tab=disks`);
    await page
      .getByText(`schläft seit ${hhmm(asleepAt)}`)
      .first()
      .waitFor();
    await page.screenshot({
      path: `${out}/laufwerke-${theme}-${width}.png`,
      fullPage: true,
    });
    await page
      .locator("tr", { hasText: "sda" })
      .first()
      .locator("button")
      .first()
      .click();
    const details = page.locator("aside.disk-details");
    await details
      .locator("section.sleep")
      .getByText(`schläft seit ${hhmm(asleepAt)}`)
      .waitFor();
    await details.screenshot({
      path: `${out}/plattendetails-${theme}-${width}.png`,
    });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("disk sleep times: ok");
} finally {
  await browser.close();
}
