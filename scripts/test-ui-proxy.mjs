// Settings › HTTPS names for apps, laid out for an overview (owner request
// of 29 Oct 2026): apps and targets first, one compact line each with the
// first address, "+N more" for the rest and a ⋯ menu for the actions; what
// holds for all names below; explanations folded. Desktop and 390 px.
// Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5186 node scripts/test-ui-proxy.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5186";
const out = process.env.UI_OUTPUT || "out/validation/ui-proxy";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const route = (o) => ({ scheme: "http", urls: [], enabled: true, localOnly: true, ...o });
function proxy() {
  return {
    domain: "home.example.org",
    suffixes: ["titan-test.local", "home.example.org"],
    mdns: true,
    defaultLocalName: "titan-test",
    localName: "",
    off: false,
    tailnet: { available: true, keySet: true },
    apps: [
      route({
        app: "immich", name: "fotos", aliases: ["bilder"], port: 2283, domains: ["fotos.example.org"], tailscale: true, tailscaleName: "immich",
        urls: ["https://fotos.titan-test.local", "https://fotos.home.example.org", "https://fotos.example.org", "https://bilder.titan-test.local"],
        tailnet: { state: "running", url: "https://immich.tail1234.ts.net" },
      }),
      route({ app: "jellyfin", name: "jellyfin", port: 8096, short: true, urls: ["https://jellyfin.titan-test.local", "https://jellyfin.home.example.org", "https://jellyfin.local"] }),
      route({ app: "nextcloud", name: "cloud", port: 8443, scheme: "https", localOnly: false, urls: ["https://cloud.titan-test.local", "https://cloud.home.example.org"] }),
      route({ app: "paperless-ngx", name: "paperless", port: 8000, enabled: false, urls: ["https://paperless.titan-test.local"] }),
    ],
    targets: [route({ app: "ha", name: "ha", host: "192.168.1.30", port: 8123, urls: ["https://ha.titan-test.local", "https://ha.home.example.org"] })],
  };
}
const overflow = (page) => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);

async function open(width) {
  const made = await create("owner", "graphit", width);
  const data = proxy();
  const calls = [];
  await made.ctx.route("**/api/v1/docker/proxy**", async (r) => {
    const req = r.request();
    calls.push([req.method(), new URL(req.url()).pathname]);
    if (req.method() === "DELETE") data.apps = data.apps.filter((a) => !req.url().endsWith(`/apps/${a.app}`));
    return r.fulfill({ json: data });
  });
  await made.page.goto(`${base}/settings/proxy`);
  await made.page.locator("section[aria-labelledby=proxy-apps] .route").first().waitFor();
  return { ...made, calls };
}

try {
  {
    const { ctx, page, calls } = await open(1440);
    const apps = page.locator("section[aria-labelledby=proxy-apps]");
    // Apps come before the settings for all names.
    const order = await page.locator(".tile").evaluateAll((tiles) => tiles.map((t) => t.getAttribute("aria-labelledby") || t.querySelector("[id]")?.id));
    assert.ok(order.indexOf("proxy-apps") < order.indexOf("proxy-general"), `order ${order}`);
    // One address per app, the rest behind "+N more".
    const immich = apps.locator(".route").first();
    assert.equal(await immich.locator(".urls a").count(), 1);
    await immich.getByRole("button", { name: "Alle Adressen von immich" }).click();
    assert.equal(await immich.locator(".details a").count(), 4, "three more names and the tailnet address");
    assert.match(await immich.locator(".details").innerText(), /127\.0\.0\.1:2283/);
    // Chips only for what is out of the ordinary.
    assert.equal(await apps.locator(".chip", { hasText: /^An$/ }).count(), 0);
    assert.equal(await apps.locator(".chip", { hasText: "Nur Heimnetz" }).count(), 0);
    assert.equal(await apps.locator(".chip.warn", { hasText: "Auch aus dem Internet" }).count(), 1);
    assert.equal(await apps.locator(".route.off .chip", { hasText: "Aus" }).count(), 1);
    // The actions are in one menu per line.
    assert.equal(await apps.getByRole("button", { name: /^HTTPS-Name entfernen$/ }).count(), 0);
    await apps.getByRole("button", { name: "Aktionen für jellyfin" }).click();
    assert.deepEqual(await page.getByRole("menuitem").allInnerTexts(), ["Bearbeiten", "Im App-Editor bearbeiten", "HTTPS-Name entfernen"]);
    assert.equal(await page.getByRole("menuitem", { name: "Im App-Editor bearbeiten" }).getAttribute("href"), "/docker/apps/jellyfin#h-proxy");
    await apps.screenshot({ path: `${out}/apps-open.png`, animations: "disabled" });
    await page.getByRole("menuitem", { name: "HTTPS-Name entfernen" }).click();
    const dialog = page.locator("dialog[open]");
    await dialog.getByRole("button", { name: "HTTPS-Name entfernen" }).click();
    await dialog.waitFor({ state: "detached" });
    assert.ok(calls.some(([m, p]) => m === "DELETE" && p.endsWith("/docker/proxy/apps/jellyfin")));
    assert.equal(await apps.getByText("jellyfin", { exact: true }).count(), 0);
    // Explanations stay folded until asked for.
    assert.equal(await page.locator("details.tile").evaluate((d) => d.open), false);
    assert.equal(await page.locator("form[aria-labelledby=proxy-tailnet] details").evaluate((d) => d.open), false);
    // The main switch sits in the head of the page.
    assert.ok(await page.locator(".page-head").getByRole("checkbox", { name: "HTTPS-Namen eingeschaltet" }).isChecked());
    await page.screenshot({ path: `${out}/desktop.png`, fullPage: true, animations: "disabled" });
    await ctx.close();
  }
  {
    const { ctx, page } = await open(390);
    assert.ok((await overflow(page)) <= 0, "no sideways scroll at 390 px");
    const first = page.locator("section[aria-labelledby=proxy-apps] .route").first();
    const box = await first.boundingBox();
    assert.ok(box && box.height < 110, `one app takes ${box?.height} px`);
    await page.screenshot({ path: `${out}/mobile.png`, fullPage: true, animations: "disabled" });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log("ui proxy ok:", out);
} finally {
  await browser.close();
}
