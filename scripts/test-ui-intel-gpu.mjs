// Settings › Hardware: GuC/HuC loading of Intel graphics. A Kaby Lake on
// i915 with HuC off, choosing "Load HuC", the plain confirmation before GuC
// submission and the restart offer. All NAS endpoints are isolated
// fixtures; no request reaches a NAS.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs UI_URL=http://127.0.0.1:5189 node scripts/test-ui-intel-gpu.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5189";
const out = process.env.UI_OUTPUT || "out/validation/intel-gpu";
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

const fw = (file, extra = {}) => ({
  file,
  present: true,
  loaded: "",
  version: "",
  state: "",
  authenticated: false,
  ...extra,
});
const status = () => ({
  mode: "auto",
  managed: false,
  active: "",
  param: "-1",
  entries: { a: "" },
  deferred: false,
  restartNeeded: false,
  writable: true,
  problem: "",
  gpus: [
    {
      address: "0000:00:02.0",
      device: "5912",
      revision: 4,
      platform: "kabylake",
      name: "Kaby Lake",
      generation: "9.5",
      driver: "i915",
      renderNode: "renderD128",
      card: "card0",
      applies: true,
      submission: false,
      submissionUsed: false,
      guc: fw("i915/kbl_guc_70.1.1.bin", { state: "disabled" }),
      huc: fw("i915/kbl_huc_4.0.0.bin", { state: "disabled" }),
      source: "debugfs",
      messages: [],
    },
  ],
});

async function intel(ctx) {
  const state = { status: status(), puts: [] };
  await ctx.route("**/api/v1/hardware/intel-gpu", async (route) => {
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON();
      state.puts.push(body);
      state.status = {
        ...state.status,
        mode: body.mode,
        managed: true,
        entries: { a: { auto: "", huc: "2", guc: "3" }[body.mode] },
        restartNeeded: body.mode !== "auto",
      };
    }
    await route.fulfill({ json: state.status });
  });
  return state;
}

try {
  for (const [lang, width] of [
    ["de", 1440],
    ["en", 390],
  ]) {
    const { ctx, page } = await context("owner", "graphit", width, lang);
    const state = await intel(ctx);
    await page.goto(`${base}/settings/hardware`);
    const de = lang === "de";
    await page
      .getByRole("heading", { name: de ? "Intel-Grafik: GuC/HuC" : "Intel graphics: GuC/HuC" })
      .waitFor();
    await page.getByText("Kaby Lake (Gen9.5)").waitFor();
    await page.getByText(de ? "HuC nicht geladen" : "HuC not loaded").waitFor();
    const save = page.getByRole("button", { name: de ? "Speichern" : "Save" }).last();
    assert.ok(await save.isDisabled());

    await page.getByRole("radio", { name: de ? /HuC laden/ : /Load HuC/ }).check();
    await save.click();
    await page.getByText(de ? "Gespeichert, wirksam nach Neustart" : "Saved, effective after a restart").waitFor();
    assert.deepEqual(state.puts.at(-1), { mode: "huc" });
    await page.getByRole("button", { name: de ? "Jetzt neu starten …" : "Restart now …" }).waitFor();

    // GuC submission: a warning and a plain confirmation, nothing typed.
    await page.getByRole("radio", { name: de ? /GuC-Submission und HuC/ : /GuC submission and HuC/ }).check();
    await page.getByText(de ? "Gen9/Gen9.5 kennt keine GuC-Submission" : "Gen9/Gen9.5 has no GuC submission", { exact: false }).waitFor();
    await save.click();
    const dialog = page.getByRole("dialog");
    await dialog.waitFor();
    assert.equal(await dialog.locator("input").count(), 0);
    await page.screenshot({ path: `${out}/confirm-${lang}-${width}.png`, fullPage: true });
    await dialog.getByRole("button", { name: de ? "Einschalten" : "Turn on" }).click();
    await page.getByText(de ? "Gespeichert, wirksam nach Neustart" : "Saved, effective after a restart").waitFor();
    assert.deepEqual(state.puts.at(-1), { mode: "guc" });

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
    // Firmware file names wrap only after "/" or "_", never inside a word.
    const badBreaks = await page.locator(".mono.file").evaluateAll((spans) =>
      spans.flatMap((span) => {
        const chars = [];
        const walker = document.createTreeWalker(span, NodeFilter.SHOW_TEXT);
        for (let node = walker.nextNode(); node; node = walker.nextNode())
          for (let i = 0; i < node.data.length; i++) {
            const range = document.createRange();
            range.setStart(node, i);
            range.setEnd(node, i + 1);
            chars.push([node.data[i], range.getBoundingClientRect().top]);
          }
        return chars.flatMap(([, top], i) =>
          i && top > chars[i - 1][1] + 2 && !"/_".includes(chars[i - 1][0]) ? [span.textContent] : [],
        );
      }),
    );
    assert.equal((await page.locator(".mono.file").count()) > 0, true);
    assert.deepEqual(badBreaks, [], "firmware names broken mid-word");
    // From the top, so the sticky header is not drawn in the middle.
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.screenshot({ path: `${out}/hardware-${lang}-${width}.png`, fullPage: true });
    await ctx.close();
  }
  assert.deepEqual(errors, []);
  console.log(`ok – screenshots in ${out}`);
} finally {
  await browser.close();
}
