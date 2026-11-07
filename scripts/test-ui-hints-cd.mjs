// Conditional hints against isolated fixtures (plan 6e, 7a, 7h):
// - Stick backup: warning only for a backup without password on an
//   unencrypted volume, and for copies without password in the list.
// - Storage: one-time hint for volumes without checks, dismissed per account.
// - ext4/XFS: no snapshot plan, with the reason, in the create dialog and
//   on the maintenance page.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs UI_URL=http://127.0.0.1:5173 node scripts/test-ui-hints-cd.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5173";
const out = process.env.UI_OUTPUT || "out/validation/cd/hinweise";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);
const shot = (page, name) =>
  page.screenshot({
    path: `${out}/${name}.png`,
    fullPage: true,
    animations: "disabled",
  });
const noSideScroll = async (page) => {
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
};

const volume = (name, over = {}) => ({
  name,
  level: "raid1",
  filesystem: "ext4",
  mountPoint: `/mnt/${name}`,
  mounted: true,
  state: "ok",
  sizeBytes: 1e12,
  usedBytes: 4e11,
  raidDisks: 2,
  members: [],
  sync: null,
  mismatches: 0,
  shareQuotas: "off",
  ...over,
});
const encryption = { locked: false, members: 2, unlocked: 2, passwords: 1 };
const backup = (name, encrypted) => ({
  name,
  volume: "Data",
  time: "2026-11-02T03:00:00Z",
  sizeBytes: 48e3,
  hostname: "titan-test",
  version: "0.47.1",
  encrypted,
});
async function configBackup(page, { encrypt, dataEncrypted, backups }) {
  await page.route("**/api/v1/storage", (route) =>
    route.fulfill({
      json: {
        volumes: [
          volume("Data", dataEncrypted ? { encryption } : {}),
          volume("Safe", { encryption }),
        ],
        disks: [],
        diskSleep: { idleMinutes: 0, available: true, errors: [] },
      },
    }),
  );
  await page.route("**/api/v1/config-backup", (route) =>
    route.fulfill({
      json: {
        backup: {
          settings: {
            enabled: true,
            volume: "",
            keep: 10,
            schedule: "",
            time: "",
            weekday: 1,
            encrypt,
          },
          target: "Data",
          lastCheck: "2026-11-02T03:00:00Z",
          lastError: "",
          backups,
          next: null,
        },
        volumes: ["Data", "Safe"],
        passwordSet: encrypt,
      },
    }),
  );
  await page.goto(base + "/settings/backups/configuration");
  await page.locator("#stick-list").waitFor();
}

try {
  // 6e: without password on an unencrypted volume, copies without password.
  for (const [name, width, theme] of [
    ["6e-plain-graphit-1440", 1440, "graphit"],
    ["6e-plain-graphit-390", 390, "graphit"],
    ["6e-plain-hell-1440", 1440, "hell"],
  ]) {
    const { ctx, page } = await context("owner", theme, width);
    await configBackup(page, {
      encrypt: false,
      dataEncrypted: false,
      backups: [backup("a.tar.gz", false), backup("b.tar.gz", true)],
    });
    await page
      .getByText("ohne Passwort auf dem unverschlüsselten Volume Data")
      .waitFor();
    assert.ok(
      await page.getByText("Kopien ohne Passwort (ohne Schloss)").isVisible(),
    );
    const options = await page
      .locator("#config-schedule select")
      .nth(1)
      .locator("option")
      .allTextContents();
    assert.deepEqual(
      options.map((o) => o.replace(/\s+/g, " ").trim()),
      [
        "Automatisch (erstes Volume)",
        "Data · unverschlüsselt",
        "Safe · verschlüsselt",
      ],
    );
    await shot(page, name);
    if (width === 390) await noSideScroll(page);
    await ctx.close();
  }
  // 6e: encrypted volume and encrypted copies: no warning.
  {
    const { ctx, page } = await context("owner", "graphit", 1440);
    await configBackup(page, {
      encrypt: false,
      dataEncrypted: true,
      backups: [backup("b.tar.gz", true)],
    });
    await page
      .getByText(
        "Alle Kopien sind mit dem Passwort der Sicherung verschlüsselt",
      )
      .waitFor();
    assert.equal(await page.getByText("unverschlüsselten Volume").count(), 0);
    assert.equal(await page.getByText("Kopien ohne Passwort").count(), 0);
    await shot(page, "6e-encrypted-graphit-1440");
    await ctx.close();
  }
  // 6e: with a password the unencrypted volume needs no warning either.
  {
    const { ctx, page } = await context("owner", "graphit", 1440);
    await configBackup(page, {
      encrypt: true,
      dataEncrypted: false,
      backups: [backup("b.tar.gz", true)],
    });
    await page
      .getByText(
        "Alle Kopien sind mit dem Passwort der Sicherung verschlüsselt",
      )
      .waitFor();
    assert.equal(await page.getByText("unverschlüsselten Volume").count(), 0);
    await ctx.close();
  }

  // 7a: hint for volumes without checks, dismissed per account.
  for (const [name, width, theme] of [
    ["7a-hint-graphit-1440", 1440, "graphit"],
    ["7a-hint-graphit-390", 390, "graphit"],
    ["7a-hint-hell-390", 390, "hell"],
  ]) {
    const { ctx, page, data } = await context("owner", theme, width);
    // Data has a monthly RAID check; Fast and Safe have none.
    data.maintenance.settings.enabled = true;
    data.maintenance.settings.jobs = [
      {
        id: "j1",
        name: "Data: RAID-Prüfung",
        kind: "raid-check",
        target: "Data",
        enabled: true,
        every: "monthly",
        weekdays: [],
        monthDay: 1,
        time: "03:30",
        missed: "window",
        readData: 0,
        maxBytes: 0,
        maxFiles: 0,
        keep: 0,
        allowPrune: false,
      },
    ];
    const puts = [];
    await page.route("**/api/v1/preferences/hints", (route) => {
      puts.push(route.request().postDataJSON());
      data.prefs.hints = puts.at(-1);
      return route.fulfill({ status: 204 });
    });
    await page.goto(base + "/storage");
    const hint = page.getByRole("region", {
      name: "Regelmäßige Prüfungen fehlen",
    });
    await hint
      .getByText(
        "Für die Volumes Fast, Safe ist keine regelmäßige Prüfung geplant",
      )
      .waitFor();
    assert.equal(
      await hint
        .getByRole("link", { name: "Prüfungen einrichten" })
        .getAttribute("href"),
      "/tools/maintenance",
    );
    await shot(page, name);
    if (width === 390) await noSideScroll(page);
    if (name === "7a-hint-graphit-1440") {
      await hint.getByRole("button", { name: "Nicht mehr anzeigen" }).click();
      await hint.waitFor({ state: "detached" });
      assert.deepEqual(puts, [{ checksDismissed: ["Fast", "Safe"] }]);
      await page.reload();
      await page.getByRole("heading", { name: "Speicher" }).first().waitFor();
      await page.waitForTimeout(500);
      assert.equal(
        await page.getByText("Regelmäßige Prüfungen fehlen").count(),
        0,
      );
    }
    await ctx.close();
  }
  // 7a: a plain user never sees it (the maintenance plan is for admins).
  {
    const { ctx, page } = await context("user");
    await page.goto(base + "/storage");
    await page.getByRole("heading", { name: "Speicher" }).first().waitFor();
    await page.waitForTimeout(500);
    assert.equal(
      await page.getByText("Regelmäßige Prüfungen fehlen").count(),
      0,
    );
    await ctx.close();
  }

  // 7h: ext4 offers no snapshot plan, with the reason.
  for (const [name, width, theme] of [
    ["7h-create-graphit-1440", 1440, "graphit"],
    ["7h-create-graphit-390", 390, "graphit"],
    ["7h-create-hell-1440", 1440, "hell"],
  ]) {
    const { ctx, page } = await context("owner", theme, width);
    await page.goto(base + "/storage");
    await page.getByRole("button", { name: "Neues Volume" }).click();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByText(
        "ext4/XFS haben keine Snapshots. Dafür als Dateisystem Btrfs wählen.",
      )
      .waitFor();
    const box = dialog.getByRole("checkbox", { name: /Snapshots täglich/ });
    assert.ok(await box.isDisabled());
    assert.equal(await box.isChecked(), false);
    await dialog
      .getByText("ext4/XFS haben keine Snapshots")
      .scrollIntoViewIfNeeded();
    await shot(page, name);
    if (width === 1440 && theme === "graphit") {
      await dialog.getByRole("radio", { name: /Btrfs/ }).click();
      assert.ok(await box.isEnabled());
      assert.ok(await box.isChecked());
      assert.equal(
        await dialog.getByText("ext4/XFS haben keine Snapshots").count(),
        0,
      );
    }
    await ctx.close();
  }
  // 7h: maintenance lists ext4 volumes for snapshots only locked, with the reason.
  for (const [name, width] of [
    ["7h-maintenance-graphit-1440", 1440],
    ["7h-maintenance-graphit-390", 390],
  ]) {
    const { ctx, page } = await context("owner", "graphit", width);
    await page.goto(base + "/tools/maintenance");
    await page.getByRole("button", { name: "Zeitplan hinzufügen" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Vorgang").selectOption("snapshot");
    await dialog
      .getByText(
        "ext4/XFS haben keine Snapshots. Geplante Snapshots gibt es nur für Btrfs-Volumes",
      )
      .waitFor();
    const target = dialog.getByLabel("Ziel");
    const options = await target
      .locator("option")
      .evaluateAll((list) =>
        list.map((o) => [o.textContent.trim(), o.disabled]),
      );
    assert.deepEqual(options, [
      ["Bitte wählen", false],
      ["Data · ext4 hat keine Snapshots", true],
      ["Fast", false],
      ["Safe · ext4 hat keine Snapshots", true],
    ]);
    await shot(page, name);
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log(`ok – screenshots in ${out}`);
} finally {
  await browser.close();
}
