// 1.11, T-Kalender and Kontakte: the page "Am iPhone einrichten"
// (/calendar/iphone, TECHNIK 4.2, draft 108 pictures 20/21/60/61) with
// isolated data for GET/POST/DELETE /account/devices and GET
// /account/devices/setup (page.route). Desktop 1440 px (the page alone and
// in its window on the desktop), the page as a window shows it at 420 and
// 800 px, the phone at 390 px; light and dark. On every view: no sideways
// scroll, no cut text. A new device password is shown once – not in the
// browser's storage, gone after a reload –, with a second factor the code
// is asked; "Entziehen …" asks in a dialog with the danger button left of
// "Abbrechen", and Enter never revokes. The servers stand in the order
// Let's Encrypt, Tailscale (recommended, as text), home network.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5211 node scripts/test-ui-iphone-setup.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5211";
const out = process.env.UI_OUTPUT || "out/validation/iphone-setup";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const PATH = "/calendar/iphone";
const PASSWORD = "k7qm-2xrp-9tfl-c4wn-hb3s-m8vd-q2ze";

/** Isolated answers of the device passwords and the setup. */
function deviceState({ certificate = "valid" } = {}) {
  const now = Date.now();
  return {
    devices: [
      {
        id: "d_1",
        name: "iPhone von Alex",
        created: "2026-11-02T09:00:00Z",
        lastSync: new Date(now - 10 * 60 * 1000).toISOString(),
        lastKind: "caldav",
      },
      {
        id: "d_2",
        name: "Mac im Arbeitszimmer mit einem sehr langen Namen für den Umbruch",
        created: "2026-10-20T09:00:00Z",
        lastSync: "2026-11-12T18:30:00Z",
        lastKind: "carddav",
        lastSyncBeforeRestart: true,
      },
      {
        id: "d_3",
        name: "iPad",
        created: "2026-11-15T09:00:00Z",
        lastSync: null,
        lastKind: "",
      },
    ],
    setup: {
      user: "alex",
      port: 443,
      addresses: [
        {
          kind: "domain",
          server: "nas.example.org",
          caldav: "https://nas.example.org/caldav/alex/",
          carddav: "https://nas.example.org/carddav/alex/",
          certificate: "valid",
          hints: [],
        },
        {
          kind: "tailscale",
          server: "titan.tail1a2b3.ts.net",
          caldav: "https://titan.tail1a2b3.ts.net/caldav/alex/",
          carddav: "https://titan.tail1a2b3.ts.net/carddav/alex/",
          certificate,
          recommended: true,
          hints:
            certificate === "valid"
              ? ["tailscaleApp"]
              : ["tailscaleApp", "tailscaleCertificate", "selfSignedOnce"],
        },
        {
          kind: "lan",
          server: "192.168.1.20",
          caldav: "https://192.168.1.20/caldav/alex/",
          carddav: "https://192.168.1.20/carddav/alex/",
          certificate: "selfSigned",
          hints: ["homeOnly", "selfSignedOnce"],
        },
      ],
    },
    setupError: false,
    requests: [],
  };
}

/** A context on the page. `phone`: the phone's frame; `desk`: the real desktop. */
async function open({
  width = 1440,
  theme = "hell",
  role = "user",
  lang = "de",
  phone = false,
  desk = false,
  twoFactor = false,
  change = () => {},
} = {}) {
  const made = await create(
    role,
    theme,
    width,
    lang,
    phone
      ? {
          isMobile: true,
          hasTouch: true,
          deviceScaleFactor: 3,
          viewport: { width, height: 844 },
        }
      : { viewport: { width, height: 1000 } },
  );
  if (phone || desk)
    await made.ctx.addInitScript(() =>
      localStorage.removeItem("titanos.layout"),
    );
  const state = deviceState();
  change(state);
  // Registered after the fixtures' catch-all: tried first.
  await made.ctx.route(
    /\/api\/v1\/account\/devices(\/[^?]*)?(\?.*)?$/,
    (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname.slice("/api/v1".length);
      const method = req.method();
      state.requests.push([method, path, req.postData()]);
      if (path === "/account/devices/setup" && method === "GET") {
        if (state.setupError)
          return route.fulfill({
            status: 503,
            json: { error: { code: "config_unavailable", message: "x" } },
          });
        return route.fulfill({ json: state.setup });
      }
      if (path === "/account/devices" && method === "GET")
        return route.fulfill({ json: { devices: state.devices, max: 20 } });
      if (path === "/account/devices" && method === "POST") {
        const body = req.postDataJSON();
        if (
          body.password !== "richtig" ||
          (twoFactor && body.code !== "123456")
        )
          return route.fulfill({
            status: 403,
            json: { error: { code: "wrong_password", message: "x" } },
          });
        const created = {
          id: "d_new",
          name: body.name,
          user: "alex",
          password: PASSWORD,
        };
        state.devices.push({
          id: "d_new",
          name: body.name,
          created: new Date().toISOString(),
          lastSync: null,
          lastKind: "",
        });
        return route.fulfill({
          json: created,
          headers: { "Cache-Control": "no-store" },
        });
      }
      const id = path.match(/^\/account\/devices\/([^/]+)$/)?.[1];
      if (id && method === "DELETE") {
        state.devices = state.devices.filter((d) => d.id !== id);
        return route.fulfill({ status: 204, body: "" });
      }
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found", message: "x" } },
      });
    },
  );
  if (twoFactor)
    await made.ctx.route("**/api/v1/session", (route) =>
      route.fulfill({
        json: {
          configAvailable: true,
          setupRequired: false,
          user: { name: "alex", role, twoFactor: true },
          hostname: "titan-test",
        },
      }),
    );
  return { ...made, state };
}

/** No sideways scroll and no cut text. */
async function sound(target, name) {
  const overflow = await target.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `${name}: scrolls sideways by ${overflow}px`);
  assert.deepEqual(await target.evaluate(cutTextIn), [], `${name}: cut text`);
}
const shot = (page, name, fullPage = true) =>
  page.screenshot({
    path: `${out}/${name}.png`,
    fullPage,
    animations: "disabled",
  });
async function until(fn, what, ms = 8000) {
  const end = Date.now() + ms;
  while (!(await fn())) {
    if (Date.now() > end) throw new Error(`timed out: ${what}`);
    await new Promise((r) => setTimeout(r, 100));
  }
}
/** The page is there with its devices and servers. */
async function ready(target) {
  await target.getByText("iPhone von Alex").first().waitFor();
  await target.getByText("192.168.1.20").first().waitFor();
}
/** The texts of the server rows, in order. */
const serverValues = (target) =>
  target.locator("#details .srow code").allInnerTexts();
const stored = (page) =>
  page.evaluate(
    () =>
      JSON.stringify({ ...localStorage }) +
      JSON.stringify({ ...sessionStorage }),
  );

try {
  // ---- Desktop 1440, the page alone: order, wording, create once ----
  for (const theme of ["hell", "graphit"]) {
    const { page, state, ctx } = await open({ theme });
    await page.goto(base + PATH);
    await ready(page);
    const values = await serverValues(page);
    assert.deepEqual(
      values.slice(0, 4),
      ["nas.example.org", "titan.tail1a2b3.ts.net", "192.168.1.20", "alex"],
      "order Let's Encrypt, Tailscale, home network, then the user",
    );
    // "empfohlen" is a word, not a color; it stands at the Tailscale row.
    const recommended = page
      .locator("#details .srow", {
        hasText: "titan.tail1a2b3.ts.net",
      })
      .first();
    assert.match(await recommended.innerText(), /empfohlen/);
    assert.match(await recommended.innerText(), /Tailscale-App/);
    const lan = page
      .locator("#details .srow", { hasText: "192.168.1.20" })
      .first();
    assert.match(await lan.innerText(), /Nur zu Hause/);
    assert.match(await lan.innerText(), /Fortfahren/);
    // Last sync as TECHNIK 3.5 words it.
    const devices = await page.locator("#devices").innerText();
    assert.match(devices, /zuletzt abgeglichen heute \d\d:\d\d \(Kalender\)/);
    assert.match(devices, /zuletzt abgeglichen vor dem Neustart: 12\.11\./);
    assert.match(devices, /noch nie abgeglichen/);
    // Full addresses with the account and the path of the steps.
    const details = await page.locator("#details").innerText();
    assert.match(
      details,
      /https:\/\/titan\.tail1a2b3\.ts\.net\/caldav\/alex\//,
    );
    assert.match(details, /Port 443/);
    const steps = await page.locator("#steps").innerText();
    assert.match(
      steps,
      /Einstellungen › Apps › Kalender › Kalender-Accounts › Account hinzufügen › Andere › CalDAV-Account hinzufügen/,
    );
    assert.match(steps, /Nicht über die Einstellungen von Mail/);
    await sound(page, `desktop ${theme}`);
    await shot(page, `desktop-${theme}`);

    // A new device: the name, then the account password in a dialog.
    await page.locator("#dev-name").fill("iPad von Alex");
    await page.getByRole("button", { name: "Erzeugen …" }).click();
    const dialog = page.locator("dialog[open]");
    await dialog.waitFor();
    assert.equal(
      await dialog.locator('input[autocomplete="one-time-code"]').count(),
      0,
      "no code without 2FA",
    );
    await dialog.locator('input[type="password"]').fill("falsch");
    await dialog.getByRole("button", { name: "Erzeugen", exact: true }).click();
    await dialog.getByText("Das aktuelle Passwort ist falsch.").waitFor();
    await dialog.locator('input[type="password"]').fill("richtig");
    await dialog.getByRole("button", { name: "Erzeugen", exact: true }).click();
    const once = page.locator(".shown");
    await once.waitFor();
    assert.equal(
      (await once.locator("code").innerText()).replace(/\s/g, ""),
      PASSWORD,
      "password shown once",
    );
    assert.match(await once.innerText(), /Nur jetzt sichtbar/);
    const post = state.requests.filter(([m]) => m === "POST").at(-1);
    assert.deepEqual(JSON.parse(post[2]), {
      name: "iPad von Alex",
      password: "richtig",
      code: "",
    });
    assert.ok(
      !(await stored(page)).includes(PASSWORD.slice(0, 9)),
      "password in the browser's storage",
    );
    await sound(page, `desktop ${theme} with the new password`);
    await shot(page, `desktop-${theme}-password`, false);
    await page.reload();
    await ready(page);
    assert.equal(
      await page.locator(".shown").count(),
      0,
      "password after a reload",
    );
    assert.ok(
      !(await page.content()).includes(PASSWORD.slice(0, 9)),
      "password in the page after a reload",
    );
    assert.ok(
      !(await stored(page)).includes(PASSWORD.slice(0, 9)),
      "password in storage after a reload",
    );
    await ctx.close();
  }

  // ---- "Entziehen …": a dialog, danger left, Enter never revokes ----
  {
    const { page, state, ctx } = await open({ theme: "graphit" });
    await page.goto(base + PATH);
    await ready(page);
    await page
      .locator("#devices .srow", { hasText: "iPhone von Alex" })
      .getByRole("button", { name: "Entziehen …" })
      .click();
    const dialog = page.locator("dialog[open]");
    await dialog.waitFor();
    assert.match(
      await dialog.innerText(),
      /„iPhone von Alex“ kann sich danach nicht mehr anmelden/,
    );
    const danger = dialog.locator(".btn-danger");
    const cancel = dialog.getByRole("button", { name: "Abbrechen" });
    const [d, c] = [await danger.boundingBox(), await cancel.boundingBox()];
    assert.ok(d.x < c.x, "danger button left of Abbrechen");
    assert.ok(
      !(await danger.evaluate((el) => el === document.activeElement)),
      "danger button has no focus",
    );
    await sound(page, "revoke dialog");
    await shot(page, "revoke-dialog", false);
    await page.keyboard.press("Enter");
    await page.waitForTimeout(400);
    assert.equal(
      state.requests.filter(([m]) => m === "DELETE").length,
      0,
      "Enter revoked",
    );
    if (await dialog.count()) await cancel.click();
    await page
      .locator("#devices .srow", { hasText: "iPhone von Alex" })
      .getByRole("button", { name: "Entziehen …" })
      .click();
    await page.locator("dialog[open] .btn-danger").click();
    await until(
      async () =>
        (await page
          .locator("#devices .srow", { hasText: "iPhone von Alex" })
          .count()) === 0,
      "device gone",
    );
    assert.deepEqual(
      state.requests.filter(([m]) => m === "DELETE").map(([, p]) => p),
      ["/account/devices/d_1"],
    );
    await ctx.close();
  }

  // ---- Second factor: the code is asked; no certificate: the link ----
  {
    const { page, state, ctx } = await open({
      role: "owner",
      twoFactor: true,
      change: (s) =>
        (s.setup = deviceState({ certificate: "selfSigned" }).setup),
    });
    await page.goto(base + PATH);
    await ready(page);
    const ts = page
      .locator("#details .srow", {
        hasText: "titan.tail1a2b3.ts.net",
      })
      .first();
    assert.equal(
      await ts
        .getByRole("link", { name: "Zertifikat von Tailscale einschalten" })
        .getAttribute("href"),
      "/settings/network?tab=remote#remote-tailscale",
    );
    await page.locator("#dev-name").fill("iPad");
    await page.locator("#dev-name").press("Enter");
    const dialog = page.locator("dialog[open]");
    await dialog.waitFor();
    const submit = dialog.getByRole("button", {
      name: "Erzeugen",
      exact: true,
    });
    await dialog.locator('input[type="password"]').fill("richtig");
    assert.ok(await submit.isDisabled(), "no code, no request");
    await dialog.locator('input[autocomplete="one-time-code"]').fill("123456");
    await submit.click();
    await page.locator(".shown").waitFor();
    assert.equal(
      JSON.parse(state.requests.filter(([m]) => m === "POST").at(-1)[2]).code,
      "123456",
    );
    await sound(page, "2FA");
    await ctx.close();
  }
  {
    // A plain account gets the words, not a link it cannot follow.
    const { page, ctx } = await open({
      change: (s) =>
        (s.setup = deviceState({ certificate: "selfSigned" }).setup),
    });
    await page.goto(base + PATH);
    await ready(page);
    const ts = page
      .locator("#details .srow", {
        hasText: "titan.tail1a2b3.ts.net",
      })
      .first();
    assert.equal(await ts.getByRole("link").count(), 0);
    assert.match(await ts.innerText(), /Ein Administrator kann es/);
    await ctx.close();
  }

  // ---- An error of the API is shown by its text ----
  {
    const { page, ctx } = await open({ change: (s) => (s.setupError = true) });
    await page.goto(base + PATH);
    await page.getByText("iPhone von Alex").first().waitFor();
    await page
      .getByText("Der Stick mit der Konfiguration ist nicht erreichbar.")
      .waitFor();
    await ctx.close();
  }

  // ---- The page as a window shows it: 420 and 800 px; English ----
  for (const [width, theme, lang] of [
    [420, "hell", "de"],
    [800, "graphit", "de"],
    [800, "hell", "en"],
  ]) {
    const { page, ctx } = await open({ width, theme, lang });
    await page.goto(base + PATH);
    await page.getByText("iPhone von Alex").first().waitFor();
    await page.getByText("192.168.1.20").first().waitFor();
    if (lang === "en")
      assert.match(
        await page.locator("#steps").innerText(),
        /Add CalDAV Account/,
      );
    await sound(page, `window ${width} ${theme} ${lang}`);
    await shot(page, `window-${width}-${theme}-${lang}`);
    await ctx.close();
  }

  // ---- In its window on the real desktop (program "Mein Konto" until step 11) ----
  {
    const { page, ctx } = await open({ desk: true });
    await page.goto(base + PATH);
    await page.locator("section.win").first().waitFor();
    let frame = null;
    await until(() => {
      frame = page.frames().find((f) => {
        try {
          return f !== page.mainFrame() && new URL(f.url()).pathname === PATH;
        } catch {
          return false;
        }
      });
      return !!frame;
    }, "window of the page");
    await ready(frame);
    await sound(frame, "desktop window");
    await sound(page, "desktop");
    await shot(page, "desktop-window", false);
    await ctx.close();
  }

  // ---- Phone 390: light and dark, 44 px targets ----
  for (const theme of ["graphit", "hell"]) {
    const { page, ctx } = await open({ width: 390, theme, phone: true });
    await page.goto(base + PATH);
    await ready(page);
    const small = await page.evaluate(() =>
      [...document.querySelectorAll(".page button, .page input, .page a")]
        .filter((el) => el.getClientRects().length)
        .map((el) => [
          el.textContent.trim() || el.getAttribute("aria-label") || el.id,
          el.getBoundingClientRect(),
        ])
        .filter(([, r]) => r.height < 44 || (r.width < 44 && r.width > 0))
        .map(([n, r]) => `${n} ${Math.round(r.width)}x${Math.round(r.height)}`),
    );
    assert.deepEqual(small, [], `phone ${theme}: targets under 44 px`);
    await sound(page, `phone ${theme}`);
    await shot(page, `phone-${theme}`);
    // The new password at 390 px breaks between its groups only.
    await page.locator("#dev-name").fill("iPad von Alex");
    await page.getByRole("button", { name: "Erzeugen …" }).click();
    await page.locator("dialog[open] input[type=password]").fill("richtig");
    await page
      .locator("dialog[open]")
      .getByRole("button", { name: "Erzeugen", exact: true })
      .click();
    await page.locator(".shown").waitFor();
    await sound(page, `phone ${theme} with the new password`);
    await page.locator(".shown").scrollIntoViewIfNeeded();
    await shot(page, `phone-${theme}-password`, false);
    await ctx.close();
  }

  assert.deepEqual(errors, [], "script errors on the page");
  console.log(`iPhone setup: all checks passed, screenshots in ${out}`);
} finally {
  await browser.close();
}
