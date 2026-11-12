// PLAN 70: quick actions in the main menu – typing finds things to do; the
// right route is called, disturbing ones ask first (Cancel calls nothing),
// accounts without admin rights see only their own actions.
// Isolated API data only; nothing reaches a NAS.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5240 node scripts/test-ui-quickactions.mjs
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
const base = process.env.UI_URL || "http://127.0.0.1:5240";
const out = process.env.UI_OUTPUT || "out/validation/quickactions";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

async function open(role) {
  const made = await create(role, "graphit", 1440);
  const { ctx, page } = made;
  await page.setViewportSize({ width: 1440, height: 900 });
  await ctx.addInitScript(() => localStorage.removeItem("titanos.layout"));
  made.posts = [];
  await ctx.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.slice(7);
    const method = route.request().method();
    if (
      method === "POST" &&
      /^\/(docker\/containers|backups|config-backup)\//.test(p)
    ) {
      made.posts.push(p);
      return route.fulfill({ status: 202, json: {} });
    }
    if (p === "/backups" && method === "GET")
      return route.fulfill({
        json: {
          jobs: [
            { job: { id: "nachts", enabled: true }, state: {}, next: null },
          ],
        },
      });
    return route.fallback();
  });
  await page.goto(base + "/");
  await page.locator(".dock").waitFor({ timeout: 15000 });
  return made;
}

// Owner: start a stopped container right away; stopping asks first.
{
  const made = await open("owner");
  const { ctx, page } = made;
  await page.keyboard.press("Control+Space");
  const menu = page.locator(".launcher");
  // PLAN 79-10: the search field of the menu sits in the top bar.
  const search = page
    .locator(".topbar")
    .getByRole("searchbox", { name: /suchen/i });
  await search.fill("db starten");
  await menu
    .getByRole("button", { name: "Container starten: photos-db-1" })
    .click();
  await menu.getByText("Ausgelöst: Container starten: photos-db-1").waitFor();
  assert.deepEqual(made.posts, ["/docker/containers/db/start"]);
  await search.fill("web stoppen");
  await page.screenshot({ path: `${out}/aktionen.png` });
  await menu
    .getByRole("button", { name: /Container stoppen: photos-web-1/ })
    .click();
  const ask = page.getByRole("dialog", { name: "Wirklich ausführen?" });
  await ask.waitFor();
  await page.screenshot({ path: `${out}/aktionen-rueckfrage.png` });
  // Enter does not run the dangerous button.
  await page.keyboard.press("Enter");
  await page.waitForTimeout(300);
  assert.deepEqual(
    made.posts,
    ["/docker/containers/db/start"],
    "Enter ran nothing",
  );
  // Enter landed on "Abbrechen" (first in the markup, focused): closed, nothing ran.
  await ask.waitFor({ state: "detached" });
  await menu
    .getByRole("button", { name: /Container stoppen: photos-web-1/ })
    .click();
  await ask.getByRole("button", { name: "Abbrechen" }).click();
  await ask.waitFor({ state: "detached" });
  assert.deepEqual(
    made.posts,
    ["/docker/containers/db/start"],
    "Cancel ran nothing",
  );
  await menu
    .getByRole("button", { name: /Container stoppen: photos-web-1/ })
    .click();
  await page
    .getByRole("dialog", { name: "Wirklich ausführen?" })
    .getByRole("button", { name: "Ausführen" })
    .click();
  await menu.getByText("Ausgelöst: Container stoppen: photos-web-1").waitFor();
  assert.deepEqual(made.posts, [
    "/docker/containers/db/start",
    "/docker/containers/web/stop",
  ]);
  // Backup jobs are asked for while typing.
  await search.fill("sicherung jetzt");
  await menu
    .getByRole("button", { name: "Sicherung jetzt starten: nachts" })
    .click();
  await menu.getByText("Ausgelöst: Sicherung jetzt starten: nachts").waitFor();
  assert.equal(made.posts.at(-1), "/backups/nachts/run");
  await ctx.close();
}

// A user: no container, VM, backup or NAS power actions.
{
  const made = await open("user");
  const { ctx, page } = made;
  await page.keyboard.press("Control+Space");
  const menu = page.locator(".launcher");
  // PLAN 79-10: the search field of the menu sits in the top bar.
  const search = page
    .locator(".topbar")
    .getByRole("searchbox", { name: /suchen/i });
  await search.fill("starten");
  await page.waitForTimeout(300);
  assert.equal(await menu.locator(".action").count(), 0, "no admin actions");
  await search.fill("sperren");
  await menu.getByRole("button", { name: "NAS-Sitzung sperren" }).waitFor();
  await ctx.close();
}

await browser.close();
assert.deepEqual(errors, [], "no script errors");
console.log("quick actions UI ok");
