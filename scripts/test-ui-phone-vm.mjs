// PLAN 87: the VM screen on a phone. noVNC takes keys only on its canvas,
// which never opens a phone's keyboard, and drops what phone keyboards send
// (key "Unidentified", code 229, text as input). Checked here against a
// synthetic RFB peer that records the keysyms and pointer messages noVNC
// sends: the key bar opens the keyboard (hidden text field), typed text and
// an Android-style key event reach the VM, Backspace, Ctrl held from the bar,
// Esc/Tab/arrows/F-keys/Ctrl+Alt+Del; the finger as trackpad (cursor, tap =
// left click, two-finger tap = right click, two-finger drag = wheel) and
// directly; the large screen fills the phone with the bar below. Phone
// emulation (isMobile, touch, 3x) at 390×844; isolated data only, no NAS, no
// real VM, no real phone.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5267 node scripts/test-ui-phone-vm.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
import { rfbPeer } from "./test-ui-rfb-peer.mjs";

const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5267";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const out = process.env.UI_OUTPUT || "out/validation/phone-vm";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const create = fixtureFactory(browser, errors);

const vm = {
  name: "vm1",
  displayName: "Debian",
  state: "running",
  template: "linux",
  memoryMiB: 2048,
  vcpus: 2,
  description: "",
  exclusive: false,
  folders: [],
  tpm: false,
  secureBoot: false,
  pci: [],
  usb: [],
  job: null,
  available: true,
  cpuPercent: 3,
  memoryBytes: 2e9,
  since: "2026-10-24T09:15:00Z",
  cpuPin: [],
  firmware: "uefi",
  iso: "",
  network: "bridge",
  autostart: true,
  volume: "Data",
  disks: [],
  mac: "52:54:00:00:00:01",
};

async function phone(theme = "graphit", locale = "de") {
  const made = await create("owner", theme, 390, locale, {
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 3,
    viewport: { width: 390, height: 844 },
  });
  const { ctx, page } = made;
  await ctx.addInitScript(() => {
    localStorage.removeItem("titanos.layout");
    localStorage.removeItem("titanos.vnc.pointer");
  });
  await ctx.addInitScript(rfbPeer);
  await ctx.route("**/api/v1/vms", (r) =>
    r.fulfill({
      json: {
        kvm: true,
        bridge: true,
        vms: [vm],
        host: { cpus: 8, memoryBytes: 16e9, usedMemoryBytes: 2e9 },
      },
    }),
  );
  await ctx.route("**/api/v1/terminal", (r) =>
    r.fulfill({ json: { enabled: true } }),
  );
  await page.goto(base + "/vms?vm=vm1&tab=screen");
  await page.waitForFunction(
    () => document.querySelector(".frame canvas")?.width === 640,
    null,
    { timeout: 20000 },
  );
  await page.waitForTimeout(400);
  return made;
}

const keys = (page) =>
  page.evaluate(() => window.__vncFixture.keyEvents.splice(0));
const pointer = (page) =>
  page.evaluate(() => window.__vncFixture.pointer.splice(0));
const downs = (events) => events.filter((e) => e.down).map((e) => e.keysym);
const field = (page) =>
  page.evaluate(() => !!document.activeElement?.matches("[data-vnc-keyboard]"));

// CDP touch: several fingers at once (Playwright's tap has one).
async function touch(cdp, type, points) {
  await cdp.send("Input.dispatchTouchEvent", {
    type,
    touchPoints: points.map(([x, y], id) => ({ x, y, id })),
  });
}

try {
  const { ctx, page } = await phone();
  const cdp = await ctx.newCDPSession(page);
  const bar = page.locator("[data-vnc-keybar]");
  await bar.waitFor();
  // Every key is a 44 px target and lies on the screen.
  for (const box of await bar
    .locator("button")
    .evaluateAll((els) => els.map((e) => e.getBoundingClientRect().toJSON())))
    assert.ok(
      box.width >= 43.5 && box.height >= 43.5 && box.right <= 390.5,
      `key ≥ 44 px on the screen (${JSON.stringify(box)})`,
    );
  // The canvas belongs to the VM, not to the page's scrolling.
  assert.equal(
    await page
      .locator(".frame canvas")
      .evaluate((el) => getComputedStyle(el).touchAction),
    "none",
  );

  // The phone's keyboard: the bar's button focuses the hidden text field.
  await bar.locator('[data-key="keyboard"]').tap();
  assert.ok(await field(page), "keyboard button focuses the text field");
  assert.equal(
    await bar.locator('[data-key="keyboard"]').getAttribute("aria-pressed"),
    "true",
  );
  await keys(page);
  // What an on-screen keyboard types arrives as input, without keydown.
  await page.keyboard.insertText("ab");
  assert.deepEqual(downs(await keys(page)), [0x61, 0x62], "typed text");
  // Android: keydown "Unidentified" (229) with the text – before PLAN 87
  // noVNC dropped it.
  await cdp.send("Input.dispatchKeyEvent", {
    type: "keyDown",
    key: "Unidentified",
    code: "",
    windowsVirtualKeyCode: 229,
    nativeVirtualKeyCode: 229,
    text: "ü",
    unmodifiedText: "ü",
  });
  await cdp.send("Input.dispatchKeyEvent", {
    type: "keyUp",
    key: "Unidentified",
    windowsVirtualKeyCode: 229,
  });
  assert.deepEqual(downs(await keys(page)), [0xfc], "Android key event");
  // Backspace, also on an "empty" field, and Enter.
  await page.keyboard.press("Backspace");
  await page.keyboard.press("Enter");
  assert.deepEqual(
    downs(await keys(page)),
    [0xff08, 0xff0d],
    "Backspace, Enter",
  );
  // A keyboard rewriting a word (autocorrect): Backspaces, then the new letters.
  await page.evaluate(() => {
    const el = document.querySelector("[data-vnc-keyboard]");
    el.value = el.value + "teh";
    el.dispatchEvent(new InputEvent("input", { bubbles: true }));
    el.value = el.value.slice(0, -3) + "the ";
    el.dispatchEvent(new InputEvent("input", { bubbles: true }));
  });
  assert.deepEqual(
    downs(await keys(page)),
    [0x74, 0x65, 0x68, 0xff08, 0xff08, 0x68, 0x65, 0x20],
    "autocorrect",
  );

  // Ctrl held from the bar for the next key; then let go. The keyboard stays open.
  await bar.locator('[data-key="ctrl"]').tap();
  assert.equal(
    await bar.locator('[data-key="ctrl"]').getAttribute("aria-pressed"),
    "true",
  );
  assert.match(await bar.locator('[data-key="ctrl"]').innerText(), /gehalten/);
  assert.ok(await field(page), "keyboard stays open on a key of the bar");
  await page.keyboard.insertText("c");
  const ctrlC = await keys(page);
  assert.deepEqual(
    ctrlC.map((e) => [e.keysym, e.down]),
    [
      [0xffe3, true],
      [0x63, true],
      [0x63, false],
      [0xffe3, false],
    ],
    "Ctrl+C",
  );
  assert.equal(
    await bar.locator('[data-key="ctrl"]').getAttribute("aria-pressed"),
    "false",
    "Ctrl let go after one key",
  );
  // Keys a phone keyboard lacks.
  for (const name of ["esc", "tab", "left", "up", "down", "right"])
    await bar.locator(`[data-key="${name}"]`).tap();
  assert.deepEqual(
    downs(await keys(page)),
    [0xff1b, 0xff09, 0xff51, 0xff52, 0xff54, 0xff53],
  );
  await bar.locator('[data-key="fkeys"]').tap();
  await bar.locator('[data-key="f5"]').tap();
  await bar.locator('[data-key="f12"]').tap();
  assert.deepEqual(downs(await keys(page)), [0xffc2, 0xffc9], "F5, F12");
  await bar.locator('[data-key="fkeys"]').tap();
  await bar.locator('[data-key="cad"]').tap();
  assert.deepEqual(
    downs(await keys(page)),
    [0xffe3, 0xffe9, 0xffff],
    "Ctrl+Alt+Del",
  );
  await bar.locator('[data-key="keyboard"]').tap();
  assert.ok(!(await field(page)), "keyboard closes");

  // Trackpad (the default): the cursor starts in the middle.
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.locator(".frame").scrollIntoViewIfNeeded();
  const canvas = await page.locator(".frame canvas").boundingBox();
  const layer = page.locator("[data-touch-layer]");
  await layer.waitFor();
  await pointer(page);
  await page.touchscreen.tap(
    canvas.x + canvas.width - 40,
    canvas.y + canvas.height - 30,
  );
  let events = await pointer(page);
  assert.ok(
    events.some((e) => e.mask === 1) && events.at(-1).mask === 0,
    `tap = left click (${JSON.stringify(events)})`,
  );
  const clickAt = events.find((e) => e.mask === 1);
  assert.ok(
    Math.abs(clickAt.x - 320) <= 2 && Math.abs(clickAt.y - 180) <= 2,
    `a tap clicks at the cursor, not at the finger (${JSON.stringify(clickAt)})`,
  );
  assert.ok(await page.locator("[data-trackpad-cursor]").isVisible());
  await page.waitForTimeout(400);
  // One finger moves the cursor (relative, no button).
  const sx = canvas.x + 200,
    sy = canvas.y + 100;
  await touch(cdp, "touchStart", [[sx, sy]]);
  for (let i = 1; i <= 8; i++)
    await touch(cdp, "touchMove", [[sx - i * 10, sy - i * 5]]);
  await touch(cdp, "touchEnd", []);
  await page.waitForTimeout(150);
  events = await pointer(page);
  assert.ok(events.length > 0, "moving sends pointer moves");
  assert.ok(
    events.every((e) => e.mask === 0),
    "moving holds no button",
  );
  assert.ok(events.at(-1).x < 320 && events.at(-1).y < 180, "cursor moved");
  await page.waitForTimeout(400);
  // Two fingers tapped: right click.
  await touch(cdp, "touchStart", [
    [sx, sy],
    [sx + 60, sy],
  ]);
  await touch(cdp, "touchEnd", []);
  await page.waitForTimeout(150);
  events = await pointer(page);
  assert.ok(
    events.some((e) => e.mask === 4),
    `two-finger tap = right click (${JSON.stringify(events)})`,
  );
  // Two fingers dragged: the wheel (buttons 4/5 = mask 8/16).
  await page.waitForTimeout(400);
  await touch(cdp, "touchStart", [
    [sx, sy + 100],
    [sx + 60, sy + 100],
  ]);
  for (let i = 1; i <= 10; i++)
    await touch(cdp, "touchMove", [
      [sx, sy + 100 - i * 12],
      [sx + 60, sy + 100 - i * 12],
    ]);
  await touch(cdp, "touchEnd", []);
  await page.waitForTimeout(200);
  events = await pointer(page);
  assert.ok(
    events.some((e) => e.mask === 16 || e.mask === 8),
    `two-finger drag = wheel (${JSON.stringify(events.map((e) => e.mask))})`,
  );
  // noVNC's mouse cover is gone again: the page stays usable.
  assert.equal(
    await page.evaluate(
      () =>
        document.getElementById("noVNC_mouse_capture_elem")?.style.display ??
        "none",
    ),
    "none",
  );
  await page.screenshot({ path: `${out}/vm-trackpad-390.png` });

  // Direct: the finger clicks where it lands (noVNC's own gestures).
  await bar.locator('[data-pointer="direct"]').tap();
  assert.equal(await layer.count(), 0, "no trackpad layer in direct mode");
  await page.waitForTimeout(300);
  await pointer(page);
  const now = await page.locator(".frame canvas").boundingBox();
  await page.touchscreen.tap(now.x + now.width / 4, now.y + now.height / 4);
  await page.waitForTimeout(150);
  events = await pointer(page);
  const direct = events.find((e) => e.mask === 1);
  assert.ok(
    direct && Math.abs(direct.x - 160) <= 4 && Math.abs(direct.y - 90) <= 4,
    `direct tap clicks at the finger (${JSON.stringify(events)})`,
  );
  assert.equal(
    await page.evaluate(() => localStorage.getItem("titanos.vnc.pointer")),
    "direct",
    "the choice is kept in this browser",
  );
  await bar.locator('[data-pointer="trackpad"]').tap();

  // Large: picture and bar fill the phone.
  await bar.locator('[data-key="full"]').tap();
  const area = await page.locator(".screen-area.full").boundingBox();
  assert.ok(
    area.x === 0 && area.y === 0 && area.width === 390 && area.height >= 843,
    `large fills the phone (${JSON.stringify(area)})`,
  );
  const barBox = await bar.boundingBox();
  assert.ok(barBox.y + barBox.height <= 844.5, "key bar on the screen");
  await page.waitForTimeout(300);
  await page.screenshot({ path: `${out}/vm-gross-390.png` });
  await bar.locator('[data-key="full"]').tap();
  assert.equal(await page.locator(".screen-area.full").count(), 0);
  assert.ok(
    (await page.evaluate(() => document.documentElement.scrollWidth)) <= 390,
    "nothing wider than the screen",
  );
  await ctx.close();

  // The screen in a window of its own (/vms/screen) has the bar as well.
  {
    const { ctx, page } = await phone("hell", "en");
    await page.goto(base + "/vms/screen?vm=vm1");
    await page.locator("[data-vnc-keybar]").waitFor();
    assert.equal(
      (await page.locator('[data-key="keyboard"]').innerText()).trim(),
      "Keyboard",
    );
    await ctx.close();
  }
} catch (err) {
  console.error(err);
  process.exitCode = 1;
}
await browser.close();
const relevant = errors.filter(
  (e) => !/Failed to load resource|net::ERR/.test(e),
);
assert.deepEqual(relevant, [], "no script errors");
if (!process.exitCode)
  console.log(
    "PASS VM screen by touch: keyboard field, Android keys, key bar, trackpad, direct, large",
  );
