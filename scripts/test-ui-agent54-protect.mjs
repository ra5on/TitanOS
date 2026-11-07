// PLAN 54-S „Schutz“: the rows in Funktionen and the three views
// (Ransomware-Warnung, Notfallhandbuch, Notfallzugang) on the phone and the
// desktop, with a mocked API only (isolated example data, no NAS).
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
const base = process.env.UI_URL || "http://127.0.0.1:5212";
const out = process.env.UI_OUTPUT || "out/validation/v057-protect";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const now = new Date();
const ago = (min) => new Date(now.getTime() - min * 60000).toISOString();
const inDays = (d) => new Date(now.getTime() + d * 86400000).toISOString();

const files = Array.from({ length: 40 }, (_, i) => {
  const was =
    i === 0
      ? "Post/Telekom 2026-10.pdf"
      : i === 1
        ? "Steuer/2025/Belege.xlsx"
        : `Post/Brief ${i}.pdf`;
  return { was, now: was + ".locked", kind: 1 };
});
const alert = {
  id: "20261106-094200-abc123",
  share: "Documents",
  volume: "Fast",
  reason: "known-extension",
  mode: "lock",
  client: {
    user: "lena",
    ip: "192.168.1.57",
    machine: "PC-Wohnzimmer",
    process: "smbd",
    pid: 4242,
  },
  first: ago(14),
  last: ago(12),
  count: 1318,
  more: 1278,
  files,
  state: "stopped",
  locked: true,
  btrfs: true,
  snapshot: "agent-20261106-093000",
  snapshotAt: ago(26),
  held: "guard-hold-20261106-094200",
  restorable: 1318,
};
const features = {
  docs: {},
  whatIf: {},
  advisor: {},
  buddy: {},
  catalog: {},
  diary: {},
  guard: {
    enabled: true,
    mode: "lock",
    known: 5,
    renames: 30,
    rewrites: 30,
    notes: 3,
    windowSec: 60,
    quietSec: 120,
    exclude: [],
  },
  handbook: { enabled: true, contacts: [] },
  emergency: {
    enabled: true,
    persons: [
      { account: "lena", waitDays: 3, shares: ["Documents"] },
      { account: "max", waitDays: 7, shares: ["Fotos"] },
    ],
  },
};
const pub = {
  docs: false,
  deadlines: false,
  paper: false,
  guard: true,
  handbook: true,
  emergency: true,
  whatIf: false,
  advisor: false,
  buddy: false,
  catalog: false,
  diary: false,
};
const handbook = {
  made: ago(60 * 7),
  volumes: [
    {
      name: "Data",
      line: "RAID 1 · ext4 · WD Red 8 TB (WD-CA1B2C3), WD Red 8 TB (WD-CA4D5E6)",
    },
    {
      name: "Fast",
      line: "RAID 1 · btrfs · Samsung 990 1 TB (S6Z1NX0), Samsung 990 1 TB (S6Z1NX1)",
    },
  ],
  facts: {
    host: "nas-haupt",
    shares: [
      { name: "Documents" },
      { name: "Fotos" },
      { name: "Backup" },
      { name: "KI-Agent" },
    ],
    apps: [{ name: "Immich" }, { name: "Paperless" }],
    vms: [{ name: "Home Assistant (VM HAOS)" }],
    network: [{ name: "br0", detail: "192.168.1.20/24" }],
    remote: [{ name: "Tailscale", detail: "nas-haupt.tailnet" }],
    backups: [{ name: "Fotos nach USB", detail: "usb: WD Elements" }],
  },
  sections: [
    {
      id: "disk",
      title: "… eine Platte ausfällt",
      sub: "welche Platte, welches Ersatzteil",
      steps: [
        "Ruhe bewahren und nichts formatieren.",
        "Unter Speicher die Platte mit der Seriennummer suchen.",
        "Ersatz: gleiche oder größere Kapazität.",
        "NAS herunterfahren, Platte tauschen, „Platte ersetzen“ wählen.",
        "Bis der Wiederaufbau fertig ist, keine zweite Platte ziehen.",
        "RAID ersetzt keine Datensicherung.",
        "Danach prüfen, ob die Sicherungen weiterlaufen.",
      ],
    },
    {
      id: "boot",
      title: "… das NAS nicht startet",
      sub: "Stick, Boot-Rückfall, Ersatzstick",
      steps: [
        "Bildschirm anschließen.",
        "Im Startmenü den anderen Eintrag wählen.",
      ],
    },
    {
      id: "stick",
      title: "… der Stick kaputt ist",
      sub: "neuer Stick mit den Einstellungen",
      steps: ["Release laden und auf einen Stick TITANOS entpacken."],
    },
    {
      id: "data",
      title: "… Daten zurückgeholt werden müssen",
      sub: "wo die Backups liegen, wie zurückstellen",
      steps: ["Einstellungen › Datensicherung › Zurückstellen."],
    },
  ],
};
const emergency = {
  enabled: true,
  code: false,
  now: now.toISOString(),
  persons: [
    { account: "lena", waitDays: 3, shares: ["Documents"], codeSet: false },
    { account: "max", waitDays: 7, shares: ["Fotos"], codeSet: false },
  ],
  requests: [
    {
      id: "r1",
      account: "lena",
      reason:
        "Komme nicht an die Versicherungsunterlagen, Papa ist im Krankenhaus.",
      created: ago(25),
      due: inDays(3 - 25 / 1440),
      state: "waiting",
      shares: ["Documents"],
    },
  ],
};

async function open(width, height) {
  const made = await create("owner", "graphit", width);
  await made.page.setViewportSize({ width, height });
  const seen = { posts: [] };
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const json = (value, status = 200) =>
      route.fulfill({ status, json: value });
    if (p === "/shares" && method === "GET")
      return json(
        ["Documents", "Fotos", "Backup", "KI-Agent"].map((name) => ({
          name,
          volume: "Data",
          access: {},
        })),
      );
    if (p === "/users" && method === "GET")
      return json([
        { name: "alex", role: "owner" },
        { name: "lena", role: "user" },
        { name: "max", role: "user" },
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
        users: ["lena"],
        policy: { preset: "standard" },
      });
    if (p === "/agent/features" && method === "GET") return json(features);
    if (p === "/agent/features" && method === "PUT")
      return json(route.request().postDataJSON());
    if (p === "/agent/features/me")
      return json({ mine: { docs: {}, paper: {}, diary: {} }, nas: pub });
    if (p === "/agent/protect/me")
      return json({
        guard: true,
        handbook: true,
        emergency: false,
        alerts: 1,
        requests: 1,
      });
    if (p === "/agent/protect/guard")
      return json({
        enabled: true,
        mode: "lock",
        watching: true,
        error: "",
        overflows: 0,
        alerts: [alert],
      });
    if (p.startsWith("/agent/protect/guard/") && method === "POST") {
      seen.posts.push(p);
      return json({ ...alert, state: "restored" });
    }
    if (p === "/agent/protect/handbook") return json(handbook);
    if (p === "/agent/protect/emergency") return json(emergency);
    if (p.startsWith("/agent/protect/emergency/") && method === "POST") {
      seen.posts.push(p);
      return json(emergency.requests[0]);
    }
    return json({});
  });
  await made.page.goto(base + "/settings/agent?tab=features");
  await made.page.getByText("Ransomware-Wächter").first().waitFor();
  return { ...made, seen };
}

async function noOverflow(page, label) {
  const w = await page.evaluate(() => ({
    doc: document.documentElement.scrollWidth,
    view: window.innerWidth,
  }));
  assert.ok(
    w.doc <= w.view + 1,
    `${label}: page scrolls sideways (${w.doc} > ${w.view})`,
  );
  // No text sticks out of the sheet.
  const out = await page.evaluate(() => {
    const sheet = document.querySelector(".sheet");
    if (!sheet) return 0;
    const r = sheet.getBoundingClientRect();
    return [...sheet.querySelectorAll("strong, small, p, button, h3")].filter(
      (el) => {
        const b = el.getBoundingClientRect();
        return b.width > 0 && (b.right > r.right + 1 || b.left < r.left - 1);
      },
    ).length;
  });
  assert.equal(out, 0, `${label}: text outside the sheet`);
}

for (const [width, height, tag] of [
  [390, 844, "phone"],
  [1440, 900, "desktop"],
]) {
  const { page, ctx, seen } = await open(width, height);
  await page.screenshot({
    path: `${out}/01-funktionen-${tag}.png`,
    fullPage: tag === "phone",
  });
  assert.ok(
    await page.getByText("Sperren und melden").first().isVisible(),
    "guard mode in the row",
  );

  // 06 Ransomware-Warnung
  await page
    .getByRole("button", { name: /Ransomware-Wächter/ })
    .first()
    .click();
  await page.getByText("Freigabe „Documents“ gesperrt").waitFor();
  for (const text of [
    "1.318 Dateien",
    "PC-Wohnzimmer (192.168.1.57)",
    "Konto „lena“",
    "Post/Telekom 2026-10.pdf.locked",
    "vorher Belege.xlsx",
    "… 1.316 weitere",
    "Zurückstellen",
    "Freigabe wieder öffnen",
    "Fehlalarm",
  ]) {
    assert.ok(
      await page.getByText(text, { exact: false }).first().isVisible(),
      `guard view shows ${text}`,
    );
  }
  await noOverflow(page, `guard ${tag}`);
  await page.screenshot({ path: `${out}/06-ransomware-${tag}.png` });
  await page
    .getByRole("button", { name: "Zurückstellen", exact: true })
    .click();
  const dialog = page.locator("dialog[open]");
  await dialog.waitFor();
  assert.ok(
    (await dialog.textContent()).includes("Papierkorb"),
    "restore asks once and says where the files go",
  );
  await page.screenshot({
    path: `${out}/06b-zurueckstellen-dialog-${tag}.png`,
  });
  await dialog.getByRole("button", { name: "Zurückstellen" }).click();
  await page.waitForTimeout(200);
  assert.ok(
    seen.posts.some((p) => p.endsWith("/restore")),
    "restore was sent",
  );
  await page.getByRole("button", { name: "Zurück", exact: true }).click();

  // 07 Notfallhandbuch
  await page
    .getByRole("button", { name: /Notfallhandbuch/ })
    .first()
    .click();
  await page.getByText("Volume Data").waitFor();
  for (const text of [
    "WD-CA4D5E6",
    "Documents, Fotos, Backup, KI-Agent",
    "Tailscale (nas-haupt.tailnet)",
    "… eine Platte ausfällt",
    "PDF",
  ]) {
    assert.ok(
      await page.getByText(text, { exact: false }).first().isVisible(),
      `handbook shows ${text}`,
    );
  }
  await noOverflow(page, `handbook ${tag}`);
  await page.screenshot({ path: `${out}/07-notfallhandbuch-${tag}.png` });
  await page.getByRole("button", { name: /eine Platte ausfällt/ }).click();
  assert.ok(await page.getByText("Ruhe bewahren").isVisible(), "steps open");
  await page.getByRole("button", { name: "Zurück", exact: true }).click();

  // 08 Notfallzugang
  await page
    .getByRole("button", { name: /^.*Notfallzugang/ })
    .first()
    .click();
  await page.getByText("lena beantragt den Notfallzugang").waitFor();
  for (const text of [
    "Papa ist im Krankenhaus",
    "Ablehnen",
    "Jetzt freigeben",
    "Eingetragene Personen",
    "Wartezeit 7 Tage",
    "Aktivitätsprotokoll",
  ]) {
    assert.ok(
      await page.getByText(text, { exact: false }).first().isVisible(),
      `emergency shows ${text}`,
    );
  }
  assert.ok(
    await page.getByRole("progressbar").isVisible(),
    "waiting time as a bar with a label",
  );
  await noOverflow(page, `emergency ${tag}`);
  await page.screenshot({ path: `${out}/08-notfallzugang-${tag}.png` });
  await page.getByText("Einstellungen").last().scrollIntoViewIfNeeded();
  await page.screenshot({
    path: `${out}/08b-notfallzugang-einstellungen-${tag}.png`,
  });
  await page.getByRole("button", { name: "Jetzt freigeben" }).click();
  await page.locator("dialog[open]").waitFor();
  await page
    .locator("dialog[open]")
    .getByRole("button", { name: "Abbrechen" })
    .click();
  await page.getByRole("button", { name: "Ablehnen" }).click();
  await page.waitForTimeout(200);
  assert.ok(
    seen.posts.some((p) => p.endsWith("/r1/decline")),
    "decline was sent",
  );
  await ctx.close();
}

// The person's side (lena, a user): ask for the emergency access in the
// agent app (Einstellungen › Funktionen), then waiting.
{
  const made = await create("user", "graphit", 390);
  const { page, ctx } = made;
  await page.setViewportSize({ width: 390, height: 844 });
  let request = null;
  await ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const json = (value, status = 200) =>
      route.fulfill({ status, json: value });
    if (!p.startsWith("/agent")) return route.fallback();
    if (p === "/agent/status")
      return json({
        enabled: true,
        allowed: true,
        admin: false,
        model: "gpt-oss:20b",
        volume: true,
        reachable: true,
        models: [],
        running: [],
      });
    if (p === "/agent/chats") return json({ chats: [] });
    if (p === "/agent/projects") return json({ projects: [] });
    if (p === "/agent/features/me")
      return json({ mine: { docs: {}, paper: {}, diary: {} }, nas: pub });
    if (p === "/agent/protect/me")
      return json({
        guard: false,
        handbook: false,
        emergency: true,
        person: { account: "lena", waitDays: 3, shares: ["Documents"] },
        codes: false,
        ...(request ? { request } : {}),
      });
    if (p === "/agent/protect/emergency/request" && method === "POST") {
      const body = route.request().postDataJSON();
      request = {
        id: "r9",
        account: "lena",
        reason: body.reason,
        created: now.toISOString(),
        due: inDays(3),
        state: "waiting",
        shares: ["Documents"],
      };
      return json(request, 201);
    }
    if (p.endsWith("/events"))
      return route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "event: end\ndata: {}\n\n",
      });
    return json({});
  });
  await page.goto(base + "/agent");
  await page
    .getByRole("button", { name: /Einstellungen/ })
    .first()
    .click();
  await page
    .getByRole("button", { name: /Funktionen/ })
    .first()
    .click();
  await page
    .getByRole("button", { name: /Notfallzugang/ })
    .first()
    .click();
  await page.getByText("Im Notfall kannst du hier").waitFor();
  await noOverflow(page, "person form");
  await page.screenshot({ path: `${out}/08c-notfallzugang-person-phone.png` });
  await page.locator("#em-reason").fill("Papa ist im Krankenhaus.");
  await page.getByRole("button", { name: "Beantragen", exact: true }).click();
  await page.getByText("Dein Antrag läuft").waitFor();
  await noOverflow(page, "person waiting");
  await page.screenshot({
    path: `${out}/08d-notfallzugang-person-wartet-phone.png`,
  });
  await ctx.close();
}

await browser.close();
const relevant = errors.filter((e) => !/favicon|ResizeObserver/.test(e));
assert.deepEqual(relevant, [], "console errors");
console.log(`protect UI ok, images in ${out}`);
