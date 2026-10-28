// Isolated API data only. No copying to a real NAS or host path.
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
const base = process.env.UI_URL || "http://127.0.0.1:5194";
const out = process.env.UI_OUTPUT || "out/validation/ui-copy-confirmation";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);
try {
  const { ctx, page, data } = await create();
	data.system.version = "0.31.0";
  const pending = [],
    copies = [];
  const file = {
    name: "report.txt",
    dir: false,
    size: 12,
    modified: "2026-10-25T00:00:00Z",
  };
  const review = {
    files: 1,
    bytes: 12,
    truncated: false,
    conflicts: [
      {
        path: file.name,
        sourcePath: file.name,
        sourceVersion: "source-v1",
        targetVersion: "target-v1",
        source: file,
        target: { ...file, size: 20 },
      },
    ],
  };
  await ctx.route("**/api/v1/files/**", async (route) => {
    const url = new URL(route.request().url()),
      path = url.pathname;
    if (path.endsWith("/list"))
      return route.fulfill({
        json: url.searchParams.get("path") ? [] : [file],
      });
    if (path.endsWith("/inspect")) {
      const request = route.request().postDataJSON();
      if (!request.toRoot)
        return route.fulfill({
          json: { files: 1, bytes: 12, conflicts: [], truncated: false },
        });
      await new Promise((resolve) =>
        pending.push({
          request,
          respond: async (json = review, status = 200) => {
            try {
              await route.fulfill({ status, json });
            } catch {
            } finally {
              resolve();
            }
          },
        }),
      );
      return;
    }
    if (path.endsWith("/copy")) {
      copies.push(route.request().postDataJSON());
      return route.fulfill({ json: { id: "fixture-copy" } });
    }
    return route.fallback();
  });
  async function next() {
    for (let i = 0; i < 200; i++) {
      if (pending.length) return pending.shift();
      await new Promise((r) => setTimeout(r, 25));
    }
    throw Error("no automatic conflict inspection");
  }
  await page.goto(base + "/files?root=share%2FDocuments");
  await page
    .getByRole("checkbox", { name: "report.txt auswählen", exact: true })
    .check();
  await page
    .getByRole("button", { name: "Kopieren nach …", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Gibt es den Namen schon", { exact: true })
    .selectOption("replace");
  const checkbox = dialog.getByRole("checkbox", {
    name: "Ich bestätige das Ersetzen vorhandener Dateien.",
    exact: true,
  });
  const copy = dialog.getByRole("button", {
    name: "Hierher kopieren",
    exact: true,
  });
  const initial = await next();
  assert.equal(await checkbox.isDisabled(), true);
  assert.equal(await copy.isDisabled(), true);
  assert.equal(copies.length, 0);
  await initial.respond();
  await checkbox.check();
  assert.equal(await copy.isEnabled(), true);
  assert.equal(
    copies.length,
    0,
    "confirmation must work without an initial Copy click",
  );
  await dialog.getByRole("combobox").first().selectOption("share/Media");
  const stale = await next();
  assert.equal(await checkbox.isChecked(), false);
  await dialog.getByRole("combobox").first().selectOption("share/Projects");
  const current = await next();
  await current.respond();
  await checkbox.check();
  await stale.respond({ ...review, truncated: true });
  assert.equal(
    await checkbox.isChecked(),
    true,
    "stale destination reply reset confirmation",
  );
  assert.equal(await copy.isEnabled(), true);
  await dialog.getByRole("combobox").first().selectOption("share/Media");
  const failed = await next();
  await failed.respond(
    { error: { code: "internal", message: "fixture error" } },
    500,
  );
  await dialog.getByRole("alert").waitFor();
  assert.equal(await checkbox.isDisabled(), true);
  assert.equal(await copy.isDisabled(), true);
  await dialog
    .getByRole("button", { name: "Erneut versuchen", exact: true })
    .click();
  await (await next()).respond({ ...review, truncated: true });
  assert.equal(
    await checkbox.isDisabled(),
    true,
    "truncated comparison allowed replacement",
  );
  await dialog.getByRole("combobox").first().selectOption("share/Projects");
  await (await next()).respond();
  await checkbox.check();
  await page.screenshot({
    path: out + "/replace-before-copy.png",
    fullPage: true,
  });
  await copy.click();
  await dialog.waitFor({ state: "hidden" });
  assert.equal(copies.length, 1);
  assert.equal(copies[0].toRoot, "share/Projects");
  assert.equal(copies[0].conflict, "replace");
  assert.deepEqual(errors, []);
  await ctx.close();
  console.log(
    "Replace confirmation before Copy, asynchronous preview, destination reset, stale replies, inspection failure/retry and truncated comparison passed",
  );
} finally {
  await browser.close();
}
