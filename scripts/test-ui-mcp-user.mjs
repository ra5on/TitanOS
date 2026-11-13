// Settings › MCP server for every account (PLAN 19) and the help "?" of
// PLAN 10b. Isolated API data only: no request reaches a NAS.
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
const base = process.env.UI_URL || "http://127.0.0.1:5193";
const out = process.env.UI_OUTPUT || "out/validation/cd/mcp-hilfe";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const time = "2026-11-03T10:00:00Z";
const tool = (name, area, kind, dangerous = false) => ({
  name,
  area,
  scope: kind === "read" ? "system:read" : "system:write",
  kind,
  dangerous,
  mode: "on",
});

// The fixture of one account: the MCP status as titand answers it for the
// role (tools only for owners and administrators) plus tokens and activity.
async function fixture(role, theme = "graphit", width = 1440) {
  const f = await create(role, theme, width);
  const admin = role === "owner" || role === "admin";
  const seen = { puts: 0, activity: 0, tokens: [] };
  await f.ctx.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.slice(7),
      method = route.request().method();
    const json = (value) => route.fulfill({ json: value });
    if (path === "/system/mcp") {
      if (method === "PUT") {
        seen.puts++;
        if (!admin)
          return route.fulfill({
            status: 403,
            json: { error: { code: "forbidden", message: "fixture" } },
          });
        return route.fulfill({ status: 204 });
      }
      return json({
        enabled: true,
        path: "/mcp",
        protocolVersions: ["2025-06-18", "2025-03-26"],
        scope: "mcp",
        tools: admin
          ? [
              tool("system_status", "system", "read"),
              tool("volume_action", "storage", "change", true),
            ]
          : [],
      });
    }
    if (path === "/account/tokens") {
      if (method === "POST") {
        const req = route.request().postDataJSON();
        seen.tokens.push(req);
        return json({ secret: "titanos_fixture_example_only" });
      }
      return json({
        tokens: [],
        scopes: [
          "system:read",
          "system:write",
          "files:read",
          "files:write",
          "docker:read",
          "docker:write",
          "vms:read",
          "vms:write",
          "mcp",
        ],
      });
    }
    if (path === "/activity" && route.request().url().includes("via=mcp")) {
      seen.activity++;
      if (!admin)
        return route.fulfill({
          status: 403,
          json: { error: { code: "forbidden", message: "fixture" } },
        });
      return json({
        entries: [{ at: time, target: "system_status", ok: true, status: 200 }],
      });
    }
    return route.fallback();
  });
  return { ...f, seen };
}

async function noOverflow(page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - innerWidth,
  );
  assert.ok(overflow <= 1, `horizontal scroll ${overflow}`);
}

// Opens the "?" behind a term and checks the explanation appears.
async function help(page, name, text) {
  const button = page.getByRole("button", { name: `Was bedeutet „${name}“?` });
  await button.first().click();
  const note = page.getByRole("note");
  await note.waitFor();
  assert.match(await note.textContent(), text);
  // Let the panel finish fading in before a screenshot.
  await page.waitForTimeout(300);
  return async () => {
    await page.keyboard.press("Escape");
    await note.waitFor({ state: "detached" });
  };
}

try {
  // A user: the page opens and shows only reading and the token button.
  for (const [theme, width] of [
    ["graphit", 1440],
    ["graphit", 390],
    ["hell", 390],
  ]) {
    const { ctx, page, seen } = await fixture("user", theme, width);
    await page.goto(base + "/settings/mcp");
    await page.getByRole("heading", { name: "Status" }).waitFor();
    assert.match(
      await page
        .locator("section", { hasText: "Status" })
        .first()
        .textContent(),
      /Bereit/,
    );
    assert.equal(
      await page.getByLabel("MCP-Server für dieses NAS einschalten").count(),
      0,
    );
    assert.equal(
      await page.getByRole("heading", { name: "Werkzeuge" }).count(),
      0,
    );
    assert.equal(
      await page.getByRole("heading", { name: "Letzte Aufrufe" }).count(),
      0,
    );
    assert.equal(await page.getByText("Aufrufe heute").count(), 0);
    assert.equal(seen.activity, 0, "a user does not ask the activity log");
    assert.equal(await page.locator("input.mono").inputValue(), base + "/mcp");
    for (const tab of ["Claude Code", "mcp-remote", "titanosctl"]) {
      await page.getByRole("tab", { name: new RegExp(tab) }).click();
      await page.locator("pre.snippet").waitFor();
    }
    await page.getByText("titanosctl liest das Token").waitFor();
    assert.equal(seen.puts, 0);
    await noOverflow(page);
    await page.screenshot({
      path: `${out}/mcp-user-${width}-${theme}.png`,
      fullPage: true,
    });

    // The token button leads to API und CLI with the MCP scope ticked.
    await page.getByRole("link", { name: "Token für MCP erstellen" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.waitFor();
    assert.equal(
      await dialog.getByLabel("KI-Clients per MCP").isChecked(),
      true,
    );
    // Users only get scopes their role may have.
    assert.equal(await dialog.getByLabel(/Container/).count(), 0);
    const close = await help(page, "Token-Bereiche", /Token/);
    await page.screenshot({
      path: `${out}/api-scopes-help-${width}-${theme}.png`,
    });
    await close();
    await dialog.getByRole("button", { name: "Abbrechen" }).click();

    // API und CLI: no MCP block any more, only the way to the page.
    const block = page.locator("#api-mcp");
    await block.waitFor();
    assert.equal(
      await page.getByLabel("MCP-Server für dieses NAS einschalten").count(),
      0,
    );
    // PLAN 78-1: the block is one plain row with the link to the page;
    // nothing of the old MCP block is left, and it has no button at all.
    assert.equal(await block.locator("pre, code, button").count(), 0);
    assert.equal(
      await block.evaluate((el) => el.classList.contains("srow")),
      true,
    );
    assert.equal(
      await block.getByRole("link", { name: "MCP-Server" }).count(),
      1,
    );
    await noOverflow(page);
    await page.screenshot({
      path: `${out}/api-user-${width}-${theme}.png`,
      fullPage: true,
    });

    // The settings search finds MCP as the page, no longer a section.
    // PLAN 71: the search sits in the side bar of the Systemsteuerung window;
    // only "MCP-Server" is left (not "API und CLI" with its old section), and
    // it opens the page.
    if (width === 1440) {
      const desk = await fixture("user", theme, width);
      await desk.ctx.addInitScript(() =>
        localStorage.removeItem("titanos.layout"),
      );
      await desk.page.goto(base + "/settings/about");
      const side = desk.page.locator("section.win nav.side");
      const search = side.getByRole("searchbox", {
        name: "Einstellung suchen",
      });
      await search.waitFor({ timeout: 15000 });
      await search.fill("mcp");
      // PLAN 86: the page first, then single settings of pages a user may
      // open – none of an admin page.
      await side.locator(".items .item.result").first().waitFor();
      const found = await side
        .locator(".items .item.result")
        .evaluateAll((a) =>
          a.map((x) => [
            x.querySelector(".two > span").textContent.trim(),
            x.querySelector(".two > small").textContent.trim(),
          ]),
        );
      assert.deepEqual(found[0], ["MCP-Server", "Smart Home und KI"]);
      for (const [, where] of found)
        assert.match(where, /^(Smart Home und KI|MCP-Server|API und CLI)/);
      await side.locator(".items .item.result").first().click();
      await desk.page.waitForFunction(
        () => {
          const loc =
            document.querySelector("section.win iframe")?.contentWindow
              ?.location;
          return loc?.pathname === "/settings/mcp" && !loc.hash;
        },
        null,
        { timeout: 8000 },
      );
      await desk.ctx.close();
    }
    await ctx.close();
  }

  // An owner: everything as before.
  for (const [theme, width] of [
    ["graphit", 1440],
    ["graphit", 390],
  ]) {
    const { ctx, page, seen } = await fixture("owner", theme, width);
    await page.goto(base + "/settings/mcp");
    await page.getByLabel("MCP-Server für dieses NAS einschalten").waitFor();
    await page.getByRole("heading", { name: "Werkzeuge" }).waitFor();
    await page.getByRole("heading", { name: "Letzte Aufrufe" }).waitFor();
    await page.getByText("Aufrufe heute").waitFor();
    assert.ok(seen.activity > 0);
    await noOverflow(page);
    await page.screenshot({
      path: `${out}/mcp-owner-${width}-${theme}.png`,
      fullPage: true,
    });
    await page.goto(base + "/settings/api");
    await page.locator("#api-mcp").waitFor();
    assert.equal(
      await page.getByLabel("MCP-Server für dieses NAS einschalten").count(),
      0,
    );
    assert.equal(await page.locator("#api-mcp pre").count(), 0);
    await ctx.close();
  }

  // Help "?" at the new places (PLAN 10b).
  {
    const { ctx, page } = await fixture("owner", "graphit", 1440);
    await page.goto(base + "/storage");
    await page.getByRole("button", { name: "Neues Volume" }).first().click();
    const dialog = page.getByRole("dialog");
    await dialog.waitFor();
    await dialog
      .getByRole("radio", { name: /RAID 1/ })
      .first()
      .click();
    await dialog.getByText("Write-Intent-Bitmap").first().waitFor();
    const close = await help(page, "Write-Intent-Bitmap", /Platte/);
    await page.screenshot({ path: `${out}/help-bitmap-1440-graphit.png` });
    await close();
    await dialog
      .getByRole("radio", { name: /RAID 0/ })
      .first()
      .click();
    assert.equal(
      await dialog
        .getByRole("button", { name: "Was bedeutet „Write-Intent-Bitmap“?" })
        .count(),
      0,
      "RAID 0 has no bitmap",
    );
    await ctx.close();
  }
  for (const [theme, width] of [
    ["graphit", 1440],
    ["hell", 390],
  ]) {
    const { ctx, page } = await fixture("owner", theme, width);
    const net = (id, name, parent, address, management = false, vlan = 0) => ({
      id,
      name,
      parent,
      address,
      management,
      vlan,
      purpose: management ? "management" : "apps",
      bridge: true,
      ipv4: "static",
      gateway: management ? "192.168.1.1" : "",
      ipv6: "auto",
      address6: "",
      gateway6: "",
      dns: [],
      routes: [],
    });
    const config = {
      hostname: "titan-test",
      ports: ["enp1s0"],
      bondMode: "",
      vlan: 0,
      bridge: true,
      dhcp: true,
      address: "",
      gateway: "",
      dns: [],
      bonds: [],
      vlans: true,
      networks: [net("admin", "Verwaltung", "enp1s0", "192.168.1.20/24", true)],
    };
    const network = {
      config,
      pending: null,
      topology: config,
      pendingTopology: null,
      pendingSeconds: 0,
      interfaces: [
        {
          name: "enp1s0",
          mac: "52:54:00:00:00:01",
          carrier: true,
          speedMbps: 2500,
        },
      ],
      device: "br0",
      addresses: ["192.168.1.20/24"],
      gateway: "192.168.1.1",
      dns: ["192.168.1.1"],
      configured: true,
      bridgeReady: true,
    };
    await ctx.route("**/api/v1/network", (route) =>
      route.fulfill({
        json: { network, bondModes: ["active-backup", "802.3ad"] },
      }),
    );
    await page.goto(base + "/settings/network");
    await page.getByRole("button", { name: "Netz hinzufügen" }).first().click();
    const editor = page.getByRole("dialog");
    await editor
      .getByText("Bridge für VMs und Container bereitstellen")
      .waitFor();
    let close = await help(page, "Bridge", /br0/);
    await page.screenshot({ path: `${out}/help-bridge-${width}-${theme}.png` });
    await close();
    // The VLAN-ID field of the editor has its "?" as well.
    await editor
      .getByRole("button", { name: "Was bedeutet „VLAN“?" })
      .waitFor();
    await editor.getByRole("button", { name: "Abbrechen" }).click();
    await editor.waitFor({ state: "detached" });
    await page.getByRole("tab", { name: "Schnittstellen" }).click();
    await page.getByRole("heading", { name: /VLANs/ }).waitFor();
    close = await help(page, "VLAN", /VLAN-ID/);
    await page.screenshot({ path: `${out}/help-vlan-${width}-${theme}.png` });
    await close();
    await noOverflow(page);
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("MCP user and help UI checks passed");
} finally {
  await browser.close();
}
