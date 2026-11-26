// Run against a local titand -dev with a disposable owner. Inventory actual window
// content and separate an unavailable target from a successful visual inspection.
import { mkdir, readdir, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { join, relative, dirname } from "node:path";
import assert from "node:assert/strict";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL;
const out = process.env.UI_OUTPUT;
assert.ok(
  out && ["localhost", "127.0.0.1", "[::1]"].includes(new URL(base).hostname),
);
const routesRoot = fileURLToPath(
  new URL("../web/src/routes/", import.meta.url),
);
const sources = (await readdir(routesRoot, { recursive: true })).filter((p) =>
  p.endsWith("+page.svelte"),
);
const examples = {
  "/[...rest]": "/unbekannt",
  "/docker/apps/[name]": "/docker/apps/web",
  "/apps/install/[source]/[id]": "/apps/install/titanos/immich",
  "/files": "/files",
  "/vms/edit": "/vms/edit?vm=source",
  "/vms/screen": "/vms/screen?vm=source",
};
const paths = [
  "/",
  ...sources
    .map((f) => "/" + relative(routesRoot, dirname(join(routesRoot, f))))
    .filter((p) => p !== "/")
    .sort(),
];
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
await mkdir(out, { recursive: true });
const results = [];
const inventoryFile = join(out, "inventar.json");
try {
  const auth = await browser.newContext({ baseURL: base, locale: "de-DE" });
  const [name, password] = (process.env.UI_LOGIN || "").split(":");
  assert.equal(
    (
      await auth.request.post("/api/v1/login", {
        data: { name, password },
        headers: { Origin: base },
      })
    ).status(),
    200,
  );
  await auth.request.post("/api/v1/system/disclaimer", {
    data: { version: 1 },
    headers: { Origin: base },
  });
  const storage = await (await auth.request.get("/api/v1/storage")).json();
  if (storage.volumes?.length)
    await auth.request.put("/api/v1/docker/settings", {
      data: { enabled: true, volume: storage.volumes[0].name },
      headers: { Origin: base },
    });
  await auth.request.post("/api/v1/nvr/enable", {
    data: {
      volume: storage.volumes?.[0]?.name,
      enableDocker: true,
      confirmSleepingVolume: true,
    },
    headers: { Origin: base },
  });
  await auth.request.put("/api/v1/preferences/hints", {
    data: { "webapps-0.63": true },
    headers: { Origin: base },
  });
  // These are opt-in programs. Enable them for this dedicated development
  // account so a cold deep link exercises a program, not its access gate.
  assert.equal(
    (
      await auth.request.put(
        "/api/v1/users/" + encodeURIComponent(name) + "/mail",
        {
          data: { enabled: true },
          headers: { Origin: base },
        },
      )
    ).status(),
    200,
  );
  const settings = await (
    await auth.request.get("/api/v1/agent/settings")
  ).json();
  const agent = await auth.request.put("/api/v1/agent/settings", {
    data: {
      ...settings,
      enabled: true,
      ollama: "http://127.0.0.1:9",
      model: "test",
    },
    headers: { Origin: base },
  });
  if (!agent.ok()) console.log("agent setup", agent.status()); // Explicitly accounted for in the inventory.
  const state = await auth.storageState();
  await auth.close();
  const only = process.env.UI_ROUTES?.split(",");
  for (const source of paths.filter((p) => !only || only.includes(p))) {
    const route = examples[source] || source;
    const path = route.split("?")[0];
    const ctx = await browser.newContext({
      storageState: state,
      viewport: { width: 1440, height: 960 },
      locale: "de-DE",
      reducedMotion: "reduce",
    });
    const page = await ctx.newPage();
    const faults = [];
    page.on("pageerror", (e) => faults.push(String(e)));
    const filename =
      path === "/"
        ? "arbeitsplatz.png"
        : path.slice(1).replaceAll("/", "-") + ".png";
    try {
      await page.goto(base + route, { waitUntil: "domcontentloaded" });
      let view;
      for (let i = 0; i < 45; i++) {
        const frames = page
          .frames()
          .filter(
            (f) =>
              f.url().startsWith(base) && new URL(f.url()).pathname === path,
          );
        view = frames.find((f) => f !== page.mainFrame()) ?? frames[0];
        if (
          view &&
          (await view
            .locator("main, .desktop, .lock-screen, [role=main]")
            .first()
            .isVisible()
            .catch(() => false))
        ) {
          if (
            view !== page.mainFrame() ||
            path === "/" ||
            !(await page
              .locator(".desktop")
              .isVisible()
              .catch(() => false))
          )
            break;
        }
        await page.waitForTimeout(100);
      }
      await page.evaluate(() => document.fonts.ready);
      await page.waitForTimeout(240);
      const inner = page
        .frames()
        .filter((f) => f !== page.mainFrame() && f.url().startsWith(base));
      view =
        inner.find((f) => new URL(f.url()).pathname === path) ??
        (new URL(page.url()).pathname === path && !inner.length
          ? page.mainFrame()
          : (inner.at(-1) ?? page.mainFrame()));
      const info = await view.evaluate(() => {
        const visible = (el) =>
          el.getClientRects().length > 0 &&
          getComputedStyle(el).visibility !== "hidden";
        const labels = (selector) => [
          ...new Set(
            [...document.querySelectorAll(selector)]
              .filter(visible)
              .map((e) =>
                (e.getAttribute("aria-label") || e.innerText || e.title || "")
                  .trim()
                  .replace(/\s+/g, " "),
              )
              .filter(Boolean),
          ),
        ];
        return {
          title: document.title,
          headings: labels("h1,h2,h3"),
          actions: labels("main button,main a,[role=toolbar] button").slice(
            0,
            50,
          ),
          alerts: labels("[role=alert]"),
          overflow: document.documentElement.scrollWidth > innerWidth + 1,
        };
      });
      await page.screenshot({
        path: join(out, filename),
        animations: "disabled",
      });
      results.push({
        source,
        route,
        actual: new URL(view.url()).pathname,
        filename,
        ...info,
        faults,
      });
    } catch (e) {
      results.push({ source, route, error: String(e), faults });
    } finally {
      await ctx.close();
    }
    await writeFile(inventoryFile, JSON.stringify(results, null, 2));
  }
  console.log(
    JSON.stringify({
      pages: results.length,
      errors: results
        .filter((r) => r.error || r.faults.length)
        .map((r) => [r.route, r.error || r.faults]),
      redirects: results
        .filter((r) => r.actual !== r.route.split("?")[0])
        .map((r) => [r.route, r.actual]),
    }),
  );
} finally {
  await browser.close();
}
