// PLAN 70: TitanOS as a phone app – the manifest offers shortcuts and a share
// target; /share uploads what the service worker kept (here put into the
// cache by the test, as the worker would) to the chosen folder through the
// file manager's own upload; Mein Konto shows the "TitanOS als App" card.
// Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5240 node scripts/test-ui-pwa.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5240";
const out = process.env.UI_OUTPUT || "out/validation/pwa";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

async function phone() {
  const made = await create("owner", "graphit", 390, "de", {
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 2,
    viewport: { width: 390, height: 844 },
  });
  const { ctx } = made;
  made.created = [];
  made.bytes = 0;
  await ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    // Not part of this test (the shared fixtures have no personal channels).
    if (p === "/account/notifications")
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found" } },
      });
    if (p === "/files/roots")
      return route.fulfill({
        json: [
          {
            id: "share/Fotos",
            name: "Fotos",
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
        json: url.searchParams.get("path")
          ? []
          : [
              {
                name: "Handy",
                dir: true,
                size: 0,
                modified: "2026-11-05T10:00:00Z",
              },
            ],
      });
    if (p === "/files/uploads" && method === "POST") {
      const body = route.request().postDataJSON();
      made.created.push(body);
      return route.fulfill({
        json: {
          id: "u" + made.created.length,
          offset: 0,
          size: body.size,
          name: body.name,
        },
      });
    }
    const m = /^\/files\/uploads\/(u\d+)(\/finish)?$/.exec(p);
    if (m && method === "PATCH") {
      const n = route.request().postDataBuffer()?.length ?? 0;
      made.bytes += n;
      const off = Number(url.searchParams.get("offset")) + n;
      return route.fulfill({
        json: { id: m[1], offset: off, size: off, name: "x" },
      });
    }
    if (m && m[2] && method === "POST") {
      const c = made.created[Number(m[1].slice(1)) - 1];
      return route.fulfill({
        json: { id: m[1], offset: c.size, size: c.size, name: c.name },
      });
    }
    return route.fallback();
  });
  return made;
}

// The manifest: share target and shortcuts.
{
  const res = await fetch(base + "/manifest.webmanifest");
  const m = await res.json();
  assert.equal(m.share_target.action, "/share-target");
  assert.equal(m.share_target.method, "POST");
  assert.equal(m.share_target.enctype, "multipart/form-data");
  assert.equal(m.share_target.params.files[0].name, "files");
  assert.deepEqual(
    m.shortcuts.map((s) => s.url),
    ["/files", "/docker", "/settings"],
  );
}

// /share with two files and a text, as the service worker leaves them.
{
  const made = await phone();
  const { ctx, page } = made;
  await page.goto(base + "/");
  await page.evaluate(async () => {
    const cache = await caches.open("titanos-share");
    await cache.put(
      "/share-target/file/0",
      new Response(new Blob(["A".repeat(1000)], { type: "image/jpeg" })),
    );
    await cache.put(
      "/share-target/file/1",
      new Response(new Blob(["%PDF-1.4 test"], { type: "application/pdf" })),
    );
    await cache.put(
      "/share-target/list",
      new Response(
        JSON.stringify({
          files: [
            {
              key: "/share-target/file/0",
              name: "IMG_2041.jpg",
              size: 1000,
              type: "image/jpeg",
            },
            {
              key: "/share-target/file/1",
              name: "Rechnung.pdf",
              size: 13,
              type: "application/pdf",
            },
          ],
          text: "https://example.org/rezept",
        }),
      ),
    );
  });
  await page.goto(base + "/share");
  await page.getByText("IMG_2041.jpg").waitFor({ timeout: 15000 });
  await page.getByText("Rechnung.pdf").waitFor();
  await page.getByText("https://example.org/rezept").waitFor();
  const wide = await page.evaluate(() => document.documentElement.scrollWidth);
  assert.ok(wide <= 390, `no sideways scrolling (${wide})`);
  assert.equal(
    await page.getByRole("button", { name: "Hochladen" }).isDisabled(),
    true,
    "upload waits for a folder",
  );
  await page.getByRole("button", { name: "Ordner wählen …" }).click();
  const dialog = page.getByRole("dialog", { name: "Ablegen in" });
  await dialog.getByRole("button", { name: "Handy" }).click();
  await dialog.getByRole("button", { name: "Hier ablegen" }).click();
  // Style W4 (0.74): the chosen folder shows in its row and, as before a
  // tile would, in the status bar at the end of the page.
  await page
    .locator(".set-row .set-label")
    .getByText("Fotos / Handy", { exact: true })
    .waitFor();
  await page
    .locator("footer.statusbar .end")
    .getByText("Fotos / Handy", { exact: true })
    .waitFor();
  await page.screenshot({ path: `${out}/teilen-handy.png` });
  await page.getByRole("button", { name: "Hochladen" }).click();
  await page
    .getByRole("link", { name: "In Dateien zeigen" })
    .waitFor({ timeout: 15000 });
  assert.deepEqual(
    made.created.map((c) => [
      c.root,
      c.dir,
      c.name.replace(/\d{4}-.*\.txt$/, "<zeit>.txt"),
      c.conflict,
    ]),
    [
      ["share/Fotos", "Handy", "IMG_2041.jpg", "rename"],
      ["share/Fotos", "Handy", "Rechnung.pdf", "rename"],
      ["share/Fotos", "Handy", "geteilt-<zeit>.txt", "rename"],
    ],
  );
  assert.equal(made.bytes, 1000 + 13 + "https://example.org/rezept\n".length);
  // Done: the files left this browser.
  await page.waitForTimeout(300);
  assert.equal(await page.evaluate(() => caches.has("titanos-share")), false);
  await page.screenshot({ path: `${out}/teilen-fertig.png` });
  // Nothing shared: a plain hint.
  await page.goto(base + "/share?missing=1");
  await page
    .getByText("Die geteilten Dateien sind nicht angekommen.", { exact: false })
    .waitFor();
  // Mein Konto: the app card.
  await page.goto(base + "/account");
  const card = page.locator('[data-testid="app-install"]');
  await card.waitFor({ timeout: 15000 });
  await card.getByText("Im Browser-Menü", { exact: false }).waitFor();
  await card.screenshot({ path: `${out}/app-karte.png` });
  await ctx.close();
}

await browser.close();
assert.deepEqual(errors, [], "no script errors");
console.log("pwa UI ok");
