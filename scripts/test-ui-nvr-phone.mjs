// The NVR (PLAN 79-7) on the phone, 390 px wide (phone emulation: touch,
// isMobile, deviceScaleFactor 2): every page of the program and what a tap
// opens (setup, live stacked with its own order, one camera large with the
// PTZ pad, events beside the pictures, recordings with export, events with
// their detail, cameras, adding and changing a camera with whom it tells,
// settings with faces, plates and the overview of the cameras' messages,
// the failover banner, the diagnosis, Benutzer › Kameras, an account that
// may only watch live). On every one: nothing scrolls sideways,
// no text is cut (cutTextIn of test-ui-fixtures.mjs, which also finds a word
// broken over lines – the bug that once set share names one letter per
// line) and no text or control lies on another. Real screenshots (light
// theme) into NVR_SHOTS or UI_OUTPUT. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5262 node scripts/test-ui-nvr-phone.mjs
import assert from "node:assert/strict";
import { mkdir, readFile } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import { installNvr, nvrState, pinAfternoon } from "./test-ui-nvr-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
pinAfternoon();
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5262";
const out = process.env.NVR_SHOTS || process.env.UI_OUTPUT || "out/validation/nvr";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);
const de = JSON.parse(
  await readFile(
    new URL("../web/src/lib/i18n/features/nvr.de.json", import.meta.url),
    "utf8",
  ),
);
const WIDTH = 390;

/** The whole page from its top (a sticky bar would otherwise sit in the middle). */
async function shot(page, name) {
  // A dialog covers the screen, not the page: then the screen is what counts.
  const dialog = await page.locator("dialog[open]").count();
  if (!dialog) await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: `${out}/${name}-${WIDTH}.png`,
    fullPage: !dialog,
    animations: "disabled",
  });
}

/** Text and controls that lie on each other inside `root` (the open dialog, else the page). */
function overlapsIn() {
  const root = document.querySelector("dialog[open]") ?? document.body;
  const found = [];
  const visible = (el) => {
    if (
      el.checkVisibility &&
      !el.checkVisibility({ visibilityProperty: true, opacityProperty: true })
    )
      return false;
    const s = getComputedStyle(el);
    return s.visibility !== "hidden" && s.display !== "none" && el.getClientRects().length > 0;
  };
  const name = (el) =>
    `${el.tagName.toLowerCase()}${el.className && typeof el.className === "string" ? "." + el.className.trim().split(/\s+/).join(".") : ""} "${(el.textContent ?? "").trim().slice(0, 40)}"`;
  const floating = (el) => {
    for (let n = el; n && n !== root; n = n.parentElement)
      if (/sticky|fixed/.test(getComputedStyle(n).position)) return true;
    return false;
  };
  const boxes = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if (!node.textContent.trim() || !node.parentElement || !visible(node.parentElement)) continue;
    if (node.parentElement.closest('.sr-only, option, svg, select, textarea, [aria-hidden="true"]'))
      continue;
    const range = document.createRange();
    range.selectNodeContents(node);
    for (const r of range.getClientRects())
      if (r.width > 1 && r.height > 1) boxes.push({ r, el: node.parentElement });
  }
  for (const el of root.querySelectorAll(
    ".btn, button, input:not([type=hidden]), select, textarea, .chip, [role=switch], [role=tab]",
  ))
    if (visible(el) && !el.closest(".sr-only")) boxes.push({ r: el.getBoundingClientRect(), el });
  for (let i = 0; i < boxes.length; i++)
    for (let j = i + 1; j < boxes.length; j++) {
      const a = boxes[i],
        b = boxes[j];
      if (a.el === b.el || a.el.contains(b.el) || b.el.contains(a.el)) continue;
      if (floating(a.el) !== floating(b.el)) continue;
      // A switch is an input drawn inside its own label.
      if (a.el.closest("label") && a.el.closest("label") === b.el.closest("label")) continue;
      const x = Math.min(a.r.right, b.r.right) - Math.max(a.r.left, b.r.left);
      const y = Math.min(a.r.bottom, b.r.bottom) - Math.max(a.r.top, b.r.top);
      if (x > 3 && y > 3) found.push(`overlap: ${name(a.el)} / ${name(b.el)}`);
    }
  return [...new Set(found)].slice(0, 20);
}

async function sound(page, what) {
  const wide = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  assert.ok(wide <= 0, `${what}: the page scrolls sideways by ${wide}px`);
  assert.deepEqual(await page.evaluate(cutTextIn), [], `${what}: cut text`);
  assert.deepEqual(await page.evaluate(overlapsIn), [], `${what}: overlapping text`);
}

/** A phone with the NVR's data; the desktop settings it saves are kept in `saved`. */
async function phone(role, change = () => {}, width = WIDTH) {
  const made = await create(role, "hell", width, "de", {
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 2,
    viewport: { width, height: 844 },
  });
  await made.ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  made.data.prefs.desktop = { version: 1 };
  const state = nvrState();
  state.admin = role !== "user";
  change(state, made.data);
  await installNvr(made.ctx, state);
  const saved = [];
  await made.ctx.route("**/api/v1/preferences/desktop", (route) => {
    if (route.request().method() !== "PUT") return route.fallback();
    const body = route.request().postDataJSON();
    saved.push(body);
    made.data.prefs.desktop = body;
    return route.fulfill({ status: 204 });
  });
  return { ...made, state, saved };
}
async function until(fn, what, ms = 6000) {
  const end = Date.now() + ms;
  while (!fn()) {
    if (Date.now() > end) throw new Error(`timed out: ${what}`);
    await new Promise((r) => setTimeout(r, 100));
  }
}
const sent = (state, method, path) =>
  state.requests.filter(([m, p]) => m === method && p === path).map((r) => r[2]);
/** The pages the phone's bar offers. */
async function barPages(page) {
  await page.locator(".phone-bar").getByRole("button", { name: "Seiten" }).tap();
  const menu = page.locator(".phone-bar .pages");
  await menu.waitFor();
  const pages = (await menu.getByRole("menuitemradio").allInnerTexts()).map((s) => s.trim());
  return { menu, pages };
}

try {
  // --- Setup on the phone ---
  {
    const { ctx, page } = await phone("owner", (s) => {
      s.settings.enabled = false;
      s.settings.privacyNoticeDismissed = false;
      s.dockerOff = true;
      s.hardware.devices.splice(2, 0, {
        kind: "coral",
        title: "Google Coral",
        present: true,
        available: false,
        reason: "plugin",
      });
    });
    await page.goto(base + "/nvr");
    await page.getByRole("heading", { name: "NVR einrichten" }).waitFor();
    await sound(page, "setup");
    await shot(page, "einrichtung");
    await page.getByRole("button", { name: "NVR einschalten" }).tap();
    const docker = page.getByRole("dialog", { name: "Docker einschalten?" });
    await docker.waitFor();
    await sound(page, "setup docker question");
    await shot(page, "einrichtung-docker");
    await ctx.close();
  }

  // --- Live: the tiles stand below each other in the account's own phone order ---
  {
    const { ctx, page, saved, state } = await phone("owner");
    await page.goto(base + "/nvr");
    await page.locator(".tile.row").nth(3).waitFor();
    const order = () =>
      page.locator(".tile.row [data-camera]").evaluateAll((els) => els.map((e) => e.dataset.camera));
    assert.deepEqual(await order(), ["haustuer", "einfahrt", "garten", "terrasse"]);
    // Every tile takes the width; nothing beside it.
    for (const box of await page.locator(".tile.row").evaluateAll((els) =>
      els.map((e) => e.getBoundingClientRect().width),
    ))
      assert.ok(box > WIDTH - 40, `a tile takes the width (${box})`);
    await page.locator('.tile.row [data-camera="haustuer"] .mark', { hasText: "Person" }).waitFor();
    // The phone's bar lists the program's pages.
    const { menu, pages } = await barPages(page);
    assert.deepEqual(pages, ["Live", "Aufnahmen", "Ereignisse", "Kameras", "Einstellungen", "Diagnose"]);
    await sound(page, "phone pages menu");
    await shot(page, "live-seiten");
    await page.keyboard.press("Escape");
    if (await menu.isVisible()) await page.locator(".phone-bar .back").last().tap();
    await page.evaluate(() => document.activeElement?.blur());
    await sound(page, "live");
    await shot(page, "live");
    // Arranging on the phone: arrows move a camera up or down; only the phone order is saved.
    await page.getByRole("button", { name: "Anordnen" }).tap();
    await page.getByText(de["nvr.arrange.hintPhone"]).waitFor();
    await page.getByRole("button", { name: "Garten nach oben" }).tap();
    await page.getByRole("button", { name: "Garten nach oben" }).tap();
    assert.deepEqual(await order(), ["garten", "haustuer", "einfahrt", "terrasse"]);
    await sound(page, "arrange on the phone");
    await shot(page, "live-anordnen");
    await page.getByRole("button", { name: "Fertig" }).tap();
    await until(() => saved.length === 1, "the phone order is saved");
    assert.deepEqual(saved[0].nvr.phoneOrder, ["garten", "haustuer", "einfahrt", "terrasse"]);
    assert.equal(saved[0].nvr.custom, false, "the desktop arrangement stays as it was");
    await page.reload();
    await page.locator(".tile.row").nth(3).waitFor();
    assert.deepEqual(await order(), ["garten", "haustuer", "einfahrt", "terrasse"]);
    // Events beside the pictures stand below them on the phone.
    await page.getByRole("button", { name: "Ereignisse" }).tap();
    const panel = page.getByRole("complementary", { name: "Ereignisse" });
    await panel.getByText("Person · Haustür").first().waitFor();
    const wall = await page.locator(".wall").boundingBox();
    const aside = await panel.boundingBox();
    assert.ok(aside.y >= wall.y + wall.height - 1, "events below the pictures");
    await sound(page, "live with events");
    await shot(page, "live-ereignisse");
    // One camera large, with the PTZ pad; a held direction stops on release.
    await page.getByRole("button", { name: "Garten groß zeigen" }).tap();
    await page.locator(".tile.solo").waitFor();
    await page.getByRole("button", { name: "Garten bewegen" }).tap();
    const left = page.getByRole("group", { name: "Garten bewegen" }).getByRole("button", { name: "Nach links" });
    await left.dispatchEvent("pointerdown", { pointerId: 1, button: 0, isPrimary: true });
    await until(() => sent(state, "POST", "/nvr/cameras/garten/ptz").some((b) => b.action === "move_left"), "moving");
    await left.dispatchEvent("pointerup", { pointerId: 1, button: 0, isPrimary: true });
    await until(() => sent(state, "POST", "/nvr/cameras/garten/ptz").at(-1)?.action === "stop", "stop on release");
    await sound(page, "one camera large with PTZ");
    await shot(page, "live-gross-ptz");
    await ctx.close();
  }

  // --- An account that may only watch one camera live ---
  {
    const { ctx, page } = await phone("user");
    await page.goto(base + "/nvr");
    await page.locator('.tile.row [data-camera="haustuer"]').waitFor();
    assert.equal(await page.locator(".tile.row").count(), 1);
    // Only "Live": no recordings right, no admin pages.
    await page.locator(".phone-bar").getByRole("button", { name: "Seiten" }).tap();
    const offered = (await page.locator(".phone-bar .pages [role^=menuitem]").allInnerTexts()).map((s) => s.trim());
    assert.deepEqual(offered, ["Live"], `the bar offers ${offered}`);
    await page.locator(".phone-bar").getByRole("button", { name: "Seiten" }).tap();
    await sound(page, "live as user");
    await shot(page, "live-benutzer");
    await ctx.close();
  }
  {
    // Recordings only: the live page points to them, the bar offers only them.
    const { ctx, page } = await phone("user", (s) => {
      s.rights = { einfahrt: { live: false, recordings: true } };
    });
    await page.goto(base + "/nvr");
    await page.getByText(de["nvr.live.onlyRecordings"]).waitFor();
    const { pages } = await barPages(page);
    assert.deepEqual(pages, ["Aufnahmen", "Ereignisse"]);
    await sound(page, "user with recordings only");
    await ctx.close();
  }

  // --- Recordings and exports ---
  {
    const { ctx, page } = await phone("owner");
    await page.goto(base + "/nvr/recordings?camera=einfahrt");
    await page.locator(".timeline .piece").first().waitFor();
    await page.locator('[data-export="haustuer_ab12cd"]').waitFor();
    await sound(page, "recordings");
    await shot(page, "aufnahmen");
    await page.locator("#nvr-exports").scrollIntoViewIfNeeded();
    await page.locator("#nvr-exports").screenshot({ path: `${out}/exporte-${WIDTH}.png`, animations: "disabled" });
    await page.getByRole("button", { name: "Exportieren" }).tap();
    await page.getByRole("dialog", { name: "Ausschnitt exportieren" }).waitFor();
    await sound(page, "export dialog");
    await shot(page, "aufnahmen-export");
    await ctx.close();
  }

  // --- Events and one event ---
  {
    const { ctx, page } = await phone("owner");
    await page.goto(base + "/nvr/events");
    await page.getByRole("button", { name: /Auto · Einfahrt/ }).waitFor();
    await sound(page, "events");
    await shot(page, "ereignisse");
    await page.getByRole("button", { name: /Auto · Einfahrt/ }).tap();
    const detail = page.getByRole("dialog", { name: "Auto · Einfahrt" });
    await detail.getByText("K-TI 2026").waitFor();
    await sound(page, "event detail");
    await shot(page, "ereignis");
    await ctx.close();
  }

  // --- Cameras: list, add with search and check, change ---
  {
    const { ctx, page } = await phone("owner");
    await page.goto(base + "/nvr/cameras");
    await page.locator('[data-camera="terrasse"]').getByText(/Nicht erreichbar seit/).waitFor();
    await sound(page, "cameras");
    await shot(page, "kameras");
    await page.getByRole("button", { name: "Entfernen" }).first().tap();
    await page.getByRole("dialog").getByText(de["nvr.cameras.removeText"]).waitFor();
    await sound(page, "remove question");
    await page.getByRole("dialog").getByRole("button", { name: "Abbrechen" }).tap();
    await page.goto(base + "/nvr/cameras?new=1");
    await page.getByRole("button", { name: "Suchen", exact: true }).tap();
    await page.getByRole("button", { name: /Reolink RLC-810A/ }).tap();
    await page.getByLabel("Benutzer", { exact: true }).fill("admin");
    await page.getByLabel("Passwort", { exact: true }).fill("wrong");
    await page.getByRole("button", { name: "Prüfen" }).tap();
    await page.getByText(de["nvr.probe.unauthorized"]).waitFor();
    await sound(page, "add camera, wrong password");
    await page.getByLabel("Passwort", { exact: true }).fill("geheim");
    await page.getByRole("button", { name: "Prüfen" }).tap();
    await page.getByText("Hauptstrom: 3840 × 2160 · H265 · mit Ton").waitFor();
    await sound(page, "add camera");
    await shot(page, "kamera-hinzufuegen");
    await page.goto(base + "/nvr/cameras?edit=haustuer");
    await page.getByRole("heading", { name: "Haustür" }).waitFor();
    await sound(page, "edit camera");
    await shot(page, "kamera-aendern");
    // PLAN 82-3: the recipients with their ways and hints, the pause and
    // the window with days and times – long texts wrap, pills wrap.
    const chevron = page.locator("#nvr-notify-recipients > .srow-head .srow-chev");
    if ((await chevron.getAttribute("aria-expanded")) !== "true") await chevron.tap();
    await page.locator("#nvr-notify-recipients").getByText(de["nvr.notify.legacy"]).waitFor();
    await page.locator("#nvr-notify-recipients").getByText(de["nvr.notify.devicesOff"]).waitFor();
    await page.getByRole("switch", { name: de["nvr.notify.window"] }).tap();
    await page.getByText(de["nvr.notify.timeHint"]).waitFor();
    await sound(page, "camera notifications");
    await page.locator("#nvr-notify-recipients").evaluate((el) => {
      el.scrollIntoView({ block: "start" });
      window.scrollBy(0, -80);
    });
    await page.screenshot({ path: `${out}/kamera-meldungen-${WIDTH}.png`, animations: "disabled" });
    await ctx.close();
  }

  // --- PLAN 82: the stream rows and the decoder on the narrowest phone (375 px) ---
  {
    const narrow = 375;
    const { ctx, page } = await phone("owner", (s) => (s.settings.decoder = "0000:01:00.0"), narrow);
    /** Every radio of the stream choices lies inside the screen (they wrap, never run out). */
    const inside = async (what) => {
      const outside = await page.evaluate((w) => {
        const out = [];
        for (const el of document.querySelectorAll('[role="radiogroup"] [role="radio"], select.field'))
          if (el.getBoundingClientRect().right > w + 0.5) out.push(el.textContent.trim().slice(0, 30));
        return out;
      }, narrow);
      assert.deepEqual(outside, [], `${what}: controls run out of the screen`);
    };
    await page.goto(base + "/nvr/cameras?new=1");
    await page.getByRole("button", { name: "Suchen", exact: true }).tap();
    await page.getByRole("button", { name: /Reolink RLC-810A/ }).tap();
    await page.getByLabel("Benutzer", { exact: true }).fill("admin");
    await page.getByLabel("Passwort", { exact: true }).fill("geheim");
    await page.getByRole("button", { name: "Prüfen" }).tap();
    await page.getByLabel("Hochauflösender Stream").waitFor();
    await page.getByRole("radiogroup", { name: "Live-Bild" }).getByRole("radio", { name: "Niedrigauflösend" }).waitFor();
    await sound(page, "stream rows (375 px)");
    await inside("stream rows");
    await page.getByRole("radiogroup", { name: "Erkennung" }).getByRole("radio", { name: "Hochauflösend" }).tap();
    await page.getByText(de["nvr.stream.detectHighHint"]).waitFor();
    await page.getByRole("switch", { name: "Streams offen halten" }).tap();
    await page.getByText(de["nvr.stream.keepOpenOffHint"]).waitFor();
    await page.getByLabel("Niedrigauflösender Stream").selectOption("");
    await page.locator("#nvr-stream-record").getByText(de["nvr.stream.single"]).waitFor();
    await sound(page, "stream rows, one stream (375 px)");
    await inside("stream rows, one stream");
    await page.getByLabel("Niedrigauflösender Stream").selectOption({ label: "640 × 360 · H264" });
    await page.locator('[id$="-streams"]').first().evaluate((el) => {
      el.scrollIntoView({ block: "start" });
      window.scrollBy(0, -160);
    });
    await page.screenshot({ path: `${out}/kamera-streams-${narrow}.png`, animations: "disabled" });
    await page.goto(base + "/nvr/cameras?edit=garten");
    await page.locator("#nvr-stream-current").getByText(/2560 × 1440/).waitFor();
    await sound(page, "stored streams (375 px)");
    await page.goto(base + "/nvr/settings");
    await page.locator("#nvr-decoder-reason").getByText(de["nvr.decoder.reason.vm"]).waitFor();
    // The whole page, the detector's and the decoder's selects included.
    const cut = await page.evaluate(cutTextIn);
    assert.deepEqual(cut, [], "decoder (375 px): cut text");
    assert.deepEqual(await page.evaluate(overlapsIn), [], "decoder (375 px): overlapping text");
    assert.equal(
      await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth) <= 0,
      true,
      "decoder (375 px): the page scrolls sideways",
    );
    await inside("decoder");
    await page.locator("#nvr-decoder-row").evaluate((el) => {
      el.scrollIntoView({ block: "start" });
      window.scrollBy(0, -160);
    });
    await page.screenshot({ path: `${out}/entschluesseln-${narrow}.png`, animations: "disabled" });
    await page.goto(base + "/nvr/diagnosis");
    await page.locator("#nvr-diag-decoder").getByText(de["nvr.decoder.reason.vm"]).waitFor();
    await sound(page, "diagnosis decoder (375 px)");
    await ctx.close();
  }

  // --- Settings: storage, faces, plates, Home Assistant; exports over the limit ---
  {
    const { ctx, page } = await phone("owner", (s) => {
      s.settings.rtspOut = true;
      s.settings.rtspUser = "titanos";
      s.settings.rtspPassword = "k3Jw-91Lm-x0Qp-77Zt";
      s.storage = { ...s.storage, exportsBytes: 230e9, usedBytes: 250.4e9, cap: "exports_exceed", lastCleanup: s.now - 3600 };
    });
    await page.goto(base + "/nvr/settings");
    await page.locator("#nvr-cap-note").waitFor();
    // PLAN 82-3: the overview of who gets what; its lines wrap.
    await page.locator("#nvr-notify-einfahrt").getByText("sam bekommt zurzeit nichts: kein Recht auf die Kamera").waitFor();
    await sound(page, "settings");
    await shot(page, "einstellungen");
    await page.locator("#nvr-notify").evaluate((el) => {
      el.scrollIntoView({ block: "start" });
      window.scrollBy(0, -80);
    });
    await page.screenshot({ path: `${out}/einstellungen-meldungen-${WIDTH}.png`, animations: "disabled" });
    await page.locator("#nvr-faces").getByRole("button", { name: /Bekannte Gesichter/ }).tap();
    await page.locator('#nvr-faces [data-face="Patrick"] b').waitFor();
    await page.locator("#nvr-faces").getByLabel("Name der Person").fill("Oma Erna");
    await page.getByText(de["nvr.faces.nameProblem.space"]).waitFor();
    await page.locator("#nvr-plates").getByRole("button", { name: /Bekannte Kennzeichen/ }).tap();
    await page.locator("#nvr-plates").getByText("HH-Q 4").waitFor();
    await sound(page, "faces and plates");
    await shot(page, "gesichter-kennzeichen");
    // The faces as the screen shows them (an element shot would stitch the sticky bar into it).
    await page.locator("#nvr-faces").evaluate((el) => {
      el.scrollIntoView({ block: "start" });
      window.scrollBy(0, -140);
    });
    await page.screenshot({ path: `${out}/gesichter-${WIDTH}.png`, animations: "disabled" });
    await ctx.close();
  }

  // --- The detection moved, and the diagnosis says why ---
  {
    const { ctx, page } = await phone("owner", (s) => {
      s.detector = {
        kind: "auto",
        active: "igpu",
        inferenceMs: 23.8,
        failover: {
          from: "npu",
          to: "igpu",
          at: new Date((s.now - 1200) * 1000).toISOString(),
          reason: "npu_firmware_old",
          detail: "MAPPED_INFERENCE_VERSION is NOT compatible with the ELF",
        },
      };
    });
    await page.goto(base + "/nvr/settings");
    await page.locator("#nvr-failover").getByText(de["nvr.reason.npu_firmware_old.title"]).waitFor();
    await sound(page, "failover banner");
    await shot(page, "ausweichen");
    await page.goto(base + "/nvr/diagnosis");
    const cause = page.locator("#nvr-diag-cause");
    await cause.getByText(de["nvr.reason.npu_firmware_old.todo"]).waitFor();
    await cause.getByRole("button", { name: "Einzelheit" }).tap();
    await cause.getByText(/MAPPED_INFERENCE_VERSION/).waitFor();
    await page.locator("pre.log").getByText(/a very long line that has to wrap/).waitFor();
    assert.equal(
      await page.locator("pre.log").evaluate((el) => el.scrollWidth <= el.clientWidth + 1),
      true,
      "the log wraps",
    );
    await sound(page, "diagnosis");
    await shot(page, "diagnose-ausweichen");
    await ctx.close();
  }

  // --- Benutzer › Kameras: one camera a line, the names never one letter per line ---
  {
    const { ctx, page } = await phone("owner", (s) => {
      s.cameras[1].name = "Einfahrt mit dem Carport und dem Gartentor";
    });
    await page.goto(base + "/users");
    await page.locator('[data-account="sam"]').tap();
    const group = page.locator("section.expander", { hasText: "Kameras" }).last();
    await group.getByRole("button", { name: /Kameras/ }).tap();
    await group.getByRole("switch", { name: "Haustür: Live ansehen" }).waitFor();
    await group.getByRole("switch", { name: "Haustür: Live ansehen" }).check();
    await group.getByText("Live: 1 von 4 · Aufnahmen: 0 von 4").waitFor();
    // Each name is at least as wide as its longest word: no letter-per-line columns.
    const narrow = await group.locator(".cam .t").evaluateAll((els) =>
      els.filter((el) => el.getBoundingClientRect().width < 60).map((el) => el.textContent),
    );
    assert.deepEqual(narrow, [], "camera names have room");
    await sound(page, "user cameras");
    // What the screen shows with the group in view (the bar stays on top).
    await group.evaluate((el) => el.scrollIntoView({ block: "end" }));
    await page.screenshot({ path: `${out}/benutzer-kameras-${WIDTH}.png`, animations: "disabled" });
    await ctx.close();
  }

  // --- PLAN 82-3 at 375 px, the narrowest phone asked for: whom a camera tells, the overview ---
  {
    const narrow = 375;
    const made = await create("owner", "hell", narrow, "de", {
      isMobile: true,
      hasTouch: true,
      deviceScaleFactor: 2,
      viewport: { width: narrow, height: 812 },
    });
    made.data.prefs.desktop = { version: 1 };
    const state = nvrState();
    // A long account name and a long camera name must wrap, never be cut.
    state.recipients.accounts.push({ name: "hausmeister_nebengebaeude", admin: false, email: false, pushDevices: 0, pushDevicesOff: 0, cameras: ["haustuer"] });
    state.cameras[0].name = "Haustür mit Vordach und Briefkasten";
    await installNvr(made.ctx, state);
    const page = made.page;
    await page.goto(base + "/nvr/cameras?edit=haustuer#nvr-notify-recipients");
    await page.locator("#nvr-notify-recipients").getByText(de["nvr.notify.legacy"]).waitFor();
    await page.locator("#nvr-notify-recipients").getByText("hausmeister_nebengebaeude").waitFor();
    await page.getByRole("switch", { name: de["nvr.notify.window"] }).tap();
    await page.getByText(de["nvr.notify.timeHint"]).waitFor();
    await sound(page, "camera notifications at 375 px");
    await page.locator("#nvr-notify-recipients").evaluate((el) => {
      el.scrollIntoView({ block: "start" });
      window.scrollBy(0, -80);
    });
    await page.screenshot({ path: `${out}/kamera-meldungen-${narrow}.png`, animations: "disabled" });
    await page.goto(base + "/nvr/settings");
    await page.locator("#nvr-notify-einfahrt").getByText("sam bekommt zurzeit nichts: kein Recht auf die Kamera").waitFor();
    const wide = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    assert.ok(wide <= 0, `notification overview at 375 px: the page scrolls sideways by ${wide}px`);
    assert.deepEqual(
      await page.evaluate(cutTextIn),
      [],
      "notification overview at 375 px: cut text",
    );
    assert.deepEqual(await page.evaluate(overlapsIn), [], "notification overview at 375 px: overlapping text");
    await page.locator("#nvr-notify").evaluate((el) => {
      el.scrollIntoView({ block: "start" });
      window.scrollBy(0, -80);
    });
    await page.screenshot({ path: `${out}/einstellungen-meldungen-${narrow}.png`, animations: "disabled" });
    // The account's camera rights with the third switch "Namen und Kennzeichen".
    state.userCameras.sam = {
      admin: false,
      cameras: state.cameras
        .filter((c) => !c.deleted)
        .map((c) => ({ id: c.id, name: c.name, live: true, recordings: c.id !== "garten", names: c.id === "haustuer" })),
    };
    await page.goto(base + "/users");
    await page.locator('[data-account="sam"]').tap();
    const group = page.locator("section.expander", { hasText: "Kameras" }).last();
    await group.getByRole("button", { name: /Kameras/ }).tap();
    await group.getByRole("switch", { name: "Haustür mit Vordach und Briefkasten: Namen und Kennzeichen sehen" }).waitFor();
    assert.equal(await group.getByRole("switch", { name: "Garten: Namen und Kennzeichen sehen" }).isDisabled(), true);
    await sound(page, "camera rights at 375 px");
    await group.evaluate((el) => el.scrollIntoView({ block: "start" }));
    await page.screenshot({ path: `${out}/benutzer-kameras-${narrow}.png`, animations: "disabled" });
    await made.ctx.close();
  }

  assert.deepEqual(errors, [], "no script errors");
  console.log("NVR phone: ok");
} finally {
  await browser.close();
}
