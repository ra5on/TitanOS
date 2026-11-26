// Real desktop shell, with isolated API replies for two backup jobs and a transfer.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const base = process.env.UI_URL || "http://127.0.0.1:5278";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const browser = await chromium.launch({ executablePath: process.env.BROWSER_EXECUTABLE });
const errors = [];
const fixture = fixtureFactory(browser, errors);
const out = process.env.UI_OUTPUT;
if (out) await mkdir(out, { recursive: true });

function backup(id, failed = false) {
  return {
    job: { id, enabled: true, sources: ["Dokumente"], applications: [], every: "daily",
      time: "03:00", destination: { type: "usb", folder: "Sicherung" }, keep: { last: 3 } },
    stateKnown: true, secretSet: {}, next: null,
    state: { running: false, phase: "", lastError: failed ? "drive missing" : "",
      lastErrorCode: failed ? "usb_not_found" : "", bytesDone: 0, bytesTotal: 0,
      lastOK: "2026-11-20T12:00:00Z", lastRun: "2026-11-22T12:00:00Z" },
  };
}

try {
  for (const { role, theme, reducedMotion } of [
    { role: "owner", theme: "graphit", reducedMotion: "no-preference" },
    { role: "user", theme: "schnee", reducedMotion: "reduce" },
  ]) {
    const { ctx, page, data } = await fixture(role, theme, 1440, "de", { reducedMotion });
    try {
      data.backups = [backup("archiv"), backup("nachtsicherung", true)];
      data.fileJobs = [{ id: "copy", state: "running", names: ["Rechnung.pdf"], copied: 25, bytes: 100 }];
      await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
      await page.goto(base);
      const status = page.locator('[data-widget="status"]');
      await status.getByRole("link", { name: /Rechnung\.pdf.*25 %/ }).waitFor();
      const problem = status.getByRole("link", { name: /USB-Laufwerk fehlt.*nachtsicherung/ });
      if (role === "owner") {
        await problem.waitFor();
        assert.equal(await problem.getAttribute("href"), "/settings/backups/data#backup-nachtsicherung");
        if (out) await page.screenshot({ path: join(out, "arbeitsplatz-sicherungsfehler.png") });
        await problem.focus();
        await page.keyboard.press("Enter");
        const detail = page.frameLocator("iframe[data-window]").first().locator('.backup-table tr[aria-selected="true"]');
        assert.match(await detail.innerText(), /nachtsicherung/);
        if (out) await page.screenshot({ path: join(out, "fenster-sicherungsauftrag.png") });
      } else {
        assert.equal(await problem.count(), 0);
      }
      const dockFiles = page.locator('.dock [data-key="app:files"] button.it');
      await dockFiles.waitFor();
      await page.evaluate(() => {
        window.__deskFrames = [];
        document.addEventListener("animationstart", (event) => {
          if (!event.target.matches("section.win.grow")) return;
          const el = event.target;
          const style = getComputedStyle(el);
          const area = el.parentElement.getBoundingClientRect();
          window.__deskFrames.push({
            x: area.x + parseFloat(el.style.left) + parseFloat(el.style.width) / 2 + parseFloat(style.getPropertyValue("--from-x")),
            y: area.y + parseFloat(el.style.top) + parseFloat(el.style.height) / 2 + parseFloat(style.getPropertyValue("--from-y")),
            duration: style.animationDuration,
          });
        }, true);
      });
      const center = await dockFiles.evaluate(el => {
        const r = el.getBoundingClientRect();
        return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
      });
      await dockFiles.focus();
      await page.keyboard.press("Enter");
      await page.locator('section.win[aria-label="Dateien"]').waitFor();
      if (reducedMotion === "reduce") {
        assert.equal(await page.locator("section.win.grow").count(), 0);
      } else {
        await page.waitForFunction(() => window.__deskFrames.length > 0);
        const first = await page.evaluate(() => window.__deskFrames[0]);
        assert.equal(first.duration, "0.18s");
        assert.ok(Math.abs(first.x - center.x) <= 2 && Math.abs(first.y - center.y) <= 2, JSON.stringify({first, center}));
      }
    } finally { await ctx.close(); }
  }
  assert.deepEqual(errors, []);
  console.log("Workspace: backup target, role boundaries, transfer progress, dock origin, reduced motion and keyboard passed.");
} finally {
  await browser.close();
}
