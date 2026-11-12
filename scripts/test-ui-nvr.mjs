// The NVR (PLAN 79-7) at 1280 px: the setup with its plain questions, adding
// a camera with search and check (the choices are sent as the API expects
// them), the live view's arrangement (drag, size, saved per account, reset),
// what an account without the admin role sees, the settings, the failover
// banner with "Wieder versuchen", the diagnosis texts of every classified
// cause, recordings with export, events, the camera rights of an account and
// the desktop's camera widget. On every page: no sideways scroll, no cut
// text. Real screenshots (deviceScaleFactor 2, light theme) into NVR_SHOTS
// or UI_OUTPUT. Isolated API data only (test-ui-nvr-fixtures.mjs).
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5262 node scripts/test-ui-nvr.mjs
import assert from "node:assert/strict";
import { mkdir, readFile } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import { installNvr, nvrState } from "./test-ui-nvr-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
  // Talking needs a microphone: a made-up one, no question.
  args: ["--use-fake-ui-for-media-stream", "--use-fake-device-for-media-stream"],
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
const WIDTH = 1280;

const shot = (page, name) =>
  page.screenshot({
    path: `${out}/${name}-${WIDTH}.png`,
    fullPage: true,
    animations: "disabled",
  });
async function sound(page, name) {
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `${name}: page scrolls sideways by ${overflow}px`);
  assert.deepEqual(await page.evaluate(cutTextIn), [], `${name}: cut text`);
}
/** A page of the NVR for a role, with the NVR's data and the desktop settings it saves. */
async function open(role, change = () => {}, theme = "hell") {
  const made = await create(role, theme, WIDTH, "de", { deviceScaleFactor: 2 });
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
const sent = (state, method, path) =>
  state.requests.filter(([m, p]) => m === method && p === path).map((r) => r[2]);
const confirmDialog = (page, title) =>
  page.getByRole("dialog", { name: title });
/** Waits until a condition of the test holds (the desktop saves 1.5 s after a change). */
async function until(fn, what, ms = 6000) {
  const end = Date.now() + ms;
  while (!fn()) {
    if (Date.now() > end) throw new Error(`timed out: ${what}`);
    await new Promise((r) => setTimeout(r, 100));
  }
}

try {
  // --- Setup: rows, the plain questions for Docker and a sleeping volume ---
  {
    const { ctx, page, state } = await open("owner", (s) => {
      s.settings.enabled = false;
      s.settings.privacyNoticeDismissed = false;
      s.settings.storage.limitGB = 0;
      s.dockerOff = true;
      s.sleepingVolumes = ["Fast"];
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
    await page.getByText(de["nvr.privacy.text"]).waitFor();
    assert.deepEqual(
      (await page.locator("#nvr-setup-detector option").allTextContents()).map(
        (o) => o.trim(),
      ),
      [
        "Automatisch (empfohlen) – nimmt NPU",
        "NPU (Intel AI Boost)",
        "iGPU (Intel Arc)",
        "Coral USB (Google Coral) – Plugin fehlt",
        "CPU",
      ],
    );
    assert.equal(
      await page.locator("#nvr-setup-detector option[value=coral]").isDisabled(),
      true,
      "a device that cannot be used cannot be chosen",
    );
    // A Coral stick without its plugin: the setup offers the plugin page.
    const plugin = page.getByRole("link", { name: /Coral-Stick gefunden/ });
    assert.equal(await plugin.getAttribute("href"), "/plugins");
    await sound(page, "setup");
    await shot(page, "einrichtung");
    await page.getByRole("button", { name: "Verstanden" }).click();
    await page.getByText("Gelesen", { exact: true }).waitFor();
    await page.locator("#nvr-setup-volume").selectOption("Fast");
    await page.locator("#nvr-setup-detector").selectOption("igpu");
    await page.locator("#nvr-setup-cap").fill("200");
    await page.getByRole("button", { name: "NVR einschalten" }).click();
    // Docker is off: a plain question, nothing to type.
    const docker = confirmDialog(page, "Docker einschalten?");
    await docker.waitFor();
    assert.equal(await docker.locator("input").count(), 0, "no typed confirmation");
    assert.equal(
      await docker.getByRole("button", { name: "Abbrechen" }).evaluate((b) => b === document.activeElement),
      true,
      "Abbrechen has the focus",
    );
    await shot(page, "einrichtung-docker");
    await docker.getByRole("button", { name: "Docker einschalten" }).click();
    const sleep = confirmDialog(page, "Platten bleiben wach");
    await sleep.waitFor();
    await sleep.getByText(/Die Platten von „Fast“ dürfen bisher schlafen/).waitFor();
    assert.equal(await sleep.locator("input").count(), 0);
    await sleep.getByRole("button", { name: "Dieses Volume nehmen" }).click();
    await page.locator("[data-tile]").first().waitFor();
    assert.deepEqual(sent(state, "POST", "/nvr/enable").at(-1), {
      volume: "Fast",
      enableDocker: true,
      confirmSleepingVolume: true,
    });
    const put = sent(state, "PUT", "/nvr/settings");
    assert.deepEqual(put[0], { privacyNoticeDismissed: true });
    assert.deepEqual(put.at(-1), {
      detector: { kind: "igpu" },
      storage: { limitGB: 200 },
      privacyNoticeDismissed: true,
    });
    await ctx.close();
  }

  // --- After switching on: the image loads, then "Kamera hinzufügen" ---
  {
    const { ctx, page } = await open("owner", (s) => {
      s.state = "pulling";
      s.image = { state: "downloading", version: "0.18.2", bytes: 650e6, size: 1500e6, progress: 0.43 };
      s.cameras = [];
    });
    await page.goto(base + "/nvr");
    await page.getByText("Die Erkennungs-Software wird geladen").waitFor();
    await page.getByText("43 % · 650 MB von 1,5 GB").waitFor();
    await page.getByText("Noch keine Kamera", { exact: true }).waitFor();
    assert.equal(
      await page.getByRole("link", { name: "Kamera hinzufügen" }).getAttribute("href"),
      "/nvr/cameras?new=1",
    );
    await sound(page, "pulling");
    await shot(page, "einrichtung-laden");
    await ctx.close();
  }

  // --- Add a camera: search, check, the choices as the API expects them ---
  {
    const { ctx, page, state } = await open("owner");
    await page.goto(base + "/nvr/cameras?new=1");
    await page.getByRole("heading", { name: "Kamera hinzufügen" }).waitFor();
    await page.getByRole("button", { name: "Suchen", exact: true }).click();
    await page.getByRole("button", { name: /Reolink RLC-810A/ }).click();
    assert.equal(await page.getByLabel("Adresse der Kamera").inputValue(), "192.168.10.41");
    await page.getByLabel("Benutzer", { exact: true }).fill("admin");
    await page.getByLabel("Passwort", { exact: true }).fill("wrong");
    await page.getByRole("button", { name: "Prüfen" }).click();
    await page.getByText(de["nvr.probe.unauthorized"]).waitFor();
    await page.getByLabel("Passwort", { exact: true }).fill("geheim");
    await page.getByRole("button", { name: "Prüfen" }).click();
    await page.getByText("Verbindung steht.").waitFor();
    await page.getByText("Hauptstrom: 3840 × 2160 · H265 · mit Ton").waitFor();
    await page.getByText("Nebenstrom: 640 × 360 · H264 · ohne Ton").waitFor();
    await page.getByText(de["nvr.probe.ptzYes"]).waitFor();
    // A name is unique, upper and lower case aside: said under the field, nothing sent.
    await page.getByLabel("Name", { exact: true }).fill("haustür");
    await page.getByText(de["nvr.camera.nameExists"]).waitFor();
    assert.equal(await page.getByRole("button", { name: "Kamera hinzufügen" }).isDisabled(), true);
    await page.getByLabel("Name", { exact: true }).fill("Carport");
    const pill = (group, name) =>
      page.getByRole("group", { name: group }).getByRole("button", { name, exact: true });
    await pill("Erkennen", "Kennzeichen").click();
    // Plates need a vehicle: the vehicles come along.
    assert.equal(await pill("Erkennen", "Fahrzeuge").getAttribute("aria-pressed"), "true");
    await page.getByLabel("Aufnehmen", { exact: true }).selectOption("continuous");
    await page.getByLabel("Aufbewahren").fill("30");
    await page.getByRole("switch", { name: "Ton", exact: true }).check();
    assert.equal(
      await page.getByRole("switch", { name: "Gegensprechen" }).isChecked(),
      false,
      "talk stays off",
    );
    await pill("Melden an", "Push aufs Handy").click();
    await pill("Melden bei", "Bekanntem Kennzeichen").click();
    await sound(page, "add camera");
    await shot(page, "kamera-hinzufuegen");
    await page.getByRole("button", { name: "Kamera hinzufügen" }).click();
    await page.locator('[data-camera="carport"]').waitFor();
    assert.equal(new URL(page.url()).pathname + new URL(page.url()).search, "/nvr/cameras");
    const body = sent(state, "POST", "/nvr/cameras")[0];
    assert.deepEqual(body, {
      name: "Carport",
      source: {
        kind: "onvif",
        host: "192.168.10.41",
        port: 0,
        onvifPort: 8000,
        user: "admin",
        password: "geheim",
        mainURL: "rtsp://192.168.10.41:554/h265Preview_01_main",
        subURL: "rtsp://192.168.10.41:554/h264Preview_01_sub",
        allowPublic: false,
      },
      detect: {
        enabled: true,
        objects: ["person", "bicycle", "car", "motorcycle", "bus"],
        plates: true,
        faces: false,
      },
      record: { mode: "continuous", retainDays: 30, preSeconds: 5, postSeconds: 5 },
      audio: { enabled: true, twoWay: false },
      ptz: true,
      notify: {
        titan: true,
        push: true,
        email: false,
        homeAssistant: false,
        agent: false,
        kinds: ["person", "offline", "plate"],
      },
      enabled: true,
    });
    const probes = sent(state, "POST", "/nvr/cameras/probe");
    assert.equal(probes.length, 2, "a wrong password costs one try at the camera");
    assert.equal(JSON.stringify(state.requests).includes("geheim@"), false);

    // The list: state, order, switch, remove with a plain question.
    await page.getByText("Nicht erreichbar seit").first().waitFor();
    // A removed camera stands apart, with the day its recordings end, never as a live tile.
    const removed = page.locator("#nvr-removed");
    const until = new Date((state.now + 12 * 86400) * 1000).toLocaleDateString("de-DE", { day: "2-digit", month: "2-digit", year: "numeric" });
    await removed.locator('[data-camera="schuppen"]').getByText(`Entfernt – Aufnahmen bis ${until}`).waitFor();
    assert.equal(
      await removed.getByRole("link", { name: "Aufnahmen ansehen" }).getAttribute("href"),
      "/nvr/recordings?camera=schuppen",
    );
    assert.equal(await removed.getByRole("switch").count(), 0, "a removed camera cannot be switched on");
    await sound(page, "cameras");
    await shot(page, "kameras");
    await page.getByRole("button", { name: "Einfahrt nach oben" }).click();
    await page.waitForFunction(
      () => document.querySelector(".cam")?.getAttribute("data-camera") === "einfahrt",
    );
    assert.deepEqual(sent(state, "PUT", "/nvr/cameras/einfahrt").at(-1), { order: 1 });
    await page.getByRole("switch", { name: "Garten einschalten" }).uncheck();
    await page.locator('[data-camera="garten"]').getByText("Ausgeschaltet").waitFor();
    assert.deepEqual(sent(state, "PUT", "/nvr/cameras/garten").at(-1), { enabled: false });
    await page.locator('[data-camera="carport"]').getByRole("button", { name: "Entfernen" }).click();
    const remove = confirmDialog(page, "Kamera „Carport“ entfernen?");
    await remove.getByText(de["nvr.cameras.removeText"]).waitFor();
    assert.equal(await remove.locator("input").count(), 0);
    await shot(page, "kamera-entfernen");
    await remove.getByRole("button", { name: "Entfernen" }).click();
    await page.locator('[data-camera="carport"]').waitFor({ state: "detached" });
    assert.equal(sent(state, "DELETE", "/nvr/cameras/carport").length, 1);

    // Changing a camera: the same rows, the password stays unless typed.
    await page.locator('[data-camera="haustuer"]').getByRole("link", { name: "Ändern" }).click();
    await page.getByRole("heading", { name: "Haustür" }).waitFor();
    assert.equal(
      await page.getByLabel("Passwort", { exact: true }).getAttribute("placeholder"),
      "unverändert",
    );
    assert.equal(await page.getByRole("switch", { name: "Gegensprechen" }).isChecked(), true);
    await sound(page, "edit camera");
    await shot(page, "kamera-aendern");
    // The NAS knows names the page does not list (a removed camera's): its answer is said too.
    await page.getByLabel("Name", { exact: true }).fill("Schuppen");
    await page.getByRole("button", { name: "Speichern" }).click();
    await page.getByText(de["nvr.camera.nameExists"]).waitFor();
    await page.getByLabel("Name", { exact: true }).fill("Haustür");
    await page.getByRole("button", { name: "Speichern" }).click();
    await page.locator('[data-camera="haustuer"]').waitFor();
    const change = sent(state, "PUT", "/nvr/cameras/haustuer").at(-1);
    assert.equal(change.source.password, "");
    assert.deepEqual(change.detect, {
      enabled: true,
      objects: ["person", "dog"],
      plates: false,
      faces: true,
    });
    assert.equal("enabled" in change, false);
    await ctx.close();
  }

  // --- A NAS that cannot recognize plates and faces says why ---
  {
    const { ctx, page } = await open("owner", (s) => {
      s.hardware.enrichmentsPossible = false;
      s.hardware.enrichmentsReason = "memory";
    });
    await page.goto(base + "/nvr/cameras?new=1");
    const detect = page.getByRole("group", { name: "Erkennen" });
    assert.equal(await detect.getByRole("button", { name: "Kennzeichen" }).isDisabled(), true);
    assert.equal(await detect.getByRole("button", { name: "Gesichter" }).isDisabled(), true);
    await page.getByText(de["nvr.camera.enrichMemory"]).waitFor();
    await ctx.close();
  }

  // --- Live: free arrangement like the widgets, saved per account ---
  {
    const { ctx, page, saved, data, state } = await open("owner");
    await page.goto(base + "/nvr");
    await page.locator("[data-tile]").nth(3).waitFor();
    assert.equal(await page.locator('[data-tile="schuppen"]').count(), 0, "a removed camera has no tile");
    const box = async (id) => page.locator(`[data-tile="${id}"]`).boundingBox();
    const before = await box("haustuer");
    // What the camera sees right now (feed), the name, the offline note.
    await page.locator('[data-tile="haustuer"] .mark', { hasText: "Person" }).waitFor();
    await page.locator('[data-tile="terrasse"]').getByText(/Nicht erreichbar seit/).waitFor();
    await page.locator('[data-tile="haustuer"] img').waitFor();
    assert.equal(await page.locator('[data-tile="haustuer"] .rec').count(), 1);
    // Sound, talk and moving only where the camera has them.
    assert.equal(await page.getByRole("button", { name: "Ton von Haustür" }).count(), 1);
    assert.equal(await page.getByRole("button", { name: "Ton von Einfahrt" }).count(), 0);
    assert.equal(await page.getByRole("button", { name: /Mit Haustür sprechen/ }).count(), 1);
    await page.getByRole("button", { name: "Garten bewegen" }).click();
    await page.getByRole("group", { name: "Garten bewegen" }).getByRole("button", { name: "Nach links" }).waitFor();
    await page.getByRole("combobox", { name: "Gespeicherte Position" }).waitFor();
    // A held direction is sent again every two seconds; "stop" follows on release.
    const ptz = () =>
      state.requests.filter(([m, p]) => m === "POST" && p === "/nvr/cameras/garten/ptz");
    const actions = () => ptz().map((r) => r[2].action);
    const hold = async (name) => {
      const b = await page.getByRole("group", { name: "Garten bewegen" }).getByRole("button", { name }).boundingBox();
      await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
      await page.mouse.down();
    };
    await hold("Nach links");
    await page.waitForTimeout(4500);
    assert.ok(actions().filter((a) => a === "move_left").length >= 3, `repeated: ${actions()}`);
    assert.equal(actions().includes("stop"), false, "no stop while held");
    // A "stop" the NAS drops (429) is sent again: the camera must not run on.
    state.ptzDropStops = 1;
    await page.mouse.up();
    await until(() => actions().filter((a) => a === "stop").length === 2, "stop after release, again after 429");
    // Leaving the window while a direction is held stops it as well.
    await hold("Nach oben");
    await until(() => actions().at(-1) === "move_up", "moving up");
    await page.evaluate(() => window.dispatchEvent(new Event("blur")));
    await until(() => actions().at(-1) === "stop", "stop when the window loses the focus");
    await page.mouse.up();
    await page.getByRole("combobox", { name: "Gespeicherte Position" }).selectOption("tor");
    await until(() => ptz().at(-1)[2].preset === "tor", "preset sent");
    assert.deepEqual(ptz().at(-1)[2], { action: "preset", preset: "tor" });
    // Never more than ten commands a second.
    const times = ptz().map((r) => r[3]);
    for (let i = 1; i < times.length; i++)
      assert.ok(times[i] - times[i - 1] >= 90, `commands ${times[i] - times[i - 1]} ms apart`);
    // Talking: the NAS's reason in plain words.
    const talk = page.getByRole("button", { name: /Mit Haustür sprechen/ });
    const tb = await talk.boundingBox();
    await page.mouse.move(tb.x + tb.width / 2, tb.y + tb.height / 2);
    await page.mouse.down();
    await page.locator('[data-camera="haustuer"] .talk-note').getByText(de["nvr.talk.not_running"]).waitFor();
    await page.mouse.up();
    await page.locator('[data-camera="haustuer"] .talk-note').getByRole("button").click();
    await sound(page, "live");
    await shot(page, "live");
    await page.getByRole("button", { name: "Garten bewegen" }).first().click();

    await page.getByRole("button", { name: "Anordnen" }).click();
    await page.getByText(/Kamera anfassen und verschieben/).waitFor();
    const cell = (await page.locator(".wall").boundingBox()).width / 24;
    // Size first: four columns narrower, two rows shorter.
    const grip = await page.locator('[data-tile="haustuer"] .grip').boundingBox();
    await page.mouse.move(grip.x + 10, grip.y + 10);
    await page.mouse.down();
    await page.mouse.move(grip.x + 10 - cell * 4, grip.y + 10 - cell * 2, { steps: 6 });
    await page.locator('[data-tile="haustuer"] .size').waitFor();
    await page.mouse.up();
    // Then move it two columns to the right and one row down.
    const tile = await box("haustuer");
    await page.mouse.move(tile.x + 40, tile.y + 40);
    await page.mouse.down();
    await page.mouse.move(tile.x + 40 + cell * 2, tile.y + 40 + cell, { steps: 6 });
    await page.mouse.up();
    // The keyboard arranges too.
    await page.locator('[data-tile="einfahrt"]').focus();
    await page.keyboard.press("ArrowDown");
    await sound(page, "arrange");
    await shot(page, "live-anordnen");
    assert.equal(saved.length, 0, "nothing is saved while arranging");
    await page.getByRole("button", { name: "Fertig" }).click();
    await page.waitForFunction(() => !document.querySelector(".wall.arranging"));
    await until(() => saved.length === 1, "the arrangement is saved");
    const nvr = saved[0].nvr;
    assert.equal(nvr.custom, true);
    const by = Object.fromEntries(nvr.tiles.map((t) => [t.id, t]));
    assert.deepEqual(by.haustuer, { id: "haustuer", x: 2, y: 1, w: 8, h: 5 });
    assert.deepEqual(by.einfahrt, { id: "einfahrt", x: 12, y: 1, w: 12, h: 7 });
    const after = await box("haustuer");
    assert.ok(Math.abs(after.x - before.x - cell * 2) < 3, "the tile moved");
    assert.ok(Math.abs(after.width - (cell * 8 - 6)) < 3, "the tile was sized");

    // The arrangement comes back after a reload.
    assert.deepEqual(data.prefs.desktop.nvr.tiles.length, 4);
    await page.reload();
    await page.locator("[data-tile]").nth(3).waitFor();
    const again = await box("haustuer");
    assert.ok(Math.abs(again.x - after.x) < 2 && Math.abs(again.width - after.width) < 2);

    // Events beside the pictures are an option, remembered too.
    assert.equal(await page.getByRole("complementary", { name: "Ereignisse" }).count(), 0);
    await page.getByRole("button", { name: "Ereignisse" }).click();
    const panel = page.getByRole("complementary", { name: "Ereignisse" });
    await panel.getByText("Person · Haustür").first().waitFor();
    await panel.getByText(/Unser Auto · K-TI 2026/).waitFor();
    await until(() => saved.at(-1).nvr.events === true, "events are remembered");
    await sound(page, "live with events");
    await shot(page, "live-ereignisse");

    // The menu "Ansicht": switches and "Anordnung zurücksetzen".
    await page.getByRole("button", { name: "Ansicht" }).click();
    const view = page.getByRole("group", { name: "Ansicht" });
    await view.getByText("Kameranamen im Bild").waitFor();
    await shot(page, "live-ansicht");
    await view.getByRole("switch", { name: "Kameranamen im Bild" }).uncheck();
    await page.waitForFunction(() => !document.querySelector("[data-tile] .name"));
    await until(() => saved.at(-1).nvr.names === false, "names off is remembered");
    await view.getByRole("button", { name: "Anordnung zurücksetzen" }).click();
    await page.waitForFunction(
      (x) => Math.abs(document.querySelector('[data-tile="haustuer"]').getBoundingClientRect().x - x) < 3,
      before.x,
    );
    await until(() => saved.at(-1).nvr.custom === false, "the reset is saved");
    await page.keyboard.press("Escape");

    // One camera large and back.
    await page.getByRole("button", { name: "Haustür groß zeigen" }).click();
    await page.locator(".tile.solo").waitFor();
    assert.equal(await page.locator("[data-tile]").count(), 0);
    await shot(page, "live-gross");
    await page.keyboard.press("Escape");
    await page.locator("[data-tile]").nth(3).waitFor();
    await ctx.close();
  }

  // --- Talking over plain http: the browser has no microphone there, said in plain words ---
  {
    const { ctx, page } = await open("owner");
    await ctx.addInitScript(() =>
      Object.defineProperty(window, "isSecureContext", { get: () => false }),
    );
    await page.goto(base + "/nvr");
    const talk = page.getByRole("button", { name: /Mit Haustür sprechen/ });
    await talk.focus();
    await page.keyboard.down("Space");
    await page.locator('[data-camera="haustuer"] .talk-note').getByText(de["nvr.talk.insecure"]).waitFor();
    await page.keyboard.up("Space");
    await sound(page, "talk over http");
    await ctx.close();
  }

  // --- An account without the admin role sees only what it was given ---
  {
    const { ctx, page, state } = await open("user");
    await page.goto(base + "/nvr");
    await page.locator('[data-tile="haustuer"]').waitFor();
    assert.equal(await page.locator("[data-tile]").count(), 1);
    assert.equal(await page.getByRole("button", { name: "Ereignisse" }).count(), 0, "no recordings right, no events");
    await sound(page, "live as user");
    await shot(page, "live-benutzer");
    await page.goto(base + "/nvr/recordings");
    await page.getByText(de["nvr.rec.noneUser"]).waitFor();
    for (const path of ["/nvr/cameras", "/nvr/settings", "/nvr/diagnosis"]) {
      await page.goto(base + path);
      await page.getByRole("alert").filter({ hasText: /Administrator|nicht erlaubt|Berechtigung|Rechte/ }).first().waitFor();
    }
    assert.equal(state.requests.some(([, p]) => p.startsWith("/nvr/diagnosis") || p.startsWith("/nvr/settings")), false);
    // No camera at all: a sentence, no empty wall.
    state.rights = {};
    await page.goto(base + "/nvr");
    await page.getByText(de["nvr.live.noneUser"]).waitFor();
    await ctx.close();
  }
  {
    // The privacy notice: dismissed by an administrator for the NAS.
    const { ctx, page, state } = await open("owner", (s) => (s.settings.privacyNoticeDismissed = false));
    await page.goto(base + "/nvr");
    await page.locator("#nvr-privacy").getByText(de["nvr.privacy.text"]).waitFor();
    await page.locator("#nvr-privacy").getByRole("button", { name: "Verstanden" }).click();
    await page.locator("#nvr-privacy").waitFor({ state: "detached" });
    assert.deepEqual(sent(state, "PUT", "/nvr/settings"), [{ privacyNoticeDismissed: true }]);
    await ctx.close();
  }

  // --- Settings ---
  {
    const { ctx, page, state } = await open("owner");
    await ctx.grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.goto(base + "/nvr/settings");
    await page.getByText("Belegt: 21,4 GB · Aufnahmen 20,4 GB, Exporte 1 GB · reicht etwa 12,4 Tage zurück").waitFor();
    await page.getByText("Läuft gerade auf: NPU (Intel AI Boost) · 11,4 ms je Bild").waitFor();
    await page.locator("#nvr-cap").fill("300");
    await page.locator("#nvr-cap-row").getByRole("button", { name: "Speichern" }).click();
    await page.getByText("Gespeichert.").waitFor();
    assert.deepEqual(sent(state, "PUT", "/nvr/settings").at(-1), { storage: { limitGB: 300 } });
    await page.locator("#nvr-detector").selectOption("igpu");
    await page.waitForFunction(() => document.querySelector("#nvr-detector").value === "igpu");
    assert.deepEqual(sent(state, "PUT", "/nvr/settings").at(-1), { detector: { kind: "igpu" } });

    // Known faces: people, pictures, faces to assign.
    await page.locator("#nvr-faces").getByRole("button", { name: /Bekannte Gesichter/ }).click();
    const faces = page.locator("#nvr-faces");
    await faces.locator('[data-face="Patrick"] b').waitFor();
    await faces.getByText("vermutlich Patrick (62 %)").waitFor();
    await faces.getByRole("button", { name: "Zuordnen" }).first().click();
    await page.waitForFunction(() => document.querySelectorAll("#nvr-faces .attempts li").length === 1);
    const assigned = state.requests.find(([m, p]) => m === "POST" && p.endsWith("/assign"));
    assert.match(assigned[1], /^\/nvr\/face-attempts\/[^/]+\/assign$/);
    assert.deepEqual(assigned[2], { name: "Patrick" });
    // The library comes back with the answer: Patrick has his third picture.
    await faces.locator('[data-face="Patrick"]').getByText("3 Bilder").waitFor();
    // Names follow the NAS's rule; what is wrong is said under the field.
    const newName = faces.getByLabel("Name der Person");
    await faces.getByText(de["nvr.faces.nameRule"]).first().waitFor();
    await newName.fill("Oma Erna");
    await faces.getByText(de["nvr.faces.nameProblem.space"]).waitFor();
    assert.equal(await newName.getAttribute("aria-invalid"), "true");
    assert.equal(await faces.getByRole("button", { name: "Person anlegen" }).isDisabled(), true);
    await newName.fill("train");
    await faces.getByText(de["nvr.faces.nameProblem.train"]).waitFor();
    await newName.fill("Jürgen_2");
    await faces.getByText(de["nvr.faces.nameRule"]).first().waitFor();
    await faces.getByRole("button", { name: "Person anlegen" }).click();
    await faces.locator('[data-face="Jürgen_2"] b').waitFor();
    assert.deepEqual(sent(state, "POST", "/nvr/faces"), [{ name: "Jürgen_2" }]);
    await sound(page, "faces");
    await faces.screenshot({ path: `${out}/gesichter-${WIDTH}.png`, animations: "disabled" });

    // Known plates: a read plate becomes a known one.
    await page.locator("#nvr-plates").getByRole("button", { name: /Bekannte Kennzeichen/ }).click();
    const plates = page.locator("#nvr-plates");
    await plates.getByText("B-XY 987").waitFor();
    // A plate older than the newest 1000 vehicle events: nothing known, said with "–".
    await plates.locator("li", { hasText: "HH-Q 4" }).getByText("gesehen: – · zuletzt: – · Kamera: –").waitFor();
    await plates.getByText(de["nvr.plates.seenOld"]).waitFor();
    await plates.locator("li", { hasText: "B-XY 987" }).getByText(/^gesehen: 1 · zuletzt: .+ · Kamera: Einfahrt$/).waitFor();
    await plates.locator("li", { hasText: "B-XY 987" }).getByRole("button", { name: "Als bekannt übernehmen" }).click();
    await plates.getByLabel("Name", { exact: true }).last().fill("Nachbar");
    await plates.getByRole("button", { name: "Speichern" }).click();
    await page.waitForFunction(() => !!document.querySelector("#nvr-plates .seen") && document.querySelectorAll("#nvr-plates .seen .chip").length === 2);
    assert.deepEqual(sent(state, "PUT", "/nvr/settings").at(-1), {
      plates: [
        { name: "Unser Auto", plates: ["K-TI 2026"] },
        { name: "Nachbar", plates: ["B-XY 987"] },
      ],
    });

    // The RTSP output for Home Assistant shows its generated login.
    await page.getByRole("switch", { name: /Kamerabilder für Home Assistant/ }).check();
    await page.locator("#nvr-rtsp-login").getByText("titanos", { exact: true }).waitFor();
    await page.locator("#nvr-rtsp-login").getByText("k3Jw-91Lm-x0Qp-77Zt").waitFor();
    await page.getByRole("button", { name: "Passwort kopieren" }).click();
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), "k3Jw-91Lm-x0Qp-77Zt");
    await page.locator("#nvr-privacy-row").getByText(de["nvr.privacy.text"]).waitFor();
    await sound(page, "settings");
    await shot(page, "einstellungen");

    // A sleeping volume is taken only after a plain question.
    state.sleepingVolumes = ["Fast"];
    await page.locator("#nvr-volume").selectOption("Fast");
    const sleep = confirmDialog(page, "Platten bleiben wach");
    await sleep.getByRole("button", { name: "Dieses Volume nehmen" }).click();
    await sleep.waitFor({ state: "detached" });
    assert.deepEqual(sent(state, "PUT", "/nvr/settings").at(-1), { volume: "Fast", confirmSleepingVolume: true });

    // Switching the NVR off: a plain question that says what stays.
    await page.getByRole("button", { name: "NVR ausschalten" }).click();
    const off = confirmDialog(page, "NVR ausschalten?");
    await off.getByText(de["nvr.settings.offKeeps"]).waitFor();
    assert.equal(await off.locator("input").count(), 0);
    await shot(page, "einstellungen-ausschalten");
    await off.getByRole("button", { name: "NVR ausschalten" }).click();
    await page.getByRole("heading", { name: "NVR einrichten" }).waitFor();
    assert.equal(sent(state, "POST", "/nvr/disable").length, 1);
    await ctx.close();
  }

  // --- The detection moved to another device: banner, reason, "Wieder versuchen" ---
  {
    const { ctx, page, state } = await open("owner", (s) => {
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
    const banner = page.locator("#nvr-failover");
    await banner.getByText("Die Erkennung läuft jetzt auf: iGPU (Intel Arc) (gewählt war: NPU (Intel AI Boost))").waitFor();
    await banner.getByText(de["nvr.reason.npu_firmware_old.title"]).waitFor();
    await banner.getByText("Aufnahmen laufen weiter.").waitFor();
    await sound(page, "failover");
    await shot(page, "ausweichen");
    await banner.getByRole("button", { name: "Wieder versuchen" }).click();
    await banner.waitFor({ state: "detached" });
    assert.equal(sent(state, "POST", "/nvr/detector/retry").length, 1);
    await ctx.close();
  }

  // --- The exports alone exceed the limit: nothing is deleted, said in plain words ---
  {
    const { ctx, page } = await open("owner", (s) => {
      s.storage = {
        ...s.storage,
        usedBytes: 250.4e9,
        recordingsBytes: 20.4e9,
        exportsBytes: 230e9,
        cap: "exports_exceed",
        lastCleanup: s.now - 3600,
      };
    });
    await page.goto(base + "/nvr/settings");
    await page.locator("#nvr-cap-note").getByText(de["nvr.storage.capExports"]).waitFor();
    await page.getByText("Belegt: 250 GB · Aufnahmen 20,4 GB, Exporte 230 GB · reicht etwa 12,4 Tage zurück").waitFor();
    await page.getByText(/^Zuletzt aufgeräumt: \d\d:\d\d$/).waitFor();
    await sound(page, "exports exceed");
    await shot(page, "einstellungen-exporte-ueber-grenze");
    await ctx.close();
  }

  // --- Diagnosis: every classified cause in plain words, the log, copying ---
  {
    const { ctx, page, state } = await open("owner");
    await ctx.grantPermissions(["clipboard-read", "clipboard-write"]);
    const causes = [
      "npu_firmware_old",
      "npu_turbo_unsupported",
      "npu_missing",
      "coral_missing",
      "config_invalid",
      "detector_unknown",
    ];
    for (const code of causes) {
      state.detector = {
        kind: code === "coral_missing" ? "coral" : "npu",
        active: "igpu",
        inferenceMs: 23.8,
        failover:
          code === "config_invalid"
            ? null
            : { from: "npu", to: "igpu", at: new Date((state.now - 600) * 1000).toISOString(), reason: code, detail: `raw line for ${code}` },
      };
      state.state = code === "config_invalid" ? "stopped" : "running";
      state.error = code === "config_invalid" ? { code, detail: "raw line for config_invalid", at: new Date(state.now * 1000).toISOString() } : null;
      await page.goto(base + "/nvr/diagnosis");
      const cause = page.locator("#nvr-diag-cause");
      for (const part of ["title", "what", "todo"])
        await cause.getByText(de[`nvr.reason.${code}.${part}`]).waitFor();
      await cause.getByText(code, { exact: true }).waitFor();
      // The NAS's own words wait behind "Einzelheit".
      assert.equal(await cause.getByText(`raw line for ${code}`).count(), 0);
      await cause.getByRole("button", { name: "Einzelheit" }).click();
      await cause.getByText(`raw line for ${code}`).waitFor();
      if (code === "config_invalid") {
        await page.getByText("Angehalten", { exact: true }).waitFor();
        await page.getByRole("button", { name: "Wieder versuchen" }).waitFor();
      }
      await sound(page, `diagnosis ${code}`);
      if (code === "npu_firmware_old") await shot(page, "diagnose-ausweichen");
      if (code === "config_invalid") await shot(page, "diagnose-angehalten");
    }
    state.state = "running";
    state.error = null;
    state.detector = { kind: "auto", active: "npu", failover: null, inferenceMs: 11.4 };
    await page.goto(base + "/nvr/diagnosis");
    await page.getByText("Läuft", { exact: true }).waitFor();
    await page.getByText("11,4 ms je Bild").waitFor();
    await page.getByText("läuft (Nummer 412)").waitFor();
    await page.getByText("vorhanden", { exact: true }).waitFor();
    await page.getByText(/vpu_37xx_v1\.bin/).waitFor();
    await page.getByText("143 von 384 MB belegt").waitFor();
    await page.getByText("Kamera 15 Bilder/s · Erkennung 1,2 · ausgewertet 5 · übersprungen 0").first().waitFor();
    assert.equal(await page.locator("#nvr-diag-cause").count(), 0);
    // The log wraps: it never scrolls sideways and nothing is cut.
    const log = page.locator("pre.log");
    await log.getByText(/a very long line that has to wrap/).waitFor();
    assert.equal(await log.evaluate((el) => el.scrollWidth <= el.clientWidth + 1), true);
    await page.getByRole("button", { name: "Protokoll kopieren" }).click();
    assert.match(await page.evaluate(() => navigator.clipboard.readText()), /Starting Frigate \(0\.18\.2\)/);
    await page.getByRole("button", { name: "Details kopieren" }).click();
    const details = JSON.parse(await page.evaluate(() => navigator.clipboard.readText()));
    assert.equal(details.imageVersion, "0.18.2");
    assert.equal(details.detector.active, "npu");
    await sound(page, "diagnosis");
    await shot(page, "diagnose");
    await ctx.close();
  }

  // --- Recordings: time line, export with its list ---
  {
    const { ctx, page, state } = await open("owner");
    await page.goto(base + "/nvr/recordings?camera=einfahrt");
    await page.getByRole("slider", { name: "Zeitleiste des Tages" }).waitFor();
    await page.locator(".timeline .piece").first().waitFor();
    assert.ok((await page.locator(".timeline .piece").count()) < 400, "segments are joined to runs");
    assert.ok((await page.locator(".timeline .event").count()) >= 1);
    await page.getByText("Wähle auf der Zeitleiste eine Zeit.").waitFor();
    // 07:00 of today lies inside the recorded stretch.
    const track = await page.locator(".timeline .track").boundingBox();
    // One pixel is about a minute of the day: 07:00 or the minute after.
    await page.mouse.click(track.x + (track.width * 7) / 24 + 0.5, track.y + 20);
    await page.getByRole("status").filter({ hasText: /^07:0[01]:\d\d$/ }).waitFor();
    // The player asks for the HLS list of the hour (hls.js loads first where the browser has no HLS).
    await until(
      () => state.requests.some(([m, p]) => m === "GET" && /^\/nvr\/cameras\/einfahrt\/vod\/\d+\/\d+\/master\.m3u8$/.test(p)),
      "the player asks for the HLS list of the hour",
    );
    // A removed camera's recordings, for administrators, with the day they end.
    assert.deepEqual(
      await page.locator(".pick select optgroup option").allTextContents(),
      ["Schuppen"],
    );
    await page.locator(".pick select").selectOption("schuppen");
    await page.getByText(/^Entfernt – Aufnahmen bis \d\d\.\d\d\.\d{4}$/).waitFor();
    await page.locator(".pick select").selectOption("einfahrt");
    await page.locator(".timeline .piece").first().waitFor();
    await page.mouse.click(track.x + (track.width * 7) / 24 + 0.5, track.y + 20);
    await page.getByRole("status").filter({ hasText: /^07:0[01]:\d\d$/ }).waitFor();
    await page.getByRole("radio", { name: "6 h" }).click();
    assert.ok(await page.locator(".timeline .inner").evaluate((el) => el.scrollWidth > el.parentElement.clientWidth * 3));
    await page.getByRole("radio", { name: "24 h" }).click();
    await page.getByRole("button", { name: "Exportieren" }).click();
    const dialog = page.getByRole("dialog", { name: "Ausschnitt exportieren" });
    assert.match(await dialog.getByLabel("Von").inputValue(), /^07:0[01]:\d\d$/);
    await dialog.getByLabel("Von").fill("07:00:00");
    await dialog.getByLabel("Bis").fill("07:05:00");
    await dialog.getByLabel("Name").fill("Lieferung");
    await shot(page, "aufnahmen-export");
    await dialog.getByRole("button", { name: "Exportieren" }).click();
    await page.locator('[data-export^="einfahrt_new"]').getByText("wird erstellt").waitFor();
    const made = sent(state, "POST", "/nvr/exports")[0];
    assert.deepEqual(made, {
      camera: "einfahrt",
      from: state.midnight + 7 * 3600,
      to: state.midnight + 7 * 3600 + 300,
      name: "Lieferung",
    });
    // A failed export says why in plain words; one made outside TitanOS has no period.
    await page.locator('[data-export="garten_ij56kl"]').getByText(de["nvr.export.failedHint"]).waitFor();
    await page.locator('[data-export="garten_ij56kl"]').getByText("fehlgeschlagen").waitFor();
    assert.equal(await page.locator('[data-export="garten_ij56kl"]').getByRole("link", { name: "Herunterladen" }).count(), 0);
    await page.locator('[data-export="haustuer_frigate"]').getByText(/^Haustür · Zeitraum – · angelegt .+ · 4,2 MB$/).waitFor();
    const done = page.locator('[data-export="haustuer_ab12cd"]');
    assert.equal(
      await done.getByRole("link", { name: "Herunterladen" }).getAttribute("href"),
      "/api/v1/nvr/exports/haustuer_ab12cd/download",
    );
    await done.getByRole("button", { name: "Umbenennen" }).click();
    const rename = page.getByRole("dialog", { name: "Umbenennen" });
    await rename.getByLabel("Name").fill("Paketbote DHL");
    await rename.getByRole("button", { name: "Speichern" }).click();
    await done.getByText("Paketbote DHL").waitFor();
    assert.deepEqual(sent(state, "PUT", "/nvr/exports/haustuer_ab12cd")[0], { name: "Paketbote DHL" });
    await sound(page, "recordings");
    await shot(page, "aufnahmen");
    await page.locator("#nvr-exports").screenshot({ path: `${out}/exporte-${WIDTH}.png`, animations: "disabled" });
    await done.getByRole("button", { name: "Löschen" }).click();
    const remove = confirmDialog(page, "Export löschen?");
    assert.equal(await remove.locator("input").count(), 0);
    await remove.getByRole("button", { name: "Löschen" }).click();
    await done.waitFor({ state: "detached" });
    await ctx.close();
  }

  // --- Events: filter, detail, delete ---
  {
    const { ctx, page, state } = await open("owner");
    await page.goto(base + "/nvr/events");
    await page.getByRole("button", { name: /Auto · Einfahrt/ }).waitFor();
    assert.equal(await page.locator(".grid .card").count(), 5, "the last seven days");
    await sound(page, "events");
    await shot(page, "ereignisse");
    await page.getByLabel("Art", { exact: true }).selectOption("vehicles");
    await page.waitForFunction(() => document.querySelectorAll(".grid .card").length === 1);
    assert.ok(state.requests.some(([, p]) => p.startsWith("/nvr/events?") && p.includes("label=car%2Cmotorcycle%2Cbicycle%2Cbus")));
    await page.getByRole("button", { name: /Auto · Einfahrt/ }).click();
    const detail = page.getByRole("dialog", { name: "Auto · Einfahrt" });
    await detail.getByText("Unser Auto").waitFor();
    await detail.getByText("K-TI 2026").waitFor();
    await detail.getByText("87 %").waitFor();
    assert.match(
      await detail.getByRole("link", { name: "Ausschnitt herunterladen" }).getAttribute("href"),
      /\/clip\.mp4\?download=1$/,
    );
    assert.ok(state.requests.some(([m, p, b]) => m === "POST" && p.endsWith("/reviewed") && b.reviewed === true));
    await shot(page, "ereignis");
    await detail.getByRole("button", { name: "Löschen" }).click();
    const remove = confirmDialog(page, "Ereignis löschen?");
    assert.equal(await remove.locator("input").count(), 0);
    await remove.getByRole("button", { name: "Löschen" }).click();
    await page.getByText("Keine Ereignisse für diese Auswahl.").waitFor();
    assert.equal(state.requests.filter(([m, p]) => m === "DELETE" && p.startsWith("/nvr/events/")).length, 1);
    await page.getByLabel("Art", { exact: true }).selectOption("");
    await page.getByRole("radio", { name: "Liste" }).click();
    await page.locator(".list .event-card").first().waitFor();
    await sound(page, "events list");
    await ctx.close();
  }

  // --- Users: the group "Kameras" beside "Apps" and "Freigaben" ---
  {
    const { ctx, page, state } = await open("owner");
    await page.goto(base + "/users");
    await page.locator('[data-account="sam"]').click();
    const group = page.locator("section.expander", { hasText: "Kameras" }).last();
    await group.getByRole("button", { name: /Kameras/ }).click();
    await group.getByText("Live: 0 von 4 · Aufnahmen: 0 von 4").waitFor();
    await group.getByRole("switch", { name: "Haustür: Live ansehen" }).check();
    await group.getByText("Live: 1 von 4 · Aufnahmen: 0 von 4").waitFor();
    await group.getByRole("switch", { name: "Garten: Aufnahmen ansehen" }).check();
    await group.getByText("Live: 1 von 4 · Aufnahmen: 1 von 4").waitFor();
    assert.deepEqual(sent(state, "PUT", "/users/sam/cameras"), [
      { cameras: { haustuer: { live: true, recordings: false } } },
      { cameras: { garten: { live: false, recordings: true } } },
    ]);
    await sound(page, "user cameras");
    await shot(page, "benutzer-kameras");
    await ctx.close();
  }

  // --- The desktop: the program in the menu and the camera widget ---
  {
    const { ctx, page, data } = await open("owner", (_s, data) => {
      data.prefs.desktop = {
        version: 1,
        customWidgets: true,
        widgets: [{ id: "wcam", kind: "camera", x: 12, y: 2, w: 20, h: 13, camera: "garten" }],
      };
    });
    await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
    await page.goto(base + "/");
    const widget = page.locator('.widget[data-widget="camera"]');
    await widget.locator("img").waitFor();
    await widget.getByText("Garten").waitFor();
    await shot(page, "widget");
    await widget.getByRole("button", { name: "Garten im NVR öffnen" }).click();
    const frame = page.frameLocator('iframe[data-app="app:nvr"]');
    await frame.locator(".tile.solo").waitFor();
    // The window's side bar lists the NVR's pages; it folds to its symbols.
    const side = page.locator("section.win nav.side");
    for (const name of ["Live", "Aufnahmen", "Ereignisse", "Kameras", "Einstellungen", "Diagnose"])
      await side.getByRole("link", { name, exact: true }).or(side.getByRole("button", { name, exact: true })).first().waitFor();
    await shot(page, "fenster");
    // The left menu folds to its symbols and stays folded for the account.
    await side.getByRole("button", { name: "Seitenleiste einklappen" }).click();
    await page.locator("section.win nav.side.folded").waitFor();
    await until(() => (data.prefs.desktop.folded ?? []).includes("app:nvr"), "folding is remembered");
    await side.getByRole("button", { name: "Seitenleiste ausklappen" }).click();
    await page.locator("section.win nav.side:not(.folded)").waitFor();
    await ctx.close();
  }

  // --- Every page in a maximized window of the desktop (what the owner sees at 1280 px) ---
  const windowShots = [
    ["einrichtung", "/nvr", (s) => ((s.settings.enabled = false), (s.settings.privacyNoticeDismissed = false))],
    ["live", "/nvr"],
    ["live-ereignisse", "/nvr", (_s, data) => (data.prefs.desktop.nvr = { events: true })],
    ["aufnahmen", "/nvr/recordings?camera=einfahrt"],
    ["ereignisse", "/nvr/events"],
    ["ereignis", "/nvr/events?event=EVENT2"],
    ["kameras", "/nvr/cameras"],
    ["kamera-aendern", "/nvr/cameras?edit=haustuer"],
    ["kamera-hinzufuegen", "/nvr/cameras?new=1"],
    ["einstellungen", "/nvr/settings", (s) => ((s.settings.rtspOut = true), (s.settings.rtspUser = "titanos"), (s.settings.rtspPassword = "k3Jw-91Lm-x0Qp-77Zt"))],
    ["diagnose-ausweichen", "/nvr/diagnosis", (s) => {
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
    }],
  ];
  for (const [name, path, change = () => {}] of windowShots) {
    const { ctx, page, state } = await open("owner", (s, data) => {
      data.prefs.desktop = { version: 1 };
      change(s, data);
    });
    await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
    await page.goto(base + path.replace("EVENT2", encodeURIComponent(state.events[1].id)));
    const win = page.locator("section.win").first();
    await win.waitFor();
    await win.locator("header.titlebar").dblclick({ position: { x: 300, y: 10 } });
    const handle = await page.locator('iframe[data-app="app:nvr"]').elementHandle();
    const frame = await handle.contentFrame();
    await frame.locator("h1, h2").first().waitFor();
    if (name === "kamera-hinzufuegen") {
      await frame.getByRole("button", { name: "Suchen", exact: true }).click();
      await frame.getByRole("button", { name: /Reolink RLC-810A/ }).click();
      await frame.getByLabel("Benutzer", { exact: true }).fill("admin");
      await frame.getByLabel("Passwort", { exact: true }).fill("geheim");
      await frame.getByRole("button", { name: "Prüfen" }).click();
      await frame.getByText("Verbindung steht.").waitFor();
    }
    if (name === "aufnahmen") await frame.locator(".timeline .piece").first().waitFor();
    if (name.startsWith("live")) await frame.locator(".tile-view img").first().waitFor();
    if (name === "live-ereignisse") await frame.getByRole("complementary", { name: "Ereignisse" }).waitFor();
    // Dialogs fade in; the picture waits for them.
    await page.waitForTimeout(name === "ereignis" ? 1500 : 700);
    assert.equal(
      await frame.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth),
      0,
      `${name} in a window: nothing sideways`,
    );
    assert.deepEqual(await frame.evaluate(cutTextIn), [], `${name} in a window: cut text`);
    await page.screenshot({ path: `${out}/fenster-${name}-${WIDTH}.png`, animations: "disabled" });
    await ctx.close();
  }

  assert.deepEqual(errors, [], "no script errors");
  console.log("NVR: ok");
} finally {
  await browser.close();
}
