// Share quotas: limit in the share dialog, usage in the details, the switch
// on the volume card. Isolated API data only: no request reaches a NAS.
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
const out = process.env.UI_OUTPUT || "out/validation/ui-share-quotas";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

try {
  {
    const { ctx, page, data } = await create("owner");
    const documents = data.shares.find((s) => s.name === "Documents");
    Object.assign(documents, {
      quota: 50e9,
      quotaID: 10000,
      quotaStatus: { state: "active", used: 47e9, limit: 50e9 },
    });
    await page.goto(base + "/shares");
    await page.getByRole("button", { name: "Documents", exact: true }).click();
    const details = page.locator("dl").filter({ hasText: "Obergrenze" });
    await details.getByText(/47(,0)? GB von 50(,0)? GB belegt/).waitFor();
    assert.equal(await page.locator(".quota-bar.full").count(), 1);
    await page.screenshot({ path: `${out}/share-details.png`, fullPage: true });

    // The dialog shows the limit in GB and sends bytes.
    await page.getByRole("button", { name: "Bearbeiten", exact: true }).first().click();
    const dialog = page.getByRole("dialog");
    const limit = dialog.getByLabel("Obergrenze (GB, 0 = keine)");
    assert.equal(await limit.inputValue(), "50");
    assert.match(await dialog.textContent(), /Windows sieht sie als Laufwerksgröße/);
    await limit.fill("0.001");
    await dialog.getByText("mindestens 16 MiB").waitFor();
    assert.equal(await dialog.getByRole("button", { name: "Speichern", exact: true }).isDisabled(), true);
    await limit.fill("120");
    await dialog.getByRole("button", { name: "Speichern", exact: true }).click();
    await dialog.waitFor({ state: "detached" });
    assert.equal(data.shareSaves.at(-1).quota, 120e9);
    assert.equal(data.shareSaves.at(-1).quotaID, undefined);

    // Btrfs shares get no limit field, only the reason.
    await page.getByRole("button", { name: "Projects", exact: true }).click();
    await page.getByRole("button", { name: "Bearbeiten", exact: true }).first().click();
    await page.getByRole("dialog").getByText("nur auf ext4- und XFS-Volumes").waitFor();
    assert.equal(await page.getByRole("dialog").getByLabel("Obergrenze (GB, 0 = keine)").count(), 0);
    await page.keyboard.press("Escape");

    // Volume card: off → on is pending until the next start; Btrfs has no switch.
    await page.goto(base + "/storage");
    await page.getByRole("button", { name: "Safe", exact: true }).click();
    const card = page.locator("article");
    await card.getByText("Freigabe-Kontingente").waitFor();
    await card.getByRole("button", { name: "Einschalten", exact: true }).click();
    await card.getByText("gelten nach dem nächsten Neustart").waitFor();
    assert.equal(data.volumeQuotas.Safe, "pending");
    await page.screenshot({ path: `${out}/storage.png`, fullPage: true });
    await page.getByRole("button", { name: "Fast", exact: true }).click();
    await card.getByText("Btrfs").first().waitFor();
    assert.equal(await card.getByText("Freigabe-Kontingente").count(), 0);

    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto(base + "/shares");
    await page.getByRole("button", { name: "Documents", exact: true }).click();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
    assert.ok(overflow <= 1, `horizontal scroll ${overflow}`);
    await page.screenshot({ path: `${out}/share-details-mobile.png`, fullPage: true });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("share quota UI checks passed");
} finally {
  await browser.close();
}
