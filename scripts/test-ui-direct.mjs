// PLAN 75-14: apps with their own desktop stream (jlesage/noVNC, Selkies,
// KasmVNC) open directly in their window under their HTTPS name, like Kasm
// Workspaces. Mocked: /apps/mine with "stream", the window address with
// ?direct=1 and the app's name (Playwright answers https://mail.nas.local
// with an enter page that says hello like titand's). Checked: the direct
// frame, the fallback to the remote view when the app is not reached
// (409 and no hello in time), and the switch in the ⋯ menu that is kept.
// Isolated data only; nothing reaches a NAS.
//
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5173 node scripts/test-ui-direct.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({ headless: true });
const base = process.env.UI_URL || "http://127.0.0.1:5173";
const out = process.env.UI_OUTPUT || "out/validation/direct";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const shot = (page, name) => page.screenshot({ path: `${out}/${name}.png` });

const time = "2026-11-09T08:00:00Z";
const ctr = (name, port, image) => ({
  id: name,
  name,
  image,
  state: "running",
  since: time,
  ports: [{ host: port, container: port, protocol: "tcp" }],
  app: name,
  service: name,
  managed: true,
  cpuPercent: 1,
  memoryBytes: 200e6,
  cpuHistory: [],
  webui: `https://[host]:${port}`,
  publicUrl: "",
  proxyUrl: `https://${name}.nas.local`,
  icon: "",
  updateAvailable: false,
});
const containers = [
  ctr("thunderbird", 5800, "jlesage/thunderbird"),
  ctr("immich", 2283, "ghcr.io/immich-app/immich-server"),
];

/**
 * A desktop with Thunderbird (own stream) and Immich (none). window: how
 * the NAS answers POST /apps/thunderbird/window?direct=1 – "ok", "far"
 * (409: the UI is reached by an IP) or "silent" (the page never says hello).
 */
async function open(window, init) {
  const made = await create("owner", "graphit", 1440);
  await made.page.setViewportSize({ width: 1440, height: 900 });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  if (init) await made.ctx.addInitScript(init);
  made.data.dockerContainers = containers;
  made.data.dockerApps = containers.map((c) => c.name);
  const asked = [];
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    if (p === "/preferences/desktop" && route.request().method() === "PUT")
      return route.fulfill({ status: 204 });
    if (p === "/apps/mine")
      return route.fulfill({
        json: {
          apps: [
            {
              app: "immich",
              url: "https://immich.nas.local",
              webui: "2283",
              running: true,
            },
            {
              app: "thunderbird",
              url: "https://mail.nas.local",
              webui: "https://[host]:5800",
              running: true,
              stream: "novnc",
            },
          ],
        },
      });
    if (p === "/apps/thunderbird/window") {
      asked.push(url.search);
      if (window === "far")
        return route.fulfill({
          status: 409,
          json: { error: { code: "direct_unreachable", message: "x" } },
        });
      return route.fulfill({
        json: {
          url: `https://mail.nas.local/.titanos/enter?t=abc&frame=1`,
          expires: time,
        },
      });
    }
    return route.fallback();
  });
  // The app's name: the enter page of titand (direct.go) says hello to the UI.
  await made.ctx.route("https://mail.nas.local/**", (route) => {
    const u = new URL(route.request().url());
    if (u.pathname === "/.titanos/enter")
      return route.fulfill({
        contentType: "text/html; charset=utf-8",
        body:
          window === "silent"
            ? "<!doctype html><p>never says hello</p>"
            : `<!doctype html><script>parent.postMessage({ type: 'titanos-direct', ok: true }, ${JSON.stringify(base)}); location.replace('/');</script>`,
      });
    return route.fulfill({
      contentType: "text/html; charset=utf-8",
      body: '<!doctype html><body style="margin:0;background:#2d6cdf;color:#fff;font:24px sans-serif"><p id="novnc">Thunderbird über noVNC</p></body>',
    });
  });
  // The remote view (fallback): a socket that only says "starting".
  made.page.routeWebSocket(/\/api\/v1\/viewer\/[^/]+\/stream/, (ws) => {
    ws.send(JSON.stringify({ t: "starting" }));
  });
  await made.page.goto(base + "/");
  await made.page.waitForSelector(".desktop", { timeout: 15000 });
  await made.page.waitForTimeout(500);
  return { ...made, asked };
}

async function launch(page, name) {
  await page.locator(".ico", { hasText: name }).first().click();
  return page.locator("section.win", { hasText: name });
}

// 1. Reached: Thunderbird in a frame of its own name; Immich keeps the remote view.
{
  const { ctx, page, asked } = await open("ok");
  const win = await launch(page, "thunderbird");
  const frame = win.locator(".direct iframe");
  await frame.waitFor();
  assert.match(
    await frame.getAttribute("src"),
    /^https:\/\/mail\.nas\.local\/\.titanos\/enter\?t=abc&frame=1$/,
  );
  await page.waitForFunction(
    () => !document.querySelector(".direct iframe.hidden"),
    null,
    { timeout: 5000 },
  );
  assert.equal(
    await page.frameLocator(".direct iframe").locator("#novnc").textContent(),
    "Thunderbird über noVNC",
  );
  assert.equal(await win.locator(".remote").count(), 0, "no remote view");
  assert.deepEqual(asked, ["?direct=1"]);
  await shot(page, "01-direct");
  await win
    .locator(".titlebar button.close, .titlebar button[aria-label='Schließen']")
    .first()
    .click();
  await win.waitFor({ state: "detached" });
  const immich = await launch(page, "immich");
  await immich.locator(".remote").waitFor();
  assert.equal(
    await immich.locator(".direct").count(),
    0,
    "Immich has no own stream",
  );
  await ctx.close();
}

// 2. The UI reached by an IP (409): the remote view at once, the menu says why.
{
  const { ctx, page } = await open("far");
  const win = await launch(page, "thunderbird");
  await win.locator(".remote").waitFor({ timeout: 5000 });
  await win.locator('.titlebar button[aria-label="Mehr"]').click();
  await win.getByText("Direkt nicht erreichbar", { exact: false }).waitFor();
  await shot(page, "02-fallback-menu");
  await ctx.close();
}

// 3. No hello in time (a tunnel without the app's name): the remote view.
{
  const { ctx, page } = await open("silent");
  const win = await launch(page, "thunderbird");
  await win.locator(".direct").waitFor();
  await win.locator(".remote").waitFor({ timeout: 12000 });
  assert.equal(await win.locator(".direct").count(), 0);
  await ctx.close();
}

// 4. The ⋯ menu switches to the remote view; the choice is kept in this browser.
{
  const { ctx, page } = await open("ok");
  let win = await launch(page, "thunderbird");
  await win.locator(".direct iframe").waitFor();
  await win.locator('.titlebar button[aria-label="Mehr"]').click();
  await win
    .getByRole("menuitemcheckbox", { name: "Über Fernansicht anzeigen" })
    .click();
  await win.locator(".remote").waitFor();
  const kept = await page.evaluate(() =>
    localStorage.getItem("titanos.direct"),
  );
  assert.equal(kept, JSON.stringify({ thunderbird: "remote" }));
  await win.locator('.titlebar button[aria-label="Mehr"]').click();
  await win.getByRole("menuitemcheckbox", { name: "Direkt anzeigen" }).click();
  await win.locator(".direct iframe").waitFor();
  assert.equal(
    await page.evaluate(() => localStorage.getItem("titanos.direct")),
    "{}",
  );
  await ctx.close();
}

await browser.close();
assert.deepEqual(errors, [], "no script errors");
console.log(`direct window UI checks passed; screenshots in ${out}`);
