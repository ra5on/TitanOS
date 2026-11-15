// The working dots of the KI-Agent really move in the built page: every
// figure (thinking, web, files, write, home, switch, code) is put on a
// blank page with the CSS of the build and looked at three times.
// 0.93 stood still: the minifier had made "animation: none" of a
// shorthand without a name, and the source alone did not show it.
//
//   PLAYWRIGHT_MODULE=… node scripts/test-ui-agent-motion.mjs
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? "playwright");
const dir = join(import.meta.dirname, "../backend/internal/webui/dist/_app/immutable/assets");
const file = readdirSync(dir)
  .filter((f) => f.endsWith(".css"))
  .map((f) => join(dir, f))
  .find((f) => /svelte-[a-z0-9]+-dot-a/.test(readFileSync(f, "utf8")));
if (!file) throw new Error("no built CSS with the dots; run npm --prefix web run build first");
const css = readFileSync(file, "utf8");
const hash = /(svelte-[a-z0-9]+)-dot-a/.exec(css)[1];
const kinds = ["", "web", "files", "write", "home", "switch", "code"];
const dot = `<i class="${hash}"></i>`;
const browser = await chromium.launch();
const page = await browser.newPage();
await page.setContent(
  `<style>${css}</style><body>` +
    kinds.map((k) => `<span class="dots ${hash}" data-kind="${k}">${dot}${dot}${dot}</span>`).join(""),
);
const look = () =>
  page.evaluate(() =>
    [...document.querySelectorAll(".dots")].map((d) =>
      [...d.children].map((i) => getComputedStyle(i).transform + getComputedStyle(i).opacity).join("|"),
    ),
  );
const a = await look();
await page.waitForTimeout(700);
const b = await look();
await page.waitForTimeout(900);
const c = await look();
await browser.close();
const still = kinds.filter((_, n) => a[n] === b[n] && b[n] === c[n]).map((k) => k || "thinking");
if (still.length) throw new Error("these figures stand still: " + still.join(", "));
console.log("KI-Agent dots move: ok");
