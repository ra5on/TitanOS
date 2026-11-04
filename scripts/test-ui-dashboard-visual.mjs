// Browser interactions against isolated API data; no NAS or user files are changed.
import assert from "node:assert/strict";
import { mkdir, writeFile, readFile } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const out = process.env.UI_OUTPUT || "out/validation/dashboard-visual";
await mkdir(out, { recursive: true });
const errors = [];
const { ctx, page, data } = await fixtureFactory(browser, errors)(
  "owner",
  "graphit",
  1600,
);
const base = process.env.UI_URL || "http://127.0.0.1:5187";
const writes = [],
  images = new Map();
await ctx.route("**/api/v1/preferences/dashboard", async (r) => {
  if (r.request().method() !== "PUT") return r.fallback();
  writes.push(r.request());
  if (data.saveDelay)
    await new Promise((resolve) => setTimeout(resolve, data.saveDelay));
  if (data.failSave)
    return r.fulfill({
      status: 500,
      json: {
        error: { code: "config_write_failed", message: "isolated failure" },
      },
    });
  if (
    r.request().headers()["content-type"]?.startsWith("multipart/form-data")
  ) {
    const form = await new Response(r.request().postDataBuffer(), {
      headers: { "Content-Type": r.request().headers()["content-type"] },
    }).formData();
    data.prefs.dashboard = JSON.parse(form.get("preferences"));
    for (const file of form.getAll("image"))
      images.set(file.name, {
        bytes: Buffer.from(await file.arrayBuffer()),
        type: file.type,
      });
  } else data.prefs.dashboard = r.request().postDataJSON();
  await r.fulfill({ status: 204 });
});
await ctx.route("**/api/v1/background/*", (r) => {
  const id = new URL(r.request().url()).pathname.split("/").at(-1),
    image = images.get(id);
  return image
    ? r.fulfill({ body: image.bytes, contentType: image.type })
    : r.fulfill({ status: 404, json: { error: { code: "not_found" } } });
});
data.prefs.appearance = {
  ...data.prefs.appearance,
  bg: "image",
  bgVersion: 1,
  dim: 30,
};
await ctx.route("**/api/v1/background?*", async (r) =>
  r.fulfill({
    body: await readFile("web/static/backgrounds/bergsee.webp"),
    contentType: "image/webp",
  }),
);
// Since 27 Oct 2026 the dashboard has no design editor of its own: the
// background gallery and the metric transitions live in Settings ›
// Appearance, saved with its one "Übernehmen" bar.
const appearance = async () => {
  await page.goto(base + "/settings/appearance");
  await page
    .getByRole("heading", { name: "Hintergrund des Dashboards", exact: true })
    .waitFor();
};
const apply = async () => {
  await page.getByRole("button", { name: "Übernehmen", exact: true }).click();
};
const gallery = async () => {
  await page
    .getByRole("button", {
      name: "Bild auswählen oder hinzufügen …",
      exact: true,
    })
    .click();
  await page
    .getByRole("dialog", { name: "Hintergrund auswählen", exact: true })
    .waitFor();
};
const shot = async (name) => {
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() =>
    Promise.all([...document.images].map((i) => i.decode().catch(() => {}))),
  );
  await page.screenshot({ path: out + "/" + name + ".png", fullPage: true });
};
try {
  await page.goto(base + "/");
  await page.locator("[gs-id=cpu]").waitFor();
  assert.equal(
    await page
      .locator(".backdrop.dashboard .dim")
      .evaluate((e) => getComputedStyle(e).opacity),
    "0.15",
    "inherited image must use the dashboard dimming setting",
  );
  await appearance();
  await gallery();
  assert.equal(await page.locator(".wallpaper-choice").count(), 10);
  await page
    .getByRole("button", { name: "Hintergrund Nordlicht", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Hintergrund verwenden", exact: true })
    .click();
  await shot("editor");
  assert.equal(writes.length, 0, "preview wrote preferences");
  await page.getByText("Nicht gespeichert", { exact: true }).waitFor();
  await apply();
  await page.getByText("Gespeichert", { exact: true }).waitFor();
  assert.equal(data.prefs.dashboard.visual.background.id, "nordlicht");
  await page.goto(base + "/");
  await page.locator(".dashboard-background").waitFor();
  assert.equal(
    await page
      .locator(".dashboard-background .picture")
      .evaluate((e) => getComputedStyle(e).backgroundSize),
    "contain",
  );
  assert.match(
    await page.locator(".dashboard-background .picture").getAttribute("style"),
    /nordlicht/,
  );
  await appearance();
  await gallery();
  await shot("backgrounds");
  await page
    .getByRole("button", { name: "Eigenes Bild hinzufügen", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "Hintergrund auswählen", exact: true })
    .locator("input[type=file]")
    .setInputFiles("web/static/backgrounds/bergsee.webp");
  await page
    .getByRole("button", { name: "Hintergrund verwenden", exact: true })
    .waitFor();
  await page.locator(".personal-image").waitFor();
  await shot("upload");
  const before = writes.length;
  await page
    .getByRole("dialog", { name: "Hintergrund auswählen", exact: true })
    .getByRole("button", { name: "Abbrechen", exact: true })
    .click();
  assert.equal(writes.length, before);
  assert.equal(
    await page.getByRole("button", { name: "Übernehmen", exact: true }).count(),
    0,
    "cancelling the gallery leaves nothing unsaved",
  );
  await gallery();
  await page
    .getByRole("button", { name: "Eigenes Bild hinzufügen", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "Hintergrund auswählen", exact: true })
    .locator("input[type=file]")
    .setInputFiles("web/static/backgrounds/bergsee.webp");
  await page.locator(".personal-image").waitFor();
  await page
    .getByRole("button", { name: "Hintergrund verwenden", exact: true })
    .click();
  // A failed save keeps the stored background and the staged image.
  data.failSave = true;
  data.saveDelay = 500;
  await apply();
  await page.getByRole("alert").waitFor();
  assert.match(
    await page.getByRole("alert").innerText(),
    /^Hintergrund des Dashboards: /,
  );
  assert.equal(data.prefs.dashboard.visual.background.id, "nordlicht");
  data.failSave = false;
  data.saveDelay = 0;
  await apply();
  await page.getByText("Gespeichert", { exact: true }).waitFor();
  assert.equal(images.size, 1);
  await page.goto(base + "/");
  await page.locator(".dashboard-background").waitFor();
  assert.match(
    await page.locator(".dashboard-background .picture").getAttribute("style"),
    /api\/v1\/background\/[a-f0-9]{64}/,
  );
  // Smooth metric transitions: off, then on again, and never with reduced motion.
  await appearance();
  const smooth = page.getByRole("checkbox", {
    name: "Sanfte Messwertübergänge",
    exact: true,
  });
  await smooth.uncheck();
  await apply();
  await page.getByText("Gespeichert", { exact: true }).waitFor();
  await page.goto(base + "/");
  assert.equal(
    await page
      .locator("[gs-id=cpu] .metric-level")
      .first()
      .evaluate((e) => getComputedStyle(e).transitionDuration),
    "0s",
  );
  await appearance();
  await smooth.check();
  await apply();
  await page.getByText("Gespeichert", { exact: true }).waitFor();
  await page.goto(base + "/");
  await page.locator("[gs-id=cpu] .metric-level").first().waitFor();
  await page.emulateMedia({ reducedMotion: "reduce" });
  assert.equal(
    await page
      .locator("[gs-id=cpu] .metric-level")
      .first()
      .evaluate((e) => getComputedStyle(e).transitionDuration),
    "0s",
  );
  await page.emulateMedia({ reducedMotion: "no-preference" });
  const cpu = page.locator("[gs-id=cpu] .metric-level").first();
  const initial = await cpu.evaluate((e) => e.getBoundingClientRect().width);
  data.system.cpu.usagePercent = 82;
  const frames = await page.evaluate(async () => {
    const el = document.querySelector("[gs-id=cpu] .metric-level");
    const widths = [];
    const start = performance.now();
    while (performance.now() - start < 2700) {
      widths.push(Math.round(el.getBoundingClientRect().width * 10));
      await new Promise(requestAnimationFrame);
    }
    return [...new Set(widths)];
  });
  assert.ok(frames.length > 5, "CPU has no interpolated frames");
  await page.setViewportSize({ width: 390, height: 844 });
  await appearance();
  await gallery();
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  );
  await shot("mobile");
  assert.deepEqual(errors, []);
  await writeFile(
    out + "/checks.json",
    JSON.stringify(
      {
        backgrounds: 10,
        noWritesDuringPreview: true,
        uploadAndReload: true,
        failedSavePreservesSelection: true,
        reducedMotion: true,
        cpuFrames: frames.length,
        initialWidth: initial,
        mobile: true,
        browserErrors: errors,
      },
      null,
      2,
    ) + "\n",
  );
  console.log(
    "Dashboard appearance, uploads, persistence, animation and mobile checks passed.",
  );
} catch (e) {
  await shot("error");
  console.error(errors);
  throw e;
} finally {
  await browser.close();
}
