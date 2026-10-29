// Restart and shutdown end the guests first (owner request of 29 Oct 2026):
// the dialog lists VMs, containers and Docker apps with their state, warns
// after five minutes, and offers "go on anyway" (red, left) and cancel.
// Desktop and 390 px. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5186 node scripts/test-ui-power.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5186";
const out = process.env.UI_OUTPUT || "out/validation/ui-power";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const items = (win) => [
  { kind: "vm", name: "haos", state: "stopped" },
  { kind: "vm", name: "win11", state: win },
  { kind: "lxc", name: "pihole", state: "stopped" },
  { kind: "docker", name: "immich-server", state: "stopped" },
  { kind: "docker", name: "immich-db", state: "stopped" },
];

async function open(width) {
  const made = await create("owner", "graphit", width);
  const state = { preparing: null, calls: [] };
  await made.ctx.route("**/api/v1/system/power**", async (r) => {
    const req = r.request();
    const path = new URL(req.url()).pathname;
    state.calls.push([req.method(), path, req.postData() ?? ""]);
    if (req.method() === "POST" && path.endsWith("/system/power")) {
      state.preparing = { action: JSON.parse(req.postData()).action, by: "alex", phase: "stopping", unattended: false, items: items("stopping") };
      return r.fulfill({ status: 202, body: "" });
    }
    if (req.method() === "POST" && path.endsWith("/now")) {
      state.preparing.phase = "going";
      return r.fulfill({ status: 202, body: "" });
    }
    if (req.method() === "DELETE") state.preparing = null;
    return r.fulfill({ json: { pending: null, rebuilding: false, preparing: state.preparing } });
  });
  // The restart itself: the health check answers once the NAS is "back".
  await made.ctx.route("**/api/v1/health", (r) => r.fulfill({ status: 503, body: "" }));
  await made.page.goto(`${base}/`);
  return { ...made, state };
}

async function startRestart(page) {
  await page.locator("header .account .trigger").click();
  await page.getByRole("menuitem", { name: /NAS neu starten/ }).click();
  const dialog = page.locator("dialog[open]");
  await dialog.getByRole("button", { name: "Neu starten", exact: true }).click();
  await dialog.getByRole("heading", { name: "Neustart wird vorbereitet" }).waitFor();
  return dialog;
}

try {
  {
    const { ctx, page, state } = await open(1440);
    const dialog = await startRestart(page);
    const post = state.calls.find(([m, p]) => m === "POST" && p.endsWith("/system/power"));
    assert.deepEqual(JSON.parse(post[2]), { action: "reboot", afterRebuild: false, wait: true });
    await dialog.locator(".items li").nth(4).waitFor();
    assert.equal(await dialog.locator(".items li").count(), 5);
    // win11 does not end; after the waiting time the dialog says so.
    state.preparing = { ...state.preparing, phase: "waiting", items: items("stopping") };
    await dialog.getByText(/Nach 5 Minuten läuft noch: win11/).waitFor();
    assert.equal(await dialog.locator(".state.ok").count(), 4);
    await dialog.screenshot({ path: `${out}/waiting.png`, animations: "disabled" });
    // The dangerous button stands left of cancel.
    const anyway = dialog.getByRole("button", { name: "Trotzdem jetzt neu starten" });
    const cancel = dialog.getByRole("button", { name: "Abbrechen" });
    assert.ok((await anyway.boundingBox()).x < (await cancel.boundingBox()).x, "anyway left of cancel");
    // The dialog cannot just be closed away while preparing.
    await page.keyboard.press("Escape");
    assert.ok(await dialog.isVisible());
    await anyway.click();
    await dialog.getByRole("heading", { name: "NAS startet neu …" }).waitFor();
    assert.ok(state.calls.some(([m, p]) => m === "POST" && p.endsWith("/system/power/now")));
    await ctx.close();
  }
  {
    const { ctx, page, state } = await open(390);
    const dialog = await startRestart(page);
    await dialog.locator(".items li").first().waitFor();
    assert.ok((await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)) <= 0, "no sideways scroll");
    await dialog.screenshot({ path: `${out}/mobile.png`, animations: "disabled" });
    await dialog.getByRole("button", { name: "Abbrechen" }).click();
    await dialog.waitFor({ state: "detached" });
    assert.ok(state.calls.some(([m]) => m === "DELETE"));
    // Opened again while a preparation runs (another tab, an update): the
    // dialog shows it right away.
    state.preparing = { action: "poweroff", by: "update", phase: "stopping", unattended: true, items: items("stopping") };
    await page.locator("header .account .trigger").click();
    await page.getByRole("menuitem", { name: /NAS neu starten/ }).click();
    await page.locator("dialog[open]").getByRole("heading", { name: "Herunterfahren wird vorbereitet" }).waitFor();
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("ui power ok:", out);
} finally {
  await browser.close();
}
