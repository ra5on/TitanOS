// 1.15, Kontakte like Apple's Kontakte (PLAN 115-6, drafts 40–44 in
// Entwuerfe/115-mail-handy) and the fixes of PLAN 115-7, on isolated data
// (test-ui-kontakte-fixtures.mjs; the clock fixed at Tuesday, 17 November
// 2026 in Berlin), nothing reaches a NAS.
//
// 115-7: rows without a letter ("1&1 Hotline") and with a folded letter
// outside a–z ("Kari Øvrebø"), in titand's byte-wise order, give one "#",
// last, and no page error; Kontakte opened, the app navigated elsewhere in
// the page (client side, the modules stay) and back: the list is still
// there (the old ETag made it a 304 and an empty list).
// 115-6: the window sizes 1440×900, 900×700, 600×800 and 420×520 (each
// desktop window is a page of its own) light and dark, and the phone
// 390×844 light and dark: three columns, two with the rail (under 900 px)
// or one level at a time (under 620 px and the phone); "Listen", the list
// with the big title, search, "Meine Karte", sections, family name bold,
// the register (a tap jumps), the contact with its quick buttons, the sheet
// "Neuer Kontakt" with its green rows; no sideways scroll, no cut text
// (cutTextIn), 44 px targets in the compact views; "Teilen" gives the card
// as a .vcf; the KI-Agent is in the phone bar's ⋯ menu. Screenshots into
// UI_OUTPUT and, with UI_BAU, the build pictures kontakte-*.png there.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5213 UI_OUTPUT=/path node scripts/test-ui-kontakte-115.mjs
import assert from "node:assert/strict";
import { mkdir, readFile } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
import {
  calendarState,
  installCalendar,
} from "./test-ui-kalender-fixtures.mjs";
import {
  contactsState,
  installContacts,
} from "./test-ui-kontakte-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5213";
const out = process.env.UI_OUTPUT || "out/validation/kontakte-115";
const bau = process.env.UI_BAU || "";
await mkdir(out, { recursive: true });
if (bau) await mkdir(bau, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);
const NOW = new Date("2026-11-17T12:29:00+01:00");

const blank = {
  book: "kontakte",
  first: "",
  last: "",
  org: "",
  phones: [],
  emails: [],
  addresses: [],
  birthday: "",
  notes: "",
  hasPhoto: false,
  modified: "2026-11-03T18:12:00+01:00",
};
/** The data of the drafts, a group, two rows under "#" and the own card. */
function withExtras(state) {
  state.contacts.push(
    {
      ...blank,
      id: "hotline.vcf",
      etag: '"1-hotline.vcf"',
      org: "1&1 Hotline",
      phones: [{ type: "work", value: "0721 9600" }],
    },
    {
      ...blank,
      id: "kari.vcf",
      etag: '"1-kari.vcf"',
      first: "Kari",
      last: "Øvrebø",
      emails: [{ type: "home", value: "kari@eksempel.example" }],
    },
  );
  state.groups = [
    { id: "familie.vcf", name: "Familie", members: ["anna.vcf", "lena.vcf"] },
  ];
}

async function open({
  width = 1440,
  height = 900,
  theme = "hell",
  phone = false,
  me = "",
  agent = false,
  change = withExtras,
} = {}) {
  const made = await create(
    "user",
    theme,
    width,
    "de",
    phone
      ? {
          isMobile: true,
          hasTouch: true,
          deviceScaleFactor: 2,
          viewport: { width, height },
          timezoneId: "Europe/Berlin",
          acceptDownloads: true,
        }
      : {
          viewport: { width, height },
          timezoneId: "Europe/Berlin",
          acceptDownloads: true,
        },
  );
  await made.ctx.clock.setFixedTime(NOW);
  if (phone)
    await made.ctx.addInitScript(() =>
      localStorage.removeItem("titanos.layout"),
    );
  // 1.15: the own card is a preference of the account on the NAS.
  if (me) made.data.prefs.contacts = { me };
  await installCalendar(made.ctx, calendarState());
  const state = contactsState();
  change(state);
  await installContacts(made.ctx, state);
  if (agent)
    await made.ctx.route("**/api/v1/services/state", (route) =>
      route.fulfill({
        json: {
          docker: true,
          vms: true,
          lxc: true,
          calendar: true,
          contacts: true,
          mail: state.mail,
          agent: true,
        },
      }),
    );
  return { ...made, state };
}

async function sound(page, name) {
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 0, `${name}: scrolls sideways by ${overflow}px`);
  const columns = await page.evaluate(() =>
    [...document.querySelectorAll(".cf .col")]
      .filter((el) => el.scrollWidth - el.clientWidth > 1)
      .map((el) => `${el.className} ${el.scrollWidth - el.clientWidth}px`),
  );
  assert.deepEqual(columns, [], `${name}: a column scrolls sideways`);
  await page.evaluate(() =>
    document
      .querySelectorAll(".letter-block")
      .forEach((el) => (el.style.contentVisibility = "visible")),
  );
  assert.deepEqual(await page.evaluate(cutTextIn), [], `${name}: cut text`);
  assert.equal(
    await page.evaluate(() => /\S…/.test(document.body.innerText)),
    false,
    `${name}: an ellipsis`,
  );
  // A placeholder wider than its field would be cut, too.
  const holders = await page.evaluate(() => {
    const c = document.createElement("canvas").getContext("2d");
    return [...document.querySelectorAll("input[placeholder]")]
      .filter((el) => el.getBoundingClientRect().width > 0 && !el.value)
      .filter((el) => {
        const cs = getComputedStyle(el);
        c.font = `${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
        const room =
          el.clientWidth -
          parseFloat(cs.paddingLeft) -
          parseFloat(cs.paddingRight);
        return c.measureText(el.placeholder).width > room + 1;
      })
      .map((el) => el.placeholder);
  });
  assert.deepEqual(holders, [], `${name}: a placeholder is cut`);
  await page.evaluate(() =>
    document
      .querySelectorAll(".letter-block")
      .forEach((el) => (el.style.contentVisibility = "")),
  );
}
/** Targets under 44 px in a part of the page (compact). The register's letters are one strip of 44 px. */
const small = (page, scope) =>
  page.evaluate((scope) => {
    const found = [];
    for (const el of document.querySelectorAll(
      `${scope} :is(button, a[href], input:not([type=checkbox]):not([type=radio]):not([type=range]), select)`,
    )) {
      const r = el.getBoundingClientRect();
      if (!r.width || el.closest("[hidden], .sr-only, [data-contact-index]"))
        continue;
      if (r.height < 43.5)
        found.push(
          `${el.tagName} ${(el.textContent || el.getAttribute("aria-label") || "").trim().slice(0, 30)} ${Math.round(r.height)}`,
        );
    }
    return found;
  }, scope);
async function shot(page, name, bauName) {
  await page.screenshot({ path: `${out}/${name}.png`, animations: "disabled" });
  if (bau && bauName)
    await page.screenshot({
      path: `${bau}/kontakte-${bauName}.png`,
      animations: "disabled",
    });
}
async function ready(page) {
  await page.locator("[data-pim-ready]").waitFor({ state: "attached" });
  await page.waitForTimeout(400);
}
async function until(fn, what, ms = 8000) {
  const end = Date.now() + ms;
  while (!(await fn())) {
    if (Date.now() > end) throw new Error(`timed out: ${what}`);
    await new Promise((r) => setTimeout(r, 100));
  }
}
const letters = (page) =>
  page
    .locator("[data-contact-list] .letter-block")
    .evaluateAll((els) => els.map((e) => e.dataset.letter));

try {
  // ---- 115-7: one "#", last; no page error ----
  {
    const { page, ctx } = await open();
    await page.goto(base + "/contacts");
    await ready(page);
    const shown = await letters(page);
    assert.equal(
      new Set(shown).size,
      shown.length,
      `each letter once: ${shown}`,
    );
    assert.equal(shown.at(-1), "#", `"#" last: ${shown}`);
    const hash = page.locator(
      '[data-contact-list] .letter-block[data-letter="#"]',
    );
    assert.match(await hash.innerText(), /1&1 Hotline[\s\S]*Kari Øvrebø/);
    assert.equal(await page.locator("[data-list-failed]").count(), 0);
    // The family name bold: "Kari" plain, "Øvrebø" bold; a company all bold.
    assert.equal(
      await page.locator("[data-contact='kari.vcf'] b").innerText(),
      "Øvrebø",
    );
    assert.equal(
      await page.locator("[data-contact='hotline.vcf'] b").innerText(),
      "1&1 Hotline",
    );
    assert.ok(
      await page
        .locator("[data-contact='ben.vcf']")
        .getByText("Ben Muster")
        .count(),
      "the name stays one text",
    );

    // ---- 115-7: elsewhere in the app and back – the list stays ----
    const rows = await page.locator("[data-contact]").count();
    assert.ok(rows >= 10, `rows ${rows}`);
    await page.locator("[data-side-iphone]").click();
    await page.waitForURL(/\/contacts\/iphone/);
    await page.waitForTimeout(500);
    await page.goBack();
    await page.waitForURL(/\/contacts$/);
    await ready(page);
    await until(
      async () => (await page.locator("[data-contact]").count()) === rows,
      "the list after coming back",
    );
    assert.match(
      await page.locator("[data-contact-list] .count").innerText(),
      new RegExp(`${rows} Kontakte`),
    );
    await ctx.close();
  }

  // ---- The sizes of a window and the phone, light and dark ----
  const sizes = [
    [1440, 900, false, "wide"],
    [900, 700, false, "wide"],
    [600, 800, false, "compact"],
    [420, 520, false, "compact"],
    [390, 844, true, "compact"],
  ];
  for (const [width, height, phone, kind] of sizes) {
    for (const theme of ["hell", "dunkel"]) {
      const tag = `${width}x${height}-${theme}`;
      const bauTag = phone ? `handy-${theme}` : `${width}x${height}-${theme}`;
      const keep =
        theme === "dunkel" || phone || width === 1440 || width === 600;
      const { page, ctx } = await open({
        width,
        height,
        theme,
        phone,
        me: "lena.vcf",
      });
      await page.goto(base + "/contacts");
      await ready(page);
      if (kind === "wide") {
        await page.locator(".cf.columns").waitFor();
        const folded = await page.locator(".cf-side.folded").count();
        assert.equal(
          folded,
          width < 900 ? 1 : 0,
          `${tag}: the side bar folded only under 900 px`,
        );
        if (!folded)
          assert.ok(
            await page
              .locator("[data-contact-side] [data-side-all]")
              .isVisible(),
            `${tag}: Listen beside the list`,
          );
      } else {
        await page.locator(".cf.levels.compact").waitFor();
        assert.ok(
          await page.locator("[data-contact-list] [data-back]").isVisible(),
          `${tag}: ‹ Listen`,
        );
        assert.deepEqual(
          await small(page, "[data-contact-list]"),
          [],
          `${tag}: list targets of 44 px`,
        );
      }
      // The list: big title, search, my card, the register.
      assert.equal(
        await page.locator("#con-list-head").innerText(),
        "Kontakte",
      );
      assert.ok(
        await page.locator("[data-my-card='lena.vcf']").isVisible(),
        `${tag}: Meine Karte`,
      );
      await page.locator("[data-contact-index]").waitFor();
      await sound(page, `${tag} list`);
      await shot(page, `liste-${tag}`, keep ? `liste-${bauTag}` : "");

      // The register jumps (a tap on the strip at "R").
      const r = await page
        .locator("[data-contact-index] [data-index='R']")
        .boundingBox();
      if (r) {
        await page.mouse.click(r.x + r.width / 2, r.y + r.height / 2);
        await page.waitForTimeout(300);
        // "R" stands right below the sticky search (not under it) – or the list
        // is scrolled to its end and R is in sight.
        const at = await page.evaluate(() => {
          const sec = document.querySelector(
            '[data-contact-list] .letter-block[data-letter="R"]',
          );
          const bar = document.querySelector("[data-contact-list] .searchbar");
          const box =
            document.querySelector(".cf.columns .cf-list") ??
            document.scrollingElement;
          if (!sec || !bar) return null;
          const r = sec.getBoundingClientRect();
          return {
            gap: r.top - bar.getBoundingClientRect().bottom,
            end: box.scrollTop + box.clientHeight >= box.scrollHeight - 2,
            seen:
              r.top >= bar.getBoundingClientRect().bottom - 2 &&
              r.top < innerHeight,
          };
        });
        assert.ok(
          at && at.seen && (Math.abs(at.gap) <= 4 || at.end),
          `${tag}: the register jumped to R ${JSON.stringify(at)}`,
        );
      }

      // A contact: quick buttons, groups of fields.
      await page.goto(base + "/contacts?id=ben.vcf");
      await page.locator("[data-contact-detail]").waitFor();
      await page.waitForTimeout(300);
      const acts = page.locator("[data-contact-actions]");
      assert.equal(
        await acts.locator("a[href^='sms:']").count(),
        1,
        `${tag}: Nachricht`,
      );
      assert.equal(
        await acts.locator("a[href^='tel:']").count(),
        1,
        `${tag}: Anrufen`,
      );
      assert.equal(
        await acts.locator("[data-contact-share]").count(),
        1,
        `${tag}: Teilen`,
      );
      assert.ok(
        await page.locator("[data-contact-edit]").isVisible(),
        `${tag}: Bearbeiten`,
      );
      if (kind === "compact")
        assert.deepEqual(
          await small(page, ".col-contact"),
          [],
          `${tag}: contact targets of 44 px`,
        );
      await sound(page, `${tag} contact`);
      await shot(page, `kontakt-${tag}`, keep ? `kontakt-${bauTag}` : "");

      // Listen (compact: a level of its own).
      if (kind === "compact") {
        await page.goto(base + "/contacts?side=1");
        await page.locator("[data-contact-side]").waitFor();
        await page.waitForTimeout(200);
        const side = await page.locator("[data-contact-side]").innerText();
        assert.match(
          side,
          /Listen[\s\S]*Alle Kontakte[\s\S]*ADRESSBÜCHER[\s\S]*GRUPPEN[\s\S]*Familie[\s\S]*MEHR[\s\S]*Import und Export[\s\S]*Am iPhone einrichten/i,
        );
        assert.deepEqual(
          await small(page, "[data-contact-side]"),
          [],
          `${tag}: Listen targets of 44 px`,
        );
        await sound(page, `${tag} lists`);
        await shot(page, `listen-${tag}`, keep ? `listen-${bauTag}` : "");
      }

      // "Neuer Kontakt": the sheet (compact) or the column, green rows.
      await page.goto(base + "/contacts?new=1");
      await page.locator("[data-contact-editor]").waitFor();
      await page.waitForTimeout(400);
      assert.equal(
        await page.locator("[data-contact-editor] [data-add]").count(),
        5,
        `${tag}: five "+ … hinzufügen"`,
      );
      if (kind === "compact") {
        assert.equal(
          await page.locator("[data-editor-sheet]").count(),
          1,
          `${tag}: a sheet`,
        );
        assert.deepEqual(
          await small(page, "[data-editor-sheet]"),
          [],
          `${tag}: sheet targets of 44 px`,
        );
      }
      await sound(page, `${tag} new`);
      await shot(page, `neu-${tag}`, keep ? `neu-${bauTag}` : "");
      // "+ Telefon hinzufügen" gives a row with its red "−".
      await page.locator("[data-add='phone']").click();
      await page.locator("[data-list='phones'] .row input[type=tel]").waitFor();
      assert.equal(
        await page.evaluate(() => document.activeElement?.tagName),
        "SELECT",
        `${tag}: the new row has the focus`,
      );
      await ctx.close();
    }
  }

  // ---- "Meine Karte": chosen at the contact, shown above the list ----
  {
    const { page, ctx, data } = await open();
    await page.goto(base + "/contacts?id=anna.vcf");
    await page.locator("[data-contact-detail]").waitFor();
    assert.equal(
      await page.locator("[data-my-card]").count(),
      0,
      "no card before it is chosen",
    );
    await page.locator("[data-contact-me]").click();
    await page.locator("[data-my-card='anna.vcf']").waitFor();
    await until(() => data.prefs.contacts?.me === "anna.vcf", "kept on the NAS");
    assert.equal(
      await page.locator("[data-contact-me]").innerText(),
      "Nicht mehr meine Karte",
    );
    await page.locator("[data-contact-me]").click();
    await page.locator("[data-my-card]").waitFor({ state: "detached" });

    // ---- "Teilen": the card as a .vcf (no share sheet here) ----
    const [download] = await Promise.all([
      page.waitForEvent("download"),
      page.locator("[data-contact-share]").click(),
    ]);
    assert.equal(download.suggestedFilename(), "Anna Beispiel.vcf");
    const card = await readFile(await download.path(), "utf8");
    assert.match(
      card,
      /^BEGIN:VCARD\r\nVERSION:3\.0\r\nN:Beispiel;Anna;;;\r\nFN:Anna Beispiel\r\n/,
    );
    assert.match(card, /TEL;TYPE=CELL:\+49 170 1234567\r\n/);
    assert.match(card, /BDAY:1991-11-12\r\n/);
    assert.match(card, /END:VCARD\r\n$/);
    await page.getByText("Als Datei Anna Beispiel.vcf gespeichert").waitFor();
    await ctx.close();
  }

  // ---- The KI-Agent in the phone bar's ⋯ menu; no bar at the bottom ----
  {
    const { page, ctx } = await open({
      width: 390,
      height: 844,
      phone: true,
      agent: true,
    });
    await page.goto(base + "/contacts");
    await ready(page);
    assert.equal(
      await page.locator(".bottombar").count(),
      0,
      "no bar at the bottom",
    );
    await page.locator("header.phone-bar button[aria-haspopup=menu]").click();
    await page
      .getByRole("menuitemcheckbox", { name: "KI-Agent" })
      .or(page.getByRole("menuitem", { name: "KI-Agent" }))
      .first()
      .waitFor();
    // No ⋯ of the page's own then: the bar's menu has the entry.
    assert.equal(
      await page.locator("[data-contact-list] .navr .dropdown").count(),
      0,
      "no second ⋯",
    );
    await shot(page, "handy-menue-agent", "handy-menue-agent");
    await ctx.close();
  }

  {
    // A narrow window (no phone bar): the page's own ⋯ has the KI-Agent.
    const { page, ctx } = await open({ width: 420, height: 520, agent: true });
    await page.goto(base + "/contacts");
    await ready(page);
    await page.locator("[data-contact-list] .navr .dropdown > button").click();
    await page.getByRole("menuitem", { name: "KI-Agent" }).waitFor();
    await sound(page, "420 own menu");
    await ctx.close();
  }

  const real = errors.filter((e) => !/503|calendar\/stream|agent/i.test(e));
  assert.deepEqual(real, [], "page errors");
  console.log("test-ui-kontakte-115: ok");
} finally {
  await browser.close();
}
