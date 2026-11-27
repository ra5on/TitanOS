import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { chromium } = await import("playwright");

const origin = "http://127.0.0.1:45852";
const sessions = JSON.parse(readFileSync(0, "utf8"));
const browser = await chromium.launch();
try {
  for (const [account, data] of Object.entries(sessions)) {
    const context = await browser.newContext({
      viewport: { width: 1440, height: 930 },
      locale: "de-DE",
      reducedMotion: "reduce",
      serviceWorkers: "block",
    });
    await context.addCookies(
      data.cookies.map((cookie) => ({ ...cookie, url: origin })),
    );
    await context.addInitScript(() =>
      localStorage.setItem("titanos.locale", "de"),
    );
    const page = await context.newPage();
    const requests = [],
      errors = [];
    page.on("request", (r) => requests.push(new URL(r.url()).pathname));
    page.on("pageerror", (error) => errors.push(error.message));
    for (const app of ["mail", "calendar"]) {
      await page.goto(origin + "/" + app);
      const offer = page
        .locator("dialog[open]")
        .filter({ hasText: "Office-Programme direkt im Browser" });
      const notice = page.getByRole("dialog", { name: "Haftungsausschluss" });
      await page.locator(".topbar").waitFor();
      if (await offer.isVisible())
        await offer.getByRole("button", { name: "Nicht mehr fragen" }).click();
      if (await notice.isVisible()) {
        await notice.getByRole("checkbox").check();
        await notice.getByRole("button", { name: "Weiter" }).click();
      }
      const win = page
        .frameLocator('iframe[data-app="app:' + app + '"]')
        .last();
      const toggle = win.locator(".agent-toggle:visible").first();
      try {
        await toggle.waitFor({ timeout: 15000 });
      } catch (e) {
        console.error(
          app,
          page.url(),
          await page
            .locator("iframe")
            .evaluateAll((nodes) =>
              nodes.map((n) => [n.dataset.app, n.getAttribute("src")]),
            ),
          await page.locator("h1,h2").allTextContents(),
          errors,
        );
        throw e;
      }
      const state = page.waitForResponse(
        (r) =>
          new URL(r.url()).pathname === "/api/v1/personal-agent/state" &&
          r.ok(),
      );
      await toggle.click();
      const panel = win.locator(".pim-agent:visible");
      await panel.waitFor({ timeout: 10000 });
      await state;
      const contents = await panel.innerText();
      assert.ok(contents.trim());
      for (const item of Object.values(sessions))
        assert.ok(!contents.includes(item.marker));
      await panel.locator("select").first().waitFor();
      await page.screenshot({
        path: "/evidence/" + account + "-" + app + ".png",
        animations: "disabled",
      });
      console.log(
        "PASS upgraded product UI:",
        account,
        app,
        "opened its personal panel",
      );
    }
    assert.ok(
      !requests.some((path) => path.startsWith("/api/v1/agent/")),
      "legacy Agent API requested",
    );
    assert.deepEqual(errors, []);
    await context.close();
  }
} finally {
  await browser.close();
}
