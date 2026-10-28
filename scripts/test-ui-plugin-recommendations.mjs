// Plugin page: hardware recommendations, catalog areas, search and details.
// All NAS endpoints are isolated fixtures; no request reaches a NAS.
// UI_URL=http://127.0.0.1:5186 PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node scripts/test-ui-plugin-recommendations.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5186";
const out = process.env.UI_OUTPUT || "out/validation/ui-plugin-recommendations";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

const card = { key: "pci:8088:1001", bus: "pci", address: "0000:03:00.0", name: "Beijing Wangxun Technology Co., Ltd. Ethernet Controller RP1000 for 10GbE SFP+" };
const plugin = (name, title, category, extra = {}) => ({
  name,
  title,
  description: { de: `${title} Beschreibung`, en: `${title} description` },
  version: "0.31.0",
  size: 176128,
  kernel: "6.12.113+deb13-amd64",
  contains: null,
  modules: [],
  services: [],
  installed: false,
  enabled: false,
  active: false,
  path: "",
  official: true,
  local: false,
  download: true,
  problem: "",
  category,
  ...extra,
});
const wangxun = plugin("wangxun-nic", "Wangxun-Netzwerkkarten", "network", {
  modules: ["ngbe", "txgbe"],
  license: "GPL-2.0-only",
  upstream: { name: "Linux (Debian linux-source)", url: "https://git.kernel.org/", version: "6.12.113 (Debian 6.12.113-1)" },
  sources: ["https://snapshot.debian.org/package/linux/6.12.113-1/", "wangxun-nic-sources.tar.gz"],
  details: {
    de: {
      when: "Wenn eine Wangxun-Karte eingebaut ist.",
      hardware: ["txgbe: Wangxun SP1000 und WX1820"],
      changes: "Lädt ngbe und txgbe.",
      limits: ["Auf echter Hardware nicht geprüft."],
      footprint: "Etwa 130 KiB.",
    },
  },
  devices: [card],
});
const status = {
  volume: "Data",
  kernel: "6.12.113+deb13-amd64",
  plugins: [
    wangxun,
    plugin("it87-extended", "IT87 Sensoren (erweitert)", "sensors"),
    plugin("tailscale", "Tailscale", "remote", { installed: true, enabled: true, active: true, download: false }),
  ],
  catalog: { release: "0.31.0", state: "ready" },
  driverUpdate: null,
  recommendations: [{ plugin: "wangxun-nic", title: "Wangxun-Netzwerkkarten", category: "network", devices: [card] }],
  dismissed: 0,
};

try {
  const { ctx, page, data } = await context();
  data.plugins = status;
  await page.goto(base + "/plugins");
  const section = page.getByRole("region", { name: "Für deine Hardware empfohlen" });
  await section.getByText(card.name).waitFor();
  await page.getByRole("heading", { name: "Netzwerkkarten", exact: true }).waitFor();
  await page.getByRole("heading", { name: "Sensoren und Mainboard" }).waitFor();
  await page.getByText("Passt zu deiner Hardware").waitFor();
  await page.screenshot({ path: `${out}/recommended.png`, fullPage: true });

  // Recommending opens the plugin and its install form; nothing is installed.
  await section.getByRole("button", { name: "Ansehen und installieren" }).click();
  await page.getByText("Wenn eine Wangxun-Karte eingebaut ist.").waitFor();
  await page.getByText("Auf echter Hardware nicht geprüft.").waitFor();
  await page.getByRole("button", { name: "Erlauben und installieren" }).waitFor();
  assert.equal(await page.getByText("0000:03:00.0").count() >= 2, true, "device shown in section and details");
  await page.screenshot({ path: `${out}/details.png`, fullPage: true });
  await page.getByRole("button", { name: "Abbrechen", exact: true }).click();

  // Search and area filter.
  await page.getByRole("searchbox", { name: "Plugins durchsuchen" }).fill("tailscale");
  await page.getByRole("button", { name: /Wangxun-Netzwerkkarten Offiziell/ }).waitFor({ state: "detached" });
  await page.getByRole("searchbox", { name: "Plugins durchsuchen" }).fill("WX1820");
  await page.getByRole("button", { name: /Wangxun-Netzwerkkarten Offiziell/ }).waitFor();
  await page.getByRole("searchbox", { name: "Plugins durchsuchen" }).fill("");
  await page.getByRole("combobox", { name: "Alle Bereiche" }).selectOption("sensors");
  assert.equal(await page.getByRole("heading", { name: "Netzwerkkarten", exact: true }).count(), 0);
  await page.getByRole("combobox", { name: "Alle Bereiche" }).selectOption("");

  // Dismissing hides the suggestion and offers to show it again.
  await section.getByRole("button", { name: "Nicht mehr vorschlagen" }).click();
  await section.waitFor({ state: "detached" });
  await page.getByRole("button", { name: "Ausgeblendete Vorschläge wieder anzeigen (1)" }).click();
  await page.getByRole("region", { name: "Für deine Hardware empfohlen" }).waitFor();
  assert.deepEqual(data.recommendationCalls, [
    ["POST", "/plugins/recommendations/wangxun-nic/dismiss"],
    ["DELETE", "/plugins/recommendations/dismissed"],
  ]);
  assert.equal(data.pluginInstalls, undefined);
  await ctx.close();
  assert.deepEqual(errors, []);
  console.log("plugin recommendations: ok");
} finally {
  await browser.close();
}
