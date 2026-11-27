// Exercise TitanOS's shipped WebAppView, host.js and Agent handoff. Only the
// independently distributed editor's selection events and NAS API are fixtures.
import assert from "node:assert/strict";
import { readFile, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const engine = process.env.UI_ENGINE || "webkit";
const playwright = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const errors = [];
const cases = [
  [
    "t-word",
    "T-Word",
    "wordcraft",
    "docx",
    [{ paragraph: 3 }, { paragraph: 6 }],
  ],
  [
    "t-exel",
    "T-Exel",
    "gridcraft",
    "xlsx",
    [
      { sheet: "Planung", range: "B2:D7" },
      { sheet: "Budget", range: "E2" },
    ],
  ],
  ["t-pdf", "T-PDF", "pdfcraft", "pdf", [{ page: 4 }, { page: 2 }]],
  [
    "t-deck",
    "T-Deck",
    "deckcraft",
    "pptx",
    [{ slide: 2, paragraph: 3 }, { slide: 4 }],
  ],
];
// Catalog compatibility uses one real loopback origin for the UI, the
// installed editor and the production bridge. Only NAS data calls are fixtures.
let baseline;
if (process.env.UI_OFFICE_ENDPOINT) {
  baseline = JSON.parse(await readFile(process.env.UI_OFFICE_ENDPOINT, "utf8"));
} else if (process.env.UI_OFFICE_PACKAGES) {
  const { startOfficeEditorServer } =
    await import("./test-ui-office-server.mjs");
  baseline = await startOfficeEditorServer(
    playwright.request,
    cases.map(([id]) => id),
  );
}
const origin =
  baseline?.origin || process.env.UI_URL || "http://127.0.0.1:5495";
assert.ok(
  ["127.0.0.1", "localhost", "[::1]"].includes(new URL(origin).hostname),
);
const nativeSelection = process.env.UI_OFFICE_NATIVE === "1";
if (nativeSelection) {
  assert.ok(
    baseline && process.env.UI_WIDTH === "1440",
    "Native interaction requires installed editors and UI_WIDTH=1440",
  );
}
if (baseline) {
  for (const [id] of cases) {
    const app = baseline.catalog.apps.find((app) => app.id === id);
    assert.ok(
      app?.installed &&
        app.state === "ready" &&
        (nativeSelection || app.version === "1.7.0"),
      "Unexpected or unavailable catalog package " + id,
    );
  }
}
const fixtureDir =
  process.env.UI_OFFICE_FIXTURES ||
  fileURLToPath(new URL("./testdata/editor-selection/", import.meta.url));
const browser = await playwright[engine].launch({
  executablePath: process.env.BROWSER_EXECUTABLE,
  args:
    baseline && engine === "chromium"
      ? ["--enable-unsafe-swiftshader", "--use-angle=swiftshader"]
      : undefined,
});
const host = await readFile(
  new URL("../web/static/webapps/_host/host.js", import.meta.url),
);
try {
  for (const width of process.env.UI_WIDTH
    ? [Number(process.env.UI_WIDTH)]
    : [390, 1440]) {
    const mobile = width < 710;
    for (const [id, name, prefix, ext, items] of cases) {
      const { ctx, page } = await fixtureFactory(browser, errors)(
        "owner",
        "graphit",
        width,
        "de",
        {
          viewport: { width, height: mobile ? 844 : 900 },
          isMobile: mobile,
          hasTouch: mobile,
          nativeShell: true,
          reducedMotion: "reduce",
        },
      );
      try {
        page.setDefaultTimeout(20000);
        await ctx.route("**/api/v1/services/state", (r) =>
          r.fulfill({ json: { agent: true } }),
        );
        await ctx.route("**/sw.js*", (r) => r.abort("blockedbyclient"));
        if (process.env.UI_DEBUG)
          ctx.on("request", (r) => {
            if (/personal-agent\/contexts|\/agent\?context/.test(r.url()))
              console.log(r.method(), r.url());
          });
        await ctx.route("**/api/v1/cluster/nodes", (r) =>
          r.fulfill({
            json: { nodes: [], version: "fixture", manages: false },
          }),
        );
        const doc = (baseline ? "Bereiche." : "Angebot.") + ext,
          version = "b".repeat(64),
          time = "2026-10-23T09:02:00Z";
        const root = "share/Documents",
          entry = { name: doc, dir: false, modified: time, version, size: 4 };
        await ctx.route("**/api/v1/webapps", (r) =>
          r.fulfill({
            json: baseline?.catalog || {
              volume: "Data",
              apps: [
                {
                  id,
                  name,
                  version: "1.0",
                  installed: true,
                  state: "ready",
                  progress: 1,
                  bridge: "host",
                  types: [ext],
                  default: true,
                  url: "/webapps/" + id + "/1.0/",
                  trusted: false,
                },
              ],
            },
          }),
        );
        await ctx.route("**/api/v1/files/list?*", (r) =>
          r.fulfill({ json: [entry] }),
        );
        await ctx.route("**/api/v1/files/properties?*", (r) =>
          r.fulfill({ json: entry }),
        );
        await ctx.route("**/api/v1/files/download?*", async (r) =>
          r.fulfill({
            headers: { "X-TitanOS-File-Version": version },
            body: baseline
              ? await readFile(join(fixtureDir, doc))
              : Buffer.from("test"),
          }),
        );
        let context;
        await ctx.route("**/api/v1/personal-agent/contexts", async (r) => {
          context = r.request().postDataJSON();
          await r.fulfill({ status: 201, json: { id: "OfficeSelection" } });
        });
        await ctx.route(
          "**/api/v1/personal-agent/contexts/OfficeSelection/consume",
          (r) => r.fulfill({ json: context }),
        );
        await ctx.route("**/api/v1/personal-agent/state", (r) =>
          r.fulfill({
            json: {
              chats: [],
              jobs: [],
              settings: {
                theme: "system",
                motion: "off",
                colors: {},
                rights: {},
                notifications: {},
              },
              capabilities: [
                {
                  id: "office.inspect",
                  title: "Auswahl prüfen",
                  app: "files",
                  effective: "auto",
                  ceiling: "auto",
                },
              ],
            },
          }),
        );
        if (!baseline)
          await ctx.route("**/webapps/_host/host.js", (r) =>
            r.fulfill({
              contentType: "text/javascript",
              headers: { "Access-Control-Allow-Origin": "*" },
              body: host,
            }),
          );
        const html = `<!doctype html><title>${name} Testeditor</title><script src="/webapps/_host/host.js" data-bridge="host" data-app="${id}" data-events="${prefix}"></script><canvas data-host="page"></canvas><button id="choose">Auswahl markieren</button><script>
     const c=document.querySelector('canvas'), event=(name,detail)=>c.dispatchEvent(new CustomEvent('${prefix}:'+name,{detail}));
     window.office = {status:'none', items:[],doc:'',calls:0};
     c.addEventListener('${prefix}:open',e=>{office.doc=e.detail.documentId;office.calls=0;office.status='none'});
     c.addEventListener('${prefix}:agent-selection-request',e=>{office.calls++;const {version,requestId,documentId}=e.detail;event('agent-selection',{version,requestId,documentId,status:office.doc===documentId?office.status:'unbound',items:office.items});});
     choose.onclick=()=>{office.status='selected';office.items=${JSON.stringify(items)}};
     event('ready',{});
     </script>`;
        if (!baseline)
          await ctx.route("**/webapps/" + id + "/1.0/**", (r) =>
            r.fulfill({
              contentType: "text/html",
              headers: { "Access-Control-Allow-Origin": "*" },
              body: html,
            }),
          );
        await page.goto(
          mobile ? origin + "/files?root=share%2FDocuments" : origin + "/",
          { waitUntil: "domcontentloaded", timeout: 45000 },
        );
        let files = page;
        if (!mobile) {
          await page
            .locator(".ico")
            .filter({ hasText: "Dateien" })
            .first()
            .dblclick();
          files = page.frameLocator("iframe[data-window]").first();
        }
        const row = files.locator('[data-file-name="' + doc + '"]:visible');
        if (mobile) await row.locator("button[aria-label]").last().click();
        else await row.click({ button: "right" });
        await files.getByRole("menuitem", { name: "Öffnen mit" }).click();
        await files
          .getByRole("menuitem", { name: new RegExp("^" + name, "i") })
          .click();
        const editor = page.frameLocator('iframe[data-webapp="' + id + '"]');
        if (!baseline) await editor.locator("#choose").click();
        const show = page.locator(".agent-strip button:visible"),
          dlg = page.getByRole("dialog", { name: "Mit Agent bearbeiten" });
        const apply = dlg.getByRole("button", {
            name: "Auswahl im Agent öffnen",
          }),
          close = dlg.getByRole("button", { name: "Schließen" });
        // Wait for the destination Agent to render its source chips and finish
        // loading before tearing down the frame. In WebKit, closing an active
        // navigation can surface cancelled shell requests as access errors.
        async function openSelection() {
          const [reply] = await Promise.all([
            page.waitForResponse((r) =>
              r.url().includes("/contexts/OfficeSelection/consume"),
            ),
            apply.click(),
          ]);
          const receiver = reply.frame();
          await receiver.locator('[data-guide="source-0"]').first().waitFor();
          await receiver.waitForLoadState("networkidle");
        }
        if (baseline) {
          await show.waitFor({ timeout: 60000 });
          if (nativeSelection) {
            const { verifyNativeSelection } =
              await import("./test-ui-office-native.mjs");
            await verifyNativeSelection({
              id,
              page,
              editor,
              show,
              dlg,
              apply,
              getContext: () => context,
              root,
              doc,
              version,
              engine,
            });
            continue;
          }
          if (process.env.UI_OUTPUT) {
            const canvas = editor.locator('canvas[data-host="page"]').first();
            await canvas.waitFor();
            await canvas.evaluate(
              () =>
                new Promise((done) =>
                  requestAnimationFrame(() => requestAnimationFrame(done)),
                ),
            );
            await mkdir(process.env.UI_OUTPUT, { recursive: true });
            await canvas.screenshot({
              path: join(
                process.env.UI_OUTPUT,
                engine + "-" + width + "-" + id + "-1.7-canvas.png",
              ),
            });
          }
          await show.click({ timeout: 60000 });
          const label = {
            docx: "Absatz",
            xlsx: "Tabellenblatt",
            pdf: "Seite",
            pptx: "Folie",
          }[ext];
          await dlg
            .getByLabel(label, { exact: true })
            .waitFor({ timeout: 12000 });
          if (ext === "xlsx") {
            await dlg.getByLabel("Tabellenblatt").fill("Planung");
            await dlg.getByLabel("Zellbereich").fill("B2");
          }
          if (ext === "docx") await dlg.getByLabel(label).fill("2");
          await openSelection();
          assert.equal(context?.sources.length, 1);
          assert.equal(context.sources[0].kind, "office.region");
          assert.equal(
            context.sources[0].ref[
              { docx: "paragraph", xlsx: "range", pdf: "page", pptx: "slide" }[
                ext
              ]
            ],
            { docx: "2", xlsx: "B2", pdf: "1", pptx: "1" }[ext],
          );
          console.log(
            engine,
            width,
            name,
            "echtes 1.7.0 WASM, manueller Bereich übernommen",
          );
          continue;
        }
        if (id === "t-word" && !mobile) {
          // Leave a conflict open and export a different buffer. An uncorrelated
          // FIFO receipt would report that the original failed before its dialog closes.
          let writes = 0;
          await ctx.route("**/api/v1/files/content?*", (r) =>
            ++writes === 1
              ? r.fulfill({ status: 409, json: { code: "conflict" } })
              : r.fulfill({ json: { mtime: "12345", version } }),
          );
          const canvas = editor.locator("canvas");
          await canvas.evaluate((c, file) => {
            window.receipts = [];
            for (const k of ["saved", "save-failed"])
              c.addEventListener("wordcraft:" + k, (e) =>
                window.receipts.push([k, e.detail.id]),
              );
            window.fixtureSave = (id, name) =>
              c.dispatchEvent(
                new CustomEvent("wordcraft:save", {
                  detail: { id, name, bytes: new Uint8Array([1, 2, 3]).buffer },
                }),
              );
            window.fixtureSave("original", file);
          }, doc);
          const cancel = page.getByRole("button", {
            name: "Abbrechen",
            exact: true,
          });
          await cancel.waitFor();
          await canvas.evaluate(() =>
            window.fixtureSave("export", "Kopie.pdf"),
          );
          const receipts = (n) =>
            canvas.evaluate(async (c, n) => {
              for (let i = 0; i < 100 && window.receipts.length < n; i++)
                await new Promise((r) => setTimeout(r, 30));
              return window.receipts;
            }, n);
          assert.deepEqual(await receipts(1), [["save-failed", "export"]]);
          await cancel.click();
          assert.deepEqual(await receipts(2), [
            ["save-failed", "export"],
            ["save-failed", "original"],
          ]);
          await canvas.evaluate(
            (c, file) => window.fixtureSave("next", file),
            doc,
          );
          assert.deepEqual((await receipts(3)).at(-1), ["saved", "next"]);
        }
        await show.click();
        await dlg.getByText("Im Editor ausgewählt").waitFor();
        assert.equal(await dlg.locator("li").count(), items.length);
        if (id === "t-word") {
          // A changed selection between opening the sheet and handoff cannot submit.
          await editor.locator("canvas").evaluate(() => {
            window.office.items = [{ paragraph: 19 }];
          });
          await apply.click();
          await dlg.getByText(/Die Auswahl hat sich geändert/).waitFor();
          assert.equal(context, undefined);
          await close.click();
          // A document opened inside the editor no longer matches the snapshot
          // originally handed to it, even if the parent still shows the same filename.
          await editor.locator("canvas").evaluate(() => {
            window.office.previousDoc = window.office.doc;
            window.office.doc = "another-buffer";
          });
          await show.click();
          await dlg.getByText(/Öffne die gespeicherte NAS-Datei/).waitFor();
          assert.equal(await apply.isDisabled(), true);
          await close.click();
          await editor.locator("canvas").evaluate(() => {
            window.office.doc = window.office.previousDoc;
          });
          for (const state of ["dirty", "unsupported"]) {
            await editor.locator("canvas").evaluate((c, state) => {
              window.office.status = state;
            }, state);
            await show.click();
            if (state === "unsupported")
              await dlg.getByText(/nicht vollständig übernehmen/).waitFor();
            else {
              await dlg.getByText(/Speichere die Änderungen/).waitFor();
              assert.equal(await apply.isDisabled(), true);
            }
            await close.click();
          }
          await editor.locator("#choose").click();
          await show.click();
          await dlg.getByText("Im Editor ausgewählt").waitFor();
        }
        if (process.env.UI_OUTPUT) {
          await mkdir(process.env.UI_OUTPUT, { recursive: true });
          await page.screenshot({
            path: join(
              process.env.UI_OUTPUT,
              engine + "-" + width + "-" + id + "-auswahl.png",
            ),
          });
        }
        await openSelection();
        assert.equal(context?.tool, "office.inspect");
        assert.equal(context.sources.length, items.length);
        for (const [index, item] of items.entries()) {
          const src = context.sources[index];
          assert.equal(src.kind, "office.region");
          assert.equal(src.ref.root, root);
          assert.equal(src.id, doc);
          assert.equal(src.ref.version, version);
          for (const [key, val] of Object.entries(item))
            assert.equal(src.ref[key], String(val));
        }
        console.log(
          engine,
          width,
          name,
          items.length,
          "Editorregionen bestätigt",
        );
      } catch (e) {
        console.error(
          name,
          width,
          page.url(),
          (await page.locator("body").innerText()).slice(0, 220),
        );
        throw e;
      } finally {
        await ctx.close();
      }
    }
  }
  assert.deepEqual(errors, []);
} finally {
  await baseline?.close?.();
  await browser.close();
}
