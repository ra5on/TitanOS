// 1.11 (step 17, TECHNIK 6.2/6.3, drafts 30/31/62): KI-Agent › Rechte
// with the two areas Kalender and Kontakte – after Dateien, closed with
// their state as text ("Lesen · Eintragen mit Nachfrage"), open with two
// parts (lesen: Erlaubt/Verboten; eintragen, ändern und löschen: Ohne
// Nachfrage/Nachfragen/Verboten) instead of a tool list. A change makes
// the quick choice "Eigene Einstellung", saving sends dataRights with both
// axes and never areas/tools for these areas, a quick choice clears them.
// KI-Agent › Home Assistant › "Was Home Assistant fragen darf": Kalender
// and Kontakte with only Verboten / Nur lesen, saved in assistData (never
// assistAreas). Desktop 1440 px light and dark, phone 390 px light and
// dark; no sideways scroll, no cut text. Isolated API data only.
// PLAYWRIGHT_MODULE=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5211 node scripts/test-ui-agent-rights-pim.mjs
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { cutTextIn, fixtureFactory } from "./test-ui-fixtures.mjs";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);

const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const base = process.env.UI_URL || "http://127.0.0.1:5211";
const out = process.env.UI_OUTPUT || "out/validation/agent-rechte";
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const status = {
  enabled: true,
  allowed: true,
  admin: true,
  model: "gpt-oss:20b",
  think: "medium",
  context: 0,
  volume: true,
  web: true,
  reachable: true,
  version: "1.11.0",
  models: [],
  running: [],
};
const baseSettings = () => ({
  enabled: true,
  ollama: "http://192.168.10.5:11434",
  model: "gpt-oss:20b",
  think: "medium",
  context: 0,
  maxSteps: 30,
  users: [],
  policy: { preset: "standard", folders: [] },
  locations: [],
  homeassistant: { release: { domains: ["sensor"] }, assist: true },
});
const tools = [
  ["system_status", "system", "read"],
  ["files_list", "files", "read"],
  ["file_write", "files", "change"],
  ["calendar_list", "calendar", "read"],
  ["calendar_search", "calendar", "read"],
  ["calendar_create", "calendar", "change"],
  ["calendar_update", "calendar", "change"],
  ["calendar_delete", "calendar", "change"],
  ["contacts_search", "contacts", "read"],
  ["contacts_create", "contacts", "change"],
  ["contacts_update", "contacts", "change"],
  ["contacts_delete", "contacts", "change"],
  ["docker_list", "docker", "read"],
  ["vm_list", "vms", "read"],
  ["web_search", "web", "read"],
].map(([name, area, kind]) => ({
  name,
  area,
  kind,
  dangerous: false,
  mode: "allow",
  set: false,
}));
const assistAreas = [
  "home",
  "system",
  "storage",
  "backups",
  "docker",
  "vms",
  "files",
  "shares",
  "accounts",
  "network",
  "updates",
  "plugins",
  "web",
  "memory",
  "plan",
  "code",
];

async function open({
  width = 1440,
  height = 1300,
  theme = "hell",
  lang = "de",
  isActor = true,
} = {}) {
  const phone = width < 600;
  const made = await create(
    "owner",
    theme,
    width,
    lang,
    phone
      ? {
          isMobile: true,
          hasTouch: true,
          deviceScaleFactor: 2,
          viewport: { width, height: 844 },
        }
      : { viewport: { width, height } },
  );
  const seen = { puts: [], consents: [] };
  // S-23: Assist acts as alex; the page is alex's own (isActor) or ada's.
  let consent = {};
  let settings = baseSettings();
  await made.ctx.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname.slice(7);
    const method = route.request().method();
    const json = (value, status = 200) =>
      route.fulfill({ status, json: value });
    if (p === "/services/state")
      return json({ docker: true, vms: true, lxc: true, agent: true });
    if (!p.startsWith("/agent")) return route.fallback();
    if (p === "/agent/status") return json(status);
    if (p === "/agent/settings") {
      if (method === "PUT") {
        const body = route.request().postDataJSON();
        seen.puts.push(body);
        settings = body;
        return json(body);
      }
      return json(settings);
    }
    if (p === "/agent/tools") return json({ tools, areas: [] });
    if (p === "/agent/assist/consent" && method === "PUT") {
      const body = route.request().postDataJSON();
      seen.consents.push(body);
      consent = { ...body };
      return json(body);
    }
    if (p === "/agent/assist")
      return json({
        keySet: true,
        addresses: [{ kind: "lan", url: "http://192.168.10.20/agent-ollama" }],
        areas: assistAreas,
        levels: Object.fromEntries(assistAreas.map((a) => [a, "deny"])),
        dataAreas: ["calendar", "contacts"],
        dataLevels: { calendar: "deny", contacts: "deny" },
        last: null,
        memories: 0,
        chats: 0,
        actor: "alex",
        isActor,
        consent: isActor ? consent : {},
        myConsent: isActor ? consent : {},
      });
    if (method === "GET" && p === "/agent/cluster")
      return json({ member: false, admin: false });
    return json({ error: { code: "not_found", message: "fixture" } }, 404);
  });
  return { ...made, seen, phone };
}

/** No sideways scroll and no cut text. */
async function sound(page, name) {
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  assert.ok(overflow <= 1, `${name}: scrolls sideways by ${overflow}px`);
  assert.deepEqual(await page.evaluate(cutTextIn), [], `${name}: cut text`);
}
const shot = (page, name, fullPage = false) =>
  page.screenshot({
    path: `${out}/${name}.png`,
    animations: "disabled",
    fullPage,
  });
const areas = (page) =>
  page.locator("section", {
    has: page.getByRole("heading", { name: "Bereiche" }),
  });
/** A row of an area by its exact title. */
const row = (page, title) =>
  areas(page)
    .locator(".srow")
    .filter({
      has: page.locator(".srow-title", {
        hasText: new RegExp(`^\\s*${title}\\s*(neu)?\\s*$`),
      }),
    })
    .first();
const checked = async (group, name) =>
  (await group.getByRole("radio", { name }).getAttribute("aria-checked")) ===
  "true";

try {
  // ---- Rechte, desktop light and dark, then phone light and dark ----
  for (const [width, theme] of [
    [1440, "hell"],
    [1440, "graphit"],
    [390, "hell"],
    [390, "graphit"],
  ]) {
    const { ctx, page, seen, phone } = await open({ width, theme });
    const tag = `${phone ? "handy" : "1440"}-${theme === "hell" ? "hell" : "dunkel"}`;
    await page.goto(base + "/settings/agent?tab=rights");
    await page.getByRole("heading", { name: "Bereiche" }).waitFor();
    const cal = row(page, "Kalender");
    const con = row(page, "Kontakte");
    await cal.waitFor();
    // After Dateien, in this order.
    const titles = await areas(page)
      .locator(":scope .srow > .srow-head .srow-title")
      .allInnerTexts();
    const clean = titles.map((x) => x.replace(/\s*neu\s*$/, "").trim());
    const i = clean.indexOf("Dateien");
    assert.deepEqual(
      clean.slice(i, i + 3),
      ["Dateien", "Kalender", "Kontakte"],
      `${tag}: order ${clean.join(", ")}`,
    );
    // Closed: the state as text, no tool list, no single tool names.
    for (const r of [cal, con]) {
      assert.equal(
        (await r.locator(".srow-summary").innerText()).trim(),
        "Lesen · Eintragen mit Nachfrage",
        `${tag}: summary from the factory`,
      );
    }
    assert.equal(
      await areas(page).getByText("calendar_create").count(),
      0,
      `${tag}: no single tools of the calendar`,
    );
    assert.equal(
      await page.getByRole("button", { name: "Speichern" }).count(),
      0,
      `${tag}: unchanged after loading`,
    );
    const preset = page.getByRole("radiogroup", { name: "Schnellwahl" });
    assert.ok(await checked(preset, "Standard"), `${tag}: Standard`);
    // Open: the two parts with their choices.
    await page.getByRole("button", { name: "Kalender: Aufklappen" }).click();
    const read = page.getByRole("radiogroup", { name: "Termine lesen" });
    const write = page.getByRole("radiogroup", {
      name: "Termine eintragen, ändern und löschen",
    });
    await read.waitFor();
    assert.deepEqual(
      await read.getByRole("radio").allInnerTexts(),
      ["Erlaubt", "Verboten"],
      `${tag}: reading`,
    );
    assert.deepEqual(
      await write.getByRole("radio").allInnerTexts(),
      ["Ohne Nachfrage", "Nachfragen", "Verboten"],
      `${tag}: writing`,
    );
    assert.ok(await checked(read, "Erlaubt"), `${tag}: read allowed`);
    assert.ok(await checked(write, "Nachfragen"), `${tag}: write asks`);
    await page
      .getByText("gelesene Termine zählen als fremder Inhalt")
      .waitFor();
    await cal.scrollIntoViewIfNeeded();
    await page.waitForTimeout(300);
    await sound(page, `${tag} open`);
    await shot(page, `rechte-${tag}`);
    if (theme === "hell" && !phone)
      await shot(page, `rechte-${tag}-ganz`, true);
    // A change: the quick choice becomes "Eigene Einstellung".
    await write.getByRole("radio", { name: "Ohne Nachfrage" }).click();
    assert.equal(
      (await cal.locator(".srow-summary").innerText()).trim(),
      "Lesen · Eintragen ohne Nachfrage",
      `${tag}: summary follows`,
    );
    assert.ok(await checked(preset, "Eigene Einstellung"), `${tag}: Eigene`);
    await page.getByRole("button", { name: "Kontakte: Aufklappen" }).click();
    await page
      .getByRole("radiogroup", { name: "Kontakte lesen" })
      .getByRole("radio", { name: "Verboten" })
      .click();
    await page
      .getByRole("radiogroup", { name: "Kontakte anlegen, ändern und löschen" })
      .getByRole("radio", { name: "Verboten" })
      .click();
    assert.equal(
      (await con.locator(".srow-summary").innerText()).trim(),
      "Nicht lesen · nicht eintragen",
      `${tag}: contacts closed`,
    );
    await sound(page, `${tag} changed`);
    if (!phone) {
      await page.getByRole("button", { name: "Speichern" }).click();
      await page.getByRole("status").first().waitFor();
      const put = seen.puts.at(-1);
      assert.deepEqual(
        put.policy.dataRights,
        {
          calendar: { read: "allow", write: "allow" },
          contacts: { read: "deny", write: "deny" },
        },
        `${tag}: both axes sent`,
      );
      for (const a of ["calendar", "contacts"]) {
        assert.equal(put.policy.areas?.[a], undefined, "never under areas");
        assert.equal(put.policy.extraAreas?.[a], undefined, "no extraAreas");
      }
      for (const name of tools.map((t) => t.name))
        if (/^(calendar|contacts)_/.test(name))
          assert.equal(put.policy.tools?.[name], undefined, "no tool rules");
      // A quick choice clears dataRights like areas and tools.
      await preset.getByRole("radio", { name: "Nur lesen" }).click();
      assert.equal(
        (await cal.locator(".srow-summary").innerText()).trim(),
        "Lesen · nicht eintragen",
        `${tag}: read only preset`,
      );
      await preset.getByRole("radio", { name: "Alles ohne Nachfrage" }).click();
      assert.equal(
        (await con.locator(".srow-summary").innerText()).trim(),
        "Lesen · Eintragen ohne Nachfrage",
        `${tag}: all preset`,
      );
      await page.getByRole("button", { name: "Speichern" }).click();
      await page.waitForTimeout(400);
      assert.equal(
        seen.puts.at(-1).policy.dataRights,
        undefined,
        `${tag}: preset cleared dataRights`,
      );
      assert.equal(seen.puts.at(-1).policy.preset, "all");
    } else {
      await shot(page, `rechte-${tag}-geaendert`);
    }
    await ctx.close();
  }

  // ---- English once ----
  {
    const { ctx, page } = await open({ width: 1440, lang: "en" });
    await page.goto(base + "/settings/agent?tab=rights");
    await page
      .getByText("Events in Calendar, all calendars of this account")
      .waitFor();
    assert.equal(
      (
        await page
          .locator(".srow-summary", { hasText: "Read ·" })
          .first()
          .innerText()
      ).trim(),
      "Read · write after asking",
      "English summary",
    );
    await sound(page, "english");
    await shot(page, "rechte-1440-en");
    await ctx.close();
  }

  // ---- Home Assistant: only Verboten / Nur lesen, saved in assistData ----
  for (const [width, theme] of [
    [1440, "hell"],
    [1440, "graphit"],
    [390, "hell"],
    [390, "graphit"],
  ]) {
    const { ctx, page, seen, phone } = await open({ width, theme });
    const tag = `${phone ? "handy" : "1440"}-${theme === "hell" ? "hell" : "dunkel"}`;
    await page.goto(base + "/settings/agent?tab=ha");
    const rights = page.locator("section.sgroup", {
      has: page.getByRole("heading", {
        name: "Was Home Assistant fragen darf",
      }),
    });
    await rights.waitFor();
    assert.equal(
      await rights.locator(".srow").count(),
      18,
      `${tag}: sixteen areas and the two of 1.11`,
    );
    for (const [title, hint] of [
      ["Kalender", "Liest den Kalender des Kontos ‚Assist als‘"],
      ["Kontakte", "Liest die Kontakte des Kontos ‚Assist als‘"],
    ]) {
      const group = rights.getByRole("radiogroup", { name: title });
      assert.deepEqual(
        await group.getByRole("radio").allInnerTexts(),
        ["Verboten", "Nur lesen"],
        `${tag}: ${title} has no "Auch ändern"`,
      );
      assert.ok(await checked(group, "Verboten"), `${tag}: ${title} denied`);
      await rights.getByText(hint).waitFor();
    }
    await rights
      .getByRole("radiogroup", { name: "Kalender" })
      .getByRole("radio", { name: "Nur lesen" })
      .click();
    await rights
      .getByRole("radiogroup", { name: "Kalender" })
      .scrollIntoViewIfNeeded();
    // S-23: "Nur lesen" waits for alex's own consent, given below.
    await rights.getByText("wartet auf deine Zustimmung (unten)").waitFor();
    const consentGroup = page.locator("section.sgroup", {
      has: page.getByRole("heading", {
        name: "Deine Zustimmung: Kalender und Kontakte für Assist",
      }),
    });
    await consentGroup.waitFor();
    await consentGroup
      .getByText("selbst danach fragen", { exact: false })
      .waitFor();
    await page.waitForTimeout(300);
    await sound(page, `${tag} assist`);
    await shot(page, `assist-${tag}`);
    await consentGroup
      .getByRole("switch", { name: "Assist darf meinen Kalender lesen" })
      .click();
    await consentGroup.getByText("Zugestimmt – nur lesen, nie ändern").waitFor();
    assert.deepEqual(
      seen.consents.at(-1),
      { calendar: true },
      `${tag}: consent sent for alex's own account`,
    );
    assert.equal(
      await rights.getByText("wartet auf deine Zustimmung (unten)").count(),
      0,
      `${tag}: no longer waiting`,
    );
    assert.equal(
      seen.puts.length,
      0,
      `${tag}: the consent is no setting of the page`,
    );
    if (!phone) {
      await page.getByRole("button", { name: "Speichern" }).click();
      await page.getByRole("status").first().waitFor();
      const ha = seen.puts.at(-1).homeassistant;
      assert.deepEqual(
        ha.assistData,
        { calendar: "read" },
        `${tag}: assistData`,
      );
      assert.equal(ha.assistAreas?.calendar, undefined, "never assistAreas");
      // "Alles verbieten" includes the two; "Zurück auf ab Werk" clears them.
      await rights.getByRole("button", { name: "Alles verbieten" }).click();
      await page.getByRole("button", { name: "Speichern" }).click();
      await page.waitForTimeout(400);
      const denied = seen.puts.at(-1).homeassistant;
      assert.deepEqual(denied.assistData, {
        calendar: "deny",
        contacts: "deny",
      });
      assert.equal(denied.assistAreas.calendar, undefined);
      await rights.getByRole("button", { name: "Zurück auf ab Werk" }).click();
      await page.getByRole("button", { name: "Speichern" }).click();
      await page.waitForTimeout(400);
      assert.equal(seen.puts.at(-1).homeassistant.assistData, undefined);
    }
    await ctx.close();
  }

  // ---- an administrator: waits for alex, no consent of its own here ----
  {
    const { ctx, page, seen } = await open({ isActor: false });
    await page.goto(base + "/settings/agent?tab=ha");
    const rights = page.locator("section.sgroup", {
      has: page.getByRole("heading", { name: "Was Home Assistant fragen darf" }),
    });
    await rights.waitFor();
    await rights
      .getByRole("radiogroup", { name: "Kontakte" })
      .getByRole("radio", { name: "Nur lesen" })
      .click();
    await rights.getByText("wartet auf Zustimmung von alex").waitFor();
    assert.equal(
      await page
        .getByRole("heading", {
          name: "Deine Zustimmung: Kalender und Kontakte für Assist",
        })
        .count(),
      0,
      "admin: no consent switches for someone else",
    );
    await sound(page, "admin assist");
    await shot(page, "assist-admin-1440-hell");
    assert.equal(seen.consents.length, 0);
    await ctx.close();
  }

  assert.deepEqual(errors, [], "script errors on the page");
  console.log(
    `Agent rights (calendar, contacts): all checks passed, screenshots in ${out}`,
  );
} finally {
  await browser.close();
}
