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

async function run() {
  const { ctx, page, data } = await fixtureFactory(browser, errors)(
    "owner",
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
          user: { name: "alex", role: "owner", twoFactor: false },
          hostname,
        },
      }),
    );
    await ctx.route("**/api/v1/cluster/nodes", (r) =>
      mode === "missing"
        ? r.fulfill({ status: 503, json: { error: { code: "unavailable" } } })
        : r.fulfill({
            json: {
              nodes: mode === "cluster" ? [local, peer] : [],
              manages: mode === "cluster",
              version: data.system.version,
            },
          }),
    );
    // On the peer, exercise its real API prefix but use only local fixture data.
    await ctx.route("**/api/v1/cluster/nodes/n2/api/**", (r) => {
      const url = new URL(r.request().url());
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
      assert.equal((await trigger.innerText()).trim(), name);
      assert.ok((await trigger.getAttribute("aria-label"))?.includes(name));
    }
    async function choose(name) {
      await trigger.click();
      const item = menu.getByRole("menuitemradio", { name: new RegExp(name) });
      await Promise.all([page.waitForEvent("domcontentloaded"), item.click()]);
    }
    await page.goto(base);
    await trigger.waitFor();
    await trigger.click();
    await menu
      .getByRole("menuitemradio", { name: /Nordlicht-Keller/, checked: true })
      .waitFor();
    await screenshot(page, "01-lokal-menue");
    await trigger.click();
    await screenshot(page, "00-lokaler-name");
    assert.equal(
      (await trigger.innerText()).trim(),
      local.name,
      "visible local NAS name",
    );
    await choose(peer.name);
    await displayed(peer.name);
    assert.equal(
      await page.evaluate(() => sessionStorage.getItem("titanos.cluster.node")),
      peer.id,
    );
    await page.reload();
    await displayed(peer.name);
    await screenshot(page, "02-zweiter-knoten");
    await choose(local.name);
    await displayed(local.name);
    assert.equal(
      await page.evaluate(() => sessionStorage.getItem("titanos.cluster.node")),
      null,
    );
    await page.reload();
    await displayed(local.name);
    await screenshot(page, "03-zurueck-zum-ersten");
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
    await reopened.close();
    for (const standalone of ["single", "missing"]) {
      mode = standalone;
      await page.reload();
      await displayed(hostname);
      await trigger.click();
      await menu.waitFor();
      assert.equal(await menu.getByRole("menuitemradio").count(), 0);
      await screenshot(
        page,
        standalone === "single" ? "04-einzelgeraet" : "05-ohne-knotenliste",
      );
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
  await run();
} finally {
  await browser.close();
}
