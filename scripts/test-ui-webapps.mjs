// PLAN 66: web apps directly in the browser (docs/WEBAPPS.md). Isolated API
// data for the NAS (web app list, files, preferences) and REAL Craft web
// builds: WordCraft (bridge "host") and VectorCraft (no bridge: adapter) are
// served the way titand serves them (CSP sandbox, ACAO *, index.html with
// data-host="page" and host.js), so open and save run through the real apps.
//
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5280 node scripts/test-ui-webapps.mjs
// The builds: WORDCRAFT_WEB_DIR / VECTORCRAFT_WEB_DIR (extracted zips), else
// they are downloaded once with `gh release download` into out/webapps-cache.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { mkdir } from "node:fs/promises";
import { extname, join, resolve } from "node:path";
import { createServer } from "node:http";
import { crc32 } from "node:zlib";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
// Software WebGL: the Craft apps need WebGL2 (WebGPU only over HTTPS).
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
  // The test's own answers have no network address; Chromium's local network
  // access would then refuse the large files from the script's server on
  // 127.0.0.1 (on the NAS page, app and files all come from the NAS).
  args: [
    "--enable-unsafe-swiftshader",
    "--use-angle=swiftshader",
    "--disable-features=LocalNetworkAccessChecks,PrivateNetworkAccessForWorkers,PrivateNetworkAccessForNavigations",
  ],
});
const base = process.env.UI_URL || "http://127.0.0.1:5280";
const out = process.env.UI_OUTPUT || "out/validation/webapps";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

// --- The real web builds -----------------------------------------------------

function webBuild(env, repo, tag, pattern) {
  if (process.env[env]) return process.env[env];
  const cache = resolve("out/webapps-cache", repo.split("/")[1]);
  // The zip has its files at the top or in one folder.
  const found = () =>
    !existsSync(cache)
      ? undefined
      : existsSync(join(cache, "index.html"))
        ? cache
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
        tag,
        "-R",
        repo,
        "-p",
        pattern,
        "-D",
        cache,
        "--clobber",
      ],
      {
        stdio: "inherit",
      },
    );
    const zip = readdirSync(cache).find((n) => n.endsWith(".zip"));
    execFileSync("unzip", ["-q", "-o", join(cache, zip), "-d", cache]);
  } catch (e) {
    console.log(`(${repo} ${tag} not available: ${e.message})`);
  }
  return found();
}
const wordcraftDir = webBuild(
  "WORDCRAFT_WEB_DIR",
  "storytold/wordcraft",
  "v0.16.0",
  "wordcraft-web-*.zip",
);
const vectorcraftDir = webBuild(
  "VECTORCRAFT_WEB_DIR",
  "storytold/vectorcraft",
  "v0.22.0",
  "vectorcraft-web-*.zip",
);
assert.ok(wordcraftDir, "a WordCraft web build to test with");

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript",
  ".mjs": "text/javascript",
  ".wasm": "application/wasm",
  ".css": "text/css",
  ".json": "application/json",
  ".ico": "image/x-icon",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".txt": "text/plain; charset=utf-8",
  ".ttf": "font/ttf",
  ".otf": "font/otf",
  ".woff2": "font/woff2",
};
/** The headers of docs/WEBAPPS.md for the app's files. */
const APP_HEADERS = {
  "content-security-policy":
    "sandbox allow-scripts allow-downloads allow-modals allow-popups; frame-ancestors 'self'",
  "x-content-type-options": "nosniff",
  "cross-origin-resource-policy": "cross-origin",
  "access-control-allow-origin": "*",
};
/**
 * The app files come from a small server of this script, index.html rewritten
 * like titand does it. The browser fetches them from there itself (the route
 * only changes the address): a 90 MB WebAssembly file through the route's own
 * answer would go through the DevTools connection, which does not survive it.
 */
const apps = new Map();
// host.js as titand serves it (a file of the web UI), here from the same server.
const HOST_JS = readFileSync(
  new URL("../web/static/webapps/_host/host.js", import.meta.url),
);
const server = createServer((req, res) => {
  const url = new URL(req.url, "http://x");
  if (url.pathname === "/webapps/_host/0/host.js") {
    res.writeHead(200, {
      "content-type": "text/javascript",
      "cross-origin-resource-policy": "cross-origin",
    });
    return res.end(HOST_JS);
  }
  const m = /^\/webapps\/([a-z0-9-]+)\/([^/]+)\/(.*)$/.exec(url.pathname);
  const dir = m && apps.get(`${m[1]}/${m[2]}`);
  const rel = m ? decodeURIComponent(m[3]) || "index.html" : "";
  const file = dir ? join(dir, rel) : "";
  if (
    !dir ||
    rel.includes("..") ||
    !existsSync(file) ||
    statSync(file).isDirectory()
  ) {
    res.writeHead(404, APP_HEADERS);
    return res.end();
  }
  let body = readFileSync(file);
  if (rel === "index.html") {
    let html = body.toString();
    html = html.replace(
      /<canvas id="([a-z]+_canvas)"/,
      '<canvas id="$1" data-host="page"',
    );
    const bridge = apps.get(`${m[1]}/${m[2]}:bridge`);
    if (bridge === "adapter") html = html.replace(' data-host="page"', "");
    html = html.replace(
      /<script type="module">/,
      `<script src="/webapps/_host/host.js" data-app="${m[1]}" data-bridge="${bridge}"></script>\n<script type="module">`,
    );
    body = Buffer.from(html);
  }
  res.writeHead(200, {
    ...APP_HEADERS,
    "content-type": TYPES[extname(file)] ?? "application/octet-stream",
  });
  res.end(body);
});
await new Promise((ok) => server.listen(0, "127.0.0.1", ok));
const appServer = `http://127.0.0.1:${server.address().port}`;

async function serveApp(ctx, id, version, dir, bridge) {
  apps.set(`${id}/${version}`, dir);
  apps.set(`${id}/${version}:bridge`, bridge);
  await ctx.route(`**/webapps/${id}/${version}/**`, async (route) => {
    const url = new URL(route.request().url());
    const rel = decodeURIComponent(
      url.pathname.slice(`/webapps/${id}/${version}/`.length),
    );
    const file = join(dir, rel || "index.html");
    // Large files straight from the server (the document keeps its address,
    // so "frame-ancestors 'self'" holds); everything else as the server answers it.
    if (
      existsSync(file) &&
      statSync(file).isFile() &&
      statSync(file).size > 16e6
    )
      return route.continue({ url: appServer + url.pathname });
    const res = await fetch(appServer + url.pathname);
    return route.fulfill({
      status: res.status,
      headers: Object.fromEntries(res.headers),
      body: Buffer.from(await res.arrayBuffer()),
    });
  });
}

// --- Test files ---------------------------------------------------------------

/** A minimal .docx (stored zip) with a heading and a paragraph. */
function docx() {
  const parts = {
    "[Content_Types].xml":
      '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>',
    "_rels/.rels":
      '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>',
    "word/document.xml":
      '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:rPr><w:b/><w:sz w:val="56"/></w:rPr><w:t>Angebot Gartenhaus</w:t></w:r></w:p><w:p><w:r><w:t>Sehr geehrte Damen und Herren, vielen Dank für Ihre Anfrage.</w:t></w:r></w:p></w:body></w:document>',
  };
  const local = [];
  const central = [];
  let offset = 0;
  for (const [name, text] of Object.entries(parts)) {
    const data = Buffer.from(text);
    const n = Buffer.from(name);
    const crc = crc32(data);
    const head = Buffer.alloc(30);
    head.writeUInt32LE(0x04034b50, 0);
    head.writeUInt16LE(20, 4);
    head.writeUInt32LE(crc, 14);
    head.writeUInt32LE(data.length, 18);
    head.writeUInt32LE(data.length, 22);
    head.writeUInt16LE(n.length, 26);
    local.push(head, n, data);
    const c = Buffer.alloc(46);
    c.writeUInt32LE(0x02014b50, 0);
    c.writeUInt16LE(20, 4);
    c.writeUInt16LE(20, 6);
    c.writeUInt32LE(crc, 16);
    c.writeUInt32LE(data.length, 20);
    c.writeUInt32LE(data.length, 24);
    c.writeUInt16LE(n.length, 28);
    c.writeUInt32LE(offset, 42);
    central.push(c, n);
    offset += 30 + n.length + data.length;
  }
  const cd = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(Object.keys(parts).length, 8);
  end.writeUInt16LE(Object.keys(parts).length, 10);
  end.writeUInt32LE(cd.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...local, cd, end]);
}
/** A small PNG with a colour gradient. */
function png() {
  const w = 64;
  const h = 48;
  const rows = [];
  for (let y = 0; y < h; y++) {
    rows.push(0);
    for (let x = 0; x < w; x++) rows.push((x * 4) % 256, (y * 5) % 256, 128);
  }
  const chunk = (type, data) => {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const td = Buffer.concat([Buffer.from(type), data]);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(crc32(td));
    return Buffer.concat([len, td, crc]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr.writeUInt8(8, 8);
  ihdr.writeUInt8(2, 9);
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", require_deflate(Buffer.from(rows))),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}
import { deflateSync as require_deflate } from "node:zlib";

const MODIFIED = "2026-11-07T09:12:03.123456789+01:00";
/** The time (ns) the mock answers for its n-th write: 10:00:<n>.5 +01:00. */
const savedNs = (n) =>
  String(
    BigInt(Date.UTC(2026, 10, 7, 9, 0, n) / 1000) * 1000000000n + 500000000n,
  );
// The same instant in ns since 1970 (what the window sends as expect).
const MODIFIED_NS = "1794039123123456789";

// --- The NAS -------------------------------------------------------------------

const webappsList = (_made, vector = !!vectorcraftDir) => ({
  volume: "Data",
  apps: [
    {
      id: "wordcraft",
      name: "WordCraft",
      version: "0.16.0",
      installed: true,
      state: "ready",
      progress: 1,
      bridge: "host",
      types: ["docx", "doc", "odt", "rtf"],
      default: true,
      url: "/webapps/wordcraft/0.16.0/",
    },
    {
      id: "gridcraft",
      name: "GridCraft",
      version: "0.17.0",
      installed: false,
      state: "missing",
      progress: 0,
      bridge: "host",
      types: ["xlsx", "xls", "ods", "csv"],
      default: true,
      url: "",
    },
    {
      id: "pdfcraft",
      name: "PdfCraft",
      version: "0.19.0",
      installed: false,
      state: "downloading",
      progress: 0.4,
      bridge: "host",
      types: ["pdf"],
      default: true,
      url: "",
    },
    {
      id: "vectorcraft",
      name: "VectorCraft",
      version: "0.22.0",
      installed: vector,
      state: vector ? "ready" : "missing",
      progress: vector ? 1 : 0,
      bridge: "adapter",
      types: ["svg", "ai"],
      default: false,
      url: vector ? "/webapps/vectorcraft/0.22.0/" : "",
    },
  ],
});

async function open(
  width = 1600,
  height = 1000,
  path = "/",
  { phone = false, offer = false, list = webappsList } = {},
) {
  const made = await create(
    "owner",
    "graphit",
    width,
    "de",
    phone ? { isMobile: true, hasTouch: true, deviceScaleFactor: 2 } : {},
  );
  await made.page.setViewportSize({ width, height });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  const nas = {
    files: {
      "Angebot.docx": { bytes: docx(), modified: MODIFIED },
      // A Mac's companion file (PLAN 69): never offered to open.
      "._Angebot.docx": { bytes: Buffer.alloc(4096), modified: MODIFIED },
      "bild.png": { bytes: png(), modified: MODIFIED },
      "logo.svg": {
        bytes: Buffer.from(
          '<svg xmlns="http://www.w3.org/2000/svg" width="200" height="120" viewBox="0 0 200 120"><rect x="10" y="10" width="180" height="100" rx="16" fill="#3b82f6"/><circle cx="60" cy="60" r="30" fill="#f59e0b"/></svg>',
        ),
        modified: MODIFIED,
      },
      "Notizen.txt": { bytes: Buffer.from("Hallo"), modified: MODIFIED },
    },
    puts: [],
    conflict: false,
    fileapps: null,
  };
  made.nas = nas;
  made.data.prefs.fileapps = null;
  // The offer after the update was answered, except where it is tested.
  made.data.prefs.hints = offer ? null : { "webapps-0.63": true };
  made.posts = [];
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const q = url.searchParams;
    if (p === "/webapps") return route.fulfill({ json: list(made) });
    if (p.startsWith("/webapps/") && method === "POST") {
      made.posts.push({ path: p, body: route.request().postDataJSON() });
      return route.fulfill({ status: 202, json: list(made) });
    }
    if (p === "/preferences/hints" && method === "PUT") {
      made.data.prefs.hints = route.request().postDataJSON();
      return route.fulfill({ status: 204 });
    }
    if (p === "/apps/mine" || (p.startsWith("/apps/") && p.endsWith("/window")))
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found" } },
      });
    if (p === "/preferences/fileapps" && method === "PUT") {
      nas.fileapps = route.request().postDataJSON();
      made.data.prefs.fileapps = nas.fileapps;
      return route.fulfill({ status: 204 });
    }
    if (p === "/files/list" && q.get("root") === "share/Documents") {
      const dir = q.get("path") ?? "";
      if (dir) return route.fulfill({ json: [] });
      return route.fulfill({
        json: Object.entries(nas.files).map(([name, f]) => ({
          name,
          dir: false,
          size: f.bytes.length,
          modified: f.modified,
        })),
      });
    }
    if (p === "/files/properties") {
      const f = nas.files[q.get("path")];
      if (!f)
        return route.fulfill({
          status: 404,
          json: { error: { code: "not_found" } },
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
    if (p === "/files/download") {
      const f = nas.files[q.get("path")];
      if (!f)
        return route.fulfill({
          status: 404,
          json: { error: { code: "not_found" } },
        });
      return route.fulfill({
        body: f.bytes,
        contentType: "application/octet-stream",
      });
    }
    if (p === "/files/content" && method === "PUT") {
      const body = route.request().postDataBuffer() ?? Buffer.alloc(0);
      const put = {
        root: q.get("root"),
        path: q.get("path"),
        expect: q.get("expect"),
        size: body.length,
        head: [...body.subarray(0, 4)],
      };
      nas.puts.push(put);
      const f = nas.files[put.path];
      if (put.expect === "none" && f)
        return route.fulfill({
          status: 409,
          json: { error: { code: "file_exists" } },
        });
      if (nas.conflict && put.expect !== "none") {
        nas.conflict = false;
        return route.fulfill({
          status: 409,
          json: { error: { code: "file_changed" } },
        });
      }
      // Every write gets a time of its own (second = number of the write).
      const n = nas.puts.length;
      const modified = `2026-11-07T10:00:${String(n).padStart(2, "0")}.5+01:00`;
      put.answer = savedNs(n);
      nas.files[put.path] = { bytes: body, modified };
      if (nas.delay) await new Promise((r) => setTimeout(r, nas.delay));
      return route.fulfill({
        json: { modified, mtime: put.answer, size: body.length },
      });
    }
    return route.fallback();
  });
  // host.js from the same server as the app (on the NAS both come from titand).
  await made.ctx.route("**/webapps/_host/host.js", async (route) => {
    const res = await fetch(appServer + "/webapps/_host/0/host.js");
    return route.fulfill({
      headers: Object.fromEntries(res.headers),
      body: Buffer.from(await res.arrayBuffer()),
    });
  });
  await serveApp(made.ctx, "wordcraft", "0.16.0", wordcraftDir, "host");
  if (vectorcraftDir)
    await serveApp(
      made.ctx,
      "vectorcraft",
      "0.22.0",
      vectorcraftDir,
      "adapter",
    );
  made.page.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
  await made.page.goto(base + path);
  await made.page.waitForSelector(".desktop, .home, main", { timeout: 15000 });
  await made.page.waitForTimeout(800);
  return made;
}
const shot = (page, name) => page.screenshot({ path: `${out}/${name}.png` });

/** How many colours a picture of the element has (a blank frame has one or two). */
async function colours(page, locator) {
  const png = (await locator.screenshot()).toString("base64");
  return page.evaluate(async (data) => {
    const img = new Image();
    img.src = "data:image/png;base64," + data;
    await img.decode();
    const c = document.createElement("canvas");
    c.width = img.width;
    c.height = img.height;
    const g = c.getContext("2d");
    g.drawImage(img, 0, 0);
    const px = g.getImageData(0, 0, c.width, c.height).data;
    const seen = new Set();
    for (let i = 0; i < px.length; i += 4 * 7)
      seen.add((px[i] >> 3) * 1024 + (px[i + 1] >> 3) * 32 + (px[i + 2] >> 3));
    return seen.size;
  }, png);
}

const waitPut = async (nas, count, ms = 20000) => {
  const until = Date.now() + ms;
  while (nas.puts.length < count && Date.now() < until)
    await new Promise((r) => setTimeout(r, 200));
  return nas.puts[count - 1];
};

/** The file manager in its window. */
async function filesWindow(page) {
  await page.locator(".ico", { hasText: "Dateien" }).first().dblclick();
  const frame = page.frameLocator("iframe[data-window]").first();
  await frame
    .locator('[data-file-name="Angebot.docx"]')
    .waitFor({ timeout: 15000 });
  return frame;
}

// 1. WordCraft from the file manager: double click opens the standard app,
//    the document shows, Ctrl+S writes the original with expect.
{
  const made = await open();
  const { page, nas } = made;
  const files = await filesWindow(page);
  await files.locator('[data-file-name="Angebot.docx"]').click();
  await files.locator('[data-file-name="Angebot.docx"]').dblclick();
  const win = page.locator("section.win", {
    has: page.locator("iframe[data-webapp=wordcraft]"),
  });
  await win.waitFor({ timeout: 15000 });
  // Startup splash with the app's name (draft 01).
  await shot(page, "01-start");
  const app = win.locator("iframe[data-webapp=wordcraft]");
  await app.and(page.locator(":not(.hidden)")).waitFor({ timeout: 60000 });
  await page.waitForTimeout(2500);
  const title = await win.locator(".titlebar").innerText();
  assert.match(title, /WordCraft/, "title names the app");
  assert.match(title, /Angebot\.docx/, "title names the document");
  assert.equal(
    await win.locator(".titlebar .addr").count(),
    0,
    "no address in the title bar",
  );
  // The app painted the document: many colours (text, ribbon), not a blank frame.
  const painted = await colours(page, app);
  assert.ok(painted > 40, `app canvas painted (${painted} colours)`);
  await shot(page, "02-open");

  // ⋯ menu (draft 02).
  await win.locator('.titlebar button[aria-label="Mehr"]').click();
  await win.locator(".win-menu").waitFor();
  for (const item of [
    "Datei öffnen …",
    "Speichern unter …",
    "In eigenem Tab öffnen",
    "App neu starten",
  ]) {
    const entry = win.getByRole("menuitem", { name: item });
    // On top: the point in its middle hits the entry itself (not the app below).
    const hit = await entry.evaluate((el) => {
      const r = el.getBoundingClientRect();
      return el.contains(
        document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2),
      );
    });
    assert.ok(hit, `menu entry visible on top: ${item}`);
  }
  await shot(page, "03-menu");
  // By keyboard like every menu: the first entry has the focus, Escape closes.
  assert.equal(
    await page.evaluate(() => document.activeElement?.textContent?.trim()),
    "Datei öffnen …",
    "first entry focused",
  );
  await page.keyboard.press("ArrowDown");
  assert.equal(
    await page.evaluate(() => document.activeElement?.textContent?.trim()),
    "Speichern unter …",
  );
  await page.keyboard.press("Escape");
  await win.locator(".win-menu").waitFor({ state: "detached" });
  // PLAN 69: "Datei öffnen …" lists the document, not the Mac's "._" file.
  await win.locator('.titlebar button[aria-label="Mehr"]').click();
  await win.getByRole("menuitem", { name: "Datei öffnen …" }).click();
  const fd = page.locator("form.fd");
  await fd.locator(".row.file", { hasText: "Angebot.docx" }).first().waitFor();
  assert.deepEqual(
    await fd.locator(".row.file .name").allTextContents(),
    ["Angebot.docx"],
    "only the document, no companion file",
  );
  await page.keyboard.press("Escape");
  await fd.waitFor({ state: "detached" });

  // Typing makes it dirty; Ctrl+S saves into the original.
  const box = await app.boundingBox();
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await page.keyboard.type("Hallo ");
  await page.waitForTimeout(400);
  await page.keyboard.press("Control+s");
  const put = await waitPut(nas, 1);
  assert.ok(put, "save reached PUT /files/content");
  assert.equal(put.root, "share/Documents");
  assert.equal(put.path, "Angebot.docx");
  assert.equal(put.expect, MODIFIED_NS, "expect = mtime (ns) when opened");
  assert.deepEqual(
    put.head,
    [0x50, 0x4b, 0x03, 0x04],
    "the document's bytes (zip)",
  );
  assert.ok(put.size > 1000);

  // Two saves close together (the NAS answers slowly): the second waits for
  // the first and expects its time – no question about a change meanwhile.
  nas.delay = 400;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await page.keyboard.type("Eins ");
  await page.keyboard.press("Control+s");
  await page.keyboard.type("Zwei ");
  await page.keyboard.press("Control+s");
  const quick = await waitPut(nas, 3);
  assert.ok(quick, "both quick saves written");
  assert.equal(
    nas.puts[1].expect,
    nas.puts[0].answer,
    "first quick save expects the last write",
  );
  assert.equal(
    quick.expect,
    nas.puts[1].answer,
    "second quick save expects the first one's time",
  );
  await page.waitForTimeout(500);
  assert.equal(
    await page
      .getByRole("dialog", { name: "Datei wurde inzwischen geändert" })
      .count(),
    0,
    "no conflict for own saves",
  );
  nas.delay = 0;

  // Changed meanwhile: 409 → the question (Overwrite / Copy / Cancel).
  nas.conflict = true;
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await page.keyboard.type("Noch ");
  await page.keyboard.press("Control+s");
  await page
    .getByRole("dialog", { name: "Datei wurde inzwischen geändert" })
    .waitFor({ timeout: 15000 });
  const second = nas.puts[3];
  assert.equal(
    second.expect,
    nas.puts[2].answer,
    "next save expects the time of the last write",
  );
  await shot(page, "04-conflict");
  await page.getByRole("button", { name: "Als Kopie speichern" }).click();
  const copy = await waitPut(nas, 5);
  assert.equal(copy.path, "Angebot (Kopie).docx");
  assert.equal(copy.expect, "none");
  await page.waitForFunction(
    () =>
      /Angebot \(Kopie\)\.docx/.test(
        document.querySelector("section.win.focused .titlebar")?.textContent ??
          "",
      ),
    null,
    { timeout: 10000 },
  );

  // Save As in the app → the save dialog of TitanOS, name filled in.
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  // WordCraft follows Word: F12 is Save As.
  await page.keyboard.press("F12");
  await page.waitForTimeout(1500);
  await shot(page, "dbg-saveas");
  const dialog = page.getByRole("dialog", { name: /Speichern aus WordCraft/ });
  await dialog.waitFor({ timeout: 15000 });
  const field = dialog.getByLabel("Dateiname");
  assert.match(await field.inputValue(), /\.docx$/, "name filled in");
  await shot(page, "05-save-as");
  await field.fill("Angebot 2.docx");
  await dialog.getByRole("button", { name: "Speichern" }).click();
  const saveAs = await waitPut(nas, 6);
  assert.equal(saveAs.path, "Angebot 2.docx");
  assert.equal(saveAs.expect, "none");

  // Unsaved changes: closing asks.
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  await page.keyboard.type("x");
  await page.waitForTimeout(600);
  await win.locator('.titlebar button[aria-label="Schließen"]').click();
  await page
    .getByRole("dialog", { name: "Ungespeicherte Änderungen" })
    .waitFor();
  await shot(page, "06-unsaved");
  await page.getByRole("button", { name: "Abbrechen" }).click();
  assert.equal(await win.count(), 1, "window stays");

  // Desktop address stays "/" for a web app window.
  assert.equal(new URL(page.url()).pathname, "/");
  await made.ctx.close();
}

// 2. "Öffnen mit" in the file manager (draft 03), the standard app and the
//    adapter (VectorCraft has no bridge: open = dropped file, save = caught download).
{
  const made = await open();
  const { page, nas } = made;
  const files = await filesWindow(page);
  await files.locator('[data-file-name="logo.svg"]').click({ button: "right" });
  await files.getByRole("menuitem", { name: "Öffnen mit" }).click();
  await shot(page, "07-open-with");
  if (vectorcraftDir) {
    assert.ok(
      await files.getByRole("menuitem", { name: /VectorCraft/ }).count(),
      "VectorCraft listed",
    );
    await files.getByLabel(/Immer mit VectorCraft öffnen/).check();
    await files.getByRole("menuitem", { name: /VectorCraft/ }).click();
    await page.waitForTimeout(300);
    assert.deepEqual(
      nas.fileapps,
      { svg: "webapp:vectorcraft" },
      "standard app stored",
    );
    const win = page.locator("section.win", {
      has: page.locator("iframe[data-webapp=vectorcraft]"),
    });
    await win.waitFor({ timeout: 15000 });
    const app = win.locator("iframe[data-webapp=vectorcraft]");
    await page
      .locator("iframe[data-webapp=vectorcraft]:not(.hidden)")
      .waitFor({ timeout: 90000 });
    await page.waitForTimeout(4000);
    await shot(page, "08-adapter-open");
    const box = await app.boundingBox();
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    await page.keyboard.press("Control+s");
    // VectorCraft asks for name and format itself first (its own dialog): OK.
    await page.waitForTimeout(1500);
    await page.keyboard.press("Enter");
    // No bridge: the download is caught and TitanOS asks where to keep it.
    const dialog = page.getByRole("dialog", {
      name: /Speichern aus VectorCraft/,
    });
    await dialog.waitFor({ timeout: 20000 });
    await shot(page, "09-adapter-save");
    assert.equal(
      await dialog.getByLabel("Dateiname").inputValue(),
      "logo.vectorcraft",
      "the app's name",
    );
    await dialog.getByRole("button", { name: "Speichern" }).click();
    const put = await waitPut(nas, 1);
    assert.ok(put && put.size > 50, "adapter save written");
    assert.equal(put.path, "logo.vectorcraft");
    assert.equal(put.expect, "none");
  }
  await made.ctx.close();
}

// 3. Settings › Standard-Apps.
{
  const made = await open(1280, 900, "/settings/files");
  const { page } = made;
  const frame = page.frameLocator("iframe[data-window]").first();
  await frame
    .getByRole("heading", { name: "Standard-Apps" })
    .waitFor({ timeout: 15000 });
  assert.equal(
    await frame.locator("#std-docx").inputValue(),
    "webapp:wordcraft",
  );
  await shot(page, "10-standard-apps");
  await made.ctx.close();
}

// 4. Phone: the web app full screen from the home grid, no overflow at 390 px.
{
  const made = await open(390, 844, "/", { phone: true });
  const { page } = made;
  await page.locator(".home .app", { hasText: "WordCraft" }).click();
  await page
    .locator(".full iframe[data-webapp=wordcraft]")
    .waitFor({ timeout: 15000 });
  await shot(page, "11-phone-start");
  await page
    .locator(".full iframe[data-webapp=wordcraft]:not(.hidden)")
    .waitFor({ timeout: 60000 });
  await page.waitForTimeout(1500);
  const wide = await page.evaluate(() => document.documentElement.scrollWidth);
  assert.ok(wide <= 390, `no horizontal overflow (${wide})`);
  await shot(page, "12-phone-app");
  await page
    .getByRole("button", { name: /Startbildschirm|Zurück/ })
    .first()
    .click();
  await page.locator(".full").waitFor({ state: "detached", timeout: 5000 });
  await made.ctx.close();
}

// 5. Draft 04: files dropped onto the app's window – from this PC and from the file manager.
{
  const made = await open();
  const { page } = made;
  await page.locator(".ico", { hasText: "WordCraft" }).first().click();
  const win = page.locator("section.win", {
    has: page.locator("iframe[data-webapp=wordcraft]"),
  });
  await page
    .locator("iframe[data-webapp=wordcraft]:not(.hidden)")
    .waitFor({ timeout: 60000 });
  // From the PC: the desktop sees the drag, the window shows where to drop.
  const bytes = [...docx()];
  await page.evaluate((bytes) => {
    const dt = new DataTransfer();
    dt.items.add(new File([new Uint8Array(bytes)], "Rechnung-2026-11.docx"));
    window.__dt = dt;
    document
      .querySelector(".desk-area")
      .dispatchEvent(
        new DragEvent("dragenter", { bubbles: true, dataTransfer: dt }),
      );
  }, bytes);
  await win.locator(".drop").waitFor();
  await shot(page, "13-drop-shield");
  await page.evaluate(() => {
    const target = document.querySelector("section.win .webapp .drop");
    target.dispatchEvent(
      new DragEvent("dragover", {
        bubbles: true,
        cancelable: true,
        dataTransfer: window.__dt,
      }),
    );
    target.dispatchEvent(
      new DragEvent("drop", {
        bubbles: true,
        cancelable: true,
        dataTransfer: window.__dt,
      }),
    );
  });
  await page.waitForFunction(
    () =>
      /Rechnung-2026-11\.docx/.test(
        document.querySelector("section.win.focused .titlebar")?.textContent ??
          "",
      ),
    null,
    { timeout: 10000 },
  );
  assert.equal(
    await win.locator(".drop").count(),
    0,
    "shield gone after the drop",
  );
  // From the file manager (another frame of TitanOS): it reports the drag to the desktop.
  const files = await filesWindow(page);
  await files.locator('[data-file-name="Angebot.docx"]').evaluate((row) => {
    window.parent.postMessage(
      {
        type: "titanos-file-drag",
        drag: { root: "share/Documents", paths: ["Angebot.docx"] },
      },
      location.origin,
    );
    void row;
  });
  await page.waitForTimeout(200);
  await page.evaluate(() => {
    const dt = new DataTransfer();
    dt.setData("application/x-titanos-files", "1");
    const target =
      document.querySelector("section.win .webapp .drop") ??
      document.querySelector("section.win .webapp");
    target.dispatchEvent(
      new DragEvent("drop", {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
      }),
    );
  });
  // The window shows a document already: the file opens in a window of its own.
  await page.waitForFunction(
    () =>
      [...document.querySelectorAll("section.win .titlebar")].some((t) =>
        /WordCraft.*Angebot\.docx/.test(t.textContent ?? ""),
      ),
    null,
    { timeout: 15000 },
  );
  assert.equal(
    await page.locator("iframe[data-webapp=wordcraft]").count(),
    2,
    "second document, second window",
  );
  await made.ctx.close();
}

// 6. The offer after the update to 0.63 (owner/admin): install, optionally remove the Docker versions.
for (const width of [1280, 390]) {
  const list = (made) => {
    const base = webappsList(made);
    base.apps = base.apps.map((a) =>
      a.id === "wordcraft"
        ? { ...a, installed: false, state: "missing", url: "" }
        : a,
    );
    base.apps.find((a) => a.id === "pdfcraft").state = "missing";
    base.docker = [
      {
        app: "wordcraft",
        webapp: "wordcraft",
        running: true,
        image: "storytold/wordcraft",
      },
      {
        app: "photocraft",
        webapp: "photocraft",
        running: false,
        image: "storytold/photocraft",
      },
    ];
    base.migration = null;
    return base;
  };
  const made = await open(width, width < 900 ? 844 : 900, "/", {
    offer: true,
    list,
    phone: width < 900,
  });
  const { page } = made;
  const dialog = page.getByRole("dialog", {
    name: "Office-Programme direkt im Browser",
  });
  await dialog.waitFor({ timeout: 15000 });
  await dialog
    .getByText("Warum die Docker-Versionen entfernt werden")
    .waitFor();
  const wide = await page.evaluate(() => document.documentElement.scrollWidth);
  assert.ok(wide <= width, `no horizontal overflow (${wide})`);
  await shot(page, `14-offer-${width}`);
  // photocraft's web app is not installed and not chosen: its Docker app stays.
  assert.ok(
    await dialog
      .getByRole("checkbox", { name: /„photocraft“ entfernen/ })
      .isDisabled(),
  );
  assert.ok(
    await dialog
      .getByRole("checkbox", { name: /„wordcraft“ entfernen/ })
      .isChecked(),
  );
  await dialog.getByRole("button", { name: "Installieren" }).click();
  await page.waitForTimeout(500);
  const post = made.posts.at(-1);
  assert.equal(post.path, "/webapps/migrate");
  assert.deepEqual(post.body.removeDocker, ["wordcraft"]);
  assert.deepEqual([...post.body.install].sort(), [
    "gridcraft",
    "pdfcraft",
    "wordcraft",
  ]);
  await made.ctx.close();
}

// PLAN 69: managing another node of the cluster, whose office programs are
// missing, asks again (with its name) although this NAS answered already.
{
  const made = await create("owner", "graphit", 1440);
  await made.page.setViewportSize({ width: 1440, height: 900 });
  await made.ctx.addInitScript(() => {
    localStorage.removeItem("titanos.layout");
    sessionStorage.setItem("titanos.cluster.node", "n2");
  });
  made.data.prefs.hints = { "webapps-0.63": true };
  const saved = [];
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    if (p === "/cluster/nodes")
      return route.fulfill({
        json: {
          manages: true,
          version: "0.65.0",
          nodes: [
            { id: "n1", name: "titan-test", self: true, online: true },
            {
              id: "n2",
              name: "Keller",
              self: false,
              online: true,
              compatible: true,
            },
          ],
        },
      });
    if (p === "/preferences/hints" && route.request().method() === "PUT") {
      saved.push(route.request().postDataJSON());
      return route.fulfill({ status: 204 });
    }
    // The other node answers like this NAS would (test data).
    if (p.startsWith("/cluster/nodes/n2/api/")) {
      const rest = p.slice("/cluster/nodes/n2/api".length);
      if (rest === "/webapps")
        return route.fulfill({
          json: webappsList({ data: made.data }),
        });
      url.pathname = "/api/v1" + rest;
      return route.fallback({ url: url.toString() });
    }
    return route.fallback();
  });
  await made.page.goto(base + "/");
  const dialog = made.page.getByRole("dialog", {
    name: "Office-Programme direkt im Browser",
  });
  await dialog.waitFor({ timeout: 15000 });
  await dialog.getByText("Für den Knoten „Keller“ im Cluster.").waitFor();
  await shot(made.page, "14-offer-cluster");
  await made.ctx.close();
}

// 7. First start: step "Office-Programme" of /welcome; App-Center section; at 390 px.
for (const width of [1280, 390]) {
  const made = await open(width, 900, "/welcome?step=3", {
    phone: width < 900,
  });
  const { page } = made;
  await page
    .getByRole("heading", { name: "Office-Programme" })
    .waitFor({ timeout: 15000 });
  assert.ok(
    await page.getByRole("checkbox", { name: /GridCraft/ }).isChecked(),
    "preselected",
  );
  let wide = await page.evaluate(() => document.documentElement.scrollWidth);
  assert.ok(wide <= width, `welcome: no overflow (${wide})`);
  await shot(page, `15-welcome-${width}`);
  await page.getByRole("checkbox", { name: /PdfCraft/ }).uncheck();
  await page.getByRole("button", { name: "Installieren und weiter" }).click();
  await page.getByRole("heading", { name: "Nächste Schritte" }).waitFor();
  const post = made.posts.at(-1);
  assert.equal(post.path, "/webapps/defaults/install");
  assert.deepEqual(
    post.body.apps,
    ["gridcraft"],
    "WordCraft is there already, PdfCraft deselected",
  );
  assert.equal(
    made.data.prefs.hints?.["webapps-0.63"],
    true,
    "the offer after the update is answered",
  );
  await page.goto(base + "/apps");
  // On the desktop the App-Center is a window.
  const scope =
    width < 900 ? page : page.frameLocator("iframe[data-window]").first();
  await scope
    .getByRole("heading", { name: "Programme für den Browser" })
    .waitFor({ timeout: 15000 });
  wide = await page.evaluate(() => document.documentElement.scrollWidth);
  assert.ok(wide <= width, `App-Center: no overflow (${wide})`);
  // States in words, not only colour: installed, installing with progress, not installed.
  for (const word of ["Installiert", "Wird installiert", "Nicht installiert"])
    assert.ok(
      await scope.locator(".browser-apps", { hasText: word }).count(),
      `state: ${word}`,
    );
  await scope
    .locator(".browser-apps")
    .screenshot({ path: `${out}/16-appcenter-${width}.png` });
  await made.ctx.close();
}

// 8. Phone: file manager › ⋯ › Öffnen shows WordCraft full screen; Back with unsaved work asks.
{
  const made = await open(390, 844, "/files", { phone: true });
  const { page } = made;
  await page
    .locator('[data-file-name="Angebot.docx"]')
    .waitFor({ timeout: 15000 });
  await page
    .locator('[data-file-name="Angebot.docx"] button[aria-label]')
    .last()
    .click();
  await page.getByRole("menuitem", { name: "Öffnen mit" }).click();
  await shot(page, "18-phone-open-with");
  let wide = await page.evaluate(() => document.documentElement.scrollWidth);
  assert.ok(wide <= 390, `open-with menu: no overflow (${wide})`);
  await page.getByRole("menuitem", { name: "Öffnen", exact: true }).click();
  await page
    .locator(".full iframe[data-webapp=wordcraft]:not(.hidden)")
    .waitFor({ timeout: 60000 });
  await page.waitForTimeout(2000);
  const app = page.locator(".full iframe[data-webapp=wordcraft]");
  const box = await app.boundingBox();
  await page.mouse.click(box.x + box.width / 2, box.y + box.height * 0.6);
  await page.keyboard.type("x");
  await page.waitForTimeout(800);
  await page.goBack();
  await page
    .getByRole("dialog", { name: "Ungespeicherte Änderungen" })
    .waitFor({ timeout: 5000 });
  assert.equal(
    await page.locator(".full").count(),
    1,
    "the app stays while asking",
  );
  await shot(page, "17-phone-back-unsaved");
  await page.getByRole("button", { name: "Schließen ohne Speichern" }).click();
  await page.locator(".full").waitFor({ state: "detached", timeout: 5000 });
  await made.ctx.close();
}

await browser.close();
server.close();
const real = errors.filter(
  (e) => !/WebGPU|IDBFactory|storage|favicon|fonts\/manifest/i.test(e),
);
if (real.length) {
  console.log(real.join("\n"));
  process.exitCode = 1;
}
console.log(
  `webapps UI checks ${process.exitCode ? "failed" : "passed"}; screenshots in ${out}`,
);
