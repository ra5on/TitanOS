// PLAN 54-V "Vorausschau": Was-wäre-wenn ("Durchspielen" on the storage
// page) and the Hardware-Berater, on the phone and the desktop. Isolated API
// data only: no request reaches a NAS, a web search or an Ollama server.
//
//   UI_URL=http://127.0.0.1:5213 PLAYWRIGHT_MODULE=… node scripts/test-ui-agent54-foresight.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5213";
const out = process.env.UI_OUTPUT || "out/validation/v057-foresight";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const now = new Date("2026-11-06T09:00:00Z");
const iso = (d) => new Date(now.getTime() + d * 3600e3).toISOString();
const smart = {
  healthy: true,
  temperatureC: 38,
  powerOnHours: 36800,
  errors: 0,
  rotationRate: 5400,
  wearPercent: -1,
};
const disk = (name, serial, extra = {}) => ({
  identity: "id-" + name,
  name,
  path: "/dev/" + name,
  serial,
  model: "WDC WD80EFZZ",
  sizeBytes: 8e12,
  kind: "hdd",
  role: "member",
  volume: "Data",
  memberState: "active",
  mounted: true,
  testing: false,
  smart,
  ...extra,
});
const storage = {
  volumes: [
    {
      name: "Data",
      level: "raid1",
      filesystem: "ext4",
      mountPoint: "/mnt/Data",
      mounted: true,
      state: "ok",
      sizeBytes: 8e12,
      usedBytes: 5.1e12,
      raidDisks: 2,
      members: [
        { disk: "sda", state: "active" },
        { disk: "sdb", state: "active" },
      ],
      sync: null,
      mismatches: 0,
      shareQuotas: "off",
      syncSpeed: "normal",
    },
  ],
  disks: [
    disk("sda", "WD-CA1B2C3", {
      smartWarnings: ["reallocated"],
      smartDefects: 4,
    }),
    disk("sdb", "WD-CA4D5E6"),
  ],
  diskSleep: { idleMinutes: 0, available: true, errors: [] },
};
const result = (sc) => {
  if (sc.kind === "full")
    return {
      scenario: sc,
      volume: "Data",
      level: "raid1",
      filesystem: "ext4",
      disks: 2,
      dataAtRiskBytes: 0,
      gaps: [],
      safe: [],
      full: {
        freeBytes: 2.9e12,
        usedPct: 63.8,
        dailyBytes: 21e9,
        daysToFull: 138,
        effects: ["writesFail", "agentStops"],
      },
      advice: [
        { code: "checkUsage", volume: "Data" },
        { code: "grow", volume: "Data" },
      ],
    };
  if (sc.kind === "level")
    return {
      scenario: sc,
      volume: "Data",
      level: "raid1",
      filesystem: "ext4",
      disks: 2,
      dataAtRiskBytes: 0,
      gaps: [],
      safe: [],
      compare: {
        from: { level: "raid1", disks: 2, tolerates: 1, usableBytes: 8e12 },
        to: { level: sc.level, disks: 4, tolerates: 2, usableBytes: 16e12 },
        needDisks: 2,
        usedBytes: 5.1e12,
        fits: true,
        way: "newVolume",
      },
      advice: [
        { code: "addDisks", volume: "Data", count: 2, level: sc.level },
        { code: "newVolume", volume: "Data", level: sc.level },
      ],
    };
  const other = sc.disk === "sda" ? "sdb" : "sda";
  return {
    scenario: sc,
    volume: "Data",
    level: "raid1",
    filesystem: "ext4",
    disks: 2,
    disk: sc.disk,
    serial: sc.disk === "sdb" ? "WD-CA4D5E6" : "WD-CA1B2C3",
    outcome: "noRedundancy",
    dataAtRiskBytes: 5.1e12,
    rebuild: { seconds: 39025, bytes: 8e12, bps: 205e6, measured: true },
    risk: {
      level: sc.disk === "sdb" ? "high" : "normal",
      reasons:
        sc.disk === "sdb"
          ? [
              { code: "reallocated", disk: other, value: 4 },
              { code: "aging", disk: other, value: 36800 },
            ]
          : [],
    },
    gaps: [{ share: "Fotos", days: 23, job: "fotos", bytes: 1.9e12 }],
    safe: [
      { share: "Documents", job: "docs", at: iso(-6) },
      { share: "KI-Agent", job: "agent", at: iso(-6) },
      { share: "Home Assistant", job: "ha", at: iso(-6) },
    ],
    advice: [
      { code: "startBackup", job: "fotos", share: "Fotos" },
      { code: "replacement", disk: sc.disk, bytes: 8e12, kind: "hdd" },
    ],
  };
};
const advisorView = (prices = false) => ({
  settings: {
    enabled: true,
    predict: true,
    bottlenecks: true,
    prices,
    monthly: false,
    at: "03:30",
  },
  running: false,
  web: true,
  saved: {
    history: true,
    lastRun: iso(-6),
    report: {
      at: iso(-6),
      findings: [
        {
          area: "disk",
          subject: "sda",
          level: "fail",
          code: "sectorsRising",
          also: ["pendingRising"],
          from: 0,
          to: 4,
          weeks: 6,
          hours: 36800,
          model: "WDC WD80EFZZ",
          kind: "hdd",
          sizeBytes: 8e12,
          volume: "Data",
        },
        {
          area: "memory",
          subject: "memory",
          level: "tight",
          code: "memoryTight",
          percent: 92,
          totalBytes: 17179869184,
          suggestBytes: 17179869184,
        },
        {
          area: "volume",
          subject: "Fast",
          level: "tight",
          code: "volumeFilling",
          percent: 87,
          days: 35,
          bytes: 3.7e9,
          volume: "Fast",
        },
        {
          area: "gpu",
          subject: "RTX 5060 Ti",
          level: "ok",
          code: "gpuOK",
          percent: 79,
          bytes: 12.6e9,
          totalBytes: 16e9,
          ollama: true,
        },
        {
          area: "temperature",
          subject: "temperatures",
          level: "ok",
          code: "tempsOK",
        },
        {
          area: "disk",
          subject: "sdb",
          level: "ok",
          code: "diskOK",
          hours: 9000,
        },
      ],
    },
    ...(prices
      ? {
          offers: {
            sda: [
              {
                title: "WD Red Plus 8 TB (WD80EFPX)",
                url: "https://shop.example/wd80",
                price: "189,90",
              },
            ],
          },
        }
      : {}),
  },
});
const features = {
  docs: {},
  guard: {},
  handbook: {},
  emergency: {},
  buddy: {},
  catalog: {},
  diary: {},
  whatIf: { enabled: true },
  advisor: {
    enabled: true,
    predict: true,
    bottlenecks: true,
    prices: false,
    monthly: false,
    at: "03:30",
  },
};
const pub = {
  docs: false,
  deadlines: false,
  paper: false,
  guard: false,
  handbook: false,
  emergency: false,
  whatIf: true,
  advisor: true,
  buddy: false,
  catalog: false,
  diary: false,
};

async function open(width, height, { whatIfOn = true, prices = false } = {}) {
  const made = await create("owner", "graphit", width);
  await made.page.setViewportSize({ width, height });
  const seen = { whatif: [], runs: 0, backups: [], puts: [] };
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const json = (value, status = 200) =>
      route.fulfill({ status, json: value });
    if (p === "/storage") return json(storage);
    if (p === "/agent/whatif" && method === "GET")
      return json({
        enabled: whatIfOn,
        levels: ["raid1", "raid5", "raid6", "raid10", "raid1c3"],
      });
    if (p === "/agent/whatif" && method === "POST") {
      const sc = route.request().postDataJSON();
      seen.whatif.push(sc);
      return json(result(sc));
    }
    if (p === "/agent/advisor") return json(advisorView(prices));
    if (p === "/agent/advisor/run") {
      seen.runs++;
      return json(advisorView(prices));
    }
    if (p === "/agent/features" && method === "GET") return json(features);
    if (p === "/agent/features" && method === "PUT") {
      seen.puts.push(route.request().postDataJSON());
      return json(route.request().postDataJSON());
    }
    if (p === "/agent/features/me")
      return json({ mine: { docs: {}, paper: {}, diary: {} }, nas: pub });
    if (/^\/backups\/[^/]+\/run$/.test(p)) {
      seen.backups.push(p);
      return json({});
    }
    return route.fallback();
  });
  return { ...made, seen };
}

async function noOverflow(page, what) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - innerWidth,
  );
  assert.ok(overflow <= 1, `${what}: horizontal scroll ${overflow}`);
  // Nothing in the sheet runs out of it.
  const wide = await page.evaluate(() => {
    const sheet = document.querySelector("dialog.fsheet[open]");
    if (!sheet) return [];
    const right = sheet.getBoundingClientRect().right + 1;
    return [...sheet.querySelectorAll("*")]
      .filter((e) => e.getBoundingClientRect().right > right)
      .map((e) => e.tagName + "." + e.className);
  });
  assert.deepEqual(wide, [], `${what}: content wider than the sheet`);
}

async function grow(page) {
  // Long pages as a whole: the window grows to the content.
  const need = await page.evaluate(() => {
    const b = document.querySelector("dialog.fsheet[open] .body");
    return b ? b.scrollHeight + b.getBoundingClientRect().top + 8 : 0;
  });
  const size = page.viewportSize();
  if (need > size.height)
    await page.setViewportSize({ width: size.width, height: Math.ceil(need) });
  await page.waitForTimeout(150);
}

for (const [width, height, tag] of [
  [390, 844, "phone"],
  [1440, 900, "desktop"],
]) {
  // 1. Durchspielen from the volume card: disk sdb fails (Entwurf 09).
  {
    const { ctx, page, seen } = await open(width, height);
    await page.goto(base + "/storage");
    await page.getByRole("button", { name: /^Data/ }).first().click();
    const play = page.getByRole("button", { name: /Durchspielen/ }).first();
    await play.waitFor();
    await play.click();
    const sheet = page.locator("dialog.fsheet[open]");
    await sheet.waitFor();
    await sheet
      .locator("select")
      .selectOption({ label: "Platte sdb fällt aus" });
    await sheet.getByText("bleibt lesbar, aber ohne Schutz").waitFor();
    assert.equal(seen.whatif.at(-1).disk, "sdb");
    const text = await sheet.innerText();
    for (const want of [
      "Durchspielen: Platte sdb fällt aus",
      "sdb (WD-CA4D5E6)",
      "5,1 TB",
      "Wiederaufbau",
      "gemessen: 205 MB/s",
      "Risiko in dieser Zeit",
      "hoch",
      "sda hat 4 umgelagerte Sektoren",
      "4,2 Jahre",
      "Backup-Lücke",
      "„Fotos“ (1,9 TB) hat seit 23 Tagen kein Backup",
      "Gesichert",
      "Documents",
      "Jetzt Backup von „Fotos“ starten",
      "Ersatzplatte bereitlegen",
      "CMR",
      "Nur gerechnet, am NAS wurde nichts geändert.",
    ])
      assert.ok(text.includes(want), `${tag} whatif lacks ${want}:\n${text}`);
    // Status words, not colour alone.
    assert.ok(await sheet.locator(".ftag", { hasText: "Lücke" }).count());
    await noOverflow(page, `${tag} whatif`);
    await grow(page);
    await page.screenshot({ path: `${out}/${tag}-09-durchspielen.png` });
    // "Starten" starts the backup job through its own route, once.
    await sheet.getByRole("button", { name: "Starten" }).click();
    await sheet.getByRole("button", { name: "Gestartet" }).waitFor();
    assert.deepEqual(seen.backups, ["/backups/fotos/run"]);
    // Other scenarios of the same volume.
    await sheet
      .locator("select")
      .selectOption({ label: "Volume Data wird voll" });
    await sheet.getByText("ist zu 64 % voll").waitFor();
    assert.ok((await sheet.innerText()).includes("voll in etwa 138 Tagen"));
    await sheet.locator("select").selectOption({ label: "Was bringt RAID 6?" });
    await sheet.getByText("RAID 6 statt RAID 1 (Spiegel)").waitFor();
    assert.ok(
      (await sheet.innerText()).includes("2 weitere Platte(n) für RAID 6"),
    );
    await noOverflow(page, `${tag} level`);
    await page.screenshot({ path: `${out}/${tag}-09b-raid6.png` });
    await ctx.close();
  }
  // 2. Off: no button on the storage page.
  {
    const { ctx, page } = await open(width, height, { whatIfOn: false });
    await page.goto(base + "/storage");
    await page.getByRole("button", { name: /^Data/ }).first().click();
    await page
      .getByRole("link", { name: "Speicherverbrauch" })
      .first()
      .waitFor()
      .catch(() => {});
    await page.waitForTimeout(400);
    assert.equal(
      await page.getByRole("button", { name: /Durchspielen/ }).count(),
      0,
      `${tag}: button while off`,
    );
    await ctx.close();
  }
  // 3. The functions in the settings and the Hardware-Berater (Entwurf 10).
  {
    const { ctx, page, seen } = await open(width, height, {
      prices: width > 400,
    });
    await page.goto(base + "/settings/agent?tab=features");
    const row = page.getByRole("button", { name: /Hardware-Berater/ }).first();
    await row.waitFor();
    await page.screenshot({ path: `${out}/${tag}-01-funktionen.png` });
    await row.click();
    const sheet = page.locator("dialog.fsheet[open]");
    await sheet.getByText("Plattenausfall vorhersagen").waitFor();
    const switches = sheet.getByRole("switch");
    assert.equal(await switches.count(), 5);
    assert.equal(
      await sheet.getByRole("switch", { name: "Ersatz mit Preis" }).isChecked(),
      false,
    );
    assert.equal(
      await sheet.getByRole("switch", { name: "Monatsbericht" }).isChecked(),
      false,
    );
    assert.equal(
      await sheet
        .getByRole("switch", { name: "Engpässe erkennen" })
        .isChecked(),
      true,
    );
    await noOverflow(page, `${tag} advisor settings`);
    await page.waitForTimeout(500);
    await page.screenshot({
      path: `${out}/${tag}-10a-berater-einstellungen.png`,
    });
    await sheet.getByRole("switch", { name: "Monatsbericht" }).click();
    await page.waitForTimeout(200);
    assert.equal(seen.puts.at(-1).advisor.monthly, true);
    assert.equal(seen.puts.at(-1).whatIf.enabled, true, "other settings kept");
    await sheet.getByRole("button", { name: "Bericht öffnen" }).click();
    const report = page.locator("dialog.fsheet[open]").last();
    await report
      .getByText("sda wird wahrscheinlich in den nächsten Monaten ausfallen")
      .waitFor();
    const text = await report.innerText();
    for (const want of [
      "Umgelagerte Sektoren: 0 → 4 in 6 Wochen",
      "4,2 Jahre in Betrieb",
      "Arbeitsspeicher",
      "Zeitweise bis 92 % belegt",
      "16 GB mehr empfohlen",
      "Volume Fast",
      "voll in etwa 35 Tagen",
      "Grafikkarte RTX 5060 Ti",
      "Ollama nutzt 12,6 GB von 16 GB",
      "Temperaturen",
      "Alle im grünen Bereich",
      "eng",
      "Verlauf",
      "Was-wäre-wenn",
    ])
      assert.ok(text.includes(want), `${tag} advisor lacks ${want}:\n${text}`);
    assert.ok(
      !text.includes("Platte sdb"),
      "healthy disks are not listed one by one",
    );
    if (width > 400) {
      const link = report.getByRole("link", { name: /WD Red Plus/ });
      assert.equal(
        await link.getAttribute("rel"),
        "noopener noreferrer nofollow",
      );
      assert.ok(text.includes("ungeprüft"));
    } else assert.ok(text.includes("Ersatz mit Preis ist ausgeschaltet"));
    await noOverflow(page, `${tag} advisor`);
    await grow(page);
    await page.screenshot({ path: `${out}/${tag}-10-hardware-berater.png` });
    await report.getByRole("button", { name: "Jetzt prüfen" }).click();
    await page.waitForTimeout(200);
    assert.equal(seen.runs, 1);
    // From the report into Was-wäre-wenn for the failing disk.
    await report.getByRole("button", { name: "Was-wäre-wenn" }).click();
    await page.getByText("Durchspielen: Platte sda fällt aus").waitFor();
    assert.equal(seen.whatif.at(-1).disk, "sda");
    await ctx.close();
  }
}

await browser.close();
assert.deepEqual(errors, [], errors.join("\n"));
console.log("agent54 foresight UI ok:", out);
