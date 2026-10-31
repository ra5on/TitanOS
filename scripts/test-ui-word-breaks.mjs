// No word broken in the middle on phones (owner request 10d, 31 Oct 2026):
// opens the main pages at 390 px in German and English, also with the
// largest font size (120 %), and fails when a word of four or more letters
// runs over two lines without a hyphen. Breaks at a written hyphen
// ("Home-Bildschirm") are fine. The headless Chromium of the tests has no
// hyphenation dictionaries, so a word that `hyphens: auto` would split
// correctly on a phone counts as broken here too: labels must fit or stand
// above their value. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5182 node scripts/test-ui-word-breaks.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || "http://127.0.0.1:5182";
const out = process.env.UI_OUTPUT || "out/validation/ui-word-breaks";
await mkdir(out, { recursive: true });
const errors = [],
  create = fixtureFactory(browser, errors);

const pages = [
  "/",
  "/storage",
  "/shares",
  "/files",
  "/users",
  "/docker",
  "/vms",
  "/vms/new",
  "/lxc",
  "/apps",
  "/plugins",
  "/settings",
  "/settings/about",
  "/settings/updates",
  "/settings/network",
  "/settings/appearance",
  "/settings/access",
  "/settings/notifications",
  "/tools",
  "/tools/monitoring",
];

function brokenWords() {
  const found = [];
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  let node;
  while ((node = walker.nextNode())) {
    const el = node.parentElement;
    // Paths, addresses and code may break anywhere.
    if (!el || el.closest("script,style,code,pre,.mono,input,textarea")) continue;
    const style = getComputedStyle(el);
    if (style.visibility === "hidden" || style.display === "none") continue;
    for (const m of node.textContent.matchAll(/[\p{L}\p{N}]{4,}/gu)) {
      const range = document.createRange();
      range.setStart(node, m.index);
      range.setEnd(node, m.index + m[0].length);
      const tops = new Set(
        [...range.getClientRects()].filter((r) => r.width > 0).map((r) => Math.round(r.top)),
      );
      if (tops.size > 1) found.push(`${m[0]} <${el.tagName.toLowerCase()} class="${el.className}">`);
    }
  }
  return found;
}

const problems = [];
for (const locale of ["de", "en"]) {
  for (const font of [100, 120]) {
    const f = await create("owner", "graphit", 390, locale);
    f.data.prefs.appearance = { ...(f.data.prefs.appearance ?? {}), theme: "graphit", font };
    await f.page.setViewportSize({ width: 390, height: 844 });
    for (const path of pages) {
      await f.page.goto(`${base}${path}`);
      await f.page.waitForTimeout(1200);
      for (const word of await f.page.evaluate(brokenWords))
        problems.push(`${locale} ${font}% ${path}: ${word}`);
      if (path === "/")
        await f.page.screenshot({ path: `${out}/dashboard-${locale}-${font}.png`, fullPage: true });
    }
    await f.page.context().close();
  }
}
await browser.close();
assert.deepEqual(problems, [], problems.join("\n"));
console.log(`ui-word-breaks: ${pages.length} pages × de/en × 100/120 % without broken words`);
