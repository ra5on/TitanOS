// PLAN 54-D, group "Dokumente und Fristen": the rows in Funktionen, the
// settings of the document memory and of the paperwork autopilot, the
// deadlines found and the paperwork overview, on the phone (390×844) and
// the desktop (1440×900, a page next to the side bar, PLAN 72). Isolated API data only: no request
// reaches a NAS, a mailbox or an Ollama server.
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
const base = process.env.UI_URL || "http://127.0.0.1:5211";
const out = process.env.UI_OUTPUT || "out/validation/v057-docs";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const at = (d, h = 10, m = 0) =>
  new Date(Date.UTC(2026, 10, d, h, m)).toISOString();
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
  version: "0.57.0",
  models: [
    {
      name: "gpt-oss:20b",
      size: 1,
      capabilities: ["completion", "tools"],
      contextLength: 131072,
      levels: ["low", "medium", "high"],
      defaultLevel: "medium",
    },
  ],
  running: [],
};
const features = {
  docs: { enabled: true, deadlines: true, paper: false },
  guard: {},
  handbook: {},
  emergency: {},
  whatIf: {},
  advisor: {},
  buddy: {},
  catalog: {},
};
const pub = {
  docs: true,
  deadlines: true,
  paper: true,
  guard: false,
  handbook: false,
  emergency: false,
  whatIf: false,
  advisor: false,
  buddy: false,
  catalog: false,
};
const mine = {
  docs: {
    on: true,
    custom: true,
    deadlines: true,
    folders: [
      { root: "share/KI-Agent", path: "Dokumente" },
      { root: "share/Documents", path: "Verträge" },
      { root: "share/Documents", path: "Post" },
    ],
  },
  paper: {
    on: true,
    mail: {
      enabled: true,
      host: "imap.example.org",
      user: "rechnungen@example.org",
      passwordSet: true,
    },
  },
};
const docs = {
  at: "03:00",
  running: true,
  folders: [
    {
      root: "share/KI-Agent",
      path: "Dokumente",
      files: 214,
      lastRun: at(6, 2, 12),
      state: "done",
    },
    {
      root: "share/Documents",
      path: "Verträge",
      files: 88,
      lastRun: at(6, 2, 14),
      state: "done",
    },
    {
      root: "share/Documents",
      path: "Post",
      files: 1402,
      state: "running",
      percent: 61,
      pending: 3,
    },
  ],
  paper: {
    folder: "KI-Agent › Eingang",
    waiting: 0,
    ask: 1,
    mailAt: at(6, 9, 40),
  },
};
const entry = (id, name, extra) => ({
  id,
  name,
  state: "filed",
  origin: "inbox",
  root: "share/Documents",
  path: "Post/" + name,
  at: new Date(Date.now() - 2 * 3600e3).toISOString(),
  type: "other",
  ...extra,
});
const paper = {
  month: "2026-11",
  months: ["2026-09", "2026-10", "2026-11"],
  total: 128437,
  count: 12,
  prevTotal: 119110,
  types: [
    { type: "energy", cents: 17200, count: 2, senders: ["Stadtwerke"] },
    {
      type: "telecom",
      cents: 7998,
      count: 2,
      senders: ["Telekom", "congstar"],
    },
    {
      type: "insurance",
      cents: 61240,
      count: 2,
      senders: ["HUK", "Haftpflicht"],
    },
    {
      type: "shopping",
      cents: 41999,
      count: 6,
      senders: ["MediaMarkt", "Amazon", "IKEA"],
    },
  ],
  open: [
    entry("o1", "2026-11-03 Stadtwerke Abschlag.pdf", {
      sender: "Stadtwerke",
      kind: "Abschlag",
      cents: 8600,
      due: "2026-11-15",
      type: "energy",
    }),
    entry("o2", "2026-10-28 Zahnarzt Dr. Weber Rechnung.pdf", {
      sender: "Zahnarzt Dr. Weber",
      cents: 21250,
      due: "2026-11-28",
      type: "health",
    }),
  ],
  recent: [
    entry("r1", "2026-11-04 Telekom Rechnung.pdf", {
      to: "Documents › Post › Telekom",
      type: "telecom",
      sender: "Telekom",
    }),
    entry("r2", "2026-11-03 Stadtwerke Abschlag.md", {
      origin: "mailtext",
      to: "Documents › Post › Stadtwerke",
      type: "energy",
      sender: "Stadtwerke",
    }),
    {
      id: "r3",
      name: "Scan_0042.pdf",
      state: "ask",
      origin: "inbox",
      root: "share/KI-Agent",
      path: "Eingang/Scan_0042.pdf",
      at: at(6),
      type: "health",
      doubt: ["health", "insurance"],
    },
  ],
};
const inbox = [
  {
    id: "i3",
    kind: "deadline",
    title: "Stadtwerke: 86,00 € fällig am 15.11.2026",
    body: "Aus der Mail „Ihre Abschlagsrechnung November“",
    data: {
      deadline: "payment",
      paper: "o1",
      debit: "0",
      root: "share/Documents",
      path: "Post/Stadtwerke/2026-11-03 Stadtwerke Abschlag.md",
      date: "2026-11-15",
    },
    created: at(6),
  },
  {
    id: "i2",
    kind: "deadline",
    title: "Garantie bis 14.03.2027 · Rechnung MediaMarkt 2025-03-14",
    body: "Aus „Rechnung MediaMarkt 2025-03-14.pdf“ (KI-Agent › Dokumente), Seite 1: „2 Jahre Gewährleistung“",
    data: {
      deadline: "warranty",
      root: "share/KI-Agent",
      path: "Dokumente/Rechnung MediaMarkt 2025-03-14.pdf",
      date: "2027-03-14",
    },
    created: at(6),
  },
  {
    id: "i1",
    kind: "deadline",
    title: "Kündigung bis 30.11.2026 · HUK Beitragsrechnung 2026",
    body: "Aus „HUK Beitragsrechnung 2026.pdf“ (Documents › Verträge), Seite 1: „Kündigung spätestens einen Monat vor Ablauf (31.12.)“",
    data: {
      deadline: "cancel",
      root: "share/Documents",
      path: "Verträge/HUK Beitragsrechnung 2026.pdf",
      date: "2026-11-30",
    },
    created: at(6),
  },
];

async function open(width, height) {
  const made = await create("owner", "graphit", width);
  await made.page.setViewportSize({ width, height });
  const seen = { posts: [], puts: [] };
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const json = (value, status = 200) =>
      route.fulfill({ status, json: value });
    if (p === "/services/state")
      return json({ docker: true, vms: true, lxc: true, agent: true });
    if (p === "/files/roots")
      return json([
        {
          id: "share/KI-Agent",
          name: "KI-Agent",
          kind: "share",
          writable: true,
          recycle: true,
        },
        {
          id: "share/Documents",
          name: "Documents",
          kind: "share",
          writable: true,
          recycle: true,
        },
      ]);
    if (!p.startsWith("/agent")) return route.fallback();
    if (p === "/agent/status") return json(status);
    if (p === "/agent/features" && method === "GET") return json(features);
    if (p === "/agent/features" && method === "PUT") {
      seen.puts.push(["nas", route.request().postDataJSON()]);
      return json(route.request().postDataJSON());
    }
    if (p === "/agent/features/me" && method === "GET")
      return json({ mine, nas: pub });
    if (p === "/agent/features/me" && method === "PUT") {
      const body = route.request().postDataJSON();
      seen.puts.push(["mine", body]);
      return json(body);
    }
    if (p === "/agent/docs") return json(docs);
    if (p === "/agent/paper") return json(paper);
    if (p === "/agent/inbox") return json({ items: inbox });
    if (p === "/agent/settings")
      return json({ enabled: true, policy: { preset: "standard" } });
    if (p === "/agent/chats" && method === "GET") return json({ chats: [] });
    if (p === "/agent/projects") return json({ projects: [] });
    if (p.endsWith("/events"))
      return route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "event: end\ndata: {}\n\n",
      });
    if (method === "POST") {
      seen.posts.push(p);
      return json({ ok: true }, 202);
    }
    return json({ items: [], schedules: [], memory: [], skills: [] });
  });
  return { ...made, seen };
}

async function noOverflow(page, what) {
  await page.waitForTimeout(350); // the panel slides in
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - innerWidth,
  );
  const wide =
    overflow > 1
      ? await page.evaluate(() =>
          [...document.querySelectorAll("body *")]
            .filter((e) => e.getBoundingClientRect().right > innerWidth + 1)
            .slice(-6)
            .map((e) => `${e.tagName}.${e.className}`),
        )
      : [];
  assert.ok(
    overflow <= 1,
    `${what}: horizontal scroll ${overflow} ${wide.join(" | ")}`,
  );
  // Nothing in a row lies over its neighbour (owner rule: no overlapping text).
  const clash = await page.evaluate(() => {
    for (const row of document.querySelectorAll(".drow, .frow")) {
      const kids = [...row.children]
        .map((c) => c.getBoundingClientRect())
        .filter((r) => r.width && r.height);
      for (let i = 1; i < kids.length; i++)
        if (
          kids[i].left < kids[i - 1].right - 1 &&
          kids[i].top < kids[i - 1].bottom - 1
        )
          return row.textContent.trim().slice(0, 60);
    }
    return "";
  });
  assert.equal(clash, "", `${what}: overlapping parts in a row`);
}
async function full(page, name) {
  const need = await page.evaluate(() => {
    const b = document.querySelector(".sheet .body");
    return b ? b.scrollHeight + b.getBoundingClientRect().top + 16 : 0;
  });
  const vp = page.viewportSize();
  if (need > vp.height)
    await page.setViewportSize({ width: vp.width, height: Math.ceil(need) });
  await page.waitForTimeout(250);
  await page.screenshot({ path: `${out}/${name}.png` });
  await page.setViewportSize(vp);
}

// ---- phone ----
{
  const { ctx, page, seen } = await open(390, 844);
  await page.goto(base + "/settings/agent?tab=features");
  await page
    .getByText("Dokumenten-Gedächtnis", { exact: true })
    .first()
    .waitFor();
  await noOverflow(page, "functions");
  await page.screenshot({
    path: `${out}/01-funktionen-handy.png`,
    fullPage: true,
  });

  // The memory page (draft 02).
  await page
    .locator(".frow .open", { hasText: "Dokumenten-Gedächtnis" })
    .click();
  const sheet = page.locator(".sheet");
  await sheet.getByText("Documents › Post").waitFor();
  await sheet.getByText("61 %", { exact: false }).waitFor();
  assert.ok(
    await sheet.locator(".tag", { hasText: "läuft" }).isVisible(),
    "running folder marked in words",
  );
  assert.ok(
    (await sheet
      .getByRole("button", { name: "Erst vorschlagen" })
      .getAttribute("aria-pressed")) === "true",
    "suggest by default",
  );
  await noOverflow(page, "memory");
  await full(page, "02-dokumente-handy");
  await sheet.getByRole("button", { name: "Gleich als Erinnerung" }).click();
  await page.waitForTimeout(200);
  const saved = seen.puts.filter(([k]) => k === "mine").at(-1)?.[1];
  assert.equal(saved?.docs?.mode, "remind", "mode saved");

  // The deadlines found (draft 03).
  await sheet.getByRole("button", { name: "Ansehen" }).click();
  await page.getByText("Kündigung bis 30.11.2026", { exact: false }).waitFor();
  assert.equal(await page.locator(".sheet .card").count(), 3);
  assert.ok(
    await page
      .locator(".sheet .card", { hasText: "Stadtwerke" })
      .getByRole("button", { name: "Bezahlt" })
      .isVisible(),
    "paid for a bill",
  );
  assert.ok(
    await page
      .locator(".sheet .card", { hasText: "Garantie" })
      .getByRole("link", { name: "Öffnen" })
      .isVisible(),
    "open for a document",
  );
  await noOverflow(page, "deadlines");
  await full(page, "03-fristen-handy");
  await page
    .locator(".sheet .card", { hasText: "Kündigung" })
    .getByRole("button", { name: "Übernehmen" })
    .click();
  await page.waitForTimeout(200);
  assert.ok(
    seen.posts.includes("/agent/inbox/i1"),
    "Übernehmen decides the suggestion",
  );
  await page.locator(".sheet .back").click();
  await page.locator(".sheet .back").click();

  // The paperwork settings and the overview (draft 04).
  await page
    .locator(".frow .open", { hasText: "Papierkram-Autopilot" })
    .click();
  await page.locator(".sheet").getByText("Eigenes Postfach abholen").waitFor();
  assert.ok(
    !(await page.locator(".sheet input[type=password]").inputValue()),
    "password never shown",
  );
  assert.ok(
    await page
      .locator(".sheet input[type=password]")
      .getAttribute("placeholder"),
    "password marked as set",
  );
  await noOverflow(page, "paper settings");
  await full(page, "04a-papierkram-einstellungen-handy");
  await page.getByRole("button", { name: "Papierkram öffnen" }).click();
  await page.locator(".sheet").getByText("Zuletzt abgelegt").waitFor();
  assert.ok(
    await page.locator(".sheet").getByText("1.284,37 €").isVisible(),
    "sum of the month",
  );
  assert.ok(
    await page
      .locator(".sheet")
      .getByText("Unsicher: Gesundheit oder Versicherungen?")
      .isVisible(),
    "unsure in words",
  );
  await noOverflow(page, "paper");
  await full(page, "04-papierkram-handy");
  await page.getByRole("button", { name: "Zuordnen" }).click();
  await page.getByRole("dialog", { name: /zuordnen/ }).waitFor();
  await page.screenshot({ path: `${out}/04b-zuordnen-handy.png` });
  await page.getByRole("button", { name: "Abbrechen" }).click();
  await page.getByRole("button", { name: "Rückgängig" }).first().click();
  await page.waitForTimeout(200);
  assert.ok(seen.posts.includes("/agent/paper/r1/undo"), "undo posted");
  await ctx.close();
}

// ---- desktop: from the agent's menu, as a page in the main area (PLAN 72) ----
{
  const { ctx, page } = await open(1440, 900);
  await page.goto(base + "/agent?app=1");
  await page.getByRole("button", { name: "Papierkram" }).first().click();
  await page.locator(".sheet").getByText("Ausgaben im November").waitFor();
  const box = await page.locator(".sheet").boundingBox();
  // The page fills the main area; the side bar (264 px wide since PLAN
  // 90-4) stays visible and usable.
  assert.ok(
    box && box.x >= 260 && box.x <= 268 && box.width > 1100,
    `page next to the side bar: ${JSON.stringify(box)}`,
  );
  await page.getByRole("button", { name: "Neuer Chat" }).first().click();
  await page.locator(".sheet").waitFor({ state: "detached" });
  await page.getByRole("button", { name: "Papierkram" }).first().click();
  await page.locator(".sheet").getByText("Ausgaben im November").waitFor();
  await page.waitForTimeout(300);
  await page.screenshot({ path: `${out}/05-papierkram-desktop.png` });
  await ctx.close();
}

await browser.close();
assert.deepEqual(errors, [], errors.join("\n"));
console.log(`agent54 docs UI ok: screenshots in ${out}`);
