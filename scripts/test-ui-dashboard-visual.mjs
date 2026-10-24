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
const edit = async () => {
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .click();
  await page
    .getByRole("complementary", { name: "Gesamtes Dashboard", exact: true })
    .waitFor();
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
  await edit();
  assert.equal(
    await page
      .locator(".backdrop.dashboard .dim")
      .evaluate((e) => getComputedStyle(e).opacity),
    "0.15",
    "inherited image must use the dashboard dimming setting",
  );
  const dimming = page.getByRole("slider", { name: "Abdunklung", exact: true });
  await dimming.focus();
  await dimming.press("ArrowRight");
  assert.equal(
    await page
      .locator(".backdrop.dashboard .dim")
      .evaluate((e) => getComputedStyle(e).opacity),
    "0.16",
  );
  await page.getByRole("button", { name: "Rückgängig", exact: true }).click();
  const slider = page.getByRole("slider", {
    name: "Globale Transparenz",
    exact: true,
  });
  await slider.focus();
  await slider.press("ArrowRight");
  assert.equal(await slider.inputValue(), "49");
  await page.getByRole("button", { name: "Rückgängig", exact: true }).click();
  assert.equal(await slider.inputValue(), "48");
  await page.getByRole("button", { name: "Wiederholen", exact: true }).click();
  assert.equal(await slider.inputValue(), "49");
  const opaque = await page
    .locator("[gs-id=cpu] .dashboard-tile")
    .evaluate((e) => getComputedStyle(e).backgroundColor);
  assert.match(
    opaque,
    /0\.5[01]/,
    "global alpha must be applied to tile surfaces",
  );
  await slider.dispatchEvent("pointerdown");
  await slider.fill("60");
  const transparent = await page
    .locator("[gs-id=cpu] .dashboard-tile")
    .evaluate((e) => getComputedStyle(e).backgroundColor);
  assert.match(transparent, /0\.4/);
  await page.getByRole("button", { name: "Rückgängig", exact: true }).click();
  assert.equal(await slider.inputValue(), "49");
  await gallery();
  assert.equal(await page.locator(".wallpaper-choice").count(), 10);
  await page
    .getByRole("button", { name: "Hintergrund Nordlicht", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Hintergrund verwenden", exact: true })
    .click();
  await shot("editor");
  assert.equal(
    await page
      .locator(".dashboard-background .picture")
      .evaluate((e) => getComputedStyle(e).backgroundSize),
    "contain",
  );
  assert.equal(writes.length, 0, "preview wrote preferences");
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  assert.equal(data.prefs.dashboard.visual.background.id, "nordlicht");
  await page.reload();
  await page.locator(".dashboard-background").waitFor();
  await edit();
  await gallery();
  await shot("backgrounds");
  await page
    .getByRole("button", { name: "Eigenes Bild hinzufügen", exact: true })
    .click();
  await page
    .locator("input[type=file]")
    .setInputFiles("web/static/backgrounds/bergsee.webp");
  await page
    .getByRole("button", { name: "Hintergrund verwenden", exact: true })
    .waitFor();
  await page.locator(".personal-image").waitFor();
  await shot("upload");
  const before = writes.length;
  await page
    .getByRole("button", { name: "Abbrechen", exact: true })
    .last()
    .click();
  assert.equal(writes.length, before);
  assert.match(
    await page.locator(".dashboard-background .picture").getAttribute("style"),
    /nordlicht/,
  );
  await gallery();
  await page
    .getByRole("button", { name: "Eigenes Bild hinzufügen", exact: true })
    .click();
  await page
    .locator("input[type=file]")
    .setInputFiles("web/static/backgrounds/bergsee.webp");
  await page.locator(".personal-image").waitFor();
  await page
    .getByRole("button", { name: "Hintergrund verwenden", exact: true })
    .click();
  await page.getByRole("button", { name: "Rückgängig", exact: true }).click();
  assert.match(
    await page.locator(".dashboard-background .picture").getAttribute("style"),
    /nordlicht/,
  );
  await page.getByRole("button", { name: "Wiederholen", exact: true }).click();
  assert.match(
    await page.locator(".dashboard-background .picture").getAttribute("style"),
    /blob:/,
    "redo must retain the staged image",
  );
  data.failSave = true;
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  await page.getByRole("alert").waitFor();
  assert.equal(data.prefs.dashboard.visual.background.id, "nordlicht");
  data.failSave = false;
  await page.getByRole("button", { name: "Speichern", exact: true }).click();
  await page
    .getByRole("button", { name: "Dashboard bearbeiten", exact: true })
    .waitFor();
  assert.equal(images.size, 1);
  await page.reload();
  await page.locator(".dashboard-background").waitFor();
  assert.match(
    await page.locator(".dashboard-background .picture").getAttribute("style"),
    /api\/v1\/background\/[a-f0-9]{64}/,
  );
  await edit();
  await page.getByRole("tab", { name: "Animationen", exact: true }).click();
  await page
    .getByRole("checkbox", { name: "Sanfte Messwertübergänge", exact: true })
    .uncheck();
  assert.equal(
    await page
      .locator("[gs-id=cpu] .metric-level")
      .first()
      .evaluate((e) => getComputedStyle(e).transitionDuration),
    "0s",
  );
  await page
    .getByRole("checkbox", { name: "Sanfte Messwertübergänge", exact: true })
    .check();
  await page.emulateMedia({ reducedMotion: "reduce" });
  assert.equal(
    await page
      .locator("[gs-id=cpu] .metric-level")
      .first()
      .evaluate((e) => getComputedStyle(e).transitionDuration),
    "0s",
  );
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.getByRole("button", { name: "Abbrechen", exact: true }).click();
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
  await edit();
  await page.getByRole("tab", { name: "Oberfläche", exact: true }).click();
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
        undoRedo: true,
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
