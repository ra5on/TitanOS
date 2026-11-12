// Settings › Data and backup › System data (PLAN 79-8) and the backup job's
// group "Laufende VMs" with the grouped notes of the last run (PLAN 79-9,
// 79-13). At 1100 and 390 px: no sideways scroll, no cut text, the choices
// are sent as the API expects them. Real screenshots (deviceScaleFactor 2)
// into UI_OUTPUT/systemdaten and UI_OUTPUT/sicherung-vms, or into
// SYSDATA_SHOTS and VMDISK_SHOTS when set. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5262 node scripts/test-ui-systemdata.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5262";
const out = process.env.UI_OUTPUT || "out/validation/systemdata";
const sysShots = process.env.SYSDATA_SHOTS || `${out}/systemdaten`;
const vmShots = process.env.VMDISK_SHOTS || `${out}/sicherung-vms`;
await mkdir(sysShots, { recursive: true });
await mkdir(vmShots, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);
const shot = (page, dir, name) =>
  page.screenshot({
    path: `${dir}/${name}.png`,
    fullPage: true,
    animations: "disabled",
  });
// The part of a long page the drawing shows, legible on its own.
const part = (page, selector, dir, name) =>
  page
    .locator(selector)
    .first()
    .screenshot({ path: `${dir}/${name}.png`, animations: "disabled" });
async function sound(page, name) {
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `${name}: page scrolls sideways by ${overflow}px`);
  const cut = await page.evaluate(cutTextIn);
  assert.deepEqual(cut, [], `${name}: cut text`);
}
const at = "2026-11-11T21:12:00Z";
const configBackup = {
  backup: {
    settings: {
      enabled: true,
      volume: "",
      keep: 14,
      schedule: "",
      time: "",
      weekday: 1,
      encrypt: true,
      pace: "daily",
    },
    target: "nvme",
    lastCheck: at,
    lastError: "",
    backups: Array.from({ length: 14 }, (_, i) => ({
      name: `config-${i}.tar.gz`,
      volume: "nvme",
      time: at,
      sizeBytes: 2846135,
      hostname: "titan",
      version: "0.77.0",
      encrypted: true,
    })),
    next: null,
  },
  volumes: ["Backup", "nvme"],
  passwordSet: true,
};
const disk = (vm, file, over = {}) => ({
  vm,
  path: `vms/${vm}/${file}`,
  file: `/mnt/nvme/vms/${vm}/${file}`,
  format: "qcow2",
  sizeBytes: 32 * 2 ** 30,
  state: "running",
  way: "btrfs_snapshot",
  agent: false,
  ...over,
});
// What GET /backups/vm-disks answers for a choice: the Btrfs disk is always
// read from a snapshot, the qcow2 disk on ext4 only with libvirtSnapshot,
// and disks without a snapshot are left out or the VM is paused.
const vmDisks = (choice) => {
  const skip = choice?.mode === "skip";
  const noSnapshot = (reason) =>
    !choice
      ? { way: "as_is" }
      : skip
        ? { way: "skip" }
        : { way: choice.raw === "pause" ? "pause" : "skip", reason };
  return {
    choice: choice ?? null,
    choiceNeeded: !choice,
    disks: [
      disk("Home Assistant", "haos_ova-18.3.qcow2", {
        way: skip ? "skip" : "btrfs_snapshot",
      }),
      disk("debian", "vdisk1.qcow2", {
        sizeBytes: 64 * 2 ** 30,
        ...(choice?.libvirtSnapshot && !skip
          ? { way: "libvirt_snapshot", agent: true }
          : noSnapshot("vm_disk_snapshot_unproven")),
      }),
      disk("win11", "system.img", {
        format: "raw",
        sizeBytes: 120 * 2 ** 30,
        ...noSnapshot("vm_disk_raw_image"),
      }),
      disk("archiv", "alt.qcow2", {
        state: "stopped",
        way: "copy",
        sizeBytes: 8 * 2 ** 30,
      }),
    ],
  };
};
const job = (over = {}) => ({
  job: {
    id: "Backup-alles",
    sources: ["vms", "Medien"],
    applications: [],
    enabled: true,
    every: "daily",
    time: "03:00",
    times: ["03:00"],
    keep: { daily: 0, weekly: 0, monthly: 0 },
    copy: { dated: false, mirror: false },
    destination: { type: "local-copy", volume: "Backup", folder: "kopie" },
    ...over,
  },
  secretSet: {},
  stateKnown: true,
  next: "2026-11-12T02:00:00Z",
  state: {
    running: false,
    phase: "",
    percent: 0,
    bytesDone: 0,
    bytesTotal: 0,
    lastRun: "2026-11-11T02:14:00Z",
    lastOK: "2026-11-11T02:14:00Z",
    lastError: "",
    warning:
      "25 links the drive cannot hold: Medien/aktuell, Dokumente/neu, …; 1 disk of a running VM not saved: vms/win11/system.img",
    skipped: 26,
    bytes: 412 * 2 ** 30,
    restoredTo: "",
    problems: [
      {
        code: "link_unsupported",
        count: 25,
        paths: [
          "Medien/aktuell",
          "Dokumente/neu",
          "Medien/Filme/zuletzt-gesehen",
          "Medien/Musik/Favoriten/Sommer 2026",
          "Dokumente/Steuer/2025/Belege-Verknüpfung",
        ],
      },
      { code: "vm_disk_changed", count: 1, paths: ["vms/win11/system.img"] },
    ],
    notices: [
      {
        code: "vm_disk_crash_consistent",
        count: 1,
        paths: ["vms/debian/vdisk1.qcow2"],
      },
      {
        code: "vm_disk_choice_needed",
        count: 3,
        paths: [
          "vms/Home Assistant/haos_ova-18.3.qcow2",
          "vms/debian/vdisk1.qcow2",
          "vms/win11/system.img",
        ],
      },
    ],
  },
});

try {
  for (const [width, name] of [
    [1100, "1100"],
    [390, "390"],
  ]) {
    // --- System data ------------------------------------------------------
    const { ctx, page, data } = await create("owner", "graphit", width, "de", {
      deviceScaleFactor: 2,
    });
    await page.route("**/api/v1/config-backup", (route) =>
      route.fulfill({ json: configBackup }),
    );
    data.sysdata.functions.find((f) => f.id === "activity").aside = [
      "2026-11-10.jsonl.from-Backup",
    ];
    await page.goto(base + "/settings/systemdata");
    await page.getByRole("heading", { name: "Systemdaten" }).waitFor();
    await page
      .getByText("Gute Wahl: Dieses Volume hat keine Platten im Ruhemodus.")
      .waitFor();
    await page
      .getByText(
        "Die Platten von „Backup“ werden durch Systemdaten nicht mehr geweckt.",
      )
      .waitFor();
    assert.deepEqual(
      (await page.locator("#sysdata-common option").allTextContents()).map(
        (o) => o.trim(),
      ),
      [
        "Automatisch (nvme)",
        "Backup · Platten im Ruhemodus",
        "nvme · schläft nie",
      ],
    );
    for (const row of [
      "Systemprotokoll",
      "Aktivitätsprotokoll",
      "Meldungen",
      "SMART-Verlauf",
      "Monitoring-Verlauf",
    ])
      await page.getByText(row, { exact: true }).first().waitFor();
    await page
      .getByText(/Journal über einen Neustart hinaus · 2\d\d/)
      .waitFor();
    await page
      .getByText("Auslastung der letzten Tage · Ausgeschaltet")
      .waitFor();
    assert.equal(
      (
        await page.locator("#sysdata-fn-logs option").first().textContent()
      ).trim(),
      "Ablage-Volume (nvme)",
    );
    // The configuration backup opens downward: when, password, copies.
    await page.getByText(/14 Stände/).waitFor();
    if (!(await page.locator("#sysdata-pace").count()))
      await page
        .locator("#config")
        .getByRole("button", { expanded: false })
        .first()
        .click();
    await page.locator("#sysdata-pace").waitFor();
    assert.equal(await page.locator("#sysdata-pace").inputValue(), "daily");
    await page
      .getByText("Nach Änderungen, höchstens einmal am Tag", { exact: true })
      .waitFor();
    assert.equal(
      await page
        .getByRole("link", { name: "Anzeigen", exact: true })
        .getAttribute("href"),
      "/settings/backups/configuration#config-downloads",
    );
    // Entries a move left next to the data in use are named with their path.
    await page
      .getByText("Aktivitätsprotokoll: 1 Eintrag beiseitegelegt")
      .waitFor();
    await page
      .getByText("/mnt/nvme/.titanos/activity/2026-11-10.jsonl.from-Backup")
      .waitFor();
    await sound(page, `systemdaten-${name}`);
    await shot(page, sysShots, `systemdaten-${name}`);

    // A volume with sleeping disks: the warning, and what is sent.
    await page.locator("#sysdata-common").selectOption("Backup");
    await page
      .getByText(
        "„Backup“ besteht aus Festplatten, die schlafen dürfen. Systemdaten wecken sie regelmäßig.",
      )
      .waitFor();
    assert.deepEqual(data.sysdataCalls.at(-1), { volume: "Backup" });
    await page.locator("#sysdata-fn-smart").selectOption("nvme");
    await page
      .locator("#sysdata-fn-smart")
      .and(page.locator(":enabled"))
      .waitFor();
    assert.deepEqual(data.sysdataCalls.at(-1), {
      functions: { smart: "nvme" },
    });
    assert.equal(
      (
        await page.locator("#sysdata-fn-logs option").first().textContent()
      ).trim(),
      "Ablage-Volume (Backup)",
    );
    await sound(page, `systemdaten-ruhemodus-${name}`);
    await shot(page, sysShots, `systemdaten-ruhemodus-${name}`);

    // The search of the control panel and the area page find it.
    await page.goto(base + "/settings?area=data");
    await page.getByRole("link", { name: /Systemdaten/ }).waitFor();
    await sound(page, `bereich-${name}`);
    await shot(page, sysShots, `bereich-daten-${name}`);
    await page.goto(base + "/settings");
    await page.getByRole("searchbox").first().fill("ablage");
    await page
      .getByRole("link", { name: /Systemdaten/ })
      .first()
      .waitFor();
    await ctx.close();

    // --- Backup job: notes of the last run, disks of running VMs ----------
    const b = await create("owner", "graphit", width, "de", {
      deviceScaleFactor: 2,
    });
    b.data.backups = [job()];
    b.data.vmDisks = vmDisks(undefined);
    let asked = [];
    let saved = null;
    await b.page.route("**/api/v1/backups", (route) =>
      route.fulfill({
        json: {
          jobs: b.data.backups,
          shares: [
            { name: "vms", volume: "nvme" },
            { name: "Medien", volume: "nvme" },
          ],
          volumes: ["Backup", "nvme"],
          smbTargets: [],
        },
      }),
    );
    await b.page.route("**/api/v1/backups/vm-disks?*", (route) => {
      const query = new URL(route.request().url()).searchParams;
      asked.push(query.toString());
      const mode = query.get("mode");
      return route.fulfill({
        json: vmDisks(
          mode
            ? {
                mode,
                raw: query.get("raw") ?? undefined,
                libvirtSnapshot: query.get("libvirtSnapshot") === "true",
              }
            : undefined,
        ),
      });
    });
    await b.page.route("**/api/v1/backups/Backup-alles", (route) => {
      if (route.request().method() !== "PUT") return route.fallback();
      saved = route.request().postDataJSON();
      b.data.backups = [job({ vmDisks: saved.vmDisks })];
      return route.fulfill({
        json: {
          jobs: b.data.backups,
          shares: [
            { name: "vms", volume: "nvme" },
            { name: "Medien", volume: "nvme" },
          ],
          volumes: ["Backup", "nvme"],
          smbTargets: [],
        },
      });
    });
    await b.page.route("**/api/v1/backups/target", (route) =>
      route.fulfill({
        json: {
          target: {
            enabled: false,
            volume: "",
            port: 8751,
            sources: [],
            listening: [],
            problem: "",
          },
          volumes: ["Backup", "nvme"],
        },
      }),
    );
    await b.page.route("**/api/v1/replication", (route) =>
      route.fulfill({ json: { jobs: [], received: [], receiveVolume: "" } }),
    );
    const p = b.page;
    await p.goto(base + "/settings/backups/data#backup-Backup-alles");
    await p.getByText("Fertig mit Hinweisen", { exact: true }).waitFor();
    await p.getByText("4 Hinweise", { exact: true }).waitFor();
    for (const text of [
      "25 Verknüpfungen nicht gesichert",
      "1 Platte einer laufenden VM nicht verlässlich gesichert",
      "1 VM-Platte ohne Gast-Agent gesichert",
      "3 Platten laufender VMs: Wahl fehlt",
      "Das Dateisystem des Ziels kann keine Verknüpfungen speichern.",
    ])
      await p.getByText(text, { exact: true }).waitFor();
    // The English sentence of the NAS is not shown once there are codes.
    assert.equal(await p.getByText(/links the drive cannot hold/).count(), 0);
    assert.equal(
      await p.getByText("Dokumente/Steuer/2025/Belege-Verknüpfung").count(),
      0,
    );
    await sound(p, `hinweise-${name}`);
    await shot(p, vmShots, `letzter-lauf-${name}`);
    await part(
      p,
      ".job:not(.hidden-job)",
      vmShots,
      `ausschnitt-letzter-lauf-${name}`,
    );
    await p.getByRole("button", { name: "Alle anzeigen" }).first().click();
    await p.getByText("Dokumente/Steuer/2025/Belege-Verknüpfung").waitFor();
    await p.getByText("… und 20 weitere", { exact: true }).waitFor();
    await sound(p, `hinweise-alle-${name}`);
    await shot(p, vmShots, `letzter-lauf-alle-${name}`);
    await part(
      p,
      ".job:not(.hidden-job)",
      vmShots,
      `ausschnitt-letzter-lauf-alle-${name}`,
    );

    // "Wahl ändern" opens the job with the group "Laufende VMs".
    await p.getByRole("button", { name: "Wahl ändern" }).first().click();
    await p.getByRole("heading", { name: "Laufende VMs" }).waitFor();
    await p.getByText("4 VM-Platten in den Quellen dieses Auftrags").waitFor();
    await p.getByText(/Für diesen Auftrag ist noch nichts gewählt/).waitFor();
    assert.equal(await p.locator("#backup-vm-mode").inputValue(), "");
    assert.equal(
      (await p.locator("#backup-vm-mode option:checked").textContent()).trim(),
      "Bitte wählen",
    );
    assert.ok(
      asked.at(-1).includes("source=vms") && !asked.at(-1).includes("mode="),
    );
    await p.locator("#backup-vm-disks").scrollIntoViewIfNeeded();
    await sound(p, `vm-bitte-waehlen-${name}`);
    await shot(p, vmShots, `laufende-vms-bitte-waehlen-${name}`);
    await part(
      p,
      "#backup-vm-disks",
      vmShots,
      `ausschnitt-laufende-vms-bitte-waehlen-${name}`,
    );

    // "So sichern": Btrfs from a snapshot; the qcow2 disk on ext4 and the
    // raw image have no snapshot and are left out unless the VM may pause.
    await p.locator("#backup-vm-mode").selectOption("consistent");
    await p.getByText("Btrfs-Snapshot · VM läuft weiter").waitFor();
    await p.getByText("VM ist aus · wird normal kopiert").waitFor();
    await p
      .getByText(
        /läuft · qcow2 · Snapshot der VM-Verwaltung ist nicht eingeschaltet/,
      )
      .waitFor();
    await p.getByText(/1\d\d(,\d+)? GB · läuft · Roh-Abbild$/).waitFor();
    assert.equal(
      await p
        .locator("#backup-vm-disks .chip", { hasText: "Wird übersprungen" })
        .count(),
      2,
    );
    assert.equal(await p.locator("#backup-vm-libvirt").isChecked(), false);
    assert.equal(await p.locator("#backup-vm-raw").inputValue(), "skip");
    await p
      .getByText(
        /in diesem Lauf nicht gelesen, die vorige Kopie am Ziel bleibt/,
      )
      .waitFor();
    assert.ok(asked.at(-1).includes("mode=consistent&raw=skip"), asked.at(-1));
    assert.ok(!asked.at(-1).includes("libvirtSnapshot"), asked.at(-1));
    await sound(p, `vm-so-sichern-${name}`);
    await shot(p, vmShots, `laufende-vms-so-sichern-${name}`);
    await part(
      p,
      "#backup-vm-disks",
      vmShots,
      `ausschnitt-laufende-vms-so-sichern-${name}`,
    );
    // A qcow2 disk without the snapshot can choose "Kurz anhalten" too.
    await p.locator("#backup-vm-raw").selectOption("pause");
    await p
      .locator("#backup-vm-disks .chip", { hasText: "VM wird kurz angehalten" })
      .nth(1)
      .waitFor();
    await shot(p, vmShots, `laufende-vms-kurz-anhalten-${name}`);
    await part(
      p,
      "#backup-vm-disks",
      vmShots,
      `ausschnitt-laufende-vms-kurz-anhalten-${name}`,
    );
    // The snapshot of the VM management is a switch, off unless chosen.
    await p
      .getByText(/Das Verfahren ist neu und nur begrenzt erprobt/)
      .waitFor();
    await p.locator("#backup-vm-libvirt").check();
    await p
      .getByText("Snapshot der VM-Verwaltung · sauber eingefroren")
      .waitFor();
    assert.ok(asked.at(-1).includes("libvirtSnapshot=true"), asked.at(-1));
    await sound(p, `vm-snapshot-${name}`);
    await shot(p, vmShots, `laufende-vms-vm-snapshot-${name}`);
    await part(
      p,
      "#backup-vm-disks",
      vmShots,
      `ausschnitt-laufende-vms-vm-snapshot-${name}`,
    );
    await Promise.all([
      p.waitForResponse(
        (res) =>
          res.url().endsWith("/backups/Backup-alles") &&
          res.request().method() === "PUT",
      ),
      p.getByRole("button", { name: "Speichern", exact: true }).click(),
    ]);
    await p
      .getByRole("heading", { name: "Laufende VMs" })
      .waitFor({ state: "detached" });
    assert.deepEqual(saved.vmDisks, {
      mode: "consistent",
      raw: "pause",
      libvirtSnapshot: true,
    });
    await p
      .locator("button", { hasText: /^Bearbeiten$/ })
      .first()
      .click();
    await p.getByRole("heading", { name: "Laufende VMs" }).waitFor();
    assert.equal(await p.locator("#backup-vm-libvirt").isChecked(), true);

    await p.locator("#backup-vm-mode").selectOption("skip");
    await p.getByText(/Wahl „Überspringen“: Platten laufender VMs/).waitFor();
    await p.getByText("Wird übersprungen", { exact: true }).first().waitFor();
    assert.equal(await p.locator("#backup-vm-raw").count(), 0);
    await sound(p, `vm-ueberspringen-${name}`);
    await shot(p, vmShots, `laufende-vms-ueberspringen-${name}`);
    await part(
      p,
      "#backup-vm-disks",
      vmShots,
      `ausschnitt-laufende-vms-ueberspringen-${name}`,
    );
    await Promise.all([
      p.waitForResponse(
        (res) =>
          res.url().endsWith("/backups/Backup-alles") &&
          res.request().method() === "PUT",
      ),
      p.getByRole("button", { name: "Speichern", exact: true }).click(),
    ]);
    await p
      .getByRole("heading", { name: "Laufende VMs" })
      .waitFor({ state: "detached" });
    assert.deepEqual(saved.vmDisks, { mode: "skip" });

    // A NAS before 0.77 sends only its sentence: shown as it is.
    const old = job();
    delete old.state.problems;
    delete old.state.notices;
    b.data.backups = [old];
    await p.reload();
    await p.getByText(/links the drive cannot hold/).waitFor();
    assert.equal(await p.getByText("Fertig mit Hinweisen").count(), 0);
    await b.ctx.close();

    // --- A VM held after an interrupted backup of its running disks -------
    const v = await create("owner", "graphit", width, "de", {
      deviceScaleFactor: 2,
    });
    let repaired = 0;
    let held = true;
    await v.page.route("**/api/v1/vms/source/start", (route) =>
      held
        ? route.fulfill({
            status: 409,
            json: {
              error: {
                code: "vm_backup_recovery",
                message:
                  "vm: the record of an interrupted backup is unreadable; repair the VM's disks first",
              },
            },
          })
        : route.fulfill({ status: 202 }),
    );
    await v.page.route("**/api/v1/vms/source/backup-recovery", (route) => {
      repaired++;
      held = false;
      return route.fulfill({ status: 204 });
    });
    await v.page.goto(base + "/vms?vm=source");
    await v.page
      .getByRole("button", { name: "Starten", exact: true })
      .first()
      .click();
    await v.page
      .getByText(
        /Platten nach unterbrochener Sicherung prüfen\. Eine Sicherung/,
      )
      .waitFor();
    assert.equal(await v.page.getByText(/record of an interrupted/).count(), 0);
    await sound(v.page, `vm-gehalten-${name}`);
    await shot(v.page, vmShots, `vm-gehalten-${name}`);
    await v.page
      .getByRole("button", { name: "Platten zurückführen …", exact: true })
      .click();
    const dialog = v.page.getByRole("dialog");
    await dialog.getByText(/prüft jede Zwischenschicht/).waitFor();
    // A plain confirmation: nothing to type.
    assert.equal(await dialog.locator("input, textarea").count(), 0);
    await shot(v.page, vmShots, `vm-gehalten-dialog-${name}`);
    await dialog
      .getByRole("button", { name: "Zurückführen", exact: true })
      .click();
    await v.page
      .getByText("Die Platten von Source VM sind wieder in Ordnung.")
      .waitFor();
    assert.equal(repaired, 1);
    assert.equal(
      await v.page
        .getByText(/Platten nach unterbrochener Sicherung prüfen/)
        .count(),
      0,
    );
    await v.ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("ok: system data and backup notes");
} finally {
  await browser.close();
}
