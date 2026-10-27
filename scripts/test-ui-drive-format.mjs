// "Format drive" in the file manager. Isolated API data only: no request
// reaches a NAS and no disk of this computer is touched.
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
const base = process.env.UI_URL || "http://127.0.0.1:5188";
const out = process.env.UI_OUTPUT || "out/validation/ui-drive-format";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const disks = () => [
  { id: "new-ssd", device: "/dev/nvme0n1", model: "Samsung SSD 980", serial: "S6", size: 1e12, transport: "nvme", kind: "nvme", readOnly: false, table: "", partitions: [], formattable: true },
  { id: "stick", device: "/dev/sdb", model: "Ultra Fit", serial: "", size: 32e9, transport: "usb", kind: "removable", readOnly: false, table: "dos", partitions: [{ device: "/dev/sdb1", fsType: "BitLocker", label: "", size: 32e9, mounted: false }], formattable: true },
  { id: "card", device: "/dev/mmcblk0", model: "", serial: "", size: 31e9, transport: "mmc", kind: "sd", readOnly: true, table: "dos", partitions: [{ device: "/dev/mmcblk0p1", fsType: "vfat", label: "CAMERA", size: 31e9, mounted: false }], formattable: false, reason: "readonly" },
  { id: "vm", device: "/dev/sdc", model: "VM disk", serial: "", size: 5e11, transport: "sata", kind: "hdd", readOnly: false, table: "gpt", partitions: [], formattable: false, reason: "claimed", claimed: "Windows 11" },
];

async function openDialog(page) {
  await page.getByRole("button", { name: "Laufwerk formatieren …" }).filter({ visible: true }).first().click();
  const dialog = page.getByRole("dialog");
  await dialog.waitFor();
  await dialog.getByRole("radio").first().waitFor();
  return dialog;
}

try {
  {
    const { ctx, page, data } = await create("owner");
    data.usbDisks = disks();
    await page.goto(base + "/files?root=share/Media&path=");
    const dialog = await openDialog(page);
    const text = await dialog.textContent();
    // Empty drives, foreign content and why others cannot be formatted.
    assert.match(text, /Samsung SSD 980/);
    assert.match(text, /Leer, ohne Partitionstabelle/);
    assert.match(text, /BitLocker/);
    assert.match(text, /Speicherkarte/);
    assert.match(text, /Schreibgeschützt/);
    assert.match(text, /Belegt: Windows 11/);
    assert.equal(await dialog.getByRole("radio", { name: /VM disk/ }).isDisabled(), true);
    await dialog.getByRole("radio", { name: /Ultra Fit/ }).click();
    assert.match(await dialog.textContent(), /Daten eines anderen Systems/);
    const fs = dialog.getByLabel("Dateisystem");
    assert.equal(await fs.inputValue(), "exfat");
    await fs.selectOption("vfat");
    const name = dialog.getByLabel("Name", { exact: true });
    await name.fill("Fotos ä");
    assert.match(await dialog.textContent(), /keine Umlaute/);
    assert.equal(await dialog.getByRole("button", { name: "Formatieren …" }).isDisabled(), true);
    await fs.selectOption("exfat");
    await name.fill("Fotos");
    await dialog.getByText("Erweitert").click();
    assert.match(await dialog.textContent(), /Automatisch \(hier MBR\)/);
    await page.screenshot({ path: `${out}/owner-options.png` });
    await dialog.getByRole("button", { name: "Formatieren …" }).click();
    // A plain confirmation, no typing.
    assert.match(await dialog.textContent(), /Alle Daten auf Ultra Fit .*\/dev\/sdb.* werden gelöscht/);
    assert.equal(data.formats?.length ?? 0, 0);
    await page.screenshot({ path: `${out}/owner-confirm.png` });
    await dialog.getByRole("button", { name: "Jetzt formatieren" }).click();
    await dialog.getByText("Dateisystem wird angelegt …").waitFor();
    assert.deepEqual(data.formats, [{ id: "stick", filesystem: "exfat", label: "Fotos", scheme: "auto", mount: true }]);
    assert.match(await dialog.textContent(), /Wird formatiert/);
    // The NAS reports the end; the dialog says so.
    data.usbJob = { ...data.usbJob, phase: "done", finished: "2026-10-23T16:01:00Z", id: "ABCD-0001" };
    await dialog.getByText("Fertig formatiert.").waitFor({ timeout: 5000 });
    await page.screenshot({ path: `${out}/owner-done.png` });
    await ctx.close();
  }
  {
    // The server refuses: the reason is shown, nothing else changes.
    const { ctx, page, data } = await create("owner");
    data.usbDisks = disks();
    data.formatError = "usb_not_formattable";
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(base + "/files?root=share/Media&path=");
    // On a phone the places open from the location button.
    await page.locator('button[aria-controls="file-places"]').click();
    const dialog = await openDialog(page);
    await dialog.getByRole("radio", { name: /Samsung SSD 980/ }).click();
    await dialog.getByRole("button", { name: "Formatieren …" }).click();
    await dialog.getByRole("button", { name: "Jetzt formatieren" }).click();
    await dialog.getByText(/kann gerade nicht formatiert werden/).waitFor();
    const width = await page.evaluate(() => document.documentElement.scrollWidth);
    assert.ok(width <= 390, "no horizontal scrolling on a phone: " + width);
    await page.screenshot({ path: `${out}/phone-refused.png` });
    await ctx.close();
  }
  {
    // The partition editor: bar, table, an action per partition and gap,
    // plain confirmations that name drive, partition and data loss.
    const { ctx, page, data } = await create("owner");
    const MiB = 2 ** 20, GiB = 2 ** 30;
    const slot = (number, start, size, fsType, label, growBy) => ({ number, device: "/dev/sdb" + number, start, size, type: "EBD0A0A2-B9E5-4433-87C0-68B6B72699C7", fsType, label, uuid: "u" + number, mounted: false, growBy, renamable: true });
    data.usbDisks = disks();
    data.usbLayouts = {
      stick: {
        disk: data.usbDisks[1], table: "gpt", sectorSize: 512,
        partitions: [slot(1, MiB, 8 * GiB, "exfat", "FOTOS", 0), slot(2, 16 * GiB + MiB, 4 * GiB, "ext4", "Archiv", 9 * GiB)],
        gaps: [{ start: 8 * GiB + MiB, size: 8 * GiB, after: 1 }, { start: 20 * GiB + MiB, size: 9 * GiB, after: 2 }],
      },
    };
    const finish = async (dialog) => {
      data.usbJob = { ...data.usbJob, phase: "done", finished: "2026-10-23T16:01:00Z" };
      await dialog.getByText("Fertig.").waitFor({ timeout: 5000 });
    };
    await page.goto(base + "/files?root=share/Media&path=");
    const list = await openDialog(page);
    await list.locator(".drive-row", { hasText: "Ultra Fit" }).getByRole("button", { name: "Partitionen …" }).click();
    const dialog = page.getByRole("dialog", { name: "Partitionen: Ultra Fit" });
    await dialog.getByRole("cell", { name: "Archiv" }).waitFor();
    assert.equal(await dialog.getByRole("group", { name: "Aufteilung der Platte" }).getByRole("button").count(), 4);
    assert.equal(await dialog.getByRole("row", { name: /Freier Bereich/ }).count(), 2);
    // exFAT cannot grow; ext4 with free space after it can.
    assert.equal(await dialog.getByRole("row", { name: /FOTOS/ }).getByRole("button", { name: "Vergrößern …" }).count(), 0);
    await page.screenshot({ path: `${out}/partitions.png` });

    // Create in the first gap, with a size and ext4.
    await dialog.getByRole("row", { name: /Freier Bereich/ }).first().getByRole("button", { name: "Partition anlegen …" }).click();
    const create1 = page.getByRole("dialog", { name: "Partition anlegen" });
    await create1.getByLabel("Den ganzen freien Bereich nutzen").uncheck();
    await create1.getByLabel("Größe in MiB").fill("999999");
    await create1.getByText("So viel Platz ist dort nicht frei.").waitFor();
    assert.equal(await create1.getByRole("button", { name: "Anlegen" }).isDisabled(), true);
    await create1.getByLabel("Größe in MiB").fill("2048");
    await create1.getByLabel("Dateisystem").selectOption("ext4");
    await create1.getByLabel("Name", { exact: true }).fill("Daten");
    await create1.getByRole("button", { name: "Anlegen" }).click();
    await create1.waitFor({ state: "detached" });
    assert.deepEqual(data.partitionActions, [{ id: "stick", action: "create", start: 8 * GiB + MiB, size: 2048 * MiB, filesystem: "ext4", label: "Daten" }]);
    await dialog.getByText("Partition anlegen").first().waitFor();
    await finish(dialog);

    // Grow the ext4 partition as far as possible.
    await dialog.getByRole("row", { name: /Archiv/ }).getByRole("button", { name: "Vergrößern …" }).click();
    const grow = page.getByRole("dialog", { name: "Partition vergrößern" });
    assert.match(await grow.textContent(), /sdb2 .*Ultra Fit \(\/dev\/sdb\) wächst von/);
    await grow.getByRole("button", { name: "Vergrößern" }).click();
    await grow.waitFor({ state: "detached" });
    assert.deepEqual(data.partitionActions[1], { id: "stick", action: "grow", number: 2, size: 0 });
    await finish(dialog);

    // Delete: a plain confirmation, no typing; nothing is sent before it.
    await dialog.getByRole("row", { name: /Archiv/ }).getByRole("button", { name: "Löschen …" }).click();
    const del = page.getByRole("dialog", { name: "Partition löschen" });
    await del.getByRole("button", { name: "Weiter …" }).click();
    assert.match(await del.textContent(), /Partition sdb2 .* auf Ultra Fit \(\/dev\/sdb\) wird gelöscht\. Alle Daten darauf sind danach verloren/);
    assert.equal(data.partitionActions.length, 2);
    await page.screenshot({ path: `${out}/partitions-delete.png` });
    await del.getByRole("button", { name: "Jetzt löschen" }).click();
    await del.waitFor({ state: "detached" });
    assert.deepEqual(data.partitionActions[2], { id: "stick", action: "delete", number: 2 });
    await finish(dialog);

    // A new table names the whole drive.
    await dialog.getByRole("button", { name: "Neue Partitionstabelle …" }).click();
    const table = page.getByRole("dialog", { name: "Neue Partitionstabelle" });
    await table.getByRole("button", { name: "Weiter …" }).click();
    assert.match(await table.textContent(), /Auf Ultra Fit \(.*\/dev\/sdb\) wird eine neue, leere GPT-Partitionstabelle angelegt\. Alle Partitionen und alle Daten/);
    await table.getByRole("button", { name: "Abbrechen" }).click();
    await table.getByRole("button", { name: "Abbrechen" }).click();
    await table.waitFor({ state: "detached" });
    assert.equal(data.partitionActions.length, 3);

    // On a phone the table scrolls inside the dialog, not the page.
    await page.setViewportSize({ width: 390, height: 844 });
    const width = await page.evaluate(() => document.documentElement.scrollWidth);
    assert.ok(width <= 390, "no horizontal scrolling on a phone: " + width);
    await page.screenshot({ path: `${out}/partitions-phone.png` });
    await dialog.getByRole("button", { name: "Zurück zur Laufwerksliste" }).click();
    await page.getByRole("dialog", { name: "Laufwerk formatieren" }).waitFor();
    await ctx.close();
  }
  {
    // Accounts with the role "user" do not see the button.
    const { ctx, page } = await create("user");
    await page.goto(base + "/files?root=share/Media&path=");
    await page.waitForTimeout(500);
    assert.equal(await page.getByRole("button", { name: "Laufwerk formatieren …" }).count(), 0);
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("drive format UI ok");
} finally {
  await browser.close();
}
