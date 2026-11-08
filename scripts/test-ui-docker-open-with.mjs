// PLAN 72: „Öffnen mit“ für Docker-Apps. The file manager lists the Docker
// apps titand names for a file under their own heading, opens the file in
// one (the desktop then shows the app's window), asks before a share is
// mounted into an app (Cancel has the focus, Enter mounts nothing), and the
// Docker editor shows the types, the program and the mounted shares.
// Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5240 node scripts/test-ui-docker-open-with.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5240";
const out = process.env.UI_OUTPUT || "out/validation/docker-open-with";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const FILES = [
  { name: "Gehäuse Deckel.3mf", dir: false, size: 2400000, modified: "2026-11-08T09:12:00Z" },
  { name: "Halterung.stl", dir: false, size: 820000, modified: "2026-11-07T18:40:00Z" },
  { name: "Notiz.txt", dir: false, size: 120, modified: "2026-11-06T10:00:00Z" },
];

/** The Docker API of the test: mounted decides what /docker/openers says. */
async function dockerApi(ctx, state) {
  await ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    if (p === "/files/roots")
      return route.fulfill({ json: [{ id: "share/Documents", name: "Documents", kind: "share", writable: true, recycle: true, totalBytes: 1e12, freeBytes: 6e11 }] });
    if (p === "/files/list") return route.fulfill({ json: FILES });
    if (p === "/docker/openers") {
      const path = url.searchParams.get("path") ?? "";
      if (!/\.(3mf|stl)$/.test(path)) return route.fulfill({ json: [] });
      state.asked++;
      const mounted = state.mounted && state.asked > state.readyAfter;
      return route.fulfill({ json: [
        { app: "bambu", name: "Bambu Studio", running: true, mounted, ...(mounted ? {} : { share: "Documents", canMount: state.canMount }) },
        { app: "orca", name: "OrcaSlicer", running: false, mounted: true },
      ] });
    }
    if (p.endsWith("/open") && method === "POST") {
      state.opened.push({ app: p.split("/")[3], ...route.request().postDataJSON() });
      return route.fulfill({ status: 204 });
    }
    if (p === "/docker/apps/bambu/shares" && method === "POST") {
      state.mounts.push(route.request().postDataJSON());
      state.mounted = true;
      state.asked = 0;
      return route.fulfill({ status: 202 });
    }
    if (p === "/preferences/fileappsdocker" && method === "PUT") {
      state.prefs.push(route.request().postDataJSON());
      return route.fulfill({ status: 204 });
    }
    return route.fallback();
  });
}

async function openFiles(width) {
  const phone = width < 500;
  const made = await create("owner", "graphit", width, "de", phone ? { isMobile: true, hasTouch: true, deviceScaleFactor: 2 } : {});
  const { ctx, page } = made;
  await page.setViewportSize({ width, height: phone ? 844 : 900 });
  await ctx.addInitScript(() => {
    localStorage.removeItem("titanos.layout");
    // What windows ask the desktop to open.
    window.__programs = [];
    addEventListener("message", (e) => {
      if (e.data?.type === "titanos-open-program") window.__programs.push(e.data.key);
    });
  });
  const state = { mounted: true, canMount: true, readyAfter: 0, asked: 0, opened: [], mounts: [], prefs: [] };
  await dockerApi(ctx, state);
  await page.goto(base + "/files?root=share/Documents");
  const ui = phone ? page : page.frameLocator("iframe[data-window]").first();
  await ui.locator('[data-file-name="Halterung.stl"]').first().waitFor({ timeout: 15000 });
  return { ...made, ui, state, phone };
}

async function openWithMenu(ui, name) {
  await ui.locator(`[data-file-name="${name}"]`).first().click({ button: "right" });
  const menu = ui.getByRole("menu");
  await menu.getByRole("menuitem", { name: "Öffnen mit" }).click();
  return menu;
}

// 1. The menu: Docker apps under their own heading; opening one.
{
  const { ctx, page, ui, state } = await openFiles(1440);
  const menu = await openWithMenu(ui, "Halterung.stl");
  await menu.getByText("Docker-Apps", { exact: true }).waitFor();
  await menu.getByRole("menuitem", { name: /OrcaSlicer/ }).waitFor();
  await page.locator("section.win").first().screenshot({ path: `${out}/menue.png` });
  await menu.getByRole("menuitem", { name: /Bambu Studio/ }).click();
  await assertEventually(() => state.opened.length === 1, "opened");
  assert.deepEqual(state.opened[0], { app: "bambu", root: "share/Documents", path: "Halterung.stl" });
  await assertEventually(async () => (await page.evaluate(() => window.__programs)).includes("docker:bambu"), "window of the app");
  // A file no Docker app opens has no Docker heading.
  await ui.locator('[data-file-name="Notiz.txt"]').first().click({ button: "right" });
  await page.waitForTimeout(400);
  assert.equal(await ui.getByText("Docker-Apps", { exact: true }).count(), 0);
  await ctx.close();
}

// 2. Not mounted yet: the question, Enter mounts nothing, then mount and open.
{
  const { ctx, page, ui, state } = await openFiles(1440);
  state.mounted = false;
  state.readyAfter = 1; // the first answer after mounting is "not yet"
  const menu = await openWithMenu(ui, "Gehäuse Deckel.3mf");
  await menu.getByText("Documents ist dort noch nicht eingehängt").waitFor();
  await menu.getByRole("menuitem", { name: /Bambu Studio/ }).click();
  const dialog = ui.getByRole("dialog", { name: "Documents in Bambu Studio einhängen?" });
  await dialog.waitFor();
  await dialog.getByText("alle Dateien in Documents", { exact: false }).waitFor();
  assert.equal(await dialog.getByRole("button", { name: "Abbrechen" }).evaluate((b) => b === document.activeElement), true, "Cancel has the focus");
  await page.keyboard.press("Enter");
  await page.waitForTimeout(300);
  assert.equal(state.mounts.length, 0, "Enter mounted the share");
  await openWithMenu(ui, "Gehäuse Deckel.3mf").then((m) => m.getByRole("menuitem", { name: /Bambu Studio/ }).click());
  await dialog.waitFor();
  await dialog.getByRole("checkbox", { name: "Nur lesen" }).check();
  await page.locator("section.win").first().screenshot({ path: `${out}/rueckfrage.png` });
  await dialog.getByRole("button", { name: "Einhängen und öffnen" }).click();
  await assertEventually(() => state.opened.length === 1, "opened after mounting", 15000);
  assert.deepEqual(state.mounts, [{ share: "Documents", readOnly: true }]);
  assert.equal(state.opened[0].path, "Gehäuse Deckel.3mf");
  await dialog.waitFor({ state: "detached" });
  await ctx.close();
}

// 3. A plain user cannot mount: a message instead of the question.
{
  const { ctx, ui, state } = await openFiles(1440);
  state.mounted = false;
  state.canMount = false;
  const menu = await openWithMenu(ui, "Gehäuse Deckel.3mf");
  await menu.getByRole("menuitem", { name: /Bambu Studio/ }).click();
  await ui.getByText("Ein Administrator muss die Freigabe Documents in Bambu Studio einhängen.").waitFor();
  assert.equal(await ui.getByRole("dialog").count(), 0);
  assert.equal(state.opened.length, 0);
  await ctx.close();
}

// 4. „Immer mit … öffnen“ makes it the standard; a double click then opens it.
{
  const { ctx, ui, state } = await openFiles(1440);
  const menu = await openWithMenu(ui, "Halterung.stl");
  await menu.getByRole("checkbox").check();
  await menu.getByRole("menuitem", { name: /OrcaSlicer/ }).click();
  await assertEventually(() => state.prefs.length === 1 && state.opened.length === 1, "standard saved");
  assert.deepEqual(state.prefs[0], { stl: "docker:orca" });
  await ui.locator('[data-file-name="Halterung.stl"]').first().dblclick();
  await assertEventually(() => state.opened.length === 2, "double click");
  assert.equal(state.opened[1].app, "orca");
  await ctx.close();
}

// 5. Phone: the menu fits, nothing scrolls sideways.
{
  const { ctx, page, ui } = await openFiles(390);
  const menu = await openWithMenu(ui, "Gehäuse Deckel.3mf");
  await menu.getByText("Docker-Apps", { exact: true }).waitFor();
  const box = await menu.boundingBox();
  assert.ok(box && box.x >= 0 && box.x + box.width <= 390, `menu inside the screen: ${JSON.stringify(box)}`);
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "no sideways scrolling");
  await page.screenshot({ path: `${out}/handy.png` });
  await ctx.close();
}

// 6. Docker › Bearbeiten: types, program and the mounted shares.
{
  const made = await create("owner", "graphit", 1440, "de");
  const { ctx, page } = made;
  await page.setViewportSize({ width: 1440, height: 1000 });
  const puts = [];
  const removed = [];
  const compose = "x-titanos:\n  name: Bambu Studio\n  main: bambustudio\nservices:\n  bambustudio:\n    image: lscr.io/linuxserver/bambustudio:latest\n";
  await ctx.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    const method = route.request().method();
    if (p === "/docker/apps/bambu" && method === "GET") return route.fulfill({ json: { compose } });
    if (p === "/docker/apps/bambu/opens" && method === "GET")
      return route.fulfill({ json: { app: "bambu", name: "Bambu Studio", types: ["3mf", "stl"], command: ["bambu-studio", "{file}"], user: "abc", service: "bambustudio", running: true,
        mounts: [{ source: "/mnt/Data/appdata/bambu/config", target: "/config", readOnly: false }, { source: "/mnt/Data/Documents", target: "/shares/Documents", readOnly: false }] } });
    if (p === "/docker/apps/bambu/opens" && method === "PUT") {
      puts.push(route.request().postDataJSON());
      return route.fulfill({ status: 202 });
    }
    if (p === "/docker/apps/bambu/shares/Documents" && method === "DELETE") {
      removed.push(p);
      return route.fulfill({ status: 202 });
    }
    return route.fallback();
  });
  await page.goto(base + "/docker/apps/bambu");
  const ui = (await page.locator("iframe[data-window]").count()) ? page.frameLocator("iframe[data-window]").first() : page;
  await ui.getByText("Dateien öffnen", { exact: true }).first().waitFor({ timeout: 15000 });
  await ui.getByText(".3mf").waitFor();
  assert.equal(await ui.locator("#opens-command").inputValue(), "bambu-studio {file}");
  await ui.locator("#opens-type").fill("step");
  await ui.locator("#opens-type").press("Enter");
  // A shell line is refused before it is sent.
  await ui.locator("#opens-command").fill("sh -c open");
  assert.equal(await ui.getByRole("button", { name: "Speichern" }).isDisabled(), true);
  await ui.locator("#opens-command").fill("bambu-studio {file}");
  await ui.getByRole("button", { name: "Speichern" }).click();
  await assertEventually(() => puts.length === 1, "opens saved");
  assert.deepEqual(puts[0].types, ["3mf", "stl", "step"]);
  assert.deepEqual(puts[0].command, ["bambu-studio", "{file}"]);
  assert.equal(puts[0].user, "abc");
  await ui.getByText("/shares/Documents · lesen und schreiben").waitFor();
  await ui.getByRole("button", { name: "Entfernen", exact: true }).click();
  const dialog = ui.getByRole("dialog", { name: "Documents aus der App entfernen?" });
  await dialog.waitFor();
  assert.equal(await dialog.getByRole("button", { name: "Abbrechen" }).evaluate((b) => b === document.activeElement), true);
  await dialog.getByRole("button", { name: "Entfernen", exact: true }).click();
  await assertEventually(() => removed.length === 1, "share removed");
  await page.screenshot({ path: `${out}/editor.png`, fullPage: false });
  await ctx.close();
}

async function assertEventually(check, what, ms = 8000) {
  const until = Date.now() + ms;
  while (Date.now() < until) {
    if (await check()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  assert.fail(`timed out: ${what}`);
}

await browser.close();
assert.deepEqual(errors, [], "browser errors");
console.log("docker open with: ok");
