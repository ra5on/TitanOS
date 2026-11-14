// Settings › Sprachdienste (PLAN 92): the switch, GPU or processor, models
// and voices to download, make active and remove, the addresses for Home
// Assistant and who may reach them, on the desktop and the phone. Isolated
// API data only: no request reaches a NAS, Docker or Hugging Face.
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
const out = process.env.UI_OUTPUT || "out/validation/speech";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
// The page shows decimal units.
const GB = 1e9;
const MB = 1e6;

function fixture() {
  return {
    settings: {
      enabled: true,
      volume: "Data",
      device: "0000:01:00.0",
      whisperModel: "large-v3-turbo",
      language: "de",
      voice: "de_DE-thorsten-high",
      speed: 1,
      reach: "ha",
      agent: true,
    },
    status: {
      available: true,
      whisper: {
        phase: "running",
        port: 10300,
        listening: true,
        version: "v3.8.1-ls13",
        seen: { at: "2026-11-14T13:32:00Z", from: "192.168.10.40", ok: true },
      },
      piper: {
        phase: "running",
        port: 10200,
        listening: true,
        version: "2.5.3-ls138",
      },
    },
    jobs: { "model/small": { done: 300 * MB, total: 486 * MB } },
    devices: [
      {
        id: "0000:01:00.0",
        title: "NVIDIA GeForce RTX 3060",
        available: true,
        memoryUsedBytes: 9.4 * GB,
        memoryTotalBytes: 12 * GB,
      },
      {
        id: "0000:02:00.0",
        title: "NVIDIA GeForce GTX 1650",
        available: false,
        reason: "vm",
      },
    ],
    models: [
      { id: "large-v3-turbo", bytes: 1622 * MB, vramBytes: 2700 * MB, recommended: true, installed: true },
      { id: "large-v3", bytes: 3091 * MB, vramBytes: 4400 * MB, installed: true },
      { id: "medium", bytes: 1531 * MB, installed: false },
      { id: "small", bytes: 486 * MB, installed: false },
      { id: "base", bytes: 148 * MB, installed: false },
      { id: "tiny", bytes: 78 * MB, installed: false },
    ],
    voices: [
      { id: "de_DE-thorsten-high", name: "thorsten", language: "de_DE", languageName: "Deutsch", country: "Germany", quality: "high", speakers: 1, bytes: 114 * MB, installed: true },
      { id: "de_DE-thorsten-medium", name: "thorsten", language: "de_DE", languageName: "Deutsch", country: "Germany", quality: "medium", speakers: 1, bytes: 63 * MB, installed: true },
      { id: "de_DE-kerstin-low", name: "kerstin", language: "de_DE", languageName: "Deutsch", country: "Germany", quality: "low", speakers: 1, bytes: 63 * MB, installed: false },
      { id: "en_US-amy-medium", name: "amy", language: "en_US", languageName: "English", country: "United States", quality: "medium", speakers: 1, bytes: 63 * MB, installed: false },
    ],
    volumes: ["Data", "Fast"],
    hosts: [
      { kind: "lan", host: "192.168.10.20" },
      { kind: "tailscale", host: "titan.tail1a2b3.ts.net" },
    ],
  };
}

async function open(width, height, theme, change) {
  const made = await create("owner", theme, width);
  await made.page.setViewportSize({ width, height });
  made.data.prefs.appearance = { theme };
  await made.ctx.addInitScript(
    (v) => localStorage.setItem("titanos.appearance", JSON.stringify(v)),
    { theme },
  );
  const data = fixture();
  change?.(data);
  const seen = [];
  await made.ctx.route("**/api/v1/speech**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    let body = null;
    try {
      body = route.request().postDataJSON();
    } catch {
      // a request without a body
    }
    if (method !== "GET") seen.push({ method, p, body });
    if (p === "/speech" && method === "GET") return route.fulfill({ json: data });
    if (p === "/speech/settings") {
      data.settings = body;
      return route.fulfill({ json: body });
    }
    if (p === "/speech/download" && method === "POST") {
      data.jobs[`${body.kind}/${body.id}`] = { done: 0, total: 63 * MB };
      return route.fulfill({ status: 202, body: "" });
    }
    if (p === "/speech/download" && method === "DELETE") {
      delete data.jobs[`${body.kind}/${body.id}`];
      return route.fulfill({ status: 204, body: "" });
    }
    if (method === "DELETE") {
      const [, , kind, id] = p.split("/");
      const list = kind === "model" ? data.models : data.voices;
      list.find((x) => x.id === decodeURIComponent(id)).installed = false;
      return route.fulfill({ status: 204, body: "" });
    }
    return route.fulfill({ status: 404, json: { error: { code: "not_found", message: "fixture" } } });
  });
  return { ...made, seen, data };
}

async function noOverflow(page, what) {
  const wide = await page.evaluate(() => {
    const w = document.documentElement.clientWidth;
    return [...document.querySelectorAll("main *")]
      .filter((el) => {
        const r = el.getBoundingClientRect();
        return r.width > 0 && r.right > w + 1;
      })
      .map((el) => el.className || el.tagName)
      .slice(0, 5);
  });
  assert.deepEqual(wide, [], `${what}: wider than the page`);
}
const row = (page, title) =>
  page.locator(".srow", {
    has: page.locator(".srow-title", { hasText: new RegExp(`^${title}`) }),
  });
const sent = (seen, method, p) => seen.filter((s) => s.method === method && s.p === p);

// ---------- desktop ----------
{
  const { ctx, page, seen } = await open(1280, 1100, "hell");
  await page.goto(base + "/settings/speech");
  await page.getByRole("heading", { level: 1, name: /Sprachdienste/ }).waitFor();
  // Both services say their state in words.
  const on = row(page, "Sprache einschalten");
  assert.match(await on.textContent(), /Sprache zu Text: läuft.*Text zu Sprache: läuft/s);
  // The GPUs with their memory; one that a VM has cannot be chosen.
  const device = row(page, "Rechnen auf");
  assert.match(
    await device.textContent(),
    /NVIDIA GeForce RTX 3060.*9,4 GB von 12 GB belegt · 2,6 GB frei.*GTX 1650.*an eine VM durchgereicht.*Prozessor/s,
  );
  assert.ok(await device.getByRole("radio").nth(0).isChecked(), "the chosen GPU");
  assert.ok(await device.getByRole("radio").nth(1).isDisabled(), "a GPU of a VM");
  await device.getByRole("radio").nth(2).check();
  await page.waitForTimeout(300);
  assert.equal(sent(seen, "PUT", "/speech/settings").at(-1).body.device, "cpu");

  // Models: the active one, one to make active, a running download, one to get.
  assert.equal((await row(page, "large-v3-turbo").locator(".chip.ok").textContent()).trim(), "Aktiv");
  assert.match(await row(page, "large-v3-turbo").textContent(), /empfohlen.*1,62 GB.*etwa 2,7 GB Grafikspeicher/s);
  assert.match(await row(page, "small").textContent(), /61 %/);
  await row(page, "small").getByRole("button", { name: "Abbrechen" }).click();
  await row(page, "small").getByRole("button", { name: "Herunterladen" }).waitFor();
  await row(page, "medium").getByRole("button", { name: "Herunterladen" }).click();
  await row(page, "medium").getByRole("button", { name: "Abbrechen" }).waitFor();
  assert.deepEqual(sent(seen, "POST", "/speech/download").at(-1).body, { kind: "model", id: "medium" });
  await row(page, "\\s*large-v3\\s*$").getByRole("button", { name: "Aktivieren" }).click();
  await page.waitForTimeout(300);
  // The active model cannot be removed; another one asks first.
  assert.equal(sent(seen, "PUT", "/speech/settings").at(-1).body.whisperModel, "large-v3");

  // Voices: only the chosen language, the downloaded ones have a sample.
  assert.equal(await page.locator(".srow-title", { hasText: /^amy/ }).count(), 0);
  const thorsten = row(page, "thorsten \\(hoch\\)");
  await thorsten.getByRole("button", { name: "Hörprobe von thorsten" }).waitFor();
  assert.equal(await row(page, "kerstin").getByRole("button", { name: /Hörprobe/ }).count(), 0);
  const medium = row(page, "thorsten \\(mittel\\)");
  await medium.getByRole("button", { name: "thorsten löschen" }).click();
  const ask = page.getByRole("dialog", { name: "Vom NAS löschen?" });
  await ask.waitFor();
  assert.equal(seen.filter((s) => s.method === "DELETE" && s.p.startsWith("/speech/voice")).length, 0);
  await ask.getByRole("button", { name: "Löschen" }).click();
  await medium.getByRole("button", { name: "Herunterladen" }).waitFor();
  assert.equal(seen.at(-1).p, "/speech/voice/de_DE-thorsten-medium");
  await page.getByRole("combobox", { name: "Sprache der Stimmen" }).selectOption("en");
  await page.locator(".srow-title", { hasText: /^amy/ }).waitFor();

  // Home Assistant: host and port per service, who connected, who may.
  const ha = row(page, "In Home Assistant einrichten");
  assert.match(
    await ha.textContent(),
    /1\. Sprache zu Text.*Host\s*192\.168\.10\.20\s*Port\s*10300.*Zuletzt verbunden.*von 192\.168\.10\.40.*2\. Text zu Sprache.*Port\s*10200.*noch niemand verbunden.*titan\.tail1a2b3\.ts\.net/s,
  );
  await ha.getByRole("radio", { name: "Heimnetz" }).click();
  await page.waitForTimeout(300);
  assert.equal(sent(seen, "PUT", "/speech/settings").at(-1).body.reach, "lan");
  await row(page, "Im KI-Agenten").getByRole("switch").uncheck();
  await page.waitForTimeout(300);
  assert.equal(sent(seen, "PUT", "/speech/settings").at(-1).body.agent, false);
  await noOverflow(page, "speech");
  await page.screenshot({ path: `${out}/01-speech.png`, fullPage: true });
  await ctx.close();
}

// ---------- off, nothing chosen yet; an error of a service ----------
{
  const { ctx, page, seen } = await open(1280, 900, "graphit", (d) => {
    d.settings = { ...d.settings, enabled: false, volume: "", whisperModel: "", voice: "", device: "cpu" };
    d.status.whisper = { phase: "off", port: 10300, listening: false, version: "x" };
    d.status.piper = { phase: "off", port: 10200, listening: false, version: "x" };
    d.devices = [];
    d.jobs = {};
    for (const m of d.models) m.installed = false;
    for (const v of d.voices) v.installed = false;
  });
  await page.goto(base + "/settings/speech");
  const on = row(page, "Sprache einschalten");
  await on.getByText("Wähle zuerst das Volume").waitFor();
  assert.ok(await on.getByRole("switch").isDisabled(), "no volume, no switch");
  assert.ok(await row(page, "medium").getByRole("button", { name: "Herunterladen" }).isDisabled());
  assert.match(await row(page, "Rechnen auf").textContent(), /Keine NVIDIA-Grafikkarte gefunden/);
  await page.getByRole("combobox", { name: "Modelle liegen auf" }).selectOption("Fast");
  await page.waitForTimeout(300);
  assert.equal(sent(seen, "PUT", "/speech/settings").at(-1).body.volume, "Fast");
  await ctx.close();

  const broken = await open(1280, 900, "hell", (d) => {
    d.status.whisper = { phase: "error", message: "device", port: 10300, listening: true, version: "x" };
    d.status.piper = { phase: "error", message: "ValueError: voice file is damaged", port: 10200, listening: false, portError: "address already in use", version: "x" };
  });
  await broken.page.goto(base + "/settings/speech");
  const state = row(broken.page, "Sprache einschalten");
  await state.getByText("Grafikkarte nicht verfügbar").waitFor();
  await broken.page.getByText("ValueError: voice file is damaged").waitFor();
  await broken.page.getByText("Port 10200 ist von einem anderen Programm belegt").waitFor();
  await broken.ctx.close();
}

// ---------- phone ----------
{
  const { ctx, page } = await open(390, 844, "graphit");
  await page.goto(base + "/settings/speech");
  await page.locator(".srow-title", { hasText: "large-v3-turbo" }).waitFor();
  await noOverflow(page, "speech on the phone");
  await page.screenshot({ path: `${out}/10-speech-phone.png`, fullPage: true });
  await ctx.close();
}

assert.deepEqual(errors, [], "console errors");
await browser.close();
console.log("Sprachdienste UI ok:", out);
