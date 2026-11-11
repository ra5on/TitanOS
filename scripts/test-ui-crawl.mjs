// Opens every page of a development titand and clicks every button on it, to
// find pages and dialogs that break while rendering. titand -dev changes no
// disks or system settings; use a throwaway -config directory.
import { mkdir, writeFile } from "node:fs/promises";
import { cutTextIn } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5186";
const out = process.env.UI_OUTPUT || "out/validation/ui-crawl";
const only = process.env.UI_ROUTES?.split(",");
await mkdir(out, { recursive: true });

const routes = [
  "/account",
  "/apps",
  "/docker",
  "/docker/apps/web",
  "/docker/networks",
  "/files",
  "/plugins",
  "/plugins/cleanup",
  "/settings",
  "/settings/about",
  "/settings/access",
  "/settings/api",
  "/settings/appearance",
  "/settings/backups",
  "/settings/cluster",
  "/settings/backups/applications",
  "/settings/backups/configuration",
  "/settings/backups/coverage",
  "/settings/backups/data",
  "/settings/backups/restore",
  "/settings/energy",
  "/settings/hardware",
  "/settings/homeassistant",
  "/settings/agent",
  "/agent",
  "/settings/memory",
  "/settings/network",
  "/settings/notifications",
  "/settings/power-button",
  "/settings/stick",
  "/settings/temperatures",
  "/settings/timezone",
  "/settings/updates",
  "/settings/ups",
  "/shares",
  "/storage",
  "/storage/import",
  "/storage/manage",
  "/terminal",
  "/tools",
  "/tools/maintenance",
  "/tools/disks",
  "/tools/bundle",
  "/tools/hardware",
  "/tools/logs",
  "/tools/scripts",
  "/tools/monitoring",
  "/tools/activity",
  "/tools/tasks",
  "/users",
  "/vms",
  "/vms/edit?vm=source",
  "/vms/media",
  "/vms/new",
  "/vms/screen?vm=source",
  "/welcome",
].filter((route) => !only || only.includes(route));

// Buttons that would only leave the page or end the session.
const skip = /^(Abmelden|Log out|NAS neu starten|NAS herunterfahren)/;

const errors = [];
const findings = [];
// Crawls a titand started with -dev (UI_LOGIN=name:password), whose answers
// have the real shapes. The shared fixtures answer unknown paths with {}, so
// pages they were never meant for would look broken.
const login = process.env.UI_LOGIN;
if (!login) {
  console.error(
    "UI_LOGIN=name:password for a titand started with -dev is required",
  );
  process.exit(2);
}
const ctx = await browser.newContext({
  viewport: { width: 1360, height: 900 },
});
// The crawl opens every page alone, as a window of the desktop shows it
// (PLAN 65, lib/desktop/mode.svelte.ts); the desktop itself has
// scripts/test-ui-desktop.mjs.
await ctx.addInitScript(() => localStorage.setItem("titanos.layout", "bare"));
const page = await ctx.newPage();
page.setDefaultTimeout(12000);
page.on("pageerror", (e) => errors.push(String(e)));
{
  const [name, password] = login.split(":");
  await page.goto(base + "/login");
  const status = await page.evaluate(
    async (body) =>
      (
        await fetch("/api/v1/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body,
        })
      ).status,
    JSON.stringify({ name, password }),
  );
  if (status !== 200) throw new Error(`login failed: ${status}`);
}
// Richer data than an empty -dev NAS has, so that forms with sources render.
await ctx.route("**/api/v1/backups", (route) =>
  route.request().method() === "GET"
    ? route.fulfill({
        json: {
          jobs: [],
          shares: [
            { name: "Documents", volume: "Data" },
            { name: "Media", volume: "Data" },
          ],
          volumes: ["Data", "Fast"],
          smbTargets: [],
        },
      })
    : route.fallback(),
);
// PLAN 79-14: several shares with short and long names on the accounts page,
// whose "Shares" row opens to one line per share (79-1: the names stood one
// letter per line there). Only for that page: the shares page would offer to
// change shares titand does not have.
const crawlShares = [
  ["Agent", "nvme"],
  ["Dokumente", "nvme"],
  ["Familienfotos-und-Videos-Archiv-2019-bis-2026", "Datenspeicher-gross"],
  ["Sicherungen_der_Arbeitsrechner_im_Buero", "Data"],
  ["Medien", "Data"],
].map(([name, volume], index) => ({
  name,
  volume,
  smb: true,
  nfs: { enabled: false, clients: [] },
  access: index % 3 ? { [login.split(":")[0]]: index % 2 ? "ro" : "rw" } : {},
  guest: false,
  browseable: true,
  recycle: { enabled: true, days: 30 },
  timeMachine: { enabled: false, sizeGB: 0 },
  path: `/mnt/${volume}/${name}`,
  available: true,
}));
await ctx.route("**/api/v1/shares", (route) =>
  route.request().method() === "GET" &&
  new URL(page.url()).pathname === "/users"
    ? route.fulfill({ json: crawlShares })
    : route.fallback(),
);
const consoleErrors = [];
page.on("console", (m) => {
  if (m.type() === "error" && !/Failed to load resource/.test(m.text()))
    consoleErrors.push(m.text());
});

async function broken() {
  return page
    .locator("main")
    .getByText(/^Internal Error$|^500$/)
    .count()
    .catch(() => 0);
}

// No text may be cut off (docs/rules/WEB-UI.md): on the page as it loads
// and in whatever a click opens (dialog, menu, folded section). Each
// finding is reported once per route.
const cutSeen = new Set();
async function cutText(route, step) {
  const found = await page.evaluate(cutTextIn).catch(() => []);
  for (const item of found) {
    const key = route + " " + item;
    if (cutSeen.has(key)) continue;
    cutSeen.add(key);
    note(route, step, "cut-off text: " + item);
  }
}

function note(route, step, detail) {
  findings.push({ route, step, detail });
  console.log(`FAIL ${route} :: ${step} :: ${detail}`);
}

async function open(route) {
  errors.length = 0;
  consoleErrors.length = 0;
  await page.goto(base + route, { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(700);
}

for (const route of routes) {
  try {
    await crawl(route);
  } catch (e) {
    note(route, "crawler", String(e).split("\n")[0]);
  }
}
async function crawl(route) {
  await open(route);
  if ((await broken()) || errors.length || consoleErrors.length) {
    note(
      route,
      "load",
      [...errors, ...consoleErrors].join(" | ") || "error page",
    );
    return;
  }
  await cutText(route, "load");
  const labels = await page
    .locator("main button:visible")
    .evaluateAll((buttons) =>
      buttons.map((b) =>
        (b.innerText || b.getAttribute("aria-label") || "").trim(),
      ),
    );
  const seen = new Set();
  for (const [index, label] of labels.entries()) {
    if (!label || skip.test(label) || seen.has(label)) continue;
    seen.add(label);
    const button = page.locator("main button:visible").nth(index);
    if ((await button.count()) === 0 || !(await button.isEnabled())) continue;
    errors.length = 0;
    consoleErrors.length = 0;
    try {
      await button.click({ timeout: 2000 });
    } catch {
      continue; // covered by an overlay or gone after an earlier click
    }
    await page.waitForTimeout(400);
    if ((await broken()) || errors.length || consoleErrors.length) {
      note(
        route,
        `click "${label}"`,
        [...errors, ...consoleErrors].join(" | ") || "error page",
      );
      await page.screenshot({ path: `${out}/${findings.length}.png` });
    }
    await cutText(route, `click "${label}"`);
    await page.keyboard.press("Escape");
    if (
      new URL(page.url()).pathname !== new URL(base + route).pathname ||
      (await broken())
    )
      await open(route);
  }
  console.log(`ok ${route} (${seen.size} buttons)`);
}

// PLAN 79-14: the same pages at phone width (390 px), where a control stands
// below its name and a select is as narrow as the screen. Every folded row
// (aria-expanded="false") is opened; nothing else is clicked.
await page.setViewportSize({ width: 390, height: 844 });
for (const route of routes) {
  try {
    await open(route);
    if (await broken()) continue; // reported by the pass above
    const account = page.locator("main [data-account]:visible").first();
    if (route === "/users" && (await account.count())) {
      await account.click();
      await page.waitForTimeout(300);
    }
    const folded = page.locator('main button[aria-expanded="false"]:visible');
    for (let n = 0; n < 40 && (await folded.count()); n++) {
      try {
        await folded.first().click({ timeout: 2000 });
      } catch {
        break;
      }
      await page.waitForTimeout(150);
    }
    await cutText(route, "phone, rows open");
  } catch (e) {
    note(route, "phone", String(e).split("\n")[0]);
  }
}

await writeFile(`${out}/findings.json`, JSON.stringify(findings, null, 2));
await ctx.close();
await browser.close();
console.log(`${findings.length} findings`);
process.exit(findings.length ? 1 : 0);
