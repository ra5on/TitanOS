// Plugins › NVIDIA › driver version against isolated fixtures: every
// version of the last check, a build in progress, a failed build.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-nvidia-choice.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5173";
const out = process.env.UI_OUTPUT || "out/validation/nvidia-choice";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

const nvidia = {
  name: "nvidia",
  title: "NVIDIA-Treiber",
  description: {
    de: "Treiber und Container Toolkit für NVIDIA-Grafikkarten",
    en: "NVIDIA driver",
  },
  version: "615.71.09-1",
  size: 120e6,
  kernel: "6.12.115+deb13-amd64",
  contains: null,
  modules: ["nvidia"],
  services: [],
  installed: true,
  enabled: true,
  active: true,
  path: "",
  official: true,
  local: false,
  download: false,
  problem: "",
  update: null,
};
const driver = (extra) => ({
  state: "sourceReady",
  automatic: true,
  canRestore: false,
  versions: [],
  chosen: "",
  bundled: "615.71.09",
  running: "615.71.09",
  runningLocal: false,
  drivers: ["620.12.03", "615.71.09", "590.44.01", "580.105.08", "535.274.02"],
  ...extra,
});
const plugins = (driverUpdate) => ({
  volume: "Data",
  kernel: "6.12.115+deb13-amd64",
  plugins: [nvidia],
  catalog: { release: "0.47.0", state: "ready" },
  driverUpdate,
});

async function open(
  name,
  driverUpdate,
  { width = 1440, theme = "graphit", pick } = {},
) {
  const { ctx, page, data } = await context("owner", theme, width);
  data.plugins = plugins(driverUpdate);
  const posts = [];
  await page.route("**/api/v1/plugins/nvidia/updates", (route) => {
    posts.push(route.request().postDataJSON());
    data.plugins = plugins(
      driver({
        state: "downloading",
        chosen: posts.at(-1).version,
        target: posts.at(-1).version,
      }),
    );
    return route.fulfill({ json: data.plugins });
  });
  await page.goto(base + "/plugins");
  const select = page.getByLabel("Version", { exact: true });
  await select.waitFor();
  if (pick) await select.selectOption(pick);
  await page.screenshot({
    path: `${out}/${name}.png`,
    fullPage: true,
    animations: "disabled",
  });
  return { ctx, page, select, posts };
}

try {
  {
    const { ctx, page, select, posts } = await open(
      "auswahl-graphit-1440",
      driver(),
      { pick: "620.12.03" },
    );
    assert.ok(
      await page
        .getByText("Läuft: 615.71.09, mit TitanOS ausgeliefert")
        .isVisible(),
    );
    assert.ok(
      await page
        .getByText("baut den Treiber selbst", { exact: false })
        .isVisible(),
    );
    await select.selectOption("535.274.02");
    assert.ok(
      await page
        .getByText("Ältere Treiberreihen", { exact: false })
        .isVisible(),
    );
    await select.selectOption("620.12.03");
    await page
      .getByRole("button", {
        name: "Bauen und beim nächsten Neustart verwenden",
      })
      .click();
    await page
      .getByText("Quelldateien von 620.12.03 werden geladen", { exact: false })
      .waitFor();
    assert.deepEqual(posts, [{ action: "choose", version: "620.12.03" }]);
    await ctx.close();
  }
  {
    const { ctx, page } = await open(
      "baut-graphit-1440",
      driver({ state: "building", chosen: "620.12.03", target: "620.12.03" }),
    );
    assert.ok(
      await page
        .getByText("620.12.03 wird für den laufenden Kernel gebaut", {
          exact: false,
        })
        .isVisible(),
    );
    assert.ok(
      await page
        .getByRole("button", {
          name: "Bauen und beim nächsten Neustart verwenden",
        })
        .isDisabled(),
    );
    await ctx.close();
  }
  {
    const failure =
      "nvidia/nv-mmap.c:412:5: error: implicit declaration of function 'vm_flags_set'";
    const { ctx, page, posts } = await open(
      "fehlgeschlagen-graphit-1440",
      driver({
        state: "error",
        chosen: "535.274.02",
        target: "535.274.02",
        error: failure,
      }),
    );
    assert.ok(
      await page
        .getByText(
          "Der Bau von 535.274.02 ist fehlgeschlagen. Es läuft der ausgelieferte Treiber 615.71.09.",
        )
        .isVisible(),
    );
    // Trying again is possible, also with the same choice.
    await page
      .getByRole("button", {
        name: "Bauen und beim nächsten Neustart verwenden",
      })
      .click();
    await page
      .getByText("Quelldateien von 535.274.02", { exact: false })
      .waitFor();
    assert.deepEqual(posts, [{ action: "choose", version: "535.274.02" }]);
    await ctx.close();
  }
  {
    // Built on this NAS, waiting for the restart: only cancelling.
    const { ctx, page } = await open(
      "wartet-graphit-1440",
      driver({
        state: "ready",
        chosen: "620.12.03",
        pending: { version: "620.12.03-titanos-0.47.0-local" },
      }),
    );
    assert.ok(
      await page
        .getByRole("button", { name: "Auswahl zurücknehmen" })
        .isVisible(),
    );
    await ctx.close();
  }
  {
    const { ctx, page } = await open("auswahl-hell-390", driver(), {
      width: 390,
      theme: "hell",
      pick: "620.12.03",
    });
    const overflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    );
    assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
    await ctx.close();
  }
  {
    // An older build environment lists no versions: the previous controls.
    const { ctx, page, data } = await context("owner");
    data.plugins = plugins(driver({ drivers: [] }));
    await page.goto(base + "/plugins");
    await page.getByText("Gewünschte Version").waitFor();
    assert.equal(await page.locator(".driver-choice").count(), 0);
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log(`ok – screenshots in ${out}`);
} finally {
  await browser.close();
}
