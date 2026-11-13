// Protokolle (Werkzeuge › Protokolle): messages known to be harmless are
// quiet with a note why, each with its own note (the owner's log of
// 13.11.2026: firmware messages of the mainboard at every start), and a
// line without text says so instead of an empty "Fehler".
// Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5266 node scripts/test-ui-logs-harmless.mjs
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5266";
const de = JSON.parse(await readFile(new URL("../web/src/lib/i18n/de.json", import.meta.url), "utf8"));
const errors = [];
const create = fixtureFactory(browser, errors);
try {
  const { ctx, page } = await create("owner", "hell", 1280, "de");
  const at = (s) => `2026-11-13T05:13:${String(s).padStart(2, "0")}Z`;
  const entries = [
    { time: at(21), priority: 3, source: "kernel", message: "vmd 0000:00:0e.0: Unknown Bus Offset Setting (3)" },
    { time: at(21), priority: 3, source: "kernel", message: "ACPI BIOS Error (bug): Could not resolve symbol [\\_SB.PC00.LPCB.HEC.DPTF.FCHG], AE_NOT_FOUND (20240827/psargs-365)" },
    { time: at(21), priority: 3, source: "kernel", message: "ACPI Error: Aborting method \\_SB.IETM.CHRG.PPSS due to previous error (AE_NOT_FOUND) (20240827/psparse-543)" },
    { time: at(34), priority: 3, source: "kernel", message: "  \n" },
    { time: at(40), priority: 4, source: "tailscaled", message: "portmapper: failed to get PCP mapping" },
    { time: at(50), priority: 3, source: "titand", message: "something really failed" },
  ];
  await ctx.route(/\/api\/v1\/logs\?/, (route) => route.fulfill({ json: { entries, cursor: "c1" } }));
  await page.goto(base + "/tools/logs");
  const lines = page.locator(".line");
  await lines.first().waitFor();
  assert.equal(await lines.count(), entries.length);
  for (const i of [0, 1, 2]) {
    assert.ok(await lines.nth(i).evaluate((el) => el.classList.contains("harmless")), `firmware line ${i} is quiet`);
    assert.equal(await lines.nth(i).getAttribute("title"), de["tools.harmlessFirmware"]);
  }
  assert.equal(await lines.nth(4).getAttribute("title"), de["tools.harmless"], "the PCP note stays its own");
  await lines.nth(3).getByText(de["tools.noText"]).waitFor();
  assert.ok(!(await lines.nth(5).evaluate((el) => el.classList.contains("harmless"))), "a real error stays an error");
  await ctx.close();
  assert.deepEqual(errors, [], "no page errors");
  console.log("logs harmless notes: ok");
} finally {
  await browser.close();
}
