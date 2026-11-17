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
  // The agent is a program of the desktop only when it is switched on.
  await made.ctx.route("**/api/v1/services/state", (route) =>
    route.fulfill({ json: { docker: true, vms: true, lxc: false, agent: true } }),
  );
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
    if (p === "/agent/projects") return route.fulfill({ json: { projects: [] } });
    if (p === "/agent/prefs") return route.fulfill({ json: {} });
    // Anything else is not there, as in test-ui-agent.mjs.
    return route.fulfill({ status: 404, json: { error: { code: "not_found", message: "fixture" } } });
  });
  return made;
}

/** Chromium's counters over `ms` milliseconds while the page runs. */
async function measure(page, ms = 4000, inner = null) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Performance.enable");
  const read = async () =>
    Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  const count = (target) => target.evaluate(
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
  const [f, g] = await Promise.all([count(page), inner ? count(inner) : null]);
  const b = await read();
  const s = (b.Timestamp - a.Timestamp) || ms / 1000;
  const per = (k) => +((b[k] - a[k]) / s).toFixed(1);
  return {
    seconds: +s.toFixed(2),
    framesPerSecond: +(f.n / s).toFixed(1),
    longestFrameMs: +f.worst.toFixed(1),
    ...(g ? { windowFramesPerSecond: +(g.n / s).toFixed(1), windowLongestFrameMs: +g.worst.toFixed(1) } : {}),
    scriptMsPerSecond: +(((b.ScriptDuration - a.ScriptDuration) * 1000) / s).toFixed(2),
    taskMsPerSecond: +(((b.TaskDuration - a.TaskDuration) * 1000) / s).toFixed(2),
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
    // Only the figure moves: the wallpaper's drift (a CSS animation of
    // the desktop) stands still, so the counters belong to the figure.
    const still = '.paper{animation:none!important}';
    let h = await page.addStyleTag({ content: still });
    await page.waitForTimeout(500);
    results.widgetFigureOnly = await measure(page);
    if (process.env.FIGURE_EXPERIMENTS === "1") {
      const g = await page.addStyleTag({ content: '[data-widget="agent"] .fig{display:none!important}' });
      await page.waitForTimeout(500);
      results.widgetNothing = await measure(page, 3000);
      console.log(await page.evaluate(() => document.getAnimations().map((a) => `${a.animationName ?? "script"} ${a.playState} on ${a.effect?.target?.className}`)));
      await g.evaluate((el) => el.remove());
    }
    await h.evaluate((el) => el.remove());
    // The agent's window: a tap on the running chat opens it.
    await page.locator('[data-widget="agent"]').getByRole("button", { name: /Bericht über die Sicherungen/ }).first().click();
    const frame = page.frameLocator("section.win iframe").last();
    try {
      await frame.locator(".live-line .fig").waitFor({ timeout: 40000 });
    } catch (e) {
      await page.screenshot({ path: `${out}/figure-window-failed.png` });
      console.error(errors);
      throw e;
    }
    await page.waitForTimeout(800);
    const inner = page.frames().find((f) => f.url().includes("/agent"));
    results.window = await measure(page, 4000, inner);
    await page.screenshot({ path: `${out}/figure-window-1850.png` });
    // Whether the browser plays the dots on its compositor: its trace
    // names every animation and why it could not (compositeFailed).
    await browser.startTracing(page, { categories: ["blink.animations", "devtools.timeline"] });
    await page.waitForTimeout(3500);
    const trace = JSON.parse((await browser.stopTracing()).toString()).traceEvents.filter((e) => e.name === "Animation");
    const named = new Map(trace.filter((e) => e.ph === "b" && e.args?.data?.nodeName).map((e) => [e.id2?.local ?? e.id, e.args.data]));
    const composited = { dots: [0, 0], other: {} };
    for (const e of trace) {
      const failed = e.args?.data?.compositeFailed;
      if (failed === undefined) continue;
      const of = named.get(e.id2?.local ?? e.id);
      if (/^I\b/.test(of?.nodeName ?? "") && !of.displayName) composited.dots[failed === 0 ? 0 : 1]++;
      else composited.other[of?.displayName || of?.nodeName || "?"] = failed === 0 ? "compositor" : (e.args.data.unsupportedProperties ?? [failed]).join(",");
    }
    results.composited = composited;
    {
      // In the window too: the wallpaper and the trail over the text stand
      // still, the dots of the window and of the widget move.
      const a = await page.addStyleTag({ content: still });
      const b = await inner.addStyleTag({ content: ".trail{animation:none!important}" });
      await page.waitForTimeout(500);
      results.windowFigureOnly = await measure(page, 4000, inner);
      await a.evaluate((el) => el.remove());
      await b.evaluate((el) => el.remove());
    }
    // FIGURE_EXPERIMENTS=1: what costs the frames – each part switched off
    // in turn (for the report; the page is changed only in this browser).
    if (process.env.FIGURE_EXPERIMENTS === "1") {
      const variants = {
        noTrail: [inner, ".trail{animation:none!important;background:none!important;color:inherit!important}"],
        noFigure: [inner, ".live-line .fig{display:none!important}"],
        noLine: [inner, ".live-line{display:none!important}"],
        noGlass: [page, "*{backdrop-filter:none!important;-webkit-backdrop-filter:none!important}"],
        noWidgetFigure: [page, '[data-widget="agent"] .fig{visibility:hidden!important}'],
        glassBehind: [page, "section.win{backdrop-filter:none!important;-webkit-backdrop-filter:none!important;isolation:isolate}section.win::before{content:'';position:absolute;inset:0;z-index:-1;pointer-events:none;backdrop-filter:blur(14px) saturate(1.3);-webkit-backdrop-filter:blur(14px) saturate(1.3)}"],
      };
      variants.nothingMoves = [
        [inner, ".live-line{display:none!important}"],
        [page, '[data-widget="agent"] .fig{visibility:hidden!important}'],
      ];
      for (const [name, list] of Object.entries(variants)) {
        const parts = Array.isArray(list[0]) ? list : [list];
        const handles = [];
        for (const [target, css] of parts) handles.push(await target.addStyleTag({ content: css }));
        await page.waitForTimeout(500);
        results["window-" + name] = await measure(page, 3000, inner);
        for (const h of handles) await h.evaluate((el) => el.remove());
      }
    }
    await ctx.close();
  }
  console.log(JSON.stringify(results, null, 2));
  await writeFile(`${out}/figure-metrics.json`, JSON.stringify(results, null, 2));
  if (!report) {
    // Layout once in a while (the clock of the bar, a word that changes)
    // is fine; one per frame is not.
    for (const [where, m] of Object.entries(results))
      if (m.framesPerSecond) assert.ok(
        m.layoutsPerSecond < m.framesPerSecond / 4,
        `${where}: no layout in every frame (${m.layoutsPerSecond}/s at ${m.framesPerSecond} frames/s)`,
      );
    // The dots run on the compositor: every stretch of them was handed to
    // it, none was refused. (Chromium still counts a style update per frame
    // for any running animation, composited or not, so the style counters
    // do not tell this.)
    const [onCompositor, refused] = results.composited.dots;
    assert.ok(onCompositor >= 3 && refused === 0, `dots on the compositor (${onCompositor} yes, ${refused} no)`);
  }
  assert.deepEqual(errors, [], "no script errors");
  console.log("agent figure ok:", out);
} finally {
  await browser.close();
}
