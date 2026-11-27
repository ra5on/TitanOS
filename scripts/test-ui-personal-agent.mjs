// Exercise the normal personal Agent entry in phone and desktop sizes using isolated API fixtures.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";

const origin = process.env.UI_URL || "http://127.0.0.1:5393";
assert.ok(
  ["localhost", "127.0.0.1", "[::1]"].includes(new URL(origin).hostname),
);
const engine = process.env.UI_ENGINE || "webkit";
const browser = await (
  await import(process.env.PLAYWRIGHT_MODULE || "playwright")
)[engine].launch({ executablePath: process.env.BROWSER_EXECUTABLE });
const errors = [],
  time = "2026-10-23T09:02:00Z";
const mail = {
  kind: "mail",
  id: "mail1",
  label: "Liefertermin",
  ref: { account: "business", folder: "INBOX", uid: "17" },
};
const pdf = {
  kind: "mail.pdf",
  id: "page",
  label: "Angebot.pdf · S. 2",
  ref: { ...mail.ref, part: "2", page: "2" },
};
const draft = {
  app: "mail",
  kind: "reply",
  fields: {
    a: "Workshop",
    from: "team@example.test",
    to: "einkauf@example.test",
    subject: "Re: Liefertermin",
    text: "Guten Morgen,\n\nder Liefertermin am Donnerstag passt. Bitte bestätigen Sie das Zeitfenster von 10 bis 12 Uhr.\n\nViele Grüße",
  },
};
const tools = [
  ["agent.example", "Beispiel ansehen"],
  ["chat.ask", "Frage stellen"],
  ["mail.reply", "Antwort vorbereiten"],
  ["mail.send", "Antwort versenden"],
  ["attachment.read", "PDF lesen"],
  ["files.read", "Datei lesen"],
  ["office.inspect", "Auswahl prüfen"],
  ["office.compare", "Angebote vergleichen"],
  ["office.pdf-merge", "PDF-Seiten sammeln"],
  ["files.search", "Im gewählten Ordner suchen"],
].map(([id, title]) => ({
  id,
  title,
  app: id.split(".")[0],
  effective: id === "mail.send" ? "ask" : "auto",
  ceiling: "auto",
  sensitive: id === "mail.send",
}));
function makeJob(id, tool, prompt, status) {
  return {
    id,
    request: {
      chatId: "discussion",
      requestId: id,
      tool,
      prompt,
      sources: [mail, pdf],
    },
    status,
    phase: "preparing",
    created: time,
    updated: time,
    seen: false,
  };
}
async function run(width, height) {
  const mobile = width < 710;
  const { page, ctx } = await fixtureFactory(browser, errors)(
    "owner",
    "graphit",
    width,
    "de",
    {
      viewport: { width, height },
      isMobile: mobile,
      nativeShell: mobile,
      hasTouch: mobile,
      serviceWorkers: "block",
      reducedMotion: "reduce",
    },
  );
  const snapshot = {
    chats: [],
    jobs: [],
    settings: {
      theme: "system",
      motion: "full",
      dotSize: 10,
      paused: false,
      colors: {},
      rights: {},
      notifications: { result: true, blocker: true, approval: true },
    },
    capabilities: tools,
  };
  let approvals = 0,
    handoff = null,
    consumed = 0,
    limits = {},
    examples = 0;
  const output = process.env.UI_OUTPUT;
  async function shot(name) {
    if (output) {
      await mkdir(output, { recursive: true });
      await page.screenshot({
        path: join(output, engine + "-" + width + "-" + name + ".png"),
      });
    }
  }
  const go = async (query = "") => {
    await page.goto(origin + "/agent" + query);
    await page.locator(".pa-welcome, .pa-conversation").first().waitFor();
  };
  await ctx.route("**/api/v1/services/state", (r) =>
    r.fulfill({ json: { agent: true } }),
  );
  await ctx.route("**/api/v1/personal-agent/**", async (route) => {
    const req = route.request(),
      part = new URL(req.url()).pathname.split("/personal-agent/")[1],
      body = req.postDataJSON();
    if (part === "contexts") {
      handoff = body;
      return route.fulfill({ json: { id: "opaque" + consumed } });
    }
    if (part.startsWith("contexts/")) {
      consumed++;
      return route.fulfill({ json: handoff });
    }
    if (part === "search")
      return route.fulfill({
        json: {
          jobs: [
            {
              id: "draft",
              chatId: "discussion",
              title: "Antwortentwurf gefunden",
            },
          ],
          chats: [],
        },
      });
    if (part.startsWith("limits/")) {
      if (req.method() === "PUT") limits = body.rights;
      return route.fulfill({
        json: { defaults: { "agent.example": "auto" }, rights: limits },
      });
    }
    if (part === "jobs" && body.tool === "agent.example") {
      examples++;
      assert.deepEqual(body.sources, []);
      const c = { id: "example", title: "Beispiel", created: time };
      const j = {
        ...makeJob("exampleJob", "agent.example", "", "done"),
        request: { ...body, chatId: "example" },
        result: { title: "Beispiel", text: "Das Beispiel ist fertig." },
      };
      snapshot.chats.push(c);
      snapshot.jobs.push(j);
      return route.fulfill({ json: j });
    }
    if (part === "state") return route.fulfill({ json: snapshot });
    if (part === "settings") {
      Object.assign(snapshot.settings, body);
      return route.fulfill({ json: snapshot });
    }
    if (part === "memory") return route.fulfill({ json: { items: [] } });
    if (part === "skills") return route.fulfill({ json: { skills: [] } });
    if (part === "attachments") return route.fulfill({ json: { sources: [] } });
    if (part === "jobs" && body.tool === "mail.send") {
      assert.equal(body.args.text, draft.fields.text);
      assert.equal(body.sources.length, 1);
      const job = {
        ...makeJob("send", "mail.send", "", "waiting"),
        request: body,
        token: "fixture-token",
        phase: "approval",
        preview: {
          title: "Antwort versenden",
          sources: [mail],
          data: { ...draft.fields, account: "Workshop" },
        },
      };
      snapshot.jobs.push(job);
      return route.fulfill({ json: job });
    }
    const action = part.match(/^jobs\/([^/]+)\/(seen|approve)$/);
    if (action) {
      const job = snapshot.jobs.find((j) => j.id === action[1]);
      if (action[2] === "seen") job.seen = true;
      else {
        assert.equal(body.token, "fixture-token");
        approvals++;
        Object.assign(job, {
          status: "done",
          phase: "complete",
          token: undefined,
          result: {
            title: "Vom Mailserver angenommen",
            text: "Der Mailserver hat die Antwort angenommen.",
            sources: [mail],
            data: { accepted: true, sentCopy: true },
          },
        });
      }
      return route.fulfill({ json: job });
    }
    return route.fulfill({ json: {} });
  });
  try {
    if (process.env.UI_BASELINE_URL) {
      const old = new URL(process.env.UI_BASELINE_URL);
      assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(old.hostname));
      await ctx.route(old.origin + "/api/v1/agent/status", (r) =>
        r.fulfill({
          json: {
            enabled: true,
            allowed: true,
            admin: true,
            model: "Testmodell",
            volume: true,
            reachable: true,
            models: [],
            running: [],
            context: 2048,
          },
        }),
      );
      for (const [name, data] of Object.entries({
        chats: { chats: [] },
        projects: { projects: [] },
        runs: { runs: [] },
        notes: { notes: [] },
        skills: { skills: [] },
      }))
        await ctx.route(old.origin + "/api/v1/agent/" + name, (r) =>
          r.fulfill({ json: data }),
        );
      await page.goto(old.origin + "/agent");
      await page.locator(".agent").waitFor();
      await page
        .locator(".agent :is(button,textarea):visible")
        .first()
        .waitFor();
      await shot("vorher");
    }
    await go();
    await shot("leer");
    await page.locator(".pa-example").click();
    await page
      .locator(".pa-chat-head")
      .getByRole("heading", { name: "Beispiel" })
      .waitFor();
    assert.equal(examples, 1);
    snapshot.jobs = [];
    snapshot.chats = [];
    snapshot.chats.push({
      id: "discussion",
      title: "Liefertermin abstimmen",
      created: time,
    });
    const job = makeJob(
      "draft",
      "mail.reply",
      "Prüfe den Liefertermin und bereite eine Antwort vor.",
      "running",
    );
    snapshot.jobs.push(job);
    await go("?chat=discussion");
    await page.locator(".pa-author").first().waitFor();
    await shot("laufend");
    job.status = "done";
    job.result = {
      title: "Antwortentwurf",
      text: "Die Antwort liegt zur Prüfung bereit.",
      sources: [mail, pdf],
      data: { draft },
    };
    await go("?chat=discussion");
    await page.locator(".pa-card-excerpt").waitFor();
    if (!mobile) await page.locator(".pa-card-open").click();
    await shot("entwurf");
    const review = mobile
      ? page.locator(".pa-mobile-card-actions .pa-primary")
      : page.locator(".pa-detail .pa-approval .pa-primary");
    await review.click();
    const send = page.locator(".pa-detail[data-job='send']");
    await send.waitFor();
    assert.match(await send.innerText(), /einkauf@example.test/);
    assert.equal(approvals, 0);
    await shot("freigabe");
    await send.locator('[data-guide="lock"] button').click();
    await send.locator(".pa-verified").waitFor();
    assert.equal(approvals, 1);
    await shot("ergebnis");
    if (!mobile) {
      await page.emulateMedia({ reducedMotion: "no-preference" });
      await page
        .getByRole("button", { name: "Spracheingabe", exact: true })
        .click();
      const orb = page.locator(".pa-sheet [data-guide=orb]");
      await orb.waitFor();
      await page.waitForFunction(
        () => {
          const guide = document.querySelector(".guide");
          const orb = document.querySelector(".pa-sheet [data-guide=orb]");
          if (!guide || !orb) return false;
          const g = guide.getBoundingClientRect(),
            o = orb.getBoundingClientRect();
          return (
            Math.abs(g.top + g.height / 2 - o.top - o.height / 2) < 4 &&
            Math.abs(g.left + g.width / 2 - o.left - o.width / 2) < 4
          );
        },
        undefined,
        { timeout: 4000 },
      );
      const clipped = await page.evaluate(cutTextIn);
      assert.deepEqual(
        clipped.filter(
          (line) => line.includes("pa-shell") || line.includes("pa-button"),
        ),
        [],
      );
      await page.keyboard.press("Escape");
      await page.emulateMedia({ reducedMotion: "reduce" });
    }
    if (mobile) await page.keyboard.press("Escape");
    await page.locator('[data-guide="jobs-tab"]:visible').click();
    await page.locator(".pa-row").first().waitFor();
    await shot("auftraege");
    await page.locator('[data-guide="settings-tab"]:visible').click();
    await page.locator(".pa-sample").waitFor();
    const appearance = page
      .locator(".pa-settings-card")
      .filter({ has: page.locator(".pa-sample") });
    await appearance.locator("select").first().selectOption("light");
    await appearance.locator("select").nth(1).selectOption("off");
    await appearance
      .getByRole("button", { name: "Speichern", exact: true })
      .click();
    await page.waitForFunction(
      () =>
        document.querySelector(".pa-shell")?.dataset.themeChoice === "light",
    );
    await go();
    assert.equal(
      await page.locator(".pa-shell").getAttribute("data-motion"),
      "off",
    );
    if (mobile)
      await page.getByRole("button", { name: "Chatverlauf öffnen" }).click();
    await page.locator(".pa-ui input[type=search]:visible").fill("Angebot");
    await page.getByRole("button", { name: "Antwortentwurf gefunden" }).click();
    await page.locator(".pa-detail[data-job=draft]").waitFor();
    if (mobile) await page.keyboard.press("Escape");
    else {
      await page.setViewportSize({ width: 760, height });
      await page.getByRole("dialog").waitFor();
      await shot("schmales-fenster");
      await page.keyboard.press("Escape");
      await page.setViewportSize({ width, height });
    }
    if (mobile) {
      const word = {
        id: "t-word",
        name: "T-Word",
        version: "1.0",
        installed: true,
        state: "ready",
        progress: 1,
        bridge: "host",
        types: ["docx"],
        default: true,
        url: "/webapps/t-word/1.0/",
        trusted: true,
      };
      const doc = "Angebot.docx";
      const entry = { name: doc, dir: false, modified: time, size: 4 };
      await ctx.route("**/api/v1/webapps", (r) =>
        r.fulfill({ json: { volume: "Data", apps: [word] } }),
      );
      await ctx.route("**/api/v1/files/list?*", (r) =>
        r.fulfill({ json: [entry] }),
      );
      await ctx.route("**/api/v1/files/properties?*", (r) =>
        r.fulfill({ json: entry }),
      );
      await ctx.route("**/api/v1/files/download?*", (r) =>
        r.fulfill({ body: Buffer.from("test") }),
      );
      // The native host and its document, dirty state, selection and transitions
      // are under test. Only the embedded editor's readiness handshake is simulated.
      await ctx.route("**/webapps/t-word/1.0/**", (r) =>
        r.fulfill({
          contentType: "text/html",
          body: '<title>T-Word-Testadapter</title><p>Angebot.docx</p><script>parent.postMessage({type:"webapp-ready"},"*")</script>',
        }),
      );
      const files = origin + "/files?root=share%2FDocuments";
      await page.goto(files);
      const row = page.locator('[data-file-name="Angebot.docx"]:visible');
      await row.locator("button[aria-label]").last().click();
      await page
        .getByRole("menuitem", { name: "Mit Agent bearbeiten" })
        .click();
      let selection = page.locator(".pa-sheet");
      await selection.waitFor();
      assert.equal(handoff.sources[0].id, doc);
      assert.equal(handoff.tool, "files.read");
      assert.equal(consumed, 1);
      await shot("dateien-uebergabe");
      await page.goto(files);
      await row.locator("button[aria-label]").last().click();
      await page.getByRole("menuitem", { name: "Öffnen mit" }).click();
      await page.getByRole("menuitem", { name: /^T-Word/i }).click();
      await page.locator(".agent-strip button").click();
      let dialog = page.getByRole("dialog", { name: "Mit Agent bearbeiten" });
      await dialog.getByLabel("Absatz").fill("3");
      await shot("office-auswahl");
      await dialog
        .getByRole("button", { name: "Auswahl im Agent öffnen" })
        .click();
      await selection.waitFor();
      assert.equal(handoff.tool, "office.inspect");
      assert.equal(handoff.sources[0].ref.paragraph, "3");
      assert.equal(await selection.getByLabel("Absatz").inputValue(), "3");
      assert.equal(consumed, 2);
    }
    // The same scoped source handoff is used by Files and the native editors.
    const src = (kind, id) => ({
      kind,
      id,
      label: id,
      ref: { root: "share/Documents" },
    });
    handoff = {
      tool: "office.compare",
      sources: [
        src("file", "A.pdf"),
        src("file", "B.pdf"),
        src("folder", "Entwuerfe"),
      ],
    };
    await go("?context=collection");
    const sheet = page.getByRole("dialog");
    await sheet.waitFor();
    const picker = sheet.locator(".pa-field select").first();
    await picker.selectOption("office.pdf-merge");
    assert.deepEqual(
      await picker
        .locator("option")
        .evaluateAll((items) => items.map((item) => item.value).sort()),
      ["office.compare", "office.pdf-merge"],
    );
    await shot("pdf-auswahl");
    handoff = { tool: "files.search", sources: [src("folder", "Entwuerfe")] };
    await go("?context=folder");
    await sheet.waitFor();
    assert.equal(await picker.inputValue(), "files.search");
    await page.keyboard.press("Escape");
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
      true,
    );
    assert.equal(await page.locator(".guide:visible").count(), 0);
    if (!mobile) {
      await page.goto(origin + "/settings/agent");
      const section = page.locator(".pa-admin");
      const limit = section.getByRole("combobox", {
        name: "Antwort versenden",
      });
      await limit.selectOption("never");
      await section
        .getByRole("button", { name: "Speichern", exact: true })
        .click();
      assert.equal(limits["mail.send"], "never");
      await page.setViewportSize({ width: 390, height });
      const clipped = await page.evaluate(cutTextIn);
      assert.deepEqual(
        clipped.filter((line) => line.startsWith("select ")),
        [],
      );
    }
  } catch (e) {
    console.error(
      "Last page",
      page.url(),
      (await page.locator("body").innerText()).slice(-1700),
    );
    console.error(
      "Frames",
      page.frames().map((f) => f.url()),
      "errors",
      errors.slice(-4),
    );
    throw e;
  } finally {
    await ctx.close();
  }
}
async function desktopLauncher() {
  const { ctx, page } = await fixtureFactory(browser, errors)(
    "owner",
    "graphit",
    1440,
    "de",
    {
      nativeShell: true,
      serviceWorkers: "block",
      reducedMotion: "reduce",
      viewport: { width: 1440, height: 950 },
    },
  );
  try {
    await ctx.route("**/api/v1/services/state", (r) =>
      r.fulfill({ json: { agent: true } }),
    );
    await ctx.route("**/api/v1/personal-agent/state", (r) =>
      r.fulfill({
        json: {
          chats: [
            { id: "a", title: "Projekt A", created: time },
            { id: "b", title: "Projekt B", created: time },
          ],
          jobs: [],
          settings: {
            rights: {},
            colors: {},
            theme: "system",
            motion: "off",
            dotSize: 10,
          },
          capabilities: tools,
        },
      }),
    );
    await page.goto(origin + "/");
    await page.locator(".topbar [data-menu-button]").click();
    await page.locator('.launcher [data-key="app:agent"] .prog').click();
    const window = page.frameLocator('iframe[data-app="app:agent"]').last();
    await window.locator(".pa-welcome").waitFor();
    await window.locator(".pa-input textarea").focus();
    await page.keyboard.press("Alt+Shift+2");
    await window
      .getByRole("heading", { name: "Aufträge", exact: true })
      .waitFor();
    await page
      .locator(".topbar .menubar")
      .getByRole("menuitem", { name: "Ansicht", exact: true })
      .click();
    await page
      .locator('.mb-menu [role^="menuitem"]')
      .filter({ hasText: "Einstellungen" })
      .click();
    await window
      .getByRole("heading", { name: "Einstellungen", exact: true })
      .waitFor();
    await window.locator(".pa-pane h1").click();
    await page.keyboard.press("Alt+Shift+N");
    await window.locator(".pa-welcome").waitFor();
    await page.keyboard.press("Alt+Shift+ArrowUp");
    await window
      .locator(".pa-chat-head")
      .getByRole("heading", { name: "Projekt B" })
      .waitFor();
    await page.keyboard.press("Alt+Shift+ArrowUp");
    await window
      .locator(".pa-chat-head")
      .getByRole("heading", { name: "Projekt A" })
      .waitFor();
    await page.keyboard.press("Alt+Shift+ArrowDown");
    await window
      .locator(".pa-chat-head")
      .getByRole("heading", { name: "Projekt B" })
      .waitFor();
    await page.keyboard.press("Alt+Shift+N");
    await window.locator(".pa-welcome").waitFor();
    if (process.env.UI_OUTPUT)
      await page.screenshot({
        path: join(process.env.UI_OUTPUT, engine + "-arbeitsplatz-agent.png"),
      });
  } catch (e) {
    console.error(
      "Launcher keys",
      await page
        .locator(".launcher [data-key]")
        .evaluateAll((nodes) => nodes.map((n) => n.dataset.key)),
    );
    throw e;
  } finally {
    await ctx.close();
  }
}
try {
  for (const [w, h] of [
    [390, 844],
    [1440, 950],
  ])
    if (
      !process.env.UI_LAUNCHER_ONLY &&
      (!process.env.UI_WIDTH || Number(process.env.UI_WIDTH) === w)
    )
      await run(w, h);
  if (!process.env.UI_WIDTH || process.env.UI_WIDTH === "1440")
    await desktopLauncher();
  assert.deepEqual(errors, []);
  console.log(
    engine + " personal Agent navigation, draft, approval and result passed",
  );
} finally {
  await browser.close();
}
