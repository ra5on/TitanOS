// Build with: cd backend && go build -tags embedui -o /tmp/titand ./cmd/titand
// TITAND_BIN=/tmp/titand PLAYWRIGHT_MODULE=<local playwright> node scripts/test-ui-personal-agent-live.mjs
// Real development daemon, real worker and browser launcher. No intercepted network or system data.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { createServer } from "node:net";
import { join } from "node:path";
import { randomBytes } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
const { chromium, request } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const binary = process.env.TITAND_BIN;
assert.ok(binary, "TITAND_BIN must point to the built development titand");
assert.notEqual(
  process.getuid?.(),
  0,
  "the development worker requires an unprivileged account",
);
const root = await mkdtemp(join(tmpdir(), "titanos-agent-ui-"));
const socket = createServer();
socket.listen(0, "127.0.0.1");
await once(socket, "listening");
const address = "127.0.0.1:" + socket.address().port;
await new Promise((resolve) => socket.close(resolve));
const origin = "http://" + address;
const opts = { baseURL: origin, extraHTTPHeaders: { Origin: origin } };
const daemon = spawn(
  binary,
  ["-dev", "-config", root, "-dev-listen", address],
  { stdio: "ignore" },
);
const owner = await request.newContext(opts),
  member = await request.newContext(opts);
const secrets = new Map(
  ["reviewer", "visitor"].map((name) => [
    name,
    randomBytes(28).toString("hex"),
  ]),
);
let browser;
async function call(api, method, path, data) {
  const res = await api[method]("/api/v1" + path, { data });
  assert.ok(res.ok(), method + " " + path + " returned " + res.status());
  return (await res.text()).trim() ? res.json() : null;
}
async function showBrowser() {
  browser = await chromium.launch({
    executablePath: process.env.BROWSER_EXECUTABLE,
  });
  const ctx = await browser.newContext({
    storageState: await owner.storageState(),
    viewport: { width: 1440, height: 950 },
    locale: "de-DE",
    reducedMotion: "reduce",
    serviceWorkers: "block",
  });
  await ctx.addInitScript(() => localStorage.setItem("titanos.locale", "de"));
  const page = await ctx.newPage();
  page.setDefaultTimeout(12000);
  return page;
}
async function launcher(page, fresh = false) {
  await page.goto(origin + "/");
  if (fresh) {
    const offer = page
      .locator("dialog[open]")
      .filter({ hasText: "Office-Programme direkt im Browser" });
    await offer
      .getByRole("button", { name: "Nicht mehr fragen", exact: true })
      .click();
    await offer.waitFor({ state: "hidden" });
    const notice = page.getByRole("dialog", { name: "Haftungsausschluss" });
    await notice.getByRole("checkbox").check();
    await notice.getByRole("button", { name: "Weiter" }).click();
    await notice.waitFor({ state: "hidden" });
  }
  try {
    await page.locator(".topbar [data-menu-button]").click();
  } catch (e) {
    console.error(
      "Launcher context",
      page.url(),
      await page.locator("dialog[open] h2").allTextContents(),
    );
    throw e;
  }
  const item = page.locator('.launcher [data-key="app:agent"] .prog');
  try {
    await item.click();
  } catch (e) {
    console.error(
      "Menu",
      await page
        .locator(".launcher [data-key]")
        .evaluateAll((nodes) => nodes.map((n) => n.dataset.key)),
    );
    throw e;
  }
  const app = page.frameLocator('iframe[data-app="app:agent"]').last();
  await app.locator(".pa-shell").waitFor();
  return app;
}
try {
  for (let i = 0; i < 80; i++) {
    if (daemon.exitCode !== null)
      throw Error("dev titand exited: " + daemon.exitCode);
    try {
      if ((await owner.get("/api/v1/session")).ok()) break;
    } catch {}
    await delay(125);
  }
  await call(owner, "post", "/setup", {
    name: "reviewer",
    password: secrets.get("reviewer"),
  });
  await call(owner, "post", "/login", {
    name: "reviewer",
    password: secrets.get("reviewer"),
  });
  await call(owner, "post", "/users", {
    name: "visitor",
    password: secrets.get("visitor"),
    role: "user",
  });
  await call(member, "post", "/login", {
    name: "visitor",
    password: secrets.get("visitor"),
  });
  assert.equal((await call(owner, "get", "/services/state")).agent, true);
  await call(owner, "put", "/personal-agent/settings", { paused: true });
  const page = await showBrowser();
  const app = await launcher(page, true);
  const createdResponse = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === "/api/v1/personal-agent/jobs" &&
      r.request().method() === "POST",
  );
  await app.locator(".pa-example").click();
  const created = await (await createdResponse).json();
  assert.ok(created.id);
  await browser.close();
  browser = null;
  await call(owner, "put", "/personal-agent/settings", { paused: false });
  let result;
  for (let i = 0; i < 120; i++) {
    result = await call(owner, "get", "/personal-agent/jobs/" + created.id);
    if (result.status === "done" || result.status === "failed") break;
    await delay(200);
  }
  assert.equal(result.status, "done", result.error);
  assert.equal(result.result.text, "2 + 3 = 5");
  assert.ok(result.audit?.length, "the worker recorded its transitions");
  const other = await call(member, "get", "/personal-agent/state");
  assert.ok(!other.jobs?.some((j) => j.id === created.id));
  const denied = await member.get("/api/v1/personal-agent/jobs/" + created.id);
  assert.equal(denied.status(), 404);
  const reopened = await showBrowser();
  const again = await launcher(reopened);
  await again.locator('[data-guide="jobs-tab"]:visible').click();
  await again.locator(".pa-row").first().click();
  await again
    .locator(".pa-detail .pa-readable", { hasText: "2 + 3 = 5" })
    .waitFor();
  assert.equal(
    (await call(owner, "get", "/personal-agent/state")).jobs.filter(
      (j) => j.request.requestId === created.request.requestId,
    ).length,
    1,
  );
  if (process.env.UI_OUTPUT) {
    await mkdir(process.env.UI_OUTPUT, { recursive: true });
    await again.locator(".pa-shell").screenshot({
      path: join(process.env.UI_OUTPUT, "echter-worker-beleg.png"),
    });
  }
  console.log(
    "PASS: desktop launcher → real job → browser closed → unprivileged worker → receipt after reopening; second account sees no job.",
  );
} finally {
  await browser?.close();
  await owner.dispose();
  await member.dispose();
  if (daemon.exitCode === null) {
    const exit = once(daemon, "exit");
    daemon.kill("SIGTERM");
    await exit;
  }
  await rm(root, { recursive: true, force: true });
}
