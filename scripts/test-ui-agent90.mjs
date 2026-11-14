// KI-Agent, PLAN 90-4: the start page, the pages Gedächtnis, Wissen and
// Einstellungen, changing a message and asking again, the search in a chat,
// the "/" commands, and on the phone the one-line head with its sheet, a
// table as cards and the actions of an own message. Isolated API data only:
// no request reaches a NAS or an Ollama server.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5191";
const out = process.env.UI_OUTPUT || "out/validation/agent90";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const at = (h, m = 0) => new Date(Date.UTC(2026, 10, 5, h, m)).toISOString();
const status = {
  enabled: true,
  allowed: true,
  admin: true,
  model: "gpt-oss:20b",
  think: "medium",
  context: 0,
  volume: true,
  web: true,
  reachable: true,
  models: [
    {
      name: "gpt-oss:20b",
      size: 13793441566,
      capabilities: ["completion", "tools", "thinking"],
      contextLength: 131072,
      levels: ["low", "medium", "high"],
      defaultLevel: "medium",
    },
    {
      name: "qwen3:8b",
      size: 5200000000,
      capabilities: ["completion", "tools"],
      contextLength: 32768,
    },
  ],
  running: [{ name: "gpt-oss:20b", contextLength: 131072, vram: 1 }],
};
const projects = [
  {
    id: "p1aaaaaaaaaa0001",
    name: "Home Assistant",
    icon: "🏠",
    instructions: "Vor Updates immer sichern.",
    created: at(1),
  },
  { id: "p1aaaaaaaaaa0002", name: "NAS-Doku", icon: "📚", created: at(1) },
];
const chat = {
  id: "c9aaaaaaaaaa0001",
  title: "Status Home Assistant",
  project: projects[0].id,
  created: at(10, 40),
  updated: at(10, 42),
  state: "idle",
  origin: "web",
  entries: [
    {
      id: "u1",
      at: at(10, 40),
      role: "user",
      content: "Wie ist der Status von Home Assistant?",
    },
    {
      id: "a1",
      at: at(10, 40),
      role: "assistant",
      content: "Home Assistant läuft zweimal. Welche meinst du?",
    },
    { id: "u2", at: at(10, 41), role: "user", content: "Beide, mit Update" },
    {
      id: "a2",
      at: at(10, 42),
      role: "assistant",
      content:
        "| Installation | Zustand | Version | Sicherung |\n|---|---|---|---|\n| Docker-App | läuft | 2026.10.4 | heute |\n| VM HAOS | läuft | Update auf 2026.10.4 | vor 9 Tagen |\n\nDas Update der VM spiele ich nach einer Sicherung ein.",
    },
  ],
};
const waiting = {
  id: "c9aaaaaaaaaa0002",
  title: "VM HAOS aktualisieren",
  created: at(11),
  updated: at(11, 42),
  state: "waiting",
  origin: "web",
  preview: "",
};
const ran = {
  id: "c9aaaaaaaaaa0003",
  title: "Gesundheitscheck",
  created: at(8),
  updated: at(8, 2),
  state: "idle",
  origin: "schedule",
  preview: "Alles in Ordnung.",
};
const info = ({ entries: _entries, ...rest }) => ({ preview: "", ...rest });

async function open(width, height, theme = "hell") {
  const made = await create("owner", theme, width);
  await made.page.setViewportSize({ width, height });
  made.data.prefs.appearance = { theme };
  await made.ctx.addInitScript(
    (v) => localStorage.setItem("titanos.appearance", JSON.stringify(v)),
    { theme },
  );
  // What the page sent, to assert on.
  const seen = [];
  let memory = [
    {
      id: "m1",
      text: "Home Assistant läuft als VM HAOS.",
      at: at(10, 42),
      chat: chat.id,
      topic: "Home Assistant",
      pinned: true,
      used: new Date().toISOString(),
    },
    {
      id: "m2",
      text: "Der Zigbee-Stick hängt hinten links.",
      at: "2026-01-12T10:00:00Z",
      topic: "Home Assistant",
    },
    {
      id: "m3",
      text: "Telekom-Rechnungen liegen in Documents › Post.",
      at: at(11, 1),
      untrusted: true,
    },
  ];
  let inbox = [
    {
      id: "i1",
      kind: "fade",
      title: "Der Zigbee-Stick hängt hinten links.",
      body: "Stimmt es noch, oder soll es weg?",
      data: { memory: "m2" },
      created: at(3, 10),
    },
  ];
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const json = (value, code = 200) =>
      route.fulfill({ status: code, json: value });
    const body = () => {
      try {
        return route.request().postDataJSON();
      } catch {
        return null; // a request without a body (DELETE)
      }
    };
    if (p === "/services/state")
      return json({ docker: true, vms: true, lxc: true, agent: true });
    if (p === "/files/roots")
      return json([
        { id: "share/Documents", name: "Documents", kind: "share" },
        { id: "share/Photos", name: "Photos", kind: "share" },
      ]);
    if (p === "/activity")
      return json({
        entries: [
          {
            at: at(9),
            user: "alex",
            action: "file.write",
            target: "Documents/NAS/Übersicht.docx",
            ok: true,
          },
        ],
      });
    if (!p.startsWith("/agent")) return route.fallback();
    if (method !== "GET") seen.push({ method, p, body: body() ?? null });
    if (p === "/agent/status") return json(status);
    if (p === "/agent/settings") return json({ locations: [], ollama: "" });
    if (p === "/agent/projects") return json({ projects });
    if (p === `/agent/projects/${projects[0].id}/files`)
      return json({
        files: [
          {
            root: "share/Backup",
            path: "Home Assistant/haos-2026-11-05.tar",
            chat: chat.id,
            at: at(10, 50),
          },
        ],
      });
    if (p === "/agent/chats" && method === "GET")
      return json({ chats: [info(chat), waiting, ran] });
    if (p === "/agent/chats/" + chat.id && method === "GET") return json(chat);
    if (p === `/agent/chats/${chat.id}/messages`) return json(chat, 202);
    if (p.endsWith("/events"))
      return route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "event: end\ndata: {}\n\n",
      });
    if (p === "/agent/home")
      return json({
        state: {
          at: at(14, 40),
          volumes: [
            {
              name: "Data",
              state: "active",
              mounted: true,
              percent: 71,
              daysLeft: 23,
            },
            { name: "Fast", state: "degraded", mounted: true, percent: 12 },
          ],
          backups: { jobs: 5, failing: 1, running: 0, lastOK: at(3) },
          apps: { running: 12, stopped: 1 },
        },
      });
    if (p === "/agent/inbox") return json({ items: inbox });
    if (p.startsWith("/agent/inbox/")) {
      inbox = [];
      return json({ ok: true });
    }
    if (p === "/agent/suggestions")
      return json({
        suggestions: [
          {
            kind: "update_available",
            title: "TitanOS 0.82 ist da",
            severity: "info",
            time: at(6),
          },
        ],
      });
    if (p === "/agent/schedules")
      return json({
        schedules: [
          {
            id: "sc1",
            name: "Gesundheitscheck",
            task: "Gesundheitscheck",
            every: "weekly",
            at: "08:00",
            weekday: 1,
            enabled: true,
            next: "2026-11-16T07:00:00Z",
            when: "montags 08:00",
            created: at(1),
          },
          {
            id: "sc2",
            name: "Müll rausstellen",
            task: "Müll rausstellen",
            kind: "reminder",
            every: "once",
            enabled: true,
            next: "2026-11-14T18:00:00Z",
            when: "einmal",
            created: at(1),
          },
        ],
      });
    if (p === "/agent/memory" && method === "GET") return json({ memory });
    if (p === "/agent/memory" && method === "DELETE") {
      memory = memory.filter((m) => m.pinned);
      return json({ memory });
    }
    if (p.startsWith("/agent/memory/")) {
      const id = p.split("/").pop();
      if (method === "DELETE") memory = memory.filter((m) => m.id !== id);
      else if (id === "new")
        memory = [...memory, { id: "m9", at: at(12), ...body() }];
      else memory = memory.map((m) => (m.id === id ? { ...m, ...body() } : m));
      return method === "DELETE"
        ? route.fulfill({ status: 204 })
        : json({ memory });
    }
    if (p === "/agent/initiative")
      return json({ level: "suggest", fadeMonths: 6, search: [] });
    if (p === "/agent/skills")
      return json({
        skills: [
          {
            id: "builtin-health",
            name: "Health check",
            when: "Gesundheitscheck",
            steps: "1. system_status\n2. storage_status",
            builtIn: true,
            updated: at(1),
          },
          {
            id: "s1",
            name: "Rechnungen einsortieren",
            when: "Rechnungen ablegen",
            steps: "1. files_search\n2. files_move",
            updated: at(11),
          },
        ],
      });
    if (p === "/agent/profile") return json({ instruction: "Antworte kurz." });
    if (p === "/agent/map") return json({ built: false, map: {} });
    if (p === "/agent/prefs") return json({});
    if (method === "GET")
      return json({ error: { code: "not_found", message: "fixture" } }, 404);
    return json({ ok: true }, 202);
  });
  return { ...made, seen };
}

/** Nothing on the page is wider than the page (no cut-off text). */
async function noOverflow(page, what) {
  const wide = await page.evaluate(() => {
    const w = document.documentElement.clientWidth;
    return [...document.querySelectorAll(".agent *")]
      .filter((el) => {
        const r = el.getBoundingClientRect();
        return (
          r.width > 0 &&
          r.right > w + 1 &&
          !el.closest(".cards, .table, .drawer")
        );
      })
      .map((el) => el.className || el.tagName)
      .slice(0, 5);
  });
  assert.deepEqual(wide, [], `${what}: wider than the page`);
}
/** Waits until the page shows what is expected (it follows input a moment later). */
async function until(what, fn) {
  for (let i = 0; i < 40; i++) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  assert.fail(what);
}
const sent = (seen, method, path) =>
  seen.filter((s) => s.method === method && s.p === path);

// ---------- desktop: the start page ----------
{
  const { ctx, page } = await open(1280, 900);
  await page.goto(base + "/agent");
  await page.getByRole("heading", { level: 1, name: /alex/ }).waitFor();
  const wait = page.locator(".sgroup", { hasText: "Wartet auf dich" });
  await wait.getByText("Freigabe nötig: VM HAOS aktualisieren").waitFor();
  assert.ok(
    await page.getByText("2 Dinge warten auf dich").isVisible(),
    "the greeting counts what waits",
  );
  // The state of the NAS: every row says its state in words, not only color.
  const state = page.locator(".sgroup", { hasText: "NAS-Zustand" });
  for (const [row, text, chip] of [
    ["Speicher", "Data zu 71 % belegt · voll in etwa 23 Tagen", "Achtung"],
    ["Platten und RAID", "Eine Platte fehlt: Fast", "Achtung"],
    ["Sicherungen", "1 von 5 schlagen fehl", "Achtung"],
    ["Apps", "12 Container laufen · 1 gestoppt", "Gut"],
    ["Updates", "TitanOS 0.82 ist da", "1 offen"],
  ]) {
    const r = state.locator("a.srow", { hasText: row }).first();
    assert.ok(
      (await r.textContent()).includes(text),
      `${row}: ${await r.textContent()}`,
    );
    assert.equal((await r.locator(".chip").textContent()).trim(), chip, row);
  }
  assert.equal(
    await state
      .locator("a.srow", { hasText: "Speicher" })
      .first()
      .getAttribute("href"),
    "/storage",
  );
  // Planned next, soonest first; what planned tasks last did.
  const next = page.locator(".sgroup", { hasText: "Als Nächstes geplant" });
  assert.deepEqual(await next.locator(".srow-title").allTextContents(), [
    "Müll rausstellen",
    "Gesundheitscheck",
  ]);
  assert.ok(
    await page
      .locator(".sgroup", { hasText: "Zuletzt erledigt" })
      .getByText("Alles in Ordnung.")
      .isVisible(),
    "last results",
  );
  // The side bar counts approvals and suggestions at "Start".
  const startItem = page.locator("nav .item", { hasText: "Start" }).first();
  assert.equal(await startItem.getAttribute("aria-current"), "page");
  assert.equal((await startItem.locator(".n").textContent()).trim(), "2");
  assert.equal(
    await page.locator("nav .item", { hasText: "Vorschläge" }).count(),
    0,
    "no entry of its own for the suggestions",
  );
  await noOverflow(page, "start");
  await page.screenshot({ path: `${out}/01-start.png` });
  // A suggestion of the agent is answered here.
  await wait.getByRole("button", { name: "Behalten" }).click();
  await wait.getByText("Der Zigbee-Stick").waitFor({ state: "detached" });
  // An approval only leads to its chat; nothing is approved from here.
  assert.equal(await wait.getByRole("button", { name: "Ausführen" }).count(), 0);
  await ctx.close();
}

// ---------- desktop: the chat ----------
{
  const { ctx, page, seen } = await open(1280, 900);
  await page.goto(base + "/agent?chat=" + chat.id);
  await page.getByText("Beide, mit Update").waitFor();
  // Search: hits counted, Enter goes on, Escape closes.
  await page.getByRole("button", { name: "In diesem Chat suchen" }).click();
  const find = page.getByRole("searchbox", { name: "In diesem Chat suchen" });
  await find.fill("update");
  await page.getByText("3 von 3").waitFor();
  await find.press("Enter");
  await page.getByText("1 von 3").waitFor();
  assert.equal(
    await page.evaluate(() => CSS.highlights.get("agent-find-current").size),
    1,
    "the current hit is marked",
  );
  await page.screenshot({ path: `${out}/02-chat-search.png` });
  await find.fill("gibt es nicht");
  await page.getByText("Kein Treffer").waitFor();
  await find.press("Escape");
  await find.waitFor({ state: "detached" });
  assert.equal(
    await page.evaluate(() => CSS.highlights.has("agent-find")),
    false,
    "marks gone with the search",
  );

  // A table can be copied.
  await page.getByRole("button", { name: "Tabelle kopieren" }).waitFor();

  // Neu erzeugen: only at the last answer, asks the last message again.
  const again = page.getByRole("button", { name: "Neu erzeugen" });
  assert.equal(await again.count(), 1);
  await again.click();
  await page.waitForTimeout(300);
  assert.deepEqual(sent(seen, "POST", `/agent/chats/${chat.id}/messages`)[0].body, {
    message: "",
    replace: "u2",
    model: "gpt-oss:20b",
    think: "medium",
  });

  // Bearbeiten: the message becomes a field; sending replaces it.
  await page
    .locator(".me", { hasText: "Wie ist der Status" })
    .getByRole("button", { name: "Bearbeiten", exact: true })
    .click();
  const field = page.getByRole("textbox", { name: "Nachricht ändern" });
  await field.waitFor();
  assert.ok(
    await page
      .getByText("Die Antworten danach werden neu erzeugt.")
      .isVisible(),
  );
  await page.screenshot({ path: `${out}/03-chat-edit.png` });
  await field.fill("Wie ist der Status von Home Assistant und Zigbee?");
  await page.getByRole("button", { name: "Erneut senden" }).click();
  await field.waitFor({ state: "detached" });
  const edits = sent(seen, "POST", `/agent/chats/${chat.id}/messages`);
  assert.equal(edits.length, 2);
  assert.equal(edits[1].body.replace, "u1");
  assert.equal(
    edits[1].body.message,
    "Wie ist der Status von Home Assistant und Zigbee?",
  );

  // "/" opens the commands; arrows and Enter choose.
  const input = page.locator(".composer textarea");
  await input.fill("/");
  const list = page.getByRole("listbox", { name: "Befehle" });
  await list.waitFor();
  assert.deepEqual(
    (await list.locator("b").allTextContents()).map((s) => s.trim()),
    ["/neu", "/projekt", "/modell", "/merke", "/plane", "/suche", "/zustand"],
  );
  await page.screenshot({ path: `${out}/04-chat-commands.png` });
  await input.fill("/me");
  await until(
    "one command fits /me",
    async () => (await list.getByRole("option").count()) === 1,
  );
  await input.press("Enter");
  assert.equal(await input.inputValue(), "/merke ");
  await input.pressSequentially("Der Drucker steht im Flur.");
  await input.press("Enter");
  await page.getByText("Gemerkt.", { exact: true }).waitFor();
  assert.deepEqual(sent(seen, "PUT", "/agent/memory/new")[0].body, {
    text: "Der Drucker steht im Flur.",
  });
  assert.equal(
    sent(seen, "POST", `/agent/chats/${chat.id}/messages`).length,
    2,
    "a command is not sent as a message",
  );
  // /suche opens the search with the words.
  await input.fill("/suche Sicherung");
  await input.press("Enter");
  await page.getByText(/von 2/).waitFor();
  // /modell opens project, model and level.
  await input.fill("/modell");
  await input.press("Enter");
  const sheet = page.getByRole("dialog", {
    name: "Projekt, Modell und Denkstufe",
  });
  await sheet.waitFor();
  await sheet.getByRole("button", { name: /qwen3:8b/ }).click();
  await page.keyboard.press("Escape");
  await sheet.waitFor({ state: "detached" });
  await page.locator(".picker", { hasText: "qwen3:8b" }).waitFor();
  await ctx.close();
}

// ---------- desktop: Gedächtnis ----------
{
  const { ctx, page, seen } = await open(1280, 900);
  await page.goto(base + "/agent?page=memory");
  await page.getByRole("heading", { level: 1, name: /Gedächtnis/ }).waitFor();
  const group = page.locator(".sgroup", { hasText: "Home Assistant" }).first();
  await group.getByText("Home Assistant läuft als VM HAOS.").waitFor();
  // By topic: the pinned one first; origin and last use are written out.
  assert.deepEqual(
    (await group.locator(".srow-title").allTextContents()).map((s) => s.trim()),
    [
      "Home Assistant läuft als VM HAOS.",
      "Der Zigbee-Stick hängt hinten links.",
    ],
  );
  const first = group.locator(".srow").first();
  assert.match(
    await first.textContent(),
    /Chat „Status Home Assistant“.*zuletzt genutzt heute.*angeheftet · verblasst nie/s,
  );
  assert.match(
    await group.locator(".srow").nth(1).textContent(),
    /noch nicht genutzt.*wird bald nachgefragt/s,
  );
  assert.ok(
    await page
      .locator(".sgroup", { hasText: "Ohne Thema" })
      .getByText("aus fremdem Inhalt")
      .isVisible(),
  );
  await noOverflow(page, "memory");
  await page.screenshot({ path: `${out}/05-memory.png` });
  // Search and order.
  await page.getByRole("searchbox", { name: "Gemerktes durchsuchen" }).fill("zigbee");
  await until(
    "the search keeps one memory",
    async () => (await page.locator(".main .swipe").count()) === 1,
  );
  await page.getByRole("searchbox", { name: "Gemerktes durchsuchen" }).fill("");
  await page.getByText("Neueste", { exact: true }).click();
  await until(
    "newest first",
    async () =>
      (
        await page.locator(".main .swipe .srow-title").first().textContent()
      ).trim() === "Telekom-Rechnungen liegen in Documents › Post.",
  );
  await page.getByText("Nach Thema", { exact: true }).click();
  // Pin, change with a topic, delete (asked first).
  const zig = page.locator(".main .swipe", { hasText: "Zigbee-Stick" });
  await zig.getByRole("button", { name: "Anheften" }).click();
  await zig.getByText("angeheftet · verblasst nie").waitFor();
  assert.deepEqual(sent(seen, "PUT", "/agent/memory/m2")[0].body, {
    pinned: true,
  });
  await zig.getByRole("button", { name: "Bearbeiten" }).click();
  await page
    .getByRole("textbox", { name: "Merksatz" })
    .fill("Der Zigbee-Stick hängt vorne.");
  await page.getByRole("combobox", { name: "Thema" }).fill("Geräte");
  await page.getByRole("button", { name: "Speichern" }).click();
  await page.locator(".sgroup", { hasText: "Geräte" }).waitFor();
  assert.deepEqual(sent(seen, "PUT", "/agent/memory/m2")[1].body, {
    text: "Der Zigbee-Stick hängt vorne.",
    topic: "Geräte",
  });
  const tele = page.locator(".main .swipe", { hasText: "Telekom" });
  await tele.getByRole("button", { name: "Löschen" }).click();
  const ask = page.getByRole("dialog", { name: "Merksatz löschen?" });
  await ask.waitFor();
  assert.equal(sent(seen, "DELETE", "/agent/memory/m3").length, 0);
  await ask.getByRole("button", { name: "Löschen" }).click();
  await tele.waitFor({ state: "detached" });
  // The settings of the memory are here: the right end of the slider is
  // "unbegrenzt" and is stored as 0. Reading them writes nothing.
  assert.equal(sent(seen, "PUT", "/agent/initiative").length, 0);
  const fade = page.getByRole("slider", { name: "Altes verblassen lassen" });
  assert.equal(await fade.getAttribute("aria-valuetext"), "6 Monate");
  await fade.fill("25");
  await page.getByText("unbegrenzt", { exact: true }).waitFor();
  await page.waitForTimeout(800);
  assert.equal(sent(seen, "PUT", "/agent/initiative").at(-1).body.fadeMonths, 0);
  // Both left are pinned: nothing to forget. A new one, then emptying
  // keeps what is pinned.
  assert.ok(
    await page.getByRole("button", { name: "Alles vergessen …" }).isDisabled(),
    "nothing to forget while all are pinned",
  );
  await page.getByRole("button", { name: "Merksatz", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Merksatz" })
    .fill("Die USV hängt am Server.");
  await page.getByRole("button", { name: "Speichern" }).click();
  await until(
    "the new memory is listed",
    async () => (await page.locator(".main .swipe").count()) === 3,
  );
  assert.deepEqual(sent(seen, "PUT", "/agent/memory/new")[0].body, {
    text: "Die USV hängt am Server.",
    topic: "",
  });
  await page.getByRole("button", { name: "Alles vergessen …" }).click();
  const clear = page.getByRole("dialog", { name: "Gedächtnis leeren" });
  await clear.getByRole("button", { name: "Alles vergessen" }).click();
  await clear.waitFor({ state: "detached" });
  assert.equal(sent(seen, "DELETE", "/agent/memory").length, 1);
  await until(
    "pinned ones stay",
    async () => (await page.locator(".main .swipe").count()) === 2,
  );
  await ctx.close();
}

// ---------- desktop: Wissen and Einstellungen ----------
{
  const { ctx, page, seen } = await open(1280, 900);
  await page.goto(base + "/agent?page=knowledge");
  await page.getByRole("heading", { level: 1, name: /Wissen/ }).waitFor();
  for (const row of [
    "Meine Anweisung",
    "Projekt-Anweisungen",
    "Health check",
    "Rechnungen einsortieren",
    "NAS-Landkarte",
    "Dokumente",
    "Protokoll",
  ])
    assert.ok(
      await page.locator(".srow-title", { hasText: row }).first().isVisible(),
      row,
    );
  // The search keeps what fits.
  await page.getByRole("searchbox", { name: "Wissen durchsuchen" }).fill("rechnung");
  await page.locator(".srow-title", { hasText: "Health check" }).waitFor({
    state: "detached",
  });
  assert.ok(
    await page
      .locator(".srow-title", { hasText: "Rechnungen einsortieren" })
      .isVisible(),
  );
  await page.getByRole("searchbox", { name: "Wissen durchsuchen" }).fill("");
  // A project opens from here: its chats and the files the agent made.
  await page.locator(".srow-title", { hasText: "Projekt-Anweisungen" }).click();
  await page
    .locator(".srow-sub", { hasText: "Home Assistant" })
    .getByRole("button", { name: "Chats und Dateien" })
    .click();
  const sheet = page.getByRole("dialog").filter({ hasText: "Home Assistant" });
  await sheet.waitFor();
  await sheet.getByRole("tab", { name: /Dateien/ }).click();
  await sheet
    .locator("a.row-btn strong", { hasText: "haos-2026-11-05.tar" })
    .waitFor();
  await page.keyboard.press("Escape");
  await sheet.waitFor({ state: "detached" });
  // A place to look things up in is switched on here.
  await page.locator(".srow-title", { hasText: "Dokumente" }).click();
  await page.locator("label.srow-sub", { hasText: "Photos" }).getByRole("switch").check();
  await page.waitForTimeout(200);
  assert.deepEqual(sent(seen, "PUT", "/agent/initiative").at(-1).body.search, [
    { root: "share/Photos", path: "" },
  ]);
  await noOverflow(page, "knowledge");
  await page.screenshot({ path: `${out}/06-knowledge.png` });

  // Einstellungen: the groups, nothing twice, the parts as pages.
  await page.locator("nav .item", { hasText: "Einstellungen" }).click();
  await page.getByRole("heading", { level: 1, name: /Einstellungen/ }).waitFor();
  assert.deepEqual(
    (await page.locator(".sgroup-title").allTextContents()).map((s) => s.trim()),
    [
      "Modell und Sprache",
      "Verhalten",
      "Dieses Gerät",
      "NAS (nur Besitzer und Administratoren)",
      "Daten",
    ],
  );
  const titles = (await page.locator(".srow-title").allTextContents()).map((s) =>
    s.trim(),
  );
  assert.equal(new Set(titles).size, titles.length, "every entry once");
  for (const gone of ["Geplant", "Gedächtnis", "Skills", "NAS-Landkarte", "Protokoll"])
    assert.ok(!titles.includes(gone), `${gone} is on its own page`);
  await page.screenshot({ path: `${out}/07-settings.png` });
  await page.locator("a.srow", { hasText: "Darstellung" }).click();
  await page.waitForURL(/part=look/);
  await page.getByRole("heading", { level: 1, name: /Darstellung/ }).waitFor();
  await page.getByRole("link", { name: "Einstellungen" }).click();
  await page.waitForURL((u) => !u.search.includes("part="));
  await ctx.close();
}

// ---------- phone ----------
{
  const { ctx, page, seen } = await open(390, 844, "graphit");
  await page.goto(base + "/agent?chat=" + chat.id);
  await page.getByText("Beide, mit Update").waitFor();
  // The head is one line; its buttons are at least 44 px.
  const head = page.locator("header.top");
  assert.ok((await head.boundingBox()).height <= 56, "one line");
  assert.equal(await page.locator(".chat-head").count(), 0);
  for (const b of await head.locator("button").all()) {
    const box = await b.boundingBox();
    assert.ok(
      box.height >= 44 && box.width >= 44,
      `44 px: ${await b.getAttribute("aria-label")}`,
    );
  }
  assert.match(
    await head.locator(".p-title").textContent(),
    /Status Home Assistant.*Home Assistant · gpt-oss:20b/s,
  );
  // A table of four columns is cards to swipe; the answer's buttons are 44 px.
  const cards = page.locator(".cards .card");
  assert.equal(await cards.count(), 2);
  assert.match(await cards.first().textContent(), /Docker-App.*Zustand.*läuft/s);
  assert.ok(await page.getByText("Karte 1 von 2 · zur Seite wischen").isVisible());
  assert.equal(await page.locator(".table.as-cards").isVisible(), false);
  for (const b of await page.locator(".answer .act").all())
    assert.ok((await b.boundingBox()).height >= 44, "44 px under an answer");
  await noOverflow(page, "phone chat");
  await page.screenshot({ path: `${out}/10-phone-chat.png` });

  // A tap on the title: project, model and thinking level from the bottom.
  await head.locator(".p-title").click();
  const sheet = page.getByRole("dialog", {
    name: "Projekt, Modell und Denkstufe",
  });
  await sheet.waitFor();
  for (const b of await sheet.locator(".sh-item").all())
    assert.ok((await b.boundingBox()).height >= 44, "44 px in the sheet");
  await page.waitForTimeout(350);
  await page.screenshot({ path: `${out}/11-phone-head-sheet.png` });
  await sheet.getByRole("button", { name: /NAS-Doku/ }).click();
  await sheet.waitFor({ state: "detached" });
  assert.deepEqual(sent(seen, "PATCH", "/agent/chats/" + chat.id)[0].body, {
    project: projects[1].id,
  });

  // A long press on the own message opens its actions.
  const mine = page.locator(".user", { hasText: "Beide, mit Update" });
  await mine.dispatchEvent("contextmenu");
  const acts = page.getByRole("dialog", { name: "Aktionen der Nachricht" });
  await acts.waitFor();
  assert.deepEqual(
    (await acts.locator(".sh-text").allTextContents()).map((s) => s.trim()),
    [
      "Bearbeiten und erneut senden",
      "Kopieren",
      "Antwort neu erzeugen",
      "In diesem Chat suchen",
      "Das merken",
    ],
  );
  await page.waitForTimeout(350);
  await page.screenshot({ path: `${out}/12-phone-message-sheet.png` });
  await acts.getByRole("button", { name: "Das merken" }).click();
  // … which leads to the memory with the text ready to save.
  await page.waitForURL(/page=memory/);
  assert.equal(
    await page.getByRole("textbox", { name: "Merksatz" }).inputValue(),
    "Beide, mit Update",
  );
  // On a page the head has "back" and the page's name.
  assert.equal(
    (await head.locator("h1").textContent()).trim(),
    "Gedächtnis",
  );
  await noOverflow(page, "phone memory");
  await page.screenshot({ path: `${out}/13-phone-memory.png` });
  await head.getByRole("button", { name: "Zurück" }).click();
  await page.waitForURL((u) => !u.search.includes("page="));
  await page.getByText("Beide, mit Update").waitFor();

  // The input grows with its text.
  const input = page.locator(".composer textarea");
  const small = (await input.boundingBox()).height;
  await input.fill("Zeile eins\nZeile zwei\nZeile drei\nZeile vier");
  assert.ok((await input.boundingBox()).height > small * 2, "the input grows");

  // The start page and the settings fit the phone.
  await page.goto(base + "/agent");
  await page.getByRole("heading", { level: 1, name: /alex/ }).waitFor();
  await noOverflow(page, "phone start");
  await page.screenshot({ path: `${out}/14-phone-start.png` });
  await page.goto(base + "/agent?page=settings");
  await page.locator(".sgroup-title", { hasText: "Dieses Gerät" }).waitFor();
  await noOverflow(page, "phone settings");
  await page.screenshot({ path: `${out}/15-phone-settings.png` });
  await ctx.close();
}

assert.deepEqual(errors, [], "console errors");
await browser.close();
console.log("KI-Agent PLAN 90-4: ok");
