// PLAN 70: quick look in the file manager – Space opens any file and closes
// it again, the arrows go through the folder's files, Office files show the
// picture they carry, other files an info card; on a phone too.
// Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5240 node scripts/test-ui-quicklook.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { deflateSync } from "node:zlib";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5240";
const out = process.env.UI_OUTPUT || "out/validation/quicklook";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const MODIFIED = "2026-11-05T10:00:00Z";

// A small solid PNG (w×h, one colour).
function png(w, h, [r, g, b]) {
  const crc = (buf) => {
    let c = ~0;
    for (const x of buf) {
      c ^= x;
      for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1));
    }
    return ~c >>> 0;
  };
  const chunk = (type, data) => {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const td = Buffer.concat([Buffer.from(type), data]);
    const c = Buffer.alloc(4);
    c.writeUInt32BE(crc(td));
    return Buffer.concat([len, td, c]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  const row = Buffer.concat([Buffer.from([0]), Buffer.alloc(w * 3)]);
  for (let x = 0; x < w; x++) row.set([r, g, b], 1 + x * 3);
  const raw = Buffer.concat(Array.from({ length: h }, () => row));
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

const files = {
  "Angebot.docx": Buffer.from("PK office"),
  "Foto.png": png(80, 60, [30, 120, 220]),
  "Notizen.txt": Buffer.from("Einkaufen\nMilch\nBrot\n"),
  "archiv.zip": Buffer.from("PK zip"),
};

async function open(width, height, phone = false) {
  const made = await create(
    "owner",
    "graphit",
    width,
    "de",
    phone ? { isMobile: true, hasTouch: true, deviceScaleFactor: 2 } : {},
  );
  const { ctx, page } = made;
  await page.setViewportSize({ width, height });
  await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  await ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const q = url.searchParams;
    if (p === "/files/roots")
      return route.fulfill({
        json: [
          {
            id: "share/Documents",
            name: "Documents",
            kind: "share",
            writable: true,
            recycle: true,
            totalBytes: 1e12,
            freeBytes: 6e11,
          },
        ],
      });
    if (p === "/files/list")
      return route.fulfill({
        json: q.get("path")
          ? []
          : Object.entries(files).map(([name, b]) => ({
              name,
              dir: false,
              size: b.length,
              modified: MODIFIED,
            })),
      });
    if (p === "/files/download") {
      const b = files[q.get("path")];
      return b
        ? route.fulfill({
            body: b,
            headers: { "content-length": String(b.length) },
          })
        : route.fulfill({
            status: 404,
            json: { error: { code: "file_not_found" } },
          });
    }
    if (p === "/files/thumb") {
      // The picture an Office document carries (titand reads it, test data here).
      if (q.get("path") === "Angebot.docx")
        return route.fulfill({
          contentType: "image/png",
          body: png(120, 160, [250, 250, 250]),
        });
      if (q.get("path") === "Foto.png")
        return route.fulfill({
          contentType: "image/png",
          body: files["Foto.png"],
        });
      return route.fulfill({
        status: 415,
        json: { error: { code: "thumb_unsupported" } },
      });
    }
    return route.fallback();
  });
  await page.goto(base + "/files?root=share/Documents");
  return made;
}

// Desktop: the file manager in its window.
{
  const { ctx, page } = await open(1440, 900);
  const frame = page.frameLocator("iframe[data-window]").first();
  const row = (name) => frame.locator(`[data-file-name="${name}"]`).first();
  await row("Angebot.docx").waitFor({ timeout: 15000 });
  await row("Angebot.docx").click();
  await page.keyboard.press(" ");
  const dialog = frame.getByRole("dialog", { name: /Angebot\.docx/ });
  await dialog.waitFor();
  await dialog.locator("img.ql-page").waitFor();
  await dialog
    .getByText("Vorschaubild, das im Dokument gespeichert ist")
    .waitFor();
  await page.locator("section.win").screenshot({ path: `${out}/office.png` });
  // Arrow: the next file of the folder (sorted by name) – the picture.
  await page.keyboard.press("ArrowRight");
  await frame.getByRole("dialog", { name: /archiv\.zip/ }).waitFor();
  await frame.getByText("Für diese Datei gibt es keine Vorschau.").waitFor();
  await page.keyboard.press("ArrowRight");
  const photo = frame.getByRole("dialog", { name: /Foto\.png/ });
  await photo.waitFor();
  await photo.locator("img").first().waitFor();
  await page.locator("section.win").screenshot({ path: `${out}/bild.png` });
  await page.keyboard.press("ArrowRight");
  const text = frame.getByRole("dialog", { name: /Notizen\.txt/ });
  await text.getByText("Milch").waitFor();
  // The selection follows the quick look.
  assert.equal(
    (await frame
      .locator(
        '[data-file-name="Notizen.txt"][aria-selected="true"], [data-file-name="Notizen.txt"].selected',
      )
      .count()) > 0 ||
      (await frame
        .locator('[data-file-name="Notizen.txt"] input[type="checkbox"]')
        .first()
        .isChecked()
        .catch(() => false)),
    true,
    "the shown file is selected",
  );
  // ArrowLeft back, Space closes.
  await page.keyboard.press("ArrowLeft");
  await photo.waitFor();
  await page.keyboard.press(" ");
  await photo.waitFor({ state: "detached" });
  await ctx.close();
}

// Phone: the preview through the file's menu, with the same head.
{
  const { ctx, page } = await open(390, 844, true);
  const row = page.locator('[data-file-name="Angebot.docx"]').first();
  await row.waitFor({ timeout: 15000 });
  await row.click({ button: "right" }).catch(() => {});
  const item = page.getByRole("menuitem", { name: "Vorschau" });
  if (await item.count()) await item.first().click();
  else {
    await page.keyboard.press("Escape");
    await row.focus();
    await page.keyboard.press(" ");
  }
  const dialog = page.getByRole("dialog", { name: /Angebot\.docx/ });
  await dialog.waitFor();
  await page.waitForTimeout(600);
  const wide = await page.evaluate(() => document.documentElement.scrollWidth);
  assert.ok(wide <= 390, `no sideways scrolling (${wide})`);
  await page.screenshot({ path: `${out}/handy.png` });
  await ctx.close();
}

await browser.close();
assert.deepEqual(errors, [], "no script errors");
console.log("quick look UI ok");
