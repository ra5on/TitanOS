// PLAN 79-3: a document opened in a web app (the T-programs) scrolls right
// away, without a click into it first: the mouse wheel over the document and
// the keys (Page Down) reach the app after opening from the file manager
// (double click) and after "Datei öffnen …" in the window. A window the
// person moved on to keeps the keyboard.
//
// The REAL T-PDF web build runs, served the way titand serves a trusted app
// (index.html with data-host="page" and host.js, no sandbox).
//
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5280 node scripts/test-ui-webapp-focus.mjs
// The build: T_PDF_WEB_DIR (an extracted zip), else it is downloaded once
// with `gh release download` into out/webapps-cache.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { mkdir } from "node:fs/promises";
import { extname, join, resolve } from "node:path";
import { createServer } from "node:http";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
  args: [
    "--enable-unsafe-swiftshader",
    "--use-angle=swiftshader",
    "--disable-features=LocalNetworkAccessChecks,PrivateNetworkAccessForWorkers,PrivateNetworkAccessForNavigations",
  ],
});
const base = process.env.UI_URL || "http://127.0.0.1:5280";
const out = process.env.UI_OUTPUT || "out/validation/webapp-focus";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const VERSION = process.env.T_PDF_VERSION || "1.4.0";
function webBuild() {
  if (process.env.T_PDF_WEB_DIR) return process.env.T_PDF_WEB_DIR;
  const cache = resolve("out/webapps-cache", "t-pdf");
  const found = () =>
    !existsSync(cache)
      ? undefined
      : readdirSync(cache)
          .map((d) => join(cache, d))
          .find(
            (d) =>
              statSync(d).isDirectory() && existsSync(join(d, "index.html")),
          );
  if (found()) return found();
  try {
    execFileSync("mkdir", ["-p", cache]);
    execFileSync(
      "gh",
      [
        "release",
        "download",
        `v${VERSION}`,
        "-R",
        "ra5on/TitanOS-Office",
        "-p",
        `t-pdf-web-${VERSION}.zip`,
        "-D",
        cache,
        "--clobber",
      ],
      { stdio: "inherit" },
    );
    const zip = readdirSync(cache).find((n) => n.endsWith(".zip"));
    execFileSync("unzip", ["-q", "-o", join(cache, zip), "-d", cache]);
  } catch (e) {
    console.log(`(T-PDF ${VERSION} not available: ${e.message})`);
  }
  return found();
}
const pdfDir = webBuild();
assert.ok(pdfDir, "a T-PDF web build to test with");

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript",
  ".wasm": "application/wasm",
  ".css": "text/css",
  ".json": "application/json",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".ttf": "font/ttf",
  ".otf": "font/otf",
};
const HOST_JS = readFileSync(
  new URL("../web/static/webapps/_host/host.js", import.meta.url),
);
// The app's files as titand serves a trusted app: index.html rewritten
// (webapps.RewriteIndex), no sandbox. Large files come straight from this
// server (the DevTools connection does not survive a big WebAssembly file).
const server = createServer((req, res) => {
  const url = new URL(req.url, "http://x");
  const prefix = `/webapps/t-pdf/${VERSION}/`;
  if (!url.pathname.startsWith(prefix)) {
    res.writeHead(404);
    return res.end();
  }
  const rel =
    decodeURIComponent(url.pathname.slice(prefix.length)) || "index.html";
  const file = join(pdfDir, rel);
  if (rel.includes("..") || !existsSync(file) || statSync(file).isDirectory()) {
    res.writeHead(404);
    return res.end();
  }
  let body = readFileSync(file);
  if (rel === "index.html") {
    let html = body.toString();
    html = html.replace(/<canvas\b/, '<canvas data-host="page"');
    html = html.replace(
      /<script\b/,
      '<script src="/webapps/_host/host.js" data-app="t-pdf" data-bridge="host" data-events="pdfcraft"></script>\n<script',
    );
    body = Buffer.from(html);
  }
  res.writeHead(200, {
    "content-type": TYPES[extname(file)] ?? "application/octet-stream",
    "access-control-allow-origin": "*",
    "cross-origin-resource-policy": "cross-origin",
  });
  res.end(body);
});
await new Promise((ok) => server.listen(0, "127.0.0.1", ok));
const appServer = `http://127.0.0.1:${server.address().port}`;

/** A PDF of several pages, each a coloured band and its number (no fonts embedded). */
function pdf(pages = 8) {
  const objs = [];
  const add = (body) => objs.push(body) && objs.length;
  const catalog = add("");
  const tree = add("");
  const font = add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>");
  const kids = [];
  for (let i = 1; i <= pages; i++) {
    const c = [(i * 37) % 10, (i * 53) % 10, (i * 71) % 10].map((v) => v / 10);
    const stream = `${c.join(" ")} rg 60 ${90 + i * 70} 475 120 re f 0 g BT /F1 96 Tf 240 420 Td (${i}) Tj ET`;
    const content = add(
      `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
    );
    kids.push(
      add(
        `<< /Type /Page /Parent ${tree} 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 ${font} 0 R >> >> /Contents ${content} 0 R >>`,
      ),
    );
  }
  objs[catalog - 1] = `<< /Type /Catalog /Pages ${tree} 0 R >>`;
  objs[tree - 1] =
    `<< /Type /Pages /Count ${pages} /Kids [${kids.map((k) => `${k} 0 R`).join(" ")}] >>`;
  let text = "%PDF-1.4\n";
  const at = [];
  objs.forEach((body, i) => {
    at.push(text.length);
    text += `${i + 1} 0 obj\n${body}\nendobj\n`;
  });
  const xref = text.length;
  text += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n`;
  for (const o of at) text += `${String(o).padStart(10, "0")} 00000 n \n`;
  text += `trailer\n<< /Size ${objs.length + 1} /Root ${catalog} 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(text, "latin1");
}

const MODIFIED = "2026-11-07T09:12:03.123456789+01:00";
const webappsList = () => ({
  volume: "Data",
  apps: [
    {
      id: "t-pdf",
      name: "T-PDF",
      version: VERSION,
      installed: true,
      state: "ready",
      progress: 1,
      bridge: "host",
      types: ["pdf"],
      default: true,
      trusted: true,
      url: `/webapps/t-pdf/${VERSION}/`,
    },
  ],
});

async function open(width = 1600, height = 1000) {
  const made = await create("owner", "graphit", width, "de");
  await made.page.setViewportSize({ width, height });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  const files = {
    "Handbuch.pdf": { bytes: pdf(), modified: MODIFIED },
    "Zweites.pdf": { bytes: pdf(6), modified: MODIFIED },
  };
  made.data.prefs.fileapps = null;
  made.data.prefs.hints = { "webapps-0.63": true };
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const q = url.searchParams;
    if (p === "/webapps") return route.fulfill({ json: webappsList() });
    if (p === "/apps/mine" || (p.startsWith("/apps/") && p.endsWith("/window")))
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found" } },
      });
    if (p.startsWith("/preferences/") && method === "PUT")
      return route.fulfill({ status: 204 });
    if (p === "/files/list" && q.get("root") === "share/Documents") {
      if (q.get("path")) return route.fulfill({ json: [] });
      return route.fulfill({
        json: Object.entries(files).map(([name, f]) => ({
          name,
          dir: false,
          size: f.bytes.length,
          modified: f.modified,
        })),
      });
    }
    if (p === "/files/properties" || p === "/files/download") {
      const f = files[q.get("path")];
      if (!f)
        return route.fulfill({
          status: 404,
          json: { error: { code: "not_found" } },
        });
      if (p === "/files/download")
        return route.fulfill({
          body: f.bytes,
          contentType: "application/octet-stream",
        });
      return route.fulfill({
        json: {
          path: q.get("path"),
          kind: "file",
          size: f.bytes.length,
          modified: f.modified,
        },
      });
    }
    return route.fallback();
  });
  await made.ctx.route("**/webapps/_host/host.js", (route) =>
    route.fulfill({ contentType: "text/javascript", body: HOST_JS }),
  );
  await made.ctx.route(`**/webapps/t-pdf/${VERSION}/**`, async (route) => {
    const url = new URL(route.request().url());
    const rel = decodeURIComponent(
      url.pathname.slice(`/webapps/t-pdf/${VERSION}/`.length),
    );
    const file = join(pdfDir, rel || "index.html");
    if (
      existsSync(file) &&
      statSync(file).isFile() &&
      statSync(file).size > 64e6
    )
      return route.continue({ url: appServer + url.pathname });
    const res = await fetch(appServer + url.pathname);
    return route.fulfill({
      status: res.status,
      headers: Object.fromEntries(res.headers),
      body: Buffer.from(await res.arrayBuffer()),
    });
  });
  made.page.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
  await made.page.goto(base + "/");
  await made.page.waitForSelector(".desktop", { timeout: 15000 });
  await made.page.waitForTimeout(800);
  return made;
}

async function filesWindow(page) {
  await page.locator(".ico", { hasText: "Dateien" }).first().dblclick();
  const frame = page.frameLocator("iframe[data-window]").first();
  await frame
    .locator('[data-file-name="Handbuch.pdf"]')
    .waitFor({ timeout: 15000 });
  return frame;
}

/** The window of T-PDF once the app shows, and its frame. */
async function pdfWindow(page, nth = 0) {
  const win = page
    .locator("section.win", { has: page.locator("iframe[data-webapp=t-pdf]") })
    .nth(nth);
  await win.waitFor({ timeout: 15000 });
  const app = win.locator("iframe[data-webapp=t-pdf]");
  await app.and(page.locator(":not(.hidden)")).waitFor({ timeout: 40000 });
  return { win, app };
}

/** The document is painted and stays as it is (rendering finished). */
async function settled(app, ms = 30000) {
  const until = Date.now() + ms;
  let last = "";
  let same = 0;
  while (Date.now() < until) {
    const now = (await app.screenshot()).toString("base64");
    same = now === last ? same + 1 : 0;
    if (same >= 2) return now;
    last = now;
    await new Promise((r) => setTimeout(r, 500));
  }
  return last;
}

/** Where the keyboard is: the element of the page, and inside a frame its element. */
const where = (page) =>
  page.evaluate(() => {
    const a = document.activeElement;
    let inner = "";
    if (a instanceof HTMLIFrameElement) {
      try {
        inner = ">" + (a.contentDocument?.activeElement?.tagName ?? "?");
      } catch {
        inner = ">x";
      }
    }
    const id =
      a?.getAttribute?.("data-webapp") ?? a?.getAttribute?.("data-app") ?? "";
    return `${a?.tagName}${id ? `[${id}]` : ""}${inner}`;
  });

// FOCUS_REPORT=1 lists every check instead of stopping at the first that fails.
const REPORT = process.env.FOCUS_REPORT === "1";
const results = [];
function check(ok, text) {
  results.push(`${ok ? "ok  " : "FAIL"} ${text}`);
  if (!REPORT) assert.ok(ok, text);
}
/** Does the document move by this action? (The picture of the app changes.) */
async function moves(page, app, action) {
  const before = await settled(app);
  await action();
  await page.waitForTimeout(600);
  return (await settled(app, 8000)) !== before;
}

// 1. Double click in the file manager: wheel and keys scroll at once.
{
  const made = await open();
  const { page } = made;
  const files = await filesWindow(page);
  const row = files.locator('[data-file-name="Handbuch.pdf"]');
  await row.dblclick();
  const { win, app } = await pdfWindow(page);
  await page.waitForFunction(
    () =>
      /Handbuch\.pdf/.test(
        document.querySelector("section.win.focused .titlebar")?.textContent ??
          "",
      ),
    null,
    { timeout: 30000 },
  );
  await settled(app);
  await page.screenshot({ path: `${out}/01-opened.png` });
  // Nobody touched the mouse since the double click: it rests over the new
  // window, where the file's row was.
  const box = await app.boundingBox();
  const at = await row.boundingBox();
  const rest = { x: at.x + at.width / 2, y: at.y + at.height / 2 };
  assert.ok(
    rest.x > box.x + 300 &&
      rest.x < box.x + box.width - 60 &&
      rest.y > box.y + 100 &&
      rest.y < box.y + box.height,
    "the resting pointer lies over the document",
  );
  const focus = await where(page);
  check(
    await moves(page, app, () => page.mouse.wheel(0, 600)),
    `the wheel scrolls right after opening from Files (keyboard: ${focus})`,
  );
  await page.screenshot({ path: `${out}/02-wheel.png` });
  check(
    await moves(page, app, () => page.keyboard.press("PageDown")),
    "Page Down scrolls right after opening from Files",
  );
  check(
    /t-pdf\]>CANVAS/.test(await where(page)),
    `the keyboard is in the app's canvas (${await where(page)})`,
  );

  // 2. Another window in front and back again (dock): keys go to the app;
  //    a click on the window's title bar leaves them there.
  const dock = (name) => page.locator(`.dock button.it[aria-label="${name}"]`);
  await dock("Dateien").click();
  await page.waitForTimeout(300);
  check(
    !/t-pdf/.test(await where(page)),
    `the file manager in front has the keyboard (${await where(page)})`,
  );
  await dock("T-PDF").click();
  await page.waitForTimeout(300);
  check(
    await moves(page, app, () => page.keyboard.press("PageUp")),
    `Page Up scrolls after the window came to the front (keyboard: ${await where(page)})`,
  );
  await win.locator(".titlebar").click({ position: { x: 300, y: 12 } });
  await page.waitForTimeout(300);
  check(
    await moves(page, app, () => page.keyboard.press("PageDown")),
    `Page Down scrolls after a click on the title bar (keyboard: ${await where(page)})`,
  );

  // 3. "Öffnen" inside the program: the window's dialog, then the document
  //    in the same window; keys and the wheel (pointer resting where the
  //    dialog's row was) scroll it.
  await page.mouse.click(box.x + 240, box.y + 18);
  const fd = page.locator("form.fd");
  await fd.waitFor({ timeout: 10000 });
  const pickRow = fd
    .locator(".list .row.file", { hasText: "Zweites.pdf" })
    .first();
  await pickRow.dblclick();
  await fd.waitFor({ state: "detached" });
  await page.waitForTimeout(2500);
  await settled(app);
  await page.screenshot({ path: `${out}/03-opened-in-program.png` });
  const focus3 = await where(page);
  check(
    await moves(page, app, () => page.keyboard.press("PageDown")),
    `Page Down scrolls right after "Öffnen" in the program (keyboard: ${focus3})`,
  );
  check(
    await moves(page, app, () => page.mouse.wheel(0, 600)),
    'the wheel scrolls right after "Öffnen" in the program',
  );

  // 4. "Datei öffnen …" of the window's menu: a window of its own, scrolls at once.
  await win.locator('.titlebar button[aria-label="Mehr"]').click();
  await win.getByRole("menuitem", { name: "Datei öffnen …" }).click();
  await fd
    .locator(".list .row.file", { hasText: "Zweites.pdf" })
    .first()
    .dblclick();
  await fd.waitFor({ state: "detached" });
  const second = await pdfWindow(page, 1);
  await page.waitForFunction(
    () =>
      /Zweites\.pdf/.test(
        document.querySelector("section.win.focused .titlebar")?.textContent ??
          "",
      ),
    null,
    { timeout: 30000 },
  );
  await settled(second.app);
  const focus4 = await where(page);
  check(
    await moves(page, second.app, () => page.keyboard.press("PageDown")),
    `Page Down scrolls right after "Datei öffnen …" (keyboard: ${focus4})`,
  );
  check(
    await moves(page, second.app, () => page.mouse.wheel(0, 600)),
    'the wheel scrolls right after "Datei öffnen …"',
  );
  await made.ctx.close();
}

// 5. Nothing is taken away: the person went on to another window while the
//    program started, or a dialog is open.
{
  const made = await open();
  const { page } = made;
  const files = await filesWindow(page);
  await files.locator('[data-file-name="Handbuch.pdf"]').dblclick();
  const win = page.locator("section.win", {
    has: page.locator("iframe[data-webapp=t-pdf]"),
  });
  await win.waitFor({ timeout: 15000 });
  // Back to the file manager at once, a file chosen there.
  await page.locator('.dock button.it[aria-label="Dateien"]').click();
  await files.locator('[data-file-name="Zweites.pdf"]').click();
  const { app } = await pdfWindow(page);
  await page.waitForTimeout(3000);
  check(
    !/t-pdf/.test(await where(page)),
    `the window the person moved on to keeps the keyboard (${await where(page)})`,
  );
  check(
    await page.evaluate(() =>
      /Durchsuchen/.test(
        document.querySelector("section.win.focused .titlebar")?.textContent ??
          "",
      ),
    ),
    "the file manager stays in front",
  );
  // A dialog over the app: the app's window in front, its "Datei öffnen …" open.
  await page.locator('.dock button.it[aria-label="T-PDF"]').click();
  await win.locator('.titlebar button[aria-label="Mehr"]').click();
  await win.getByRole("menuitem", { name: "Datei öffnen …" }).click();
  const fd = page.locator("form.fd");
  await fd.waitFor();
  await page.waitForTimeout(500);
  check(
    await page.evaluate(() => !!document.activeElement?.closest("dialog")),
    `the dialog keeps the keyboard (${await where(page)})`,
  );
  await page.keyboard.press("Escape");
  await fd.waitFor({ state: "detached" });
  await page.waitForTimeout(300);
  check(
    /t-pdf\]>CANVAS/.test(await where(page)),
    `after the dialog the keyboard is back in the app (${await where(page)})`,
  );
  await settled(app);
  await made.ctx.close();
}

console.log(results.join("\n"));
await browser.close();
server.close();
assert.ok(!results.some((r) => r.startsWith("FAIL")), "all checks");
assert.deepEqual(errors, [], "no page errors");
console.log("webapp focus: ok");
