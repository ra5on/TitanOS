// 1.07 (owner 17 Nov 2026: "Die Animation der 3 Punkte am Arbeitsplatz ist
// nicht flüssig"): how much the working figure of the KI-Agent costs the
// browser per frame – on the desktop at 1850 px with its glass, in the
// agent's window (a running chat) and in the agent widget. Measured with
// Chromium's own counters (CDP Performance.getMetrics) over a few seconds:
// layouts and style recalculations per second and their time, and the
// frames the page drew. With FIGURE_REPORT=1 the numbers are printed only;
// otherwise the test asks that a frame does no layout work.
// Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5206 node scripts/test-ui-agent-figure.mjs
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5206";
const out = process.env.UI_OUTPUT || "out/validation/agent-figure";
const report = process.env.FIGURE_REPORT === "1";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const now = new Date().toISOString();
const chat = {
  id: "c1",
  title: "Bericht über die Sicherungen schreiben",
  state: "running",
  created: now,
  updated: now,
  origin: "web",
  entries: [{ id: "e1", at: now, role: "user", content: "Schreib einen Bericht." }],
};

async function desk() {
  const made = await create("owner", "graphit", 1850);
  await made.page.setViewportSize({ width: 1850, height: 1000 });
  // The desktop mode (the shared fixtures use the classic frame).
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  made.data.prefs.desktop = {
    version: 1,
    customIcons: true,
    icons: ["app:files"],
    customWidgets: true,
    widgets: [{ id: "w-agent", kind: "agent", x: 12, y: 1, w: 26, h: 20 }],
  };
  await made.ctx.route("**/api/v1/agent/**", (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    if (p === "/agent/status")
      return route.fulfill({
        json: { enabled: true, allowed: true, admin: true, model: "gpt-oss:20b", volume: true, reachable: true, models: [] },
      });
    if (p === "/agent/chats") return route.fulfill({ json: { chats: [chat] } });
    if (p === "/agent/chats/c1") return route.fulfill({ json: chat });
    if (p === "/agent/chats/c1/events")
      return route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body:
          `event: snapshot\ndata: ${JSON.stringify({ type: "snapshot", entries: [], state: "running" })}\n\n` +
          'event: state\ndata: {"type":"state","state":"running"}\n\n',
      });
    if (p === "/agent/runs") return route.fulfill({ json: { runs: ["c1"], max: 0 } });
    if (p.endsWith("/seen")) return route.fulfill({ status: 204 });
    return route.fulfill({ json: {} });
  });
  return made;
}

/** Chromium's counters over `ms` milliseconds while the page runs. */
async function measure(page, ms = 4000) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Performance.enable");
  const read = async () =>
    Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  const frames = page.evaluate(
    (ms) =>
      new Promise((done) => {
        let n = 0;
        let worst = 0;
        let last = performance.now();
        const end = last + ms;
        const tick = (t) => {
          n++;
          worst = Math.max(worst, t - last);
          last = t;
          if (t < end) requestAnimationFrame(tick);
          else done({ n, worst });
        };
        requestAnimationFrame(tick);
      }),
    ms,
  );
  const a = await read();
  const f = await frames;
  const b = await read();
  const s = (b.Timestamp - a.Timestamp) || ms / 1000;
  const per = (k) => +((b[k] - a[k]) / s).toFixed(1);
  return {
    seconds: +s.toFixed(2),
    framesPerSecond: +(f.n / s).toFixed(1),
    longestFrameMs: +f.worst.toFixed(1),
    layoutsPerSecond: per("LayoutCount"),
    styleRecalcsPerSecond: per("RecalcStyleCount"),
    layoutMsPerSecond: +(((b.LayoutDuration - a.LayoutDuration) * 1000) / s).toFixed(2),
    styleMsPerSecond: +(((b.RecalcStyleDuration - a.RecalcStyleDuration) * 1000) / s).toFixed(2),
  };
}

const results = {};
try {
  // The widget on the desktop, behind the glass of the desktop.
  {
    const { ctx, page } = await desk();
    await page.goto(base + "/");
    const fig = page.locator('[data-widget="agent"] [data-agent-figure] .fig');
    await fig.waitFor({ timeout: 30000 });
    await page.waitForTimeout(800);
    results.widget = await measure(page);
    // The agent's window: a tap on the running chat opens it.
    await page.locator('[data-widget="agent"]').getByRole("button", { name: /Bericht über die Sicherungen/ }).first().click();
    const frame = page.frameLocator("iframe").last();
    await frame.locator(".live-line .fig").waitFor({ timeout: 40000 });
    await page.waitForTimeout(800);
    results.window = await measure(page);
    await page.screenshot({ path: `${out}/figure-window-1850.png` });
    await ctx.close();
  }
  console.log(JSON.stringify(results, null, 2));
  await writeFile(`${out}/figure-metrics.json`, JSON.stringify(results, null, 2));
  if (!report) {
    // Layout once in a while (the clock of the bar, a word that changes)
    // is fine; one per frame is not.
    for (const [where, m] of Object.entries(results))
      assert.ok(
        m.layoutsPerSecond < m.framesPerSecond / 4,
        `${where}: no layout in every frame (${m.layoutsPerSecond}/s at ${m.framesPerSecond} frames/s)`,
      );
  }
  assert.deepEqual(errors, [], "no script errors");
  console.log("agent figure ok:", out);
} finally {
  await browser.close();
}
