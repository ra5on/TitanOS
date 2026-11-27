// Run true canvas selections through the installed editors' normal UI.
// The fixed desktop window and anonymous documents are shared with the Go HTTP test.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

const examples = {
  "t-word": {
    name: "T-Word",
    ext: "docx",
    refs: [{ paragraph: 2 }, { paragraph: 3 }],
    contains: "Zweiter Absatz",
    excludes: "Fünfter Absatz",
  },
  "t-exel": {
    name: "T-Exel",
    ext: "xlsx",
    refs: [{ sheet: "Planung", range: "B2:D7" }],
    contains: "40",
    excludes: "Freigabe",
  },
  "t-pdf": {
    name: "T-PDF",
    ext: "pdf",
    refs: [{ page: 2 }, { page: 3 }],
    contains: "Seite zwei",
    excludes: "Seite eins",
  },
  "t-deck": {
    name: "T-Deck",
    ext: "pptx",
    refs: [{ slide: 2 }],
    contains: "Planung",
    excludes: "Angebot",
  },
};

async function frames(canvas) {
  // eframe queues input until a paint. Keep taps and screenshots on separate frames.
  await canvas.evaluate(async () => {
    for (let i = 0; i < 4; i++) await new Promise(requestAnimationFrame);
  });
}
async function chooseInsideEditor(id, page, canvas) {
  const box = await canvas.boundingBox();
  assert.ok(
    box && box.width > 1000 && box.height >= 590,
    "Open the regular 1440px desktop workspace",
  );
  const click = (x, y, modifiers = []) =>
    canvas.click({ position: { x, y }, modifiers });
  if (id === "t-word") {
    // Navigate by paragraphs so WebKit and Chromium may lay out the page differently.
    await click(box.width / 2, 310);
    for (const key of [
      "Control+Home",
      "Control+ArrowDown",
      "Control+Shift+ArrowDown",
      "Control+Shift+ArrowDown",
    ]) {
      await page.keyboard.press(key);
      await frames(canvas);
    }
  } else if (id === "t-exel") {
    await click(103, 239); // B2
    await frames(canvas);
    await click(209, 319, ["Shift"]); // D7
  } else if (id === "t-pdf") {
    await click(100, 400); // Seiten organisieren
    await frames(canvas);
    await click(673, 242);
    await frames(canvas);
    await click(860, 242, ["Control"]);
  } else {
    await click(106, 352); // Second slide in the filmstrip.
  }
  await frames(canvas);
}

function assertReferences(sources, expected, doc, root, version) {
  assert.equal(sources.length, expected.length);
  for (const [i, fields] of expected.entries()) {
    const source = sources[i];
    assert.equal(source.kind, "office.region");
    assert.equal(source.id, doc);
    const { root: parent, version: revision, mtime, ...region } = source.ref;
    assert.equal(parent, root);
    assert.match(revision, /^[a-f0-9]{64}$/);
    if (version) assert.equal(revision, version);
    assert.match(mtime, /^\d+$/);
    assert.deepEqual(
      region,
      Object.fromEntries(
        Object.entries(fields).map(([k, v]) => [k, String(v)]),
      ),
    );
  }
}
async function screenshot(locator, engine, id, name) {
  if (!process.env.UI_OUTPUT) return;
  await mkdir(process.env.UI_OUTPUT, { recursive: true });
  await locator.screenshot({
    path: join(process.env.UI_OUTPUT, engine + "-" + id + "-" + name + ".png"),
  });
}

export async function verifyNativeSelection({
  id,
  page,
  editor,
  show,
  dlg,
  apply,
  getContext,
  root,
  doc,
  version,
  engine,
  expectManual = process.env.UI_OFFICE_EXPECT_MANUAL === "1",
}) {
  const example = examples[id];
  const canvas = editor.locator("canvas").first();
  await show.waitFor({ timeout: 60000 });
  await canvas.waitFor({ state: "visible" });
  await frames(canvas);
  await chooseInsideEditor(id, page, canvas);
  await screenshot(canvas, engine, id, "im-editor");
  await show.click();
  let expected = example.refs;
  if (expectManual) {
    // This is a deliberate compatibility assertion for the old public WASM.
    await dlg.getByText("Bereich manuell wählen", { exact: true }).waitFor();
    if (id === "t-exel") {
      await dlg.getByLabel("Tabellenblatt").fill("Planung");
      await dlg.getByLabel("Zellbereich").fill("B2:D7");
    } else {
      const label = { "t-word": "Absatz", "t-pdf": "Seite", "t-deck": "Folie" }[
        id
      ];
      await dlg.getByLabel(label, { exact: true }).fill("2");
      if (id === "t-deck")
        await dlg.getByLabel("Absatz", { exact: true }).fill("1");
    }
    expected =
      id === "t-deck" ? [{ slide: 2, paragraph: 1 }] : example.refs.slice(0, 1);
  } else {
    await dlg.getByText("Im Editor ausgewählt", { exact: true }).waitFor();
    assert.equal(await dlg.locator("li").count(), example.refs.length);
  }
  await screenshot(
    dlg,
    engine,
    id,
    expectManual ? "manuelle-uebergabe" : "markierung-uebernommen",
  );
  const [reply] = await Promise.all([
    page.waitForResponse((r) =>
      /\/personal-agent\/contexts\/[^/]+\/consume$/.test(
        new URL(r.url()).pathname,
      ),
    ),
    apply.click(),
  ]);
  assert.equal(reply.status(), 200);
  const received = await reply.json();
  assert.equal(received.tool, "office.inspect");
  assertReferences(received.sources, expected, doc, root, version);
  if (getContext) assert.deepEqual(getContext().sources, received.sources);
  console.log(
    engine,
    id,
    expectManual
      ? "manual compatibility assertion"
      : "native coordinates verified",
    JSON.stringify(expected),
  );
  return received.sources;
}

async function runLive() {
  const origin = process.env.UI_URL,
    root = process.env.UI_OFFICE_ROOT;
  assert.ok(
    process.env.UI_STORAGE_STATE && root === "share/Docs",
    "Run this through TestOfficeEditorSelectionLive",
  );
  assert.equal(new URL(origin).hostname, "127.0.0.1");
  const engine = process.env.UI_ENGINE || "chromium";
  const driver = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
  const browser = await driver[engine].launch({
    executablePath: process.env.BROWSER_EXECUTABLE,
    args:
      engine === "chromium"
        ? ["--enable-unsafe-swiftshader", "--use-angle=swiftshader"]
        : [],
  });
  const expectManual = process.env.UI_OFFICE_EXPECT_MANUAL === "1";
  try {
    for (const [id, example] of Object.entries(examples)) {
      if (process.env.UI_OFFICE_APP && process.env.UI_OFFICE_APP !== id)
        continue;
      const ctx = await browser.newContext({
        baseURL: origin,
        viewport: { width: 1440, height: 900 },
        locale: "de-DE",
        storageState: process.env.UI_STORAGE_STATE,
        serviceWorkers: "block",
        reducedMotion: "reduce",
      });
      try {
        await ctx.addInitScript(() => {
          if (location.pathname.startsWith("/webapps/")) return;
          localStorage.setItem("titanos.locale", "de");
        });
        if (process.env.UI_OFFICE_TRACE === "1") {
          await ctx.addInitScript(() => {
            window.addEventListener("message", ({ data }) => {
              if (
                data?.type === "webapp-agent-selection-request" ||
                data?.type === "webapp-agent-selection"
              ) {
                console.info(
                  "office-selection",
                  Date.now(),
                  data.type,
                  data.requestId,
                  data.status,
                  JSON.stringify(data.items),
                );
              }
            });
          });
        }
        const page = await ctx.newPage(),
          errors = [];
        page.on("pageerror", (e) => {
          console.error(engine, id, "pageerror", e.stack || e.message);
          errors.push(e.message);
        });
        page.on("console", (m) => {
          if (
            process.env.UI_OFFICE_TRACE === "1" &&
            m.text().startsWith("office-selection")
          )
            console.log(engine, id, m.text());
          if (
            m.type() === "error" &&
            (m.location().url.includes("/webapps/") ||
              /wasm|panicked/i.test(m.text()))
          )
            console.error(engine, id, m.location().url, m.text());
        });
        page.setDefaultTimeout(25000);
        const doc = "Bereiche." + example.ext;
        const params = new URLSearchParams({ root, path: doc });
        const download = async () => {
          const resp = await ctx.request.get(
            "/api/v1/files/download?" + params,
          );
          assert.ok(resp.ok());
          const version = resp.headers()["x-titanos-file-version"];
          assert.match(version, /^[a-f0-9]{64}$/);
          return {
            version,
            sha256: createHash("sha256")
              .update(await resp.body())
              .digest("hex"),
          };
        };
        const before = await download();
        await page.goto("/");
        const notice = page.getByRole("dialog", { name: "Haftungsausschluss" });
        const offer = page
          .locator("dialog[open]")
          .filter({ hasText: "Office-Programme direkt im Browser" });
        await page.locator(".topbar").waitFor();
        if (await offer.isVisible())
          await offer
            .getByRole("button", { name: "Nicht mehr fragen" })
            .click();
        if (await notice.isVisible()) {
          await notice.getByRole("checkbox").check();
          await notice.getByRole("button", { name: "Weiter" }).click();
        }
        await page
          .locator(".ico")
          .filter({ hasText: "Dateien" })
          .first()
          .dblclick();
        const files = page.frameLocator('iframe[data-app="app:files"]');
        const row = files.locator('[data-file-name="' + doc + '"]:visible');
        // The test share is the default location. Wait for the file list itself;
        // the shell and the hidden mobile location label render before the list.
        await row.click({ button: "right" });
        await files.getByRole("menuitem", { name: "Öffnen mit" }).click();
        await files
          .getByRole("menuitem", { name: new RegExp("^" + example.name) })
          .click();
        const dlg = page.getByRole("dialog", { name: "Mit Agent bearbeiten" });
        const sources = await verifyNativeSelection({
          id,
          page,
          editor: page.frameLocator('iframe[data-webapp="' + id + '"]'),
          show: page.locator(".agent-strip button:visible"),
          dlg,
          apply: dlg.getByRole("button", { name: "Auswahl im Agent öffnen" }),
          doc,
          root,
          version: before.version,
          engine,
          expectManual,
        });
        const agent = page.frameLocator('iframe[data-app="app:agent"]').last();
        await agent.locator(".pa-context > button").last().click();
        const [created] = await Promise.all([
          page.waitForResponse(
            (r) =>
              r.request().method() === "POST" &&
              new URL(r.url()).pathname === "/api/v1/personal-agent/jobs",
          ),
          agent.locator(".pa-compose [type=submit]").click(),
        ]);
        assert.ok(created.ok());
        const { id: jobId } = await created.json();
        let result;
        for (let i = 0; i < 120; i++) {
          const r = await ctx.request.get(
            "/api/v1/personal-agent/jobs/" + encodeURIComponent(jobId),
          );
          assert.ok(r.ok());
          result = await r.json();
          if (!["queued", "running", "planned"].includes(result.status)) break;
          await delay(250);
        }
        assert.equal(result.status, "done", result.error);
        assert.ok(
          result.result.text.includes(example.contains),
          id + " missing selected content: " + result.result.text,
        );
        assert.ok(
          !result.result.text.includes(example.excludes),
          id + " returned content outside the chosen region",
        );
        assert.deepEqual(result.result.sources, sources);
        const reply = agent.locator('[data-guide="reply-' + jobId + '"]');
        const answer = reply.locator(".pa-readable");
        await answer.waitFor();
        assert.equal((await answer.textContent()).trim(), result.result.text);
        await reply.locator(".pa-card-open").click();
        const detail = agent.locator('.pa-detail[data-job="' + jobId + '"]');
        await detail.locator(".pa-readable").waitFor();
        assert.equal(
          await detail.locator(".pa-source-list li").count(),
          sources.length,
        );
        assert.equal(
          await detail.locator(".pa-diff").count(),
          0,
          "reading does not change the source",
        );
        await screenshot(detail, engine, id, "worker-ergebnis");
        assert.deepEqual(await download(), before, "Original file changed");
        assert.deepEqual(errors, []);
        console.log(
          engine,
          id,
          expectManual ? "1.7 manual fallback" : "native editor selection",
          "→ stored Agent job, original verified",
        );
      } catch (err) {
        const current = ctx.pages()[0];
        if (current && process.env.UI_OUTPUT)
          await screenshot(current, engine, id, "fehlgeschlagen").catch(
            () => {},
          );
        throw err;
      } finally {
        await ctx.close();
      }
    }
  } finally {
    await browser.close();
  }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  await runLive();
