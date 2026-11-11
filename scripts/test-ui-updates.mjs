// Browser checks of Settings › Updates for app updates, against isolated API fixtures.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-updates.mjs
// UI_URL picks the dev server (default http://127.0.0.1:5179), UI_OUTPUT the screenshot folder.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
const { chromium } = await import(
  process.env.PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.UI_URL || "http://127.0.0.1:5179";
const out = process.env.UI_OUTPUT || "out/validation/updates";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.BROWSER_EXECUTABLE,
});
const errors = [];
const time = "2026-10-21T10:00:00Z";
const system = {
  hostname: "titan-test",
  version: "0.8.0",
  kernel: "test",
  uptimeSeconds: 1000,
  cpu: { model: "Test CPU", threads: 8, usagePercent: 12 },
  memory: {
    totalBytes: 16e9,
    usedBytes: 3e9,
    freeBytes: 13e9,
    availableBytes: 13e9,
  },
  temperatures: [],
  accelerators: [],
  image: {
    slot: "a",
    sizeBytes: 1e9,
    overlayUsedBytes: 0,
    overlayTotalBytes: 512e6,
  },
  stick: { present: true, label: "TITANOS" },
  network: null,
  networks: [],
  networkPendingSeconds: 0,
  notificationsLatest: 0,
};
const slots = [
  {
    name: "a",
    version: "0.8.0",
    running: true,
    complete: true,
    next: true,
    triesLeft: null,
  },
  {
    name: "b",
    version: "0.7.2",
    running: false,
    complete: true,
    next: false,
    triesLeft: null,
  },
];
const manifest = {
  version: "0.8.1",
  channel: "stable",
  kernel: "6.12.48",
  released: time,
  changelog: "Kleinere Verbesserungen der VM-Seite.",
  files: [{ size: 300e6 }, { size: 40e6 }, { size: 10e6 }],
  app: { base: "0.8.0", size: 19e6 },
};
let state;
let installs = 0,
  appRollbacks = 0,
  failRestart = 0;
let volumes = [];
// Releases to choose from (Settings › Updates › Choose a version).
let versions = [
  {
    version: "0.46.5",
    channel: "stable",
    released: "2026-11-12T03:00:00Z",
    kernel: "6.12.117",
    size: 412e6,
    changelog: "# TitanOS 0.46.5 — Sicherheitsupdate (Autopilot)",
    running: false,
    newest: true,
  },
  {
    version: "0.8.0",
    channel: "stable",
    released: "2026-11-05T03:00:00Z",
    kernel: "6.12.115",
    size: 410e6,
    changelog: "# TitanOS 0.8.0 — Läuft",
    running: true,
    newest: false,
  },
  {
    version: "0.46.2",
    channel: "stable",
    released: "2026-11-03T08:00:00Z",
    kernel: "6.12.115",
    size: 409e6,
    changelog:
      "# TitanOS 0.46.2 — Kernel 6.12.115, neue Signierschlüssel\n\nMehr.",
    running: false,
    newest: false,
  },
];
const chosenVersions = [];
const progress = (phase) => ({
  phase,
  file: "",
  percent: phase === "restart" ? 100 : 0,
  speedBytesPerSecond: 0,
  error: "",
});
function reset() {
  state = {
    version: "0.8.0",
    release: true,
    slots,
    settings: { channel: "stable", autoInstall: false, installTime: "03:30" },
    canVerify: true,
    available: manifest,
    lastCheck: time,
    checkError: "",
    progress: progress(""),
    pending: null,
    canRollback: true,
    unconfirmed: false,
    base: "0.8.0",
    kind: "app",
    app: { current: "", previous: "", trial: false, canRollback: false },
  };
}
reset();

async function context(theme = "graphit", width = 1440) {
  const ctx = await browser.newContext({
    locale: "de-DE",
    viewport: { width, height: 1000 },
  });
  await ctx.addInitScript(
    ({ theme }) => {
      localStorage.setItem("titanos.locale", "de");
      // PLAN 65: the page alone, the way its window shows it.
      localStorage.setItem("titanos.layout", "bare");
      localStorage.setItem("titanos.appearance", JSON.stringify({ theme }));
    },
    { theme },
  );
  await ctx.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.slice(
      "/api/v1".length,
    );
    const method = route.request().method();
    const json = (data, status = 200) => route.fulfill({ status, json: data });
    if (path === "/session")
      return json({
        configAvailable: true,
        setupRequired: false,
        user: { name: "tester", role: "owner", twoFactor: false },
        hostname: "titan-test",
        version: state.version,
      });
    if (path === "/preferences")
      return json({
        appearance: { theme },
        dashboard: null,
        docker: null,
        vnc: null,
        sound: null,
      });
    if (path === "/system") return json({ ...system, version: state.version });
    if (path === "/storage")
      return json({
        volumes,
        disks: [],
        diskSleep: { idleMinutes: 0, available: true, errors: [] },
      });
    if (path === "/notifications") return json({ messages: [] });
    if (path === "/update" && method === "GET") {
      // While titand restarts it does not answer.
      if (failRestart > 0) {
        failRestart--;
        return route.abort();
      }
      return json(state);
    }
    if (path === "/update/install" && state.kind === "system") {
      // A system update: loading; the test moves it on.
      installs++;
      state = {
        ...state,
        progress: {
          ...progress("download"),
          file: "filesystem.squashfs",
          percent: 62,
          speedBytesPerSecond: 18e6,
        },
      };
      return json(state);
    }
    if (path === "/update/install") {
      installs++;
      state = { ...state, progress: progress("restart") };
      // The next requests fail (restart), then the app answers.
      setTimeout(() => {
        failRestart = 1;
        state = {
          ...state,
          version: "0.8.1",
          available: null,
          kind: "",
          progress: progress(""),
          app: {
            current: "0.8.1",
            previous: "",
            trial: true,
            canRollback: false,
          },
        };
      }, 300);
      return json(state);
    }
    if (path === "/update/versions") return json({ floor: "0.46.2", versions });
    if (path === "/update/install-version") {
      const body = route.request().postDataJSON();
      chosenVersions.push(body.version);
      state = {
        ...state,
        settings: { ...state.settings, autoInstall: false },
        pending: { kind: "update", version: body.version, slot: "b" },
      };
      return json(state);
    }
    if (path === "/update/app-rollback") {
      appRollbacks++;
      state = { ...state, progress: progress("restart") };
      setTimeout(() => {
        state = {
          ...state,
          version: "0.8.0",
          progress: progress(""),
          app: {
            current: "",
            previous: "0.8.1",
            trial: false,
            canRollback: true,
          },
        };
      }, 300);
      return json(state);
    }
    return json({});
  });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => errors.push(String(e)));
  return { ctx, page };
}

try {
  {
    const { ctx, page } = await context();
    await page.goto(`${base}/settings/updates`);
    // PLAN 78-3: the card of the new version says what it is.
    await page.getByRole("heading", { name: "TitanOS 0.8.1 ist da" }).waitFor();
    assert.equal(
      (await page.getByTestId("update-kind").textContent()).trim(),
      "App-Update · ohne Neustart",
    );
    assert.ok(await page.getByText("Ohne Neustart").isVisible());
    assert.ok(
      /1[89][,.]?\d* MB/.test(
        await page.getByTestId("update-size").textContent(),
      ),
    );
    assert.equal(
      (await page.locator(".hero .jump .sr-only").textContent()).trim(),
      "Von 0.8 auf 0.8.1",
    );
    await page.screenshot({
      path: `${out}/app-available-graphit-1440.png`,
      fullPage: true,
    });
    await page.getByRole("button", { name: "Jetzt installieren" }).click();
    assert.ok(
      await page
        .getByText("startet danach nur seine Weboberfläche neu", {
          exact: false,
        })
        .isVisible(),
    );
    assert.ok(
      await page
        .getByText("18,1 MB", { exact: false })
        .or(page.getByText("19 MB", { exact: false }))
        .first()
        .isVisible(),
    );
    await page.screenshot({
      path: `${out}/app-confirm-graphit-1440.png`,
      fullPage: true,
    });
    await page.locator(".dialog-actions .btn-primary").click();
    // The steps of an app update: the service restarts, loading and
    // checking are done.
    const appSteps = page.getByTestId("update-steps");
    await appSteps
      .locator('li[aria-current="step"]', { hasText: "Neustart des Dienstes" })
      .waitFor();
    assert.deepEqual(
      await appSteps.locator("li.done .step-name").allTextContents(),
      ["Laden", "Prüfen"],
    );
    assert.equal(await appSteps.locator("li").count(), 4);
    assert.equal(await page.getByTestId("update-hero").count(), 0);
    // The page waits for the new titand and says which version runs.
    await page
      .getByText(
        "TitanOS 0.8.1 läuft. Das NAS wurde dafür nicht neu gestartet.",
      )
      .waitFor({ timeout: 15000 });
    assert.ok(
      await page
        .getByText("Die neue Version läuft auf Probe", { exact: false })
        .isVisible(),
    );
    // The trial is the last step of an app update.
    await appSteps
      .locator('li[aria-current="step"]', { hasText: "Probe" })
      .waitFor();
    assert.equal(await appSteps.locator("li.done").count(), 3);
    assert.ok(
      await page.getByText("App-Update auf Systemabbild 0.8").isVisible(),
    );
    assert.equal(installs, 1);
    await page.screenshot({
      path: `${out}/app-trial-graphit-1440.png`,
      fullPage: true,
    });

    // Confirmed: the way back to the titand of the system image.
    state = {
      ...state,
      app: { current: "0.8.1", previous: "", trial: false, canRollback: true },
    };
    await page.reload();
    await page
      .getByRole("button", { name: "Zurückgehen … – Systemabbild 0.8.0" })
      .click();
    await page.locator(".dialog-actions .btn-primary").click();
    await page
      .getByText("TitanOS 0.8.0 läuft.", { exact: false })
      .waitFor({ timeout: 15000 });
    assert.equal(appRollbacks, 1);
    assert.ok(
      await page
        .getByRole("button", { name: "Zurückgehen … – 0.8.1" })
        .isVisible(),
    );
    await ctx.close();
  }
  {
    // A release for another system image is a system update, as before.
    reset();
    state = {
      ...state,
      kind: "system",
      available: {
        ...manifest,
        version: "0.9.0",
        app: { base: "0.9.0", size: 19e6 },
      },
    };
    const { ctx, page } = await context("hell", 390);
    await page.goto(`${base}/settings/updates`);
    await page.getByRole("heading", { name: "TitanOS 0.9 ist da" }).waitFor();
    assert.equal(await page.getByText("Ohne Neustart").count(), 0);
    assert.equal(
      (await page.getByTestId("update-kind").textContent()).trim(),
      "Systemupdate · mit Neustart",
    );
    await page.screenshot({
      path: `${out}/system-available-hell-390.png`,
      fullPage: true,
    });
    await page.getByRole("button", { name: "Jetzt installieren" }).click();
    assert.ok(
      await page
        .getByText("Aktiv wird die neue Version erst mit einem Neustart", {
          exact: false,
        })
        .isVisible(),
    );
    const overflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    );
    assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
    await ctx.close();
  }
  {
    // Going back from 0.31.0 to 0.30.0 with an encrypted volume: the
    // dialog says that 0.30.0 cannot open it.
    reset();
    state = {
      ...state,
      version: "0.31.0",
      base: "0.31.0",
      available: null,
      kind: "",
      slots: [
        { ...slots[0], version: "0.31.0" },
        { ...slots[1], version: "0.30.0" },
      ],
    };
    volumes = [
      { name: "Data", mounted: true, state: "ok" },
      {
        name: "vault",
        mounted: true,
        state: "ok",
        encryption: { locked: false, members: 1, unlocked: 1 },
      },
    ];
    const { ctx, page } = await context();
    await page.goto(`${base}/settings/updates`);
    await page
      .getByRole("button", { name: /0\.30\.0/ })
      .first()
      .click();
    const warning = page.getByTestId("rollback-encrypted");
    await warning.waitFor();
    assert.ok((await warning.textContent()).includes("vault"));
    assert.ok(!(await warning.textContent()).includes("Data"));
    await page.screenshot({
      path: `${out}/rollback-encrypted-graphit-1440.png`,
      fullPage: true,
    });
    await ctx.close();
    // Without encrypted volumes there is nothing to warn about.
    volumes = [{ name: "Data", mounted: true, state: "ok" }];
    const second = await context();
    await second.page.goto(`${base}/settings/updates`);
    await second.page
      .getByRole("button", { name: /0\.30\.0/ })
      .first()
      .click();
    await second.page.locator(".dialog-actions").waitFor();
    await second.page.waitForTimeout(500);
    assert.equal(
      await second.page.getByTestId("rollback-encrypted").count(),
      0,
    );
    await second.ctx.close();
  }
  {
    // Choose a version: an older one switches installing by itself off.
    reset();
    state = {
      ...state,
      available: null,
      kind: "",
      settings: { ...state.settings, autoInstall: true },
    };
    const { ctx, page } = await context();
    await page.goto(`${base}/settings/updates`);
    // PLAN 78-3: "Version wählen" is a row that opens downward.
    await page
      .getByRole("button", { name: "Version wählen: Aufklappen" })
      .click();
    const select = page.getByLabel("Version", { exact: true });
    await select.waitFor();
    assert.equal(await select.inputValue(), "0.46.5");
    assert.ok(
      await page
        .getByText("Sicherheitsupdate (Autopilot)", { exact: true })
        .isVisible(),
    );
    await select.selectOption("0.46.2");
    assert.ok(
      await page
        .getByText("Kernel 6.12.115, neue Signierschlüssel", { exact: true })
        .isVisible(),
    );
    await page.screenshot({
      path: `${out}/choose-graphit-1440.png`,
      fullPage: true,
    });
    await page
      .getByRole("button", { name: "TitanOS 0.46.2 installieren …" })
      .click();
    assert.ok(
      await page.getByText("wird ausgeschaltet", { exact: false }).isVisible(),
    );
    await page.screenshot({ path: `${out}/choose-confirm-graphit-1440.png` });
    const installed = page.waitForResponse((res) =>
      res.url().endsWith("/update/install-version"),
    );
    await page.locator(".dialog-actions .btn-primary").click();
    await installed;
    await page.getByRole("dialog").waitFor({ state: "detached" });
    assert.deepEqual(chosenVersions, ["0.46.2"]);
    assert.equal(
      await page.getByLabel("Updates von selbst installieren").isChecked(),
      false,
    );
    // The running version cannot be installed again.
    await select.selectOption("0.8.0");
    assert.ok(
      await page
        .getByRole("button", { name: "TitanOS 0.8.0 installieren …" })
        .isDisabled(),
    );
    await ctx.close();
    const phone = await context("hell", 390);
    await phone.page.goto(`${base}/settings/updates`);
    await phone.page
      .getByRole("button", { name: "Version wählen: Aufklappen" })
      .click();
    await phone.page.getByLabel("Version", { exact: true }).waitFor();
    const overflow = await phone.page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    );
    assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
    await phone.page.screenshot({
      path: `${out}/choose-hell-390.png`,
      fullPage: true,
    });
    await phone.ctx.close();
  }
  {
    // Running 0.46.3: an older version warns about newer settings, the
    // newest one keeps installing by itself and warns about nothing.
    reset();
    const listed = versions;
    versions = [
      { ...listed[0] },
      {
        version: "0.46.3",
        channel: "stable",
        released: "2026-11-04T03:00:00Z",
        kernel: "6.12.115",
        size: 410e6,
        changelog: "# TitanOS 0.46.3 — Sicherheitsupdate (Autopilot)",
        running: true,
        newest: false,
      },
      { ...listed[2] },
    ];
    state = {
      ...state,
      version: "0.46.3",
      base: "0.46.3",
      available: null,
      kind: "",
      settings: { ...state.settings, autoInstall: true },
      slots: [
        { ...slots[0], version: "0.46.3" },
        { ...slots[1], version: "0.46.2" },
      ],
    };
    const { ctx, page } = await context();
    await page.goto(`${base}/settings/updates`);
    await page
      .getByRole("button", { name: "Version wählen: Aufklappen" })
      .click();
    const select = page.getByLabel("Version", { exact: true });
    await select.waitFor();
    await select.selectOption("0.46.2");
    await page
      .getByRole("button", { name: "TitanOS 0.46.2 installieren …" })
      .click();
    const dialog = page.getByRole("dialog");
    assert.ok(
      await dialog
        .getByText("Eine ältere Version kennt Einstellungen nicht", {
          exact: false,
        })
        .isVisible(),
    );
    assert.ok(
      await dialog
        .getByText("wird ausgeschaltet", { exact: false })
        .isVisible(),
    );
    await page.screenshot({
      path: `${out}/choose-older-graphit-1440.png`,
      animations: "disabled",
    });
    await dialog.getByRole("button", { name: "Abbrechen" }).click();
    await select.selectOption("0.46.5");
    await page
      .getByRole("button", { name: "TitanOS 0.46.5 installieren …" })
      .click();
    assert.equal(
      await dialog
        .getByText("Eine ältere Version kennt Einstellungen nicht", {
          exact: false,
        })
        .count(),
      0,
    );
    assert.equal(
      await dialog.getByText("wird ausgeschaltet", { exact: false }).count(),
      0,
    );
    await dialog.getByRole("button", { name: "Abbrechen" }).click();
    assert.deepEqual(chosenVersions, ["0.46.2"]); // only the earlier block installed
    await ctx.close();
    versions = listed;
  }
  {
    // PLAN 78-3: what is new in the update – the release notes in their
    // three tagged blocks, as text only.
    reset();
    installs = 0;
    const notes = [
      "# TitanOS 0.9 — Systemsteuerung im neuen Aufbau",
      "",
      "Ein **volles Systemupdate**. Tag und Manifest heißen `v0.9.0`.",
      "",
      "## Neu",
      "",
      "**Oberfläche**",
      "- **Systemsteuerung:** jede Einstellung eine eigene Zeile.",
      "- Punkt zwei, der in der nächsten",
      "  Zeile weitergeht.",
      "- Punkt drei.",
      "",
      "**Cluster**",
      "- Punkt vier.",
      "- Punkt fünf.",
      "- Punkt sechs.",
      "- Punkt sieben.",
      "- Punkt acht.",
      "",
      "## Sicherheit",
      "",
      "- <img src=x onerror=window.__pwned=1> bleibt Text.",
      "- [link](https://evil.example) bleibt Text.",
      "- Siehe [Release](https://github.com/ra5on/TitanOS/releases/tag/v0.9.0).",
      "",
      "## Behoben",
      "",
      "- Eine Korrektur mit `mdadm --detail`.",
      "",
      "## Rückweg",
      "",
      "Über Einstellungen › Updates zurück auf 0.8.",
    ].join("\n");
    state = {
      ...state,
      kind: "system",
      available: {
        ...manifest,
        version: "0.9.0",
        changelog: notes,
        app: { base: "0.9.0", size: 19e6 },
      },
    };
    for (const [theme, width] of [
      ["graphit", 1100],
      ["hell", 390],
    ]) {
      const { ctx, page } = await context(theme, width);
      await page.goto(`${base}/settings/updates`);
      const news = page.locator("#news");
      await news.waitFor();
      // The card: version jump, kind and size.
      const hero = page.getByTestId("update-hero");
      assert.equal(
        (await hero.locator(".jump .sr-only").textContent()).trim(),
        "Von 0.8 auf 0.9",
      );
      assert.deepEqual(await hero.locator(".jump b").allTextContents(), [
        "0.8",
        "0.9",
      ]);
      assert.ok(
        /\d+ MB/.test(await hero.getByTestId("update-size").textContent()),
      );
      assert.ok(
        await hero
          .getByText("Signatur des Releases geprüft", { exact: false })
          .isVisible(),
      );
      assert.ok(await hero.getByText("Kernel 6.12.48").isVisible());
      // Closed, the row sums up what the notes hold.
      assert.ok(
        await news
          .getByText("8 Neuerungen · 3 zur Sicherheit · 1 Korrektur", {
            exact: true,
          })
          .isVisible(),
      );
      assert.equal(await news.locator(".notes").count(), 0);
      await page
        .getByRole("button", { name: "Das ist neu in 0.9: Aufklappen" })
        .click();
      await news.locator(".notes").waitFor();
      assert.deepEqual(
        await news
          .locator(".notes section")
          .evaluateAll((all) =>
            all.map((s) => [
              s.dataset.kind,
              s.querySelector("h4").textContent.trim(),
            ]),
          ),
        [
          ["intro", "Zu diesem Release"],
          ["new", "Neu"],
          ["security", "Sicherheit"],
          ["fixed", "Behoben"],
          ["other", "Rückweg"],
        ],
      );
      // The tags are chips with their word, not a colour alone.
      assert.equal(await news.locator("h4 .chip").count(), 3);
      assert.ok(
        await news
          .getByText("Systemsteuerung im neuen Aufbau", { exact: true })
          .isVisible(),
      );
      // Bold and code are elements, sub-headings name their bullets.
      assert.equal(
        await news.locator('[data-kind="new"] li strong').first().textContent(),
        "Systemsteuerung:",
      );
      assert.equal(
        await news.locator('[data-kind="fixed"] code').textContent(),
        "mdadm --detail",
      );
      assert.ok(
        await news.getByText("Oberfläche", { exact: true }).isVisible(),
      );
      assert.ok(
        await news
          .getByText("Punkt zwei, der in der nächsten Zeile weitergeht.")
          .isVisible(),
      );
      // A long block shows its first six points and a button for the rest.
      const fresh = news.locator('[data-kind="new"]');
      assert.equal(await fresh.locator("li").count(), 6);
      assert.equal(await fresh.getByText("Punkt sieben.").count(), 0);
      await fresh.getByRole("button", { name: "Alle 8 anzeigen" }).click();
      assert.equal(await fresh.locator("li").count(), 8);
      assert.ok(await fresh.getByText("Punkt acht.").isVisible());
      await fresh.getByRole("button", { name: "Weniger anzeigen" }).click();
      assert.equal(await fresh.locator("li").count(), 6);
      assert.equal(
        await news
          .locator('[data-kind="security"]')
          .getByRole("button")
          .count(),
        0,
      );
      // Nothing of the notes becomes HTML; only links to the project are links.
      assert.equal(await news.locator("img, script, iframe").count(), 0);
      assert.equal(await page.evaluate(() => window.__pwned), undefined);
      assert.ok(
        await news
          .getByText("<img src=x onerror=window.__pwned=1> bleibt Text.", {
            exact: true,
          })
          .isVisible(),
      );
      assert.ok(
        await news
          .getByText("[link](https://evil.example) bleibt Text.", {
            exact: true,
          })
          .isVisible(),
      );
      assert.deepEqual(
        await news
          .locator(".notes a")
          .evaluateAll((all) =>
            all.map((a) => [
              a.textContent.trim(),
              a.getAttribute("href"),
              a.target,
              a.rel,
            ]),
          ),
        [
          [
            "Release",
            "https://github.com/ra5on/TitanOS/releases/tag/v0.9.0",
            "_blank",
            "noopener noreferrer",
          ],
          [
            "Auf GitHub lesen",
            "https://github.com/ra5on/TitanOS/releases/tag/v0.9.0",
            "_blank",
            "noopener noreferrer",
          ],
        ],
      );
      // No text is cut, nothing scrolls sideways.
      const cut = await page.evaluate(() => {
        const found = [];
        for (const el of document.querySelectorAll("main *")) {
          const cs = getComputedStyle(el);
          if (cs.textOverflow === "ellipsis" || cs.webkitLineClamp !== "none")
            found.push(el.className);
        }
        return {
          found,
          over:
            document.documentElement.scrollWidth -
            document.documentElement.clientWidth,
        };
      });
      assert.deepEqual(cut.found, []);
      assert.ok(cut.over <= 0, `page scrolls sideways by ${cut.over}px`);
      await page.screenshot({
        path: `${out}/news-open-${theme}-${width}.png`,
        fullPage: true,
      });
      // "Was war neu?": the notes of the running version from the list of releases.
      const installed = page.locator("#installed");
      assert.ok(await installed.getByText("Was war neu?").isVisible());
      await page
        .getByRole("button", { name: "TitanOS 0.8: Aufklappen" })
        .click();
      assert.ok(
        await installed.getByText("Läuft", { exact: true }).isVisible(),
      );
      assert.equal(
        await installed.locator(".notes .source a").getAttribute("href"),
        "https://github.com/ra5on/TitanOS/releases/tag/v0.8.0",
      );
      await ctx.close();
    }

    // The steps of a system update follow the phases titand reports.
    const { ctx, page } = await context("graphit", 1100);
    await page.goto(`${base}/settings/updates`);
    await page.getByRole("button", { name: "Jetzt installieren" }).click();
    await page.locator(".dialog-actions .btn-primary").click();
    const steps = page.getByTestId("update-steps");
    const current = (name) =>
      steps.locator('li[aria-current="step"]', { hasText: name }).waitFor();
    const done = () => steps.locator("li.done .step-name").allTextContents();
    await current("Laden");
    assert.equal(installs, 1);
    assert.equal(await steps.locator("li").count(), 5);
    assert.deepEqual(await done(), []);
    assert.ok(await page.getByText("TitanOS 0.9 wird installiert").isVisible());
    assert.ok(await page.getByText("62 % · 18.0 MB/s").isVisible());
    assert.equal(
      await page.getByRole("progressbar").getAttribute("aria-valuenow"),
      "62",
    );
    assert.ok(
      await steps
        .getByText("kommt TitanOS 0.8 von selbst zurück", { exact: false })
        .isVisible(),
    );
    // The card of the update gives way while it installs.
    assert.equal(await page.getByTestId("update-hero").count(), 0);
    await page.screenshot({
      path: `${out}/steps-download-graphit-1100.png`,
      fullPage: true,
    });
    state = { ...state, progress: { ...progress("switch"), percent: 100 } };
    await current("Start einrichten");
    assert.deepEqual(await done(), ["Laden", "Prüfen"]);
    // Done is said in words too, not by the colour of the frame.
    assert.ok(
      (await steps.locator("li.done").first().textContent()).includes(
        "(erledigt)",
      ),
    );
    assert.equal(await steps.locator("li.done svg").count(), 2);
    assert.equal(await page.getByText("62 % · 18.0 MB/s").count(), 0);
    state = {
      ...state,
      progress: { ...progress("done"), percent: 100 },
      pending: { kind: "update", version: "0.9.0", slot: "b" },
    };
    await current("Neustart");
    assert.deepEqual(await done(), ["Laden", "Prüfen", "Start einrichten"]);
    assert.ok(
      await page.getByText("TitanOS 0.9 wartet auf den Neustart").isVisible(),
    );
    assert.ok(
      await steps
        .getByText("liegt auf dem Stick und startet beim nächsten Neustart", {
          exact: false,
        })
        .isVisible(),
    );
    assert.ok(
      await steps
        .getByRole("button", { name: "Jetzt neu starten …" })
        .isVisible(),
    );
    assert.ok(
      await steps.getByRole("button", { name: "Doch nicht" }).isVisible(),
    );
    assert.equal(await page.getByRole("progressbar").count(), 0);
    await page.screenshot({
      path: `${out}/steps-pending-graphit-1100.png`,
      fullPage: true,
    });
    // After the restart the new system runs on trial: the last step.
    state = {
      ...state,
      version: "0.9.0",
      base: "0.9.0",
      available: null,
      kind: "",
      pending: null,
      progress: progress(""),
      unconfirmed: true,
      slots: [
        { ...slots[0], running: false, next: false },
        { ...slots[1], version: "0.9.0", running: true, next: true },
      ],
    };
    await page.reload();
    await current("Probestart");
    assert.equal((await done()).length, 4);
    assert.ok(
      await steps
        .getByText("Dieses System läuft noch auf Probe", { exact: false })
        .isVisible(),
    );
    assert.ok(
      await steps
        .getByText("kommt TitanOS 0.8 von selbst zurück", { exact: false })
        .isVisible(),
    );
    await ctx.close();

    // Up to date, and no notes of the running version in the list: the row
    // of the installed version has nothing to open.
    reset();
    state = { ...state, available: null, kind: "" };
    const listed = versions;
    versions = listed.filter((v) => !v.running);
    const plain = await context("graphit", 1100);
    await plain.page.goto(`${base}/settings/updates`);
    await plain.page
      .getByText("TitanOS ist aktuell.", { exact: true })
      .waitFor();
    assert.equal(await plain.page.locator("#news").count(), 0);
    assert.equal(await plain.page.getByTestId("update-hero").count(), 0);
    assert.equal(await plain.page.getByTestId("update-steps").count(), 0);
    assert.equal(await plain.page.getByText("Was war neu?").count(), 0);
    assert.equal(
      await plain.page.locator("#installed button.srow-chev").count(),
      0,
    );
    await plain.page.screenshot({
      path: `${out}/up-to-date-graphit-1100.png`,
      fullPage: true,
    });
    await plain.ctx.close();
    versions = listed;
  }
  assert.deepEqual(errors, []);
  console.log(`ok – screenshots in ${out}`);
} finally {
  await browser.close();
}
