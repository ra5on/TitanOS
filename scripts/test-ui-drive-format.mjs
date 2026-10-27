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
