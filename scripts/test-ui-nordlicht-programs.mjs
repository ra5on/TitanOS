// Activated specialist applications, rendered with the local NAS fixtures.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { fixtureFactory } from "./test-ui-fixtures.mjs";
import { installMail, mailState } from "./test-ui-mail-fixtures.mjs";
import {
  calendarState,
  installCalendar,
} from "./test-ui-kalender-fixtures.mjs";
import {
  contactsState,
  installContacts,
} from "./test-ui-kontakte-fixtures.mjs";
import { installNvr, nvrState } from "./test-ui-nvr-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5297";
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname));
const out = process.env.UI_OUTPUT;
if (out) await mkdir(out, { recursive: true });
const before = process.env.UI_BASELINE === "1";
const errors = [];
const create = fixtureFactory(browser, errors);
const at = "2026-11-17T12:00:00Z";
const chat = {
  id: "ca10000000000001",
  title: "Wöchentliche Sicherung",
  updated: at,
  created: at,
  state: "idle",
  origin: "web",
  entries: [
    { id: "entry-a", role: "user", content: "Wann läuft die Sicherung?", at },
    {
      id: "entry-b",
      role: "assistant",
      content: "Die nächste Sicherung ist für Freitag um 18 Uhr geplant.",
      at,
    },
  ],
};
const services = {
  calendar: true,
  contacts: true,
  nvr: true,
  agent: true,
  mail: true,
  docker: true,
};
const contact = "/contacts?id=anna.vcf";
const back = [];
async function shot(page, name) {
  if (!out) return;
  await page.evaluate(async () => {
    await document.fonts.ready;
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await page.screenshot({
    path: out + "/" + name + ".png",
    animations: "disabled",
  });
}
async function app(id, run) {
  if (process.env.UI_APPS && !process.env.UI_APPS.split(",").includes(id))
    return;
  const { ctx, page } = await create("owner", "graphit", 1280, "de", {
    viewport: { width: 1280, height: 830 },
    reducedMotion: "reduce",
    serviceWorkers: "block",
  });
  page.setDefaultTimeout(12000);
  try {
    page.on("console", (m) => {
      if (m.type() === "error" && !m.text().includes("Failed to load resource"))
        console.error(id, m.text().slice(0, 320));
    });
    const state = nvrState();
    state.vod = true;
    if (id === "mail") await installMail(ctx, mailState());
    if (id === "calendar") await installCalendar(ctx, calendarState());
    if (id === "contacts") await installContacts(ctx, contactsState());
    if (id === "nvr") await installNvr(ctx, state);
    if (id === "agent") {
      const replies = {
        "/agent/status": {
          allowed: true,
          admin: true,
          enabled: true,
          volume: true,
          web: true,
          reachable: true,
          model: "Ollama",
          models: [{ name: "Ollama", capabilities: ["completion", "tools"] }],
          running: [],
        },
        "/agent/chats": { chats: [chat] },
        ["/agent/chats/" + chat.id]: chat,
        "/agent/projects": { projects: [] },
      };
      await ctx.route("**/api/v1/agent/**", (r) => {
        const d = replies[new URL(r.request().url()).pathname.slice(7)];
        return d ? r.fulfill({ json: d }) : r.fallback();
      });
    }
    await ctx.route("**/api/v1/services/state", (r) =>
      r.fulfill({ json: services }),
    );
    await ctx.route("**/api/v1/mail/access", (r) =>
      r.fulfill({ json: { enabled: true } }),
    );
    await run(page);
    console.log(id, "OK");
  } catch (e) {
    back.push(id + ": " + e.message);
    console.error(
      id,
      page.url(),
      (
        await page
          .locator("body")
          .innerText({ timeout: 1000 })
          .catch(() => "")
      ).slice(0, 900),
    );
  } finally {
    await ctx.close();
  }
}
try {
  await app("mail", async (page) => {
    await page.goto(base + "/mail?a=acc-privat&f=INBOX&m=999");
    await page.locator(".mail .col-message .body").waitFor();
    await shot(page, "Mail-Lesen");
    if (!before) {
      const more = page
        .locator(".message .actions")
        .getByRole("button", { name: "Weitere Aktionen" });
      await more.press("ArrowDown");
      const move = page.getByRole("menuitem", { name: "Verschieben" });
      assert.ok(await move.evaluate((el) => el === document.activeElement));
      await move.press("Escape");
      assert.ok(await more.evaluate((el) => el === document.activeElement));
    }
    await page.goto(base + "/mail/compose?a=acc-privat");
    await page.locator("textarea").first().waitFor();
    await shot(page, "Mail-Verfassen");
  });
  await app("calendar", async (page) => {
    await page.goto(base + "/calendar?view=month&date=2026-11-17");
    await page
      .locator(".calendar [role=grid], .calendar .month,.calendar .dow")
      .first()
      .waitFor();
    await shot(page, "Kalender-Monat");
    const bar = page.locator(".cbar");
    const period = await bar.locator("h2").innerText();
    await bar.getByRole("button", { name: "Weiter", exact: true }).click();
    await page.waitForFunction(
      (period) => document.querySelector(".cbar h2")?.textContent !== period,
      period,
    );
    await bar.getByRole("button", { name: "Zurück", exact: true }).click();
    await page
      .getByRole("button", { name: "Neuer Termin", exact: true })
      .first()
      .click();
    await page.locator("[data-event-form]").waitFor();
    await shot(page, "Kalender-Termin");
  });
  await app("contacts", async (page) => {
    await page.goto(base + contact);
    const card = page.locator('[data-contact-detail="anna.vcf"]');
    await card.waitFor();
    await shot(page, "Kontakte-Karte");
    await card.locator("[data-contact-edit]").click();
    await page.locator("[data-editor-save]").waitFor();
    await shot(page, "Kontakte-Bearbeiten");
  });
  await app("nvr", async (page) => {
    await page.goto(base + "/nvr/events");
    await page
      .locator("ul.grid button.card, ul.list button.event-card")
      .filter({ hasText: "Einfahrt" })
      .first()
      .waitFor();
    await shot(page, "NVR-Ereignisse");
    await page
      .locator("ul.grid button.card, ul.list button.event-card")
      .filter({ hasText: /Auto.*Einfahrt/ })
      .first()
      .click();
    const video = page.locator("video").first();
    await video.waitFor();
    await page.waitForFunction(
      () => document.querySelector("video")?.duration > 0,
    );
    if (!before) {
      await video.evaluate((el) => {
        el.pause();
        el.requestVideoFrameCallback(() => {
          el.dataset.presented = "true";
        });
      });
      const seek = page.getByRole("slider", {
        name: "Zeitleiste der Aufnahme",
      });
      await seek.focus();
      await seek.press("ArrowRight");
      await page.waitForFunction(() => {
        const el = document.querySelector("video");
        return (
          el?.dataset.presented === "true" &&
          el.currentTime > 0 &&
          !el.seeking &&
          el.readyState >= el.HAVE_CURRENT_DATA
        );
      });
    }
    await shot(page, "NVR-Fahrzeug");
  });
  await app("agent", async (page) => {
    await page.goto(base + "/agent?chat=" + chat.id);
    await page
      .getByText(chat.entries[1].content, { exact: true })
      .first()
      .waitFor();
    await shot(page, "Agent-Gespraech");
    const find = page.getByRole("button", {
      name: "In diesem Chat suchen",
      exact: true,
    });
    await find.click();
    assert.equal(await find.getAttribute("aria-pressed"), "true");
    await find.click();
    assert.equal(await find.getAttribute("aria-pressed"), "false");
  });
  if (
    (!process.env.UI_APPS ||
      process.env.UI_APPS.split(",").includes("calendar-phone")) &&
    !before
  ) {
    const { ctx, page } = await create("user", "graphit", 390, "de", {
      nativeShell: true,
      isMobile: true,
      hasTouch: true,
      viewport: { width: 390, height: 844 },
      serviceWorkers: "block",
    });
    try {
      await installCalendar(ctx, calendarState());
      await page.goto(base + "/calendar?view=month&date=2026-11-17");
      const today = page.getByRole("button", { name: "Heute", exact: true });
      // §121: the bar at the bottom (ab Werk) reserves its height like the dock did.
      const dock = page.locator("[data-phone-tabs]");
      await dock.waitFor();
      await page.waitForFunction(
        () =>
          parseFloat(
            getComputedStyle(document.documentElement).getPropertyValue(
              "--phone-dock-height",
            ),
          ) > 0,
      );
      assert.ok(
        (await today.boundingBox()).y + (await today.boundingBox()).height <=
          (await dock.boundingBox()).y,
      );
      await today.click();
      await shot(page, "Kalender-Heute-Handy");
      console.log("calendar-phone bottom bar and Today OK");
    } finally {
      await ctx.close();
    }
  }
} finally {
  await browser.close();
}
assert.deepEqual(back, []);
assert.deepEqual(
  errors.filter((e) => !e.includes("allow-same-origin")),
  [],
);
