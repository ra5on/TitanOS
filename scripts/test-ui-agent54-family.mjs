// PLAN 54-G „Gemeinschaft und Familie“: the rows in Einstellungen ›
// KI-Agent › Funktionen, the settings of Backup-Buddy, Skill-Katalog and
// Familien-Tagebuch, the views Backup-Buddy (mockup 11) and Skill-Katalog
// (mockup 12) on the phone and the desktop. Isolated API data only: no
// request reaches a NAS, a partner, a catalog, Immich or Frigate.
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
const base = process.env.UI_URL || "http://127.0.0.1:5214";
const out = process.env.UI_OUTPUT || "out/validation/v057-family";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const TB = 1e12;
const GiB = 2 ** 30;
const features = {
  docs: {},
  guard: {},
  handbook: {},
  emergency: {},
  whatIf: {},
  advisor: {},
  buddy: {
    enabled: true,
    address: "nas-eva.tail1234.ts.net:8752",
    port: 8752,
    volume: "Data",
    protectDays: 30,
    every: "weekly",
    weekday: 0,
    time: "02:00",
    probe: "monthly",
    probeFiles: 50,
  },
  catalog: { enabled: true },
  diary: { enabled: false },
};
const pub = {
  docs: false,
  deadlines: false,
  paper: false,
  guard: false,
  handbook: false,
  emergency: false,
  whatIf: false,
  advisor: false,
  buddy: true,
  catalog: true,
  diary: false,
};
const mine = {
  docs: {},
  paper: {},
  diary: {
    off: ["chats"],
    immichUrl: "http://192.168.1.20:2283",
    immichKeySet: true,
    frigateUrl: "http://192.168.1.21:5000",
    photos: { root: "share/Fotos", path: "2026" },
  },
};
const buddy = {
  settings: features.buddy,
  name: "nas-eva",
  partners: [
    {
      id: "nas-tom",
      name: "nas-tom",
      address: "nas-tom.tail1234.ts.net:8752",
      quota: 2 * TB,
      used: 1.1 * TB,
      usedKnown: true,
      theirQuota: 3 * TB,
      shares: ["Documents", "Fotos"],
      paired: "2026-10-01T10:00:00Z",
      job: {
        lastRun: "2026-11-06T01:10:00Z",
        lastOK: "2026-11-06T01:10:00Z",
        bytes: 2.3 * TB,
        running: false,
        next: "2026-11-08T01:00:00Z",
        probe: {
          at: "2026-11-01T02:30:00Z",
          files: 50,
          bytes: 210e6,
          error: "",
        },
      },
    },
  ],
  code: {
    code: "K7Q-4MZ-92X",
    expires: new Date(Date.now() + 9 * 60e3).toISOString(),
  },
  volumes: ["Data", "Fast"],
  shares: ["Documents", "Fotos", "Media"],
  listening: ["100.64.1.2:8752"],
  problem: "",
};
const skill = (id, name, description, sourceName, tools, steps) => ({
  key: (sourceName === "TitanOS-Apps" ? "titanos/" : "a1b2c3/") + id,
  source: sourceName === "TitanOS-Apps" ? "titanos" : "a1b2c3",
  sourceName,
  id,
  name,
  description,
  when: "Wenn die Person danach fragt",
  steps,
  tools,
  digest: id.padEnd(64, "0"),
  unknown: [],
  installed: false,
});
const catalog = {
  skills: [
    skill(
      "steuer",
      "Rechnungen nach Steuerjahr",
      "Sortiert Belege in Steuer/<Jahr>",
      "TitanOS-Apps",
      ["files_search", "file_move", "note_write"],
      "1. In Documents › Post nach Rechnungen des Jahres suchen (files_search).\n2. Jede Rechnung nach Steuer/<Jahr> verschieben (file_move), vorher fragen.\n3. Eine Notiz mit der Liste schreiben (note_write).",
    ),
    skill(
      "immich-dupes",
      "Immich-Duplikate finden",
      "Liest Immich über die API",
      "TitanOS-Apps",
      ["web_read"],
      "1. Duplikate über die Immich-API lesen.",
    ),
    {
      ...skill(
        "strom",
        "Wöchentlicher Strombericht",
        "Home Assistant Statistik",
        "Meine Skills",
        ["ha_statistics"],
        "1. ha_statistics für den Stromzähler lesen.",
      ),
      unknown: ["ha_statistics"],
    },
  ],
};

async function open(width, height) {
  const made = await create("owner", "graphit", width);
  await made.page.setViewportSize({ width, height });
  const seen = { puts: [], posts: [] };
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
        { id: "share/Fotos", name: "Fotos", kind: "share", writable: true },
        {
          id: "share/Documents",
          name: "Documents",
          kind: "share",
          writable: true,
        },
      ]);
    if (!p.startsWith("/agent")) return route.fallback();
    if (p === "/agent/status")
      return json({
        enabled: true,
        allowed: true,
        admin: true,
        model: "gpt-oss:20b",
        volume: true,
        reachable: true,
        models: [],
        running: [],
      });
    if (p === "/agent/settings")
      return json({
        enabled: true,
        ollama: "http://192.168.10.5:11434",
        model: "gpt-oss:20b",
        policy: { preset: "standard" },
        users: [],
      });
    if (p === "/agent/features" && method === "GET") return json(features);
    if (p === "/agent/features" && method === "PUT") {
      seen.puts.push(route.request().postDataJSON());
      return json(route.request().postDataJSON());
    }
    if (p === "/agent/features/me" && method === "GET")
      return json({ mine, nas: pub });
    if (p === "/agent/features/me" && method === "PUT") {
      const body = route.request().postDataJSON();
      seen.puts.push(body);
      return json({
        ...body,
        diary: { ...body.diary, immichKey: undefined, immichKeySet: true },
      });
    }
    if (p === "/agent/buddy") return json(buddy);
    if (p === "/agent/catalog") return json(catalog);
    if (p === "/agent/skills")
      return json({
        skills: [
          {
            id: "s1",
            name: "Rechnungen einsortieren",
            when: "Rechnungen ablegen",
            steps: "1. files_search …",
            updated: "2026-11-05T10:00:00Z",
          },
        ],
      });
    if (method === "POST") {
      seen.posts.push(p);
      return json({ ok: true });
    }
    return json({ error: { code: "not_found", message: "fixture" } }, 404);
  });
  return { ...made, seen };
}

async function noOverflow(page, what) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - innerWidth,
  );
  assert.ok(overflow <= 1, `${what}: horizontal scroll ${overflow}`);
  // Nothing inside an open dialog sticks out of it.
  const outside = await page.evaluate(() => {
    const bad = [];
    for (const d of document.querySelectorAll("dialog[open]")) {
      const box = d.getBoundingClientRect();
      for (const e of d.querySelectorAll("*")) {
        const r = e.getBoundingClientRect();
        if (r.width && (r.right > box.right + 1 || r.left < box.left - 1))
          bad.push(`${e.tagName}.${e.className}`);
      }
    }
    return bad.slice(0, 5);
  });
  assert.deepEqual(outside, [], `${what}: wider than its dialog`);
}

for (const [width, height, tag] of [
  [390, 844, "phone"],
  [1440, 900, "desktop"],
]) {
  const { ctx, page, seen } = await open(width, height);
  await page.goto(base + "/settings/agent?tab=features");
  const group = page.locator(".fgrp").filter({ hasText: "Backup-Buddy" });
  await group.waitFor();
  for (const title of ["Backup-Buddy", "Skill-Katalog", "Familien-Tagebuch"]) {
    assert.ok(
      await group.getByText(title, { exact: true }).isVisible(),
      `row ${title}`,
    );
  }
  assert.equal(
    await group.getByRole("switch", { name: "Familien-Tagebuch" }).isChecked(),
    false,
    "diary off",
  );
  assert.equal(
    await group.getByRole("switch", { name: "Backup-Buddy" }).isChecked(),
    true,
    "buddy on",
  );
  await noOverflow(page, `${tag} features`);
  await group.scrollIntoViewIfNeeded();
  await page.screenshot({ path: `${out}/${tag}-01-funktionen.png` });

  // Backup-Buddy: settings, then the view.
  await group.getByRole("button", { name: /Backup-Buddy/ }).click();
  let dialog = page.getByRole("dialog", { name: "Backup-Buddy" });
  await dialog.waitFor();
  assert.equal(
    await dialog.getByLabel("Adresse dieses NAS für den Partner").inputValue(),
    features.buddy.address,
  );
  await noOverflow(page, `${tag} buddy settings`);
  await page.screenshot({ path: `${out}/${tag}-02-buddy-einstellungen.png` });
  await dialog.getByRole("button", { name: "Backup-Buddy öffnen" }).click();
  const view = page.getByRole("dialog", { name: "Backup-Buddy" }).last();
  await view.getByText("Mit nas-tom gekoppelt").waitFor();
  assert.ok(
    await view.getByText("Ich sichere bei nas-tom").isVisible(),
    "i back up at them",
  );
  assert.ok(
    await view.getByText("nas-tom sichert bei mir").isVisible(),
    "they back up at me",
  );
  assert.ok(await view.getByText("K7Q-4MZ-92X").isVisible(), "pairing code");
  assert.ok(
    await view.getByText(/50 Stichproben gelesen/).isVisible(),
    "probe",
  );
  assert.equal(
    await view.getByText(/token/i).count(),
    0,
    "no token on the page",
  );
  await noOverflow(page, `${tag} buddy view`);
  await page.screenshot({ path: `${out}/${tag}-03-backup-buddy.png` });
  await view.getByText("K7Q-4MZ-92X").scrollIntoViewIfNeeded();
  await page.screenshot({ path: `${out}/${tag}-04-backup-buddy-code.png` });
  // Unpairing asks with a plain dialog, the dangerous button left of "Abbrechen".
  await view.getByRole("button", { name: "Kopplung lösen" }).click();
  const ask = page.getByRole("dialog", { name: "Kopplung mit nas-tom lösen?" });
  await ask.waitFor();
  const [cancelBox, endBox] = await Promise.all([
    ask.getByRole("button", { name: "Abbrechen" }).boundingBox(),
    ask.getByRole("button", { name: "Kopplung lösen" }).boundingBox(),
  ]);
  assert.ok(
    endBox.x < cancelBox.x || endBox.y < cancelBox.y,
    "dangerous button first",
  );
  assert.equal(
    await ask.locator("input[type=text]").count(),
    0,
    "nothing to type",
  );
  await page.screenshot({ path: `${out}/${tag}-05-kopplung-loesen.png` });
  await ask.getByRole("button", { name: "Abbrechen" }).click();
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");

  // Skill-Katalog: list, then the question with the whole content.
  await group.getByRole("button", { name: /Skill-Katalog/ }).click();
  dialog = page.getByRole("dialog", { name: "Skill-Katalog" });
  await dialog.getByRole("button", { name: "Katalog öffnen" }).click();
  const cat = page.getByRole("dialog", { name: "Skill-Katalog" }).last();
  await cat.getByText("Rechnungen nach Steuerjahr").click();
  await cat.getByText("Rechnungen nach Steuerjahr installieren?").waitFor();
  assert.ok(
    await cat.getByText(/files_search, file_move, note_write/).isVisible(),
    "tools shown",
  );
  assert.ok(
    await cat.getByText(/Jede Rechnung nach Steuer/).isVisible(),
    "steps shown",
  );
  assert.ok(await cat.getByText("aus TitanOS-Apps").isVisible(), "source shown");
  assert.ok(
    await cat.getByText(/Home Assistant Statistik · Meine Skills/).isVisible(),
    "own source shown",
  );
  await noOverflow(page, `${tag} catalog`);
  await page.screenshot({ path: `${out}/${tag}-06-skill-katalog.png` });
  await cat.getByRole("button", { name: "Installieren" }).click();
  await cat.getByText("Rechnungen nach Steuerjahr ist installiert.").waitFor();
  assert.ok(seen.posts.includes("/agent/catalog/install"), "install posted");
  // A skill with tools this account does not have cannot be installed.
  await cat.getByText("Wöchentlicher Strombericht").click();
  assert.ok(
    await cat.getByRole("button", { name: "Installieren" }).isDisabled(),
    "unknown tool refused",
  );
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");

  // Familien-Tagebuch: the account's own sources; the key never comes back.
  await group.getByRole("button", { name: /Familien-Tagebuch/ }).click();
  dialog = page.getByRole("dialog", { name: "Familien-Tagebuch" });
  await dialog.waitFor();
  assert.equal(
    await dialog.getByLabel("API-Schlüssel").inputValue(),
    "",
    "key not shown",
  );
  assert.equal(
    await dialog
      .getByRole("checkbox", { name: /Chats mit dem Agenten/ })
      .isChecked(),
    false,
    "chats off",
  );
  await noOverflow(page, `${tag} diary settings`);
  await page.screenshot({
    path: `${out}/${tag}-07-tagebuch-einstellungen.png`,
  });
  await dialog.getByRole("checkbox", { name: /Chats mit dem Agenten/ }).check();
  await dialog.getByRole("button", { name: "Speichern" }).click();
  await dialog.getByText("Gespeichert.").waitFor();
  const sent = seen.puts.at(-1);
  assert.deepEqual(sent.diary.off, [], "chats back on");
  assert.equal(sent.diary.immichKey, undefined, "no key sent without a change");
  await ctx.close();
}

// Without a code shown: offer space and shares, show a code or enter one.
{
  buddy.code = null;
  const { ctx, page, seen } = await open(390, 844);
  await page.goto(base + "/settings/agent?tab=features");
  const group = page.locator(".fgrp").filter({ hasText: "Backup-Buddy" });
  await group.getByRole("button", { name: /Backup-Buddy/ }).click();
  await page
    .getByRole("dialog", { name: "Backup-Buddy" })
    .getByRole("button", { name: "Backup-Buddy öffnen" })
    .click();
  const view = page.getByRole("dialog", { name: "Backup-Buddy" }).last();
  const pair = view.getByRole("button", { name: "Koppeln" });
  await pair.scrollIntoViewIfNeeded();
  assert.ok(await pair.isDisabled(), "pair needs address and code");
  await view
    .getByLabel("Adresse des anderen TitanOS")
    .fill("nas-tom.tail1234.ts.net");
  await view.getByLabel("Code").fill("k7q4mz92x");
  assert.equal(
    await view.getByLabel("Code").inputValue(),
    "K7Q-4MZ-92X",
    "code grouped",
  );
  await noOverflow(page, "phone pairing form");
  await page.screenshot({ path: `${out}/phone-08-koppeln.png` });
  await pair.click();
  await page.waitForTimeout(200);
  assert.ok(seen.posts.includes("/agent/buddy/pair"), "pair posted");
  await ctx.close();
}

assert.deepEqual(errors, [], errors.join("\n"));
console.log("agent54 family: ok", out);
await browser.close();
