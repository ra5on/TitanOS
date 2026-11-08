// PLAN 72: chats of the KI-Agent go to the Archiv and back (menu of the
// chat, the Archiv in the side bar), and a planned task chooses whether it
// may wake a sleeping disk. Isolated API data only: nothing reaches a NAS
// or an Ollama server.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5240 node scripts/test-ui-agent-archive.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5240";
const out = process.env.UI_OUTPUT || "out/validation/agent-archive";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const at = (h, m = 0) => new Date(Date.UTC(2026, 10, 8, h, m)).toISOString();

const status = {
  enabled: true, allowed: true, admin: true, model: "gpt-oss:20b", think: "medium", context: 0, volume: true, web: false,
  reachable: true, version: "0.40.14", running: [],
  models: [{ name: "gpt-oss:20b", size: 1e10, capabilities: ["completion", "tools"], contextLength: 131072 }],
};

async function open(width, height) {
  const made = await create("owner", "graphit", width);
  const { ctx, page } = made;
  await page.setViewportSize({ width, height });
  const state = {
    chats: [
      { id: "c1aaaaaaaaaa0001", title: "Rechnungen Oktober", created: at(9), updated: at(10), state: "idle", origin: "web" },
      { id: "c1aaaaaaaaaa0002", title: "Alter Umzugsplan", created: at(7), updated: at(8), state: "idle", origin: "web", archived: true },
    ],
    patches: [],
    schedules: [{ id: "s1aaaaaaaaaa0001", name: "Fotos sortieren", task: "Neue Fotos nach Jahr sortieren", every: "daily", at: "03:00", enabled: true, created: at(1) }],
    puts: [],
  };
  await ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const json = (v, s = 200) => route.fulfill({ status: s, json: v });
    if (p === "/services/state") return json({ docker: true, vms: true, lxc: true, agent: true });
    if (!p.startsWith("/agent")) return route.fallback();
    if (p === "/agent/status") return json(status);
    if (p === "/agent/projects") return json({ projects: [] });
    if (p === "/agent/chats" && method === "GET") return json({ chats: state.chats });
    const m = /^\/agent\/chats\/([a-z0-9]+)$/.exec(p);
    if (m && method === "PATCH") {
      const body = route.request().postDataJSON();
      state.patches.push({ id: m[1], ...body });
      const c = state.chats.find((x) => x.id === m[1]);
      Object.assign(c, body);
      return json(c);
    }
    if (m) {
      const c = state.chats.find((x) => x.id === m[1]);
      return json({ ...c, entries: [{ id: "e1", at: c.created, role: "user", content: c.title }] });
    }
    if (p === "/agent/schedules" && method === "GET") return json({ schedules: state.schedules });
    if (p.startsWith("/agent/schedules/") && method === "PUT") {
      const body = route.request().postDataJSON();
      state.puts.push(body);
      state.schedules = state.schedules.map((s) => (s.id === body.id ? { ...s, ...body } : s));
      return json({ schedules: state.schedules });
    }
    if (p === "/agent/tools") return json({ tools: [], areas: [] });
    if (p === "/agent/inbox") return json({ items: [] });
    return json({});
  });
  await page.goto(base + "/agent");
  return { ...made, state };
}

async function assertEventually(check, what, ms = 8000) {
  const until = Date.now() + ms;
  while (Date.now() < until) {
    if (await check()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  assert.fail(`timed out: ${what}`);
}

// Desktop: the archived chat is not in the list; the Archiv shows it.
{
  const { ctx, page, state } = await open(1440, 900);
  const side = page.locator(".agent");
  await side.getByText("Rechnungen Oktober").first().waitFor({ timeout: 15000 });
  assert.equal(await side.getByText("Alter Umzugsplan").count(), 0, "archived chat in the list");
  await side.getByRole("button", { name: /^Archiv\s*1$/ }).click();
  const dialog = page.getByRole("dialog", { name: "Archiv" });
  await dialog.getByText("Alter Umzugsplan").waitFor();
  await page.screenshot({ path: `${out}/archiv.png` });
  await dialog.getByRole("button", { name: "Aus dem Archiv holen" }).click();
  await assertEventually(() => state.patches.some((x) => x.id === "c1aaaaaaaaaa0002" && x.archived === false), "restored");
  await page.keyboard.press("Escape");
  await side.getByText("Alter Umzugsplan").first().waitFor();

  // Archive the open chat from its ⋯ menu.
  await side.getByText("Rechnungen Oktober").first().click();
  await page.getByRole("button", { name: "Weitere Aktionen" }).first().click();
  await page.getByRole("menuitem", { name: "Archivieren" }).click();
  await assertEventually(() => state.patches.some((x) => x.id === "c1aaaaaaaaaa0001" && x.archived === true), "archived");
  await assertEventually(async () => (await side.locator(".item, a, button").filter({ hasText: "Rechnungen Oktober" }).count()) === 0, "gone from the list");

  // The wake choice of a planned task.
  await side.getByRole("button", { name: /Geplant/ }).click();
  await page.getByText("Fotos sortieren").first().waitFor();
  await page.getByRole("button", { name: /Bearbeiten|Fotos sortieren/ }).first().click();
  const wake = page.locator("#plan-wake");
  await wake.waitFor();
  assert.equal(await wake.inputValue(), "", "as the account chose by default");
  await wake.selectOption("never");
  await page.screenshot({ path: `${out}/wecken.png` });
  await page.getByRole("button", { name: "Speichern" }).last().click();
  await assertEventually(() => state.puts.length === 1, "schedule saved");
  assert.equal(state.puts[0].wake, "never");
  await ctx.close();
}

// Phone: the swipe/menu sheet offers „Archivieren“; the Archiv fits the screen.
{
  const { ctx, page, state } = await open(390, 844);
  await page.getByRole("button", { name: /Menü|Chats/ }).first().click().catch(() => {});
  const archiv = page.getByRole("button", { name: /^Archiv\s*1$/ });
  await archiv.waitFor({ timeout: 15000 });
  await archiv.click();
  const dialog = page.getByRole("dialog", { name: "Archiv" });
  await dialog.getByText("Alter Umzugsplan").waitFor();
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "no sideways scrolling");
  await page.waitForTimeout(800);
  const dbox = await dialog.boundingBox();
  assert.ok(dbox && dbox.x >= 0 && dbox.x + dbox.width <= 390, `dialog on the screen: ${JSON.stringify(dbox)}`);
  await page.screenshot({ path: `${out}/archiv-handy.png` });
  await dialog.getByRole("button", { name: "Öffnen" }).click();
  await page.getByText("Alter Umzugsplan").first().waitFor();
  assert.equal(state.patches.length, 0, "opening changes nothing");
  await ctx.close();
}

await browser.close();
assert.deepEqual(errors, [], "browser errors");
console.log("agent archive: ok");
