// Verify the name painted on the phone's NAS switcher through actual node changes.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { fixtureFactory } from "./test-ui-fixtures.mjs";

const base = process.env.UI_URL || "http://127.0.0.1:5276";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const engine = process.env.UI_ENGINE || "webkit";
const driver = (await import(process.env.PLAYWRIGHT_MODULE || "playwright"))[
  engine
];
const browser = await driver.launch({
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const local = {
  id: "n1",
  name: "Nordlicht-Keller",
  self: true,
  online: true,
  compatible: true,
};
const peer = {
  id: "n2",
  name: "Nordlicht-Dach",
  self: false,
  online: true,
  compatible: true,
};
const hostname = "werkstatt-nas";
const peerHost = "dach-nas";

async function screenshot(page, name) {
  const dir = process.env.UI_OUTPUT;
  if (!dir) return;
  await mkdir(dir, { recursive: true });
  await page.screenshot({ path: join(dir, engine + "-" + name + ".png") });
}

async function run(asHomeApp, role) {
  const { ctx, page, data } = await fixtureFactory(browser, errors)(
    role,
    "graphit",
    390,
    "de",
    {
      nativeShell: true,
      isMobile: true,
      hasTouch: true,
      reducedMotion: "reduce",
      serviceWorkers: "block",
      viewport: { width: 390, height: 844 },
    },
  );
  try {
    if (asHomeApp)
      await ctx.addInitScript(() => {
        Object.defineProperty(navigator, "standalone", { get: () => true });
        const original = window.matchMedia.bind(window);
        window.matchMedia = (query) =>
          query.includes("display-mode: standalone")
            ? Object.assign(original("(min-width: 0px)"), {
                displayMode: "standalone",
              })
            : original(query);
      });
    const label = [role, asHomeApp ? "homescreen" : "browser"].join("-");
    const peerRequests = [];
    data.system.hostname = hostname;
    data.prefs.desktop = {
      version: 1,
      wallpaper: { source: "builtin", id: "nordlicht", live: false },
    };
    let mode = "cluster";
    await ctx.route("**/api/v1/session", (r) =>
      r.fulfill({
        json: {
          configAvailable: true,
          setupRequired: false,
          user: { name: "alex", role, twoFactor: false },
          hostname,
        },
      }),
    );
    await ctx.route("**/api/v1/cluster/nodes", (r) =>
      mode === "missing"
        ? r.fulfill({ status: 503, json: { error: { code: "unavailable" } } })
        : r.fulfill({
            json: {
              nodes: ["cluster", "member"].includes(mode)
                ? [local, peer]
                : mode === "offline"
                  ? [local, { ...peer, online: false }]
                  : [],
              manages: ["cluster", "offline"].includes(mode),
              version: data.system.version,
            },
          }),
    );
    // On the peer, exercise its real API prefix but use only local fixture data.
    await ctx.route("**/api/v1/cluster/nodes/n2/api/**", (r) => {
      const url = new URL(r.request().url());
      peerRequests.push(url.pathname);
      url.pathname = url.pathname.replace("/cluster/nodes/n2/api", "");
      if (url.pathname === "/api/v1/system")
        return r.fulfill({ json: { ...data.system, hostname: peerHost } });
      return r.fallback({ url: url.toString() });
    });
    const trigger = page.locator("[data-nord-home] h1 button.trigger");
    const menu = page.locator("[data-nord-home] h1 [role=menu]");
    async function displayed(name) {
      // A matching aria-label alone does not prove the visible heading is correct.
      await trigger.getByText(name, { exact: true }).waitFor();
      await page.waitForLoadState("networkidle");
      assert.equal(
        (await trigger.locator(".current-name").innerText()).trim(),
        name,
      );
      assert.equal(
        (await trigger.locator(".switch-label").innerText()).trim(),
        "NAS wechseln",
      );
      assert.ok((await trigger.getAttribute("aria-label"))?.includes(name));
    }
    async function choose(name) {
      await trigger.click();
      const item = menu.getByRole("menuitemradio", { name: new RegExp(name) });
      await Promise.all([page.waitForEvent("domcontentloaded"), item.click()]);
    }
    await page.goto(base);
    await page.waitForLoadState("networkidle");
    await trigger.waitFor();
    await trigger.click();
    await menu
      .getByRole("menuitemradio", { name: /Nordlicht-Keller/, checked: true })
      .waitFor();
    assert.equal(
      await menu.getByRole("menuitem", { name: "Cluster verwalten" }).count(),
      role === "user" ? 0 : 1,
    );
    await screenshot(page, label + "-01-lokal-menue");
    await trigger.click();
    await screenshot(page, label + "-00-lokaler-name");
    await displayed(local.name);
    await choose(peer.name);
    await displayed(peer.name);
    assert.equal(
      await page.evaluate(() => sessionStorage.getItem("titanos.cluster.node")),
      peer.id,
    );
    await page.reload();
    await displayed(peer.name);
    await screenshot(page, label + "-02-zweiter-knoten");
    // Follow two regular app routes with the remote node selected.
    for (const app of ["files", ...(role === "owner" ? ["docker"] : [])]) {
      const sent = page.waitForRequest((req) =>
        new URL(req.url()).pathname.startsWith(
          "/api/v1/cluster/nodes/n2/api/" + app + "/",
        ),
      );
      await page.goto(new URL("/" + app, base).href);
      await sent;
      await page.waitForLoadState("networkidle");
    }
    assert.ok(peerRequests.some((path) => path.includes("/api/files/")));
    await page.goto(new URL("/?phone=pages", base).href);
    const grid = page.locator(".node-menu.phone");
    await grid.getByText("NAS wechseln", { exact: true }).waitFor();
    await grid.getByText(peer.name, { exact: true }).first().waitFor();
    await grid.getByRole("button").first().click();
    await grid
      .getByRole("menuitemradio", {
        name: new RegExp(peer.name),
        checked: true,
      })
      .waitFor();
    await screenshot(page, label + "-app-raster");
    await Promise.all([
      page.waitForEvent("domcontentloaded"),
      grid.getByRole("menuitemradio", { name: new RegExp(local.name) }).click(),
    ]);
    await grid.getByText(local.name, { exact: true }).first().waitFor();
    await page.waitForLoadState("networkidle");
    await page.goto(base, { waitUntil: "networkidle" });
    await displayed(local.name);
    assert.equal(
      await page.evaluate(() => sessionStorage.getItem("titanos.cluster.node")),
      null,
    );
    await page.reload();
    await displayed(local.name);
    await screenshot(page, label + "-03-zurueck-zum-ersten");
    const reopened = await ctx.newPage();
    await reopened.goto(base);
    await reopened
      .locator("[data-nord-home] h1 button.trigger")
      .getByText(local.name, { exact: true })
      .waitFor();
    assert.equal(
      await reopened.evaluate(() =>
        sessionStorage.getItem("titanos.cluster.node"),
      ),
      null,
    );
    await reopened.waitForLoadState("networkidle");
    await reopened.close();
    mode = "member";
    await page.reload({ waitUntil: "networkidle" });
    await displayed(local.name);
    await trigger.click();
    assert.equal(await menu.getByRole("menuitemradio").count(), 1);
    assert.equal(
      await menu
        .getByRole("menuitemradio", { name: new RegExp(peer.name) })
        .count(),
      0,
    );
    mode = "offline";
    await page.reload();
    await displayed(local.name);
    await trigger.click();
    assert.ok(
      await menu
        .getByRole("menuitemradio", { name: new RegExp(peer.name) })
        .isDisabled(),
    );
    for (const standalone of ["single", "missing"]) {
      mode = standalone;
      await page.reload();
      await page.waitForLoadState("networkidle");
      await displayed(hostname);
      await trigger.click();
      await menu.waitFor();
      await page.waitForLoadState("networkidle");
      assert.equal(await menu.getByRole("menuitemradio").count(), 0);
      await screenshot(page, label + "-" + standalone);
    }
    assert.deepEqual(errors, []);
    console.log(
      engine +
        ": local cluster name, switch to peer, reload, switch back, single NAS and unavailable list passed",
    );
  } finally {
    await ctx.close();
  }
}

try {
  await run(false, "owner");
  await run(true, "owner");
  await run(false, "user");
} finally {
  await browser.close();
}
