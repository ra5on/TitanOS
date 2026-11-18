// 1.11 (TECHNIK 7.5, duty of PLAN.md, lesson of T-Mail 1.05–1.08): the
// opt-in programs show where the real answer of the NAS says so – against a
// running `titand -dev` with a throwaway -config, never against fixtures,
// and without any page.route stand-in for /api/v1/services/state (the
// script refuses to install one and asserts the answers came from titand).
//
// Signed in as a plain account (made by the owner through the API):
// T-Kalender and Kontakte stand in the menu, in the dock after pinning and
// on the phone's home grid (390 px); opening shows the page, and since
// -dev has no data volume the page says "nicht verfügbar", never an empty
// list. The same check for T-Mail with and without its release and for the
// KI-Agent switched on for the account and off again.
//
// titand -dev with the web UI embedded (see BAU-API "Schritt 11/12"):
//   (cd web && npm run build)
//   (cd backend && CGO_ENABLED=0 go build -tags embedui -o $DIR/titand ./cmd/titand)
//   setsid nohup systemd-run --user --scope -q -p MemoryMax=1G $DIR/titand -dev \
//     -config $DIR/config -dev-listen 127.0.0.1:8411 >$DIR/titand.log 2>&1 &
//   PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:8411 \
//     node scripts/test-ui-pim-visibility.mjs
// A fresh config is set up with PIM_OWNER (default alex:<password below>);
// on a config set up before, PIM_OWNER must name its owner.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn } from "./test-ui-fixtures.mjs";
const pw = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const { chromium, request } = pw;

const base = process.env.UI_URL || "http://127.0.0.1:8411";
const out = process.env.UI_OUTPUT || "out/validation/pim-visibility";
await mkdir(out, { recursive: true });
const PASSWORD = "a long test password";
const [ownerName, ownerPassword] = (
  process.env.PIM_OWNER || `alex:${PASSWORD}`
).split(":");
const USER = process.env.PIM_USER || "bea";

// ---- The owner, through the API of titand ----
const owner = await request.newContext({ baseURL: base });
async function call(ctx, method, path, data) {
  const res = await ctx.fetch("/api/v1" + path, {
    method,
    data,
    headers: data ? { "Content-Type": "application/json" } : {},
  });
  const text = await res.text();
  return { status: res.status(), body: text ? JSON.parse(text) : null };
}
{
  const session = (await call(owner, "GET", "/session")).body;
  assert.equal(
    typeof session?.setupRequired,
    "boolean",
    "titand answers /api/v1/session",
  );
  const done = session.setupRequired
    ? await call(owner, "POST", "/setup", {
        name: ownerName,
        password: ownerPassword,
      })
    : await call(owner, "POST", "/login", {
        name: ownerName,
        password: ownerPassword,
      });
  assert.ok(
    done.status < 300,
    `owner: ${done.status} ${JSON.stringify(done.body)}`,
  );
  const made = await call(owner, "POST", "/users", {
    name: USER,
    password: PASSWORD,
    role: "user",
  });
  assert.ok(
    made.status === 201 || made.body?.error?.code === "user_exists",
    `account ${USER}: ${made.status} ${JSON.stringify(made.body)}`,
  );
}
/** T-Mail's release and the KI-Agent for the plain account. */
async function release({ mail, agent }) {
  const m = await call(owner, "PUT", `/users/${USER}/mail`, { enabled: mail });
  assert.equal(m.status, 200, `mail release: ${JSON.stringify(m.body)}`);
  const settings = (await call(owner, "GET", "/agent/settings")).body;
  const a = await call(owner, "PUT", "/agent/settings", {
    ...settings,
    enabled: agent,
    ollama: settings.ollama || "http://127.0.0.1:1",
    model: settings.model || "m",
    users: agent ? [USER] : [],
  });
  assert.equal(a.status, 200, `agent: ${JSON.stringify(a.body)}`);
}

// ---- The browser: no stand-in for /services/state, ever ----
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const routed = [];
const stateAnswers = [];
const forbidden = (pattern) =>
  /services\/state|services\/\*|\*\*$|\/\*\*\/\*$/.test(String(pattern));
async function signedIn({ phone = false } = {}) {
  const ctx = await browser.newContext({
    locale: "de-DE",
    viewport: phone
      ? { width: 390, height: 844 }
      : { width: 1440, height: 900 },
    ...(phone ? { isMobile: true, hasTouch: true, deviceScaleFactor: 3 } : {}),
  });
  // Any route a later edit might add is recorded and refused here.
  for (const target of [ctx]) {
    const original = target.route.bind(target);
    target.route = (pattern, handler) => {
      routed.push(String(pattern));
      assert.ok(!forbidden(pattern), `a route stand-in for ${pattern}`);
      return original(pattern, handler);
    };
  }
  const page = await ctx.newPage();
  page.on("pageerror", (e) => errors.push(String(e)));
  // Answers of titand to the menu's question (also from the windows' frames).
  ctx.on("response", async (res) => {
    if (new URL(res.url()).pathname !== "/api/v1/services/state") return;
    try {
      stateAnswers.push({
        fromServiceWorker: res.fromServiceWorker(),
        status: res.status(),
        body: await res.json(),
      });
    } catch {
      // A frame that went away; the next answer counts.
    }
  });
  await page.goto(base + "/login");
  const status = await page.evaluate(
    async (body) =>
      (
        await fetch("/api/v1/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body,
        })
      ).status,
    JSON.stringify({ name: USER, password: PASSWORD }),
  );
  assert.equal(status, 200, "login of the plain account");
  // The desktop, not the page alone.
  await page.evaluate(() => localStorage.removeItem("titanos.layout"));
  await page.goto(base + "/");
  return { ctx, page };
}
const shot = (page, name) => page.screenshot({ path: `${out}/${name}.png` });
async function until(fn, what, ms = 10000) {
  const end = Date.now() + ms;
  for (;;) {
    if (await fn().catch(() => false)) return;
    if (Date.now() > end) throw new Error(`timed out: ${what}`);
    await new Promise((r) => setTimeout(r, 150));
  }
}
/** The frame of a window that shows this path. */
async function frameAt(page, path) {
  let frame = null;
  await until(() => {
    frame = page.frames().find((f) => {
      try {
        return f !== page.mainFrame() && new URL(f.url()).pathname === path;
      } catch {
        return false;
      }
    });
    return Promise.resolve(!!frame);
  }, `window at ${path}`);
  return frame;
}

const PROGRAMS = {
  calendar: { label: "T-Kalender", path: "/calendar", pim: true },
  contacts: { label: "Kontakte", path: "/contacts", pim: true },
  mail: { label: "T-Mail", path: "/mail" },
  agent: { label: "KI-Agent", path: "/agent" },
};

/** The page of a program as its window shows it. */
async function checkPage(page, frame, id) {
  const p = PROGRAMS[id];
  if (p.pim) {
    // -dev has no data volume: "nicht verfügbar", never an empty list.
    const shell = frame.locator(`[data-pim-shell="${id}"]`);
    await shell.waitFor();
    await frame
      .locator('[data-pim-state="unavailable"]')
      .getByText(/nicht verfügbar/)
      .waitFor();
    await frame.getByText(/Daten-Volume ist nicht eingebunden/).waitFor();
    assert.equal(
      await frame.locator("[data-pim-ready]").count(),
      0,
      `${id}: no list`,
    );
    assert.deepEqual(await frame.evaluate(cutTextIn), [], `${id}: no cut text`);
  } else if (id === "mail") {
    // With the release T-Mail asks for its first mailbox, not "nicht freigegeben".
    await frame.locator("main, body").first().waitFor();
    await until(
      async () => !(await frame.getByText(/nicht freigegeben/i).count()),
      "T-Mail without the 'not released' page",
    );
  } else {
    await frame.locator("body").waitFor();
  }
}

/** Desktop 1440: the menu, opening, pinning to the dock. */
async function desktop(shown, hidden, tag) {
  const { ctx, page } = await signedIn();
  await page.locator("nav.dock").waitFor({ timeout: 20000 });
  const openMenu = async () => {
    await page.locator('[data-opens="launcher"]').first().click();
    await page.locator(".launcher").waitFor();
  };
  await openMenu();
  for (const id of shown)
    await page.locator(`.launcher .cell[data-key="app:${id}"]`).waitFor();
  for (const id of hidden)
    assert.equal(
      await page.locator(`.launcher .cell[data-key="app:${id}"]`).count(),
      0,
      `${id} hidden in the menu (${tag})`,
    );
  await page.waitForTimeout(600); // the menu fades in
  await shot(page, `desktop-menu-${tag}`);
  await page.keyboard.press("Escape");
  for (const id of shown) {
    const p = PROGRAMS[id];
    await openMenu();
    await page.locator(`.launcher .cell[data-key="app:${id}"] .prog`).click();
    const frame = await frameAt(page, p.path);
    // The window bar names the program.
    await until(
      async () =>
        (await page.locator("section.win").last().textContent()).includes(
          p.label,
        ),
      `window bar of ${p.label}`,
    );
    await checkPage(page, frame, id);
    if (PROGRAMS[id].pim) await shot(page, `desktop-window-${id}`);
    // Running: in the dock; pinned through its menu, it stays after closing.
    const slot = page.locator(`nav.dock .slot[data-key="app:${id}"]`);
    await slot.waitFor();
    if ((await slot.getAttribute("data-pinned")) === null) {
      await slot.locator("button.it").click({ button: "right" });
      await page
        .getByRole("menuitem", { name: "Im Dock behalten" })
        .or(page.getByRole("button", { name: "Im Dock behalten" }))
        .first()
        .click();
    }
    await until(
      async () => (await slot.getAttribute("data-pinned")) !== null,
      `${id} pinned`,
    );
    await page.keyboard.press("Escape");
  }
  for (const id of hidden)
    assert.equal(
      await page.locator(`nav.dock .slot[data-key="app:${id}"]`).count(),
      0,
      `${id} not in the dock (${tag})`,
    );
  await shot(page, `desktop-dock-${tag}`);
  // After a reload (the pinning is stored), the pinned programs stay.
  await page.reload();
  await page.locator("nav.dock").waitFor({ timeout: 20000 });
  for (const id of shown)
    await page
      .locator(`nav.dock .slot[data-key="app:${id}"][data-pinned]`)
      .waitFor();
  await ctx.close();
}

/** Phone 390: the home grid, opening from it. */
async function phone(shown, hidden, tag) {
  const { ctx, page } = await signedIn({ phone: true });
  await page.locator(".home .pages").waitFor({ timeout: 20000 });
  const icon = (id) => page.locator(`.home [data-home-key="app:${id}"]`);
  for (const id of shown) await icon(id).first().waitFor();
  for (const id of hidden)
    assert.equal(
      await icon(id).count(),
      0,
      `${id} hidden on the phone (${tag})`,
    );
  assert.ok(
    (await page.evaluate(() => document.documentElement.scrollWidth)) <= 390,
    "nothing wider than the phone",
  );
  await shot(page, `phone-home-${tag}`);
  for (const id of shown.filter((x) => PROGRAMS[x].pim)) {
    const target = icon(id).first();
    // On another page of the grid: swipe there first.
    for (let i = 0; i < 4 && !(await target.isVisible()); i++) {
      await page
        .locator(".home .pages")
        .evaluate((el) => el.scrollBy(el.clientWidth, 0));
      await page.waitForTimeout(400);
    }
    await target.click();
    await until(
      async () => new URL(page.url()).pathname === PROGRAMS[id].path,
      `phone opens ${id}`,
    );
    await checkPage(page, page.mainFrame(), id);
    await page.waitForTimeout(900); // the page slides in
    await shot(page, `phone-${id}`);
    await page.goto(base + "/");
    await page.locator(".home .pages").waitFor({ timeout: 20000 });
  }
  await ctx.close();
}

try {
  // 1. From the start: T-Kalender and Kontakte; T-Mail and the agent not.
  await release({ mail: false, agent: false });
  await desktop(["calendar", "contacts"], ["mail", "agent"], "start");
  await phone(["calendar", "contacts"], ["mail", "agent"], "start");
  // 2. T-Mail released and the agent on for the account.
  await release({ mail: true, agent: true });
  await desktop(["calendar", "contacts", "mail", "agent"], [], "released");
  await phone(["calendar", "contacts", "mail", "agent"], [], "released");
  // 3. Taken back: gone again, T-Kalender and Kontakte stay.
  await release({ mail: false, agent: false });
  await desktop(["calendar", "contacts"], ["mail", "agent"], "taken-back");
} finally {
  await release({ mail: false, agent: false }).catch(() => {});
  await browser.close();
  await owner.dispose();
}

// The answers came from titand, never a stand-in.
assert.deepEqual(routed.filter(forbidden), [], "no route for /services/state");
assert.ok(stateAnswers.length >= 5, `answers seen: ${stateAnswers.length}`);
for (const a of stateAnswers) {
  assert.equal(a.status, 200);
  assert.equal(a.fromServiceWorker, false, "answered by titand");
  assert.equal(a.body.calendar, true, JSON.stringify(a.body));
  assert.equal(a.body.contacts, true, JSON.stringify(a.body));
}
assert.ok(
  stateAnswers.some((a) => a.body.mail === true && a.body.agent === true),
  "mail and agent on with the release",
);
assert.ok(
  stateAnswers.some((a) => a.body.mail === false && a.body.agent === false),
  "mail and agent off without",
);
const relevant = errors.filter(
  (e) => !/Failed to load resource|net::ERR/.test(e),
);
assert.deepEqual(relevant, [], "no script errors");
console.log(
  "PASS T-Kalender, Kontakte, T-Mail and KI-Agent show as titand -dev says (menu, dock, phone; no volume: nicht verfügbar)",
);
