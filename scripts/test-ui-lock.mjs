// Lock screen of the session (PLAN 61): "Sperren" in the account menu and
// Strg/⌘+Alt+L lock, the NAS then answers 423 "locked" to everything but the
// session, unlocking and logout; the lock screen asks for the password (and
// the code with two-factor login) or a passkey, Escape does nothing, other
// shortcuts do not reach the page. A same-origin frame follows the lock and
// shows nothing. The automatic lock after the account's minutes. 1440 and
// 390 px. Isolated API data only: no request reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5232 node scripts/test-ui-lock.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5232";
// Passkeys need a host name, not an IP address.
const named = base.replace("127.0.0.1", "localhost");
const out = process.env.UI_OUTPUT || "out/validation/lock";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);
const PASSWORD = "richtiges Passwort";

// The NAS side of the lock, over the general fixtures.
async function open(width, options = {}) {
  const made = await create("owner", "graphit", width);
  const nas = {
    locked: !!options.locked,
    twoFactor: !!options.twoFactor,
    passkey: !!options.passkey,
    lockMinutes: options.lockMinutes ?? 15,
    calls: [],
    refused: 0,
  };
  await made.ctx.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.slice("/api/v1".length);
    const method = req.method();
    if (path === "/session" && method === "GET")
      return route.fulfill({
        json: {
          configAvailable: true,
          setupRequired: false,
          user: { name: "alex", role: "owner", twoFactor: nas.twoFactor },
          hostname: "titan-test",
          lockMinutes: nas.lockMinutes,
          ...(nas.locked
            ? { locked: true, passkey: nas.passkey }
            : { version: "0.0.0" }),
        },
      });
    if (path === "/session/lock" && method === "POST") {
      nas.calls.push(["lock"]);
      nas.locked = true;
      return route.fulfill({ status: 204, body: "" });
    }
    if (path === "/session/unlock" && method === "POST") {
      const body = req.postDataJSON();
      nas.calls.push(["unlock", body]);
      const deny = (code) =>
        route.fulfill({ status: 403, json: { error: { code, message: code } } });
      if (body.passkey) {
        if (body.passkey.challengeId !== "c-1") return deny("passkey_failed");
      } else {
        if (body.password !== PASSWORD) return deny("wrong_password");
        if (nas.twoFactor && !body.code) return deny("second_factor_required");
        if (nas.twoFactor && body.code !== "123456") return deny("invalid_code");
      }
      nas.locked = false;
      return route.fulfill({ json: { name: "alex", role: "owner", twoFactor: nas.twoFactor } });
    }
    if (path === "/account/passkeys/confirm" && method === "POST")
      return route.fulfill({
        json: {
          challengeId: "c-1",
          publicKey: {
            challenge: "AAAA",
            rpId: "localhost",
            timeout: 60000,
            userVerification: "required",
            allowCredentials: [{ type: "public-key", id: "AAAA" }],
          },
        },
      });
    if (path === "/logout") return route.fallback();
    if (nas.locked) {
      nas.refused++;
      return route.fulfill({
        status: 423,
        json: { error: { code: "locked", message: "locked" } },
      });
    }
    return route.fallback();
  });
  if (options.fakePasskey)
    await made.ctx.addInitScript(() => {
      const buf = () => new Uint8Array([1, 2, 3, 4]).buffer;
      navigator.credentials.get = async () => ({
        rawId: buf(),
        response: {
          clientDataJSON: buf(),
          authenticatorData: buf(),
          signature: buf(),
          userHandle: buf(),
        },
      });
    });
  if (options.clock) await made.page.clock.install();
  await made.page.goto(`${options.base ?? base}/`);
  return { ...made, nas };
}

const screen = (page) => page.locator(".lockscreen:not(.frame)");

// Boxes of the visible parts must not overlap, nor leave the screen.
async function noOverlap(page, width) {
  const boxes = {};
  for (const part of [".clock", ".date", ".avatar", ".name", ".pill", ".actions", ".quiet"]) {
    const el = page.locator(`.lockscreen ${part}`);
    if (await el.count()) boxes[part] = await el.first().boundingBox();
  }
  const names = Object.keys(boxes);
  for (const [i, a] of names.entries()) {
    const box = boxes[a];
    assert.ok(box.x >= 0 && box.x + box.width <= width + 0.5, `${a} inside at ${width}`);
    for (const b of names.slice(i + 1)) {
      const o = boxes[b];
      const apart =
        box.x + box.width <= o.x ||
        o.x + o.width <= box.x ||
        box.y + box.height <= o.y ||
        o.y + o.height <= box.y;
      assert.ok(apart, `${a} overlaps ${b} at ${width}`);
    }
  }
  assert.ok(
    (await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)) <= 0,
    "no sideways scroll",
  );
}

try {
  // 1. Lock from the account menu, wrong and right password.
  {
    const { ctx, page, nas } = await open(1440);
    await page.locator("header .account .trigger").click();
    const item = page.getByRole("menuitem", { name: /Sperren/ });
    await item.waitFor();
    assert.match(await item.textContent(), /Strg\+Alt\+L|⌘/);
    await item.click();
    await screen(page).waitFor();
    assert.deepEqual(nas.calls[0], ["lock"]);
    const field = page.getByLabel("Passwort", { exact: true });
    await page.waitForFunction(() => document.activeElement?.getAttribute("type") === "password");
    assert.equal(await page.locator(".lockscreen .name").textContent(), "alex");
    assert.match(await page.locator(".lockscreen .quiet").textContent(), /Gesperrt nach 15 Min ohne Bedienung/);
    // Escape does nothing, the command palette does not open behind.
    await page.keyboard.press("Escape");
    await page.keyboard.press("Control+k");
    assert.ok(await screen(page).isVisible());
    assert.equal(await page.locator("dialog[open]").count(), 0, "nothing opened behind the lock");
    // The page behind cannot be reached.
    assert.ok(await page.evaluate(() => [...document.body.children].filter((e) => !e.classList.contains("lockscreen")).every((e) => e.inert)));
    await page.screenshot({ path: `${out}/locked-1440.png`, animations: "disabled" });
    await field.fill("falsch");
    await page.keyboard.press("Enter");
    await page.getByRole("alert").filter({ hasText: "Das Passwort stimmt nicht." }).waitFor();
    assert.equal(await field.inputValue(), "", "wrong password cleared");
    await page.screenshot({ path: `${out}/wrong-password-1440.png`, animations: "disabled" });
    await field.fill(PASSWORD);
    await page.getByRole("button", { name: "Entsperren" }).click();
    await screen(page).waitFor({ state: "detached" });
    assert.deepEqual(nas.calls.at(-1), ["unlock", { password: PASSWORD, code: "" }]);
    assert.ok(await page.evaluate(() => [...document.body.children].every((e) => !e.inert)), "page usable again");
    // Strg+Alt+L locks too.
    await page.locator("body").click({ position: { x: 5, y: 500 } });
    await page.keyboard.press("Control+Alt+KeyL");
    await screen(page).waitFor();
    assert.equal(nas.calls.filter(([c]) => c === "lock").length, 2);
    await ctx.close();
  }

  // 2. Loaded while locked, with two-factor login: the code is asked for; a
  //    request of the page meanwhile got 423 and changed nothing.
  {
    const { ctx, page, nas } = await open(1440, { locked: true, twoFactor: true });
    await screen(page).waitFor();
    const code = page.getByLabel("Code aus der Authentifizierungs-App");
    await code.waitFor();
    await page.getByLabel("Passwort", { exact: true }).fill(PASSWORD);
    await code.fill("000000");
    await page.keyboard.press("Enter");
    await page.getByRole("alert").filter({ hasText: /Code stimmt nicht/ }).waitFor();
    assert.equal(await page.getByLabel("Passwort", { exact: true }).inputValue(), PASSWORD, "password kept after a wrong code");
    assert.equal(await code.inputValue(), "", "wrong code cleared");
    assert.ok((await code.boundingBox()).height >= 40, "code field full height");
    await page.screenshot({ path: `${out}/two-factor-1440.png`, animations: "disabled" });
    await code.fill("123456");
    await page.keyboard.press("Enter");
    await screen(page).waitFor({ state: "detached" });
    assert.deepEqual(nas.calls.at(-1), ["unlock", { password: PASSWORD, code: "123456" }]);
    await ctx.close();
  }

  // 3. Passkey (host name, faked authenticator) on the phone width.
  {
    const { ctx, page, nas } = await open(390, { locked: true, passkey: true, fakePasskey: true, base: named });
    await screen(page).waitFor();
    const button = page.getByRole("button", { name: "Mit Passkey entsperren" });
    await button.waitFor();
    await noOverlap(page, 390);
    await page.screenshot({ path: `${out}/passkey-390.png`, animations: "disabled" });
    await button.click();
    await screen(page).waitFor({ state: "detached" });
    assert.equal(nas.calls.at(-1)[1].passkey.challengeId, "c-1");
    await ctx.close();
  }

  // 4. Phone width with the password only, and the 1440 layout checked too.
  for (const width of [390, 1440]) {
    const { ctx, page } = await open(width, { locked: true });
    await screen(page).waitFor();
    await noOverlap(page, width);
    if (width === 390)
      await page.screenshot({ path: `${out}/locked-390.png`, animations: "disabled" });
    await ctx.close();
  }

  // 5. A same-origin frame (a window of the desktop): locking inside it
  //    locks the top window; the frame shows nothing; unlocking frees both.
  {
    const { ctx, page } = await open(1440);
    await page.locator("header .account .trigger").waitFor();
    await page.evaluate((src) => {
      const frame = document.createElement("iframe");
      frame.src = src;
      frame.id = "win";
      frame.style.cssText = "position:fixed;left:40px;top:120px;width:700px;height:500px;z-index:5;border:0";
      document.body.append(frame);
    }, `${base}/`);
    const frame = page.frameLocator("#win");
    await frame.locator("header .account .trigger").waitFor();
    await frame.locator("body").click({ position: { x: 10, y: 300 } });
    await frame.locator("body").press("Control+Alt+KeyL");
    await screen(page).waitFor();
    await frame.locator(".lockscreen.frame").waitFor();
    assert.equal(await frame.locator(".lockscreen.frame input").count(), 0, "no form in the frame");
    await page.getByLabel("Passwort", { exact: true }).fill(PASSWORD);
    await page.keyboard.press("Enter");
    await screen(page).waitFor({ state: "detached" });
    await frame.locator(".lockscreen").waitFor({ state: "detached" });
    await ctx.close();
  }

  // 6. The automatic lock after the account's minutes without use.
  {
    const { ctx, page, nas } = await open(1440, { clock: true, lockMinutes: 5 });
    await page.locator("header .account .trigger").waitFor();
    await page.clock.fastForward("04:00");
    assert.equal(await screen(page).count(), 0, "not yet");
    await page.clock.fastForward("01:30");
    await screen(page).waitFor();
    assert.ok(nas.calls.some(([c]) => c === "lock"));
    assert.match(await page.locator(".lockscreen .quiet").textContent(), /5 Min/);
    await ctx.close();
  }

  assert.deepEqual(errors, []);
  console.log("ui lock ok:", out);
} finally {
  await browser.close();
}
